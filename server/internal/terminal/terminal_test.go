package terminal

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// recordingSink is the client side of a terminal: the pump owns the PTY reads, so a
// test asserts on what the sink received instead of reading the same file twice.
type recordingSink struct {
	mu       sync.Mutex
	output   bytes.Buffer
	closed   []closeReport
	closedCh chan closeReport
}

type closeReport struct {
	id       string
	exitCode int
	reason   string
}

func newRecordingSink() *recordingSink {
	return &recordingSink{closedCh: make(chan closeReport, 4)}
}

func (s *recordingSink) Output(_ string, data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.output.Write(data)
}

func (s *recordingSink) Closed(id string, exitCode int, reason string) {
	report := closeReport{id: id, exitCode: exitCode, reason: reason}
	s.mu.Lock()
	s.closed = append(s.closed, report)
	s.mu.Unlock()
	s.closedCh <- report
}

func (s *recordingSink) text() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.output.String()
}

// waitForOutput waits until the sink saw want, and returns everything it saw.
func (s *recordingSink) waitForOutput(want string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if text := s.text(); strings.Contains(text, want) {
			return text
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s.text()
}

// waitForClose waits for the close notification of one terminal.
func (s *recordingSink) waitForClose(timeout time.Duration) (closeReport, bool) {
	select {
	case report := <-s.closedCh:
		return report, true
	case <-time.After(timeout):
		return closeReport{}, false
	}
}

func newManager(t *testing.T, maxSessions int) (*Manager, string, *recordingSink) {
	t.Helper()
	if _, err := os.Stat(DefaultShell); err != nil {
		t.Skipf("%s is not available", DefaultShell)
	}
	root := t.TempDir()
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	sink := newRecordingSink()
	manager, err := New(Config{FS: files, MaxSessions: maxSessions, Sink: sink})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Shutdown() })
	return manager, root, sink
}

func TestOpenRunsAShellInTheWorkspace(t *testing.T) {
	manager, root, sink := newManager(t, 2)

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
	if session.PID == 0 || session.Cols != 80 || session.Rows != 24 {
		t.Fatalf("unexpected session %+v", session)
	}
	if _, err := session.Write([]byte("echo pi-ui-terminal\n")); err != nil {
		t.Fatal(err)
	}
	if output := sink.waitForOutput("pi-ui-terminal", 5*time.Second); !strings.Contains(output, "pi-ui-terminal") {
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
	manager, root, sink := newManager(t, 2)
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
	if output := sink.waitForOutput("marker.txt", 5*time.Second); !strings.Contains(output, "marker.txt") {
		t.Fatalf("the shell cannot see its directory: %q", output)
	}
}

func TestCloseEndsTheTerminalAndItsChildren(t *testing.T) {
	manager, root, sink := newManager(t, 2)

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
	report, ok := sink.waitForClose(5 * time.Second)
	if !ok || report.reason != ReasonClient {
		t.Fatalf("unexpected close report %+v (%v)", report, ok)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("closing twice must be harmless: %v", err)
	}
	if _, err := session.Write([]byte("echo late\n")); err == nil {
		t.Fatal("writing to a closed terminal must fail")
	}
}

func TestOpenRefusesADirectoryOutsideTheWorkspaces(t *testing.T) {
	manager, _, _ := newManager(t, 2)

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
	manager, root, _ := newManager(t, 1)

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

// TestTheOwnerReasonIsReported covers the close a dead connection causes: the
// notification says who asked, so a client can tell "I closed it" from "the shell
// ended" without guessing.
func TestTheOwnerReasonIsReported(t *testing.T) {
	manager, root, sink := newManager(t, 2)

	session, err := manager.Open(context.Background(), root, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if session.Cols != DefaultCols || session.Rows != DefaultRows {
		t.Fatalf("an omitted size should default: %+v", session)
	}
	if err := manager.CloseWithReason(session.ID, ReasonOwner); err != nil {
		t.Fatal(err)
	}
	report, ok := sink.waitForClose(5 * time.Second)
	if !ok || report.reason != ReasonOwner {
		t.Fatalf("unexpected close report %+v (%v)", report, ok)
	}
	if _, err := manager.Get(session.ID); err {
		t.Fatal("a closed terminal must leave the registry")
	}
}

// TestShellExitIsReportedWithoutAClose covers the other direction: the shell ends on
// its own, and the client is told with its exit status.
func TestShellExitIsReportedWithoutAClose(t *testing.T) {
	root := t.TempDir()
	files, err := fs.New(fs.Config{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	sink := newRecordingSink()
	manager, err := New(Config{FS: files, Sink: sink, Command: func(string) *exec.Cmd {
		return exec.Command(DefaultShell, "-c", "echo bye; exit 7")
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = manager.Shutdown() }()

	session, err := manager.Open(context.Background(), root, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if output := sink.waitForOutput("bye", 5*time.Second); !strings.Contains(output, "bye") {
		t.Fatalf("the output should arrive before the close: %q", output)
	}
	report, ok := sink.waitForClose(5 * time.Second)
	if !ok {
		t.Fatal("the close notification is missing")
	}
	if report.reason != ReasonExit || report.exitCode != 7 {
		t.Fatalf("unexpected close report %+v", report)
	}
	if code := session.ExitCode(); code != 7 {
		t.Fatalf("exit code %d, want 7", code)
	}
	if err := session.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
}

// lockedBuffer is a writer a test can read while another goroutine writes it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// TestCopyToDeliversTheOutput covers the sink-less path: a plain reader still sees
// the shell, which is what a future `pi-ui attach` uses.
func TestCopyToDeliversTheOutput(t *testing.T) {
	manager, root, _ := newManager(t, 2)

	session, err := manager.Open(context.Background(), root, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()

	collected := &lockedBuffer{}
	done := make(chan struct{})
	go func() {
		_ = session.CopyTo(collected)
		close(done)
	}()
	if _, err := session.Write([]byte("echo copied\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !strings.Contains(collected.String(), "copied") {
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(collected.String(), "copied") {
		t.Fatalf("CopyTo saw %q", collected.String())
	}
	_ = session.Close()
	<-done
}

// TestDiscardSinkIsUsable keeps the no-transport sink honest.
func TestDiscardSinkIsUsable(t *testing.T) {
	Discard{}.Output("t_1", []byte("x"))
	Discard{}.Closed("t_1", 0, ReasonExit)
}
