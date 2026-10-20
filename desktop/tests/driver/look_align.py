"""Alignment captures in the real window (phase 7 look pass), run inside the
sandbox: tests/look/align_app.py runs it through tests/check.sh and measures
what it leaves in tests/out/look_align/.

The page's boxes come from the real WebKit render, not a stand-in browser:
probe_eval runs tests/out/look_align/in/collect.js (the align skill's mark
helpers, tests/look/app-marks.js and its collector, put together outside the
sandbox, which cannot see the skill) in the window. For each screen the
driver takes the window's picture first, then collects, so the annotations
never reach the picture. Screens: the first screen with a seeded chat, then
every dialog that opens without arguments, one at a time."""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, click, finish, key, probe_eval, probe_heavy, type_text, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
WIDTH, HEIGHT = 1600, 992
SEEDS = ["Seed turn short", "Seed turn code", "Seed turn list", "Seed turn tool"]
DIALOGS = [
    "commands", "settings", "autocompact", "memory_limit", "subagent_instructions", "notifications",
    "models", "reasoning", "usage", "sessions", "projects", "theme", "theme_new", "logs", "about",
    "skills", "kept_skills", "mcp_toggles", "remote_setup", "launcher_password",
    "background", "sub-agents", "filepicker", "folder_picker", "quit",
]
# reasoning and projects close at once here (a model without reasoning
# levels, one project); changes needs a message's tool calls; auth_method
# goes on into a sign-in flow.
COLLECT = open(os.path.join(out_dir, "in", "collect.js")).read()


def app_window(timeout=20):
    """The largest visible window of the app (GTK also makes tiny helpers; the
    title follows the chat and project, so it is found by its class)."""
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


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def settle():
    """Two frames after the last change: sheets open with a short motion."""
    time.sleep(0.8)


def capture(wid, name, surface):
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, name + ".png")], check=True)
    got = probe_eval(probe_path, "window.__alignSurface = " + json.dumps(surface) + ";\n" + COLLECT, timeout=30)
    with open(os.path.join(out_dir, name + ".json"), "w") as f:
        json.dump(got, f, indent=1)
    ok = isinstance(got, dict) and not got.get("error")
    check(ok, f"{name}: collected {len((got or {}).get('rows') or [])} rows" + ("" if ok else f" ({(got or {}).get('error')})"))
    return got


def send(text):
    """Focuses the typing box, then types with the real keyboard."""
    if not run("const f = document.querySelector('.composer textarea, .composer [contenteditable]'); f?.focus(); return Boolean(f);"):
        return False
    type_text(text)
    key("Return")
    return True


wid = app_window()
if not check(wid is not None, "the app window opens"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, WIDTH, HEIGHT)
p = wait_for(probe_path, lambda p: p.get("ready") and p.get("fonts") == "loaded" and (p.get("chat") or {}).get("ready", True), 30)
check(p.get("ready"), "the first screen is ready")

# The first screen asks to initialize the project; answer Not now.
run("const b = [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Not now'); b?.click(); return Boolean(b);")
settle()
for i, text in enumerate(SEEDS):
    if not check(send(text), f"sent {text!r}"):
        break
    wait_for(probe_path, lambda p, n=i + 1: sum(1 for m in (p.get("chat") or {}).get("messages") or []
                                                 if m.get("role") == "assistant" and m.get("finished")) >= n, 30)
settle()
capture(wid, "main", {"id": "main", "name": "First screen with a chat", "kind": "page", "selector": '[data-probe="shell"]'})

for entry in DIALOGS:
    dialog, props = entry if isinstance(entry, tuple) else (entry, "{}")
    opened = run(f"window.__probe.openDialog({json.dumps(dialog)}, {props}); return true;")
    settle()
    front = (probe_heavy(probe_path).get("overlay") or {}).get("dialogs") or []
    if not check(dialog in front, f"{dialog} opens ({opened})"):
        run(f"window.__probe.closeDialog({json.dumps(dialog)}); return true;")
        continue
    capture(wid, "dialog-" + dialog, {"id": dialog, "name": f"Dialog {dialog}", "kind": "dialog",
                                      "selector": f'[data-dialog="{dialog}"] .frame'})
    run(f"window.__probe.closeDialog({json.dumps(dialog)}); return true;")
    settle()

# Nothing may stay open over the next captures.
run("for (const d of document.querySelectorAll('[data-dialog]')) window.__probe.closeDialog(d.dataset.dialog); return true;")
settle()

# Picture size (12/12): Compact's 8/12 puts many edges on a third of a pixel,
# so a pixel off there is checked against the same screens at whole pixels.
for _ in range(4):
    xdo("key", "--clearmodifiers", "ctrl+equal")
    time.sleep(0.3)
p = wait_for(probe_path, lambda p: abs((p.get("zoom") or 0) - 1) < 1e-6, 10)
if check(abs((p.get("zoom") or 0) - 1) < 1e-6, f"zoomed to picture size ({p.get('zoom')})"):
    settle()
    capture(wid, "main-12", {"id": "main-12", "name": "First screen at 12/12", "kind": "page", "selector": '[data-probe="shell"]'})
    run("window.__probe.openDialog('sessions', {}); return true;")
    settle()
    capture(wid, "dialog-sessions-12", {"id": "sessions", "name": "Dialog sessions at 12/12", "kind": "dialog",
                                        "selector": '[data-dialog="sessions"] .frame'})
    run("window.__probe.closeDialog('sessions'); return true;")
xdo("key", "--clearmodifiers", "ctrl+0")

finish()
