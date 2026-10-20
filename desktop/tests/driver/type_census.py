"""Type census (owner, 2026-10-20: "keeping the number of fonts and the size
variations of fonts limited"). In the real window on the hidden screen, every
visible piece of text on the main screens and sheets is grouped by the font it
actually renders in (face, size in screen pixels, weight), with where each one
appears, into tests/out/type_census/census.json. A measurement, no pass/fail.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json type_census.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
SHEETS = ["commands", "sessions", "theme", "skills", "about", "usage", "models", "notifications",
          "memory_limit", "logs", "background", "sub-agents", "folder_picker", "quit", "remote_setup"]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def app_window(timeout=20):
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


COLLECT = r"""
  const out = {};
  const walker = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
  const label = (el) => {
    const parts = [];
    for (let e = el; e && e !== document.body && parts.length < 3; e = e.parentElement) {
      const c = typeof e.className === 'string' && e.className.trim() ? '.' + e.className.trim().split(/\s+/).filter((x) => !x.startsWith('svelte-'))[0] : '';
      parts.unshift(e.tagName.toLowerCase() + (c && c !== '.undefined' ? c : ''));
    }
    return parts.join('>');
  };
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const text = n.textContent.trim();
    if (!text) continue;
    const el = n.parentElement;
    if (!el || el.closest('svg, .katex, [aria-hidden="true"]')) continue;
    const r = el.getBoundingClientRect();
    if (r.width === 0 || r.height === 0 || r.bottom < 0 || r.top > innerHeight) continue;
    const s = getComputedStyle(el);
    if (s.visibility === 'hidden' || Number(s.opacity) === 0) continue;
    const face = s.fontFamily.split(',')[0].replace(/["']/g, '').trim();
    const key = face + ' | ' + Math.round(parseFloat(s.fontSize) * 10) / 10 + 'px | ' + s.fontWeight + (s.fontStyle === 'italic' ? ' italic' : '') + (s.textTransform === 'uppercase' || s.fontVariantCaps !== 'normal' ? ' caps' : '');
    (out[key] ??= { count: 0, where: {} }).count += 1;
    const w = label(el);
    out[key].where[w] = (out[key].where[w] || 0) + 1;
    if (!out[key].sample) out[key].sample = text.slice(0, 40);
  }
  return out;
"""

wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
wid = app_window()
if not check(wid is not None, "the app window is on the hidden screen"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(2.5)
screens = {"main": run(COLLECT)}
run("window.__probe.askYesNo({ label: 'Update', question: 'Install updates?', description: 'Each CLI runs its own updater.' }); return true;")
time.sleep(0.8)
screens["yes_no"] = run(COLLECT)
xdo("key", "Escape")
time.sleep(0.5)
for name in SHEETS:
    run(f"window.__probe.openDialog({name!r}); return true;")
    time.sleep(0.8)
    screens["sheet:" + name] = run(COLLECT)
    run(f"window.__probe.closeDialog({name!r}); return true;")
    time.sleep(0.4)
run("document.querySelector('nav.chats button.settings')?.click(); return true;")
time.sleep(1)
for i in range(8):
    run(f"document.querySelectorAll('.settings-nav .row')[{i}]?.click(); return true;")
    time.sleep(0.6)
    screens[f"settings:{i}"] = run(COLLECT)

total = {}
for screen, got in screens.items():
    for key, v in (got or {}).items():
        if not isinstance(v, dict):
            continue
        t = total.setdefault(key, {"count": 0, "screens": [], "where": {}, "sample": v.get("sample")})
        t["count"] += v["count"]
        t["screens"].append(screen)
        for w, c in v["where"].items():
            t["where"][w] = t["where"].get(w, 0) + c
json.dump({"styles": dict(sorted(total.items(), key=lambda kv: -kv[1]["count"])), "screens": list(screens)},
          open(os.path.join(out_dir, "census.json"), "w"), indent=1)
check(len(total) > 0, f"collected {len(total)} distinct text styles")
finish()
