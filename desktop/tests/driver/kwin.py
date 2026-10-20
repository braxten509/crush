"""Helpers for checks on the private hidden KWin screen (tests/check.sh --kwin,
tests/kwin/screen.sh): KWin's own view of the app's window, read through a
KWin script, and a real mouse KWin sees (tests/kwin/ei-mouse.c)."""
import json, os, subprocess, time

ROOT = os.environ.get("DESKTOP_ROOT", os.path.join(os.path.dirname(__file__), "..", ".."))
LOG = os.path.join(ROOT, "tests", "out", "kwin", "kwin.log")
MOUSE = os.path.join(ROOT, "tests", "out", "kwin", "ei-mouse")
_scripts = 0


def _gdbus(path, method, *args):
    r = subprocess.run(["gdbus", "call", "--session", "--dest", "org.kde.KWin", "--object-path", path,
                        "--method", method, *map(str, args)], capture_output=True, text=True, timeout=10)
    if r.returncode != 0:
        raise RuntimeError(f"gdbus {method}: {r.stderr.strip()}")
    return r.stdout


def kwin_eval(body, timeout=5):
    """Runs body (JavaScript returning a value) as a KWin script and returns
    the value: the script prints it as JSON after a marker into KWin's log."""
    global _scripts
    _scripts += 1
    name = f"check-{os.getpid()}-{_scripts}"
    marker = f"CHECK-RESULT-{_scripts}-{time.time_ns()}"
    path = f"/tmp/{name}.js"
    with open(path, "w") as f:
        f.write(f"print({json.dumps(marker)} + JSON.stringify((function () {{ {body} }})()));\n")
    start = os.path.getsize(LOG) if os.path.exists(LOG) else 0
    out = _gdbus("/Scripting", "org.kde.kwin.Scripting.loadScript", path, name)
    sid = out.strip().strip("(),").split()[0]
    try:
        _gdbus(f"/Scripting/Script{sid}", "org.kde.kwin.Script.run")
        end = time.time() + timeout
        while time.time() < end:
            with open(LOG, errors="replace") as f:
                f.seek(start)
                for line in f:
                    if marker in line:
                        return json.loads(line.split(marker, 1)[1].strip().rstrip('"').replace('\\"', '"'))
            time.sleep(0.05)
        raise RuntimeError(f"KWin printed no answer for {name}")
    finally:
        try:
            _gdbus("/Scripting", "org.kde.kwin.Scripting.unloadScript", name)
        except Exception:
            pass


APP_WINDOWS = """
  return workspace.windowList().filter((w) => w.resourceClass == "crush-desktop" && w.normalWindow).map((w) => ({
    caption: w.caption, x: w.frameGeometry.x, y: w.frameGeometry.y, w: w.frameGeometry.width, h: w.frameGeometry.height,
    keepAbove: w.keepAbove, minimized: w.minimized, active: w === workspace.activeWindow, pid: w.pid,
    noBorder: w.noBorder, area: (() => { const a = workspace.clientArea(KWin.MaximizeArea, w); return { x: a.x, y: a.y, w: a.width, h: a.height }; })(),
  }));
"""


def app_windows():
    return kwin_eval(APP_WINDOWS)


def app_window(timeout=20):
    """The app's one window as KWin sees it, waiting until it shows."""
    end = time.time() + timeout
    while time.time() < end:
        try:
            got = app_windows()
        except RuntimeError:
            got = []
        if got:
            return got[0]
        time.sleep(0.3)
    return None


class Mouse:
    """A real pointer on the hidden KWin screen, in screen points."""

    def __init__(self):
        if not os.path.exists(MOUSE):
            os.makedirs(os.path.dirname(MOUSE), exist_ok=True)
            flags = subprocess.run(["pkg-config", "--cflags", "--libs", "libei-1.0", "libsystemd"],
                                   capture_output=True, text=True, check=True).stdout.split()
            subprocess.run(["cc", "-O2", "-o", MOUSE, os.path.join(ROOT, "tests", "kwin", "ei-mouse.c"), *flags], check=True)
        self.p = subprocess.Popen([MOUSE], stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True)
        line = self.p.stdout.readline().strip()
        if line != "ready":
            raise RuntimeError(f"ei-mouse did not start ({line!r})")

    def _do(self, command):
        self.p.stdin.write(command + "\n")
        self.p.stdin.flush()
        self.p.stdout.readline()

    def move(self, x, y):
        self._do(f"move {x} {y}")

    def press(self, button=1):
        self._do(f"down {button}")

    def release(self, button=1):
        self._do(f"up {button}")

    def click(self, x, y, button=1):
        self.move(x, y)
        self._do("sleep 60")
        self.press(button)
        self._do("sleep 40")
        self.release(button)

    def double_click(self, x, y):
        self.move(x, y)
        self._do("sleep 60")
        for _ in range(2):
            self.press()
            self._do("sleep 30")
            self.release()
            self._do("sleep 60")

    def drag(self, x0, y0, x1, y1, steps=20, pace_ms=15, hold_ms=80):
        """Press, move in steps pace_ms apart, wait hold_ms, release. A slow
        pace gives a window that redraws as it goes (a resize) time to follow."""
        self.move(x0, y0)
        self._do("sleep 80")
        self.press()
        self._do("sleep 80")
        for i in range(1, steps + 1):
            self.move(x0 + (x1 - x0) * i / steps, y0 + (y1 - y0) * i / steps)
            self._do(f"sleep {pace_ms}")
        self._do(f"sleep {hold_ms}")
        self.release()

    def close(self):
        try:
            self._do("quit")
        except Exception:
            pass
        self.p.wait(timeout=5)
