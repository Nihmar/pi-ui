# pi-ui

**Server companion + Flutter client for the [pi](https://github.com/earendil-works/pi-coding-agent)
coding agent** (Android · Linux · Windows).

pi runs the agent; pi-ui wraps it. The Go server orchestrates one `pi --mode rpc` child
process per session — each in the working directory you choose — and exposes REST
(`/api/v1`) and WebSocket (`/ws/v1`) for clients, adding the capabilities pi does not
have on its own: PTY terminals, files, git, search, MCP, background tasks and updates.
The Flutter client lives in `app/`; the current milestone is the Phase 1 server spike,
whose measured numbers and protocol contracts are in `docs/`.

## Repository layout

| Path | What it is |
|---|---|
| `server/` | Go service (`CGO_ENABLED=0`, static binary): `internal/{rpc,ws,sessions,api,cli,protocol/gen,spike}` |
| `app/` | Flutter client (Riverpod · go_router · dio · web_socket_channel) |
| `mockups/` | Clickable Flutter mock that freezes the UI before product work |
| `bridge/` | `pi-ui-bridge` extension (TypeScript sources, loaded by pi via jiti) |
| `packages/piui-markdown/` | Shared markdown/editor engine (vendored from Niman, MIT) |
| `schemas/` | JSON Schema, the source of truth for every DTO and event |
| `docs/` | ADRs, protocol documents and the spike report |
| `deploy/` | Docker Compose reference, systemd, Tailscale/nginx examples |

## Build and run the server

Go 1.27+; Node ≥ 22.19 wherever the sessions run (pi itself is a Node CLI).

```bash
cd server
CGO_ENABLED=0 go build -o bin/pi-ui ./cmd/pi-ui

./bin/pi-ui version
./bin/pi-ui serve --addr 127.0.0.1:8787 --pi pi \
  --session /path/to/project[:name] \
  --bridge ../bridge/pi-ui-bridge.ts
```

`serve` starts one `pi --mode rpc` child per `--session`, serves REST on `/api/v1` and
WebSockets on `/ws/v1`, and shuts down gracefully on SIGINT/SIGTERM, reaping every child.
Configuration also comes from `PIUI_*` environment variables (see
`docs/spike-interfaces.md` §5.7).

## Test

```bash
cd server
gofmt -l . && go vet ./... && go test -race ./... && CGO_ENABLED=0 go build ./...
```

The suites run without a model and without network: `server/test/fake-pi` is a
deterministic `pi --mode rpc` impersonator, `server/test/e2e` drives the built binary
(REST → WebSocket → events → stop, SIGTERM reaping) and talks to the installed pi when
it is present, and `server/test/adversarial` attacks the framing, WebSocket and REST
surfaces.

## Measure

```bash
server/scripts/measure.sh
```

Builds the static binary and the fake-pi harness, runs the acceptance measurements
(spawn latency and RSS, WebSocket throughput and latency, the three-minute soak) and
keeps every raw sample under `.piui/spike/`, which is gitignored. The results and how to
read them are in [`docs/spike-report.md`](docs/spike-report.md); the pass/fail decision
per criterion and the Python-fallback trigger are in
[`docs/adr/0007-go-spike-decision.md`](docs/adr/0007-go-spike-decision.md).

The same runs are available directly:

```bash
server/bin/pi-ui measure [--sessions 8] [--pi <path>|--fake-pi <path>] \
  [--events N] [--rate N] [--clients N] [--out <file>]
```

## Documentation

- [`docs/spike-interfaces.md`](docs/spike-interfaces.md) — the frozen Phase 1 interface contract.
- [`docs/ws-protocol.md`](docs/ws-protocol.md) — WebSocket v1 handshake, frames, replay and error codes.
- [`docs/spike-report.md`](docs/spike-report.md) — the acceptance measurements C1–C9 with their raw samples.
- [`docs/adr/0007-go-spike-decision.md`](docs/adr/0007-go-spike-decision.md) — why the Go server stands.
- [`docs/verification-report.md`](docs/verification-report.md) — adversarial verification of framing and shutdown.
- [`docs/review-notes.md`](docs/review-notes.md) — Phase 1 code review: findings, severity and status.
- [`schemas/`](schemas) — JSON Schema for the wire surface (`core.json`, `pi.json`, `ws.json`).
- [`AGENTS.md`](AGENTS.md) — repository conventions, boundaries and definition of done.
