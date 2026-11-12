"""Folder + creates and keeps a new chat, in this folder and another one.

Every frame for a second after each click is recorded too (owner,
2026-10-23, from a screencast of a wrong-frame flash): the new row must never
show under another folder, never go missing from its own folder once drawn,
no folder may lose its rows for a frame, and no row may be drawn without its
title."""
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


def run_async(script):
    return probe_eval(probe, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


# Every frame for a second after the click: the groups as drawn, in order,
# with each row's id and title. Started without waiting; read back later.
RECORD = """
  window.__frames = null;
  const frames = [];
  const sample = () => [...document.querySelectorAll('nav.chats section.group')].map((g) => ({
    dir: g.getAttribute('data-folder') || '',
    rows: [...g.querySelectorAll('.chat')].map((r) => ({
      id: r.getAttribute('data-id') || '',
      title: (r.querySelector('.title')?.textContent || '').trim(),
    })),
  }));
  let framesLeft = 60;
  const tick = () => {
    frames.push(sample());
    framesLeft -= 1;
    if (framesLeft <= 0) window.__frames = frames;
    else requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return true;
"""


def recorded():
    """The frames RECORD took, once it has taken them all."""
    for _ in range(100):
        frames = run_async("return window.__frames;")
        if isinstance(frames, list) and frames:
            return frames
        time.sleep(0.05)
    return []


def frame_faults(frames, folder, new_ids):
    """Wrong-folder, missing, dropped-rows and untitled-row frames."""
    wrong, missing, dropped, blank = [], [], [], []
    seen, drawn = {}, False
    for index, frame in enumerate(frames):
        own = False
        for group in frame:
            ids = {r["id"] for r in group["rows"]}
            if group["dir"] == folder:
                own = bool(ids & new_ids)
            elif ids & new_ids:
                wrong.append((index, group["dir"]))
            if seen.get(group["dir"], 0) > 0 and not group["rows"]:
                dropped.append((index, group["dir"]))
            seen[group["dir"]] = len(group["rows"])
            blank += [(index, group["dir"], r["id"]) for r in group["rows"] if not r["title"]]
        if drawn and not own:
            missing.append(index)
        drawn = drawn or own
    return wrong, missing, dropped, blank


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
        run_async(RECORD)
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
        until = time.monotonic() + 0.5
        stayed = True
        while time.monotonic() < until:
            time.sleep(0.1)
            latest = app.window()
            stayed = stayed and shown(latest) == current
        check(stayed, "the new chat stays shown through list refreshes")
        frames = recorded()
        check(bool(frames), "the frames after the click were recorded")
        wrong, missing, dropped, blank = frame_faults(frames, folder, {"new-chat", current})
        check(not wrong, f"the new row never shows under another folder {wrong[:3]}")
        check(not missing, f"once drawn, the new row stays in its own folder {missing[:5]}")
        check(not dropped, f"no folder loses its rows for a frame {dropped[:3]}")
        check(not blank, f"no row is ever drawn without its title {blank[:3]}")
        previous = current
finally:
    app.stop()
finish()
