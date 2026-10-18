"""A fake OpenAI-style AI server for tests (desktop-design 6.2). It plays
scripted replies, streams them the way the openai-compat provider reads them,
and logs every request. Standard library only.

Usage:
  fake_ai_server.py --port-file FILE [--scripts DIR] [--script NAME|FILE]
  fake_ai_server.py --self-test [--engine CRUSH]

Scripts. Every tests/fakes/scripts/*.json is loaded at start; "script X" in the
design means the turns of scripts/X.json. A script is
  {"turns": [{"when": "substring of the last user message", "title": "optional",
              "steps": [STEP, ...]}, ...],
   "default": STEP, "about": "optional note"}
and a STEP is
  {"delay_ms": 10, "reasoning": "thinking text", "text": "streamed reply",
   "tool_calls": [{"name": "bash", "arguments": {"command": "ls"}}],
   "finish": "stop" | "tool_calls" | "length" | "content_filter",
   "error": {"status": 500, "body": "text or JSON", "times": 1, "headers": {}},
   "usage": {"prompt_tokens": 1200, "completion_tokens": 300}}
Every field is optional. "finish" defaults to tool_calls when the step calls
tools, else stop. "error" answers with that HTTP status instead of a reply;
with "times": N it does so only for the first N requests of one conversation,
and the step then plays normally (a 429 once). "headers" go on the error
answer (for example {"retry-after": "1"}, so a retry does not wait 5 s).

Picking a reply. The server is stateless: it takes the turn whose "when" is a
substring of the last user message, then the step whose index is the number of
assistant messages after that user message. The active script's turns are
tried first, then every script in file-name order; nothing matching gives the
active script's default, else {"text": "OK."}. A turn played past its last
step answers with its script's default. A request that offers no tools (titles,
summaries) never gets tool calls. Title requests ("Generate a concise title for
the following content: ...") are answered at once with the turn's "title", or
the content's first six words, and never play a script step.

Streaming. Text and reasoning go out in chunks of 1-4 words (the same chunks on
every run of the same step), tool calls as OpenAI deltas (id and name first,
then the arguments in pieces), then the finish chunk, a usage chunk and
[DONE]. "delay_ms" sleeps before every chunk. FAKE_AI_TIME_SCALE (or the
control call) multiplies every delay; the self-test plays scripts at 0.

Other paths. GET /file.txt and GET /page.html are served for the Download and
Fetch tools; GET .../models lists the models. POST /_fake/control
{"script": NAME|null, "time_scale": N} selects the active script and the time
scale (a driver reaches it on the port it is given); GET /_fake/control reports
them. Every other GET is logged with "beacon": true (the safety check, 2.9).

Log. Every request is one JSON line in $FAKE_AI_LOG: method, path, model,
stream, messages (the count), tools (the count), last_user (its first 200
characters), kind ("turn", "default", "title", "control", "file", "models",
"beacon"), the script, turn and step played, the status sent and the
x-session-id header.

Self-test. --self-test plays every script in scripts/ through a real server
over HTTP, step by step as the engine would (tool results go back as tool
messages), and checks every stream: chunk shape, 1-4 words per text chunk, the
reassembled text, reasoning and tool-call arguments, the finish reason, usage,
errors and their "times". It also checks each script's shape and that every
turn is reached by its own "when" text, and runs built-in cases for the rest
(timing, titles, files, beacons, the control call, non-streamed requests).
With --engine CRUSH (inside tests/sandbox.sh only) it also runs real
`crush run` turns against the server, the fake AI tools, seed_chat.py through
the engine's API, the fake MCP server and the fake LSP server (see
engine_checks below); their logs are copied to tests/out/fake_ai_server/.
Exit 0 only when all pass.
"""
import argparse, hashlib, importlib.util, json, os, random, re, shutil, socket, subprocess, sys, tempfile, threading, time, uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib import request as urlrequest
from urllib.error import HTTPError

HERE = os.path.dirname(os.path.abspath(__file__))
SCRIPTS_DIR = os.path.join(HERE, "scripts")
MODELS = ["fake-small", "fake-large"]
TITLE_PREFIX = "Generate a concise title for the following content:"
GLOBAL_DEFAULT = {"text": "OK."}
STEP_KEYS = {"delay_ms", "reasoning", "text", "tool_calls", "finish", "error", "usage"}
FINISHES = {"stop", "tool_calls", "length", "content_filter"}
FILE_TXT = "Fake file for the Download tool.\nSecond line.\n"
PAGE_HTML = ("<!doctype html><html><head><title>Fake page</title></head><body>"
             "<h1>Fake page</h1><p>A page the fake AI server serves for the Fetch tool.</p>"
             "<ul><li>first point</li><li>second point</li></ul></body></html>")


# Scripts --------------------------------------------------------------------

def load_scripts(folder):
    """Every *.json in folder, by name (file name without .json), in name order."""
    scripts = {}
    if folder and os.path.isdir(folder):
        for name in sorted(os.listdir(folder)):
            if name.endswith(".json"):
                with open(os.path.join(folder, name)) as f:
                    scripts[name[:-5]] = json.load(f)
    return scripts


def message_text(message):
    content = message.get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        return "".join(p.get("text", "") for p in content if isinstance(p, dict) and p.get("type", "text") == "text")
    return ""


def last_user(messages):
    """(index, text) of the last user message; (-1, "") when there is none."""
    for i in range(len(messages) - 1, -1, -1):
        if messages[i].get("role") == "user":
            return i, message_text(messages[i])
    return -1, ""


def title_content(text):
    """The content a title request asks about, or None for other requests."""
    if not text.startswith(TITLE_PREFIX):
        return None
    content = text[len(TITLE_PREFIX):]
    cut = content.rfind("<think>")
    if cut >= 0:
        content = content[:cut]
    return content.strip()


def default_title(content):
    words = content.split()
    return " ".join(words[:6]) or "New chat"


def stable_seed(*parts):
    return int(hashlib.sha256("\x00".join(map(str, parts)).encode()).hexdigest()[:16], 16)


def word_chunks(text, seed):
    """Splits text into chunks of 1-4 words; joined they give text back exactly.
    The chunk sizes come from seed, so a step streams the same way every run."""
    tokens = re.findall(r"\s+|\S+", text)
    words, lead = [], ""
    for tok in tokens:
        if tok.isspace():
            if words:
                words[-1] += tok
            else:
                lead += tok
        else:
            words.append(lead + tok)
            lead = ""
    if lead:
        if words:
            words[-1] += lead
        else:
            return [lead]
    rng = random.Random(seed)
    chunks, i = [], 0
    while i < len(words):
        n = rng.randint(1, 4)
        chunks.append("".join(words[i:i + n]))
        i += n
    return chunks


def argument_pieces(text, size=60):
    return [text[i:i + size] for i in range(0, len(text), size)] or [""]


class Fake:
    """The server's state: scripts, the active one, the time scale, the counters
    of errors with "times", and the log."""

    def __init__(self, scripts, active=None, time_scale=1.0, log_path=None):
        self.scripts = scripts
        self.active = active
        self.time_scale = time_scale
        self.log_path = log_path
        self.lock = threading.Lock()
        self.served = {}  # once-error key -> times served

    def log(self, entry):
        if not self.log_path:
            return
        line = json.dumps(entry, ensure_ascii=False) + "\n"
        with self.lock, open(self.log_path, "a") as f:
            f.write(line)

    def order(self):
        names = list(self.scripts)
        if self.active in self.scripts:
            names.remove(self.active)
            names.insert(0, self.active)
        return names

    def match(self, text):
        """(script name, turn index, turn) of the first turn whose "when" is in text."""
        for name in self.order():
            for i, turn in enumerate(self.scripts[name].get("turns", [])):
                when = turn.get("when")
                if isinstance(when, str) and when and when in text:
                    return name, i, turn
        return None, None, None

    def default_step(self, name=None):
        script = self.scripts.get(name if name is not None else self.active)
        step = (script or {}).get("default")
        return step if isinstance(step, dict) else GLOBAL_DEFAULT

    def pick(self, messages):
        """What to answer: a dict with kind, step, script, turn, step index."""
        idx, text = last_user(messages)
        content = title_content(text)
        if content is not None:
            name, ti, turn = self.match(content)
            title = (turn or {}).get("title") or default_title(content)
            return {"kind": "title", "step": {"text": title}, "script": name, "turn": ti, "step_index": None}
        name, ti, turn = self.match(text)
        if turn is None:
            return {"kind": "default", "step": self.default_step(), "script": self.active, "turn": None, "step_index": None}
        n = sum(1 for m in messages[idx + 1:] if m.get("role") == "assistant")
        steps = turn.get("steps") or []
        if n < len(steps):
            return {"kind": "turn", "step": steps[n], "script": name, "turn": ti, "step_index": n}
        return {"kind": "default", "step": self.default_step(name), "script": name, "turn": ti, "step_index": n}

    def error_now(self, step, key):
        """The step's error when it should be answered for this request, else None."""
        err = step.get("error")
        if not isinstance(err, dict):
            return None
        times = err.get("times")
        if times is None:
            return err
        with self.lock:
            served = self.served.get(key, 0)
            if served >= int(times):
                return None
            self.served[key] = served + 1
        return err


# HTTP -------------------------------------------------------------------------

def chunk_json(cid, model, created, delta=None, finish=None, usage=None):
    body = {"id": cid, "object": "chat.completion.chunk", "created": created, "model": model}
    if usage is not None:
        body["choices"] = []
        body["usage"] = usage
    else:
        body["choices"] = [{"index": 0, "delta": delta or {}, "finish_reason": finish}]
    return body


def estimate_usage(messages, step):
    chars = sum(len(message_text(m)) for m in messages)
    out_words = len((step.get("text") or "").split()) + len((step.get("reasoning") or "").split())
    out_words += sum(len(json.dumps(c.get("arguments", {}))) // 4 for c in step.get("tool_calls") or [])
    usage = {"prompt_tokens": 50 + chars // 4, "completion_tokens": max(1, out_words)}
    given = step.get("usage")
    if isinstance(given, dict):
        usage.update({k: v for k, v in given.items() if isinstance(v, int)})
    usage["total_tokens"] = usage["prompt_tokens"] + usage["completion_tokens"]
    return usage


def error_body(err):
    body = err.get("body", "fake error")
    if isinstance(body, (dict, list)):
        return json.dumps(body).encode()
    if isinstance(body, str):
        try:
            json.loads(body)
            return body.encode()
        except ValueError:
            pass
    status = int(err.get("status", 500))
    kind = {400: "invalid_request_error", 401: "authentication_error", 429: "rate_limit_exceeded"}.get(status, "server_error")
    return json.dumps({"error": {"message": str(body), "type": kind, "code": status}}).encode()


class Handler(BaseHTTPRequestHandler):
    server_version = "FakeAI/2"

    def log_message(self, *args):  # keep test output quiet
        pass

    @property
    def fake(self):
        return self.server.fake

    def _send(self, code, body, ctype="application/json", headers=None):
        data = body if isinstance(body, bytes) else json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(data)))
        for k, v in (headers or {}).items():
            self.send_header(k, str(v))
        self.end_headers()
        self.wfile.write(data)

    def _base(self, method):
        return {"at": round(time.time(), 3), "method": method, "path": self.path}

    def do_GET(self):
        entry = self._base("GET")
        path = self.path.split("?", 1)[0]
        if path.rstrip("/").endswith("/models"):
            entry["kind"] = "models"
            self.fake.log(entry)
            return self._send(200, {"object": "list", "data": [{"id": m, "object": "model"} for m in MODELS]})
        if path == "/file.txt":
            entry["kind"] = "file"
            self.fake.log(entry)
            return self._send(200, FILE_TXT.encode(), "text/plain; charset=utf-8")
        if path == "/page.html":
            entry["kind"] = "file"
            self.fake.log(entry)
            return self._send(200, PAGE_HTML.encode(), "text/html; charset=utf-8")
        if path == "/_fake/control":
            entry["kind"] = "control"
            self.fake.log(entry)
            return self._send(200, self._state())
        entry.update({"kind": "beacon", "beacon": True})
        self.fake.log(entry)
        self._send(404, {"error": {"message": "not found"}})

    def _state(self):
        return {"script": self.fake.active, "time_scale": self.fake.time_scale, "scripts": list(self.fake.scripts)}

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        raw = self.rfile.read(length) if length else b""
        try:
            body = json.loads(raw or b"{}")
        except ValueError:
            body = {}
        path = self.path.split("?", 1)[0]
        if path == "/_fake/control":
            entry = self._base("POST")
            entry["kind"] = "control"
            if "script" in body:
                name = body["script"]
                if name is not None and name not in self.fake.scripts:
                    entry["status"] = 404
                    self.fake.log(entry)
                    return self._send(404, {"error": {"message": f"no script {name!r}"}, **self._state()})
                self.fake.active = name
            if "time_scale" in body:
                self.fake.time_scale = float(body["time_scale"])
            entry.update({"script": self.fake.active, "time_scale": self.fake.time_scale, "status": 200})
            self.fake.log(entry)
            return self._send(200, self._state())
        messages = body.get("messages") or []
        idx, text = last_user(messages)
        entry = self._base("POST")
        entry.update({"model": body.get("model"), "stream": bool(body.get("stream", False)),
                      "messages": len(messages), "tools": len(body.get("tools") or []),
                      "last_user": text[:200], "session": self.headers.get("x-session-id", "")})
        if not path.rstrip("/").endswith("/chat/completions"):
            entry.update({"kind": "unknown", "status": 404})
            self.fake.log(entry)
            return self._send(404, {"error": {"message": "not found"}})
        choice = self.fake.pick(messages)
        step = dict(choice["step"])
        entry.update({"kind": choice["kind"], "script": choice["script"], "turn": choice["turn"], "step": choice["step_index"]})
        if not body.get("tools") and step.get("tool_calls"):
            step.pop("tool_calls")
            entry["tools_dropped"] = True
        if choice["kind"] != "title":
            key = (choice["script"], choice["turn"], choice["step_index"], text, len(messages), entry["session"])
            err = self.fake.error_now(step, key)
            if err is not None:
                status = int(err.get("status", 500))
                entry["status"] = status
                self.fake.log(entry)
                return self._send(status, error_body(err), headers=err.get("headers"))
        entry["status"] = 200
        self.fake.log(entry)
        seed = stable_seed(choice["script"], choice["turn"], choice["step_index"], step.get("text", ""))
        if body.get("stream"):
            self._stream(body, messages, step, seed)
        else:
            self._complete(body, messages, step)

    def _finish(self, step):
        finish = step.get("finish")
        if finish in FINISHES:
            return finish
        return "tool_calls" if step.get("tool_calls") else "stop"

    def _complete(self, body, messages, step):
        message = {"role": "assistant", "content": step.get("text") or None}
        if step.get("reasoning"):
            message["reasoning_content"] = step["reasoning"]
        calls = step.get("tool_calls") or []
        if calls:
            message["tool_calls"] = [{"id": "call_" + uuid.uuid4().hex[:24], "type": "function",
                                      "function": {"name": c.get("name", ""), "arguments": json.dumps(c.get("arguments", {}))}}
                                     for c in calls]
        self._send(200, {"id": "chatcmpl-" + uuid.uuid4().hex[:12], "object": "chat.completion", "created": int(time.time()),
                         "model": body.get("model"), "choices": [{"index": 0, "message": message, "finish_reason": self._finish(step)}],
                         "usage": estimate_usage(messages, step)})

    def _stream(self, body, messages, step, seed):
        model = body.get("model")
        cid = "chatcmpl-" + uuid.uuid4().hex[:12]
        created = int(time.time())
        delay = max(0.0, float(step.get("delay_ms") or 0) / 1000.0)
        self.send_response(200)
        self.send_header("Content-Type", "text/event-stream")
        self.send_header("Cache-Control", "no-cache")
        self.end_headers()

        def emit(obj, wait=True):
            if wait:
                pause = delay * self.fake.time_scale
                if pause > 0:
                    time.sleep(pause)
            data = obj if isinstance(obj, str) else json.dumps(obj, ensure_ascii=False)
            self.wfile.write(f"data: {data}\n\n".encode())
            self.wfile.flush()

        try:
            emit(chunk_json(cid, model, created, {"role": "assistant", "content": ""}), wait=False)
            if step.get("reasoning"):
                for piece in word_chunks(step["reasoning"], seed ^ 1):
                    emit(chunk_json(cid, model, created, {"reasoning_content": piece}))
            if step.get("text"):
                for piece in word_chunks(step["text"], seed):
                    emit(chunk_json(cid, model, created, {"content": piece}))
            for i, call in enumerate(step.get("tool_calls") or []):
                args = json.dumps(call.get("arguments", {}), ensure_ascii=False)
                emit(chunk_json(cid, model, created, {"tool_calls": [{"index": i, "id": "call_" + uuid.uuid4().hex[:24], "type": "function",
                                                                      "function": {"name": call.get("name", ""), "arguments": ""}}]}))
                for piece in argument_pieces(args):
                    emit(chunk_json(cid, model, created, {"tool_calls": [{"index": i, "function": {"arguments": piece}}]}))
            emit(chunk_json(cid, model, created, {}, finish=self._finish(step)))
            emit(chunk_json(cid, model, created, usage=estimate_usage(messages, step)), wait=False)
            emit("[DONE]", wait=False)
        except (BrokenPipeError, ConnectionResetError):
            pass  # the engine stopped reading (a cancel)


class Server(ThreadingHTTPServer):
    daemon_threads = True


def make_server(fake, port=0):
    srv = Server(("127.0.0.1", port), Handler)
    srv.fake = fake
    return srv


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port-file")
    ap.add_argument("--scripts", default=SCRIPTS_DIR)
    ap.add_argument("--script", help="the active script: a name in --scripts, or a JSON file")
    ap.add_argument("--self-test", action="store_true")
    ap.add_argument("--engine", help="with --self-test: the crush binary for the engine checks (sandbox only)")
    a = ap.parse_args()
    if a.self_test:
        return self_test(a.scripts, a.engine)
    if not a.port_file:
        ap.error("--port-file is required")
    scripts = load_scripts(a.scripts)
    active = None
    if a.script:
        if os.path.isfile(a.script):
            name = os.path.basename(a.script).rsplit(".", 1)[0]
            with open(a.script) as f:
                scripts[name] = json.load(f)
            active = name
        else:
            active = a.script
    scale = float(os.environ.get("FAKE_AI_TIME_SCALE", "1") or 1)
    srv = make_server(Fake(scripts, active, scale, os.environ.get("FAKE_AI_LOG")))
    with open(a.port_file + ".tmp", "w") as f:
        f.write(str(srv.server_address[1]))
    os.replace(a.port_file + ".tmp", a.port_file)
    srv.serve_forever()


# Self-test ---------------------------------------------------------------------

class Results:
    def __init__(self):
        self.failures = 0
        self.passes = 0

    def check(self, ok, what):
        print(("PASS " if ok else "FAIL ") + what, flush=True)
        if ok:
            self.passes += 1
        else:
            self.failures += 1
        return ok

    def note(self, what):
        print("NOTE " + what, flush=True)


def http(port, method, path, body=None, timeout=30):
    """(status, headers, raw body) of one request to the fake server."""
    data = json.dumps(body).encode() if body is not None else None
    req = urlrequest.Request(f"http://127.0.0.1:{port}{path}", data=data, method=method,
                             headers={"Content-Type": "application/json", "x-session-id": "self-test"})
    try:
        with urlrequest.urlopen(req, timeout=timeout) as resp:
            return resp.status, dict(resp.headers), resp.read()
    except HTTPError as e:
        return e.code, dict(e.headers), e.read()


def parse_stream(raw):
    """Checks one SSE body; returns (problems, result) where result has the
    reassembled text, reasoning, tool calls, finish, usage and text chunks."""
    problems = []
    out = {"text": "", "reasoning": "", "calls": {}, "finish": None, "usage": None, "text_chunks": [], "chunks": 0}
    events = [e for e in raw.decode().split("\n\n") if e.strip()]
    if not events or events[-1].strip() != "data: [DONE]":
        problems.append("the stream does not end with data: [DONE]")
    for ev in events[:-1]:
        if not ev.startswith("data: "):
            problems.append(f"an event is not a data line: {ev[:40]!r}")
            continue
        try:
            c = json.loads(ev[6:])
        except ValueError:
            problems.append(f"a chunk is not JSON: {ev[:60]!r}")
            continue
        out["chunks"] += 1
        if c.get("object") != "chat.completion.chunk" or not isinstance(c.get("choices"), list):
            problems.append("a chunk lacks object chat.completion.chunk or choices")
            continue
        if c.get("usage"):
            if c["choices"]:
                problems.append("the usage chunk has choices")
            out["usage"] = c["usage"]
        for ch in c["choices"]:
            if out["finish"] is not None:
                problems.append("a choice came after the finish chunk")
            d = ch.get("delta") or {}
            if d.get("reasoning_content"):
                out["reasoning"] += d["reasoning_content"]
            if d.get("content"):
                out["text"] += d["content"]
                out["text_chunks"].append(d["content"])
            for tc in d.get("tool_calls") or []:
                slot = out["calls"].setdefault(tc.get("index"), {"id": None, "name": None, "arguments": ""})
                if tc.get("id"):
                    if slot["id"] is not None:
                        problems.append(f"tool call {tc.get('index')} got a second id")
                    slot["id"] = tc["id"]
                    slot["name"] = (tc.get("function") or {}).get("name")
                    if tc.get("type") != "function":
                        problems.append("a tool call's first delta lacks type function")
                elif slot["id"] is None:
                    problems.append(f"tool call {tc.get('index')} streamed arguments before its id")
                slot["arguments"] += (tc.get("function") or {}).get("arguments") or ""
            if ch.get("finish_reason"):
                out["finish"] = ch["finish_reason"]
    if out["finish"] is None:
        problems.append("no finish chunk")
    if not out["usage"]:
        problems.append("no usage chunk")
    return problems, out


def check_step_stream(r, label, raw, step):
    problems, out = parse_stream(raw)
    want_finish = step.get("finish") if step.get("finish") in FINISHES else ("tool_calls" if step.get("tool_calls") else "stop")
    if out["text"] != (step.get("text") or ""):
        problems.append("the text does not reassemble exactly")
    if out["reasoning"] != (step.get("reasoning") or ""):
        problems.append("the reasoning does not reassemble exactly")
    bad = [c for c in out["text_chunks"] if not 1 <= len(c.split()) <= 4]
    if bad:
        problems.append(f"{len(bad)} text chunks are not 1-4 words (first {bad[0]!r})")
    calls = [out["calls"][k] for k in sorted(out["calls"])]
    want = step.get("tool_calls") or []
    if len(calls) != len(want):
        problems.append(f"{len(calls)} tool calls streamed, the step has {len(want)}")
    for got, exp in zip(calls, want):
        try:
            args = json.loads(got["arguments"])
        except ValueError:
            problems.append(f"tool call {exp.get('name')}: arguments are not JSON")
            continue
        if got["name"] != exp.get("name") or args != exp.get("arguments", {}):
            problems.append(f"tool call {exp.get('name')}: name or arguments differ")
    if out["finish"] != want_finish:
        problems.append(f"finish {out['finish']!r}, want {want_finish!r}")
    r.check(not problems, f"{label}: " + ("; ".join(problems) if problems else
            f"{out['chunks']} chunks, {len(out['text_chunks'])} text chunks, {len(calls)} tool calls, finish {out['finish']}"))
    return out


TOOL_REQUIRED = {
    "bash": ["command"], "view": ["file_path"], "edit": ["file_path", "old_string", "new_string"],
    "multiedit": ["file_path", "edits"], "write": ["file_path", "content"], "grep": ["pattern"],
    "glob": ["pattern"], "ls": [], "fetch": ["url"], "download": ["url", "file_path"], "todos": ["todos"],
    "agent": ["prompt"], "agentic_fetch": ["prompt"], "web_fetch": ["url"], "web_search": ["query"],
    "sourcegraph": ["query"], "job_output": ["shell_id"], "job_kill": ["shell_id"], "question": [],
    "list_mcp_resources": [], "read_mcp_resource": [], "crush_info": [], "crush_logs": [],
}


def check_shapes(r, scripts):
    """Each script's shape: turns, steps, fields and finish values."""
    for name, script in scripts.items():
        problems, notes = [], []
        if not isinstance(script, dict):
            r.check(False, f"script {name}: is not a JSON object")
            continue
        for key in script:
            if key not in ("turns", "default", "about"):
                problems.append(f"unknown top-level key {key!r}")
        turns = script.get("turns", [])
        if not isinstance(turns, list):
            problems.append("turns is not a list")
            turns = []
        steps = [("default", script["default"])] if "default" in script else []
        for i, turn in enumerate(turns):
            if not isinstance(turn, dict) or not isinstance(turn.get("when"), str) or not turn.get("when"):
                problems.append(f"turn {i} has no \"when\" text")
                continue
            for key in turn:
                if key not in ("when", "steps", "title", "about"):
                    problems.append(f"turn {i} has an unknown key {key!r}")
            if not isinstance(turn.get("steps", []), list) or not turn.get("steps"):
                problems.append(f"turn {i} ({turn['when']!r}) has no steps")
                continue
            steps += [(f"turn {i} step {j}", s) for j, s in enumerate(turn["steps"])]
            if (turn["steps"][-1] or {}).get("tool_calls"):
                notes.append(f"turn {i} ({turn['when']!r}) ends with tool calls; after them the script's default answers")
        for where, step in steps:
            if not isinstance(step, dict):
                problems.append(f"{where} is not an object")
                continue
            for key in step:
                if key not in STEP_KEYS and key != "about":
                    problems.append(f"{where} has an unknown key {key!r}")
            if "finish" in step and step["finish"] not in FINISHES:
                problems.append(f"{where} has finish {step['finish']!r}")
            if "error" in step and not (isinstance(step["error"], dict) and isinstance(step["error"].get("status"), int)):
                problems.append(f"{where} has an error without a numeric status")
            for k in ("text", "reasoning"):
                if k in step and not isinstance(step[k], str):
                    problems.append(f"{where} {k} is not text")
            for call in step.get("tool_calls") or []:
                if not isinstance(call, dict) or not isinstance(call.get("name"), str) or not isinstance(call.get("arguments", {}), dict):
                    problems.append(f"{where} has a tool call without a name and an arguments object")
                    continue
                tool = call["name"]
                if tool in TOOL_REQUIRED:
                    missing = [p for p in TOOL_REQUIRED[tool] if p not in call.get("arguments", {})]
                    if missing:
                        notes.append(f"{where}: {tool} lacks {', '.join(missing)} (fine only when the script wants it to fail)")
                elif not tool.startswith("mcp_"):
                    notes.append(f"{where}: {tool!r} is not a Crush tool name this test knows")
        r.check(not problems, f"script {name}: shape" + (": " + "; ".join(problems) if problems else f" ({len(turns)} turns)"))
        for n in notes:
            r.note(f"script {name}: {n}")


def check_reachable(r, scripts):
    """Every turn must be the one its own "when" text picks, with its script
    active and with no script active (a driver may not select one)."""
    fake = Fake(scripts)
    for name, script in scripts.items():
        for i, turn in enumerate(script.get("turns") or []):
            when = turn.get("when")
            if not isinstance(when, str) or not when:
                continue
            fake.active = name
            n1, i1, _ = fake.match(when)
            fake.active = None
            n2, i2, _ = fake.match(when)
            r.check((n1, i1) == (name, i), f"script {name} turn {i} ({when!r}) is reached with its script active"
                    + ("" if (n1, i1) == (name, i) else f" (got {n1} turn {i1})"))
            r.check((n2, i2) == (name, i), f"script {name} turn {i} ({when!r}) is reached with no script active"
                    + ("" if (n2, i2) == (name, i) else f": script {n2} turn {i2} wins; make one \"when\" unique"))


def fake_tool_result(call):
    return f"fake result of {call.get('name')}"


def play_turn(r, port, name, i, turn, script_default):
    """Plays one turn as the engine would: the user message, then one request
    per step with the earlier steps' assistant messages and tool results."""
    messages = [{"role": "system", "content": "self-test"}, {"role": "user", "content": turn["when"]}]
    tools = [{"type": "function", "function": {"name": "bash", "parameters": {"type": "object"}}}]
    for j, step in enumerate(turn["steps"]):
        label = f"script {name} turn {i} ({turn['when'][:40]!r}) step {j}"
        err = step.get("error")
        req = {"model": "fake-large", "stream": True, "messages": messages, "tools": tools}
        if isinstance(err, dict):
            times = err.get("times")
            for k in range(int(times) if times is not None else 1):
                status, headers, raw = http(port, "POST", "/v1/chat/completions", req)
                r.check(status == int(err.get("status", 500)),
                        f"{label}: error {k + 1} answers {status} (want {err.get('status')})")
                try:
                    json.loads(raw)
                    ok_body = True
                except ValueError:
                    ok_body = False
                r.check(ok_body, f"{label}: the error body is JSON")
                for hk, hv in (err.get("headers") or {}).items():
                    r.check({k.lower(): v for k, v in headers.items()}.get(hk.lower()) == str(hv), f"{label}: error header {hk}")
            if times is None:
                return  # the run ends with this error
        status, _, raw = http(port, "POST", "/v1/chat/completions", req)
        if not r.check(status == 200, f"{label}: answers 200 ({status})"):
            return
        check_step_stream(r, label, raw, step)
        calls = step.get("tool_calls") or []
        assistant = {"role": "assistant", "content": step.get("text") or None}
        if calls:
            ids = [f"call_{j}_{k}" for k in range(len(calls))]
            assistant["tool_calls"] = [{"id": cid, "type": "function", "function": {"name": c["name"], "arguments": json.dumps(c.get("arguments", {}))}}
                                       for cid, c in zip(ids, calls)]
        messages = messages + [assistant] + [{"role": "tool", "tool_call_id": cid, "content": fake_tool_result(c)}
                                             for cid, c in zip(ids if calls else [], calls)]
        if (step.get("finish") or ("tool_calls" if calls else "stop")) != "tool_calls":
            return
    # The engine asks once more after the last step's tool results.
    status, _, raw = http(port, "POST", "/v1/chat/completions", {"model": "fake-large", "stream": True, "messages": messages, "tools": tools})
    if r.check(status == 200, f"script {name} turn {i}: the request after the last step answers 200"):
        check_step_stream(r, f"script {name} turn {i}: after the last step the script's default plays", raw, script_default)


def play_scripts(r, scripts):
    fake = Fake(scripts, time_scale=0.0)
    srv = make_server(fake)
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    port = srv.server_address[1]
    try:
        for name, script in scripts.items():
            status, _, _ = http(port, "POST", "/_fake/control", {"script": name})
            r.check(status == 200, f"script {name}: selected through /_fake/control")
            default = script.get("default") if isinstance(script.get("default"), dict) else GLOBAL_DEFAULT
            for i, turn in enumerate(script.get("turns") or []):
                if isinstance(turn, dict) and turn.get("when") and turn.get("steps"):
                    play_turn(r, port, name, i, turn, default)
            req = {"model": "fake-large", "stream": True, "tools": [],
                   "messages": [{"role": "user", "content": "zz-nothing-matches-this-zz"}]}
            status, _, raw = http(port, "POST", "/v1/chat/completions", req)
            if r.check(status == 200, f"script {name}: an unmatched prompt answers 200"):
                check_step_stream(r, f"script {name}: an unmatched prompt plays the script's default", raw, default)
    finally:
        srv.shutdown()


def planned_seconds(step, seed):
    """How long a step streams at time scale 1: (all seconds, the seconds its
    text chunks take, the words in its text)."""
    delay = float(step.get("delay_ms") or 0) / 1000.0
    chunks = 0
    text_chunks = 0
    words = 0
    if step.get("reasoning"):
        chunks += len(word_chunks(step["reasoning"], seed ^ 1))
    if step.get("text"):
        text_chunks = len(word_chunks(step["text"], seed))
        chunks += text_chunks
        words += len(step["text"].split())
    for call in step.get("tool_calls") or []:
        chunks += 1 + len(argument_pieces(json.dumps(call.get("arguments", {}), ensure_ascii=False)))
    return (chunks + 1) * delay, text_chunks * delay, words


def check_stream2m(r, scripts):
    """The speed script (2.9): one turn streamed for about 120 s at about 20
    words a second, 12 tool calls (view, grep, bash with colors, an edit with a
    40-line diff), two Mermaid fences and a 30-row table."""
    script = scripts.get("stream2m")
    if not r.check(script is not None, "stream2m.json is in scripts/"):
        return
    turn = next((t for t in script.get("turns", []) if t.get("when") == "Stream for two minutes"), None)
    if not r.check(turn is not None, "stream2m has the turn \"Stream for two minutes\""):
        return
    total, text_time, words, calls = 0.0, 0.0, 0, []
    for j, step in enumerate(turn["steps"]):
        seconds, text_seconds, w = planned_seconds(step, stable_seed("stream2m", script["turns"].index(turn), j, step.get("text", "")))
        total += seconds
        text_time += text_seconds
        words += w
        calls += step.get("tool_calls") or []
    text = "".join(s.get("text", "") for s in turn["steps"])
    names = [c["name"] for c in calls]
    r.check(110 <= total <= 130, f"stream2m streams for {total:.1f} s (want 110-130)")
    rate = words / text_time if text_time else 0
    r.check(18 <= rate <= 22, f"stream2m's reply text streams {words} words at {rate:.1f} words a second (want about 20)")
    r.check(len(calls) == 12, f"stream2m makes {len(calls)} tool calls (want 12)")
    r.check(all(n in names for n in ("view", "grep", "bash", "edit")), f"stream2m calls view, grep, bash and edit ({sorted(set(names))})")
    colored = [c for c in calls if c["name"] == "bash" and "\\033[" in c["arguments"].get("command", "")]
    r.check(len(colored) >= 1, f"stream2m has {len(colored)} bash calls with colored output")
    big = [c for c in calls if c["name"] == "edit" and c["arguments"].get("new_string", "").count("\n") + 1 >= 40]
    r.check(len(big) >= 1, "stream2m has an edit whose new text has at least 40 lines")
    viewed = set()
    order_ok = True
    for c in calls:
        if c["name"] == "view":
            viewed.add(c["arguments"]["file_path"])
        if c["name"] == "edit" and c["arguments"]["file_path"] not in viewed:
            order_ok = False
    r.check(order_ok, "stream2m views every file before it edits it (Crush refuses an edit of an unread file)")
    r.check(text.count("```mermaid") == 2, f"stream2m has {text.count('```mermaid')} Mermaid fences (want 2)")
    rows = [line for line in text.splitlines() if line.startswith("|") and not set(line) <= set("|-: ")]
    r.check(len(rows) == 31, f"stream2m's table has {len(rows) - 1} body rows (want 30)")


def builtin_cases(r):
    """Features no script may use: timing, titles, files, beacons, control,
    non-streamed answers, errors with "times", picking order, no-tools."""
    folder = tempfile.mkdtemp(prefix="fake-ai-selftest-")
    log = os.path.join(folder, "log.jsonl")
    try:
        os.mkdir(os.path.join(folder, "scripts"))
        a = {"turns": [{"when": "alpha", "title": "Alpha title", "steps": [
                 {"delay_ms": 40, "reasoning": "think a bit", "text": "one two three four five six seven"},
             ]},
             {"when": "tools please", "steps": [
                 {"text": "Calling.", "tool_calls": [{"name": "bash", "arguments": {"command": "echo hi"}},
                                                      {"name": "view", "arguments": {"file_path": "README.md"}}]},
                 {"text": "Done."}]},
             {"when": "flaky", "steps": [{"error": {"status": 429, "body": "slow down", "times": 1, "headers": {"retry-after": "1"}},
                                          "text": "after the retry"}]},
             {"when": "broken", "steps": [{"error": {"status": 500, "body": "{\"error\": {\"message\": \"boom\"}}"}}]},
             {"when": "refuse", "steps": [{"text": "I can't.", "finish": "content_filter"}]}],
             "default": {"text": "A default."}}
        b = {"turns": [{"when": "alpha beta", "steps": [{"text": "from b"}]}], "default": {"text": "B default."}}
        for name, s in (("a", a), ("b", b)):
            with open(os.path.join(folder, "scripts", name + ".json"), "w") as f:
                json.dump(s, f)
        fake = Fake(load_scripts(os.path.join(folder, "scripts")), time_scale=1.0, log_path=log)
        srv = make_server(fake)
        threading.Thread(target=srv.serve_forever, daemon=True).start()
        port = srv.server_address[1]
        tools = [{"type": "function", "function": {"name": "bash"}}]

        def ask(text, stream=True, with_tools=True, extra=None):
            msgs = [{"role": "user", "content": text}] + (extra or [])
            return http(port, "POST", "/v1/chat/completions",
                        {"model": "fake-large", "stream": stream, "messages": msgs, "tools": tools if with_tools else []})

        started = time.time()
        status, _, raw = ask("alpha")
        spent = time.time() - started
        problems, out = parse_stream(raw)
        n = len(out["text_chunks"]) + len(word_chunks("think a bit", stable_seed("a", 0, 0, a["turns"][0]["steps"][0]["text"]) ^ 1))
        r.check(status == 200 and not problems and out["text"] == a["turns"][0]["steps"][0]["text"] and out["reasoning"] == "think a bit",
                "built-in: text and reasoning stream and reassemble")
        r.check(spent >= n * 0.04 * 0.9, f"built-in: delay_ms spaces the chunks ({n} chunks took {spent:.2f} s, want at least {n * 0.04:.2f})")
        status, _, raw2 = ask("alpha")
        r.check(parse_stream(raw2)[1]["text_chunks"] == out["text_chunks"], "built-in: the same step streams the same chunks every run")

        fake.time_scale = 0.0
        _, _, raw = ask("alpha beta")
        r.check(parse_stream(raw)[1]["text"] == a["turns"][0]["steps"][0]["text"],
                "built-in: with no script active, scripts are tried in name order (a before b)")
        http(port, "POST", "/_fake/control", {"script": "b"})
        _, _, raw = ask("alpha beta")
        r.check(parse_stream(raw)[1]["text"] == "from b", "built-in: the active script's turns are tried first")
        _, _, raw = ask("nothing at all")
        r.check(parse_stream(raw)[1]["text"] == "B default.", "built-in: the active script's default answers an unmatched prompt")
        status, _, body = http(port, "POST", "/_fake/control", {"script": "nope"})
        r.check(status == 404, "built-in: selecting an unknown script answers 404")
        http(port, "POST", "/_fake/control", {"script": None})
        _, _, raw = ask("nothing at all")
        r.check(parse_stream(raw)[1]["text"] == "OK.", "built-in: with no script active an unmatched prompt gets OK.")
        status, _, body = http(port, "GET", "/_fake/control")
        r.check(status == 200 and json.loads(body).get("scripts") == ["a", "b"], "built-in: GET /_fake/control reports the loaded scripts")

        _, _, raw = ask("tools please")
        out = parse_stream(raw)[1]
        r.check(out["finish"] == "tool_calls" and len(out["calls"]) == 2, "built-in: tool calls stream and finish with tool_calls")
        prev = [{"role": "assistant", "content": "Calling.", "tool_calls": [{"id": "x", "type": "function", "function": {"name": "bash", "arguments": "{}"}}]},
                {"role": "tool", "tool_call_id": "x", "content": "hi"}]
        _, _, raw = ask("tools please", extra=prev)
        r.check(parse_stream(raw)[1]["text"] == "Done.", "built-in: the step index is the number of assistant messages after the user message")
        _, _, raw = ask("tools please", extra=prev + [{"role": "assistant", "content": "Done."}])
        r.check(parse_stream(raw)[1]["text"] == "A default.", "built-in: past the last step the turn's script default answers")
        _, _, raw = ask("tools please", with_tools=False)
        out = parse_stream(raw)[1]
        r.check(not out["calls"] and out["finish"] == "stop" and out["text"] == "Calling.",
                "built-in: a request without tools never gets tool calls")

        status, headers, body = ask("flaky")
        r.check(status == 429 and {k.lower(): v for k, v in headers.items()}.get("retry-after") == "1",
                f"built-in: an error with times 1 answers 429 with its header first ({status})")
        status, _, raw = ask("flaky")
        r.check(status == 200 and parse_stream(raw)[1]["text"] == "after the retry", "built-in: the retry gets the step itself")
        status, _, body = ask("broken")
        r.check(status == 500 and json.loads(body)["error"]["message"] == "boom", "built-in: an error without times answers every time, body as given")
        status, _, body = ask("broken")
        r.check(status == 500, "built-in: the error without times answers again")
        _, _, raw = ask("refuse")
        r.check(parse_stream(raw)[1]["finish"] == "content_filter", "built-in: finish content_filter streams as given")

        title_req = TITLE_PREFIX + "\n\nalpha and more words here for the title\n <think>\n\n</think>"
        _, _, raw = ask(title_req, with_tools=False)
        r.check(parse_stream(raw)[1]["text"] == "Alpha title", "built-in: a title request gets the matching turn's title")
        _, _, raw = ask(TITLE_PREFIX + "\n\nfirst from the desktop app today please\n <think>\n\n</think>", with_tools=False)
        r.check(parse_stream(raw)[1]["text"] == "first from the desktop app today", "built-in: other titles are the content's first six words")

        status, _, body = ask("alpha", stream=False)
        msg = json.loads(body)["choices"][0]["message"] if status == 200 else {}
        r.check(msg.get("content") == a["turns"][0]["steps"][0]["text"] and msg.get("reasoning_content") == "think a bit",
                "built-in: a request without stream gets the whole step as one completion")
        status, _, body = ask("tools please", stream=False)
        calls = json.loads(body)["choices"][0]["message"].get("tool_calls") if status == 200 else None
        r.check(bool(calls) and json.loads(calls[0]["function"]["arguments"]) == {"command": "echo hi"},
                "built-in: a request without stream gets its tool calls whole")

        status, headers, body = http(port, "GET", "/file.txt")
        r.check(status == 200 and body.decode() == FILE_TXT, "built-in: GET /file.txt serves the Download file")
        status, headers, body = http(port, "GET", "/page.html")
        r.check(status == 200 and b"<h1>Fake page</h1>" in body, "built-in: GET /page.html serves the Fetch page")
        status, _, body = http(port, "GET", "/v1/models")
        r.check(status == 200 and [m["id"] for m in json.loads(body)["data"]] == MODELS, "built-in: GET /v1/models lists the models")
        status, _, _ = http(port, "GET", "/beacon?x=1")
        r.check(status == 404, "built-in: any other GET answers 404")
        srv.shutdown()
        entries = [json.loads(line) for line in open(log)]
        r.check(any(e.get("path") == "/beacon?x=1" and e.get("beacon") is True for e in entries),
                "built-in: the other GET is logged as a beacon")
        r.check(not any(e.get("beacon") for e in entries if e.get("path") in ("/file.txt", "/page.html", "/v1/models", "/_fake/control")),
                "built-in: files, models and control are never beacons")
        posts = [e for e in entries if e.get("method") == "POST" and e.get("kind") != "control"]
        r.check(all(isinstance(e.get("messages"), int) and "last_user" in e and "status" in e for e in posts),
                "built-in: every request is logged with its message count, last user message and status")
        r.check(any(e.get("kind") == "title" for e in posts) and any(e.get("tools_dropped") for e in posts),
                "built-in: the log marks title requests and dropped tool calls")
    finally:
        shutil.rmtree(folder, ignore_errors=True)


# Engine checks (inside tests/sandbox.sh only) -------------------------------------

DESKTOP = os.path.dirname(os.path.dirname(HERE))
ECHO_TEXT = "echo check 42"
# A script for the engine checks alone, served beside the real ones by the
# checks' own server (it is never written to scripts/).
ENGINE_CHECK_SCRIPT = {"turns": [{"when": "Engine check: call the echo tool", "steps": [
    {"text": "Calling the echo tool.", "tool_calls": [{"name": "mcp_fake_echo", "arguments": {"text": ECHO_TEXT}}]},
    {"text": "The echo tool answered."}]}], "default": {"text": "OK."}}
TOOL_MODELS = [("claude", "claude-code/opus", "Claude Code"), ("codex", "codex-cli/gpt-6-luna", "Codex"),
               ("grok", "grok-cli/grok-4.7", "Grok"), ("opencode", "opencode-cli/opencode-go/glm-5.3", "OpenCode"),
               ("agy", "agy-cli/gemini-3.8-flash-low", "Antigravity")]
BASIC_DONE = "Done: the README describes a throwaway project for the window checks."


def read_jsonl(path):
    entries = []
    if os.path.exists(path):
        with open(path) as f:
            for line in f:
                try:
                    entries.append(json.loads(line))
                except ValueError:
                    pass
    return entries


def flat(text):
    return " ".join((text or "").split())


def load_module(name):
    spec = importlib.util.spec_from_file_location(name, os.path.join(HERE, name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def wait_for(read, ok, seconds):
    """Calls read until ok(value) or the time is up; returns the last value."""
    deadline = time.time() + seconds
    while True:
        value = read()
        if ok(value) or time.time() > deadline:
            return value
        time.sleep(0.2)


def engine_project(work, port, logs):
    """tests/project copied into work, with the fake provider on port, the fake
    MCP server (and a broken one) and the fake LSP server."""
    proj = os.path.join(work, "proj")
    shutil.copytree(os.path.join(DESKTOP, "tests", "project"), proj, ignore=shutil.ignore_patterns("crush.json.in"))
    subprocess.run(["git", "-C", proj, "init", "-q"], check=True)
    with open(os.path.join(DESKTOP, "tests", "project", "crush.json.in")) as f:
        cfg = json.loads(f.read().replace("@PORT@", str(port)))
    py = sys.executable
    cfg["mcp"] = {
        "fake": {"type": "stdio", "command": py, "args": ["-I", os.path.join(HERE, "fake_mcp.py"), "--push-on", logs["push"]],
                 "env": {"FAKE_MCP_LOG": logs["mcp"]}, "channel_enabled": True},
        "broken": {"type": "stdio", "command": py, "args": ["-I", os.path.join(HERE, "fake_mcp.py"), "--fail"]},
    }
    cfg["lsp"] = {"fake": {"command": py, "args": ["-I", os.path.join(HERE, "fake_lsp.py")], "filetypes": ["go"],
                           "root_markers": ["go.mod"], "env": {"FAKE_LSP_LOG": logs["lsp"]}}}
    with open(os.path.join(proj, "crush.json"), "w") as f:
        json.dump(cfg, f, indent=2)
    return proj


def crush_run(crush, proj, env, prompt, model=None, timeout=180):
    args = [crush, "run", "-q"] + (["-m", model] if model else []) + [prompt]
    p = subprocess.run(args, cwd=proj, env=env, capture_output=True, text=True, timeout=timeout, stdin=subprocess.DEVNULL)
    return p.returncode, p.stdout, p.stderr


def turn_text(scripts, when):
    for script in scripts.values():
        for turn in script.get("turns", []):
            if turn.get("when") == when:
                return "".join(s.get("text", "") for s in turn.get("steps", []))
    return ""


def check_engine_turns(r, crush, proj, env, logs, scripts):
    """`crush run` with the openai-compat provider: a seed turn, then stream2m
    played at time scale 0, which must leave its edit in speed/session.go."""
    code, out, err = crush_run(crush, proj, env, "Seed turn short 1")
    want = flat(turn_text(scripts, "Seed turn short"))
    r.check(code == 0 and want and want in flat(out), f"engine: a seed turn prints the script's reply (exit {code}{', ' + err.strip()[-200:] if code else ''})")
    before = open(os.path.join(proj, "auth", "session.go")).read()
    code, out, err = crush_run(crush, proj, env, "Stream for two minutes", timeout=400)
    r.check(code == 0, f"engine: stream2m plays to the end at time scale 0 (exit {code}{', ' + err.strip()[-200:] if code else ''})")
    played = sorted({e.get("step") for e in read_jsonl(logs["ai"])
                     if e.get("kind") == "turn" and e.get("script") == "stream2m" and e.get("turn") == 0 and e.get("status") == 200})
    r.check(played == list(range(13)), f"engine: the engine asked for all 13 stream2m steps (got {played})")
    copy = os.path.join(proj, "speed", "session.go")
    text = open(copy).read() if os.path.exists(copy) else ""
    r.check("func NewCookieWith(" in text and "12 * time.Hour" in text,
            "engine: stream2m's two edits landed in speed/session.go (the view before each edit, the copy, the edit arguments)")
    r.check(open(os.path.join(proj, "auth", "session.go")).read() == before, "engine: stream2m left auth/session.go alone")
    titles = [e for e in read_jsonl(logs["ai"]) if e.get("kind") == "title" and e.get("script") == "stream2m" and e.get("turn") == 0]
    r.check(bool(titles), "engine: the engine asked for stream2m's title")


def check_engine_tools(r, crush, proj, env, logs):
    """One turn per fake AI tool with each stream, checked against the tool log."""
    for tool, model, label in TOOL_MODELS:
        for prompt, want in (("basic hello", BASIC_DONE), ("hello there", f"Reply from the fake {label}.")):
            start = len(read_jsonl(logs["tools"]))
            code, out, err = crush_run(crush, proj, env, prompt, model=model)
            ok = r.check(code == 0 and want in out, f"engine: {tool} replays its {prompt.split()[0] if prompt.startswith('basic') else 'default'} stream "
                         f"(exit {code}{', ' + err.strip()[-200:] if code else ''})")
            if not ok or prompt != "basic hello":
                continue
            entries = [e for e in read_jsonl(logs["tools"])[start:] if e.get("tool") == tool]
            lines = []
            for e in entries:
                try:
                    lines.append(json.loads(e.get("stdin") or "null"))
                except ValueError:
                    pass
            lines = [x for x in lines if isinstance(x, dict)]
            methods = {x.get("method") for x in lines}
            if tool == "claude":
                shake = any((x.get("request") or {}).get("subtype") == "initialize" for x in lines) and any(x.get("type") == "user" for x in lines)
            elif tool == "codex":
                shake = {"initialize", "hooks/list", "thread/start", "turn/start"} <= methods
            elif tool in ("grok", "opencode"):
                shake = {"initialize", "session/new", "session/prompt"} <= methods and any(
                    ((x.get("result") or {}).get("outcome") or {}).get("outcome") == "selected" for x in lines)
            else:
                shake = any(x.get("event") == "user" for x in lines)
            r.check(any("args" in e for e in entries) and shake, f"engine: {tool}'s log has its start and protocol lines")


def check_engine_api(r, crush, proj, env, logs):
    """Through the project's engine, joined first and kept as an attached
    client (as the desktop app is while a driver seeds): seed_chat.py, then the
    seeded chat, MCP, LSP and the push over the engine's API."""
    sc = load_module("seed_chat")
    api = sc.Engine(sc.ensure(crush, proj))
    client = str(uuid.uuid4())
    ws = api.call("POST", "/v1/workspaces", {"path": proj, "data_dir": "", "version": "self-test", "protocol": 1,
                                             "client_id": client, "yolo": True, "env": ["%s=%s" % kv for kv in env.items()]})
    base, q = "/v1/workspaces/" + ws["id"], "?client_id=" + client
    del ws  # holds the environment; never print it
    events = api.follow_events(base + "/events" + q)
    try:
        api.call("POST", base + "/agent/init" + q, {"interactive": True}, timeout=120)
        seed = subprocess.run([sys.executable, "-I", os.path.join(HERE, "seed_chat.py"), "--cwd", proj, "--items", "12",
                               "--crush", crush, "--title", "Seeded for checks"], env=env, capture_output=True, text=True, timeout=400)
        if not r.check(seed.returncode == 0, f"engine: seed_chat.py --items 12 succeeds ({seed.stderr.strip()[-300:]})"):
            return
        info = json.loads(seed.stdout)
        r.check(info.get("turns") == 5 and info.get("messages") == 12,
                f"engine: seed_chat made 5 turns and 12 messages (got {info.get('turns')} and {info.get('messages')})")
        r.check(info.get("workspace_id") == base.rsplit("/", 1)[1], "engine: seed_chat joined the workspace the checks hold")
        sid = info["session_id"]
        seeded = next((s for s in api.call("GET", base + "/sessions") if s["id"] == sid), {})
        r.check(seeded.get("title") == "Seeded for checks" and seeded.get("message_count") == 12,
                f"engine: the seeded chat is listed with its title and 12 messages (got {seeded.get('title')!r}, {seeded.get('message_count')})")
        roles = [m["role"] for m in api.call("GET", "%s/sessions/%s/messages" % (base, sid))]
        r.check(roles == ["user", "assistant"] * 4 + ["user", "assistant", "tool", "assistant"],
                f"engine: the seeded messages come in turn order (got {roles})")
        asked = [e.get("last_user", "") for e in read_jsonl(logs["ai"]) if e.get("kind") == "turn" and "Seed turn" in e.get("last_user", "")]
        kinds = [re.search(r"Seed turn (\w+) (\d+)", t).group(1, 2) for t in asked if re.search(r"Seed turn (\w+) (\d+)", t)]
        r.check(("short", "1") in kinds and ("tool", "5") in kinds, "engine: the fake server answered the seeded prompts")

        states = wait_for(lambda: api.call("GET", base + "/mcp/states"),
                          lambda s: (s.get("fake") or {}).get("state") == "connected" and (s.get("broken") or {}).get("state") == "error", 20)
        # The engine lists a server's resources only when asked to refresh
        # them (the terminal's MCP command does), so the count needs this.
        api.call("POST", base + "/mcp/refresh-resources" + q, {"name": "fake"})
        states = wait_for(lambda: api.call("GET", base + "/mcp/states"), lambda s: (s.get("fake") or {}).get("resource_count") == 1, 10)
        fake = states.get("fake") or {}
        r.check(fake.get("state") == "connected" and fake.get("tool_count") == 1 and fake.get("resource_count") == 1
                and not fake.get("prompt_count") and fake.get("channel") is True,
                f"engine: the fake MCP server is connected with 1 tool, 0 prompts, 1 resource and its channel (got {fake})")
        r.check((states.get("broken") or {}).get("state") == "error", f"engine: the broken MCP server is in error (got {states.get('broken')})")
        contents = api.call("POST", base + "/mcp/read-resource" + q, {"name": "fake", "uri": "fake://notes/today.md"})
        note = load_module("fake_mcp").NOTE
        r.check(bool(contents) and contents[0].get("text") == note, "engine: the fake MCP resource reads back its text")

        esid = api.call("POST", base + "/sessions" + q, {"title": "Echo check"})["id"]
        api.call("POST", base + "/agent" + q, {"session_id": esid, "prompt": "Engine check: call the echo tool"})
        done = wait_for(lambda: api.call("GET", "%s/sessions/%s" % (base, esid)), lambda s: s["message_count"] >= 4 and not s["is_busy"], 60)
        calls = [m["in"] for m in read_jsonl(logs["mcp"]) if (m.get("in") or {}).get("method") == "tools/call"]
        r.check(bool(calls) and calls[-1].get("params") == {"name": "echo", "arguments": {"text": ECHO_TEXT}},
                f"engine: a turn calls mcp_fake_echo with its arguments (calls {calls})")
        msgs = api.call("GET", "%s/sessions/%s/messages" % (base, esid))
        r.check(done["message_count"] >= 4 and any(m["role"] == "tool" and ECHO_TEXT in json.dumps(m["parts"]) for m in msgs),
                "engine: the echo tool's result is in the chat")

        lsps = wait_for(lambda: api.call("GET", base + "/lsps"), lambda l: (l.get("fake") or {}).get("state") in (2, "ready"), 15)
        r.check((lsps.get("fake") or {}).get("state") in (2, "ready"), f"engine: the fake LSP is ready (got {lsps.get('fake')})")
        diags = wait_for(lambda: api.call("GET", base + "/lsps/fake/diagnostics"),
                         lambda d: any(u.endswith("/auth/session.go") and len(v) == 2 for u, v in d.items()), 10)
        found = next((v for u, v in diags.items() if u.endswith("/auth/session.go")), [])
        r.check(sorted(d.get("severity") for d in found) == [1, 2],
                f"engine: the fake LSP's error and warning reached the engine for the viewed auth/session.go (got {found})")

        open(logs["push"], "w").close()
        pushed = wait_for(lambda: [e for e in read_jsonl(logs["ai"]) if '<channel source="fake"' in e.get("last_user", "")], bool, 20)
        r.check(bool(pushed) and "Build finished: 12 tests passed." in pushed[0]["last_user"],
                "engine: the fake MCP channel push became a turn (the engine routes it)")
        bound = wait_for(lambda: [s for s in api.call("GET", base + "/sessions") if s.get("channel") == "fake"], bool, 10)
        r.check(bool(bound), "engine: the push's chat is bound to the fake channel")
    finally:
        try:
            api.call("DELETE", "/v1/clients/" + client)
        except (sc.ApiError, OSError):
            pass
        events.close()


def engine_checks(r, crush, scripts):
    """Real engine checks, inside tests/sandbox.sh only (it hides the owner's
    files and screen): `crush run` turns against this server (a seed turn,
    stream2m at time scale 0 with its edit landing), a turn per fake AI tool,
    and through `crush engine ensure`: seed_chat.py, the seeded chat over the
    engine's API, the fake MCP server (state, resource, echo called from a
    turn, the channel push becoming a turn), a broken MCP server, and the
    fake LSP server's diagnostics."""
    if os.environ.get("HOME") != "/tmp/home" or not os.environ.get("DESKTOP_ROOT"):
        r.check(False, "engine checks: run them inside tests/sandbox.sh")
        return
    crush = os.path.abspath(crush)
    if not r.check(os.access(crush, os.X_OK), f"engine checks: {crush} is executable"):
        return
    work = tempfile.mkdtemp(prefix="engine-checks-", dir="/tmp")
    logs = {name: os.path.join(work, name + ".jsonl") for name in ("ai", "tools", "mcp", "lsp")}
    logs["push"] = os.path.join(work, "push-now")
    srv = make_server(Fake({**scripts, "zz_engine_checks": ENGINE_CHECK_SCRIPT}, None, 0.0, logs["ai"]))
    threading.Thread(target=srv.serve_forever, daemon=True).start()
    env = dict(os.environ, FAKE_TOOL_LOG=logs["tools"])
    try:
        proj = engine_project(work, srv.server_address[1], logs)
        check_engine_turns(r, crush, proj, env, logs, scripts)
        check_engine_tools(r, crush, proj, env, logs)
        check_engine_api(r, crush, proj, env, logs)
    except Exception as e:  # a crash is a failed check, with its reason
        r.check(False, f"engine checks stopped: {type(e).__name__}: {e}")
    finally:
        srv.shutdown()
        out = os.path.join(DESKTOP, "tests", "out", "fake_ai_server")
        os.makedirs(out, exist_ok=True)
        for name in ("ai", "tools", "mcp", "lsp"):
            if os.path.exists(logs[name]):
                shutil.copy(logs[name], os.path.join(out, f"engine-{name}.jsonl"))


def self_test(scripts_dir, engine=None):
    r = Results()
    try:
        scripts = load_scripts(scripts_dir)
    except ValueError as e:
        r.check(False, f"every script in {scripts_dir} is JSON ({e})")
        scripts = {}
    r.check(bool(scripts), f"{len(scripts)} scripts loaded from {scripts_dir}: {', '.join(scripts)}")
    check_shapes(r, scripts)
    check_reachable(r, scripts)
    play_scripts(r, scripts)
    check_stream2m(r, scripts)
    builtin_cases(r)
    if engine:
        engine_checks(r, engine, scripts)
    print(("ALL PASSED" if r.failures == 0 else f"{r.failures} FAILED") + f" ({r.passes} checks passed)", flush=True)
    return 1 if r.failures else 0


if __name__ == "__main__":
    sys.exit(main())
