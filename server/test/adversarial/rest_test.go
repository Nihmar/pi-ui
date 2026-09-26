package adversarial_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/api"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// TestREST_HealthNeedsNoCredential and TestREST_AuthScopes cover the credential
// boundary of the REST surface: health is public, a loopback peer of a
// token-protected server is a viewer (reads yes, writes no), and a non-loopback
// peer without a valid token is unauthorized.
func TestREST_HealthNeedsNoCredential(t *testing.T) {
	stack := newStack(t, func(o *stackOptions) { o.token = "s3cret" })

	resp, data := stack.requestNoAuth(http.MethodGet, "/api/v1/health", "")
	wantStatus(t, resp, data, http.StatusOK)
	if got := resp.Header.Get("X-Piui-Protocol"); got != "1" {
		t.Fatalf("X-Piui-Protocol = %q, want 1", got)
	}
	var body struct {
		Status string `json:"status"`
	}
	decodeJSON(t, data, &body)
	if body.Status != "ok" {
		t.Fatalf("health = %+v, want ok", body)
	}
}

func TestREST_AuthScopes(t *testing.T) {
	stack := newStack(t, func(o *stackOptions) { o.token = "s3cret" })

	t.Run("loopback without token is a viewer: read ok, write forbidden", func(t *testing.T) {
		resp, data := stack.requestNoAuth(http.MethodGet, "/api/v1/sessions", "")
		wantStatus(t, resp, data, http.StatusOK)

		resp, data = stack.requestNoAuth(http.MethodPost, "/api/v1/sessions", `{"cwd":"/tmp"}`)
		wantStatus(t, resp, data, http.StatusForbidden)
		code, _ := apiError(t, data)
		if code != "forbidden_scope" {
			t.Fatalf("write from a viewer = %q, want forbidden_scope", code)
		}
	})

	t.Run("non-loopback without a valid token is unauthorized", func(t *testing.T) {
		router := api.NewRouter(api.Options{Auth: api.NewLoopbackOrToken("s3cret")})
		for _, target := range []string{"/api/v1/sessions", "/api/v1/sessions?token=s3cret"} {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			req.RemoteAddr = "198.51.100.7:4321"
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, req)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("GET %s from a non-loopback peer = %d, want 401 (body %s)", target, recorder.Code, recorder.Body.String())
			}
			code, _ := apiError(t, recorder.Body.Bytes())
			if code != "unauthorized" {
				t.Fatalf("code = %q, want unauthorized", code)
			}
		}
	})
}

// TestREST_CreateRejectsBadBodies covers the request-shape failures: a body that
// is not JSON, a JSON body without cwd, and a valid but over-limit body.
func TestREST_CreateRejectsBadBodies(t *testing.T) {
	stack := newStack(t, nil)
	cwd := t.TempDir()

	tests := []struct {
		name string
		body string
	}{
		{name: "not JSON", body: `{oops`},
		{name: "missing cwd", body: `{"name":"no-cwd"}`},
		{name: "blank cwd", body: `{"cwd":"   "}`},
		{name: "scalar JSON", body: `"hello"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, data := stack.postJSON("/api/v1/sessions", tt.body)
			wantStatus(t, resp, data, http.StatusBadRequest)
			code, _ := apiError(t, data)
			if code != "bad_request" {
				t.Fatalf("code = %q, want bad_request", code)
			}
		})
	}

	t.Run("oversized body", func(t *testing.T) {
		// The API caps a body at 1 MiB (maxBodyBytes). A bigger one must be
		// rejected, not buffered: the size failure is too_large / 413, so the
		// client knows to retry with less data instead of fixing its JSON.
		body := `{"cwd":"` + cwd + `","name":"` + strings.Repeat("x", 2<<20) + `"}`
		resp, data := stack.postJSON("/api/v1/sessions", body)
		wantStatus(t, resp, data, http.StatusRequestEntityTooLarge)
		if code, _ := apiError(t, data); code != "too_large" {
			t.Fatalf("oversized body code = %q, want too_large", code)
		}
	})
}

// TestREST_SessionLimitAndRelease drives MaxSessions to its edge: the second
// create is 409, stopping the first releases the slot while the exited session
// stays listed.
func TestREST_SessionLimitAndRelease(t *testing.T) {
	stack := newStack(t, func(o *stackOptions) { o.maxSessions = 1 })

	first, resp, data := stack.createSession(t.TempDir(), "first")
	wantStatus(t, resp, data, http.StatusCreated)

	_, resp, data = stack.createSession(t.TempDir(), "second")
	wantStatus(t, resp, data, http.StatusConflict)
	if code, _ := apiError(t, data); code != "session_limit" {
		t.Fatalf("limit code = %q, want session_limit", code)
	}

	// Stop the first session and wait for its slot to be released; it must stay
	// listed with a terminal status.
	resp, data = stack.postJSON("/api/v1/sessions/"+first.ID+"/stop", "")
	wantStatus(t, resp, data, http.StatusAccepted)

	deadline := time.Now().Add(waitTimeout)
	for {
		info, ok := stack.mgr.Get(first.ID)
		if ok && !info.Status.Live() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session %s did not reach a terminal status (last: %+v)", first.ID, info)
		}
		time.Sleep(5 * time.Millisecond)
	}

	third, resp, data := stack.createSession(t.TempDir(), "third")
	wantStatus(t, resp, data, http.StatusCreated)
	if third.Status != sessions.StatusReady {
		t.Fatalf("third session status = %q, want ready", third.Status)
	}

	resp, data = stack.getJSON("/api/v1/sessions")
	wantStatus(t, resp, data, http.StatusOK)
	var listed struct {
		Sessions []sessions.Info `json:"sessions"`
	}
	decodeJSON(t, data, &listed)
	if len(listed.Sessions) != 2 {
		t.Fatalf("sessions = %d, want the exited and the new one", len(listed.Sessions))
	}
}

// TestREST_UnknownSessionAndMethodAbuse pins the error envelope on the paths a
// client hits after a typo: unknown ids, wrong methods on known paths, and
// unknown paths must all answer JSON with the shared code taxonomy.
func TestREST_UnknownSessionAndMethodAbuse(t *testing.T) {
	stack := newStack(t, nil)
	unknown := "s_0000000000000000"

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantCode   string
	}{
		{name: "get unknown session", method: http.MethodGet, path: "/api/v1/sessions/" + unknown, wantStatus: http.StatusNotFound, wantCode: "session_not_found"},
		{name: "stop unknown session", method: http.MethodPost, path: "/api/v1/sessions/" + unknown + "/stop", wantStatus: http.StatusNotFound, wantCode: "session_not_found"},
		{name: "put on sessions", method: http.MethodPut, path: "/api/v1/sessions", wantStatus: http.StatusMethodNotAllowed, wantCode: "bad_request"},
		{name: "delete on health", method: http.MethodDelete, path: "/api/v1/health", wantStatus: http.StatusMethodNotAllowed, wantCode: "bad_request"},
		{name: "post on server", method: http.MethodPost, path: "/api/v1/server", wantStatus: http.StatusMethodNotAllowed, wantCode: "bad_request"},
		{name: "unknown path", method: http.MethodGet, path: "/api/v1/nope", wantStatus: http.StatusNotFound, wantCode: "not_found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, data := stack.request(tt.method, tt.path, "", nil)
			wantStatus(t, resp, data, tt.wantStatus)
			if got := resp.Header.Get("X-Piui-Protocol"); got != "1" {
				t.Errorf("X-Piui-Protocol = %q, want 1", got)
			}
			if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
				t.Errorf("Content-Type = %q, want JSON", ct)
			}
			code, _ := apiError(t, data)
			if code != tt.wantCode {
				t.Errorf("code = %q, want %q", code, tt.wantCode)
			}
		})
	}
}

// TestREST_ChildThatNeverBecomesReadyIsPiError covers the failed-readiness path:
// a child that ignores stdin never answers get_state, the create answers 502
// pi_error, and the child is stopped instead of leaking.
func TestREST_ChildThatNeverBecomesReadyIsPiError(t *testing.T) {
	stack := newStack(t, func(o *stackOptions) { o.piArgs = []string{"--ignore-stdin"} })

	resp, data := stack.postJSON("/api/v1/sessions", `{"cwd":"`+t.TempDir()+`"}`)
	wantStatus(t, resp, data, http.StatusBadGateway)
	if code, _ := apiError(t, data); code != "pi_error" {
		t.Fatalf("code = %q, want pi_error", code)
	}

	// The session stays listed with a terminal status and no live child.
	deadline := time.Now().Add(waitTimeout)
	for time.Now().Before(deadline) {
		list := stack.mgr.List()
		if len(list) != 1 {
			t.Fatalf("List() = %+v, want the failed session to stay listed", list)
		}
		info := list[0]
		if !info.Status.Live() {
			if processAlive(info.PID) {
				t.Fatalf("child %d of the failed session is still alive", info.PID)
			}
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("failed session never reached a terminal status: %+v", stack.mgr.List())
}
