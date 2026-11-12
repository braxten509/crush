"""Locked chats with the password typed in Crush (owner, 2026-11-03), with the
real engine and the vault stand-in (seam vault: a plain folder, closed at
start, that opens only with its password), real keys and clicks:

- the lock button opens the lock box with a password field (the vault is
  closed); a wrong password shows a field error and the box stays; the
  right one closes the box and locks the chat;
- the locked page shows a password field; a wrong password there shows a
  field error; the right one unlocks the chat and its messages come back;
- locking again while the vault is open asks with no password field.

Usage: tests/check.sh --no-app lock_password.py
"""
import json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from apps import App, seams
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
PASSWORD = "test-pass-42"


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


def type_in(text):
    xdo("type", "--delay", "15", "--", text)


def shot(name, wait=0.4):
    time.sleep(wait)
    subprocess.run(["import", "-window", wid, os.path.join(out_dir, name + ".png")])


SEAMS = seams({"vault": {"dir": "/tmp/home/vault", "password": PASSWORD}})
app = App(env=SEAMS)
wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
wid = ""
for _ in range(60):
    r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
    if r.stdout.split():
        wid = r.stdout.split()[-1]
        break
    time.sleep(0.3)
if not check(bool(wid), "the app window is on the hidden screen"):
    finish()
g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
geo = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
geo_x, geo_y = int(geo["X"]), int(geo["Y"])
nope = run("const b = [...document.querySelectorAll('button')].find((e) => e.textContent.trim() === 'Not now'); if (!b) return null; const r = b.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height };")
if nope:
    click_at(nope)
    time.sleep(0.6)

click_at(rect(".composer textarea"))
time.sleep(0.2)
type_in("Seed turn short")
key("Return")
check(until("return [...document.querySelectorAll('.slot:not(.parked) .timeline [data-kind=footer]')].length > 0 && !document.querySelector('.slot:not(.parked) .timeline .status');", 20),
      "a chat starts and its reply finishes")

# -- The lock box: closed vault, wrong then right password --
click_at(rect("[data-lock-chat]"))
check(until("return document.querySelector('[data-lock-box]')?.dataset.lockBox === 'password';"), "the lock button asks with a password field")
check(until("return document.activeElement?.matches?.('[data-lock-box] input');", 5), "the password field has the focus")
shot("1-lock-box")
type_in("nope")
key("Return")
check(until("return !!document.querySelector('[data-lock-box] [data-field-error]');"), "a wrong password shows a field error and the box stays")
shot("2-lock-wrong")
click_at(rect("[data-lock-box] input"))
type_in(PASSWORD)
key("Return")
check(until("return !document.querySelector('[data-lock-box]');", 10), "the right password closes the box")
check(until("return document.querySelector('[data-locked-page]')?.dataset.lockedPage === 'locked';", 20), "the chat is locked")

# -- The locked page: password field --
check(until("return !!document.querySelector('[data-vault-password]');", 10), "the locked page shows a password field (the vault closed again)")
shot("3-locked")
click_at(rect("[data-vault-password] input"))
type_in("nope")
key("Return")
check(until("return !!document.querySelector('[data-vault-password] [data-field-error]');"), "a wrong password on the locked page shows a field error")
check(run("return document.querySelector('[data-locked-page]')?.dataset.lockedPage;") == "locked", "the chat stays locked")
shot("5-unlock-wrong")
click_at(rect("[data-vault-password] input"))
type_in(PASSWORD)
key("Return")
check(until("return !document.querySelector('[data-locked-page]');", 20), "the right password unlocks the chat")
check(until("return [...document.querySelectorAll('.slot:not(.parked) .timeline [data-kind=footer]')].length > 0;", 10), "its messages come back")
shot("6-unlocked")

# -- Lock again with the vault open: no password asked --
click_at(rect("[data-lock-chat]"))
check(until("return document.querySelector('[data-lock-box]')?.dataset.lockBox === 'plain';"), "locking again with the vault open asks with no password field")
shot("7-lock-again")
key("Escape")
check(until("return !document.querySelector('[data-lock-box]');", 5), "Esc cancels it")
check(not run("return !!document.querySelector('[data-locked-page]');"), "and the chat stays unlocked")

app.stop()
finish()
