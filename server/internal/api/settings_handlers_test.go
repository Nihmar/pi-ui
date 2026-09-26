package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/git"
	"github.com/Nihmar/pi-ui/server/internal/settings"
	"github.com/Nihmar/pi-ui/server/internal/store"
)

// newSettingsService opens a real state database: the settings only mean something
// once they are stored.
func newSettingsService(t *testing.T) *settings.Service {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return settings.New(db.Settings(), nil)
}

// adminAuth authenticates every request as an admin device, which is what the
// settings endpoints require; the loopback authenticator of the other tests grants
// operator only.
type adminAuth struct{}

// Authenticate implements Authenticator.
func (adminAuth) Authenticate(*http.Request) (Scope, error) {
	return ScopeAdmin, nil
}

// adminRequest builds a request with a JSON body for the settings endpoints.
func adminRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	return request
}

func TestSettingsAreReadableAndDefaulted(t *testing.T) {
	service := newSettingsService(t)
	handler := NewRouter(Options{Auth: NewLoopbackOrToken(""), Settings: service})

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	var body struct {
		Values   map[string]json.RawMessage `json:"values"`
		Defaults map[string]json.RawMessage `json:"defaults"`
		Known    []struct {
			Key         string `json:"key"`
			Kind        string `json:"kind"`
			Description string `json:"description"`
		} `json:"known"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if string(body.Values[settings.KeyGitWrite]) != "false" {
		t.Fatalf("git.write should be off: %s", body.Values[settings.KeyGitWrite])
	}
	if len(body.Known) != len(settings.Keys) {
		t.Fatalf("known keys = %d, want %d", len(body.Known), len(settings.Keys))
	}
	for _, entry := range body.Known {
		if entry.Kind == "" || entry.Description == "" {
			t.Fatalf("a catalogue row must explain itself: %+v", entry)
		}
	}
}

func TestSettingsChangeAppliesAndValidates(t *testing.T) {
	service := newSettingsService(t)
	handler := NewRouter(Options{Auth: adminAuth{}, Settings: service})

	recorder := do(t, handler, adminRequest(http.MethodPatch, "/api/v1/settings",
		`{"git.write":true,"terminal.maxSessions":8}`), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if !service.Bool(context.Background(), settings.KeyGitWrite) {
		t.Fatal("the change should be in force")
	}
	if got := service.Int(context.Background(), settings.KeyTerminalMax); got != 8 {
		t.Fatalf("terminal.maxSessions = %d", got)
	}

	// One bad value in a batch changes nothing at all.
	recorder = do(t, handler, adminRequest(http.MethodPatch, "/api/v1/settings",
		`{"audit.retentionDays":7,"nope.key":true}`), loopback)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if got := service.Int(context.Background(), settings.KeyAuditRetentionDays); got != 30 {
		t.Fatalf("a refused batch must not apply anything: retention = %d", got)
	}

	recorder = do(t, handler, adminRequest(http.MethodPatch, "/api/v1/settings", `{}`), loopback)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("an empty patch is a bad request, got %d", recorder.Code)
	}
}

func TestSettingsResetGoesBackToTheDefault(t *testing.T) {
	service := newSettingsService(t)
	handler := NewRouter(Options{Auth: adminAuth{}, Settings: service})

	do(t, handler, adminRequest(http.MethodPatch, "/api/v1/settings", `{"git.write":true}`), loopback)
	recorder := do(t, handler, httptest.NewRequest(http.MethodDelete,
		"/api/v1/settings/git.write", nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if service.Bool(context.Background(), settings.KeyGitWrite) {
		t.Fatal("the reset should restore the default (off)")
	}
}

func TestSettingsWithoutAStateDirectoryAreUnsupported(t *testing.T) {
	handler := NewRouter(Options{Auth: NewLoopbackOrToken("")})

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil), loopback)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}

func TestGitWriteIsGatedByTheSetting(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "project")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-b", "main"},
		{"config", "user.email", "test@pi-ui.local"},
		{"config", "user.name", "pi-ui test"},
	} {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "readme.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := git.New(git.Config{FS: files})
	if err != nil {
		t.Fatal(err)
	}
	service := newSettingsService(t)
	handler := NewRouter(Options{FS: files, Git: repository, Settings: service, Auth: adminAuth{}})

	// Off by default: writing a repository is a deliberate decision.
	recorder := do(t, handler, jsonRequest(t, http.MethodPost, "/api/v1/git/stage",
		map[string]any{"dir": repo, "paths": []string{filepath.Join(repo, "readme.md")}}), loopback)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "feature_disabled") {
		t.Fatalf("expected feature_disabled, got %s", recorder.Body)
	}

	// Reading is not gated: status stays available.
	recorder = do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/git/status?dir="+repo, nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status: %d %s", recorder.Code, recorder.Body)
	}

	// An admin turns it on, and the mutation works.
	do(t, handler, adminRequest(http.MethodPatch, "/api/v1/settings", `{"git.write":true}`), loopback)
	recorder = do(t, handler, jsonRequest(t, http.MethodPost, "/api/v1/git/stage",
		map[string]any{"dir": repo, "paths": []string{filepath.Join(repo, "readme.md")}}), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stage after enabling: %d %s", recorder.Code, recorder.Body)
	}
}

func TestSettingsAreAdminOnly(t *testing.T) {
	service := newSettingsService(t)
	// The loopback authenticator grants operator: enough to read, not to change.
	handler := NewRouter(Options{Auth: NewLoopbackOrToken(""), Settings: service})

	recorder := do(t, handler, adminRequest(http.MethodPatch, "/api/v1/settings",
		`{"git.write":true}`), loopback)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "forbidden_scope") {
		t.Fatalf("expected forbidden_scope, got %s", recorder.Body)
	}
	if service.Bool(context.Background(), settings.KeyGitWrite) {
		t.Fatal("nothing may change for an operator")
	}
}
