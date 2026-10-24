"""The right panel's usage meters with four of them (owner, 2026-10-24: "can
you put two on each row instead of 3 on top and 1 on the bottom?"), in the
real window on the hidden screen with a chat open: CONTEXT, 5-HOUR, WEEK and
MONTHLY make two rows of two, in that order, each row split by one rule and
no meter's words cut.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/meters-four.json meters_four.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]

wait_for(probe_path, lambda p: p.get("ready") and p.get("fonts") == "loaded", 40)
time.sleep(1.2)

meters = probe_eval(probe_path, """(async () => {
  const cells = [...document.querySelectorAll('.panel-column .usage .cell')];
  const box = (el) => { const r = el.getBoundingClientRect(); return { top: Math.round(r.top), left: Math.round(r.left) }; };
  const rows = [];
  for (const cell of cells) {
    const at = box(cell).top;
    const row = rows.find((r) => Math.abs(r.top - at) <= 1);
    const label = cell.querySelector('.label')?.textContent.trim();
    if (row) row.cells.push(label);
    else rows.push({ top: at, cells: [label] });
  }
  const cut = cells.flatMap((c) => [...c.querySelectorAll('.label, .resets, .value > span:not(.visually-hidden)')]
    .filter((e) => e.scrollWidth > e.clientWidth + 1).map((e) => e.textContent.trim()));
  const rules = [...document.querySelectorAll('.panel-column .usage .separator')].map((s) => {
    const r = s.getBoundingClientRect();
    return { top: Math.round(r.top), height: Math.round(r.height) };
  });
  window.__probeEval = {
    count: cells.length,
    rows: rows.map((r) => r.cells),
    firstLefts: cells.map((c) => box(c).left),
    rules,
    cut,
  };
})()""") or {}

meters = {} if meters.get("error") else meters
check(meters.get("count") == 4, f"the panel shows four meters ({meters})")
check(meters.get("rows") == [["CONTEXT", "5-HOUR"], ["WEEK", "MONTHLY"]],
      f"two rows of two: CONTEXT and 5-HOUR over WEEK and MONTHLY ({meters.get('rows')})")
check(len(meters.get("rules") or []) == 2, f"each row has its one rule between the two meters ({meters.get('rules')})")
check(not meters.get("cut"), f"no meter's words are cut ({meters.get('cut')})")
finish()
