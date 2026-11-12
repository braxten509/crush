"""The open chats shared with the terminal, and the folder per chat (owner,
2026-10-20). In the real window on the hidden screen, with the fixture's open
chats (proj's, beta's and gamma's; the window is on proj):
- the left list holds the open chats, each with its folder;
- a chat the terminal lists (written to the shared file from outside, as the
  terminal does) shows within a few seconds; the list's x takes a chat off
  the file for every app;
- picking beta's chat switches the window to beta in place: no reload, the
  top bar and the connection follow, a terminal tab stays;
- the search (the Sessions sheet) lists the 5 newest chats in a first
  group, then the shown folder's newest 10 in a second; the rest behind
  "Show more" (a search finds them);
- New chat and New terminal ask where to start (the folder picker), on the
  shown folder.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/folder-switch.json folder_switch.py
"""
import os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, list_open_chat, open_chat_ids, probe_eval, read_probe, shot, type_text, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
BETA = "b0b0b0b0-1111-4222-8333-000000000001"
GAMMA = "c0c0c0c0-1111-4222-8333-000000000001"
DELTA = "d0d0d0d0-1111-4222-8333-000000000001"


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def rows():
    return ((read_probe(probe_path) or {}).get("list") or {}).get("rows") or []


def dialogs():
    return ((read_probe(probe_path) or {}).get("overlay") or {}).get("dialogs") or []


def until(read, ok, timeout=5):
    end = time.time() + timeout
    value = read()
    while time.time() < end and not ok(value):
        time.sleep(0.1)
        value = read()
    return value


def workspace():
    return ((read_probe(probe_path) or {}).get("connection") or {}).get("workspace_id")


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
wait_for(probe_path, lambda p: len(((p.get("list") or {}).get("rows") or [])) >= 5, 20)

# The open chats, each with its folder.
seen = until(lambda: [(r["title"], r["folder"]) for r in rows() if r["depth"] == 0],
             lambda s: [f for _, f in s][-2:] == ["beta", "gamma"])
check([f for _, f in seen][-2:] == ["beta", "gamma"] and all(f == "proj" for _, f in seen[:-2]) and len(seen) >= 5,
      f"the list is the open chats, each with its folder as its second line ({seen})")
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
key("Return")
before = until(lambda: run(TERMINALS) or 0, lambda n: n >= 1)
check(before >= 1, f"a terminal tab opens ({before})")

# Beta's chat: the window switches to beta in place and shows it.
run(f"document.querySelector('nav.chats .chat[data-id=\"{BETA}\"] .row').click(); return true;")
wait_for(probe_path, lambda p: (p.get("connection") or {}).get("workspace_id") == "ws-beta" and (p.get("connection") or {}).get("state") == "connected", 20)
after = until(lambda: run("""
  return { kept: window.__noReload === true, project: document.querySelector('[data-probe="title-project"]')?.textContent.trim(),
           shown: document.querySelector('nav.chats .chat .row[aria-current]')?.closest('.chat')?.getAttribute('data-id') ?? null,
           terminals: document.querySelectorAll('nav.chats li.terminal').length };
""") or {}, lambda a: a.get("project") == "beta" and a.get("shown") == BETA)
check(workspace() == "ws-beta" and after.get("kept"), f"picking beta's chat switches the window to beta with no reload ({workspace()}, {after})")
check(after.get("project") == "beta" and after.get("shown") == BETA, f"the top bar shows beta and beta's chat is open ({after})")
check(after.get("terminals") == before, f"the terminal rows stay ({before} before, {after.get('terminals')} after)")
order = [r["id"] for r in rows() if r["depth"] == 0]
check(BETA in order and DELTA in order and order.index(BETA) < order.index(DELTA), f"the list keeps its order across the switch ({order})")
shot(out_dir, "list-beta")

# The search: first the 5 newest chats of any folder, then under beta's name
# the rest of beta's chats (owner, 2026-10-20).
run("document.querySelector('nav.chats button.search').click(); return true;")
sheet = until(lambda: run("""
  const s = document.querySelector('[data-sheet="sessions"]');
  return s && { groups: [...s.querySelectorAll('[data-group]')].map((el) => el.textContent.trim()),
                rows: [...s.querySelectorAll('[data-index]')].map((el) => el.textContent.trim()) };
""") or {}, lambda sh: len(sh.get("rows") or []) >= 15)
want = ["Beta chat", "Is SameSite=Lax safe?", "Cookie lifetime", "Gamma chat", "Login keeps redirecting", "Beta notes"] + [f"Beta old {i:02d}" for i in range(1, 10)]
got = [next((w for w in want if r.startswith(w)), r) for r in sheet.get("rows") or []]
groups = sheet.get("groups") or []
check(len(groups) == 2 and groups[1].endswith("beta") and got == want,
      f"the search shows the 5 newest chats, then beta's newest 10 under beta's group ({groups}, {got})")
# The rest wait behind "Show more" at the end; a search finds them anyway.
MORE = "return !!document.querySelector('[data-sheet=\"sessions\"] [data-show-more]');"
FOUND = "return [...document.querySelectorAll('[data-sheet=\"sessions\"] [data-index]')].map((el) => el.textContent.trim());"
COUNT = "return document.querySelectorAll('[data-sheet=\"sessions\"] [data-index]').length;"
check(run(MORE) is True, "the folder's other chats wait behind Show more")
type_text("old 12")
found = until(lambda: run(FOUND) or [], lambda f: any(r.startswith("Beta old 12") for r in f))
check(any(r.startswith("Beta old 12") for r in found) and run(MORE) is False, f"a search finds a chat behind Show more ({found})")
for _ in range(len("old 12")):
    key("BackSpace")
until(lambda: run(MORE), lambda m: m is True)
run("document.querySelector('[data-sheet=\"sessions\"] [data-show-more]').click(); return true;")
rows_now = until(lambda: run(COUNT), lambda n: n == 5 + 13)
check(rows_now == 5 + 13 and run(MORE) is False, f"Show more shows the rest ({rows_now} rows)")
key("Escape")
until(dialogs, lambda d: "sessions" not in d)

# New chat and New terminal ask where to start, on the shown folder.
for button, kind in [("nav.chats .new-row > button.new-chat:not(.new-terminal)", "chat"), ("nav.chats .new-row > button.new-terminal", "terminal")]:
    run(f"document.querySelector({button!r}).click(); return true;")
    picker = until(lambda: run("""
      const sheet = document.querySelector('[data-sheet="folder_picker"]:not([inert])');
      return sheet ? { field: sheet.querySelector('input')?.value ?? '' } : null;
    """) or {}, lambda p: bool(p))
    asked = (((read_probe(probe_path) or {}).get("leftlist") or {}).get("picker") or {}).get("kind")
    check(asked == kind and (picker.get("field") or "").rstrip("/").endswith("beta"), f"New {kind} asks where to start, on beta ({asked}, {picker.get('field')!r})")
    key("Escape")
    until(dialogs, lambda d: "folder_picker" not in d)
finish()
