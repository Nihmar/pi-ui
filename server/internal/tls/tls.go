// Package tls reads the operator's certificate and describes it to clients.
//
// The server terminates TLS itself when it is given a certificate and a key; a reverse
// proxy that terminates it is equally supported, and then the fingerprint is the proxy's
// certificate, reported the same way. What this package adds is the fact a client needs
// to pin: the SHA-256 of the leaf, lowercase hex, which is what `GET /server` and the
// pairing answer carry (schemas/server.json, `SrvTlsInfo`).
package tls

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

// Info is what a client learns about the certificate in use.
type Info struct {
	// FingerprintSHA256 is the lowercase hex digest of the leaf's DER bytes.
	FingerprintSHA256 string
	// NotAfter is when the leaf expires.
	NotAfter time.Time
	// Subject is the leaf's subject, for display only.
	Subject string
}

// Load reads a certificate and a key and describes the leaf.
//
// It fails loudly: a server that was told to speak TLS and cannot is a configuration
// error, and starting in plain HTTP instead would silently drop the pin the client
// expects.
func Load(certFile, keyFile string) (tls.Certificate, Info, error) {
	certificate, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, Info{}, fmt.Errorf("tls: %s / %s: %w", certFile, keyFile, err)
	}
	leaf, err := leafOf(certificate, certFile)
	if err != nil {
		return tls.Certificate{}, Info{}, err
	}
	certificate.Leaf = leaf
	return certificate, Describe(leaf), nil
}

// Describe reads the facts of one certificate.
func Describe(leaf *x509.Certificate) Info {
	return Info{
		FingerprintSHA256: Fingerprint(leaf),
		NotAfter:          leaf.NotAfter.UTC(),
		Subject:           leaf.Subject.String(),
	}
}

// Fingerprint is the lowercase hex SHA-256 of the leaf's DER bytes: the value the app
// compares, and the only part of a self-signed certificate a user can actually verify.
func Fingerprint(leaf *x509.Certificate) string {
	sum := sha256.Sum256(leaf.Raw)
	return hex.EncodeToString(sum[:])
}

// FingerprintFile reads one certificate file and returns its fingerprint, so `pi-ui tls
// fingerprint` and the running server agree on the value a user compares by eye.
func FingerprintFile(path string) (Info, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Info{}, fmt.Errorf("tls: %s: %w", path, err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return Info{}, fmt.Errorf("tls: %s is not PEM", path)
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return Info{}, fmt.Errorf("tls: %s: %w", path, err)
	}
	return Describe(leaf), nil
}

// leafOf returns the certificate's leaf, parsing the first certificate when the pair was
// loaded without one (tls.LoadX509KeyPair leaves Leaf nil).
func leafOf(certificate tls.Certificate, certFile string) (*x509.Certificate, error) {
	if certificate.Leaf != nil {
		return certificate.Leaf, nil
	}
	if len(certificate.Certificate) == 0 {
		return nil, errors.New("tls: the certificate file holds no certificate")
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil {
		return nil, fmt.Errorf("tls: %s: %w", certFile, err)
	}
	return leaf, nil
}

// selfSignedTemplate is the shape of the certificate a test needs: a leaf that is valid
// for loopback and for a hostname, with no CA to verify it against.
func selfSignedTemplate() *x509.Certificate {
	return &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "pi-ui.test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"pi-ui.test", "localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
}
