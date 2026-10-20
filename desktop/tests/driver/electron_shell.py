"""The Electron shell's own parts (owner 2026-10-20: the app moved from Tauri
to Electron, electron/main.cjs): the ones no other window check reaches. In
the real window on the hidden screen: the clipboard round trip through
Electron, an event from the helper reaching the page, the window's title
following the page, the taskbar marks of a blocked chat (the app must live
through them), the navigation guard, and a page reload after a crash.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json electron_shell.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]


def page(script, wait=8):
    """Runs an async page script that leaves its answer in window.__shellCheck."""
    probe_eval(probe_path, "window.__shellCheck = undefined; (async () => { try { window.__shellCheck = await (async () => {"
               + script + "})(); } catch (e) { window.__shellCheck = { error: String(e && e.message || e) }; } })(); window.__probeEval = 1;")
    end = time.time() + wait
    while time.time() < end:
        got = probe_eval(probe_path, "window.__probeEval = window.__shellCheck === undefined ? '__waiting' : window.__shellCheck;")
        if got != "__waiting":
            return got
        time.sleep(0.2)
    return None


def window_name():
    r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
    for wid in r.stdout.split():
        name = subprocess.run(["xdotool", "getwindowname", wid], capture_output=True, text=True).stdout.strip()
        if name:
            return name
    return ""


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)

got = page('const wrote = await window.crushBridge.invoke("clipboard_write", { text: "crush electron check" });'
           ' return { wrote, read: (await window.crushBridge.invoke("clipboard_read", {})).ok };')
check(got == {"wrote": {"ok": None}, "read": "crush electron check"},
      f"a clipboard write succeeds and the clipboard holds it ({got!r})")

got = page('const seen = new Promise((done) => window.crushBridge.listen("settings-changed", (e) => done(e.payload)));'
           ' const set = await window.crushBridge.invoke("settings_set", { key: "appearance", value: "dark" });'
           ' const payload = await Promise.race([seen, new Promise((done) => setTimeout(() => done(null), 3000))]);'
           ' return { set: Boolean(set.ok), event: payload && payload.appearance };')
check(got == {"set": True, "event": "dark"}, f"a setting the page sets comes back as the helper's event ({got})")

title = page('return document.title;')
end = time.time() + 5
while time.time() < end and window_name() != title:
    time.sleep(0.2)
check(bool(title) and window_name() == title, f"the window's title follows the page ({window_name()!r}, page {title!r})")

got = page('const r = await window.crushBridge.invoke("alert_window_state", { state: "blocked", projectDir: "/tmp/home/proj",'
           ' sessionId: "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01", focused: false }); return "ok" in r;')
time.sleep(1)
alive = page('return (await window.crushBridge.invoke("updates_claim", {})).ok !== undefined;')
check(got is True and alive is True, "the taskbar marks of a blocked chat are set and the app lives on")

opened = probe_path + ".opened.jsonl"
before = open(opened).read().count("\n") if os.path.exists(opened) else 0
page('location.href = "https://example.com/"; return 1;')
time.sleep(1.5)
lines = open(opened).read().splitlines() if os.path.exists(opened) else []
here = page('return location.protocol + "//" + location.host;')
check(len(lines) > before and json.loads(lines[-1]) == {"refused_navigation": "https:"} and here == "app://localhost",
      f"a navigation away is refused and recorded ({lines[-1:]}, page still at {here})")

page('window.__beforeReload = 1; return 1;')
page('await window.crushBridge.invoke("crash_restart", {}); return 1;')
time.sleep(1)
wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
got = page('return window.__beforeReload === undefined;', wait=15)
check(got is True, "crash_restart reloads the page, which joins again")

finish()
