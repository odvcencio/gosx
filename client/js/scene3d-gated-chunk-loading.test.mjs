// A content-gated Scene3D chunk that a scene needs but cannot get is reported
// once, by name, through window.__gosx.reportIssue, instead of disappearing
// into a caller's catch. The terrain and crowd chunks have no hard-coded
// /gosx/ fallback: the page advertises their hashed URLs
// (island.writeScene3DChunkURLs), or the load fails loudly.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { createRequire } from "node:module";

const here = path.dirname(fileURLToPath(import.meta.url));
const ts = createRequire(import.meta.url)("../runtime/node_modules/typescript");
const read = (rel) => fs.readFileSync(path.join(here, rel), "utf8");
const transpile = (rel) => ts.transpileModule(read(rel), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText;
const loaderSource = transpile("../runtime/scene3d/script-loader.ts");
const bridgeSource = transpile("../runtime/scene3d/command-bridge.ts");

// sandbox evaluates the loader and the public command bridge against a page
// whose feature-scene3d tag carries `dataset`. Script elements are recorded,
// never executed; a test fires their onload or onerror itself.
function sandbox(dataset = {}) {
  const issues = [], scripts = [], errors = [];
  const tag = { dataset };
  const document = {
    querySelector: (selector) => (selector.includes("feature-scene3d") ? tag : null),
    createElement: () => ({ attrs: {}, setAttribute(name, value) { this.attrs[name] = value; } }),
    head: { appendChild(el) { scripts.push(el); return el; } },
  };
  const window = { __gosx: { reportIssue: (issue) => { issues.push(issue); return issue; } }, __gosx_runtime_api: {} };
  const console = { error: (...args) => errors.push(args.join(" ")), warn() {}, info() {}, log() {} };
  const load = new Function("window", "document", "console", `${loaderSource}\n${bridgeSource}\nreturn ensureSceneGatedFeatureLoaded;`);
  return { ensure: load(window, document, console), window, issues, scripts, errors };
}

test("a crowd frame on a page that did not advertise the crowd chunk fails loudly and fetches nothing", async () => {
  const page = sandbox();
  const first = page.window.__gosx.scene3d.dispatchCrowdFrame("crowd-root", new Uint8Array(8));
  assert.equal(page.scripts.length, 0, "no unversioned /gosx/ fallback fetch");
  await assert.rejects(first, /scene3d-crowd chunk URL was not advertised/);
  await assert.rejects(page.window.__gosx.scene3d.dispatchCrowdFrame("crowd-root", new Uint8Array(8)), /not advertised/);
  assert.equal(page.issues.length, 1, "reported once, not once per frame");
  assert.deepEqual(
    { scope: page.issues[0].scope, type: page.issues[0].type, ref: page.issues[0].ref, component: page.issues[0].component },
    { scope: "scene3d", type: "chunk", ref: "scene3d-crowd", component: "GoSXScene3D" },
  );
  assert.equal(page.errors.length, 1, page.errors.join("\n"));
});

test("terrain loads from the advertised hashed URL and a failed fetch is reported with that URL", async () => {
  const url = "../gosx/assets/runtime/bootstrap-feature-scene3d-terrain.t1.js";
  const page = sandbox({ gosxScene3dTerrainUrl: url, gosxScene3dTerrainIntegrity: "sha384-terrain" });
  const load = page.ensure("terrain", "gosxScene3dTerrainUrl", "");
  assert.equal(page.scripts.length, 1);
  assert.equal(page.scripts[0].attrs.src, url);
  assert.equal(page.scripts[0].attrs.integrity, "sha384-terrain");
  page.scripts[0].onerror({});
  await assert.rejects(load, /failed to load scene3d-terrain chunk/);
  assert.equal(page.issues.length, 1);
  assert.equal(page.issues[0].ref, "scene3d-terrain");
  assert.equal(page.issues[0].source, url);
  assert.match(page.errors[0], /scene3d-terrain/);
});

test("a speculative request for an unadvertised chunk rejects quietly", async () => {
  // The renderers ask for the Selena lighting chunk on every page; "not
  // advertised" there means the page does not need it.
  const page = sandbox();
  await assert.rejects(page.ensure("selena-lighting", "gosxScene3dSelenaLightingUrl", ""), /not advertised/);
  assert.equal(page.scripts.length, 0);
  assert.deepEqual(page.issues, []);
  assert.deepEqual(page.errors, []);
});

test("a chunk that loads its API resolves without an issue", async () => {
  const page = sandbox({ gosxScene3dCrowdUrl: "/gosx/assets/runtime/bootstrap-feature-scene3d-crowd.c1.js" });
  const load = page.ensure("crowd", "gosxScene3dCrowdUrl", "");
  page.window.__gosx_scene3d_crowd_api = { apply() {} };
  page.scripts[0].onload();
  assert.equal(await load, page.window.__gosx_scene3d_crowd_api);
  assert.deepEqual(page.issues, []);
  assert.deepEqual(page.errors, []);
});

test("no shipped bundle hard-codes an unversioned terrain or crowd chunk URL", () => {
  for (const bundle of ["bootstrap-feature-scene3d.js", "bootstrap.js"]) {
    assert.doesNotMatch(read(bundle), /\/gosx\/bootstrap-feature-scene3d-(terrain|crowd)\.js/, bundle);
  }
});
