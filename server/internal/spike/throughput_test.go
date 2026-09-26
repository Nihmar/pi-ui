package spike

import (
	"context"
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
