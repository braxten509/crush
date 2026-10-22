"""A sub-agent's chat is a normal chat (owner, 2026-10-21). On the real engine:
- sub-1-open    the app's chat starts a sub-agent (crush spawn, scripts/
                subagent.json, about 25 s); a click on its SUBAGENTS row opens
                its chat with a typing box, and the left list has its row
                under the folder, marked Subagent.
- sub-2-running "Worker chat message one" typed there while it runs goes to
                the sub-agent: its reply is in that chat, and the parent is
                handed it as the task's result.
- sub-3-done    once done the row stays while shown; "Worker chat message
                two" typed there is a follow-up whose result reaches the
                parent too. Opening the parent then takes the row away.
Usage: tests/check.sh subagent_chat.py
"""
import contextlib, glob, importlib.util, json, os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import harness as H
from harness import check, finish, probe_eval


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


rc = load("p3_reconnect", os.path.join(os.path.dirname(os.path.abspath(__file__)), "p3_reconnect.py"))
pc = rc.pc
FIRST = "Worker chat message one"
SECOND = "Worker chat message two"


@contextlib.contextmanager
def step(name, out_dir):
    print("STEP " + name, flush=True)
    yield
    if os.environ.get("DISPLAY"):
        H.shot(out_dir, name)


def run(probe_path, script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def texts(engine, wid, sid):
    """Each message of the chat as the engine has it: (role, flat text)."""
    try:
        got = engine.call("GET", f"/v1/workspaces/{wid}/sessions/{sid}/messages") or []
    except Exception:  # noqa: BLE001
        return []
    return [(m.get("role"), pc.flat(rc.engine_text(m))) for m in got]


def told(engine, wid, parent, text):
    return any(role == "user" and "<crush-task-result>" in t and text in t for role, t in texts(engine, wid, parent))


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    side = rc.WindowSide(probe_path)
    with step("sub-1-open", out_dir):
        check(pc.set_active_script(port, "subagent") == "subagent", "the fake server plays script subagent")
        if not check(side.join(), "the app joins proj's engine"):
            return
        _, sock, wid = side.engine()
        engine = pc.seed_chat.Engine(sock)
        check(side.prepare(), "the chat is ready")
        check(side.send("Parent chat for the sub-agent check"), "the app sends the parent's first message")
        parent = side.wait_chat_id(15)
        dirs = sorted(glob.glob("/tmp/crush-*/tasks-*"), key=os.path.getmtime)
        if not check(bool(parent) and bool(dirs), f"the parent chat and the engine's task folder ({parent}, {dirs})"):
            return
        env = {**os.environ, "CRUSH_TASKS_DIR": dirs[-1], "CRUSH_SESSION_ID": parent}
        spawned = subprocess.run(["crush", "spawn", "--cli", "fake", "--model", "fake-small", "--name", "worker"],
                                 input="Worker task: stream slowly", text=True, capture_output=True, env=env, timeout=30)
        check(spawned.returncode == 0, f"crush spawn starts the sub-agent ({spawned.stdout.strip()} {spawned.stderr.strip()})")
        shown, _, _ = rc.poll(lambda: run(probe_path, "return !!document.querySelector('[data-section=\"subagents\"] .open');"), bool, 15)
        check(shown, "the SUBAGENTS row shows")
        run(probe_path, "document.querySelector('[data-section=\"subagents\"] .open').click(); return true;")
        child, got, _ = rc.poll(side.chat_id, lambda c: bool(c) and c != parent, 10)
        check(child, f"its chat opens ({got} after {parent})")
        row = lambda: run(probe_path, f"const r = document.querySelector('nav.chats .chat[data-id=\"{got}\"]'); return {{mark: r?.querySelector('.helper-mark')?.textContent ?? '', selected: r?.classList.contains('selected') ?? false, box: !!document.querySelector('textarea.input')}};") or {}
        listed, state, _ = rc.poll(row, lambda r: r.get("mark") == "Subagent" and r.get("selected") and r.get("box"), 10)
        check(listed, f"its left-list row is marked Subagent and selected, with a typing box ({state})")
        child = got

    with step("sub-2-running", out_dir):
        check(side.send(FIRST), f"\"{FIRST}\" is typed in its chat while it runs")
        replied, _, _ = rc.poll(lambda: texts(engine, wid, child), lambda t: ("assistant", "Worker heard the first message.") in t, 60)
        check(replied, "the sub-agent answers it in its own chat")
        main_left = not any(FIRST in t for _, t in texts(engine, wid, parent) if "<crush-task-result>" not in t)
        check(main_left, "the main agent did not get it as a prompt")
        handed, _, _ = rc.poll(lambda: told(engine, wid, parent, "Worker heard the first message."), bool, 30)
        check(handed, "the parent is handed the reply as the task's result")

    with step("sub-3-done", out_dir):
        stays = run(probe_path, f"return !!document.querySelector('nav.chats .chat[data-id=\"{child}\"]');")
        check(stays, "the done sub-agent's row stays while its chat shows")
        check(side.send(SECOND), f"\"{SECOND}\" is typed in its chat once it is done")
        followed, _, _ = rc.poll(lambda: told(engine, wid, parent, "Worker answered the follow-up."), bool, 40)
        check(followed, "the follow-up's result reaches the parent")
        check(("user", SECOND) in texts(engine, wid, child), "the follow-up shows in its chat as typed")
        rc.poll(lambda: run(probe_path, f"return document.querySelector('nav.chats .chat[data-id=\"{child}\"]')?.innerText ?? '';"), lambda t: "Working" not in t, 15)
        run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{parent}\"] .row').click(); return true;")
        gone, _, _ = rc.poll(lambda: run(probe_path, f"return !document.querySelector('nav.chats .chat[data-id=\"{child}\"]');"), bool, 10)
        if not check(gone, "opening the parent takes the done sub-agent's row away"):
            print("shown chat:", side.chat_id(), "parent:", parent, flush=True)
            live = side.app.report().get("live") or {}
            print("helper chats:", live.get("helper_chats"), flush=True)
            print("engine helpers:", engine.call("GET", f"/v1/workspaces/{wid}/sessions/{parent}/helpers"), flush=True)
            print("rows:", run(probe_path, "return [...document.querySelectorAll('nav.chats .chat')].map((r) => r.dataset.id + ' ' + r.innerText.replace(/\\n/g, ' | '));"), flush=True)


main()
finish()
