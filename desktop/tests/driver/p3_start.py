"""P3.9's window check (desktop-design 2.5, LIST-012, LIST-033, LIST-034 and
CHAT-001's landing part): the chat list, the chat's head and the start
screens in the real window on the hidden screen, read through the probe's
start, list and gate sections (1.9) and pressed with the real keyboard and
mouse. Each step is named after its row and saves <ROW>.png (2.0).

Engine run, on the fresh /tmp/home/proj: tests/check.sh p3_start.py
  START-002     the fresh project asks first: the question, the folder, the
                initialize_as file, the hint, Initialize chosen; Tab, ← and → switch;
                y makes a chat that sends the engine's init prompt and marks
                the project; a restart asks no more.
  START-015     the data folder's .gitignore holds *.
  START-016     projects.json lists the project.
  START-002-no  a second fresh folder: n goes to the landing page and marks it;
                CHAT-001 there: the landing page is blank.
  LIST-033      Ctrl+N and New chat each save one empty "New Session" chat and
                show it; on a busy chat Ctrl+N does the same, with no warning.
  START-003     a new chat: the folder and branch, its model, and the
                LSP / MCP / Skills columns as the engine lists them.
  START-004     the new chat's head; the app's own version.
  START-005     no animation keeps running.
  LIST-034      opening a 500-item chat while the engine is held: "Opening
                <title>…" with the mark and the seconds after 1 s; a second
                switch wins once the engine answers.
  LIST-012      the terminal app (hidden tmux, 6.4) starts a chat and sends in
                it: the desktop's row says "Working…" within 1 s of the first
                streamed words in its pane and nothing within 1 s of the
                reply's end (p3_two_apps.py's LIST-012 step, on a chat seeded
                through the engine, so it runs before P3.7's composer exists).
  START-006     -s <id> opens that chat; -C the newest.
  START-021     -s takes a full hash or a hash prefix (the terminal's own
                hashes, crush session list --json); "session not found: abc"
                and `session ID "<p>" is ambiguous (N matches)` show as error
                notices with the chat-list search focused.
  START-008     --cwd /tmp/home/missing: the gate's body is the terminal's
                words and no engine starts. Then a crush whose engine start
                answers late holds "Starting Crush…" for the gate's captures.
  START-007     a broken MCP server says its error; LSPs, skills, the model and
                the branch still fill.
  START-017     each chat owns its model: a new folder's landing page shows the
                last pick, not a model the folder's crush.json names.
  START-019     the old "theme" setting reaches options.tui.active_theme.
  START-012     a fresh folder opened with --yolo=false (the first window on
                it, so its value holds): the engine asks before tools, and a
                prompt whose reply calls bash opens the permission slot.
  START-018     the desktop saves Customize Composer (from the meters' detail)
                while this driver, another client, posts a config/set of
                another key: both values are kept.

Fixture run: tests/check.sh --fixture tests/fixtures/list-states.json p3_start.py
  The app is started again on a copy of the fixture whose days are moved so
  its anchor day is today (its times are local clock times).
  LIST-012      the four states as the engine reports them, and a chat another
                app replies in (fixture events): "Working…", then nothing,
                keeping its place.
  LIST-days     every chat listed, no day groups, and the row times.
  LIST-search   Ctrl+K focuses the search ("Ctrl K" hint); it filters by
                title; "No chats match “zzz”."; Esc clears it.
  START-003     more LSPs and skills than fit: "…and N more", rows never run
                past the columns; the LSP and MCP words; the model line.
  CHAT-001      the landing block at two window widths.
  LIST-narrow   the chat list at its narrowest: no time touches its title
                (an S3 defect).
  LIST-short    a 600 px tall window: New chat and search keep their height
                and the rows scroll (an S3 defect).

Alignment (the align skill's screenshot checker on the real window, as
tests/look/align_real.py measures it): the steps save align-<surface>.png
and align-<surface>.json (the probe's marks); then, outside the sandbox
(the skill lives in the owner's home):
  python3 tests/driver/p3_start.py --align tests/out/p3_start
builds each surface's manifest with align_real.py's manifest_for() and runs
align.py --self-review on it.
"""
import importlib.util, json, os, re, shlex, shutil, signal, struct, subprocess, sys, threading, time, types, uuid
from datetime import datetime, timedelta, date

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, key, list_open_chat, probe_heavy, read_probe, shot, step, type_text, xdo

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(HERE))


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


# --- The list's rules, written from the design (an independent oracle) -------

WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
STATES = {"busy": "Working…", "permission": "Waiting for permission", "question": "Waiting for your answer", "background": "Background"}


def days_ago(at, now):
    return (datetime.fromtimestamp(now).date() - datetime.fromtimestamp(at).date()).days


def row_time(at, now):
    t, d = datetime.fromtimestamp(at), days_ago(at, now)
    if d <= 1:
        return f"{t.hour % 12 or 12}:{t.minute:02d} {'AM' if t.hour < 12 else 'PM'}"
    if d <= 7:
        return WEEKDAYS[(t.weekday() + 1) % 7]
    text = f"{MONTHS[t.month - 1]} {t.day}"
    return text if t.year == datetime.fromtimestamp(now).year else f"{text}, {t.year}"


def flat(text):
    return " ".join((text or "").split())


# --- --align (outside the sandbox) -------------------------------------------

def png_size(path):
    with open(path, "rb") as f:
        head = f.read(24)
    return struct.unpack(">II", head[16:24])


def align_main(out_dir):
    """Builds each saved surface's manifest the way tests/look/align_real.py
    does (manifest_for: icon and text margins, tight text boxes, full-height
    reference boxes) and runs the align skill's checker on it."""
    if "playwright.sync_api" not in sys.modules:
        try:
            import playwright.sync_api  # noqa: F401  (align_real.py imports it; only its Chromium half needs it)
        except ImportError:
            sys.modules["playwright"] = types.ModuleType("playwright")
            stub = types.ModuleType("playwright.sync_api")
            stub.sync_playwright = None
            sys.modules["playwright.sync_api"] = stub
    align_real = load("align_real", os.path.join(ROOT, "tests", "look", "align_real.py"))
    names = sorted(n[len("align-"):-len(".json")] for n in os.listdir(out_dir) if n.startswith("align-") and n.endswith(".json"))
    if not names:
        print(f"FAIL no align-*.json in {out_dir}; run the driver first")
        return 1
    worst = 0
    for name in names:
        with open(os.path.join(out_dir, f"align-{name}.json")) as f:
            saved = json.load(f)
        image = os.path.join(out_dir, f"align-{name}.png")
        marks = saved.get("marks") or {}
        if not marks.get("rows"):
            print(f"FAIL {name}: the probe gave no marks")
            worst = max(worst, 1)
            continue
        work = os.path.join(out_dir, f"align-{name}")
        os.makedirs(work, exist_ok=True)
        shutil.copyfile(image, os.path.join(work, os.path.basename(image)))
        collected = dict(marks, surface={"id": name, "name": saved.get("title") or name, "kind": "page", "selector": '[data-probe="shell"]'})
        manifest = align_real.manifest_for(collected, name, os.path.basename(image), png_size(image))
        with open(os.path.join(work, "align.json"), "w") as f:
            json.dump(manifest, f, indent=1)
        run = subprocess.run([sys.executable, os.path.join(align_real.ALIGN, "align.py"), os.path.join(work, "align.json"),
                              "--out", os.path.join(work, "results"), "--self-review"], capture_output=True, text=True)
        print(run.stdout + run.stderr, end="", flush=True)
        code = run.returncode
        # An element the checker could not measure is a hole in the coverage,
        # not a pass (the skill: inspect exclusions, the exit code alone does
        # not certify coverage); variable text is left out by rule and is not
        # listed here.
        dropped = []
        for line in run.stdout.splitlines():
            if line.startswith("{") and '"excluded_elements"' in line:
                dropped += json.loads(line).get("excluded_elements") or []
        for d in dropped:
            print(f"FAIL align {name}: {d.get('element')} could not be measured: {d.get('reason')}", flush=True)
        if dropped:
            code = max(code, 1)
        print(("PASS " if code == 0 else "FAIL ") + f"align {name} (exit {run.returncode}; {work}/results)", flush=True)
        worst = max(worst, code)
    return worst


if __name__ == "__main__" and sys.argv[1:2] == ["--align"]:
    sys.exit(align_main(sys.argv[2]))


# --- The run -----------------------------------------------------------------

probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
pc = load("p3_connect", os.path.join(HERE, "p3_connect.py"))
seed_chat = pc.seed_chat
AppRun, windows_of = pc.AppRun, pc.windows_of
PROJ, HOMEDIR = pc.PROJ, pc.HOME
CRUSH, APP = pc.CRUSH, pc.APP
TMUX = pc.TMUX
FIXTURE = (re.search(r"--fixture\s+(\S+)", os.environ.get("DESKTOP_ARGS", "")) or [None, None])[1]
INIT_DESC = "When I initialize your codebase I examine the project and put the result into an {} file which serves as general context."
BUSY_WARNING = "Agent is busy, please wait before starting a new session..."
LONG_REPLY = "Reconnect check: a long reply"  # scripts/stream2m.json: a reply of about 10-25 s
WIDTH, HEIGHT = 1600, 992


def app_version():
    out = subprocess.run([APP, "--version"], capture_output=True, text=True, timeout=30).stdout
    found = re.search(r"(\d+\.\d+\.\d+\S*)", out)
    return found.group(1) if found else None


def section(app, name):
    return app.section(name)


def wait_on(app, name, test, timeout=10):
    """(True, section) once test(section) holds; else (False, the last one)."""
    held, last = pc.wait_section(app, name, test, timeout)
    return bool(held), last or {}


def heavy(app, timeout=20):
    report = probe_heavy(app.probe, timeout)
    return report.get("heavy") or {}, report


def window(app):
    boxes = windows_of(app.pid) if app.pid else []
    return boxes[0] if boxes else None


def focus(app):
    """Gives the app's window the keyboard (no window manager on the hidden
    screen) and parks the pointer in its bottom corner, off every control."""
    box = window(app)
    if not box:
        return False
    wid, x, y, w, h = box
    xdo("windowraise", wid)
    xdo("windowfocus", wid)
    xdo("mousemove", x + w - 4, y + h - 4)
    time.sleep(0.2)
    return True


def place(app, w=WIDTH, h=HEIGHT):
    """Moves the window to the screen's corner at w x h and waits for the page to follow."""
    box = window(app)
    if not box:
        return False
    xdo("windowmove", box[0], 0, 0)
    xdo("windowsize", box[0], w, h)
    # The page sits under the window's own title bar, so it ends at the window's bottom edge.
    p = app.wait(lambda p: round((p.get("shell") or {}).get("w", 0)) == w and round((p.get("shell") or {}).get("y", 0) + (p.get("shell") or {}).get("h", 0)) == h, 10)
    time.sleep(0.4)
    return round((p.get("shell") or {}).get("w", 0)) == w


def zoom_of(app):
    return app.report().get("zoom") or 8 / 12


def joined(app, timeout=45):
    p = app.wait(lambda p: (p.get("connection") or {}).get("state") == "connected", timeout)
    return (p.get("connection") or {}).get("state") == "connected"


def engine_of(app, path, timeout=45):
    """The engine this window joined and its workspace id for path: each data
    folder (a project's .crush by default) has an engine of its own."""
    api = pc.engine_api(pc.join_outcome(app, timeout))
    return api, (pc.workspace_for(api, path) if api else None)


def screen_is(app, want, timeout=15):
    ok, start = wait_on(app, "start", lambda s: s.get("screen") == want, timeout)
    return ok, start.get("screen")


def rows(app):
    return section(app, "list").get("rows") or []


def row_rect(app, sid):
    h, _ = heavy(app)
    return next((r.get("rect") for r in (h.get("list") or {}).get("rows") or [] if r.get("id") == sid), None)


def click_row(app, sid):
    rect = row_rect(app, sid)
    return bool(rect) and app.click(rect)


def selected(app):
    return next((r.get("id") for r in rows(app) if r.get("selected")), None)


def save_align(app, surface, title, sec="start"):
    """The probe's marks for one surface and the window's capture, for --align."""
    h, report = heavy(app)
    marks = (h.get(sec) or {}).get("align")
    shot(out_dir, f"align-{surface}")
    with open(os.path.join(out_dir, f"align-{surface}.json"), "w") as f:
        json.dump({"title": title, "zoom": report.get("zoom"), "marks": marks}, f, indent=1)
    return check(bool(marks and marks.get("rows")), f"the {surface} surface's marks and capture are saved for --align ({len((marks or {}).get('rows') or [])} rows)")


def set_zoom(app, picture):
    """Picture size (12/12: four Ctrl = presses, as look_real.py does) or back
    to Compact (8/12, Ctrl 0)."""
    focus(app)
    if picture:
        for _ in range(4):
            key("ctrl+equal")
    else:
        key("ctrl+0")
    want = 1.0 if picture else 8 / 12
    p = app.wait(lambda p: p.get("zoom") is not None and abs(p["zoom"] - want) < 1e-6, 10)
    time.sleep(0.5)
    return p.get("zoom") is not None and abs(p["zoom"] - want) < 1e-6


def save_align_both(app, surface, title, sec="start"):
    """save_align() at Compact, then at picture size."""
    save_align(app, surface, title, sec)
    if check(set_zoom(app, True), f"{surface}: picture size (12/12)"):
        save_align(app, f"{surface}-picture", f"{title} at picture size", sec)
    check(set_zoom(app, False), f"{surface}: back to Compact (8/12)")


def close(app):
    if app and app.pid and app.alive():
        app.close()


# --- The engine, as a client of its own (it keeps the project open) ----------

class Client:
    """This driver joined to a project as its own client, the API calls
    seed_chat.py makes, so the engine keeps the project open between the
    window's restarts. The workspace object holds the login environment and is
    never printed."""

    def __init__(self, api, path):
        self.api, self.path = api, path
        self.id = str(uuid.uuid4())
        env = ["%s=%s" % kv for kv in os.environ.items()]
        ws = api.call("POST", "/v1/workspaces", {"path": path, "data_dir": "", "version": "p3_start", "protocol": 1,
                                                 "client_id": self.id, "yolo": True, "env": env})
        self.wid = ws["id"]
        del ws
        self.events = api.follow_events(f"/v1/workspaces/{self.wid}/events?client_id={self.id}")

    def get(self, path):
        return self.api.call("GET", f"/v1/workspaces/{self.wid}/{path}")

    def post(self, path, body=None):
        return self.api.call("POST", f"/v1/workspaces/{self.wid}/{path}?client_id={self.id}", body)

    def sessions(self):
        return self.get("sessions") or []

    def messages(self, sid):
        out = []
        for m in self.get(f"sessions/{sid}/messages") or []:
            parts = m.get("parts") or []
            text = "".join((p.get("data") or {}).get("text", "") for p in parts if p.get("type") == "text")
            out.append({"role": m.get("role"), "text": text, "finished": any(p.get("type") == "finish" for p in parts)})
        return out

    def leave(self):
        try:
            self.api.call("DELETE", f"/v1/clients/{self.id}")
        except Exception:  # noqa: BLE001 - leaving is best effort
            pass
        self.events.close()


def poll(read, ok, timeout, every=0.1):
    start = time.time()
    while True:
        value = read()
        if ok(value):
            return True, value, time.time() - start
        if time.time() - start > timeout:
            return False, value, time.time() - start
        time.sleep(every)


def tmux(*args):
    return subprocess.run(TMUX + list(args), capture_output=True, text=True)


def pane():
    r = tmux("capture-pane", "-p", "-t", "tui")
    return r.stdout if r.returncode == 0 else ""


def script_reply(when):
    with open(os.path.join(ROOT, "tests", "fakes", "scripts", "stream2m.json")) as f:
        for turn in json.load(f)["turns"]:
            if turn["when"] == when:
                return "".join(s.get("text", "") for s in turn["steps"])
    raise SystemExit(f"stream2m.json has no turn {when!r}")


def edit_config(path, change):
    with open(os.path.join(path, "crush.json")) as f:
        cfg = json.load(f)
    change(cfg)
    with open(os.path.join(path, "crush.json"), "w") as f:
        json.dump(cfg, f, indent=1)


def answer_no(app):
    """START-002 on a fresh folder: n answers No (the step for Yes is START-002 itself)."""
    if not screen_is(app, "init", 20)[0]:
        return False
    focus(app)
    key("n")
    return screen_is(app, "landing", 15)[0]


def list_edges(h, label):
    """The align skill's companion geometry check (its glyph checker does not
    compare backgrounds): every row's button, whose background is the open
    chat's band, spans the column's full width, and New chat and the search
    share both edges."""
    lh = h.get("list") or {}
    col = lh.get("column") or {}
    rects = [(r["id"], r["band"]) for r in lh.get("rows") or [] if r.get("band")]
    off = [rid for rid, r in rects if not col or abs(r["x"] - col["x"]) > 0.5 or abs(r["x"] + r["w"] - col["x"] - col["w"]) > 0.5]
    check(bool(rects) and not off, f"{label}: every row and the open chat's band span the list's full width {off or ''}")
    # New chat and New terminal share one row (owner, 2026-10-20): New chat's
    # left edge and New terminal's right edge are the search's.
    nc, nt, sr = lh.get("new_chat") or {}, lh.get("new_terminal") or {}, lh.get("search") or {}
    check(bool(nc and nt and sr) and abs(nc["x"] - sr["x"]) <= 0.5 and abs(nt["x"] + nt["w"] - sr["x"] - sr["w"]) <= 0.5,
          f"{label}: New chat and New terminal span the search's edges ({nc.get('x')}, {sr.get('x')}; {nt.get('x', 0) + nt.get('w', 0)}, {sr.get('x', 0) + sr.get('w', 0)})")


def ask_state(app, path):
    """What the folder's engine holds for the window's shown chat: its
    messages' roles and finishes, for a failed permission check."""
    api, wid = engine_of(app, path, 5)
    sid = selected(app)
    if not (api and wid and sid):
        return f"no engine or chat ({wid}, {sid})"
    msgs = api.call("GET", f"/v1/workspaces/{wid}/sessions/{sid}/messages") or []
    chats = [(c.get("id"), c.get("title")) for c in api.call("GET", f"/v1/workspaces/{wid}/sessions") or []]
    return {"chats": chats, "messages": [(m.get("role"), [p.get("type") for p in m.get("parts") or []][:6]) for m in msgs]}


def landing_checks(app, label):
    """CHAT-001 since the landing page went blank (owner, 2026-10-19): it
    reports itself blank and draws no block, and START-005."""
    h, report = heavy(app)
    st = h.get("start") or {}
    landing = section(app, "start").get("landing")
    check(landing == {"blank": True}, f"{label}: the landing page is blank ({landing})")
    check(not (st.get("landing") or {}).get("content"), f"{label}: it draws no landing block ({(st.get('landing') or {}).get('content')})")
    return h


def panel_rows(app, name):
    """The panel's rows of one section (lsp, mcp, skills) as "name text"
    lines: where the window lists the engine's LSPs, MCP servers and skills
    since the landing page went blank (the details overlay draws them)."""
    sec = next((s for s in section(app, "panel").get("sections") or [] if s.get("name") == name), {}) or {}
    return [" ".join(x for x in (str(r.get("name") or ""), str(r.get("text") or "")) if x).strip() if isinstance(r, dict) else str(r)
            for r in sec.get("rows") or []]


# --- Engine steps ------------------------------------------------------------

def engine_run():
    tmux("kill-server")
    main = AppRun.existing(probe_path, PROJ)
    extra = []
    client = None
    init_chat = big = short = None
    version = app_version()
    try:
        with step("START-002", out_dir):
            outcome = pc.join_outcome(main, 45)
            if not check(outcome.get("state") == "connected", f"the run's app joins the engine ({outcome.get('state')}, {outcome.get('error')})"):
                check(False, "the remaining steps need the engine; stopping here")
                return
            api = pc.engine_api(outcome)
            client = Client(api, PROJ)
            ok, screen = screen_is(main, "init", 20)
            check(ok, f"the fresh project asks to initialize first (screen {screen})")
            init = section(main, "start").get("init") or {}
            initialize_as = ((client.get("config") or {}).get("options") or {}).get("initialize_as") or "AGENTS.md"
            want = {"question": "Would you like to initialize this project?", "path": PROJ, "desc": INIT_DESC.format(initialize_as),
                    "hint": "You can also initialize anytime via Ctrl+P.", "ask": "Would you like to initialize now?",
                    "buttons": ["Initialize", "Not now"], "selected": "yes"}
            for field, value in want.items():
                check(init.get(field) == value, f"init {field}: {init.get(field)!r}")
            place(main)
            save_align_both(main, "init", "The init prompt")
            h, _ = heavy(main)
            yes, no = (((h.get("start") or {}).get("init") or {}).get(k) or {} for k in ("yes", "no"))
            check(bool(yes and no) and abs(yes["y"] - no["y"]) <= 0.5 and abs(yes["h"] - no["h"]) <= 0.5 and abs(yes["w"] - no["w"]) <= 0.5,
                  f"the two buttons share their top and bottom edges and width ({yes}, {no})")
            focus(main)
            for k, after in (("Tab", "no"), ("Left", "yes"), ("Right", "no"), ("Tab", "yes")):
                key(k)
                ok, init = wait_on(main, "start", lambda s, a=after: (s.get("init") or {}).get("selected") == a, 3)
                check(ok, f"{k} switches the chosen button to {after}")
            prompt = (client.get("project/init-prompt") or {}).get("prompt") or ""
            before = len(client.sessions())
            key("y")
            ok, chats, took = poll(client.sessions, lambda s: len(s) > before, 15)
            check(ok and len(chats) == before + 1, f"y makes one new chat ({before} → {len(chats)}, {took:.1f} s)")
            sid = chats[0]["id"] if ok else None
            if sid:
                ok, msgs, _ = poll(lambda: client.messages(sid), lambda ms: any(m["role"] == "user" for m in ms), 15)
                first = next((m["text"] for m in msgs if m["role"] == "user"), "")
                check(ok and flat(first) == flat(prompt) and len(prompt) > 40, f"and sends the engine's init prompt in it ({len(first)} of {len(prompt)} characters)")
                ok, _, _ = poll(lambda: selected(main), lambda s: s == sid, 10)
                check(ok and section(main, "start").get("screen") == "chat", "the window shows that chat")
            check(not (client.get("project/needs-init") or {}).get("needs_init"), "the project is marked initialized")
            close(main)
            main = AppRun("restart", out_dir, PROJ)
            extra.append(main)
            check(joined(main), "the window starts again on the project")
            ok, start = wait_on(main, "start", lambda s: s.get("needs_init") is False and s.get("screen") in ("landing", "chat"), 15)
            time.sleep(1.5)
            check(ok and section(main, "start").get("screen") != "init", f"and does not ask again (screen {section(main, 'start').get('screen')})")
            init_chat = sid

        with step("START-015", out_dir):
            path = os.path.join(PROJ, ".crush", ".gitignore")
            text = open(path).read() if os.path.exists(path) else None
            check(text is not None and "*" in text.split(), f"{path} holds * ({text!r})")

        with step("START-016", out_dir):
            found = []
            for base, _, files in os.walk(HOMEDIR):
                if "projects.json" in files:
                    found.append(os.path.join(base, "projects.json"))
            entries = []
            for p in found:
                try:
                    entries += (json.load(open(p)) or {}).get("projects") or []
                except ValueError:
                    pass
            mine = next((e for e in entries if e.get("path") == PROJ), None)
            check(bool(mine) and bool(mine.get("data_dir")) and "last_accessed" in mine,
                  f"projects.json ({', '.join(found) or 'none'}) lists {PROJ} with its data_dir and last_accessed ({mine})")

        with step("START-002-no", out_dir):
            other = pc.make_project(os.path.join(HOMEDIR, "proj-no"), port)
            app = AppRun("init-no", out_dir, other)
            extra.append(app)
            check(joined(app), "a window on a second fresh folder joins")
            ok = answer_no(app)
            check(ok, "n goes to the landing page")
            other_api, wid = engine_of(app, other)
            needs = (other_api.call("GET", f"/v1/workspaces/{wid}/project/needs-init") if wid else None) or {}
            check(bool(wid) and needs.get("needs_init") is False, f"and the folder is marked initialized ({needs})")
            check(len(other_api.call("GET", f"/v1/workspaces/{wid}/sessions") or []) == 0 if wid else False, "and no chat was made")
            # CHAT-001 with the engine: New chat now opens a saved chat, so a
            # folder with no chat is where the landing page still shows.
            place(app)
            landing_checks(app, "engine landing at 1600 x 992")
            close(app)

        with step("LIST-033", out_dir):
            place(main)
            if section(main, "start").get("screen") != "chat":
                click_row(main, init_chat)
            ok, _, _ = poll(lambda: selected(main), lambda s: s == init_chat, 10)
            check(ok, "the init chat is shown")
            # New chat saves an empty "New Session" chat at once and shows it,
            # so leaving it keeps its row (owner, 2026-10-23).
            count = len(client.sessions())
            focus(main)
            key("ctrl+n")
            ok, shown, _ = poll(lambda: (section(main, "start").get("screen"), selected(main)), lambda v: v[0] == "chat" and v[1] not in (None, init_chat), 5)
            first = shown[1]
            check(ok, f"Ctrl+N shows a new chat ({shown})")
            click_row(main, init_chat)
            screen_is(main, "chat", 10)
            h, _ = heavy(main)
            new_chat = (h.get("list") or {}).get("new_chat")
            check(bool(new_chat) and main.click(new_chat), "New chat is clicked")
            # The button asks where to start, this window's folder first
            # (owner, 2026-10-20); Ctrl+N starts here at once.
            ok, _ = wait_on(main, "overlay", lambda o: "folder_picker" in (o.get("dialogs") or []), 5)
            check(ok, "it asks which folder")
            key("Return")
            ok, shown, _ = poll(lambda: (section(main, "start").get("screen"), selected(main)), lambda v: v[0] == "chat" and v[1] not in (None, init_chat, first), 5)
            check(ok, f"New chat shows another new chat ({shown})")
            made = [s for s in client.sessions() if s.get("id") in (first, shown[1])]
            check(len(client.sessions()) == count + 2 and len(made) == 2 and all(s.get("title") == "New Session" and not s.get("message_count") for s in made),
                  f"each made one empty \"New Session\" record ({count} → {len(client.sessions())}: {[(s.get('title'), s.get('message_count')) for s in made]})")
            # Busy: a long reply runs in the shown chat.
            click_row(main, init_chat)
            screen_is(main, "chat", 10)
            client.post("agent", {"session_id": init_chat, "prompt": LONG_REPLY})
            ok, _, _ = poll(lambda: next((r for r in rows(main) if r.get("id") == init_chat), {}), lambda r: r.get("state") == "Working…", 15)
            check(ok, "the shown chat is busy (its row says Working…)")
            focus(main)
            key("ctrl+n")
            # A working chat no longer blocks a new one (owner, 2026-10-21).
            ok, shown, _ = poll(lambda: (section(main, "start").get("screen"), selected(main)), lambda v: v[0] == "chat" and v[1] not in (None, init_chat), 5)
            check(ok and (section(main, "overlay").get("notice") or {}).get("text") != BUSY_WARNING, f"Ctrl+N on a busy chat shows a new chat, no busy warning ({shown})")
            ok, _, _ = poll(lambda: next((r for r in rows(main) if r.get("id") == init_chat), {}), lambda r: r.get("state") == "Working…", 5)
            check(ok, "and the busy chat keeps working in the list")
            client.post(f"agent/sessions/{init_chat}/cancel")
            ok, _, _ = poll(lambda: next((r for r in rows(main) if r.get("id") == init_chat), {}), lambda r: not r.get("state"), 20)
            check(ok, "once the run is cancelled the row has no state")

        with step("START-003", out_dir):
            # A new chat opens empty; the engine's project data still reaches
            # the window: the branch in the top bar, the model, and the LSP /
            # MCP / skills lists of the panel.
            focus(main)
            key("ctrl+n")
            ok, start = wait_on(main, "start", lambda s: s.get("screen") == "chat" and (s.get("chat_model") or {}).get("model"), 10)
            check(ok, f"Ctrl+N opens a new chat with its model ({start.get('screen')}, {start.get('chat_model')})")
            branch = (client.get("git/branch") or {}).get("branch") or ""
            check(start.get("branch") == branch, f"the window has the engine's branch: {start.get('branch')!r} (engine {branch!r})")
            check((start.get("chat_model") or {}).get("model") == "fake-large", f"the landing page shows the last pick, fake-large: {start.get('chat_model')}")
            engine_counts = {"lsp": len(client.get("lsps") or {}), "mcp": len(client.get("mcp/states") or {}), "skills": len(client.get("skills") or [])}
            for k, n in engine_counts.items():
                got = len(panel_rows(main, k))
                check(got == n, f"the panel's {k} rows: {got} = the engine's {n}")

        with step("START-004", out_dir):
            # The new chat's head names it; the wordmark and "Charm™ ·
            # v<version>" stay on the init and onboarding screens.
            head = section(main, "start").get("head") or {}
            check(head.get("title") == "New Session" and "wordmark" not in head, f"the new chat's head says New Session, with no wordmark ({head})")
            check(bool(version), f"the app knows its own version (crush-desktop --version {version})")
            check(main.report().get("heading") == "Crush AI", f"the list's wordmark is still the page's heading ({main.report().get('heading')!r})")

        with step("START-005", out_dir):
            # A one-off ease (a meter's bar sliding to a new value, 400 ms)
            # may be caught mid-way; nothing keeps running.
            ok, anims, _ = poll(lambda: (heavy(main)[0].get("start") or {}).get("animations"), lambda a: a == [], 3, 0.3)
            check(ok, f"no animation keeps running on the landing page ({anims})")

        with step("LIST-034", out_dir):
            sock = (section(main, "connection").get("engine") or {}).get("socket")
            seeded = {}
            for title, how in (("Five hundred items", ["--items", "500"]), ("Short chat", ["--turns", "1"])):
                out = subprocess.run([sys.executable, "-I", os.path.join(ROOT, "tests", "fakes", "seed_chat.py"), "--cwd", PROJ, "--socket", sock,
                                      "--title", title, *how], capture_output=True, text=True, timeout=300)
                info = json.loads(out.stdout or "{}") if out.returncode == 0 else {}
                seeded[title] = info.get("session_id")
                check(bool(info.get("session_id")), f"seeded \"{title}\" ({info.get('messages')} items, {info.get('seconds')} s) {out.stderr.strip()[-200:]}")
            big, short = seeded.get("Five hundred items"), seeded.get("Short chat")
            # The list draws the open chats (owner, 2026-10-21), and an open
            # chat loads behind the shown one so a click shows it at once. The
            # seeded chats are listed the way the terminal lists one it opens,
            # only once the engine is held, so neither is loaded before its
            # click and the opening line shows.
            pid = (section(main, "connection").get("engine") or {}).get("pid")
            if check(bool(pid), f"the engine's pid ({pid})"):
                os.kill(pid, signal.SIGSTOP)
                try:
                    for title, sid in seeded.items():
                        if sid:
                            list_open_chat({"id": sid, "dir": PROJ, "title": title})
                    ok, _, _ = poll(lambda: {r.get("id"): r.get("title") for r in rows(main)}, lambda r: r.get(big) == "Five hundred items" and r.get(short) == "Short chat", 15)
                    check(ok, f"both chats reach the list with their titles ({[(r.get('id'), r.get('title')) for r in rows(main)]})")
                    rect_big, rect_short = row_rect(main, big), row_rect(main, short)
                    check(bool(rect_big and rect_short), "both rows have rects")
                    main.click(rect_big)
                    ok, start = wait_on(main, "start", lambda s: (s.get("opening") or {}).get("label") == "Opening Five hundred items…", 5)
                    check(ok and start.get("screen") == "opening", f"\"Opening Five hundred items…\" shows while the engine is held ({start.get('opening')})")
                    head = start.get("head") or {}
                    check(head.get("title") == "Five hundred items", f"the head names the chat being opened ({head.get('title')!r})")
                    ok, start = wait_on(main, "start", lambda s: re.fullmatch(r"[1-9]\d*s", (s.get("opening") or {}).get("elapsed") or "") is not None, 4)
                    check(ok, f"after 1 s the seconds show ({(start.get('opening') or {}).get('elapsed')!r})")
                    save_align_both(main, "opening", "The opening line")
                    check((section(main, "start").get("opening") or {}).get("label") == "Opening Five hundred items…", "the line still shows after the captures")
                    main.click(rect_short)
                    ok, start = wait_on(main, "start", lambda s: (s.get("opening") or {}).get("label") == "Opening Short chat…", 5)
                    check(ok, f"a second switch takes the line: {(start.get('opening') or {}).get('label')!r}")
                finally:
                    os.kill(pid, signal.SIGCONT)
                ok, _, _ = poll(lambda: (selected(main), section(main, "start").get("screen")), lambda v: v == (short, "chat"), 20)
                check(ok, "once the engine answers, the newer switch's chat shows")
                time.sleep(3)
                check(selected(main) == short and section(main, "start").get("screen") == "chat", f"and stays: the older switch never shows last ({selected(main)})")
                main.click(row_rect(main, big))
                ok, _, took = poll(lambda: (selected(main), section(main, "start").get("screen")), lambda v: v == (big, "chat"), 20)
                check(ok, f"the 500-item chat opens ({took:.1f} s)")

        with step("LIST-012", out_dir):
            pc.set_active_script(port, "stream2m")
            reply = flat(script_reply("from the terminal"))
            words = " ".join(reply.split()[:3])
            shown = selected(main) or short
            started = tmux("new-session", "-d", "-s", "tui", "-x", "200", "-y", "50",
                           f"env CRUSH_CLIENT_SERVER=1 {CRUSH} --cwd {PROJ} -s {shown}")
            check(started.returncode == 0, f"the terminal app starts in a hidden tmux ({started.stderr.strip()})")
            time.sleep(4)
            before = {r.get("id") for r in rows(main)}
            tmux("send-keys", "-t", "tui", "C-n")
            time.sleep(1)
            tmux("send-keys", "-t", "tui", "-l", "from the terminal")
            tmux("send-keys", "-t", "tui", "Enter")
            t_first = t_working = t_end = t_idle = None
            new_id = None
            deadline = time.time() + 45
            while time.time() < deadline and t_idle is None:
                now = time.time()
                if t_first is None and words in flat(pane()):
                    t_first = now
                new = [r for r in rows(main) if r.get("id") not in before]
                if new:
                    new_id = new[0].get("id")
                    working = "Working…" in (new[0].get("state") or "")
                    if working and t_working is None:
                        t_working = now
                    if t_end is None:
                        msgs = client.messages(new_id)
                        if any(m["role"] == "assistant" and m["finished"] and flat(m["text"]) == reply for m in msgs):
                            t_end = now
                    if t_end is not None and not working:
                        t_idle = now
                time.sleep(0.1)
            with open(os.path.join(out_dir, "LIST-012.pane.txt"), "w") as f:
                f.write(pane())
            check(new_id is not None, "the desktop's list gains the terminal's new chat")
            check(t_first is not None and t_working is not None and t_working <= t_first + 1.0,
                  f"its row says \"Working…\" within 1 s of the first streamed words in the pane "
                  f"({None if t_first is None or t_working is None else round(t_working - t_first, 2)} s)")
            check(t_end is not None and t_idle is not None and t_idle <= t_end + 1.0,
                  f"and no state within 1 s of the reply's end ({None if t_end is None or t_idle is None else round(t_idle - t_end, 2)} s)")
            tmux("kill-server")

        with step("START-006", out_dir):
            close(main)
            app = AppRun("launch-s", out_dir, PROJ, args=["-s", short])
            extra.append(app)
            check(joined(app), "a window with -s <id> joins")
            ok, _, _ = poll(lambda: (selected(app), section(app, "start").get("screen")), lambda v: v == (short, "chat"), 15)
            check(ok, f"-s {short} opens that chat ({selected(app)})")
            check((section(app, "start").get("head") or {}).get("title") == "Short chat", "its head shows its title")
            close(app)
            newest = max(client.sessions(), key=lambda s: s.get("updated_at") or 0)
            main = AppRun("launch-c", out_dir, PROJ, args=["-C"])
            extra.append(main)
            check(joined(main), "a window with -C joins")
            ok, _, _ = poll(lambda: selected(main), lambda s: s == newest["id"], 15)
            check(ok, f"-C opens the newest chat, \"{newest.get('title')}\" ({selected(main)})")

        with step("START-021", out_dir):
            n = 0
            while len(client.sessions()) < 17:
                n += 1
                client.post("sessions", {"title": f"Hash check {n}"})
            listed = subprocess.run([CRUSH, "session", "list", "--json", "--cwd", PROJ], capture_output=True, text=True, timeout=60,
                                    env=dict(os.environ, CRUSH_DISABLE_METRICS="1"))
            hashes = {e.get("uuid"): e.get("id") for e in json.loads(listed.stdout or "[]") or []}
            check(len(hashes) >= 17, f"the terminal lists the project's chats with their hashes ({len(hashes)}; {listed.stderr.strip()[-200:]})")
            by_first = {}
            for sid, h in hashes.items():
                by_first.setdefault(h[0], []).append(sid)
            shared, ids = max(by_first.items(), key=lambda kv: len(kv[1]))
            target = short if short in hashes else next(iter(hashes))
            target_hash = hashes.get(target, "")
            prefix = next((target_hash[:k] for k in range(4, 17) if sum(h.startswith(target_hash[:k]) for h in hashes.values()) == 1), target_hash)
            missing = next(p for p in ("abc", "abd", "abe", "abf") if not any(h.startswith(p) for h in hashes.values()))
            close(main)
            cases = [
                ("prefix", prefix, target, None),
                ("hash", target_hash, target, None),
                ("missing", missing, None, f"session not found: {missing}"),
                ("ambiguous", shared, None, f'session ID "{shared}" is ambiguous ({len(ids)} matches)'),
            ]
            for name, value, opens, error in cases:
                app = AppRun(f"hash-{name}", out_dir, PROJ, args=["-s", value])
                extra.append(app)
                if not check(joined(app), f"-s {value} ({name}): the window joins"):
                    continue
                if opens:
                    ok, _, _ = poll(lambda: selected(app), lambda s: s == opens, 15)
                    check(ok, f"-s {value} ({name}) opens that chat ({selected(app)})")
                else:
                    ok, overlay = wait_on(app, "overlay", lambda o: (o.get("notice") or {}).get("text") == error, 15)
                    check(ok and (overlay.get("notice") or {}).get("kind") == "error", f"-s {value}: the error notice says {error!r} ({overlay.get('notice')})")
                    ok, _ = wait_on(app, "overlay", lambda o: "sessions" in (o.get("dialogs") or []), 5)
                    check(ok, "and the Sessions sheet opens with the ID in its filter (the search is a button since 2026-10-20)")
                    check(selected(app) is None and section(app, "start").get("screen") == "landing", "and no chat opened")
                    if name == "ambiguous":
                        time.sleep(6)
                        check((section(app, "overlay").get("notice") or {}).get("text") == error, "the error stays past the usual 5 s")
                close(app)
            main = AppRun("after-hash", out_dir, PROJ)
            extra.append(main)
            joined(main)

        with step("START-008", out_dir):
            before = pc.engines()
            app = AppRun("missing", out_dir, os.path.join(HOMEDIR, "missing"))
            extra.append(app)
            want = "failed to change directory: chdir /tmp/home/missing: no such file or directory"
            ok, gate = wait_on(app, "gate", lambda g: g.get("state") == "failed", 20)
            check(ok and flat(gate.get("body")) == want, f"the gate's body says the terminal's words ({gate.get('body')!r})")
            check(gate.get("title") == "Crush could not open this project" and (gate.get("button") or {}).get("label") == "Try again" and gate.get("starting") is False,
                  f"with its title and Try again ({gate.get('title')!r}, {gate.get('button')})")
            check(pc.engines() == before, "and no engine starts")
            place(app)
            save_align_both(app, "gate", "The engine gate", "gate")
            close(app)
            # The gate's other look, for its captures: "Starting Crush…" with the
            # mark, held by a crush whose engine start answers 12 s late (under
            # the app's 20 s limit, engine_cli.rs ENSURE_LIMIT).
            slow = pc.wrapper(os.path.join(HOMEDIR, "slow-start", "crush"),
                              "# A crush whose engine start answers 12 s late (p3_start.py, START-008's captures).\n"
                              f"[ \"$1 $2\" = \"engine ensure\" ] && sleep 12\nexec {shlex.quote(CRUSH)} \"$@\"\n")
            app = AppRun("starting", out_dir, pc.make_project(os.path.join(HOMEDIR, "proj-slow"), port), env={"CRUSH_DESKTOP_CRUSH": slow})
            extra.append(app)
            ok, gate = wait_on(app, "gate", lambda g: g.get("starting") is True, 10)
            if check(ok, f"a slow engine start shows \"Starting Crush…\" (gate.starting {gate.get('starting')})"):
                place(app)
                save_align_both(app, "gate-starting", "The gate's starting line", "gate")
                check(section(app, "gate").get("starting") is True, "it still shows after the captures (they are of that state)")
            close(app)

        with step("START-007", out_dir):
            path = pc.make_project(os.path.join(HOMEDIR, "proj-mcp"), port)
            edit_config(path, lambda c: c.update({
                "mcp": {"broken": {"type": "stdio", "command": os.path.join(HOMEDIR, "no-such-mcp-server")}},
                "lsp": {"fakelsp": {"command": sys.executable, "args": [os.path.join(ROOT, "tests", "fakes", "fake_lsp.py")], "filetypes": ["go"]}},
            }))
            subprocess.run(["git", "-C", path, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "start"], check=False)
            app = AppRun("broken-mcp", out_dir, path)
            extra.append(app)
            check(joined(app), "a window on a project with a broken MCP server joins")
            check(answer_no(app), "its init prompt is answered (No)")
            mcp_api, wid = engine_of(app, path)
            check(bool(wid), f"the window's engine has the project ({wid})")
            eng = lambda p: mcp_api.call("GET", f"/v1/workspaces/{wid}/{p}") if wid else None
            _, broken, _ = poll(lambda: (eng("mcp/states") or {}).get("broken") or {}, lambda b: b.get("state") == "error", 20, 0.3)
            want = f"broken error: {broken.get('error')}".lower()
            ok, _ = wait_on(app, "panel", lambda p: any(want == r.lower() for r in panel_rows(app, "mcp")), 20)
            check(ok and bool(broken.get("error")), f"the broken server's row says its error as the engine has it ({panel_rows(app, 'mcp')}; engine {broken.get('error')!r})")
            check(any(r.startswith("fakelsp") for r in panel_rows(app, "lsp")), f"the LSP rows fill ({panel_rows(app, 'lsp')})")
            skills = eng("skills") or []
            check(len(panel_rows(app, "skills")) == len(skills), f"the skills rows have the engine's {len(skills)} skills")
            start = section(app, "start")
            check((start.get("chat_model") or {}).get("model") == "fake-large", f"the model fills ({start.get('chat_model')})")
            branch = (eng("git/branch") or {}).get("branch") or ""
            check(bool(branch) and start.get("branch") == branch, f"the branch fills ({start.get('branch')!r}, engine {branch!r})")
            close(app)

        with step("START-017", out_dir):
            # Each chat owns its model (owner, 2026-10-24): a new chat starts on
            # the last pick, never on a model a folder's config names. The
            # project's crush.json (tests/project) still names fake-large under
            # the old "large" key, which is only read once to seed a first pick.
            kept = client.get("last-pick")
            client.post("last-pick", {"model": "fake-small", "provider": "fake"})
            try:
                path = pc.make_project(os.path.join(HOMEDIR, "proj-models"), port)
                app = AppRun("models", out_dir, path)
                extra.append(app)
                check(joined(app), "a window on a project whose crush.json names fake-large joins")
                answer_no(app)
                ok, start = wait_on(app, "start", lambda s: (s.get("chat_model") or {}).get("model"), 10)
                check((start.get("chat_model") or {}).get("model") == "fake-small",
                      f"its landing page shows the last pick, fake-small, not the folder's fake-large ({start.get('chat_model')})")
                close(app)
            finally:
                if kept:
                    client.post("last-pick", kept)

        with step("START-019", out_dir):
            path = pc.make_project(os.path.join(HOMEDIR, "proj-theme"), port)
            edit_config(path, lambda c: c.setdefault("options", {}).update({"tui": {"theme": "gruvbox-dark"}}))
            app = AppRun("theme", out_dir, path)
            extra.append(app)
            check(joined(app), "a window on a project with the old \"theme\" setting joins")
            ok, start = wait_on(app, "start", lambda s: s.get("active_theme") is not None, 10)
            check(start.get("active_theme") == "gruvbox-dark", f"options.tui.active_theme holds it ({start.get('active_theme')!r})")
            close(app)

        with step("START-012", out_dir):
            pc.set_active_script(port, "stream2m")
            path = pc.make_project(os.path.join(HOMEDIR, "proj-ask"), port)
            app = AppRun("ask", out_dir, path, args=["--yolo=false"])
            extra.append(app)
            check(joined(app), "a window with --yolo=false on a fresh folder joins (the first on it, so its value holds)")
            check(answer_no(app), "its init prompt is answered (No)")
            ok, comp = wait_on(app, "composer", lambda c: c.get("yolo") is False, 10)
            check(ok, f"the engine asks before tools (composer.yolo {comp.get('yolo')})")
            focus(app)
            ok, comp = wait_on(app, "composer", lambda c: c.get("focused") is True, 5)
            if check(ok, "the typing box has the keyboard"):
                type_text("START-012 permission check")
                ok, _ = wait_on(app, "composer", lambda c: c.get("value") == "START-012 permission check", 5)
                check(ok, "the prompt is typed")
                key("Return")
                ok, comp = wait_on(app, "composer", lambda c: (c.get("slot") or {}).get("kind") == "permission", 20)
                shot(out_dir, "START-012")
                check(ok, f"the reply's bash call opens the permission slot ({comp.get('slot')}; screen {section(app, 'start').get('screen')}, shown {selected(app)}, main slot {(section(main, 'composer').get('slot') or {}).get('kind')}, engine {ask_state(app, path)})")
                buttons = [b.get("label") for b in (comp.get("slot") or {}).get("buttons") or []]
                check(buttons == ["Allow", "Allow for session", "Deny"], f"with Allow, Allow for session and Deny ({buttons})")
                check(not os.path.exists(os.path.join(path, "start-012.txt")), "and the command has not run")
            close(app)

        with step("START-018", out_dir):
            footer_key, other_key, other_value = "options.tui.composer_footer", "options.tui.exit_banner", "compact"
            # The meters have no detail box (owner, 2026-10-19): Ctrl+P keeps Customize.
            focus(main)
            key("ctrl+p")
            time.sleep(0.6)
            type_text("Customize composer")
            time.sleep(0.4)
            key("Return")
            ok, panel = wait_on(main, "panel", lambda p: p.get("customize") is not None, 5)
            if check(ok, "Customize Composer opens in the slot"):
                before = set(panel["customize"].get("picked") or [])
                key("8")  # "When usage limits reset", off by default
                ok, panel = wait_on(main, "panel", lambda p: set((p.get("customize") or {}).get("picked") or []) != before, 3)
                picked = sorted((panel.get("customize") or {}).get("picked") or [])
                check(ok and len(picked) == len(before) + 1, f"8 picks one more row ({sorted(before)} → {picked})")
                posted = {}
                other = threading.Thread(target=lambda: posted.update(answer=client.post("config/set", {"scope": 0, "key": other_key, "value": other_value})))
                key("Return")
                other.start()
                other.join(15)
                ok, overlay = wait_on(main, "overlay", lambda o: (o.get("notice") or {}).get("text") == "Composer updated", 10)
                check(ok, f"the desktop saved it (\"Composer updated\": {overlay.get('notice')})")
                check("answer" in posted, "the other client's config/set answered")

                def kept(cfg):
                    tui = ((cfg or {}).get("options") or {}).get("tui") or {}
                    return sorted(tui.get("composer_footer") or []) == picked and tui.get("exit_banner") == other_value

                ok, cfg, took = poll(lambda: client.get("config"), kept, 10, 0.3)
                tui = ((cfg or {}).get("options") or {}).get("tui") or {}
                check(ok, f"the engine keeps both: {footer_key} {tui.get('composer_footer')} and {other_key} {tui.get('exit_banner')!r} ({took:.1f} s)")
                time.sleep(2)
                check(kept(client.get("config")), "and still does 2 s later (no reload lost one)")
                for k in (footer_key, other_key):
                    client.post("config/remove", {"scope": 0, "key": k})
    finally:
        tmux("kill-server")
        if client:
            client.leave()
        for app in extra:
            close(app)
    pc.check_all_ran()


# --- Fixture steps -----------------------------------------------------------

def moved_fixture(src, dst):
    """The fixture with its days moved so its anchor day is today (times kept)."""
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


def fixture_run():
    main = AppRun.existing(probe_path, PROJ)
    data = moved_fixture(FIXTURE, "/tmp/list-today.json")
    close(main)
    app = AppRun("fixture", out_dir, PROJ, args=["--fixture", "/tmp/list-today.json"])
    sessions = data["get"]["sessions"]
    version = app_version()
    try:
        with step("LIST-012", out_dir):
            check(joined(app, 30), "the window plays the fixture")
            place(app)
            ok, lst = wait_on(app, "list", lambda l: len(l.get("rows") or []) == len(sessions), 10)
            check(ok, f"every chat is listed ({len(lst.get('rows') or [])} of {len(sessions)})")
            got = {r["id"]: r.get("state") for r in lst.get("rows") or []}
            for sid, text in STATES.items():
                check(got.get(sid) == text, f"{sid}: {got.get(sid)!r}")
            check(all(not got.get(s["id"]) for s in sessions if s["id"] not in STATES and s["id"] != "live"), "every other row has no state")
            h, _ = heavy(app)
            hr = {r["id"]: r for r in (h.get("list") or {}).get("rows") or []}
            check(all(hr.get(s, {}).get("state") and not hr.get(s, {}).get("time") for s in STATES), "a state takes the time's place on its row")
            before = [r["id"] for r in lst.get("rows") or []]
            ok, lst = wait_on(app, "list", lambda l: next((r for r in l.get("rows") or [] if r["id"] == "live"), {}).get("state") == "Working…", 12)
            check(ok, "a chat another app replies in says Working… (an unfinished assistant message)")
            # The rows follow the open chats shared with the terminal (owner,
            # 2026-10-20), not the newest reply, so the chat keeps its place.
            check([r["id"] for r in lst.get("rows") or []] == before, "and keeps its place")
            ok, _ = wait_on(app, "list", lambda l: next((r for r in l.get("rows") or [] if r["id"] == "live"), {}).get("state") == "", 8)
            check(ok, "and has no state once the reply finishes")

        with step("LIST-days", out_dir):
            now = time.time()
            live_at = next(e for e in data["events"] if e["at_ms"] == 9000)["entries"][0]["payload"]["updated_at"]
            when = {s["id"]: (live_at if s["id"] == "live" else s["updated_at"]) for s in sessions}
            order = sorted(when, key=lambda k: -when[k])
            lst = section(app, "list")
            # Chats sit under their folders, not day groups (owner, 2026-10-21).
            check(sorted(r["id"] for r in lst.get("rows") or []) == sorted(order), f"every chat listed: {[r['id'] for r in lst.get('rows') or []]}")
            check(lst.get("days") == [], f"no day groups ({lst.get('days')})")
            times = {r["id"]: r.get("time") for r in lst.get("rows") or []}
            bad = {sid: (times.get(sid), row_time(when[sid], now)) for sid in order if times.get(sid) != row_time(when[sid], now)}
            check(not bad, f"row times as the design writes them {bad or ''}")
            check(lst.get("search_hint") == "Ctrl K", f"the search hint: {lst.get('search_hint')!r}")

        with step("START-003", out_dir):
            # Blank landing page (owner, 2026-10-19); the fixture's LSPs and
            # MCP servers show in the panel's lists (the details overlay).
            ok, start = wait_on(app, "start", lambda s: s.get("screen") == "landing" and s.get("landing") == {"blank": True}, 10)
            check(ok, f"the blank landing page shows ({start.get('landing')})")
            check(start.get("branch") == "main", f"the window has the branch: {start.get('branch')!r}")
            check((start.get("chat_model") or {}).get("model"), f"the window has the model: {start.get('chat_model')}")
            # The panel lists only the MCP servers the config names (this recording names none).
            configured = set(((data["get"].get("config") or {}).get("mcp") or {}).keys())
            counts = {"lsp": len(data["get"]["lsps"]), "mcp": len(configured & set(data["get"]["mcp/states"])), "skills": len(data["get"]["skills"])}
            for k, n in counts.items():
                check(len(panel_rows(app, k)) == n, f"the panel's {k} rows: {len(panel_rows(app, k))} = {n}")
            lsp_rows = " | ".join(panel_rows(app, "lsp"))
            for text in ("deno disabled", "gopls", "pyright unstarted", "rust-analyzer error: exited with status 1", "typescript starting...", "zls stopped"):
                check(text in lsp_rows, f"LSP row {text!r}")
            h, _ = heavy(app)
            check((h.get("start") or {}).get("animations") == [], "START-005: no animation runs")

        with step("CHAT-001", out_dir):
            landing_checks(app, f"fixture landing at {WIDTH} x {HEIGHT}")
            place(app, 1100, HEIGHT)
            landing_checks(app, f"fixture landing at 1100 x {HEIGHT}")
            place(app)

        with step("LIST-search", out_dir):
            # The search is a button since 2026-10-20: Ctrl+K opens the
            # Sessions sheet, where the search text goes; Esc closes it.
            focus(app)
            key("ctrl+k")
            ok, _ = wait_on(app, "overlay", lambda o: "sessions" in (o.get("dialogs") or []), 3)
            check(ok, "Ctrl+K opens the Sessions sheet")
            key("Escape")
            ok, _ = wait_on(app, "overlay", lambda o: "sessions" not in (o.get("dialogs") or []), 3)
            check(ok, "Esc closes it")
            ok, lst = wait_on(app, "list", lambda l: len(l.get("rows") or []) > 0, 3)
            check(ok, "and the chat list keeps its rows")

        with step("LIST-narrow", out_dir):
            focus(app)
            # "cookie" (the one recorded chat) is past the 6 drawn rows: the
            # Sessions sheet opens it, and the open chat is always drawn.
            key("ctrl+k")
            wait_on(app, "overlay", lambda o: "sessions" in (o.get("dialogs") or []), 3)
            time.sleep(0.3)
            type_text("cookie")
            time.sleep(0.5)
            key("Return")
            ok, _, _ = poll(lambda: selected(app), lambda s: s == "cookie", 10)
            check(ok, f"a chat is open (the band; {selected(app)})")
            place(app)
            save_align_both(app, "list", "The chat list", "list")
            h, report = heavy(app)
            list_edges(h, "full width")
            col = (h.get("list") or {}).get("column") or {}
            z = report.get("zoom") or 8 / 12
            box = window(app)
            if check(bool(col) and bool(box), "the list column's rect"):
                x0, y0 = box[1] + col["x"] + col["w"] + 1, box[2] + col["y"] + col["h"] / 2
                xdo("mousemove", round(x0), round(y0))
                xdo("mousedown", 1)
                for i in range(1, 11):
                    xdo("mousemove", max(1, round(x0 - 40 * i)), round(y0))
                    time.sleep(0.03)
                xdo("mouseup", 1)
                time.sleep(0.5)
                h, _ = heavy(app)
                col = (h.get("list") or {}).get("column") or {}
                # Its narrowest is where New chat and New terminal fit whole (owner, 2026-10-20), never under 280.
                nt = (h.get("list") or {}).get("new_terminal") or {}
                check(col.get("w", 0) >= 280 * z - 1.5 and bool(nt), f"the list stops at its narrowest, at least 280 picture px ({col.get('w')} ≥ {280 * z:.1f})")
                touching = []
                for r in (h.get("list") or {}).get("rows") or []:
                    title, t = r.get("title_text"), r.get("time")
                    if title and t and title["x"] + title["w"] > t["x"] - 1 and title["y"] < t["y"] + t["h"] and t["y"] < title["y"] + title["h"]:
                        touching.append(r["id"])
                    if t and t["x"] + t["w"] > col["x"] + col["w"]:
                        touching.append(r["id"] + " (past the column)")
                check(not touching, f"no row's time touches its title {touching or ''}")
                list_edges(h, "narrowest")
                save_align(app, "list-narrow", "The chat list at its narrowest", "list")

        with step("LIST-short", out_dir):
            h, report = heavy(app)
            z = report.get("zoom") or 8 / 12
            tall = (h.get("list") or {}).get("new_chat") or {}
            place(app, WIDTH, 600)
            h, _ = heavy(app)
            lh = h.get("list") or {}
            nc, sr, sc = lh.get("new_chat") or {}, lh.get("search") or {}, lh.get("scroll") or {}
            check(abs(nc.get("h", 0) - 52 * z) <= 1 and abs(sr.get("h", 0) - 50 * z) <= 1, f"New chat and the search keep their height ({nc.get('h')}, {sr.get('h')}; {52 * z:.1f}, {50 * z:.1f})")
            check(abs(nc.get("y", 0) - tall.get("y", -99)) <= 0.5, f"and their place ({nc.get('y')} as {tall.get('y')})")
            last = max(((r.get("rect") or {}).get("y", 0) + (r.get("rect") or {}).get("h", 0) for r in lh.get("rows") or []), default=0)
            check(bool(sc) and last > sc["y"] + sc["h"], f"the rows run on below the list's bottom and scroll there ({last} > {sc.get('y', 0) + sc.get('h', 0)})")
            place(app)
    finally:
        close(app)


if __name__ == "__main__":
    if FIXTURE:
        fixture_run()
    else:
        engine_run()
    finish()
