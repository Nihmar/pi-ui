package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// newDrainRouter builds a router with a supervisor that starts fake children.
func newDrainRouter(t *testing.T) http.Handler {
	t.Helper()
	supervisor := &fakeSupervisor{}
	return NewRouter(Options{
		Supervisor: supervisor,
		Auth:       adminAuth{},
		Info:       ServerInfo{Version: "test", Limits: map[string]any{"maxSessions": 8}},
	})
}

func TestDrainRefusesNewSessionsAndResumes(t *testing.T) {
	handler := newDrainRouter(t)

	recorder := do(t, handler, httptest.NewRequest(http.MethodPost, "/api/v1/drain/start", nil), loopback)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}

	// A create is refused with the code a client branches on, and nothing is spawned.
	recorder = do(t, handler, adminRequest(http.MethodPost, "/api/v1/sessions",
		`{"cwd":"`+filepath.Join(t.TempDir())+`"}`), loopback)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("create while draining: %d %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "unavailable") {
		t.Fatalf("expected unavailable, got %s", recorder.Body)
	}

	// The state is visible where a client looks for capabilities.
	if limits := limitsOf(t, handler); limits["draining"] != true {
		t.Fatalf("limits = %v", limits)
	}

	recorder = do(t, handler, httptest.NewRequest(http.MethodPost, "/api/v1/drain/resume", nil), loopback)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("resume: %d %s", recorder.Code, recorder.Body)
	}
	if limits := limitsOf(t, handler); limits["draining"] != nil {
		t.Fatalf("a resumed server must not report draining: %v", limits)
	}
}

// limitsOf reads GET /server and returns its limits map. It decodes into a fresh map
// every time: unmarshalling into a reused one would keep a key the server no longer
// sends.
func limitsOf(t *testing.T, handler http.Handler) map[string]any {
	t.Helper()
	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/server", nil), loopback)
	var body struct {
		Limits map[string]any `json:"limits"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Limits
}

func TestDrainIsAdminOnly(t *testing.T) {
	supervisor := &fakeSupervisor{}
	handler := NewRouter(Options{Supervisor: supervisor, Auth: NewLoopbackOrToken("")})

	recorder := do(t, handler, httptest.NewRequest(http.MethodPost, "/api/v1/drain/start", nil), loopback)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}

func TestDrainingDoesNotTouchRunningSessions(t *testing.T) {
	supervisor := &fakeSupervisor{}
	handler := NewRouter(Options{Supervisor: supervisor, Auth: adminAuth{}})

	// A session started before the drain keeps running: the drain closes the front
	// door, it does not evict anybody.
	if _, err := supervisor.Start(context.Background(), sessions.Spec{CWD: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	do(t, handler, httptest.NewRequest(http.MethodPost, "/api/v1/drain/start", nil), loopback)

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil), loopback)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "s_0123456789abcdef") {
		t.Fatalf("list while draining: %d %s", recorder.Code, recorder.Body)
	}
}
