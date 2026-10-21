"""A huge chat opens fast (owner, 2026-10-21: an 18,000-message chat took
10 s to open). In the real window on the hidden screen, from a small chat, a
click on "Huge chat" (18,000 messages, about 50 MB) shows its newest question
within 1 s, since only the newest page is read; wheeling up to the top then
reads older pages, so older questions show (the scroll bar dragged to the top).

Usage: python3 tests/fixtures/make_big_chat.py && \\
       CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/out/big-chat.json big_chat.py
"""
import json, os, re, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
SMALL = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
BIG = "b16c4a75-0000-4000-8000-000000000001"
FIXTURE = os.path.join(os.path.dirname(__file__), "..", "out", "big-chat.json")


def user_lines():
    """Each user message's opening words (unique ones) and its place in the chat."""
    with open(FIXTURE) as f:
        msgs = json.load(f)["get"][f"sessions/{BIG}/messages"]
    seen, out = {}, []
    for i, m in enumerate(msgs):
        if m.get("role") != "user":
            continue
        words = next((p["data"].get("text", "") for p in m.get("parts", []) if p.get("type") == "text"), "")
        words = " ".join(words.split())[:28]
        if len(words) >= 12:
            seen[words] = seen.get(words, 0) + 1
            out.append((words, i))
    return [(w, i) for w, i in out if seen[w] == 1]


LINES = user_lines()
NEWEST = LINES[-1][0]


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def rect(selector):
    return run(f"const r = document.querySelector({selector!r})?.getBoundingClientRect(); return r && {{x: r.x, y: r.y, w: r.width, h: r.height}};")


def text():
    return " ".join((run("return document.querySelector('main.conversation')?.textContent ?? '';") or "").split())


def questions():
    """The places in the chat of the user messages on the page."""
    shown = text()
    return sorted(i for w, i in LINES if w in shown)


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
start = time.time()
while time.time() - start < 15 and "login" not in text().lower():
    time.sleep(0.1)
time.sleep(1)

row = rect(f'nav.chats .chat[data-id="{BIG}"] .row')
if not check(bool(row), "Huge chat is in the chat list"):
    finish()
H.click(row["x"] + row["w"] / 2, row["y"] + row["h"] / 2)
clicked = time.time()
took = None
while time.time() - clicked < 20:
    if NEWEST in text():
        took = time.time() - clicked
        break
    time.sleep(0.05)
check(took is not None and took < 1.0, f"Huge chat shows its newest question within 1 s ({took if took is None else round(took, 2)} s)")
H.shot(out_dir, "big-chat")

# Drag to the top, as with the scroll bar, a few times: each time an older
# page is read and goes ahead, so older questions show.
lowest_first = min(questions() or [99999])
lowest = lowest_first
for _ in range(12):
    run("const s = document.querySelector('main.conversation .scroller[role=feed]'); s.scrollTop = 0; s.dispatchEvent(new Event('scroll')); return true;")
    time.sleep(0.6)
    lowest = min([lowest] + questions())
    if lowest < lowest_first - 400:
        break
check(lowest < lowest_first - 400, f"scrolling up reads older pages (first shown {lowest_first}, reached {lowest})")
H.shot(out_dir, "older")
finish()
