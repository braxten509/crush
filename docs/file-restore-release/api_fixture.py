#!/usr/bin/env python3
"""Offline tool fixture. FILE_RESTORE_FIXTURE_FILE must name a disposable file."""
import json
import os
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
        messages = body.get('messages', [])
        users = [m.get('content') for m in messages if m.get('role') == 'user']
        # Titles and summary requests never edit files.
        text = 'FILE_RESTORE_FIXTURE_REPLY'
        delta = {'role': 'assistant', 'content': text}
        finish = 'stop'
        tool_ids = [m.get('tool_call_id') for m in messages if m.get('role') == 'tool']
        tool = None
        if body.get('tools'):
            if 'restore-fixture-view' not in tool_ids:
                tool = 'view'
            elif tool_ids[-1] == 'restore-fixture-view':
                tool = 'write'
        if tool:
            arguments = {'file_path': os.environ['FILE_RESTORE_FIXTURE_FILE']}
            if tool == 'write':
                arguments['content'] = 'fixture after\n'
            delta = {'role': 'assistant', 'tool_calls': [{'index': 0, 'id': 'restore-fixture-' + tool, 'type': 'function', 'function': {'name': tool, 'arguments': json.dumps(arguments)}}]}
            finish = 'tool_calls'
        self.send_response(200)
        if body.get('stream'):
            self.send_header('Content-Type', 'text/event-stream')
            self.end_headers()
            for content, reason in [(delta, None), ({}, finish)]:
                chunk = {'id': 'restore-test', 'object': 'chat.completion.chunk', 'model': 'tree-fixture', 'choices': [{'index': 0, 'delta': content, 'finish_reason': reason}]}
                self.wfile.write(('data: ' + json.dumps(chunk) + '\n\n').encode())
            self.wfile.write(b'data: [DONE]\n\n')
        else:
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({'id': 'restore-title', 'object': 'chat.completion', 'model': 'tree-fixture', 'choices': [{'index': 0, 'message': {'role': 'assistant', 'content': text}, 'finish_reason': 'stop'}]}).encode())

ThreadingHTTPServer(('127.0.0.1', 18589), Handler).serve_forever()
