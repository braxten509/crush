"""TERM's window checks (desktop-design-5-6 2.25, 2.26, 8.4, 8.5): terminal
tabs and the external editor, one step per checklist row.

Runs in the sandbox on TERM's fixture (no engine):
  tests/check.sh --no-app --fixture tests/fixtures/terminal-chats.json p6_terminal.py [names]
Names pick steps (default: all): editor, new_terminal, env_shown,
cwd_tracking, scrollback, select_copy, typing_jumps, paste_focus,
shift_enter, cursor, theme_follow, close_box, close_buttons, exit_closes_tab,
close_kinds, busy.

The app starts here (apps.App) on the chat "Shell checks" with SHELL=/bin/sh
and VISUAL naming tests/fakes/fake-editor through a link called nvim, so the
editor gets the position argument x/editor gives nvim (2.26 step 3); EDITOR
names a missing program, so only $VISUAL can have been used. The variables
the tabs must not get are exported by /tmp/home/.profile, which the app's
login shell reads (login_env.rs) and the tab's shell does not.

Until the folder picker (LST) is merged, a terminal tab is opened through
the editor: the fake editor in its shell mode runs an interactive /bin/sh in
the tab, titled "Editor". With the picker (Alt+T, LIST-005) the steps run on
real "Terminal" tabs. The rows' other parts that need LST, KIT, THM or PAL
are named in each step's NOT-RUN lines."""
import os, re, shutil, subprocess, sys, time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import harness as H
from harness import check, finish, key, probe_heavy, step, type_text, wait_for, xdo
import apps
import importlib.util

_spec = importlib.util.spec_from_file_location("p3_connect", os.path.join(HERE, "p3_connect.py"))
pc = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(pc)

probe_path, out_dir = sys.argv[1], sys.argv[2]
NAMES = sys.argv[4:]

FAKE_EDITOR = os.path.join(apps.ROOT, "tests", "fakes", "fake-editor")
EDITOR_LINK = "/tmp/editor-bin/nvim"
MODE = "/tmp/fake-editor.mode"
LOG = "/tmp/fake-editor.log"
CLIP = [sys.executable, "-I", os.path.join(HERE, "clip.py")]
APP_ENV = {"SHELL": "/bin/sh", "VISUAL": EDITOR_LINK, "EDITOR": "/nonexistent/editor"}

# What 2.25 removes, as the login shell exports it (/tmp/home/.profile).
REMOVED = {
    "TERM_PROGRAM": "crushtest", "TERM_PROGRAM_VERSION": "1", "LC_TERMINAL": "crushtest", "LC_TERMINAL_VERSION": "1",
    "VTE_VERSION": "7000", "WT_SESSION": "crushtest", "KONSOLE_VERSION": "1", "KITTY_WINDOW_ID": "1",
    "GHOSTTY_RESOURCES_DIR": "/x", "WEZTERM_PANE": "1", "ITERM_SESSION_ID": "1", "TMUX": "/tmp/tmux-x,1,0",
    "TMUX_PANE": "%1", "CRUSH_SESSION_HOST": "1", "CRUSH_TASKS_DIR": "/tmp/crushtest-tasks", "CRUSH_SESSION_ID": "s1",
    "CRUSH_CRASH_REPORT": "/tmp/crushtest-report", "CRUSH_REMOTE_LAUNCH": "crushtest-launch",
}
REMOVED_PREFIXES = ("KONSOLE_", "KITTY_", "GHOSTTY_", "WEZTERM_", "ITERM_", "TMUX")
CLOSE_DETAIL = "This closes the terminal. Its output is not saved."
BUSY = "Agent is working, please wait..."
PNG_1PX = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

app = None
notes = []  # what ran on a stand-in or waits for another piece, for the report


def wanted(name):
    return not NAMES or name in NAMES


def not_run(row, why):
    print(f"NOT-RUN {row}: {why}", flush=True)
    notes.append(f"{row}: {why}")


# -- The probe --

def report():
    return H.read_probe(probe_path, tries=5)


def term(p=None):
    return (p or report()).get("terminal") or {}


def tabs(p=None):
    return term(p).get("tabs") or []


def tab_of(tid, p=None):
    return next((t for t in tabs(p) if t["id"] == tid), None)


def wait_term(cond, timeout=5):
    p = wait_for(probe_path, lambda p: bool(cond(term(p))), timeout)
    return term(p)


def wait_tab(tid, cond, timeout=5):
    """The tab once cond(tab) holds; else its last state (or {})."""
    found = {}

    def test(t):
        nonlocal found
        found = next((x for x in t.get("tabs") or [] if x["id"] == tid), None) or {}
        return found and cond(found)

    wait_term(test, timeout)
    return found


def lines_of(tid, p=None):
    return (tab_of(tid, p) or {}).get("lines") or []


def wait_line(tid, test, timeout=5):
    """The first line of the tab that test(line) accepts, or None."""
    hit = []
    wait_tab(tid, lambda t: any(test(line) and not hit.append(line) for line in t.get("lines") or []), timeout)
    return hit[0] if hit else None


def heavy():
    return ((probe_heavy(probe_path) or {}).get("heavy") or {}).get("terminal") or {}


def dialogs(p=None):
    return ((p or report()).get("overlay") or {}).get("dialogs") or []


def notice(p=None):
    return ((p or report()).get("overlay") or {}).get("notice") or {}


def composer(p=None):
    return (p or report()).get("composer") or {}


def clipboard(p=None):
    return ((p or report()).get("clipboard") or {}).get("last") or {}


# -- Typing and the mouse --

def run(tid, command, want, timeout=8):
    """Types command and Enter into the shown tab; returns the first line want
    accepts: a test, or text the line holds (built so the typed line itself
    never holds it)."""
    type_text(command)
    key("Return")
    return wait_line(tid, want if callable(want) else (lambda line: want in line), timeout)


def screen_point(x, y):
    return H._screen(x, y)


def cell_center(h, row, col):
    s, c = h["screen"], h["cell"]
    return s["x"] + (col + 0.5) * c["w"], s["y"] + (row + 0.5) * c["h"]


def drag_cells(h, row, first, last, shift=False):
    """Drags the real mouse over cells first..last of a visible row."""
    s, c = h["screen"], h["cell"]
    y = s["y"] + (row + 0.5) * c["h"]
    sx, sy = screen_point(s["x"] + first * c["w"] + 1, y)
    ex, ey = screen_point(s["x"] + (last + 1) * c["w"] - 1, y)
    if shift:
        xdo("keydown", "shift")
    xdo("mousemove", sx, sy)
    xdo("mousedown", 1)
    for i in range(1, 6):
        xdo("mousemove", round(sx + (ex - sx) * i / 5), round(sy + (ey - sy) * i / 5))
        time.sleep(0.03)
    xdo("mouseup", 1)
    if shift:
        xdo("keyup", "shift")


def row_index(h, text):
    rows = h.get("rows") or []
    return next((i for i, r in enumerate(rows) if r == text), None)


def select_word(tid, line, word, page_back=False):
    """Selects word in the visible row that reads exactly line, paging back
    (Shift+PgUp) to it when page_back; the selection's text, or None."""
    h = heavy()
    r = row_index(h, line)
    for _ in range(8 if page_back else 0):
        if r is not None:
            break
        key("shift+Prior")
        time.sleep(0.2)
        h = heavy()
        r = row_index(h, line)
    if r is None:
        return None
    at = line.index(word)
    drag_cells(h, r, at, at + len(word) - 1)
    t = wait_tab(tid, lambda t: t.get("selection") == word, 3)
    return t.get("selection")


def mouse_over_terminal():
    h = heavy()
    if h.get("screen"):
        x, y = cell_center(h, max(1, len(h.get("rows") or [1]) // 2), 10)
        xdo("mousemove", *screen_point(x, y))
    return h


def set_clipboard(text):
    subprocess.run([*CLIP, "set-text", text], check=True, timeout=10)


PRIMARY = """
import gi, os, sys
gi.require_version("Gtk", "3.0")
gi.require_version("Gdk", "3.0")
from gi.repository import Gdk, GLib, Gtk
Gtk.init_check(sys.argv)
Gtk.Clipboard.get(Gdk.SELECTION_PRIMARY).set_text(sys.argv[1], -1)
Gtk.Clipboard.get(Gdk.SELECTION_PRIMARY).store()
print("owned", flush=True)
GLib.timeout_add_seconds(8, Gtk.main_quit)
Gtk.main()
"""


def set_primary(text):
    """Owns the PRIMARY selection with text for a few seconds (clip.py does CLIPBOARD only)."""
    proc = subprocess.Popen([sys.executable, "-I", "-c", PRIMARY, text], stdout=subprocess.PIPE, text=True)
    proc.stdout.readline()
    return proc


def set_mode(mode):
    with open(MODE, "w") as f:
        f.write(mode + "\n")


def editor_calls():
    return apps._lines(LOG)


def app_window():
    found = pc.windows_of(app.proc.pid) if app else []
    return found[0][0] if found else None


def focus_app():
    wid = app_window()
    if wid:
        subprocess.run(["xdotool", "windowfocus", "--sync", str(wid)], capture_output=True)


# -- The app and its tabs --

def setup():
    os.makedirs(os.path.dirname(EDITOR_LINK), exist_ok=True)
    if not os.path.islink(EDITOR_LINK):
        os.symlink(FAKE_EDITOR, EDITOR_LINK)
    with open("/tmp/home/.profile", "w") as f:
        for name, value in REMOVED.items():
            f.write(f"export {name}='{value}'\n")
        f.write("export TERM=dumb COLORTERM=crushtest-colors CRUSHTEST_KEPT=kept\n")
    set_mode("edit")


def start_app(session="idle"):
    global app
    app = apps.App(args=("-s", session), env=APP_ENV)
    ok = H.window_geometry(timeout=30) is not None
    p = wait_for(probe_path, lambda p: "terminal" in p and composer(p).get("session_id") == session, 30)
    focus_app()
    return check(ok and composer(p).get("session_id") == session, f"the app shows the chat {session!r} with the terminal probe section")


def stop_app():
    global app
    if app:
        app.stop()
        app = None


def focus_composer():
    """The typing box has the focus (a click into it), the caret at the end."""
    p = probe_heavy(probe_path)
    box = (((p or {}).get("heavy") or {}).get("composer") or {}).get("textarea")
    if box:
        H.click(box["x"] + box["w"] / 2, box["y"] + box["h"] / 2)
    wait_for(probe_path, lambda p: composer(p).get("focused"), 3)
    key("ctrl+End")


def clear_composer():
    """An empty typing box out of shell mode (Backspace on an empty one leaves it)."""
    focus_composer()
    for _ in range(len(composer().get("value") or "") + 2):
        key("BackSpace")
    p = wait_for(probe_path, lambda p: composer(p).get("value") == "" and composer(p).get("mode") != "bang", 3)
    return composer(p).get("value") == "" and composer(p).get("mode") != "bang"


def open_shell():
    """A new terminal tab with a shell, shown; its probe entry, or None.
    LIST-005's real path (Alt+T, the folder picker) when LST is merged; else
    the editor's tab with the fake editor's shell."""
    before = {t["id"] for t in tabs()}
    focus_composer()
    key("alt+t")
    if wait_for(probe_path, lambda p: "folder_picker" in dialogs(p), 2) and "folder_picker" in dialogs():
        key("Return")
    else:
        set_mode("shell")
        key("ctrl+o")
    new = wait_term(lambda t: any(x["id"] not in before and x["state"] == "running" for x in t.get("tabs") or []), 10)
    tab = next((x for x in new.get("tabs") or [] if x["id"] not in before), None)
    set_mode("edit")
    if not tab:
        return None
    wait_line(tab["id"], lambda line: re.search(r"[$#] ?$", line) is not None, 8)
    return tab


shell = None  # the shown shell tab the next steps use


def ensure_shell():
    global shell
    t = term()
    if shell and t.get("shown") == shell["id"] and tab_of(shell["id"]):
        return shell
    shell = open_shell()
    return shell


# -- Steps --

def step_editor():
    """TYPE-015 and CMD-030 (2.26): Ctrl+O in the typing box opens $VISUAL in a
    terminal tab at the caret, and the edited text comes back."""
    with step("TYPE-015"):
        check(clear_composer(), "an empty typing box")
        type_text("first line")
        key("shift+Return")  # LIST-024's chat part: a new line in the typing box
        type_text("second")
        p = wait_for(probe_path, lambda p: composer(p).get("value") == "first line\nsecond", 3)
        check(composer(p).get("value") == "first line\nsecond", "Shift+Enter in the typing box makes a new line (LIST-024, chat part)")
        key("Left", "Left", "Left")
        calls = len(editor_calls())
        key("ctrl+o")
        p = wait_for(probe_path, lambda p: composer(p).get("value") == "edited at 2:4", 10)
        got = composer(p).get("value")
        check(got == "edited at 2:4", f"the editor's text replaces the typing box's ({got!r})")
        new = editor_calls()[calls:]
        call = new[0] if new else {}
        args = call.get("args") or []
        check(len(new) == 1 and call.get("name") == "nvim", f"$VISUAL ran once, not $EDITOR ({[c.get('name') for c in new]})")
        check(len(args) == 2 and args[0] == "+call cursor(2,4)", f"nvim gets the caret as +call cursor(2,4) (line 2, column 4): {args}")
        path = args[-1] if args else ""
        check(bool(re.fullmatch(r"/tmp/msg_\d+\.md", path)), f"the text went to a temporary msg_*.md ({path})")
        check(call.get("before") == "first line\nsecond", f"the file held the typing box's text ({call.get('before')!r})")
        check(call.get("file_mode") == "0o600", f"the file is the owner's only ({call.get('file_mode')})")
        check(bool(call.get("tty")) and call.get("term") == "xterm-256color", f"the editor ran in a terminal tab ({call.get('tty')}, TERM={call.get('term')})")
        time.sleep(0.5)
        check(path != "" and not os.path.exists(path), "the file is deleted once the text is back")
        t = term()
        check(t.get("shown") is None and not tabs(), "the editor's tab closed and the chat shows again")
        p = wait_for(probe_path, lambda p: composer(p).get("focused"), 3)
        check(composer(p).get("focused"), "the typing box has the focus again")

        # Shell mode: "!" goes in front, and a "!" coming back turns it on.
        check(clear_composer(), "an empty typing box")
        type_text("!")
        type_text("ls -la")
        p = wait_for(probe_path, lambda p: composer(p).get("mode") == "bang" and composer(p).get("value") == "ls -la", 3)
        check(composer(p).get("mode") == "bang", f"typing \"!\" first turns shell mode on ({composer(p).get('mode')})")
        set_mode("write !echo back")
        calls = len(editor_calls())
        key("ctrl+o")
        p = wait_for(probe_path, lambda p: composer(p).get("value") == "echo back", 10)
        new = editor_calls()[calls:]
        check(bool(new) and new[0].get("before") == "!ls -la", f"in shell mode the editor gets \"!\" in front ({new[0].get('before') if new else None!r})")
        check(composer(p).get("value") == "echo back" and composer(p).get("mode") == "bang", f"a returned \"!\" keeps shell mode, without the \"!\" ({composer(p).get('value')!r}, {composer(p).get('mode')})")
        set_mode("write plain again")
        key("ctrl+o")
        p = wait_for(probe_path, lambda p: composer(p).get("value") == "plain again", 10)
        check(composer(p).get("value") == "plain again" and composer(p).get("mode") != "bang",
              f"text without \"!\" leaves shell mode ({composer(p).get('value')!r}, {composer(p).get('mode')})")

        # An editor that fails gives nothing back, and says so.
        set_mode("fail")
        key("ctrl+o")
        p = wait_for(probe_path, lambda p: notice(p).get("text") == "exit status 3", 10)
        check(notice(p).get("kind") == "error" and notice(p).get("text") == "exit status 3", f"a failing editor shows \"exit status 3\" ({notice(p)})")
        check(composer(p).get("value") == "plain again", f"and the typing box keeps its text ({composer(p).get('value')!r})")
        set_mode("edit")

    with step("CMD-030"):
        # The palette's entry runs the same action; PAL is not merged here.
        check(clear_composer(), "an empty typing box")
        calls = len(editor_calls())
        type_text("from the palette")
        key("ctrl+o")
        p = wait_for(probe_path, lambda p: composer(p).get("value") == "edited at 1:17", 10)
        check(composer(p).get("value") == "edited at 1:17" and len(editor_calls()) == calls + 1,
              f"editor.open runs $VISUAL in a terminal tab and returns its text ({composer(p).get('value')!r})")
        not_run("CMD-030", "the palette entry \"Open External Editor\" runs after PAL merges (the action editor.open is registered)")
        clear_composer()


def step_new_terminal():
    global shell
    with step("LIST-005"):
        shell = open_shell()
        check(shell is not None, "a terminal tab opens with a shell")
        if not shell:
            return
        t = term()
        check(t.get("shown") == shell["id"], "the new tab is shown")
        title = shell["title"]
        if title != "Terminal":
            not_run("LIST-005", "Alt+T, the Terminals \"+\" and the folder picker \"New terminal in…\" run after LST merges; this tab came through the editor (\"Editor\")")
        check(shell["cwd"] == apps.PROJ, f"it starts in the chosen folder ({shell['cwd']})")


def step_env_shown():
    with step("LIST-025"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        line = run(s["id"], r"env | cut -d= -f1 | sort | tr '\n' ' '; printf 'END%s\n' 1", "END1")
        names = set((line or "").replace("END1", " ").split())
        leaked = sorted(n for n in names if n in REMOVED or n.startswith(REMOVED_PREFIXES))
        check(line is not None and not leaked, f"no name 2.25 removes reaches the tab ({leaked})")
        check({"TERM", "COLORTERM", "CRUSHTEST_KEPT", "PATH", "HOME"} <= names, "the login shell's environment is the tab's, with TERM and COLORTERM")
        vals = run(s["id"], r"""printf 'T=%s C=%s K=%s\n' "$TERM" "$COLORTERM" "$CRUSHTEST_KEPT" """, lambda line: line.startswith("T="))
        check(vals == "T=xterm-256color C=truecolor K=kept", f"TERM=xterm-256color and COLORTERM=truecolor are set ({vals!r})")
        found = run(s["id"], r"command -v crush || printf 'NO%s\n' CRUSH",
                    lambda line: line == "NOCRUSH" or (line.startswith("/") and line.endswith("/crush")), 5) or ""
        path = run(s["id"], r"""printf 'PATH=%s\n' "$PATH" """, lambda line: line.startswith("PATH=/")) or ""
        fakes = os.path.join(apps.ROOT, "tests", "fakes", "bin")
        check(fakes in path.split("=", 1)[-1].split(":"), "the sandbox's PATH reaches the tab through the login shell")
        print(f"INFO crush in a tab: {found!r}; DESKTOP_CRUSH={os.environ.get('DESKTOP_CRUSH')!r}", flush=True)
        notes.append(f"crush in a tab: {found!r} (PATH keeps the sandbox's folders: {fakes in path})")


def step_cwd_tracking():
    with step("LIST-020"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        run(s["id"], "mkdir -p /tmp/termdir && cd /tmp/termdir && printf 'CD%s\\n' 1", "CD1")
        t = wait_tab(s["id"], lambda t: t.get("cwd") == "/tmp/termdir")
        check(t.get("cwd") == "/tmp/termdir", f"the tab's folder follows cd (/proc/<pid>/cwd): {t.get('cwd')}")
        check(heavy().get("path_line") == "/tmp/termdir", "the head's path line names it")
        run(s["id"], r"printf '\033]7;file://localhost/var\007'; printf 'OSC%s\n' 1", "OSC1")
        t = wait_tab(s["id"], lambda t: t.get("osc_folder") == "/var")
        check(t.get("osc_folder") == "/var", f"OSC 7 from localhost is read ({t.get('osc_folder')})")
        check(t.get("cwd") == "/tmp/termdir", "and /proc still decides on Linux (terminal.go:91-98)")
        run(s["id"], r"printf '\033]7;file://elsewhere.example/etc\007'; printf 'OSC%s\n' 2", "OSC2")
        time.sleep(0.5)
        check(tab_of(s["id"]).get("osc_folder") == "/var", "OSC 7 from another computer is ignored")
        run(s["id"], "cd ~ && printf 'HOME%s\\n' 1", "HOME1")
        wait_tab(s["id"], lambda t: t.get("cwd") == "/tmp/home")
        h = heavy()
        check(h.get("path_line") == "~", f"the home folder reads as ~ ({h.get('path_line')!r})")
        run(s["id"], "cd /tmp/home/proj && printf 'BACK%s\\n' 1", "BACK1")


def step_scrollback():
    with step("LIST-021"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        tid = s["id"]
        run(tid, "seq 1 400; printf 'SEQ%s\\n' 1", "SEQ1")
        t = wait_tab(tid, lambda t: t.get("base_y", 0) > 100 and t.get("viewport_y") == t.get("base_y"))
        base, rows = t.get("base_y"), t.get("rows")
        key("shift+Prior")
        t = wait_tab(tid, lambda t: t.get("viewport_y") != base)
        check(t.get("viewport_y") == base - (rows - 1), f"Shift+PgUp scrolls a page, the height minus one line ({base} → {t.get('viewport_y')}, {rows} rows)")
        key("shift+Next")
        t = wait_tab(tid, lambda t: t.get("viewport_y") == base)
        check(t.get("viewport_y") == base and not t.get("scrolled_back"), "Shift+PgDn scrolls back down")
        mouse_over_terminal()
        xdo("click", 4)
        t = wait_tab(tid, lambda t: t.get("viewport_y") != base)
        check(t.get("viewport_y") == base - 3, f"a wheel notch scrolls 3 lines ({base} → {t.get('viewport_y')})")
        xdo("click", 5)
        wait_tab(tid, lambda t: t.get("viewport_y") == base)
        # New output keeps a scrolled-back view where it is (terminal.go:119-133).
        type_text("(sleep 2; seq 1 5; printf 'LATE%s\\n' 1) &")
        key("Return")
        time.sleep(0.5)
        key("shift+Prior")
        t = wait_tab(tid, lambda t: t.get("scrolled_back"))
        held = t.get("viewport_y")
        late = wait_line(tid, lambda line: line == "LATE1", 6)
        t = tab_of(tid)
        check(late is not None and t.get("viewport_y") == held and t.get("base_y") > held, f"new output leaves the scrolled-back view where it was ({held} → {t.get('viewport_y')})")
        key("shift+Next")
        # The alternate screen: the wheel and Shift+PgUp go to the program as keys.
        type_text(r"printf '\033[?1049h'; cat -v")
        key("Return")
        wait_tab(tid, lambda t: t.get("screen") == "alternate")
        mouse_over_terminal()
        xdo("click", 4)
        hit = wait_line(tid, lambda line: "^[[A^[[A^[[A" in line)
        check(hit is not None, "on the alternate screen a wheel notch sends ↑ three times")
        key("shift+Prior")
        hit = wait_line(tid, lambda line: "^[[5;2~" in line)
        check(hit is not None, "and Shift+PgUp goes to the program (CSI 5;2~)")
        key("ctrl+c")
        type_text(r"printf '\033[?1000h'; cat -v")
        key("Return")
        wait_tab(tid, lambda t: (t.get("modes") or {}).get("mouse") != "none")
        mouse_over_terminal()
        xdo("click", 4)
        hit = wait_line(tid, lambda line: "^[[M" in line)
        check(hit is not None, "a program that asked for the mouse gets the wheel as mouse reports")
        key("ctrl+c")
        run(tid, r"printf '\033[?1000l\033[?1049l'; printf 'ALT%s\n' 1", "ALT1")
        wait_tab(tid, lambda t: t.get("screen") == "normal")


def step_select_copy():
    with step("LIST-022"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        tid = s["id"]
        run(tid, "clear; printf 'alpha beta gamma\\n'", lambda line: line == "alpha beta gamma")
        type_text("(sleep 3; printf 'LATE%s\\n' 2) &")
        key("Return")
        time.sleep(0.3)
        got = select_word(tid, "alpha beta gamma", "beta")
        check(got == "beta", f"a left drag selects ({got!r})")
        clip = wait_for(probe_path, lambda p: clipboard(p).get("text") == "beta", 3)
        check(clipboard(clip).get("text") == "beta", "releasing copies it to the clipboard")
        late = wait_line(tid, lambda line: line == "LATE2", 6)
        check(late is not None and tab_of(tid).get("selection") == "beta", "the selection stays on its text while output arrives")
        h = heavy()
        r = row_index(h, "alpha beta gamma")
        if r is not None:
            x, y = cell_center(h, r, 2)
            xdo("mousemove", *screen_point(x, y))
            xdo("click", 1)
        t = wait_tab(tid, lambda t: t.get("selection") == "")
        check(t.get("selection") == "", "a new click clears it")
        got = select_word(tid, "alpha beta gamma", "gamma")
        first = wait_for(probe_path, lambda p: clipboard(p).get("text") == "gamma", 3)
        at = clipboard(first).get("at")
        set_clipboard("something else")
        key("ctrl+shift+c")
        again = wait_for(probe_path, lambda p: clipboard(p).get("at") != at, 3)
        check(clipboard(again).get("text") == "gamma" and clipboard(again).get("at") != at, "Ctrl+Shift+C copies the selection")
        run(tid, r"printf '\033[?1000h'; printf 'MOUSE%s\n' 1", "MOUSE1")
        wait_tab(tid, lambda t: (t.get("modes") or {}).get("mouse") != "none")
        h = heavy()
        r = row_index(h, "alpha beta gamma")
        if r is not None:
            drag_cells(h, r, 6, 9, shift=True)
        t = wait_tab(tid, lambda t: t.get("selection") == "beta", 3)
        check(t.get("selection") == "beta", f"Shift forces a selection while the program has the mouse ({t.get('selection')!r})")
        run(tid, r"printf '\033[?1000l'; printf 'MOUSE%s\n' 2", "MOUSE2")


def step_typing_jumps():
    with step("LIST-051"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        tid = s["id"]
        run(tid, "clear; printf 'alpha beta gamma\\n'; seq 1 200", lambda line: line == "200")
        got = select_word(tid, "alpha beta gamma", "beta", page_back=True)
        t = tab_of(tid)
        check(got == "beta" and t.get("scrolled_back"), f"a selection, scrolled back ({got!r})")
        type_text("x")
        t = wait_tab(tid, lambda t: not t.get("scrolled_back") and t.get("selection") == "")
        check(not t.get("scrolled_back") and t.get("selection") == "", "typing jumps back to the newest output and clears the selection")
        key("BackSpace")
        got = select_word(tid, "alpha beta gamma", "beta", page_back=True)
        set_clipboard("pasted-in")
        key("ctrl+shift+v")
        t = wait_tab(tid, lambda t: not t.get("scrolled_back"))
        check(got == "beta" and not t.get("scrolled_back") and t.get("selection") == "beta",
              f"pasting jumps back and keeps the selection ({got!r}, {t.get('selection')!r})")
        key("ctrl+u")
        wid = app_window()
        geo = H.window_geometry()
        if wid and geo:
            subprocess.run(["xdotool", "windowsize", str(wid), str(geo[2] - 120), str(geo[3] - 60)], capture_output=True)
            t = wait_tab(tid, lambda t: t.get("selection") == "")
            check(t.get("selection") == "", "resizing clears the selection")
            subprocess.run(["xdotool", "windowsize", str(wid), str(geo[2]), str(geo[3])], capture_output=True)
            time.sleep(0.5)


def step_paste_focus():
    with step("LIST-023"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        tid = s["id"]
        type_text(r"printf '\033[?2004h'; cat -v")
        key("Return")
        wait_tab(tid, lambda t: (t.get("modes") or {}).get("bracketed_paste"))
        set_clipboard("clip-one")
        key("ctrl+shift+v")
        hit = wait_line(tid, lambda line: "^[[200~clip-one^[[201~" in line)
        check(hit is not None, "Ctrl+Shift+V pastes, as a bracketed paste when the program asked for it")
        key("ctrl+c")
        type_text("cat -v")
        key("Return")
        time.sleep(0.4)
        set_clipboard("clip-two")
        key("shift+Insert")
        hit = wait_line(tid, lambda line: line == "clip-two")
        check(hit is not None, "Shift+Insert pastes the clipboard")
        primary = set_primary("primary-three")
        mouse_over_terminal()
        xdo("click", 2)
        hit = wait_line(tid, lambda line: "primary-three" in line)
        check(hit is not None, "a middle click pastes the primary selection")
        primary.terminate()
        key("ctrl+c")
        type_text("sh -c 'stty raw -echo; dd bs=1 count=1 2>/dev/null | od -An -c; stty sane'")
        key("Return")
        time.sleep(0.6)
        key("ctrl+v")
        hit = wait_line(tid, lambda line: line.strip() == "026")
        check(hit is not None, "Ctrl+V goes to the shell as its control character (026)")
        before = clipboard().get("at")
        run(tid, r"printf '\033]52;c;%s\007' $(printf osc52-copied | base64); printf 'OSC%s\n' 52", "OSC52")
        p = wait_for(probe_path, lambda p: clipboard(p).get("at") != before, 3)
        check(clipboard(p).get("text") == "osc52-copied", f"a program's OSC 52 copy sets the clipboard ({clipboard(p).get('text')!r})")
        type_text(r"printf '\033]52;c;?\007'; cat -v")
        key("Return")
        time.sleep(1.0)
        check(not any("52;" in line and "printf" not in line for line in lines_of(tid)), "an OSC 52 read gets no answer")
        key("ctrl+c")
        type_text(r"printf '\033[?1004h'; cat -v")
        key("Return")
        wait_tab(tid, lambda t: (t.get("modes") or {}).get("focus"))
        time.sleep(0.3)
        away = apps.focus_away()
        out = wait_line(tid, lambda line: "^[[O" in line)
        check(out is not None, "the window losing focus sends a focus-out report")
        away.close()
        focus_app()
        back = wait_line(tid, lambda line: "^[[O^[[I" in line or "^[[I" in line.split("^[[O", 1)[-1])
        check(back is not None, "and getting it back a focus-in report")
        not_run("LIST-023", "focus reports when another tab or the chat is shown run after LST merges (no way to switch views without the left list)")
        key("ctrl+c")
        run(tid, r"printf '\033[?1004l'; printf 'FOCUS%s\n' 1", "FOCUS1")
        run(tid, r"clear; printf '\033Pq#1;2;100;0;0#1~~~~~~~~-~~~~~~~~\033\\'; printf '\nSIX%s\n' 1", "SIX1")
        time.sleep(0.5)
        sixel = heavy().get("image_cells", 0)
        check(sixel > 0, f"a sixel picture shows inline ({sixel} cells)")
        run(tid, "clear; printf '\\033]1337;File=inline=1;width=4;height=2:%s\\007' " + PNG_1PX + "; printf '\\nIIP%s\\n' 1", "IIP1")
        time.sleep(0.5)
        iip = heavy().get("image_cells", 0)
        check(iip > 0, f"an iTerm2 inline picture shows ({iip} cells)")


def step_shift_enter():
    with step("LIST-024"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        tid = s["id"]
        type_text("sh -c 'stty raw -echo; dd bs=1 count=1 2>/dev/null | od -An -c; stty sane'")
        key("Return")
        time.sleep(0.6)
        key("shift+Return")
        hit = wait_line(tid, lambda line: line.strip() == "\\n")
        check(hit is not None, "Shift+Enter sends Ctrl+J (\\n) to the program")
        type_text("printf 'NL%s\\n' OK")
        key("shift+Return")
        check(wait_line(tid, lambda line: line == "NLOK") is not None, "and the shell runs the line, as for Enter")


def step_cursor():
    with step("LIST-056"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        tid = s["id"]
        focus_app()
        run(tid, r"printf '\033[6 q'; printf 'CUR%s\n' 6", "CUR6")
        t = wait_tab(tid, lambda t: (t.get("cursor") or {}).get("style") == "bar")
        check((t.get("cursor") or {}) == {"visible": True, "style": "bar", "blink": False}, f"a steady bar ({t.get('cursor')})")
        run(tid, r"printf '\033[3 q'; printf 'CUR%s\n' 3", "CUR3")
        t = wait_tab(tid, lambda t: (t.get("cursor") or {}).get("style") == "underline")
        check((t.get("cursor") or {}) == {"visible": True, "style": "underline", "blink": True}, f"a blinking underline ({t.get('cursor')})")
        run(tid, r"printf '\033[2 q'; seq 1 60; printf 'CUR%s\n' 2", "CUR2")
        t = wait_tab(tid, lambda t: (t.get("cursor") or {}).get("style") == "block")
        check((t.get("cursor") or {}) == {"visible": True, "style": "block", "blink": False}, f"a steady block ({t.get('cursor')})")
        key("shift+Prior")
        t = wait_tab(tid, lambda t: t.get("scrolled_back") and not (t.get("cursor") or {}).get("visible"))
        check(t.get("scrolled_back") and not (t.get("cursor") or {}).get("visible"), "the cursor hides while scrolled back")
        key("shift+Next")
        t = wait_tab(tid, lambda t: (t.get("cursor") or {}).get("visible"))
        check((t.get("cursor") or {}).get("visible"), "and shows again at the bottom")
        not_run("LIST-056", "the folder picker's text caret is LST's picker; runs after LST merges")


def step_theme_follow():
    with step("LIST-019"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        t = term()
        colors = (tab_of(s["id"], {"terminal": t}) or {}).get("colors") or {}
        tokens = t.get("tokens") or {}
        check(bool(tokens) and colors == tokens, f"the tab uses the theme's tokens ({colors} vs {tokens})")
        bg = heavy().get("background")
        want = css_rgb(tokens.get("background") or "")
        check(bool(want) and bg == want, f"its background is drawn in --paper ({bg}, {tokens.get('background')})")
        not_run("LIST-019", "a theme switch recoloring every tab at once runs after THM merges (no theme can be switched here)")


def css_rgb(color):
    """"#rrggbb" as the page's computed style writes it, else ""."""
    m = re.fullmatch(r"#([0-9a-fA-F]{2})([0-9a-fA-F]{2})([0-9a-fA-F]{2})", color.strip())
    return f"rgb({int(m[1], 16)}, {int(m[2], 16)}, {int(m[3], 16)})" if m else ""


def ask_close():
    key("alt+w")
    return wait_term(lambda t: t.get("close_box"), 3).get("close_box")


def step_close_box():
    with step("LIST-014"):
        s = ensure_shell()
        if not check(s is not None, "a shell tab"):
            return
        tid, title = s["id"], s["title"]
        box = ask_close()
        check(bool(box), "Alt+W on a shown terminal tab opens the close box")
        box = box or {}
        check(box.get("question") == f"Close “{title}”?", f"its question ({box.get('question')!r})")
        check(box.get("detail") == CLOSE_DETAIL, f"its detail ({box.get('detail')!r})")
        check(box.get("buttons") == ["Close", "Keep"] and box.get("selected") == "close", f"\"Close\" and \"Keep\", Close selected first ({box.get('buttons')}, {box.get('selected')})")
        if title != "Terminal":
            not_run("LIST-014", f"'Close “Terminal”?' itself needs a tab from the folder picker (LST); this one is titled {title!r}")
    with step("LIST-015"):
        def selected():
            return (term().get("close_box") or {}).get("selected")

        def press(k, want):
            key(k)
            got = wait_term(lambda t: (t.get("close_box") or {}).get("selected") == want, 2)
            return (got.get("close_box") or {}).get("selected") == want

        check(press("Tab", "keep") and press("shift+Tab", "close") and press("l", "keep") and press("h", "close")
              and press("Right", "keep") and press("Left", "close"), "←, →, Tab, Shift+Tab, H and L switch the buttons")
        before = list(lines_of(tid))
        type_text("q")
        set_clipboard("not-for-the-shell")
        key("ctrl+shift+v")
        time.sleep(0.5)
        check(bool(term().get("close_box")) and lines_of(tid) == before, "other keys and pastes do nothing while it is open")
        for keys, what in ((["Escape"], "Esc"), (["n"], "N"), (["shift+n"], "Shift+N"), (["Tab", "Return"], "Enter on Keep"), (["Tab", "space"], "Space on Keep")):
            if not term().get("close_box"):
                ask_close()
            key(*keys)
            t = wait_term(lambda t: not t.get("close_box"), 2)
            check(not t.get("close_box") and tab_of(tid, {"terminal": t}) is not None, f"{what} keeps the tab")
        pid = tab_of(tid).get("pid")
        ask_close()
        key("y")
        t = wait_term(lambda t: tab_of(tid, {"terminal": t}) is None, 3)
        check(tab_of(tid, {"terminal": t}) is None and not t.get("close_box"), "Y closes the tab at once")
        ended = wait_until(lambda: not pc.alive(pid), 6)
        check(ended, f"and its shell ends within 6 s (pid {pid})")
        not_run("LIST-015", "the keys reach the box through KIT's sheet keys after KIT merges; here the box takes them itself")


def wait_until(test, timeout):
    end = time.time() + timeout
    while time.time() < end:
        if test():
            return True
        time.sleep(0.1)
    return test()


def click_button(label):
    for b in heavy().get("close_buttons") or []:
        if b["label"] == label:
            r = b["rect"]
            H.click(r["x"] + r["w"] / 2, r["y"] + r["h"] / 2)
            return True
    return False


def step_close_buttons():
    global shell
    with step("LIST-043"):
        shell = open_shell()
        if not check(shell is not None, "a shell tab"):
            return
        tid = shell["id"]
        ask_close()
        check(click_button("Keep"), "the box's Keep is a button")
        t = wait_term(lambda t: not t.get("close_box"), 2)
        check(not t.get("close_box") and tab_of(tid, {"terminal": t}) is not None, "clicking Keep keeps the tab")
        pid = tab_of(tid).get("pid")
        ask_close()
        check(click_button("Close"), "the box's Close is a button")
        t = wait_term(lambda t: tab_of(tid, {"terminal": t}) is None, 3)
        check(tab_of(tid, {"terminal": t}) is None, "clicking Close closes the tab")
        check(wait_until(lambda: not pc.alive(pid), 6), f"and stops its shell (pid {pid})")
        not_run("LIST-043", "the row's \"×\" (shown on hover and on the shown row) is LST's row; runs after LST merges")


def step_exit_closes_tab():
    global shell
    with step("LIST-046"):
        shell = open_shell()
        if not check(shell is not None, "a shell tab"):
            return
        tid = shell["id"]
        type_text("exit")
        key("Return")
        t = wait_term(lambda t: tab_of(tid, {"terminal": t}) is None, 5)
        check(tab_of(tid, {"terminal": t}) is None, "the tab closes when its shell exits")
        check(t.get("shown") is None, "with no terminal left, the window shows its chat again")
        not_run("LIST-046", "showing the next tab, else the one before, needs two \"Terminal\" tabs from the folder picker; runs after LST merges")


def step_close_kinds():
    global shell
    with step("LIST-050"):
        shell = open_shell()
        if not check(shell is not None, "a shell tab"):
            return
        pid = shell.get("pid") or tab_of(shell["id"]).get("pid")
        wid = app_window()
        check(bool(wid) and pc.ask_close(wid), "the window gets its close request")
        closed_at = time.time()
        check(app.wait_exit(15) is not None, "the window closes (the last one ends the app)")
        check(wait_until(lambda: not pc.alive(pid), max(0.5, 6 - (time.time() - closed_at))), f"the tab's shell does not outlive the window by 6 s (pid {pid})")
        not_run("LIST-050", "closing a chat by kind (it only hides) is P4.1's and LST's")


def step_busy():
    with step("TYPE-015-busy"):
        check(start_app("busy"), "the busy chat")
        calls = len(editor_calls())
        focus_composer()
        key("ctrl+o")
        p = wait_for(probe_path, lambda p: notice(p).get("text") == BUSY, 5)
        check(notice(p).get("kind") == "warn" and notice(p).get("text") == BUSY, f"while the chat works Ctrl+O warns {BUSY!r} ({notice(p)})")
        time.sleep(1)
        check(len(editor_calls()) == calls and not tabs(), "and opens no editor")
        stop_app()


STEPS = [
    ("editor", step_editor),
    ("new_terminal", step_new_terminal),
    ("env_shown", step_env_shown),
    ("cwd_tracking", step_cwd_tracking),
    ("scrollback", step_scrollback),
    ("select_copy", step_select_copy),
    ("typing_jumps", step_typing_jumps),
    ("paste_focus", step_paste_focus),
    ("shift_enter", step_shift_enter),
    ("cursor", step_cursor),
    ("theme_follow", step_theme_follow),
    ("close_box", step_close_box),
    ("close_buttons", step_close_buttons),
    ("exit_closes_tab", step_exit_closes_tab),
    ("close_kinds", step_close_kinds),
]


def main():
    fixture = "--fixture" in os.environ.get("DESKTOP_ARGS", "")
    if not check(fixture, "runs on TERM's fixture (tests/fixtures/terminal-chats.json)"):
        finish()
    setup()
    if any(wanted(name) for name, _ in STEPS):
        if start_app("idle"):
            for name, fn in STEPS:
                if wanted(name) and app:
                    fn()
        stop_app()
    if wanted("busy"):
        step_busy()
    for note in notes:
        print(f"NOTE {note}", flush=True)
    if os.path.exists(LOG):
        shutil.copy(LOG, os.path.join(out_dir, "fake-editor.log"))
    finish()


if __name__ == "__main__":
    main()
