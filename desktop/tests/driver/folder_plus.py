"""Folder + creates and keeps a new chat, in this folder and another one."""
import json
import os
import sys
import time

sys.path.insert(0, os.path.dirname(__file__))
from apps import App, _copy_project, config
from harness import check, finish, probe_eval

probe, output = sys.argv[1:3]
if os.environ.get("HOME") != "/tmp/home" or os.environ.get("XDG_STATE_HOME") != "/tmp/home/.local/state":
    raise RuntimeError("Run this driver inside an isolated test home")
config({"notifications": "disabled"})
other = "/tmp/home/other"
_copy_project(other)
config({"notifications": "disabled"}, other)


def run(script):
    return probe_eval(probe, "window.__probeEval = (() => {" + script + "})();")


def shown(p):
    return next((r["id"] for r in (p.get("leftlist") or {}).get("rows", [])
                 if r.get("shown") and r["id"] != "new-chat"), "")


app = App(args=["--no-sandbox", "--ozone-platform=wayland"])
try:
    app.wait_window(lambda p: (p.get("connection") or {}).get("state") == "connected")
    run('window.__probe.runAction("chat.new"); return true;')
    previous = shown(app.wait_window(shown))
    check(bool(previous), "starting chat is shown")
    run('localStorage.setItem("crush.listFolders", ' + json.dumps(json.dumps(["/tmp/home/proj", other]))
        + '); window.dispatchEvent(new StorageEvent("storage", {key:"crush.listFolders"})); return true;')
    for folder in ["/tmp/home/proj", other, other, "/tmp/home/proj"]:
        clicked = run('const button = document.querySelector(' + json.dumps('section[data-folder="' + folder + '"] .folder-new')
                      + '); button?.click(); return !!button;')
        check(clicked is True, "folder + clicked: " + folder)
        p = app.wait_window(lambda p: shown(p) and shown(p) != previous, timeout=8)
        current = shown(p)
        check(bool(current) and current != previous, "+ shows a new saved chat in " + folder)
        check(folder in (p.get("connection") or {}).get("folders", []),
              "the chosen folder stays connected")
        selected_folder = run('return document.querySelector(' + json.dumps('.chat[data-id="' + current + '"]')
                              + ')?.closest("section.group")?.getAttribute("data-folder");')
        check(selected_folder == folder, "the new chat belongs to the chosen folder")
        rows = (p.get("leftlist") or {}).get("rows", [])
        check(any(r["id"] == previous for r in rows), "the previous chat stays listed")
        until = time.monotonic() + 3.2
        stayed = True
        while time.monotonic() < until:
            time.sleep(0.2)
            latest = app.window()
            stayed = stayed and shown(latest) == current
        check(stayed, "the new chat stays shown through list refreshes")
        previous = current
finally:
    app.stop()
finish()
