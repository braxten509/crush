"""The right-click menu on a reply still being written (owner, 2026-10-20:
"This should say wait for reply to finish but it doesn't when the whole line
is selected. Also, remove the orange selector there"). In the real window on
the hidden screen, with a chat whose last reply has no finish yet:
- a triple click selects the reply's whole line, and the menu's disabled
  "Branch from here" says "Wait for the reply to finish";
- an item that has keyboard focus draws no ring round it.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/branch-live.json branch_hint.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, key, probe_eval, shot, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
TEXT = "The checks are running now"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
spot = None
end = time.time() + 15
while time.time() < end and not spot:
    spot = run(f"""
      const el = [...document.querySelectorAll('main.conversation p')].find((p) => p.textContent.includes({TEXT!r}));
      if (!el) return null;
      el.scrollIntoView({{ block: 'center' }});
      const r = el.getBoundingClientRect();
      return {{ x: r.x + 40, y: r.y + r.height / 2 }};
    """)
    time.sleep(0.3)
check(bool(spot), f"the unfinished reply shows ({spot})")
time.sleep(0.5)
spot = run(f"const r = [...document.querySelectorAll('main.conversation p')].find((p) => p.textContent.includes({TEXT!r})).getBoundingClientRect(); return {{ x: r.x + 40, y: r.y + r.height / 2 }};")
ox, oy = H.window_geometry()[:2]
xdo("mousemove", round(ox + spot["x"]), round(oy + spot["y"]))
xdo("click", "--repeat", "3", "--delay", "80", "1")
time.sleep(0.3)
picked = run("return String(getSelection()).trim();")
xdo("click", "3")
time.sleep(0.6)
menu = run("""
  const entries = [...document.querySelectorAll('[role="menu"] *')].map((el) => el.textContent.trim()).filter(Boolean);
  return entries;
""") or []
shot(out_dir, "menu")
check(picked.startswith(TEXT), f"the triple click selects the whole line ({picked!r})")
check(any(e == "Wait for the reply to finish" for e in menu), f"Branch from here says to wait for the reply ({menu[:6]})")
key("Escape")
time.sleep(0.3)
key("Up")
time.sleep(0.3)
ring = run("""
  const el = document.activeElement?.closest?.('[role="article"]');
  return el ? { outline: getComputedStyle(el).outlineStyle, visible: el.matches(':focus-visible') } : { none: document.activeElement?.tagName };
""") or {}
check(ring.get("outline") == "none", f"a focused item draws no ring ({ring})")
finish()
