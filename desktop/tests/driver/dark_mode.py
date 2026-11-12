"""The chat's scroll bar is clickable (owner, 2026-10-20: "currently it's not
clickable since the resize for the vertical bar is overtaking the hitbox").
In the real window on the hidden screen, with a real wheel turn: the point on
the chat's scroll bar belongs to the chat, not to the right column's grip
beside it, and the wheel scrolls the chat.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json dark_mode.py
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
SCROLLER = "document.querySelector('.slot:not(.parked) .scroller, .scroller')"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def app_window(timeout=20):
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
        best = None
        for wid in r.stdout.split():
            g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
            d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
            if "WIDTH" in d and (best is None or int(d["WIDTH"]) * int(d["HEIGHT"]) > best[1]):
                best = (wid, int(d["WIDTH"]) * int(d["HEIGHT"]))
        if best and best[1] > 100 * 100:
            return best[0]
        time.sleep(0.3)
    return None


def poll(read, ok, timeout):
    end = time.time() + timeout
    value = read()
    while time.time() < end and not ok(value):
        time.sleep(0.05)
        value = read()
    return value


BAR = f"""
  const s = {SCROLLER};
  if (!s) return null;
  const r = s.getBoundingClientRect();
  const x = r.right - 3, y = r.top + r.height / 2;
  const hit = document.elementFromPoint(x, y);
  return {{ x, y, left: r.left, width: r.width,
           tall: s.scrollHeight > s.clientHeight + 10, inside: !!hit && s.contains(hit),
           hit: hit ? hit.tagName.toLowerCase() + '.' + String(hit.className) : null }};
"""

wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
# The recorded chat fits a full window: a short window makes it scroll.
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 420)
bar = poll(lambda: run(BAR), lambda b: bool(b) and "x" in b and b["tall"], 10)
if check(bar is not None and "x" in bar, f"the chat scroller is on the page ({bar})"):
    check(bar["tall"], "the chat is long enough to scroll")
    check(bar["inside"], f"the point on the scroll bar belongs to the chat, not the column grip (hit {bar['hit']})")
    g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
    geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
    xdo("mousemove", int(geo["X"]) + round(bar["left"] + bar["width"] / 2), int(geo["Y"]) + round(bar["y"]))
    before = run(f"return {SCROLLER}.scrollTop;")
    for _ in range(4):
        xdo("click", 4)
        time.sleep(0.05)
    moved = poll(lambda: run(f"return {SCROLLER}.scrollTop;"), lambda v: v is not None and v != before, 3)
    check(before is not None and moved is not None and moved != before, f"the wheel scrolls the chat ({before} to {moved})")
finish()
