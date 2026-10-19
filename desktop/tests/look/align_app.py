#!/usr/bin/env python3
"""Alignment check of every screen in the real window (phase 7 look pass).

  align_app.py prepare   puts tests/out/look_align/in/collect.js together from
                         the align skill's helpers, app-marks.js and collect-tail.js
  align_app.py measure   turns each screen the driver left (NAME.png + NAME.json)
                         into a screenshot manifest, runs the skill's checker on
                         it, checks the sheets' shared edges, and writes
                         tests/out/look_align/summary.json

In between, tests/check.sh look_align.py runs the window driver in the sandbox
(the sandbox cannot see the skill, so prepare runs outside it). The boxes come
from the real WebKit page, the pixels from the real window's picture, so no
stand-in browser is involved. Exit codes: 0 every screen passes, 1 something
needs a look, 2 bad input.
"""
import glob
import json
import os
import subprocess
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
OUT = os.path.normpath(os.path.join(HERE, "..", "out", "look_align"))
ALIGN = os.path.expanduser("~/.agents/skills/align/scripts")
ICON_MARGIN = 4  # icons can paint to their box edge; the checker needs background around the ink
TEXT_MARGIN = 3  # underlined key letters reach below the text box
EDGE_SLACK = 1  # physical pixels two shared edges may differ by
EDGE_INDENT = 24  # a bigger difference is a deliberate indent, not a slip


def prepare():
    import re
    web = open(os.path.join(ALIGN, "align-web.mjs")).read()
    found = re.search(r"const MARK_HELPERS = `([\s\S]*?)`;", web)
    if not found:
        sys.exit("align-web.mjs no longer defines MARK_HELPERS; update align_app.py")
    # Each part in its own block, so their top-level names cannot clash.
    body = "\n".join("{\n" + part + "\n}" for part in [
        found.group(1),
        open(os.path.join(ALIGN, "align-browser.js")).read(),
        open(os.path.join(HERE, "app-marks.js")).read(),
        open(os.path.join(HERE, "collect-tail.js")).read(),
    ])
    script = ("(async () => { try { window.__probeEval = await (async () => {\n" + body
              + "\n})(); } catch (e) { window.__probeEval = { error: String(e && e.stack || e) }; } })();\n")
    os.makedirs(os.path.join(OUT, "in"), exist_ok=True)
    with open(os.path.join(OUT, "in", "collect.js"), "w") as f:
        f.write(script)
    print(os.path.join(OUT, "in", "collect.js"))


def grow(rect, n, width, height):
    x, y, w, h = rect
    x0, y0 = max(0, x - n), max(0, y - n)
    x1, y1 = min(width, x + w + n), min(height, y + h + n)
    return [x0, y0, x1 - x0, y1 - y0]


def manifest_for(collected, capture_id, image, size):
    width, height = size
    rows, ids = [], []
    inside = lambda r: r[0] >= 0 and r[1] >= 0 and r[0] + r[2] <= width and r[1] + r[3] <= height
    for row in collected["rows"]:
        # Rows scrolled out of the window are not in the picture.
        if not inside(row["rect"]) or not all(inside(e["rect"]) for e in row["elements"]):
            continue
        elements = []
        for e in row["elements"]:
            out = {k: e[k] for k in ("id", "kind", "text_kind", "group", "text", "column", "column_anchor", "source", "role", "text_lines") if e.get(k) is not None}
            if e["kind"] == "icon":
                out["rect"] = grow(e["rect"], ICON_MARGIN, width, height)
            else:
                tight = collected["text_rects"].get(e["id"]) or e["rect"]
                out["rect"] = grow(tight, TEXT_MARGIN, width, height)
                if e.get("reference_rect"):
                    rx, _, rw, _ = e["reference_rect"]
                    out["reference_rect"] = [rx, out["rect"][1], rw, out["rect"][3]]
                    out["reference_text"] = e["reference_text"]
            elements.append(out)
            ids.append(e["id"])
        rows.append({k: row[k] for k in ("id", "label", "rect", "source", "center", "center_reason") if row.get(k) is not None} | {"elements": elements})
    return {
        "version": 2,
        "surface": collected["surface"],
        "inventory": [{"id": capture_id, "elements": ids}],
        "captures": [{"id": capture_id, "method": "screenshot", "image": image, "device_scale": 1,
                      "rows": rows, "spacing_groups": collected.get("spacing_groups", [])}],
    }


def edge_findings(geometry):
    """Left and right edges inside a sheet that miss the common edge by more
    than EDGE_SLACK but less than EDGE_INDENT physical pixels."""
    parts = geometry.get("parts") or {}
    scale = geometry.get("scale") or 1
    found = []
    for side, value in (("left", lambda b: b[0]), ("right", lambda b: b[0] + b[2])):
        # Text ends wherever its words end, so only boxes have a right edge to share.
        names = ("title", "fields", "lists", "areas", "buttons", "help", "sections") if side == "left" else ("fields", "areas", "sections")
        boxes = [(name, i, b) for name in names for i, b in enumerate(parts.get(name) or [])]
        edges = [(name, i, round(value(b) * scale, 1)) for name, i, b in boxes]
        if len(edges) < 2:
            continue
        counts = {}
        for _, _, e in edges:
            counts[round(e)] = counts.get(round(e), 0) + 1
        common = max(counts, key=lambda k: (counts[k], -k if side == "left" else k))
        for name, i, e in edges:
            off = e - common
            if EDGE_SLACK < abs(off) < EDGE_INDENT:
                found.append({"side": side, "part": f"{name}[{i}]", "edge": e, "common": common, "off_px": round(off, 1)})
    return found


def measure():
    from PIL import Image
    summary = {"screens": {}, "missing": []}
    worst = 0
    for path in sorted(glob.glob(os.path.join(OUT, "*.json"))):
        name = os.path.splitext(os.path.basename(path))[0]
        if name in ("summary", "outside-before"):
            continue
        image = os.path.join(OUT, name + ".png")
        collected = json.load(open(path))
        if not isinstance(collected, dict) or collected.get("error") or not os.path.isfile(image):
            summary["missing"].append({"screen": name, "error": (collected or {}).get("error") if isinstance(collected, dict) else "no data"})
            worst = max(worst, 1)
            continue
        size = Image.open(image).size
        work = os.path.join(OUT, "measure", name)
        os.makedirs(work, exist_ok=True)
        with open(os.path.join(work, name + ".png"), "wb") as f:
            f.write(open(image, "rb").read())
        entry = {"rows": len(collected.get("rows") or []), "edges": edge_findings(collected.get("geometry") or {})}
        if collected.get("rows"):
            manifest = manifest_for(collected, name, name + ".png", size)
            with open(os.path.join(work, "align.json"), "w") as f:
                json.dump(manifest, f, indent=1)
            r = subprocess.run([sys.executable, os.path.join(ALIGN, "align.py"), os.path.join(work, "align.json"),
                                "--out", os.path.join(work, "results"), "--self-review"], capture_output=True, text=True)
            entry["checker_exit"] = r.returncode
            report = os.path.join(work, "results", "report.json")
            if os.path.isfile(report):
                rep = json.load(open(report))
                entry["passed"] = rep.get("passed")
                entry["coverage_complete"] = rep.get("coverage_complete")
                entry["review"] = [{"id": i["id"], "title": i["title"], "image": i.get("image")} for i in rep.get("review_items") or []]
                entry["failed"] = [{"id": c["id"], "offset_px": c.get("center_offset_px"), "gap_difference_px": c.get("gap_difference_px"),
                                    "shift_y_px": c.get("suggested_shift_y_px"), "shift_x_px": c.get("suggested_shift_x_px")}
                                   for c in rep.get("checks") or [] if not c.get("passed") and not c.get("excluded")]
                entry["errors"] = rep.get("errors") or []
                entry["needs_capture"] = rep.get("needs_capture") or []
            if r.returncode == 2:
                entry["checker_error"] = (r.stderr or r.stdout)[-800:]
            worst = max(worst, min(r.returncode, 2))
        if entry["edges"]:
            worst = max(worst, 1)
        summary["screens"][name] = entry
    with open(os.path.join(OUT, "summary.json"), "w") as f:
        json.dump(summary, f, indent=1)
    for name, e in summary["screens"].items():
        print(f"{name}: rows {e['rows']}, checker {e.get('checker_exit', '-')}, passed {e.get('passed')}, "
              f"review {len(e.get('review') or [])}, edges {len(e['edges'])}")
    for m in summary["missing"]:
        print(f"{m['screen']}: not measured ({m['error']})")
    return worst


if __name__ == "__main__":
    if len(sys.argv) != 2 or sys.argv[1] not in ("prepare", "measure"):
        sys.exit(__doc__)
    sys.exit(prepare() or 0 if sys.argv[1] == "prepare" else measure())
