"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

const {
  bootstrapRuntimeSource,
  createContext,
  createBoardWebGPUHarness,
  createWebGLRendererForPost,
  freshFeatureBundleSource,
  runScript,
} = require("./runtime-test-harness.js");

function retainedTriangle(overrides, Float32Ctor) {
  const F32 = Float32Ctor || Float32Array;
  const vertices = {
    count: 3,
    positions: new F32([-1, -1, 0, 1, -1, 0, 0, 1, 0]),
    normals: new F32([0, 0, 1, 0, 0, 1, 0, 0, 1]),
    uvs: new F32([0, 0, 1, 0, 0.5, 1]),
    tangents: new F32([1, 0, 0, 1, 1, 0, 0, 1, 1, 0, 0, 1]),
    immutable: true,
    revision: 0,
    dynamic: false,
  };
  return Object.assign({
    id: "retained-triangle",
    kind: "mesh",
    x: 2,
    y: 0,
    z: 0,
    scaleX: 1,
    scaleY: 1,
    scaleZ: 1,
    rotationX: 0,
    rotationY: 0,
    rotationZ: 0,
    spinX: 0,
    spinY: 0,
    spinZ: 0.5,
    driftPhase: 0,
    driftSpeed: 0,
    shiftX: 0,
    shiftY: 0,
    shiftZ: 0,
    materialKind: "standard",
    color: "#8de1ff",
    opacity: 1,
    roughness: 0.5,
    metalness: 0,
    wireframe: false,
    castShadow: false,
    receiveShadow: true,
    vertices,
  }, overrides || {});
}

function obliqueTriangle(overrides, Float32Ctor) {
  const F32 = Float32Ctor || Float32Array;
  const invSqrt3 = 1 / Math.sqrt(3);
  const invSqrt2 = 1 / Math.sqrt(2);
  const vertices = {
    count: 3,
    positions: new F32([
      1, -1, 0,
      0, 1, -1,
      -1, 0, 1,
    ]),
    normals: new F32([
      invSqrt3, invSqrt3, invSqrt3,
      invSqrt3, invSqrt3, invSqrt3,
      invSqrt3, invSqrt3, invSqrt3,
    ]),
    uvs: new F32([1, 0, 0, 1, 0, 0]),
    tangents: new F32([
      invSqrt2, -invSqrt2, 0, 1,
      invSqrt2, -invSqrt2, 0, 1,
      invSqrt2, -invSqrt2, 0, 1,
    ]),
    immutable: true,
    revision: 0,
    dynamic: false,
  };
  return retainedTriangle(Object.assign({
    id: "oblique-triangle",
    x: 0,
    spinZ: 0,
    vertices,
  }, overrides || {}), F32);
}

function vec3At(values, index) {
  const offset = index * 3;
  return [values[offset], values[offset + 1], values[offset + 2]];
}

function vec4At(values, index) {
  const offset = index * 4;
  return [values[offset], values[offset + 1], values[offset + 2], values[offset + 3]];
}

function normalized(values) {
  const length = Math.hypot(values[0], values[1], values[2]);
  return values.map((value) => value / length);
}

function subtract(left, right) {
  return [left[0] - right[0], left[1] - right[1], left[2] - right[2]];
}

function cross(left, right) {
  return [
    left[1] * right[2] - left[2] * right[1],
    left[2] * right[0] - left[0] * right[2],
    left[0] * right[1] - left[1] * right[0],
  ];
}

function dot(left, right) {
  return left[0] * right[0] + left[1] * right[1] + left[2] * right[2];
}

function assertVectorClose(actual, expected, message) {
  assert.equal(actual.length, expected.length, message);
  for (let index = 0; index < actual.length; index += 1) {
    assert.ok(
      Math.abs(actual[index] - expected[index]) <= 1e-5,
      `${message || "vector"}[${index}] = ${actual[index]}, want ${expected[index]}`,
    );
  }
}

function renderBundle(api, object, timeSeconds, waterSystems, retainedGeometry) {
  return api.createSceneRenderBundle(
    320,
    180,
    "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    [object],
    [],
    [],
    [],
    [],
    {},
    timeSeconds || 0,
    [],
    [],
    [],
    waterSystems || [],
    [],
    0,
    false,
    { retainedGeometry: retainedGeometry !== false },
  );
}

function latestPBRMaterialBuffer(fake) {
  for (let index = fake.state.bindGroups.length - 1; index >= 0; index -= 1) {
    const group = fake.state.bindGroups[index];
    // The material bind group now carries 17 entries to the frame group's
    // 15, so entry count alone misclassifies groups. Identify the real
    // PBR material group by its gosx-material bind group layout descriptor.
    const layout = group && group.desc && group.desc.layout;
    const layoutDesc = layout && layout.desc;
    if (!layoutDesc || layoutDesc.label !== "gosx-material") continue;
    const entries = group.desc.entries;
    if (!Array.isArray(entries)) continue;
    const uniform = entries.find((entry) => entry.binding === 0);
    if (uniform && uniform.resource && uniform.resource.buffer) {
      return uniform.resource.buffer;
    }
  }
  return null;
}

function loadFreshSceneAPI() {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  return { env, api: env.context.__gosx_scene3d_api };
}

test("Scene3D retains eligible local mesh arrays and updates only compact transform state", () => {
  const { env, api } = loadFreshSceneAPI();
  const object = retainedTriangle({}, vm.runInContext("Float32Array", env.context));
  const first = renderBundle(api, object, 0);
  const firstMatrix = Array.from(first.meshObjects[0].modelMatrix);
  const second = renderBundle(api, object, 1);
  const retained = second.meshObjects[0];

  assert.equal(first.retainedMeshObjectCount, 1);
  assert.equal(first.retainedMeshVertexCount, 3);
  assert.equal(first.worldBakedMeshObjectCount, 0);
  assert.equal(first.worldMeshPositions.length, 0);
  assert.equal(retained.retainedGeometry, true);
  assert.equal(retained.directVertices, true);
  assert.equal(retained.vertices, object.vertices);
  assert.equal(retained.resourceOwner, object);
  assert.equal(retained.modelMatrix, first.meshObjects[0].modelMatrix, "model matrix storage should be reused");
  assert.notDeepEqual(Array.from(retained.modelMatrix), firstMatrix, "animated transform values should still update");
  assert.deepEqual(
    { minX: retained.bounds.minX, maxX: retained.bounds.maxX },
    {
      minX: Math.min(
        retained.modelMatrix[0] * -1 + retained.modelMatrix[4] * -1 + retained.modelMatrix[12],
        retained.modelMatrix[0] * 1 + retained.modelMatrix[4] * -1 + retained.modelMatrix[12],
        retained.modelMatrix[0] * -1 + retained.modelMatrix[4] * 1 + retained.modelMatrix[12],
        retained.modelMatrix[0] * 1 + retained.modelMatrix[4] * 1 + retained.modelMatrix[12],
      ),
      maxX: Math.max(
        retained.modelMatrix[0] * -1 + retained.modelMatrix[4] * -1 + retained.modelMatrix[12],
        retained.modelMatrix[0] * 1 + retained.modelMatrix[4] * -1 + retained.modelMatrix[12],
        retained.modelMatrix[0] * -1 + retained.modelMatrix[4] * 1 + retained.modelMatrix[12],
        retained.modelMatrix[0] * 1 + retained.modelMatrix[4] * 1 + retained.modelMatrix[12],
      ),
    },
  );
});

test("Scene3D retained geometry uses explicit semantic fallbacks", () => {
  const { env, api } = loadFreshSceneAPI();
  const F32 = vm.runInContext("Float32Array", env.context);
  const cases = [
    retainedTriangle({ id: "dynamic", geometryDirty: true }, F32),
    retainedTriangle({ id: "wire", wireframe: true }, F32),
    retainedTriangle({ id: "custom", customVertex: "void main() {}" }, F32),
    retainedTriangle({ id: "morph", computedMorph: { alpha: 0.5 } }, F32),
    retainedTriangle({ id: "mutable", vertices: Object.assign({}, retainedTriangle({}, F32).vertices, { immutable: false }) }, F32),
    retainedTriangle({ id: "revisionless", vertices: Object.assign({}, retainedTriangle({}, F32).vertices, { revision: null }) }, F32),
    retainedTriangle({ id: "reflected-scale", scaleX: -1 }, F32),
  ];
  for (const object of cases) {
    const bundle = renderBundle(api, object, 0);
    assert.equal(bundle.retainedMeshObjectCount, 0, object.id);
    assert.equal(bundle.worldBakedMeshObjectCount, 1, object.id);
    assert.equal(bundle.worldMeshPositions.length, 9, object.id);
  }

  const waterBundle = renderBundle(api, retainedTriangle({ id: "water-subject" }, F32), 0, [{ id: "pool" }]);
  assert.equal(waterBundle.retainedMeshObjectCount, 0);
  assert.equal(waterBundle.worldBakedMeshObjectCount, 1);

  const backendNeutral = renderBundle(api, retainedTriangle({ id: "backend-neutral" }, F32), 0, [], false);
  assert.equal(backendNeutral.retainedMeshObjectCount, 0);
  assert.equal(backendNeutral.worldBakedMeshObjectCount, 1);
  assert.equal(backendNeutral.worldMeshPositions.length, 9);
});

test("Scene3D retains unindexed shadow casters with positive non-uniform scale", () => {
  const { env, api } = loadFreshSceneAPI();
  const object = retainedTriangle({
    id: "scaled-shadow-caster",
    castShadow: true,
    scaleX: 2,
    scaleY: 0.25,
    scaleZ: 1.5,
  }, vm.runInContext("Float32Array", env.context));
  const bundle = renderBundle(api, object, 0);

  assert.equal(bundle.retainedMeshObjectCount, 1);
  assert.equal(bundle.retainedMeshVertexCount, 3);
  assert.equal(bundle.worldBakedMeshObjectCount, 0);
  assert.equal(bundle.worldMeshPositions.length, 0);
  assert.equal(bundle.meshObjects[0].retainedGeometry, true);
  assert.equal(bundle.meshObjects[0].castShadow, true);
  assert.deepEqual(
    {
      minX: bundle.meshObjects[0].bounds.minX,
      minY: bundle.meshObjects[0].bounds.minY,
      maxX: bundle.meshObjects[0].bounds.maxX,
      maxY: bundle.meshObjects[0].bounds.maxY,
    },
    { minX: 0, minY: -0.25, maxX: 4, maxY: 0.25 },
  );
});

test("Scene3D retains a generated flattened sphere shadow caster end to end", () => {
  const { api } = loadFreshSceneAPI();
  const object = api.normalizeSceneObject({
    id: "board-socket",
    kind: "sphere",
    radius: 0.3,
    segments: 20,
    scale: { x: 1, y: 0.28, z: 1 },
    materialKind: "standard",
    color: "#071019",
    wireframe: false,
    castShadow: true,
    receiveShadow: true,
  }, 0, null);
  const bundle = renderBundle(api, object, 0);

  assert.equal(object.vertices.count, 1080, "20-segment sphere keeps its existing tessellation");
  assert.equal(object.vertices.tangents.length, object.vertices.count * 4, "primitive lowering completes tangent frames once");
  assert.equal(bundle.retainedMeshObjectCount, 1);
  assert.equal(bundle.retainedMeshVertexCount, object.vertices.count);
  assert.equal(bundle.worldBakedMeshObjectCount, 0);
  assert.equal(bundle.worldBakedMeshVertexCount, 0);
  assert.equal(bundle.worldMeshPositions.length, 0);
});

test("Scene3D world-baked non-uniform transforms use inverse-transpose normals and linear tangents", () => {
  const { env, api } = loadFreshSceneAPI();
  const object = obliqueTriangle({ scaleX: 2, scaleY: 3, scaleZ: 4 }, vm.runInContext("Float32Array", env.context));
  const bundle = renderBundle(api, object, 0, [], false);
  const mesh = bundle.meshObjects[0];
  const expectedNormal = normalized([1 / 2, 1 / 3, 1 / 4]);
  const expectedTangent = normalized([2, -3, 0]);

  assert.equal(bundle.retainedMeshObjectCount, 0);
  assert.equal(bundle.worldBakedMeshObjectCount, 1);
  assert.equal(mesh.viewCulled, false);
  assertVectorClose(vec3At(bundle.worldMeshPositions, 0), [2, -3, 0], "positive determinant keeps vertex 0");
  assertVectorClose(vec3At(bundle.worldMeshPositions, 1), [0, 3, -4], "positive determinant keeps vertex 1");
  assert.deepEqual(
    {
      minX: mesh.bounds.minX, minY: mesh.bounds.minY, minZ: mesh.bounds.minZ,
      maxX: mesh.bounds.maxX, maxY: mesh.bounds.maxY, maxZ: mesh.bounds.maxZ,
    },
    { minX: -2, minY: -3, minZ: -4, maxX: 2, maxY: 3, maxZ: 4 },
  );

  for (let vertex = 0; vertex < 3; vertex += 1) {
    const normal = vec3At(bundle.worldMeshNormals, vertex);
    const tangent = vec4At(bundle.worldMeshTangents, vertex);
    assertVectorClose(normal, expectedNormal, `normal ${vertex}`);
    assertVectorClose(tangent.slice(0, 3), expectedTangent, `tangent ${vertex}`);
    assert.ok(Math.abs(dot(normal, tangent)) <= 1e-5, `tangent ${vertex} must stay orthogonal`);
    assert.equal(tangent[3], 1);
  }
  const geometricNormal = normalized(cross(
    subtract(vec3At(bundle.worldMeshPositions, 1), vec3At(bundle.worldMeshPositions, 0)),
    subtract(vec3At(bundle.worldMeshPositions, 2), vec3At(bundle.worldMeshPositions, 0)),
  ));
  assert.ok(dot(geometricNormal, expectedNormal) > 0.99999, "CCW face and shading normal must agree");

  const hit = api.sceneRaycastPick(160, 90, 320, 180, bundle.camera, bundle);
  assert.ok(hit, "center ray must survive transformed AABB and triangle narrow phases");
  assert.equal(hit.triangleIndex, 0);
  assertVectorClose([hit.worldPosition.x, hit.worldPosition.y, hit.worldPosition.z], [0, 0, 0], "pick position");
  assertVectorClose([hit.uv.x, hit.uv.y], [1 / 3, 1 / 3], "pick UV");
});

test("Scene3D world-baked reflections preserve CCW faces and tangent-frame handedness", () => {
  const { env, api } = loadFreshSceneAPI();
  const F32 = vm.runInContext("Float32Array", env.context);
  const cases = [
    { id: "single-negative", scale: [-2, 3, 4], reversed: true, handedness: -1 },
    { id: "double-negative", scale: [-2, -3, 4], reversed: false, handedness: 1 },
    { id: "triple-negative", scale: [-2, -3, -4], reversed: true, handedness: -1 },
  ];
  const sourcePositions = [[1, -1, 0], [0, 1, -1], [-1, 0, 1]];

  for (const fixture of cases) {
    const [scaleX, scaleY, scaleZ] = fixture.scale;
    const object = obliqueTriangle({ id: fixture.id, scaleX, scaleY, scaleZ }, F32);
    const bundle = renderBundle(api, object, 0, [], false);
    const mesh = bundle.meshObjects[0];
    const expectedOrder = fixture.reversed ? [0, 2, 1] : [0, 1, 2];
    const expectedNormal = normalized([1 / scaleX, 1 / scaleY, 1 / scaleZ]);
    const expectedTangent = normalized([scaleX, -scaleY, 0]);

    assert.equal(bundle.retainedMeshObjectCount, 0, fixture.id);
    assert.equal(mesh.viewCulled, false, fixture.id);
    for (let vertex = 0; vertex < 3; vertex += 1) {
      const source = sourcePositions[expectedOrder[vertex]];
      assertVectorClose(
        vec3At(bundle.worldMeshPositions, vertex),
        [source[0] * scaleX, source[1] * scaleY, source[2] * scaleZ],
        `${fixture.id} position ${vertex}`,
      );
      const normal = vec3At(bundle.worldMeshNormals, vertex);
      const tangent = vec4At(bundle.worldMeshTangents, vertex);
      assertVectorClose(normal, expectedNormal, `${fixture.id} normal ${vertex}`);
      assertVectorClose(tangent.slice(0, 3), expectedTangent, `${fixture.id} tangent ${vertex}`);
      assert.ok(Math.abs(dot(normal, tangent)) <= 1e-5, `${fixture.id} tangent ${vertex} must stay orthogonal`);
      assert.equal(tangent[3], fixture.handedness, `${fixture.id} tangent handedness`);
    }
    assertVectorClose(
      Array.from(bundle.worldMeshUVs.slice(2, 4)),
      fixture.reversed ? [0, 0] : [0, 1],
      `${fixture.id} UV order follows position order`,
    );
    const geometricNormal = normalized(cross(
      subtract(vec3At(bundle.worldMeshPositions, 1), vec3At(bundle.worldMeshPositions, 0)),
      subtract(vec3At(bundle.worldMeshPositions, 2), vec3At(bundle.worldMeshPositions, 0)),
    ));
    assert.ok(dot(geometricNormal, expectedNormal) > 0.99999, `${fixture.id} must remain CCW/outward`);

    const hit = api.sceneRaycastPick(160, 90, 320, 180, bundle.camera, bundle);
    assert.ok(hit, `${fixture.id} remains pickable`);
    assert.equal(hit.triangleIndex, 0, fixture.id);
    assertVectorClose([hit.worldPosition.x, hit.worldPosition.y, hit.worldPosition.z], [0, 0, 0], `${fixture.id} pick`);
    assert.deepEqual(
      {
        minX: mesh.bounds.minX, minY: mesh.bounds.minY, minZ: mesh.bounds.minZ,
        maxX: mesh.bounds.maxX, maxY: mesh.bounds.maxY, maxZ: mesh.bounds.maxZ,
      },
      { minX: -2, minY: -3, minZ: -4, maxX: 2, maxY: 3, maxZ: 4 },
      `${fixture.id} bounds`,
    );
  }
});

test("Scene3D retained mesh CPU picking transforms local positions on demand", () => {
  const { env, api } = loadFreshSceneAPI();
  const object = retainedTriangle({ x: 0, spinZ: 0 }, vm.runInContext("Float32Array", env.context));
  const bundle = renderBundle(api, object, 0);
  const hit = api.sceneRaycastPick(160, 90, 320, 180, bundle.camera, bundle);

  assert.ok(hit, "center ray should hit retained local triangle");
  assert.equal(hit.object.retainedGeometry, true);
  assert.equal(hit.triangleIndex, 0);
  assert.ok(Math.abs(hit.worldPosition.z) < 1e-6);
});

test("Scene3D WebGL retained meshes upload static attributes once across animated frames", () => {
  const harness = createWebGLRendererForPost({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  const object = retainedTriangle({}, vm.runInContext("Float32Array", harness.env.context));
  const gl = harness.canvas.getContext("webgl2");
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  harness.env.document.body.appendChild(harness.canvas);

  harness.renderer.render(renderBundle(api, object, 0), viewport);
  const firstStaticUploads = gl.ops.filter((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW).length;
  const firstModelUploads = gl.ops.filter((entry) => entry[0] === "uniformMatrix4fv" && entry[1] === "u_modelMatrix").length;
  harness.renderer.render(renderBundle(api, object, 1), viewport);
  const secondStaticUploads = gl.ops.filter((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW).length;
  const secondModelUploads = gl.ops.filter((entry) => entry[0] === "uniformMatrix4fv" && entry[1] === "u_modelMatrix").length;

  assert.equal(secondStaticUploads, firstStaticUploads, "unchanged retained attributes must not re-upload");
  assert.ok(secondModelUploads > firstModelUploads, "animated retained draw must upload its compact model matrix");
  assert.equal(harness.canvas.parentNode.getAttribute("data-gosx-scene3d-retained-mesh-objects"), "1");

  object.vertices.positions[0] = -2;
  object.vertices.revision = 1;
  harness.renderer.render(renderBundle(api, object, 2), viewport);
  const dirtyStaticUploads = gl.ops.filter((entry) => entry[0] === "bufferData" && entry[4] === gl.STATIC_DRAW).length;
  assert.equal(
    dirtyStaticUploads,
    secondStaticUploads + firstStaticUploads,
    "explicit revision must refresh every retained attribute active in the linked program",
  );
  const dirtyStats = harness.renderer.diagnostics().retainedGeometry;
  assert.equal(dirtyStats.revisionInvalidations, 1);
  assert.equal(dirtyStats.cacheEntries, 1);
});

for (const fixture of [
  { label: "source", fresh: true },
  { label: "generated", fresh: false },
]) {
  test(`Scene3D WebGL ${fixture.label} publishes retained/planner telemetry only when values change and keeps mounts isolated`, () => {
    const harness = createWebGLRendererForPost({ fresh: fixture.fresh });
    const api = harness.env.context.__gosx_scene3d_api;
    const F32 = vm.runInContext("Float32Array", harness.env.context);
    const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
    const mountA = harness.env.document.createElement("section");
    const mountB = harness.env.document.createElement("section");
    mountA.appendChild(harness.canvas);
    harness.env.document.body.appendChild(mountA);
    const secondCanvas = harness.env.document.createElement("canvas");
    secondCanvas.width = 320;
    secondCanvas.height = 180;
    mountB.appendChild(secondCanvas);
    harness.env.document.body.appendChild(mountB);
    const backend = api.sceneBackendRegistry.select({
      webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false,
    });
    const secondRenderer = backend.create(secondCanvas, { background: "#000000" }, { tier: "full" });

    const writes = [];
    const originalSetAttribute = mountA.setAttribute;
    mountA.setAttribute = function(name, value) {
      writes.push([name, String(value)]);
      return originalSetAttribute.call(this, name, value);
    };
    const retainedBundle = renderBundle(api, retainedTriangle({}, F32), 0);
    harness.renderer.render(retainedBundle, viewport);
    const firstHits = Number(mountA.getAttribute("data-gosx-scene3d-retained-cache-hits"));

    // A second mount performs one full vertex scan. Renderer A's next retained
    // plan still publishes its own zero delta, not the module-global total.
    secondRenderer.render(renderBundle(api, retainedTriangle({ id: "world-baked" }, F32), 0, [], false), viewport);
    harness.renderer.render(retainedBundle, viewport);

    const writesFor = (name) => writes.filter((entry) => entry[0] === name);
    assert.equal(mountA.getAttribute("data-gosx-scene3d-planner-full-vertex-hash-scans"), "0");
    assert.equal(mountB.getAttribute("data-gosx-scene3d-planner-full-vertex-hash-scans"), "1");
    assert.equal(
      writesFor("data-gosx-scene3d-planner-full-vertex-hash-scans").length,
      1,
      "unchanged per-mount planner telemetry must not rewrite the DOM",
    );
    assert.equal(
      writesFor("data-gosx-scene3d-retained-mesh-objects").length,
      1,
      "unchanged retained counts must not rewrite the DOM",
    );
    assert.ok(Number(mountA.getAttribute("data-gosx-scene3d-retained-cache-hits")) > firstHits);
    assert.equal(
      writesFor("data-gosx-scene3d-retained-cache-hits").length,
      2,
      "changed retained telemetry must publish its new value",
    );

    harness.renderer.dispose();
    secondRenderer.dispose();
  });
}

test("Scene3D WebGL retained caches are renderer-scoped and sweep replaced geometry", () => {
  const harness = createWebGLRendererForPost({ fresh: true });
  const api = harness.env.context.__gosx_scene3d_api;
  const F32 = vm.runInContext("Float32Array", harness.env.context);
  const object = retainedTriangle({}, F32);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const secondCanvas = harness.env.document.createElement("canvas");
  secondCanvas.width = 320;
  secondCanvas.height = 180;
  const backend = api.sceneBackendRegistry.select({ webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false });
  const secondRenderer = backend.create(secondCanvas, { background: "#000000" }, { tier: "full" });

  harness.renderer.render(renderBundle(api, object, 0), viewport);
  secondRenderer.render(renderBundle(api, object, 0), viewport);
  const firstRendererAllocations = harness.renderer.diagnostics().retainedGeometry.allocations;
  assert.ok(firstRendererAllocations >= 3);
  assert.equal(secondRenderer.diagnostics().retainedGeometry.allocations, firstRendererAllocations);
  assert.equal(Object.keys(object.vertices).some((key) => key === "_pbrAttributeBuffers"), false);

  harness.renderer.dispose();
  const beforeSecond = secondRenderer.diagnostics().retainedGeometry;
  secondRenderer.render(renderBundle(api, object, 1), viewport);
  const afterSecond = secondRenderer.diagnostics().retainedGeometry;
  assert.equal(afterSecond.uploadCalls, beforeSecond.uploadCalls, "disposing renderer A must not invalidate renderer B");

  const replacement = retainedTriangle({ id: "replacement" }, F32);
  secondRenderer.render(renderBundle(api, replacement, 2), viewport);
  const replaced = secondRenderer.diagnostics().retainedGeometry;
  assert.equal(replaced.cacheEntries, 1);
  assert.equal(replaced.liveBytes, afterSecond.liveBytes);
  assert.ok(replaced.retirements >= firstRendererAllocations);
  secondRenderer.dispose();
});

for (const fixture of [
  { label: "source", fresh: true },
  { label: "generated", fresh: false },
]) {
test(`Scene3D WebGPU ${fixture.label} retained meshes retire material uniforms with geometry`, async () => {
  const harness = await createBoardWebGPUHarness({ fresh: fixture.fresh });
  harness.env.context.__gosx_scene3d_webgpu_render_bundles = false;
  const api = harness.env.context.__gosx_scene3d_api;
  const object = retainedTriangle({}, vm.runInContext("Float32Array", harness.env.context));
  const viewport = { cssWidth: 640, cssHeight: 480, pixelWidth: 640, pixelHeight: 480, pixelRatio: 1 };

  harness.renderer.render(renderBundle(api, object, 0), viewport);
  const firstMaterialBuffer = latestPBRMaterialBuffer(harness.fake);
  assert.ok(firstMaterialBuffer, "retained draw must allocate a material uniform buffer");
  const firstWrites = harness.fake.state.writeBufferCalls.length;
  const firstRetainedStats = harness.renderer.diagnostics().retainedGeometry;
  assert.equal(firstRetainedStats.allocations, 4, "first frame must create every retained attribute buffer");
  assert.equal(firstRetainedStats.uploadCalls, 4);
  assert.equal(Object.keys(object.vertices).some((key) => key.includes("WGPURetained")), false, "GPU handles must not leak onto public vertices");
  harness.renderer.render(renderBundle(api, object, 1), viewport);
  const secondWrites = harness.fake.state.writeBufferCalls.slice(firstWrites);
  const secondRetainedStats = harness.renderer.diagnostics().retainedGeometry;
  assert.equal(secondRetainedStats.uploadCalls, firstRetainedStats.uploadCalls, "second frame must not upload retained vertex arrays");
  assert.ok(secondRetainedStats.hits >= firstRetainedStats.hits + 4);
  const materialWrite = secondWrites.find((call) => call.buffer === firstMaterialBuffer);
  assert.ok(materialWrite, "second frame must rewrite the identified retained material uniform buffer");
  assert.ok(materialWrite.data && materialWrite.data.byteLength === 208,
    "second frame must upload the current 208-byte material uniform to the identified buffer");
  assert.equal(materialWrite.data.byteLength / 4, 52, "material uniform payload must be 52 floats");
  assert.equal(harness.mount.getAttribute("data-gosx-scene3d-retained-mesh-objects"), "1");

  object.vertices.positions[0] = -2;
  object.vertices.revision = 1;
  harness.renderer.render(renderBundle(api, object, 2), viewport);
  const revisedMaterialBuffer = latestPBRMaterialBuffer(harness.fake);
  const dirtyRetainedStats = harness.renderer.diagnostics().retainedGeometry;
  assert.equal(firstMaterialBuffer.destroyed, true, "revision retirement must destroy the old material uniform buffer");
  assert.notEqual(revisedMaterialBuffer, firstMaterialBuffer, "revision retirement must allocate a fresh material owner");
  assert.notEqual(revisedMaterialBuffer.destroyed, true);
  assert.equal(dirtyRetainedStats.uploadCalls, secondRetainedStats.uploadCalls + 4, "explicit revision must refresh all retained GPU attributes");
  assert.equal(dirtyRetainedStats.revisionInvalidations, 1);

  const replacement = retainedTriangle({ id: "wgpu-replacement" }, vm.runInContext("Float32Array", harness.env.context));
  harness.renderer.render(renderBundle(api, replacement, 3), viewport);
  const replacementMaterialBuffer = latestPBRMaterialBuffer(harness.fake);
  const replacedStats = harness.renderer.diagnostics().retainedGeometry;
  assert.equal(revisedMaterialBuffer.destroyed, true, "epoch sweep must destroy a removed retained material buffer");
  assert.notEqual(replacementMaterialBuffer, revisedMaterialBuffer);
  assert.equal(replacedStats.cacheEntries, 1);
  assert.equal(replacedStats.liveBytes, dirtyRetainedStats.liveBytes);
  assert.ok(replacedStats.retirements >= dirtyRetainedStats.retirements + 4);

  harness.renderer.render(renderBundle(api, replacement, 4, [], false), viewport);
  const retiredStats = harness.renderer.diagnostics().retainedGeometry;
  assert.equal(replacementMaterialBuffer.destroyed, true, "retained-to-world-baked transition must retire its material buffer");
  assert.equal(retiredStats.cacheEntries, 0);
  assert.equal(retiredStats.liveBytes, 0);
});
}
