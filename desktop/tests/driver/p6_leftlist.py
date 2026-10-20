#!/usr/bin/env python3
"""LST's window checks (desktop-design-5-6 2.20 to 2.23; the row map's
w:lst.* and x:lst.* checks, failing as 8.5 says), one step per user flow.
The app is started here (tests/check.sh --no-app, apps.App); a run of
check.sh without --no-app works too: the app it starts is stopped first.

Fixture legs, with --fixture tests/fixtures/leftlist-list.json (moved so its
anchor day is today; CRUSH_DESKTOP_TEST_PICK_FOLDER stands in for the folder
dialog, 8.4):
  LIST-001  what the list shows: the project's chats, numbered, then the
            server rows pinned at the bottom; day groups; row content; the
            six states with their state lines; the server rows' texts and
            look (also LIST-010, LIST-011, LIST-012, LIST-028, LIST-047)
  LIST-008  Alt+number and a click show a chat and mark it read; the wheel
            scrolls the list and the shown row comes back into view
            (also LIST-045)
  LIST-002  Alt+S, "‹‹" and "›" switch to and from the strip, saved in the
            desktop settings; the strip's parts, marks and numbers, its "+"
            and ">_"; the strip forced below the narrowest width
            (also LIST-044)
  LIST-004  the folder picker: Alt+N, its first folder, keys, editing,
            errors, mouse; starting in this window's folder is a new chat
            here; Ctrl+N starts one with no picker (also LIST-006, LIST-007,
            LIST-038 to LIST-042, CMD-014)
  LIST-053  a picture under the folder picker is covered and comes back
  LIST-049  a window opened for another folder gets only the folder of the
            start flags; Browse… with that folder brings it forward on a new
            chat (window-request's newChat), opening no third window;
            the working mark shows the same frame in both windows; the other
            window follows the collapsed choice (also LIST-013, LIST-003)
  LIST-030  the right panel's server rows: open, fold and stop with clicks
            (also LIST-029, LIST-031, LIST-032)
  LIST-017  closing: one window's close leaves the app running; Ctrl+Shift+W
            runs the draft step and the last window quits the app
            (also LIST-026's and MISC-024's fixture part)
  LIST-003  the next start opens on the strip (the saved choice)
  LIST-048  a folder the window cannot switch to: "Couldn't start a chat."
            in the notice line
Engine legs, without --fixture (tests/fakes/scripts/leftlist-engine.json; the
terminal app in the hidden tmux):
  LIST-018  a chat the terminal app runs shows working, then Done, from the
            engine's records
  LIST-032  a real background job's port: its row, open, stop
            (also LIST-028, LIST-030, LIST-031)
  LIST-026  closing the window: the shown empty draft is discarded, the job
            of a chat the terminal shows keeps going, the other stops; the
            app quits; the terminal's job stops when it leaves (also
            MISC-024, LIST-017)
A row whose real behavior needs a piece that is not merged prints
"SKIP <row> ... runs after <piece> merges"; a text another piece draws
differently from the design prints "MISMATCH" (the report lists both).
Check names (lst.<name> or <name>) given as arguments run only the steps
that cover them."""
import ctypes, itertools, json, os, re, signal, socket, subprocess, sys, time
from datetime import date, datetime, timedelta

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import apps  # noqa: E402
from harness import check, finish, key, probe_eval, shot, step, type_text, wait_for, xdo  # noqa: E402

PROBE, OUT = sys.argv[1], sys.argv[2]
WANT = {name.removeprefix("lst.") for name in sys.argv[4:]}
FIXTURE = (re.search(r"--fixture\s+(\S+)", os.environ.get("DESKTOP_ARGS", "")) or [None, None])[1]
os.environ["DESKTOP_ARGS"] = ""  # the app gets the moved copy below instead

HOME = "/tmp/home"
PROJ, PROJ2 = apps.PROJ, apps.PROJ2
MOVED = "/tmp/leftlist-today.json"
SETTINGS = os.path.join(os.environ.get("XDG_CONFIG_HOME") or os.path.join(HOME, ".config"), "crush-desktop", "settings.json")
CLIP = os.path.join(HERE, "clip.py")
PICKER_HINT = "↑↓ pick · tab open folder · enter start · esc cancel"  # draw.go:673
WIDTH, HEIGHT = 1600, 992


def wanted(*names):
    return not WANT or bool(WANT & set(names))


def skip(row, why):
    print(f"SKIP {row} {why}", flush=True)


def mismatch(row, what):
    print(f"MISMATCH {row} {what}", flush=True)


def note(what):
    print(f"NOTE {what}", flush=True)


def lines(path):
    if not os.path.exists(path):
        return []
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


def near(a, b, tolerance=1.5):
    return a is not None and b is not None and abs(a - b) <= tolerance


def inside(inner, outer, slack=1):
    return bool(inner and outer) and inner["x"] >= outer["x"] - slack and inner["y"] >= outer["y"] - slack and \
        inner["x"] + inner["w"] <= outer["x"] + outer["w"] + slack and inner["y"] + inner["h"] <= outer["y"] + outer["h"] + slack


def poll(read, ok, timeout, every=0.1):
    """(True, value) once ok(value) holds, else (False, the last value)."""
    end = time.time() + timeout
    value = None
    while True:
        try:
            value = read()
            if ok(value):
                return True, value
        except (KeyError, TypeError, IndexError, AttributeError, ValueError, RuntimeError, StopIteration, OSError):
            pass
        if time.time() >= end:
            return False, value
        time.sleep(every)


# --- Windows -------------------------------------------------------------------

class _ClientMessage(ctypes.Structure):
    _fields_ = [("type", ctypes.c_int), ("serial", ctypes.c_ulong), ("send_event", ctypes.c_int), ("display", ctypes.c_void_p),
                ("window", ctypes.c_ulong), ("message_type", ctypes.c_ulong), ("format", ctypes.c_int), ("data", ctypes.c_long * 5)]


class _XEvent(ctypes.Union):
    _fields_ = [("xclient", _ClientMessage), ("pad", ctypes.c_long * 24)]


def ask_close(wid):
    """The window's close request (WM_DELETE_WINDOW), as a window manager sends
    for the close button (p3_connect.py; the hidden screen has none)."""
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


def window_ids(pid):
    """The app's visible windows (tiny helper windows left out)."""
    found = []
    out = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True).stdout
    for wid in out.split():
        owner = subprocess.run(["xdotool", "getwindowpid", wid], capture_output=True, text=True).stdout.strip()
        if owner == str(pid) and (geometry(int(wid)) or (0, 0, 0, 0))[2] > 100:
            found.append(int(wid))
    return found


def geometry(wid):
    g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", str(wid)], capture_output=True, text=True).stdout
    d = dict(line.split("=", 1) for line in g.split() if "=" in line)
    return (int(d["X"]), int(d["Y"]), int(d["WIDTH"]), int(d["HEIGHT"])) if "WIDTH" in d else None


_request_ids = itertools.count(int(time.time() * 1000) + 7_000_000)


class Win:
    """One window of an App: its probe file, its X window and the real mouse and keyboard."""

    def __init__(self, app, label, wid):
        self.app, self.label, self.wid = app, label, wid
        self.path = app.probe_file(label)

    def report(self):
        return self.app.window(self.label)

    def section(self, name):
        return self.report().get(name) or {}

    def ll(self):
        return self.section("leftlist")

    def until(self, cond, timeout=5):
        """The leftlist section once cond(section) holds, else None."""
        ok, sec = poll(self.ll, lambda s: bool(s) and bool(cond(s)), timeout)
        return sec if ok else None

    def heavy(self, invoke=None, timeout=20):
        """The whole report with heavy fields; every window answers the one
        shared request file in its own probe file (8.2)."""
        rid = next(_request_ids)
        request = {"id": rid}
        if invoke is not None:
            request["invoke"] = invoke
        side = self.app.probe + ".request.json"
        with open(side + ".tmp", "w") as f:
            json.dump(request, f)
        os.replace(side + ".tmp", side)
        report = wait_for(self.path, lambda p: p.get("request_id") == rid, timeout)
        return report if report.get("request_id") == rid else {}

    def hv(self, section="leftlist"):
        return (self.heavy().get("heavy") or {}).get(section) or {}

    def invoke(self, command, args=None):
        report = self.heavy(invoke=[{"command": command, "args": args or {}}])
        return (report.get("invoked") or [{}])[0] or {}

    def origin(self):
        g = geometry(self.wid)
        return (g[0], g[1]) if g else (0, 0)

    def focus(self):
        xdo("windowraise", self.wid)
        xdo("windowfocus", self.wid)
        x, y, w, h = geometry(self.wid)
        xdo("mousemove", x + w - 6, y + h - 6)
        time.sleep(0.2)

    def place(self, x, y, w, h):
        xdo("windowmove", self.wid, x, y)
        xdo("windowsize", self.wid, w, h)
        # The page sits under the window's own title bar, so it ends at the window's bottom edge.
        ok, _ = poll(lambda: self.report().get("shell") or {}, lambda s: round(s.get("w", 0)) == w and round(s.get("y", 0) + s.get("h", 0)) == h, 10)
        time.sleep(0.4)
        return ok

    def point(self, rect):
        ox, oy = self.origin()
        return round(ox + rect["x"] + rect["w"] / 2), round(oy + rect["y"] + rect["h"] / 2)

    def click(self, rect):
        if not rect:
            return False
        xdo("windowraise", self.wid)
        xdo("windowfocus", self.wid)
        x, y = self.point(rect)
        xdo("mousemove", x, y)
        xdo("click", 1)
        time.sleep(0.15)
        return True

    def click_at(self, vx, vy):
        ox, oy = self.origin()
        xdo("windowraise", self.wid)
        xdo("mousemove", round(ox + vx), round(oy + vy))
        xdo("click", 1)
        time.sleep(0.15)

    def wheel(self, rect, clicks, down=True):
        x, y = self.point(rect)
        xdo("mousemove", x, y)
        for _ in range(clicks):
            xdo("click", 5 if down else 4)
            time.sleep(0.02)
        time.sleep(0.4)

    def alive(self):
        return self.wid in window_ids(self.app.proc.pid)


def stop_strays():
    """An app run-in-sandbox started (check.sh without --no-app) holds the single
    instance; stop it so the apps started here are their own."""
    mine = os.getpid()
    for pid in filter(str.isdigit, os.listdir("/proc")):
        try:
            with open(f"/proc/{pid}/cmdline", "rb") as f:
                args = f.read().split(b"\0")
        except OSError:
            continue
        if int(pid) != mine and args and args[0].endswith(b"crush-desktop"):
            os.kill(int(pid), signal.SIGTERM)
            ok, _ = poll(lambda: os.path.exists(f"/proc/{pid}"), lambda there: not there, 10)
            if not ok:
                os.kill(int(pid), signal.SIGKILL)


def start_app(args=(), env=None):
    """The app and its first window, joined, at WIDTH x HEIGHT in the corner."""
    for side in ("api.jsonl", "opened.jsonl"):
        if os.path.exists(f"{PROBE}.{side}"):
            os.remove(f"{PROBE}.{side}")
    app = apps.App(args=args, env=env or {})
    ok, wids = poll(lambda: window_ids(app.proc.pid), bool, 30, 0.3)
    check(ok, f"the app's window shows (app-{app.n}.log)")
    win = Win(app, "main", wids[0] if wids else 0)
    ok, _ = poll(lambda: win.section("connection").get("state"), lambda s: s == "connected", 60, 0.3)
    check(ok, "the window joined (the fixture or the engine)")
    if wids:
        win.place(0, 0, WIDTH, HEIGHT)
    return app, win


def row_of(sec, rid):
    return next((r for r in (sec or {}).get("rows") or [] if r.get("id") == rid), None)


def state_of(sec, rid):
    return (row_of(sec, rid) or {}).get("state")


def shown_id(sec):
    return next((r["id"] for r in (sec or {}).get("rows") or [] if r.get("shown")), "")


def number_of(sec, rid):
    return (row_of(sec, rid) or {}).get("number")


def show(win, rid):
    """Alt+<number> for a row (numbers 1 to 9)."""
    n = number_of(win.ll(), rid)
    if not n or n > 9:
        return False
    win.focus()
    key(f"alt+{n}")
    return bool(win.until(lambda s: shown_id(s) == rid, 5))


def settings():
    try:
        with open(SETTINGS) as f:
            return json.load(f)
    except (OSError, ValueError):
        return {}


def api_calls(path_end, since=0):
    return [c for c in lines(f"{PROBE}.api.jsonl")[since:] if c.get("method") == "POST" and str(c.get("path", "")).endswith(path_end)]


def opened_urls(since=0):
    return [o.get("url") for o in lines(f"{PROBE}.opened.jsonl")[since:]]


def dialogs(win):
    return win.section("overlay").get("dialogs") or []


def clip_set(text):
    return subprocess.run([sys.executable, "-I", CLIP, "set-text", text], capture_output=True, timeout=30).returncode == 0


def listening(port):
    with socket.socket() as s:
        s.settimeout(1)
        return s.connect_ex(("127.0.0.1", port)) == 0


# --- The fixture -----------------------------------------------------------------

def moved_fixture(src, dst):
    """The fixture with its days moved so its anchor day is today, times kept
    (p3_start.py's moved_fixture)."""
    with open(src) as f:
        data = json.load(f)
    days = (date.today() - datetime.fromtimestamp(data["anchor"]).date()).days

    def move(t):
        dt = datetime.fromtimestamp(t)
        return int(datetime.combine(dt.date() + timedelta(days=days), dt.time()).timestamp())

    def walk(v):
        if isinstance(v, dict):
            return {k: (move(x) if (k.endswith("_at") or k == "time") and isinstance(x, int) and x > 0 else walk(x)) for k, x in v.items()}
        if isinstance(v, list):
            return [walk(x) for x in v]
        return v

    moved = walk(data)
    with open(dst, "w") as f:
        json.dump(moved, f)
    return moved


STATES = {"spin": "working", "done": "unread", "wait": "waiting", "bg": "background", "ready": "ready", "untitled": "ready",
          "pic": "ready", "yday": "ready", "fresh": "unknown"}
STATE_LINES = {"spin": "Working…", "done": "Done", "wait": "Waiting for permission", "bg": "Background", "ready": "",
               "untitled": "", "fresh": ""}
WEEKDAYS = ["Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"]
MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]


def design_day(at, today):
    """2.20's group label: Today, Yesterday, then "<weekday> <day> <month>"."""
    d = datetime.fromtimestamp(at).date()
    if d == today:
        return "Today"
    if d == today - timedelta(days=1):
        return "Yesterday"
    return f"{WEEKDAYS[d.weekday()]} {d.day} {MONTHS[d.month - 1]}"


def flow_list(main, data):
    """LIST-001 with LIST-010, LIST-011, LIST-012, LIST-028, LIST-047."""
    with step("LIST-001", OUT):
        chats = data["get"]["sessions"]
        sec = main.until(lambda s: state_of(s, "fresh") == "unknown" and state_of(s, "done") == "unread", 15)
        check(bool(sec), "the chat another app made (fresh) is listed as unknown, and done turned unread after its first read")
        sec = sec or main.ll()
        lst = main.section("list")
        rows = sec.get("rows") or []
        ids = [r["id"] for r in rows if r["kind"] == "chat"]
        want = {c["id"] for c in chats} | {"fresh"}
        check(set(ids) == want and len(ids) == len(want), f"the list has every chat of the project ({len(ids)} of {len(want)})")
        check(ids == [r["id"] for r in lst.get("rows") or []], "in the list's own order (newest first)")
        check([r["number"] for r in rows] == list(range(1, len(rows) + 1)), "each row is numbered by its place (its Alt+number)")
        check(all(r["kind"] == "chat" for r in rows), "chats come first; this window has no terminal tabs")
        skip("LIST-001", "terminal rows between the chats and the server rows: runs after TERM merges")

        # LIST-010: groups by day.
        today = date.today()
        when = {c["id"]: c["updated_at"] for c in chats}
        days = lst.get("days") or []
        # The list draws the newest 6 chats (with the shown one), then "All
        # chats (N more)" (owner 2026-10-19): the groups are the drawn rows'.
        drawn_ids = [r["id"] for r in main.hv().get("chat_rows") or []]
        check(drawn_ids == ids[:6], f"the list draws the newest 6 chats ({drawn_ids})")
        more = probe_eval(main.path, 'window.__probeEval = document.querySelector("nav.chats .all-chats")?.textContent.replace(/\\s+/g, " ").trim() ?? null;')
        check(more == f"All chats ({len(ids) - 6} more)", f"then the line for the rest ({more!r})")
        want_days = []
        for rid in drawn_ids:
            label = design_day(when.get(rid, time.time()), today)
            if not want_days or want_days[-1] != label:
                want_days.append(label)
        check(days == want_days, f"its day groups ({days}, want {want_days})")
        skip("LIST-010", "the Terminals [n] group: runs after TERM merges")
        shot(OUT, "LIST-010")

        # LIST-011: row content.
        by_id = {r["id"]: r for r in lst.get("rows") or []}
        titled = [c for c in chats if c["title"]]
        check(all(by_id.get(c["id"], {}).get("title") == c["title"] for c in titled), "each row shows its chat's title")
        check(all(by_id.get(c["id"], {}).get("agent") == "Fake AI" for c in chats), "and its AI tool (the chat's provider, by name)")
        timed = [c for c in chats if not STATE_LINES.get(c["id"])]
        check(all(by_id.get(c["id"], {}).get("time") for c in timed), "and its time where it has no state line")
        fallback = by_id.get("untitled", {}).get("title")
        if fallback == "New session":
            check(True, "an untitled chat reads \"New session\"")
        elif fallback == "New Session":
            mismatch("LIST-011", "an untitled chat reads \"New Session\" (P3.9's listmodel.js:139); the design and the terminal say \"New session\" (draw.go:267-272)")
        else:
            check(False, f"an untitled chat reads \"New session\" ({fallback!r})")
        shot(OUT, "LIST-011")

        # LIST-012: states and state lines.
        got = {r["id"]: r["state"] for r in rows}
        for rid, state in STATES.items():
            check(got.get(rid) == state, f"{rid} is {state} ({got.get(rid)})")
        notes = [r for r in rows if r["id"].startswith("note-")]
        check(all(r["state"] == "ready" for r in notes), "every other chat is ready")
        h = main.hv()
        drawn = {r["id"]: r for r in h.get("chat_rows") or []}
        for rid, text in STATE_LINES.items():
            if rid not in drawn:
                continue  # past the newest 6
            check(drawn.get(rid, {}).get("state_text") == text, f"{rid}'s state line reads {text!r} ({drawn.get(rid, {}).get('state_text')!r})")
        done = drawn.get("done", {})
        check(done.get("registered") and done.get("mark"), "Done is the list's own state, drawn with its mark in the state's place")
        check(sec.get("unknown") == ["fresh"], f"only fresh waits for its first read ({sec.get('unknown')})")
        shot(OUT, "LIST-012")

        # LIST-028 and LIST-047: the server rows.
        servers = [(s["label"], s["right"]) for s in sec.get("servers") or []]
        check(servers == [("localhost:8100", ""), ("dev", "2 ports"), ("web", ":8000")],
              f"server rows in the list's order, each port once, a program's ports folded ({servers})")
        check(sec.get("server_count") == 4, "four ports")
        # The rows are the right panel's SERVERS since 2026-10-19 (mouse only,
        # owner 2026-10-20): a name, then the port or the program's port count.
        panel = panel_servers(main)
        check([r["text"] for r in panel] == ["localhost:8100", "dev 2 ports", "web :8000"], f"the right panel lists them the same ({[r['text'] for r in panel]})")
        shot(OUT, "LIST-028")


def flow_switch(main):
    """LIST-008 with LIST-045."""
    with step("LIST-008", OUT):
        main.focus()
        sec = main.ll()
        n = number_of(sec, "done")
        key(f"alt+{n}")
        sec = main.until(lambda s: shown_id(s) == "done", 5)
        check(bool(sec), f"Alt+{n} shows the chat numbered {n}")
        check(state_of(sec, "done") == "ready", f"showing it marks it read ({state_of(sec, 'done')})")
        check(next((r["id"] for r in main.section("list").get("rows") or [] if r.get("selected")), "") == "done", "the list marks it shown")
        rect = next((r["rect"] for r in (main.hv("list").get("rows") or []) if r["id"] == "wait"), None)
        main.click(rect)
        check(bool(main.until(lambda s: shown_id(s) == "wait", 5)), "a click on a row shows its chat")
        # LIST-045: the list draws the newest 6 chats and the shown one
        # (owner 2026-10-19); showing a chat past them adds its row.
        ids = [r["id"] for r in main.ll().get("rows") or []]
        far = ids[7]
        key("alt+8")
        main.until(lambda s: shown_id(s) == far, 5)
        ok, h = poll(main.hv, lambda h: [r["id"] for r in h.get("chat_rows") or []] == ids[:6] + [far], 3, 0.3)
        check(ok, f"Alt+8 shows {far}, past the newest 6, and its row joins them ({[r['id'] for r in (h or {}).get('chat_rows') or []]})")
        key(f"alt+{number_of(main.ll(), 'ready')}")
        main.until(lambda s: shown_id(s) == "ready", 5)
        ok, h = poll(main.hv, lambda h: [r["id"] for r in h.get("chat_rows") or []] == ids[:6], 3, 0.3)
        check(ok, "showing one of the 6 again leaves the 6")
        shot(OUT, "LIST-045")


def flow_collapse(main):
    """LIST-002 with LIST-044 and LIST-003's saved choice."""
    with step("LIST-002", OUT):
        main.focus()
        key("alt+s")
        sec = main.until(lambda s: s["collapsed"] and s["strip"], 5)
        check(bool(sec), "Alt+S collapses the list to the strip")
        ok, _ = poll(settings, lambda s: s.get("list_collapsed") is True, 3)
        check(ok, "and saves list_collapsed in the desktop settings")
        h = main.hv()
        px = h.get("px") or 1
        strip = h.get("strip")
        check(strip and near(strip["w"], 64 * px) and not h.get("wordmark"), "the strip, 64 wide, takes the open list's place")
        ex, ns, nt = h.get("strip_expand"), h.get("strip_new_session"), h.get("strip_new_terminal")
        check(ex and ns and nt and ex["y"] < ns["y"] < nt["y"], "top to bottom: ›, +, >_")
        cells = h.get("strip_cells") or []
        rows = (sec or main.ll()).get("rows") or []
        check([c["row"] for c in cells] == [f"{r['kind']}:{r['id']}" for r in rows], "then one cell per row, in the list's order")
        check([c["number"] for c in cells] == [str(r["number"])[:2] for r in rows], "each with its number (at most two digits)")
        check([c["state"] for c in cells] == [r["state"] for r in rows], "and its state's mark")
        check(all(c["mark"] for c in cells), "every cell draws its mark")
        check([c["row"] for c in cells if c["shown"]] == ["chat:ready"], "the shown row's cell has the bar")
        check(not h.get("failure"), "no notice line in the strip (the servers are the right panel's)")
        skip("LIST-044", "the rule between chats and terminal tabs: runs after TERM merges")
        cell = next((c for c in cells if c["row"] == "chat:wait"), None)
        main.click(cell and cell["rect"])
        check(bool(main.until(lambda s: shown_id(s) == "wait", 5)), "a click on a cell shows its row")
        main.click(ns)
        sec = main.until(lambda s: (s.get("picker") or {}).get("kind") == "session", 5)
        check(bool(sec) and sec["picker"]["title"] == "New chat in…", "the strip's + opens \"New chat in…\"")
        key("Escape")
        main.until(lambda s: not s.get("picker"), 3)
        main.click(nt)
        sec = main.until(lambda s: (s.get("picker") or {}).get("kind") == "terminal", 5)
        check(bool(sec) and sec["picker"]["title"] == "New terminal in…", "its >_ opens \"New terminal in…\"")
        key("Escape")
        main.until(lambda s: not s.get("picker"), 3)
        shot(OUT, "LIST-044")
        main.click(ex)
        check(bool(main.until(lambda s: not s["collapsed"] and not s["strip"], 5)), "a click on › opens the list")
        ok, _ = poll(settings, lambda s: s.get("list_collapsed") is False, 3)
        check(ok, "and saves it open")
        collapse = main.hv().get("collapse")
        wordmark = main.hv().get("wordmark")
        check(collapse and wordmark and collapse["y"] < wordmark["y"] + wordmark["h"] and collapse["x"] > wordmark["x"], "‹‹ sits at the right end of the wordmark's row")
        main.click(collapse)
        check(bool(main.until(lambda s: s["collapsed"], 5)), "a click on ‹‹ collapses the list")
        main.focus()
        key("alt+s")
        main.until(lambda s: not s["strip"], 5)
        # Forced below the narrowest width, the saved choice kept.
        main.place(0, 0, 900, HEIGHT)
        main.focus()
        for _ in range(10):
            if main.ll().get("forced"):
                break
            key("ctrl+equal")
            time.sleep(0.4)
        sec = main.until(lambda s: s["forced"] and s["strip"] and not s["collapsed"], 3)
        check(bool(sec), f"too narrow for the open list (zoom {main.report().get('zoom')}), the strip is forced")
        check(settings().get("list_collapsed") is False, "and the saved choice stays open")
        shot(OUT, "LIST-002-forced")
        key("ctrl+0")
        main.place(0, 0, WIDTH, HEIGHT)
        check(bool(main.until(lambda s: not s["forced"] and not s["strip"], 5)), "with room again the open list comes back")


def flow_picker(main):
    """LIST-004 with LIST-006, LIST-007, LIST-038 to LIST-042 and CMD-014."""
    with step("LIST-004", OUT):
        show(main, "ready")
        main.focus()
        key("alt+n")
        sec = main.until(lambda s: (s.get("picker") or {}).get("kind") == "session", 5)
        check(bool(sec), "Alt+N opens the folder picker")
        p = (sec or {}).get("picker") or {}
        check(p.get("title") == "New chat in…" and "folder_picker" in dialogs(main), f"titled \"New chat in…\" ({p.get('title')!r})")
        # LIST-038: the shown chat's folder, home as ~, a / at the end, the caret after it.
        check(p.get("input") == "~/proj/" and p.get("cursor") == 7 and p.get("sel") == -1 and not p.get("err"),
              f"it starts with the shown chat's folder as ~/proj/, the caret at the end, nothing picked ({p.get('input')!r} {p.get('cursor')})")
        hp = main.hv().get("picker") or {}
        check(hp.get("focused") and hp.get("field_value") == "~/proj/" and hp.get("caret") == 7, "the field has the focus and the caret")
        check(bool(hp.get("browse")), "Browse… sits beside the field")
        check(p.get("hint") == PICKER_HINT, "its hint is the terminal's")
        skip("LIST-006", "the sheet's drawn frame, title and help footer: runs after KIT merges")
        skip("LIST-038", "a shown terminal tab's folder: runs after TERM merges")
        shot(OUT, "LIST-038")
        # LIST-039: the matches over dir_list and the Recent list.
        listing = main.invoke("dir_list", {"path": HOME})
        if listing.get("ok"):
            type_text("")
            key("ctrl+u")
            type_text("~/pro")
            want = sorted(n for n in os.listdir(HOME) if n.lower().startswith("pro") and os.path.isdir(os.path.join(HOME, n)))[:5]
            ok, sec = poll(main.ll, lambda s: s["picker"]["matches"] == [f"{HOME}/{n}" for n in want], 3)
            check(ok, f"typing ~/pro lists the folders starting with it ({(sec or {}).get('picker', {}).get('matches')})")
            key("Tab")
            ok, _ = poll(main.ll, lambda s: s["picker"]["input"] == f"~/{want[0]}/", 3)
            check(ok, "Tab fills in the first match with a /")
        else:
            skip("LIST-039", f"the matches over dir_list: runs after VIEW merges ({(listing.get('error') or {}).get('code')})")
            skip("LIST-041", "clicks on match and recent rows: runs after VIEW and SES merge")
        recent = main.invoke("recent_folders", {"limit": 5})
        if recent.get("ok"):
            got = main.ll().get("picker", {}).get("recent")
            check(got == (recent.get("value") or [])[:5], f"Recent lists recent_folders' folders ({got})")
            heading = (main.hv().get("picker") or {}).get("heading")
            check(bool(heading) == bool(got), "under the heading Recent when there are any")
        else:
            skip("LIST-039", f"the Recent list over recent_folders: runs after SES merges ({(recent.get('error') or {}).get('code')})")
        shot(OUT, "LIST-039")
        # LIST-040: errors.
        main.focus()
        key("ctrl+u")
        sec = main.until(lambda s: s["picker"]["input"] == "" and s["picker"]["cursor"] == 0, 3)
        check(bool(sec), "Ctrl+U clears the field")
        key("Return")
        sec = main.until(lambda s: s["picker"]["err"] == "Type a folder.", 3)
        check(bool(sec), "Enter with nothing typed says \"Type a folder.\"")
        check(bool((main.hv().get("picker") or {}).get("error")), "on the error line under the field")
        type_text("nope")
        sec = main.until(lambda s: s["picker"]["input"] == "nope", 3)
        check(bool(sec) and sec["picker"]["err"] == "", "typing clears the error")
        key("Return")
        sec = main.until(lambda s: s["picker"]["err"].startswith("No folder"), 3)
        check(bool(sec) and sec["picker"]["err"] == "No folder at ~/proj/nope",
              f"a path that is no folder: \"No folder at <path>\", relative to the starting folder, home as ~ ({(sec or {}).get('picker', {}).get('err')!r})")
        key("Down")
        time.sleep(0.3)
        check(main.ll()["picker"]["err"] == "No folder at ~/proj/nope", "moving with ↓ leaves the error")
        h = main.hv().get("picker") or {}
        main.click(h.get("error"))
        time.sleep(0.3)
        check((main.ll().get("picker") or {}).get("err") == "No folder at ~/proj/nope" and "folder_picker" in dialogs(main),
              "a click inside the sheet off its rows does nothing")
        key("ctrl+u")
        long_tail = "a-folder-with-a-long-name/" * 5
        type_text("~/proj/" + long_tail)
        main.until(lambda s: s["picker"]["input"].endswith(long_tail), 5)
        key("Return")
        sec = main.until(lambda s: s["picker"]["err"].startswith("No folder"), 3)
        check(bool(sec) and sec["picker"]["err"] == "No folder at ~/proj/" + long_tail.rstrip("/"), "a long path is named in full")
        h = main.hv().get("picker") or {}
        check(h.get("error_cut") is True and inside(h.get("error"), h.get("frame") or h.get("error")), "and drawn cut from the front to fit")
        shot(OUT, "LIST-040")
        # LIST-007 and LIST-042: keys and editing.
        key("ctrl+u")
        type_text("abcd")
        main.until(lambda s: s["picker"]["input"] == "abcd", 3)
        key("ctrl+a")
        check(bool(main.until(lambda s: s["picker"]["cursor"] == 0, 2)) and (main.hv().get("picker") or {}).get("caret") == 0, "Ctrl+A goes to the start")
        key("ctrl+e")
        check(bool(main.until(lambda s: s["picker"]["cursor"] == 4, 2)) and (main.hv().get("picker") or {}).get("caret") == 4, "Ctrl+E goes to the end")
        key("Home")
        check((main.hv().get("picker") or {}).get("caret") == 0, "Home goes to the start")
        key("End")
        check((main.hv().get("picker") or {}).get("caret") == 4, "End goes to the end")
        key("BackSpace")
        check(bool(main.until(lambda s: s["picker"]["input"] == "abc", 2)), "Backspace deletes before the caret")
        key("ctrl+a", "ctrl+d")
        check(bool(main.until(lambda s: s["picker"]["input"] == "bc", 2)), "Ctrl+D deletes after the caret")
        key("Delete")
        check(bool(main.until(lambda s: s["picker"]["input"] == "c", 2)), "Delete deletes after the caret")
        key("ctrl+u")
        type_text("~/proj/sub/")
        main.until(lambda s: s["picker"]["input"] == "~/proj/sub/", 3)
        key("ctrl+w")
        check(bool(main.until(lambda s: s["picker"]["input"] == "~/proj/", 2)), "Ctrl+W deletes the last folder name with its slash")
        type_text("deeper")
        main.until(lambda s: s["picker"]["input"] == "~/proj/deeper", 3)
        key("alt+BackSpace")
        check(bool(main.until(lambda s: s["picker"]["input"] == "~/proj/", 2)), "so does Alt+Backspace")
        key("alt+x")
        time.sleep(0.3)
        check(main.ll()["picker"]["input"] == "~/proj/", "Alt with a letter types nothing")
        key("ctrl+u")
        if check(clip_set(f"{HOME}/\nproj2\t"), "the clipboard holds a path with a new line and a tab"):
            key("ctrl+v")
            check(bool(main.until(lambda s: s["picker"]["input"] == f"{HOME}/proj2", 3)), "a paste loses new lines and tabs")
        key("Up", "Down", "shift+Tab", "ctrl+p", "ctrl+n")
        time.sleep(0.3)
        check(main.ll()["picker"]["sel"] == -1 or main.ll()["picker"]["matches"], "with no rows, ↑ ↓ Shift+Tab Ctrl+P Ctrl+N stay on the typed path")
        skip("LIST-007", "↑ ↓ Tab over rows (wrapping through the typed path): runs after VIEW and SES merge")
        shot(OUT, "LIST-007")
        shot(OUT, "LIST-042")
        # LIST-041: a click outside closes it.
        x, y, w, hgt = geometry(main.wid)
        main.click_at(w - 40, hgt - 40)
        check(bool(main.until(lambda s: not s.get("picker"), 3)) and "folder_picker" not in dialogs(main), "a click outside the sheet closes it")
        shot(OUT, "LIST-041")
        # Esc cancels; Enter in this window's folder starts a new chat here.
        key("alt+n")
        main.until(lambda s: s.get("picker"), 3)
        key("Escape")
        check(bool(main.until(lambda s: not s.get("picker"), 3)) and shown_id(main.ll()) == "ready", "Esc cancels and changes nothing")
        key("alt+n")
        main.until(lambda s: (s.get("picker") or {}).get("input") == "~/proj/", 3)
        key("Return")
        sec = main.until(lambda s: not s.get("picker") and not shown_id(s), 5)
        check(bool(sec), "Enter on this window's folder starts a new chat here (no chat shown, the new chat's page)")
        check(len(window_ids(main.app.proc.pid)) == 1, "no other window opens")
        # CMD-014: Ctrl+N, no picker.
        show(main, "ready")
        key("ctrl+n")
        sec = main.until(lambda s: not shown_id(s), 5)
        check(bool(sec) and not sec.get("picker") and "folder_picker" not in dialogs(main), "Ctrl+N starts a new chat with no picker")
        skip("CMD-014", "the palette's \"New Session\" entry: runs after PAL merges")
        shot(OUT, "CMD-014")


def flow_pictures(main):
    """LIST-053."""
    with step("LIST-053", OUT):
        check(show(main, "pic"), "the picture's chat is shown")
        ok, h = poll(main.hv, lambda h: h.get("tool_lines"), 8, 0.3)
        check(ok, "its tool line is drawn")
        for _ in range(3):
            h = main.hv()
            if h.get("images"):
                break
            line = next((t for t in h.get("tool_lines") or [] if t["tool"] == "pic-call-1"), None) or \
                next((t for t in h.get("tool_lines") or [] if not t["tool"]), None)
            main.click(line and line["rect"])
            time.sleep(0.8)
        ok, h = poll(main.hv, lambda h: h.get("images"), 5, 0.3)
        check(ok and all(i["on_top"] for i in h["images"]), "the picture shows in the chat")
        main.focus()
        key("alt+n")
        opened = main.until(lambda s: s.get("picker"), 3)
        h = main.hv()
        check(bool(h.get("images")) and not any(i["on_top"] for i in h["images"]),
              f"the folder picker covers it (picker {bool(opened)}, pictures {h.get('images')}, dialogs {dialogs(main)})")
        key("Escape")
        main.until(lambda s: not s.get("picker"), 3)
        ok, h = poll(main.hv, lambda h: h.get("images") and all(i["on_top"] for i in h["images"]), 3, 0.3)
        check(ok, "and it comes back when the picker closes")
        skip("LIST-053", "the terminal close box over a picture: runs after TERM merges")


def grab():
    from PIL import ImageGrab
    return ImageGrab.grab(xdisplay=os.environ.get("DISPLAY"))


def mark_box(win, rid):
    h = win.hv()
    mark = next((r["mark"] for r in h.get("chat_rows") or [] if r["id"] == rid), None)
    if not mark:
        return None
    ox, oy = win.origin()
    x, y = round(ox + mark["x"]), round(oy + mark["y"])
    return (x, y, x + max(round(mark["w"]), 1), y + max(round(mark["h"]), 1))


def flow_windows(app, main):
    """LIST-049 with LIST-013 and LIST-003's other window. Returns w2."""
    w2 = None
    with step("LIST-049", OUT):
        show(main, "ready")
        before = set(window_ids(app.proc.pid))
        # Another folder switches this window (owner 2026-10-19: no new
        # window; panel_rework.py's switch leg checks that on the engine), so
        # the second window opens as Open Project's does.
        main.invoke("window_open", {"projectDir": PROJ2, "newChat": True})
        ok, wids = poll(lambda: set(window_ids(app.proc.pid)) - before, bool, 20, 0.3)
        check(ok, "a window opens on another folder")
        if not ok:
            return None
        w2 = Win(app, "w2", sorted(wids)[0])
        ok, _ = poll(lambda: w2.section("connection").get("state"), lambda s: s == "connected", 30, 0.3)
        check(ok, "the new window joins")
        a2, a1 = w2.invoke("app_info").get("value") or {}, main.invoke("app_info").get("value") or {}
        check(a2.get("cwd") == PROJ2 and a2.get("first_window") is False and a2.get("new_chat") is True,
              f"it is on the chosen folder, not the first window, with a new chat ({a2.get('cwd')}, {a2.get('first_window')}, {a2.get('new_chat')})")
        check(a1.get("first_window") is True and a1.get("session") == "ready", f"the first window keeps -s ready ({a1.get('session')})")
        check(a2.get("session") is None and a2.get("continue_last") is False, "the later window gets no -s (start flags apply to the first window only)")
        note("LIST-049: --yolo and --debug reach later windows through S5's join; the fixture has no engine to show them, so the engine run's windows carry them")
        check(bool(w2.until(lambda s: s.get("rows") and not shown_id(s), 10)), "the new window shows a new chat")
        # window-request's newChat: the window already open comes forward with a new chat.
        w2.until(lambda s: row_of(s, "fresh"), 8)  # its numbers settle once fresh arrives
        w2.focus()
        check(show(w2, "ready"), "the new window shows a chat")
        main.focus()
        key("alt+n")
        main.until(lambda s: s.get("picker"), 3)
        main.click((main.hv().get("picker") or {}).get("browse"))
        check(bool(w2.until(lambda s: not shown_id(s), 5)), "Browse… with that folder brings its window forward with a new chat")
        check(bool(main.until(lambda s: not s.get("picker"), 3)), "the picker closes")
        check(set(window_ids(app.proc.pid)) == before | {w2.wid} and shown_id(main.ll()) == "ready", "no third window opens and this window stays where it was")

    with step("LIST-013", OUT):
        # Side by side, not overlapping: each list's marks must be on screen.
        main.place(0, 0, WIDTH // 2, 1000)
        w2.place(WIDTH // 2, 0, WIDTH // 2, 1000)
        for win in (main, w2):  # both lists at their top, so the working chat is in view
            scroll = (win.hv().get("scroll") or {}).get("rect")
            if scroll:
                win.wheel(scroll, 30, down=False)
        main.focus()
        boxes = [mark_box(main, "spin"), mark_box(w2, "spin")]
        check(all(boxes), f"both windows draw the working chat's mark ({boxes})")
        if all(boxes):
            size = (min(b[2] - b[0] for b in boxes), min(b[3] - b[1] for b in boxes))
            same, frames = 0, set()
            for i in range(8):
                now = time.time() * 1000
                time.sleep(((55 - now % 100) % 100 + 100 * (i % 3)) / 1000)  # well inside a frame
                picture = grab()
                a, b = (picture.crop((x, y, x + size[0], y + size[1])).tobytes() for x, y, _, _ in boxes)
                same += a == b
                frames.add(a)
            check(same == 8, f"the two marks show the same frame in every screenshot ({same} of 8)")
            check(len(frames) >= 2, f"and the mark moves ({len(frames)} frames seen)")
        main.place(0, 0, WIDTH, HEIGHT)
        w2.place(WIDTH - 900, 0, 900, 1000)
        main.focus()

    with step("LIST-003", OUT):
        key("alt+s")
        check(bool(main.until(lambda s: s["collapsed"], 3)), "Alt+S collapses this window's list")
        check(bool(w2.until(lambda s: s["collapsed"] and s["strip"], 2)), "the other window follows within 2 s")
        key("alt+s")
        check(bool(w2.until(lambda s: not s["collapsed"], 2)), "and opens again with it")
    return w2


def flow_servers(main):
    """LIST-030 with LIST-029, LIST-031 and LIST-032, with the mouse in the
    right panel's SERVERS (mouse only, owner 2026-10-20)."""
    with step("LIST-030", OUT):
        main.focus()
        main.until(lambda s: s.get("server_count") == 4, 5)

        def prow(text):
            return next((r for r in panel_servers(main) if r["text"] == text), {})

        opened0 = len(lines(f"{PROBE}.opened.jsonl"))
        # LIST-029: the fold.
        main.click(prow("dev 2 ports").get("open"))
        sec = main.until(lambda s: [(x["label"], x["right"], x["child"]) for x in s["servers"]] ==
                         [("localhost:8100", "", False), ("dev", "2 ports", False), ("node", ":5173", True), ("esbuild", ":5174", True), ("web", ":8000", False)], 3)
        check(bool(sec), "a click on a program opens it: its servers follow, named after the program holding each port")
        panel = panel_servers(main)
        check([r["text"] for r in panel] == ["localhost:8100", "dev 2 ports", "node :5173", "esbuild :5174", "web :8000"] and panel[2]["child"] and panel[3]["child"]
              and panel[2]["open"]["x"] > panel[1]["open"]["x"] + 10, f"indented under it ({[r['text'] for r in panel]})")
        check(len(lines(f"{PROBE}.opened.jsonl")) == opened0, "opening a program opens no page")
        shot(OUT, "LIST-029")
        main.click(prow("dev 2 ports").get("open"))
        check(bool(main.until(lambda s: len(s["servers"]) == 3 and not s["servers"][1]["open"], 2)), "another click closes it")
        # LIST-031: open.
        main.click(prow("web :8000").get("open"))
        ok, _ = poll(lambda: opened_urls(opened0), lambda u: u == ["http://localhost:8000"], 3)
        check(ok, "a click on a server's name opens http://localhost:8000")
        shot(OUT, "LIST-031")
        # LIST-032: stop.
        calls0 = len(lines(f"{PROBE}.api.jsonl"))
        main.click(prow("web :8000").get("stop"))
        ok, calls = poll(lambda: api_calls("/jobs/kill", calls0), lambda c: len(c) == 1, 3)
        check(ok and calls[0]["path"].endswith("sessions/ready/jobs/kill") and calls[0]["body"] == {"job_id": "job-web"},
              f"its cross stops the job: POST …/sessions/ready/jobs/kill {{\"job_id\": \"job-web\"}} ({calls[-1] if calls else None})")
        main.until(lambda s: s.get("server_count") == 4, 3)  # the fixture's next read has it again
        main.click(prow("localhost:8100").get("stop"))
        ok, calls = poll(lambda: api_calls("/jobs/kill", calls0), lambda c: len(c) == 2, 3)
        check(ok and calls[1]["path"].endswith("sessions/wait/jobs/kill") and calls[1]["body"] == {"pid": 4242},
              "a plain process's cross stops it by its pid")
        main.until(lambda s: s.get("server_count") == 4, 3)
        main.click(prow("dev 2 ports").get("stop"))
        ok, calls = poll(lambda: api_calls("/jobs/kill", calls0), lambda c: len(c) == 3, 3)
        check(ok and calls[2]["path"].endswith("sessions/bg/jobs/kill") and calls[2]["body"] == {"job_id": "job-dev"},
              "a program's cross stops its job")
        shot(OUT, "LIST-032")
        main.until(lambda s: s.get("server_count") == 4, 3)


def flow_close(app, main, w2):
    """LIST-017 with LIST-026's and MISC-024's fixture part; leaves the list collapsed for LIST-003."""
    with step("LIST-017", OUT):
        if w2:
            ask_close(w2.wid)
            ok, _ = poll(lambda: w2.alive(), lambda alive: not alive, 10, 0.3)
            check(ok, "the system close button closes the second window")
            time.sleep(1)
            check(app.proc.poll() is None and main.alive(), "the app keeps running with a window left")
        show(main, "ready")
        key("alt+s")
        main.until(lambda s: s["collapsed"], 3)
        poll(settings, lambda s: s.get("list_collapsed") is True, 3)
        calls0 = len(lines(f"{PROBE}.api.jsonl"))
        main.focus()
        key("ctrl+shift+w")
        code = app.wait_exit(15)
        check(code is not None, f"Ctrl+Shift+W closes the last window and the app quits (exit {code})")
        drafts = [c for c in api_calls("saved-sessions", calls0) if c.get("window") == "main"]
        check(any(c["body"] == {"DiscardEmpty": True, "Session": {"ID": "ready"}} for c in drafts),
              f"the draft step asked the engine to discard the shown chat if it is an empty draft ({[c['body'] for c in drafts]})")
        skip("LIST-026", "the close steps of TERM (terminals) and SHARE (remote): run after those pieces merge")
        app.stop()


def flow_restart():
    """LIST-003 (the next start) and LIST-048."""
    app, main = start_app(("--fixture", MOVED), {"CRUSH_DESKTOP_TEST_PICK_FOLDER": "not/absolute"})
    try:
        with step("LIST-003", OUT):
            check(settings().get("list_collapsed") is True, "the settings file keeps list_collapsed")
            check(bool(main.until(lambda s: s["collapsed"] and s["strip"], 10)), "the next start opens on the strip")
            main.focus()
            key("alt+s")
            main.until(lambda s: not s["strip"], 3)

        with step("LIST-048", OUT):
            main.until(lambda s: s.get("server_count") == 4, 10)
            show(main, "ready")
            key("alt+n")
            main.until(lambda s: s.get("picker"), 3)
            main.click((main.hv().get("picker") or {}).get("browse"))
            # A folder the window cannot switch to says so in the notice line
            # (the switch in this window, owner 2026-10-19).
            ok, notice = poll(lambda: main.section("overlay").get("notice"), lambda n: bool(n), 5)
            check(ok and notice == {"kind": "error", "text": "Couldn't start a chat."}, f"a chat that cannot start says so ({notice})")
            check(not main.ll().get("failure"), "and the list draws no line of its own")
            skip("LIST-048", "\"Couldn't start the terminal.\" from a real failed start: runs after TERM merges")
    finally:
        with step("LIST-017-close", OUT):
            ask_close(main.wid)
            code = app.wait_exit(15)
            check(code is not None, f"the system close button on the last window quits the app (exit {code})")
            app.stop()


def fixture_run():
    apps.second_project()
    data = moved_fixture(FIXTURE, MOVED)
    stop_strays()
    if os.path.exists(SETTINGS):
        os.remove(SETTINGS)
    app, main = start_app(("--fixture", MOVED, "-s", "ready"), {"CRUSH_DESKTOP_TEST_PICK_FOLDER": PROJ2})
    w2 = None
    try:
        if wanted("list_shows_project_chats", "groups", "row_content", "state_marks", "server_rows", "server_look"):
            flow_list(main, data)
        if wanted("switch_marks_read", "scroll_keeps_active"):
            flow_switch(main)
        if wanted("collapse_strip", "strip", "collapse_remembered"):
            flow_collapse(main)
        if wanted("new_session_picker", "picker_layout", "picker_keys", "picker_prefill", "picker_matches", "picker_errors",
                  "picker_mouse", "picker_editing", "entry_new_session"):
            flow_picker(main)
        if wanted("cover_images"):
            flow_pictures(main)
        if wanted("flags_first_window", "spinner", "collapse_remembered"):
            w2 = flow_windows(app, main)
        if wanted("server_keys", "server_fold", "server_open", "server_stop"):
            flow_servers(main)
        if wanted("last_window_quits", "collapse_remembered"):
            flow_close(app, main, w2)
    finally:
        app.stop()
    if wanted("collapse_remembered", "start_failures", "last_window_quits"):
        flow_restart()
    skip("LIST-009", "Alt+Shift+↑/↓ and dragging terminal rows: run after TERM merges (no terminal tab can open before)")
    skip("LIST-055", "the pointer capture of a terminal row's drag: runs after TERM merges")


# --- The engine ------------------------------------------------------------------

class Engine:
    """The project's engine over its socket (the ground truth), from the
    window's connection section; still reachable after the window is gone."""

    def __init__(self, win):
        conn = win.section("connection")
        self.sock = (conn.get("engine") or {}).get("socket")
        self.ws = conn.get("workspace_id")

    def call(self, method, path, body=None):
        url = f"http://crush/v1/workspaces/{self.ws}/{path}" if not path.startswith("/") else f"http://crush{path}"
        args = ["curl", "--silent", "--fail", "--unix-socket", self.sock, "-X", method, url]
        if body is not None:
            args += ["-H", "Content-Type: application/json", "--data", json.dumps(body)]
        r = subprocess.run(args, capture_output=True, text=True, timeout=20)
        if r.returncode != 0:
            raise RuntimeError(f"{method} {path}: curl exit {r.returncode}")
        return json.loads(r.stdout or "null")

    def get(self, path):
        return self.call("GET", path)

    def servers(self):
        return self.get("servers") or []

    def session_ids(self):
        return {s["id"] for s in self.get("sessions") or []}


def panel_servers(win):
    """The right panel's SERVERS rows: their words and the rects of the name
    and the stop cross."""
    return (win.hv().get("servers") or {}).get("rows") or []


def composer_send(win, text):
    for _ in range(4):
        if win.section("composer").get("focused"):
            break
        key("Tab")
        time.sleep(0.3)
    type_text(text)
    ok, _ = poll(lambda: win.section("composer").get("value"), lambda v: v == text, 5)
    if ok:
        key("Return")
    return ok


def serve_from_desktop(main, engine, label):
    """A new chat in the window that starts a listening job: (chat id, port)."""
    main.focus()
    key("ctrl+n")
    ok, _ = poll(lambda: main.section("chat").get("id"), lambda c: not c, 3)
    check(ok, f"{label}: Ctrl+N starts a new chat")
    known = {s["port"] for s in engine.servers()}
    check(composer_send(main, "leftlist serve the folder"), f"{label}: the desktop sends the serve prompt")
    ok, sid = poll(lambda: main.section("chat").get("id"), bool, 15)
    ok, found = poll(engine.servers, lambda ss: any(s["session_id"] == sid and s["port"] not in known for s in ss), 25, 0.5)
    server = next((s for s in found or [] if s["session_id"] == sid and s["port"] not in known), None)
    check(ok and bool(server), f"{label}: the engine lists the job's port ({server})")
    # Ctrl+N waits for a working chat (the terminal's "busy" warning), so the
    # next chat starts once this reply ends.
    poll(lambda: engine.get(f"sessions/{sid}") if sid else {}, lambda s: not s.get("is_busy"), 20, 0.3)
    return sid, (server or {}).get("port"), server


def engine_run():
    stop_strays()
    apps.config({"permissions": {"allowed_tools": ["bash"]}})
    app, main = start_app()
    term = None
    try:
        engine = Engine(main)
        for _ in range(20):
            if not engine.get("project/needs-init").get("needs_init"):
                break
            main.focus()
            key("n")
            time.sleep(0.5)
        value = main.section("composer").get("value") or ""
        if value and set(value) == {"n"}:
            key(*["BackSpace"] * len(value))
        term = apps.Term("tui", PROJ)
        poll(term.text, lambda t: bool(t.strip()), 15, 0.3)

        if wanted("status_from_engine"):
            with step("LIST-018", OUT):
                before = {r["id"] for r in main.ll().get("rows") or []}
                # Typed once the terminal app's typing box shows, and sent once it holds the words.
                poll(term.text, lambda t: "›" in t, 15, 0.3)
                term.keys("-l", "leftlist slow reply")
                poll(term.text, lambda t: "leftlist slow reply" in t, 5, 0.2)
                term.keys("Enter")
                ok, sid = poll(lambda: next((r["id"] for r in main.ll()["rows"] if r["id"] not in before), ""), bool, 15)
                check(ok, f"a chat the terminal app starts is listed ({sid})" + ("" if ok else f"; the terminal shows {term.text().strip()[-400:]!r}"))
                ok, busy = poll(lambda: engine.get(f"sessions/{sid}"), lambda s: s.get("is_busy"), 10)
                t_busy = time.time()
                ok2 = main.until(lambda s: state_of(s, sid) == "working", 2.5)
                check(ok and bool(ok2), f"the desktop shows it working within 2 s of the engine ({time.time() - t_busy:.1f} s)")
                lst_row = next((r for r in main.section("list").get("rows") or [] if r["id"] == sid), {})
                check(lst_row.get("state") == "Working…", f"its row says Working… ({lst_row})")
                ok, _ = poll(lambda: engine.get(f"sessions/{sid}"), lambda s: not s.get("is_busy"), 30, 0.2)
                t_idle = time.time()
                ok2 = main.until(lambda s: state_of(s, sid) == "unread", 2.5)
                check(ok and bool(ok2), f"when it finishes, the desktop shows it Done within 2 s ({time.time() - t_idle:.1f} s)")
                # The agent line names the provider on the chat's record, which
                # only a CLI agent's context budget sets; the built-in fake
                # agent sets none, so the line stays empty.
                provider = ((engine.get(f"sessions/{sid}") or {}).get("context_budget") or {}).get("provider") or ""
                ok, lst_row = poll(lambda: next((r for r in main.section("list").get("rows") or [] if r["id"] == sid), {}),
                                   lambda r: r.get("agent") == ("Fake AI" if provider == "fake" else ""), 3)
                check(ok, f"and its agent line follows the record's provider ({lst_row.get('agent')!r}, provider {provider!r})")
                title = engine.get(f"sessions/{sid}").get("title")
                ok, _ = poll(lambda: (row_of(main.section("list"), sid) or {}).get("title"), lambda t: t == title, 3)
                check(ok, f"its title follows the engine's record ({title!r})")
                drawn = next((r for r in main.hv().get("chat_rows") or [] if r["id"] == sid), {})
                check(drawn.get("state_text") == "Done", "its state line reads Done")

        if wanted("server_rows", "server_keys", "server_open", "server_stop"):
            with step("LIST-032", OUT):
                sid, port, server = serve_from_desktop(main, engine, "a job")
                if port:
                    sec = main.until(lambda s: any(x["port"] == port for x in s["servers"]), 2.5)
                    check(bool(sec), f"its row shows within 2 s (:{port})")
                    # The engine names a command by its program until its job is
                    # registered, then by the job; the list follows within its
                    # next read, so the row may show the program name first.
                    def named():
                        name = next((x.get("name") or "" for x in engine.servers() if x["port"] == port), server.get("name") or "")
                        want = (name, f":{port}") if name else (f"localhost:{port}", "")
                        row = next((x for x in main.ll().get("servers") or [] if x["port"] == port), {})
                        return (row.get("label"), row.get("right")), want, name
                    ok, last = poll(named, lambda r: r[0] == r[1], 4, 0.3)
                    got, want, name = last or ((None, None), None, "")
                    check(ok, f"named after the job ({got[0]!r} {got[1]!r}, engine {name!r})")
                    shot(OUT, "LIST-028")
                    # The servers are the right panel's SERVERS since 2026-10-19:
                    # one click on the name opens it, the row's cross stops it.
                    opened0 = len(lines(f"{PROBE}.opened.jsonl"))
                    panel = next((r for r in panel_servers(main) if f":{port}" in r["text"]), {})
                    main.click(panel.get("open"))
                    ok, _ = poll(lambda: opened_urls(opened0), lambda u: u == [f"http://localhost:{port}"], 3)
                    check(ok, f"a click on its name opens http://localhost:{port}")
                    shot(OUT, "LIST-031")
                    check(listening(port), "the server answers on its port")
                    main.click(panel.get("stop"))
                    check(bool(main.until(lambda s: not any(x["port"] == port for x in s["servers"]), 1)), "its cross: the row leaves at once")
                    ok, _ = poll(engine.servers, lambda ss: not any(s["port"] == port for s in ss), 10, 0.5)
                    check(ok and not listening(port), "the engine killed the job: nothing listens on the port")
                    time.sleep(1.5)
                    check(not any(x["port"] == port for x in main.ll().get("servers") or []), "and the next reads keep it gone")
                    shot(OUT, "LIST-030")

        if wanted("window_close_q2", "quit_discards_draft", "last_window_quits"):
            with step("LIST-026", OUT):
                sid_a, port_a, _ = serve_from_desktop(main, engine, "chat A")
                sid_b, port_b, _ = serve_from_desktop(main, engine, "chat B")
                term.stop()
                term = apps.Term("tui", PROJ, args=("-s", sid_a))
                ok, _ = poll(term.text, lambda t: "Serving the folder." in t or "leftlist serve the folder" in t, 15, 0.3)
                check(ok, "the terminal app shows chat A")
                made = main.invoke("api", {"method": "POST", "path": "sessions", "body": {"title": "Empty draft"}})
                draft = (made.get("value") or {}).get("id")
                check(bool(draft), f"an empty chat exists ({draft})")
                ok = main.until(lambda s: row_of(s, draft), 5)
                rect = next((r["rect"] for r in main.hv().get("chat_rows") or [] if r["id"] == draft), None)
                main.click(rect)
                check(bool(main.until(lambda s: shown_id(s) == draft, 5)), "the desktop shows the empty draft")
                main.focus()
                key("ctrl+shift+w")
                code = app.wait_exit(15)
                closed_at = time.time()
                check(code is not None, f"Ctrl+Shift+W closes the window and, the last one, quits the app (exit {code})")
                ok, _ = poll(engine.session_ids, lambda ids: draft not in ids, 5, 0.3)
                check(ok, "the shown empty draft is discarded (MISC-024)")
                check(sid_a in engine.session_ids() and sid_b in engine.session_ids(), "chats with messages stay")
                if port_a and port_b:
                    ok, _ = poll(lambda: listening(port_b), lambda on: not on, 12, 0.5)
                    check(ok, f"the job of chat B, which no app shows, stops (:{port_b})")
                    time.sleep(max(0.0, closed_at + 12 - time.time()))
                    check(listening(port_a) and any(s["port"] == port_a for s in engine.servers()),
                          f"the job of chat A, which the terminal shows, keeps going past the 10 s grace (:{port_a})")
                    term.stop()
                    term = None
                    ok, _ = poll(lambda: listening(port_a), lambda on: not on, 15, 0.5)
                    check(ok, "when the terminal app leaves too, chat A's job stops")
    finally:
        if term:
            term.stop()
        apps.kill_terms()
        app.stop()


def main():
    if FIXTURE:
        fixture_run()
    else:
        engine_run()
    finish()


if __name__ == "__main__":
    main()
