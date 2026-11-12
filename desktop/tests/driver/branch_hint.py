"""The right-click menu on a reply still being written (owner, 2026-10-20:
"This should say wait for reply to finish but it doesn't when the whole line
is selected. Also, remove the orange selector there"). In the real window on
the hidden screen, with a chat whose last reply has no finish yet:
a triple click selects the reply's whole line, and the menu's "Branch from
here" is disabled with the unfinished-reply hint (HINTS.unfinished in
src/lib/branch.js).

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/branch-live.json branch_hint.py
"""
import os, re, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, key, probe_eval, shot, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
TEXT = "The checks are running now"
with open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "..", "src", "lib", "branch.js")) as f:
    UNFINISHED = re.search(r'unfinished:\s*"([^"]*)"', f.read()).group(1)


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
spot = run(f"const r = [...document.querySelectorAll('main.conversation p')].find((p) => p.textContent.includes({TEXT!r})).getBoundingClientRect(); return {{ x: r.x + 40, y: r.y + r.height / 2 }};")
ox, oy = H.window_geometry()[:2]
xdo("mousemove", round(ox + spot["x"]), round(oy + spot["y"]))
xdo("click", "--repeat", "3", "--delay", "80", "1")
time.sleep(0.3)
picked = run("return String(getSelection()).trim();")
xdo("click", "3")
item = None
end = time.time() + 5
while time.time() < end and not item:
    item = run("""
      const el = document.querySelector('[role="menu"] [role="menuitem"][data-menu-id="branch"]');
      return el && { disabled: el.disabled || el.getAttribute('aria-disabled') === 'true', hint: el.querySelector('.hint')?.textContent.trim() ?? '' };
    """)
    if not item:
        time.sleep(0.05)
shot(out_dir, "menu")
check(picked.startswith(TEXT), f"the triple click selects the whole line ({picked!r})")
check(bool(item) and item["disabled"] and item["hint"] == UNFINISHED, f"Branch from here is disabled until the reply finishes ({item})")
key("Escape")
finish()
