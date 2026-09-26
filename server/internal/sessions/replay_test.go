package sessions

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/ws"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// replayEntries is the durable history the fake child pages through get_entries.
func replayEntries() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`{"id":"e1","type":"message","message":{"role":"user"}}`),
		json.RawMessage(`{"id":"e2","type":"message","message":{"role":"assistant"}}`),
		json.RawMessage(`{"id":"e3","type":"message","message":{"role":"user"}}`),
	}
}

func TestReplayFromEntryEmitsOneEventPerEntry(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{Entries: replayEntries()})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	var emitted []ws.Event
	complete, err := mgr.ReplayFromEntry(context.Background(), info.ID, "e1", func(ev ws.Event) {
		emitted = append(emitted, ev)
	})
	if err != nil {
		t.Fatalf("ReplayFromEntry: %v", err)
	}
	if !complete {
		t.Errorf("complete = false, want true for a known cursor")
	}
	if len(emitted) != 2 {
		t.Fatalf("emitted %d events, want 2 (the entries after e1)", len(emitted))
	}
	for i, want := range []string{"e2", "e3"} {
		ev := emitted[i]
		if ev.Type != ws.EventEntryAppended {
			t.Errorf("event %d type = %q, want %q", i, ev.Type, ws.EventEntryAppended)
		}
		if ev.EntryID != want {
			t.Errorf("event %d entryId = %q, want %q", i, ev.EntryID, want)
		}
		if ev.SessionID != info.ID {
			t.Errorf("event %d sessionId = %q, want %q", i, ev.SessionID, info.ID)
		}
		var payload struct {
			Entry struct {
				ID string `json:"id"`
			} `json:"entry"`
		}
		if err := json.Unmarshal(ev.Payload, &payload); err != nil {
			t.Fatalf("event %d payload = %s, want {\"entry\": {...}}: %v", i, ev.Payload, err)
		}
		if payload.Entry.ID != want {
			t.Errorf("event %d payload entry id = %q, want %q", i, payload.Entry.ID, want)
		}
		if ev.Seq != 0 {
			t.Errorf("event %d seq = %d, want the hub to assign it", i, ev.Seq)
		}
	}
}

func TestReplayFromStartReturnsEveryEntry(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{Entries: replayEntries()})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	count := 0
	complete, err := mgr.ReplayFromEntry(context.Background(), info.ID, "", func(ws.Event) { count++ })
	if err != nil || !complete {
		t.Fatalf("ReplayFromEntry = (%v, %v), want (true, nil)", complete, err)
	}
	if count != 3 {
		t.Errorf("emitted %d events, want every entry (3)", count)
	}
}

func TestReplayWithUnknownCursorFailsWithACodedError(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{Entries: replayEntries()})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	complete, err := mgr.ReplayFromEntry(context.Background(), info.ID, "nope", func(ws.Event) {
		t.Errorf("an unknown cursor must not emit entries")
	})
	if complete {
		t.Errorf("complete = true, want false: the client must reload through REST")
	}
	if code := CodeOf(err); code != CodeReplayCursorInvalid {
		t.Fatalf("ReplayFromEntry(unknown) = %v (code %q), want a %q failure", err, code, CodeReplayCursorInvalid)
	}
	if events := rec.ofType(ws.EventError); len(events) != 0 {
		t.Errorf("published %d %s events, want none: the failure belongs to the replaying connection", len(events), ws.EventError)
	}
}

func TestReplayForAnUnknownSessionFails(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	if _, err := mgr.ReplayFromEntry(context.Background(), "s_missing", "e1", nil); CodeOf(err) != CodeSessionNotFound {
		t.Errorf("ReplayFromEntry(unknown session) = %v (code %q), want %q", err, CodeOf(err), CodeSessionNotFound)
	}
}

func TestReplayForAFinishedSessionReportsTransport(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{Entries: replayEntries()})})
	if err := mgr.Stop(context.Background(), info.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitFor(t, "the child to be reaped", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && !got.Status.Live()
	})

	complete, err := mgr.ReplayFromEntry(context.Background(), info.ID, "e1", nil)
	if complete {
		t.Errorf("complete = true, want false when the child is gone")
	}
	if code := CodeOf(err); code != CodePiError {
		t.Errorf("ReplayFromEntry = %v (code %q), want %q", err, code, CodePiError)
	}
}
