package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// Peer addresses the tests use: the authenticator branches on loopback, never on a header.
const (
	loopbackPeer = "127.0.0.1:40000"
	publicPeer   = "203.0.113.9:40000"
)

func TestHealthIsUnauthenticatedAndMinimal(t *testing.T) {
	router := newTestRouter(t, &fakeSupervisor{}, "shared-secret")

	recorder := do(t, router, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil), publicPeer)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (health must not need a credential)", recorder.Code)
	}
	requireJSON(t, recorder)
	if body := strings.TrimSpace(recorder.Body.String()); body != `{"status":"ok"}` {
		t.Errorf("body = %s, want {\"status\":\"ok\"} and nothing else", body)
	}
}

func TestServerInfoReportsTheProtocol(t *testing.T) {
	router := newTestRouter(t, &fakeSupervisor{}, "")

	recorder := do(t, router, httptest.NewRequest(http.MethodGet, "/api/v1/server", nil), loopbackPeer)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}
	requireJSON(t, recorder)

	var info ServerInfo
	if err := json.Unmarshal(recorder.Body.Bytes(), &info); err != nil {
		t.Fatalf("body %q: %v", recorder.Body.String(), err)
	}
	if info.Protocol != Protocol {
		t.Errorf("protocol = %d, want %d", info.Protocol, Protocol)
	}
	if info.Version != "0.0.1-test" {
		t.Errorf("version = %q, want the wired version", info.Version)
	}
	if len(info.Features) != 2 || info.Limits["maxSessions"] != float64(8) {
		t.Errorf("features/limits = %v/%v, want the wired values", info.Features, info.Limits)
	}
}

func TestAuthenticationAndScopeGating(t *testing.T) {
	router := newTestRouter(t, &fakeSupervisor{}, "shared-secret")

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		remoteAddr string
		auth       string
		wantStatus int
		wantCode   string
	}{
		{
			name: "loopback reads", method: http.MethodGet, path: "/api/v1/sessions",
			remoteAddr: loopbackPeer, wantStatus: http.StatusOK,
		},
		{
			name: "loopback writes need the token", method: http.MethodPost, path: "/api/v1/sessions",
			body: `{"cwd":"/tmp"}`, remoteAddr: loopbackPeer,
			wantStatus: http.StatusForbidden, wantCode: sessions.CodeForbiddenScope,
		},
		{
			name: "the token grants operator writes", method: http.MethodPost, path: "/api/v1/sessions",
			body: `{"cwd":"/tmp"}`, remoteAddr: loopbackPeer, auth: "Bearer shared-secret",
			wantStatus: http.StatusCreated,
		},
		{
			name: "a wrong token is unauthorized", method: http.MethodPost, path: "/api/v1/sessions",
			body: `{"cwd":"/tmp"}`, remoteAddr: loopbackPeer, auth: "Bearer nope",
			wantStatus: http.StatusUnauthorized, wantCode: sessions.CodeUnauthorized,
		},
		{
			name: "a remote peer without a token is unauthorized", method: http.MethodGet, path: "/api/v1/sessions",
			remoteAddr: publicPeer, wantStatus: http.StatusUnauthorized, wantCode: sessions.CodeUnauthorized,
		},
		{
			name: "the token works from a remote peer", method: http.MethodGet, path: "/api/v1/sessions",
			remoteAddr: publicPeer, auth: "Bearer shared-secret", wantStatus: http.StatusOK,
		},
		{
			name:   "a non-bearer Authorization header is not a credential",
			method: http.MethodGet, path: "/api/v1/sessions", remoteAddr: publicPeer, auth: "Basic c2VjcmV0",
			wantStatus: http.StatusUnauthorized, wantCode: sessions.CodeUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, strings.NewReader(tt.body))
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			recorder := do(t, router, req, tt.remoteAddr)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if tt.wantCode == "" {
				return
			}
			code, message := decodeError(t, recorder)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
			if message == "" {
				t.Errorf("the error envelope carries no message")
			}
		})
	}
}

func TestLoopbackWithoutATokenIsOperator(t *testing.T) {
	router := newTestRouter(t, &fakeSupervisor{}, "")

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"cwd":"/tmp"}`))
	recorder := do(t, router, req, loopbackPeer)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: the default local server has no token", recorder.Code)
	}
}

func TestCreateSessionValidation(t *testing.T) {
	router := newTestRouter(t, &fakeSupervisor{}, "")

	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantCode   string
	}{
		{name: "empty body", body: "", wantStatus: http.StatusBadRequest, wantCode: sessions.CodeBadRequest},
		{name: "broken JSON", body: `{"cwd":`, wantStatus: http.StatusBadRequest, wantCode: sessions.CodeBadRequest},
		{name: "missing cwd", body: `{"name":"alpha"}`, wantStatus: http.StatusBadRequest, wantCode: sessions.CodeBadRequest},
		{name: "blank cwd", body: `{"cwd":"   "}`, wantStatus: http.StatusBadRequest, wantCode: sessions.CodeBadRequest},
		{name: "unknown members are tolerated", body: `{"cwd":"/tmp","extra":1}`, wantStatus: http.StatusCreated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(tt.body))
			recorder := do(t, router, req, loopbackPeer)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if tt.wantCode == "" {
				return
			}
			if code, _ := decodeError(t, recorder); code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
		})
	}
}

func TestCreateSessionMapsSupervisorFailures(t *testing.T) {
	tests := []struct {
		name       string
		startErr   error
		wantStatus int
		wantCode   string
	}{
		{
			name:     "limit",
			startErr: fmt.Errorf("%w: 8 sessions are already live", sessions.ErrLimit),
			// A full server is a conflict, not a bad request: the client can retry later.
			wantStatus: http.StatusConflict, wantCode: sessions.CodeSessionLimit,
		},
		{
			name:       "unusable cwd",
			startErr:   fmt.Errorf("%w: cwd /nope", sessions.ErrInvalidSpec),
			wantStatus: http.StatusBadRequest, wantCode: sessions.CodeBadRequest,
		},
		{
			name:       "child never became ready",
			startErr:   fmt.Errorf("%w: get_state", sessions.ErrStart),
			wantStatus: http.StatusBadGateway, wantCode: sessions.CodePiError,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(t, &fakeSupervisor{startErr: tt.startErr}, "")

			req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions", strings.NewReader(`{"cwd":"/tmp"}`))
			recorder := do(t, router, req, loopbackPeer)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if code, _ := decodeError(t, recorder); code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
		})
	}
}
