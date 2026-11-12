"""Refills and the fallback order (owner, 2026-10-21). In the real window on
the hidden screen, with real mouse clicks, on a chat running Codex with two
free refills: the right panel shows the refills with "Use one", and a click
asks the engine for one; Settings > Fallback order shows the fixture's three
blocks in order; the switch saves the whole order (PUT fallback/order in the
fixture's api.jsonl); dragging block 1 below block 2 with the mouse saves the
two swapped; + Add block > Wait for a refill saves a fourth block, and its
time word's menu > "at most" saves an hour's limit; per-model lists are added
and removed; with the right panel hidden the refills still offer "Use one".
Pictures go to tests/out/fallback_editor/.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/fallback-editor.json fallback_editor.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


def app_window(timeout=20):
    """The largest visible window of the app, found by its class."""
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


def picture(name):
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, name)], check=True)


def calls(path):
    """The fixture's recorded writes to path, oldest first."""
    try:
        with open(probe_path + ".api.jsonl") as f:
            found = []
            for line in f:
                try:
                    call = json.loads(line)
                except ValueError:
                    continue
                if path in json.dumps(call):
                    found.append(call)
            return found
    except FileNotFoundError:
        return []


def body(call):
    """The JSON body of a recorded call, wherever the recorder put it."""
    for key in ("body", "data", "payload"):
        if isinstance(call.get(key), dict):
            return call[key]
    args = call.get("args")
    if isinstance(args, list):
        for a in args:
            if isinstance(a, dict) and ("blocks" in a or "kind" in a):
                return a
    return call


def blocks():
    return run("""
      return [...document.querySelectorAll('[data-probe="fallback-order"] [data-block]')].map((e) => ({
        text: e.querySelector('.summary')?.textContent.replace(/\\s+/g, ' ').trim() ?? '',
      }));
    """)


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 30)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])

# 1. The right panel shows the Codex account's refills with "Use one".
panel = run("""
  const p = document.querySelector('[data-probe="panel"]');
  return { use: Boolean(p?.querySelector('[data-refill-use]')) };
""")
check(panel["use"], f"the right panel shows the free refills with Use one ({panel})")
picture("panel.png")
click('[data-probe="panel"] [data-refill-use]')
refill = calls("limits/refill")
check(len(refill) == 1 and body(refill[0]).get("kind") == "codex-cli", f"Use one asks the engine for one Codex refill ({refill})")

# 2. Settings opens.
click("nav.chats button.settings")

# 3. Settings > Fallback order shows the three blocks in order.
click(".settings-nav .row", "Fallback order")
shown = blocks()
texts = [b["text"] for b in shown]
check(len(texts) == 3 and "Claude 1" in texts[0] and "Claude 2" in texts[1] and "GPT-6.1 Sol" in texts[2], f"the three blocks show in order ({texts})")
picture("order.png")

# 4. The switch saves the order turned off, then on again.
click('[data-probe="fallback-order"] [role="switch"]')
puts = calls("fallback/order")
check(len(puts) == 1 and body(puts[-1]).get("on") is False and len(body(puts[-1]).get("blocks", [])) == 3, f"the switch saves the order off with its blocks ({puts[-1:] })")
click('[data-probe="fallback-order"] [role="switch"]')

# 4b. Dragging block 1 by its handle below block 2 with the real mouse saves
#     the order with the two swapped.
h = center('[data-probe="fallback-order"] [data-block="0"] .handle')
gap = run("""
  const r = (i) => document.querySelector(`[data-probe="fallback-order"] [data-block="${i}"]`).getBoundingClientRect();
  return r(1).top + r(1).height / 2 - (r(0).top + r(0).height / 2);
""")
saved = len(calls("fallback/order"))
x, y = geo_x + round(h["x"]), geo_y + round(h["y"])
xdo("mousemove", x, y)
xdo("mousedown", 1)
for k in range(1, 13):
    xdo("mousemove", x, y + round(k * (gap + 10) / 12))
    time.sleep(0.04)
time.sleep(0.4)
xdo("mouseup", 1)
end = time.time() + 5
while time.time() < end and len(calls("fallback/order")) <= saved:
    time.sleep(0.05)
puts = calls("fallback/order")
order = body(puts[-1]).get("blocks", []) if len(puts) > saved else []
check(len(order) == 3 and order[0].get("account") == "claude-2" and not order[1].get("account"), f"the order is saved with the two swapped ({order})")
picture("dragged.png")

# 5. + Add block > Wait for a refill adds a fourth block and saves it.
click('[data-probe="fallback-order"] button.add')
click('[data-probe="fallback-order"] .choice', "Wait for a refill")
puts = calls("fallback/order")
last = body(puts[-1]) if puts else {}
check(len(last.get("blocks", [])) == 4 and last["blocks"][3].get("type") == "wait", f"adding a wait block saves four blocks ({last})")

# 6. Its time word opens a menu, where "at most" saves an hour's limit.
click('[data-probe="fallback-order"] [data-block="3"] .word', "with no time limit")
check(run("return Boolean(document.querySelector('[data-probe=\"word-menu\"]'));"), "the word opens its menu")
picture("menu.png")
click('[data-probe="word-menu"] .pick', "at most")
puts = calls("fallback/order")
last = body(puts[-1]) if puts else {}
check(last.get("blocks", [{}] * 4)[3].get("max_wait_minutes") == 60, f"at most saves a 60 minute limit ({last.get('blocks', [])[3:]})")
picture("added.png")

# 8. Per-model lists (layout C, owner 2026-10-21): + Add a model makes the
# picked model's own list, shown at once with one block on that model; every
# other model's list keeps its four; x removes the list.
click('[data-probe="fallback-lists"] .start-add .word', "+ Add a model")
first = run("return document.querySelector('[data-probe=\"word-menu\"] .pick')?.textContent.trim() ?? null;")
check(bool(first), f"+ Add a model lists the models ({first})")
picture("add-list-menu.png")
click('[data-probe="word-menu"] .pick', first)
puts = calls("fallback/order")
last = body(puts[-1]) if puts else {}
lists = last.get("lists") or []
check(len(lists) == 1 and len(lists[0].get("blocks", [])) == 1 and lists[0]["blocks"][0].get("model") == lists[0].get("model") and len(last.get("blocks", [])) == 4,
      f"the new list is saved with one block on its model, beside the four others ({lists})")
side = run("""return [...document.querySelectorAll('[data-probe="fallback-lists"] .start-pick')].map((e) => ({ text: e.textContent.trim(), on: e.getAttribute('aria-current') === 'true' }));""") or []
texts = [b["text"] for b in blocks()]
check(len(side) == 2 and side[0]["text"].startswith(first) and side[0]["on"] and side[1]["text"] == "Every other model" and len(texts) == 1,
      f"the left column shows the model's list, picked, with its one block on the right ({side}, {texts})")
picture("lists.png")
click('[data-probe="fallback-lists"] .start-pick', "Every other model")
check(len(blocks()) == 4, f"Every other model still shows its four blocks ({len(blocks())})")
click('[data-probe="fallback-lists"] .start-remove')
puts = calls("fallback/order")
last = body(puts[-1]) if puts else {}
check(not last.get("lists") and len(last.get("blocks", [])) == 4, f"x removes the list, the others stay ({last.get('lists')})")

# 7. With the right panel hidden the refills show under the composer.
click(".settings-nav .row", "Appearance")
click('[data-probe="settings-page"] [role="switch"][aria-label="Show the right panel"]')
click(".settings-nav button.back")
time.sleep(0.5)
strip = run("""
  const p = document.querySelector('[data-probe="panel"]');
  const b = document.querySelector('[data-refill-use]');
  return { panel: Boolean(p), use: Boolean(b) };
""")
check(not strip["panel"] and strip["use"], f"with the panel hidden, Use one shows under the composer ({strip})")
picture("strip.png")
finish()
