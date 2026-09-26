package ws

import (
	"context"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// connection is one accepted /ws/v1 socket.
//
// Writes are serialized through a single bounded queue and a single writer
// goroutine, because a WebSocket connection allows one concurrent writer and
// because the queue is what turns "this client is too slow" into a defined
// behaviour instead of growing memory: when the queue is full the connection is
// closed with a server.error{code:"slow_consumer"} frame.
type connection struct {
	hub    *hub
	ws     *websocket.Conn
	ctx    context.Context
	cancel context.CancelFunc

	// readCtx is the context of the read loop and the handshake, cancelled as soon
	// as the socket starts closing. Without it the reader would stay blocked until
	// the library's own closing handshake gives up on a peer that stopped reading,
	// and the hub would keep counting a connection that is already gone.
	readCtx    context.Context
	cancelRead context.CancelFunc
	clientName string // from hello, diagnostics only

	// who is the identity the handshake accepted this socket with. It never changes:
	// a scope change means a new token and a new connection.
	who principal

	sendMu      sync.Mutex
	send        chan []byte
	failed      bool // no further frames are accepted
	closeCode   websocket.StatusCode
	closeReason string

	mu   sync.Mutex
	subs map[string]*subscription

	wg sync.WaitGroup // writer and heartbeat goroutines
}

// newConnection prepares the connection state. start must be called before any
// frame is enqueued, so the writer is already draining the queue.
func newConnection(h *hub, ws *websocket.Conn, ctx context.Context, cancel context.CancelFunc, who principal) *connection {
	readCtx, cancelRead := context.WithCancel(ctx)
	return &connection{
		hub:        h,
		ws:         ws,
		ctx:        ctx,
		cancel:     cancel,
		readCtx:    readCtx,
		cancelRead: cancelRead,
		who:        who,
		send:       make(chan []byte, max(1, h.opts.SendBuffer)),
		subs:       map[string]*subscription{},
	}
}

// start launches the writer and the heartbeat.
func (c *connection) start() {
	c.wg.Add(2)
	go c.writeLoop()
	go c.heartbeat()
}

// wait blocks until the writer and the heartbeat are done, with a bound.
//
// The bound matters for two reasons: the writer may be inside a Write that only
// fails when its own deadline expires, and the library's closing handshake waits up
// to five seconds for a peer that is not going to answer. Neither may be inherited
// by Close, whose budget is two seconds (docs/spike-interfaces.md §12); whatever is
// still running finishes on its own and cannot reach the connection any more, since
// the queue is closed and the hub has already forgotten it.
func (c *connection) wait() {
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(closeGrace):
	}
}

// enqueue appends one already-marshalled text frame. It never blocks: the caller
// is a publisher or the read loop, and neither may stall on a single client.
func (c *connection) enqueue(frame []byte) bool {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	if len(frame) == 0 || c.failed {
		return false
	}
	select {
	case c.send <- frame:
		return true
	default:
		// The subscriber is not keeping up. Drop the backlog we could not deliver
		// and spend the room on one terminal frame that says why, so the client
		// can tell "the server could not keep up with me" from a dropped socket.
		c.failLocked(mustMarshalEvent(c.hub.errorEvent(codeSlowConsumer, "subscriber is not keeping up")),
			websocket.StatusPolicyViolation, "slow consumer")
		return false
	}
}

// shutdown closes the connection without a terminal frame. Queued frames are
// dropped: the connection is going away and the client cannot act on them.
func (c *connection) shutdown() {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	c.failLocked(nil, websocket.StatusNormalClosure, "")
}

// shutdownWith closes the connection with a specific close status, used by the
// heartbeat. No terminal frame is queued: a peer that stopped reading would not
// see it anyway.
func (c *connection) shutdownWith(code websocket.StatusCode, reason string) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	c.failLocked(nil, code, reason)
}

// failLocked performs the one-way transition to a closed queue. Callers must hold
// sendMu; the first caller wins so the close status cannot keep changing.
func (c *connection) failLocked(terminal []byte, code websocket.StatusCode, reason string) {
	if c.failed {
		return
	}
	c.failed = true
	c.closeCode, c.closeReason = code, reason
	if terminal != nil {
		drain(c.send)
		c.send <- terminal
	}
	close(c.send)
}

// closingCode returns the status the writer must close with.
func (c *connection) closingCode() (websocket.StatusCode, string) {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	if c.closeCode == 0 {
		return websocket.StatusNormalClosure, ""
	}
	return c.closeCode, c.closeReason
}

// drain empties a queue without blocking.
func drain(ch chan []byte) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// writeLoop is the only writer of the socket.
func (c *connection) writeLoop() {
	defer c.wg.Done()

	for frame := range c.send {
		ctx, cancel := context.WithTimeout(c.ctx, c.hub.opts.WriteTimeout)
		err := c.ws.Write(ctx, websocket.MessageText, frame)
		cancel()
		if err != nil {
			// The peer is gone or wedged: stop writing, unblock the read loop and
			// let serveConn do the bookkeeping. A terminal frame may still be queued
			// behind this write, but a socket that cannot take one more frame cannot
			// deliver it either; the close status carries the reason instead.
			c.abort()
			return
		}
	}
	code, reason := c.closingCode()
	c.closeSocket(code, reason)
	// The socket is closed (or the grace ran out): the reader has nothing left to
	// read, and serveConn can do its bookkeeping now instead of later.
	c.cancelRead()
	c.cancel()
}

// closeSocket performs the closing handshake with a bounded grace period, then
// drops the connection.
//
// The bound matters: Close waits up to five seconds for the peer's close frame, and
// the peers that reach this path are exactly the ones that stopped reading (a slow
// consumer, a missed heartbeat, a server shutdown next to a wedged client). Shutdown
// must not wait for a peer that is already gone, so after closeGrace the socket is
// dropped without it.
func (c *connection) closeSocket(code websocket.StatusCode, reason string) {
	closing := make(chan struct{})
	go func() {
		defer close(closing)
		_ = c.ws.Close(code, reason)
	}()

	select {
	case <-closing:
		return
	case <-time.After(closeHandshakeGrace):
	}

	// The close frame is on the wire, but the peer is not answering the handshake.
	// Cancelling the read context closes the socket underneath it (the library watches
	// the context of the in-flight read and closes the connection with it), which ends
	// the handshake now instead of waiting the library's own five seconds. CloseNow is
	// not an option here: it waits for the very close it cannot hurry along.
	c.cancelRead()
	select {
	case <-closing:
	case <-time.After(closeHandshakeGrace):
	}
}

// abort tears the connection down after a write failure.
func (c *connection) abort() {
	c.sendMu.Lock()
	c.failed = true
	c.sendMu.Unlock()

	_ = c.ws.CloseNow()
	c.cancel()
}

// heartbeat keeps the connection honest: one WebSocket ping per interval and a
// close after maxMissedPongs unanswered ones.
//
// The ping is a protocol-level control frame, not a JSON frame, so it stays
// invisible to the frame schema and every standards-compliant client (a browser
// included) answers it without application code. The JSON ping/pong frames are
// the client-initiated counterpart and are answered by route.
func (c *connection) heartbeat() {
	defer c.wg.Done()

	interval := c.hub.opts.Heartbeat
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	misses := 0
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
		}

		ctx, cancel := context.WithTimeout(c.ctx, interval)
		err := c.ws.Ping(ctx)
		cancel()
		if err == nil {
			misses = 0
			continue
		}
		if c.ctx.Err() != nil {
			return
		}
		misses++
		if misses >= maxMissedPongs {
			c.shutdownWith(websocket.StatusPolicyViolation, "heartbeat timeout")
			return
		}
	}
}

// readLoop reads frames until the connection dies and routes each one. Only text
// frames carry the protocol; a binary frame is ignored (the read limit already
// bounds its size) instead of closing a connection over a client bug.
func (c *connection) readLoop() {
	for {
		typ, data, err := c.ws.Read(c.readCtx)
		if err != nil {
			return
		}
		if typ != websocket.MessageText {
			continue
		}
		c.hub.route(c, data)
	}
}

// subscribe registers this connection as a subscriber of one session and returns
// the fresh subscription, which starts holding.
//
// The hub is updated before the local map so the lock order stays h.mu → c.mu; an
// event published in between lands in the ring and is therefore covered by the
// replay snapshot, so it cannot be lost, and the dedup cursor drops it when the
// replay delivers it too.
func (c *connection) subscribe(sessionID string) *subscription {
	c.hub.addSubscriber(sessionID, c)

	sub := newSubscription(c, sessionID)

	c.mu.Lock()
	previous := c.subs[sessionID]
	c.subs[sessionID] = sub
	c.mu.Unlock()

	if previous != nil {
		previous.stop()
	}
	return sub
}

// unsubscribe removes one subscription and cancels any replay still running for
// it. Unsubscribing twice, or unsubscribing from a session this connection never
// subscribed to, is a no-op: a client must not have to track server state.
func (c *connection) unsubscribe(sessionID string) {
	c.hub.removeSubscriber(sessionID, c)

	c.mu.Lock()
	sub := c.subs[sessionID]
	delete(c.subs, sessionID)
	c.mu.Unlock()

	if sub != nil {
		sub.stop()
	}
}

// deliver hands one published event to the subscription of its session, if any.
func (c *connection) deliver(h *hub, ev Event) {
	c.mu.Lock()
	sub := c.subs[ev.SessionID]
	c.mu.Unlock()

	if sub != nil {
		sub.deliverLive(h, ev)
	}
}

// deliverServer writes a server-wide event (no session) to this connection. Such
// events are never replayed and never deduplicated: they are observations about
// the server, not part of a session stream.
func (c *connection) deliverServer(ev Event) {
	frame, err := marshalEvent(ev)
	if err != nil {
		return
	}
	c.enqueue(frame)
}

// stopAll cancels every replay and drops every subscription of this connection.
func (c *connection) stopAll() {
	c.mu.Lock()
	subs := make([]*subscription, 0, len(c.subs))
	for sessionID, sub := range c.subs {
		subs = append(subs, sub)
		delete(c.subs, sessionID)
	}
	c.mu.Unlock()

	for _, sub := range subs {
		sub.stop()
	}
}
