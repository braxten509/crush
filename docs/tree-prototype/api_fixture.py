#!/usr/bin/env python3
"""Local OpenAI-compatible fixture for repeatable, credential-free tree QA.
Run under crush bg; point a scratch provider at http://127.0.0.1:18479/v1.
Records model-visible user/assistant history, never credentials or system text.
"""
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        history = [m for m in body.get('messages', []) if m.get('role') in ('user', 'assistant')]
        with open(os.environ.get('TREE_FIXTURE_LOG', '/tmp/pi-tree-qa/api-history.jsonl'), 'a') as stream:
            stream.write(json.dumps(history) + '\n')
        text = 'TREE_FIXTURE_REPLY'
        self.send_response(200)
        if body.get('stream'):
            self.send_header('Content-Type', 'text/event-stream')
            self.end_headers()
            for delta, finish in [({'role': 'assistant', 'content': text}, None), ({}, 'stop')]:
                chunk = {'id': 'tree-test', 'object': 'chat.completion.chunk', 'model': 'tree-fixture', 'choices': [{'index': 0, 'delta': delta, 'finish_reason': finish}]}
                self.wfile.write(('data: ' + json.dumps(chunk) + '\n\n').encode())
            self.wfile.write(b'data: [DONE]\n\n')
        else:
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({'id': 'tree-title', 'object': 'chat.completion', 'model': 'tree-fixture', 'choices': [{'index': 0, 'message': {'role': 'assistant', 'content': text}, 'finish_reason': 'stop'}]}).encode())

ThreadingHTTPServer(('127.0.0.1', 18479), Handler).serve_forever()
