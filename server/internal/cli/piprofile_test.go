package cli

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakePi writes a script that answers `--version` the way a test wants.
func fakePi(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the probe test uses a shell script")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "pi")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheProbeReadsTheVersion(t *testing.T) {
	cases := map[string]string{
		"echo 0.87.1":                             "0.87.1",
		"echo 'pi-coding-agent 0.87.1'":           "0.87.1",
		"echo '0.90.0-rc1'":                       "0.90.0-rc1",
		"printf 'version: 1.2.3\\nbuild: abc\\n'": "1.2.3",
		"echo 'no version here'":                  "",
	}
	for body, want := range cases {
		path := fakePi(t, body)
		version, err := probePiVersion(context.Background(), []string{path, "--mode", "rpc"})
		if want == "" {
			if err == nil {
				t.Fatalf("%q: expected an error, got %q", body, version)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%q: %v", body, err)
		}
		if version != want {
			t.Fatalf("%q: version = %q, want %q", body, version, want)
		}
	}
}

func TestTheProbeReportsWhatItCannotAsk(t *testing.T) {
	if _, err := probePiVersion(context.Background(), nil); err == nil {
		t.Fatal("no command is an error")
	}
	if _, err := probePiVersion(context.Background(), []string{"/nonexistent-pi"}); err == nil {
		t.Fatal("a missing binary is an error")
	}
	// A binary that never answers must not hold the startup: the budget kills it.
	slow := fakePi(t, "sleep 5; echo 0.87.1")
	start := time.Now()
	_, err := probePiVersionWithin(context.Background(), []string{slow}, 150*time.Millisecond)
	if err == nil {
		t.Fatal("a binary that does not answer in the budget is an error")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the probe waited %s, over its budget", elapsed)
	}
	if !strings.Contains(err.Error(), "did not answer") {
		t.Fatalf("the error must say what happened: %v", err)
	}

	// A slow but answering binary is not an error: it printed a version.
	slowEnough := fakePi(t, "sleep 0.2; echo 0.87.1")
	version, err := probePiVersionWithin(context.Background(), []string{slowEnough}, 5*time.Second)
	if err != nil || version != "0.87.1" {
		t.Fatalf("version = %q (%v)", version, err)
	}
}

func TestAMismatchIsSaidClearly(t *testing.T) {
	if message := versionMismatch("0.87.1", "0.87.1"); message != "" {
		t.Fatalf("equal versions are not a mismatch: %q", message)
	}
	if message := versionMismatch("0.87.1", ""); message != "" {
		t.Fatalf("an unknown version is not a mismatch: %q", message)
	}
	if message := versionMismatch("", "0.91.0"); message != "" {
		t.Fatalf("no expectation is not a mismatch: %q", message)
	}

	message := versionMismatch("0.87.1", "0.91.0")
	if !strings.Contains(message, "0.91.0") || !strings.Contains(message, "0.87.1") {
		t.Fatalf("the message must name both versions: %q", message)
	}
	if !strings.Contains(message, "align") {
		t.Fatalf("the message must say what to do: %q", message)
	}
}
