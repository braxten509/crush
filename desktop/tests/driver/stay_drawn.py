"""Every chat on the left stays drawn (owner, 2026-10-22: "all chats should
stay open with all drawing and functionality, even when they are not the
currently selected one"). In the real window on the hidden screen, with
chat-switch.json's chats (tests/fixtures/stay-drawn.json):
- drawn-1-all    every listed chat gets its own drawn timeline soon after
                 the start, beta's (another folder) too, through the
                 other-folder read, before any is picked;
- drawn-2-spot   the first chat, scrolled up, then left for another chat and
                 picked again, shows at once at the same scroll spot with the
                 very same drawn rows (not new ones);
- drawn-3-live   a reply that reaches a never-picked chat is already in its
                 hidden timeline, and the chat shows at once when picked.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/stay-drawn.json stay_drawn.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
import harness as H
from harness import check, finish, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
A = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
B = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f04"
NEVER = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f05"
BETA = "b0b0b0b0-1111-4222-8333-000000000001"
FRESH = "Fresh words while hidden."


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def poll(fn, ok, limit, step=0.05):
    start, got = time.time(), None
    while time.time() - start < limit:
        got = fn()
        if ok(got):
            return True, got, time.time() - start
        time.sleep(step)
    return False, got, time.time() - start


def click(selector):
    if not os.environ.get("DISPLAY"):
        return bool(run(f"const el = document.querySelector({selector!r}); el?.click(); return !!el;"))
    r = run(f"const r = document.querySelector({selector!r})?.getBoundingClientRect(); return r && {{x: r.x, y: r.y, w: r.width, h: r.height}};")
    if r:
        H.click(r["x"] + r["w"] / 2, r["y"] + r["h"] / 2)
    return bool(r)


def slots():
    return run("return [...document.querySelectorAll('[data-timeline-slot]')].map((el) => ({ id: el.dataset.timelineSlot, "
               "seen: !el.classList.contains('parked'), rows: el.querySelectorAll('[data-key]').length, text: el.textContent }));") or []


def seen(cid):
    return next((s for s in slots() if s["id"] == cid and s["seen"] and s["rows"] > 0), None)


SCROLLER = f"document.querySelector('[data-timeline-slot=\"{A}\"] .scroller')"

wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
check(poll(lambda: seen(A), bool, 10)[0], "the first chat shows")

print("STEP drawn-1-all", flush=True)
ok, got, _ = poll(slots, lambda s: {A, B, NEVER, BETA} <= {x["id"] for x in s if x["rows"] > 0}, 10)
check(ok, f"every listed chat is drawn before any is picked ({sorted((x['id'][-4:], x['rows']) for x in got or [])})")
check(sum(1 for x in got or [] if x["seen"]) == 1, "only the shown chat's timeline is seen")
H.shot(out_dir, "drawn-1-all") if os.environ.get("DISPLAY") else None

print("STEP drawn-2-spot", flush=True)
before = run(f"const s = {SCROLLER}; s.scrollTop = Math.round(s.scrollHeight / 3); await new Promise((r) => setTimeout(r, 400));"
             f" const view = s.getBoundingClientRect(); const row = [...s.querySelectorAll('[data-key]')].find((el) => {{ const r = el.getBoundingClientRect(); return r.top >= view.top && r.bottom <= view.bottom; }});"
             " const key = row?.dataset.key ?? null; const el = key && s.querySelector(`[data-key=\"${key}\"]`); if (el) el.__drawnMark = 'kept';"
             " return { top: s.scrollTop, height: s.scrollHeight, key };") or {}
check(bool(before.get("key")) and before.get("top", 0) > 0, f"the first chat is scrolled up to a row ({before})")
click(f'nav.chats .chat[data-id="{B}"] .row')
check(poll(lambda: seen(B), bool, 5)[0], "another chat shows")
click(f'nav.chats .chat[data-id="{A}"] .row')
ok, _, took = poll(lambda: seen(A), bool, 5, 0.01)
check(ok and took < 0.5, f"the first chat shows again at once ({round(took, 2)} s)")
after = run(f"const s = {SCROLLER}; const row = s.querySelector('[data-key=\"{before.get('key')}\"]');"
            " return { top: s.scrollTop, same: row?.__drawnMark === 'kept' };") or {}
check(abs(after.get("top", -999) - before.get("top", 0)) <= 2, f"at the same scroll spot ({before.get('top')} then {after.get('top')})")
check(after.get("same") is True, "with the very same drawn rows, not new ones")
H.shot(out_dir, "drawn-2-spot") if os.environ.get("DISPLAY") else None

print("STEP drawn-3-live", flush=True)
ok, _, _ = poll(lambda: next((x for x in slots() if x["id"] == NEVER), {}), lambda s: FRESH in (s or {}).get("text", ""), 12, 0.2)
check(ok, "a reply that reaches a never-picked chat is already in its hidden timeline")
click(f'nav.chats .chat[data-id="{NEVER}"] .row')
# (Shown, the chat is read again: the recorded answers lack the timed reply,
# so only the at-once showing is checked here.)
ok, got, took = poll(lambda: seen(NEVER), bool, 5, 0.01)
check(ok and took < 0.5, f"and it shows at once when picked ({round(took, 2)} s)")
H.shot(out_dir, "drawn-3-live") if os.environ.get("DISPLAY") else None
finish()
