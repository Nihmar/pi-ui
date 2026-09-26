package api

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
	"github.com/Nihmar/pi-ui/server/internal/protocol/gen"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// AuditService is the trail seam of the REST surface: record what happens, and let an
// admin read it back. internal/audit implements it over the state database.
type AuditService interface {
	audit.Recorder
	// Query returns one page of the trail, newest first, and whether more matched.
	Query(filter audit.Filter) ([]audit.Event, bool, error)
}

// record stores one audited action. A server without an audit service (or with a nil
// one) simply does not keep a trail: auditing is a capability, not a precondition of
// the REST surface.
func (a *api) record(ev audit.Event) {
	if a.audit == nil {
		return
	}
	a.audit.Record(ev)
}

// auditEvent builds an event from an authenticated request: who (device, name, scope)
// and where (peer address) come from the request itself, never from the caller.
func (a *api) auditEvent(r *http.Request, action audit.Action, outcome audit.Outcome) audit.Event {
	ev := audit.Event{Action: action, Outcome: outcome, RemoteAddr: clientIP(r)}
	if scope := scopeFrom(r.Context()); scope != "" {
		ev.ActorScope = string(scope)
	}
	if deviceID := deviceFrom(r.Context()); deviceID != "" {
		ev.ActorDeviceID = deviceID
		ev.ActorName = a.deviceName(deviceID)
	}
	return ev
}

// deviceName looks the actor's name up in the device list, so the trail keeps the name
// used at the time even after a rename. Best effort: a missing name stays empty.
func (a *api) deviceName(deviceID string) string {
	if a.authService == nil {
		return ""
	}
	for _, device := range a.authService.Devices() {
		if device.ID == deviceID {
			return device.Name
		}
	}
	return ""
}

// listAudit returns a page of the trail to an admin (docs/api-v1.md, "Audit").
func (a *api) listAudit(w http.ResponseWriter, r *http.Request) {
	if a.audit == nil {
		writeError(w, http.StatusNotImplemented, sessions.CodeUnsupported,
			"the audit trail is not configured on this server")
		return
	}
	filter, err := parseAuditFilter(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, sessions.CodeBadRequest, err.Error())
		return
	}
	entries, truncated, err := a.audit.Query(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, sessions.CodeInternal,
			"the audit trail could not be read")
		return
	}
	writeJSON(w, http.StatusOK, auditResponse(entries, truncated))
}

// parseAuditFilter reads the query string: since (RFC3339, exclusive), action,
// deviceId, sessionId and limit. An unknown parameter is ignored, so a newer client can
// send one without breaking an older server.
func parseAuditFilter(r *http.Request) (audit.Filter, error) {
	var filter audit.Filter
	query := r.URL.Query()
	if raw := query.Get("since"); raw != "" {
		at, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return filter, fmt.Errorf("since must be an RFC3339 timestamp")
		}
		filter.Since = at
	}
	filter.Action = audit.Action(query.Get("action"))
	filter.DeviceID = query.Get("deviceId")
	filter.SessionID = query.Get("sessionId")
	if raw := query.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			return filter, fmt.Errorf("limit must be a positive integer")
		}
		filter.Limit = limit
	}
	return filter, nil
}

// auditResponse renders one page. The truncation flag tells a viewer there is more,
// which is what stops it from believing the page is the whole trail.
func auditResponse(entries []audit.Event, truncated bool) gen.SrvAuditResponse {
	out := gen.SrvAuditResponse{
		Entries:   make([]gen.SrvAuditEntry, 0, len(entries)),
		Truncated: boolPointer(truncated),
	}
	for _, ev := range entries {
		entry := gen.SrvAuditEntry{
			Id:      int(ev.ID),
			At:      gen.SrvTimestamp(wireTime(ev.At)),
			Action:  string(ev.Action),
			Outcome: gen.SrvAuditOutcome(ev.Outcome),
		}
		if ev.ActorDeviceID != "" {
			entry.ActorDeviceId = stringPointer(ev.ActorDeviceID)
		}
		if ev.ActorName != "" {
			entry.ActorName = stringPointer(ev.ActorName)
		}
		if ev.ActorScope != "" {
			scope := gen.SrvScope(ev.ActorScope)
			entry.ActorScope = &scope
		}
		if ev.SessionID != "" {
			entry.SessionId = stringPointer(ev.SessionID)
		}
		if ev.Target != "" {
			entry.Target = stringPointer(ev.Target)
		}
		if ev.RemoteAddr != "" {
			entry.RemoteAddr = stringPointer(ev.RemoteAddr)
		}
		if len(ev.Details) > 0 {
			entry.Details = gen.SrvAuditEntryDetails(ev.Details)
		}
		out.Entries = append(out.Entries, entry)
	}
	return out
}
