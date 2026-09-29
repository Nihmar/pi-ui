# pi-ui client

The Flutter client of pi-ui: sessions running in host directories, streaming chat,
approvals, terminals, git and the server surfaces. It talks to the Go server over
REST (`/api/v1`) and WebSocket (`/ws/v1`) and never sees provider secrets.

The visual contract is the HTML mockup in [`../mockups`](../mockups): the theme
tokens, the breakpoints and every widget come from it (see
[`../docs/mockups.md`](../docs/mockups.md)).

## Run

```bash
cd app
flutter run -d linux        # desktop
flutter run -d <device>     # Android
```

The theme follows the server's `ui.theme` setting and the language its `ui.language`, so a
deployment meant to look and read a certain way says so once; a server that cannot answer
leaves the app on its defaults, and a language this build does not ship falls back to the
device.

The text lives in `lib/l10n/app_en.arb` (the source language) and `app_it.arb`, generated
into `AppLocalizations` by `flutter gen-l10n` (wired through `l10n.yaml`). A widget reads a
string as `context.l10n.someKey`; a literal in a widget is a string nobody can translate.
After adding a key, run `flutter gen-l10n` — the analyzer fails until the generated class
knows it.

The first launch is one pairing screen: type the server address (`pi-ui.local:8787`) or
paste the `piui://pair…` link the server printed, then the 6-character code. The server's
`pi-ui pair` prints that card on demand (and `serve` prints it on first run), with the
code, the link and a QR you can **scan** from Android; on Linux and Windows you type the
code or paste the link. A link the operator handed over already carries the TLS
fingerprint, so it is trusted without a prompt; a certificate you did not get over a link
is confirmed by its SHA-256 fingerprint once and pinned afterwards — a fingerprint that
changes is a hard failure, not a prompt. The device token is stored in the OS keystore;
the server keeps only its hash. `pi-ui status` lists the paired devices.

A session that finishes while the app is in the background tells the user through
the OS notification centre; everything else is already on the timeline.

## Checks

```bash
dart fix --apply && dart format . && flutter analyze && flutter test
```

The same checks run in CI (`.github/workflows/ci.yml`) together with a Linux and a
Windows build.

## Layout

| Path | What it is |
|---|---|
| `lib/core/theme/` | `AppTokens`, the two `ThemeData`s, breakpoints, markdown style. |
| `lib/core/api/` | REST + WebSocket client, DTOs, reconnection and replay. |
| `lib/core/models/` | The UI models the screens render (`SessionModel`, `ChatEntry`). |
| `lib/features/` | One directory per screen/feature. The chat header's model chip opens the picker, which asks pi itself (`get_available_models`, `set_model`) through the generic command passthrough. The Files branch browses the server's workspaces (`/workspaces`, `/fs/list`, `/files/read`) and renders markdown through `packages/piui-markdown`; the session menu's Git panel shows the working tree (`/git/status`) and commits, with the diff rendered by the chat's own diff view; the Terminal entry opens a PTY over the WebSocket (`terminal.*`) with a byte-exact scrollback; Settings shows this device, renders the server's policy from its own catalogue (a switch for a bool, a dropdown for an enum) and, for an admin, the update panel. |
| `lib/widgets/` | Widgets shared by more than one feature. |
