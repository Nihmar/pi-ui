// Package settings is the server's own policy: the small set of keys an admin can
// change while the server runs, with the defaults and the validation for each one.
//
// It is not a free-form config store. A key that is not in [Keys] is refused, because
// a stored typo would otherwise look like a setting that silently does nothing, and a
// value is validated before it is written so a reader never has to defend itself
// against its own database. Changing one publishes `server.settings.changed`, so a
// client that shows the settings notices a change made by another device.
package settings

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// The keys this server knows. Every effect a key has is documented here and in
// docs/api-v1.md; a key with no effect is not a key.
const (
	// KeyGitWrite allows the git mutations (`POST /git/stage`, `/git/commit`).
	// Off by default: writing somebody's repository is a deliberate decision.
	KeyGitWrite = "git.write"
	// KeyTerminalMax bounds the PTY terminals one client may hold open at once. It
	// replaces the `--terminals` default when set.
	KeyTerminalMax = "terminal.maxSessions"
	// KeyIdleTimeout wraps up a session after this much silence ("0" disables the
	// watchdog). It is a duration string, as the CLI flag is.
	KeyIdleTimeout = "session.idleTimeout"
	// KeyWrapUpPrompt is what an idle session is asked before it is stopped.
	KeyWrapUpPrompt = "session.wrapUpPrompt"
	// KeyAuditRetentionDays is how long the audit trail is kept.
	KeyAuditRetentionDays = "audit.retentionDays"
)

// Kind is the JSON type a key accepts.
type Kind string

// The three kinds a setting can have. A duration is a string with a unit
// ("1h", "30s") and an integer is a count, which is what keeps a value readable in
// the settings file and in a `PATCH` body.
const (
	KindBool     Kind = "bool"
	KindInt      Kind = "int"
	KindDuration Kind = "duration"
	KindString   Kind = "string"
)

// Definition is one known key: its type, its default and what it does.
type Definition struct {
	// Key is the dotted name a client sends.
	Key string
	// Kind is the accepted JSON type.
	Kind Kind
	// Default is the value in force when the store has nothing (already JSON).
	Default json.RawMessage
	// Description is the sentence a settings screen shows.
	Description string
	// Min and Max bound an integer (0 means unbounded).
	Min int
	Max int
}

// Keys is the whole catalogue, in the order a settings screen lists it.
var Keys = []Definition{
	{
		Key:         KeyGitWrite,
		Kind:        KindBool,
		Default:     json.RawMessage("false"),
		Description: "Allow git mutations (stage, commit). Off by default: writing a repository is a deliberate decision.",
	},
	{
		Key:         KeyTerminalMax,
		Kind:        KindInt,
		Default:     json.RawMessage("4"),
		Description: "How many PTY terminals one client may hold open at once.",
		Min:         1,
		Max:         32,
	},
	{
		Key:         KeyIdleTimeout,
		Kind:        KindDuration,
		Default:     json.RawMessage(`"1h"`),
		Description: "Wrap up an idle session after this much silence; \"0\" disables the watchdog.",
	},
	{
		Key:         KeyWrapUpPrompt,
		Kind:        KindString,
		Default:     json.RawMessage(`""`),
		Description: "What an idle session is asked before it is stopped. Empty stops it immediately.",
	},
	{
		Key:         KeyAuditRetentionDays,
		Kind:        KindInt,
		Default:     json.RawMessage("30"),
		Description: "How many days of audit trail the server keeps.",
		Min:         1,
		Max:         3650,
	},
}

// Publisher is the subset of the WebSocket hub this package needs (injected, never a
// global), so a running server tells its clients that a setting changed. It takes a
// ws.Event, exactly like the sessions package's publisher: one event envelope for
// every server-side producer.
type Publisher interface {
	Publish(ev ws.Event) uint64
}

// Store is where the values live (internal/store implements it).
type Store interface {
	Get(key string) ([]byte, bool, error)
	All() (map[string][]byte, error)
	Set(key string, value []byte, at time.Time) error
	Delete(key string) error
}

// Service reads and writes the settings of one server.
type Service struct {
	store  Store
	events Publisher

	mu     sync.Mutex
	cached map[string]json.RawMessage
	loaded bool
}

// New builds the service. A nil store means every key keeps its default and a write
// is refused: a server without a state directory has nowhere to remember a change.
func New(store Store, events Publisher) *Service {
	return &Service{store: store, events: events}
}

// definitions indexes Keys by name.
var definitions = func() map[string]Definition {
	index := make(map[string]Definition, len(Keys))
	for _, definition := range Keys {
		index[definition.Key] = definition
	}
	return index
}()

// Snapshot is every setting with its default already applied.
type Snapshot struct {
	// Values is the effective value per key.
	Values map[string]json.RawMessage
	// Defaults is the default per key, so a client can offer "reset".
	Defaults map[string]json.RawMessage
	// Known lists the keys in catalogue order.
	Known []Definition
}

// Values returns the effective settings, defaults included.
func (s *Service) Values() (Snapshot, error) {
	stored, err := s.stored()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{
		Values:   make(map[string]json.RawMessage, len(Keys)),
		Defaults: make(map[string]json.RawMessage, len(Keys)),
		Known:    append([]Definition(nil), Keys...),
	}
	for _, definition := range Keys {
		snapshot.Defaults[definition.Key] = definition.Default
		if value, ok := stored[definition.Key]; ok {
			snapshot.Values[definition.Key] = value
			continue
		}
		snapshot.Values[definition.Key] = definition.Default
	}
	return snapshot, nil
}

// Get returns one effective value.
func (s *Service) Get(key string) (json.RawMessage, error) {
	if _, known := definitions[key]; !known {
		return nil, unknownKey(key)
	}
	values, err := s.Values()
	if err != nil {
		return nil, err
	}
	return values.Values[key], nil
}

// Set validates and stores one value, then announces the change.
func (s *Service) Set(key string, raw json.RawMessage, actor string) (json.RawMessage, error) {
	definition, known := definitions[key]
	if !known {
		return nil, unknownKey(key)
	}
	if s.store == nil {
		return nil, sessions.Codedf(sessions.CodeUnsupported,
			"this server has no state directory, so %s cannot be stored", key)
	}
	normalized, err := validate(definition, raw)
	if err != nil {
		return nil, err
	}
	if err := s.store.Set(key, normalized, time.Now()); err != nil {
		return nil, sessions.Codedf(sessions.CodeInternal, "%v", err)
	}
	s.mu.Lock()
	s.cached = nil
	s.loaded = false
	s.mu.Unlock()

	s.publish(key, normalized, actor)
	return normalized, nil
}

// SetMany validates every change first and writes them only if all of them are
// acceptable, so a `PATCH` carrying one typo changes nothing instead of half of what
// the admin asked for.
func (s *Service) SetMany(changes map[string]json.RawMessage, actor string) (map[string]json.RawMessage, error) {
	if len(changes) == 0 {
		return nil, sessions.Codedf(sessions.CodeBadRequest, "no setting to change")
	}
	if s.store == nil {
		return nil, sessions.Codedf(sessions.CodeUnsupported,
			"this server has no state directory, so its settings cannot be stored")
	}
	normalized := make(map[string]json.RawMessage, len(changes))
	for key, raw := range changes {
		definition, known := definitions[key]
		if !known {
			return nil, unknownKey(key)
		}
		value, err := validate(definition, raw)
		if err != nil {
			return nil, err
		}
		normalized[key] = value
	}
	for key, value := range normalized {
		if err := s.store.Set(key, value, time.Now()); err != nil {
			return nil, sessions.Codedf(sessions.CodeInternal, "%v", err)
		}
	}
	s.mu.Lock()
	s.cached = nil
	s.loaded = false
	s.mu.Unlock()

	for key, value := range normalized {
		s.publish(key, value, actor)
	}
	return normalized, nil
}

// Reset removes one value, so the key falls back to its default.
func (s *Service) Reset(key, actor string) error {
	definition, known := definitions[key]
	if !known {
		return unknownKey(key)
	}
	if s.store == nil {
		return sessions.Codedf(sessions.CodeUnsupported,
			"this server has no state directory, so %s cannot be reset", key)
	}
	if err := s.store.Delete(key); err != nil {
		return sessions.Codedf(sessions.CodeInternal, "%v", err)
	}
	s.mu.Lock()
	s.cached = nil
	s.loaded = false
	s.mu.Unlock()

	s.publish(key, definition.Default, actor)
	return nil
}

// Bool reads a boolean setting; a value that is not a boolean reads as its default,
// because a corrupted row must not become a panic in a request path.
func (s *Service) Bool(ctx context.Context, key string) bool {
	_ = ctx
	raw, err := s.Get(key)
	if err != nil {
		return false
	}
	var value bool
	if err := json.Unmarshal(raw, &value); err != nil {
		var fallback bool
		if definition, ok := definitions[key]; ok {
			_ = json.Unmarshal(definition.Default, &fallback)
		}
		return fallback
	}
	return value
}

// Int reads an integer setting.
func (s *Service) Int(ctx context.Context, key string) int {
	_ = ctx
	raw, err := s.Get(key)
	if err != nil {
		return 0
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0
	}
	return value
}

// String reads a string setting.
func (s *Service) String(ctx context.Context, key string) string {
	_ = ctx
	raw, err := s.Get(key)
	if err != nil {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return ""
	}
	return value
}

// stored reads the store once and keeps it until something changes.
func (s *Service) stored() (map[string]json.RawMessage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loaded {
		return s.cached, nil
	}
	if s.store == nil {
		s.cached = map[string]json.RawMessage{}
		s.loaded = true
		return s.cached, nil
	}
	raw, err := s.store.All()
	if err != nil {
		return nil, sessions.Codedf(sessions.CodeInternal, "%v", err)
	}
	cached := make(map[string]json.RawMessage, len(raw))
	for key, value := range raw {
		if _, known := definitions[key]; !known {
			// A key that is no longer known is not lost, it is just not read: a
			// downgrade must not delete an admin's value.
			continue
		}
		cached[key] = json.RawMessage(value)
	}
	s.cached = cached
	s.loaded = true
	return cached, nil
}

// publish announces one change, if a hub is wired.
func (s *Service) publish(key string, value json.RawMessage, actor string) {
	if s.events == nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"key":   key,
		"value": value,
		"actor": actor,
	})
	if err != nil {
		return
	}
	s.events.Publish(ws.Event{Type: EventSettingsChanged, Payload: payload})
}

// EventSettingsChanged is the event a change publishes (docs/ws-protocol.md).
const EventSettingsChanged = "server.settings.changed"

// validate checks a value against its definition and returns the canonical form.
func validate(definition Definition, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, sessions.Codedf(sessions.CodeBadRequest, "%s needs a value", definition.Key)
	}
	switch definition.Kind {
	case KindBool:
		var value bool
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, sessions.Codedf(sessions.CodeBadRequest, "%s is a boolean", definition.Key)
		}
		return json.RawMessage(strconv.FormatBool(value)), nil
	case KindInt:
		var value int
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, sessions.Codedf(sessions.CodeBadRequest, "%s is an integer", definition.Key)
		}
		if definition.Min != 0 && value < definition.Min {
			return nil, sessions.Codedf(sessions.CodeBadRequest,
				"%s is at least %d", definition.Key, definition.Min)
		}
		if definition.Max != 0 && value > definition.Max {
			return nil, sessions.Codedf(sessions.CodeBadRequest,
				"%s is at most %d", definition.Key, definition.Max)
		}
		return json.RawMessage(strconv.Itoa(value)), nil
	case KindDuration:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, sessions.Codedf(sessions.CodeBadRequest,
				"%s is a duration string like \"1h\"", definition.Key)
		}
		trimmed := strings.TrimSpace(value)
		if trimmed != "0" && trimmed != "" {
			if _, err := time.ParseDuration(trimmed); err != nil {
				return nil, sessions.Codedf(sessions.CodeBadRequest,
					"%s is not a duration: %v", definition.Key, err)
			}
		}
		encoded, err := json.Marshal(trimmed)
		if err != nil {
			return nil, sessions.Codedf(sessions.CodeBadRequest, "%s is not encodable", definition.Key)
		}
		return encoded, nil
	case KindString:
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, sessions.Codedf(sessions.CodeBadRequest, "%s is a string", definition.Key)
		}
		if len(value) > 4096 {
			return nil, sessions.Codedf(sessions.CodeBadRequest,
				"%s is at most 4096 bytes", definition.Key)
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, sessions.Codedf(sessions.CodeBadRequest, "%s is not encodable", definition.Key)
		}
		return encoded, nil
	default:
		return nil, sessions.Codedf(sessions.CodeInternal,
			"%s has an unknown kind %q", definition.Key, definition.Kind)
	}
}

// unknownKey is the failure of a key outside the catalogue.
func unknownKey(key string) error {
	known := make([]string, 0, len(Keys))
	for _, definition := range Keys {
		known = append(known, definition.Key)
	}
	sort.Strings(known)
	return sessions.Codedf(sessions.CodeBadRequest,
		"%s is not a setting of this server (known: %s)", key, strings.Join(known, ", "))
}

// Describe renders one definition for a log line or a test failure.
func (d Definition) Describe() string {
	return fmt.Sprintf("%s (%s): %s", d.Key, d.Kind, d.Description)
}

// AuditAction is the trail entry a settings change writes.
const AuditAction = audit.ActionSettingsUpdate
