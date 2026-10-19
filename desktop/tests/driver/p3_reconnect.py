"""Losing the engine (desktop-design 2.9 "Losing the engine", SIDE-030's
notices, TYPE-048's window step): the engine dies under the window and the
window gets a new one without losing anything.

Case 1, every run:
  reconnect-1-stream    the run's app (/tmp/home/proj) is connected and sends
                        "Reconnect check: a long reply" (scripts/stream2m.json,
                        about 10 s); the reply streams.
  reconnect-2-kill      with "Reconnect check: kept while reconnecting" typed in
                        the field, kill -9 on connection.engine.pid: "Lost
                        connection to the Crush server — reconnecting…", a new
                        engine from ensure (another pid, fresh_engine true),
                        "Reconnected to the Crush server.", connected within
                        15 s; the field keeps its text the whole time.
  reconnect-3-reload    the chat reloads: the window has every finished message
                        the new engine has, none twice, in the engine's order.
  reconnect-4-send      the kept text sends, and its reply comes.
  reconnect-5-mid-send  TYPE-048: a prompt sent just as the engine is stopped is
                        not lost: after the rejoin it is in the engine's list,
                        back in the field, or shown pending in the chat.
Case 2, only with CHECK_CRUSH=$PWD/tests/fakes/crush-wrap (its ensure fails
while /tmp/fail-ensure exists):
  reconnect-6-failed    /tmp/fail-ensure made, kill -9: reconnecting, then after
                        three failed tries "Can't restore the connection to the
                        Crush server. Restart Crush to recover." with Try again;
                        the field keeps "Reconnect check: typed while failed"
                        and Enter sends nothing.
  reconnect-7-try-again the file removed, Try again: connected within 15 s,
                        "Reconnected to the Crush server.", the chat matches the
                        engine, nothing was sent while failed, and the kept text
                        then sends.
Fails when the state is not connected within 15 s, a text is missing, messages
are missing or doubled against the engine's list, or the field loses its text
or sends while the engine is gone. The replies are scripts/stream2m.json's
"Reconnect check: ..." turns, so every run streams the same (2.9 names script
basic, another piece's; its turns are not known here). A step that cannot run
prints NOT RUN and fails the check.

--stand-in plays the screens' part through the app's own commands (engine_open,
api, events_take, engine_retry), for builds without P3.3's stores: it checks
the Rust side's reconnect (its connection events, engine_unavailable while the
engine is gone) and this driver's steps; texts on screen are not checked.
Usage: tests/check.sh p3_reconnect.py [--stand-in]
       CHECK_CRUSH=$PWD/tests/fakes/crush-wrap tests/check.sh p3_reconnect.py [--stand-in]
"""
import importlib.util, os, signal, sys, threading, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, key, probe_heavy, step, type_text, wait_for


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
LOST = "Lost connection to the Crush server — reconnecting…"
FAILED = "Can't restore the connection to the Crush server. Restart Crush to recover."
BACK = "Reconnected to the Crush server."
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
        """One look at the window: state, attempt, engine pid, fresh_engine, its texts, the field."""
        p = self.app.report()
        c = p.get("connection") or {}
        return {"state": c.get("state"), "attempt": c.get("attempt"), "pid": (c.get("engine") or {}).get("pid"),
                "fresh": c.get("fresh_engine"), "text": pc.light_text(p), "field": (p.get("composer") or {}).get("value") or "",
                "at": time.time()}

    def states(self, samples):
        """The states the window went through. The probe writes every 200 ms, so a
        quick reconnect can fall between two writes; the rejoin's attempt (1 or
        more, 1.2.6) still says it happened."""
        seen = {s["state"] for s in samples if s["state"]}
        if samples and (samples[-1].get("attempt") or 0) >= 1:
            seen.add("reconnecting")
        return seen

    def shows(self, text, timeout=5):
        return pc.shows(self.app.wait(lambda p: pc.shows(p, text), timeout), text)

    def chat_shows(self, text):
        return pc.flat(text) in pc.light_text({"chat": self.app.section("chat")})

    def try_again(self, row):
        """The failed notice's Try again: a real click, found by its words in the
        heavy fields; when the probe has no rect for it, NOT RUN, and engine_retry
        (what the button calls) so the steps after it can still run."""
        rect = pc.find_rect(probe_heavy(self.probe, 20), pc.TRY_AGAIN)
        if rect and self.app.click(rect):
            return True
        pc.not_run(row, "the probe has no rect for the notice's \"Try again\" (1.9 pins none); "
                        "engine_retry stood in for the click so the later checks still run")
        self.app.invoke("engine_retry")
        return False


class StandInSide:
    """--stand-in: the screens' part through the app's own commands. The page's
    frame loop takes every batch at its doorbell, so the connection's states are
    read the way P3.1's own scratch check read them: an api call answers the
    engine's pid while connected and engine_unavailable while reconnecting or
    failed (with a message for each), and the app's log names each try."""

    def __init__(self, probe_path, out_dir):
        self.app = pc.AppRun.existing(probe_path, PROJ)
        self.log = os.path.join(out_dir, "app.log")
        self.mark = 0
        self.sid = None
        self.text = ""
        self.last_send = None

    def has_connection(self):
        return True

    def join(self, timeout=45):
        return bool(self.app.invoke("engine_open", {"cwd": PROJ}, timeout).get("ok"))

    def log_lines(self):
        """The app's log since the last mark (run-in-sandbox.sh saves it as app.log)."""
        try:
            with open(self.log, errors="replace") as f:
                return f.read().splitlines()[self.mark:]
        except OSError:
            return []

    def set_mark(self):
        self.mark += len(self.log_lines())

    def engine(self):
        """(pid, socket, workspace id) of the engine the window is on, or Nones while it has none."""
        a = self.api("GET", "/v1/engine")
        if not a.get("ok"):
            return None, None, None
        info = a.get("value") or {}
        sock = (info.get("address") or "").removeprefix("unix://")
        return info.get("pid"), sock, pc.workspace_for(seed_chat.Engine(sock), PROJ)

    def api(self, method, path, body=None):
        args = {"method": method, "path": path}
        if body is not None:
            args["body"] = body
        return self.app.invoke("api", args, 30)

    def prepare(self):
        a = self.api("POST", "sessions", {"title": "Reconnect check"})
        self.sid = (a.get("value") or {}).get("id")
        return bool(self.sid)

    def field(self):
        return self.text

    def clear(self):
        self.text = ""
        return True

    def wait_chat_id(self, timeout):
        return self.sid

    def type_only(self, text):
        self.text = text
        return True

    def enter(self):
        """The composer's send (TYPE-048): sent, the field empties; refused, it keeps the text."""
        self.last_send = self.api("POST", "agent", {"session_id": self.sid, "prompt": self.text})
        if self.last_send.get("ok"):
            self.text = ""

    def send(self, text):
        self.type_only(text)
        self.enter()
        return bool(self.last_send.get("ok"))

    def chat_id(self):
        return self.sid

    def messages(self):
        a = self.api("GET", f"sessions/{self.sid}/messages")
        return [{"id": m["id"], "role": m["role"], "finished": engine_finished(m), "text": engine_text(m)} for m in a.get("value") or []]

    def sample(self):
        a = self.api("GET", "/v1/engine")
        error = a.get("error") or {}
        if a.get("ok"):
            state = "connected"
        elif error.get("code") == "engine_unavailable":
            state = "failed" if "lost its connection" in (error.get("message") or "") else "reconnecting"
        else:
            state = error.get("code")
        return {"state": state, "attempt": None, "pid": (a.get("value") or {}).get("pid") if a.get("ok") else None,
                "fresh": None, "text": "", "field": self.text, "at": time.time()}

    def states(self, samples):
        """The states the calls answered, and reconnecting when the log says a
        reconnect began (a quick one can fall between two calls)."""
        seen = {s["state"] for s in samples if s["state"]}
        if any("lost the engine" in line and "reconnecting" in line for line in self.log_lines()):
            seen.add("reconnecting")
        return seen

    def failed_tries(self):
        """The reconnect tries the log names as failed since the mark: [(n, message)]."""
        tries = []
        for line in self.log_lines():
            head, sep, rest = line.partition("reconnect try ")
            if sep and " failed: " in rest:
                n, _, message = rest.partition(" failed: ")
                tries.append((int(n) if n.isdigit() else n, message))
        return tries

    def shows(self, text, timeout=5):
        return True  # texts on screen are the window's; not checked here

    def chat_shows(self, text):
        return False

    def try_again(self, row):
        self.app.invoke("engine_retry")
        return True


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
    pc.STAND_IN = stand_in = "--stand-in" in sys.argv[4:]
    side = StandInSide(probe_path, out_dir) if stand_in else WindowSide(probe_path)
    if stand_in:
        print("NOTE --stand-in: the screens' part is played through the app's commands; texts on screen are not checked", flush=True)
    print(f"NOTE the app runs {pc.CRUSH}" + ("; case 2 runs (ensure fails while /tmp/fail-ensure exists)" if WRAPPED else ""), flush=True)
    sid = None
    try:
        with step("reconnect-1-stream", out_dir):
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
            old_pid, old_wid = pid, wid
            if stand_in:
                side.set_mark()
            killed_at = time.time()
            check(kill_engine(old_pid), f"kill -9 on the engine (pid {old_pid})")
            back, samples = watch(side, lambda s: s["state"] == "connected" and s["pid"] not in (None, old_pid), LIMIT)
            took = time.time() - killed_at
            states = side.states(samples)
            check("reconnecting" in states, f"the window turns reconnecting (states seen: {sorted(states)})")
            if not stand_in:
                check(any(pc.flat(LOST) in s["text"] for s in samples) or side.shows(LOST, 1),
                      f"it shows \"{LOST}\"")
            new_pid = samples[-1]["pid"] if samples else None
            check(back, f"it is connected to a new engine within {LIMIT} s (pid {old_pid} -> {new_pid}, {took:.1f} s)")
            pid, sock, wid = side.engine()
            if stand_in:
                check(bool(wid) and wid != old_wid, f"the new engine gave the window a fresh workspace ({old_wid} -> {wid})")
            else:
                check(bool(samples) and samples[-1]["fresh"] is True, "the window knows the engine is a fresh one (fresh_engine)")
                check(side.shows(BACK), f"it shows \"{BACK}\"")
                kept = [s["field"] for s in samples if s["field"] != KEPT]
                check(not kept and side.field() == KEPT, f"the field kept its text the whole time ({kept[:3]})")

        with step("reconnect-3-reload", out_dir):
            agree, why = False, "no read"
            for _ in range(50):
                agree, why = compare(side.messages(), engine_messages(sock, wid, sid))
                if agree:
                    break
                time.sleep(0.2)
            check(agree, f"the chat has every finished message of the new engine, none twice, in order {why}")
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
            if stand_in:
                threading.Timer(0.1, kill_engine, [old_pid]).start()
                side.enter()
                answer = side.last_send or {}
                kind = "accepted" if answer.get("ok") else (answer.get("error") or {}).get("code")
                check(kind in ("accepted", "transport", "engine_unavailable"),
                      f"the send's answer is accepted, transport or engine_unavailable ({kind})")
            else:
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
            if stand_in:
                print(f"NOTE the prompt sent as the engine stopped ended up {where or 'nowhere the stand-in can see'} "
                      "(keeping it is the composer's job, TYPE-048)", flush=True)
            else:
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
            if stand_in:
                side.set_mark()
            check(kill_engine(old_pid), f"kill -9 on the engine (pid {old_pid}) while ensure fails")
            failed, samples = watch(side, lambda s: s["state"] == "failed", LIMIT)
            states = side.states(samples)
            check(failed and "reconnecting" in states, f"the window reconnects, then fails (states seen: {sorted(states)})")
            if stand_in:
                tries = side.failed_tries()
                check([n for n, _ in tries] == [1, 2, 3], f"after three failed tries ({[n for n, _ in tries]})")
                check(bool(tries) and all(m.endswith("(start_failed)") for _, m in tries),
                      f"each with ensure's error ({tries[-1][1] if tries else None})")
            else:
                check(side.shows(FAILED) and side.shows(pc.TRY_AGAIN), f"it shows \"{FAILED}\" and Try again")
            side.enter()
            if stand_in:
                code = ((side.last_send or {}).get("error") or {}).get("code")
                check(code == "engine_unavailable", f"a send while failed is refused at once, nothing sent ({code})")
            else:
                time.sleep(2)
            check(side.field() == TYPED, "the field keeps its text and Enter sends nothing")

        with step("reconnect-7-try-again", out_dir):
            os.remove(FAIL_FILE)
            pressed_at = time.time()
            side.try_again("reconnect-7-try-again")
            back, samples = watch(side, lambda s: s["state"] == "connected" and s["pid"] not in (None, old_pid), LIMIT)
            check(back, f"Try again connects within {LIMIT} s ({time.time() - pressed_at:.1f} s, pid {samples[-1]['pid'] if samples else None})")
            if not stand_in:
                check(side.shows(BACK), f"it shows \"{BACK}\"")
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
    pc.check_all_ran()
    finish()
