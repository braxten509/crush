"""Yes/no forms (owner, 2026-10-19: "the Yes and No are not centered in
their boxes"; "you don't need an orange AND gray line"). In the real window
on the hidden screen, opens a local yes/no form (like the CLI update offer)
and checks that each label sits in the middle of its button and that the
form's edge lies on the slot's gray edge, so a focused form shows one line.
Saves tests/out/<run>/yes_no.png.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json yes_no_look.py
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def app_window(timeout=20):
    """The largest visible window of the app (as no_flash.py)."""
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
        best = None
        for wid in r.stdout.split():
            g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
            d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
            if "WIDTH" in d and (best is None or int(d["WIDTH"]) * int(d["HEIGHT"]) > best[1]):
                best = (wid, int(d["WIDTH"]) * int(d["HEIGHT"]))
        if best and best[1] > 100 * 100:
            return best[0]
        time.sleep(0.3)
    return None


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 30)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(1.5)
run("window.__probe.askYesNo({ label: 'Update', question: 'Install updates for AGY?', description: 'AGY 1.4.2 to 1.4.3.' }); return true;")
time.sleep(0.8)
got = run(("""
  const form = document.querySelector('[data-probe="slot"] .form');
  const slot = document.querySelector('[data-probe="slot"]');
  if (!form || !slot) return { missing: true };
  // "break" as the driver argument puts the old look back, to see this check fail.
  if (%s) { form.style.margin = '0'; form.querySelectorAll('.pill').forEach((b) => (b.style.textAlign = 'left')); }
  form.focus();
  await new Promise((done) => setTimeout(done, 300));
  const box = (r) => ({ left: r.left, right: r.right, top: r.top, bottom: r.bottom });
  const pills = [...form.querySelectorAll('.pill')].map((b) => {
    const range = document.createRange();
    range.selectNodeContents(b);
    const t = range.getBoundingClientRect(), r = b.getBoundingClientRect();
    return { label: b.textContent.trim(), off: (t.left + t.right) / 2 - (r.left + r.right) / 2 };
  });
  const s = getComputedStyle(form);
  return { pills, form: box(form.getBoundingClientRect()), slot: box(slot.getBoundingClientRect()), focused: form.classList.contains('focused'), edge: s.borderTopColor };
""").replace("%s", "true" if "break" in sys.argv[3:] else "false"))
check(got is not None and not got.get("missing"), f"the yes/no form opens in the slot ({got})")
if got and not got.get("missing"):
    for pill in got["pills"]:
        check(abs(pill["off"]) <= 1, f"{pill['label']} sits in the middle of its button (off by {pill['off']:.1f} px)")
    check(all(abs(got["form"][k] - got["slot"][k]) <= 0.5 for k in ("left", "right", "top", "bottom")),
          f"the form's edge lies on the slot's edge (form {got['form']}, slot {got['slot']})")
    check(got["focused"], f"the form is focused, so its edge is vermilion ({got['edge']})")
subprocess.run(["import", "-window", wid, os.path.join(out_dir, "yes_no.png")])
finish()
