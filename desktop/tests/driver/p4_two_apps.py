"""Two app processes on one project (desktop-design 3.7, B16): a second
desktop window sees a new branch, its link and its closing at once: the branch
and the link through the engine's events, the closing through the settings
watcher (1.2.8).

  setup  p4_branch.py's in the run's own app (the first window): init answered
         No; stream2m active; seed_chat.py makes a five-turn chat S, opened
         from its row. A second app process starts on the same folder with its
         own probe (/tmp/probe-second.json). The windows are 1000 px wide and
         overlap by 400 px, so neither is ever wholly covered (a covered page
         may slow its timers). The second window opens S from its row and
         brings the medium reply's words into view.
  B16    the first window branches from the medium reply's words. From the
         moment the first window holds the branch's link (its probe report's
         own time stamp), the second window lists the branch under S (depth 1)
         within 1 s and draws the link over the same words within 1 s. The
         first window then closes the branch with its ×; from the moment its
         closed set holds the branch, the second window hides the row and
         counts it in the footer within 1 s, and its link then says Reopen.
         Each window is raised and focused before it is used; the second app
         is closed the way its close button closes it, and reaped.

Usage: tests/check.sh p4_two_apps.py
"""
import importlib.util, json, os, sys, threading, time

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import harness
from harness import check, finish, read_probe, shot, step, xdo

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("p4_branch", os.path.join(HERE, "p4_branch.py"))
pb = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pb)

WIDE = 1000  # each window's width (the app's least is 900)
SECOND_X = 600  # the second window's left edge
LAG = 1.0  # B16's bar, in seconds


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


def light(app):
    return lambda: read_probe(app.probe, tries=1)


def heavy(app):
    """The whole report with the heavy fields: probe_heavy with a finer wait,
    so a drawing is seen near the report that first holds it."""
    def read():
        rid = next(harness._request_ids)
        side = app.probe + ".request.json"
        with open(side + ".tmp", "w") as f:
            json.dump({"id": rid}, f)
        os.replace(side + ".tmp", side)
        end = time.time() + 5
        while time.time() < end:
            p = read_probe(app.probe, tries=1)
            if p.get("request_id") == rid:
                return p
            time.sleep(0.02)
        return {}
    return read


class First:
    """Reads in a thread until test(report) gives a value; keeps the value and
    the report's own time stamp ("at", the page's clock) in seconds."""

    def __init__(self, read, test, timeout=10):
        self.value = self.at = None
        self.thread = threading.Thread(target=self._run, args=(read, test, timeout), daemon=True)
        self.thread.start()

    def _run(self, read, test, timeout):
        end = time.time() + timeout
        while time.time() < end:
            report = read() or {}
            value = test(report)
            if value:
                self.value, self.at = value, report.get("at", 0) / 1000
                return
            time.sleep(0.02)

    def result(self):
        self.thread.join()
        return self.value, self.at


def lag(later, ref):
    return None if later is None or ref is None else later - ref


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
            made = First(light(first), lambda r: next((l.get("session_id") for l in (r.get("branch") or {}).get("links") or []
                                                      if l.get("origin") and l.get("id") not in links_before), None))
            listed = First(light(second), lambda r: next((x.get("id") for x in (r.get("list") or {}).get("rows") or []
                                                         if x.get("id") not in rows_before and x.get("depth") == 1), None))
            drawn = First(heavy(second), lambda r: next((l for l in ((r.get("heavy") or {}).get("branch") or {}).get("shown") or []
                                                        if l.get("session_id") not in shown_before), None))
            clicked = time.time()
            pb.click_menu(first, "Branch from here")
            B, made_at = made.result()
            row_id, listed_at = listed.result()
            link, drawn_at = drawn.result()
            if not check(bool(B), f"the first window holds the new branch's link ({B}, {lag(made_at, clicked) or 0:.2f} s after the click)"):
                raise RuntimeError("no branch")
            stored = (engine.origin(B) or {}).get("highlight_text", "")
            took = lag(listed_at, made_at)
            check(row_id == B and took is not None and took <= LAG,
                  f"the second window lists the branch under S within {LAG:.0f} s ({row_id}, {took if took is None else round(took, 2)} s)")
            took = lag(drawn_at, made_at)
            check(bool(link) and link.get("session_id") == B and took is not None and took <= LAG,
                  f"and draws its link within {LAG:.0f} s ({took if took is None else round(took, 2)} s)")
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
            shut = First(light(first), lambda r: B in ((r.get("branch") or {}).get("closed") or []))
            hidden = First(light(second), lambda r: B in ((r.get("branch") or {}).get("closed") or [])
                           and all(x.get("id") != B for x in (r.get("list") or {}).get("rows") or [])
                           and (r.get("list") or {}).get("closed_count") == 1)
            first.click_rect(row["close"])
            _, shut_at = shut.result()
            _, hidden_at = hidden.result()
            check(shut_at is not None, "the first window closes the branch")
            took = lag(hidden_at, shut_at)
            check(took is not None and took <= LAG, f"the second window hides its row and counts it closed within {LAG:.0f} s ({took if took is None else round(took, 2)} s)")
            h = second.heavy()
            footer = ((h.get("branch") or {}).get("footer") or {}).get("text")
            check(footer == "1 closed · links reopen them", f"its footer says so ({footer!r})")
            ok, shown, _ = pb.poll(second.shown_links, lambda ls: any(l.get("session_id") == B and (l.get("title") or "").startswith("Reopen ") for l in ls), 3)
            check(ok, f"its link now says Reopen ({[l.get('title') for l in shown if l.get('session_id') == B]})")
            closed_cleanly = second_run.close()
            check(closed_cleanly, "the second app closes the way its close button closes it")
    finally:
        if second_run and not closed_cleanly:
            second_run.close()


if __name__ == "__main__":
    main()
    finish()
