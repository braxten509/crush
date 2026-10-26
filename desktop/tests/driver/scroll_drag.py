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
import os, subprocess, sys, time

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
      const s = document.querySelector('main.conversation .slot:not(.parked) .scroller');
      if (!s || s.scrollHeight < s.clientHeight * 20) return '';
      s.scrollTop = (s.scrollHeight - s.clientHeight) / 2;
      const r = s.getBoundingClientRect();
      const thumb = Math.max(20, s.clientHeight * s.clientHeight / s.scrollHeight);
      const at = s.scrollTop / Math.max(1, s.scrollHeight - s.clientHeight) * (s.clientHeight - thumb);
      // Halfway down, the thumb's middle is the track's middle, whatever its least length.
      // The bar column's middle: the system's bar draws its thumb narrower than its column.
      return { x: r.right - (s.offsetWidth - s.clientWidth) / 2, top: r.top, h: s.clientHeight, y: r.top + s.clientHeight / 2, thumb, range: s.scrollHeight };
    """)
    time.sleep(0.3)
check(bool(bar), f"the long chat shows with its scroll bar ({bar})")
if not bar:
    finish()

# Every frame: does the chat's middle show a row?
run("""
  const s = document.querySelector('main.conversation .slot:not(.parked) .scroller');
  const r = s.getBoundingClientRect();
  const pts = [0.3, 0.5, 0.7].map((f) => [r.left + r.width * 0.4, r.top + r.height * f]);
  window.__drag = { frames: 0, blank: 0, tops: [], on: true, last: 0, gaps: [], start: s.scrollTop };
  const tick = (now) => {
    if (!window.__drag.on) return;
    if (window.__drag.last && now - window.__drag.last > 50) window.__drag.gaps.push(Math.round(now - window.__drag.last));
    window.__drag.last = now;
    const blank = pts.every(([x, y]) => !document.elementFromPoint(x, y)?.closest('.row'));
    window.__drag.frames += 1;
    window.__drag.min = Math.min(window.__drag.min ?? Infinity, s.scrollTop);
    window.__drag.max = Math.max(window.__drag.max ?? 0, s.scrollTop);
    if (blank) { window.__drag.blank += 1; window.__drag.tops.push(Math.round(s.scrollTop)); }
    requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return 1;
""")
# The real pixels: the chat's text area recorded at 60 frames a second
# while the bar is dragged; a frame with no text in it is blank.
area = run("""
  const r = document.querySelector('main.conversation .slot:not(.parked) .scroller').getBoundingClientRect();
  return { x: r.left + 66, y: r.top + 40, w: Math.floor((r.width - 110) / 2) * 2, h: Math.floor((r.height - 160) / 2) * 2 };
""")
ax, ay = H._screen(area["x"], area["y"])
video = os.path.join(out_dir, "drag.mkv")
if os.path.exists(video):
    os.remove(video)
rec = subprocess.Popen(["ffmpeg", "-loglevel", "error", "-f", "x11grab", "-framerate", "60", "-video_size", f"{area['w']}x{area['h']}",
                        "-i", f"{os.environ.get('DISPLAY', ':0')}+{ax},{ay}", "-c:v", "ffv1", video], stdin=subprocess.PIPE)
time.sleep(0.5)
sx, sy = H._screen(bar["x"], bar["y"])
xdo("mousemove", str(sx), str(sy))
time.sleep(0.2)
xdo("mousedown", "1")
time.sleep(0.2)
top_y = H._screen(bar["x"], bar["top"] + 3)[1]
for i in range(1, STEPS + 1):
    xdo("mousemove", str(sx), str(round(sy + (top_y - sy) * i / STEPS)))
    time.sleep(0.03)
low_y = H._screen(bar["x"], bar["top"] + bar["h"] - 80)[1]
for i in range(1, 2 * STEPS + 1):
    xdo("mousemove", str(sx), str(round(top_y + (low_y - top_y) * i / (2 * STEPS))))
    time.sleep(0.03)
xdo("mouseup", "1")
time.sleep(0.3)
rec.communicate(b"q", timeout=20)
stats = subprocess.run(["ffmpeg", "-loglevel", "error", "-i", video, "-vf", "signalstats,metadata=print:file=-", "-f", "null", "-"],
                       capture_output=True, text=True, timeout=120).stdout
lo = [float(l.split("=")[1]) for l in stats.splitlines() if "signalstats.YMIN=" in l]
hi = [float(l.split("=")[1]) for l in stats.splitlines() if "signalstats.YMAX=" in l]
spans = [h - l for l, h in zip(lo, hi)]
blank_frames = [i for i, span in enumerate(spans) if span < 40]
print("NOTE recorded", len(spans), "frames; blank", len(blank_frames), "first", blank_frames[:10], "least span", min(spans) if spans else None)
seen = run("window.__drag.on = false; const d = window.__drag; return { frames: d.frames, blank: d.blank, tops: d.tops.slice(0, 12), gaps: d.gaps, start: Math.round(d.start), min: Math.round(d.min), max: Math.round(d.max) };") or {}
print("NOTE", seen)
shot(out_dir, "after-drag")
check((seen.get("start") or 0) - (seen.get("min") or 0) > 20000 and (seen.get("max") or 0) - (seen.get("start") or 0) > 20000, f"the drag scrolled far both ways ({seen.get('start')} → {seen.get('min')} → {seen.get('max')})")
check((seen.get("frames") or 0) > 30, f"frames were watched during the drag ({seen.get('frames')})")
check(len(seen.get("gaps") or []) <= 1, f"at most one frame of the drag took over 50 ms ({seen.get('gaps')})")
check(len(spans) > 60 and not blank_frames, f"no recorded frame of the drag is blank ({len(blank_frames)} of {len(spans)})")
check(seen.get("blank") == 0, f"no frame of the drag showed a blank chat ({seen})")
finish()
