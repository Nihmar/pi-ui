package audit

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// testLog builds a Log over a memory store with a manual clock.
func testLog(t *testing.T, mutate func(*Options)) (*Log, *MemoryStore, func(time.Duration)) {
	t.Helper()
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	opts := Options{
		Retention:  time.Hour,
		PruneEvery: time.Hour,
		Now:        func() time.Time { return now },
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	if mutate != nil {
		mutate(&opts)
	}
	store := NewMemoryStore()
	return New(store, opts), store, func(d time.Duration) { now = now.Add(d) }
}

func TestRecordNormalisesAndBounds(t *testing.T) {
	log, store, _ := testLog(t, nil)
	longTarget := strings.Repeat("é", maxTargetLen+10)
	log.Record(Event{
		Action:  Action(strings.Repeat("a", maxActionLen+10)),
		Outcome: OutcomeOK,
		Target:  longTarget,
	})

	events, _, err := log.Query(Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("entries = %d, want 1", len(events))
	}
	if got := len(string(events[0].Action)); got != maxActionLen {
		t.Fatalf("action length = %d, want %d", got, maxActionLen)
	}
	if got := len([]rune(events[0].Target)); got != maxTargetLen {
		t.Fatalf("target = %d runes, want %d", got, maxTargetLen)
	}
	if events[0].At.IsZero() {
		t.Fatal("the timestamp was not filled from the clock")
	}
	if len(store.events) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(store.events))
	}
}

func TestRecordDropsEventsThatCannotBeRead(t *testing.T) {
	log, store, _ := testLog(t, nil)
	log.Record(Event{Outcome: OutcomeOK})                                     // no action
	log.Record(Event{Action: ActionSessionPrompt, Outcome: Outcome("maybe")}) // bad outcome
	if len(store.events) != 0 {
		t.Fatalf("stored rows = %d, want 0", len(store.events))
	}
}

func TestRecordTruncatesOversizedDetails(t *testing.T) {
	log, _, _ := testLog(t, nil)
	log.Record(Event{
		Action:  ActionSettingsUpdate,
		Outcome: OutcomeOK,
		Details: map[string]any{"blob": strings.Repeat("x", maxDetailsBytes+1)},
	})
	events, _, err := log.Query(Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("entries = %d, want 1", len(events))
	}
	if events[0].Details["truncated"] != true {
		t.Fatalf("details = %v, want the truncation marker", events[0].Details)
	}
}

func TestRecordSwallowsStoreFailures(t *testing.T) {
	log := New(failingStore{}, Options{
		Now:    func() time.Time { return time.Unix(0, 0) },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	// The point: no panic and no error path for the caller.
	log.Record(Event{Action: ActionAuthPair, Outcome: OutcomeOK})
}

func TestRecordPrunesByRetention(t *testing.T) {
	log, store, advance := testLog(t, nil)
	log.Record(Event{Action: ActionAuthPair, Outcome: OutcomeOK})

	// Past the retention window a new write triggers the prune of the old entry.
	advance(2 * time.Hour)
	log.Record(Event{Action: ActionAuthDenied, Outcome: OutcomeDenied})

	events, _, err := log.Query(Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 1 || events[0].Action != ActionAuthDenied {
		t.Fatalf("events = %+v, want only the fresh one", events)
	}
	if len(store.events) != 1 {
		t.Fatalf("stored rows = %d, want 1", len(store.events))
	}
}

func TestQueryFiltersAndPaginates(t *testing.T) {
	log, _, advance := testLog(t, nil)
	log.Record(Event{Action: ActionAuthPair, Outcome: OutcomeOK, ActorDeviceID: "d_a", SessionID: ""})
	advance(time.Minute)
	log.Record(Event{Action: ActionSessionPrompt, Outcome: OutcomeOK, ActorDeviceID: "d_a", SessionID: "s_1"})
	advance(time.Minute)
	log.Record(Event{Action: ActionSessionPrompt, Outcome: OutcomeError, ActorDeviceID: "d_b", SessionID: "s_1"})

	events, _, err := log.Query(Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(events) != 3 || events[0].Action != ActionSessionPrompt || events[0].ActorDeviceID != "d_b" {
		t.Fatalf("events = %+v, want newest first", events)
	}

	byAction, _, err := log.Query(Filter{Action: ActionSessionPrompt})
	if err != nil {
		t.Fatalf("Query(action): %v", err)
	}
	if len(byAction) != 2 {
		t.Fatalf("by action = %d, want 2", len(byAction))
	}
	byDevice, _, err := log.Query(Filter{DeviceID: "d_a"})
	if err != nil {
		t.Fatalf("Query(device): %v", err)
	}
	if len(byDevice) != 2 {
		t.Fatalf("by device = %d, want 2", len(byDevice))
	}
	bySession, _, err := log.Query(Filter{SessionID: "s_1"})
	if err != nil {
		t.Fatalf("Query(session): %v", err)
	}
	if len(bySession) != 2 {
		t.Fatalf("by session = %d, want 2", len(bySession))
	}

	// Since is exclusive: the second entry and later.
	since := events[2].At
	newer, _, err := log.Query(Filter{Since: since})
	if err != nil {
		t.Fatalf("Query(since): %v", err)
	}
	if len(newer) != 2 {
		t.Fatalf("since = %d, want 2", len(newer))
	}

	// A page of two reports that more matched.
	page, truncated, err := log.Query(Filter{Limit: 2})
	if err != nil {
		t.Fatalf("Query(limit): %v", err)
	}
	if len(page) != 2 || !truncated {
		t.Fatalf("page = %d truncated = %v, want 2 and true", len(page), truncated)
	}
	exact, truncated, err := log.Query(Filter{Limit: 3})
	if err != nil {
		t.Fatalf("Query(limit 3): %v", err)
	}
	if len(exact) != 3 || truncated {
		t.Fatalf("page = %d truncated = %v, want 3 and false", len(exact), truncated)
	}

	// The maximum is enforced, not honoured blindly.
	_, _, err = log.Query(Filter{Limit: MaxLimit + 100})
	if err != nil {
		t.Fatalf("Query(huge limit): %v", err)
	}
}

func TestMemoryStoreCopiesDetails(t *testing.T) {
	store := NewMemoryStore()
	details := map[string]any{"key": "value"}
	if _, err := store.Append(Event{Action: ActionAuthPair, Outcome: OutcomeOK, Details: details}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	details["key"] = "mutated"

	events, err := store.Query(Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if events[0].Details["key"] != "value" {
		t.Fatalf("stored details = %v, want the original", events[0].Details)
	}
	events[0].Details["key"] = "again"
	again, _ := store.Query(Filter{})
	if again[0].Details["key"] != "value" {
		t.Fatalf("the store aliases the returned map: %v", again[0].Details)
	}
}

// failingStore is a Store whose every method fails.
type failingStore struct{}

func (failingStore) Append(Event) (int64, error) { return 0, errors.New("audit: store down") }
func (failingStore) Query(Filter) ([]Event, error) {
	return nil, errors.New("audit: store down")
}
func (failingStore) Prune(time.Time) (int, error) { return 0, errors.New("audit: store down") }
