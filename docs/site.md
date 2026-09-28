# The published site: landing page and every-screen gallery

**Status:** the landing page is written and publishes together with the mockup.
**Where:** `site/` — static HTML/CSS/JS, no build step; `site/tools/` holds the two scripts
that keep it honest.
**Purpose:** one URL that tells the product story and shows the real screens, built from the
sources the repository already keeps — no second copy of the UI and no hand-kept list of
screens.

## 1. What the page says

| Section | What it carries |
|---|---|
| hero | the pitch, three calls to action, and the two app captures inside one panel |
| `#features` | what pi-ui adds to pi: one card per capability |
| `#screens` | **Every screen** — the generated gallery (§2 and §3) |
| `#parity` | mockup → app: the same screen as a mockup render and as the Flutter widget that implements it |
| `#architecture` | the layers and where each boundary sits |
| `#run` | from a clone to a phone in the same room, with the real commands |
| `#security` | what a deployment trusts and what never leaves the server |
| `#roadmap` | the eight phases, with the gaps named instead of hidden |

Nothing on the page invents a fact: the capability cards mirror the README, the roadmap mirrors
`docs/roadmap.md`, and every screen is a render of the mockup or of the client — see below.

## 2. The gallery is generated, never hand-written

The block between `<!-- gallery:begin -->` and `<!-- gallery:end -->` in `site/index.html` is
produced by `site/tools/build_gallery.py`. It reads:

* **the groups, the order and the captions** from `mockups/index.html` — the gallery the mockup
  already has. There is no second screen inventory to keep in step.
* **the renders** from `mockups/screenshots/{dark,light,thumbs}/`, including their real pixel
  dimensions.
* **the client's own screens** from `mockups/screenshots/app/`, where each file is one line in
  the script's `APP_RENDERS` map (`caption`, `mobile|desktop`). That map is the only hand-kept
  list in the script, because the app renders are not 2× the way the mockup's are, so their form
  factor cannot be read from the pixels.

Two details that keep the tiles honest:

* The mockup renders are captured at 2× and the app renders at 1×, so the **declared frame width
  in `mockups/index.html`** decides *mobile* vs *desktop* — measuring the PNG would classify a
  640-pixel-wide phone mockup as a tablet.
* Every tile carries `data-thumb-dark` and `data-thumb-light`, and the script swaps `src` when
  the theme changes, so a tile shows the same theme the page is in.

The block is **committed**: with JavaScript disabled the grid still lists every screen.
`build_gallery.py --check` fails when the committed block no longer matches the mockup, and the
Pages workflow runs exactly that before staging — a published site can never disagree with the
mockup it shows. Filters, counts (48 screens: 43 from the mockup, 5 from the client) and the
"the Flutter client" group are all computed, never typed.

## 3. Filtering and the lightbox

`site/assets/site.js` adds two things on top of the committed markup:

* **Filter chips** — *Area* (one per group), *Form* (phone/desktop) and *Source*
  (mockup/client), each with its count, plus a name search, a reset and an "nothing matches"
  note. The chips are `aria-pressed`, and `/` focuses the search box.
* **A lightbox** — clicking a tile opens the render at the size its form factor deserves (a phone
  capture centred at phone width, a desktop capture as wide as the viewport allows), with `←`/`→`
  to step through the *filtered* set, `Esc` to close, "Open the live screen" for the browsable
  mockup page and "Open at full size" for the PNG.

## 4. Staging and publishing

GitHub Pages serves one directory, and this site needs two things under one root: `site/` at the
top and `mockups/` next to it. `site/tools/stage_site.py` is that copy.

```bash
python3 site/tools/stage_site.py                  # → _site/ at the repository root (gitignored)
python3 site/tools/stage_site.py --out /tmp/site  # somewhere else
python3 site/tools/stage_site.py --serve          # stage, then serve it on :8000
```

The tooling directories (`site/tools/`, `mockups/tools/`) and every markdown file inside the
mockups are left out; the mockup itself is copied verbatim, relative links included.
`.github/workflows/pages.yml` runs `build_gallery.py --check`, then this script, and uploads
`_site/` — the same command a local `--serve` runs, so what is reviewed locally is what ships.

## 5. Local preview

Open the **staged** directory, not `site/index.html` directly: the page links to `mockups/`, which
only exists next to it once staged — a `file://` open of the source would 404 on those links
(and on nothing else).

```bash
python3 site/tools/stage_site.py --serve   # http://127.0.0.1:8000/
```

## 6. Conventions

* **Tokens only.** Colours, spacings and radii come from the variables in
  `site/assets/site.css` (`html[data-theme="light"]` carries the light palette); a section widget
  never hard-codes one.
* **The mockup stays the visual contract.** A screen changes in `mockups/` first; the landing page
  then shows the new render after one `build_gallery.py` run.
* **One section, one block**, inside `.wrap` for the shared measure, so a new section is an
  insertion rather than a rework.
* Adding a screen is two steps: add it to `mockups/index.html`, run
  `python3 site/tools/build_gallery.py`.
