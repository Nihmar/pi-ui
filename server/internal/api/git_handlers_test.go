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
	"github.com/Nihmar/pi-ui/server/internal/git"
)

// newGitRouter builds a router over a temporary repository, or skips when git is
// not installed.
func newGitRouter(t *testing.T) (http.Handler, string) {
	t.Helper()
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
	for _, args := range [][]string{{"add", "readme.md"}, {"commit", "-m", "first"}} {
		command := exec.Command("git", append([]string{"-C", repo}, args...)...)
		if out, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	repository, err := git.New(git.Config{FS: files})
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(Options{
		FS:   files,
		Git:  repository,
		Auth: NewLoopbackOrToken(""),
	}), repo
}

func TestGitStatusLogAndDiff(t *testing.T) {
	handler, repo := newGitRouter(t)

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/git/status?dir="+repo, nil), loopback)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "main") {
		t.Fatalf("status: %d %s", recorder.Code, recorder.Body)
	}

	recorder = do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/git/log?dir="+repo+"&limit=5", nil), loopback)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "first") {
		t.Fatalf("log: %d %s", recorder.Code, recorder.Body)
	}

	if err := os.WriteFile(filepath.Join(repo, "readme.md"), []byte("hello\nthere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recorder = do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/git/diff?dir="+repo, nil), loopback)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "+there") {
		t.Fatalf("diff: %d %s", recorder.Code, recorder.Body)
	}
}

func TestGitStageAndCommit(t *testing.T) {
	handler, repo := newGitRouter(t)
	target := filepath.Join(repo, "readme.md")
	if err := os.WriteFile(target, []byte("hello\nagain\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	recorder := do(t, handler, jsonRequest(t, http.MethodPost, "/api/v1/git/stage",
		map[string]any{"dir": repo, "paths": []string{target}}), loopback)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stage: %d %s", recorder.Code, recorder.Body)
	}

	recorder = do(t, handler, jsonRequest(t, http.MethodPost, "/api/v1/git/commit",
		map[string]any{"dir": repo, "message": "second"}), loopback)
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "second") {
		t.Fatalf("commit: %d %s", recorder.Code, recorder.Body)
	}
}

func TestGitRefusesADirectoryOutsideTheWorkspace(t *testing.T) {
	handler, _ := newGitRouter(t)

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/git/status?dir=/etc", nil), loopback)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
	if !strings.Contains(recorder.Body.String(), "path_escape") {
		t.Fatalf("expected path_escape, got %s", recorder.Body)
	}
}

func TestGitWithoutWorkspaceIsUnsupported(t *testing.T) {
	handler := NewRouter(Options{Auth: NewLoopbackOrToken("")})

	recorder := do(t, handler, httptest.NewRequest(http.MethodGet,
		"/api/v1/git/status?dir=/tmp", nil), loopback)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status %d: %s", recorder.Code, recorder.Body)
	}
}
