package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// drainState is the server's "finish what you are doing, take nothing new" switch.
//
// It is a small piece of state of its own rather than a flag on the supervisor: the
// supervisor owns children, and a drain is about the front door — no new session is
// spawned, the running ones are untouched, and an operator can see the state in
// GET /server before wondering why a create came back 503.
type drainState struct {
	mu       sync.Mutex
	draining bool
	since    time.Time
	actor    string
}

// draining reports whether the server takes new sessions.
func (d *drainState) drainingNow() (bool, string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.draining {
		return false, ""
	}
	return true, "the server is draining since " + d.since.UTC().Format(time.RFC3339) +
		"; resume it with POST /api/v1/drain/resume"
}

// set turns the state on or off and remembers who asked.
func (d *drainState) set(draining bool, actor string) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.draining = draining
	d.actor = actor
	if draining {
		d.since = time.Now()
		return
	}
	d.since = time.Time{}
}

// startDrain answers POST /api/v1/drain/start.
func (a *api) startDrain(w http.ResponseWriter, r *http.Request) {
	a.drain.set(true, a.actorOf(r))
	a.recordDrain(r, audit.ActionDrainStart)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"draining": true,
		"running":  len(a.supervisor.List()),
	})
}

// resumeDrain answers POST /api/v1/drain/resume.
func (a *api) resumeDrain(w http.ResponseWriter, r *http.Request) {
	a.drain.set(false, a.actorOf(r))
	a.recordDrain(r, audit.ActionDrainResume)
	writeJSON(w, http.StatusAccepted, map[string]any{"draining": false})
}

// recordDrain writes the trail entry and a log line: a drain is an operational
// decision somebody will ask about later.
func (a *api) recordDrain(r *http.Request, action audit.Action) {
	event := a.auditEvent(r, action, audit.OutcomeOK)
	event.Details = map[string]any{"sessions": len(a.supervisor.List())}
	a.record(event)
}

// drainRefusal is the refusal a create gets while the server is draining.
func (a *api) drainRefusal() error {
	draining, message := a.drain.drainingNow()
	if !draining {
		return nil
	}
	return sessions.Codedf(sessions.CodeUnavailable, "%s", message)
}
