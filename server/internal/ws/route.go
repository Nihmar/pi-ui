package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/coder/websocket"

	"github.com/Nihmar/pi-ui/server/internal/audit"
)

// serveConn runs the hello handshake and then the read loop of one accepted
// socket. It returns when the connection is done, having drained the writer and
// released every subscription.
func (h *hub) serveConn(wsConn *websocket.Conn, who principal) {
	// The request context dies with the hijacked request, so the connection gets
	// its own, cancelled once the socket is done.
	ctx, cancel := context.WithCancel(context.Background())
	conn := newConnection(h, wsConn, ctx, cancel, who)

	if err := h.handshake(conn); err != nil {
		// The socket is already upgraded, so the refusal travels as the 4401 close
		// status that mirrors the HTTP response of a rejected handshake.
		_ = wsConn.Close(websocket.StatusCode(closeCodeUnauthorized), err.Error())
		_ = wsConn.CloseNow()
		cancel()
		return
	}

	if !h.addConn(conn) {
		_ = wsConn.Close(websocket.StatusGoingAway, "server is shutting down")
		_ = wsConn.CloseNow()
		cancel()
		return
	}
	defer func() {
		conn.stopAll()
		// The shells of a gone socket are gone with it: a browser tab that closes
		// must not leave a process tree running on the host.
		h.releaseTerminals(conn)
		conn.shutdown()
		// The hub forgets the connection before waiting for the socket to finish
		// closing: counts and fan-out must be correct immediately, and the library's
		// own closing handshake may still be waiting for a peer that stopped reading.
		h.removeConn(conn)
		conn.wait()
		cancel()
	}()

	conn.start()
	conn.enqueue(h.marshalWelcome())
	conn.readLoop()
}

// handshake reads hello, the only frame accepted before the connection is served.
//
// Everything that can be decided before the upgrade (token, Host, Origin) has
// already been decided; this is about what the client claims to speak, so a wrong
// version or a missing hello is answered with the same 4401 close as an
// unauthorized peer.
func (h *hub) handshake(c *connection) error {
	ctx, cancel := context.WithTimeout(c.ctx, h.handshakeTimeout)
	defer cancel()

	// A binary frame is not the protocol, so it is skipped here exactly like the read
	// loop skips one after the handshake; the deadline still bounds a peer that sends
	// nothing but binary frames.
	var data []byte
	for {
		typ, frame, err := c.ws.Read(ctx)
		if err != nil {
			return errors.New("hello must be the first frame")
		}
		if typ == websocket.MessageText {
			data = frame
			break
		}
	}
	if !h.validateFrame(data) {
		if version, ok := helloVersion(data); ok && version != protocolVersion {
			return fmt.Errorf("protocol version %d is not supported (this server speaks v%d)", version, protocolVersion)
		}
		return errors.New("hello does not match schemas/ws.json")
	}
	var frame inbound
	if err := json.Unmarshal(data, &frame); err != nil {
		return errors.New("hello could not be decoded")
	}
	if frame.Type != frameHello {
		return fmt.Errorf("hello must be the first frame, got %q", frame.Type)
	}
	if frame.Client != nil {
		c.clientName = frame.Client.Name
	}
	return nil
}

// helloVersion reads the claimed protocol version out of a frame that failed
// validation. The schema pins `v` to the version this server speaks, so a
// well-formed v2 hello can only be recognised before the schema gate; this keeps
// the close reason useful for a client that upgraded its protocol instead of
// reporting a generic schema mismatch.
func helloVersion(data []byte) (int, bool) {
	var frame struct {
		Type string `json:"type"`
		V    *int   `json:"v"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		return 0, false
	}
	if frame.Type != frameHello || frame.V == nil {
		return 0, false
	}
	return *frame.V, true
}

// route validates one inbound frame and dispatches it.
//
// Validation is the gate: no handler ever sees a frame that does not match
// schemas/ws.json. A rejected frame is answered with bad_request when it carries
// an id — a client waiting for a response must never be left with a pending
// command — and dropped otherwise, because there is nothing to correlate.
func (h *hub) route(c *connection, raw []byte) {
	if !h.validateFrame(raw) {
		if id := frameID(raw); id != "" {
			c.enqueue(marshalErrorResponse(id, codeBadRequest, "frame does not match schemas/ws.json"))
		}
		return
	}

	var frame inbound
	if err := json.Unmarshal(raw, &frame); err != nil {
		return
	}

	switch frame.Type {
	case framePing:
		c.enqueue(marshalPong())
	case frameSubscribe:
		h.handleSubscribe(c, frame)
	case frameUnsubscribe:
		c.unsubscribe(frame.SessionID)
	case frameCommand:
		h.handleCommand(c, frame)
	case frameUIResponse:
		h.handleUIResponse(c, frame)
	case frameTerminalOpen:
		h.handleTerminalOpen(c, frame)
	case frameTerminalInput:
		h.handleTerminalInput(c, frame)
	case frameTerminalResize:
		h.handleTerminalResize(c, frame)
	case frameTerminalClose:
		h.handleTerminalClose(c, frame)
	case frameHello:
		// Only the first frame may be hello. A second one is ignored: it has no id
		// to answer, and renegotiating would change the meaning of everything
		// already sent on this connection.
	}
}

// handleSubscribe registers a subscription and starts its replay, if the client
// asked for one.
func (h *hub) handleSubscribe(c *connection, frame inbound) {
	cursor, err := parseCursor(frame.Since)
	if err != nil {
		// The schema guarantees the cursor shape, so this branch is defensive: the
		// frame carries no id and there is nothing to answer, so it is dropped.
		return
	}

	sub := c.subscribe(frame.SessionID)
	if frame.Replay != nil && !*frame.Replay {
		// Live-only subscriber: nothing to catch up on, so the subscription is
		// released right away. It starts holding, so nothing published while it was
		// being registered is lost.
		sub.release(h)
		return
	}
	h.startReplay(sub, cursor)
}

// handleCommand dispatches one command frame to the injected handler.
//
// The handler runs in its own goroutine: a long command (a prompt that streams)
// must not stop the connection from processing an abort, which is exactly the
// command a user sends while one is running.
func (h *hub) handleCommand(c *connection, frame inbound) {
	if !c.who.scope.allows(ScopeOperator) {
		// A read-only connection still gets the terminal answer the protocol promises
		// for its id: it learns why, and nothing is dispatched.
		h.auditDenied(c, frame.SessionID, frame.Op, "scope")
		c.enqueue(marshalErrorResponse(frame.ID, codeForbiddenScope, "this connection is read-only"))
		return
	}
	handler, _ := h.handlers()
	if handler == nil {
		c.enqueue(marshalErrorResponse(frame.ID, codeUnsupported, "no command handler is configured"))
		return
	}

	cmd := Command{ID: frame.ID, SessionID: frame.SessionID, Op: frame.Op, Payload: frame.Payload}
	h.spawnHandler(func() {
		data, err := handler.Handle(c.ctx, cmd)
		if err != nil {
			h.auditCommand(c, cmd, audit.OutcomeError)
			code, message := codeOf(err)
			c.enqueue(marshalErrorResponse(cmd.ID, code, message))
			return
		}
		h.auditCommand(c, cmd, audit.OutcomeOK)
		c.enqueue(marshalHandlerData(cmd.ID, data))
	})
}

// handleUIResponse dispatches one dialog answer to the injected handler.
//
// The handler receives only the answer object ({value} | {confirmed} |
// {cancelled}), not the whole frame: it is the same contract a REST dialog
// endpoint would use, and the request id arrives as its own argument. The response
// frame echoes the dialog id, not a client command id.
func (h *hub) handleUIResponse(c *connection, frame inbound) {
	if !c.who.scope.allows(ScopeOperator) {
		// Answering a dialog drives the run, so a viewer cannot do it; the dialog id
		// correlates the refusal, and the dialog itself stays open for a real operator.
		h.auditDenied(c, frame.SessionID, "ui_response", "scope")
		c.enqueue(marshalErrorResponse(frame.ID, codeForbiddenScope, "this connection is read-only"))
		return
	}
	_, handler := h.handlers()
	if handler == nil {
		c.enqueue(marshalErrorResponse(frame.ID, codeUnsupported, "no dialog handler is configured"))
		return
	}

	answer := frame.answer()
	h.spawnHandler(func() {
		if err := handler.Respond(c.ctx, frame.SessionID, frame.ID, answer); err != nil {
			code, message := codeOf(err)
			c.enqueue(marshalErrorResponse(frame.ID, code, message))
			return
		}
		c.enqueue(marshalResponse(frame.ID, nil))
	})
}

// spawnHandler tracks one handler call. Handler goroutines are counted but never
// awaited by Close (see hub.handlerWG): they are cancelled through the connection
// context, and one that ignores cancellation must not be able to delay shutdown.
func (h *hub) spawnHandler(fn func()) {
	h.handlerWG.Add(1)
	go func() {
		defer h.handlerWG.Done()
		fn()
	}()
}
