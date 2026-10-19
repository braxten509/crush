"""The speed bar (desktop-design 2.9, PLAN.md:156): script stream2m's two-minute
reply (about 20 words a second in chunks of 1-4 words, 12 tool calls, two
Mermaid fences, a 30-row table) on a chat that already has 300 items.

Setup: seed_chat.py seeds the chat through the engine's API until the
desktop's timeline has 300 items, and the project is marked initialized so no
start-up question gets in the way. The engine is the one in the probe's
connection.engine.socket (1.9); crush engine ensure finds it when the field
is missing (a NOTE line says so). tests/run-in-sandbox.sh has already made
Ctrl+Alt+F12 reach the window (the hidden screen's keymap gives it to the X
server's console switch), and the screen keeps that change for the whole run
(-noreset in tests/sandbox.sh). Then three times (three launches, the same
script and chunking):
  1. launch the app with --probe and -s on the chat, press Ctrl+Alt+F12 (the
     probe's frame mark starts the measured stretch, 1.9), send the prompt;
  2. every 500 ms during the stream type one letter into the field
     (xdotool type --delay 0), and every 10 s press BackSpace ten times;
  3. when the run is over (probe chat.busy false), press Ctrl+Alt+F12 again to
     end the stretch, then read the probe's frames.
A run passes when p95 of frame times is 20 ms or less, at most one frame is
over 50 ms, and at most one key-to-next-frame time is over 50 ms (and the
stretch measured frames and keys over the whole stream). The check passes when
all three runs pass and no two runs have a frame over 50 ms within 250 ms of
the same time after the mark. tests/out/p3_speed/report.json lists every
run's p95, max and over-50 times.

Usage: tests/check.sh p3_speed.py
       tests/check.sh p3_speed.py --meter-check          (the driver's keys and the frame meter only; no engine)
       python3 -I tests/driver/p3_speed.py --self-check   (the verdict rules on made-up runs; no app)
"""
import ctypes, importlib.util, itertools, json, os, subprocess, sys, time
from urllib import request as urlrequest

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, fake_ai_requests, finish, key, read_probe, step, type_text, wait_for, xdo

RUNS = 3
P95_MS = 20
OVER_MS = 50
NEAR_MS = 250
ITEMS = 300
MIN_SECONDS = 100  # the stream is planned at about 120 s (fake_ai_server.py --self-test)
PROMPT = "Stream for two minutes"
PROJ = "/tmp/home/proj"
ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
CRUSH = os.environ.get("DESKTOP_CRUSH") or os.path.join(ROOT, "tests", "out", "bin", "crush")
APP = os.environ.get("DESKTOP_APP") or os.path.join(ROOT, "src-tauri", "target", "release", "crush-desktop")


def verdict(runs):
    """(passed, reasons, per-run rows) for the frames sections read after each
    run's stretch ended (each with the driver's "seconds" added)."""
    reasons, rows = [], []
    for i, f in enumerate(runs, 1):
        row = {"run": i, "p95_ms": f.get("p95_ms"), "max_ms": f.get("max_ms"), "count": f.get("count"),
               "over50_at": list(f.get("over50_at") or []), "input_over50_at": list(f.get("input_over50_at") or []),
               "input_count": f.get("input_count"), "seconds": f.get("seconds")}
        bad = []
        if f.get("measuring"):
            bad.append("the stretch had not ended when it was read")
        if not f.get("count"):
            bad.append("no frames were measured")
        if not f.get("input_count"):
            bad.append("no keys were measured")
        if (f.get("seconds") or 0) < MIN_SECONDS:
            bad.append(f"the stretch lasted {f.get('seconds')} s, under the stream's {MIN_SECONDS} s")
        if (f.get("p95_ms") or 0) > P95_MS:
            bad.append(f"p95 {f.get('p95_ms')} ms is over {P95_MS} ms")
        if len(row["over50_at"]) > 1:
            bad.append(f"{len(row['over50_at'])} frames over {OVER_MS} ms")
        if len(row["input_over50_at"]) > 1:
            bad.append(f"{len(row['input_over50_at'])} key-to-frame times over {OVER_MS} ms")
        row["passed"], row["why"] = not bad, bad
        rows.append(row)
        reasons += [f"run {i}: {b}" for b in bad]
    if len(runs) < RUNS:
        reasons.append(f"{len(runs)} of {RUNS} runs were measured")
    for a, b in itertools.combinations(rows, 2):
        near = [(x, y) for x in a["over50_at"] for y in b["over50_at"] if abs(x - y) <= NEAR_MS]
        if near:
            reasons.append(f"runs {a['run']} and {b['run']} both have a frame over {OVER_MS} ms near the same time "
                           f"({near[0][0]} and {near[0][1]} ms after the mark)")
    return not reasons, reasons, rows


def self_check():
    good = {"measuring": False, "count": 7200, "p95_ms": 16.7, "max_ms": 41.0, "over50_at": [], "input_over50_at": [],
            "input_count": 260, "seconds": 121.0}

    def run(**changes):
        return dict(good, **changes)
    cases = [
        ("three good runs pass", [good] * 3, True),
        ("a p95 of exactly 20 ms passes", [run(p95_ms=20.0)] * 3, True),
        ("a run with p95 over 20 ms fails", [good, run(p95_ms=20.1), good], False),
        ("one frame over 50 ms in each run, far apart, passes", [run(over50_at=[1000]), run(over50_at=[5000]), run(over50_at=[90000])], True),
        ("two frames over 50 ms in one run fail", [run(over50_at=[1000, 60000]), good, good], False),
        ("one slow key in each run passes", [run(input_over50_at=[700])] * 3, True),
        ("two slow keys in one run fail", [run(input_over50_at=[100, 900]), good, good], False),
        ("frames over 50 ms in two runs 250 ms apart fail", [run(over50_at=[10000]), run(over50_at=[10250]), good], False),
        ("frames over 50 ms in two runs 251 ms apart pass", [run(over50_at=[10000]), run(over50_at=[10251]), good], True),
        ("frames over 50 ms in runs 1 and 3 at the same time fail", [run(over50_at=[4000]), good, run(over50_at=[3900])], False),
        ("two runs are not enough", [good] * 2, False),
        ("a run read while its stretch still runs fails", [run(measuring=True), good, good], False),
        ("a run with no frames fails", [run(count=0), good, good], False),
        ("a run with no keys fails", [run(input_count=0), good, good], False),
        ("a stream that stopped early fails", [run(seconds=30.0), good, good], False),
    ]
    for name, runs, want in cases:
        passed, reasons, _ = verdict(runs)
        check(passed == want, f"verdict: {name}" + (f" ({'; '.join(reasons)})" if reasons and passed != want else ""))
    finish()


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


SEED_CHAT = os.path.join(ROOT, "tests", "fakes", "seed_chat.py")
seed_chat = load("seed_chat", SEED_CHAT)


def engine_socket(probe_path):
    """The engine's socket from the probe's connection.engine (1.9, 6.4); when
    the field is missing, crush engine ensure answers the same engine."""
    conn = (read_probe(probe_path, tries=3) or {}).get("connection") or {}
    sock = (conn.get("engine") or {}).get("socket")
    if sock:
        return sock
    print("NOTE the probe has no connection.engine.socket; crush engine ensure finds the engine", flush=True)
    return seed_chat.ensure(CRUSH, PROJ)


class App:
    """One launch of the desktop app on the chat, read through its probe."""

    def __init__(self, probe_path, out_dir):
        self.probe_path, self.out_dir, self.proc = probe_path, out_dir, None

    def section(self, name):
        return (read_probe(self.probe_path, tries=3) or {}).get(name) or {}

    def launch(self, sid, n):
        since = time.time()
        log = open(os.path.join(self.out_dir, f"app-run{n}.log"), "w")
        self.proc = subprocess.Popen([APP, "--probe", self.probe_path, "--cwd", PROJ, "-s", sid],
                                     env=dict(os.environ, CRUSH_DESKTOP_CRUSH=CRUSH), stdout=log, stderr=subprocess.STDOUT)
        return since

    def wait(self, cond, timeout):
        p = wait_for(self.probe_path, cond, timeout)
        return bool(p) and cond(p)

    def focus_field(self):
        for _ in range(4):
            if self.section("composer").get("focused"):
                return True
            key("Tab")
            time.sleep(0.3)
        return bool(self.section("composer").get("focused"))

    def clear_field(self):
        if self.section("composer").get("value"):
            key("ctrl+shift+a", "BackSpace")

    def stop(self):
        if self.proc and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(10)
            except subprocess.TimeoutExpired:
                self.proc.kill()
                self.proc.wait(5)


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


def free_ctrl_alt_f12():
    """The hidden screen's keyboard map gives Ctrl+Alt+F12 the X server's
    console switch (XF86Switch_VT_12), which the server keeps, so the page
    never sees the probe's mark key. Rewrites F12's key on this screen alone
    (the sandbox's own X server) as plain F12 at every level.
    tests/run-in-sandbox.sh makes the same change for every driver, and the
    screen keeps it for the whole run (-noreset), so this is only the meter
    check's own guard; the speed runs rely on the harness."""
    table = subprocess.run(["xmodmap", "-pke"], capture_output=True, text=True).stdout
    codes = [line.split()[1] for line in table.splitlines() if line.startswith("keycode") and "= F12" in line]
    for code in codes:
        subprocess.run(["xmodmap", "-e", f"keycode {code} = " + " ".join(["F12"] * 7)], check=True)
    after = subprocess.run(["xmodmap", "-pke"], capture_output=True, text=True).stdout
    return bool(codes) and "Switch_VT_12" not in after


def close_windows():
    """Closes every app window on the hidden screen and waits for the apps to exit."""
    pids = set()
    for wid in subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^Crush$"], capture_output=True, text=True).stdout.split():
        pid = subprocess.run(["xdotool", "getwindowpid", wid], capture_output=True, text=True).stdout.strip()
        if pid.isdigit():
            pids.add(int(pid))
        ask_close(int(wid))
    end = time.time() + 10
    while time.time() < end and any(os.path.exists(f"/proc/{p}") for p in pids):
        time.sleep(0.2)
    return bool(pids) and not any(os.path.exists(f"/proc/{p}") for p in pids)


def stream_steps_served(since):
    """The stream2m steps the fake server answered for the prompt since a time."""
    return {e.get("step") for e in fake_ai_requests() if e.get("at", 0) >= since and e.get("kind") == "turn"
            and e.get("script") == "stream2m" and e.get("turn") == 0 and e.get("status") == 200}


def seed(args, sock, timeout=600):
    """Runs seed_chat.py on the engine at sock; returns its JSON line, or None
    (with a FAIL line)."""
    p = subprocess.run([sys.executable, "-I", SEED_CHAT, "--cwd", PROJ, "--socket", sock, *args],
                       capture_output=True, text=True, timeout=timeout)
    if not check(p.returncode == 0, f"seed_chat.py {' '.join(args)}: {p.stdout.strip()} {p.stderr.strip()[-300:]}"):
        return None
    return json.loads(p.stdout)


def measure(app, sid, n):
    """One launch: open the chat, measure the stream, return the frames section
    (with "seconds"), or None when a step before the measurement failed."""
    since = app.launch(sid, n)
    ok = app.wait(lambda p: (p.get("connection") or {}).get("state") == "connected" and p.get("at", 0) >= since * 1000
                  and (p.get("chat") or {}).get("id") == sid, 60)
    if not check(ok, f"run {n}: the app opens the seeded chat"):
        return None
    # A timeline item is not always one engine message, so the chat grows
    # until the timeline itself has the items (the app shows new turns live).
    for _ in range(6):
        if app.wait(lambda p: len((p.get("chat") or {}).get("items") or []) >= ITEMS, 15):
            break
        seed(["--session", sid, "--turns", "25"], engine_socket(app.probe_path))
    loaded = app.wait(lambda p: len((p.get("chat") or {}).get("items") or []) >= ITEMS, 15)
    count = len(app.section("chat").get("items") or [])
    if not check(loaded, f"run {n}: the timeline has at least {ITEMS} items ({count})"):
        return None
    time.sleep(2)  # let the first paint settle; the stretch measures the stream
    if not check(app.focus_field(), f"run {n}: the typing box has focus"):
        return None
    app.clear_field()
    key("ctrl+alt+F12")
    if not check(app.wait(lambda p: (p.get("frames") or {}).get("measuring") is True, 3), f"run {n}: Ctrl+Alt+F12 starts the measured stretch"):
        return None
    started = time.time()
    type_text(PROMPT, 0)
    key("Return")
    busy = app.wait(lambda p: (p.get("chat") or {}).get("busy") is True, 15)
    check(busy, f"run {n}: the prompt is sent and the chat turns busy")
    if busy:
        type_while(lambda: app.section("chat").get("busy") is not False and time.time() - started < 300, time.time())
    seconds = round(time.time() - started, 1)
    key("ctrl+alt+F12")
    ended = app.wait(lambda p: (p.get("frames") or {}).get("measuring") is False, 5)
    frames = dict(app.section("frames"), seconds=seconds)
    check(ended, f"run {n}: Ctrl+Alt+F12 ends the stretch after {seconds} s")
    served = stream_steps_served(since)
    check(served == set(range(13)), f"run {n}: the whole stream played (steps {sorted(s for s in served if s is not None)})")
    app.clear_field()
    close_windows()
    app.stop()
    return frames


def type_while(running, started):
    """The input sample of step 2 while running() holds: one letter every
    500 ms, ten BackSpaces every 10 s."""
    last_key = last_wipe = started
    while running():
        now = time.time()
        if now - last_wipe >= 10:
            xdo("key", "--delay", "0", *["BackSpace"] * 10)
            last_wipe = now
        if now - last_key >= 0.5:
            type_text("x", 0)
            last_key = now
        time.sleep(0.05)


def meter_check(probe_path, out_dir):
    """--meter-check: the driver's keys against the window's frame meter for
    12 s, without the engine (the stretch, the letters, the BackSpaces)."""
    with step("speed-meter", out_dir):
        p = wait_for(probe_path, lambda p: "frames" in p, 30)
        check("frames" in p, "the probe reports the frame meter")
        check(free_ctrl_alt_f12(), "Ctrl+Alt+F12 reaches the window (the hidden screen's console switch key is turned into F12)")
        time.sleep(1)
        key("ctrl+alt+F12")
        started = time.time()
        p = wait_for(probe_path, lambda p: (p.get("frames") or {}).get("measuring") is True, 3)
        check((p.get("frames") or {}).get("measuring") is True, "Ctrl+Alt+F12 starts the measured stretch")
        type_while(lambda: time.time() - started < 12, started)
        key("ctrl+alt+F12")
        p = wait_for(probe_path, lambda p: (p.get("frames") or {}).get("measuring") is False, 3)
        f = p.get("frames") or {}
        check(f.get("measuring") is False and f.get("frames_mark", 0) > 0, "Ctrl+Alt+F12 ends it and the mark is kept")
        check(f.get("count", 0) > 100 and f.get("input_count", 0) >= 30,
              f"the stretch counted frames and the typed keys ({f.get('count')} frames, {f.get('input_count')} keys, "
              f"p95 {f.get('p95_ms')} ms, max {f.get('max_ms')} ms)")


def main():
    if sys.argv[1:] == ["--self-check"]:
        self_check()
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    if sys.argv[4:] == ["--meter-check"]:
        return meter_check(probe_path, out_dir)
    app = App(probe_path, out_dir)
    runs, sid = [], None
    try:
        with step("speed-seed", out_dir):
            req = urlrequest.Request(f"http://127.0.0.1:{port}/_fake/control", data=json.dumps({"script": "stream2m"}).encode(),
                                     method="POST", headers={"Content-Type": "application/json"})
            with urlrequest.urlopen(req, timeout=5) as resp:
                fake = json.load(resp)
            check(fake.get("script") == "stream2m", f"the fake server plays script stream2m, whose turns this check uses (6.2; {fake.get('script')})")
            scale = fake.get("time_scale")
            check(scale == 1, f"the fake server streams in real time (time scale {scale})")
            p = wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 45)
            connected = check((p.get("connection") or {}).get("state") == "connected", "the first app joined the project's engine")
            sock = engine_socket(probe_path) if connected else None
            info = seed(["--items", str(ITEMS), "--title", "Speed check"], sock) if connected else None
            if info:
                sid = info["session_id"]
                api = seed_chat.Engine(sock)
                api.call("POST", f"/v1/workspaces/{info['workspace_id']}/project/init")
                needs = api.call("GET", f"/v1/workspaces/{info['workspace_id']}/project/needs-init").get("needs_init")
                check(needs is False, "the project is marked initialized, so no start-up question shows")
            check(close_windows(), "the first app closes; each run launches its own")
        if not sid:
            check(False, "the runs need the app on the engine and a seeded chat; stopping here")
            return
        for n in range(1, RUNS + 1):
            with step(f"speed-run-{n}", out_dir):
                frames = measure(app, sid, n)
                if frames is not None:
                    runs.append(frames)
                    print(f"NOTE run {n}: p95 {frames.get('p95_ms')} ms, max {frames.get('max_ms')} ms, "
                          f"over 50 ms at {frames.get('over50_at')}, slow keys at {frames.get('input_over50_at')}, "
                          f"{frames.get('count')} frames, {frames.get('input_count')} keys", flush=True)
            app.stop()
        with step("speed-verdict", out_dir):
            passed, reasons, rows = verdict(runs)
            with open(os.path.join(out_dir, "report.json"), "w") as f:
                json.dump({"passed": passed, "reasons": reasons, "runs": rows,
                           "bar": {"p95_ms": P95_MS, "over_ms": OVER_MS, "near_ms": NEAR_MS, "items": ITEMS}}, f, indent=1)
            check(passed, "the speed bar holds in all three runs" + (": " + "; ".join(reasons) if reasons else ""))
    finally:
        app.stop()


if __name__ == "__main__":
    main()
    finish()
