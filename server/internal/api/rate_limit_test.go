package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/auth"
	"github.com/Nihmar/pi-ui/server/internal/ratelimit"
)

// newRateRouter wires the real auth service, the audit log and both REST budgets.
// A 4/min budget has a burst of one, so the second request of a key is refused.
func newRateRouter(t *testing.T, rest, refresh int) (http.Handler, *auth.Service, *audit.MemoryStore) {
	t.Helper()
	service := auth.New(auth.NewMemoryStore(), auth.Options{
		Now:       func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) },
		Argon2:    auth.Argon2Params{Time: 1, Memory: 8, Threads: 1, KeyLen: 32},
		PairLimit: 10,
	})
	store := audit.NewMemoryStore()
	log := audit.New(store, audit.Options{})
	handler := NewRouter(Options{
		Supervisor:       &fakeSupervisor{},
		Hub:              &fakeHub{},
		Info:             ServerInfo{Version: "0.1.0-test", Features: []string{}, Limits: map[string]any{}},
		AuthService:      service,
		Audit:            log,
		RateLimit:        ratelimit.New(rest),
		RefreshRateLimit: ratelimit.New(refresh),
	})
	return handler, service, store
}

// pairOperator pairs a typed invitation and returns the token.
func pairOperator(t *testing.T, handler http.Handler, service *auth.Service, name string) string {
	t.Helper()
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	recorder := pair(t, handler, `{"deviceName":"`+name+`","code":"`+invite.Code+`"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("pairing %s = %d (body %s)", name, recorder.Code, recorder.Body.String())
	}
	return decodePair(t, recorder).Token
}

func TestRESTBudgetIsPerDeviceAndCarriesRetryAfter(t *testing.T) {
	handler, service, store := newRateRouter(t, 4, 0)
	first := pairOperator(t, handler, service, "phone one")
	second := pairOperator(t, handler, service, "phone two")

	if got := withToken(t, handler, http.MethodGet, "/api/v1/sessions", first); got.Code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", got.Code)
	}
	limited := withToken(t, handler, http.MethodGet, "/api/v1/sessions", first)
	requireJSON(t, limited)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("second request = %d, want 429 (body %s)", limited.Code, limited.Body.String())
	}
	if code, _ := decodeError(t, limited); code != "rate_limited" {
		t.Fatalf("code = %q, want rate_limited", code)
	}
	if retry := limited.Header().Get("Retry-After"); retry == "" || retry == "0" {
		t.Fatalf("Retry-After = %q, want a positive number of seconds", retry)
	}

	// Another device has its own bucket.
	if got := withToken(t, handler, http.MethodGet, "/api/v1/sessions", second); got.Code != http.StatusOK {
		t.Fatalf("request as the second device = %d, want 200", got.Code)
	}

	// The refusal is in the trail as a policy decision.
	events, err := store.Query(audit.Filter{Action: audit.ActionRateLimited})
	if err != nil || len(events) != 1 {
		t.Fatalf("trail = %+v (%v), want one rate.limited", events, err)
	}
	if events[0].ActorDeviceID == "" || events[0].Details["what"] != "rest" {
		t.Fatalf("event = %+v, want the device and the surface", events[0])
	}
}

func TestDisabledRESTBudgetAllowsEveryRequest(t *testing.T) {
	handler, service, _ := newRateRouter(t, 0, 0)
	token := pairOperator(t, handler, service, "phone")
	for i := 0; i < 5; i++ {
		if got := withToken(t, handler, http.MethodGet, "/api/v1/sessions", token); got.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 with the budget off", i+1, got.Code)
		}
	}
}

func TestRefreshBudgetIsSeparateFromTheRESTBudget(t *testing.T) {
	handler, service, _ := newRateRouter(t, 0, 4)
	token := pairOperator(t, handler, service, "phone")

	// The REST budget is off, so reads are unlimited…
	for i := 0; i < 3; i++ {
		if got := withToken(t, handler, http.MethodGet, "/api/v1/sessions", token); got.Code != http.StatusOK {
			t.Fatalf("read %d = %d, want 200", i+1, got.Code)
		}
	}
	// …while the refresh budget is not. Each refresh rotates the token, so the next
	// call uses the new one: the budget is per device, not per token value.
	first := withToken(t, handler, http.MethodPost, "/api/v1/auth/refresh", token)
	if first.Code != http.StatusOK {
		t.Fatalf("first refresh = %d, want 200 (body %s)", first.Code, first.Body.String())
	}
	var rotated struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &rotated); err != nil || rotated.Token == "" {
		t.Fatalf("refresh body = %s (err %v), want a new token", first.Body.String(), err)
	}
	limited := withToken(t, handler, http.MethodPost, "/api/v1/auth/refresh", rotated.Token)
	requireJSON(t, limited)
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("second refresh = %d, want 429 (body %s)", limited.Code, limited.Body.String())
	}
	if retry := limited.Header().Get("Retry-After"); retry == "" {
		t.Fatal("the refresh refusal carries no Retry-After")
	}
}

func TestRateLimitedResponseStillCarriesTheProtocolHeader(t *testing.T) {
	handler, service, _ := newRateRouter(t, 4, 0)
	token := pairOperator(t, handler, service, "phone")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if got := do(t, handler, req, "127.0.0.1:55000"); got.Code != http.StatusOK {
		t.Fatalf("first request = %d, want 200", got.Code)
	}
	limited := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	limited.Header.Set("Authorization", "Bearer "+token)
	recorder := do(t, handler, limited, "127.0.0.1:55000")
	if got := recorder.Header().Get(protocolHeader); got != "1" {
		t.Fatalf("%s = %q, want 1 on a 429", protocolHeader, got)
	}
}
