#!/usr/bin/env python3
import http.server
import json


class Handler(http.server.BaseHTTPRequestHandler):
    def do_POST(self):
        length = int(self.headers.get("Content-Length", 0))
        event = json.loads(self.rfile.read(length))
        payment = event["payment"]
        print(f"\n>> {event['event']}  payment {payment['id']}  status={payment['status']}\n", flush=True)
        self.send_response(200)
        self.end_headers()

    def log_message(self, format, *args):
        pass


if __name__ == "__main__":
    http.server.HTTPServer(("0.0.0.0", 9000), Handler).serve_forever()
