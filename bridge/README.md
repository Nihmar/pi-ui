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
| `mcpConfig` | no | — | Path of the MCP configuration the bridge should connect to (`GET/PUT /api/v1/mcp`). The server writes it when MCP is configured; **it names a file, it does not carry the configuration**, because one document serves every session and a change takes effect at the next spawn |
| `approvals.mode` | no | `"confirm"` | `"confirm"` asks the client; `"off"` never confirms |
| `approvals.patterns` | no | `["rm -rf", "git push --force", "sudo"]` | Case-insensitive substrings; empty entries are ignored |

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