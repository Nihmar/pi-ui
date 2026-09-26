package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/rpc"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// Extension UI methods (§8): the four dialog methods become a WS `request` frame, the five
// fire-and-forget methods become ext.* events, anything else is forwarded verbatim.
const (
	methodSelect        = "select"
	methodConfirm       = "confirm"
	methodInput         = "input"
	methodEditor        = "editor"
	methodNotify        = "notify"
	methodSetStatus     = "setStatus"
	methodSetWidget     = "setWidget"
	methodSetTitle      = "setTitle"
	methodSetEditorText = "set_editor_text"

	// frameTypeRequest is the WS frame type the hub writes for an extension dialog (§6).
	frameTypeRequest = "request"
	// recordExtensionUIResponse is what the server writes back to the child.
	recordExtensionUIResponse = "extension_ui_response"
)

// errNoRequestID reports a dialog the hub could never correlate: the schema requires the id.
var errNoRequestID = errors.New("extension dialog has no id")

// uiRequest is the lenient view of one extension_ui_request record: unknown methods and
// unknown fields stay in the raw bytes and are never dropped.
type uiRequest struct {
	ID     string `json:"id"`
	Method string `json:"method"`
}

// dialog is one pending extension dialog: the request frame sent to the session's
// subscribers and the first-answer-wins state of §8.
type dialog struct {
	requestID string
	method    string
	answered  bool
	closedAt  time.Time
	timer     *time.Timer
}

// handleExtensionUIRequest terminates the extension UI subprotocol (§5.3, §8). Fire-and-
// forget methods are published as ext.* events; the dialog methods are routed as `request`
// frames; anything unknown is forwarded verbatim so a newer bridge cannot be silenced.
func (s *session) handleExtensionUIRequest(raw json.RawMessage) {
	var request uiRequest
	if err := json.Unmarshal(raw, &request); err != nil {
		s.logf("extension_ui_request is not readable: %v", err)
		s.mgr.publish(ws.Event{Type: EventPiUnknown, SessionID: s.id, Payload: recordPayload(rpc.Record{Raw: raw})})
		return
	}
	if request.ID == "" {
		// A dialog is resolved by its id; without one nothing could ever answer it, so it
		// is forwarded verbatim instead of vanishing into a pending map.
		s.logf("extension_ui_request without id (method %s): forwarded as an event", request.Method)
		s.mgr.publish(ws.Event{Type: piEventType(recordExtensionUIRequest), SessionID: s.id, Payload: recordPayload(rpc.Record{Raw: raw})})
		return
	}

	switch request.Method {
	case methodSelect, methodConfirm, methodInput, methodEditor:
		s.openDialog(request, raw)
	case methodNotify, methodSetStatus, methodSetWidget, methodSetTitle, methodSetEditorText:
		s.mgr.publish(ws.Event{Type: extEventType(request.Method), SessionID: s.id, Payload: recordPayload(rpc.Record{Raw: raw})})
	default:
		s.mgr.publish(ws.Event{Type: piEventType(recordExtensionUIRequest), SessionID: s.id, Payload: recordPayload(rpc.Record{Raw: raw})})
	}
}

// extEventType maps one fire-and-forget method onto its ext.* event (§7).
func extEventType(method string) string {
	switch method {
	case methodNotify:
		return EventExtNotify
	case methodSetStatus:
		return EventExtStatus
	case methodSetWidget:
		return EventExtWidget
	case methodSetTitle:
		return EventExtTitle
	case methodSetEditorText:
		return EventExtEditorText
	default:
		return EventPiUnknown
	}
}

// openDialog routes one dialog to the session's subscribers and arms the timeout. The
// request frame is the child's own record with `type` replaced, plus sessionId, timeoutMs
// and ts, so no field of the extension request is lost on the way to the client.
func (s *session) openDialog(request uiRequest, raw json.RawMessage) {
	frame, err := requestFrame(s.id, raw, s.mgr.cfg.DialogTimeout)
	if err != nil {
		s.logf("extension_ui_request %s is not routable: %v", request.ID, err)
		s.mgr.publish(ws.Event{Type: piEventType(recordExtensionUIRequest), SessionID: s.id, Payload: raw})
		return
	}

	pending := &dialog{requestID: request.ID, method: request.Method}
	s.mu.Lock()
	s.pruneDialogsLocked(time.Now())
	s.dialogs[request.ID] = pending
	// The timer is created while the lock is held: a client answer that races the request
	// must never read the field while it is written. The callback only blocks on s.mu if it
	// fires immediately, and by then the dialog is already in the map.
	pending.timer = time.AfterFunc(s.mgr.cfg.DialogTimeout, func() {
		s.dialogTimeout(request.ID, request.Method)
	})
	s.mu.Unlock()

	s.mgr.sendDialog(s.id, frame)
}

// requestFrame builds the WS `request` frame of §6 out of one extension_ui_request: the
// child's own fields (id, method, title, message, options, placeholder, prefill) plus the
// frame type, sessionId, timeoutMs and ts the hub validates against schemas/ws.json. A
// missing title becomes an empty string because the schema requires the field.
func requestFrame(sessionID string, raw json.RawMessage, timeout time.Duration) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	delete(fields, "type")
	setField(fields, "type", frameTypeRequest)
	setField(fields, "sessionId", sessionID)
	setField(fields, "timeoutMs", int(timeout/time.Millisecond))
	setField(fields, "ts", now())
	if _, ok := fields["title"]; !ok {
		setField(fields, "title", "")
	}
	if _, ok := fields["id"]; !ok {
		return nil, errNoRequestID
	}
	return json.Marshal(fields)
}

// answer resolves one dialog with the client's response (§8). The first answer wins: an id
// that was answered or timed out is already_answered, an id that never existed is
// not_found.
func (s *session) answer(ctx context.Context, requestID string, response json.RawMessage) error {
	record, err := extensionUIResponse(requestID, response)
	if err != nil {
		return err
	}

	s.mu.Lock()
	pending, ok := s.dialogs[requestID]
	switch {
	case !ok:
		s.mu.Unlock()
		return Codedf(CodeNotFound, "no extension dialog with id %q", requestID)
	case pending.answered:
		s.mu.Unlock()
		return Codedf(CodeAlreadyAnswered, "dialog %q was already answered", requestID)
	}
	pending.answered = true
	pending.closedAt = time.Now()
	timer, bridge := pending.timer, s.bridge
	s.mu.Unlock()

	if timer != nil {
		timer.Stop()
	}
	if bridge == nil {
		return Codedf(CodePiError, "session %s has no child", s.id)
	}
	if err := bridge.Write(ctx, record); err != nil {
		return Codedf(CodePiError, "extension_ui_response: %v", err)
	}
	return nil
}

// dialogTimeout answers a dialog nobody answered: cancelled:true to the child and
// server.dialog.timeout to the subscribers (§8).
func (s *session) dialogTimeout(requestID, method string) {
	s.mu.Lock()
	pending, ok := s.dialogs[requestID]
	if !ok || pending.answered || s.finished {
		s.mu.Unlock()
		return
	}
	pending.answered = true
	pending.closedAt = time.Now()
	bridge := s.bridge
	s.mu.Unlock()

	record := mustJSON(map[string]any{
		"type":      recordExtensionUIResponse,
		"id":        requestID,
		"cancelled": true,
	})
	if bridge != nil {
		ctx, cancel := context.WithTimeout(context.Background(), writeTimeout)
		if err := bridge.Write(ctx, record); err != nil {
			s.logf("dialog %s: cancelled response not delivered: %v", requestID, err)
		}
		cancel()
	}
	s.mgr.publishJSON(EventServerDialogTimeout, s.id, dialogTimeoutPayload{RequestID: requestID, Method: method})
}

// extensionUIResponse builds the child record for one answer: the request id plus whichever
// of value/confirmed/cancelled the client sent. Both shapes are accepted — the whole
// ui_response frame or only its value object — because the hub owns that unpacking.
func extensionUIResponse(requestID string, response json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if err := json.Unmarshal(response, &fields); err != nil {
		return nil, Codedf(CodeBadRequest, "ui_response is not a JSON object: %v", err)
	}
	out := map[string]json.RawMessage{}
	answered := false
	for _, key := range []string{"value", "confirmed", "cancelled"} {
		if value, ok := fields[key]; ok {
			out[key] = value
			answered = true
		}
	}
	if !answered {
		return nil, Codedf(CodeBadRequest, `ui_response carries none of "value", "confirmed", "cancelled"`)
	}
	setField(out, "type", recordExtensionUIResponse)
	setField(out, "id", requestID)
	return json.Marshal(out)
}

// cancelDialogsLocked stops the timers of a finished session: writing cancelled:true to a
// reaped child would only produce a log line. The entries stay for dialogRetention, so a
// late answer still reads as already_answered instead of not_found.
func (s *session) cancelDialogsLocked(at time.Time) {
	for _, pending := range s.dialogs {
		if pending.timer != nil {
			pending.timer.Stop()
			pending.timer = nil
		}
		if !pending.answered {
			pending.answered = true
			pending.closedAt = at
		}
	}
}

// pruneDialogsLocked drops resolved dialogs older than dialogRetention, so a long-lived
// session does not accumulate one map entry per dialog forever.
func (s *session) pruneDialogsLocked(at time.Time) {
	for id, pending := range s.dialogs {
		if pending.answered && at.Sub(pending.closedAt) > dialogRetention {
			delete(s.dialogs, id)
		}
	}
}
