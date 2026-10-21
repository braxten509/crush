"""The restart after an update (owner, 2026-10-20: "automatically installs new
versions ... and restarts it"). In the sandbox with the real engine, a chat
is listed open, the page holds the open chats for a restart
(app_restart_hold) and asks Electron to relaunch, as the update button does
when its run ends:
- the app's process ends and a new one starts and connects again by itself;
- the listed chat is still open afterwards (without the hold, the last app
  out empties the list).

Usage: tests/check.sh relaunch_check.py
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
KEPT = {"id": "relaunch-kept-0001", "dir": "/tmp/home/proj", "title": "Kept across the restart"}


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def app_pids():
    """The Electron main processes (the program started with --probe)."""
    out = subprocess.run(["pgrep", "-af", "electron.* --probe "], capture_output=True, text=True).stdout
    return {int(line.split()[0]) for line in out.splitlines() if "--type=" not in line}


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 60)
time.sleep(1)
H.list_open_chat(KEPT)
before = app_pids()
check(bool(before), f"the app runs ({before})")
started = os.path.getmtime(probe_path)
asked = run("""
  const held = await window.crushBridge.invoke('app_restart_hold');
  setTimeout(() => window.crushBridge.window('relaunch'), 200);
  return held;
""") or {}
check(asked.get("ok") is True, f"the open chats are held for the restart ({asked})")

end = time.time() + 40
after = set()
while time.time() < end:
    after = app_pids()
    if after and not (after & before):
        break
    time.sleep(0.3)
check(bool(after) and not (after & before), f"the old app ends and a new one starts ({before} → {after})")
back = wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected" and os.path.getmtime(probe_path) > started + 1, 60)
check(bool(back), "the new app connects again by itself")
time.sleep(2)
ids = H.open_chat_ids()
check(KEPT["id"] in ids, f"the listed chat is still open after the restart ({ids})")
finish()
