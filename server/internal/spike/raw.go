package spike

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// configSnapshot is the subset of Config a raw sample file records, so a report can
// name the run it points at without re-reading the command line that produced it.
type configSnapshot struct {
	Pi       string  `json:"pi,omitempty"`
	FakePi   string  `json:"fakePi,omitempty"`
	Sessions int     `json:"sessions"`
	Events   int     `json:"events"`
	Rate     float64 `json:"rate"`
	Clients  int     `json:"clients"`
}

// snapshot captures the resolved settings of a measurement.
func (c Config) snapshot() configSnapshot {
	return configSnapshot{
		Pi:       c.Pi,
		FakePi:   c.FakePi,
		Sessions: c.Sessions,
		Events:   c.Events,
		Rate:     c.Rate,
		Clients:  c.Clients,
	}
}

// machineSnapshot returns the host description, or an error string when /proc is
// unreadable, so a raw file always says where it came from.
func machineSnapshot() any {
	machine, err := CurrentMachine()
	if err != nil {
		return map[string]string{"error": err.Error()}
	}
	return machine
}

// writeRaw stores one measurement's raw samples as JSON under dir. An empty dir is a
// no-op, so a caller that only wants the summary pays nothing. The file is written
// whole and the directory is created when missing: the measurement script points
// RawDir at a gitignored output directory, and raw samples must never half-land.
func writeRaw(dir, name string, value any) error {
	if dir == "" {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("spike: raw dir %s: %w", dir, err)
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("spike: encode %s: %w", name, err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("spike: write %s: %w", path, err)
	}
	return nil
}
