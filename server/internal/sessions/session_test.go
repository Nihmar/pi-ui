package sessions

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestStderrLinesAreCappedAndNeverTakeTheRecordStream pins the diagnostics rule of §3: the
// child's stderr goes to slog, is capped, and stays out of the event stream.
func TestStderrLinesAreCappedAndNeverTakeTheRecordStream(t *testing.T) {
	var logged bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logged, &slog.HandlerOptions{Level: slog.LevelDebug}))
	mgr := New(Config{PiCommand: []string{"pi"}, Logger: logger})
	s := newSession(mgr, codegenSessionID, Spec{CWD: "/work"})

	s.stderrLine([]byte("fake-pi: stderr line 1"))
	if !strings.Contains(logged.String(), "stderr line 1") {
		t.Errorf("log = %q, want the child's diagnostic line", logged.String())
	}

	logged.Reset()
	s.stderrLine([]byte(strings.Repeat("x", maxStderrLog*2)))
	line := logged.String()
	if len(line) > maxStderrLog+512 {
		t.Errorf("logged %d bytes for a %d byte line, want it capped", len(line), maxStderrLog*2)
	}
	if !strings.Contains(line, "truncated") {
		t.Errorf("log = %q, want the truncation to be visible", line)
	}
}

// TestSessionIDIsUsedAsTheSessionDirectoryDocument keeps the bridge config path derivable:
// RuntimeDir()/<sid>.json is what the CLI and the e2e tests look for.
func TestSessionIDIsUsedAsTheSessionDirectoryDocument(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(runtimeDirEnv, dir)
	path, err := writeBridgeConfig(codegenSessionID)
	if err != nil {
		t.Fatalf("writeBridgeConfig: %v", err)
	}
	if !strings.HasSuffix(path, codegenSessionID+".json") {
		t.Errorf("path = %q, want <runtime>/%s.json", path, codegenSessionID)
	}
}
