# G10 — Governance and size budgets: procedures and the final run (repo: gosx)

Other tasks call into this file: "apply G10 §A" after a change that moves
`webgpu.ts` line counts or function metrics, "apply G10 §B" when
`bootstrap-size.test.mjs` fails. Run §C once at the very end.

Two helper scripts live OUTSIDE the repo (for example `/tmp/`). Never commit
them.

## §A — Renderer architecture ratchets

`client/js/testdata/scene3d-renderer-architecture.json` pins, for
`webgpu.ts`: the physical line ceiling (`files`, `sourceSets.webgpu.maxLines`
and `.maxFileLines`); every function over the default budgets as an exception
keyed `name@line:col` (so inserting lines renames every key below the edit);
and symbol ceilings for `createSceneWebGPURenderer` and `render`. The Go test
`cmd/buildbootstrap/scene3d_renderer_architecture_test.go` prints exactly what
moved. The script below reads that output and applies the smallest change the
code needs: it renames moved keys and keeps their metrics, raises a metric
only to the value the ratchet reports, raises line ceilings only to the live
count, and re-counts the governance line total. It exits non-zero if anything
is left for a human.

Save as `/tmp/gd-governance-refresh.mjs`:

```js
// gd-governance-refresh.mjs — refresh client/js/testdata/scene3d-renderer-architecture.json
// after an intended change to a governed renderer source. Run from the gosx
// repo root:  node /tmp/gd-governance-refresh.mjs
//
// It never loosens a metric the code does not need: exception and symbol
// values are raised only to the values the Go ratchet reports, renamed keys
// carry their old metrics, and line ceilings are raised only to the live
// physical line count.
import { execFileSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";

const root = process.cwd();
const jsonPath = path.join(root, "client/js/testdata/scene3d-renderer-architecture.json");
const clientJS = path.join(root, "client/js");
const METRICS = ["lines", "cyclomatic", "cognitive", "maxNesting", "parameters"];
const LABEL_TO_FIELD = { "lines": "lines", "cyclomatic": "cyclomatic", "cognitive": "cognitive", "max nesting": "maxNesting", "parameters": "parameters" };

function runGoRatchets() {
  try {
    execFileSync("go", ["test", "-count=1", "-tags", "grammar_subset grammar_subset_typescript", "-run", "TestRenderer", "./..."],
      { cwd: path.join(root, "cmd/buildbootstrap"), env: { ...process.env, GOWORK: "off" }, encoding: "utf8", stdio: ["ignore", "pipe", "pipe"] });
    return "";
  } catch (err) {
    return (err.stdout || "") + (err.stderr || "");
  }
}

function parseMetrics(text) {
  const out = {};
  for (const part of text.split(",")) {
    const m = part.trim().match(/^([a-z ]+?) (\d+) > (\d+)$/);
    if (m && LABEL_TO_FIELD[m[1]]) out[LABEL_TO_FIELD[m[1]]] = Number(m[2]);
  }
  return out;
}

function physicalLines(file) {
  const s = fs.readFileSync(file, "utf8");
  if (s.length === 0) return 0;
  return s.split("\n").length - (s.endsWith("\n") ? 1 : 0);
}

function write(doc) {
  fs.writeFileSync(jsonPath, JSON.stringify(doc, null, 2) + "\n");
}

let doc = JSON.parse(fs.readFileSync(jsonPath, "utf8"));
const defaults = doc.functionDefaults;
const changes = [];

for (let pass = 0; pass < 4; pass++) {
  const out = runGoRatchets();
  const stale = new Map(); // "source\0baseName" -> [exception]
  const lines = out.split("\n");
  let touched = false;
  for (const line of lines) {
    let m = line.match(/renderer function exception no longer matches a governed function: (\S+?\.(?:ts|js)):(\S+)$/);
    if (m) {
      const [source, name] = [m[1], m[2]];
      const base = name.split("@")[0];
      const entry = doc.functionExceptions.find((e) => e.source === source && e.name === name);
      if (entry) {
        const key = source + "\0" + base;
        if (!stale.has(key)) stale.set(key, []);
        stale.get(key).push(entry);
      }
    }
  }
  for (const line of lines) {
    let m = line.match(/: (\S+?\.(?:ts|js)):(\S+@\d+:\d+) exceeds default renderer function budget without an explicit exception: (.*)$/);
    if (m) {
      const [source, name, metricText] = [m[1], m[2], m[3]];
      const base = name.split("@")[0];
      const actual = parseMetrics(metricText);
      const bucket = stale.get(source + "\0" + base) || [];
      let entry = bucket.shift();
      if (entry) {
        changes.push(`rename ${entry.name} -> ${name}`);
        entry.name = name;
      } else {
        entry = { source, name, lines: defaults.lines, cyclomatic: defaults.cyclomatic, cognitive: defaults.cognitive, maxNesting: defaults.maxNesting, parameters: defaults.parameters };
        doc.functionExceptions.push(entry);
        changes.push(`add exception ${name}`);
      }
      for (const [k, v] of Object.entries(actual)) if (v > entry[k]) { entry[k] = v; }
      touched = true;
      continue;
    }
    m = line.match(/: (\S+?\.(?:ts|js)):(\S+@\d+:\d+) complexity regressed beyond exception: (.*)$/);
    if (m) {
      const entry = doc.functionExceptions.find((e) => e.source === m[1] && e.name === m[2]);
      if (entry) {
        for (const [k, v] of Object.entries(parseMetrics(m[3]))) if (v > entry[k]) entry[k] = v;
        changes.push(`raise exception ${m[2]} ${m[3]}`);
        touched = true;
      }
      continue;
    }
    m = line.match(/: (\S+?\.(?:ts|js)):([A-Za-z0-9_$]+) complexity regressed: (.*)$/);
    if (m) {
      const entry = doc.symbols.find((e) => e.source === m[1] && e.name === m[2]);
      if (entry) {
        for (const [k, v] of Object.entries(parseMetrics(m[3]))) if (v > entry[k]) entry[k] = v;
        changes.push(`raise symbol ${m[2]} ${m[3]}`);
        touched = true;
      }
      continue;
    }
    m = line.match(/renderer function exception no longer exceeds default budgets and must be pruned: (\S+?\.(?:ts|js)):(\S+)$/);
    if (m) {
      const before = doc.functionExceptions.length;
      doc.functionExceptions = doc.functionExceptions.filter((e) => !(e.source === m[1] && e.name === m[2]));
      if (doc.functionExceptions.length !== before) { changes.push(`prune ${m[2]}`); touched = true; }
    }
  }
  // Keep exceptions sorted the way the file already is: by source, then name.
  doc.functionExceptions.sort((a, b) => (a.source === b.source ? a.name.localeCompare(b.name) : a.source.localeCompare(b.source)));
  write(doc);
  if (!touched) break;
}

// Line ceilings: raise to the live count only when a governed file grew.
for (const [source, max] of Object.entries(doc.files)) {
  const live = physicalLines(path.join(clientJS, source));
  if (live > max) { doc.files[source] = live; changes.push(`files[${source}] ${max} -> ${live}`); }
}
for (const [backend, set] of Object.entries(doc.sourceSets)) {
  const counts = set.sources.map((s) => physicalLines(path.join(clientJS, s)));
  const total = counts.reduce((a, b) => a + b, 0);
  const largest = Math.max(...counts);
  if (total > set.maxLines) { changes.push(`sourceSets.${backend}.maxLines ${set.maxLines} -> ${total}`); set.maxLines = total; }
  if (largest > set.maxFileLines) { changes.push(`sourceSets.${backend}.maxFileLines ${set.maxFileLines} -> ${largest}`); set.maxFileLines = largest; }
}
write(doc);

// Governance line count must equal the live total of these five files.
const govFiles = [
  "scene3d-renderer-source-set.js",
  "scene3d-renderer-architecture.test.js",
  "testdata/scene3d-renderer-architecture.json",
  "../../cmd/buildbootstrap/scene3d_renderer_architecture_test.go",
  "../../internal/scene3drenderersource/source.go",
];
const gov = doc.governanceBudgetRevision;
const actual = govFiles.reduce((sum, f) => sum + physicalLines(path.join(clientJS, f)), 0);
if (actual !== gov.actualGovernanceLinesAtReview) {
  changes.push(`governance lines ${gov.actualGovernanceLinesAtReview} -> ${actual}`);
  gov.actualGovernanceLinesAtReview = actual;
  if (actual > gov.revisedGovernanceLineBudget) gov.revisedGovernanceLineBudget = actual;
  write(doc);
}

console.log(changes.length ? changes.join("\n") : "no changes");
const residual = runGoRatchets().split("\n").filter((l) => /scene3d_renderer_architecture_test\.go:\d+:/.test(l));
if (residual.length) {
  console.error("\nRESIDUAL Go ratchet failures (fix by hand):\n" + residual.join("\n"));
  process.exit(1);
}
```

Run from the gosx root:

```sh
node /tmp/gd-governance-refresh.mjs
git diff --stat client/js/testdata/scene3d-renderer-architecture.json
```

Expected output after G06 (with G01 before it; the G01 run is recorded in
G01): 24 lines of `rename <fn>@<old> -> <fn>@<new>` (for example
`render@18023:5 -> render@18075:5`), then

```
raise symbol createSceneWebGPURenderer lines 12190 > 12145, cyclomatic 4104 > 4095, cognitive 5326 > 5315
raise symbol render lines 689 > 679, cyclomatic 183 > 182, cognitive 237 > 236
files[../runtime/scene3d/webgpu.ts] 19162 -> 19227
sourceSets.webgpu.maxLines 19367 -> 19432
sourceSets.webgpu.maxFileLines 19162 -> 19227
```

Exact key positions depend on the preceding edits; the counts (24 renames,
two symbol raises, three ceilings) are what to compare. Then run:

```sh
(cd cmd/buildbootstrap && GOWORK=off go test -count=1 -tags 'grammar_subset grammar_subset_typescript' ./...)
node --test client/js/scene3d-renderer-architecture.test.js
```

Commit the JSON alone: `update(testdata): update scene3d renderer architecture metrics`.

If the script prints `RESIDUAL`, read the listed Go failures. A residual of
"exceeds default renderer function budget" for a NEW function means your edit
made a function too complex; simplify the edit instead of adding an
exception. None of the G06 edits create one.

## §B — Byte budgets

`client/js/bootstrap-size.test.mjs` holds a reviewed target per bundle
(`budgets`) and per page route (`routeBudgets`). The hard limit is the target
plus an allowance from `client/js/size-budget-policy.js` (5% capped at 64 KiB
raw and 16 KiB gzip/brotli). The test stops at the first failure, so use this
report to see all of them. Save as `/tmp/budget-report.mjs` and run it FROM
THE GOSX ROOT with a copy placed there, because it imports the policy module
by relative path:

```js
// budget-report.mjs — lists every bundle and route metric over its hard limit.
// Run from the gosx root: node <path>/budget-report.mjs
import fs from "node:fs";
import path from "node:path";
import policy from "./client/js/size-budget-policy.js";
const src = fs.readFileSync("client/js/bootstrap-size.test.mjs", "utf8");
const { evaluateSizeBudget } = policy;
function grab(name) {
  const start = src.indexOf("const " + name + " = [");
  let depth = 0, i = src.indexOf("[", start);
  for (let j = i; j < src.length; j++) {
    if (src[j] === "[") depth++;
    if (src[j] === "]") { depth--; if (depth === 0) return (0, eval)("(" + src.slice(i, j + 1).replace(/(\d)_(?=\d)/g, "$1") + ")"); }
  }
}
const size = (f) => fs.statSync(path.join("client/js", f)).size;
for (const b of grab("budgets")) {
  for (const [metric, ext] of [["raw", ""], ["gzip", ".gz"], ["brotli", ".br"]]) {
    const r = evaluateSizeBudget(size(b.file + ext), b[metric], metric);
    if (r.hardLimitExceeded) console.log(`BUNDLE ${b.file} ${metric} ${r.actual} > ${r.hardLimit} (target ${r.target})`);
  }
}
for (const b of grab("routeBudgets")) {
  for (const [metric, ext] of [["raw", ""], ["gzip", ".gz"], ["brotli", ".br"]]) {
    const actual = b.files.reduce((t, f) => t + size(f + ext), 0);
    const r = evaluateSizeBudget(actual, b[metric], metric);
    if (r.hardLimitExceeded) console.log(`ROUTE ${b.name} ${metric} ${r.actual} > ${r.hardLimit} (target ${r.target})`);
  }
}
console.log("done");
```

```sh
cp /tmp/budget-report.mjs ./budget-report.tmp.mjs && node ./budget-report.tmp.mjs; rm ./budget-report.tmp.mjs
```

Procedure, for every line the report prints:

1. A bundle whose OWN content this spec grew on purpose
   (`bootstrap-feature-scene3d-compute.js` in G05): set all three targets to
   the measurement rounded up to the next multiple of 100.
2. Anything else (routes, `bootstrap.js`, `bootstrap-feature-scene3d.js`):
   raise only the failing metric, by the smallest multiple of 100 that makes
   `target + allowance >= measured`.
3. Above the edited entry, add a ledger comment in the file's voice:
   what grew, the measurement `raw / gzip / brotli` with `_` separators, and
   the old → new target. Example (the validation run):

   ```js
       // GPU-driven instancing (docs/scene3d-gpu-driven): the gpuDriven
       // scene-state field and the WebGPU renderer seam, plus the instanced
       // cache-owner fix. Measured: 1_306_891 / 351_905 / 296_527. gzip target
       // 335_000 -> 335_600 and brotli 282_000 -> 282_500, the smallest 100-byte
       // steps that clear the hard limits.
   ```
4. Re-run the report until it prints only `done`, then
   `node --test client/js/bootstrap-size.test.mjs`.
5. Commit alone: `test(scene3d): raise size budgets for gpu-driven instancing`.

Measurements at the end of the validation run (G01–G06 applied in order):

| Budget | raw / gzip / brotli | Target change |
|---|---|---|
| `bootstrap-feature-scene3d-compute.js` | 61_083 / 17_800 / 16_038 | `32_000 / 9_500 / 8_600` → `61_100 / 17_800 / 16_100` |
| `bootstrap.js` | 1_715_779 / 471_432 / 379_008 | gzip `453_200` → `455_100` |
| Scene3D Chromium route (WebGPU, with labels) | 1_306_891 / 351_905 / 296_527 | gzip `335_000` → `335_600`, brotli `282_000` → `282_500` |

If you applied §B after each task, your intermediate targets differ (for
example the route's gzip went to `335_100` after G04) but the final values
land within one 100-byte step of this table.

## §C — Final full run (after G08)

```sh
make build-bootstrap
(cd cmd/buildbootstrap && GOWORK=off go run -tags 'grammar_subset grammar_subset_typescript' . --check)
(cd cmd/buildbootstrap && GOWORK=off go test -count=1 -tags 'grammar_subset grammar_subset_typescript' ./...)
(cd client/runtime && npm run typecheck)
node --test client/js/*.test.js client/js/*.test.mjs client/runtime/*/*.test.js
go test ./scene/... ./render/bundle ./scene/capability
go test ./examples/gosx-docs/... -count=1
go test ./cmd/gosx -run Scene -count=1
git diff --check
git status --porcelain            # nothing: every generated file is committed
```

Validation run: JS suite 2046 tests, 2044 pass, 2 skipped, 0 fail (baseline
78 in the three files 01 lists; the full suite was not counted at baseline);
ratchet `4874 diagnostic(s) (baseline: 4880)`; all Go packages `ok`.
