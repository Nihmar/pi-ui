package ws

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// localAddrContext returns a request context carrying the listener address, which
// is what the Host rule compares the Host header against.
func localAddrContext(ctx context.Context, authority string) context.Context {
	host, port := splitHostPort(authority)
	addr := &net.TCPAddr{IP: net.ParseIP(host)}
	if port != "" {
		if parsed, err := strconv.Atoi(port); err == nil {
			addr.Port = parsed
		}
	}
	return context.WithValue(ctx, http.LocalAddrContextKey, addr)
}

// request builds a request the way the checks see it: a Host header, a peer
// address and, when given, the local listen address.
func request(host, remote, local, origin string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://"+host+path, nil)
	r.Host = host
	if remote != "" {
		r.RemoteAddr = remote
	}
	if local != "" {
		r = r.WithContext(localAddrContext(r.Context(), local))
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	return r
}

// TestHandshakeSendsWelcome is the happy path: hello first, welcome with the
// negotiated version and the advertised server facts.
func TestHandshakeSendsWelcome(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()

	welcome := client.hello()

	if got := welcome["v"]; got != float64(1) {
		t.Errorf("welcome v = %v, want 1", got)
	}
	if sec, ok := welcome["heartbeatSec"].(float64); !ok || sec < 1 {
		t.Errorf("welcome heartbeatSec = %v, want >= 1", welcome["heartbeatSec"])
	}
	server, ok := welcome["server"].(map[string]any)
	if !ok {
		t.Fatalf("welcome has no server object: %v", welcome)
	}
	if server["version"] != "0.0.1-test" || server["piVersion"] != "0.87.1" {
		t.Errorf("welcome server = %v, want the configured versions", server)
	}
	if features, ok := server["features"].([]any); !ok || len(features) != 1 {
		t.Errorf("welcome features = %v, want [bridge]", server["features"])
	}
	if limits, ok := server["limits"].(map[string]any); !ok || limits["maxSessions"] != float64(8) {
		t.Errorf("welcome limits = %v, want maxSessions", server["limits"])
	}
}

// TestHandshakeRequiresHelloFirst refuses a connection whose first frame is
// something else: nothing may be negotiated before the version is.
func TestHandshakeRequiresHelloFirst(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()

	client.sendRaw(`{"type":"ping"}`)
	client.expectClose(websocket.StatusCode(closeCodeUnauthorized))
}

// TestHandshakeRejectsUnsupportedVersion pins the v:1 negotiation: a well-formed v2
// hello is refused with the version in the close reason, not with the generic schema
// message, because the schema cannot express a version it does not speak.
func TestHandshakeRejectsUnsupportedVersion(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()

	client.sendRaw(`{"type":"hello","v":2,"client":{"name":"future","version":"9"}}`)
	_, err := client.read()
	var closeErr websocket.CloseError
	if !errors.As(err, &closeErr) {
		t.Fatalf("read error = %v, want a close error", err)
	}
	if closeErr.Code != websocket.StatusCode(closeCodeUnauthorized) {
		t.Fatalf("close code = %v, want %v", closeErr.Code, closeCodeUnauthorized)
	}
	if !strings.Contains(closeErr.Reason, "version 2") {
		t.Fatalf("close reason = %q, want it to name the offered version", closeErr.Reason)
	}
}

// TestHandshakeSkipsBinaryFrames pins finding W6: a binary frame is not the protocol, so
// it is ignored while waiting for hello exactly like it is ignored after the handshake.
func TestHandshakeSkipsBinaryFrames(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()

	client.sendBinary([]byte{0x00, 0x01, 0x02})
	if welcome := client.hello(); welcome["type"] != frameWelcome {
		t.Fatalf("first frame after hello = %v, want %s", welcome["type"], frameWelcome)
	}
}

// TestHandshakeRejectsMalformedHello covers a hello that is not a hello: the
// schema requires the client object, so a partial frame cannot be negotiated.
func TestHandshakeRejectsMalformedHello(t *testing.T) {
	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()

	client.sendRaw(`{"type":"hello","v":1}`)
	client.expectClose(websocket.StatusCode(closeCodeUnauthorized))
}

// TestHandshakeTimeoutClosesSilentClient proves a client that upgrades and then
// says nothing cannot hold a slot.
func TestHandshakeTimeoutClosesSilentClient(t *testing.T) {
	previous := handshakeTimeout
	handshakeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { handshakeTimeout = previous })

	ts := newTestHub(t, nil)
	client := ts.dial(nil)
	defer client.close()

	client.expectClosed()
}

// TestHandshakeTokenRules checks the three token outcomes: a configured token
// needs a matching Authorization header, the query string never counts, and a
// missing header is refused before the socket is upgraded.
func TestHandshakeTokenRules(t *testing.T) {
	const token = "s3cret-token"
	ts := newTestHub(t, func(o *Options) { o.Token = token })

	t.Run("missing token", func(t *testing.T) {
		_, resp, err := ts.dialRaw(nil)
		if err == nil {
			t.Fatal("dial succeeded, want a refused handshake")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %v (%v), want %d", resp, err, http.StatusUnauthorized)
		}
	})

	t.Run("wrong token", func(t *testing.T) {
		_, resp, err := ts.dialRaw(func(o *websocket.DialOptions) {
			o.HTTPHeader = http.Header{"Authorization": {"Bearer nope"}}
		})
		if err == nil {
			t.Fatal("dial succeeded, want a refused handshake")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %v (%v), want %d", resp, err, http.StatusUnauthorized)
		}
	})

	t.Run("query string token is ignored", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		defer cancel()
		conn, resp, err := websocket.Dial(ctx, ts.wsURL()+"?token="+token, nil)
		if err == nil {
			_ = conn.CloseNow()
			t.Fatal("dial succeeded, want a refused handshake")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %v (%v), want %d", resp, err, http.StatusUnauthorized)
		}
	})

	t.Run("correct token", func(t *testing.T) {
		client := ts.dial(func(o *websocket.DialOptions) {
			o.HTTPHeader = http.Header{"Authorization": {"Bearer " + token}}
		})
		defer client.close()
		client.hello()
	})
}

// TestHandshakeOriginRules refuses a cross-authority Origin and accepts the ones
// the operator listed.
func TestHandshakeOriginRules(t *testing.T) {
	ts := newTestHub(t, func(o *Options) {
		o.AllowOrigins = []string{"http://localhost:5173"}
	})
	authority := strings.TrimPrefix(ts.srv.URL, "http://")

	t.Run("same authority", func(t *testing.T) {
		client := ts.dial(func(o *websocket.DialOptions) {
			o.HTTPHeader = http.Header{"Origin": {"http://" + authority}}
		})
		defer client.close()
		client.hello()
	})

	t.Run("foreign origin", func(t *testing.T) {
		_, resp, err := ts.dialRaw(func(o *websocket.DialOptions) {
			o.HTTPHeader = http.Header{"Origin": {"http://evil.example"}}
		})
		if err == nil {
			t.Fatal("dial succeeded, want a refused handshake")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %v (%v), want %d", resp, err, http.StatusUnauthorized)
		}
	})

	t.Run("allow-listed origin", func(t *testing.T) {
		client := ts.dial(func(o *websocket.DialOptions) {
			o.HTTPHeader = http.Header{"Origin": {"http://localhost:5173"}}
		})
		defer client.close()
		client.hello()
	})

	t.Run("null origin", func(t *testing.T) {
		_, resp, err := ts.dialRaw(func(o *websocket.DialOptions) {
			o.HTTPHeader = http.Header{"Origin": {"null"}}
		})
		if err == nil {
			t.Fatal("dial succeeded, want a refused handshake")
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status = %v (%v), want %d", resp, err, http.StatusUnauthorized)
		}
	})
}

// TestAuthorizeHostRules is the DNS-rebinding guard: the Host header has to name
// an authority this connection really serves.
func TestAuthorizeHostRules(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		local   string
		allow   []string
		wantErr bool
	}{
		{name: "loopback listener, same authority", host: "127.0.0.1:8787", local: "127.0.0.1:8787"},
		{name: "loopback listener, localhost name", host: "localhost:8787", local: "127.0.0.1:8787"},
		{name: "wildcard listener, loopback name", host: "localhost:8787", local: "0.0.0.0:8787"},
		{name: "wildcard listener, ipv6 loopback", host: "[::1]:8787", local: "0.0.0.0:8787"},
		{name: "wildcard listener, foreign name", host: "evil.example:8787", local: "0.0.0.0:8787", wantErr: true},
		{name: "loopback listener, foreign name", host: "evil.example:8787", local: "127.0.0.1:8787", wantErr: true},
		{name: "wrong port", host: "127.0.0.1:9999", local: "127.0.0.1:8787", wantErr: true},
		{name: "allow-listed name", host: "pi-ui.local:8787", local: "127.0.0.1:8787", allow: []string{"pi-ui.local"}},
		{name: "allow-listed authority", host: "pi-ui.local:8787", local: "127.0.0.1:8787", allow: []string{"pi-ui.local:8787"}},
		{name: "allow-listed but wrong port", host: "pi-ui.local:1234", local: "127.0.0.1:8787", allow: []string{"pi-ui.local:8787"}, wantErr: true},
		{name: "missing host", host: "", local: "127.0.0.1:8787", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := New(withDefaults(Options{Token: "t", AllowHosts: tc.allow})).(*hub)
			defer h.Close()

			err := h.authorizeHost(request(tc.host, "127.0.0.1:50000", tc.local, ""))
			if tc.wantErr && err == nil {
				t.Fatalf("Host %q against %s was accepted, want a refusal", tc.host, tc.local)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Host %q against %s was refused: %v", tc.host, tc.local, err)
			}
		})
	}
}

// TestAuthorizeTokenLoopbackRule pins the no-token deployment: only a loopback
// peer may connect, and a configured token is required from anyone.
func TestAuthorizeTokenLoopbackRule(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		remote  string
		header  string
		wantErr bool
	}{
		{name: "no token, loopback peer", remote: "127.0.0.1:50000"},
		{name: "no token, ipv6 loopback peer", remote: "[::1]:50000"},
		{name: "no token, remote peer", remote: "203.0.113.9:50000", wantErr: true},
		{name: "no token, supplied token is ignored", remote: "127.0.0.1:50000", header: "Bearer whatever"},
		{name: "token configured, matching header", token: "s3cret", remote: "203.0.113.9:50000", header: "Bearer s3cret"},
		{name: "token configured, bare value", token: "s3cret", remote: "203.0.113.9:50000", header: "s3cret"},
		{name: "token configured, missing header", token: "s3cret", remote: "203.0.113.9:50000", wantErr: true},
		{name: "token configured, wrong header", token: "s3cret", remote: "203.0.113.9:50000", header: "Bearer nope", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := New(withDefaults(Options{Token: tc.token})).(*hub)
			defer h.Close()

			r := request("127.0.0.1:8787", tc.remote, "127.0.0.1:8787", "")
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			err := h.authorizeToken(r)
			if tc.wantErr && err == nil {
				t.Fatal("peer was authorized, want a refusal")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("peer was refused: %v", err)
			}
		})
	}
}

// TestAuthorizeOriginAllowList checks the allow-list matching itself, including
// the authority-only spelling.
func TestAuthorizeOriginAllowList(t *testing.T) {
	tests := []struct {
		name    string
		allow   []string
		origin  string
		wantErr bool
	}{
		{name: "no origin", origin: ""},
		{name: "same authority", origin: "http://127.0.0.1:8787"},
		{name: "other authority", origin: "http://evil.example", wantErr: true},
		{name: "listed origin", allow: []string{"http://localhost:5173"}, origin: "http://localhost:5173"},
		{name: "listed authority", allow: []string{"localhost:5173"}, origin: "http://localhost:5173"},
		{name: "listed with a different scheme", allow: []string{"https://localhost:5173"}, origin: "http://localhost:5173", wantErr: true},
		{name: "malformed", origin: "//", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := New(withDefaults(Options{Token: "t", AllowOrigins: tc.allow})).(*hub)
			defer h.Close()

			err := h.authorizeOrigin(request("127.0.0.1:8787", "127.0.0.1:50000", "127.0.0.1:8787", tc.origin))
			if tc.wantErr && err == nil {
				t.Fatalf("Origin %q was accepted, want a refusal", tc.origin)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("Origin %q was refused: %v", tc.origin, err)
			}
		})
	}
}

// TestServeHTTPRejectsOtherRoutes keeps the hub from becoming a catch-all handler
// when it is mounted at the wrong place.
func TestServeHTTPRejectsOtherRoutes(t *testing.T) {
	h := New(withDefaults(Options{})).(*hub)
	defer h.Close()

	recorder := httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8787/nope", nil))
	if recorder.Code != http.StatusNotFound {
		t.Errorf("GET /nope = %d, want 404", recorder.Code)
	}

	recorder = httptest.NewRecorder()
	h.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8787"+path, nil))
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST %s = %d, want 405", path, recorder.Code)
	}
}
