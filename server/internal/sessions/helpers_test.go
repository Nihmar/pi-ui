package sessions

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// waitTimeout bounds every "the child did something" assertion: the child is a separate
// process, so a test polls instead of sleeping a fixed amount and hoping.
const waitTimeout = 10 * time.Second

// recorder is the injected hub of every test: it keeps the published events and the routed
// dialog frames so a test can assert the fan-out without a websocket.
type recorder struct {
	mu     sync.Mutex
	events []ws.Event
	frames []dialogFrame
	seq    uint64
}

// dialogFrame is one `request` frame the supervisor handed to the hub.
type dialogFrame struct {
	sessionID string
	frame     json.RawMessage
}

var _ ws.Requester = (*recorder)(nil)

func newRecorder() *recorder { return &recorder{} }

// Publish implements sessions.Publisher and fills seq/ts like the real hub does.
func (r *recorder) Publish(ev ws.Event) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq++
	ev.Seq = r.seq
	if ev.TS == "" {
		ev.TS = now()
	}
	r.events = append(r.events, ev)
	return ev.Seq
}

// SendRequest implements ws.Requester, the dialog path of §8.
func (r *recorder) SendRequest(sessionID string, frame json.RawMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.frames = append(r.frames, dialogFrame{sessionID: sessionID, frame: append(json.RawMessage(nil), frame...)})
	return nil
}

// snapshot returns copies of everything recorded so far.
func (r *recorder) snapshot() ([]ws.Event, []dialogFrame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ws.Event(nil), r.events...), append([]dialogFrame(nil), r.frames...)
}

// ofType returns the events published under one type, in order.
func (r *recorder) ofType(eventType string) []ws.Event {
	events, _ := r.snapshot()
	var matching []ws.Event
	for _, ev := range events {
		if ev.Type == eventType {
			matching = append(matching, ev)
		}
	}
	return matching
}

// last returns the most recent event of one type.
func (r *recorder) last(eventType string) (ws.Event, bool) {
	matching := r.ofType(eventType)
	if len(matching) == 0 {
		return ws.Event{}, false
	}
	return matching[len(matching)-1], true
}

// types returns the published event types in order, for assertions on the sequence.
func (r *recorder) types() []string {
	events, _ := r.snapshot()
	types := make([]string, 0, len(events))
	for _, ev := range events {
		types = append(types, ev.Type)
	}
	return types
}

// frames returns the routed dialog frames.
func (r *recorder) dialogFrames() []dialogFrame {
	_, frames := r.snapshot()
	return frames
}

// waitFor polls cond until it holds or the deadline expires.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", waitTimeout, what)
}

// waitForEvent waits for one event type to appear and returns it.
func waitForEvent(t *testing.T, rec *recorder, eventType string) ws.Event {
	t.Helper()
	waitFor(t, "event "+eventType, func() bool {
		_, ok := rec.last(eventType)
		return ok
	})
	ev, _ := rec.last(eventType)
	return ev
}

// newTestManager returns a supervisor wired to a recording publisher. mutate adjusts the
// Config (a short DialogTimeout, MaxSessions, BridgeExt, ...) before New applies defaults.
func newTestManager(t *testing.T, mutate func(*Config)) (*Manager, *recorder) {
	t.Helper()
	rec := newRecorder()
	cfg := Config{
		PiCommand:   []string{"fake-pi"},
		Hub:         rec,
		SendTimeout: 5 * time.Second,
		// Child diagnostics stay out of the test output: the assertions are on events,
		// and t.Log from a pump goroutine would race the end of the test.
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&cfg)
	}
	mgr := New(cfg)
	t.Cleanup(func() {
		// A leaked child would fail the race run and the orphan check later, so every
		// test reaps through the same path the server uses.
		if err := mgr.Shutdown(context.Background()); err != nil {
			t.Errorf("cleanup shutdown: %v", err)
		}
	})
	return mgr, rec
}

// startSession spawns one session and returns its projection. The session outlives the
// test only until the cleanup shutdown.
func startSession(t *testing.T, mgr *Manager, spec Spec) Info {
	t.Helper()
	info, err := mgr.Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	return info
}

// decodePayload unmarshals an event payload into a map, so a test can assert on individual
// fields without pinning the whole shape.
func decodePayload(t *testing.T, ev ws.Event) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(ev.Payload, &fields); err != nil {
		t.Fatalf("payload of %s is not a JSON object: %v (%s)", ev.Type, err, ev.Payload)
	}
	return fields
}

// payloadString returns one string field of an event payload.
func payloadString(t *testing.T, ev ws.Event, key string) string {
	t.Helper()
	raw, ok := decodePayload(t, ev)[key]
	if !ok {
		t.Fatalf("payload of %s has no %q: %s", ev.Type, key, ev.Payload)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("payload %q of %s is not a string: %v", key, ev.Type, err)
	}
	return value
}

// payloadInt returns one numeric field of an event payload.
func payloadInt(t *testing.T, ev ws.Event, key string) int {
	t.Helper()
	raw, ok := decodePayload(t, ev)[key]
	if !ok {
		t.Fatalf("payload of %s has no %q: %s", ev.Type, key, ev.Payload)
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("payload %q of %s is not an integer: %v", key, ev.Type, err)
	}
	return value
}

// statusValues returns the status field of every server.status event recorded so far.
func statusValues(t *testing.T, rec *recorder) []string {
	t.Helper()
	statuses := make([]string, 0, 4)
	for _, ev := range rec.ofType(EventServerStatus) {
		statuses = append(statuses, payloadString(t, ev, "status"))
	}
	return statuses
}
