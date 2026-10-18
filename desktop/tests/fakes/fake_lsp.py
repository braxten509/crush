#!/usr/bin/env python3
"""A stdio LSP server for tests (desktop-design 6.2): it answers initialize,
then publishes one error and one warning for each file it is told about
(didOpen, and the same two again on didChange and didSave).

In crush.json:
  "lsp": {"fake": {"command": "python3", "args": ["-I", "<desktop>/tests/fakes/fake_lsp.py"],
                   "filetypes": ["go"], "root_markers": ["go.mod"]}}
Every message in and out is logged to $FAKE_LSP_LOG when it is set.

It speaks LSP's base protocol: a Content-Length header, a blank line, then
one JSON-RPC message.
"""
import json, os, sys, time

LOG = os.environ.get("FAKE_LSP_LOG")
ERROR, WARNING = 1, 2


def log(direction, msg):
    if LOG:
        with open(LOG, "a") as f:
            f.write(json.dumps({"at": time.time(), direction: msg}) + "\n")


def read():
    """The next message, or None once stdin closes."""
    length = None
    while True:
        line = sys.stdin.buffer.readline()
        if not line:
            return None
        line = line.strip()
        if not line:
            if length is not None:
                break
            continue
        name, _, value = line.decode("ascii", "replace").partition(":")
        if name.strip().lower() == "content-length":
            length = int(value.strip())
    body = sys.stdin.buffer.read(length)
    if len(body) < length:
        return None
    msg = json.loads(body)
    log("in", msg)
    return msg


def send(msg):
    msg = {"jsonrpc": "2.0", **msg}
    log("out", msg)
    body = json.dumps(msg).encode()
    try:
        sys.stdout.buffer.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
        sys.stdout.buffer.flush()
    except (BrokenPipeError, ValueError):
        sys.exit(0)


def diagnostic(line, severity, code, message):
    return {"range": {"start": {"line": line, "character": 0}, "end": {"line": line, "character": 1}},
            "severity": severity, "code": code, "source": "fake-lsp", "message": message}


def publish(uri, version=None):
    params = {"uri": uri, "diagnostics": [
        diagnostic(0, ERROR, "F001", "fake error: something is wrong here"),
        diagnostic(1, WARNING, "F002", "fake warning: this could be better"),
    ]}
    if version is not None:
        params["version"] = version
    send({"method": "textDocument/publishDiagnostics", "params": params})


def main():
    while True:
        msg = read()
        if msg is None:
            return 0
        method, params = msg.get("method"), msg.get("params") or {}
        if method is None:
            continue  # a response to nothing we asked
        if "id" in msg:
            if method == "initialize":
                result = {"capabilities": {"textDocumentSync": {"openClose": True, "change": 1, "save": {"includeText": False}}},
                          "serverInfo": {"name": "fake-lsp", "version": "0.0.0"}}
            else:
                result = None  # shutdown, and anything else asked of it
            send({"id": msg["id"], "result": result})
            continue
        doc = params.get("textDocument") or {}
        if method in ("textDocument/didOpen", "textDocument/didChange", "textDocument/didSave") and doc.get("uri"):
            publish(doc["uri"], doc.get("version"))
        elif method == "exit":
            return 0


if __name__ == "__main__":
    sys.exit(main())
