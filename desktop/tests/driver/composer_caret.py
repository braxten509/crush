"""Alt+Backspace in the typing box (owner, 2026-10-20: "alt+backspace doesn't
work, which I need"). In the real window on the hidden screen: type three
words, press Alt+Backspace; the word before the caret is gone.

Usage: tests/check.sh --fixture tests/fixtures/model-menu.json composer_caret.py
"""
import os, sys, time
sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, key, probe_eval, probe_heavy, type_text, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
STATE = """(async () => { const t = document.querySelector('textarea.input');
  window.__probeEval = { value: t.value }; })();"""

wait_for(probe_path, lambda p: p.get("ready") and p.get("fonts") == "loaded", 40)
box = None
end = time.time() + 10
while time.time() < end and not box:
    box = (((probe_heavy(probe_path) or {}).get("heavy") or {}).get("composer") or {}).get("textarea")
    if not box:
        time.sleep(0.1)
if not check(bool(box), "the typing box shows"):
    finish()
H.click(box["x"] + box["w"] / 2, box["y"] + box["h"] / 2)
type_text("one two three")
key("alt+BackSpace")
got = {}
end = time.time() + 3
while time.time() < end:
    got = probe_eval(probe_path, STATE) or {}
    if got.get("value") == "one two ":
        break
    time.sleep(0.05)
check(got.get("value") == "one two ", f"Alt+Backspace deletes the word before the caret ({got.get('value')!r})")
finish()
