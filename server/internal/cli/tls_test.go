package cli

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pitui "github.com/Nihmar/pi-ui/server/internal/tls"
)

// TestServeTerminatesTLSAndReportsThePin runs the real server with a self-signed
// certificate and asks it, over TLS, which certificate a client should pin.
func TestServeTerminatesTLSAndReportsThePin(t *testing.T) {
	certPath, keyPath, leaf := selfSignedPair(t)
	stateDir := t.TempDir()

	address, stop := startServe(t, []string{
		"--addr", "127.0.0.1:0",
		"--pi", "/bin/echo",
		"--state-dir", stateDir,
		"--tls-cert", certPath,
		"--tls-key", keyPath,
	})
	defer stop()

	// The certificate is self-signed, so the client has to accept it — which is exactly
	// what a user does once, by comparing the fingerprint.
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{
		RootCAs:    pool,
		MinVersion: tls.VersionTLS12,
	}}}

	response, err := client.Get("https://" + address + "/api/v1/server")
	if err != nil {
		t.Fatalf("https request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}

	var identity struct {
		Version string `json:"version"`
		TLS     *struct {
			FingerprintSha256 string `json:"fingerprintSha256"`
			NotAfter          string `json:"notAfter"`
			Subject           string `json:"subject"`
		} `json:"tls"`
	}
	if err := json.Unmarshal(body, &identity); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	if identity.TLS == nil {
		t.Fatalf("a TLS server must describe its certificate: %s", body)
	}
	if identity.TLS.FingerprintSha256 != pitui.Fingerprint(leaf) {
		t.Fatalf("fingerprint = %q", identity.TLS.FingerprintSha256)
	}
	if identity.TLS.Subject == "" || identity.TLS.NotAfter == "" {
		t.Fatalf("tls = %+v", identity.TLS)
	}
	if !strings.Contains(identity.TLS.FingerprintSha256, "") {
		t.Fatal("unreachable")
	}
}

// TestTLSWantsBothHalves keeps a half-configured server from starting in plain HTTP.
func TestTLSWantsBothHalves(t *testing.T) {
	certPath, _, _ := selfSignedPair(t)
	if err := runServe(t, []string{
		"--addr", "127.0.0.1:0",
		"--pi", "/bin/echo",
		"--state-dir", t.TempDir(),
		"--tls-cert", certPath,
	}); err == nil {
		t.Fatal("a certificate without its key must be refused")
	}

	junk := filepath.Join(t.TempDir(), "junk.pem")
	if err := os.WriteFile(junk, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := runServe(t, []string{
		"--addr", "127.0.0.1:0",
		"--pi", "/bin/echo",
		"--state-dir", t.TempDir(),
		"--tls-cert", junk,
		"--tls-key", junk,
	}); err == nil {
		t.Fatal("an unreadable certificate must be refused")
	}
}

// TestAllowIPsRefusesAnOutsidePeer proves the front door over a real listener.
func TestAllowIPsRefusesAnOutsidePeer(t *testing.T) {
	address, stop := startServe(t, []string{
		"--addr", "127.0.0.1:0",
		"--pi", "/bin/echo",
		"--state-dir", t.TempDir(),
		// The test connects from loopback, which is not on this list.
		"--allow-ips", "192.0.2.0/24",
	})
	defer stop()

	response, err := http.Get("http://" + address + "/api/v1/health")
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", response.StatusCode)
	}
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "forbidden_scope") {
		t.Fatalf("body = %s", body)
	}
}

// startServe runs Serve until the test stops it, and returns the address it bound.
func startServe(t *testing.T, args []string) (string, func()) {
	t.Helper()
	clearServeEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr syncBuffer
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, args, &stdout, &stderr) }()
	address := waitForAddress(t, &stdout)
	stop := func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Serve: %v (stderr %s)", err, stderr.String())
			}
		case <-time.After(10 * time.Second):
			t.Fatal("Serve did not return after cancel")
		}
	}
	return address, stop
}

// runServe runs Serve and expects it to fail before it starts listening.
func runServe(t *testing.T, args []string) error {
	t.Helper()
	clearServeEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stdout, stderr syncBuffer
	return Serve(ctx, args, &stdout, &stderr)
}

// selfSignedPair writes a self-signed certificate and its key, and returns the leaf.
func selfSignedPair(t *testing.T) (string, string, *x509.Certificate) {
	t.Helper()
	certPath, keyPath, leaf := pitui.SelfSignedForTest(t)
	return certPath, keyPath, leaf
}
