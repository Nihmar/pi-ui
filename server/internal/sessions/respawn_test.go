package sessions

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// crashOnce is a fake child that becomes ready and then crashes shortly after, every
// time it is spawned (the script is reused by a respawn).
func crashOnce(t *testing.T) []string {
	t.Helper()
	return fakeChild(t, fakeharness.Script{Faults: fakeharness.Faults{CrashAfterMs: intPointer(200)}})
}

func intPointer(value int) *int { return &value }

func TestCrashedSessionIsRespawnedWithinTheBudget(t *testing.T) {
	mgr, _ := newTestManager(t, func(cfg *Config) {
		cfg.RespawnAttempts = 1
		cfg.RespawnWindow = time.Minute
		cfg.RespawnBackoff = func(int) time.Duration { return 0 }
	})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: crashOnce(t)})
	firstPID := info.PID

	// The child crashes, the session is buffered as respawning, and a new child takes
	// its place.
	waitFor(t, "the session to respawn", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && got.Status == StatusReady && got.PID != 0 && got.PID != firstPID
	})
	respawned, _ := mgr.Get(info.ID)
	if respawned.Status != StatusReady || respawned.PID == firstPID {
		t.Fatalf("session = %+v, want a ready session with a new pid (first %d)", respawned, firstPID)
	}

	// The budget is one attempt: the second crash is final.
	waitFor(t, "the second crash", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && got.Status == StatusCrashed
	})
	time.Sleep(300 * time.Millisecond)
	final, _ := mgr.Get(info.ID)
	if final.Status != StatusCrashed || final.PID != respawned.PID {
		t.Fatalf("session = %+v, want it to stay crashed on the second pid %d", final, respawned.PID)
	}
}

func TestStopCancelsAPendingRespawn(t *testing.T) {
	mgr, _ := newTestManager(t, func(cfg *Config) {
		cfg.RespawnAttempts = 3
		cfg.RespawnWindow = time.Minute
		cfg.RespawnBackoff = func(int) time.Duration { return 400 * time.Millisecond }
	})
	info := startSession(t, mgr, Spec{CWD: t.TempDir(), Command: crashOnce(t)})

	waitFor(t, "the session to wait for its respawn", func() bool {
		got, ok := mgr.Get(info.ID)
		return ok && got.Status == StatusRespawning
	})

	if err := mgr.Stop(context.Background(), info.ID); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	// The stop ends the session immediately instead of waiting out the backoff.
	stopped, _ := mgr.Get(info.ID)
	if stopped.Status != StatusExited {
		t.Fatalf("status = %q, want exited after cancelling the respawn", stopped.Status)
	}

	// The cancelled timer must not bring a child back.
	time.Sleep(600 * time.Millisecond)
	after, _ := mgr.Get(info.ID)
	if after.Status != StatusExited || after.PID != stopped.PID {
		t.Fatalf("session = %+v, want it to stay exited", after)
	}
}

func TestRespawnResumesThePiSessionFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX wrapper script")
	}
	fake := fakeharness.Build(t)
	dir := t.TempDir()
	argvLog := filepath.Join(dir, "argv.log")
	script := fakeharness.WriteScript(t, fakeharness.Script{
		SessionFile: "/tmp/pi-session.jsonl",
		Faults:      fakeharness.Faults{CrashAfterMs: intPointer(200)},
	})
	wrapper := filepath.Join(dir, "pi-wrapper.sh")
	body := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> %q\nexec %q --script %q \"$@\"\n", argvLog, fake, script)
	if err := os.WriteFile(wrapper, []byte(body), 0o755); err != nil {
		t.Fatalf("write wrapper: %v", err)
	}

	mgr, _ := newTestManager(t, func(cfg *Config) {
		cfg.RespawnAttempts = 1
		cfg.RespawnWindow = time.Minute
		cfg.RespawnBackoff = func(int) time.Duration { return 0 }
	})
	startSession(t, mgr, Spec{CWD: dir, Command: []string{wrapper}})

	// Two spawns means the respawn happened; only the second argv may carry the resume
	// flag, or the respawn would not actually resume (and the first one would be wrong).
	var logged string
	waitFor(t, "the respawn to spawn a second child", func() bool {
		data, err := os.ReadFile(argvLog)
		if err != nil {
			return false
		}
		logged = string(data)
		return strings.Count(logged, "\n") >= 2
	})
	if !strings.Contains(logged, "--session /tmp/pi-session.jsonl") {
		t.Fatalf("no spawn resumed the pi session:\n%s", logged)
	}
	if got := strings.Count(logged, "--session"); got != 1 {
		t.Fatalf("the resume flag appears %d times, want only on the respawn:\n%s", got, logged)
	}
}

func TestRespawnBackoffDoublesAndCaps(t *testing.T) {
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 16 * time.Second}
	for index, expected := range want {
		if got := DefaultRespawnBackoff(index + 1); got != expected {
			t.Fatalf("attempt %d backoff = %s, want %s", index+1, got, expected)
		}
	}
}
