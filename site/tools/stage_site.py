#!/usr/bin/env python3
"""Stages the published site into one directory.

GitHub Pages serves a single directory, and this site wants three things under one root:
`site/` at the top (the landing page) and `mockups/` next to it (the browsable mockup plus
its rendered screenshots). This script is that copy — and it is the *same* command that runs
in CI (`.github/workflows/pages.yml`) and in a local preview, so what you look at is what
ships.

    python3 site/tools/stage_site.py                  # → _site/ at the repository root
    python3 site/tools/stage_site.py --out /tmp/site  # somewhere else
    python3 site/tools/stage_site.py --serve          # stage, then serve it on :8000

What is left out: the tooling (`site/tools/`, `mockups/tools/`) and every markdown file
inside the mockups, because the published site carries no raw markdown. The mockup itself is
copied verbatim, relative links included: it is browsable at `/mockups/` exactly as it is in
the repository.
"""

from __future__ import annotations

import argparse
import shutil
import sys
from pathlib import Path

SKIP_DIRS = {"tools"}
SKIP_SUFFIXES = {".md"}


def dir_size(path: Path) -> tuple[int, int]:
    files = [p for p in path.rglob("*") if p.is_file()]
    return len(files), sum(p.stat().st_size for p in files)


def copy_tree(source: Path, target: Path, *, name: str) -> int:
    """Copies one directory, skipping tooling and markdown; returns the file count."""
    copied = 0
    for item in sorted(source.rglob("*")):
        relative = item.relative_to(source)
        if item.is_dir():
            continue
        if any(part in SKIP_DIRS for part in relative.parts[:-1]) or relative.parts[0] in SKIP_DIRS:
            continue
        if item.suffix in SKIP_SUFFIXES:
            continue
        destination = target / relative
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(item, destination)
        copied += 1
    print(f"  {name}: {copied} files")
    return copied


def stage(root: Path, out: Path) -> int:
    site = root / "site"
    mockups = root / "mockups"
    for required in (site / "index.html", mockups / "index.html"):
        if not required.is_file():
            print(f"stage_site: {required} is missing — run this from the repository", file=sys.stderr)
            return 2
    # The destination is wiped before it is written, so it must never be the repository
    # itself nor either of the two directories being copied.
    destination = out.resolve()
    if destination in {root.resolve(), site.resolve(), mockups.resolve()} or any(
        protected in destination.parents for protected in (site.resolve(), mockups.resolve())
    ):
        print(f"stage_site: refusing to stage over the sources ({out})", file=sys.stderr)
        return 2
    if out.exists():
        shutil.rmtree(out)
    out.mkdir(parents=True)
    files = copy_tree(site, out, name="site/")
    files += copy_tree(mockups, out / "mockups", name="mockups/")
    count, total = dir_size(out)
    print(f"staged {count} files ({total / 1e6:.1f} MB) into {out}")
    return 0


def main() -> int:
    root = Path(__file__).resolve().parents[2]
    parser = argparse.ArgumentParser(description="Stage the published pi-ui site.")
    parser.add_argument("--out", default=str(root / "_site"), help="output directory (default: <repo>/_site)")
    parser.add_argument("--root", default=str(root), help=argparse.SUPPRESS)
    parser.add_argument("--serve", action="store_true", help="serve the staged directory after staging it")
    parser.add_argument("--port", type=int, default=8000, help="port for --serve (default: 8000)")
    args = parser.parse_args()

    out = Path(args.out).expanduser()
    if (code := stage(Path(args.root).resolve(), out)) != 0:
        return code
    if args.serve:
        from functools import partial
        from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer

        handler = partial(SimpleHTTPRequestHandler, directory=str(out))
        print(f"serving {out} on http://127.0.0.1:{args.port}/ — Ctrl-C to stop")
        try:
            ThreadingHTTPServer(("127.0.0.1", args.port), handler).serve_forever()
        except KeyboardInterrupt:
            print()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
