package search

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

func newService(t *testing.T, sessionDirs ...string) (*Service, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.go"),
		[]byte("package main\n\nfunc helloWorld() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"),
		[]byte("nothing to see here\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{FS: files, SessionDirs: sessionDirs})
	if err != nil {
		t.Fatal(err)
	}
	return service, root
}

func hasRipgrep(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rg"); err != nil {
		t.Skip("ripgrep is not installed")
	}
}

func TestFileSearchFindsAndLocates(t *testing.T) {
	hasRipgrep(t)
	service, root := newService(t)

	hits, err := service.Search(context.Background(), Query{Text: "helloWorld", Dir: root})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("expected one hit, got %+v", hits)
	}
	hit := hits[0]
	if hit.Kind != "file" || hit.Rel != filepath.Join("src", "main.go") {
		t.Fatalf("unexpected hit %+v", hit)
	}
	if hit.Line != 3 || hit.RootID != filepath.Base(root) {
		t.Fatalf("unexpected location %+v", hit)
	}
	if hit.Path != filepath.Join(root, "src", "main.go") {
		t.Fatalf("the hit should be the absolute path: %+v", hit)
	}
}

func TestFileSearchIsCaseInsensitiveByDefault(t *testing.T) {
	hasRipgrep(t)
	service, root := newService(t)

	hits, err := service.Search(context.Background(), Query{Text: "HELLOWORLD", Dir: root})
	if err != nil || len(hits) != 1 {
		t.Fatalf("case-insensitive: %v %+v", err, hits)
	}
	hits, err = service.Search(context.Background(), Query{
		Text: "HELLOWORLD", Dir: root, CaseSensitive: true,
	})
	if err != nil || len(hits) != 0 {
		t.Fatalf("case-sensitive should not match: %v %+v", err, hits)
	}
}

func TestFileSearchRefusesAnOutsideDirectory(t *testing.T) {
	service, _ := newService(t)

	_, err := service.Search(context.Background(), Query{Text: "root", Dir: "/etc"})
	if code := sessions.CodeOf(err); code != "path_escape" {
		t.Fatalf("expected path_escape, got %q (%v)", code, err)
	}
}

func TestShortQueriesAreRefused(t *testing.T) {
	service, root := newService(t)

	if _, err := service.Search(context.Background(), Query{Text: "a", Dir: root}); err == nil {
		t.Fatal("a one-character query is too broad to be useful")
	} else if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
		t.Fatalf("code %q", code)
	}
}

func TestMessageSearchScansSessionFiles(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "session.jsonl")
	lines := `{"type":"header","sessionId":"01a0dccc"}
{"type":"message","timestamp":"2026-09-26T12:00:00.000Z","message":{"role":"user","content":"please fix the RpcBridge framing"}}
{"type":"message","timestamp":"2026-09-26T12:01:00.000Z","message":{"role":"assistant","content":[{"type":"text","text":"I will look at RpcBridge"},{"type":"thinking","thinking":"RpcBridge"}]}}
{"type":"message","message":{"role":"system","content":"RpcBridge"}}
`
	if err := os.WriteFile(file, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	service, root := newService(t, dir)

	hits, err := service.Search(context.Background(), Query{
		Text: "RpcBridge", Scope: []string{"messages"}, Dir: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("the user and the assistant message should match, not the system one: %+v", hits)
	}
	if hits[0].Kind != "message" || hits[0].Role != "user" || hits[0].SessionID != "01a0dccc" {
		t.Fatalf("unexpected hit %+v", hits[0])
	}
	if hits[1].Role != "assistant" || hits[1].Text == "" {
		t.Fatalf("unexpected hit %+v", hits[1])
	}
}

func TestScopeSelectsTheHalves(t *testing.T) {
	hasRipgrep(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"),
		[]byte(`{"type":"message","message":{"role":"user","content":"helloWorld"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	service, root := newService(t, dir)

	files, err := service.Search(context.Background(), Query{
		Text: "helloWorld", Scope: []string{"files"}, Dir: root,
	})
	if err != nil || len(files) != 1 || files[0].Kind != "file" {
		t.Fatalf("files scope: %v %+v", err, files)
	}
	messages, err := service.Search(context.Background(), Query{
		Text: "helloWorld", Scope: []string{"messages"}, Dir: root,
	})
	if err != nil || len(messages) != 1 || messages[0].Kind != "message" {
		t.Fatalf("messages scope: %v %+v", err, messages)
	}
}

func TestNoMatchIsNotAFailure(t *testing.T) {
	hasRipgrep(t)
	service, root := newService(t)

	hits, err := service.Search(context.Background(), Query{Text: "zzz-not-here", Dir: root})
	if err != nil {
		t.Fatalf("no match must not be an error: %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("unexpected hits %+v", hits)
	}
}
