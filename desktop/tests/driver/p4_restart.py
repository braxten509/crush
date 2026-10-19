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

Usage: tests/check.sh p4_restart.py
"""
import importlib.util, os, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, step

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
    finally:
        if again:
            again.close()


if __name__ == "__main__":
    main()
    finish()
