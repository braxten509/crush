"""Two app processes on one project (desktop-design 3.7, B16): a second
desktop window sees a new branch, its link and its closing: the branch
and the link through the engine's events, the closing through the settings
watcher (1.2.8).

  setup  p4_branch.py's in the run's own app (the first window): init answered
         No; stream2m active; seed_chat.py makes a five-turn chat S, opened
         from its row. A second app process starts on the same folder with its
         own probe (/tmp/probe-second.json). The windows are 1000 px wide and
         overlap by 400 px, so neither is ever wholly covered (a covered page
         may slow its timers). The second window opens S from its row and
         brings the medium reply's words into view.
  B16    the first window branches from the medium reply's words. Once the
         first window holds the branch's link, the second window lists the
         branch under S (depth 1) and draws the link over the same words. The
         first window then closes the branch with its ×; once its closed set
         holds the branch, the second window hides the row, counts it closed,
         and its link leads to a closed chat.
         Each window is raised and focused before it is used; the second app
         is closed the way its close button closes it, and reaped.

Usage: tests/check.sh p4_two_apps.py
"""
import importlib.util, os, sys, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, shot, step, xdo

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("p4_branch", os.path.join(HERE, "p4_branch.py"))
pb = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pb)

WIDE = 1000  # each window's width (the app's least is 900)
SECOND_X = 600  # the second window's left edge
ARRIVE = 10  # how long B16 waits for the other window to catch up, in seconds


def put(app, x, w=WIDE, h=pb.HEIGHT):
    """Moves the app's window to (x, 0) and sizes it; True once its page has the size."""
    box = app.box()
    if not box:
        return False
    xdo("windowmove", box[0], x, 0)
    xdo("windowsize", box[0], w, h)
    p = app.wait(lambda p: round((p.get("shell") or {}).get("w", 0)) == w and round((p.get("shell") or {}).get("h", 0)) == h, 10)
    time.sleep(0.4)
    return round((p.get("shell") or {}).get("w", 0)) == w


def squash(text):
    """The words without any white space: a link drawn across a paragraph break
    has no break in its spans."""
    return "".join((text or "").split())


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    first = pb.App(pb.pc.AppRun.existing(probe_path, pb.PROJ))
    second_run = second = None
    closed_cleanly = False
    try:
        with step("setup", out_dir):
            engine, S = pb.setup(first, port, title="Two windows source")
            if not S:
                raise RuntimeError("no source chat")
            check(put(first, 0), f"the first window is {WIDE} px wide at the screen's left edge")
            second_run = pb.pc.AppRun("second", out_dir)
            second = pb.App(second_run)
            check(second.connected(45), "a second app process on the same folder joins the engine")
            check(put(second, SECOND_X), f"its window is {WIDE} px wide at x {SECOND_X}, over 400 px of the first")
            ok, _, _ = pb.poll(lambda: second.row(S), bool, 15)
            check(ok, "the second window lists S")
            check(second.open_chat(S), "its row opens S in the second window")
            a2 = engine.messages(S)[3]["id"]
            seen = second.bring_into_view(second.item_key(a2, "reply") or a2, ["shared", "stolen"])
            check(bool(seen), "the second window shows the medium reply's words")

        with step("B16", out_dir):
            if not (second and S):
                raise RuntimeError("B16 needs both windows on S")
            # The branch: the first window makes it.
            words = first.bring_into_view(first.item_key(a2, "reply") or a2, ["shared", "stolen"])
            w1 = pb.find_word(words, "shared")
            w2 = pb.find_word(words, "stolen", after=w1["i"]) if w1 else None
            if not (w1 and w2):
                raise RuntimeError("the first window does not show the words")
            menu = pb.select_and_menu(first, pb.word_point(w1), pb.word_point(w2, end=True), pb.word_point(pb.between(words, w1, w2), frac=0.5))
            entry = pb.menu_entry(menu, "Branch from here")
            if not check(bool(entry) and not entry.get("disabled"), f"Branch from here is enabled in the first window ({entry})"):
                raise RuntimeError("no branch to watch")
            links_before = {l.get("id") for l in first.branch().get("links") or []}
            rows_before = {r.get("id") for r in second.rows()}
            shown_before = {l.get("session_id") for l in second.shown_links()}
            pb.click_menu(first, "Branch from here")
            _, B, _ = pb.poll(lambda: next((l.get("session_id") for l in first.branch().get("links") or []
                                            if l.get("origin") and l.get("id") not in links_before), None), bool, ARRIVE, 0.2)
            if not check(bool(B), f"the first window holds the new branch's link ({B})"):
                raise RuntimeError("no branch")
            stored = (engine.origin(B) or {}).get("highlight_text", "")
            _, row_id, _ = pb.poll(lambda: next((x.get("id") for x in second.rows() if x.get("id") not in rows_before and x.get("depth") == 1), None),
                                   bool, ARRIVE, 0.2)
            check(row_id == B, f"the second window lists the branch under S ({row_id})")
            _, link, _ = pb.poll(lambda: next((l for l in second.shown_links() if l.get("session_id") not in shown_before), None), bool, ARRIVE, 0.2)
            check(bool(link) and link.get("session_id") == B, f"and draws its link ({(link or {}).get('session_id')})")
            check(bool(link) and squash(link.get("text")) == squash(stored) and link.get("mid") == a2,
                  f"over the same words of the same reply ({(link or {}).get('text')!r})")
            ok, _, _ = pb.poll(first.chat_id, lambda c: c == B, 5)
            check(ok, "the first window shows the branch")
            second.focus()
            time.sleep(0.4)
            shot(out_dir, "B16-second")

            # The closing: the first window's ×; the second hears it through the settings file.
            rows = (first.heavy().get("branch") or {}).get("rows") or []
            row = next((r for r in rows if r.get("id") == B), None)
            if not check(bool(row) and row.get("close_shown"), "the branch's × shows on its selected row in the first window"):
                raise RuntimeError("no × to click")
            first.click_rect(row["close"])
            ok, _, _ = pb.poll(lambda: first.branch().get("closed") or [], lambda ids: B in ids, ARRIVE, 0.2)
            check(ok, "the first window closes the branch")
            ok, closed, _ = pb.poll(lambda: second.branch().get("closed") or [], lambda ids: B in ids, ARRIVE, 0.2)
            check(ok and closed == [B], f"the second window counts it closed ({closed})")
            ok, _, _ = pb.poll(second.rows, lambda rs: all(x.get("id") != B for x in rs), ARRIVE, 0.2)
            check(ok, "and hides its row")
            ok, _, _ = pb.poll(second.shown_links, lambda ls: any(l.get("session_id") == B for l in ls), 3)
            check(ok and B in (second.branch().get("closed") or []), "its link to the branch stays and leads to a closed chat")
            closed_cleanly = second_run.close()
            check(closed_cleanly, "the second app closes the way its close button closes it")
    finally:
        if second_run and not closed_cleanly:
            second_run.close()


if __name__ == "__main__":
    main()
    finish()
