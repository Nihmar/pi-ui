package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/spike"
	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// TestMeasureRegistered pins the registration seam: the command is reachable by
// name and carries a summary for the usage text.
func TestMeasureRegistered(t *testing.T) {
	cmd, ok := lookup("measure")
	if !ok {
		t.Fatal("the measure command is not registered")
	}
	if cmd.Summary == "" || cmd.Run == nil {
		t.Fatalf("measure command = %+v, want a summary and a Run", cmd)
	}
}

// TestParseMeasureConfigDefaults pins the contract behind the exported spike
// defaults: every unset number must stay zero, because the spike package owns
// the fallback values the measurement script relies on.
func TestParseMeasureConfigDefaults(t *testing.T) {
	cfg, err := parseMeasureConfig([]string{"--fake-pi", "/tmp/fake-pi"}, io.Discard)
	if err != nil {
		t.Fatalf("parseMeasureConfig: %v", err)
	}
	if cfg.sessions != 0 || cfg.events != 0 || cfg.clients != 0 || cfg.rate != 0 {
		t.Errorf("unset numbers = %d/%d/%d/%v, want zeros so spike.Default* applies",
			cfg.sessions, cfg.events, cfg.clients, cfg.rate)
	}
	if cfg.fakePi != "/tmp/fake-pi" || cfg.pi != "" || cfg.out != "" {
		t.Errorf("config = %+v, want only fakePi set", cfg)
	}
}

func TestParseMeasureConfigFlags(t *testing.T) {
	cfg, err := parseMeasureConfig([]string{
		"--pi", "/usr/bin/pi",
		"--sessions", "3",
		"--events", "120",
		"--rate", "2500.5",
		"--clients", "2",
		"--out", "/tmp/spike/summary.txt",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parseMeasureConfig: %v", err)
	}
	if cfg.pi != "/usr/bin/pi" || cfg.sessions != 3 || cfg.events != 120 ||
		cfg.rate != 2500.5 || cfg.clients != 2 || cfg.out != "/tmp/spike/summary.txt" {
		t.Errorf("config = %+v, want every flag captured", cfg)
	}
}

func TestParseMeasureConfigErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no target", nil, "one of --pi or --fake-pi is required"},
		{"both targets", []string{"--pi", "pi", "--fake-pi", "fake"}, "mutually exclusive"},
		{"negative sessions", []string{"--fake-pi", "fake", "--sessions", "-1"}, "must not be negative"},
		{"non-numeric rate", []string{"--fake-pi", "fake", "--rate", "fast"}, "invalid value"},
		{"positional argument", []string{"--fake-pi", "fake", "extra"}, "unexpected argument"},
		{"unknown flag", []string{"--fake-pi", "fake", "--nope"}, "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			if _, err := parseMeasureConfig(tt.args, &stderr); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want it to contain %q (stderr %q)", err, tt.want, stderr.String())
			}
		})
	}
}

// TestMeasureUsageQuotesSpikeDefaults keeps the usage text and the measurement
// constants in lockstep: the script and the help must name the same numbers.
func TestMeasureUsageQuotesSpikeDefaults(t *testing.T) {
	var usage bytes.Buffer
	writeMeasureUsage(&usage)
	for _, want := range []string{
		strconv.Itoa(spike.DefaultSessions),
		strconv.Itoa(spike.DefaultEvents),
		strconv.Itoa(spike.DefaultClients),
		spike.DefaultWarmup.String(),
		spike.DefaultTimeout.String(),
		"--pi", "--fake-pi", "--sessions", "--events", "--rate", "--clients", "--out",
	} {
		if !strings.Contains(usage.String(), want) {
			t.Errorf("usage misses %q:\n%s", want, usage.String())
		}
	}
}

// TestMeasureUsageErrorWithoutTarget is the acceptance behaviour: no target is a
// usage error on stderr with the usage exit code, not a panic and not the failure code of
// a run that started.
func TestMeasureUsageErrorWithoutTarget(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"measure"}, &stdout, &stderr)
	if code != ExitUsage {
		t.Fatalf("Run = %d, want %d (stderr %q)", code, ExitUsage, stderr.String())
	}
	if stdout.Len() != 0 {
		t.Errorf("stdout = %q, want it empty", stdout.String())
	}
	for _, want := range []string{"pi-ui measure: one of --pi or --fake-pi is required", "usage: pi-ui measure"} {
		if !strings.Contains(stderr.String(), want) {
			t.Errorf("stderr misses %q:\n%s", want, stderr.String())
		}
	}
}

// TestMeasureTimeoutDerivesFromRun pins the derived deadline that keeps the C9
// soak from being cut off at the five minute default.
func TestMeasureTimeoutDerivesFromRun(t *testing.T) {
	tests := []struct {
		name string
		cfg  measureConfig
		want time.Duration
	}{
		{"unpaced keeps the default", measureConfig{}, spike.DefaultTimeout},
		{"rate without events counts the default events", measureConfig{rate: 2000}, spike.DefaultTimeout},
		{"rapid run stays under the default", measureConfig{events: 150000, rate: 5000}, spike.DefaultTimeout},
		{
			"soak exceeds the default",
			measureConfig{events: 360000, rate: 2000},
			180*time.Second + spike.DefaultWarmup + measureSlack,
		},
		{"negative rate keeps the default", measureConfig{events: 360000, rate: -1}, spike.DefaultTimeout},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := measureTimeout(tt.cfg); got != tt.want {
				t.Errorf("measureTimeout(%+v) = %s, want %s", tt.cfg, got, tt.want)
			}
		})
	}
}

// TestMeasureSpawnSmoke runs the --pi branch end to end against the harness
// binary (which answers get_state like pi does) and checks the file contract:
// the summary file holds the printed line, the raw samples sit next to it.
func TestMeasureSpawnSmoke(t *testing.T) {
	fakePi := fakeharness.Build(t)
	out := filepath.Join(t.TempDir(), "nested", "summary.txt")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runMeasure(ctx, []string{
		"--pi", fakePi,
		"--sessions", "1",
		"--out", out,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runMeasure: %v (stderr %q)", err, stderr.String())
	}

	if !strings.Contains(stdout.String(), "spawn: n=1") {
		t.Errorf("stdout = %q, want the spawn summary", stdout.String())
	}
	assertSummaryFile(t, out, stdout.String())
	assertRawFiles(t, filepath.Dir(out), "spawn.json")
}

// TestMeasureFakePiSmoke runs the --fake-pi branch: the idle server RSS
// measurement and then the throughput measurement, both reported.
func TestMeasureFakePiSmoke(t *testing.T) {
	fakePi := fakeharness.Build(t)
	out := filepath.Join(t.TempDir(), "nested", "summary.txt")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	var stdout, stderr bytes.Buffer
	if err := runMeasure(ctx, []string{
		"--fake-pi", fakePi,
		"--sessions", "1",
		"--events", "20",
		"--clients", "1",
		"--rate", "0",
		"--out", out,
	}, &stdout, &stderr); err != nil {
		t.Fatalf("runMeasure: %v (stderr %q)", err, stderr.String())
	}

	for _, want := range []string{"server-rss: sessions=1 clients=1", "throughput: sessions=1 clients=1"} {
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout misses %q:\n%s", want, stdout.String())
		}
	}
	assertSummaryFile(t, out, stdout.String())
	assertRawFiles(t, filepath.Dir(out), "server-rss.json", "throughput.json")
}

// assertSummaryFile checks that --out holds exactly the summaries printed on
// stdout, which is what server/scripts/measure.sh concatenates.
func assertSummaryFile(t *testing.T, path, stdout string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read --out file: %v", err)
	}
	if string(data) != stdout {
		t.Errorf("summary file = %q, want exactly the printed summaries %q", data, stdout)
	}
}

// assertRawFiles checks that the raw samples of every measurement landed in the
// --out directory, where the report points for evidence.
func assertRawFiles(t *testing.T, dir string, names ...string) {
	t.Helper()
	for _, name := range names {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Errorf("raw sample %s: %v", path, err)
			continue
		}
		if info.Size() == 0 {
			t.Errorf("raw sample %s is empty", path)
		}
	}
}
