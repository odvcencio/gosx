/**
 * Browser-free rendered-output gate for the Scene3D water demo.
 *
 * This boots the real GoSX docs application and certifies the fully composed
 * route (root layout + demos layout + page + runtime injection). Demos may use
 * runtime-owned scripts, but must never smuggle in page or escape-hatch JS.
 */

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("rendered water route executes only provenance-labeled GoSX scripts", { timeout: 60000 }, async (t) => {
  const port = await freePort();
  const baseURL = `http://127.0.0.1:${port}`;
  let logs = "";
  const child = spawn(process.env.GO_BINARY || "go", ["run", "./cmd/gosx", "dev", "./examples/gosx-docs"], {
    cwd: repoRoot,
    detached: true,
    env: { ...process.env, PORT: String(port), SESSION_SECRET: "gosx-http-certification" },
    stdio: ["ignore", "pipe", "pipe"],
  });
  child.stdout.setEncoding("utf8");
  child.stderr.setEncoding("utf8");
  child.stdout.on("data", (chunk) => { logs += chunk; });
  child.stderr.on("data", (chunk) => { logs += chunk; });
  t.after(() => killProcessGroup(child.pid));

  await waitForHealthy(`${baseURL}/readyz`, 45000, () => logs);
  const response = await fetch(`${baseURL}/demos/water`);
  assert.equal(response.status, 200, logs);
  const html = await response.text();
  const scripts = Array.from(html.matchAll(/<script\b([^>]*)>/gi), (match) => match[1]);
  assert.ok(scripts.length >= 4, "expected navigation, manifest, and runtime scripts");

  const unowned = scripts.filter((attrs) => !(
    /\bdata-gosx-script\s*=/.test(attrs) ||
    /\bdata-gosx-navigation\s*=/.test(attrs) ||
    /\bid\s*=\s*["']gosx-manifest["']/.test(attrs) ||
    /\bdata-gosx-dev-reload\s*=/.test(attrs)
  ));
  assert.deepEqual(unowned, [], `rendered water route contains scripts without GoSX provenance:\n${unowned.join("\n")}`);
  assert.doesNotMatch(html, /(?:demos-dock|reveal|water-controls)\.js(?:\?|["'])/i);
});

function freePort() {
  return new Promise((resolve, reject) => {
    const server = createServer();
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => {
      const address = server.address();
      server.close((error) => error ? reject(error) : resolve(address.port));
    });
  });
}

async function waitForHealthy(url, timeoutMS, logs) {
  const deadline = Date.now() + timeoutMS;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
    } catch {}
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error(`GoSX app did not become healthy at ${url}\n${logs()}`);
}

function killProcessGroup(pid) {
  if (!pid) return;
  try { process.kill(-pid, "SIGTERM"); } catch {
    try { process.kill(pid, "SIGTERM"); } catch {}
  }
}
