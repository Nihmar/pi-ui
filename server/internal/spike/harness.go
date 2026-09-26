package spike

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/Nihmar/pi-ui/server/internal/api"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// clientReadLimit is the largest frame a measuring client accepts. Event payloads
// are pi records (a --big record is the largest one), so a few MiB are plenty; the
// library default of 32 KiB would close the socket on a large pi record.
const clientReadLimit = 4 << 20

// clientSetupTimeout bounds dial, hello/welcome and the subscribe barrier.
const clientSetupTimeout = 15 * time.Second

// shutdownBudget bounds the child reaping of a harness teardown: it matches the
// two second shutdown budget of acceptance criterion C8 with a little slack.
const shutdownBudget = 3 * time.Second

// harness is the in-process server one throughput measurement drives: the real
// rpc → sessions → ws pipeline behind an httptest listener, with the deterministic
// fake-pi children as the event source.
type harness struct {
	hub        ws.Hub
	supervisor *sessions.Manager
	server     *httptest.Server
	ids        []string
	pids       []int
	cleanup    func()
}

// newHarness wires hub, supervisor and router exactly like `pi-ui serve` does and
// starts cfg.Sessions children, each in its own working directory. emit decides
// whether the children stream synthetic events (throughput) or stay idle (server
// RSS measurement).
func newHarness(ctx context.Context, cfg Config, emit bool) (*harness, error) {
	executable, err := cfg.requireFakePi()
	if err != nil {
		return nil, err
	}

	command := []string{executable}
	if emit {
		command = append(command, "--emit", strconv.Itoa(cfg.Events),
			"--rate", strconv.FormatFloat(cfg.Rate, 'g', -1, 64))
	}

	hub := ws.New(ws.Options{
		ServerVersion: "spike",
		Features:      []string{},
		Limits:        map[string]any{},
	})
	supervisor := sessions.New(sessions.Config{
		PiCommand:   command,
		MaxSessions: max(cfg.Sessions, sessions.DefaultMaxSessions),
		Logger:      cfg.logger(),
		Hub:         hub,
	})
	hub.SetCommandHandler(supervisor)
	hub.SetDialogHandler(supervisor)
	hub.SetReplayer(supervisor)

	router := api.NewRouter(api.Options{
		Supervisor: supervisor,
		Hub:        hub,
		Info: api.ServerInfo{
			Version:  "spike",
			Protocol: api.Protocol,
			Features: []string{},
			Limits:   map[string]any{},
		},
		Auth: api.NewLoopbackOrToken(""),
	})

	root, remove, err := cfg.workRoot("spike-throughput-")
	if err != nil {
		return nil, err
	}
	h := &harness{hub: hub, supervisor: supervisor, server: httptest.NewServer(router)}
	h.cleanup = func() {
		_ = hub.Close()
		h.server.Close()
		remove()
	}

	for index := 0; index < cfg.Sessions; index++ {
		dir, dirErr := sessionDir(root, index)
		if dirErr != nil {
			h.shutdown()
			return nil, dirErr
		}
		info, startErr := supervisor.Start(ctx, sessions.Spec{
			CWD:  dir,
			Name: fmt.Sprintf("spike-%02d", index),
		})
		if startErr != nil {
			h.shutdown()
			return nil, fmt.Errorf("spike: start session %d: %w", index, startErr)
		}
		h.ids = append(h.ids, info.ID)
		h.pids = append(h.pids, info.PID)
	}
	return h, nil
}

// close releases the server, the hub and the work directory. Safe to call twice.
func (h *harness) close() {
	if h.cleanup == nil {
		return
	}
	cleanup := h.cleanup
	h.cleanup = nil
	cleanup()
}

// shutdown reaps the children with their own budget and then releases the server,
// so a measurement never leaves fake-pi processes behind.
func (h *harness) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
	defer cancel()
	_ = h.supervisor.Shutdown(ctx)
	h.close()
}

// wsURL is the /ws/v1 endpoint of the harness listener.
func (h *harness) wsURL() string {
	return "ws" + strings.TrimPrefix(h.server.URL, "http") + "/ws/v1"
}

// inboundFrame is the routing view of one server frame: the event type itself
// (pi.*, server.*, ext.*), the session it belongs to and its verbatim payload.
type inboundFrame struct {
	Type      string          `json:"type"`
	SessionID string          `json:"sessionId"`
	Payload   json.RawMessage `json:"payload"`
}

// spikeNS extracts the fake-pi latency probe {"spike":{"i","ns"}} from a record.
func spikeNS(payload json.RawMessage) (int64, bool) {
	if len(payload) == 0 {
		return 0, false
	}
	var record struct {
		Spike *struct {
			NS int64 `json:"ns"`
		} `json:"spike"`
	}
	if err := json.Unmarshal(payload, &record); err != nil || record.Spike == nil {
		return 0, false
	}
	return record.Spike.NS, true
}

// client is one measuring WebSocket client: it completes the hello handshake,
// subscribes every session live-only and counts the events that arrive.
type client struct {
	index int
	conn  *websocket.Conn

	mu        sync.Mutex
	events    int       // pi.* events received
	latencies []float64 // end-to-end samples of spike-marked records
	err       error

	expected int
	complete chan struct{} // closed once events reached expected
	once     sync.Once
	finished chan struct{} // closed when the reader loop returned
}

// write sends one text frame with its own short deadline, so a stalled server
// fails the measurement instead of hanging it.
func (c *client) write(ctx context.Context, frame string) error {
	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.conn.Write(writeCtx, websocket.MessageText, []byte(frame)); err != nil {
		return fmt.Errorf("spike: client %d write: %w", c.index, err)
	}
	return nil
}

// counts returns the event count and the latency samples taken so far.
func (c *client) counts() (int, []float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.events, append([]float64(nil), c.latencies...)
}

// readErr returns the first reader error, if any.
func (c *client) readErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

func (c *client) fail(err error) {
	c.mu.Lock()
	if c.err == nil {
		c.err = err
	}
	c.mu.Unlock()
}

// readLoop counts events until the connection or the context ends. A connection
// that dies unfulfilled completes too, so the caller reports the loss instead of
// waiting for the whole timeout.
func (c *client) readLoop(ctx context.Context) {
	defer close(c.finished)
	for {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			c.fail(err)
			c.once.Do(func() { close(c.complete) })
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		c.observe(data)
	}
}

// observe accounts for one frame: only pi.* records count as session events, and
// only records carrying the fake-pi probe contribute an end-to-end sample. A
// `server.error` disconnecting this client as a slow consumer completes it, so a
// measurement reports the loss instead of hanging.
func (c *client) observe(data []byte) {
	var frame inboundFrame
	if err := json.Unmarshal(data, &frame); err != nil {
		return
	}
	if frame.Type == "server.error" {
		var payload struct {
			Code string `json:"code"`
		}
		if err := json.Unmarshal(frame.Payload, &payload); err == nil && payload.Code == "slow_consumer" {
			c.fail(fmt.Errorf("spike: client %d was disconnected as a slow consumer", c.index))
			c.once.Do(func() { close(c.complete) })
		}
		return
	}
	if !strings.HasPrefix(frame.Type, "pi.") {
		return
	}
	latency, marked := 0.0, false
	if frame.Type == "pi.message_update" {
		if ns, ok := spikeNS(frame.Payload); ok {
			latency = float64(time.Since(time.Unix(0, ns))) / float64(time.Millisecond)
			marked = true
		}
	}

	c.mu.Lock()
	c.events++
	if marked {
		c.latencies = append(c.latencies, latency)
	}
	done := c.events >= c.expected
	c.mu.Unlock()
	if done {
		c.once.Do(func() { close(c.complete) })
	}
}

// awaitFrame reads until want arrives, failing loudly on anything else: after
// hello the server's next frames are fixed by the protocol, so a different frame
// is a server defect worth naming.
func (c *client) awaitFrame(ctx context.Context, want string) error {
	for {
		typ, data, err := c.conn.Read(ctx)
		if err != nil {
			return fmt.Errorf("spike: client %d waiting for %s: %w", c.index, want, err)
		}
		if typ != websocket.MessageText {
			continue
		}
		var frame inboundFrame
		if err := json.Unmarshal(data, &frame); err != nil {
			return fmt.Errorf("spike: client %d waiting for %s: unreadable frame %q", c.index, want, data)
		}
		if frame.Type == want {
			return nil
		}
		return fmt.Errorf("spike: client %d expected %s, got %s", c.index, want, frame.Type)
	}
}

// connectClient dials one client and gets it ready: hello → welcome → subscribe
// (live-only) for every session → ping → pong. The pong is the barrier that proves
// the subscriptions are registered before prompts are sent.
func connectClient(ctx context.Context, url string, ids []string, index, expected int) (*client, error) {
	setupCtx, cancel := context.WithTimeout(ctx, clientSetupTimeout)
	defer cancel()

	conn, _, err := websocket.Dial(setupCtx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("spike: client %d dial: %w", index, err)
	}
	conn.SetReadLimit(clientReadLimit)
	c := &client{
		index:    index,
		conn:     conn,
		expected: expected,
		complete: make(chan struct{}),
		finished: make(chan struct{}),
	}

	hello := `{"type":"hello","v":1,"client":{"name":"pi-ui-spike","version":"0.1.0"}}`
	if err := c.write(ctx, hello); err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	if err := c.awaitFrame(setupCtx, "welcome"); err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	for _, id := range ids {
		frame := fmt.Sprintf(`{"type":"subscribe","sessionId":%q,"replay":false}`, id)
		if err := c.write(ctx, frame); err != nil {
			_ = conn.CloseNow()
			return nil, err
		}
	}
	if err := c.write(ctx, `{"type":"ping"}`); err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	if err := c.awaitFrame(setupCtx, "pong"); err != nil {
		_ = conn.CloseNow()
		return nil, err
	}
	return c, nil
}

// connectClients opens cfg.Clients ready clients and starts their reader loops.
func (h *harness) connectClients(ctx context.Context, cfg Config, expected int) ([]*client, context.CancelFunc, error) {
	runCtx, cancel := context.WithCancel(ctx)
	clients := make([]*client, 0, cfg.Clients)
	for index := 0; index < cfg.Clients; index++ {
		c, err := connectClient(runCtx, h.wsURL(), h.ids, index, expected)
		if err != nil {
			cancel()
			for _, opened := range clients {
				_ = opened.conn.CloseNow()
			}
			return nil, nil, err
		}
		clients = append(clients, c)
	}
	for _, c := range clients {
		go c.readLoop(runCtx)
	}
	return clients, cancel, nil
}

// closeClients shuts every client down and waits for its reader loop, so no
// goroutine outlives the measurement.
func closeClients(clients []*client, cancel context.CancelFunc) {
	cancel()
	for _, c := range clients {
		_ = c.conn.Close(websocket.StatusNormalClosure, "measurement done")
	}
	for _, c := range clients {
		select {
		case <-c.finished:
		case <-time.After(2 * time.Second):
		}
	}
}
