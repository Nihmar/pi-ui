package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/ratelimit"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// stubWSHub counts the handshakes that reached it.
type stubWSHub struct{ served int }

var _ ws.Hub = (*stubWSHub)(nil)

func (s *stubWSHub) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	s.served++
	w.WriteHeader(http.StatusOK)
}
func (s *stubWSHub) Publish(ws.Event) uint64             { return 0 }
func (s *stubWSHub) SetReplayer(ws.Replayer)             {}
func (s *stubWSHub) SetCommandHandler(ws.CommandHandler) {}
func (s *stubWSHub) SetDialogHandler(ws.DialogHandler)   {}
func (s *stubWSHub) Close() error                        { return nil }

// connectRequest builds one handshake request with an optional token.
func connectRequest(remoteAddr, token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/ws/v1", nil)
	r.RemoteAddr = remoteAddr
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func TestConnectBudgetAnswers429WithRetryAfter(t *testing.T) {
	store := audit.NewMemoryStore()
	log := audit.New(store, audit.Options{})
	stub := &stubWSHub{}
	limited := connectLimitedHub{Hub: stub, limit: ratelimit.New(4), audit: log}

	first := httptest.NewRecorder()
	limited.ServeHTTP(first, connectRequest("198.51.100.7:1234", ""))
	if first.Code != http.StatusOK || stub.served != 1 {
		t.Fatalf("first handshake = %d (served %d), want 200 and 1", first.Code, stub.served)
	}

	second := httptest.NewRecorder()
	limited.ServeHTTP(second, connectRequest("198.51.100.7:1234", ""))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second handshake = %d, want 429", second.Code)
	}
	if retry := second.Header().Get("Retry-After"); retry == "" || retry == "0" {
		t.Fatalf("Retry-After = %q, want a positive number of seconds", retry)
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &body); err != nil || body.Error.Code != "rate_limited" {
		t.Fatalf("body = %s (err %v), want the coded error", second.Body.String(), err)
	}
	if stub.served != 1 {
		t.Fatalf("the refused handshake reached the hub (served %d)", stub.served)
	}

	// A different credential has its own bucket.
	third := httptest.NewRecorder()
	limited.ServeHTTP(third, connectRequest("198.51.100.7:1234", "device-token-a"))
	if third.Code != http.StatusOK {
		t.Fatalf("handshake with another token = %d, want 200", third.Code)
	}

	// The refusal is a policy decision in the trail.
	events, err := store.Query(audit.Filter{Action: audit.ActionRateLimited})
	if err != nil || len(events) != 1 {
		t.Fatalf("trail = %+v (%v), want one rate.limited entry", events, err)
	}
	if events[0].Details["what"] != "ws-connect" || events[0].RemoteAddr != "198.51.100.7" {
		t.Fatalf("event = %+v, want the connect refusal and its peer", events[0])
	}
}

func TestConnectKeyNeverCarriesTheRawToken(t *testing.T) {
	key := connectKey(connectRequest("198.51.100.7:1234", "super-secret-token"))
	if strings.Contains(key, "super-secret-token") {
		t.Fatalf("connect key %q leaks the token", key)
	}
	if !strings.HasPrefix(key, "token:") {
		t.Fatalf("connect key = %q, want a token bucket", key)
	}
	ipKey := connectKey(connectRequest("198.51.100.7:1234", ""))
	if ipKey != "ip:198.51.100.7" {
		t.Fatalf("ip key = %q, want the peer host", ipKey)
	}
	// Two connections with the same credential share one bucket; different credentials
	// do not.
	if key != connectKey(connectRequest("203.0.113.9:9999", "super-secret-token")) {
		t.Fatal("the same token produced two buckets")
	}
}

// TestDisabledConnectLimitLetsEverythingThrough keeps the 0 = off contract: the
// wrapper with a nil limiter must be transparent.
func TestDisabledConnectLimitLetsEverythingThrough(t *testing.T) {
	stub := &stubWSHub{}
	limited := connectLimitedHub{Hub: stub, limit: nil}
	for i := 0; i < 5; i++ {
		recorder := httptest.NewRecorder()
		limited.ServeHTTP(recorder, connectRequest("198.51.100.7:1234", ""))
		if recorder.Code != http.StatusOK {
			t.Fatalf("request %d = %d, want 200", i+1, recorder.Code)
		}
	}
	if stub.served != 5 {
		t.Fatalf("served = %d, want 5", stub.served)
	}
	_ = time.Second
}
