# WebSocket protocol v1 — spike subset

**Status:** implemented by `server/internal/ws` (Phase 1 spike). The normative contract
for the seams is `docs/spike-interfaces.md` §5.2, §6, §7, §8; the normative frame
shapes are `schemas/ws.json`, which is generated-into and validated by the hub.
Where this document and the schema disagree, the schema wins; where the schema and
§5.2 disagree, §5.2 wins and the schema is fixed in the same commit.

Everything here is the **spike subset**: one endpoint, six client frames, five server
frames, one JWT-less bearer token, in-memory plus delegated replay. Terminal, git,
MCP, background tasks and the SQLite store are not part of it (§2 of the contract).

## Endpoint

`GET /ws/v1` (Upgrade: websocket). The router mounts the hub with
`mux.Handle("/ws/v1", hub)`; `ServeHTTP` itself answers only that path and only `GET`,
so a wrong mount fails loudly instead of accepting a socket. Any other path is `404`,
any other method is `405`.

Text frames only. A binary frame is ignored (the read limit still bounds it); it is not
a reason to drop a working connection. Inbound frames are capped at **1 MiB**, which is
generous for the largest legitimate frame (a command carrying a pasted message).

## Handshake

```
client                                  server
  ├─ HTTP GET /ws/v1 ───────────────────►│  host / origin / token checks
  │                                       │  → 401 + {"error":{"code":"unauthorized",…}} if refused
  │◄──────────────── 101 Switching Protocols
  ├─ {"type":"hello","v":1,"client":{…}} ─►│  schema + version check
  │                                       │  → close 4401 if hello is missing or wrong
  │◄──────────── {"type":"welcome","v":1,…}│
  ├─ subscribe / command / ui_response / ping …
```

- `hello` **must** be the first frame and `v` must be `1`. The server answers
  `welcome` with the same version, or closes with **4401** — the same number the
  contract names, in the only place it is legal (see below).
- The first frame is read under a 10 s deadline, so a client that upgrades and then
  says nothing cannot hold a connection slot.
- A second `hello` is ignored: nothing can be renegotiated on a live connection.

### Why a refused handshake answers HTTP 401 and not 4401

`docs/spike-interfaces.md` §5.2 says a refused handshake closes with HTTP 4401. That is
not implementable: RFC 9110 defines status codes as exactly three digits, Go's
`net/http` panics on `WriteHeader(4401)` (`invalid WriteHeader code 4401`), and a Go
client parsing a hand-written `HTTP/1.1 4401 …` response line rejects it as a malformed
status code. The hub therefore splits it by phase:

| Phase | Failure | Result |
|---|---|---|
| before the upgrade | missing/wrong token, non-loopback peer with no token configured, Host or Origin not allowed | `401 Unauthorized`, body `{"error":{"code":"unauthorized","message":"…"}}` (the §11 shape) |
| after the upgrade | first frame is not `hello`, `hello` does not match the schema, or `v != 1` | WebSocket close status **4401** (private-use range, 4000–4999) with a reason |

Both carry the same meaning and the same code (`unauthorized` / 4401) to the client; a
client should treat either as "re-handshake, do not retry with the same credentials".

## Authentication and origin rules

| Input | Rule |
|---|---|
| token | `Token` in `Options`; a request must carry it as `Authorization: Bearer <token>` (a bare `<token>` value is also accepted). |
| query string | **never** consulted. A `?token=` in the URL is not a credential: URLs leak into logs, shell history and `Referer` headers. |
| no token configured | the peer address must be loopback (`127.0.0.0/8`, `::1`). An `Authorization` header is then ignored, because the server has nothing to compare it against. |
| `Host` | must be the authority this connection really arrived on (a loopback name on the same port is accepted when the server is bound to a wildcard address). Any other name must be listed in `Options.AllowHosts`. This is the DNS-rebinding guard: a page on `evil.example` that resolves to this server sends `Host: evil.example`. |
| `Origin` | a *present* `Origin` must be the same authority as the request, otherwise it must be listed in `Options.AllowOrigins`. A missing `Origin` is accepted: browsers always send one for WebSocket handshakes, the Flutter client and the test harness do not. `Origin: null` is refused (it carries no authority to compare). |
| `Options.AllowHosts` / `Options.AllowOrigins` | allow-list entries are compared case-insensitively. A Host entry without a port matches every port of that host (`pi-ui.local` covers `pi-ui.local:8787`). An Origin entry may be a complete origin (`http://localhost:5173`) or a bare authority. There is deliberately **no** wildcard entry. |

A refusal always carries the reason in the body, so an operator can diagnose it without
weakening the check: the same status and code go out for every failure mode.

## Client frames

Every frame is a JSON object with a `type` field. Unknown fields are **preserved and
ignored** — a frame from a newer client is not rejected because of a field this server
does not know. Every inbound frame is validated against `schemas/ws.json` before any
handler sees it.

| Frame | Shape | Notes |
|---|---|---|
| `hello` | `{"type":"hello","v":1,"client":{"name":"pi-ui-test","version":"0.0.1"}}` | First frame only. `client.name` is used for diagnostics. |
| `subscribe` | `{"type":"subscribe","sessionId":"s_…","since":{"seq":123}\|{"entryId":"abc"},"replay":true}` | Registers the connection as a subscriber. `since` and `replay` are optional. |
| `unsubscribe` | `{"type":"unsubscribe","sessionId":"s_…"}` | Idempotent, including for a session never subscribed to. |
| `command` | `{"type":"command","id":"c1","sessionId":"s_…","op":"session.prompt","payload":{…}}` | `id` is client-chosen and echoed. `payload` is optional (ops without arguments). |
| `ui_response` | `{"type":"ui_response","sessionId":"s_…","id":"<pi request id>","value":"Allow"}` | Exactly one of `value`, `confirmed`, `cancelled`. `id` is the dialog's request id, not a client id. |
| `ping` | `{"type":"ping"}` | Answered immediately with `pong`. |

A frame that fails validation:

- with a readable `id` → `response{ok:false,error:{code:"bad_request",…}}`, so a client
  never waits forever for an answer to a frame it sent;
- without an id → dropped. There is nothing to correlate an answer with, and the
  connection stays up.

## Server frames

| Frame | Shape | Notes |
|---|---|---|
| `welcome` | `{"type":"welcome","v":1,"server":{"version","piVersion","features":[],"limits":{}},"heartbeatSec":30}` | Once per connection. `heartbeatSec` is rounded up to at least 1. |
| `event` | `{"type":"pi.message_update","sessionId":"s_…","seq":42,"entryId":"e-1","ts":"…","payload":{…}}` | The event stream; see below. |
| `request` | `{"type":"request","id":"u1","sessionId":"s_…","method":"confirm","title":"Bash","message":"run?","ts":"…"}` | A blocking extension dialog. `method` is one of `select`, `confirm`, `input`, `editor`. |
| `response` | `{"type":"response","id":"c1","ok":true,"data":{…}}` or `{"type":"response","id":"c1","ok":false,"error":{"code","message"}}` | One per request id, including for frames that failed validation. |
| `pong` | `{"type":"pong"}` | Answer to `ping`. |

## Operations (`op` of a `command`)

Ops are registered in one map in `internal/sessions`, not in the hub: the hub validates
the *shape* of `op` (a dotted lowercase name) and forwards it. The spike implements
`session.prompt`, `session.steer`, `session.follow_up`, `session.abort`,
`session.clear_queue`, `session.rename`, `session.stop` and `session.command.raw`
(payload is a raw pi command object; the server assigns the pi-side `id`). An unknown op
is answered by the handler with a coded error — never a dropped frame — so adding an op
is a new map entry, not a hub change.

## Events

`event.type` is namespaced and stable:

| Namespace | Meaning | Examples |
|---|---|---|
| `pi.*` | verbatim child record (`pi.<record.type>`), payload is the record byte for byte | `pi.message_update`, `pi.entry_appended`, `pi.agent_end` |
| `server.*` | a fact the server observed about a session or itself | `server.spawned`, `server.ready`, `server.status`, `server.stopping`, `server.exited`, `server.crashed`, `server.dialog.timeout`, `server.error` |
| `ext.*` | fire-and-forget extension UI notification (never answerable) | `ext.notify`, `ext.status`, `ext.widget`, `ext.title`, `ext.editor_text` |

A client must ignore an unknown event name: that is what lets the server ship new pi
events without a protocol bump.

A child line that is not valid JSON cannot travel byte for byte, so it becomes
`pi.unknown` with payload `{"raw":"<line>"}` — readable on the wire instead of a
connection-scoped `server.error`. When the line is not valid UTF-8 the payload also
carries `rawBase64` with the exact bytes, because a JSON string would replace every
invalid byte with `U+FFFD`; a client that needs the record reconstructs it from there.

- `seq` is assigned by the hub from one global counter (starting at 1), so one number
  orders every stream a client sees. It is deliberately **not** per session: a session
  stream may have gaps, and a cursor is only ever compared against the same session's
  events.
- `ts` is RFC3339 UTC with milliseconds, filled by the hub when the publisher leaves it
  empty.
- `entryId` is set when the record carries one (`pi.entry_appended` →
  `payload.entry.id`, derived by the hub) and omitted otherwise.
- `sessionId` is absent only for server-wide facts (heartbeat, startup diagnostics).
  Those are delivered to **every** connected client and are never replayable.

Events the hub itself publishes:

| Event | Payload | Emitted |
|---|---|---|
| `server.heartbeat` | `{"uptimeSec","clients","sessions"}` | every `Heartbeat` (default 30 s), server-wide |
| `server.replay.begin` | `{"direction":"seq"\|"entry"}` | once per replay, to the replaying subscriber only |
| `server.replay.end` | `{"count","complete","truncated?"}` | once per replay, to the replaying subscriber only |
| `server.error` | `{"code","message"}` | connection-scoped failures: `slow_consumer`, replay failures, an event payload that could not be encoded |

The session lifecycle events (`server.spawned`, `server.exited`, …) are published by
`internal/sessions`, not by the hub.


## Subscriptions and replay

`subscribe` registers one connection as a subscriber of one session. Replay is
per **subscriber**, never shared: two clients resuming from different cursors do not
see each other's gap.

Subscribing to a session the server does not know is **not** an error: the server
cannot tell a race (a client that subscribes before the REST call that creates the
session has been processed) from a bug, so the subscription is accepted, its replay is
empty (`count:0`), and it starts receiving events as soon as the session publishes any.
This holds for both cursor paths: an unknown session answers `since.entryId` with the
same empty replay, never a failure. The client learns about a session that really does
not exist from the REST surface, which is the only place that can answer the question.

### Without a cursor

`{"type":"subscribe","sessionId":"s_…"}` (or with `replay:true` and no `since`) replays
whatever the ring still holds for that session and reports `truncated` as absent: the
client never claimed to have anything, so nothing was lost. `replay` never defaults a
cursor into existence — a live-only subscriber sends `"replay":false`, which suppresses
the whole replay sequence.

### `since.seq` — the in-memory ring

`{"since":{"seq":123}}` returns the session's retained events with `Seq > 123`:

```
server.replay.begin {direction:"seq"} → <events> → server.replay.end {count,complete,truncated?}
```

- Each session has its **own** ring: the last `ReplayEvents` events of that session
  (default 2000), forgetting events older than `ReplayWindow` (default 15 m) by the
  moment the hub accepted them, not by their `ts` (a replayed event may carry an old
  timestamp). A busy session therefore cannot shrink a quiet one's replay window, and
  server-wide events are never kept: they have no session to replay into. A ring that has
  no subscriber and nothing replayable left (every entry aged past `ReplayWindow`) is
  released, so a long-running server that churns sessions does not keep one ring per
  session forever.
- `truncated:true` means events of **this** session with a `seq` above the cursor were
  evicted or aged out. The client is expected to reload the session through REST instead
  of rendering a stream with a hole in it. `complete` is `true` here: the server did
  everything the cursor allowed.
- A cursor at or ahead of the newest seq replays nothing and is not truncated, and neither
  is a session whose first event happens to carry a high global seq because other sessions
  published before it: nothing was lost, so nothing is reported as lost.

### `since.entryId` — durable replay

`{"since":{"entryId":"abc"}}` delegates to the injected `Replayer` (implemented by
`internal/sessions` with `get_entries`):

```
server.replay.begin {direction:"entry"} → <events from the Replayer> → server.replay.end {count,complete}
```

- The Replayer emits one `pi.entry_appended` per entry; the hub stamps what the emitter
  left empty (`seq` from the same global counter, so the client's cursor stays meaningful
  after a reconnect, and `ts`), assigns the `sessionId` and sets `entryId` from the
  payload when the emitter did not.
- Replayed events are delivered to that subscriber only and do **not** enter the ring: a
  replayed entry is history, and writing it back would let every other subscriber replay
  it a second time.
- An unknown cursor yields `server.error{code:"replay_cursor_invalid"}` and
  `complete:false`, so the client reloads through REST instead of rendering a partial
  history. The Replayer returns that failure as an error — sessions does, because it knows
  which pi rejection means "cursor unknown" — and the hub reports it with the Replayer's
  own `ErrorCode()` to the replaying connection, before `server.replay.end`; the failure
  never enters the session event stream. Without a Replayer configured the answer is
  `server.error{code:"unsupported"}` + `complete:false` — never a silent empty replay.

### Ordering, buffering and dedup

1. `server.replay.begin`
2. the replayed events
3. `server.replay.end`
4. live events

Live events published for that session **while its replay runs** are buffered per
subscriber and flushed after `replay.end`. How the flush orders and dedups them depends
on the cursor, because the two replay paths do not produce seqs the same way:

- **`since.seq`** — the replayed events come from the ring and already carry their seq,
  all of them lower than anything published during the replay. The flush sorts by `seq`
  and drops what the subscriber already received (`seq <= lastSent`), so the stream stays
  monotonic and duplicate-free.
- **`since.entryId`** — the hub stamps the replayed events as it emits them, so a live
  event published while the replay ran can carry a *lower* seq than the last replayed one
  even though the replay never covered it. A seq cursor would silently drop it, so the
  flush deduplicates by **entry id** instead: a buffered `pi.entry_appended` whose id the
  replay already wrote is dropped, every other buffered event is delivered in arrival
  order. The replay therefore does not promise a monotonic `seq` across the boundary —
  `seq` resumes the ring, `entryId` is the durable cursor — but it does promise that no
  event is lost.

The entry ids compared are the last `SendBuffer` the replay wrote, which is where an
overlap with the buffer can live: the buffer is capped by `SendBuffer` and the replay is
chronological, so a duplicate can only be one of its last entries.

A subscriber that re-subscribes to the same session restarts its replay.

Buffered events are capped by `SendBuffer`: a client that cannot even consume its own
catch-up is disconnected like any other slow consumer.

## Flow control

- One subscriber, one bounded queue (`SendBuffer`, default 512 frames) and one writer
  goroutine per connection. Publishers never block on a client.
- A queue that overflows drops the backlog and queues a `server.error{code:"slow_consumer"}`
  frame before closing the connection with WebSocket status `1008` (policy violation). The
  hub drops the subscriber immediately: it stops being counted and stops receiving events,
  whether or not the socket finishes closing.
- A peer that never reads cannot be told anything either; in that case the close status is
  the only signal it will find when it eventually reads.
- Every frame write has a deadline (`WriteTimeout`, default 10 s). A write that times out
  ends the connection.


## Keepalive

Two independent mechanisms, and they do different jobs:

| Mechanism | Direction | Purpose |
|---|---|---|
| WebSocket protocol ping/pong | server → client, every `Heartbeat` | liveness: **three** consecutive unanswered pings close the connection with `1008`. It is a control frame, so it is invisible to the frame schema and every standards-compliant client (a browser included) answers it without application code. |
| `{"type":"ping"}` / `{"type":"pong"}` | client → server | application-level liveness a client can issue whenever it wants, e.g. while it has no subscription. |
| `server.heartbeat` event | server → every client, every `Heartbeat` | the observation a client renders: uptime and the live client/session counts. |

Because protocol pings are answered by the client's *reader*, a client that never reads
its socket is closed after three intervals — a half-open TCP connection cannot hold a
subscriber slot forever.

## Errors

Failures reach a client in three shapes, all with the same error object
(`{"code","message"}`) and the codes of `schemas/core.json`:

| Where | Meaning |
|---|---|
| `response{ok:false,error}` | terminal outcome of a `command` or `ui_response`; the `id` correlates it |
| `server.error` event | connection-scoped failure: `slow_consumer`, replay failures, an unencodable event payload |
| handshake refusal (401 / close 4401) | the connection was never established, or never negotiated |

Codes the hub itself produces: `bad_request` (frame failed validation),
`unsupported` (no handler configured, or durable replay without a Replayer), `internal`
(a handler error without a usable code, or an event payload that could not be encoded),
`timeout` (`context.DeadlineExceeded` from a handler), `slow_consumer`,
`replay_cursor_invalid`, `unauthorized`. Handler errors keep their own code when they
report one through `ErrorCode()`/`ErrorMessage()`; an unknown code degrades to `internal`
rather than putting a value on the wire no client can branch on.

## Defaults

| Option | Default | Effect |
|---|---|---|
| `ReplayEvents` | 2000 | ring size: events retained per session |
| `ReplayWindow` | 15 m | ring age |
| `Heartbeat` | 30 s | protocol ping interval, `server.heartbeat` interval, and the missed-pong budget |
| `WriteTimeout` | 10 s | per-frame write deadline |
| `SendBuffer` | 512 | per-subscriber queue length |
| `ServerVersion` / `PiVersion` / `Features` / `Limits` | empty | advertised in `welcome.server`; a limit the operator sets overrides the documented one |

Internal, not configurable in the spike: inbound frame limit 1 MiB, hello deadline 10 s,
close status 4401 for a failed post-upgrade handshake, closing-handshake grace 250 ms.

## Wiring and the dialog seam

```go
hub := ws.New(ws.Options{Token: token, ServerVersion: version, PiVersion: piVersion})
hub.SetCommandHandler(sup) // sessions.Handle: `command` frames
hub.SetDialogHandler(sup)  // sessions.Respond: `ui_response` frames
hub.SetReplayer(sup)       // sessions.ReplayFromEntry: `since.entryId`
mux.Handle("/ws/v1", hub)

requester, _ := hub.(ws.Requester)  // the outbound half of the dialog path
requester.SendRequest(sessionID, frame) // writes one `request` frame to that session's subscribers
```

`Requester` is a separate interface on purpose: `Hub` is frozen
(`docs/spike-interfaces.md` §5.2) and adding a method to it would change a surface three
packages depend on, while the dialog path needs exactly one extra call. `SendRequest`
validates the frame against `#/$defs/WsRequest` and refuses a malformed one at the call
site; a session with no subscribers is not an error (the spike does not queue dialogs for
clients that are not there, and the dialog times out on the session side).

## Extending this protocol

The seams are deliberate, so each of them grows by adding a file or a map entry rather
than a branch:

1. **A new frame**: add it to `schemas/ws.json` (client frames are the document root's
   `oneOf`), run `cd server && bash scripts/gen.sh`, add the case to `route`. Inbound
   frames are validated before any handler sees them, so the schema is the gate.
2. **A new op**: register it in the map in `internal/sessions` and answer it. The hub
   forwards `command` frames without knowing which ops exist.
3. **A new event**: publish it. `Publish` assigns `seq`/`ts`, keeps it for replay and
   fans it out; clients must ignore names they do not know.
4. **A new version**: bump `v` and add the shapes next to the v1 ones (dual support), or
   mount a second hub — the frame schema, not the code, decides what is acceptable.

