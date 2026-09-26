# pi-ui mockup

The clickable mockup of the pi-ui client: the **whole UI of `app/`** with
`MockPiApi` and fake data, no server and no network. A screen is reviewed and
approved here, then it becomes a screen of `app/` unchanged — the widgets, the
theme tokens and the routes are shared.

## Run it

```bash
cd mockups

# Linux desktop (the review build for a workstation).
flutter run -d linux

# Android (the review build for a phone: sideload the debug APK).
flutter run -d <device>
flutter build apk --debug
```

## What is here

| Path | What it is |
|---|---|
| `lib/core/theme/` | The design tokens (`AppTokens`), the two `ThemeData`s and the layout breakpoints. |
| `lib/core/router.dart` | `go_router` routes and the adaptive shell branches. |
| `lib/features/` | One directory per feature, named after the screen it owns. |
| `lib/widgets/` | Widgets shared by more than one feature. |
| `lib/core/data/` | `MockPiApi`: the same streams and commands the real client uses, backed by a scenario driver. |
| `test/` | Widget tests: the shell at both breakpoints, and one test per screen as it lands. |

## Layout rules

* `< 600` logic pixels: phone. `NavigationBar`, full-screen routes.
* `>= 1024`: desktop. `NavigationRail`, master/detail panes.
* Between them: one pane plus a rail.
* No widget hard-codes a colour, a gap or a radius: everything reads
  `context.tokens` (or `context.markdownStyle` for markdown).

## Checks

```bash
dart fix --apply && dart format . && flutter analyze && flutter test
```

The review checklist and the screen inventory live in `docs/mockups.md`.
