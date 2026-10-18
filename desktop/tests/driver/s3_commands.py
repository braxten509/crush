"""Scaffold S3's check 4 (desktop-design 5.1): every Tauri command of 1.3
answers through invoke. The page calls each one (probe_heavy's invoke list)
and each must come back with a value or an AppError object {code, message};
"not_built" is a valid answer. A command the app lacks, or arguments it does
not take, come back as Tauri's own error text and fail.
Runs in the sandbox, with or without --fixture: tests/check.sh [--fixture FILE] s3_commands.py"""
import json, os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_heavy, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
PROJ = "/tmp/home/proj"

# The 17 commands of desktop-design 1.3, with arguments as the JS wrappers pass them.
COMMANDS = [
    ("app_info", {}),
    ("probe_write", {"json": json.dumps({"s3": "probe_write"})}),
    ("probe_request", {}),
    ("engine_open", {"cwd": PROJ}),
    ("engine_retry", {}),
    ("engine_set_current_session", {"sessionId": ""}),
    ("events_take", {}),
    ("api", {"method": "GET", "path": "sessions"}),
    ("settings_get", {}),
    ("settings_set", {"key": "yolo", "value": True}),
    ("closed_set", {"project": PROJ, "sessionId": "s3-check", "closed": True}),
    ("clipboard_write", {"text": "s3 check"}),
    ("clipboard_read", {}),
    ("open_link", {"url": "https://example.com/", "cwd": PROJ}),
    ("file_info", {"path": "README.md", "cwd": PROJ}),
    ("file_read", {"path": "README.md", "cwd": PROJ, "maxBytes": 1024}),
    ("files_list", {"cwd": PROJ, "maxDepth": 2, "maxItems": 50}),
]

p = wait_for(probe_path, lambda p: p.get("ready"))
check(p.get("ready") is True, "the app window is up and reporting")

# The doorbell (1.2.5, 1.5.4), with a fixture only: engine_open pushes the
# "connected" event and rings "events-ready"; the page's own frame loop must
# take that batch, so a take a moment later finds nothing. This is also the
# smoke test that capabilities/default.json (core:default) allows the listener.
first = probe_heavy(probe_path, timeout=30, invoke=[{"command": "app_info"}])
if ((first.get("invoked") or [{}])[0].get("value") or {}).get("fixture"):
    opened = probe_heavy(probe_path, timeout=30, invoke=[{"command": "engine_open", "args": {"cwd": PROJ}}])
    check(((opened.get("invoked") or [{}])[0]).get("ok") is True, "engine_open joins the fixture's engine")
    time.sleep(1)
    later = probe_heavy(probe_path, timeout=30, invoke=[{"command": "events_take"}])
    left = ((later.get("invoked") or [{}])[0]).get("value")
    check(left == [], f"the page's frame loop took the batch after the doorbell (left for the driver: {json.dumps(left)[:120]})")

report = probe_heavy(probe_path, timeout=30, invoke=[{"command": c, "args": a} for c, a in COMMANDS])
answers = report.get("invoked") or []
check(len(answers) == len(COMMANDS), f"the page answered {len(answers)} of {len(COMMANDS)} commands")
info = None
for (command, _), answer in zip(COMMANDS, answers):
    if answer.get("ok"):
        value = answer.get("value")
        check(True, f"{command} answers: {json.dumps(value)[:120]}")
        if command == "app_info":
            info = value
    else:
        error = answer.get("error")
        is_app_error = isinstance(error, dict) and isinstance(error.get("code"), str) and "message" in error
        check(is_app_error, f"{command} answers with an AppError: {json.dumps(error)[:160]}")
with open(os.path.join(out_dir, "answers.json"), "w") as f:
    json.dump([{"command": c, **a} for (c, _), a in zip(COMMANDS, answers)], f, indent=1)
check(bool(info) and info.get("probe") is True and info.get("cwd") == PROJ,
      f"app_info reports probe mode and the project folder ({info and info.get('cwd')})")
finish()
