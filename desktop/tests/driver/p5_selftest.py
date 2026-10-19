"""The harness self-test of phases 5 and 6 (desktop-design-5-6 8.6; S5's
check 3). It passes only when all of these hold in one sandbox run:

  1  without Bus, DBUS_SESSION_BUS_ADDRESS is unset and a Gio session-bus
     connection fails; App() still starts and connects
  2  with Bus, one Gio Notify call makes exactly one catcher line with the
     summary and body sent
  3  command -v finds every stand-in in tests/fakes/bin/; each call adds one
     $FAKE_CMD_LOG line; FAKE_CMD_FAIL=paplay makes paplay exit 1;
     FAKE_CMD_STDOUT_DOCKER=x makes docker print x
  4  Term starts tests/out/bin/crush and capture-pane shows its screen within 10 s
  5  a seams file with a memory seam makes GET /v1/memory report high: true
  6  scripts/p5_selftest.json's bash step prints $CRUSH_TASKS_DIR and
     $CRUSH_DESKTOP_CRUSH, both set, in the tool result
  7  window_open for /tmp/home/proj2 opens a second window: /tmp/probe.json.w2
     shows it connected, while /tmp/probe.json still shows the first project
  8  a check written to fail (this driver with --broken) is reported as a failure
  9  the outside check: tests/check.sh runs it after this driver and fails the
     run when the owner's Crush files changed

Usage: tests/check.sh --no-app p5_selftest.py
       (--broken, given by step 8: one check that fails, nothing else)
"""
import json, os, re, subprocess, sys, time, uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, engine_get, finish, probe_heavy, step
import apps

STAND_INS = ["canberra-gtk-play", "pw-play", "paplay", "docker", "xdg-open", "x-www-browser", "www-browser"]
PROMPT = "harness self-test environment"
MEMORY = {"percent": 97, "top": [{"name": "crushtest-hog", "bytes": 3 << 30}], "total_ram_bytes": 16 << 30}


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


def workspace_path(probe):
    with open(probe) as f:
        ws = (json.load(f).get("connection") or {}).get("workspace_id")
    return (engine_get(probe, f"/v1/workspaces/{ws}") or {}).get("path") if ws else None


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


def notify(address, summary, body):
    from gi.repository import Gio, GLib
    flags = Gio.DBusConnectionFlags.AUTHENTICATION_CLIENT | Gio.DBusConnectionFlags.MESSAGE_BUS_CONNECTION
    conn = Gio.DBusConnection.new_for_address_sync(address, flags, None, None)
    reply = conn.call_sync("org.freedesktop.Notifications", "/org/freedesktop/Notifications",
                           "org.freedesktop.Notifications", "Notify",
                           GLib.Variant("(susssasa{sv}i)", ("crushtest", 0, "", summary, body, [], {}, -1)),
                           GLib.VariantType("(u)"), Gio.DBusCallFlags.NONE, 5000, None)
    conn.close_sync(None)
    return reply.unpack()[0]


def stand_in(name, *args, env=None):
    return subprocess.run([name, *args], capture_output=True, text=True, env={**os.environ, **(env or {})}, timeout=10)


def main():
    probe, out_dir = sys.argv[1], sys.argv[2]
    if "--broken" in sys.argv[3:]:
        check(False, "a check written to fail")
        finish()
    app = bus = None
    try:
        with step("1-no-bus", out_dir):
            check("DBUS_SESSION_BUS_ADDRESS" not in os.environ, "DBUS_SESSION_BUS_ADDRESS is unset")
            app = apps.App(args=["--yolo"], env=apps.seams({"memory": MEMORY}))
            p = app.wait_window(connected, timeout=60)
            check(connected(p), f"App() starts and connects without a bus ({(p.get('connection') or {}).get('state')})")
            check(session_bus_fails(), "a Gio session-bus connection fails")

        with step("2-bus", out_dir):
            bus = apps.Bus()
            check(bus.ready, "the catcher owns org.freedesktop.Notifications on the private bus")
            before = len(bus.notifications())
            nid = notify(bus.address, "Self-test summary", "Self-test body")
            time.sleep(0.3)
            lines = bus.notifications()[before:]
            check(nid > 0 and len(lines) == 1 and lines[0].get("summary") == "Self-test summary"
                  and lines[0].get("body") == "Self-test body",
                  f"one Notify call makes exactly one catcher line with its summary and body ({len(lines)} lines)")

        with step("3-stand-ins", out_dir):
            fakes = os.path.join(apps.ROOT, "tests", "fakes", "bin")
            for name in STAND_INS:
                found = subprocess.run(["bash", "-c", f"command -v {name}"], capture_output=True, text=True).stdout.strip()
                check(found == os.path.join(fakes, name), f"command -v {name} finds the stand-in ({found})")
                before = len(apps.calls(name))
                stand_in(name, "--self-test")
                got = apps.calls(name)[before:]
                check(len(got) == 1 and got[0].get("args") == ["--self-test"], f"one {name} call adds one $FAKE_CMD_LOG line")
            check(stand_in("paplay", "x.oga", env={"FAKE_CMD_FAIL": "paplay"}).returncode == 1, "FAKE_CMD_FAIL=paplay makes paplay exit 1")
            check(stand_in("paplay", "x.oga").returncode == 0, "without it paplay exits 0")
            r = stand_in("docker", "mcp", "version", env={"FAKE_CMD_STDOUT_DOCKER": "x"})
            check(r.returncode == 0 and r.stdout == "x", f"FAKE_CMD_STDOUT_DOCKER=x makes docker print x ({r.stdout!r})")

        with step("4-term", out_dir):
            term = apps.Term()
            end, screen = time.time() + 10, ""
            while time.time() < end and not screen.strip():
                screen = term.text()
                time.sleep(0.2)
            check(bool(screen.strip()), "capture-pane shows the terminal app's screen within 10 s")
            with open(os.path.join(out_dir, "term.txt"), "w") as f:
                f.write(screen)
            term.stop()
            apps.kill_terms()

        with step("5-memory", out_dir):
            mem = engine_get(probe, "/v1/memory") or {}
            check(mem.get("high") is True and mem.get("percent") == MEMORY["percent"],
                  f"the memory seam makes GET /v1/memory report high ({mem.get('high')}, {mem.get('percent')}%)")

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
            desktop = re.search(r"DESKTOP\[(/[^\]]*)\]", found or "")
            check(found is not None, "the bash step's tool result arrives")
            check(bool(tasks), "$CRUSH_TASKS_DIR is set in the engine's shell")
            check(bool(desktop), "$CRUSH_DESKTOP_CRUSH is set in the engine's shell")

        with step("7-second-window", out_dir):
            apps.second_project()
            report = probe_heavy(probe, invoke=[{"command": "window_open", "args": {"projectDir": apps.PROJ2}}])
            invoked = (report.get("invoked") or [{}])[0]
            check(invoked.get("ok") is True, f"window_open answers ({invoked.get('error') or invoked.get('value')})")
            w2 = app.wait_window(connected, label="w2", timeout=60)
            check(connected(w2), f"/tmp/probe.json.w2 shows the second window connected ({(w2.get('connection') or {}).get('state')})")
            check(workspace_path(app.probe_file("w2")) == apps.PROJ2, "the second window is on /tmp/home/proj2")
            check(workspace_path(probe) == apps.PROJ, "/tmp/probe.json still shows the first window's project")

        with step("8-broken", out_dir):
            r = subprocess.run([sys.executable, "-I", os.path.abspath(__file__), probe, out_dir, sys.argv[3] if len(sys.argv) > 3 else "",
                                "--broken"], capture_output=True, text=True, timeout=60)
            check(r.returncode != 0 and "FAIL a check written to fail" in r.stdout and "1 FAILED" in r.stdout,
                  f"a check written to fail is reported as a failure (exit {r.returncode})")

        with step("9-outside", out_dir):
            check(True, "tests/check.sh runs the outside check after this driver and fails the run when it finds a change")
    finally:
        if bus:
            bus.stop()
        if app:
            app.stop()
        apps.kill_terms()
    finish()


if __name__ == "__main__":
    main()
