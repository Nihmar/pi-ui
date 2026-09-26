package api

import (
	"crypto/subtle"
	"errors"
	"net"
	"net/http"
	"strings"
)

// LoopbackOrToken is the only Authenticator the spike ships (§5.4): a request is trusted
// when it carries the configured bearer token, and a request from the loopback interface is
// trusted without one.
//
// Scopes follow from that, in the one order that keeps a local server usable and a shared
// server safe:
//
//   - a valid token is the operator credential: it grants ScopeOperator from anywhere;
//   - a loopback peer grants ScopeOperator when no token is configured (single-user local
//     mode, the default of `pi-ui serve`);
//   - a loopback peer grants only ScopeViewer when a token is configured, so a stray local
//     process can read the session list but cannot stop sessions or spawn children;
//   - anything else is unauthorized.
type LoopbackOrToken struct {
	token string
}

// NewLoopbackOrToken builds the spike authenticator. An empty token means no token is
// configured.
func NewLoopbackOrToken(token string) *LoopbackOrToken {
	return &LoopbackOrToken{token: token}
}

// Authenticate implements Authenticator.
func (a *LoopbackOrToken) Authenticate(r *http.Request) (Scope, error) {
	if a.token != "" {
		if token, ok := bearerToken(r); ok && subtle.ConstantTimeCompare([]byte(token), []byte(a.token)) == 1 {
			return ScopeOperator, nil
		}
		// A wrong token is a wrong token, wherever it comes from: do not silently
		// downgrade an explicit credential to a viewer.
		if _, ok := bearerToken(r); ok {
			return "", errors.New("the bearer token is not valid")
		}
	}
	if !IsLoopback(r) {
		if a.token == "" {
			return "", errors.New("the server is loopback-only: send a bearer token to use it remotely")
		}
		return "", errors.New("a bearer token is required for requests from outside the loopback interface")
	}
	if a.token == "" {
		return ScopeOperator, nil
	}
	return ScopeViewer, nil
}

// IsLoopback reports whether the request's peer address is the loopback interface. It reads
// the transport address, never a forwarded header: a proxy in front of the server must not
// be able to claim to be local.
func IsLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// bearerToken extracts the Authorization bearer token, if the header uses that scheme.
func bearerToken(r *http.Request) (string, bool) {
	header := r.Header.Get("Authorization")
	if header == "" {
		return "", false
	}
	scheme, value, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token := strings.TrimSpace(value)
	if token == "" {
		return "", false
	}
	return token, true
}

// denied is the Authenticator used when Options.Auth is nil. It fails closed.
type denied struct{}

// Authenticate implements Authenticator.
func (denied) Authenticate(*http.Request) (Scope, error) {
	return "", errors.New("the server was wired without an Authenticator")
}
