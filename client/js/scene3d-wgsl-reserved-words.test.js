"use strict";
// WGSL reserves a list of words (https://www.w3.org/TR/WGSL/#reserved-words).
// A shader that declares one fails to compile, and the renderer then drops to
// WebGL2. Auto exposure shipped with `let target`, which Chrome rejects, so any
// scene that set Environment.AutoExposure lost WebGPU. This test scans every
// WGSL source in the Scene3D runtime for declarations of reserved words.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const RESERVED = new Set(("NULL Self abstract active alignas alignof as asm asm_fragment async attribute auto await become " +
  "binding_array cast catch class co_await co_return co_yield coherent column_major common compile compile_fragment concept " +
  "const_cast consteval constexpr constinit crate debugger decltype delete demote demote_to_helper do dynamic_cast enum " +
  "explicit export extends extern external fallthrough filter final finally friend from fxgroup get goto groupshared highp " +
  "impl implements import inline instanceof interface layout lowp macro macro_rules match mediump meta mod module move mut " +
  "mutable namespace new nil noexcept noinline nointerpolation noperspective null nullptr of operator package packoffset " +
  "partition pass patch pixelfragment precise precision premerge priv protected pub public readonly ref regardless register " +
  "reinterpret_cast require resource restrict self set shared sizeof smooth snorm static static_assert static_cast std " +
  "subroutine super target template this thread_local throw trait try type typedef typeid typename typeof union unless " +
  "unorm unsafe unsized use using varying virtual volatile wgsl where with writeonly yield").split(" "));

const dir = path.join(__dirname, "..", "runtime", "scene3d");

// Template literals that hold WGSL: they declare an entry point.
function wgslSources(text) {
  const out = [];
  const re = /`([^`]*)`/gs;
  let m;
  while ((m = re.exec(text))) {
    if (/@(compute|fragment|vertex)\b/.test(m[1])) out.push(m[1]);
  }
  return out;
}

function declared(source) {
  const names = [];
  // Template substitutions are not WGSL text; blank them so they never match.
  const text = source.replace(/\$\{[^}]*\}/g, "0");
  for (const m of text.matchAll(/\b(?:let|var|const|override)\s*(?:<[^>]*>)?\s*([A-Za-z_]\w*)/g)) names.push(m[1]);
  for (const m of text.matchAll(/\bfn\s+([A-Za-z_]\w*)\s*\(([^)]*)\)/g)) {
    names.push(m[1]);
    for (const p of m[2].matchAll(/(?:^|,)\s*(?:@[^\s]+(?:\([^)]*\))?\s*)*([A-Za-z_]\w*)\s*:/g)) names.push(p[1]);
  }
  for (const m of text.matchAll(/\bstruct\s+\w+\s*\{([^}]*)\}/g)) {
    for (const f of m[1].matchAll(/(?:^|[,{;])\s*(?:@[^\s]+(?:\([^)]*\))?\s*)*([A-Za-z_]\w*)\s*:/g)) names.push(f[1]);
  }
  return names;
}

test("no Scene3D WGSL source declares a reserved word", () => {
  const offenders = [];
  let scanned = 0;
  for (const file of fs.readdirSync(dir).filter((f) => f.endsWith(".ts"))) {
    for (const source of wgslSources(fs.readFileSync(path.join(dir, file), "utf8"))) {
      scanned++;
      for (const name of declared(source)) if (RESERVED.has(name)) offenders.push(file + ": " + name);
    }
  }
  assert.ok(scanned > 10, "scan found only " + scanned + " WGSL sources; the matcher is broken");
  assert.deepEqual([...new Set(offenders)], []);
});

test("the matcher catches the auto exposure defect", () => {
  assert.deepEqual(declared("@compute @workgroup_size(1) fn main() { let target = 1.0; }").filter((n) => RESERVED.has(n)), ["target"]);
});
