package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"io"
	"math/big"
	"sync"
	"time"
)

// InviteKind selects what a pairing invitation is made of.
type InviteKind string

const (
	// InviteQR carries a code plus a high-entropy secret; the client must send both,
	// so a shoulder-surfer who reads the six digits off the screen cannot pair.
	InviteQR InviteKind = "qr"

	// InviteTyped carries only the code: the fallback for a device without a
	// camera, kept safe by the pairing rate limit and lockout.
	InviteTyped InviteKind = "typed"
)

// Invite is a pending pairing invitation. The QR payload of docs/api-v1.md carries
// the code and the secret (when present) plus the server origin and fingerprint.
type Invite struct {
	// Kind is how the invitation was produced.
	Kind InviteKind
	// Code is the short human-typable code, shown by `pi-ui status`.
	Code string
	// Secret is the QR-only high-entropy secret; empty for InviteTyped.
	Secret string
	// ExpiresAt is when the invitation stops being usable.
	ExpiresAt time.Time
}

// inviteCodeDigits is the length of the typable pairing code.
const inviteCodeDigits = 6

// inviteStore keeps the pending invitations. They are process-local on purpose: an
// invitation that survives a restart is a credential nobody remembers creating.
type inviteStore struct {
	mu      sync.Mutex
	invites []Invite
}

// put records one invitation.
func (s *inviteStore) put(invite Invite) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invites = append(s.invites, invite)
}

// take consumes the invitation matching both the code and the secret, dropping
// expired ones on the way. The comparisons are constant-time across every pending
// invitation, so the timing of a failed attempt does not reveal whether a code
// exists; a wrong secret leaves the invitation alive, because burning it would let
// anyone who read the digits cancel the pairing.
func (s *inviteStore) take(code, secret string, now time.Time) (Invite, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	kept := s.invites[:0]
	var found Invite
	matched := false
	for _, invite := range s.invites {
		if !now.Before(invite.ExpiresAt) {
			continue
		}
		codeMatches := subtle.ConstantTimeCompare([]byte(invite.Code), []byte(code)) == 1
		secretMatches := subtle.ConstantTimeCompare([]byte(invite.Secret), []byte(secret)) == 1
		if !matched && codeMatches && secretMatches {
			found = invite
			matched = true
			continue // consumed: not kept
		}
		kept = append(kept, invite)
	}
	s.invites = kept
	return found, matched
}

// len reports the pending invitation count, for tests and diagnostics.
func (s *inviteStore) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.invites)
}

// newInviteCode returns a zero-padded code of inviteCodeDigits digits.
func newInviteCode(random io.Reader) (string, error) {
	limit := big.NewInt(1)
	for range inviteCodeDigits {
		limit.Mul(limit, big.NewInt(10))
	}
	value, err := rand.Int(random, limit)
	if err != nil {
		return "", fmt.Errorf("auth: pairing code: %w", err)
	}
	return fmt.Sprintf("%0*d", inviteCodeDigits, value), nil
}
