package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
	"github.com/coder/websocket"
)

// syncBuffer is a bytes.Buffer that is safe to read while the command writes to it: serve
// runs in its own goroutine in these tests, and -race would flag an unsynchronised read.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// clearServeEnv removes every PIUI_* setting serve reads, so a test starts from the defaults
// whatever the developer's shell exports.
func clearServeEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		envAddr, envPi, envToken, envLogLevel, envMaxSessions, envReplayEvents, envReplayWindow,
		envDialogTimeout, envHeartbeat, envAllowHosts, envAllowOrigins, envBridge,
	} {
		t.Setenv(name, "")
	}
}

func TestParseServeConfigDefaults(t *testing.T) {
	clearServeEnv(t)

	cfg, err := parseServeConfig(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseServeConfig: %v", err)
	}
	if cfg.addr != defaultAddr || cfg.pi != defaultPi || cfg.logLevel != defaultLogLevel {
		t.Errorf("addr/pi/log-level = %q/%q/%q, want the documented defaults", cfg.addr, cfg.pi, cfg.logLevel)
	}
	if cfg.maxSessions != sessions.DefaultMaxSessions {
		t.Errorf("max-sessions = %d, want %d", cfg.maxSessions, sessions.DefaultMaxSessions)
	}
	if cfg.replayEvents != defaultReplayEvents || cfg.replayWindow != defaultReplayWindow {
		t.Errorf("replay = %d/%s, want %d/%s", cfg.replayEvents, cfg.replayWindow, defaultReplayEvents, defaultReplayWindow)
	}
	if cfg.dialogTimeout != sessions.DefaultDialogTimeout || cfg.heartbeat != defaultHeartbeat {
		t.Errorf("timeouts = %s/%s, want %s/%s", cfg.dialogTimeout, cfg.heartbeat, sessions.DefaultDialogTimeout, defaultHeartbeat)
	}
	if cfg.token != "" || cfg.bridge != "" {
		t.Errorf("token/bridge = %q/%q, want both empty by default", cfg.token, cfg.bridge)
	}
	if len(cfg.sessionFlags) != 0 {
		t.Errorf("sessions = %v, want none", cfg.sessionFlags)
	}
}

func TestParseServeConfigEnvThenFlags(t *testing.T) {
	clearServeEnv(t)
	t.Setenv(envAddr, "0.0.0.0:9000")
	t.Setenv(envMaxSessions, "4")
	t.Setenv(envReplayWindow, "2m")
	t.Setenv(envAllowOrigins, "https://a.example, https://b.example")
	t.Setenv(envLogLevel, "warn")

	cfg, err := parseServeConfig([]string{"--addr", "127.0.0.1:9100", "--max-sessions", "2"}, io.Discard)
	if err != nil {
		t.Fatalf("parseServeConfig: %v", err)
	}
	if cfg.addr != "127.0.0.1:9100" {
		t.Errorf("addr = %q, want the flag to win over %s", cfg.addr, envAddr)
	}
	if cfg.maxSessions != 2 {
		t.Errorf("max-sessions = %d, want the flag to win over the variable", cfg.maxSessions)
	}
	if cfg.replayWindow != 2*time.Minute {
		t.Errorf("replay-window = %s, want the variable value", cfg.replayWindow)
	}
	if cfg.logLevel != "warn" {
		t.Errorf("log-level = %q, want the variable value", cfg.logLevel)
	}
	if len(cfg.allowOrigins) != 2 || cfg.allowOrigins[0] != "https://a.example" {
		t.Errorf("allow-origins = %v, want the comma-separated values", cfg.allowOrigins)
	}
}

func TestParseServeConfigRejectsBadValues(t *testing.T) {
	clearServeEnv(t)

	tests := []struct {
		name    string
		args    []string
		wantMsg string
	}{
		{name: "unknown flag", args: []string{"--nope"}, wantMsg: "flag provided but not defined"},
		{name: "unexpected argument", args: []string{"extra"}, wantMsg: "unexpected argument"},
		{name: "bad integer", args: []string{"--max-sessions", "many"}, wantMsg: "is not an integer"},
		{name: "negative integer", args: []string{"--replay-events", "-1"}, wantMsg: "must not be negative"},
		{name: "bad duration", args: []string{"--replay-window", "soon"}, wantMsg: "is not a duration"},
		{name: "zero duration", args: []string{"--heartbeat", "0s"}, wantMsg: "must be positive"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, err := parseServeConfig(tt.args, &stderr)
			if err == nil {
				t.Fatalf("parseServeConfig(%v) = nil error, want a usage error", tt.args)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("error = %q, want it to mention %q", err, tt.wantMsg)
			}
		})
	}

	if _, err := newLogger(io.Discard, "loud"); err == nil {
		t.Errorf("newLogger(loud) = nil error, want a level error")
	}
}

func TestSplitSessionFlag(t *testing.T) {
	tests := []struct {
		value    string
		wantCWD  string
		wantName string
	}{
		{value: "/work", wantCWD: "/work"},
		{value: "/work:alpha", wantCWD: "/work", wantName: "alpha"},
		{value: "/work/a:b", wantCWD: "/work/a", wantName: "b"},
		{value: "/work/", wantCWD: "/work/"},
		{value: "C:/work", wantCWD: "C:/work"},
		{value: "/work:name/with/slash", wantCWD: "/work:name/with/slash"},
		{value: "/work:", wantCWD: "/work:"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cwd, name := splitSessionFlag(tt.value)
			if cwd != tt.wantCWD || name != tt.wantName {
				t.Errorf("splitSessionFlag(%q) = (%q, %q), want (%q, %q)", tt.value, cwd, name, tt.wantCWD, tt.wantName)
			}
		})
	}

	cfg := serveConfig{sessionFlags: []string{"/work:alpha", "/other"}}
	specs, err := cfg.startSpecs()
	if err != nil {
		t.Fatalf("startSpecs: %v", err)
	}
	if len(specs) != 2 || specs[0].CWD != "/work" || specs[0].Name != "alpha" || specs[1].CWD != "/other" {
		t.Errorf("startSpecs = %+v, want both sessions with their names", specs)
	}
}

func TestServeHelpIsNotAFailure(t *testing.T) {
	clearServeEnv(t)

	var stdout, stderr bytes.Buffer
	if err := Serve(context.Background(), []string{"--help"}, &stdout, &stderr); err != nil {
		t.Fatalf("Serve(--help) = %v, want nil: --help is a request", err)
	}
	if !strings.Contains(stderr.String(), "usage: pi-ui serve") {
		t.Errorf("stderr = %q, want the flag usage", stderr.String())
	}
	if !strings.Contains(stderr.String(), "PIUI_RUNTIME_DIR") {
		t.Errorf("stderr = %q, want the environment-only setting documented", stderr.String())
	}
}

// TestServeEndToEnd starts the real server with a fake-pi child and drives it over HTTP.
func TestServeEndToEnd(t *testing.T) {
	clearServeEnv(t)
	binary := fakeharness.Build(t)
	workDir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var stdout, stderr syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, []string{
			"--addr", "127.0.0.1:0",
			"--pi", binary,
			"--session", workDir + ":alpha",
		}, &stdout, &stderr)
	}()

	addr := waitForAddress(t, &stdout)
	client := &http.Client{Timeout: 5 * time.Second}

	// The health probe answers with the protocol header and nothing else.
	health := getJSON(t, client, "http://"+addr+"/api/v1/health")
	if got := health.Header.Get("X-Piui-Protocol"); got != "1" {
		t.Errorf("X-Piui-Protocol = %q, want 1", got)
	}

	// The session started from --session is listed. It may still be spawning when the
	// listener answers (boot is asynchronous by design: readiness is what server.ready
	// reports), so poll until it is ready.
	var info sessions.Info
	waitFor(t, "the boot session to become ready", func() bool {
		var listed struct {
			Sessions []sessions.Info `json:"sessions"`
		}
		decodeJSON(t, getJSON(t, client, "http://"+addr+"/api/v1/sessions"), &listed)
		if len(listed.Sessions) != 1 {
			return false
		}
		info = listed.Sessions[0]
		return info.Status == sessions.StatusReady
	})
	if info.CWD != workDir || info.Name != "alpha" {
		t.Errorf("session = %+v, want cwd %s and name alpha", info, workDir)
	}
	if info.PID <= 0 {
		t.Fatalf("pid = %d, want the child's pid", info.PID)
	}

	// /api/v1/server advertises the protocol and the resolved limits.
	var serverInfo struct {
		Protocol int            `json:"protocol"`
		Features []string       `json:"features"`
		Limits   map[string]any `json:"limits"`
	}
	decodeJSON(t, getJSON(t, client, "http://"+addr+"/api/v1/server"), &serverInfo)
	if serverInfo.Protocol != 1 || serverInfo.Limits["maxSessions"] != float64(sessions.DefaultMaxSessions) {
		t.Errorf("server info = %+v, want protocol 1 and the resolved limits", serverInfo)
	}
	if len(serverInfo.Features) == 0 {
		t.Errorf("features = %v, want the advertised capabilities", serverInfo.Features)
	}

	// Cancelling the process context is what SIGTERM does: Serve returns, the child is
	// reaped and the port stops answering.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v (stderr %s)", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Serve did not return after cancellation (stderr %s)", stderr.String())
	}

	if err := syscall.Kill(info.PID, 0); !errors.Is(err, syscall.ESRCH) {
		t.Errorf("child %d is still alive after shutdown (kill(0) = %v)", info.PID, err)
	}
	if response, err := http.Get("http://" + addr + "/api/v1/health"); err == nil {
		response.Body.Close()
		t.Errorf("the listener still answers after shutdown")
	}
	log := stderr.String()
	if !strings.Contains(log, "shutting down") || !strings.Contains(log, "stopped") {
		t.Errorf("stderr = %q, want the shutdown to be logged", log)
	}
	if strings.Contains(stdout.String(), "session started") {
		t.Errorf("stdout = %q, want only the listening line on stdout", stdout.String())
	}
}

// waitForAddress polls the command's stdout for the listening line, so a test can talk to the
// port the kernel actually assigned (--addr 127.0.0.1:0).
func waitForAddress(t *testing.T, stdout *syncBuffer) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, rest, ok := strings.Cut(stdout.String(), "listening "); ok {
			if addr, _, _ := strings.Cut(strings.TrimSpace(rest), "\n"); addr != "" {
				return addr
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("no listening address on stdout: %q", stdout.String())
	return ""
}

// getJSON performs one GET and fails the test unless it answered 200.
func getJSON(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()
	response, err := client.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		t.Fatalf("GET %s = %d, want 200", url, response.StatusCode)
	}
	return response
}

// decodeJSON decodes a response body and closes it.
func decodeJSON(t *testing.T, response *http.Response, target any) {
	t.Helper()
	defer response.Body.Close()
	if err := json.NewDecoder(response.Body).Decode(target); err != nil {
		t.Fatalf("decode body: %v", err)
	}
}

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out after 10s waiting for %s", what)
}

// TestServeWebSocketCommandRoundTrip drives the wiring `serve` owns end to end: a real
// websocket client completes the handshake, subscribes to a session and sends a command that
// reaches the child through the hub, the supervisor and rpc, then comes back as a response.
func TestServeWebSocketCommandRoundTrip(t *testing.T) {
	clearServeEnv(t)
	binary := fakeharness.Build(t)
	workDir := t.TempDir()

	processCtx, stopServer := context.WithCancel(context.Background())
	defer stopServer()
	var stdout, stderr syncBuffer
	done := make(chan error, 1)
	go func() {
		done <- Serve(processCtx, []string{
			"--addr", "127.0.0.1:0",
			"--pi", binary,
			"--session", workDir,
		}, &stdout, &stderr)
	}()

	addr := waitForAddress(t, &stdout)
	client := &http.Client{Timeout: 5 * time.Second}

	var info sessions.Info
	waitFor(t, "the boot session to become ready", func() bool {
		var listed struct {
			Sessions []sessions.Info `json:"sessions"`
		}
		decodeJSON(t, getJSON(t, client, "http://"+addr+"/api/v1/sessions"), &listed)
		if len(listed.Sessions) != 1 {
			return false
		}
		info = listed.Sessions[0]
		return info.Status == sessions.StatusReady
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws://"+addr+"/ws/v1", nil)
	if err != nil {
		t.Fatalf("dial /ws/v1: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "test done")

	writeFrame(t, ctx, conn, `{"type":"hello","v":1,"client":{"name":"pi-ui-test","version":"0.0.1"}}`)
	welcome := readFrame(t, ctx, conn, hasType("welcome"))
	if string(welcome["v"]) != "1" {
		t.Errorf("welcome v = %s, want 1", welcome["v"])
	}

	writeFrame(t, ctx, conn, `{"type":"subscribe","sessionId":"`+info.ID+`"}`)

	// A command reaches the child and its response comes back through the same socket.
	writeFrame(t, ctx, conn, `{"type":"command","id":"c1","sessionId":"`+info.ID+
		`","op":"session.command.raw","payload":{"type":"get_commands"}}`)
	response := readFrame(t, ctx, conn, func(frame map[string]json.RawMessage) bool {
		return string(frame["type"]) == `"response"` && string(frame["id"]) == `"c1"`
	})
	if string(response["ok"]) != "true" {
		t.Fatalf("command response = %v, want ok:true", response)
	}
	if _, ok := response["data"]; !ok {
		t.Errorf("command response carries no data: %v", response)
	}

	// The protocol-level ping is answered with a pong frame.
	writeFrame(t, ctx, conn, `{"type":"ping"}`)
	readFrame(t, ctx, conn, hasType("pong"))

	// An unknown session answers with the taxonomy code, not with a dropped frame. The id
	// must still be schema-valid: the hub rejects a malformed sessionId as bad_request
	// before the command ever reaches the supervisor, which is a different case.
	writeFrame(t, ctx, conn, `{"type":"command","id":"c2","sessionId":"s_ffffffffffffffff","op":"session.abort"}`)
	failure := readFrame(t, ctx, conn, func(frame map[string]json.RawMessage) bool {
		return string(frame["type"]) == `"response"` && string(frame["id"]) == `"c2"`
	})
	if string(failure["ok"]) != "false" {
		t.Fatalf("unknown-session response = %v, want ok:false", failure)
	}
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(failure["error"], &envelope.Error); err != nil {
		t.Fatalf("error frame %s: %v", failure["error"], err)
	}
	if envelope.Error.Code != sessions.CodeSessionNotFound {
		t.Errorf("error code = %q, want %q", envelope.Error.Code, sessions.CodeSessionNotFound)
	}

	stopServer()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v (stderr %s)", err, stderr.String())
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("Serve did not return after cancellation (stderr %s)", stderr.String())
	}
}

// hasType matches one server frame by its type.
func hasType(want string) func(map[string]json.RawMessage) bool {
	return func(frame map[string]json.RawMessage) bool {
		return string(frame["type"]) == `"`+want+`"`
	}
}

// writeFrame sends one JSON frame to the hub.
func writeFrame(t *testing.T, ctx context.Context, conn *websocket.Conn, frame string) {
	t.Helper()
	if err := conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
		t.Fatalf("write %s: %v", frame, err)
	}
}

// readFrame reads frames until one satisfies want, so a test does not depend on how many
// replay or heartbeat frames arrive first.
func readFrame(t *testing.T, ctx context.Context, conn *websocket.Conn, want func(map[string]json.RawMessage) bool) map[string]json.RawMessage {
	t.Helper()
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read frame: %v", err)
		}
		var frame map[string]json.RawMessage
		if err := json.Unmarshal(data, &frame); err != nil {
			t.Fatalf("frame %s is not a JSON object: %v", data, err)
		}
		if want(frame) {
			return frame
		}
	}
}

// TestServeBadFlagIsAUsageError pins the exit-code contract at the command boundary: a
// malformed flag is a UsageError, which Run turns into ExitUsage (2) instead of the 1 of a
// run that failed.
func TestServeBadFlagIsAUsageError(t *testing.T) {
	err := Serve(context.Background(), []string{"--replay-window", "soon"}, io.Discard, io.Discard)
	var usage *UsageError
	if !errors.As(err, &usage) {
		t.Fatalf("Serve with a bad flag = %v, want a UsageError", err)
	}
	if !strings.Contains(err.Error(), "is not a duration") {
		t.Fatalf("error = %v, want it to name the flag problem", err)
	}
}
