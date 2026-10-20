package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"

	runtimehost "m31labs.dev/gosx/client/runtime/host"
)

func TestEdgeWorkerSessionCacheBoundaries(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the generated worker")
	}
	dir := t.TempDir()
	worker := edgeWorkerSource(exportManifest{Routes: []exportRoute{
		{Path: "/", File: "index.html"},
		{Path: "/news", File: "news/index.html", RevalidateSeconds: 60},
	}})
	if err := os.WriteFile(filepath.Join(dir, "worker.mjs"), []byte(worker), 0644); err != nil {
		t.Fatal(err)
	}
	script := `import assert from "node:assert/strict";
import worker from "./worker.mjs";

// Node requires duplex for streamed request bodies; the worker runtime does not.
const NativeRequest = globalThis.Request;
globalThis.Request = class extends NativeRequest {
  constructor(input, init) {
    if (init?.body instanceof ReadableStream) init = { ...init, duplex: "half" };
    super(input, init);
  }
};

let staticCalls = 0;
let originCalls = 0;
let assetHeaders = { "Content-Type": "text/html", "Vary": "Accept-Encoding" };
const env = {
  GOSX_ORIGIN: "http://origin.example",
  ASSETS: { fetch: async request => {
    staticCalls++;
    return new Response("anonymous", { headers: assetHeaders });
  } },
};
globalThis.fetch = async request => {
  originCalls++;
  return new Response("origin", { headers: { "Cache-Control": "private, no-store" } });
};

let response = await worker.fetch(new Request("https://app.example/"), env);
assert.equal(await response.text(), "anonymous");
assert.equal(staticCalls, 1);
assert.equal(originCalls, 0);
assert.equal(response.headers.get("Cache-Control"), "public, max-age=0, must-revalidate");
assert.ok(response.headers.get("Vary").includes("Cookie"));
assert.ok(response.headers.get("Vary").includes("Authorization"));
assert.ok(response.headers.get("Vary").includes("Accept-Encoding"));

response = await worker.fetch(new Request("https://app.example/news"), env);
assert.equal(response.headers.get("Cache-Control"), "public, max-age=0, stale-while-revalidate=60");

for (const headers of [{ Cookie: "gosx_session=example" }, { Authorization: "Bearer example" }]) {
  const previousStatic = staticCalls;
  response = await worker.fetch(new Request("https://app.example/", { headers }), env);
  assert.equal(await response.text(), "origin");
  assert.equal(staticCalls, previousStatic);
  assert.equal(response.headers.get("Cache-Control"), "private, no-store");
}

let previousStatic = staticCalls;
response = await worker.fetch(new Request("https://app.example/__actions/subscribe", { method: "POST", body: "email=reader" }), env);
assert.equal(await response.text(), "origin");
assert.equal(staticCalls, previousStatic);

response = await worker.fetch(new Request("https://app.example/index.html", { headers: { Cookie: "gosx_session=example" } }), env);
assert.equal(await response.text(), "origin");
assert.equal(staticCalls, previousStatic);

response = await worker.fetch(new Request("https://app.example/styles.css", { headers: { Cookie: "gosx_session=example" } }), env);
assert.equal(await response.text(), "anonymous");
assert.equal(staticCalls, previousStatic + 1);

for (const headers of [{ "Set-Cookie": "session=example" }, { "Cache-Control": "private, max-age=60" }]) {
  assetHeaders = headers;
  response = await worker.fetch(new Request("https://app.example/"), env);
  assert.equal(await response.text(), "origin");
}
`
	path := filepath.Join(dir, "check.mjs")
	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(node, path).CombinedOutput(); err != nil {
		t.Fatalf("generated edge worker: %v\n%s", err, output)
	}
}

func TestEdgeWorkerNavigationStaleHashRevalidates(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to execute the generated worker")
	}
	dir := t.TempDir()
	writeTempFile(t, dir, "worker.mjs", edgeWorkerSource(exportManifest{}))
	script := `import assert from "node:assert/strict";
import worker from "./worker.mjs";
const current = ` + strconv.Quote(runtimehost.NavigationRuntimePath) + `;
const stale = "/gosx/assets/runtime/navigation." + "0".repeat(64) + ".js";
let calls = [];
const env = { ASSETS: { fetch: async request => {
  const path = new URL(request.url).pathname;
  calls.push(path);
  if (path !== current) return new Response("missing", { status: 404 });
  const headers = { "Content-Type": "application/javascript", "Cache-Control": "public, max-age=31536000, immutable", "ETag": '"current"' };
  if (request.headers.get("If-None-Match") === '"current"') return new Response(null, { status: 304, headers });
  return new Response(request.method === "HEAD" ? null : "current navigation", { headers });
} } };
for (const options of [{}, { method: "HEAD" }, { headers: { "If-None-Match": '"current"' } }]) {
  calls = [];
  const response = await worker.fetch(new Request("https://app.example" + stale, options), env);
  assert.equal(response.status, options.headers ? 304 : 200);
  assert.equal(response.headers.get("Cache-Control"), "no-cache");
  assert.equal(response.headers.get("ETag"), '"current"');
  assert.equal(await response.text(), options.method === "HEAD" || options.headers ? "" : "current navigation");
  assert.deepEqual(calls, [stale, current]);
}
calls = [];
let response = await worker.fetch(new Request("https://app.example" + current), env);
assert.equal(response.headers.get("Cache-Control"), "public, max-age=31536000, immutable");
assert.deepEqual(calls, [current]);
calls = [];
response = await worker.fetch(new Request("https://app.example/gosx/assets/runtime/navigation.wrong.js"), env);
assert.equal(response.status, 404);
assert.deepEqual(calls, ["/gosx/assets/runtime/navigation.wrong.js"]);
`
	writeTempFile(t, dir, "check.mjs", script)
	if output, err := exec.Command(node, filepath.Join(dir, "check.mjs")).CombinedOutput(); err != nil {
		t.Fatalf("generated edge worker navigation fallback: %v\n%s", err, output)
	}
}
