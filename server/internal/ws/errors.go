package ws

import (
	"context"
	"errors"

	"github.com/Nihmar/pi-ui/server/internal/protocol/gen"
)

// Error codes the hub itself produces. They are taken from the generated
// taxonomy, so a code cannot drift from schemas/core.json by a typo, and core.json
// stays the single source of truth for the wire values.
const (
	codeBadRequest          = string(gen.WsErrorCodeBadRequest)
	codeInternal            = string(gen.WsErrorCodeInternal)
	codeSlowConsumer        = string(gen.WsErrorCodeSlowConsumer)
	codeTimeout             = string(gen.WsErrorCodeTimeout)
	codeUnauthorized        = string(gen.WsErrorCodeUnauthorized)
	codeUnsupported         = string(gen.WsErrorCodeUnsupported)
	codeReplayCursorInvalid = string(gen.WsErrorCodeReplayCursorInvalid)
	codeSessionNotFound     = string(gen.WsErrorCodeSessionNotFound)
)

// knownCodes is the closed set of wire codes, built from the generated constants.
// A handler is free to return any error, but only a code from the taxonomy may
// reach a client: anything else would be a value no client can branch on, so it
// degrades to internal.
var knownCodes = map[string]struct{}{
	string(gen.WsErrorCodeAlreadyAnswered):     {},
	string(gen.WsErrorCodeBadRequest):          {},
	string(gen.WsErrorCodeBusyStreaming):       {},
	string(gen.WsErrorCodeForbiddenScope):      {},
	string(gen.WsErrorCodeInternal):            {},
	string(gen.WsErrorCodeManagedMode):         {},
	string(gen.WsErrorCodeModelProviderError):  {},
	string(gen.WsErrorCodeNotFound):            {},
	string(gen.WsErrorCodePathEscape):          {},
	string(gen.WsErrorCodePiError):             {},
	string(gen.WsErrorCodePiRejected):          {},
	string(gen.WsErrorCodeRateLimited):         {},
	string(gen.WsErrorCodeReplayCursorInvalid): {},
	string(gen.WsErrorCodeSessionExited):       {},
	string(gen.WsErrorCodeSessionLimit):        {},
	string(gen.WsErrorCodeSessionNotFound):     {},
	string(gen.WsErrorCodeSessionNotReady):     {},
	string(gen.WsErrorCodeSlowConsumer):        {},
	string(gen.WsErrorCodeTimeout):             {},
	string(gen.WsErrorCodeTooLarge):            {},
	string(gen.WsErrorCodeUnauthorized):        {},
	string(gen.WsErrorCodeUnsupported):         {},
	string(gen.WsErrorCodeWorkspaceNotAllowed): {},
}

// codedError is implemented by errors that know which wire code they map to.
//
// It is the only contract between the hub and a handler's failures: sessions
// returns its own error type with ErrorCode()/ErrorMessage(), and a handler that
// does not implement it still works, degrading to internal.
type codedError interface {
	ErrorCode() string
	ErrorMessage() string
}

// codeOf maps a handler error onto a response code and message.
//
// The lookup is deliberate about untrusted values: the code is checked against
// the taxonomy, and a missing or unknown one becomes internal, so a handler can
// never put a value on the wire that clients cannot interpret.
func codeOf(err error) (code, message string) {
	if err == nil {
		return "", ""
	}

	var ce codedError
	if errors.As(err, &ce) {
		code = ce.ErrorCode()
		message = ce.ErrorMessage()
	}

	if code == "" {
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			code = codeTimeout
		default:
			code = codeInternal
		}
	}
	if _, ok := knownCodes[code]; !ok {
		code = codeInternal
	}
	if message == "" {
		message = err.Error()
	}
	return code, message
}
