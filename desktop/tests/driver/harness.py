"""Shared helpers for hidden-screen checks: PASS/FAIL lines, the app's probe
report, the real mouse and keyboard (xdotool) and screenshots."""
import json, os, subprocess, sys, time

failures = 0


def check(ok, what):
    global failures
    print(("PASS " if ok else "FAIL ") + what, flush=True)
    if not ok:
        failures += 1
    return ok


def read_probe(path, tries=50):
    for _ in range(tries):
        try:
            with open(path) as f:
                return json.load(f)
        except (OSError, ValueError):
            time.sleep(0.1)
    return {}


def wait_for(path, cond, timeout=20):
    end = time.time() + timeout
    p = {}
    while time.time() < end:
        p = read_probe(path, tries=1)
        if p and cond(p):
            return p
        time.sleep(0.2)
    return p


def xdo(*args):
    return subprocess.run(["xdotool", *map(str, args)], check=True, capture_output=True, text=True).stdout


def window_geometry(name, timeout=20):
    """Find the app's visible window by exact title and return its (x, y, w, h).
    GTK also makes tiny hidden helper windows with the same title, so only
    visible windows count and the largest one wins. None if none appears."""
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", f"^{name}$"], capture_output=True, text=True)
        best = None
        for wid in r.stdout.split():
            g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
            d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
            if "WIDTH" in d:
                box = (int(d["X"]), int(d["Y"]), int(d["WIDTH"]), int(d["HEIGHT"]))
                if best is None or box[2] * box[3] > best[2] * best[3]:
                    best = box
        if best and best[2] > 100:
            return best
        time.sleep(0.3)
    return None


def shot(out_dir, name):
    os.makedirs(out_dir, exist_ok=True)
    path = os.path.join(out_dir, f"{name}.png")
    subprocess.run(["import", "-window", "root", path], check=True)
    return path


def finish():
    print("ALL PASSED" if failures == 0 else f"{failures} FAILED", flush=True)
    sys.exit(1 if failures else 0)
