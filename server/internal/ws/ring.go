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

// ring is the bounded, time-windowed history of one session's events that serves
// `since.seq` replay.
//
// One ring per session (the hub keeps a map of them): a shared ring would make every
// session's replay window shrink with the number of sessions publishing, and the memory
// ceiling stays bounded by the session limit either way. Server-wide events have no
// session and are never replayable, so they are not kept at all.
type ring struct {
	mu     sync.Mutex
	size   int
	window time.Duration
	buf    []ringEntry
	head   int // index of the oldest entry
	n      int // number of valid entries
	// evicted is the highest seq this ring has dropped, by size or by age. It is what
	// makes truncation exact: a session whose first event happens to carry a high
	// global seq (other sessions published before it) has lost nothing, and a client
	// must not be told to reload because of it.
	evicted uint64
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
	if r.n == r.size {
		// The slot the new entry goes into is the one holding the oldest entry, so the
		// old one is dropped first: reading it afterwards would read the new event.
		r.dropLocked()
	}
	idx := (r.head + r.n) % r.size
	r.buf[idx] = ringEntry{event: ev, at: at}
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
		r.dropLocked()
	}
}

// dropLocked removes the oldest entry and remembers that its seq is gone. Callers
// must hold the lock.
func (r *ring) dropLocked() {
	dropped := r.buf[r.head].event.Seq
	if dropped > r.evicted {
		r.evicted = dropped
	}
	r.head = (r.head + 1) % r.size
	r.n--
}

// snapshot returns the retained events of the ring (one session), oldest first, and
// reports whether the client's cursor fell out of it.
//
// hasSince is false for a subscriber that asked for replay without a cursor: it gets
// whatever the ring still holds and truncated stays false, because it never claimed to
// have anything. With a cursor, truncated means events of this session with a seq above
// it were evicted or aged out, so the client has to reload through REST instead of
// rendering a stream with a hole in it.
func (r *ring) snapshot(sessionID string, since uint64, hasSince bool, now time.Time) ([]Event, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pruneLocked(now)

	var out []Event
	for i := 0; i < r.n; i++ {
		ev := r.buf[(r.head+i)%r.size].event
		if ev.SessionID != sessionID {
			continue
		}
		if !hasSince || ev.Seq > since {
			out = append(out, ev)
		}
	}
	return out, hasSince && since < r.evicted
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

// reusable reports whether the ring can still serve a replay: it holds at least
// one entry after aging out everything past the replay window. The hub drops the
// history of a session nobody subscribes to once this is false, so a server that
// churns sessions does not keep one ring per session forever.
func (r *ring) reusable(now time.Time) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.pruneLocked(now)
	return r.n > 0
}
