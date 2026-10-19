"""P3.9's window check (desktop-design 2.5, LIST-012, LIST-033, LIST-034 and
CHAT-001's landing part): the chat list, the chat's head and the start
screens in the real window on the hidden screen, read through the probe's
start, list and gate sections (1.9) and pressed with the real keyboard and
mouse. Each step is named after its row and saves <ROW>.png (2.0).

Engine run, on the fresh /tmp/home/proj: tests/check.sh p3_start.py
  START-002     the fresh project asks first: the question, the folder, the
                initialize_as file, the hint, Yep! chosen; Tab, ← and → switch;
                y makes a chat that sends the engine's init prompt and marks
                the project; a restart asks no more.
  START-015     the data folder's .gitignore holds *.
  START-016     projects.json lists the project.
  START-002-no  a second fresh folder: n goes to the landing page and marks it.
  LIST-033      Ctrl+N and New chat show the landing page and make no record;
                on a busy chat Ctrl+N says the terminal's warning and stays.
  START-003     the landing page: the folder and branch, the model line, and the
                LSP / MCP / Skills columns as the engine lists them.
  START-004     the wordmark and "Charm™ · v<version>", the app's own version.
  START-005     no animation runs on the landing page.
  CHAT-001      the landing block is centred, at most 720 wide, 60 in from the
                column's edges.
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
                words and no engine starts.
  START-007     a broken MCP server says its error; LSPs, skills, the model and
                the branch still fill.
  START-017     a global large model and the project's: the project's shows.
  START-019     the old "theme" setting reaches options.tui.active_theme.
  START-012     --yolo=false then a bash tool opens the permission slot: needs
                P3.7's composer section; NOT RUN (a failure) until it exists.
  START-018     needs Customize Composer (P3.7, P3.8); NOT RUN until then.

Fixture run: tests/check.sh --fixture tests/fixtures/list-states.json p3_start.py
  The app is started again on a copy of the fixture whose days are moved so
  its anchor day is today (its times are local clock times).
  LIST-012      the four states as the engine reports them, and a chat another
                app replies in (fixture events): "Working…", then nothing.
  LIST-days     day groups and row times, newest first.
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
import importlib.util, json, os, re, shutil, signal, struct, subprocess, sys, time, types, uuid
from datetime import datetime, timedelta, date

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, key, probe_heavy, read_probe, shot, step, type_text, xdo

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(HERE))


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


# --- The list's rules, written from the design (an independent oracle) -------

DAY_LABELS = ["Today", "Yesterday", "Previous 7 days", "Previous 30 days", "Older"]
WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
STATES = {"busy": "Working…", "permission": "Waiting for permission", "question": "Waiting for your answer", "background": "Background"}


def days_ago(at, now):
    return (datetime.fromtimestamp(now).date() - datetime.fromtimestamp(at).date()).days


def day_label(at, now):
    d = days_ago(at, now)
    return DAY_LABELS[0] if d <= 0 else DAY_LABELS[1] if d == 1 else DAY_LABELS[2] if d <= 7 else DAY_LABELS[3] if d <= 30 else DAY_LABELS[4]


def row_time(at, now):
    t, d = datetime.fromtimestamp(at), days_ago(at, now)
    if d <= 1:
        return f"{t.hour:02d}:{t.minute:02d}"
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
        code = subprocess.run([sys.executable, os.path.join(align_real.ALIGN, "align.py"), os.path.join(work, "align.json"),
                               "--out", os.path.join(work, "results"), "--self-review"]).returncode
        print(("PASS " if code == 0 else "FAIL ") + f"align {name} (exit {code}; {work}/results)", flush=True)
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
    p = app.wait(lambda p: round((p.get("shell") or {}).get("w", 0)) == w and round((p.get("shell") or {}).get("h", 0)) == h, 10)
    time.sleep(0.4)
    return round((p.get("shell") or {}).get("w", 0)) == w


def zoom_of(app):
    return app.report().get("zoom") or 8 / 12


def joined(app, timeout=45):
    p = app.wait(lambda p: (p.get("connection") or {}).get("state") == "connected", timeout)
    return (p.get("connection") or {}).get("state") == "connected"


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


def landing_checks(app, label):
    """CHAT-001's rules on the landing block's content box, and START-005."""
    h, report = heavy(app)
    st = h.get("start") or {}
    z = report.get("zoom") or 8 / 12
    col, content = st.get("column") or {}, (st.get("landing") or {}).get("content") or {}
    if not check(bool(col) and bool(content), f"{label}: the probe has the column and the landing block ({col}, {content})"):
        return
    left = content["x"] - col["x"]
    right = col["x"] + col["w"] - (content["x"] + content["w"])
    check(content["w"] <= 720 * z + 1, f"{label}: the landing content is at most 720 picture px wide ({content['w']:.1f} ≤ {720 * z:.1f})")
    check(left >= 60 * z - 1 and right >= 60 * z - 1, f"{label}: at least 60 picture px from each edge of the column ({left:.1f}, {right:.1f}; {60 * z:.1f})")
    check(abs(left - right) <= 1, f"{label}: centred in the column ({left:.1f} and {right:.1f})")
    return h


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
                    "buttons": ["Yep!", "Nope"], "selected": "yes"}
            for field, value in want.items():
                check(init.get(field) == value, f"init {field}: {init.get(field)!r}")
            place(main)
            save_align_both(main, "init", "The init prompt")
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
            wid = pc.workspace_for(api, other)
            needs = api.call("GET", f"/v1/workspaces/{wid}/project/needs-init") if wid else {}
            check(bool(wid) and not (needs or {}).get("needs_init"), f"and the folder is marked initialized ({needs})")
            check(len(api.call("GET", f"/v1/workspaces/{wid}/sessions") or []) == 0 if wid else False, "and no chat was made")
            close(app)

        with step("LIST-033", out_dir):
            place(main)
            if section(main, "start").get("screen") != "chat":
                click_row(main, init_chat)
            ok, _, _ = poll(lambda: selected(main), lambda s: s == init_chat, 10)
            check(ok, "the init chat is shown")
            count = len(client.sessions())
            focus(main)
            key("ctrl+n")
            ok, screen = screen_is(main, "landing", 5)
            check(ok and selected(main) is None, f"Ctrl+N shows the landing page with no chat chosen (screen {screen})")
            click_row(main, init_chat)
            screen_is(main, "chat", 10)
            h, _ = heavy(main)
            new_chat = (h.get("list") or {}).get("new_chat")
            check(bool(new_chat) and main.click(new_chat), "New chat is clicked")
            ok, screen = screen_is(main, "landing", 5)
            check(ok, f"New chat shows the landing page (screen {screen})")
            time.sleep(1)
            check(len(client.sessions()) == count, f"and neither made a chat record ({count} → {len(client.sessions())})")
            # Busy: a long reply runs in the shown chat.
            click_row(main, init_chat)
            screen_is(main, "chat", 10)
            client.post("agent", {"session_id": init_chat, "prompt": LONG_REPLY})
            ok, _, _ = poll(lambda: next((r for r in rows(main) if r.get("id") == init_chat), {}), lambda r: r.get("state") == "Working…", 15)
            check(ok, "the shown chat is busy (its row says Working…)")
            focus(main)
            key("ctrl+n")
            ok, overlay = wait_on(main, "overlay", lambda o: (o.get("notice") or {}).get("text") == BUSY_WARNING, 5)
            check(ok, f"Ctrl+N on a busy chat says \"{BUSY_WARNING}\" ({(overlay.get('notice') or {})})")
            check(section(main, "start").get("screen") == "chat" and selected(main) == init_chat, "and the chat stays")
            client.post(f"agent/sessions/{init_chat}/cancel")
            ok, _, _ = poll(lambda: next((r for r in rows(main) if r.get("id") == init_chat), {}), lambda r: not r.get("state"), 20)
            check(ok, "once the run is cancelled the row has no state")

        with step("START-003", out_dir):
            focus(main)
            key("ctrl+n")
            screen_is(main, "landing", 5)
            ok, start = wait_on(main, "start", lambda s: (s.get("landing") or {}).get("model"), 10)
            landing = start.get("landing") or {}
            branch = (client.get("git/branch") or {}).get("branch") or ""
            check(landing.get("path_line") == (f"{PROJ} · {branch}" if branch else PROJ), f"the folder and branch: {landing.get('path_line')!r} (engine branch {branch!r})")
            check((landing.get("model") or "").startswith("Fake Large · via Fake AI"), f"the model line names the project's model and provider: {landing.get('model')!r}")
            columns = landing.get("columns") or {}
            check([c.get("title") for c in columns.values()] == ["LSP", "MCP", "Skills"], f"three columns: {[c.get('title') for c in columns.values()]}")
            engine_counts = {"lsps": len(client.get("lsps") or {}), "mcps": len(client.get("mcp/states") or {}), "skills": len(client.get("skills") or [])}
            for k, c in columns.items():
                shown = len(c.get("rows") or []) + (c.get("more") or 0)
                check(shown == engine_counts.get(k) and (c.get("none") == (engine_counts.get(k) == 0)),
                      f"{c.get('title')}: {len(c.get('rows') or [])} rows + {c.get('more')} more = the engine's {engine_counts.get(k)}")

        with step("START-004", out_dir):
            head = section(main, "start").get("head") or {}
            check(head.get("wordmark") == "CRUSH AI", f"the wordmark: {head.get('wordmark')!r}")
            check(bool(version) and head.get("charm") == f"Charm™ · v{version}", f"\"Charm™ · v<version>\" is the app's own version ({head.get('charm')!r}, crush-desktop --version {version})")
            h, _ = heavy(main)
            check(bool((h.get("start") or {}).get("wordmark")), "the wordmark is drawn in the head")
            check(main.report().get("heading") == "Crush AI", f"the list's wordmark is still the page's heading ({main.report().get('heading')!r})")

        with step("START-005", out_dir):
            h, _ = heavy(main)
            anims = (h.get("start") or {}).get("animations")
            check(anims == [], f"no animation runs on the landing page ({anims})")

        with step("CHAT-001", out_dir):
            landing_checks(main, "engine landing at 1600 x 992")

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
            ok, _, _ = poll(lambda: {r.get("id"): r.get("title") for r in rows(main)}, lambda r: r.get(big) == "Five hundred items" and r.get(short) == "Short chat", 15)
            check(ok, "both chats reach the list with their titles")
            pid = (section(main, "connection").get("engine") or {}).get("pid")
            rect_big, rect_short = row_rect(main, big), row_rect(main, short)
            if check(bool(pid and rect_big and rect_short), f"the engine's pid and both rows' rects ({pid})"):
                os.kill(pid, signal.SIGSTOP)
                try:
                    main.click(rect_big)
                    ok, start = wait_on(main, "start", lambda s: (s.get("opening") or {}).get("label") == "Opening Five hundred items…", 5)
                    check(ok and start.get("screen") == "opening", f"\"Opening Five hundred items…\" shows while the engine is held ({start.get('opening')})")
                    head = start.get("head") or {}
                    check(head.get("title") == "Five hundred items", f"the head names the chat being opened ({head.get('title')!r})")
                    ok, start = wait_on(main, "start", lambda s: re.fullmatch(r"[1-9]\d*s", (s.get("opening") or {}).get("elapsed") or "") is not None, 4)
                    check(ok, f"after 1 s the seconds show ({(start.get('opening') or {}).get('elapsed')!r})")
                    save_align(main, "opening", "The opening line")
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
                    ok, _ = wait_on(app, "list", lambda l: l.get("search_focused") is True, 5)
                    check(ok, "and the chat-list search has the focus")
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
            ok, start = wait_on(app, "start", lambda s: any(r.startswith("broken error") for r in (((s.get("landing") or {}).get("columns") or {}).get("mcps") or {}).get("rows") or []), 20)
            landing = start.get("landing") or {}
            cols = landing.get("columns") or {}
            check(ok, f"the broken server's row says its error ({(cols.get('mcps') or {}).get('rows')})")
            wid = pc.workspace_for(api, path)
            eng = lambda p: api.call("GET", f"/v1/workspaces/{wid}/{p}")
            check(any(r.startswith("fakelsp") for r in (cols.get("lsps") or {}).get("rows") or []), f"the LSP column fills ({(cols.get('lsps') or {}).get('rows')})")
            skills = eng("skills") or []
            sk = cols.get("skills") or {}
            check(len(sk.get("rows") or []) + (sk.get("more") or 0) == len(skills), f"the skills column has the engine's {len(skills)} skills")
            check((landing.get("model") or "").startswith("Fake Large"), f"the model line fills ({landing.get('model')!r})")
            branch = (eng("git/branch") or {}).get("branch") or ""
            check(bool(branch) and landing.get("path_line") == f"{path} · {branch}", f"the branch fills ({landing.get('path_line')!r}, engine {branch!r})")
            close(app)

        with step("START-017", out_dir):
            gdir = os.path.join(HOMEDIR, ".config", "crush")
            gfile = os.path.join(gdir, "crush.json")
            os.makedirs(gdir, exist_ok=True)
            kept = open(gfile).read() if os.path.exists(gfile) else None
            glob = json.loads(kept) if kept else {}
            glob["models"] = {"large": {"model": "fake-small", "provider": "fake"}, "small": {"model": "fake-small", "provider": "fake"}}
            with open(gfile, "w") as f:
                json.dump(glob, f)
            try:
                path = pc.make_project(os.path.join(HOMEDIR, "proj-models"), port)
                app = AppRun("models", out_dir, path)
                extra.append(app)
                check(joined(app), "a window on a project whose crush.json sets fake-large, under a global fake-small, joins")
                answer_no(app)
                ok, start = wait_on(app, "start", lambda s: (s.get("landing") or {}).get("model"), 10)
                check((start.get("large_model") or {}).get("model") == "fake-large", f"the merged config's large model is the project's ({start.get('large_model')})")
                check((start.get("landing") or {}).get("model", "").startswith("Fake Large"), f"the landing page names it ({(start.get('landing') or {}).get('model')!r})")
                close(app)
            finally:
                if kept is None:
                    os.remove(gfile)
                else:
                    with open(gfile, "w") as f:
                        f.write(kept)

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
            if "composer" not in main.report():
                pc.not_run("START-012", "the permission slot is P3.7's composer section, not in this build")
            else:
                pc.not_run("START-012", "its window step is written once P3.7's slot fields are known (1.9 composer.slot)")

        with step("START-018", out_dir):
            pc.not_run("START-018", "the desktop's side saves Customize Composer (P3.7, P3.8), not in this build")
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
            ok, lst = wait_on(app, "list", lambda l: next((r for r in l.get("rows") or [] if r["id"] == "live"), {}).get("state") == "Working…", 12)
            check(ok, "a chat another app replies in says Working… (an unfinished assistant message)")
            check((lst.get("rows") or [{}])[0].get("id") == "live", "and moves to the top")
            ok, _ = wait_on(app, "list", lambda l: next((r for r in l.get("rows") or [] if r["id"] == "live"), {}).get("state") == "", 8)
            check(ok, "and has no state once the reply finishes")

        with step("LIST-days", out_dir):
            now = time.time()
            live_at = next(e for e in data["events"] if e["at_ms"] == 9000)["entries"][0]["payload"]["updated_at"]
            when = {s["id"]: (live_at if s["id"] == "live" else s["updated_at"]) for s in sessions}
            order = sorted(when, key=lambda k: -when[k])
            lst = section(app, "list")
            check([r["id"] for r in lst.get("rows") or []] == order, f"newest first: {[r['id'] for r in lst.get('rows') or []]}")
            want_days = []
            for sid in order:
                label = day_label(when[sid], now)
                if not want_days or want_days[-1] != label:
                    want_days.append(label)
            check(lst.get("days") == want_days, f"day groups {lst.get('days')} (want {want_days})")
            times = {r["id"]: r.get("time") for r in lst.get("rows") or []}
            bad = {sid: (times.get(sid), row_time(when[sid], now)) for sid in order if times.get(sid) != row_time(when[sid], now)}
            check(not bad, f"row times as the design writes them {bad or ''}")
            check(lst.get("search_hint") == "Ctrl K", f"the search hint: {lst.get('search_hint')!r}")

        with step("START-003", out_dir):
            ok, start = wait_on(app, "start", lambda s: s.get("screen") == "landing" and (s.get("landing") or {}).get("model"), 10)
            check(ok, "the landing page shows")
            landing = start.get("landing") or {}
            check(landing.get("path_line") == f"{PROJ} · main", f"the folder and branch: {landing.get('path_line')!r}")
            check(landing.get("model") == "Fake Large · via Fake AI · Reasoning High", f"the model line: {landing.get('model')!r}")
            cols = landing.get("columns") or {}
            counts = {"lsps": len(data["get"]["lsps"]), "mcps": len(data["get"]["mcp/states"]), "skills": len(data["get"]["skills"])}
            for k, c in cols.items():
                rows_ = c.get("rows") or []
                check(len(rows_) + (c.get("more") or 0) == counts[k], f"{c.get('title')}: {len(rows_)} rows + {c.get('more')} more = {counts[k]}")
            check((cols.get("lsps") or {}).get("more", 0) > 0 and (cols.get("skills") or {}).get("more", 0) > 0, "LSPs and skills that do not fit end with \"…and N more\"")
            lsp_rows = " | ".join((cols.get("lsps") or {}).get("rows") or [])
            for text in ("deno disabled", "gopls", "pyright unstarted", "rust-analyzer error: exited with status 1", "typescript starting...", "zls stopped"):
                check(text in lsp_rows, f"LSP row {text!r}")
            mcp_rows = (cols.get("mcps") or {}).get("rows") or []
            check(mcp_rows == ["Docker MCP 12 tools 3 resources", "broken error: exec: \"mcp-broken\": executable file not found in $PATH",
                               "files 5 tools 2 prompts channel", "github needs authentication", "off disabled", "slow starting..."], f"MCP rows {mcp_rows}")
            head = start.get("head") or {}
            check(head.get("charm") == f"Charm™ · v{version}", f"the head: {head}")
            h, _ = heavy(app)
            land = (h.get("start") or {}).get("landing") or {}
            area = land.get("columns") or {}
            ends = land.get("rows_end") or {}
            check(bool(area) and all(v <= area["y"] + area["h"] + 0.5 for v in ends.values()), f"no column's rows run past the area ({ends}, bottom {area.get('y', 0) + area.get('h', 0)})")
            check((h.get("start") or {}).get("animations") == [], "START-005: no animation runs")
            save_align_both(app, "landing", "The landing page")

        with step("CHAT-001", out_dir):
            landing_checks(app, f"fixture landing at {WIDTH} x {HEIGHT}")
            place(app, 1100, HEIGHT)
            landing_checks(app, f"fixture landing at 1100 x {HEIGHT}")
            place(app)

        with step("LIST-search", out_dir):
            focus(app)
            key("ctrl+k")
            ok, _ = wait_on(app, "list", lambda l: l.get("search_focused") is True, 3)
            check(ok, "Ctrl+K focuses the chat-list search")
            type_text("cookie")
            ok, lst = wait_on(app, "list", lambda l: [r["id"] for r in l.get("rows") or []] == ["cookie"], 5)
            check(ok, f"\"cookie\" leaves its chat ({[r['id'] for r in lst.get('rows') or []]})")
            key("ctrl+a")
            type_text("zzz")
            ok, lst = wait_on(app, "list", lambda l: l.get("empty") == "No chats match “zzz”.", 5)
            check(ok and not lst.get("rows"), f"no match says {lst.get('empty')!r}")
            h, _ = heavy(app)
            check(bool((h.get("list") or {}).get("empty")), "and draws it")
            key("Escape")
            ok, lst = wait_on(app, "list", lambda l: l.get("query") == "" and len(l.get("rows") or []) == len(sessions), 5)
            check(ok, "Esc clears the search")

        with step("LIST-narrow", out_dir):
            focus(app)
            click_row(app, "cookie")
            ok, _, _ = poll(lambda: selected(app), lambda s: s == "cookie", 10)
            check(ok, "a chat is open (the band)")
            place(app)
            save_align_both(app, "list", "The chat list", "list")
            h, report = heavy(app)
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
                check(abs(col.get("w", 0) - 280 * z) <= 1.5, f"the list is at its narrowest, 280 picture px ({col.get('w')} ≈ {280 * z:.1f})")
                touching = []
                for r in (h.get("list") or {}).get("rows") or []:
                    title, t = r.get("title_text"), r.get("time")
                    if title and t and title["x"] + title["w"] > t["x"] - 1 and title["y"] < t["y"] + t["h"] and t["y"] < title["y"] + title["h"]:
                        touching.append(r["id"])
                    if t and t["x"] + t["w"] > col["x"] + col["w"]:
                        touching.append(r["id"] + " (past the column)")
                check(not touching, f"no row's time touches its title {touching or ''}")
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
