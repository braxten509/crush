"""Look check in the real window, run inside the sandbox by run-in-sandbox.sh.
The app draws with WebKitGTK, not the Chromium the design capture uses, so this
captures the first screen as it really ships: at the approved picture's size
(1600 x 992), once at the default zoom (Compact, 8/12) and once at picture size
(12/12), which is what gets compared with the picture."""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
WIDTH, HEIGHT = 1600, 992


def app_window(timeout=20):
    """The id of the largest visible window titled "Crush"; GTK also makes tiny
    hidden helper windows with the same title."""
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
        best = None
        for wid in r.stdout.split():
            g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
            d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
            if "WIDTH" in d:
                area = int(d["WIDTH"]) * int(d["HEIGHT"])
                if best is None or area > best[1]:
                    best = (wid, area)
        if best and best[1] > 100 * 100:
            return best[0]
        time.sleep(0.3)
    return None


def settled(p, zoom):
    s = p.get("shell") or {}
    return (p.get("ready") and p.get("fonts") == "loaded" and p.get("zoom") is not None
            and abs(p["zoom"] - zoom) < 1e-6 and round(s.get("w", 0)) == WIDTH and round(s.get("h", 0)) == HEIGHT)


def capture(wid, name):
    # Two more probe reports after the page settled, so the last frame is on screen.
    time.sleep(0.5)
    path = os.path.join(out_dir, f"{name}.png")
    subprocess.run(["import", "-window", wid, path], check=True)
    return path


os.makedirs(out_dir, exist_ok=True)
wid = app_window()
if not check(wid is not None, "the app window opens"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, WIDTH, HEIGHT)

p = wait_for(probe_path, lambda p: settled(p, 8 / 12))
if check(settled(p, 8 / 12), f"first screen at {WIDTH}x{HEIGHT}, Compact (8/12), fonts loaded {p.get('shell')} zoom={p.get('zoom')} fonts={p.get('fonts')}"):
    capture(wid, "real-compact")

# Ctrl = steps the zoom up one twelfth at a time: 8 -> 12 is picture size.
xdo("windowfocus", "--sync", wid)
for _ in range(4):
    xdo("key", "--clearmodifiers", "ctrl+equal")
    time.sleep(0.2)
p = wait_for(probe_path, lambda p: settled(p, 1.0))
if check(settled(p, 1.0), f"first screen at picture size (12/12) after four Ctrl = presses, zoom={p.get('zoom')}"):
    capture(wid, "real-picture")

# Ctrl 0 returns to the default.
xdo("key", "--clearmodifiers", "ctrl+0")
p = wait_for(probe_path, lambda p: settled(p, 8 / 12))
check(settled(p, 8 / 12), f"Ctrl 0 returns to Compact, zoom={p.get('zoom')}")
finish()
