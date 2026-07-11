/**
 * WebGPU honesty gate e2e test.
 *
 * Navigates to the skinned-GLB fixture page served by gosx-docs at
 * /test/webgpu-honesty-gate. That route sets preferWebGPU=true and includes a
 * model that the server-side SkinLookup marks as skinned, so the honesty gate
 * must ship backendCaps that allow WebGPU/WebGL and exclude canvas2d for
 * skinning. The JS runtime mounts WebGL when headless Chrome has no usable
 * WebGPU adapter and reports the environment fallback honestly:
 *
 *   data-gosx-scene3d-backend         = "webgl"
 *   data-gosx-scene3d-renderer-fallback = "webgpu-unavailable"
 *
 * Harness mirrors gosx_docs_e2e.test.mjs: same app server launch, same
 * before/after lifecycle, same waitForHealthy / killProcessGroup helpers.
 */

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import path from "node:path";
import test, { after, before } from "node:test";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright-core";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");
const chromePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || "/usr/bin/google-chrome";
// Use a different port than the main e2e so both suites can run concurrently.
const baseURL = process.env.GOSX_E2E_BASE_URL || "http://127.0.0.1:3071";

let appProcess;
let browser;
let context;
let page;
let logs = "";

before(async () => {
  appProcess = spawn("go", ["run", "./cmd/gosx", "dev", "./examples/gosx-docs"], {
    cwd: repoRoot,
    detached: true,
    env: {
      ...process.env,
      PORT: new URL(baseURL).port,
      SESSION_SECRET: "gosx-e2e-session-secret",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });

  appProcess.stdout.setEncoding("utf8");
  appProcess.stderr.setEncoding("utf8");
  appProcess.stdout.on("data", (chunk) => {
    logs += chunk;
  });
  appProcess.stderr.on("data", (chunk) => {
    logs += chunk;
  });

  await waitForHealthy(`${baseURL}/readyz`, 45000);

  browser = await chromium.launch({
    executablePath: chromePath,
    headless: true,
  });
  context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
  });
  page = await context.newPage();
});

after(async () => {
  await page?.close().catch(() => {});
  await context?.close().catch(() => {});
  await browser?.close().catch(() => {});

  if (!appProcess) {
    return;
  }
  killProcessGroup(appProcess.pid);
  await new Promise((resolve) => setTimeout(resolve, 250));
});

test(
  "skinned-glb with preferWebGPU falls back to webgl via honesty gate",
  { timeout: 90000 },
  async () => {
    try {
      const fixturePath = "/test/webgpu-honesty-gate";
      const res = await page.goto(`${baseURL}${fixturePath}`, {
        waitUntil: "domcontentloaded",
      });
      assert.ok(
        res.ok(),
        `fixture page returned ${res.status()}\n\nLogs:\n${logs}`,
      );

      // Prove the server-side honesty verdict itself reached the browser. The
      // renderer's fallback reason may still be environment-specific when
      // headless Chrome has no usable WebGPU adapter.
      const backendCaps = await page.evaluate(() => {
        const script = document.getElementById("gosx-manifest");
        if (!script?.textContent) return [];
        const manifest = JSON.parse(script.textContent);
        const matches = [];
        const seen = new Set();
        function visit(value) {
          if (!value || typeof value !== "object" || seen.has(value)) return;
          seen.add(value);
          if (value.backendCaps && typeof value.backendCaps === "object") {
            matches.push(value.backendCaps);
          }
          if (Array.isArray(value)) value.forEach(visit);
          else Object.values(value).forEach(visit);
        }
        visit(manifest);
        return matches;
      });
      assert.ok(
        backendCaps.some((caps) => Array.isArray(caps.capable) && caps.capable.includes("webgpu") && caps.capable.includes("webgl") &&
          Array.isArray(caps.reasons) && caps.reasons.some(
            (reason) => reason?.feature === "skinning" && reason?.excludes === "canvas2d",
          )),
        `fixture manifest omitted the current skinning capability contract: ${JSON.stringify(backendCaps)}\n\nLogs:\n${logs}`,
      );

      // Wait for the Scene3D mount to finish initialising. The runtime sets
      // data-gosx-scene3d-backend once it has selected and started the renderer.
      await page.waitForSelector(
        "[data-gosx-scene3d-mounted][data-gosx-scene3d-backend]",
        { timeout: 30000 },
      );

      const attrs = await page.evaluate(() => {
        const el = document.querySelector("[data-gosx-scene3d-mounted]");
        if (!el) return null;
        return {
          backend: el.getAttribute("data-gosx-scene3d-backend"),
          fallback: el.getAttribute("data-gosx-scene3d-renderer-fallback"),
        };
      });

      assert.ok(
        attrs !== null,
        `no [data-gosx-scene3d-mounted] element found on ${fixturePath}\n\nLogs:\n${logs}`,
      );

      assert.equal(
        attrs.backend,
        "webgl",
        `expected data-gosx-scene3d-backend="webgl" (honesty gate must downgrade from webgpu), got "${attrs.backend}"\n\nLogs:\n${logs}`,
      );

      assert.ok(
        attrs.fallback === "skinning" || attrs.fallback === "webgpu-unavailable",
        `expected fallback "skinning" from feature exclusion or "webgpu-unavailable" without a usable adapter, got "${attrs.fallback}"\n\nLogs:\n${logs}`,
      );
    } catch (error) {
      error.message += `\n\nCaptured logs:\n${logs}`;
      throw error;
    }
  },
);

async function waitForHealthy(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError = "";

  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) {
        return;
      }
      lastError = `status ${response.status}`;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }

  throw new Error(
    `timed out waiting for ${url}: ${lastError}\n\nLogs:\n${logs}`,
  );
}

function killProcessGroup(pid) {
  if (!pid) {
    return;
  }
  try {
    process.kill(-pid, "SIGTERM");
  } catch {}
}
