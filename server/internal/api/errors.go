package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/Nihmar/pi-ui/server/internal/sessions"
)

// errorBody is the REST error envelope of §11: {"error":{"code","message"}}.
type errorBody struct {
	Error errorDetail `json:"error"`
}

// errorDetail is the code/message pair clients branch on.
type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON writes one JSON body with the given status. A body that cannot be marshalled is
// a programmer error: the status is already written, so it is logged by the caller's
// request log rather than replaced with a second answer.
func writeJSON(w http.ResponseWriter, status int, body any) {
	data, err := json.Marshal(body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the response could not be encoded")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(data, '\n'))
}

// writeError writes the shared error envelope.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// codeOr returns the taxonomy code carried by err (a *sessions.CodedError, or anything else
// with an ErrorCode method), or fallback when it carries none.
func codeOr(err error, fallback string) string {
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		if code := coded.ErrorCode(); code != "" {
			return code
		}
	}
	if code := errorCodeOf(err); code != "" {
		return code
	}
	return fallback
}

// statusFor maps a taxonomy code onto its HTTP status (PLAN.md §4.5).
func statusFor(code string) int {
	switch code {
	case "unauthorized":
		return http.StatusUnauthorized
	case "forbidden_scope", "workspace_not_allowed", "path_escape":
		return http.StatusForbidden
	case "not_found", "session_not_found":
		return http.StatusNotFound
	case "session_limit", "busy_streaming", "already_answered", "session_not_ready", "session_exited", "managed_mode":
		return http.StatusConflict
	case "too_large":
		return http.StatusRequestEntityTooLarge
	case "pi_rejected":
		return http.StatusUnprocessableEntity
	case "rate_limited":
		return http.StatusTooManyRequests
	case "unsupported":
		return http.StatusNotImplemented
	case "pi_error", "model_provider_error":
		return http.StatusBadGateway
	case "timeout":
		return http.StatusGatewayTimeout
	case "bad_request":
		return http.StatusBadRequest
	default:
		return http.StatusInternalServerError
	}
}

// scopeKey is the context key of the resolved scope.
type scopeKey struct{}

// withScope attaches the resolved scope to a request context.
func withScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

// scopeFrom returns the scope the request was authenticated with.
func scopeFrom(ctx context.Context) Scope {
	scope, _ := ctx.Value(scopeKey{}).(Scope)
	return scope
}

// errorCodeOf returns the taxonomy code of a plain sessions sentinel (ErrLimit, ErrNotFound,
// ErrInvalidSpec, ErrStart, ErrNotRunning): those are the errors the supervisor returns
// without wrapping them in a *CodedError. Anything else returns "", so an unrelated error is
// never reported as internal by accident.
func errorCodeOf(err error) string {
	switch {
	case errors.Is(err, sessions.ErrLimit):
		return sessions.CodeSessionLimit
	case errors.Is(err, sessions.ErrNotFound):
		return sessions.CodeSessionNotFound
	case errors.Is(err, sessions.ErrInvalidSpec):
		return sessions.CodeBadRequest
	case errors.Is(err, sessions.ErrStart), errors.Is(err, sessions.ErrNotRunning):
		return sessions.CodePiError
	default:
		return ""
	}
}

// itoa is strconv.Itoa for the one place that needs it.
func itoa(n int) string { return strconv.Itoa(n) }
