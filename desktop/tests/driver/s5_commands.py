"""A second window's close request (desktop-design-5-6 7.1, S5's check 4):
a second window is opened with window_open and sent its close request
(WM_DELETE_WINDOW), and lib/closing.js's call of window_close must end it well
before Rust's 10 s fallback, with the first window and the app still up.
Runs in the sandbox on the engine: tests/check.sh s5_commands.py"""
import importlib.util, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, probe_heavy, wait_for
import apps

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("p3_connect", os.path.join(HERE, "p3_connect.py"))
pc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pc)

probe_path, out_dir = sys.argv[1], sys.argv[2]


def crush_windows():
    r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
    out = []
    for wid in r.stdout.split():
        g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
        d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
        if int(d.get("WIDTH", 0)) > 100:
            out.append(int(wid))
    return set(out)


p = wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 60)
check((p.get("connection") or {}).get("state") == "connected", "the first window is up and connected")
app_pid = int(subprocess.run(["pgrep", "-f", "-n", "crush-desktop --probe"], capture_output=True, text=True).stdout.split()[0])

# window_close, from a second window's close request.
apps.second_project()
before = crush_windows()
opened = probe_heavy(probe_path, timeout=30, invoke=[{"command": "window_open", "args": {"projectDir": apps.PROJ2}}])
answer = (opened.get("invoked") or [{}])[0]
check(answer.get("ok") is True and (answer.get("value") or {}).get("created") is True, f"window_open opens a window for proj2 ({answer})")
w2 = wait_for(probe_path + ".w2", lambda p: (p.get("connection") or {}).get("state") == "connected", 60)
check((w2.get("connection") or {}).get("state") == "connected", "the second window connects")
end = time.time() + 10
new = set()
while time.time() < end and not new:
    new = crush_windows() - before
    time.sleep(0.2)
check(len(new) == 1, f"one new window appears ({len(new)})")
if new:
    wid = new.pop()
    asked = time.time()
    check(pc.ask_close(wid), "the second window gets its close request")
    while time.time() < asked + 12 and wid in crush_windows():
        time.sleep(0.1)
    took = time.time() - asked
    check(wid not in crush_windows() and took < 8, f"lib/closing.js's window_close ends it before the 10 s fallback ({took:.1f} s)")
    check(pc.alive(app_pid), "the app keeps running")
    check(len(crush_windows()) == 1, "the first window stays")
finish()
