"""The panel's update button (owner, 2026-10-20: bottom right, "just one
[button] that changes state", "automatically installs new versions ... and
restarts it"). In the real window on the hidden screen, with a fixture whose
updater has a newer build (app_update):
- the foot sits at the bottom of the right panel; the app's quiet check at
  connect lights the one button as Update, with "old → new" beside it;
- pressing it: Updating…, disabled, its sync mark turning in place;
- when the run ends: Restarting…, and the open chats are held for the
  restart (open-chats.restart); a fixture run is not really restarted.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f03" \\
       tests/check.sh --fixture tests/fixtures/app-update.json update_button.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
HOLD = os.path.splitext(H.OPEN_CHATS)[0] + ".restart"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


FOOT = """
  const foot = document.querySelector('[data-probe="update-foot"]');
  if (!foot) return null;
  const panel = document.querySelector('[data-probe="panel"]').getBoundingClientRect();
  const r = foot.getBoundingClientRect();
  const b = foot.querySelector('[data-update-button]');
  return { phase: foot.dataset.phase, text: foot.querySelector('.version').textContent.trim(), label: b.textContent.trim(),
           disabled: b.disabled, ready: b.classList.contains('ready'), bottom: panel.bottom - r.bottom,
           right: Math.abs(panel.right - r.right), inPanel: r.left >= panel.left - 1 };
"""


def foot(want, timeout=10):
    end = time.time() + timeout
    got = None
    while time.time() < end:
        got = run(FOOT) or {}
        if want(got):
            return got
        time.sleep(0.15)
    return got


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
if os.path.exists(HOLD):
    os.remove(HOLD)
ready = foot(lambda f: f.get("phase") == "ready")
check(ready.get("phase") == "ready" and ready.get("label") == "Update" and ready.get("ready"), f"the quiet check lights the one button as Update ({ready})")
check(ready.get("text") == "aaaaaaa → bbbbbbb", f"with the old and new builds beside it ({ready.get('text')!r})")
check(ready.get("inPanel") and abs(ready.get("bottom", 99)) <= 1 and ready.get("right", 99) <= 20, f"at the bottom of the right panel ({ready})")
shot(out_dir, "ready")

run("document.querySelector('[data-update-button]').click(); return true;")
busy = foot(lambda f: f.get("phase") == "updating", 3)
check(busy.get("label") == "Updating…" and busy.get("disabled"), f"pressing it updates, the button waiting ({busy})")
turns = run("""
  const out = [];
  for (let i = 0; i < 20; i++) {
    await new Promise((r) => requestAnimationFrame(r));
    const svg = document.querySelector('[data-update-button] .mark svg');
    const r = svg.getBoundingClientRect();
    out.push([getComputedStyle(svg).transform, r.left + r.width / 2, r.top + r.height / 2]);
  }
  return out;
""") or []
angles = {t[0] for t in turns}
drift = max((max(abs(t[1] - turns[0][1]), abs(t[2] - turns[0][2])) for t in turns), default=99)
check(len(angles) >= 5 and drift <= 0.5, f"its sync mark turns in place ({len(angles)} angles, {round(drift, 2)} px)")

done = foot(lambda f: f.get("phase") == "restarting", 6)
check(done.get("label") == "Restarting…", f"then it restarts ({done})")
time.sleep(1.2)
check(os.path.exists(HOLD), f"with the open chats held for the restart ({HOLD})")
finish()
