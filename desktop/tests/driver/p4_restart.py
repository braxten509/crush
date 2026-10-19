"""Branches survive a restart (desktop-design 3.7, B11): closed chats, links
and nesting are the same after quitting the app and starting it again.

  setup  p4_branch.py's: init answered No; stream2m active; seed_chat.py makes
         a five-turn chat S, opened from its row
  B11    in the real window: a branch B from the medium reply's words, a branch
         D of B, a sibling C from the short reply's words, then C closed with its
         ×; with D open, the list (ids, titles, depths), the closed set, the
         footer and the links are noted. The window is closed the way its close
         button closes it (WM_DELETE_WINDOW; the hidden screen has no window
         manager) and the app exits. It starts again with -s D: the list, the
         closed set, the footer and the links equal the noted ones, and S still
         shows both links (C's says Reopen) once opened.
  look   the branching look on the restarted window, in two states: D open
         (S and B expanded, B's row hovered) and S open with the medium reply's
         link in view. Checked from the probe's rects: every row's band keeps
         the same edges at depths 0 to 2; each shown × is centred on the
         chevrons' column and on its title's line; the footer's letters start
         where the day label's do; the head's path line starts with the title,
         keeps one line box with its parent link, and does not move between a
         chat and a branch; link words sit on their line's boxes. The captures
         and look/align.json (the align skill's screenshot manifest: the × and
         chevron icons, "Branched from"; titles, times, counts and link words
         are variable text it leaves out) go to look/ for the skill's checker,
         which runs outside the sandbox.

Usage: tests/check.sh p4_restart.py
"""
import importlib.util, json, math, os, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, shot, step

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("p4_branch", os.path.join(HERE, "p4_branch.py"))
pb = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pb)


def branch_from(app, key_, first_text, last_text, why):
    words = app.bring_into_view(key_, [first_text, last_text])
    first = pb.find_word(words, first_text)
    last = pb.find_word(words, last_text, after=first["i"]) if first else None
    if not (first and last):
        check(False, f"{why}: the words {first_text!r} to {last_text!r} are on screen")
        return None
    sid, _, _ = pb.make_branch(app, pb.word_point(first), pb.word_point(last, end=True), pb.word_point(pb.between(words, first, last), frac=0.5), why)
    check(bool(sid), f"{why}: the branch opened ({sid})")
    return sid


LOOK_ICON, LOOK_TEXT = 4, 2  # margins around icons and text: the checker needs background around the ink


def px_box(app, r, n=0):
    """[x, y, w, h] in screen pixels around the viewport rect r, grown by n."""
    ox, oy = app.screen(0, 0)
    x0, y0 = math.floor(ox + r["x"]) - n, math.floor(oy + r["y"]) - n
    x1, y1 = math.ceil(ox + r["x"] + r["w"]) + n, math.ceil(oy + r["y"] + r["h"]) + n
    return [x0, y0, x1 - x0, y1 - y0]


def clip(r, frame):
    """r cut to frame (a cut title's letters run on under its "…")."""
    if not r or not frame:
        return r
    x0, y0 = max(r["x"], frame["x"]), max(r["y"], frame["y"])
    x1, y1 = min(r["x"] + r["w"], frame["x"] + frame["w"]), min(r["y"] + r["h"], frame["y"] + frame["h"])
    return {"x": x0, "y": y0, "w": max(0, x1 - x0), "h": max(0, y1 - y0)}


def mid(r, axis):
    return r[axis] + r["w" if axis == "x" else "h"] / 2


def list_rows(app, rows, names, titles):
    """The manifest rows for the list: each shown × in its slot (its own
    28 px frame), each row's band with its chevron; titles and times are
    variable text."""
    out = []
    for r in rows:
        name = names.get(r.get("id"))
        if not name:
            continue
        if r.get("close_shown") and r.get("close_glyph"):
            out.append({"id": f"close-{name}", "label": f"{name}: the × slot", "rect": px_box(app, r["close"]), "source": "src/components/list/ChatRow.svelte:241",
                        "elements": [{"id": f"x-{name}", "kind": "icon", "label": "×", "rect": px_box(app, r["close_glyph"], LOOK_ICON), "column": "right-icons", "column_anchor": "center"}]})
        elements = []
        if r.get("chevron"):
            elements.append({"id": f"chevron-{name}", "kind": "icon", "label": "Chevron", "rect": px_box(app, r["chevron"], LOOK_ICON), "column": "right-icons", "column_anchor": "center"})
        title = clip(r.get("title_text"), r.get("title"))
        if title and title["w"]:
            elements.append({"id": f"title-{name}", "kind": "text", "text_kind": "variable", "label": "Title", "text": titles.get(r["id"], ""), "rect": px_box(app, title, LOOK_TEXT)})
        if r.get("time"):
            elements.append({"id": f"time-{name}", "kind": "text", "text_kind": "variable", "label": "Time", "text": "time", "rect": px_box(app, r["time"], LOOK_TEXT)})
        out.append({"id": f"row-{name}", "label": f"{name}'s row", "rect": px_box(app, r["row"]), "source": "src/components/list/ChatRow.svelte:91", "elements": elements})
    return out


def slot_checks(rows, names, why):
    """Each shown × on the chevrons' column and on its title's line; the table of both."""
    chevrons = [mid(r["chevron"], "x") for r in rows if r.get("chevron") and r.get("id") in names]
    table = []
    for r in rows:
        if r.get("id") not in names or not r.get("close_shown"):
            continue
        x, y = mid(r["close"], "x"), mid(r["close"], "y")
        off_x = max(abs(x - c) for c in chevrons) if chevrons else None
        off_y = y - mid(r["title"], "y")
        table.append({"row": names[r["id"]], "column_off": off_x, "line_off": off_y})
        check(off_x is not None and off_x <= 1 and abs(off_y) <= 1,
              f"look ({why}): {names[r['id']]}'s × is on the chevrons' column ({off_x if off_x is None else round(off_x, 2)} px) and its title's line ({off_y:.2f} px)")
    return table


def look(app, S, B, D, a2, out_dir):
    look_dir = os.path.join(out_dir, "look")
    os.makedirs(look_dir, exist_ok=True)
    names = {S: "S", B: "B", D: "D"}
    titles = {r.get("id"): r.get("title") or "" for r in app.rows()}
    geometry, captures = {}, []

    # D open: S and B expanded, D selected, B hovered (its × shows).
    check(app.open_chat(B) and app.open_chat(D), "look: D is open, under B under S")
    rows = (app.heavy().get("branch") or {}).get("rows") or []
    band_b = next((r["row"] for r in rows if r.get("id") == B), None)
    if not band_b:
        raise RuntimeError("B's row is not drawn")
    app.move(band_b["x"] + band_b["w"] / 2, band_b["y"] + band_b["h"] / 2)
    ok, branch, _ = pb.poll(lambda: app.heavy().get("branch") or {}, lambda b: any(r.get("id") == B and r.get("close_shown") for r in b.get("rows") or []), 3)
    check(ok, "look: B's × shows while its row is hovered")
    shot(look_dir, "list-d-open")
    rows = [r for r in branch.get("rows") or [] if r.get("id") in names]
    depth = {r.get("id"): r.get("depth") for r in app.rows()}
    bands = [(names[r["id"]], depth.get(r["id"]), r["row"]) for r in rows]
    lefts, rights = [b["x"] for _, _, b in bands], [b["x"] + b["w"] for _, _, b in bands]
    geometry["bands"] = [{"row": n, "depth": d, "left": b["x"], "right": b["x"] + b["w"]} for n, d, b in bands]
    check(len(bands) == 3 and max(lefts) - min(lefts) <= 1 and max(rights) - min(rights) <= 1,
          f"look: the rows' bands share both edges at depths {sorted(d for _, d, _ in bands)} (left {min(lefts)}..{max(lefts)}, right {min(rights)}..{max(rights)})")
    geometry["slots_d_open"] = slot_checks(rows, names, "D open")
    footer, head = branch.get("footer") or {}, branch.get("head") or {}
    off = (footer.get("ink") or {}).get("x", 0) - (footer.get("day") or {}).get("x", 0) if footer.get("ink") and footer.get("day") else None
    geometry["footer"] = {"text": footer.get("text"), "ink_left": (footer.get("ink") or {}).get("x"), "day_left": (footer.get("day") or {}).get("x")}
    check(off is not None and abs(off) <= 1, f"look: the footer's letters start where the day label's do ({off if off is None else round(off, 2)} px; {footer.get('text')!r})")
    words, link_text = head.get("words"), head.get("link_text")
    off_x = head["rect"]["x"] - head["title"]["x"] if head.get("rect") and head.get("title") else None
    same_line = bool(words and link_text) and abs(words["y"] - link_text["y"]) <= 1 and abs(words["h"] - link_text["h"]) <= 1
    geometry["head_branch"] = {"text": head.get("text"), "rect": head.get("rect"), "title": head.get("title"), "words": words, "link_text": link_text}
    check(off_x is not None and abs(off_x) <= 1, f"look: the path line starts with the title ({off_x} px)")
    check(same_line, f"look: \"Branched from\" and the parent's title share one line box ({words}, {link_text})")
    capture = {"id": "list-d-open", "method": "screenshot", "image": "list-d-open.png", "device_scale": 1,
               "surface": {"id": "list-d-open", "name": "D open under B under S, B hovered", "kind": "page"},
               "rows": list_rows(app, rows, names, titles)}
    if footer.get("rect"):
        capture["rows"].append({"id": "footer", "label": "List footer", "rect": px_box(app, footer["rect"]), "source": "src/components/list/ListFooter.svelte:9",
                                "elements": [{"id": "footer-text", "kind": "text", "text_kind": "variable", "label": "Closed count", "text": footer.get("text") or "", "rect": px_box(app, footer["ink"], LOOK_TEXT)}]})
    if words and head.get("glyph"):
        fixed = px_box(app, words, LOOK_TEXT)
        glyph = px_box(app, head["glyph"])
        capture["rows"].append({"id": "head-path", "label": "Head path line", "rect": px_box(app, head["rect"]), "source": "src/components/ChatHead.svelte:65",
                                "center": False, "center_reason": "The path line's line box under the title, not a control; its line box is checked by geometry",
                                "elements": [{"id": "branched-from", "kind": "text", "text_kind": "fixed", "label": "Branched from", "text": "BRANCHED FROM", "rect": fixed,
                                              "reference_rect": [glyph[0], fixed[1], glyph[2], fixed[3]], "reference_text": "B"},
                                             {"id": "parent-title", "kind": "text", "text_kind": "variable", "label": "Parent's title", "text": (head.get("text") or "")[len("Branched from"):].strip(),
                                              "rect": px_box(app, link_text, LOOK_TEXT)}]})
    captures.append(capture)

    # S open: its × and chevron on a top-level selected row; the medium reply's link in view.
    check(app.open_chat(S), "look: S is open")
    key_a2 = app.item_key(a2, "reply") or a2
    app.bring_into_view(key_a2, ["shared", "stolen"])
    app.park()
    h = app.heavy()
    shot(look_dir, "links-s")
    branch = h.get("branch") or {}
    rows = [r for r in branch.get("rows") or [] if r.get("id") in names]
    geometry["slots_s_open"] = slot_checks(rows, names, "S open")
    head_s = branch.get("head") or {}
    moved = pb.rect_moves(head_s.get("rect"), head.get("rect"))
    geometry["head_chat"] = {"text": head_s.get("text"), "rect": head_s.get("rect")}
    check(moved is not None and moved <= 1, f"look: the path line keeps its box between a branch and a chat ({moved} px; {head_s.get('text')!r})")
    links = [l for l in branch.get("shown") or [] if l.get("session_id") == B]
    on_line = []
    words_a2 = [w for w in ((h.get("chat") or {}).get("words") or []) if w.get("key") == key_a2]
    for r in (links[0].get("rects") if links else []):
        plain = [w["rect"] for w in words_a2 if r["y"] <= mid(w["rect"], "y") <= r["y"] + r["h"] and (w["rect"]["x"] + w["rect"]["w"] <= r["x"] or w["rect"]["x"] >= r["x"] + r["w"])]
        worst = max((max(abs(p["y"] - r["y"]), abs(p["h"] - r["h"])) for p in plain), default=None)
        on_line.append({"link": r, "plain_words": len(plain), "worst": worst})
    geometry["links"] = on_line
    check(bool(on_line) and all(l["plain_words"] and l["worst"] <= 1 for l in on_line),
          f"look: the link's words sit on their lines' boxes beside plain words ({[(l['plain_words'], l['worst']) for l in on_line]})")
    capture = {"id": "links-s", "method": "screenshot", "image": "links-s.png", "device_scale": 1,
               "surface": {"id": "links-s", "name": "S open, the medium reply's link in view", "kind": "page"},
               "rows": list_rows(app, rows, names, titles)}
    for i, l in enumerate(on_line):
        capture["rows"].append({"id": f"link-line-{i + 1}", "label": f"Link, line {i + 1}", "rect": px_box(app, l["link"]), "source": "src/components/ChatHead.svelte:133",
                                "center": False, "center_reason": "Words inside a reply's line, not a control",
                                "elements": [{"id": f"link-words-{i + 1}", "kind": "text", "text_kind": "variable", "label": "Link words", "text": "link", "rect": px_box(app, l["link"], LOOK_TEXT)}]})
    captures.append(capture)

    manifest = {"version": 2, "platform": "native", "surface": {"id": "p4-branching", "name": "Branching look (P4.1)", "kind": "app"},
                "inventory": [{"id": c["id"], "elements": [e["id"] for row in c["rows"] for e in row["elements"]]} for c in captures],
                "captures": captures}
    with open(os.path.join(look_dir, "align.json"), "w") as f:
        json.dump(manifest, f, indent=1)
    with open(os.path.join(look_dir, "geometry.json"), "w") as f:
        json.dump(geometry, f, indent=1)
    print(f"NOTE look: {look_dir}/align.json and geometry.json written", flush=True)


def state(app):
    """What must survive: the list as drawn, the closed set, the footer, the links."""
    rows = [(r.get("id"), r.get("title"), r.get("depth")) for r in app.rows()]
    branch = app.branch()
    links = sorted((l.get("id"), l.get("session_id"), l.get("parent_session_id"), l.get("mid"), l.get("start"), l.get("end"), l.get("origin")) for l in branch.get("links") or [])
    footer = ((app.heavy().get("branch") or {}).get("footer") or {}).get("text")
    return {"rows": rows, "closed": sorted(branch.get("closed") or []), "footer": footer, "links": links}


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    first_app = pb.App(pb.pc.AppRun.existing(probe_path, pb.PROJ))
    again = None
    try:
        with step("setup", out_dir):
            engine, S = pb.setup(first_app, port, title="Restart source")
            if not S:
                raise RuntimeError("no source chat")

        with step("B11", out_dir):
            app = first_app
            msgs = engine.messages(S)
            a1, a2 = msgs[1]["id"], msgs[3]["id"]
            B = branch_from(app, app.item_key(a2, "reply") or a2, "shared", "stolen", "B from the medium reply")
            D = None
            if B:
                b_a2 = next(m["id"] for m in engine.messages(B) if m["role"] == "assistant" and "Secure keeps" in pb.text_of(m))
                D = branch_from(app, app.item_key(b_a2, "reply") or b_a2, "Secure", "connections,", "D, a branch of B")
            check(app.open_chat(S), "back in S")
            C = branch_from(app, app.item_key(a1, "reply") or a1, "steady,", "answers", "C from the short reply")
            if not (B and D and C):
                raise RuntimeError("B11 needs its three branches")
            ok, _, _ = pb.poll(lambda: (app.heavy().get("branch") or {}).get("rows") or [], lambda rs: any(r.get("id") == C for r in rs), 5)
            row = next(r for r in (app.heavy().get("branch") or {}).get("rows") or [] if r.get("id") == C)
            app.click_rect(row["close"])
            ok, _, _ = pb.poll(lambda: app.branch().get("closed") or [], lambda ids: C in ids, 5)
            check(ok, "C is closed with its ×")
            # D's row shows once B is open (the open chat's line is expanded).
            check(app.open_chat(B) and app.open_chat(D), "D is open, so S and B are expanded")
            app.park()
            before = state(app)
            check(before["closed"] == [C] and before["footer"] == "1 closed · links reopen them" and len(before["links"]) == 3,
                  f"noted before the restart: {len(before['rows'])} rows, closed {before['closed']}, footer {before['footer']!r}, {len(before['links'])} links")
            depth = {r[0]: r[2] for r in before["rows"]}
            check(depth.get(S) == 0 and depth.get(B) == 1 and depth.get(D) == 2 and C not in depth, f"nesting before: S {depth.get(S)}, B {depth.get(B)}, D {depth.get(D)}, C hidden")

            check(app.run.close(), "the window closes the way its close button closes it, and the app exits")
            again = pb.pc.AppRun("again", out_dir, args=["-s", D])
            app = pb.App(again)
            check(app.connected(45), "the app starts again and joins the engine")
            app.place()
            ok, _, took = pb.poll(app.chat_id, lambda c: c == D, 20)
            check(ok, f"-s D opens D ({took:.1f} s)")
            ok, after, _ = pb.poll(lambda: state(app), lambda s: s["rows"] == before["rows"] and s["links"] == before["links"], 15, 0.5)
            check(after["rows"] == before["rows"], f"the list is the same: ids, titles and depths ({after['rows']})")
            check(after["closed"] == before["closed"], f"the closed set is the same ({after['closed']})")
            check(after["footer"] == before["footer"], f"the footer is the same ({after['footer']!r})")
            check(after["links"] == before["links"], f"the links are the same ({len(after['links'])})")
            check(app.open_chat(S), "S opens")
            key_a2, key_a1 = app.item_key(a2, "reply") or a2, app.item_key(a1, "reply") or a1
            app.bring_into_view(key_a2, ["shared", "stolen"])
            to_b = [l for l in app.shown_links() if l.get("session_id") == B]
            app.bring_into_view(key_a1, ["steady,", "answers"])
            to_c = [l for l in app.shown_links() if l.get("session_id") == C]
            check(len(to_b) == 1 and len(to_c) == 1, f"S still shows both links ({len(to_b)} to B, {len(to_c)} to C)")
            if to_c:
                title = pb.hover_title(app, to_c[0])
                check(bool(title) and title.startswith("Reopen "), f"C's link says Reopen, since C is still closed ({title!r})")

        with step("look", out_dir):
            look(app, S, B, D, a2, out_dir)
    finally:
        if again:
            again.close()


if __name__ == "__main__":
    main()
    finish()
