"""The open chats shared with the terminal, and the folder per chat (owner,
2026-10-20). In the real window on the hidden screen, with the fixture's open
chats (proj's, beta's and gamma's; the window is on proj):
- the left list is one flat list of the open chats, each with its folder as
  its second line, no headings and no "All chats" line; the open chat's red
  bar sits flush with the column's edge;
- a chat the terminal lists (written to the shared file from outside, as the
  terminal does) shows within a few seconds; the list's x takes a chat off
  the file for every app;
- picking beta's chat switches the window to beta in place: no reload, the
  top bar and the connection follow, a terminal tab stays;
- the search (the Sessions sheet) lists only the shown folder's chats;
- New chat and New terminal ask where to start (the folder picker), on the
  shown folder.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/folder-switch.json folder_switch.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, list_open_chat, open_chat_ids, probe_eval, read_probe, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
BETA = "b0b0b0b0-1111-4222-8333-000000000001"
GAMMA = "c0c0c0c0-1111-4222-8333-000000000001"
DELTA = "d0d0d0d0-1111-4222-8333-000000000001"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def rows():
    return ((read_probe(probe_path) or {}).get("list") or {}).get("rows") or []


def workspace():
    return ((read_probe(probe_path) or {}).get("connection") or {}).get("workspace_id")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
wait_for(probe_path, lambda p: len(((p.get("list") or {}).get("rows") or [])) >= 5, 20)
time.sleep(1)

# One flat list of the open chats, each with its folder.
seen = [(r["title"], r["folder"]) for r in rows() if r["depth"] == 0]
check([f for _, f in seen][-2:] == ["beta", "gamma"] and all(f == "proj" for _, f in seen[:-2]) and len(seen) >= 5,
      f"the list is the open chats, each with its folder as its second line ({seen})")
look = run("""
  const nav = document.querySelector('nav.chats');
  const band = nav.querySelector('.chat.selected .row');
  return { headings: nav.querySelectorAll('h2.day').length, all: !!nav.querySelector('.all-chats'),
           bar: band ? getComputedStyle(band, '::after').left : null };
""") or {}
check(look.get("headings") == 0 and not look.get("all"), f"no folder headings and no All chats line ({look})")
check(look.get("bar") == "0px", f"the open chat's red bar sits flush with the column's edge ({look.get('bar')})")
shot(out_dir, "list")

# The terminal opens a chat: it shows here.
list_open_chat({"id": DELTA, "dir": "/tmp/home/delta", "title": "Delta chat"}, as_app=True)
ok = wait_for(probe_path, lambda p: any(r["id"] == DELTA for r in ((p.get("list") or {}).get("rows") or [])), 6)
row = next((r for r in rows() if r["id"] == DELTA), {})
check(bool(ok) and row.get("title") == "Delta chat" and row.get("folder") == "delta", f"a chat the terminal opens shows in the list ({row})")

# The list's x closes a chat for every app.
run(f"document.querySelector('nav.chats .chat[data-id=\"{GAMMA}\"] .close').click(); return true;")
deadline = time.time() + 4
while GAMMA in open_chat_ids() and time.time() < deadline:
    time.sleep(0.2)
check(GAMMA not in open_chat_ids() and not any(r["id"] == GAMMA for r in rows()), f"its x takes the chat off the shared list ({open_chat_ids()})")

# A terminal tab in proj (Alt+T asks where; Enter: here), and a mark a reload
# would wipe.
TERMINALS = "return document.querySelectorAll('nav.chats li.terminal').length;"
run("window.__noReload = true; document.querySelector('main.conversation textarea')?.focus(); return true;")
key("alt+t")
wait_for(probe_path, lambda p: "folder_picker" in ((p.get("overlay") or {}).get("dialogs") or []), 5)
time.sleep(0.3)
key("Return")
time.sleep(1.5)
before = run(TERMINALS) or 0
check(before >= 1, f"a terminal tab opens ({before})")

# Beta's chat: the window switches to beta in place and shows it.
run(f"document.querySelector('nav.chats .chat[data-id=\"{BETA}\"] .row').click(); return true;")
wait_for(probe_path, lambda p: (p.get("connection") or {}).get("workspace_id") == "ws-beta" and (p.get("connection") or {}).get("state") == "connected", 20)
time.sleep(1.5)
after = run("""
  return { kept: window.__noReload === true, project: document.querySelector('[data-probe="title-project"]')?.textContent.trim(),
           shown: document.querySelector('nav.chats .chat.selected')?.getAttribute('data-id') ?? null,
           terminals: document.querySelectorAll('nav.chats li.terminal').length };
""") or {}
check(workspace() == "ws-beta" and after.get("kept"), f"picking beta's chat switches the window to beta with no reload ({workspace()}, {after})")
check(after.get("project") == "beta" and after.get("shown") == BETA, f"the top bar shows beta and beta's chat is open ({after})")
check(after.get("terminals") == before, f"the terminal rows stay ({before} before, {after.get('terminals')} after)")
order = [r["id"] for r in rows() if r["depth"] == 0]
check(BETA in order and DELTA in order and order.index(BETA) < order.index(DELTA), f"the list keeps its order across the switch ({order})")
shot(out_dir, "list-beta")

# The search lists only beta's chats.
run("document.querySelector('nav.chats button.search').click(); return true;")
time.sleep(1.2)
titles = run("return [...document.querySelectorAll('[data-sheet=\"sessions\"] [data-index]')].map((el) => el.textContent.trim());") or []
check(len(titles) == 1 and "Beta chat" in titles[0], f"the search shows only the shown folder's chats ({titles})")
key("Escape")
time.sleep(0.6)

# New chat and New terminal ask where to start, on the shown folder.
for button, title in [("nav.chats .new-row > button.new-chat:not(.new-terminal)", "New chat in…"), ("nav.chats .new-row > button.new-terminal", "New terminal in…")]:
    run(f"document.querySelector({button!r}).click(); return true;")
    time.sleep(0.8)
    picker = run("""
      const sheet = document.querySelector('[data-sheet="folder_picker"]:not([inert])');
      return sheet ? { title: sheet.textContent, field: sheet.querySelector('input')?.value ?? '' } : null;
    """) or {}
    check(title in (picker.get("title") or "") and (picker.get("field") or "").rstrip("/").endswith("beta"), f"{title.split(' in')[0]} asks where to start, on beta ({picker.get('field')!r})")
    key("Escape")
    time.sleep(0.6)
finish()
