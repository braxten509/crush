"""Safety check in the real window (desktop-design 2.9 "Safety" and "The
environment never reaches the window"; 1.7; PLAN.md section 15).

Fixture first, then the engine:
  tests/check.sh --fixture tests/fixtures/unsafe-chat.json p3_safety.py
  tests/check.sh p3_safety.py
The fixture shows the chat "Unsafe replies"; with the engine the driver picks
the fake server's script "unsafe" and sends its four prompts. Either way the
window then holds the replies of tests/fakes/scripts/unsafe.json.

Steps (each one PASS or FAIL, with a screenshot in tests/out/p3_safety/):
  corpus   the replies are drawn; the probe's safety scans find no script,
           frame, on* attribute, address that is not allowed or form control;
           no content-rule violation; nothing opened; nothing reached the
           listener on 127.0.0.1:47321 (the address the corpus aims at: the
           fake server's own port changes every run, so a script cannot name
           it) and the fake server logged no beacon; the fake permission slot
           is plain text in the reply
  links    on the one http link and on every refused link: a middle click and
           a third-button click (and a left click on each refused one); a drag
           of the http link 200 px toward the field; a left click on the http
           link, then Enter on it (the click focused it). Only those two ask to
           open (the safety section's links_asked) and add lines to
           <probe>.opened.jsonl; no refused navigation; the page never moves;
           the field holds no dropped address
  diagram  the Mermaid figure draws (strict) or shows its source with the
           fallback line, in VISUALS and again in the chat with the right
           panel hidden (picture size in a 900 px window, SIDE-002); the
           content rule still saw nothing and the scans stay clean
  env      the workspace keys hold neither env nor config; the run's
           environment mark (and the fixture's own secret) never appears in a
           probe write, opened.jsonl, the app's api log, its output or any
           file the run left in tests/out/p3_safety/
"""
import glob, json, os, subprocess, sys, threading, time
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

sys.path.insert(0, os.path.dirname(__file__))
from harness import _screen, check, click, fake_ai_requests, finish, key, probe_heavy, probe_section, read_probe, step, wait_for, xdo

probe_path, out_dir, fake_port = sys.argv[1], sys.argv[2], sys.argv[3]
LISTENER = ("127.0.0.1", 47321)
PAGE = "http://127.0.0.1:47321/page.html"
FIXTURE_SECRET = "leak-fixture-5e1f0c2d9a7b3e48"  # tests/fixtures/unsafe-chat.json workspace env and config
SCRIPT = json.load(open(os.path.join(os.path.dirname(__file__), "..", "fakes", "scripts", "unsafe.json")))
PROMPTS = [turn["when"] for turn in SCRIPT["turns"]]
# Words each drawn reply must show (one per reply of the script).
SHOWN = ["The markup reply ends here.", "Links a reply might try.", "A diagram that asks for loose security", "Allow bash to run this?"]
REFUSED_LINKS = ["javascript link", "data link", "tab first", "space first", "upper case", "vbscript link", "blob link",
                 "data html", "entity link", "colon entity", "reference link", "javascript:alert(1)"]
RAW_LINKS = ["raw tab first", "raw space first", "raw upper case", "raw newline inside", "raw vbscript", "raw blob", "raw data"]
os.makedirs(out_dir, exist_ok=True)
with open(os.path.join(out_dir, "env-mark")) as f:
    MARK = f.read().strip()
SECRETS = [s for s in (MARK, FIXTURE_SECRET) if s]


def note(text):
    print(f"NOTE {text}", flush=True)


# The listener the corpus aims its pictures, frames and form at.
hits = []


class Listener(BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def _hit(self):
        hits.append({"method": self.command, "path": self.path})
        self.send_response(204)
        self.end_headers()

    do_GET = do_POST = do_HEAD = _hit


listener = ThreadingHTTPServer(LISTENER, Listener)
threading.Thread(target=listener.serve_forever, daemon=True).start()

# Every probe write, read as it lands: the page's address, and no secret.
seen = {"writes": 0, "secret_in": [], "locations": set()}


def watch_probe():
    last = None
    while True:
        try:
            st = os.stat(probe_path)
            stamp = (st.st_mtime_ns, st.st_size)
            if stamp != last:
                last = stamp
                with open(probe_path) as f:
                    text = f.read()
                seen["writes"] += 1
                for secret in SECRETS:
                    if secret in text and secret not in seen["secret_in"]:
                        seen["secret_in"].append(secret)
                try:
                    location = (json.loads(text).get("safety") or {}).get("location")
                    if location:
                        seen["locations"].add(location)
                except ValueError:
                    pass
        except OSError:
            pass
        time.sleep(0.05)


threading.Thread(target=watch_probe, daemon=True).start()


def app_window(timeout=20):
    """The id of the largest visible window titled "Crush" (GTK also makes tiny hidden ones)."""
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^Crush$"], capture_output=True, text=True)
        best = None
        for wid in r.stdout.split():
            g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
            d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
            if "WIDTH" in d:
                area = int(d["WIDTH"]) * int(d["HEIGHT"])
                if best is None or area > best[1]:
                    best = (wid, area, int(d["WIDTH"]), int(d["HEIGHT"]))
        if best and best[1] > 100 * 100:
            return best
        time.sleep(0.3)
    return None


def opened_lines():
    path = probe_path + ".opened.jsonl"
    if not os.path.exists(path):
        return []
    with open(path) as f:
        return [line for line in f.read().splitlines() if line.strip()]


def safety_heavy():
    report = probe_heavy(probe_path)
    return ((report.get("heavy") or {}).get("safety")) or {}, report


def corpus_drawn(heavy):
    roots = " ".join(r.get("text", "") for r in heavy.get("roots") or [])
    return all(words in roots for words in SHOWN)


def fake_control(script):
    req = urllib.request.Request(f"http://127.0.0.1:{fake_port}/_fake/control", data=json.dumps({"script": script}).encode(),
                                 method="POST", headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as r:
        return r.status


def show_corpus():
    """Brings the four replies on screen: already there, the fixture's chat
    from the list, or the engine through the composer."""
    heavy, _ = safety_heavy()
    if corpus_drawn(heavy):
        return True
    conn = probe_section(probe_path, "connection", timeout=5) or {}
    fixture = str(conn.get("client_id", "")).startswith("fixture-") or conn.get("engine_version") == "fixture"
    rows = (probe_section(probe_path, "list", timeout=5) or {}).get("rows") or []
    row = next((r for r in rows if r.get("title") == "Unsafe replies"), None)
    if row:
        rects = (((probe_heavy(probe_path).get("heavy") or {}).get("list") or {}).get("rows")) or []
        rect = next((r.get("rect") for r in rects if r.get("id") == row.get("id")), None)
        if rect:
            click(rect["x"] + rect["w"] / 2, rect["y"] + rect["h"] / 2)
    elif not fixture and probe_section(probe_path, "composer", timeout=5) is not None:
        try:
            fake_control("unsafe")
        except Exception as e:  # noqa: BLE001 - the S3 fake server has no control call
            note(f"the fake server took no script ({e})")
        for prompt in PROMPTS:
            key("ctrl+a")
            xdo("type", "--delay", 20, "--", prompt)
            key("Return")
            wait_for(probe_path, lambda p: any(m.get("role") == "assistant" and m.get("finished")
                                               for m in ((p.get("chat") or {}).get("messages") or [])[-1:]), 60)
    else:
        note("no list row \"Unsafe replies\" and no composer to send the prompts from")
    end = time.time() + 30
    while time.time() < end:
        heavy, _ = safety_heavy()
        if corpus_drawn(heavy):
            return True
        time.sleep(0.5)
    return False


win = app_window()
if not check(win is not None, "the app window opens"):
    finish()
wid, _, start_w, start_h = win
safety = probe_section(probe_path, "safety", timeout=30)
if not check(safety is not None, "the probe has the renderer's safety section (the renderer is loaded)"):
    finish()

with step("safety-corpus", out_dir):
    drawn = show_corpus()
    check(drawn, "the four replies of script \"unsafe\" are drawn")
    time.sleep(1.0)
    heavy, report = safety_heavy()
    light = report.get("safety") or {}
    check(light.get("inline_blocked") is True, f"the content rule blocked the probe's own inline script and handler ({light.get('inline_blocked')})")
    for field in ("scripts_found", "handlers_found", "bad_hrefs_found", "controls_found"):
        check(heavy.get(field) == 0, f"{field} 0 ({heavy.get(field)})")
    check(light.get("csp_violations") == 0, f"csp_violations 0 ({light.get('csp_violations')}; {heavy.get('violations')})")
    anchors = heavy.get("anchors") or []
    check([a.get("href") for a in anchors] == [PAGE], f"one link in the corpus, the fake page ({[a.get('href') for a in anchors]})")
    refused = sorted(r.get("text") for r in heavy.get("refused") or [])
    check(refused == sorted(REFUSED_LINKS), f"every refused link spelling is text ({refused})")
    blocks = [b.get("text") for b in heavy.get("blocks") or []]
    check(all(words in blocks for words in RAW_LINKS), "every raw <a> shows as its words")
    check(any("Allow bash to run this?" in b for b in blocks), "the fake permission slot is plain text in the reply")
    composer = probe_section(probe_path, "composer", timeout=2)
    if composer is None:
        note("no composer section in the probe: the slot itself is not checked")
    else:
        check(composer.get("slot") in (None, {}), f"the composer's slot stays empty ({composer.get('slot')})")
    check(opened_lines() == [], f"<probe>.opened.jsonl stays empty ({opened_lines()})")
    check(hits == [], f"nothing reached {LISTENER[0]}:{LISTENER[1]} ({hits})")
    beacons = [r for r in fake_ai_requests() if r.get("beacon") or r.get("kind") == "beacon"]
    check(beacons == [], f"the fake server logged no beacon ({beacons})")

start_location = (probe_section(probe_path, "safety") or {}).get("location")

def visible_zone():
    """The part of the window where a link can be clicked (inside the chat,
    clear of the head and the composer), in viewport pixels."""
    shell = (read_probe(probe_path).get("shell")) or {"y": 0, "h": start_h}
    return shell["y"] + 0.18 * shell["h"], shell["y"] + 0.72 * shell["h"]


def on_screen(find, tries=40):
    """Scrolls the chat with the wheel until find(heavy) gives a rect inside the
    visible zone; returns that rect, or None."""
    top, bottom = visible_zone()
    for _ in range(tries):
        heavy, _ = safety_heavy()
        rect = find(heavy)
        if rect is None:
            return None
        if rect["y"] >= top and rect["y"] + rect["h"] <= bottom:
            return rect
        blocks = heavy.get("blocks") or []
        wx = (blocks[0]["rect"] if blocks else rect)["x"] + 40
        wy = (top + bottom) / 2
        click(wx, wy, 5 if rect["y"] + rect["h"] > bottom else 4)
        time.sleep(0.15)
    return None


def centre(rect):
    return rect["x"] + min(rect["w"] / 2, 12), rect["y"] + rect["h"] / 2


def page_link(heavy):
    return next((a["rect"] for a in heavy.get("anchors") or [] if a.get("href") == PAGE), None)


def notice_now():
    return ((probe_section(probe_path, "overlay") or {}).get("notice") or {})


with step("safety-links", out_dir):
    heavy, report = safety_heavy()
    names = [("refused", r["text"]) for r in heavy.get("refused") or []]
    names += [("raw", b["text"]) for b in heavy.get("blocks") or [] if b.get("text") in RAW_LINKS]
    if not check(page_link(heavy) is not None and len(names) == len(REFUSED_LINKS) + len(RAW_LINKS), f"the links to try are drawn ({len(names)} refused)"):
        raise RuntimeError("no links to try")
    before = len(opened_lines())
    asked = (report.get("safety") or {}).get("links_asked", 0)

    def find(kind, text):
        if kind == "refused":
            return lambda h: next((r["rect"] for r in h.get("refused") or [] if r["text"] == text), None)
        return lambda h: next((b["rect"] for b in h.get("blocks") or [] if b["text"] == text), None)

    tried = 0
    for kind, text in [("page", PAGE)] + names:
        rect = on_screen(page_link if kind == "page" else find(kind, text))
        if not check(rect is not None, f"{kind} {text!r} is on screen"):
            continue
        x, y = centre(rect)
        click(x, y, 2)
        time.sleep(0.2)
        click(x, y, 3)
        time.sleep(0.3)
        key("Escape")
        time.sleep(0.2)
        if kind != "page":
            click(x, y, 1)
            time.sleep(0.2)
        tried += 1
    light = probe_section(probe_path, "safety") or {}
    check(len(opened_lines()) == before and light.get("links_asked") == asked,
          f"middle and third-button clicks, and clicks on refused text, open nothing ({tried} tried; opened {opened_lines()[before:]}, asked {light.get('links_asked', 0) - asked})")

    # A drag of the http link toward the field: no address may travel.
    rect = on_screen(page_link)
    x, y = centre(rect)
    sx, sy = _screen(x, y)
    xdo("mousemove", sx, sy)
    xdo("mousedown", 1)
    for i in range(1, 11):
        xdo("mousemove", sx, sy + 20 * i)
        time.sleep(0.03)
    xdo("mouseup", 1)
    time.sleep(0.5)
    light = probe_section(probe_path, "safety") or {}
    check(len(opened_lines()) == before and light.get("links_asked") == asked, f"the drag opens nothing ({opened_lines()[before:]})")
    composer = probe_section(probe_path, "composer", timeout=2)
    if composer is None:
        note("no composer section in the probe: the field's text is not checked")
    else:
        check(PAGE not in str(composer.get("value", "")), "the field holds no dropped address")

    # A plain left click opens it once; the click focused the link, and Enter opens it once more.
    rect = on_screen(page_link)
    click(*centre(rect))
    time.sleep(0.8)
    if notice_now():
        note(f"the notice after the click: {notice_now()}")
    heavy, _ = safety_heavy()
    check((heavy.get("focus") or {}).get("href") == PAGE, f"the click left the http link focused ({heavy.get('focus')})")
    key("Return")
    time.sleep(0.8)
    if notice_now():
        note(f"the notice after Enter: {notice_now()}")
    light = probe_section(probe_path, "safety") or {}
    check(light.get("links_asked", 0) - asked == 2, f"the page asked to open the http link exactly twice ({light.get('links_asked', 0) - asked})")
    added = opened_lines()[before:]
    check(len(added) == 2 and all(PAGE in line for line in added), f"the left click and Enter each add one line to opened.jsonl ({added})")
    check(not any("refused_navigation" in line for line in opened_lines()), "no navigation reached the Rust guard")
    time.sleep(0.5)
    check(seen["locations"] == {start_location}, f"the page never moved ({sorted(seen['locations'])})")
    check(hits == [], f"nothing reached the listener ({hits})")

def diagrams(heavy, inline):
    return [f for f in heavy.get("figures") or [] if f.get("kind") == "diagram" and bool(f.get("inline")) == inline]


def check_diagram(inline):
    """The Mermaid figure in VISUALS or, with the panel hidden, in the chat."""
    where = "in the chat" if inline else "in VISUALS"
    end = time.time() + 30
    while True:
        heavy, _ = safety_heavy()
        figures = diagrams(heavy, inline)
        if (figures and all(f.get("state") in ("drawn", "fallback") for f in figures)) or time.time() > end:
            break
        time.sleep(0.5)
    states = [f.get("state") for f in figures]
    check(bool(figures) and all(s in ("drawn", "fallback") for s in states), f"{where}: the Mermaid figure draws or shows its source with the fallback line ({states})")
    if "fallback" in states:
        note(f"{where}: the diagram fell back to its source")
    light = probe_section(probe_path, "safety") or {}
    check(light.get("csp_violations") == 0, f"{where}: csp_violations still 0 with the figures drawn ({light.get('csp_violations')}; {heavy.get('violations')})")
    for field in ("scripts_found", "handlers_found", "bad_hrefs_found", "controls_found"):
        check(heavy.get(field) == 0, f"{where}: {field} 0 with the figures drawn ({heavy.get(field)})")
    check(hits == [], f"{where}: nothing reached the listener ({hits})")


def zoom_is(target):
    p = wait_for(probe_path, lambda p: p.get("zoom") is not None and abs(p["zoom"] - target) < 1e-6)
    return p.get("zoom") is not None and abs(p["zoom"] - target) < 1e-6


with step("safety-diagram", out_dir):
    if diagrams(safety_heavy()[0], False):
        check_diagram(False)
    else:
        note("no figure in VISUALS (nothing draws the panel's figures in this build)")
    # The right panel hides when the page is under 1,060 picture px wide (SIDE-002); the
    # window's least width is 900, so at picture size (Ctrl = four times from 8/12).
    xdo("windowfocus", "--sync", wid)
    for _ in range(4):
        key("ctrl+equal")
        time.sleep(0.2)
    check(zoom_is(1.0), "picture size (12/12) after four Ctrl = presses")
    xdo("windowsize", wid, 900, start_h)
    check_diagram(True)
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, "safety-diagram-inline.png")], check=False)
    key("ctrl+0")
    xdo("windowsize", wid, start_w, start_h)
    check(zoom_is(8 / 12), "Ctrl 0 returns to Compact (8/12)")
    time.sleep(1.0)

with step("safety-env", out_dir):
    light = probe_section(probe_path, "safety") or {}
    keys = light.get("workspace_keys") or []
    check(bool(keys) and "env" not in keys and "config" not in keys, f"the window's workspace has no env and no config ({keys})")
    check(seen["writes"] > 0 and not seen["secret_in"], f"no probe write held the environment mark or the fixture's secret ({seen['writes']} writes read)")
    leaked = []
    files = glob.glob(probe_path + "*") + [p for p in glob.glob(os.path.join(out_dir, "*"))
                                           if os.path.isfile(p) and not p.endswith((".png", "env-mark", "check.log"))]
    for path in files:
        try:
            with open(path, errors="replace") as f:
                text = f.read()
        except OSError:
            continue
        leaked += [os.path.basename(path) for secret in SECRETS if secret in text]
    check(not leaked, f"no file of the run holds the mark or the secret ({sorted(set(leaked))}; {len(files)} files read)")

listener.shutdown()
finish()
