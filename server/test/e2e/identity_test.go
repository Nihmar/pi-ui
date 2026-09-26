package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Nihmar/pi-ui/server/test/fakeharness"
)

// inviteCodePattern reads the code out of `pi-ui pair` output.
var inviteCodePattern = regexp.MustCompile(`(?m)^  code    ([0-9]{6})$`)

// doAuthJSON performs one authenticated (or anonymous) JSON request.
func doAuthJSON(t *testing.T, method, url, token string, payload any, wantStatus int, into any) []byte {
	t.Helper()
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s = %d, want %d: %s", method, url, resp.StatusCode, wantStatus, data)
	}
	if into != nil {
		if err := json.Unmarshal(data, into); err != nil {
			t.Fatalf("decode %s: %v (%s)", url, err, data)
		}
	}
	return data
}

// expectClose asserts that the next read of the socket fails with the wanted close
// status, whatever frames arrive in between.
func expectClose(t *testing.T, client *wsConn, want websocket.StatusCode) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Until(deadline))
		_, _, err := client.conn.Read(ctx)
		cancel()
		if err != nil {
			if got := websocket.CloseStatus(err); got != want {
				t.Fatalf("close status = %v (%v), want %v", got, err, want)
			}
			return
		}
	}
	t.Fatalf("the connection stayed open, want a %v close", want)
}

// TestDeviceIdentityOverRestAndWebSocket drives the whole Phase 3 identity path with
// the built binary and two processes: `pi-ui pair` mints an invitation in the shared
// state database, the running server exchanges it for an operator token, that token
// opens the WebSocket and drives a session, `pi-ui auth set-password` (a third process,
// while the server runs) enables the admin branch, and revoking the operator device
// closes its socket immediately.
func TestDeviceIdentityOverRestAndWebSocket(t *testing.T) {
	binary := buildServer(t)
	stateDir := t.TempDir()

	// 1. Mint a typed invitation in the state database the server will open.
	pair := exec.Command(binary, "pair", "--state-dir", stateDir, "--kind", "typed")
	output, err := pair.CombinedOutput()
	if err != nil {
		t.Fatalf("pi-ui pair: %v (%s)", err, output)
	}
	match := inviteCodePattern.FindStringSubmatch(string(output))
	if match == nil {
		t.Fatalf("no pairing code in the output:\n%s", output)
	}

	// 2. Start the server over the same state directory.
	script := fakeharness.Script{
		SessionID: "fake-identity-session",
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
	proc := startServer(t, "--state-dir", stateDir, "--pi", wrapper)
	base := "http://" + proc.addr

	// 3. The invitation becomes an operator token.
	var paired struct {
		DeviceId string `json:"deviceId"`
		Token    string `json:"token"`
		Scope    string `json:"scope"`
	}
	doAuthJSON(t, http.MethodPost, base+"/api/v1/auth/pair", "",
		map[string]any{"deviceName": "e2e phone", "platform": "linux", "code": match[1]},
		http.StatusCreated, &paired)
	if paired.Scope != "operator" || paired.Token == "" || paired.DeviceId == "" {
		t.Fatalf("paired = %+v, want an operator token", paired)
	}

	// The server is configured now, so a loopback peer without a token reads but does
	// not write; the token is what drives it.
	doAuthJSON(t, http.MethodPost, base+"/api/v1/sessions", "",
		map[string]any{"cwd": t.TempDir()}, http.StatusForbidden, nil)

	var created sessionInfo
	doAuthJSON(t, http.MethodPost, base+"/api/v1/sessions", paired.Token,
		map[string]any{"cwd": t.TempDir(), "name": "identity"}, http.StatusCreated, &created)
	if created.ID == "" || created.Status != "ready" {
		t.Fatalf("created = %+v, want a ready session", created)
	}

	// 4. The same token opens the WebSocket with the same scope and drives the session.
	client := newWSClientWithToken(t, proc.addr, paired.Token)
	client.write(t, `{"type":"subscribe","sessionId":"`+created.ID+`","replay":false}`)
	client.write(t, `{"type":"command","id":"id-1","sessionId":"`+created.ID+`","op":"session.prompt","payload":{"message":"hi"}}`)

	sawEvent, sawResponse := false, false
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) && !(sawEvent && sawResponse) {
		frame := client.readFrame(t, time.Until(deadline))
		switch frame["type"] {
		case "pi.message_update":
			sawEvent = true
		case "response":
			if ok, _ := frame["ok"].(bool); !ok {
				t.Fatalf("the operator command failed: %v", frame)
			}
			sawResponse = true
		}
	}
	if !sawEvent || !sawResponse {
		t.Fatalf("event=%v response=%v, want both over the authenticated socket", sawEvent, sawResponse)
	}

	// 5. A second process sets the admin password while the server is running: the
	// shared SQLite file is what makes this visible to the live service.
	setPassword := exec.Command(binary, "auth", "set-password", "--state-dir", stateDir, "--password", "correct horse battery staple")
	if out, err := setPassword.CombinedOutput(); err != nil {
		t.Fatalf("pi-ui auth set-password: %v (%s)", err, out)
	}
	var admin struct {
		DeviceId string `json:"deviceId"`
		Token    string `json:"token"`
		Scope    string `json:"scope"`
	}
	doAuthJSON(t, http.MethodPost, base+"/api/v1/auth/pair", "",
		map[string]any{"deviceName": "admin shell", "password": "correct horse battery staple"},
		http.StatusCreated, &admin)
	if admin.Scope != "admin" {
		t.Fatalf("admin scope = %q, want admin", admin.Scope)
	}

	// The admin list shows both devices.
	var devices struct {
		Devices []struct {
			DeviceID string `json:"deviceId"`
			Name     string `json:"name"`
			Current  bool   `json:"current"`
		} `json:"devices"`
	}
	doAuthJSON(t, http.MethodGet, base+"/api/v1/auth/devices", admin.Token, nil, http.StatusOK, &devices)
	if len(devices.Devices) != 2 {
		t.Fatalf("devices = %+v, want the phone and the admin shell", devices.Devices)
	}

	// 6. Revoking the operator device closes its open socket with 4401.
	doAuthJSON(t, http.MethodDelete, base+"/api/v1/auth/devices/"+paired.DeviceId, admin.Token, nil, http.StatusNoContent, nil)
	expectClose(t, client, websocket.StatusCode(4401))
}
