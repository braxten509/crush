"""No flashes (owner, 2026-10-19: "a lot of places flash ... all over the
app"). In the real window on the hidden screen, with real mouse moves and
clicks, the page records every running animation on every frame while the
owner's paths run: opening the Themes sheet, switching Settings sections, back
to the chats, opening a tool group, hovering the chat rows, opening the model
menu. It fails when an animation fades a piece in from see-through (an opacity
under 1 in its keyframes) anywhere but the scrim layer behind sheets
(.veil::before), or when the settings page shows a frame with no rows during
a section switch. The veil itself once faded, and every sheet inside it faded
with it (FLASHING.md, attempt 10).

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json no_flash.py
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
      const r = el.getBoundingClientRect();
      return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};
    """)


def move(point):
    xdo("mousemove", geo_x + round(point["x"]), geo_y + round(point["y"]))


def click(selector, text=None, wait=0.5):
    point = center(selector, text)
    if not point or "x" not in point:
        seen = run("return { timeline: document.querySelectorAll('.timeline').length, tools: document.querySelectorAll('.tool-row').length, expandable: document.querySelectorAll('.tool-row[aria-expanded]').length, settings: !!document.querySelector('[data-probe=\"settings-page\"]'), items: document.querySelectorAll('[role=article]').length };")
        check(False, f"found {selector} {text or ''}: {point}; page has {seen}")
        return False
    move(point)
    xdo("click", 1)
    time.sleep(wait)
    return True


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

# Every frame: animations that fade something in, and settings frames with no rows.
started = run("""
  window.__fades = [];
  window.__blank = 0;
  const name = (el) => !el ? '?' : (el.tagName.toLowerCase() + (el.className && typeof el.className === 'string' ? '.' + el.className.trim().split(/\\s+/).join('.') : ''));
  const seen = new Set();
  const frame = () => {
    for (const a of document.getAnimations()) {
      const target = a.effect?.target;
      if (!target) continue;
      // The scrim alone may fade: its own layer, under the sheets.
      if (target.classList?.contains('veil') && a.effect.pseudoElement === '::before') continue;
      const fades = (a.effect.getKeyframes?.() ?? []).some((k) => k.opacity !== undefined && k.opacity !== '' && Number(k.opacity) < 0.99);
      const key = name(target) + (a.effect.pseudoElement || '') + '|' + (a.animationName || a.transitionProperty || 'svelte');
      if (fades && !seen.has(key)) { seen.add(key); window.__fades.push(key); }
    }
    const page = document.querySelector('[data-probe="settings-page"]');
    if (page && page.querySelectorAll('.row').length === 0) window.__blank += 1;
    requestAnimationFrame(frame);
  };
  requestAnimationFrame(frame);
  return true;
""")
check(started is True, "the frame recorder runs in the page")

# The owner's paths.
for _ in range(2):
    run("window.__probe.openDialog('theme'); return true;")
    time.sleep(0.6)
    run("window.__probe.closeDialog('theme'); return true;")
    time.sleep(0.4)
click("nav.chats button.settings")
for section in ["Typing", "Work", "Appearance", "Typing"]:
    click(".settings-nav .row", section, wait=0.35)
click(".settings-nav button.back", wait=0.6)
click(".timeline .tool-row[aria-expanded]", wait=0.5)
click(".timeline .tool-row[aria-expanded]", wait=0.5)
rows = run("return [...document.querySelectorAll('nav.chats li, nav.chats [role=\"option\"], nav.chats .row')].slice(0, 6).map((e) => { const r = e.getBoundingClientRect(); return { x: r.x + r.width / 2, y: r.y + r.height / 2 }; });") or []
for point in rows * 2:
    move(point)
    time.sleep(0.06)
click(".composer button.model", wait=0.6)
xdo("key", "Escape")
time.sleep(0.5)

got = run("return { fades: window.__fades, blank: window.__blank };")
check(got is not None and got.get("fades") == [], f"no animation fades a piece in from see-through ({got and got.get('fades')})")
check(got is not None and got.get("blank") == 0, f"the settings page never shows a frame without its rows ({got and got.get('blank')} frames)")
finish()
