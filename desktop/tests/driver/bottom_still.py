"""The chat stays still at its bottom (owner, 2026-10-20: "when I scroll to
the bottom of the chat, sometimes scrolling further down pushes it up/down
1-2 pixels which it shouldn't"). In the real window on the hidden screen, a
long chat at its bottom gets more wheel downs, then a few ups and many downs
back past the bottom; every frame the last row's place is sampled, and once
the chat is at its bottom the row must never move.

Usage: CHECK_ARGS="-s 1f0c6a2e-5b7d-4c11-9e3a-7a5d2b8c4e11" \\
       tests/check.sh --fixture tests/fixtures/live-view.json bottom_still.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, window_geometry, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


WHERE = """
  const s = document.querySelector('main.conversation .scroller');
  if (!s) return null;
  const r = s.getBoundingClientRect();
  return { gap: s.scrollHeight - s.clientHeight - s.scrollTop, x: r.x + r.width / 2, y: r.y + r.height / 2 };
"""

# Every frame: the scroller's gap to its end and the last row's top on screen.
SAMPLE = """
  window.__still = [];
  const s = document.querySelector('main.conversation .scroller');
  if (!window.__sets) {
    window.__sets = [];
    const d = Object.getOwnPropertyDescriptor(Element.prototype, 'scrollTop');
    Object.defineProperty(s, 'scrollTop', { configurable: true, get() { return d.get.call(this); },
      set(v) { window.__sets.push({ v, before: d.get.call(this), n: window.__still.length, h: this.scrollHeight, stack: new Error().stack.slice(0, 500) }); d.set.call(this, v); } });
  }
  const tick = () => {
    const rows = s.querySelectorAll('.slot');
    const last = rows[rows.length - 1];
    window.__still.push({ gap: Math.round((s.scrollHeight - s.clientHeight - s.scrollTop) * 100) / 100,
                          row: last ? Math.round(last.getBoundingClientRect().top * 100) / 100 : null,
                          h: s.scrollHeight, t: s.scrollTop });
    if (window.__still.length < 4000) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return true;
"""


def moves(samples):
    """Row moves once a down phase has reached the bottom (gap 0)."""
    out = []
    prev = None
    for s in samples:
        if s.get("row") is None:
            continue
        if prev is None:
            if s.get("gap", 99) <= 0.5:
                prev = s
            continue
        if abs(s["row"] - prev["row"]) >= 0.5:
            i = samples.index(s)
            out.append({"at": i, "of": len(samples), "t": [x["t"] for x in samples[max(0, i - 3):i + 8]]})
        prev = s
    return out


def downs(count):
    """Samples while the wheel goes down count times, then settles."""
    run(SAMPLE)
    for _ in range(count):
        xdo("click", 5)
        time.sleep(0.05)
    time.sleep(1.2)
    got = run("return window.__still;")
    if not isinstance(got, list):
        print("SAMPLES", str(got)[:800])
        return []
    return got


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
end = time.time() + 15
start = None
while time.time() < end:
    start = run(WHERE) or {}
    if start.get("gap") is not None and start["gap"] <= 2:
        break
    time.sleep(0.2)
check(start and start.get("gap", 99) <= 2, f"the chat opens at its bottom ({start})")
time.sleep(1.5)
ox, oy = window_geometry()[:2]
xdo("mousemove", round(ox + start["x"]), round(oy + start["y"]))

first = downs(12)
for _ in range(3):
    xdo("click", 4)
    time.sleep(0.05)
time.sleep(0.8)
second = downs(15)
found = [("first", m) for m in moves(first)] + [("second", m) for m in moves(second)]
check(len(first) > 50 and len(second) > 50 and not found,
      f"at its bottom the chat never moves ({len(first)}+{len(second)} frames, {len(found)} moves: {found[:4]})")
finish()
