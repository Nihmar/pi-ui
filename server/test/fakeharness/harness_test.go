package fakeharness

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestBuildFromNestedPackageDir proves Build works from a package directory that is not
// the module root: the tests of every package call it this way.
func TestBuildFromNestedPackageDir(t *testing.T) {
	if _, err := os.Stat("go.mod"); err == nil {
		t.Fatal("this test must run from a nested package directory, not the module root")
	}

	binary := Build(t)
	info, err := os.Stat(binary)
	if err != nil {
		t.Fatalf("stat %s: %v", binary, err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("%s is not executable", binary)
	}
	if !strings.HasSuffix(binary, "fake-pi") {
		t.Errorf("binary path %s does not end in fake-pi", binary)
	}
}

func TestBuildIsCachedPerTestProcess(t *testing.T) {
	first := Build(t)
	if second := Build(t); second != first {
		t.Errorf("Build returned %s then %s; the binary must be built once", first, second)
	}
}

func TestCommandArgv(t *testing.T) {
	Build(t)
	argv := Command("/tmp/script.json", "--mode", "rpc", "--no-session", "--emit", "5")
	want := []string{argv[0], "--script", "/tmp/script.json", "--mode", "rpc", "--no-session", "--emit", "5"}
	if len(argv) != len(want) {
		t.Fatalf("argv = %v, want %v", argv, want)
	}
	for i := range want {
		if argv[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, argv[i], want[i])
		}
	}
}

func TestCommandPanicsWithoutBuild(t *testing.T) {
	buildMu.Lock()
	saved := builtPath
	buildMu.Unlock()
	t.Cleanup(func() {
		buildMu.Lock()
		builtPath = saved
		buildMu.Unlock()
	})

	for _, tc := range []struct {
		name string
		path string
		want string
	}{
		{"no build yet", "", "before Build"},
		{"cached binary removed", "/tmp/fakeharness-does-not-exist", "is gone"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			buildMu.Lock()
			builtPath = tc.path
			buildMu.Unlock()

			recovered := catchPanic(func() { Command("/tmp/script.json") })
			if recovered == nil {
				t.Fatal("Command did not panic")
			}
			message, ok := recovered.(string)
			if !ok || !strings.Contains(message, tc.want) {
				t.Fatalf("panic = %v, want a message containing %q", recovered, tc.want)
			}
			if !strings.Contains(message, "Build(t)") {
				t.Fatalf("panic = %v, want a message telling the caller to call Build(t)", recovered)
			}
		})
	}
}

func catchPanic(call func()) (recovered any) {
	defer func() { recovered = recover() }()
	call()
	return nil
}

// TestWriteScriptAndDriveChild runs the whole helper chain the way the RPC tests will:
// Build, WriteScript, Command, then talk JSONL to the child over pipes.
func TestWriteScriptAndDriveChild(t *testing.T) {
	Build(t)
	script := Script{
		SessionID: "harness-session",
		Startup: []Step{{Record: json.RawMessage(
			`{"type":"extension_ui_request","id":"u1","method":"notify","message":"harness ready"}`)}},
		Commands: map[string]CommandScript{
			"prompt": {
				Response: json.RawMessage(`{"success":true}`),
				Events:   []Step{{Record: json.RawMessage(`{"type":"agent_settled"}`)}},
			},
		},
	}
	path := WriteScript(t, script)

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read script %s: %v", path, err)
	}
	var decoded Script
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("script %s is not a Script: %v", path, err)
	}
	if decoded.SessionID != "harness-session" {
		t.Errorf("script round trip lost sessionId: %q", decoded.SessionID)
	}

	argv := Command(path, "--mode", "rpc", "--no-session", "--offline")
	child := exec.Command(argv[0], argv[1:]...)
	child.Stdin = strings.NewReader("{\"id\":\"prompt-1\",\"type\":\"prompt\",\"message\":\"hi\"}\n")
	var stdout, stderr bytes.Buffer
	child.Stdout = &stdout
	child.Stderr = &stderr
	if err := child.Run(); err != nil {
		t.Fatalf("run %v: %v (stderr: %s)", argv, err, stderr.String())
	}

	lines := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d records, want 3:\n%s", len(lines), stdout.String())
	}
	for i, line := range lines {
		var record map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("record %d does not decode: %v", i, err)
		}
	}
	if !strings.Contains(lines[1], `"id":"prompt-1"`) {
		t.Errorf("response does not carry the request id: %s", lines[1])
	}
	if !strings.Contains(lines[2], `"agent_settled"`) {
		t.Errorf("scripted event missing: %s", lines[2])
	}
}
