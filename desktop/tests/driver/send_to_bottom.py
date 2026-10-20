"""Sending brings the chat to its bottom (owner, 2026-10-20: "When I send a
message it should pull me to the bottom of the current chat"). In the real
window on the hidden screen, a long chat is scrolled to its top with the
wheel, a message is typed and sent with Enter, and the chat must end at its
bottom, following.

Usage: CHECK_ARGS="-s 1f0c6a2e-5b7d-4c11-9e3a-7a5d2b8c4e11" \\
       tests/check.sh --fixture tests/fixtures/live-view.json send_to_bottom.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, probe_eval, type_text, wait_for, window_geometry, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


WHERE = """
  const s = document.querySelector('main.conversation .scroller');
  if (!s) return null;
  const r = s.getBoundingClientRect();
  return { gap: s.scrollHeight - s.clientHeight - s.scrollTop, top: s.scrollTop, x: r.x + r.width / 2, y: r.y + r.height / 2 };
"""


def where(want, timeout=4):
    end = time.time() + timeout
    got = None
    while time.time() < end:
        got = run(WHERE) or {}
        if want(got):
            return got
        time.sleep(0.2)
    return got


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
start = where(lambda w: w.get("gap") is not None and w["gap"] <= 2, 15)
check(start and start.get("gap", 99) <= 2, f"the chat opens at its bottom ({start})")
ox, oy = window_geometry()[:2]
xdo("mousemove", round(ox + start["x"]), round(oy + start["y"]))
for _ in range(40):
    xdo("click", 4)
up = where(lambda w: w.get("gap", 0) > 200)
check(up.get("gap", 0) > 200, f"the wheel scrolls it up ({up})")
field = run("const r = document.querySelector('main.conversation textarea')?.getBoundingClientRect(); return r && [r.x + 30, r.y + r.height / 2];")
check(bool(field), "the typing box is there")
xdo("mousemove", round(ox + field[0]), round(oy + field[1]))
xdo("click", 1)
time.sleep(0.3)
type_text("Back to the bottom")
key("Return")
end = where(lambda w: w.get("gap", 99) <= 2)
check(end.get("gap", 99) <= 2, f"sending brings it to its bottom ({end})")
time.sleep(1)
later = where(lambda w: w.get("gap", 99) <= 2, 1)
check(later.get("gap", 99) <= 2, f"and it stays there ({later})")
finish()
