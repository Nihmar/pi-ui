package tls

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"time"
)

// FingerprintURL fetches the certificate a server serves and describes it.
//
// It connects for the handshake only: the connection is closed before any request is
// written, because the point is the certificate and not the server's content. A
// certificate that does not validate is not an error here — a self-signed one is exactly
// what a user wants to look at — so verification is skipped on purpose, and the value
// printed is the one to compare with what the server reported.
func FingerprintURL(raw string) (Info, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return Info{}, fmt.Errorf("tls: %q is not a URL", raw)
	}
	host := parsed.Host
	if parsed.Port() == "" {
		host = parsed.Hostname() + ":443"
	}
	dialer := &tls.Dialer{Config: &tls.Config{
		InsecureSkipVerify: true, // the fingerprint is compared by the user, not by the store
		MinVersion:         tls.VersionTLS12,
	}}
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()

	// Dialing already performs the handshake; the state is read right after it.
	connection, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return Info{}, fmt.Errorf("tls: %s: %w", host, err)
	}
	defer func() { _ = connection.Close() }()

	state := connection.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Info{}, fmt.Errorf("tls: %s served no certificate", host)
	}
	return Describe(state.PeerCertificates[0]), nil
}

// dialTimeout bounds one handshake: a server that answers nothing must not hold a
// terminal.
const dialTimeout = 5 * time.Second

// Grouped formats a fingerprint the way a user compares it by eye: pairs of hex digits
// separated by colons, upper case.
func Grouped(fingerprint string) string {
	grouped := make([]byte, 0, len(fingerprint)+len(fingerprint)/2)
	for index := 0; index < len(fingerprint); index += 2 {
		if index > 0 {
			grouped = append(grouped, ':')
		}
		end := index + 2
		if end > len(fingerprint) {
			end = len(fingerprint)
		}
		grouped = append(grouped, []byte(fingerprint[index:end])...)
	}
	return string(bytesToUpper(grouped))
}

// bytesToUpper upper-cases ASCII without pulling in strings for one call.
func bytesToUpper(in []byte) []byte {
	out := make([]byte, len(in))
	for index, char := range in {
		if char >= 'a' && char <= 'z' {
			char -= 'a' - 'A'
		}
		out[index] = char
	}
	return out
}
