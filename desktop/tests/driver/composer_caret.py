"""The typing box never scrolls under 15 lines (owner, 2026-10-20: "the ghost
text is not centered with the cursor"). Its lines are taller than its own
line height, so Chromium scrolled it a few pixels to show the caret and kept
that after the text went: the words sat above the cursor. In the real window
on the hidden screen: type three lines, move the caret, delete them; the box
is never scrolled.

Usage: tests/check.sh --fixture tests/fixtures/model-menu.json composer_caret.py
"""
import os, sys, time
sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, key, probe_eval, probe_heavy, type_text, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
STATE = """(async () => { const t = document.querySelector('textarea.input');
  window.__probeEval = { top: t.scrollTop, value: t.value }; })();"""

wait_for(probe_path, lambda p: p.get("ready") and p.get("fonts") == "loaded", 40)
time.sleep(1.5)
box = (((probe_heavy(probe_path) or {}).get("heavy") or {}).get("composer") or {}).get("textarea")
check(bool(box), "the typing box shows")
H.click(box["x"] + box["w"] / 2, box["y"] + box["h"] / 2)
time.sleep(0.4)
type_text("one")
key("shift+Return")
type_text("two")
key("shift+Return")
type_text("three")
key("Up", "Up", "Down")
time.sleep(0.4)
got = probe_eval(probe_path, STATE) or {}
check(got.get("top") == 0, f"with three lines and the caret moved, the box is not scrolled ({got})")
key("ctrl+End")
for _ in range(len("one\ntwo\nthree")):
    key("BackSpace")
time.sleep(0.5)
got = probe_eval(probe_path, STATE) or {}
check(got.get("value") == "" and got.get("top") == 0, f"emptied, the ghost text sits unscrolled on the cursor's line ({got})")
finish()
