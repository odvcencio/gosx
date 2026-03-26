import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
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

test("gosx dev serves the docs app with working nav, scoped 404s, forms, and auth", { timeout: 90000 }, async () => {
  const navigationRequests = [];
  page.on("request", (request) => {
    if (request.headers()["x-gosx-navigation"] === "1") {
      navigationRequests.push(request.url());
    }
  });

  try {
    await page.goto(baseURL, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", {
      name: "GoSX now has a docs site that actually runs through its own pipeline.",
    }).waitFor();

    await page.getByRole("link", { name: "See file routing conventions" }).click();
    await page.waitForURL(/\/docs\/routing$/);
    await page.getByRole("heading", {
      name: "Routes can come from code or from the directory tree. Both are first-class now.",
    }).waitFor();
    assert.ok(
      navigationRequests.some((url) => url.endsWith("/docs/routing")),
      `expected a client navigation fetch for /docs/routing\n\nLogs:\n${logs}`,
    );

    await page.goto(`${baseURL}/docs/runtime`, { waitUntil: "domcontentloaded" });
    await page.getByText("Move across the surface to steer the camera and pull the geometry off center.").waitFor();
    await page.locator('[data-gosx-engine="GoSXScene3D"] canvas').waitFor();

    await page.goto(`${baseURL}/docs/missing`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", {
      name: "The docs subtree could not resolve this page.",
    }).waitFor();

    await page.goto(`${baseURL}/docs/forms`, { waitUntil: "domcontentloaded" });
    await page.getByRole("button", { name: "Submit the example form" }).click();
    await page.getByText("Email is required.").waitFor();

    await page.getByLabel("Name").fill("Ada");
    await page.getByLabel("Email").fill("ada@example.com");
    await page.getByRole("button", { name: "Submit the example form" }).click();
    await page.getByText("Validation state and success messages now survive a normal browser redirect.").waitFor();
    assert.equal(await page.getByLabel("Name").inputValue(), "Ada");

    await page.goto(`${baseURL}/labs/secret`, { waitUntil: "domcontentloaded" });
    await page.waitForURL(/\/docs\/auth(?:\?|$)/);
    await page.getByRole("heading", {
      name: "Auth in GoSX is a session concern, not a separate framework bolted on later.",
    }).waitFor();

    await page.getByLabel("Name").fill("Ada Lovelace");
    await page.getByRole("button", { name: "Sign in to the docs demo" }).click();
    await page.getByText("Signed in as Ada Lovelace.").waitFor();
    await page.getByText("Ada Lovelace").first().waitFor();

    await page.getByRole("link", { name: "Open the secret page" }).click();
    await page.waitForURL(/\/labs\/secret$/);
    await page.getByText("Current user: Ada Lovelace").waitFor();
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
