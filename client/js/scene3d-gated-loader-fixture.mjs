// Exercise the real canonical loader without mounting an entire renderer.
import assert from "node:assert/strict";
import fs from "node:fs";
import { createRequire } from "node:module";
import { readSceneMountSrc } from "./runtime-test-harness.js";

const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
function loaderSource(text, names, statements = []) {
  const parsed = ts.createSourceFile("scene-loader.ts", text, ts.ScriptTarget.ES2022, true);
  const selected = parsed.statements.filter(node =>
    (ts.isFunctionDeclaration(node) && names.includes(node.name.text)) || statements.includes(node.getText(parsed)));
  assert.equal(selected.length, names.length + statements.length, "all real loader authorities must be exercised");
  return selected.map(node => node.getText(parsed)).join("\n");
}
export const sceneGatedLoaders = loaderSource(readSceneMountSrc(), [
  "gosxConfigureSceneScript", "resolveSceneSubFeatureURL", "sceneGatedFeatureAPI",
  "ensureSceneGatedFeatureLoaded", "ensureComputeFeatureLoaded",
], ["var sceneGatedFeaturePromises = Object.create(null);",
  "window.__gosx_scene3d_api.ensureFeatureLoaded = ensureSceneGatedFeatureLoaded;",
  "window.__gosx_ensure_scene3d_compute_loaded = ensureComputeFeatureLoaded;"])
  + "\n" + loaderSource(fs.readFileSync(new URL("bootstrap-src/10-runtime-scene-utils.ts", import.meta.url), "utf8"), [
    "gosxScriptNonceValue", "gosxCurrentScriptNonce", "gosxApplyCurrentScriptNonce",
  ]);
