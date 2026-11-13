"""The engine gate (desktop-design 2.9 "Engine gate", P3.1's window check): what
the window shows from launch until it has joined the project's engine, each
failure on the way, the notices after joining (5.2), and the flag effects of
P3.1's rows that show at join (START-006, START-008, START-009).

  connect-1-flags     START-006: -s and -C together exit 1 before any window.
                      START-010, START-011 and START-013 are Rust only (6.5);
                      a NOTE line says so.
  connect-2-join      the run's own app (run-in-sandbox.sh, /tmp/home/proj) is
                      connected: the probe's connection section has the
                      state, client id, engine version and engine{socket, pid}
                      (1.9), and they match the engine's own answers.
  connect-3-notices   a second desktop on the same folder joins and shows no
                      notice; one on sub/ shows an info notice naming the
                      project folder; one started with --yolo=false shows an
                      info notice (engine-design.md:545-550).
  connect-4-starting  a join that starts a new engine (a fresh folder) and is
                      slowed (CRUSH_DESKTOP_CRUSH names a crush that answers
                      1.5 s late): gate.starting turns true during the join
                      and is false once joined.
  connect-5-not-found CRUSH_DESKTOP_CRUSH naming a wrapper that exits 127, then
                      no variable and a PATH without crush: the error crush_not_found
                      and the gate with its Try again button.
  connect-6-too-old   CRUSH_DESKTOP_CRUSH naming a link to tests/fakes/crush-old:
                      the error crush_too_old and the gate with Try again; with
                      the link moved to a crush that has the engine, pressing
                      it joins.
  connect-7-locked    another engine holds a fresh folder (a terminal app on an
                      engine in another runtime folder, which the desktop's
                      ensure cannot see): the error project_locked naming the
                      holder's PID, and the gate with Try again; once that
                      engine is stopped, pressing it joins.
  connect-8-no-folder START-008: --cwd /tmp/home/missing gives the error
                      bad_path, stays on the gate and starts no engine.
  connect-9-data-dir  START-009: -D /tmp/home/data-d is the engine's data_dir.

Each extra app runs with its own probe file (/tmp/probe-<name>.json) and is
closed through its close button's request at the end of its step. The screen
is read from the probe's gate section (1.9, P3.9: shown, starting,
button) and notice section (P3.8: kind, text, action); buttons are pressed
by their heavy rects (gate.button.rect, notice.action.rect). A missing
section is a failed check.

Usage: tests/check.sh p3_connect.py
p3_reconnect.py uses this file's helpers (AppRun, wait_section, press_button, ...).
"""
import ctypes, importlib.util, json, os, shlex, shutil, signal, subprocess, sys, time
from urllib import request as urlrequest
from urllib.error import HTTPError, URLError

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import answer_close_question, check, finish, probe_heavy, read_probe, step, xdo

ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
HOME = "/tmp/home"
PROJ = "/tmp/home/proj"
MAIN_PROBE = "/tmp/probe.json"
# The crush the app runs (tests/fakes/crush-wrap with CHECK_CRUSH), and the
# worktree's own build, which the driver uses for answers it relies on.
REAL_CRUSH = os.path.join(ROOT, "tests", "out", "bin", "crush")
CRUSH = os.environ.get("DESKTOP_CRUSH") or REAL_CRUSH
APP = os.environ.get("DESKTOP_APP") or os.path.join(ROOT, "electron", "crush-desktop")
TMUX = ["tmux", "-L", "crushtest", "-f", "/dev/null"]

TRY_AGAIN = "Try again"
SLOW_SECONDS = 1.5  # connect-4's crush answers this late, so gate.starting shows


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


seed_chat = load("seed_chat", os.path.join(ROOT, "tests", "fakes", "seed_chat.py"))


def flat(text):
    return " ".join((text or "").split())


def set_active_script(port, name):
    """Makes name the fake AI server's active script (6.2: a driver activates
    the script whose turns it uses); answers the script it then plays."""
    req = urlrequest.Request(f"http://127.0.0.1:{port}/_fake/control", data=json.dumps({"script": name}).encode(),
                             method="POST", headers={"Content-Type": "application/json"})
    try:
        with urlrequest.urlopen(req, timeout=5) as resp:
            return json.load(resp).get("script")
    except (HTTPError, URLError, ValueError):
        return None


# The screen, through the probe's gate and notice sections (1.9) -------------

def button_of(report, section, field):
    """(label, rect) of a section's button (1.9: gate.button, notice.action):
    the label from the light fields, the rect from the heavy ones."""
    light = (report.get(section) or {}).get(field) or {}
    heavy = ((report.get("heavy") or {}).get(section) or {}).get(field) or {}
    return light.get("label"), heavy.get("rect")


def wait_section(run, name, test, timeout):
    """Reads run's probe until test(section) holds for the section name.
    Answers (True, section) when it did; (False, the last one seen) when the
    section showed but test never held; (None, None) when no write had the
    section (its piece is not in this build)."""
    end = time.time() + timeout
    last = None
    while True:
        p = read_probe(run.probe, tries=1)
        if p and name in p:
            last = p.get(name) or {}
            if test(last):
                return True, last
        if time.time() >= end:
            return (None, None) if last is None else (False, last)
        time.sleep(0.1)


def press_button(run, section, field, owner, row):
    """A real click on a section's button by its heavy rect (1.9). A failed
    check when the probe has no such section (owner is not in this build),
    when the section has no button with a rect, or when the click cannot be
    made."""
    return press_found(run, probe_heavy(run.probe, 20), section, field, owner, row)


def press_found(run, report, section, field, owner, row):
    """press_button() on a probe_heavy() report already read."""
    if not check(section in report, f"{row}: the probe has a {section} section ({owner})"):
        return False
    label, rect = button_of(report, section, field)
    if not check(label is not None and bool(rect), f"the {section} has its button with a rect ({label}, {rect})"):
        return False
    return check(run.click(rect), f"the {section}'s button is clicked in the app's window (at the middle of {rect})")


def starting_cleared(run, timeout=3):
    """Window: once joined, gate.starting must say false, explicitly (a missing
    field is not false). Answers True or False (None without a gate section)."""
    held, _ = wait_section(run, "gate", lambda g: g.get("starting") is False, timeout)
    return held


def starting_seen(run, timeout=45):
    """Window: reads the probe until it says connected; answers whether any
    write had gate.starting true while joining."""
    seen, end = False, time.time() + timeout
    while time.time() < end:
        p = read_probe(run.probe, tries=1)
        if p:
            seen = seen or (p.get("gate") or {}).get("starting") is True
            if (p.get("connection") or {}).get("state") == "connected":
                break
        time.sleep(0.05)
    return seen


class _ClientMessage(ctypes.Structure):
    _fields_ = [("type", ctypes.c_int), ("serial", ctypes.c_ulong), ("send_event", ctypes.c_int),
                ("display", ctypes.c_void_p), ("window", ctypes.c_ulong), ("message_type", ctypes.c_ulong),
                ("format", ctypes.c_int), ("data", ctypes.c_long * 5)]


class _XEvent(ctypes.Union):
    _fields_ = [("xclient", _ClientMessage), ("pad", ctypes.c_long * 24)]


def ask_close(wid):
    """Sends the window its close request (WM_DELETE_WINDOW), as a window
    manager does for the close button. The hidden screen has no window
    manager, so xdotool windowquit (_NET_CLOSE_WINDOW) reaches no one."""
    x11 = ctypes.cdll.LoadLibrary("libX11.so.6")
    x11.XOpenDisplay.restype = ctypes.c_void_p
    x11.XOpenDisplay.argtypes = [ctypes.c_char_p]
    x11.XInternAtom.restype = ctypes.c_ulong
    x11.XInternAtom.argtypes = [ctypes.c_void_p, ctypes.c_char_p, ctypes.c_int]
    x11.XSendEvent.argtypes = [ctypes.c_void_p, ctypes.c_ulong, ctypes.c_int, ctypes.c_long, ctypes.POINTER(_XEvent)]
    x11.XFlush.argtypes = x11.XCloseDisplay.argtypes = [ctypes.c_void_p]
    dpy = x11.XOpenDisplay(None)
    if not dpy:
        return False
    ev = _XEvent()
    ev.xclient.type = 33  # ClientMessage
    ev.xclient.window = wid
    ev.xclient.message_type = x11.XInternAtom(dpy, b"WM_PROTOCOLS", 0)
    ev.xclient.format = 32
    ev.xclient.data[0] = x11.XInternAtom(dpy, b"WM_DELETE_WINDOW", 0)
    sent = x11.XSendEvent(dpy, wid, 0, 0, ctypes.byref(ev)) != 0
    x11.XFlush(dpy)
    x11.XCloseDisplay(dpy)
    return sent


def alive(pid):
    try:
        with open(f"/proc/{pid}/stat") as f:
            return f.read().rsplit(")", 1)[1].split()[0] != "Z"
    except (OSError, IndexError):
        return False


def windows_of(pid):
    """The visible app windows of process pid, largest first: [(wid, x, y, w, h)]."""
    found = []
    for wid in subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True).stdout.split():
        owner = subprocess.run(["xdotool", "getwindowpid", wid], capture_output=True, text=True).stdout.strip()
        if owner != str(pid):
            continue
        g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True).stdout
        d = dict(line.split("=", 1) for line in g.split() if "=" in line)
        if "WIDTH" in d:
            found.append((int(wid), int(d["X"]), int(d["Y"]), int(d["WIDTH"]), int(d["HEIGHT"])))
    return sorted(found, key=lambda b: -b[3] * b[4])


def processes(match):
    """The live processes whose argument list satisfies match(args)."""
    found = []
    for pid in filter(str.isdigit, os.listdir("/proc")):
        try:
            with open(f"/proc/{pid}/cmdline", "rb") as f:
                args = f.read().decode("utf-8", "replace").rstrip("\0").split("\0")
        except OSError:
            continue
        if args and match(args) and alive(int(pid)):
            found.append(int(pid))
    return found


def pid_with_probe(probe):
    """The app process started with --probe probe (the run's own app is not the driver's child):
    Electron's main process, named crush-desktop when installed and electron in the
    worktree (electron/crush-desktop runs it). The helper, crush-desktop-core, is not it."""
    found = processes(lambda args: os.path.basename(args[0]) in ("crush-desktop", "electron")
                      and any(a == "--probe" and b == probe for a, b in zip(args, args[1:])))
    return found[0] if found else None


class AppRun:
    """One desktop app process with its own probe file."""

    def __init__(self, name, out_dir, cwd=PROJ, args=(), env=None):
        self.name, self.cwd = name, cwd
        self.probe = f"/tmp/probe-{name}.json"
        self.started = time.time()
        full = dict(os.environ, CRUSH_DESKTOP_CRUSH=CRUSH)
        for k, v in (env or {}).items():
            if v is None:
                full.pop(k, None)
            else:
                full[k] = v
        log = open(os.path.join(out_dir, f"app-{name}.log"), "w")
        self.proc = subprocess.Popen([APP, "--probe", self.probe, "--cwd", cwd, *args], env=full, stdout=log, stderr=subprocess.STDOUT)
        self.pid = self.proc.pid

    @classmethod
    def existing(cls, probe, cwd):
        """The run's own app, started by run-in-sandbox.sh."""
        run = cls.__new__(cls)
        run.name, run.cwd, run.probe, run.started, run.proc = "main", cwd, probe, 0.0, None
        run.pid = pid_with_probe(probe)
        return run

    def report(self):
        return read_probe(self.probe, tries=3) or {}

    def section(self, name):
        return self.report().get(name) or {}

    def wait(self, cond, timeout):
        """The first report for which cond holds, or the last one read."""
        end = time.time() + timeout
        p = {}
        while time.time() < end:
            p = read_probe(self.probe, tries=1)
            if p and cond(p):
                return p
            time.sleep(0.1)
        return p

    def invoke(self, command, args=None, timeout=60):
        """Runs one of the app's commands from its page (the probe's invoke)."""
        report = probe_heavy(self.probe, timeout, invoke=[{"command": command, "args": args or {}}])
        return (report.get("invoked") or [{}])[0] or {"ok": False, "error": {"code": "no_answer", "message": "the page did not answer"}}

    def alive(self):
        return bool(self.pid) and alive(self.pid)

    def click(self, rect):
        """A real click at the middle of a viewport rect, in this app's window (raised first)."""
        boxes = windows_of(self.pid)
        if not boxes:
            return False
        wid, x, y, _, _ = boxes[0]
        xdo("windowraise", wid)
        xdo("windowfocus", wid)
        xdo("mousemove", round(x + rect["x"] + rect["w"] / 2), round(y + rect["y"] + rect["h"] / 2))
        xdo("click", 1)
        return True

    def close(self, timeout=10):
        """Closes the window the way its close button does; True when the app
        then exits by itself. One that does not is stopped."""
        boxes = windows_of(self.pid) if self.pid else []
        for box in boxes:
            ask_close(box[0])
        if boxes:
            answer_close_question(self.probe, boxes[0][0], self.alive)
        end = time.time() + timeout
        while time.time() < end and self.alive():
            time.sleep(0.2)
        clean = bool(boxes) and not self.alive()
        for sig in (signal.SIGTERM, signal.SIGKILL):
            if not self.alive():
                break
            os.kill(self.pid, sig)
            end = time.time() + 1
            while time.time() < end and self.alive():
                time.sleep(0.05)
        if self.proc:
            try:
                self.proc.wait(5)
            except subprocess.TimeoutExpired:
                pass
        return clean


def join_outcome(run, timeout):
    """How a join ended: {"state", "error", "engine", "workspace_id", ...}:
    the probe's connection section once it says connected or holds an error
    ("missing" when the probe has no connection section)."""
    p = run.wait(lambda p: (p.get("connection") or {}).get("state") == "connected" or bool((p.get("connection") or {}).get("error")), timeout)
    if "connection" not in p:
        return {"state": None, "error": None, "missing": True, "report": p}
    conn = p["connection"]
    return {"state": conn.get("state"), "error": conn.get("error"), "engine": conn.get("engine") or {},
            "workspace_id": conn.get("workspace_id"), "client_id": conn.get("client_id"),
            "engine_version": conn.get("engine_version"), "fresh_engine": conn.get("fresh_engine"), "report": p}


def error_code(outcome):
    e = outcome.get("error") or {}
    return e.get("code"), f"{e.get('code')}: {e.get('message')}"


def check_gate(run, what, timeout=10):
    """Window: check()s that the gate shows a failure with its button."""
    held, gate = wait_section(run, "gate", lambda g: g.get("shown") is True and g.get("button") is not None, timeout)
    check(held is True, f"{what} ({gate})")


def press_try_again(run, row):
    """The gate's button (Try again): a real click on gate.button.rect (1.9)."""
    return press_button(run, "gate", "button", "P3.9", row)


def state_after(run, timeout):
    """The connection state once the window has joined, or what it says at the timeout."""
    p = run.wait(lambda p: (p.get("connection") or {}).get("state") == "connected", timeout)
    return (p.get("connection") or {}).get("state")


def make_project(path, port):
    """A fresh copy of tests/project at path with git init and a crush.json for the
    fake AI server, as run-in-sandbox.sh makes /tmp/home/proj."""
    if not os.path.exists(path):
        shutil.copytree(os.path.join(ROOT, "tests", "project"), path, ignore=shutil.ignore_patterns("crush.json.in"))
        with open(os.path.join(ROOT, "tests", "project", "crush.json.in")) as f:
            text = f.read().replace("@PORT@", str(port))
        with open(os.path.join(path, "crush.json"), "w") as f:
            f.write(text)
        subprocess.run(["git", "-C", path, "init", "-q"], check=True)
    return path


def wrapper(path, body):
    """Writes an executable shell script at path (a stand-in crush for one step)."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with open(path, "w") as f:
        f.write("#!/bin/sh\n" + body)
    os.chmod(path, 0o755)
    return path


def engine_api(outcome):
    """The engine the join reached, over its socket (the ground truth)."""
    sock = (outcome.get("engine") or {}).get("socket")
    return seed_chat.Engine(sock) if sock else None


def workspace_for(api, path):
    """The id of the engine's workspace for path. Only id and path are read:
    the objects hold the login environment and are never printed."""
    return next((w.get("id") for w in api.call("GET", "/v1/workspaces") or [] if w.get("path") == path), None)


def engines(runtime=None):
    """This sandbox's live engines (crush engine list), or those whose sockets
    are in another runtime folder: {(pid, data_dir)}."""
    env = dict(os.environ, XDG_RUNTIME_DIR=runtime) if runtime else None
    out = subprocess.run([REAL_CRUSH, "engine", "list"], capture_output=True, text=True, timeout=30, env=env).stdout
    try:
        return {(e.get("pid"), e.get("data_dir")) for e in json.loads(out or "[]") or []}
    except ValueError:
        return set()


def no_notice(run, timeout=0.5):
    """Window: True when no notice shows during a short watch."""
    end = time.time() + timeout
    while time.time() < end:
        if (read_probe(run.probe, tries=1) or {}).get("notice"):
            return False
        time.sleep(0.05)
    return True


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    main_app = AppRun.existing(probe_path, PROJ)
    extra = []
    tmux = lambda *args: subprocess.run(TMUX + list(args), capture_output=True, text=True)
    tmux("kill-server")
    try:
        with step("connect-1-flags", out_dir):
            p = subprocess.run([APP, "--probe", "/tmp/probe-flags.json", "-s", "abc", "-C"], capture_output=True, text=True, timeout=30)
            # The app's own last word is the reason, naming both flags, not
            # only that its helper stopped (owner, 2026-11-12).
            said = [line for line in p.stderr.splitlines() if line.startswith("crush-desktop: ")]
            why = said[-1] if said else ""
            check(p.returncode == 1 and "session" in why and "continue" in why, f"START-006: -s with -C exits 1 and says why (exit {p.returncode}: {why[:200]})")
            check(not os.path.exists("/tmp/probe-flags.json"), "and opens no window (it wrote no probe file)")
            print("NOTE START-010 (-d), START-011 (-H) and START-013 (--channels) are Rust only (6.5): "
                  "they show only in the requests the app sends, which P3.1's Rust tests check", flush=True)

        with step("connect-2-join", out_dir):
            joined = join_outcome(main_app, 45)
            if joined.get("missing"):
                check(False, "the probe has a connection section (P3.3's store is not in this build)")
                check(False, "the remaining steps read the window's connection; stopping here")
                return
            connected = check(joined["state"] == "connected", f"the run's app joins the project's engine ({joined['state']}, {joined.get('error')})")
            if not connected:
                check(False, "the remaining steps need a working join; stopping here")
                return
            engine = joined["engine"]
            check(bool(engine.get("socket")) and isinstance(engine.get("pid"), int) and engine.get("pid") > 0,
                  f"its connection names the engine: engine.socket and engine.pid ({engine.get('socket')}, {engine.get('pid')})")
            api = engine_api(joined)
            info = api.call("GET", "/v1/engine") if api else {}
            version = (api.call("GET", "/v1/version") or {}).get("version") if api else None
            check(info.get("pid") == engine.get("pid"), f"the engine at that socket has that pid ({info.get('pid')})")
            check(joined.get("engine_version") == version, f"the engine version is the engine's ({joined.get('engine_version')}, {version})")
            wid = workspace_for(api, PROJ) if api else None
            check(bool(wid) and joined.get("workspace_id") == wid, f"the workspace is the engine's for {PROJ} ({joined.get('workspace_id')}, {wid})")
            check(bool(joined.get("client_id")), "the window has a client id")

        with step("connect-3-notices", out_dir):
            same = AppRun("same-folder", out_dir, PROJ)
            extra.append(same)
            out = join_outcome(same, 45)
            check(out["state"] == "connected", f"a second desktop on the same folder joins ({out['state']}, {out.get('error')})")
            check(no_notice(same), "it shows no notice")
            check(same.close(), "it closes")
            os.makedirs(os.path.join(PROJ, "sub"), exist_ok=True)
            sub = AppRun("subfolder", out_dir, os.path.join(PROJ, "sub"))
            extra.append(sub)
            out = join_outcome(sub, 45)
            check(out["state"] == "connected", f"a desktop on sub/ joins ({out['state']}, {out.get('error')})")
            shown, notice = wait_section(sub, "notice", lambda n: n.get("kind") == "info" and PROJ in flat(n.get("text")), 10)
            check(shown is True, f"its notice line is an info notice naming {PROJ} ({notice})")
            check(sub.close(), "it closes")
            nos = AppRun("yolo-off", out_dir, PROJ, ["--yolo=false"])
            extra.append(nos)
            out = join_outcome(nos, 45)
            check(out["state"] == "connected", f"a desktop started with --yolo=false joins ({out['state']}, {out.get('error')})")
            shown, notice = wait_section(nos, "notice", lambda n: n.get("kind") == "info" and bool(n.get("text")), 10)
            check(shown is True, f"its notice line says the project keeps the first window's choice ({notice})")
            check(nos.close(), "it closes")

        with step("connect-4-starting", out_dir):
            slow = wrapper(os.path.join(HOME, "slow", "crush"),
                           f"# A crush that answers {SLOW_SECONDS} s late, so the join shows its starting line (desktop-design 2.9).\n"
                           f"sleep {SLOW_SECONDS}\nexec {shlex.quote(CRUSH)} \"$@\"\n")
            cold = AppRun("cold", out_dir, make_project(os.path.join(HOME, "cold"), port), env={"CRUSH_DESKTOP_CRUSH": slow})
            extra.append(cold)
            check(starting_seen(cold), "a slowed join that starts a new engine turns gate.starting true")
            check(starting_cleared(cold) is True, "once joined gate.starting is false (the text is gone)")
            check(cold.close(), "it closes")

        with step("connect-5-not-found", out_dir):
            fake = wrapper(os.path.join(HOME, "gone", "crush"), "# A wrapper whose crush is gone (desktop-design 2.9).\nexit 127\n")
            for name, env in (("not-found-127", {"CRUSH_DESKTOP_CRUSH": fake}),
                              ("not-found-path", {"CRUSH_DESKTOP_CRUSH": None, "PATH": "/usr/bin:/bin"})):
                run = AppRun(name, out_dir, PROJ, env=env)
                extra.append(run)
                out = join_outcome(run, 30)
                code, got = error_code(out)
                check(code == "crush_not_found", f"{name}: the error is crush_not_found ({got})")
                check_gate(run, f"{name}: the gate shows the failure with its Try again button")
                check(run.close(), f"{name}: it closes")

        with step("connect-6-too-old", out_dir):
            link = os.path.join(HOME, "switch", "crush")
            os.makedirs(os.path.dirname(link), exist_ok=True)
            if os.path.lexists(link):
                os.remove(link)
            os.symlink(os.path.join(ROOT, "tests", "fakes", "crush-old"), link)
            run = AppRun("too-old", out_dir, PROJ, env={"CRUSH_DESKTOP_CRUSH": link})
            extra.append(run)
            out = join_outcome(run, 30)
            code, got = error_code(out)
            check(code == "crush_too_old", f"the error is crush_too_old ({got})")
            check_gate(run, "the gate shows the failure with its Try again button")
            os.remove(link)
            os.symlink(CRUSH, link)
            if press_try_again(run, "connect-6-too-old: Try again"):
                state = state_after(run, 45)
                check(state == "connected", f"with a crush that has the engine, Try again joins ({state})")
            check(run.close(), "it closes")

        with step("connect-7-locked", out_dir):
            locked = make_project(os.path.join(HOME, "locked"), port)
            other = os.path.join(HOME, "other-run")
            os.makedirs(other, exist_ok=True)
            os.chmod(other, 0o700)  # a private runtime folder, as the engine requires
            holder_tui = tmux("new-session", "-d", "-s", "other", "-x", "200", "-y", "50",
                              f"env XDG_RUNTIME_DIR={other} CRUSH_CLIENT_SERVER=1 {REAL_CRUSH} --cwd {locked}")
            holder = None
            for _ in range(150):
                holder = next((pid for pid, data_dir in engines(other) if (data_dir or "").startswith(locked)), None)
                if holder:
                    break
                time.sleep(0.2)
            check(holder_tui.returncode == 0 and bool(holder),
                  f"a terminal app holds the folder through an engine in another runtime folder (pid {holder})")
            run = AppRun("locked", out_dir, locked)
            extra.append(run)
            out = join_outcome(run, 60)
            code, got = error_code(out)
            check(code == "project_locked" and str(holder) in got, f"the error is project_locked, naming the holder's PID ({got})")
            check_gate(run, "the gate shows the failure with its Try again button")
            tmux("kill-session", "-t", "other")
            # The holder would exit 10 s after its last client; stop it now.
            if holder and alive(holder):
                os.kill(holder, signal.SIGTERM)
            gone = False
            for _ in range(100):
                if not holder or not alive(holder):
                    gone = True
                    break
                time.sleep(0.05)
            check(gone, "the other engine is gone")
            if press_try_again(run, "connect-7-locked: Try again"):
                state = state_after(run, 60)
                check(state == "connected", f"then Try again joins ({state})")
            check(run.close(), "it closes")

        with step("connect-8-no-folder", out_dir):
            before = engines()
            missing_dir = os.path.join(HOME, "missing")
            run = AppRun("no-folder", out_dir, missing_dir)
            extra.append(run)
            out = join_outcome(run, 30)
            code, got = error_code(out)
            check(code == "bad_path", f"START-008: the error is bad_path ({got})")
            check_gate(run, "the gate shows the failure")
            check((run.section("connection").get("state")) != "connected", "the window stays on the gate")
            new = engines() - before  # an earlier step's engine may exit meanwhile (10 s after its last window)
            check(not new, f"no engine starts (new: {sorted(new, key=str)})")
            check(run.close(), "it closes")

        with step("connect-9-data-dir", out_dir):
            data_dir = os.path.join(HOME, "data-d")
            run = AppRun("data-dir", out_dir, make_project(os.path.join(HOME, "proj-d"), port), ["-D", data_dir])
            extra.append(run)
            out = join_outcome(run, 45)
            api = engine_api(out) if out["state"] == "connected" else None
            got = (api.call("GET", "/v1/engine") or {}).get("data_dir") if api else None
            check(got == data_dir, f"START-009: with -D {data_dir} the engine's data_dir is that folder ({out['state']}, {got})")
            check(run.close(), "it closes")
    finally:
        tmux("kill-server")
        for run in extra:
            if run.alive():
                run.close(2)


if __name__ == "__main__":
    main()
    finish()
