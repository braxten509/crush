"""A fake OpenAI-style AI server for tests. Gives scripted replies, streams
them like the real thing, and logs every request. Standard library only.

Usage: fake_ai_server.py --port-file FILE [--script FILE]
The script is JSON: {"replies": [{"match": "substring", "text": "reply"}, ...],
"default": "reply"}. The first reply whose match is in the last user message wins.
"""
import argparse, json, os, sys, time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODELS = ["fake-small", "fake-large"]


def load_script(path):
    if path and os.path.exists(path):
        return json.load(open(path))
    return {"replies": [], "default": "Fake reply."}


class Handler(BaseHTTPRequestHandler):
    script = {}
    log_path = None

    def log_message(self, *args):  # keep test output quiet
        pass

    def _log(self, entry):
        if self.log_path:
            with open(self.log_path, "a") as f:
                f.write(json.dumps(entry) + "\n")

    def _send_json(self, code, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        self._log({"method": "GET", "path": self.path})
        if self.path.rstrip("/").endswith("/models"):
            self._send_json(200, {"object": "list", "data": [{"id": m, "object": "model"} for m in MODELS]})
        else:
            self._send_json(404, {"error": {"message": "not found"}})

    def do_POST(self):
        length = int(self.headers.get("Content-Length") or 0)
        body = json.loads(self.rfile.read(length) or b"{}")
        self._log({"method": "POST", "path": self.path, "model": body.get("model"), "stream": body.get("stream", False),
                   "messages": len(body.get("messages", []))})
        if not self.path.rstrip("/").endswith("/chat/completions"):
            return self._send_json(404, {"error": {"message": "not found"}})
        text = self.reply_for(body)
        if body.get("stream"):
            self.send_response(200)
            self.send_header("Content-Type", "text/event-stream")
            self.end_headers()
            for i, word in enumerate(text.split(" ")):
                chunk = {"id": "fake", "object": "chat.completion.chunk", "model": body.get("model"),
                         "choices": [{"index": 0, "delta": {"content": (" " if i else "") + word}, "finish_reason": None}]}
                self.wfile.write(f"data: {json.dumps(chunk)}\n\n".encode())
                self.wfile.flush()
                time.sleep(0.01)
            done = {"id": "fake", "object": "chat.completion.chunk", "model": body.get("model"),
                    "choices": [{"index": 0, "delta": {}, "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 10, "completion_tokens": len(text.split()), "total_tokens": 10 + len(text.split())}}
            self.wfile.write(f"data: {json.dumps(done)}\n\ndata: [DONE]\n\n".encode())
            self.wfile.flush()
        else:
            self._send_json(200, {"id": "fake", "object": "chat.completion", "model": body.get("model"),
                                  "choices": [{"index": 0, "message": {"role": "assistant", "content": text}, "finish_reason": "stop"}],
                                  "usage": {"prompt_tokens": 10, "completion_tokens": len(text.split()), "total_tokens": 10 + len(text.split())}})

    def reply_for(self, body):
        last = ""
        for m in body.get("messages", []):
            if m.get("role") == "user":
                c = m.get("content")
                last = c if isinstance(c, str) else " ".join(p.get("text", "") for p in c or [] if isinstance(p, dict))
        for r in self.script.get("replies", []):
            if r.get("match", "") in last:
                return r["text"]
        return self.script.get("default", "Fake reply.")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--port-file", required=True)
    ap.add_argument("--script")
    a = ap.parse_args()
    Handler.script = load_script(a.script)
    Handler.log_path = os.environ.get("FAKE_AI_LOG")
    srv = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    with open(a.port_file + ".tmp", "w") as f:
        f.write(str(srv.server_address[1]))
    os.replace(a.port_file + ".tmp", a.port_file)
    srv.serve_forever()


if __name__ == "__main__":
    sys.exit(main())
