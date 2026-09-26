package rpc

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// TestSendIgnoresALateResponseForAClosedExpectation pins the correlation rule for the one
// window the pending-id check cannot cover by itself: a response that reaches the bridge
// while its sender is already on its way out — the sender timed out, or is being released —
// still matches `pending`, so it lands in the one-slot result channel after nobody is
// waiting for it. The next Send must not take that answer for its own.
//
// The sequence is driven directly (expect → answer → clear) because the window between a
// timeout firing and the deferred clearExpectation is microseconds wide and cannot be hit
// reliably from outside; the observable consequence is asserted through Send.
func TestSendIgnoresALateResponseForAClosedExpectation(t *testing.T) {
	b, ok := startBridge(t, Options{RecordBuffer: 8, SendTimeout: 5 * time.Second}, fakePi(t, fakeharness.Script{}), nil).(*bridge)
	if !ok {
		t.Fatal("New did not return the bridge implementation")
	}

	// The timed-out sender's expectation, then the child's answer arriving in that window.
	if err := b.expectResponse("first"); err != nil {
		t.Fatalf("expectResponse(first): %v", err)
	}
	b.answer(Record{
		Type: typeResponse,
		ID:   "first",
		Raw:  json.RawMessage(`{"type":"response","id":"first","command":"get_state","success":true,"late":true}`),
	})
	b.clearExpectation("first")

	raw, err := b.Send(context.Background(), "second", json.RawMessage(`{"type":"get_entries"}`))
	if err != nil {
		t.Fatalf("Send(second): %v", err)
	}
	if id := fieldString(t, raw, "id"); id != "second" {
		t.Fatalf("Send(second) received the late response of %q: %s", id, raw)
	}
	if command := fieldString(t, raw, "command"); command != "get_entries" {
		t.Fatalf("Send(second) received the response of command %q: %s", command, raw)
	}
}
