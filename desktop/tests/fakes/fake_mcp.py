#!/usr/bin/env python3
"""A stdio MCP server for tests (desktop-design 6.2): tool `echo`, one
resource and one channel push.

In crush.json (the server's name is up to the config; "fake" makes the tool
mcp_fake_echo and the push <channel source="fake">):
  "mcp": {"fake": {"type": "stdio", "command": "python3",
                   "args": ["-I", "<desktop>/tests/fakes/fake_mcp.py"],
                   "channel_enabled": true}}
The push only becomes a turn when the server is opted in as a channel
(channel_enabled, or `--channels fake` / `--channels server:fake`); otherwise
the engine drops it.

Options:
  --push-after S  push S seconds after the client's notifications/initialized
                  (default 3; a negative number never pushes on a timer)
  --push-on FILE  push once FILE exists instead of on the timer
  --push-text T   the push's content (default "Build finished: 12 tests passed.")
  --fail          print an error and exit 1 at once, as a broken server
Every message in and out is logged to $FAKE_MCP_LOG when it is set.

It speaks MCP's stdio transport: one JSON-RPC message per line.
"""
import argparse, json, os, sys, threading, time

TOOL = {
    "name": "echo",
    "title": "Echo",
    "description": "Returns the text it is given.",
    "inputSchema": {"type": "object", "properties": {"text": {"type": "string", "description": "The text to return."}}, "required": ["text"]},
}
RESOURCE = {
    "uri": "fake://notes/today.md",
    "name": "today.md",
    "title": "Today's notes",
    "description": "A short note for tests.",
    "mimeType": "text/markdown",
}
NOTE = "# Today\n\n- Review the session cookie change.\n- Ship the desktop build.\n"

_out = threading.Lock()
LOG = os.environ.get("FAKE_MCP_LOG")


def log(direction, msg):
    if LOG:
        with _out, open(LOG, "a") as f:
            f.write(json.dumps({"at": time.time(), direction: msg}) + "\n")


def send(msg):
    msg = {"jsonrpc": "2.0", **msg}
    log("out", msg)
    with _out:
        try:
            sys.stdout.write(json.dumps(msg) + "\n")
            sys.stdout.flush()
        except (BrokenPipeError, ValueError):
            os._exit(0)


def push(text):
    send({"method": "notifications/claude/channel", "params": {"content": text, "meta": {"kind": "build"}}})


def answer(method, params):
    """The result for a request, or a JSON-RPC error as ("error", {...})."""
    if method == "initialize":
        return {
            "protocolVersion": params.get("protocolVersion") or "2025-06-18",
            "capabilities": {"tools": {"listChanged": False}, "resources": {"listChanged": False},
                             "experimental": {"claude/channel": {}}},
            "serverInfo": {"name": "fake-mcp", "version": "0.0.0"},
            "instructions": "A fake MCP server for tests.",
        }
    if method == "ping" or method == "logging/setLevel":
        return {}
    if method == "tools/list":
        return {"tools": [TOOL]}
    if method == "tools/call":
        if params.get("name") != "echo":
            return ("error", {"code": -32602, "message": "Unknown tool: %s" % params.get("name")})
        text = (params.get("arguments") or {}).get("text")
        if not isinstance(text, str):
            return {"content": [{"type": "text", "text": "echo needs a text argument"}], "isError": True}
        return {"content": [{"type": "text", "text": text}], "isError": False}
    if method == "resources/list":
        return {"resources": [RESOURCE]}
    if method == "resources/templates/list":
        return {"resourceTemplates": []}
    if method == "resources/read":
        if params.get("uri") != RESOURCE["uri"]:
            return ("error", {"code": -32002, "message": "Resource not found", "data": {"uri": params.get("uri")}})
        return {"contents": [{"uri": RESOURCE["uri"], "mimeType": RESOURCE["mimeType"], "text": NOTE}]}
    if method == "prompts/list":
        return {"prompts": []}
    return ("error", {"code": -32601, "message": "Method not found: %s" % method})


def schedule_push(opts):
    def wait_file():
        while not os.path.exists(opts.push_on):
            time.sleep(0.1)
        push(opts.push_text)

    def wait_time():
        time.sleep(opts.push_after)
        push(opts.push_text)

    if opts.push_on:
        threading.Thread(target=wait_file, daemon=True).start()
    elif opts.push_after >= 0:
        threading.Thread(target=wait_time, daemon=True).start()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--push-after", type=float, default=3.0)
    ap.add_argument("--push-on")
    ap.add_argument("--push-text", default="Build finished: 12 tests passed.")
    ap.add_argument("--fail", action="store_true")
    opts = ap.parse_args()
    if opts.fail:
        print("fake MCP server: failed on purpose", file=sys.stderr)
        return 1
    pushed = False
    for line in sys.stdin:
        try:
            msg = json.loads(line)
        except ValueError:
            continue
        log("in", msg)
        method, params = msg.get("method"), msg.get("params") or {}
        if method is None:
            continue  # a response to nothing we asked
        if "id" not in msg:
            if method == "notifications/initialized" and not pushed:
                pushed = True
                schedule_push(opts)
            continue
        result = answer(method, params)
        if isinstance(result, tuple):
            send({"id": msg["id"], "error": result[1]})
        else:
            send({"id": msg["id"], "result": result})
    return 0


if __name__ == "__main__":
    sys.exit(main())
