// Package e2e drives the real `pi-ui` binary end to end: REST session creation,
// WebSocket prompt and events, and the SIGTERM reaping guarantee of acceptance
// criterion C8. Everything here runs against the built binary and the deterministic
// fake-pi child, except realpi_test.go which talks to the installed pi.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// serverBinary is the one `go build` of cmd/pi-ui per test process, removed by
// TestMain when the tests end.
var (
	buildOnce    sync.Once
	serverBinary string
	buildDir     string
	buildErr     error
)

func TestMain(m *testing.M) {
	code := m.Run()
	if buildDir != "" {
		_ = os.RemoveAll(buildDir)
	}
	os.Exit(code)
}

// buildServer compiles cmd/pi-ui once per test process.
func buildServer(t *testing.T) string {
	t.Helper()
	buildOnce.Do(func() {
		root, err := moduleRoot()
		if err != nil {
			buildErr = err
			return
		}
		buildDir, err = os.MkdirTemp("", "pi-ui-e2e-")
		if err != nil {
			buildErr = err
			return
		}
		serverBinary = filepath.Join(buildDir, "pi-ui")
		build := exec.Command("go", "build", "-trimpath", "-o", serverBinary, "./cmd/pi-ui")
		build.Dir = root
		if output, err := build.CombinedOutput(); err != nil {
			buildErr = fmt.Errorf("go build ./cmd/pi-ui: %w\n%s", err, output)
		}
	})
	if buildErr != nil {
		t.Fatalf("build pi-ui: %v", buildErr)
	}
	return serverBinary
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

// wrapFakePi writes a shell wrapper that runs fake-pi with a script and forwards
// the pi argv, because `pi-ui serve --pi` takes one executable and cannot carry
// harness flags. exec.LookPath resolves the wrapper like any other pi.
func wrapFakePi(t *testing.T, scriptPath string) string {
	t.Helper()
	fakePi := fakeharness.Build(t)
	path := filepath.Join(t.TempDir(), "pi-wrapper.sh")
	body := fmt.Sprintf("#!/bin/sh\nexec %q --script %q \"$@\"\n", fakePi, scriptPath)
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}
	return path
}

// scriptJSON marshals one fake-pi script record.
func scriptJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal script record: %v", err)
	}
	return data
}

// serverProcess is one running `pi-ui serve` with its listening address.
type serverProcess struct {
	cmd    *exec.Cmd
	addr   string
	stderr *bytes.Buffer
	done   chan struct{} // closed after Wait returned

	mu      sync.Mutex
	waitErr error
}

// waitErrOf returns the exit error of Wait.
func (p *serverProcess) waitErrOf() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.waitErr
}

// startServer launches the built binary with `serve --addr 127.0.0.1:0` plus extra
// flags and waits for the listening line on stdout.
func startServer(t *testing.T, extra ...string) *serverProcess {
	t.Helper()
	binary := buildServer(t)
	args := append([]string{"serve", "--addr", "127.0.0.1:0", "--log-level", "error"}, extra...)
	cmd := exec.Command(binary, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout pipe: %v", err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		t.Fatalf("start pi-ui serve: %v", err)
	}
	proc := &serverProcess{cmd: cmd, stderr: stderr, done: make(chan struct{})}

	lines := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			lines <- scanner.Text()
		}
		close(lines)
	}()
	select {
	case line, ok := <-lines:
		if !ok || !strings.HasPrefix(line, "listening ") {
			t.Fatalf("server did not announce its address (got %q); stderr:\n%s", line, stderr.String())
		}
		proc.addr = strings.TrimSpace(strings.TrimPrefix(line, "listening "))
	case <-time.After(20 * time.Second):
		t.Fatalf("server did not start within 20 s; stderr:\n%s", stderr.String())
	}

	go func() {
		err := cmd.Wait()
		proc.mu.Lock()
		proc.waitErr = err
		proc.mu.Unlock()
		close(proc.done)
	}()
	t.Cleanup(func() {
		select {
		case <-proc.done:
			return
		default:
		}
		_ = cmd.Process.Kill()
		<-proc.done
	})
	return proc
}

// terminate sends SIGTERM and returns the wall time until the process exited.
func (p *serverProcess) terminate(t *testing.T) (time.Duration, error) {
	t.Helper()
	start := time.Now()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("SIGTERM: %v (stderr:\n%s)", err, p.stderr.String())
	}
	select {
	case <-p.done:
		return time.Since(start), p.waitErrOf()
	case <-time.After(10 * time.Second):
		t.Fatalf("server did not exit after SIGTERM; stderr:\n%s", p.stderr.String())
		return 0, nil
	}
}

// getJSON performs a GET and decodes the JSON answer.
func getJSON(t *testing.T, url string, into any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d: %s", url, resp.StatusCode, body)
	}
	if into != nil {
		if err := json.Unmarshal(body, into); err != nil {
			t.Fatalf("decode %s: %v (%s)", url, err, body)
		}
	}
}

// postJSON performs a POST with a JSON body and decodes the JSON answer.
func postJSON(t *testing.T, url string, payload any, wantStatus int, into any) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		body = bytes.NewReader(data)
	}
	resp, err := http.Post(url, "application/json", body)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s = %d, want %d: %s", url, resp.StatusCode, wantStatus, data)
	}
	if into != nil {
		if err := json.Unmarshal(data, into); err != nil {
			t.Fatalf("decode %s: %v (%s)", url, err, data)
		}
	}
}

// waitFor polls until condition is true or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, what string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

// sessionInfo mirrors the subset of sessions.Info the e2e assertions need.
type sessionInfo struct {
	ID     string `json:"id"`
	CWD    string `json:"cwd"`
	Status string `json:"status"`
	PID    int    `json:"pid"`
	Name   string `json:"name"`
}

// sessionList is the GET /api/v1/sessions body.
type sessionList struct {
	Sessions []sessionInfo `json:"sessions"`
}

// newWSClient dials /ws/v1, sends hello and waits for welcome.
func newWSClient(t *testing.T, addr string) *wsConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws://" + addr + "/ws/v1"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	conn.SetReadLimit(4 << 20)
	client := &wsConn{conn: conn}
	client.write(t, `{"type":"hello","v":1,"client":{"name":"pi-ui-e2e","version":"0.1.0"}}`)
	client.readUntil(t, 10*time.Second, "welcome")
	t.Cleanup(func() { _ = conn.CloseNow() })
	return client
}

// wsConn is a minimal frame-level WebSocket client for the assertions.
type wsConn struct {
	conn *websocket.Conn
}

func (c *wsConn) write(t *testing.T, frame string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
		t.Fatalf("ws write: %v", err)
	}
}

// readFrame reads one JSON frame.
func (c *wsConn) readFrame(t *testing.T, timeout time.Duration) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	typ, data, err := c.conn.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("ws read: frame type %v, want text", typ)
	}
	var frame map[string]any
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("ws frame %q: %v", data, err)
	}
	return frame
}

// readUntil reads frames until one of the wanted types arrives, failing on
// anything else after the timeout.
func (c *wsConn) readUntil(t *testing.T, timeout time.Duration, wanted ...string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		frame := c.readFrame(t, time.Until(deadline))
		got, _ := frame["type"].(string)
		for _, want := range wanted {
			if got == want {
				return frame
			}
		}
	}
	t.Fatalf("no frame of type %v within %s", wanted, timeout)
	return nil
}
