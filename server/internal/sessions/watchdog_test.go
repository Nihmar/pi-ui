package sessions

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

func TestIdleSessionIsEvicted(t *testing.T) {
	mgr, rec := newTestManager(t, func(cfg *Config) {
		cfg.IdleTimeout = 100 * time.Millisecond
		cfg.WatchInterval = 50 * time.Millisecond
	})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})

	waitFor(t, "the idle session to be evicted", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && got.Status == StatusExited
	})
	if _, ok := rec.last(EventServerStopping); !ok {
		t.Error("no server.stopping event for the eviction")
	}
	if _, ok := rec.last(EventServerExited); !ok {
		t.Error("no server.exited event for the eviction")
	}
}

func TestIdleEvictionAsksForTheWrapUpNote(t *testing.T) {
	script := fakeharness.Script{Commands: map[string]fakeharness.CommandScript{
		"prompt": {Events: []fakeharness.Step{
			{Record: json.RawMessage(`{"type":"agent_settled"}`)},
		}},
	}}
	mgr, rec := newTestManager(t, func(cfg *Config) {
		cfg.IdleTimeout = 100 * time.Millisecond
		cfg.WatchInterval = 50 * time.Millisecond
		cfg.WrapUpPrompt = "write the handoff note"
		cfg.WrapUpBudget = 300 * time.Millisecond
	})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, script)})

	waitFor(t, "the eviction to finish", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && got.Status == StatusExited
	})
	if _, ok := rec.last(piEventType(recordAgentSettled)); !ok {
		t.Fatal("the wrap-up turn never ran: no agent_settled after the idle window")
	}
}

func TestStreamingSessionIsNotEvicted(t *testing.T) {
	mgr, _ := newTestManager(t, func(cfg *Config) {
		// The idle window is shorter than the stream, so only a session that keeps
		// producing records can survive it.
		cfg.IdleTimeout = 200 * time.Millisecond
		cfg.WatchInterval = 50 * time.Millisecond
	})
	argv := fakeChild(t, fakeharness.Script{}, "--emit", "40", "--rate", "20")
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})
	if _, err := mgr.Send(context.Background(), info.ID, "session.prompt", json.RawMessage(`{"message":"go"}`)); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	time.Sleep(700 * time.Millisecond)
	got, _ := mgr.Get(info.ID)
	if got.Status == StatusExited {
		t.Fatalf("a streaming session was evicted: %+v", got)
	}
}

func TestIdleTimeoutZeroDisablesTheWatchdog(t *testing.T) {
	mgr, _ := newTestManager(t, func(cfg *Config) {
		cfg.IdleTimeout = 0
	})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})
	time.Sleep(300 * time.Millisecond)
	got, _ := mgr.Get(info.ID)
	if got.Status != StatusReady {
		t.Fatalf("status = %q, want ready with the watchdog off", got.Status)
	}
}
