"""The answers pop-up grows to fit (owner, 2026-10-26: "make it so it can
expand so it doesn't need the scrollbar"). Its page had its own scroll box,
which rounding left a dot shorter than the text: a bar showed, narrowed the
text and made it longer still. In the real window on the hidden screen, with a
chat whose one yes/no question has a long description:
- a click on the answers line opens the pop-up with the description and "Yes";
- nothing in the pop-up scrolls: every box is as tall as what it holds.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/answers-fit.json answers_fit.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
spot = None
end = time.time() + 15
while time.time() < end and not spot:
    spot = run("""
      const el = document.querySelector('main.conversation [data-answers-open]');
      if (!el) return '';
      el.scrollIntoView({ block: 'center' });
      const r = el.getBoundingClientRect();
      return { x: r.x + 60, y: r.y + r.height / 2 };
    """)
    time.sleep(0.3)
check(bool(spot), "the answers line is shown")
time.sleep(0.4)
if spot:
    H.click(spot["x"], spot["y"])
sheet = ""
end = time.time() + 10
while time.time() < end and not sheet:
    sheet = run("return document.querySelector('[data-probe=\"answers-sheet\"]')?.innerText ?? '';") or ""
    time.sleep(0.2)
time.sleep(0.5)
shot(out_dir, "answers")
check("No commit, push, or phone access." in sheet and "Yes" in sheet, "the pop-up shows the description and the pick")
scrolls = run("""
  const frame = document.querySelector('[data-sheet="answers"]');
  if (!frame) return ['no pop-up'];
  return [frame, ...frame.querySelectorAll('*')]
    .filter((el) => el.scrollHeight > el.clientHeight && /auto|scroll/.test(getComputedStyle(el).overflowY))
    .map((el) => `${el.className}: ${el.scrollHeight} > ${el.clientHeight}`);
""")
check(scrolls == [], f"nothing in the pop-up scrolls ({scrolls})")
finish()
