package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/cli"
)

// TestServeCommandIsRegistered pins the registration seam: a subcommand is a file with an
// init(), never a branch in main.go (docs/spike-interfaces.md §5.7).
func TestServeCommandIsRegistered(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := cli.Run(context.Background(), []string{"help", "serve"}, &stdout, &stderr); code != cli.ExitOK {
		t.Fatalf("help serve = %d, want %d (stderr %q)", code, cli.ExitOK, stderr.String())
	}
	if got := stdout.String(); !strings.Contains(got, "serve") || !strings.Contains(got, "HTTP") {
		t.Errorf("help serve stdout = %q, want the command's summary", got)
	}
}

// TestBuildStampsReachTheCLI pins the other half of the seam: -ldflags stamps package main,
// so an init() must copy the values internal/cli reports through GET /api/v1/server.
func TestBuildStampsReachTheCLI(t *testing.T) {
	if cli.Version != version {
		t.Errorf("cli.Version = %q, want the binary stamp %q", cli.Version, version)
	}
	if cli.Commit != commit {
		t.Errorf("cli.Commit = %q, want the binary stamp %q", cli.Commit, commit)
	}
}

// TestServeRejectsAnUnknownFlag drives the command through the real entry point: the flag
// error reaches the caller as an error, and cli.Run turns it into a failure exit code.
func TestServeRejectsAnUnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := cli.Run(context.Background(), []string{"serve", "--definitely-not-a-flag"}, &stdout, &stderr)
	if code != cli.ExitError {
		t.Fatalf("serve --definitely-not-a-flag = %d, want %d", code, cli.ExitError)
	}
	if !strings.Contains(stderr.String(), "pi-ui serve") {
		t.Errorf("stderr = %q, want the command error", stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want it empty", stdout.String())
	}
}
