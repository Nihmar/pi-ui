package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Script is the replay program read from --script. The field and tag set is identical
// to fakeharness.Script (docs/spike-interfaces.md §9), so the helper writes files this
// binary replays unchanged.
type Script struct {
	SessionID   string                   `json:"sessionId,omitempty"`
	SessionFile string                   `json:"sessionFile,omitempty"`
	Entries     []json.RawMessage        `json:"entries,omitempty"`
	Startup     []Step                   `json:"startup,omitempty"`
	Commands    map[string]CommandScript `json:"commands,omitempty"`
	Default     *CommandScript           `json:"default,omitempty"`
	Faults      Faults                   `json:"faults,omitempty"`
}

// Step is one record plus the delay that precedes it.
type Step struct {
	DelayMs int             `json:"delayMs,omitempty"`
	Record  json.RawMessage `json:"record"`
}

// CommandScript is the answer to one command type.
type CommandScript struct {
	DelayMs  int             `json:"delayMs,omitempty"`
	Response json.RawMessage `json:"response,omitempty"`
	Events   []Step          `json:"events,omitempty"`
	Error    string          `json:"error,omitempty"`
	// ExitCode, when set, exits the child right after this command's answer and events.
	// It is the deterministic counterpart of the wall-clock --exit-after/--crash-after:
	// a test arms a crash after readiness by sending the command, never by racing a timer.
	ExitCode *int `json:"exitCode,omitempty"`
}

// Faults are the scripted wall-clock faults; nil means "not set".
type Faults struct {
	StallMs      int  `json:"stallMs,omitempty"`
	CrashAfterMs *int `json:"crashAfterMs,omitempty"`
	ExitAfterMs  *int `json:"exitAfterMs,omitempty"`
}

// Canned values of the built-in get_state response.
const (
	defaultSessionID   = "fake-session-id"
	defaultSessionFile = "/tmp/fake-session.jsonl"
)

// sessionState mirrors the RpcSessionState fields the spike reads (schemas/pi.json).
type sessionState struct {
	SessionID             string `json:"sessionId"`
	SessionFile           string `json:"sessionFile"`
	ThinkingLevel         string `json:"thinkingLevel"`
	IsStreaming           bool   `json:"isStreaming"`
	IsCompacting          bool   `json:"isCompacting"`
	SteeringMode          string `json:"steeringMode"`
	FollowUpMode          string `json:"followUpMode"`
	AutoCompactionEnabled bool   `json:"autoCompactionEnabled"`
	MessageCount          int    `json:"messageCount"`
	PendingMessageCount   int    `json:"pendingMessageCount"`
}

// entriesData is the built-in get_entries payload; leafId is a string or null.
type entriesData struct {
	Entries []json.RawMessage `json:"entries"`
	LeafID  json.RawMessage   `json:"leafId"`
}

// loadScript reads the replay program; an empty path yields an empty script, which
// makes the built-in defaults the whole behaviour.
func loadScript(path string) (*Script, error) {
	script := &Script{}
	if path == "" {
		return script, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read script: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(script); err != nil {
		return nil, fmt.Errorf("parse script %s: %w", path, err)
	}
	if err := script.validate(); err != nil {
		return nil, fmt.Errorf("script %s: %w", path, err)
	}
	return script, nil
}

// validate rejects scripts that would otherwise fail mid-run with a half-written
// record or a nonsense fault.
func (s *Script) validate() error {
	if err := s.Faults.validate(); err != nil {
		return err
	}
	if err := validateSteps("startup", s.Startup); err != nil {
		return err
	}
	for name, cs := range s.Commands {
		if err := validateCommandScript("commands["+name+"]", cs); err != nil {
			return err
		}
	}
	if s.Default != nil {
		if err := validateCommandScript("default", *s.Default); err != nil {
			return err
		}
	}
	return nil
}

func validateCommandScript(where string, cs CommandScript) error {
	if cs.DelayMs < 0 {
		return fmt.Errorf("%s: delayMs must not be negative", where)
	}
	if cs.ExitCode != nil && (*cs.ExitCode < 0 || *cs.ExitCode > 255) {
		return fmt.Errorf("%s: exitCode %d is not a process status (0-255)", where, *cs.ExitCode)
	}
	if len(cs.Response) > 0 {
		if err := validateRecord(cs.Response); err != nil {
			return fmt.Errorf("%s.response: %w", where, err)
		}
	}
	return validateSteps(where+".events", cs.Events)
}

func (f Faults) validate() error {
	if f.StallMs < 0 {
		return fmt.Errorf("faults.stallMs must not be negative")
	}
	if f.CrashAfterMs != nil && *f.CrashAfterMs < 0 {
		return fmt.Errorf("faults.crashAfterMs must not be negative")
	}
	if f.ExitAfterMs != nil && *f.ExitAfterMs < 0 {
		return fmt.Errorf("faults.exitAfterMs must not be negative")
	}
	return nil
}

func validateSteps(where string, steps []Step) error {
	for i, step := range steps {
		if step.DelayMs < 0 {
			return fmt.Errorf("%s[%d]: delayMs must not be negative", where, i)
		}
		if err := validateRecord(step.Record); err != nil {
			return fmt.Errorf("%s[%d]: %w", where, i, err)
		}
	}
	return nil
}

func validateRecord(raw json.RawMessage) error {
	if len(raw) == 0 {
		return fmt.Errorf("record is missing")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return fmt.Errorf("record is not a JSON object: %w", err)
	}
	return nil
}

func (s *Script) sessionID() string {
	if s.SessionID != "" {
		return s.SessionID
	}
	return defaultSessionID
}

func (s *Script) sessionFile() string {
	if s.SessionFile != "" {
		return s.SessionFile
	}
	return defaultSessionFile
}

// cannedState is the built-in get_state payload: an idle session that streams nothing.
func (s *Script) cannedState() sessionState {
	return sessionState{
		SessionID:             s.sessionID(),
		SessionFile:           s.sessionFile(),
		ThinkingLevel:         "off",
		IsStreaming:           false,
		IsCompacting:          false,
		SteeringMode:          "one-at-a-time",
		FollowUpMode:          "one-at-a-time",
		AutoCompactionEnabled: true,
		MessageCount:          0,
		PendingMessageCount:   0,
	}
}

// entriesPage returns the script's entries strictly after the `since` cursor plus the
// current leaf id (the last entry of the whole script, unaffected by paging). An
// unknown cursor is an error, which the caller turns into a failing response.
func (s *Script) entriesPage(since string) ([]json.RawMessage, json.RawMessage, error) {
	leaf := json.RawMessage("null")
	if len(s.Entries) > 0 {
		id, err := entryID(s.Entries[len(s.Entries)-1])
		if err != nil {
			return nil, nil, err
		}
		encoded, err := json.Marshal(id)
		if err != nil {
			return nil, nil, err
		}
		leaf = encoded
	}
	if since == "" {
		return s.Entries, leaf, nil
	}
	for i, entry := range s.Entries {
		id, err := entryID(entry)
		if err != nil {
			return nil, nil, err
		}
		if id == since {
			return s.Entries[i+1:], leaf, nil
		}
	}
	return nil, nil, fmt.Errorf("unknown since cursor %q", since)
}

func entryID(raw json.RawMessage) (string, error) {
	var fields struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", fmt.Errorf("entry is not a JSON object: %w", err)
	}
	return fields.ID, nil
}
