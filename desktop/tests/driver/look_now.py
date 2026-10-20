"""Pictures of how the app looks now (design work, 2026-10-19): the open
chat, then the model picker and settings, each from the hidden screen into
tests/out/look_now/. No checks beyond the window opening.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/picture.json look_now.py
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


def app_window(timeout=20):
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
        best = None
        for wid in r.stdout.split():
            g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
            d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
            if "WIDTH" in d and (best is None or int(d["WIDTH"]) * int(d["HEIGHT"]) > best[1]):
                best = (wid, int(d["WIDTH"]) * int(d["HEIGHT"]))
        if best and best[1] > 100 * 100:
            return best[0]
        time.sleep(0.3)
    return None


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


wid = app_window()
if not check(wid is not None, "the app window opens"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
wait_for(probe_path, lambda p: p.get("ready") and p.get("fonts") == "loaded", 30)
time.sleep(2)
subprocess.run(["import", "-window", wid, os.path.join(out_dir, "main.png")], check=True)
for dialog in ["models", "settings"]:
    run(f"window.__probe.openDialog('{dialog}', {{}}); return true;")
    time.sleep(1)
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, dialog + ".png")], check=True)
    run(f"window.__probe.closeDialog('{dialog}'); return true;")
    time.sleep(0.5)
finish()
