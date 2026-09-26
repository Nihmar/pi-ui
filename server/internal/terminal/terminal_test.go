package terminal

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

func newManager(t *testing.T, maxSessions int) (*Manager, string) {
	t.Helper()
	if _, err := os.Stat(DefaultShell); err != nil {
		t.Skipf("%s is not available", DefaultShell)
	}
	root := t.TempDir()
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	manager, err := New(Config{FS: files, MaxSessions: maxSessions})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown() })
	return manager, root
}

// readUntil reads the terminal until the buffer contains want or the deadline passes.
func readUntil(t *testing.T, session *Session, want string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var output bytes.Buffer
	buffer := make([]byte, 4096)
	for time.Now().Before(deadline) {
		_ = session.file.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		read, err := session.Read(buffer)
		if read > 0 {
			output.Write(buffer[:read])
			if strings.Contains(output.String(), want) {
				return output.String()
			}
		}
		if err != nil && !errors.Is(err, os.ErrDeadlineExceeded) && read == 0 {
			break
		}
	}
	return output.String()
}

func TestOpenRunsAShellInTheWorkspace(t *testing.T) {
	manager, root := newManager(t, 2)

	session, err := manager.Open(context.Background(), root, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	if session.Dir != root {
		t.Fatalf("the shell should run in %q, not %q", root, session.Dir)
	}
	if !strings.HasPrefix(session.ID, "t_") {
		t.Fatalf("unexpected id %q", session.ID)
	}
	if _, err := session.Write([]byte("echo pi-ui-terminal\n")); err != nil {
		t.Fatal(err)
	}
	if output := readUntil(t, session, "pi-ui-terminal", 5*time.Second); !strings.Contains(output, "pi-ui-terminal") {
		t.Fatalf("the shell did not echo: %q", output)
	}
	if err := session.Resize(120, 40); err != nil {
		t.Fatalf("resize: %v", err)
	}
	if err := session.Resize(0, 0); err == nil {
		t.Fatal("a zero size must be refused")
	}
}

func TestTheShellSeesTheWorkspaceFile(t *testing.T) {
	manager, root := newManager(t, 2)
	if err := os.WriteFile(filepath.Join(root, "marker.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	session, err := manager.Open(context.Background(), root, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	if _, err := session.Write([]byte("ls marker.txt\n")); err != nil {
		t.Fatal(err)
	}
	if output := readUntil(t, session, "marker.txt", 5*time.Second); !strings.Contains(output, "marker.txt") {
		t.Fatalf("the shell cannot see its directory: %q", output)
	}
}

func TestCloseEndsTheTerminalAndItsChildren(t *testing.T) {
	manager, root := newManager(t, 2)

	session, err := manager.Open(context.Background(), root, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := session.Write([]byte("sleep 30 &\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)

	if err := session.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !session.Done() {
		t.Fatal("the session should report itself closed")
	}
	if err := session.Close(); err != nil {
		t.Fatalf("closing twice must be harmless: %v", err)
	}
	if _, err := session.Write([]byte("echo late\n")); err == nil {
		t.Fatal("writing to a closed terminal must fail")
	}
}

func TestOpenRefusesADirectoryOutsideTheWorkspaces(t *testing.T) {
	manager, _ := newManager(t, 2)

	_, err := manager.Open(context.Background(), "/etc", 80, 24)
	if code := sessions.CodeOf(err); code != "path_escape" {
		t.Fatalf("expected path_escape, got %q (%v)", code, err)
	}
	_, err = manager.Open(context.Background(), filepath.Join(t.TempDir(), "missing"), 80, 24)
	if code := sessions.CodeOf(err); code != "path_escape" {
		t.Fatalf("a directory outside the roots is an escape, got %q", code)
	}
}

func TestTheTerminalLimitIsEnforced(t *testing.T) {
	manager, root := newManager(t, 1)

	first, err := manager.Open(context.Background(), root, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	_, err = manager.Open(context.Background(), root, 80, 24)
	if code := sessions.CodeOf(err); code != sessions.CodeSessionLimit {
		t.Fatalf("expected session_limit, got %q (%v)", code, err)
	}
	if err := manager.Close(first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Open(context.Background(), root, 80, 24); err != nil {
		t.Fatalf("closing the first terminal should free the slot: %v", err)
	}
	if err := manager.Close("t_missing"); err == nil {
		t.Fatal("closing an unknown terminal must fail")
	}
}
