"""Chats under their folders, the folder's "+", and Complete (owner,
2026-10-21). In the real window on the hidden screen, with the fixture's open
chats (proj's five, beta's and gamma's; the window shows proj's f01):
- the left list has a heading per folder in order (proj, beta, gamma), each
  chat under its own folder, a "+" on each heading and no "x" while the
  folder has chats;
- Complete sits on the right above the typing box; a real click takes the
  shown chat off the shared list (the window then opens the next proj chat,
  which the fixture has no messages for);
- a folder whose last chat closes stays, saying "No open chats"; pointing at
  its heading shows the "x", and a real click on it takes the folder off;
- a real click on beta's "+" switches the window to beta, on a new chat.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/folder-switch.json folder_groups.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, click, finish, open_chat_ids, probe_eval, read_probe, shot, wait_for, xdo, _screen

probe_path, out_dir = sys.argv[1], sys.argv[2]
SHOWN = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
BETA = "b0b0b0b0-1111-4222-8333-000000000001"
GAMMA = "c0c0c0c0-1111-4222-8333-000000000001"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def groups():
    return run("""
      const rect = (el) => { if (!el) return null; const r = el.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height }; };
      return [...document.querySelectorAll('nav.chats section.group')].map((g) => ({
        dir: g.dataset.folder, label: g.querySelector('.folder-name')?.textContent.trim(),
        ids: [...g.querySelectorAll('.chat')].map((c) => c.dataset.id),
        plus: rect(g.querySelector('.folder-new')), drop: rect(g.querySelector('.folder-drop')),
        dropShown: g.querySelector('.folder-drop') ? getComputedStyle(g.querySelector('.folder-drop')).opacity : null,
        empty: g.querySelector('.folder-empty')?.textContent.trim() ?? null, head: rect(g.querySelector('.folder')) }));
    """) or []


def find(name):
    return next((g for g in groups() if g["dir"].endswith("/" + name)), None)


def middle(r):
    return r["x"] + r["w"] / 2, r["y"] + r["h"] / 2


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
wait_for(probe_path, lambda p: len(((p.get("list") or {}).get("rows") or [])) >= 5, 20)
time.sleep(1)

# A heading per folder, each chat under its own.
seen = groups()
check([g["dir"].rsplit("/", 1)[-1] for g in seen] == ["proj", "beta", "gamma"], f"one heading per folder, in order ({[g['dir'] for g in seen]})")
check(len(seen) == 3 and len(seen[0]["ids"]) == 5 and seen[1]["ids"] == [BETA] and seen[2]["ids"] == [GAMMA],
      f"each chat sits under its own folder ({[(g['label'], g['ids']) for g in seen]})")
check(all(g["plus"] and g["drop"] is None for g in seen), "each heading has a + and no x while it has chats")
shot(out_dir, "groups")

# Complete: on the right above the typing box; a real click closes the chat.
place = run("""
  const r = (s) => { const e = document.querySelector(s); if (!e) return null; const b = e.getBoundingClientRect(); return { x: b.x, y: b.y, w: b.width, h: b.height }; };
  return { complete: r('[data-probe="composer"] .complete'), field: r('[data-probe="composer"] .field'), send: r('[data-probe="composer"] .send') };
""") or {}
c, field, send = place.get("complete"), place.get("field"), place.get("send")
check(bool(c and field and send) and abs((c["x"] + c["w"]) - (send["x"] + send["w"])) <= 1 and c["y"] + c["h"] <= field["y"],
      f"Complete sits on the right, above the typing box ({place})")
if c:
    click(*middle(c))
deadline = time.time() + 5
while SHOWN in open_chat_ids() and time.time() < deadline:
    time.sleep(0.2)
time.sleep(0.5)
listed = [r["id"] for r in ((read_probe(probe_path) or {}).get("list") or {}).get("rows") or []]
# The fixture has no messages for the next chat, so only the list is checked.
check(SHOWN not in open_chat_ids() and SHOWN not in listed and len(listed) == 6,
      f"Complete takes the shown chat off the list for every app ({listed})")

# Gamma's last chat closes: the folder stays, then its x takes it off.
run(f"document.querySelector('nav.chats .chat[data-id=\"{GAMMA}\"] .close').click(); return true;")
wait_for(probe_path, lambda p: not any(r["id"] == GAMMA for r in ((p.get("list") or {}).get("rows") or [])), 5)
time.sleep(0.5)
gamma = find("gamma") or {}
check(gamma.get("ids") == [] and gamma.get("empty") == "No open chats", f"gamma stays with no chats ({gamma})")
if gamma.get("head"):
    hx, hy = _screen(gamma["head"]["x"] + 30, gamma["head"]["y"] + gamma["head"]["h"] / 2)
    xdo("mousemove", hx, hy)
    time.sleep(0.4)
gamma = find("gamma") or {}
check(gamma.get("dropShown") == "1", f"pointing at the heading shows its x ({gamma.get('dropShown')})")
shot(out_dir, "empty-folder")
if gamma.get("drop"):
    click(*middle(gamma["drop"]))
time.sleep(0.5)
check(find("gamma") is None, f"the x takes the folder off ({[g['dir'] for g in groups()]})")

# Beta's +: the window switches to beta, on a new chat.
beta = find("beta") or {}
if beta.get("plus"):
    click(*middle(beta["plus"]))
ok = wait_for(probe_path, lambda p: (p.get("connection") or {}).get("workspace_id") == "ws-beta" and (p.get("connection") or {}).get("state") == "connected", 20)
time.sleep(1.5)
screen = ((read_probe(probe_path) or {}).get("start") or {}).get("screen")
check(bool(ok) and screen == "landing", f"beta's + switches to beta on a new chat (screen {screen})")
shot(out_dir, "beta-new")

finish()
