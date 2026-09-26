package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/mcp"
)

// newMcpRouter builds a router over a temporary MCP configuration file.
func newMcpRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp.json")
	return NewRouter(Options{MCP: mcp.New(path), Auth: adminAuth{}}), path
}

func TestMcpRoundTripRedactsSecrets(t *testing.T) {
	handler, path := newMcpRouter(t)

	recorder := do(t, handler, adminRequest(http.MethodPut, "/api/v1/mcp",
		`{"servers":{"files":{"command":"npx","args":["-y","server-filesystem","/srv"],"env":{"TOKEN":"s3cret"}}}}`), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if strings.Contains(recorder.Body.String(), "s3cret") {
		t.Fatalf("the answer leaked a secret: %s", recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), mcp.Redacted) {
		t.Fatalf("the value should be redacted, not absent: %s", recorder.Body)
	}

	// The file itself holds the real value.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "s3cret") {
		t.Fatalf("file = %s", raw)
	}

	// Reading back gives the same redacted view, and disabling a server is a round trip.
	recorder = do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/mcp", nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read: %d %s", recorder.Code, recorder.Body)
	}
	var view struct {
		Path    string                    `json:"path"`
		Servers map[string]map[string]any `json:"servers"`
		Enabled []string                  `json:"enabled"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Path != path || len(view.Enabled) != 1 || view.Enabled[0] != "files" {
		t.Fatalf("view = %+v", view)
	}
}

func TestMcpRejectsABadEntry(t *testing.T) {
	handler, _ := newMcpRouter(t)

	for _, body := range []string{
		`{"servers":{"x":{}}}`,
		`{"servers":{"x":{"command":"npx","url":"https://x.example"}}}`,
		`{"servers":{"x":{"url":"ftp://x.example"}}}`,
		`{"servers":{"x":{"command":"npx","env":{"A":"***"}}}}`,
	} {
		recorder := do(t, handler, adminRequest(http.MethodPut, "/api/v1/mcp", body), loopback)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("%s: status %d (%s)", body, recorder.Code, recorder.Body)
		}
	}
}

func TestMcpIsAdminOnlyAndNeedsAFile(t *testing.T) {
	// The loopback authenticator grants operator: not enough for MCP.
	path := filepath.Join(t.TempDir(), "mcp.json")
	handler := NewRouter(Options{MCP: mcp.New(path), Auth: NewLoopbackOrToken("")})
	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/mcp", nil), loopback)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}

	// A server without a configuration file says so instead of inventing one.
	handler = NewRouter(Options{Auth: adminAuth{}})
	recorder = do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/mcp", nil), loopback)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}
