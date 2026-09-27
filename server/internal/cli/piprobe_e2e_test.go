package cli

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestServeReportsTheProbedPiVersion runs the real server against a fake pi and asks it, over
// HTTP, what version of pi it is driving: the probe is what makes GET /server trustworthy.
func TestServeReportsTheProbedPiVersion(t *testing.T) {
	clearServeEnv(t)
	fake := fakePi(t, "echo 0.91.0")

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, []string{
			"--addr", "127.0.0.1:0",
			"--pi", fake,
			"--state-dir", t.TempDir(),
		}, &stdout, &stderr)
	}()
	address := waitForAddress(t, &stdout)

	response, err := http.Get("http://" + address + "/api/v1/server")
	if err != nil {
		t.Fatalf("server: %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), `"piVersion":"0.91.0"`) {
		t.Fatalf("body = %s", body)
	}

	cancel()
	<-done
}

// TestServeWarnsAboutAMismatch keeps the expectation in the log where an operator reads it.
func TestServeWarnsAboutAMismatch(t *testing.T) {
	clearServeEnv(t)
	fake := fakePi(t, "echo 0.91.0")
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "unused"), nil, 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var stdout, stderr syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, []string{
			"--addr", "127.0.0.1:0",
			"--pi", fake,
			"--pi-version", "0.87.1",
			"--state-dir", stateDir,
		}, &stdout, &stderr)
	}()
	waitForAddress(t, &stdout)
	cancel()
	<-done

	if !strings.Contains(stderr.String(), "version mismatch") {
		t.Fatalf("stderr = %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "0.87.1") || !strings.Contains(stderr.String(), "0.91.0") {
		t.Fatalf("the warning must name both versions: %s", stderr.String())
	}
}
