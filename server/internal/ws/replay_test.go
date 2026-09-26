package ws

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/coder/websocket"
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

// TestBeginReplayKeepsEventsBufferedBeforeIt pins the invariant behind the replay
// buffer. A subscription is created holding, so a live event published between its
// registration and the replay's first frame is buffered — and the ring snapshot (or
// the durable replay) the replay is served from was taken before that event. beginReplay
// therefore must not clear the buffer: an event the replay does deliver again is dropped
// by the seq dedup in sendLocked, so clearing it only ever loses events nobody replays.
func TestBeginReplayKeepsEventsBufferedBeforeIt(t *testing.T) {
	const sessionID = "s_cccccccccccccccc"
	ts := newTestHub(t, nil)

	// A connection needs no writer for this test: the frames stay in its queue.
	conn := &connection{hub: ts.hub, ctx: context.Background(), send: make(chan []byte, 8)}
	sub := newSubscription(conn, sessionID)

	live := Event{Type: "pi.message_update", SessionID: sessionID, Payload: json.RawMessage(`{"live":true}`)}
	ts.hub.stamp(&live)
	sub.deliverLive(ts.hub, live)

	// The replay itself delivers nothing: the buffered event is the whole point.
	sub.beginReplay(ts.hub, "seq")
	sub.endReplay(ts.hub, 0, true, false)

	var frames []string
	beginIndex, liveIndex := -1, -1
	for len(conn.send) > 0 {
		var frame struct {
			Type    string          `json:"type"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(<-conn.send, &frame); err != nil {
			t.Fatalf("queued frame is not JSON: %v", err)
		}
		frames = append(frames, frame.Type)
		switch {
		case frame.Type == EventReplayBegin:
			beginIndex = len(frames) - 1
		case frame.Type == "pi.message_update" && string(frame.Payload) == `{"live":true}`:
			liveIndex = len(frames) - 1
		}
	}
	if liveIndex < 0 {
		t.Fatalf("the buffered live event never reached the subscriber (frames: %v)", frames)
	}
	if liveIndex < beginIndex {
		t.Fatalf("the live event arrived before %s (frames: %v)", EventReplayBegin, frames)
	}
}

// TestReplayBufferOverflowClosesTheSubscriber pins the flow-control contract on the replay
// path: a subscriber that cannot even consume its own catch-up is disconnected like any
// other slow consumer — server.error{slow_consumer} and a 1008 close — instead of being
// told about the dropped events and left running.
func TestReplayBufferOverflowClosesTheSubscriber(t *testing.T) {
	const session = "s_dddddddddddddddd"
	ts := newTestHub(t, func(o *Options) { o.SendBuffer = 2 })

	inReplay := make(chan struct{})
	release := make(chan struct{})
	replayer := &stubReplayer{fn: func(context.Context, string, string, func(Event)) (bool, error) {
		close(inReplay)
		<-release
		return true, nil
	}}
	ts.hub.SetReplayer(replayer)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","since":{"entryId":"e-1"}}`)
	client.waitFor(EventReplayBegin)

	// The replay is in flight, so live events are buffered; more of them than SendBuffer
	// makes the subscription overflow.
	<-inReplay
	for i := 0; i < 5; i++ {
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":1}`)})
	}
	close(release)

	frame := client.waitFor(EventError)
	if code := errorCodeOf(t, frame); code != codeSlowConsumer {
		t.Fatalf("server.error code = %q, want %q", code, codeSlowConsumer)
	}
	client.expectClose(websocket.StatusPolicyViolation)
}

// TestEntryReplayKeepsALiveEventPublishedDuringTheReplay pins the durable dedup:
// the replay stamps its events as it emits them, so a live event published while
// the replay runs can carry a lower seq than the last replayed one. The flush must
// not drop it — only the live copies of entries the replay already wrote are
// dropped, and by entry id. With the seq dedup this test loses both e-4 and the
// message update, because the replay of e-3 stamped a higher seq after they were
// published.
func TestEntryReplayKeepsALiveEventPublishedDuringTheReplay(t *testing.T) {
	const session = "s_eeeeeeeeeeeeeee1"
	ts := newTestHub(t, nil)

	replayer := &stubReplayer{fn: func(_ context.Context, _, _ string, emit func(Event)) (bool, error) {
		emit(Event{Type: EventEntryAppended, EntryID: "e-2", Payload: json.RawMessage(`{"entry":{"id":"e-2"}}`)})
		// Published while the replay is in flight: e-2 is the live copy of an entry
		// the replay just emitted (dropped by identity), e-4 and the message update
		// were never replayed (delivered even though their seq is lower than e-3's).
		ts.hub.Publish(Event{Type: EventEntryAppended, SessionID: session, Payload: json.RawMessage(`{"entry":{"id":"e-2"}}`)})
		ts.hub.Publish(Event{Type: EventEntryAppended, SessionID: session, Payload: json.RawMessage(`{"entry":{"id":"e-4"}}`)})
		ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"i":1}`)})
		emit(Event{Type: EventEntryAppended, EntryID: "e-3", Payload: json.RawMessage(`{"entry":{"id":"e-3"}}`)})
		return true, nil
	}}
	ts.hub.SetReplayer(replayer)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","since":{"entryId":"e-1"}}`)

	client.waitFor(EventReplayBegin)
	for _, wantEntry := range []string{"e-2", "e-3"} {
		event := client.waitFor(EventEntryAppended)
		if event["entryId"] != wantEntry {
			t.Fatalf("entryId = %v, want %s", event["entryId"], wantEntry)
		}
	}
	client.waitFor(EventReplayEnd)

	// The two live events the replay did not cover arrive in arrival order.
	first := client.next()
	if first["type"] != EventEntryAppended || first["entryId"] != "e-4" {
		t.Fatalf("first flushed frame = %v, want the live entry e-4", first)
	}
	second := client.next()
	if second["type"] != "pi.message_update" {
		t.Fatalf("second flushed frame = %v, want the live message update", second)
	}
	// Nothing else is buffered: the duplicate e-2 never reaches the client.
	client.sendRaw(`{"type":"ping"}`)
	for {
		frame := client.next()
		if frame["type"] == framePong {
			break
		}
		t.Fatalf("unexpected frame after the flush: %v", frame)
	}
}

// TestEntryReplayForAnUnknownSessionIsEmptyNotAnError pins the subscribe race on
// the durable path: the client may pass a cursor before the REST call that creates
// the session has been processed, so an unknown session answers like an unknown
// ring — begin, an empty end, no error. The client learns about a really missing
// session from REST, which is the only surface that can answer the question.
func TestEntryReplayForAnUnknownSessionIsEmptyNotAnError(t *testing.T) {
	const session = "s_ddddddddddddddd1"
	ts := newTestHub(t, nil)
	ts.hub.SetReplayer(&stubReplayer{fn: func(context.Context, string, string, func(Event)) (bool, error) {
		return false, &codedStubError{code: codeSessionNotFound, message: "no such session"}
	}})

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","since":{"entryId":"e-1"}}`)

	client.waitFor(EventReplayBegin)
	end := client.waitFor(EventReplayEnd)
	if payload := payloadOf(t, end); payload["count"] != float64(0) || payload["complete"] != true {
		t.Fatalf("replay.end = %v, want count 0 and complete true", payload)
	}
	client.sendRaw(`{"type":"ping"}`)
	for {
		frame := client.next()
		if frame["type"] == framePong {
			break
		}
		t.Fatalf("an unknown session produced %v, want nothing but the empty replay", frame)
	}
}
