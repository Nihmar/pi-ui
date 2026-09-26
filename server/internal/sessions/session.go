package sessions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Nihmar/pi-ui/server/internal/rpc"
	"github.com/Nihmar/pi-ui/server/internal/ws"
)

// pi command types this package sends by name; everything else reaches the child through
// session.command.raw (§6), so a new pi command needs no change here.
const (
	commandTypeGetState   = "get_state"
	commandTypeGetEntries = "get_entries"
	commandTypeRename     = "rename"
)

// Record types of the pi protocol this package interprets. Every other type is forwarded
// as pi.<type>: its bytes verbatim when they are valid JSON, a {"raw":"<line>"} envelope
// otherwise (see recordPayload).
const (
	recordExtensionUIRequest = "extension_ui_request"
	recordAgentEnd           = "agent_end"
	recordAgentSettled       = "agent_settled"
)

// piState is the part of pi's get_state payload the projection exposes (§5.3).
type piState struct {
	SessionID     string
	SessionFile   string
	ModelProvider string
	ModelID       string
	ThinkingLevel string
}

// session is one child process plus everything the supervisor knows about it. Lock order
// is cmdMu before mu, and mgr.mu before mu when both are needed: no path takes them the
// other way round.
type session struct {
	mgr *Manager
	id  string

	createdAt time.Time

	mu        sync.Mutex
	cwd       string
	name      string
	status    Status
	bridge    rpc.Bridge
	pid       int
	piState   piState
	lastEvent time.Time
	finished  bool
	exitCode  *int
	dialogs   map[string]*dialog

	cmdMu  sync.Mutex
	nextID uint64

	// done is closed by pump once finish has written the terminal status. stop
	// waits for it, so a caller never observes a session whose child is already
	// reaped but whose projection still says "stopping".
	done chan struct{}
}

// newSession builds the record for a child that is about to be spawned.
func newSession(mgr *Manager, id string, spec Spec) *session {
	return &session{
		mgr:       mgr,
		id:        id,
		createdAt: time.Now().UTC(),
		cwd:       spec.CWD,
		name:      spec.Name,
		status:    StatusSpawning,
		dialogs:   map[string]*dialog{},
		done:      make(chan struct{}),
	}
}

// attach publishes the child driver to the rest of the session.
func (s *session) attach(bridge rpc.Bridge) {
	s.mu.Lock()
	s.bridge = bridge
	s.mu.Unlock()
}

// bridgeRef returns the child driver, or nil before Start.
func (s *session) bridgeRef() rpc.Bridge {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.bridge
}

// setPID records the child's process id for the projection.
func (s *session) setPID(pid int) {
	s.mu.Lock()
	s.pid = pid
	s.mu.Unlock()
}

// setName follows a successful rename, so the projection and the child agree.
func (s *session) setName(name string) {
	s.mu.Lock()
	s.name = name
	s.mu.Unlock()
}

// isLive reports whether the session still occupies a MaxSessions slot.
func (s *session) isLive() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status.Live()
}

// stopRequested reports whether a Stop has marked the session stopping, which is the state
// a spawn that raced the Stop has to honour.
func (s *session) stopRequested() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status == StatusStopping && !s.finished
}

// touch records that the child produced a record, which is what LastEventAt reports.
func (s *session) touch() {
	s.mu.Lock()
	s.lastEvent = time.Now().UTC()
	s.mu.Unlock()
}

// info returns the projection: the payload of every lifecycle event, the body of the REST
// endpoints and the answer of Get.
func (s *session) info() Info {
	s.mu.Lock()
	defer s.mu.Unlock()
	info := Info{
		ID:            s.id,
		CWD:           s.cwd,
		Name:          s.name,
		Status:        s.status,
		PID:           s.pid,
		PiSessionID:   s.piState.SessionID,
		PiSessionFile: s.piState.SessionFile,
		ModelProvider: s.piState.ModelProvider,
		ModelID:       s.piState.ModelID,
		ThinkingLevel: s.piState.ThinkingLevel,
		CreatedAt:     timestamp(s.createdAt),
	}
	if s.exitCode != nil {
		// Copy the value: the projection must not hand callers a pointer into the
		// session's own state, which the pump mutates under the lock.
		code := *s.exitCode
		info.ExitCode = &code
	}
	if !s.lastEvent.IsZero() {
		info.LastEventAt = timestamp(s.lastEvent)
	}
	return info
}

// setStatusQuiet moves the session into next and reports whether that changed anything.
func (s *session) setStatusQuiet(next Status) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status == next {
		return false
	}
	s.status = next
	return true
}

// setStatus publishes server.status when the state actually changed, so ready↔streaming is
// visible to clients without a second source of truth.
func (s *session) setStatus(next Status) {
	if s.setStatusQuiet(next) {
		s.mgr.publishJSON(EventServerStatus, s.id, s.info())
	}
}

// logf reports one session diagnostic. It never carries conversation content: the child's
// stderr is pi's own diagnostics channel (§3) and everything logged here is a lifecycle
// fact.
func (s *session) logf(format string, args ...any) {
	s.mgr.logger.Debug("sessions: child", "sessionId", s.id, "message", fmt.Sprintf(format, args...))
}

// maxStderrLog caps one logged diagnostic line. pi's stderr is a diagnostics channel (§3),
// and a pathological line must not become a pathological log entry.
const maxStderrLog = 2048

// stderrLine is the rpc.Spec.Stderr hook, so the child's diagnostics reach slog instead of
// the record stream.
func (s *session) stderrLine(line []byte) {
	message := string(line)
	if len(message) > maxStderrLog {
		message = strings.ToValidUTF8(message[:maxStderrLog], "") + "…[truncated]"
	}
	s.logf("stderr: %s", message)
}

// pump consumes the child's records until stdout closes, then classifies the exit and
// closes done, which is what stop waits for. Records() closes only after the child exited
// and stdout was drained, so the lifecycle event always follows the last payload record.
func (s *session) pump() {
	for record := range s.bridge.Records() {
		s.handleRecord(record)
	}
	s.finish(s.bridge.Wait())
	close(s.done)
}

// handleRecord publishes one child record: pi.* verbatim for everything except the
// extension UI subprotocol, which the server terminates instead of forwarding (§5.3, §8).
// A line the child got wrong is not valid JSON and cannot be framed as-is; it still
// reaches clients as pi.unknown, carrying the line in a {"raw":"<line>"} payload.
func (s *session) handleRecord(record rpc.Record) {
	s.touch()
	switch record.Type {
	case recordExtensionUIRequest:
		s.handleExtensionUIRequest(record.Raw)
		return
	}
	s.noteActivity(record.Type)
	s.mgr.publish(ws.Event{
		Type:      piEventType(record.Type),
		SessionID: s.id,
		EntryID:   entryIDOf(record.Raw, record.Type),
		Payload:   recordPayload(record),
	})
}

// recordPayload is the wire payload of one pi.* event: the child's record bytes verbatim
// when they are valid JSON, or {"raw":"<line>"} when they are not. The hub refuses an
// event payload that is not valid JSON, and a malformed line must stay readable on the
// wire instead of surfacing as a connection-scoped server.error. Bytes that are not valid
// UTF-8 cannot survive a JSON string, so they are also carried base64-encoded in rawBase64
// (see rawLinePayload): the record is recoverable exactly, not approximated to U+FFFD.
func recordPayload(record rpc.Record) json.RawMessage {
	if json.Valid(record.Raw) {
		return record.Raw
	}
	payload := rawLinePayload{Raw: string(record.Raw)}
	if !utf8.Valid(record.Raw) {
		payload.RawBase64 = base64.StdEncoding.EncodeToString(record.Raw)
	}
	return mustJSON(payload)
}

// Record types that say whether a session is working. Only the types the spike documents
// are interpreted; anything else leaves the status alone, so an unknown pi event can never
// move a session out of ready.
var (
	workingRecordTypes = map[string]bool{
		"message_update":        true,
		"bash_execution_update": true,
	}
	settledRecordTypes = map[string]bool{
		recordAgentEnd:     true,
		recordAgentSettled: true,
	}
)

// noteActivity derives the ready↔streaming status from the child's own records. Deriving it
// from the record stream (instead of from the prompt call) keeps the order deterministic:
// one pump goroutine consumes the records in order, so a session can never be left
// streaming after its own turn ended.
func (s *session) noteActivity(recordType string) {
	switch {
	case settledRecordTypes[recordType]:
		s.setStatus(StatusReady)
	case workingRecordTypes[recordType]:
		s.setStatus(StatusStreaming)
	}
}

// finish records the exit, publishes server.exited or server.crashed (§5.3) and releases
// the session's slot. An explicit stop is an orderly exit whatever the exit status says,
// and the session stays listed with its final status either way.
func (s *session) finish(err error) {
	s.mu.Lock()
	if s.finished {
		s.mu.Unlock()
		return
	}
	s.finished = true
	explicitStop := s.status == StatusStopping
	code := exitCodeOf(err)
	s.exitCode = &code
	status := StatusCrashed
	if err == nil || explicitStop {
		status = StatusExited
	}
	s.status = status
	s.cancelDialogsLocked(time.Now())
	s.mu.Unlock()

	if status == StatusCrashed {
		s.logf("child exited abnormally: %v", err)
		s.mgr.publishJSON(EventServerCrashed, s.id, exitPayload{ExitCode: code})
		return
	}
	s.mgr.publishJSON(EventServerExited, s.id, exitPayload{ExitCode: code})
}

// exitCodeOf is the exit status of the child: 0 on an orderly exit, the process status
// otherwise, and -1 when a signal ended it — Go reports -1 for a signalled process, which
// is the honest answer, because a signalled child has no exit code at all.
func exitCodeOf(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}

// stop shuts the child down and is idempotent: stdin close, then SIGTERM, then SIGKILL,
// all inside rpc's graces. server.stopping is published before the child is asked to
// leave, so a client sees the intent even when the child dies slowly. It waits for the
// pump to publish the terminal status before returning — a caller can never read
// "stopping" for a session whose child is already reaped — and reports a timeout instead
// of a false success when that status never arrives.
func (s *session) stop(ctx context.Context) error {
	s.mu.Lock()
	bridge := s.bridge
	if s.finished {
		s.mu.Unlock()
		return nil
	}
	if s.status == StatusStopping {
		s.mu.Unlock()
		return s.awaitTerminal(ctx, bridge)
	}
	s.status = StatusStopping
	s.mu.Unlock()

	s.mgr.publishJSON(EventServerStopping, s.id, s.info())
	if bridge == nil {
		// No bridge yet means Start has not launched the pump either: there is
		// no terminal status to wait for.
		return nil
	}
	err := bridge.Close()
	waitErr := s.awaitTerminal(ctx, bridge)
	if err != nil {
		return Codedf(CodePiError, "stopping session %s: %v", s.id, err)
	}
	return waitErr
}

// closeIfStopRequested honours a Stop that ran before the child existed: stop saw a nil
// bridge, marked the session stopping and returned, so the spawn that Start is completing
// would otherwise leave the session ready and its child running. It reports a coded
// session_exited when it intervened, and nil when no stop raced the spawn.
func (s *session) closeIfStopRequested(ctx context.Context, bridge rpc.Bridge) error {
	if !s.stopRequested() {
		return nil
	}
	_ = bridge.Close()
	_ = s.awaitTerminal(ctx, bridge)
	return Codedf(CodeSessionExited, "session %s was stopped while it was spawning", s.id)
}

// awaitTerminal waits for the pump to publish the session's terminal status, bounded by
// terminalBudget so a wedged child can never hold Stop or Shutdown forever. bridge is the
// reference stop read under the lock: a bridge that was never attached means no pump was
// launched and there is nothing to wait for.
//
// A budget that expires is reported as a timeout, not swallowed: the child was reaped, but
// the status a client reads is still "stopping", so a caller that gets no error must be
// able to trust that the session finished its bookkeeping. A cancelled ctx aborts the wait
// the same way: the caller is gone (a closed connection), and the pump still writes the
// terminal status for whoever asks next.
func (s *session) awaitTerminal(ctx context.Context, bridge rpc.Bridge) error {
	if bridge == nil {
		return nil
	}
	timer := time.NewTimer(terminalBudget)
	defer timer.Stop()
	select {
	case <-s.done:
		return nil
	case <-ctx.Done():
		return Codedf(CodeTimeout, "session %s: waiting for its terminal status: %v", s.id, ctx.Err())
	case <-timer.C:
		s.logf("terminal status not written within %s", terminalBudget)
		return Codedf(CodeTimeout, "session %s did not publish its terminal status within %s", s.id, terminalBudget)
	}
}

// call sends one pi command and returns its response data. Commands are serialised per
// session because rpc.Bridge correlates exactly one command in flight: a second concurrent
// command would be rejected by the bridge instead of being correlated wrongly.
func (s *session) call(ctx context.Context, command json.RawMessage) (json.RawMessage, error) {
	s.cmdMu.Lock()
	defer s.cmdMu.Unlock()

	bridge := s.bridgeRef()
	if bridge == nil {
		return nil, Codedf(CodePiError, "session %s has no child", s.id)
	}
	raw, err := bridge.Send(ctx, s.nextCommandID(), command)
	if err != nil {
		return nil, piError(err)
	}
	return responseData(raw)
}

// callPi builds a pi command from a type plus optional fields and sends it.
func (s *session) callPi(ctx context.Context, commandType string, fields map[string]any) (json.RawMessage, error) {
	command, err := encodePiCommand(commandType, fields)
	if err != nil {
		return nil, err
	}
	return s.call(ctx, command)
}

// nextCommandID mints the correlation id of one server-side command. The client never sees
// it: the id on a WS frame belongs to the hub, this one belongs to the child.
func (s *session) nextCommandID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	return fmt.Sprintf("srv-%d", s.nextID)
}

// busyPhrase is pi 0.87.1's rejection of a prompt sent while a turn is already running
// ("Agent is already processing. Specify streamingBehavior ('steer' or 'followUp') to
// queue the message."). pi reports the reason as free text with no machine-readable
// code, so the mapping to busy_streaming has to match on it; it stays narrow on
// purpose, and a pi version that rewords it degrades to pi_rejected — the client
// still gets a terminal, coded failure either way.
const busyPhrase = "already processing"

// responseData validates one pi response and returns its data field. success:false becomes
// the code a client can act on: busy_streaming when pi refused because a turn is already
// running, pi_rejected for every other rejection (§5.3, §11); an unreadable response is a
// pi_error, so a broken child can never look like a success.
func responseData(raw json.RawMessage) (json.RawMessage, error) {
	var response struct {
		Success bool            `json:"success"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &response) != nil {
		return nil, Codedf(CodePiError, "the child sent an unreadable response")
	}
	if !response.Success {
		message := response.Error
		if message == "" {
			message = "the child rejected the command"
		}
		code := CodePiRejected
		if strings.Contains(message, busyPhrase) {
			code = CodeBusyStreaming
		}
		return nil, &CodedError{Code: code, Msg: message}
	}
	return response.Data, nil
}

// applyState folds a get_state payload into the projection. Fields the payload omits keep
// their previous value instead of blanking it, and the caller decides when to announce
// readiness: Start publishes server.ready itself.
func (s *session) applyState(data json.RawMessage) {
	var state struct {
		SessionID     string `json:"sessionId"`
		SessionFile   string `json:"sessionFile"`
		ThinkingLevel string `json:"thinkingLevel"`
		Model         *struct {
			ID       string `json:"id"`
			Provider string `json:"provider"`
		} `json:"model"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		s.logf("get_state payload is not readable: %v", err)
	}

	s.mu.Lock()
	s.piState.SessionID = firstNonEmpty(state.SessionID, s.piState.SessionID)
	s.piState.SessionFile = firstNonEmpty(state.SessionFile, s.piState.SessionFile)
	s.piState.ThinkingLevel = firstNonEmpty(state.ThinkingLevel, s.piState.ThinkingLevel)
	if state.Model != nil {
		s.piState.ModelID = firstNonEmpty(state.Model.ID, s.piState.ModelID)
		s.piState.ModelProvider = firstNonEmpty(state.Model.Provider, s.piState.ModelProvider)
	}
	s.mu.Unlock()

	s.setStatusQuiet(StatusReady)
}
