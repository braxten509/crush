"""A folder the window leaves keeps its running work (owner, 2026-10-21: "it
cancels the agents in other chats, which it should NOT do"). On the real
engine:
- keep-1-stream  the app on /tmp/home/proj sends "Reconnect check: a long
                 reply" (scripts/stream2m.json, about 10 s); the reply streams.
- keep-2-switch  a chat of /tmp/home/beta, listed as the terminal lists one,
                 is picked: the window joins beta's engine.
- keep-3-goes-on a second client stays in proj during the switch, then leaves;
                 returning to the cached chat keeps its reply running.
                 It ends while shown, so its row shows its time and no
                 "Done"; the idle workspace then closes.
- keep-4-other-folder another app sends "Stream for two minutes" (about
                 120 s) in proj while the window shows beta:
                 within the 30 s check proj's row shows "Working…", then
                 "Done" once the reply ends.
Usage: tests/check.sh keep_work.py
"""
import contextlib, importlib.util, json, os, shutil, subprocess, sys, time, uuid

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


@contextlib.contextmanager
def step(name, out_dir):
    print("STEP " + name, flush=True)
    yield
    if os.environ.get("DISPLAY"):
        H.shot(out_dir, name)
    else:
        subprocess.run(["grim", os.path.join(out_dir, name + ".png")], check=True)


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
        if os.environ.get("DISPLAY"):
            check(side.prepare(), "the chat is ready")
            check(side.send(LONG), f"the app sends \"{LONG}\"")
        else:
            run(probe_path, "document.querySelector('[aria-label=\"Initialize this project\"] .no')?.click(); return true;")
            ready, _, _ = rc.poll(lambda: run(probe_path, "return !!document.querySelector('textarea.input');"), bool, 10)
            check(ready, "the chat is ready")
            sent = run(probe_path, f"const el = document.querySelector('textarea.input'); el.value = {json.dumps(LONG)}; el.dispatchEvent(new Event('input', {{bubbles:true}})); await new Promise(resolve => setTimeout(resolve, 0)); document.querySelector('[data-probe=\"composer\"] .send').click(); return true;")
            check(sent is True, f"the app sends \"{LONG}\"")
        sid = side.wait_chat_id(15)
        started, got, _ = rc.poll(lambda: reply_state(sock, wid, sid), lambda s: bool(s) and s[0] > 40 and not s[1], 20)
        if not check(started, f"the reply streams ({got})"):
            return

    with step("keep-2-switch", out_dir):
        os.makedirs(BETA, exist_ok=True)
        shutil.copy(os.path.join(pc.PROJ, "crush.json"), os.path.join(BETA, "crush.json"))
        # A real second client in proj exercises immediate departure cleanup.
        engine = pc.seed_chat.Engine(sock)
        extra = str(uuid.uuid4())
        engine.call("POST", "/v1/workspaces", {"path": pc.PROJ, "data_dir": "", "version": "keep-work", "protocol": 1,
                    "client_id": extra, "yolo": True, "env": [f"{k}={v}" for k, v in os.environ.items()]})
        extra_events = engine.follow_events(f"/v1/workspaces/{wid}/events?client_id={extra}")
        beta_engine = pc.seed_chat.Engine(pc.seed_chat.ensure(pc.CRUSH, BETA))
        beta_client = str(uuid.uuid4())
        beta_ws = beta_engine.call("POST", "/v1/workspaces", {"path": BETA, "data_dir": "", "version": "keep-work", "protocol": 1,
                    "client_id": beta_client, "yolo": True, "env": [f"{k}={v}" for k, v in os.environ.items()]})["id"]
        beta_events = beta_engine.follow_events(f"/v1/workspaces/{beta_ws}/events?client_id={beta_client}")
        other = beta_engine.call("POST", f"/v1/workspaces/{beta_ws}/sessions?client_id={beta_client}", {"title": "Beta chat"})["id"]
        list_open_chat({"id": other, "dir": BETA, "title": "Beta chat"})
        shown, _, _ = rc.poll(lambda: run(probe_path, f"return !!document.querySelector('nav.chats .chat[data-id=\"{other}\"] .row');"), bool, 10)
        check(shown, "beta's chat shows in the list")
        run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{other}\"] .row').click(); return true;")
        moved, conn, _ = rc.poll(side.conn, lambda c: c.get("state") == "connected" and c.get("workspace_id") not in ("", wid), 30)
        check(moved, f"the window joins beta ({conn.get('workspace_id')} after {wid})")
        current = reply_state(sock, wid, sid)
        check(bool(current) and not current[1], f"proj still replies while a second client stays there ({current})")
        engine.call("DELETE", f"/v1/clients/{extra}?keep_work=1")
        extra_events.close()
        run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{sid}\"] .row').click(); return true;")
        returned, _, _ = rc.poll(side.conn, lambda c: c.get("state") == "connected" and c.get("workspace_id") == wid, 20)
        check(returned, "returning to the cached chat joins its own folder")
        current = reply_state(sock, wid, sid)
        check(bool(current) and current[1] not in ("canceled", "cancelled"), f"returning does not stop the original reply ({current})")

    with step("keep-3-goes-on", out_dir):
        done, got, _ = rc.poll(lambda: reply_state(sock, wid, sid), lambda s: s is None or bool(s[1]), 40)
        check(done and got is not None and got[1] == "end_turn" and got[0] > 200,
              f"proj's reply finishes on its engine, not stopped ({got})")
        # It ended while shown: read at once, so no "Done", and its time shows.
        row = lambda: run(probe_path, "const r = document.querySelector('nav.chats .chat.selected'); return {text: r?.innerText ?? '', time: r?.querySelector('.time')?.textContent ?? ''};") or {}
        visible, state, _ = rc.poll(row, lambda r: bool(r.get("time")) and "Done" not in r.get("text", ""), 8)
        check(visible, f"the selected chat shows its time and no Done ({state!r})")
        run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{other}\"] .row').click(); return true;")
        moved, _, _ = rc.poll(side.conn, lambda c: c.get("state") == "connected" and c.get("workspace_id") == beta_ws, 20)
        check(moved, "the cached beta chat also rejoins its own folder")
        state = run(probe_path, f"return document.querySelector('nav.chats .chat[data-id=\"{sid}\"]')?.innerText ?? '';") or ""
        check("Done" not in state and "Working" not in state, f"the read chat stays plain in another folder ({state!r})")
        closed, _, _ = rc.poll(lambda: reply_state(sock, wid, sid), lambda s: s is None, 15)
        # Multi-folder: the window keeps proj joined, so its workspace stays
        # (owner, 2026-10-22). Only its reply ends here.
        check(not closed and bool(reply_state(sock, wid, sid)), "proj's workspace stays: the window still keeps the folder joined")

    with step("keep-4-other-folder", out_dir):
        # Another app sends in proj while the window shows beta: the 30 s
        # check of other folders' chats shows the work, then "Done".
        proj_engine = pc.seed_chat.Engine(pc.seed_chat.ensure(pc.CRUSH, pc.PROJ))
        again = str(uuid.uuid4())
        proj_ws = proj_engine.call("POST", "/v1/workspaces", {"path": pc.PROJ, "data_dir": "", "version": "keep-work", "protocol": 1,
                                   "client_id": again, "yolo": True, "env": [f"{k}={v}" for k, v in os.environ.items()]})["id"]
        again_events = proj_engine.follow_events(f"/v1/workspaces/{proj_ws}/events?client_id={again}")
        proj_engine.call("POST", f"/v1/workspaces/{proj_ws}/agent/init?client_id={again}", {"interactive": True}, timeout=120)
        proj_engine.call("POST", f"/v1/workspaces/{proj_ws}/agent?client_id={again}", {"session_id": sid, "prompt": "Stream for two minutes"})
        text = lambda: run(probe_path, f"return document.querySelector('nav.chats .chat[data-id=\"{sid}\"]')?.innerText ?? '';") or ""
        working, state, _ = rc.poll(text, lambda t: "Working" in t, 40)
        check(working, f"proj's chat shows Working… from beta within 30 s ({state!r})")
        finished, state, _ = rc.poll(text, lambda t: "Done" in t, 180)
        check(finished, f"then Done once it ends, unread ({state!r})")
        proj_engine.call("DELETE", f"/v1/clients/{again}")
        again_events.close()
        beta_engine.call("DELETE", f"/v1/clients/{beta_client}")
        beta_events.close()


if __name__ == "__main__":
    main()
    finish()
