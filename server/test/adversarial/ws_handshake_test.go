package adversarial_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// TestWS_FirstFrameMustBeHello dials, sends a valid ping before hello, and
// expects the post-upgrade refusal: close status 4401 (the number the interface
// document names, legal only after the upgrade).
func TestWS_FirstFrameMustBeHello(t *testing.T) {
	stack := newStack(t, nil)

	client := stack.mustDial(nil)
	client.send(`{"type":"ping"}`)
	client.expectClose(websocket.StatusCode(4401))
}

// TestWS_DuplicateHelloIsIgnored keeps the connection usable: a second hello has
// no id to answer and renegotiating would change the meaning of everything
// already sent, so it must be dropped rather than fatal.
func TestWS_DuplicateHelloIsIgnored(t *testing.T) {
	stack := newStack(t, nil)

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{"type":"hello","v":1,"client":{"name":"again","version":"0.0.1"}}`)
	client.send(`{"type":"ping"}`)
	client.mustNext("pong", hasType("pong"))
}

// TestWS_WrongProtocolVersionCloses4401 covers a hello the schema rejects and a
// hello that decodes but asks for a version this server does not speak.
func TestWS_WrongProtocolVersionCloses4401(t *testing.T) {
	tests := []struct {
		name  string
		hello string
	}{
		{name: "version 2", hello: `{"type":"hello","v":2,"client":{"name":"v2","version":"1"}}`},
		{name: "missing client", hello: `{"type":"hello","v":1}`},
		{name: "version as string", hello: `{"type":"hello","v":"1","client":{"name":"x","version":"1"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stack := newStack(t, nil)
			client := stack.mustDial(nil)
			client.send(tt.hello)
			client.expectClose(websocket.StatusCode(4401))
		})
	}
}

// TestWS_RefusedHandshakeIsHTTP401 covers the pre-upgrade refusals: a missing
// bearer token, a wrong one, and a token smuggled through the query string (which
// is never consulted). The §11 error envelope must come back as JSON.
func TestWS_RefusedHandshakeIsHTTP401(t *testing.T) {
	stack := newStack(t, func(o *stackOptions) { o.token = "s3cret" })

	t.Run("missing token", func(t *testing.T) {
		client, resp, err := stack.dial(nil)
		if err == nil {
			client.close()
			t.Fatalf("dial without a token succeeded, want 401")
		}
		wantStatus(t, resp, []byte(responseBody(t, resp)), http.StatusUnauthorized)
	})
	t.Run("wrong token", func(t *testing.T) {
		client, resp, err := stack.dial(func(o *websocket.DialOptions) {
			o.HTTPHeader = http.Header{"Authorization": []string{"Bearer nope"}}
		})
		if err == nil {
			client.close()
			t.Fatalf("dial with a wrong token succeeded, want 401")
		}
		wantStatus(t, resp, []byte(responseBody(t, resp)), http.StatusUnauthorized)
	})
	t.Run("token in the query string is ignored", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
		defer cancel()
		client, resp, err := websocket.Dial(ctx, stack.wsURL()+"?token=s3cret", nil)
		if err == nil {
			_ = client.CloseNow()
			t.Fatalf("dial with only a query-string token succeeded, want 401")
		}
		wantStatus(t, resp, []byte(responseBody(t, resp)), http.StatusUnauthorized)
	})
	t.Run("correct token upgrades", func(t *testing.T) {
		client := stack.mustDial(func(o *websocket.DialOptions) {
			o.HTTPHeader = http.Header{"Authorization": []string{"Bearer s3cret"}}
		})
		welcome := client.hello()
		if string(welcome["v"]) != "1" {
			t.Fatalf("welcome v = %s, want 1", welcome["v"])
		}
	})
}

// TestWS_HostAndOriginRefusedBeforeUpgrade forges the Host and Origin headers
// over a raw TCP upgrade: a DNS-rebinding Host, a foreign Origin and an opaque
// Origin must all be refused with the 401 envelope, and the same request with the
// real authority must upgrade.
func TestWS_HostAndOriginRefusedBeforeUpgrade(t *testing.T) {
	stack := newStack(t, nil)
	origin := "http://" + stack.addr()

	tests := []struct {
		name    string
		host    string
		headers map[string]string
		want    int
	}{
		{name: "real authority", host: stack.addr(), want: http.StatusSwitchingProtocols},
		{name: "same-authority origin", host: stack.addr(), headers: map[string]string{"Origin": origin}, want: http.StatusSwitchingProtocols},
		{name: "rebinding host", host: "evil.example:1234", want: http.StatusUnauthorized},
		{name: "foreign origin", host: stack.addr(), headers: map[string]string{"Origin": "http://evil.example"}, want: http.StatusUnauthorized},
		{name: "opaque origin", host: stack.addr(), headers: map[string]string{"Origin": "null"}, want: http.StatusUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, body := rawUpgrade(t, stack.addr(), tt.host, tt.headers)
			wantStatus(t, resp, []byte(body), tt.want)
			if tt.want == http.StatusUnauthorized {
				code, _ := apiError(t, []byte(body))
				if code != "unauthorized" {
					t.Fatalf("refusal code = %q, want unauthorized", code)
				}
			}
		})
	}
}

// TestWS_NonLoopbackPeerNeedsAToken drives the handler with a peer address that
// is not loopback: without a configured token the connection must be refused,
// with a configured token a missing credential must be refused too. The request
// never reaches the upgrade, so an httptest recorder is enough.
func TestWS_NonLoopbackPeerNeedsAToken(t *testing.T) {
	newRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodGet, "/ws/v1", nil)
		req.Host = "127.0.0.1"
		req.RemoteAddr = "198.51.100.7:4321"
		return req
	}

	t.Run("no token configured", func(t *testing.T) {
		hub := ws.New(ws.Options{})
		t.Cleanup(func() { _ = hub.Close() })
		recorder := httptest.NewRecorder()
		hub.ServeHTTP(recorder, newRequest())
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401 (body %s)", recorder.Code, recorder.Body.String())
		}
		code, _ := apiError(t, recorder.Body.Bytes())
		if code != "unauthorized" {
			t.Fatalf("code = %q, want unauthorized", code)
		}
	})

	t.Run("token configured, no credential", func(t *testing.T) {
		hub := ws.New(ws.Options{Token: "s3cret"})
		t.Cleanup(func() { _ = hub.Close() })
		recorder := httptest.NewRecorder()
		hub.ServeHTTP(recorder, newRequest())
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", recorder.Code)
		}
	})
}

// TestWS_OversizedFrameIsRejectedWithoutKillingTheServer sends a 2 MiB frame
// through a healthy socket. The hub's read limit must end that one connection
// (1009), and the server must keep serving new ones.
func TestWS_OversizedFrameIsRejectedWithoutKillingTheServer(t *testing.T) {
	stack := newStack(t, nil)

	client := stack.mustDial(nil)
	client.hello()
	huge := `{"type":"command","id":"huge","sessionId":"s_0000000000000000","op":"session.ping","payload":"` +
		strings.Repeat("x", 2<<20) + `"}`
	client.send(huge)

	deadline := time.Now().Add(waitTimeout)
	for {
		_, err := client.next(time.Until(deadline))
		if err != nil {
			if status := websocket.CloseStatus(err); status != websocket.StatusMessageTooBig {
				t.Fatalf("close status = %d (%v), want 1009", status, err)
			}
			break
		}
	}

	// The listener is still alive: a fresh connection completes its handshake.
	fresh := stack.mustDial(nil)
	fresh.hello()
	resp, data := stack.getJSON("/api/v1/health")
	wantStatus(t, resp, data, http.StatusOK)
}

// TestWS_InvalidJSONUnknownTypeAndBadCommandFrame feeds the connection the three
// shapes a client bug produces: unparseable JSON, an unknown frame type, and a
// command frame that fails schema validation while carrying an id. The first two
// are dropped (nothing to correlate); the third must be answered bad_request.
func TestWS_InvalidJSONUnknownTypeAndBadCommandFrame(t *testing.T) {
	stack := newStack(t, nil)

	client := stack.mustDial(nil)
	client.hello()
	client.send(`{this is not json`)
	client.send(`{"type":"frobnicate"}`)
	client.send(`{"type":"command","id":"bad-1"}`)

	response := client.mustNext("bad_request response", hasResponse("bad-1"))
	if fieldBool(t, response, "ok") {
		t.Fatalf("response = %v, want ok:false", response)
	}
	if code := errorCodeOf(response); code != "bad_request" {
		t.Fatalf("response code = %q, want bad_request", code)
	}

	client.send(`{"type":"ping"}`)
	client.mustNext("pong after the bad frames", hasType("pong"))
}
