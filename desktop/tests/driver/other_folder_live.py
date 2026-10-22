"""Another folder's open chat stays drawn and live (owner, 2026-10-22: every
listed chat stays open "with all drawing and functionality", and "I DO want
live busy dots"). On the real engine, the window stays in /tmp/home/proj:
- live-1-drawn  a chat of /tmp/home/beta, listed as the terminal lists one,
                gets its drawn timeline (read from beta's saved files) without
                being picked;
- live-2-reply  another app sends in beta's chat (scripts/stream2m.json, about
                10 s): the hidden timeline takes the reply as it streams, and
                beta's row shows "Working…" within 10 s (the 30 s check alone
                would be slower);
- live-3-done   once the reply ends the row shows "Done", and picking the
                chat shows the whole reply at once.
Usage: tests/check.sh other_folder_live.py
"""
import contextlib, importlib.util, os, shutil, subprocess, sys, time, uuid

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
        subprocess.run(["grim", os.path.join(out_dir, name + ".png")], check=False)


def run(probe_path, script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    side = rc.WindowSide(probe_path)
    slot = lambda cid: run(probe_path, f"const el = document.querySelector('[data-timeline-slot=\"{cid}\"]');"
                           " return el ? { seen: !el.classList.contains('parked'), text: el.textContent } : null;")
    row = lambda cid: run(probe_path, f"return document.querySelector('nav.chats .chat[data-id=\"{cid}\"]')?.innerText ?? '';") or ""

    with step("live-1-drawn", out_dir):
        check(pc.set_active_script(port, "stream2m") == "stream2m", "the fake server plays script stream2m")
        if not check(side.join(), "the app joins proj's engine"):
            return
        _, _, wid = side.engine()
        os.makedirs(BETA, exist_ok=True)
        shutil.copy(os.path.join(pc.PROJ, "crush.json"), os.path.join(BETA, "crush.json"))
        beta = pc.seed_chat.Engine(pc.seed_chat.ensure(pc.CRUSH, BETA))
        client = str(uuid.uuid4())
        env = [f"{k}={v}" for k, v in os.environ.items()]
        beta_ws = beta.call("POST", "/v1/workspaces", {"path": BETA, "data_dir": "", "version": "other-folder", "protocol": 1,
                                                       "client_id": client, "yolo": True, "env": env})["id"]
        events = beta.follow_events(f"/v1/workspaces/{beta_ws}/events?client_id={client}")
        chat = beta.call("POST", f"/v1/workspaces/{beta_ws}/sessions?client_id={client}", {"title": "Beta live chat"})["id"]
        list_open_chat({"id": chat, "dir": BETA, "title": "Beta live chat"})
        drawn, got, _ = rc.poll(lambda: slot(chat), lambda s: s is not None and not s["seen"], 15)
        check(drawn, f"beta's chat is drawn, hidden, without being picked ({got})")
        conn = side.conn()
        check(conn.get("workspace_id") == wid, "the window stays in proj")

    with step("live-2-reply", out_dir):
        beta.call("POST", f"/v1/workspaces/{beta_ws}/agent/init?client_id={client}", {"interactive": True}, timeout=120)
        beta.call("POST", f"/v1/workspaces/{beta_ws}/agent?client_id={client}", {"session_id": chat, "prompt": LONG})
        working, state, took = rc.poll(lambda: row(chat), lambda t: "Working" in t, 10)
        check(working, f"beta's row shows Working… within 10 s ({state!r}, {round(took, 1)} s)")
        grew, got, _ = rc.poll(lambda: slot(chat), lambda s: bool(s) and LONG in s["text"] and len(s["text"]) > len(LONG) + 40, 15)
        check(grew and not got["seen"], f"the hidden timeline takes the reply as it streams ({len((got or {}).get('text', ''))} characters)")

    with step("live-3-done", out_dir):
        done, state, _ = rc.poll(lambda: row(chat), lambda t: "Done" in t, 40)
        check(done, f"beta's row shows Done once the reply ends ({state!r})")
        streamed = len((slot(chat) or {}).get("text", ""))
        run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{chat}\"] .row').click(); return true;")
        shown, got, took = rc.poll(lambda: slot(chat), lambda s: bool(s) and s["seen"], 5, 0.02)
        check(shown and took < 0.5, f"picking it shows it at once ({round(took, 2)} s)")
        check(len((got or {}).get("text", "")) >= streamed > 200, f"with the whole reply ({streamed} characters)")
        beta.call("DELETE", f"/v1/clients/{client}")
        events.close()


if __name__ == "__main__":
    main()
    finish()
