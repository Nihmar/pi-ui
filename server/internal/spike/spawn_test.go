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

// harnessConfig is the smoke configuration used by the package tests: the
// deterministic fake-pi child, three samples and no settle pause.
func harnessConfig(t *testing.T) Config {
	t.Helper()
	return Config{
		FakePi:   fakeharness.Build(t),
		Sessions: 3,
		Settle:   time.Millisecond,
		Timeout:  30 * time.Second,
	}
}

func TestMeasureSpawnWithHarness(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("child RSS comes from /proc")
	}
	result, err := MeasureSpawn(context.Background(), harnessConfig(t))
	if err != nil {
		t.Fatalf("MeasureSpawn: %v", err)
	}
	if result.N != 3 {
		t.Fatalf("N = %d, want 3", result.N)
	}
	if len(result.ReadyMs) != 3 || len(result.ChildRSSMiB) != 3 {
		t.Fatalf("sample counts = %d/%d, want 3/3", len(result.ReadyMs), len(result.ChildRSSMiB))
	}
	for i, sample := range result.ReadyMs {
		if sample <= 0 {
			t.Errorf("ready sample %d = %v ms, want > 0", i, sample)
		}
	}
	if result.P50Ms > result.P95Ms {
		t.Errorf("p50 %v > p95 %v", result.P50Ms, result.P95Ms)
	}
	total := 0.0
	for i, rss := range result.ChildRSSMiB {
		if rss <= 0 {
			t.Errorf("child RSS %d = %v MiB, want > 0", i, rss)
		}
		total += rss
	}
	if diff := result.TotalRSSMiB - total; diff > 0.01 || diff < -0.01 {
		t.Errorf("TotalRSSMiB = %v, want the sum %v", result.TotalRSSMiB, total)
	}
	if summary := result.Summary(); !strings.Contains(summary, "readyMs") || !strings.Contains(summary, "totalRssMiB") {
		t.Errorf("Summary() = %q", summary)
	}
	t.Log(result.Summary())
}

func TestMeasureSpawnRawOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("child RSS comes from /proc")
	}
	cfg := harnessConfig(t)
	cfg.RawDir = t.TempDir()
	if _, err := MeasureSpawn(context.Background(), cfg); err != nil {
		t.Fatalf("MeasureSpawn: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.RawDir, "spawn.json"))
	if err != nil {
		t.Fatalf("read raw output: %v", err)
	}
	for _, want := range []string{`"readyMs"`, `"childRssMiB"`, `"machine"`, `"sessions": 3`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("raw output misses %s:\n%s", want, data)
		}
	}
}

func TestMeasureSpawnRequiresExecutable(t *testing.T) {
	if _, err := MeasureSpawn(context.Background(), Config{}); err == nil {
		t.Fatal("MeasureSpawn without an executable must fail")
	}
}

func TestMeasureSpawnHonoursCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := MeasureSpawn(ctx, harnessConfig(t)); err == nil {
		t.Fatal("MeasureSpawn with a cancelled context must fail")
	}
}

func TestMeasureSpawnRejectsEarlyExit(t *testing.T) {
	// A child that exits before answering get_state is not a spawn sample: the
	// measurement must fail instead of reporting a ready time that never happened.
	cfg := Config{
		Pi:           "/bin/false",
		Sessions:     1,
		Settle:       time.Millisecond,
		ReadyTimeout: 2 * time.Second,
		Timeout:      10 * time.Second,
	}
	if _, err := MeasureSpawn(context.Background(), cfg); err == nil {
		t.Fatal("a child that exits before get_state must fail the measurement")
	}
}
