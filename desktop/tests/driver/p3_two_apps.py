"""Two apps on one chat (desktop-design 2.9, the phase 3 done-line,
PLAN.md:65): the desktop and the terminal app on the same chat through the
project's engine, its nine steps, then LIST-012's step.

  1. The terminal app is the worktree's build (DESKTOP_CRUSH), the one the
     desktop's engine runs.
  2. The desktop sends "first from desktop" on /tmp/home/proj.
  3. The terminal app starts in a hidden tmux on that chat (6.4).
  4. Within 5 s its pane shows the prompt and the reply's first words.
  5. The terminal sends "second from terminal"; within 5 s the desktop has the
     prompt and its streaming reply, in the engine's order (the ground truth:
     GET sessions/{sid}/messages over the engine's socket).
  6. While it streams the desktop's last item grows and it shows busy; the
     pane shows the terminal's own busy text.
  7. The desktop sends "third from desktop": both apps and the engine hold it
     in the queue, not in the chat (TYPE-044).
  8. The desktop window closes; the terminal's turn finishes, then the held
     prompt runs.
  9. LIST-012: the desktop starts again with -s on the chat; the terminal
     starts a new chat (Ctrl+N) and sends "from the terminal": the desktop's
     list gains it, "Working…" within 1 s of the first streamed words in the
     pane, and no state within 1 s of the reply's end.
Fails when a wait times out, the apps and the engine disagree on order or
text, the desktop shows a message twice, or step 8's reply stops. The replies
are scripts/stream2m.json's two-app turns; "basic" is made the active script
when it exists, as the design says.

--stand-in plays the desktop's part with the engine API calls the desktop
makes (join, send, read), for running the terminal and engine half before the
window's pieces are merged; it proves nothing about the window.
Usage: tests/check.sh p3_two_apps.py [--stand-in]
"""
import importlib.util, json, os, signal, subprocess, sys, time, uuid
from urllib import request as urlrequest
from urllib.error import HTTPError, URLError

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, read_probe, step, type_text, wait_for

probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
STAND_IN = "--stand-in" in sys.argv[4:]
PROJ = "/tmp/home/proj"
ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
CRUSH = os.environ.get("DESKTOP_CRUSH") or os.path.join(ROOT, "tests", "out", "bin", "crush")
APP = os.environ.get("DESKTOP_APP") or os.path.join(ROOT, "src-tauri", "target", "release", "crush-desktop")
TMUX = ["tmux", "-L", "crushtest", "-f", "/dev/null"]
# The terminal's own busy status: the live reply's status line
# (internal/ui/chat/assistant_render.go:185-193, toolgroup.go:293-369). Its
# typing box's busy placeholders (editor.go workingPlaceholders) never show in
# YOLO mode, which says "Go crazy" instead (composer.go:81-97).
LIVE = ("Responding", "Thinking", "Writing command")


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


seed_chat = load("seed_chat", os.path.join(ROOT, "tests", "fakes", "seed_chat.py"))


def script_reply(when):
    with open(os.path.join(ROOT, "tests", "fakes", "scripts", "stream2m.json")) as f:
        for turn in json.load(f)["turns"]:
            if turn["when"] == when:
                return "".join(s.get("text", "") for s in turn["steps"])
    raise SystemExit(f"stream2m.json has no turn {when!r}")


def flat(text):
    return " ".join((text or "").split())


def words(text, n):
    return " ".join(text.split()[:n])


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


def engine_text(message):
    return "".join((p.get("data") or {}).get("text", "") for p in message.get("parts") or [] if p.get("type") == "text")


def engine_finished(message):
    return any(p.get("type") == "finish" for p in message.get("parts") or [])


class EngineView:
    """The engine's own answers over its socket: the ground truth."""

    def __init__(self, sock, wid=None):
        self.api = seed_chat.Engine(sock)
        if not wid:
            # Only the id of the folder's workspace is kept: the objects hold
            # the login environment and are never printed.
            wid = next((w["id"] for w in self.api.call("GET", "/v1/workspaces") if w.get("path") == PROJ), None)
        self.wid = wid

    def get(self, path):
        return self.api.call("GET", f"/v1/workspaces/{self.wid}/{path}")

    def post(self, path, body=None):
        return self.api.call("POST", f"/v1/workspaces/{self.wid}/{path}", body)

    def messages(self, sid):
        return [{"id": m["id"], "role": m["role"], "finished": engine_finished(m), "text": engine_text(m)}
                for m in self.get(f"sessions/{sid}/messages")]

    def queued(self, sid):
        return self.get(f"agent/sessions/{sid}/prompts/list") or []

    def needs_init(self):
        return bool((self.get("project/needs-init") or {}).get("needs_init"))

    def sessions(self):
        return self.get("sessions") or []


class Window:
    """The real desktop app, read through its probe and driven with the real keyboard."""

    def __init__(self):
        self.proc = None

    def report(self):
        return read_probe(probe_path, tries=3) or {}

    def section(self, name):
        return self.report().get(name) or {}

    def wait_connected(self, timeout, since=0.0):
        p = wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected"
                     and p.get("at", 0) >= since * 1000, timeout)
        return (p.get("connection") or {}).get("state") == "connected"

    def engine_version(self):
        return self.section("connection").get("engine_version")

    def engine(self):
        conn = self.section("connection")
        sock = (conn.get("engine") or {}).get("socket") or conn.get("socket") or seed_chat.ensure(CRUSH, PROJ)
        return EngineView(sock, conn.get("workspace_id"))

    def dismiss_init(self, engine):
        """START-002: a fresh project asks first; n answers No."""
        for _ in range(20):
            if not engine.needs_init():
                break
            key("n")
            time.sleep(0.5)
        value = self.section("composer").get("value") or ""
        if value and set(value) == {"n"}:  # an n that reached the typing box
            key(*["BackSpace"] * len(value))
        return not engine.needs_init()

    def focus_composer(self):
        for _ in range(4):
            if self.section("composer").get("focused"):
                return True
            key("Tab")
            time.sleep(0.3)
        return bool(self.section("composer").get("focused"))

    def send(self, text):
        if not self.focus_composer():
            return False
        type_text(text)
        typed = wait_for(probe_path, lambda p: (p.get("composer") or {}).get("value") == text, 5)
        if (typed.get("composer") or {}).get("value") != text:
            return False
        key("Return")
        return True

    def chat_id(self):
        return self.section("chat").get("id")

    def messages(self):
        return self.section("chat").get("messages") or []

    def busy(self):
        return bool(self.section("chat").get("busy"))

    def queued(self):
        return [t.get("text", "") for t in self.section("composer").get("tray") or []]

    def rows(self):
        return self.section("list").get("rows") or []

    def close(self):
        """Closes the window the way its close button does, then waits for the app to exit."""
        found = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^Crush$"], capture_output=True, text=True).stdout.split()
        pids = set()
        for wid in found:
            pid = subprocess.run(["xdotool", "getwindowpid", wid], capture_output=True, text=True).stdout.strip()
            if pid.isdigit():
                pids.add(int(pid))
            subprocess.run(["xdotool", "windowquit", wid], capture_output=True)
        gone, _, _ = poll(lambda: [p for p in pids if os.path.exists(f"/proc/{p}")], lambda alive: not alive, 10)
        return bool(pids) and gone

    def relaunch(self, sid):
        since = time.time()
        env = dict(os.environ, CRUSH_DESKTOP_CRUSH=CRUSH)
        log = open(os.path.join(out_dir, "app-again.log"), "w")
        self.proc = subprocess.Popen([APP, "--probe", probe_path, "--cwd", PROJ, "-s", sid], env=env, stdout=log, stderr=subprocess.STDOUT)
        return since

    def stop(self):
        if self.proc and self.proc.poll() is None:
            self.proc.terminate()
            try:
                self.proc.wait(5)
            except subprocess.TimeoutExpired:
                self.proc.kill()


class StandIn:
    """--stand-in: the desktop's engine calls without its window."""

    def __init__(self):
        self.view = None
        self.client = None
        self.events = None
        self.sid = None

    def join(self):
        sock = seed_chat.ensure(CRUSH, PROJ)
        api = seed_chat.Engine(sock)
        self.client = str(uuid.uuid4())
        ws = api.call("POST", "/v1/workspaces", {"path": PROJ, "data_dir": "", "version": "stand-in", "protocol": 1,
                                                 "client_id": self.client, "yolo": True, "env": ["%s=%s" % kv for kv in os.environ.items()]})
        self.view = EngineView(sock, ws["id"])
        del ws  # holds the environment; never print it
        self.events = api.follow_events(f"/v1/workspaces/{self.view.wid}/events?client_id={self.client}")
        self.view.post(f"agent/init?client_id={self.client}", {"interactive": True})

    def wait_connected(self, timeout, since=0.0):
        self.join()
        return True

    def engine_version(self):
        return self.view.api.call("GET", "/v1/version").get("version")

    def engine(self):
        return EngineView(self.view.api.sock, self.view.wid)

    def dismiss_init(self, engine):
        if engine.needs_init():
            engine.post(f"project/init?client_id={self.client}")
        return not engine.needs_init()

    def send(self, text):
        if not self.sid:
            self.sid = self.view.post(f"sessions?client_id={self.client}", {"title": "New Session"})["id"]
        self.view.post(f"agent?client_id={self.client}", {"session_id": self.sid, "prompt": text})
        return True

    def chat_id(self):
        return self.sid

    def messages(self):
        return self.view.messages(self.sid) if self.sid else []

    def busy(self):
        return bool(self.view.get(f"agent/sessions/{self.sid}").get("is_busy"))

    def queued(self):
        return self.view.queued(self.sid)

    def rows(self):
        return [{"id": s["id"], "title": s.get("title"), "state": "Working…" if s.get("is_busy") else ""} for s in self.view.sessions()]

    def close(self):
        try:
            self.view.api.call("DELETE", "/v1/clients/" + self.client)
        finally:
            self.events.close()
        return True

    def relaunch(self, sid):
        since = time.time()
        self.join()
        self.sid = sid
        return since

    def stop(self):
        if self.events:
            self.events.close()


def tmux(*args):
    return subprocess.run(TMUX + list(args), capture_output=True, text=True)


def pane():
    r = tmux("capture-pane", "-p", "-t", "tui")
    return r.stdout if r.returncode == 0 else ""


def has(text, snippet):
    return flat(snippet) in flat(text)


def save_pane(name):
    with open(os.path.join(out_dir, f"{name}.pane.txt"), "w") as f:
        f.write(pane())


def in_order(text, snippets):
    """Whether the snippets that are on screen come in this order."""
    places = [flat(text).find(flat(s)) for s in snippets]
    seen = [p for p in places if p >= 0]
    return seen == sorted(seen)


def user_texts(messages):
    return [flat(m.get("text")) for m in messages if m.get("role") == "user"]


def last_assistant_after(messages, prompt):
    """The first assistant message after the user message prompt, or None."""
    after = False
    for m in messages:
        if m.get("role") == "user":
            after = flat(m.get("text")) == prompt
        elif after and m.get("role") == "assistant":
            return m
    return None


def same_order(desktop, engine):
    """The desktop's messages and the engine's: the same ids in the same order
    (one may be a message ahead while a reply streams), the same text for every
    finished message, and no id twice."""
    dids, eids = [m["id"] for m in desktop], [m["id"] for m in engine]
    if len(set(dids)) != len(dids):
        return False, "the desktop shows a message twice"
    n = min(len(dids), len(eids))
    if dids[:n] != eids[:n] or abs(len(dids) - len(eids)) > 1:
        return False, f"order differs: desktop {dids} engine {eids}"
    etext = {m["id"]: m for m in engine}
    for m in desktop:
        e = etext.get(m["id"])
        if e and m.get("finished") and e["finished"] and flat(m.get("text")) != flat(e["text"]):
            return False, f"text differs for {m['id']}"
    return True, ""


def set_active_script(name):
    req = urlrequest.Request(f"http://127.0.0.1:{port}/_fake/control", data=json.dumps({"script": name}).encode(),
                             method="POST", headers={"Content-Type": "application/json"})
    try:
        urlrequest.urlopen(req, timeout=5).read()
        return True
    except (HTTPError, URLError):
        return False


def main():
    first, second, third, terminal = (flat(script_reply(w)) for w in
                                      ("first from desktop", "second from terminal", "third from desktop", "from the terminal"))
    if set_active_script("basic"):
        print("NOTE the fake server plays script basic first", flush=True)
    desktop = StandIn() if STAND_IN else Window()
    if STAND_IN:
        print("NOTE --stand-in: the desktop's part is played through the engine API; the window is not checked", flush=True)
    engine = None
    connected = False
    sid = None
    tmux("kill-server")
    try:
        with step("two-apps-1-build", out_dir):
            out = subprocess.run([CRUSH, "engine", "version"], capture_output=True, text=True, timeout=30)
            version = json.loads(out.stdout or "{}").get("version")
            check(os.access(CRUSH, os.X_OK) and bool(version), f"the terminal app is the worktree's build {CRUSH} ({version})")
            connected = check(desktop.wait_connected(45), "the desktop joined the project's engine")
            if connected:
                check(desktop.engine_version() == version, f"the desktop's engine runs that build ({desktop.engine_version()})")
                engine = desktop.engine()
                check(bool(engine.wid), "the engine has the folder's workspace")
        if not connected:
            check(False, "the remaining steps need the desktop on the engine; stopping here")
            return

        with step("two-apps-2-first", out_dir):
            check(desktop.dismiss_init(engine), "the project's init prompt is answered (No)")
            check(desktop.send("first from desktop"), "the desktop sends the first prompt")
            ok, sid, _ = poll(desktop.chat_id, bool, 15)
            check(ok, f"the desktop is on the new chat ({sid})")
            ok, msgs, took = poll(desktop.messages, lambda ms: "first from desktop" in user_texts(ms) and any(
                m.get("role") == "assistant" and m.get("finished") and flat(m.get("text")) == first for m in ms), 20)
            check(ok, f"the desktop shows the first prompt and its whole reply ({took:.1f} s)")

        with step("two-apps-4-terminal", out_dir):
            started = tmux("new-session", "-d", "-s", "tui", "-x", "200", "-y", "50",
                           f"env CRUSH_CLIENT_SERVER=1 {CRUSH} --cwd {PROJ} -s {sid}")
            check(started.returncode == 0, f"the terminal app starts in a hidden tmux ({started.stderr.strip()})")
            ok, text, took = poll(pane, lambda p: has(p, "first from desktop") and has(p, words(first, 3)), 5)
            save_pane("two-apps-4-terminal")
            check(ok, f"within 5 s the terminal shows the first prompt and its reply's first words ({took:.1f} s)")

        with step("two-apps-5-second", out_dir):
            tmux("send-keys", "-t", "tui", "-l", "second from terminal")
            tmux("send-keys", "-t", "tui", "Enter")
            ok, msgs, took = poll(desktop.messages, lambda ms: "second from terminal" in user_texts(ms) and bool(
                (last_assistant_after(ms, "second from terminal") or {}).get("text")), 5)
            check(ok, f"within 5 s the desktop shows the terminal's prompt and its streaming reply ({took:.1f} s)")
            agree, why = same_order(desktop.messages(), engine.messages(sid))
            check(agree, f"the desktop's messages match the engine's order and text {why}")
            check(in_order(pane(), ["first from desktop", "second from terminal"]), "the terminal shows them in the same order")

        with step("two-apps-6-streaming", out_dir):
            grow = lambda: len((last_assistant_after(desktop.messages(), "second from terminal") or {}).get("text") or "")
            a = grow()
            time.sleep(0.5)
            b = grow()
            check(b > a, f"the desktop's last item grows while the terminal's reply streams ({a} then {b} characters)")
            check(desktop.busy(), "the desktop shows the chat busy")
            ok, _, _ = poll(pane, lambda p: any(w in p for w in LIVE), 3)
            save_pane("two-apps-6-streaming")
            check(ok, "the terminal shows its own busy status (\"Responding\")")

        with step("two-apps-7-held", out_dir):
            check(desktop.send("third from desktop"), "the desktop sends a third prompt while the terminal's turn runs")
            ok, _, _ = poll(desktop.queued, lambda q: any("third from desktop" in t for t in q), 5)
            check(ok and "third from desktop" not in user_texts(desktop.messages()),
                  "the desktop holds it in its queue, not in the chat (TYPE-044)")
            ok, _, _ = poll(pane, lambda p: has(p, "third from desktop") and has(p, "waiting · sends next"), 5)
            save_pane("two-apps-7-held")
            check(ok, "the terminal shows it held: \"waiting · sends next\"")
            check(any("third from desktop" in t for t in engine.queued(sid)), "the engine has it queued")
            reply = last_assistant_after(engine.messages(sid), "second from terminal") or {}
            check(not reply.get("finished"), "the terminal's turn was still running")

        with step("two-apps-8-close", out_dir):
            check(desktop.close(), "the desktop window closes")
            done = lambda ms: (last_assistant_after(ms, "second from terminal") or {})
            ok, msgs, took = poll(lambda: engine.messages(sid), lambda ms: done(ms).get("finished") and flat(done(ms).get("text")) == second, 40, 0.3)
            check(ok, f"the terminal's turn kept going and finished with its whole reply ({took:.1f} s)")
            ok, _, _ = poll(pane, lambda p: has(p, "End of the second reply."), 10)
            check(ok, "the terminal shows the reply's end")
            ok, msgs, _ = poll(lambda: engine.messages(sid), lambda ms: flat((last_assistant_after(ms, "third from desktop") or {}).get("text")) == third
                               and (last_assistant_after(ms, "third from desktop") or {}).get("finished"), 30, 0.3)
            check(ok and user_texts(msgs).count("third from desktop") == 1,
                  "the held prompt joined the chat once its turn started, and was answered")
            ok, _, _ = poll(pane, lambda p: has(p, words(third, 3)) and not any(w in p for w in LIVE), 5)
            save_pane("two-apps-8-close")
            check(ok, "the terminal shows the held prompt's reply and its busy status is gone")

        with step("LIST-012", out_dir):
            since = desktop.relaunch(sid)
            check(desktop.wait_connected(45, since), "the desktop starts again")
            ok, _, _ = poll(desktop.chat_id, lambda c: c == sid, 15)
            check(ok, "it opens the chat given with -s")
            before = {r.get("id") for r in desktop.rows()}
            tmux("send-keys", "-t", "tui", "C-n")
            time.sleep(1)
            tmux("send-keys", "-t", "tui", "-l", "from the terminal")
            tmux("send-keys", "-t", "tui", "Enter")
            t_first = t_working = t_end = t_idle = None
            new_id = None
            deadline = time.time() + 45
            while time.time() < deadline and t_idle is None:
                now = time.time()
                if t_first is None and has(pane(), words(terminal, 3)):
                    t_first = now
                rows = [r for r in desktop.rows() if r.get("id") not in before]
                if rows:
                    new_id = rows[0].get("id")
                    working = "Working…" in (rows[0].get("state") or "")
                    if working and t_working is None:
                        t_working = now
                    if t_end is None and new_id:
                        reply = last_assistant_after(engine.messages(new_id), "from the terminal") or {}
                        if reply.get("finished"):
                            t_end = now
                    if t_end is not None and not working:
                        t_idle = now
                time.sleep(0.1)
            save_pane("LIST-012")
            check(new_id is not None, "the desktop's list gains the terminal's new chat")
            check(t_first is not None and t_working is not None and t_working <= t_first + 1.0,
                  f"its row shows \"Working…\" within 1 s of the first streamed words in the pane "
                  f"({None if t_first is None or t_working is None else round(t_working - t_first, 2)} s)")
            check(t_end is not None and t_idle is not None and t_idle <= t_end + 1.0,
                  f"and no state within 1 s of the reply's end ({None if t_end is None or t_idle is None else round(t_idle - t_end, 2)} s)")
    finally:
        tmux("kill-server")
        desktop.stop()


if __name__ == "__main__":
    main()
    finish()
