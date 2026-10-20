#!/usr/bin/env python3
"""The title bar's align-skill manifests from tests/driver/titlebar_align.py's
captures, and the skill's checker on each (run outside the sandbox: the skill
lives in the owner's home).

The bar's left run (the heart, the chat's title, the folder, the branch) is one
row; each window button is a row of its own (its hover box) holding its icon.
The title, folder and branch are data, so they are variable text: the checker
measures the icons and the rows, and leaves the words' ink out.

Usage: python3 tests/look/titlebar_manifest.py tests/out/titlebar_align
Exit: the worst of the checker's codes (0 pass, 1 needs attention, 2 bad input).
"""
import json, os, shutil, subprocess, sys

ALIGN = os.path.expanduser("~/.agents/skills/align/scripts/align.py")
BUTTONS = ["panel", "pin", "minimize", "maximize", "close"]


def px(rect, origin, grow=0):
    x, y, w, h = rect
    return [round(x + origin[0]) - grow, round(y + origin[1]) - grow, round(w) + 2 * grow, round(h) + 2 * grow]


ICON_MARGIN = 3  # icons paint to their box edge; the checker needs background around the ink


def text_element(name, part, origin, group):
    first = part.get("first")
    return {"id": name, "kind": "text", "text_kind": "variable", "group": group, "label": name.title(),
            "text": part["text"].strip(), "rect": px(part["rect"], origin),
            "reference_rect": px(first, origin), "reference_text": part["text"].strip()[0]}


def manifest(data, name):
    p, o = data["parts"], data["origin"]
    bar = p["bar"]
    last_text = next(p[k]["rect"] for k in ("branch", "project", "chat") if p.get(k))
    left = [bar[0], bar[1], last_text[0] + last_text[2] - bar[0] + 4, bar[3]]
    elements = [{"id": "heart", "kind": "icon", "group": "left", "label": "App icon", "rect": px(p["app_icon"], o, ICON_MARGIN)}]
    elements += [text_element(k, p[k], o, "left") for k in ("chat", "project", "branch") if p.get(k)]
    rows = [{"id": "left", "label": "Title, folder and branch", "rect": px(left, o), "elements": elements}]
    for button, rect in zip(BUTTONS, p["buttons"]):
        rows.append({"id": button, "label": f"{button.title()} button", "rect": px(rect, o),
                     "elements": [{"id": f"{button}-icon", "kind": "icon", "group": button, "label": button.title(), "rect": px(p[button], o, ICON_MARGIN)}]})
    capture = f"titlebar-{name}"
    return {"version": 2, "surface": {"id": "titlebar", "name": "Title bar", "kind": "page"},
            "inventory": [{"id": capture, "elements": [e["id"] for r in rows for e in r["elements"]]}],
            "captures": [{"id": capture, "method": "screenshot", "image": data["image"], "rows": rows}]}


def main(out):
    worst = 0
    for name in ("compact", "picture"):
        data = json.load(open(os.path.join(out, f"titlebar-{name}.json")))
        work = os.path.join(out, f"align-{name}")
        os.makedirs(work, exist_ok=True)
        shutil.copy(os.path.join(out, data["image"]), os.path.join(work, data["image"]))
        path = os.path.join(work, "align.json")
        json.dump(manifest(data, name), open(path, "w"), indent=1)
        code = subprocess.run([sys.executable, ALIGN, path, "--out", os.path.join(work, "results"), "--self-review"]).returncode
        print(f"{name} (zoom {data['zoom']}): exit {code}", flush=True)
        worst = max(worst, code)
    return worst


if __name__ == "__main__":
    sys.exit(main(sys.argv[1]))
