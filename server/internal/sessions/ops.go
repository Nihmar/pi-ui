package sessions

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
)

// pi command types the op table maps onto. Everything else reaches pi through
// session.command.raw, so adding a pi command needs no change in this package.
const (
	commandTypePrompt     = "prompt"
	commandTypeSteer      = "steer"
	commandTypeFollowUp   = "follow_up"
	commandTypeAbort      = "abort"
	commandTypeClearQueue = "clear_queue"
)

// opHandler is one entry of the op registration map (§6): it receives the payload of a
// validated `command` frame and answers with the data of a `response` frame.
type opHandler func(ctx context.Context, s *session, payload json.RawMessage) (json.RawMessage, error)

// ops is the single registration point for client operations (docs/spike-interfaces.md
// §6): a Phase 3 operation is one more entry here, never another branch in Send.
//
// The keys are exactly the `op` values of the frozen protocol:
//
//	session.prompt       pi prompt            session.rename        pi rename + projection
//	session.steer        pi steer             session.stop          local graceful stop
//	session.follow_up    pi follow_up         session.command.raw   any pi command verbatim
//	session.abort        pi abort
//	session.clear_queue  pi clear_queue
var ops = map[string]opHandler{
	"session.prompt":      promptOp,
	"session.steer":       forwardOp(commandTypeSteer),
	"session.follow_up":   forwardOp(commandTypeFollowUp),
	"session.abort":       forwardOp(commandTypeAbort),
	"session.clear_queue": forwardOp(commandTypeClearQueue),
	"session.rename":      renameOp,
	"session.stop":        stopOp,
	"session.command.raw": rawOp,
}

// OpNames returns the registered operation names in lexicographic order, for diagnostics
// and for the feature list the server advertises.
func OpNames() []string {
	names := make([]string, 0, len(ops))
	for name := range ops {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// encodePiCommand builds one pi command object from a type and optional fields.
func encodePiCommand(commandType string, fields map[string]any) (json.RawMessage, error) {
	command := make(map[string]json.RawMessage, len(fields)+1)
	for key, value := range fields {
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, Codedf(CodeBadRequest, "encode %s: %v", key, err)
		}
		command[key] = encoded
	}
	setField(command, "type", commandType)
	return json.Marshal(command)
}

// commandFromPayload maps one client payload onto a pi command: the payload's own fields
// are copied through — {message: "hi"} becomes {"type":"prompt","message":"hi"} — and any
// client-supplied id is dropped, because the server assigns the correlation id.
func commandFromPayload(commandType string, payload json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if err := decodeObject("payload", payload, &fields); err != nil {
		return nil, err
	}
	delete(fields, "id")
	setField(fields, "type", commandType)
	return json.Marshal(fields)
}

// promptForward is the plain pi prompt the rate-limited op delegates to.
var promptForward = forwardOp(commandTypePrompt)

// promptOp bounds prompts per session (PLAN.md §4.6). A client that hammers a session
// gets a coded rate_limited answer carrying the wait; steer and follow-up stay the way
// to talk to a running turn, and they are not what this budget exists to stop.
func promptOp(ctx context.Context, s *session, payload json.RawMessage) (json.RawMessage, error) {
	if ok, retry := s.mgr.promptLimit.Allow(s.id); !ok {
		return nil, Codedf(CodeRateLimited, "prompt rate limit reached; retry in %s", retry.Round(time.Second))
	}
	return promptForward(ctx, s, payload)
}

// forwardOp maps one client op onto the same-named pi command.
func forwardOp(commandType string) opHandler {
	return func(ctx context.Context, s *session, payload json.RawMessage) (json.RawMessage, error) {
		command, err := commandFromPayload(commandType, payload)
		if err != nil {
			return nil, err
		}
		return s.call(ctx, command)
	}
}

// renameOp renames the session: the child gets the rename command and the projection
// follows, so GET /api/v1/sessions and the lifecycle events agree with pi.
func renameOp(ctx context.Context, s *session, payload json.RawMessage) (json.RawMessage, error) {
	var body struct {
		Name string `json:"name"`
	}
	if err := decodeInto("payload", payload, &body); err != nil {
		return nil, err
	}
	if strings.TrimSpace(body.Name) == "" {
		return nil, Codedf(CodeBadRequest, "rename needs a non-empty name")
	}
	command, err := commandFromPayload(commandTypeRename, payload)
	if err != nil {
		return nil, err
	}
	data, err := s.call(ctx, command)
	if err != nil {
		return nil, err
	}
	s.setName(body.Name)
	s.mgr.publishJSON(EventServerStatus, s.id, s.info())
	return data, nil
}

// stopOp stops the session through the same path as POST /api/v1/sessions/{id}/stop, so
// both transports share one shutdown sequence.
func stopOp(ctx context.Context, s *session, _ json.RawMessage) (json.RawMessage, error) {
	if err := s.stop(ctx); err != nil {
		return nil, err
	}
	return nil, nil
}

// rawOp is the generic passthrough of §6 and the reason a new pi command needs no server
// change: the payload is a complete pi command object and the server only assigns the id.
func rawOp(ctx context.Context, s *session, payload json.RawMessage) (json.RawMessage, error) {
	fields := map[string]json.RawMessage{}
	if err := decodeObject("payload", payload, &fields); err != nil {
		return nil, err
	}
	if _, ok := fields["type"]; !ok {
		return nil, Codedf(CodeBadRequest, "session.command.raw needs a pi command object with a type")
	}
	delete(fields, "id")
	command, err := json.Marshal(fields)
	if err != nil {
		return nil, Codedf(CodeBadRequest, "encode command: %v", err)
	}
	return s.call(ctx, command)
}
