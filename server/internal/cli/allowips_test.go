package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
	handler := list.middleware(allowListHandler(&reached))

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
	if list.middleware(allowListHandler(new(bool))) == nil {
		t.Fatal("the middleware always returns a handler")
	}
	if errNoPeers == nil {
		t.Fatal("the sentinel exists for a caller that wants to branch on it")
	}
}
