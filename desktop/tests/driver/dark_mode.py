"""Dark mode and the chat's scroll bar (owner, 2026-10-20: "add a dark mode
that switches the cream and black colors"; "this should be hidden when I'm
not scrolling and currently it's not clickable since the resize for the
vertical bar is overtaking the hitbox"). In the real window on the hidden
screen, with real mouse moves, wheel turns and clicks:

- the chat's scroll bar is not painted before any scroll, shows while the
  wheel turns and paints away again 2 s after scrolling stops (owner,
  2026-10-26: "hide after a few seconds ... not showing until it was used",
  app-wide), and the point on the bar belongs to the chat, not to the right
  column's grip beside it;
- Settings > Appearance > Mode > Dark lays soft linen ink on graphite paper
  (owner, 2026-10-20: "it loses the soft vibe", then the Graphite pick):
  the paper is #232321, the ink reads at 10 to 13:1 (not 18:1), every window
  color still reads (ink on paper, band and controls), quiet text stays
  quieter than the ink, the frame rules stay faint, and Light brings the
  cream paper back;
- on the themed chat, a diagram's box fills with the graphite paper, not
  black (part "diagram").

Pictures of the dark screens go to tests/out/dark_mode/.

The recorded engine has no themes, so each part runs on its own leg:

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json dark_mode.py scroll
       tests/check.sh dark_mode.py dark        (the real engine; no CHECK_ARGS)
       CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu-themed.json dark_mode.py dark diagram
                                               (a full chat in dark; the recording plus
                                               the engine's theme, from capture_theme.py)
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
parts = sys.argv[3:] or ["scroll", "dark"]
BAR_SHOW_MS = 2000  # VirtualList's BAR_SHOW_MS


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
    path = os.path.join(out_dir, name + ".png")
    subprocess.run(["import", "-window", wid, path])
    return path


def column_mean(path, x, y, w, h):
    """The mean brightness (0..1) of a strip of a picture."""
    r = subprocess.run(["magick", path, "-crop", f"{w}x{h}+{x}+{y}", "+repage", "-colorspace", "Gray", "-format", "%[fx:mean]", "info:"],
                       capture_output=True, text=True)
    try:
        return float(r.stdout.strip())
    except ValueError:
        return None


def click(selector, text=None, wait=0.6):
    find = f"[...document.querySelectorAll({selector!r})]" + (f".find((e) => e.textContent.trim() === {text!r})" if text else "[0]")
    point = run(f"const el = {find}; if (!el) return null; const r = el.getBoundingClientRect(); return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};")
    if not point or "x" not in point:
        check(False, f"found {selector} {text or ''}")
        return False
    xdo("mousemove", geo_x + round(point["x"]), geo_y + round(point["y"]))
    xdo("click", 1)
    time.sleep(wait)
    return True


COLORS = """
  const css = getComputedStyle(document.documentElement);
  const get = (n) => css.getPropertyValue(n).trim().toLowerCase();
  const lum = (hex) => {
    const m = /^#([0-9a-f]{6})$/.exec(hex); if (!m) return null;
    const n = parseInt(m[1], 16);
    const c = [(n >> 16) & 255, (n >> 8) & 255, n & 255].map((v) => { v /= 255; return v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4; });
    return 0.2126 * c[0] + 0.7152 * c[1] + 0.0722 * c[2];
  };
  const ratio = (a, b) => { const x = lum(a), y = lum(b); return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05); };
  const names = ['--paper', '--ink', '--band', '--slate', '--control-rest', '--control-hover', '--paper-raised', '--rule-frame'];
  const c = Object.fromEntries(names.map((n) => [n, get(n)]));
  return {
    mode: document.documentElement.dataset.mode,
    paper: c['--paper'], ink: c['--ink'], paperLum: lum(c['--paper']),
    worst: Math.min(...['--paper', '--band', '--control-rest', '--control-hover', '--paper-raised'].map((b) => ratio(c['--ink'], c[b]))),
    slate: Math.min(ratio(c['--slate'], c['--paper']), ratio(c['--slate'], c['--band'])),
    ink: ratio(c['--ink'], c['--paper']), frame: ratio(c['--rule-frame'], c['--paper']),
  };
"""

wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(2)
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])

# -- The chat's scroll bar --
# The recorded chat fits a full window: a short window makes it scroll.
if "scroll" in parts:
    xdo("windowsize", wid, 1600, 420)
    time.sleep(1.5)
bar = None if "scroll" not in parts else run("""
  const s = document.querySelector('.slot:not(.parked) .scroller') || document.querySelector('.scroller');
  if (!s) return null;
  const r = s.getBoundingClientRect();
  const x = r.right - 3, y = r.top + r.height / 2;
  const hit = document.elementFromPoint(x, y);
  return { x, y, left: r.left, top: r.top, height: r.height, width: r.width, client: s.clientWidth,
           tall: s.scrollHeight > s.clientHeight + 10, inside: !!hit && s.contains(hit),
           hit: hit ? hit.tagName.toLowerCase() + '.' + String(hit.className) : null };
""")
if "scroll" in parts and check(bar is not None and "x" in bar, f"the chat scroller is on the page ({bar})"):
    check(bar["tall"], "the chat is long enough to scroll")
    check(bar["inside"], f"the point on the scroll bar belongs to the chat, not the column grip (hit {bar['hit']})")
    # Real wheel turns over the chat.
    xdo("mousemove", geo_x + round(bar["left"] + bar["width"] / 2), geo_y + round(bar["y"]))
    before = run("return document.querySelector('.slot:not(.parked) .scroller, .scroller').scrollTop;")
    strip = (round(bar["x"]) - 2, round(bar["top"] + 20), 5, round(bar["height"] - 40))
    unused = column_mean(shot("bar-unused", 0.2), *strip)
    for _ in range(4):
        xdo("click", 4)
        time.sleep(0.05)
    time.sleep(0.25)
    shown = run("return document.querySelector('.slot:not(.parked) .scroller, .scroller').classList.contains('bar-shown');")
    during = column_mean(shot("bar-scrolling", 0), *strip)
    time.sleep(BAR_SHOW_MS / 1000 + 1.5)
    hidden = run("return !document.querySelector('.slot:not(.parked) .scroller, .scroller').classList.contains('bar-shown');")
    after = column_mean(shot("bar-still", 0.2), *strip)
    moved = run("return document.querySelector('.slot:not(.parked) .scroller, .scroller').scrollTop;")
    check(before is not None and moved is not None and moved != before, f"the wheel scrolls the chat ({before} to {moved})")
    check(shown is True, "the thumb shows while the wheel turns")
    check(hidden is True, "the thumb's class goes once scrolling stops")
    check(unused is not None and after is not None and abs(after - unused) < 0.01,
          f"the bar is not painted before any scroll (strip brightness {unused} before, {after} after it hid)")
    check(during is not None and after is not None and after - during > 0.01,
          f"the thumb is painted away on screen after scrolling stops (strip brightness {during} while scrolling, {after} after)")
xdo("mousemove", geo_x + 5, geo_y + 400)
if "scroll" in parts:
    xdo("windowsize", wid, 1600, 992)
    time.sleep(1.5)

# -- Dark mode --
if "dark" not in parts:
    finish()
light = run(COLORS)
check(light and light.get("mode") == "light", f"the window starts light ({light})")
click("nav.chats button.settings")
click(".settings-nav .row", "Appearance")
mode_row = run("return !!document.querySelector('[role=radiogroup][aria-label=Mode]');")
check(mode_row is True, "Settings > Appearance has the Mode choice")
click("[role=radiogroup][aria-label=Mode] .choice", "Dark")
dark = run(COLORS)
check(dark and dark.get("mode") == "dark", f"Dark turns the window dark ({dark and dark.get('mode')})")
check(dark and dark.get("paper") == "#232321", f"the paper is soft graphite ({dark and dark.get('paper')})")
check(dark and 10 <= dark.get("ink", 0) <= 13, f"the ink is soft, not glaring ({dark and dark.get('ink')}:1 on the paper)")
check(dark and dark.get("worst", 0) >= 4.5, f"the ink reads on every dark surface (worst {dark and dark.get('worst')}:1)")
check(dark and 4.5 <= dark.get("slate", 0) < dark.get("ink", 0), f"quiet text reads and stays quieter than the ink ({dark and dark.get('slate')}:1)")
check(dark and dark.get("frame", 99) < 2.2, f"the frame rules stay faint ({dark and dark.get('frame')}:1)")
checked = run("return document.querySelector('[role=radiogroup][aria-label=Mode] [aria-checked=true]')?.textContent.trim();")
check(checked == "Dark", f"the Dark choice shows as chosen ({checked})")
shot("dark-settings-appearance")
for name in ["Typing", "Work", "Notifications"]:
    if click(".settings-nav .row", name, 0.5):
        shot(f"dark-settings-{name.lower()}", 0.3)
click(".settings-nav button.back", wait=0.8)
shot("dark-chat")
if "diagram" in parts:
    # A box of the chat's diagram, read off the screen beside its label.
    node = None
    end = time.time() + 15
    while time.time() < end and not node:
        node = run("""
          const fig = document.querySelector('.figure-view[data-kind="diagram"]');
          const box = fig && fig.querySelector('svg g.node');
          if (!box) return null;
          fig.scrollIntoView({ block: 'center' });
          const r = box.getBoundingClientRect();
          return { x: r.x + 10, y: r.y + r.height / 2, w: r.width };
        """)
        time.sleep(0.5)
    if check(node is not None, "the themed chat shows its diagram"):
        time.sleep(0.8)
        node = run("const r = document.querySelector('.figure-view[data-kind=\"diagram\"] svg g.node').getBoundingClientRect(); return { x: r.x + 10, y: r.y + r.height / 2 };")
        path = shot("dark-diagram", 0.3)
        got = subprocess.run(["magick", path, "-format", f"%[pixel:p{{{round(node['x'])},{round(node['y'])}}}]", "info:"], capture_output=True, text=True).stdout
        nums = [int(float(v)) for v in got[got.find("(") + 1:got.find(")")].split(",")[:3]] if "(" in got else []
        paper = [0x23, 0x23, 0x21]
        check(len(nums) == 3 and all(abs(a - b) <= 10 for a, b in zip(nums, paper)), f"the diagram's box fills with the graphite paper ({got.strip()} vs #232321)")
run("document.querySelector('.slot:not(.parked) .scroller, .scroller')?.scrollBy(0, -1200); return true;")
shot("dark-chat-top")
run("window.__probe.openDialog('theme'); return true;")
shot("dark-themes-sheet")
run("window.__probe.closeDialog('theme'); return true;")
time.sleep(0.4)
run("window.__probe.askYesNo({ label: 'Update', question: 'Install updates for AGY?', description: 'AGY 1.4.2 → 1.4.3.' }); return true;")
shot("dark-yes-no")
xdo("key", "Escape")
time.sleep(0.4)
if click(".composer button.model", wait=0.7):
    shot("dark-model-menu", 0.2)
    xdo("key", "Escape")
    time.sleep(0.4)
run("window.__probe.runAction('chat.new'); return true;")
shot("dark-new-chat", 1.2)

# Back to light.
click("nav.chats button.settings")
click(".settings-nav .row", "Appearance")
click("[role=radiogroup][aria-label=Mode] .choice", "Light")
back = run(COLORS)
check(back and light and back.get("paper") == light.get("paper") and back.get("mode") == "light", f"Light brings the cream paper back ({back and back.get('paper')})")
click(".settings-nav button.back", wait=0.5)
finish()
