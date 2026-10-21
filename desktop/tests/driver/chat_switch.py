"""Switching chats (owner, 2026-10-20): "keep a chat loaded when I switch"
and "if I type while the chat is loading, the text I type gets deleted". In
the real window on the hidden screen, with two chats of proj, the second
answering its messages 2 s late (fixture `slow`):
- words typed while the slow chat opens are in its typing box once it shows;
- back to the first chat and to the slow one again: it shows at once (well
  under its 2 s), its words still there.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/chat-switch.json chat_switch.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, type_text, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
A = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
B = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f04"
WORDS = "hello there"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


STATE = """
  const sel = document.querySelector('nav.chats .chat.selected')?.getAttribute('data-id') ?? null;
  return { sel, msgs: document.querySelectorAll('main.conversation .user').length,
           loading: !!document.querySelector('main.conversation .opening, main.conversation [data-loading]'),
           value: document.querySelector('textarea.input')?.value ?? null };
"""


def rect(selector):
    return run(f"const r = document.querySelector({selector!r})?.getBoundingClientRect(); return r && {{x: r.x, y: r.y, w: r.width, h: r.height}};")


def click(selector):
    r = rect(selector)
    if r:
        H.click(r["x"] + r["w"] / 2, r["y"] + r["h"] / 2)
    return bool(r)


def shown(cid, limit):
    """Seconds until chat cid shows with its messages, or None."""
    start = time.time()
    while time.time() - start < limit:
        s = run(STATE) or {}
        if s.get("sel") == cid and s.get("msgs", 0) > 0:
            return time.time() - start
        time.sleep(0.05)
    return None


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
check(shown(A, 10) is not None, "the first chat shows")

# Open the slow chat and type at once, while it loads.
click(f'nav.chats .chat[data-id="{B}"] .row')
time.sleep(0.2)
click("textarea.input")
type_text(WORDS)
took = shown(B, 8)
after = run(STATE) or {}
check(took is not None and after.get("value") == WORDS, f"words typed while it loads are in its box once it shows ({after.get('value')!r})")

# Back and forth: the slow chat shows at once from memory.
click(f'nav.chats .chat[data-id="{A}"] .row')
check(shown(A, 5) is not None, "back to the first chat")
click(f'nav.chats .chat[data-id="{B}"] .row')
again = shown(B, 5)
value = (run(STATE) or {}).get("value")
check(again is not None and again < 0.8, f"the slow chat shows again at once ({again and round(again, 2)} s; a fresh load takes 2 s)")
check(value == WORDS, f"with its words still in the box ({value!r})")
finish()
