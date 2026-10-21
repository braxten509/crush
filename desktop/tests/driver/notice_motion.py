"""The notice line comes and goes with motion, and an update's sync mark turns
in place (owner, 2026-10-20: "make this bar appear/disappear with an
animation and the sync icon also animate while it's working", "if you rotate
it wrong it moves the entire SVG a few pixels off"). In the real window on
the hidden screen, sampled every frame:
- an update notice unrolls (its clip changes over several frames);
- its sync mark turns (several angles) while its middle stays put (≤ 0.5 px);
- a done notice's check mark stands still;
- the line rolls away when the notice ends.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json notice_motion.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


# Every frame for ms: the line's clip, the mark's turn and its middle.
SAMPLE = """
  window.__frames = [];
  const t0 = performance.now();
  const step = () => {
    const line = document.querySelector('[data-notice-kind]');
    const svg = line?.querySelector('.mark svg');
    const r = svg?.getBoundingClientRect();
    window.__frames.push(line ? { clip: getComputedStyle(line).clipPath, turn: getComputedStyle(svg).transform,
      mid: [Math.round((r.left + r.width / 2) * 100) / 100, Math.round((r.top + r.height / 2) * 100) / 100] } : null);
    if (performance.now() - t0 < MS) requestAnimationFrame(step);
  };
  requestAnimationFrame(step);
  return true;
"""


def frames(ms, then=""):
    run(SAMPLE.replace("MS", str(ms)) + "")
    if then:
        run(then)
    time.sleep(ms / 1000 + 0.4)
    return run("return window.__frames;") or []


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
# Start with no notice up, so the next one comes in rather than replacing it
# (the fixture's chat opens with a lasting notice: its × clears it).
end = time.time() + 15
while time.time() < end and run("document.querySelector('[data-notice-dismiss]')?.click(); return !!document.querySelector('[data-notice-kind]');"):
    time.sleep(0.3)
time.sleep(0.5)

# An update notice: it unrolls, and its mark turns in place.
got = frames(1500, "window.__probe.notify('update', 'Updating Claude Code…', 2600); return true;")
shown = [f for f in got if f]
clips = {f["clip"] for f in shown}
check(len(clips) >= 3, f"the update notice unrolls ({len(clips)} clips over {len(shown)} frames)")
settled = shown[len(shown) // 3:]
turns = {f["turn"] for f in settled}
mids = [f["mid"] for f in settled]
drift = max(max(abs(a[0] - b[0]), abs(a[1] - b[1])) for a in mids for b in mids[:1]) if mids else 99
check(len(turns) >= 5, f"its sync mark turns ({len(turns)} angles)")
check(drift <= 0.5, f"and its middle stays put while it turns ({drift} px)")

# It ends: the line rolls away.
got = frames(900)
gone = [f for f in got if f]
check(len({f["clip"] for f in gone}) >= 3 and got and got[-1] is None, f"the line rolls away when the notice ends ({len(gone)} frames still drawn)")

# A done notice's mark stands still.
got = frames(900, "window.__probe.notify('success', 'Updated Claude Code 2.1.306', 3000); return true;")
done = [f for f in got if f][-10:]
check(done and all(f["turn"] == "none" for f in done), f"a done notice's mark stands still ({done[-1]['turn'] if done else None})")
finish()
