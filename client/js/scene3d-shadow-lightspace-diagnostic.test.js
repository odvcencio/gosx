"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { bootstrapRuntimeSource, createContext, freshFeatureBundleSource, runScript } = require("./runtime-test-harness.js");

function loadSceneAndWebGLAPI() {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  runScript(freshFeatureBundleSource("scene3d-webgl"), env.context, "bootstrap-feature-scene3d-webgl.js");
  return {
    api: env.context.__gosx_scene3d_api,
    resourceAPI: env.context.__gosx_scene3d_resource_api,
  };
}

function transformPoint(matrix, point) {
  const [x, y, z] = point;
  const cx = matrix[0] * x + matrix[4] * y + matrix[8] * z + matrix[12];
  const cy = matrix[1] * x + matrix[5] * y + matrix[9] * z + matrix[13];
  const cz = matrix[2] * x + matrix[6] * y + matrix[10] * z + matrix[14];
  const cw = matrix[3] * x + matrix[7] * y + matrix[11] * z + matrix[15];
  return { x: cx / cw, y: cy / cw, z: cz / cw, w: cw };
}

function insideGLClip(clip) {
  return clip.w !== 0
    && Math.abs(clip.x) <= 1.0001
    && Math.abs(clip.y) <= 1.0001
    && Math.abs(clip.z) <= 1.0001;
}

test("G1b single-map light-space matrix contains all scene AABB corners and center in GL depth", () => {
  const { api } = loadSceneAndWebGLAPI();
  for (const [name, light, bounds] of [
    ["front-z", { directionX: 0, directionY: 0, directionZ: -1 }, { minX: -1.6, maxX: 1.6, minY: -1.6, maxY: 1.6, minZ: 0, maxZ: 1.12 }],
    ["diagonal", { directionX: 0.35, directionY: -0.55, directionZ: -0.76 }, { minX: -2, maxX: 1, minY: -0.5, maxY: 1.5, minZ: -3, maxZ: 0.8 }],
  ]) {
    const matrix = api.sceneShadowLightSpaceMatrix(light, bounds);
    const points = aabbPoints(bounds);
    points.push([
      (bounds.minX + bounds.maxX) * 0.5,
      (bounds.minY + bounds.maxY) * 0.5,
      (bounds.minZ + bounds.maxZ) * 0.5,
    ]);
    const clips = points.map((point) => transformPoint(matrix, point));
    assert.ok(clips.every(insideGLClip), name + " clips=" + JSON.stringify(clips));
    const center = clips[clips.length - 1];
    assert.ok(center.z > -1 && center.z < 1, name + " center=" + JSON.stringify(center));
  }
});

function aabbPoints(bounds) {
  const out = [];
  for (const x of [bounds.minX, bounds.maxX]) {
    for (const y of [bounds.minY, bounds.maxY]) {
      for (const z of [bounds.minZ, bounds.maxZ]) {
        out.push([x, y, z]);
      }
    }
  }
  return out;
}

test("G1b CSM light-space fit contains the receiver and upstream caster when extension is present", () => {
  const { api, resourceAPI } = loadSceneAndWebGLAPI();
  const camera = { x: 0, y: 0, z: 6, fov: 36, near: 5.5, far: 6.5 };
  const view = api.scenePBRViewMatrix(camera);
  const corners = resourceAPI.frustumSubCorners(view, camera.fov, 1, camera.near, camera.far);
  const radius = Math.sqrt(1.6 * 1.6 + 1.6 * 1.6 + 0.56 * 0.56);
  const extended = resourceAPI.fitLightSpaceOrtho([0, 0, -1], corners, radius, 512);
  const unextended = resourceAPI.fitLightSpaceOrtho([0, 0, -1], corners, 0, 512);
  const receiver = transformPoint(extended, [0, 0, 0]);
  const nearCasterFront = transformPoint(extended, [0, 0, 1.12]);
  const nearCasterBack = transformPoint(extended, [0, 0, 0.88]);
  const unextendedCaster = transformPoint(unextended, [0, 0, 1.12]);

  assert.equal(insideGLClip(receiver), true, JSON.stringify({ receiver, nearCasterFront, nearCasterBack, unextendedCaster }));
  assert.equal(insideGLClip(nearCasterFront), true, JSON.stringify({ receiver, nearCasterFront, nearCasterBack, unextendedCaster }));
  assert.equal(insideGLClip(nearCasterBack), true, JSON.stringify({ receiver, nearCasterFront, nearCasterBack, unextendedCaster }));
  assert.equal(insideGLClip(unextendedCaster), false, JSON.stringify({ receiver, nearCasterFront, nearCasterBack, unextendedCaster }));
  assert.ok(nearCasterFront.z < nearCasterBack.z, JSON.stringify({ receiver, nearCasterFront, nearCasterBack, unextendedCaster }));
});
