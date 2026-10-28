"""A sliding page stops at its own edge: going into Settings and back, the
side list never paints over the frame line left of its column (owner,
2026-10-27: Settings' list slid over the left line, which went for a
moment). In the real window on the hidden screen, the driver records the
window's left side at 60 frames a second while Settings opens and closes,
and counts the line's pixels down the column in every frame: never fewer
than at rest.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json slide_edges.py
"""
import os, subprocess, sys, time

import numpy as np

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def until(script, timeout=10):
    end = time.time() + timeout
    while time.time() < end:
        if run(script) is True:
            return True
        time.sleep(0.2)
    return False


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
check(until("return Boolean(document.querySelector('nav.chats button.settings'));"), "the chat list shows")
side = run("""
  const line = document.querySelector('.vertical').getBoundingClientRect();
  const col = document.querySelector('.chats-column').getBoundingClientRect();
  const v = getComputedStyle(document.documentElement).getPropertyValue('--rule-frame').trim();
  return { x: Math.round(line.left), top: Math.ceil(col.top), bottom: Math.floor(col.bottom), w: 64, h: Math.floor(innerHeight / 2) * 2, color: v };
""")
print("NOTE", side)
hexcolor = (side or {}).get("color", "").lstrip("#")
check(len(hexcolor) == 6, f"the frame line is a plain color ({side})")
if len(hexcolor) != 6:
    finish()
line = np.array([int(hexcolor[i:i + 2], 16) for i in (0, 2, 4)], dtype=np.int16)

ox, oy = H._screen(0, 0)
video = os.path.join(out_dir, "slides.mkv")
if os.path.exists(video):
    os.remove(video)
rec = subprocess.Popen(["ffmpeg", "-loglevel", "error", "-f", "x11grab", "-framerate", "60", "-video_size", f"{side['w']}x{side['h']}",
                       "-i", f"{os.environ.get('DISPLAY', ':0')}+{ox},{oy}", "-c:v", "ffv1", video], stdin=subprocess.PIPE)
end = time.time() + 5
while time.time() < end and not (os.path.exists(video) and os.path.getsize(video) > 4096):
    time.sleep(0.2)
time.sleep(0.3)
run("document.querySelector('nav.chats button.settings').click(); return true;")
check(until("return Boolean(document.querySelector('.settings-nav')) && !document.querySelector('.chats-column').getAnimations({ subtree: true }).length;"), "Settings opened")
time.sleep(0.3)
run("document.querySelector('.settings-nav button.back').click(); return true;")
check(until("return Boolean(document.querySelector('nav.chats')) && !document.querySelector('.settings-nav') && !document.querySelector('.chats-column').getAnimations({ subtree: true }).length;"), "back at the chats")
time.sleep(0.3)
rec.communicate(b"q", timeout=20)

raw = subprocess.run(["ffmpeg", "-loglevel", "error", "-i", video, "-f", "rawvideo", "-pix_fmt", "rgb24", "-"], capture_output=True, timeout=120).stdout
size = side["w"] * side["h"] * 3
frames = np.frombuffer(raw, dtype=np.uint8)[: len(raw) // size * size].reshape(-1, side["h"], side["w"], 3)
strip = frames[:, side["top"]: side["bottom"], side["x"]].astype(np.int16)
drawn = (np.abs(strip - line).sum(axis=2) < 24).sum(axis=1)
rest = int(drawn[0]) if len(drawn) else 0
short = [(n, int(c)) for n, c in enumerate(drawn) if c < rest - 4]
print("NOTE frames", len(frames), "line pixels at rest", rest, "of", side["bottom"] - side["top"], "short", short[:12])
check(len(frames) > 40, f"both slides were recorded ({len(frames)} frames)")
check(rest > (side["bottom"] - side["top"]) * 0.9, f"the line is drawn at rest ({rest} pixels)")
check(not short, f"the left frame line is whole in every frame ({len(short)} frames short: {short[:8]})")
finish()
