"""The open chat's orange edge never slides in from outside while the chat
list folds to the strip and opens again (owner, 2026-10-21: "the orange bars
come from outside, so we need to smooth that transition"). In the real
window on the hidden screen: the driver records the list's side at 60 frames
a second while Alt+S folds and opens the list, and finds the orange pixels
of every frame. The open chat's edge sits at the same x in the list and in
the strip, so in every frame the edge is one bar at that x: never left of
it, never two bars, never a frame without one.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/long-chat.json list_fold.py
"""
import os, subprocess, sys, time

import numpy as np

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, key, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
side = None
end = time.time() + 20
while time.time() < end and not side:
    side = run("""
      const list = document.querySelector('nav.chats');
      const row = list?.querySelector('[aria-current="page"], .selected');
      const main = document.querySelector('main.conversation');
      if (!list || !row || !main) return '';
      const v = getComputedStyle(document.documentElement).getPropertyValue('--vermilion').trim();
      // The list's side up to the conversation's thread line, which moves
      // with the column and is not the edge this checks.
      return { w: Math.floor(list.getBoundingClientRect().right / 2) * 2, h: Math.floor(innerHeight / 2) * 2, edge: Math.round(row.getBoundingClientRect().left), color: v };
    """)
    time.sleep(0.3)
check(bool(side), f"the list shows with the open chat ({side})")
if not side:
    finish()
print("NOTE", side)
hexcolor = side["color"].lstrip("#")
target = np.array([int(hexcolor[i:i + 2], 16) for i in (0, 2, 4)], dtype=np.int16) if len(hexcolor) == 6 else None
check(target is not None, f"the theme's vermilion is a plain color ({side['color']})")
if target is None:
    finish()

ox, oy = H._screen(0, 0)
video = os.path.join(out_dir, "fold.mkv")
if os.path.exists(video):
    os.remove(video)
rec = subprocess.Popen(["ffmpeg", "-loglevel", "error", "-f", "x11grab", "-framerate", "60", "-video_size", f"{side['w']}x{side['h']}",
                       "-i", f"{os.environ.get('DISPLAY', ':0')}+{ox},{oy}", "-c:v", "ffv1", video], stdin=subprocess.PIPE)
time.sleep(0.6)
key("alt+s")
time.sleep(0.8)
shot(out_dir, "strip")
key("alt+s")
time.sleep(0.8)
rec.communicate(b"q", timeout=20)
shot(out_dir, "list")

raw = subprocess.run(["ffmpeg", "-loglevel", "error", "-i", video, "-f", "rawvideo", "-pix_fmt", "rgb24", "-"], capture_output=True, timeout=120).stdout
frame = side["w"] * side["h"] * 3
frames = np.frombuffer(raw, dtype=np.uint8)[: len(raw) // frame * frame].reshape(-1, side["h"], side["w"], 3)
edge = side["edge"]
outside, doubled, missing = [], [], []
for n, f in enumerate(frames):
    near = (np.abs(f.astype(np.int16) - target).sum(axis=2) < 40)
    # Columns holding a bar (not a glyph) in the edge's own lane, left of
    # the conversation's thread line, which moves with the column.
    cols = np.nonzero(near[:, : edge + 12].sum(axis=0) >= 6)[0]
    rows = np.nonzero(near[:, max(0, edge - 1): edge + 4].any(axis=1))[0]
    runs = 0 if not len(rows) else 1 + int((np.diff(rows) > 2).sum())
    if len(cols) and cols.min() < edge - 1:
        outside.append((n, int(cols.min())))
    if len(cols) and cols.max() > edge + 8:
        outside.append((n, int(cols.max())))
    if runs > 1:
        doubled.append(n)
    if runs == 0:
        missing.append(n)
print("NOTE frames", len(frames), "outside", outside[:12], "doubled", doubled[:12], "missing", missing[:12])
check(len(frames) > 60, f"the fold was recorded ({len(frames)} frames)")
check(not outside, f"the orange edge never shows away from its place ({len(outside)} frames: {outside[:8]})")
check(not doubled, f"the orange edge is never two bars ({len(doubled)} frames: {doubled[:8]})")
check(not missing, f"the orange edge never vanishes ({len(missing)} frames: {missing[:8]})")
finish()
