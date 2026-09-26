package adversarial_test

import (
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Nihmar/pi-ui/server/internal/ws"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// TestWS_UnknownOpAndMalformedPayloads sends commands a buggy or hostile client
// produces: an op nobody registered, and payloads that are not the object the op
// needs. Every one must come back as a coded response, never as a closed socket.
func TestWS_UnknownOpAndMalformedPayloads(t *testing.T) {
	stack := newStack(t, nil)
	info := stack.startSession(t, t.TempDir())

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + info.ID + `"}`)

	tests := []struct {
		name string
		op   string
		body string
		want string
	}{
		{name: "unknown op", op: "session.explode", body: `{}`, want: "bad_request"},
		{name: "command.raw without an object", op: "session.command.raw", body: `"hi"`, want: "bad_request"},
		{name: "command.raw without a type", op: "session.command.raw", body: `{"message":"hi"}`, want: "bad_request"},
		{name: "rename without a name", op: "session.rename", body: `{}`, want: "bad_request"},
		{name: "prompt with a scalar payload", op: "session.prompt", body: `"hi"`, want: "bad_request"},
	}
	for i, tt := range tests {
		id := "op-" + itoa(i)
		client.send(`{"type":"command","id":"` + id + `","sessionId":"` + info.ID + `","op":"` + tt.op + `","payload":` + tt.body + `}`)
		response := client.mustNext("the response of "+tt.name, hasResponse(id))
		if fieldBool(t, response, "ok") {
			t.Errorf("%s: response = %v, want ok:false", tt.name, response)
			continue
		}
		if code := errorCodeOf(response); code != tt.want {
			t.Errorf("%s: code = %q, want %q", tt.name, code, tt.want)
		}
	}

	// The connection is still a connection: a ping is answered.
	client.send(`{"type":"ping"}`)
	client.mustNext("pong", hasType("pong"))
}

// TestWS_SubscribeToUnknownSessionIsNotAnError pins the documented behaviour: the
// server cannot tell a race from a typo, so subscribing to an id it never issued
// is silent and the client learns about the missing session from REST.
func TestWS_SubscribeToUnknownSessionIsNotAnError(t *testing.T) {
	stack := newStack(t, nil)

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"s_0000000000000000"}`)
	client.send(`{"type":"ping"}`)
	client.mustNext("pong", hasType("pong"))

	for _, frameType := range client.frames {
		if frameType == "server.error" || frameType == "response" {
			t.Fatalf("subscribing to an unknown session produced a %s (frames: %v)", frameType, client.frames)
		}
	}
}

// dialogScript makes fake-pi raise one blocking confirm dialog right after it
// answers a prompt, which is the extension UI subprotocol of §8.
func dialogScript(id string) fakeharness.Script {
	return fakeharness.Script{
		Commands: map[string]fakeharness.CommandScript{
			"prompt": {Events: []fakeharness.Step{
				{Record: []byte(`{"type":"extension_ui_request","id":"` + id + `","method":"confirm","title":"Approve?"}`)},
			}},
		},
	}
}

// TestWS_DialogFirstAnswerWins covers the §8 contract: the dialog reaches the
// session's subscribers as a `request` frame; the first ui_response wins and the
// second gets already_answered, both correlated by the pi request id.
func TestWS_DialogFirstAnswerWins(t *testing.T) {
	script := dialogScript("dlg-1")
	stack := newStack(t, func(o *stackOptions) { o.script = &script })
	info := stack.startSession(t, t.TempDir())

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + info.ID + `"}`)
	client.send(`{"type":"command","id":"c1","sessionId":"` + info.ID + `","op":"session.prompt","payload":{"message":"hi"}}`)
	client.mustNext("the prompt response", hasResponse("c1"))

	request := client.mustNext("the dialog request frame", hasType("request"))
	if got := fieldString(request, "id"); got != "dlg-1" {
		t.Fatalf("request id = %q, want dlg-1", got)
	}
	if got := fieldString(request, "sessionId"); got != info.ID {
		t.Fatalf("request sessionId = %q, want %s", got, info.ID)
	}
	if got := fieldString(request, "method"); got != "confirm" {
		t.Fatalf("request method = %q, want confirm", got)
	}

	client.send(`{"type":"ui_response","sessionId":"` + info.ID + `","id":"dlg-1","confirmed":true}`)
	first := client.mustNext("the dialog response", hasResponse("dlg-1"))
	if !fieldBool(t, first, "ok") {
		t.Fatalf("first answer = %v, want ok:true", first)
	}

	client.send(`{"type":"ui_response","sessionId":"` + info.ID + `","id":"dlg-1","confirmed":false}`)
	second := client.mustNext("the already_answered response", hasResponse("dlg-1"))
	if fieldBool(t, second, "ok") {
		t.Fatalf("second answer = %v, want ok:false", second)
	}
	if code := errorCodeOf(second); code != "already_answered" {
		t.Fatalf("second answer code = %q, want already_answered", code)
	}
}

// TestWS_DialogTimeoutAnswersAndRetains covers the timeout path: a dialog nobody
// answers is cancelled for the child and reported as server.dialog.timeout, and a
// late answer is already_answered rather than not_found (the entry is retained).
func TestWS_DialogTimeoutAnswersAndRetains(t *testing.T) {
	script := dialogScript("dlg-timeout")
	stack := newStack(t, func(o *stackOptions) {
		o.script = &script
		o.dialogTimeout = 250 * time.Millisecond
	})
	info := stack.startSession(t, t.TempDir())

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + info.ID + `"}`)
	client.send(`{"type":"command","id":"c1","sessionId":"` + info.ID + `","op":"session.prompt","payload":{"message":"hi"}}`)
	client.mustNext("the prompt response", hasResponse("c1"))
	client.mustNext("the dialog request frame", hasType("request"))

	timeout := client.mustNext("server.dialog.timeout", hasType("server.dialog.timeout"))
	if got := fieldString(payloadOf(t, timeout), "requestId"); got != "dlg-timeout" {
		t.Fatalf("dialog.timeout requestId = %q, want dlg-timeout", got)
	}

	client.send(`{"type":"ui_response","sessionId":"` + info.ID + `","id":"dlg-timeout","confirmed":true}`)
	late := client.mustNext("the late answer", hasResponse("dlg-timeout"))
	if fieldBool(t, late, "ok") {
		t.Fatalf("late answer = %v, want ok:false", late)
	}
	if code := errorCodeOf(late); code != "already_answered" {
		t.Fatalf("late answer code = %q, want already_answered", code)
	}
}

// TestWS_ReplayUnknownEntryIdIsCursorInvalid subscribes with a durable cursor pi
// does not know. The replaying subscriber must get
// server.error{replay_cursor_invalid} before server.replay.end{complete:false}, so
// the client knows to reload through REST instead of rendering an empty history —
// and the failure belongs to that connection alone: a second subscriber of the
// same session never sees it (finding 6.3, fixed in `9c8d604`).
func TestWS_ReplayUnknownEntryIdIsCursorInvalid(t *testing.T) {
	stack := newStack(t, nil)
	info := stack.startSession(t, t.TempDir())

	// The other subscriber is connected first: if the failure were published into
	// the session stream, it would arrive on this socket as a live event.
	other := stack.mustDial(nil)
	other.hello()
	other.send(`{"type":"subscribe","sessionId":"` + info.ID + `"}`)

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + info.ID + `","since":{"entryId":"does-not-exist"}}`)
	client.mustNext("server.replay.begin", hasType("server.replay.begin"))

	// Strict order: the reason must precede the end of the replay.
	var order []string
	var complete bool
	deadline := time.Now().Add(waitTimeout)
	for len(order) < 2 && time.Now().Before(deadline) {
		frame, err := client.next(time.Until(deadline))
		if err != nil {
			t.Fatalf("reading the replay: %v (frames: %v)", err, client.frames)
		}
		switch frameType(frame) {
		case "server.error":
			if errorCodeOf(frame) == "replay_cursor_invalid" {
				order = append(order, "error")
			}
		case "server.replay.end":
			complete = fieldBool(t, payloadOf(t, frame), "complete")
			order = append(order, "end")
		}
	}
	if len(order) != 2 {
		t.Fatalf("replay frames = %v, want server.error{replay_cursor_invalid} then server.replay.end (frames: %v)", order, client.frames)
	}
	if order[0] != "error" {
		t.Errorf("frame order = %v, want the error before replay.end (frames: %v)", order, client.frames)
	}
	if complete {
		t.Errorf("server.replay.end complete = true, want false (frames: %v)", client.frames)
	}

	// The failure is connection-scoped: the other subscriber pings and sees no
	// server.error at all, so nothing was published into the session stream.
	other.send(`{"type":"ping"}`)
	deadline = time.Now().Add(waitTimeout)
	for {
		frame, err := other.next(time.Until(deadline))
		if err != nil {
			t.Fatalf("waiting for the other subscriber's pong: %v (frames: %v)", err, other.frames)
		}
		if frameType(frame) == "server.error" {
			t.Errorf("the other subscriber saw server.error{%s}: the cursor failure leaked into the session stream (frames: %v)", errorCodeOf(frame), other.frames)
		}
		if frameType(frame) == "pong" {
			break
		}
	}
}

// TestWS_ReplaySinceSeqOutOfWindowIsTruncated fills a 3-event ring, resumes from
// a cursor older than what is retained, and asserts the client is told about the
// gap: the three retained events in order, deduplicated, truncated=true.
func TestWS_ReplaySinceSeqOutOfWindowIsTruncated(t *testing.T) {
	stack := newStack(t, func(o *stackOptions) { o.replayEvents = 3 })
	sessionID := "s_00000000000000aa"

	payload := func(i int) []byte {
		return []byte(`{"i":` + itoa(i) + `}`)
	}
	var firstSeq uint64
	for i := 1; i <= 6; i++ {
		seq := stack.hub.Publish(ws.Event{Type: "pi.note", SessionID: sessionID, Payload: payload(i)})
		if i == 1 {
			firstSeq = seq
		}
	}

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + sessionID + `","since":{"seq":` + itoa(int(firstSeq)) + `}}`)
	client.mustNext("server.replay.begin", hasType("server.replay.begin"))

	var notes []int
	deadline := time.Now().Add(waitTimeout)
	truncated, complete := false, true
	for time.Now().Before(deadline) {
		frame, err := client.next(time.Until(deadline))
		if err != nil {
			t.Fatalf("reading the replay: %v (frames: %v)", err, client.frames)
		}
		switch frameType(frame) {
		case "pi.note":
			var note struct {
				I int `json:"i"`
			}
			decodeJSON(t, frame["payload"], &note)
			notes = append(notes, note.I)
		case "server.replay.end":
			endPayload := payloadOf(t, frame)
			truncated = fieldBool(t, endPayload, "truncated")
			complete = fieldBool(t, endPayload, "complete")
			goto done
		}
	}
done:
	if len(notes) != 3 || notes[0] != 4 || notes[1] != 5 || notes[2] != 6 {
		t.Errorf("replayed notes = %v, want [4 5 6] (the last three of the ring)", notes)
	}
	if !truncated {
		t.Errorf("server.replay.end truncated = false, want true (frames: %v)", client.frames)
	}
	if !complete {
		t.Errorf("server.replay.end complete = false, want true (frames: %v)", client.frames)
	}
}

// TestWS_SlowConsumerIsDisconnected sends far more data than the subscriber reads:
// the bounded queue overflows, the server spends the queue on one terminal
// server.error{slow_consumer} frame and closes the socket with 1008.
func TestWS_SlowConsumerIsDisconnected(t *testing.T) {
	stack := newStack(t, func(o *stackOptions) { o.sendBuffer = 2 })
	sessionID := "s_00000000000000bb"

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"subscribe","sessionId":"` + sessionID + `"}`)

	// Nothing is read yet, so the writer blocks once the socket buffers are full
	// and the 2-slot queue overflows after a handful of 256 KiB frames.
	filler := strings.Repeat("x", 256<<10)
	bulk := []byte(`{"pad":"` + filler + `"}`)
	for i := 0; i < 100; i++ {
		stack.hub.Publish(ws.Event{Type: "pi.bulk", SessionID: sessionID, Payload: bulk})
	}

	sawSlowConsumer := false
	deadline := time.Now().Add(waitTimeout)
	for {
		frame, err := client.next(time.Until(deadline))
		if err != nil {
			if status := websocket.CloseStatus(err); status != websocket.StatusPolicyViolation {
				t.Fatalf("close status = %d (%v), want 1008 (frames: %v)", status, err, client.frames)
			}
			break
		}
		if frameType(frame) == "server.error" && errorCodeOf(frame) == "slow_consumer" {
			sawSlowConsumer = true
		}
	}
	if !sawSlowConsumer {
		t.Errorf("no server.error{slow_consumer} arrived before the close (frames: %v)", client.frames)
	}
}
