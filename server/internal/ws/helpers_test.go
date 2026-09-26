package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// testReadLimit is the client-side read limit of the tests: big enough for the
// large frames the slow-consumer test publishes.
const testReadLimit = 4 << 20

// testTimeout bounds every read in the tests: long enough to survive a loaded CI
// machine, short enough that a protocol bug fails the suite instead of hanging it.
const testTimeout = 5 * time.Second

// testServer is one hub behind a real HTTP listener on loopback, which is what the
// handshake rules are written against (RemoteAddr, LocalAddrContextKey, Origin) —
// an in-process handler would not exercise them.
type testServer struct {
	t   *testing.T
	hub *hub
	srv *httptest.Server
}

func newTestHub(t *testing.T, mutate func(*Options)) *testServer {
	t.Helper()

	opts := Options{
		// The keepalive is effectively off unless a test lowers it: a wrong pong
		// count would otherwise close connections in the middle of unrelated
		// assertions.
		Heartbeat:     time.Minute,
		ReplayEvents:  32,
		ReplayWindow:  time.Minute,
		SendBuffer:    8,
		WriteTimeout:  2 * time.Second,
		ServerVersion: "0.0.1-test",
		PiVersion:     "0.87.1",
		Features:      []string{"bridge"},
		Limits:        map[string]any{"maxSessions": 8},
	}
	if mutate != nil {
		mutate(&opts)
	}

	impl := New(opts).(*hub)
	srv := httptest.NewServer(impl)
	t.Cleanup(func() {
		_ = impl.Close()
		srv.Close()
	})
	return &testServer{t: t, hub: impl, srv: srv}
}

// wsURL is the URL of the /ws/v1 endpoint of this test server.
func (ts *testServer) wsURL() string {
	return "ws" + strings.TrimPrefix(ts.srv.URL, "http") + path
}

// testClient is one dialed socket with read helpers that fail the test instead of
// returning a bare error, so an assertion reads like the protocol it checks.
type testClient struct {
	t    *testing.T
	conn *websocket.Conn
	ctx  context.Context
}

// dial opens a connection and returns it without completing the handshake, so a
// test can decide what the first frame is.
func (ts *testServer) dial(mutate func(*websocket.DialOptions)) *testClient {
	ts.t.Helper()

	client, resp, err := ts.dialRaw(mutate)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		ts.t.Fatalf("dial failed (status %d): %v", status, err)
	}
	return client
}

// dialRaw exposes the HTTP response of a refused handshake.
func (ts *testServer) dialRaw(mutate func(*websocket.DialOptions)) (*testClient, *http.Response, error) {
	opts := &websocket.DialOptions{}
	if mutate != nil {
		mutate(opts)
	}
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	conn, resp, err := websocket.Dial(ctx, ts.wsURL(), opts)
	if err != nil {
		return nil, resp, err
	}
	// The tests publish frames far larger than a library default, so the client
	// limit is raised the same way the server raises its own.
	conn.SetReadLimit(testReadLimit)
	return &testClient{t: ts.t, conn: conn, ctx: context.Background()}, resp, nil
}

// hello completes the handshake and returns the welcome frame.
func (c *testClient) hello() map[string]any {
	c.t.Helper()

	c.sendRaw(`{"type":"hello","v":1,"client":{"name":"pi-ui-test","version":"0.0.1"}}`)
	frame := c.next()
	if frame["type"] != frameWelcome {
		c.t.Fatalf("first frame after hello = %v, want %s", frame, frameWelcome)
	}
	return frame
}

func (c *testClient) sendRaw(raw string) {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(c.ctx, testTimeout)
	defer cancel()
	if err := wsjson.Write(ctx, c.conn, json.RawMessage(raw)); err != nil {
		c.t.Fatalf("send %s: %v", raw, err)
	}
}

// send marshals v and writes it as one text frame.
func (c *testClient) send(v any) {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(c.ctx, testTimeout)
	defer cancel()
	if err := wsjson.Write(ctx, c.conn, v); err != nil {
		c.t.Fatalf("send %v: %v", v, err)
	}
}

// read returns the next frame, or the read error when the connection is gone.
func (c *testClient) read() (map[string]any, error) {
	ctx, cancel := context.WithTimeout(c.ctx, testTimeout)
	defer cancel()

	var frame map[string]any
	err := wsjson.Read(ctx, c.conn, &frame)
	return frame, err
}

// next returns the next frame and fails the test when the read fails.
func (c *testClient) next() map[string]any {
	c.t.Helper()

	frame, err := c.read()
	if err != nil {
		c.t.Fatalf("read: %v", err)
	}
	return frame
}

// waitFor reads frames until one has the wanted type, reporting the types it saw
// in between so a failure shows what actually happened.
func (c *testClient) waitFor(want string) map[string]any {
	c.t.Helper()

	deadline := time.Now().Add(testTimeout)
	var seen []string
	for time.Now().Before(deadline) {
		frame, err := c.read()
		if err != nil {
			c.t.Fatalf("waiting for %q: %v (saw %v)", want, err, seen)
		}
		got, _ := frame["type"].(string)
		if got == want {
			return frame
		}
		seen = append(seen, got)
	}
	c.t.Fatalf("waiting for %q: timed out (saw %v)", want, seen)
	return nil
}

// expectClose asserts that the connection ends with the wanted status code, which
// is how the hub refuses a handshake it can no longer answer with HTTP.
func (c *testClient) expectClose(want websocket.StatusCode) {
	c.t.Helper()

	_, err := c.read()
	if err == nil {
		c.t.Fatal("expected the connection to be closed, got a frame")
	}
	if got := websocket.CloseStatus(err); got != want {
		c.t.Fatalf("close status = %v (%v), want %v", got, err, want)
	}
}

// expectClosed asserts that the connection is gone, whatever status carried it.
// Frames the server had already queued (a heartbeat event, for example) are read
// through first, so the assertion is about the close and not about timing.
func (c *testClient) expectClosed() {
	c.t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if _, err := c.read(); err != nil {
			return
		}
	}
	c.t.Fatal("expected the connection to be closed, got nothing but frames")
}

func (c *testClient) close() {
	_ = c.conn.CloseNow()
}

// seqOf reads the seq of an event frame.
func seqOf(t *testing.T, frame map[string]any) uint64 {
	t.Helper()

	seq, ok := frame["seq"].(float64)
	if !ok {
		t.Fatalf("frame %v has no numeric seq", frame)
	}
	return uint64(seq)
}

// payloadOf reads the payload object of an event frame.
func payloadOf(t *testing.T, frame map[string]any) map[string]any {
	t.Helper()

	payload, ok := frame["payload"].(map[string]any)
	if !ok {
		t.Fatalf("frame %v has no payload object", frame)
	}
	return payload
}

// errorCodeOf reads the code of a failed response or of a server.error event. A
// frame that carries neither returns "", which is how the tests assert success.
func errorCodeOf(t *testing.T, frame map[string]any) string {
	t.Helper()

	if errObj, ok := frame["error"].(map[string]any); ok {
		code, _ := errObj["code"].(string)
		return code
	}
	payload, ok := frame["payload"].(map[string]any)
	if !ok {
		return ""
	}
	code, _ := payload["code"].(string)
	return code
}

// eventually waits for a condition another goroutine has to observe, such as the
// connection counters after a disconnect.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(testTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// stubCommandHandler is a CommandHandler that records the commands it saw.
type stubCommandHandler struct {
	calls []Command
	fn    func(ctx context.Context, c Command) (json.RawMessage, error)
}

func (s *stubCommandHandler) Handle(ctx context.Context, c Command) (json.RawMessage, error) {
	s.calls = append(s.calls, c)
	return s.fn(ctx, c)
}

// stubDialogHandler records the answers that reached it.
type stubDialogHandler struct {
	calls []dialogCall
	err   error
}

type dialogCall struct {
	sessionID string
	requestID string
	response  json.RawMessage
}

func (s *stubDialogHandler) Respond(_ context.Context, sessionID, requestID string, response json.RawMessage) error {
	s.calls = append(s.calls, dialogCall{sessionID: sessionID, requestID: requestID, response: response})
	return s.err
}

// stubReplayer is the durable replay seam of the tests.
type stubReplayer struct {
	calls int
	fn    func(ctx context.Context, sessionID, entryID string, emit func(Event)) (bool, error)
}

func (s *stubReplayer) ReplayFromEntry(ctx context.Context, sessionID, entryID string, emit func(Event)) (bool, error) {
	s.calls++
	return s.fn(ctx, sessionID, entryID, emit)
}

// codedStubError mirrors sessions.CodedError: the code and the client-facing
// message travel through the two methods the hub looks for.
type codedStubError struct {
	code    string
	message string
	err     error
}

func (e *codedStubError) Error() string        { return e.message }
func (e *codedStubError) ErrorCode() string    { return e.code }
func (e *codedStubError) ErrorMessage() string { return e.message }
func (e *codedStubError) Unwrap() error        { return e.err }

var errStubPlain = errors.New("stub: plain failure")
