"""Scaffold S5's check 4 (desktop-design-5-6 7.1): every command of 6.4
answers through invoke. The page calls each one (probe_heavy's invoke list)
and each must come back with a value or an AppError object {code, message};
"not_built" is a valid answer. A command the app lacks, or arguments it does
not take, come back as Tauri's own error text and fail.

Every window answers the shared heavy request (8.2), so window_close is not
in the list: a second window is opened with window_open and sent its close
request (WM_DELETE_WINDOW), and lib/closing.js's call of window_close must end
it well before Rust's 10 s fallback, with the first window and the app still
up. project_switch runs last in the list, as it closes the window's engine
connection. Channel arguments are passed in Tauri's channel form; the
placeholders never send on them.
Runs in the sandbox on the engine: tests/check.sh s5_commands.py"""
import importlib.util, json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, probe_heavy, wait_for
import apps

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("p3_connect", os.path.join(HERE, "p3_connect.py"))
pc = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pc)

probe_path, out_dir = sys.argv[1], sys.argv[2]
PROJ = apps.PROJ

# The commands of 6.4 but window_close, with arguments as the JS wrappers pass them.
COMMANDS = [
    ("api_secret", {"path": "sessions", "body": {"name": "s5"}}),
    ("api_secret", {"path": "secure-forms/s5-check/submit", "body": {"values": {}}}),
    ("window_open", {"projectDir": PROJ}),
    ("pty_spawn", {"cwd": PROJ, "argv": None, "cols": 80, "rows": 24, "onData": "__CHANNEL__:1"}),
    ("pty_write", {"id": "1", "data": ""}),
    ("pty_resize", {"id": "1", "cols": 80, "rows": 24}),
    ("pty_cwd", {"id": "1"}),
    ("pty_close", {"id": "1"}),
    ("pty_close_all", {}),
    ("editor_prepare", {"text": "s5", "line": 1, "column": 1}),
    ("editor_finish", {"path": "/tmp/s5-none"}),
    ("alert_notify", {"title": "S5", "body": "check"}),
    ("alert_sound", {"id": "bell"}),
    ("alert_window_state", {"state": "idle", "projectDir": PROJ, "sessionId": "", "focused": True}),
    ("alert_handler", {"projectDir": PROJ, "sessionId": ""}),
    ("crash_pending", {}),
    ("crash_restart", {}),
    ("logs_tail", {"lines": 10}),
    ("logs_follow", {"onLines": "__CHANNEL__:2"}),
    ("logs_unfollow", {"followId": "s5"}),
    ("selfupdate_run", {"tag": "v0.0.0"}),
    ("selfupdate_install", {}),
    ("crush_dirs", {}),
    ("crush_stats", {}),
    ("docker_mcp_version", {}),
    ("projects_list", {}),
    ("project_save", {"path": PROJ}),
    ("project_remove", {"path": PROJ}),
    ("recent_folders", {"limit": 5}),
    ("subagent_instructions_read", {}),
    ("subagent_instructions_write", {"text": "s5"}),
    ("remote_password_exists", {}),
    ("remote_password_set", {"first": "s5-check", "second": "s5-check"}),
    ("dir_list", {"path": PROJ}),
    ("folder_pick", {"start": PROJ}),
    ("app_info", {}),
    ("project_switch", {"projectDir": PROJ}),
]


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

report = probe_heavy(probe_path, timeout=60, invoke=[{"command": c, "args": a} for c, a in COMMANDS])
answers = report.get("invoked") or []
check(len(answers) == len(COMMANDS), f"the page answered {len(answers)} of {len(COMMANDS)} commands")
by_name = {}
for (command, _), answer in zip(COMMANDS, answers):
    by_name.setdefault(command, []).append(answer)
    if answer.get("ok"):
        check(True, f"{command} answers: {json.dumps(answer.get('value'))[:120]}")
    else:
        error = answer.get("error")
        is_app_error = isinstance(error, dict) and isinstance(error.get("code"), str) and "message" in error
        check(is_app_error, f"{command} answers with an AppError: {json.dumps(error)[:160]}")
with open(os.path.join(out_dir, "answers.json"), "w") as f:
    json.dump([{"command": c, **a} for (c, _), a in zip(COMMANDS, answers)], f, indent=1)

refused = (by_name.get("api_secret") or [{}])[0]
check((refused.get("error") or {}).get("code") == "secret_route", f"api_secret refuses a route outside its list ({json.dumps(refused)[:120]})")
focused = (by_name.get("window_open") or [{}])[0].get("value") or {}
check(focused == {"label": "main", "created": False}, f"window_open on the window's own project focuses it ({focused})")
info = (by_name.get("app_info") or [{}])[0].get("value") or {}
check(info.get("first_window") is True and info.get("new_chat") is False and "launch_id" in info and bool(info.get("home")),
      f"app_info gains first_window, launch_id, new_chat and home ({ {k: info.get(k) for k in ('first_window', 'launch_id', 'new_chat', 'home')} })")

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
