"""Accounts and the fallback order (owner, 2026-10-21). In the real window on
the hidden screen, with real mouse clicks, on a chat running Codex with two
free refills: the right panel shows REFILLS 2 with "Use one", and a click asks
the engine for one; Settings > Accounts lists Claude 1, Claude 2 and Codex 1,
each name with a space on both sides of its dot and Rename last on every row,
in one place (owner, 2026-10-21);
Settings > Fallback order shows the fixture's three blocks in order, each a
sentence saying how the chat leaves it (look C, owner 2026-10-21); the switch
saves the whole order (PUT fallback/order in the fixture's api.jsonl);
dragging block 1 below block 2 with the mouse draws it under the pointer while
block 2 slides up (sampled every frame), settles it in place and saves the
two swapped; + Add block > Wait for a refill saves a fourth block, and its
time word's menu > "at most" saves an hour's limit; with the right panel
hidden the refills show under the composer with their own "Use one".
Pictures go to tests/out/fallback_editor/.

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
check(all(" · " in n for n in names), f"each name has a space on both sides of its dot ({names})")
renames = run("return [...document.querySelectorAll('[data-account]')].map((row) => { const acts = [...row.querySelectorAll('button.act')]; return { last: acts.at(-1)?.textContent.trim() ?? '', right: Math.round((acts.at(-1)?.getBoundingClientRect().right ?? 0) * 10) / 10 }; });") or []
check(len(renames) == 3 and all(r["last"] == "Rename" for r in renames) and len({r["right"] for r in renames}) == 1, f"every account, the own logins too, has Rename last, in one place ({renames})")
picture("accounts.png")

# 3. Settings > Fallback order shows the three blocks in order.
click(".settings-nav .row", "Fallback order")
shown = blocks()
texts = [b["text"] for b in shown]
check(len(texts) == 3 and "Claude 1" in texts[0] and "Claude 2" in texts[1] and "GPT-6.1 Sol" in texts[2], f"the three blocks show in order ({texts})")
leave = run("return [...document.querySelectorAll('[data-probe=\"fallback-order\"]')].map((e) => e.textContent.replace(/\\s+/g, ' '))[0];") or ""
check("and come back when it refills" in leave and "after a free refill" in leave, "each block says how the chat leaves it")
picture("order.png")

# 4. The switch saves the order turned off, then on again.
click('[data-probe="fallback-order"] [role="switch"]')
puts = calls("fallback/order")
check(len(puts) == 1 and body(puts[-1]).get("on") is False and len(body(puts[-1]).get("blocks", [])) == 3, f"the switch saves the order off with its blocks ({puts[-1:] })")
click('[data-probe="fallback-order"] [role="switch"]')

# 4b. Dragging block 1 by its handle below block 2 with the real mouse: it
#     follows the pointer, block 2 slides up out of its way (drawn part-way on
#     some frames, not jumping), block 1 settles into its new place when let
#     go, and the order is saved with the two swapped.
h = center('[data-probe="fallback-order"] [data-block="0"] .handle')
gap = run("""
  const r = (i) => document.querySelector(`[data-probe="fallback-order"] [data-block="${i}"]`).getBoundingClientRect();
  return r(1).top + r(1).height / 2 - (r(0).top + r(0).height / 2);
""")
run("""
  const ol = document.querySelector('[data-probe="fallback-order"] ol');
  const [a, b] = [...ol.children];
  const log = (window.__slideLog = { a: a.dataset.key, b: b.dataset.key, frames: [] });
  const top = (key) => ol.querySelector(`[data-key="${key}"] .block`)?.getBoundingClientRect().top ?? null;
  const t0 = performance.now();
  (function sample() {
    log.frames.push({ t: performance.now() - t0, a: top(log.a), b: top(log.b) });
    if (performance.now() - t0 < 5000) requestAnimationFrame(sample);
  })();
  return true;
""")
x, y = geo_x + round(h["x"]), geo_y + round(h["y"])
xdo("mousemove", x, y)
xdo("mousedown", 1)
for k in range(1, 13):
    xdo("mousemove", x, y + round(k * (gap + 10) / 12))
    time.sleep(0.04)
time.sleep(0.4)
xdo("mouseup", 1)
time.sleep(0.8)
log = run("return window.__slideLog;") or {}
frames = [f for f in log.get("frames", []) if f["a"] is not None and f["b"] is not None]
a0, b0 = (frames[0]["a"], frames[0]["b"]) if frames else (0, 0)
a_end, b_end = (frames[-1]["a"], frames[-1]["b"]) if frames else (0, 0)
between = sorted({round(f["b"]) for f in frames if a0 + 2 < f["b"] < b0 - 2})
check(frames and abs(b_end - a0) < 2 and len(between) >= 3, f"block 2 slides up out of the way ({b0:.0f} to {b_end:.0f}, part-way on {len(between)} frames)")
followed = sorted({round(f["a"]) for f in frames})
rest = run(f"""
  const li = document.querySelector('[data-probe="fallback-order"] [data-key="{log.get('a')}"]');
  return {{ transform: li?.style.transform ?? null, moving: li ? li.getAnimations().length : null }};
""") or {}
check(len(followed) >= 6 and a_end > b_end and rest.get("transform") == "" and rest.get("moving") == 0, f"block 1 follows the pointer and settles in its new place ({len(followed)} places drawn, {rest})")
puts = calls("fallback/order")
order = body(puts[-1]).get("blocks", []) if puts else []
check(len(order) == 3 and order[0].get("account") == "claude-2" and not order[1].get("account"), f"the order is saved with the two swapped ({order})")
picture("dragged.png")

# 5. + Add block > Wait for a refill adds a fourth block, a sentence, and saves it.
click('[data-probe="fallback-order"] button.add')
click('[data-probe="fallback-order"] .choice', "Wait for a refill")
puts = calls("fallback/order")
last = body(puts[-1]) if puts else {}
check(len(last.get("blocks", [])) == 4 and last["blocks"][3].get("type") == "wait", f"adding a wait block saves four blocks ({last})")
texts = [b["text"] for b in blocks()]
check(len(texts) == 4 and "Wait for Claude 1 to refill, with no time limit, just waiting." in texts[3], f"the new block reads as a sentence ({texts[3:]})")

# 6. Its time word opens a menu, where "at most" saves an hour's limit.
click('[data-probe="fallback-order"] [data-block="3"] .word', "with no time limit")
check(run("return Boolean(document.querySelector('[data-probe=\"word-menu\"]'));"), "the word opens its menu")
picture("menu.png")
click('[data-probe="word-menu"] .pick', "at most")
puts = calls("fallback/order")
last = body(puts[-1]) if puts else {}
check(last.get("blocks", [{}] * 4)[3].get("max_wait_minutes") == 60, f"at most saves a 60 minute limit ({last.get('blocks', [])[3:]})")
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
