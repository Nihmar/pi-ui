#!/usr/bin/env python3
"""Builds mockups/screenshots/index.html: one page with every rendered screen.

The sections and captions come from mockups/index.html, the images from the PNGs this
folder holds next to it: the script makes a thumbnail per image with ImageMagick and writes
the gallery that reviews them. The full-size renders are produced by
mockups/tools/shoot_screenshots.mjs.

Usage:
  python3 mockups/tools/make_gallery.py [--force]    # --force rebuilds every thumbnail
"""
import html
import pathlib
import re
import subprocess
import sys

MOCKUPS = pathlib.Path(__file__).resolve().parent.parent
SCREENSHOTS = MOCKUPS / "screenshots"
THEMES = ("dark", "light")
THUMB_WIDTH = 300
FORCE = "--force" in sys.argv

# The app renders are not mockup screens: each one is captioned here.
APP = [
    ("01-sessions-list.png", "Sessions list — streaming, ready, crashed and exited"),
    ("02-chat.png", "Chat — thinking, tool card, approval dialog, steer queue, composer"),
    ("03-files.png", "Files — the roots of the workspace"),
    ("04-git-panel.png", "Git — status, diff and log of a working tree"),
    ("05-settings.png", "Settings — device, server policy and updates"),
]


def magick(*args):
    """Runs ImageMagick, whichever name it is installed under."""
    for name in ("magick", "convert"):
        if subprocess.run(["which", name], capture_output=True).returncode == 0:
            return subprocess.run([name, *args], check=True)
    raise SystemExit("ImageMagick is required for the thumbnails (magick or convert)")


def sections():
    """[(title, [(screen file, caption)])] as mockups/index.html groups them."""
    source = (MOCKUPS / "index.html").read_text(encoding="utf8")
    groups = []
    for chunk in source.split('<section class="group">')[1:]:
        head = re.search(r"<h2>(.*?)</h2>", chunk, re.S)
        title = html.unescape(re.sub(r"<.*?>", " ", head.group(1))).strip() if head else "Screens"
        caption_re = re.compile(
            r'<a class="thumb" href="screens/([^"]+)">.*?<div class="caption">(.*?)</div>', re.S)
        entries = []
        for match in caption_re.finditer(chunk):
            caption = html.unescape(re.sub(r"<.*?>", " ", match.group(2)))
            caption = re.sub(r"\s+", " ", caption).strip()
            entries.append((match.group(1), re.sub(r"^[★\d]+\s*", "", caption)))
        if entries:
            groups.append((title, entries))
    return groups


def device(screen):
    """`mobile` or `desktop`: the width the mockup was drawn for."""
    source = (MOCKUPS / "screens" / screen).read_text(encoding="utf8")
    return re.search(r'data-device="([a-z]+)"', source).group(1)


def thumbnails():
    """One thumbnail per render, skipping the ones already built."""
    for theme in THEMES:
        for image in sorted((SCREENSHOTS / theme).glob("*.png")):
            target = SCREENSHOTS / "thumbs" / theme / image.name
            if target.exists() and not FORCE:
                continue
            target.parent.mkdir(parents=True, exist_ok=True)
            magick(str(image), "-resize", f"{THUMB_WIDTH}x", "-strip", str(target))


def app_card(name, caption):
    return f"""      <figure class="card">
        <a href="app/{name}"><img src="app/{name}" alt="{html.escape(caption)}" loading="lazy"></a>
        <figcaption>{html.escape(caption)}</figcaption>
      </figure>
"""


def card(screen, caption):
    name = f"{pathlib.Path(screen).stem}.png"
    return f"""      <figure class="card">
        <a href="dark/{name}">
          <img data-dark="thumbs/dark/{name}" data-light="thumbs/light/{name}"
               src="thumbs/dark/{name}" alt="{html.escape(caption)}" loading="lazy">
        </a>
        <figcaption>
          <span class="name">{html.escape(caption)}</span>
          <span class="links">
            <span class="device">{device(screen)}</span>
            <a href="dark/{name}">dark</a>
            <a href="light/{name}">light</a>
            <a href="../screens/{screen}">html</a>
          </span>
        </figcaption>
      </figure>
"""

PAGE = """<!doctype html>
<html lang="en" data-theme="dark">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>pi-ui — rendered screens</title>
<style>
  :root {{ --bg:#0f1115; --surface:#161a21; --border:#232936; --text:#e6e9ef; --muted:#8b93a3; --accent:#6ea8fe; }}
  html[data-theme="light"] {{ --bg:#f6f7f9; --surface:#fff; --border:#e2e6ec; --text:#101828; --muted:#667085; --accent:#1f5fd0; }}
  * {{ box-sizing:border-box; }}
  body {{ margin:0; background:var(--bg); color:var(--text);
         font:14px/1.45 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif; }}
  header {{ position:sticky; top:0; z-index:5; display:flex; gap:16px; align-items:center;
            padding:14px 22px; background:var(--surface); border-bottom:1px solid var(--border); }}
  header h1 {{ margin:0; font-size:16px; }}
  header .count {{ color:var(--muted); font-size:12.5px; }}
  header button {{ margin-left:auto; padding:8px 14px; border-radius:8px; border:1px solid var(--border);
                   background:transparent; color:var(--text); cursor:pointer; font-size:13px; }}
  main {{ padding:22px; }}
  p.intro {{ color:var(--muted); max-width:80ch; }}
  h2 {{ font-size:14px; margin:26px 0 12px; color:var(--muted); text-transform:uppercase; letter-spacing:.06em; }}
  .grid {{ display:grid; grid-template-columns:repeat(auto-fill,minmax(260px,1fr)); gap:18px; }}
  .grid.app img {{ max-height:420px; object-fit:contain; }}
  .card {{ margin:0; background:var(--surface); border:1px solid var(--border); border-radius:12px; padding:10px; }}
  .card img {{ width:100%; height:auto; display:block; border-radius:8px; background:var(--bg); }}
  figcaption {{ display:flex; flex-direction:column; gap:6px; margin-top:10px; }}
  .name {{ font-size:12.5px; }}
  .links {{ display:flex; gap:10px; font-size:11.5px; color:var(--muted); }}
  .links a {{ color:var(--accent); text-decoration:none; }}
  a {{ color:var(--accent); }}
  .device {{ margin-right:auto; text-transform:uppercase; letter-spacing:.08em; }}
</style>
</head>
<body>
<header>
  <h1>pi-ui — rendered screens</h1>
  <span class="count">{screens} mockup screens (dark + light) · {app} app screens · 2×</span>
  <button id="theme">Theme: dark</button>
</header>
<main>
  <p class="intro">A frozen look at the UI for reviews without a display: every screen of
  <code>mockups/screens/</code> rendered in both themes, plus a few screens of the Flutter
  app itself. The live mockup is <a href="../index.html">../index.html</a>; the mockup is the
  visual contract, these PNGs only mirror it. Regenerate with
  <code>node mockups/tools/shoot_screenshots.mjs</code> and
  <code>python3 mockups/tools/make_gallery.py</code>.</p>
  <h2>App screens (Flutter renders)</h2>
  <div class="grid app">
{app_cards}  </div>
{blocks}</main>
<script>
  const button = document.getElementById('theme');
  button.addEventListener('click', () => {{
    const root = document.documentElement;
    const next = root.getAttribute('data-theme') === 'dark' ? 'light' : 'dark';
    root.setAttribute('data-theme', next);
    button.textContent = 'Theme: ' + next;
    for (const image of document.querySelectorAll('img[data-' + next + ']')) {{
      image.src = image.getAttribute('data-' + next);
    }}
  }});
</script>
</body>
</html>
"""


def main():
    thumbnails()
    rendered = sorted(path.name for path in (SCREENSHOTS / "dark").glob("*.png"))
    if not rendered:
        raise SystemExit("no renders found: run mockups/tools/shoot_screenshots.mjs first")

    blocks = []
    for title, entries in sections():
        cards = "".join(
            card(screen, caption)
            for screen, caption in entries
            if f"{pathlib.Path(screen).stem}.png" in rendered
        )
        if cards:
            blocks.append(f'  <h2>{html.escape(title)}</h2>\n  <div class="grid">\n{cards}  </div>\n')

    app_cards = "".join(
        app_card(name, caption)
        for name, caption in APP
        if (SCREENSHOTS / "app" / name).exists()
    )
    page = PAGE.format(
        screens=len(rendered),
        app=app_cards.count("<figure"),
        app_cards=app_cards,
        blocks="".join(blocks),
    )
    (SCREENSHOTS / "index.html").write_text(page, encoding="utf8")
    print(f"wrote screenshots/index.html ({len(rendered)} screens, {page.count('<figure')} cards)")


if __name__ == "__main__":
    main()
