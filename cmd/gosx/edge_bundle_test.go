package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
