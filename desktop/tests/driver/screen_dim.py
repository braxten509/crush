"""The window comes back bright after the screen dims (owner, 2026-11-01:
"when my screen darkens the entire crush GUI darkens until I move it").
KDE dims the screen by announcing a dimmer white over the Wayland color
protocol (wp_color_manager_v1): about 280 changes in 4 s, from 123 down to
39 and back (logged on the owner's KWin 6.8.2). Chromium follows those
announcements and kept the dark one until the window moved; apps that ignore
the protocol stayed right. So the app must not use the protocol at all.

On the private hidden KWin: the app starts with the Wayland traffic logged,
and its log must show the window being set up but no wp_color_manager_v1
bound.

Usage: tests/check.sh --no-app --kwin screen_dim.py
"""
import os, re, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import apps
from harness import check, finish


app = apps.App(env={"WAYLAND_DEBUG": "client"})
try:
    app.wait_window(lambda p: bool(p), timeout=60)
    log, end = "", time.time() + 20
    while time.time() < end and "xdg_toplevel" not in log:
        with open(app.log, errors="replace") as f:
            log = f.read()
        time.sleep(0.2)
    check("xdg_toplevel" in log, "the app's Wayland traffic is logged (the window was set up)")
    binds = re.findall(r"bind\(\d+, \"wp_color_manager_v1\"", log)
    check(not binds, f"the app does not use KDE's color protocol ({len(binds)} binds)")
finally:
    app.stop()
finish()
