"""The engine gate (desktop-design 2.9 "Engine gate", P3.1's window check): what
the window shows from launch until it has joined the project's engine, each
failure on the way, the notices after joining (5.2), and the flag effects of
P3.1's rows that show at join (START-006, START-008, START-009).

  connect-1-flags     START-006: -s and -C together exit 1 before any window
                      with cobra's text. START-010, START-011 and START-013
                      are Rust only (6.5); a NOTE line says so.
  connect-2-join      the run's own app (run-in-sandbox.sh, /tmp/home/proj) is
                      connected: the probe's connection section has the
                      state, client id, engine version and engine{socket, pid}
                      (1.9), and they match the engine's own answers.
  connect-3-notices   a second desktop on the same folder joins quickly, keeps
                      the 400 ms rule and shows no notice; one on sub/ shows
                      "The AI works in /tmp/home/proj, the folder the first
                      window on this project opened."; one started with
                      --yolo=false shows "Crush skips permission prompts on
                      this project, as the first window on it chose."
                      (engine-design.md:545-550).
  connect-4-starting  a join that starts a new engine (a fresh folder) and is
                      slowed to over 1 s (CRUSH_DESKTOP_CRUSH names a crush
                      that answers 1.5 s late): gate.starting ("Starting
                      Crush…") never turns true sooner than 400 ms into the
                      join, turns true during it, and is false once joined.
  connect-5-not-found CRUSH_DESKTOP_CRUSH naming a wrapper that exits 127, then
                      no variable and an empty PATH: the gate's title "Crush
                      not found", its body "Crush Desktop could not find crush
                      at <path>." and "... on your PATH.", and its fix line,
                      which names CRUSH_DESKTOP_CRUSH until phase 5 adds
                      Settings.
  connect-6-too-old   CRUSH_DESKTOP_CRUSH naming a link to tests/fakes/crush-old:
                      title "Crush is too old", body "The crush at <link> is
                      too old for Crush Desktop: it has no engine command.",
                      fix "Update Crush, then press Try again." and the button
                      "Try again"; with the link moved to a crush that has the
                      engine, pressing it joins.
  connect-7-locked    another engine holds a fresh folder (a terminal app on an
                      engine in another runtime folder, which the desktop's
                      ensure cannot see): title "Crush could not open this
                      project", the engine's message as is ("This project is
                      already open in another Crush engine (PID N). ...") and
                      Try again; once that engine has exited, pressing it
                      joins. (2.9's example, a terminal working alone, cannot
                      lock a project on this branch: nothing takes
                      db.ProjectLockAlone yet.)
  connect-8-no-folder START-008: --cwd /tmp/home/missing shows "failed to
                      change directory: chdir /tmp/home/missing: no such file
                      or directory" in the gate's body, stays on the gate and
                      starts no engine.
  connect-9-data-dir  START-009: -D /tmp/home/data-d is the engine's data_dir.

Each extra app runs with its own probe file (/tmp/probe-<name>.json) and is
closed through its close button's request at the end of its step. Texts on
screen are read from the probe's gate section (1.9, P3.9: title, body, fix,
starting, button{label}) and notice section (P3.8: kind, text,
action{label}); buttons are pressed by their heavy rects (gate.button.rect,
notice.action.rect). While a section is absent (its piece is not merged), each
check that needs it prints NOT RUN and fails: it is never a pass.

--stand-in plays the screens' part through the app's own commands (the probe's
invoke: engine_open, and engine_open again for Try again, as connection.svelte.js
does), for builds without P3.3's connection store. It checks the Rust side and
this driver's own steps; texts on screen are not checked.
--self-check runs the gate, notice and 400 ms helpers on made-up probe reports
(no app, no sandbox needed).
Usage: tests/check.sh p3_connect.py [--stand-in]
       python3 -I tests/driver/p3_connect.py --self-check
p3_reconnect.py uses this file's helpers (AppRun, wait_section, press_button, not_run, ...).
"""
import ctypes, importlib.util, json, os, shlex, shutil, signal, subprocess, sys, tempfile, time
from urllib import request as urlrequest
from urllib.error import HTTPError, URLError

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, probe_heavy, read_probe, step, xdo

ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
HOME = "/tmp/home"
PROJ = "/tmp/home/proj"
MAIN_PROBE = "/tmp/probe.json"
# The crush the app runs (tests/fakes/crush-wrap with CHECK_CRUSH), and the
# worktree's own build, which the driver uses for answers it relies on.
REAL_CRUSH = os.path.join(ROOT, "tests", "out", "bin", "crush")
CRUSH = os.environ.get("DESKTOP_CRUSH") or REAL_CRUSH
APP = os.environ.get("DESKTOP_APP") or os.path.join(ROOT, "src-tauri", "target", "release", "crush-desktop")
TMUX = ["tmux", "-L", "crushtest", "-f", "/dev/null"]
STAND_IN = False
NOT_RUN = []

STARTING = "Starting Crush…"
TRY_AGAIN = "Try again"
COBRA = "if any flags in the group [session continue] are set none of the others can be"
SLOW_SECONDS = 1.5  # connect-4's crush answers this late, so its join lasts over 1 s


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


seed_chat = load("seed_chat", os.path.join(ROOT, "tests", "fakes", "seed_chat.py"))


def not_run(row, why):
    """A step that could not run: said plainly, and counted as a failure, so
    neither its step line nor the run says PASS."""
    NOT_RUN.append(row)
    print(f"NOT RUN {row}: {why}", flush=True)
    check(False, f"{row} did not run, so it is not a pass")


def check_all_ran():
    if NOT_RUN:
        print(f"NOTE {len(NOT_RUN)} step(s) did not run: {', '.join(NOT_RUN)}", flush=True)


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

def gate_mismatches(gate, want):
    """The fields of a gate section (1.9: title, body, fix, button{label}) that
    differ from want, as "field 'got'" texts; empty when all match. A wanted
    value is the exact text (spaces folded) or a test of the text; for
    "button" it is the button's label."""
    bad = []
    for field, wanted in want.items():
        got = (gate.get("button") or {}).get("label") if field == "button" else gate.get(field)
        if not (wanted(flat(got)) if callable(wanted) else flat(got) == flat(wanted)):
            bad.append(f"{field} {got!r}")
    return bad


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


def wait_gate(run, want, row, timeout=10):
    """Window: waits until the gate (the probe's gate section, P3.9) shows want
    (see gate_mismatches). Answers the fields that still differ (empty when
    all match), or None with a NOT RUN line when no write had a gate section."""
    held, gate = wait_section(run, "gate", lambda g: not gate_mismatches(g, want), timeout)
    if held is None:
        not_run(row, "the probe has no gate section (P3.9 is not in this build), so the gate's texts could not be read")
        return None
    return [] if held else gate_mismatches(gate, want)


def watch_notice(run, test, row, timeout):
    """Window: watches the notice line (the probe's notice section, P3.8) until
    test(its text) holds. Answers True or False, or None with a NOT RUN line
    when no write had a notice section."""
    held, _ = wait_section(run, "notice", lambda n: test(flat(n.get("text"))), timeout)
    if held is None:
        not_run(row, "the probe has no notice section (P3.8 is not in this build), so the notice line could not be read")
    return held


def press_button(run, section, field, owner, row):
    """A real click on a section's "Try again" button by its heavy rect (1.9).
    NOT RUN when the probe has no such section (owner is not in this build); a
    failed check when the section has no "Try again" button with a rect."""
    report = probe_heavy(run.probe, 20)
    if section not in report:
        not_run(row, f"the probe has no {section} section ({owner} is not in this build), so \"{TRY_AGAIN}\" could not be pressed")
        return False
    label, rect = button_of(report, section, field)
    if not check(label == TRY_AGAIN and bool(rect), f"the {section}'s button is \"{TRY_AGAIN}\" with a rect ({label}, {rect})"):
        return False
    return run.click(rect)


def join_times(samples):
    """From the probe writes read during a join, [(at, connection state, gate
    section or None when the write had none)]: the times (ms) of the first
    write saying connecting, the first with gate.starting true and the first
    saying connected, and whether any write had a gate section."""
    first = {"connecting": None, "starting": None, "connected": None, "gate": False}
    for at, state, gate in samples:
        first["gate"] = first["gate"] or gate is not None
        if state == "connecting" and first["connecting"] is None:
            first["connecting"] = at
        if gate and gate.get("starting") is True and first["starting"] is None:
            first["starting"] = at
        if state == "connected" and first["connected"] is None:
            first["connected"] = at
    return first


def starting_findings(first, long_join=False):
    """The 400 ms rule (2.9) on join_times(): (join ms, [(ok, what)]), or None
    when no write had a gate section, so gate.starting could not be read. The
    probe writes every 200 ms, so the text shown after 400 ms lands at least
    about 200 ms after the first write saying connecting, and one shown at
    once lands with it. A join under 200 ms shows nothing; a long join (slowed
    on purpose) must last 1 s and show the text."""
    if not first["gate"]:
        return None
    if first["connected"] is None or (long_join and first["connecting"] is None):
        return None, [(False, f"the probe saw the join (connecting at {first['connecting']}, connected at {first['connected']})")]
    start = first["connecting"] if first["connecting"] is not None else first["connected"]
    took = first["connected"] - start
    shown = None if first["starting"] is None else first["starting"] - start
    found = [(shown is None or shown >= 150, f"\"{STARTING}\" (gate.starting) never shows sooner than 400 ms into the join "
                                             f"(first shown {shown} ms after connecting; join {took} ms)")]
    if took < 200:
        found.append((first["starting"] is None, f"a join under 400 ms shows nothing (join {took} ms)"))
    if long_join:
        found.append((took >= 1000, f"the slowed join lasts over 1 s ({took} ms)"))
        found.append((first["starting"] is not None, f"a join of {took} ms shows \"{STARTING}\" (gate.starting turns true)"))
    return took, found


def check_starting(first, label, row, long_join=False):
    """check()s starting_findings(); NOT RUN when no write had a gate section.
    Answers the join's ms, or None."""
    result = starting_findings(first, long_join)
    if result is None:
        not_run(row, "the probe has no gate section (P3.9 is not in this build), so gate.starting could not be read")
        return None
    took, found = result
    for ok, what in found:
        check(ok, f"{label}: {what}")
    return took


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
    for wid in subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^Crush$"], capture_output=True, text=True).stdout.split():
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
    """The app process started with --probe probe (the run's own app is not the driver's child)."""
    found = processes(lambda args: args[0].endswith("crush-desktop") and any(a == "--probe" and b == probe for a, b in zip(args, args[1:])))
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
        end = time.time() + timeout
        while time.time() < end and self.alive():
            time.sleep(0.2)
        clean = bool(boxes) and not self.alive()
        for sig in (signal.SIGTERM, signal.SIGKILL):
            if not self.alive():
                break
            os.kill(self.pid, sig)
            time.sleep(1)
        if self.proc:
            try:
                self.proc.wait(5)
            except subprocess.TimeoutExpired:
                pass
        return clean


def join_outcome(run, timeout):
    """How a join ended: {"state", "error", "engine", "workspace_id", ...}.
    Window: the probe's connection section once it says connected or holds an
    error ("missing" when the probe has no connection section). Stand-in:
    engine_open's answer."""
    if STAND_IN:
        a = run.invoke("engine_open", {"cwd": run.cwd}, timeout)
        if a.get("ok"):
            v = a.get("value") or {}
            ws = v.get("workspace") or {}
            return {"state": "connected", "error": None, "engine": v.get("engine") or {}, "workspace_id": ws.get("id"),
                    "workspace": ws, "cwd": v.get("cwd"), "sent_yolo": v.get("sent_yolo"), "client_id": v.get("client_id"),
                    "engine_version": (v.get("version") or {}).get("version")}
        return {"state": "failed", "error": a.get("error") or {}}
    p = run.wait(lambda p: (p.get("connection") or {}).get("state") == "connected" or bool((p.get("connection") or {}).get("error")), timeout)
    if "connection" not in p:
        return {"state": None, "error": None, "missing": True, "report": p}
    conn = p["connection"]
    return {"state": conn.get("state"), "error": conn.get("error"), "engine": conn.get("engine") or {},
            "workspace_id": conn.get("workspace_id"), "client_id": conn.get("client_id"),
            "engine_version": conn.get("engine_version"), "fresh_engine": conn.get("fresh_engine"), "report": p}


def error_is(outcome, code, message):
    e = outcome.get("error") or {}
    return e.get("code") == code and e.get("message") == message, f"{e.get('code')}: {e.get('message')}"


def check_gate(run, want, what, row):
    """Window: check()s that the gate shows want (wait_gate); NOT RUN without a gate section."""
    bad = wait_gate(run, want, row)
    if bad is not None:
        check(not bad, f"{what}" + (f" (differs: {'; '.join(bad)})" if bad else ""))


def press_try_again(run, row):
    """The gate's "Try again". Window: a real click on gate.button.rect (1.9);
    NOT RUN when the probe has no gate section. Stand-in: engine_open again,
    what the button does after a failed join (connection.svelte.js retry)."""
    if STAND_IN:
        run.invoke("engine_open", {"cwd": run.cwd})
        return True
    return press_button(run, "gate", "button", "P3.9", row)


def state_after(run, timeout):
    """The connection state once the window has joined, or what it says at the timeout."""
    if STAND_IN:
        return join_outcome(run, timeout)["state"]  # a second engine_open answers the same join
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


def sample_join(run, timeout=45):
    """Window: reads the probe every 50 ms until it says connected; answers
    join_times() of the writes read."""
    samples, end = [], time.time() + timeout
    while time.time() < end:
        p = read_probe(run.probe, tries=1)
        if p:
            state = (p.get("connection") or {}).get("state")
            samples.append((p.get("at"), state, (p.get("gate") or {}) if "gate" in p else None))
            if state == "connected":
                break
        time.sleep(0.05)
    return join_times(samples)


def self_check():
    """--self-check: the helpers that read the gate and notice sections and the
    400 ms rule, on made-up probe reports. No app runs."""
    body = "The crush at /tmp/home/switch/crush is too old for Crush Desktop: it has no engine command."
    gate = {"state": "failed", "title": "Crush is too old", "body": body, "fix": "Update Crush, then press Try again.",
            "starting": False, "button": {"label": TRY_AGAIN}}
    want = {"title": "Crush is too old", "body": body, "fix": "Update Crush, then press Try again.", "button": TRY_AGAIN}
    check(gate_mismatches(gate, want) == [], "gate: the right title, body, fix line and button match")
    check(gate_mismatches(dict(gate, body=body.replace(" for ", "\n for ") + " "), want) == [], "gate: spaces and line breaks are folded")
    check(gate_mismatches(dict(gate, title="Crush not found"), want) == ["title 'Crush not found'"], "gate: another title differs")
    check(gate_mismatches(dict(gate, body=body.replace("too old", "old")), want) != [], "gate: a reworded body differs")
    check(gate_mismatches(dict(gate, button=None), want) == ["button None"], "gate: a gate with no button differs")
    names_variable = lambda t: t.startswith("Install Crush") and "CRUSH_DESKTOP_CRUSH" in t
    check(gate_mismatches({"fix": "Install Crush, or set CRUSH_DESKTOP_CRUSH to its path."}, {"fix": names_variable}) == []
          and gate_mismatches({"fix": "Install Crush, or set the crush path in Settings."}, {"fix": names_variable}) != [],
          "gate: a test of the fix line passes only on a line naming CRUSH_DESKTOP_CRUSH")

    rect = {"x": 600, "y": 400, "w": 96, "h": 32}
    check(button_of({"gate": {"button": {"label": TRY_AGAIN}}, "heavy": {"gate": {"button": {"rect": rect}}}}, "gate", "button") == (TRY_AGAIN, rect),
          "button: the label comes from gate.button, the rect from heavy gate.button.rect")
    check(button_of({"notice": {"action": {"label": TRY_AGAIN}}, "heavy": {"notice": {"action": {"rect": rect}}}}, "notice", "action") == (TRY_AGAIN, rect),
          "button: the notice's comes from notice.action and heavy notice.action.rect")
    check(button_of({"gate": {"button": None}, "heavy": {"gate": {"button": None}}}, "gate", "button") == (None, None), "button: none when the gate has none")
    check(button_of({"gate": {"button": {"label": TRY_AGAIN}}}, "gate", "button") == (TRY_AGAIN, None), "button: no rect without heavy fields")

    idle, shown = {"starting": False}, {"starting": True}
    passes = lambda result: result is not None and all(ok for ok, _ in result[1])
    fast = lambda *writes: starting_findings(join_times(list(writes)))
    slow = lambda *writes: starting_findings(join_times(list(writes)), long_join=True)
    check(fast((0, "connecting", None), (200, "connected", None)) is None, "400 ms rule: no gate section in any write is not a pass (NOT RUN)")
    check(passes(fast((0, "connecting", idle), (150, "connected", idle))), "400 ms rule: a 150 ms join that never shows the text passes")
    check(not passes(fast((0, "connecting", shown), (150, "connected", idle))), "400 ms rule: a 150 ms join that shows the text at once fails")
    check(passes(fast((0, "connected", idle))), "400 ms rule: a join over before the first write, the text never shown, passes")
    check(passes(slow((0, "connecting", idle), (200, "connecting", idle), (400, "connecting", shown), (3000, "connected", idle))),
          "400 ms rule: a 3 s join showing the text from 400 ms passes")
    check(not passes(slow((0, "connecting", shown), (3000, "connected", idle))), "400 ms rule: a 3 s join showing the text at once fails")
    check(not passes(slow((0, "connecting", idle), (1600, "connecting", idle), (3000, "connected", idle))),
          "400 ms rule: a 3 s join where gate.starting never turns true fails")
    check(not passes(slow((0, "connecting", idle), (400, "connecting", shown), (600, "connected", idle))), "400 ms rule: a slowed join under 1 s fails")
    check(not passes(slow((0, "connected", idle))), "400 ms rule: a slowed join the probe never saw connecting fails")

    with tempfile.TemporaryDirectory() as folder:
        run = type("Run", (), {"probe": os.path.join(folder, "probe.json")})()

        def write(report):
            with open(run.probe, "w") as f:
                json.dump(report, f)
        write({"gate": gate})
        check(wait_section(run, "gate", lambda g: not gate_mismatches(g, want), 0.3)[0] is True, "probe file: a gate that shows the texts answers True")
        check(wait_section(run, "gate", lambda g: g.get("title") == "Crush not found", 0.3)[0] is False, "probe file: a gate that never does answers False")
        write({"connection": {"state": "connecting"}})
        check(wait_section(run, "gate", lambda g: True, 0.3) == (None, None), "probe file: no gate section answers None (NOT RUN)")


def main():
    global STAND_IN
    if sys.argv[1:] == ["--self-check"]:
        return self_check()
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    STAND_IN = "--stand-in" in sys.argv[4:]
    if STAND_IN:
        print("NOTE --stand-in: the screens' part is played through the app's commands; texts on screen are not checked", flush=True)
    main_app = AppRun.existing(probe_path, PROJ)
    extra = []
    tmux = lambda *args: subprocess.run(TMUX + list(args), capture_output=True, text=True)
    tmux("kill-server")
    try:
        with step("connect-1-flags", out_dir):
            p = subprocess.run([APP, "--probe", "/tmp/probe-flags.json", "-s", "abc", "-C"], capture_output=True, text=True, timeout=30)
            check(p.returncode == 1 and p.stderr.strip().startswith(COBRA),
                  f"START-006: -s with -C exits 1 with cobra's text (exit {p.returncode}: {p.stderr.strip()[:120]})")
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
            if STAND_IN:
                ws = joined.get("workspace") or {}
                check("env" not in ws and "config" not in ws, f"the join's workspace has no env and no config ({sorted(ws)})")

        with step("connect-3-notices", out_dir):
            same = AppRun("same-folder", out_dir, PROJ)
            extra.append(same)
            if STAND_IN:
                out = join_outcome(same, 45)
                check(out["state"] == "connected" and (out.get("workspace") or {}).get("path") == out.get("cwd") == PROJ,
                      f"a second desktop on the same folder joins with its own folder as the AI's ({(out.get('workspace') or {}).get('path')})")
            else:
                check_starting(sample_join(same), "a second desktop on the same folder", "connect-3-notices: its join's 400 ms rule")
                shown = watch_notice(same, lambda t: t.startswith("The AI works in"), "connect-3-notices: no folder notice", 3)
                if shown is not None:
                    check(not shown, "it shows no folder notice")
            check(same.close(), "it closes")
            os.makedirs(os.path.join(PROJ, "sub"), exist_ok=True)
            sub = AppRun("subfolder", out_dir, os.path.join(PROJ, "sub"))
            extra.append(sub)
            out = join_outcome(sub, 45)
            path_notice = f"The AI works in {PROJ}, the folder the first window on this project opened."
            if STAND_IN:
                check(out["state"] == "connected" and (out.get("workspace") or {}).get("path") == PROJ and out.get("cwd") == os.path.join(PROJ, "sub"),
                      f"a desktop on sub/ joins the top folder's workspace, whose path differs from its own "
                      f"({(out.get('workspace') or {}).get('path')} vs {out.get('cwd')})")
            else:
                check(out["state"] == "connected", f"a desktop on sub/ joins ({out['state']}, {out.get('error')})")
                shown = watch_notice(sub, lambda t: t == path_notice, "connect-3-notices: the folder notice", 10)
                if shown is not None:
                    check(shown, f"its notice line shows \"{path_notice}\"")
            check(sub.close(), "it closes")
            nos = AppRun("yolo-off", out_dir, PROJ, ["--yolo=false"])
            extra.append(nos)
            out = join_outcome(nos, 45)
            yolo_notice = "Crush skips permission prompts on this project, as the first window on it chose."
            if STAND_IN:
                check(out["state"] == "connected" and (out.get("workspace") or {}).get("yolo") is True and out.get("sent_yolo") is False,
                      f"a desktop started with --yolo=false asks for false and the project keeps true "
                      f"(sent {out.get('sent_yolo')}, project {(out.get('workspace') or {}).get('yolo')})")
            else:
                check(out["state"] == "connected", f"a desktop started with --yolo=false joins ({out['state']}, {out.get('error')})")
                shown = watch_notice(nos, lambda t: t == yolo_notice, "connect-3-notices: the yolo notice", 10)
                if shown is not None:
                    check(shown, f"its notice line shows \"{yolo_notice}\"")
            check(nos.close(), "it closes")

        with step("connect-4-starting", out_dir):
            slow = wrapper(os.path.join(HOME, "slow", "crush"),
                           f"# A crush that answers {SLOW_SECONDS} s late, so the join lasts over 1 s (desktop-design 2.9).\n"
                           f"sleep {SLOW_SECONDS}\nexec {shlex.quote(CRUSH)} \"$@\"\n")
            cold = AppRun("cold", out_dir, make_project(os.path.join(HOME, "cold"), port), env={"CRUSH_DESKTOP_CRUSH": slow})
            extra.append(cold)
            if STAND_IN:
                started = time.time()
                out = join_outcome(cold, 45)
                check(out["state"] == "connected" and out["engine"].get("started") is True,
                      f"a fresh folder's slowed join starts a new engine ({out['state']}, started {out['engine'].get('started')}, {time.time() - started:.1f} s)")
            else:
                took = check_starting(sample_join(cold), "a slowed join that starts a new engine", "connect-4-starting: gate.starting", long_join=True)
                if took is not None:
                    gone, _ = wait_section(cold, "gate", lambda g: g.get("starting") is not True, 3)
                    check(gone is not False, "once joined gate.starting is false (the text is gone)")
            check(cold.close(), "it closes")

        with step("connect-5-not-found", out_dir):
            fake = wrapper(os.path.join(HOME, "gone", "crush"), "# A wrapper whose crush is gone (desktop-design 2.9).\nexit 127\n")
            for name, env, message in (
                    ("not-found-127", {"CRUSH_DESKTOP_CRUSH": fake}, f"Crush Desktop could not find crush at {fake}."),
                    ("not-found-path", {"CRUSH_DESKTOP_CRUSH": None, "PATH": ""}, "Crush Desktop could not find crush on your PATH.")):
                run = AppRun(name, out_dir, PROJ, env=env)
                extra.append(run)
                out = join_outcome(run, 30)
                ok, got = error_is(out, "crush_not_found", message)
                check(ok, f"{name}: the error is crush_not_found, \"{message}\" ({got})")
                if not STAND_IN:
                    check_gate(run, {"title": "Crush not found", "body": message,
                                     "fix": lambda t: t.startswith("Install Crush") and "CRUSH_DESKTOP_CRUSH" in t},
                               f"{name}: the gate shows the title, the message and the fix line naming CRUSH_DESKTOP_CRUSH",
                               f"connect-5-not-found: {name}'s gate")
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
            message = f"The crush at {link} is too old for Crush Desktop: it has no engine command."
            ok, got = error_is(out, "crush_too_old", message)
            check(ok, f"the error is crush_too_old, \"{message}\" ({got})")
            if not STAND_IN:
                check_gate(run, {"title": "Crush is too old", "body": message, "fix": "Update Crush, then press Try again.", "button": TRY_AGAIN},
                           "the gate shows the title, the message, the fix line and Try again", "connect-6-too-old: the gate")
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
            held = (f"This project is already open in another Crush engine (PID {holder}). "
                    "Close its windows, or wait a minute, then try again.")
            ok, got = error_is(out, "project_locked", held)
            check(ok, f"the error is project_locked with the engine's message as is ({got})")
            if not STAND_IN:
                check_gate(run, {"title": "Crush could not open this project", "body": held, "button": TRY_AGAIN},
                           "the gate shows the title, the engine's message and Try again", "connect-7-locked: the gate")
            tmux("kill-session", "-t", "other")
            gone = False
            for _ in range(225):  # the engine exits 10 s after its last client's hold ends
                if not holder or not alive(holder):
                    gone = True
                    break
                time.sleep(0.2)
            check(gone, "with the terminal app closed, the other engine exits")
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
            message = f"failed to change directory: chdir {missing_dir}: no such file or directory"
            ok, got = error_is(out, "bad_path", message)
            check(ok, f"START-008: the error is \"{message}\" ({got})")
            if not STAND_IN:
                check_gate(run, {"body": message}, "the gate's body shows it", "connect-8-no-folder: the gate")
                check((run.section("connection").get("state")) != "connected", "the window stays on the gate")
            time.sleep(1)
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
    check_all_ran()
    finish()
