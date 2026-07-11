/**
 * Water release gate.
 *
 * This deliberately does not treat "a canvas exists" or parsed SceneIR state
 * as proof of rendering. The runtime must publish the common water-renderer
 * contract, advance real presentation/simulation counters, produce a
 * non-trivial composited image, react to authored controls, and stop work when
 * paused or offscreen. A second test removes GPU contexts and verifies that the
 * mount reports an honest unsupported state instead of masquerading as generic
 * WebGL.
 */

import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { createServer } from "node:net";
import path from "node:path";
import test, { after, before } from "node:test";
import { fileURLToPath } from "node:url";

import { chromium } from "playwright-core";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");
const chromePath = process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || "/usr/bin/google-chrome";
let baseURL = process.env.GOSX_WATER_E2E_BASE_URL || "";
const requireWebGPU = process.env.GOSX_WATER_E2E_REQUIRE_WEBGPU === "1";
const hardwareMinFPS = positiveNumber(process.env.GOSX_WATER_E2E_MIN_FPS, 50);
const hardwareP95MaxMS = positiveNumber(process.env.GOSX_WATER_E2E_P95_MAX_MS, 25);
const hardwareP99MaxMS = positiveNumber(process.env.GOSX_WATER_E2E_P99_MAX_MS, 33);

let appProcess;
let browser;
let context;
let page;
let logs = "";
let consoleLogs = "";

before(async () => {
  if (!baseURL) baseURL = `http://127.0.0.1:${await freePort()}`;
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
  appProcess.stdout.on("data", (chunk) => { logs += chunk; });
  appProcess.stderr.on("data", (chunk) => { logs += chunk; });

  await waitForHealthy(`${baseURL}/readyz`, 45000);

  const args = requireWebGPU
    ? ["--enable-unsafe-webgpu"]
    : [
        "--use-angle=swiftshader",
        "--enable-unsafe-swiftshader",
        "--disable-features=WebGPU",
      ];
  browser = await chromium.launch({ executablePath: chromePath, headless: true, args });
  context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  page = await context.newPage();
  page.on("console", (msg) => { consoleLogs += `[console.${msg.type()}] ${msg.text()}\n`; });
  page.on("pageerror", (err) => {
    consoleLogs += `[pageerror] ${err && err.message ? err.message : String(err)}\n`;
  });
});

after(async () => {
  await page?.close().catch(() => {});
  await context?.close().catch(() => {});
  await browser?.close().catch(() => {});
  if (appProcess) {
    killProcessGroup(appProcess.pid);
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
});

test("water renderer presents real frames, responds to controls, and obeys lifecycle", { timeout: 120000 }, async () => {
  const pageErrors = [];
  const requestURLs = [];
  const onPageError = (err) => pageErrors.push(err?.message || String(err));
  const onRequest = (request) => requestURLs.push(request.url());
  page.on("pageerror", onPageError);
  page.on("request", onRequest);

  try {
    const response = await page.goto(`${baseURL}/demos/water`, { waitUntil: "domcontentloaded" });
    assert.ok(response.ok(), `/demos/water returned ${response.status()}\n\nLogs:\n${logs}`);

    const mountSelector = "[data-gosx-scene3d-mounted]";
    await page.waitForSelector(`${mountSelector}[data-gosx-scene3d-water-renderer]`, { timeout: 45000 });
    const canvas = page.locator("canvas[data-gosx-scene3d-canvas]");
    await canvas.waitFor({ state: "visible", timeout: 30000 });

    const initial = await waterSnapshot(page);
    assert.equal(initial.renderer, "active", diagnostics("water renderer did not become active", initial));
    assert.ok(["webgl", "webgpu"].includes(initial.backend), diagnostics("water selected no real GPU backend", initial));
    if (requireWebGPU) {
      assert.equal(initial.backend, "webgpu", diagnostics("hardware certification requires WebGPU", initial));
    } else {
      assert.equal(initial.backend, "webgl", diagnostics("generic CI must exercise forced software WebGL", initial));
    }
    assert.equal(initial.systems, "1");
    assert.equal(initial.activeObject, "Sphere");
    assert.equal(initial.poolShape, "Box");

    const advanced = await waitForWaterAdvance(page, initial.frameSeq, initial.simulationSeq);
    assert.ok(advanced.frameSeq > initial.frameSeq, diagnostics("water presentation counter did not advance", advanced));
    assert.ok(advanced.simulationSeq > initial.simulationSeq, diagnostics("water simulation counter did not advance", advanced));

    const pixels = await compositedPixelStats(canvas, page);
    assert.ok(pixels.quantizedColors >= 24, `water canvas is visually flat (${JSON.stringify(pixels)})`);
    assert.ok(pixels.luminanceRange >= 35, `water canvas lacks visible tonal structure (${JSON.stringify(pixels)})`);
    assert.ok(pixels.luminanceStdDev >= 10, `water canvas resembles a blank/gradient fallback (${JSON.stringify(pixels)})`);

    if (requireWebGPU) {
      const framePerformance = await sampleWaterFramePerformance(page, 150);
      assert.ok(framePerformance.fps >= hardwareMinFPS,
        `hardware WebGPU FPS ${framePerformance.fps.toFixed(2)} is below ${hardwareMinFPS}: ${JSON.stringify(framePerformance)}`);
      assert.ok(framePerformance.p95 <= hardwareP95MaxMS,
        `hardware WebGPU p95 ${framePerformance.p95.toFixed(2)}ms exceeds ${hardwareP95MaxMS}ms: ${JSON.stringify(framePerformance)}`);
      assert.ok(framePerformance.p99 <= hardwareP99MaxMS,
        `hardware WebGPU p99 ${framePerformance.p99.toFixed(2)}ms exceeds ${hardwareP99MaxMS}ms: ${JSON.stringify(framePerformance)}`);
    }

    const objectSelect = page.locator('form[data-gosx-scene3d-controls] select[name="object"]');
    assert.deepEqual(
      await objectSelect.locator("option").evaluateAll((options) => options.map((option) => option.value)),
      ["None", "Sphere", "Cube", "TorusKnot", "Rubber Duck"],
    );
    await setControlValue(page, 'select[name="object"]', "Cube");
    await page.waitForFunction(
      () => document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-state-active-object") === "Cube",
    );

    const isDuckAsset = (url) => /\/water\/models\/duck\/Duck(?:\.gltf|0\.bin|CM\.(?:png|jpe?g))/i.test(url);
    const isGLTFFeature = (url) => /bootstrap-feature-scene3d-gltf(?:\.[^.]+)?\.js(?:\?|$)/i.test(url);
    assert.equal(requestURLs.some(isDuckAsset), false, "Duck assets loaded before Duck selection");
    assert.equal(requestURLs.some(isGLTFFeature), false, "glTF feature chunk loaded before Duck selection");

    await setControlValue(page, 'select[name="object"]', "TorusKnot");
    await page.waitForFunction(
      () => document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-state-active-object") === "TorusKnot",
    );
    assert.equal(requestURLs.some(isDuckAsset), false, "TorusKnot selection loaded Duck assets");
    assert.equal(requestURLs.some(isGLTFFeature), false, "TorusKnot selection loaded the glTF feature chunk");

    const duckModelRequest = page.waitForRequest((request) => /\/water\/models\/duck\/Duck\.gltf(?:\?|$)/i.test(request.url()), { timeout: 30000 });
    const gltfFeatureRequest = page.waitForRequest((request) => isGLTFFeature(request.url()), { timeout: 30000 });
    await setControlValue(page, 'select[name="object"]', "Rubber Duck");
    await page.waitForFunction(
      () => document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-state-active-object") === "Rubber Duck",
    );
    await Promise.all([duckModelRequest, gltfFeatureRequest]);
    assert.ok(requestURLs.some(isDuckAsset), "Duck selection did not request its glTF assets");
    assert.ok(requestURLs.some(isGLTFFeature), "Duck selection did not request the glTF feature chunk");

    await setControlValue(page, 'select[name="object"]', "Sphere");
    await page.waitForFunction(
      () => document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-state-active-object") === "Sphere",
    );

    await setControlValue(page, 'select[name="poolShape"]', "Rounded Box");
    await setRange(page, 'input[name="poolWidth"]', "1.5");
    await page.waitForFunction(() => {
      const el = document.querySelector("[data-gosx-scene3d-mounted]");
      return el?.getAttribute("data-gosx-scene3d-water-state-pool-shape") === "Rounded Box" &&
        Number(el?.getAttribute("data-gosx-scene3d-water-state-pool-width")) === 1.5;
    });

    await setChecked(page, 'input[name="paused"]', true);
    await page.waitForFunction(
      () => document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-paused") === "true",
    );
    await page.waitForTimeout(250);
    const pauseStart = await waterSnapshot(page);
    await page.waitForTimeout(750);
    const pauseEnd = await waterSnapshot(page);
    assert.equal(pauseEnd.simulationSeq, pauseStart.simulationSeq, diagnostics("paused water continued simulating", pauseEnd));

    await setChecked(page, 'input[name="paused"]', false);
    await waitForWaterAdvance(page, pauseEnd.frameSeq, pauseEnd.simulationSeq);

    // Force the observed mount outside layout. The mount-owned scheduler must
    // stop submitting water work and recover after visibility returns.
    await page.evaluate(() => {
      const el = document.querySelector("[data-gosx-scene3d-mounted]");
      if (el) el.style.display = "none";
    });
    await page.waitForFunction(
      () => document.querySelector("[data-gosx-scene3d-mounted]")?.getAttribute("data-gosx-scene3d-water-lifecycle") === "offscreen",
    );
    await page.waitForTimeout(250);
    const hiddenStart = await waterSnapshot(page);
    await page.waitForTimeout(750);
    const hiddenEnd = await waterSnapshot(page);
    assert.equal(hiddenEnd.frameSeq, hiddenStart.frameSeq, diagnostics("offscreen water continued presenting", hiddenEnd));
    assert.equal(hiddenEnd.simulationSeq, hiddenStart.simulationSeq, diagnostics("offscreen water continued simulating", hiddenEnd));

    await page.evaluate(() => {
      const el = document.querySelector("[data-gosx-scene3d-mounted]");
      if (el) el.style.display = "";
    });
    await waitForWaterAdvance(page, hiddenEnd.frameSeq, hiddenEnd.simulationSeq);

    assert.deepEqual(pageErrors, [], `page errors:\n${pageErrors.join("\n")}\n\n${consoleLogs}`);
  } catch (error) {
    error.message += `\n\nCaptured console:\n${consoleLogs}\n\nCaptured server logs:\n${logs}`;
    throw error;
  } finally {
    page.off("pageerror", onPageError);
    page.off("request", onRequest);
  }
});

test("water reports unsupported when no GPU water context can be created", { timeout: 60000 }, async () => {
  const unsupportedContext = await browser.newContext({ viewport: { width: 800, height: 600 } });
  await unsupportedContext.addInitScript(() => {
    const original = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (kind, ...args) {
      if (kind === "webgl" || kind === "experimental-webgl" || kind === "webgl2" || kind === "webgpu") return null;
      return original.call(this, kind, ...args);
    };
    try { Object.defineProperty(navigator, "gpu", { configurable: true, value: undefined }); } catch {}
  });
  const unsupportedPage = await unsupportedContext.newPage();
  try {
    const response = await unsupportedPage.goto(`${baseURL}/demos/water`, { waitUntil: "domcontentloaded" });
    assert.ok(response.ok());
    await unsupportedPage.waitForSelector('[data-gosx-scene3d-mounted][data-gosx-scene3d-water-renderer="unsupported"]', { timeout: 30000 });
    const state = await waterSnapshot(unsupportedPage);
    assert.equal(state.renderer, "unsupported");
    assert.ok(state.unsupportedReason, diagnostics("unsupported water state omitted its reason", state));
    assert.notEqual(state.backend, "webgl", diagnostics("generic WebGL masqueraded as a working water renderer", state));
  } finally {
    await unsupportedPage.close();
    await unsupportedContext.close();
  }
});

async function waterSnapshot(targetPage) {
  return targetPage.evaluate(() => {
    const el = document.querySelector("[data-gosx-scene3d-mounted]");
    const attr = (name) => el?.getAttribute(name) || "";
    const number = (name) => Number(attr(name) || 0);
    return {
      backend: attr("data-gosx-scene3d-backend"),
      renderer: attr("data-gosx-scene3d-water-renderer"),
      unsupportedReason: attr("data-gosx-scene3d-water-unsupported-reason"),
      systems: attr("data-gosx-scene3d-water-state-systems"),
      activeObject: attr("data-gosx-scene3d-water-state-active-object"),
      poolShape: attr("data-gosx-scene3d-water-state-pool-shape"),
      frameSeq: Number(el?.__gosxScene3DWaterFrameSeq || number("data-gosx-scene3d-water-frame-seq")),
      simulationSeq: Number(el?.__gosxScene3DWaterSimulationSeq || number("data-gosx-scene3d-water-simulation-seq")),
      loop: attr("data-gosx-scene3d-render-loop"),
      loopReason: attr("data-gosx-scene3d-render-loop-reason"),
      lifecycle: attr("data-gosx-scene3d-water-lifecycle"),
    };
  });
}

async function waitForWaterAdvance(targetPage, frameSeq, simulationSeq) {
  await targetPage.waitForFunction(
    ([frame, simulation]) => {
      const el = document.querySelector("[data-gosx-scene3d-mounted]");
      const exactFrame = Number(el?.__gosxScene3DWaterFrameSeq || el?.getAttribute("data-gosx-scene3d-water-frame-seq") || 0);
      const exactSimulation = Number(el?.__gosxScene3DWaterSimulationSeq || el?.getAttribute("data-gosx-scene3d-water-simulation-seq") || 0);
      return exactFrame > frame && exactSimulation > simulation;
    },
    [frameSeq, simulationSeq],
    { timeout: 15000 },
  );
  return waterSnapshot(targetPage);
}

async function sampleWaterFramePerformance(targetPage, sampleCount) {
  const intervals = await targetPage.evaluate((count) => new Promise((resolve) => {
    const samples = [];
    let previous = 0;
    let warmup = 20;
    function frame(now) {
      if (warmup > 0) {
        warmup--;
      } else if (previous > 0) {
        samples.push(now - previous);
      }
      previous = now;
      if (samples.length >= count) resolve(samples);
      else requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  }), sampleCount);
  const sorted = [...intervals].sort((a, b) => a - b);
  const mean = intervals.reduce((sum, value) => sum + value, 0) / intervals.length;
  const percentile = (value) => sorted[Math.min(sorted.length - 1, Math.max(0, Math.ceil(sorted.length * value) - 1))];
  return { samples: intervals.length, fps: 1000 / mean, mean, p95: percentile(0.95), p99: percentile(0.99) };
}

function positiveNumber(value, fallback) {
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

async function setRange(targetPage, selector, value) {
  await targetPage.locator(selector).evaluate((input, next) => {
    input.value = next;
    input.dispatchEvent(new Event("input", { bubbles: true }));
    input.dispatchEvent(new Event("change", { bubbles: true }));
  }, value);
}

async function setControlValue(targetPage, selector, value) {
  await targetPage.locator(selector).evaluate((control, next) => {
    control.value = next;
    control.dispatchEvent(new Event("change", { bubbles: true }));
  }, value);
}

async function setChecked(targetPage, selector, checked) {
  await targetPage.locator(selector).evaluate((control, next) => {
    control.checked = next;
    control.dispatchEvent(new Event("change", { bubbles: true }));
  }, checked);
}

async function compositedPixelStats(canvas, targetPage) {
  const png = await canvas.screenshot({ type: "png" });
  return targetPage.evaluate(async (bytes) => {
    const blob = new Blob([new Uint8Array(bytes)], { type: "image/png" });
    const bitmap = await createImageBitmap(blob);
    const bitmapWidth = bitmap.width;
    const bitmapHeight = bitmap.height;
    const sampleWidth = Math.min(160, bitmap.width);
    const sampleHeight = Math.min(100, bitmap.height);
    const surface = new OffscreenCanvas(sampleWidth, sampleHeight);
    const ctx = surface.getContext("2d", { willReadFrequently: true });
    ctx.drawImage(bitmap, 0, 0, sampleWidth, sampleHeight);
    bitmap.close();
    const data = ctx.getImageData(0, 0, sampleWidth, sampleHeight).data;
    const colors = new Set();
    const luminances = [];
    for (let i = 0; i < data.length; i += 16) {
      const r = data[i];
      const g = data[i + 1];
      const b = data[i + 2];
      colors.add(`${r >> 4}:${g >> 4}:${b >> 4}`);
      luminances.push(0.2126 * r + 0.7152 * g + 0.0722 * b);
    }
    const mean = luminances.reduce((sum, value) => sum + value, 0) / luminances.length;
    const variance = luminances.reduce((sum, value) => sum + (value - mean) ** 2, 0) / luminances.length;
    return {
      width: bitmapWidth,
      height: bitmapHeight,
      quantizedColors: colors.size,
      luminanceRange: Math.max(...luminances) - Math.min(...luminances),
      luminanceStdDev: Math.sqrt(variance),
    };
  }, [...png]);
}

function diagnostics(message, state) {
  return `${message}: ${JSON.stringify(state)}\n\nConsole:\n${consoleLogs}\n\nLogs:\n${logs}`;
}

async function waitForHealthy(url, timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  let lastError = "";
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      if (response.ok) return;
      lastError = `status ${response.status}`;
    } catch (error) {
      lastError = error instanceof Error ? error.message : String(error);
    }
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
  throw new Error(`timed out waiting for ${url}: ${lastError}\n\nLogs:\n${logs}`);
}

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

function killProcessGroup(pid) {
  if (!pid) return;
  try { process.kill(-pid, "SIGTERM"); } catch {}
}
