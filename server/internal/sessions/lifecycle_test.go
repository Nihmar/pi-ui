package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/rpc"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

func TestStopPublishesExitedAndKeepsSessionListed(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})

	if err := mgr.Stop(context.Background(), info.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	exited := waitForEvent(t, rec, EventServerExited)
	if code := payloadInt(t, exited, "exitCode"); code != 0 {
		t.Errorf("%s exitCode = %d, want 0 (stdin EOF is pi's orderly shutdown)", EventServerExited, code)
	}
	if _, crashed := rec.last(EventServerCrashed); crashed {
		t.Errorf("an explicit stop was reported as a crash")
	}
	if _, stopping := rec.last(EventServerStopping); !stopping {
		t.Errorf("Stop did not publish %s", EventServerStopping)
	}

	waitFor(t, "the session to be marked exited", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && got.Status == StatusExited
	})
	got, ok := mgr.Get(info.ID)
	if !ok {
		t.Fatalf("the stopped session was dropped from the list")
	}
	if got.ExitCode == nil || *got.ExitCode != 0 {
		t.Errorf("exitCode = %v, want 0", got.ExitCode)
	}

	// Stop is idempotent: the REST endpoint must stay callable after the child is gone.
	if err := mgr.Stop(context.Background(), info.ID); err != nil {
		t.Errorf("second Stop: %v", err)
	}
	if err := mgr.Stop(context.Background(), "s_missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Stop(unknown) = %v, want ErrNotFound", err)
	}
}

func TestChildCrashIsReportedAndStaysListed(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{}, "--crash-after", "300")
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	crashed := waitForEvent(t, rec, EventServerCrashed)
	if code := payloadInt(t, crashed, "exitCode"); code != 9 {
		t.Errorf("%s exitCode = %d, want 9", EventServerCrashed, code)
	}
	if _, exited := rec.last(EventServerExited); exited {
		t.Errorf("a crashing child was reported as an orderly exit")
	}

	waitFor(t, "the crashed session to be listed", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && got.Status == StatusCrashed
	})
	got, _ := mgr.Get(info.ID)
	if got.ExitCode == nil || *got.ExitCode != 9 {
		t.Errorf("exitCode = %v, want 9", got.ExitCode)
	}
}

func TestSessionLimitCountsLiveSessionsOnly(t *testing.T) {
	mgr, _ := newTestManager(t, func(cfg *Config) { cfg.MaxSessions = 1 })
	first := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})

	_, err := mgr.Start(context.Background(), Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})
	if !errors.Is(err, ErrLimit) {
		t.Fatalf("Start over the limit = %v, want ErrLimit", err)
	}
	if code := CodeOf(err); code != CodeSessionLimit {
		t.Errorf("CodeOf(%v) = %q, want %q", err, code, CodeSessionLimit)
	}

	// A finished session releases its slot: a long-lived server must not wedge on history.
	if err := mgr.Stop(context.Background(), first.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitFor(t, "the first session to exit", func() bool {
		got, ok := mgr.Get(first.ID)
		return ok && got.Status == StatusExited
	})
	second := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})
	if len(mgr.List()) != 2 {
		t.Errorf("List() = %+v, want the exited session to stay listed", mgr.List())
	}
	if second.Status != StatusReady {
		t.Errorf("second session status = %q, want %q", second.Status, StatusReady)
	}
}

func TestShutdownStopsEveryChildWithinBudget(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	infos := make([]Info, 0, 2)
	for i := 0; i < 2; i++ {
		infos = append(infos, startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})}))
	}

	// The server's own context is already cancelled when Shutdown runs on SIGTERM, which
	// must not make it skip the wait.
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	started := time.Now()
	if err := mgr.Shutdown(cancelled); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed > stopBudget+200*time.Millisecond {
		t.Errorf("Shutdown took %s, want it bounded by %s", elapsed, stopBudget)
	}

	for _, info := range infos {
		got, ok := mgr.Get(info.ID)
		if !ok {
			t.Fatalf("session %s vanished from the list on shutdown", info.ID)
		}
		if got.Status != StatusExited {
			t.Errorf("session %s status = %q, want %q", info.ID, got.Status, StatusExited)
		}
		if err := syscall.Kill(got.PID, 0); !errors.Is(err, syscall.ESRCH) {
			t.Errorf("child %d of session %s is still alive (kill(0) = %v)", got.PID, info.ID, err)
		}
	}
	if err := mgr.Shutdown(cancelled); err != nil {
		t.Errorf("second Shutdown: %v", err)
	}
}

// stalledBridge is an rpc.Bridge whose child is "reaped" as soon as Close runs while the
// record stream stays open. It makes the window the verification report calls 6.1
// deterministic: the child is gone, but the pump has not published the terminal status.
type stalledBridge struct {
	records chan rpc.Record
	closed  sync.Once
}

var _ rpc.Bridge = (*stalledBridge)(nil)

func newStalledBridge() *stalledBridge {
	return &stalledBridge{records: make(chan rpc.Record)}
}

func (b *stalledBridge) Start(context.Context) error { return nil }

func (b *stalledBridge) Send(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	return nil, rpc.ErrClosed
}

func (b *stalledBridge) Write(context.Context, json.RawMessage) error { return rpc.ErrClosed }

func (b *stalledBridge) Records() <-chan rpc.Record { return b.records }

func (b *stalledBridge) PID() int { return 0 }

func (b *stalledBridge) Wait() error { return nil }

func (b *stalledBridge) Close() error { return nil }

// end closes the record stream, which is what lets the pump publish the terminal status.
func (b *stalledBridge) end() { b.closed.Do(func() { close(b.records) }) }

// TestStopWaitsForTheTerminalStatus pins finding 6.1: Shutdown waits for the per-session
// stop goroutines, so stop must not return before the pump published the terminal status —
// waiting for the reaped child alone leaves a window in which the projection still says
// "stopping". The stalled record stream makes that window deterministic.
func TestStopWaitsForTheTerminalStatus(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	s := newSession(mgr, codegenSessionID, Spec{CWD: "/work"})
	bridge := newStalledBridge()
	s.attach(bridge)
	go s.pump()

	stopped := make(chan error, 1)
	go func() { stopped <- s.stop(context.Background()) }()

	select {
	case err := <-stopped:
		t.Fatalf("stop returned (%v) while the terminal status was still unwritten", err)
	case <-time.After(50 * time.Millisecond):
	}

	bridge.end()
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("stop: %v", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("stop did not return after the pump published the terminal status")
	}

	if _, ok := rec.last(EventServerExited); !ok {
		t.Errorf("no %s event after stop returned", EventServerExited)
	}
	if status := s.info().Status; status != StatusExited {
		t.Errorf("status = %q, want %q", status, StatusExited)
	}
}

// TestStartFailureReapsTheChild covers a child that never confirms readiness: it ignores
// stdin, so get_state never comes back. Start must report ErrStart, stop the child anyway
// (stdin close, then SIGTERM) and still list the session with its final status.
func TestStartFailureReapsTheChild(t *testing.T) {
	mgr, rec := newTestManager(t, func(cfg *Config) { cfg.SendTimeout = 200 * time.Millisecond })
	spec := Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{}, "--ignore-stdin")}

	_, err := mgr.Start(context.Background(), spec)
	if !errors.Is(err, ErrStart) {
		t.Fatalf("Start of a silent child = %v, want ErrStart", err)
	}

	waitFor(t, "the child to be reaped", func() bool {
		list := mgr.List()
		return len(list) == 1 && !list[0].Status.Live()
	})
	info := mgr.List()[0]
	if info.Status != StatusExited {
		t.Errorf("status = %q, want %q after the failed readiness", info.Status, StatusExited)
	}
	if err := syscall.Kill(info.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("child %d is still alive (kill(0) = %v)", info.PID, err)
	}
	if _, ok := rec.last(EventServerSpawned); !ok {
		t.Errorf("no %s event for a child that was spawned", EventServerSpawned)
	}
	if _, ok := rec.last(EventServerReady); ok {
		t.Errorf("%s was published for a session that never answered get_state", EventServerReady)
	}
}

// TestStopReportsAStalledTerminalStatus pins the other half of finding 6.1: when the pump
// never publishes the terminal status, stop must report the timeout instead of returning
// nil, so a caller that gets no error can trust that the session finished its bookkeeping.
func TestStopReportsAStalledTerminalStatus(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	s := newSession(mgr, codegenSessionID, Spec{CWD: "/work"})
	bridge := newStalledBridge()
	s.attach(bridge)
	go s.pump()

	err := s.stop(context.Background())
	if code := CodeOf(err); code != CodeTimeout {
		t.Fatalf("stop of a stalled pump = %v (code %q), want %s", err, code, CodeTimeout)
	}

	// Let the pump finish so the next test starts from a quiet tree.
	bridge.end()
}
