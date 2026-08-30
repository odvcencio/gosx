#!/usr/bin/env python3
import json
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, HTTPServer


def listen_addr():
    raw = os.environ.get("PORT", "")
    if not raw:
        raise SystemExit("PORT is required")
    if ":" in raw:
        host, port = raw.rsplit(":", 1)
    else:
        host, port = "127.0.0.1", raw
    if host != "127.0.0.1":
        raise SystemExit(f"expected loopback bind, got {host}")
    return host, int(port)


MODE = os.environ.get("GOSX_FAKE_IDENTITY_APP_MODE", "ok")
FRAMEWORK_VERSION = os.environ.get("GOSX_DOCS_REVISION_FRAMEWORK_VERSION", "v0.53.9")
REVISION = os.environ.get("GOSX_DOCS_REVISION", "1111111111111111111111111111111111111111")
BUILT_AT = os.environ.get("GOSX_DOCS_BUILT_AT", "2026-08-30T00:00:00Z")
PUBLIC_URL = os.environ.get("PUBLIC_URL", "https://gosx.m31labs.dev").rstrip("/")

pid_file = os.environ.get("GOSX_FAKE_IDENTITY_APP_PID_FILE")
if pid_file:
    with open(pid_file, "w", encoding="utf-8") as handle:
        handle.write(str(os.getpid()))

secret_log = os.environ.get("GOSX_FAKE_IDENTITY_APP_SECRET_LOG")
if secret_log:
    with open(secret_log, "a", encoding="utf-8") as handle:
        handle.write(os.environ.get("SESSION_SECRET", "") + "\n")


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def write_json(self, payload):
        body = json.dumps(payload).encode("utf-8")
        self.send_response(200)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
        self.wfile.flush()

    def do_GET(self):
        if self.path == "/api/site":
            revision = REVISION
            if MODE in ("bad-identity", "wrong-responder"):
                revision = "2222222222222222222222222222222222222222"
            self.write_json({
                "site": "gosx-docs",
                "status": "ok",
                "apiVersion": "1",
                "framework": "GoSX",
                "frameworkVersion": FRAMEWORK_VERSION,
                "revision": revision,
                "builtAt": BUILT_AT,
                "publicURL": PUBLIC_URL,
            })
            if MODE == "exit-after-site":
                threading.Thread(target=lambda: (time.sleep(0.1), os._exit(0)), daemon=True).start()
            return
        if self.path == "/healthz":
            self.write_json({"ok": True})
            return
        if self.path == "/readyz":
            self.write_json({"ok": True})
            return
        self.send_response(404)
        self.end_headers()


if __name__ == "__main__":
    try:
        server = HTTPServer(listen_addr(), Handler)
    except OSError as exc:
        print(f"fake identity app bind failed: {exc}", file=sys.stderr)
        raise SystemExit(1)
    server.serve_forever()
