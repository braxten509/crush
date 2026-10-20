"""The window's own title bar (owner 2026-10-20, design D;
components/system/TitleBar.svelte, electron/main.cjs), on a real KWin: the
private hidden one of tests/kwin/screen.sh, with a mouse KWin itself sees.

- The system bar is gone and the page's bar shows the chat and the folder,
  with the page right under it.
- Dragging the bar moves the window; double-clicking it maximizes and
  restores; dragging the window's right edge resizes it.
- Right-clicking the bar opens KWin's window menu.
- Maximize/Restore, Minimize, the pin (KWin's keep-above, the vermilion rule)
  and Close work; a pinned window opens pinned again.

Usage: tests/check.sh --no-app --kwin --size 1900x1200 --fixture tests/fixtures/model-menu.json title_bar.py
(the screen has room around the 1586x992 window to move and widen it)
"""
import json, os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from apps import App
from harness import check, finish, probe_eval
from kwin import Mouse, app_window, kwin_eval

probe_path, out_dir = sys.argv[1], sys.argv[2]
PINS = "/tmp/home/.local/share/crush-desktop/electron/window-pins.json"


def page(script):
    return probe_eval(probe_path, "window.__probeEval = (() => {" + script + "})();")


BAR = """
  const q = (s) => document.querySelector(s);
  const box = (s) => { const r = q(s)?.getBoundingClientRect(); return r ? { x: r.x, y: r.y, w: r.width, h: r.height } : null; };
  const bar = q('[data-probe="title-bar"]');
  return bar && {
    bar: box('[data-probe="title-bar"]'), shell: box('[data-probe="shell"]'),
    chat: q('[data-probe="title-chat"]')?.textContent, project: q('[data-probe="title-project"]')?.textContent,
    pin: box('[data-probe="title-pin"]'), minimize: box('[data-probe="title-minimize"]'),
    maximize: box('[data-probe="title-maximize"]'), close: box('[data-probe="title-close"]'),
    maximizeLabel: q('[data-probe="title-maximize"]').getAttribute('aria-label'),
    pinned: bar.classList.contains('pinned'), rule: getComputedStyle(bar, '::after').backgroundColor,
    vermilion: getComputedStyle(document.documentElement).getPropertyValue('--vermilion').trim(),
  };
"""


def bar():
    return page(BAR)


def wait(cond, timeout=6):
    end = time.time() + timeout
    got = None
    while time.time() < end:
        got = cond()
        if got:
            return got
        time.sleep(0.15)
    return got


def at(win, box, dx=None):
    """Screen point of a page box's middle (or dx in from its left)."""
    x = box["x"] + (dx if dx is not None else box["w"] / 2)
    return win["x"] + x, win["y"] + box["y"] + box["h"] / 2


app = App()
win = app_window(30)
check(win is not None, f"KWin shows the app's window ({win})")
if win is None:
    finish()
b = wait(lambda: (lambda got: got if got and got.get("project") else None)(bar()), 20)
check(bool(b), f"the page draws its own title bar ({b})")
if not b:
    finish()
inner = page("return [innerWidth, innerHeight];")
check(inner == [win["w"], win["h"]], f"KWin draws no title bar of its own: its window is the page ({win['w']}x{win['h']}, page {inner})")
check(b["chat"] == "New session" and b["project"] == "proj", f"the bar names the chat and the folder ({b['chat']!r}, {b['project']!r})")
check(abs(b["bar"]["y"]) < 0.5 and abs(b["shell"]["y"] - (b["bar"]["y"] + b["bar"]["h"])) < 0.5,
      f"the bar is at the top and the page starts right under it (bar {b['bar']}, page {b['shell']})")

mouse = Mouse()
# An empty stretch of the bar: between the folder's name and the pin.
empty = lambda w, b: (w["x"] + (b["pin"]["x"] - 120), w["y"] + b["bar"]["h"] / 2)

# Move the window off the corner first so it can be dragged in any direction.
before = app_window()
x0, y0 = empty(before, b)
mouse.drag(x0, y0, x0 + 140, y0 + 90)
moved = wait(lambda: (lambda w: w if (w["x"], w["y"]) != (before["x"], before["y"]) else None)(app_window()))
# The move starts once the pointer has gone a few pixels (the app's drag
# threshold), so the window trails the pointer by those few pixels.
check(bool(moved) and 120 <= moved["x"] - before["x"] <= 142 and 75 <= moved["y"] - before["y"] <= 92,
      f"dragging the bar moves the window ({before['x']},{before['y']} -> {moved and (moved['x'], moved['y'])})")
win = moved or before

# Double-click: maximize, then restore.
x0, y0 = empty(win, b)
mouse.double_click(x0, y0)
big = wait(lambda: (lambda w: w if w["w"] == w["area"]["w"] and w["h"] == w["area"]["h"] else None)(app_window()))
check(bool(big), f"double-clicking the bar maximizes the window ({app_window()})")
b2 = wait(lambda: (lambda got: got if got and got["maximizeLabel"] == "Restore" else None)(bar()))
check(bool(b2), "maximized, the button reads Restore")
if big:
    x0, y0 = empty(big, b2 or b)
    mouse.double_click(x0, y0)
    back = wait(lambda: (lambda w: w if w["w"] == win["w"] and w["h"] == win["h"] else None)(app_window()))
    check(bool(back), f"double-clicking again restores it ({app_window()})")
    win = back or app_window()

# The window's right edge resizes it: the edge's grip lies just outside the
# window, in its shadow. Inward, so the pointer stays on the screen.
win = app_window()
ex, ey = win["x"] + win["w"] + 3, win["y"] + win["h"] / 2
mouse.drag(ex, ey, ex - 150, ey)
narrower = wait(lambda: (lambda w: w if w["w"] <= win["w"] - 120 else None)(app_window()))
check(bool(narrower), f"dragging the right edge resizes the window ({win['w']} -> {app_window()['w']})")
win = app_window()

# Right-click: KWin's window menu, a popup window of KWin's own.
OTHERS = 'return workspace.windowList().filter((w) => w.resourceClass != "crush-desktop").map((w) => ({ cls: w.resourceClass, caption: w.caption, popup: w.popupWindow }));'
before_menu = kwin_eval(OTHERS)
x0, y0 = empty(win, bar())
mouse.click(x0, y0, button=3)
menus = wait(lambda: [m for m in kwin_eval(OTHERS) if m not in before_menu])
check(bool(menus), f"right-clicking the bar opens KWin's window menu ({menus})")
mouse.click(win["x"] + 300, win["y"] + 400)  # closes the menu
wait(lambda: kwin_eval(OTHERS) == before_menu)

# Maximize / Restore buttons.
b = bar()
mouse.click(*at(win, b["maximize"]))
big = wait(lambda: (lambda w: w if w["w"] == w["area"]["w"] else None)(app_window()))
check(bool(big), "the Maximize button maximizes the window")
b = wait(lambda: (lambda got: got if got and got["maximizeLabel"] == "Restore" else None)(bar())) or bar()
mouse.click(*at(big or app_window(), b["maximize"]))
back = wait(lambda: (lambda w: w if w["w"] == win["w"] else None)(app_window()))
check(bool(back), "the Restore button gives the old size back")
win = app_window()

# The pin: KWin's keep-above, the vermilion rule, remembered.
b = bar()
check(not win["keepAbove"] and not b["pinned"], "the window starts unpinned")
mouse.click(*at(win, b["pin"]))
up = wait(lambda: (lambda w: w if w["keepAbove"] else None)(app_window()))
check(bool(up), "the pin turns on KWin's keep-above for the window")
b = wait(lambda: (lambda got: got if got and got["pinned"] else None)(bar())) or bar()
check(b["pinned"], "pinned, the bar shows it")
v = b["vermilion"].lstrip("#")
vermilion = f"rgb({int(v[0:2], 16)}, {int(v[2:4], 16)}, {int(v[4:6], 16)})"
check(b["rule"] == vermilion, f"pinned, the bar's bottom rule turns vermilion ({b['rule']}, vermilion {vermilion})")
saved = wait(lambda: os.path.exists(PINS) and json.load(open(PINS)))
check(saved == {"main": True}, f"the pin is remembered ({saved})")

# Minimize.
mouse.click(*at(win, b["minimize"]))
low = wait(lambda: (lambda w: w if w["minimized"] else None)(app_window()))
check(bool(low), "the Minimize button minimizes the window")
kwin_eval('const w = workspace.windowList().find((w) => w.resourceClass == "crush-desktop" && w.normalWindow); w.minimized = false; workspace.activeWindow = w; return true;')
time.sleep(0.5)

# Close runs the page's close steps and ends the app.
win = app_window()
b = bar()
mouse.click(*at(win, b["close"]))
code = app.wait_exit(15)
check(code is not None, f"the Close button closes the window and the app ends (exit {code})")
app.stop()

# A pinned window opens pinned again.
app = App()
again = wait(lambda: (lambda w: w if w and w["keepAbove"] else None)(app_window(30)), 20)
check(bool(again), f"reopened, the window is kept above again ({app_window()})")
b = wait(lambda: (lambda got: got if got and got["pinned"] else None)(bar()), 20)
check(bool(b), "reopened, the bar shows the pin on")
if b:
    mouse.click(*at(app_window(), b["pin"]))
    down = wait(lambda: (lambda w: w if not w["keepAbove"] else None)(app_window()))
    check(bool(down), "unpinning turns KWin's keep-above off")
    saved = wait(lambda: os.path.exists(PINS) and json.load(open(PINS)) == {} and {"ok": 1})
    check(bool(saved), "and the window is no longer remembered as pinned")
mouse.close()
app.stop()
finish()
