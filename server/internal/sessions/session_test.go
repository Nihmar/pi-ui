package sessions

import (
	"bytes"
	"encoding/json"
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

// TestResponseDataMapsPiRejections pins the §11 mapping at the child boundary: a prompt
// pi refused because a turn is already running becomes busy_streaming (the client can
// offer steer or a queue), every other rejection stays pi_rejected, an unreadable
// response is a pi_error rather than a success, and success data passes through.
func TestResponseDataMapsPiRejections(t *testing.T) {
	busy := json.RawMessage(`{"type":"response","success":false,"error":"Agent is already processing. Specify streamingBehavior ('steer' or 'followUp') to queue the message."}`)
	if _, err := responseData(busy); CodeOf(err) != CodeBusyStreaming {
		t.Fatalf("busy rejection code = %q, want %s", CodeOf(err), CodeBusyStreaming)
	}

	rejected := json.RawMessage(`{"type":"response","success":false,"error":"unknown command"}`)
	if _, err := responseData(rejected); CodeOf(err) != CodePiRejected {
		t.Fatalf("plain rejection code = %q, want %s", CodeOf(err), CodePiRejected)
	}

	if _, err := responseData(json.RawMessage(`not json`)); CodeOf(err) != CodePiError {
		t.Fatalf("unreadable response code = %q, want %s", CodeOf(err), CodePiError)
	}

	data, err := responseData(json.RawMessage(`{"type":"response","success":true,"data":{"ok":1}}`))
	if err != nil || string(data) != `{"ok":1}` {
		t.Fatalf("success data = %s, err = %v", data, err)
	}
}
