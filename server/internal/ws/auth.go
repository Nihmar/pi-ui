package ws

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

// ServeHTTP implements http.Handler for the /ws/v1 endpoint: refuse the request,
// upgrade it, or hand the socket to serveConn.
//
// The refusals are plain HTTP responses, so a client learns the request was
// rejected before a socket exists; that is what keeps a bad token, a foreign Host
// and a foreign Origin indistinguishable to an attacker while staying diagnosable
// to the operator.
func (h *hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := h.authorize(r); err != nil {
		denyUnauthorized(w, err)
		return
	}

	// The hub enforces Host and Origin itself, allow-lists included, so the
	// library's built-in same-origin check is disabled instead of being duplicated
	// with subtly different rules.
	wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return // Accept already wrote the refusal
	}
	wsConn.SetReadLimit(maxInboundFrameBytes)
	h.serveConn(wsConn)
}

// authorize applies the handshake rules of docs/spike-interfaces.md §5.2.
func (h *hub) authorize(r *http.Request) error {
	if err := h.authorizeHost(r); err != nil {
		return err
	}
	if err := h.authorizeOrigin(r); err != nil {
		return err
	}
	return h.authorizeToken(r)
}

// denyUnauthorized refuses a handshake before the upgrade.
//
// The refusal is a plain 401: HTTP status codes are three digits by definition
// (RFC 9110), so the 4401 of docs/spike-interfaces.md §5.2 cannot be sent as a
// status — Go's net/http panics on it and a Go client rejects the response as
// malformed. 4401 stays the close status for a failure that happens after the
// upgrade (a missing or unsupported hello), where the same number is legal, and the
// body keeps the §11 error shape so the reason is machine-readable either way.
func denyUnauthorized(w http.ResponseWriter, reason error) {
	body := mustMarshal(struct {
		Error outError `json:"error"`
	}{Error: outError{Code: codeUnauthorized, Message: reason.Error()}})

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write(body)
}

// authorizeToken checks the bearer token, or the peer address when the server has
// no token.
//
// The token is read from the Authorization header only. A token in the query
// string never counts: URLs end up in logs, shell history and Referer headers, so
// accepting one would turn an accidental leak into a working credential.
func (h *hub) authorizeToken(r *http.Request) error {
	if h.opts.Token == "" {
		host, _ := splitHostPort(r.RemoteAddr)
		if !isLoopbackHost(host) {
			return fmt.Errorf("ws: no token configured, so only loopback clients may connect (peer %s)", r.RemoteAddr)
		}
		return nil
	}
	got := bearerToken(r)
	if subtle.ConstantTimeCompare([]byte(got), []byte(h.opts.Token)) != 1 {
		return errors.New("ws: invalid or missing bearer token")
	}
	return nil
}

// bearerToken extracts the credential from the Authorization header, accepting
// "Bearer <token>" (case-insensitive) and a bare token value.
func bearerToken(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get("Authorization"))
	if raw == "" {
		return ""
	}
	if len(raw) > 7 && strings.EqualFold(raw[:7], "bearer ") {
		return strings.TrimSpace(raw[7:])
	}
	return raw
}

// authorizeHost pins the Host header to an authority the server actually serves.
//
// This is the DNS-rebinding guard: a page on evil.example that resolves to this
// server sends Host: evil.example, and without the check the browser would be
// talking to a loopback server it should not reach. Accepted values are the
// configured allow-list, the authority the connection really arrived on, and a
// loopback name on the listening port when the server is bound to a wildcard
// address; every other name has to be listed explicitly in Options.AllowHosts.
func (h *hub) authorizeHost(r *http.Request) error {
	if r.Host == "" {
		return errors.New("ws: missing Host header")
	}
	if allowsAuthority(h.opts.AllowHosts, r.Host) {
		return nil
	}

	host, port := splitHostPort(r.Host)
	local, _ := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if local == nil {
		// No listener information (an in-process handler, or a transport that does
		// not set it): fall back to the only authority that is safe without one.
		if isLoopbackHost(host) {
			return nil
		}
		return fmt.Errorf("ws: Host %q is not allowed and no local authority is known", r.Host)
	}
	localHost, localPort := splitHostPort(local.String())

	switch {
	case strings.EqualFold(host, localHost) && (port == "" || localPort == "" || port == localPort):
		return nil
	case isWildcardHost(localHost) && port == localPort && isLoopbackHost(host):
		// Bound to every interface: a client may legitimately reach it as
		// 127.0.0.1 or localhost on the same port, but a name still needs the
		// allow-list, because a name is exactly what a rebinding attack controls.
		return nil
	case isLoopbackHost(localHost) && isLoopbackHost(host) && port == localPort:
		return nil
	}
	return fmt.Errorf("ws: Host %q is not the authority this connection arrived on (%s); add it to Options.AllowHosts to serve it", r.Host, local)
}

// authorizeOrigin refuses a cross-authority Origin unless it is allow-listed.
//
// A missing Origin is accepted: browsers always send one for WebSocket
// handshakes, native clients and the test harness do not, and rejecting them
// would break the Flutter client without adding protection. The comparison is
// against the request authority, so an allow-list is only needed for a front end
// served from a different host (a Vite dev server, for example).
func (h *hub) authorizeOrigin(r *http.Request) error {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return nil
	}
	if strings.EqualFold(origin, "null") {
		// "null" means a sandboxed or file:// context: it carries no authority to
		// compare, and accepting it would accept every opaque origin.
		return errors.New(`ws: Origin "null" is not accepted`)
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("ws: malformed Origin %q", origin)
	}
	if sameAuthority(parsed.Host, r.Host) {
		return nil
	}
	if allowsOrigin(h.opts.AllowOrigins, origin, parsed.Host) {
		return nil
	}
	return fmt.Errorf("ws: Origin %q is not the request authority %q; add it to Options.AllowOrigins to serve it", origin, r.Host)
}

// splitHostPort splits "host:port", "[::1]:port" and a bare host, tolerating the
// missing port; IPv6 brackets are stripped so two spellings of one address
// compare equal.
func splitHostPort(authority string) (host, port string) {
	authority = strings.TrimSpace(authority)
	if authority == "" {
		return "", ""
	}
	if h, p, err := net.SplitHostPort(authority); err == nil {
		return strings.Trim(h, "[]"), p
	}
	return strings.Trim(authority, "[]"), ""
}

// sameAuthority compares two authorities case-insensitively and by port: a missing port
// matches only a missing port. Treating one as a wildcard would accept `http://localhost`
// (the default port, written by omission) for `localhost:8787`, which is a different
// origin; a proxy that strips the port leaves both sides portless, so it still matches.
func sameAuthority(a, b string) bool {
	aHost, aPort := splitHostPort(a)
	bHost, bPort := splitHostPort(b)
	return strings.EqualFold(aHost, bHost) && aPort == bPort
}

// isLoopbackHost reports whether host names the local machine: "localhost", an
// IPv4 loopback address or ::1.
func isLoopbackHost(host string) bool {
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// isWildcardHost reports whether a listen address accepts every interface.
func isWildcardHost(host string) bool {
	switch host {
	case "", "0.0.0.0", "::":
		return true
	}
	return false
}

// allowsAuthority reports whether authority is allow-listed. An entry without a
// port matches every port of that host, so "pi-ui.local" covers
// "pi-ui.local:8787".
func allowsAuthority(allow []string, authority string) bool {
	host, port := splitHostPort(authority)
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, strings.TrimSpace(authority)) {
			return true
		}
		entryHost, entryPort := splitHostPort(entry)
		if !strings.EqualFold(entryHost, host) {
			continue
		}
		if entryPort == "" || entryPort == port {
			return true
		}
	}
	return false
}

// allowsOrigin reports whether origin is allow-listed, either as a complete
// origin ("http://localhost:5173") or as a bare authority.
func allowsOrigin(allow []string, origin, authority string) bool {
	for _, entry := range allow {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.EqualFold(entry, origin) {
			return true
		}
		if !strings.Contains(entry, "://") && allowsAuthority([]string{entry}, authority) {
			return true
		}
	}
	return false
}
