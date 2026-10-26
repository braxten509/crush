"""The taskbar count covers every chat (owner, 2026-10-25: "a circle with
the pending actions doesn't show up" for a question in another chat).
Electron sends the count as the Unity launcher signal, which KDE's taskbar
reads. On a private session bus:
the fixture's other chat (…f05) waits for a permission answer, so the count
says 1; a question in the shown chat makes it 2; its answer elsewhere brings
it back to 1. The count covers every chat, not only the shown one.

Usage: tests/check.sh --no-app taskbar_badge.py
(the driver writes its fixture in tests/out/taskbar_badge/)
"""
import json, os, re, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from apps import App, Bus
from harness import check, finish, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
CHAT = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
ROOT = os.path.join(os.path.dirname(__file__), "..", "..")

question = {"id": "qa", "session_id": CHAT, "tool_call_id": "", "questions": [{
    "id": "pick", "type": "single_choice", "label": "Pick", "question": "Question A?", "description": "A test question.",
    "choices": [{"id": "a", "label": "First"}, {"id": "b", "label": "Second"}]}]}
fixture = json.load(open(os.path.join(ROOT, "tests", "fixtures", "model-menu.json")))
fixture["events"] = [
    {"at_ms": 5000, "entries": [{"kind": "event", "type": "question_batch_request", "event": "created", "payload": question}]},
    {"at_ms": 9000, "entries": [{"kind": "event", "type": "question_batch_notification", "event": "created", "payload": {"batch_id": "qa", "session_id": CHAT}}]},
]
fixture_path = os.path.join(out_dir, "fixture.json")
json.dump(fixture, open(fixture_path, "w"))

bus = Bus()
monitor_log = os.path.join(out_dir, "launcher.log")
monitor = subprocess.Popen(["dbus-monitor", "--address", bus.address, "interface='com.canonical.Unity.LauncherEntry'"],
                           stdout=open(monitor_log, "w"), stderr=subprocess.DEVNULL)


def counts():
    text = open(monitor_log).read()
    return [(int(n), shown == "true") for n, shown in
            re.findall(r'string "count"\s+variant\s+int64 (\d+).*?string "count-visible"\s+variant\s+boolean (\w+)', text, re.S)]


def wait_count(want, timeout):
    end = time.time() + timeout
    while time.time() < end:
        if counts() and counts()[-1] == want:
            return True
        time.sleep(0.2)
    return False


app = App(args=("--fixture", fixture_path, "-s", CHAT), env=bus.env)
wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
check(wait_count((1, True), 15), f"the other chat's permission ask shows 1 on the taskbar ({counts()})")
check(wait_count((2, True), 15), f"a question in the shown chat makes it 2 ({counts()})")
check(wait_count((1, True), 10), f"once answered elsewhere it is 1 again ({counts()})")
text = open(monitor_log).read()
check("application://crush-desktop.desktop" in text, "the signal names the app's menu entry")
app.stop()
monitor.terminate()
bus.stop()
finish()
