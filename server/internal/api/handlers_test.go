package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// fakeSupervisor is the in-memory Supervisor the router tests drive: no child process, no
// child directory, exact control over the failures the handlers must map.
type fakeSupervisor struct {
	mu       sync.Mutex
	infos    []sessions.Info
	startErr error
	stopErr  error
	sent     []string
}

var _ sessions.Supervisor = (*fakeSupervisor)(nil)

func (f *fakeSupervisor) Start(_ context.Context, spec sessions.Spec) (sessions.Info, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.startErr != nil {
		return sessions.Info{}, f.startErr
	}
	info := sessions.Info{
		ID:        "s_0123456789abcdef",
		CWD:       spec.CWD,
		Name:      spec.Name,
		Status:    sessions.StatusReady,
		PID:       4242,
		CreatedAt: "2026-01-01T00:00:00.000Z",
	}
	f.infos = append(f.infos, info)
	return info, nil
}

func (f *fakeSupervisor) Get(id string) (sessions.Info, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, info := range f.infos {
		if info.ID == id {
			return info, true
		}
	}
	return sessions.Info{}, false
}

func (f *fakeSupervisor) List() []sessions.Info {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]sessions.Info(nil), f.infos...)
}

func (f *fakeSupervisor) Send(_ context.Context, sessionID, op string, _ json.RawMessage) (json.RawMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, sessionID+":"+op)
	return json.RawMessage(`{}`), nil
}

func (f *fakeSupervisor) Stop(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopErr != nil {
		return f.stopErr
	}
	for i := range f.infos {
		if f.infos[i].ID == id {
			f.infos[i].Status = sessions.StatusStopping
			return nil
		}
	}
	return sessions.ErrNotFound
}

func (f *fakeSupervisor) Shutdown(context.Context) error { return nil }

// fakeHub is the smallest ws.Hub that satisfies the router: the REST tests must not need a
// websocket, but the router mounts /ws/v1 when a hub is wired.
type fakeHub struct {
	handler ws.CommandHandler
	replay  ws.Replayer
	dialog  ws.DialogHandler
	closed  bool
}

var _ ws.Hub = (*fakeHub)(nil)

func (f *fakeHub) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusSwitchingProtocols)
}
func (f *fakeHub) Publish(ws.Event) uint64               { return 1 }
func (f *fakeHub) SetReplayer(r ws.Replayer)             { f.replay = r }
func (f *fakeHub) SetCommandHandler(h ws.CommandHandler) { f.handler = h }
func (f *fakeHub) SetDialogHandler(h ws.DialogHandler)   { f.dialog = h }
func (f *fakeHub) Close() error                          { f.closed = true; return nil }

// newTestRouter builds the router with the spike authenticator and a configurable token.
func newTestRouter(t *testing.T, supervisor *fakeSupervisor, token string) http.Handler {
	t.Helper()
	return NewRouter(Options{
		Supervisor: supervisor,
		Hub:        &fakeHub{},
		Info: ServerInfo{
			Version:  "0.0.1-test",
			Features: []string{"sessions", "replay"},
			Limits:   map[string]any{"maxSessions": 8},
		},
		Auth: NewLoopbackOrToken(token),
	})
}

// do runs one request against the router with the peer address set explicitly: the
// authenticator trusts loopback, so a test must say where the request came from.
func do(t *testing.T, handler http.Handler, req *http.Request, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	if remoteAddr != "" {
		req.RemoteAddr = remoteAddr
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

// decodeError returns the code and message of an error envelope.
func decodeError(t *testing.T, recorder *httptest.ResponseRecorder) (string, string) {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not the error envelope: %v", recorder.Body.String(), err)
	}
	return body.Error.Code, body.Error.Message
}

// requireJSON asserts the content type and that the protocol header is always echoed.
func requireJSON(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if got := recorder.Header().Get(protocolHeader); got != "1" {
		t.Errorf("%s = %q, want 1", protocolHeader, got)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}
