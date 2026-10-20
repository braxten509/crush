"""Clean lines (owner, 2026-10-20): no + marks anywhere, the lines keep their
gaps; no red rail without an open chat; the notice line's words start where
reply text starts. In the real window on the hidden screen.

Start leg (default, no chat open): no rail, and no sliver of it on screen;
the page frame's corners and joins and a sheet's corners are bare paper on
screen; no + mark in the page or the sheet.

Chat leg ("chat" argument, a chat open): the rail is there; a copy shows the
notice line, whose words start at the chat's text edge.

Usage: tests/check.sh --fixture tests/fixtures/model-menu.json clean_lines.py
       CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json clean_lines.py chat
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, probe_eval, probe_heavy, select_words, shot, wait_for, window_geometry

probe_path, out_dir = sys.argv[1], sys.argv[2]
CHAT = "chat" in sys.argv[3:]
MARKS = "document.querySelectorAll('.plus, .md-plus, .corner').length"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def pixels(path):
    """The screen picture as rows of (r, g, b), read through ImageMagick."""
    r = subprocess.run(["magick", path, "-depth", "8", "rgb:-"], capture_output=True, check=True)
    w, h = map(int, subprocess.run(["magick", "identify", "-format", "%w %h", path], capture_output=True, text=True).stdout.split())
    return w, h, r.stdout


def bare(picture, points, paper, reach=5):
    """The points whose square of reach real pixels round them holds anything
    but the paper: a + mark drawn at a corner fills it."""
    w, h, data = picture
    ox, oy = window_geometry()[:2]
    marked = []
    for x, y in points:
        cx, cy = round(ox + x), round(oy + y)
        for py in range(cy - reach, cy + reach + 1):
            for px in range(cx - reach, cx + reach + 1):
                i = (py * w + px) * 3
                if max(abs(data[i + k] - paper[k]) for k in range(3)) > 12:
                    marked.append((x, y))
                    break
            else:
                continue
            break
    return marked


wait_for(probe_path, lambda p: p.get("ready") and p.get("fonts") == "loaded", 40)
time.sleep(1.5)
paper = run("""
  const c = getComputedStyle(document.documentElement).getPropertyValue('--paper').trim().replace('#', '');
  return [0, 2, 4].map((i) => parseInt(c.slice(i, i + 2), 16));
""")

if not CHAT:
    screen = run("return { rail: !!document.querySelector('.conversation .rail'), start: !!document.querySelector('.start-layer') };")
    check(screen and screen["start"], f"a start screen shows ({screen})")
    check(screen and not screen["rail"], "no red rail without an open chat")
    # No sliver of the rail on screen: no vermilion pixel in the column's rail strip.
    strip = run("""
      const col = document.querySelector('main.conversation').getBoundingClientRect();
      const v = getComputedStyle(document.documentElement).getPropertyValue('--vermilion').trim().replace('#', '');
      return { x: col.x, y: col.y, h: col.height, px: parseFloat(getComputedStyle(document.documentElement).fontSize) / 16,
               red: [0, 2, 4].map((i) => parseInt(v.slice(i, i + 2), 16)) };
    """)
    picture = pixels(shot(out_dir, "start"))
    w, h, data = picture
    ox, oy = window_geometry()[:2]
    left, right = round(ox + strip["x"] + 26 * strip["px"]), round(ox + strip["x"] + 38 * strip["px"])
    red = sum(1 for y in range(round(oy + strip["y"]), round(oy + strip["y"] + strip["h"]))
              for x in range(left, right)
              if max(abs(data[(y * w + x) * 3 + k] - strip["red"][k]) for k in range(3)) < 30)
    check(red == 0, f"no red pixel where the rail runs ({red})")
    check(run(f"return {MARKS};") == 0, "no + mark in the page")
    # The frame's corners and joins: where each vertical line meets the top and bottom lines.
    corners = run("""
      const v = [...document.querySelectorAll('.vertical, .divider[aria-orientation="vertical"]')].map((e) => e.getBoundingClientRect());
      const hz = [...document.querySelectorAll('.horizontal')].map((e) => e.getBoundingClientRect());
      const top = Math.min(...hz.map((r) => r.y)), bottom = Math.max(...hz.map((r) => r.y));
      return v.flatMap((r) => [[r.x, top], [r.x, bottom]]);
    """)
    check(len(corners or []) >= 6, f"the frame has its corners and joins ({len(corners or [])})")
    marked = bare(picture, corners or [], paper)
    check(not marked, f"the frame's corners and joins are bare paper ({marked})")
    # A sheet's corners.
    run("window.__probe.openDialog('models', {}); return true;")
    time.sleep(1.2)
    sheet = run("""
      const f = document.querySelector('[data-kit-title]')?.parentElement?.getBoundingClientRect();
      return f && [[f.x, f.y], [f.right - 1, f.y], [f.x, f.bottom - 1], [f.right - 1, f.bottom - 1]];
    """)
    check(bool(sheet), "the model sheet opens")
    marked = bare(pixels(shot(out_dir, "sheet")), sheet or [], paper)
    check(not marked, f"the sheet's corners are bare paper ({marked})")
    check(run(f"return {MARKS};") == 0, "no + mark in the sheet")
    finish()

# Chat leg.
wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
time.sleep(1)
check(run("return !!document.querySelector('.conversation .rail');") is True, "an open chat has its red rail")
check(run(f"return {MARKS};") == 0, "no + mark in the chat")
words = [w for w in (((probe_heavy(probe_path).get("heavy") or {}).get("chat") or {}).get("words") or []) if w.get("text", "").isalpha()]
pair = next(((a, b) for a, b in zip(words, words[1:]) if a.get("mid") == b.get("mid") and a["rect"]["y"] == b["rect"]["y"]), None)
check(pair is not None and select_words(probe_path, pair[0]["text"], pair[1]["text"], pair[0].get("mid")), "two words in a reply are selected")
key("ctrl+c")
edges = None
end = time.time() + 5
while time.time() < end and not edges:
    edges = run("""
      const text = document.querySelector('[data-notice-kind] .text');
      const canvas = document.querySelector('.timeline .canvas, .timeline [class*="canvas"]');
      const col = document.querySelector('main.conversation').getBoundingClientRect();
      const px = parseFloat(getComputedStyle(document.documentElement).fontSize) / 16;
      return text && { text: text.getBoundingClientRect().x, chat: col.x + 66 * px,
                       canvas: canvas ? canvas.getBoundingClientRect().x + parseFloat(getComputedStyle(canvas).paddingLeft) : null };
    """)
    time.sleep(0.2)
shot(out_dir, "notice")
check(bool(edges), "copying shows the notice line")
if edges:
    chat = edges["canvas"] if edges["canvas"] is not None else edges["chat"]
    check(abs(edges["text"] - chat) < 0.5, f"the notice's words start at the chat's text edge ({edges})")
finish()
