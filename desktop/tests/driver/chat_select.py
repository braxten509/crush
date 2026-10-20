"""A mouse selection in the chat with composer_focus_only on (owner,
2026-10-20): the highlight stays (focusing the typing box would end it in
Chromium), nothing is copied by itself (Ctrl+C and the right-click menu
copy), Ctrl+C copies it, Esc clears it and gives the typing box the
focus back, and a typed letter goes to the typing box.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu-focus-only.json chat_select.py
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, probe_eval, probe_heavy, select_words, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
CLIP = [sys.executable, "-I", os.path.join(os.path.dirname(__file__), "clip.py")]


def clipboard():
    return subprocess.run([*CLIP, "get-text"], capture_output=True, text=True, timeout=10).stdout


def page(script):
    return probe_eval(probe_path, f"window.__probeEval = {script};")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
words = [w for w in (((probe_heavy(probe_path).get("heavy") or {}).get("chat") or {}).get("words") or []) if w.get("text", "").isalpha()]
pair = next(((a, b) for a, b in zip(words, words[1:]) if a.get("mid") == b.get("mid") and a["rect"]["y"] == b["rect"]["y"]), None)
if not check(pair is not None, "two words side by side in a reply"):
    finish()
first, last = pair
subprocess.run([*CLIP, "set-text", "before"], check=True, timeout=10)
check(select_words(probe_path, first["text"], last["text"], first.get("mid")), f"dragged over {first['text']!r} to {last['text']!r}")
time.sleep(1.5)
selected = page("document.getSelection().toString()")
check(first["text"] in (selected or "") and last["text"] in (selected or ""), f"the selection is still there after 1.5 s ({selected!r})")
check(page("document.activeElement?.localName ?? ''") != "textarea", "the typing box has not taken the focus while text is selected")
check(clipboard() == "before", f"letting go copies nothing by itself ({clipboard()!r})")
key("ctrl+c")
end = time.time() + 5
while time.time() < end and clipboard() == "before":
    time.sleep(0.2)
got = clipboard()
check(first["text"] in got and last["text"] in got, f"Ctrl+C copies the selection ({got!r})")
key("Escape")
p = wait_for(probe_path, lambda p: (p.get("composer") or {}).get("focused") is True, 5)
check(page("document.getSelection().toString()") == "", "Esc clears the selection")
check((p.get("composer") or {}).get("focused") is True, "and the typing box has the focus again")
check(select_words(probe_path, first["text"], last["text"], first.get("mid")), "selected again")
time.sleep(1)
key("x")
p = wait_for(probe_path, lambda p: (p.get("composer") or {}).get("value") == "x", 5)
check((p.get("composer") or {}).get("value") == "x", f"a typed letter goes to the typing box ({(p.get('composer') or {}).get('value')!r})")
finish()
