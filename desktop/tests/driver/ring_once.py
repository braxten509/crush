"""One ring for a chat in a folder no window shows (owner, 2026-10-20: "if
console and desktop are open, please only ring ONCE - from the desktop
one"). In the sandbox with the real engine: the window shows
/tmp/home/proj; this driver plays a terminal in /tmp/home/other (its own
engine, an attached event stream, the chat listed open, signed in as a
terminal app) and runs one turn there. Then:
- the desktop lists both folders as heard in the shared open-chats file
  (the terminal there stays quiet: internal/openchats DesktopHears);
- the desktop's listener holds no claim on the other engine (GET
  /v1/engine still counts one app);
- the finished turn rings once, from the desktop (the probe's
  alerts.jsonl gets one "complete" from the watch for that folder).

Usage: tests/check.sh ring_once.py
"""
import importlib.util, json, os, sys, time, uuid

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import harness as H
from harness import check, finish, wait_for

ROOT = os.path.dirname(os.path.dirname(HERE))


def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


seed_chat = load("seed_chat", os.path.join(ROOT, "tests", "fakes", "seed_chat.py"))
probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
CRUSH = os.environ.get("DESKTOP_CRUSH") or os.path.join(ROOT, "tests", "out", "bin", "crush")
PROJ = "/tmp/home/proj"
OTHER = "/tmp/home/other"
ALERTS = probe_path + ".alerts.jsonl"


def make_project(path):
    """A copy of tests/project with the fake AI's crush.json, as run-in-sandbox.sh makes /tmp/home/proj."""
    import shutil, subprocess
    if not os.path.exists(path):
        shutil.copytree(os.path.join(ROOT, "tests", "project"), path, ignore=shutil.ignore_patterns("crush.json.in"))
        with open(os.path.join(ROOT, "tests", "project", "crush.json.in")) as f:
            config = json.loads(f.read().replace("@PORT@", str(port)))
        # Test projects ring for nothing; this one rings, which a probe run
        # records in alerts.jsonl instead of playing.
        config.setdefault("options", {})["notifications"] = "auto"
        with open(os.path.join(path, "crush.json"), "w") as f:
            json.dump(config, f)
        subprocess.run(["git", "-C", path, "init", "-q"], check=True)


def heard():
    try:
        with open(H.OPEN_CHATS) as f:
            apps = json.load(f).get("apps") or []
    except (OSError, ValueError):
        return []
    return [d for a in apps if a.get("kind") == "desktop" for d in a.get("dirs") or []]


def rings():
    try:
        with open(ALERTS) as f:
            return [r for r in (json.loads(line) for line in f if line.strip()) if r.get("kind") == "sound"]
    except OSError:
        return []


def until(test, timeout):
    end = time.time() + timeout
    while time.time() < end:
        if test():
            return True
        time.sleep(0.2)
    return test()


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 60)
make_project(OTHER)
engine = seed_chat.Engine(seed_chat.ensure(CRUSH, OTHER))
client = str(uuid.uuid4())
env = ["%s=%s" % kv for kv in os.environ.items()]
ws = engine.call("POST", "/v1/workspaces", {"path": OTHER, "data_dir": "", "version": "ring_once", "protocol": seed_chat.PROTOCOL,
                                            "client_id": client, "yolo": True, "env": env})
wid = ws["id"]
del ws  # holds the environment; never print it
q = "?client_id=" + client
base = "/v1/workspaces/" + wid
events = engine.follow_events(base + "/events" + q)
try:
    engine.call("POST", base + "/agent/init" + q, {"interactive": True}, timeout=120)
    sid = engine.call("POST", base + "/sessions" + q, {"title": "Other folder chat"})["id"]
    H.list_open_chat({"id": sid, "dir": OTHER, "title": "Other folder chat"}, as_app=True)

    ok = until(lambda: OTHER in heard() and PROJ in heard(), 20)
    check(ok, f"the desktop hears the other folder and its own ({heard()})")
    apps = (engine.call("GET", "/v1/engine") or {}).get("apps")
    check(apps == 1, f"listening holds no claim on the other engine (apps {apps}, want 1)")

    before = len(rings())
    count = engine.call("GET", "%s/sessions/%s" % (base, sid))["message_count"]
    engine.call("POST", base + "/agent" + q, {"session_id": sid, "prompt": "Say hello from the other folder"})
    seed_chat.wait_turn(engine, base, sid, count)
    until(lambda: len(rings()) > before, 15)
    time.sleep(2)
    new = rings()[before:]
    mine = [r for r in new if r.get("from") == "watch" and r.get("dir") == OTHER and r.get("sound") == "complete" and r.get("session") == sid]
    check(len(mine) == 1 and len(new) == 1, f"the finished turn rings once, from the desktop ({new})")
finally:
    try:
        engine.call("DELETE", "/v1/clients/" + client)
    except Exception:
        pass
    events.close()
finish()
