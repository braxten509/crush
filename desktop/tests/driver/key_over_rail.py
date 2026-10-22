"""A rail key covers the rail (owner, 2026-10-22: "the A's boxes are not
drawing over the line", a picture of the vermilion rail running through
the "A" box). In the real window on the hidden screen, the pixels inside
each key where the rail crosses it must be the key's wash, not the rail's
vermilion.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json key_over_rail.py
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, shot, wait_for, window_geometry

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


# The rail's middle column and, for each shown key, the rows inside its
# border, plus the colours both should be drawn in.
WHERE = """
  const rail = document.querySelector('main.conversation > .rail');
  const keys = [...document.querySelectorAll('main.conversation .slot:not(.parked) .frame > .key')]
    .filter((k) => k.getBoundingClientRect().height > 0);
  if (!rail || keys.length === 0) return null;
  const r = rail.getBoundingClientRect();
  const css = (el, p) => getComputedStyle(el)[p];
  return {
    x: r.x + r.width / 2,
    rail: css(rail, 'backgroundColor'),
    keys: keys.map((k) => { const b = k.getBoundingClientRect(); return { text: k.textContent, left: b.left, right: b.right, top: b.top, bottom: b.bottom, wash: css(k, 'backgroundColor'), border: parseFloat(css(k, 'borderTopWidth')) }; }),
  };
"""


def rgb(text):
    return tuple(int(float(v)) for v in text[text.index("(") + 1:text.index(")")].split(",")[:3])


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
end = time.time() + 15
got = None
while time.time() < end and not got:
    got = run(WHERE)
    if not got or got.get("error"):
        got = None
        time.sleep(0.3)
check(bool(got), f"the chat shows the rail and at least one key ({got})")
if got:
    time.sleep(1.6)  # past the key's settle motion
    path = shot(out_dir, "keys")
    data = subprocess.run(["magick", path, "-depth", "8", "rgb:-"], capture_output=True, check=True).stdout
    w = int(subprocess.run(["magick", "identify", "-format", "%w", path], capture_output=True, text=True).stdout)
    ox, oy = window_geometry()[:2]
    rail, x = rgb(got["rail"]), round(ox + got["x"])
    for k in got["keys"]:
        check(k["left"] < got["x"] < k["right"], f"key {k['text']} sits across the rail ({k['left']:.1f}-{k['right']:.1f}, rail at {got['x']:.1f})")
        wash = rgb(k["wash"])
        # The first and last rows inside the border, clear of the letter's ink.
        rows = [round(oy + k["top"] + k["border"] + 1), round(oy + k["bottom"] - k["border"] - 1.5)]
        for y in rows:
            px = tuple(data[(y * w + x) * 3 + i] for i in range(3))
            near = lambda c: max(abs(px[i] - c[i]) for i in range(3)) <= 12
            check(near(wash) and not near(rail), f"key {k['text']} covers the rail at y={y}: {px} (wash {wash}, rail {rail})")
finish()
