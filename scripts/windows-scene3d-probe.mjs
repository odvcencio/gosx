#!/usr/bin/env node
//
// windows-scene3d-probe.mjs -- the bridge between a real GPU and a sandbox.
//
// WHY THIS EXISTS
//
// Everyone who works on this renderer except the owner runs on a software
// rasterizer (SwiftShader / llvmpipe) or silently falls back to a 2D canvas.
// Nobody in that position can see what a real GPU draws. The cost has been
// concrete: a post-processing pass tuned across three sessions while producing
// zero pixels; three mesh planes invisible for two weeks; an accessibility
// auditor who saw a real defect, assumed it was a software-rasterizer artifact
// and did not report it; two deploys that shipped visibly broken pages through
// a green visual gate.
//
// This script runs on the owner's machine, against a real GPU, and produces
// ONE FOLDER that answers the questions a sandboxed agent cannot answer for
// itself. The owner runs one command, reads PASS or FAIL, and hands back the
// folder.
//
// HONEST ASSESSMENT OF THE PREVIOUS VERSION
//
// The prior script was a water-demo regression harness wearing a probe's name.
// It hardcoded /demos/water, and ~45 of its ~60 assertions were water-specific
// (caustics passes, pool tile textures, duck mesh selection, a 50fps floor). On
// any other URL it failed immediately on assertions that had nothing to do with
// the page under test. It also required --browser to run a browser at all, so
// the default invocation only fetched HTML and grepped the bundle text.
//
// That harness still has value, so it is preserved verbatim behind --water.
// The DEFAULT is now a general render-truth probe that works on any Scene3D
// page, which is what the failures above actually needed.
//
// USAGE (the common case has no flags):
//
//   node scripts/windows-scene3d-probe.mjs
//
// That probes https://m31labs.dev/ with Edge and writes ./scene3d-probe/.
// Everything else is optional:
//
//   --url <URL>            page to probe (default https://m31labs.dev/)
//   --browser <name>       edge | chrome | firefox   (default edge)
//   --out <dir>            output directory (default ./scene3d-probe)
//   --at <seconds,...>     extra capture offsets (default 8,10,12 -- the
//                          galaxy supernova window; outside it the scene is
//                          legitimately dark and a naive check misreads it)
//   --water                run the legacy water-demo regression harness
//
// COMPARING TWO BROWSERS
//
// Edge and Firefox do not share a WebGPU implementation. Edge uses Dawn, which
// translates WGSL through Tint. Firefox uses wgpu, which translates WGSL
// through naga. Selena validates its emitted WGSL with naga, so a shader can
// pass authoring-time validation and still hit a Tint bug in the browser where
// the defects were actually seen. Two runs with the same schema make that a
// mechanical diff instead of a judgement call:
//
//   node scripts/windows-scene3d-probe.mjs --browser edge    --out probe-edge
//   node scripts/windows-scene3d-probe.mjs --browser firefox --out probe-firefox
//   node scripts/windows-scene3d-probe.mjs --diff probe-edge probe-firefox
//
// OUTPUT (one directory, hand the whole thing back):
//   report.json          full machine-readable dump, stable schema
//   summary.txt          human-readable PASS/FAIL with the reasons
//   initial.png          first paint
//   settled.png          after the scene reports ready
//   at-<n>s.png          one per --at offset
//   console.log          console + page errors + failed requests

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

const SCHEMA_VERSION = 2;
const DEFAULT_URL = "https://m31labs.dev/";
const DEFAULT_OUT = "scene3d-probe";
const DEFAULT_OFFSETS = [8, 10, 12];
const DEFAULT_PLAYWRIGHT_CORE =
  "C:/Users/odvce/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/.pnpm/playwright-core@1.61.0/node_modules/playwright-core/index.mjs";

const BROWSER_CANDIDATES = {
  edge: [
    "C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
    "C:/Program Files/Microsoft/Edge/Application/msedge.exe",
  ],
  chrome: [
    "C:/Program Files/Google/Chrome/Application/chrome.exe",
    "C:/Program Files (x86)/Google/Chrome/Application/chrome.exe",
  ],
  firefox: [
    "C:/Program Files/Mozilla Firefox/firefox.exe",
    "C:/Program Files (x86)/Mozilla Firefox/firefox.exe",
  ],
};

const argv = process.argv.slice(2);

if (hasFlag("--diff")) {
  await runDiff();
} else if (hasFlag("--water")) {
  await runLegacyWaterHarness();
} else {
  await runRenderTruthProbe();
}

// ---------------------------------------------------------------------------
// Argument helpers
// ---------------------------------------------------------------------------

function hasFlag(name) {
  return argv.includes(name);
}

function readArg(name, fallback = "") {
  const prefix = `${name}=`;
  const inline = argv.find((arg) => arg.startsWith(prefix));
  if (inline) return inline.slice(prefix.length);
  const index = argv.indexOf(name);
  if (index >= 0 && index + 1 < argv.length) return argv[index + 1];
  return fallback;
}

function readOffsets() {
  const raw = readArg("--at", "").trim();
  if (!raw) return DEFAULT_OFFSETS;
  return raw
    .split(/[,;\s]+/)
    .map((part) => Number(part))
    .filter((value) => Number.isFinite(value) && value >= 0);
}

// ---------------------------------------------------------------------------
// The probe
// ---------------------------------------------------------------------------

async function runRenderTruthProbe() {
  const url = readArg("--url") || process.env.GOSX_SCENE3D_PROBE_URL || DEFAULT_URL;
  const browserName = (readArg("--browser") || process.env.GOSX_SCENE3D_PROBE_BROWSER || "edge").toLowerCase();
  const outDir = path.resolve(readArg("--out") || process.env.GOSX_SCENE3D_PROBE_OUT || DEFAULT_OUT);
  const offsets = readOffsets();

  fs.mkdirSync(outDir, { recursive: true });

  const report = {
    schemaVersion: SCHEMA_VERSION,
    startedAt: new Date().toISOString(),
    url,
    requestedBrowser: browserName,
    host: { platform: process.platform, release: os.release(), node: process.version, cpus: os.cpus().length },
    browser: {},
    gpuReport: null,
    samples: [],
    failures: [],
    warnings: [],
    pass: false,
  };

  const consoleLines = [];
  let browser = null;
  let context = null;
  let page = null;

  try {
    const launch = await launchBrowser(browserName);
    browser = launch.browser;
    report.browser = {
      requested: browserName,
      engine: launch.engine,
      executablePath: launch.executablePath,
      version: browser.version ? browser.version() : "",
      // Which WGSL translator this engine uses. Edge/Chrome -> Dawn/Tint,
      // Firefox -> wgpu/naga. Recorded up front so a diff between two runs
      // has the compiler identity in hand before anything else is compared.
      expectedWebGPUImplementation: launch.engine === "gecko" ? "wgpu" : "dawn",
    };

    context = await browser.newContext({ viewport: { width: 1600, height: 900 } });

    // Enable the render-truth diagnostics tier BEFORE any page script runs.
    // Production pays nothing for this telemetry; a probe must opt in.
    await context.addInitScript(() => {
      window.__gosx_scene3d_render_truth = true;
      window.__gosx_telemetry_config = Object.assign({}, window.__gosx_telemetry_config, {
        scene3dDiagnostics: true,
      });
    });

    page = await context.newPage();
    page.on("console", (message) => {
      consoleLines.push(`[${message.type()}] ${message.text()}`);
    });
    page.on("pageerror", (error) => {
      consoleLines.push(`[pageerror] ${error.message}`);
      report.failures.push(`page error: ${error.message}`);
    });
    page.on("requestfailed", (request) => {
      const failure = request.failure()?.errorText || "";
      consoleLines.push(`[requestfailed] ${request.url()} ${failure}`);
    });

    const response = await page.goto(url, { waitUntil: "domcontentloaded", timeout: 60000 });
    if (!response || !response.ok()) {
      report.failures.push(`navigation returned ${response ? response.status() : "no response"}`);
    }

    // --- initial: first paint, before the scene settles.
    report.samples.push(await capture(page, outDir, "initial", 0));

    // --- settled: the scene says it is ready, or 15s, whichever comes first.
    await page
      .waitForFunction(() => {
        const mount = document.querySelector("[data-gosx-scene3d-renderer]");
        if (!mount) return false;
        return mount.getAttribute("data-gosx-scene3d-ready") === "true"
          || Number(mount.getAttribute("data-gosx-scene3d-webgpu-frame-seq") || 0) > 5;
      }, { timeout: 15000 })
      .catch(() => report.warnings.push("scene did not report ready within 15s"));
    report.samples.push(await capture(page, outDir, "settled", 0));

    // --- caller-specified offsets, measured from navigation.
    // The galaxy supernova peaks at 8-12s and the scene is legitimately dark
    // outside that window; a probe that samples only at settle time reads a
    // correct dark frame as a failure and a broken bright frame as a pass.
    const navStart = Date.now();
    for (const seconds of offsets) {
      const waitMs = seconds * 1000 - (Date.now() - navStart);
      if (waitMs > 0) await page.waitForTimeout(waitMs);
      report.samples.push(await capture(page, outDir, `at-${seconds}s`, seconds));
    }

    report.gpuReport = await collectGPUReport(page);
    evaluateProbe(report);
  } catch (err) {
    report.failures.push(`probe error: ${err && err.message ? err.message : String(err)}`);
  } finally {
    await page?.close().catch(() => {});
    await context?.close().catch(() => {});
    await browser?.close().catch(() => {});
  }

  report.pass = report.failures.length === 0;
  report.finishedAt = new Date().toISOString();

  fs.writeFileSync(path.join(outDir, "report.json"), JSON.stringify(report, null, 2));
  fs.writeFileSync(path.join(outDir, "console.log"), consoleLines.join("\n"));
  const summary = renderSummary(report, outDir);
  fs.writeFileSync(path.join(outDir, "summary.txt"), summary);
  console.log(summary);
  process.exit(report.pass ? 0 : 1);
}

async function capture(page, outDir, label, offsetSeconds) {
  const telemetry = await readRenderTruth(page);
  const file = path.join(outDir, `${label}.png`);
  await page.screenshot({ path: file, fullPage: false }).catch(() => {});
  return {
    label,
    offsetSeconds,
    at: new Date().toISOString(),
    screenshot: path.basename(file),
    telemetry,
  };
}

// readRenderTruth pulls the ENTIRE render-truth surface out of the page in one
// evaluate. Everything here reports an observed GPU action or the specific
// reason none happened -- no configuration values, because configuration is
// what read healthy through every incident this probe exists to prevent.
async function readRenderTruth(page) {
  return page.evaluate(() => {
    const mount = document.querySelector("[data-gosx-scene3d-renderer]")
      || document.querySelector("[data-gosx-scene3d-backend]")
      || document.querySelector("[data-gosx-scene3d]");
    const canvas = document.querySelector("canvas[data-gosx-scene3d-canvas]") || document.querySelector("canvas");
    const attr = (name) => (mount ? mount.getAttribute(name) || "" : "");
    const num = (name) => Number(attr(name) || 0);

    let backendTruth = null;
    try {
      const raw = attr("data-gosx-scene3d-render-backend-truth");
      backendTruth = raw ? JSON.parse(raw) : null;
    } catch (_err) {
      backendTruth = null;
    }

    // Parse the encoded post chain back into structured records so a diff
    // between two browsers compares fields, not strings.
    const chainRaw = attr("data-gosx-scene3d-render-post-chain");
    const postChain = chainRaw
      ? chainRaw.split("|").map((entry) => {
          const parts = entry.split(":");
          return {
            index: Number(parts[0] || 0),
            effect: parts[1] || "",
            pipeline: parts[2] || "",
            dispatched: Number(parts[3] || 0),
          };
        })
      : [];

    const events = (attr("data-gosx-scene3d-render-events") || "")
      .split("|")
      .filter(Boolean)
      .map((entry) => {
        const [at, kind, ...rest] = entry.split(":");
        return { at: Number(at || 0), kind: kind || "", detail: rest.join(":") };
      });

    return {
      mountFound: !!mount,
      // --- backend truth ---------------------------------------------------
      backend: attr("data-gosx-scene3d-backend") || attr("data-gosx-scene3d-renderer"),
      renderer: attr("data-gosx-scene3d-renderer"),
      fallbackReason: attr("data-gosx-scene3d-renderer-fallback"),
      dropped: attr("data-gosx-scene3d-dropped"),
      gpu: attr("data-gosx-scene3d-render-gpu") === "true",
      implementation: attr("data-gosx-scene3d-render-implementation"),
      backendTruth,
      // --- post-FX truth ---------------------------------------------------
      postChain,
      postAuthored: num("data-gosx-scene3d-render-post-authored"),
      postDispatched: num("data-gosx-scene3d-render-post-dispatched"),
      postDead: num("data-gosx-scene3d-render-post-dead"),
      postFailed: num("data-gosx-scene3d-render-post-failed"),
      postPending: num("data-gosx-scene3d-render-post-pending"),
      // The OLD attributes, kept alongside so a reader can see for themselves
      // that they report healthy while postDead > 0.
      legacyPostFX: attr("data-gosx-scene3d-postfx"),
      legacyPostEffects: num("data-gosx-scene3d-webgpu-post-effects"),
      // --- per-object truth ------------------------------------------------
      meshSubmitted: num("data-gosx-scene3d-render-mesh-submitted"),
      meshDrawn: num("data-gosx-scene3d-render-mesh-drawn"),
      meshViewCulled: num("data-gosx-scene3d-render-mesh-view-culled"),
      meshUndrawable: num("data-gosx-scene3d-render-mesh-undrawable"),
      pointsSubmitted: num("data-gosx-scene3d-render-points-submitted"),
      pointsDrawn: num("data-gosx-scene3d-render-points-drawn"),
      pointInstancesSubmitted: num("data-gosx-scene3d-render-point-instances-submitted"),
      pointInstancesDrawn: num("data-gosx-scene3d-render-point-instances-drawn"),
      legacyMeshObjects: num("data-gosx-scene3d-webgpu-mesh-objects"),
      // --- reserved uniforms -----------------------------------------------
      uniformTime: num("data-gosx-scene3d-render-uniform-time"),
      uniformTimeAdvancing: attr("data-gosx-scene3d-render-uniform-time-advancing") === "1",
      // --- compiler + lifecycle --------------------------------------------
      shaderMessages: num("data-gosx-scene3d-render-shader-messages"),
      shaderErrors: num("data-gosx-scene3d-render-shader-errors"),
      events,
      latches: attr("data-gosx-scene3d-render-latches"),
      staleLatches: num("data-gosx-scene3d-render-stale-latches"),
      // --- frame liveness ---------------------------------------------------
      frameSeq: num("data-gosx-scene3d-webgpu-frame-seq"),
      lastError: attr("data-gosx-scene3d-webgpu-last-error"),
      canvas: canvas
        ? { width: canvas.width, height: canvas.height, clientWidth: Math.round(canvas.getBoundingClientRect().width), clientHeight: Math.round(canvas.getBoundingClientRect().height) }
        : null,
    };
  });
}

// collectGPUReport asks the BROWSER what hardware and what WGSL implementation
// it has, independent of anything gosx published. If the two disagree, that
// disagreement is itself the finding.
async function collectGPUReport(page) {
  return page.evaluate(async () => {
    const out = { webgpu: null, webgl: null, userAgent: navigator.userAgent };

    if (navigator.gpu && typeof navigator.gpu.requestAdapter === "function") {
      try {
        const adapter = await navigator.gpu.requestAdapter({ powerPreference: "high-performance" });
        if (adapter) {
          let info = {};
          try {
            info = adapter.info || (adapter.requestAdapterInfo ? await adapter.requestAdapterInfo() : {}) || {};
          } catch (_err) {
            info = {};
          }
          out.webgpu = {
            available: true,
            vendor: info.vendor || "",
            architecture: info.architecture || "",
            device: info.device || "",
            description: info.description || "",
            // isFallbackAdapter true means a SOFTWARE adapter -- exactly the
            // situation that makes a sandboxed agent's observations worthless.
            isFallbackAdapter: !!adapter.isFallbackAdapter,
            features: [...(adapter.features || [])],
          };
        } else {
          out.webgpu = { available: false, reason: "requestAdapter returned null" };
        }
      } catch (err) {
        out.webgpu = { available: false, reason: String(err && err.message ? err.message : err) };
      }
    } else {
      out.webgpu = { available: false, reason: "navigator.gpu missing" };
    }

    try {
      const canvas = document.createElement("canvas");
      const gl = canvas.getContext("webgl2") || canvas.getContext("webgl");
      if (gl) {
        const dbg = gl.getExtension("WEBGL_debug_renderer_info");
        out.webgl = {
          available: true,
          version: gl.getParameter(gl.VERSION),
          vendor: dbg ? gl.getParameter(dbg.UNMASKED_VENDOR_WEBGL) : gl.getParameter(gl.VENDOR),
          renderer: dbg ? gl.getParameter(dbg.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER),
        };
      } else {
        out.webgl = { available: false };
      }
    } catch (err) {
      out.webgl = { available: false, reason: String(err && err.message ? err.message : err) };
    }

    return out;
  });
}

// evaluateProbe turns the dump into PASS or FAIL. Every check below is one of
// today's failures, expressed as an assertion that would have caught it.
function evaluateProbe(report) {
  const settled = report.samples.find((sample) => sample.label === "settled") || report.samples[report.samples.length - 1];
  const telemetry = settled && settled.telemetry;
  if (!telemetry || !telemetry.mountFound) {
    report.failures.push("no Scene3D mount found on the page");
    return;
  }

  // 1. Software rendering makes every other observation meaningless. Fail
  //    loudly rather than producing a confident report about a lie.
  const gpu = report.gpuReport && report.gpuReport.webgpu;
  const webgl = report.gpuReport && report.gpuReport.webgl;
  const softwareSignals = /swiftshader|llvmpipe|software|basic render|microsoft basic/i;
  if (gpu && gpu.isFallbackAdapter) {
    report.failures.push("WebGPU adapter is a FALLBACK (software) adapter -- this run cannot speak for real hardware");
  }
  if (webgl && webgl.renderer && softwareSignals.test(webgl.renderer)) {
    report.failures.push(`WebGL renderer is software: ${webgl.renderer}`);
  }

  // 2. Backend truth. A Canvas2D mount runs no shader at all, which is how a
  //    dead post pass shipped behind a green gate for a week.
  if (!telemetry.gpu) {
    report.failures.push(`backend is not GPU-backed: backend=${telemetry.backend || "none"} fallback=${telemetry.fallbackReason || "none"}`);
  }

  // 3. Post-FX truth. This is the check that would have found the liquid-glass
  //    pass in one glance instead of three sessions.
  if (telemetry.postDead > 0) {
    const dead = telemetry.postChain.filter((entry) => entry.dispatched === 0);
    report.failures.push(
      `${telemetry.postDead} authored post effect(s) never dispatched: ${dead.map((d) => `${d.effect}(${d.pipeline})`).join(", ")}`
    );
  }
  if (telemetry.postFailed > 0) {
    report.failures.push(`${telemetry.postFailed} post effect(s) failed shader compilation -- see shader-* entries in the event journal`);
  }

  // 4. Per-object truth. "3 in the bundle, 0 on screen" was invisible for two
  //    weeks because only the bundle count was published.
  if (telemetry.meshSubmitted > 0 && telemetry.meshDrawn === 0) {
    report.failures.push(
      `all ${telemetry.meshSubmitted} mesh object(s) submitted, none drawn (viewCulled=${telemetry.meshViewCulled}, undrawable=${telemetry.meshUndrawable})`
    );
  }
  if (telemetry.pointsSubmitted > 0 && telemetry.pointsDrawn === 0) {
    report.failures.push(`all ${telemetry.pointsSubmitted} point entr(ies) submitted, none drawn`);
  }

  // 5. Reserved uniforms. A `time` uniform stuck at 0 renders a static frame
  //    and reads healthy on every other attribute.
  const animated = report.samples.filter((sample) => sample.telemetry && sample.telemetry.uniformTime > 0);
  if (report.samples.length > 1 && animated.length === 0) {
    report.warnings.push("reserved `time` uniform never left 0 -- time-driven shaders render a static frame");
  }

  // 6. Shader compilation. A Tint/naga disagreement surfaces here as text.
  if (telemetry.shaderErrors > 0) {
    report.failures.push(`${telemetry.shaderErrors} shader compilation error(s) reported by this browser's WGSL compiler`);
  }
  if (telemetry.shaderMessages > telemetry.shaderErrors) {
    report.warnings.push(`${telemetry.shaderMessages - telemetry.shaderErrors} shader warning(s) from this browser's WGSL compiler`);
  }

  // 7. Device loss and latched guards.
  const lossEvents = report.samples
    .flatMap((sample) => (sample.telemetry && sample.telemetry.events) || [])
    .filter((event) => event.kind === "device-lost");
  if (lossEvents.length > 0) {
    report.failures.push(`WebGPU device was lost during the run: ${lossEvents.map((e) => e.detail).join("; ")}`);
  }
  if (telemetry.staleLatches > 0) {
    report.failures.push(`${telemetry.staleLatches} host-page guard(s) latched on a backend that is no longer running: ${telemetry.latches}`);
  }
  if (telemetry.lastError) {
    report.failures.push(`renderer reported a frame error: ${telemetry.lastError}`);
  }
}

function renderSummary(report, outDir) {
  const lines = [];
  lines.push(report.pass ? "RESULT: PASS" : "RESULT: FAIL");
  lines.push(`url:      ${report.url}`);
  lines.push(`browser:  ${report.browser.requested} (${report.browser.engine || "?"}) ${report.browser.version || ""}`);
  lines.push(`expects:  WebGPU via ${report.browser.expectedWebGPUImplementation || "?"}`);
  const gpu = report.gpuReport && report.gpuReport.webgpu;
  const webgl = report.gpuReport && report.gpuReport.webgl;
  if (gpu && gpu.available) {
    lines.push(`adapter:  ${[gpu.vendor, gpu.architecture, gpu.device, gpu.description].filter(Boolean).join(" ") || "(no info)"}${gpu.isFallbackAdapter ? "  [FALLBACK/SOFTWARE]" : ""}`);
  } else {
    lines.push(`adapter:  WebGPU unavailable (${(gpu && gpu.reason) || "?"})`);
  }
  if (webgl && webgl.available) lines.push(`webgl:    ${webgl.renderer}`);

  const settled = report.samples.find((s) => s.label === "settled");
  const t = settled && settled.telemetry;
  if (t) {
    lines.push("");
    lines.push("render truth at settle:");
    lines.push(`  backend        ${t.backend || "?"}${t.fallbackReason ? ` (fallback: ${t.fallbackReason})` : ""}`);
    lines.push(`  implementation ${t.implementation || "?"}`);
    lines.push(`  post effects   authored=${t.postAuthored} dispatched=${t.postDispatched} dead=${t.postDead} failed=${t.postFailed} pending=${t.postPending}`);
    if (t.postChain.length) {
      for (const entry of t.postChain) {
        lines.push(`    [${entry.index}] ${entry.effect}  pipeline=${entry.pipeline}  dispatched=${entry.dispatched}`);
      }
    }
    lines.push(`  meshes         submitted=${t.meshSubmitted} drawn=${t.meshDrawn} viewCulled=${t.meshViewCulled} undrawable=${t.meshUndrawable}`);
    lines.push(`  points         submitted=${t.pointsSubmitted} drawn=${t.pointsDrawn} instances=${t.pointInstancesDrawn}/${t.pointInstancesSubmitted}`);
    lines.push(`  uniform time   ${t.uniformTime} (advancing=${t.uniformTimeAdvancing})`);
    lines.push(`  shader msgs    ${t.shaderMessages} (${t.shaderErrors} errors)`);
    if (t.events.length) {
      lines.push("  timeline:");
      for (const event of t.events) lines.push(`    +${event.at}ms  ${event.kind}  ${event.detail}`);
    }
  }

  if (report.failures.length) {
    lines.push("");
    lines.push("FAILURES:");
    for (const failure of report.failures) lines.push(`  - ${failure}`);
  }
  if (report.warnings.length) {
    lines.push("");
    lines.push("warnings:");
    for (const warning of report.warnings) lines.push(`  - ${warning}`);
  }
  lines.push("");
  lines.push(`Full dump and screenshots: ${outDir}`);
  lines.push("Hand back the whole folder.");
  return lines.join("\n");
}

// ---------------------------------------------------------------------------
// Browser launch
// ---------------------------------------------------------------------------

async function launchBrowser(name) {
  const playwright = await import(playwrightCoreURL());
  if (name === "firefox") {
    // Playwright's bundled Firefox is used when present; a system Firefox is
    // NOT interchangeable (playwright patches its remote protocol), so the
    // executablePath override is deliberately not applied here.
    const browser = await playwright.firefox.launch({ headless: true });
    return { browser, engine: "gecko", executablePath: "(playwright firefox)" };
  }
  const executablePath = browserExecutablePath(name);
  const browser = await playwright.chromium.launch({
    executablePath,
    headless: true,
    args: [
      // Real hardware, no software substitute. If these flags cannot produce a
      // hardware adapter the probe FAILS rather than quietly reporting on
      // SwiftShader -- an honest failure beats a confident lie.
      "--enable-unsafe-webgpu",
      "--ignore-gpu-blocklist",
      "--enable-features=Vulkan",
    ],
  });
  return { browser, engine: name === "edge" ? "blink-edge" : "blink", executablePath };
}

function browserExecutablePath(name) {
  if (process.env.GOSX_BROWSER_EXECUTABLE) return process.env.GOSX_BROWSER_EXECUTABLE;
  const candidates = BROWSER_CANDIDATES[name] || BROWSER_CANDIDATES.edge;
  for (const candidate of candidates) {
    if (fs.existsSync(candidate)) return candidate;
  }
  if (process.platform !== "win32") {
    return process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE || "chromium";
  }
  throw new Error(`no ${name} executable found; set GOSX_BROWSER_EXECUTABLE (checked ${candidates.join(", ")})`);
}

function playwrightCoreURL() {
  const override = process.env.GOSX_PLAYWRIGHT_CORE;
  return pathToFileURL(override || DEFAULT_PLAYWRIGHT_CORE).href;
}

// ---------------------------------------------------------------------------
// Diff mode
// ---------------------------------------------------------------------------

// runDiff compares two probe folders field by field. Two browsers that both
// "support WebGPU" run two different WGSL translators; the interesting output
// is what differs, not what either says on its own.
async function runDiff() {
  const index = argv.indexOf("--diff");
  const left = argv[index + 1];
  const right = argv[index + 2];
  if (!left || !right) {
    console.error("usage: --diff <dirA> <dirB>");
    process.exit(2);
  }
  const a = JSON.parse(fs.readFileSync(path.join(path.resolve(left), "report.json"), "utf8"));
  const b = JSON.parse(fs.readFileSync(path.join(path.resolve(right), "report.json"), "utf8"));

  const lines = [`diff: ${left} vs ${right}`, ""];
  lines.push(`browser:        ${a.browser.requested} vs ${b.browser.requested}`);
  lines.push(`implementation: ${a.browser.expectedWebGPUImplementation} vs ${b.browser.expectedWebGPUImplementation}`);
  lines.push(`result:         ${a.pass ? "PASS" : "FAIL"} vs ${b.pass ? "PASS" : "FAIL"}`);
  lines.push("");

  const fields = [
    "backend", "fallbackReason", "implementation", "gpu",
    "postAuthored", "postDispatched", "postDead", "postFailed", "postPending",
    "meshSubmitted", "meshDrawn", "meshViewCulled", "meshUndrawable",
    "pointsSubmitted", "pointsDrawn", "pointInstancesDrawn",
    "uniformTimeAdvancing", "shaderMessages", "shaderErrors", "staleLatches", "lastError",
  ];
  for (const sample of a.samples) {
    const other = b.samples.find((s) => s.label === sample.label);
    if (!other) continue;
    const diffs = fields
      .filter((field) => JSON.stringify(sample.telemetry?.[field]) !== JSON.stringify(other.telemetry?.[field]))
      .map((field) => `    ${field}: ${JSON.stringify(sample.telemetry?.[field])} vs ${JSON.stringify(other.telemetry?.[field])}`);
    lines.push(`  ${sample.label}: ${diffs.length ? "" : "identical"}`);
    lines.push(...diffs);
  }
  console.log(lines.join("\n"));
}

// ---------------------------------------------------------------------------
// Legacy water harness (--water)
// ---------------------------------------------------------------------------

// The previous contents of this file, unchanged in behaviour, reachable via
// --water. It is a valuable regression harness for /demos/water and a poor
// general probe; keeping the two separate is the whole point of the split.
async function runLegacyWaterHarness() {
  const target = process.env.GOSX_SCENE3D_PROBE_WATER_SCRIPT
    || path.join(path.dirname(fileURLToPath(import.meta.url)), "windows-water-probe.mjs");
  if (!fs.existsSync(target)) {
    console.error(`water harness not found at ${target}`);
    process.exit(2);
  }
  process.argv = [process.argv[0], target, ...argv.filter((arg) => arg !== "--water")];
  await import(pathToFileURL(target).href);
}
