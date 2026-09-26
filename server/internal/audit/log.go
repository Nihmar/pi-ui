package audit

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"
)

// Defaults of the trail.
const (
	// DefaultRetention is how long an entry is kept before a prune removes it.
	DefaultRetention = 30 * 24 * time.Hour
	// DefaultPruneEvery is the coarsest rate at which a write also prunes: one scan an
	// hour, not one per request.
	DefaultPruneEvery = time.Hour
	// DefaultLimit is the page size of a query without one.
	DefaultLimit = 100
	// MaxLimit is the largest page a caller may ask for.
	MaxLimit = 1000
	// maxActionLen bounds a stored action name.
	maxActionLen = 64
	// maxTargetLen bounds a stored target in runes.
	maxTargetLen = 256
	// maxDetailsBytes bounds the encoded details of one entry.
	maxDetailsBytes = 4 << 10
)

// Options tunes a Log.
type Options struct {
	// Retention is how long entries are kept (default 30 days).
	Retention time.Duration
	// PruneEvery is the coarsest prune interval (default one hour).
	PruneEvery time.Duration
	// Now is the clock (default time.Now), injectable for tests.
	Now func() time.Time
	// Logger receives store failures (default slog.Default()).
	Logger *slog.Logger
}

// Log is the Recorder and reader over a Store: it normalises what it accepts, bounds
// it, prunes on a coarse schedule and never lets a store failure reach a request.
type Log struct {
	store Store
	opts  Options

	mu        sync.Mutex
	lastPrune time.Time
}

var _ Recorder = (*Log)(nil)

// New builds a Log over store.
func New(store Store, opts Options) *Log {
	if store == nil {
		panic("audit: New called without a Store")
	}
	if opts.Retention <= 0 {
		opts.Retention = DefaultRetention
	}
	if opts.PruneEvery <= 0 {
		opts.PruneEvery = DefaultPruneEvery
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &Log{store: store, opts: opts}
}

// Record stores one event. It is safe for concurrent use and never returns an error:
// a trail that cannot be written is logged, because refusing the action it describes
// would be worse than a gap in the trail (the gap is itself logged).
func (l *Log) Record(ev Event) {
	now := l.opts.Now()
	if ev.At.IsZero() {
		ev.At = now
	}
	if ev.Action == "" {
		l.opts.Logger.Error("audit: dropping an event without an action")
		return
	}
	if !ev.Outcome.Valid() {
		l.opts.Logger.Error("audit: dropping an event with an invalid outcome", "action", ev.Action, "outcome", ev.Outcome)
		return
	}
	ev.Action = Action(clip(string(ev.Action), maxActionLen))
	ev.Target = clipRunes(ev.Target, maxTargetLen)
	if ev.Details != nil {
		if data, err := json.Marshal(ev.Details); err != nil {
			ev.Details = map[string]any{"detailEncoding": "failed"}
		} else if len(data) > maxDetailsBytes {
			l.opts.Logger.Warn("audit: details truncated", "action", ev.Action, "bytes", len(data))
			ev.Details = map[string]any{"truncated": true, "bytes": len(data)}
		}
	}

	if _, err := l.store.Append(ev); err != nil {
		l.opts.Logger.Error("audit: append failed", "action", ev.Action, "error", err)
		return
	}
	l.maybePrune(now)
}

// Query returns one page of the trail, newest first, and whether more rows matched
// than the page returned.
func (l *Log) Query(filter Filter) ([]Event, bool, error) {
	if filter.Limit <= 0 {
		filter.Limit = DefaultLimit
	}
	if filter.Limit > MaxLimit {
		filter.Limit = MaxLimit
	}
	// One extra row is what distinguishes "exactly a page" from "there is more".
	probe := filter
	probe.Limit = filter.Limit + 1
	events, err := l.store.Query(probe)
	if err != nil {
		return nil, false, err
	}
	if len(events) > filter.Limit {
		return events[:filter.Limit], true, nil
	}
	return events, false, nil
}

// Prune deletes the entries older than the retention window and reports how many went.
func (l *Log) Prune(now time.Time) (int, error) {
	cutoff := now.Add(-l.opts.Retention)
	return l.store.Prune(cutoff)
}

// maybePrune runs a prune at most once per PruneEvery window.
func (l *Log) maybePrune(now time.Time) {
	l.mu.Lock()
	due := l.lastPrune.IsZero() || now.Sub(l.lastPrune) >= l.opts.PruneEvery
	if due {
		l.lastPrune = now
	}
	l.mu.Unlock()
	if !due {
		return
	}
	if removed, err := l.Prune(now); err != nil {
		l.opts.Logger.Error("audit: prune failed", "error", err)
	} else if removed > 0 {
		l.opts.Logger.Info("audit: pruned expired entries", "removed", removed, "retention", l.opts.Retention.String())
	}
}

// MemoryStore is the in-process Store: tests and a server whose trail dies with it.
// It copies the details map in and out, so a caller cannot mutate a stored row.
type MemoryStore struct {
	mu     sync.Mutex
	nextID int64
	events []Event
}

// NewMemoryStore returns an empty in-process store.
func NewMemoryStore() *MemoryStore { return &MemoryStore{} }

// Append implements Store.
func (m *MemoryStore) Append(ev Event) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	ev.ID = m.nextID
	ev.Details = cloneDetails(ev.Details)
	m.events = append(m.events, ev)
	return ev.ID, nil
}

// Query implements Store.
func (m *MemoryStore) Query(filter Filter) ([]Event, error) {
	m.mu.Lock()
	events := make([]Event, 0, len(m.events))
	for _, ev := range m.events {
		if !matches(ev, filter) {
			continue
		}
		ev.Details = cloneDetails(ev.Details)
		events = append(events, ev)
	}
	m.mu.Unlock()

	sort.SliceStable(events, func(i, j int) bool { return events[i].ID > events[j].ID })
	if filter.Limit > 0 && len(events) > filter.Limit {
		events = events[:filter.Limit]
	}
	return events, nil
}

// Prune implements Store.
func (m *MemoryStore) Prune(before time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.events[:0]
	removed := 0
	for _, ev := range m.events {
		if ev.At.Before(before) {
			removed++
			continue
		}
		kept = append(kept, ev)
	}
	m.events = kept
	return removed, nil
}

// matches applies the filter to one event.
func matches(ev Event, filter Filter) bool {
	if !filter.Since.IsZero() && !ev.At.After(filter.Since) {
		return false
	}
	if filter.Action != "" && ev.Action != filter.Action {
		return false
	}
	if filter.DeviceID != "" && ev.ActorDeviceID != filter.DeviceID {
		return false
	}
	if filter.SessionID != "" && ev.SessionID != filter.SessionID {
		return false
	}
	return true
}

// clip shortens a plain string to at most n bytes.
func clip(value string, n int) string {
	if len(value) <= n {
		return value
	}
	return value[:n]
}

// clipRunes shortens a string to at most n runes, so a multi-byte value is never cut
// mid-character.
func clipRunes(value string, n int) string {
	runes := []rune(value)
	if len(runes) <= n {
		return value
	}
	return string(runes[:n])
}

// cloneDetails copies a details map one level deep, enough for the scalar values the
// server records.
func cloneDetails(details map[string]any) map[string]any {
	if details == nil {
		return nil
	}
	out := make(map[string]any, len(details))
	for key, value := range details {
		out[key] = value
	}
	return out
}

// String renders an event for diagnostics and tests.
func (ev Event) String() string {
	return fmt.Sprintf("%s %s %s", ev.At.UTC().Format(time.RFC3339), ev.Action, ev.Outcome)
}
