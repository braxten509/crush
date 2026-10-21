"""The Settings page (owner, 2026-10-19: a full page, the chat list swapped
for the section names, the right panel stepping aside) and the right panel's
own switch (not the terminal's compact mode). In the real window on the
hidden screen, with real mouse clicks: Settings opens the page with no sheet;
a section click shows that section; "Show the right panel" off hides the panel back
in the chats without asking the engine to change compact mode, and on brings
it back; Back to chats and Esc both return. Pictures go to
tests/out/settings_page/.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json settings_page.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


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


def center(selector, text=None):
    find = f"[...document.querySelectorAll({json.dumps(selector)})]" + (f".find((e) => e.textContent.trim().startsWith({json.dumps(text)}))" if text else "[0]")
    return run(f"""
      const el = {find};
      if (!el) return null;
      el.scrollIntoView({{ block: 'nearest' }});
      const r = el.getBoundingClientRect();
      return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};
    """)


def click(selector, text=None):
    point = center(selector, text)
    if not point:
        return False
    xdo("mousemove", geo_x + round(point["x"]), geo_y + round(point["y"]))
    xdo("click", 1)
    time.sleep(0.5)
    return True


def view():
    return run("""
      return {
        page: Boolean(document.querySelector('[data-probe="settings-page"]')),
        nav: Boolean(document.querySelector('.settings-nav')),
        chats: Boolean(document.querySelector('nav.chats')),
        panel: Boolean(document.querySelector('[data-probe="panel"]')),
        sheets: document.querySelectorAll('[data-dialog]').length,
        title: document.querySelector('[data-probe="settings-page"] .title')?.textContent.trim() ?? '',
        rows: [...document.querySelectorAll('[data-probe="settings-page"] .row .name')].map((e) => e.textContent.trim()),
      };
    """)


def compact_posts():
    try:
        with open(probe_path + ".api.jsonl") as f:
            return [l for l in f if '"config/compact"' in l]
    except FileNotFoundError:
        return []


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 30)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(1.5)
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])

before = view()
check(before["chats"] and before["panel"] and not before["page"], f"the chats show with the right panel first ({before})")
subprocess.run(["import", "-window", wid, os.path.join(out_dir, "chat.png")], check=True)

# 1. Settings opens the page: nav on the left, no panel, no sheet.
click("nav.chats button.settings")
v = view()
check(v["page"] and v["nav"] and not v["chats"], f"Settings opens the page with the sections on the left ({v})")
check(not v["panel"] and v["sheets"] == 0, "the right panel steps aside and no sheet opens")
check(v["title"] == "Appearance" and "Show the right panel" in v["rows"], f"it starts on Appearance ({v['title']}, {v['rows']})")
subprocess.run(["import", "-window", wid, os.path.join(out_dir, "settings.png")], check=True)

# 2. A section click shows that section.
click(".settings-nav .row", "Typing")
v = view()
check(v["title"] == "Typing" and "Keep focus on the typing box" in v["rows"], f"a click on Typing shows its rows ({v['title']}, {v['rows']})")

# 3. Show the right panel off hides the panel in the chats, without the engine's compact mode.
click(".settings-nav .row", "Appearance")
click('[data-probe="settings-page"] [role="switch"][aria-label="Show the right panel"]')
off = run("return document.querySelector('[data-probe=\"settings-page\"] [aria-label=\"Show the right panel\"]').getAttribute('aria-checked');")
check(off == "false", f"the Show the right panel switch turns off ({off})")
click(".settings-nav button.back")
v = view()
check(v["chats"] and not v["page"], "Back to chats returns to the chats")
check(not v["panel"], "with the switch off the right panel stays hidden")
subprocess.run(["import", "-window", wid, os.path.join(out_dir, "no-panel.png")], check=True)

# 4. Ctrl+, opens it again; switch back on; Esc returns with the panel.
xdo("key", "--clearmodifiers", "ctrl+comma")
time.sleep(0.6)
check(view()["page"], "Ctrl+, opens the Settings page")
click('[data-probe="settings-page"] [role="switch"][aria-label="Show the right panel"]')
xdo("key", "Escape")
time.sleep(0.6)
v = view()
check(v["chats"] and not v["page"] and v["panel"], f"Esc returns to the chats and the panel is back ({v})")
check(compact_posts() == [], "the switch never asked the engine to change the terminal's compact mode")
finish()
