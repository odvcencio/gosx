package main

import (
	"encoding/json"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func TestCompressionEdgeWorker(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node is required to execute the generated edge worker")
	}
	workerPath := filepath.Join(t.TempDir(), "worker.mjs")
	manifest := exportManifest{Routes: []exportRoute{{Path: "/", File: "index.html"}}}
	if err := os.WriteFile(workerPath, []byte(edgeWorkerSource(manifest)), 0644); err != nil {
		t.Fatal(err)
	}
	workerURL := (&url.URL{Scheme: "file", Path: filepath.ToSlash(workerPath)}).String()
	script := "import worker from " + strconv.Quote(workerURL) + ";\n" + `
import assert from "node:assert/strict";
import { brotliCompressSync, gzipSync } from "node:zlib";

const raw = Buffer.from("<p>static HTML page</p>".repeat(128));
const br = brotliCompressSync(raw);
const gzip = gzipSync(raw);
const files = new Map([
  ["/index.html", [raw, "text/html; charset=utf-8"]],
  ["/index.html.br", [br, "application/octet-stream", "br"]],
  ["/index.html.gz", [gzip, "application/octet-stream"]],
  ["/styles.css", [raw, "text/css"]],
  ["/styles.css.br", [br, "application/octet-stream"]],
  ["/small.txt", [Buffer.from("small"), "text/plain"]],
  ["/small.txt.br", [br, "application/octet-stream"]],
  ["/app.wasm", [raw, "application/wasm"]],
  ["/app.wasm.br", [br, "application/octet-stream"]],
]);
let paths = [];
const env = { ASSETS: { async fetch(request) {
  assert.equal(request.headers.get("Accept-Encoding"), "identity");
  const path = new URL(request.url).pathname;
  paths.push(path);
  const file = files.get(path);
  if (!file) return new Response("missing", { status: 404 });
  const [body, type, encoding] = file;
  const headers = new Headers({
    "Content-Type": type,
    "Content-Length": String(body.length),
    "Cache-Control": "private, no-store",
    "Vary": "Origin",
    "ETag": '"identity-body"',
  });
  if (encoding) headers.set("Content-Encoding", encoding);
  if ((request.headers.get("If-None-Match") || "").replace(/^W\//, "") === '"identity-body"') {
    headers.delete("Content-Length");
    headers.delete("Content-Type");
    return new Response(null, { status: 304, headers });
  }
  if (request.headers.has("Range")) {
    headers.set("Content-Length", "10");
    headers.set("Content-Range", "bytes 0-9/" + body.length);
    return new Response(body.subarray(0, 10), { status: 206, headers });
  }
  return new Response(request.method === "HEAD" ? null : body, { headers });
}}};

for (const [accept, want] of [
  ["br, gzip", "br"], ["gzip", "gzip"], ["br;q=0, gzip", "gzip"],
  ["br;q=0, gzip;q=0", null], ["identity", null], ["", null],
  ["*", "br"], ["*;q=1, br;q=0", "gzip"], ["*;q=0", null],
  ["br;q=invalid, gzip", "gzip"],
]) {
  const response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": accept } }), env);
  assert.equal(response.headers.get("Content-Encoding"), want, accept);
  assert.equal(response.headers.get("Vary"), "Origin, Accept-Encoding");
  assert.equal(response.headers.get("Cache-Control"), "private, no-store");
  assert.match(response.headers.get("Content-Type"), /^text\/html/);
  assert.equal(response.headers.get("ETag"), want ? 'W/"identity-body"' : '"identity-body"');
  assert.deepEqual(Buffer.from(await response.arrayBuffer()), want === "br" ? br : want === "gzip" ? gzip : raw);
}

files.delete("/index.html.br");
let response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br, gzip" } }), env);
assert.equal(response.headers.get("Content-Encoding"), "gzip");
await response.arrayBuffer();
files.delete("/index.html.gz");
response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br, gzip" } }), env);
assert.equal(response.headers.get("Content-Encoding"), null);
assert.deepEqual(Buffer.from(await response.arrayBuffer()), raw);
files.set("/index.html.br", [br, "application/octet-stream"]);

for (const [method, extra, status, body] of [
  ["HEAD", {}, 200, Buffer.alloc(0)],
  ["GET", { Range: "bytes=0-9" }, 206, raw.subarray(0, 10)],
  ["GET", { Upgrade: "websocket" }, 200, raw],
  ["GET", { "If-None-Match": 'W/"identity-body"' }, 304, Buffer.alloc(0)],
]) {
  paths = [];
  response = await worker.fetch(new Request("https://example.test/", { method, headers: { "Accept-Encoding": "br, gzip", ...extra } }), env);
  assert.equal(response.status, status);
  assert.equal(response.headers.get("Content-Encoding"), null);
  assert.equal(response.headers.get("Vary"), "Origin, Accept-Encoding");
  assert.deepEqual(Buffer.from(await response.arrayBuffer()), body);
  assert.deepEqual(paths, ["/index.html"]);
}

for (const path of ["/small.txt", "/app.wasm"]) {
  paths = [];
  response = await worker.fetch(new Request("https://example.test" + path, { headers: { "Accept-Encoding": "br" } }), env);
  assert.equal(response.headers.get("Content-Encoding"), null);
  await response.arrayBuffer();
  assert.deepEqual(paths, [path]);
}
response = await worker.fetch(new Request("https://example.test/styles.css", { headers: { "Accept-Encoding": "br" } }), env);
assert.equal(response.headers.get("Content-Encoding"), "br");
assert.equal(response.headers.get("Content-Type"), "text/css");
assert.deepEqual(Buffer.from(await response.arrayBuffer()), br);
`
	cmd := exec.Command(node, "--input-type=module")
	cmd.Stdin = strings.NewReader(script)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated worker failed: %v\n%s", err, output)
	}
}

func TestCompressionPlatformMetadata(t *testing.T) {
	root := t.TempDir()
	if err := writeEdgeBundle(root, exportManifest{}, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(root, "platform", "deployment.json"))
	if err != nil {
		t.Fatal(err)
	}
	var descriptor deploymentDescriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		t.Fatal(err)
	}
	compression := descriptor.Compression
	if strings.Join(compression.Encodings, ",") != "br,gzip" || compression.Suffixes["br"] != ".br" || compression.Suffixes["gzip"] != ".gz" || compression.Vary != "Accept-Encoding" || compression.MinimumBytes != 1024 {
		t.Fatalf("incomplete deployment compression metadata: %+v", compression)
	}
	var config struct {
		Headers []struct {
			Source  string
			Headers []struct{ Key, Value string }
		}
	}
	if err := json.Unmarshal([]byte(vercelConfigSource()), &config); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, rule := range config.Headers {
		for _, header := range rule.Headers {
			got[rule.Source+":"+header.Key] = header.Value
		}
	}
	for key, value := range map[string]string{
		"/(.*):Vary":                     "Accept-Encoding",
		"/(.*).html.br:Content-Encoding": "br",
		"/(.*).html.gz:Content-Encoding": "gzip",
		"/(.*).wasm.br:Content-Encoding": "br",
		"/(.*).html.br:Content-Type":     "text/html; charset=utf-8",
		"/(.*).html.gz:Content-Type":     "text/html; charset=utf-8",
		"/gosx/(.*):Cache-Control":       "public, max-age=31536000, immutable",
		"/assets/(.*):Cache-Control":     "public, max-age=31536000, immutable",
	} {
		if got[key] != value {
			t.Errorf("%s = %q, want %q", key, got[key], value)
		}
	}
	// A standalone archive must not be served with Content-Encoding: no rule
	// may match it and set the header.
	for _, rule := range config.Headers {
		pattern := "^" + strings.ReplaceAll(strings.ReplaceAll(rule.Source, ".", `\.`), `(\.*)`, "(.*)") + "$"
		if !regexp.MustCompile(pattern).MatchString("/downloads/app.tar.gz") {
			continue
		}
		for _, header := range rule.Headers {
			if header.Key == "Content-Encoding" {
				t.Errorf("rule %q sets Content-Encoding on a standalone .tar.gz download", rule.Source)
			}
		}
	}
}
