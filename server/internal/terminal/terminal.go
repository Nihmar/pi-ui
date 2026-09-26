// Package terminal owns the PTY sessions: one shell per session, in a directory a
// workspace allows, killed with its whole process group when it closes.
//
// A PTY is the one host capability that runs arbitrary commands, so the confinement
// of internal/fs is applied before the shell is started, not after: a directory
// outside every workspace never gets a process.
package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/creack/pty"

	"github.com/Nihmar/pi-ui/server/internal/fs"
	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// Defaults for a terminal nobody tuned.
const (
	DefaultShell       = "/bin/sh"
	DefaultMaxSessions = 4
	DefaultCols        = 120
	DefaultRows        = 32
	// KillGrace is how long a closing terminal waits for SIGTERM before SIGKILL.
	KillGrace = 2 * time.Second
)

// Config wires the manager.
type Config struct {
	// FS is the confinement every directory goes through (required).
	FS *fs.Service
	// Shell is the program to run (default DefaultShell).
	Shell string
	// MaxSessions bounds the open terminals (default DefaultMaxSessions): a browser
	// tab that reconnects must not leak a shell.
	MaxSessions int
	// Command overrides shell selection for a test (receives the shell path).
	Command func(shell string) *exec.Cmd
}

// Session is one open terminal.
type Session struct {
	// ID is the server-side id a client addresses it by.
	ID string
	// Dir is the confined working directory the shell started in.
	Dir string
	// Started is when the PTY was opened.
	Started time.Time

	file *os.File
	cmd  *exec.Cmd

	// onClose is the manager's unregister hook, so a terminal closed through the
	// session frees its slot exactly like one closed through the manager.
	onClose func()

	mu     sync.Mutex
	closed bool
}

// forget drops one terminal from the registry.
func (m *Manager) forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
}

// Manager owns every open terminal of one server.
type Manager struct {
	cfg      Config
	mu       sync.Mutex
	sessions map[string]*Session
	counter  atomic.Uint64
}

// New builds the manager.
func New(cfg Config) (*Manager, error) {
	if cfg.FS == nil {
		return nil, errors.New("terminal: the filesystem service is required")
	}
	if cfg.Shell == "" {
		cfg.Shell = DefaultShell
	}
	if cfg.MaxSessions <= 0 {
		cfg.MaxSessions = DefaultMaxSessions
	}
	return &Manager{cfg: cfg, sessions: map[string]*Session{}}, nil
}

// Open starts one shell in dir and returns its terminal.
func (m *Manager) Open(ctx context.Context, dir string, cols, rows uint16) (*Session, error) {
	confined, err := m.cfg.FS.Resolve(dir)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(confined)
	if err != nil || !info.IsDir() {
		return nil, sessions.Codedf(sessions.CodeNotFound, "%s is not a directory", dir)
	}
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}

	m.mu.Lock()
	if len(m.sessions) >= m.cfg.MaxSessions {
		m.mu.Unlock()
		return nil, sessions.Codedf(sessions.CodeSessionLimit,
			"%d terminals are already open", len(m.sessions))
	}
	m.mu.Unlock()

	var command *exec.Cmd
	if m.cfg.Command != nil {
		command = m.cfg.Command(m.cfg.Shell)
	} else {
		command = exec.CommandContext(ctx, m.cfg.Shell)
	}
	command.Dir = confined
	command.Env = append(os.Environ(), "TERM=xterm-256color")

	file, err := pty.StartWithSize(command, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, sessions.Codedf(sessions.CodeInternal, "the terminal could not start: %v", err)
	}
	sessionID := fmt.Sprintf("t_%016x", m.counter.Add(1))
	session := &Session{
		ID:      sessionID,
		onClose: func() { m.forget(sessionID) },
		Dir:     confined,
		Started: time.Now(),
		file:    file,
		cmd:     command,
	}
	m.mu.Lock()
	m.sessions[session.ID] = session
	m.mu.Unlock()
	return session, nil
}

// Get returns one open terminal.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	return session, ok
}

// List returns every open terminal.
func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	list := make([]*Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		list = append(list, session)
	}
	return list
}

// Shutdown closes every terminal: the server is going down, no shell outlives it.
func (m *Manager) Shutdown() error {
	for _, session := range m.List() {
		_ = session.Close()
	}
	return nil
}

// Close stops the terminal and reaps its process tree.
func (m *Manager) Close(id string) error {
	session, ok := m.Get(id)
	if !ok {
		return sessions.Codedf(sessions.CodeNotFound, "no terminal %s", id)
	}
	return session.Close()
}

// Read is the terminal's output: it blocks like a file, and a closed PTY ends it.
func (s *Session) Read(p []byte) (int, error) {
	s.mu.Lock()
	file := s.file
	s.mu.Unlock()
	if file == nil {
		return 0, os.ErrClosed
	}
	return file.Read(p)
}

// Write sends input, exactly as typed.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	file := s.file
	s.mu.Unlock()
	if file == nil {
		return 0, os.ErrClosed
	}
	return file.Write(p)
}

// Resize tells the shell how big the client's terminal is.
func (s *Session) Resize(cols, rows uint16) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return sessions.Codedf(sessions.CodeNotFound, "the terminal is closed")
	}
	if cols == 0 || rows == 0 {
		return sessions.Codedf(sessions.CodeBadRequest, "a resize needs columns and rows")
	}
	if err := pty.Setsize(s.file, &pty.Winsize{Cols: cols, Rows: rows}); err != nil {
		return sessions.Codedf(sessions.CodeInternal, "the terminal cannot be resized: %v", err)
	}
	return nil
}

// Wait blocks until the shell exits.
func (s *Session) Wait() error {
	s.mu.Lock()
	command := s.cmd
	s.mu.Unlock()
	if command == nil {
		return nil
	}
	return command.Wait()
}

// Close ends the terminal: SIGTERM to the process group, SIGKILL after the grace
// period, then the PTY is released. Closing twice is not an error.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	file, command := s.file, s.cmd
	s.file = nil
	s.mu.Unlock()

	if command != nil && command.Process != nil {
		// The whole group: a `sleep` a shell started must not survive the terminal.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() {
			_ = command.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(KillGrace):
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}
	if onClose := s.onClose; onClose != nil {
		onClose()
	}
	if file != nil {
		return file.Close()
	}
	return nil
}

// Done reports whether the terminal was closed.
func (s *Session) Done() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}
