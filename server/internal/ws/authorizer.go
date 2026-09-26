package ws

import "net/http"

// Scope is what a WebSocket connection may do. It mirrors the REST scopes
// (docs/api-v1.md, "Scopes") and travels from the hub's Authorizer into every frame
// decision, so both transports answer the same question with the same answer.
type Scope string

const (
	// ScopeViewer reads: subscribe, replay, receive events, ping.
	ScopeViewer Scope = "viewer"
	// ScopeOperator drives sessions: viewer plus `command` and `ui_response`.
	ScopeOperator Scope = "operator"
	// ScopeAdmin is accepted and stored, but no WebSocket frame needs it yet; it is
	// what the connection carries so a future admin frame does not have to change the
	// handshake.
	ScopeAdmin Scope = "admin"
)

// valid reports whether the scope is one of the three the taxonomy defines.
func (s Scope) valid() bool {
	switch s {
	case ScopeViewer, ScopeOperator, ScopeAdmin:
		return true
	default:
		return false
	}
}

// allows reports whether s satisfies required: operator implies viewer, admin implies
// both, and an unknown scope allows nothing.
func (s Scope) allows(required Scope) bool {
	return rank(s) >= rank(required)
}

// rank orders the scopes.
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

// Authorizer decides who may open a /ws/v1 connection and with which scope. It is the
// seam the server fills with its device authenticator, so REST and WebSocket make the
// same decision about the same request; when it is nil the hub keeps the static-token
// rules of Options.Token.
type Authorizer interface {
	// Authorize returns the scope of the connection and, when the credential names a
	// device, its id (which is what a revocation closes by). An error refuses the
	// handshake with 401 before the upgrade.
	Authorize(r *http.Request) (scope Scope, deviceID string, err error)
}

// DeviceCloser is the optional hub extension that closes the connections of one
// device. The auth service's revoke hook calls it, so revoking a device from any
// client ends its sockets immediately instead of leaving them until the token is used
// again.
type DeviceCloser interface {
	// CloseDevice closes every connection authenticated as deviceID and returns how
	// many it closed. An empty id closes nothing.
	CloseDevice(deviceID string) int
}

// principal is the identity a connection was accepted with. It is fixed for the life
// of the connection: a scope change means a new token and a new handshake.
type principal struct {
	scope    Scope
	deviceID string
}

var _ DeviceCloser = (*hub)(nil)
