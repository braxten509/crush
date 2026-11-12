"""A tool's sign-ins read at the same moment (owner, 2026-11-12: the main
Claude sign-in "just says checking forever" after a restart). The engine
sends one limits event per sign-in; three arriving in one batch were folded
into the last one, so the shown chat's own sign-in never got its numbers. In
the real window on the hidden screen: the meters start on "checking…", the
recording sends the three readings together, and the chat's own sign-in's
numbers show.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/limits-together.json limits_together.py
"""
import os, re, sys

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]

wait_for(probe_path, lambda p: p.get("ready") and p.get("fonts") == "loaded", 40)
seen = probe_eval(probe_path, """(async () => {
  const text = (el) => el?.textContent.replace(/\\s+/g, ' ').trim() ?? '';
  const meters = () => [...document.querySelectorAll('.panel-column .usage .cell')].map((c) => [text(c.querySelector('.label')), text(c.querySelector('.value > span'))]);
  // The readings arrive 1.5 s after the window joins; the page gives a
  // script 5 s to report.
  const waiting = () => meters().length < 3 || meters().some((m) => m[1] === 'checking…');
  for (let i = 0; i < 40 && waiting(); i++) await new Promise((r) => setTimeout(r, 100));
  window.__probeEval = { meters: meters() };
})()""") or {}
shot(out_dir, "limits-together")
meters = ({} if seen.get("error") else seen).get("meters") or []
numbers = [int(m.group()) if (m := re.search(r"\d+", value)) else None for _, value in meters[1:]]
check(numbers == [74, 42] and not any("checking" in value for _, value in meters),
      f"the chat's own sign-in shows its numbers though three sign-ins were read together ({meters})")
finish()
