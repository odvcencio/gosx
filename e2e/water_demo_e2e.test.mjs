/**
 * Water demo e2e smoke test.
 *
 * CI's other e2e suites never load /demos/water; this covers the gap. Navigates
 * to the jeantimex-water port at /demos/water (examples/gosx-docs/app/demos/
 * water/page.gsx), waits for the Scene3D to mount, and asserts:
 *
 *   - the Scene3D canvas + mount element exist and report a real backend.
 *     preferWebGPU={true} is set on the <Scene3D>, but headless Chrome has no
 *     navigator.gpu, so the honesty gate downgrades to WebGL. Accept either
 *     "webgl" or "webgpu" (never "canvas"/"canvas2d"/absent — unlike the
 *     simpler motion-spin fixture, the water scene's feature set does not fit
 *     the canvas2d fallback).
 *   - the water renderer actually engaged: publishSceneWaterStateSnapshot
 *     (client/js/bootstrap-src/20-scene-mount.js) stamps
 *     data-gosx-scene3d-water-state-systems / -active-object onto the mount
 *     element from the live scene state at mount time, independent of which
 *     backend (webgl/webgpu) ultimately renders it.
 *   - no page errors fire during load or over a couple of seconds of runtime.
 *   - the declarative control form (page.gsx's <select name="object">) is
 *     present with the Sphere/Cube/TorusKnot/Rubber Duck options.
 *
 * Harness mirrors gosx_docs_e2e.test.mjs / webgpu_honesty_gate_e2e.test.mjs:
 * same app server launch, same before/after lifecycle, same waitForHealthy /
 * killProcessGroup helpers.
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
// Distinct port: 3070-3073 are already claimed by the other e2e suites and
// 8123 is in use elsewhere in this environment.
const baseURL = process.env.GOSX_WATER_E2E_BASE_URL || "http://127.0.0.1:8127";

let appProcess;
let browser;
let context;
let page;
let logs = "";
let consoleLogs = "";

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
  page.on("console", (msg) => {
    consoleLogs += `[console.${msg.type()}] ${msg.text()}\n`;
  });
  page.on("pageerror", (err) => {
    consoleLogs += `[pageerror] ${err && err.message ? err.message : String(err)}\n`;
  });
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
  "water demo mounts, renders on a real backend, and engages the water renderer",
  { timeout: 90000 },
  async () => {
    const pageErrors = [];
    const onPageError = (err) => {
      pageErrors.push(err && err.message ? err.message : String(err));
    };
    page.on("pageerror", onPageError);

    try {
      const fixturePath = "/demos/water";
      const res = await page.goto(`${baseURL}${fixturePath}`, {
        waitUntil: "domcontentloaded",
      });
      assert.ok(
        res.ok(),
        `${fixturePath} returned ${res.status()}\n\nLogs:\n${logs}`,
      );

      // Wait for the Scene3D mount to finish initialising. The runtime sets
      // data-gosx-scene3d-backend once it has selected and started the renderer.
      await page.waitForSelector(
        "[data-gosx-scene3d-mounted][data-gosx-scene3d-backend]",
        { timeout: 30000 },
      );

      // The canvas the renderer draws into must exist and be visible.
      const canvas = page.locator("canvas[data-gosx-scene3d-canvas]");
      await canvas.waitFor({ state: "visible", timeout: 30000 });

      const attrs = await page.evaluate(() => {
        const el = document.querySelector("[data-gosx-scene3d-mounted]");
        if (!el) return null;
        return {
          backend: el.getAttribute("data-gosx-scene3d-backend"),
          waterSystems: el.getAttribute("data-gosx-scene3d-water-state-systems"),
          waterActiveObject: el.getAttribute("data-gosx-scene3d-water-state-active-object"),
          waterPoolShape: el.getAttribute("data-gosx-scene3d-water-state-pool-shape"),
        };
      });

      assert.ok(
        attrs !== null,
        `no [data-gosx-scene3d-mounted] element found on ${fixturePath}\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`,
      );

      // preferWebGPU={true} is set on page.gsx's <Scene3D>, but headless
      // Chrome has no navigator.gpu, so the honesty gate downgrades to WebGL.
      // Accept either a real WebGL or WebGPU backend -- never a canvas2d
      // fallback or an absent backend.
      assert.ok(
        ["webgl", "webgpu"].includes(attrs.backend),
        `expected data-gosx-scene3d-backend in {webgl,webgpu}, got "${attrs.backend}"\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`,
      );
      console.log(`[water-demo] Scene3D backend = ${attrs.backend}`);

      // publishSceneWaterStateSnapshot (20-scene-mount.js) stamps these from
      // the live sceneState.waterSystems at mount time, independent of which
      // backend ultimately renders -- so they're robust indicators that the
      // <WaterSystem> in page.gsx was parsed and handed to the renderer,
      // rather than a brittle draw-call/dispatch count that only the WebGPU
      // debug path publishes.
      assert.equal(
        attrs.waterSystems,
        "1",
        `expected data-gosx-scene3d-water-state-systems="1" (page.gsx declares one <WaterSystem>), got "${attrs.waterSystems}"\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`,
      );
      assert.equal(
        attrs.waterActiveObject,
        "Sphere",
        `expected data-gosx-scene3d-water-state-active-object="Sphere" (page.gsx activeObject default), got "${attrs.waterActiveObject}"\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`,
      );
      assert.equal(
        attrs.waterPoolShape,
        "Box",
        `expected data-gosx-scene3d-water-state-pool-shape="Box" (page.gsx poolShape default), got "${attrs.waterPoolShape}"\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`,
      );

      // The declarative control form (page.gsx's <form data-gosx-scene3d-controls>)
      // must be present with the object picker and its authored options.
      const objectOptions = await page.evaluate(() => {
        const select = document.querySelector(
          'form[data-gosx-scene3d-controls] select[name="object"]',
        );
        if (!select) return null;
        return Array.from(select.options).map((opt) => opt.value);
      });
      assert.ok(
        objectOptions !== null,
        `no select[name="object"] found inside the water controls form\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`,
      );
      for (const expected of ["Sphere", "Cube", "TorusKnot", "Rubber Duck"]) {
        assert.ok(
          objectOptions.includes(expected),
          `expected object dropdown to include "${expected}", got [${objectOptions.join(", ")}]\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`,
        );
      }

      // Let the scene run for a couple of seconds (water sim ticks, render
      // loop draws) and confirm nothing throws during runtime, not just load.
      await new Promise((resolve) => setTimeout(resolve, 2000));

      assert.deepEqual(
        pageErrors,
        [],
        `expected no page errors during load + runtime, got:\n${pageErrors.join("\n")}\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`,
      );
    } catch (error) {
      error.message += `\n\nCaptured console:\n${consoleLogs}\n\nCaptured logs:\n${logs}`;
      throw error;
    } finally {
      page.off("pageerror", onPageError);
    }
  },
);

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
