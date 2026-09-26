package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
)

// newAuditedHub builds a hub whose frames are recorded in a memory trail.
func newAuditedHub(t *testing.T, scope Scope, device string) (*testServer, *audit.MemoryStore) {
	t.Helper()
	store := audit.NewMemoryStore()
	log := audit.New(store, audit.Options{
		Now: func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) },
	})
	ts := newTestHub(t, func(o *Options) {
		o.Authorizer = fixedAuthorizer{scope: scope, device: device}
		o.Audit = log
	})
	return ts, store
}

// trail reads the stored events.
func trail(t *testing.T, store *audit.MemoryStore, filter audit.Filter) []audit.Event {
	t.Helper()
	events, err := store.Query(filter)
	if err != nil {
		t.Fatalf("query trail: %v", err)
	}
	return events
}

func TestDispatchedCommandsAreAudited(t *testing.T) {
	const session = "s_0123456789abcdef"
	handler := &stubCommandHandler{fn: func(context.Context, Command) (json.RawMessage, error) {
		return json.RawMessage(`{"accepted":true}`), nil
	}}
	ts, store := newAuditedHub(t, ScopeOperator, "d_0000000000000001")
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"command","id":"c1","sessionId":"` + session + `","op":"session.prompt","payload":{"message":"hi"}}`)
	client.waitFor(frameResponse)

	events := trail(t, store, audit.Filter{Action: audit.ActionSessionPrompt})
	if len(events) != 1 {
		t.Fatalf("trail = %+v, want one session.prompt", events)
	}
	event := events[0]
	if event.Outcome != audit.OutcomeOK || event.ActorDeviceID != "d_0000000000000001" ||
		event.ActorScope != "operator" || event.SessionID != session {
		t.Fatalf("event = %+v, want the operator's prompt", event)
	}
	if event.Details["op"] != "session.prompt" {
		t.Fatalf("details = %v, want the raw op", event.Details)
	}
}

func TestUnknownOpsLandInTheBoundedAction(t *testing.T) {
	handler := &stubCommandHandler{fn: func(context.Context, Command) (json.RawMessage, error) {
		return json.RawMessage(`{}`), nil
	}}
	ts, store := newAuditedHub(t, ScopeOperator, "d_0000000000000002")
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.someday_new"}`)
	client.waitFor(frameResponse)

	events := trail(t, store, audit.Filter{})
	if len(events) != 1 || events[0].Action != audit.ActionSessionCommand {
		t.Fatalf("trail = %+v, want the bounded session.command action", events)
	}
	if events[0].Details["op"] != "session.someday_new" {
		t.Fatalf("details = %v, want the raw op preserved", events[0].Details)
	}
}

func TestFailedCommandsAreAuditedWithAnErrorOutcome(t *testing.T) {
	handler := &stubCommandHandler{fn: func(context.Context, Command) (json.RawMessage, error) {
		return nil, errors.New("the child refused")
	}}
	ts, store := newAuditedHub(t, ScopeOperator, "d_0000000000000003")
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.abort"}`)
	frame := client.waitFor(frameResponse)
	if ok, _ := frame["ok"].(bool); ok {
		t.Fatalf("the command answered ok: %v", frame)
	}

	events := trail(t, store, audit.Filter{Action: audit.ActionSessionAbort})
	if len(events) != 1 || events[0].Outcome != audit.OutcomeError {
		t.Fatalf("trail = %+v, want one failed abort", events)
	}
}

func TestScopeDenialsAreAudited(t *testing.T) {
	ts, store := newAuditedHub(t, ScopeViewer, "d_0000000000000004")

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.prompt"}`)
	client.waitFor(frameResponse)

	events := trail(t, store, audit.Filter{Action: audit.ActionAuthDenied})
	if len(events) != 1 {
		t.Fatalf("trail = %+v, want one denial", events)
	}
	event := events[0]
	if event.Outcome != audit.OutcomeDenied || event.ActorScope != "viewer" || event.SessionID != "s_0123456789abcdef" {
		t.Fatalf("event = %+v, want the viewer's denied prompt", event)
	}
	if event.Details["reason"] != "scope" || event.Details["op"] != "session.prompt" {
		t.Fatalf("details = %v, want the op and the scope reason", event.Details)
	}
}

func TestRefusedHandshakesAreAudited(t *testing.T) {
	store := audit.NewMemoryStore()
	log := audit.New(store, audit.Options{})
	ts := newTestHub(t, func(o *Options) {
		o.Authorizer = fixedAuthorizer{err: errors.New("the token is not valid")}
		o.Audit = log
	})

	_, resp, err := ts.dialRaw(nil)
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("dial = %v (%v), want 401", err, resp)
	}

	events := trail(t, store, audit.Filter{})
	if len(events) != 1 || events[0].Action != audit.ActionAuthDenied ||
		events[0].Outcome != audit.OutcomeDenied || events[0].Details["reason"] != "handshake" {
		t.Fatalf("trail = %+v, want the refused handshake", events)
	}
}
