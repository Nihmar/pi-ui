package api

import (
	"net/http"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// Protocol is the value of the X-Piui-Protocol header on every response (§5.4) and the
// protocol number GET /api/v1/server reports.
const Protocol = 1

// protocolHeader names the wire version the client is talking to.
const protocolHeader = "X-Piui-Protocol"

// Options wires the router (docs/spike-interfaces.md §5.4). All fields except Hub are
// required; a nil Hub simply makes the router skip the /ws/v1 mount, which lets a REST
// test build one without a websocket.
type Options struct {
	Supervisor sessions.Supervisor
	Hub        ws.Hub
	Info       ServerInfo // version, piVersion, features, limits
	Auth       Authenticator
	// AuthService wires the device-identity endpoints (pair, refresh, devices,
	// revoke). When Auth is nil and this is set, the router authenticates bearer
	// tokens with it; when both are nil the router fails closed.
	AuthService AuthService
}

// Authenticator decides who is talking and with which scope: the Phase 3 seam behind which
// pairing, device tokens and admin accounts land.
type Authenticator interface {
	Authenticate(r *http.Request) (Scope, error)
}

// Scope is what an authenticated request may do.
type Scope string

const (
	ScopeViewer   Scope = "viewer"
	ScopeOperator Scope = "operator"
	ScopeAdmin    Scope = "admin"
)

// allows reports whether s satisfies required: operator implies viewer, admin implies both.
func (s Scope) allows(required Scope) bool { return rank(s) >= rank(required) }

// AllowsWrite reports whether the scope may change server or session state.
func (s Scope) AllowsWrite() bool { return s.allows(ScopeOperator) }

// rank orders the scopes. An unknown scope ranks 0, so a custom Authenticator cannot
// accidentally grant access by returning a typo.
func rank(s Scope) int {
	switch s {
	case ScopeViewer:
		return 1
	case ScopeOperator:
		return 2
	case ScopeAdmin:
		return 3
	default:
		return 0
	}
}

// ServerInfo is the body of GET /api/v1/server (§5.4).
type ServerInfo struct {
	Version   string         `json:"version"`
	PiVersion string         `json:"piVersion,omitempty"`
	Protocol  int            `json:"protocol"`
	Features  []string       `json:"features"`
	Limits    map[string]any `json:"limits"`
}

// NewRouter returns the handler that serves /api/v1 and, when a hub is wired, /ws/v1. It is
// mounted on an http.Server by cli/serve; nothing else in the process needs the mux.
//
// Every path has a JSON answer: a known path with the wrong method is 405 and an unknown
// path is 404, both in the {"error":{"code","message"}} envelope, so a client never has to
// parse a text/plain body from net/http.
func NewRouter(o Options) http.Handler {
	a := &api{
		supervisor:  o.Supervisor,
		hub:         o.Hub,
		info:        o.Info,
		auth:        o.Auth,
		authService: o.AuthService,
		pairSchema:  compilePairSchema(),
	}
	switch {
	case a.auth != nil:
		// The caller wired an explicit authenticator (the spike's LoopbackOrToken, or a
		// test fake).
	case a.authService != nil:
		a.auth = DeviceAuthenticator{Service: a.authService}
	default:
		// A caller that forgets both gets an explicit 401 instead of a nil panic on the
		// first request: the server is useful only with a decision about identity.
		a.auth = denied{}
	}

	mux := http.NewServeMux()

	// /api/v1/health is the liveness probe: it answers without a credential and exposes
	// nothing but "ok" (§5.4), which is what a container/systemd check needs.
	mux.HandleFunc("GET /api/v1/health", a.health)
	mux.HandleFunc("GET /api/v1/server", a.authorized(ScopeViewer, a.server))
	mux.HandleFunc("GET /api/v1/sessions", a.authorized(ScopeViewer, a.listSessions))
	mux.HandleFunc("POST /api/v1/sessions", a.authorized(ScopeOperator, a.createSession))
	mux.HandleFunc("GET /api/v1/sessions/{id}", a.authorized(ScopeViewer, a.getSession))
	mux.HandleFunc("POST /api/v1/sessions/{id}/stop", a.authorized(ScopeOperator, a.stopSession))

	// Device identity (docs/api-v1.md). Pairing takes no credential: the pairing
	// code or the admin password is the credential in the body. Everything else is
	// scoped, so a read-only device cannot list or revoke devices.
	mux.HandleFunc("POST /api/v1/auth/pair", a.pair)
	mux.HandleFunc("POST /api/v1/auth/refresh", a.authorized(ScopeViewer, a.refresh))
	mux.HandleFunc("GET /api/v1/auth/devices", a.authorized(ScopeAdmin, a.listDevices))
	mux.HandleFunc("DELETE /api/v1/auth/devices/{id}", a.authorized(ScopeAdmin, a.revokeDevice))

	// Method fallbacks: without them the mux answers 405/404 in text/plain.
	for _, path := range []string{
		"/api/v1/health",
		"/api/v1/server",
		"/api/v1/sessions",
		"/api/v1/sessions/{id}",
		"/api/v1/sessions/{id}/stop",
		"/api/v1/auth/pair",
		"/api/v1/auth/refresh",
		"/api/v1/auth/devices",
		"/api/v1/auth/devices/{id}",
	} {
		mux.HandleFunc(path, methodNotAllowed)
	}
	mux.HandleFunc("/", notFound)

	if o.Hub != nil {
		mux.Handle("/ws/v1", o.Hub)
	}

	return withProtocolHeader(mux)
}

// api holds the wired dependencies; every handler is a method on it, so adding an endpoint
// is one method plus one registration above.
type api struct {
	supervisor  sessions.Supervisor
	hub         ws.Hub
	info        ServerInfo
	auth        Authenticator
	authService AuthService
	pairSchema  *jsonschema.Schema
}

// authorized authenticates the request and enforces the required scope. The resolved scope
// travels in the request context for handlers that want to branch on it later.
func (a *api) authorized(required Scope, fn http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scope, deviceID, err := authenticate(a.auth, r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, codeOr(err, sessions.CodeUnauthorized), err.Error())
			return
		}
		if !scope.allows(required) {
			writeError(w, http.StatusForbidden, sessions.CodeForbiddenScope,
				"this endpoint needs the "+string(required)+" scope")
			return
		}
		ctx := withScope(r.Context(), scope)
		if deviceID != "" {
			ctx = withDevice(ctx, deviceID)
		}
		fn(w, r.WithContext(ctx))
	}
}

// withProtocolHeader stamps every answer, including the WS upgrade and the error
// envelopes, with the wire version (§5.4) and disables content sniffing.
func withProtocolHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set(protocolHeader, itoa(Protocol))
		w.Header().Set("X-Content-Type-Options", "nosniff")
		next.ServeHTTP(w, r)
	})
}

// methodNotAllowed answers a known path with an unsupported method.
func methodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusMethodNotAllowed, sessions.CodeBadRequest, "method not allowed for this endpoint")
}

// notFound answers every unmapped path, so even a typo comes back as JSON.
func notFound(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotFound, sessions.CodeNotFound, "no endpoint "+r.Method+" "+r.URL.Path)
}
