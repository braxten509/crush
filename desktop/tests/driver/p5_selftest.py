"""The harness self-test of phases 5 and 6 (desktop-design-5-6 8.6; S5's
check 3). It passes only when all of these hold in one sandbox run:

  1  without a session bus, DBUS_SESSION_BUS_ADDRESS is unset and a Gio
     session-bus connection fails; App() still starts and connects
  6  scripts/p5_selftest.json's bash step shows, in the tool result,
     $CRUSH_TASKS_DIR set, `command -v crush` resolving to the sandbox's test
     build (DESKTOP_CRUSH, put on PATH by tests/sandbox.sh) and
     $CRUSH_DESKTOP_CRUSH empty (the app keeps it from the engine)

Usage: tests/check.sh --no-app p5_selftest.py
"""
import json, os, re, subprocess, sys, time, uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, engine_get, finish, step
import apps

PROMPT = "harness self-test environment"


def connected(p):
    return (p.get("connection") or {}).get("state") == "connected"


def engine_post(probe, api_path, body):
    """POST JSON to the window's engine over its socket (as harness.engine_get reads)."""
    with open(probe) as f:
        conn = json.load(f).get("connection") or {}
    sock = (conn.get("engine") or {}).get("socket")
    if not api_path.startswith("/"):
        api_path = f"/v1/workspaces/{conn.get('workspace_id')}/{api_path}"
    r = subprocess.run(["curl", "--silent", "--fail", "--unix-socket", sock, "-X", "POST", "-H",
                        "Content-Type: application/json", "--data-binary", "@-", f"http://crush{api_path}"],
                       input=json.dumps(body), capture_output=True, text=True, timeout=20)
    if r.returncode != 0:
        raise RuntimeError(f"curl POST {api_path} failed with exit {r.returncode}")
    return json.loads(r.stdout or "null")


def tool_results(messages):
    for m in messages or []:
        for part in m.get("parts") or []:
            if part.get("type") == "tool_result":
                yield (part.get("data") or {}).get("content") or ""


def session_bus_fails():
    from gi.repository import Gio
    try:
        Gio.bus_get_sync(Gio.BusType.SESSION, None)
        return False
    except Exception:  # noqa: BLE001 - GLib.Error: no bus to reach
        return True


def main():
    probe, out_dir = sys.argv[1], sys.argv[2]
    app = None
    try:
        with step("1-no-bus", out_dir):
            check("DBUS_SESSION_BUS_ADDRESS" not in os.environ, "DBUS_SESSION_BUS_ADDRESS is unset")
            app = apps.App(args=["--yolo"])
            p = app.wait_window(connected, timeout=60)
            check(connected(p), f"App() starts and connects without a bus ({(p.get('connection') or {}).get('state')})")
            check(session_bus_fails(), "a Gio session-bus connection fails")

        with step("6-shell-env", out_dir):
            session = engine_post(probe, "sessions", {"title": "Harness self-test"}) or {}
            sid = session.get("id")
            check(bool(sid), "a chat is made for the script")
            engine_post(probe, "agent", {"session_id": sid, "prompt": PROMPT, "run_id": str(uuid.uuid4())})
            end, found = time.time() + 60, None
            while time.time() < end and not found:
                for text in tool_results(engine_get(probe, f"sessions/{sid}/messages")):
                    if "TASKS[" in text:
                        found = text
                time.sleep(0.5)
            with open(os.path.join(out_dir, "shell-env.txt"), "w") as f:
                f.write(found or "")
            tasks = re.search(r"TASKS\[(/[^\]]*)\]", found or "")
            crush = re.search(r"CRUSH\[([^\]]*)\]", found or "")
            desktop = re.search(r"DESKTOP\[([^\]]*)\]", found or "")
            want = os.path.realpath(os.environ.get("DESKTOP_CRUSH", ""))
            got = os.path.realpath(crush.group(1)) if crush and crush.group(1) else ""
            check(found is not None, "the bash step's tool result arrives")
            check(bool(tasks), "$CRUSH_TASKS_DIR is set in the engine's shell")
            check(bool(got) and got == want, f"command -v crush finds the sandbox's test build ({got or 'nothing'})")
            check(bool(desktop) and desktop.group(1) == "", "$CRUSH_DESKTOP_CRUSH is empty in the engine's shell")

    finally:
        if app:
            app.stop()
        apps.kill_terms()
    finish()


if __name__ == "__main__":
    main()
