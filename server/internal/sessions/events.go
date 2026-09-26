package sessions

import (
	"encoding/json"

	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// Event types the supervisor publishes (docs/spike-interfaces.md §7). The pi.* names are
// derived from the child's own record type; the server.* and ext.* names are fixed here.
// Names the hub owns (server.replay.begin/end, server.heartbeat, server.error and
// pi.entry_appended) come from package ws, so the two sides cannot drift by a typo.
//
// The payload of every event published by this package is documented at its publish
// site: lifecycle events carry the session Info, pi.* events carry the child's record
// bytes verbatim — except a line that is not valid JSON, which travels as
// {"raw":"<line>"} because the hub only frames an event payload that is valid JSON —
// and ext.* events carry the extension_ui_request record verbatim.
const (
	EventServerSpawned       = "server.spawned"
	EventServerReady         = "server.ready"
	EventServerStatus        = "server.status"
	EventServerStopping      = "server.stopping"
	EventServerExited        = "server.exited"
	EventServerCrashed       = "server.crashed"
	EventServerDialogTimeout = "server.dialog.timeout"

	EventExtNotify     = "ext.notify"
	EventExtStatus     = "ext.status"
	EventExtWidget     = "ext.widget"
	EventExtTitle      = "ext.title"
	EventExtEditorText = "ext.editor_text"

	// EventPiUnknown is what a record without a readable `type` becomes, so a malformed
	// child line is still forwarded instead of dropped. A line that is not valid JSON
	// travels as {"raw":"<line>"} (see recordPayload), because the hub only frames an
	// event payload that is valid JSON.
	EventPiUnknown = "pi.unknown"

	piEventPrefix = "pi."
)

// errorPayload is the body of server.error (§7).
type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// rawLinePayload is the body of a pi.unknown event whose record is not valid JSON: the
// child's line as a JSON string, so the hub can frame it and a client can still recover
// the bytes it received (`raw` round-trips them exactly).
type rawLinePayload struct {
	Raw string `json:"raw"`
}

// exitPayload is the body of server.exited and server.crashed: the exit status of the
// child, -1 when a signal ended it (§5.3).
type exitPayload struct {
	ExitCode int `json:"exitCode"`
}

// dialogTimeoutPayload is the body of server.dialog.timeout (§7).
type dialogTimeoutPayload struct {
	RequestID string `json:"requestId"`
	Method    string `json:"method"`
}

// publish forwards one event to the injected hub. A nil hub (tests that only exercise the
// supervisor) makes publishing a no-op instead of a panic.
func (m *Manager) publish(ev ws.Event) uint64 {
	if m.cfg.Hub == nil {
		return 0
	}
	return m.cfg.Hub.Publish(ev)
}

// publishJSON marshals a payload and publishes it. A payload that cannot be marshalled is
// a bug in this package, not a runtime condition: it is logged and the event is published
// without a payload rather than dropped.
func (m *Manager) publishJSON(evType, sessionID string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		m.logger.Error("sessions: event payload is not marshalable",
			"type", evType, "sessionId", sessionID, "error", err)
		m.publish(ws.Event{Type: evType, SessionID: sessionID})
		return
	}
	m.publish(ws.Event{Type: evType, SessionID: sessionID, Payload: data})
}

// piEventType names the event for one child record: "pi." + the record type, or
// pi.unknown when the record carries no readable type.
func piEventType(recordType string) string {
	if recordType == "" {
		return EventPiUnknown
	}
	return piEventPrefix + recordType
}
