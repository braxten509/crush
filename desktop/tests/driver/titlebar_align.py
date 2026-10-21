"""The title bar's parts for the align skill (owner, 2026-10-20: "Use the align
tool and fix these"). In the real window on the hidden screen, at Compact
(8/12), 9/12 and picture size (12/12), each part's box and a capture of the screen
are saved; outside the sandbox (the skill lives in the owner's home),
tests/look/titlebar_manifest.py turns them into the skill's screenshot
manifests and runs its checker.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json titlebar_align.py
then:  python3 tests/look/titlebar_manifest.py tests/out/titlebar_align
"""
import json, os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, probe_eval, read_probe, shot, wait_for, window_geometry

probe_path, out_dir = sys.argv[1], sys.argv[2]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


PARTS = """
  const box = (el) => { if (!el) return null; const r = el.getBoundingClientRect(); return [r.x, r.y, r.width, r.height]; };
  const q = (s) => document.querySelector(s);
  const bar = q('[data-probe="title-bar"]');
  const icon = (p) => box(q(`[data-probe="${p}"] svg`));
  // A text part: its box, its text and its first letter's box.
  const text = (s) => {
    const el = q(s);
    if (!el) return null;
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
    let node = walker.nextNode();
    while (node && !node.textContent.trim()) node = walker.nextNode();
    const at = node ? node.textContent.search(/\S/) : -1;
    let first = null;
    if (node && at >= 0) { const r = document.createRange(); r.setStart(node, at); r.setEnd(node, at + 1); const b = r.getBoundingClientRect(); first = [b.x, b.y, b.width, b.height]; }
    const all = document.createRange(); all.selectNodeContents(el); const w = all.getBoundingClientRect();
    const m = document.createElement('span'); m.style.cssText = 'display:inline-block;width:0;height:0';
    el.append(m); const baseline = m.getBoundingClientRect().top; m.remove();
    return { rect: box(el), baseline, words: [w.x, w.y, w.width, w.height], text: getComputedStyle(el).textTransform === 'uppercase' ? el.textContent.toUpperCase() : el.textContent, first };
  };
  return {
    bar: box(bar),
    app_icon: box(q('.title-bar .app-icon')),
    chat: text('[data-probe="title-chat"]'), project: text('[data-probe="title-project"]'), branch: text('[data-probe="title-branch"]'),
    panel: icon('title-panel'), pin: icon('title-pin'), minimize: icon('title-minimize'),
    maximize: icon('title-maximize'), close: icon('title-close'),
    buttons: [...bar.querySelectorAll('.buttons button')].map(box),
  };
"""

wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
time.sleep(1.5)
ox, oy = window_geometry()[:2]
saved = []
for step, name in [(0, "compact"), (1, "nine"), (3, "picture")]:
    for _ in range(step):
        key("ctrl+equal")
    time.sleep(1)
    parts = run(PARTS) or {}
    zoom = (read_probe(probe_path) or {}).get("zoom")
    if check(bool(parts.get("bar")) and not parts.get("error"), f"{name}: the title bar's parts ({parts.get('error')})"):
        image = shot(out_dir, f"titlebar-{name}")
        json.dump({"zoom": zoom, "origin": [ox, oy], "image": os.path.basename(image), "parts": parts},
                  open(os.path.join(out_dir, f"titlebar-{name}.json"), "w"), indent=1)
        saved.append(name)
    if parts.get("chat") and parts.get("project") and parts.get("branch"):
        c, f, b = parts["chat"]["words"], parts["project"]["words"], parts["branch"]["words"]
        gaps = [round(f[0] - c[0] - c[2], 2), round(b[0] - f[0] - f[2], 2)]
        check(abs(gaps[0] - gaps[1]) <= 0.5, f"{name}: the title, folder and branch are evenly spaced word to word, even with a short folder name ({gaps})")
        lines = [parts[k]["baseline"] for k in ("chat", "project", "branch")]
        check(max(lines) - min(lines) <= 0.5, f"{name}: the title, folder and branch share one baseline ({lines})")
check(saved == ["compact", "nine", "picture"], f"every zoom is saved ({saved})")
finish()
