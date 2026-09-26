package ws

import (
	"encoding/json"
	"testing"
)

// TestPublishFillsSeqAndTimestamp pins the publisher contract: seq and ts belong
// to the hub, and a publisher that supplies its own seq cannot break monotonicity.
func TestPublishFillsSeqAndTimestamp(t *testing.T) {
	ts := newTestHub(t, nil)

	first := ts.hub.Publish(Event{Type: "server.status", SessionID: "s_0123456789abcdef"})
	second := ts.hub.Publish(Event{Type: "server.status", SessionID: "s_0123456789abcdef"})
	if first == 0 || second <= first {
		t.Fatalf("seqs = %d, %d, want increasing non-zero values", first, second)
	}

	jumped := ts.hub.Publish(Event{Type: "server.status", SessionID: "s_0123456789abcdef", Seq: second + 10})
	if jumped != second+10 {
		t.Fatalf("publisher seq = %d, want it preserved as %d", jumped, second+10)
	}
	after := ts.hub.Publish(Event{Type: "server.status", SessionID: "s_0123456789abcdef"})
	if after <= jumped {
		t.Fatalf("seq = %d after an explicit %d, want the counter to stay monotonic", after, jumped)
	}

	if seq := ts.hub.Publish(Event{SessionID: "s_0123456789abcdef"}); seq != 0 {
		t.Fatalf("Publish without a type returned %d, want 0", seq)
	}
}

// TestPublishDerivesEntryID: clients get a durable cursor without knowing pi's
// payload shape.
func TestPublishDerivesEntryID(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, nil)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","replay":false}`)
	eventually(t, "the subscription to be registered", func() bool {
		_, sessions := ts.hub.counts()
		return sessions == 1
	})

	ts.hub.Publish(Event{
		Type:      "pi.entry_appended",
		SessionID: session,
		Payload:   json.RawMessage(`{"entry":{"id":"e-42","type":"message"}}`),
	})

	event := client.waitFor("pi.entry_appended")
	if event["entryId"] != "e-42" {
		t.Fatalf("entryId = %v, want e-42 from payload.entry.id", event["entryId"])
	}
}

// TestPublishAfterCloseIsDropped: nothing reaches a socket after Close, and the
// return value says so instead of pretending the event was queued.
func TestPublishAfterCloseIsDropped(t *testing.T) {
	ts := newTestHub(t, nil)
	if err := ts.hub.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if seq := ts.hub.Publish(Event{Type: "server.status", SessionID: "s_0123456789abcdef"}); seq != 0 {
		t.Fatalf("Publish after Close returned %d, want 0", seq)
	}
	// Close is idempotent: the shutdown path may run twice (a signal and the
	// deferred cleanup, for example).
	if err := ts.hub.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestEventWithBrokenPayloadIsReported: a payload that cannot be encoded must not
// take the connection down or disappear silently.
func TestEventWithBrokenPayloadIsReported(t *testing.T) {
	const session = "s_0123456789abcdef"
	ts := newTestHub(t, nil)

	client := ts.dial(nil)
	defer client.close()
	client.hello()
	client.sendRaw(`{"type":"subscribe","sessionId":"` + session + `","replay":false}`)
	eventually(t, "the subscription to be registered", func() bool {
		_, sessions := ts.hub.counts()
		return sessions == 1
	})

	ts.hub.Publish(Event{Type: "pi.message_update", SessionID: session, Payload: json.RawMessage(`{"broken":`)})

	event := client.waitFor(EventError)
	if code := errorCodeOf(t, event); code != codeInternal {
		t.Fatalf("error.code = %q, want %q", code, codeInternal)
	}
	if _, ok := event["seq"].(float64); !ok {
		t.Fatalf("error event has no seq: %v", event)
	}
}
