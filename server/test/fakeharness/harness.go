// Package fakeharness builds the deterministic fake-pi child (server/test/fake-pi) and
// hands tests the argv the server's RpcBridge expects, so protocol tests run without a
// model, without network and without the real agent.
//
// Contract: docs/spike-interfaces.md §9. Usage from any package:
//
//	binary := fakeharness.Build(t)                                  // once per test process
//	argv := fakeharness.Command(fakeharness.WriteScript(t, script))  // ready for rpc.Spec
package fakeharness

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// Script is the replay program fake-pi executes; the field and tag set is frozen by
// docs/spike-interfaces.md §9 and mirrored by the harness itself.
type Script struct {
	SessionID   string                   `json:"sessionId,omitempty"`
	SessionFile string                   `json:"sessionFile,omitempty"`
	Entries     []json.RawMessage        `json:"entries,omitempty"`
	Startup     []Step                   `json:"startup,omitempty"`
	Commands    map[string]CommandScript `json:"commands,omitempty"`
	Default     *CommandScript           `json:"default,omitempty"`
	Faults      Faults                   `json:"faults,omitempty"`
}

// Step is one record plus the delay that precedes it.
type Step struct {
	DelayMs int             `json:"delayMs,omitempty"`
	Record  json.RawMessage `json:"record"`
}

// CommandScript is the answer to one command type. An omitted response falls back to the
// harness default for that command; Error forces a failing response; ExitCode makes the
// child exit right after the answer, which is how a test arms a crash without a timer.
type CommandScript struct {
	DelayMs  int             `json:"delayMs,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Events   []Step          `json:"events,omitempty"`
	Error    string          `json:"error,omitempty"`
	ExitCode *int            `json:"exitCode,omitempty"`
}

// Faults are scripted wall-clock faults; nil means "not set".
type Faults struct {
	StallMs      int  `json:"stallMs,omitempty"`
	CrashAfterMs *int `json:"crashAfterMs,omitempty"`
	ExitAfterMs  *int `json:"exitAfterMs,omitempty"`
}

var (
	buildOnce sync.Once
	buildMu   sync.Mutex
	builtPath string
	builtErr  error
)

// Build compiles test/fake-pi once per test process and returns the binary path. The
// module root is located by walking up from the working directory until a go.mod is
// found, so tests in any nested package directory can call it.
func Build(t testing.TB) string {
	t.Helper()
	path, err := binaryPath(t)
	if err != nil {
		t.Fatalf("fakeharness: %v", err)
	}
	return path
}

// WriteScript marshals a replay script into a temp file and returns its path.
func WriteScript(t testing.TB, script Script) string {
	t.Helper()
	data, err := json.Marshal(script)
	if err != nil {
		t.Fatalf("fakeharness: marshal script: %v", err)
	}
	file, err := os.CreateTemp("", "fake-pi-script-*.json")
	if err != nil {
		t.Fatalf("fakeharness: create script: %v", err)
	}
	name := file.Name()
	t.Cleanup(func() { os.Remove(name) })

	if _, err := file.Write(data); err != nil {
		file.Close()
		t.Fatalf("fakeharness: write script %s: %v", name, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("fakeharness: close script %s: %v", name, err)
	}
	return name
}

// Command returns the fake-pi argv for one run, ready for rpc.Spec.Command: the binary,
// the script and the caller's extras (pi flags, --emit, --crlf, ...). Build must have run
// in the same test first; Command has no testing.TB, so it reports that contract with a
// panic instead of a later exec error.
func Command(scriptPath string, extra ...string) []string {
	buildMu.Lock()
	binary := builtPath
	buildMu.Unlock()

	if binary == "" {
		panic("fakeharness: Command called before Build; call Build(t) first")
	}
	if _, err := os.Stat(binary); err != nil {
		panic("fakeharness: " + binary + " is gone; call Build(t) in this test before Command")
	}
	argv := make([]string, 0, len(extra)+3)
	argv = append(argv, binary, "--script", scriptPath)
	return append(argv, extra...)
}

// binaryPath builds the harness once per test process and rebuilds it when the cached
// file is gone: the first caller's cleanup removes its cache directory, so every later
// test in the same process must still get a working binary (go build is cached, so a
// rebuild costs ~0.15 s).
func binaryPath(t testing.TB) (string, error) {
	buildOnce.Do(func() {
		path, err := buildBinary(t)
		buildMu.Lock()
		builtPath, builtErr = path, err
		buildMu.Unlock()
	})

	buildMu.Lock()
	defer buildMu.Unlock()
	if builtErr != nil {
		return "", builtErr
	}
	if _, err := os.Stat(builtPath); err == nil {
		return builtPath, nil
	}
	builtPath, builtErr = buildBinary(t)
	return builtPath, builtErr
}

// buildBinary compiles the harness into a fresh temp directory; the directory is removed
// when t ends, and the failure carries the captured stderr.
func buildBinary(t testing.TB) (string, error) {
	t.Helper()
	root, err := moduleRoot()
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "fakeharness-")
	if err != nil {
		return "", err
	}
	t.Cleanup(func() { os.RemoveAll(dir) })

	binary := filepath.Join(dir, "fake-pi")
	build := exec.Command("go", "build", "-trimpath", "-o", binary, "./test/fake-pi")
	build.Dir = root
	var output strings.Builder
	build.Stdout = &output
	build.Stderr = &output
	if err := build.Run(); err != nil {
		os.RemoveAll(dir)
		return "", fmt.Errorf("go build -trimpath -o %s ./test/fake-pi in %s: %w\n%s",
			binary, root, err, strings.TrimSpace(output.String()))
	}
	return binary, nil
}

// moduleRoot walks up from the working directory until it finds a go.mod.
func moduleRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found above %s", dir)
		}
		dir = parent
	}
}
