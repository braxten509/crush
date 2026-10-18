#!/usr/bin/env python3
"""Capture the built front end (desktop/dist) in a hidden browser.

Serves dist/ on a private local port, opens it in headless Chromium (nothing is
shown on screen), waits for the fonts, and saves a screenshot. Used to compare
the build with the approved picture.

Usage: capture.py OUT.png [--size 1600x992] [--zoom 1] [--query "a=b"]
"""
import argparse
import functools
import http.server
import os
import socketserver
import sys
import threading

from playwright.sync_api import sync_playwright

HERE = os.path.dirname(os.path.abspath(__file__))
DIST = os.path.normpath(os.path.join(HERE, "..", "..", "dist"))


class Quiet(http.server.SimpleHTTPRequestHandler):
    def log_message(self, *args):
        pass


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("out")
    parser.add_argument("--size", default="1600x992")
    parser.add_argument("--zoom", default="1")
    parser.add_argument("--query", default="")
    args = parser.parse_args()
    width, height = (int(v) for v in args.size.lower().split("x"))
    if not os.path.isfile(os.path.join(DIST, "index.html")):
        sys.exit(f"no build in {DIST}; run vite build first")

    server = socketserver.TCPServer(("127.0.0.1", 0), functools.partial(Quiet, directory=DIST))
    threading.Thread(target=server.serve_forever, daemon=True).start()
    query = f"zoom={args.zoom}" + (f"&{args.query}" if args.query else "")
    url = f"http://127.0.0.1:{server.server_address[1]}/?{query}"
    try:
        with sync_playwright() as p:
            browser = p.chromium.launch(headless=True)
            page = browser.new_page(viewport={"width": width, "height": height}, device_scale_factor=1)
            errors = []
            page.on("pageerror", lambda e: errors.append(str(e)))
            page.goto(url)
            page.evaluate("document.fonts.ready.then(() => document.fonts.size)")
            page.wait_for_timeout(400)
            page.screenshot(path=args.out)
            browser.close()
    finally:
        server.shutdown()
    for e in errors:
        print("page error:", e, file=sys.stderr)
    print(f"saved {args.out} ({width}x{height}, zoom {args.zoom})")
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main())
