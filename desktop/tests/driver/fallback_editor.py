"""Accounts and the fallback order (owner, 2026-10-21). In the real window on
the hidden screen, with real mouse clicks, on a chat running Codex with two
free refills: the right panel shows REFILLS 2 with "Use one", and a click asks
the engine for one; Settings > Accounts lists Claude 1, Claude 2 and Codex 1;
Settings > Fallback order shows the fixture's three blocks in order with how
the chat leaves each; the switch, + Add block > Wait for a refill and the new
block's "Up to" each save the whole order (PUT fallback/order in the
fixture's api.jsonl); with the right panel hidden the refills show under the
composer with their own "Use one". Pictures go to tests/out/fallback_editor/.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/fallback-editor.json fallback_editor.py
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
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(1.5)
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])

# 1. The right panel shows the Codex account's refills with "Use one".
panel = run("""
  const p = document.querySelector('[data-probe="panel"]');
  return { text: p?.textContent.replace(/\\s+/g, ' ') ?? '', use: Boolean(p?.querySelector('[data-refill-use]')) };
""")
check("REFILLS" in panel["text"] and panel["use"], f"the right panel shows the free refills with Use one ({panel['text'][:200]})")
picture("panel.png")
click('[data-probe="panel"] [data-refill-use]')
refill = calls("limits/refill")
check(len(refill) == 1 and body(refill[0]).get("kind") == "codex-cli", f"Use one asks the engine for one Codex refill ({refill})")

# 2. Settings > Accounts lists every account.
click("nav.chats button.settings")
click(".settings-nav .row", "Accounts")
names = run("return [...document.querySelectorAll('[data-account]')].map((e) => e.getAttribute('data-account') + ' ' + e.querySelector('.name').textContent.trim());")
check(len(names) == 3 and any("Claude 2" in n for n in names) and any("Codex 1" in n for n in names), f"Accounts lists Claude 1, Claude 2 and Codex 1 ({names})")
picture("accounts.png")

# 3. Settings > Fallback order shows the three blocks in order.
click(".settings-nav .row", "Fallback order")
shown = blocks()
texts = [b["text"] for b in shown]
check(len(texts) == 3 and "Claude 1" in texts[0] and "Claude 2" in texts[1] and "GPT-6.1 Sol" in texts[2], f"the three blocks show in order ({texts})")
leave = run("return [...document.querySelectorAll('[data-probe=\"fallback-order\"]')].map((e) => e.textContent.replace(/\\s+/g, ' '))[0];") or ""
check("comes back when it refills" in leave and "after a free refill" in leave, "each block says how the chat leaves it")
picture("order.png")

# 4. The switch saves the order turned off, then on again.
click('[data-probe="fallback-order"] [role="switch"]')
puts = calls("fallback/order")
check(len(puts) == 1 and body(puts[-1]).get("on") is False and len(body(puts[-1]).get("blocks", [])) == 3, f"the switch saves the order off with its blocks ({puts[-1:] })")
click('[data-probe="fallback-order"] [role="switch"]')

# 5. + Add block > Wait for a refill adds a fourth block, open, and saves it.
click('[data-probe="fallback-order"] button.add')
click('[data-probe="fallback-order"] .choice', "Wait for a refill")
puts = calls("fallback/order")
last = body(puts[-1]) if puts else {}
check(len(last.get("blocks", [])) == 4 and last["blocks"][3].get("type") == "wait", f"adding a wait block saves four blocks ({last})")
check(run("return Boolean(document.querySelector('[data-probe=\"block-options\"]'));"), "the new block opens to its options")

# 6. Its "Up to" saves an hour's limit.
click('[data-probe="block-options"] .choice', "Up to")
puts = calls("fallback/order")
last = body(puts[-1]) if puts else {}
check(last.get("blocks", [{}] * 4)[3].get("max_wait_minutes") == 60, f"Up to saves a 60 minute limit ({last.get('blocks', [])[3:]})")
texts = [b["text"] for b in blocks()]
check(len(texts) == 4 and "at most 1 h" in texts[3], f"the block's line says so ({texts[3:]})")
picture("added.png")

# 7. With the right panel hidden the refills show under the composer.
click(".settings-nav .row", "Appearance")
click('[data-probe="settings-page"] [role="switch"][aria-label="Show the right panel"]')
click(".settings-nav button.back")
time.sleep(0.5)
strip = run("""
  const p = document.querySelector('[data-probe="panel"]');
  const b = document.querySelector('[data-refill-use]');
  const c = document.querySelector('[data-probe="composer"]') ?? document.querySelector('.composer');
  return { panel: Boolean(p), use: Boolean(b), below: b && c ? b.getBoundingClientRect().top >= c.getBoundingClientRect().top : null };
""")
check(not strip["panel"] and strip["use"], f"with the panel hidden, Use one shows under the composer ({strip})")
picture("strip.png")
finish()
