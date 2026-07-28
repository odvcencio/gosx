#!/usr/bin/env node

import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { pathToFileURL } from "node:url";

const runtimeProcess = typeof process === "object" && process
  ? process
  : {
      env: {},
      argv: [],
      platform: "unknown",
      arch: "unknown",
      version: "unknown",
      exitCode: 0,
    };

export const WATER_PROFILES = ["hero", "balanced", "battery"];
export const WATER_WORKLOADS = [
  { id: "sphere", label: "Sphere", kind: "analytic" },
  { id: "duck", label: "Rubber Duck", kind: "gltf" },
];

const DEFAULT_BUDGET = "perf/budgets/water-profile-evidence.json";
const DEFAULT_SCHEMA = "perf/water-profile-evidence.schema.json";
const DEFAULT_OUTPUT = "build/water-profile-evidence";
const DEFAULT_VIEWPORT = { width: 1440, height: 900 };

export function parseArgs(argv) {
  const options = {
    url: runtimeProcess.env.GOSX_WATER_EVIDENCE_URL || "http://127.0.0.1:3000/demos/water",
    outDir: runtimeProcess.env.GOSX_WATER_EVIDENCE_OUT || DEFAULT_OUTPUT,
    budgetPath: runtimeProcess.env.GOSX_WATER_EVIDENCE_BUDGET || DEFAULT_BUDGET,
    schemaPath: runtimeProcess.env.GOSX_WATER_EVIDENCE_SCHEMA || DEFAULT_SCHEMA,
    playwrightModule: runtimeProcess.env.GOSX_PLAYWRIGHT_MODULE || "",
    browserExecutable: runtimeProcess.env.GOSX_BROWSER_EXECUTABLE || "",
    environment: runtimeProcess.env.GOSX_WATER_EVIDENCE_ENVIRONMENT || "unspecified",
    samples: positiveInteger(runtimeProcess.env.GOSX_WATER_EVIDENCE_SAMPLES, 120),
    warmupFrames: positiveInteger(runtimeProcess.env.GOSX_WATER_EVIDENCE_WARMUP_FRAMES, 24),
    timeoutMS: positiveInteger(runtimeProcess.env.GOSX_WATER_EVIDENCE_TIMEOUT_MS, 45000),
    width: positiveInteger(runtimeProcess.env.GOSX_WATER_EVIDENCE_WIDTH, DEFAULT_VIEWPORT.width),
    height: positiveInteger(runtimeProcess.env.GOSX_WATER_EVIDENCE_HEIGHT, DEFAULT_VIEWPORT.height),
    headed: false,
    enforceHardware: false,
    checkConfig: false,
    help: false,
  };

  for (let index = 0; index < argv.length; index++) {
    const argument = argv[index];
    const [name, inline] = splitArgument(argument);
    const value = () => inline ?? argv[++index];
    switch (name) {
      case "--url": options.url = value(); break;
      case "--out-dir": options.outDir = value(); break;
      case "--budget": options.budgetPath = value(); break;
      case "--schema": options.schemaPath = value(); break;
      case "--playwright-module": options.playwrightModule = value(); break;
      case "--browser-executable": options.browserExecutable = value(); break;
      case "--environment": options.environment = value(); break;
      case "--samples": options.samples = positiveInteger(value(), options.samples); break;
      case "--warmup-frames": options.warmupFrames = positiveInteger(value(), options.warmupFrames); break;
      case "--timeout-ms": options.timeoutMS = positiveInteger(value(), options.timeoutMS); break;
      case "--width": options.width = positiveInteger(value(), options.width); break;
      case "--height": options.height = positiveInteger(value(), options.height); break;
      case "--headed": options.headed = true; break;
      case "--enforce-hardware": options.enforceHardware = true; break;
      case "--check-config": options.checkConfig = true; break;
      case "--help":
      case "-h": options.help = true; break;
      default:
        throw new Error(`unknown argument: ${argument}`);
    }
  }

  if (!["unspecified", "hardware", "software-raster"].includes(options.environment)) {
    throw new Error("--environment must be unspecified, hardware, or software-raster");
  }
  return options;
}

export function percentile(sorted, fraction) {
  if (!Array.isArray(sorted) || sorted.length === 0) return 0;
  const index = Math.max(0, Math.min(sorted.length - 1, Math.ceil(sorted.length * fraction) - 1));
  return sorted[index];
}

export function summarizeFrameIntervals(intervals) {
  const finite = intervals.filter((value) => Number.isFinite(value) && value >= 0);
  if (finite.length === 0) {
    return { samples: 0, meanMS: 0, p50MS: 0, p95MS: 0, p99MS: 0, maxMS: 0, fps: 0 };
  }
  const sorted = [...finite].sort((left, right) => left - right);
  const meanMS = finite.reduce((sum, value) => sum + value, 0) / finite.length;
  return {
    samples: finite.length,
    meanMS: round(meanMS),
    p50MS: round(percentile(sorted, 0.50)),
    p95MS: round(percentile(sorted, 0.95)),
    p99MS: round(percentile(sorted, 0.99)),
    maxMS: round(sorted[sorted.length - 1]),
    fps: meanMS > 0 ? round(1000 / meanMS) : 0,
  };
}

export function validateBudget(budget) {
  const failures = [];
  if (!budget || budget.schemaVersion !== 1) failures.push("budget.schemaVersion must be 1");
  if (!budget || !budget.profiles || typeof budget.profiles !== "object") {
    failures.push("budget.profiles is required");
    return failures;
  }
  for (const profile of WATER_PROFILES) {
    const value = budget.profiles[profile];
    if (!value) {
      failures.push(`budget profile ${profile} is missing`);
      continue;
    }
    for (const field of monotonicFields()) {
      if (!Number.isFinite(Number(value[field]))) {
        failures.push(`budget profile ${profile}.${field} must be numeric`);
      }
    }
    if (!value.features || typeof value.features !== "object") {
      failures.push(`budget profile ${profile}.features is required`);
    }
  }
  failures.push(...validateMonotonicProfiles(budget.profiles, "budget"));
  return failures;
}

export function validateMonotonicProfiles(profiles, label = "profiles") {
  const failures = [];
  const ordered = WATER_PROFILES.map((name) => [name, profiles[name]]);
  for (const field of monotonicFields()) {
    for (let index = 1; index < ordered.length; index++) {
      const [higherName, higher] = ordered[index - 1];
      const [lowerName, lower] = ordered[index];
      if (!higher || !lower) continue;
      if (Number(higher[field]) < Number(lower[field])) {
        failures.push(`${label} is not monotonic: ${higherName}.${field} < ${lowerName}.${field}`);
      }
    }
  }
  for (let index = 1; index < ordered.length; index++) {
    const [higherName, higher] = ordered[index - 1];
    const [lowerName, lower] = ordered[index];
    if (!higher || !lower) continue;
    if (Number(higher.expensivePassCadence) > Number(lower.expensivePassCadence)) {
      failures.push(`${label} is not monotonic: ${higherName}.expensivePassCadence > ${lowerName}.expensivePassCadence`);
    }
    const higherMSAA = Math.max(1, Number(higher.msaaSamples) || 0);
    const lowerMSAA = Math.max(1, Number(lower.msaaSamples) || 0);
    if (higherMSAA < lowerMSAA) {
      failures.push(`${label} is not monotonic: ${higherName}.msaaSamples < ${lowerName}.msaaSamples`);
    }
  }
  return failures;
}

export function validateEvidence(evidence, budget, options = {}) {
  const failures = [...validateBudget(budget)];
  const warnings = [];
  const cases = Array.isArray(evidence?.cases) ? evidence.cases : [];

  for (const profile of WATER_PROFILES) {
    const profileCases = cases.filter((entry) => entry.profile === profile);
    for (const workload of WATER_WORKLOADS) {
      if (!profileCases.some((entry) => entry.workload.id === workload.id)) {
        failures.push(`missing ${profile}/${workload.id} evidence`);
      }
    }
    const first = profileCases[0];
    if (!first) continue;
    const expected = budget.profiles[profile];
    const authored = first.authored;
    for (const field of monotonicFields()) {
      if (Number(authored[field]) !== Number(expected[field])) {
        failures.push(`${profile} authored ${field}=${authored[field]}, expected ${expected[field]}`);
      }
    }
    if (Number(authored.expensivePassCadence) !== Number(expected.expensivePassCadence)) {
      failures.push(`${profile} authored expensivePassCadence drifted`);
    }
    if (Math.max(1, Number(authored.msaaSamples) || 0) !== Math.max(1, Number(expected.msaaSamples) || 0)) {
      failures.push(`${profile} authored msaaSamples drifted`);
    }
    for (const [feature, expectedValue] of Object.entries(expected.features || {})) {
      if (authored.features[feature] !== expectedValue) {
        failures.push(`${profile} feature ${feature}=${authored.features[feature]}, expected ${expectedValue}`);
      }
    }

    for (const entry of profileCases) {
      if (entry.selectedProfile !== profile) {
        failures.push(`${profile}/${entry.workload.id} selected profile is ${entry.selectedProfile || "unknown"}`);
      }
      if (entry.backend.waterRenderer !== "active") {
        failures.push(`${profile}/${entry.workload.id} water renderer is ${entry.backend.waterRenderer || "missing"}`);
      }
      if (!["webgpu", "webgl"].includes(entry.backend.backend)) {
        failures.push(`${profile}/${entry.workload.id} has no real GPU backend`);
      }
      if (entry.shaderDiagnostics.errors > 0) {
        failures.push(`${profile}/${entry.workload.id} published ${entry.shaderDiagnostics.errors} shader errors`);
      }
      if (entry.advance.frame <= 0 || entry.advance.simulation <= 0) {
        failures.push(`${profile}/${entry.workload.id} did not advance frame and simulation counters`);
      }
      if (entry.featureState.activeObject !== entry.workload.label) {
        failures.push(
          `${profile}/${entry.workload.id} active object is ${entry.featureState.activeObject || "missing"}`,
        );
      }
      if (entry.workload.id === "sphere" && entry.network.duckAssetRequests.length > 0) {
        failures.push(`${profile}/sphere loaded Duck assets before the glTF workload`);
      }
      if (entry.workload.id === "duck" && entry.network.duckAssetRequests.length < 3) {
        failures.push(`${profile}/duck did not load its glTF, buffer, and texture evidence`);
      }
      if (entry.errors.console.length || entry.errors.page.length || entry.errors.requests.length || entry.errors.http.length) {
        failures.push(`${profile}/${entry.workload.id} observed browser errors`);
      }
      if (!entry.screenshot || !entry.screenshot.path) {
        failures.push(`${profile}/${entry.workload.id} screenshot path is missing`);
      }
      failures.push(...validateNoUpgrade(entry, expected));
    }
  }

  const observedProfiles = {};
  for (const profile of WATER_PROFILES) {
    const sphere = cases.find((entry) => entry.profile === profile && entry.workload.id === "sphere");
    if (sphere) observedProfiles[profile] = sphere.authored;
  }
  failures.push(...validateMonotonicProfiles(observedProfiles, "authored evidence"));

  const environment = options.environment || evidence.environment?.classification || "unspecified";
  const enforceHardware = options.enforceHardware === true;
  if (enforceHardware && environment === "hardware") {
    for (const entry of cases) {
      const thresholds = budget.hardwareThresholds?.[entry.backend.backend]?.[entry.workload.id];
      if (!thresholds) {
        warnings.push(`no hardware threshold for ${entry.backend.backend}/${entry.workload.id}`);
        continue;
      }
      const raf = entry.raf;
      if (Number.isFinite(thresholds.fpsMin) && raf.fps < thresholds.fpsMin) {
        failures.push(`${entry.profile}/${entry.workload.id} fps ${raf.fps} < ${thresholds.fpsMin}`);
      }
      for (const [field, thresholdField] of [["p95MS", "p95MaxMS"], ["p99MS", "p99MaxMS"], ["maxMS", "maxMaxMS"]]) {
        if (Number.isFinite(thresholds[thresholdField]) && raf[field] > thresholds[thresholdField]) {
          failures.push(`${entry.profile}/${entry.workload.id} ${field} ${raf[field]} > ${thresholds[thresholdField]}`);
        }
      }
    }
  } else if (enforceHardware) {
    warnings.push(`hardware thresholds skipped for ${environment}; use --environment hardware only on a real GPU`);
  }

  for (const entry of cases.filter((value) => value.workload.id === "duck")) {
    if (entry.backend.backend === "webgpu") {
      if (!(entry.objectTarget.actualTargets > 0 && entry.objectTarget.actualPixels > 0)) {
        failures.push(`${entry.profile}/duck did not publish WebGPU object-target work`);
      }
    } else if (!entry.telemetryAvailability.backendPassCounters) {
      warnings.push(`${entry.profile}/duck WebGL has no backend pass counters; authored bounds and common telemetry were recorded`);
    }
  }

  return { passed: failures.length === 0, failures, warnings };
}

export async function runWaterProfileEvidence({ browser, options, budget }) {
  const outputDir = path.resolve(options.outDir);
  fs.mkdirSync(outputDir, { recursive: true });
  const cases = [];
  const runStartedAt = new Date().toISOString();

  for (const profile of WATER_PROFILES) {
    const context = await browser.newContext({
      viewport: { width: options.width, height: options.height },
      extraHTTPHeaders: { "X-GoSX-ISR-Revalidate": "1" },
    });
    const page = await context.newPage();
    const errors = installErrorCapture(page);
    const requests = [];
    page.on("request", (request) => requests.push(request.url()));
    try {
      const profileURL = withProfile(options.url, profile);
      const cold = await navigateAndWait(page, profileURL, options.timeoutMS, "goto");
      const warm = await navigateAndWait(page, profileURL, options.timeoutMS, "reload");
      const manifest = await readAuthoredManifest(page);
      const selectedProfile = await readSelectedProfile(page);
      const sphere = await sampleWorkload({
        page,
        profile,
        workload: WATER_WORKLOADS[0],
        manifest,
        selectedProfile,
        timings: { cold, warm, workloadReadyMS: 0 },
        options,
        outputDir,
        errors,
        workloadRequests: requests,
      });
      cases.push(sphere);

      const requestIndex = requests.length;
      const duckStart = Date.now();
      await selectDuck(page, options.timeoutMS);
      const workloadReadyMS = Date.now() - duckStart;
      await waitAnimationFrames(page, options.warmupFrames);
      const duck = await sampleWorkload({
        page,
        profile,
        workload: WATER_WORKLOADS[1],
        manifest,
        selectedProfile,
        timings: { cold, warm, workloadReadyMS },
        options,
        outputDir,
        errors,
        workloadRequests: requests.slice(requestIndex),
      });
      cases.push(duck);
    } finally {
      await page.close().catch(() => {});
      await context.close().catch(() => {});
    }
  }

  const evidence = {
    schemaVersion: 1,
    generatedAt: new Date().toISOString(),
    runStartedAt,
    sourceURL: options.url,
    environment: {
      classification: options.environment,
      platform: runtimeProcess.platform,
      architecture: runtimeProcess.arch || "unknown",
      node: runtimeProcess.version || "unknown",
      hostname: os.hostname(),
      viewport: { width: options.width, height: options.height },
      samples: options.samples,
      warmupFrames: options.warmupFrames,
      hardwareThresholdsEnforced: options.enforceHardware,
    },
    schema: portablePath(path.resolve(options.schemaPath)),
    budget: portablePath(path.resolve(options.budgetPath)),
    cases,
  };
  evidence.validation = validateEvidence(evidence, budget, options);
  const reportPath = path.join(outputDir, "water-profile-evidence.json");
  fs.writeFileSync(reportPath, JSON.stringify(evidence, null, 2) + "\n");
  return { evidence, reportPath };
}

export async function runWaterProfileEvidenceFromFiles({ browser, options }) {
  const budget = JSON.parse(fs.readFileSync(path.resolve(options.budgetPath), "utf8"));
  const failures = validateBudget(budget);
  if (failures.length) {
    throw new Error(`water evidence configuration failed:\n- ${failures.join("\n- ")}`);
  }
  return runWaterProfileEvidence({ browser, options, budget });
}

async function sampleWorkload({
  page,
  profile,
  workload,
  manifest,
  selectedProfile,
  timings,
  options,
  outputDir,
  errors,
  workloadRequests,
}) {
  const before = await readMountedState(page);
  const intervals = await sampleAnimationFrames(page, options.samples);
  const after = await readMountedState(page);
  const screenshotPath = path.join(outputDir, `${profile}-${workload.id}.png`);
  await page.screenshot({ path: screenshotPath, type: "png", fullPage: false, scale: "css" });
  const counterKeys = Object.keys(after.counterAttributes);
  return {
    profile,
    selectedProfile,
    workload,
    url: page.url(),
    timings,
    backend: after.backend,
    authored: manifest.authored,
    resolved: after.resolved,
    adaptive: after.adaptive,
    shaderDiagnostics: after.shaderDiagnostics,
    featureState: after.featureState,
    objectTarget: after.objectTarget,
    raf: summarizeFrameIntervals(intervals),
    advance: {
      frame: after.sequence.frame - before.sequence.frame,
      simulation: after.sequence.simulation - before.sequence.simulation,
      before: before.sequence,
      after: after.sequence,
    },
    passDrawCounters: after.counterAttributes,
    telemetryAvailability: {
      backendPassCounters: counterKeys.some((name) => (
        /^data-gosx-scene3d-(?:webgpu|webgl)-water-/.test(name) &&
        /(?:pass|draw|dispatch)/.test(name)
      )),
      objectTargetActuals: counterKeys.some((name) => name.endsWith("water-object-texture-pixels")),
    },
    network: {
      requestCount: workloadRequests.length,
      duckAssetRequests: workloadRequests.filter((url) => /\/water\/models\/duck\/|scene3d-gltf/i.test(url)),
    },
    errors: snapshotErrors(errors),
    screenshot: {
      path: portablePath(screenshotPath),
      width: options.width,
      height: options.height,
    },
  };
}

async function navigateAndWait(page, targetURL, timeoutMS, mode) {
  const startedAt = Date.now();
  let response;
  if (mode === "reload") {
    response = await page.reload({ waitUntil: "domcontentloaded", timeout: timeoutMS });
  } else {
    response = await page.goto(targetURL, { waitUntil: "domcontentloaded", timeout: timeoutMS });
  }
  if (!response || !response.ok()) {
    throw new Error(`${mode} ${targetURL} returned ${response?.status() ?? "no response"}`);
  }
  await page.waitForFunction(() => {
    const mount = document.querySelector("[data-gosx-scene3d-mounted]");
    const renderer = mount?.getAttribute("data-gosx-scene3d-water-renderer") || "";
    const frame = Number(mount?.__gosxScene3DWaterFrameSeq || mount?.getAttribute("data-gosx-scene3d-water-frame-seq") || 0);
    return renderer === "unsupported" || (renderer === "active" && frame > 0);
  }, { timeout: timeoutMS });
  await waitAnimationFrames(page, 2);
  const navigation = await page.evaluate(() => {
    const entry = performance.getEntriesByType("navigation")[0];
    return entry ? {
      responseEndMS: entry.responseEnd,
      domContentLoadedMS: entry.domContentLoadedEventEnd,
      loadEventMS: entry.loadEventEnd,
      transferSize: entry.transferSize,
      encodedBodySize: entry.encodedBodySize,
    } : {};
  });
  return { externalReadyMS: Date.now() - startedAt, ...roundObject(navigation) };
}

async function selectDuck(page, timeoutMS) {
  const toggle = page.locator("[data-gosx-scene3d-control-toggle]");
  if (await toggle.getAttribute("aria-expanded") !== "true") {
    await toggle.click();
  }
  await page.locator('select[name="object"]').selectOption({ label: "Rubber Duck" });
  await page.waitForFunction(() => {
    const mount = document.querySelector("[data-gosx-scene3d-mounted]");
    return mount?.getAttribute("data-gosx-scene3d-water-state-active-object") === "Rubber Duck";
  }, { timeout: timeoutMS });
  await page.waitForFunction(() => {
    const paths = performance.getEntriesByType("resource").map((entry) => {
      try {
        return new URL(entry.name).pathname;
      } catch {
        return "";
      }
    });
    return [
      /\/water\/models\/duck\/Duck\.gltf$/i,
      /\/water\/models\/duck\/Duck0\.bin$/i,
      /\/water\/models\/duck\/DuckCM\.(?:png|jpe?g)$/i,
    ].every((pattern) => paths.some((pathname) => pattern.test(pathname)));
  }, { timeout: timeoutMS });
}

async function readAuthoredManifest(page) {
  return page.evaluate(() => {
    const node = document.querySelector("#gosx-manifest");
    if (!node) throw new Error("missing #gosx-manifest");
    const manifest = JSON.parse(node.textContent);
    const props = manifest.engines?.find((engine) => engine?.props?.scene?.waterSystems?.length)?.props;
    const water = props?.scene?.waterSystems?.[0];
    if (!props || !water) throw new Error("water Scene3D props are missing from manifest");
    const active = props.qualityProfiles?.full || {};
    const features = Object.fromEntries(
      Object.entries(water)
        .filter(([, value]) => typeof value === "boolean")
        .sort(([left], [right]) => left.localeCompare(right)),
    );
    return {
      authored: {
        dprCap: Number(props.maxDevicePixelRatio || active.dprCap || 0),
        maxPixels: Number(props.maxPixels || 0),
        simulationResolution: Number(water.resolution || 0),
        surfaceResolution: Number(water.surfaceResolution || active.surfaceResolution || 0),
        causticsResolution: Number(water.causticsResolution || active.causticsResolution || 0),
        objectShadowResolution: Number(water.objectShadowResolution || active.objectShadowResolution || 0),
        objectTextureMaxSide: Number(active.objectTextureMaxSide || 0),
        objectTexturePixelBudget: Number(water.objectTexturePixelBudget || active.objectTexturePixelBudget || 0),
        expensivePassCadence: Number(active.expensivePassCadence || 1),
        msaaSamples: Number(props.msaaSamples || 0),
        antialias: props.antialias ?? null,
        capabilityTier: props.capabilityTier || "",
        qualityTier: props.qualityTier || "",
        features,
        qualityProfiles: props.qualityProfiles || {},
      },
    };
  });
}

async function readSelectedProfile(page) {
  return page.evaluate(() => {
    const link = [...document.querySelectorAll('a[href*="quality="]')]
      .find((candidate) => candidate.getAttribute("aria-current") === "page");
    return link ? new URL(link.href).searchParams.get("quality") || "" : "";
  });
}

async function readMountedState(page) {
  return page.evaluate(() => {
    const mount = document.querySelector("[data-gosx-scene3d-mounted]");
    if (!mount) throw new Error("water mount is not committed");
    const string = (name) => mount.getAttribute(name) || "";
    const number = (name) => Number(string(name) || 0);
    let truth = {};
    try {
      truth = JSON.parse(string("data-gosx-scene3d-render-backend-truth") || "{}");
    } catch {
      truth = { parseError: true };
    }
    const attributes = Object.fromEntries([...mount.attributes].map((attribute) => [attribute.name, attribute.value]));
    const counterAttributes = Object.fromEntries(
      Object.entries(attributes)
        .filter(([name, value]) => (
          name.startsWith("data-gosx-scene3d-") &&
          /(?:pass|draw|dispatch|frame-seq|simulation-seq|vertices|cells)/.test(name) &&
          Number.isFinite(Number(value))
        ))
        .map(([name, value]) => [name, Number(value)])
        .sort(([left], [right]) => left.localeCompare(right)),
    );
    const numberEnding = (suffix) => {
      const entry = Object.entries(attributes).find(([name]) => name.endsWith(suffix));
      return entry ? Number(entry[1] || 0) : 0;
    };
    const shaderDiagnostics = truth.shaderDiagnostics || {};
    return {
      backend: {
        renderer: string("data-gosx-scene3d-renderer"),
        backend: string("data-gosx-scene3d-backend") || truth.backend || "",
        waterRenderer: string("data-gosx-scene3d-water-renderer"),
        fallbackReason: truth.fallbackReason || string("data-gosx-scene3d-renderer-fallback"),
        gpu: truth.gpu === true || string("data-gosx-scene3d-render-gpu") === "true",
        implementation: truth.implementation || string("data-gosx-scene3d-render-implementation"),
        browserEngine: truth.browserEngine || "",
        adapter: truth.adapter || "",
        adapterInfo: truth.adapterInfo || {},
        deviceLost: truth.deviceLost === true,
        initError: truth.initError || "",
        lastError: truth.lastError || "",
      },
      resolved: {
        actualDevicePixelRatio: number("data-gosx-scene3d-pixel-ratio"),
        dprCap: number("data-gosx-scene3d-quality-dpr-cap"),
        surfaceResolution: number("data-gosx-scene3d-quality-surface-resolution"),
        causticsResolution: number("data-gosx-scene3d-quality-caustics-resolution"),
        objectShadowResolution: number("data-gosx-scene3d-quality-object-shadow-resolution"),
        objectTextureMaxSide: number("data-gosx-scene3d-quality-object-texture-max-side"),
        objectTexturePixelBudget: number("data-gosx-scene3d-quality-object-texture-pixel-budget"),
        expensivePassCadence: number("data-gosx-scene3d-quality-expensive-pass-cadence"),
        canvasCSSWidth: number("data-gosx-scene3d-css-width"),
        canvasCSSHeight: number("data-gosx-scene3d-css-height"),
      },
      adaptive: {
        enabled: string("data-gosx-scene3d-adaptive-quality") === "true",
        requestedTier: string("data-gosx-scene3d-quality-requested"),
        activeTier: string("data-gosx-scene3d-quality-active"),
        tier: string("data-gosx-scene3d-quality-tier"),
        reason: string("data-gosx-scene3d-quality-reason"),
        revision: number("data-gosx-scene3d-quality-revision"),
        measurement: string("data-gosx-scene3d-quality-measurement"),
        frameMS: number("data-gosx-scene3d-quality-frame-ms"),
        ewmaMS: number("data-gosx-scene3d-quality-ewma-ms"),
        p95MS: number("data-gosx-scene3d-quality-p95-ms"),
        rung: number("data-gosx-scene3d-quality-rung"),
        rungName: string("data-gosx-scene3d-quality-rung-name"),
        rungReason: string("data-gosx-scene3d-quality-rung-reason"),
      },
      shaderDiagnostics: {
        messages: Number(shaderDiagnostics.messages || 0),
        errors: Number(shaderDiagnostics.errors || 0),
      },
      featureState: {
        waterSystems: number("data-gosx-scene3d-water-state-systems"),
        objectSystems: number("data-gosx-scene3d-water-state-object-systems"),
        causticSystems: number("data-gosx-scene3d-water-state-caustic-systems"),
        reflectionSystems: number("data-gosx-scene3d-water-state-reflection-systems"),
        refractionSystems: number("data-gosx-scene3d-water-state-refraction-systems"),
        activeObject: string("data-gosx-scene3d-water-state-active-object"),
        lifecycle: string("data-gosx-scene3d-water-lifecycle"),
        watchdog: string("data-gosx-scene3d-render-watchdog"),
      },
      objectTarget: {
        actualTargets: numberEnding("water-object-texture-targets"),
        actualWidth: numberEnding("water-object-texture-width"),
        actualHeight: numberEnding("water-object-texture-height"),
        actualPixels: numberEnding("water-object-texture-pixels"),
        actualShadowPixels: numberEnding("water-object-shadow-texture-pixels"),
      },
      sequence: {
        frame: Number(mount.__gosxScene3DWaterFrameSeq || number("data-gosx-scene3d-water-frame-seq")),
        simulation: Number(mount.__gosxScene3DWaterSimulationSeq || number("data-gosx-scene3d-water-simulation-seq")),
      },
      counterAttributes,
    };
  });
}

async function sampleAnimationFrames(page, samples) {
  return page.evaluate((count) => new Promise((resolve) => {
    const intervals = [];
    let previous = 0;
    function frame(now) {
      if (previous > 0) intervals.push(now - previous);
      previous = now;
      if (intervals.length >= count) resolve(intervals);
      else requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  }), samples);
}

async function waitAnimationFrames(page, count) {
  await page.evaluate((frameCount) => new Promise((resolve) => {
    let remaining = Math.max(1, frameCount);
    function frame() {
      remaining--;
      if (remaining <= 0) resolve();
      else requestAnimationFrame(frame);
    }
    requestAnimationFrame(frame);
  }), count);
}

function installErrorCapture(page) {
  const errors = { console: [], page: [], requests: [], ignoredRequests: [], http: [] };
  page.on("console", (message) => {
    if (message.type() === "error") errors.console.push(message.text());
  });
  page.on("pageerror", (error) => errors.page.push(error.message));
  page.on("requestfailed", (request) => {
    const failure = {
      url: request.url(),
      reason: request.failure()?.errorText || "",
    };
    if (isExpectedDevStreamAbort(failure)) errors.ignoredRequests.push(failure);
    else errors.requests.push(failure);
  });
  page.on("response", (response) => {
    if (response.status() >= 400) errors.http.push({ url: response.url(), status: response.status() });
  });
  return errors;
}

function snapshotErrors(errors) {
  return {
    console: [...errors.console],
    page: [...errors.page],
    requests: [...errors.requests],
    ignoredRequests: [...errors.ignoredRequests],
    http: [...errors.http],
  };
}

export function isExpectedDevStreamAbort(failure) {
  if (!failure || failure.reason !== "net::ERR_ABORTED") return false;
  try {
    const pathname = new URL(failure.url).pathname;
    return pathname === "/gosx/dev/events" || pathname === "/_gosx/client-events";
  } catch {
    return false;
  }
}

function validateNoUpgrade(entry, expected) {
  const failures = [];
  const resolved = entry.resolved;
  const checks = [
    ["dprCap", resolved.dprCap],
    ["surfaceResolution", resolved.surfaceResolution],
    ["causticsResolution", resolved.causticsResolution],
    ["objectShadowResolution", resolved.objectShadowResolution],
    ["objectTextureMaxSide", resolved.objectTextureMaxSide],
    ["objectTexturePixelBudget", resolved.objectTexturePixelBudget],
  ];
  for (const [field, actual] of checks) {
    if (Number(actual) > Number(expected[field]) + 0.001) {
      failures.push(`${entry.profile}/${entry.workload.id} silently upgraded ${field}: ${actual} > ${expected[field]}`);
    }
  }
  if (resolved.actualDevicePixelRatio > resolved.dprCap + 0.001) {
    failures.push(`${entry.profile}/${entry.workload.id} actual DPR ${resolved.actualDevicePixelRatio} exceeds cap ${resolved.dprCap}`);
  }
  const activeTier = entry.adaptive.activeTier;
  if (activeTier && !["full", "balanced", "survival"].includes(activeTier)) {
    failures.push(`${entry.profile}/${entry.workload.id} published unknown adaptive tier ${activeTier}`);
  }
  return failures;
}

function withProfile(source, profile) {
  const url = new URL(source);
  url.searchParams.set("quality", profile);
  return url.href;
}

function monotonicFields() {
  return [
    "dprCap",
    "maxPixels",
    "simulationResolution",
    "surfaceResolution",
    "causticsResolution",
    "objectShadowResolution",
    "objectTextureMaxSide",
    "objectTexturePixelBudget",
  ];
}

function splitArgument(argument) {
  const index = argument.indexOf("=");
  return index >= 0 ? [argument.slice(0, index), argument.slice(index + 1)] : [argument, undefined];
}

function positiveInteger(value, fallback) {
  const parsed = Number.parseInt(String(value ?? ""), 10);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : fallback;
}

function round(value) {
  return Math.round(Number(value) * 1000) / 1000;
}

function roundObject(value) {
  return Object.fromEntries(Object.entries(value).map(([key, entry]) => [key, typeof entry === "number" ? round(entry) : entry]));
}

function portablePath(value) {
  return path.resolve(value).replaceAll("\\", "/");
}

async function loadChromium(options) {
  if (options.playwrightModule) {
    const source = /^[a-z]+:/i.test(options.playwrightModule)
      ? options.playwrightModule
      : pathToFileURL(path.resolve(options.playwrightModule)).href;
    return (await import(source)).chromium;
  }
  try {
    return (await import("playwright")).chromium;
  } catch (firstError) {
    try {
      return (await import("playwright-core")).chromium;
    } catch {
      throw new Error(
        `Playwright is required for browser evidence. Set GOSX_PLAYWRIGHT_MODULE or --playwright-module. ${firstError.message}`,
      );
    }
  }
}

function usage() {
  return `Water profile/workload evidence

Usage:
  node scripts/water-profile-evidence.mjs [options]

Options:
  --url URL                    Served /demos/water URL
  --out-dir DIR                JSON and screenshot directory
  --budget FILE                Suggested profile/hardware budget
  --schema FILE                Evidence JSON schema path
  --playwright-module FILE     Playwright or playwright-core ESM entry
  --browser-executable FILE    Chrome/Chromium executable
  --environment KIND           unspecified|hardware|software-raster
  --samples N                  rAF interval sample count (default 120)
  --warmup-frames N            Frames before workload sampling (default 24)
  --timeout-ms N               Navigation/readiness timeout
  --width N --height N         Reproducible viewport
  --headed                     Show the browser
  --enforce-hardware           Apply configured timing thresholds on hardware
  --check-config               Validate budget/schema presence without a browser
`;
}

async function main(argv) {
  const options = parseArgs(argv);
  if (options.help) {
    console.log(usage());
    return;
  }
  const budget = JSON.parse(fs.readFileSync(path.resolve(options.budgetPath), "utf8"));
  const budgetFailures = validateBudget(budget);
  if (!fs.existsSync(path.resolve(options.schemaPath))) {
    budgetFailures.push(`evidence schema is missing: ${options.schemaPath}`);
  }
  if (budgetFailures.length) {
    throw new Error(`water evidence configuration failed:\n- ${budgetFailures.join("\n- ")}`);
  }
  if (options.checkConfig) {
    console.log(JSON.stringify({ passed: true, budget: options.budgetPath, schema: options.schemaPath }, null, 2));
    return;
  }
  const chromium = await loadChromium(options);
  const browser = await chromium.launch({
    executablePath: options.browserExecutable || undefined,
    headless: !options.headed,
  });
  try {
    const result = await runWaterProfileEvidence({ browser, options, budget });
    console.log(JSON.stringify({
      passed: result.evidence.validation.passed,
      report: portablePath(result.reportPath),
      cases: result.evidence.cases.length,
      failures: result.evidence.validation.failures,
      warnings: result.evidence.validation.warnings,
    }, null, 2));
    if (!result.evidence.validation.passed) runtimeProcess.exitCode = 1;
  } finally {
    await browser.close().catch(() => {});
  }
}

const argv = Array.isArray(runtimeProcess.argv) ? runtimeProcess.argv : [];
const isMain = argv[1] && pathToFileURL(path.resolve(argv[1])).href === import.meta.url;
if (isMain) {
  await main(argv.slice(2));
}
