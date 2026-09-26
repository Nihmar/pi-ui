package spike

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestWithDefaults(t *testing.T) {
	got := Config{}.withDefaults()
	if got.Sessions != DefaultSessions || got.Events != DefaultEvents || got.Clients != DefaultClients {
		t.Fatalf("counts = %d/%d/%d, want %d/%d/%d",
			got.Sessions, got.Events, got.Clients, DefaultSessions, DefaultEvents, DefaultClients)
	}
	if got.Settle != DefaultSettle || got.Warmup != DefaultWarmup || got.RSSInterval != DefaultRSSInterval {
		t.Fatalf("durations = %v/%v/%v, want %v/%v/%v",
			got.Settle, got.Warmup, got.RSSInterval, DefaultSettle, DefaultWarmup, DefaultRSSInterval)
	}
	if got.ReadyTimeout != DefaultReadyTimeout || got.Timeout != DefaultTimeout {
		t.Fatalf("timeouts = %v/%v, want %v/%v",
			got.ReadyTimeout, got.Timeout, DefaultReadyTimeout, DefaultTimeout)
	}
}

func TestWithDefaultsKeepsExplicitValues(t *testing.T) {
	cfg := Config{Sessions: 2, Events: 5, Rate: 100, Clients: 3, Settle: time.Millisecond, Timeout: time.Second}
	got := cfg.withDefaults()
	if got.Sessions != 2 || got.Events != 5 || got.Rate != 100 || got.Clients != 3 {
		t.Fatalf("explicit values were overwritten: %+v", got)
	}
	if got.Settle != time.Millisecond || got.Timeout != time.Second {
		t.Fatalf("explicit durations were overwritten: %+v", got)
	}
}

func TestSpawnExecutablePrefersPi(t *testing.T) {
	got, err := Config{Pi: "/usr/bin/pi", FakePi: "/tmp/fake-pi"}.spawnExecutable()
	if err != nil || got != "/usr/bin/pi" {
		t.Fatalf("spawnExecutable = %q, %v; want /usr/bin/pi", got, err)
	}
	got, err = Config{FakePi: "/tmp/fake-pi"}.spawnExecutable()
	if err != nil || got != "/tmp/fake-pi" {
		t.Fatalf("spawnExecutable fallback = %q, %v; want /tmp/fake-pi", got, err)
	}
	if _, err := (Config{}).spawnExecutable(); err == nil {
		t.Fatal("spawnExecutable with no path must fail")
	}
}

func TestRequireFakePi(t *testing.T) {
	if _, err := (Config{}).requireFakePi(); err == nil {
		t.Fatal("requireFakePi with no path must fail")
	}
	if got, err := (Config{FakePi: "/tmp/fake-pi"}).requireFakePi(); err != nil || got != "/tmp/fake-pi" {
		t.Fatalf("requireFakePi = %q, %v", got, err)
	}
}

func TestWorkRootAndSessionDirs(t *testing.T) {
	base := t.TempDir()
	cfg := Config{WorkDir: base}
	root, cleanup, err := cfg.workRoot("spike-test-")
	if err != nil {
		t.Fatalf("workRoot: %v", err)
	}
	if filepath.Dir(root) != base {
		t.Fatalf("workRoot = %s, want inside %s", root, base)
	}
	dir, err := sessionDir(root, 3)
	if err != nil {
		t.Fatalf("sessionDir: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("session dir %s: %v", dir, err)
	}
	cleanup()
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("cleanup left %s behind: %v", root, err)
	}
}

func TestWorkRootRejectsMissingDir(t *testing.T) {
	if _, _, err := (Config{WorkDir: filepath.Join(t.TempDir(), "missing")}).workRoot("spike-test-"); err == nil {
		t.Fatal("workRoot with a missing WorkDir must fail")
	}
}

func TestCurrentMachine(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("machine facts come from /proc")
	}
	m, err := CurrentMachine()
	if err != nil {
		t.Fatalf("CurrentMachine: %v", err)
	}
	if m.CPU == "" {
		t.Error("CPU is empty")
	}
	if m.Cores < 1 {
		t.Errorf("Cores = %d, want >= 1", m.Cores)
	}
	if m.MemTotalMiB <= 0 {
		t.Errorf("MemTotalMiB = %v, want > 0", m.MemTotalMiB)
	}
	if m.Kernel == "" {
		t.Error("Kernel is empty")
	}
	t.Logf("machine: %+v", m)
}

func TestWriteRaw(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "raw")
	if err := writeRaw(dir, "sample.json", map[string]int{"n": 1}); err != nil {
		t.Fatalf("writeRaw: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "sample.json")); err != nil {
		t.Fatalf("raw file: %v", err)
	}
	if err := writeRaw("", "ignored.json", nil); err != nil {
		t.Fatalf("writeRaw with empty dir must be a no-op: %v", err)
	}
}
