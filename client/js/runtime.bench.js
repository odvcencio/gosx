// Scene3D microbenchmark harness — measures wall-clock time for the
// JS-side hot-path functions the v0.15.x perf sweep optimized. Runs in
// Node with zero external dependencies beyond the built-in vm module.
//
// Usage:
//
//     node client/js/runtime.bench.js
//     node client/js/runtime.bench.js --iterations 100000 --warmup 1000
//     node client/js/runtime.bench.js --only lightsHash,translate
//     node client/js/runtime.bench.js --json > bench-results.json
//
// The harness does NOT mount a full Scene3D (which would require the
// entire DOM mock from runtime.test.js — substantial code the bench
// doesn't need). Instead it loads the bootstrap bundle in a VM context
// with window.__gosx_bench_exports = true, which triggers the bench
// export hook at the end of 30-tail.js. The hook exposes the pure JS
// helpers we care about on window.__gosx_bench so the bench can call
// them directly with fixture inputs.
//
// Functions measured:
//
//   sceneRenderCamera           — camera normalization (allocates result)
//   translateScenePointInto     — inlined alloc-free scene-space transform
//   translateScenePoint         — legacy allocating wrapper (comparison)
//   scenePBRLightsHash          — FNV-1a over lights+environment fields
//   scenePBRUploadLights        — dirty-tracked uniform upload (hit vs miss)
//   scenePBRUploadExposure      — same, 2 uniforms only
//   expandSceneThickLineIntoScratch — pooled thick-line vertex expansion
//
// Stats reported per benchmark: min, p50, p95, max, mean, std dev, and
// total wall time. A summary table prints at the end.

"use strict";

const fs = require("fs");
const path = require("path");
const vm = require("vm");

// ---------- CLI parsing ------------------------------------------------

function parseArgs(argv) {
  const opts = {
    iterations: 50000,
    warmup: 1000,
    only: null,
    json: false,
  };
  for (let i = 2; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--iterations" || arg === "-n") {
      opts.iterations = parseInt(argv[++i], 10) || opts.iterations;
    } else if (arg === "--warmup" || arg === "-w") {
      opts.warmup = parseInt(argv[++i], 10) || opts.warmup;
    } else if (arg === "--only") {
      opts.only = new Set(argv[++i].split(",").map((s) => s.trim()));
    } else if (arg === "--json") {
      opts.json = true;
    } else if (arg === "--help" || arg === "-h") {
      process.stdout.write(
        "Usage: node client/js/runtime.bench.js [options]\n" +
          "  --iterations N   samples per benchmark (default 50000)\n" +
          "  --warmup N       warmup iterations (default 1000)\n" +
          "  --only a,b,c     run only named benchmarks\n" +
          "  --json           emit JSON results to stdout\n",
      );
      process.exit(0);
    }
  }
  return opts;
}

// ---------- Minimal VM context --------------------------------------

// The bench only needs enough of a window/document stub to satisfy
// bootstrap.js during its global init. We never mount a Scene3D from
// here — we reach into window.__gosx_bench for the exported helpers.

function createBenchContext() {
  const listeners = new Map();
  function addListener(target, type, fn) {
    const key = target + "::" + type;
    if (!listeners.has(key)) listeners.set(key, []);
    listeners.get(key).push(fn);
  }

  const documentStub = {
    readyState: "complete",
    visibilityState: "visible",
    documentElement: { scrollTop: 0, scrollHeight: 0, clientHeight: 0 },
    body: { scrollTop: 0, scrollHeight: 0 },
    createElement() {
      return {
        style: {},
        children: [],
        attributes: {},
        setAttribute() {},
        getAttribute() { return null; },
        appendChild() {},
        removeChild() {},
        addEventListener() {},
        removeEventListener() {},
        getContext() { return null; },
      };
    },
    getElementById() { return null; },
    querySelector() { return null; },
    querySelectorAll() { return []; },
    addEventListener: (t, f) => addListener("document", t, f),
    removeEventListener() {},
  };

  const windowStub = {
    performance: { now: () => Number(process.hrtime.bigint()) / 1e6 },
    requestAnimationFrame() { return 0; },
    cancelAnimationFrame() {},
    setTimeout: (fn) => setTimeout(fn, 0),
    clearTimeout: (id) => clearTimeout(id),
    addEventListener: (t, f) => addListener("window", t, f),
    removeEventListener() {},
    __gosx_bench_exports: true,
    __gosx_engine_factories: Object.create(null),
    __gosx_register_engine_factory(name, factory) {
      windowStub.__gosx_engine_factories[name] = factory;
    },
  };

  // Cross-link so bootstrap's `typeof window !== "undefined"` checks pass
  // AND document.X references go to the same stub.
  windowStub.window = windowStub;
  windowStub.document = documentStub;
  windowStub.self = windowStub;
  windowStub.globalThis = windowStub;

  const context = vm.createContext(windowStub);
  return context;
}

function loadBootstrapInto(context) {
  const bootstrapSource = fs.readFileSync(
    path.join(__dirname, "bootstrap.js"),
    "utf8",
  );
  try {
    vm.runInContext(bootstrapSource, context, { filename: "bootstrap.js" });
  } catch (err) {
    // bootstrap's DOMContentLoaded / bootstrapPage call may reach for
    // features we haven't stubbed (like document.body.appendChild). That's
    // fine — we only need the pre-bootstrap hook to fire, which happens
    // during the initial run-through. Swallow the post-load error.
    if (!context.__gosx_bench) {
      throw new Error(
        "bench exports missing: " + err.message +
        "\n(bootstrap.js failed before reaching the bench-exports hook)",
      );
    }
  }
  if (!context.__gosx_bench) {
    throw new Error(
      "bench exports missing after bootstrap load. Did you rebuild " +
      "client/js/bootstrap.js after editing bootstrap-src/30-tail.js?",
    );
  }
  return context.__gosx_bench;
}

// ---------- Statistics ----------------------------------------------

function computeStats(samplesNs) {
  const sorted = samplesNs.slice().sort((a, b) => a - b);
  const n = sorted.length;
  let sum = 0;
  for (let i = 0; i < n; i++) sum += sorted[i];
  const mean = sum / n;
  let variance = 0;
  for (let i = 0; i < n; i++) {
    const d = sorted[i] - mean;
    variance += d * d;
  }
  variance /= n;
  return {
    n,
    min: sorted[0],
    p50: sorted[Math.floor(n * 0.5)],
    p95: sorted[Math.floor(n * 0.95)],
    max: sorted[n - 1],
    mean,
    std: Math.sqrt(variance),
    total: sum,
  };
}

function formatNs(ns) {
  if (ns < 1000) return ns.toFixed(1) + "ns";
  if (ns < 1e6) return (ns / 1000).toFixed(2) + "µs";
  if (ns < 1e9) return (ns / 1e6).toFixed(2) + "ms";
  return (ns / 1e9).toFixed(2) + "s";
}

// ---------- Per-benchmark runner -----------------------------------

// Each benchmark is a closure that calls its target function exactly once.
// We batch to amortize hrtime.bigint() overhead — measuring individual
// ~100ns calls directly would be noisy. Batch size tuned so total call
// duration is well above hrtime precision.

function runBenchmark(name, setup, target, opts) {
  const fixtures = setup();
  const batchSize = 1000;
  const batches = Math.max(1, Math.floor(opts.iterations / batchSize));
  const samples = new Array(batches);

  // Warmup: call enough to let V8's tier-up kick in.
  for (let i = 0; i < opts.warmup; i++) target(fixtures);

  for (let b = 0; b < batches; b++) {
    const start = process.hrtime.bigint();
    for (let i = 0; i < batchSize; i++) {
      target(fixtures);
    }
    const end = process.hrtime.bigint();
    // Per-call ns: (end - start) / batchSize.
    samples[b] = Number(end - start) / batchSize;
  }

  return {
    name,
    iterations: batches * batchSize,
    stats: computeStats(samples),
  };
}

// ---------- Fixtures ------------------------------------------------

// Raw lights fixture — no cached _lightHash / _envHash stamps. Exercises
// the cold-cache fallback path inside scenePBRLightsHash where it has to
// call hashLightContent / hashEnvironmentContent for every light and the
// env in full. Represents a caller that builds lights outside the
// normalizeSceneLight pipeline (no production code path does this today,
// but tests and the first-frame precompute path can).
function makeLightsFixtureRaw() {
  return {
    lights: [
      { kind: "directional", x: 5, y: 10, z: 5, directionX: 0, directionY: -1, directionZ: 0, color: "#ffffff", intensity: 1.2, range: 0, decay: 2 },
      { kind: "point", x: -3, y: 4, z: 2, color: "#ffb380", intensity: 0.8, range: 10, decay: 2 },
      { kind: "ambient", color: "#404060", intensity: 0.3 },
    ],
    environment: {
      ambientColor: "#404060",
      ambientIntensity: 0.3,
      skyColor: "#8de1ff",
      skyIntensity: 0.5,
      groundColor: "#1a1a18",
      groundIntensity: 0.2,
      fogDensity: 0.01,
      fogColor: "#0a0a12",
    },
  };
}

// Pre-stamped lights fixture — simulates the production path where
// normalizeSceneLight and normalizeSceneEnvironment have already computed
// the sub-hashes at mutation time. scenePBRLightsHash just combines the
// cached numbers instead of walking every field. This is the real
// dirty-tracking steady state for a scene that doesn't mutate lights.
function makeLightsFixture(bench) {
  const fx = makeLightsFixtureRaw();
  // Mimic normalizeSceneLight / normalizeSceneEnvironment by stamping
  // the cached sub-hashes the bench would otherwise miss.
  for (const light of fx.lights) {
    light._lightHash = bench.hashLightContent
      ? bench.hashLightContent(light)
      : undefined;
  }
  fx.environment._envHash = bench.hashEnvironmentContent
    ? bench.hashEnvironmentContent(fx.environment)
    : undefined;
  return fx;
}

function makeObjectFixture() {
  return {
    x: 1.2, y: 0.4, z: -0.8,
    rotationX: 0.1, rotationY: 0.2, rotationZ: 0,
    spinX: 0, spinY: 0.02, spinZ: 0,
    scaleX: 1, scaleY: 1, scaleZ: 1,
    shiftX: 0, shiftY: 0, shiftZ: 0,
    driftPhase: 0, driftSpeed: 0,
  };
}

function makeThickLineFixture(segmentCount) {
  const worldPositions = new Float32Array(segmentCount * 6);
  const worldColors = new Float32Array(segmentCount * 8);
  const worldLineWidths = new Float32Array(segmentCount);
  const worldLinePasses = new Uint8Array(segmentCount);
  for (let seg = 0; seg < segmentCount; seg++) {
    const base = seg * 6;
    worldPositions[base + 0] = seg * 0.1;
    worldPositions[base + 1] = 0;
    worldPositions[base + 2] = 0;
    worldPositions[base + 3] = seg * 0.1 + 0.5;
    worldPositions[base + 4] = 0.5;
    worldPositions[base + 5] = 0;
    const colorBase = seg * 8;
    for (let c = 0; c < 8; c++) worldColors[colorBase + c] = 1;
    worldLineWidths[seg] = 3.5;
    worldLinePasses[seg] = seg % 3; // mix of opaque/alpha/additive
  }
  return { worldPositions, worldColors, worldLineWidths, worldLinePasses, segmentCount };
}

// Minimal mock gl: only the methods scenePBRUploadLights / Exposure call.
function makeMockGl() {
  let calls = 0;
  return {
    uniform1i() { calls++; },
    uniform1f() { calls++; },
    uniform3f() { calls++; },
    get _calls() { return calls; },
    reset() { calls = 0; },
  };
}

function makeUniformsStub() {
  // Each uniform location is just a truthy sentinel — the mock gl methods
  // ignore the location anyway. Real WebGL uniforms are integer handles
  // but the dirty-tracking code only checks `uniforms._lastLightsHash`
  // against a number, not the location objects themselves.
  const stub = {};
  const names = [
    "lightCount", "ambientColor", "ambientIntensity", "skyColor", "skyIntensity",
    "groundColor", "groundIntensity", "fogColor", "fogDensity", "hasFog",
    "exposure", "toneMapMode",
  ];
  for (const n of names) stub[n] = { _loc: n };
  stub.lightTypes = [];
  stub.lightPositions = [];
  stub.lightDirections = [];
  stub.lightColors = [];
  stub.lightIntensities = [];
  stub.lightRanges = [];
  stub.lightDecays = [];
  stub.lightAngles = [];
  stub.lightPenumbras = [];
  stub.lightGroundColors = [];
  for (let i = 0; i < 8; i++) {
    stub.lightTypes.push({ _loc: "lightType" + i });
    stub.lightPositions.push({ _loc: "lightPos" + i });
    stub.lightDirections.push({ _loc: "lightDir" + i });
    stub.lightColors.push({ _loc: "lightColor" + i });
    stub.lightIntensities.push({ _loc: "lightIntensity" + i });
    stub.lightRanges.push({ _loc: "lightRange" + i });
    stub.lightDecays.push({ _loc: "lightDecay" + i });
    stub.lightAngles.push({ _loc: "lightAngle" + i });
    stub.lightPenumbras.push({ _loc: "lightPenumbra" + i });
    stub.lightGroundColors.push({ _loc: "lightGroundColor" + i });
  }
  return stub;
}

// ---------- Benchmarks ---------------------------------------------

function buildBenchmarks(bench, opts) {
  const list = [];

  list.push({
    key: "sceneRenderCamera",
    setup: () => ({ camera: { x: 1, y: 2, z: 6, rotationX: 0.1, rotationY: 0.2, rotationZ: 0, fov: 60, near: 0.1, far: 100 } }),
    target: (fx) => { bench.sceneRenderCamera(fx.camera); },
  });

  list.push({
    key: "translateScenePointInto (alloc-free)",
    setup: () => ({ out: { x: 0, y: 0, z: 0 }, object: makeObjectFixture() }),
    target: (fx) => { bench.translateScenePointInto(fx.out, 1, 2, 3, fx.object, 0.5); },
  });

  list.push({
    key: "scenePBRLightsHash (cold, no stamps)",
    setup: makeLightsFixtureRaw,
    target: (fx) => { bench.scenePBRLightsHash(fx.lights, fx.environment); },
  });

  list.push({
    key: "scenePBRLightsHash (warm, normalized stamps)",
    setup: () => makeLightsFixture(bench),
    target: (fx) => { bench.scenePBRLightsHash(fx.lights, fx.environment); },
  });

  list.push({
    key: "scenePBRUploadLights (first frame, upload path)",
    setup: () => {
      const base = makeLightsFixture(bench);
      return {
        gl: makeMockGl(),
        uniforms: makeUniformsStub(),
        lights: base.lights,
        environment: base.environment,
      };
    },
    target: (fx) => {
      // Reset the stamp each call so we exercise the full upload path.
      fx.uniforms._lastLightsHash = undefined;
      bench.scenePBRUploadLights(fx.gl, fx.uniforms, fx.lights, fx.environment);
    },
  });

  list.push({
    key: "scenePBRUploadLights (cache hit, hash inline)",
    setup: () => {
      const base = makeLightsFixture(bench);
      const fx = {
        gl: makeMockGl(),
        uniforms: makeUniformsStub(),
        lights: base.lights,
        environment: base.environment,
      };
      // Prime the stamp so every subsequent call early-exits.
      bench.scenePBRUploadLights(fx.gl, fx.uniforms, fx.lights, fx.environment);
      return fx;
    },
    target: (fx) => {
      bench.scenePBRUploadLights(fx.gl, fx.uniforms, fx.lights, fx.environment);
    },
  });

  list.push({
    key: "scenePBRUploadLights (cache hit, precomputed hash)",
    setup: () => {
      const base = makeLightsFixture(bench);
      const fx = {
        gl: makeMockGl(),
        uniforms: makeUniformsStub(),
        lights: base.lights,
        environment: base.environment,
        hash: bench.scenePBRLightsHash(base.lights, base.environment),
      };
      // Prime the stamp so every subsequent call early-exits.
      bench.scenePBRUploadLights(fx.gl, fx.uniforms, fx.lights, fx.environment, fx.hash);
      return fx;
    },
    target: (fx) => {
      // Production path: render() hoists scenePBRLightsHash once per
      // frame and passes it to all 3 scenePBRUploadLights call sites.
      // This case measures just the comparison + early return — the
      // hash cost lives in the scenePBRLightsHash benchmark above, to
      // be amortized across the 3 call sites.
      bench.scenePBRUploadLights(fx.gl, fx.uniforms, fx.lights, fx.environment, fx.hash);
    },
  });

  list.push({
    key: "scenePBRUploadExposure (first frame, upload path)",
    setup: () => ({
      gl: makeMockGl(),
      uniforms: makeUniformsStub(),
      environment: { exposure: 1.2, toneMapping: "aces" },
    }),
    target: (fx) => {
      fx.uniforms._lastExposure = undefined;
      fx.uniforms._lastToneMapMode = undefined;
      bench.scenePBRUploadExposure(fx.gl, fx.uniforms, fx.environment, false);
    },
  });

  list.push({
    key: "scenePBRUploadExposure (cache hit / dirty-tracked)",
    setup: () => {
      const fx = {
        gl: makeMockGl(),
        uniforms: makeUniformsStub(),
        environment: { exposure: 1.2, toneMapping: "aces" },
      };
      bench.scenePBRUploadExposure(fx.gl, fx.uniforms, fx.environment, false);
      return fx;
    },
    target: (fx) => {
      bench.scenePBRUploadExposure(fx.gl, fx.uniforms, fx.environment, false);
    },
  });

  list.push({
    key: "expandSceneThickLineIntoScratch (128 segments)",
    setup: () => {
      const lineFx = makeThickLineFixture(128);
      return {
        scratch: bench.createSceneThickLineScratch(),
        ...lineFx,
      };
    },
    target: (fx) => {
      bench.expandSceneThickLineIntoScratch(
        fx.scratch,
        fx.worldPositions,
        fx.worldColors,
        fx.worldLineWidths,
        fx.worldLinePasses,
        fx.segmentCount,
      );
    },
  });

  // Composite "simulated frame" benchmark: chains the per-frame hot-path
  // calls that a real render() makes, end-to-end, on a steady-state
  // static scene. Doesn't stand up a real DOM or GL context (that would
  // need ~1000 lines of mock surface), but approximates the frame cost
  // of the parts of render() we've been optimizing:
  //
  //   1. scenePBRLightsHash once per frame (hoisted)
  //   2. scenePBRUploadExposure × 3 (main + skinned + instanced programs)
  //   3. scenePBRUploadLights × 3 with precomputed hash
  //   4. expandSceneThickLineIntoScratch for 64 thick-line segments
  //
  // The shadow upload path, matrix math, and actual GL draw calls are
  // not included — those are GPU-bound on real hardware and can't be
  // meaningfully measured in a mock. This captures what the sweep
  // actually optimized: dirty-tracked uniform uploads + thick-line
  // scratch expansion + the hash hoist.
  list.push({
    key: "simulated frame (3 lights static, 64 thick lines)",
    setup: () => {
      const lights = makeLightsFixture(bench);
      const lineFx = makeThickLineFixture(64);
      const mainUniforms = makeUniformsStub();
      const skinnedUniforms = makeUniformsStub();
      const instancedUniforms = makeUniformsStub();
      const gl = makeMockGl();
      // Prime the uniforms stamps so steady-state iterations hit the
      // cache-hit fast path (what a static scene looks like after the
      // first frame).
      bench.scenePBRUploadExposure(gl, mainUniforms, lights.environment, false);
      bench.scenePBRUploadExposure(gl, skinnedUniforms, lights.environment, false);
      bench.scenePBRUploadExposure(gl, instancedUniforms, lights.environment, false);
      bench.scenePBRUploadLights(gl, mainUniforms, lights.lights, lights.environment);
      bench.scenePBRUploadLights(gl, skinnedUniforms, lights.lights, lights.environment);
      bench.scenePBRUploadLights(gl, instancedUniforms, lights.lights, lights.environment);
      return {
        gl,
        mainUniforms,
        skinnedUniforms,
        instancedUniforms,
        lights: lights.lights,
        environment: lights.environment,
        scratch: bench.createSceneThickLineScratch(),
        ...lineFx,
      };
    },
    target: (fx) => {
      // 1. Hoisted hash (once per frame).
      const hash = bench.scenePBRLightsHash(fx.lights, fx.environment);
      // 2. Three exposure uploads (cache hit, 2 field comparisons each).
      bench.scenePBRUploadExposure(fx.gl, fx.mainUniforms, fx.environment, false);
      bench.scenePBRUploadExposure(fx.gl, fx.skinnedUniforms, fx.environment, false);
      bench.scenePBRUploadExposure(fx.gl, fx.instancedUniforms, fx.environment, false);
      // 3. Three lights uploads with precomputed hash (cache hit fast path).
      bench.scenePBRUploadLights(fx.gl, fx.mainUniforms, fx.lights, fx.environment, hash);
      bench.scenePBRUploadLights(fx.gl, fx.skinnedUniforms, fx.lights, fx.environment, hash);
      bench.scenePBRUploadLights(fx.gl, fx.instancedUniforms, fx.lights, fx.environment, hash);
      // 4. Thick-line buffer expansion (runs once per frame on scenes
      //    with thick lines — scales linearly with segment count).
      bench.expandSceneThickLineIntoScratch(
        fx.scratch,
        fx.worldPositions,
        fx.worldColors,
        fx.worldLineWidths,
        fx.worldLinePasses,
        fx.segmentCount,
      );
    },
  });

  // Filter by --only if the user asked for a subset.
  if (opts.only) {
    return list.filter((entry) => {
      for (const token of opts.only) {
        if (entry.key.toLowerCase().indexOf(token.toLowerCase()) !== -1) return true;
      }
      return false;
    });
  }
  return list;
}

// ---------- Reporting -----------------------------------------------

function printTable(results) {
  const nameWidth = Math.max(
    32,
    results.reduce((max, r) => Math.max(max, r.name.length), 0) + 2,
  );
  const header = [
    "benchmark".padEnd(nameWidth),
    "min".padStart(10),
    "p50".padStart(10),
    "p95".padStart(10),
    "max".padStart(10),
    "mean".padStart(10),
    "std".padStart(10),
  ].join("  ");
  process.stdout.write("\n" + header + "\n");
  process.stdout.write("-".repeat(header.length) + "\n");
  for (const r of results) {
    const line = [
      r.name.padEnd(nameWidth),
      formatNs(r.stats.min).padStart(10),
      formatNs(r.stats.p50).padStart(10),
      formatNs(r.stats.p95).padStart(10),
      formatNs(r.stats.max).padStart(10),
      formatNs(r.stats.mean).padStart(10),
      formatNs(r.stats.std).padStart(10),
    ].join("  ");
    process.stdout.write(line + "\n");
  }
  process.stdout.write("\n");
}

function printJson(results) {
  const out = results.map((r) => ({
    name: r.name,
    iterations: r.iterations,
    stats: r.stats,
  }));
  process.stdout.write(JSON.stringify(out, null, 2) + "\n");
}

// ---------- Main ----------------------------------------------------

function main() {
  const opts = parseArgs(process.argv);
  const context = createBenchContext();
  const bench = loadBootstrapInto(context);

  if (!opts.json) {
    process.stdout.write(
      `Scene3D microbenchmark — ${opts.iterations} iterations per bench, ` +
        `${opts.warmup} warmup\n`,
    );
  }

  const benches = buildBenchmarks(bench, opts);
  const results = benches.map((entry) =>
    runBenchmark(entry.key, entry.setup, entry.target, opts),
  );

  if (opts.json) {
    printJson(results);
  } else {
    printTable(results);
  }
}

main();
