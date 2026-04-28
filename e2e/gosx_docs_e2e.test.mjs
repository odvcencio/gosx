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
    // Homepage renders successfully
    const homeRes = await page.goto(baseURL, { waitUntil: "domcontentloaded" });
    assert.ok(homeRes.ok(), `homepage returned ${homeRes.status()}\n\nLogs:\n${logs}`);
    const homeTitle = await page.title();
    assert.ok(homeTitle.includes("GoSX"), `expected title containing GoSX, got "${homeTitle}"\n\nLogs:\n${logs}`);

    // Docs redirect works
    await page.goto(`${baseURL}/docs`, { waitUntil: "domcontentloaded" });
    assert.ok(
      page.url().includes("/docs/getting-started"),
      `expected /docs to redirect to /docs/getting-started, got ${page.url()}\n\nLogs:\n${logs}`,
    );

    // Reference pages render
    for (const path of ["/docs/routing", "/docs/forms", "/docs/scene3d"]) {
      const res = await page.goto(`${baseURL}${path}`, { waitUntil: "domcontentloaded" });
      assert.ok(res.ok(), `${path} returned ${res.status()}\n\nLogs:\n${logs}`);
    }

    // Scoped 404 within /docs returns page (not crash)
    const scoped404 = await page.goto(`${baseURL}/docs/nonexistent`, { waitUntil: "domcontentloaded" });
    assert.equal(scoped404.status(), 404, `expected 404 for /docs/nonexistent\n\nLogs:\n${logs}`);

    // Root 404
    const root404 = await page.goto(`${baseURL}/totally-missing`, { waitUntil: "domcontentloaded" });
    assert.equal(root404.status(), 404, `expected 404 for /totally-missing\n\nLogs:\n${logs}`);
  } catch (error) {
    error.message += `\n\nCaptured logs:\n${logs}`;
    throw error;
  }
});

test("docs site preserves baseline accessibility invariants", { timeout: 90000 }, async () => {
  const res = await page.goto(`${baseURL}/docs/forms`, { waitUntil: "domcontentloaded" });
  assert.ok(res.ok(), `/docs/forms returned ${res.status()}\n\nLogs:\n${logs}`);

  const report = await page.evaluate(() => {
    const ids = new Map();
    for (const el of document.querySelectorAll("[id]")) {
      const id = el.getAttribute("id");
      ids.set(id, (ids.get(id) || 0) + 1);
    }
    const duplicateIds = [...ids.entries()].filter(([, count]) => count > 1).map(([id]) => id);
    const unnamedControls = [...document.querySelectorAll("button, a[href], input, select, textarea")]
      .filter((el) => {
        if (el.matches("input[type=hidden]")) return false;
        const labelledBy = el.getAttribute("aria-labelledby");
        const label = el.id ? document.querySelector(`label[for="${CSS.escape(el.id)}"]`) : null;
        const name = [
          el.getAttribute("aria-label"),
          labelledBy && labelledBy.split(/\s+/).map((id) => document.getElementById(id)?.textContent || "").join(" "),
          label?.textContent,
          el.textContent,
          el.getAttribute("title"),
          el.getAttribute("placeholder"),
        ].filter(Boolean).join(" ").trim();
        return name === "";
      })
      .map((el) => el.outerHTML.slice(0, 160));
    const brokenDescriptions = [...document.querySelectorAll("[aria-describedby]")]
      .filter((el) => el.getAttribute("aria-describedby").split(/\s+/).some((id) => id && !document.getElementById(id)))
      .map((el) => el.outerHTML.slice(0, 160));
    return {
      hasMain: !!document.querySelector("main#main-content"),
      hasContentInfo: !!document.querySelector('[role="contentinfo"]'),
      duplicateIds,
      unnamedControls,
      brokenDescriptions,
    };
  });

  assert.equal(report.hasMain, true, "expected main#main-content landmark");
  assert.equal(report.hasContentInfo, true, "expected contentinfo landmark");
  assert.deepEqual(report.duplicateIds, [], `duplicate ids: ${report.duplicateIds.join(", ")}`);
  assert.deepEqual(report.unnamedControls, [], `unnamed controls: ${report.unnamedControls.join("\n")}`);
  assert.deepEqual(report.brokenDescriptions, [], `broken aria-describedby refs: ${report.brokenDescriptions.join("\n")}`);
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
