package e2e

import (
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// TestFullServerOverFakePi is the whole spike in one test: the built binary serves
// HTTP + WebSocket, REST creates a session, a WebSocket client prompts it and sees
// the child's events, and the session stops through REST.
func TestFullServerOverFakePi(t *testing.T) {
	script := fakeharness.Script{
		SessionID: "fake-e2e-session",
		Commands: map[string]fakeharness.CommandScript{
			"prompt": {
				Events: []fakeharness.Step{
					{Record: scriptJSON(t, map[string]any{
						"type":                  "message_update",
						"assistantMessageEvent": map[string]any{"type": "text_delta", "delta": "hello from fake-pi"},
					})},
					{Record: scriptJSON(t, map[string]any{"type": "agent_end", "messages": []any{}, "willRetry": false})},
					{Record: scriptJSON(t, map[string]any{"type": "agent_settled"})},
				},
			},
		},
	}
	wrapper := wrapFakePi(t, fakeharness.WriteScript(t, script))
	proc := startServer(t, "--pi", wrapper)
	base := "http://" + proc.addr

	var health struct {
		Status string `json:"status"`
	}
	getJSON(t, base+"/api/v1/health", &health)
	if health.Status != "ok" {
		t.Fatalf("health = %q, want ok", health.Status)
	}

	var created sessionInfo
	postJSON(t, base+"/api/v1/sessions", map[string]any{"cwd": t.TempDir(), "name": "e2e"},
		http.StatusCreated, &created)
	if created.ID == "" || created.PID == 0 || created.Status != "ready" {
		t.Fatalf("created session = %+v, want a ready session with an id and a pid", created)
	}

	client := newWSClient(t, proc.addr)
	client.write(t, fmt.Sprintf(`{"type":"subscribe","sessionId":%q,"replay":false}`, created.ID))
	client.write(t, `{"type":"ping"}`)
	client.readUntil(t, 5*time.Second, "pong")
	client.write(t, fmt.Sprintf(
		`{"type":"command","id":"e2e-1","sessionId":%q,"op":"session.prompt","payload":{"message":"hi"}}`, created.ID))

	var sawUpdate, sawSettled, sawResponse bool
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !(sawUpdate && sawSettled && sawResponse) {
		frame := client.readFrame(t, time.Until(deadline))
		switch frame["type"] {
		case "pi.message_update":
			if payload, ok := frame["payload"].(map[string]any); ok {
				if event, ok := payload["assistantMessageEvent"].(map[string]any); ok && event["delta"] == "hello from fake-pi" {
					sawUpdate = true
				}
			}
		case "pi.agent_settled":
			sawSettled = true
		case "response":
			if frame["id"] == "e2e-1" {
				if ok, _ := frame["ok"].(bool); !ok {
					t.Fatalf("prompt response not ok: %v", frame)
				}
				sawResponse = true
			}
		}
	}
	if !sawUpdate || !sawSettled || !sawResponse {
		t.Fatalf("prompt flow: update=%v settled=%v response=%v", sawUpdate, sawSettled, sawResponse)
	}

	postJSON(t, base+"/api/v1/sessions/"+created.ID+"/stop", nil, http.StatusAccepted, nil)
	waitFor(t, 10*time.Second, "the session to exit", func() bool {
		var current sessionInfo
		getJSON(t, base+"/api/v1/sessions/"+created.ID, &current)
		return current.Status == "exited"
	})

	if elapsed, err := proc.terminate(t); err != nil {
		t.Fatalf("server exit after SIGTERM: %v (stderr:\n%s)", err, proc.stderr.String())
	} else {
		t.Logf("server exited %s after SIGTERM", elapsed)
	}
}

// TestServerSigtermReapsChildren is acceptance criterion C8: SIGTERM to the server
// reaps every child within two seconds and leaves zero orphans.
func TestServerSigtermReapsChildren(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the orphan check uses signals and /proc semantics")
	}
	wrapper := wrapFakePi(t, fakeharness.WriteScript(t, fakeharness.Script{}))
	proc := startServer(t, "--pi", wrapper,
		"--session", t.TempDir(), "--session", t.TempDir())
	base := "http://" + proc.addr

	var pids []int
	waitFor(t, 15*time.Second, "two ready sessions", func() bool {
		var list sessionList
		getJSON(t, base+"/api/v1/sessions", &list)
		pids = pids[:0]
		for _, session := range list.Sessions {
			if session.Status == "ready" && session.PID > 0 {
				pids = append(pids, session.PID)
			}
		}
		return len(pids) == 2
	})

	elapsed, err := proc.terminate(t)
	if err != nil {
		t.Fatalf("server exit after SIGTERM: %v (stderr:\n%s)", err, proc.stderr.String())
	}
	if elapsed > 2*time.Second {
		t.Errorf("server took %s to exit after SIGTERM, want <= 2s", elapsed)
	}
	for _, pid := range pids {
		switch err := syscall.Kill(pid, 0); {
		case err == nil:
			t.Errorf("child pid %d is still alive after the server exited", pid)
		case !errors.Is(err, syscall.ESRCH):
			t.Errorf("checking child pid %d: %v", pid, err)
		}
	}
	t.Logf("SIGTERM → exit in %s, %d children reaped", elapsed, len(pids))
}
