"""Text and the red pen on pictures and video frames (owner, 2026-11-08:
"add text as well as mark specific frames on videos ... white text with a
black shadow"; "a << and >> button that moves 10 frames"). In the real window
on the hidden screen, with the fake phone (tests/fakes/bin/adb):

- a phone picture gets a note typed with the real keyboard and a pen line;
- a phone recording: >> and << move 10 frames; a pen line on frame 11 marks
  it by itself; a note typed there (its letters, "m" included, go to the
  note, not to the viewer's keys);
- the sent picture and the sent frame both hold white note pixels and red
  pen pixels; the frame not drawn on stays plain.

Usage: CHECK_ARGS="-s 7c1e5a90-3d2b-4f6e-9a41-0b8c2d4e6f01" \\
       tests/check.sh --fixture tests/fixtures/viewer-notes.json viewer_notes.py
(viewer-notes.json is model-menu.json with every model taking pictures)
"""
import base64, io, json, os, subprocess, sys, time

from PIL import Image

sys.path.insert(0, os.path.dirname(__file__))
from harness import check, click, finish, key, probe_eval, shot, type_text, wait_for, xdo, _screen

probe_path, out_dir = sys.argv[1], sys.argv[2]
GRAY = "#303030"

phone = os.path.join(os.environ["HOME"], "fake-phone")
os.makedirs(phone, exist_ok=True)
subprocess.run(["magick", "-size", "360x640", f"xc:{GRAY}", os.path.join(phone, "screen.png")], check=True)
subprocess.run(["ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "color=c=0x303030:s=360x640:r=30", "-t", "2",
                "-pix_fmt", "yuv420p", os.path.join(phone, "video.mp4")], check=True)


def run(script):
    return probe_eval(probe_path, "(async () => { try { window.__probeEval = await (async () => {" + script
                      + "})() ?? null; } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();")


def box(selector):
    return run(f"""const e = document.querySelector({json.dumps(selector)}); if (!e) return false;
      const r = e.getBoundingClientRect(); return {{ x: r.x, y: r.y, w: r.width, h: r.height }};""")


def wait_box(selector, timeout=20):
    end = time.time() + timeout
    while time.time() < end:
        b = box(selector)
        if isinstance(b, dict) and "error" in b:
            print("eval error:", b["error"], flush=True)
        if isinstance(b, dict) and b.get("w", 0) > 0:
            return b
        time.sleep(0.3)
    return None


def press(selector):
    wait_box(selector)
    time.sleep(0.4)  # let a menu or viewer finish opening before measuring
    b = wait_box(selector)
    if not check(bool(b), f"{selector} is on screen"):
        shot(out_dir, "viewer_notes_missing")
        print("composer:", run("return document.querySelector('[data-probe=\"composer\"]')?.innerText ?? 'none';"), flush=True)
        print("chips:", run("return [...document.querySelectorAll('.chip')].map((c) => c.outerHTML.slice(0, 200));"), flush=True)
        raise SystemExit(finish())
    click(b["x"] + b["w"] / 2, b["y"] + b["h"] / 2)
    time.sleep(0.4)


def drag(b, fy):
    sx, sy = _screen(b["x"] + b["w"] * 0.2, b["y"] + b["h"] * fy)
    ex = _screen(b["x"] + b["w"] * 0.8, 0)[0]
    xdo("mousemove", sx, sy)
    xdo("mousedown", 1)
    for i in range(1, 9):
        xdo("mousemove", round(sx + (ex - sx) * i / 8), sy)
        time.sleep(0.03)
    xdo("mouseup", 1)
    time.sleep(0.4)


def counts(img):
    data = img.tobytes()
    white = red = 0
    for i in range(0, len(data), 3):
        r, g, b = data[i], data[i + 1], data[i + 2]
        if r > 235 and g > 235 and b > 235:
            white += 1
        elif r > 180 and g < 100 and b < 100:
            red += 1
    return white, red


def sheet_counts(selector):
    got = run(f"""const c = document.querySelector({json.dumps(selector)}); if (!c) return null;
      const d = c.getContext('2d').getImageData(0, 0, c.width, c.height).data; let w = 0, r = 0;
      for (let i = 0; i < d.length; i += 4) {{ if (d[i + 3] < 200) continue;
        if (d[i] > 235 && d[i + 1] > 235 && d[i + 2] > 235) w++; else if (d[i] > 180 && d[i + 1] < 100 && d[i + 2] < 100) r++; }}
      return [w, r];""")
    return tuple(got) if isinstance(got, list) else (0, 0)


def until(read, ok, timeout=5):
    """read() once ok(value) holds, else its last value."""
    end = time.time() + timeout
    got = read()
    while time.time() < end and not ok(got):
        time.sleep(0.15)
        got = read()
    return got


def note(sheet, fy, text):
    click(sheet["x"] + sheet["w"] * 0.25, sheet["y"] + sheet["h"] * fy)
    time.sleep(0.4)
    focused = run("return document.activeElement?.matches('input.note') ?? false;")
    check(focused is True, "clicking with Text on gives a typing box with the caret in it")
    type_text(text)
    time.sleep(0.2)
    key("Return")
    time.sleep(0.4)


wait_for(probe_path, lambda p: (p.get("connection") or {}).get("state") == "connected", 40)

# A picture from the phone: a note and a line.
press("[data-phone-shot]")
press('[data-phone-pick="picture"]')
press('.chip[data-kind="picture"] .label')
check(bool(wait_box("[data-image-viewer]")), "the picture viewer opens")
press("[data-viewer-text]")
sheet = wait_box("[data-viewer-sheet]")
note(sheet, 0.3, "Hello")
check(box("[data-viewer-note]") is False, "Enter keeps the note and closes the typing box")
white, _ = sheet_counts("[data-viewer-sheet]")
check(white > 200, f"the note is drawn white on the picture ({white} white pixels)")
press("[data-viewer-pen]")
drag(sheet, 0.7)
_, red = sheet_counts("[data-viewer-sheet]")
check(red > 200, f"the pen line is drawn red ({red} red pixels)")
key("Escape")
check(until(lambda: box("[data-image-viewer]"), lambda b: b is False) is False, "Esc closes the picture viewer")

# A recording from the phone.
press("[data-phone-shot]")
press('[data-phone-pick="record"]')
check(bool(wait_box("[data-phone-recording]")), "the red dot shows while recording")
press("[data-phone-recording]")
press('.chip[data-kind="video"] .label')
check(bool(wait_box("[data-video-viewer]")), "the video viewer opens")
sheet = wait_box("[data-video-sheet]")
check(bool(sheet), "the drawing sheet lies over the paused video")


def frame():
    """The shown frame's index (0 for the first), from data-video-frame."""
    got = run("return document.querySelector('[data-video-frame]')?.dataset.videoFrame ?? null;")
    return int(got) if isinstance(got, str) and got.isdigit() else None


def marks():
    """The marked frames' indexes."""
    return run("return [...document.querySelectorAll('[data-video-marks] [data-frame]')].map((b) => Number(b.dataset.frame));") or []


press("[data-video-forward]")
check(until(frame, lambda f: f == 10) == 10, f">> moves 10 frames, to frame 11 ({frame()})")
press("[data-video-back]")
check(until(frame, lambda f: f == 0) == 0, f"<< moves back 10 frames ({frame()})")
press("[data-video-forward]")
press("[data-video-pen]")
drag(sheet, 0.7)
marked = until(marks, lambda m: len(m) > 0)
check(marked == [10], f"drawing on frame 11 marks it ({marked})")
press("[data-video-text]")
note(sheet, 0.3, "Mom")
marked = until(marks, lambda m: len(m) != 1, 0.4)
check(len(marked) == 1, f"the m in the note did not unmark the frame ({marked})")
white, red = sheet_counts("[data-video-sheet]")
check(white > 200 and red > 200, f"the frame shows the note and the line ({white} white, {red} red)")
press("[data-video-next]")
white, red = sheet_counts("[data-video-sheet]")
check(white == 0 and red == 0, f"frame 12 has its own empty sheet ({white} white, {red} red)")
key("Escape")
until(lambda: box("[data-video-viewer]"), lambda b: b is False)

# Send, and read the pictures as the engine got them.
api_log = probe_path + ".api.jsonl"
start = os.path.getsize(api_log) if os.path.exists(api_log) else 0
press('[data-probe="composer"] .field textarea')
type_text("see")
key("Return")
pictures = []
end = time.time() + 15
while time.time() < end and not pictures:
    time.sleep(0.5)
    if not os.path.exists(api_log):
        continue
    with open(api_log) as f:
        f.seek(start)
        for line in f:
            text = line.strip()
            if '"attachments"' not in text:
                continue
            found = []

            def walk(v):
                if isinstance(v, dict):
                    if str(v.get("mime_type") or v.get("mime") or "").startswith("image/"):
                        found.append(v)
                    for x in v.values():
                        walk(x)
                elif isinstance(v, list):
                    for x in v:
                        walk(x)

            walk(json.loads(text))
            pictures = found or pictures
check(len(pictures) == 2, f"the send carries the picture and the marked frame ({len(pictures)})")
for i, pic in enumerate(pictures):
    data = pic.get("content") or pic.get("base64") or ""
    img = Image.open(io.BytesIO(base64.b64decode(data))).convert("RGB")
    img.save(os.path.join(out_dir, f"viewer_notes_sent_{i + 1}.png"))
    white, red = counts(img)
    name = pic.get("file_name") or pic.get("name")
    check(white > 200 and red > 200, f"sent {name} holds the note and the line ({white} white, {red} red)")

finish()
