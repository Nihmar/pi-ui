# Phase 1 spike — interface contract

**Status:** working document for the Phase 1 team. English only (see `AGENTS.md`).
**Scope reference:** `PLAN.md` §8, row "1. Spike server (Go)".

This file freezes the seams that let several workstreams implement the Phase 1 spike in
parallel. Every package named here is owned by exactly one workstream (see §13). When an
interface in this document must change, the owner reports it to the lead first: the change
lands in the same commit as the code that depends on it, and this document is updated in
that commit.

---

## 1. Scope

Phase 1 ("spike server, Go") delivers, end to end:

1. Two or more `pi --mode rpc` child processes in different working directories.
2. `RpcBridge` in Go: byte-level LF-only framing, id-based correlation, backpressure.
3. WebSocket fan-out with `hello`/`welcome` v1 negotiation and replay (`since.seq` from a
   ring buffer, `since.entryId` through `get_entries`).
4. A minimal `pi-ui-bridge` extension in TypeScript that demonstrates the extension UI
   subprotocol (`notify` + `confirm`).
5. A deterministic `fake-pi` harness with JSONL fixtures, so tests run without a model.
6. Measured numbers (spawn latency, RSS, events/second, framing correctness) with the
   acceptance matrix in §12, recorded in `docs/spike-report.md` and `docs/adr/0007-*.md`.

Because Phase 0 has not been implemented yet, this work also lands the **minimum Phase 0
slice** that the repository rules require: the JSON schemas for the touched wire surface,
their generated Go types (committed, drift-checked in CI), the Go module skeleton and the
CI workflow. The rest of Phase 0 (full DTO catalog, all ADRs, mockup plan) stays out.

## 2. Non-goals (spike)

No PTY, git, files, search, MCP, background tasks, SQLite store, real pairing/auth,
rate limiting, updates, goal mode, Flutter client. No `SessionSupervisor` watchdog policy
(backoff/respawn) beyond clean lifecycle reporting. No REST command surface beyond §5.5.

## 3. Process model

- One Go process (`pi-ui`) serves HTTP + WebSocket (`/api/v1/*`, `/ws/v1`) and owns N child
  processes, one `pi --mode rpc` per session, each with its own `cwd`, env and argv.
- Child stdout is **protocol only** (JSONL, LF-terminated). Diagnostics go to stderr and
  are forwarded to `log/slog` (never mixed into the record stream).
- The server is the only writer of child stdin; records are written under backpressure.
- Tests run the same code paths with `fake-pi` as the child command.

## 4. Module and versions

- Go module: `github.com/Nihmar/pi-ui/server`, Go 1.27, `CGO_ENABLED=0`.
- Direct dependencies: `github.com/coder/websocket`, `github.com/santhosh-tekuri/jsonschema/v6`.
- Tool dependency (codegen): `github.com/atombender/go-jsonschema` pinned in `server/tools.go`
  behind the `tools` build tag; `server/scripts/gen.sh` runs it through `go run`.
- Bridge: TypeScript sources only (pi loads them via jiti); `typescript` and
  `@earendil-works/pi-coding-agent` (exact pi version, currently `0.87.1`) are devDependencies.
## 5. Package seams

### 5.1 `internal/rpc` — child driver

```go
package rpc

// Record is one JSONL record read from the child's stdout, byte-for-byte.
type Record struct {
    Raw  json.RawMessage
    Type string // "response", "extension_ui_request", or an event type
    ID   string // correlation id when the record carries one
}

// Spec describes one child process. Dir is the session working directory.
type Spec struct {
    Command []string          // argv; argv[0] is resolved with exec.LookPath
    Dir     string
    Env     []string          // extra entries, appended to os.Environ()
    Stderr  func(line []byte) // diagnostics hook; nil discards
}

type Options struct {
    RecordBuffer int           // bounded channel size (default 1024)
    SendTimeout  time.Duration // default per-command deadline (default 15s)
    KillGrace    time.Duration // TERM→KILL grace on Close (default 5s)
}

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

var ErrClosed  = errors.New("rpc: bridge closed")
var ErrTimeout = errors.New("rpc: command timeout")
```

Rules:
- Framing reads bytes and splits on `0x0A` only (a trailing `0x0D` is stripped). Records
  containing `U+2028`/`U+2029` must survive untouched. Empty lines are skipped.
- The reader goroutine reads stdout continuously and never blocks on the consumer: it
  publishes into the bounded `RecordBuffer` channel and, when full, stops reading (which
  is pi's documented backpressure behaviour) instead of dropping records.
- `Send` correlates by `id`. A response that never arrives is `ErrTimeout`. A response
  with `success:false` is returned as-is; the caller maps it to the error taxonomy (§11).
- Child process group: `Setpgid: true` plus Linux `Pdeathsig: SIGKILL` (set while the
  thread is locked, `runtime.LockOSThread`). `Close` kills the whole group.
- `Records()` closes exactly once, after the child exits and stdout is drained.

### 5.2 `internal/ws` — hub, frames, replay

```go
package ws

type Event struct {
    Type      string          `json:"type"`              // "pi.*", "server.*", "ext.*"
    SessionID string          `json:"sessionId,omitempty"`
    Seq       uint64          `json:"seq"`
    EntryID   string          `json:"entryId,omitempty"`
    TS        string          `json:"ts"`                // RFC3339 with ms, UTC
    Payload   json.RawMessage `json:"payload,omitempty"` // verbatim pi record / server data
}

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

type Hub interface {
    http.Handler // upgrades /ws/v1
    Publish(ev Event) uint64 // fills Seq and TS when zero; fans out; returns Seq
    SetReplayer(Replayer)
    SetCommandHandler(CommandHandler)
    SetDialogHandler(DialogHandler)
    Close() error
}

// Replayer serves durable replay for `since.entryId` (implemented by sessions).
type Replayer interface {
    ReplayFromEntry(ctx context.Context, sessionID, entryID string, emit func(Event)) (complete bool, err error)
}

type Command struct {
    ID        string
    SessionID string
    Op        string // "session.prompt", "session.command.raw", ...
    Payload   json.RawMessage
}

type CommandHandler interface {
    Handle(ctx context.Context, c Command) (data json.RawMessage, err error)
}

type DialogHandler interface {
    Respond(ctx context.Context, sessionID, requestID string, response json.RawMessage) error
}
```

Rules:
- `Seq` is global and monotonic per server process, assigned in `Publish`.
- One subscriber = one bounded queue. A subscriber that cannot keep up is disconnected
  with a `server.error{code:"slow_consumer"}` frame instead of silently losing events.
- Inbound frames are validated against the embedded `ws.json` schema before acting;
  invalid frames get `response{ok:false,error:{code:"bad_request"}}` (or are dropped when
  there is no id) and never reach handlers.
- Handshake: bearer token from the `Authorization` header (never query string); with no
  token configured the peer address must be loopback; `Host` must match the request
  authority unless allow-listed; a present `Origin` must be same-authority unless
  allow-listed.
- Heartbeat: server sends `ping` every `Heartbeat`, closes the connection after three
  missed `pong`s.
- `Replay(sessionID, since)` returns buffered events with `Seq > since` for the session
  plus `truncated=true` when `since` fell out of the ring window.
### 5.3 `internal/sessions` — supervisor and event layer

```go
package sessions

type Spec struct {
    CWD        string
    Name       string            // display name; also passed to pi as --name
    Command    []string          // full argv for the child; defaults resolved from Config
    Env        map[string]string
    SessionDir string            // optional --session-dir for the child
    BridgeExt  string            // optional path passed to the child as -e <path>
}

type Status string // "spawning" | "ready" | "streaming" | "exited" | "crashed" | "stopping"

type Info struct {
    ID            string `json:"id"` // server session id, "s_" + 16 hex chars
    CWD           string `json:"cwd"`
    Name          string `json:"name,omitempty"`
    Status        Status `json:"status"`
    PID           int    `json:"pid,omitempty"`
    PiSessionID   string `json:"piSessionId,omitempty"`
    PiSessionFile string `json:"piSessionFile,omitempty"`
    ModelProvider string `json:"modelProvider,omitempty"`
    ModelID       string `json:"modelId,omitempty"`
    ThinkingLevel string `json:"thinkingLevel,omitempty"`
    ExitCode      *int   `json:"exitCode,omitempty"`
    CreatedAt     string `json:"createdAt"`
    LastEventAt   string `json:"lastEventAt,omitempty"`
}

type Config struct {
    PiCommand     []string      // e.g. ["pi","--mode","rpc"]; fake-pi in tests
    SessionDir    string        // optional base --session-dir
    BridgeExt     string        // optional -e path
    MaxSessions   int           // default 8
    DialogTimeout time.Duration // default 60s
    SendTimeout   time.Duration // default 15s
    Logger        *slog.Logger
    Hub           Publisher     // injected
}

// Publisher is the subset of ws.Hub that sessions needs (injected, never a global).
type Publisher interface {
    Publish(ev ws.Event) uint64
}

type Supervisor interface {
    Start(ctx context.Context, spec Spec) (Info, error) // ErrLimit when MaxSessions reached
    Get(id string) (Info, bool)
    List() []Info
    // Send dispatches one op ("prompt", "steer", "abort", "command.raw", ...).
    Send(ctx context.Context, sessionID, op string, payload json.RawMessage) (json.RawMessage, error)
    Stop(ctx context.Context, sessionID string) error // graceful: stdin close, TERM, KILL
    Shutdown(ctx context.Context) error
}

var ErrLimit = errors.New("sessions: limit reached")
var ErrNotFound = errors.New("sessions: session not found")
```

Rules:
- Start: spawn through `rpc.Bridge`, then `get_state` to confirm readiness; `server.spawned`
  is published immediately, `server.ready` after `get_state` returns.
- Every non-response record from the child is published as `pi.<record.type>` with the
  record as `payload` (verbatim bytes, unknown types tolerated).
- `extension_ui_request` records with `method` in `select|confirm|input|editor` are routed
  as WS `request` frames (not events); `notify|setStatus|setWidget|setTitle|set_editor_text`
  are published as `ext.notify|ext.status|ext.widget|ext.title|ext.editor_text` events with
  `{method, ...fields}` payload.
- Dialog lifecycle: broadcast to current subscribers of the session, first `ui_response`
  wins (the others receive `response{ok:false,error:{code:"already_answered"}}`), no answer
  within `DialogTimeout` writes `{"type":"extension_ui_response","id":…,"cancelled":true}`
  to the child and publishes `server.dialog.timeout{requestId,method}`.
- Exit: code 0 or explicit stop → `server.exited`; any other exit → `server.crashed`
  (both carry `exitCode`); the session stays listed with its final status.
- Durable replay: `ReplayFromEntry` calls `get_entries{since}` and emits one
  `pi.entry_appended` event per entry (`payload = {"entry": {...}}`, `entryId` set), then
  reports `complete=true`. An unknown cursor is a coded `replay_cursor_invalid` failure
  (`complete=false`), never a session event: the hub reports it to the replaying
  connection only, before `replay.end`. An unknown session is not a failure at all — both
  cursor paths answer it with an empty replay — because the subscribe may be racing the
  REST call that creates it.
- `Send` maps pi errors: `success:false` → `pi_rejected` with pi's `error` string;
  transport/timeout → `pi_error`/`timeout`. `bash_execution_update` events keep flowing as
  `pi.bash_execution_update` while the `bash` command is still pending.

### 5.4 `internal/api` — HTTP surface

```go
type Options struct {
    Supervisor sessions.Supervisor
    Hub        ws.Hub
    Info       ServerInfo // version, piVersion, features, limits
    Auth       Authenticator
}

// Authenticator is the Phase 3 seam; the spike ships LoopbackOrToken only.
type Authenticator interface {
    Authenticate(r *http.Request) (Scope, error)
}

type Scope string // "viewer" | "operator" | "admin"

func NewRouter(o Options) http.Handler // method+wildcard ServeMux patterns
```

Endpoints (spike subset, JSON, `X-Piui-Protocol: 1` echoed):
- `GET /api/v1/health` → `{"status":"ok"}` (no cwd/names).
- `GET /api/v1/server` → `{version, piVersion, protocol:1, features[], limits{}}`.
- `GET /api/v1/sessions` → `{"sessions":[Info...]}`.
- `POST /api/v1/sessions` → `{cwd, name?}` → 201 `Info`; 409 `session_limit`; 400 `bad_request`.
- `GET /api/v1/sessions/{id}` → `Info`; 404 `session_not_found`.
- `POST /api/v1/sessions/{id}/stop` → 202; 404 `session_not_found`.

### 5.5 `internal/protocol/gen` — generated types and embedded schemas

- Generated from `schemas/` by `server/scripts/gen.sh` (documented, idempotent,
  drift-checked in CI). Never edited by hand; if the codegen cannot express a construct,
  the fallback is a hand-written struct in the owning package plus validation through
  `jsonschema/v6`.
- Each schema also lands as an embedded literal: `gen.SchemaJSON("ws")` returns the raw
  schema bytes used by the hub to validate inbound frames.
- **Schemas are self-contained**: `scripts/gen.sh` runs the generator once per file, so a
  schema must not `$ref` another schema file (no cross-file `$ref`; `ws.json` re-declares
  the few shapes it shares with `core.json`). A test in `internal/protocol/gen` guards this.
- Generator caveats observed with `atombender/go-jsonschema` v0.24.1: use `additionalProperties: true`
  only on property-less open maps (else the generator imports `mapstructure`, which is not a
  module dependency); prefer a `pattern` over `format: date-time` for timestamps (the latter
  generates `time.Time`, which does not round-trip as text); `required` is enforced by the
  generated `UnmarshalJSON`, so callers that need tolerance keep the raw JSON.

### 5.6 `internal/spike` — measurements

```go
package spike

type SpawnResult struct { N int; ReadyMs []float64; P50Ms, P95Ms float64; ChildRSSMiB []float64; TotalRSSMiB float64 }
type ThroughputResult struct { Events int; DurationSec float64; EventsPerSec float64; Loss int; P50Ms, P95Ms float64 }
type ServerRSSResult struct { Sessions, Clients int; ServerRSSMiB, ChildrenRSSMiB float64; ChildRSSMiB []float64; TotalRSSMiB float64 }
type RSSSample struct { ElapsedSec, MiB float64 }
type Machine struct { CPU string; Cores int; MemTotalMiB float64; Kernel string }

func MeasureSpawn(ctx context.Context, cfg Config) (SpawnResult, error)
func MeasureServerRSS(ctx context.Context, cfg Config) (ServerRSSResult, error)
func MeasureThroughput(ctx context.Context, cfg Config) (ThroughputResult, error)
// ProcessRSSMiB reads /proc/<pid>/status VmRSS, including descendants (Linux).
func ProcessRSSMiB(pid int) (float64, error)
```

`MeasureServerRSS` is the measurement behind C2/C3 and `MeasureThroughput` the one behind
C5/C6/C9; both return the result even when the run did not settle, because the raw samples
are the evidence. `ThroughputResult` samples the end-to-end latency into a bounded
reservoir window rather than keeping one sample per event, so a soak does not grow the
memory of the process whose RSS it measures (see `docs/spike-report.md`).

### 5.7 `cmd/pi-ui` and `internal/cli`

```
pi-ui version
pi-ui serve   [--addr 127.0.0.1:8787] [--pi "pi"] [--session <cwd>[:<name>]]... [--bridge <path>] [--token ...]
pi-ui measure [--sessions 8] [--pi <path>|--fake-pi <path>] [--events N] [--rate N] [--clients N] [--out <file>]
```

- `internal/cli` is the single registration point: `cli.Register(cli.Command{Name, Summary, Run})`
  and `cli.Run(ctx, args, stdout, stderr) int`. New commands land as new files in
  `cmd/pi-ui` with a small `init()` registration, not as branches in `main.go`.
- `serve` resolves config from flags, then `PIUI_*` env vars, then defaults:
  `PIUI_ADDR`, `PIUI_PI`, `PIUI_TOKEN`, `PIUI_LOG_LEVEL`, `PIUI_MAX_SESSIONS`,
  `PIUI_REPLAY_EVENTS`, `PIUI_REPLAY_WINDOW`, `PIUI_DIALOG_TIMEOUT`, `PIUI_HEARTBEAT`,
  `PIUI_ALLOW_HOSTS`, `PIUI_ALLOW_ORIGINS`, `PIUI_RUNTIME_DIR` (default
  `$XDG_RUNTIME_DIR/pi-ui` or `os.TempDir()/pi-ui-<uid>`), `PIUI_BRIDGE` (extension path).
- Session ids: `s_` + 16 lowercase hex chars from `crypto/rand`.
- Every session with a bridge extension gets `PI_UI_BRIDGE_CONFIG=<runtime>/<sid>.json` in
  its environment (per `PLAN.md` §3.3), containing at least
  `{"sessionId":"<sid>","approvals":{"mode":"confirm|off","patterns":["rm -rf","git push --force","sudo"]}}`.

## 6. WebSocket protocol v1 (spike subset)

Endpoint `GET /ws/v1` (upgrade). All frames are JSON objects with a `type` field.
`hello` must be the first client frame; the server answers `welcome` or closes with HTTP 4401.

Client → server:

| Frame | Shape |
|---|---|
| `hello` | `{"type":"hello","v":1,"client":{"name":"pi-ui-test","version":"0.0.1"}}` |
| `subscribe` | `{"type":"subscribe","sessionId":"s_…","since":{"seq":123}\|{"entryId":"abc"},"replay":true}` |
| `unsubscribe` | `{"type":"unsubscribe","sessionId":"s_…"}` |
| `command` | `{"type":"command","id":"c1","sessionId":"s_…","op":"session.prompt","payload":{"message":"hi"}}` |
| `ui_response` | `{"type":"ui_response","sessionId":"s_…","id":"<pi request id>","value":"Allow"\|"confirmed":true\|"cancelled":true}` |
| `ping` | `{"type":"ping"}` |

Server → client:

| Frame | Shape |
|---|---|
| `welcome` | `{"type":"welcome","v":1,"server":{"version","piVersion","features":[],"limits":{}},"heartbeatSec":30}` |
| `event` | §7 |
| `request` | `{"type":"request","id","sessionId","method","title","message"?,"options"?,"placeholder"?,"prefill"?,"timeoutMs"?,"ts"}` |
| `response` | `{"type":"response","id","ok":true,"data":{…}}` or `{"type":"response","id","ok":false,"error":{"code","message"}}` |
| `pong` | `{"type":"pong"}` |

Ops implemented by the spike (`op` field): `session.prompt`, `session.steer`,
`session.follow_up`, `session.abort`, `session.clear_queue`, `session.rename`,
`session.stop`, `session.command.raw` (payload is a raw pi command object; the server
assigns the `id`). Ops are registered in one map in `internal/sessions` so Phase 3 adds
entries instead of branches.

## 7. Events and replay

- `event.type` names: `pi.<record.type>` for verbatim child records; `server.spawned`,
  `server.ready`, `server.status`, `server.exited`, `server.crashed`, `server.stopping`,
  `server.replay.begin{direction:"entry",sessionId}`, `server.replay.end{sessionId,count,complete,truncated?}`,
  `server.heartbeat{uptimeSec,sessions,clients}`, `server.dialog.timeout{requestId,method}`,
  `server.error{code,message}`; `ext.notify{message,notifyType}`, `ext.status{statusKey,statusText}`,
  `ext.widget{widgetKey,widgetLines,widgetPlacement}`, `ext.title{title}`, `ext.editor_text{text}`.
- `seq` is assigned by the hub; `ts` is RFC3339 UTC with milliseconds when the publisher
  leaves it empty.
- `entryId` is set when the record carries one (`entry_appended` → `payload.entry.id`),
  otherwise omitted; clients fall back to `seq`.
- Subscriber replay order: `server.replay.begin` → replayed events → `server.replay.end` →
  live events. Live events published during a replay are buffered per subscriber and
  flushed after `replay.end`: the ring path deduplicates by `seq`, the durable path by
  entry id (`since.entryId` stamps its events as it emits them, so a live event can carry
  a lower seq without having been replayed).
- `since.seq` replay uses the ring (2000 events / 15 min defaults): events for the session
  with `Seq > since.seq`. Out of window → `truncated:true` in `server.replay.end` and the
  client is expected to reload through REST.
- `since.entryId` replay delegates to `sessions.ReplayFromEntry`; failure to match the
  cursor produces `server.error{code:"replay_cursor_invalid"}` and `complete:false` on the
  replaying connection; an unknown session produces an empty replay instead.
- Heartbeat events are published by the server every 30 s; `ping`/`pong` frames are the
  connection-level keepalive.

## 8. Extension dialogs

- `extension_ui_request` with `method` `select|confirm|input|editor` becomes a `request`
  frame to every subscriber of the session.
- The first `ui_response` with that request id is written to the child as
  `{"type":"extension_ui_response","id":…,"value"|"confirmed"|"cancelled"}`; later
  responses receive `response{ok:false,error:{code:"already_answered"}}`.
- No response within `PIUI_DIALOG_TIMEOUT` (default 60 s) → the server writes
  `cancelled:true` to the child and publishes `server.dialog.timeout`.
- Fire-and-forget methods never produce a `request` frame and are not answerable.

## 9. `fake-pi` harness, fixtures, `fakeharness`

`server/test/fake-pi` is a standalone Go program (stdlib only) that impersonates
`pi --mode rpc` on stdio. It ignores unknown pi flags so the server can pass the real argv.

CLI (all optional):

```
fake-pi [--script FILE] [--emit N] [--rate R] [--big BYTES] [--crlf] [--separators]
        [--stderr LINES] [--stall-ms MS] [--crash-after MS] [--exit-after MS] [--ignore-stdin]
```

Script format (JSON):

```json
{
  "sessionId": "fake-session-id",
  "sessionFile": "/tmp/fake-session.jsonl",
  "entries": [ { "type": "message", "id": "e1", "message": { "role": "user" } } ],
  "startup": [ { "delayMs": 0, "record": { "type": "extension_ui_request", "id": "u1", "method": "notify", "message": "ready" } } ],
  "commands": {
    "prompt": { "response": { "type": "response", "command": "prompt", "success": true },
                "events": [ { "delayMs": 1, "record": { "type": "message_update", "assistantMessageEvent": { "type": "text_delta", "delta": "hi" } } } ] },
    "bash":   { "events": [ { "delayMs": 1, "record": { "type": "bash_execution_update", "delta": "out" } } ] }
  },
  "default": { "response": { "type": "response", "success": true } },
  "faults": { "stallMs": 0, "crashAfterMs": null, "exitAfterMs": null }
}
```

Rules:
- Responses are auto-filled with the request `id` and `command` when the script omits them.
- Built-in defaults when no script/command entry matches: `get_state` returns a canned
  `RpcSessionState`, `get_entries` returns `entries` (paged by `since`), `get_commands`
  returns `{commands:[]}`, everything else `success:true` with `{}`.
- `--emit N --rate R` (after a `prompt`): N synthetic `message_update` records at R/s,
  then `agent_end` + `agent_settled`; each payload carries
  `"spike":{"i":<n>,"ns":<time.Now().UnixNano()>}` so tests can measure latency.
- `--big BYTES`: one `message_update` whose `delta` is that many bytes.
- `--separators`: embed `U+2028` and `U+2029` inside a record; `--crlf`: terminate records
  with CRLF; `--stderr LINES`: write LINES to stderr before the first record.
- Exit on stdin EOF (like pi); `--exit-after`/`--crash-after` exit with 0/9.

`server/test/fakeharness` is the Go helper imported by tests of every package:

```go
package fakeharness

func Build(t testing.TB) string                       // builds test/fake-pi once, returns binary path
func WriteScript(t testing.TB, s Script) string       // writes the JSON script, returns its path
func Command(scriptPath string, extra ...string) []string // fake-pi argv, ready for rpc.Spec

type Script struct {
    SessionID   string                     `json:"sessionId,omitempty"`
    SessionFile string                     `json:"sessionFile,omitempty"`
    Entries     []json.RawMessage          `json:"entries,omitempty"`
    Startup     []Step                     `json:"startup,omitempty"`
    Commands    map[string]CommandScript   `json:"commands,omitempty"`
    Default     *CommandScript             `json:"default,omitempty"`
    Faults      Faults                     `json:"faults,omitempty"`
}
type Step struct {
    DelayMs int             `json:"delayMs,omitempty"`
    Record  json.RawMessage `json:"record"`
}
type CommandScript struct {
    DelayMs  int             `json:"delayMs,omitempty"`
    Response json.RawMessage `json:"response,omitempty"`
    Events   []Step          `json:"events,omitempty"`
    Error    string          `json:"error,omitempty"`
}
type Faults struct {
    StallMs      int  `json:"stallMs,omitempty"`
    CrashAfterMs *int `json:"crashAfterMs,omitempty"`
    ExitAfterMs  *int `json:"exitAfterMs,omitempty"`
}
```

- `test/fixtures/*.jsonl` are recorded from the installed pi 0.87.1; each file starts with a
  `#` comment line naming the exact command used and the capture date, and the capture
  script (`server/scripts/capture-fixtures.sh`) is committed next to it. Hand-authored
  fixtures are allowed when the real capture needs a model, and must say so in the header.
- Fixture-driven tests must run without network and without a model.

## 10. `bridge/` — minimal `pi-ui-bridge`

- `bridge/pi-ui-bridge.ts` default-exports `(pi: ExtensionAPI) => void`; loaded by pi with
  `-e <path>`; jiti executes the TypeScript sources directly (no build step).
- Behaviour (spike scope):
  1. `session_start` → `ctx.ui.notify("pi-ui-bridge ready", "info")` (proves the
     fire-and-forget path end to end).
  2. `tool_call` for `bash`: if the command matches a pattern in
     `PI_UI_BRIDGE_CONFIG.approvals.patterns` and `ctx.hasUI`, ask
     `ctx.ui.confirm(title, message)`; a negative answer returns
     `{ block: true, reason: "blocked by pi-ui-bridge" }` and a `ctx.ui.notify(..., "warning")`.
  3. Missing/invalid config → approvals `off`, still emits the startup notify.
- Config JSON path comes from `PI_UI_BRIDGE_CONFIG` (see §5.7); parsing is defensive: any
  error logs to stderr and degrades to `off`.
- `bridge/package.json` pins `typescript` and `@earendil-works/pi-coding-agent@0.87.1` as
  devDependencies with a `typecheck` script (`tsc --noEmit`); sources stay runtime-free of
  Node APIs the pi runtime does not provide.
- `bridge/README.md` documents loading (`pi --mode rpc -e bridge/pi-ui-bridge.ts`), the
  config schema and the degradation rules.

## 11. Error taxonomy used by the spike

`unauthorized`, `forbidden_scope`, `not_found`, `session_not_found`, `session_limit`,
`bad_request`, `busy_streaming`, `pi_rejected`, `pi_error`, `timeout`, `already_answered`,
`slow_consumer`, `replay_cursor_invalid`, `internal` (see `PLAN.md` §4.5 for HTTP mappings).
REST errors are `{"error":{"code","message"}}`; WS errors travel in `response.error` or as
`server.error` events. The spike implements the subset above and maps everything else to
`internal`.

## 12. Measurement and acceptance matrix

All numbers are produced on the development host and recorded in `docs/spike-report.md`
with raw samples; the ADR states pass/fail per criterion.

| # | Criterion | Threshold |
|---|---|---|
| C1 | `pi` child spawn → `get_state` ready (real pi, 8 samples) | p95 ≤ 5 000 ms |
| C2 | Go server RSS, idle + 2 sessions + 1 WS client | ≤ 128 MiB |
| C3 | Go server RSS, 8 sessions | ≤ 192 MiB |
| C4 | Per-child RSS (real pi, idle) | reported; sum of 8 ≤ 2 400 MiB |
| C5 | fake-pi → bridge → hub → 1 WS client throughput | ≥ 5 000 events/s sustained 30 s, loss = 0 |
| C6 | End-to-end event latency under load (p95) | ≤ 300 ms |
| C7 | Framing correctness: U+2028/U+2029, CRLF, 8 MiB record, split reads | 100 % of records intact, no panic |
| C8 | Shutdown: SIGTERM to server reaps all children | ≤ 2 s, zero orphans |
| C9 | Memory stability: 3 min soak at ~2 000 events/s | Go RSS growth ≤ 10 % after warm-up |

Fallback trigger (documented in ADR 0007): failing C2+C3 together, or C5 by more than 2×,
means the Go spike does not meet the criteria and the Python fallback is evaluated before
Phase 3.

## 13. Ownership, verification and commit protocol

| Workstream | Owned paths |
|---|---|
| lead | `docs/spike-interfaces.md`, `.gitignore`, final history review |
| contracts | `server/go.mod`, `server/go.sum`, `server/tools.go`, `server/Makefile`, `server/scripts/gen.sh`, `schemas/core.json`, `schemas/pi.json`, `server/internal/protocol/gen/**`, `.github/workflows/ci.yml`, module skeleton |
| rpc | `server/internal/rpc/**` |
| fakepi | `server/test/fake-pi/**`, `server/test/fakeharness/**`, `server/test/fixtures/**`, `server/scripts/capture-fixtures.sh` |
| ws | `schemas/ws.json`, `server/internal/protocol/gen/ws*.go`, `server/internal/ws/**`, `docs/ws-protocol.md` |
| sessions | `server/internal/sessions/**`, `server/internal/api/**`, `server/internal/cli/**`, `server/cmd/pi-ui/**` |
| bridge | `bridge/**` |
| measure | `server/internal/spike/**`, `server/scripts/measure.sh`, `server/test/e2e/**`, `docs/adr/0007-*.md`, `docs/spike-report.md`, `README.md` |
| verify | `server/test/adversarial/**`, `docs/verification-report.md` |

Commit protocol (from `AGENTS.md`, non-negotiable):

1. Stage only your owned paths (`git add <paths>`); never `git add -A`; never commit
   `PLAN.md`, node_modules, build outputs or runtime state.
2. Run the area checks before committing: server →
   `gofmt -l . && go vet ./... && go test -race ./... && go build ./...` from `server/`;
   bridge → `npm run typecheck` from `bridge/`.
3. One commit per coherent change, message `area: imperative summary` in English, no
   co-author trailer, no AI attribution. Include schema/doc updates in the same commit as
   the code that needs them.
4. If a commit fails because the tree is mid-flight in another workstream, fix your part or
   wait for the dependency — never commit a red suite.
5. Report blockers through the team task list and the lead; keep interface changes in
   lockstep with this document.






