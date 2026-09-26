package ws

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/audit"
)

// The terminal seam (docs/ws-protocol.md, "Terminals").
//
// A terminal is connection-owned live state, not a conversation: its output is a
// stream of its own, it is never replayed and it is never shared. That is why it is
// a separate interface from CommandHandler — the hub validates and routes, the
// terminal service runs the PTY — and why the hub keeps the ownership map: routing
// output means knowing which socket opened which shell.

// Terminal is one open PTY as the client learns it from the open response.
type Terminal struct {
	// ID is the server-side terminal id (`t_…`), opaque to the client.
	ID string
	// CWD is the confined directory the shell runs in.
	CWD string
	// PID is the shell's process id, for display and for an operator's audit trail.
	PID int
	// Cols and Rows are the size the shell was started with.
	Cols uint16
	Rows uint16
}

// TerminalHandler is the inbound half: the hub validates a frame against
// schemas/ws.json and hands the decoded request here.
//
// A handler error is answered as a coded `response` frame, so a client always gets
// an answer to the id it sent.
type TerminalHandler interface {
	// TerminalOpen starts a shell in dir. The directory is the handler's to confine:
	// by the time this runs the frame is valid, and nothing else is guaranteed.
	TerminalOpen(ctx context.Context, dir string, cols, rows uint16) (Terminal, error)
	// TerminalInput writes bytes to one terminal.
	TerminalInput(ctx context.Context, terminalID string, data []byte) error
	// TerminalResize reports the client's new size.
	TerminalResize(ctx context.Context, terminalID string, cols, rows uint16) error
	// TerminalClose ends one terminal and its process group. reason is what the
	// close notification reports: "client" for a frame, "owner" when the connection
	// that opened it is gone, "server" for a shutdown.
	TerminalClose(ctx context.Context, terminalID string, reason string) error
}

// TerminalSink is the outbound half: what the terminal service writes its output
// into. The hub implements it and routes each chunk to the connection that owns the
// terminal.
type TerminalSink interface {
	// TerminalOutput delivers bytes produced by one terminal.
	TerminalOutput(terminalID string, data []byte)
	// TerminalClosed reports that a terminal is gone, exactly once, after the last
	// chunk. reason is "client", "owner", "server" or "exit".
	TerminalClosed(terminalID string, exitCode int, reason string)
}

// SetTerminalHandler installs the handler of client terminal frames.
func (h *hub) SetTerminalHandler(handler TerminalHandler) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.terminal = handler
}

// terminalHandler returns the installed terminal seam, if any.
func (h *hub) terminalHandler() TerminalHandler {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.terminal
}

// claimTerminal records that one connection owns one terminal.
func (h *hub) claimTerminal(c *connection, terminalID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.terminalOwner[terminalID] = c
}

// releaseTerminals closes every terminal one connection owned.
//
// It runs when the socket is done, before the connection is forgotten: a browser tab
// that disappears must not leave a shell behind, and the close travels through the
// handler so the terminal service decides how a process tree dies.
func (h *hub) releaseTerminals(c *connection) {
	h.mu.Lock()
	owned := make([]string, 0, 4)
	for terminalID, owner := range h.terminalOwner {
		if owner == c {
			owned = append(owned, terminalID)
			delete(h.terminalOwner, terminalID)
		}
	}
	handler := h.terminal
	h.mu.Unlock()

	if handler == nil {
		return
	}
	for _, terminalID := range owned {
		ctx, cancel := context.WithTimeout(context.Background(), terminalCloseTimeout)
		_ = handler.TerminalClose(ctx, terminalID, "owner")
		cancel()
	}
}

// TerminalOutput implements TerminalSink: one chunk goes to the owner of the
// terminal and to nobody else.
func (h *hub) TerminalOutput(terminalID string, data []byte) {
	if len(data) == 0 {
		return
	}
	if owner := h.ownerOf(terminalID); owner != nil {
		owner.enqueue(marshalTerminalOutput(terminalID, data))
	}
}

// TerminalClosed implements TerminalSink: the owner is told once, and the
// ownership is released.
func (h *hub) TerminalClosed(terminalID string, exitCode int, reason string) {
	h.mu.Lock()
	owner := h.terminalOwner[terminalID]
	delete(h.terminalOwner, terminalID)
	h.mu.Unlock()

	if owner != nil {
		owner.enqueue(marshalTerminalClosed(terminalID, exitCode, reason))
	}
}

// ownerOf returns the connection that owns one terminal.
func (h *hub) ownerOf(terminalID string) *connection {
	h.mu.Lock()
	defer h.mu.Unlock()

	return h.terminalOwner[terminalID]
}

// handleTerminalOpen dispatches one terminal.open frame and answers it with the
// terminal it created.
func (h *hub) handleTerminalOpen(c *connection, frame inbound) {
	if !c.who.scope.allows(ScopeOperator) {
		// A PTY runs arbitrary commands: a read-only connection may not have one.
		h.auditDenied(c, "", "terminal.open", "scope")
		c.enqueue(marshalErrorResponse(frame.ID, codeForbiddenScope, "this connection is read-only"))
		return
	}
	handler := h.terminalHandler()
	if handler == nil {
		c.enqueue(marshalErrorResponse(frame.ID, codeUnsupported, "no terminal handler is configured"))
		return
	}

	request, err := parseTerminalOpen(frame)
	if err != nil {
		c.enqueue(marshalErrorResponse(frame.ID, codeBadRequest, err.Error()))
		return
	}
	h.spawnHandler(func() {
		terminal, err := handler.TerminalOpen(c.ctx, request.dir, request.cols, request.rows)
		if err != nil {
			code, message := codeOf(err)
			h.auditTerminal(c, audit.ActionTerminalOpen, terminal.ID, audit.OutcomeError)
			c.enqueue(marshalErrorResponse(frame.ID, code, message))
			return
		}
		h.claimTerminal(c, terminal.ID)
		h.auditTerminal(c, audit.ActionTerminalOpen, terminal.ID, audit.OutcomeOK)
		c.enqueue(marshalHandlerData(frame.ID, terminalData(terminal)))
	})
}

// handleTerminalInput, handleTerminalResize and handleTerminalClose share the same
// shape: the connection may only touch a terminal it owns, and the answer is the
// `response` frame of the id the client sent (close frames carry none, so a
// successful close is silent).
func (h *hub) handleTerminalInput(c *connection, frame inbound) {
	handler, terminalID, ok := h.terminalFor(c, frame)
	if !ok {
		return
	}
	data, err := base64.StdEncoding.DecodeString(frame.Data)
	if err != nil {
		c.enqueue(marshalErrorResponse(frame.ID, codeBadRequest, "data is not valid base64"))
		return
	}
	h.spawnHandler(func() {
		if err := handler.TerminalInput(c.ctx, terminalID, data); err != nil {
			code, message := codeOf(err)
			c.enqueue(marshalErrorResponse(frame.ID, code, message))
			return
		}
		// Input has no answer of its own: the response is what tells a client its
		// keystrokes were accepted, and it carries no data.
		c.enqueue(marshalHandlerData(frame.ID, nil))
	})
}

// handleTerminalResize dispatches one terminal.resize frame.
func (h *hub) handleTerminalResize(c *connection, frame inbound) {
	handler, terminalID, ok := h.terminalFor(c, frame)
	if !ok {
		return
	}
	if frame.Cols == 0 || frame.Rows == 0 {
		c.enqueue(marshalErrorResponse(frame.ID, codeBadRequest, "a resize needs columns and rows"))
		return
	}
	h.spawnHandler(func() {
		if err := handler.TerminalResize(c.ctx, terminalID, uint16(frame.Cols), uint16(frame.Rows)); err != nil {
			code, message := codeOf(err)
			c.enqueue(marshalErrorResponse(frame.ID, code, message))
			return
		}
		c.enqueue(marshalHandlerData(frame.ID, nil))
	})
}

// handleTerminalClose dispatches one terminal.close frame.
func (h *hub) handleTerminalClose(c *connection, frame inbound) {
	handler := h.terminalHandler()
	if handler == nil {
		c.enqueue(marshalErrorResponse(frame.ID, codeUnsupported, "no terminal handler is configured"))
		return
	}
	if !h.ownsTerminal(c, frame.TerminalID) {
		c.enqueue(marshalErrorResponse(frame.ID, codeNotFound, "no such terminal on this connection"))
		return
	}
	h.mu.Lock()
	delete(h.terminalOwner, frame.TerminalID)
	h.mu.Unlock()

	h.spawnHandler(func() {
		if err := handler.TerminalClose(c.ctx, frame.TerminalID, "client"); err != nil {
			code, message := codeOf(err)
			c.enqueue(marshalErrorResponse(frame.ID, code, message))
			return
		}
		h.auditTerminal(c, audit.ActionTerminalClose, frame.TerminalID, audit.OutcomeOK)
		c.enqueue(marshalHandlerData(frame.ID, nil))
	})
}

// terminalFor resolves the handler and the terminal one frame addresses, answering
// the client when either is missing.
func (h *hub) terminalFor(c *connection, frame inbound) (TerminalHandler, string, bool) {
	handler := h.terminalHandler()
	if handler == nil {
		c.enqueue(marshalErrorResponse(frame.ID, codeUnsupported, "no terminal handler is configured"))
		return nil, "", false
	}
	if frame.TerminalID == "" || !h.ownsTerminal(c, frame.TerminalID) {
		c.enqueue(marshalErrorResponse(frame.ID, codeNotFound, "no such terminal on this connection"))
		return nil, "", false
	}
	return handler, frame.TerminalID, true
}

// ownsTerminal reports whether one connection opened one terminal.
func (h *hub) ownsTerminal(c *connection, terminalID string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	owner, ok := h.terminalOwner[terminalID]
	return ok && owner == c
}

// Errors and bounds of the terminal frames. The schema enforces them too; these
// exist so the handler answers a bad frame with a sentence instead of a panic.
var (
	errNoDir   = errors.New("terminal.open needs a directory")
	errTooWide = errors.New("the terminal size is out of range")
)

// maxTerminalDimension bounds columns and rows: a client that sends 0xffff would ask
// the kernel for a screen nobody has.
const maxTerminalDimension = 1000

// terminalCloseTimeout bounds the close of a terminal whose owner is gone: the
// connection teardown must not wait for a stubborn process tree.
const terminalCloseTimeout = 5 * time.Second

// terminalOpenRequest is the validated view of a terminal.open frame.
type terminalOpenRequest struct {
	dir  string
	cols uint16
	rows uint16
}

// parseTerminalOpen reads the fields the handler needs, applying the defaults the
// service documents for an omitted size.
func parseTerminalOpen(frame inbound) (terminalOpenRequest, error) {
	if frame.Dir == "" {
		return terminalOpenRequest{}, errNoDir
	}
	request := terminalOpenRequest{dir: frame.Dir}
	if frame.Cols > 0 {
		if frame.Cols > maxTerminalDimension {
			return terminalOpenRequest{}, errTooWide
		}
		request.cols = uint16(frame.Cols)
	}
	if frame.Rows > 0 {
		if frame.Rows > maxTerminalDimension {
			return terminalOpenRequest{}, errTooWide
		}
		request.rows = uint16(frame.Rows)
	}
	return request, nil
}

// terminalData is the data of the `response` that answers a terminal.open.
func terminalData(terminal Terminal) json.RawMessage {
	return mustMarshal(struct {
		TerminalID string `json:"terminalId"`
		CWD        string `json:"cwd"`
		PID        int    `json:"pid,omitempty"`
		Cols       uint16 `json:"cols,omitempty"`
		Rows       uint16 `json:"rows,omitempty"`
	}{
		TerminalID: terminal.ID,
		CWD:        terminal.CWD,
		PID:        terminal.PID,
		Cols:       terminal.Cols,
		Rows:       terminal.Rows,
	})
}

// marshalTerminalOutput builds one terminal.output frame.
func marshalTerminalOutput(terminalID string, data []byte) []byte {
	return mustMarshal(struct {
		Type       string `json:"type"`
		TerminalID string `json:"terminalId"`
		Data       string `json:"data"`
		Ts         string `json:"ts"`
	}{
		Type:       frameTerminalOutput,
		TerminalID: terminalID,
		Data:       base64.StdEncoding.EncodeToString(data),
		Ts:         time.Now().UTC().Format(tsLayout),
	})
}

// marshalTerminalClosed builds one terminal.closed frame.
func marshalTerminalClosed(terminalID string, exitCode int, reason string) []byte {
	return mustMarshal(struct {
		Type       string `json:"type"`
		TerminalID string `json:"terminalId"`
		ExitCode   int    `json:"exitCode"`
		Reason     string `json:"reason,omitempty"`
		Ts         string `json:"ts"`
	}{
		Type:       frameTerminalClosed,
		TerminalID: terminalID,
		ExitCode:   exitCode,
		Reason:     reason,
		Ts:         time.Now().UTC().Format(tsLayout),
	})
}

// auditTerminal records one terminal action in the trail: opening a shell and
// closing it are exactly the kind of host effect an operator wants to read back.
func (h *hub) auditTerminal(c *connection, action audit.Action, terminalID string, outcome audit.Outcome) {
	h.record(audit.Event{
		Action:        action,
		Outcome:       outcome,
		ActorDeviceID: c.who.deviceID,
		ActorScope:    string(c.who.scope),
		Target:        terminalID,
		Details:       map[string]any{"method": "terminal"},
	})
}
