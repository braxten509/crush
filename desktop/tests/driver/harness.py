"""Shared helpers for hidden-screen checks: PASS/FAIL lines, the app's probe
report, the real mouse and keyboard (xdotool) and screenshots.

Drivers are started by tests/run-in-sandbox.sh as DRIVER.py PROBE OUT PORT
[args]. Rects in the probe are viewport pixels {x, y, w, h}; the helpers that
click add the app window's place on the hidden screen."""
import contextlib, itertools, json, os, subprocess, sys, time

failures = 0
_step_failures = 0


def check(ok, what):
    global failures, _step_failures
    print(("PASS " if ok else "FAIL ") + what, flush=True)
    if not ok:
        failures += 1
        _step_failures += 1
    return ok


def read_probe(path, tries=50):
    for _ in range(tries):
        try:
            with open(path) as f:
                return json.load(f)
        except (OSError, ValueError):
            time.sleep(0.1)
    return {}


def wait_for(path, cond, timeout=20):
    end = time.time() + timeout
    p = {}
    while time.time() < end:
        p = read_probe(path, tries=1)
        if p and cond(p):
            return p
        time.sleep(0.2)
    return p


def xdo(*args):
    return subprocess.run(["xdotool", *map(str, args)], check=True, capture_output=True, text=True).stdout


def window_geometry(timeout=20):
    """Find the app's visible window by its class (crush-desktop; the title
    follows the chat) and return its (x, y, w, h). Only visible windows count
    and the largest one wins. None if none appears."""
    end = time.time() + timeout
    while time.time() < end:
        r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
        best = None
        for wid in r.stdout.split():
            g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
            d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
            if "WIDTH" in d:
                box = (int(d["X"]), int(d["Y"]), int(d["WIDTH"]), int(d["HEIGHT"]))
                if best is None or box[2] * box[3] > best[2] * best[3]:
                    best = box
        if best and best[2] > 100:
            return best
        time.sleep(0.3)
    return None


def shot(out_dir, name):
    os.makedirs(out_dir, exist_ok=True)
    path = os.path.join(out_dir, f"{name}.png")
    subprocess.run(["import", "-window", "root", path], check=True)
    return path


def finish():
    print("ALL PASSED" if failures == 0 else f"{failures} FAILED", flush=True)
    sys.exit(1 if failures else 0)


# The probe's sections and heavy fields (desktop-design 1.9).

def probe_section(path, name, timeout=20):
    """One section of the probe report (light fields), waiting until it appears."""
    p = wait_for(path, lambda p: name in p, timeout)
    return p.get(name)


_request_ids = itertools.count(int(time.time() * 1000))


def probe_eval(path, script, timeout=20):
    """Runs script in the page through the app (probe mode only) and returns
    what it put in window.__probeEval: report["evaluated"]."""
    with open(path + ".eval.js.tmp", "w") as f:
        f.write(script)
    os.replace(path + ".eval.js.tmp", path + ".eval.js")
    rid = next(_request_ids)
    side = path + ".request.json"
    with open(side + ".tmp", "w") as f:
        json.dump({"id": rid, "eval": True}, f)
    os.replace(side + ".tmp", side)
    return wait_for(path, lambda p: p.get("request_id") == rid, timeout).get("evaluated")


def probe_heavy(path, timeout=20, invoke=None):
    """Asks the page for its heavy fields (rects, words, selection, scans) and
    returns the whole report once it answers: report["heavy"][section] holds
    them. invoke: an optional list of {"command", "args"} the page runs once,
    whose answers come back in report["invoked"]. Never call it inside a
    measured stretch: the page holds the request until the stretch ends."""
    rid = next(_request_ids)
    request = {"id": rid}
    if invoke is not None:
        request["invoke"] = invoke
    side = path + ".request.json"
    with open(side + ".tmp", "w") as f:
        json.dump(request, f)
    os.replace(side + ".tmp", side)
    return wait_for(path, lambda p: p.get("request_id") == rid, timeout)


def word_rect(report, text, mid=None, nth=0):
    """The rect of the nth visible word equal to text (in message mid, if given),
    from a probe_heavy() report's chat words; None when it is not on screen."""
    words = (((report.get("heavy") or {}).get("chat") or {}).get("words")) or []
    found = [w for w in words if w.get("text") == text and (mid is None or w.get("mid") == mid)]
    return found[nth]["rect"] if len(found) > nth else None


def _window_origin():
    geo = window_geometry()
    return (geo[0], geo[1]) if geo else (0, 0)


def _screen(x, y):
    ox, oy = _window_origin()
    return round(ox + x), round(oy + y)


def select_words(path, first, last, mid=None):
    """Drags the real mouse from the start of the word first to the end of the
    word last. Returns False when either word is not on screen."""
    report = probe_heavy(path)
    a, b = word_rect(report, first, mid), word_rect(report, last, mid)
    if not a or not b:
        return False
    sx, sy = _screen(a["x"] + 1, a["y"] + a["h"] / 2)
    ex, ey = _screen(b["x"] + b["w"] - 1, b["y"] + b["h"] / 2)
    xdo("mousemove", sx, sy)
    xdo("mousedown", 1)
    for i in range(1, 6):
        xdo("mousemove", round(sx + (ex - sx) * i / 5), round(sy + (ey - sy) * i / 5))
        time.sleep(0.03)
    xdo("mouseup", 1)
    return True


def click(x, y, button=1):
    """A real click at a viewport point."""
    sx, sy = _screen(x, y)
    xdo("mousemove", sx, sy)
    xdo("click", button)


def right_click(x, y):
    """A real right click at a viewport point (opens the menu there)."""
    click(x, y, button=3)


def type_text(text, delay_ms=20):
    """Types text with the real keyboard into whatever has focus."""
    xdo("type", "--delay", delay_ms, "--", text)


def key(*combos):
    """Presses keys by xdotool name, for example key("ctrl+g") or key("Down", "Return")."""
    for combo in combos:
        xdo("key", "--clearmodifiers", combo)


def wait_text(path, text, timeout=20):
    """Waits until text appears anywhere in the probe report's light fields;
    returns the report, or {} on timeout."""
    p = wait_for(path, lambda p: text in json.dumps(p, ensure_ascii=False), timeout)
    return p if p and text in json.dumps(p, ensure_ascii=False) else {}


def engine_get(path, api_path):
    """GET from the engine over its socket (curl --unix-socket), the ground truth
    for engine checks. api_path is relative to the window's workspace
    ("sessions") or absolute ("/v1/limits"). The socket and workspace come from
    the probe's connection section."""
    conn = probe_section(path, "connection") or {}
    sock = (conn.get("engine") or {}).get("socket") or conn.get("socket")
    if not sock:
        raise RuntimeError("the probe's connection section has no engine socket")
    if not api_path.startswith("/"):
        api_path = f"/v1/workspaces/{conn.get('workspace_id')}/{api_path}"
    r = subprocess.run(["curl", "--silent", "--fail", "--unix-socket", sock, f"http://crush{api_path}"],
                       capture_output=True, text=True, timeout=20)
    if r.returncode != 0:
        raise RuntimeError(f"curl {api_path} failed with exit {r.returncode}")
    return json.loads(r.stdout or "null")


def fake_ai_requests():
    """Every request the fake AI server logged, in order."""
    log = os.environ.get("FAKE_AI_LOG", "/tmp/fake-ai.jsonl")
    if not os.path.exists(log):
        return []
    with open(log) as f:
        return [json.loads(line) for line in f if line.strip()]


@contextlib.contextmanager
def step(row, out_dir=None):
    """One check step named after its row (desktop-design 2.0): its checks run
    inside the with-block; at the end the window is saved as <out>/<ROW>.png,
    the row's proof, and a PASS or FAIL line names the row."""
    global _step_failures
    out_dir = out_dir or (sys.argv[2] if len(sys.argv) > 2 else "tests/out")
    _step_failures = 0
    print(f"STEP {row}", flush=True)
    try:
        yield
    except Exception as e:  # noqa: BLE001 - a crash in a step is that step's failure
        check(False, f"{row} raised {type(e).__name__}: {e}")
    finally:
        try:
            shot(out_dir, row)
        except Exception as e:  # noqa: BLE001
            check(False, f"{row} screenshot ({e})")
        print(("PASS " if _step_failures == 0 else "FAIL ") + f"step {row}", flush=True)
