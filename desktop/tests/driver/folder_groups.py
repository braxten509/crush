"""Chats under their folders, the folder's "+", and Complete (owner,
2026-10-21). In the real window on the hidden screen, with the fixture's open
chats (proj's five, beta's and gamma's; the window shows proj's f01):
- the left list has a heading per folder in order (proj, beta, gamma), each
  chat under its own folder, a "+" on each heading and no "x" while the
  folder has chats;
- a real click on Complete takes the shown chat off the shared list (the
  window then opens the next proj chat, which the fixture has no messages
  for);
- a folder whose last chat closes stays, with no chats; its "x" takes the
  folder off.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/folder-switch.json folder_groups.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, click, finish, open_chat_ids, probe_eval, read_probe, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
SHOWN = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
BETA = "b0b0b0b0-1111-4222-8333-000000000001"
GAMMA = "c0c0c0c0-1111-4222-8333-000000000001"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def groups():
    """The real folders' groups; No project (data-folder "", always drawn last
    since 2026-11-09) is left out."""
    return run("""
      const rect = (el) => { if (!el) return null; const r = el.getBoundingClientRect(); return { x: r.x, y: r.y, w: r.width, h: r.height }; };
      return [...document.querySelectorAll('nav.chats section.group')].filter((g) => g.dataset.folder).map((g) => ({
        dir: g.dataset.folder, label: g.querySelector('.folder-name')?.textContent.trim(),
        ids: [...g.querySelectorAll('.chat')].map((c) => c.dataset.id),
        plus: rect(g.querySelector('.folder-new')), drop: rect(g.querySelector('.folder-drop')),
        head: rect(g.querySelector('.folder')) }));
    """) or []


def find(name):
    return next((g for g in groups() if g["dir"].endswith("/" + name)), None)


def until(read, ok, timeout=5):
    end = time.time() + timeout
    value = read()
    while time.time() < end and not ok(value):
        time.sleep(0.1)
        value = read()
    return value


def middle(r):
    return r["x"] + r["w"] / 2, r["y"] + r["h"] / 2


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
wait_for(probe_path, lambda p: len(((p.get("list") or {}).get("rows") or [])) >= 5, 20)

# A heading per folder, each chat under its own.
seen = until(groups, lambda g: len(g) == 3 and len(g[0]["ids"]) == 5, 10)
check([g["dir"].rsplit("/", 1)[-1] for g in seen] == ["proj", "beta", "gamma"], f"one heading per folder, in order ({[g['dir'] for g in seen]})")
check(len(seen) == 3 and len(seen[0]["ids"]) == 5 and seen[1]["ids"] == [BETA] and seen[2]["ids"] == [GAMMA],
      f"each chat sits under its own folder ({[(g['label'], g['ids']) for g in seen]})")
check(all(g["plus"] and g["drop"] is None for g in seen), "each heading has a + and no x while it has chats")

shot(out_dir, "groups")

# Complete: a real click closes the chat.
c = run("""
  const e = document.querySelector('[data-probe="composer"] .complete'); if (!e) return null;
  const b = e.getBoundingClientRect(); return { x: b.x, y: b.y, w: b.width, h: b.height };
""")
check(bool(c), "Complete shows")
if c:
    click(*middle(c))
until(open_chat_ids, lambda ids: SHOWN not in ids)
listed = until(lambda: [r["id"] for r in ((read_probe(probe_path) or {}).get("list") or {}).get("rows") or []],
               lambda ids: SHOWN not in ids and len(ids) == 6)
# The fixture has no messages for the next chat, so only the list is checked.
check(SHOWN not in open_chat_ids() and SHOWN not in listed and len(listed) == 6,
      f"Complete takes the shown chat off the list for every app ({listed})")

# Gamma's last chat closes: the folder stays, then its x takes it off.
run(f"document.querySelector('nav.chats .chat[data-id=\"{GAMMA}\"] .close').click(); return true;")
gamma = until(lambda: find("gamma") or {}, lambda g: g.get("ids") == [])
check(gamma.get("ids") == [], f"gamma stays with no chats ({gamma})")
shot(out_dir, "empty-folder")
run(f"document.querySelector('nav.chats [data-folder-drop=\"{gamma.get('dir')}\"]')?.click(); return true;")
until(lambda: find("gamma"), lambda g: g is None)
check(find("gamma") is None, f"the x takes the folder off ({[g['dir'] for g in groups()]})")

finish()
