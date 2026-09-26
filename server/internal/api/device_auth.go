package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/Nihmar/pi-ui/server/internal/auth"
)

// AuthService is the device-identity seam of the REST surface: what the pairing
// endpoints need from internal/auth, and nothing more. Tests fake it; the real
// implementation is *auth.Service.
type AuthService interface {
	// Pair exchanges a pairing code (with the QR secret) or the admin password for a
	// device token.
	Pair(req auth.PairRequest, callerKey string) (auth.PairResult, error)
	// Authenticate verifies a bearer token and returns the device behind it.
	Authenticate(token string) (auth.Device, error)
	// Refresh rotates the token of the device presenting it.
	Refresh(token string) (auth.PairResult, error)
	// Devices lists the active devices.
	Devices() []auth.Device
	// Revoke makes a device stop authenticating immediately.
	Revoke(deviceID string) error
	// Configured reports whether any identity exists yet (a paired device or an admin
	// password); an unconfigured server is the loopback bootstrap.
	Configured() bool
}

// DeviceAuthenticator authenticates bearer tokens against the auth service.
//
// The local fallback matches the rest of the server's trust model: a loopback peer
// without a token is a viewer, so the admin's own machine can read before it has
// paired, while only a token unlocks operator and admin.
type DeviceAuthenticator struct {
	Service AuthService
}

// Authenticate implements Authenticator.
func (d DeviceAuthenticator) Authenticate(r *http.Request) (Scope, error) {
	scope, _, err := d.AuthenticateDevice(r)
	return scope, err
}

// AuthenticateDevice is Authenticate plus the device id, so a handler can mark the
// caller in the device list without a second verification.
func (d DeviceAuthenticator) AuthenticateDevice(r *http.Request) (Scope, string, error) {
	if d.Service == nil {
		return "", "", errors.New("the server was wired without an auth service")
	}
	token, ok := bearerToken(r)
	if !ok {
		if IsLoopback(r) {
			// The bootstrap state of the plan: no device and no password yet, so a local
			// peer is the operator and can set one up. With an identity configured, a
			// local peer without a token is a viewer.
			if !d.Service.Configured() {
				return ScopeOperator, "", nil
			}
			return ScopeViewer, "", nil
		}
		return "", "", errors.New("a device token is required for requests from outside the loopback interface")
	}
	device, err := d.Service.Authenticate(token)
	if err != nil {
		return "", "", err
	}
	if !device.Scope.Valid() {
		return "", "", errors.New("the device carries an unknown scope")
	}
	return Scope(device.Scope), device.ID, nil
}

// deviceAuthenticator is the optional extension of Authenticator: an authenticator
// that can name the device it authenticated.
type deviceAuthenticator interface {
	AuthenticateDevice(r *http.Request) (Scope, string, error)
}

// authenticate resolves the scope and, when the seam provides it, the device id.
func authenticate(a Authenticator, r *http.Request) (Scope, string, error) {
	if extended, ok := a.(deviceAuthenticator); ok {
		return extended.AuthenticateDevice(r)
	}
	scope, err := a.Authenticate(r)
	return scope, "", err
}

// deviceKey is the context key of the authenticated device id.
type deviceKey struct{}

// withDevice attaches the device id to a request context.
func withDevice(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, deviceKey{}, id)
}

// deviceFrom returns the device id the request was authenticated with, or "" when
// the authenticator does not know one (the loopback fallback).
func deviceFrom(ctx context.Context) string {
	id, _ := ctx.Value(deviceKey{}).(string)
	return id
}
