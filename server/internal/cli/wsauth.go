package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/Nihmar/pi-ui/server/internal/api"
	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/ratelimit"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// restToWS adapts the REST authenticator to the WebSocket Authorizer seam, so both
// transports make the same decision about the same request: one credential, one scope,
// one loopback rule.
//
// The adapter exists because the two packages cannot import each other (api imports ws
// for the hub seam), and because the device-aware decision lives on the REST
// authenticator: the optional AuthenticateDevice method is used when present.
type restToWS struct {
	auth api.Authenticator
}

// Authorize implements ws.Authorizer.
func (a restToWS) Authorize(r *http.Request) (ws.Scope, string, error) {
	type deviceAuthenticator interface {
		AuthenticateDevice(r *http.Request) (api.Scope, string, error)
	}
	if extended, ok := a.auth.(deviceAuthenticator); ok {
		scope, deviceID, err := extended.AuthenticateDevice(r)
		if err != nil {
			return "", "", err
		}
		return ws.Scope(scope), deviceID, nil
	}
	scope, err := a.auth.Authenticate(r)
	if err != nil {
		return "", "", err
	}
	return ws.Scope(scope), "", nil
}

// connectLimitedHub applies the WebSocket connect budget of PLAN.md §4.6 before the
// hub sees the request: a client that opens and drops sockets in a loop is answered
// 429 with Retry-After instead of being allowed to occupy handshake slots.
type connectLimitedHub struct {
	ws.Hub
	limit *ratelimit.Limiter
	audit audit.Recorder
}

// ServeHTTP implements http.Handler.
func (h connectLimitedHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if ok, retry := h.limit.Allow(connectKey(r)); !ok {
		seconds := int(math.Ceil(retry.Seconds()))
		if seconds < 1 {
			seconds = 1
		}
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"code":"rate_limited","message":"too many connection attempts; retry later"}}` + "\n"))
		if h.audit != nil {
			h.audit.Record(audit.Event{
				Action:     audit.ActionRateLimited,
				Outcome:    audit.OutcomeDenied,
				RemoteAddr: peerHostOf(r.RemoteAddr),
				Details:    map[string]any{"what": "ws-connect", "retryAfterSec": seconds},
			})
		}
		return
	}
	h.Hub.ServeHTTP(w, r)
}

// connectKey is the bucket key of a handshake: a hash of the bearer credential when
// there is one (never the token itself, so a heap dump or a map key never leaks it),
// the peer host otherwise.
func connectKey(r *http.Request) string {
	if header := r.Header.Get("Authorization"); header != "" {
		sum := sha256.Sum256([]byte(header))
		return "token:" + hex.EncodeToString(sum[:8])
	}
	return "ip:" + peerHostOf(r.RemoteAddr)
}

// peerHostOf strips the port from a peer address.
func peerHostOf(remoteAddr string) string {
	if host, _, found := strings.Cut(remoteAddr, ":"); found {
		return host
	}
	return remoteAddr
}
