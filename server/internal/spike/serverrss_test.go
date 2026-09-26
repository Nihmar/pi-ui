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

func TestMeasureServerRSS(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RSS comes from /proc")
	}
	cfg := Config{
		FakePi:   fakeharness.Build(t),
		Sessions: 2,
		Clients:  1,
		Settle:   50 * time.Millisecond,
		Timeout:  30 * time.Second,
	}
	result, err := MeasureServerRSS(context.Background(), cfg)
	if err != nil {
		t.Fatalf("MeasureServerRSS: %v", err)
	}
	if result.Sessions != 2 || result.Clients != 1 {
		t.Fatalf("setup = %d sessions/%d clients, want 2/1", result.Sessions, result.Clients)
	}
	if result.ServerRSSMiB <= 0 {
		t.Fatalf("ServerRSSMiB = %v, want > 0", result.ServerRSSMiB)
	}
	if len(result.ChildRSSMiB) != 2 || result.ChildrenRSSMiB <= 0 {
		t.Fatalf("child RSS = %v (sum %v), want two positive samples", result.ChildRSSMiB, result.ChildrenRSSMiB)
	}
	if result.TotalRSSMiB < result.ServerRSSMiB {
		t.Fatalf("TotalRSSMiB %v < ServerRSSMiB %v", result.TotalRSSMiB, result.ServerRSSMiB)
	}
	t.Log(result.Summary())
}

func TestMeasureServerRSSRawOutput(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("RSS comes from /proc")
	}
	cfg := Config{
		FakePi:   fakeharness.Build(t),
		Sessions: 1,
		Clients:  1,
		Settle:   50 * time.Millisecond,
		Timeout:  30 * time.Second,
		RawDir:   t.TempDir(),
	}
	if _, err := MeasureServerRSS(context.Background(), cfg); err != nil {
		t.Fatalf("MeasureServerRSS: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.RawDir, "server-rss.json"))
	if err != nil {
		t.Fatalf("read raw output: %v", err)
	}
	for _, want := range []string{`"serverRssMiB"`, `"childRssMiB"`, `"totalRssMiB"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("raw output misses %s", want)
		}
	}
}

func TestMeasureServerRSSRequiresFakePi(t *testing.T) {
	if _, err := MeasureServerRSS(context.Background(), Config{}); err == nil {
		t.Fatal("MeasureServerRSS without FakePi must fail")
	}
}
