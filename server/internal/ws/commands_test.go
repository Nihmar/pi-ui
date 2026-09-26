package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestCommandReachesTheHandler checks the whole command path: the validated frame
// arrives as a Command, and the handler's data becomes response.data.
func TestCommandReachesTheHandler(t *testing.T) {
	handler := &stubCommandHandler{fn: func(_ context.Context, c Command) (json.RawMessage, error) {
		if c.Op != "session.prompt" {
			t.Errorf("op = %q, want session.prompt", c.Op)
		}
		if string(c.Payload) != `{"message":"hi"}` {
			t.Errorf("payload = %s, want the frame payload verbatim", c.Payload)
		}
		return json.RawMessage(`{"accepted":true}`), nil
	}}
	ts := newTestHub(t, nil)
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.prompt","payload":{"message":"hi"}}`)

	frame := client.waitFor(frameResponse)
	if frame["id"] != "c1" {
		t.Fatalf("response id = %v, want c1", frame["id"])
	}
	if ok, _ := frame["ok"].(bool); !ok {
		t.Fatalf("response.ok = false: %v", frame)
	}
	if data, _ := frame["data"].(map[string]any); data["accepted"] != true {
		t.Fatalf("response.data = %v, want the handler data", frame["data"])
	}
	if len(handler.calls) != 1 || handler.calls[0].SessionID != "s_0123456789abcdef" {
		t.Fatalf("handler calls = %+v, want one call for the session", handler.calls)
	}
}

// TestCommandPayloadIsOptional covers the ops that take no arguments.
func TestCommandPayloadIsOptional(t *testing.T) {
	handler := &stubCommandHandler{fn: func(_ context.Context, c Command) (json.RawMessage, error) {
		if len(c.Payload) != 0 {
			t.Errorf("payload = %s, want empty", c.Payload)
		}
		return nil, nil
	}}
	ts := newTestHub(t, nil)
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.abort"}`)

	frame := client.waitFor(frameResponse)
	if ok, _ := frame["ok"].(bool); !ok {
		t.Fatalf("response.ok = false: %v", frame)
	}
	if _, present := frame["data"]; present {
		t.Fatalf("response carries data %v for a handler that returned none", frame["data"])
	}
}

// TestCommandErrorMapping checks the contract with sessions: a coded error keeps
// its code and message, an unknown code degrades to internal, and a plain error
// becomes internal instead of leaking a made-up code.
func TestCommandErrorMapping(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode string
	}{
		{name: "coded error", err: &codedStubError{code: "session_not_found", message: "no such session"}, wantCode: "session_not_found"},
		{name: "coded timeout", err: &codedStubError{code: "timeout", message: "slow"}, wantCode: "timeout"},
		{name: "unknown code", err: &codedStubError{code: "made_up", message: "?"}, wantCode: codeInternal},
		{name: "empty code", err: &codedStubError{message: "?"}, wantCode: codeInternal},
		{name: "plain error", err: errStubPlain, wantCode: codeInternal},
		{name: "deadline", err: context.DeadlineExceeded, wantCode: codeTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := &stubCommandHandler{fn: func(context.Context, Command) (json.RawMessage, error) {
				return nil, tc.err
			}}
			ts := newTestHub(t, nil)
			ts.hub.SetCommandHandler(handler)

			client := ts.dial(nil)
			defer client.close()
			client.hello()

			client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.abort"}`)

			frame := client.waitFor(frameResponse)
			if ok, _ := frame["ok"].(bool); ok {
				t.Fatalf("response.ok = true for a failing handler: %v", frame)
			}
			if code := errorCodeOf(t, frame); code != tc.wantCode {
				t.Fatalf("error.code = %q, want %q", code, tc.wantCode)
			}
			if errObj, _ := frame["error"].(map[string]any); errObj["message"] == "" {
				t.Fatalf("error.message is empty: %v", frame)
			}
		})
	}
}

// TestCommandWithoutHandlerAnswersUnsupported: a hub with no wiring is a
// configuration state, not a dropped request.
func TestCommandWithoutHandlerAnswersUnsupported(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.abort"}`)

	frame := client.waitFor(frameResponse)
	if code := errorCodeOf(t, frame); code != codeUnsupported {
		t.Fatalf("error.code = %q, want %q", code, codeUnsupported)
	}
}

// TestSlowCommandDoesNotBlockTheConnection is why handlers run in their own
// goroutine: a prompt that takes seconds must not stop an abort from being read.
func TestSlowCommandDoesNotBlockTheConnection(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	handler := &stubCommandHandler{fn: func(ctx context.Context, c Command) (json.RawMessage, error) {
		if c.Op == "session.prompt" {
			close(started)
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return json.RawMessage(`{"done":true}`), nil
		}
		return nil, nil
	}}
	ts := newTestHub(t, nil)
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.prompt","payload":{"message":"hi"}}`)
	select {
	case <-started:
	case <-time.After(testTimeout):
		t.Fatal("handler never started")
	}

	// The prompt is still in flight; the connection has to answer an abort while it
	// is, and the abort's response must not wait for the prompt.
	client.sendRaw(`{"type":"command","id":"c2","sessionId":"s_0123456789abcdef","op":"session.abort"}`)

	abort := client.waitFor(frameResponse)
	if abort["id"] != "c2" {
		t.Fatalf("first response = %v, want the abort response first", abort)
	}
	if code := errorCodeOf(t, abort); code != "" {
		t.Fatalf("abort failed with %q, want success while the prompt is pending", code)
	}

	close(release)
	done := client.waitFor(frameResponse)
	if done["id"] != "c1" {
		t.Fatalf("second response = %v, want the prompt response", done)
	}
}

// TestUIResponseReachesTheDialogHandler pins the answer contract: the handler gets
// the request id and only the answer object, and the response echoes the dialog id.
func TestUIResponseReachesTheDialogHandler(t *testing.T) {
	handler := &stubDialogHandler{}
	ts := newTestHub(t, nil)
	ts.hub.SetDialogHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"ui_response","sessionId":"s_0123456789abcdef","id":"u1","confirmed":false}`)

	frame := client.waitFor(frameResponse)
	if frame["id"] != "u1" {
		t.Fatalf("response id = %v, want the dialog id u1", frame["id"])
	}
	if ok, _ := frame["ok"].(bool); !ok {
		t.Fatalf("response.ok = false: %v", frame)
	}
	if len(handler.calls) != 1 {
		t.Fatalf("dialog handler calls = %+v, want one", handler.calls)
	}
	call := handler.calls[0]
	if call.sessionID != "s_0123456789abcdef" || call.requestID != "u1" {
		t.Fatalf("handler saw %+v, want the session and request id", call)
	}
	if string(call.response) != `{"confirmed":false}` {
		t.Fatalf("answer = %s, want only the keys the client sent", call.response)
	}
}

// TestUIResponseAnswers covers the three answer shapes: each becomes the minimal
// object the handler can act on.
func TestUIResponseAnswers(t *testing.T) {
	tests := []struct {
		name  string
		frame string
		want  string
	}{
		{name: "confirmed true", frame: `{"type":"ui_response","sessionId":"s_0123456789abcdef","id":"u1","confirmed":true}`, want: `{"confirmed":true}`},
		{name: "cancelled", frame: `{"type":"ui_response","sessionId":"s_0123456789abcdef","id":"u1","cancelled":true}`, want: `{"cancelled":true}`},
		{name: "select value", frame: `{"type":"ui_response","sessionId":"s_0123456789abcdef","id":"u1","value":"Allow"}`, want: `{"value":"Allow"}`},
		{name: "editor value", frame: `{"type":"ui_response","sessionId":"s_0123456789abcdef","id":"u1","value":{"text":"edited"}}`, want: `{"value":{"text":"edited"}}`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			handler := &stubDialogHandler{}
			ts := newTestHub(t, nil)
			ts.hub.SetDialogHandler(handler)

			client := ts.dial(nil)
			defer client.close()
			client.hello()

			client.sendRaw(tc.frame)
			client.waitFor(frameResponse)

			if len(handler.calls) != 1 {
				t.Fatalf("dialog handler calls = %+v, want one", handler.calls)
			}
			if got := string(handler.calls[0].response); got != tc.want {
				t.Fatalf("answer = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestUIResponseErrorMapping: losing the race for a dialog must reach the client
// as already_answered, not as a generic failure.
func TestUIResponseErrorMapping(t *testing.T) {
	handler := &stubDialogHandler{err: &codedStubError{code: "already_answered", message: "another client answered first"}}
	ts := newTestHub(t, nil)
	ts.hub.SetDialogHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"ui_response","sessionId":"s_0123456789abcdef","id":"u1","confirmed":true}`)

	frame := client.waitFor(frameResponse)
	if ok, _ := frame["ok"].(bool); ok {
		t.Fatalf("response.ok = true for a refused answer: %v", frame)
	}
	if code := errorCodeOf(t, frame); code != "already_answered" {
		t.Fatalf("error.code = %q, want already_answered", code)
	}
}

// TestUIResponseWithoutHandlerAnswersUnsupported covers the unwired case.
func TestUIResponseWithoutHandlerAnswersUnsupported(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"ui_response","sessionId":"s_0123456789abcdef","id":"u1","cancelled":true}`)

	frame := client.waitFor(frameResponse)
	if code := errorCodeOf(t, frame); code != codeUnsupported {
		t.Fatalf("error.code = %q, want %q", code, codeUnsupported)
	}
}

// TestCommandWithUnencodableDataStillAnswers pins finding W8: a handler result that is
// not valid JSON cannot be framed, and dropping the terminal response would leave the
// client waiting for that id forever. It answers internal instead.
func TestCommandWithUnencodableDataStillAnswers(t *testing.T) {
	handler := &stubCommandHandler{fn: func(context.Context, Command) (json.RawMessage, error) {
		return json.RawMessage(`not json`), nil
	}}
	ts := newTestHub(t, nil)
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.prompt"}`)

	frame := client.waitFor(frameResponse)
	if frame["id"] != "c1" {
		t.Fatalf("response id = %v, want c1", frame["id"])
	}
	if ok, _ := frame["ok"].(bool); ok {
		t.Fatalf("response.ok = true for a result that is not JSON: %v", frame)
	}
	if code := errorCodeOf(t, frame); code != codeInternal {
		t.Fatalf("error code = %q, want %q", code, codeInternal)
	}
}
