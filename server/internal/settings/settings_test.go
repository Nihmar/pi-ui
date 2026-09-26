package settings

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/store"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// recordingPublisher collects the change events.
type recordingPublisher struct {
	mu     sync.Mutex
	events []ws.Event
}

func (p *recordingPublisher) Publish(event ws.Event) uint64 {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.events = append(p.events, event)
	return uint64(len(p.events))
}

func (p *recordingPublisher) all() []ws.Event {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]ws.Event(nil), p.events...)
}

// newService opens a real state database in a temporary directory, because the
// settings are only interesting once they survive a write.
func newService(t *testing.T) (*Service, *store.SettingsStore) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(db.Settings(), &recordingPublisher{}), db.Settings()
}

func TestDefaultsAreInForceWithoutAStore(t *testing.T) {
	service := New(nil, nil)

	if service.Bool(context.Background(), KeyGitWrite) {
		t.Fatal("git write is off by default: it is a deliberate decision")
	}
	if got := service.Int(context.Background(), KeyTerminalMax); got != 4 {
		t.Fatalf("terminal limit = %d, want 4", got)
	}
	if _, err := service.Set(KeyGitWrite, json.RawMessage("true"), "d_1"); err == nil {
		t.Fatal("a store-less server cannot remember a change")
	} else if code := sessions.CodeOf(err); code != sessions.CodeUnsupported {
		t.Fatalf("code = %q", code)
	}
}

func TestSetValidatesAndPersists(t *testing.T) {
	service, settings := newService(t)

	value, err := service.Set(KeyGitWrite, json.RawMessage("true"), "d_admin")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != "true" {
		t.Fatalf("stored value = %s", value)
	}
	if !service.Bool(context.Background(), KeyGitWrite) {
		t.Fatal("the change should be in force")
	}
	stored, ok, err := settings.Get(KeyGitWrite)
	if err != nil || !ok || string(stored) != "true" {
		t.Fatalf("the value is not in the database: %s %v %v", stored, ok, err)
	}

	// A second service over the same database sees it: it is a setting, not a cache.
	if !New(settings, nil).Bool(context.Background(), KeyGitWrite) {
		t.Fatal("another reader should see the stored value")
	}
}

func TestBadValuesAreRefused(t *testing.T) {
	service, _ := newService(t)

	cases := []struct {
		key   string
		value string
	}{
		{KeyGitWrite, `"yes"`},
		{KeyTerminalMax, `"four"`},
		{KeyTerminalMax, `0`},
		{KeyTerminalMax, `999`},
		{KeyIdleTimeout, `"soon"`},
		{KeyAuditRetentionDays, `-1`},
		{"nope.key", `true`},
	}
	for _, testCase := range cases {
		_, err := service.Set(testCase.key, json.RawMessage(testCase.value), "d_1")
		if err == nil {
			t.Fatalf("%s=%s should be refused", testCase.key, testCase.value)
		}
		if code := sessions.CodeOf(err); code != sessions.CodeBadRequest {
			t.Fatalf("%s=%s: code %q", testCase.key, testCase.value, code)
		}
	}
}

func TestDurationsAreCanonical(t *testing.T) {
	service, _ := newService(t)

	value, err := service.Set(KeyIdleTimeout, json.RawMessage(`"  2h30m "`), "d_1")
	if err != nil {
		t.Fatal(err)
	}
	if string(value) != `"2h30m"` {
		t.Fatalf("canonical value = %s", value)
	}
	if _, err := service.Set(KeyIdleTimeout, json.RawMessage(`"0"`), "d_1"); err != nil {
		t.Fatalf("zero must be allowed to disable the watchdog: %v", err)
	}
}

func TestChangesAreAnnounced(t *testing.T) {
	publisher := &recordingPublisher{}
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	service := New(db.Settings(), publisher)

	if _, err := service.Set(KeyTerminalMax, json.RawMessage("8"), "d_admin"); err != nil {
		t.Fatal(err)
	}
	events := publisher.all()
	if len(events) != 1 || events[0].Type != EventSettingsChanged {
		t.Fatalf("events = %+v", events)
	}
	payload := map[string]any{}
	if err := json.Unmarshal(events[0].Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["key"] != KeyTerminalMax || payload["actor"] != "d_admin" {
		t.Fatalf("payload = %v", payload)
	}
}

func TestResetFallsBackToTheDefault(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.Set(KeyGitWrite, json.RawMessage("true"), "d_1"); err != nil {
		t.Fatal(err)
	}
	if err := service.Reset(KeyGitWrite, "d_1"); err != nil {
		t.Fatal(err)
	}
	if service.Bool(context.Background(), KeyGitWrite) {
		t.Fatal("the default is off again")
	}
	if err := service.Reset("nope.key", "d_1"); err == nil {
		t.Fatal("an unknown key cannot be reset")
	}
}

func TestValuesCarryDefaultsAndCatalogue(t *testing.T) {
	service, _ := newService(t)
	if _, err := service.Set(KeyWrapUpPrompt, json.RawMessage(`"wrap up"`), "d_1"); err != nil {
		t.Fatal(err)
	}

	snapshot, err := service.Values()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Known) != len(Keys) || len(snapshot.Values) != len(Keys) {
		t.Fatalf("snapshot = %d known, %d values", len(snapshot.Known), len(snapshot.Values))
	}
	if string(snapshot.Values[KeyWrapUpPrompt]) != `"wrap up"` {
		t.Fatalf("stored value = %s", snapshot.Values[KeyWrapUpPrompt])
	}
	if string(snapshot.Defaults[KeyWrapUpPrompt]) != `""` {
		t.Fatalf("default = %s", snapshot.Defaults[KeyWrapUpPrompt])
	}
	if string(snapshot.Values[KeyAuditRetentionDays]) != "30" {
		t.Fatalf("a key without a stored value keeps its default: %s", snapshot.Values[KeyAuditRetentionDays])
	}
	for _, definition := range snapshot.Known {
		if definition.Description == "" || definition.Describe() == "" {
			t.Fatalf("every key documents itself: %+v", definition)
		}
	}
}

func TestUnknownStoredKeysAreIgnoredNotDeleted(t *testing.T) {
	service, settings := newService(t)
	if err := settings.Set("legacy.key", []byte(`"kept"`), time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Values(); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := settings.Get("legacy.key"); err != nil || !ok {
		t.Fatalf("a downgrade must not delete a value: %v %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "unused")); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
