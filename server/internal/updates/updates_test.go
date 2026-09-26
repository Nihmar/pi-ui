package updates

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// tableChecker answers from a map and fails on demand.
type tableChecker struct {
	latest map[string]string
	errs   map[string]error
}

func (c tableChecker) Latest(_ context.Context, component string) (string, error) {
	if err := c.errs[component]; err != nil {
		return "", err
	}
	return c.latest[component], nil
}

func TestCheckReportsOneRowPerComponent(t *testing.T) {
	service := New(Config{
		Components: []Component{
			{Name: ComponentServer, Current: "0.0.1-spike", Source: "build stamp"},
			{Name: ComponentPi, Current: "0.87.1"},
			{Name: ComponentBridge, Current: "0.1.0"},
		},
		Checker: tableChecker{
			latest: map[string]string{ComponentPi: "0.90.0", ComponentBridge: "0.1.0"},
			errs:   map[string]error{ComponentBridge: context.DeadlineExceeded},
		},
	})

	reports := service.Check(context.Background())
	if len(reports) != 3 {
		t.Fatalf("reports = %+v", reports)
	}
	byName := map[string]Report{}
	for _, report := range reports {
		byName[report.Name] = report
	}
	if byName[ComponentPi].Latest != "0.90.0" || !byName[ComponentPi].UpdateAvailable {
		t.Fatalf("pi row = %+v", byName[ComponentPi])
	}
	// The bridge row carries the failure instead of pretending it is current.
	if byName[ComponentBridge].Error == "" || byName[ComponentBridge].UpdateAvailable {
		t.Fatalf("bridge row = %+v", byName[ComponentBridge])
	}
	// A component whose version is not comparable is never reported as outdated.
	if byName[ComponentServer].Error == "" || byName[ComponentServer].Latest != "" {
		t.Fatalf("server row = %+v", byName[ComponentServer])
	}
	for _, report := range reports {
		if report.CheckedAt == "" {
			t.Fatalf("%s has no timestamp", report.Name)
		}
	}
}

func TestNewerOrdersNumericVersions(t *testing.T) {
	cases := []struct {
		candidate string
		current   string
		want      bool
	}{
		{"1.2.4", "1.2.3", true},
		{"v1.3.0", "1.2.9", true},
		{"1.2.3", "1.2.3", false},
		{"1.2.3", "1.3.0", false},
		{"2.0", "1.9.9", true},
		{"1.2.3-rc1", "1.2.2", false},
		{"latest", "1.2.2", false},
		{"", "1.2.2", false},
	}
	for _, testCase := range cases {
		if got := Newer(testCase.candidate, testCase.current); got != testCase.want {
			t.Fatalf("Newer(%q, %q) = %v, want %v", testCase.candidate, testCase.current, got, testCase.want)
		}
	}
}

func TestHTTPCheckerReadsTheDistTag(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/@earendil-works/pi-coding-agent" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"dist-tags": map[string]string{"latest": "0.91.0", "beta": "0.92.0-beta.1"},
		})
	}))
	defer server.Close()

	checker := &HTTPChecker{
		Registry: server.URL,
		Packages: map[string]string{ComponentPi: "@earendil-works/pi-coding-agent"},
		Client:   server.Client(),
	}
	latest, err := checker.Latest(context.Background(), ComponentPi)
	if err != nil {
		t.Fatal(err)
	}
	if latest != "0.91.0" {
		t.Fatalf("latest = %q", latest)
	}

	if _, err := checker.Latest(context.Background(), "nope"); err == nil {
		t.Fatal("an unknown component must fail")
	}
}

func TestHTTPCheckerReportsARegistryFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	checker := &HTTPChecker{
		Registry: server.URL,
		Packages: map[string]string{ComponentPi: "pi"},
		Client:   server.Client(),
	}
	_, err := checker.Latest(context.Background(), ComponentPi)
	if err == nil {
		t.Fatal("a 503 must be an error, not an empty version")
	}
	if coded := CodedFailure(err); coded == nil {
		t.Fatal("a failure must map onto the taxonomy")
	}
}

func TestACheckerlessDeploymentSaysSo(t *testing.T) {
	service := New(Config{Components: []Component{{Name: ComponentServer, Current: "1.0.0"}}})

	reports := service.Check(context.Background())
	if reports[0].Error == "" || reports[0].UpdateAvailable {
		t.Fatalf("report = %+v", reports[0])
	}
}

func TestCheckerErrorKeepsTheReason(t *testing.T) {
	reason := errors.New("connection refused")
	wrapped := CheckerError{Component: ComponentPi, Reason: reason}
	if !errors.Is(wrapped, reason) {
		t.Fatal("the reason must be unwrappable")
	}
	if wrapped.Error() == "" {
		t.Fatal("an error explains itself")
	}
}
