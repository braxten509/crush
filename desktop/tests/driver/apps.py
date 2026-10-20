"""Helpers the phases 5 and 6 drivers use next to harness.py
(desktop-design-5-6 8.2). Drivers run with tests/check.sh --no-app, so
tests/run-in-sandbox.sh sets up the project, the fake AI server and the
environment but leaves starting the app to App here.

- App(args=(), env={}, probe="/tmp/probe.json"): $DESKTOP_APP --probe <probe>
  --cwd /tmp/home/proj <args>, with run-in-sandbox's app environment plus env;
  output in tests/out/<driver>/app-<n>.log. .window(label) reads that window's
  probe file (the first window's is <probe>, every other <probe>.<label>);
  .stop(); .wait_exit(timeout).
- second_project(): /tmp/home/proj2, made the way run-in-sandbox makes
  /tmp/home/proj (same template, same fake provider).
- config(options, project): merges keys into the project's crush.json.
- Term(name="tui", cwd="/tmp/home/proj", args=(), env={}): the terminal app in
  the hidden tmux of the 3-4 design 6.4; .keys(), .text(), .sgr(),
  .wait_text(), .stop(); kill_terms() ends tmux -L crushtest.
- Bus(): a private session bus with tests/fakes/notify_catcher.py on it; .env
  gives DBUS_SESSION_BUS_ADDRESS; .notifications(); .stop().
- focus_away(): a 10x10 GTK window that takes the focus; .close().
- calls(name), alerts(probe), opened(probe), api_log(probe): the stand-ins'
  $FAKE_CMD_LOG lines and the app's recorded alerts, opens and API calls.
- seams(dict): writes /tmp/crushtest-seams.json and returns the environment
  that names it (G-21 to G-24).

Every stand-in call is recorded in /tmp/fake-cmd.jsonl unless the driver
names another $FAKE_CMD_LOG; the apps and terminals started here inherit it."""
import itertools, json, os, shlex, subprocess, sys, time

ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
PROJ = "/tmp/home/proj"
PROJ2 = "/tmp/home/proj2"
PROBE = "/tmp/probe.json"
SEAMS = "/tmp/crushtest-seams.json"
BUS = "/tmp/crushtest-bus"
NOTIFICATIONS = "/tmp/crushtest-notifications.jsonl"
TMUX = ["tmux", "-L", "crushtest"]

os.environ.setdefault("FAKE_CMD_LOG", "/tmp/fake-cmd.jsonl")


def out_dir():
    """tests/out/<driver>: the driver's second argument (run-in-sandbox.sh)."""
    return sys.argv[2] if len(sys.argv) > 2 else os.path.join(ROOT, "tests", "out")


def fake_port():
    if len(sys.argv) > 3 and sys.argv[3]:
        return sys.argv[3]
    with open("/tmp/fake-ai.port") as f:
        return f.read().strip()


def _lines(path):
    if not os.path.exists(path):
        return []
    with open(path) as f:
        return [json.loads(line) for line in f if line.strip()]


def _app_env(env):
    """run-in-sandbox's app environment: the driver's own, CRUSH_DESKTOP_CRUSH
    when DESKTOP_CRUSH is set, and the run's CRUSH_DESKTOP_ENV_MARK."""
    out = dict(os.environ)
    crush = os.environ.get("DESKTOP_CRUSH")
    if crush:
        out["CRUSH_DESKTOP_CRUSH"] = crush
    try:
        with open(os.path.join(out_dir(), "env-mark")) as f:
            out["CRUSH_DESKTOP_ENV_MARK"] = f.read().strip()
    except OSError:
        pass
    out.update(env)
    return out


_app_numbers = itertools.count(1)


class App:
    def __init__(self, args=(), env=None, probe=PROBE):
        self.probe = probe
        self.n = next(_app_numbers)
        self.log = os.path.join(out_dir(), f"app-{self.n}.log")
        binary = os.environ.get("DESKTOP_APP") or os.path.join(ROOT, "electron", "crush-desktop")
        extra = shlex.split(os.environ.get("DESKTOP_ARGS", ""))
        self.argv = [binary, "--probe", probe, "--cwd", PROJ, *extra, *args]
        os.makedirs(out_dir(), exist_ok=True)
        self._out = open(self.log, "w")
        self.proc = subprocess.Popen(self.argv, env=_app_env(env or {}), stdout=self._out, stderr=subprocess.STDOUT,
                                     stdin=subprocess.DEVNULL)

    def probe_file(self, label="main"):
        return self.probe if label == "main" else f"{self.probe}.{label}"

    def window(self, label="main"):
        """That window's probe report, or {} while it has none."""
        try:
            with open(self.probe_file(label)) as f:
                return json.load(f)
        except (OSError, ValueError):
            return {}

    def wait_window(self, cond, label="main", timeout=30):
        end = time.time() + timeout
        p = {}
        while time.time() < end:
            p = self.window(label)
            if p and cond(p):
                return p
            time.sleep(0.2)
        return p

    def wait_exit(self, timeout=10):
        try:
            return self.proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            return None

    def stop(self):
        if self.proc.poll() is None:
            self.proc.terminate()
            if self.wait_exit(10) is None:
                self.proc.kill()
                self.proc.wait()
        self._out.close()


def _copy_project(dest):
    port = fake_port()
    os.makedirs(dest, exist_ok=True)
    subprocess.run(f"tar -C {shlex.quote(os.path.join(ROOT, 'tests', 'project'))} --exclude=crush.json.in -cf - . "
                   f"| tar -C {shlex.quote(dest)} -xf -", shell=True, check=True)
    with open(os.path.join(ROOT, "tests", "project", "crush.json.in")) as f:
        text = f.read().replace("@PORT@", port)
    with open(os.path.join(dest, "crush.json"), "w") as f:
        f.write(text)
    subprocess.run(["git", "-C", dest, "init", "-q"], check=True)
    return dest


def second_project():
    return _copy_project(PROJ2)


def _merge(base, extra):
    for k, v in extra.items():
        if isinstance(v, dict) and isinstance(base.get(k), dict):
            _merge(base[k], v)
        else:
            base[k] = v
    return base


def config(options, project=PROJ):
    path = os.path.join(project, "crush.json")
    with open(path) as f:
        data = json.load(f)
    _merge(data, options)
    with open(path, "w") as f:
        json.dump(data, f, indent=2)
    return data


class Term:
    def __init__(self, name="tui", cwd=PROJ, args=(), env=None):
        self.name = name
        crush = os.environ.get("DESKTOP_CRUSH") or os.path.join(ROOT, "tests", "out", "bin", "crush")
        sets = [f"{k}={v}" for k, v in (env or {}).items()]
        command = shlex.join(["env", "CRUSH_CLIENT_SERVER=1", *sets, crush, "--cwd", cwd, *args])
        subprocess.run([*TMUX, "-f", "/dev/null", "new-session", "-d", "-s", name, "-x", "200", "-y", "50", command],
                       check=True)

    def keys(self, *keys):
        subprocess.run([*TMUX, "send-keys", "-t", self.name, *keys], check=True)

    def text(self):
        r = subprocess.run([*TMUX, "capture-pane", "-p", "-t", self.name], capture_output=True, text=True)
        return r.stdout if r.returncode == 0 else ""

    def sgr(self):
        r = subprocess.run([*TMUX, "capture-pane", "-p", "-e", "-t", self.name], capture_output=True, text=True)
        return r.stdout if r.returncode == 0 else ""

    def wait_text(self, text, timeout=10):
        end = time.time() + timeout
        while time.time() < end:
            screen = self.text()
            if text in screen:
                return screen
            time.sleep(0.2)
        return None

    def stop(self):
        subprocess.run([*TMUX, "kill-session", "-t", self.name], capture_output=True)


def kill_terms():
    subprocess.run([*TMUX, "kill-server"], capture_output=True)


class Bus:
    def __init__(self, timeout=10):
        r = subprocess.run(["dbus-daemon", "--session", "--fork", "--print-address=1", "--print-pid=1",
                            f"--address=unix:path={BUS}"], capture_output=True, text=True, check=True)
        lines = r.stdout.split()
        self.address = lines[0]
        self.pid = int(lines[1]) if len(lines) > 1 else None
        self.env = {"DBUS_SESSION_BUS_ADDRESS": self.address}
        ready = "/tmp/crushtest-catcher.ready"
        if os.path.exists(ready):
            os.remove(ready)
        self.catcher = subprocess.Popen(
            [sys.executable, "-I", os.path.join(ROOT, "tests", "fakes", "notify_catcher.py"), "--log", NOTIFICATIONS,
             "--ready", ready],
            env={**os.environ, **self.env}, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        end = time.time() + timeout
        while time.time() < end and not os.path.exists(ready):
            time.sleep(0.05)
        self.ready = os.path.exists(ready)

    def notifications(self):
        return _lines(NOTIFICATIONS)

    def stop(self):
        self.catcher.terminate()
        self.catcher.wait()
        if self.pid:
            try:
                os.kill(self.pid, 15)
            except OSError:
                pass


_AWAY = """
import gi, sys
gi.require_version("Gtk", "3.0")
from gi.repository import Gtk
w = Gtk.Window(title="crushtest-away")
w.set_default_size(10, 10)
w.connect("destroy", Gtk.main_quit)
w.show_all()
print("shown", flush=True)
Gtk.main()
"""


class focus_away:
    def __init__(self, timeout=10):
        self.proc = subprocess.Popen([sys.executable, "-I", "-c", _AWAY], stdout=subprocess.PIPE, text=True)
        self.proc.stdout.readline()
        end = time.time() + timeout
        self.wid = None
        while time.time() < end and not self.wid:
            r = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^crushtest-away$"],
                               capture_output=True, text=True)
            self.wid = (r.stdout.split() or [None])[0]
            if not self.wid:
                time.sleep(0.1)
        if self.wid:
            subprocess.run(["xdotool", "windowfocus", "--sync", self.wid], capture_output=True)

    def close(self):
        self.proc.terminate()
        self.proc.wait()


def calls(name):
    return [c for c in _lines(os.environ["FAKE_CMD_LOG"]) if c.get("cmd") == name]


def alerts(probe=PROBE):
    return _lines(probe + ".alerts.jsonl")


def opened(probe=PROBE):
    return _lines(probe + ".opened.jsonl")


def api_log(probe=PROBE):
    return _lines(probe + ".api.jsonl")


def seams(entries):
    with open(SEAMS, "w") as f:
        json.dump(entries, f)
    return {"CRUSH_TEST_SEAMS": SEAMS}
