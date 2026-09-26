package ws

import (
	"context"
	"encoding/json"
	"testing"
	"time"
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

// TestSlowConsumerIsDisconnected: a subscriber that stops reading is told why
// instead of being starved of events without an explanation.
func TestSlowConsumerIsDisconnected(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, func(o *Options) {
		o.SendBuffer = 1
		o.WriteTimeout = 200 * time.Millisecond
	})

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	subscribe(t, ts, client, `{"type":"subscribe","sessionId":"`+session+`","replay":false}`, 1)

	// Payloads large enough that the socket buffers fill while the reader is stalled:
	// the writer blocks, the one-slot queue overflows, and the hub drops the backlog.
	payload := make([]byte, 64<<10)
	for i := range payload {
		payload[i] = 'x'
	}
	big := json.RawMessage(`{"delta":"` + string(payload) + `"}`)

	// The reader is the slow one: it stalls long enough for the publisher to fill the
	// queue and the socket, then resumes. That is the shape of a real slow consumer,
	// and it is why the terminal frame can still be delivered at all.
	var (
		observedSlowConsumer bool
		readerDone           = make(chan struct{})
	)
	go func() {
		defer close(readerDone)
		time.Sleep(150 * time.Millisecond)
		for {
			frame, err := client.read()
			if err != nil {
				return
			}
			if frame["type"] == EventError && errorCodeOf(t, frame) == codeSlowConsumer {
				observedSlowConsumer = true
				return
			}
		}
	}()

	for i := 0; i < 400; i++ {
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: big})
	}

	<-readerDone
	if !observedSlowConsumer {
		t.Fatal("the slow subscriber was dropped without a slow_consumer frame")
	}
	eventually(t, "the slow subscriber to be dropped", func() bool {
		clients, _ := ts.hub.counts()
		return clients == 0
	})
}
