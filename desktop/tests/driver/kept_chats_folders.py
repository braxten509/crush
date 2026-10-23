"""Real app check: empty chats stay listed; every sidebar folder stays live.
Run through gpu-screen and run-in-sandbox.sh --no-app, with the real test engine.
"""
import json, os, sys

sys.path.insert(0, os.path.dirname(__file__))
from apps import App, _copy_project, config
from harness import check, finish, probe_eval

probe, output = sys.argv[1:3]
config({"notifications": "disabled"})
folders = [f"/tmp/home/folder{i}" for i in range(6)]
for folder in folders:
    _copy_project(folder)
    config({"notifications": "disabled"}, folder)


def rows(p):
    return (p.get("leftlist") or {}).get("rows") or []


def shown(p):
    return next((r["id"] for r in rows(p) if r.get("shown") and r["id"] != "new-chat"), "")


def run(script):
    return probe_eval(probe, "window.__probeEval = (() => {" + script + "})();")


app = App(args=["--no-sandbox", "--ozone-platform=wayland"])
try:
    app.wait_window(lambda p: (p.get("connection") or {}).get("state") == "connected")
    run('window.__probe.runAction("chat.new"); return true;')
    first = shown(app.wait_window(shown))
    check(bool(first), "New chat gets a saved ID before any message")
    run('window.__probe.runAction("chat.new"); return true;')
    second = shown(app.wait_window(lambda p: shown(p) and shown(p) != first))
    check(bool(second) and any(r["id"] == first for r in rows(app.window())), "leaving an empty chat keeps it in the sidebar")
    run(f'document.querySelector(\'.chat[data-id="{first}"] .row\').click(); return true;')
    check(shown(app.wait_window(lambda p: shown(p) == first)) == first, "the empty chat reopens from its row")
    run(f'document.querySelector(\'.chat[data-id="{first}"] .close\').click(); return true;')
    check(all(r["id"] != first for r in rows(app.wait_window(lambda p: all(r["id"] != first for r in rows(p))))), "only closing the chat removes its row")
    run('localStorage.setItem("crush.listFolders", ' + json.dumps(json.dumps(["/tmp/home/proj", *folders])) + '); window.dispatchEvent(new StorageEvent("storage", {key:"crush.listFolders"})); return true;')
    p = app.wait_window(lambda p: len((p.get("connection") or {}).get("folders") or []) == 7)
    check(len((p.get("connection") or {}).get("folders") or []) == 7, "all seven sidebar folders stay connected")
finally:
    app.stop()
finish()
