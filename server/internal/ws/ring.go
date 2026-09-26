package ws

import (
	"sync"
	"time"
)

// ringEntry is one retained event together with the moment the hub accepted it.
// The acceptance time, and not the event's own ts, is what the replay window
// measures: a replayed event may carry an old ts, and aging it out immediately
// would make the window meaningless.
type ringEntry struct {
	event Event
	at    time.Time
}

// ring is the bounded, time-windowed history that serves `since.seq` replay.
//
// One ring holds the events of every session, filtered by session at snapshot
// time: Options carries a single size, and a per-session ring would have made the
// memory ceiling depend on how many sessions happen to exist.
type ring struct {
	mu     sync.Mutex
	size   int
	window time.Duration
	buf    []ringEntry
	head   int // index of the oldest entry
	n      int // number of valid entries
}

// newRing returns a ring of exactly size entries (at least one) that forgets
// entries accepted more than window ago.
func newRing(size int, window time.Duration) *ring {
	if size < 1 {
		size = 1
	}
	return &ring{size: size, window: window, buf: make([]ringEntry, size)}
}

// add appends one event, evicting the oldest entry when the ring is full and
// everything that aged out of the window.
func (r *ring) add(ev Event, at time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pruneLocked(at)
	idx := (r.head + r.n) % r.size
	r.buf[idx] = ringEntry{event: ev, at: at}
	if r.n == r.size {
		r.head = (r.head + 1) % r.size
		return
	}
	r.n++
}

// pruneLocked drops the entries that fell out of the replay window. Callers must
// hold the lock.
func (r *ring) pruneLocked(now time.Time) {
	if r.window <= 0 {
		return
	}
	cutoff := now.Add(-r.window)
	for r.n > 0 && !r.buf[r.head].at.After(cutoff) {
		r.head = (r.head + 1) % r.size
		r.n--
	}
}

// snapshot returns the retained events of one session, oldest first, and reports
// whether the client's cursor fell out of the ring.
//
// hasSince is false for a subscriber that asked for replay without a cursor: it
// gets whatever the ring still holds and truncated stays false, because it never
// claimed to have anything. With a cursor, a gap between the cursor and the
// oldest retained event of the session means events were evicted or aged out, and
// the client has to reload through REST instead of rendering a stream with a hole
// in it.
func (r *ring) snapshot(sessionID string, since uint64, hasSince bool, now time.Time) ([]Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pruneLocked(now)

	var (
		first uint64
		count int
		out   []Event
	)
	for i := 0; i < r.n; i++ {
		ev := r.buf[(r.head+i)%r.size].event
		if ev.SessionID != sessionID {
			continue
		}
		if count == 0 {
			first = ev.Seq
		}
		count++
		if !hasSince || ev.Seq > since {
			out = append(out, ev)
		}
	}
	truncated := hasSince && count > 0 && first > since+1
	return out, truncated
}

// latest returns the highest seq the ring still holds, or 0 when it is empty.
// It is diagnostics only (tests and the heartbeat payload), never a cursor.
func (r *ring) latest() uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.n == 0 {
		return 0
	}
	return r.buf[(r.head+r.n-1)%r.size].event.Seq
}
