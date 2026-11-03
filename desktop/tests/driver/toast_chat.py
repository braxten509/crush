"""A toast's click shows its chat in the window already open (owner,
2026-11-03: a click on a toast for another folder's chat started a second
Crush window). The window is on proj; the toast's chat is beta's. The toast
click's helper call (window_show_chat, as electron/main.cjs makes it) must
switch the open window to beta in place and show the chat there, with no
second window.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/folder-switch.json toast_chat.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, probe_heavy, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
BETA = "b0b0b0b0-1111-4222-8333-000000000001"

wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
wait_for(probe_path, lambda p: len(((p.get("list") or {}).get("rows") or [])) >= 5, 20)
probe_eval(probe_path, "window.__noReload = true;")

report = probe_heavy(probe_path, invoke=[{"command": "window_show_chat", "args": {"projectDir": "/tmp/home/beta", "sessionId": BETA}}])
answer = ((report or {}).get("invoked") or [{}])[0]
check(answer.get("ok") is True and (answer.get("value") or {}).get("label") == "main" and (answer.get("value") or {}).get("created") is False,
      f"the click's chat goes to the open window ({answer})")

shown = wait_for(probe_path, lambda p: (p.get("connection") or {}).get("workspace_id") == "ws-beta"
                 and (p.get("connection") or {}).get("state") == "connected", 20)
check(((shown or {}).get("connection") or {}).get("workspace_id") == "ws-beta", f"the window switches to beta ({(shown or {}).get('connection')})")
deadline = time.time() + 5
selected = None
while time.time() < deadline:
    selected = probe_eval(probe_path, "window.__probeEval = { id: document.querySelector('nav.chats .chat.selected')?.getAttribute('data-id') ?? null, kept: window.__noReload === true };")
    if (selected or {}).get("id") == BETA:
        break
    time.sleep(0.3)
check((selected or {}).get("id") == BETA and (selected or {}).get("kept"), f"beta's chat shows, in place with no reload ({selected})")
time.sleep(0.5)
check(not os.path.exists(probe_path + ".w2"), "no second window opens")
finish()
