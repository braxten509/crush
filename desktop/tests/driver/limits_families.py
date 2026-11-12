"""A model family's own usage limit (owner, 2026-11-12: "add fable limits
for claude code on the top right of crush and also argon limits for agy"),
in the real window on the hidden screen with a Fable chat open: the meters
at the panel's top gain Fable's own weekly meter beside the account's own,
and the list of every sign-in shows Fable as a family line under Claude and
Argon as one under Agy, each with its own numbers.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/limits-families.json limits_families.py
"""
import os, re, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
ready = lambda p: p.get("ready") and p.get("fonts") == "loaded"



def numbers(texts):
    """The whole numbers in texts, skipping the ones with none (the "–")."""
    return [int(m.group()) for t in texts if (m := re.search(r"\d+", t or ""))]


wait_for(probe_path, ready, 40)

TEXT = "const text = (el) => el?.textContent.replace(/\\s+/g, ' ').trim() ?? '';"
METERS = """(async () => {
  """ + TEXT + """
  const cells = [...document.querySelectorAll('.panel-column .usage .cell')];
  window.__probeEval = { meters: cells.map((c) => [c.dataset.meter ?? '', text(c.querySelector('.value > span'))]) };
})()"""
meters = {}
end = time.time() + 10
while time.time() < end:
    meters = probe_eval(probe_path, METERS) or {}
    meters = {} if meters.get("error") else meters
    if len(numbers([v for _, v in (meters.get("meters") or [])[1:]])) >= 3:
        break
    time.sleep(0.1)
shot(out_dir, "meters")
shown = (meters.get("meters") or [])[1:]
check(numbers([v for _, v in shown]) == [74, 42, 57] and len(shown) == 3 and "Fable" in shown[2][0],
      f"a Fable chat's meters end with Fable's own weekly limit ({meters.get('meters')})")

# The list of every sign-in is a saved switch, read as the page starts.
probe_eval(probe_path, """(async () => {
  localStorage.setItem("crush-desktop.account-meters", "on");
  window.__probeEval = true;
  setTimeout(() => location.reload(), 50);
})()""")
end = time.time() + 40
while time.time() < end and probe_eval(probe_path, "window.__probeEval = !!document.querySelector('.panel-column .accounts');") is not True:
    time.sleep(0.1)
wait_for(probe_path, ready, 40)

seen = probe_eval(probe_path, """(async () => {
  """ + TEXT + """
  // The readings arrive after the list's first draw: wait for its family lines.
  for (let i = 0; i < 50 && document.querySelectorAll('.panel-column .accounts .row.family').length < 2; i++) await new Promise((r) => setTimeout(r, 100));
  window.__probeEval = {
    rows: [...document.querySelectorAll('.panel-column .accounts .row')].map((row) => ({
      name: text(row.querySelector('.name .text')),
      family: row.classList.contains('family'),
      parts: [...row.querySelectorAll('.part')].map((p) => text(p.querySelector('.pct > span:last-child') ?? p.querySelector('.pct'))),
    })),
  };
})()""") or {}
shot(out_dir, "list")

rows = ({} if seen.get("error") else seen).get("rows") or []
check([[r["name"], r["family"], numbers(r["parts"])] for r in rows] == [
    ["Claude", False, [74, 42]],
    ["Fable", True, [57]],
    ["Agy", False, [100, 90]],
    ["Argon", True, [60]],
], f"the list shows Fable under Claude and Argon under Agy ({rows})")
finish()
