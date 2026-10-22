"""A reply lands in view after a tiny nudge (owner, 2026-10-22: "sometimes
when the AI responds the chat doesn't scroll down to their response, even if
I'm at the bottom"). In the real window on the hidden screen, a chat at its
bottom is nudged up 2 pixels, as a touchpad can, and the reply that arrives
next must still be followed to the bottom. Scrolled well up (200 pixels),
the next reply must leave the view where it is.

Usage: CHECK_ARGS="-s 1f0c6a2e-5b7d-4c11-9e3a-7a5d2b8c4e11" \\
       tests/check.sh --fixture tests/fixtures/scroll-follow.json follow_nudge.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for

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
time.sleep(1.0)
w = where()
check(w and w["height"] > before["height"] + 100, f"the first reply arrives ({before and before['height']} -> {w and w['height']})")
check(w and w["gap"] <= 1, f"the reply after a tiny nudge is followed to the bottom (gap {w and w['gap']})")

# 2. Well up (200 pixels), then a reply: the view stays.
run(f"{SCROLLER}.scrollTop -= 200;")
time.sleep(0.5)
before = where()
w = until(lambda w: w["height"] > before["height"] + 100, 25)
time.sleep(1.0)
w = where()
check(w and w["height"] > before["height"] + 100, f"the second reply arrives ({before and before['height']} -> {w and w['height']})")
check(w and w["gap"] > before["gap"] + 100, f"scrolled well up, the next reply leaves the view where it is (gap {before['gap']} -> {w and w['gap']})")
finish()
