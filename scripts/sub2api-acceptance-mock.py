"""Bounded local HTTP fixtures for Sub2API acceptance; never contacts a provider."""
import json
import os
from pathlib import Path
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

release = threading.Event()
lock = threading.Lock()
attempts = {}
log = Path('/runtime/mock-requests.jsonl')


class Handler(BaseHTTPRequestHandler):
    protocol_version = 'HTTP/1.1'

    def log_message(self, *_):
        pass

    def send_json(self, code, payload):
        body = json.dumps(payload).encode()
        self.send_response(code)
        self.send_header('Content-Type', 'application/json')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == '/control/release':
            release.set()
            self.send_json(200, {'released': True})
            return
        if self.path == '/control/reset':
            release.clear()
            self.send_json(200, {'reset': True})
            return
        self.send_json(200, {'object': 'list', 'data': [{'id': m, 'object': 'model'} for m in
                       ['mock-chat', 'mock-response', 'mock-message', 'mock-reject', 'mock-incomplete', 'mock-cancel', 'mock-drop', 'mock-badjson', 'ordinary-model']]})

    def do_POST(self):
        body = self.rfile.read(int(self.headers.get('Content-Length', 0)))
        data = json.loads(body)
        model = data.get('model', '')
        auth_ok = (self.headers.get('Authorization') == 'Bearer ' + os.environ['MOCK_KEY'] or
                   self.headers.get('X-Api-Key') == os.environ['MOCK_KEY'])
        with lock:
            attempts[model] = attempts.get(model, 0) + 1
            attempt = attempts[model]
            with log.open('a', encoding='utf-8') as f:
                f.write(json.dumps({'path': self.path, 'model': model, 'stream': data.get('stream', False),
                                    'auth_matches_mock_account': auth_ok, 'prompt_cache_key': data.get('prompt_cache_key')}) + '\n')
        if not auth_ok:
            self.send_json(401, {'error': {'type': 'authentication_error', 'message': 'mock credential mismatch'}})
            return
        if model == 'mock-retry' and attempt == 1:
            self.send_json(503, {'error': {'type': 'server_error', 'message': 'mock rejected before execution; retry once'}})
            return
        if model == 'mock-reject':
            self.send_json(400, {'error': {'type': 'invalid_request_error', 'message': 'bounded mock rejection'}})
            return
        if model == 'mock-drop':
            self.close_connection = True
            return
        if model == 'mock-badjson':
            self.send_response(200)
            self.send_header('Content-Length', '8')
            self.end_headers()
            self.wfile.write(b'not-json')
            return
        path = urlsplit(self.path).path
        is_messages = path.endswith('/messages')
        is_responses = path.endswith('/responses')
        usage = {'input_tokens': 1000, 'output_tokens': 20, 'total_tokens': 1020,
                 'input_tokens_details': {'cached_tokens': 200}}
        chat_usage = {'prompt_tokens': 1000, 'completion_tokens': 20, 'total_tokens': 1020,
                      'prompt_tokens_details': {'cached_tokens': 200}}
        response = {'id': 'resp_mock', 'object': 'response', 'model': model, 'status': 'completed',
                    'output': [{'id': 'msg_mock', 'type': 'message', 'role': 'assistant', 'status': 'completed',
                                'content': [{'type': 'output_text', 'text': 'mock ok', 'annotations': []}]}], 'usage': usage}
        if not data.get('stream'):
            if is_messages:
                self.send_json(200, {'id': 'msg_mock', 'type': 'message', 'role': 'assistant', 'model': model,
                    'content': [{'type': 'text', 'text': 'mock ok'}], 'stop_reason': 'end_turn',
                    'usage': {'input_tokens': 800, 'output_tokens': 20, 'cache_read_input_tokens': 200}})
            elif is_responses:
                self.send_json(200, response)
            else:
                self.send_json(200, {'id': 'chatcmpl_mock', 'object': 'chat.completion', 'model': model,
                    'choices': [{'index': 0, 'message': {'role': 'assistant', 'content': 'mock ok'}, 'finish_reason': 'stop'}],
                    'usage': chat_usage})
            return
        self.send_response(200)
        self.send_header('Content-Type', 'text/event-stream')
        self.send_header('Connection', 'close')
        self.end_headers()
        self.close_connection = True
        def emit(payload):
            self.wfile.write(('data: ' + json.dumps(payload) + '\n\n').encode())
            self.wfile.flush()
        try:
            if is_responses:
                emit({'type': 'response.created', 'response': {'id': 'resp_mock', 'model': model, 'status': 'in_progress', 'output': []}})
                emit({'type': 'response.output_text.delta', 'item_id': 'msg_mock', 'output_index': 0, 'content_index': 0, 'delta': 'mock '})
                if model == 'mock-cancel':
                    release.wait(timeout=10)
                if model != 'mock-incomplete':
                    emit({'type': 'response.completed', 'response': response})
            else:
                emit({'id': 'chatcmpl_mock', 'model': model, 'choices': [{'index': 0, 'delta': {'content': 'mock ok'}, 'finish_reason': None}]})
                emit({'id': 'chatcmpl_mock', 'model': model, 'choices': [{'index': 0, 'delta': {}, 'finish_reason': 'stop'}], 'usage': chat_usage})
                self.wfile.write(b'data: [DONE]\n\n')
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            pass  # Expected only when the exercised service cancels its transport.


ThreadingHTTPServer(('0.0.0.0', 8090), Handler).serve_forever()
