import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";
const source = fs.readFileSync(new URL("bootstrap-src/13-scene-material.ts", import.meta.url), "utf8");
function harness() {
  const c = {};
  vm.runInNewContext(source.slice(source.indexOf("  var sceneLongStringHashes")), c);
  return c;
}
function reference(seed, value) {
  const text = String(value || "");
  let digest = text.length < 256 ? seed : 2166136261;
  for (let i = 0; i < text.length; i++) digest = Math.imul(digest ^ text.charCodeAt(i), 16777619) >>> 0;
  return text.length < 256 ? digest : Math.imul((Math.imul(seed ^ text.length, 16777619) >>> 0) ^ digest, 16777619) >>> 0;
}
test("bounded digest cache preserves hashes and replacement invalidation", () => {
  const c = harness();
  for (let i = 0; i < 2000; i++) {
    const value = "material:" + i + String(i % 7).repeat(i % 4000);
    assert.equal(c.sceneContentHashString(i * 3124, value), reference(i * 3124, value));
  }
  assert.notEqual(c.sceneContentHashString(123, "x".repeat(512)), c.sceneContentHashString(123, "y".repeat(512)));
  assert.ok(c.sceneLongStringHashes.size <= 256);
  assert.ok(c.sceneLongStringHashUnits <= 16 * 1024 * 1024);
});
test("hot textures survive animated material identity churn", () => {
  const c = harness();
  const textures = Array.from({length:128}, (_,i) => "atlas:" + i + "x".repeat(8000));
  let expectedScanned = textures.reduce((sum,t) => sum+t.length,0);
  for (let frame=0; frame<100; frame++) {
    for (const texture of textures) c.sceneContentHashString(123,texture);
    for (let i=0;i<12;i++) {
      const key = "phase:" + frame + ":" + i + "s".repeat(512);
      expectedScanned += key.length;
      c.sceneContentHashString(123,key);
    }
  }
  assert.equal(c.sceneLongStringHashScannedUnits,expectedScanned);
  assert.equal(c.sceneLongStringHashes.size,256);
});
test("byte budget remains bounded when textures exceed the working set", () => {
  const c = harness();
  for (let i=0;i<40;i++) c.sceneContentHashString(17,"large:"+i+"a".repeat(500000));
  assert.ok(c.sceneLongStringHashUnits <= 16 * 1024 * 1024);
  assert.ok(c.sceneLongStringHashes.size <= 256);
  const previous = c.sceneContentHashString(17,"large:0"+"a".repeat(500000));
  assert.equal(previous,reference(17,"large:0"+"a".repeat(500000)));
});
