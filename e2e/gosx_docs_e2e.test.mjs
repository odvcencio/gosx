import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import path from "node:path";
import test, { after, before } from "node:test";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright-core";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");
const chromePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || "/usr/bin/google-chrome";
const baseURL = process.env.GOSX_E2E_BASE_URL || "http://127.0.0.1:3070";

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
    viewport: { width: 1440, height: 960 },
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

test("gosx dev serves the redesigned docs site", { timeout: 90000 }, async () => {
  try {
    // Homepage loads with showroom content
    await page.goto(baseURL, { waitUntil: "domcontentloaded" });
    await page.locator(".hero").waitFor();

    // Docs redirect works
    const docsResponse = await page.goto(`${baseURL}/docs`, { waitUntil: "domcontentloaded" });
    assert.ok(
      page.url().includes("/docs/getting-started"),
      `expected /docs to redirect to /docs/getting-started, got ${page.url()}\n\nLogs:\n${logs}`,
    );

    // Reference page loads with docs layout
    await page.goto(`${baseURL}/docs/routing`, { waitUntil: "domcontentloaded" });
    await page.locator(".docs-content").waitFor();

    // Demo page loads
    await page.goto(`${baseURL}/demos/galaxy`, { waitUntil: "domcontentloaded" });
    await page.locator(".galaxy-demo").waitFor();

    // Scoped 404 within /docs
    await page.goto(`${baseURL}/docs/nonexistent`, { waitUntil: "domcontentloaded" });
    await page.locator(".docs-404").waitFor();

    // Root 404
    await page.goto(`${baseURL}/totally-missing`, { waitUntil: "domcontentloaded" });
    await page.locator(".error-page").waitFor();
  } catch (error) {
    error.message += `\n\nCaptured logs:\n${logs}`;
    throw error;
  }
});

async function waitForHealthy(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError = "";

  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.status < 500) {
        return;
      }
      lastError = `status ${response.status}`;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }

  throw new Error(`timed out waiting for ${url}: ${lastError}\n\nLogs:\n${logs}`);
}

function killProcessGroup(pid) {
  if (!pid) {
    return;
  }
  try {
    process.kill(-pid, "SIGTERM");
  } catch {}
}
