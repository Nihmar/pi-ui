package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync"
	"time"
)

// inviteAlphabet is Crockford base32: the ten digits and the letters with I, L, O
// and U removed, so a code read off a screen or typed by hand cannot be confused
// with another. Codes are generated and displayed in this alphabet, uppercase.
const inviteAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// inviteCodeLength is the number of symbols in a pairing code. 32^6 ≈ 2^30, which
// with the pairing rate limit and the ten-minute TTL makes guessing infeasible.
const inviteCodeLength = 6

// Invite is a pending pairing invitation. It is one short code and nothing else:
// after unifying pairing on a single invite kind the code is the credential the
// client presents (the QR carries the same value the user can type), so it is
// single-use, expires with the invitation and is rate-limited and audited.
type Invite struct {
	// Code is the short human-typable Crockford base32 code, shown by `pi-ui status`.
	Code string
	// ExpiresAt is when the invitation stops being usable.
	ExpiresAt time.Time
}

// InviteStore keeps the pending pairing invitations. The seam exists because the
// invitations must be shared between processes: `pi-ui pair` mints one, the running
// `pi-ui serve` consumes it, and only the store both open can make that work. The
// memory implementation ships for tests and for a server whose invitations die with
// it; the SQLite implementation persists them behind the same interface.
type InviteStore interface {
	// Save records one invitation, replacing an invitation with the same code.
	Save(invite Invite) error
	// Take removes and returns the invitation matching the code, dropping expired
	// ones. The code is normalized and compared in constant time over fixed-length
	// values, so neither the length of a guess nor which invitation exists leaks.
	// A wrong code leaves the invitation alive: burning it would let anyone who read
	// the code cancel the pairing.
	Take(code string, now time.Time) (Invite, bool)
	// Pending counts the unexpired invitations.
	Pending(now time.Time) int
}

// MemoryInviteStore is the in-process InviteStore.
type MemoryInviteStore struct {
	mu      sync.Mutex
	invites []Invite
}

// NewMemoryInviteStore returns an empty in-process store.
func NewMemoryInviteStore() *MemoryInviteStore { return &MemoryInviteStore{} }

// Save implements InviteStore.
func (s *MemoryInviteStore) Save(invite Invite) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invites = append(s.invites, invite)
	return nil
}

// Take implements InviteStore with constant-time comparisons across every pending
// invitation, so the timing of a failed attempt does not reveal whether a code
// exists.
func (s *MemoryInviteStore) Take(code string, now time.Time) (Invite, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	want := NormalizeCode(code)
	kept := s.invites[:0]
	var found Invite
	matched := false
	for _, invite := range s.invites {
		if !now.Before(invite.ExpiresAt) {
			continue
		}
		if !matched && constantTimeCodeEqual(invite.Code, want) {
			found = invite
			matched = true
			continue // consumed: not kept
		}
		kept = append(kept, invite)
	}
	s.invites = kept
	return found, matched
}

// Pending implements InviteStore.
func (s *MemoryInviteStore) Pending(now time.Time) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, invite := range s.invites {
		if now.Before(invite.ExpiresAt) {
			count++
		}
	}
	return count
}

// NormalizeCode canonicalizes a pairing code from the wire or from what a user
// typed, so equal codes compare equal: it trims surrounding whitespace, uppercases
// the input, drops the separators people type between symbols ('-', '_' and spaces)
// and folds the letters Crockford omits onto the digits they are mistaken for
// (O→0, I→1, L→1). The generated code is already normalized; a client sends the raw
// value and the server applies this before any comparison.
func NormalizeCode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		switch r {
		case '-', '_', ' ':
			continue
		case 'O':
			b.WriteByte('0')
		case 'I', 'L':
			b.WriteByte('1')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// constantTimeCodeEqual compares a stored invitation code with an already-normalized
// candidate in constant time. Both sides are copied into buffers of the same fixed
// length first, so a short guess is padded and compared for the full length instead of
// making the underlying compare return early on a length mismatch.
func constantTimeCodeEqual(stored, candidate string) bool {
	var a, b [inviteCodeLength]byte
	copy(a[:], stored)
	copy(b[:], candidate)
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

// newInviteCode returns a uniformly random code of inviteCodeLength Crockford base32
// symbols, drawn from crypto/rand so its entropy is the full 2^30.
func newInviteCode(random io.Reader) (string, error) {
	limit := big.NewInt(int64(len(inviteAlphabet)))
	code := make([]byte, inviteCodeLength)
	for i := range code {
		value, err := rand.Int(random, limit)
		if err != nil {
			return "", fmt.Errorf("auth: pairing code: %w", err)
		}
		code[i] = inviteAlphabet[value.Int64()]
	}
	return string(code), nil
}
