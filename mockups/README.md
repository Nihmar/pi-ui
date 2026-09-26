# pi-ui mockup — static HTML

The whole UI of pi-ui as plain HTML files: no build step, no server, no
dependencies. Open `index.html` in a browser and click through the screens.

```bash
xdg-open mockups/index.html     # file:///…/mockups/index.html
```

* `index.html` — the gallery: every screen of the plan as a scaled frame,
  grouped by area.
* `screens/00-states.html` — the mandatory-states board (streaming, watchdog,
  dialog timeout, quota, crash, offline, replay, wrap-up, empty, skeleton,
  scope, notifications).
* `screens/NN-<name>.html` — one file per screen, `NN` matching the inventory in
  `docs/mockups.md`.
* `assets/styles.css` — the design tokens (dark/light) and every component; the
  Flutter client reuses these values through `AppTokens`.
* `assets/app.js` — theme and device-width toggles, tabs, dialog countdowns.

Each screen page has a toolbar to switch **theme** and **device** (mobile 390 px
/ desktop 1280 px); the choice is remembered across pages. The gallery embeds
the screens as iframes and always shows each one in its default width.

The screen inventory, the states to check and the approval checklist live in
[`docs/mockups.md`](../docs/mockups.md).
