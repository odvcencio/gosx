import assert from "node:assert/strict";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import vm from "node:vm";
import { execFileSync } from "node:child_process";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
const file = path.join(root, "client/runtime/scene3d/webgpu-ocean-kernels.ts");
const source = fs.readFileSync(file, "utf8");

const unescapeTemplate = (s) => s.replaceAll("\\${", "${").replaceAll("\\`", "`").replaceAll("\\\\", "\\");

// The generator hashes each kernel's WGSL and, for gather kernels, its GLSL, in
// file order. Recompute that from the text so a hand edit cannot go unnoticed.
function embeddedSources(text) {
  const out = [];
  const re = /^ {6}(?:wgsl|glsl): `((?:[^`\\]|\\[\s\S])*)`,$/gm;
  for (let m; (m = re.exec(text)); ) out.push(unescapeTemplate(m[1]));
  return out;
}

test("generated Elio ocean kernels match their embedded SHA-256 header", () => {
  const header = /^\/\/ sha256: ([0-9a-f]{64}) /m.exec(source);
  assert.ok(header, "file lacks the sha256 header");
  const sources = embeddedSources(source);
  assert.ok(sources.length === 19, `expected the ocean set's WGSL and GLSL sources, found ${sources.length}`);
  const hash = crypto.createHash("sha256");
  for (const s of sources) hash.update(s);
  assert.equal(hash.digest("hex"), header[1], "edited by hand? regenerate with scripts/generate-elio-ocean.sh <elio-dir>");
});

test("generated Elio ocean kernels assign one frozen global", () => {
  assert.ok(!/^export /m.test(source), "classic script must not use ES exports");
  const window = {};
  vm.runInNewContext(source, { window });
  const set = window.__gosx_scene3d_elio_ocean;
  assert.ok(set && Object.isFrozen(set) && Object.isFrozen(set.kernels));
  assert.equal(set.sha256, /^\/\/ sha256: ([0-9a-f]{64}) /m.exec(source)[1]);
  for (const name of ["oceanSpectrum", "oceanEvolve", "oceanAssemble", "oceanMip", "oceanFoam", "oceanProbe", "fftCompute64", "fftCompute128", "fftCompute256"]) {
    const k = set.kernels[name];
    assert.ok(k, `${name} missing`);
    assert.match(k.wgsl, /@compute/);
    assert.ok(Object.isFrozen(k.bindings) && k.workgroup.length === 3);
  }
});

test("generated Elio ocean kernels match a fresh Elio emit when ELIO_DIR is set", { skip: !process.env.ELIO_DIR }, () => {
  const fresh = execFileSync("go", ["run", "./cmd/elio", "emit", "ts", "--set", "ocean", "--global", "__gosx_scene3d_elio_ocean"], {
    cwd: process.env.ELIO_DIR,
    env: { ...process.env, GOWORK: "off" },
    encoding: "utf8",
    maxBuffer: 1 << 28,
  });
  assert.equal(fresh, source, "regenerate with scripts/generate-elio-ocean.sh <elio-dir>");
});
