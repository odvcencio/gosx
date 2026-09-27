#!/usr/bin/env python3
"""Serve the WebView2 desktop smoke page and collect browser reports."""

from __future__ import annotations

import argparse
import json
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit


PAGE = b"""<!doctype html>
<html><head><meta charset=\"utf-8\"><title>GoSX WebView2 smoke</title>
<style>body{background:#123;color:#fff;font:20px sans-serif;margin:32px}</style>
</head><body><h1>GoSX WebView2 smoke</h1><p id=\"status\">Loading probe</p>
<button id=\"enter-fullscreen\">Test HTML fullscreen</button>
<script>
const report = { title: document.title };
function snapshot() {
  const canvas = document.createElement('canvas');
  const gl = canvas.getContext('webgl2');
  report.webgl2 = !!gl;
  report.chromeWebview = !!(window.chrome && window.chrome.webview);
  report.webview = report.chromeWebview;
  report.bridgeAvailable = !!(window.gosxDesktop && window.gosxDesktop.app);
  report.devicePixelRatio = window.devicePixelRatio;
  report.innerWidth = window.innerWidth;
  report.innerHeight = window.innerHeight;
  report.fullscreen = !!document.fullscreenElement;
  return report;
}
async function sendReport() {
  try { await fetch('/report', {method:'POST', headers:{'Content-Type':'application/json'}, body:JSON.stringify(report)}); }
  catch (err) { report.reportError = String(err); }
}
function observe() {
  const fields = new URLSearchParams({
    dpr: String(window.devicePixelRatio),
    width: String(window.innerWidth),
    height: String(window.innerHeight),
    fullscreen: String(!!document.fullscreenElement)
  });
  fetch('/observe?' + fields.toString(), {cache:'no-store'}).catch(()=>{});
}
document.addEventListener('fullscreenchange', () => {
  report.fullscreen = !!document.fullscreenElement;
  if (report.fullscreen) report.fullscreenEntered = true;
  else report.fullscreenExited = true;
  sendReport();
});
document.getElementById('enter-fullscreen').addEventListener('click', async () => {
  try {
    await document.documentElement.requestFullscreen();
    report.fullscreenRequestResolved = true;
    await sendReport();
    setTimeout(() => document.exitFullscreen().catch(()=>{}), 2500);
  } catch (err) {
    report.fullscreenError = String(err);
    sendReport();
  }
});
(async function () {
  snapshot();
  try {
    report.loadCount = Number(localStorage.getItem('gosx-smoke-loads') || '0') + 1;
    localStorage.setItem('gosx-smoke-loads', String(report.loadCount));
  } catch (err) {
    report.storageError = String(err);
  }
  await sendReport();
  if (report.bridgeAvailable) {
    try {
      const info = await window.gosxDesktop.app.info();
      report.bridgeRoundTrip = !!info && info.title === 'wb-rel-gosx-smoke';
      report.bridgeTitle = info && info.title;
    } catch (err) {
      report.bridgeRoundTrip = false;
      report.bridgeError = String(err);
    }
  } else {
    report.bridgeRoundTrip = false;
  }
  snapshot();
  document.getElementById('status').textContent = 'Probe complete';
  await sendReport();
  observe();
  setInterval(observe, 250);
})();
</script></body></html>"""


class SmokeState:
    def __init__(self, output: Path):
        self.output = output
        self.lock = threading.Lock()
        self.probe_gets = 0
        self.report = None
        self.observations = []

    def snapshot(self):
        return {
            "probe_gets": self.probe_gets,
            "report": self.report,
            "observations": self.observations,
        }

    def persist(self):
        temporary = self.output.with_suffix(self.output.suffix + ".tmp")
        temporary.write_text(json.dumps(self.snapshot(), indent=2) + "\n", encoding="utf-8")
        temporary.replace(self.output)


def handler_for(state: SmokeState):
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            parsed = urlsplit(self.path)
            if parsed.path == "/health":
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b"ok")
                return
            if parsed.path == "/probe":
                with state.lock:
                    state.probe_gets += 1
                    state.persist()
                self.send_response(200)
                self.send_header("Content-Type", "text/html; charset=utf-8")
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                try:
                    self.wfile.write(PAGE)
                except (BrokenPipeError, ConnectionResetError):
                    # The origin/main baseline may terminate WebView2 while
                    # this response is being written; that is the failure
                    # the Windows harness is expected to capture.
                    pass
                print(time.strftime("%H:%M:%S"), "GET", self.path, flush=True)
                return
            if parsed.path == "/observe":
                query = parse_qs(parsed.query)
                item = {name: values[-1] for name, values in query.items()}
                item["time"] = time.time()
                with state.lock:
                    state.observations.append(item)
                    state.observations = state.observations[-300:]
                    state.persist()
                self.send_response(204)
                self.end_headers()
                return
            if parsed.path == "/results":
                with state.lock:
                    body = json.dumps(state.snapshot()).encode("utf-8")
                self.send_response(200)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)
                return
            self.send_error(404)

        def do_POST(self):
            if urlsplit(self.path).path != "/report":
                self.send_error(404)
                return
            length = int(self.headers.get("Content-Length", "0"))
            body = self.rfile.read(length)
            try:
                report = json.loads(body)
            except (UnicodeDecodeError, json.JSONDecodeError):
                self.send_error(400)
                return
            with state.lock:
                state.report = report
                state.persist()
            print(time.strftime("%H:%M:%S"), "REPORT", json.dumps(report, sort_keys=True), flush=True)
            self.send_response(204)
            self.end_headers()

        def log_message(self, *_args):
            return

    return Handler


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--port", type=int, default=8175)
    parser.add_argument("--output", required=True, type=Path)
    args = parser.parse_args()
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text("{}\n", encoding="utf-8")
    state = SmokeState(args.output)
    server = ThreadingHTTPServer(("0.0.0.0", args.port), handler_for(state))
    print(f"serving WebView2 smoke page on 0.0.0.0:{args.port}", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
