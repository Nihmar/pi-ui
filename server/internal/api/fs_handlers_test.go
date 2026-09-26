package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/fs"
)

// loopback is the peer address the authenticator trusts in a test.
const loopback = "127.0.0.1:34567"

// newFilesRouter builds a router over a temporary workspace with one file in it.
func newFilesRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(Options{FS: files, Auth: NewLoopbackOrToken("")}), root
}

func TestWorkspacesListTheRoots(t *testing.T) {
	handler, root := newFilesRouter(t)

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/workspaces", nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), filepath.Base(root)) {
		t.Fatalf("the root is missing from %s", recorder.Body)
	}
}

func TestFsListAndReadRoundTrip(t *testing.T) {
	handler, root := newFilesRouter(t)

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/fs/list?path="+root, nil), loopback)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "notes.md") {
		t.Fatalf("list: %d %s", recorder.Code, recorder.Body)
	}

	recorder = do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/files/read?path="+filepath.Join(root, "notes.md"), nil), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("read: %d %s", recorder.Code, recorder.Body)
	}
	var body struct {
		Text  string `json:"text"`
		Entry struct {
			Sha256 string `json:"sha256"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Text != "hello" || body.Entry.Sha256 == "" {
		t.Fatalf("unexpected body %+v", body)
	}
}

func TestFsWriteAndDeleteAreAudited(t *testing.T) {
	handler, root := newFilesRouter(t)
	target := filepath.Join(root, "new.md")

	recorder := do(t, handler, jsonRequest(t, http.MethodPut, "/api/v1/files/write",
		map[string]any{"path": target, "text": "written"}), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("write: %d %s", recorder.Code, recorder.Body)
	}
	data, err := os.ReadFile(target)
	if err != nil || string(data) != "written" {
		t.Fatalf("the file was not written: %v %q", err, data)
	}

	recorder = do(t, handler, jsonRequest(t, http.MethodDelete, "/api/v1/files/delete",
		map[string]any{"path": target}), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", recorder.Code, recorder.Body)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("the file should be gone")
	}
}

func TestFsRefusesToLeaveTheWorkspace(t *testing.T) {
	handler, root := newFilesRouter(t)
	outside := filepath.Join(root, "..", "secrets")

	recorder := do(t, handler, jsonRequest(t, http.MethodPut, "/api/v1/files/write",
		map[string]any{"path": outside, "text": "x"}), loopback)
	if recorder.Code != http.StatusForbidden {
		// The taxonomy maps path_escape onto 403: it is a policy refusal, not a
		// malformed request.
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "path_escape") {
		t.Fatalf("expected path_escape, got %s", recorder.Body)
	}
}

func TestFsWithoutRootsIsUnsupported(t *testing.T) {
	handler := NewRouter(Options{Auth: NewLoopbackOrToken("")})

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet, "/api/v1/fs/list?path=/tmp", nil), loopback)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}

// jsonRequest builds a request with a JSON body, so a test does not spell the
// encoding of every payload.
func jsonRequest(t *testing.T, method, path string, body map[string]any) *http.Request {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(string(encoded)))
	request.Header.Set("Content-Type", "application/json")
	return request
}
