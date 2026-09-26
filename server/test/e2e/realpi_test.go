package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// findRealPi locates the installed pi: an explicit PIUI_TEST_PI, then PATH, then
// the documented ~/.local/bin/pi. The test skips cleanly when none is executable,
// which is what CI needs.
func findRealPi(t *testing.T) string {
	t.Helper()
	if env := os.Getenv("PIUI_TEST_PI"); env != "" {
		if info, err := os.Stat(env); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return env
		}
		t.Fatalf("PIUI_TEST_PI=%s is not an executable file", env)
	}
	if path, err := exec.LookPath("pi"); err == nil {
		return path
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidate := filepath.Join(home, ".local", "bin", "pi")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return candidate
		}
	}
	t.Skip("real pi is not installed (looked at PIUI_TEST_PI, PATH and ~/.local/bin/pi)")
	return ""
}

// TestRealPiSessionBecomesReadyAndStops runs one real pi 0.87.1 child through the
// supervisor: spawn → get_state ready → clean stop. It never prompts a model, so it
// stays deterministic and costs no provider quota.
func TestRealPiSessionBecomesReadyAndStops(t *testing.T) {
	pi := findRealPi(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	start := time.Now()
	manager := sessions.New(sessions.Config{
		PiCommand:   []string{pi, "--mode", "rpc", "--no-session"},
		MaxSessions: 1,
	})
	info, err := manager.Start(ctx, sessions.Spec{CWD: t.TempDir(), Name: "real-pi-e2e"})
	if err != nil {
		t.Fatalf("start real pi session: %v", err)
	}
	if info.Status != sessions.StatusReady {
		t.Fatalf("status = %q, want ready", info.Status)
	}
	if info.PID <= 0 {
		t.Fatalf("pid = %d, want > 0", info.PID)
	}

	if err := manager.Stop(ctx, info.ID); err != nil {
		t.Fatalf("stop real pi session: %v", err)
	}
	waitFor(t, 20*time.Second, "the real pi session to exit", func() bool {
		current, ok := manager.Get(info.ID)
		return ok && current.Status == sessions.StatusExited
	})
	current, ok := manager.Get(info.ID)
	if !ok {
		t.Fatal("session vanished from the supervisor after stopping")
	}
	if current.ExitCode == nil || *current.ExitCode != 0 {
		t.Errorf("exit code = %v, want 0", current.ExitCode)
	}
	t.Logf("real pi ready in %s, stopped cleanly (pid %d)", time.Since(start).Round(time.Millisecond), info.PID)
}
