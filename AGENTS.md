# AGENTS.md

pi-ui: **server companion + Flutter client for the pi coding agent** (Android + Linux + Windows).
Pending work is tracked in [GitHub Issues](https://github.com/Nihmar/pi-ui/issues).

## Language

- Everything in the repository is written in **English**: code, comments, docs, commit messages — regardless of the conversation language.

## Purpose and layout

Two deliverables:

- `server/` — Node.js/TypeScript service that orchestrates **one `pi --mode rpc` child process per session** (working directory on the host), exposes REST `/api/v1` + WebSocket `/ws/v1`, and adds the capabilities pi does not have: PTY terminals, files, git, search, MCP, background tasks, updates.
- `app/` — Flutter client (Riverpod · go_router · dio · web_socket_channel).
- `mockups/` — clickable Flutter mock with fake data; it freezes the UI **before** product work and then becomes the base of `app/`.
- `docs/` + `schemas/` — ADRs, API/WS specifications, JSON Schema (source of truth for every DTO and event) and `openapi.yaml`.

Intended layout once the definition phase lands:

```
pi-ui/
├── AGENTS.md
├── README.md
├── docs/          ADRs, api-v1.md, ws-protocol.md, protocol-mapping.md, security.md, mockups.md, roadmap.md
├── schemas/       core.json, pi.json, server.json, ws.json, openapi.yaml
├── server/        src/{api,ws,rpc,sessions,auth,fs,git,terminal,search,tasks,mcp,store,obs}
│                  test/fixtures/fake-pi/    (fixture-driven test harness)
├── app/           lib/{core,features,widgets}
├── mockups/       Flutter mock app (fake data)
├── deploy/        systemd, docker, nginx/caddy examples
└── .github/workflows/
```

`PLAN.md` is a **local-only** working document (gitignored): never commit it, never paste its content into commits, PRs, issues or docs.

## Boundary rules (non-negotiable)

- **pi is orchestrated, never forked.** The server talks to `pi --mode rpc` over JSONL stdio and forwards `pi.*` payloads **verbatim**. New pi commands or events must flow through the generic passthrough (`POST /sessions/{id}/commands`, WS `session.command.raw`) instead of new server-side special cases.
- **Conversations live in pi's session JSONL.** Server state (devices, sessions, cursors, audit) goes to SQLite and must stay reconstructible; never duplicate conversation history.
- **RPC framing**: split records on `\n` only (never Node's `readline`, which also splits on `U+2028`/`U+2029`), read stdout continuously, keep stdout reserved for the protocol and send diagnostics to stderr.
- **Never send provider secrets to clients.** API keys, tokens and provider headers stay server-side; the client may only see nicknames.

## Commits

- **Commit continuously — one commit per coherent change**, not one commit per session. A finished module, a green test suite, a schema+fixture pair, a docs section: each is its own commit. Never bundle unrelated changes, and never batch a whole feature into one commit.
- **Never add a co-author.** No `Co-authored-by:` trailers, no `generated with/by …` lines, no AI attribution in commits, PR bodies, issues or code comments. The author is the human who made the change.
- Messages: `area: imperative summary` — e.g. `server: add RpcBridge LF-only framing`, `app: stream assistant deltas`, `schemas: add ws frames`, `docs: write api-v1`, `repo: add AGENTS.md`. The body explains *why* when it is not obvious.
- Every commit must be **self-consistent**: the touched package builds, analyzes and passes its tests at that commit.
- Run the checks for the area you touched **before every commit** (see Flutter and Server sections); never commit a red suite.
- Keep **schema, protocol and docs changes in the same commit** as the code that changes them — that is the traceability contract of this repository.
- Never rewrite pushed history. Never commit `node_modules/`, build outputs, logs, tokens, provider credentials or session transcripts.
- Reference the related issue/PR in the body when one exists (`Refs #12`).

## GitHub (`gh`)

`gh` is available and authenticated in this environment (account `Nihmar`; token scopes `repo`, `workflow`, `gist`, `read:org`) — use it for GitHub work instead of raw HTTP or ad-hoc scripts:

- `gh auth status` — confirm authentication before assuming it.
- `gh issue create|list|view|comment|close` — issue tracking.
- `gh pr create|view|checks|comment|merge` — pull requests; read CI state with `gh pr checks <n> --watch` instead of guessing.
- `gh run list|view|watch|rerun` — GitHub Actions runs and failures.
- `gh api` — anything not covered by a dedicated command (releases, rulesets, raw endpoints).
- `gh release create|view|upload` — tagged releases and artifacts.

Do not merge, push tags or force-rewrite history unless the user explicitly asks.

## Build to expand

Everything here must be written with **expandability as a first-class requirement** — this server and client will grow one capability at a time (new pi commands, new services, new screens) and each addition must land without reworking the previous ones.

- Put a **seam at every boundary** — pi driver, transports, stores, UI data sources — and inject implementations (constructor/parameter injection, interfaces). No hard-wired paths, ports, versions or single-instance assumptions.
- **Schema-first**: `schemas/` is the source of truth for the wire surface. Endpoint/event changes update schema + fixtures + docs in the same commit. Unknown pi fields and event types are **tolerated** (lenient pass-through), never rejected.
- Prefer **capability flags** (`features[]`) and **versioned protocol changes** (`v:1` → `v:2` with dual support) over breaking shapes; clients degrade gracefully when a feature or pi version is missing.
- **Small, single-purpose modules**: a new command, event, server service or screen lands as a new file plus a registration, not as another branch in a growing function. Keep the registration points obvious and documented.
- TypeScript strict on; **no `any`** and no unchecked casts; validate every inbound payload against its schema (Ajv) before acting on it.
- **Tests ship with the feature**: fixture-driven protocol tests against the `fake-pi` harness so behaviour is reproducible without a model, plus e2e against real pi for integration paths.

## Flutter (`app/` and `mockups/`)

Platform targets: **Android, Linux, Windows** (iOS/macOS are out of scope in this environment; Windows builds run on CI `windows-latest` or a Windows host).

Before a change is considered finished, from the affected package directory (`app/` or `mockups/`):

```bash
dart fix --apply      # idempotent; applies all safe automated fixes
dart format .         # idempotent; formats every Dart file
flutter analyze       # must be clean: zero issues introduced by the change
flutter test          # when the package has tests
```

- **Always run the trio `dart fix` → `dart format` → `flutter analyze` before every commit** involving Dart/Flutter code; order matters (`fix` and `format` run *before* analyze and tests).
- After touching `pubspec.yaml`: `flutter pub get`, then re-run the trio.
- One widget file per screen/panel; theme tokens from `theme/` — **no hard-coded colors, sizes or paddings inside widgets**.
- Layout breakpoints: `< 600` mobile (NavigationBar + drawer, full-screen routes), `> 1024` desktop (NavigationRail + master/detail, resizable panels), intermediate in between.
- Keep `mockups/` and `app/` **structurally identical** (same widgets, different data source) so approving a mockup approves the real UI.
- Naming: `snake_case` files, `PascalCase` classes; Riverpod providers in their own file next to the feature they serve.

## Server (`server/`, Node.js/TypeScript)

Node ≥ 22.19, strict ESM + TypeScript, Fastify, Ajv validation, pino logging, `node:sqlite`, `node-pty`, git CLI, ripgrep.

```bash
npm ci
npm run typecheck && npm run lint && npm test
```

- Logs never contain tokens or conversation content by default; all outbound payloads are validated against `schemas/` before being sent.
- Every child `pi` process is version-pinned and probed (`GET /server.piVersion`) so a version mismatch degrades with a clear message instead of failing mid-run.

## Environment notes

- Flutter 3.47.2 stable on PATH (`~/develop/flutter/bin`); Android SDK at `~/develop` (`ANDROID_HOME`) with `android/local.properties` gitignored — never commit it.
- System JDK is 26; Android/Gradle builds may require a JDK 17/21 — pin one for Android work instead of downgrading the system.
- pi is installed at `~/.local/bin/pi` (`@earendil-works/pi-coding-agent` 0.87.1, `~/.pi/agent` config). Contract tests target that exact version.

## Definition of done

- [ ] One coherent commit with `area: imperative summary` — **no co-author trailer, no AI attribution**.
- [ ] Area checks pass locally (Flutter trio; server typecheck + lint + test) before the commit.
- [ ] Schema/fixtures/docs updated in the same commit when the wire surface changed.
- [ ] No secrets, tokens, transcripts, build artifacts or `PLAN.md` in the tree.
- [ ] New capability has its seam/registration documented and its tests included.


