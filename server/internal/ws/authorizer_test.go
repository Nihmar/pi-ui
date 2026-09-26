package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
)

// fixedAuthorizer accepts every request with one identity, for the scope tests.
type fixedAuthorizer struct {
	scope  Scope
	device string
	err    error
}

// Authorize implements Authorizer.
func (a fixedAuthorizer) Authorize(*http.Request) (Scope, string, error) {
	if a.err != nil {
		return "", "", a.err
	}
	return a.scope, a.device, nil
}

// headerAuthorizer reads the device id from a header, so one hub can hold connections
// with different identities (the CloseDevice test).
type headerAuthorizer struct {
	scope Scope
}

// Authorize implements Authorizer.
func (a headerAuthorizer) Authorize(r *http.Request) (Scope, string, error) {
	return a.scope, r.Header.Get("X-Test-Device"), nil
}

// scopedHub builds a hub whose handshake is decided by a.
func scopedHub(t *testing.T, a Authorizer) *testServer {
	t.Helper()
	return newTestHub(t, func(o *Options) { o.Authorizer = a })
}

// dialWithHeader opens a connection that carries one device header.
func dialWithHeader(t *testing.T, ts *testServer, device string) *testClient {
	t.Helper()
	return ts.dial(func(o *websocket.DialOptions) {
		o.HTTPHeader = http.Header{"X-Test-Device": {device}}
	})
}

func TestAuthorizerRefusesTheHandshake(t *testing.T) {
	ts := scopedHub(t, fixedAuthorizer{err: errors.New("the token is not valid")})

	_, resp, err := ts.dialRaw(nil)
	if err == nil {
		t.Fatal("dial succeeded, want a refused handshake")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v (%v), want 401", resp, err)
	}
}

func TestUnknownScopeFromTheAuthorizerIsRefused(t *testing.T) {
	// A typo must not become an operator by accident: the hub fails closed.
	ts := scopedHub(t, fixedAuthorizer{scope: Scope("root")})

	_, resp, err := ts.dialRaw(nil)
	if err == nil {
		t.Fatal("dial succeeded, want a refused handshake")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %v (%v), want 401", resp, err)
	}
}

func TestViewerReadsButCannotDrive(t *testing.T) {
	const session = "s_0123456789abcdef"
	handler := &stubCommandHandler{fn: func(context.Context, Command) (json.RawMessage, error) {
		return json.RawMessage(`{"accepted":true}`), nil
	}}
	dialog := &stubDialogHandler{}
	ts := scopedHub(t, fixedAuthorizer{scope: ScopeViewer, device: "d_0000000000000001"})
	ts.hub.SetCommandHandler(handler)
	ts.hub.SetDialogHandler(dialog)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	// Reading is a viewer's job: a subscription and its replay are accepted.
	subscribe(t, ts, client, `{"type":"subscribe","sessionId":"`+session+`","replay":false}`, 1)

	// Driving is not. The command gets the terminal answer the protocol promises, and
	// nothing is dispatched.
	client.sendRaw(`{"type":"command","id":"c1","sessionId":"` + session + `","op":"session.prompt","payload":{"message":"hi"}}`)
	frame := client.waitFor(frameResponse)
	if frame["id"] != "c1" {
		t.Fatalf("response id = %v, want c1", frame["id"])
	}
	if ok, _ := frame["ok"].(bool); ok {
		t.Fatalf("viewer command answered ok: %v", frame)
	}
	if code := errorCodeOf(t, frame); code != codeForbiddenScope {
		t.Fatalf("error code = %q, want %q", code, codeForbiddenScope)
	}
	if len(handler.calls) != 0 {
		t.Fatalf("the handler ran for a viewer: %+v", handler.calls)
	}

	// Answering a dialog drives the run too.
	client.sendRaw(`{"type":"ui_response","sessionId":"` + session + `","id":"dlg-1","confirmed":true}`)
	dialogFrame := client.waitFor(frameResponse)
	if code := errorCodeOf(t, dialogFrame); code != codeForbiddenScope {
		t.Fatalf("dialog answer code = %q, want %q", code, codeForbiddenScope)
	}
	if len(dialog.calls) != 0 {
		t.Fatalf("the dialog handler ran for a viewer: %+v", dialog.calls)
	}

	// The connection is still usable.
	client.sendRaw(`{"type":"ping"}`)
	client.waitFor(framePong)
}

func TestOperatorDrivesTheSession(t *testing.T) {
	const session = "s_0123456789abcdef"
	handler := &stubCommandHandler{fn: func(context.Context, Command) (json.RawMessage, error) {
		return json.RawMessage(`{"accepted":true}`), nil
	}}
	ts := scopedHub(t, fixedAuthorizer{scope: ScopeOperator, device: "d_0000000000000002"})
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	client.sendRaw(`{"type":"command","id":"c1","sessionId":"` + session + `","op":"session.prompt","payload":{"message":"hi"}}`)
	frame := client.waitFor(frameResponse)
	if ok, _ := frame["ok"].(bool); !ok {
		t.Fatalf("operator command refused: %v", frame)
	}
	if len(handler.calls) != 1 {
		t.Fatalf("handler calls = %d, want 1", len(handler.calls))
	}
}

func TestCloseDeviceClosesOnlyThatDevice(t *testing.T) {
	ts := scopedHub(t, headerAuthorizer{scope: ScopeOperator})

	first := dialWithHeader(t, ts, "d_000000000000000a")
	defer first.close()
	first.hello()
	second := dialWithHeader(t, ts, "d_000000000000000b")
	defer second.close()
	second.hello()

	if got := ts.hub.CloseDevice(""); got != 0 {
		t.Fatalf("CloseDevice(empty) = %d, want 0", got)
	}
	if got := ts.hub.CloseDevice("d_000000000000000a"); got != 1 {
		t.Fatalf("CloseDevice(a) = %d, want 1", got)
	}
	first.expectClose(websocket.StatusCode(closeCodeUnauthorized))

	// The other device's connection is untouched.
	second.sendRaw(`{"type":"ping"}`)
	second.waitFor(framePong)
	if got := ts.hub.CloseDevice("d_000000000000000a"); got != 0 {
		t.Fatalf("CloseDevice(a) again = %d, want 0", got)
	}
}

func TestAuthorizerSeesTheHostAndOriginChecksFirst(t *testing.T) {
	// A foreign Host is refused before the authorizer even runs: the transport rules
	// are not negotiable by a credential. The request is built directly because a
	// dialer's HTTPHeader cannot forge Host (net/http takes it from the URL).
	called := false
	h := New(Options{Authorizer: funcAuthorizer(func(*http.Request) (Scope, string, error) {
		called = true
		return ScopeOperator, "", nil
	})}).(*hub)
	defer h.Close()

	r := httptest.NewRequest(http.MethodGet, "http://evil.example"+path, nil)
	r.Host = "evil.example"
	r = r.WithContext(localAddrContext(r.Context(), "127.0.0.1:8787"))
	if _, err := h.authorize(r); err == nil {
		t.Fatal("a foreign Host was accepted")
	}
	if called {
		t.Fatal("the authorizer ran for a request with a foreign Host")
	}
}

// funcAuthorizer adapts a function to the Authorizer seam.
type funcAuthorizer func(*http.Request) (Scope, string, error)

// Authorize implements Authorizer.
func (f funcAuthorizer) Authorize(r *http.Request) (Scope, string, error) { return f(r) }
