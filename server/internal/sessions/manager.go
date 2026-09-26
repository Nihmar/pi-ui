package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Nihmar/pi-ui/server/internal/ratelimit"
	"github.com/Nihmar/pi-ui/server/internal/rpc"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// Manager is the supervisor implementation (docs/spike-interfaces.md §5.3). One value
// serves all three hub seams, so cli/serve wires the same pointer into
// SetCommandHandler, SetDialogHandler and SetReplayer and no state is duplicated.
type Manager struct {
	cfg    Config
	logger *slog.Logger

	mu       sync.Mutex
	sessions map[string]*session
	order    []string // creation order, so List is stable

	// promptLimit bounds prompts per session per minute; nil when disabled.
	promptLimit *ratelimit.Limiter
}

var (
	_ Supervisor        = (*Manager)(nil)
	_ ws.CommandHandler = (*Manager)(nil)
	_ ws.DialogHandler  = (*Manager)(nil)
	_ ws.Replayer       = (*Manager)(nil)
)

// New returns the supervisor for cfg, filling the documented defaults. Nothing is spawned
// until Start; a nil Hub makes publishing a no-op, which keeps supervisor-only tests
// simple while the real wiring always injects the hub.
func New(cfg Config) *Manager {
	if len(cfg.PiCommand) == 0 {
		cfg.PiCommand = append([]string(nil), defaultPiCommand...)
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = DefaultMaxSessions
	}
	if cfg.DialogTimeout <= 0 {
		cfg.DialogTimeout = DefaultDialogTimeout
	}
	if cfg.SendTimeout <= 0 {
		cfg.SendTimeout = DefaultSendTimeout
	}
	if cfg.PromptLimit < 0 {
		cfg.PromptLimit = 0
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	return &Manager{
		cfg:         cfg,
		logger:      cfg.Logger,
		sessions:    map[string]*session{},
		promptLimit: ratelimit.New(cfg.PromptLimit),
	}
}

// Start spawns one child and confirms readiness with a get_state round trip: server.spawned
// is published as soon as the process exists, server.ready once the child answered. A child
// that never answers is stopped again and reported as ErrStart (wrapped), and its session
// stays listed with the status the exit produced.
func (m *Manager) Start(ctx context.Context, spec Spec) (Info, error) {
	if err := validateSpec(spec, m.cfg); err != nil {
		return Info{}, err
	}
	sessionID, err := newSessionID()
	if err != nil {
		return Info{}, err
	}
	argv := m.childArgv(spec)
	env, err := m.childEnv(spec, argv, sessionID)
	if err != nil {
		return Info{}, err
	}

	s := newSession(m, sessionID, spec)

	m.mu.Lock()
	if m.liveLocked() >= m.cfg.MaxSessions {
		m.mu.Unlock()
		return Info{}, fmt.Errorf("%w: %d sessions are already live", ErrLimit, m.cfg.MaxSessions)
	}
	m.sessions[sessionID] = s
	m.order = append(m.order, sessionID)
	m.mu.Unlock()

	bridge := rpc.New(
		rpc.Spec{Command: argv, Dir: spec.CWD, Env: env, Stderr: s.stderrLine},
		rpc.Options{SendTimeout: m.cfg.SendTimeout, KillGrace: killGrace, Logf: s.logf},
	)
	s.attach(bridge)

	// A Stop may land before the child exists: it sees no bridge, marks the session
	// stopping and returns. Honour it here instead of spawning a child only to kill it,
	// and forget the session: no child was ever created, so there is no terminal status
	// for a pump to write.
	if s.stopRequested() {
		m.forget(sessionID)
		return Info{}, Codedf(CodeSessionExited, "session %s was stopped while it was spawning", sessionID)
	}

	if err := bridge.Start(ctx); err != nil {
		m.forget(sessionID)
		return Info{}, fmt.Errorf("%w: spawn: %v", ErrStart, err)
	}
	s.setPID(bridge.PID())
	m.publishJSON(EventServerSpawned, sessionID, s.info())
	go s.pump()

	// The Stop can also land while the child is spawning: the child exists now, so close
	// it and let the pump publish the terminal status.
	if err := s.closeIfStopRequested(ctx, bridge); err != nil {
		return s.info(), err
	}

	state, err := s.callPi(ctx, commandTypeGetState, nil)
	if err != nil {
		// The child never became usable: stop it so the server is not left with an
		// unaccounted process, and let the pump report the exit.
		_ = s.stop(ctx)
		return Info{}, fmt.Errorf("%w: get_state: %v", ErrStart, err)
	}
	s.applyState(state)
	m.publishJSON(EventServerReady, sessionID, s.info())
	return s.info(), nil
}

// Get returns the projection of one session.
func (m *Manager) Get(id string) (Info, bool) {
	s, ok := m.find(id)
	if !ok {
		return Info{}, false
	}
	return s.info(), true
}

// List returns every session, live or finished, in creation order (§5.3: a finished
// session stays listed with its final status).
func (m *Manager) List() []Info {
	m.mu.Lock()
	ids := append([]string(nil), m.order...)
	m.mu.Unlock()

	infos := make([]Info, 0, len(ids))
	for _, id := range ids {
		if s, ok := m.find(id); ok {
			infos = append(infos, s.info())
		}
	}
	return infos
}

// find returns one session by id.
func (m *Manager) find(id string) (*session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	return s, ok
}

// all returns a snapshot of every session.
func (m *Manager) all() []*session {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := make([]*session, 0, len(m.sessions))
	for _, id := range m.order {
		if s, ok := m.sessions[id]; ok {
			all = append(all, s)
		}
	}
	return all
}

// liveLocked counts the sessions that still occupy a MaxSessions slot. A finished session
// stays listed and releases its slot, so a long-lived server is not wedged by history.
func (m *Manager) liveLocked() int {
	live := 0
	for _, s := range m.sessions {
		if s.isLive() {
			live++
		}
	}
	return live
}

// forget drops a session that never started, so a spawn failure leaves no ghost entry.
func (m *Manager) forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	for i, existing := range m.order {
		if existing == id {
			m.order = append(m.order[:i], m.order[i+1:]...)
			break
		}
	}
}

// validateSpec rejects a Spec that cannot become a child process.
func validateSpec(spec Spec, cfg Config) error {
	switch {
	case spec.CWD == "":
		return fmt.Errorf("%w: cwd is required", ErrInvalidSpec)
	case len(spec.Command) == 0 && len(cfg.PiCommand) == 0:
		return fmt.Errorf("%w: no child command", ErrInvalidSpec)
	}
	info, err := os.Stat(spec.CWD)
	if err != nil {
		return fmt.Errorf("%w: cwd %s: %v", ErrInvalidSpec, spec.CWD, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: cwd %s is not a directory", ErrInvalidSpec, spec.CWD)
	}
	return nil
}

// Send dispatches one operation to one session (docs/spike-interfaces.md §5.3, §6). The
// op table is the single registration point: a Phase 3 operation is one more entry there,
// never another branch here.
func (m *Manager) Send(ctx context.Context, sessionID, op string, payload json.RawMessage) (json.RawMessage, error) {
	s, ok := m.find(sessionID)
	if !ok {
		return nil, Codedf(CodeSessionNotFound, "no session %q", sessionID)
	}
	handler, ok := ops[op]
	if !ok {
		return nil, Codedf(CodeBadRequest, "unsupported op %q", op)
	}
	return handler(ctx, s, payload)
}

// Handle implements ws.CommandHandler: the hub forwards every validated `command` frame
// here and turns the returned data or *CodedError into a `response` frame.
func (m *Manager) Handle(ctx context.Context, c ws.Command) (json.RawMessage, error) {
	return m.Send(ctx, c.SessionID, c.Op, c.Payload)
}

// Respond implements ws.DialogHandler: the first ui_response for a pending dialog wins,
// every later one is already_answered (§8).
func (m *Manager) Respond(ctx context.Context, sessionID, requestID string, response json.RawMessage) error {
	s, ok := m.find(sessionID)
	if !ok {
		return Codedf(CodeSessionNotFound, "no session %q", sessionID)
	}
	return s.answer(ctx, requestID, response)
}

// Stop shuts one session down gracefully — stdin close, then SIGTERM, then SIGKILL, all
// bounded by rpc's graces — and returns ErrNotFound for an unknown id. Stopping a session
// that already finished is a no-op: the POST /stop endpoint stays idempotent.
func (m *Manager) Stop(ctx context.Context, sessionID string) error {
	s, ok := m.find(sessionID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, sessionID)
	}
	return s.stop(ctx)
}

// Shutdown stops every child in parallel and waits for the reaping and the terminal
// status of each. The server's own context is normally already cancelled when this runs
// (SIGTERM), so only a deadline in ctx is honoured; otherwise the call bounds itself with
// stopBudget, the worst case of one graceful stop, so a slow child cannot be reported as a
// stuck one.
func (m *Manager) Shutdown(ctx context.Context) error {
	budget := stopBudget
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < budget {
			budget = remaining
		}
	}

	// stopCtx detaches the wait from the caller's cancellation: on SIGTERM the caller's
	// context is already cancelled, and that must not skip the terminal status. A deadline
	// still applies, so a caller with a tighter budget keeps it.
	stopCtx := context.WithoutCancel(ctx)
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc
		stopCtx, cancel = context.WithDeadline(stopCtx, deadline)
		defer cancel()
	}

	var wg sync.WaitGroup
	for _, s := range m.all() {
		wg.Add(1)
		go func(s *session) {
			defer wg.Done()
			_ = s.stop(stopCtx)
		}(s)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(budget):
		return fmt.Errorf("sessions: shutdown: %d sessions did not stop within %s", m.liveLocked(), budget)
	}
}

// sendDialog hands one extension dialog to the hub (§8): the complete `request` frame goes
// through ws.Requester, which routes it to that session's current subscribers. A dialog is
// not an event — it carries no seq, it is never replayed, and the frame is validated by the
// hub — so a hub that cannot send requests is a wiring bug, not a silent downgrade.
func (m *Manager) sendDialog(sessionID string, frame json.RawMessage) {
	requester, ok := m.cfg.Hub.(ws.Requester)
	if !ok {
		if m.cfg.Hub != nil {
			m.logger.Error("sessions: hub does not implement ws.Requester: extension dialogs cannot be routed",
				"sessionId", sessionID)
		}
		return
	}
	if err := requester.SendRequest(sessionID, frame); err != nil {
		m.logger.Error("sessions: extension dialog could not be routed",
			"sessionId", sessionID, "error", err)
	}
}
