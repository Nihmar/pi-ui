package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// Event is one entry of the fan-out stream: either a verbatim child record
// ("pi.*"), a fact the server observed about a session ("server.*") or a
// fire-and-forget extension notification ("ext.*").
//
// It is the only type sessions publishes through, so it stays flat and cheap to
// marshal: one JSON object per event, no envelope, no per-type schema. Payload is
// kept as raw bytes so a pi record reaches the client byte for byte, with the
// fields this server does not understand intact.
type Event struct {
	Type      string          `json:"type"`                // "pi.*", "server.*", "ext.*"
	SessionID string          `json:"sessionId,omitempty"` // empty for server-wide facts
	Seq       uint64          `json:"seq"`                 // assigned by Publish when 0
	EntryID   string          `json:"entryId,omitempty"`   // pi session entry, when the record has one
	TS        string          `json:"ts"`                  // RFC3339 with ms, UTC; filled by Publish when empty
	Payload   json.RawMessage `json:"payload,omitempty"`   // verbatim pi record / server data
}

// Event names the hub itself emits while serving connections. They are exported
// so sessions, api and the tests do not have to spell them out and cannot drift
// from the protocol document by a typo.
const (
	// EventReplayBegin opens a replay on one subscription, payload
	// {"direction":"seq"|"entry"}; it is written only to the subscriber that
	// asked for the replay.
	EventReplayBegin = "server.replay.begin"
	// EventReplayEnd closes a replay on one subscription, payload
	// {"count":N,"complete":bool,"truncated":bool}.
	EventReplayEnd = "server.replay.end"
	// EventHeartbeat is the periodic "the server is alive" observation, payload
	// {"uptimeSec":N,"clients":N,"sessions":N}.
	EventHeartbeat = "server.heartbeat"
	// EventError reports a failure that belongs to a connection rather than to a
	// request, payload {"code":"...","message":"..."}.
	EventError = "server.error"
	// EventEntryAppended is the pi record whose payload carries the entry id the
	// hub copies into Event.EntryID, so clients get a durable replay cursor
	// without parsing pi's payload shape themselves.
	EventEntryAppended = "pi.entry_appended"
)

// Options tunes one hub. Every zero field is replaced by its default in New, so
// Options{} is a valid spike configuration. The slices and the map are copied:
// mutating them afterwards does not affect a running hub.
type Options struct {
	Token         string        // bearer token; empty => loopback-only handshake
	AllowHosts    []string      // extra Host values accepted
	AllowOrigins  []string      // extra Origin values accepted
	ReplayEvents  int           // ring size (default 2000)
	ReplayWindow  time.Duration // ring age (default 15m)
	Heartbeat     time.Duration // server ping interval (default 30s)
	WriteTimeout  time.Duration // per-frame write deadline (default 10s)
	SendBuffer    int           // per-subscriber queue (default 512)
	ServerVersion string
	PiVersion     string
	Features      []string
	Limits        map[string]any
}

// Hub is the WebSocket seam of the server: /ws/v1, the event fan-out, replay and
// the handler slots the rest of the process fills in.
//
// It is an http.Handler so the router mounts it with mux.Handle("/ws/v1", hub);
// ServeHTTP itself answers only GET /ws/v1 and rejects everything else, so a
// wrong mount cannot turn into a silently accepted socket.
type Hub interface {
	http.Handler             // upgrades /ws/v1
	Publish(ev Event) uint64 // fills Seq and TS when zero; fans out; returns Seq
	SetReplayer(Replayer)
	SetCommandHandler(CommandHandler)
	SetDialogHandler(DialogHandler)
	Close() error
}

// Requester is the outbound half of the dialog seam: sessions writes extension UI
// `request` frames through it.
//
// It is a separate interface, and not a method on Hub, so the frozen Hub surface
// above stays exactly as agreed in docs/spike-interfaces.md §5.2; the value
// returned by New implements both, and a caller that needs the dialog path
// asserts:
//
//	requester, ok := hub.(ws.Requester)
//
// A request frame is not an event: it carries no seq, it is never replayed, and
// it is answered by a ui_response with the same id. Routing it through Publish
// would have made a blocking dialog look like a log line, which is why it gets
// its own method and its own interface.
type Requester interface {
	// SendRequest writes frame — a complete `request` object, validated against
	// schemas/ws.json before it goes out — to every connection currently
	// subscribed to sessionID. It returns an error only when the frame is
	// malformed or the hub is closed: a session nobody is watching is not an
	// error, because the spike does not queue dialogs for clients that are not
	// there.
	SendRequest(sessionID string, frame json.RawMessage) error
}

// Replayer serves durable replay for `since.entryId` (implemented by sessions).
//
// emit is called synchronously, in order, and must not be used after
// ReplayFromEntry returns; the hub assigns Seq and TS to events that arrive
// without them. complete=false means the cursor could not be honoured (the entry
// is unknown, for example) and reaches the subscriber as complete=false in
// server.replay.end.
type Replayer interface {
	ReplayFromEntry(ctx context.Context, sessionID, entryID string, emit func(Event)) (complete bool, err error)
}

// Command is one client `command` frame, already validated against the schema.
type Command struct {
	ID        string
	SessionID string
	Op        string // "session.prompt", "session.command.raw", ...
	Payload   json.RawMessage
}

// CommandHandler executes one Command. The returned data becomes the `data` of
// the response frame; an error becomes response{ok:false} with the code the error
// reports through ErrorCode() (see codeOf in errors.go), defaulting to
// "internal".
type CommandHandler interface {
	Handle(ctx context.Context, c Command) (data json.RawMessage, err error)
}

// DialogHandler answers one extension UI dialog. response is the minimal answer
// object the client sent — {"value":…}, {"confirmed":bool} or {"cancelled":bool}
// — and requestID is the pi request id of the `request` frame being answered.
type DialogHandler interface {
	Respond(ctx context.Context, sessionID, requestID string, response json.RawMessage) error
}
