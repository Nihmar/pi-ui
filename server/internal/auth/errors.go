package auth

import (
	"errors"
	"fmt"
	"time"
)

// Sentinel failures of the package. The api layer maps them onto the wire
// taxonomy (unauthorized, rate_limited, not_found, device_limit); a caller matches
// them with errors.Is.
var (
	// ErrUnauthorized is the single answer to every credential failure: unknown,
	// expired or revoked token, unknown/expired/consumed invitation, wrong secret or
	// wrong password. The client learns nothing about which part was wrong.
	ErrUnauthorized = errors.New("auth: unauthorized")

	// ErrRateLimited means too many pairing or password attempts from one caller in
	// the window; RetryAfter says when to try again.
	ErrRateLimited = errors.New("auth: rate limited")

	// ErrDeviceLimit means the configured maximum of paired devices is reached;
	// revoke one before pairing another.
	ErrDeviceLimit = errors.New("auth: device limit reached")

	// ErrNotFound means the device id does not exist.
	ErrNotFound = errors.New("auth: no such device")

	// ErrNoPassword means the admin password was never set, so the password branch
	// cannot be used. It is reported to the operator (CLI/logs), never to a client.
	ErrNoPassword = errors.New("auth: no admin password is set")

	// ErrWeakPassword means a new admin password is shorter than MinPasswordLength.
	ErrWeakPassword = errors.New("auth: password is too short")
)

// RateLimitError is ErrRateLimited with the moment a new attempt becomes possible.
// The api layer turns RetryAfter into the Retry-After header.
type RateLimitError struct {
	// RetryAfter is how long the caller should wait.
	RetryAfter time.Duration
}

// Error implements error.
func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s: retry after %s", ErrRateLimited, e.RetryAfter)
}

// Unwrap exposes ErrRateLimited, so errors.Is(err, ErrRateLimited) holds.
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }

// unauthorized wraps ErrUnauthorized with a reason for the server log. The reason
// never reaches a client.
func unauthorized(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnauthorized, fmt.Sprintf(format, args...))
}
