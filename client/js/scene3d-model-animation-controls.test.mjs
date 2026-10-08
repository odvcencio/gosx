import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import vm from "node:vm";
import { createRequire } from "node:module";
import { freshFeatureBundleSource } from "./runtime-test-harness.js";

const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const inline = ts.transpileModule(fs.readFileSync(new URL("../runtime/scene3d/animation.ts", import.meta.url), "utf8"), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText;
const lazy = freshFeatureBundleSource("scene3d-animation");

for (const [publication, source] of [["inline", inline], ["lazy", lazy]]) {
  for (const backend of ["JS", "WASM"]) {
    test(publication + " model controls preserve " + backend + " playback, replay and stop semantics without loading", () => {
      const calls = [], playing = new Set();
      const sandbox = { console, document: { head: { appendChild() { assert.fail("control adapters must not load scripts"); } } } };
      sandbox.window = sandbox;
      vm.runInContext(source, vm.createContext(sandbox));
      const api = sandbox.__gosx_scene3d_animation_api;
      const play = (name, options) => { calls.push(["play", name, JSON.parse(JSON.stringify(options))]); if (name === "walk") playing.add(name); };
      const stop = (name, options) => { calls.push(["stop", name, JSON.parse(JSON.stringify(options))]); playing.delete(name); };
      const record = { model: { loop: false, animationSpeed: 1.5, animationWeight: 0.6, animationFadeInMS: 250 }, animation: "", animationSeq: "", morphTargets: [{}] };
      if (backend === "WASM") {
        record.wasmMixerActive = true; record.wasmMixer = 7;
        sandbox.__gosx_motion_mixer_play = (handle, name, fadeIn, loop, speed, weight) => {
          assert.equal(handle, 7); play(name, { loop, speed, weight, fadeIn });
        };
        sandbox.__gosx_motion_mixer_stop = (handle, name, fadeOut) => { assert.equal(handle, 7); stop(name, { fadeOut }); };
        sandbox.__gosx_motion_mixer_is_playing = (handle, name) => { assert.equal(handle, 7); return playing.has(name); };
      } else {
        record.mixer = { play, stop, isPlaying: name => playing.has(name) };
      }
      api.initializeModelPlayback(record, { ...record.model, animation: " walk ", animationSeq: "initial" });
      assert.equal(record.animation, "walk");
      assert.equal(record.animationSeq, "initial");
      assert.equal(api.isModelPlaying(record), true);
      assert.deepEqual(calls, [["play", "walk", { loop: false, speed: 1.5, weight: 0.6, fadeIn: 0.25 }]]);
      assert.equal(api.applyModelAnimation(record, { animation: "walk", animationSeq: "initial" }), false);
      assert.equal(calls.length, 1, "an unchanged sequence must not restart playback");
      assert.equal(api.applyModelAnimation(record, { animation: "walk", animationSeq: "next", animationSpeed: 2 }), true);
      assert.deepEqual(calls.slice(1), [
        ["stop", "walk", { fadeOut: 0 }],
        ["play", "walk", { loop: false, speed: 2, weight: 0.6, fadeIn: 0.25 }],
      ]);
      assert.equal(record.model.animationSpeed, 2);
      assert.equal(record.animationSeq, "next");
      assert.equal(api.applyModelAnimation(record, { animation: "", animationFadeOutMS: 0 }), true);
      assert.deepEqual(calls.at(-1), ["stop", "walk", { fadeOut: 0 }]);
      assert.equal(record.animation, "");
      assert.equal(record.poseDirty, true, "stopping leaves one final morph/node refresh");
      assert.equal(api.isModelPlaying(record), false);
      const count = calls.length;
      for (const invalid of [null, [], new Float32Array(1)]) assert.equal(api.applyModelAnimation(record, invalid), false);
      assert.equal(calls.length, count);
      api.initializeModelPlayback(record, { animation: "missing", animationSeq: "unavailable" });
      assert.equal(record.animation, "", "unknown clips cannot claim playback ownership");
    });
  }
}
