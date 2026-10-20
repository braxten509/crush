"""Question pictures (owner, 2026-10-20: "it only shows the first; when I hover
over the second it briefly shows and then never changes"). In the real window
on the hidden screen, a question with three picture choices: the real mouse
hovers each row twice round, so pictures already loaded show again, then the
arrow keys move the cursor; each step must show that row's picture.

The cause was components/ask/Picture.svelte reading its own `src` back after
writing it for a picture already loaded, which looped the effect until Svelte
stopped it.

Usage: tests/check.sh --no-app question_pictures.py
(the driver makes its pictures and its fixture in tests/out/question_pictures/)
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from apps import App
from harness import check, finish, key, probe_eval, shot, wait_for, window_geometry, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
CHAT = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
ROOT = os.path.join(os.path.dirname(__file__), "..", "..")
COLORS = ["red", "green", "blue"]

pictures = {}
for color in COLORS:
    path = os.path.join(out_dir, f"{color}.png")
    subprocess.run(["magick", "-size", "400x250", f"xc:{color}", path], check=True)
    pictures[color] = os.path.abspath(path)
fixture = json.load(open(os.path.join(ROOT, "tests", "fixtures", "model-menu.json")))
request = {"id": "q-pictures", "session_id": CHAT, "tool_call_id": "tc-pictures", "questions": [{
    "id": "look", "type": "single_choice", "label": "Look", "question": "Which one?", "description": "Pick a color.",
    "choices": [{"id": c, "label": c.title(), "image": pictures[c]} for c in COLORS]}]}
fixture["events"] = [{"at_ms": 2000, "entries": [{"kind": "event", "type": "question_batch_request", "event": "created", "payload": request}]}]
fixture_path = os.path.join(out_dir, "fixture.json")
json.dump(fixture, open(fixture_path, "w"))


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


STATE = """
  const pic = document.querySelector('[data-probe="question-picture"]');
  const img = pic?.querySelector('img');
  const rows = [...document.querySelectorAll('.choice')].map((c) => { const r = c.getBoundingClientRect();
    return { text: c.innerText.trim().split('\\n')[0].toLowerCase(), x: r.x + 40, y: r.y + r.height / 2, cursor: c.classList.contains('cursor') }; });
  return { img: img ? img.alt.replace('.png', '') : null, rows };
"""


def shown(want, timeout=4):
    """Waits for the picture of `want`; returns what showed last."""
    end = time.time() + timeout
    got = None
    while time.time() < end:
        got = run(STATE) or {}
        if got.get("img") == want:
            return got
        time.sleep(0.15)
    return got


app = App(args=("--fixture", fixture_path, "-s", CHAT))
wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
state = None
end = time.time() + 20
while time.time() < end and not (state and len(state.get("rows") or []) == 3):
    state = run(STATE)
    time.sleep(0.3)
if not check(state and len(state.get("rows") or []) == 3, f"the question shows its three choices ({state})"):
    app.stop()
    finish()
got = shown("red")
check(got.get("img") == "red", f"the first choice's picture shows at first ({got.get('img')})")

ox, oy = window_geometry()[:2]
rows = {row["text"]: row for row in state["rows"]}
for step, color in enumerate(["green", "blue", "red", "green", "blue"]):
    xdo("mousemove", round(ox + rows[color]["x"]), round(oy + rows[color]["y"]))
    got = shown(color)
    round_ = "back on it" if step >= 2 else "first visit"
    check(got.get("img") == color, f"hovering {color} ({round_}) shows its picture ({got.get('img')})")
    time.sleep(0.6)
    later = run(STATE) or {}
    check(later.get("img") == color, f"and {color}'s picture stays ({later.get('img')})")
shot(out_dir, "hovered")

# Off the rows, the arrow keys move the cursor and the picture follows.
xdo("mousemove", round(ox + 5), round(oy + 300))
time.sleep(0.4)
start = next((r["text"] for r in (run(STATE) or {}).get("rows", []) if r["cursor"]), None)
for _ in range(2):
    key("Down")
    time.sleep(0.4)
    cursor = next((r["text"] for r in (run(STATE) or {}).get("rows", []) if r["cursor"]), None)
    got = shown(cursor)
    check(cursor is not None and got.get("img") == cursor, f"Down moves the cursor to {cursor} and its picture shows ({got.get('img')}; from {start})")
app.stop()
finish()
