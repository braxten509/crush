"""Everything that opens also closes with motion (owner, 2026-10-20: "This is
missing a close animation. EVERYHING needs an open and close animation"). In
the real window on the hidden screen, each piece is opened, left to settle,
then closed while the page notes every frame until the piece leaves: it must
stay on screen with its closing animation running for several frames, and be
gone within about the close time (0.14 s, lib/motion.js), not vanish at once.

Pieces: the right panel and the usage strip under the typing box, the chat
details overlay, a sheet and its scrim, the keyboard help, the "/" popup, a
tool group's list, the right-click menu, Settings (its page and its section
list), the chat list collapsing to its strip and back (its width glides;
the strip itself leaves at once as the list opens, owner 2026-10-21), and a
form in the typing box's place.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json close_motion.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, probe_eval, right_click, type_text, wait_for, window_geometry, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


# watch(name, selector): every frame until the element leaves the page, how
# many frames it showed a running animation, when the first was, and when it went.
run("""
  window.__watch = {};
  window.__watchStart = (name, sel) => {
    const el = document.querySelector(sel);
    const rec = window.__watch[name] = { found: Boolean(el), moving: 0, first: null, gone: null };
    if (!el) return;
    const t0 = performance.now();
    const frame = () => {
      const t = performance.now() - t0;
      if (!el.isConnected) { rec.gone = Math.round(t); return; }
      if (el.getAnimations().some((a) => a.playState === 'running')) { rec.moving += 1; rec.first ??= Math.round(t); }
      if (t < 3000) requestAnimationFrame(frame);
    };
    requestAnimationFrame(frame);
  };
  return true;
""")


def closes(name, selector, close, settle=0.6):
    """Starts the watch on selector, runs close (JS text or a Python callable),
    and checks the piece left with its animation running."""
    time.sleep(settle)
    if isinstance(close, str):
        run(f"window.__watchStart({name!r}, {selector!r}); {close}; return true;")
    else:
        run(f"window.__watchStart({name!r}, {selector!r}); return true;")
        close()
    time.sleep(0.8)
    rec = run(f"return window.__watch[{name!r}];") or {}
    ok = rec.get("found") and rec.get("gone") is not None and rec.get("moving", 0) >= 3 and rec["gone"] - (rec.get("first") or 0) <= 320
    check(ok, f"{name} closes with motion ({rec})")


# track(selector, side): every frame for 0.6 s, the element's rect side, to
# show the room round a piece glides rather than jumps.
def track(selector, side):
    run(f"""
      window.__track = [];
      const el = document.querySelector({selector!r});
      const t0 = performance.now();
      const frame = () => {{ if (el) window.__track.push(Math.round(el.getBoundingClientRect()[{side!r}] * 10) / 10); if (performance.now() - t0 < 600) requestAnimationFrame(frame); }};
      requestAnimationFrame(frame);
      return true;
    """)


def glided(what):
    values = run("return window.__track;") or []
    between = {v for v in values if min(values or [0]) < v < max(values or [0])}
    check(len(between) >= 3, f"{what} ({min(values or [0])}..{max(values or [0])}, {len(between)} values between)")


# Every frame for 0.7 s: the shortest piece in the left column against the
# column's own height. A piece shorter than its column has its foot pushed up
# (owner, 2026-10-25: "squished to the top during the transition").
def watch_full_height(column):
    run(f"""
      window.__short = [];
      const t0 = performance.now();
      const frame = () => {{
        const col = document.querySelector({column!r});
        if (col) for (const el of col.children) {{
          const gap = col.getBoundingClientRect().height - el.getBoundingClientRect().height;
          if (gap > 1) window.__short.push([el.className.split(' ')[0], Math.round(gap)]);
        }}
        if (performance.now() - t0 < 700) requestAnimationFrame(frame);
      }};
      requestAnimationFrame(frame);
      return true;
    """)


def full_height(what):
    time.sleep(0.8)
    short = run("return window.__short;") or []
    check(not short, f"{what} keeps its full height ({short[:4]})")


def click_js(selector):
    return f"document.querySelector({selector!r}).click()"


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
time.sleep(1.5)
ox, oy = window_geometry()[:2]

# The right panel; then, with it hidden, the usage strip and the details overlay.
# The conversation widens as the panel leaves, not once it has gone (owner,
# 2026-10-20), and narrows back as it comes.
track("main.conversation", "width")
# The panel's words hold still while the moving edge covers them: same
# place, same width and height, never rewrapped, the foot kept at the
# bottom (owner, 2026-10-25).
run("""
  const el = document.querySelector('.panel-column > aside');
  window.__panelRects = [];
  const t0 = performance.now();
  const frame = () => {
    if (!el.isConnected || performance.now() - t0 > 600) return;
    const r = el.getBoundingClientRect();
    window.__panelRects.push([Math.round(r.right * 10) / 10, Math.round(r.width * 10) / 10, Math.round(r.height * 10) / 10]);
    requestAnimationFrame(frame);
  };
  requestAnimationFrame(frame);
  return true;
""")
closes("the right panel", ".panel-column", click_js('[data-probe="title-panel"]'), settle=0)
glided("the conversation widens with the panel as it closes")
panel_rects = run("return window.__panelRects;") or []
check(len(panel_rects) >= 3 and len({tuple(r) for r in panel_rects}) == 1,
      f"the panel's content keeps its place and width as it closes ({sorted({tuple(r) for r in panel_rects})[:4]})")
run("window.__probe.runAction('details.toggle'); return true;")
closes("the details overlay", ".details", "window.__probe.runAction('details.toggle')")
time.sleep(0.6)
track("main.conversation", "width")
run("""
  window.__panelRects = [];
  const t0 = performance.now();
  const frame = () => {
    const el = document.querySelector('.panel-column > aside');
    if (performance.now() - t0 > 600) return;
    if (el) { const r = el.getBoundingClientRect(); window.__panelRects.push([Math.round(r.right * 10) / 10, Math.round(r.width * 10) / 10, Math.round(r.height * 10) / 10]); }
    requestAnimationFrame(frame);
  };
  requestAnimationFrame(frame);
  return true;
""")
closes("the usage strip", "[data-meter-strip]", click_js('[data-probe="title-panel"]'), settle=0)
glided("the conversation narrows with the panel as it opens")
panel_rects = run("return window.__panelRects;") or []
check(len({tuple(r) for r in panel_rects}) == 1,
      f"the panel's content is revealed in its place as it opens ({sorted({tuple(r) for r in panel_rects})[:4]})")

# A sheet and its scrim.
run("window.__probe.openDialog('about'); return true;")
time.sleep(0.6)
run("window.__watchStart('the scrim', '.scrim'); return true;")
closes("a sheet", '[data-sheet="about"]', "window.__probe.closeDialog('about')", settle=0)
time.sleep(0.2)
rec = run("return window.__watch['the scrim'];") or {}
check(rec.get("found") and rec.get("gone") is not None and rec.get("moving", 0) >= 3, f"the scrim fades out ({rec})")

# The keyboard help.
run("window.__probe.runAction('help.toggle'); return true;")
closes("the keyboard help", "[data-help-sheet]", "window.__probe.runAction('help.toggle')")

# The "/" popup, closed with Esc.
field = run("const r = document.querySelector('main.conversation textarea')?.getBoundingClientRect(); return r && [r.x + 30, r.y + r.height / 2];")
if check(bool(field), "the typing box is there"):
    xdo("mousemove", round(ox + field[0]), round(oy + field[1]))
    xdo("click", 1)
    time.sleep(0.3)
    type_text("/")
    closes("the / popup", '[data-probe="popup"]', lambda: key("Escape"))
    key("BackSpace")

# A tool group's list; what lies below it rises with it as it closes.
run(click_js(".timeline .tool-row[aria-expanded]") + "; return true;")
time.sleep(0.6)
below = run("""
  const list = document.querySelector('.timeline .group .list');
  let el = list?.closest('.group');
  while (el && !el.nextElementSibling) el = el.parentElement;
  const next = el?.nextElementSibling;
  if (next) next.setAttribute('data-probe-below', '');
  return Boolean(next);
""")
if check(bool(below), "something lies below the tool group"):
    track("[data-probe-below]", "top")
closes("a tool group's list", ".timeline .group .list", click_js(".timeline .tool-row[aria-expanded]"), settle=0)
if below:
    glided("what lies below a tool group rises with its list as it closes")

# The right-click menu on a message, closed with Esc.
spot = run("const r = document.querySelector('.timeline .frame')?.getBoundingClientRect(); return r && [r.x + 40, r.y + 12];")
if check(bool(spot), "a message to right-click"):
    right_click(round(spot[0]), round(spot[1]))
    closes("the right-click menu", "[data-menu]", lambda: key("Escape"))

# Settings: its page slides away over the chat, its list over the chats.
watch_full_height(".chats-column")
run(click_js("nav.chats button.settings") + "; return true;")
full_height("the left column as Settings opens")
run("window.__watchStart('the Settings list', '.settings-nav'); return true;")
watch_full_height(".chats-column")
closes("the Settings page", ".main-view", click_js(".settings-nav button.back"), settle=0)
full_height("the left column as Settings closes")
rec = run("return window.__watch['the Settings list'];") or {}
check(rec.get("found") and rec.get("gone") is not None and rec.get("moving", 0) >= 3, f"the Settings list closes with motion ({rec})")
# The chat list collapsing to its strip and back: each side rolls away while
# the column's width glides through widths between (frame by frame).
run("""
  window.__widths = [];
  const col = document.querySelector('.chats-column');
  const t0 = performance.now();
  const frame = () => { window.__widths.push(Math.round(col.getBoundingClientRect().width)); if (performance.now() - t0 < 600) requestAnimationFrame(frame); };
  requestAnimationFrame(frame);
  return true;
""")
watch_full_height(".chats-column")
closes("the chat list (collapsing)", "nav.chats", click_js('button.collapse[aria-label="Collapse the list"]'), settle=0)
full_height("the left column as the list collapses")
widths = run("return window.__widths;") or []
between = [w for w in widths if min(widths or [0]) < w < max(widths or [0])]
check(len(set(between)) >= 3, f"the list's width glides as it collapses ({sorted(set(widths))[:3]}..{sorted(set(widths))[-3:]}, {len(set(between))} widths between)")
# Opening, the strip leaves at once and the moving edge uncovers the list
# (owner, 2026-10-21, look A): the motion is the column's width gliding open.
time.sleep(0.6)
run("""
  window.__widths = [];
  const col = document.querySelector('.chats-column');
  const t0 = performance.now();
  const frame = () => { window.__widths.push(Math.round(col.getBoundingClientRect().width)); if (performance.now() - t0 < 600) requestAnimationFrame(frame); };
  requestAnimationFrame(frame);
  window.__watchStart('the strip (expanding)', 'nav.strip');
  return true;
""")
watch_full_height(".chats-column")
run(click_js("nav.strip button.expand") + "; return true;")
full_height("the left column as the list opens")
rec = run("return window.__watch['the strip (expanding)'];") or {}
check(rec.get("found") and rec.get("gone") is not None and rec.get("moving", 0) == 0, f"the strip leaves at once as the list opens ({rec})")
widths = run("return window.__widths;") or []
between = [w for w in widths if min(widths or [0]) < w < max(widths or [0])]
check(len(set(between)) >= 3, f"the list's width glides as it opens ({sorted(set(widths))[:3]}..{sorted(set(widths))[-3:]}, {len(set(between))} widths between)")

# A form in the typing box's place (Customize composer), closed with Esc.
# It leaves from right where it stood, then only rolls down: no jump up
# first (owner, 2026-10-25: "it teleports a tab's width up").
run("window.__probe.runAction('composer.customize'); return true;")
time.sleep(0.6)
run("""
  const el = document.querySelector('[data-probe="slot"]');
  const at = el.getBoundingClientRect();
  window.__slotRects = { open: [at.top, at.bottom], frames: [] };
  const frame = () => {
    if (!el.isConnected) return;
    const r = el.getBoundingClientRect();
    window.__slotRects.frames.push([r.top, r.bottom]);
    requestAnimationFrame(frame);
  };
  requestAnimationFrame(frame);
  return true;
""")
closes("a typing-box form", '[data-probe="slot"]', lambda: key("Escape"), settle=0)
rects = run("return window.__slotRects;") or {}
open_top, open_bottom = (rects.get("open") or [0, 0])
moving = [f for f in rects.get("frames") or [] if abs(f[0] - open_top) > 0.5 or abs(f[1] - open_bottom) > 0.5]
check(bool(moving) and all(abs((f[1] - f[0]) - (open_bottom - open_top)) <= 1 and f[1] >= open_bottom - 1 for f in moving),
      f"a closing typing-box form keeps its size and only rolls down from where it stood (open {open_top:.1f}..{open_bottom:.1f}, first moving frames {moving[:3]})")
finish()
