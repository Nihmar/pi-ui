# Roadmap and status

Where the project is against `PLAN.md`, phase by phase. It is written to be **resumable**:
each phase says what exists, what the exit criterion was, and — where something is missing —
what exactly is missing and where to start.

Legend: ✅ done · 🔶 partial (the gap is named) · ⬜ not started.

## Phase 0 — Definition ✅

ADRs 0001–0009, the protocol documents, `schemas/*.json` as the source of truth,
`server/internal/protocol/gen/` generated and drift-checked in CI, the Go module skeleton
and the CI workflow.

`docs/` holds `api-v1.md`, `ws-protocol.md`, `spike-interfaces.md`, `spike-report.md`,
`security.md`, `mockups.md`, `review-notes.md`, `verification-report.md` and this file.

## Phase 1 — Server spike (Go) ✅

`RpcBridge` with LF-only framing, backpressure, WS fan-out with `since` replay, the minimal
bridge with `notify`/`confirm`, and the `fake-pi` harness. The measured numbers (spawn,
memory, events per second) are in `docs/spike-report.md`; the criteria held, so no fallback
was needed.

## Phase 2 — Mockup ✅ (gate approved)

42 screens for phone and desktop in `mockups/`, with the design tokens the app reads
(`mockups/assets/styles.css` → `app/lib/core/theme/theme_tokens.dart`).

## Phase 3 — Server core ✅

Auth (pairing, device tokens, scopes, revocation), REST + WS, the `SessionSupervisor`
(spawn, watchdog, respawn with resume, eviction), the core commands, audit, health, rate
limits, drain. `internal/cli/serve.go` is the wiring; `v0.1-server` e2e tests run against
`fake-pi` and real pi.

## Phase 4 — App core ✅

Pairing with the OS keystore, the session list, streaming chat with replay and reconnection,
the composer with steer/follow-up, approvals, local notifications, Android/Linux/Windows
builds. `v0.1` is usable on a phone over a LAN and on the desktop.

## Phase 5 — Server parity ✅

| Capability | Where | Surface |
|---|---|---|
| Files | `internal/fs` | `/workspaces`, `/fs/list`, `/fs/stat`, `/files/read\|write\|delete` |
| Git | `internal/git` | `/git/status\|log\|diff\|stage\|commit` (writes gated by `git.write`) |
| Search | `internal/search` | `/search` (ripgrep over the roots, JSONL scan of the sessions) |
| Terminals | `internal/terminal` | WS `terminal.open\|input\|resize\|close`, `terminal.output\|closed` |
| Tasks | `internal/tasks` | `/tasks`, `/tasks/{id}/stop` |
| Settings | `internal/settings` | `/settings` (admin), `server.settings.changed` |
| MCP | `internal/mcp` + `bridge/mcp.ts` | `/mcp` (admin), the bridge connects and registers the tools |
| Updates | `internal/updates` | `/updates`, `/updates/apply` (runs the operator's script) |
| Goal mode | `bridge/goal.ts` | `/goal start\|status\|stop` inside the session (feature flag) |
| Drain | `internal/api` | `/drain/start\|resume` |

The `rpc-commands.md` matrix flows through `session.command.raw`: a new pi command needs no
server change, which is the fidelity rule the plan asks for.

## Phase 6 — App parity 🔶

Delivered: file browser, git panel, global search, terminal, settings (catalogue-driven
policy, admin editing, update panel), themes and models, dialogs.

**Missing: the app's own translations.** `ui.language` is stored and documented, and the
server sends it, but the client ships English only: there is no `flutter_localizations`
setup, no ARB files and no `l10n` calls. Doing it means extracting every user-facing string
from ~15 screens and updating the widget tests that assert them — start with
`app/pubspec.yaml` (`flutter_localizations`, `generate: true`), then
`app/lib/core/l10n/` and one screen at a time.

## Phase 7 — Security and networks 🔶

Delivered: TLS termination with the certificate fingerprint reported and pinned
(`internal/tls`, `--tls-cert/--tls-key`, `pi-ui tls fingerprint`), the peer allowlist
(`--allow-ips`, audited), the scope model and revocation, audit, path confinement, rate
limits, `docs/security.md`, and container-per-session isolation (`--isolate <image>`).

**Missing:** the container shares the host's network namespace (a session reaches the same
provider endpoints), and the server itself still runs on the host. The plan's remaining item
is putting *the server* in the container as the only supported deployment; `deploy/` has the
image, so the work is a network-namespace mode and its documentation.

## Phase 8 — Distribution 🔶

Delivered: the release workflow (tag → server binaries for linux amd64/arm64 and windows,
Linux client bundle, unsigned APK, Windows zip, checksums, GitHub release), the Docker image
and Compose reference, a hardened systemd unit, the network/TLS/deployment guide, the Arch
`PKGBUILD` and the Inno Setup script.

**Missing:** a cut release (no tag has been pushed yet — CI is green, the workflow is
untested against a real tag), AppImage (the plan names it; today the Linux client ships as a
tar.gz), and code signing for the APK and the Windows installer (deliberate: an unsigned
artifact that says so beats one signed with a throw-away key).

## Beyond the plan

The natural next steps, in the order they pay off:

1. **App translations** (Phase 6's gap) — the only user-visible hole.
2. **A first tagged release** — exercises `release.yml` end to end and gives the deployment
   docs something to point at.
3. **A VT emulator for the terminal** — today it renders the byte stream; a REPL that redraws
   a line (progress bars, `top`) needs a parser.
4. **Server-in-container** (Phase 7's gap) and its network mode.
5. **The MCP client's remote transport** — `url` entries are validated, reported as
   unsupported and skipped; stdio works.
