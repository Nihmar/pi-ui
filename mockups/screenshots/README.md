# Rendered screenshots

Frozen PNG renders of the UI, for a review that does not want to open 43 HTML files one by
one: **every mockup screen in both themes**, plus a handful of screens of the Flutter client
itself. The mockup stays the visual contract — these images only mirror it — and the live,
interactive version remains [`../index.html`](../index.html).

Open [`index.html`](index.html) here for the gallery: a thumbnail per screen, grouped the
way `../index.html` groups them, with the theme toggle and links to the full-size renders,
the light variant and the screen's own HTML.

## Layout

| Path | What it is |
|---|---|
| `index.html` | The gallery: app renders first, then every mockup screen with its caption. |
| `dark/NN-<name>.png` | The screen as the mockup draws it in the dark theme, 2×. |
| `light/NN-<name>.png` | The same in the light theme. |
| `thumbs/<theme>/NN-<name>.png` | 300 px wide thumbnails the gallery loads. |
| `app/*.png` | Screens of the Flutter client: real widgets, fake data. |

The images follow `../screens/NN-<name>.html` one to one, in the same order.

## Regenerating

```bash
node mockups/tools/shoot_screenshots.mjs      # every screen, dark + light, 2×
python3 mockups/tools/make_gallery.py         # thumbnails + screenshots/index.html
```

`shoot_screenshots.mjs` drives a headless Chromium over the DevTools Protocol and needs
nothing but Node 22 and a browser: it takes `--out`, `--themes`, `--scale`, `--only <prefix>`
and `--chrome` (otherwise `$PIUI_CHROME`, the Playwright cache and `PATH` are tried in that
order). Each capture hides the mockup toolbar and the review notes and clips to the device
frame, so a render is the screen and nothing else.

`make_gallery.py` reads the grouping and the captions from `../index.html`, so a new screen
shows up in the gallery as soon as its section lists it; `--force` rebuilds every thumbnail.
Both scripts take their paths from the repository, so any clone can regenerate the set.

## The app renders

`app/*.png` come from the Flutter client, not from the mockup: they are widget renders with
the real theme, real fonts and fixed sample data (`flutter test --update-goldens` on a
temporary shot test wired to the fake server harness). They exist to compare the app against
the mockup during review; treat the mockup file as the reference when the two differ.
