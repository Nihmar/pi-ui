package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/search"
)

// newSearchRouter builds a router over a temporary workspace with one matching file.
func newSearchRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep is not installed")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "main.go"),
		[]byte("package main\nfunc helloWorld() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := search.New(search.Config{FS: files})
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(Options{
		FS:     files,
		Search: service,
		Auth:   NewLoopbackOrToken(""),
	}), root
}

func TestSearchFindsAFile(t *testing.T) {
	handler, root := newSearchRouter(t)

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/search?q=helloWorld&cwd="+root, nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	body := recorder.Body.String()
	if !strings.Contains(body, `"kind":"file"`) || !strings.Contains(body, "main.go") {
		t.Fatalf("unexpected body %s", body)
	}
}

func TestSearchRefusesAShortQuery(t *testing.T) {
	handler, root := newSearchRouter(t)

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/search?q=a&cwd="+root, nil), loopback)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}

func TestSearchRefusesAnOutsideDirectory(t *testing.T) {
	handler, _ := newSearchRouter(t)

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/search?q=root&cwd=/etc", nil), loopback)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}

func TestSearchWithoutWorkspaceIsUnsupported(t *testing.T) {
	handler := NewRouter(Options{Auth: NewLoopbackOrToken("")})

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/search?q=root&cwd=/tmp", nil), loopback)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}
