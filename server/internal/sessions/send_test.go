package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/ws"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

func TestPiRejectionBecomesPiRejected(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	argv := fakeChild(t, fakeharness.Script{
		Commands: map[string]fakeharness.CommandScript{
			"prompt": {Error: "model is not configured"},
		},
	})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: argv})

	_, err := mgr.Send(context.Background(), info.ID, "session.prompt", json.RawMessage(`{"message":"hi"}`))
	if code := CodeOf(err); code != CodePiRejected {
		t.Fatalf("Send = %v (code %q), want %q", err, code, CodePiRejected)
	}
	var coded *CodedError
	if !errors.As(err, &coded) {
		t.Fatalf("error %v is not a *CodedError", err)
	}
	if message := coded.ErrorMessage(); message != "model is not configured" {
		t.Errorf("message = %q, want pi's own error string", message)
	}
	if coded.ErrorCode() != CodePiRejected {
		t.Errorf("ErrorCode() = %q, want %q", coded.ErrorCode(), CodePiRejected)
	}
	if !errors.Is(coded, nil) && coded.Unwrap() != nil {
		t.Errorf("Unwrap() = %v, want nil for a pi rejection", coded.Unwrap())
	}
}

func TestCommandRawIsAPassthrough(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{SessionID: "from-raw"})})

	data, err := mgr.Send(context.Background(), info.ID, "session.command.raw", json.RawMessage(`{"type":"get_state","id":"client-id"}`))
	if err != nil {
		t.Fatalf("command.raw: %v", err)
	}
	var state struct {
		SessionID string `json:"sessionId"`
	}
	if err := json.Unmarshal(data, &state); err != nil || state.SessionID != "from-raw" {
		t.Errorf("command.raw data = %s (err %v), want the get_state payload", data, err)
	}

	_, err = mgr.Send(context.Background(), info.ID, "session.command.raw", json.RawMessage(`{"since":"e1"}`))
	if code := CodeOf(err); code != CodeBadRequest {
		t.Errorf("command.raw without a type = %v (code %q), want %q", err, code, CodeBadRequest)
	}
}

func TestSendAfterTheChildIsGoneIsPiError(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})

	if err := mgr.Stop(context.Background(), info.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitFor(t, "the child to be reaped", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && !got.Status.Live()
	})

	_, err := mgr.Send(context.Background(), info.ID, "session.abort", nil)
	if code := CodeOf(err); code != CodePiError {
		t.Errorf("Send to a stopped session = %v (code %q), want %q", err, code, CodePiError)
	}
}

func TestHandleImplementsTheHubCommandSeam(t *testing.T) {
	mgr, _ := newTestManager(t, nil)
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})

	data, err := mgr.Handle(context.Background(), ws.Command{
		ID:        "c1",
		SessionID: info.ID,
		Op:        "session.command.raw",
		Payload:   json.RawMessage(`{"type":"get_commands"}`),
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	var payload struct {
		Commands []json.RawMessage `json:"commands"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("Handle data = %s, want the get_commands payload: %v", data, err)
	}
}

func TestCommandFromPayloadDropsTheClientID(t *testing.T) {
	command, err := commandFromPayload("prompt", json.RawMessage(`{"id":"c1","message":"hi"}`))
	if err != nil {
		t.Fatalf("commandFromPayload: %v", err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(command, &fields); err != nil {
		t.Fatalf("command is not a JSON object: %v", err)
	}
	if _, ok := fields["id"]; ok {
		t.Errorf("command kept the client id: %s", command)
	}
	if string(fields["type"]) != `"prompt"` || string(fields["message"]) != `"hi"` {
		t.Errorf("command = %s, want type prompt and message hi", command)
	}
	if _, err := commandFromPayload("prompt", json.RawMessage(`"not an object"`)); CodeOf(err) != CodeBadRequest {
		t.Errorf("non-object payload = %v, want bad_request", err)
	}
}

// TestCommandFromPayloadNullIsAnEmptyPayload pins the nil-map trap: the JSON literal
// null unmarshals into a nil map, and the setField that adds the pi command type panics
// on it. A websocket `command` frame may legitimately carry no payload at all, so the
// literal must be treated as an absent payload, never as a crashing one.
func TestCommandFromPayloadNullIsAnEmptyPayload(t *testing.T) {
	fields := map[string]json.RawMessage{}
	if err := decodeObject("payload", json.RawMessage(`null`), &fields); err != nil {
		t.Fatalf("decodeObject(null): %v", err)
	}
	if fields == nil {
		t.Fatal("decodeObject(null) left a nil map: the next setField would panic")
	}

	command, err := commandFromPayload("prompt", json.RawMessage(`null`))
	if err != nil {
		t.Fatalf("commandFromPayload(null): %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(command, &decoded); err != nil {
		t.Fatalf("command is not a JSON object: %v", err)
	}
	if string(decoded["type"]) != `"prompt"` {
		t.Errorf("command = %s, want a prompt type", command)
	}
}

// TestPromptRateLimitIsCodedAndPerSession pins PLAN.md §4.6: a session that exhausts its
// prompt budget answers rate_limited with the wait in the message, while another session
// keeps its own bucket.
func TestPromptRateLimitIsCodedAndPerSession(t *testing.T) {
	mgr, _ := newTestManager(t, func(cfg *Config) { cfg.PromptLimit = 1 })
	first := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})
	second := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: fakeChild(t, fakeharness.Script{})})

	if _, err := mgr.Send(context.Background(), first.ID, "session.prompt", json.RawMessage(`{"message":"one"}`)); err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	_, err := mgr.Send(context.Background(), first.ID, "session.prompt", json.RawMessage(`{"message":"two"}`))
	if code := CodeOf(err); code != CodeRateLimited {
		t.Fatalf("second prompt = %v (code %q), want %q", err, code, CodeRateLimited)
	}
	if !strings.Contains(err.Error(), "retry in") {
		t.Fatalf("message = %q, want the wait", err)
	}

	// Another session has its own bucket.
	if _, err := mgr.Send(context.Background(), second.ID, "session.prompt", json.RawMessage(`{"message":"one"}`)); err != nil {
		t.Fatalf("prompt on the other session: %v", err)
	}
}
