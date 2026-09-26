package spike

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

func TestMeasureThroughputWithHarness(t *testing.T) {
	cfg := Config{
		FakePi:      fakeharness.Build(t),
		Sessions:    2,
		Events:      200,
		Clients:     2,
		Settle:      time.Millisecond,
		Warmup:      10 * time.Millisecond,
		RSSInterval: 5 * time.Millisecond,
		Timeout:     30 * time.Second,
	}
	result, err := MeasureThroughput(context.Background(), cfg)
	if err != nil {
		t.Fatalf("MeasureThroughput: %v", err)
	}

	expected := cfg.Sessions * (cfg.Events + 2)
	if result.Events != expected {
		t.Fatalf("Events = %d, want %d", result.Events, expected)
	}
	if result.Loss != 0 {
		t.Fatalf("Loss = %d, want 0", result.Loss)
	}
	if counts := result.ClientEvents(); len(counts) != 2 || counts[0] != expected || counts[1] != expected {
		t.Fatalf("ClientEvents = %v, want two times %d", counts, expected)
	}
	if samples := result.RawLatencyMs(); len(samples) != cfg.Sessions*cfg.Events {
		t.Fatalf("RawLatencyMs has %d samples, want %d", len(samples), cfg.Sessions*cfg.Events)
	}
	if result.DurationSec <= 0 || result.EventsPerSec <= 0 {
		t.Fatalf("duration/perSec = %v/%v, want > 0", result.DurationSec, result.EventsPerSec)
	}
	if result.P50Ms <= 0 || result.P95Ms < result.P50Ms {
		t.Fatalf("latency p50/p95 = %v/%v", result.P50Ms, result.P95Ms)
	}
	if runtime.GOOS == "linux" {
		if series := result.RSSSeries(); len(series) < 2 {
			t.Fatalf("RSS series has %d samples, want >= 2", len(series))
		}
		if result.RSSFinalMiB() <= 0 || result.RSSWarmupMiB() <= 0 {
			t.Fatalf("RSS warmup/final = %v/%v, want > 0", result.RSSWarmupMiB(), result.RSSFinalMiB())
		}
	}
	if summary := result.Summary(); !strings.Contains(summary, "eventsPerSec") {
		t.Errorf("Summary() = %q", summary)
	}
	t.Log(result.Summary())
}

func TestMeasureThroughputPaced(t *testing.T) {
	cfg := Config{
		FakePi:      fakeharness.Build(t),
		Sessions:    1,
		Events:      100,
		Rate:        2000,
		Clients:     1,
		Settle:      time.Millisecond,
		RSSInterval: 5 * time.Millisecond,
		Timeout:     30 * time.Second,
	}
	result, err := MeasureThroughput(context.Background(), cfg)
	if err != nil {
		t.Fatalf("MeasureThroughput: %v", err)
	}
	if result.Events != cfg.Events+2 {
		t.Fatalf("Events = %d, want %d", result.Events, cfg.Events+2)
	}
	// 100 events at 2000/s take about 50 ms; the lower bound only proves that the
	// rate was applied at all, because the harness timer granularity of the host
	// may cap the achievable rate (fake-pi is the source of the stream).
	if want := 0.5 * float64(cfg.Events) / cfg.Rate; result.DurationSec < want {
		t.Fatalf("durationSec = %.3f, want >= %.3f for --rate %.0f", result.DurationSec, want, cfg.Rate)
	}
}

func TestMeasureThroughputRawOutput(t *testing.T) {
	cfg := Config{
		FakePi:      fakeharness.Build(t),
		Sessions:    1,
		Events:      50,
		Clients:     1,
		Settle:      time.Millisecond,
		RSSInterval: 5 * time.Millisecond,
		Timeout:     30 * time.Second,
		RawDir:      t.TempDir(),
	}
	if _, err := MeasureThroughput(context.Background(), cfg); err != nil {
		t.Fatalf("MeasureThroughput: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.RawDir, "throughput.json"))
	if err != nil {
		t.Fatalf("read raw output: %v", err)
	}
	for _, want := range []string{`"latencyMs"`, `"clientEvents"`, `"rss"`, `"eventsPerSec"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("raw output misses %s", want)
		}
	}
}

func TestMeasureThroughputRequiresFakePi(t *testing.T) {
	if _, err := MeasureThroughput(context.Background(), Config{}); err == nil {
		t.Fatal("MeasureThroughput without FakePi must fail")
	}
}

func TestMeasureThroughputHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cfg := Config{FakePi: fakeharness.Build(t), Sessions: 1, Events: 10, Timeout: 5 * time.Second}
	if _, err := MeasureThroughput(ctx, cfg); err == nil {
		t.Fatal("MeasureThroughput with a cancelled context must fail")
	}
}

// TestClientErrors pins the failure message of a run that did not settle: a
// client whose reader died must be named, so the report says why the run was
// short instead of only that it was.
func TestClientErrors(t *testing.T) {
	if got := clientErrors(nil); got != "none" {
		t.Errorf("clientErrors(nil) = %q, want %q", got, "none")
	}

	var healthy, failed client
	failed.index = 1
	failed.fail(errors.New("spike: client 1 read: unexpected EOF"))
	if got := clientErrors([]*client{&healthy, &failed}); !strings.Contains(got, "client 1 read: unexpected EOF") {
		t.Errorf("clientErrors = %q, want it to name the reader error", got)
	}
}

// TestClientLatencyReservoir pins the bounded sample window: a short run keeps
// every sample, a long run keeps exactly latencyLimit of them while still
// counting all of them, and the window is deterministic for a given seed — the
// percentiles must not depend on what the replacement policy happened to keep.
func TestClientLatencyReservoir(t *testing.T) {
	var short client
	short.rng = rand.New(rand.NewSource(1))
	for i := 0; i < 5; i++ {
		short.addLatency(float64(i))
	}
	if _, samples, taken := short.counts(); len(samples) != 5 || taken != 5 {
		t.Fatalf("short run retained/taken = %d/%d, want 5/5", len(samples), taken)
	}

	feed := func(c *client, total int) []float64 {
		for i := 0; i < total; i++ {
			c.addLatency(float64(i))
		}
		_, samples, taken := c.counts()
		if taken != total {
			t.Fatalf("samples taken = %d, want %d", taken, total)
		}
		if len(samples) != latencyLimit {
			t.Fatalf("retained %d samples, want the window of %d", len(samples), latencyLimit)
		}
		return samples
	}

	total := latencyLimit + 1000
	var first, second client
	first.rng = rand.New(rand.NewSource(1))
	second.rng = rand.New(rand.NewSource(1))
	samples := feed(&first, total)
	for i := range samples {
		if samples[i] < 0 || samples[i] >= float64(total) {
			t.Fatalf("sample %d = %v, not drawn from the stream", i, samples[i])
		}
	}
	if other := feed(&second, total); !equalFloats(samples, other) {
		t.Error("the same seed and stream produced two different windows")
	}
	// With 1000 replacements offered, the window cannot still be only the first
	// latencyLimit samples: replacement must have happened.
	replaced := false
	for _, sample := range samples {
		if sample >= float64(latencyLimit) {
			replaced = true
			break
		}
	}
	if !replaced {
		t.Error("the window kept only the first latencyLimit samples: no replacement happened")
	}
}

// equalFloats reports whether two sample slices are identical.
func equalFloats(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
