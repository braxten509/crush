#!/usr/bin/env python3
"""Reads and sets the hidden screen's real clipboard (desktop-design 1.9): the
sandbox has no xclip or xsel, but Python's GTK 3 bindings are in /usr.

Usage (inside tests/sandbox.sh, where DISPLAY is the hidden screen):
  clip.py get-text          prints the clipboard's text (nothing when it has none)
  clip.py set-text TEXT     owns the clipboard with TEXT
  clip.py set-image FILE    owns the clipboard with the picture in FILE (as image/png)
  clip.py targets           prints the formats the clipboard offers, one a line
  clip.py self-test         checks the above across processes
A setter returns once it owns the clipboard; a copy of it stays in the
background to answer the apps that paste, for up to 10 s or until another
program takes the clipboard. Exit 0 on success, 1 on failure, 2 without a screen.
"""
import os, select, subprocess, sys, tempfile, time

HOLD_SECONDS = 10
NO_SCREEN = "clip.py: no screen (DISPLAY is not set or not reachable)"


class NoScreen(Exception):
    pass


def gtk():
    import gi
    gi.require_version("Gtk", "3.0")
    gi.require_version("Gdk", "3.0")
    from gi.repository import Gdk, GdkPixbuf, GLib, Gtk
    if not Gtk.init_check(sys.argv)[0]:
        raise NoScreen()
    return Gdk, GdkPixbuf, GLib, Gtk


def clipboard():
    Gdk, _, _, Gtk = gtk()
    return Gtk.Clipboard.get(Gdk.SELECTION_CLIPBOARD)


def get_text():
    text = clipboard().wait_for_text()
    sys.stdout.write(text or "")
    sys.stdout.flush()
    return 0


def targets():
    ok, atoms = clipboard().wait_for_targets()
    for atom in atoms or [] if ok else []:
        print(atom.name())
    return 0


def own(kind, value):
    """Sets the clipboard in a child that keeps answering for HOLD_SECONDS; the
    parent returns once the child owns it. The child forks before GTK starts and
    lets go of stdout and stderr, so a caller reading them is not held up."""
    read_end, write_end = os.pipe()
    pid = os.fork()
    if pid:
        os.close(write_end)
        ready, _, _ = select.select([read_end], [], [], 10)
        answer = os.read(read_end, 64).decode() if ready else ""
        os.close(read_end)
        if answer.startswith("ok"):
            return 0
        if answer.startswith("no screen"):
            print(NO_SCREEN, file=sys.stderr)
            return 2
        print("clip.py: could not take the clipboard: " + (answer or "no answer"), file=sys.stderr)
        return 1
    os.close(read_end)
    os.setsid()
    devnull = os.open(os.devnull, os.O_RDWR)
    for fd in (0, 1, 2):
        os.dup2(devnull, fd)
    try:
        Gdk, GdkPixbuf, GLib, Gtk = gtk()
        board = Gtk.Clipboard.get(Gdk.SELECTION_CLIPBOARD)
        if kind == "text":
            board.set_text(value, -1)
        else:
            board.set_image(GdkPixbuf.Pixbuf.new_from_file(value))
        started = time.monotonic()
        while Gtk.events_pending():
            Gtk.main_iteration()
        # GDK wraps a foreign owner's window too, so ownership means the
        # owner is still the window GTK took the clipboard with.
        mine = Gdk.selection_owner_get(Gdk.SELECTION_CLIPBOARD)

        def owned():
            owner = Gdk.selection_owner_get(Gdk.SELECTION_CLIPBOARD)
            return mine is not None and owner is not None and owner.get_xid() == mine.get_xid()

        def watch():
            if not owned() or time.monotonic() - started > HOLD_SECONDS:
                Gtk.main_quit()
                return False
            return True
        os.write(write_end, b"ok\n" if owned() else b"not owner\n")
        os.close(write_end)
        GLib.timeout_add(100, watch)
        Gtk.main()
    except NoScreen:
        os.write(write_end, b"no screen\n")
    except Exception as e:  # report to the parent, which is still waiting
        try:
            os.write(write_end, ("error: %s\n" % e).encode())
        except OSError:
            pass
    os._exit(0)


def holders(value):
    """PIDs of setters still running with value on their command line."""
    found = []
    for pid in filter(str.isdigit, os.listdir("/proc")):
        try:
            with open("/proc/%s/cmdline" % pid, "rb") as f:
                args = f.read().decode("utf-8", "replace").split("\0")
        except OSError:
            continue
        if value in args and any(a.endswith("clip.py") for a in args):
            found.append(int(pid))
    return found


def run(*args):
    p = subprocess.run([sys.executable, "-I", os.path.abspath(__file__), *args], capture_output=True, timeout=30)
    return p.returncode, p.stdout.decode("utf-8", "replace")


def self_test():
    failures = 0

    def check(ok, what):
        nonlocal failures
        print(("PASS " if ok else "FAIL ") + what, flush=True)
        failures += 0 if ok else 1

    text = "clip check: Grüße <b>&amp;</b>\nsecond line"
    started = time.monotonic()
    code, _ = run("set-text", text)
    check(code == 0 and time.monotonic() - started < 5, "set-text returns at once, owning the clipboard")
    code, got = run("get-text")
    check(code == 0 and got == text, "get-text in another process reads the text back exactly")
    code, got = run("targets")
    check(code == 0 and "UTF8_STRING" in got.split(), "the text is offered as UTF8_STRING")
    code, _ = run("set-text", "second owner")
    code2, got = run("get-text")
    check(code == 0 and code2 == 0 and got == "second owner", "a later setter takes the clipboard over")
    time.sleep(0.5)
    check(not holders(text), "the first setter stops once another program has the clipboard")
    folder = tempfile.mkdtemp(prefix="clip-")
    png = os.path.join(folder, "dot.png")
    _, GdkPixbuf, _, _ = gtk()
    pixbuf = GdkPixbuf.Pixbuf.new(GdkPixbuf.Colorspace.RGB, False, 8, 2, 2)
    pixbuf.fill(0xFF000000)
    pixbuf.savev(png, "png", [], [])
    code, _ = run("set-image", png)
    code2, got = run("targets")
    check(code == 0 and "image/png" in got.split(), "set-image offers image/png")
    code, got = run("get-text")
    check(got == "", "a picture on the clipboard has no text")
    run("set-text", "held for a while")
    time.sleep(HOLD_SECONDS + 1.5)
    code, got = run("get-text")
    check(got == "", f"a setter lets go after {HOLD_SECONDS} s")
    print("ALL PASSED" if failures == 0 else f"{failures} FAILED", flush=True)
    return 1 if failures else 0


def main():
    try:
        return command(sys.argv[1:])
    except NoScreen:
        print(NO_SCREEN, file=sys.stderr)
        return 2


def command(args):
    if args == ["get-text"]:
        return get_text()
    if args == ["targets"]:
        return targets()
    if len(args) == 2 and args[0] == "set-text":
        return own("text", args[1])
    if len(args) == 2 and args[0] == "set-image":
        if not os.path.isfile(args[1]):
            print("clip.py: no such file: " + args[1], file=sys.stderr)
            return 1
        return own("image", os.path.abspath(args[1]))
    if args == ["self-test"]:
        return self_test()
    print(__doc__, file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main())
