"""Losing the engine (desktop-design 2.9 "Losing the engine", SIDE-030's
notices, TYPE-048's window step): the engine dies under the window and the
window gets a new one without losing anything.

Case 1, every run:
  reconnect-1-stream    the run's app (/tmp/home/proj) is connected and sends
                        "Reconnect check: a long reply" (scripts/stream2m.json,
                        about 10 s); the reply streams.
  reconnect-2-kill      with "Reconnect check: kept while reconnecting" typed in
                        the field, kill -9 on connection.engine.pid: the lost
                        notice (kind warn) is issued (on the line in a probe
                        write, or in connection.notices: a quick rejoin
                        replaces it before a write, as the terminal does;
                        reconnect-6 proves it reaches the line), a new engine
                        from ensure (another pid, fresh_engine true), the
                        reconnected notice (kind success), connected within
                        15 s; the field keeps its text the whole time.
  reconnect-3-reload    the chat reloads: the window has every finished message
                        the new engine has, none twice, in the engine's order;
                        and chat.busy is false (D41: the reply the engine's
                        death stopped does not keep the chat busy).
  reconnect-4-send      the kept text sends, and its reply comes.
  reconnect-5-mid-send  TYPE-048: a prompt sent just as the engine is stopped is
                        not lost: after the rejoin it is in the engine's list,
                        back in the field, or shown pending in the chat.
Case 2, only with CHECK_CRUSH=$PWD/tests/fakes/crush-wrap (its ensure fails
while /tmp/fail-ensure exists):
  reconnect-6-failed    /tmp/fail-ensure made, kill -9: reconnecting, with the
                        lost notice on the line while the tries fail, then the
                        failed notice (kind error) with Try again; the field
                        keeps "Reconnect check: typed while failed" and Enter
                        sends nothing.
  reconnect-7-try-again the file removed, Try again: connected within 15 s, the
                        reconnected notice, the chat matches the engine,
                        nothing was sent while failed, and the kept text then
                        sends.
Fails when the state is not connected within 15 s, a notice is missing,
messages are missing or doubled against the engine's list, the chat stays busy
after the reload, or the field loses its text or sends while the engine is
gone. The replies are scripts/stream2m.json's "Reconnect check: ..." turns, so
stream2m is made the active script (6.2: drivers activate the script whose
turns they use; 2.9 names script basic, another piece's). The notices are read
from the probe's notice section (1.9, P3.8: kind, text, action{label}) and Try
again is pressed at its heavy rect (notice.action.rect).

Usage: tests/check.sh p3_reconnect.py
       CHECK_CRUSH=$PWD/tests/fakes/crush-wrap tests/check.sh p3_reconnect.py
"""
import importlib.util, os, signal, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, key, step, type_text, wait_for


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


pc = load("p3_connect", os.path.join(os.path.dirname(os.path.abspath(__file__)), "p3_connect.py"))
seed_chat = pc.seed_chat
PROJ = pc.PROJ
FAIL_FILE = "/tmp/fail-ensure"
WRAPPED = os.path.basename(pc.CRUSH) == "crush-wrap"
LONG = "Reconnect check: a long reply"
KEPT = "Reconnect check: kept while reconnecting"
MID = "Reconnect check: sent as the engine stops"
TYPED = "Reconnect check: typed while failed"
# The notices' kinds (lib/stores/connection.svelte.js): lost, failed, back.
LOST = "warn"
FAILED = "error"
BACK = "success"
LIMIT = 15  # 2.9: connected again within 15 s


def engine_text(message):
    return "".join((p.get("data") or {}).get("text", "") for p in message.get("parts") or [] if p.get("type") == "text")


def engine_finished(message):
    return any(p.get("type") == "finish" for p in message.get("parts") or [])


def engine_messages(sock, wid, sid):
    """The engine's own list (the ground truth): [{id, role, finished, text}]."""
    got = seed_chat.Engine(sock).call("GET", f"/v1/workspaces/{wid}/sessions/{sid}/messages") or []
    return [{"id": m["id"], "role": m["role"], "finished": engine_finished(m), "text": engine_text(m)} for m in got]


def user_texts(messages):
    return [pc.flat(m.get("text")) for m in messages if m.get("role") == "user"]


def reply_after(messages, prompt):
    """The last assistant message after the user message prompt, or {}."""
    found, after = {}, False
    for m in messages:
        if m.get("role") == "user":
            after = pc.flat(m.get("text")) == prompt
        elif after and m.get("role") == "assistant":
            found = m
    return found


def compare(window, engine):
    """(agree, why): the window's messages against the engine's: no id twice,
    the engine's order, every finished engine message there with its text."""
    wids = [m.get("id") for m in window]
    if len(set(wids)) != len(wids):
        return False, "the window shows a message twice"
    finished = [m for m in engine if m["finished"]]
    missing = [m["id"] for m in finished if m["id"] not in wids]
    if missing:
        return False, f"the window lacks finished messages {missing}"
    order = [i for i in wids if i in {m["id"] for m in engine}]
    if order != [m["id"] for m in engine if m["id"] in set(wids)]:
        return False, f"the order differs: window {wids} engine {[m['id'] for m in engine]}"
    by_id = {m.get("id"): m for m in window}
    for m in finished:
        if pc.flat(by_id[m["id"]].get("text")) != pc.flat(m["text"]):
            return False, f"the text of {m['id']} differs"
    return True, ""


def notice_seen(samples, kind):
    """Whether the notice line showed a notice of kind in any sample: True or
    False, or None when no sample had a notice section (P3.8 is not in this
    build)."""
    shown = [s for s in samples if s.get("has_notice")]
    if not shown:
        return None
    return any(s.get("notice_kind") == kind for s in shown)


def notices_added(before, after):
    """The kinds of the connection notices shown between two reads of the connection
    section's notices list (1.9: every connection notice handed to the notice
    line, newest last, at most 20). The terminal replaces its status line at
    once (internal/ui/model/agent_events.go:29-52), so a reconnect quicker than
    the probe's 200 ms write shows its lost notice between two writes: the list
    still has it. None when the section has no list."""
    if before is None or after is None:
        return None
    kinds = lambda notices: [n.get("kind") for n in notices]
    before, after = kinds(before), kinds(after)
    for start in range(len(before) + 1):  # the longest end of before that starts after
        if after[:len(before) - start] == before[start:]:
            return after[len(before) - start:]
    return after


def busy_after_reload(read, timeout=5):
    """reconnect-3's D41 check: reads the window's chat.busy until it is false.
    Answers "idle", "busy" (still true at the timeout: a stuck live flag), or
    None when the probe has no chat.busy."""
    passed, value, _ = poll(read, lambda b: b is False, timeout)
    return "idle" if passed else (None if value is None else "busy")


class WindowSide:
    """The real window, read through its probe and driven with the real keyboard."""

    def __init__(self, probe_path):
        self.app = pc.AppRun.existing(probe_path, PROJ)
        self.probe = probe_path

    def conn(self, report=None):
        return (report or self.app.report()).get("connection") or {}

    def has_connection(self):
        return "connection" in self.app.wait(lambda p: "connection" in p, 20)

    def join(self, timeout=45):
        return self.conn(self.app.wait(lambda p: (p.get("connection") or {}).get("state") == "connected", timeout)).get("state") == "connected"

    def engine(self):
        c = self.conn()
        return (c.get("engine") or {}).get("pid"), (c.get("engine") or {}).get("socket"), c.get("workspace_id")

    def prepare(self):
        """START-002: a fresh project asks first; n answers No."""
        _, sock, wid = self.engine()
        api = seed_chat.Engine(sock)
        needs = lambda: bool((api.call("GET", f"/v1/workspaces/{wid}/project/needs-init") or {}).get("needs_init"))
        for _ in range(20):
            if not needs():
                break
            key("n")
            time.sleep(0.5)
        value = self.field()
        if value and set(value) == {"n"}:  # an n that reached the typing box
            key(*["BackSpace"] * len(value))
        return not needs()

    def field(self):
        return self.app.section("composer").get("value") or ""

    def clear(self):
        """Empties the field: Ctrl+Shift+A selects all there (TYPE-009), BackSpace deletes it."""
        if self.field() and self.focus():
            key("ctrl+shift+a", "BackSpace")
        return poll(self.field, lambda v: v == "", 3)[0]

    def wait_chat_id(self, timeout):
        return (self.app.wait(lambda p: bool((p.get("chat") or {}).get("id")), timeout).get("chat") or {}).get("id")

    def focus(self):
        for _ in range(4):
            if self.app.section("composer").get("focused"):
                return True
            key("Tab")
            time.sleep(0.3)
        return bool(self.app.section("composer").get("focused"))

    def type_only(self, text):
        if not self.focus():
            return False
        type_text(text)
        return (wait_for(self.probe, lambda p: (p.get("composer") or {}).get("value") == text, 5).get("composer") or {}).get("value") == text

    def enter(self):
        key("Return")

    def send(self, text):
        if not self.type_only(text):
            return False
        self.enter()
        return True

    def chat_id(self):
        return self.app.section("chat").get("id")

    def messages(self):
        return self.app.section("chat").get("messages") or []

    def sample(self):
        """One look at the window: state, attempt, engine pid, fresh_engine, the notice line, the field."""
        p = self.app.report()
        c = p.get("connection") or {}
        return {"state": c.get("state"), "attempt": c.get("attempt"), "pid": (c.get("engine") or {}).get("pid"),
                "fresh": c.get("fresh_engine"), "has_notice": "notice" in p, "notice_kind": (p.get("notice") or {}).get("kind"),
                "field": (p.get("composer") or {}).get("value") or "", "at": time.time()}

    def states(self, samples):
        """The states the window went through. The probe writes every 200 ms, so a
        quick reconnect can fall between two writes; the rejoin's attempt (1 or
        more, 1.2.6) still says it happened."""
        seen = {s["state"] for s in samples if s["state"]}
        if samples and (samples[-1].get("attempt") or 0) >= 1:
            seen.add("reconnecting")
        return seen

    def notice(self, kind, timeout=5, action=None):
        """Waits until the notice line (the probe's notice section, P3.8) shows
        a notice of kind, with the button action when one is given: True or
        False (None when no write had a notice section)."""
        held, _ = pc.wait_section(self.app, "notice", lambda n: n.get("kind") == kind
                                  and (action is None or (n.get("action") or {}).get("label") == action), timeout)
        return held

    def busy(self):
        return self.app.section("chat").get("busy")

    def chat_shows(self, text):
        return any(pc.flat(m.get("text")) == pc.flat(text) for m in self.messages())

    def try_again(self, row):
        """The failed notice's Try again: a real click at notice.action.rect
        (1.9). When it cannot be pressed (a failed check), engine_retry, what
        the button calls, stands in so the steps after it can still run."""
        if pc.press_button(self.app, "notice", "action", "P3.8", row):
            return True
        print("NOTE engine_retry stood in for the click so the later checks still run", flush=True)
        self.app.invoke("engine_retry")
        return False


def watch(side, until, timeout):
    """Samples the window until until(sample) or the timeout: (reached, samples)."""
    samples, end = [], time.time() + timeout
    while time.time() < end:
        s = side.sample()
        samples.append(s)
        if until(s):
            return True, samples
        time.sleep(0.05)
    return False, samples


def kill_engine(pid):
    try:
        os.kill(pid, signal.SIGKILL)
        return True
    except (OSError, TypeError):
        return False


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    side = WindowSide(probe_path)
    print(f"NOTE the app runs {pc.CRUSH}" + ("; case 2 runs (ensure fails while /tmp/fail-ensure exists)" if WRAPPED else ""), flush=True)
    sid = None
    try:
        with step("reconnect-1-stream", out_dir):
            check(pc.set_active_script(port, "stream2m") == "stream2m", "the fake server plays script stream2m, whose turns this check uses (6.2)")
            if not side.has_connection():
                check(False, "the probe has a connection section (P3.3's store is not in this build)")
                check(False, "every step reads the window's connection; stopping here")
                return
            joined = check(side.join(), "the app joins the project's engine")
            pid, sock, wid = side.engine() if joined else (None, None, None)
            ok = check(isinstance(pid, int) and pid > 0 and bool(sock), f"its connection names the engine's pid and socket ({pid}, {sock})")
            if not (joined and ok):
                check(False, "the remaining steps need the engine's pid; stopping here")
                return
            check(side.prepare(), "the chat is ready (the project's init question answered No)")
            check(side.send(LONG), f"the app sends \"{LONG}\"")
            sid = side.wait_chat_id(15)
            check(bool(sid), f"it is on the chat ({sid})")
            streaming = lambda: reply_after(engine_messages(sock, wid, sid), LONG)
            started, reply, _ = poll(lambda: streaming(), lambda m: len(m.get("text") or "") > 40 and not m.get("finished"), 20)
            check(started, f"the reply streams ({len(reply.get('text') or '')} characters so far, finished {reply.get('finished')})")

        with step("reconnect-2-kill", out_dir):
            check(side.type_only(KEPT), f"\"{KEPT}\" is typed in the field")
            old_pid = pid
            notices_before = side.conn().get("notices")
            killed_at = time.time()
            check(kill_engine(old_pid), f"kill -9 on the engine (pid {old_pid})")
            back, samples = watch(side, lambda s: s["state"] == "connected" and s["pid"] not in (None, old_pid), LIMIT)
            took = time.time() - killed_at
            states = side.states(samples)
            check("reconnecting" in states, f"the window turns reconnecting (states seen: {sorted(states)})")
            on_line = notice_seen(samples, LOST)
            added = notices_added(notices_before, side.conn().get("notices"))
            check(bool(on_line) or LOST in (added or []),
                  f"the lost notice is issued (on the line in a probe write: {on_line}; connection notices since the kill: {added})")
            new_pid = samples[-1]["pid"] if samples else None
            check(back, f"it is connected to a new engine within {LIMIT} s (pid {old_pid} -> {new_pid}, {took:.1f} s)")
            pid, sock, wid = side.engine()
            check(bool(samples) and samples[-1]["fresh"] is True, "the window knows the engine is a fresh one (fresh_engine)")
            check(side.notice(BACK) is True, "its notice line shows the reconnected notice")
            kept = [s["field"] for s in samples if s["field"] != KEPT]
            check(not kept and side.field() == KEPT, f"the field kept its text the whole time ({kept[:3]})")

        if not sid:
            check(False, "reconnect-3 to 7 compare the chat reconnect-1 should have opened, and there is none; stopping here")
            return

        with step("reconnect-3-reload", out_dir):
            agree, why = False, "no read"
            for _ in range(50):
                agree, why = compare(side.messages(), engine_messages(sock, wid, sid))
                if agree:
                    break
                time.sleep(0.2)
            check(agree, f"the chat has every finished message of the new engine, none twice, in order {why}")
            busy = busy_after_reload(side.busy)
            check(busy == "idle", f"after the reload the chat is not busy (chat.busy {busy}; "
                                  "D41: the reply the engine's death stopped does not keep it busy)")
            ends = reply_after(engine_messages(sock, wid, sid), LONG)
            print(f"NOTE the stopped reply in the new engine: {len(ends.get('text') or '')} characters, finished {ends.get('finished')}", flush=True)

        with step("reconnect-4-send", out_dir):
            side.enter()
            done, reply, took = poll(lambda: reply_after(engine_messages(sock, wid, sid), KEPT), lambda m: m.get("finished"), 20)
            msgs = engine_messages(sock, wid, sid)
            check(done and user_texts(msgs).count(KEPT) == 1, f"the kept text is sent once and answered ({took:.1f} s)")
            ok, _, _ = poll(side.field, lambda v: v == "", 5)
            check(ok, "the field is empty after sending")

        with step("reconnect-5-mid-send", out_dir):
            check(side.type_only(MID), f"\"{MID}\" is typed in the field")
            old_pid = pid
            side.enter()
            kill_engine(old_pid)
            back, samples = watch(side, lambda s: s["state"] == "connected" and s["pid"] not in (None, old_pid), LIMIT)
            check(back, f"the window is connected to a new engine within {LIMIT} s (pid {old_pid} -> {samples[-1]['pid'] if samples else None})")
            pid, sock, wid = side.engine()
            where = None
            for _ in range(50):
                msgs = engine_messages(sock, wid, sid)
                if MID in user_texts(msgs):
                    where = "in the engine's list"
                elif side.field() == MID:
                    where = "back in the field"
                elif side.chat_shows(MID):
                    where = "pending in the chat"
                if where:
                    break
                time.sleep(0.2)
            check(where is not None, f"TYPE-048: the prompt is not lost ({where})")
            check(user_texts(engine_messages(sock, wid, sid)).count(MID) <= 1, "and the engine has it at most once")

        if not WRAPPED:
            print("NOTE case 2 (ensure fails three times, then Try again) runs with CHECK_CRUSH=$PWD/tests/fakes/crush-wrap", flush=True)
            return

        with step("reconnect-6-failed", out_dir):
            check(side.clear() and side.type_only(TYPED), f"\"{TYPED}\" is typed in an empty field")
            with open(FAIL_FILE, "w") as f:
                f.write("p3_reconnect.py: ensure fails while this file exists\n")
            old_pid = pid
            check(kill_engine(old_pid), f"kill -9 on the engine (pid {old_pid}) while ensure fails")
            failed, samples = watch(side, lambda s: s["state"] == "failed", LIMIT)
            states = side.states(samples)
            check(failed and "reconnecting" in states, f"the window reconnects, then fails (states seen: {sorted(states)})")
            # The tries fail for seconds here, so the lost notice stays up long
            # enough for a probe write: this proves it reaches the line.
            trying = [s for s in samples if s["state"] == "reconnecting"]
            check(notice_seen(trying, LOST) is True, f"while the tries fail, its notice line shows the lost notice ({len(trying)} probe reads while reconnecting)")
            check(side.notice(FAILED, action=pc.TRY_AGAIN) is True, f"its notice line shows the failed notice with a \"{pc.TRY_AGAIN}\" button")
            side.enter()
            # Nothing can go out while failed: the field keeps its text (the
            # engine's list is checked once it is back, in reconnect-7).
            poll(side.field, lambda v: v != TYPED, 0.5, 0.05)
            check(side.field() == TYPED, "the field keeps its text and Enter sends nothing")

        with step("reconnect-7-try-again", out_dir):
            os.remove(FAIL_FILE)
            pressed_at = time.time()
            side.try_again("reconnect-7-try-again: Try again")
            back, samples = watch(side, lambda s: s["state"] == "connected" and s["pid"] not in (None, old_pid), LIMIT)
            check(back, f"Try again connects within {LIMIT} s ({time.time() - pressed_at:.1f} s, pid {samples[-1]['pid'] if samples else None})")
            check(side.notice(BACK) is True, "its notice line shows the reconnected notice")
            pid, sock, wid = side.engine()
            agree, why = False, "no read"
            for _ in range(50):
                agree, why = compare(side.messages(), engine_messages(sock, wid, sid))
                if agree:
                    break
                time.sleep(0.2)
            check(agree, f"the chat matches the engine again {why}")
            check(TYPED not in user_texts(engine_messages(sock, wid, sid)), "nothing was sent while the window was failed")
            check(side.field() == TYPED, "the field still has the text")
            side.enter()
            done, _, took = poll(lambda: reply_after(engine_messages(sock, wid, sid), TYPED), lambda m: m.get("finished"), 20)
            check(done, f"now it sends, and its reply comes ({took:.1f} s)")
    finally:
        if os.path.exists(FAIL_FILE):
            os.remove(FAIL_FILE)


def poll(read, ok, timeout, every=0.1):
    """Calls read until ok(value) or timeout; returns (passed, value, seconds)."""
    start = time.time()
    while True:
        value = read()
        if ok(value):
            return True, value, time.time() - start
        if time.time() - start > timeout:
            return False, value, time.time() - start
        time.sleep(every)


if __name__ == "__main__":
    main()
    finish()
