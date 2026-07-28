// WebGPU custom-post `time` uniform: the reserved clock must reach a post pass.
//
// THE REPORT THIS SETTLES
//
// A source-level trace claimed the WebGPU custom-post uniform packer was
// called with neither a frame nor a render context, so the resolver returned
// 0 for `time` and any post effect gated on it -- `smoothstep(revealDelay,
// ..., time)` is the shape that matters -- never opened on WebGPU. That would
// have reframed a series of "the effect never appears" reports as one root
// cause. WebGL received exactly that fix in v0.35.10.
//
// The trace is FALSE against this tree, and these assertions pin the three
// facts that make it false:
//
//  1. `time` is a reserved auto-uniform resolved from a renderer-closure
//     clock (sceneSelenaFrameTime), not from the per-draw arguments. The
//     packer therefore does not need a frame or a render context to answer.
//  2. The renderer writes that clock EVERY frame, before any Selena draw.
//  3. The post chain applies AFTER the clock is written, in the same frame.
//
// Any of the three regressing puts the effect back to a permanently-zero
// clock, which fails silently: a time-gated post renders as a passthrough and
// every counter reads healthy. The ordering assertion is the load-bearing
// one, because moving the clock write below the post chain is an easy and
// invisible mistake during a refactor.
import test from "node:test";
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const source = fs.readFileSync(
  path.join(__dirname, "bootstrap-src", "16a-scene-webgpu.js"),
  "utf8"
);

test("time is a reserved auto-uniform, not a per-draw argument", () => {
  assert.match(
    source,
    /if \(name === "time"\) return sceneSelenaFrameTime;/,
    "the uniform resolver must answer `time` from the renderer clock"
  );
  // The resolver reads a closure variable, so a caller that passes no owner
  // and no renderContext -- which is exactly what the custom-post packer
  // does -- still gets a live clock.
  assert.match(
    source,
    /packSelenaUniforms\(\{ customUniforms: effect\.uniforms, shaderLayout: effect\.shaderLayout \}\)/,
    "the custom-post packer call shape changed; re-verify the time path"
  );
});

test("the custom-post packer is the renderer's own Selena packer", () => {
  // wgpuCreatePostProcessor is a SIBLING of the renderer closure, so the
  // packer has to be injected. If it were called by bare name it would throw
  // ReferenceError, and if a different packer were injected it would not see
  // sceneSelenaFrameTime.
  assert.match(
    source,
    /wgpuCreatePostProcessor\(device, targetFormat, reportWebGPUFrameError, sceneSelenaUniformData\)/,
    "the post processor must receive sceneSelenaUniformData as its packer"
  );
});

test("the frame clock is written before the post chain applies", () => {
  const clockWrite = source.indexOf("sceneSelenaFrameTime = frameTimeSeconds");
  assert.ok(clockWrite >= 0, "the per-frame clock write disappeared");
  const postApply = source.indexOf("postProcessor.apply(encoder, postEffects");
  assert.ok(postApply >= 0, "the post chain dispatch disappeared");
  assert.ok(
    clockWrite < postApply,
    "the frame clock must be written before the post chain reads it; " +
      "otherwise a time-gated custom post samples the PREVIOUS frame's clock " +
      "on frame one and zero before that"
  );
});
