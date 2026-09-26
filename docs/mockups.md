# Phase 2 — mockup plan, inventory and approval checklist

**Status:** in progress. The mockup lives in `mockups/`; this document is the
screen-by-screen contract a reviewer approves before the code becomes `app/`.
**Surface:** Android + Linux (Windows follows the same widget tree and is built
on CI; iOS/macOS are out of scope).
**Rule:** the mockup and `app/` are **structurally identical** — same widgets,
same theme tokens, same routes — with `MockPiApi` in place of the network
(AGENTS.md). Approving a screen here approves the real one.

## 1. How to review it

```bash
cd mockups

# Desktop review (Linux).
flutter run -d linux
flutter build linux --release   # build/linux/x64/release/bundle/piui_mockups

# Phone review (sideload the debug APK).
flutter build apk --debug       # build/app/outputs/flutter-apk/app-debug.apk
```

No server, no network: `MockPiApi` seeds two sessions with history and the
scenario recorder in the session header replays every state the plan must mock.

## 2. Design system

| Layer | Where | What it fixes |
|---|---|---|
| Tokens | `lib/core/theme/theme_tokens.dart` | Every colour, gap and radius; `AppTokens.dark`/`.light` and `context.tokens`. No widget hard-codes one. |
| Themes | `lib/core/theme/app_theme.dart` | The two `ThemeData`s (Material 3) built from the tokens. |
| Breakpoints | `lib/core/theme/breakpoints.dart` | `< 600` phone, `600–1024` one pane plus rails, `>= 1024` master/detail. |
| Markdown | `packages/piui-markdown` + `lib/core/theme/markdown_theme.dart` | The one markdown engine (ADR 0009) and its pi-ui style. |
| Shell | `lib/features/shell/adaptive_shell.dart` | `NavigationBar` on a phone, `NavigationRail` from 1024 up, one branch stack each. |

## 3. Screen inventory (PLAN.md §7)

`done` means the screen exists in `mockups/` and has a widget test; `draft`
means it exists but the plan's full state set is not covered yet; `planned`
means it is not built.

| # | Screen | Route / owner | State |
|---|---|---|---|
| 1 | Server URL (first launch) | `/connect` | planned |
| 2–4 | Pairing QR + code, certificate trust, network profile | `/pairing` | planned |
| 5 | Device/token management | settings → devices | planned |
| 6 | Session list (host/cwd, status, filters) | `/sessions` | done |
| 7 | Session creation with cwd picker | `NewSessionSheet` | draft (host browser deferred) |
| 8 | Chat timeline, streaming, tool cards | `/sessions/:id` | done |
| 9 | Search within the conversation | chat overflow | planned |
| 10–11 | Composer: text, images, slash commands, templates | `Composer` | draft (templates deferred) |
| 12 | Message queue (steer/follow-up) with cancel | `QueueStrip` | done |
| 13–14 | Header: model, thinking level, status, context/cost bar | `SessionHeader` | draft (model picker deferred) |
| 15 | Session statistics | chat overflow | planned |
| 16–18 | Branching: tree, fork, clone, rename | chat overflow | planned |
| 19 | Export (HTML/JSONL) | chat overflow | planned |
| 20 | Direct bash console | chat tool card | planned |
| 21–22 | Extension dialogs with countdown, notify/status/widget | `DialogCard` | draft (widget/status bar deferred) |
| 23 | Server settings | `/settings` | planned |
| 24 | Log/audit + error panel | settings → logs | planned |
| 25 | Connection status and replay banner | `ConnectionBanner` | done |
| 26–27 | Desktop multi-session tabs/split | desktop sessions | planned |
| 28 | Host file browser | `/files` | planned |
| 29 | Multiple terminals | `/terminal` | planned |
| 30 | Git panel | `/git` | planned |
| 31 | Global search | `/search` | draft (empty state only) |
| 32 | Background tasks + stop | `/tasks` | planned |
| 33 | MCP configuration | settings → MCP | planned |
| 34 | Updates (pi/server/packages) | settings → updates | planned |
| 35 | Themes and languages | settings → appearance | planned |
| 36 | Goal mode | feature flag | planned |
| 37 | pi packages | settings → packages | planned |
| 38 | Wrap-up/handoff states | scenario | draft (scenario only) |
| 39 | Two-mode login (QR or admin password) | `/connect` | planned |
| 40 | Offline queue with deferred send | `Composer` | done |
| 41 | Log viewer + audit with filters | settings → logs | planned |
| 42 | Markdown editor (source/WYSIWYG/read) | any `.md` surface | planned |

## 4. Mandatory states — how to see each one

Every state is one tap: open a session, tap **▷** in the header, pick the
scenario. All of them are covered by the `mock_api_test.dart` suite.

| State | Scenario | What to look at |
|---|---|---|
| Skeleton / empty | fresh session | empty state of the timeline and the list |
| Streaming | Streaming answer | streaming caret, thinking block, abort |
| Long tool call | Tool call with diff | pending → running → done, output, diff |
| Extension dialog | Approval dialog | countdown, approve/deny, status line |
| Dialog timeout | Dialog timeout | the server cancels and the card disappears |
| Provider quota | Provider quota exhausted | coded error card with a retry action |
| Dead / crashed session | Session crash | status badge, exit code, restart action |
| Offline + queue | Offline queue | banner, queued chips, flush on reconnect |
| Reconnection with replay | Reconnect with replay | banner, "replaying…" status, catch-up |
| Wrap-up + handoff | Wrap-up and handoff | "Closing…", handoff saved, exited |

## 5. Approval checklist (Phase 2 exit criterion)

A reviewer signs off one line per area; "approved" is what freezes the widget
for `app/`. A rejected line becomes an issue, not a silent edit.

- [ ] Theme: dark and light read well at both breakpoints.
- [ ] Shell: phone bar and desktop rail are the same navigation.
- [ ] Session list: status, cwd, model, context and cost are legible at a glance.
- [ ] Session creation: the form is clear about the host directory and trust.
- [ ] Chat timeline: user/assistant/tool/status/error entries are distinguishable.
- [ ] Markdown: code, lists, links and thinking render as designed.
- [ ] Composer: send, steer, follow-up, slash commands and attachments are obvious.
- [ ] Queue: what waits, of which kind, and how to drop it.
- [ ] Dialog: answerable without losing the conversation; the countdown reads.
- [ ] Connection states: offline, reconnecting and replayed are unmistakable.
- [ ] Every mandatory state of §4 was seen at least once.

## 6. What is next

1. The onboarding flow (1–4, 39): URL, certificate, pairing, network profile.
2. The remaining panes of the desktop layout (26–27) and global search (31).
3. The settings tree (23, 24, 33–35, 37, 41) with the admin sections stubbed.
4. The feature panes (20, 28–30, 32, 42) once their server surfaces exist.
5. Freeze: tag the mockup, generate the annotated screenshots and finish this
   checklist.
