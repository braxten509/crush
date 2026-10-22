"""A folder the window leaves keeps its running work (owner, 2026-10-21: "it
cancels the agents in other chats, which it should NOT do"). On the real
engine:
- keep-1-stream  the app on /tmp/home/proj sends "Reconnect check: a long
                 reply" (scripts/stream2m.json, about 10 s); the reply streams.
- keep-2-switch  a chat of /tmp/home/beta, listed as the terminal lists one,
                 is picked: the window joins beta's engine.
- keep-3-goes-on proj's reply still finishes, not stopped, on proj's engine
                 (the window left it with keep_work=1), and then that
                 workspace closes by itself.
Usage: tests/check.sh keep_work.py
"""
import importlib.util, os, shutil, sys, time, uuid

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, list_open_chat, probe_eval, step


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


def reply_state(sock, wid, sid):
    """proj's reply as its engine has it: (text length, finish reason) or None
    when the engine no longer has the workspace."""
    try:
        got = pc.seed_chat.Engine(sock).call("GET", f"/v1/workspaces/{wid}/sessions/{sid}/messages") or []
    except Exception:  # noqa: BLE001 - a gone workspace answers with an error
        return None
    after, found = False, None
    for m in got:
        if m.get("role") == "user":
            after = pc.flat(rc.engine_text(m)) == LONG
        elif after and m.get("role") == "assistant":
            found = m
    if not found:
        return (0, "")
    reason = next(((p.get("data") or {}).get("reason", "") for p in found.get("parts") or [] if p.get("type") == "finish"), "")
    return (len(rc.engine_text(found)), reason)


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    side = rc.WindowSide(probe_path)
    with step("keep-1-stream", out_dir):
        check(pc.set_active_script(port, "stream2m") == "stream2m", "the fake server plays script stream2m")
        if not check(side.join(), "the app joins proj's engine"):
            return
        _, sock, wid = side.engine()
        check(side.prepare(), "the chat is ready")
        check(side.send(LONG), f"the app sends \"{LONG}\"")
        sid = side.wait_chat_id(15)
        started, got, _ = rc.poll(lambda: reply_state(sock, wid, sid), lambda s: bool(s) and s[0] > 40 and not s[1], 20)
        if not check(started, f"the reply streams ({got})"):
            return

    with step("keep-2-switch", out_dir):
        os.makedirs(BETA, exist_ok=True)
        shutil.copy(os.path.join(pc.PROJ, "crush.json"), os.path.join(BETA, "crush.json"))
        other = str(uuid.uuid4())
        list_open_chat({"id": other, "dir": BETA, "title": "Beta chat"})
        shown, _, _ = rc.poll(lambda: run(probe_path, f"return !!document.querySelector('nav.chats .chat[data-id=\"{other}\"] .row');"), bool, 10)
        check(shown, "beta's chat shows in the list")
        run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{other}\"] .row').click(); return true;")
        moved, conn, _ = rc.poll(side.conn, lambda c: c.get("state") == "connected" and c.get("workspace_id") not in ("", wid), 30)
        check(moved, f"the window joins beta ({conn.get('workspace_id')} after {wid})")

    with step("keep-3-goes-on", out_dir):
        done, got, _ = rc.poll(lambda: reply_state(sock, wid, sid), lambda s: s is None or bool(s[1]), 40)
        check(done and got is not None and got[1] not in ("canceled", "cancelled") and got[0] > 200,
              f"proj's reply finishes on its engine, not stopped ({got})")
        closed, _, _ = rc.poll(lambda: reply_state(sock, wid, sid), lambda s: s is None, 15)
        check(closed, "proj's workspace closes once its work is done")


if __name__ == "__main__":
    main()
    finish()
