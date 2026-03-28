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

test("gosx dev serves the landing page, demos, responsive drawer, scoped 404s, and auth", { timeout: 90000 }, async () => {
  const navigationRequests = [];
  const assetRequests = [];
  page.on("request", (request) => {
    if (request.headers()["x-gosx-navigation"] === "1") {
      navigationRequests.push(request.url());
    }
    if (request.url().includes("docs.css")) {
      assetRequests.push(request.url());
    }
  });

  try {
    await page.goto(baseURL, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", {
      name: "Build in Go. Ship the site, the editor, and the 3D demo together.",
    }).waitFor();

    await page.setViewportSize({ width: 900, height: 900 });
    await page.reload({ waitUntil: "domcontentloaded" });
    assert.equal(
      await page.locator(".route-drawer-panel").evaluate((el) => getComputedStyle(el).display),
      "none",
      `expected route drawer panel to stay hidden before opening\n\nLogs:\n${logs}`,
    );
    await page.locator(".route-drawer-summary").click();
    await page.locator(".route-drawer-panel").waitFor();
    assert.equal(
      await page.locator(".route-drawer").evaluate((el) => el.hasAttribute("open")),
      true,
      `expected route drawer to open\n\nLogs:\n${logs}`,
    );
    await page.mouse.click(860, 180);
    assert.equal(
      await page.locator(".route-drawer").evaluate((el) => el.hasAttribute("open")),
      false,
      `expected route drawer to close from the backdrop\n\nLogs:\n${logs}`,
    );

    await page.setViewportSize({ width: 1440, height: 960 });
    await page.goto(baseURL, { waitUntil: "domcontentloaded" });
    await page.getByRole("link", { name: "Open the CMS demo" }).first().click();
    await page.waitForURL(/\/demos\/cms$/);
    await page.getByRole("heading", {
      name: "The CMS flow stays document-shaped. Compose once, publish once.",
    }).waitFor();
    assert.ok(
      navigationRequests.some((url) => url.endsWith("/demos/cms")),
      `expected a client navigation fetch for /demos/cms\n\nLogs:\n${logs}`,
    );

    await page.locator('[data-cms-add-type="quote"]').click();
    await page.waitForFunction(() => document.querySelector("[data-cms-count]")?.textContent?.trim() === "4");
    await page.getByRole("button", { name: "Publish draft" }).click();
    await page.getByText("Draft published.").waitFor();

    await page.goto(`${baseURL}/demos/scene3d`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", {
      name: "Geometry Zoo is a native 3D route, not a detached client app.",
    }).waitFor();
    await page.locator('[data-gosx-engine="GoSXScene3D"] canvas').waitFor();

    await page.goto(`${baseURL}/docs/runtime`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", {
      name: "Page transitions reuse the runtime instead of pretending the browser does not exist.",
    }).waitFor();
    await page.waitForTimeout(150);
    assetRequests.length = 0;
    await page.getByRole("link", { name: "Back to overview" }).click();
    await page.waitForURL(baseURL);
    assert.equal(
      assetRequests.some((url) => url.includes("/docs/docs.css")),
      false,
      `expected nested-route navigation to avoid misresolved docs.css requests\n\nRequests:\n${assetRequests.join("\n")}\n\nLogs:\n${logs}`,
    );
    assert.equal(
      await page.locator(".docs-shell").evaluate((el) => getComputedStyle(el).display),
      "grid",
      `expected overview navigation to keep the docs shell styled after client navigation\n\nLogs:\n${logs}`,
    );
    assert.equal(
      await page.locator(".home-shell").evaluate((el) => getComputedStyle(el).display),
      "flex",
      `expected overview navigation to reapply page-scoped styles after client navigation\n\nLogs:\n${logs}`,
    );
    assert.match(
      await page.locator('link[rel="stylesheet"]').evaluate((el) => el.href),
      /\/docs\.css(?:\?|$)/,
      `expected overview navigation to keep the shared docs stylesheet rooted correctly\n\nLogs:\n${logs}`,
    );

    await page.goto(`${baseURL}/docs/missing`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", {
      name: "The docs subtree could not resolve this page.",
    }).waitFor();

    await page.goto(`${baseURL}/docs/forms`, { waitUntil: "domcontentloaded" });
    await page.getByRole("heading", {
      name: "GoSX forms can stay boring HTML and still feel like a framework feature.",
    }).waitFor();

    await page.goto(`${baseURL}/labs/secret`, { waitUntil: "domcontentloaded" });
    await page.waitForURL(/\/docs\/auth(?:\?|$)/);
    await page.getByRole("heading", {
      name: "Auth in GoSX is a session concern, not a bolt-on password stack.",
    }).waitFor();
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
