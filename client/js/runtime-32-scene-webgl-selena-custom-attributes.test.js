"use strict";
// Focused coverage for the reachable retained WebGL custom BufferAttribute
// vertical slice: an immutable, revisioned geometry carrying custom float
// streams must (a) survive scene-core normalization with exact item sizes,
// (b) reach the planner's retained snapshot path even under a Selena material,
// and (c) draw through the retained WebGL Selena path with the custom streams
// bound at descriptor locations — while non-Selena authored materials keep
// their historical world-baked fallback semantics.
//
// Planner-level assertions run against the fresh scene API (bootstrap-runtime +
// feature-scene3d bundle). Renderer-level assertions run the full bootstrap in
// the fake WebGL2 context and inspect issued GL calls plus the published
// retained-cache telemetry on the mount.

const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");

const {
  bootstrapSource,
  FakeWebGLContext,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
} = require("./runtime-test-harness.js");

function loadFreshSceneAPI() {
  const env = createContext({});
  runScript(bootstrapRuntimeSourceForTests(), env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSourceForTests(), env.context, "bootstrap-feature-scene3d.js");
  return { env, api: env.context.__gosx_scene3d_api };
}

let cachedRuntimeSource = null;
function bootstrapRuntimeSourceForTests() {
  if (cachedRuntimeSource === null) {
    const fs = require("node:fs");
    const path = require("node:path");
    cachedRuntimeSource = fs.readFileSync(
      path.join(__dirname, "bootstrap-runtime.js"),
      "utf8",
    );
  }
  return cachedRuntimeSource;
}

let cachedFeatureSource = null;
function freshFeatureBundleSourceForTests() {
  if (cachedFeatureSource === null) {
    // Match runtime-test-harness.freshFeatureBundleSource semantics without
    // importing its full harness surface here.
    cachedFeatureSource = require("./runtime-test-harness.js").freshFeatureBundleSource("scene3d");
  }
  return cachedFeatureSource;
}

const SELena = "selena";

function selenaMaterialVertexGLSL(customNames) {
  const declarations = customNames.map((entry) =>
    `attribute ${entry.type} ${entry.name};`,
  );
  const varyingDecl = "varying float vGlow;";
  return [
    "precision mediump float;",
    ...declarations,
    varyingDecl,
    "void main() {",
    "  vGlow = padGlow;",
    "  gl_Position = vec4(position, 1.0);",
    "}",
  ].join("\n");
}

const SELENA_CUSTOM_FRAGMENT_GLSL = [
  "precision mediump float;",
  "varying float vGlow;",
  "void main() {",
  "  gl_FragColor = vec4(vGlow, 0.5, 0.5, 1.0);",
  "}",
].join("\n");

function selenaCustomAttributeLayout(customAttributes) {
  return {
    schemaVersion: "selena.descriptor.v1",
    languageVersion: "selena.lang.v1",
    material: "CustomAttributes",
    kind: "mesh",
    entryPoints: { vertex: "vertexMain", fragment: "fragmentMain" },
    attributes: [
      { location: 0, name: "position", type: "vec3" },
      { location: 1, name: "normal", type: "vec3" },
      ...customAttributes.map((entry, index) => ({
        location: 3 + index,
        name: entry.name,
        type: entry.type,
      })),
    ],
    textures: [],
    uniformBlock: {
      size: 16,
      fields: [],
      defaults: [],
    },
    wgsl: { group: 0, binding: 0 },
    metal: { buffer: 0 },
  };
}

function selenaObjectWithCustomAttributes(options) {
  const opts = options || {};
  // Stream data must be allocated in the runtime's VM realm: the scene
  // runtime checks `data instanceof Float32Array` against its own context's
  // constructor, so a host-realm typed array fails that contract.
  if (!opts.env) {
    throw new Error("selenaObjectWithCustomAttributes requires opts.env (the runtime VM environment)");
  }
  const F32 = vm.runInContext("Float32Array", opts.env.context);
  const count = 4;
  const vertices = {
    count,
    positions: new F32(count * 3),
    normals: new F32(count * 3),
    uvs: new F32(count * 2),
    tangents: new F32(count * 4),
    indices: new Uint32Array([0, 1, 2, 0, 2, 3]),
    immutable: true,
    revision: 0,
    dynamic: false,
  };
  if (!opts.omitAttributes) {
    vertices.attributes = {
      // Scalar stream: must keep itemSize 1 end to end.
      padGlow: { data: new F32([0.1, 0.2, 0.3, 0.4]), itemSize: 1 },
      // vec3 stream: previously missing Go + JS width coverage.
      flowVec: {
        data: new F32([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]),
        itemSize: 3,
      },
    };
  }
  const object = {
    id: "selena-custom-quad",
    kind: "mesh",
    x: 0,
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
    spinZ: 0,
    driftPhase: 0,
    driftSpeed: 0,
    shiftX: 0,
    shiftY: 0,
    shiftZ: 0,
    materialKind: "custom",
    shaderBackend: SELena,
    customVertex: selenaMaterialVertexGLSL([
      { name: "padGlow", type: "float" },
      { name: "flowVec", type: "vec3" },
    ]),
    customFragment: SELENA_CUSTOM_FRAGMENT_GLSL,
    shaderLayout: selenaCustomAttributeLayout([
      { name: "padGlow", type: "float" },
      { name: "flowVec", type: "vec3" },
    ]),
    color: "#f8f8f8",
    opacity: 1,
    wireframe: false,
    castShadow: false,
    receiveShadow: false,
    vertices,
  };
  if (opts.objectOverrides) {
    Object.assign(object, opts.objectOverrides);
  }
  return object;
}

function renderBundle(api, objects, timeSeconds) {
  return api.createSceneRenderBundle(
    320,
    180,
    "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    objects,
    [],
    [],
    [],
    {},
    // environment intentionally omitted; next slot is timeSeconds per the
    // exact createSceneRenderBundle signature at
    // bootstrap-src/10-runtime-scene-core.ts:4628.
    undefined,
    timeSeconds || 0,
    [],
    [],
    [],
    [],
    [],
    0,
    false,
    { retainedGeometry: true },
  );
}

// The shared harness FakeWebGLContext resolves only a_-prefixed builtin names
// ("a_position", ...) and returns -1 for every other attribute name, while the
// Selena layout declares bare GLSL names. Override getAttribLocation on just
// this canvas's fake GL context with a local deterministic map for the built-in
// and custom attributes of this fixture, delegating all other names to the
// original harness method so no shared harness behavior is weakened.
function overrideCanvasAttribLocationMap(canvas) {
  const gl = canvas.getContext("webgl2");
  const localAttribLocations = { position: 0, normal: 1, padGlow: 3, flowVec: 4 };
  const originalGetAttribLocation = gl.getAttribLocation.bind(gl);
  gl.getAttribLocation = function (program, name) {
    if (Object.prototype.hasOwnProperty.call(localAttribLocations, name)) {
      return localAttribLocations[name];
    }
    return originalGetAttribLocation(program, name);
  };
  return gl;
}

test("Scene3D keeps immutable revisioned custom attribute streams with exact item sizes", () => {
  const { env, api } = loadFreshSceneAPI();
  const object = selenaObjectWithCustomAttributes({ env });
  const bundle = renderBundle(api, [object], 0);

  assert.equal(bundle.retainedMeshObjectCount, 1,
    "a Selena-material mesh with immutable revisioned custom streams must retain");
  const meshObject = bundle.meshObjects[0];
  assert.equal(meshObject.retainedGeometry, true);
  assert.equal(meshObject.geometryRevision, 0);
  const attributes = meshObject.vertices.attributes;
  assert.ok(attributes, "retained snapshot must carry the normalized custom streams");
  assert.equal(attributes.padGlow.itemSize, 1,
    "scalar float metadata must stay itemSize 1, never widen to 3");
  assert.equal(attributes.padGlow.data.length, 4);
  assert.equal(attributes.flowVec.itemSize, 3, "vec3 metadata must stay itemSize 3");
  assert.equal(attributes.flowVec.data.length, 12);
  assert.deepEqual(Array.from(attributes.flowVec.data), [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12]);
});

test("Scene3D revision rebuild republishes changed custom attribute streams", () => {
  const { env, api } = loadFreshSceneAPI();
  const F32 = vm.runInContext("Float32Array", env.context);
  const object = selenaObjectWithCustomAttributes({ env });

  const first = renderBundle(api, [object], 0);
  const firstFlow = Array.from(first.meshObjects[0].vertices.attributes.flowVec.data);

  // Bump the snapshot revision and change one value: the immutable contract
  // says a new revision may republish streams, which the geometry hash (and
  // therefore the renderer's revision-keyed cache) must observe.
  object.vertices = Object.assign({}, object.vertices, {
    revision: 1,
    attributes: {
      padGlow: { data: new F32([0.9, 0.2, 0.3, 0.4]), itemSize: 1 },
      flowVec: {
        data: new F32([1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 99]),
        itemSize: 3,
      },
    },
  });

  const second = renderBundle(api, [object], 0);
  assert.equal(second.meshObjects[0].geometryRevision, 1);
  const secondFlow = Array.from(second.meshObjects[0].vertices.attributes.flowVec.data);
  assert.notDeepEqual(secondFlow, firstFlow, "revision bump must be able to change stream values");
  assert.equal(secondFlow[11], 99);
});

test("Scene3D non-Selena authored shaders keep the world-baked fallback (no retention)", () => {
  const { env, api } = loadFreshSceneAPI();
  const object = selenaObjectWithCustomAttributes({
    env,
    objectOverrides: {
      id: "custom-glsl-quad",
      shaderBackend: "",
      customVertex: "void main() {}",
      customFragment: "void main() {}",
      shaderLayout: undefined,
    },
  });
  delete object.shaderLayout;
  const bundle = renderBundle(api, [object], 0);
  assert.equal(bundle.retainedMeshObjectCount, 0,
    "non-Selena authored shaders must keep the historical baked fallback");
  assert.equal(bundle.worldBakedMeshObjectCount, 1);
});

test("Scene3D WebGL retained Selena draw binds custom attributes by descriptor order with exact sizes", async () => {
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const registry = api.sceneBackendRegistry;
  const backend = registry.select({
    webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false,
  });
  const canvas = env.document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 180;
  // Attach the canvas so the renderer has a mount: retained-cache telemetry
  // publishes on canvas.parentNode after each frame.
  env.document.body.appendChild(canvas);
  overrideCanvasAttribLocationMap(canvas);
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });
  assert.equal(renderer && renderer.type, "webgl-pbr");

  const object = selenaObjectWithCustomAttributes({ env });
  const bundle = renderBundle(api, [object], 0);
  assert.equal(bundle.retainedMeshObjectCount, 1, "fixture must retain before the renderer runs");

  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  renderer.render(bundle, viewport);
  renderer.render(bundle, viewport);

  const gl = canvas.getContext("webgl2");
  const mount = canvas.parentNode;

  // Indexed Selena drawing stays correct: the quad has 6 authored indices.
  const elementDraws = gl.ops.filter((op) => op[0] === "drawElements");
  assert.equal(elementDraws.length, 2, "both frames must draw through the element buffer");
  for (const draw of elementDraws) {
    assert.equal(draw[3], gl.UNSIGNED_INT);
  }

  // The Selena program must have compiled and bound both custom streams at
  // their descriptor locations with EXACT component counts: scalar=1, vec3=3.
  const scalarPointers = gl.ops.filter((op) =>
    op[0] === "vertexAttribPointer" && op[2] === 1);
  const vec3Pointers = gl.ops.filter((op) =>
    op[0] === "vertexAttribPointer" && op[2] === 3);
  assert.ok(scalarPointers.length >= 2,
    "the scalar custom attribute must bind with size 1 each frame");
  assert.ok(vec3Pointers.length >= 2,
    "the vec3 custom attribute must bind with size 3 each frame");

  // Retained cache reuse: after the first frame uploads everything, the second
  // frame must hit the cache instead of re-uploading.
  const stats = mount.__gosxScene3DRetainedGeometryStats;
  assert.ok(stats, "retained buffer telemetry must publish on the mount");
  assert.ok(stats.hits > 0, `second frame must reuse cached buffers, got ${JSON.stringify(stats)}`);
  assert.ok(stats.uploadCalls >= 1, "first frame must upload the retained buffers once");

  renderer.dispose();
});

test("Scene3D WebGL safely skips the retained Selena draw when a declared custom stream is missing", async () => {
  const env = createContext({ enableWebGL2: true, disableCanvas2D: true });
  env.context.WebGL2RenderingContext = FakeWebGLContext;
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  const api = env.context.__gosx_scene3d_api;
  const backend = api.sceneBackendRegistry.select({
    webgl: true, webgl2: true, webgpu: false, canvas: false, canvas2d: false,
  });
  const canvas = env.document.createElement("canvas");
  canvas.width = 320;
  canvas.height = 180;
  overrideCanvasAttribLocationMap(canvas);
  const renderer = backend.create(canvas, { background: "#000000" }, { tier: "full" });

  const object = selenaObjectWithCustomAttributes({ omitAttributes: true, env });
  const bundle = renderBundle(api, [object], 0);
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  renderer.render(bundle, viewport);

  const gl = canvas.getContext("webgl2");
  const triangleDraws = gl.ops.filter((op) =>
    (op[0] === "drawElements" || op[0] === "drawArrays") && op[1] === gl.TRIANGLES);
  assert.equal(triangleDraws.length, 0,
    "a missing declared stream must skip the draw instead of drawing partial data");

  renderer.dispose();
});

test("Scene3D retained snapshots preserve JSON-parsed exotic own attribute keys", () => {
  const { env, api } = loadFreshSceneAPI();
  const object = selenaObjectWithCustomAttributes({ env });

  // JSON.parse is what makes "__proto__" an *own* enumerable key; a plain
  // object literal would rewrite the prototype instead of adding the key.
  const raw = JSON.parse(
    '{"__proto__":{"itemSize":1,"data":[1,2,3,4]},' +
      '"constructor":{"itemSize":1,"data":[5,6,7,8]},' +
      '"toString":{"itemSize":1,"data":[9,10,11,12]}}'
  );
  object.vertices = Object.assign({}, object.vertices, { attributes: raw });

  // renderBundle consumes already-normalized objects, so run the fixture
  // through the public normalizer first or sceneNormalizeCustomAttributes
  // never executes over the JSON-parsed exotic keys.
  const normalizedObject = api.normalizeSceneObject(object, 0, null);
  const bundle = renderBundle(api, [normalizedObject], 0);
  assert.equal(bundle.retainedMeshObjectCount, 1,
    "a Selena mesh with exotic-named immutable custom streams must retain");
  const meshObject = bundle.meshObjects[0];
  assert.equal(meshObject.retainedGeometry, true);
  assert.equal(meshObject.geometryRevision, 0);
  const attributes = meshObject.vertices.attributes;
  assert.ok(attributes, "retained snapshot must carry the normalized custom streams");

  const expected = [
    ["__proto__", [1, 2, 3, 4]],
    ["constructor", [5, 6, 7, 8]],
    ["toString", [9, 10, 11, 12]],
  ];
  const assertStreams = (snapshot, label) => {
    assert.deepEqual(Object.keys(snapshot), ["__proto__", "constructor", "toString"],
      `${label} must keep every JSON-parsed exotic name as an own enumerable key`);
    assert.ok(Object.prototype.hasOwnProperty.call(snapshot, "__proto__"),
      `${label} must hold "__proto__" as an own property, never as its prototype`);
    for (const [name, values] of expected) {
      const record = snapshot[name];
      assert.ok(record && typeof record === "object",
        `${label} must expose stream "${name}" to own-key lookup`);
      assert.equal(record.itemSize, 1, `${label} stream "${name}" must stay itemSize 1`);
      assert.equal(record.data.length, values.length);
      assert.deepEqual(Array.from(record.data), values,
        `${label} stream "${name}" must keep its exact four floats`);
    }
  };

  assertStreams(attributes, "first render snapshot");
  assert.notEqual(attributes, raw,
    "the snapshot must be an independent dictionary, not the caller's input");

  // Unchanged input: normalization must leave the JSON-parsed source alone.
  assert.deepEqual(Object.keys(raw), ["__proto__", "constructor", "toString"],
    "normalization must not mutate the caller's input dictionary");
  for (const [name, values] of expected) {
    assert.ok(Array.isArray(raw[name].data),
      `input stream "${name}" must stay the caller's plain JSON array`);
    assert.deepEqual(Array.from(raw[name].data), values,
      `input stream "${name}" must keep its original values`);
  }

  // Second render: feed the first normalized dictionary into a fresh fixture
  // so normalization runs again over an own-"__proto__" dictionary.
  const secondObject = selenaObjectWithCustomAttributes({ env });
  secondObject.vertices = Object.assign({}, secondObject.vertices, {
    revision: 1,
    attributes: attributes,
  });
  const secondNormalized = api.normalizeSceneObject(secondObject, 0, null);
  const second = renderBundle(api, [secondNormalized], 0);
  assert.equal(second.retainedMeshObjectCount, 1);
  assert.equal(second.meshObjects[0].retainedGeometry, true);
  assert.equal(second.meshObjects[0].geometryRevision, 1,
    "republishing the snapshot through a new fixture must honor the new revision");
  const secondAttributes = second.meshObjects[0].vertices.attributes;
  assert.notEqual(secondAttributes, attributes,
    "each render must snapshot independently instead of aliasing the prior one");
  assertStreams(secondAttributes, "second render snapshot");

  // Direct data-array non-alias assertions across the raw/first/second
  // snapshots: every stage must copy, never share, the underlying floats.
  for (const [name] of expected) {
    assert.notEqual(attributes[name].data, raw[name].data,
      `first snapshot stream "${name}" must copy, never alias, the caller's data array`);
    assert.notEqual(secondAttributes[name].data, attributes[name].data,
      `second snapshot stream "${name}" must copy, never alias, the first snapshot's data array`);
    assert.notEqual(secondAttributes[name].data, raw[name].data,
      `second snapshot stream "${name}" must not alias the caller's original data array`);
  }
});
