"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const {
  buildMinimalGLBBytes,
  buildSkinnedGLBBytes,
  bootstrapSource,
  createContext,
  FakeElement,
  FakeWebGLContext,
  flushAsyncWork,
  createBoardWebGPUHarness,
  createWebGLRendererForPost,
  freshFeatureBundleSource,
  mainRenderPasses,
  runScript,
} = require("./runtime-test-harness.js");

const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };

test("generated runtime batches imported actors through the mounted browser path", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "mounted-crowd";
  const env = createContext({ elements: [mount], enableWebGL2: true,
    fetchRoutes: { "/crowd.glb": { bytes: buildMinimalGLBBytes() } },
    manifest: { engines: [{ id: "crowd-engine", component: "GoSXScene3D", kind: "surface", mountId: mount.id,
      props: { width: 320, height: 180, forceWebGL: true, scene: { objects: [], instancedGLBMeshes: [{ id: "crowd", src: "/crowd.glb", instances: [{id:"a",x:-1},{id:"b",x:1}] }] } },
    }] },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl.ops.some(op => op[0] === "drawArraysInstanced" && op.at(-1) === 2), "mounted generated runtime must batch both actors: " + JSON.stringify(env.context.__gosx_scene3d_debug.inspect(mount.id)));
  assert.equal(gl.ops.filter(op => op[0] === "drawArrays" || op[0] === "drawElements").length, 0,
    "the shared prepared-pass planner must not also draw the individual colour records");
  assert.equal(env.consoleLogs.error.length, 0);
  const snapshot = env.context.__gosx_scene3d_debug.inspect(mount.id);
  assert.equal(snapshot.rigidGLBBatching.batches, 1);
  assert.equal(snapshot.counts.drawCalls, 1, "shadow/pick records must not masquerade as colour draws");
});

async function loadCrowd(harness, count = 32, offset = 0) {
  const env = harness.env;
  const api = env.context.__gosx_scene3d_api;
  if (!harness.crowdState) {
    // Exercise the same split chunks used by the production browser.
    for (const name of ["scene3d-gltf", "scene3d-hydrate", "scene3d-command"]) {
      runScript(freshFeatureBundleSource(name), env.context, name + ".js");
    }
    const glb = Uint8Array.from(harness.crowdGLBBytes || buildMinimalGLBBytes()).buffer;
    const previousFetch = env.context.fetch;
    env.context.fetch = function(url, options) {
      if (String(url).endsWith("/crowd.glb")) {
        return Promise.resolve({ ok: true, status: 200, arrayBuffer: async () => glb });
      }
      return previousFetch(url, options);
    };
    harness.crowdState = api.createSceneState({ scene: { objects: [] }, camera: { x: 0, y: 0, z: 30, fov: 60, near: 0.1, far: 100 } }, { tier: "full" });
  }
  await api.applySceneCommands(harness.crowdState, [{ kind: 11, data: { instancedGLBMeshes: [{
    id: "crowd", src: "/crowd.glb", instances: Array.from({ length: count }, (_, i) => ({ id: "actor-" + i, x: (i % 8) * 2 - 7 + offset, y: Math.floor(i / 8), scaleX: 1, scaleY: 1.5, scaleZ: 0.8 })),
  }] } }]);
  assert.equal(harness.crowdState.objects.size, count * (harness.primitiveCount || 1), "the real GLB loader must hydrate every actor: " + JSON.stringify(harness.warnLog || []));
  return [...harness.crowdState.objects.values()];
}

function bundleFor(harness, objects, caps = true, lights = []) {
  return harness.env.context.__gosx_scene3d_api.createSceneRenderBundle(
    320, 180, "#000000", harness.crowdState.camera, objects, [], [], [], lights, {}, 0,
    [], [], [], [], [], 0, false, { retainedGeometry: true, rigidGLBInstancing: caps },
  );
}

test("imported rigid GLB actors share geometry and emit one colour batch", async () => {
  const h = createWebGLRendererForPost({ fresh: true });
  try {
    let actors = await loadCrowd(h, 64);
    const geometry = actors[0]._rigidGLBGeometry;
    assert.ok(geometry, "imported geometry must enter the shared path");
    assert.ok(actors.every(actor => actor.vertices === geometry.vertices));
    let bundle = bundleFor(h, actors);
    assert.equal(bundle.instancedMeshes.length, 1);
    assert.equal(bundle.instancedMeshes[0].count, 64);
    assert.equal(bundle.worldMeshPositions.length, 0, "moving actors must not bake world-space vertex soup");
    assert.equal(bundle.meshObjects.length, 64, "preserve per-actor picking and shadow records");
    assert.ok(bundle.meshObjects.every(record => record._colorInstanced && record.modelMatrix));
    const api = h.env.context.__gosx_scene3d_api;
    const prepared = api.prepareScene(bundle, h.crowdState.camera, viewport);
    assert.equal(prepared.pbrPasses.opaque.length, 0, "prepared colour passes exclude batch-owned actors");
    bundle.meshObjects[0]._colorInstanced = false;
    const ordinary = api.prepareScene(bundle, h.crowdState.camera, viewport, prepared);
    assert.notEqual(ordinary.signature, prepared.signature, "colour ownership invalidates cached pass planning");
    assert.equal(ordinary.pbrPasses.opaque.length, 1);
    const oldX = bundle.instancedMeshes[0].transforms[12];
    actors = await loadCrowd(h, 64, 0.75);
    assert.strictEqual(actors[0]._rigidGLBGeometry, geometry, "rehydration must retain the decoded geometry");
    bundle = bundleFor(h, actors);
    assert.equal(bundle.instancedMeshes[0].transforms[12], oldX + 0.75);
  } finally { h.renderer.dispose(); }
});

test("WebGL crowd pose updates reuse transform buffers and retire removed batches", async () => {
  const h = createWebGLRendererForPost({ fresh: true });
  try {
    const gl = h.canvas.getContext("webgl2");
    for (let frame = 0; frame < 12; frame++) {
      const actors = await loadCrowd(h, 32, frame * 0.01);
      h.renderer.render(bundleFor(h, actors), viewport);
    }
    const draws = gl.ops.filter(op => op[0] === "drawArraysInstanced");
    assert.equal(draws.length, 12, "one draw per primitive per frame");
    assert.ok(draws.every(op => op.at(-1) === 32));
    const streams = gl.ops.filter(op => op[0] === "bufferSubData" && op[3] === 0);
    assert.ok(streams.length >= 12, "pose changes must stream into existing buffers");
    const before = gl.ops.filter(op => op[0] === "deleteBuffer").length;
    const actors = await loadCrowd(h, 0);
    h.renderer.render(bundleFor(h, actors), viewport);
    assert.ok(gl.ops.filter(op => op[0] === "deleteBuffer").length > before, "removed crowd streams must be retired");
  } finally { h.renderer.dispose(); }
});

test("WebGPU crowd draws retain GPU allocations across moving declaration records", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    let actors = await loadCrowd(h, 32);
    h.renderer.render(bundleFor(h, actors), viewport);
    const firstAllocations = h.fake.state.buffers.length;
    for (let frame = 1; frame <= 8; frame++) {
      actors = await loadCrowd(h, 32, frame * 0.01);
      h.renderer.render(bundleFor(h, actors), viewport);
    }
    const draws = mainRenderPasses(h.fake).flatMap(pass => pass.draws);
    assert.ok(draws.some(draw => draw.instanceCount === 32), "the real WebGPU renderer must issue an instanced draw");
    assert.equal(h.fake.state.buffers.length, firstAllocations, "steady-state poses must not allocate GPU buffers");
    const buffers = h.fake.state.buffers.slice();
    actors = await loadCrowd(h, 0);
    h.renderer.render(bundleFor(h, actors), viewport);
    assert.ok(buffers.some(buffer => buffer.destroyed), "removing a crowd must retire its renderer-owned GPU allocations");
  } finally { h.renderer.dispose(); }
});

test("transparent, selected and backend fallback actors preserve ordinary mesh rendering", async () => {
  const h = createWebGLRendererForPost({ fresh: true });
  try {
    const actors = await loadCrowd(h, 3);
    const fallback = bundleFor(h, actors, false);
    assert.equal(fallback.instancedMeshes.length, 0);
    assert.equal(fallback.meshObjects.length, 3);
    assert.ok(fallback.meshObjects.every(object => !object._colorInstanced));
    actors[0].selected = true;
    actors[1].renderPass = "alpha";
    actors[1]._renderPassDerived = false;
    const mixed = bundleFor(h, actors);
    assert.equal(mixed.instancedMeshes.length, 1);
    assert.equal(mixed.instancedMeshes[0].count, 1);
    assert.equal(mixed.meshObjects.filter(object => !object._colorInstanced).length, 2);
    assert.equal(mixed.meshObjects.filter(object => object.renderPass === "alpha").length, 1);
  } finally { h.renderer.dispose(); }
});

test("batched colour preserves individual retained shadow casters", async () => {
  const h = createWebGLRendererForPost({ fresh: true });
  try {
    const actors = await loadCrowd(h, 2);
    actors.forEach(actor => { actor.castShadow = true; actor.receiveShadow = true; });
    const bundle = bundleFor(h, actors);
    assert.equal(bundle.instancedMeshes.length, 1);
    assert.equal(bundle.instancedMeshes[0].castShadow, false, "shadow ownership stays with the original actors");
    assert.equal(bundle.meshObjects.filter(record => record.castShadow && record.retainedGeometry).length, 2);
    assert.notEqual(bundle.meshObjects[0].modelMatrix[12], bundle.meshObjects[1].modelMatrix[12]);
  } finally { h.renderer.dispose(); }
});

function modifyGLB(bytes, modify) {
  const input = Buffer.from(bytes);
  const length = input.readUInt32LE(12);
  const root = JSON.parse(input.subarray(20, 20 + length).toString());
  modify(root);
  let json = Buffer.from(JSON.stringify(root));
  while (json.length % 4) json = Buffer.concat([json, Buffer.from(" ")]);
  const tail = input.subarray(20 + length);
  const result = Buffer.alloc(20 + json.length + tail.length);
  input.copy(result, 0, 0, 20);
  result.writeUInt32LE(result.length, 8);
  result.writeUInt32LE(json.length, 12);
  json.copy(result, 20);
  tail.copy(result, 20 + json.length);
  return result;
}

test("imported actors preserve separate primitive and material batches", async () => {
  const h = createWebGLRendererForPost({ fresh: true });
  h.primitiveCount = 2;
  h.crowdGLBBytes = modifyGLB(buildMinimalGLBBytes(), root => {
    root.materials.push({pbrMetallicRoughness:{baseColorFactor:[1,0.1,0.2,1],roughnessFactor:0.3}});
    root.meshes[0].primitives.push({...root.meshes[0].primitives[0],material:1});
  });
  try {
    const actors = await loadCrowd(h, 8);
    const bundle = bundleFor(h, actors);
    assert.equal(bundle.instancedMeshes.length, 2);
    assert.ok(bundle.instancedMeshes.every(batch => batch.count === 8));
    assert.notEqual(bundle.instancedMeshes[0].materialIndex, bundle.instancedMeshes[1].materialIndex);
    h.renderer.render(bundle, viewport);
    assert.equal(h.canvas.getContext("webgl2").ops.filter(op => op[0] === "drawArraysInstanced").length, 2);
  } finally { h.renderer.dispose(); }
});

test("skin, animated morph and node clips keep their animation-owned geometry", async () => {
  function withoutSkin(root) {
    delete root.skins;
    root.nodes.forEach(node => { delete node.skin; });
    delete root.meshes[0].primitives[0].attributes.JOINTS_0;
    delete root.meshes[0].primitives[0].attributes.WEIGHTS_0;
  }
  for (const [label, bytes] of [["skin", buildSkinnedGLBBytes()], ["morph", modifyGLB(buildSkinnedGLBBytes(), root => {
    withoutSkin(root);
    root.meshes[0].primitives[0].targets = [{POSITION:0}];
    root.meshes[0].weights = [0.5];
    root.animations[0].samplers[0].output = 6;
    root.animations[0].channels[0].target = {node:0,path:"weights"};
  })], ["node", modifyGLB(buildSkinnedGLBBytes(), root => {
    withoutSkin(root);
    root.animations[0].channels[0].target = {node:0,path:"translation"};
  })]]) {
    const h = createWebGLRendererForPost({ fresh: true });
    h.crowdGLBBytes = bytes;
    try {
      const actors = await loadCrowd(h, 3);
      assert.ok(actors.every(actor => !actor._rigidGLBGeometry), label + " geometry must retain animation ownership");
      assert.equal(bundleFor(h, actors).instancedMeshes.length, 0);
      assert.notStrictEqual(actors[0].vertices, actors[1].vertices);
    } finally { h.renderer.dispose(); }
  }
});

test("WebGPU batched colour keeps distinct per-actor shadow matrix slots", async () => {
  const h = await createBoardWebGPUHarness({ fresh: true });
  try {
    h.env.context.__gosx_scene3d_webgpu_render_bundles = false;
    const actors = await loadCrowd(h, 2);
    actors.forEach(actor => { actor.castShadow = true; });
    const light = h.env.context.__gosx_scene3d_api.normalizeSceneLight({id:"sun",kind:"directional",x:4,y:6,z:8,intensity:1,castShadow:true},0,null);
    h.renderer.render(bundleFor(h, actors, true, [light]), viewport);
    const pass = h.fake.state.renderPasses.find(pass => pass.descriptor?.colorAttachments?.length === 0 && pass.draws.length === 2);
    assert.ok(pass, "both actors must draw into a depth-only shadow pass");
    const offsets = pass.bindGroups.flatMap(binding => binding.dynamicOffsets || []).filter(offset => offset > 0);
    assert.equal(new Set(offsets).size, 2, "shared geometry must not collapse distinct caster matrices");
  } finally { h.renderer.dispose(); }
});
