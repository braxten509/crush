"""A sub-agent's chat shows, read only (owner, 2026-10-21, 2026-10-22). On the real engine:
- sub-1-open    the app's chat starts a sub-agent (crush spawn, scripts/
                subagent.json, about 25 s); a click on its SUBAGENTS row opens
                its chat with a typing box, and the left list has its row
                under the folder, marked Subagent.
- sub-2-running while it runs its typing box takes no typing, its model box
                shows the sub-agent's own model and opens no picker, and the
                parent is handed its result once it finishes.
- sub-3-done    once done the row stays while shown and the chat stays read
                only; opening the parent then takes the row away and gives
                the parent's own typing box and model button back.
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
# What the sub-agent's own turn streams (scripts/subagent.json), handed to the
# parent as the task's result.
RESULT = "The worker reads each file in turn"
BOX = """const c = document.querySelector('[data-probe="composer"]');
  const f = c?.querySelector('textarea.input');
  const m = c?.querySelector('[data-helper-model]');
  return { field: Boolean(f), read_only: f ? f.readOnly : null, tab: f?.getAttribute('tabindex') ?? null,
           model: (m?.innerText ?? '').trim(), tag: m?.tagName?.toLowerCase() ?? null,
           picker: Boolean(c?.querySelector('button[aria-haspopup="dialog"]')) };"""


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
        # Its composer shows but takes nothing (owner, 2026-10-22), and its
        # model box names the sub-agent's own model with no picker to open.
        box = run(probe_path, BOX) or {}
        check(box.get("field") and box.get("read_only") is True and box.get("tab") == "-1", f"its typing box shows and takes no typing ({box})")
        check(box.get("tag") == "span" and not box.get("picker"), f"its model box opens no picker ({box})")
        check("fake-small" in (box.get("model") or ""), f"its model box shows the sub-agent's own model ({box})")
        typed = side.send(FIRST)
        time.sleep(1.0)
        check(not typed and side.field() == "", f"\"{FIRST}\" cannot be typed there ({typed}, {side.field()!r})")
        check(not any(FIRST in t for _, t in texts(engine, wid, child)), "nothing typed reaches the sub-agent")
        handed, _, _ = rc.poll(lambda: told(engine, wid, parent, RESULT), bool, 60)
        check(handed, "the parent is handed the sub-agent's result")

    with step("sub-3-done", out_dir):
        stays = run(probe_path, f"return !!document.querySelector('nav.chats .chat[data-id=\"{child}\"]');")
        check(stays, "the done sub-agent's row stays while its chat shows")
        box = run(probe_path, BOX) or {}
        check(box.get("read_only") is True, f"its chat stays read only once it is done ({box})")
        run(probe_path, f"document.querySelector('nav.chats .chat[data-id=\"{parent}\"] .row').click(); return true;")
        gone, _, _ = rc.poll(lambda: run(probe_path, f"return !document.querySelector('nav.chats .chat[data-id=\"{child}\"]');"), bool, 10)
        if not check(gone, "opening the parent takes the done sub-agent's row away"):
            print("shown chat:", side.chat_id(), "parent:", parent, flush=True)
            live = side.app.report().get("live") or {}
            print("helper chats:", live.get("helper_chats"), flush=True)
            print("engine helpers:", engine.call("GET", f"/v1/workspaces/{wid}/sessions/{parent}/helpers"), flush=True)
            print("rows:", run(probe_path, "return [...document.querySelectorAll('nav.chats .chat')].map((r) => r.dataset.id + ' ' + r.innerText.replace(/\\n/g, ' | '));"), flush=True)
        back = run(probe_path, BOX) or {}
        check(back.get("field") and back.get("read_only") is False and not back.get("model") and back.get("picker"), f"the parent's chat types again, with its model button back ({back})")


main()
finish()
