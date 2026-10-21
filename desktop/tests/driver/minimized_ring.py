"""A minimized window still rings when its chat finishes (owner, 2026-10-21:
"When minimized I don't hear the notifications"). A minimized window draws
no frames, and the page took engine events only on a frame, so the finished
alert waited until the window was opened again. On the private hidden KWin
with the real engine: the window shows a seeded chat, KWin minimizes it, a
turn runs in that chat through the engine (the page's frames stopped, as
the real KWin stops them), and while the window stays
minimized the page's finished alert must go out (the probe's alerts.jsonl
gets "Crush finished"; the same raise() plays the sound just
before it).

Usage: tests/check.sh --no-app --kwin minimized_ring.py
"""
import importlib.util, os, sys, time, uuid

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import apps
from harness import check, finish, probe_eval
from kwin import app_window, kwin_eval

ROOT = os.path.dirname(os.path.dirname(HERE))
spec = importlib.util.spec_from_file_location("seed_chat", os.path.join(ROOT, "tests", "fakes", "seed_chat.py"))
seed_chat = importlib.util.module_from_spec(spec)
spec.loader.exec_module(seed_chat)

CRUSH = os.environ.get("DESKTOP_CRUSH") or os.path.join(ROOT, "tests", "out", "bin", "crush")
TITLE = "Minimized chat"

# Test projects ring for nothing; this one rings, which a probe run records.
apps.config({"options": {"notifications": "auto"}}, apps.PROJ)

engine = seed_chat.Engine(seed_chat.ensure(CRUSH, apps.PROJ))
client = str(uuid.uuid4())
env = ["%s=%s" % kv for kv in os.environ.items()]
ws = engine.call("POST", "/v1/workspaces", {"path": apps.PROJ, "data_dir": "", "version": "minimized_ring",
                                            "protocol": seed_chat.PROTOCOL, "client_id": client, "yolo": True, "env": env})
q = "?client_id=" + client
base = "/v1/workspaces/" + ws["id"]
del ws  # holds the environment; never print it
events = engine.follow_events(base + "/events" + q)
app = None
try:
    engine.call("POST", base + "/agent/init" + q, {"interactive": True}, timeout=120)
    sid = engine.call("POST", base + "/sessions" + q, {"title": TITLE})["id"]

    app = apps.App(args=("-s", sid))
    app.wait_window(lambda p: (p.get("connection") or {}).get("state") == "connected", timeout=60)
    check(app_window() is not None, "the window shows on the hidden KWin")
    time.sleep(1)

    gone = kwin_eval("""
      const w = workspace.windowList().find((w) => w.resourceClass == "crush-desktop" && w.normalWindow);
      w.minimized = true;
      return w.minimized;
    """)
    check(gone is True, "KWin minimizes the window")
    # The owner's KWin sends a minimized window no frame callbacks, so its
    # page gets no animation frames; this hidden KWin still does (91 in
    # 1.5 s, measured 2026-10-21) and the page stays "visible". The page's
    # frames stop here instead, as on the real desktop.
    stopped = probe_eval(apps.PROBE, "window.requestAnimationFrame = () => 0; window.__probeEval = true;")
    check(stopped is True, "the page gets no more animation frames")
    time.sleep(2)

    def finished():
        return [a for a in apps.alerts() if a.get("kind") == "notify" and a.get("title") == "Crush finished"]

    before = len(finished())
    count = engine.call("GET", "%s/sessions/%s" % (base, sid))["message_count"]
    engine.call("POST", base + "/agent" + q, {"session_id": sid, "prompt": "Say hello while minimized"})
    seed_chat.wait_turn(engine, base, sid, count)
    end = time.time() + 10
    while time.time() < end and len(finished()) == before:
        time.sleep(0.2)
    still = (app_window() or {}).get("minimized")
    check(len(finished()) > before, f"the finished alert goes out while minimized ({apps.alerts()[-3:]})")
    check(still is True, f"the window was still minimized when it rang (minimized {still})")
finally:
    try:
        engine.call("DELETE", "/v1/clients/" + client)
    except Exception:
        pass
    events.close()
    if app:
        app.stop()
finish()
