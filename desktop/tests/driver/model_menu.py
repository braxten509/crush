"""The model menu (owner, 2026-10-19: the model button opens picker "B" over
itself). In the real window on the hidden screen, with real mouse clicks:
the button opens the menu just above it, right edges lined up; the agents
column lists the fixture's providers with the current one chosen and the
current model ticked; a click on another provider shows its models; a click
on a model saves it with the open chat (POST sessions/<id>/model in the
fixture's api.jsonl) and keeps the menu open; a click on an effort level saves that level; Esc and a click
outside close it. Pictures of the open menu go to tests/out/model_menu/.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json model_menu.py
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


def rect_of(selector):
    return run(f"""
      const el = document.querySelector({json.dumps(selector)});
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return {{ x: r.x, y: r.y, w: r.width, h: r.height, right: r.right, bottom: r.bottom }};
    """)


def button_by_text(column, text):
    return run(f"""
      const el = [...document.querySelectorAll('[data-menu="model"] [data-column="{column}"] button')]
        .find((b) => b.textContent.trim().startsWith({json.dumps(text)}));
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};
    """)


def click_at(point):
    xdo("mousemove", geo_x + round(point["x"]), geo_y + round(point["y"]))
    xdo("click", 1)


def menu_open():
    return run("return Boolean(document.querySelector('[data-menu=\"model\"]'));")


def saves(since):
    try:
        with open(probe_path + ".api.jsonl") as f:
            lines = [json.loads(l) for l in f if l.strip()]
    except FileNotFoundError:
        return []
    # A model is saved with the open chat (sessions/<id>/model; every chat
    # owns its model, owner 2026-10-24); the effort still goes to config/model.
    return [c for c in lines[since:] if c.get("method") == "POST"
            and (c.get("path") == "config/model" or (c.get("path") or "").endswith("/model") and (c.get("path") or "").startswith("sessions/"))]


def api_count():
    try:
        with open(probe_path + ".api.jsonl") as f:
            return sum(1 for l in f if l.strip())
    except FileNotFoundError:
        return 0


def open_menu():
    b = rect_of(".composer button.model")
    click_at({"x": b["x"] + b["w"] / 2, "y": b["y"] + b["h"] / 2})
    end = time.time() + 3
    while time.time() < end and not menu_open():
        time.sleep(0.05)
    time.sleep(0.4)  # the unfold has finished
    return b


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

# 1. The button opens the menu just above itself.
button = open_menu()
menu = rect_of('[data-menu="model"]')
check(menu is not None, "a click on the model button opens the model menu")
if menu is None:
    finish()
subprocess.run(["import", "-window", wid, os.path.join(out_dir, "open.png")], check=True)
gap = button["y"] - menu["bottom"]
check(0 <= gap <= 12, f"the menu sits just above the button (gap {gap:.1f} px)")
check(abs(menu["right"] - button["right"]) <= 1.5, f"its right edge lines up with the button's ({menu['right']:.1f} vs {button['right']:.1f})")
check(menu["x"] >= 0 and menu["y"] >= 0, "it stays inside the window")

# 2. The columns: providers, the current one chosen, the current model ticked.
state = run("""
  const m = document.querySelector('[data-menu="model"]');
  const groups = [...m.querySelectorAll('[data-column="groups"] button')].map((b) => ({ name: b.textContent.trim(), chosen: b.getAttribute('aria-selected') === 'true' }));
  const models = [...m.querySelectorAll('[data-column="models"] .rows button')].map((b) => ({ name: b.querySelector('.name')?.textContent.trim(), current: b.getAttribute('aria-selected') === 'true' }));
  const levels = [...m.querySelectorAll('[data-column="effort"] button')].map((b) => ({ name: b.textContent.trim(), on: b.getAttribute('aria-checked') === 'true' }));
  return { groups, models, levels };
""")
names = [g["name"] for g in state["groups"]]
check("Claude Code" in names and "Codex" in names, f"the agents column lists the providers ({names})")
check([g["name"] for g in state["groups"] if g["chosen"]] == ["Claude Code"], "the current model's provider is chosen")
check([m["name"] for m in state["models"] if m["current"]] == ["Opus 5.5"], f"the current model is ticked ({state['models']})")
check([l["name"] for l in state["levels"] if l["on"]] == ["Max"], f"the effort in force is on ({state['levels']})")

# 3. Another provider shows its models.
click_at(button_by_text("groups", "Codex"))
time.sleep(0.3)
shown = run("return [...document.querySelectorAll('[data-menu=\"model\"] .rows button .name')].map((b) => b.textContent.trim());")
check(shown == ["GPT-6.1 Sol", "GPT-6 Astra"], f"a click on Codex shows its models ({shown})")
click_at(button_by_text("groups", "Claude Code"))
time.sleep(0.3)

# 4. A model click saves it and keeps the menu open, so its effort can be
# picked next (owner, ModelMenu.svelte).
since = api_count()
click_at(button_by_text("models", "Sonnet 5.5"))
time.sleep(1.0)
got = saves(since)
model = (got[0].get("body") or {}).get("model", {}) if got else {}
check(model.get("model") == "sonnet" and model.get("provider") == "claude-code", f"a click on Sonnet 5.5 saves it ({got})")
check(menu_open(), "choosing a model keeps the menu open for its effort")

# 5. An effort level click saves the level.
if not menu_open():
    open_menu()
since = api_count()
click_at(button_by_text("effort", "Medium"))
time.sleep(1.0)
got = saves(since)
check(any((c.get("body") or {}).get("model", {}).get("reasoning_effort") == "medium" for c in got), f"a click on Medium saves the effort ({got})")

# 6. Esc closes; a click outside closes.
xdo("key", "Escape")
time.sleep(0.4)
check(not menu_open(), "Esc closes the menu")
open_menu()
click_at({"x": 800, "y": 300})
time.sleep(0.4)
check(not menu_open(), "a click outside closes the menu")
finish()
