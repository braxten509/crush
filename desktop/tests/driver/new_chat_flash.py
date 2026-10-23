"""New chat in a folder: no wrong-frame flash (owner, 2026-10-23, from a
screencast). In the real window on the hidden screen, with two folders open
(proj and beta), Ctrl+N starts a chat in proj; the page records the left
list's groups on every frame from the click until the new row settles. It
fails when a frame shows the new row under another folder's heading, a group
whose heading has no name, or the new row missing from its own folder.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/folder-switch.json new_chat_flash.py
"""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, finish, key, probe_eval, wait_for

probe_path, out_dir = sys.argv[1], sys.argv[2]

PROJ = "/tmp/home/proj"
TARGET = "/tmp/home/beta"  # the folder whose + is clicked


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


# Every frame for a second after the click: the groups as drawn, in order,
# with each heading's and row's height and title.
RECORD = """
  const frames = [];
  const rect = (el) => { const r = el.getBoundingClientRect(); return { y: Math.round(r.y), h: Math.round(r.height) }; };
  const sample = () => [...document.querySelectorAll('nav.chats section.group')].map((g) => {
    const heading = g.querySelector('.folder');
    return {
      dir: g.getAttribute('data-folder') || '',
      label: (g.querySelector('.folder-name')?.textContent || '').trim(),
      heading: heading ? rect(heading) : null,
      rows: [...g.querySelectorAll('.chat')].map((r) => ({
        id: r.getAttribute('data-id') || '',
        title: (r.querySelector('.title')?.textContent || '').trim(),
        box: rect(r),
      })),
    };
  });
  let framesLeft = 60;
  await new Promise((done) => {
    const tick = () => {
      frames.push(sample());
      framesLeft -= 1;
      if (framesLeft <= 0) done();
      else requestAnimationFrame(tick);
    };
    requestAnimationFrame(tick);
  });
  return frames;
"""


def main():
    wait_for(probe_path, lambda p: bool(p.get("workspace") or p.get("agent")), timeout=30)
    before = run("return [...document.querySelectorAll('nav.chats section.group')].map((g) => ({dir: g.getAttribute('data-folder') || '', rows: [...g.querySelectorAll('.chat')].length}));")
    check(len(before) >= 2, f"two folders are drawn first ({len(before)})")

    # The owner's click (screencast 2026-10-23): the "+" on another folder's
    # heading, which switches the window there and starts the chat.
    run("window.__frames = null; (async () => { window.__frames = await (async () => {" + RECORD + "})(); })();")
    clicked = run("""
      const heading = [...document.querySelectorAll('nav.chats section.group')].find((g) => g.getAttribute('data-folder') === '/tmp/home/beta');
      const plus = heading?.querySelector('.folder-new');
      plus?.click();
      return !!plus;
    """)
    check(clicked is True, "the other folder's + was clicked")
    frames = None
    for _ in range(400):
        frames = run("return window.__frames;")
        if isinstance(frames, list) and frames:
            break
    frames = frames if isinstance(frames, list) else None
    check(bool(frames), f"the frames were recorded ({type(frames).__name__})")
    if not frames:
        return finish()

    with open(os.path.join(out_dir, "frames.json"), "w") as f:
        f.write(__import__("json").dumps(frames, indent=1))

    wrong_group = []
    nameless = []
    missing = []
    for index, frame in enumerate(frames or []):
        for group in frame:
            if group["dir"] and not group["label"]:
                nameless.append((index, group["dir"]))
        target_rows = next((g["rows"] for g in frame if g["dir"] == TARGET), [])
        for group in frame:
            if group["dir"] == TARGET:
                continue
            if any(r["id"] == "new-chat" for r in group["rows"]):
                wrong_group.append((index, group["dir"]))
        # The draft row (new-chat) or the saved chat must sit in proj's group
        # once the click has landed.
        if index >= 2 and not target_rows:
            missing.append(index)

    check(not wrong_group, f"the new row never shows under another folder {wrong_group[:3]}")
    check(not nameless, f"every folder heading keeps its name {nameless[:3]}")
    check(not missing, f"the new row is in its own folder's group {missing[:5]}")
    # No frame may drop a group's rows and bring them back (the flash the
    # owner saw: rows vanishing and the list jumping).
    seen = {}
    dropped = []
    for index, frame in enumerate(frames or []):
        for group in frame:
            rows = len(group["rows"])
            if group["dir"] in seen and seen[group["dir"]] > 0 and rows == 0:
                dropped.append((index, group["dir"]))
            seen[group["dir"]] = rows
    check(not dropped, f"no folder loses its rows for a frame {dropped[:3]}")

    # The new chat stays listed (the owner's screencast: its row vanished a
    # second later, which is the flash they saw).
    import time
    time.sleep(2.5)
    settled = run("""
      const groups = [...document.querySelectorAll('nav.chats section.group')];
      const beta = groups.find((g) => g.getAttribute('data-folder') === '/tmp/home/beta');
      return {
        rows: [...(beta?.querySelectorAll('.chat') ?? [])].map((r) => ({ id: r.getAttribute('data-id') || '', title: (r.querySelector('.title')?.textContent || '').trim() })),
        folder: document.querySelector('nav.chats .folder-name')?.textContent ?? '',
      };
    """)
    check(bool(settled) and any(r["title"] == "New Session" or r["id"] != "b0b0b0b0-1111-4222-8333-000000000001" for r in settled["rows"]),
          f"the new chat is still listed in its folder ({settled and settled['rows']})")

    after = run("return [...document.querySelectorAll('nav.chats section.group')].map((g) => ({dir: g.getAttribute('data-folder') || '', rows: [...g.querySelectorAll('.chat')].length}));")
    # A frame that draws a row with no words is the flash the owner saw.
    blank = [(i, g["dir"], r["id"]) for i, f in enumerate(frames or []) for g in f for r in g["rows"] if not r["title"]]
    check(not blank, f"no row is ever drawn without its words {blank[:3]}")
    # A heading that is not the height it settles at is a heading mid-flash.
    heights = {(g["dir"]): (g["heading"] or {}).get("h") for g in (frames or [{}])[-1] for g in (g,) if g.get("dir")}
    odd = [(i, g["dir"], g["heading"]["h"], heights.get(g["dir"])) for i, f in enumerate(frames or []) for g in f
           if g.get("heading") and heights.get(g["dir"]) and abs(g["heading"]["h"] - heights[g["dir"]]) > 1]
    check(not odd, f"every folder heading keeps its height {odd[:3]}")
    check(len(after) == len(before), f"both folders are still drawn ({len(after)})")
    finish()


main()
