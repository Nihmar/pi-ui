package ws

import (
	"context"
	"encoding/base64"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// fakeTerminal is a terminal service that records what the hub asked of it.
type fakeTerminal struct {
	mu       sync.Mutex
	opened   []Terminal
	inputs   map[string][]byte
	resizes  []termResize
	closed   []termClose
	openErr  error
	lastCols uint16
	lastRows uint16
}

type termResize struct {
	id   string
	cols uint16
	rows uint16
}

type termClose struct {
	id     string
	reason string
}

func newFakeTerminal() *fakeTerminal {
	return &fakeTerminal{inputs: map[string][]byte{}}
}

func (f *fakeTerminal) TerminalOpen(_ context.Context, dir string, cols, rows uint16) (Terminal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.openErr != nil {
		return Terminal{}, f.openErr
	}
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 32
	}
	terminal := Terminal{ID: "t_0001", CWD: dir, PID: 4242, Cols: cols, Rows: rows}
	f.opened = append(f.opened, terminal)
	f.lastCols, f.lastRows = cols, rows
	return terminal, nil
}

func (f *fakeTerminal) TerminalInput(_ context.Context, terminalID string, data []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.inputs[terminalID] = append(f.inputs[terminalID], data...)
	return nil
}

func (f *fakeTerminal) TerminalResize(_ context.Context, terminalID string, cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.resizes = append(f.resizes, termResize{id: terminalID, cols: cols, rows: rows})
	return nil
}

func (f *fakeTerminal) TerminalClose(_ context.Context, terminalID string, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.closed = append(f.closed, termClose{id: terminalID, reason: reason})
	return nil
}

func (f *fakeTerminal) snapshot() (opened int, inputs string, resizes []termResize, closed []termClose) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var data []byte
	for _, chunk := range f.inputs {
		data = append(data, chunk...)
	}
	return len(f.opened), string(data), append([]termResize(nil), f.resizes...), append([]termClose(nil), f.closed...)
}

// openTerminal sends a terminal.open frame and returns the response frame.
func openTerminal(t *testing.T, client *testClient, id, dir string) map[string]any {
	t.Helper()

	client.send(map[string]any{"type": "terminal.open", "id": id, "dir": dir, "cols": 100, "rows": 40})
	return client.waitFor(frameResponse)
}

func TestTerminalOpenAnswersWithTheTerminal(t *testing.T) {
	terminals := newFakeTerminal()
	server := newTestHub(t, nil)
	server.hub.SetTerminalHandler(terminals)
	client := server.dial(nil)
	client.hello()

	response := openTerminal(t, client, "c1", "/srv/project")
	if ok, _ := response["ok"].(bool); !ok {
		t.Fatalf("open response = %v, want ok", response)
	}
	data, _ := response["data"].(map[string]any)
	if data["terminalId"] != "t_0001" || data["cwd"] != "/srv/project" {
		t.Fatalf("open data = %v", data)
	}
	if data["cols"] != float64(100) || data["rows"] != float64(40) {
		t.Fatalf("the size should be echoed: %v", data)
	}
}

func TestTerminalInputResizeAndCloseReachTheHandler(t *testing.T) {
	terminals := newFakeTerminal()
	server := newTestHub(t, nil)
	server.hub.SetTerminalHandler(terminals)
	client := server.dial(nil)
	client.hello()
	openTerminal(t, client, "c1", "/srv/project")

	client.send(map[string]any{
		"type":       "terminal.input",
		"terminalId": "t_0001",
		"data":       base64.StdEncoding.EncodeToString([]byte("ls\n")),
	})
	if response := client.waitFor(frameResponse); response["type"] != frameResponse {
		t.Fatalf("input response = %v", response)
	}

	client.send(map[string]any{
		"type": "terminal.resize", "terminalId": "t_0001", "cols": 80, "rows": 24,
	})
	client.waitFor(frameResponse)

	client.send(map[string]any{"type": "terminal.close", "terminalId": "t_0001"})
	client.waitFor(frameResponse)

	opened, input, resizes, closed := terminals.snapshot()
	if opened != 1 || input != "ls\n" {
		t.Fatalf("opened=%d input=%q", opened, input)
	}
	if len(resizes) != 1 || resizes[0].cols != 80 || resizes[0].rows != 24 {
		t.Fatalf("resizes = %+v", resizes)
	}
	if len(closed) != 1 || closed[0].reason != "client" {
		t.Fatalf("closes = %+v", closed)
	}
}

func TestTerminalInputOfAnotherConnectionIsNotFound(t *testing.T) {
	terminals := newFakeTerminal()
	server := newTestHub(t, nil)
	server.hub.SetTerminalHandler(terminals)
	owner := server.dial(nil)
	owner.hello()
	openTerminal(t, owner, "c1", "/srv/project")

	other := server.dial(nil)
	other.hello()
	other.send(map[string]any{
		"type":       "terminal.input",
		"terminalId": "t_0001",
		"data":       base64.StdEncoding.EncodeToString([]byte("rm -rf /\n")),
	})
	response := other.waitFor(frameResponse)
	if ok, _ := response["ok"].(bool); ok {
		t.Fatalf("another connection must not type into it: %v", response)
	}
	if code := errorCodeOf(t, response); code != codeNotFound {
		t.Fatalf("code = %q, want %q", code, codeNotFound)
	}
}

func TestTerminalOutputGoesToTheOwnerOnly(t *testing.T) {
	terminals := newFakeTerminal()
	server := newTestHub(t, nil)
	server.hub.SetTerminalHandler(terminals)
	owner := server.dial(nil)
	owner.hello()
	openTerminal(t, owner, "c1", "/srv/project")

	other := server.dial(nil)
	other.hello()

	server.hub.TerminalOutput("t_0001", []byte("hello from the shell"))
	output := owner.waitFor(frameTerminalOutput)
	if output["terminalId"] != "t_0001" {
		t.Fatalf("output frame = %v", output)
	}
	decoded, err := base64.StdEncoding.DecodeString(output["data"].(string))
	if err != nil || string(decoded) != "hello from the shell" {
		t.Fatalf("decoded output = %q (%v)", decoded, err)
	}

	// The other connection sees nothing: no output frame arrived while the owner got
	// its own, which is asserted by the absence of any terminal frame.
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var frame map[string]any
	err = wsjson.Read(ctx, other.conn, &frame)
	if err == nil {
		t.Fatalf("a non-owner received %v", frame)
	}
}

func TestTerminalClosedIsSentOnceAndForgets(t *testing.T) {
	terminals := newFakeTerminal()
	server := newTestHub(t, nil)
	server.hub.SetTerminalHandler(terminals)
	client := server.dial(nil)
	client.hello()
	openTerminal(t, client, "c1", "/srv/project")

	server.hub.TerminalClosed("t_0001", 3, "exit")
	closed := client.waitFor(frameTerminalClosed)
	if closed["exitCode"] != float64(3) || closed["reason"] != "exit" {
		t.Fatalf("closed frame = %v", closed)
	}

	// A second notification is not sent, and the output of a forgotten terminal goes
	// nowhere.
	server.hub.TerminalClosed("t_0001", 3, "exit")
	server.hub.TerminalOutput("t_0001", []byte("late"))

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	var frame map[string]any
	if err := wsjson.Read(ctx, client.conn, &frame); err == nil {
		t.Fatalf("expected nothing more, got %v", frame)
	}
}

func TestTerminalOfAGoneConnectionIsClosedAsOwner(t *testing.T) {
	terminals := newFakeTerminal()
	server := newTestHub(t, nil)
	server.hub.SetTerminalHandler(terminals)
	client := server.dial(nil)
	client.hello()
	openTerminal(t, client, "c1", "/srv/project")

	_ = client.conn.Close(websocket.StatusNormalClosure, "done")

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		fn := func() bool {
			_, _, _, closed := terminals.snapshot()
			return len(closed) > 0
		}
		if fn() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, _, _, closed := terminals.snapshot()
	if len(closed) != 1 || closed[0].reason != "owner" {
		t.Fatalf("closes = %+v, want one owner close", closed)
	}
}

func TestTerminalNeedsOperatorScope(t *testing.T) {
	terminals := newFakeTerminal()
	server := newTestHub(t, func(o *Options) {
		o.Authorizer = funcAuthorizer(func(*http.Request) (Scope, string, error) {
			return ScopeViewer, "d_viewer", nil
		})
	})
	server.hub.SetTerminalHandler(terminals)
	client := server.dial(nil)
	client.hello()

	client.send(map[string]any{"type": "terminal.open", "id": "c1", "dir": "/srv/project"})
	response := client.waitFor(frameResponse)
	if ok, _ := response["ok"].(bool); ok {
		t.Fatal("a viewer must not open a terminal")
	}
	if code := errorCodeOf(t, response); code != codeForbiddenScope {
		t.Fatalf("code = %q", code)
	}
	if opened, _, _, _ := terminals.snapshot(); opened != 0 {
		t.Fatal("no terminal may be started for a viewer")
	}
}

func TestTerminalWithoutHandlerIsUnsupported(t *testing.T) {
	server := newTestHub(t, nil)
	client := server.dial(nil)
	client.hello()

	client.send(map[string]any{"type": "terminal.input", "terminalId": "t_1", "data": ""})
	response := client.waitFor(frameResponse)
	if code := errorCodeOf(t, response); code != codeUnsupported {
		t.Fatalf("code = %q", code)
	}
}

func TestTerminalFrameWithABadSizeIsRefused(t *testing.T) {
	terminals := newFakeTerminal()
	server := newTestHub(t, nil)
	server.hub.SetTerminalHandler(terminals)
	client := server.dial(nil)
	client.hello()

	// The schema bounds the size, so this frame never reaches the handler.
	client.send(map[string]any{"type": "terminal.open", "id": "c1", "dir": "/srv", "cols": 99999})
	response := client.waitFor(frameResponse)
	if code := errorCodeOf(t, response); code != codeBadRequest {
		t.Fatalf("code = %q", code)
	}
	if opened, _, _, _ := terminals.snapshot(); opened != 0 {
		t.Fatal("nothing may start from an invalid frame")
	}
}
