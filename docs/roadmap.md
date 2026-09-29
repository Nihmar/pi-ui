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

## Phase 6 — App parity ✅

Delivered: file browser, git panel, global search, terminal, settings (catalogue-driven
policy, admin editing, update panel), themes and models, dialogs, and the client's own
translations: `flutter_localizations` with `lib/l10n/app_en.arb` (the source) and
`app_it.arb`, generated into `lib/l10n/app_localizations.dart`, selected by the server's
`ui.language` and falling back to the device for a language this build does not ship.

Strings live in the ARB files, not in widgets: every screen reads `context.l10n`, English is
kept byte-identical so a sentence is still greppable, and
`test/unit/l10n_test.dart` checks the choice of locale plus that the Italian file is a
*translation* rather than a copy.

**The residual is named rather than hidden**: two kinds of text are still English only —
the sentences the **server** sends (it does not know a client's locale, and its messages are
part of the API) and the **client-side fallbacks produced outside a widget** (the error
sentences in `lib/core/api/errors.dart` and the status lines the timeline fold builds, such
as "Compacting the context…"). Making those translatable means giving the fold and the
error taxonomy a strings seam; a third locale would make it worth doing.

## Phase 7 — Security and networks 🔶

Delivered: TLS termination with the certificate fingerprint reported and pinned
(`internal/tls`, `--tls-cert/--tls-key`, `pi-ui tls fingerprint`), the peer allowlist
(`--allow-ips`, audited), the scope model and revocation, audit, path confinement, rate
limits, `docs/security.md`, and container-per-session isolation (`--isolate <image>`).

The network mode of an isolated session is a flag (`--isolate-network host|bridge|none`,
`host` by default so a session reaches a model server on the machine), and where isolation
works is documented rather than implied: on the host, where the paths it mounts exist. A
containerised server cannot spawn session containers that mount paths living only inside it,
so `deploy/` ships without `--isolate` and says why — the honest boundary, not a gap waiting
for code.

## Phase 8 — Distribution ✅

Delivered: the release workflow (tag → server binaries for linux amd64/arm64 and windows,
Linux client bundle, unsigned APK, Windows zip, checksums, GitHub release), the Docker image
and Compose reference, a hardened systemd unit, the network/TLS/deployment guide, the Arch
`PKGBUILD` and the Inno Setup script.

**The first release is out**: `v0.1.1` (2026-09-27) with the server for linux amd64/arm64 and
windows amd64, the client for Linux, Android and Windows, and a `.sha256` next to every
archive. Its story is worth keeping: `v0.1.0` was tagged first and its build failed — the
Windows server did not compile, because the terminal, task and session services reached for
POSIX calls on a platform that has none. The fix landed on main rather than on a moved tag,
so the first published release is `v0.1.1`, and the portability that came out of it (build
tags per platform, PTYs `unsupported` on Windows with a clear message) is part of it.

`v0.2.0` (2026-09-29) is the current release: it adds the AppImage beside the Linux
tarball, the terminal emulator the client paints a PTY with, the MCP client's Streamable
HTTP transport and the landing page in `site/`. The version pins the workflow does not
stamp — `app/pubspec.yaml`, `packaging/PKGBUILD` — move with the tag.

**Missing:** proper code signing (the APK carries the template's debug signature and the
Windows installer is unsigned; both say so in their names).

## Beyond the plan

The natural next steps, in the order they pay off:

1. **App translations** (Phase 6's gap) — the only user-visible hole.
2. ~~A VT emulator for the terminal~~ — delivered (`app/lib/features/terminal/vt.dart`, drawn by
   `terminal_view.dart`): the grid, the cursor, the scrollback, SGR and the palette, the
   alternate screen, DEC special graphics, origin and auto-wrap modes, tab stops, the cursor
   position report and mouse tracking in both encodings. What is left is genuinely niche
   (DEC double-width lines, the sixel and ReGIS graphics extensions, printer passthrough).
3. ~~The MCP client's remote transport~~ — delivered (`bridge/mcp_http.ts`): a `url` entry is
   spoken to over Streamable HTTP, with the session id kept and answers read from JSON or
   from an event stream. What it does not consume is what a server *pushes* (sampling,
   logging) — this client calls tools.
4. **Server-in-container** (Phase 7's gap) and its network mode.
