# pi-ui-bridge

The `pi-ui` extension that runs inside every `pi --mode rpc` child process. It is
deliberately small: it exercises the **RPC extension UI subprotocol** so the Go server
and the WebSocket hub can be built and tested against real pi behaviour.

Contract: `docs/spike-interfaces.md` §10 (behaviour) and §5.7 (`PI_UI_BRIDGE_CONFIG`).

## Behaviour

| Trigger | Effect |
|---|---|
| `session_start` | `ctx.ui.notify("pi-ui-bridge ready", "info")` — fire-and-forget, unconditional |
| `tool_call` for `bash`, approvals `confirm`, command matches a pattern, `ctx.hasUI` | `ctx.ui.confirm("pi-ui-bridge: confirm command", "<message containing the command>")` |
| confirmation refused | `ctx.ui.notify("Command blocked by pi-ui-bridge", "warning")` and `{ block: true, reason: "blocked by pi-ui-bridge" }` |
| everything else | no UI, returns `undefined` (the tool call proceeds) |
| `session_start`, when `mcpConfig` is set | connects every enabled **stdio** MCP server, lists its tools and registers each as a pi tool named `mcp_<server>_<tool>` |
| `session_shutdown` | closes those connections (SIGTERM, then SIGKILL) — idempotent, like every cleanup path |
| `/goal start <objective>` | starts a goal: after every settled turn the driver sends the next round, until the model writes `GOAL_DONE` or the round budget is spent |
| `/goal status` · `/goal stop` | what the current goal is doing, and ending it |

Patterns match as **case-insensitive substrings**, not regular expressions: the patterns
come from configuration, and a substring can neither fail to compile nor backtrack.
The startup notify string is a fixed contract — the JSONL fixtures and the `ext.notify`
WebSocket tests assert it verbatim, so do not reword it.

## Loading it

pi executes the TypeScript source directly through jiti — there is no build step and the
sources are the artifact. Explicit `-e` paths work even with extension discovery disabled:

```bash
pi --mode rpc --no-extensions -e /path/to/bridge/pi-ui-bridge.ts
```

The server starts each session with `-e <bridge>` and `PI_UI_BRIDGE_CONFIG=<runtime>/<sid>.json`
in the child environment.

## Configuration

`PI_UI_BRIDGE_CONFIG` is the path to a JSON file:

```json
{
  "sessionId": "s_0123456789abcdef",
  "approvals": {
    "mode": "confirm",
    "patterns": ["rm -rf", "git push --force", "sudo"]
  }
}
```

| Field | Required | Default | Notes |
|---|---|---|---|
| `sessionId` | no | — | Echoed in the bridge's stderr line, so server logs name the session |
| `goal` | no | — | Present turns goal mode on: `{}` or `{"maxRounds": 10}` (clamped to 50). Absent leaves `/goal` unregistered |
| `mcpConfig` | no | — | Path of the MCP configuration the bridge should connect to (`GET/PUT /api/v1/mcp`). The server writes it when MCP is configured; **it names a file, it does not carry the configuration**, because one document serves every session and a change takes effect at the next spawn |
| `approvals.mode` | no | `"confirm"` | `"confirm"` asks the client; `"off"` never confirms |
| `approvals.patterns` | no | `["rm -rf", "git push --force", "sudo"]` | Case-insensitive substrings; empty entries are ignored |

## Goal mode

A long-running objective the session keeps working on, **off unless the configuration
turns it on** (`goal: {}` in the bridge config; `maxRounds` defaults to 10 and is clamped
to 50). It is a round driver, not a second agent:

- `/goal start <objective>` sends the first round as a user message; every round opens
  with a review of what is done and what is next, so the model works from state instead
  of re-reading the objective;
- after each settled turn the driver decides: the model's `GOAL_DONE` marker ends the
  goal, otherwise the next round is sent until the budget is spent — a goal can never
  loop forever, because the budget is the server's and not the model's;
- the state is published with `setStatus` (a UI bar) and as a `goal` custom entry, which
  the app renders as a status line on the timeline: no new endpoint, no new frame, and
  the generic command passthrough is enough to drive it from a client (a prompt
  `/goal start …`).

The state machine lives in `goal.ts` and is pure — no pi, no clock, no I/O — because the
loop's decisions are the part worth testing; `test/goal.test.ts` covers the commands, the
budget, the marker, the status line and the round prompt without a model.

## MCP

The bridge is the half of MCP that speaks the protocol: the Go server stores and
validates the configuration (`GET/PUT /api/v1/mcp`) and writes its **path** into
`mcpConfig`, and this extension connects to the servers it names. A change therefore
takes effect at the next spawn, and the server never runs a tool server itself.

- **Transport**: two, decided by the entry. A `command` gets one child process and JSON-RPC
  over stdio; a `url` is spoken to over HTTP (the specification's Streamable HTTP: one POST
  per message, an answer as JSON or as a `text/event-stream`, the session id the server hands
  out kept and sent back). Sampling and logging pushed by a server are not consumed: this
  client calls tools and reads their answers.
- **Tools**: `initialize` → `notifications/initialized` → `tools/list`, and each tool is
  registered with the MCP server's own JSON Schema wrapped rather than rebuilt, so no
  keyword is lost.
- **Failures**: one server that cannot start, cannot list or exits mid-call is reported
  on stderr and skipped; its tools simply are not there, and a session still starts. A
  tool that reports `isError` throws, because that is pi's tool contract for a failed
  call, and a request that gets no answer in 60 s fails instead of hanging a turn.
- **Tests**: `npm test` runs the Node test runner against a real child process and a real
  HTTP listener that speak the protocol (start, handshake, listing, a call, a failure, a
  shutdown, an SSE answer, a refused server), so both transports are exercised end to end
  without an MCP server installed.

### Degradation rules

A valid config file that omits fields takes the defaults above. **Any** of these problems
degrades to `mode: "off"`, which never asks for confirmation and never blocks a command —
the `session_start` notify still fires, so the extension stays observable:

- `PI_UI_BRIDGE_CONFIG` unset or empty;
- the file cannot be read (missing, permissions);
- the file is not valid JSON;
- the JSON is not an object, or `sessionId` / `approvals` / `approvals.mode` /
  `approvals.patterns` have the wrong type.

Every degradation writes one diagnostic line to **stderr** (`pi-ui-bridge: …`) and then
reports the effective mode (`pi-ui-bridge: approvals mode: off`). stdout is reserved for
the RPC protocol stream and is never touched by diagnostics.

## Development

```bash
npm install        # devDependencies: typescript, @earendil-works/pi-coding-agent, @types/node
npm run typecheck  # tsc --noEmit
```

`typescript` is pinned to the newest 5.x release and `@earendil-works/pi-coding-agent` to
the exact pi version the server pins (`0.87.1`), so the extension types cannot drift from
the child binary. `tsconfig.json` is strict with `skipLibCheck` (pi's own declarations
import JSON without import attributes, which `NodeNext` rejects).

`node_modules/` is never committed; `package-lock.json` is.