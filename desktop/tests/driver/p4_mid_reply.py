"""Branching mid-reply in the real window on the engine (desktop-design 3.7,
B7 and B8), with scripts/stream2m.json's "Stream for two minutes" (about 120 s
of text and tool calls) as the source chat's running reply.

  setup  init answered No; stream2m active; seed_chat.py makes a two-turn chat S
         (short, medium), opened from its row; "Stream for two minutes" is sent
         from the typing box, and the fake server's time scale is raised to 3
         so every step of the reply streams long enough to be selected
  B7     words of the reply still being written: Branch from here is disabled
         with "Wait for the reply to finish"; words of the earlier finished
         reply branch at once while the source streams, and every message of
         the branch is finished; at the end the source's reply finishes
         undisturbed: the engine holds the script's whole reply, each message
         finished, none canceled or failed
  B8     a send in the branch right away is answered while the source still
         streams (it never waits for the source)

Usage: tests/check.sh p4_mid_reply.py
"""
import importlib.util, json, os, sys, time
from urllib import request as urlrequest

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from harness import check, finish, key, step, type_text

HERE = os.path.dirname(os.path.abspath(__file__))
spec = importlib.util.spec_from_file_location("p4_branch", os.path.join(HERE, "p4_branch.py"))
pb = importlib.util.module_from_spec(spec)
spec.loader.exec_module(pb)

LONG = "Stream for two minutes"
SLOW = 3  # the fake server's time scale while the driver selects words in the running reply


def control(port, body):
    req = urlrequest.Request(f"http://127.0.0.1:{port}/_fake/control", data=json.dumps(body).encode(), method="POST",
                             headers={"Content-Type": "application/json"})
    with urlrequest.urlopen(req, timeout=5) as resp:
        return json.load(resp)


def script_reply(when):
    with open(os.path.join(pb.ROOT, "tests", "fakes", "scripts", "stream2m.json")) as f:
        for turn in json.load(f)["turns"]:
            if turn["when"] == when:
                return "".join(s.get("text", "") for s in turn["steps"])
    raise SystemExit(f"stream2m.json has no turn {when!r}")


def finished(m):
    return any(p.get("type") == "finish" for p in m.get("parts") or [])


def finish_reasons(ms):
    return [(p.get("data") or {}).get("reason") for m in ms for p in m.get("parts") or [] if p.get("type") == "finish"]


def after_prompt(ms, prompt):
    """The messages after the user message holding prompt."""
    at = next((i for i, m in enumerate(ms) if m["role"] == "user" and prompt in pb.text_of(m)), None)
    return [] if at is None else ms[at + 1:]


def send(app, text):
    """Types text into the typing box with the real keyboard and presses Enter."""
    chat = app.heavy().get("chat") or {}
    view, top = chat.get("view"), chat.get("composer_top")
    if not view or top is None:
        return False
    app.click(view["x"] + 200, top + 25)
    type_text(text)
    ok, _, _ = pb.poll(app.composer, lambda c: (c.get("value") or "").endswith(text), 5)
    key("Return")
    return ok


def running_reply(app):
    """The id of the source's reply message still being written, or None."""
    ms = app.section("chat").get("messages") or []
    live = [m for m in ms if m.get("role") == "assistant" and not m.get("finished") and (m.get("text") or "").strip()]
    return live[-1]["id"] if live else None


def main():
    probe_path, out_dir, port = sys.argv[1], sys.argv[2], sys.argv[3]
    app = pb.App(pb.pc.AppRun.existing(probe_path, pb.PROJ))
    engine = S = None
    try:
        with step("setup", out_dir):
            if not check(app.connected(45), "the window joined the project's engine"):
                raise RuntimeError("no engine")
            check(app.place(), "the window is at the screen's corner")
            engine = app.engine()
            check(pb.answer_init(app, engine), "the project's init question is answered (No)")
            check(pb.set_script(port, "stream2m"), "the fake server plays stream2m")
            S = pb.seed(2, "Mid-reply source")
            check(bool(S), f"seed_chat.py made the two-turn chat {S}")
            pb.poll(lambda: app.row(S), bool, 15)
            check(app.open_chat(S), "its row opens it")
            pb.poll(lambda: app.section("chat").get("messages") or [], lambda ms: len(ms) == 4, 15)
            check(send(app, LONG), f"the typing box sends \"{LONG}\"")
            ok, _, took = pb.poll(lambda: running_reply(app), bool, 20)
            check(ok, f"the reply streams ({took:.1f} s)")
            check(control(port, {"time_scale": SLOW}).get("time_scale") == SLOW, f"the fake server streams {SLOW} times slower while words are selected")
        if not S:
            check(False, "the remaining steps need the source chat; stopping here")
            return
        msgs = engine.messages(S)
        a2 = msgs[3]["id"]

        branch = None
        with step("B7", out_dir):
            # The reply still being written: its words cannot branch. The
            # timeline stops following first (one reading line up), so the
            # words stay put under the pointer while the reply grows.
            app.focus_timeline(app.heavy().get("chat") or {})
            key("Up")
            entry, selected = None, ""
            for _ in range(4):
                mid = running_reply(app)
                key_ = app.item_key(mid, "reply") or mid
                words = app.bring_into_view(key_) if mid else []
                text = [w for w in words if not (w["text"].isupper() or ":" in w["text"])]  # the byline: FAKE 04:17
                if len(text) >= 3 and running_reply(app) == mid:
                    app.drag(pb.word_point(text[0]), pb.word_point(text[2], end=True))
                    selected = (((app.heavy().get("chat") or {}).get("selection")) or {}).get("text") or ""
                    app.click(*pb.word_point(text[1], frac=0.5), button=3)
                    entry = pb.menu_entry(pb.menu_open(app), "Branch from here")
                    still = running_reply(app) == mid
                    key("Escape")
                    if still and selected:
                        break
                time.sleep(1)
            check(bool(selected), f"words of the reply still being written are selected ({selected!r})")
            check(bool(entry) and entry.get("disabled") and entry.get("hint") == pb.HINT_UNFINISHED,
                  f"and Branch from here is disabled with \"{pb.HINT_UNFINISHED}\" ({entry})")
            # An earlier finished reply branches while the source streams.
            key_a2 = app.item_key(a2, "reply") or a2
            words = app.bring_into_view(key_a2, ["shared", "stolen"])
            first = pb.find_word(words, "shared")
            last = pb.find_word(words, "stolen", after=first["i"]) if first else None
            branch, took, _ = pb.make_branch(app, pb.word_point(first), pb.word_point(last, end=True), pb.word_point(pb.between(words, first, last), frac=0.5), "B7 finished reply") if first and last else (None, None, None)
            source_live = not all(finished(m) for m in after_prompt(engine.messages(S), LONG))
            if check(bool(branch), f"the earlier finished reply branches while the source streams ({branch}, {took and round(took, 2)} s)"):
                check(source_live, "the source was still streaming when the branch opened")
                got = engine.messages(branch)
                unfinished = [m["id"] for m in got if m["role"] == "assistant" and not finished(m)]
                check([m["role"] for m in got] == ["user", "assistant", "user", "assistant"] and not unfinished,
                      f"the branch holds only finished messages ({[m['role'] for m in got]}, unfinished {unfinished})")

        with step("B8", out_dir):
            if not check(bool(branch) and app.chat_id() == branch, "the branch is shown"):
                raise RuntimeError("B8 needs B7's branch")
            prompt = "B8 check: sent while the source streams"
            sent = time.time()
            # The typing box already holds the quoted words; the prompt follows them.
            type_text(prompt)
            pb.poll(app.composer, lambda c: (c.get("value") or "").endswith(prompt), 5)
            key("Return")
            ok, got, took = pb.poll(lambda: engine.messages(branch), lambda ms: any(prompt in pb.text_of(m) for m in ms) and finished(ms[-1]) and ms[-1]["role"] == "assistant", 20, 0.2)
            source_live = not all(finished(m) for m in after_prompt(engine.messages(S), LONG))
            check(ok, f"the send in the branch is answered ({took:.1f} s after Enter)")
            check(ok and source_live, "while the source still streams: it never waited for the source")
            shown, _, _ = pb.poll(lambda: app.section("chat").get("messages") or [], lambda ms: any(prompt in (m.get("text") or "") for m in ms) and ms[-1].get("finished"), 5)
            check(shown, "the window shows the answer in the branch")

        with step("B7-source", out_dir):
            control(port, {"time_scale": 0.05})
            want = pb.flat(script_reply(LONG))
            ok, ms, took = pb.poll(lambda: after_prompt(engine.messages(S), LONG),
                                   lambda ms: bool(ms) and all(finished(m) for m in ms) and pb.flat("".join(pb.text_of(m) for m in ms if m["role"] == "assistant")) == want, 120, 0.5)
            reasons = finish_reasons(ms)
            check(ok, f"the source's reply finished with the script's whole text ({took:.1f} s; {len(ms)} messages)")
            check(bool(reasons) and not any(r in ("canceled", "error") for r in reasons), f"no message of it was canceled or failed ({sorted(set(reasons))})")
            check(app.open_chat(S), "back in the source chat")
            shown, _, _ = pb.poll(lambda: app.section("chat").get("messages") or [], lambda ms: bool(ms) and ms[-1].get("finished") and not app.section("chat").get("busy"), 15)
            check(shown, "the window shows it finished, the chat no longer busy")
    finally:
        try:
            control(port, {"time_scale": 1})
        except Exception:  # noqa: BLE001 - the server may be gone
            pass


if __name__ == "__main__":
    main()
    finish()
