package adversarial_test

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Nihmar/pi-ui/server/internal/api"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/ws"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

const (
	// waitTimeout bounds every "the server or the child did something" assertion:
	// the child is a separate process, so a test polls with a deadline instead of
	// sleeping a fixed amount and hoping.
	waitTimeout = 15 * time.Second

	// bigRecordBytes is the 8 MiB record of acceptance criterion C7.
	bigRecordBytes = 8 << 20

	// clientReadLimit is raised above the library default so a test can read the
	// large frames it asked the server to emit. Only the test client needs it:
	// the server's own read limit bounds *inbound* frames.
	clientReadLimit = 32 << 20
)

// stackOptions tunes one in-process pipeline. Zero values are replaced by
// defaultStackOptions, so a test only sets what it attacks.
type stackOptions struct {
	token         string
	maxSessions   int
	dialogTimeout time.Duration
	heartbeat     time.Duration
	replayEvents  int
	replayWindow  time.Duration
	sendBuffer    int
	script        *fakeharness.Script
	piArgs        []string
	// piCommand replaces the default fake-pi argv entirely. It is how a test
	// puts a script in front of the child (a dirty stdout, a pid recorder).
	piCommand []string
}

func defaultStackOptions() stackOptions {
	return stackOptions{
		maxSessions:   sessions.DefaultMaxSessions,
		dialogTimeout: time.Minute,
		heartbeat:     time.Minute, // effectively off unless a test lowers it
		replayEvents:  64,
		replayWindow:  time.Minute,
		sendBuffer:    64,
	}
}

// stack is one complete pipeline behind a real HTTP listener: hub → supervisor →
// rpc bridge → fake-pi, plus the REST router that mounts them. It is the same
// wiring as cli.Serve, minus the command line, so a test can dial the real
// endpoints and inspect the real behaviour.
type stack struct {
	t          *testing.T
	hub        ws.Hub
	mgr        *sessions.Manager
	srv        *httptest.Server
	scriptPath string
	command    []string // custom child argv, empty for fakeharness.Command
	token      string
}

// newStack builds the pipeline for one test. mutate adjusts the options before
// anything is constructed; every test gets its own listener, hub and children.
func newStack(t *testing.T, mutate func(*stackOptions)) *stack {
	t.Helper()

	opts := defaultStackOptions()
	if mutate != nil {
		mutate(&opts)
	}
	if opts.script == nil {
		opts.script = &fakeharness.Script{}
	}

	// Build first: Command reads the binary it cached, and the tests that build
	// their own wrapper need the same binary path from Build.
	fakeharness.Build(t)
	scriptPath := fakeharness.WriteScript(t, *opts.script)
	argv := opts.piCommand
	if len(argv) == 0 {
		argv = fakeharness.Command(scriptPath, opts.piArgs...)
	}

	hub := ws.New(ws.Options{
		Token:         opts.token,
		ReplayEvents:  opts.replayEvents,
		ReplayWindow:  opts.replayWindow,
		Heartbeat:     opts.heartbeat,
		SendBuffer:    opts.sendBuffer,
		WriteTimeout:  5 * time.Second,
		ServerVersion: "0.0.1-adversarial",
		PiVersion:     "0.87.1",
		Features:      []string{"sessions", "replay"},
		Limits:        map[string]any{"maxSessions": opts.maxSessions},
	})
	mgr := sessions.New(sessions.Config{
		PiCommand:     argv,
		MaxSessions:   opts.maxSessions,
		DialogTimeout: opts.dialogTimeout,
		SendTimeout:   5 * time.Second,
		// Child diagnostics stay out of the test output: the assertions are on
		// events, and t.Log from a pump goroutine would race the end of the test.
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Hub:    hub,
	})
	hub.SetCommandHandler(mgr)
	hub.SetDialogHandler(mgr)
	hub.SetReplayer(mgr)

	router := api.NewRouter(api.Options{
		Supervisor: mgr,
		Hub:        hub,
		Info: api.ServerInfo{
			Version:   "0.0.1-adversarial",
			PiVersion: "0.87.1",
			Features:  []string{"sessions", "replay"},
			Limits:    map[string]any{"maxSessions": opts.maxSessions},
		},
		Auth: api.NewLoopbackOrToken(opts.token),
	})
	srv := httptest.NewServer(router)

	t.Cleanup(func() {
		// Same order as the server's own shutdown: drop sockets, stop the
		// listener, reap the children.
		_ = hub.Close()
		srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
		defer cancel()
		if err := mgr.Shutdown(ctx); err != nil {
			t.Errorf("cleanup shutdown: %v", err)
		}
	})

	return &stack{t: t, hub: hub, mgr: mgr, srv: srv, scriptPath: scriptPath, command: argv, token: opts.token}
}

// addr is the host:port the listener bound.
func (s *stack) addr() string {
	return strings.TrimPrefix(s.srv.URL, "http://")
}

// wsURL is the /ws/v1 URL of this stack.
func (s *stack) wsURL() string {
	return "ws" + strings.TrimPrefix(s.srv.URL, "http") + "/ws/v1"
}

// startSession spawns one child through the supervisor with extra fake-pi flags
// appended to the stacked argv, so a single test can give one session a scripted
// fault without rebuilding the stack.
func (s *stack) startSession(t *testing.T, cwd string, extra ...string) sessions.Info {
	t.Helper()

	argv := s.command
	if len(extra) > 0 {
		argv = fakeharness.Command(s.scriptPath, extra...)
	}
	info, err := s.mgr.Start(context.Background(), sessions.Spec{CWD: cwd, Command: argv})
	if err != nil {
		t.Fatalf("start session in %s: %v", cwd, err)
	}
	return info
}

// request performs one HTTP request with the stack's credentials and returns the
// response and its body. A non-2xx status is returned, never fatal, because the
// status is what the tests assert.
func (s *stack) request(method, path, body string, headers map[string]string) (*http.Response, []byte) {
	s.t.Helper()

	req, err := http.NewRequest(method, s.srv.URL+path, strings.NewReader(body))
	if err != nil {
		s.t.Fatalf("build %s %s: %v", method, path, err)
	}
	if s.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.token)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := (&http.Client{Timeout: waitTimeout}).Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		s.t.Fatalf("read %s %s body: %v", method, path, err)
	}
	return resp, data
}

func (s *stack) postJSON(path, body string) (*http.Response, []byte) {
	s.t.Helper()
	return s.request(http.MethodPost, path, body, nil)
}

func (s *stack) getJSON(path string) (*http.Response, []byte) {
	s.t.Helper()
	return s.request(http.MethodGet, path, "", nil)
}

// createSession is the REST path to a running session.
func (s *stack) createSession(cwd, name string) (sessions.Info, *http.Response, []byte) {
	s.t.Helper()

	body, err := json.Marshal(struct {
		CWD  string `json:"cwd"`
		Name string `json:"name,omitempty"`
	}{CWD: cwd, Name: name})
	if err != nil {
		s.t.Fatalf("marshal create body: %v", err)
	}
	resp, data := s.postJSON("/api/v1/sessions", string(body))
	var info sessions.Info
	if resp.StatusCode == http.StatusCreated {
		decodeJSON(s.t, data, &info)
	}
	return info, resp, data
}

// decodeJSON unmarshals a response body that a test expects to be JSON.
func decodeJSON(t *testing.T, data []byte, target any) {
	t.Helper()
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("decode body %s: %v", truncate(string(data), 300), err)
	}
}

// apiError extracts the §11 error envelope.
func apiError(t *testing.T, data []byte) (code, message string) {
	t.Helper()

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("decode error envelope %s: %v", truncate(string(data), 300), err)
	}
	if body.Error.Code == "" {
		t.Fatalf("body %s carries no error.code", truncate(string(data), 300))
	}
	return body.Error.Code, body.Error.Message
}

// itoa is strconv.Itoa for the tests that build JSON by hand.
func itoa(n int) string { return strconv.Itoa(n) }

// responseBody reads and closes an HTTP response body.
func responseBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	if resp == nil || resp.Body == nil {
		return ""
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	return string(data)
}

// wantStatus fails the test when the response status is not the expected one.
func wantStatus(t *testing.T, resp *http.Response, data []byte, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, want, truncate(string(data), 300))
	}
}

// wsClient is one dialed socket of the stack.
type wsClient struct {
	t      *testing.T
	conn   *websocket.Conn
	frames []string // types of every frame read, for failure diagnostics
}

// dial opens a socket without completing the handshake, so a test can decide what
// the first frame is. A non-nil response is the HTTP answer of a refused upgrade.
func (s *stack) dial(mutate func(*websocket.DialOptions)) (*wsClient, *http.Response, error) {
	s.t.Helper()

	opts := &websocket.DialOptions{}
	if mutate != nil {
		mutate(opts)
	}
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, s.wsURL(), opts)
	if err != nil {
		return nil, resp, err
	}
	conn.SetReadLimit(clientReadLimit)
	return &wsClient{t: s.t, conn: conn}, resp, nil
}

// mustDial dials and fails the test on a refused handshake.
func (s *stack) mustDial(mutate func(*websocket.DialOptions)) *wsClient {
	s.t.Helper()

	client, resp, err := s.dial(mutate)
	if err != nil {
		s.t.Fatalf("dial /ws/v1 (status %d): %v", statusOf(resp), err)
	}
	s.t.Cleanup(client.close)
	return client
}

// bearerHeader is the Authorization header of this stack.
func (s *stack) bearerHeader() map[string]string {
	return map[string]string{"Authorization": "Bearer " + s.token}
}

// hello completes the handshake and returns the welcome frame.
func (c *wsClient) hello() map[string]json.RawMessage {
	c.t.Helper()

	c.send(`{"type":"hello","v":1,"client":{"name":"pi-ui-adversarial","version":"0.0.1"}}`)
	return c.mustNext("welcome", hasType("welcome"))
}

// send writes one text frame.
func (c *wsClient) send(raw string) {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(raw)); err != nil {
		c.t.Fatalf("write frame %s: %v", truncate(raw, 200), err)
	}
}

// next reads the next text frame, with a timeout. A close or a timeout comes back
// as the error, which is what tests asserting a disconnect use.
func (c *wsClient) next(timeout time.Duration) (map[string]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	typ, data, err := c.conn.Read(ctx)
	if err != nil {
		return nil, err
	}
	if typ != websocket.MessageText {
		return nil, fmt.Errorf("unexpected binary frame of %d bytes", len(data))
	}
	var frame map[string]json.RawMessage
	if err := json.Unmarshal(data, &frame); err != nil {
		return nil, fmt.Errorf("frame is not a JSON object: %v (%s)", err, truncate(string(data), 200))
	}
	c.frames = append(c.frames, frameType(frame))
	return frame, nil
}

// mustNext reads until pred holds, failing the test on timeout or disconnect.
func (c *wsClient) mustNext(what string, pred func(map[string]json.RawMessage) bool) map[string]json.RawMessage {
	c.t.Helper()

	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		frame, err := c.next(time.Until(deadline))
		if err != nil {
			c.t.Fatalf("waiting for %s: %v (frames read: %v)", what, err, c.frames)
		}
		if pred(frame) {
			return frame
		}
	}
	c.t.Fatalf("timed out waiting for %s (frames read: %v)", what, c.frames)
	return nil
}

// expectClose reads until the socket is closed and asserts the close status.
func (c *wsClient) expectClose(want websocket.StatusCode) {
	c.t.Helper()

	deadline := time.Now().Add(waitTimeout)
	for {
		_, err := c.next(time.Until(deadline))
		if err != nil {
			if got := websocket.CloseStatus(err); got != want {
				c.t.Fatalf("close status = %d (%v), want %d (frames read: %v)", got, err, want, c.frames)
			}
			return
		}
	}
}

func (c *wsClient) close() {
	_ = c.conn.CloseNow()
}

// statusOf returns the HTTP status of a possibly absent response.
func statusOf(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}

// frameType reads the type field of a decoded frame.
func frameType(frame map[string]json.RawMessage) string {
	var value string
	_ = json.Unmarshal(frame["type"], &value)
	return value
}

// hasType matches one frame type.
func hasType(want string) func(map[string]json.RawMessage) bool {
	return func(frame map[string]json.RawMessage) bool { return frameType(frame) == want }
}

// hasResponse matches a response frame by correlation id.
func hasResponse(id string) func(map[string]json.RawMessage) bool {
	return func(frame map[string]json.RawMessage) bool {
		return frameType(frame) == "response" && fieldString(frame, "id") == id
	}
}

// fieldString reads a string field of a frame, "" when absent.
func fieldString(frame map[string]json.RawMessage, key string) string {
	var value string
	_ = json.Unmarshal(frame[key], &value)
	return value
}

// fieldObject reads an object field of a frame.
func fieldObject(t *testing.T, frame map[string]json.RawMessage, key string) map[string]json.RawMessage {
	t.Helper()

	var value map[string]json.RawMessage
	if err := json.Unmarshal(frame[key], &value); err != nil {
		t.Fatalf("frame field %q is not an object: %v (%s)", key, err, frame[key])
	}
	return value
}

// fieldBool reads a boolean field of a frame.
func fieldBool(t *testing.T, frame map[string]json.RawMessage, key string) bool {
	t.Helper()

	var value bool
	if err := json.Unmarshal(frame[key], &value); err != nil {
		t.Fatalf("frame field %q is not a boolean: %v (%s)", key, err, frame[key])
	}
	return value
}

// errorCodeOf reads the code of a failed response or of a server.error event; a
// frame that carries neither returns "".
func errorCodeOf(frame map[string]json.RawMessage) string {
	if raw, ok := frame["error"]; ok {
		var detail struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(raw, &detail)
		return detail.Code
	}
	var payload struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(frame["payload"], &payload)
	return payload.Code
}

// payloadOf reads the payload object of an event frame.
func payloadOf(t *testing.T, frame map[string]json.RawMessage) map[string]json.RawMessage {
	t.Helper()
	return fieldObject(t, frame, "payload")
}

// truncate shortens a value for a failure message.
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}

// rawUpgrade sends a hand-written WebSocket upgrade request over plain TCP and
// returns the HTTP response. It exists because the library's dialer cannot forge
// Host or Origin, and those headers are exactly what the handshake checks are
// about.
func rawUpgrade(t *testing.T, addr, host string, headers map[string]string) (*http.Response, string) {
	t.Helper()

	conn, err := net.DialTimeout("tcp", addr, waitTimeout)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(waitTimeout))

	key := base64.StdEncoding.EncodeToString([]byte("adversarial-key!"))
	var request strings.Builder
	fmt.Fprintf(&request, "GET /ws/v1 HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Key: %s\r\n", host, key)
	for name, value := range headers {
		fmt.Fprintf(&request, "%s: %s\r\n", name, value)
	}
	request.WriteString("\r\n")

	if _, err := io.WriteString(conn, request.String()); err != nil {
		t.Fatalf("write upgrade request: %v", err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodGet})
	if err != nil {
		t.Fatalf("read upgrade response: %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	response.Body.Close()
	return response, string(body)
}

// shellPath returns the POSIX shell the framing tests spawn, or skips the test on
// a host without one. The server itself is portable, but the spike's adversarial
// framing cases script a raw child, and a real shell is the simplest way to
// control exactly which bytes reach which pipe.
func shellPath(t *testing.T) string {
	t.Helper()

	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("no POSIX shell on this host: %v", err)
	}
	return shell
}

// writeFile writes an executable script and returns its path.
func writeFile(t *testing.T, name, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

// processAlive reports whether a pid still exists, zombies included.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// waitPIDsGone waits until every pid is gone and returns how long that took, or
// -1 when the deadline expired with a process still alive.
func waitPIDsGone(deadline time.Time, pids ...int) time.Duration {
	start := time.Now()
	for {
		alive := 0
		for _, pid := range pids {
			if processAlive(pid) {
				alive++
			}
		}
		if alive == 0 {
			return time.Since(start)
		}
		if time.Now().After(deadline) {
			return -1
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// readPIDs reads the pid files a wrapper script wrote into dir.
func readPIDs(t *testing.T, dir string) []int {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read pid dir: %v", err)
	}
	pids := make([]int, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".pid" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue // the file is still being written
		}
		var pid int
		if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}

// waitPIDs polls the pid files until want of them exist or the deadline expires.
func waitPIDs(t *testing.T, dir string, want int) []int {
	t.Helper()

	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		pids := readPIDs(t, dir)
		if len(pids) >= want {
			return pids
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d pid files in %s (found %v)", want, dir, readPIDs(t, dir))
	return nil
}

// serverBinary builds ./cmd/pi-ui once per test process and returns the binary
// path, so the lifecycle tests exercise the real entry point and its signal
// handling rather than an in-process copy.
var (
	serverBinaryOnce sync.Once
	serverBinaryPath string
	serverBinaryErr  error
)

func serverBinary(t testing.TB) string {
	t.Helper()

	serverBinaryOnce.Do(func() {
		root, err := moduleRoot()
		if err != nil {
			serverBinaryErr = err
			return
		}
		dir, err := os.MkdirTemp("", "piui-adversarial-bin-")
		if err != nil {
			serverBinaryErr = err
			return
		}
		binary := filepath.Join(dir, "pi-ui")
		build := exec.Command("go", "build", "-trimpath", "-o", binary, "./cmd/pi-ui")
		build.Dir = root
		if output, err := build.CombinedOutput(); err != nil {
			serverBinaryErr = fmt.Errorf("go build ./cmd/pi-ui: %w\n%s", err, output)
			return
		}
		serverBinaryPath = binary
	})
	if serverBinaryErr != nil {
		t.Fatalf("build server binary: %v", serverBinaryErr)
	}
	return serverBinaryPath
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

// writePiWrapper writes the `--pi` executable of the lifecycle tests: a shell
// script that records its own pid (the exec keeps the pid) and then replaces
// itself with fake-pi plus the flags a command line cannot express.
func writePiWrapper(t *testing.T, pidDir, fakePi, scriptPath string, extra ...string) string {
	t.Helper()

	args := append([]string{fakePi, "--script", scriptPath}, extra...)
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = shellQuote(arg)
	}
	lines := []string{"#!/bin/sh"}
	if pidDir != "" {
		lines = append(lines, `echo $$ > "`+pidDir+`/$$.pid"`)
	}
	lines = append(lines, "exec "+strings.Join(quoted, " "))
	return writeFile(t, "pi-wrapper", strings.Join(lines, "\n")+"\n")
}

// shellQuote single-quotes one argument for the wrapper script.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
