package sessions

import (
	"strings"
	"testing"
)

func TestNewSessionIDShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 256; i++ {
		id, err := newSessionID()
		if err != nil {
			t.Fatalf("newSessionID: %v", err)
		}
		if !strings.HasPrefix(id, sessionIDPrefix) {
			t.Fatalf("id = %q, want the %q prefix", id, sessionIDPrefix)
		}
		body := strings.TrimPrefix(id, sessionIDPrefix)
		if len(body) != 2*sessionIDBytes {
			t.Fatalf("id = %q, want %d hex characters after the prefix", id, 2*sessionIDBytes)
		}
		for _, char := range body {
			if !strings.ContainsRune("0123456789abcdef", char) {
				t.Fatalf("id = %q, want lowercase hex only (found %q)", id, char)
			}
		}
		if seen[id] {
			t.Fatalf("id %q was generated twice", id)
		}
		seen[id] = true
	}
}

func TestStatusLiveCoversTheLifecycle(t *testing.T) {
	live := []Status{StatusSpawning, StatusReady, StatusStreaming, StatusStopping}
	for _, status := range live {
		if !status.Live() {
			t.Errorf("%q.Live() = false, want true", status)
		}
	}
	finished := []Status{StatusExited, StatusCrashed}
	for _, status := range finished {
		if status.Live() {
			t.Errorf("%q.Live() = true, want false: a finished session releases its slot", status)
		}
	}
}
