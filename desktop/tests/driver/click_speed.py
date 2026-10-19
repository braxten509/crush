"""Click speed (owner, 2026-10-19: "clicking a button takes a half second to
a second to show the dropdown"). In the real window on the hidden screen, a
real mouse click on a tool group's row must show it open quickly: the page
records the click and the moment the row's aria-expanded turns true, and the
check passes when every one of five clicks (open, close, open, ...) shows
within 100 ms. Before the fix a click waited 400 ms in the timeline and
400 ms more in the row for a possible double click.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/picture.json click_speed.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
LIMIT_MS = 100
CLICKS = 5


def app_window(timeout=20):
    """The largest visible window of the app, found by its class (as look_align.py)."""
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


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 30)
wid = app_window()
check(wid is not None, "the app window is on the hidden screen")
row = None
for _ in range(50):
    row = run("""
      const el = document.querySelector('.timeline .tool-row[aria-expanded]');
      if (!el) return null;
      el.scrollIntoView({ block: 'center' });
      await new Promise((r) => requestAnimationFrame(() => requestAnimationFrame(r)));
      const r = el.getBoundingClientRect();
      window.__clicks = [];
      window.__clickAt = 0;
      // Only the real mouse click starts the clock: the old code replayed the
      // click itself after its wait, which would restart it.
      window.addEventListener('click', (e) => { if (e.isTrusted) window.__clickAt = performance.now(); }, true);
      new MutationObserver(() => {
        window.__clicks.push({ expanded: el.getAttribute('aria-expanded'), ms: performance.now() - window.__clickAt });
      }).observe(el, { attributes: true, attributeFilter: ['aria-expanded'] });
      return { x: r.x + 40, y: r.y + r.height / 2, expanded: el.getAttribute('aria-expanded') };
    """)
    if row and "x" in row:
        break
    time.sleep(0.2)
check(bool(row and "x" in row), f"a tool group row is shown ({row})")
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
sx, sy = int(geo["X"]) + round(row["x"]), int(geo["Y"]) + round(row["y"])
for _ in range(CLICKS):
    xdo("mousemove", sx, sy)
    xdo("click", 1)
    time.sleep(0.9)  # longer than any double-click interval, so each click is a lone click
clicks = run("return window.__clicks;") or []
with open(os.path.join(out_dir, "report.json"), "w") as f:
    json.dump({"start": row, "clicks": clicks, "limit_ms": LIMIT_MS}, f, indent=2)
times = [round(c["ms"]) for c in clicks]
check(len(clicks) == CLICKS, f"each of {CLICKS} clicks opened or closed the group ({len(clicks)} changes: {times} ms)")
check(all(c["ms"] <= LIMIT_MS for c in clicks), f"every click showed within {LIMIT_MS} ms ({times} ms)")
finish()
