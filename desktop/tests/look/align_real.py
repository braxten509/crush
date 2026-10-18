#!/usr/bin/env python3
"""Alignment check on the renderer the app ships with.

The app draws with WebKit (WebKitGTK on Linux, WKWebView on a Mac), which can
place text a pixel away from where Chromium puts it. So the measured pixels come
from the real window's capture (tests/driver/look_real.py), and only the boxes
come from headless Chromium: the same CSS lays out both, each box gets a small
margin, and the align skill's screenshot checker finds the real ink inside it.
Ink it cannot measure reliably is reported as excluded, never as a pass.

Usage: align_real.py IMAGE --zoom 8/12 --out DIR [--size 1600x992]
                     [--marks tests/look/align-marks.js] [--id first-screen-compact]
Exit codes are the checker's: 0 pass, 1 mismatches or review pending, 2 bad input.
"""
import argparse
import functools
import http.server
import json
import os
import re
import shutil
import socketserver
import subprocess
import sys
import threading

from playwright.sync_api import sync_playwright

HERE = os.path.dirname(os.path.abspath(__file__))
DIST = os.path.normpath(os.path.join(HERE, "..", "..", "dist"))
ALIGN = os.path.expanduser("~/.agents/skills/align/scripts")
ICON_MARGIN = 4  # icons can paint to their box edge; the checker needs background around the ink
TEXT_MARGIN = 2


class Quiet(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass


def align_helpers():
    """The align skill's own in-page helpers: mark()/spacing() and workbenchAlign.collect()."""
    web = open(os.path.join(ALIGN, "align-web.mjs")).read()
    found = re.search(r"const MARK_HELPERS = `([\s\S]*?)`;", web)
    if not found:
        sys.exit("align-web.mjs no longer defines MARK_HELPERS; update align_real.py")
    return found.group(1), open(os.path.join(ALIGN, "align-browser.js")).read()


def grow(rect, n, width, height):
    x, y, w, h = rect
    x0, y0 = max(0, x - n), max(0, y - n)
    x1, y1 = min(width, x + w + n), min(height, y + h + n)
    return [x0, y0, x1 - x0, y1 - y0]


TEXT_RECTS = """() => Object.fromEntries([...document.querySelectorAll('[data-align-kind="text"]')].map((el) => {
  const range = document.createRange();
  range.selectNodeContents(el);
  const boxes = [...range.getClientRects()].filter((b) => b.width && b.height);
  if (!boxes.length) return [el.dataset.alignItem, null];
  const x = Math.min(...boxes.map((b) => b.left)), y = Math.min(...boxes.map((b) => b.top));
  const r = Math.max(...boxes.map((b) => b.right)), btm = Math.max(...boxes.map((b) => b.bottom));
  return [el.dataset.alignItem, [Math.floor(x), Math.floor(y), Math.ceil(r) - Math.floor(x), Math.ceil(btm) - Math.floor(y)]];
}))"""


def collect(size, zoom, marks, surface):
    width, height = size
    server = socketserver.TCPServer(("127.0.0.1", 0), functools.partial(Quiet, directory=DIST))
    threading.Thread(target=server.serve_forever, daemon=True).start()
    mark_helpers, browser_helper = align_helpers()
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True)
            page = browser.new_page(viewport={"width": width, "height": height}, device_scale_factor=1)
            page.goto(f"http://127.0.0.1:{server.server_address[1]}/?zoom={zoom}")
            page.evaluate("document.fonts.ready.then(() => document.fonts.size)")
            page.evaluate(mark_helpers)
            page.evaluate(open(marks).read())
            page.evaluate(browser_helper)
            collected = page.evaluate("s => window.workbenchAlign.collect(s)", surface)
            # A block-level label's box spans its whole column and can reach a neighbouring
            # line; measure the text itself instead (the union of its line boxes).
            collected["text_rects"] = page.evaluate(TEXT_RECTS)
            browser.close()
    finally:
        server.shutdown()
    if collected["device_scale"] != 1:
        sys.exit(f"device scale {collected['device_scale']}, expected 1")
    return collected


def manifest_for(collected, capture_id, image, size):
    width, height = size
    rows, ids = [], []
    for row in collected["rows"]:
        elements = []
        for e in row["elements"]:
            out = {k: e[k] for k in ("id", "kind", "text_kind", "group", "text", "column", "column_anchor", "source", "role", "text_lines") if e.get(k) is not None}
            if e["kind"] == "icon":
                out["rect"] = grow(e["rect"], ICON_MARGIN, width, height)
            else:
                tight = collected["text_rects"].get(e["id"]) or e["rect"]
                out["rect"] = grow(tight, TEXT_MARGIN, width, height)
                if e.get("reference_rect"):
                    # Full height of the text box: WebKit may sit the glyph a pixel lower than Chromium.
                    rx, _, rw, _ = e["reference_rect"]
                    out["reference_rect"] = [rx, out["rect"][1], rw, out["rect"][3]]
                    out["reference_text"] = e["reference_text"]
            elements.append(out)
            ids.append(e["id"])
        rows.append({k: row[k] for k in ("id", "label", "rect", "source", "center", "center_reason") if row.get(k) is not None} | {"elements": elements})
    return {
        "version": 2,
        "surface": collected["surface"],
        "inventory": [{"id": capture_id, "elements": ids}],
        "captures": [{"id": capture_id, "method": "screenshot", "image": image, "device_scale": 1,
                      "rows": rows, "spacing_groups": collected.get("spacing_groups", [])}],
    }


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("image")
    parser.add_argument("--zoom", required=True)
    parser.add_argument("--out", required=True)
    parser.add_argument("--size", default="1600x992")
    parser.add_argument("--marks", default=os.path.join(HERE, "align-marks.js"))
    parser.add_argument("--id", default="first-screen")
    args = parser.parse_args()
    size = tuple(int(v) for v in args.size.lower().split("x"))
    if not os.path.isfile(os.path.join(DIST, "index.html")):
        sys.exit(f"no build in {DIST}; run vite build first")
    os.makedirs(args.out, exist_ok=True)
    image = os.path.basename(args.image)
    shutil.copyfile(args.image, os.path.join(args.out, image))
    surface = {"id": args.id, "name": f"First screen at zoom {args.zoom}", "kind": "page", "selector": '[data-probe="shell"]'}
    manifest = manifest_for(collect(size, args.zoom, args.marks, surface), args.id, image, size)
    path = os.path.join(args.out, "align.json")
    with open(path, "w") as f:
        json.dump(manifest, f, indent=1)
    return subprocess.run([sys.executable, os.path.join(ALIGN, "align.py"), path, "--out", os.path.join(args.out, "results"), "--self-review"]).returncode


if __name__ == "__main__":
    sys.exit(main())
