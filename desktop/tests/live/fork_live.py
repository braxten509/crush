#!/usr/bin/env python3
"""Live fork checks for one AI tool (desktop design 3.5, piece P4.3).

    fork_live.py claude|codex|grok|opencode|agy   run every step live
    fork_live.py --judge-only DIR                 re-judge saved evidence

Each tool runs in its own throwaway project /var/tmp/crush-desktop-live/<tool>/,
on CPUs 6-11,18-23, under `timeout 300`, with API-key variables removed so
only subscription logins are used. Native IDs and markers come only from the
tools' own output. Raw output goes to desktop/docs/evidence/phase4/live-<tool>/.

Steps: 1 help and version; 2 a tiny chat (ALPHA, BETA) with markers; 3 fork at
the first reply and ask which words were said (ALPHA only); 4 ask the source
(ALPHA and BETA); 5 fork while the source is mid-reply and send at once (the
branch answers from the cut, the source finishes counting). AGY: steps 1-2.
"""
import argparse
import json
import os
import queue
import re
import shutil
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request

DESKTOP = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
EVIDENCE = os.path.join(DESKTOP, "docs", "evidence", "phase4")
LIVE_ROOT = "/var/tmp/crush-desktop-live"
WRAP = ["timeout", "300", "taskset", "-c", "6-11,18-23"]
WAIT = 300

P_ALPHA = "Say the word ALPHA, nothing else."
P_BETA = "Say the word BETA."
P_WHICH = "Which words did you say?"
# A short sentence per number keeps the source streaming long enough for the
# branch to start while it runs (the bare count ends in about a second).
P_COUNT = "Count slowly from 1 to 20, one number per line. After each number, add one short sentence about it."

MODELS = {
    "claude": "haiku",
    "codex": None,  # picked from model/list: a "mini" model at low effort
    "grok": "grok-4.7",  # the only model offered; low effort
    "opencode": "opencode-go/deepseek-v4.1-flash",  # OpenCode Go subscription
    "agy": "gemini-3.8-flash-low",
}


def tool_env(extra=None):
    """The environment with every API-key variable removed."""
    env = {k: v for k, v in os.environ.items()
           if not (k.endswith("_API_KEY") or k in ("ANTHROPIC_AUTH_TOKEN", "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL"))}
    env.update(extra or {})
    return env


SECRET_KEY = re.compile(r"api[_-]?key|secret|password|email|bearer|authorization|(^|_)token$|[a-z]Token$|^token$", re.I)
EMAIL = re.compile(r"[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+\.[A-Za-z0-9.-]+")
ACCOUNT = re.compile(r"\b(team|account|org|organization|user)( id)?[ :=]+[0-9a-f]{8}-[0-9a-f-]{27}", re.I)


def sanitize(v, key=""):
    if isinstance(v, dict):
        return {k: sanitize(x, k) for k, x in v.items()}
    if isinstance(v, list):
        return [sanitize(x) for x in v]
    if isinstance(v, str):
        if key and SECRET_KEY.search(key):
            return "<redacted>"
        return ACCOUNT.sub(r"\1 <redacted>", EMAIL.sub("<redacted>", v))
    return v


class Log:
    """One step's raw output: one JSON record per line sent or received."""

    def __init__(self, path):
        self.f = open(path, "w")
        self.t0 = time.time()
        self.lock = threading.Lock()

    def now(self):
        return round(time.time() - self.t0, 3)

    def rec(self, proc, kind, msg):
        line = json.dumps(sanitize({"t": self.now(), "proc": proc, "dir": kind, "msg": msg}))
        with self.lock:
            self.f.write(line + "\n")
            self.f.flush()

    def answer(self, proc, text):
        self.rec(proc, "answer", text)

    def close(self):
        self.f.close()


class Proc:
    """A tool process; stdout lines are logged and queued (JSON when they parse)."""

    def __init__(self, log, label, cmd, cwd, env=None):
        self.log, self.label = log, label
        log.rec(label, "start", cmd)
        self.p = subprocess.Popen(WRAP + cmd, cwd=cwd, env=tool_env(env), stdin=subprocess.PIPE,
                                  stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, bufsize=1)
        self.q = queue.Queue()
        threading.Thread(target=self._read, daemon=True).start()
        threading.Thread(target=self._err, daemon=True).start()

    def _read(self):
        for line in self.p.stdout:
            line = line.rstrip("\n")
            try:
                msg = json.loads(line)
            except ValueError:
                msg = line
            self.log.rec(self.label, "recv", msg)
            self.q.put(msg)
        self.q.put(None)

    def _err(self):
        for line in self.p.stderr:
            self.log.rec(self.label, "stderr", line.rstrip("\n")[:2000])

    def send(self, obj):
        self.log.rec(self.label, "send", obj)
        try:
            self.p.stdin.write(json.dumps(obj) + "\n")
            self.p.stdin.flush()
        except (BrokenPipeError, ValueError):
            pass

    def next(self, timeout=WAIT):
        try:
            return self.q.get(timeout=timeout)
        except queue.Empty:
            raise RuntimeError(f"{self.label}: no output for {timeout}s")

    def close(self):
        try:
            self.p.stdin.close()
        except (BrokenPipeError, ValueError):
            pass
        try:
            self.p.wait(10)
        except subprocess.TimeoutExpired:
            self.p.kill()
            self.p.wait()
        self.log.rec(self.label, "exit", self.p.returncode)


class RpcError(Exception):
    pass


class Rpc(Proc):
    """JSON-RPC over stdio (Codex app-server and ACP agents)."""

    def __init__(self, log, label, cmd, cwd, env=None, jsonrpc=True, deny=None):
        super().__init__(log, label, cmd, cwd, env)
        self.jsonrpc, self.n, self.deny, self.done = jsonrpc, 0, deny, {}

    def _msg(self, d):
        if self.jsonrpc:
            d = {"jsonrpc": "2.0", **d}
        return d

    def notify(self, method, params=None):
        self.send(self._msg({"method": method, **({"params": params} if params is not None else {})}))

    def start(self, method, params):
        self.n += 1
        rid = f"r{self.n}"
        self.send(self._msg({"id": rid, "method": method, "params": params}))
        return rid

    def pump(self, until, on_note=None):
        """Reads messages until until() is true; answers the tool's own requests (all denied)."""
        while not until():
            msg = self.next()
            if msg is None:
                raise RpcError(f"{self.label}: process ended")
            if not isinstance(msg, dict):
                continue
            if "method" in msg and "id" in msg:
                self.send(self._msg({"id": msg["id"], "result": self.deny(msg) if self.deny else {}}))
            elif "method" in msg:
                if on_note:
                    on_note(msg)
            elif "id" in msg:
                self.done[str(msg["id"])] = msg

    def wait(self, rid, on_note=None):
        self.pump(lambda: rid in self.done, on_note)
        msg = self.done.pop(rid)
        if "error" in msg:
            raise RpcError(json.dumps(msg["error"]))
        return msg.get("result")

    def request(self, method, params, on_note=None):
        return self.wait(self.start(method, params), on_note)


def has(text, word):
    return re.search(r"\b" + word + r"\b", text or "", re.I) is not None


# ---------------------------------------------------------------- Claude

class Claude:
    """Claude Code in stream-json print mode, as claude.go runs it."""
    name = "claude"
    help_cmds = [["claude", "--version"], ["claude", "--help"]]
    marker_kind = "message uuid"

    def __init__(self, cwd):
        self.cwd = cwd
        self.model = MODELS["claude"]

    def fork_call(self, src, marker):
        return ["--resume", src, "--fork-session", "--resume-session-at", marker]

    def _run(self, log, label, prompt, extra, on_first=None):
        args = ["-p", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
                "--include-partial-messages", "--no-chrome", "--replay-user-messages", "--model", self.model,
                "--tools", "", "--strict-mcp-config"] + extra
        env = {"CLAUDE_CODE_DISABLE_AUTO_MEMORY": "1", "CLAUDE_CODE_DISABLE_ORG_MEMORY": "1",
               "CLAUDE_CODE_DISABLE_CLAUDE_MDS": "1", "MCP_CONNECTION_NONBLOCKING": "0"}
        p = Proc(log, label, ["claude"] + args, self.cwd, env)
        r = {"native": None, "text": "", "assistant_uuids": [], "user_uuids": [], "t_start": log.now()}
        p.send({"type": "user", "message": {"role": "user", "content": prompt}})
        first = False
        try:
            while True:
                msg = p.next()
                if msg is None:
                    break
                if not isinstance(msg, dict):
                    continue
                kind = msg.get("type")
                if kind == "system" and msg.get("subtype") == "init":
                    r["native"], r["t_init"] = msg.get("session_id"), log.now()
                elif kind == "user" and msg.get("uuid"):
                    r["user_uuids"].append(msg["uuid"])
                elif kind == "assistant" and msg.get("uuid"):
                    r["assistant_uuids"].append(msg["uuid"])
                elif kind == "stream_event" and not first and on_first:
                    ev = msg.get("event", {})
                    if ev.get("type") == "content_block_delta" and ev.get("delta", {}).get("text"):
                        first = True
                        on_first()
                elif kind == "result":
                    r["text"], r["error"] = msg.get("result") or "", msg.get("is_error")
                    r["native"] = r["native"] or msg.get("session_id")
                    break
        finally:
            r["t_end"] = log.now()
            p.close()
        log.answer(label, r["text"])
        r["marker"] = r["assistant_uuids"][-1] if r["assistant_uuids"] else None
        return r

    def turn(self, log, label, native, prompt, on_first=None):
        return self._run(log, label, prompt, ["--resume", native] if native else [], on_first)

    def fork_turn(self, log, label, src, marker, prompt):
        r = self._run(log, label, prompt, self.fork_call(src, marker))
        r["fork_call"] = "claude " + " ".join(self.fork_call(src, marker)) + " (with the stream-json print flags)"
        r["t_fork"] = r.get("t_init")
        return r


# ---------------------------------------------------------------- Codex

class Codex:
    """`codex app-server` over JSON-RPC, as codex.go runs it."""
    name = "codex"
    help_cmds = [["codex", "--version"], ["codex", "--help"], ["codex", "app-server", "--help"]]
    marker_kind = "turn id"

    def __init__(self, cwd):
        self.cwd = cwd
        self.model = MODELS["codex"]

    def extra_help(self):
        out = os.path.join(LIVE_ROOT, "codex-schema")
        shutil.rmtree(out, ignore_errors=True)
        subprocess.run(WRAP + ["codex", "app-server", "generate-json-schema", "--out", out], cwd=self.cwd,
                       env=tool_env(), capture_output=True)
        try:
            with open(os.path.join(out, "v2", "ThreadForkParams.json")) as f:
                schema = json.load(f)
        except OSError as e:
            return f"ThreadForkParams schema unavailable: {e}"
        props = {k: schema["properties"][k] for k in ("threadId", "lastTurnId") if k in schema.get("properties", {})}
        return "$ codex app-server generate-json-schema (v2/ThreadForkParams.json, fork fields)\n" + \
            json.dumps({"required": schema.get("required"), "properties": props}, indent=1)

    def _open(self, log, label):
        deny = lambda m: {"decision": "decline"}
        p = Rpc(log, label, ["codex", "app-server", "--disable", "memories", "-c", "project_doc_max_bytes=0"],
                self.cwd, jsonrpc=False, deny=deny)
        p.request("initialize", {"clientInfo": {"name": "crush-live-check", "title": "Crush live check", "version": "0"},
                                 "capabilities": {"experimentalApi": True}})
        p.notify("initialized")
        if self.model is None:
            try:
                res = p.request("model/list", {}) or {}
            except RpcError as e:
                log.rec(label, "note", {"model_list_error": str(e)})
                res = {}
            models = res.get("data") or res.get("models") or []
            ids = [m.get("model") or m.get("id") for m in models]
            minis = [i for i in ids if i and "mini" in i] or [i for i in ids if i and "luna" in i]
            default = [m.get("model") or m.get("id") for m in models if m.get("isDefault")]
            self.model = (minis or default or ids or [None])[0]
            log.rec(label, "note", {"model_choice": self.model, "offered": ids})
        return p

    def _thread_params(self):
        return {"cwd": self.cwd, "model": self.model, "approvalPolicy": "never", "sandbox": "read-only"}

    def _turn(self, p, log, label, r, prompt, on_first=None):
        st = {"text": "", "deltas": "", "done": False, "first": False}

        def note(msg):
            m, prm = msg["method"], msg.get("params") or {}
            if m == "item/agentMessage/delta":
                st["deltas"] += prm.get("delta", "")
                if on_first and not st["first"]:
                    st["first"] = True
                    on_first()
            elif m == "item/completed" and (prm.get("item") or {}).get("type") == "agentMessage":
                st["text"] += prm["item"].get("text", "")
            elif m == "turn/completed":
                st["done"] = True
                st["status"] = (prm.get("turn") or {}).get("status")
                st["turn_id"] = (prm.get("turn") or {}).get("id")

        res = p.request("turn/start", {"threadId": r["native"], "effort": "low",
                                       "input": [{"type": "text", "text": prompt, "text_elements": []}]}, note)
        r["marker"] = (res.get("turn") or {}).get("id")
        p.pump(lambda: st["done"], note)
        r["text"] = st["text"] or st["deltas"]
        r["status"] = st.get("status")
        r["marker"] = r["marker"] or st.get("turn_id")
        log.answer(label, r["text"])

    def turn(self, log, label, native, prompt, on_first=None, p=None):
        r = {"t_start": log.now()}
        own = p is None
        p = p or self._open(log, label)
        try:
            if native and own:
                res = p.request("thread/resume", {"threadId": native, **self._thread_params()})
            elif not native:
                res = p.request("thread/start", self._thread_params())
            else:
                res = {"thread": {"id": native}}
            r["native"] = res["thread"]["id"]
            self._turn(p, log, label, r, prompt, on_first)
        finally:
            r["t_end"] = log.now()
            if own:
                p.close()
        return r

    def chat(self, log):
        """Step 2 in one process, as Crush keeps a Codex process between turns."""
        p = self._open(log, "source")
        try:
            a = self.turn(log, "source", None, P_ALPHA, p=p)
            b = self.turn(log, "source", a["native"], P_BETA, p=p)
        finally:
            p.close()
        return a, b

    def fork_turn(self, log, label, src, marker, prompt):
        r = {"t_start": log.now()}
        p = self._open(log, label)
        try:
            params = {"threadId": src, "lastTurnId": marker, **self._thread_params()}
            res = p.request("thread/fork", params)
            r["t_fork"] = log.now()
            r["native"] = res["thread"]["id"]
            r["fork_turns_copied"] = len(res["thread"].get("turns") or [])
            r["fork_call"] = "thread/fork " + json.dumps({"threadId": "<source thread>", "lastTurnId": "<cut turn id>"})
            self._turn(p, log, label, r, prompt)
        finally:
            r["t_end"] = log.now()
            p.close()
        return r


# ---------------------------------------------------------------- ACP (Grok, OpenCode)

def acp_deny(msg):
    return {"outcome": {"outcome": "cancelled"}}


class Acp:
    """An ACP agent over stdio, as acp.go runs it."""
    set_model = False

    def __init__(self, cwd):
        self.cwd = cwd

    def _open(self, log, label, native=None):
        p = Rpc(log, label, self.cmd, self.cwd, self.env, deny=acp_deny)
        p.request("initialize", {"protocolVersion": 1, "clientCapabilities": {
            "fs": {"readTextFile": False, "writeTextFile": False}, "terminal": False}})
        if native:
            p.request("session/load", {"sessionId": native, "cwd": self.cwd, "mcpServers": []})
            sid = native
        else:
            sid = p.request("session/new", {"cwd": self.cwd, "mcpServers": []})["sessionId"]
        if self.set_model:
            try:
                p.request("session/set_config_option", {"sessionId": sid, "configId": "model", "value": self.model})
            except RpcError:
                try:
                    p.request("session/set_model", {"sessionId": sid, "modelId": self.model})
                except RpcError as e:
                    log.rec(label, "note", {"set_model_error": str(e)})
        return p, sid

    def _prompt(self, p, log, label, sid, prompt, on_first=None, mid_prompt=None):
        st = {"text": "", "ids": [], "first": False, "mid": None, "mid_text": ""}

        def note(msg):
            prm = msg.get("params") or {}
            u = prm.get("update") or {}
            if msg.get("method") != "session/update" or prm.get("sessionId") != sid:
                return
            st.setdefault("t_update", log.now())
            # The mid-turn prompt goes 1.5 s into the turn (thinking counts):
            # Grok allows 2 model requests a second.
            if mid_prompt and not st["mid"] and not st.get("main_done") and log.now() - st["t_update"] >= 1.5:
                st["mid"] = p.start("session/prompt", {"sessionId": sid, "prompt": [{"type": "text", "text": mid_prompt}]})
                st["t_mid"] = log.now()
            if u.get("sessionUpdate") == "agent_message_chunk":
                text = (u.get("content") or {}).get("text", "")
                if st["mid"] and st.get("main_done"):
                    st["mid_text"] += text
                else:
                    st["text"] += text
                if u.get("messageId") and u["messageId"] not in st["ids"]:
                    st["ids"].append(u["messageId"])
                if text and not st["first"]:
                    st["first"] = True
                    if on_first:
                        on_first()

        rid = p.start("session/prompt", {"sessionId": sid, "prompt": [{"type": "text", "text": prompt}]})
        p.pump(lambda: rid in p.done, note)
        res = p.done.pop(rid)
        st["main_done"] = True
        r = {"native": sid, "text": st["text"], "message_ids": st["ids"], "result": res.get("result"),
             "error": res.get("error")}
        if st["mid"]:
            p.pump(lambda: st["mid"] in p.done, note)
            mres = p.done.pop(st["mid"])
            r["mid_prompt"] = {"text": mid_prompt, "sent_at": st["t_mid"], "answered_at": log.now(),
                               "result": mres.get("result"), "error": mres.get("error"), "answer": st["mid_text"]}
        log.answer(label, r["text"])
        return r

    def turn(self, log, label, native, prompt, on_first=None, p=None, mid_prompt=None):
        r0 = {"t_start": log.now()}
        own = p is None
        sid = native
        if own:
            p, sid = self._open(log, label, native)
        try:
            r = self._prompt(p, log, label, sid, prompt, on_first, mid_prompt)
            r.update(r0, t_end=log.now())  # the reply's end, not the process's exit
        finally:
            if own:
                p.close()
        return r

    def chat(self, log):
        """Step 2 in one process: Crush runs one process per turn, but the
        prompt count (Grok's marker) is the same either way."""
        p, sid = self._open(log, "source")
        try:
            a = self.turn(log, "source", sid, P_ALPHA, p=p)
            b = self.turn(log, "source", sid, P_BETA, p=p)
        finally:
            p.close()
        a["prompt_index"], b["prompt_index"] = 1, 2  # Crush's Grok marker: prompts in the session after the turn
        a["marker"], b["marker"] = self.marker_of(a), self.marker_of(b)
        return a, b


class Grok(Acp):
    name = "grok"
    help_cmds = [["grok", "--version"], ["grok", "--help"], ["grok", "agent", "--help"]]
    marker_kind = "prompt index (Crush's count of prompts after the turn)"

    def __init__(self, cwd):
        super().__init__(cwd)
        self.model = MODELS["grok"]
        self.cmd = ["grok", "agent", "--no-leader", "-m", self.model, "--reasoning-effort", "low", "stdio"]
        self.env = {"GROK_MEMORY": "0"}

    def marker_of(self, r):
        return r["prompt_index"]

    def extra_help(self):
        log = Log(os.devnull)
        p = Rpc(log, "help", self.cmd, self.cwd, self.env, deny=acp_deny)
        try:
            init = p.request("initialize", {"protocolVersion": 1, "clientCapabilities": {
                "fs": {"readTextFile": False, "writeTextFile": False}, "terminal": False}})
            out = ["ACP initialize -> agentCapabilities (no fork listed):",
                   json.dumps((init or {}).get("agentCapabilities", {}).get("sessionCapabilities"), indent=1)]
            for method in ("session/fork", "_x.ai/session/fork"):
                try:
                    out.append(f"{method} {{}} -> " + json.dumps(p.request(method, {})))
                except RpcError as e:
                    out.append(f"{method} {{}} -> error {e}")
            return "\n".join(out)
        finally:
            p.close()

    def fork_params(self, src, index):
        return {"sourceSessionId": src, "sourceCwd": self.cwd, "newCwd": self.cwd, "targetPromptIndex": index}

    def fork_turn(self, log, label, src, marker, prompt, calibrate=False):
        r = {"t_start": log.now()}
        p = Rpc(log, label, self.cmd, self.cwd, self.env, deny=acp_deny)
        try:
            p.request("initialize", {"protocolVersion": 1, "clientCapabilities": {
                "fs": {"readTextFile": False, "writeTextFile": False}, "terminal": False}})
            if calibrate:
                # Free: forks copy messages without a model call. Shows how
                # targetPromptIndex counts (chatMessagesCopied per index).
                r["calibration"] = {}
                for i in range(0, 4):
                    try:
                        res = p.request("_x.ai/session/fork", self.fork_params(src, i))
                        r["calibration"][i] = res.get("chatMessagesCopied")
                    except RpcError as e:
                        r["calibration"][i] = f"error {e}"
            # targetPromptIndex is the 0-based index of the last prompt kept
            # (the first run, with Crush's count of 1, kept BETA too), so the
            # cut prompt's index is Crush's marker minus one.
            r["target_prompt_index"] = marker - 1
            res = p.request("_x.ai/session/fork", self.fork_params(src, marker - 1))
            r["t_fork"] = log.now()
            new = res["newSessionId"]
            r["fork_result"] = res
            r["fork_call"] = "_x.ai/session/fork " + json.dumps(self.fork_params("<source>", "<0-based prompt index>")) + \
                ", then session/load of newSessionId"
            p.request("session/load", {"sessionId": new, "cwd": self.cwd, "mcpServers": []})
            r.update(self._prompt(p, log, label, new, prompt))
        finally:
            r["t_end"] = log.now()
            p.close()
        return r


class OpenCode(Acp):
    name = "opencode"
    help_cmds = [["opencode", "--version"], ["opencode", "--help"], ["opencode", "run", "--help"],
                 ["opencode", "serve", "--help"]]
    marker_kind = "OpenCode message id (agent_message_chunk messageId)"
    set_model = True

    def __init__(self, cwd):
        super().__init__(cwd)
        self.model = MODELS["opencode"]
        self.cmd = ["opencode", "acp"]
        self.env = {}

    def marker_of(self, r):
        return r["message_ids"][-1] if r["message_ids"] else None

    def extra_help(self):
        log = Log(os.devnull)
        p = Rpc(log, "help", self.cmd, self.cwd, self.env, deny=acp_deny)
        try:
            init = p.request("initialize", {"protocolVersion": 1, "clientCapabilities": {
                "fs": {"readTextFile": False, "writeTextFile": False}, "terminal": False}})
            return "ACP initialize -> agentCapabilities.sessionCapabilities:\n" + \
                json.dumps((init or {}).get("agentCapabilities", {}).get("sessionCapabilities"), indent=1) + \
                "\nACP session/fork takes {sessionId, cwd, mcpServers} and copies the WHOLE session (no cut point);" + \
                "\nthe HTTP API (opencode serve) POST /session/{id}/fork takes {messageID} and keeps the messages before it."
        finally:
            p.close()

    def _serve(self, log, label):
        p = Proc(log, label, ["opencode", "serve", "--port", "0", "--hostname", "127.0.0.1"], self.cwd)
        deadline = time.time() + 60
        while time.time() < deadline:
            line = p.next(60)
            if line is None:
                break
            m = re.search(r"http://127\.0\.0\.1:\d+", str(line))
            if m:
                return p, m.group(0)
        p.close()
        raise RuntimeError("opencode serve printed no address")

    def _http(self, log, label, method, url, body=None):
        data = json.dumps(body).encode() if body is not None else None
        log.rec(label, "send", {"http": method, "url": url, "body": body})
        req = urllib.request.Request(url, data=data, method=method, headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=60) as resp:
            out = json.loads(resp.read().decode() or "null")
        log.rec(label, "recv", {"http": resp.status, "body": out})
        return out

    def probe_fork(self, log, src):
        """No model call: forks the step-2 session at its second user message
        through the HTTP API, and whole through ACP session/fork, and lists
        what each fork kept. Used when the chat itself could not run."""
        out = {}
        srv, base = self._serve(log, "serve")
        q = "?directory=" + urllib.parse.quote(self.cwd, safe="")
        try:
            msgs = self._http(log, "serve", "GET", f"{base}/session/{src}/message{q}")
            src_ids = [((m.get("info") or {}).get("id"), (m.get("info") or {}).get("role")) for m in msgs]
            users = [i for i, role in src_ids if role == "user"]
            out["source_messages"] = src_ids
            if len(users) >= 2:
                res = self._http(log, "serve", "POST", f"{base}/session/{src}/fork{q}", {"messageID": users[1]})
                kept = self._http(log, "serve", "GET", f"{base}/session/{res['id']}/message{q}")
                out["http_fork_at"] = users[1]
                out["http_fork_kept"] = [((m.get("info") or {}).get("id"), (m.get("info") or {}).get("role")) for m in kept]
                out["http_fork_kept_only_before_cut"] = len(kept) == src_ids.index((users[1], "user"))
        finally:
            srv.close()
        p = Rpc(log, "acp", self.cmd, self.cwd, self.env, deny=acp_deny)
        try:
            p.request("initialize", {"protocolVersion": 1, "clientCapabilities": {
                "fs": {"readTextFile": False, "writeTextFile": False}, "terminal": False}})
            res = p.request("session/fork", {"sessionId": src, "cwd": self.cwd, "mcpServers": []})
            out["acp_fork_session"] = res.get("sessionId")
        except RpcError as e:
            out["acp_fork_error"] = str(e)
        finally:
            p.close()
        return out

    def fork_turn(self, log, label, src, marker, prompt):
        """Forks through the HTTP API at the message after the cut (marker =
        the cut reply's message id), then loads the fork over ACP."""
        r = {"t_start": log.now()}
        srv, base = self._serve(log, label + "-serve")
        q = "?directory=" + urllib.parse.quote(self.cwd, safe="")
        try:
            msgs = self._http(log, label + "-serve", "GET", f"{base}/session/{src}/message{q}")
            ids = [(m.get("info") or {}).get("id") for m in msgs]
            roles = [(m.get("info") or {}).get("role") for m in msgs]
            after = None
            if marker in ids:
                after = next((ids[i] for i in range(ids.index(marker) + 1, len(ids))), None)
            r["fork_message_id"] = after
            body = {"messageID": after} if after else {}
            res = self._http(log, label + "-serve", "POST", f"{base}/session/{src}/fork{q}", body)
            r["t_fork"] = log.now()
            r["fork_call"] = "HTTP POST /session/<source>/fork?directory=<cwd> {\"messageID\": <message after the cut>} " + \
                "(opencode serve), then ACP session/load of the new id"
            r["source_messages"] = list(zip(ids, roles))
            new = res["id"]
            kept = self._http(log, label + "-serve", "GET", f"{base}/session/{new}/message{q}")
            r["fork_messages"] = [((m.get("info") or {}).get("id"), (m.get("info") or {}).get("role")) for m in kept]
        finally:
            srv.close()
        p, _ = self._open(log, label, new)
        try:
            r.update(self._prompt(p, log, label, new, prompt))
        finally:
            r["t_end"] = log.now()
            p.close()
        return r


# ---------------------------------------------------------------- AGY

class Agy:
    """AGY print mode with stream-json, as agy.go runs it. No fork (Q3)."""
    name = "agy"
    help_cmds = [["agy", "--version"], ["agy", "--help"]]
    marker_kind = "step_index of the finished agent_response step"

    def __init__(self, cwd):
        self.cwd = cwd
        self.model = MODELS["agy"]

    def turn(self, log, label, native, prompt, on_first=None):
        args = ["--print=", "--input-format", "stream-json", "--output-format", "stream-json", "--model", self.model]
        if native:
            args += ["--conversation", native]
        p = Proc(log, label, [shutil.which("agy") or "agy"] + args, self.cwd)
        r = {"native": None, "text": "", "markers": [], "t_start": log.now()}
        p.send({"event": "user", "message": {"role": "user", "content": prompt}})
        try:
            while True:
                msg = p.next()
                if msg is None:
                    break
                if not isinstance(msg, dict):
                    continue
                ev = msg.get("event")
                if ev == "init":
                    r["native"] = msg.get("conversation_id")
                elif ev == "step_update":
                    s = msg.get("step_update") or {}
                    if s.get("step_type") == "agent_response":
                        r["text"] += s.get("text_delta") or ""
                        if s.get("state") == "DONE":
                            r["markers"].append(s.get("step_index"))
                elif ev == "result":
                    res = msg.get("result") or {}
                    r["status"] = res.get("status")
                    r["text"] = r["text"] or res.get("response") or ""
                    break
        finally:
            r["t_end"] = log.now()
            p.close()
        r["marker"] = r["markers"][-1] if r["markers"] else None
        log.answer(label, r["text"])
        return r


TOOLS = {"claude": Claude, "codex": Codex, "grok": Grok, "opencode": OpenCode, "agy": Agy}


# ---------------------------------------------------------------- judges

def answers(path, proc):
    out = []
    try:
        with open(path) as f:
            for line in f:
                rec = json.loads(line)
                if rec.get("dir") == "answer" and rec.get("proc") == proc:
                    out.append(rec.get("msg") or "")
    except OSError:
        return None
    return out


def judge(d):
    """Pass or fail per step, from the saved answers only."""
    v = {}
    try:
        with open(os.path.join(d, "ids.json")) as f:
            ids = json.load(f)
    except OSError:
        ids = {}
    v["1"] = "pass" if os.path.exists(os.path.join(d, "01-help.txt")) else "missing"
    a = answers(os.path.join(d, "02-chat.jsonl"), "source")
    if a is None:
        v["2"] = "missing"
    else:
        ok = len(a) >= 2 and has(a[0], "ALPHA") and has(a[1], "BETA") and \
            bool(ids.get("alpha_marker") is not None and ids.get("beta_marker") is not None)
        v["2"] = "pass" if ok else "fail"
    if ids.get("tool") == "agy":
        return v
    a = answers(os.path.join(d, "03-fork.jsonl"), "branch")
    v["3"] = "missing" if a is None else ("pass" if a and has(a[-1], "ALPHA") and not has(a[-1], "BETA") else "fail")
    a = answers(os.path.join(d, "04-source.jsonl"), "source")
    v["4"] = "missing" if a is None else ("pass" if a and has(a[-1], "ALPHA") and has(a[-1], "BETA") else "fail")
    br = answers(os.path.join(d, "05-mid-reply.jsonl"), "branch")
    src = answers(os.path.join(d, "05-mid-reply.jsonl"), "source")
    if br is None or src is None:
        v["5"] = "missing"
    else:
        mid = ids.get("step5") or {}
        counted = bool(src) and all(re.search(r"(^|\D)%d(\D|$)" % n, src[-1]) for n in range(1, 21))
        branch_ok = bool(br) and has(br[-1], "ALPHA") and not has(br[-1], "BETA")
        overlap = mid.get("branch_forked_at") is not None and mid.get("source_ended_at") is not None and \
            mid["branch_forked_at"] < mid["source_ended_at"]
        v["5"] = "pass" if counted and branch_ok and overlap else "fail"
        v["5_detail"] = {"source_counted_to_20": counted, "branch_alpha_only": branch_ok,
                         "fork_made_while_source_streaming": overlap}
    return v


# ---------------------------------------------------------------- steps

def step_help(tool, d):
    out = []
    for cmd in tool.help_cmds:
        res = subprocess.run(WRAP + cmd, cwd=tool.cwd, env=tool_env(), capture_output=True, text=True)
        out.append("$ " + " ".join(cmd) + "\n" + res.stdout + res.stderr)
    if hasattr(tool, "extra_help"):
        try:
            out.append(tool.extra_help())
        except Exception as e:  # evidence, not a crash
            out.append(f"extra help failed: {e}")
    text = EMAIL.sub("<redacted>", "\n".join(out))
    with open(os.path.join(d, "01-help.txt"), "w") as f:
        f.write(text)
    first = out[0].splitlines()
    return first[1].strip() if len(first) > 1 else ""


def run(name):
    cwd = os.path.join(LIVE_ROOT, name)
    os.makedirs(cwd, exist_ok=True)
    with open(os.path.join(cwd, "README.txt"), "w") as f:
        f.write("Throwaway project for the Crush desktop fork live check (P4.3).\n")
    d = os.path.join(EVIDENCE, "live-" + name)
    os.makedirs(d, exist_ok=True)
    tool = TOOLS[name](cwd)
    ids = {"tool": name, "marker_kind": tool.marker_kind}

    def save():
        with open(os.path.join(d, "ids.json"), "w") as f:
            json.dump(sanitize(ids), f, indent=1)

    ids["version"] = step_help(tool, d)
    print(f"[1] {name} {ids['version']}", flush=True)

    log = Log(os.path.join(d, "02-chat.jsonl"))
    try:
        if hasattr(tool, "chat"):
            a, b = tool.chat(log)
        else:
            a = tool.turn(log, "source", None, P_ALPHA)
            b = tool.turn(log, "source", a["native"], P_BETA)
    finally:
        log.close()
    ids["model"] = tool.model
    ids["source"] = a["native"]
    ids["alpha_marker"], ids["beta_marker"] = a.get("marker"), b.get("marker")
    ids["step2"] = {"alpha": a, "beta": b, "same_session": a["native"] == b["native"]}
    save()
    print(f"[2] source {a['native']} alpha={a.get('marker')} beta={b.get('marker')}", flush=True)
    if name == "agy" or not a["native"] or a.get("marker") is None:
        if hasattr(tool, "probe_fork") and a["native"]:
            log = Log(os.path.join(d, "03-fork-no-model.jsonl"))
            try:
                ids["fork_probe_no_model"] = tool.probe_fork(log, a["native"])
            finally:
                log.close()
            print(f"[3] no-model fork probe: {ids['fork_probe_no_model']}", flush=True)
        ids["verdicts"] = judge(d)
        save()
        return ids

    log = Log(os.path.join(d, "03-fork.jsonl"))
    try:
        kw = {"calibrate": True} if name == "grok" else {}
        f3 = tool.fork_turn(log, "branch", a["native"], a["marker"], P_WHICH, **kw)
    finally:
        log.close()
    ids["fork_call"] = f3.get("fork_call")
    ids["step3"] = f3
    save()
    print(f"[3] fork {f3.get('native')}: {f3.get('text')!r}", flush=True)

    log = Log(os.path.join(d, "04-source.jsonl"))
    try:
        s4 = tool.turn(log, "source", a["native"], P_WHICH)
    finally:
        log.close()
    ids["step4"] = s4
    save()
    print(f"[4] source {s4.get('native')}: {s4.get('text')!r}", flush=True)

    log = Log(os.path.join(d, "05-mid-reply.jsonl"))
    streaming = threading.Event()
    box = {}

    def source():
        try:
            kw = {"mid_prompt": "Say the word GAMMA."} if name == "grok" else {}
            box["r"] = tool.turn(log, "source", a["native"], P_COUNT, on_first=streaming.set, **kw)
        except Exception as e:
            box["err"] = repr(e)
            streaming.set()

    th = threading.Thread(target=source)
    th.start()
    try:
        streaming.wait(WAIT)
        t_branch = log.now()
        log.rec("main", "note", "source is streaming; branching now")
        try:
            f5 = tool.fork_turn(log, "branch", a["native"], a["marker"], P_WHICH)
        except Exception as e:
            f5 = {"error": repr(e)}
        th.join(WAIT + 30)
    finally:
        log.close()
    s5 = box.get("r") or {"error": box.get("err")}
    ids["step5"] = {"source": s5, "branch": f5, "branch_started_at": t_branch,
                    "branch_forked_at": f5.get("t_fork"), "source_ended_at": s5.get("t_end"),
                    "count_prompt": P_COUNT}
    save()  # the judge reads step 5's times from ids.json
    ids["verdicts"] = judge(d)
    save()
    print(f"[5] branch: {f5.get('text')!r}; source ended {s5.get('t_end')} fork at {f5.get('t_fork')}", flush=True)
    return ids


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("tool", nargs="?", choices=sorted(TOOLS))
    ap.add_argument("--judge-only", metavar="DIR", help="judge saved evidence without running anything")
    ap.add_argument("--save", action="store_true", help="with --judge-only: store the verdicts in DIR/ids.json")
    ap.add_argument("--model", help="use this model instead of the default (fork behavior does not depend on it)")
    args = ap.parse_args()
    if args.model and args.tool:
        MODELS[args.tool] = args.model
    if args.judge_only:
        v = judge(args.judge_only)
        if args.save:
            path = os.path.join(args.judge_only, "ids.json")
            with open(path) as f:
                ids = json.load(f)
            ids["verdicts"] = v
            with open(path, "w") as f:
                json.dump(ids, f, indent=1)
    elif args.tool:
        v = run(args.tool)["verdicts"]
    else:
        ap.error("name a tool or --judge-only DIR")
    print(json.dumps(v, indent=1))
    steps = [k for k in v if k.isdigit()]
    sys.exit(0 if all(v[k] == "pass" for k in steps) else 1)


if __name__ == "__main__":
    main()
