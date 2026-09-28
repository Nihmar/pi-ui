package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Nihmar/pi-ui/server/internal/audit"
)

// serve records that a request reached the handler.
func allowListHandler(reached *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*reached = true
		w.WriteHeader(http.StatusOK)
	})
}

func TestAnEmptyAllowListLetsEveryoneIn(t *testing.T) {
	list, err := newIPAllowList(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !list.allows("203.0.113.7:1234") {
		t.Fatal("an empty list is not a filter")
	}
}

func TestTheAllowListTakesAddressesAndBlocks(t *testing.T) {
	list, err := newIPAllowList([]string{"127.0.0.1", "10.0.0.0/8", "::1", "  "})
	if err != nil {
		t.Fatal(err)
	}

	cases := map[string]bool{
		"127.0.0.1:5555": true,
		"10.4.5.6:80":    true,
		"[::1]:80":       true,
		"192.168.1.9:80": false,
		"11.0.0.1:80":    false,
		"not-an-address": false,
	}
	for addr, want := range cases {
		if got := list.allows(addr); got != want {
			t.Fatalf("allows(%q) = %v, want %v", addr, got, want)
		}
	}
}

func TestABadAllowListIsRefusedAtStartup(t *testing.T) {
	if _, err := newIPAllowList([]string{"pi-ui.local"}); err == nil {
		t.Fatal("a hostname is not an address: the peer address is what is compared")
	}
}

func TestTheMiddlewareAnswersForbiddenWithoutReachingTheHandler(t *testing.T) {
	list, err := newIPAllowList([]string{"127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	reached := false
	handler := list.middleware(allowListHandler(&reached), nil)

	allowed := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	allowed.RemoteAddr = "127.0.0.1:4444"
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, allowed)
	if recorder.Code != http.StatusOK || !reached {
		t.Fatalf("an allowed peer: %d (reached %v)", recorder.Code, reached)
	}

	reached = false
	denied := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	denied.RemoteAddr = "198.51.100.4:4444"
	recorder = httptest.NewRecorder()
	handler.ServeHTTP(recorder, denied)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("a denied peer: %d", recorder.Code)
	}
	if reached {
		t.Fatal("a denied peer must not reach a handler")
	}
	if body := recorder.Body.String(); body == "" {
		t.Fatal("the refusal carries the error envelope")
	}
}

func TestNoAllowListMeansNoWrapping(t *testing.T) {
	list, err := newIPAllowList(nil)
	if err != nil {
		t.Fatal(err)
	}
	if list.middleware(allowListHandler(new(bool)), nil) == nil {
		t.Fatal("the middleware always returns a handler")
	}
	if errNoPeers == nil {
		t.Fatal("the sentinel exists for a caller that wants to branch on it")
	}
}

// recordingTrail collects the audit events a test is interested in.
type recordingTrail struct {
	events []audit.Event
}

// Record implements audit.Recorder.
func (t *recordingTrail) Record(event audit.Event) { t.events = append(t.events, event) }

func TestARefusedPeerIsRecorded(t *testing.T) {
	list, err := newIPAllowList([]string{"192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	trail := &recordingTrail{}
	handler := list.middleware(allowListHandler(new(bool)), trail)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/server", nil)
	request.RemoteAddr = "198.51.100.9:4444"
	handler.ServeHTTP(httptest.NewRecorder(), request)

	if len(trail.events) != 1 {
		t.Fatalf("events = %+v", trail.events)
	}
	event := trail.events[0]
	if event.Action != audit.ActionAuthDenied || event.Outcome != audit.OutcomeDenied {
		t.Fatalf("event = %+v", event)
	}
	if event.RemoteAddr != "198.51.100.9" || event.Target != "/api/v1/server" {
		t.Fatalf("event = %+v", event)
	}
	if event.Details["reason"] != "allow_ips" {
		t.Fatalf("details = %+v", event.Details)
	}

	// An allowed peer leaves no trace: the trail is for refusals, not for traffic.
	trail.events = nil
	allowed := httptest.NewRequest(http.MethodGet, "/api/v1/server", nil)
	allowed.RemoteAddr = "192.0.2.1:4444"
	handler.ServeHTTP(httptest.NewRecorder(), allowed)
	if len(trail.events) != 0 {
		t.Fatalf("an allowed request was recorded: %+v", trail.events)
	}
}

// TestPeerHostKeepsAnIPv6Address pins what the trail records for a refused peer: an IPv6
// address is full of colons, so cutting at the first one reports an empty host exactly when
// somebody is probing from an IPv6 network — the case the entry exists for.
func TestPeerHostKeepsAnIPv6Address(t *testing.T) {
	cases := map[string]string{
		"192.0.2.7:41234":   "192.0.2.7",
		"[2001:db8::1]:443": "2001:db8::1",
		"[::1]:80":          "::1",
		"::1":               "::1",
	}
	for addr, want := range cases {
		if got := peerHost(addr); got != want {
			t.Errorf("peerHost(%q) = %q, want %q", addr, got, want)
		}
	}
}
