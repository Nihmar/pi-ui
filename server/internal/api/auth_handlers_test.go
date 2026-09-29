package api

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/auth"
)

// newAuthRouter wires the real auth service with deterministic clock and randomness and
// the cheap argon2 cost, so the pairing endpoints are tested against the production seam
// without the production cost.
func newAuthRouter(t *testing.T) (http.Handler, *auth.Service) {
	t.Helper()
	service := auth.New(auth.NewMemoryStore(), auth.Options{
		Now:            func() time.Time { return time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC) },
		Rand:           rand.New(rand.NewSource(7)),
		Argon2:         auth.Argon2Params{Time: 1, Memory: 8, Threads: 1, KeyLen: 32},
		TokenTTL:       24 * time.Hour,
		MaxDevices:     2,
		PairLimit:      3,
		PairWindow:     time.Minute,
		SlidingRefresh: time.Hour,
	})
	handler := NewRouter(Options{
		Supervisor: &fakeSupervisor{},
		Hub:        &fakeHub{},
		Info: ServerInfo{
			Version:   "0.1.0-test",
			PiVersion: "0.87.1",
			Features:  []string{"sessions"},
			Limits:    map[string]any{"maxSessions": 8},
		},
		AuthService: service,
	})
	return handler, service
}

// pair performs one pairing request from a loopback peer.
func pair(t *testing.T, handler http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/pair", strings.NewReader(body))
	return do(t, handler, req, "127.0.0.1:55000")
}

// withToken runs one request with a bearer token.
func withToken(t *testing.T, handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return do(t, handler, req, "127.0.0.1:55000")
}

type pairBody struct {
	DeviceId  string `json:"deviceId"`
	Token     string `json:"token"`
	Scope     string `json:"scope"`
	ExpiresAt string `json:"expiresAt"`
	Server    struct {
		Version   string   `json:"version"`
		PiVersion string   `json:"piVersion"`
		Protocol  int      `json:"protocol"`
		Features  []string `json:"features"`
	} `json:"server"`
}

func decodePair(t *testing.T, recorder *httptest.ResponseRecorder) pairBody {
	t.Helper()
	var body pairBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("body %q is not a pair response: %v", recorder.Body.String(), err)
	}
	return body
}

func TestPairWithACodeReturnsAnOperatorToken(t *testing.T) {
	handler, service := newAuthRouter(t)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}

	recorder := pair(t, handler, `{"deviceName":"Pixel 9","platform":"android","code":"`+invite.Code+`"}`)
	requireJSON(t, recorder)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", recorder.Code, recorder.Body.String())
	}
	body := decodePair(t, recorder)
	if body.Scope != "operator" {
		t.Fatalf("scope = %q, want operator", body.Scope)
	}
	if body.DeviceId == "" || !strings.HasPrefix(body.Token, body.DeviceId+".") {
		t.Fatalf("token %q does not carry the device id %q", body.Token, body.DeviceId)
	}
	if body.ExpiresAt == "" {
		t.Fatal("the response carries no expiry")
	}
	if body.Server.Version != "0.1.0-test" || body.Server.PiVersion != "0.87.1" || body.Server.Protocol != Protocol {
		t.Fatalf("server identity = %+v, want the wired one", body.Server)
	}

	// The token opens the read and write surfaces of an operator…
	if got := withToken(t, handler, http.MethodGet, "/api/v1/sessions", body.Token); got.Code != http.StatusOK {
		t.Fatalf("GET /sessions with the new token = %d, want 200", got.Code)
	}
	create := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"cwd":"/tmp/x"}`))
	create.Header.Set("Authorization", "Bearer "+body.Token)
	if got := do(t, handler, create, "127.0.0.1:55000"); got.Code != http.StatusCreated {
		t.Fatalf("POST /sessions with the new token = %d, want 201", got.Code)
	}
	// …but not the admin surface.
	admin := withToken(t, handler, http.MethodGet, "/api/v1/auth/devices", body.Token)
	requireJSON(t, admin)
	if admin.Code != http.StatusForbidden {
		t.Fatalf("GET /auth/devices as operator = %d, want 403", admin.Code)
	}
	if code, _ := decodeError(t, admin); code != "forbidden_scope" {
		t.Fatalf("code = %q, want forbidden_scope", code)
	}
}

// TestPairIgnoresALegacySecret proves the compatibility path of the schema change:
// an old client that still sends `secret` alongside the code pairs exactly as if the
// field were absent, because objects are lenient and the field is no longer read.
func TestPairIgnoresALegacySecret(t *testing.T) {
	handler, service := newAuthRouter(t)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}

	recorder := pair(t, handler, `{"deviceName":"old client","platform":"android","code":"`+invite.Code+`","secret":"stale-qr-secret"}`)
	requireJSON(t, recorder)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", recorder.Code, recorder.Body.String())
	}
	if body := decodePair(t, recorder); body.Scope != "operator" || body.Token == "" {
		t.Fatalf("paired = %+v, want an operator token", body)
	}
}

func TestPairWithTheAdminPasswordReturnsAdminAndListsDevices(t *testing.T) {
	handler, service := newAuthRouter(t)
	if err := service.SetAdminPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}

	recorder := pair(t, handler, `{"deviceName":"admin shell","password":"correct horse battery staple"}`)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (body %s)", recorder.Code, recorder.Body.String())
	}
	body := decodePair(t, recorder)
	if body.Scope != "admin" {
		t.Fatalf("scope = %q, want admin", body.Scope)
	}

	listed := withToken(t, handler, http.MethodGet, "/api/v1/auth/devices", body.Token)
	requireJSON(t, listed)
	if listed.Code != http.StatusOK {
		t.Fatalf("GET /auth/devices = %d, want 200", listed.Code)
	}
	var devices struct {
		Devices []struct {
			DeviceID string `json:"deviceId"`
			Name     string `json:"name"`
			Scope    string `json:"scope"`
			Current  bool   `json:"current"`
		} `json:"devices"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &devices); err != nil {
		t.Fatalf("devices body: %v", err)
	}
	if len(devices.Devices) != 1 || devices.Devices[0].DeviceID != body.DeviceId {
		t.Fatalf("devices = %+v, want the caller", devices.Devices)
	}
	if !devices.Devices[0].Current || devices.Devices[0].Name != "admin shell" {
		t.Fatalf("current/name = %+v, want current:true and the pairing name", devices.Devices[0])
	}
}

func TestPairRefusesBadCredentialsAndBadBodies(t *testing.T) {
	handler, _ := newAuthRouter(t)

	for name, tc := range map[string]struct {
		body       string
		wantStatus int
		wantCode   string
	}{
		"unknown code": {
			body: `{"deviceName":"phone","code":"000000"}`, wantStatus: http.StatusUnauthorized, wantCode: "unauthorized",
		},
		"missing device name": {
			body: `{"code":"123456"}`, wantStatus: http.StatusBadRequest, wantCode: "bad_request",
		},
		"neither code nor password": {
			body: `{"deviceName":"phone"}`, wantStatus: http.StatusBadRequest, wantCode: "bad_request",
		},
		"not json": {
			body: `{oops`, wantStatus: http.StatusBadRequest, wantCode: "bad_request",
		},
		"password without one set": {
			body: `{"deviceName":"phone","password":"guess"}`, wantStatus: http.StatusUnauthorized, wantCode: "unauthorized",
		},
	} {
		t.Run(name, func(t *testing.T) {
			recorder := pair(t, handler, tc.body)
			requireJSON(t, recorder)
			if recorder.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tc.wantStatus, recorder.Body.String())
			}
			if code, _ := decodeError(t, recorder); code != tc.wantCode {
				t.Fatalf("code = %q, want %q", code, tc.wantCode)
			}
		})
	}
}

func TestPairIsRateLimitedWithRetryAfter(t *testing.T) {
	handler, _ := newAuthRouter(t)

	var last *httptest.ResponseRecorder
	for i := 0; i < 4; i++ {
		last = pair(t, handler, `{"deviceName":"phone","code":"000000"}`)
	}
	requireJSON(t, last)
	if last.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt 4 = %d, want 429 (body %s)", last.Code, last.Body.String())
	}
	if code, _ := decodeError(t, last); code != "rate_limited" {
		t.Fatalf("code = %q, want rate_limited", code)
	}
	if retry := last.Header().Get("Retry-After"); retry == "" || retry == "0" {
		t.Fatalf("Retry-After = %q, want a positive number of seconds", retry)
	}
}

func TestRefreshRotatesTheToken(t *testing.T) {
	handler, service := newAuthRouter(t)
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	paired := decodePair(t, pair(t, handler, `{"deviceName":"laptop","code":"`+invite.Code+`"}`))

	recorder := withToken(t, handler, http.MethodPost, "/api/v1/auth/refresh", paired.Token)
	requireJSON(t, recorder)
	if recorder.Code != http.StatusOK {
		t.Fatalf("refresh = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}
	var refreshed struct {
		Token     string `json:"token"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &refreshed); err != nil {
		t.Fatalf("refresh body: %v", err)
	}
	if refreshed.Token == paired.Token || refreshed.Token == "" {
		t.Fatalf("refresh token = %q, want a new one", refreshed.Token)
	}
	if got := withToken(t, handler, http.MethodGet, "/api/v1/sessions", paired.Token); got.Code != http.StatusUnauthorized {
		t.Fatalf("old token after refresh = %d, want 401", got.Code)
	}
	if got := withToken(t, handler, http.MethodGet, "/api/v1/sessions", refreshed.Token); got.Code != http.StatusOK {
		t.Fatalf("new token after refresh = %d, want 200", got.Code)
	}
}

func TestRevokeDeviceEndsItsAccess(t *testing.T) {
	handler, service := newAuthRouter(t)
	if err := service.SetAdminPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}
	admin := decodePair(t, pair(t, handler, `{"deviceName":"admin shell","password":"correct horse battery staple"}`))
	invite, err := service.NewInvite()
	if err != nil {
		t.Fatalf("NewInvite: %v", err)
	}
	phone := decodePair(t, pair(t, handler, `{"deviceName":"phone","code":"`+invite.Code+`"}`))

	recorder := withToken(t, handler, http.MethodDelete, "/api/v1/auth/devices/"+phone.DeviceId, admin.Token)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d, want 204 (body %s)", recorder.Code, recorder.Body.String())
	}
	if got := withToken(t, handler, http.MethodGet, "/api/v1/sessions", phone.Token); got.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token = %d, want 401", got.Code)
	}
	unknown := withToken(t, handler, http.MethodDelete, "/api/v1/auth/devices/d_ffffffffffffffff", admin.Token)
	requireJSON(t, unknown)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("revoking an unknown device = %d, want 404", unknown.Code)
	}
}

func TestLoopbackWithoutATokenIsOperatorUntilTheServerIsConfigured(t *testing.T) {
	handler, service := newAuthRouter(t)

	// Nothing paired and no password: the bootstrap state, where the local operator can
	// create the first session.
	create := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"cwd":"/tmp/x"}`))
	created := do(t, handler, create, "127.0.0.1:55001")
	requireJSON(t, created)
	if created.Code != http.StatusCreated {
		t.Fatalf("bootstrap POST /sessions = %d, want 201 (body %s)", created.Code, created.Body.String())
	}

	// As soon as an identity exists, a local peer without a token is only a viewer.
	if err := service.SetAdminPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}
	write := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"cwd":"/tmp/y"}`))
	forbidden := do(t, handler, write, "127.0.0.1:55001")
	requireJSON(t, forbidden)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("configured loopback write = %d, want 403", forbidden.Code)
	}
	read := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	if got := do(t, handler, read, "127.0.0.1:55001"); got.Code != http.StatusOK {
		t.Fatalf("configured loopback read = %d, want 200", got.Code)
	}
}

func TestLoopbackWithoutATokenIsAViewer(t *testing.T) {
	handler, service := newAuthRouter(t)
	if err := service.SetAdminPassword("correct horse battery staple"); err != nil {
		t.Fatalf("SetAdminPassword: %v", err)
	}

	read := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	if got := do(t, handler, read, "127.0.0.1:55001"); got.Code != http.StatusOK {
		t.Fatalf("loopback read = %d, want 200", got.Code)
	}
	write := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"cwd":"/tmp/x"}`))
	forbidden := do(t, handler, write, "127.0.0.1:55001")
	requireJSON(t, forbidden)
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("loopback write = %d, want 403", forbidden.Code)
	}

	remote := httptest.NewRequest(http.MethodGet, "/api/v1/sessions", nil)
	unauthorized := do(t, handler, remote, "198.51.100.7:4321")
	requireJSON(t, unauthorized)
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("remote without a token = %d, want 401", unauthorized.Code)
	}
}

func TestAuthEndpointsWithoutAServiceAnswerUnsupported(t *testing.T) {
	handler := newTestRouter(t, &fakeSupervisor{}, "")
	recorder := pair(t, handler, `{"deviceName":"phone","code":"123456"}`)
	requireJSON(t, recorder)
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (body %s)", recorder.Code, recorder.Body.String())
	}
	if code, _ := decodeError(t, recorder); code != "unsupported" {
		t.Fatalf("code = %q, want unsupported", code)
	}
}
