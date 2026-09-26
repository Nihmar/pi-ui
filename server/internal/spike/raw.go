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

// writeRaw stores one measurement's raw samples as JSON under dir. An empty dir is a
// no-op, so a caller that only wants the summary pays nothing. The file lands whole: it is
// written to a temporary file in the same directory and renamed over the target, so a
// reader never sees a half-written sample even if the process dies mid-write. The
// measurement script points RawDir at a gitignored output directory.
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

	tmp, err := os.CreateTemp(dir, name+".tmp-*")
	if err != nil {
		return fmt.Errorf("spike: create temp for %s: %w", name, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeded

	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return fmt.Errorf("spike: write %s: %w", tmpName, err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return fmt.Errorf("spike: chmod %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("spike: close %s: %w", tmpName, err)
	}
	path := filepath.Join(dir, name)
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("spike: rename %s: %w", path, err)
	}
	return nil
}
