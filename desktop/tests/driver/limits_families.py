"""A model family's own usage limit (owner, 2026-11-12: "add fable limits
for claude code on the top right of crush and also argon limits for agy"),
in the real window on the hidden screen with a Fable chat open: the meters
at the panel's top gain WEEKLY FABLE beside the account's own, and the list
of every sign-in shows Fable on a line under Claude and Gemini 4 Argon on a
line under Agy, each set in under its account.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/limits-families.json limits_families.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
ready = lambda p: p.get("ready") and p.get("fonts") == "loaded"

wait_for(probe_path, ready, 40)
time.sleep(1.2)

TEXT = "const text = (el) => el?.textContent.replace(/\\s+/g, ' ').trim() ?? '';"
meters = probe_eval(probe_path, """(async () => {
  """ + TEXT + """
  const cells = [...document.querySelectorAll('.panel-column .usage .cell')];
  const cut = cells.flatMap((c) => [...c.querySelectorAll('.label, .value > span:not(.visually-hidden)')]
    .filter((e) => e.scrollWidth > e.clientWidth + 1).map(text));
  window.__probeEval = { meters: cells.map((c) => [text(c.querySelector('.label')), text(c.querySelector('.value > span'))]), cut };
})()""") or {}
shot(out_dir, "meters")
meters = {} if meters.get("error") else meters
check((meters.get("meters") or [])[1:] == [["5-HOUR", "74% left"], ["WEEK", "42% left"], ["WEEKLY FABLE", "57% left"]],
      f"a Fable chat's meters end with Fable's own weekly limit ({meters.get('meters')})")
check(not meters.get("cut"), f"no meter's words are cut ({meters.get('cut')})")

# The list of every sign-in is a saved switch, read as the page starts.
probe_eval(probe_path, """(async () => {
  localStorage.setItem("crush-desktop.account-meters", "on");
  window.__probeEval = true;
  setTimeout(() => location.reload(), 50);
})()""")
time.sleep(0.5)
wait_for(probe_path, ready, 40)
time.sleep(0.5)

seen = probe_eval(probe_path, """(async () => {
  """ + TEXT + """
  // The readings arrive after the list's first draw: wait for its family lines.
  for (let i = 0; i < 50 && document.querySelectorAll('.panel-column .accounts .row.family').length < 2; i++) await new Promise((r) => setTimeout(r, 100));
  window.__probeEval = {
    rows: [...document.querySelectorAll('.panel-column .accounts .row')].map((row) => ({
      name: text(row.querySelector('.name .text')),
      family: row.classList.contains('family'),
      left: Math.round(row.querySelector('.name .text').getBoundingClientRect().left),
      parts: [...row.querySelectorAll('.part')].map((p) => text(p.querySelector('.pct > span:last-child') ?? p.querySelector('.pct'))),
    })),
  };
})()""") or {}
shot(out_dir, "list")

rows = ({} if seen.get("error") else seen).get("rows") or []
check([[r["name"], r["family"], r["parts"]] for r in rows] == [
    ["Claude", False, ["74%", "42%", "–"]],
    ["Fable", True, ["–", "57%", "–"]],
    ["Agy", False, ["100%", "90%", "–"]],
    ["Gemini 4 Argon", True, ["–", "60%", "–"]],
], f"the list shows Fable under Claude and Gemini 4 Argon under Agy ({rows})")
own = [r["left"] for r in rows if not r["family"]]
check(len(set(own)) == 1 and all(r["left"] > own[0] for r in rows if r["family"]),
      f"a family's line is set in under its account ({[(r['name'], r['left']) for r in rows]})")
finish()
