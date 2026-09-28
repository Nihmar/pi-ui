# pi-ui

**Server companion + Flutter client for the [pi](https://github.com/earendil-works/pi-coding-agent)
coding agent** (Android · Linux · Windows).

pi runs the agent; pi-ui wraps it. The Go server orchestrates one `pi --mode rpc` child
process per session — each in the working directory you choose — and exposes REST
(`/api/v1`) and WebSocket (`/ws/v1`) for clients, adding the capabilities pi does not
have on its own: PTY terminals, files, git, search, MCP, background tasks and updates.
The Flutter client lives in `app/`: it pairs with the server (invitation code or admin
password), keeps the device token in the OS keystore and streams the sessions over the
WebSocket with replay and reconnection. The UI is the approved Phase 2 mockup in
`mockups/` (see `docs/mockups.md`) — same widgets, real data source — over the Phase 1
server spike whose measured numbers and protocol contracts are in `docs/`.

Beyond driving pi, the server owns the capabilities pi does not have, each confined to
the workspaces an operator allows:

| Capability | Where it lives | Endpoints |
|---|---|---|
| Files | `internal/fs` | `/workspaces`, `/fs/list`, `/fs/stat`, `/files/read\|write\|delete` |
| Git | `internal/git` | `/git/status\|log\|diff\|stage\|commit` |
| Search | `internal/search` | `/search` (ripgrep over the roots, JSONL scan of the sessions) |
| Terminals | `internal/terminal` | WebSocket `terminal.open\|input\|resize\|close` |
| Background tasks | `internal/tasks` | `/tasks`, `/tasks/{id}/stop` |
| Server policy | `internal/settings` | `/settings` (admin), `server.settings.changed` |
| Drain | `internal/api` | `/drain/start\|resume` |
| TLS and pinning | `internal/tls`, `internal/cli` | `--tls-cert`/`--tls-key`, the `tls` block in `/server` |
| Peer allowlist | `internal/cli` | `--allow-ips` (addressed or CIDR, checked before any handler) |
| Session isolation | `internal/sessions` | `--isolate <image>`: one `docker run` per session, every path mounted same-path, `--isolate-network` for its network |

The confinement rule is one implementation (`internal/fs`): a path is decided on its
resolved form, so a symlink inside a root cannot point outside it, and every capability
reuses it rather than re-deriving it. A server started without `--root` exposes no
filesystem, git, task or terminal surface at all (a `501`, never a guess).

## Repository layout

| Path | What it is |
|---|---|
| `server/` | Go service (`CGO_ENABLED=0`, static binary): `internal/{rpc,ws,sessions,api,cli,protocol/gen,spike}` |
| `app/` | Flutter client (Riverpod · go_router · dio · web_socket_channel) |
| `mockups/` | Clickable Flutter mock that freezes the UI before product work, with [rendered screenshots](mockups/screenshots/index.html) of every screen |
| `bridge/` | `pi-ui-bridge` extension (TypeScript sources, loaded by pi via jiti) |
| `packages/piui-markdown/` | Shared markdown/editor engine (vendored from Niman, MIT) |
| `schemas/` | JSON Schema, the source of truth for every DTO and event |
| `docs/` | ADRs, protocol documents, the spike report, [security model](docs/security.md) and [roadmap](docs/roadmap.md) |
| `deploy/` | The reference deployment: Docker image + Compose, a hardened systemd unit, and the network matrix (LAN/VPN/HTTPS) |
| `packaging/` | The Arch `PKGBUILD`, the Inno Setup script for Windows and the AppImage builder |

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

The reference deployment is [deploy/](deploy/README.md): a container that mounts the
projects same-path, or the static binary under systemd. Prebuilt archives are on the
[release page](https://github.com/Nihmar/pi-ui/releases) (server for linux amd64/arm64 and
windows amd64, client for Linux, Android and Windows, with checksums).

`serve` starts one `pi --mode rpc` child per `--session`, serves REST on `/api/v1` and
WebSockets on `/ws/v1`, and shuts down gracefully on SIGINT/SIGTERM, reaping every child
(and stopping every terminal and background task first).

Useful flags beyond the session ones: `--root <path>` (repeatable) for the workspaces a
client may browse, `--session-dirs <list>` for the pi session JSONL the message search
reads, `--terminals N` for the shell budget, `--state-dir` for the settings and the
device tokens.
Configuration also comes from `PIUI_*` environment variables (see
`docs/spike-interfaces.md` §5.7).

## Pair a device

The server keeps its state (paired devices, the admin password, pending pairing
invitations) in one SQLite file, `$PIUI_STATE_DIR/state.db`
(default `$XDG_STATE_HOME/pi-ui/state.db`).

```bash
./bin/pi-ui auth set-password          # the admin credential and recovery path
./bin/pi-ui pair --url http://<host>:8787   # code, deep link and a scannable QR
./bin/pi-ui status                     # devices, password, pending invitations
./bin/pi-ui tls fingerprint --cert cert.pem   # what the app compares when it pins
```

`pair` mints a single-use invitation that expires after ten minutes; a running
`serve` over the same state directory consumes it and returns a device token once.
A server that starts with nothing configured mints one invitation itself and logs
its code. `serve --token <token>` keeps the old static-token mode.

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
- [`docs/api-v1.md`](docs/api-v1.md) — REST conventions, scopes and the pairing/device contract.
- [`docs/ws-protocol.md`](docs/ws-protocol.md) — WebSocket v1 handshake, frames, replay and error codes.
- [`docs/spike-report.md`](docs/spike-report.md) — the acceptance measurements C1–C9 with their raw samples.
- [`docs/mockups.md`](docs/mockups.md) — the Phase 2 screen inventory, the mandatory states and the approval checklist; the rendered screens are in [`mockups/screenshots/`](mockups/screenshots/index.html).
- [`docs/adr/0007-go-spike-decision.md`](docs/adr/0007-go-spike-decision.md) — why the Go server stands.
- [`docs/verification-report.md`](docs/verification-report.md) — adversarial verification of framing and shutdown.
- [`docs/review-notes.md`](docs/review-notes.md) — Phase 1 code review: findings, severity and status.
- [`schemas/`](schemas) — JSON Schema for the wire surface (`core.json`, `pi.json`, `ws.json`).
- [`AGENTS.md`](AGENTS.md) — repository conventions, boundaries and definition of done.
