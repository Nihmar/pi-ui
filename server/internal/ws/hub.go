package ws

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/Nihmar/pi-ui/server/internal/protocol/gen"
)

// hub is the concrete Hub and Requester of one server process.
//
// Lock order, which the whole package is written against:
//
//	h.mu  →  c.mu (connection subscriptions)  →  sub.mu (replay state)  →  c.sendMu
//
// Nothing takes h.mu while holding one of the others, which is what makes the
// publisher path (Publish → deliver → enqueue) safe next to the per-subscription
// replay goroutines without a global lock around the socket.
type hub struct {
	opts     Options
	frames   *jsonschema.Schema // client frames: the document root of schemas/ws.json
	request  *jsonschema.Schema // one WsRequest, for outbound dialog validation
	schemaID string

	startedAt time.Time
	seq       atomic.Uint64

	// handshakeTimeout is copied from the package variable at construction, so a
	// live connection never observes a value changing under it.
	handshakeTimeout time.Duration

	mu sync.Mutex
	// rings is one bounded history per session (docs/spike-interfaces.md §5.2): a
	// shared ring would make every session's replay window shrink with the number of
	// sessions publishing, and the spike's memory ceiling is bounded by the session
	// limit anyway. Server-wide events have no session and are never replayable, so
	// they are not kept at all.
	rings     map[string]*ring
	conns     map[*connection]struct{}
	bySession map[string]map[*connection]struct{}
	cmd       CommandHandler
	dialog    DialogHandler
	replayer  Replayer
	closed    bool

	// connWG tracks the goroutines owned by a connection plus the heartbeat loop:
	// Close waits for those, because draining connections is Close's contract.
	connWG sync.WaitGroup
	// handlerWG tracks handler calls. Close deliberately does not wait for them:
	// handlers are cancelled through their context, and a handler that ignores
	// cancellation must not be able to hold shutdown hostage (see the shutdown
	// budget in the acceptance matrix, docs/spike-interfaces.md §12).
	handlerWG sync.WaitGroup

	done chan struct{}
}

// The hub is both interfaces; the assertions keep them from drifting apart.
var (
	_ Hub       = (*hub)(nil)
	_ Requester = (*hub)(nil)
)

// New builds the WebSocket hub of one server process.
//
// It never fails, so the wiring in cli/serve stays a single line: the frame
// schemas are compiled from the embedded, generated schemas/ws.json, and a problem
// there is a build-time defect of the repository (a missing server/scripts/gen.sh
// run, or a schema whose refs do not resolve), not a runtime condition. It panics
// with the reason instead of degrading into a server that accepts frames it cannot
// validate.
func New(o Options) Hub {
	o = withDefaults(o)
	frames, request, schemaID := compileFrames()

	h := &hub{
		opts:             o,
		frames:           frames,
		request:          request,
		schemaID:         schemaID,
		startedAt:        time.Now(),
		rings:            map[string]*ring{},
		handshakeTimeout: handshakeTimeout,
		conns:            map[*connection]struct{}{},
		bySession:        map[string]map[*connection]struct{}{},
		done:             make(chan struct{}),
	}

	h.connWG.Add(1)
	go h.heartbeatLoop()
	return h
}

// withDefaults fills the zero fields of Options and copies the collections, so a
// caller cannot change a running hub by mutating the values it passed in.
func withDefaults(o Options) Options {
	if o.ReplayEvents <= 0 {
		o.ReplayEvents = defaultReplayEvents
	}
	if o.ReplayWindow <= 0 {
		o.ReplayWindow = defaultReplayWindow
	}
	if o.Heartbeat <= 0 {
		o.Heartbeat = defaultHeartbeat
	}
	if o.WriteTimeout <= 0 {
		o.WriteTimeout = defaultWriteTimeout
	}
	if o.SendBuffer <= 0 {
		o.SendBuffer = defaultSendBuffer
	}
	o.AllowHosts = slices.Clone(o.AllowHosts)
	o.AllowOrigins = slices.Clone(o.AllowOrigins)
	o.Features = slices.Clone(o.Features)
	o.Limits = maps.Clone(o.Limits)
	if o.Features == nil {
		// The welcome schema requires an array: "no optional features" is an empty
		// list, not a missing field.
		o.Features = []string{}
	}
	if o.Limits == nil {
		o.Limits = map[string]any{}
	}
	return o
}

// compileFrames compiles the embedded ws schema once per hub: the document root
// (the union of client frames) for inbound validation, and the single WsRequest
// definition for outbound dialog frames.
func compileFrames() (*jsonschema.Schema, *jsonschema.Schema, string) {
	data, ok := gen.SchemaJSON("ws")
	if !ok {
		panic("ws: schemas/ws.json is not embedded; run server/scripts/gen.sh")
	}
	var meta struct {
		ID string `json:"$id"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		panic(fmt.Sprintf("ws: schemas/ws.json is not valid JSON: %v", err))
	}
	if meta.ID == "" {
		panic("ws: schemas/ws.json has no $id; refs cannot resolve")
	}

	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		panic(fmt.Sprintf("ws: decode schemas/ws.json: %v", err))
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(meta.ID, doc); err != nil {
		panic(fmt.Sprintf("ws: register schema %s: %v", meta.ID, err))
	}
	frames, err := compiler.Compile(meta.ID)
	if err != nil {
		panic(fmt.Sprintf("ws: compile %s: %v", meta.ID, err))
	}
	request, err := compiler.Compile(meta.ID + "#/$defs/WsRequest")
	if err != nil {
		panic(fmt.Sprintf("ws: compile %s#/$defs/WsRequest: %v", meta.ID, err))
	}
	return frames, request, meta.ID
}

// nextSeq assigns the next global sequence number. It is a plain atomic counter:
// Publish is called from every session goroutine, and the number must stay
// monotonic across all of them without a lock that would serialize publishers.
func (h *hub) nextSeq() uint64 {
	return h.seq.Add(1)
}

// raiseSeq keeps the counter ahead of a publisher that supplied its own seq, so a
// client can never see a sequence number go backwards.
func (h *hub) raiseSeq(seq uint64) {
	for {
		current := h.seq.Load()
		if current >= seq || h.seq.CompareAndSwap(current, seq) {
			return
		}
	}
}

// timestamp is the ts of an event the publisher left empty.
func (h *hub) timestamp() string {
	return time.Now().UTC().Format(tsLayout)
}

// localEvent builds an event the hub emits to one subscriber only: it takes a seq
// from the global counter so the client's cursor keeps moving, but it never enters
// the ring and is never fanned out.
func (h *hub) localEvent(eventType, sessionID string, payload json.RawMessage) Event {
	return Event{
		Type:      eventType,
		SessionID: sessionID,
		Seq:       h.nextSeq(),
		TS:        h.timestamp(),
		Payload:   payload,
	}
}

// errorEvent is a server.error event, the shape both the slow-consumer close and
// the replay failures use.
func (h *hub) errorEvent(code, message string) Event {
	return Event{
		Type: EventError,
		Seq:  h.nextSeq(),
		TS:   h.timestamp(),
		Payload: mustMarshal(struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}{Code: code, Message: message}),
	}
}

// Publish assigns the global Seq and the ts of an event, keeps it for replay and
// fans it out to the subscribers of its session. It returns the Seq, or 0 when the
// hub is closed or the event carries no type.
//
// It is safe to call from any goroutine and never blocks on a slow client: the
// per-connection queue turns that case into a disconnect instead.
func (h *hub) Publish(ev Event) uint64 {
	if ev.Type == "" {
		// Nothing to route and nothing a client could name: a publisher bug, not a
		// wire condition. Dropping it keeps a background goroutine from taking the
		// process down over a missing field.
		return 0
	}
	h.stamp(&ev)
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return 0
	}
	if ev.SessionID == "" {
		// A server-wide fact (heartbeat, startup diagnostics) is not part of any
		// session stream: every connection gets it, none of them can replay it.
		for c := range h.conns {
			c.deliverServer(ev)
		}
		return ev.Seq
	}
	h.sessionRing(ev.SessionID).add(ev, now)
	for c := range h.bySession[ev.SessionID] {
		c.deliver(h, ev)
	}
	return ev.Seq
}

// stamp fills the fields the hub owns on an event: a global seq when the publisher
// supplied none (and a forward-only bump when it did), a timestamp, and the entry id
// a pi.entry_appended record carries in its payload. The live path and the durable
// replay both use it, so an event looks the same however it reached a client.
func (h *hub) stamp(ev *Event) {
	if ev.Seq == 0 {
		ev.Seq = h.nextSeq()
	} else {
		h.raiseSeq(ev.Seq)
	}
	if ev.TS == "" {
		ev.TS = h.timestamp()
	}
	if ev.EntryID == "" && ev.Type == EventEntryAppended {
		ev.EntryID = entryIDOf(ev.Payload)
	}
}

// sessionRing returns the replay history of one session, creating it on first use.
// Callers must hold h.mu.
func (h *hub) sessionRing(sessionID string) *ring {
	r, ok := h.rings[sessionID]
	if !ok {
		r = newRing(h.opts.ReplayEvents, h.opts.ReplayWindow)
		h.rings[sessionID] = r
	}
	return r
}

// entryIDOf reads payload.entry.id out of a pi.entry_appended record, so clients
// get a durable replay cursor without re-implementing pi's payload shape.
func entryIDOf(payload json.RawMessage) string {
	var decoded struct {
		Entry struct {
			ID string `json:"id"`
		} `json:"entry"`
	}
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return ""
	}
	return decoded.Entry.ID
}

// SendRequest implements Requester: it writes one extension UI `request` frame to
// the current subscribers of a session.
func (h *hub) SendRequest(sessionID string, frame json.RawMessage) error {
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(frame))
	if err != nil {
		return fmt.Errorf("ws: request frame is not valid JSON: %w", err)
	}
	if err := h.request.Validate(doc); err != nil {
		return fmt.Errorf("ws: request frame does not match %s#/$defs/WsRequest: %w", h.schemaID, err)
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return errors.New("ws: hub is closed")
	}
	targets := make([]*connection, 0, len(h.bySession[sessionID]))
	for c := range h.bySession[sessionID] {
		targets = append(targets, c)
	}
	h.mu.Unlock()

	// A dialog is not an event: no seq, no ring, no dedup. It is written as it is
	// and stays answerable until the dialog times out.
	for _, c := range targets {
		c.enqueue(frame)
	}
	return nil
}

// SetReplayer installs the durable replay seam (sessions performs it with
// get_entries). Passing nil makes `since.entryId` answer "unsupported".
func (h *hub) SetReplayer(r Replayer) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.replayer = r
}

// SetCommandHandler installs the handler of client `command` frames.
func (h *hub) SetCommandHandler(c CommandHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.cmd = c
}

// SetDialogHandler installs the handler of client `ui_response` frames.
func (h *hub) SetDialogHandler(d DialogHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.dialog = d
}

// handlers returns the current handler slots. They are read under the lock so a
// running server can be reconfigured, and copied out so a handler call never holds
// h.mu for its duration.
func (h *hub) handlers() (CommandHandler, DialogHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.cmd, h.dialog
}

// replayerOf returns the durable replay seam, if one is installed.
func (h *hub) replayerOf() Replayer {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.replayer
}

// counts reports the live connection and subscribed-session counts for the
// heartbeat payload.
func (h *hub) counts() (clients, sessions int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	return len(h.conns), len(h.bySession)
}

// addConn registers a handshaken connection. It returns false when the hub is
// already closed, in which case the caller must not serve the socket.
func (h *hub) addConn(c *connection) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return false
	}
	h.conns[c] = struct{}{}
	return true
}

// removeConn forgets a connection and every subscription it had.
func (h *hub) removeConn(c *connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	delete(h.conns, c)
	for sessionID, subs := range h.bySession {
		delete(subs, c)
		if len(subs) == 0 {
			delete(h.bySession, sessionID)
		}
	}
}

// addSubscriber records that a connection wants the events of a session.
func (h *hub) addSubscriber(sessionID string, c *connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}
	if h.bySession[sessionID] == nil {
		h.bySession[sessionID] = map[*connection]struct{}{}
	}
	h.bySession[sessionID][c] = struct{}{}
}

// removeSubscriber drops one connection from the subscriber set of a session.
func (h *hub) removeSubscriber(sessionID string, c *connection) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subs, ok := h.bySession[sessionID]
	if !ok {
		return
	}
	delete(subs, c)
	if len(subs) == 0 {
		delete(h.bySession, sessionID)
	}
}

// heartbeatLoop publishes the server-wide heartbeat event. The per-connection
// keepalive is a different thing (connection.heartbeat): this one is the
// observation clients render, that one is what decides a peer is gone.
func (h *hub) heartbeatLoop() {
	defer h.connWG.Done()

	ticker := time.NewTicker(h.opts.Heartbeat)
	defer ticker.Stop()

	for {
		var tick time.Time
		select {
		case <-h.done:
			return
		case tick = <-ticker.C:
		}
		clients, sessions := h.counts()
		h.Publish(Event{
			Type: EventHeartbeat,
			Payload: mustMarshal(struct {
				UptimeSec int `json:"uptimeSec"`
				Clients   int `json:"clients"`
				Sessions  int `json:"sessions"`
			}{
				UptimeSec: int(tick.Sub(h.startedAt).Seconds()),
				Clients:   clients,
				Sessions:  sessions,
			}),
		})
		h.sweepRings(tick)
	}
}

// sweepRings releases the replay history of sessions that have no subscribers and
// nothing left to replay. Without it a long-running server that churns sessions
// would keep one ring per session forever; with it, a ring lives until it is older
// than ReplayWindow and nobody is watching, which is exactly the point at which it
// can no longer serve a replay anyway.
//
// It runs on the heartbeat tick rather than in Publish, so the publisher path
// stays O(subscribers) and never walks the sessions that are not involved. A ring
// released while a replay goroutine still holds its pointer is harmless: the
// replay finishes on the old ring and later events start a new one.
func (h *hub) sweepRings(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		return
	}
	for sessionID, r := range h.rings {
		if len(h.bySession[sessionID]) > 0 {
			continue
		}
		if !r.reusable(now) {
			delete(h.rings, sessionID)
		}
	}
}

// Close drains every connection and stops the heartbeat.
//
// It is idempotent: the second call returns immediately. The error is always nil
// in the spike — a socket that fails to close cleanly leaves nothing the caller
// could act on, and the signature is shared with rpc.Bridge.Close so shutdown is
// one loop over the components.
func (h *hub) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	close(h.done)
	conns := make([]*connection, 0, len(h.conns))
	for c := range h.conns {
		conns = append(conns, c)
	}
	h.mu.Unlock()

	for _, c := range conns {
		c.shutdown()
	}
	// Bounded like connection.wait: a client that stopped reading must not be able to
	// push shutdown past its budget, and by now every connection is out of the
	// registry and has a closed queue, so nothing can reach it any more.
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.connWG.Wait()
	}()
	select {
	case <-done:
	case <-time.After(closeGrace):
	}
	return nil
}
