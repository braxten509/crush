"""Your answers in a pop-up (owner, 2026-10-20: clicking "Your answers to
the questions" opens a pop-up with the questions and the answers; picked
"just your picks"). In the real window on the hidden screen, with a chat
whose question form was answered:
- the answers line is a button, and a real click on it opens the pop-up;
- the pop-up shows each question, the picked choice by its label (found in
  the `crush ask` call before the line), the free-text answer and the note.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/answers.json answers_popup.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
spot = None
end = time.time() + 15
while time.time() < end and not spot:
    spot = run("""
      const el = document.querySelector('main.conversation [data-answers-open]');
      if (!el) return '';
      el.scrollIntoView({ block: 'center' });
      const r = el.getBoundingClientRect();
      return { x: r.x + 60, y: r.y + r.height / 2, tag: el.tagName };
    """)
    time.sleep(0.3)
check(bool(spot) and spot.get("tag") == "BUTTON", f"the answers line is a button ({spot})")
time.sleep(0.4)
if spot:
    H.click(spot["x"], spot["y"])
sheet = ""
end = time.time() + 10
while time.time() < end and not sheet:
    sheet = run("return document.querySelector('[data-probe=\"answers-sheet\"]')?.innerText ?? '';") or ""
    time.sleep(0.2)
shot(out_dir, "answers")
check(bool(sheet), "a click opens the answers pop-up")
for text in ["How should the one ring work?", "Desktop hears every open chat", "Note: only for now", "Anything else?", "Keep it quiet"]:
    check(text in sheet, f"the pop-up shows {text!r}")
check("Per-folder hand-off" not in sheet, "only the picked choice shows")
finish()
