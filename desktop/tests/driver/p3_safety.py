"""Safety check in the real window (desktop-design 2.9 "Safety" and "The
environment never reaches the window"; 1.7; PLAN.md section 15).

Fixture first, then the engine:
  tests/check.sh --fixture tests/fixtures/unsafe-chat.json p3_safety.py
  tests/check.sh p3_safety.py
The fixture shows the chat "Unsafe replies"; with the engine the driver picks
the fake server's script "unsafe" and sends its five prompts (after answering
the project's init question No). Either way the window then holds the replies
of tests/fakes/scripts/unsafe.json. The virtual list mounts only the view and
one view above and below (1.6), never the long chat whole, so the corpus and
the inline diagrams are read by wheeling the chat from its top to its bottom
and scanning every stop; each link is counted against the item that holds it.

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
  diagram  in the chat, at the start size and again at picture size in a
           900 px window (figures sit in the chat; no panel shows them since
           2026-10-19): every valid diagram of
           the diagrams reply (one per kind the renderer supports, titled
           "Valid <kind>") draws as an SVG that names its kind, and a fallback
           or a missing figure there fails; only the hostile Mermaid figure may
           draw (strict) or show its source with the fallback line. The content
           rule still saw nothing and the scans stay clean
  env      the workspace keys hold neither env nor config; the run's
           environment mark (and the fixture's own secret) never appears in a
           probe write, opened.jsonl, the app's api log, its output or any
           file the run left in tests/out/p3_safety/
"""
import glob, json, os, re, subprocess, sys, threading, time
from collections import Counter
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
# Words each drawn reply must show (one per reply of the script, within the
# first 400 characters the probe reports of each).
SHOWN = ["The markup reply ends here.", "Links a reply might try.", "A diagram that asks for loose security", "Allow bash to run this?",
         "Diagrams of every kind the renderer draws"]
# The diagrams reply: one valid diagram per kind the renderer supports
# (render/mermaid.js DIAGRAM_KINDS), each titled "Valid <kind>" in its front
# matter, <kind> being the name Mermaid puts on its SVG (aria-roledescription).
DIAGRAMS_TEXT = next("".join(s["text"] for s in t["steps"]) for t in SCRIPT["turns"] if t["when"] == "unsafe diagrams")
VALID = re.findall(r"^```mermaid\n---\ntitle: (Valid (\S+))\n---\n", DIAGRAMS_TEXT, re.M)
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
    """The id of the largest visible crush-desktop window (the title names the chat; tiny helper windows left out)."""
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
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


def scan():
    """The safety and chat heavy fields of one probe answer (the same moment)."""
    report = probe_heavy(probe_path)
    heavy = report.get("heavy") or {}
    return heavy.get("safety") or {}, heavy.get("chat") or {}, report


def owner(rect, chat):
    """The key of the timeline item whose frame holds rect's centre; "" outside
    the chat (the panel)."""
    cx, cy = rect["x"] + rect["w"] / 2, rect["y"] + rect["h"] / 2
    for frame in chat.get("items") or []:
        r = frame.get("rect")
        if r and r["x"] <= cx <= r["x"] + r["w"] and r["y"] <= cy <= r["y"] + r["h"]:
            return frame["key"]
    return ""


def wheel(chat, down, notches):
    """Wheel notches in the middle of the chat's scroller."""
    view = chat.get("view")
    if not view:
        return
    sx, sy = _screen(view["x"] + view["w"] / 2, view["y"] + view["h"] / 2)
    xdo("mousemove", sx, sy)
    xdo("click", "--repeat", notches, "--delay", 15, 5 if down else 4)


def sweep(visit):
    """The virtual list mounts only the view and one view above and below it
    (design 1.6 item 2, vlist.js windowRange), so the page never holds a long
    chat whole. This wheels the chat from its top to its bottom, a step of at
    most one view at a time (so every item is mounted at one of the stops),
    and calls visit(safety, chat) at each stop. False when the chat stopped
    moving before its bottom."""
    _, chat, _ = scan()
    for _ in range(200):
        if (chat.get("scroll_top") or 0) <= 0:
            break
        wheel(chat, False, 20)
        time.sleep(0.2)
        _, chat, _ = scan()
    notches, last, still = 3, None, 0
    for _ in range(800):
        time.sleep(0.3)
        safety, chat, _ = scan()
        visit(safety, chat)
        top = chat.get("scroll_top") or 0
        view_h = (chat.get("view") or {}).get("h") or 0
        if top + view_h >= (chat.get("scroll_height") or 0) - 1:
            return True
        if last is not None:
            moved = top - last
            if moved > 0:
                still = 0
                notches = max(1, min(notches * 2, int(notches * view_h / moved)))
            else:
                still += 1
                if still > 10:
                    return False
        last = top
        wheel(chat, True, notches)
    return False


SCANS = ("scripts_found", "handlers_found", "bad_hrefs_found", "controls_found")


def merge(total, seen):
    for k, n in seen.items():
        total[k] = max(total.get(k, 0), n)


def corpus():
    """What the chat draws over a whole sweep. Each link, refused link and
    paragraph is counted against the item that holds it, as often as one stop
    shows it there, so a link drawn twice still counts twice. scans holds every
    stop's safety scan."""
    out = {"roots": set(), "anchors": {}, "refused": {}, "blocks": {}, "scans": [], "violations": None}

    def visit(heavy, chat):
        out["roots"].update(r.get("text", "") for r in heavy.get("roots") or [])
        out["scans"].append({field: heavy.get(field) for field in SCANS})
        out["violations"] = heavy.get("violations")
        count = lambda items, name: Counter(name(x) for x in items)
        merge(out["anchors"], count(heavy.get("anchors") or [], lambda a: (owner(a["rect"], chat), a.get("href"), a.get("text"))))
        merge(out["refused"], count(heavy.get("refused") or [], lambda r: (owner(r["rect"], chat), r.get("text"))))
        merge(out["blocks"], count(heavy.get("blocks") or [], lambda b: (owner(b["rect"], chat), b.get("text"))))

    out["swept"] = sweep(visit)
    return out


def corpus_drawn(found):
    roots = " ".join(found["roots"])
    return all(words in roots for words in SHOWN)


def fake_control(script):
    req = urllib.request.Request(f"http://127.0.0.1:{fake_port}/_fake/control", data=json.dumps({"script": script}).encode(),
                                 method="POST", headers={"Content-Type": "application/json"})
    with urllib.request.urlopen(req, timeout=10) as r:
        return r.status


def show_corpus():
    """Brings the five replies on screen: already there, the fixture's chat
    from the list, or the engine through the composer."""
    found = corpus()
    if corpus_drawn(found):
        return found
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
        # START-002: a fresh project asks first and n answers No (as p3_reconnect.py
        # does); unanswered, the n of "unsafe" would answer it and the rest reach the field.
        start = wait_for(probe_path, lambda p: (p.get("start") or {}).get("needs_init") is not None, 20).get("start") or {}
        for _ in range(20):
            if start.get("needs_init") is not True:
                break
            key("n")
            time.sleep(0.5)
            start = probe_section(probe_path, "start", timeout=5) or {}
        check(start.get("needs_init") is False, f"the project's init question is answered No ({start.get('needs_init')})")
        for prompt in PROMPTS:
            field = lambda: probe_section(probe_path, "composer", timeout=5) or {}
            for _ in range(4):
                if field().get("focused"):
                    break
                key("Tab")
                time.sleep(0.3)
            if field().get("value"):
                key("ctrl+shift+a", "BackSpace")  # TYPE-009: selects all in the field
            xdo("type", "--delay", 20, "--", prompt)
            typed = (wait_for(probe_path, lambda p: (p.get("composer") or {}).get("value") == prompt, 5).get("composer") or {}).get("value")
            check(typed == prompt, f"the field holds {prompt!r} before Enter ({typed!r})")
            key("Return")
            wait_for(probe_path, lambda p: any(m.get("role") == "assistant" and m.get("finished")
                                               for m in ((p.get("chat") or {}).get("messages") or [])[-1:]), 60)
    else:
        note("no list row \"Unsafe replies\" and no composer to send the prompts from")
    end = time.time() + 60
    while time.time() < end:
        found = corpus()
        if corpus_drawn(found):
            break
        time.sleep(0.5)
    return found


win = app_window()
if not check(win is not None, "the app window opens"):
    finish()
wid, _, start_w, start_h = win
safety = probe_section(probe_path, "safety", timeout=30)
if not check(safety is not None, "the probe has the renderer's safety section (the renderer is loaded)"):
    finish()

found = {"anchors": {}, "refused": {}, "blocks": {}}
with step("safety-corpus", out_dir):
    found = show_corpus()
    check(found["swept"], "the chat scrolls from its top to its bottom (each item mounted at one stop)")
    check(corpus_drawn(found), f"the {len(SHOWN)} replies of script \"unsafe\" are drawn ({[w for w in SHOWN if w not in ' '.join(found['roots'])] or 'all'})")
    time.sleep(1.0)
    _, report = safety_heavy()
    light = report.get("safety") or {}
    check(light.get("inline_blocked") is True, f"the content rule blocked the probe's own inline script and handler ({light.get('inline_blocked')})")
    for field in SCANS:
        values = sorted({s.get(field) for s in found["scans"]}, key=str)
        check(bool(found["scans"]) and values == [0], f"{field} 0 at each of the {len(found['scans'])} stops ({values})")
    check(light.get("csp_violations") == 0, f"csp_violations 0 ({light.get('csp_violations')}; {found['violations']})")
    anchors = [href for (_, href, _), n in found["anchors"].items() for _ in range(n)]
    check(anchors == [PAGE], f"one link in the corpus, the fake page ({anchors})")
    refused = sorted(text for (_, text), n in found["refused"].items() for _ in range(n))
    check(refused == sorted(REFUSED_LINKS), f"every refused link spelling is text ({refused})")
    blocks = [text for (_, text) in found["blocks"]]
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


def on_screen(find, item, tries=80):
    """Scrolls the chat with the wheel until find(heavy, chat) gives a rect
    inside the visible zone; returns that rect, or None. While the timeline
    item that holds it is not mounted (the virtual list's window, 1.6), the
    wheel first moves toward that item."""
    top, bottom = visible_zone()
    for _ in range(tries):
        heavy, chat, _ = scan()
        rect = find(heavy, chat)
        if rect is None:
            frames = chat.get("items") or []
            keys = [f.get("key") for f in frames]
            mounted = [i for i, f in enumerate(frames) if f.get("rect")]
            if item not in keys or not mounted or keys.index(item) in mounted:
                return None
            wheel(chat, keys.index(item) > mounted[len(mounted) // 2], 10)
            time.sleep(0.3)
            continue
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


def located(field, match, item):
    """find for on_screen: the rect of the first element of the safety
    section's field that match() takes, inside the timeline item item."""
    return lambda heavy, chat: next((x["rect"] for x in heavy.get(field) or [] if match(x) and owner(x["rect"], chat) == item), None)


# The item holding the http link, and its finder.
page_item = next((item for (item, href, _) in found["anchors"] if href == PAGE), None)
page_link = located("anchors", lambda a: a.get("href") == PAGE, page_item)


def notice_now():
    return ((probe_section(probe_path, "overlay") or {}).get("notice") or {})


with step("safety-links", out_dir):
    _, report = safety_heavy()
    names = [("refused", text, item) for (item, text) in found["refused"]]
    names += [("raw", text, item) for (item, text) in found["blocks"] if text in RAW_LINKS]
    if not check(page_item is not None and len(names) == len(REFUSED_LINKS) + len(RAW_LINKS), f"the links to try are drawn ({len(names)} refused)"):
        raise RuntimeError("no links to try")
    before = len(opened_lines())
    asked = (report.get("safety") or {}).get("links_asked", 0)

    def find(kind, text, item):
        return located("refused" if kind == "refused" else "blocks", lambda x: x["text"] == text, item)

    tried = 0
    for kind, text, item in [("page", PAGE, page_item)] + names:
        rect = on_screen(page_link if kind == "page" else find(kind, text, item), item)
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
    rect = on_screen(page_link, page_item)
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
    rect = on_screen(page_link, page_item)
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


def inline_figures():
    """The diagrams drawn in the chat over a whole sweep (they sit in different
    replies, which the virtual list need not mount together), by figure id. At
    each stop it waits for the mounted ones to finish drawing."""
    seen, scans = {}, []

    def visit(heavy, chat):
        end = time.time() + 20
        while any(f.get("state") not in ("drawn", "fallback") for f in diagrams(heavy, True)) and time.time() < end:
            time.sleep(0.5)
            heavy, _, _ = scan()
        for f in diagrams(heavy, True):
            seen[f.get("figure")] = f
        scans.append({field: heavy.get(field) for field in SCANS})

    sweep(visit)
    return list(seen.values()), scans


def check_diagram(where):
    """The diagrams in the chat: every valid one draws as an SVG of its kind;
    only the hostile one may fall back."""
    titles = {title for title, _ in VALID}
    end = time.time() + 120
    while True:
        heavy, _ = safety_heavy()
        figures, scans = inline_figures()
        if (len(figures) > len(VALID) and all(f.get("state") in ("drawn", "fallback") for f in figures)) or time.time() > end:
            break
        time.sleep(0.5)
    bad = []
    for title, kind in VALID:
        found = [f for f in figures if f.get("title") == title]
        if len(found) != 1 or found[0].get("state") != "drawn" or found[0].get("svg") != kind:
            bad.append(f"{title}: {[(f.get('state'), f.get('svg')) for f in found] or 'missing'}")
    check(VALID and not bad, f"{where}: all {len(VALID)} valid diagrams draw as an SVG of their kind ({bad or 'all drawn'})")
    hostile = [f for f in figures if f.get("title") not in titles]
    states = [f.get("state") for f in hostile]
    check(len(hostile) == 1 and states[0] in ("drawn", "fallback"), f"{where}: the hostile Mermaid figure draws or shows its source with the fallback line ({states})")
    if "fallback" in states:
        note(f"{where}: the hostile diagram fell back to its source")
    light = probe_section(probe_path, "safety") or {}
    check(light.get("csp_violations") == 0, f"{where}: csp_violations still 0 with the figures drawn ({light.get('csp_violations')}; {heavy.get('violations')})")
    for field in SCANS:
        values = sorted({s.get(field) for s in scans}, key=str)
        check(values == [0], f"{where}: {field} 0 with the figures drawn ({values}; {len(scans)} stops)")
    check(hits == [], f"{where}: nothing reached the listener ({hits})")


def zoom_is(target):
    p = wait_for(probe_path, lambda p: p.get("zoom") is not None and abs(p["zoom"] - target) < 1e-6)
    return p.get("zoom") is not None and abs(p["zoom"] - target) < 1e-6


with step("safety-diagram", out_dir):
    check_diagram("in the chat")
    # Again in the narrowest window (900 px) at picture size (Ctrl = four times
    # from 8/12), where the right panel hides (SIDE-002).
    xdo("windowfocus", "--sync", wid)
    for _ in range(4):
        key("ctrl+equal")
        time.sleep(0.2)
    check(zoom_is(1.0), "picture size (12/12) after four Ctrl = presses")
    xdo("windowsize", wid, 900, start_h)
    check_diagram("in the narrow chat")
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
