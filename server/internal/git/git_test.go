package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// newRepo builds a service over one temporary repository with a first commit.
func newRepo(t *testing.T) (*Service, string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	root := t.TempDir()
	repo := filepath.Join(root, "project")
	if err := os.Mkdir(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "init", "-b", "main")
	runGit(t, repo, "config", "user.email", "test@pi-ui.local")
	runGit(t, repo, "config", "user.name", "pi-ui test")
	if err := os.WriteFile(filepath.Join(repo, "readme.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, repo, "add", "readme.md")
	runGit(t, repo, "commit", "-m", "first commit")

	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{FS: files, Timeout: DefaultTimeout})
	if err != nil {
		t.Fatal(err)
	}
	return service, repo
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	command.Env = append(os.Environ(),
		"GIT_AUTHOR_DATE=2026-09-26T12:00:00Z",
		"GIT_COMMITTER_DATE=2026-09-26T12:00:00Z")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return string(out)
}

func TestStatusReportsABranchAndACleanTree(t *testing.T) {
	service, repo := newRepo(t)

	status, err := service.Status(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.Branch != "main" || status.Detached || !status.Clean {
		t.Fatalf("unexpected status %+v", status)
	}

	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	status, err = service.Status(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if status.Clean || len(status.Changes) != 1 {
		t.Fatalf("the untracked file should show: %+v", status.Changes)
	}
	if status.Changes[0].Status != "??" || status.Changes[0].Staged {
		t.Fatalf("unexpected change %+v", status.Changes[0])
	}
}

func TestLogDiffStageCommit(t *testing.T) {
	service, repo := newRepo(t)
	ctx := context.Background()

	commits, err := service.Log(ctx, repo, 5)
	if err != nil || len(commits) != 1 {
		t.Fatalf("log: %v %+v", err, commits)
	}
	if commits[0].Subject != "first commit" || commits[0].Short == "" {
		t.Fatalf("unexpected commit %+v", commits[0])
	}

	file := filepath.Join(repo, "readme.md")
	if err := os.WriteFile(file, []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	diff, err := service.Diff(ctx, repo, file, false)
	if err != nil || !strings.Contains(diff, "+world") {
		t.Fatalf("diff: %v %q", err, diff)
	}

	if err := service.Stage(ctx, repo, []string{file}); err != nil {
		t.Fatal(err)
	}
	staged, err := service.Diff(ctx, repo, file, true)
	if err != nil || !strings.Contains(staged, "+world") {
		t.Fatalf("staged diff: %v %q", err, staged)
	}
	commit, err := service.Commit(ctx, repo, "second commit")
	if err != nil {
		t.Fatal(err)
	}
	if commit.Subject != "second commit" {
		t.Fatalf("unexpected commit %+v", commit)
	}
	status, err := service.Status(ctx, repo)
	if err != nil || !status.Clean {
		t.Fatalf("the tree should be clean: %v %+v", err, status.Changes)
	}
}

func TestGitIsConfinedToTheWorkspaces(t *testing.T) {
	service, repo := newRepo(t)
	outside := filepath.Join(repo, "..", "..")

	_, err := service.Status(context.Background(), outside)
	if code := sessions.CodeOf(err); code != "path_escape" {
		t.Fatalf("expected path_escape, got %q (%v)", code, err)
	}
}

func TestGitFailuresCarryTheReason(t *testing.T) {
	service, repo := newRepo(t)
	// The root is inside the workspace but is not a repository itself: git finds a
	// parent only when one exists, which is exactly the failure under test.
	plain := filepath.Dir(repo)

	_, err := service.Status(context.Background(), plain)
	if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
		t.Fatalf("expected bad_request, got %q (%v)", code, err)
	}
	if err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("the message should explain itself: %v", err)
	}
	if _, err := service.Commit(context.Background(), repo, "   "); err == nil {
		t.Fatal("an empty message must be refused")
	}
	if err := service.Stage(context.Background(), repo, nil); err == nil {
		t.Fatal("staging nothing must be refused")
	}
}
