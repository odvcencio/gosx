#!/usr/bin/env node
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

const DEFAULT_URL = "http://localhost:3000/demos/water?managed=1782462513988";
const DEFAULT_PLAYWRIGHT_CORE =
  "C:/Users/odvce/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/.pnpm/playwright-core@1.61.0/node_modules/playwright-core/index.mjs";
const BROWSER_CANDIDATES = [
  "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
  "C:/Program Files/Microsoft/Edge/Application/msedge.exe",
  "C:/Program Files/Google/Chrome/Application/chrome.exe",
  "C:/Program Files (x86)/Google/Chrome/Application/chrome.exe",
];

const args = new Set(process.argv.slice(2));
const url = readArg("--url") || process.env.GOSX_SCENE3D_PROBE_URL || DEFAULT_URL;
const runBrowser = args.has("--browser") || process.env.GOSX_SCENE3D_PROBE_BROWSER === "1";
const exerciseControls = args.has("--exercise-controls") || process.env.GOSX_SCENE3D_PROBE_EXERCISE_CONTROLS === "1";
const screenshotPath = readArg("--screenshot") || process.env.GOSX_SCENE3D_PROBE_SCREENSHOT || "";

const report = {
  platform: process.platform,
  node: process.version,
  cwd: process.cwd(),
  url,
  http: {},
  assets: {},
  browser: null,
};

const page = await fetchText(url);
report.http = {
  status: page.status,
  bytes: page.text.length,
  scene3dBootstrap: page.text.includes("bootstrap-feature-scene3d"),
  webgpuBootstrap: page.text.includes("bootstrap-feature-scene3d-webgpu"),
  modelMatrixUniform: page.text.includes("modelMatrix: mat4x4f"),
  objectShadowRadiusWorld: page.text.includes("objectShadowRadiusWorld"),
};

assert(page.status >= 200 && page.status < 400, `page returned ${page.status}`);
assert(report.http.scene3dBootstrap, "page does not reference the Scene3D runtime chunk");
assert(report.http.webgpuBootstrap, "page does not reference the WebGPU Scene3D runtime chunk");
assert(report.http.modelMatrixUniform, "page does not contain the water modelMatrix Selena uniform");
assert(report.http.objectShadowRadiusWorld, "page does not contain object-shadow world-radius shader code");

const assetURLs = assetURLsFromPage(page.text, url);
const webglAsset = assetURLs.find((assetURL) => /bootstrap-feature-scene3d\.[^.]+\.js$/.test(assetURL));
const webgpuAsset = assetURLs.find((assetURL) => /bootstrap-feature-scene3d-webgpu\.[^.]+\.js$/.test(assetURL));
assert(webglAsset, "could not find hashed WebGL Scene3D asset URL in page");
assert(webgpuAsset, "could not find hashed WebGPU Scene3D asset URL in page");

const webgl = await fetchText(webglAsset);
const webgpu = await fetchText(webgpuAsset);
report.assets.webgl = inspectScene3DAsset(webglAsset, webgl.text);
report.assets.webgpu = inspectScene3DAsset(webgpuAsset, webgpu.text);

assert(report.assets.webgl.directVertices, "WebGL Scene3D asset does not contain directVertices branch");
assert(report.assets.webgl.modelMatrix, "WebGL Scene3D asset does not contain modelMatrix handling");
assert(report.assets.webgpu.directVertices, "WebGPU Scene3D asset does not contain directVertices branch");
assert(report.assets.webgpu.modelMatrix, "WebGPU Scene3D asset does not contain modelMatrix handling");
assert(report.assets.webgpu.waterObjectTexturePipeline, "WebGPU Scene3D asset does not contain the water object-texture pipeline");
assert(report.assets.webgpu.waterObjectMeshShadowPipeline, "WebGPU Scene3D asset does not contain the water object mesh-shadow pipeline");

if (runBrowser) {
  report.browser = await runBrowserProbe(url, screenshotPath);
}

console.log(JSON.stringify(report, null, 2));

function readArg(name) {
  const prefix = `${name}=`;
  const inline = process.argv.find((arg) => arg.startsWith(prefix));
  if (inline) return inline.slice(prefix.length);
  const index = process.argv.indexOf(name);
  if (index >= 0 && index + 1 < process.argv.length) return process.argv[index + 1];
  return "";
}

async function fetchText(targetURL) {
  const response = await fetch(targetURL);
  const text = await response.text();
  return { status: response.status, text };
}

function assetURLsFromPage(html, pageURL) {
  const urls = new Set();
  const sourcePattern = /\b(?:src|href)=["']([^"']+)["']/g;
  for (const match of html.matchAll(sourcePattern)) {
    if (match[1].includes("bootstrap-feature-scene3d")) {
      urls.add(new URL(match[1], pageURL).href);
    }
  }
  return [...urls];
}

function inspectScene3DAsset(assetURL, text) {
  return {
    url: assetURL,
    bytes: text.length,
    directVertices: text.includes("directVertices"),
    modelMatrix: text.includes("modelMatrix"),
    selena: text.includes("Selena") || text.includes("selena"),
    waterObjectTexturePipeline: text.includes("gosx-water-object-texture-pass"),
    waterObjectMeshShadowPipeline: text.includes("gosx-water-object-mesh-shadow"),
  };
}

async function runBrowserProbe(targetURL, explicitScreenshotPath) {
  const executablePath = browserExecutablePath();
  const { chromium } = await import(playwrightCoreURL());
  const browser = await chromium.launch({
    executablePath,
    headless: true,
  });
  const context = await browser.newContext({
    viewport: { width: 1440, height: 960 },
  });
  const browserReport = {
    executablePath,
    title: "",
    url: "",
    canvases: [],
    scene3dMount: null,
    waterTelemetry: null,
    performance: null,
    controls: null,
    consoleErrors: [],
    failedRequests: [],
    httpErrors: [],
    pageErrors: [],
    screenshot: "",
  };
  const page = await context.newPage();
  page.on("console", (message) => {
    if (message.type() === "error") {
      browserReport.consoleErrors.push({ text: message.text(), location: message.location() });
    }
  });
  page.on("pageerror", (error) => {
    browserReport.pageErrors.push(error.message);
  });
  page.on("requestfailed", (request) => {
    browserReport.failedRequests.push({
      url: request.url(),
      failure: request.failure()?.errorText || "",
    });
  });
  page.on("response", (response) => {
    if (response.status() >= 400) {
      browserReport.httpErrors.push({
        url: response.url(),
        status: response.status(),
      });
    }
  });

  try {
    const response = await page.goto(targetURL, { waitUntil: "domcontentloaded", timeout: 30000 });
    assert(response && response.ok(), `browser navigation returned ${response?.status() ?? "no response"}`);
    await page.waitForLoadState("networkidle", { timeout: 15000 }).catch(() => {});
    await page.waitForTimeout(1000);
    browserReport.title = await page.title();
    browserReport.url = page.url();
    browserReport.canvases = await page.evaluate(() =>
      [...document.querySelectorAll("canvas")].map((canvas) => {
        const rect = canvas.getBoundingClientRect();
        return {
          width: canvas.width,
          height: canvas.height,
          clientWidth: Math.round(rect.width),
          clientHeight: Math.round(rect.height),
          webgpu: !!canvas.getContext("webgpu"),
          webgl:
            !!canvas.getContext("webgl2") ||
            !!canvas.getContext("webgl") ||
            !!canvas.getContext("experimental-webgl"),
        };
      }),
    );
    browserReport.scene3dMount = await page.evaluate(() => {
      const mount = document.querySelector("[data-gosx-scene3d-renderer], .gosx-scene3d, [data-gosx-scene3d]");
      if (!mount) return null;
      return {
        tagName: mount.tagName,
        renderer: mount.getAttribute("data-gosx-scene3d-renderer") || "",
        fallback: mount.getAttribute("data-gosx-scene3d-renderer-fallback") || "",
        frameSeq: Number(mount.getAttribute("data-gosx-scene3d-webgpu-frame-seq") || 0),
        waterRenderer: mount.getAttribute("data-gosx-scene3d-water-renderer") || "",
        waterFrameSeq: Number(mount.getAttribute("data-gosx-scene3d-water-frame-seq") || 0),
      };
    });
    browserReport.waterTelemetry = await page.evaluate(() => {
      const mount = document.querySelector("[data-gosx-scene3d-webgpu-water-systems]");
      if (!mount) return null;
      const numberAttr = (name) => Number(mount.getAttribute("data-gosx-scene3d-webgpu-" + name) || 0);
      const stringAttr = (name) => mount.getAttribute("data-gosx-scene3d-webgpu-" + name) || "";
      return {
        waterSystems: numberAttr("water-systems"),
        waterCells: numberAttr("water-cells"),
        waterVertices: numberAttr("water-vertices"),
        waterComputeDispatches: numberAttr("water-compute-dispatches"),
        waterAuthoredComputeSystems: numberAttr("water-authored-compute-systems"),
        waterAuthoredComputeDispatches: numberAttr("water-authored-compute-dispatches"),
        waterAuthoredComputeFallbacks: numberAttr("water-authored-compute-fallbacks"),
        waterLightDirX: numberAttr("water-light-dir-x"),
        waterLightDirY: numberAttr("water-light-dir-y"),
        waterLightDirZ: numberAttr("water-light-dir-z"),
        waterCausticPasses: numberAttr("water-caustic-passes"),
        waterCausticTexturePixels: numberAttr("water-caustic-texture-pixels"),
        waterAuthoredCausticPasses: numberAttr("water-authored-caustic-passes"),
        waterAuthoredCausticFallbacks: numberAttr("water-authored-caustic-fallbacks"),
        waterObjectTexturePasses: numberAttr("water-object-texture-passes"),
        waterObjectTextureTargets: numberAttr("water-object-texture-targets"),
        waterObjectTexturePixels: numberAttr("water-object-texture-pixels"),
        waterObjectTextureWidth: numberAttr("water-object-texture-width"),
        waterObjectTextureHeight: numberAttr("water-object-texture-height"),
        waterObjectTexturePixelBudget: numberAttr("water-object-texture-pixel-budget"),
        waterObjectTextureMeshPasses: numberAttr("water-object-texture-mesh-passes"),
        waterObjectTextureMeshDrawCalls: numberAttr("water-object-texture-mesh-draw-calls"),
        waterObjectTextureSelenaDrawCalls: numberAttr("water-object-texture-selena-draw-calls"),
        waterObjectTextureFallbackPasses: numberAttr("water-object-texture-fallback-passes"),
        waterObjectTextureSelectedObjects: numberAttr("water-object-texture-selected-objects"),
        waterObjectShadowPasses: numberAttr("water-object-shadow-passes"),
        waterObjectShadowTexturePixels: numberAttr("water-object-shadow-texture-pixels"),
        waterObjectShadowMeshPasses: numberAttr("water-object-shadow-mesh-passes"),
        waterObjectShadowMeshDrawCalls: numberAttr("water-object-shadow-mesh-draw-calls"),
        waterAuthoredObjectMeshShadowPasses: numberAttr("water-authored-object-mesh-shadow-passes"),
        waterAuthoredObjectMeshShadowFallbacks: numberAttr("water-authored-object-mesh-shadow-fallbacks"),
        waterObjectShadowFallbackPasses: numberAttr("water-object-shadow-fallback-passes"),
        waterPoolTileTextureLoaded: numberAttr("water-pool-tile-texture-loaded"),
        waterPoolTileTextureFallbacks: numberAttr("water-pool-tile-texture-fallbacks"),
        waterAuthoredPoolPasses: numberAttr("water-authored-pool-passes"),
        waterAuthoredPoolFallbacks: numberAttr("water-authored-pool-fallbacks"),
        waterDrawCalls: numberAttr("water-draw-calls"),
        waterDrawVertices: numberAttr("water-draw-vertices"),
        waterSurfaceAboveDrawCalls: numberAttr("water-surface-above-draw-calls"),
        waterSurfaceBelowDrawCalls: numberAttr("water-surface-below-draw-calls"),
        waterAuthoredSurfaceSystems: numberAttr("water-authored-surface-systems"),
        waterAuthoredSurfaceDrawCalls: numberAttr("water-authored-surface-draw-calls"),
        waterAuthoredSurfaceFallbacks: numberAttr("water-authored-surface-fallbacks"),
        waterSkyCubeTextureLoaded: numberAttr("water-sky-cube-texture-loaded"),
        waterSkyCubeTextureFallbacks: numberAttr("water-sky-cube-texture-fallbacks"),
        candidateProfile: stringAttr("water-object-texture-candidate-profile"),
      };
    });
    browserReport.performance = await sampleWaterFramePerformance(page);
    browserReport.pixels = await compositedPixelStats(page);
    assert(browserReport.canvases.length > 0, "browser page did not render a canvas");
    assert(
      browserReport.canvases.some((canvas) => canvas.clientWidth > 0 && canvas.clientHeight > 0),
      "browser rendered only zero-sized canvases",
    );
    assertWaterTelemetry(browserReport);
    assertWaterPerformance(browserReport);
    if (exerciseControls) {
      browserReport.controls = await exerciseWaterControls(page);
    }
    assert(browserReport.pageErrors.length === 0, "browser page errors: " + JSON.stringify(browserReport.pageErrors));
    assert(browserReport.failedRequests.length === 0, "browser failed requests: " + JSON.stringify(browserReport.failedRequests));
    assert(browserReport.httpErrors.length === 0, "browser HTTP errors: " + JSON.stringify(browserReport.httpErrors));
    assert(browserReport.consoleErrors.length === 0, "browser console errors: " + JSON.stringify(browserReport.consoleErrors));
    if (explicitScreenshotPath) {
      const absoluteScreenshotPath = path.resolve(explicitScreenshotPath);
      fs.mkdirSync(path.dirname(absoluteScreenshotPath), { recursive: true });
      await page.screenshot({ path: absoluteScreenshotPath, fullPage: true });
      browserReport.screenshot = absoluteScreenshotPath;
    }
  } finally {
    await page.close().catch(() => {});
    await context.close().catch(() => {});
    await browser.close().catch(() => {});
  }

  return browserReport;
}

async function exerciseWaterControls(page) {
  const before = await readWaterControlState(page);
  assert(before.objectValue === "Sphere", "initial water object should be Sphere: " + JSON.stringify(before));
  assertAnalyticWaterObjectFastPath(before, "initial sphere");
  await page.click("[data-gosx-scene3d-control-toggle]");
  const menu = await readWaterControlState(page);
  assert(menu.open === "true", "water settings menu did not open: " + JSON.stringify(menu));
  assert(menu.controlsReady === "true", "water managed controls did not report ready: " + JSON.stringify(menu));
  const pool = await exerciseWaterPoolControls(page);
  const physics = await exerciseWaterPhysicsControls(page);
  const followCamera = await exerciseWaterFollowCamera(page);
  await page.selectOption("select[name=object]", "Rubber Duck");
  await waitForWaterCandidateProfile(page, "float-duck");
  const duck = await readWaterControlState(page);
  assert(duck.objectValue === "Rubber Duck", "water object control did not select Rubber Duck: " + JSON.stringify(duck));
  assert(duck.candidateProfile.indexOf("float-duck") >= 0, "duck object was not selected by water mesh texture pass: " + duck.candidateProfile);
  assert(duck.waterObjectTextureTargets === 3, "duck should render three projected optical targets: " + JSON.stringify(duck));
  assert(duck.waterObjectTexturePasses >= 3, "duck object texture passes did not run: " + JSON.stringify(duck));
  assert(duck.waterObjectTextureMeshPasses >= 3, "duck object texture mesh passes did not run: " + JSON.stringify(duck));
  assert(duck.waterObjectTextureMeshDrawCalls >= 3, "duck object texture mesh draw calls did not run: " + JSON.stringify(duck));
  assert(duck.waterObjectTextureSelenaDrawCalls >= 3, "duck Selena object texture draw calls did not run: " + JSON.stringify(duck));
  assert(duck.waterObjectTextureFallbackPasses === 0, "duck object texture fell back: " + JSON.stringify(duck));
  assert(duck.waterObjectTextureSelectedObjects > 0, "duck object texture did not select a mesh: " + JSON.stringify(duck));
  assert(duck.waterObjectTexturePixelBudget === 786432, "duck object texture pixel budget changed: " + JSON.stringify(duck));
  assert(duck.waterObjectTexturePixels === duck.waterObjectTextureTargets * duck.waterObjectTextureWidth * duck.waterObjectTextureHeight, "duck object texture pixel telemetry mismatch: " + JSON.stringify(duck));
  assert(duck.waterObjectTexturePixels <= duck.waterObjectTexturePixelBudget, "duck object texture pixel budget exceeded: " + JSON.stringify(duck));
  assert(duck.waterObjectShadowMeshPasses > 0, "duck object mesh shadow did not run: " + JSON.stringify(duck));
  assert(duck.waterObjectShadowFallbackPasses === 0, "duck object shadow fell back: " + JSON.stringify(duck));
  await page.selectOption("select[name=object]", "Sphere");
  await waitForAnalyticWaterObjectFastPath(page, "Sphere");
  const restored = await readWaterControlState(page);
  assert(restored.objectValue === "Sphere", "water object control did not restore Sphere: " + JSON.stringify(restored));
  assertAnalyticWaterObjectFastPath(restored, "restored sphere");
  return { before, menu, pool, physics, followCamera, duck, restored };
}

async function exerciseWaterPoolControls(page) {
  await setWaterControlValue(page, "poolShape", "Rounded Box");
  await setWaterControlValue(page, "poolWidth", "1.5");
  await setWaterControlValue(page, "poolHeight", "1.2");
  await setWaterControlValue(page, "poolLength", "1.75");
  await setWaterControlValue(page, "cornerRadius", "0.4");
  await waitForWaterControlState(page, (state) => {
    return state.poolShapeValue === "Rounded Box"
      && state.roundedOpen === "true"
      && state.waterStatePoolShape === "Rounded Box"
      && state.waterStateRoundedSystems > 0
      && Math.abs(state.formCornerRadius - 0.4) < 0.02
      && Math.abs(state.waterStateCornerRadius - 0.4) < 0.02;
  });
  const rounded = await readWaterControlState(page);
  assert(rounded.poolWidthValue === "1.5", "rounded pool width control did not stick: " + JSON.stringify(rounded));
  assert(rounded.poolHeightValue === "1.2", "rounded pool height control did not stick: " + JSON.stringify(rounded));
  assert(rounded.poolLengthValue === "1.75", "rounded pool length control did not stick: " + JSON.stringify(rounded));
  assert(Number(rounded.maxCornerRadius) >= 1.45, "rounded pool max corner radius did not follow width/length: " + JSON.stringify(rounded));
  await setWaterControlValue(page, "poolShape", "Box");
  await waitForWaterControlState(page, (state) => {
    return state.poolShapeValue === "Box"
      && state.roundedOpen === "false"
      && state.waterStatePoolShape === "Box"
      && state.waterStateRoundedSystems === 0
      && state.waterStateCornerRadius === 0;
  });
  return { rounded, restored: await readWaterControlState(page) };
}

async function exerciseWaterPhysicsControls(page) {
  await setWaterControlChecked(page, "gravity", true);
  await setWaterControlChecked(page, "densityEnabled", true);
  await setWaterControlValue(page, "density", "1.35");
  await waitForWaterControlState(page, (state) => {
    return state.gravityChecked
      && state.densityEnabledChecked
      && state.densityOpen === "true"
      && !state.densityDisabled
      && state.densityValue === "1.35";
  });
  const enabled = await readWaterControlState(page);
  await page.selectOption("select[name=object]", "None");
  await waitForWaterControlState(page, (state) => {
    return state.objectValue === "None"
      && state.physicsAvailable === "false"
      && state.gravityDisabled
      && state.densityEnabledDisabled
      && state.densityDisabled
      && state.fluidObject === "None";
  });
  const none = await readWaterControlState(page);
  assert(none.status === "None", "None selection did not update status: " + JSON.stringify(none));
  await page.selectOption("select[name=object]", "Sphere");
  await waitForAnalyticWaterObjectFastPath(page, "Sphere");
  await setWaterControlChecked(page, "gravity", false);
  await setWaterControlChecked(page, "densityEnabled", false);
  return { enabled, none, restored: await readWaterControlState(page) };
}

async function exerciseWaterFollowCamera(page) {
  const initial = await readWaterControlState(page);
  await setWaterControlChecked(page, "followCamera", true);
  await waitForWaterControlState(page, (state) => state.followCameraChecked);
  await page.waitForFunction(() => {
    const mount = document.querySelector("[data-gosx-scene3d-webgpu-water-systems]");
    if (!mount) return false;
    const x = Number(mount.getAttribute("data-gosx-scene3d-webgpu-water-light-dir-x") || 0);
    const y = Number(mount.getAttribute("data-gosx-scene3d-webgpu-water-light-dir-y") || 0);
    const z = Number(mount.getAttribute("data-gosx-scene3d-webgpu-water-light-dir-z") || 0);
    const defaultLen = Math.sqrt(2 * 2 + 2 * 2 + 1);
    return Math.abs(x - 2 / defaultLen) > 0.08
      || Math.abs(y - 2 / defaultLen) > 0.08
      || Math.abs(z + 1 / defaultLen) > 0.08;
  }, { timeout: 8000 });
  const followed = await readWaterControlState(page);
  const length = Math.hypot(followed.waterLightDirX, followed.waterLightDirY, followed.waterLightDirZ);
  assert(Math.abs(length - 1) < 0.02, "follow-camera light direction was not normalized: " + JSON.stringify(followed));
  await setWaterControlChecked(page, "followCamera", false);
  await waitForWaterControlState(page, (state) => !state.followCameraChecked);
  return { initial, followed, restored: await readWaterControlState(page) };
}

function assertAnalyticWaterObjectFastPath(state, label) {
  assert(state.waterObjectTexturePasses === 0, label + " should skip projected object texture passes: " + JSON.stringify(state));
  assert(state.waterObjectTextureTargets === 0, label + " should skip projected object texture targets: " + JSON.stringify(state));
  assert(state.waterObjectTextureMeshPasses === 0, label + " should skip projected object texture mesh passes: " + JSON.stringify(state));
  assert(state.waterObjectTextureMeshDrawCalls === 0, label + " should skip projected object texture mesh draw calls: " + JSON.stringify(state));
  assert(state.waterObjectTextureSelenaDrawCalls === 0, label + " should skip Selena object texture draw calls: " + JSON.stringify(state));
  assert(state.waterObjectTextureFallbackPasses === 0, label + " should not fall back for projected object textures: " + JSON.stringify(state));
  assert(state.waterObjectTextureSelectedObjects === 0, label + " should not select projected object meshes: " + JSON.stringify(state));
  assert(state.waterObjectShadowPasses === 0, label + " should skip projected object shadow passes: " + JSON.stringify(state));
  assert(state.waterObjectShadowMeshPasses === 0, label + " should skip projected object mesh shadows: " + JSON.stringify(state));
  assert(state.waterObjectShadowMeshDrawCalls === 0, label + " should skip projected object mesh shadow draw calls: " + JSON.stringify(state));
  assert(state.waterObjectShadowFallbackPasses === 0, label + " should not fall back for projected object shadows: " + JSON.stringify(state));
}

async function waitForAnalyticWaterObjectFastPath(page, objectValue) {
  await page.waitForFunction((expected) => {
    const mount = document.querySelector("[data-gosx-scene3d-webgpu-water-systems]");
    const form = document.querySelector("[data-gosx-scene3d-control-form=fluid-object]");
    const field = form && form.elements ? form.elements.object : null;
    const numberAttr = (name) => mount ? Number(mount.getAttribute("data-gosx-scene3d-webgpu-" + name) || 0) : -1;
    return !!(mount && field && field.value === expected)
      && numberAttr("water-object-texture-passes") === 0
      && numberAttr("water-object-texture-targets") === 0
      && numberAttr("water-object-texture-mesh-passes") === 0
      && numberAttr("water-object-texture-selected-objects") === 0
      && numberAttr("water-object-shadow-passes") === 0
      && numberAttr("water-object-shadow-mesh-passes") === 0;
  }, objectValue, { timeout: 8000 });
}

async function waitForWaterCandidateProfile(page, needle) {
  await page.waitForFunction((value) => {
    const mount = document.querySelector("[data-gosx-scene3d-webgpu-water-object-texture-candidate-profile]");
    return !!(mount && (mount.getAttribute("data-gosx-scene3d-webgpu-water-object-texture-candidate-profile") || "").indexOf(value) >= 0);
  }, needle, { timeout: 8000 });
}

async function waitForWaterControlState(page, predicate) {
  await page.waitForFunction((predicateSource) => {
    const fn = new Function("state", "return (" + predicateSource + ")(state);");
    const state = window.__gosxReadWaterControlProbeState && window.__gosxReadWaterControlProbeState();
    return !!(state && fn(state));
  }, String(predicate), { timeout: 8000 });
}

async function setWaterControlValue(page, name, value) {
  await page.evaluate(({ name, value }) => {
    const form = document.querySelector("[data-gosx-scene3d-control-form=fluid-object]");
    const field = form && form.elements ? form.elements[name] : null;
    if (!field) throw new Error("missing water control field " + name);
    field.value = String(value);
    field.dispatchEvent(new Event("input", { bubbles: true }));
    field.dispatchEvent(new Event("change", { bubbles: true }));
  }, { name, value });
}

async function setWaterControlChecked(page, name, checked) {
  await page.evaluate(({ name, checked }) => {
    const form = document.querySelector("[data-gosx-scene3d-control-form=fluid-object]");
    const field = form && form.elements ? form.elements[name] : null;
    if (!field) throw new Error("missing water control field " + name);
    field.checked = !!checked;
    field.dispatchEvent(new Event("input", { bubbles: true }));
    field.dispatchEvent(new Event("change", { bubbles: true }));
  }, { name, checked });
}

async function readWaterControlState(page) {
  return page.evaluate(() => {
    window.__gosxReadWaterControlProbeState = function() {
    const mount = document.querySelector("[data-gosx-scene3d-webgpu-water-systems]");
    const form = document.querySelector("[data-gosx-scene3d-control-form=fluid-object]");
    const numberAttr = (name) => mount ? Number(mount.getAttribute("data-gosx-scene3d-webgpu-" + name) || 0) : 0;
    const stringAttr = (name) => mount ? mount.getAttribute("data-gosx-scene3d-webgpu-" + name) || "" : "";
    const field = form && form.elements ? form.elements.object : null;
    const formField = (name) => form && form.elements ? form.elements[name] : null;
    const status = document.querySelector("[data-gosx-scene3d-control-status]");
    return {
      objectValue: field && field.value || "",
      status: status && status.textContent ? status.textContent.trim() : "",
      open: form ? form.getAttribute("data-gosx-scene3d-control-open") || "" : "",
      controlsReady: form ? form.dataset.gosxScene3dControlsReady || "" : "",
      physicsAvailable: form ? form.dataset.physicsAvailable || "" : "",
      densityOpen: form ? form.dataset.densityOpen || "" : "",
      roundedOpen: form ? form.dataset.roundedOpen || "" : "",
      fluidObject: form ? form.dataset.fluidObject || "" : "",
      maxCornerRadius: form ? form.dataset.maxCornerRadius || "" : "",
      formCornerRadius: form ? Number(form.dataset.cornerRadius || 0) : 0,
      poolShapeValue: formField("poolShape") && formField("poolShape").value || "",
      poolWidthValue: formField("poolWidth") && formField("poolWidth").value || "",
      poolHeightValue: formField("poolHeight") && formField("poolHeight").value || "",
      poolLengthValue: formField("poolLength") && formField("poolLength").value || "",
      densityValue: formField("density") && formField("density").value || "",
      gravityChecked: !!(formField("gravity") && formField("gravity").checked),
      densityEnabledChecked: !!(formField("densityEnabled") && formField("densityEnabled").checked),
      followCameraChecked: !!(formField("followCamera") && formField("followCamera").checked),
      gravityDisabled: !!(formField("gravity") && formField("gravity").disabled),
      densityEnabledDisabled: !!(formField("densityEnabled") && formField("densityEnabled").disabled),
      densityDisabled: !!(formField("density") && formField("density").disabled),
      waterStatePoolShape: mount ? mount.getAttribute("data-gosx-scene3d-water-state-pool-shape") || "" : "",
      waterStateCornerRadius: mount ? Number(mount.getAttribute("data-gosx-scene3d-water-state-corner-radius") || 0) : 0,
      waterStateRoundedSystems: mount ? Number(mount.getAttribute("data-gosx-scene3d-water-state-rounded-systems") || 0) : 0,
      waterLightDirX: numberAttr("water-light-dir-x"),
      waterLightDirY: numberAttr("water-light-dir-y"),
      waterLightDirZ: numberAttr("water-light-dir-z"),
      frameSeq: numberAttr("frame-seq"),
      candidateProfile: stringAttr("water-object-texture-candidate-profile"),
      waterObjectTexturePasses: numberAttr("water-object-texture-passes"),
      waterObjectTextureTargets: numberAttr("water-object-texture-targets"),
      waterObjectTexturePixels: numberAttr("water-object-texture-pixels"),
      waterObjectTextureWidth: numberAttr("water-object-texture-width"),
      waterObjectTextureHeight: numberAttr("water-object-texture-height"),
      waterObjectTexturePixelBudget: numberAttr("water-object-texture-pixel-budget"),
      waterObjectTextureMeshPasses: numberAttr("water-object-texture-mesh-passes"),
      waterObjectTextureMeshDrawCalls: numberAttr("water-object-texture-mesh-draw-calls"),
      waterObjectTextureSelenaDrawCalls: numberAttr("water-object-texture-selena-draw-calls"),
      waterObjectTextureFallbackPasses: numberAttr("water-object-texture-fallback-passes"),
      waterObjectTextureSelectedObjects: numberAttr("water-object-texture-selected-objects"),
      waterObjectShadowPasses: numberAttr("water-object-shadow-passes"),
      waterObjectShadowTexturePixels: numberAttr("water-object-shadow-texture-pixels"),
      waterObjectShadowMeshPasses: numberAttr("water-object-shadow-mesh-passes"),
      waterObjectShadowMeshDrawCalls: numberAttr("water-object-shadow-mesh-draw-calls"),
      waterObjectShadowFallbackPasses: numberAttr("water-object-shadow-fallback-passes"),
    };
    };
    return window.__gosxReadWaterControlProbeState();
  });
}

async function sampleWaterFramePerformance(page) {
  function readFrame() {
    const mount = document.querySelector("[data-gosx-scene3d-webgpu-frame-seq]");
    if (!mount) return { seq: 0, at: 0 };
    return {
      seq: Number(mount.getAttribute("data-gosx-scene3d-webgpu-frame-seq") || 0),
      at: Number(mount.getAttribute("data-gosx-scene3d-webgpu-frame-at") || 0),
    };
  }
  const start = await page.evaluate(readFrame);
  await page.waitForTimeout(1000);
  const end = await page.evaluate(readFrame);
  const frames = Math.max(0, end.seq - start.seq);
  const milliseconds = Math.max(0, end.at - start.at);
  return {
    startFrameSeq: start.seq,
    endFrameSeq: end.seq,
    frames,
    milliseconds,
    fps: milliseconds > 0 ? frames * 1000 / milliseconds : 0,
  };
}

function assertWaterTelemetry(browserReport) {
  const mount = browserReport.scene3dMount || {};
  const telemetry = browserReport.waterTelemetry || {};
  assert(mount.waterRenderer === "active", "common water renderer did not report active: " + JSON.stringify(mount));
  assert(mount.waterFrameSeq > 0, "common water presentation telemetry did not advance: " + JSON.stringify(mount));
  assert(mount.frameSeq > 0, "Scene3D WebGPU frame telemetry did not advance");
  assert(telemetry.waterSystems > 0, "water telemetry missing water systems");
  assert(telemetry.waterCells >= 36864, "water simulation has too few cells: " + telemetry.waterCells);
  assert(telemetry.waterComputeDispatches > 0, "water compute dispatches did not run");
  assert(telemetry.waterAuthoredComputeSystems > 0, "authored Elio compute systems did not activate");
  assert(telemetry.waterAuthoredComputeDispatches > 0, "authored Elio compute dispatches did not run");
  assert(telemetry.waterAuthoredComputeFallbacks === 0, "authored Elio compute fell back");
  const expectedLightLen = Math.sqrt(2 * 2 + 2 * 2 + 1);
  assert(Math.abs(telemetry.waterLightDirX - 2 / expectedLightLen) < 0.02, "water light X did not follow .gsx lightDirectionX: " + JSON.stringify(telemetry));
  assert(Math.abs(telemetry.waterLightDirY - 2 / expectedLightLen) < 0.02, "water light Y did not follow .gsx lightDirectionY: " + JSON.stringify(telemetry));
  assert(Math.abs(telemetry.waterLightDirZ + 1 / expectedLightLen) < 0.02, "water light Z did not follow .gsx lightDirectionZ: " + JSON.stringify(telemetry));
  assert(telemetry.waterCausticPasses > 0, "water caustics pass did not run");
  assert(telemetry.waterAuthoredCausticPasses > 0, "authored Selena caustics pass did not run");
  assert(telemetry.waterAuthoredCausticFallbacks === 0, "authored Selena caustics fell back");
  assert(telemetry.waterObjectTexturePasses === 0, "analytic sphere should skip projected object texture passes: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectTextureTargets === 0, "analytic sphere should skip projected object texture targets: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectTextureMeshPasses === 0, "analytic sphere should skip projected object texture mesh passes: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectTextureMeshDrawCalls === 0, "analytic sphere should skip projected object texture mesh draw calls: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectTextureSelenaDrawCalls === 0, "analytic sphere should skip Selena object texture draw calls: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectTextureFallbackPasses === 0, "analytic sphere should not fall back for projected object textures: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectTextureSelectedObjects === 0, "analytic sphere should not select projected object meshes: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectShadowPasses === 0, "analytic sphere should skip projected object shadow passes: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectShadowMeshPasses === 0, "analytic sphere should skip projected object mesh shadows: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectShadowMeshDrawCalls === 0, "analytic sphere should skip projected object mesh shadow draw calls: " + JSON.stringify(telemetry));
  assert(telemetry.waterAuthoredObjectMeshShadowPasses === 0, "analytic sphere should skip authored object mesh shadows: " + JSON.stringify(telemetry));
  assert(telemetry.waterAuthoredObjectMeshShadowFallbacks === 0, "analytic sphere should not fall back for object mesh shadows: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectShadowFallbackPasses === 0, "analytic sphere should not fall back for projected object shadows: " + JSON.stringify(telemetry));
  assert(telemetry.waterPoolTileTextureLoaded > 0, "pool tile texture did not load");
  assert(telemetry.waterPoolTileTextureFallbacks === 0, "pool tile texture fell back");
  assert(telemetry.waterAuthoredPoolPasses > 0, "authored Selena pool pass did not run");
  assert(telemetry.waterAuthoredPoolFallbacks === 0, "authored Selena pool pass fell back");
  assert(telemetry.waterDrawCalls > 0, "water surface draw calls did not run");
  assert(telemetry.waterSurfaceAboveDrawCalls > 0, "above-water surface draw did not run");
  assert(telemetry.waterSurfaceBelowDrawCalls > 0, "below-water surface draw did not run");
  assert(telemetry.waterAuthoredSurfaceSystems >= 2, "authored Selena above/below surface systems did not both run");
  assert(telemetry.waterAuthoredSurfaceDrawCalls >= 2, "authored Selena surface draw calls did not both run");
  assert(telemetry.waterAuthoredSurfaceFallbacks === 0, "authored Selena surface fell back");
  assert(telemetry.waterSkyCubeTextureLoaded > 0, "water sky cubemap did not load");
  assert(telemetry.waterSkyCubeTextureFallbacks === 0, "water sky cubemap fell back");
}

function assertWaterPerformance(browserReport) {
  const telemetry = browserReport.waterTelemetry || {};
  const perf = browserReport.performance || {};
  const minimumFPS = Number(process.env.GOSX_WATER_MIN_FPS || 50);
  assert(telemetry.waterCells === 36864, "water cell budget changed: " + telemetry.waterCells);
  assert(telemetry.waterVertices <= 240000, "water vertex budget exceeded: " + telemetry.waterVertices);
  assert(telemetry.waterComputeDispatches <= 8, "water compute dispatch budget exceeded: " + telemetry.waterComputeDispatches);
  assert(telemetry.waterCausticTexturePixels <= 262144, "caustics texture budget exceeded: " + telemetry.waterCausticTexturePixels);
  assert(telemetry.waterObjectTextureTargets === 0, "analytic sphere should not allocate projected object optical targets: " + telemetry.waterObjectTextureTargets);
  assert(telemetry.waterObjectTexturePixels === 0, "analytic sphere projected object optical pixels should be zero: " + telemetry.waterObjectTexturePixels);
  assert(telemetry.waterObjectTextureWidth === 0 && telemetry.waterObjectTextureHeight === 0, "analytic sphere projected object texture dimensions should be zero: " + JSON.stringify(telemetry));
  assert(telemetry.waterObjectTexturePixelBudget === 0, "analytic sphere projected object texture budget should be inactive: " + telemetry.waterObjectTexturePixelBudget);
  assert(telemetry.waterObjectTextureMeshDrawCalls === 0, "analytic sphere projected object draw-call budget should be zero: " + telemetry.waterObjectTextureMeshDrawCalls);
  assert(telemetry.waterObjectShadowTexturePixels === 0, "analytic sphere projected object shadow pixels should be zero: " + telemetry.waterObjectShadowTexturePixels);
  assert(telemetry.waterObjectShadowMeshDrawCalls === 0, "analytic sphere projected object shadow draw calls should be zero: " + telemetry.waterObjectShadowMeshDrawCalls);
  assert(telemetry.waterDrawCalls <= 2, "water surface draw-call budget exceeded: " + telemetry.waterDrawCalls);
  assert(telemetry.waterDrawVertices <= 800000, "water surface vertex budget exceeded: " + telemetry.waterDrawVertices);
  assert(perf.frames >= Math.floor(minimumFPS * 0.9), "water frame cadence too low: " + JSON.stringify(perf));
  assert(perf.fps >= minimumFPS, "water sampled fps below " + minimumFPS + ": " + JSON.stringify(perf));
  const pixels = browserReport.pixels || {};
  assert(pixels.quantizedColors >= 24, "water hardware image is visually flat: " + JSON.stringify(pixels));
  assert(pixels.luminanceRange >= 35, "water hardware image lacks tonal structure: " + JSON.stringify(pixels));
  assert(pixels.luminanceStdDev >= 10, "water hardware image resembles a blank fallback: " + JSON.stringify(pixels));
}

async function compositedPixelStats(page) {
  const canvas = page.locator("canvas[data-gosx-scene3d-canvas]").first();
  const png = await canvas.screenshot({ type: "png" });
  return page.evaluate(async (bytes) => {
    const bitmap = await createImageBitmap(new Blob([new Uint8Array(bytes)], { type: "image/png" }));
    const surface = new OffscreenCanvas(Math.min(160, bitmap.width), Math.min(100, bitmap.height));
    const ctx = surface.getContext("2d", { willReadFrequently: true });
    ctx.drawImage(bitmap, 0, 0, surface.width, surface.height);
    bitmap.close();
    const data = ctx.getImageData(0, 0, surface.width, surface.height).data;
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
      quantizedColors: colors.size,
      luminanceRange: Math.max(...luminances) - Math.min(...luminances),
      luminanceStdDev: Math.sqrt(variance),
    };
  }, [...png]);
}

function playwrightCoreURL() {
  const override = process.env.GOSX_PLAYWRIGHT_CORE;
  return pathToFileURL(override || DEFAULT_PLAYWRIGHT_CORE).href;
}

function browserExecutablePath() {
  if (process.env.GOSX_BROWSER_EXECUTABLE) {
    return process.env.GOSX_BROWSER_EXECUTABLE;
  }
  if (process.platform !== "win32") {
    return process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || "chromium";
  }
  for (const candidate of BROWSER_CANDIDATES) {
    if (fs.existsSync(candidate)) return candidate;
  }
  throw new Error(
    `no browser executable found; set GOSX_BROWSER_EXECUTABLE (checked ${BROWSER_CANDIDATES.join(", ")})`,
  );
}

function assert(condition, message) {
  if (!condition) {
    const error = new Error(message);
    error.report = report;
    throw error;
  }
}
