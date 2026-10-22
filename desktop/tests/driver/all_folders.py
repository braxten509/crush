"""Several folders live at once (owner, 2026-10-22: "I should be able to have
multiple folders open at once - not this one folder stuff"). On the real
engine:

- af-1-join      the app joins /tmp/home/proj; its client id and workspace
                 are kept for the checks below.
- af-2-both-live a chat the terminal opens in /tmp/home/beta (written to the
                 shared open chats file) joins beta to the window: the probe
                 lists both folders, and the engine runs both workspaces.
- af-3-show-beta picking beta's chat shows beta; no reload, and proj's
                 workspace is still there 15 s later (the engine gives a
                 folder up 10 s after its last stream closes, so a folder
                 that is only still there at once was left behind).
- af-4-send      a prompt sent in beta answers, and proj stays joined.
- af-5-back      picking proj's chat shows proj again with the same client
                 id and workspace: nothing rejoined; beta stays joined.
- af-6-live      a long reply in proj keeps streaming while beta shows: its
                 row reads Working… and then its time, from the other folder
                 (the file watch still draws it).

Fails when a switch gives a folder up (the helper never sends a DELETE, so
no keep_work either: the folder's stream is kept), when the shown folder's
client id changes across a round trip, when both folders are not joined, or
when the other folder's chat stops drawing.

Usage: tests/check.sh all_folders.py
"""
import importlib.util, os, shutil, sys, time, uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import harness as H
from harness import check, finish, list_open_chat, probe_eval


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


rc = load("p3_reconnect", os.path.join(os.path.dirname(os.path.abspath(__file__)), "p3_reconnect.py"))
pc = rc.pc
BETA = "/tmp/home/beta"
LONG = rc.LONG


def run(probe_path, script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def folders(side):
    return side.conn().get("folders") or []


def workspaces(sock):
    """The workspaces the engine runs, by id (internal/rpc/routes.go:36)."""
    try:
        got = pc.seed_chat.Engine(sock).call("GET", "/v1/workspaces") or []
    except Exception:  # noqa: BLE001 - an engine that went away answers with an error
        return {}
    return {ws.get("id"): ws for ws in got if isinstance(ws, dict)}


def finished(sock, wid, sid, prompt):
    """Whether the engine has a finished reply to `prompt` in that chat."""
    try:
        got = rc.engine_messages(sock, wid, sid)
    except Exception:  # noqa: BLE001 - a workspace that closed answers with an error
        return False
    reply = rc.reply_after(got, prompt)
    return bool(reply) and bool(reply.get("finished"))


def row_text(probe_path, chat):
    return run(probe_path, f"return document.querySelector('nav.chats .chat[data-id=\"{chat}\"]')?.innerText ?? '';") or ""


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    side = rc.WindowSide(probe_path)
    check(pc.set_active_script(port, "stream2m") == "stream2m", "the fake server plays script stream2m")
    if not check(side.join(), "af-1-join: the app joins proj's engine"):
        return
    _, sock, wid = side.engine()
    proj_client = side.conn().get("client_id")
    check(bool(sock) and bool(wid) and bool(proj_client), f"proj's engine, workspace and client are known ({wid}, {proj_client})")
    if not check(side.prepare(), "the chat is ready"):
        return
    # A chat in proj: the first prompt makes one, and its long reply is the
    # af-6-live check below.
    if not check(side.send(LONG) if os.environ.get("DISPLAY") else run(
        probe_path,
        f"const el = document.querySelector('textarea.input'); el.value = {LONG!r};"
        "el.dispatchEvent(new Event('input', {bubbles:true})); await new Promise(r => setTimeout(r, 0));"
        "document.querySelector('[data-probe=\"composer\"] .send').click(); return true;"), "proj sends a prompt"):
        return
    proj_chat = side.wait_chat_id(15)
    if not check(bool(proj_chat), f"proj's chat is known ({proj_chat})"):
        return
    started, _, _ = rc.poll(lambda: row_text(probe_path, proj_chat), lambda t: "Working" in t, 20)
    check(started, f"proj's reply shows Working… ({row_text(probe_path, proj_chat)!r})")

    # beta: a folder with a chat, as the terminal has one.
    os.makedirs(BETA, exist_ok=True)
    shutil.copy(os.path.join(pc.PROJ, "crush.json"), os.path.join(BETA, "crush.json"))
    beta_engine = pc.seed_chat.Engine(pc.seed_chat.ensure(pc.CRUSH, BETA))
    driver_client = str(uuid.uuid4())
    beta_ws = beta_engine.call("POST", "/v1/workspaces", {
        "path": BETA, "data_dir": "", "version": "all-folders", "protocol": 1, "client_id": driver_client, "yolo": True,
        "env": [f"{k}={v}" for k, v in os.environ.items()],
    })["id"]
    driver_events = beta_engine.follow_events(f"/v1/workspaces/{beta_ws}/events?client_id={driver_client}")
    other = beta_engine.call("POST", f"/v1/workspaces/{beta_ws}/sessions?client_id={driver_client}", {"title": "Beta chat"})["id"]
    list_open_chat({"id": other, "dir": BETA, "title": "Beta chat"})

    # Each folder has its own engine (a folder is a project's): beta's answer
    # comes from beta's engine, proj's from proj's.
    live, state, _ = rc.poll(lambda: (sorted(folders(side)), wid in workspaces(sock), beta_ws in workspaces(beta_engine.sock)),
                             lambda s: BETA in s[0] and s[1] and s[2], 40)
    check(live, f"af-2-both-live: both folders are joined, and each engine runs its workspace ({state})")
    shown, _, _ = rc.poll(lambda: run(probe_path, f"return !!document.querySelector('nav.chats .chat[data-id=\"{other}\"] .row');"), bool, 10)
    if not check(shown, "beta's chat shows in the list"):
        return

    # Showing beta leaves proj alone.
    run(probe_path, "window.__noReload = true; return true;")
    run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{other}\"] .row').click(); return true;")
    moved, conn, _ = rc.poll(side.conn, lambda c: c.get("state") == "connected" and c.get("workspace_id") == beta_ws, 30)
    check(moved, f"af-3-show-beta: picking beta's chat shows beta ({conn.get('workspace_id')})")
    check(run(probe_path, "return window.__noReload === true;") is True, "the window did not reload")
    time.sleep(15)
    check(wid in workspaces(sock), "proj's workspace is still there 15 s after the switch")
    check(BETA in folders(side) and len(folders(side)) >= 2, f"the window still keeps both folders ({folders(side)})")

    # A prompt in beta answers while proj stays joined.
    sent = side.send("Reconnect check: beta folder") if os.environ.get("DISPLAY") else run(
        probe_path,
        "const el = document.querySelector('textarea.input'); el.value = 'Reconnect check: beta folder';"
        "el.dispatchEvent(new Event('input', {bubbles:true})); await new Promise(r => setTimeout(r, 0));"
        "document.querySelector('[data-probe=\"composer\"] .send').click(); return true;")
    if not check(sent, "af-4-send: the app sends in beta"):
        return
    beta_chat = side.wait_chat_id(15)
    done, got, _ = rc.poll(lambda: finished(beta_engine.sock, beta_ws, beta_chat, "Reconnect check: beta folder"), bool, 90)
    check(done, f"beta's reply finishes on beta's engine ({got})")
    check(wid in workspaces(sock), "proj is still joined while beta answers")
    check("Working" not in row_text(probe_path, beta_chat), f"beta's row settles ({row_text(probe_path, beta_chat)!r})")

    # Back to proj: the same client, so nothing rejoined.
    run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{proj_chat}\"] .row').click(); return true;")
    back, conn, _ = rc.poll(side.conn, lambda c: c.get("state") == "connected" and c.get("workspace_id") == wid, 30)
    check(back, f"af-5-back: picking proj's chat shows proj again ({conn.get('workspace_id')})")
    check(conn.get("client_id") == proj_client, f"the same client joins proj: nothing rejoined ({conn.get('client_id')} was {proj_client})")
    check(BETA in folders(side), f"beta stays joined ({folders(side)})")

    # af-6-live: a reply running in proj is drawn while beta shows.
    LONGER = "Stream for two minutes"
    sent = side.send(LONGER) if os.environ.get("DISPLAY") else run(
        probe_path,
        f"const el = document.querySelector('textarea.input'); el.value = {LONGER!r};"
        "el.dispatchEvent(new Event('input', {bubbles:true})); await new Promise(r => setTimeout(r, 0));"
        "document.querySelector('[data-probe=\"composer\"] .send').click(); return true;")
    if not check(sent, "af-6-live: proj sends a long prompt"):
        return
    ran, _, _ = rc.poll(lambda: row_text(probe_path, proj_chat), lambda t: "Working" in t, 30)
    check(ran, f"proj's reply runs ({row_text(probe_path, proj_chat)!r})")
    run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{other}\"] .row').click(); return true;")
    rc.poll(side.conn, lambda c: c.get("workspace_id") == beta_ws, 20)
    drawn, state, _ = rc.poll(lambda: row_text(probe_path, proj_chat), lambda t: "Working" in t, 15)
    check(drawn, f"proj's row still reads Working… from beta ({state!r})")
    check(wid in workspaces(sock), "proj's workspace stays joined while beta shows")
    # Nothing streams on after the run: the reply is stopped.
    pc.seed_chat.Engine(sock).call("POST", f"/v1/workspaces/{wid}/agent/sessions/{proj_chat}/interrupt?client_id={proj_client}", {})

    beta_engine.call("DELETE", f"/v1/clients/{driver_client}")
    driver_events.close()


if __name__ == "__main__":
    main()
    finish()
