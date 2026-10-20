"""Pictures of the app's screens for a design review (owner, 2026-10-20):
the chat with its visuals, each Settings section, a yes/no question, the
collapsed list and a new chat, from the real window on the hidden screen
into tests/out/look_tour/. No checks beyond the window opening.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json look_tour.py
"""
import os, subprocess, sys, time

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


def shot(name, wait=0.8):
    time.sleep(wait)
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, name + ".png")])


def click(selector, text=None):
    find = f"[...document.querySelectorAll({selector!r})]" + (f".find((e) => e.textContent.trim().startsWith({text!r}))" if text else "[0]")
    point = run(f"const el = {find}; if (!el) return null; const r = el.getBoundingClientRect(); return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};")
    if point and "x" in point:
        xdo("mousemove", round(point["x"]), round(point["y"]))
        xdo("click", 1)
        return True
    return False


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(2)
xdo("mousemove", 5, 985)
shot("01-chat", 1.5)
run("document.querySelector('.timeline')?.scrollBy(0, 400); return true;")
shot("02-chat-scrolled")
run("window.__probe.askYesNo({ label: 'Update', question: 'Install updates for AGY?', description: 'AGY 1.4.2 → 1.4.3. Each CLI runs its own updater in the background.' }); return true;")
shot("03-yes-no")
xdo("key", "Escape")
time.sleep(0.5)
if click("nav.chats button.settings"):
    shot("04-settings")
    names = run("return [...document.querySelectorAll('.settings-nav .row')].map((r) => r.textContent.trim());") or []
    for i, name in enumerate(names[:12]):
        if click(".settings-nav .row", name):
            shot(f"05-settings-{i:02d}-{''.join(c for c in name.lower() if c.isalnum())[:16]}", 0.6)
    click(".settings-nav button.back")
    time.sleep(0.8)
run("window.__probe.runAction('chat.new'); return true;")
shot("06-new-chat", 1.2)
xdo("key", "alt+s")
shot("07-list-collapsed", 1.0)
xdo("key", "alt+s")
finish()
