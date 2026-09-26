package sessions

import (
	"errors"
	"time"
)

// Errors the supervisor reports. ErrLimit and ErrNotFound are frozen by §5.3; the rest
// are additions a caller can match with errors.Is.
var (
	// ErrLimit means MaxSessions live sessions already exist. Exited and crashed
	// sessions stay listed but release their slot.
	ErrLimit = errors.New("sessions: limit reached")
	// ErrNotFound means no session carries that id.
	ErrNotFound = errors.New("sessions: session not found")
	// ErrInvalidSpec means the Spec cannot be spawned: an empty working directory, a
	// working directory that is not a directory, or an empty child argv.
	ErrInvalidSpec = errors.New("sessions: invalid spec")
	// ErrStart means the child was spawned but never confirmed readiness: it died
	// before the get_state round trip or the round trip failed. The session stays
	// listed with its final status.
	ErrStart = errors.New("sessions: start failed")
	// ErrNotRunning means the session has no live child to talk to anymore.
	ErrNotRunning = errors.New("sessions: session is not running")
)

// Defaults New applies to a zero Config field (§5.3, §5.7).
const (
	// DefaultMaxSessions bounds the number of sessions that are alive at once.
	DefaultMaxSessions = 8
	// DefaultDialogTimeout is how long an unanswered extension dialog waits before the
	// server answers cancelled:true (PIUI_DIALOG_TIMEOUT).
	DefaultDialogTimeout = 60 * time.Second
	// DefaultSendTimeout bounds one command round trip. PIUI_* has no knob for it in
	// the spike; it stays a Config field so Phase 3 can expose one.
	DefaultSendTimeout = 15 * time.Second
	// DefaultPiCommand is the child argv when Config.PiCommand is empty.
	DefaultPiCommand = "pi"
)

// defaultPiCommand is DefaultPiCommand spelled as argv.
var defaultPiCommand = []string{DefaultPiCommand, "--mode", "rpc"}

// Budgets of the child lifecycle.
const (
	// killGrace is how long rpc.Close waits for the child to exit after stdin EOF
	// before SIGTERM; rpc's own termGrace (1s) follows before SIGKILL. Shutdown must
	// reap every child within two seconds (acceptance criterion C8), so the worst case
	// here is 0.7s + 1s and the shutdown budget below leaves room for both.
	killGrace = 700 * time.Millisecond
	// shutdownBudget bounds the whole Shutdown call, whatever the caller's context says.
	shutdownBudget = 2 * time.Second
	// writeTimeout bounds one extension_ui_response write that happens outside any
	// caller's context, because a dialog timeout has no request to borrow one from.
	writeTimeout = 5 * time.Second
	// dialogRetention keeps an answered dialog id recognisable long enough to tell
	// "already answered" from "never existed"; afterwards the entry is pruned.
	dialogRetention = 5 * time.Minute
)
