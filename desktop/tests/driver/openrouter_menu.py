"""OpenRouter in the model menu, and each model's own effort (owner,
2026-10-22). In the real window on the hidden screen, with real clicks:
- choosing Sonnet saves it with the effort last used with it (Low, from
  the fixture's model_efforts), not its default (High);
- the OpenRouter group lists only its saved model, with "Add model" under;
- a right-click on the saved model offers Remove, which saves the list
  without it (POST config/set providers.openrouter.models);
- "Add model" opens the catalog sheet: every catalog model with its price,
  the saved one marked, a search that narrows it; a click ticks a model
  and Done saves the saved list plus it, then the menu opens again on
  OpenRouter.
Pictures go to tests/out/openrouter_menu/.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/openrouter-menu.json openrouter_menu.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, type_text, wait_for, xdo

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
    return run(f"""
      const el = [...document.querySelectorAll({json.dumps(selector)})]
        .find((b) => {json.dumps(text)} === null || b.textContent.trim().startsWith({json.dumps(text)}));
      if (!el) return null;
      const r = el.getBoundingClientRect();
      return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};
    """)


def click_at(point, button=1):
    xdo("mousemove", geo_x + round(point["x"]), geo_y + round(point["y"]))
    xdo("click", str(button))


def calls(since, path):
    try:
        with open(probe_path + ".api.jsonl") as f:
            lines = [json.loads(l) for l in f if l.strip()]
    except FileNotFoundError:
        return []
    return [c for c in lines[since:] if c.get("method") == "POST" and c.get("path") == path]


def api_count():
    try:
        with open(probe_path + ".api.jsonl") as f:
            return sum(1 for l in f if l.strip())
    except FileNotFoundError:
        return 0


def menu_open():
    return run("return Boolean(document.querySelector('[data-menu=\"model\"]'));")


def open_menu():
    click_at(center(".composer button.model"))
    end = time.time() + 3
    while time.time() < end and not menu_open():
        time.sleep(0.05)
    time.sleep(0.4)


def shown_rows():
    return run("return [...document.querySelectorAll('[data-menu=\"model\"] .rows button')].map((b) => b.textContent.trim());")


def snap(name):
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, f"{name}.png")], check=True)


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
GROUP = '[data-menu="model"] [data-column="groups"] button'
ROW = '[data-menu="model"] .rows button'

# 1. A model comes with the effort last used with it.
open_menu()
groups = run(f"return [...document.querySelectorAll({json.dumps(GROUP)})].map((b) => b.textContent.trim());")
check("OpenRouter" in (groups or []), f"the agents column lists OpenRouter ({groups})")
since = api_count()
click_at(center(ROW, "Sonnet 5.5"))
time.sleep(1.0)
got = calls(since, "config/model")
model = (got[0].get("body") or {}).get("model", {}) if got else {}
check(model.get("model") == "sonnet" and model.get("reasoning_effort") == "low", f"Sonnet is saved with its remembered effort, Low ({model})")

# 2. OpenRouter shows its saved model and "Add model" under it.
open_menu()
click_at(center(GROUP, "OpenRouter"))
time.sleep(0.3)
rows = shown_rows()
check(rows == ["Moonshot: Kimi K3", "Add model"], f"OpenRouter lists its saved model, then Add model ({rows})")
snap("saved")

# 3. Right-click, Remove.
click_at(center(ROW, "Moonshot: Kimi K3"), 3)
time.sleep(0.3)
remove = center('[data-menu="model"] .remove-menu button', "Remove")
check(remove is not None, "a right-click on a saved model offers Remove")
snap("remove")
if remove:
    since = api_count()
    click_at(remove)
    time.sleep(1.0)
    got = calls(since, "config/set")
    body = got[0].get("body") if got else {}
    check(body.get("key") == "providers.openrouter.models" and body.get("value") == [], f"Remove saves the list without it ({body})")

# 4. Add model opens the catalog sheet.
if not menu_open():
    open_menu()
    click_at(center(GROUP, "OpenRouter"))
    time.sleep(0.3)
click_at(center(ROW, "Add model"))
time.sleep(1.0)
SHEET_ROWS = "[role=dialog] [role=checkbox]"
sheet = run(f"""
  const boxes = [...document.querySelectorAll({json.dumps(SHEET_ROWS)})];
  return {{ menu: Boolean(document.querySelector('[data-menu="model"]')),
           title: [...document.querySelectorAll('h1, h2, [role=dialog] *')].map((e) => e.textContent.trim()).find((t) => t.startsWith('Add OpenRouter models')) ?? null,
           rows: boxes.map((b) => ({{ name: b.getAttribute('aria-label'), on: b.getAttribute('aria-checked') === 'true',
                                      info: b.closest('.line')?.querySelector('.info')?.textContent.trim() }})) }};
""")
check(sheet and sheet["title"] and not sheet["menu"], f"Add model closes the menu and opens the catalog sheet ({sheet and sheet['title']})")
rows = (sheet or {}).get("rows") or []
check([r["name"] for r in rows] == ["Moonshot: Kimi K3", "Z.ai: GLM 5.3", "Free Coder"], f"the sheet lists the catalog ({rows})")
check(rows and rows[0]["on"] and rows[0]["info"].startswith("saved · $0.6 / $2.5"), f"the saved model shows ticked and saved, with its price ({rows[:1]})")
check(len(rows) > 2 and rows[2]["info"] == "free · 1M", f"a free model says free with its context ({rows[2:3]})")
snap("sheet")
type_text("glm")
time.sleep(0.4)
narrowed = run(f"return [...document.querySelectorAll({json.dumps(SHEET_ROWS)})].map((b) => b.getAttribute('aria-label'));")
check(narrowed == ["Z.ai: GLM 5.3"], f"the search narrows the list ({narrowed})")
click_at(center("[role=dialog] .line", "Z.ai: GLM 5.3"))
time.sleep(0.3)
ticked = run(f"return [...document.querySelectorAll({json.dumps(SHEET_ROWS)})].map((b) => b.getAttribute('aria-checked'));")
check(ticked == ["true"], f"a click ticks it ({ticked})")
since = api_count()
click_at(center("[role=dialog] button", "Done"))
time.sleep(1.5)
got = calls(since, "config/set")
body = got[0].get("body") if got else {}
ids = [m.get("id") for m in (body.get("value") or [])]
check(body.get("key") == "providers.openrouter.models" and ids == ["moonshot/kimi-k3", "z-ai/glm-5.3"], f"Done saves the saved model plus the ticked one ({ids})")
chosen = run(f"return [...document.querySelectorAll({json.dumps(GROUP)})].filter((b) => b.getAttribute('aria-selected') === 'true').map((b) => b.textContent.trim());")
check(menu_open() and chosen == ["OpenRouter"], f"the menu opens again on OpenRouter ({chosen})")
snap("after")
finish()
