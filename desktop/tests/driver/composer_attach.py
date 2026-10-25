"""Adding an attachment leaves the typing box where it was (owner,
2026-10-25: "the height somehow changes even though the row with Complete
should have space"). In the real window on the hidden screen, with a chat
open, a long text is pasted into the field (it becomes an attachment): the attachment shows beside
Complete, and the field's top and the composer's height stay put.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json composer_attach.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


PLACE = """
  const field = document.querySelector('[data-probe="composer"] form.row');
  const box = document.querySelector('[data-probe="composer"]');
  return field && box && [field.getBoundingClientRect().top, box.getBoundingClientRect().height, Boolean(document.querySelector('[data-probe="composer"] .complete'))];
"""

wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
time.sleep(1.5)
before = run(PLACE)
check(bool(before) and before[2], f"a chat's typing box with Complete ({before})")
run("""
  const data = new DataTransfer();
  data.setData('text/plain', Array.from({ length: 40 }, (_, i) => `pasted line ${i}`).join('\\n'));
  const input = document.querySelector('[data-probe="composer"] .field textarea');
  input.focus();
  input.dispatchEvent(new ClipboardEvent('paste', { clipboardData: data, bubbles: true, cancelable: true }));
  return true;
""")
time.sleep(1.0)
chips = run("return document.querySelectorAll('[data-probe=\"composer\"] .chips-side .chips > *').length;")
check((chips or 0) > 0, f"the pasted text shows as an attachment ({chips})")
after = run(PLACE)
check(bool(after) and abs(after[0] - before[0]) <= 0.5 and abs(after[1] - before[1]) <= 0.5,
      f"the typing box stays put (field top {before and before[0]} -> {after and after[0]}, composer height {before and before[1]} -> {after and after[1]})")
finish()
