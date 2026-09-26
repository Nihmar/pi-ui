package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

func TestStartMissingBinaryFails(t *testing.T) {
	bridge := New(Spec{Command: []string{"pi-ui-no-such-binary", "--mode", "rpc"}}, Options{})
	err := bridge.Start(context.Background())
	if err == nil || !strings.Contains(err.Error(), "pi-ui-no-such-binary") {
		t.Fatalf("Start with a missing binary = %v, want an error naming it", err)
	}
	if pid := bridge.PID(); pid != 0 {
		t.Errorf("PID() = %d after a failed start, want 0", pid)
	}
	// The failed spawn leaves the bridge idle: a retry reports the same resolution error
	// instead of "already started".
	if retry := bridge.Start(context.Background()); retry == nil || retry.Error() != err.Error() {
		t.Errorf("second Start = %v, want %v", retry, err)
	}
	if err := bridge.Wait(); !errors.Is(err, errNotStarted) {
		t.Errorf("Wait on an unstarted bridge = %v, want %v", err, errNotStarted)
	}
	if err := bridge.Close(); err != nil {
		t.Errorf("Close on an unstarted bridge = %v, want nil", err)
	}
}

func TestStartWithoutCommandFails(t *testing.T) {
	if err := New(Spec{}, Options{}).Start(context.Background()); err == nil {
		t.Fatal("Start without a command succeeded")
	}
}

func TestStartRejectsSecondStart(t *testing.T) {
	bridge := startBridge(t, Options{RecordBuffer: 4, KillGrace: 300 * time.Millisecond},
		fakePi(t, fakeharness.Script{}, "--ignore-stdin"), nil)
	if err := bridge.Start(context.Background()); err == nil {
		t.Fatal("second Start succeeded, want an error")
	}
}

// TestStartForwardsStderr covers the diagnostics hook: stderr is line-framed, copied and
// never mixed into the record stream.
func TestStartForwardsStderr(t *testing.T) {
	lines := make(chan string, 8)
	bridge := startBridge(t, Options{RecordBuffer: 4},
		fakePi(t, fakeharness.Script{}, "--stderr", "3", "--exit-after", "300"),
		func(line []byte) { lines <- string(line) })

	for i := 0; i < 3; i++ {
		select {
		case line := <-lines:
			if !strings.Contains(line, "stderr line") {
				t.Errorf("stderr line %d = %q, want a fake-pi diagnostic", i, line)
			}
		case <-time.After(recordTimeout):
			t.Fatalf("only %d stderr lines arrived", i)
		}
	}
	if records := drainRecords(t, bridge.Records()); len(records) != 0 {
		t.Errorf("Records yielded %d records, want none: stderr must not reach it", len(records))
	}
}

// TestRecordsCloseAfterCleanExit covers the orderly path: the child exits on --exit-after,
// Records closes exactly once and Wait reports status 0.
func TestRecordsCloseAfterCleanExit(t *testing.T) {
	script := fakeharness.Script{Startup: []fakeharness.Step{{Record: json.RawMessage(
		`{"type":"extension_ui_request","id":"u1","method":"notify","message":"ready"}`)}}}
	bridge := startBridge(t, Options{RecordBuffer: 4}, fakePi(t, script, "--exit-after", "100"), nil)

	records := drainRecords(t, bridge.Records())
	if len(records) != 1 {
		t.Fatalf("got %d records, want the startup record", len(records))
	}
	if records[0].Type != "extension_ui_request" || records[0].ID != "u1" {
		t.Errorf("record = %s, want the extension_ui_request u1", records[0].Raw)
	}
	if err := bridge.Wait(); err != nil {
		t.Errorf("Wait after exit status 0 = %v, want nil", err)
	}
	if pid := bridge.PID(); pid == 0 {
		t.Error("PID() = 0 after a start, want the child pid")
	}
}

// TestWaitReportsCrash covers the fault path: exit status 9 surfaces as an error, and the
// bridge reports the exit reason instead of pretending the child is still there.
func TestWaitReportsCrash(t *testing.T) {
	bridge := startBridge(t, Options{RecordBuffer: 4},
		fakePi(t, fakeharness.Script{}, "--crash-after", "100"), nil)

	err := bridge.Wait()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Wait after --crash-after = %v, want an *exec.ExitError", err)
	}
	if code := exitErr.ExitCode(); code != 9 {
		t.Errorf("exit code = %d, want 9", code)
	}
	_, sendErr := bridge.Send(context.Background(), "after-crash", json.RawMessage(`{"type":"get_state"}`))
	if !errors.Is(sendErr, ErrClosed) {
		t.Errorf("Send to a crashed child = %v, want ErrClosed", sendErr)
	}
	if !strings.Contains(sendErr.Error(), "exit status 9") {
		t.Errorf("Send error %q does not carry the exit reason", sendErr)
	}
}

// TestCloseKillsIgnoreStdinChild covers the escalation: a child that ignores stdin EOF is
// terminated, reaped within KillGrace and reports ErrClosed afterwards.
func TestCloseKillsIgnoreStdinChild(t *testing.T) {
	bridge := startBridge(t, Options{RecordBuffer: 4, KillGrace: 300 * time.Millisecond},
		fakePi(t, fakeharness.Script{}, "--ignore-stdin"), nil)
	pid := bridge.PID()
	if pid == 0 {
		t.Fatal("PID() = 0 after a start, want the child pid")
	}

	started := time.Now()
	if err := bridge.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	if elapsed := time.Since(started); elapsed > closeTimeout {
		t.Errorf("Close took %v, want it bounded by KillGrace", elapsed)
	}
	if err := bridge.Wait(); err == nil {
		t.Error("Wait after a killed child = nil, want an exit error")
	}
	if runtime.GOOS == "linux" && processExists(pid) {
		t.Errorf("child %d is still visible in /proc after Close: it was not reaped", pid)
	}

	if _, err := bridge.Send(context.Background(), "after-close", json.RawMessage(`{"type":"get_state"}`)); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after Close = %v, want ErrClosed", err)
	}
	if err := bridge.Write(context.Background(), json.RawMessage(`{"type":"extension_ui_response","id":"u1"}`)); !errors.Is(err, ErrClosed) {
		t.Errorf("Write after Close = %v, want ErrClosed", err)
	}
}

func TestCloseIsIdempotent(t *testing.T) {
	bridge := startBridge(t, Options{RecordBuffer: 4, KillGrace: 300 * time.Millisecond},
		fakePi(t, fakeharness.Script{}, "--ignore-stdin"), nil)

	if err := bridge.Close(); err != nil {
		t.Fatalf("first Close = %v, want nil", err)
	}
	if err := bridge.Close(); err != nil {
		t.Fatalf("second Close = %v, want nil", err)
	}
	if _, err := bridge.Send(context.Background(), "after-close", json.RawMessage(`{"type":"get_state"}`)); !errors.Is(err, ErrClosed) {
		t.Errorf("Send after Close = %v, want ErrClosed", err)
	}
}

// TestCloseLeavesNoZombieWhenConsumerStops proves Close still reaps when the reader
// goroutine is parked on a consumer that stopped draining Records.
//
// Two startup records with a one-slot buffer are what put the reader on the channel send:
// with a single record the slot absorbs it and the reader waits on the pipe instead, so it
// would see stdout EOF, reap the child itself and the fallback would never be exercised.
func TestCloseLeavesNoZombieWhenConsumerStops(t *testing.T) {
	script := fakeharness.Script{Startup: []fakeharness.Step{
		{Record: json.RawMessage(`{"type":"agent_settled"}`)},
		{Record: json.RawMessage(`{"type":"agent_settled"}`)},
	}}
	bridge := startBridge(t, Options{RecordBuffer: 1, KillGrace: 200 * time.Millisecond},
		fakePi(t, script, "--ignore-stdin"), nil)
	pid := bridge.PID()

	// Nobody reads Records: the reader parks as soon as the single buffer slot is taken.
	time.Sleep(50 * time.Millisecond)
	if err := bridge.Close(); err != nil {
		t.Fatalf("Close = %v, want nil", err)
	}
	if runtime.GOOS == "linux" && processExists(pid) {
		t.Errorf("child %d is still visible in /proc after Close", pid)
	}
}

func TestWriteWithoutStartFails(t *testing.T) {
	bridge := New(Spec{Command: []string{"true"}}, Options{})
	if err := bridge.Write(context.Background(), json.RawMessage(`{"type":"extension_ui_response"}`)); !errors.Is(err, errNotStarted) {
		t.Errorf("Write before Start = %v, want %v", err, errNotStarted)
	}
}

// TestRegisterAfterCloseKillsTheChild pins finding R6: Close can run in the window between
// the spawn and register, sees started=false and returns, so the registration that lost the
// race has to kill the child itself — otherwise the just-spawned process outlives the
// bridge. The sequence is driven directly because the window cannot be hit reliably.
func TestRegisterAfterCloseKillsTheChild(t *testing.T) {
	argv := fakePi(t, fakeharness.Script{}, "--ignore-stdin")
	b, ok := New(Spec{Command: argv, Dir: t.TempDir()}, Options{KillGrace: 50 * time.Millisecond}).(*bridge)
	if !ok {
		t.Fatal("New did not return the bridge implementation")
	}
	if err := b.claimStart(); err != nil {
		t.Fatalf("claimStart: %v", err)
	}

	cmd := exec.Command(argv[0], argv[1:]...)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start scripted child: %v", err)
	}
	pid := cmd.Process.Pid

	// Close in the window: nothing is registered yet, so it cannot see the child.
	if err := b.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	b.register(cmd, nil)

	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	select {
	case <-waited:
	case <-time.After(exitTimeout):
		_ = cmd.Process.Kill()
		<-waited
		t.Fatalf("child %d survived the registration that lost the Close race", pid)
	}
	if processExists(pid) {
		t.Fatalf("child %d is still visible to the kernel after its reap", pid)
	}
}
