"""Coming back from Settings, the right panel comes back as it was: the usage
meters never show under the typing box on the way (owner, 2026-10-23: they
flashed there for a moment before the panel returned). In the real window,
Settings is opened and closed while the page notes every frame for a second
whether the strip under the typing box is on the page.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json settings_return.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
time.sleep(1.5)

check(run("return Boolean(document.querySelector('.panel-column'));") is True, "the right panel shows before Settings")
check(run("return !document.querySelector('[data-meter-strip]');") is True, "no strip under the typing box with the panel shown")

run("document.querySelector('nav.chats button.settings').click(); return true;")
time.sleep(1.0)
check(run("return Boolean(document.querySelector('.main-view'));") is True, "Settings opened")

# Every frame for a second after Back: was the strip ever on the page?
run("""
  window.__strip = { frames: 0, seen: 0 };
  const t0 = performance.now();
  const frame = () => {
    window.__strip.frames += 1;
    if (document.querySelector('[data-meter-strip]')) window.__strip.seen += 1;
    if (performance.now() - t0 < 1000) requestAnimationFrame(frame);
  };
  requestAnimationFrame(frame);
  document.querySelector('.settings-nav button.back').click();
  return true;
""")
time.sleep(1.4)
rec = run("return window.__strip;") or {}
check(rec.get("frames", 0) > 10 and rec.get("seen") == 0, f"the strip never shows under the typing box after Settings ({rec})")
check(run("return Boolean(document.querySelector('.panel-column'));") is True, "the right panel is back after Settings")

finish()
