"""A reply lands in view after a tiny nudge (owner, 2026-10-22: "sometimes
when the AI responds the chat doesn't scroll down to their response, even if
I'm at the bottom"). In the real window on the hidden screen, a chat at its
bottom is nudged up 2 pixels, as a touchpad can, and the reply that arrives
next must still be followed to the bottom. Scrolled well up (200 pixels),
the next reply must leave the view where it is. Last, back at the bottom
(owner, 2026-10-20: "scrolling further down pushes it up/down 1-2 pixels"),
more wheel downs, then a few ups and many downs back past the bottom: every
frame the last row's place is sampled, and once the chat is at its bottom the
row must never move.

Usage: CHECK_ARGS="-s 1f0c6a2e-5b7d-4c11-9e3a-7a5d2b8c4e11" \\
       tests/check.sh --fixture tests/fixtures/scroll-follow.json follow_nudge.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, window_geometry, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
SCROLLER = "document.querySelector('main.conversation .slot:not(.parked) .scroller')"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def where():
    return run(f"""const s = {SCROLLER}; if (!s) return null;
      return {{ gap: s.scrollHeight - s.clientHeight - s.scrollTop, height: s.scrollHeight, text: document.querySelector('main.conversation .slot:not(.parked)').textContent }};""")


def until(test, timeout):
    end = time.time() + timeout
    while time.time() < end:
        w = where()
        if w and test(w):
            return w
        time.sleep(0.2)
    return where()


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 30)
w = until(lambda w: w["gap"] <= 1 and w["height"] > 2000, 15)
check(w and w["gap"] <= 1, f"the chat opens at its bottom (gap {w and w['gap']})")

# 1. A 2-pixel nudge up, then a reply: it is followed. Replies may land out
# of view and not be drawn, so their arrival shows as the chat growing.
run(f"{SCROLLER}.scrollTop -= 2;")
time.sleep(0.5)
before = where()
check(before and before["gap"] <= 3, f"after the nudge the chat still looks at its bottom (gap {before and before['gap']})")
w = until(lambda w: w["height"] > before["height"] + 100, 25)
check(w and w["height"] > before["height"] + 100, f"the first reply arrives ({before and before['height']} -> {w and w['height']})")
w = until(lambda w: w["gap"] <= 1, 5)
check(w and w["gap"] <= 1, f"the reply after a tiny nudge is followed to the bottom (gap {w and w['gap']})")

# 2. Well up (200 pixels), then a reply: the view stays.
run(f"{SCROLLER}.scrollTop -= 200;")
time.sleep(0.5)
before = where()
w = until(lambda w: w["height"] > before["height"] + 100, 25)
check(w and w["height"] > before["height"] + 100, f"the second reply arrives ({before and before['height']} -> {w and w['height']})")
# A short watch: a late follow would close the gap within it.
end = time.time() + 0.5
while time.time() < end:
    w = where()
    if not (w and w["gap"] > before["gap"] + 100):
        break
    time.sleep(0.05)
check(w and w["gap"] > before["gap"] + 100, f"scrolled well up, the next reply leaves the view where it is (gap {before['gap']} -> {w and w['gap']})")

# 3. Back at the bottom, more wheel downs never move the last row.
CENTER = f"""const s = {SCROLLER}; if (!s) return null; const r = s.getBoundingClientRect();
  return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};"""

# Every frame: the scroller's gap to its end and the last row's top on screen.
SAMPLE = f"""
  window.__still = [];
  const s = {SCROLLER};
  const tick = () => {{
    const rows = s.querySelectorAll('.slot');
    const last = rows[rows.length - 1];
    window.__still.push({{ gap: Math.round((s.scrollHeight - s.clientHeight - s.scrollTop) * 100) / 100,
                          row: last ? Math.round(last.getBoundingClientRect().top * 100) / 100 : null,
                          t: s.scrollTop }});
    if (window.__still.length < 4000) requestAnimationFrame(tick);
  }};
  requestAnimationFrame(tick);
  return true;
"""


def moves(samples):
    """Row moves once a down phase has reached the bottom (gap 0)."""
    out, prev = [], None
    for i, s in enumerate(samples):
        if s.get("row") is None:
            continue
        if prev is None:
            if s.get("gap", 99) <= 0.5:
                prev = s
            continue
        if abs(s["row"] - prev["row"]) >= 0.5:
            out.append({"at": i, "of": len(samples), "t": [x["t"] for x in samples[max(0, i - 3):i + 8]]})
        prev = s
    return out


def settle(timeout=5):
    """Waits until the last 10 frames share one scroll place; returns the samples."""
    end = time.time() + timeout
    got = []
    while time.time() < end:
        got = run("return window.__still;")
        if isinstance(got, list) and len(got) >= 10 and len({x["t"] for x in got[-10:]}) == 1:
            return got
        time.sleep(0.05)
    return got if isinstance(got, list) else []


def downs(count):
    """Samples while the wheel goes down count times, until the chat settles."""
    run(SAMPLE)
    for _ in range(count):
        xdo("click", 5)
        time.sleep(0.05)
    return settle()


run(f"const s = {SCROLLER}; s.scrollTop = s.scrollHeight; return true;")
w = until(lambda w: w["gap"] <= 1, 5)
check(w and w["gap"] <= 1, f"the chat is back at its bottom (gap {w and w['gap']})")
center = run(CENTER) or {}
ox, oy = window_geometry()[:2]
xdo("mousemove", round(ox + center.get("x", 0)), round(oy + center.get("y", 0)))
first = downs(12)
run(SAMPLE)
for _ in range(3):
    xdo("click", 4)
    time.sleep(0.05)
settle()
second = downs(15)
found = [("first", m) for m in moves(first)] + [("second", m) for m in moves(second)]
check(len(first) >= 10 and len(second) >= 10 and not found,
      f"at its bottom the chat never moves ({len(first)}+{len(second)} frames, {len(found)} moves: {found[:4]})")
finish()
