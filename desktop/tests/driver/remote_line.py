"""Remote per chat in the desktop app (owner, 2026-10-21: "One remote per chat",
shown as a status line under the typing box with nothing after it), with the
real engine and the Tailscale stand-in (seam remote), real keys and clicks:

- Remote control from the palette turns the line ([data-remote-line]) on
  under the typing box, and the phone's share names the chat;
- the line belongs to the chat: a new chat takes it away and another chat
  shows none, the phone's share still names the first chat, and going back
  to that chat shows the line again;
- the terminal app showing that chat opens its Remote Control on the share
  the desktop turned on, and turning it off there takes the desktop's line
  away and stops the share; turned on again from the terminal, the
  desktop's line comes back;
- a click on the line opens the Remote control sheet for that chat; Turn off
  there takes it away and the share stops.

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
wait_for(probe_path, lambda p: round((p.get("shell") or {}).get("w", 0)) == 1600, 10)
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])
xdo("mousemove", geo_x + 5, geo_y + 980)

# The new project's init question holds the keys until it is answered.
nope = run("const b = [...document.querySelectorAll('button')].find((e) => e.textContent.trim() === 'Not now'); if (!b) return null; const r = b.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height };")
if nope:
    click_at(nope)
    until("return ![...document.querySelectorAll('button')].some((e) => e.textContent.trim() === 'Not now');", 5)


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
key("ctrl+p")
time.sleep(0.5)
xdo("type", "--delay", "15", "--", "Remote control")
time.sleep(0.4)
key("Return")
check(until("return !!document.querySelector('[data-remote-line]');", 10), "Remote control from the palette turns the line on")
sheet = run("const s = [...document.querySelectorAll('[role=dialog]')].find((d) => d.textContent.includes('Your phone can see this chat')); if (!s) return null; const td = [...s.querySelectorAll('td')].map((e) => e.textContent); return td;")
check(bool(sheet), f"the Remote control sheet opens on it ({sheet})")
port = None
if sheet:
    try:
        port = int(sheet[0].split("port")[-1].strip())
    except (ValueError, IndexError):
        pass
# A sheet that opens by itself takes no keys for a moment (lib/grace.js), so
# an Esc pressed as it appears is dropped: press until the sheet has gone.
closed = False
for _ in range(6):
    key("Escape")
    closed = until("return ![...document.querySelectorAll('[role=dialog]')].some((d) => d.textContent.includes('Your phone can see this chat'));", 0.5)
    if closed:
        break
check(closed, "Esc closes the Remote control sheet")
check(port is not None and share_names(port) == chat_a, f"the phone's share (port {port}) names the chat ({share_names(port) if port else None})")
shot("on")

# -- The line belongs to its chat --
key("ctrl+n")
gone = until("return !document.querySelector('[data-remote-line]');", 6)
check(gone, "a new chat takes the line away")
check(send("Seed turn short"), "a second chat starts")
chat_b = shown_chat()
until("return !document.querySelector('[data-remote-line]');", 2)
strip = run("const s = document.querySelector('[data-meter-strip]'); return s ? s.outerHTML.slice(0, 400) : null;")
check(bool(chat_b) and chat_b != chat_a and rect("[data-remote-line]") is None, f"chat {chat_b[:8]} shows no line ({strip})")
shot("second")
check(port is not None and share_names(port) == chat_a, f"the phone stays on the first chat ({share_names(port) if port else None})")
row = rect(f'nav [data-id="{chat_a}"] .row')
if check(row is not None, "the first chat is in the left list"):
    click_at(row)
    back = until("return !!document.querySelector('[data-remote-line]');", 8)
    check(back and shown_chat() == chat_a, "going back to it shows the line again")

# -- The terminal shows the same share, and either app turns it off --
term = Term(args=("-s", chat_a), env=SEAMS)
# A terminal that quits leaves its last screen (and any crash) to read.
subprocess.run(["tmux", "-L", "crushtest", "set-option", "-t", "tui", "remain-on-exit", "on"], capture_output=True)


def term_shot(name):
    with open(os.path.join(out_dir, f"term-{name}.txt"), "w") as f:
        f.write(term.text())


def wait_screen(timeout):
    """The terminal app's first screen, or "" when it never draws."""
    end = time.time() + timeout
    while time.time() < end:
        text = term.text()
        if text.strip():
            return text
        time.sleep(0.2)
    return ""


check(bool(wait_screen(30)), "the terminal app starts on that chat")
# Its status line names the share once it has read it; a wait, not a check
# (the dialog below is the check that it sees the desktop's share).
term.wait_text("REMOTE", 30)


def terminal_remote():
    term.keys("C-p")
    term.wait_text("Commands", 5)
    term.keys("-l", "Remote Control")
    term.wait_text("Remote Control", 5)
    term.keys("Enter")


terminal_remote()
dialog = term.wait_text("Turn off", 10)
term_shot("dialog")
check(dialog is not None, "the terminal's Remote Control opens on that chat's share")
term.keys("Left")
time.sleep(0.3)
term_shot("left")
term.keys("Enter")
time.sleep(0.3)
term_shot("off")
off = until("return !document.querySelector('[data-remote-line]');", 8)
check(off, "turned off in the terminal, the desktop's line goes away")
end = time.time() + 10
while port is not None and time.time() < end and not share_names(port).startswith("no share"):
    time.sleep(0.2)
term_shot("after")
dead = subprocess.run(["tmux", "-L", "crushtest", "display-message", "-p", "-t", "tui", "#{pane_dead} #{pane_dead_status}"], capture_output=True, text=True).stdout.strip()
alive = dead.startswith("0")
if not alive:
    subprocess.run(f"find /tmp/home -name '*.log' -mmin -10 -exec cp --backup=numbered {{}} {out_dir}/ \\;", shell=True)
    with open(os.path.join(out_dir, "term-dead.txt"), "w") as f:
        f.write(dead + "\n" + subprocess.run(["tmux", "-L", "crushtest", "capture-pane", "-p", "-S", "-200", "-t", "tui"], capture_output=True, text=True).stdout)
check(alive, f"the terminal keeps running (terminal running: {alive})")
check(port is not None and share_names(port).startswith("no share"), f"and the share stops ({share_names(port) if port else None})")

terminal_remote()
back = until("return !!document.querySelector('[data-remote-line]');", 15)
check(back, "turned on again in the terminal, the desktop's line comes back")
term.wait_text("Keep on", 10)
term.keys("Escape")

# -- Off from its sheet --
line = rect("[data-remote-line]")
if check(line is not None, "the line is there to click"):
    click_at(line)
    opened = until("return [...document.querySelectorAll('[role=dialog]')].some((d) => d.textContent.includes('Your phone can see this chat'));", 5)
    check(opened, "a click on the line opens the Remote control sheet")
    shot("sheet")
    key("t")
    off = until("return !document.querySelector('[data-remote-line]');", 6)
    check(off, "Turn off takes the line away")
    # The terminal's start may have taken another port.
    end = time.time() + 10
    while time.time() < end and not all(share_names(p).startswith("no share") for p in range(7373, 7377)):
        time.sleep(0.2)
    check(all(share_names(p).startswith("no share") for p in range(7373, 7377)), "and no share is left")
term.stop()
kill_terms()
app.stop()
finish()
