// Main-thread cost and heap behavior of one worker or engine view batch: the
// time a page spends in ctx.setSignalsJSON(text, changedOnly) with the real
// shared-signal store (00-textlayout.ts), the real engine context
// (30b-tail-engine-mounting.ts) and a real GoSX WASM runtime, in Node. No DOM,
// island render or presenter is included, so the numbers isolate the signal
// path: JSON in, the runtime's decode, compare and store, and the page's
// notifications.
//
// One run, 1,000 batches after a warm-up:
//
//   node client/js/signal-batch.bench.mjs --wasm core.wasm --wasm-exec wasm_exec.js
//
// A soak, which is what to run before a change to the signal path or the TinyGo
// runtime ships. It sends 2,000 batches of each payload, reports mean, p50, p95,
// p99, max, the number of batches over 2 ms and over 20 ms, and the size of the
// WASM memory at the start, the middle and the end, and --gate makes it fail if
// the memory is still growing in the second half of the run:
//
//   node client/js/signal-batch.bench.mjs --soak --gate --direct --wasm full.wasm --wasm-exec wasm_exec.js
//   node client/js/signal-batch.bench.mjs --soak --wasm core.wasm --wasm-exec wasm_exec.js \
//        --scenario hud,ui,full --payloads recorded-hud.json,recorded-ui.json
//
// A comparison of two builds, in interleaved runs, one process per run. Each
// build brings its own tree, because the page code is part of what is measured:
//
//   node client/js/signal-batch.bench.mjs --soak --compare --trials 3 \
//        --wasm new/gosx-runtime.wasm --wasm-exec new/wasm_exec.js \
//        --baseline-wasm old/gosx-runtime.wasm --baseline-tree ../gosx-origin-main
//
// With --compare the exit code is 1 when, for a payload, the median mean or max
// of the new build is worse than the baseline's by more than the tolerance (10%
// by default), or the new build's memory grew by more than --allow-growth-mib
// (64) in the second half of the run. Without --compare, --gate fails a run whose
// memory grew by more than that in the second half, or ends above --max-memory-mib N.
//
// A TinyGo runtime built with the direct batch entry (it exports gosx_batch_apply)
// takes a batch through its exports once the loader has published them on
// __gosx_set_input_batch. --direct does what the loader does and fails if the
// runtime lacks the exports; without it the bench measures the call through the
// function, which is what older loaders and the standard Go runtime use.
//
// Building the runtimes to measure (the flags are cmd/gosx tinyGoBuildArgs; run
// from the root of the checkout, and add -buildvcs=false in a worktree):
//
//   tinygo build -target wasm -tags "tinygo gosx_tiny_runtime gosx_runtime_core" \
//        -stack-size=256KB -ldflags=-extldflags=-zstack-size=262144 \
//        -no-debug -panic=trap -o core.wasm m31labs.dev/gosx/client/wasm
//   (engine: gosx_runtime_engine; full: -tags "tinygo gosx_runtime_full")
//
// -stack-size sizes goroutine stacks only. The direct batch entry runs on the system
// stack, which the linker sizes at 64 KiB unless -zstack-size says otherwise, and a
// batch that re-renders an island about 30 levels deep overflows that.
//
// or let the bench build one: --build core|engine|full. TinyGo's wasm_exec.js is
// $(tinygo env TINYGOROOT)/targets/wasm_exec.js. The standard Go runtime works
// too, with its own wasm_exec.js from $(go env GOROOT)/lib/wasm.
//
// Payloads are the built-in synthetic batches (--scenario hud|ui|full, about
// 2 KB, 8 KB and 30 KB in the shape of a ship's HUD, its UI views and a full
// state snapshot) or a file of recorded batches (--payloads file.json, a JSON
// array of batch strings, played in order and repeated).
//
// What the soak is for: TinyGo's collector scans memory conservatively, and it
// starts a goroutine with a stack that is a block of the garbage-collected heap
// for every call from JavaScript. Dead stacks that a stray word in the static
// data points into stay alive, together with whatever their frames pointed to,
// and the heap doubled at each collection until a collection took seconds. A
// median per batch does not show this, because the batches between collections
// are fast. The mean, the maximum and the memory at the end do.
//
// Node's JIT and the missing browser make absolute times differ from Chrome's.
// Compare runs from the same machine, and run the host quiet.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { spawnSync } from "node:child_process";
import { performance } from "node:perf_hooks";
import { fileURLToPath } from "node:url";
import { bootPage } from "./signal-batch-page.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(here, "..", "..");
const argv = process.argv.slice(2);
const flags = new Set(["soak", "compare", "worker", "json", "no-yield", "help", "gate", "direct"]);

function option(name, fallback) {
  const at = argv.indexOf("--" + name);
  if (at < 0) return fallback;
  if (flags.has(name)) return true;
  return argv[at + 1];
}
const has = (name) => argv.includes("--" + name);

if (has("help") || argv.length === 0) {
  console.log(fs.readFileSync(fileURLToPath(import.meta.url), "utf8").split("\n").filter((l) => l.startsWith("//")).map((l) => l.slice(3)).join("\n"));
  process.exit(argv.length === 0 ? 2 : 0);
}

const settings = {
  wasm: option("wasm"),
  wasmExec: option("wasm-exec"),
  tree: path.resolve(option("tree", repoRoot)),
  label: option("label", "run"),
  scenarios: String(option("scenario", "")).split(",").filter(Boolean),
  payloadFiles: String(option("payloads", "")).split(",").filter(Boolean),
  soak: has("soak"),
  iters: Number(option("iters", has("soak") ? "2000" : "1000")),
  warmup: Number(option("warmup", "200")),
  listeners: option("listeners", "raw"), // raw | value | none
  changedOnly: option("changed-only", "true") !== "false",
  yieldEvery: has("no-yield") ? 0 : Number(option("yield", "1")),
  memEvery: Number(option("mem-every", "100")),
  allowGrowthMiB: Number(option("allow-growth-mib", "64")),
  json: has("json"),
};

// The built-in synthetic batches. A seeded generator keeps every run on the same bytes.
function rng(seed) {
  let s = (seed >>> 0) || 1;
  return () => {
    s = (Math.imul(s ^ (s >>> 15), 0x2c1b3c6d) + 0x297a2d39) >>> 0;
    return s / 4294967296;
  };
}
const pad2 = (n) => String(n).padStart(2, "0");
const button = (k, i) => ({ Label: "Speed " + k, Short: String(k), Key: "speed|" + k, Disabled: false, Hint: "Set the game speed to " + k + "x", Pressed: k === 1 + (i % 6), Icon: "icon-speed-" + k });
function rows(n, i, spark) {
  return Array.from({ length: n }, (_, k) => ({
    ID: "row-" + k, Label: "Row label number " + k, Detail: "Detail text for row " + k + " at tick " + i, Tone: k % 4,
    Value: (k * 7 + i) % 1000, Spark: Array.from({ length: spark }, (_, s) => (s * k + i) % 100),
    Key: "select|row-" + k, Selected: k === i % n, Unit: "kW", Trend: ((i + k) % 3) - 1,
  }));
}
function hud(i) {
  const r = rng(i);
  return {
    version: 3, vessel: 1, tick: i, day: 5 + Math.floor(i / 1440), minute: 578 + i, speed: 1 + (i % 6), paused: false,
    cash: { v: 247360000 + i * 100, unit: "USD", trend: -275 },
    weather: { now: "Clear, light swell", next: "Squall at 18:00", wind: 12 + Math.floor(r() * 8), swell: Math.round(r() * 30) / 10 },
    systems: Array.from({ length: 20 }, (_, k) => ({ id: "sys-" + k, label: "System number " + k, value: { v: (k * 37 + i) % 1000, unit: "kW" }, state: k % 5 === 0 ? "warn" : "ok" })),
  };
}
const alerts = (i) => ({ version: 3, vessel: 1, latches: i % 50 === 0 ? ["fuel"] : [], stops: [], incidents: [], count: i % 50 === 0 ? 1 : 0 });
function people(i) {
  const r = rng(i * 3 + 1);
  return {
    version: 3, vessel: 1,
    cohorts: Array.from({ length: 42 }, (_, k) => ({ id: k + 1, count: 20 + ((k * 13 + i) % 40), satisfaction: Math.round(r() * 1000) / 10, health: 90 + (k % 10), rest: (i + k) % 100, dept: k < 24 ? "guest" : "crew" })),
    departments: Array.from({ length: 6 }, (_, k) => ({ name: "dept-" + k, labor: 100 + k * 10, used: (i * 7 + k) % 100, shift: k % 3 })),
    cast: Array.from({ length: 80 }, (_, k) => ({ id: 1000 + k, name: "Guest " + k, mood: ((i + k) % 5) - 2, activity: "activity-" + (k % 7), history: [(i + k) % 9, (i * 3 + k) % 9, (i * 5 + k) % 9] })),
  };
}
function plant(i) {
  return {
    version: 3, vessel: 1,
    power: { demand: 3200 + (i % 100), supply: 3300, store: 0 },
    gensets: Array.from({ length: 4 }, (_, k) => ({ id: 100 + k, load: (i * 3 + k * 11) % 100, fuel: 5000 - i, state: "run", temp: 80 + (k % 5) })),
    water: { potable: 120000 - i * 3, grey: 5000 + i, black: 3000 + i, desal: { rate: 90 + (i % 10) } },
    food: { stock: Array.from({ length: 4 }, (_, k) => ({ category: "cat-" + k, grams: 800000 - i * (k + 1) * 10, cold: k % 2 === 0 })) },
  };
}
function voyage(i) {
  return {
    version: 3, vessel: 1, machine: "underway", plan: { leg: 2, eta: 1440 - (i % 1440), speed: 14 },
    ports: Array.from({ length: 12 }, (_, k) => ({ id: k + 1, name: "Port " + k, nm: 100 + k * 40, open: k % 3 !== 0 })),
    contracts: Array.from({ length: 10 }, (_, k) => ({ id: k + 1, kind: "supply", due: 2000 + k * 50, paid: i % 2 === 0 })),
    ledger: Array.from({ length: 120 }, (_, k) => ({ id: i * 100 + k, label: "Entry " + k, amount: ((k * 31 + i) % 5000) - 2500, cause: "cause-" + (k % 9) })),
  };
}
function minigame(i) {
  return {
    version: 3, vessel: 1,
    sessions: Array.from({ length: 4 }, (_, k) => ({ id: k, kind: ["dock", "buffet", "room"][k % 3], t: i % 600, score: (i * 5 + k) % 100, nodes: Array.from({ length: 54 }, (_, n) => ({ n, load: (n * 3 + i) % 10, edges: [n + 1, n + 2] })) })),
    standing: Array.from({ length: 16 }, (_, k) => ({ id: k, duty: "duty-" + k, covered: (i + k) % 2 === 0 })),
    outcomes: Array.from({ length: 12 }, (_, k) => ({ seq: i - k, score: (k * 17) % 100, note: "Outcome " + k })),
  };
}
const synthetic = {
  hud: (i) => ({ "$bv.hud": hud(i), "$bv.alerts": alerts(i) }),
  ui: (i) => ({
    "$ui.bar": { Guide: "Next: fix the fuel line", GuideKey: "guide|next", Day: "Day " + (5 + (i % 30)), Clock: pad2(Math.floor(i / 60) % 24) + ":" + pad2(i % 60), Speeds: [1, 2, 3, 4, 5, 6].map((k) => button(k, i)), Pause: button(0, i), FastForward: button(9, i), Cash: "$" + (247360000 + i * 100), CashTrend: "-$275/h", TrendArrow: "down", WeatherNow: "Clear, light swell", WeatherNext: "Squall at 18:00" },
    "$ui.cover": { Headline: "Bon Voyage", Sub: "Your cruise line starts here", Rows: rows(8, i, 8), Buttons: [1, 2, 3].map((k) => button(k, i)) },
    "$ui.rail": { Title: "Systems", Rows: rows(14, i, 24), Sparks: rows(6, i, 24), Footer: "All systems nominal " + i },
  }),
  full: (i) => ({ "$bv.hud": hud(i), "$bv.alerts": alerts(i), "$bv.people": people(i), "$bv.plant": plant(i), "$bv.voyage": voyage(i), "$bv.minigame": minigame(i) }),
};

// payloadSets lists the batches to send. Each set is measured in a process of its own, so the
// arguments that name a set are kept with it.
function payloadSets() {
  const sets = [];
  const synthetics = settings.scenarios.length || settings.payloadFiles.length ? settings.scenarios : ["ui"];
  for (const name of synthetics) {
    if (!synthetic[name]) throw new Error("unknown --scenario " + name + " (hud, ui, full)");
    sets.push({ name, args: ["--scenario", name, "--payloads", ""], load: () => Array.from({ length: 64 }, (_, i) => JSON.stringify(synthetic[name](i + 1))) });
  }
  for (const file of settings.payloadFiles) {
    sets.push({
      name: path.basename(file, ".json"), args: ["--scenario", "", "--payloads", file],
      load: () => {
        const list = JSON.parse(fs.readFileSync(file, "utf8"));
        if (!Array.isArray(list) || !list.every((text) => typeof text === "string")) throw new Error(file + " must be a JSON array of batch strings");
        return list;
      },
    });
  }
  return sets;
}

// ---- one measurement, in this process -------------------------------------

async function measure(set) {
  const list = set.load();
  // --direct publishes the instance's exports on the batch function, as the loaders do, and
  // insists that they take a batch: the page would otherwise fall back to the function without a word.
  const page = await bootPage({ wasm: settings.wasm, wasmExec: settings.wasmExec, tree: settings.tree, direct: has("direct"), id: "bench" });
  const { ctx, instance } = page;
  const crashed = () => page.crashed();

  const names = [...new Set(list.flatMap((text) => Object.keys(JSON.parse(text))))];
  let heard = 0;
  if (settings.listeners !== "none") {
    for (const name of names) {
      if (settings.listeners === "value") ctx.subscribeSignal(name, () => { heard++; }, { immediate: false });
      else ctx.subscribeSignalJSON(name, (text) => { heard += text.length; }, { immediate: false });
    }
  }

  const memoryMiB = () => page.memoryMiB();
  const samples = new Float64Array(settings.iters);
  const memory = [];
  let bytes = 0;
  let failure = "";
  let done = 0;
  const total = settings.warmup + settings.iters;
  for (let i = 0; i < total; i++) {
    const text = list[i % list.length];
    const measured = i >= settings.warmup;
    if (measured && (i - settings.warmup) % settings.memEvery === 0) memory.push([i - settings.warmup, memoryMiB()]);
    const start = performance.now();
    try {
      const result = ctx.setSignalsJSON(text, settings.changedOnly);
      if (result) throw new Error("setSignalsJSON: " + result);
    } catch (error) {
      failure = String(error && error.message || error);
      break;
    }
    const spent = performance.now() - start;
    if (measured) { samples[i - settings.warmup] = spent; bytes += text.length; done++; }
    if (settings.yieldEvery && i % settings.yieldEvery === 0) await new Promise((r) => setImmediate(r));
  }
  const peak = memoryMiB();
  memory.push([done, peak]);

  const taken = Array.from(samples.subarray(0, done)).sort((a, b) => a - b);
  const at = (p) => taken.length ? taken[Math.min(taken.length - 1, Math.ceil(taken.length * p) - 1)] : 0;
  const mean = taken.length ? taken.reduce((a, b) => a + b, 0) / taken.length : 0;
  const memAt = (fraction) => {
    const wanted = Math.floor(settings.iters * fraction);
    let last = memory[0] ? memory[0][1] : 0;
    for (const [i, mib] of memory) { if (i <= wanted) last = mib; }
    return last;
  };
  const half = memAt(0.5);
  return {
    label: settings.label, payload: set.name, runtime: path.basename(settings.wasm), iters: done, requested: settings.iters,
    avgBytes: done ? Math.round(bytes / done) : 0, changedOnly: settings.changedOnly, listeners: settings.listeners,
    meanMS: round(mean), p50MS: round(at(0.5)), p95MS: round(at(0.95)), p99MS: round(at(0.99)), maxMS: round(taken.at(-1) || 0),
    over2ms: taken.filter((v) => v > 2).length, over20ms: taken.filter((v) => v > 20).length,
    stallMS: round(taken.filter((v) => v > 20).reduce((a, b) => a + b, 0)),
    memoryMiB: { start: memory[0] ? memory[0][1] : 0, half, end: peak, growthSecondHalf: peak - half },
    heard, failed: failure || crashed(),
  };
}
const round = (v) => Math.round(v * 1000) / 1000;

// ---- compare: interleaved child runs --------------------------------------

// child measures one set in a process of its own: a fresh WASM instance, a cold JIT and no
// memory left over from an earlier set.
function child(set, extra) {
  // The first occurrence of an option wins, so what the caller passes in extra comes before the defaults.
  const args = [fileURLToPath(import.meta.url), "--worker", ...extra, "--yield", String(settings.yieldEvery), "--iters", String(settings.iters), "--warmup", String(settings.warmup),
    "--listeners", settings.listeners, "--changed-only", String(settings.changedOnly), "--mem-every", String(settings.memEvery),
    "--label", settings.label, "--wasm", settings.wasm, "--wasm-exec", settings.wasmExec, "--tree", settings.tree, ...set.args];
  const run = spawnSync(process.execPath, args, { encoding: "utf8", maxBuffer: 1 << 26 });
  const rows = run.stdout.split("\n").filter((line) => line.startsWith("{")).map((line) => JSON.parse(line));
  if (rows.length === 0) throw new Error("worker failed (exit " + run.status + "): " + (run.stderr || run.stdout).slice(0, 2000));
  return rows[0];
}
const median = (values) => { const s = [...values].sort((a, b) => a - b); return s.length ? (s.length % 2 ? s[(s.length - 1) / 2] : (s[s.length / 2 - 1] + s[s.length / 2]) / 2) : 0; };

function printRow(row) {
  const m = row.memoryMiB;
  console.log(`${row.label.padEnd(10)} ${row.payload.padEnd(16)} ${String(row.avgBytes).padStart(6)} B  mean ${row.meanMS.toFixed(3).padStart(8)}  p50 ${row.p50MS.toFixed(3).padStart(7)}  p95 ${row.p95MS.toFixed(3).padStart(7)}  p99 ${row.p99MS.toFixed(3).padStart(8)}  max ${row.maxMS.toFixed(1).padStart(8)} ms  >2ms ${String(row.over2ms).padStart(4)}  >20ms ${String(row.over20ms).padStart(3)}  mem ${m.start}/${m.half}/${m.end} MiB${m.growthSecondHalf > settings.allowGrowthMiB ? "  GROWING" : ""}${row.failed ? "  FAILED: " + row.failed : ""}`);
}

async function main() {
  if (!settings.wasm || !settings.wasmExec) {
    console.error("usage: --wasm <runtime.wasm> --wasm-exec <wasm_exec.js> (--help for the rest)");
    process.exit(2);
  }
  if (has("build")) {
    // --build core|engine|full: build that TinyGo runtime from --tree into a temporary directory.
    const variant = option("build");
    const tags = { core: "tinygo gosx_tiny_runtime gosx_runtime_core", engine: "tinygo gosx_tiny_runtime gosx_runtime_engine", full: "tinygo gosx_runtime_full" }[variant];
    if (!tags) throw new Error("--build takes core, engine or full");
    const out = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-signal-bench-"));
    const target = path.join(out, "gosx-runtime.wasm");
    const build = spawnSync("tinygo", ["build", "-target", "wasm", "-tags=" + tags, "-stack-size=256KB", "-ldflags=-extldflags=-zstack-size=262144", "-no-debug", "-panic=trap", "-o", target, "m31labs.dev/gosx/client/wasm"],
      { cwd: settings.tree, stdio: "inherit", env: { ...process.env, GOFLAGS: "-buildvcs=false", GOWORK: "off" } });
    if (build.status !== 0) process.exit(build.status || 1);
    settings.wasm = target;
  }
  const sets = payloadSets();

  if (has("worker")) {
    console.log(JSON.stringify(await measure(sets[0])));
    process.exit(0);
  }

  if (!has("compare")) {
    // --gate fails the run when a batch was refused, the memory is still growing in the second
    // half of the run, or it ends above --max-memory-mib.
    const cap = Number(option("max-memory-mib", "0"));
    let failed = 0;
    for (const set of sets) {
      const row = child(set, has("direct") ? ["--direct"] : []);
      if (settings.json) console.log(JSON.stringify(row)); else printRow(row);
      if (!has("gate")) continue;
      const problems = [];
      if (row.failed) problems.push("a batch failed: " + row.failed);
      if (row.memoryMiB.growthSecondHalf > settings.allowGrowthMiB) problems.push(`memory grew by ${row.memoryMiB.growthSecondHalf} MiB in the second half of the run`);
      if (cap > 0 && row.memoryMiB.end > cap) problems.push(`memory ended at ${row.memoryMiB.end} MiB, over the ${cap} MiB cap`);
      if (problems.length) { failed++; console.log("  FAIL " + row.payload + ": " + problems.join("; ")); }
    }
    return finish(failed ? 1 : 0);
  }

  const baselineWasm = option("baseline-wasm");
  if (!baselineWasm) throw new Error("--compare needs --baseline-wasm (and --baseline-tree if the page code differs)");
  const baselineTree = path.resolve(option("baseline-tree", settings.tree));
  const baselineExec = option("baseline-wasm-exec", settings.wasmExec);
  const trials = Number(option("trials", "3"));
  const tolerance = Number(option("tolerance", "0.10"));
  const rowsByKey = new Map();
  const keep = (label, row) => { const key = label + "|" + row.payload; if (!rowsByKey.has(key)) rowsByKey.set(key, []); rowsByKey.get(key).push(row); };
  for (const set of sets) {
    for (let trial = 0; trial < trials; trial++) {
      // Alternate the order, so neither build always runs first on a warm host.
      const order = trial % 2 === 0 ? ["baseline", "new"] : ["new", "baseline"];
      for (const which of order) {
        // --direct is for the new build only; a baseline from before the direct entry has no exports to publish.
        const row = which === "new"
          ? child(set, ["--label", "new", ...(has("direct") ? ["--direct"] : [])])
          : child(set, ["--label", "baseline", "--wasm", baselineWasm, "--wasm-exec", baselineExec, "--tree", baselineTree]);
        keep(which, row);
        printRow(row);
      }
    }
  }
  console.log("\nmedian of " + trials + " trial(s):");
  let failed = 0;
  for (const set of sets) {
    const pick = (label) => rowsByKey.get(label + "|" + set.name) || [];
    const med = (rows, field) => median(rows.map((row) => row[field]));
    const summary = (label, rows) => ({
      ...rows[0], label, meanMS: med(rows, "meanMS"), p50MS: med(rows, "p50MS"), p95MS: med(rows, "p95MS"), p99MS: med(rows, "p99MS"), maxMS: med(rows, "maxMS"),
      over2ms: med(rows, "over2ms"), over20ms: med(rows, "over20ms"), failed: rows.map((r) => r.failed).find(Boolean) || "",
      memoryMiB: {
        start: med(rows.map((r) => r.memoryMiB), "start"), half: med(rows.map((r) => r.memoryMiB), "half"), end: med(rows.map((r) => r.memoryMiB), "end"),
        growthSecondHalf: Math.max(...rows.map((r) => r.memoryMiB.growthSecondHalf)),
      },
    });
    const b = summary("baseline", pick("baseline"));
    const n = summary("new", pick("new"));
    printRow(b);
    printRow(n);
    const problems = [];
    if (n.failed) problems.push("the new build failed: " + n.failed);
    if (n.meanMS > b.meanMS * (1 + tolerance)) problems.push(`mean ${n.meanMS} ms is more than ${tolerance * 100}% above the baseline's ${b.meanMS} ms`);
    if (n.maxMS > b.maxMS * (1 + tolerance)) problems.push(`max ${n.maxMS} ms is more than ${tolerance * 100}% above the baseline's ${b.maxMS} ms`);
    if (n.memoryMiB.growthSecondHalf > settings.allowGrowthMiB) problems.push(`memory grew by ${n.memoryMiB.growthSecondHalf} MiB in the second half of the run`);
    console.log(problems.length
      ? "  FAIL " + n.payload + ": " + problems.join("; ")
      : "  ok   " + n.payload + `: mean ${(n.meanMS / b.meanMS).toFixed(2)}x, max ${(n.maxMS / b.maxMS).toFixed(2)}x of the baseline, memory ${n.memoryMiB.end} MiB`);
    failed += problems.length ? 1 : 0;
  }
  finish(failed ? 1 : 0);
}

function finish(code) {
  process.exit(code);
}

main().catch((error) => { console.error(error); process.exit(2); });
