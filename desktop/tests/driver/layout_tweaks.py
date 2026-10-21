"""Layout changes (owner, 2026-10-20), in the real window on the hidden screen
with a chat open:
- no head over the chat: its title, folder and Git branch are in the top bar,
  the branch after the folder;
- New chat and New terminal share one row in equal halves, their words whole
  even with the chat list at its narrowest;
- the right panel never gets so narrow that a meter's words are cut or run
  into the next meter (dragged to its narrowest with the divider's keys);
- the top bar's panel switch hides it and brings it back;
- the vermilion rail starts at the column's top, and the list's "‹‹" sits
  on the middle of the wordmark's capitals.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json layout_tweaks.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, key, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def press_on(label, combo, times):
    """Focuses the column divider named label and presses combo times."""
    run(f"document.querySelector('[aria-label=\"{label}\"]').focus(); return true;")
    for _ in range(times):
        key(combo)
    time.sleep(0.6)


def click(selector):
    box = run(f"const r = document.querySelector({selector!r})?.getBoundingClientRect(); return r && [r.x + r.width / 2, r.y + r.height / 2];")
    if box:
        H.click(*box)
        time.sleep(0.8)
    return bool(box)


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
time.sleep(1.5)

# The head is gone; the top bar holds the title, the folder, then the branch.
bar = run("""
  const conv = document.querySelector('main.conversation');
  const r = (s) => document.querySelector(s)?.getBoundingClientRect();
  const chat = r('[data-probe="title-chat"]'), project = r('[data-probe="title-project"]'), branch = r('[data-probe="title-branch"]');
  return { head: !!conv.querySelector('header.head'), rule: !!conv.querySelector(':scope > .rule.under-head'),
           branch: document.querySelector('[data-probe="title-branch"]')?.textContent.trim() ?? null,
           order: !!(chat && project && branch) && chat.right <= project.left && project.right <= branch.left };
""") or {}
check(not bar.get("head") and not bar.get("rule"), f"no head or head rule over the chat ({bar})")
check(bar.get("branch") == "main" and bar.get("order"), f"the top bar shows the branch after the folder ({bar})")

# The vermilion rail starts at the chat column's top, not where the old head
# ended (owner, 2026-10-20: "the orange line is getting cut off").
rail = run("""
  const r = document.querySelector('.rail')?.getBoundingClientRect(), c = document.querySelector('main.conversation')?.getBoundingClientRect();
  return r && c ? { rail: r.top, column: c.top } : null;
""") or {}
check(rail.get("rail") is not None and abs(rail["rail"] - rail["column"]) <= 0.5, f"the rail starts at the column's top ({rail})")

# The open chat's band reaches 1px past the lines on each side and its words
# stay where a plain row has them; the list's end rule right under it goes
# clear, so no second line shows beneath the band (owner, 2026-10-20).
band = run("""
  const li = document.querySelector('nav.chats .chat.selected'), next = li?.nextElementSibling;
  if (!li || !next || !next.matches('.chat')) return null;
  const r = li.querySelector('.row').getBoundingClientRect(), n = next.getBoundingClientRect();
  const textTop = (e) => e.querySelector('.title').getBoundingClientRect().top - e.getBoundingClientRect().top - parseFloat(getComputedStyle(e).borderTopWidth);
  const end = document.querySelector('nav.chats .rule.end-of-list');
  const groups = [...document.querySelectorAll('nav.chats .group')], last = groups.at(-1)?.querySelector('.chat:last-child');
  const shownEnd = end && getComputedStyle(end).visibility;
  // The last row open, as its class says, while the rule is read.
  li.classList.remove('selected'); last.classList.add('selected');
  const hiddenEnd = end && getComputedStyle(end).visibility;
  last.classList.remove('selected'); li.classList.add('selected');
  return { past: r.bottom - n.top, text: textTop(li), plain: textTop(next), end_after: !!end && end.previousElementSibling === groups.at(-1), shownEnd, hiddenEnd };
""") or {}
check(band.get("past") == 1, f"the open chat's band reaches 1px over the line under it ({band})")
check(band and abs(band["text"] - band["plain"]) <= 0.5, f"its title sits where a plain row's does ({band})")
check(band.get("end_after") and band.get("shownEnd") == "visible" and band.get("hiddenEnd") == "hidden",
      f"the end rule shows under a plain last row and goes clear under an open one ({band})")

# The list's "‹‹" sits on the middle of the wordmark's capitals (owner,
# 2026-10-20: "not centered"), read from the font's own ink box.
mark = run("""
  const crush = document.querySelector('nav.chats .wordmark .crush');
  const pair = document.querySelector('nav.chats .collapse .pair');
  if (!crush || !pair) return null;
  const m = document.createElement('span'); m.style.cssText = 'display:inline-block;width:0;height:0';
  crush.append(m); const baseline = m.getBoundingClientRect().top; m.remove();
  const ctx = document.createElement('canvas').getContext('2d'); ctx.font = getComputedStyle(crush).font;
  const ink = ctx.measureText(crush.textContent.toUpperCase());
  const caps = baseline - (ink.actualBoundingBoxAscent - ink.actualBoundingBoxDescent) / 2;
  const r = pair.getBoundingClientRect();
  return { caps, arrow: r.top + r.height / 2 };
""") or {}
check(mark.get("caps") is not None and abs(mark["caps"] - mark["arrow"]) <= 0.5, f"the list's ‹‹ is centred on the wordmark's capitals ({mark})")

# New chat and New terminal, at the chat list's narrowest.
press_on("Resize the chat list", "shift+Left", 12)
buttons = run("""
  const [a, b] = [...document.querySelectorAll('nav.chats .new-row > button')];
  if (!a || !b) return null;
  const ra = a.getBoundingClientRect(), rb = b.getBoundingClientRect();
  const whole = (e) => { const l = e.querySelector('.label'); return l.getBoundingClientRect().right <= e.getBoundingClientRect().right - 1; };
  return { texts: [a.innerText.trim(), b.innerText.trim()], widths: [ra.width, rb.width], tops: [ra.top, rb.top],
           whole: [whole(a), whole(b)], list: document.querySelector('nav.chats').getBoundingClientRect().width };
""") or {}
check(buttons.get("texts") == ["New chat", "New terminal"], f"New chat and New terminal share a row ({buttons})")
check(buttons and abs(buttons["widths"][0] - buttons["widths"][1]) <= 1 and buttons["tops"][0] == buttons["tops"][1], f"in equal halves ({buttons})")
check(buttons and all(buttons["whole"]), f"their words are whole with the list at its narrowest ({buttons})")

# The panel at its narrowest: every meter's words whole and apart.
press_on("Resize the project panel", "shift+Right", 15)
meters = run("""
  const cells = [...document.querySelectorAll('.panel-column .usage .cell')];
  const cut = cells.flatMap((c) => [c.querySelector('.label'), c.querySelector('.resets')].filter(Boolean)
    .filter((e) => e.scrollWidth > e.clientWidth).map((e) => e.textContent.trim()));
  const spans = cells.map((c) => c.querySelector('.value > span').getBoundingClientRect());
  const overlap = spans.some((s, i) => i > 0 && s.top === spans[i - 1].top && s.left < spans[i - 1].right + 4);
  const into = cells.some((c) => c.querySelector('.value > span').getBoundingClientRect().right > c.getBoundingClientRect().right + 0.5);
  return { count: cells.length, cut, overlap, into, panel: document.querySelector('.panel-column').getBoundingClientRect().width };
""") or {}
shot(out_dir, "narrowest")
check(meters.get("count", 0) >= 2, f"the panel shows its meters ({meters})")
check(not meters.get("error") and not meters.get("cut") and not meters.get("overlap") and not meters.get("into"), f"at the panel's narrowest no meter word is cut or runs into the next ({meters})")

# Hide and bring back: the top bar's panel switch (owner, 2026-10-20, A), the
# only control for it there; the edge tab is gone.
state = "return { panel: !!document.querySelector('.panel-column'), tab: !!document.querySelector('[data-probe=\"panel-tab\"]'), pressed: document.querySelector('[data-probe=\"title-panel\"]').getAttribute('aria-pressed') };"
check(click('[data-probe="title-panel"]'), "the top bar has the panel switch")
gone = run(state) or {}
check(not gone.get("panel") and not gone.get("tab") and gone.get("pressed") == "false", f"it hides the panel, with no tab at the edge ({gone})")
shot(out_dir, "hidden")
# With the panel hidden, the meter strip's words sit midway between the typing
# box's bottom edge and the column's foot (owner, 2026-10-20), read from the
# capture's ink.
strip = run("""
  const field = document.querySelector('main.conversation .composer .field')?.getBoundingClientRect() ?? document.querySelector('main.conversation textarea')?.closest('[class*="field"]')?.getBoundingClientRect();
  const s = document.querySelector('[data-meter-strip]')?.getBoundingClientRect();
  const col = document.querySelector('main.conversation').getBoundingClientRect();
  return field && s ? { field_bottom: field.bottom, foot: col.bottom, x0: s.left, x1: s.right } : null;
""") or {}
if check(bool(strip), f"the meter strip shows under the typing box ({strip})"):
    from PIL import Image
    ox, oy = H.window_geometry()[:2]
    image = Image.open(os.path.join(out_dir, "hidden.png")).convert("L")
    xs = range(round(ox + strip["x0"]), round(ox + strip["x1"]))
    bg = image.getpixel((xs[0], round(oy + strip["field_bottom"]) + 3))
    marked = lambda y: sum(abs(image.getpixel((x, y)) - bg) > 40 for x in xs) / len(xs)
    # The space runs from the typing box's bottom border to the frame line under
    # the column (both rows inked edge to edge); the words are the rows between.
    rows = range(round(oy + strip["field_bottom"]) - 3, round(oy + strip["foot"]) + 4)
    lines = [y for y in rows if marked(y) > 0.9]
    ink = [y for y in rows if 0 < marked(y) <= 0.9 and lines and lines[0] < y < lines[-1]]
    above, below = (ink[0] - lines[0] - 1, lines[-1] - ink[-1] - 1) if ink and len(lines) >= 2 else (None, None)
    check(ink and abs(above - below) <= 1, f"the strip's words sit midway: {above} px above, {below} px below")
click('[data-probe="title-panel"]')
back = run(state) or {}
check(back.get("panel") and back.get("pressed") == "true", f"it brings the panel back ({back})")
finish()
