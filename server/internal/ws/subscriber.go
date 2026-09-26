package ws

import (
	"context"
	"sort"
	"sync"

	"github.com/coder/websocket"
)

// subscription is one connection's feed of one session.
//
// Every subscriber has exactly one subscription per session, and every
// subscription owns the two pieces of state that make replay safe:
//
//   - holding: while a replay runs, live events of that session are buffered
//     instead of written, so the subscriber always sees
//     server.replay.begin → replayed events → server.replay.end → live events
//     even though the replay is served from another goroutine.
//   - lastSent: the highest seq already written to this subscriber. It is the
//     dedup cursor: an event may legitimately arrive twice (once from the
//     durable replay and once as a live event published while the replay was
//     being prepared), and the client must not see it twice.
//
// A subscription is created holding, before anything can be delivered to it;
// whoever creates it must therefore finish with either endReplay or release,
// otherwise the connection would never see live events again.
type subscription struct {
	conn      *connection
	sessionID string
	ctx       context.Context
	cancel    context.CancelFunc

	mu       sync.Mutex
	holding  bool
	pending  []Event
	lastSent uint64
	overflow bool
}

// newSubscription creates a subscription that is holding from the start.
func newSubscription(conn *connection, sessionID string) *subscription {
	ctx, cancel := context.WithCancel(conn.ctx)
	return &subscription{conn: conn, sessionID: sessionID, ctx: ctx, cancel: cancel, holding: true}
}

// stop cancels a replay that is still running. It is safe to call more than once
// and from any goroutine.
func (s *subscription) stop() {
	s.cancel()
}

// deliverLive hands one published event to this subscriber: buffered while a
// replay runs, written straight to the queue otherwise.
func (s *subscription) deliverLive(h *hub, ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.holding {
		s.sendLocked(h, ev, true)
		return
	}
	if len(s.pending) >= h.opts.SendBuffer {
		// A replay must not turn into an unbounded buffer: the subscriber is not
		// keeping up with the session it is trying to catch up on.
		s.overflow = true
		return
	}
	s.pending = append(s.pending, ev)
}

// sendLocked writes one event to the connection. advance is false for the meta
// frames of the replay protocol (begin/end/error): they are bookkeeping for this
// subscriber, so they must not move the dedup cursor and drop a live event the
// client still needs.
func (s *subscription) sendLocked(h *hub, ev Event, advance bool) {
	if advance && ev.Seq != 0 && ev.Seq <= s.lastSent {
		return
	}
	frame, err := marshalEvent(ev)
	if err != nil {
		// The publisher handed us a payload we cannot encode; report it instead of
		// losing the event silently. The error event carries its own seq, so it
		// cannot be confused with the event it replaces.
		s.conn.enqueue(mustMarshalEvent(h.errorEvent(codeInternal, "event payload could not be encoded")))
		return
	}
	if !s.conn.enqueue(frame) {
		return
	}
	if advance {
		s.lastSent = ev.Seq
	}
}

// beginReplay opens the replay sequence on this subscription and keeps holding,
// so live events that arrive from now on are buffered.
//
// The buffer already collected is deliberately kept: a subscription is created
// holding, so events published between its registration and this call are in it,
// and the ring snapshot (or the durable replay) was taken before at least some of
// them — clearing the buffer here would drop those events without a trace. An
// event the replay does cover again is dropped by the seq dedup in sendLocked
// instead, which is the case this buffer is not needed for.
func (s *subscription) beginReplay(h *hub, direction string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.holding = true
	s.sendLocked(h, h.localEvent(EventReplayBegin, s.sessionID, replayBeginPayload(direction)), false)
}

// replayEvent writes one replayed event. seq and ts are filled in by the caller
// for the durable path, which produces events the ring has never seen.
func (s *subscription) replayEvent(h *hub, ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sendLocked(h, ev, true)
}

// replayError reports a replay failure that belongs to this subscriber only: a
// cursor the server cannot honour is not an error for every other client.
func (s *subscription) replayError(h *hub, code, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sendLocked(h, h.errorEvent(code, message), false)
}

// endReplay closes the replay sequence, stops holding and flushes whatever was
// buffered while the replay ran.
//
// The buffered events are sorted by seq before they go out: a durable replay
// assigns its seqs as it emits them, so an event published just before the first
// emitted one can have a lower seq, and flushing it in arrival order would hand
// the client a stream that goes backwards. Events the replay already covered
// (seq <= lastSent) are dropped, which is what makes the duplicate case explicit
// rather than a double render in the client.
//
// A buffer that overflowed means the subscriber could not consume its own
// catch-up: it gets the reason and then the close the flow-control contract
// promises (docs/ws-protocol.md), exactly like a subscriber whose live queue
// overflowed.
func (s *subscription) endReplay(h *hub, count int, complete, truncated bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.sendLocked(h, h.localEvent(EventReplayEnd, s.sessionID, replayEndPayload(count, complete, truncated)), false)
	s.flushLocked(h)
	if s.overflow {
		s.overflow = false
		s.conn.enqueue(mustMarshalEvent(h.errorEvent(codeSlowConsumer, "dropped events while replaying")))
		s.conn.shutdownWith(websocket.StatusPolicyViolation, "slow consumer")
	}
}

// release stops holding without a replay, used when a subscriber asked for live
// events only. Buffered events are flushed so nothing published while the
// subscription was being registered is lost.
func (s *subscription) release(h *hub) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.flushLocked(h)
}

// flushLocked stops holding and writes the buffered events in seq order. Callers
// must hold the lock.
func (s *subscription) flushLocked(h *hub) {
	s.holding = false
	if len(s.pending) == 0 {
		return
	}
	pending := s.pending
	s.pending = nil
	sort.SliceStable(pending, func(i, j int) bool { return pending[i].Seq < pending[j].Seq })
	for _, ev := range pending {
		s.sendLocked(h, ev, true)
	}
}
