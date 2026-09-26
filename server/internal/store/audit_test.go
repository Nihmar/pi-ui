package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
)

func TestAuditTrailPersistsAndPrunes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.db")
	ctx := context.Background()
	db, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	store := db.Audit()
	base := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

	entries := []audit.Event{
		{At: base.Add(-2 * time.Hour), Action: audit.ActionAuthPair, Outcome: audit.OutcomeOK,
			ActorDeviceID: "d_a", ActorName: "phone", ActorScope: "operator", RemoteAddr: "10.0.0.1",
			Details: map[string]any{"platform": "android"}},
		{At: base.Add(-time.Hour), Action: audit.ActionSessionPrompt, Outcome: audit.OutcomeOK,
			ActorDeviceID: "d_a", SessionID: "s_1", Target: "s_1",
			Details: map[string]any{"op": "session.prompt"}},
		{At: base, Action: audit.ActionAuthDenied, Outcome: audit.OutcomeDenied,
			RemoteAddr: "198.51.100.7", Details: map[string]any{"reason": "invalid token"}},
	}
	for index, entry := range entries {
		id, err := store.Append(entry)
		if err != nil {
			t.Fatalf("Append %d: %v", index, err)
		}
		if id == 0 {
			t.Fatalf("Append %d returned no row id", index)
		}
	}

	all, err := store.Query(audit.Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(all) != 3 || all[0].Action != audit.ActionAuthDenied {
		t.Fatalf("all = %+v, want three rows newest first", all)
	}
	if all[2].Details["platform"] != "android" {
		t.Fatalf("details = %v, want the stored context", all[2].Details)
	}

	byAction, err := store.Query(audit.Filter{Action: audit.ActionAuthPair})
	if err != nil || len(byAction) != 1 {
		t.Fatalf("by action = %+v (%v), want one", byAction, err)
	}
	byDevice, err := store.Query(audit.Filter{DeviceID: "d_a"})
	if err != nil || len(byDevice) != 2 {
		t.Fatalf("by device = %d (%v), want two", len(byDevice), err)
	}
	bySession, err := store.Query(audit.Filter{SessionID: "s_1"})
	if err != nil || len(bySession) != 1 {
		t.Fatalf("by session = %d (%v), want one", len(bySession), err)
	}
	since, err := store.Query(audit.Filter{Since: base.Add(-90 * time.Minute), Limit: 10})
	if err != nil || len(since) != 2 {
		t.Fatalf("since = %d (%v), want two", len(since), err)
	}

	removed, err := store.Prune(base.Add(-90 * time.Minute))
	if err != nil {
		t.Fatalf("Prune: %v", err)
	}
	if removed != 1 {
		t.Fatalf("pruned = %d, want 1", removed)
	}

	// The trail survives a restart, details included.
	if err := db.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	after, err := reopened.Audit().Query(audit.Filter{})
	if err != nil {
		t.Fatalf("Query after restart: %v", err)
	}
	if len(after) != 2 {
		t.Fatalf("after restart = %d rows, want 2", len(after))
	}
	// Newest first, both rows intact: the denied verdict and the command context.
	if after[0].Action != audit.ActionAuthDenied || after[0].Details["reason"] != "invalid token" {
		t.Fatalf("after restart = %+v, want the denied entry and its reason", after[0])
	}
	if after[1].Action != audit.ActionSessionPrompt || after[1].Details["op"] != "session.prompt" ||
		after[1].SessionID != "s_1" || after[1].ActorDeviceID != "d_a" {
		t.Fatalf("after restart = %+v, want the command entry and its context", after[1])
	}
}
