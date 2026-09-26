package ws

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"
)

// subscribe makes one client subscribe and waits until the hub registered it, so a
// following publish tests fan-out instead of a registration race.
func subscribe(t *testing.T, ts *testServer, client *testClient, raw string, wantSessions int) {
	t.Helper()

	client.sendRaw(raw)
	eventually(t, "the subscription to be registered", func() bool {
		_, sessions := ts.hub.counts()
		return sessions == wantSessions
	})
}

// itoa renders a seq for a hand-built frame, keeping the tests free of fmt.
func itoa(seq uint64) string {
	if seq == 0 {
		return "0"
	}
	var digits []byte
	for seq > 0 {
		digits = append([]byte{byte('0' + seq%10)}, digits...)
		seq /= 10
	}
	return string(digits)
}

// tsPattern is the timestamp shape the schema pins, checked on the wire.
var tsPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// TestFanOutIsScopedToSubscribers checks the basic routing: subscribers of a
// session get its events in publish order, everyone else gets nothing.
func TestFanOutIsScopedToSubscribers(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, nil)

	first := ts.dial(nil)
	defer first.close()
	first.hello()
	subscribe(t, ts, first, `{"type":"subscribe","sessionId":"`+session+`","replay":false}`, 1)

	second := ts.dial(nil)
	defer second.close()
	second.hello()
	subscribe(t, ts, second, `{"type":"subscribe","sessionId":"`+session+`","replay":false}`, 1)

	other := ts.dial(nil)
	defer other.close()
	other.hello()
	subscribe(t, ts, other, `{"type":"subscribe","sessionId":"s_ffffffffffffffff","replay":false}`, 2)

	published := []uint64{
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":1}`)}),
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":2}`)}),
	}

	for _, client := range []*testClient{first, second} {
		for i, want := range published {
			event := client.waitFor("pi.message_update")
			if got := seqOf(t, event); got != want {
				t.Fatalf("event %d seq = %d, want %d", i, got, want)
			}
			if got, _ := event["ts"].(string); !tsPattern.MatchString(got) {
				t.Fatalf("event %d ts = %q, want RFC3339 with milliseconds", i, got)
			}
		}
	}

	other.sendRaw(`{"type":"ping"}`)
	if typ := other.next()["type"]; typ != framePong {
		t.Fatalf("subscriber of another session received %v, want nothing but the pong", typ)
	}
}

// TestUnsubscribeStopsTheStream: unsubscribing is idempotent and takes effect.
func TestUnsubscribeStopsTheStream(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, nil)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	subscribe(t, ts, client, `{"type":"subscribe","sessionId":"`+session+`","replay":false}`, 1)

	client.sendRaw(`{"type":"unsubscribe","sessionId":"` + session + `"}`)
	eventually(t, "the subscription to be dropped", func() bool {
		_, sessions := ts.hub.counts()
		return sessions == 0
	})
	// Unsubscribing twice is not an error: a client racing a reconnect must not
	// have to track server state.
	client.sendRaw(`{"type":"unsubscribe","sessionId":"` + session + `"}`)

	ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":1}`)})
	client.sendRaw(`{"type":"ping"}`)
	if typ := client.next()["type"]; typ != framePong {
		t.Fatalf("after unsubscribe the client received %v, want nothing but the pong", typ)
	}
}

// TestSeqReplayServesTheRing is the common reconnect: the client says which seq it
// already has and gets the rest, framed by begin/end.
func TestSeqReplayServesTheRing(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, nil)

	published := make([]uint64, 0, 3)
	for i := 0; i < 3; i++ {
		published = append(published, ts.hub.Publish(Event{
			Type:      "pi.message_update",
			SessionID: session,
			Payload:   json.RawMessage(`{"i":` + itoa(uint64(i)) + `}`),
		}))
	}

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","since":{"seq":` + itoa(published[0]) + `}}`)

	begin := client.waitFor(EventReplayBegin)
	if begin["sessionId"] != session {
		t.Fatalf("replay.begin sessionId = %v, want %s", begin["sessionId"], session)
	}
	if direction := payloadOf(t, begin)["direction"]; direction != "seq" {
		t.Fatalf("replay direction = %v, want seq", direction)
	}

	for _, want := range published[1:] {
		event := client.waitFor("pi.message_update")
		if got := seqOf(t, event); got != want {
			t.Fatalf("replayed seq = %d, want %d", got, want)
		}
	}

	end := client.waitFor(EventReplayEnd)
	payload := payloadOf(t, end)
	if payload["count"] != float64(2) || payload["complete"] != true {
		t.Fatalf("replay.end = %v, want count 2 and complete true", payload)
	}
	if _, truncated := payload["truncated"]; truncated {
		t.Fatalf("replay.end reported truncation for a cursor inside the ring: %v", payload)
	}
}

// TestSeqReplayTruncatedWhenOutOfWindow: the client's cursor fell out of the ring,
// so the server says so and the client reloads through REST instead of rendering a
// stream with a hole in it.
func TestSeqReplayTruncatedWhenOutOfWindow(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, func(o *Options) { o.ReplayEvents = 2 })

	for i := 0; i < 3; i++ {
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":1}`)})
	}

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","since":{"seq":0}}`)

	client.waitFor(EventReplayBegin)

	kept := 0
	for {
		frame := client.next()
		if frame["type"] == EventReplayEnd {
			payload := payloadOf(t, frame)
			if payload["truncated"] != true {
				t.Fatalf("replay.end = %v, want truncated true (the ring holds only 2 events)", payload)
			}
			if payload["complete"] != true {
				t.Fatalf("replay.end = %v, want complete true", payload)
			}
			break
		}
		kept++
	}
	if kept != 2 {
		t.Fatalf("replayed %d events, want the 2 the ring still holds", kept)
	}
}

// TestSeqReplayWithoutCursorIsNotTruncated: a subscriber asking for "what you still
// have" never claimed to have anything, so nothing is reported as lost.
func TestSeqReplayWithoutCursorIsNotTruncated(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, func(o *Options) { o.ReplayEvents = 2 })

	for i := 0; i < 4; i++ {
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":1}`)})
	}

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `"}`)

	client.waitFor(EventReplayBegin)
	for {
		frame := client.next()
		if frame["type"] != EventReplayEnd {
			continue
		}
		payload := payloadOf(t, frame)
		if _, truncated := payload["truncated"]; truncated {
			t.Fatalf("replay.end = %v, want no truncation flag without a cursor", payload)
		}
		if payload["count"] != float64(2) {
			t.Fatalf("replay.end count = %v, want the 2 retained events", payload["count"])
		}
		return
	}
}

// TestReplayWindowAgesEventsOut: the ring forgets by age as well as by size.
func TestReplayWindowAgesEventsOut(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, func(o *Options) { o.ReplayWindow = 40 * time.Millisecond })

	ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":1}`)})
	time.Sleep(60 * time.Millisecond)
	ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":2}`)})

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","since":{"seq":0}}`)

	client.waitFor(EventReplayBegin)
	for {
		frame := client.next()
		if frame["type"] != EventReplayEnd {
			continue
		}
		payload := payloadOf(t, frame)
		if payload["truncated"] != true {
			t.Fatalf("replay.end = %v, want truncated true after the first event aged out", payload)
		}
		if payload["count"] != float64(1) {
			t.Fatalf("replay.end count = %v, want only the fresh event", payload["count"])
		}
		return
	}
}

// TestEntryReplayDelegatesToTheReplayer covers the durable path: order, stamping
// and the complete flag.
func TestEntryReplayDelegatesToTheReplayer(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, nil)

	replayer := &stubReplayer{fn: func(_ context.Context, sessionID, entryID string, emit func(Event)) (bool, error) {
		if sessionID != session || entryID != "e-1" {
			t.Errorf("replayer called with (%s, %s), want (%s, e-1)", sessionID, entryID, session)
		}
		emit(Event{Type: "pi.entry_appended", Payload: json.RawMessage(`{"entry":{"id":"e-2"}}`)})
		emit(Event{Type: "pi.entry_appended", EntryID: "e-3", Payload: json.RawMessage(`{"entry":{"id":"e-3"}}`)})
		return true, nil
	}}
	ts.hub.SetReplayer(replayer)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","since":{"entryId":"e-1"}}`)

	begin := client.waitFor(EventReplayBegin)
	if direction := payloadOf(t, begin)["direction"]; direction != "entry" {
		t.Fatalf("replay direction = %v, want entry", direction)
	}

	for _, wantEntry := range []string{"e-2", "e-3"} {
		event := client.waitFor("pi.entry_appended")
		if event["entryId"] != wantEntry {
			t.Fatalf("entryId = %v, want %s", event["entryId"], wantEntry)
		}
		if seqOf(t, event) == 0 {
			t.Fatalf("replayed event has no seq: %v", event)
		}
		if got, _ := event["ts"].(string); !tsPattern.MatchString(got) {
			t.Fatalf("replayed ts = %q, want RFC3339 with milliseconds", got)
		}
		if event["sessionId"] != session {
			t.Fatalf("replayed sessionId = %v, want %s", event["sessionId"], session)
		}
	}

	end := client.waitFor(EventReplayEnd)
	if payload := payloadOf(t, end); payload["count"] != float64(2) || payload["complete"] != true {
		t.Fatalf("replay.end = %v, want count 2 and complete true", payload)
	}
	if replayer.calls != 1 {
		t.Fatalf("replayer calls = %d, want 1", replayer.calls)
	}
}

// TestEachSessionHasItsOwnReplayWindow pins the per-session ring: a busy session must
// not shrink the replay window of a quiet one.
func TestEachSessionHasItsOwnReplayWindow(t *testing.T) {
	const busy = "s_aaaaaaaaaaaaaaaa"
	const quiet = "s_bbbbbbbbbbbbbbbb"
	ts := newTestHub(t, func(o *Options) { o.ReplayEvents = 2 })

	for i := 0; i < 3; i++ {
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: busy, Payload: json.RawMessage(`{"i":1}`)})
	}
	quietSeq := ts.hub.Publish(Event{Type: "pi.message_update", SessionID: quiet, Payload: json.RawMessage(`{"i":0}`)})
	for i := 0; i < 3; i++ {
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: busy, Payload: json.RawMessage(`{"i":2}`)})
	}

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + quiet + `","since":{"seq":0}}`)

	client.waitFor(EventReplayBegin)
	event := client.waitFor("pi.message_update")
	if got := seqOf(t, event); got != quietSeq {
		t.Fatalf("replayed seq = %d, want the quiet session's %d", got, quietSeq)
	}

	end := client.waitFor(EventReplayEnd)
	payload := payloadOf(t, end)
	if payload["count"] != float64(1) {
		t.Fatalf("replay.end count = %v, want 1: the busy session must not evict the quiet one's history", payload["count"])
	}
	if _, truncated := payload["truncated"]; truncated {
		t.Fatalf("replay.end = %v, want no truncation: the quiet session lost nothing", payload)
	}
}
