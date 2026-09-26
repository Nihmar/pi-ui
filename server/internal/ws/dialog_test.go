package ws

import (
	"encoding/json"
	"testing"
)

// validRequestFrame is a complete request frame, the shape sessions builds from an
// extension_ui_request.
func validRequestFrame(sessionID string) json.RawMessage {
	return json.RawMessage(`{"type":"request","id":"u1","sessionId":"` + sessionID +
		`","method":"confirm","title":"Bash","message":"run?","ts":"2025-01-02T03:04:05.678Z"}`)
}

// TestSendRequestReachesSubscribers covers the outbound dialog seam: the frame is
// written verbatim to the subscribers of that session and to nobody else.
func TestSendRequestReachesSubscribers(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, nil)
	requester := interface{}(ts.hub).(Requester)

	watcher := ts.dial(nil)
	defer watcher.close()
	watcher.hello()
	watcher.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","replay":false}`)

	other := ts.dial(nil)
	defer other.close()
	other.hello()
	other.sendRaw(`{"type":"subscribe","sessionId":"s_ffffffffffffffff","replay":false}`)

	eventually(t, "both subscriptions to be registered", func() bool {
		_, sessions := ts.hub.counts()
		return sessions == 2
	})

	if err := requester.SendRequest(session, validRequestFrame(session)); err != nil {
		t.Fatalf("SendRequest: %v", err)
	}

	got := watcher.waitFor(frameRequest)
	if got["id"] != "u1" || got["method"] != "confirm" || got["title"] != "Bash" {
		t.Fatalf("request frame = %v, want the frame verbatim", got)
	}
	if _, present := got["seq"]; present {
		t.Fatalf("request frame carries a seq: %v", got)
	}

	// The other subscriber must not see the dialog: it is scoped to one session.
	other.sendRaw(`{"type":"ping"}`)
	if typ := other.next()["type"]; typ != framePong {
		t.Fatalf("subscriber of another session received %v, want nothing but the pong", typ)
	}
}

// TestSendRequestValidatesTheFrame: the hub is the last gate before the wire, so a
// frame the schema would reject is refused at the call site.
func TestSendRequestValidatesTheFrame(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, nil)
	requester := interface{}(ts.hub).(Requester)

	tests := []struct {
		name  string
		frame string
	}{
		{name: "not an object", frame: `"nope"`},
		{name: "not json", frame: `{`},
		{name: "missing ts", frame: `{"type":"request","id":"u1","sessionId":"` + session + `","method":"confirm","title":"Bash"}`},
		{name: "missing title", frame: `{"type":"request","id":"u1","sessionId":"` + session + `","method":"confirm","ts":"2025-01-02T03:04:05.678Z"}`},
		{name: "fire-and-forget method", frame: `{"type":"request","id":"u1","sessionId":"` + session + `","method":"notify","title":"Bash","ts":"2025-01-02T03:04:05.678Z"}`},
		{name: "wrong session pattern", frame: `{"type":"request","id":"u1","sessionId":"nope","method":"confirm","title":"Bash","ts":"2025-01-02T03:04:05.678Z"}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := requester.SendRequest(session, json.RawMessage(tc.frame)); err == nil {
				t.Fatalf("SendRequest accepted an invalid frame: %s", tc.frame)
			}
		})
	}
}

// TestSendRequestWithoutSubscribersIsNotAnError: a dialog with nobody to answer it
// is a state the session layer handles (it times the dialog out), not a hub failure.
func TestSendRequestWithoutSubscribersIsNotAnError(t *testing.T) {
	ts := newTestHub(t, nil)
	requester := interface{}(ts.hub).(Requester)

	if err := requester.SendRequest("s_0123456789abcdef", validRequestFrame("s_0123456789abcdef")); err != nil {
		t.Fatalf("SendRequest without subscribers: %v", err)
	}
}

// TestSendRequestAfterClose: a closed hub cannot deliver, and saying so is the
// only honest answer.
func TestSendRequestAfterClose(t *testing.T) {
	ts := newTestHub(t, nil)
	requester := interface{}(ts.hub).(Requester)

	if err := ts.hub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := requester.SendRequest("s_0123456789abcdef", validRequestFrame("s_0123456789abcdef")); err == nil {
		t.Fatal("SendRequest accepted a frame after Close")
	}
}
