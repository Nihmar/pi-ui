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

The first launch asks for the server URL and a pairing code: on the server,
`pi-ui status` prints the code and `pi-ui pair --url http://<host>:8787` renders a
scannable QR. The device token is stored in the OS keystore; the server keeps only
its hash.

## Checks

```bash
dart fix --apply && dart format . && flutter analyze && flutter test
```

## Layout

| Path | What it is |
|---|---|
| `lib/core/theme/` | `AppTokens`, the two `ThemeData`s, breakpoints, markdown style. |
| `lib/core/api/` | REST + WebSocket client, DTOs, reconnection and replay. |
| `lib/core/models/` | The UI models the screens render (`SessionModel`, `ChatEntry`). |
| `lib/features/` | One directory per screen/feature. |
| `lib/widgets/` | Widgets shared by more than one feature. |
