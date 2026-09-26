package cli

import (
	"context"
	"errors"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/terminal"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// terminalBridge adapts the PTY manager to the WebSocket hub's terminal seam.
//
// The adapter exists so neither side imports the other: the hub knows frames and
// ownership, the manager knows PTYs and process groups, and this is the only place
// that knows both.
type terminalBridge struct {
	manager *terminal.Manager
}

// TerminalOpen implements ws.TerminalHandler.
func (b terminalBridge) TerminalOpen(ctx context.Context, dir string, cols, rows uint16) (ws.Terminal, error) {
	session, err := b.manager.Open(ctx, dir, cols, rows)
	if err != nil {
		return ws.Terminal{}, err
	}
	return ws.Terminal{
		ID:   session.ID,
		CWD:  session.Dir,
		PID:  session.PID,
		Cols: session.Cols,
		Rows: session.Rows,
	}, nil
}

// TerminalInput implements ws.TerminalHandler.
func (b terminalBridge) TerminalInput(_ context.Context, terminalID string, data []byte) error {
	return b.manager.Input(terminalID, data)
}

// TerminalResize implements ws.TerminalHandler.
func (b terminalBridge) TerminalResize(_ context.Context, terminalID string, cols, rows uint16) error {
	return b.manager.Resize(terminalID, cols, rows)
}

// TerminalClose implements ws.TerminalHandler.
func (b terminalBridge) TerminalClose(_ context.Context, terminalID, reason string) error {
	return b.manager.CloseWithReason(terminalID, reason)
}

// terminalSink adapts the hub to the manager's outbound sink: the PTY service
// describes what happened, the hub decides which socket hears it.
type terminalSink struct {
	hub ws.TerminalSink
}

// Output implements terminal.Sink.
func (s terminalSink) Output(terminalID string, data []byte) {
	s.hub.TerminalOutput(terminalID, data)
}

// Closed implements terminal.Sink.
func (s terminalSink) Closed(terminalID string, exitCode int, reason string) {
	s.hub.TerminalClosed(terminalID, exitCode, reason)
}

// newTerminals builds the PTY service of one server and installs both halves of the
// seam, or reports that this server has no workspace to run a shell in.
func newTerminals(files *fs.Service, hub ws.Hub, maxSessions int) (*terminal.Manager, error) {
	sink, ok := hub.(ws.TerminalSink)
	if !ok {
		return nil, errors.New("cli: the hub does not implement terminal.Sink")
	}
	manager, err := terminal.New(terminal.Config{
		FS:          files,
		Sink:        terminalSink{hub: sink},
		MaxSessions: maxSessions,
	})
	if err != nil {
		return nil, err
	}
	hub.SetTerminalHandler(terminalBridge{manager: manager})
	return manager, nil
}
