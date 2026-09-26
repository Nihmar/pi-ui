package ws

import (
	"encoding/json"
	"testing"
	"time"
)

// ringEvent builds a minimal event for the ring tests.
func ringEvent(session string, seq uint64) Event {
	return Event{
		Type:      "pi.message_update",
		SessionID: session,
		Seq:       seq,
		TS:        "2025-01-02T03:04:05.678Z",
		Payload:   json.RawMessage(`{}`),
	}
}

// TestRingEvictsOldestWhenFull pins the size bound: the ring holds exactly the last
// N events, and a cursor older than the oldest one is reported as truncated.
func TestRingEvictsOldestWhenFull(t *testing.T) {
	r := newRing(2, time.Minute)
	now := time.Now()
	for seq := uint64(1); seq <= 3; seq++ {
		r.add(ringEvent("s_a", seq), now)
	}

	events, truncated := r.snapshot("s_a", 0, true, now)
	if len(events) != 2 {
		t.Fatalf("snapshot returned %d events, want 2", len(events))
	}
	if events[0].Seq != 2 || events[1].Seq != 3 {
		t.Fatalf("snapshot seqs = %d, %d, want 2, 3", events[0].Seq, events[1].Seq)
	}
	if !truncated {
		t.Fatal("truncated = false, want true after an eviction")
	}

	// A cursor past the eviction point is served without a truncation flag.
	events, truncated = r.snapshot("s_a", 2, true, now)
	if len(events) != 1 || events[0].Seq != 3 {
		t.Fatalf("snapshot from seq 2 = %+v, want only seq 3", events)
	}
	if truncated {
		t.Fatal("truncated = true for a cursor at the oldest retained event")
	}
}

// TestRingPrunesByAge: the window is an age bound, not only a size bound.
func TestRingPrunesByAge(t *testing.T) {
	r := newRing(10, 50*time.Millisecond)
	start := time.Now()
	r.add(ringEvent("s_a", 1), start)
	r.add(ringEvent("s_a", 2), start.Add(30*time.Millisecond))

	events, _ := r.snapshot("s_a", 0, true, start.Add(40*time.Millisecond))
	if len(events) != 2 {
		t.Fatalf("snapshot inside the window returned %d events, want 2", len(events))
	}

	events, truncated := r.snapshot("s_a", 0, true, start.Add(100*time.Millisecond))
	if len(events) != 0 {
		t.Fatalf("snapshot past the window returned %d events, want 0", len(events))
	}
	if !truncated {
		t.Fatal("truncated = false after both events aged out of the window")
	}
	// A cursor that never claimed to have anything is still not truncated.
	if _, truncated := r.snapshot("s_a", 0, false, start.Add(100*time.Millisecond)); truncated {
		t.Fatal("truncated = true for a subscriber without a cursor")
	}
}

// TestRingSnapshotIsPerSession: one ring holds every session, and a snapshot only
// returns the events of the session it was asked about.
func TestRingSnapshotIsPerSession(t *testing.T) {
	r := newRing(8, time.Minute)
	now := time.Now()
	r.add(ringEvent("s_a", 1), now)
	r.add(ringEvent("s_b", 2), now)
	r.add(ringEvent("s_a", 3), now)

	events, truncated := r.snapshot("s_a", 1, true, now)
	if len(events) != 1 || events[0].Seq != 3 {
		t.Fatalf("snapshot = %+v, want only seq 3 of s_a", events)
	}
	if truncated {
		t.Fatal("truncated = true for a cursor whose next event is retained")
	}

	events, _ = r.snapshot("s_a", 0, false, now)
	if len(events) != 2 || events[0].Seq != 1 || events[1].Seq != 3 {
		t.Fatalf("snapshot without a cursor = %+v, want both retained events of s_a", events)
	}

	if events, _ := r.snapshot("s_missing", 0, false, now); len(events) != 0 {
		t.Fatalf("snapshot of an unknown session = %+v, want nothing", events)
	}
}

// TestRingWithoutCursorIsNotTruncated: a subscriber that never claimed to have
// anything cannot have fallen behind.
func TestRingWithoutCursorIsNotTruncated(t *testing.T) {
	r := newRing(1, time.Minute)
	now := time.Now()
	r.add(ringEvent("s_a", 1), now)
	r.add(ringEvent("s_a", 2), now)

	if _, truncated := r.snapshot("s_a", 0, false, now); truncated {
		t.Fatal("truncated = true for a subscriber without a cursor")
	}
	if _, truncated := r.snapshot("s_a", 0, true, now); !truncated {
		t.Fatal("truncated = false for a cursor older than the evicted event")
	}
}

// TestRingClampsASizeOfZero keeps a misconfigured size from panicking the hub: New
// already defaults it, and the ring defends itself anyway.
func TestRingClampsASizeOfZero(t *testing.T) {
	r := newRing(0, time.Minute)
	now := time.Now()
	r.add(ringEvent("s_a", 1), now)
	r.add(ringEvent("s_a", 2), now)

	events, _ := r.snapshot("s_a", 0, false, now)
	if len(events) != 1 || events[0].Seq != 2 {
		t.Fatalf("snapshot = %+v, want only the newest event", events)
	}
	if got := r.latest(); got != 2 {
		t.Fatalf("latest = %d, want 2", got)
	}
}

// TestRingLatestIsEmptyOnAFreshRing documents the diagnostics helper.
func TestRingLatestIsEmptyOnAFreshRing(t *testing.T) {
	if got := newRing(4, time.Minute).latest(); got != 0 {
		t.Fatalf("latest = %d, want 0", got)
	}
}

// TestRingDoesNotReportTruncationWithoutEviction pins the exactness of the flag: a
// session whose first event happens to carry a high global seq (other sessions
// published before it) has lost nothing, and a client must not be sent to REST for it.
func TestRingDoesNotReportTruncationWithoutEviction(t *testing.T) {
	r := newRing(4, time.Minute)
	now := time.Now()
	r.add(ringEvent("s_a", 7), now)

	events, truncated := r.snapshot("s_a", 0, true, now)
	if len(events) != 1 || events[0].Seq != 7 {
		t.Fatalf("snapshot = %+v, want the single retained event", events)
	}
	if truncated {
		t.Fatal("truncated = true although nothing was evicted")
	}
}
