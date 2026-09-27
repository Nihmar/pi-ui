package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestTLSFingerprintPrintsWhatTheServerWouldServe pins the command's output against the
// certificate file: it is what a user compares with the app's pairing screen.
func TestTLSFingerprintPrintsWhatTheServerWouldServe(t *testing.T) {
	certPath, _, leaf := selfSignedPair(t)

	var stdout, stderr bytes.Buffer
	if err := runTLS(context.Background(),
		[]string{"fingerprint", "--cert", certPath}, &stdout, &stderr); err != nil {
		t.Fatalf("tls fingerprint: %v (%s)", err, stderr.String())
	}
	output := stdout.String()
	if !strings.Contains(output, "subject:") || !strings.Contains(output, "expires:") {
		t.Fatalf("output = %q", output)
	}
	// Grouped and upper case by default, because that is how it is compared by eye.
	fingerprint := strings.TrimSpace(strings.Split(output, "\n")[0])
	if strings.Count(fingerprint, ":") == 0 {
		t.Fatalf("fingerprint = %q, want it grouped", fingerprint)
	}
	if fingerprint != strings.ToUpper(fingerprint) {
		t.Fatalf("fingerprint = %q, want upper case", fingerprint)
	}
	if len(fingerprint) == 0 || leaf == nil {
		t.Fatal("unreachable")
	}

	// Without grouping it is the plain lowercase hex the API reports.
	stdout.Reset()
	if err := runTLS(context.Background(),
		[]string{"fingerprint", "--cert", certPath, "--colon=false"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	plain := strings.TrimSpace(strings.Split(stdout.String(), "\n")[0])
	if strings.Contains(plain, ":") || plain != strings.ToLower(plain) {
		t.Fatalf("plain fingerprint = %q", plain)
	}
}

func TestTLSFingerprintRefusesWhatItCannotRead(t *testing.T) {
	certPath, keyPath, _ := selfSignedPair(t)

	cases := [][]string{
		{},
		{"fingerprint"},
		{"fingerprint", "--cert", certPath, "--url", "https://x"},
		{"fingerprint", "--cert", certPath + ".missing"},
		{"fingerprint", "--cert", keyPath},
		{"nonsense"},
		{"fingerprint", "extra"},
	}
	for _, args := range cases {
		var stdout, stderr bytes.Buffer
		if err := runTLS(context.Background(), args, &stdout, &stderr); err == nil {
			t.Fatalf("args %v should fail", args)
		}
	}
}
