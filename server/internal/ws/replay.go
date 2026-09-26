package ws

import (
	"errors"
	"time"
)

// startReplay serves the replay of one subscription.
//
// It runs in its own goroutine for two reasons: the durable path talks to the
// child process, which must not stall the connection's read loop, and the ring
// path copies up to ReplayEvents events, which must not stall the publisher. The
// subscription starts holding (see newSubscription), so live events published
// while the replay is being prepared are buffered instead of racing the replayed
// ones.
func (h *hub) startReplay(sub *subscription, cursor replayCursor) {
	h.connWG.Add(1)
	go func() {
		defer h.connWG.Done()

		if cursor.hasEntry {
			h.replayFromEntry(sub, cursor.entryID)
			return
		}
		h.replayFromRing(sub, cursor.seq, cursor.hasSeq)
	}()
}

// replayFromRing serves the in-memory replay of a session.
//
// hasSince distinguishes the two subscriptions the protocol allows: a client with
// a cursor gets told when events were lost (truncated), a client asking for "what
// you still have" does not, because it never claimed to have anything.
func (h *hub) replayFromRing(sub *subscription, since uint64, hasSince bool) {
	events, truncated := h.ring.snapshot(sub.sessionID, since, hasSince, time.Now())
	if sub.ctx.Err() != nil {
		return
	}

	sub.beginReplay(h, "seq")
	for _, ev := range events {
		if sub.ctx.Err() != nil {
			// The connection went away or unsubscribed: the subscription is gone
			// too, so there is nothing left to flush or to finish.
			return
		}
		sub.replayEvent(h, ev)
	}
	sub.endReplay(h, len(events), true, truncated)
}

// replayFromEntry serves the durable replay of a session through the injected
// Replayer (sessions performs it with get_entries).
//
// The events the replayer emits are stamped here, not by the replayer: seq comes
// from the same global counter as live events, so the client's cursor stays
// meaningful after a reconnect, and these events deliberately do not enter the
// ring — a replayed entry is history, and writing it back would let every other
// subscriber replay it a second time.
func (h *hub) replayFromEntry(sub *subscription, entryID string) {
	sub.beginReplay(h, "entry")

	replayer := h.replayerOf()
	if replayer == nil {
		sub.replayError(h, codeUnsupported, "durable replay is not configured")
		sub.endReplay(h, 0, false, false)
		return
	}

	ctx := sub.ctx
	count := 0
	complete, err := replayer.ReplayFromEntry(ctx, sub.sessionID, entryID, func(ev Event) {
		if ctx.Err() != nil || ev.Type == "" {
			return
		}
		if ev.SessionID == "" {
			ev.SessionID = sub.sessionID
		}
		h.stamp(&ev)
		sub.replayEvent(h, ev)
		count++
	})

	if err != nil {
		sub.replayError(h, cursorCode(err), err.Error())
		sub.endReplay(h, count, false, false)
		return
	}
	sub.endReplay(h, count, complete, false)
}

// cursorCode maps a durable-replay failure onto a wire code.
//
// A handler that reports its own code keeps it (a session that vanished is
// session_not_found, for example). Anything else becomes replay_cursor_invalid,
// which is the one thing a client can act on here: reload the session through
// REST instead of trusting the cursor.
func cursorCode(err error) string {
	var coded codedError
	if errors.As(err, &coded) {
		if code := coded.ErrorCode(); code != "" {
			if _, ok := knownCodes[code]; ok {
				return code
			}
		}
	}
	return codeReplayCursorInvalid
}
