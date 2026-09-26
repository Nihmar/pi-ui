package ws

import (
	"strings"

	"github.com/Nihmar/pi-ui/server/internal/audit"
)

// commandActions maps the op vocabulary of internal/sessions onto the trail. An op
// outside this map is recorded as session.command with the raw op in the details, so a
// hostile or newer client can never put an unbounded string in the action column.
var commandActions = map[string]audit.Action{
	"session.prompt":      audit.ActionSessionPrompt,
	"session.steer":       audit.ActionSessionSteer,
	"session.follow_up":   audit.ActionSessionFollowUp,
	"session.abort":       audit.ActionSessionAbort,
	"session.stop":        audit.ActionSessionStop,
	"session.rename":      audit.ActionSessionRename,
	"session.clear_queue": audit.ActionSessionClearQueue,
}

// auditCommand records one dispatched command with its outcome.
func (h *hub) auditCommand(c *connection, cmd Command, outcome audit.Outcome) {
	if h.opts.Audit == nil {
		return
	}
	action, known := commandActions[cmd.Op]
	details := map[string]any{"op": cmd.Op}
	if !known {
		action = audit.ActionSessionCommand
	}
	h.opts.Audit.Record(audit.Event{
		Action:        action,
		Outcome:       outcome,
		ActorDeviceID: c.who.deviceID,
		ActorScope:    string(c.who.scope),
		SessionID:     cmd.SessionID,
		Target:        cmd.SessionID,
		Details:       details,
	})
}

// auditDenied records a frame refused for its scope: a policy decision, so the trail
// keeps it as auth.denied with what was attempted.
func (h *hub) auditDenied(c *connection, sessionID, op, reason string) {
	if h.opts.Audit == nil {
		return
	}
	h.opts.Audit.Record(audit.Event{
		Action:        audit.ActionAuthDenied,
		Outcome:       audit.OutcomeDenied,
		ActorDeviceID: c.who.deviceID,
		ActorScope:    string(c.who.scope),
		SessionID:     sessionID,
		Details:       map[string]any{"op": op, "reason": reason},
	})
}

// record writes one event when a recorder is configured.
func (h *hub) record(ev audit.Event) {
	if h.opts.Audit == nil {
		return
	}
	h.opts.Audit.Record(ev)
}

// peerHost strips the port from a remote address for the trail.
func peerHost(remoteAddr string) string {
	if host, _, found := strings.Cut(remoteAddr, ":"); found {
		return host
	}
	return remoteAddr
}
