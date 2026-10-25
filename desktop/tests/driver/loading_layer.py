"""The gray loading layer (owner, 2026-10-25): while the app starts, the
window grays out under "Starting Crush…"; while a folder opens, under
"Opening <folder>…", taking the clicks; it goes once the load is done.

The engine starts half a second slower here (tests/fakes/crush-wrap with
/tmp/slow-ensure), so each load outlasts the layer's 400 ms wait.

Usage: CHECK_CRUSH=$PWD/tests/fakes/crush-wrap tests/check.sh --no-app loading_layer.py
"""
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(__file__))
from apps import App, _copy_project, config
from harness import check, finish, probe_eval, shot

probe, output = sys.argv[1:3]
if os.environ.get("HOME") != "/tmp/home" or os.environ.get("XDG_STATE_HOME") != "/tmp/home/.local/state":
    raise RuntimeError("Run this driver inside an isolated test home")
if not os.environ.get("DESKTOP_CRUSH", "").endswith("crush-wrap"):
    raise RuntimeError("Run with CHECK_CRUSH=$PWD/tests/fakes/crush-wrap (the slow engine start)")
open("/tmp/slow-ensure", "w").close()
config({"notifications": "disabled"})
other = "/tmp/home/other"
_copy_project(other)
config({"notifications": "disabled"}, other)


def run(script):
    return probe_eval(probe, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def layer(p):
    return p.get("loading") or {}


app = App(args=["--no-sandbox"])
try:
    # The start: the layer shows with the starting line, then goes.
    labels = set()
    end = time.time() + 30
    p = {}
    while time.time() < end:
        p = app.window()
        if layer(p).get("shown"):
            labels.add(layer(p).get("label"))
        if (p.get("connection") or {}).get("state") == "connected" and p.get("loading") and not layer(p).get("pending"):
            break
        time.sleep(0.1)
    check("Starting Crush…" in labels, f"the start grays the window under the starting line (seen: {sorted(labels)})")
    p = app.wait_window(lambda p: not layer(p).get("shown"), timeout=5)
    check(not layer(p).get("shown"), "the layer goes once the app is ready")

    # A folder not yet open: the layer names it and takes the clicks.
    seen = run('''
      window.__layerSeen = [];
      new MutationObserver(() => {
        const el = document.querySelector('[data-probe="loading-layer"]');
        if (!el || window.__layerSeen.length) return;
        const r = el.getBoundingClientRect();
        const hit = document.elementFromPoint(r.left + 40, r.top + r.height / 2);
        window.__layerSeen.push({ text: el.textContent.trim(), blocks: el.contains(hit), w: r.width, h: r.height });
      }).observe(document.body, { childList: true, subtree: true });
      localStorage.setItem("crush.listFolders", ''' + json.dumps(json.dumps(["/tmp/home/proj", other])) + ''');
      window.dispatchEvent(new StorageEvent("storage", { key: "crush.listFolders" }));
      for (let i = 0; i < 50; i++) {
        const button = document.querySelector(''' + json.dumps('section[data-folder="' + other + '"] .folder-new') + ''');
        if (button) { button.click(); return true; }
        await new Promise((r) => setTimeout(r, 20));
      }
      return false;''')
    check(seen is True, "folder + clicked on the new folder")
    p = app.wait_window(lambda p: layer(p).get("shown"), timeout=10)
    check(layer(p).get("shown") is True, "the layer shows while the folder opens")
    shot(output, "opening")
    p = app.wait_window(lambda p: not layer(p).get("pending") and not layer(p).get("shown"), timeout=30)
    check(not layer(p).get("shown"), "the layer goes once the folder is open")
    found = run("return window.__layerSeen;") or []
    first = found[0] if found else {}
    check(first.get("text") == "Opening other…", f"it names the folder (got {first.get('text')!r})")
    check(first.get("blocks") is True, "it takes the clicks meant for the chat list")
    check(first.get("w", 0) > 600 and first.get("h", 0) > 400, f"it covers the window (size {first.get('w')}x{first.get('h')})")
    shot(output, "after")
finally:
    app.stop()
finish()
