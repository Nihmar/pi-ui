package adversarial_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// lockedBuffer is a bytes.Buffer that is safe to read while a child process
// writes to it, so a failure message can carry the server's own logs.
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

// processesWithArg counts the live processes whose command line mentions needle.
// It is the orphan check: every fake-pi of this test carries a unique binary
// path, so a hit after the server exited is a leaked child.
func processesWithArg(t *testing.T, needle string) []int {
	t.Helper()

	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skipf("no /proc on this host: %v", err)
	}
	var pids []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		data, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "cmdline"))
		if err != nil {
			continue
		}
		if strings.Contains(strings.ReplaceAll(string(data), "\x00", " "), needle) {
			pids = append(pids, pid)
		}
	}
	return pids
}

// TestLifecycle_SIGTERMReapsAllChildrenWithinTwoSeconds is acceptance criterion
// C8 against the real binary: two sessions are running, SIGTERM is delivered, the
// server exits cleanly within the budget and both `pi` children are gone with
// zero orphans left behind.
func TestLifecycle_SIGTERMReapsAllChildrenWithinTwoSeconds(t *testing.T) {
	fakePi := fakeharness.Build(t)
	scriptPath := fakeharness.WriteScript(t, fakeharness.Script{})
	pidDir := t.TempDir()
	wrapper := writePiWrapper(t, pidDir, fakePi, scriptPath)

	first, second := t.TempDir(), t.TempDir()
	cmd := exec.Command(serverBinary(t), "serve",
		"--addr", "127.0.0.1:0",
		"--pi", wrapper,
		"--state-dir", t.TempDir(),
		"--session", first+":one",
		"--session", second+":two",
	)
	var stdout, stderr lockedBuffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start server binary: %v", err)
	}
	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}
	})

	// Two sessions means at least two children; a child that exited early is respawned by
	// the supervisor (Phase 3), so the directory legitimately holds more pid files than
	// sessions. What this test is about is the shutdown: every child that ever ran is gone
	// within the budget.
	pids := waitPIDs(t, pidDir, 2)

	sent := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server exit after SIGTERM = %v (want a clean 0)\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("server did not exit within 2s of SIGTERM\nstderr:\n%s", stderr.String())
	}

	// The final list, not the snapshot: a respawn between the snapshot and the signal is
	// still a child the server owns and must reap.
	everyone := readPIDs(t, pidDir)
	reaped := waitPIDsGone(time.Now().Add(2*time.Second), everyone...)
	if reaped < 0 {
		t.Fatalf("children %v were still alive 2s after SIGTERM\nstderr:\n%s", everyone, stderr.String())
	}
	if len(everyone) < len(pids) {
		t.Fatalf("pid files disappeared from %s: %v → %v", pidDir, pids, everyone)
	}
	if elapsed := time.Since(sent); elapsed > 2*time.Second {
		t.Fatalf("children were reaped after %s, want ≤ 2s", elapsed)
	}
	if orphans := processesWithArg(t, fakePi); len(orphans) > 0 {
		t.Fatalf("orphan fake-pi processes after shutdown: %v", orphans)
	}
}

// TestLifecycle_CrashAndCleanExitClassified runs one child that dies with exit
// code 9 and one that exits 0. The crash must be classified as server.crashed
// with its exit code, the orderly exit as server.exited, and both sessions must
// stay listed with their final status.
func TestLifecycle_CrashAndCleanExitClassified(t *testing.T) {
	stack := newStack(t, nil)

	// The fault is armed by a command, not by a wall-clock timer: the child answers
	// session.abort and then exits with the scripted status. A delay would race the
	// get_state round trip that Start performs on a loaded machine.
	crashCode := 9
	crashScript := fakeharness.WriteScript(t, fakeharness.Script{Commands: map[string]fakeharness.CommandScript{
		"abort": {ExitCode: &crashCode},
	}})
	orderlyCode := 0
	orderlyScript := fakeharness.WriteScript(t, fakeharness.Script{Commands: map[string]fakeharness.CommandScript{
		"abort": {ExitCode: &orderlyCode},
	}})
	crashing := stack.startSession(t, t.TempDir(), "--script", crashScript)
	orderly := stack.startSession(t, t.TempDir(), "--script", orderlyScript)

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + crashing.ID + `"}`)
	client.send(`{"type":"subscribe","sessionId":"` + orderly.ID + `"}`)
	client.send(`{"type":"command","id":"c1","sessionId":"` + crashing.ID + `","op":"session.abort"}`)
	client.send(`{"type":"command","id":"c2","sessionId":"` + orderly.ID + `","op":"session.abort"}`)

	// Both children answer in their own goroutine, so the two terminal events can arrive
	// in either order: collect them by type instead of by sequence.
	var crashed, exited map[string]json.RawMessage
	eventsDeadline := time.Now().Add(waitTimeout)
	for (crashed == nil || exited == nil) && time.Now().Before(eventsDeadline) {
		frame, err := client.next(time.Until(eventsDeadline))
		if err != nil {
			t.Fatalf("waiting for the terminal events: %v (frames read: %v)", err, client.frames)
		}
		switch frameType(frame) {
		case "server.crashed":
			crashed = frame
		case "server.exited":
			exited = frame
		}
	}
	if crashed == nil || exited == nil {
		t.Fatalf("terminal events missing (frames read: %v)", client.frames)
	}

	if got := fieldString(crashed, "sessionId"); got != crashing.ID {
		t.Fatalf("server.crashed sessionId = %q, want %s", got, crashing.ID)
	}
	if got := fieldInt(t, payloadOf(t, crashed), "exitCode"); got != 9 {
		t.Fatalf("server.crashed exitCode = %d, want 9", got)
	}
	if got := fieldString(exited, "sessionId"); got != orderly.ID {
		t.Fatalf("server.exited sessionId = %q, want %s", got, orderly.ID)
	}
	if got := fieldInt(t, payloadOf(t, exited), "exitCode"); got != 0 {
		t.Fatalf("server.exited exitCode = %d, want 0", got)
	}

	// Both sessions stay listed with their final status and exit code.
	statusDeadline := time.Now().Add(waitTimeout)
	for time.Now().Before(statusDeadline) {
		crashInfo, crashOK := stack.mgr.Get(crashing.ID)
		exitInfo, exitOK := stack.mgr.Get(orderly.ID)
		if crashOK && crashInfo.Status == sessions.StatusCrashed && crashInfo.ExitCode != nil && *crashInfo.ExitCode == 9 &&
			exitOK && exitInfo.Status == sessions.StatusExited && exitInfo.ExitCode != nil && *exitInfo.ExitCode == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	crashInfo, _ := stack.mgr.Get(crashing.ID)
	if crashInfo.Status != sessions.StatusCrashed {
		t.Errorf("crashed session status = %q, want crashed", crashInfo.Status)
	}
	if crashInfo.ExitCode == nil || *crashInfo.ExitCode != 9 {
		t.Errorf("crashed session exitCode = %v, want 9", crashInfo.ExitCode)
	}
	exitInfo, _ := stack.mgr.Get(orderly.ID)
	if exitInfo.Status != sessions.StatusExited {
		t.Errorf("orderly session status = %q, want exited", exitInfo.Status)
	}
	if exitInfo.ExitCode == nil || *exitInfo.ExitCode != 0 {
		t.Errorf("orderly session exitCode = %v, want 0", exitInfo.ExitCode)
	}

	resp, data := stack.getJSON("/api/v1/sessions")
	wantStatus(t, resp, data, 200)
	var listed struct {
		Sessions []sessions.Info `json:"sessions"`
	}
	decodeJSON(t, data, &listed)
	if len(listed.Sessions) != 2 {
		t.Fatalf("sessions = %d, want both finished sessions still listed", len(listed.Sessions))
	}
}

// TestLifecycle_ShutdownLeavesNoStoppingSession drives the supervisor's own
// Shutdown and reads every session through REST the instant it returns. A session
// whose child has already been reaped must already carry its terminal status
// (finding 6.1: Shutdown used to return while the pump had not yet written it, so
// a client could read "stopping" for a dead child; fixed in `ac88717`).
func TestLifecycle_ShutdownLeavesNoStoppingSession(t *testing.T) {
	stack := newStack(t, nil)
	ids := make([]string, 0, 3)
	for i := 0; i < 3; i++ {
		ids = append(ids, stack.startSession(t, t.TempDir()).ID)
	}

	if err := stack.mgr.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	// The instant Shutdown returns, every session a client can read is terminal:
	// never "stopping" for a child that has already been reaped.
	for _, id := range ids {
		resp, data := stack.getJSON("/api/v1/sessions/" + id)
		wantStatus(t, resp, data, 200)
		var info sessions.Info
		decodeJSON(t, data, &info)
		if info.Status != sessions.StatusExited {
			t.Errorf("session %s status = %q the instant Shutdown returned, want %q", id, info.Status, sessions.StatusExited)
		}
		if info.ExitCode == nil {
			t.Errorf("session %s has no exit code the instant Shutdown returned", id)
		}
	}
}
