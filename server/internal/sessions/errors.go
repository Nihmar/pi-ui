package sessions

import (
	"context"
	"errors"
	"fmt"

	"github.com/Nihmar/pi-ui/server/internal/rpc"
)

// Error codes of the shared taxonomy (docs/spike-interfaces.md §11). The spike implements
// this subset; anything else maps to CodeInternal. They travel on a *CodedError so a
// transport (the WS hub building `response.error`, the REST envelope) can map a failure
// without importing this package's internals.
const (
	CodeUnauthorized        = "unauthorized"
	CodeForbiddenScope      = "forbidden_scope"
	CodeNotFound            = "not_found"
	CodeSessionNotFound     = "session_not_found"
	CodeSessionLimit        = "session_limit"
	CodeSessionExited       = "session_exited"
	CodeBadRequest          = "bad_request"
	CodeBusyStreaming       = "busy_streaming"
	CodePiRejected          = "pi_rejected"
	CodePiError             = "pi_error"
	CodeTimeout             = "timeout"
	CodeAlreadyAnswered     = "already_answered"
	CodeSlowConsumer        = "slow_consumer"
	CodeReplayCursorInvalid = "replay_cursor_invalid"
	CodeTooLarge            = "too_large"
	CodeUnsupported         = "unsupported"
	CodeRateLimited         = "rate_limited"
	// CodeFeatureDisabled is the server's own policy: an administrative setting turned
	// the capability off, which is not the caller's scope refusing it.
	CodeFeatureDisabled = "feature_disabled"
	// CodeUnavailable is a server that refuses for an operational reason: it is
	// draining, or it is shutting down.
	CodeUnavailable = "unavailable"
	CodeInternal    = "internal"
)

// CodedError is an error that carries its taxonomy code and a message that is safe to
// send to a client. Msg never contains provider secrets or file contents beyond what the
// child itself reported.
type CodedError struct {
	Code string // one of the Code* constants
	Msg  string // client-facing message
	Err  error  // underlying failure, for errors.Is/errors.As and logs
}

// Error implements error.
func (e *CodedError) Error() string {
	switch {
	case e.Msg != "" && e.Err != nil:
		return e.Msg + ": " + e.Err.Error()
	case e.Msg != "":
		return e.Msg
	case e.Err != nil:
		return e.Err.Error()
	default:
		return e.Code
	}
}

// ErrorCode is the taxonomy code; transports look for this method.
func (e *CodedError) ErrorCode() string { return e.Code }

// ErrorMessage is the client-facing message; transports look for this method too.
func (e *CodedError) ErrorMessage() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Error()
}

// Unwrap exposes the underlying failure.
func (e *CodedError) Unwrap() error { return e.Err }

// Codedf builds a *CodedError with a formatted message.
func Codedf(code, format string, args ...any) *CodedError {
	return &CodedError{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// CodeOf returns the taxonomy code carried by err, or "" when it carries none. A plain
// sentinel (ErrLimit, ErrNotFound, ErrInvalidSpec, ErrStart, ErrNotRunning) also maps, so
// callers of Start/Stop do not have to know the coded errors.
func CodeOf(err error) string {
	if err == nil {
		return ""
	}
	switch {
	case errors.Is(err, ErrLimit):
		return CodeSessionLimit
	case errors.Is(err, ErrNotFound):
		return CodeSessionNotFound
	case errors.Is(err, ErrInvalidSpec):
		return CodeBadRequest
	}
	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}
	switch {
	case errors.Is(err, ErrStart), errors.Is(err, ErrNotRunning):
		return CodePiError
	default:
		return CodeInternal
	}
}

// piError maps one transport failure onto the taxonomy of §11: a deadline (or a stream
// that ended under the caller) is a timeout, everything else is a child-side failure.
func piError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, rpc.ErrTimeout), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return &CodedError{Code: CodeTimeout, Msg: "no answer from the child in time", Err: err}
	default:
		return &CodedError{Code: CodePiError, Msg: "the child could not answer", Err: err}
	}
}
