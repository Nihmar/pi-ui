package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/rpc"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// fakeChild builds fake-pi once per test process, writes script and returns the argv the
// supervisor spawns. The harness ignores every pi flag it does not know, so a test drives
// the real argv path (--mode rpc plus whatever extra adds).
func fakeChild(t *testing.T, script fakeharness.Script, extra ...string) []string {
	t.Helper()
	fakeharness.Build(t)
	path := fakeharness.WriteScript(t, script)
	argv := fakeharness.Command(path, "--mode", "rpc")
	return append(argv, extra...)
}

// indexOf returns the position of value in values, or -1.
func indexOf(values []string, value string) int {
	for i, candidate := range values {
		if candidate == value {
			return i
		}
	}
	return -1
}

func TestStartPublishesSpawnedBeforeReady(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{SessionID: "pi-session-1", SessionFile: "/tmp/pi-1.jsonl"})

	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Name: "alpha", Command: argv})

	if !strings.HasPrefix(info.ID, "s_") || len(info.ID) != len("s_")+16 {
		t.Errorf("session id = %q, want s_ + 16 hex characters", info.ID)
	}
	if info.Status != StatusReady {
		t.Errorf("status = %q, want %q", info.Status, StatusReady)
	}
	if info.PiSessionID != "pi-session-1" || info.PiSessionFile != "/tmp/pi-1.jsonl" {
		t.Errorf("pi projection = %+v, want the get_state values", info)
	}
	if info.PID <= 0 {
		t.Errorf("pid = %d, want the child's pid", info.PID)
	}

	types := rec.types()
	spawnedIndex, readyIndex := indexOf(types, EventServerSpawned), indexOf(types, EventServerReady)
	if spawnedIndex < 0 || readyIndex < 0 {
		t.Fatalf("events = %v, want %s and %s", types, EventServerSpawned, EventServerReady)
	}
	if spawnedIndex > readyIndex {
		t.Errorf("events = %v, want %s before %s", types, EventServerSpawned, EventServerReady)
	}

	spawned, _ := rec.last(EventServerSpawned)
	if status := payloadString(t, spawned, "status"); status != string(StatusSpawning) {
		t.Errorf("%s payload status = %q, want %q", EventServerSpawned, status, StatusSpawning)
	}
	if spawned.SessionID != info.ID {
		t.Errorf("%s sessionId = %q, want %q", EventServerSpawned, spawned.SessionID, info.ID)
	}

	got, ok := mgr.Get(info.ID)
	if !ok || got.Status != StatusReady {
		t.Errorf("Get(%s) = %+v (ok=%v), want the ready session", info.ID, got, ok)
	}
	if list := mgr.List(); len(list) != 1 || list[0].ID != info.ID {
		t.Errorf("List() = %+v, want exactly the started session", list)
	}
}

func TestChildRecordsArePublishedVerbatim(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	record := json.RawMessage(`{"type":"extension_ui_request","id":"u1","method":"notify","message":"ready","notifyType":"info"}`)
	argv := fakeChild(t, fakeharness.Script{Startup: []fakeharness.Step{{Record: record}}})

	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	notify := waitForEvent(t, rec, EventExtNotify)
	if notify.SessionID != info.ID {
		t.Errorf("%s sessionId = %q, want %q", EventExtNotify, notify.SessionID, info.ID)
	}
	if string(notify.Payload) != string(record) {
		t.Errorf("%s payload = %s, want the record verbatim (%s)", EventExtNotify, notify.Payload, record)
	}
	if frames := rec.dialogFrames(); len(frames) != 0 {
		t.Errorf("a fire-and-forget notify produced request frames: %+v", frames)
	}
}

func TestPiRecordsReachThePublisherWithTheirEntryID(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	entry := json.RawMessage(`{"type":"entry_appended","entry":{"id":"e7","type":"message"}}`)
	argv := fakeChild(t, fakeharness.Script{Startup: []fakeharness.Step{{Record: entry}}})

	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	appended := waitForEvent(t, rec, "pi.entry_appended")
	if appended.EntryID != "e7" {
		t.Errorf("entryId = %q, want e7", appended.EntryID)
	}
	if string(appended.Payload) != string(entry) {
		t.Errorf("payload = %s, want the record verbatim", appended.Payload)
	}
	if appended.SessionID != info.ID {
		t.Errorf("sessionId = %q, want %q", appended.SessionID, info.ID)
	}
}

func TestUnreadableRecordIsNotDropped(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	// A record without a type is what rpc hands over for a line it could not classify;
	// the supervisor forwards it instead of inventing an event name or dropping it.
	argv := fakeChild(t, fakeharness.Script{Startup: []fakeharness.Step{{Record: json.RawMessage(`{"unknown":"shape"}`)}}})

	startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	unknown := waitForEvent(t, rec, EventPiUnknown)
	if string(unknown.Payload) != `{"unknown":"shape"}` {
		t.Errorf("payload = %s, want the record verbatim", unknown.Payload)
	}
}

// TestUnparseableRecordStaysReadableOnTheWire pins finding 6.2: a line the child got wrong
// is not valid JSON and cannot be a WS payload as-is (the hub refuses what it cannot
// frame), so it travels as pi.unknown with a {"raw":"<line>"} payload that round-trips the
// original bytes. A record that is valid JSON still travels verbatim.
func TestUnparseableRecordStaysReadableOnTheWire(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	s := newSession(mgr, codegenSessionID, Spec{CWD: "/work"})

	line := "not json: {\"type\": \"message\", \"text\": \"a\u2028b\"}"
	s.handleRecord(rpc.Record{Raw: []byte(line)})

	events := rec.ofType(EventPiUnknown)
	if len(events) != 1 {
		t.Fatalf("%s events = %d, want 1", EventPiUnknown, len(events))
	}
	if !json.Valid(events[0].Payload) {
		t.Fatalf("payload = %s, want valid JSON the hub can frame", events[0].Payload)
	}
	envelope := decodePayload(t, events[0])
	if len(envelope) != 1 {
		t.Errorf("payload = %s, want exactly the raw field", events[0].Payload)
	}
	if got := payloadString(t, events[0], "raw"); got != line {
		t.Errorf("raw = %q, want the original line %q", got, line)
	}

	record := json.RawMessage(`{"unknown":"shape"}`)
	s.handleRecord(rpc.Record{Raw: record})
	events = rec.ofType(EventPiUnknown)
	if len(events) != 2 {
		t.Fatalf("%s events = %d, want 2", EventPiUnknown, len(events))
	}
	if string(events[1].Payload) != string(record) {
		t.Errorf("payload = %s, want a valid-JSON record verbatim (%s)", events[1].Payload, record)
	}
}

func TestInvalidSpecIsRejectedBeforeSpawning(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	valid := fakeChild(t, fakeharness.Script{})

	file := t.TempDir() + "/not-a-directory"
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	tests := []struct {
		name string
		spec Spec
	}{
		{name: "no cwd", spec: Spec{Command: valid}},
		{name: "cwd is a file", spec: Spec{CWD: file, Command: valid}},
		{name: "cwd does not exist", spec: Spec{CWD: "/nonexistent-pi-ui-dir", Command: valid}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := mgr.Start(context.Background(), tt.spec); !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("Start = %v, want ErrInvalidSpec", err)
			}
			if code := CodeOf(ErrInvalidSpec); code != CodeBadRequest {
				t.Errorf("CodeOf(ErrInvalidSpec) = %q, want %q", code, CodeBadRequest)
			}
		})
	}
	if events := rec.types(); len(events) != 0 {
		t.Errorf("events = %v, want none: nothing was spawned", events)
	}
	if list := mgr.List(); len(list) != 0 {
		t.Errorf("List() = %+v, want no session for a rejected spec", list)
	}
}
