package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"fmt"
	"io"

	"golang.org/x/crypto/argon2"
)

// Argon2Params is the cost of the key derivation behind a stored credential. It is
// part of the stored record, so raising the cost later never invalidates existing
// devices: a record is verified with the parameters it was written with.
type Argon2Params struct {
	// Time is the number of passes.
	Time uint32
	// Memory is the memory in KiB.
	Memory uint32
	// Threads is the degree of parallelism.
	Threads uint8
	// KeyLen is the derived key length in bytes.
	KeyLen uint32
}

// saltLen is the per-credential salt length.
const saltLen = 16

// DefaultArgon2Params is the cost of a production verifier: one pass over 64 MiB,
// which is fast enough for an authenticated request and expensive enough to make an
// offline guess of a stolen hash unattractive. Tests inject a cheaper setting.
func DefaultArgon2Params() Argon2Params {
	return Argon2Params{Time: 1, Memory: 64 * 1024, Threads: 2, KeyLen: 32}
}

// validate rejects a parameter set that would make argon2 panic or produce a
// useless key.
func (p Argon2Params) validate() error {
	switch {
	case p.Time < 1:
		return fmt.Errorf("auth: argon2 time must be >= 1, got %d", p.Time)
	case p.Memory < 8:
		return fmt.Errorf("auth: argon2 memory must be >= 8 KiB, got %d", p.Memory)
	case p.Threads < 1:
		return fmt.Errorf("auth: argon2 threads must be >= 1, got %d", p.Threads)
	case p.KeyLen < 16:
		return fmt.Errorf("auth: argon2 key length must be >= 16, got %d", p.KeyLen)
	}
	return nil
}

// derive runs argon2id over one credential with its salt.
func (p Argon2Params) derive(credential, salt []byte) ([]byte, error) {
	if err := p.validate(); err != nil {
		return nil, err
	}
	return argon2.IDKey(credential, salt, p.Time, p.Memory, p.Threads, p.KeyLen), nil
}

// hashCredential derives and salts one credential (a token secret or the admin
// password). The salt comes from the injected randomness so tests can pin it.
func hashCredential(credential []byte, params Argon2Params, random io.Reader) (salt, hash []byte, err error) {
	salt = make([]byte, saltLen)
	if _, err := io.ReadFull(random, salt); err != nil {
		return nil, nil, fmt.Errorf("auth: credential salt: %w", err)
	}
	hash, err = params.derive(credential, salt)
	if err != nil {
		return nil, nil, err
	}
	return salt, hash, nil
}

// verifyCredential reports whether credential matches a stored salt/hash pair,
// comparing in constant time so a wrong secret cannot be found byte by byte.
func verifyCredential(credential, salt, hash []byte, params Argon2Params) bool {
	derived, err := params.derive(credential, salt)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(derived, hash) == 1
}

// cryptoRandom is the production randomness of the package.
var cryptoRandom io.Reader = rand.Reader
