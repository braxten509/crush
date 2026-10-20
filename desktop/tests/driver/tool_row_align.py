"""Tool row alignment (owner, 2026-10-20: "These aren't all aligned", a
screenshot of "▸ Ran 2 tools • ··· Thinking"). In the real window on the
hidden screen, a busy chat whose last item is a finished tool group, so the
row carries the live "Thinking" status. Every word on the row shares one
baseline, and the status's three dots sit on the same middle as the dot
between "Ran 4 tools" and them, at every zoom from Compact (8/12) to
picture size (12/12): the status used to sit by its middle, so its baseline
matched only at some zooms.

Usage: tests/check.sh --no-app tool_row_align.py
(the driver makes its fixture in tests/out/tool_row_align/)
"""
import json, os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from apps import App
from harness import check, finish, key, probe_eval, read_probe, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
CHAT = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
ROOT = os.path.join(os.path.dirname(__file__), "..", "..")

# model-menu's chat, busy, ending on its tool group (its last reply left out).
fixture = json.load(open(os.path.join(ROOT, "tests", "fixtures", "model-menu.json")))
get = fixture["get"]
get[f"sessions/{CHAT}/messages"] = get[f"sessions/{CHAT}/messages"][:3]
get[f"sessions/{CHAT}"]["is_busy"] = True
for s in get["sessions"]:
    if s["id"] == CHAT:
        s["is_busy"] = True
fixture_path = os.path.join(out_dir, "fixture.json")
json.dump(fixture, open(fixture_path, "w"))


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


# A zero-size inline block at the end of a word sits on that word's baseline.
MEASURE = """
  const row = [...document.querySelectorAll('button.tool-row')].find((r) => r.querySelector('.live'));
  if (!row) return null;
  const base = (el) => { const m = document.createElement('span'); m.style.cssText = 'display:inline-block;width:0;height:0';
    el.append(m); const y = m.getBoundingClientRect().top; m.remove(); return y; };
  const mid = (el) => { const r = el.getBoundingClientRect(); return r.top + r.height / 2; };
  const words = row.querySelector('.live .words');
  return { text: row.innerText.trim(), count: base(row.querySelector('.count')), words: base(words),
           dot: mid(row.querySelector('.line > .dot')), mark: mid(row.querySelector('.live .mark')) };
"""

app = App(args=("--fixture", fixture_path, "-s", CHAT))
wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
got = None
end = time.time() + 20
while time.time() < end and not got:
    got = run(MEASURE)
    time.sleep(0.3)
shot(out_dir, "row")
if check(bool(got) and not got.get("error"), f"the tool row shows a live status ({got})"):
    check("Thinking" in got["text"], f"the status says Thinking ({got['text']!r})")
    for step in range(5):
        if step:
            key("ctrl+equal")
            time.sleep(0.8)
            got = run(MEASURE) or {}
        z = f"zoom {(read_probe(probe_path) or {}).get('zoom')}"
        check(abs(got["count"] - got["words"]) <= 0.5, f"{z}: \"Thinking\" sits on \"Ran 4 tools\"'s baseline ({got['words']} vs {got['count']})")
        check(abs(got["dot"] - got["mark"]) <= 0.75, f"{z}: the status's dots share the separator dot's middle ({got['mark']} vs {got['dot']})")
    shot(out_dir, "row-picture-size")
app.stop()
finish()
