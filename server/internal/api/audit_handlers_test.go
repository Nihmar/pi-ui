package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/auth"
)

// newAuditRouter wires the real auth service and the real audit log (memory store),
// so the events the handlers record are the ones the endpoint returns.
func newAuditRouter(t *testing.T) (http.Handler, *auth.Service, *audit.MemoryStore) {
	t.Helper()
	service := auth.New(auth.NewMemoryStore(), auth.Options{
		Now:       func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) },
		Argon2:    auth.Argon2Params{Time: 1, Memory: 8, Threads: 1, KeyLen: 32},
		PairLimit: 10,
	})
	store := audit.NewMemoryStore()
	log := audit.New(store, audit.Options{Now: func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) }})
	handler := NewRouter(Options{
		Supervisor:  &fakeSupervisor{},
		Hub:         &fakeHub{},
		Info:        ServerInfo{Version: "0.1.0-test", Features: []string{"sessions"}, Limits: map[string]any{}},
		AuthService: service,
		Audit:       log,
	})
	return handler, service, store
}

// adminToken sets a password and pairs an admin device.
func adminToken(t *testing.T, handler http.Handler, service *auth.Service) string {
	t.Helper()
	if err := service.SetAdminPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}
	recorder := pair(t, handler, `{"deviceName":"admin shell","password":"correct horse battery staple"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("admin pairing = %d (body %s)", recorder.Code, recorder.Body.String())
	}
	return decodePair(t, recorder).Token
}

// auditEntries reads the trail through the endpoint.
func auditEntries(t *testing.T, handler http.Handler, token, query string) ([]map[string]any, bool) {
	t.Helper()
	recorder := withToken(t, handler, http.MethodGet, "/api/v1/audit"+query, token)
	requireJSON(t, recorder)
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /audit%s = %d (body %s)", query, recorder.Code, recorder.Body.String())
	}
	var body struct {
		Entries   []map[string]any `json:"entries"`
		Truncated *bool            `json:"truncated"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("audit body: %v", err)
	}
	return body.Entries, body.Truncated != nil && *body.Truncated
}

func TestAuditRecordsPairingAndServesItToAnAdmin(t *testing.T) {
	handler, service, _ := newAuditRouter(t)
	token := adminToken(t, handler, service)

	entries, _ := auditEntries(t, handler, token, "")
	if len(entries) == 0 {
		t.Fatal("the trail is empty after a pairing")
	}
	first := entries[0]
	if first["action"] != "auth.pair" || first["outcome"] != "ok" {
		t.Fatalf("newest entry = %v, want the pairing", first)
	}
	if first["actorScope"] != "admin" || first["actorName"] != "admin shell" {
		t.Fatalf("actor = %v, want the admin shell", first)
	}
	if first["remoteAddr"] != "127.0.0.1" {
		t.Fatalf("remoteAddr = %v, want the loopback peer host", first["remoteAddr"])
	}
}

func TestAuditRecordsDenials(t *testing.T) {
	handler, service, _ := newAuditRouter(t)
	token := adminToken(t, handler, service)

	// A remote peer without a token: refused, and the refusal is in the trail.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	requireJSON(t, do(t, handler, req, "198.51.100.7:4321"))

	entries, _ := auditEntries(t, handler, token, "?action=auth.denied")
	if len(entries) == 0 {
		t.Fatal("the refusal is not in the trail")
	}
	if entries[0]["outcome"] != "denied" || entries[0]["remoteAddr"] != "198.51.100.7" {
		t.Fatalf("denial = %v, want denied from the remote peer", entries[0])
	}
}

func TestAuditRecordsSessionsAndFilters(t *testing.T) {
	handler, service, _ := newAuditRouter(t)
	token := adminToken(t, handler, service)

	create := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"cwd":"/tmp/x"}`))
	create.Header.Set("Authorization", "Bearer "+token)
	if got := do(t, handler, create, "127.0.0.1:55000"); got.Code != http.StatusCreated {
		t.Fatalf("create = %d (body %s)", got.Code, got.Body.String())
	}

	entries, truncated := auditEntries(t, handler, token, "?action=session.create")
	if truncated {
		t.Fatal("truncated for a single entry")
	}
	if len(entries) != 1 || entries[0]["action"] != "session.create" {
		t.Fatalf("session.create entries = %v", entries)
	}
	if entries[0]["sessionId"] == "" || entries[0]["actorDeviceId"] == "" {
		t.Fatalf("entry = %v, want the session and the actor", entries[0])
	}

	// A page of one reports that more matched.
	page, truncated := auditEntries(t, handler, token, "?limit=1")
	if len(page) != 1 || !truncated {
		t.Fatalf("page = %d truncated = %v, want 1 and true", len(page), truncated)
	}
}

func TestAuditEndpointRejectsBadFiltersAndRefusesNonAdmins(t *testing.T) {
	handler, service, _ := newAuditRouter(t)
	admin := adminToken(t, handler, service)

	operatorInvite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	operator := decodePair(t, pair(t, handler, `{"deviceName":"phone","code":"`+operatorInvite.Code+`"}`)).Token

	for name, query := range map[string]string{
		"bad since": "?since=yesterday",
		"bad limit": "?limit=0",
		"limit nan": "?limit=many",
	} {
		t.Run(name, func(t *testing.T) {
			recorder := withToken(t, handler, http.MethodGet, "/api/v1/audit"+query, admin)
			requireJSON(t, recorder)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body %s)", recorder.Code, recorder.Body.String())
			}
			if code, _ := decodeError(t, recorder); code != "bad_request" {
				t.Fatalf("code = %q, want bad_request", code)
			}
		})
	}

	forbidden := withToken(t, handler, http.MethodGet, "/api/v1/audit", operator)
	requireJSON(t, forbidden)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("operator reading the trail = %d, want 403", forbidden.Code)
	}
	// The forbidden attempt is itself a policy decision in the trail.
	entries, _ := auditEntries(t, handler, admin, "?action=auth.denied")
	found := false
	for _, entry := range entries {
		if details, ok := entry["details"].(map[string]any); ok && details["required"] == "admin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("the scope denial is not in the trail: %v", entries)
	}
}

// adminOnly is an Authenticator that grants admin to every request, so a test can
// reach an admin endpoint without a device store.
type adminOnly struct{}

// Authenticate implements Authenticator.
func (adminOnly) Authenticate(*http.Request) (Scope, error) { return ScopeAdmin, nil }

func TestAuditEndpointWithoutAServiceAnswersUnsupported(t *testing.T) {
	handler := NewRouter(Options{
		Supervisor: &fakeSupervisor{},
		Hub:        &fakeHub{},
		Info:       ServerInfo{Version: "0.1.0-test", Features: []string{}, Limits: map[string]any{}},
		Auth:       adminOnly{},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/audit", nil)
	recorder := do(t, handler, req, "127.0.0.1:55000")
	requireJSON(t, recorder)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (body %s)", recorder.Code, recorder.Body.String())
	}
	if code, _ := decodeError(t, recorder); code != "unsupported" {
		t.Fatalf("code = %q, want unsupported", code)
	}
}
