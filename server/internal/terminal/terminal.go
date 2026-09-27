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
	"io"
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
	// KillGrace is how long a closing terminal waits for SIGHUP before SIGKILL. The
	// signal is SIGHUP and not SIGTERM because an interactive shell ignores SIGTERM
	// by design (POSIX): a close that used it waited out the whole grace period on
	// every terminal, which is exactly what a user notices as a hang.
	KillGrace = 2 * time.Second
	// readChunk is how much output one read takes from the PTY. It is the frame size
	// a client receives, so it is a compromise between per-frame overhead and how
	// promptly an interactive prompt appears.
	readChunk = 32 << 10
	// readWait is how long the pump waits for output before it looks at whether the
	// terminal is being closed.
	//
	// The master is opened blocking by the PTY library, and a blocking read on a PTY
	// is not interrupted by closing the file: the pump would stay stuck in a read
	// forever, and a client would never be told the terminal is gone (measured: it
	// hung until the process was killed and then still waited). Polling the
	// descriptor is what makes a close interruptible without a second goroutine per
	// read and without leaking one blocked reader per closed terminal.
	readWait = 200 * time.Millisecond
	// writeRetries and writeRetryDelay bound the retry of a write into a full PTY
	// buffer; readRetryDelay is the pause of a sink-less Reader between two empty
	// reads.
	writeRetries    = 200
	writeRetryDelay = time.Millisecond
	readRetryDelay  = 2 * time.Millisecond
)

// Where a close came from, as the notification reports it.
const (
	ReasonClient = "client"
	ReasonOwner  = "owner"
	ReasonServer = "server"
	ReasonExit   = "exit"
)

// Sink is where a terminal's output goes. The WebSocket hub implements it in
// production; a test implements it to assert what a client would have received.
//
// It is an interface rather than a callback field so the terminal package stays
// independent of the transport that shows it.
type Sink interface {
	// Output delivers bytes produced by one terminal, in order.
	Output(terminalID string, data []byte)
	// Closed reports that one terminal is gone, exactly once, after the last chunk.
	Closed(terminalID string, exitCode int, reason string)
}

// Config wires the manager.
type Config struct {
	// FS is the confinement every directory goes through (required).
	FS *fs.Service
	// Sink receives the output and the close notifications.
	Sink Sink
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
	// PID is the shell's process id.
	PID int
	// Cols and Rows are the size the shell was started with.
	Cols uint16
	Rows uint16
	// Started is when the PTY was opened.
	Started time.Time

	sink Sink

	file *os.File
	cmd  *exec.Cmd

	// onClose is the manager's unregister hook, so a terminal closed through the
	// session frees its slot exactly like one closed through the manager.
	onClose func()

	// exited is closed once the shell has been reaped, so a close can wait for the
	// process tree instead of guessing.
	exited chan struct{}
	once   sync.Once

	mu       sync.Mutex
	closed   bool
	stopping bool
	reason   string
	exitCode int
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

// SetSink installs (or replaces) where the output goes.
func (m *Manager) SetSink(sink Sink) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.Sink = sink
	for _, session := range m.sessions {
		session.sink = sink
	}
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
	cols, rows = sizeOrDefault(cols, rows)

	m.mu.Lock()
	if len(m.sessions) >= m.cfg.MaxSessions {
		open := len(m.sessions)
		m.mu.Unlock()
		return nil, sessions.Codedf(sessions.CodeSessionLimit,
			"%d terminals are already open", open)
	}
	sink := m.cfg.Sink
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
	// The master is put in non-blocking mode on purpose: a blocking read on a PTY is
	// not interrupted by closing the file, so one terminal that stopped producing
	// output would hold a goroutine and a descriptor until the process left. Every
	// read below tolerates EAGAIN instead.
	_ = syscall.SetNonblock(int(file.Fd()), true)
	sessionID := fmt.Sprintf("t_%016x", m.counter.Add(1))
	session := &Session{
		ID:       sessionID,
		Dir:      confined,
		PID:      command.Process.Pid,
		Cols:     cols,
		Rows:     rows,
		Started:  time.Now(),
		sink:     sink,
		file:     file,
		cmd:      command,
		onClose:  func() { m.forget(sessionID) },
		exited:   make(chan struct{}),
		exitCode: -1,
	}
	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	// The pump owns the reads and the reaping: one goroutine per terminal, gone when the
	// PTY is.
	//
	// Without a sink there is nothing to forward, and a pump would be a second reader —
	// stealing the bytes a plain `CopyTo` client is waiting for. The session then only
	// reaps, which is what ends the stream for whoever reads it.
	if sink == nil {
		go session.reap()
	} else {
		go session.pump()
	}
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

// Close stops one terminal; an unknown id is a coded not_found.
func (m *Manager) Close(id string) error {
	return m.CloseWithReason(id, ReasonClient)
}

// CloseWithReason stops one terminal and says who asked.
func (m *Manager) CloseWithReason(id, reason string) error {
	session, ok := m.Get(id)
	if !ok {
		return sessions.Codedf(sessions.CodeNotFound, "no terminal %s", id)
	}
	return session.CloseWithReason(reason)
}

// Input writes bytes to one terminal.
func (m *Manager) Input(id string, data []byte) error {
	session, ok := m.Get(id)
	if !ok {
		return sessions.Codedf(sessions.CodeNotFound, "no terminal %s", id)
	}
	_, err := session.Write(data)
	return err
}

// Resize reports a new size for one terminal.
func (m *Manager) Resize(id string, cols, rows uint16) error {
	session, ok := m.Get(id)
	if !ok {
		return sessions.Codedf(sessions.CodeNotFound, "no terminal %s", id)
	}
	return session.Resize(cols, rows)
}

// Shutdown closes every terminal: the server is going down, no shell outlives it.
func (m *Manager) Shutdown() error {
	for _, session := range m.List() {
		_ = session.CloseWithReason(ReasonServer)
	}
	return nil
}

// forget drops one terminal from the registry.
func (m *Manager) forget(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.sessions, id)
}

// pump reads the PTY until it ends, then reaps the shell and reports the close.
//
// It is the only reader and the only Waiter: a second read would steal output from
// the client, and a second Wait would race the exit status the client is told.
func (s *Session) pump() {
	buffer := make([]byte, readChunk)
	for {
		s.mu.Lock()
		file, stopping := s.file, s.stopping
		s.mu.Unlock()
		if file == nil || stopping {
			break
		}
		ready, err := waitReadable(file, readWait)
		if err != nil {
			// A descriptor the kernel will not poll is not a reason to lose the
			// terminal: the read below blocks, which is what the old path did anyway.
			ready = true
		}
		if !ready {
			continue
		}
		read, err := file.Read(buffer)
		if read > 0 {
			if sink := s.sinkFor(); sink != nil {
				sink.Output(s.ID, append([]byte(nil), buffer[:read]...))
			}
			continue
		}
		if err != nil && !errors.Is(err, syscall.EAGAIN) && !errors.Is(err, syscall.EWOULDBLOCK) {
			break
		}
	}
	s.reap()
}

// waitReadable reports whether the PTY has something to read within timeout.
func waitReadable(file *os.File, timeout time.Duration) (bool, error) {
	fd := int(file.Fd())
	var readSet syscall.FdSet
	index := fd / 64
	if index < 0 || index >= len(readSet.Bits) {
		return false, errors.New("terminal: the descriptor is out of range for select")
	}
	readSet.Bits[index] |= 1 << (uint(fd) % 64)
	tv := syscall.NsecToTimeval(timeout.Nanoseconds())
	ready, err := syscall.Select(fd+1, &readSet, nil, nil, &tv)
	if errors.Is(err, syscall.EINTR) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return ready > 0, nil
}

// reap waits for the shell once and tells the sink it is gone.
func (s *Session) reap() {
	s.once.Do(func() {
		code := -1
		if s.cmd != nil {
			_ = s.cmd.Wait()
			if state := s.cmd.ProcessState; state != nil {
				code = state.ExitCode()
			}
		}
		s.mu.Lock()
		s.exitCode = code
		reason := s.reason
		s.mu.Unlock()
		if reason == "" {
			reason = ReasonExit
		}
		close(s.exited)
		if sink := s.sinkFor(); sink != nil {
			sink.Closed(s.ID, code, reason)
		}
	})
}

// sinkFor reads the sink under the lock: SetSink may replace it while a terminal
// opened earlier is still producing output.
func (s *Session) sinkFor() Sink {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.sink
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
//
// The master is non-blocking, so a full type-ahead buffer answers EAGAIN; a bounded
// retry loop is what keeps a paste from being dropped while the shell catches up.
func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	file := s.file
	s.mu.Unlock()
	if file == nil {
		return 0, os.ErrClosed
	}
	for attempt := 0; ; attempt++ {
		written, err := file.Write(p)
		if err == nil {
			return written, nil
		}
		if attempt < writeRetries &&
			(errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK)) {
			time.Sleep(writeRetryDelay)
			continue
		}
		return written, err
	}
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
	s.Cols, s.Rows = cols, rows
	return nil
}

// Wait blocks until the shell has been reaped.
func (s *Session) Wait() error {
	<-s.exited
	return nil
}

// ExitCode reports the shell's exit status, -1 while it is running or when a signal
// ended it.
func (s *Session) ExitCode() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.exitCode
}

// Close ends the terminal: SIGHUP to the process group, SIGKILL after the grace
// period, then the PTY is released. Closing twice is not an error.
func (s *Session) Close() error {
	return s.CloseWithReason(ReasonClient)
}

// CloseWithReason is Close with the reason the notification carries.
func (s *Session) CloseWithReason(reason string) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	s.stopping = true
	if s.reason == "" {
		s.reason = reason
	}
	file, command := s.file, s.cmd
	s.mu.Unlock()

	if command != nil && command.Process != nil {
		// The whole group, and SIGHUP because that is the signal a shell reads as
		// "your terminal is gone": SIGTERM is ignored by an interactive shell.
		_ = syscall.Kill(-command.Process.Pid, syscall.SIGHUP)
		select {
		case <-s.exited:
		case <-time.After(KillGrace):
			_ = syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
			select {
			case <-s.exited:
			case <-time.After(KillGrace):
			}
		}
	}
	// The reap happens here as well as in the pump: `sync.Once` makes it one Wait and
	// one notification whoever gets there first, and a client is told the terminal is
	// gone even if the pump is between two reads.
	s.reap()
	if file != nil {
		_ = file.Close()
	}
	s.mu.Lock()
	s.file = nil
	s.mu.Unlock()

	if onClose := s.onClose; onClose != nil {
		onClose()
	}
	return nil
}

// Done reports whether the terminal was closed.
func (s *Session) Done() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.closed
}

// sizeOrDefault applies the documented default for an omitted size.
func sizeOrDefault(cols, rows uint16) (uint16, uint16) {
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}
	return cols, rows
}

// Discard is a sink that throws everything away, for a manager that has no transport
// (a test, or a server whose only client is the CLI).
type Discard struct{}

// Output implements Sink.
func (Discard) Output(string, []byte) {}

// Closed implements Sink.
func (Discard) Closed(string, int, string) {}

// CopyTo pumps one terminal into a writer until it ends: what a plain terminal
// client (a test, or a future `pi-ui attach`) needs without a WebSocket.
func (s *Session) CopyTo(w io.Writer) error {
	buffer := make([]byte, readChunk)
	for {
		if s.Done() {
			return nil
		}
		read, err := s.Read(buffer)
		if read > 0 {
			if _, writeErr := w.Write(buffer[:read]); writeErr != nil {
				return writeErr
			}
			continue
		}
		if err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				time.Sleep(readRetryDelay)
				continue
			}
			return nil
		}
	}
}
