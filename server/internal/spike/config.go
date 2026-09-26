package spike

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Defaults the zero fields of Config resolve to (docs/spike-interfaces.md §5.6).
// They are exported so cmd/pi-ui advertises the same numbers in its usage text and
// the measurement script can rely on them.
const (
	// DefaultSessions matches the acceptance matrix: 8 samples for C1 and 8
	// sessions for C3/C4.
	DefaultSessions = 8
	// DefaultEvents is a short, unpaced stream: a smoke measurement, not a load
	// test. A criterion run passes its own --events/--rate.
	DefaultEvents = 10000
	// DefaultClients is one WebSocket subscriber per run; more clients exercise
	// the fan-out at the cost of more copies of every event.
	DefaultClients = 1
	// DefaultSettle is the pause between "last child ready" and the RSS sample, so
	// a runtime that is still allocating at startup is measured idle.
	DefaultSettle = 500 * time.Millisecond
	// DefaultWarmup is the part of a throughput run excluded from the RSS-growth
	// calculation of acceptance criterion C9.
	DefaultWarmup = 30 * time.Second
	// DefaultRSSInterval is how often the server's own RSS is sampled during a
	// throughput run.
	DefaultRSSInterval = 5 * time.Second
	// DefaultReadyTimeout bounds one spawn → get_state-ready round trip.
	DefaultReadyTimeout = 30 * time.Second
	// DefaultTimeout bounds one whole measurement call.
	DefaultTimeout = 5 * time.Minute
)

// Config selects and bounds one measurement. Every zero field falls back to the
// exported default above, so Config{} is a valid smoke configuration.
type Config struct {
	// Pi is the executable MeasureSpawn launches as `pi --mode rpc --no-session`.
	Pi string
	// FakePi is the standalone harness binary (server/test/fake-pi)
	// MeasureThroughput and MeasureServerRSS launch. Throughput is only
	// reproducible against the deterministic child, so the field is required
	// there. MeasureSpawn falls back to it when Pi is empty, which is how the
	// package tests measure without the real agent.
	FakePi string
	// Sessions is the number of concurrent children: spawned and timed by
	// MeasureSpawn, started and driven by MeasureThroughput/MeasureServerRSS.
	Sessions int
	// Events is the number of synthetic message_update records each fake-pi child
	// streams per prompt (fake-pi --emit).
	Events int
	// Rate is the per-child target event rate in events/s (fake-pi --rate);
	// 0 means unpaced.
	Rate float64
	// Clients is the number of WebSocket clients a measurement connects. Every
	// client subscribes to every session, so the hub fan-out is part of the
	// measured path.
	Clients int
	// WorkDir is the base directory for the per-session working directories.
	// Empty means a fresh temp directory, removed when the call returns; either
	// way each child gets its own directory, because sessions are expected to run
	// in different working directories.
	WorkDir string
	// Settle is the pause between "last child ready" and the RSS sample
	// (default DefaultSettle). Pass a small positive value to sample
	// immediately; zero means the default.
	Settle time.Duration
	// Warmup is the window excluded from the RSS-growth calculation of C9
	// (default DefaultWarmup).
	Warmup time.Duration
	// RSSInterval is the sampling interval of the server's own RSS during a
	// throughput run (default DefaultRSSInterval).
	RSSInterval time.Duration
	// ReadyTimeout bounds one spawn → get_state-ready round trip
	// (default DefaultReadyTimeout).
	ReadyTimeout time.Duration
	// Timeout bounds one whole measurement call (default DefaultTimeout). A
	// measurement that needs longer (a 3 minute soak, for example) sets it.
	Timeout time.Duration
	// RawDir, when non-empty, receives one JSON file per measurement with every
	// raw sample taken: spawn.json, throughput.json or server-rss.json. The
	// directory is created when it does not exist; the measurement script points
	// it at a gitignored output directory, so raw samples never enter a commit.
	RawDir string
	// Logger receives lifecycle diagnostics. Nil discards them.
	Logger *slog.Logger
}

// withDefaults returns a copy of c with every zero field resolved. For the counts a
// negative value is a programming error and is deliberately left alone, so the
// measurement's own guard rejects it instead of silently running a different size than
// the caller asked for; the durations treat zero-or-negative as unset, because a
// non-positive duration can only mean "not configured" (Settle, the one duration that
// legitimately wants zero, is documented as needing an explicit value).
func (c Config) withDefaults() Config {
	if c.Sessions == 0 {
		c.Sessions = DefaultSessions
	}
	if c.Events == 0 {
		c.Events = DefaultEvents
	}
	if c.Clients == 0 {
		c.Clients = DefaultClients
	}
	if c.Settle <= 0 {
		c.Settle = DefaultSettle
	}
	if c.Warmup <= 0 {
		c.Warmup = DefaultWarmup
	}
	if c.RSSInterval <= 0 {
		c.RSSInterval = DefaultRSSInterval
	}
	if c.ReadyTimeout <= 0 {
		c.ReadyTimeout = DefaultReadyTimeout
	}
	if c.Timeout <= 0 {
		c.Timeout = DefaultTimeout
	}
	return c
}

// logger returns the configured logger or a discarding one, so call sites never
// branch on nil.
func (c Config) logger() *slog.Logger {
	if c.Logger == nil {
		return slog.New(slog.DiscardHandler)
	}
	return c.Logger
}

// spawnExecutable resolves the child command of MeasureSpawn: the real pi when it
// is configured, the harness otherwise.
func (c Config) spawnExecutable() (string, error) {
	if c.Pi != "" {
		return c.Pi, nil
	}
	if c.FakePi != "" {
		return c.FakePi, nil
	}
	return "", errors.New("spike: no child executable: set Config.Pi (real pi) or Config.FakePi (harness)")
}

// requireFakePi resolves the child command of the throughput measurements.
func (c Config) requireFakePi() (string, error) {
	if c.FakePi == "" {
		return "", errors.New("spike: Config.FakePi is required: throughput is measured against the deterministic harness")
	}
	return c.FakePi, nil
}

// workRoot creates the directory that holds the per-session working directories of
// one measurement, inside Config.WorkDir when set and inside os.TempDir otherwise.
// The returned cleanup removes it again.
func (c Config) workRoot(prefix string) (root string, cleanup func(), err error) {
	if c.WorkDir != "" {
		info, statErr := os.Stat(c.WorkDir)
		if statErr != nil {
			return "", nil, fmt.Errorf("spike: work dir %s: %w", c.WorkDir, statErr)
		}
		if !info.IsDir() {
			return "", nil, fmt.Errorf("spike: work dir %s is not a directory", c.WorkDir)
		}
	}
	root, err = os.MkdirTemp(c.WorkDir, prefix)
	if err != nil {
		return "", nil, fmt.Errorf("spike: create work dir: %w", err)
	}
	return root, func() { _ = os.RemoveAll(root) }, nil
}

// sessionDir creates one child's working directory under root. Separate
// directories are part of the measured setup (docs/spike-interfaces.md §1).
func sessionDir(root string, index int) (string, error) {
	dir := filepath.Join(root, fmt.Sprintf("session-%02d", index))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("spike: create session dir: %w", err)
	}
	return dir, nil
}
