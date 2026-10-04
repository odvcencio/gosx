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

// Record the Workers-specific option at the runtime boundary. Node ignores it.
const NativeResponse = Response;
globalThis.Response = class extends NativeResponse {
  constructor(body, options = {}) {
    super(body, options);
    this.encodeBody = options.encodeBody;
  }
};

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
]);
// A valid WASM module with a custom section exceeds the sidecar size threshold.
const wasm = Buffer.concat([
  Buffer.from([0, 97, 115, 109, 1, 0, 0, 0, 0, 0x81, 0x10, 0]),
  Buffer.alloc(2048),
]);
assert.equal(WebAssembly.validate(wasm), true);
const binaryAssets = new Map([
  ["/gosx/runtime.wasm", [wasm, "application/wasm"]],
  ["/data.bin", [Buffer.alloc(4096, 0x80), "application/octet-stream"]],
]);
for (const [path, [body, type]] of binaryAssets) {
  files.set(path, [body, type]);
  files.set(path + ".br", [brotliCompressSync(body), "application/octet-stream"]);
  files.set(path + ".gz", [gzipSync(body), "application/gzip"]);
}
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
    "Cache-Control": "public, max-age=60",
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
  ["*", "br"], ["*;q=1, br;q=0", "gzip"],
  ["br;q=invalid, gzip", "gzip"],
]) {
  const response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": accept } }), env);
  assert.equal(response.headers.get("Content-Encoding"), want, accept);
  assert.equal(response.encodeBody, "manual", "shared pages preserve precompressed bytes");
  assert.equal(response.headers.get("Vary"), "Origin, Accept-Encoding, Cookie, Authorization");
  assert.equal(response.headers.get("Cache-Control"), "public, max-age=0, must-revalidate");
  assert.match(response.headers.get("Content-Type"), /^text\/html/);
  assert.equal(response.headers.get("ETag"), want ? 'W/"identity-body"' : '"identity-body"');
  assert.deepEqual(Buffer.from(await response.arrayBuffer()), want === "br" ? br : want === "gzip" ? gzip : raw);
}

for (const [path, accept, status, encoding] of [
 ["/small.txt", "br, identity;q=0", 200, "br"],
 ["/small.txt", "*;q=0", 406, null],
 ["/small.txt", "*;q=0, identity;q=1", 200, null],
 ["/index.html", "*;q=0", 406, null],
]) {
 const r = await worker.fetch(new Request("https://example.test" + path, { headers: {"Accept-Encoding":accept} }), env);
 assert.equal(r.status, status, path + " " + accept);
 assert.equal(r.headers.get("Content-Encoding"), encoding);
 await r.arrayBuffer();
}
const denied = await worker.fetch(new Request("https://example.test/absent.css", {headers:{"Accept-Encoding":"br, identity;q=0"}}), {
 ASSETS:{fetch: async () => new Response("unencoded", {headers:{"Content-Type":"text/css"}})}
});
assert.equal(denied.status, 406, "missing accepted sidecars cannot fall back to rejected identity");
await denied.arrayBuffer();

files.delete("/index.html.br");
let response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br, gzip" } }), env);
assert.equal(response.headers.get("Content-Encoding"), "gzip");
await response.arrayBuffer();
files.delete("/index.html.gz");
response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br, gzip" } }), env);
assert.equal(response.headers.get("Content-Encoding"), null);
assert.deepEqual(Buffer.from(await response.arrayBuffer()), raw);
files.set("/index.html.br", [br, "application/octet-stream"]);

// A missing sidecar may return the SPA HTML fallback with a successful status.
const spaEnv = { ASSETS: { async fetch(request) {
  const path = new URL(request.url).pathname;
  if (path.endsWith(".br") || path.endsWith(".gz")) {
    paths.push(path);
    return new Response(raw, { headers: {
      "Content-Type": "text/html; charset=utf-8",
      "Content-Length": String(raw.length),
    }});
  }
  return env.ASSETS.fetch(request);
}}};
paths = [];
response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br, gzip" } }), spaEnv);
assert.equal(response.headers.get("Content-Encoding"), null, "HTML fallback must not be labeled as compressed");
assert.deepEqual(Buffer.from(await response.arrayBuffer()), raw);
assert.deepEqual(paths, ["/index.html", "/index.html.br", "/index.html.gz"]);

// A rejected Brotli fallback must still allow a verified gzip sidecar.
const mixedEnv = { ASSETS: { async fetch(request) {
  if (new URL(request.url).pathname.endsWith(".br")) return spaEnv.ASSETS.fetch(request);
  return env.ASSETS.fetch(request);
}}};
files.set("/index.html.gz", [gzip, "application/gzip"]);
response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br, gzip" } }), mixedEnv);
assert.equal(response.headers.get("Content-Encoding"), "gzip");
assert.deepEqual(Buffer.from(await response.arrayBuffer()), gzip);

// An absent media type or a conflicting encoding does not verify a sidecar.
for (const [type, encoding] of [["", undefined], ["application/octet-stream", "gzip"]]) {
  files.set("/index.html.br", [br, type, encoding]);
  response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br" } }), env);
  assert.equal(response.headers.get("Content-Encoding"), null);
  assert.deepEqual(Buffer.from(await response.arrayBuffer()), raw);
}
// Explicit encoding metadata supports sidecars served with the original media type.
files.set("/index.html.br", [br, "text/html", "br"]);
response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br" } }), env);
assert.equal(response.headers.get("Content-Encoding"), "br");
assert.deepEqual(Buffer.from(await response.arrayBuffer()), br);
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
  assert.equal(response.headers.get("Vary"), "Origin, Accept-Encoding, Cookie, Authorization");
  assert.deepEqual(Buffer.from(await response.arrayBuffer()), body);
  assert.deepEqual(paths, ["/index.html"]);
}

for (const path of ["/small.txt"]) {
  paths = [];
  response = await worker.fetch(new Request("https://example.test" + path, { headers: { "Accept-Encoding": "br" } }), env);
  assert.equal(response.headers.get("Content-Encoding"), null);
  await response.arrayBuffer();
  assert.deepEqual(paths, [path]);
}
// Sidecar metadata verifies binary assets without sniffing compressed bytes or
// requiring the original asset to have a text media type.
for (const [path, [body, type]] of binaryAssets) {
  for (const [accept, want] of [["br, gzip", "br"], ["gzip", "gzip"], ["br;q=0, gzip", "gzip"], ["identity", null], ["br;q=0, gzip;q=0", null]]) {
    paths = [];
    response = await worker.fetch(new Request("https://example.test" + path, { headers: { "Accept-Encoding": accept } }), env);
    const expected = want ? files.get(path + (want === "br" ? ".br" : ".gz"))[0] : body;
    assert.equal(response.status, 200);
    assert.equal(response.headers.get("Content-Encoding"), want, path + " " + accept);
    assert.equal(response.headers.get("Content-Type"), type);
    assert.equal(response.headers.get("Content-Length"), String(expected.length));
    assert.equal(response.headers.get("Vary"), "Origin, Accept-Encoding");
    assert.equal(response.encodeBody, "manual");
    assert.deepEqual(Buffer.from(await response.arrayBuffer()), expected);
    assert.deepEqual(paths, want ? [path, path + (want === "br" ? ".br" : ".gz")] : [path]);
  }
  // Successful HTML fallback responses are never treated as binary sidecars.
  paths = [];
  response = await worker.fetch(new Request("https://example.test" + path, { headers: { "Accept-Encoding": "br, gzip" } }), spaEnv);
  assert.equal(response.headers.get("Content-Encoding"), null);
  assert.equal(response.headers.get("Content-Type"), type);
  assert.deepEqual(Buffer.from(await response.arrayBuffer()), body);
  assert.deepEqual(paths, [path, path + ".br", path + ".gz"]);
  // Reject the Brotli HTML fallback, then serve the verified gzip sidecar.
  paths = [];
  response = await worker.fetch(new Request("https://example.test" + path, { headers: { "Accept-Encoding": "br, gzip" } }), mixedEnv);
  assert.equal(response.headers.get("Content-Encoding"), "gzip");
  assert.equal(response.headers.get("Content-Type"), type);
  assert.deepEqual(Buffer.from(await response.arrayBuffer()), files.get(path + ".gz")[0]);
  assert.deepEqual(paths, [path, path + ".br", path + ".gz"]);
}

response = await worker.fetch(new Request("https://example.test/styles.css", { headers: { "Accept-Encoding": "br" } }), env);
assert.equal(response.headers.get("Content-Encoding"), "br");
assert.equal(response.headers.get("Content-Type"), "text/css");
assert.deepEqual(Buffer.from(await response.arrayBuffer()), br);

// Private mapped pages must reach the origin without becoming shared responses.
// Private static assets may still negotiate compression; compression is not caching.
const privateEnv = { ASSETS: { async fetch(request) {
  const response = await env.ASSETS.fetch(request);
  response.headers.set("Cache-Control", "private, no-store");
  return response;
}}};
response = await worker.fetch(new Request("https://example.test/", { headers: { "Accept-Encoding": "br" } }), privateEnv);
assert.equal(response.status, 502, "a private mapped page requires the origin fallback");
response = await worker.fetch(new Request("https://example.test/styles.css", { headers: { "Accept-Encoding": "br" } }), privateEnv);
assert.equal(response.headers.get("Content-Encoding"), "br");
assert.equal(response.headers.get("Cache-Control"), "private, no-store");
assert.equal(response.headers.get("Vary"), "Origin, Accept-Encoding");
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
