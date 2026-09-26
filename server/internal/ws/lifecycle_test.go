package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// TestHeartbeatEventIsPublished proves the server-wide observation reaches every
// connected client without a subscription, and carries the counts.
func TestHeartbeatEventIsPublished(t *testing.T) {
	ts := newTestHub(t, func(o *Options) { o.Heartbeat = 20 * time.Millisecond })

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	event := client.waitFor(EventHeartbeat)
	payload := payloadOf(t, event)
	if _, ok := payload["uptimeSec"].(float64); !ok {
		t.Fatalf("heartbeat payload %v has no uptimeSec", payload)
	}
	if clients, ok := payload["clients"].(float64); !ok || clients < 1 {
		t.Fatalf("heartbeat payload clients = %v, want at least this connection", payload["clients"])
	}
	if _, ok := payload["sessions"].(float64); !ok {
		t.Fatalf("heartbeat payload %v has no sessions", payload)
	}
	if _, present := event["sessionId"]; present {
		t.Fatalf("heartbeat carries a sessionId: %v", event)
	}
}

// TestHeartbeatClosesASilentPeer: three unanswered pings end the connection, which
// is how a half-open TCP connection stops holding a subscriber slot.
//
// Nothing reads from the client here, and in coder/websocket only a read processes a
// control frame, so the pings stay unanswered — the client's own reader would pop
// them off and pong, which is exactly what the reading case below covers.
func TestHeartbeatClosesASilentPeer(t *testing.T) {
	ts := newTestHub(t, func(o *Options) { o.Heartbeat = 25 * time.Millisecond })

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	eventually(t, "the silent connection to be dropped", func() bool {
		clients, _ := ts.hub.counts()
		return clients == 0
	})
	client.expectClosed()
}

// TestHeartbeatKeepsAReadingClientAlive: a client that keeps reading answers the
// pings and survives many intervals.
func TestHeartbeatKeepsAReadingClientAlive(t *testing.T) {
	ts := newTestHub(t, func(o *Options) { o.Heartbeat = 20 * time.Millisecond })

	client := ts.dial(nil)
	defer client.close()
	client.hello()

	// Several heartbeat intervals: the hub publishes one event per tick, so the reads
	// return promptly and the client library answers the pings in between.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := client.read(); err != nil {
			t.Fatalf("connection closed while the client was reading: %v", err)
		}
	}

	client.sendRaw(`{"type":"ping"}`)
	if got := client.next()["type"]; got != framePong {
		t.Fatalf("after several heartbeats the answer was %v, want %s", got, framePong)
	}
}

// TestCloseDrainsConnections: Close is the shutdown path, so it has to end the
// sockets it owns rather than just stopping the fan-out.
func TestCloseDrainsConnections(t *testing.T) {
	ts := newTestHub(t, nil)

	first := ts.dial(nil)
	defer first.close()
	first.hello()
	second := ts.dial(nil)
	defer second.close()
	second.hello()

	eventually(t, "both connections to be registered", func() bool {
		clients, _ := ts.hub.counts()
		return clients == 2
	})

	if err := ts.hub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	first.expectClosed()
	second.expectClosed()

	eventually(t, "the connection registry to be empty", func() bool {
		clients, _ := ts.hub.counts()
		return clients == 0
	})
}

// TestHandlerContextIsCancelledOnDisconnect: handlers are cancelled through the
// connection, which is how a detached prompt stops when the client goes away.
func TestHandlerContextIsCancelledOnDisconnect(t *testing.T) {
	cancelled := make(chan struct{})
	handler := &stubCommandHandler{fn: func(ctx context.Context, _ Command) (json.RawMessage, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}}
	ts := newTestHub(t, nil)
	ts.hub.SetCommandHandler(handler)

	client := ts.dial(nil)
	client.hello()
	client.sendRaw(`{"type":"command","id":"c1","sessionId":"s_0123456789abcdef","op":"session.prompt","payload":{"message":"hi"}}`)
	client.close()

	select {
	case <-cancelled:
	case <-time.After(testTimeout):
		t.Fatal("the handler context was not cancelled when the client disconnected")
	}
}

// TestSlowConsumerIsDisconnected: a subscriber that stops reading is told why and
// disconnected. The trigger is the connection's own failed state, not how much the kernel
// socket buffer absorbed: the client never reads until the one-slot queue has overflowed,
// which is what makes the test independent of the host's buffering.
func TestSlowConsumerIsDisconnected(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, func(o *Options) {
		o.SendBuffer = 1
		// Long enough that the queue decides the outcome, not a write deadline.
		o.WriteTimeout = 30 * time.Second
	})

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	subscribe(t, ts, client, `{"type":"subscribe","sessionId":"`+session+`","replay":false}`, 1)
	conn := onlyConnection(t, ts.hub)

	// Payloads large enough that the socket fills while the reader is stalled: the
	// writer blocks, the one-slot queue overflows, and the hub fails the connection.
	payload := make([]byte, 64<<10)
	for i := range payload {
		payload[i] = 'x'
	}
	big := json.RawMessage(`{"delta":"` + string(payload) + `"}`)

	// The cap is far more than any socket buffer absorbs, so the loop cannot spin.
	for i := 0; i < 4096 && !connectionFailed(conn); i++ {
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: big})
	}
	if !connectionFailed(conn) {
		t.Fatal("the one-slot queue never overflowed")
	}

	// The terminal frame is queued behind the backlog the writer already wrote.
	for {
		frame, err := client.read()
		if err != nil {
			t.Fatalf("reading the terminal frame: %v", err)
		}
		if frame["type"] == EventError && errorCodeOf(t, frame) == codeSlowConsumer {
			break
		}
	}
	eventually(t, "the slow subscriber to be dropped", func() bool {
		clients, _ := ts.hub.counts()
		return clients == 0
	})
}

// TestEnqueueOverflowQueuesTheSlowConsumerFrame pins the overflow decision without a
// socket: the one-slot queue fills, the next frame fails the connection, the terminal
// server.error{slow_consumer} is queued and the close status is 1008.
func TestEnqueueOverflowQueuesTheSlowConsumerFrame(t *testing.T) {
	ts := newTestHub(t, nil)
	conn := &connection{hub: ts.hub, ctx: context.Background(), send: make(chan []byte, 1)}
	frame := []byte(`{"type":"pong"}`)

	if !conn.enqueue(frame) {
		t.Fatal("the first frame was refused although the queue had room")
	}
	if conn.enqueue(frame) {
		t.Fatal("the second frame was accepted although the queue was full")
	}
	if code, reason := conn.closingCode(); code != websocket.StatusPolicyViolation || reason != "slow consumer" {
		t.Fatalf("close = %v %q, want %d slow consumer", code, reason, websocket.StatusPolicyViolation)
	}
	terminal, ok := <-conn.send
	if !ok {
		t.Fatal("no terminal frame was queued")
	}
	var decoded map[string]any
	if err := json.Unmarshal(terminal, &decoded); err != nil {
		t.Fatalf("terminal frame is not JSON: %v", err)
	}
	if decoded["type"] != EventError || errorCodeOf(t, decoded) != codeSlowConsumer {
		t.Fatalf("terminal frame = %v, want server.error{slow_consumer}", decoded)
	}
}

// connectionFailed reports whether the connection has entered its closing state.
func connectionFailed(c *connection) bool {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.failed
}

// onlyConnection returns the one registered connection, waiting for it to register.
func onlyConnection(t *testing.T, h *hub) *connection {
	t.Helper()

	eventually(t, "the connection to register", func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		return len(h.conns) == 1
	})
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.conns {
		return c
	}
	return nil
}
