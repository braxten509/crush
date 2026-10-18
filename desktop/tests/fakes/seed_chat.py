#!/usr/bin/env python3
"""Seeds a chat through the engine's API (desktop-design 6.2), so drivers get
long chats without typing them: p3_speed.py's 300 items, p3_long.py's 1,000.

Usage:
  seed_chat.py --cwd DIR (--items N | --turns N) [--session SID] [--title T]
               [--crush CRUSH | --socket SOCK]

It finds the project's engine with `CRUSH engine ensure --cwd DIR` (CRUSH is
--crush, else $DESKTOP_CRUSH, else crush on PATH; --socket skips this), joins
the project as its own client (the API calls the desktop app makes,
engine-design.md:2809-2818, with yolo on and no channels, so it joins a
workspace the app already has), creates a chat (or adds to --session), and
sends the turns one after another, each once the last has finished. The
prompts are "Seed turn short|medium|code|list|tool <n>" in that order, which
scripts/stream2m.json answers at once with no delays: a sentence, a long
paragraph, a code fence, a list, and a turn that views auth/session.go. An
item is one message in the engine's list (the user's prompt, each reply, each
tool result); --items N sends turns until the chat has at least N. --title
renames the chat afterwards. When it leaves, the engine may close the project
if no other client holds it (the chat stays saved); a driver that checks the
engine afterwards keeps its own client attached meanwhile, as the app does.

It prints one JSON line: {"session_id", "workspace_id", "turns", "messages",
"seconds"}, and exits 1 with a message on stderr when anything fails.

The engine is reached over its unix socket with Python's http.client (what
`curl --unix-socket` does). The workspace object the engine returns holds the
login environment, so it is never printed.
"""
import argparse, http.client, json, os, socket, subprocess, sys, threading, time, uuid

KINDS = ["short", "medium", "code", "list", "tool"]
PROTOCOL = 1


class ApiError(Exception):
    pass


class UnixConnection(http.client.HTTPConnection):
    def __init__(self, path, timeout):
        super().__init__("localhost", timeout=timeout)
        self.unix_path = path

    def connect(self):
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.settimeout(self.timeout)
        sock.connect(self.unix_path)
        self.sock = sock


class Engine:
    def __init__(self, sock):
        self.sock = sock

    def call(self, method, path, body=None, timeout=30):
        conn = UnixConnection(self.sock, timeout)
        try:
            data = None if body is None else json.dumps(body).encode()
            conn.request(method, path, body=data, headers={"Content-Type": "application/json"} if data else {})
            resp = conn.getresponse()
            raw = resp.read()
        finally:
            conn.close()
        if resp.status >= 400:
            try:
                err = json.loads(raw)
                detail = "%s %s" % (err.get("code", ""), err.get("message", ""))
            except ValueError:
                detail = raw[:200].decode("utf-8", "replace")
            raise ApiError("%s %s: %d %s" % (method, path.split("?")[0], resp.status, detail.strip()))
        return json.loads(raw) if raw.strip() else None

    def follow_events(self, path):
        """Keeps the event stream open in the background (an attached client
        keeps the workspace alive); returns the socket to close."""
        sock = socket.socket(socket.AF_UNIX, socket.SOCK_STREAM)
        sock.connect(self.sock)
        sock.sendall(("GET %s HTTP/1.1\r\nHost: localhost\r\nAccept: text/event-stream\r\n\r\n" % path).encode())

        def drain():
            try:
                while sock.recv(65536):
                    pass
            except OSError:
                pass
        threading.Thread(target=drain, daemon=True).start()
        return sock


def ensure(crush, cwd):
    proc = subprocess.run([crush, "engine", "ensure", "--cwd", cwd], capture_output=True, text=True, timeout=60)
    try:
        out = json.loads(proc.stdout)
    except ValueError:
        raise ApiError("crush engine ensure printed no JSON (exit %d): %s" % (proc.returncode, proc.stderr.strip()[-300:]))
    if proc.returncode != 0 or "socket" not in out:
        raise ApiError("crush engine ensure failed (exit %d): %s" % (proc.returncode, (out.get("error") or {}).get("message", "")))
    return out["socket"]


def seed(opts):
    sock = opts.socket or ensure(opts.crush or os.environ.get("DESKTOP_CRUSH") or "crush", opts.cwd)
    engine = Engine(sock)
    info = engine.call("GET", "/v1/version")
    if (info or {}).get("protocol") != PROTOCOL:
        raise ApiError("engine protocol %r, want %d" % ((info or {}).get("protocol"), PROTOCOL))
    client = str(uuid.uuid4())
    env = ["%s=%s" % kv for kv in os.environ.items()]
    ws = engine.call("POST", "/v1/workspaces", {"path": opts.cwd, "data_dir": "", "version": "seed_chat", "protocol": PROTOCOL,
                                                "client_id": client, "yolo": True, "env": env})
    wid = ws["id"]
    del ws  # holds the environment; never print it
    q = "?client_id=" + client
    base = "/v1/workspaces/" + wid
    events = engine.follow_events(base + "/events" + q)
    try:
        engine.call("POST", base + "/agent/init" + q, {"interactive": True}, timeout=120)
        sid = opts.session or engine.call("POST", base + "/sessions" + q, {"title": opts.title or "Seeded chat"})["id"]
        started = time.time()
        turns = 0
        count = engine.call("GET", "%s/sessions/%s" % (base, sid))["message_count"]
        while (opts.turns is not None and turns < opts.turns) or (opts.items is not None and count < opts.items):
            kind = KINDS[turns % len(KINDS)]
            engine.call("POST", base + "/agent" + q, {"session_id": sid, "prompt": "Seed turn %s %d" % (kind, turns + 1)})
            turns += 1
            count = wait_turn(engine, base, sid, count)
        if opts.title:
            sess = engine.call("GET", "%s/sessions/%s" % (base, sid))
            sess["title"] = opts.title
            engine.call("PUT", "%s/sessions/%s" % (base, sid) + q, sess)
        messages = len(engine.call("GET", "%s/sessions/%s/messages" % (base, sid)))
        return {"session_id": sid, "workspace_id": wid, "turns": turns, "messages": messages,
                "seconds": round(time.time() - started, 1)}
    finally:
        try:
            engine.call("DELETE", "/v1/clients/" + client)
        except (ApiError, OSError):
            pass
        events.close()


def wait_turn(engine, base, sid, before, timeout=60):
    """Waits until the turn just sent has finished: a prompt and a reply more
    than before, and the chat no longer busy. Returns the new message count."""
    deadline = time.time() + timeout
    while time.time() < deadline:
        sess = engine.call("GET", "%s/sessions/%s" % (base, sid))
        if sess["message_count"] >= before + 2 and not sess["is_busy"]:
            return sess["message_count"]
        time.sleep(0.03)
    raise ApiError("a seeded turn did not finish within %d s (messages %d, busy %s)" % (timeout, sess["message_count"], sess["is_busy"]))


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--cwd", required=True)
    ap.add_argument("--items", type=int)
    ap.add_argument("--turns", type=int)
    ap.add_argument("--session")
    ap.add_argument("--title")
    ap.add_argument("--crush")
    ap.add_argument("--socket")
    opts = ap.parse_args()
    if (opts.items is None) == (opts.turns is None):
        ap.error("give exactly one of --items and --turns")
    opts.cwd = os.path.abspath(opts.cwd)
    try:
        print(json.dumps(seed(opts)))
    except (ApiError, OSError, KeyError, subprocess.TimeoutExpired) as e:
        print("seed_chat: %s" % e, file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
