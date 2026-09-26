package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
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

// TestSendTimeoutReturnsErrTimeout pins the documented deadline: a command the child never
// answers fails with ErrTimeout, within SendTimeout, and the child stays alive — a timeout
// never touches it (the next command can still be sent).
func TestSendTimeoutReturnsErrTimeout(t *testing.T) {
	b := startBridge(t, Options{RecordBuffer: 8, SendTimeout: 100 * time.Millisecond, KillGrace: 50 * time.Millisecond},
		fakePi(t, fakeharness.Script{}, "--stall-ms", "30000"), nil)

	started := time.Now()
	if _, err := b.Send(context.Background(), "c1", json.RawMessage(`{"type":"get_state"}`)); !errors.Is(err, ErrTimeout) {
		t.Fatalf("Send = %v, want ErrTimeout", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("Send took %s, want it bounded by SendTimeout", elapsed)
	}
	if pid := b.PID(); pid == 0 || !processExists(pid) {
		t.Fatalf("child %d is gone after a command timeout, want it untouched", pid)
	}
}

// TestResponseForAnUnknownIDIsDropped pins the other direction of the correlation rule: a
// response whose id matches no pending command is dropped, so it can never be handed to a
// later Send as its own answer.
func TestResponseForAnUnknownIDIsDropped(t *testing.T) {
	script := fakeharness.Script{Startup: []fakeharness.Step{
		{Record: json.RawMessage(`{"type":"response","id":"ghost","command":"get_state","success":true}`)},
	}}
	b := startBridge(t, Options{RecordBuffer: 8, SendTimeout: 2 * time.Second}, fakePi(t, script), nil)

	raw, err := b.Send(context.Background(), "c1", json.RawMessage(`{"type":"get_state"}`))
	if err != nil {
		t.Fatalf("Send(c1): %v", err)
	}
	if id := fieldString(t, raw, "id"); id != "c1" {
		t.Fatalf("Send received the response of %q: %s", id, raw)
	}
}

// TestSendRejectsASecondCommandInFlight pins the one-command-at-a-time rule: a second
// command is rejected with the pending id named instead of being correlated with the first
// command's response. The expectation is set directly because the window between two
// Sends is a scheduling race no test can hit reliably.
func TestSendRejectsASecondCommandInFlight(t *testing.T) {
	b, ok := startBridge(t, Options{RecordBuffer: 8, SendTimeout: time.Second}, fakePi(t, fakeharness.Script{}), nil).(*bridge)
	if !ok {
		t.Fatal("New did not return the bridge implementation")
	}
	if err := b.expectResponse("first"); err != nil {
		t.Fatalf("expectResponse(first): %v", err)
	}
	defer b.clearExpectation("first")

	_, err := b.Send(context.Background(), "second", json.RawMessage(`{"type":"get_state"}`))
	if err == nil || !strings.Contains(err.Error(), "already in flight") {
		t.Fatalf("Send while a command is in flight = %v, want the pending-command rejection", err)
	}
}
