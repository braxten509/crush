"""The owner's fixes of 2026-10-20 (late), in the real window at the default
Compact zoom, with real keys and clicks:

panels (the live-tray recording, its chat with three to-dos):
- the TO-DO steps stack one under the other at the panel's full width (they
  were laid out side by side, each wrapping letter by letter);
- the collapsed list's "›" cell is 44 picture px tall with the arrow in its
  middle (the arrow sat low in a 67-tall box);
- the Send square is square, its plane in the exact middle (it sat 3 picture
  px right);
- the step in progress shows the busy dots, pulsing on the page's one beat
  (owner, 2026-10-21: it was a still "…");
- the panel foot's words are Settings' size, inside their button (owner,
  2026-10-21); the line before the model button runs the typing box's
  height as the box grows (owner, 2026-10-21).

engine (the real engine and the fake AI server):
- a reply's footer names the model and provider from the config, "Fake Large
  via Fake AI", never "Unknown Model" (the agent CLIs were missing);
- while a reply runs and the box is empty, the Send square is Stop (owner's
  choice A); typing turns it back into Send and clearing it into Stop again;
  a click on Stop ends the run and the typing box keeps the focus;
- the running reply's three dots are whole pixels of one width (the last one
  came out thinner, "cut off");
- they pulse on the page's one beat, and keep it after Settings hides the chat
  and gives it back (owner, 2026-10-21: "so it's not two different rythms").

Usage: CHECK_ARGS="-s 1f0c6a2e-5b7d-4c11-9e3a-7a5d2b8c4e01" \\
       tests/check.sh --fixture tests/fixtures/live-tray.json owner_fixes.py panels
       tests/check.sh owner_fixes.py engine        (no CHECK_ARGS)
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
parts = [a for a in sys.argv[3:] if not a.isdigit()] or ["panels"]


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


def shot(name, wait=0.4):
    time.sleep(wait)
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, name + ".png")])


def rect(selector):
    return run(f"const e = document.querySelector({json.dumps(selector)}); if (!e) return null; const r = e.getBoundingClientRect(); return {{ x: r.x, y: r.y, w: r.width, h: r.height }};")


def click_at(r):
    xdo("mousemove", geo_x + round(r["x"] + r["w"] / 2), geo_y + round(r["y"] + r["h"] / 2))
    xdo("click", 1)


def key(*combos):
    for combo in combos:
        xdo("key", "--clearmodifiers", combo)


# Where each busy dot of `selector` stands against the page's one beat (motion.js
# busyBeat): its fade runs 1.2 s, each dot 0.2 s after the one before, counted
# from the page's start.
def beat(selector):
    return run("""
      const now = document.timeline.currentTime;
      return [...document.querySelectorAll(%s)].map((dot) => {
        const fades = dot.getAnimations().filter((a) => a.playState === 'running');
        if (fades.length !== 1) return fades.length + ' fades';
        const at = [...dot.parentNode.children].indexOf(dot);
        const want = ((((now - 200 * at) %% 1200) + 1200) %% 1200) / 1200;
        const off = Math.abs(fades[0].effect.getComputedTiming().progress - want);
        return Math.min(off, 1 - off) < 0.02 ? 'on beat' : 'off by ' + Math.round(Math.min(off, 1 - off) * 1200) + ' ms';
      });
    """ % json.dumps(selector))


def until(script, timeout=10, step=0.2):
    end = time.time() + timeout
    while time.time() < end:
        got = run(script)
        if got is True:
            return True
        time.sleep(step)
    return False


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(1.5)
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])
xdo("mousemove", geo_x + 5, geo_y + 980)

PX = run("const e = document.createElement('div'); e.style.width = 'calc(100 * var(--px))'; document.body.append(e); const w = e.getBoundingClientRect().width / 100; e.remove(); return w;") or 1
check(abs(PX - 2 / 3) < 0.01, f"the window is at the default Compact zoom, a picture pixel {PX:.4f} real ones")

if "panels" in parts:
    # -- The to-do steps --
    todo = run("""
      const sec = document.querySelector('[data-section="todos"]');
      if (!sec) return null;
      const box = sec.getBoundingClientRect();
      const rows = [...sec.querySelectorAll('li.line')].map((li) => { const r = li.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height, bottom: r.bottom }; });
      const text = sec.querySelector('li.line .text');
      return { width: box.width, rows, line: text ? parseFloat(getComputedStyle(text).lineHeight) : 0 };
    """)
    if check(todo is not None and len(todo.get("rows", [])) == 3, f"the TO-DO section lists the chat's three steps ({todo and len(todo.get('rows', []))})"):
        rows = todo["rows"]
        check(all(rows[i]["y"] >= rows[i - 1]["bottom"] - 1 for i in range(1, 3)), f"the steps stack one under the other ({[round(r['y']) for r in rows]})")
        check(all(abs(r["x"] - rows[0]["x"]) <= 1 for r in rows), "every step starts at the same left edge")
        check(all(r["w"] >= 0.8 * todo["width"] for r in rows), f"every step takes the panel's width ({[round(r['w']) for r in rows]} of {round(todo['width'])})")
        check(all(r["h"] <= 2.5 * todo["line"] for r in rows), f"no step wraps letter by letter (heights {[round(r['h']) for r in rows]}, line {todo['line']:.1f})")
        dots = beat('[data-section="todos"] li.in_progress .mark .dot')
        check(dots == ["on beat"] * 3, f"the step in progress shows the busy dots, on the page's one beat ({dots})")
    shot("panels")

    # -- The Send square --
    send = run("""
      const b = document.querySelector('.composer .send');
      if (!b) return null;
      const r = b.getBoundingClientRect(), i = b.querySelector('svg').getBoundingClientRect();
      return { w: r.width, h: r.height, dx: (i.x + i.width / 2) - (r.x + r.width / 2), dy: (i.y + i.height / 2) - (r.y + r.height / 2), label: b.getAttribute('aria-label') };
    """)
    if check(send is not None, "the typing box has its Send square"):
        check(abs(send["w"] - send["h"]) <= 0.75, f"the Send square is square ({send['w']:.2f} x {send['h']:.2f})")
        check(abs(send["dx"]) <= 0.75 and abs(send["dy"]) <= 0.75, f"its plane sits in the middle (off by {send['dx']:.2f}, {send['dy']:.2f})")
        check(send["label"] == "Send", f"with nothing running it is Send ({send['label']})")

    # -- The strip's "›" cell --
    key("alt+s")
    if check(until("return !!document.querySelector('.strip .expand');", 5), "Alt+S collapses the list to the strip"):
        time.sleep(0.4)
        cell = run("""
          const b = document.querySelector('.strip .expand');
          const r = b.getBoundingClientRect(), i = b.querySelector('svg').getBoundingClientRect();
          return { h: r.height, dx: (i.x + i.width / 2) - (r.x + r.width / 2), dy: (i.y + i.height / 2) - (r.y + r.height / 2) };
        """)
        check(abs(cell["h"] - 44 * PX) <= 1, f"the › cell is 44 picture px tall ({cell['h']:.1f} real, {44 * PX:.1f} expected)")
        check(abs(cell["dx"]) <= 1 and abs(cell["dy"]) <= 1, f"its arrow sits in the middle of the cell (off by {cell['dx']:.2f}, {cell['dy']:.2f})")
        r = rect(".strip .expand")
        xdo("mousemove", geo_x + round(r["x"] + r["w"] / 2), geo_y + round(r["y"] + r["h"] / 2))
        shot("strip-hover")
        key("alt+s")
        check(until("return !document.querySelector('.strip');", 5), "Alt+S opens the list again")
    xdo("mousemove", geo_x + 5, geo_y + 980)

    # -- The feet's words --
    feet = run("""
      const foot = document.querySelector('[data-probe="update-foot"]');
      const words = [document.querySelector('nav.chats button.settings .label'), foot?.querySelector('.version'), foot?.querySelector('.update .label')];
      if (words.some((w) => !w)) return null;
      const button = foot.querySelector('.update').getBoundingClientRect(), label = words[2].getBoundingClientRect();
      return { sizes: words.map((w) => getComputedStyle(w).fontSize), inside: label.top >= button.top - 0.5 && label.bottom <= button.bottom + 0.5, label: [label.top, label.bottom], button: [button.top, button.bottom] };
    """)
    if check(feet is not None, "both feet show their words"):
        check(len(set(feet["sizes"])) == 1, f"the panel foot's words are as big as Settings' ({feet['sizes']})")
        check(feet["inside"], f"the update button's words fit inside it (words {feet['label']}, button {feet['button']})")

    # -- The line before the model button --
    def line_span():
        return run("""
          const field = document.querySelector('.composer .field').getBoundingClientRect();
          const line = document.querySelector('.composer .divider').getBoundingClientRect();
          return { field: field.height, top: line.top - field.top, bottom: field.bottom - line.bottom };
        """)
    one = line_span()
    click_at(rect(".composer textarea"))
    for _ in range(3):
        key("shift+Return")
    time.sleep(0.4)
    four = line_span()
    gap = 7 * PX + 1  # 7 picture px inside the field's one-pixel border
    spans = lambda g: g is not None and abs(g["top"] - gap) <= 1 and abs(g["bottom"] - gap) <= 1
    check(spans(one) and spans(four) and four["field"] > one["field"] + 40, f"the line runs the typing box's height as it grows ({one} -> {four})")
    key("ctrl+a", "BackSpace")

if "engine" in parts:
    # The new project's init question holds the keys until it is answered.
    nope = run("const b = [...document.querySelectorAll('button')].find((e) => e.textContent.trim() === 'Not now'); if (!b) return null; const r = b.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height };")
    if nope:
        click_at(nope)
        time.sleep(0.6)
    field = rect(".composer textarea")
    if not check(field is not None, "the typing box is on the page"):
        finish()
    click_at(field)
    time.sleep(0.3)

    # -- A reply's footer --
    xdo("type", "--delay", "15", "--", "Seed turn short")
    key("Return")
    done = until("return [...document.querySelectorAll('.timeline [data-kind=footer]')].length > 0 && !document.querySelector('.timeline .status');", 20)
    footer = run("return [...document.querySelectorAll('.timeline [data-kind=footer]')].map((e) => e.textContent.trim()).pop() ?? null;")
    check(done and footer is not None, f"the reply finishes with its footer ({footer!r})")
    check(footer is not None and "Unknown Model" not in footer and footer.startswith("Fake Large via Fake AI in "), f"the footer names the model and provider from the config ({footer!r})")

    # -- Stop --
    label = lambda: run("return document.querySelector('.composer .send')?.getAttribute('aria-label') ?? null;")
    click_at(rect(".composer textarea"))
    xdo("type", "--delay", "15", "--", "leftlist slow reply")
    key("Return")
    working = until("return !!document.querySelector('.timeline .status');", 10)
    check(working, "the slow reply runs (the live status shows)")
    time.sleep(0.3)
    check(label() == "Stop", f"while it runs with an empty box, the square is Stop ({label()})")
    dots = run("return [...document.querySelectorAll('.timeline .status .mark .dot')].map((d) => d.getBoundingClientRect().width);") or []
    check(len(dots) == 3 and max(dots) - min(dots) < 0.01 and all(abs(w - round(w)) < 0.01 for w in dots), f"its three dots are whole pixels of one width ({dots})")
    shot("stop")
    xdo("type", "--delay", "15", "--", "x")
    time.sleep(0.3)
    check(label() == "Send", f"typing turns it back into Send ({label()})")
    key("BackSpace")
    time.sleep(0.3)
    check(label() == "Stop", f"clearing the box turns it into Stop again ({label()})")
    click_at(rect(".composer .send"))
    stopped = until("return !document.querySelector('.timeline .status');", 8)
    check(stopped, "a click on Stop ends the run")
    time.sleep(0.4)
    check(label() == "Send", f"and the square is Send again ({label()})")
    check(run("return document.activeElement === document.querySelector('.composer textarea');") is True, "the typing box keeps the focus")

    # -- One beat --
    xdo("type", "--delay", "15", "--", "leftlist slow reply")
    key("Return")
    if check(until("return !!document.querySelector('.timeline .status .mark');", 10), "another slow reply runs"):
        time.sleep(0.3)
        dots = beat(".timeline .status .mark .dot")
        check(dots == ["on beat"] * 3, f"its dots pulse on the page's one beat ({dots})")
        click_at(rect("nav.chats button.settings"))
        time.sleep(0.7)
        click_at(rect(".settings-nav button.back"))
        time.sleep(0.7)
        dots = beat(".timeline .status .mark .dot")
        check(dots == ["on beat"] * 3, f"and keep it after Settings hid the chat and gave it back ({dots})")
finish()
