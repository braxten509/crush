"""The owner's fixes of 2026-10-20 (late), in the real window with real keys
and clicks, on the real engine and the fake AI server:
- a reply's footer names the model from the config ("Fake Large"), never
  "Unknown Model" (the agent CLIs were missing);
- while a reply runs and the box is empty, the Send square is Stop (owner's
  choice A); typing turns it back into Send and clearing it into Stop again;
  a click on Stop ends the run and the typing box keeps the focus.

Usage: tests/check.sh owner_fixes.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

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


def shot(name):
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, name + ".png")])


def rect(selector):
    return run(f"const e = document.querySelector({json.dumps(selector)}); if (!e) return null; const r = e.getBoundingClientRect(); return {{ x: r.x, y: r.y, w: r.width, h: r.height }};")


def click_at(r):
    xdo("mousemove", geo_x + round(r["x"] + r["w"] / 2), geo_y + round(r["y"] + r["h"] / 2))
    xdo("click", 1)


def key(*combos):
    for combo in combos:
        xdo("key", "--clearmodifiers", combo)


def until(script, timeout=10, step=0.2):
    end = time.time() + timeout
    while time.time() < end:
        got = run(script)
        if got is True:
            return True
        time.sleep(step)
    return False


def label_becomes(want, timeout=5):
    """Polls the Send square's label until it reads want; returns the last one."""
    end = time.time() + timeout
    got = label()
    while time.time() < end and got != want:
        time.sleep(0.05)
        got = label()
    return got


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])

# The new project's init question holds the keys until it is answered.
NOPE = "[...document.querySelectorAll('button')].find((e) => e.textContent.trim() === 'Not now')"
nope = run(f"const b = {NOPE}; if (!b) return null; const r = b.getBoundingClientRect(); return {{ x: r.x, y: r.y, w: r.width, h: r.height }};")
if nope:
    click_at(nope)
    until(f"return !{NOPE};", 5, 0.05)
field = rect(".composer textarea")
if not check(field is not None, "the typing box is on the page"):
    finish()
click_at(field)

# -- A reply's footer --
xdo("type", "--delay", "15", "--", "Seed turn short")
key("Return")
done = until("return [...document.querySelectorAll('.slot:not(.parked) .timeline [data-kind=footer]')].length > 0 && !document.querySelector('.slot:not(.parked) .timeline .status');", 20)
footer = run("return [...document.querySelectorAll('.slot:not(.parked) .timeline [data-kind=footer]')].map((e) => e.textContent.trim()).pop() ?? null;")
check(done and footer is not None, f"the reply finishes with its footer ({footer!r})")
check(footer is not None and "Unknown Model" not in footer and "Fake Large" in footer, f"the footer names the model from the config ({footer!r})")

# -- Stop --
label = lambda: run("return document.querySelector('.composer .send')?.getAttribute('aria-label') ?? null;")
click_at(rect(".composer textarea"))
xdo("type", "--delay", "15", "--", "leftlist slow reply")
key("Return")
working = until("return !!document.querySelector('.slot:not(.parked) .timeline .status');", 10)
check(working, "the slow reply runs (the live status shows)")
got = label_becomes("Stop")
check(got == "Stop", f"while it runs with an empty box, the square is Stop ({got})")
shot("stop")
xdo("type", "--delay", "15", "--", "x")
got = label_becomes("Send")
check(got == "Send", f"typing turns it back into Send ({got})")
key("BackSpace")
got = label_becomes("Stop")
check(got == "Stop", f"clearing the box turns it into Stop again ({got})")
click_at(rect(".composer .send"))
stopped = until("return !document.querySelector('.slot:not(.parked) .timeline .status');", 8)
check(stopped, "a click on Stop ends the run")
got = label_becomes("Send")
check(got == "Send", f"and the square is Send again ({got})")
check(run("return document.activeElement === document.querySelector('.composer textarea');") is True, "the typing box keeps the focus")
finish()
