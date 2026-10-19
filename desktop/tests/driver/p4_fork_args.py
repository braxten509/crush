"""B17 (desktop-design 3.4, 3.7): AI memory in a branch. For each AI tool the
first send in a branch runs the fake tool with that tool's fork call, as the
live checks found it works (docs/evidence/phase4/live-summary.md):

  claude  --resume <source session> --fork-session --resume-session-at <uuid
          of the cut reply>; a later send resumes the fork's session.
  codex   app-server thread/fork {threadId: <source thread>, lastTurnId:
          <the cut reply's turn>}, then turn/start on the new thread; no
          thread/start or thread/resume.
  grok    ACP _x.ai/session/fork {sourceSessionId, sourceCwd, newCwd,
          targetPromptIndex: 0 (the first prompt's count, 1, minus one)},
          then session/load of the new session, never of the source.
  agy     the written copy: no --conversation, the handoff text in the
          prompt.

Per tool: a chat on that tool gets two prompts through the engine's API,
the branch is made at the first reply's first word through the engine's
route (POST …/sessions/{sid}/branches, not the window), and one prompt is
sent in the branch. The fake tools ($FAKE_TOOL_LOG) log their arguments,
every stdin line, and the IDs each reply used, so the fork call is matched to
the source's own session and the cut reply's own ID. The driver needs no
window: it joins the project's engine as its own client, as seed_chat.py
does. Fails when a logged call differs from the table above, when a branch
send resumes the source session in place, or when a turn does not finish.
Usage: tests/check.sh p4_fork_args.py
"""
import json, os, sys, time, uuid

sys.path.insert(0, os.path.dirname(__file__))
sys.path.insert(0, os.path.join(os.path.dirname(os.path.dirname(os.path.abspath(__file__))), "fakes"))
from harness import check, finish  # noqa: E402
import seed_chat  # noqa: E402

out_dir = sys.argv[2] if len(sys.argv) > 2 else "tests/out/p4_fork_args"
PROJ = "/tmp/home/proj"
ROOT = os.environ.get("DESKTOP_ROOT") or os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
CRUSH = os.environ.get("DESKTOP_CRUSH") or os.path.join(ROOT, "tests", "out", "bin", "crush")
LOG = os.environ.get("FAKE_TOOL_LOG", "/tmp/fake-tools.jsonl")
WRITTEN_COPY = "This conversation started before you joined"

# provider and model per tool: the built-in lists of internal/config/cli_providers.go.
TOOLS = [("claude", "claude-code", "haiku"), ("codex", "codex-cli", "gpt-6-sol"),
         ("grok", "grok-cli", "grok-4.7"), ("agy", "agy-cli", "gemini-3.8-flash-low")]


def log_lines():
    if not os.path.exists(LOG):
        return []
    with open(LOG) as f:
        return [json.loads(line) for line in f if line.strip()]


def requests(entries, method):
    out = []
    for e in entries:
        try:
            msg = json.loads(e.get("stdin") or "null")
        except ValueError:
            continue
        if isinstance(msg, dict) and msg.get("method") == method:
            out.append(msg.get("params") or {})
    return out


def prompt_text(entries):
    """What the tool was asked: Claude's and AGY's user line, Codex's
    turn/start input, ACP's session/prompt."""
    texts = []
    for e in entries:
        try:
            msg = json.loads(e.get("stdin") or "null")
        except ValueError:
            continue
        if not isinstance(msg, dict):
            continue
        if msg.get("type") == "user" or msg.get("event") == "user":
            content = (msg.get("message") or {}).get("content")
            texts.append(content if isinstance(content, str) else "".join(b.get("text", "") for b in content or []))
        elif msg.get("method") == "turn/start":
            texts += [i.get("text", "") for i in (msg.get("params") or {}).get("input") or []]
        elif msg.get("method") == "session/prompt":
            texts += [p.get("text", "") for p in (msg.get("params") or {}).get("prompt") or []]
    return "\n".join(texts)


def flag(args, name):
    return args[args.index(name) + 1] if name in args and args.index(name) + 1 < len(args) else None


class Engine:
    def __init__(self):
        self.api = seed_chat.Engine(seed_chat.ensure(CRUSH, PROJ))
        self.client = str(uuid.uuid4())
        ws = self.api.call("POST", "/v1/workspaces", {"path": PROJ, "data_dir": "", "version": "p4_fork_args", "protocol": seed_chat.PROTOCOL,
                                                      "client_id": self.client, "yolo": True, "env": ["%s=%s" % kv for kv in os.environ.items()]})
        self.base = "/v1/workspaces/" + ws["id"]
        del ws  # holds the environment; never print it
        self.q = "?client_id=" + self.client
        self.events = self.api.follow_events(self.base + "/events" + self.q)
        self.api.call("POST", self.base + "/agent/init" + self.q, {"interactive": True}, timeout=120)

    def get(self, path):
        return self.api.call("GET", "%s/%s" % (self.base, path))

    def post(self, path, body=None):
        return self.api.call("POST", "%s/%s%s" % (self.base, path, self.q), body, timeout=60)

    def use(self, provider, model):
        """The project's model, as the terminal's model picker sets it."""
        self.post("config/model", {"scope": 1, "model_type": "large", "model": {"model": model, "provider": provider}})
        self.post("agent/update")

    def new_chat(self, title):
        return self.post("sessions", {"title": title})["id"]

    def send(self, sid, prompt, timeout=60):
        before = self.get("sessions/%s" % sid)["message_count"]
        self.post("agent", {"session_id": sid, "prompt": prompt})
        return seed_chat.wait_turn(self.api, self.base, sid, before, timeout)

    def messages(self, sid):
        return self.get("sessions/%s/messages" % sid)

    def close(self):
        try:
            self.api.call("DELETE", "/v1/clients/" + self.client)
        finally:
            self.events.close()


def text_of(message):
    return "".join((p.get("data") or {}).get("text", "") for p in message.get("parts") or [] if p.get("type") == "text")


def branch_at_first_reply(engine, sid):
    reply = next(m for m in engine.messages(sid) if m.get("role") == "assistant" and text_of(m).strip())
    word = text_of(reply).split()[0]
    made = engine.post("sessions/%s/branches" % sid, {"message_id": reply["id"], "highlight_text": word,
                                                      "highlight_start": 0, "highlight_end": len(word), "title": word})
    return made["session"]["id"]


def run_tool(engine, tool, provider, model):
    engine.use(provider, model)
    source = engine.new_chat("B17 %s source" % tool)
    start = len(log_lines())
    engine.send(source, "First")
    engine.send(source, "Second")
    replies = [e["reply"] for e in log_lines()[start:] if e.get("tool") == tool and "reply" in e]
    if not check(len(replies) >= 2, "%s: the source chat's two replies are logged (%d)" % (tool, len(replies))):
        return
    cut = replies[0]
    branch = branch_at_first_reply(engine, source)
    start = len(log_lines())
    engine.send(branch, "Third")
    calls = [e for e in log_lines()[start:] if e.get("tool") == tool]
    # Model discovery (`agy models`, `--version`) may start the tool too;
    # only turn processes count.
    argv = [e["args"] for e in calls if "args" in e and e["args"][:1] != ["models"] and "--version" not in e["args"]]
    prompt = prompt_text(calls)

    if tool == "claude":
        forks = [a for a in argv if "--fork-session" in a]
        ok = len(forks) == 1 and flag(forks[0], "--resume") == cut["session"] and flag(forks[0], "--resume-session-at") == (cut["uuids"] or [None])[-1]
        check(ok, "claude: the branch's first send runs --resume <source> --fork-session --resume-session-at <cut uuid> (%s)" %
              [" ".join(a[a.index("--resume"):a.index("--resume") + 5]) if "--resume" in a else "no --resume" for a in argv])
        check(not any(flag(a, "--resume") == cut["session"] and "--fork-session" not in a for a in argv),
              "claude: the source session is never resumed in place")
        fork_session = next((e["reply"]["session"] for e in calls if "reply" in e), None)
        check(fork_session not in (None, cut["session"]), "claude: the fork answers in a new session")
        check(WRITTEN_COPY not in prompt, "claude: no written copy is handed over")
    elif tool == "codex":
        forks = requests(calls, "thread/fork")
        ok = len(forks) == 1 and forks[0].get("threadId") == cut["thread"] and forks[0].get("lastTurnId") == cut["turn"]
        check(ok, "codex: thread/fork names the source thread and the cut reply's turn (%s)" %
              [{k: f.get(k) for k in ("threadId", "lastTurnId")} for f in forks])
        check(all(forks[0].get(k) for k in ("cwd", "model", "approvalPolicy", "sandbox")) if forks else False,
              "codex: thread/fork carries cwd, model, approvalPolicy and sandbox")
        check(not requests(calls, "thread/start") and not requests(calls, "thread/resume"),
              "codex: no thread/start or thread/resume in the branch's first send")
        check(len(requests(calls, "turn/start")) == 1, "codex: the turn starts on the forked thread")
        check(WRITTEN_COPY not in prompt, "codex: no written copy is handed over")
    elif tool == "grok":
        forks = requests(calls, "_x.ai/session/fork")
        ok = len(forks) == 1 and forks[0].get("sourceSessionId") == cut["session"] and forks[0].get("targetPromptIndex") == 0 \
            and forks[0].get("sourceCwd") == PROJ and forks[0].get("newCwd") == PROJ
        check(ok, "grok: _x.ai/session/fork names the source session, the project and index 0 (count 1 minus one) (%s)" %
              [{k: f.get(k) for k in ("sourceSessionId", "targetPromptIndex", "sourceCwd", "newCwd")} for f in forks])
        loads = [p.get("sessionId") for p in requests(calls, "session/load")]
        check(len(loads) == 1 and loads[0] != cut["session"], "grok: session/load opens the fork, not the source (%s)" % loads)
        check(not requests(calls, "session/new"), "grok: no new session")
        check(WRITTEN_COPY not in prompt, "grok: no written copy is handed over")
    elif tool == "agy":
        check(len(argv) == 1 and "--conversation" not in argv[0], "agy: the branch starts a new conversation (%s)" % argv)
        check(WRITTEN_COPY in prompt, "agy: the written copy is handed over")
    check("Third" in prompt, "%s: the branch's prompt reaches the tool" % tool)

    if tool == "claude":
        # A later send continues the fork's session (in the kept process, or
        # a new one started with --resume <fork>), never the source's.
        start = len(log_lines())
        engine.send(branch, "Fourth")
        later = [e for e in log_lines()[start:] if e.get("tool") == tool]
        sessions = [e["reply"]["session"] for e in later if "reply" in e]
        resumed = [flag(e["args"], "--resume") for e in later if "args" in e]
        check(sessions == [fork_session] and all(r == fork_session for r in resumed) and not any("--fork-session" in e.get("args", []) for e in later),
              "claude: a later send in the branch continues the fork's session (replies in %s, --resume %s)" % (sessions, resumed))


def main():
    if os.environ.get("HOME") != "/tmp/home" or not os.environ.get("DESKTOP_ROOT"):
        print("FAIL run it inside tests/sandbox.sh: it sends its environment to the engine", flush=True)
        sys.exit(1)
    engine = Engine()
    try:
        for tool, provider, model in TOOLS:
            print("STEP %s" % tool, flush=True)
            try:
                run_tool(engine, tool, provider, model)
            except (seed_chat.ApiError, OSError, KeyError, StopIteration, ValueError) as e:
                check(False, "%s raised %s: %s" % (tool, type(e).__name__, e))
    finally:
        engine.close()
    finish()


if __name__ == "__main__":
    main()
