"""Phase-0 harness self-test, run inside the sandbox by run-in-sandbox.sh.
Proves the harness can pass and can fail: with --broken it adds one check
that is wrong on purpose, which must be reported as FAIL."""
import json, os, shutil, socket, subprocess, sys, urllib.request

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, shot, wait_for, window_geometry

probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
broken = "--broken" in sys.argv[4:]
root = os.environ["DESKTOP_ROOT"]

# 0. The sandbox is sealed: silent, offline and cut off from the real desktop.
check(not os.path.exists("/dev/snd"), "no sound card inside the sandbox")
run_dir = f"/run/user/{os.getuid()}"
leaks = [n for n in ("pulse", "pipewire-0", "wayland-0", "wayland-1", "bus") if os.path.exists(os.path.join(run_dir, n))]
check(not leaks, f"no desktop, sound or message-bus sockets inside the sandbox {leaks or ''}")
own = os.environ.get("DISPLAY", ":").split(":")[1].split(".")[0]
reachable = []
for n in range(10):
    if str(n) == own:
        continue
    for addr in (f"\0/tmp/.X11-unix/X{n}", f"/tmp/.X11-unix/X{n}"):
        s = socket.socket(socket.AF_UNIX)
        try:
            s.connect(addr)
            reachable.append(f":{n}")
        except OSError:
            pass
        finally:
            s.close()
check(not reachable, f"no real screen is reachable, only the hidden one :{own} {reachable or ''}")
ifaces = [l.split(":")[0].strip() for l in open("/proc/net/dev").read().splitlines()[2:]]
check(ifaces == ["lo"], f"the only network device is loopback {ifaces}")
try:
    socket.create_connection(("1.1.1.1", 443), timeout=2).close()
    check(False, "no outside network inside the sandbox")
except OSError:
    check(True, "no outside network inside the sandbox")
bus = socket.socket(socket.AF_UNIX)
try:
    bus.connect("/run/dbus/system_bus_socket")
    check(False, "the system message bus is not reachable")
except OSError:
    check(True, "the system message bus is not reachable")
finally:
    bus.close()
check(os.environ.get("HOME") == "/tmp/home" and not any(k.startswith("CRUSH_") for k in os.environ), "throwaway home and no Crush settings from outside")
# The owner's real home shows only the folders leading down to this worktree.
outer = os.environ.get("OUTER_HOME", "")
path_down = os.path.relpath(os.path.dirname(root), outer).split(os.sep) if outer else []
seen, here = [], outer
for part in path_down:
    names = sorted(os.listdir(here)) if here else []
    seen.append(names)
    here = os.path.join(here, part)
check(bool(outer) and all(names == [part] for names, part in zip(seen, path_down)), f"the owner's home folder is hidden except this worktree {seen}")

# 1. The fake AI server answers from inside the sandbox.
try:
    with urllib.request.urlopen(f"http://127.0.0.1:{port}/v1/models", timeout=5) as r:
        models = [m["id"] for m in json.load(r)["data"]]
    check(r.status == 200 and "fake-small" in models, "fake AI server answers inside the sandbox")
except Exception as e:  # noqa: BLE001
    check(False, f"fake AI server answers inside the sandbox ({e})")

# 2. The fake AI tools come first on PATH and record their calls.
claude = shutil.which("claude") or ""
check(claude.startswith(os.path.join(root, "tests", "fakes", "bin")), f"fake claude is first on PATH ({claude})")
subprocess.run(["claude", "-p", "hello", "--output-format", "stream-json"], capture_output=True, check=False)
calls = [json.loads(l) for l in open(os.environ["FAKE_TOOL_LOG"])] if os.path.exists(os.environ["FAKE_TOOL_LOG"]) else []
check(any(c["tool"] == "claude" and "hello" in c["args"] for c in calls), "fake claude recorded its call")

# 3. The real app window opens on the hidden screen and reports itself.
geo = window_geometry()
check(geo is not None and geo[2] >= 1500 and geo[3] >= 900, f"app window is on the hidden screen at full size {geo}")
p = wait_for(probe_path, lambda p: p.get("ready"))
check(p.get("ready") is True, "app reports the shell is on screen")
check(p.get("heading") == "Crush AI", f"app heading is the Crush AI wordmark ({p.get('heading')!r})")
sh = p.get("shell") or {}
check(geo is not None and (sh.get("x"), sh.get("y"), sh.get("w"), sh.get("h")) == (0, 0, geo[2], geo[3]), f"shell fills the window exactly ({sh} vs window {geo})")
check(p.get("inlineBlocked") is True, f"the window blocks injected inline code ({p.get('inlineBlocked')!r})")
shot(out_dir, "selftest-start")

if broken:
    # Deliberately wrong: proves a failing check is reported as a failure.
    check(p.get("heading") == "Not Crush", "deliberately broken check (must FAIL)")

finish()
