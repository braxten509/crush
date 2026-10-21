"""A question answered in another window leaves the next one showing here
(owner, 2026-10-21: "when a question shows up from you it shows up in only
the terminal ... it should show in both until closed"). The slot read its
closing entry while it rolled away (Slot.svelte), which threw once the
entry was gone; from then on the window showed no question.

In the real window on the hidden screen, the fixture asks question A, then
says A was answered elsewhere (the terminal), then asks question B. A must
show, close without a page error, and B must show.

Usage: tests/check.sh --no-app question_twice.py
(the driver writes its fixture in tests/out/question_twice/)
"""
import json, os, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from apps import App
from harness import check, finish, probe_eval, shot, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]
CHAT = "7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01"
ROOT = os.path.join(os.path.dirname(__file__), "..", "..")


def question(qid, words):
    return {"id": qid, "session_id": CHAT, "tool_call_id": "", "questions": [{
        "id": "pick", "type": "single_choice", "label": "Pick", "question": words, "description": "A test question.",
        "choices": [{"id": "a", "label": "First"}, {"id": "b", "label": "Second"}]}]}


fixture = json.load(open(os.path.join(ROOT, "tests", "fixtures", "model-menu.json")))
fixture["events"] = [
    {"at_ms": 2000, "entries": [{"kind": "event", "type": "question_batch_request", "event": "created", "payload": question("qa", "Question A?")}]},
    {"at_ms": 5000, "entries": [{"kind": "event", "type": "question_batch_notification", "event": "created", "payload": {"batch_id": "qa", "session_id": CHAT}}]},
    {"at_ms": 8000, "entries": [{"kind": "event", "type": "question_batch_request", "event": "created", "payload": question("qb", "Question B?")}]},
]
fixture_path = os.path.join(out_dir, "fixture.json")
json.dump(fixture, open(fixture_path, "w"))

state_home = os.environ.get("XDG_STATE_HOME") or os.path.join(os.path.expanduser("~"), ".local", "state")
log_path = os.path.join(state_home, "crush-desktop", "desktop.log")
log_start = os.path.getsize(log_path) if os.path.exists(log_path) else 0


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


SHOWN = "return document.querySelector('[data-probe=\"slot\"][data-kind=\"question\"]')?.innerText.match(/Question [AB]\\?/)?.[0] ?? '';"


def wait_shown(want, timeout):
    end = time.time() + timeout
    got = None
    while time.time() < end:
        got = run(SHOWN)
        if got == want:
            return got
        time.sleep(0.1)
    return got


app = App(args=("--fixture", fixture_path, "-s", CHAT))
wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)
check(wait_shown("Question A?", 15) == "Question A?", "question A shows")
check(wait_shown("", 8) == "", "question A closes once answered in another window")
check(wait_shown("Question B?", 8) == "Question B?", "question B shows after A was answered elsewhere")
shot(out_dir, "question-b")
time.sleep(0.5)
errors = []
if os.path.exists(log_path):
    with open(log_path, errors="replace") as f:
        f.seek(log_start)
        errors = [line.strip()[:200] for line in f if "Uncaught" in line]
check(not errors, f"the window logged no uncaught error ({errors[:3]})")
app.stop()
finish()
