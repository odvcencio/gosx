// CPU proof of the actual mounted motion graph, with a mock DOM/RAF/scene writer.
// Includes VM boundary overhead; excludes GPU, full-frame and browser timings.
// Run: node --expose-gc client/js/timeline-motion.bench.mjs <baseline-ref> > results.json
// Requires Go on PATH. Overlay only the baseline compiler; runtime JS must match.
import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import inspector from "node:inspector";
import {execFileSync} from "node:child_process";
import {createHash} from "node:crypto";
import {performance} from "node:perf_hooks";
import {fileURLToPath} from "node:url";
import {gzipSync} from "node:zlib";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../..");
const git = (...args) => execFileSync("git", ["-c", "gc.auto=0", "-c", "maintenance.auto=false", ...args], {cwd: root, encoding: "utf8"}).trim();
assert.ok(process.argv[2], "supply an explicit baseline ref");
const baseline = git("rev-parse", "--verify", `${process.argv[2]}^{commit}`), compiler = "scene/timeline_motion.go";
const sha = value => createHash("sha256").update(value).digest("hex");
const read = file => fs.readFileSync(path.join(root, file), "utf8");
const files = {core: "client/js/bootstrap-src/06-motion-core.ts", track: "client/runtime/motion/track.ts", transport: "client/runtime/motion/transport.ts"};
const sources = Object.fromEntries(Object.entries(files).map(([name, file]) => [name, read(file)]));
for (const file of Object.values(files)) assert.equal(git("show", `${baseline}:${file}`), read(file).trim(), `runtime changed: ${file}`);
const scratch = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-timeline-bench-"));
const baselineSource = execFileSync("git", ["-c", "gc.auto=0", "-c", "maintenance.auto=false", "show", `${baseline}:${compiler}`], {cwd: root, encoding: "utf8"});
fs.writeFileSync(path.join(scratch, "baseline.go"), baselineSource);
fs.writeFileSync(path.join(scratch, "overlay.json"), JSON.stringify({Replace: {[path.join(root, compiler)]: path.join(scratch, "baseline.go")}}));
const exporter = "scene/testdata/timeline_motion_bench/main.go";
function exportPrograms(before) {
  return JSON.parse(execFileSync("go", ["run", ...(before ? ["-overlay="+path.join(scratch, "overlay.json")] : []), exporter], {cwd: root, encoding: "utf8", maxBuffer: 16 << 20}));
}
const programs = {baseline: exportPrograms(true), candidate: exportPrograms(false)};
fs.rmSync(scratch, {recursive: true});

function mount(program, profile = false) {
  let frameID = 0, latest;
  const frames = new Map(), body = {nodeType: 1, querySelectorAll() {return [];}};
  const document = {body, documentElement: body, hidden: false, addEventListener() {}, removeEventListener() {}};
  const window = {__gosx: {}, console, addEventListener() {}, matchMedia() {return {matches: false, addEventListener() {}};},
    requestAnimationFrame(fn) {frames.set(++frameID, fn); return frameID;}, cancelAnimationFrame(id) {frames.delete(id);}};
  const context = vm.createContext({window, document, console, performance: {now: () => 0}, setTimeout, clearTimeout, queueMicrotask});
  for (const [name, source] of Object.entries(sources)) vm.runInContext(source, context, {filename: profile && name === "track" ? "timeline-work-profile.ts" : `timeline-${name}.ts`});
  const motion = window.__gosx.motion, scene = {}, payload = JSON.stringify(program);
  const root = {nodeType: 1, hasAttribute: name => name === "data-gosx-motion-program", getAttribute: () => payload,
    querySelectorAll(selector) {return selector === "#scene" ? [scene] : [];}};
  motion.attachScene(scene, {write(_binding, value) {latest = value;}, invalidate() {}});
  const advance = () => {const pending = [...frames.values()]; frames.clear(); for (const fn of pending) fn(0);};
  const start = performance.now(); motion.mountPrograms(root); advance(); const mountMS = performance.now() - start;
  const transport = motion.transport(program.id);
  return {mountMS, seek(seconds) {transport.seek(seconds * 1000); advance(); return latest;},
    dispose() {motion.disposePrograms(root); advance(); assert.equal(frames.size, 0);}};
}

const session = new inspector.Session(); session.connect();
const post = (method, params = {}) => new Promise((resolve, reject) => session.post(method, params, (error, result) => error ? reject(error) : resolve(result)));
await post("Profiler.enable"); await post("Profiler.startPreciseCoverage", {callCount: true, detailed: true});
const work = [];
for (const variant of ["baseline", "candidate"]) for (const fixture of programs[variant]) {
  const r = mount(fixture.program, true);
  for (const sample of fixture.samples) assert.ok(Math.abs(r.seek(sample.seconds) - sample.value) < 1e-9);
  r.seek(fixture.n / 2 + .125); await post("Profiler.takePreciseCoverage");
  const value = r.seek(fixture.n / 2 + .25), coverage = await post("Profiler.takePreciseCoverage");
  const start = sources.track.indexOf("function(input, output)"); assert.ok(start >= 0);
  const samplers = coverage.result.filter(script => script.url === "timeline-work-profile.ts")
    .flatMap(script => script.functions.filter(fn => fn.ranges[0].startOffset === start && fn.ranges[0].count));
  assert.equal(samplers.length, 1);
  work.push({variant, tweens: fixture.n, samplerCalls: samplers[0].ranges[0].count, pose: value,
    signals: fixture.program.signals.length, programBytes: Buffer.byteLength(JSON.stringify(fixture.program)),
    programGzipBytes: gzipSync(JSON.stringify(fixture.program), {level: 9}).byteLength,
    programSHA256: sha(JSON.stringify(fixture.program)), nativeSamplesChecked: fixture.samples.length});
  r.dispose();
}
await post("Profiler.stopPreciseCoverage"); session.disconnect();

const trials = [], frames = 1000, warmupFrames = 256, repetitions = 8;
function timeAt(n, kind, index) {
  return kind === "steady" ? Math.floor(n / 2) + .1 + (index % 256) / 320
    : ((Math.imul(index, 2654435761) >>> 0) % n) + .25 + (index % 4) / 64;
}
for (let repetition = 0; repetition < repetitions; repetition++) {
  const order = repetition % 2 ? ["candidate", "baseline"] : ["baseline", "candidate"];
  for (const variant of order) for (const fixture of programs[variant]) for (const kind of ["steady", "seek"]) {
    global.gc?.();
    const r = mount(fixture.program);
    for (let i = 0; i < warmupFrames; i++) r.seek(timeAt(fixture.n, kind, i));
    const start = performance.now(); let value;
    for (let i = 0; i < frames; i++) value = r.seek(timeAt(fixture.n, kind, i));
    const elapsedMS = performance.now() - start;
    assert.ok(Math.abs(value - timeAt(fixture.n, kind, frames - 1)) < 1e-9);
    trials.push({repetition, variant, tweens: fixture.n, kind, mountMS: r.mountMS, microsecondsPerUpdate: elapsedMS * 1000 / frames});
    r.dispose();
  }
}
const median = values => {const sorted = values.toSorted((a, b) => a - b), mid = sorted.length >> 1; return (sorted[mid - 1] + sorted[mid]) / 2;};
const summary = [];
for (const fixture of programs.baseline) for (const kind of ["steady", "seek"]) {
  const row = {tweens: fixture.n, kind};
  for (const variant of ["baseline", "candidate"]) {
    const cases = trials.filter(t => t.variant === variant && t.tweens === fixture.n && t.kind === kind), values = cases.map(t => t.microsecondsPerUpdate);
    row[variant] = {medianUS: median(values), minUS: Math.min(...values), maxUS: Math.max(...values), medianMountMS: median(cases.map(t => t.mountMS))};
  }
  row.speedup = row.baseline.medianUS / row.candidate.medianUS;
  summary.push(row);
}
console.log(JSON.stringify({scope: "CPU only: actual mounted GoSX signal graph with mock DOM/RAF/scene writer; VM boundary included; excludes Go authoring, source loading, browser and GPU/frame-rate budgets",
  baseline, head: git("rev-parse", "HEAD"), dirty: !!git("status", "--porcelain"), compilerSHA256: {baseline: sha(baselineSource), candidate: sha(read(compiler))},
  sourceSHA256: Object.fromEntries([...Object.values(files), exporter, "client/js/timeline-motion.bench.mjs"].map(file => [file, sha(read(file))])),
  environment: {node: process.version, go: execFileSync("go", ["version"], {encoding: "utf8"}).trim(), os: os.type(), release: os.release(), arch: os.arch(), cpu: os.cpus()[0].model,
    affinity: process.platform === "linux" ? fs.readFileSync("/proc/self/status", "utf8").match(/^Cpus_allowed_list:\s*(.+)$/m)?.[1] : null, gcExposed: !!global.gc},
  timing: {inspectorDisabled: true, repetitions, frames, warmupFrames, alternatingOrder: true}, work, trials, summary}, null, 2));
