package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// typeResponse is the record envelope that answers a command. Every other type is an
// event or an extension request and travels to the consumer untouched.
const typeResponse = "response"

// Defaults New applies to a zero Options field.
const (
	defaultRecordBuffer = 1024
	defaultSendTimeout  = 15 * time.Second
	defaultKillGrace    = 5 * time.Second
)

// Errors the bridge reports. Both sentinels are wrapped, never returned bare, so a caller
// can add context without losing errors.Is; the wrapped message carries the exit status
// when it is known.
var (
	// ErrClosed means the child is gone or Close ran: the bridge cannot talk to it
	// anymore.
	ErrClosed = errors.New("rpc: bridge closed")
	// ErrTimeout means the response did not arrive within the caller's deadline, or
	// within Options.SendTimeout when the caller had none.
	ErrTimeout = errors.New("rpc: command timeout")

	// errNotStarted guards the calls that need a running child.
	errNotStarted = errors.New("rpc: bridge not started")
)

// Record is one JSONL record read from the child's stdout, byte-for-byte.
type Record struct {
	Raw  json.RawMessage
	Type string // "response", "extension_ui_request", or an event type
	ID   string // correlation id when the record carries one
}

// Spec describes one child process. Dir is the session working directory.
type Spec struct {
	Command []string          // argv; argv[0] is resolved with exec.LookPath
	Dir     string            // child working directory ("" inherits the server's)
	Env     []string          // extra entries, appended to os.Environ() (a later entry wins)
	Stderr  func(line []byte) // diagnostics hook; nil discards
}

// Options tunes one bridge. New replaces a zero field with its default.
type Options struct {
	RecordBuffer int           // bounded channel size (default 1024)
	SendTimeout  time.Duration // default per-command deadline (default 15s)
	KillGrace    time.Duration // TERM→KILL grace on Close (default 5s)
	// Logf receives bridge diagnostics that belong to no caller: a response nobody waits
	// for, a child that ignores stdin EOF. It never carries child payloads. Nil discards.
	Logf func(format string, args ...any)
}

// Bridge drives one child process. All methods are safe for concurrent use.
type Bridge interface {
	Start(ctx context.Context) error
	// Send writes one command (raw JSON object) and waits for the response with the
	// same id. Context cancellation returns ErrTimeout/ctx.Err(), never closes the child.
	Send(ctx context.Context, id string, command json.RawMessage) (json.RawMessage, error)
	// Write writes a record without waiting (used for extension_ui_response).
	Write(ctx context.Context, record json.RawMessage) error
	// Records streams non-response records (events and extension_ui_request) in order.
	Records() <-chan Record
	PID() int
	// Wait blocks until the child exits and returns its exit error (nil on code 0).
	Wait() error
	// Close closes stdin, waits KillGrace, then TERMs and KILLs the process group.
	// Idempotent; always reaps the child.
	Close() error
}

// waiterResult is what a waiting Send receives: the response record, or the reason it will
// never arrive.
type waiterResult struct {
	raw json.RawMessage
	err error
}

// bridge is the Bridge implementation: one child, one reader goroutine, one write lock.
type bridge struct {
	spec Spec
	opts Options

	records  chan Record       // published records; closed once, by readLoop
	response chan waiterResult // answer for the command the bridge waits for

	writeMu sync.Mutex // serialises child stdin writes

	mu       sync.Mutex
	starting bool // a Start is in flight
	started  bool // the child was spawned
	closed   bool // Close ran; Send/Write report ErrClosed
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	pid      int
	pending  string // id of the command being waited for
	exited   bool
	exitErr  error

	procDone   chan struct{} // closed once the child is reaped
	stderrDone chan struct{} // closed once stderr reached EOF
	reapOnce   sync.Once
	closeOnce  sync.Once
}

// New returns a bridge for one child process. Nothing is spawned until Start.
func New(spec Spec, opts Options) Bridge {
	if opts.RecordBuffer <= 0 {
		opts.RecordBuffer = defaultRecordBuffer
	}
	if opts.SendTimeout <= 0 {
		opts.SendTimeout = defaultSendTimeout
	}
	if opts.KillGrace <= 0 {
		opts.KillGrace = defaultKillGrace
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	// Own the caller's slices: a session may reuse its argv buffer.
	owned := Spec{
		Command: append([]string(nil), spec.Command...),
		Dir:     spec.Dir,
		Env:     append([]string(nil), spec.Env...),
		Stderr:  spec.Stderr,
	}
	return &bridge{
		spec:       owned,
		opts:       opts,
		records:    make(chan Record, opts.RecordBuffer),
		response:   make(chan waiterResult, 1),
		procDone:   make(chan struct{}),
		stderrDone: make(chan struct{}),
	}
}

// Records streams every non-response record in order. Exactly one close happens, after
// stdout reached EOF and the child was reaped.
func (b *bridge) Records() <-chan Record { return b.records }

// PID is the child's process id, or 0 before Start.
func (b *bridge) PID() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pid
}

// parseRecord extracts the top-level type and id of a record without validating it: an
// unknown field, an unknown type or a record that is not a JSON object still yields its
// bytes, because the server forwards pi payloads verbatim.
func parseRecord(raw []byte) Record {
	record := Record{Raw: json.RawMessage(raw)}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return record
	}
	_ = json.Unmarshal(fields["type"], &record.Type)
	_ = json.Unmarshal(fields["id"], &record.ID)
	return record
}

// route handles one record from the child: a response goes to the sender that waits for
// that id, everything else is published to the consumer in order.
func (b *bridge) route(record Record) {
	if record.Type == typeResponse {
		b.answer(record)
		return
	}
	// Blocks while the consumer is behind: pi honours stdout backpressure, so the child
	// slows down instead of losing records.
	b.records <- record
}

// answer hands a response to the waiting sender. A response nobody waits for is dropped:
// pi answers a malformed command without an id, and an answer can arrive after its sender
// gave up.
func (b *bridge) answer(record Record) {
	b.mu.Lock()
	match := b.pending != "" && record.ID != "" && record.ID == b.pending
	if match {
		b.pending = ""
	}
	b.mu.Unlock()
	if !match {
		b.logf("rpc: child %d sent a response for id %q that nobody waits for", b.PID(), record.ID)
		return
	}
	select {
	case b.response <- waiterResult{raw: record.Raw}:
	default:
	}
}

// releaseWaiters unblocks a sender that is still waiting when the child is gone.
func (b *bridge) releaseWaiters() {
	b.mu.Lock()
	waiting := b.pending != ""
	b.pending = ""
	b.mu.Unlock()
	if !waiting {
		return
	}
	select {
	case b.response <- waiterResult{err: b.closedError()}:
	default:
	}
}

// Send writes one command and waits for the response. pi answers a command exactly once,
// and the supervisor keeps one command in flight per session, so the bridge waits for the
// response carrying the id it was given. The wait is bounded by ctx, or by
// Options.SendTimeout when ctx has no deadline; a timeout never touches the child.
func (b *bridge) Send(ctx context.Context, id string, command json.RawMessage) (json.RawMessage, error) {
	ctx = contextOrBackground(ctx)
	if id == "" {
		return nil, errors.New("rpc: send: id is required")
	}
	payload, err := commandWithID(command, id)
	if err != nil {
		return nil, fmt.Errorf("rpc: send %s: %w", id, err)
	}
	subject := "command " + id
	if err := b.expectResponse(id); err != nil {
		return nil, err
	}
	defer b.clearExpectation(id)

	if err := b.writeRecord(ctx, payload, subject); err != nil {
		return nil, err
	}
	waitCtx, cancel := deadlineContext(ctx, b.opts.SendTimeout)
	defer cancel()

	select {
	case result := <-b.response:
		return result.raw, result.err
	case <-waitCtx.Done():
		return nil, timeoutError(ctx, subject)
	}
}

// expectResponse registers the one command the bridge waits for. A second concurrent
// command is rejected instead of being correlated with the wrong response.
func (b *bridge) expectResponse(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.closed:
		return fmt.Errorf("%w (command %s)", b.closedErrorLocked(), id)
	case !b.started:
		return errNotStarted
	case b.pending != "":
		return fmt.Errorf("rpc: send %s: command %s is already in flight", id, b.pending)
	}
	b.pending = id
	return nil
}

// clearExpectation drops an expectation the caller stopped waiting for, so a late response
// is reported instead of being handed to the next command.
func (b *bridge) clearExpectation(id string) {
	b.mu.Lock()
	if b.pending == id {
		b.pending = ""
	}
	b.mu.Unlock()
}

// Write writes one record unchanged, without waiting: the extension_ui_response path. The
// record goes through the same discipline as a command, so it can never interleave with
// one.
func (b *bridge) Write(ctx context.Context, record json.RawMessage) error {
	return b.writeRecord(contextOrBackground(ctx), record, "write")
}

// writeRecord sends one record to the child's stdin: serialised by one mutex, terminated
// with a single line feed and bounded by ctx (Options.SendTimeout when ctx carries no
// deadline). subject names the caller in the error, "command req-1" or "write".
func (b *bridge) writeRecord(ctx context.Context, record []byte, subject string) error {
	b.mu.Lock()
	stdin, closed, started := b.stdin, b.closed, b.started
	b.mu.Unlock()

	switch {
	case closed:
		return fmt.Errorf("%w (%s)", b.closedError(), subject)
	case !started || stdin == nil:
		return errNotStarted
	case len(record) == 0:
		return fmt.Errorf("rpc: %s: record is empty", subject)
	case bytes.IndexByte(record, lineFeed) >= 0:
		return fmt.Errorf("rpc: %s: record must not contain a raw newline", subject)
	}

	line := make([]byte, 0, len(record)+1)
	line = append(line, record...)
	line = append(line, lineFeed)

	writeCtx, cancel := deadlineContext(ctx, b.opts.SendTimeout)
	defer cancel()

	b.writeMu.Lock()
	defer b.writeMu.Unlock()
	restore := applyWriteDeadline(stdin, writeCtx)
	defer restore()

	if _, err := stdin.Write(line); err != nil {
		if b.hasExited() {
			return fmt.Errorf("%w (%s)", b.closedError(), subject)
		}
		if ctx.Err() != nil {
			return timeoutError(ctx, subject)
		}
		return fmt.Errorf("rpc: %s: write record: %w", subject, err)
	}
	return nil
}

// commandWithID guarantees the record carries the correlation id. A record that already
// carries the same id is written byte-for-byte; one without an id gets the field injected
// without re-encoding the rest; one that carries a different id is re-encoded with the
// caller's id, because the response is matched on the id the caller passed.
func commandWithID(command json.RawMessage, id string) (json.RawMessage, error) {
	trimmed := bytes.TrimSpace(command)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return nil, errors.New("command must be a JSON object")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return nil, fmt.Errorf("command is not a JSON object: %w", err)
	}
	encoded, err := json.Marshal(id)
	if err != nil {
		return nil, fmt.Errorf("encode id: %w", err)
	}
	if current, ok := fields["id"]; ok {
		var existing string
		if err := json.Unmarshal(current, &existing); err == nil && existing == id {
			return trimmed, nil
		}
		fields["id"] = encoded
		return json.Marshal(fields)
	}
	// Inject the field in place: the rest of the object stays byte-for-byte as the caller
	// wrote it.
	body := bytes.TrimSpace(trimmed[1:]) // an object always ends with '}'
	injected := make([]byte, 0, len(trimmed)+len(encoded)+len(`"id":,`))
	injected = append(injected, '{')
	injected = append(injected, `"id":`...)
	injected = append(injected, encoded...)
	if len(body) > 1 {
		injected = append(injected, ',')
	}
	return append(injected, body...), nil
}

// timeoutError describes a bound that expired: the caller's own context when it had one,
// the bridge default otherwise. A plain cancellation keeps context.Canceled, an expired
// deadline is additionally reported as ErrTimeout so callers treat both the same way.
func timeoutError(ctx context.Context, subject string) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return fmt.Errorf("%w: %w (%s)", ErrTimeout, ctxErr, subject)
		}
		return fmt.Errorf("%w (%s)", ctxErr, subject)
	}
	return fmt.Errorf("%w (%s)", ErrTimeout, subject)
}

// applyWriteDeadline bounds a stdin write where the platform supports it and returns the
// call that restores the pipe. Pipe files are pollable on Linux, so a child that stopped
// reading makes the write fail instead of blocking the caller forever; where deadlines are
// unsupported the write blocks, which is pi's stdin backpressure.
func applyWriteDeadline(stdin io.WriteCloser, ctx context.Context) func() {
	deadline, ok := ctx.Deadline()
	if !ok {
		return func() {}
	}
	file, ok := stdin.(*os.File)
	if !ok {
		return func() {}
	}
	if err := file.SetWriteDeadline(deadline); err != nil {
		return func() {}
	}
	return func() { _ = file.SetWriteDeadline(time.Time{}) }
}

// logf reports a bridge diagnostic. Diagnostics never carry conversation content: the
// hook is for lifecycle and framing problems.
func (b *bridge) logf(format string, args ...any) { b.opts.Logf(format, args...) }

// closedError is what a caller gets once the bridge cannot talk to the child anymore:
// ErrClosed plus the exit status when it is known, so a supervisor can tell an orderly
// shutdown from a crash.
func (b *bridge) closedError() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.closedErrorLocked()
}

func (b *bridge) closedErrorLocked() error {
	switch {
	case b.exitErr != nil:
		return fmt.Errorf("%w: child exited: %v", ErrClosed, b.exitErr)
	case b.exited:
		return fmt.Errorf("%w: child exited", ErrClosed)
	default:
		return fmt.Errorf("%w: child stdout closed", ErrClosed)
	}
}

// claimStart reserves the one Start of this bridge.
func (b *bridge) claimStart() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.closed:
		return b.closedErrorLocked()
	case b.starting || b.started:
		return errors.New("rpc: bridge already started")
	}
	b.starting = true
	return nil
}

// releaseStart lets the caller retry after a spawn that failed.
func (b *bridge) releaseStart() {
	b.mu.Lock()
	b.starting = false
	b.mu.Unlock()
}

// register publishes the running child to the rest of the bridge.
func (b *bridge) register(cmd *exec.Cmd, stdin io.WriteCloser) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.starting = false
	b.started = true
	b.cmd = cmd
	b.stdin = stdin
	if cmd.Process != nil {
		b.pid = cmd.Process.Pid
	}
}

// command returns the child command, or nil before Start.
func (b *bridge) command() *exec.Cmd {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cmd
}

// hasExited reports whether the child is gone and reaped.
func (b *bridge) hasExited() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.exited
}

// process returns the child's OS process, or nil before Start.
func (b *bridge) process() *os.Process {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil {
		return nil
	}
	return b.cmd.Process
}

// childAlive reports whether the child still exists; an unreaped child counts as existing.
// Only Close uses it, to reap a child whose reader goroutine is parked.
func (b *bridge) childAlive() bool {
	process := b.process()
	return process != nil && process.Signal(syscall.Signal(0)) == nil
}

// contextError reports a context that was cancelled before a call started.
func contextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}

// contextOrBackground accepts a nil context: the bridge has no use for a cancellation it
// could not observe anyway, and panicking on nil would be worse.
func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// deadlineContext bounds an operation by the caller's deadline and falls back to the
// bridge default when the caller has none.
func deadlineContext(ctx context.Context, fallback time.Duration) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, fallback)
}

// bridge must keep satisfying the frozen interface of docs/spike-interfaces.md §5.1.
var _ Bridge = (*bridge)(nil)
