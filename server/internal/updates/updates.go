// Package updates reports what this deployment runs and what is available.
//
// It is a read-only service with one seam: a [Checker] that answers "what is the newest
// version of this component". The real checker asks the npm registry for pi and the
// bridge; a test hands in a table. Nothing here installs anything — applying an update is
// running a command the operator configured, which is why it lives in the API layer and
// goes through the task runner, not through this package.
package updates

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// The components a deployment runs, in the order a panel shows them.
const (
	ComponentServer  = "server"
	ComponentPi      = "pi"
	ComponentBridge  = "bridge"
	ComponentFlutter = "app"
)

// defaultTimeout bounds one lookup: an update check must never hold a request open.
const defaultTimeout = 5 * time.Second

// Component is what this deployment runs.
type Component struct {
	// Name is one of the Component* constants.
	Name string
	// Current is the version in use.
	Current string
	// Source describes where the current version came from (a build stamp, a package).
	Source string
}

// Checker answers what the newest version of a component is.
//
// It is an interface because the network is not a unit test's business: the real
// implementation asks npm, a test answers from a map or fails on purpose.
type Checker interface {
	// Latest returns the newest published version of one component.
	Latest(ctx context.Context, component string) (string, error)
}

// Report is one component's row in the panel.
type Report struct {
	// Name is the component.
	Name string `json:"name"`
	// Current is what this deployment runs.
	Current string `json:"current"`
	// Source describes where the current version came from.
	Source string `json:"source,omitempty"`
	// Latest is the newest published version, empty when the check failed.
	Latest string `json:"latest,omitempty"`
	// UpdateAvailable is true only when both versions are known and differ.
	UpdateAvailable bool `json:"updateAvailable"`
	// Error explains a failed check, so a client can say "unknown" instead of "current".
	Error string `json:"error,omitempty"`
	// CheckedAt is when the answer was produced.
	CheckedAt string `json:"checkedAt"`
}

// Service reports the state of one deployment.
type Service struct {
	components []Component
	checker    Checker
	timeout    time.Duration
}

// Config wires the service.
type Config struct {
	// Components are the things this deployment runs.
	Components []Component
	// Checker answers the lookups. Nil means checks are disabled, and every row says so.
	Checker Checker
	// Timeout bounds one lookup (default 5s).
	Timeout time.Duration
}

// New builds the service.
func New(cfg Config) *Service {
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	return &Service{components: cfg.Components, checker: cfg.Checker, timeout: cfg.Timeout}
}

// Check looks every component up, in parallel, and returns one row each.
//
// A failing lookup is a row with an error and no latest version: an offline deployment
// still gets the list of what it runs, which is the part it can act on.
func (s *Service) Check(ctx context.Context) []Report {
	reports := make([]Report, len(s.components))
	var group sync.WaitGroup
	for index, component := range s.components {
		index, component := index, component
		reports[index] = Report{
			Name:      component.Name,
			Current:   component.Current,
			Source:    component.Source,
			CheckedAt: time.Now().UTC().Format(time.RFC3339),
		}
		if s.checker == nil || component.Current == "" {
			reports[index].Error = "this deployment does not check for updates"
			continue
		}
		group.Add(1)
		go func() {
			defer group.Done()
			lookupCtx, cancel := context.WithTimeout(ctx, s.timeout)
			defer cancel()
			latest, err := s.checker.Latest(lookupCtx, component.Name)
			if err != nil {
				reports[index].Error = err.Error()
				return
			}
			if strings.TrimSpace(latest) == "" {
				// An empty answer is not "up to date": saying so would hide a broken
				// checker behind a reassuring row.
				reports[index].Error = "the registry answered no version"
				return
			}
			reports[index].Latest = latest
			reports[index].UpdateAvailable = Newer(latest, component.Current)
		}()
	}
	group.Wait()
	return reports
}

// Newer reports whether candidate is a newer version than current.
//
// It compares dot-separated numeric parts and ignores a leading `v`; a candidate that is
// not comparable (a tag, a commit) counts as not newer, because offering an update on a
// guess is worse than not offering one.
func Newer(candidate, current string) bool {
	left, okLeft := parse(candidate)
	right, okRight := parse(current)
	if !okLeft || !okRight {
		return false
	}
	for index := 0; index < len(left) || index < len(right); index++ {
		var a, b int
		if index < len(left) {
			a = left[index]
		}
		if index < len(right) {
			b = right[index]
		}
		if a != b {
			return a > b
		}
	}
	return false
}

// parse turns "v1.2.3" into [1,2,3], refusing anything that is not all digits.
func parse(version string) ([]int, bool) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(version), "v"))
	if trimmed == "" {
		return nil, false
	}
	// A pre-release suffix (`1.2.3-rc1`) is not ordered here: two builds of a project
	// that publishes them would need a rule this server does not have.
	if strings.ContainsAny(trimmed, "-+") {
		return nil, false
	}
	parts := strings.Split(trimmed, ".")
	numbers := make([]int, 0, len(parts))
	for _, part := range parts {
		var number int
		if _, err := fmt.Sscanf(part, "%d", &number); err != nil || fmt.Sprintf("%d", number) != part {
			return nil, false
		}
		numbers = append(numbers, number)
	}
	return numbers, true
}

// HTTPChecker asks a registry for the newest version of a package.
type HTTPChecker struct {
	// Registry is the base URL of an npm-compatible registry (default DefaultRegistry).
	Registry string
	// Packages maps a component onto the package name it is published as.
	Packages map[string]string
	// Client is the HTTP client (default a client with a timeout).
	Client *http.Client
}

// DefaultRegistry is the public npm registry.
const DefaultRegistry = "https://registry.npmjs.org"

// Latest implements Checker: it reads the `dist-tags.latest` of one package.
func (c *HTTPChecker) Latest(ctx context.Context, component string) (string, error) {
	name, known := c.Packages[component]
	if !known {
		return "", fmt.Errorf("no package is known for %s", component)
	}
	registry := strings.TrimSuffix(c.Registry, "/")
	if registry == "" {
		registry = DefaultRegistry
	}
	client := c.Client
	if client == nil {
		client = &http.Client{Timeout: defaultTimeout}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, registry+"/"+name, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("the registry answered %d for %s", response.StatusCode, name)
	}
	var body struct {
		DistTags map[string]string `json:"dist-tags"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("the registry answer is unreadable: %v", err)
	}
	latest := strings.TrimSpace(body.DistTags["latest"])
	if latest == "" {
		return "", fmt.Errorf("the registry has no latest version for %s", name)
	}
	return latest, nil
}

// CheckerError is a lookup failure a caller may want to branch on.
type CheckerError struct {
	Component string
	Reason    error
}

// Error implements error.
func (e CheckerError) Error() string {
	return fmt.Sprintf("checking %s: %v", e.Component, e.Reason)
}

// Unwrap exposes the reason.
func (e CheckerError) Unwrap() error { return e.Reason }

// ErrNoChecker is what a server without a checker reports.
var ErrNoChecker = errors.New("this deployment does not check for updates")

// CodedFailure maps a lookup failure onto the taxonomy, so a REST handler can answer
// without knowing whether the registry timed out or answered nonsense.
func CodedFailure(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, context.DeadlineExceeded):
		return sessions.Codedf(sessions.CodeTimeout, "the update check timed out")
	default:
		return sessions.Codedf(sessions.CodeUnavailable, "the update check failed: %v", err)
	}
}
