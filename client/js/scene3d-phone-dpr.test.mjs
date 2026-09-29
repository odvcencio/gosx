import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";

const source = fs.readFileSync(new URL("../runtime/scene3d/mount-viewport.ts", import.meta.url), "utf8");
function viewport({ width = 390, height = 844, coarse = true, dpr = 3, max = 0, props = {}, screen = true } = {}) {
  const environment = { viewportWidth: width, viewportHeight: height, devicePixelRatio: dpr };
  const context = vm.createContext({
    window: { devicePixelRatio: dpr, screen: screen ? { width, height } : undefined },
    sceneEnvironmentState: () => environment,
    sceneMediaQueryMatches: () => false,
    sceneNumber: (value, fallback) => value == null || !Number.isFinite(Number(value)) ? fallback : Number(value),
    sceneBool: (value, fallback) => value == null ? fallback : Boolean(value),
    defaultSceneMaxDevicePixelRatio: () => 2,
  });
  vm.runInContext(source, context);
  return context.sceneViewportFromMount(null, props, {
    baseWidth: 300, baseHeight: 200, responsive: false, explicitMaxDevicePixelRatio: max,
  }, null, { tier: "full", coarsePointer: coarse }, null);
}

test("phone canvas caps DPR at 1.5 while keeping CSS size", () => {
  const result = viewport();
  assert.equal(result.devicePixelRatio, 1.5);
  assert.equal(result.cssWidth, 300);
  assert.equal(result.cssHeight, 200);
  assert.equal(result.pixelWidth, 450);
  assert.equal(result.pixelHeight, 300);
  assert.equal(viewport({ width: 844, height: 390 }).devicePixelRatio, 1.5);
  assert.equal(viewport({ screen: false }).devicePixelRatio, 1.5);
});

test("fine-pointer displays and tablets retain their capability cap", () => {
  assert.equal(viewport({ coarse: false }).devicePixelRatio, 2);
  assert.equal(viewport({ width: 800, height: 1200 }).devicePixelRatio, 2);
});

test("authored maximum overrides the phone default and lower DPR is preserved", () => {
  assert.equal(viewport({ max: 2 }).devicePixelRatio, 2);
  assert.equal(viewport({ max: 1.25 }).devicePixelRatio, 1.25);
  assert.equal(viewport({ dpr: 1.25 }).devicePixelRatio, 1.25);
  assert.equal(viewport({ props: { minDevicePixelRatio: 2 } }).devicePixelRatio, 1.5);
  assert.equal(viewport({ max: 2, props: { maxPixels: 60000 } }).devicePixelRatio, 1);
});
