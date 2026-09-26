package audit

import "time"

// Action is the dotted name of an audited action. Unknown names are accepted and
// stored as they are (bounded in length), so a new call site never waits for a schema
// change; the constants below are the vocabulary the server itself uses.
type Action string

// The actions the server records today (PLAN.md §4.7).
const (
	// ActionAuthPair is a successful pairing.
	ActionAuthPair Action = "auth.pair"
	// ActionAuthDenied is a refused credential or a missing scope: the policy
	// decisions a review looks for first.
	ActionAuthDenied Action = "auth.denied"
	// ActionDeviceRevoke is an immediate device revocation.
	ActionDeviceRevoke Action = "device.revoke"

	// ActionSessionCreate spawns a session.
	ActionSessionCreate Action = "session.create"
	// ActionSessionStop shuts one down through the stop endpoint.
	ActionSessionStop Action = "session.stop"
	// ActionSessionDelete forgets one (Phase 3 delete endpoint).
	ActionSessionDelete Action = "session.delete"
	// ActionSessionPrompt sends a prompt.
	ActionSessionPrompt Action = "session.prompt"
	// ActionSessionSteer queues a steering message.
	ActionSessionSteer Action = "session.steer"
	// ActionSessionFollowUp queues a follow-up.
	ActionSessionFollowUp Action = "session.follow_up"
	// ActionSessionAbort aborts the run.
	ActionSessionAbort Action = "session.abort"
	// ActionSessionBash runs a direct bash command.
	ActionSessionBash Action = "session.bash"
	// ActionSessionRename renames a session.
	ActionSessionRename Action = "session.rename"
	// ActionSessionClearQueue drops the queued messages.
	ActionSessionClearQueue Action = "session.clear_queue"
	// ActionSessionCommand is an op outside the known vocabulary: the raw op travels
	// in the details, so the action column stays a bounded set.
	ActionSessionCommand Action = "session.command"

	// ActionTerminalOpen opens a PTY terminal.
	ActionTerminalOpen Action = "terminal.open"
	// ActionTerminalClose closes one.
	ActionTerminalClose Action = "terminal.close"
	// ActionFileWrite writes a host file.
	ActionFileWrite Action = "file.write"
	// ActionFileDelete deletes one.
	ActionFileDelete Action = "file.delete"
	// ActionGitWrite is any mutating git operation.
	ActionGitWrite Action = "git.write"
	// ActionMcpUpdate changes the MCP configuration.
	ActionMcpUpdate Action = "mcp.update"
	// ActionUpdatesApply applies server, pi or package updates.
	ActionUpdatesApply Action = "updates.apply"
	// ActionSettingsUpdate changes server settings.
	ActionSettingsUpdate Action = "settings.update"
	// ActionDrainStart quiesces the server.
	ActionDrainStart Action = "drain.start"
	// ActionDrainResume resumes it.
	ActionDrainResume Action = "drain.resume"
	// ActionPathEscapeBlocked is a path that tried to leave its root.
	ActionPathEscapeBlocked Action = "path.escape.blocked"
	// ActionRateLimited is a request refused by a rate limit.
	ActionRateLimited Action = "rate.limited"
)

// Outcome is how an audited action ended.
type Outcome string

const (
	// OutcomeOK means the action happened.
	OutcomeOK Outcome = "ok"
	// OutcomeDenied means it was refused before doing anything (credential or scope):
	// a policy decision, not an incident.
	OutcomeDenied Outcome = "denied"
	// OutcomeError means it was attempted and failed.
	OutcomeError Outcome = "error"
)

// Valid reports whether the outcome is one of the three the wire defines.
func (o Outcome) Valid() bool {
	switch o {
	case OutcomeOK, OutcomeDenied, OutcomeError:
		return true
	default:
		return false
	}
}

// Event is one row of the trail: who did what, to what, with which outcome.
type Event struct {
	// ID is the monotonic row id; zero means "not stored yet".
	ID int64
	// At is when the server observed the action; zero means now.
	At time.Time
	// Action is the dotted action name.
	Action Action
	// Outcome is how it ended.
	Outcome Outcome
	// ActorDeviceID is the device that caused it, when the request carried one.
	ActorDeviceID string
	// ActorName is the device name at the time, copied so a rename does not rewrite
	// history.
	ActorName string
	// ActorScope is the scope the actor was authenticated with.
	ActorScope string
	// SessionID is the session the action concerned, when it concerned one.
	SessionID string
	// Target is what the action acted on when that is not a session (a device id, a
	// path, a component).
	Target string
	// RemoteAddr is the peer host of the request, never a forwarded header.
	RemoteAddr string
	// Details is the structured context of the action; it never carries secrets.
	Details map[string]any
}

// Filter selects a page of the trail, newest first.
type Filter struct {
	// Since keeps entries strictly newer than it; zero means "no lower bound".
	Since time.Time
	// Action keeps one action when set.
	Action Action
	// DeviceID keeps one actor when set.
	DeviceID string
	// SessionID keeps one session when set.
	SessionID string
	// Limit is the page size; the caller clamps it to the configured maximum.
	Limit int
}

// Store persists the trail. MemoryStore ships for tests; the SQLite implementation
// lives in internal/store behind this interface.
type Store interface {
	// Append stores one event and returns its row id.
	Append(ev Event) (int64, error)
	// Query returns the newest matching events first, at most Filter.Limit of them.
	Query(filter Filter) ([]Event, error)
	// Prune deletes every event older than before and reports how many went.
	Prune(before time.Time) (int, error)
}

// Recorder is what the transports hold: one method, best effort, never a failure the
// caller has to handle.
type Recorder interface {
	// Record stores one event. A failure is logged, never returned: an audit store
	// that is down must not break the request it is describing.
	Record(ev Event)
}
