package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
)

// TestPingIsAnsweredWithPong covers the client-initiated keepalive.
func TestPingIsAnsweredWithPong(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"ping"}`)
	if got := client.next()["type"]; got != framePong {
		t.Fatalf("answer to ping = %v, want %s", got, framePong)
	}
}

// TestMalformedFrameWithIDIsAnswered checks the validation gate: a frame that does
// not match the schema never reaches a handler, but the client still gets a
// terminal answer for the id it used.
func TestMalformedFrameWithIDIsAnswered(t *testing.T) {
	handler := &stubCommandHandler{fn: func(context.Context, Command) (json.RawMessage, error) {
		t.Error("handler was called for an invalid frame")
		return nil, nil
	}}
	ts := newTestHub(t, nil)
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	// sessionId does not match the schema pattern, so the frame is rejected before
	// the op is even looked at.
	client.sendRaw(`{"type":"command","id":"c1","sessionId":"not-a-session","op":"session.abort"}`)

	frame := client.next()
	if frame["type"] != frameResponse || frame["id"] != "c1" {
		t.Fatalf("answer = %v, want a response for c1", frame)
	}
	if ok, _ := frame["ok"].(bool); ok {
		t.Fatalf("response.ok = true for an invalid frame: %v", frame)
	}
	if code := errorCodeOf(t, frame); code != codeBadRequest {
		t.Fatalf("response.error.code = %q, want %q", code, codeBadRequest)
	}
	if len(handler.calls) != 0 {
		t.Fatalf("handler saw %d commands, want none", len(handler.calls))
	}
}

// TestDroppedFramesKeepTheConnectionUsable: a malformed frame without an id has
// nothing to correlate, so it is dropped — and the connection stays healthy.
func TestDroppedFramesKeepTheConnectionUsable(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()
	client.hello()

	for _, raw := range []string{
		`{"type":"subscribe","sessionId":"not-a-session"}`,
		`{"type":"something_new"}`,
		`{"type":"command","id":"","sessionId":"s_0123456789abcdef","op":"session.abort"}`,
		`["not","an","object"]`,
	} {
		client.sendRaw(raw)
		client.sendRaw(`{"type":"ping"}`)
		if got := client.next()["type"]; got != framePong {
			t.Fatalf("after %s the answer was %v, want %s (the frame must be dropped, not fatal)", raw, got, framePong)
		}
	}
}

// TestUnknownFieldsArePreserved: the schema is lenient on purpose, and everything
// the hub does not understand travels with the event it belongs to.
func TestUnknownFieldsArePreserved(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"subscribe","sessionId":"s_0123456789abcdef","replay":false,"futureField":{"a":1}}`)
	// The subscribe is processed by the read loop, so the publish has to wait for
	// the subscription to exist: publishing first would test a race, not fan-out.
	eventually(t, "the subscription to be registered", func() bool {
		_, sessions := ts.hub.counts()
		return sessions == 1
	})
	ts.hub.Publish(Event{Type: "pi.message_update", SessionID: "s_0123456789abcdef", Payload: json.RawMessage(`{"delta":"hi","piOnly":true}`)})

	event := client.waitFor("pi.message_update")
	payload := payloadOf(t, event)
	if payload["piOnly"] != true || payload["delta"] != "hi" {
		t.Fatalf("payload = %v, want the published fields untouched", payload)
	}
}

// TestDuplicateHelloIsIgnored: only the first frame may negotiate, and a second
// hello cannot reset what was already agreed.
func TestDuplicateHelloIsIgnored(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"hello","v":1,"client":{"name":"again","version":"1"}}`)
	client.sendRaw(`{"type":"ping"}`)

	if got := client.next()["type"]; got != framePong {
		t.Fatalf("second hello produced %v, want it dropped (next frame: %s)", got, framePong)
	}
}

// TestHelloValidation pins the shapes the schema accepts and rejects for the
// handshake frame itself.
func TestHelloValidation(t *testing.T) {
	h := New(withDefaults(Options{})).(*hub)
	defer h.Close()

	valid := []string{
		`{"type":"hello","v":1,"client":{"name":"c","version":"1"}}`,
		`{"type":"hello","v":1,"client":{"name":"c","version":"1"},"extra":[1,2]}`,
		`{"type":"hello","v":1,"client":{"name":"c","version":"1","os":"linux"}}`,
	}
	for _, raw := range valid {
		if !h.validateFrame([]byte(raw)) {
			t.Errorf("valid hello rejected: %s", raw)
		}
	}
	invalid := []string{
		`{"type":"hello","v":1,"client":{"name":"c"}}`,
		`{"type":"hello","v":2,"client":{"name":"c","version":"1"}}`,
		`{"type":"hello","v":1}`,
		`{"type":"hello","client":{"name":"c","version":"1"}}`,
	}
	for _, raw := range invalid {
		if h.validateFrame([]byte(raw)) {
			t.Errorf("invalid hello accepted: %s", raw)
		}
	}
}

// TestBinaryFramesAreIgnored: the protocol is text only, and a stray binary frame
// is not a reason to drop a working connection.
func TestBinaryFramesAreIgnored(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()
	client.hello()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()
	if err := client.conn.Write(ctx, websocket.MessageBinary, []byte{0x01, 0x02}); err != nil {
		t.Fatalf("write binary frame: %v", err)
	}

	client.sendRaw(`{"type":"ping"}`)
	if got := client.next()["type"]; got != framePong {
		t.Fatalf("answer after a binary frame = %v, want %s", got, framePong)
	}
}

// TestUnauthorizedResponseShape pins the body of a refused handshake: the same
// §11 error shape the REST surface uses, so a client needs one parser for both.
func TestUnauthorizedResponseShape(t *testing.T) {
	h := New(withDefaults(Options{Token: "s3cret"})).(*hub)
	defer h.Close()

	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, request("127.0.0.1:8787", "127.0.0.1:50000", "127.0.0.1:8787", ""))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("refusal body is not JSON: %v (%s)", err, recorder.Body.String())
	}
	if body.Error.Code != codeUnauthorized || body.Error.Message == "" {
		t.Fatalf("refusal body = %+v, want code %q and a message", body.Error, codeUnauthorized)
	}
}
