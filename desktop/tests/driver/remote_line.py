"""Remote per chat in the desktop app (owner, 2026-10-21: "One remote per chat",
shown as a status line under the typing box with nothing after it), with the
real engine and the Tailscale stand-in (seam remote), real keys and clicks:

- Remote control from the palette slides "● REMOTE" in under the typing box,
  the space under the field gliding with it; nothing inside the typing box
  says REMOTE any more;
- its dot is whole pixels, vermilion, and centred with the word's capitals
  (read from the capture's ink); the word is in the strip's label type;
- the line belongs to the chat: a new chat slides it away and another chat
  shows none, the phone's share still names the first chat, and going back
  to that chat slides the line in again;
- the terminal app showing that chat shows REMOTE though the desktop turned
  it on, and turning it off there slides the desktop's line away; turned on
  again from the terminal, the desktop's line slides back in;
- a click on the line opens the Remote control sheet for that chat; Turn off
  there slides it away, the terminal's REMOTE goes and the share stops.

Usage: tests/check.sh --no-app remote_line.py
"""
import json, os, subprocess, sys, time, urllib.request

sys.path.insert(0, os.path.dirname(__file__))
from apps import App, Term, kill_terms, seams
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]


# Synchronous, and wrapped: the probe reads a null answer as "the script did
# not report", so "nothing there" comes back as {value: null}, i.e. None.
def run(script):
    got = probe_eval(probe_path, "window.__probeEval = (() => { try { return { value: (() => {" + script
                     + "})() ?? null }; } catch (e) { return { error: String(e && e.stack || e) }; } })();")
    if isinstance(got, dict) and "value" in got:
        return got["value"]
    return got


def until(script, timeout=10, step=0.2):
    end = time.time() + timeout
    while time.time() < end:
        if run(script) is True:
            return True
        time.sleep(step)
    return False


def rect(selector):
    return run(f"const e = document.querySelector({json.dumps(selector)}); if (!e) return null; const r = e.getBoundingClientRect(); return {{ x: r.x, y: r.y, w: r.width, h: r.height }};")


def click_at(r):
    xdo("mousemove", geo_x + round(r["x"] + r["w"] / 2), geo_y + round(r["y"] + r["h"] / 2))
    xdo("click", 1)


def key(*combos):
    for combo in combos:
        xdo("key", "--clearmodifiers", combo)


def shot(name, wait=0.4):
    time.sleep(wait)
    path = os.path.join(out_dir, name + ".png")
    subprocess.run(["import", "-window", wid, path])
    return path


def shown_chat():
    return (wait_for(probe_path, lambda p: True, 2).get("chat") or {}).get("id") or ""


# The strip's own room under the typing box, frame by frame, from now until it
# has changed and then held still for 12 frames (or 600 frames).
RECORD = """
  window.__room = [];
  const room = () => { const a = document.querySelector('[data-meter-strip]')?.parentElement; return a ? Math.round(a.getBoundingClientRect().height * 100) / 100 : 0; };
  let still = 0, frames = 0;
  const tick = () => {
    const h = room(); const last = window.__room[window.__room.length - 1];
    window.__room.push(h); still = h === last ? still + 1 : 0; frames += 1;
    if ((still < 12 || window.__room.every((v) => v === window.__room[0])) && frames < 600) requestAnimationFrame(tick);
  };
  requestAnimationFrame(tick);
  return true;
"""


def recorded(timeout=5):
    end = time.time() + timeout
    got = []
    while time.time() < end:
        got = run("return window.__room ?? [];") or []
        if len(got) >= 12 and len(set(got[-12:])) == 1 and len(set(got)) > 1:
            break
        time.sleep(0.2)
    return got


def glides(values, start, stop):
    """True when the room runs from start to stop through 3 or more values
    between, never turning back."""
    seen = [v for i, v in enumerate(values) if i == 0 or v != values[i - 1]]
    if not seen or abs(seen[0] - start) > 1 or abs(seen[-1] - stop) > 0.5:
        return False
    steps = [b - a for a, b in zip(seen, seen[1:])]
    between = [v for v in seen if min(start, stop) + 0.5 < v < max(start, stop) - 0.5]
    return len(between) >= 3 and (all(s >= 0 for s in steps) or all(s <= 0 for s in steps))


def share_names(port):
    try:
        with urllib.request.urlopen(f"http://127.0.0.1:{port}/v1/share", timeout=3) as r:
            return json.load(r).get("session")
    except Exception as e:  # a stopped share refuses the connection
        return f"no share ({type(e).__name__})"


SEAMS = seams({"remote": {"listen": "127.0.0.1", "host": "crushtest-pc", "user": "tester@example.com", "phones": ["Pixel"]}})
app = App(env=SEAMS)
wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
found = ""
for _ in range(60):
    r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
    if r.stdout.split():
        found = r.stdout.split()[-1]
        break
    time.sleep(0.3)
wid = found
if not check(bool(wid), "the app window is on the hidden screen"):
    finish()
xdo("windowmove", wid, 0, 0)
xdo("windowsize", wid, 1600, 992)
time.sleep(1.5)
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])
xdo("mousemove", geo_x + 5, geo_y + 980)
PX = run("const e = document.createElement('div'); e.style.width = 'calc(100 * var(--px))'; document.body.append(e); const w = e.getBoundingClientRect().width / 100; e.remove(); return w;") or 1

# The new project's init question holds the keys until it is answered.
nope = run("const b = [...document.querySelectorAll('button')].find((e) => e.textContent.trim() === 'Not now'); if (!b) return null; const r = b.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height };")
if nope:
    click_at(nope)
    time.sleep(0.6)


def send(text):
    click_at(rect(".composer textarea"))
    time.sleep(0.2)
    xdo("type", "--delay", "15", "--", text)
    key("Return")
    return until("return [...document.querySelectorAll('.slot:not(.parked) .timeline [data-kind=footer]')].length > 0 && !document.querySelector('.slot:not(.parked) .timeline .status');", 20)


check(send("Seed turn short"), "a first chat starts and its reply finishes")
chat_a = shown_chat()
strip = run("const s = document.querySelector('[data-meter-strip]'); return s ? s.outerHTML.slice(0, 400) : null;")
check(bool(chat_a) and strip is None, f"chat {chat_a[:8]} shows, with no strip under the typing box (the panel shows the meters) ({strip})")

# -- On --
run(RECORD)
key("ctrl+p")
time.sleep(0.5)
xdo("type", "--delay", "15", "--", "Remote control")
time.sleep(0.4)
key("Return")
check(until("return !!document.querySelector('[data-remote-line]');", 10), "Remote control from the palette turns the line on")
room = recorded()
check(glides(room, 0, 18 * PX), f"the line slides in, the space under the field gliding with it ({room[:3]}…{room[-3:]}, {len(set(room))} sizes)")
sheet = run("const s = [...document.querySelectorAll('[role=dialog]')].find((d) => d.textContent.includes('Your phone can see this chat')); if (!s) return null; const td = [...s.querySelectorAll('td')].map((e) => e.textContent); return td;")
check(bool(sheet), f"the Remote control sheet opens on it ({sheet})")
port = None
if sheet:
    try:
        port = int(sheet[0].split("port")[-1].strip())
    except (ValueError, IndexError):
        pass
key("Escape")
time.sleep(0.5)
check(port is not None and share_names(port) == chat_a, f"the phone's share (port {port}) names the chat ({share_names(port) if port else None})")
inside = run("const f = document.querySelector('.composer .field'); return f ? { badge: !!f.querySelector('[data-badge]'), text: f.innerText } : null;")
check(inside is not None and not inside["badge"] and "REMOTE" not in inside["text"].upper().replace("REMOTE CONTROL", ""), f"nothing inside the typing box says REMOTE ({inside})")

look = run("""
  const line = document.querySelector('[data-remote-line]');
  const dot = line.querySelector('.dot').getBoundingClientRect(), word = line.querySelector('.label');
  const s = getComputedStyle(word), d = getComputedStyle(line.querySelector('.dot'));
  const field = document.querySelector('.composer .field').getBoundingClientRect();
  return { dot: { x: dot.x, y: dot.y, w: dot.width, h: dot.height }, word: (() => { const r = word.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height }; })(),
           transform: s.textTransform, color: d.backgroundColor, field_left: field.left, after: line.nextElementSibling?.textContent ?? '' };
""")
if check(look is not None, "the line has its dot and word"):
    whole = all(abs(v - round(v)) < 0.01 for v in (look["dot"]["w"], look["dot"]["h"]))
    check(whole and look["dot"]["w"] == look["dot"]["h"], f"the dot is a whole-pixel circle ({look['dot']})")
    check(look["transform"] == "uppercase", f"the word is in the strip's label type ({look['transform']})")
    check(look["after"] == "", f"nothing follows the word ({look['after']!r})")
    check(abs(look["dot"]["x"] - look["field_left"]) <= 1, f"it starts at the typing box's left edge ({look['dot']['x']} vs {look['field_left']})")
    path = shot("on")
    from PIL import Image
    image = Image.open(path).convert("L")
    bg = image.getpixel((round(look["word"]["x"] + look["word"]["w"] + 30), round(look["word"]["y"] + look["word"]["h"] / 2)))

    def ink_rows(box):
        xs = range(round(box["x"]), round(box["x"] + box["w"]))
        ys = range(round(box["y"]) - 2, round(box["y"] + box["h"]) + 2)
        return [y for y in ys if any(abs(image.getpixel((x, y)) - bg) > 40 for x in xs)]

    dot_rows, word_rows = ink_rows(look["dot"]), ink_rows(look["word"])
    if check(bool(dot_rows) and bool(word_rows), "the dot and the word are drawn"):
        dot_mid = (dot_rows[0] + dot_rows[-1]) / 2
        word_mid = (word_rows[0] + word_rows[-1]) / 2
        check(abs(dot_mid - word_mid) <= 1, f"the dot sits centred on the capitals (dot rows {dot_rows[0]}-{dot_rows[-1]}, word {word_rows[0]}-{word_rows[-1]})")

# -- The line belongs to its chat --
run(RECORD)
key("ctrl+n")
gone = until("return !document.querySelector('[data-meter-strip]');", 6)
room = recorded()
check(gone and glides(room, 18 * PX, 0), f"a new chat slides the line away ({room[:3]}…{room[-3:]})")
check(send("Seed turn short"), "a second chat starts")
chat_b = shown_chat()
time.sleep(0.6)
strip = run("const s = document.querySelector('[data-meter-strip]'); return s ? s.outerHTML.slice(0, 400) : null;")
check(bool(chat_b) and chat_b != chat_a and rect("[data-remote-line]") is None, f"chat {chat_b[:8]} shows no line ({strip})")
shot("second")
check(port is not None and share_names(port) == chat_a, f"the phone stays on the first chat ({share_names(port) if port else None})")
row = rect(f'nav [data-id="{chat_a}"] .row')
if check(row is not None, "the first chat is in the left list"):
    run(RECORD)
    click_at(row)
    back = until("return !!document.querySelector('[data-remote-line]');", 8)
    room = recorded()
    check(back and shown_chat() == chat_a and glides(room, 0, 18 * PX), f"going back to it slides the line in again ({room[:3]}…{room[-3:]})")

# -- The terminal shows the same share, and either app turns it off --
term = Term(args=("-s", chat_a), env=SEAMS)
# A terminal that quits leaves its last screen (and any crash) to read.
subprocess.run(["tmux", "-L", "crushtest", "set-option", "-t", "tui", "remain-on-exit", "on"], capture_output=True)
terminal_on = term.wait_text("REMOTE", 30)
check(terminal_on is not None, "the terminal showing that chat shows REMOTE, though the desktop turned it on")


def term_shot(name):
    with open(os.path.join(out_dir, f"term-{name}.txt"), "w") as f:
        f.write(term.text())


def terminal_remote():
    term.keys("C-p")
    time.sleep(0.8)
    term.keys("-l", "Remote Control")
    time.sleep(0.6)
    term.keys("Enter")


terminal_remote()
dialog = term.wait_text("Turn off", 10)
term_shot("dialog")
check(dialog is not None, "the terminal's Remote Control opens on that chat's share")
run(RECORD)
term.keys("Left")
time.sleep(0.3)
term_shot("left")
term.keys("Enter")
time.sleep(0.3)
term_shot("off")
off = until("return !document.querySelector('[data-meter-strip]');", 8)
room = recorded()
check(off and glides(room, 18 * PX, 0), f"turned off in the terminal, the desktop's line slides away ({room[:3]}…{room[-3:]})")
time.sleep(1)
term_shot("after")
dead = subprocess.run(["tmux", "-L", "crushtest", "display-message", "-p", "-t", "tui", "#{pane_dead} #{pane_dead_status}"], capture_output=True, text=True).stdout.strip()
alive = dead.startswith("0")
if not alive:
    subprocess.run(f"find /tmp/home -name '*.log' -mmin -10 -exec cp --backup=numbered {{}} {out_dir}/ \\;", shell=True)
    with open(os.path.join(out_dir, "term-dead.txt"), "w") as f:
        f.write(dead + "\n" + subprocess.run(["tmux", "-L", "crushtest", "capture-pane", "-p", "-S", "-200", "-t", "tui"], capture_output=True, text=True).stdout)
check(alive and "REMOTE" not in term.text(), f"and the terminal's REMOTE goes (terminal running: {alive})")
check(port is not None and share_names(port).startswith("no share"), f"and the share stops ({share_names(port) if port else None})")

run(RECORD)
terminal_remote()
back = until("return !!document.querySelector('[data-remote-line]');", 15)
room = recorded()
check(back and glides(room, 0, 18 * PX), f"turned on again in the terminal, the desktop's line slides in ({room[:3]}…{room[-3:]})")
term.wait_text("Keep on", 10)
term.keys("Escape")

# -- Off from its sheet --
line = rect("[data-remote-line]")
if check(line is not None, "the line is there to click"):
    click_at(line)
    opened = until("return [...document.querySelectorAll('[role=dialog]')].some((d) => d.textContent.includes('Your phone can see this chat'));", 5)
    check(opened, "a click on the line opens the Remote control sheet")
    shot("sheet")
    run(RECORD)
    key("t")
    off = until("return !document.querySelector('[data-meter-strip]');", 6)
    room = recorded()
    check(off and glides(room, 18 * PX, 0), f"Turn off slides the line away ({room[:3]}…{room[-3:]})")
    time.sleep(1)
    check("REMOTE" not in term.text(), "the terminal's REMOTE goes too")
    # The terminal's start may have taken another port.
    check(all(share_names(p).startswith("no share") for p in range(7373, 7377)), "and no share is left")
term.stop()
kill_terms()
app.stop()
finish()
