package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// codegenSessionID is a literal session id: these tests exercise argv and environment
// building, not the id generator.
const codegenSessionID = "s_0123456789abcdef"

func TestChildArgvAddsPerSessionFlags(t *testing.T) {
	mgr := New(Config{
		PiCommand:  []string{"pi", "--mode", "rpc"},
		SessionDir: "/base/sessions",
		BridgeExt:  "/opt/pi-ui/bridge.ts",
	})

	got := mgr.childArgv(Spec{
		CWD:        "/work",
		Name:       "alpha",
		SessionDir: "/per-session",
		Command:    []string{"pi", "--mode", "rpc", "--provider", "llama.cpp"},
	})
	want := []string{
		"pi", "--mode", "rpc", "--provider", "llama.cpp",
		"--session-dir", "/per-session",
		"--name", "alpha",
		"-e", "/opt/pi-ui/bridge.ts",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("childArgv = %v, want %v", got, want)
	}
}

func TestChildArgvNeverDuplicatesWhatTheCallerPassed(t *testing.T) {
	mgr := New(Config{PiCommand: []string{"pi", "--mode", "rpc"}, SessionDir: "/base", BridgeExt: "/bridge.ts"})

	got := mgr.childArgv(Spec{
		CWD:     "/work",
		Name:    "alpha",
		Command: []string{"pi", "--mode", "rpc", "--name", "alpha", "--session-dir=/elsewhere", "-e", "/other.ts"},
	})
	want := []string{"pi", "--mode", "rpc", "--name", "alpha", "--session-dir=/elsewhere", "-e", "/other.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("childArgv = %v, want the caller's argv untouched (%v)", got, want)
	}
}

func TestChildArgvFallsBackToTheConfiguredCommand(t *testing.T) {
	mgr := New(Config{PiCommand: []string{"fake-pi", "--mode", "rpc"}})
	got := mgr.childArgv(Spec{CWD: "/work"})
	if !reflect.DeepEqual(got, []string{"fake-pi", "--mode", "rpc"}) {
		t.Errorf("childArgv = %v, want the configured command", got)
	}
	if got := mgr.childArgv(Spec{CWD: "/work"}); &got[0] == &mgr.cfg.PiCommand[0] {
		t.Errorf("childArgv returned the config's backing array: a caller could mutate it")
	}
}

func TestChildEnvWritesTheBridgeConfigOnlyWithTheExtension(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(runtimeDirEnv, dir)

	mgr := New(Config{PiCommand: []string{"pi"}, BridgeExt: "/bridge.ts"})
	argv := mgr.childArgv(Spec{CWD: "/work", BridgeExt: "/bridge.ts", Env: map[string]string{"B": "2", "A": "1"}})
	env, err := mgr.childEnv(Spec{Env: map[string]string{"B": "2", "A": "1"}}, argv, codegenSessionID)
	if err != nil {
		t.Fatalf("childEnv: %v", err)
	}
	wantEnv := []string{"A=1", "B=2", bridgeConfigEnv + "=" + filepath.Join(dir, codegenSessionID+".json")}
	if !reflect.DeepEqual(env, wantEnv) {
		t.Fatalf("childEnv = %v, want %v", env, wantEnv)
	}

	data, err := os.ReadFile(filepath.Join(dir, codegenSessionID+".json"))
	if err != nil {
		t.Fatalf("bridge config was not written: %v", err)
	}
	var document struct {
		SessionID string `json:"sessionId"`
		Approvals struct {
			Mode     string   `json:"mode"`
			Patterns []string `json:"patterns"`
		} `json:"approvals"`
	}
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("bridge config is not valid JSON: %v (%s)", err, data)
	}
	if document.SessionID != codegenSessionID {
		t.Errorf("sessionId = %q, want %q", document.SessionID, codegenSessionID)
	}
	if document.Approvals.Mode != "confirm" {
		t.Errorf("approvals.mode = %q, want confirm", document.Approvals.Mode)
	}
	if !reflect.DeepEqual(document.Approvals.Patterns, approvalPatterns) {
		t.Errorf("approvals.patterns = %v, want %v", document.Approvals.Patterns, approvalPatterns)
	}

	info, err := os.Stat(filepath.Join(dir, codegenSessionID+".json"))
	if err != nil {
		t.Fatalf("stat bridge config: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("bridge config mode = %o, want 600", mode)
	}

	// Without the extension nothing is written and no variable is exported.
	plain := New(Config{PiCommand: []string{"pi"}})
	plainArgv := plain.childArgv(Spec{CWD: "/work", Env: map[string]string{"A": "1"}})
	plainEnv, err := plain.childEnv(Spec{Env: map[string]string{"A": "1"}}, plainArgv, "s_ffffffffffffffff")
	if err != nil {
		t.Fatalf("childEnv without the bridge: %v", err)
	}
	if !reflect.DeepEqual(plainEnv, []string{"A=1"}) {
		t.Errorf("childEnv = %v, want only the caller's entries", plainEnv)
	}
	if _, err := os.Stat(filepath.Join(dir, "s_ffffffffffffffff.json")); !os.IsNotExist(err) {
		t.Errorf("a bridge config was written without the extension: %v", err)
	}
}

func TestRuntimeDirFollowsTheDocumentedRule(t *testing.T) {
	t.Setenv(runtimeDirEnv, "/run/pi-ui-explicit")
	if got := RuntimeDir(); got != "/run/pi-ui-explicit" {
		t.Errorf("RuntimeDir() = %q, want PIUI_RUNTIME_DIR", got)
	}

	t.Setenv(runtimeDirEnv, "")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := RuntimeDir(); got != filepath.Join("/run/user/1000", "pi-ui") {
		t.Errorf("RuntimeDir() = %q, want $XDG_RUNTIME_DIR/pi-ui", got)
	}

	t.Setenv("XDG_RUNTIME_DIR", "")
	want := filepath.Join(os.TempDir(), "pi-ui-"+itoa(os.Getuid()))
	if got := RuntimeDir(); got != want {
		t.Errorf("RuntimeDir() = %q, want %q", got, want)
	}
	if !strings.HasPrefix(want, os.TempDir()) {
		t.Errorf("the fallback runtime dir %q is not under os.TempDir()", want)
	}
}

// itoa is strconv.Itoa without the import churn in a test that only needs one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
