# Protocol mapping

How one thing a user does travels: from a `pi` record or command, through the server, to the
client, and back. It is the document to read before adding a capability, because it says
where the seam is and what must not be invented on the way.

## The three surfaces

| Surface | Owns | Contract |
|---|---|---|
| pi (`--mode rpc`) | the conversation, the model, the tools | `pi/docs/rpc-commands.md`, `message-types.md` |
| Server | the host: sessions, files, git, search, PTYs, tasks, policy | `docs/api-v1.md`, `docs/ws-protocol.md`, `schemas/` |
| Client | what a user sees and does | `app/`, the mockup in `mockups/` |

The rule between them: **`pi.*` payloads are forwarded verbatim.** The server adds a layer
(`server.*`), it never rewrites what the child said. A record it cannot read travels as
`pi.unknown` with the line; an event type it does not know is still published, because that
is what lets a new pi release reach clients without a server release.

## Commands (client → pi)

| Client action | Wire | op | pi command |
|---|---|---|---|
| send a message | WS `command` | `session.prompt` | `{"type":"prompt","message":…}` (rate limited per session) |
| steer a running turn | WS `command` | `session.steer` | `{"type":"steer","message":…}` |
| queue a follow-up | WS `command` | `session.follow_up` | `{"type":"follow_up","message":…}` |
| stop the turn | WS `command` | `session.abort` | `{"type":"abort"}` |
| drop the queue | WS `command` | `session.clear_queue` | `{"type":"clear_queue"}` |
| rename | WS `command` | `session.rename` | `{"type":"rename","name":…}` + the projection follows |
| stop the session | REST `POST /sessions/{id}/stop` | `session.stop` | graceful shutdown, then the terminal status |
| anything else pi gains | WS `command` | `session.command.raw` | the payload **is** the command object |

`session.command.raw` is why the plan's "matrix covered 100%" is a property of the design
and not a checklist: `get_state`, `get_available_models`, `set_model`,
`set_thinking_level`, `get_entries`, `compact`, `fork` and the next command all travel
through the same op. The client's model picker, its thinking-level chips and its stats
header are three users of it and no server code was added for them.

## Records (pi → client)

| pi record | Server event | What the client does |
|---|---|---|
| `entry_appended` | `pi.entry_appended` (+ `entryId` for replay) | appends a message, a tool card or a status line |
| `message_update` | `pi.message_update` | builds the streaming assistant text and thinking |
| `agent_end`, `agent_settled` | `pi.agent_end`, `pi.agent_settled` | ends the streaming state, refreshes the stats |
| `queue_update` | `pi.queue_update` | renders the steer/follow-up strip |
| `session_info_changed` | `pi.session_info_changed` | renames the session in the list |
| `thinking_level_changed` | `pi.thinking_level_changed` | updates the header chip |
| `compaction_start`, `compaction_end` | same names | status lines |
| `auto_retry_*`, `summarization_retry_*` | same names | status lines |
| `bash_execution_update` | `pi.bash_execution_update` | streams a bash card |
| `extension_ui_request` | `request` frame (dialog methods) or `ext.*` event | the dialog card, or a status line |
| `response` | the answer of the `command` that caused it | resolves the caller's future |
| a line that is not JSON | `pi.unknown` | a status line saying so |

Server facts (`server.*`) are lifecycle and meta: `spawned`, `ready`, `status`, `stopping`,
`exited`, `crashed`, `dialog.timeout`, `settings.changed`, `tasks.changed`, `heartbeat`,
`replay.begin`, `replay.end`, `error`.

## Replay, cursors and the terminal

- Every event carries a global `seq` and, when it has one, an `entryId`. A client resumes
  with `since: {entryId}` (durable) or `{seq}` (ring), and `server.replay.begin/end` wrap the
  gap. **Meta frames never move the cursor**: they can carry a higher `seq` than the events
  they wrap.
- Terminal frames (`terminal.*`) are the exception that proves the rule: they carry no `seq`,
  are never replayed, and go to exactly one connection, because a PTY is live state of a
  socket and not a conversation.

## What must not be invented

- A client-side model of a conversation. The timeline is a fold over the events; a
  reconnecting client replays instead of remembering.
- A server-side copy of a conversation. State lives in SQLite (devices, settings, audit,
  cursors) and must stay reconstructible; history is pi's session JSONL.
- A new endpoint per pi command. `session.command.raw` is the one that exists.
- A second markdown, diff or terminal implementation. `packages/piui-markdown` and the chat's
  `DiffView` are shared on purpose.
