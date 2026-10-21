"""The folder is per chat (owner, 2026-10-20). In the real window on the hidden
screen, with the fixture's three folders (proj, the window's; beta and gamma,
chats in the catalog only):
- the left list shows every folder's open chats under folder headings, this
  folder first;
- picking beta's chat switches the window to beta in place: no reload, the
  top bar and the connection follow, a terminal tab stays, and the list puts
  beta first with proj under it;
- the search (the Sessions sheet) lists only the shown folder's chats;
- New chat and New terminal ask where to start (the folder picker), on the
  shown folder.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/folder-switch.json folder_switch.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, probe_eval, read_probe, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
BETA = "b0b0b0b0-1111-4222-8333-000000000001"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def folders():
    return [(f["label"], len(f["rows"])) for f in ((read_probe(probe_path) or {}).get("list") or {}).get("folders", [])]


def workspace():
    return ((read_probe(probe_path) or {}).get("connection") or {}).get("workspace_id")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
wait_for(probe_path, lambda p: len(((p.get("list") or {}).get("folders") or [])) >= 3, 20)
time.sleep(1)

# Every folder's open chats under its heading, the window's folder first.
shown = folders()
check([label for label, _ in shown] == ["proj", "beta", "gamma"] and all(n > 0 for _, n in shown), f"the list groups every folder's chats under folder headings, this one first ({shown})")
shot(out_dir, "list-proj")

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
shown = folders()
check([label for label, _ in shown][0] == "beta" and "proj" in dict(shown), f"the list now puts beta first, proj among the others ({shown})")
check(dict(shown).get("proj") == 3, f"proj's two branches stay under their chat, not listed on their own ({shown})")
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
