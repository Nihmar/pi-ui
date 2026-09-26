package sessions

import (
	"context"
	"encoding/json"
	"sort"
	"testing"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

func TestOpsTableCoversTheFrozenProtocol(t *testing.T) {
	want := []string{
		"session.abort",
		"session.clear_queue",
		"session.command.raw",
		"session.follow_up",
		"session.prompt",
		"session.rename",
		"session.steer",
		"session.stop",
	}
	sort.Strings(want)
	got := OpNames()
	if len(got) != len(want) {
		t.Fatalf("OpNames() = %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("OpNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestUnknownOpAndUnknownSessionAreCoded(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})

	_, err := mgr.Send(context.Background(), info.ID, "session.nope", nil)
	if code := CodeOf(err); code != CodeBadRequest {
		t.Errorf("unknown op = %v (code %q), want %q", err, code, CodeBadRequest)
	}
	_, err = mgr.Send(context.Background(), "s_missing", "session.abort", nil)
	if code := CodeOf(err); code != CodeSessionNotFound {
		t.Errorf("unknown session = %v (code %q), want %q", err, code, CodeSessionNotFound)
	}
	if code := CodeOf(nil); code != "" {
		t.Errorf("CodeOf(nil) = %q, want the empty code", code)
	}
}

func TestPromptAndTurnEndMoveTheSessionStatus(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{}, "--emit", "2", "--rate", "0")
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	if _, err := mgr.Send(context.Background(), info.ID, "session.prompt", json.RawMessage(`{"message":"hi"}`)); err != nil {
		t.Fatalf("prompt: %v", err)
	}

	waitFor(t, "the session to stream", func() bool {
		return indexOf(statusValues(t, rec), string(StatusStreaming)) >= 0
	})
	waitFor(t, "the turn to end", func() bool {
		statuses := statusValues(t, rec)
		streamingIndex := indexOf(statuses, string(StatusStreaming))
		return streamingIndex >= 0 && indexOf(statuses, string(StatusReady)) > streamingIndex
	})
	waitFor(t, "both synthetic deltas", func() bool {
		return len(rec.ofType("pi.message_update")) == 2
	})

	statuses := statusValues(t, rec)
	if streamingIndex, readyIndex := indexOf(statuses, string(StatusStreaming)), indexOf(statuses, string(StatusReady)); streamingIndex > readyIndex {
		t.Errorf("status events = %v, want streaming before ready", statuses)
	}
	if got, _ := mgr.Get(info.ID); got.Status != StatusReady {
		t.Errorf("final status = %q, want %q once the turn ended", got.Status, StatusReady)
	}
}

func TestRenameUpdatesTheProjection(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{
		Commands: map[string]fakeharness.CommandScript{
			"rename": {Response: json.RawMessage(`{"type":"response","success":true,"data":{"renamed":true}}`)},
		},
	})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Name: "before", Command: argv})

	data, err := mgr.Send(context.Background(), info.ID, "session.rename", json.RawMessage(`{"name":"after"}`))
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if string(data) != `{"renamed":true}` {
		t.Errorf("rename data = %s, want pi's data verbatim", data)
	}
	got, _ := mgr.Get(info.ID)
	if got.Name != "after" {
		t.Errorf("name = %q, want after", got.Name)
	}
	status := waitForEvent(t, rec, EventServerStatus)
	if name := payloadString(t, status, "name"); name != "after" {
		t.Errorf("%s payload name = %q, want after", EventServerStatus, name)
	}

	if _, err := mgr.Send(context.Background(), info.ID, "session.rename", json.RawMessage(`{"name":"  "}`)); CodeOf(err) != CodeBadRequest {
		t.Errorf("rename without a name = %v, want bad_request", err)
	}
	if _, err := mgr.Send(context.Background(), info.ID, "session.rename", json.RawMessage(`{`)); CodeOf(err) != CodeBadRequest {
		t.Errorf("rename with a broken payload = %v, want bad_request", err)
	}
}

func TestStopOpStopsTheSession(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})

	if _, err := mgr.Send(context.Background(), info.ID, "session.stop", nil); err != nil {
		t.Fatalf("session.stop: %v", err)
	}
	waitFor(t, "the session to stop", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && got.Status == StatusExited
	})
}
