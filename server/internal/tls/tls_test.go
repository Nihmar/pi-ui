package tls

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// selfSigned writes a certificate and its key into a temporary directory.
func selfSigned(t *testing.T) (certPath, keyPath string, leaf *x509.Certificate) {
	t.Helper()
	return SelfSignedForTest(t)
}

func TestLoadDescribesTheLeaf(t *testing.T) {
	certPath, keyPath, leaf := selfSigned(t)

	certificate, info, err := Load(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if certificate.Leaf == nil {
		t.Fatal("the loaded pair should carry its leaf")
	}
	sum := sha256.Sum256(leaf.Raw)
	if info.FingerprintSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("fingerprint = %q", info.FingerprintSHA256)
	}
	if info.FingerprintSHA256 != Fingerprint(leaf) {
		t.Fatal("the two ways of asking must agree")
	}
	if info.Subject == "" || !info.NotAfter.Equal(leaf.NotAfter.UTC()) {
		t.Fatalf("info = %+v", info)
	}

	// The same certificate read from the file gives the same fingerprint: this is what
	// `pi-ui tls fingerprint` prints for a user to compare.
	fromFile, err := FingerprintFile(certPath)
	if err != nil {
		t.Fatal(err)
	}
	if fromFile.FingerprintSHA256 != info.FingerprintSHA256 {
		t.Fatalf("file fingerprint = %q", fromFile.FingerprintSHA256)
	}
}

func TestLoadRefusesWhatItCannotUse(t *testing.T) {
	certPath, keyPath, _ := selfSigned(t)

	if _, _, err := Load(certPath+".missing", keyPath); err == nil {
		t.Fatal("a missing certificate must fail")
	}
	if _, _, err := Load(keyPath, certPath); err == nil {
		t.Fatal("a swapped pair must fail")
	}
	junk := filepath.Join(t.TempDir(), "junk.pem")
	if err := os.WriteFile(junk, []byte("not pem"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := FingerprintFile(junk); err == nil {
		t.Fatal("a file that is not PEM must fail")
	}
	if _, err := FingerprintFile(junk + ".missing"); err == nil {
		t.Fatal("a missing file must fail")
	}
}
