"""Dragging the chat's scroll bar never blanks the chat (owner, 2026-10-20:
"when I drag this and a lot of text scrolls, the entire chat goes blank
until I stop"). In the real window on the hidden screen, on a chat of 240
long messages: the driver presses the scroll bar's thumb with the real mouse
and drags it to the top in small steps, then back down, while the page
checks every frame whether the chat's middle shows a message (a point on a
row) or nothing (the empty room the list keeps for rows not drawn yet).

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/long-chat.json scroll_drag.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, shot, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
STEPS = 40


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
bar = None
end = time.time() + 20
while time.time() < end and not bar:
    bar = run("""
      const s = document.querySelector('main.conversation .scroller');
      if (!s || s.scrollHeight < s.clientHeight * 20) return '';
      const r = s.getBoundingClientRect();
      const thumb = Math.max(20, s.clientHeight * s.clientHeight / s.scrollHeight);
      const at = s.scrollTop / s.scrollHeight * s.clientHeight;
      return { x: r.right - 4, top: r.top, h: s.clientHeight, y: r.top + at + thumb / 2, thumb, range: s.scrollHeight };
    """)
    time.sleep(0.3)
check(bool(bar), f"the long chat shows with its scroll bar ({bar})")
if not bar:
    finish()

# Every frame: does the chat's middle show a row?
run("""
  const s = document.querySelector('main.conversation .scroller');
  const r = s.getBoundingClientRect();
  const pts = [0.3, 0.5, 0.7].map((f) => [r.left + r.width * 0.4, r.top + r.height * f]);
  window.__drag = { frames: 0, blank: 0, tops: [], on: true };
  const tick = () => {
    if (!window.__drag.on) return;
    const blank = pts.every(([x, y]) => !document.elementFromPoint(x, y)?.closest('.row'));
    window.__drag.frames += 1;
    if (blank) { window.__drag.blank += 1; window.__drag.tops.push(Math.round(s.scrollTop)); }
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return 1;
""")
sx, sy = H._screen(bar["x"], bar["y"])
xdo("mousemove", str(sx), str(sy))
time.sleep(0.2)
xdo("mousedown", "1")
top_y = H._screen(bar["x"], bar["top"] + bar["thumb"] / 2)[1]
for i in range(1, STEPS + 1):
    xdo("mousemove", str(sx), str(round(sy + (top_y - sy) * i / STEPS)))
    time.sleep(0.03)
for i in range(1, STEPS + 1):
    xdo("mousemove", str(sx), str(round(top_y + (sy - top_y) * i / STEPS)))
    time.sleep(0.03)
xdo("mouseup", "1")
time.sleep(0.3)
seen = run("window.__drag.on = false; return { frames: window.__drag.frames, blank: window.__drag.blank, tops: window.__drag.tops.slice(0, 12) };") or {}
moved = run("return Math.round(document.querySelector('main.conversation .scroller').scrollTop);")
shot(out_dir, "after-drag")
check((seen.get("frames") or 0) > 30, f"frames were watched during the drag ({seen.get('frames')})")
check(seen.get("blank") == 0, f"no frame of the drag showed a blank chat ({seen})")
finish()
