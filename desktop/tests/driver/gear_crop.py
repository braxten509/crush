"""Scratch reproduction: capture the Settings gear at each zoom."""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, read_probe, shot, wait_for, window_geometry, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


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


MEASURE = """
  const box = (el) => { if (!el) return null; const r = el.getBoundingClientRect(); return [r.x, r.y, r.width, r.height]; };
  const btn = document.querySelector('button.settings');
  const icon = btn && btn.querySelector('svg.icon');
  const label = btn && btn.querySelector('span.label');
  const fs = getComputedStyle(document.documentElement).fontSize;
  return { fontSize: fs, btn: box(btn), icon: box(icon), label: box(label) };
"""

wid = app_window()
check(wid is not None, "the app window opens")
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
wait_for(probe_path, lambda p: p.get("ready") and p.get("fonts") == "loaded", 30)
time.sleep(1.5)
ox, oy = window_geometry()[:2]

for name, zoom in [("compact", "0.6667"), ("twelve", "1"), ("sixteen", "1.3333")]:
    run(f"document.documentElement.style.setProperty('--zoom', '{zoom}'); return true;")
    time.sleep(0.6)
    m = run(MEASURE) or {}
    image = shot(out_dir, f"gear-{name}")
    json.dump({"zoom": zoom, "origin": [ox, oy], "image": os.path.basename(image), "measure": m},
              open(os.path.join(out_dir, f"gear-{name}.json"), "w"), indent=1)
    check(bool(m.get("icon")) and not m.get("error"), f"{name}: the gear is measured ({m})")
finish()
