"""P4.1's branching behaviors in the real window on the engine (desktop-design
3.7), with the real mouse and keyboard on the hidden screen: words are
selected by pressing on the first word's rect and releasing on the last
word's (the probe's chat.words), the menu is opened with a right click inside
the selection, and every result is checked against the engine's own answers
over its socket. Each step is named after its row and saves <ROW>.png.

  setup  the fresh project's init question is answered No; scripts/stream2m.json
         is the active script; seed_chat.py makes a five-turn chat S (short,
         medium, code, list, tool), opened from its row
  B1     a right click on a reply shows exactly Branch from here, Copy, Quote in
         reply, Copy whole message; no native menu window appears and the
         capture shows our box where the probe says it is
  B2     words of the medium reply across its paragraph break (over 34
         characters): the branch opens at once, titled branchTitle(words)
         (33 characters and "…"), its field holding quoteDraft(words) in full
         with the caret at the end; back in S the words are a link with the
         title "Open <branch>" and the reply has a key
  B6     words of the short reply ending inside a word branch; words starting
         at that very point (touching) branch too; words over both are refused
         with "These words already open a branch" and nothing is sent
  B3     words of your own second message: the branch ends with that message;
         the next send's AI request (fake AI log) holds it unanswered before
         the new prompt (one message more than the source's request for it)
  B4     words of the tool turn's first reply: the branch's messages are the
         source's path through the cut plus the tool result that answers it
  B5     a selection in the tool's body: Branch from here is disabled with
         "Branch from message text, not tool output"
  B12    a sibling C of B from other words of the same reply; inside C the
         copied link opens B; a branch of B nests at depth 2
  B9     B's row plain, hovered and selected: the title's box (where a cut
         title's "…" falls), the title's letters, the time's and the
         chevron's rects move by at most 1 real px, the × shows in the
         reserved slot only when hovered or selected; its × hides B, B's
         branch moves up to depth 1, the footer says "1 closed · links reopen
         them", the window opens S, and the settings file holds the closing
  B10    clicking the link in S opens B and lists it again
  B15    the engine's tree switch moves S's head off the linked reply: the
         link goes; switching back shows it again
  B14    the terminal app (hidden tmux) lists the branch by its title
  B13    S deleted through the engine API: its branches stay, at the top
         level; B's branch stays under B; B's head no longer says BRANCHED FROM

Usage: tests/check.sh p4_branch.py [--stop-after ROW]
The other p4 drivers load this file for its helpers (App, Engine, the oracle
functions); its main() runs only as a script.
"""
import importlib.util, json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, fake_ai_requests, finish, key, probe_heavy, read_probe, shot, step, type_text, xdo

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(HERE))
PROJ = "/tmp/home/proj"
CRUSH = os.environ.get("DESKTOP_CRUSH") or os.path.join(ROOT, "tests", "out", "bin", "crush")
APP_BIN = os.environ.get("DESKTOP_APP") or os.path.join(ROOT, "src-tauri", "target", "release", "crush-desktop")
TMUX = ["tmux", "-L", "crushtest", "-f", "/dev/null"]
SETTINGS = "/tmp/home/.config/crush-desktop/settings.json"
WIDTH, HEIGHT = 1600, 992
TITLE_MAX = 34
MENU = ["Branch from here", "Copy", "Quote in reply", "Copy whole message"]
HINT_OVERLAP = "These words already open a branch"
HINT_TOOL = "Branch from message text, not tool output"
HINT_UNFINISHED = "Wait for the reply to finish"
PAPER_RAISED = (0xFB, 0xFA, 0xF2)
RULE_CONTROL = (0x90, 0x8F, 0x89)


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


pc = load("p3_connect", os.path.join(HERE, "p3_connect.py"))
seed_chat = pc.seed_chat


# --- The branch rules, from the design (an independent oracle, 3.1) --------

def flat(text):
    return " ".join((text or "").split())


def branch_title(words):
    """branchTitle: white space collapsed; over 34 characters, 33 and "…"."""
    t = flat(words)
    return t if len(t) <= TITLE_MAX else t[: TITLE_MAX - 1] + "…"


def quote_draft(words):
    """quoteDraft: "> " and the words in full on one line, then a blank line."""
    return f"> {flat(words)}\n\n"


def poll(read, ok, timeout, every=0.1):
    """Calls read until ok(value) or timeout; returns (passed, value, seconds)."""
    start = time.time()
    while True:
        value = read()
        if ok(value):
            return True, value, time.time() - start
        if time.time() - start > timeout:
            return False, value, time.time() - start
        time.sleep(every)


def text_of(m):
    return "".join((p.get("data") or {}).get("text", "") for p in m.get("parts") or [] if p.get("type") == "text")


def shape_of(m):
    """What a copied message must keep: its role, text, tool calls and tool results."""
    calls = [((p.get("data") or {}).get("name"), (p.get("data") or {}).get("input")) for p in m.get("parts") or [] if p.get("type") == "tool_call"]
    results = [((p.get("data") or {}).get("name"), (p.get("data") or {}).get("content")) for p in m.get("parts") or [] if p.get("type") == "tool_result"]
    return (m.get("role"), text_of(m), calls, results)


# --- The engine over its socket: the ground truth ---------------------------

class Engine:
    def __init__(self, sock, wid):
        self.api = seed_chat.Engine(sock)
        self.wid = wid

    def call(self, method, path, body=None):
        return self.api.call(method, f"/v1/workspaces/{self.wid}/{path}", body)

    def get(self, path):
        return self.call("GET", path)

    def sessions(self):
        return {s["id"]: s for s in self.get("sessions") or []}

    def messages(self, sid):
        return self.get(f"sessions/{sid}/messages") or []

    def links(self):
        return self.get("chat-links") or []

    def origin(self, sid):
        return next((l for l in self.links() if l.get("session_id") == sid and l.get("origin")), None)

    def needs_init(self):
        return bool((self.get("project/needs-init") or {}).get("needs_init"))


# --- The window --------------------------------------------------------------

class App:
    """The real app, read through its probe and driven with the real mouse and keyboard."""

    def __init__(self, run):
        self.run = run

    @property
    def probe(self):
        return self.run.probe

    @property
    def pid(self):
        return self.run.pid

    def report(self):
        return read_probe(self.probe, tries=3) or {}

    def section(self, name):
        return self.report().get(name) or {}

    def wait(self, cond, timeout):
        return self.run.wait(cond, timeout)

    def heavy(self, timeout=20):
        return (probe_heavy(self.probe, timeout) or {}).get("heavy") or {}

    def box(self):
        boxes = pc.windows_of(self.pid) if self.pid else []
        return boxes[0] if boxes else None

    def focus(self):
        box = self.box()
        if not box:
            return False
        xdo("windowraise", box[0])
        xdo("windowfocus", box[0])
        return True

    def place(self, w=WIDTH, h=HEIGHT):
        box = self.box()
        if not box:
            return False
        xdo("windowmove", box[0], 0, 0)
        xdo("windowsize", box[0], w, h)
        p = self.wait(lambda p: round((p.get("shell") or {}).get("w", 0)) == w and round((p.get("shell") or {}).get("h", 0)) == h, 10)
        time.sleep(0.4)
        return round((p.get("shell") or {}).get("w", 0)) == w

    def screen(self, x, y):
        box = self.box()
        ox, oy = (box[1], box[2]) if box else (0, 0)
        return round(ox + x), round(oy + y)

    def move(self, x, y):
        xdo("mousemove", *self.screen(x, y))
        time.sleep(0.15)

    def park(self):
        """The pointer in the window's bottom corner, off every control."""
        box = self.box()
        if box:
            xdo("mousemove", box[1] + box[3] - 4, box[2] + box[4] - 4)
        time.sleep(0.2)

    def click(self, x, y, button=1):
        self.focus()
        self.move(x, y)
        xdo("click", button)

    def click_rect(self, rect, button=1):
        return bool(rect) and self.click(rect["x"] + rect["w"] / 2, rect["y"] + rect["h"] / 2, button) is None

    def drag(self, a, b):
        """Presses at viewport point a and releases at b, moving in steps."""
        self.focus()
        sx, sy = self.screen(*a)
        ex, ey = self.screen(*b)
        xdo("mousemove", sx, sy)
        xdo("mousedown", 1)
        for i in range(1, 7):
            xdo("mousemove", round(sx + (ex - sx) * i / 6), round(sy + (ey - sy) * i / 6))
            time.sleep(0.03)
        xdo("mouseup", 1)
        time.sleep(0.2)

    # Light sections
    def chat_id(self):
        return self.section("chat").get("id") or ""

    def rows(self):
        return self.section("list").get("rows") or []

    def row(self, sid):
        return next((r for r in self.rows() if r.get("id") == sid), None)

    def composer(self):
        return self.section("composer")

    def branch(self):
        return self.section("branch")

    def items(self):
        return self.section("chat").get("items") or []

    def item_key(self, mid, kind=None):
        return next((i["key"] for i in self.items() if i.get("id") == mid and (kind is None or i.get("kind") == kind)), None)

    def connected(self, timeout=45, since=0.0):
        p = self.wait(lambda p: (p.get("connection") or {}).get("state") == "connected" and p.get("at", 0) >= since * 1000, timeout)
        return (p.get("connection") or {}).get("state") == "connected"

    def engine(self):
        conn = self.section("connection")
        sock = (conn.get("engine") or {}).get("socket")
        return Engine(sock, conn.get("workspace_id")) if sock and conn.get("workspace_id") else None

    # Heavy reads
    def words(self, key):
        return [w for w in ((self.heavy().get("chat") or {}).get("words") or []) if w.get("key") == key]

    def view(self):
        return (self.heavy().get("chat") or {}).get("view")

    def shown_links(self):
        return (self.heavy().get("branch") or {}).get("shown") or []

    def open_chat(self, sid, timeout=15):
        """A real click on the chat's row in the list."""
        if self.chat_id() == sid:
            return True
        rows = (self.heavy().get("branch") or {}).get("rows") or []
        rect = next((r.get("row") for r in rows if r.get("id") == sid), None)
        if not rect:
            return False
        self.click_rect(rect)
        ok, _, _ = poll(self.chat_id, lambda c: c == sid, timeout)
        if ok:
            poll(lambda: self.section("chat").get("status"), lambda s: s in ("ready", ""), 10)
            time.sleep(0.5)
        return ok

    def visible_words(self, key, need=()):
        """The item's words inside the timeline's view, when all of need are
        among them (the item's first words when need is empty); else None.
        Also the chat's heavy fields."""
        chat = self.heavy().get("chat") or {}
        view = chat.get("view") or {}
        words = [w for w in chat.get("words") or [] if w.get("key") == key]
        inside = [w for w in words if view and w["rect"]["y"] >= view["y"] + 4 and w["rect"]["y"] + w["rect"]["h"] <= view["y"] + view["h"] - 4]
        texts = {w["text"] for w in inside}
        return (inside if inside and all(n in texts for n in need) else None), chat

    def focus_timeline(self, chat):
        """The typing box, then Tab: the timeline takes the keys (TYPE-022)."""
        view, top = chat.get("view"), chat.get("composer_top")
        if not view or top is None:
            return False
        self.click(view["x"] + 200, top + 25)
        time.sleep(0.2)
        key("Tab")
        time.sleep(0.4)
        return True

    def bring_into_view(self, key_, need=(), tries=40):
        """Selects the item with the timeline's own keys (Shift+↑/↓ show the
        selected item, chat_scroll.go's moves), then steps a reading line
        (↑/↓) until the words in need are inside the view; returns the item's
        visible words. The mouse wheel is not used: on the hidden screen one
        wheel click moves the timeline by about a pixel."""
        got, chat = self.visible_words(key_, need)
        if got:
            return got
        if not self.focus_timeline(chat):
            print(f"NOTE bring_into_view({key_}): the probe has no timeline view", flush=True)
            return []
        for _ in range(tries):
            items = self.items()
            keys = [i.get("key") for i in items]
            selected = next((i.get("key") for i in items if i.get("selected")), None)
            if key_ not in keys:
                break
            if selected != key_:
                at = keys.index(selected) if selected in keys else len(keys)
                key("shift+Up" if keys.index(key_) < at else "shift+Down")
            else:
                got, chat = self.visible_words(key_, need)
                if got:
                    return got
                view = chat.get("view") or {}
                words = [w for w in chat.get("words") or [] if w.get("key") == key_]
                above = any(w["rect"]["y"] < view.get("y", 0) + 4 for w in words if w["text"] in need) or not words
                key("Up" if above else "Down")
            time.sleep(0.3)
        print(f"NOTE bring_into_view({key_}) gave up: listed {key_ in [i.get('key') for i in self.items()]}, "
              f"words {[w['text'] for w in self.words(key_)][:12]}, need {list(need)}", flush=True)
        return []


def word_point(w, end=False, frac=None):
    r = w["rect"]
    if frac is not None:
        return (r["x"] + r["w"] * frac, r["y"] + r["h"] / 2)
    return (r["x"] + r["w"] - 1, r["y"] + r["h"] / 2) if end else (r["x"] + 1, r["y"] + r["h"] / 2)


def find_word(words, text, after=None):
    """The first visible word equal to text (after word index `after` when given)."""
    return next((w for w in words if w["text"] == text and (after is None or w["i"] > after)), None)


def between(words, first, last):
    """A word strictly inside the selection from first to last (for the right
    click: a press outside the selection would collapse it)."""
    inner = [w for w in words if first["i"] < w["i"] < last["i"]]
    return inner[len(inner) // 2] if inner else first


def hover_title(app, link):
    """The title attribute a link carries once the pointer is over it."""
    r = link["rects"][0]
    app.move(r["x"] + r["w"] / 2, r["y"] + r["h"] / 2)
    time.sleep(0.2)
    again = [l for l in app.shown_links() if l.get("link_id") == link.get("link_id")]
    return again[0].get("title_attr") if again else None


def menu_open(app, timeout=5):
    ok, menu, _ = poll(lambda: app.section("menu"), lambda m: m.get("open") and m.get("items"), timeout)
    return menu if ok else None


def menu_entry(menu, label):
    return next((i for i in (menu or {}).get("items") or [] if i.get("label") == label), None)


def click_menu(app, label):
    items = (app.heavy().get("menu") or {}).get("items") or []
    rect = next((i.get("rect") for i in items if i.get("label") == label), None)
    return app.click_rect(rect)


def select_and_menu(app, a, b, inside):
    """Drags from point a to b, then right-clicks at point inside; the open menu or None."""
    app.drag(a, b)
    app.click(*inside, button=3)
    return menu_open(app)


def make_branch(app, a, b, inside, why):
    """Selects, right-clicks and runs Branch from here; (new chat id, seconds to open, menu)."""
    before = app.chat_id()
    menu = select_and_menu(app, a, b, inside)
    entry = menu_entry(menu, "Branch from here")
    if not check(bool(entry) and not entry.get("disabled"), f"{why}: Branch from here is enabled ({entry})"):
        key("Escape")
        return None, None, menu
    started = time.time()
    click_menu(app, "Branch from here")
    ok, sid, _ = poll(app.chat_id, lambda c: bool(c) and c != before, 10)
    return (sid if ok else None), time.time() - started, menu


def visible_windows():
    out = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", ""], capture_output=True, text=True).stdout.split()
    return set(out)


def png_pixels(path):
    """(width, height, rows of RGB tuples) of an 8-bit RGB or RGBA PNG (import's output)."""
    import struct, zlib
    with open(path, "rb") as f:
        data = f.read()
    pos, chunks = 8, []
    width = height = color = None
    while pos < len(data):
        n, kind = struct.unpack(">I4s", data[pos:pos + 8])
        body = data[pos + 8:pos + 8 + n]
        if kind == b"IHDR":
            width, height, depth, color = struct.unpack(">IIBB", body[:10])
            if depth != 8 or color not in (2, 6):
                raise ValueError(f"PNG depth {depth} colour type {color}")
        elif kind == b"IDAT":
            chunks.append(body)
        pos += 12 + n
    raw = zlib.decompress(b"".join(chunks))
    bpp = 3 if color == 2 else 4
    stride = width * bpp
    rows, prev = [], bytearray(stride)
    i = 0
    for _ in range(height):
        ftype = raw[i]
        line = bytearray(raw[i + 1:i + 1 + stride])
        i += 1 + stride
        for x in range(stride):
            a = line[x - bpp] if x >= bpp else 0
            b = prev[x]
            c = prev[x - bpp] if x >= bpp else 0
            if ftype == 1:
                line[x] = (line[x] + a) & 255
            elif ftype == 2:
                line[x] = (line[x] + b) & 255
            elif ftype == 3:
                line[x] = (line[x] + (a + b) // 2) & 255
            elif ftype == 4:
                p = a + b - c
                pa, pb, pc_ = abs(p - a), abs(p - b), abs(p - c)
                line[x] = (line[x] + (a if pa <= pb and pa <= pc_ else b if pb <= pc_ else c)) & 255
        rows.append([tuple(line[x * bpp:x * bpp + 3]) for x in range(width)])
        prev = line
    return width, height, rows


def near(c, want, tol=3):
    return all(abs(a - b) <= tol for a, b in zip(c, want))


def rect_moves(a, b):
    """The largest change of x, y, w or h between two rects (None when one is missing)."""
    if not a or not b:
        return None
    return max(abs(a[k] - b[k]) for k in ("x", "y", "w", "h"))


def settings_closed():
    try:
        with open(SETTINGS) as f:
            return (json.load(f) or {}).get("closed_chats") or {}
    except (OSError, ValueError):
        return {}


def tmux(*args):
    return subprocess.run(TMUX + list(args), capture_output=True, text=True)


def pane():
    r = tmux("capture-pane", "-p", "-t", "tui")
    return r.stdout if r.returncode == 0 else ""


def set_script(port, name):
    return pc.set_active_script(port, name)


def seed(turns, title):
    """seed_chat.py through the project's engine; the new chat's id or None."""
    out = subprocess.run([sys.executable, "-I", os.path.join(ROOT, "tests", "fakes", "seed_chat.py"), "--cwd", PROJ,
                          "--turns", str(turns), "--title", title, "--crush", CRUSH], capture_output=True, text=True, timeout=300)
    if out.returncode != 0:
        print(f"NOTE seed_chat failed: {out.stderr.strip()[-300:]}", flush=True)
        return None
    return json.loads(out.stdout.strip().splitlines()[-1]).get("session_id")


def answer_init(app, engine):
    """START-002: the fresh project asks first; n answers No."""
    app.focus()
    app.park()
    for _ in range(20):
        if not engine.needs_init():
            break
        key("n")
        time.sleep(0.5)
    value = app.composer().get("value") or ""
    if value and set(value) == {"n"}:  # an n that reached the typing box
        key(*["BackSpace"] * len(value))
    return not engine.needs_init()


def setup(app, port, title="Cookie review"):
    """Joins, answers init, seeds the five-turn chat and opens it; (engine, S) or (None, None)."""
    if not check(app.connected(45), "the window joined the project's engine"):
        return None, None
    check(app.place(), f"the window is {WIDTH}x{HEIGHT} at the screen's corner")
    engine = app.engine()
    if not check(engine is not None, "the probe names the engine's socket and workspace"):
        return None, None
    check(answer_init(app, engine), "the project's init question is answered (No)")
    check(set_script(port, "stream2m"), "the fake server plays stream2m, whose seed turns these checks use")
    sid = seed(5, title)
    if not check(bool(sid), f"seed_chat.py made the five-turn chat {sid}"):
        return engine, None
    ok, _, _ = poll(lambda: app.row(sid), bool, 15)
    check(ok, "the seeded chat is listed")
    check(app.open_chat(sid), "its row opens it")
    ok, _, _ = poll(lambda: app.section("chat").get("messages") or [], lambda ms: len(ms) == 12, 15)
    check(ok, "the window shows its 12 messages")
    return engine, sid


def stop_after(row):
    args = sys.argv[4:]
    return args[args.index("--stop-after") + 1] == row if "--stop-after" in args else False


# --- The steps ---------------------------------------------------------------

def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    app = App(pc.AppRun.existing(probe_path, PROJ))
    tmux("kill-server")
    s = {}  # what later steps need: chat ids, message ids, link words
    try:
        with step("setup", out_dir):
            engine, S = setup(app, port)
        if not S:
            check(False, "the remaining steps need the seeded chat; stopping here")
            return
        msgs = engine.messages(S)
        ids = [m["id"] for m in msgs]
        u1, a1, u2, a2 = ids[0], ids[1], ids[2], ids[3]
        tool_reply = next(m for m in msgs if text_of(m) == "Checking the file first.")
        a5 = tool_reply["id"]
        s.update(S=S, a1=a1, u2=u2, a2=a2, a5=a5)
        key_a2 = app.item_key(a2, "reply") or a2

        with step("B1", out_dir):
            words = app.bring_into_view(key_a2, ["shared", "stolen"])
            w = find_word(words, "design")
            before = visible_windows()
            app.click(*word_point(w, frac=0.5), button=3)
            menu = menu_open(app)
            labels = [i.get("label") for i in (menu or {}).get("items") or []]
            check(labels == MENU, f"the menu shows exactly our four entries {labels}")
            time.sleep(0.5)
            extra = visible_windows() - before
            check(not extra, f"no native menu window appears ({len(extra)} new windows)")
            h = app.heavy()
            box = (h.get("menu") or {}).get("box")
            path = shot(out_dir, "B1-menu")
            if check(bool(box), f"the probe has the menu box {box}"):
                width, height, px = png_pixels(path)
                ox, oy = app.screen(0, 0)
                x0, y0 = round(ox + box["x"]), round(oy + box["y"])
                x1, y1 = x0 + round(box["w"]) - 1, y0 + round(box["h"]) - 1
                edge = [px[(y0 + y1) // 2][x0], px[(y0 + y1) // 2][x1], px[y0][(x0 + x1) // 2], px[y1][(x0 + x1) // 2]]
                check(all(near(c, RULE_CONTROL, 6) for c in edge), f"the capture has our box's hairline where the probe says ({edge})")
                inner = [px[y0 + 2][x] for x in range(x0 + 4, x1 - 4)]
                share = sum(near(c, PAPER_RAISED, 3) for c in inner) / max(1, len(inner))
                check(share > 0.9, f"and our paper inside it ({share:.0%} of its top padding row)")
            key("Escape")
            ok, _, _ = poll(lambda: app.section("menu").get("open"), lambda o: not o, 3)
            check(ok, "Esc closes the menu")
        if stop_after("B1"):
            return

        with step("B2", out_dir):
            words = app.bring_into_view(key_a2, ["shared", "stolen"])
            first = find_word(words, "shared")
            last = find_word(words, "stolen", after=first["i"])
            B, took, _ = make_branch(app, word_point(first), word_point(last, end=True), word_point(between(words, first, last), frac=0.5), "B2")
            if check(bool(B), f"Branch from here opened a new chat {B}"):
                check(took is not None and took <= 2.0, f"at once ({took:.2f} s)")
                link = engine.origin(B)
                stored = text_of(next(m for m in engine.messages(S) if m["id"] == a2))
                cut = (link or {}).get("highlight_text", "")
                check(bool(link) and cut == stored[link["highlight_start"]:link["highlight_end"]] and cut.startswith("shared") and cut.endswith("stolen") and "\n\n" in cut,
                      f"the engine's origin link holds the words across the paragraph break as stored ({cut!r})")
                want_title = branch_title(cut)
                check(len(flat(cut)) > TITLE_MAX and want_title.endswith("…") and len(want_title) == TITLE_MAX, f"the words are over 34 characters, so the title is cut: {want_title!r}")
                check((engine.sessions().get(B) or {}).get("title") == want_title, f"the branch's title is branchTitle(words) ({(engine.sessions().get(B) or {}).get('title')!r})")
                ok, row, _ = poll(lambda: app.row(B), lambda r: bool(r) and r.get("title") == want_title, 5)
                check(ok, f"its row shows that title ({(row or {}).get('title')!r})")
                draft = quote_draft(cut)
                ok, comp, _ = poll(app.composer, lambda c: c.get("value") == draft, 5)
                check(ok, f"the field holds quoteDraft(words), the words in full on one line ({comp.get('value')!r})")
                check("…" not in (comp.get("value") or ""), "the field does not hold the shortened title")
                check(comp.get("selection") == [len(draft), len(draft)], f"the caret is at its end ({comp.get('selection')})")
                check(bool(comp.get("focused")), "the field has the focus")
                s.update(B=B, B_title=want_title, B_words=cut)
                check(app.open_chat(S), "back in the source chat")
                app.bring_into_view(key_a2, ["shared", "stolen"])
                shown = [l for l in app.shown_links() if l.get("session_id") == B]
                check(len(shown) == 1 and shown[0].get("mid") == a2 and flat(shown[0].get("text")).replace(".The", ". The") == flat(cut),
                      f"the source shows the words as one link to the branch ({shown})")
                if shown:
                    check(hover_title(app, shown[0]) == f"Open {want_title}", f"its hover title says Open <branch> ({hover_title(app, shown[0])!r})")
                    app.park()
                letter = next((i.get("letter") for i in app.items() if i.get("key") == key_a2), "")
                check(bool(letter), f"the reply has a key on the rail ({letter!r})")
        if stop_after("B2"):
            return

        with step("B6", out_dir):
            key_a1 = app.item_key(a1, "reply") or a1
            words = app.bring_into_view(key_a1, ["Short", "keeps", "plain", "going"])
            w_short, w_keeps, w_plain = find_word(words, "Short"), find_word(words, "keeps"), find_word(words, "plain")
            mid = word_point(w_keeps, frac=0.5)
            first, _, _ = make_branch(app, word_point(w_short), mid, word_point(find_word(words, "chat"), frac=0.5), "B6 first")
            l1 = engine.origin(first) if first else None
            head = (l1 or {}).get("highlight_text", "")
            check(head.startswith("Short") and any(head.endswith("chat " + part) for part in ("k", "ke", "kee", "keep")),
                  f"the first cut ends inside a word ({head!r})")
            check(app.open_chat(S), "back in the source chat")
            # The link now ends inside "keeps": the word is drawn as two text
            # nodes, the link's and the rest, so the rest starts at the link's end.
            rest = "keeps"[len(head.rsplit(" ", 1)[-1]):] if head else "eps"
            words = app.bring_into_view(key_a1, [rest, "plain", "going"])
            w_rest, w_plain = find_word(words, rest), find_word(words, "plain")
            second, _, _ = make_branch(app, word_point(w_rest, frac=0.0), word_point(w_plain, end=True), word_point(find_word(words, "going"), frac=0.5), "B6 touching")
            l2 = engine.origin(second) if second else None
            check(bool(l1 and l2) and l2["highlight_start"] == l1["highlight_end"],
                  f"words starting where the link ends branch too: touching ranges work ({(l1 or {}).get('highlight_end')} → {(l2 or {}).get('highlight_start')}, {(l2 or {}).get('highlight_text')!r})")
            check(app.open_chat(S), "back in the source chat")
            words = app.bring_into_view(key_a1, ["chat", "going"])
            count = len(engine.links())
            menu = select_and_menu(app, word_point(find_word(words, "chat")), word_point(find_word(words, "going"), end=True), word_point(find_word(words, "going"), frac=0.5))
            entry = menu_entry(menu, "Branch from here")
            check(bool(entry) and entry.get("disabled") and entry.get("hint") == HINT_OVERLAP,
                  f"words over both links: Branch from here is disabled with \"{HINT_OVERLAP}\" ({entry})")
            click_menu(app, "Branch from here")
            time.sleep(1.5)
            check(len(engine.links()) == count and app.chat_id() == S, "pressing it anyway sends nothing: no branch is made")
            key("Escape")
            s.update(B6=[x for x in (first, second) if x])
        if stop_after("B6"):
            return

        with step("B3", out_dir):
            check(app.open_chat(S), "in the source chat")
            key_u2 = app.item_key(u2, "user") or u2
            words = app.bring_into_view(key_u2, ["turn", "medium"])
            B3, _, _ = make_branch(app, word_point(find_word(words, "turn")), word_point(find_word(words, "medium"), end=True), word_point(find_word(words, "turn"), frac=0.5), "B3")
            if check(bool(B3), f"a branch from your own message's words opened {B3}"):
                got = engine.messages(B3)
                check([m["role"] for m in got] == ["user", "assistant", "user"] and text_of(got[-1]) == "Seed turn medium 2",
                      f"the branch ends with that message ({[(m['role'], text_of(m)[:30]) for m in got]})")
                ok, _, _ = poll(app.composer, lambda c: c.get("value") == quote_draft("turn medium"), 5)
                check(ok, "its field holds the quoted words")
                prompt = "B3 check: the answer after the quote"
                type_text(prompt)
                ok, _, _ = poll(app.composer, lambda c: (c.get("value") or "").endswith(prompt), 5)
                key("Return")
                ok, got, _ = poll(lambda: engine.messages(B3), lambda ms: len(ms) >= 5 and any(p.get("type") == "finish" for p in ms[-1].get("parts") or []), 30)
                roles = [m["role"] for m in got]
                check(ok and roles[:5] == ["user", "assistant", "user", "user", "assistant"], f"the send ran after the copied message, unanswered before it ({roles})")
                asks = [e for e in fake_ai_requests() if e.get("kind") in ("turn", "default") and "B3 check" in (e.get("last_user") or "")]
                source = [e for e in fake_ai_requests() if e.get("kind") in ("turn", "default") and (e.get("last_user") or "") == "Seed turn medium 2"]
                n_branch = asks[0].get("messages") if asks else None
                n_source = source[0].get("messages") if source else None
                check(n_branch is not None and n_source is not None and n_branch == n_source + 1,
                      f"the AI request holds the copied message unanswered, then the new prompt: one message more than the source's request for it ({n_branch} vs {n_source})")
                s["B3"] = B3
        if stop_after("B3"):
            return

        with step("B4", out_dir):
            check(app.open_chat(S), "in the source chat")
            key_a5 = app.item_key(a5, "reply") or a5
            words = app.bring_into_view(key_a5, ["Checking", "first."])
            w0 = find_word(words, "Checking")
            w1 = find_word(words, "first.", after=w0["i"])
            B4, _, _ = make_branch(app, word_point(w0), word_point(w1, end=True), word_point(between(words, w0, w1), frac=0.5), "B4")
            if check(bool(B4), f"a branch from the tool turn's reply opened {B4}"):
                src = engine.messages(S)
                at = next(i for i, m in enumerate(src) if m["id"] == a5)
                calls = {(p.get("data") or {}).get("id") for p in src[at].get("parts") or [] if p.get("type") == "tool_call"}
                answer = next(m for m in src[at + 1:] if m["role"] == "tool" and any((p.get("data") or {}).get("tool_call_id") in calls for p in m.get("parts") or []))
                want = [shape_of(m) for m in src[: at + 1]] + [shape_of(answer)]
                got = [shape_of(m) for m in engine.messages(B4)]
                check(got == want, f"the branch holds the source's path through the cut and the tool's result ({len(got)} messages, want {len(want)}; roles {[g[0] for g in got]})")
                s["B4"] = B4
        if stop_after("B4"):
            return

        with step("B5", out_dir):
            check(app.open_chat(S), "in the source chat")
            tool_key = next((i["key"] for i in app.items() if i.get("kind") in ("tools", "tool") and i.get("id") == a5), None)
            if check(bool(tool_key), f"the tool row is in the timeline ({tool_key})"):
                # The group's row opens it (its "Ran 1 tool" toggle; the rail's
                # letter is not the row), then the tool's own line opens its body.
                words = app.bring_into_view(tool_key, ["Ran"])
                ran = find_word(words, "Ran")
                if ran:
                    app.click(*word_point(ran, frac=0.5))
                    poll(lambda: next((i.get("expanded") for i in app.items() if i.get("key") == tool_key), None), bool, 5)
                    time.sleep(0.8)
                for _ in range(3):
                    got, _ = app.visible_words(tool_key, ["package"])
                    if got:
                        break
                    words, _ = app.visible_words(tool_key)
                    ran = find_word(words or [], "Ran")
                    line = [w for w in words or [] if ran and w["rect"]["y"] > ran["rect"]["y"] + ran["rect"]["h"]]
                    if line:
                        app.click(*word_point(line[-1], frac=0.5))
                        time.sleep(0.8)
                words = app.bring_into_view(tool_key, ["package"])
                pkg = find_word(words, "package")
                if check(bool(pkg) and len([w for w in words if w["i"] > pkg["i"]]) >= 3, f"the tool's body is on screen ({[w['text'] for w in words[:12]]})"):
                    after = [w for w in words if w["i"] > pkg["i"]]
                    last = after[min(len(after) - 1, 6)]
                    menu = select_and_menu(app, word_point(pkg), word_point(last, end=True), word_point(between(words, pkg, last), frac=0.5))
                    entry = menu_entry(menu, "Branch from here")
                    check(bool(entry) and entry.get("disabled") and entry.get("hint") == HINT_TOOL,
                          f"a selection in the tool's body: Branch from here is disabled with \"{HINT_TOOL}\" ({entry})")
                    key("Escape")
        if stop_after("B5"):
            return

        with step("B12", out_dir):
            B = s.get("B")
            if not check(bool(B), "B2's branch exists"):
                raise RuntimeError("B12 needs B2's branch")
            check(app.open_chat(S), "in the source chat")
            words = app.bring_into_view(key_a2, ["HttpOnly", "scripts"])
            w0 = find_word(words, "HttpOnly")
            w1 = find_word(words, "scripts", after=w0["i"])
            C, _, _ = make_branch(app, word_point(w0), word_point(w1, end=True), word_point(between(words, w0, w1), frac=0.5), "B12 sibling")
            if check(bool(C), f"a sibling branch C opened {C}"):
                c_a2 = next(m["id"] for m in engine.messages(C) if text_of(m) == text_of(next(x for x in engine.messages(S) if x["id"] == a2)))
                key_c_a2 = app.item_key(c_a2, "reply") or c_a2
                app.bring_into_view(key_c_a2, ["shared", "stolen"])
                copied = [l for l in app.shown_links() if l.get("session_id") == B and l.get("mid") == c_a2]
                if check(len(copied) == 1, f"inside C the link copied with the reply leads to its sibling B ({copied})"):
                    r = copied[0]["rects"][0]
                    app.click(r["x"] + r["w"] / 2, r["y"] + r["h"] / 2)
                    ok, _, _ = poll(app.chat_id, lambda c: c == B, 5)
                    check(ok, "clicking it opens the sibling B")
            s["C"] = C
            if app.chat_id() != B:
                app.open_chat(B)
            b_a2 = next(m["id"] for m in engine.messages(B) if m["role"] == "assistant" and "Secure keeps" in text_of(m))
            key_b_a2 = app.item_key(b_a2, "reply") or b_a2
            words = app.bring_into_view(key_b_a2, ["Secure", "connections,"])
            w0 = find_word(words, "Secure")
            w1 = find_word(words, "connections,", after=w0["i"])
            D, _, _ = make_branch(app, word_point(w0), word_point(w1, end=True), word_point(between(words, w0, w1), frac=0.5), "B12 depth")
            if check(bool(D), f"a branch of the branch opened {D}"):
                ok, rows, _ = poll(app.rows, lambda rs: any(r.get("id") == D for r in rs), 5)
                depth = {r.get("id"): r.get("depth") for r in rows}
                check(depth.get(S) == 0 and depth.get(B) == 1 and depth.get(D) == 2, f"it nests one level deeper: S {depth.get(S)}, B {depth.get(B)}, its branch {depth.get(D)}")
                head = (app.heavy().get("branch") or {}).get("head") or {}
                check(flat(head.get("text")).upper() == f"BRANCHED FROM {s['B_title']}".upper(), f"its head's path line says BRANCHED FROM <B> ({head.get('text')!r})")
                s["D"] = D
        if stop_after("B12"):
            return

        with step("B9", out_dir):
            B, D = s.get("B"), s.get("D")
            if not check(bool(B and D), "B and its branch exist"):
                raise RuntimeError("B9 needs B12's chats")
            if app.chat_id() != D:
                app.open_chat(D)
            app.park()

            def row_of(h, sid):
                return next((r for r in (h.get("branch") or {}).get("rows") or [] if r.get("id") == sid), None)

            plain = row_of(app.heavy(), B)
            check(bool(plain) and not plain.get("close_shown"), f"B's row plain: the slot is empty ({(plain or {}).get('close_shown')})")
            t = plain["title"]
            app.move(t["x"] + min(40, t["w"] / 2), t["y"] + t["h"] / 2)
            hovered = row_of(app.heavy(), B)
            check(bool(hovered) and hovered.get("close_shown"), "hovered: the × shows in it")
            app.click(t["x"] + min(40, t["w"] / 2), t["y"] + t["h"] / 2)
            ok, _, _ = poll(app.chat_id, lambda c: c == B, 5)
            check(ok, "a click selects the row (B opens)")
            time.sleep(0.4)
            selected_hover = row_of(app.heavy(), B)
            app.park()
            selected = row_of(app.heavy(), B)
            check(bool(selected) and selected.get("close_shown"), "selected: the × shows without the pointer")
            # The title twice: its box decides where a cut title's "…" falls;
            # its letters' box keeps the whole text and shows where they start.
            for name, label in (("title", "title's box"), ("title_text", "title's letters"), ("time", "time"), ("chevron", "chevron")):
                moves = [rect_moves((plain or {}).get(name), (other or {}).get(name)) for other in (hovered, selected_hover, selected)]
                check(all(m is not None and m <= 1.0 for m in moves), f"the {label} moves at most 1 real px between plain, hovered and selected ({moves})")
            g, chev, title = (selected or {}).get("close_glyph"), (selected or {}).get("chevron"), (selected or {}).get("title")
            if check(bool(g and chev and title), "the × glyph, the chevron and the title are drawn"):
                dx = abs((g["x"] + g["w"] / 2) - (chev["x"] + chev["w"] / 2))
                dy = abs((g["y"] + g["h"] / 2) - (title["y"] + title["h"] / 2))
                check(dx <= 1.0 and dy <= 1.5, f"the × sits in the reserved slot: on the chevron's column ({dx:.2f} px) and the title's first line ({dy:.2f} px)")
            check((selected or {}).get("close_label") == f"Close {s['B_title']}", f"the × is a button named Close <title> ({(selected or {}).get('close_label')!r})")
            shot(out_dir, "B9-selected")
            app.click_rect(selected["close"])
            ok, rows, _ = poll(app.rows, lambda rs: not any(r.get("id") == B for r in rs), 5)
            check(ok, "its × hides B")
            depth = {r.get("id"): r.get("depth") for r in rows}
            check(depth.get(D) == 1, f"B's branch moves up to depth 1, under S ({depth.get(D)})")
            ok, footer, _ = poll(lambda: (app.heavy().get("branch") or {}).get("footer"), lambda f: bool(f) and f.get("text") == "1 closed · links reopen them", 5)
            check(ok, f"the footer says \"1 closed · links reopen them\" ({(footer or {}).get('text')!r})")
            ok, _, _ = poll(app.chat_id, lambda c: c == S, 5)
            check(ok, f"the window opens the first chat still listed, S ({app.chat_id()})")
            closed_sets = settings_closed()
            check(any(B in ids for ids in closed_sets.values()), f"the settings file holds the closing ({ {k: len(v) for k, v in closed_sets.items()} })")
        if stop_after("B9"):
            return

        with step("B10", out_dir):
            B = s.get("B")
            check(app.open_chat(S), "in the source chat")
            app.bring_into_view(key_a2, ["shared", "stolen"])
            link = [l for l in app.shown_links() if l.get("session_id") == B]
            if check(bool(link), "the link to the closed branch still shows in S"):
                title = hover_title(app, link[0])
                check(title == f"Reopen {s['B_title']}", f"its hover title says Reopen <branch> ({title!r})")
                r = link[0]["rects"][0]
                app.click(r["x"] + r["w"] / 2, r["y"] + r["h"] / 2)
                ok, _, _ = poll(app.chat_id, lambda c: c == B, 5)
                check(ok, "clicking it opens B")
                ok, _, _ = poll(lambda: app.branch().get("closed") or [], lambda ids: B not in ids, 5)
                check(ok and bool(app.row(B)), "and B is listed again")
                check(not any(B in ids for ids in settings_closed().values()), "the settings file no longer holds it")
        if stop_after("B10"):
            return

        with step("B15", out_dir):
            B = s.get("B")
            check(app.open_chat(S), "in the source chat")
            tree = engine.get(f"sessions/{S}/tree")
            head = next((e["message_id"] for e in tree.get("entries") or [] if e.get("active")), None)
            engine.call("POST", f"sessions/{S}/tree/switch", {"expected_revision": tree["revision"], "target": a1})
            ok, _, _ = poll(lambda: [m.get("id") for m in app.section("chat").get("messages") or []], lambda ids: a2 not in ids and a1 in ids, 10)
            check(ok, "the tree switch moved S's head off the linked reply (the window reloaded the path)")
            time.sleep(0.5)
            gone = [l for l in app.shown_links() if l.get("session_id") == B]
            check(not gone, f"the link to B is gone ({gone})")
            tree = engine.get(f"sessions/{S}/tree")
            engine.call("POST", f"sessions/{S}/tree/switch", {"expected_revision": tree["revision"], "target": head})
            ok, _, _ = poll(lambda: [m.get("id") for m in app.section("chat").get("messages") or []], lambda ids: a2 in ids, 10)
            check(ok, "switching back brings the reply back")
            app.bring_into_view(key_a2, ["shared", "stolen"])
            back = [l for l in app.shown_links() if l.get("session_id") == B]
            check(len(back) == 1, f"and the link shows again ({len(back)})")
        if stop_after("B15"):
            return

        with step("B14", out_dir):
            started = tmux("new-session", "-d", "-s", "tui", "-x", "200", "-y", "50", f"env CRUSH_CLIENT_SERVER=1 {CRUSH} --cwd {PROJ}")
            check(started.returncode == 0, "the terminal app starts in a hidden tmux")
            time.sleep(4)
            tmux("send-keys", "-t", "tui", "C-s")
            title = s.get("B_title", "")
            ok, text, _ = poll(pane, lambda p: title in p or flat(title[:25]) in flat(p), 10, 0.3)
            with open(os.path.join(out_dir, "B14.pane.txt"), "w") as f:
                f.write(text)
            check(ok, f"its sessions list shows the branch by its title ({title!r}; full title {'shown' if title in text else 'cut by the terminal'})")
            tmux("send-keys", "-t", "tui", "Escape")
            tmux("kill-server")
        if stop_after("B14"):
            return

        with step("B13", out_dir):
            B, D = s.get("B"), s.get("D")
            check(app.open_chat(B), "B is shown")
            ok, head, _ = poll(lambda: (app.heavy().get("branch") or {}).get("head") or {}, lambda h: flat(h.get("text")).upper().startswith("BRANCHED FROM"), 5)
            check(ok, f"its head says BRANCHED FROM ({head.get('text')!r})")
            branches = [x for x in [B, s.get("C"), s.get("B3"), s.get("B4"), *s.get("B6", [])] if x]
            engine.call("DELETE", f"sessions/{S}")
            ok, rows, _ = poll(app.rows, lambda rs: not any(r.get("id") == S for r in rs), 10)
            check(ok, "S is gone from the list")
            ok, rows, _ = poll(app.rows, lambda rs: all(any(r.get("id") == x and r.get("depth") == 0 for r in rs) for x in branches), 10)
            depth = {r.get("id"): r.get("depth") for r in rows}
            check(ok, f"every branch of S stays, at the top level ({[depth.get(x) for x in branches]})")
            check(depth.get(D) == 1, f"B's own branch stays under B ({depth.get(D)})")
            ok, head, _ = poll(lambda: (app.heavy().get("branch") or {}).get("head") or {}, lambda h: not flat(h.get("text")).upper().startswith("BRANCHED FROM"), 5)
            check(ok, f"B's head now shows the folder line ({head.get('text')!r})")
    finally:
        tmux("kill-server")


if __name__ == "__main__":
    main()
    finish()
