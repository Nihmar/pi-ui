package api

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// rateKey is the bucket key of a request: the device when the request carries one (a
// token belongs to a device), the peer host otherwise. It never contains a raw token.
func rateKey(deviceID string, r *http.Request) string {
	if deviceID != "" {
		return "device:" + deviceID
	}
	return "ip:" + clientIP(r)
}

// writeRateLimited answers a request whose budget is spent: 429, the wait in
// Retry-After, the coded error body, and one rate.limited entry in the trail (a policy
// decision, which is what a rate limit is).
func (a *api) writeRateLimited(w http.ResponseWriter, r *http.Request, scope Scope, deviceID string, retry time.Duration, what string) {
	seconds := int(math.Ceil(retry.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))

	ev := audit.Event{
		Action:        audit.ActionRateLimited,
		Outcome:       audit.OutcomeDenied,
		ActorDeviceID: deviceID,
		ActorScope:    string(scope),
		RemoteAddr:    clientIP(r),
		Details: map[string]any{
			"method":        r.Method,
			"path":          r.URL.Path,
			"what":          what,
			"retryAfterSec": seconds,
		},
	}
	if deviceID != "" {
		ev.ActorName = a.deviceName(deviceID)
	}
	a.record(ev)

	writeError(w, http.StatusTooManyRequests, sessions.CodeRateLimited,
		fmt.Sprintf("too many requests; retry in %ds", seconds))
}
