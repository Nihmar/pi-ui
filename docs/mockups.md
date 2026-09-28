# Phase 2 — HTML mockup: inventory and approval checklist

**Status:** every screen of the plan exists as a static HTML file.
**Where:** `mockups/` — no build step, no server, no dependency: open
`mockups/index.html` in a browser.
**Purpose:** the mockup freezes the UI **before** product work. Approving a
screen here is approving the widget `app/` will implement; the design tokens in
`mockups/assets/styles.css` are the palette, spacing, radii and breakpoints the
Flutter client inherits.

## 1. How to review it

```bash
xdg-open mockups/index.html      # or any browser: file:///…/mockups/index.html
```

* The **gallery** shows every screen as a scaled frame; click one to open it.
* Each screen page has a toolbar: **theme** (dark/light), **device**
  (mobile/desktop widths) and a link back to the gallery.
* The **states board** (`screens/00-states.html`) freezes every mandatory state:
  streaming, long tool with the watchdog, dialog timeout, provider quota, crash,
  offline queue, reconnect with replay, wrap-up/handoff, empty, skeleton,
  insufficient scope, notification permission denied.
* Screens with interaction (tabs, dialogs, countdowns, collapsible thinking) are
  live: the countdown ticks, tabs switch, `data-toggle` buttons reveal extra
  states.

## 2. Structure

| Path | What it is |
|---|---|
| `index.html` | The gallery: every screen, grouped by area, as a scaled iframe. |
| `assets/styles.css` | Design tokens (dark/light) and every component class. |
| `assets/app.js` | Theme/device toggles, tabs, countdowns. Dependency-free. |
| `screens/00-states.html` | The mandatory-states board. |
| `screens/NN-<name>.html` | One screen per file, `NN` matching the inventory below. |
| `screenshots/` | Frozen PNG renders of every screen (dark + light) and of the app; gallery in `screenshots/index.html`. |
| `tools/` | `shoot_screenshots.mjs` renders the screens headlessly, `make_gallery.py` builds the thumbnails and the gallery. |

Since the renders are only a mirror of the screens, they are regenerated on demand rather
than kept in step by hand: `node mockups/tools/shoot_screenshots.mjs` followed by
`python3 mockups/tools/make_gallery.py` (both path-independent, documented in
[`mockups/screenshots/README.md`](../mockups/screenshots/README.md)). Rendering never edits a mockup:
if an image and its screen disagree, the screen wins.

The tokens mirror `AppTokens` of the Flutter client: `--accent`, `--bg`,
`--surface`, `--border`, `--text`, `--muted`, `--success`, `--error`,
`--warning`, `--user-bubble`, `--tool-*`, `--code-*`, the `--sp-*` scale and the
`--r-*` radii. Layout rules: `< 600` phone (bottom navigation, full-screen
routes), `600–1024` one pane plus rails, `>= 1024` desktop (rail, master/detail,
multi-pane). A widget never hard-codes a colour or a gap.

## 3. Screen inventory (PLAN.md §7)

All 42 screens are **done** as HTML. "States" lists the variants the file shows.

| # | Screen | File | States |
|---|---|---|---|
| 1 | Server URL (first launch) | `screens/01-server-url.html` | unreachable, wrong scheme |
| 2 | Pairing — QR | `screens/02-pairing-qr.html` | expired, camera denied |
| 3 | Pairing — code | `screens/03-pairing-code.html` | wrong/expired, rate limited |
| 4 | Certificate trust | `screens/04-certificate.html` | pinning, changed certificate |
| 5 | Network profile | `screens/05-network-profile.html` | public profile without hardening |
| 6 | Session list | `screens/06-sessions.html` | empty, offline, duplicate cwd, crashed |
| 7 | New session | `screens/07-session-new.html` | roots outside allow-list, duplicate cwd |
| 8 | Chat — streaming | `screens/08-chat-streaming.html` | streaming, thinking, caret |
| 9 | Search in conversation | `screens/09-chat-search.html` | no hits, case toggle |
| 10 | Composer and slash commands | `screens/10-composer.html` | attachments, no commands |
| 11 | Composer — steer/follow-up | `screens/11-composer-steer.html` | busy_streaming refusal |
| 12 | Message queue | `screens/12-queue.html` | cancel, cleared by the child |
| 13 | Model and thinking | `screens/13-header-model.html` | provider without key |
| 14 | Run status | `screens/14-status-sheet.html` | compacting, retrying, aborted |
| 15 | Session statistics | `screens/15-stats.html` | zero messages, unknown cost |
| 16 | Branch tree | `screens/16-tree.html` | large tree, error branch |
| 17 | Fork from a message | `screens/17-fork.html` | before/at, cancel |
| 18 | Clone, rename, switch | `screens/18-clone-rename.html` | empty/duplicate name |
| 19 | Export | `screens/19-export.html` | no messages, failed download |
| 20 | Direct bash console | `screens/20-bash.html` | non-zero exit, truncated output |
| 21 | Extension dialogs | `screens/21-dialogs.html` | select/confirm/input/editor, timeout |
| 22 | Notify, status and widget | `screens/22-notifications.html` | cleared status, long widget |
| 23 | Server settings | `screens/23-server-settings.html` | viewer read-only, managed mode |
| 24 | Errors and log tail | `screens/24-logs-errors.html` | extension error, retry |
| 25 | Connection states | `screens/25-connection.html` | online/offline/reconnect/replay |
| 26 | Desktop multi-session | `screens/26-desktop-tabs.html` | background run in a closed tab |
| 27 | Multi-session dashboard | `screens/27-desktop-dashboard.html` | 8 sessions (limit), crashed |
| 28 | Host file browser | `screens/28-files.html` | outside roots, binary file |
| 29 | PTY terminals | `screens/29-terminals.html` | shell exited, limit reached |
| 30 | Git panel | `screens/30-git.html` | write disabled/enabled, conflict |
| 31 | Global search | `screens/31-search.html` | no results, stale cache |
| 32 | Background tasks | `screens/32-tasks.html` | stopped, task without a port |
| 33 | MCP configuration | `screens/33-mcp.html` | server exits, duplicate name |
| 34 | Updates | `screens/34-updates.html` | managed mode, failed update |
| 35 | Themes and language | `screens/35-themes.html` | missing theme variables, RTL |
| 36 | Goal mode | `screens/36-goal-mode.html` | budget exhausted, achieved |
| 37 | pi packages | `screens/37-packages.html` | install failure, conflict |
| 38 | Wrap-up and handoff | `screens/38-wrapup.html` | wrap-up timeout, handoff failure |
| 39 | Two-mode login | `screens/39-login.html` | wrong password, revoked token |
| 40 | Offline queue | `screens/40-offline.html` | server restarted while queued |
| 41 | Log viewer and audit | `screens/41-log-viewer.html` | rotation, audit-only |
| 42 | Markdown editor | `screens/42-markdown-editor.html` | unsaved close, read-only root |

## 4. Approval checklist (Phase 2 exit criterion)

A reviewer signs off one line per area; the sign-off freezes the widgets for
`app/`. A rejected line becomes an issue, not a silent edit.

- [ ] Theme: dark and light read well in both device widths.
- [ ] Shell: phone bottom bar and desktop rail are the same navigation.
- [ ] Session list: status, cwd, model, context and queue depth read at a glance.
- [ ] Session creation: the host directory and the trust posture are clear.
- [ ] Chat: user/assistant/tool/status/error entries are distinguishable.
- [ ] Markdown: code, lists, links and the thinking block render as designed.
- [ ] Composer: send, steer, follow-up, slash commands and attachments are obvious.
- [ ] Queue and offline: what waits, of which kind, and how to drop it.
- [ ] Dialogs: answerable without losing the conversation; the countdown reads.
- [ ] Connection states: offline, reconnecting and replayed are unmistakable.
- [ ] Every state of the states board (`screens/00-states.html`) was reviewed.

## 5. What is next

1. Review the gallery and the states board; collect edits as issues.
2. Iterate on the HTML until the checklist is signed.
3. Implement `app/` screen by screen against these files, reusing the tokens in
   `mockups/assets/styles.css` and the markdown engine in
   `packages/piui-markdown`.

The first Flutter prototype of the mockup (theme, shell, MockPiApi, timeline,
composer) is in git history (`2d64335`…`6c53487`) and can seed `app/` when its
time comes; the HTML files are the visual contract.
