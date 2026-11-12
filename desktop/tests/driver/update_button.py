"""The panel's update button (owner, 2026-10-20: bottom right, "just one
[button] that changes state", "automatically installs new versions ... and
restarts it"). In the real window on the hidden screen, with a fixture whose
updater has a newer build (app_update):
- the app's quiet check at connect sets the one button ready, with "old →
  new" beside it;
- pressing it asks first whether the engines restart too (owner,
  2026-11-12), with "Keep engines running" selected; Esc cancels and
  updates nothing;
- answered: updating, disabled;
- when the run ends: restarting, and the open chats are held for the
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
  const b = foot.querySelector('[data-update-button]');
  return { phase: foot.dataset.phase, text: foot.querySelector('.version').textContent.trim(), disabled: b.disabled };
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
check(ready.get("phase") == "ready", f"the quiet check sets the one button ready ({ready})")
check(ready.get("text") == "aaaaaaa → bbbbbbb", f"with the old and new builds beside it ({ready.get('text')!r})")
shot(out_dir, "ready")

SHEET = """
  const sheet = document.querySelector('[data-sheet="update-engines"]');
  if (!sheet) return { gone: true };
  return { open: true, buttons: [...sheet.querySelectorAll('[data-button]')].map((b) => b.dataset.button),
           selected: sheet.querySelector('[data-button].selected')?.dataset.button ?? '' };
"""


def sheet(want, timeout=5):
    end = time.time() + timeout
    got = None
    while time.time() < end:
        got = run(SHEET)
        if want(got):
            return got
        time.sleep(0.15)
    return got


run("document.querySelector('[data-update-button]').click(); return true;")
asked = sheet(lambda s: bool(s) and s.get("open")) or {}
check(sorted(asked.get("buttons") or []) == ["cancel", "keep", "restart"], f"pressing it asks about the engines ({asked.get('buttons')})")
check(asked.get("selected") == "keep", f"Keep engines running is selected ({asked.get('selected')!r})")
shot(out_dir, "asked")
H.key("Escape")
gone = sheet(lambda s: bool(s) and s.get("gone")) or {}
still = foot(lambda f: f.get("phase") == "ready", 2)
check(gone.get("gone") is True and still.get("phase") == "ready", f"Esc cancels: nothing updates ({still.get('phase')}, sheet {gone})")

run("document.querySelector('[data-update-button]').click(); return true;")
sheet(lambda s: bool(s) and s.get("open"))
H.key("Return")
busy = foot(lambda f: f.get("phase") == "updating", 3)
check(busy.get("phase") == "updating" and busy.get("disabled"), f"pressing it updates, the button waiting ({busy})")
done = foot(lambda f: f.get("phase") == "restarting", 6)
check(done.get("phase") == "restarting", f"then it restarts ({done})")
end = time.time() + 5
while time.time() < end and not os.path.exists(HOLD):
    time.sleep(0.1)
check(os.path.exists(HOLD), f"with the open chats held for the restart ({HOLD})")
finish()
