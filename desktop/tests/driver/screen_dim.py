"""The window comes back bright after the screen dims (owner, 2026-11-01:
"when my screen darkens the entire crush GUI darkens until I move it").
KDE dims the screen by announcing a dimmer white over the Wayland color
protocol (wp_color_manager_v1): about 280 changes in 4 s, from 123 down to
39 and back (logged on the owner's KWin 6.8.2). Chromium follows those
announcements and kept the dark one until the window moved; apps that ignore
the protocol stayed right. So the app must not use the protocol at all.

On the private hidden KWin: the app starts with the Wayland traffic logged,
and its log must show the window being set up but no wp_color_manager_v1
bound. The GPU process's --disable-features must still hold Electron's own
entries next to ours (appendSwitch adds to Electron's list, not over it).

Usage: tests/check.sh --no-app --kwin screen_dim.py
"""
import os, re, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import apps
from harness import check, finish


def children(pid):
    """Every process under pid, by /proc parent links."""
    kids = {}
    for name in os.listdir("/proc"):
        if not name.isdigit():
            continue
        try:
            with open(f"/proc/{name}/stat") as f:
                parent = int(f.read().rsplit(")", 1)[1].split()[1])
        except (OSError, IndexError, ValueError):
            continue
        kids.setdefault(parent, []).append(int(name))
    found, todo = [], [pid]
    while todo:
        for kid in kids.get(todo.pop(), []):
            found.append(kid)
            todo.append(kid)
    return found


def gpu_args(pid):
    for kid in children(pid):
        try:
            with open(f"/proc/{kid}/cmdline", "rb") as f:
                # Chromium rewrites its title into one space-separated line.
                argv = f.read().decode(errors="replace").replace("\0", " ").split()
        except OSError:
            continue
        if "--type=gpu-process" in argv:
            return argv
    return []


app = apps.App(env={"WAYLAND_DEBUG": "client"})
try:
    app.wait_window(lambda p: bool(p), timeout=60)
    time.sleep(1)
    with open(app.log, errors="replace") as f:
        log = f.read()
    check("xdg_toplevel" in log, "the app's Wayland traffic is logged (the window was set up)")
    binds = re.findall(r"bind\(\d+, \"wp_color_manager_v1\"", log)
    check(not binds, f"the app does not use KDE's color protocol ({len(binds)} binds)")
    features = next((a.split("=", 1)[1].split(",") for a in gpu_args(app.proc.pid) if a.startswith("--disable-features=")), [])
    check("WaylandWpColorManagerV1" in features, f"the GPU process has the feature off ({features})")
    check("SpareRendererForSitePerProcess" in features, "Electron's own disabled features are still there")
finally:
    app.stop()
finish()
