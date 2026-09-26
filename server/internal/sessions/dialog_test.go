package sessions

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// confirmRecord is the dialog the bridge raises for a dangerous bash command (§8).
const confirmRecord = `{"type":"extension_ui_request","id":"u1","method":"confirm","title":"Approve?","message":"rm -rf /","timeout":30000}`

func TestDialogBecomesRequestFrameAndFirstAnswerWins(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{
		Startup:  []fakeharness.Step{{Record: json.RawMessage(confirmRecord)}},
		Commands: map[string]fakeharness.CommandScript{"prompt": {Response: json.RawMessage(`{"type":"response","success":true,"data":{"ok":true}}`)}},
	})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	waitFor(t, "the dialog to be routed to the hub", func() bool { return len(rec.dialogFrames()) == 1 })
	frame := rec.dialogFrames()[0]
	if frame.sessionID != info.ID {
		t.Errorf("request frame sessionID = %q, want %q", frame.sessionID, info.ID)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(frame.frame, &fields); err != nil {
		t.Fatalf("request frame is not a JSON object: %v (%s)", err, frame.frame)
	}
	for key, want := range map[string]string{
		"type":      `"request"`,
		"id":        `"u1"`,
		"method":    `"confirm"`,
		"title":     `"Approve?"`,
		"message":   `"rm -rf /"`,
		"sessionId": `"` + info.ID + `"`,
		"timeoutMs": `60000`,
	} {
		if got, ok := fields[key]; !ok || string(got) != want {
			t.Errorf("request frame %s = %s (present %v), want %s", key, fields[key], ok, want)
		}
	}
	if _, ok := fields["ts"]; !ok {
		t.Errorf("request frame has no ts: %s", frame.frame)
	}

	// First answer wins.
	if err := mgr.Respond(context.Background(), info.ID, "u1", json.RawMessage(`{"confirmed":true}`)); err != nil {
		t.Fatalf("first Respond: %v", err)
	}
	err := mgr.Respond(context.Background(), info.ID, "u1", json.RawMessage(`{"confirmed":false}`))
	if code := CodeOf(err); code != CodeAlreadyAnswered {
		t.Errorf("second Respond = %v (code %q), want %q", err, code, CodeAlreadyAnswered)
	}
	if err := mgr.Respond(context.Background(), info.ID, "u_unknown", json.RawMessage(`{"confirmed":true}`)); CodeOf(err) != CodeNotFound {
		t.Errorf("Respond with an unknown id = %v (code %q), want %q", err, CodeOf(err), CodeNotFound)
	}
	if err := mgr.Respond(context.Background(), "s_missing", "u1", json.RawMessage(`{"confirmed":true}`)); CodeOf(err) != CodeSessionNotFound {
		t.Errorf("Respond for an unknown session = %v (code %q), want %q", err, CodeOf(err), CodeSessionNotFound)
	}

	// The answer went to the child on the same stdin the commands use: the session must
	// still answer a prompt afterwards.
	data, err := mgr.Send(context.Background(), info.ID, "session.prompt", json.RawMessage(`{"message":"hi"}`))
	if err != nil {
		t.Fatalf("prompt after answering a dialog: %v", err)
	}
	if string(data) != `{"ok":true}` {
		t.Errorf("prompt data = %s, want pi's data verbatim", data)
	}
}

func TestDialogTimeoutAnswersCancelled(t *testing.T) {
	mgr, rec := newTestManager(t, func(cfg *Config) { cfg.DialogTimeout = 40 * time.Millisecond })
	argv := fakeChild(t, fakeharness.Script{Startup: []fakeharness.Step{{Record: json.RawMessage(confirmRecord)}}})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	timedOut := waitForEvent(t, rec, EventServerDialogTimeout)
	if requestID := payloadString(t, timedOut, "requestId"); requestID != "u1" {
		t.Errorf("requestId = %q, want u1", requestID)
	}
	if method := payloadString(t, timedOut, "method"); method != "confirm" {
		t.Errorf("method = %q, want confirm", method)
	}
	if timedOut.SessionID != info.ID {
		t.Errorf("sessionId = %q, want %q", timedOut.SessionID, info.ID)
	}

	// A late answer is already_answered, not an error the client could retry into.
	waitFor(t, "the dialog to be closed by the timeout", func() bool {
		err := mgr.Respond(context.Background(), info.ID, "u1", json.RawMessage(`{"confirmed":true}`))
		return CodeOf(err) == CodeAlreadyAnswered
	})
}

func TestRequestFrameKeepsEveryExtensionField(t *testing.T) {
	raw := json.RawMessage(`{"type":"extension_ui_request","id":"u9","method":"select","title":"Pick","options":["a","b"],"placeholder":"p","prefill":"f","unknown":"kept"}`)
	frame, err := requestFrame("s_0123456789abcdef", raw, 60*time.Second)
	if err != nil {
		t.Fatalf("requestFrame: %v", err)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(frame, &fields); err != nil {
		t.Fatalf("frame is not a JSON object: %v", err)
	}
	for _, key := range []string{"id", "method", "title", "options", "placeholder", "prefill", "unknown", "sessionId", "timeoutMs", "ts", "type"} {
		if _, ok := fields[key]; !ok {
			t.Errorf("frame has no %q: %s", key, frame)
		}
	}
	if string(fields["unknown"]) != `"kept"` {
		t.Errorf("unknown field = %s, want it preserved", fields["unknown"])
	}
	if string(fields["timeoutMs"]) != "60000" {
		t.Errorf("timeoutMs = %s, want 60000", fields["timeoutMs"])
	}
	if string(fields["sessionId"]) != `"s_0123456789abcdef"` {
		t.Errorf("sessionId = %s, want the server session id", fields["sessionId"])
	}
}

func TestExtensionUIResponseAcceptsBothShapes(t *testing.T) {
	tests := []struct {
		name     string
		response string
		want     map[string]string
	}{
		{name: "value", response: `{"value":"Allow"}`, want: map[string]string{"value": `"Allow"`}},
		{name: "confirmed", response: `{"confirmed":true}`, want: map[string]string{"confirmed": `true`}},
		{name: "cancelled", response: `{"cancelled":true}`, want: map[string]string{"cancelled": `true`}},
		{name: "the whole frame", response: `{"type":"ui_response","sessionId":"s_1","id":"other","value":"Deny"}`, want: map[string]string{"value": `"Deny"`}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record, err := extensionUIResponse("u1", json.RawMessage(tt.response))
			if err != nil {
				t.Fatalf("extensionUIResponse: %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(record, &fields); err != nil {
				t.Fatalf("record is not a JSON object: %v", err)
			}
			if string(fields["type"]) != `"extension_ui_response"` || string(fields["id"]) != `"u1"` {
				t.Errorf("record = %s, want type extension_ui_response and id u1", record)
			}
			for key, want := range tt.want {
				if string(fields[key]) != want {
					t.Errorf("record %s = %s, want %s", key, fields[key], want)
				}
			}
		})
	}
}

func TestExtensionUIResponseRejectsAnUnusableAnswer(t *testing.T) {
	for _, response := range []string{`{"nope":1}`, `"value"`, ``, `[]`} {
		record, err := extensionUIResponse("u1", json.RawMessage(response))
		if err == nil {
			t.Errorf("extensionUIResponse(%q) = %s, want an error", response, record)
			continue
		}
		if code := CodeOf(err); code != CodeBadRequest {
			t.Errorf("CodeOf(%v) = %q, want %q", err, code, CodeBadRequest)
		}
	}
}

func TestUnanswerableRequestWithoutIDIsForwarded(t *testing.T) {
	mgr, rec := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{Startup: []fakeharness.Step{
		{Record: json.RawMessage(`{"type":"extension_ui_request","method":"confirm","title":"Approve?"}`)},
	}})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	ev := waitForEvent(t, rec, "pi.extension_ui_request")
	if ev.SessionID != info.ID {
		t.Errorf("sessionId = %q, want %q", ev.SessionID, info.ID)
	}
	if frames := rec.dialogFrames(); len(frames) != 0 {
		t.Errorf("a dialog without an id was routed as a request frame: %+v", frames)
	}
}

func TestFireAndForgetMethodsBecomeExtEvents(t *testing.T) {
	tests := []struct {
		method string
		event  string
	}{
		{method: "notify", event: EventExtNotify},
		{method: "setStatus", event: EventExtStatus},
		{method: "setWidget", event: EventExtWidget},
		{method: "setTitle", event: EventExtTitle},
		{method: "set_editor_text", event: EventExtEditorText},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			mgr, rec := newTestManager(t, nil)
			record := json.RawMessage(`{"type":"extension_ui_request","id":"m1","method":"` + tt.method + `","title":"t"}`)
			argv := fakeChild(t, fakeharness.Script{Startup: []fakeharness.Step{{Record: record}}})
			startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

			ev := waitForEvent(t, rec, tt.event)
			if string(ev.Payload) != string(record) {
				t.Errorf("payload = %s, want the record verbatim (%s)", ev.Payload, record)
			}
			if frames := rec.dialogFrames(); len(frames) != 0 {
				t.Errorf("%s was routed as a request frame", tt.method)
			}
			if drain := rec.ofType(EventServerDialogTimeout); len(drain) != 0 {
				t.Errorf("%s armed a dialog timeout", tt.method)
			}
		})
	}
}
