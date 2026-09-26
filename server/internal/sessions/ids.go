package sessions

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// Session ids are "s_" + 16 lowercase hex characters from crypto/rand
// (docs/spike-interfaces.md §5.7). 8 bytes of entropy make a collision within one server
// process impossible in practice and keep the id short enough to read in a log line.
const (
	sessionIDPrefix = "s_"
	sessionIDBytes  = 8
)

// newSessionID returns one session id, or the reason the entropy source failed.
func newSessionID() (string, error) {
	var buf [sessionIDBytes]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("sessions: session id: %w", err)
	}
	return sessionIDPrefix + hex.EncodeToString(buf[:]), nil
}
