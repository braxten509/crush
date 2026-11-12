"""The update's question about the engines (owner, 2026-11-12: "when updating
crush it should ask me if I want to restart the engine or not"). In the
sandbox with the real engine, the page does what the update button does when
its run ends, once for each answer:
- Keep engines running: the app starts again and joins the same engine, and
  a chat listed open stays open (without the restart hold, the last app out
  empties the list);
- Restart engines: the engine stops first (engines_stop), and the app that
  starts next starts a new one.

Usage: tests/check.sh update_engines.py
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


def engine():
    """The engine the window is joined to, as (pid, when it started): in the
    sandbox a new engine can get the old one's pid again."""
    info = H.engine_get(probe_path, "/v1/engine") or {}
    return info.get("pid"), info.get("started_at")


def restart(stop_engines):
    """The update's restart; True once a new app is connected (another app
    process, joined to an engine as another client)."""
    before = app_pids()
    old = (H.probe_section(probe_path, "connection") or {}).get("client_id")
    run(("await window.crushBridge.invoke('engines_stop', { dirs: [''] });" if stop_engines else "") + """
      await window.crushBridge.invoke('app_restart_hold');
      setTimeout(() => window.crushBridge.window('relaunch'), 500);
      return true;
    """)
    # The probe hands its last script to every app that starts: the new app
    # must find a harmless one, or it restarts too, again and again.
    run("return 'quiet';")
    end = time.time() + 40
    after = set()
    while time.time() < end:
        after = app_pids()
        if after and not (after & before):
            break
        time.sleep(0.3)
    if not after or (after & before):
        print(f"no new app process within 40 s ({before} → {after})", flush=True)
        return False

    def joined(p):
        c = p.get("connection") or {}
        return c.get("state") == "connected" and c.get("client_id") not in ("", None, old) and bool((c.get("engine") or {}).get("pid"))

    if joined(wait_for(probe_path, joined, 90) or {}):
        return True
    print(f"the new app did not join within 90 s ({H.probe_section(probe_path, 'connection')})", flush=True)
    return False


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 60)
first = engine()
check(bool(first[0]) and bool(first[1]), f"the app's engine runs ({first})")

H.list_open_chat(KEPT)
check(restart(False), "Keep engines running: the app starts again")
kept = engine()
check(kept == first, f"and joins the same engine ({first} → {kept})")
ids = H.open_chat_ids()
check(KEPT["id"] in ids, f"the listed chat is still open after the restart ({ids})")

check(restart(True), "Restart engines: the app starts again")
new = engine()
check(bool(new[1]) and new[1] != kept[1], f"on a new engine ({kept} → {new})")
finish()
