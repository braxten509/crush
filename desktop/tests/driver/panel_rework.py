"""The right panel and the folder line (owner, 2026-10-19). In the real
window on the hidden screen.

Fixture leg (default): the panel shows TO-DO, SUBAGENTS, BACKGROUND and
SERVERS only while they have rows (owner, 2026-10-20), and no visuals; the chat's diagram and table are drawn in the
message, not as chips; the tray above the typing box has no to-do or
background entries and the left list no server rows; the typing box has no
focus ring; a new chat has no folder line above the typing box (owner,
2026-10-20); the panel has no folder section. Pictures:
panel.png, landing.png.

Engine leg ("switch" argument, no fixture): the init notice sits on the left,
in line with the typing box, in the question forms' sizes; picking another
folder from New chat switches this window to it; no second
window opens.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/model-menu.json panel_rework.py
       tests/check.sh panel_rework.py switch
"""
import os, subprocess, sys, time

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, probe_eval, wait_for, xdo

probe_path, out_dir = sys.argv[1], sys.argv[2]
SWITCH = "switch" in sys.argv[3:]


def windows():
    """The app's visible windows bigger than 100x100 (as no_flash.py)."""
    r = subprocess.run(["xdotool", "search", "--onlyvisible", "--class", "crush-desktop"], capture_output=True, text=True)
    found = []
    for wid in r.stdout.split():
        g = subprocess.run(["xdotool", "getwindowgeometry", "--shell", wid], capture_output=True, text=True)
        d = dict(l.split("=", 1) for l in g.stdout.split() if "=" in l)
        if "WIDTH" in d and int(d["WIDTH"]) * int(d["HEIGHT"]) > 100 * 100:
            found.append((wid, int(d["WIDTH"]) * int(d["HEIGHT"])))
    return [wid for wid, _ in sorted(found, key=lambda x: -x[1])]


def app_window(timeout=20):
    end = time.time() + timeout
    while time.time() < end:
        found = windows()
        if found:
            return found[0]
        time.sleep(0.3)
    return None


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def place():
    if not os.environ.get("DISPLAY"):
        check(bool(os.environ.get("WAYLAND_DISPLAY")), "the app runs on the hidden GPU screen")
        return None
    wid = app_window()
    if not check(wid is not None, "the app window is on the hidden screen"):
        finish()
    xdo("windowmove", wid, 0, 0)
    xdo("windowsize", wid, 1600, 992)
    time.sleep(1.5)
    return wid


def click_at(selector):
    if not os.environ.get("DISPLAY"):
        return bool(run(f"const el = document.querySelector({selector!r}); el?.click(); return !!el;"))
    point = run(f"const el = document.querySelector({selector!r}); if (!el) return null; const r = el.getBoundingClientRect(); return {{ x: r.x + r.width / 2, y: r.y + r.height / 2 }};")
    if not point or "x" not in point:
        return False
    xdo("mousemove", round(point["x"]), round(point["y"]))
    xdo("click", 1)
    return True


def picture(name):
    path = os.path.join(out_dir, name + ".png")
    command = ["import", "-window", wid, path] if wid else ["grim", path]
    subprocess.run(command, check=True)


wait_for(probe_path, lambda p: p.get("connection", {}).get("state") == "connected", 40)
wid = place()

if not SWITCH:
    time.sleep(1.5)
    got = run("""
      const panel = document.querySelector('[data-probe="panel"]');
      const tray = document.querySelector('[data-probe="tray"]');
      const field = document.querySelector('.composer .field');
      document.querySelector('.composer textarea, .composer [contenteditable]')?.focus();
      await new Promise((done) => setTimeout(done, 200));
      const ring = field ? getComputedStyle(field) : null;
      return {
        sections: [...(panel?.querySelectorAll('[data-section]') ?? [])].map((s) => s.dataset.section),
        emptyShown: [...(panel?.querySelectorAll('[data-section]:not([data-section="folder"])') ?? [])].filter((s) => !s.querySelector('li')).map((s) => s.dataset.section),
        panelText: panel?.innerText ?? '',
        folder: panel?.querySelector('[data-section="folder"] .name')?.innerText ?? '',
        figures: [...document.querySelectorAll('.figure-view.inline')].map((f) => f.dataset.kind),
        chips: document.querySelectorAll('.md-chip').length,
        tray: tray?.innerText ?? '',
        servers: document.querySelectorAll('nav.chats .servers-part').length,
        ring: ring ? ring.outlineStyle + ' ' + ring.outlineWidth : 'no field',
        focused: field?.contains(document.activeElement) ?? false,
      };
    """) or {}
    order = ["todos", "subagents", "background", "servers"]
    shown = got.get("sections") or []
    check("folder" not in shown and shown == [n for n in order if n in shown], f"the panel shows to-do, subagents, background, servers in that order, with no folder ({shown})")
    check(got.get("emptyShown") == [], f"an empty to-do, subagents, background or servers section is not shown ({got.get('emptyShown')})")
    check("VISUALS" not in got.get("panelText", "").upper(), "the panel has no visuals section")
    check(not got.get("folder"), "the panel has no folder picker")
    check("diagram" in got.get("figures", []) and "table" in got.get("figures", []), f"the diagram and the table are drawn in the message ({got.get('figures')})")
    check(got.get("chips") == 0, f"no visual chips in the chat ({got.get('chips')})")
    check("TO-DO" not in got.get("tray", "") and "background process" not in got.get("tray", ""), f"the tray has no to-do or background entries ({got.get('tray')!r})")
    check(got.get("servers") == 0, "the left list has no server rows")
    check(got.get("focused") and (got.get("ring", "").startswith("none") or got.get("ring", "").endswith(" 0px")), f"the focused typing box has no ring ({got.get('ring')}, focused {got.get('focused')})")
    picture("panel")

    run("window.__probe.runAction('chat.new'); return true;")
    time.sleep(1.2)
    line = run("return document.querySelectorAll('[data-probe=\"folder-line\"]').length;")
    check(line == 0, f"a new chat has no folder line above the typing box ({line})")
    picture("landing")
    finish()

# Engine leg: switch to another folder in this window.
other = "/tmp/home/other-folder"
os.makedirs(other, exist_ok=True)
subprocess.run(["git", "-C", other, "init", "-q"])
before = run("return document.querySelector('[data-probe=\"title-project\"]')?.textContent.trim() ?? '';")
time.sleep(1.5)
picture("init")
init = run("""
  const block = document.querySelector('[aria-label="Initialize this project"] .block, .block[role="group"]');
  const field = document.querySelector('.composer .field');
  const yep = block?.querySelector('.buttons button');
  const probe = document.createElement('span');
  probe.style.font = 'var(--t-reading)';
  probe.textContent = 'x';
  document.body.append(probe);
  const reading = parseFloat(getComputedStyle(probe).fontSize);
  probe.remove();
  if (!block || !field) return { missing: true };
  const q = block.querySelector('.question');
  return { blockLeft: block.getBoundingClientRect().left + parseFloat(getComputedStyle(block).paddingLeft), fieldLeft: field.getBoundingClientRect().left,
           question: parseFloat(getComputedStyle(q).fontSize), reading, yep: yep.getBoundingClientRect().height,
           pill: parseFloat(getComputedStyle(document.documentElement).fontSize) / 16 * 36 };
""") or {}
check(not init.get("missing") and abs(init["blockLeft"] - init["fieldLeft"]) <= 1, f"the init notice starts in line with the typing box ({init})")
check(not init.get("missing") and abs(init["question"] - init["reading"]) < 0.5, "the init question is in the question forms' size")
check(not init.get("missing") and abs(init["yep"] - init["pill"]) <= 1, "the init buttons are the question forms' Yes/No size")
if not check(click_at('nav.chats .new-chat:not(.new-terminal)'), "New chat opens the folder picker"):
    print("page:", run("return document.body.innerText.slice(0, 600);"), flush=True)
time.sleep(0.8)
if os.environ.get("DISPLAY"):
    xdo("key", "ctrl+u")
    xdo("type", "--delay", "20", other)
    time.sleep(0.5)
    xdo("key", "Return")
else:
    run(f"const input = document.querySelector('[data-sheet=\"folder_picker\"] input'); input.value = {other!r}; input.dispatchEvent(new Event('input', {{bubbles:true}})); await new Promise(resolve => setTimeout(resolve, 500)); input.dispatchEvent(new KeyboardEvent('keydown', {{key:'Enter', bubbles:true}})); return true;")
# The page reloads and joins the other folder's engine.
end = time.time() + 40
now = before
while time.time() < end:
    time.sleep(1)
    try:
        now = run("return document.querySelector('[data-probe=\"title-project\"]')?.textContent.trim() ?? '';") or now
    except Exception:
        continue
    if now == "other-folder":
        break
check(before != "other-folder" and now == "other-folder", f"this window switched from {before!r} to {now!r}")
time.sleep(2)
if wid:
    check(len(windows()) == 1, f"no second window opened ({len(windows())} windows)")
check(run("return document.querySelector('[data-section=\"folder\"]') === null;") is True, "the folder stays absent from the right panel")
finish()
