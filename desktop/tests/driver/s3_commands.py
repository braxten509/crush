"""Scaffold S3's check 4 (desktop-design 5.1): every Tauri command of 1.3
answers through invoke. The page calls each one (probe_heavy's invoke list)
and each must come back with a value or an AppError object {code, message};
"not_built" is a valid answer. A command the app lacks, or arguments it does
not take, come back as Tauri's own error text and fail. Also checks the
probe's "opened" count (1.9) and, with a fixture, the doorbell and that an
api call's callId and an api_abort reach the fixture player.
Runs in the sandbox, with or without --fixture: tests/check.sh [--fixture FILE] s3_commands.py"""
import json, os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_heavy, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
PROJ = "/tmp/home/proj"

# The 18 commands of desktop-design 1.3, with arguments as the JS wrappers pass them.
COMMANDS = [
    ("app_info", {}),
    ("probe_write", {"json": json.dumps({"s3": "probe_write"})}),
    ("probe_request", {}),
    ("engine_open", {"cwd": PROJ}),
    ("engine_retry", {}),
    ("engine_set_current_session", {"sessionId": ""}),
    ("events_take", {}),
    ("api", {"method": "GET", "path": "sessions"}),
    ("api_abort", {"callId": "s3-check"}),
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

    # api's callId (1.3) reaches the player, which records every call but a GET;
    # api_abort answers false there (no fixture call runs long) and is recorded too.
    post = {"method": "POST", "path": "sessions/s3-check/shell", "body": {"command": "true"}, "callId": "s3-call"}
    probe_heavy(probe_path, timeout=30, invoke=[{"command": "api", "args": post},
                                                {"command": "api_abort", "args": {"callId": "s3-call"}}])
    try:
        with open(probe_path + ".api.jsonl") as f:
            recorded = [json.loads(line) for line in f if line.strip()]
    except OSError:
        recorded = []
    check(any(r.get("command") == "api" and r.get("call_id") == "s3-call" for r in recorded),
          "a POST sent with a callId reaches the fixture with its callId")
    check(any(r.get("command") == "api_abort" and r.get("call_id") == "s3-call" for r in recorded),
          "api_abort reaches the fixture")

# The probe's "opened" (1.9) counts the lines of <probe>.opened.jsonl, read
# through probe_request's answer: 0 while nothing opened anything.
before = wait_for(probe_path, lambda p: "opened" in p).get("opened")
check(before == 0, f"the probe reports opened 0 before anything opened ({before})")
with open(probe_path + ".opened.jsonl", "a") as f:
    f.write(json.dumps({"s3_check": "opened"}) + "\n" + json.dumps({"s3_check": "opened"}) + "\n")
counted = wait_for(probe_path, lambda p: p.get("opened") == 2).get("opened")
check(counted == 2, f"the probe's opened counts the lines of <probe>.opened.jsonl (2 written, {counted} reported)")

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
abort = dict(zip((c for c, _ in COMMANDS), answers)).get("api_abort") or {}
check(abort.get("ok") is True and abort.get("value") is False,
      f"api_abort answers false when no call with that id runs ({json.dumps(abort)[:120]})")
check(bool(info) and info.get("probe") is True and info.get("cwd") == PROJ,
      f"app_info reports probe mode and the project folder ({info and info.get('cwd')})")
finish()
