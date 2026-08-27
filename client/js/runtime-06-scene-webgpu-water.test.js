"use strict";
// WebGPU buffer packing, point rendering, post-effect passes, custom Selena
// materials and the WebGPU / WebGL2 water renderers.
//
// Split out of the former client/js/runtime.test.js, which this set replaces
// and which no longer exists. Every shared fake, sandbox builder and fixture
// factory lives in ./runtime-test-harness.js.

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapSource,
  bootstrapFeatureScene3DSource,
  FakeWebGLContext,
  FakeElement,
  createContext,
  runScript,
  flushAsyncWork,
  loadSceneWaterClockAPI,
  createAdaptiveQualityHarness,
  readSceneMountSrc,
  readWebGPUBackendSrc,
} = require("./runtime-test-harness.js");

test("bootstrap keeps WebGPU Scene3D points on per-entry cached GPU buffers", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(source, /function ensurePointsUniformGPUBuffer\(owner, uniformData\)/);
  assert.match(source, /function ensurePointsParticleGPUBuffer\(entry, particleData\)/);
  assert.match(source, /function ensurePointsParticleVertexGPUBuffer\(entry, particleData\)/);
  assert.match(source, /sceneCachedBuffer\(owner,\s*typedArray/);
  assert.match(source, /ensurePointsUniformGPUBuffer\(entry,\s*puF\)/);
  assert.match(source, /ensurePointsParticleVertexGPUBuffer\(entry,\s*particleData\)/);
  assert.match(source, /resource: \{ buffer: system\.renderBuffer \}/);
  assert.doesNotMatch(source, /var\s+pointsUniformBuffer\s*=\s*device\.createBuffer/);
  assert.doesNotMatch(source, /device\.queue\.writeBuffer\(pointsUniformBuffer,\s*0/);
});

test("bootstrap keeps WebGPU Scene3D PBR mesh attributes on packed scene GPU buffers", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const sceneBuffers = source.slice(source.indexOf("function ensurePBRSceneAttributeBuffers"), source.indexOf("function webGPUBindSceneMeshVertexBuffer"));
  const bindSceneBuffer = source.slice(source.indexOf("function webGPUBindSceneMeshVertexBuffer"), source.indexOf("// -----------------------------------------------------------------------\n    // Draw list construction"));
  const shadowPass = source.slice(source.indexOf("function renderShadowPass"), source.indexOf("// -----------------------------------------------------------------------\n    // PBR object drawing"));
  const drawPBR = source.slice(source.indexOf("function drawPBRObjects"), source.indexOf("function instancedMeshCount"));
  const skinnedBind = source.slice(source.indexOf("function webGPUBindElioSkinnedBuffers"), source.indexOf("// -----------------------------------------------------------------------\n    // Shadow Pass"));

  // wgpuStablePBRAttributeBuffer caches each attribute on a renderer-scoped,
  // content-compared slot (not wgpuCachedTrackedBuffer(bundle, "_gosxWGPU...")
  // -- `bundle` is a brand-new object every render() call, so an
  // owner-identity cache keyed on it can never hit; see
  // pbrSceneAttributeCache's declaration for the full rationale).
  assert.match(sceneBuffers, /wgpuStablePBRAttributeBuffer\("positions",\s*positions\)/);
  assert.match(sceneBuffers, /wgpuStablePBRAttributeBuffer\("normals",\s*normals\)/);
  assert.match(sceneBuffers, /wgpuStablePBRAttributeBuffer\("uvs",\s*uvs\)/);
  assert.match(sceneBuffers, /wgpuStablePBRAttributeBuffer\("tangents",\s*tangents\)/);
  assert.match(bindSceneBuffer, /byteOffset = offset \* components \* 4/);
  assert.match(bindSceneBuffer, /pass\.setVertexBuffer\(slot,\s*record\.buffer,\s*byteOffset,\s*byteSize\)/);

  assert.match(shadowPass, /webGPUBindSceneMeshVertexBuffer\(pass,\s*0,\s*pbrBuffers && pbrBuffers\.positions/);
  assert.doesNotMatch(shadowPass, /shadowPositionBuffer\s*=\s*wgpuEnsureBufferData/);
  assert.doesNotMatch(shadowPass, /sliceToFloat32/);

  assert.match(drawPBR, /webGPUBindSceneMeshVertexBuffer\(pass,\s*0,\s*pbrBuffers && pbrBuffers\.positions/);
  assert.match(drawPBR, /webGPUBindSceneMeshVertexBuffer\(pass,\s*1,\s*pbrBuffers && pbrBuffers\.normals/);
  assert.match(drawPBR, /webGPUBindSceneMeshVertexBuffer\(pass,\s*2,\s*pbrBuffers && pbrBuffers\.uvs/);
  assert.match(drawPBR, /webGPUBindSceneMeshVertexBuffer\(pass,\s*3,\s*pbrBuffers && pbrBuffers\.tangents/);
  assert.doesNotMatch(drawPBR, /sliceToFloat32/);
  assert.doesNotMatch(drawPBR, /positionBuffer\s*=\s*wgpuEnsureBufferData/);
  assert.doesNotMatch(drawPBR, /normalBuffer\s*=\s*wgpuEnsureBufferData/);
  assert.doesNotMatch(drawPBR, /uvBuffer\s*=\s*wgpuEnsureBufferData/);
  assert.doesNotMatch(drawPBR, /tangentBuffer\s*=\s*wgpuEnsureBufferData/);

  // One tracked output buffer backs slots 0/1/3: positions at offset 0,
  // normals at paddedCount * 12, tangents at paddedCount * 24, each bound
  // with its logical draw size; only slot 2 keeps a cached UV buffer.
  assert.match(skinnedBind, /pass\.setVertexBuffer\(0,\s*outputBuffer,\s*0,\s*vec3Bytes\)/);
  assert.match(skinnedBind, /pass\.setVertexBuffer\(1,\s*outputBuffer,\s*paddedCount \* 12,\s*vec3Bytes\)/);
  assert.match(skinnedBind, /wgpuCachedTrackedBuffer\(obj,\s*"_gosxWGPUSkinnedUVs"/);
  assert.match(skinnedBind, /pass\.setVertexBuffer\(3,\s*outputBuffer,\s*paddedCount \* 24,\s*Math\.max\(4,\s*count \* 4 \* 4\)\)/);
  assert.doesNotMatch(skinnedBind, /"_gosxWGPUSkinnedNormals"/);
  assert.doesNotMatch(skinnedBind, /"_gosxWGPUSkinnedTangents"/);
  assert.doesNotMatch(skinnedBind, /wgpuEnsureBufferData/);
});

test("bootstrap renders WebGPU Scene3D static points from instanced vertex buffers", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(source, /var WGSL_POINTS_INSTANCED_VERTEX = \[/);
  assert.match(source, /var WGPU_POINTS_INSTANCE_VERTEX_LAYOUT = \[/);
  assert.match(source, /stepMode: "instance"[\s\S]*shaderLocation: 0[\s\S]*shaderLocation: 2/);
  assert.match(source, /function wgpuCreatePointsUniformBindGroupLayout\(device\)/);
  assert.match(source, /function wgpuCreatePointsVertexPipeline/);
  assert.match(source, /function getPointsVertexPipeline\(blendMode, depthWrite\)/);
  assert.match(source, /GPUBufferUsage\.VERTEX \| GPUBufferUsage\.COPY_DST/);
  assert.match(source, /pass\.setVertexBuffer\(0,\s*pointsParticleBuffer\)/);
  assert.match(source, /pass\.draw\(6,\s*count\)/);
});

test("bootstrap renders WebGPU Scene3D glow points with radial alpha", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(source, /points\.flags\.w == 2u/);
  assert.match(source, /let radial = length\(centered\) \* 2\.0/);
  assert.match(source, /"        if \(radial > 1\.0\) \{",/);
  assert.match(source, /"            discard;",/);
  assert.match(source, /let edgeFeather = 1\.0 - smoothstep\(0\.78, 1\.0, radial\)/);
  assert.match(source, /alpha = core \* edgeFeather \* in\.alpha/);
  assert.match(source, /if \(alpha <= 0\.003\) \{/);
});

test("bootstrap keeps WebGPU Scene3D point uniforms vec4-aligned", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(source, /defaultColorAndSize: vec4f/);
  assert.match(source, /flags: vec4u/);
  assert.match(source, /params: vec4f/);
  assert.match(source, /fogColor: vec4f/);
  assert.match(source, /var pointsUniformScratch = new ArrayBuffer\(128\)/);
  assert.match(source, /puF\[16\] = defaultColorRGBA\[0\]/);
  assert.match(source, /puF\[19\] = sceneNumber\(entry\.size, 1\)/);
  assert.match(source, /puU\[23\] = scenePointStyleCode\(entry\.style\)/);
  assert.match(source, /puF\[27\] = sceneNumber\(entry\.maxPixelSize, 0\)/);
  assert.match(source, /puF\[28\] = fogColorRGBA\[0\]/);
  assert.match(source, /points\.params\.w > 0\.0/);
  assert.match(source, /pixelSize = min\(pixelSize, points\.params\.w\)/);
  assert.doesNotMatch(source, /defaultSize: f32/);
  assert.doesNotMatch(source, /defaultColor: vec3f/);
  assert.doesNotMatch(source, /hasPerVertexColor: u32/);
});

test("bootstrap keeps WebGL and WebGPU Scene3D point size clamps in parity", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgl, /uniform float u_minPixelSize;/);
  assert.match(webgl, /uniform float u_maxPixelSize;/);
  assert.match(webgl, /float minPixelSize = max\(u_minPixelSize, 0\.0\);/);
  assert.match(webgl, /pixelSize = max\(pixelSize, minPixelSize\);/);
  assert.match(webgl, /pixelSize = min\(pixelSize, u_maxPixelSize\);/);
  assert.match(webgl, /gl\.uniform1f\(pp\.uniforms\.minPixelSize,\s*Math\.max\(0,\s*sceneNumber\(entry\.minPixelSize,\s*0\)\)\)/);
  assert.match(webgl, /gl\.uniform1f\(pp\.uniforms\.maxPixelSize,\s*Math\.max\(0,\s*sceneNumber\(entry\.maxPixelSize,\s*0\)\)\)/);
  assert.match(webgpu, /let minPixelSize = max\(points\.fogColor\.a, 0\.0\);/);
  assert.match(webgpu, /pixelSize = max\(pixelSize, minPixelSize\);/);
  assert.match(webgpu, /pixelSize = min\(pixelSize, points\.params\.w\);/);
});

test("bootstrap uploads Scene3D point pixel clamps to WebGL", async () => {
  const mount = new FakeElement("div", null);
  mount.id = "scene-point-clamp-webgl";
  const env = createContext({
    elements: [mount],
    enableWebGL2: true,
    disableCanvas2D: true,
    manifest: {
      engines: [
        {
          id: "gosx-engine-point-clamp-webgl",
          component: "GoSXScene3D",
          kind: "surface",
          mountId: "scene-point-clamp-webgl",
          props: {
            width: 320,
            height: 180,
            forceWebGL: true,
            autoRotate: false,
            camera: { z: 6, fov: 72, near: 0.05, far: 128 },
            scene: {
              points: [
                {
                  id: "stars",
                  count: 2,
                  positions: [0, 0, 0, 1, 0, 0],
                  size: 4,
                  minPixelSize: 3,
                  maxPixelSize: 12,
                  attenuation: true,
                },
              ],
            },
          },
        },
      ],
    },
  });
  env.context.WebGL2RenderingContext = FakeWebGLContext;

  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();

  assert.equal(mount.getAttribute("data-gosx-scene3d-renderer"), "webgl");
  const gl = mount.children[0].getContext("webgl2");
  assert.ok(gl.ops.some((entry) => entry[0] === "getUniformLocation" && entry[1] === "u_minPixelSize"));
  assert.ok(gl.ops.some((entry) => entry[0] === "getUniformLocation" && entry[1] === "u_maxPixelSize"));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1f" && entry[1] === "u_minPixelSize" && entry[2] === 3));
  assert.ok(gl.ops.some((entry) => entry[0] === "uniform1f" && entry[1] === "u_maxPixelSize" && entry[2] === 12));
  assert.ok(gl.ops.some((entry) => entry[0] === "drawArrays" && entry[1] === gl.POINTS && entry[3] === 2));
});

test("bootstrap preserves Scene3D point maxPixelSize from GLB extras", () => {
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");
  const gltf = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "gltf.ts"), "utf8");

  assert.match(core, /maxPixelSize: sceneClampNumberOrCSSVar\(item\.maxPixelSize/);
  assert.match(gltf, /"maxPixelSize"/);
});

test("bootstrap exposes WebGPU Scene3D planned draw stats on the mount", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(source, /function publishWebGPUFrameStats\(stats\)/);
  assert.match(source, /var webGPUFrameSeq = 0/);
  assert.match(source, /data-gosx-scene3d-webgpu-frame-seq/);
  assert.match(source, /data-gosx-scene3d-webgpu-frame-at/);
  assert.match(source, /__gosxScene3DWebGPUStats = published/);
  assert.match(source, /__gosxScene3DWebGPUProof = \{/);
  assert.match(source, /WEBGPU_DIAGNOSTIC_ATTRIBUTE_INTERVAL_MS = 250/);
  assert.match(source, /var mirrorDiagnostics = verboseTelemetry \|\| diagnosticElapsed < 0 \|\| diagnosticElapsed >= WEBGPU_DIAGNOSTIC_ATTRIBUTE_INTERVAL_MS/);
  assert.match(source, /if \(!mirrorDiagnostics\) return/);
  assert.match(source, /data-gosx-scene3d-webgpu-point-entries/);
  assert.match(source, /data-gosx-scene3d-webgpu-point-instances/);
  assert.match(source, /data-gosx-scene3d-webgpu-point-draw-instances/);
  assert.match(source, /pointAuthoredDrawEntries/);
  assert.match(source, /data-gosx-scene3d-webgpu-point-authored-draw-entries/);
  assert.match(source, /data-gosx-scene3d-webgpu-point-authored-draw-instances/);
  assert.match(source, /data-gosx-scene3d-webgpu-point-authored-draw-calls/);
  assert.match(source, /computeParticleAuthoredDrawEntries/);
  assert.match(source, /data-gosx-scene3d-webgpu-compute-particle-authored-draw-entries/);
  assert.match(source, /data-gosx-scene3d-webgpu-compute-particle-authored-draw-instances/);
  assert.match(source, /data-gosx-scene3d-webgpu-compute-particle-authored-draw-calls/);
  assert.match(source, /data-gosx-scene3d-webgpu-mesh-objects/);
  // mesh-objects publishes bundle.meshObjects.length UNCONDITIONALLY (it
  // includes CPU-frustum-culled objects) -- mesh-draw-calls/mesh-view-culled
  // split SUBMITTED from CULLED, mirroring the point/compute-particle
  // draw-call counters above, so a culled-to-zero mesh no longer reads as
  // "drawing" on the mesh-objects count alone.
  assert.match(source, /function webGPUCountViewCulledMeshObjects\(bundle\)/);
  assert.match(source, /data-gosx-scene3d-webgpu-mesh-draw-calls/);
  assert.match(source, /data-gosx-scene3d-webgpu-mesh-view-culled/);
  assert.match(source, /data-gosx-scene3d-webgpu-instanced-instances/);
});

test("Scene3D WebGPU ignores popErrorScope lifecycle drops", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(source, /function isWebGPUErrorScopeLifecycleMessage\(message\)/);
  assert.match(source, /function wgpuPopScopedErrorScope\(scopedDevice\)/);
  assert.match(source, /rendererDeviceStillActive\(scopedDevice\)/);
  assert.match(source, /indexOf\("instance dropped"\) >= 0/);
  assert.match(source, /indexOf\("poperrorscope"\) >= 0/);
  assert.match(source, /\.catch\(function\(error\) \{[\s\S]*if \(isWebGPUErrorScopeLifecycleMessage\(message\)\) return;[\s\S]*reportWebGPUFrameError\(message\);[\s\S]*\}\);/);
});

test("Scene3D WebGPU skinning is driven by Elio compute output buffers", () => {
  const source = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(source, /SCENE_ELIO_SKIN_LBS_SOURCE/);
  assert.match(source, /Emitted by m31labs\.dev\/elio\/emit\/wgsl from stdlib\.Skin\(\)/);
  assert.match(source, /@compute @workgroup_size\(64\)/);
  assert.match(source, /device\.createComputePipeline\(\{[\s\S]*label: "gosx-elio-skin-lbs"/);
  assert.match(source, /updateElioSkinnedMeshes\(bundle, encoder\)/);
  assert.match(source, /pass\.dispatchWorkgroups\(record\.workgroups\)/);
  assert.match(source, /GPUBufferUsage\.STORAGE \| GPUBufferUsage\.VERTEX \| GPUBufferUsage\.COPY_DST/);
  assert.match(source, /webGPUBindElioSkinnedBuffers\(pass, obj, count\)/);
  assert.match(source, /pass\.setVertexBuffer\(0,\s*outputBuffer,\s*0,\s*vec3Bytes\)/);
  assert.match(source, /data-gosx-scene3d-webgpu-elio-skinning-dispatches/);
  assert.match(source, /data-gosx-scene3d-webgpu-elio-skinning-kernel/);
});

test("Scene3D WebGPU water supports compound sphere object displacement", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");

  // The hand-written data-prop-authored compute pipeline tier
  // (sceneWaterAuthoredComputePipeline and its sceneWaterAuthoredComputeField/
  // EntryPoint/Source/Backend helpers, plus waterAuthoredComputePipelineCache/
  // Failures) has been retired: every compute kernel now resolves
  // Selena-primary (getSelenaComputePipeline/createSelenaComputeBindGroup)
  // falling through directly to the builtin SCENE_WATER_COMPUTE_SOURCE
  // pipeline literals below, with no more per-entry "authored WGSL" pipeline
  // builder in between.
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredComputePipeline/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredComputeField/);
  assert.doesNotMatch(webgpu, /waterAuthoredComputePipelineCache = new Map/);
  assert.match(webgpu, /var seedCompute = \{ pipeline: waterSeedPipeline, authored: false, failed: false \};/);
  assert.match(webgpu, /var dropCompute = \{ pipeline: waterDropPipeline, authored: false, failed: false \};/);
  assert.match(webgpu, /var displacementCompute = \{ pipeline: waterDisplacementPipeline, authored: false, failed: false \};/);
  assert.match(webgpu, /var simulationCompute = \{ pipeline: waterStepPipeline, authored: false, failed: false \};/);
  assert.match(webgpu, /var normalCompute = \{ pipeline: waterNormalPipeline, authored: false, failed: false \};/);
  assert.match(webgpu, /waterAuthoredComputeDispatches/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-compute-systems/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-compute-dispatches/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-compute-fallbacks/);
  assert.match(webgpu, /struct WaterDisplacementSphere/);
  assert.match(webgpu, /var<storage, read> objectSpheres/);
  assert.match(webgpu, /WATER_MAX_DISPLACEMENT_SPHERES = 32/);
  assert.match(webgpu, /sceneWaterDisplacementSpheres/);
  assert.match(webgpu, /kind < 2\.5/);
  assert.match(webgpu, /sphereCount = min\(u32\(params\.objectParams\.z\), 32u\)/);
  assert.match(webgpu, /previous \+ offset/);
  assert.match(webgpu, /current \+ offset/);
  assert.match(webgpu, /waterObjectSpheres/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-spheres/);
  assert.match(webgpu, /function sceneWaterObjectExplicitPreviousSignature/);
  assert.match(webgpu, /entry\.objectPreviousSet/);
  assert.match(webgpu, /function sceneWaterObjectDisplacementEvents/);
  assert.match(webgpu, /function dispatchWaterObjectDisplacementEvents/);
  assert.match(webgpu, /transientObject \? null : system/);
  assert.match(webgpu, /waterObjectEventDispatches/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-event-dispatches/);
  assert.match(webgpu, /system\.waterObjectExplicitPreviousSignature !== explicitPreviousSignature/);
  assert.match(webgpu, /interactiveDrop: vec4f/);
  assert.match(webgpu, /fn addDrop/);
  assert.match(webgpu, /waterDropPipeline/);
  assert.match(webgpu, /entryPoint: "addDrop"/);
  assert.match(webgpu, /let polarity = select\(1\.0, -1\.0, \(j & 1u\) == 0u\);/, "seed drops start negative like upstream");
  assert.match(webgpu, /seedSalt: f32/, "water uniforms should carry a per-system seed salt");
  assert.match(webgpu, /seedSalt: Number\.isFinite\(Number\(entry && entry\.seedSalt\)\) \? Number\(entry\.seedSalt\) : Math\.random\(\) \* 4096/, "water systems should randomize initial seed centers per mount like upstream Math.random");
  assert.match(webgpu, /waterUniformScratchF\[52\] = Math\.max\(0, sceneNumber\(system && system\.seedSalt, 0\)\);/, "seed salt should be uploaded in spare water uniform space");
  assert.match(webgpu, /let seedSalt = params\.seedSalt;/);
  assert.match(webgpu, /hash01\(jf \* 12\.9898 \+ seedSalt \+ 0\.173\)/, "seed centers should use the randomized seed salt");
  assert.match(webgpu, /return \(vec2f\(f32\(x\), f32\(y\)\) \+ vec2f\(0\.5\)\) \/ max\(vec2f\(f32\(res\)\), vec2f\(1\.0\)\);/, "waterCoord should use render-target texel centers like upstream");
  assert.match(webgpu, /let uv = \(vec2f\(f32\(x\), f32\(y\)\) \+ vec2f\(0\.5\)\) \/ max\(vec2f\(f32\(res\)\), vec2f\(1\.0\)\);/, "seed pass should use render-target texel centers like upstream");
  assert.doesNotMatch(webgpu, /vec2f\(f32\(x\), f32\(y\)\) \/ max\(vec2f\(f32\(res - 1u\)\), vec2f\(1\.0\)\)/, "water compute passes should not use edge-based UVs");
  // seed/drop dispatch through dispatchWaterComputeStage (tries the generic
  // descriptor-driven Selena feedback-compute path first, see
  // getSelenaComputePipeline/createSelenaComputeBindGroup above), which falls
  // back to dispatchWaterPass(encoder, system, fallbackPipeline) -- the SAME
  // seedCompute/dropCompute resolved pipeline this test already exercises --
  // when Selena's WGSL+descriptor aren't present on the entry.
  assert.match(webgpu, /dispatchWaterComputeStage\(encoder, system, entry, "seed", seedCompute\.pipeline\)/);
  assert.match(webgpu, /dispatchWaterComputeStage\(encoder, system, entry, "drop", dropCompute\.pipeline\)/);
  assert.match(webgpu, /return \{ dispatches: dispatchWaterPass\(encoder, system, fallbackPipeline, sharedPass\), selena: 0, selenaFallback: 0 \};/);
  assert.match(webgpu, /if \(dropDispatches > 0\) \{\s*system\.lastDropEventID = dropEventID;/s);
  assert.match(webgpu, /system\.dropDispatchCount = Math\.max/);
  assert.match(webgpu, /entry\.dropEventID/);
  assert.match(webgpu, /system\.lastDropEventID !== dropEventID/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-drop-dispatches/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-drop-dispatch-total/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-drop-event/);
  const math = fs.readFileSync(path.join(__dirname, "bootstrap-src", "11-scene-math.ts"), "utf8");
  assert.match(math, /function sceneRayIntersectYPlane/);
  assert.match(math, /function sceneRayIntersectPlane/);
  assert.match(math, /function sceneRayIntersectSphere/);
  assert.match(math, /function sceneRayIntersectAABB/);
  assert.match(math, /sceneScreenToRay/);
  assert.match(math, /sceneRayIntersectYPlane/);
  assert.match(math, /sceneRayIntersectPlane/);
  assert.match(core, /objectDisplacementSpheres: normalizeSceneWaterDisplacementSpheres/);
  assert.match(core, /objectPreviousSet: sceneBool/);
  assert.match(core, /objectPreviousX: sceneNumber/);
  assert.match(core, /dropEventID: Math\.max/);
  assert.match(core, /dropEventRadius: Math\.max/);
  assert.match(core, /interactionProfile: typeof item\.interactionProfile === "string"/);
  assert.match(core, /interactionTarget: typeof item\.interactionTarget === "string"/);
  assert.match(core, /interactionObject: typeof item\.interactionObject === "string"/);
  assert.match(core, /computeSource: typeof item\.computeSource === "string"/);
  assert.match(core, /materialSource: typeof item\.materialSource === "string"/);
  assert.match(core, /SCENE_WATER_SOURCE_FILE_MAP_FIELDS = \["computeSourceFiles", "materialSourceFiles"\]/);
  assert.match(core, /computeSourceFiles: sceneIsPlainObject\(item\.computeSourceFiles\)/);
  assert.match(core, /materialSourceFiles: sceneIsPlainObject\(item\.materialSourceFiles\)/);
  assert.match(core, /const currentWaterByID = new Map\(\)/);
  assert.match(core, /const SCENE_WATER_SHADER_STRING_FIELDS = \[/);
  assert.match(core, /function sceneWaterShaderSourceMap/);
  assert.match(core, /state\._waterShaderSourceByID = sceneWaterShaderSourceMap\(state\.waterSystems\)/);
  assert.match(core, /const waterShaderSourceByID = state\._waterShaderSourceByID instanceof Map \? state\._waterShaderSourceByID : new Map\(\)/);
  assert.match(core, /const sourceFallback = waterShaderSourceByID\.get\(id\) \|\| null/);
  assert.match(core, /Object\.assign\(\{\}, currentFallback \|\| \{\}, sourceFallback\)/);
  assert.match(core, /const waterShaderString = function\(name\)/);
  assert.match(core, /typeof item\[name\] === "string" && item\[name\]\.trim\(\)/);
  // The hand-written Elio/Selena *WGSL/*WGSLRef water fields (shaderLib-dedup
  // refs included) have been retired from normalizeSceneWaterSystemEntry's
  // whitelist -- Selena is the sole primary WGSL source now, so only the
  // *SelenaWGSL slots remain wired through waterShaderString.
  assert.doesNotMatch(core, /objectShadowWGSL: waterShaderString\("objectShadowWGSL"\)/);
  assert.doesNotMatch(core, /objectMeshShadowVertexWGSL: waterShaderString\("objectMeshShadowVertexWGSL"\)/);
  assert.doesNotMatch(core, /objectMeshShadowFragmentWGSL: waterShaderString\("objectMeshShadowFragmentWGSL"\)/);
  assert.doesNotMatch(core, /seedWGSLRef: waterShaderString\("seedWGSLRef"\)/);
  assert.doesNotMatch(core, /dropWGSLRef: waterShaderString\("dropWGSLRef"\)/);
  assert.doesNotMatch(core, /causticsWGSLRef: waterShaderString\("causticsWGSLRef"\)/);
  assert.doesNotMatch(core, /surfaceBelowFragmentWGSLRef: waterShaderString\("surfaceBelowFragmentWGSLRef"\)/);
  assert.doesNotMatch(core, /objectMeshShadowFragmentWGSLRef: waterShaderString\("objectMeshShadowFragmentWGSLRef"\)/);
  assert.match(core, /objectShadowSelenaWGSL: waterShaderString\("objectShadowSelenaWGSL"\)/);
  assert.match(core, /objectMeshShadowSelenaWGSL: waterShaderString\("objectMeshShadowSelenaWGSL"\)/);
});

test("Scene3D WebGPU water clips rounded box surfaces with a shader SDF", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /cornerRadius: f32/);
  assert.match(webgpu, /poolShape: f32/);
  assert.match(webgpu, /function sceneWaterPoolShapeRounded/);
  assert.match(webgpu, /sceneWaterPoolShapeRounded\(entry\)/);
  assert.match(webgpu, /waterUniformScratchF\[14\] = cornerRadius/);
  assert.match(webgpu, /waterUniformScratchF\[15\] = rounded \? 1 : 0/);
  assert.match(webgpu, /fn roundedPoolSDF\(point: vec2f, halfSize: vec2f, radius: f32\)/);
  assert.match(webgpu, /fn roundedWaterSDF\(point: vec2f, halfSize: vec2f, radius: f32\)/);
  assert.match(webgpu, /roundedWaterSDF\(in\.worldPos\.xz, halfSize, params\.cornerRadius\)/);
  assert.match(webgpu, /if \(shapeAlpha <= 0\.001\) \{ discard; \}/);
  assert.match(webgpu, /return vec4f\(mix\(refractedColor, reflectedColor, fresnel\), shapeAlpha\)/);
  assert.match(webgpu, /return vec4f\(mix\(reflectedColor, refractedColor, \(1\.0 - fresnel\) \* length\(refractDir\)\), shapeAlpha\)/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-rounded-systems/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-corner-radius/);
});

test("Scene3D WebGPU water renders upstream-style above and below surface passes", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /SCENE_WATER_RENDER_FRAGMENT_SOURCE/);
  assert.match(webgpu, /SCENE_WATER_RENDER_BELOW_FRAGMENT_SOURCE/);
  assert.match(webgpu, /const WATER_SURFACE_VIEW_BELOW: bool = false/);
  assert.match(webgpu, /const WATER_SURFACE_VIEW_BELOW: bool = true/);
  assert.match(webgpu, /if \(WATER_SURFACE_VIEW_BELOW\) \{ n = -n; \}/);
  assert.match(webgpu, /let fresnelBase = select\(0\.25, 0\.50, WATER_SURFACE_VIEW_BELOW\)/);
  assert.match(webgpu, /let refractEta = select\(1\.0 \/ 1\.333, 1\.333 \/ 1\.0, WATER_SURFACE_VIEW_BELOW\)/);
  assert.match(webgpu, /let refractDir = refract\(-viewDir, n, refractEta\)/);
  assert.match(webgpu, /var reflectedColor = sampleWaterSky\(reflectDir\)/);
  assert.match(webgpu, /var refractedColor = mix\(params\.deepColor\.rgb, params\.shallowColor\.rgb, depthMix\)/);
  assert.match(webgpu, /if \(params\.objectParams\.x >= 2\.5 && params\.opticsFlags\.w > 0\.0\)/);
  assert.match(webgpu, /reflectedColor = mix\(reflectedColor, clippedReflectionTexel\.rgb, clippedReflectionTexel\.a \* reflectionEnabled\)/);
  assert.match(webgpu, /return vec4f\(mix\(reflectedColor, refractedColor, \(1\.0 - fresnel\) \* length\(refractDir\)\), shapeAlpha\)/);
  assert.match(webgpu, /return vec4f\(mix\(refractedColor, reflectedColor, fresnel\), shapeAlpha\)/);
  assert.doesNotMatch(webgpu, /objectOpticalFootprint/);
  assert.match(webgpu, /waterRenderBelowFragmentModule = device\.createShaderModule/);
  assert.match(webgpu, /label: "gosx-water-render-below-frag"/);
  assert.match(webgpu, /function sceneWaterObjectSubtype\(entry, kind\)/);
  assert.match(webgpu, /raw\.indexOf\("torus"\) >= 0 \|\| raw\.indexOf\("knot"\) >= 0/);
  assert.match(webgpu, /raw\.indexOf\("duck"\) >= 0 \|\| raw\.indexOf\("mesh"\) >= 0/);
  assert.match(webgpu, /surfaceBelowFragmentWGSL/);
  assert.match(webgpu, /function sceneWaterSurfaceSourceBytes\(record\)/);
  // The hand-written data-prop-authored surface pipeline tier
  // (sceneWaterAuthoredMaterialBackend, sceneWaterAuthoredSurfaceVertexSource/
  // FragmentSource, sceneWaterResolvedSurfaceSourceBytes,
  // sceneWaterAuthoredShaderSource) has been retired: each surface side
  // resolves Selena-primary falling through to a builtin-only
  // getWaterRenderPipeline with no more per-entry "authored WGSL" pipeline
  // builder in between.
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredMaterialBackend/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredSurfaceFragmentSource/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredSurfaceVertexSource/);
  assert.doesNotMatch(webgpu, /function sceneWaterResolvedSurfaceSourceBytes/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "causticsWGSL"\)/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "surfaceVertexWGSL"\)/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "surfaceFragmentWGSL"\)/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "surfaceBelowFragmentWGSL"\)/);
  assert.match(webgpu, /binding: 1, visibility: GPUShaderStage\.VERTEX \| GPUShaderStage\.FRAGMENT, buffer: \{ type: "read-only-storage" \}/);
  assert.match(webgpu, /binding: 8, visibility: GPUShaderStage\.FRAGMENT, buffer: \{ type: "uniform" \}/);
  assert.match(webgpu, /binding: 9, visibility: GPUShaderStage\.FRAGMENT, texture: \{ sampleType: "float", viewDimension: "2d" \}/);
  assert.match(webgpu, /binding: 10, visibility: GPUShaderStage\.FRAGMENT, buffer: \{ type: "read-only-storage" \}/);
  assert.match(webgpu, /system\.waterSurfaceTileRequested = !!tileURL/);
  assert.match(webgpu, /system\.waterSurfaceTileLoaded = tileLoaded/);
  assert.match(webgpu, /system\.waterSurfaceTilePending = tilePending/);
  assert.match(webgpu, /system\.waterSurfaceTileFailed = tileFailed/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-pool-tile-texture-pending/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-pool-tile-texture-failed/);
  assert.match(webgpu, /\{ binding: 9, resource: tileLoaded \? tileRecord\.view : placeholderView \}/);
  assert.match(webgpu, /\{ binding: 10, resource: \{ buffer: system\.objectSphereBuffer \} \}/);
  assert.match(webgpu, /objectTextureMatrixBuffer/);
  assert.match(webgpu, /function writeWaterObjectTextureMatrices/);
  assert.match(webgpu, /objectViewProjectionMatrix/);
  assert.match(webgpu, /objectReflectionViewProjectionMatrix/);
  assert.match(webgpu, /system\.objectViewProjectionMatrix\.set\(scratchSelenaViewProjection\)/);
  assert.match(webgpu, /system\.objectReflectionViewProjectionMatrix\.set\(scratchSelenaViewProjection\)/);
  assert.match(webgpu, /var viewMatrix = system\.objectViewProjectionReady \? system\.objectViewProjectionMatrix : null/);
  assert.doesNotMatch(webgpu, /system\.objectViewProjectionReady = false;[\s\S]*system\.objectReflectionViewProjectionReady = false/,
    "retained duck targets must keep the projection matrices they were rendered with across cadence skips");
  assert.match(webgpu, /if \(passSlot === 0\) \{[\s\S]*system\.objectViewProjectionMatrix\.set\(scratchSelenaViewProjection\)[\s\S]*\} else \{[\s\S]*system\.objectReflectionViewProjectionMatrix\.set\(scratchSelenaViewProjection\)/,
    "each alternating target must update only its matching retained projection matrix");
  assert.match(webgpu, /function getWaterRenderPipeline\(system, surfaceSide, forceBuiltin\)/);
  assert.doesNotMatch(webgpu, /var entry = forceBuiltin \? \{\} : \(system && system\.entry \|\| \{\}\)/);
  assert.doesNotMatch(webgpu, /var vertexSource = forceBuiltin \? "" : sceneWaterAuthoredSurfaceVertexSource\(entry\)/);
  assert.match(webgpu, /pipelineRecord = getWaterRenderPipeline\(null, side, true\)/);
  assert.match(webgpu, /function getWaterPoolPipeline\(system, forceBuiltin\)/);
  assert.doesNotMatch(webgpu, /getWaterPoolPipeline\(null, true\)/);
  assert.doesNotMatch(webgpu, /label: authored[\s\S]*"gosx-water-" \+ backend \+ "-surface-" \+ side/);
  assert.doesNotMatch(webgpu, /authoredVertex: !!vertexSource/);
  assert.match(webgpu, /authoredVertex: false/);
  assert.doesNotMatch(webgpu, /waterAuthoredSurfacePipelineFailures/);
  assert.match(webgpu, /waterAuthoredSurfacePipelineLastError = ""/);
  assert.doesNotMatch(webgpu, /var validationDevice = authored \? device : null/);
  assert.doesNotMatch(webgpu, /validationDevice\.pushErrorScope\("validation"\)/);
  assert.match(webgpu, /var pipeline = device\.createRenderPipeline\(descriptor\)/);
  assert.match(webgpu, /pending: false/);
  assert.doesNotMatch(webgpu, /wgpuPopScopedErrorScope\(validationDevice\)\.then/);
  assert.doesNotMatch(webgpu, /waterAuthoredSurfacePipelineLastError = String\(error && error\.message \|\| error \|\| "validation failed"\)/);
  assert.match(webgpu, /cullMode: side === "below" \? "back" : "front"/);
  assert.match(webgpu, /label: side === "below" \? "gosx-water-render-below"[\s\S]*?depthWriteEnabled: true/);
  assert.match(webgpu, /getWaterSelenaMeshDraw\(material, renderContext, system, \{ blendMode: "alpha", depthWrite: true, cullMode: "front" \}\)/);
  assert.match(webgpu, /function drawWaterSurfaceSide/);
  assert.match(webgpu, /drawWaterSurfaceSide\(renderPass, records, frameBindGroup, "above", stats, camera\)/);
  assert.match(webgpu, /drawWaterSurfaceSide\(renderPass, records, frameBindGroup, "below", stats, camera\)/);
  assert.match(webgpu, /waterSurfaceAboveDrawCalls/);
  assert.match(webgpu, /waterSurfaceBelowDrawCalls/);
  assert.match(webgpu, /waterAuthoredSurfaceDrawCalls/);
  assert.match(webgpu, /waterAuthoredSurfaceVertexDrawCalls/);
  assert.match(webgpu, /waterAuthoredSurfacePendingDrawCalls/);
  assert.match(webgpu, /stats\.waterAuthoredSurfacePendingDrawCalls \+= 1/);
  assert.match(webgpu, /waterAuthoredSurfaceSourceBytes/);
  assert.match(webgpu, /waterEntrySurfaceSourceBytes/);
  assert.match(webgpu, /waterResolvedSurfaceSourceBytes/);
  assert.match(webgpu, /waterManifestSurfaceSourceBytes/);
  assert.match(webgpu, /waterBundleSurfaceSourceBytes/);
  assert.match(webgpu, /waterAuthoredSurfaceFallbacks/);
  assert.match(webgpu, /waterAuthoredSurfaceFallbackReason/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-surface-above-draw-calls/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-surface-below-draw-calls/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-surface-draw-calls/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-surface-vertex-draw-calls/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-surface-pending-draw-calls/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-surface-source-bytes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-entry-surface-source-bytes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-resolved-surface-source-bytes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-manifest-surface-source-bytes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-bundle-surface-source-bytes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-surface-fallbacks/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-surface-fallback-reason/);
});

test("Scene3D WebGPU water renders an upstream-style pool pass with caustics and tile texture", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /SCENE_WATER_POOL_VERTEX_SOURCE/);
  assert.match(webgpu, /SCENE_WATER_POOL_FRAGMENT_SOURCE/);
  assert.match(webgpu, /label: "gosx-water-pool"/);
  assert.match(webgpu, /waterPoolPipelineLayout = device\.createPipelineLayout/);
  assert.match(webgpu, /waterPoolVertexModule = device\.createShaderModule/);
  assert.match(webgpu, /function getWaterPoolPipeline\(system, forceBuiltin\)/);
  assert.match(webgpu, /function createWaterPoolBindGroup\(system, buffer\)/);
  // The hand-written data-prop-authored pool pipeline tier
  // (sceneWaterAuthoredPoolVertexSource/FragmentSource, sceneWaterAuthoredShaderSource,
  // waterAuthoredPoolPipelineCache/Failures) has been retired: the pool pass
  // resolves Selena-primary (sceneWaterPoolUsesSelena/getWaterPoolSelenaDraw)
  // falling through to this builtin-only getWaterPoolPipeline.
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredPoolVertexSource/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredPoolFragmentSource/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "poolVertexWGSL"\)/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "poolFragmentWGSL"\)/);
  assert.doesNotMatch(webgpu, /waterAuthoredPoolPipelineCache = new Map/);
  assert.doesNotMatch(webgpu, /waterAuthoredPoolPipelineFailures = new Set/);
  assert.match(webgpu, /label: "gosx-water-pool-pass"/);
  assert.match(webgpu, /label: "gosx-water-pool-pass"[\s\S]*?primitive: \{ topology: "triangle-list", cullMode: "back" \}/);
  assert.match(webgpu, /getWaterSelenaMeshDraw\(material, renderContext, system, \{ cullMode: "back" \}\)/);
  assert.match(webgpu, /corner == 1u \|\| corner == 2u \|\| corner == 5u/,
    "box pool must face inward so back-face culling hides the exterior shell");
  assert.match(webgpu, /corner == 1u \|\| corner == 4u \|\| corner == 5u/,
    "box pool floor and walls must share the open-vessel winding contract");
  assert.match(webgpu, /const WATER_POOL_ROUNDED_SEGMENTS: u32 = 44u/);
  assert.match(webgpu, /fn waterPoolRoundedBoundaryPoint/);
  assert.match(webgpu, /fn waterPoolRoundedBoundaryNormal/);
  assert.match(webgpu, /fn waterPoolRoundedVertex/);
  assert.match(webgpu, /roundedPoolVertexCount = 44 \* 9/);
  assert.match(webgpu, /tileRecord = tileURL \? wgpuLoadTexture\(device, tileURL, textureCache\) : null/);
  assert.match(webgpu, /textureSample\(tileTexture, poolSampler, in\.tileUV\)/);
  assert.match(webgpu, /textureSample\(causticTexture, poolSampler, causticUV\)/);
  assert.match(webgpu, /textureSample\(objectShadowTexture, poolSampler, waterUV\)/);
  assert.match(webgpu, /var rounded = sceneWaterPoolShapeRounded\(entry\) && sceneNumber\(entry\.cornerRadius, 0\) > 0\.0001/);
  assert.match(webgpu, /renderPass\.draw\(vertexCount\)/);
  assert.match(webgpu, /stats\.waterPoolDrawVertices \+= vertexCount/);
  assert.match(webgpu, /drawWaterPoolEntries\(mainPass, waterUpdateStats\.records, frameBindGroup\)/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-pool-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-pool-tile-texture-loaded/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-pool-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-pool-vertex-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-pool-fragment-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-pool-fallbacks/);
});

test("Scene3D managed control forms replace the route water-controls bridge", () => {
  const build = fs.readFileSync(path.join(__dirname, "..", "..", "cmd", "buildbootstrap", "main.go"), "utf8");
  const controls = fs.readFileSync(path.join(__dirname, "bootstrap-src", "19b-scene-control-forms.ts"), "utf8");
  const strictSchema = fs.readFileSync(path.join(__dirname, "bootstrap-src", "15-scene-ir-schema-strict.ts"), "utf8");
  const mount = readSceneMountSrc();
  const waterDir = path.join(__dirname, "..", "..", "examples", "gosx-docs", "app", "demos", "water");
  const waterPage = fs.readFileSync(path.join(waterDir, "page.gsx"), "utf8");

  assert.match(build, /bootstrap-src\/19b-scene-control-forms\.ts/);
  assert.match(controls, /function bindSceneManagedControlForms/);
  assert.match(controls, /function registerSceneManagedControlProfile/);
  assert.match(controls, /function publishSceneManagedControlProfiles/);
  assert.match(controls, /SCENE_MANAGED_CONTROL_PROFILES/);
  assert.match(controls, /registerSceneManagedControlProfile\("fluid-object"/);
  assert.doesNotMatch(controls, /registerSceneManagedControlProfile\("water"/);
  assert.match(controls, /__gosx_scene3d_register_control_profile/);
  assert.match(controls, /sceneManagedControlProfiles/);
  assert.match(controls, /data-gosx-scene3d-control-profiles/);
  assert.match(controls, /function sceneManagedControlData/);
  assert.match(controls, /function sceneManagedControlScope/);
  assert.match(controls, /function sceneManagedControlBindDisclosure/);
  assert.match(controls, /function sceneManagedControlBindPanelToggles/);
  assert.match(controls, /data-gosx-scene3d-control-toggle/);
  assert.match(controls, /data-gosx-scene3d-control-open/);
  assert.match(controls, /data-gosx-scene3d-panel-toggle/);
  assert.match(controls, /data-gosx-scene3d-panel-open/);
  assert.match(controls, /data-gosx-scene3d-control-scope/);
  assert.match(controls, /data-gosx-scene3d-panel-scope/);
  assert.match(controls, /data-gosx-scene3d-active-panel/);
  assert.match(controls, /function sceneManagedFluidObjectProfile/);
  assert.match(controls, /data-gosx-scene3d-control-data/);
  assert.match(controls, /data-gosx-scene3d-control-data-ref/);
  assert.match(controls, /SCENE_CMD_SET_MODELS/);
  assert.match(controls, /SCENE_CMD_SET_PARTICLES/);
  assert.match(controls, /sceneManagedFluidObjectSystemPayload/);
  assert.match(controls, /function sceneManagedFluidObjectSetDisabled/);
  assert.match(controls, /function sceneManagedFluidObjectControlState/);
  assert.match(controls, /objectDisplacementEvents: \[\]/);
  assert.match(controls, /function sceneManagedFluidObjectQueueObjectDisplacementEvent/);
  assert.match(controls, /function sceneManagedFluidObjectObserveSelection/);
  assert.match(controls, /sceneManagedFluidObjectInactiveY/);
  assert.match(controls, /function sceneManagedFluidObjectStepPhysics/);
  assert.match(controls, /function sceneManagedFluidObjectPhysicsSettings/);
  assert.match(controls, /function sceneManagedFluidObjectInteractionSettings/);
  assert.match(controls, /minZoomDistance: 2/);
  assert.match(controls, /maxZoomDistance: 10/);
  assert.match(controls, /function sceneManagedFluidObjectSceneSystem/);
  assert.match(controls, /function sceneManagedFluidObjectWaterSystemByID/);
  assert.match(controls, /const selected = sceneManagedFluidObjectWaterSystemByID\(systems, waterID\)/);
  assert.match(controls, /function sceneManagedFluidObjectInteractionProfile/);
  assert.match(controls, /sceneManagedFluidObjectInteractionProfile\(form, sceneState, profile\) !== "water-object-drop-orbit"/);
  assert.match(controls, /form\.dataset\.gosxScene3dInteractionProfile = interactionProfile/);
  assert.match(controls, /function sceneManagedFluidObjectQueueDrop/);
  assert.match(controls, /function sceneManagedControlCamera/);
  assert.match(controls, /typeof options\.getCamera === "function"/);
  assert.match(controls, /function sceneManagedControlOrbitState/);
  assert.match(controls, /typeof options\.getOrbitState === "function"/);
  assert.match(controls, /function sceneManagedFluidObjectOrbitLightDirection/);
  assert.match(controls, /function sceneManagedFluidObjectCameraLightDirection/);
  assert.match(controls, /function sceneManagedFluidObjectSyncLightDirection/);
  assert.match(controls, /lightCameraKey: ""/);
  assert.match(controls, /function sceneManagedFluidObjectCameraLightKey/);
  assert.match(controls, /function sceneManagedFluidObjectLightCameraChanged/);
  assert.match(controls, /const orbit = sceneManagedControlOrbitState\(sceneState, options\)/);
  assert.match(controls, /sceneManagedFluidObjectCameraLightDirection\(camera, orbit\)/);
  assert.match(controls, /let lightFrame = 0/);
  assert.match(controls, /function scheduleLightSync/);
  assert.match(controls, /sceneManagedFluidObjectLightCameraChanged\(controlState, sceneState, options\)/);
  assert.match(controls, /sceneManagedFluidObjectCancelFrame\(lightFrame\)/);
  assert.match(controls, /controlState\.settingLightDirection = true/);
  assert.match(controls, /controlState\.settingLightDirection = false/);
  assert.match(controls, /sceneManagedFluidObjectApply\(form, sceneState, applyCommands, options\)/);
  assert.match(controls, /next\.lightDirectionX = sceneNumber\(lightDirection\.x, 2\)/);
  assert.match(controls, /function sceneManagedFluidObjectPointerRay/);
  assert.match(controls, /function sceneManagedFluidObjectPointerID/);
  assert.match(controls, /function sceneManagedFluidObjectTouchDistance/);
  assert.match(controls, /function sceneManagedFluidObjectZoomCameraByScale/);
  assert.match(controls, /function sceneManagedFluidObjectZoomCameraByWheel/);
  assert.match(controls, /Math\.exp\(sceneNumber\(event && event\.deltaY, 0\) \* 0\.001\)/);
  assert.match(controls, /function sceneManagedFluidObjectStopCameraInertia/);
  assert.match(controls, /sceneManagedFluidObjectStopCameraInertia\(options\)/);
  assert.match(controls, /const touchPointers = new Map\(\)/);
  assert.match(controls, /let pinchDistance = null/);
  assert.match(controls, /pinching = true/);
  assert.match(controls, /sceneManagedFluidObjectZoomCameraByScale\(sceneState, options, pinchDistance \/ nextDistance, profile\)/);
  assert.match(controls, /sceneScreenToRay\(sample\.pointerX, sample\.pointerY, sample\.rect\.width, sample\.rect\.height, camera\)/);
  assert.match(controls, /sceneRayIntersectYPlane\(raySample\.ray, 0\)/);
  assert.match(controls, /function sceneManagedFluidObjectStartInteraction/);
  assert.match(controls, /sceneManagedFluidObjectActiveObjectHit/);
  assert.match(controls, /function sceneManagedFluidObjectAnalyticObjectHit/);
  assert.match(controls, /sceneRayIntersectSphere\(raySample\.ray/);
  assert.match(controls, /sceneRayIntersectAABB\(raySample\.ray/);
  assert.match(controls, /controlState\.pointerMode = "MoveObject"/);
  assert.match(controls, /controlState\.pointerMode = "AddDrops"/);
  assert.match(controls, /controlState\.pointerMode = "OrbitCamera"/);
  assert.match(controls, /function sceneManagedFluidObjectDragObject/);
  assert.match(controls, /function sceneManagedFluidObjectCameraDragNormal/);
  assert.match(controls, /typeof sceneRotatePoint === "function"/);
  assert.match(controls, /\{ x: 0, y: 0, z: 1 \}/);
  assert.match(controls, /const nz = -sceneNumber\(cam && cam\.z, 0\) - hz/);
  assert.match(controls, /sceneRayIntersectPlane\(raySample\.ray, drag\.previousHit, drag\.dragPlaneNormal\)/);
  assert.match(controls, /sceneManagedFluidObjectConsumePointerEvent/);
  assert.match(controls, /addEventListener\("pointerdown", onPointerDown, true\)/);
  assert.match(controls, /let suppressMouseUntil = 0/);
  assert.match(controls, /suppressMouseUntil = sceneManagedFluidObjectNowSeconds\(\) \+ 0\.8/);
  assert.match(controls, /function onMouseDown\(event\)/);
  assert.match(controls, /addEventListener\("mousedown", onMouseDown, true\)/);
  assert.match(controls, /addEventListener\("mousemove", onMouseMove, true\)/);
  assert.match(controls, /removeEventListener\("mouseup", onMouseEnd, true\)/);
  assert.match(controls, /function onWheel\(event\)/);
  assert.match(controls, /sceneManagedFluidObjectZoomCameraByWheel\(sceneState, options, event, profile\)/);
  assert.match(controls, /addEventListener\("wheel", onWheel, \{ passive: false, capture: true \}\)/);
  assert.match(controls, /removeEventListener\("wheel", onWheel, true\)/);
  assert.match(controls, /const SCENE_MANAGED_FLUID_OBJECT_MIN_STRAIGHT_POOL_EDGE = 0\.05/);
  assert.match(controls, /function sceneManagedFluidObjectMaxCornerRadius/);
  assert.match(controls, /sceneManagedFluidObjectMaxCornerRadius\(poolWidth, poolLength\)/);
  assert.match(controls, /function sceneManagedFluidObjectClampCornerRadius/);
  assert.match(controls, /cornerRadius: sceneManagedFluidObjectClampCornerRadius\(sceneManagedFluidObjectField\(form, "cornerRadius"\)/);
  assert.match(controls, /function sceneManagedFluidObjectEffectivePoolControls/);
  assert.match(controls, /function sceneManagedFluidObjectPoolKey/);
  assert.match(controls, /controlState\.poolKey = poolKey/);
  assert.match(controls, /const poolWidth = rounded \? sceneManagedFluidObjectClamp\(sceneNumber\(controls && controls\.poolWidth, 1\), 0\.5, 3\) : 1/);
  assert.match(controls, /cornerRadius: rounded \? sceneManagedFluidObjectClampCornerRadius\(controls && controls\.cornerRadius, poolWidth, poolLength\) : 0/);
  assert.match(controls, /const maxCornerRadius = sceneManagedFluidObjectMaxCornerRadius\(pool\.poolWidth, pool\.poolLength\)/);
  assert.match(controls, /cornerField\.max = String\(maxCornerRadius\)/);
  assert.match(controls, /cornerField\.value = String\(pool\.cornerRadius\)/);
  assert.match(controls, /form\.dataset\.maxCornerRadius = String\(rounded \? maxCornerRadius : 0\)/);
  assert.match(controls, /hit\.x \/ Math\.max\(0\.001, pool\.poolWidth\)/);
  assert.match(controls, /source: "ray-plane"/);
  assert.match(controls, /dropEventID/);
  assert.match(controls, /dropEventRadius/);
  assert.match(controls, /dropEventStrength/);
  assert.match(controls, /event\.code === "Space"/);
  assert.match(controls, /event\.code === "KeyG"/);
  assert.match(controls, /event\.code === "KeyL"/);
  assert.match(controls, /data-gosx-scene3d-fluid-drop-events/);
  assert.match(controls, /function sceneManagedFluidObjectObjectStep/);
  assert.match(controls, /poolChanged/);
  assert.match(controls, /syncPoolPrevious/);
  assert.match(controls, /previousSet: syncPoolPrevious \? false : \(moved \|\| !!state\.transitionPending\)/);
  assert.match(controls, /velocity\.y \+= settings\.gravityY/);
  assert.match(controls, /next\.objectSubtype = config\.objectSubtype \|\| ""/);
  assert.match(controls, /objectPreviousSet/);
  assert.match(controls, /objectDisplacementEvents/);
  assert.match(controls, /objectPreviousY/);
  assert.match(controls, /data-gosx-scene3d-controls-ready/);
  assert.match(controls, /data-gosx-scene3d-fluid-object-controls-ready/);
  assert.match(controls, /const physicsAvailable = active !== "None"/);
  assert.match(controls, /sceneManagedFluidObjectSetDisabled\(form, "gravity", !physicsAvailable\)/);
  assert.match(controls, /sceneManagedFluidObjectSetDisabled\(form, "densityEnabled", !physicsAvailable\)/);
  assert.match(controls, /sceneManagedFluidObjectSetDisabled\(form, "density", !physicsAvailable \|\| !controls\.densityEnabled\)/);
  assert.match(controls, /form\.dataset\.physicsAvailable = String\(physicsAvailable\)/);
  assert.match(controls, /form\.dataset\.densityOpen = String\(physicsAvailable && controls\.densityEnabled\)/);
  assert.match(controls, /function sceneManagedFluidObjectReflectObjectState/);
  assert.match(controls, /function sceneManagedFluidObjectReflectPointerMode/);
  assert.match(controls, /function sceneManagedFluidObjectReflectLightDirection/);
  assert.match(controls, /data-gosx-scene3d-fluid-object-active/);
  assert.match(controls, /data-gosx-scene3d-fluid-pointer-mode/);
  assert.match(controls, /data-gosx-scene3d-fluid-object-x/);
  assert.match(controls, /data-gosx-scene3d-fluid-object-previous-x/);
  assert.match(controls, /sceneManagedFluidObjectReflectPointerMode\(form, controlState\)/);
  assert.match(controls, /data-gosx-scene3d-fluid-light-x/);
  assert.match(controls, /data-gosx-scene3d-fluid-light-setting/);
  assert.match(controls, /sceneManagedFluidObjectReflectLightDirection\(form, controlState\)/);
  assert.match(controls, /if \(field\.disabled\) return false/);
  for (const field of [
    "seedWGSL",
    "dropWGSL",
    "displacementWGSL",
    "simulationWGSL",
    "normalWGSL",
    "causticsWGSL",
    "poolVertexWGSL",
    "poolFragmentWGSL",
    "surfaceVertexWGSL",
    "surfaceFragmentWGSL",
    "surfaceBelowFragmentWGSL",
    "objectShadowWGSL",
    "objectMeshShadowVertexWGSL",
    "objectMeshShadowFragmentWGSL",
  ]) {
    assert.match(strictSchema, new RegExp(`"${field}"`));
  }
  for (const field of [
    "seedWGSLRef",
    "dropWGSLRef",
    "displacementWGSLRef",
    "simulationWGSLRef",
    "normalWGSLRef",
    "causticsWGSLRef",
    "poolVertexWGSLRef",
    "poolFragmentWGSLRef",
    "surfaceVertexWGSLRef",
    "surfaceFragmentWGSLRef",
    "surfaceBelowFragmentWGSLRef",
    "objectShadowWGSLRef",
    "objectMeshShadowVertexWGSLRef",
    "objectMeshShadowFragmentWGSLRef",
  ]) {
    assert.match(strictSchema, new RegExp(`"${field}"`));
  }
  assert.doesNotMatch(controls, /SCENE_MANAGED_WATER_OBJECTS/);
  assert.doesNotMatch(controls, /sceneManagedFluidObjectTorusKnotDisplacementSpheres/);
  assert.doesNotMatch(controls, /sceneManagedFluidObjectDuckDisplacementSpheres/);
  assert.doesNotMatch(controls, /Duck\.gltf/);
  assert.doesNotMatch(controls, /objectKind: "compound"/);
  assert.doesNotMatch(controls, /config\.objectY - 0\.08/);
  assert.doesNotMatch(controls, /controls\.densityEnabled \? controls\.density/);
  assert.doesNotMatch(controls, /sceneManagedFluidObjectRoundedFloorVertices/);
  assert.doesNotMatch(controls, /sceneManagedFluidObjectRoundedWallVertices/);
  assert.doesNotMatch(controls, /pool-rounded-floor/);
  assert.doesNotMatch(controls, /pool-rounded-walls/);
  assert.doesNotMatch(controls, /water-surface/);
  assert.doesNotMatch(controls, /caustic-floor/);
  assert.doesNotMatch(controls, /foam-rings/);
  assert.doesNotMatch(controls, /pool-north-wall/);
  assert.doesNotMatch(controls, /pool-south-wall/);
  assert.doesNotMatch(controls, /pool-west-wall/);
  assert.doesNotMatch(controls, /pool-east-wall/);
  assert.doesNotMatch(controls, /pool-rim-lines/);
  assert.doesNotMatch(controls, /controls\.followCamera \? -0\.45 : 2/);
  assert.doesNotMatch(controls, /sceneManagedFluidObjectSetChecked\(form, "followCamera", true\)/);
  assert.doesNotMatch(controls, /sceneManagedFluidObjectSetChecked\(form, "followCamera", false\)/);
  assert.match(controls, /x: -Math\.sin\(yaw\) \* cosPitch/);
  assert.match(controls, /y: Math\.sin\(pitch\)/);
  assert.match(controls, /x: Math\.sin\(yaw\) \* cosPitch/);
  assert.match(controls, /z: -Math\.cos\(yaw\) \* cosPitch/);
  assert.doesNotMatch(controls, /data-gosx-scene3d-water-controls['"\]]/);
  assert.doesNotMatch(controls, /data-water-/);
  assert.doesNotMatch(controls, /sceneManagedWater/);
  assert.match(mount, /bindSceneManagedControlForms\(ctx\.mount, sceneState/);
  assert.match(mount, /function publishSceneWaterStateSnapshot/);
  assert.match(mount, /data-gosx-scene3d-water-state-rounded-systems/);
  assert.match(mount, /data-gosx-scene3d-water-state-active-object/);
  assert.match(mount, /publishSceneWaterStateSnapshot\(ctx\.mount, sceneState\)/);
  assert.match(mount, /const result = applySceneCommands\(sceneState, commands\);\n\s+publishSceneWaterStateSnapshot\(ctx\.mount, sceneState\)/);
  assert.match(mount, /function currentMountedSceneOrbitState/);
  assert.match(mount, /getCamera: currentMountedSceneCamera/);
  assert.match(mount, /getOrbitState: currentMountedSceneOrbitState/);
  assert.match(mount, /setCamera: function\(camera\)/);
  assert.match(mount, /applyMountedSceneCamera\(camera, "managed-control-forms-camera"\)/);
  assert.match(mount, /getControlTarget: function\(\)/);
  assert.match(mount, /sceneControlsTarget\(props\)/);
  assert.match(mount, /getBundle: function\(\)/);
  assert.doesNotMatch(mount, /bindSceneManagedWaterControls/);
  assert.doesNotMatch(mount, /bindSceneManagedFluidObjectControls/);
  assert.match(bootstrapFeatureScene3DSource, /registerSceneManagedControlProfile/);
  assert.match(bootstrapFeatureScene3DSource, /__gosx_scene3d_register_control_profile/);
  assert.match(bootstrapFeatureScene3DSource, /stopCameraInertia:function\(/);
  assert.match(bootstrapFeatureScene3DSource, /stopCameraInertia\(\)/);
  assert.match(bootstrapFeatureScene3DSource, /sceneManagedControlProfiles/);
  assert.match(bootstrapFeatureScene3DSource, /data-gosx-scene3d-control-profiles/);
  assert.match(waterPage, /data-gosx-scene3d-control-form="fluid-object"/);
  assert.match(waterPage, /data-gosx-scene3d-control-open="false"/);
  assert.match(waterPage, /data-gosx-scene3d-control-scope="true"/);
  assert.doesNotMatch(waterPage, /data-water-/);
  assert.match(waterPage, /data-gosx-scene3d-panel-scope="true"/);
  assert.match(waterPage, /data-gosx-scene3d-control-toggle="true"/);
  assert.match(waterPage, /data-gosx-scene3d-control-body="true"/);
  assert.match(waterPage, /data-gosx-scene3d-control-group="Scene"/);
  assert.match(waterPage, /data-gosx-scene3d-control-group="Object"/);
  assert.match(waterPage, /data-gosx-scene3d-control-group="Pool"/);
  assert.match(waterPage, /data-gosx-scene3d-control-group="Lights"/);
  assert.match(waterPage, /data-gosx-scene3d-panel-toggle="water-demo-help"/);
  assert.match(waterPage, /data-gosx-scene3d-help-panel="true"/);
  assert.match(waterPage, /data-gosx-scene3d-rounded-control="true"/);
  assert.match(waterPage, /data-gosx-scene3d-pool-boundary-control="true"/);
  assert.match(waterPage, /Water, in motion\./);
  assert.match(waterPage, /jeantimex\/threejs-water/);
  assert.match(waterPage, /Press SPACEBAR to pause and unpause/);
  assert.match(waterPage, /controlTargetY=\{-0\.5\}/);
  // gosx fmt renders the <Camera> tag with one attribute per line, so match
  // the camera-position contract per attribute rather than as a single line.
  assert.match(waterPage, /x=\{1\.38\}/);
  assert.match(waterPage, /y=\{1\.52\}/);
  assert.match(waterPage, /z=\{2\.87\}/);
  assert.match(waterPage, /interactionProfile="water-object-drop-orbit"/);
  assert.match(waterPage, /interactionTarget="water-main"/);
  assert.match(waterPage, /interactionObject="Sphere"/);
  assert.doesNotMatch(waterPage, /water-demo__overlay/);
  assert.doesNotMatch(waterPage, /water-demo__readout/);
  assert.doesNotMatch(waterPage, /Selena Surface/);
  assert.match(waterPage, /shallowColor="#54c4d8"/);
  assert.match(waterPage, /deepColor="#041c38"/);
  assert.match(waterPage, /aboveWaterColorR=\{0\.18\}/);
  assert.match(waterPage, /aboveWaterColorG=\{0\.78\}/);
  assert.match(waterPage, /aboveWaterColorB=\{0\.98\}/);
  assert.match(waterPage, /qualityProfiles=\{data\.diagQualityProfiles\}/);
  assert.match(waterPage, /msaaSamples=\{data\.diagMsaa\}/);
  assert.match(waterPage, /antialias=\{data\.diagAntialias\}/);
  assert.match(waterPage, /capabilityTier=\{data\.diagCapabilityTier\}/);
  assert.match(waterPage, /aria-current=\{data\.diagQualityBalancedCurrent\}/);
  // The hand-written Elio/Selena *WGSL props have been retired -- Selena is
  // the sole primary WGSL source now (see the *SelenaWGSL props below).
  assert.doesNotMatch(waterPage, /displacementWGSL=\{data\.waterDisplacementWGSL\}/);
  assert.doesNotMatch(waterPage, /simulationWGSL=\{data\.waterSimulationWGSL\}/);
  assert.doesNotMatch(waterPage, /normalWGSL=\{data\.waterNormalWGSL\}/);
  assert.match(waterPage, /displacementSelenaWGSL=\{data\.waterDisplacementSelenaWGSL\}/);
  assert.match(waterPage, /simulationSelenaWGSL=\{data\.waterSimulationSelenaWGSL\}/);
  assert.match(waterPage, /normalSelenaWGSL=\{data\.waterNormalSelenaWGSL\}/);
  assert.match(waterPage, /data-gosx-scene3d-control-subject="water-main"/);
  assert.match(waterPage, /data-gosx-scene3d-control-data=\{data\.waterControlData\}/);
  assert.doesNotMatch(waterPage, /water-controls\.js/);
  assert.doesNotMatch(waterPage, /pool-north-wall|pool-south-wall|pool-west-wall|pool-east-wall|pool-rim-lines/);
  assert.doesNotMatch(waterPage, /data\.poolRimPoints|data\.poolRimSegments/);
  const waterProgram = fs.readFileSync(path.join(waterDir, "program.go"), "utf8");
  // The hand-written Elio/Selena shader trees (shaders/jeantimex-water.elio/,
  // shaders/jeantimex-water.sel/) and shader_sources.go have been deleted --
  // Selena (shaders/jeantimex-water.selena/, selena_glsl.go) is the sole
  // primary WGSL source now. program.go still carries the demo's
  // route-authored control-data JSON glue, which is unaffected.
  assert.match(waterProgram, /waterControlDataJSON/);
  assert.match(waterProgram, /torusKnotDisplacementSpheres/);
  assert.match(waterProgram, /duckDisplacementSpheres/);
  assert.match(waterProgram, /\/water\/models\/duck\/Duck\.gltf/);
  assert.doesNotMatch(waterProgram, /poolRimPoints|poolRimSegments/);
  assert.doesNotMatch(waterProgram, /waterShaderSources|waterComputeSourceFiles|waterMaterialSourceFiles/);
});

test("Scene3D orbit controls keep upstream-style release inertia", () => {
  const mount = readSceneMountSrc();

  assert.match(mount, /const SCENE_ORBIT_MAX_SPEED = Math\.PI \* 6;/);
  assert.match(mount, /const SCENE_ORBIT_DAMPING = 6;/);
  assert.match(mount, /const SCENE_ORBIT_STOP_SPEED = \(Math\.PI \/ 180\) \* 0\.01;/);
  assert.match(mount, /orbitVelocityYaw: 0/);
  assert.match(mount, /orbitVelocityPitch: 0/);
  assert.match(mount, /orbitLastMoveMS: 0/);
  assert.match(mount, /rotateMode: sceneControlsRotateMode\(props\)/);
	assert.match(mount, /rotateDirection: sceneControlsRotateDirection\(props\)/);
  assert.match(mount, /minDistance,\n\s+maxDistance,\n\s+pitchLimit: sceneControlsPitchLimit\(props\)/);
  assert.match(mount, /function sceneControlsRotateMode\(props\)/);
	assert.match(mount, /function sceneControlsRotateDirection\(props\)/);
	assert.match(mount, /case "grab":/);
  assert.match(mount, /return "pixel-degrees";/);
  assert.match(mount, /function sceneControlsMinDistance\(props\)/);
  assert.match(mount, /function sceneControlsMaxDistance\(props, minDistance\)/);
  assert.match(mount, /function sceneControlsPitchLimit\(props\)/);
  assert.match(mount, /controls\.rotateMode === "pixel-degrees"/);
	assert.match(mount, /sample\.deltaX \* pixelRadians \* rotateDirection/);
	assert.match(mount, /sample\.deltaY \* pixelRadians \* pitchDirection/);
  assert.match(mount, /SCENE_ORBIT_MAX_PITCH_LIMIT = Math\.PI \/ 2 - \(Math\.PI \/ 180\) \* 0\.001/);
  assert.match(mount, /function sceneOrbitStopInertia\(controls\)/);
  assert.match(mount, /function sceneOrbitInertiaActive\(controls\)/);
  assert.match(mount, /function sceneOrbitApplyInertia\(controls, readSourceCamera, deltaSeconds\)/);
  assert.match(mount, /const damping = Math\.exp\(-SCENE_ORBIT_DAMPING \* seconds\);/);
  assert.match(mount, /controls\.orbitVelocityYaw \*= damping;/);
  assert.match(mount, /controls\.orbitVelocityPitch \*= damping;/);
  assert.match(mount, /controls\.orbitVelocityYaw = sceneClamp\(deltaYaw \/ seconds, -SCENE_ORBIT_MAX_SPEED, SCENE_ORBIT_MAX_SPEED\);/);
  assert.match(mount, /controls\.orbitVelocityPitch = sceneClamp\(deltaPitch \/ seconds, -SCENE_ORBIT_MAX_SPEED, SCENE_ORBIT_MAX_SPEED\);/);
  assert.match(mount, /const releaseDamping = Math\.exp\(-SCENE_ORBIT_DAMPING \* releaseDelay\);/);
  assert.match(mount, /function sceneOrbitFinishDrag\(controls, canvas, detachDocumentListeners, event, scheduleOrbitInertia\)/);
  assert.match(mount, /if \(typeof scheduleOrbitInertia === "function"\) \{\s*scheduleOrbitInertia\(\);\s*\}/s);
  assert.match(mount, /function cancelOrbitInertia\(\)/);
  assert.match(mount, /function scheduleOrbitInertia\(\)/);
  assert.match(mount, /sceneOrbitApplyInertia\(controls, readSourceCamera, delta\)/);
  assert.match(mount, /scheduleRender\("controls-inertia"\);/);
  assert.match(mount, /cancelOrbitInertia\(\);\s*sceneOrbitStartDrag/s);
  assert.match(mount, /detachDocumentListeners\(\);\s*cancelOrbitInertia\(\);/s);
  assert.match(mount, /function applySceneControlsCamera\(controls, camera\) \{[\s\S]*sceneOrbitStopInertia\(controls\);/);
  assert.match(mount, /function sceneOrbitApplyWheel\(controls, readSourceCamera, scheduleRender, event\) \{[\s\S]*sceneOrbitStopInertia\(controls\);/);
  assert.match(bootstrapFeatureScene3DSource, /controls-inertia/);
});

test("Scene3D orbit controls expose focused keyboard exploration and authored reset", () => {
  const mount = readSceneMountSrc();

  assert.match(mount, /function sceneOrbitKeyCode\(event\)/);
  assert.match(mount, /case "arrowleft":/);
  assert.match(mount, /case "home":\s+return "reset";/);
  assert.match(mount, /function sceneOrbitApplyKey\(controls, readSourceCamera, key\)/);
  assert.match(mount, /applySceneControlsCamera\(controls, sourceCamera\);/);
  assert.match(mount, /canvas\.setAttribute\("tabindex", "0"\);/);
  assert.match(mount, /canvas\.setAttribute\("aria-keyshortcuts", "ArrowLeft ArrowRight ArrowUp ArrowDown Home \+ -"\);/);
  assert.match(mount, /document\.activeElement !== canvas/);
  assert.match(mount, /scheduleRender\(orbitKey === "reset" \? "controls-reset" : "controls-keyboard"\);/);
  assert.match(mount, /document\.addEventListener\("keydown", onKeyDown\);/);
  assert.match(mount, /document\.removeEventListener\("keydown", onKeyDown\);/);
});

test("Scene3D WebGPU water consumes caustic reflection refraction optics flags", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /opticsFlags: vec4f/);
  assert.match(webgpu, /function sceneWaterOpticsFlags/);
  assert.match(webgpu, /caustics: sceneBool\(entry && entry\.caustics, true\)/);
  assert.match(webgpu, /reflection: sceneBool\(entry && entry\.reflection, true\)/);
  assert.match(webgpu, /refraction: sceneBool\(entry && entry\.refraction, true\)/);
  assert.match(webgpu, /waterUniformScratchF\[44\] = optics\.caustics \? 1 : 0/);
  assert.match(webgpu, /waterUniformScratchF\[45\] = optics\.reflection \? 1 : 0/);
  assert.match(webgpu, /waterUniformScratchF\[46\] = optics\.refraction \? 1 : 0/);
  assert.match(webgpu, /waterUniformScratchF\[47\] = optics\.object \? 1 : 0/);
  assert.match(webgpu, /waterUniformScratchF\[43\] = objectState\.subtype \|\| 0/);
  assert.match(webgpu, /let causticsEnabled = clamp\(params\.opticsFlags\.x, 0\.0, 1\.0\)/);
  assert.match(webgpu, /let reflectionEnabled = clamp\(params\.opticsFlags\.y, 0\.0, 1\.0\)/);
  assert.match(webgpu, /let refractionEnabled = clamp\(params\.opticsFlags\.z, 0\.0, 1\.0\)/);
  assert.doesNotMatch(webgpu, /objectOpticalFootprint/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-caustic-systems/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-reflection-systems/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-refraction-systems/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-optics-systems/);
});

test("Scene3D WebGPU water renders dynamic caustics to a sampled texture", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");
  const mount = readSceneMountSrc();

  assert.match(webgpu, /SCENE_WATER_CAUSTICS_VERTEX_SOURCE/);
  assert.match(webgpu, /SCENE_WATER_CAUSTICS_FRAGMENT_SOURCE/);
  assert.match(webgpu, /WATER_CAUSTICS_TEXTURE_FORMAT = "rgba8unorm"/);
  assert.match(webgpu, /WATER_CAUSTICS_TEXTURE_SIZE = 1024/);
  assert.match(webgpu, /waterCausticsBindGroupLayout = device\.createBindGroupLayout/);
  assert.match(webgpu, /waterCausticsPipelineLayout = device\.createPipelineLayout/);
  assert.match(webgpu, /waterCausticsPipeline = device\.createRenderPipeline/);
  assert.match(webgpu, /label: "gosx-water-caustics-pass"/);
  assert.match(webgpu, /format: WATER_CAUSTICS_TEXTURE_FORMAT/);
  assert.match(webgpu, /function sceneWaterCausticsResolution/);
  assert.match(webgpu, /sceneWaterCausticsResolution\(entry\)/);
  assert.match(webgpu, /causticsTexture = scopedDevice\.createTexture/);
  assert.match(webgpu, /GPUTextureUsage\.RENDER_ATTACHMENT \| GPUTextureUsage\.TEXTURE_BINDING \| GPUTextureUsage\.COPY_DST/);
  assert.match(webgpu, /createWaterCausticsBindGroup/);
  // waterManifestShaderSourcesByID/activeWaterShaderSourcesByID and the
  // generic bundle/manifest water-source diagnostic functions remain (they
  // no longer feed any pipeline decision); the hand-written
  // data-prop-authored caustics pipeline tier itself
  // (sceneWaterAuthoredShaderSource, sceneWaterAuthoredCausticsSource/Pipeline,
  // waterAuthoredCausticsPipelineCache) has been retired -- caustics resolves
  // Selena-primary falling through directly to the builtin waterCausticsPipeline.
  assert.match(webgpu, /waterAuthoredCausticsPipelineLastError = ""/);
  assert.match(webgpu, /waterManifestShaderSourcesByID = null/);
  assert.match(webgpu, /activeWaterShaderSourcesByID = null/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredShaderSource/);
  assert.match(webgpu, /function sceneWaterManifestShaderSources/);
  assert.match(webgpu, /function sceneWaterShaderSourcesFromEntries/);
  assert.match(webgpu, /function sceneHydrateWaterEntriesFromSources/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredCausticsSource/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredCausticsPipeline/);
  assert.doesNotMatch(webgpu, /waterAuthoredCausticsPipelineCache = new Map/);
  assert.doesNotMatch(webgpu, /waterAuthoredCausticsPipelineCache\.set\(key, pending\)/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "causticsWGSL"\)/);
  assert.match(webgpu, /incomingWaterShaderSourcesByID/);
  assert.match(webgpu, /bundle\.waterSystems = sceneHydrateWaterEntriesFromSources\(bundle\.waterSystems, incomingWaterShaderSourcesByID\)/);
  assert.match(webgpu, /var pipeline = waterCausticsPipeline;/);
  assert.match(webgpu, /function renderWaterCausticsPass/);
  assert.match(webgpu, /pass\.draw\(3\)/);
  assert.match(webgpu, /renderWaterCausticsPass\(encoder, system\)/);
  assert.match(webgpu, /var causticTexture: texture_2d<f32>/);
  assert.match(webgpu, /textureSample\(causticTexture, causticSampler/);
  assert.match(webgpu, /var objectShadowTexture: texture_2d<f32>/);
  assert.match(webgpu, /textureSample\(objectShadowTexture, objectShadowSampler/);
  assert.match(webgpu, /waterCausticPasses/);
  assert.match(webgpu, /waterAuthoredCausticPasses/);
  assert.match(webgpu, /waterAuthoredCausticFallbacks/);
  assert.match(webgpu, /waterAuthoredCausticFallbackReason/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-caustic-fallback-reason/);
  assert.match(webgpu, /waterAuthoredCausticSourceBytes/);
  assert.match(webgpu, /waterAuthoredCausticSourceBytes: waterUpdateStats\.waterAuthoredCausticSourceBytes/);
  assert.match(webgpu, /waterResolvedCausticSourceBytes: waterUpdateStats\.waterResolvedCausticSourceBytes/);
  assert.match(webgpu, /waterBundleCausticSourceBytes: waterUpdateStats\.waterBundleCausticSourceBytes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-caustic-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-caustic-texture-pixels/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-caustic-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-caustic-fallbacks/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-caustic-source-bytes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-resolved-caustic-source-bytes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-bundle-caustic-source-bytes/);
  assert.match(mount, /function sceneMountedWaterShaderSources/);
  assert.match(mount, /const mountedWaterShaderSources = [\s\S]*sceneMountedWaterShaderSources\(\)/);
  assert.equal((mount.match(/sceneMountedWaterShaderSources\(\)/g) || []).length, 2,
    "water manifest source extraction must be defined and invoked only once per mount");
  assert.doesNotMatch(mount, /waterShaderSourcesByID = sceneMountedWaterShaderSources\(\)/);
  assert.match(mount, /reason: "water-simulation"/);
  assert.match(mount, /reason: "water-paused"/);
  assert.match(mount, /data-gosx-scene3d-water-renderer/);
  assert.match(mount, /data-gosx-scene3d-water-frame-seq/);
  assert.match(mount, /data-gosx-scene3d-water-simulation-seq/);
  assert.match(mount, /data-gosx-scene3d-water-unsupported-reason/);
  assert.match(mount, /data-gosx-scene3d-water-lifecycle/);
  assert.match(mount, /data-gosx-scene3d-water-state-pool-width/);
  assert.match(mount, /data-gosx-scene3d-water-state-pool-height/);
  assert.match(mount, /data-gosx-scene3d-water-state-pool-length/);
  assert.match(mount, /unsupportedReason: "water-webgl2-unavailable"/);
  assert.match(mount, /if \(sceneFirstWaterEntry\(props\)\) \{[\s\S]*return \{\s*renderer: null,[\s\S]*unsupportedReason: "water-webgl2-unavailable"/);
  assert.match(core, /function sceneObjectAnimated\(object\) \{[\s\S]*hasOwnProperty\.call\(object, "visible"\)[\s\S]*!sceneBool\(object\.visible, true\)/);
  assert.match(mount, /SCENE_MOUNT_WATER_SOURCE_ID_FIELDS = \["computeSource", "materialSource"\]/);
  assert.match(mount, /SCENE_MOUNT_WATER_SOURCE_FILE_MAP_FIELDS = \["computeSourceFiles", "materialSourceFiles"\]/);
  assert.match(mount, /record\[name\] = files/);
  assert.match(mount, /hydrated\[name\] = files/);
  assert.match(mount, /function sceneHydrateBundleWaterShaderSources/);
  assert.match(mount, /sceneHydrateBundleWaterShaderSources\(effectiveBundle, effectiveBundle\.waterShaderSourcesByID\)/);
});

test("Scene3D WebGPU water renders upstream-style object texture targets", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");
  const mount = readSceneMountSrc();
  const geometry = fs.readFileSync(path.join(__dirname, "bootstrap-src", "12-scene-geometry.ts"), "utf8");
  const waterPage = fs.readFileSync(path.join(__dirname, "..", "..", "examples", "gosx-docs", "app", "demos", "water", "page.gsx"), "utf8");
  const waterProgram = fs.readFileSync(path.join(__dirname, "..", "..", "examples", "gosx-docs", "app", "demos", "water", "program.go"), "utf8");

  assert.match(webgpu, /SCENE_WATER_OBJECT_TEXTURE_VERTEX_SOURCE/);
  assert.match(webgpu, /SCENE_WATER_OBJECT_TEXTURE_FRAGMENT_SOURCE/);
  assert.match(webgpu, /SCENE_WATER_OBJECT_SHADOW_FRAGMENT_SOURCE/);
  assert.match(webgpu, /SCENE_WATER_OBJECT_MESH_SHADOW_VERTEX_SOURCE/);
  assert.match(webgpu, /SCENE_WATER_OBJECT_MESH_SHADOW_FRAGMENT_SOURCE/);
  assert.match(webgpu, /refract\(-normalize\(shadow\.light\.xyz\), vec3f\(0\.0, 1\.0, 0\.0\), 1\.0 \/ 1\.333\)/);
  assert.match(webgpu, /worldPosition\.xz - worldPosition\.y \* refractedLight\.xz \/ refractedY/);
  assert.match(webgpu, /function sceneWaterObjectMeshFragmentSource/);
  assert.match(webgpu, /let texturePassMode = " \+ mode \+ "u/);
  assert.match(webgpu, /texturePassMode == 2u && in\.worldPos\.y < 0\.0/);
  assert.match(webgpu, /struct ObjectTextureOutput/);
  assert.match(webgpu, /@location\(0\) reflection: vec4f/);
  assert.match(webgpu, /@location\(1\) clippedReflection: vec4f/);
  assert.match(webgpu, /@location\(2\) refraction: vec4f/);
  assert.match(webgpu, /WATER_OBJECT_TEXTURE_FORMAT = "rgba8unorm"/);
  assert.match(webgpu, /WATER_OBJECT_TEXTURE_SIZE = 256/);
  assert.match(webgpu, /WATER_OBJECT_SHADOW_TEXTURE_SIZE = 256/);
  assert.match(webgpu, /waterObjectTextureBindGroupLayout = device\.createBindGroupLayout/);
  assert.match(webgpu, /waterObjectMeshShadowBindGroupLayout = device\.createBindGroupLayout/);
  assert.match(webgpu, /waterObjectTexturePipelineLayout = device\.createPipelineLayout/);
  assert.match(webgpu, /waterObjectMeshShadowPipelineLayout = device\.createPipelineLayout/);
  assert.match(webgpu, /waterObjectTexturePipeline = device\.createRenderPipeline/);
  assert.match(webgpu, /waterObjectShadowPipeline = device\.createRenderPipeline/);
  assert.match(webgpu, /waterObjectMeshShadowPipeline = device\.createRenderPipeline/);
  // The hand-written data-prop-authored object-shadow/object-mesh-shadow
  // pipeline tier has been retired: both passes resolve Selena-primary
  // falling through directly to the builtin waterObjectShadowPipeline/
  // waterObjectMeshShadowPipeline with no more per-entry "authored WGSL"
  // pipeline builder in between.
  assert.doesNotMatch(webgpu, /waterAuthoredObjectShadowPipelineCache = new Map/);
  assert.doesNotMatch(webgpu, /waterAuthoredObjectMeshShadowPipelineCache = new Map/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredObjectShadowPipeline/);
  assert.doesNotMatch(webgpu, /function sceneWaterAuthoredObjectMeshShadowPipeline/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "objectShadowWGSL"\)/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "objectMeshShadowVertexWGSL"\)/);
  assert.doesNotMatch(webgpu, /sceneWaterAuthoredShaderSource\(entry, "objectMeshShadowFragmentWGSL"\)/);
  assert.match(webgpu, /pass\.setPipeline\(waterObjectShadowPipeline\)/);
  assert.match(webgpu, /pass\.setPipeline\(waterObjectMeshShadowPipeline\)/);
  assert.match(webgpu, /waterObjectMeshRefractionFragmentModule = device\.createShaderModule/);
  assert.match(webgpu, /waterObjectMeshClippedFragmentModule = device\.createShaderModule/);
  assert.match(webgpu, /waterObjectMeshShadowVertexModule = device\.createShaderModule/);
  assert.match(webgpu, /waterObjectMeshShadowFragmentModule = device\.createShaderModule/);
  assert.match(webgpu, /function getWaterObjectMeshPipeline/);
  assert.match(webgpu, /label: "gosx-water-object-texture-pass"/);
  assert.match(webgpu, /"gosx-water-object-mesh-refraction-pass"/);
  assert.match(webgpu, /"gosx-water-object-mesh-reflection-pass"/);
  assert.match(webgpu, /"gosx-water-object-mesh-clipped-reflection-pass"/);
  assert.match(webgpu, /"gosx-water-object-mesh-shadow-pass"/);
  assert.match(webgpu, /label: "gosx-water-object-shadow-pass"/);
  assert.match(webgpu, /label: "gosx-water-object-reflection-target"/);
  assert.match(webgpu, /label: "gosx-water-object-clipped-reflection-target"/);
  assert.match(webgpu, /label: "gosx-water-object-refraction-target"/);
  assert.match(webgpu, /label: "gosx-water-object-texture-depth"/);
  assert.match(webgpu, /label: "gosx-water-object-shadow-target"/);
  assert.match(webgpu, /function sceneWaterObjectTextureResolution/);
  assert.match(webgpu, /function sceneWaterObjectShadowResolution/);
  assert.match(webgpu, /function wgpuWaterCubeMapFaceURLs/);
  assert.match(webgpu, /function wgpuLoadCubeTexture/);
  assert.match(webgpu, /GPUTextureUsage\.TEXTURE_BINDING \| GPUTextureUsage\.COPY_DST \| GPUTextureUsage\.RENDER_ATTACHMENT/);
  assert.match(webgpu, /texture_cube<f32>/);
  assert.match(webgpu, /var waterSkyTexture: texture_cube<f32>/);
  assert.match(webgpu, /fn sampleWaterSky\(direction: vec3f\) -> vec3f/);
  assert.match(webgpu, /textureSample\(waterSkyTexture, causticSampler/);
  assert.match(webgpu, /struct WaterObjectTextureMatrices/);
  assert.match(webgpu, /viewProjectionMatrix: mat4x4f/);
  assert.match(webgpu, /reflectionViewProjectionMatrix: mat4x4f/);
  assert.match(webgpu, /@group\(1\) @binding\(8\) var<uniform> objectTextureMatrices: WaterObjectTextureMatrices/);
  assert.match(webgpu, /fn sampleProjectedTexture\(tex: texture_2d<f32>, matrix: mat4x4f, worldPos: vec3f\) -> vec4f/);
  assert.match(webgpu, /textureSampleLevel\(tex, causticSampler, uv, 0\.0\) \* inBounds/);
  assert.match(webgpu, /fn sampleObjectRefraction\(origin: vec3f, ray: vec3f\) -> vec4f/);
  assert.match(webgpu, /sampleProjectedTexture\(objectRefractionTexture, objectTextureMatrices\.viewProjectionMatrix, origin \+ ray \* hit\)/);
  assert.match(webgpu, /fn sampleObjectReflection\(origin: vec3f, ray: vec3f\) -> vec4f/);
  assert.match(webgpu, /sampleProjectedTexture\(objectReflectionTexture, objectTextureMatrices\.reflectionViewProjectionMatrix, origin \+ ray \* hit\)/);
  assert.match(webgpu, /sampleProjectedTexture\(objectClippedReflectionTexture, objectTextureMatrices\.reflectionViewProjectionMatrix, in\.worldPos\)/);
  assert.match(webgpu, /refractionTexel = sampleObjectRefraction\(in\.worldPos, refractDir\)/);
  assert.doesNotMatch(webgpu, /let sampleUV = clamp\(in\.uv \+ n\.xz \* 0\.018/);
  assert.match(webgpu, /viewDimension: "cube"/);
  assert.match(webgpu, /binding: 7, visibility: GPUShaderStage\.FRAGMENT, texture: \{ sampleType: "float", viewDimension: "cube" \}/);
  assert.match(webgpu, /cubeRecord = entry\.cubeMap \? wgpuLoadCubeTexture/);
  assert.match(webgpu, /system\.waterSkyCubePending = cubePending/);
  assert.match(webgpu, /system\.waterSkyCubeFailed = cubeFailed/);
  assert.match(webgpu, /waterSkyCubeTexturePending: 0/);
  assert.match(webgpu, /waterSkyCubeTextureFailed: 0/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-sky-cube-texture-pending/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-sky-cube-texture-failed/);
  assert.match(webgpu, /function sceneWaterLightVector\(entry, fallback\)/);
  assert.match(webgpu, /entry\.lightDirectionX/);
  assert.match(webgpu, /waterLightDirX: waterUpdateStats\.waterLightDirX/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-light-dir-x/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-light-dir-y/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-light-dir-z/);
  assert.match(webgpu, /function sceneWaterObjectTextureTargetSize\(entry, width, height\)/);
  assert.match(webgpu, /WATER_OBJECT_TEXTURE_MAX_SIZE = 2048/);
  assert.match(webgpu, /WATER_OBJECT_TEXTURE_TARGET_COUNT = 3/);
  assert.match(webgpu, /function waterSystemUsesProjectedObjectTextures\(system\)/);
  assert.match(webgpu, /return kind === 3;/);
  assert.match(webgpu, /return waterSystemUsesProjectedObjectTextures\(system\);/);
  assert.match(webgpu, /function sceneWaterObjectTexturePixelBudget\(entry\)/);
  assert.match(webgpu, /function sceneWaterObjectTextureClampToPixelBudget\(size, pixelBudget\)/);
  assert.match(webgpu, /Math\.sqrt\(budget \/ totalPixels\)/);
  assert.match(webgpu, /var objectTextureSize = sceneWaterObjectTextureTargetSize\(entry, width, height\)/);
  assert.match(webgpu, /size: \[objectTextureWidth, objectTextureHeight, 1\]/);
  assert.match(webgpu, /objectTexturePixelBudget: objectTextureSize\.pixelBudget/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-width/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-height/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-pixel-budget/);
  assert.match(webgpu, /var targetWidth = Math\.max\(1, system\.objectTextureWidth \|\| system\.objectTextureResolution \|\| WATER_OBJECT_TEXTURE_SIZE\)/);
  assert.match(webgpu, /var targetHeight = Math\.max\(1, system\.objectTextureHeight \|\| system\.objectTextureResolution \|\| WATER_OBJECT_TEXTURE_SIZE\)/);
  assert.match(webgpu, /uploadFrameUniforms\(bundle && bundle\.camera, targetWidth, targetHeight, false\)/);
  assert.match(webgpu, /uploadWaterReflectionFrameUniforms\(bundle && bundle\.camera, targetWidth, targetHeight, false\)/);
  assert.doesNotMatch(webgpu, /uploadFrameUniforms\(sceneWaterReflectionCamera\(bundle && bundle\.camera\), targetWidth, targetHeight, false\)/);
  assert.match(webgpu, /var objectShadowResolution = sceneWaterObjectShadowResolution\(entry\)/);
  assert.match(webgpu, /qualityTier: "full",[\s\S]*surfaceResolution: authoredSurfaceResolution,[\s\S]*causticsResolution: causticsResolution,[\s\S]*objectTexturePixelBudget: objectTextureSize\.pixelBudget,[\s\S]*objectShadowResolution: objectShadowResolution/);
  assert.match(core, /causticsResolution: Math\.max\(0, Math\.floor\(sceneNumber\(item\.causticsResolution/);
  assert.match(core, /objectTextureResolution: Math\.max\(0, Math\.floor\(sceneNumber\(item\.objectTextureResolution/);
  assert.match(core, /objectTextureResolutionMode: typeof item\.objectTextureResolutionMode === "string"/);
  assert.match(core, /objectTexturePixelBudget: Math\.max\(0, Math\.floor\(sceneNumber\(item\.objectTexturePixelBudget/);
  assert.match(core, /objectShadowResolution: Math\.max\(0, Math\.floor\(sceneNumber\(item\.objectShadowResolution/);
  assert.match(webgpu, /function sceneWaterActiveObjectID/);
  assert.match(webgpu, /return "float-sphere"/);
  assert.match(webgpu, /return "float-cube"/);
  assert.match(webgpu, /return "float-torus"/);
  assert.match(webgpu, /return "float-duck"/);
  assert.match(core, /material: materialName/);
  assert.match(core, /castShadow: hasCastShadow \? sceneBool\(current\.castShadow, false\) : undefined/);
  assert.match(core, /receiveShadow: hasReceiveShadow \? sceneBool\(current\.receiveShadow, false\) : undefined/);
  assert.match(mount, /const keys = \["material", "materialKind"[\s\S]*"customVertexWGSL"[\s\S]*"shaderLayout"[\s\S]*"shaderSourceFiles"\]/);
  assert.match(mount, /function sceneApplyModelMaterialName/);
  assert.match(mount, /function sceneApplyModelRenderFlags/);
  assert.match(mount, /instanced\.castShadow = model\.castShadow/);
  assert.match(mount, /const namedMaterialOverride = typeof override\.material === "string" && override\.material\.trim\(\)/);
  assert.match(mount, /if \(material && !namedMaterialOverride\)/);
  assert.match(webgpu, /function sceneWaterObjectMeshKindMatches/);
  assert.match(webgpu, /if \(!obj \|\| !obj\.castShadow\) return false/);
  assert.match(webgpu, /waterKind === 1\) return kind\.indexOf\("sphere"\) >= 0/);
  assert.match(webgpu, /waterKind === 2\) return kind\.indexOf\("box"\) >= 0 \|\| kind\.indexOf\("cube"\) >= 0/);
  assert.match(webgpu, /function sceneWaterObjectMeshCandidateProfile/);
  assert.match(webgpu, /function sceneWaterObjectMeshList/);
  assert.match(webgpu, /if \(targetID\) \{/);
  assert.doesNotMatch(webgpu, /var caster = objects\[k\]/);
  assert.doesNotMatch(webgpu, /!caster\.castShadow/);
  assert.match(webgpu, /function drawWaterObjectMeshObjects/);
  assert.match(webgpu, /function bindWaterObjectMeshVertexBuffers/);
  assert.match(webgpu, /function bindWaterObjectSelenaAttributes/);
  assert.match(webgpu, /getSelenaPipeline\(mat, blendMode, depthWrite, \{\s*targetFormat: WATER_OBJECT_TEXTURE_FORMAT/);
  assert.match(webgpu, /function sceneWaterObjectTextureSelenaUniforms/);
  assert.match(webgpu, /system\.waterPoolWidth = poolWidth/);
  assert.match(webgpu, /system\.waterLightDir = \{ x: light\.x \/ lightLen, y: light\.y \/ lightLen, z: light\.z \/ lightLen \}/);
  assert.match(webgpu, /system\.waterObjectRadius = active \? Math\.max\(0\.0001, radius\) : 0/);
  assert.match(webgpu, /sceneNumber\(system && system\.waterPoolWidth, sceneNumber\(entry && entry\.poolWidth, 1\.0\)\)/);
  assert.match(webgpu, /sceneNumber\(system && system\.waterObjectRadius, sceneNumber\(entry && entry\.objectRadius/);
  assert.match(webgpu, /sceneWaterLightVector\(entry, \{ x: 0\.3, y: 0\.9, z: 0\.45 \}\)/);
  assert.match(webgpu, /poolSize: \[poolWidth, poolHeight, poolLength, cornerRadius\]/);
  assert.match(webgpu, /params: \[resolution, radius, kind, subtype\]/);
  assert.match(webgpu, /function sceneWaterObjectTextureSelenaContext/);
  assert.match(webgpu, /uniformSlotSuffix: \["water-object-texture", waterID, target, mode\]\.join\("-"\)/);
  assert.match(webgpu, /isTexturePass: \[1, 0, 0, 0\]/);
  assert.match(webgpu, /texturePassMode: \[mode, 0, 0, 0\]/);
  assert.match(webgpu, /waterObjectTexturePassMode: \[mode, 0, 0, 0\]/);
  assert.match(webgpu, /uniforms: sceneWaterObjectTextureSelenaUniforms\(system, mode\)/);
  assert.match(webgpu, /"gosx-water-object-mesh-refraction-pass",\s*"refraction"/);
  assert.match(webgpu, /"gosx-water-object-mesh-reflection-pass",\s*"reflection"/);
  assert.match(webgpu, /"gosx-water-object-mesh-clipped-reflection-pass",\s*"clipped-reflection"/);
  assert.match(webgpu, /record\.system\.id = id/);
  assert.match(webgpu, /createSelenaBindGroup\(mat, selenaResource, obj, renderContext\)/);
  assert.doesNotMatch(webgpu, /_gosxWaterObjectTexturePassMode/);
  assert.match(webgpu, /function drawWaterObjectProjectedShadowObjects/);
  assert.match(webgpu, /function renderWaterObjectMeshTargetPass/);
  assert.match(webgpu, /function renderWaterObjectMeshShadowPass/);
  assert.match(webgpu, /function sceneWaterObjectMeshShadowUniformData/);
  assert.match(webgpu, /createWaterObjectMeshShadowBindGroup/);
  assert.match(webgpu, /function sceneWaterNormalizeReflectionDirection/);
  assert.match(webgpu, /function sceneWaterReflectionCameraForward/);
  assert.match(webgpu, /var x = 0;[\s\S]*var y = 0;[\s\S]*var z = 1;/);
  assert.match(webgpu, /return sceneWaterNormalizeReflectionDirection\(\{ x: nextX, y: nextY, z: z \}\)/);
  assert.match(webgpu, /function sceneWaterCameraWorldPosition/);
  assert.match(webgpu, /function sceneWaterCameraWorldDirection/);
  assert.match(webgpu, /return sceneWaterNormalizeReflectionDirection\(\{ x: -forward\.x, y: -forward\.y, z: -forward\.z \}\)/);
  assert.match(webgpu, /function sceneWaterMirrorWaterPoint/);
  assert.match(webgpu, /function sceneWaterReflectionCamera/);
  assert.match(webgpu, /var forward = sceneWaterReflectionCameraForward\(cam\)/);
  assert.match(webgpu, /y: -forward\.y/);
  assert.match(webgpu, /var horizontal = Math\.sqrt/);
  assert.match(webgpu, /rotationX: -Math\.atan2\(reflectedForward\.y, horizontal\)/);
  assert.match(webgpu, /rotationY: Math\.atan2\(reflectedForward\.x, reflectedForward\.z\)/);
  assert.match(webgpu, /function sceneWaterReflectionCameraUp/);
  assert.match(webgpu, /return \{ x: up\.x, y: -up\.y, z: up\.z \}/);
  assert.match(webgpu, /function sceneWaterLookAtViewMatrix/);
  assert.match(webgpu, /var position = sceneWaterCameraWorldPosition\(cam\)/);
  assert.match(webgpu, /var direction = sceneWaterCameraWorldDirection\(cam\)/);
  assert.match(webgpu, /x: position\.x \+ direction\.x/);
  assert.match(webgpu, /var eye = sceneWaterMirrorWaterPoint\(position\)/);
  assert.match(webgpu, /var reflectedTarget = sceneWaterMirrorWaterPoint\(target\)/);
  assert.match(webgpu, /var reflectedUp = sceneWaterReflectionCameraUp\(camera\)/);
  assert.match(webgpu, /sceneWaterLookAtViewMatrix\(eye, reflectedTarget, reflectedUp, scratchViewMatrix\)/);
  assert.match(webgpu, /function uploadWaterReflectionFrameUniforms/);
  assert.match(webgpu, /f\[33\] = eye\.y/);
  assert.match(webgpu, /f\[34\] = eye\.z/);
  assert.match(webgpu, /function renderWaterObjectSceneTexturePasses/);
  assert.match(webgpu, /function waterSystemUsesProjectedObjectTextures\(system\)/);
  assert.match(webgpu, /function waterSystemHasObjectTextureSubject\(system\)/);
  assert.match(webgpu, /return kind === 3;/);
  assert.match(webgpu, /return waterSystemUsesProjectedObjectTextures\(system\);/);
  assert.doesNotMatch(webgpu, /system\.waterObjectActive && \(system\.waterObjectKind \|\| 0\) > 0/);
  assert.doesNotMatch(webgpu, /renderWaterObjectTexturePass\(encoder, system\);\s*continue;/);
  assert.match(webgpu, /createWaterObjectTextureBindGroup/);
  assert.match(webgpu, /function renderWaterObjectTexturePass/);
  assert.match(webgpu, /function renderWaterObjectShadowPass/);
  assert.match(webgpu, /objectReflectionTexture: texture_2d<f32>/);
  assert.match(webgpu, /objectClippedReflectionTexture: texture_2d<f32>/);
  assert.match(webgpu, /objectRefractionTexture: texture_2d<f32>/);
  assert.match(webgpu, /sampleObjectReflection\(in\.worldPos, reflectDir\)/);
  assert.match(webgpu, /sampleObjectRefraction\(in\.worldPos, refractDir\)/);
  assert.match(webgpu, /renderWaterObjectTexturePass\(encoder, system\)/);
  assert.match(webgpu, /renderWaterObjectShadowPass\(encoder, system\)/);
  assert.match(webgpu, /renderWaterObjectMeshShadowPass\(encoder, system, objectList, pbrBuffers\)/);
  assert.match(webgpu, /renderWaterObjectSceneTexturePasses\(/);
  assert.match(webgpu, /uploadWaterReflectionFrameUniforms\(bundle && bundle\.camera, targetWidth, targetHeight, false\)/);
  assert.match(webgpu, /updateWaterSystems\(bundle\.waterSystems, encoder, frameNowMS, frameActive, frameQualityProfile, frameQualityRevision, bundle, pbrSceneBuffers, scaledW, scaledH\)/);
  assert.match(webgpu, /waterObjectTexturePasses/);
  assert.match(webgpu, /waterObjectTextureTargets/);
  assert.match(webgpu, /waterObjectTextureMeshPasses/);
  assert.match(webgpu, /waterObjectTextureMeshDrawCalls/);
  assert.match(webgpu, /waterObjectTextureFallbackPasses/);
  assert.match(webgpu, /waterObjectTextureCandidateObjects/);
  assert.match(webgpu, /waterObjectTextureSelectedObjects/);
  assert.match(webgpu, /waterObjectTextureFallbackMissingObjects/);
  assert.match(webgpu, /waterObjectTextureFallbackMissingResources/);
  assert.match(webgpu, /waterObjectTextureCandidateProfile/);
  assert.match(webgpu, /waterObjectShadowPasses/);
  assert.match(webgpu, /waterObjectShadowMeshPasses/);
  assert.match(webgpu, /waterObjectShadowMeshDrawCalls/);
  assert.match(webgpu, /waterAuthoredObjectShadowPasses/);
  assert.match(webgpu, /waterAuthoredObjectMeshShadowPasses/);
  assert.match(webgpu, /waterObjectShadowFallbackPasses/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-targets/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-pixels/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-mesh-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-selected-objects/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-candidate-profile/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-mesh-draw-calls/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-selena-draw-calls/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-texture-fallback-passes/);
  assert.match(geometry, /function scenePrimitiveTriangleMesh/);
  assert.match(geometry, /function sphereTriangleMesh/);
  assert.match(geometry, /function torusTriangleMesh/);
  assert.match(core, /torusknot/);
  assert.match(core, /normalized\.wireframe === false/);
  assert.match(core, /normalized\.vertices = scenePrimitiveTriangleMesh\(normalized\)/);
  assert.match(waterPage, /id="float-sphere"[\s\S]*wireframe=\{false\}/);
  assert.match(waterPage, /id="float-cube"[\s\S]*wireframe=\{false\}/);
  assert.match(waterPage, /id="float-torus"[\s\S]*wireframe=\{false\}/);
  // Cost knobs bind to the diag-resolved data values (waterDiagDefaults in
  // diag.go is the single source of truth for shipped configuration) — see
  // the matching Go-side assertions in demos/water/program_test.go.
  assert.match(waterPage, /resolution=\{data\.diagResolution\}/);
  assert.match(waterPage, /surfaceResolution=\{data\.diagMeshRes\}/);
  assert.match(waterPage, /causticsResolution=\{data\.diagCausticsRes\}/);
  assert.match(waterPage, /objectTextureResolutionMode="viewport"/);
  assert.match(waterPage, /objectTexturePixelBudget=\{data\.diagObjectTexBudget\}/);
  assert.match(waterPage, /adaptiveQuality=\{true\}/);
  assert.match(waterPage, /adaptiveTargetFrameMS=\{16\.7\}/);
  assert.doesNotMatch(waterPage, /objectTextureResolution=\{512\}/);
  assert.match(waterPage, /objectShadowResolution=\{data\.diagShadowRes\}/);
  // The hand-written Elio/Selena *WGSL props (and the two <Material> blocks'
  // generic shaderSource/shaderSourceFiles) have been retired -- Selena is
  // the sole primary WGSL source now.
  assert.doesNotMatch(waterPage, /seedWGSL=\{data\.waterSeedWGSL\}/);
  assert.doesNotMatch(waterPage, /dropWGSL=\{data\.waterDropWGSL\}/);
  assert.doesNotMatch(waterPage, /poolVertexWGSL=\{data\.waterPoolVertexWGSL\}/);
  assert.doesNotMatch(waterPage, /poolFragmentWGSL=\{data\.waterPoolFragmentWGSL\}/);
  assert.doesNotMatch(waterPage, /surfaceVertexWGSL=\{data\.waterSurfaceVertexWGSL\}/);
  assert.doesNotMatch(waterPage, /objectShadowWGSL=\{data\.waterObjectShadowWGSL\}/);
  assert.doesNotMatch(waterPage, /objectMeshShadowVertexWGSL=\{data\.waterObjectMeshShadowVertexWGSL\}/);
  assert.doesNotMatch(waterPage, /objectMeshShadowFragmentWGSL=\{data\.waterObjectMeshShadowFragmentWGSL\}/);
  // Selena-compiled combined-WGSL slots: the sole primary WGSL source for
  // every render pass routed through the generic descriptor-driven Selena
  // WebGPU render path (see
  // sceneWaterSelenaMaterial/getWaterSelenaMeshDraw/getWaterSelenaPostDraw in
  // 16a-scene-webgpu.js).
  assert.match(waterPage, /poolSelenaWGSL=\{data\.waterPoolSelenaWGSL\}/);
  assert.match(waterPage, /surfaceSelenaWGSL=\{data\.waterSurfaceSelenaWGSL\}/);
  assert.match(waterPage, /surfaceBelowSelenaWGSL=\{data\.waterSurfaceBelowSelenaWGSL\}/);
  assert.match(waterPage, /causticsSelenaWGSL=\{data\.waterCausticsSelenaWGSL\}/);
  assert.match(waterPage, /objectShadowSelenaWGSL=\{data\.waterObjectShadowSelenaWGSL\}/);
  assert.match(waterPage, /compoundShadowSelenaWGSL=\{data\.waterCompoundShadowSelenaWGSL\}/);
  assert.match(waterPage, /objectMeshShadowSelenaWGSL=\{data\.waterObjectMeshShadowSelenaWGSL\}/);
  assert.match(waterPage, /cubeMap="\/water\/"/);
  assert.doesNotMatch(waterPage, /shaderSource=\{data\.waterObjectMaterialSource\}/);
  assert.doesNotMatch(waterPage, /shaderSourceFiles=\{data\.waterObjectMaterialSourceFiles\}/);
  assert.doesNotMatch(waterPage, /shaderSource=\{data\.waterDuckMaterialSource\}/);
  assert.doesNotMatch(waterPage, /shaderSourceFiles=\{data\.waterDuckMaterialSourceFiles\}/);
  assert.match(waterPage, /name="water-object-material"[\s\S]*customVertexWGSL=\{data\.waterObjectPassSelenaWGSL\}[\s\S]*shaderLayout=\{data\.waterObjectMaterialSelenaLayout\}[\s\S]*customUniforms=\{data\.waterObjectMaterialSelenaUniforms\}/);
  assert.match(waterPage, /name="water-duck-material"[\s\S]*customVertexWGSL=\{data\.waterDuckPassSelenaWGSL\}[\s\S]*shaderLayout=\{data\.waterDuckMaterialSelenaLayout\}[\s\S]*customUniforms=\{data\.waterDuckMaterialSelenaUniforms\}/);
  // The duck descriptor is introduced by the control mutation only; keeping a
  // hidden <Model> in the initial graph eagerly fetched glTF before selection.
  assert.doesNotMatch(waterPage, /id="float-duck"/);
  assert.match(waterProgram, /\/water\/models\/duck\/Duck\.gltf/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-sky-cube-texture-loaded/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-sky-cube-texture-fallbacks/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-shadow-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-shadow-texture-pixels/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-shadow-mesh-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-shadow-mesh-draw-calls/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-object-shadow-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-authored-object-mesh-shadow-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-object-shadow-fallback-passes/);
});

test("Scene3D planner hashes inline mesh vertex payloads", () => {
  const planner = fs.readFileSync(path.join(__dirname, "bootstrap-src", "15b-scene-planner.ts"), "utf8");

  assert.match(planner, /function scenePlannerHashFloatArray/);
  assert.match(planner, /function scenePlannerHashMeshVertices/);
  assert.match(planner, /scenePlannerHashMeshVertices\(hash, object && object\.vertices\)/);
  assert.match(planner, /scenePlannerHashFloatArray\(hash, vertices\.positions, 0\)/);
  assert.match(planner, /scenePlannerHashFloatArray\(hash, vertices\.normals, 0\)/);
  assert.match(planner, /scenePlannerHashFloatArray\(hash, vertices\.uvs, 0\)/);
});

test("Scene3D static GLB models can receive live motion patches", () => {
  const source = readSceneMountSrc();

  assert.match(source, /function sceneRegisterStaticModelLiveRecord\(state, instanceModel, objectIDs\)/);
  assert.match(source, /staticModel: true/);
  assert.match(source, /_modelLocalVertices/);
  assert.match(source, /function sceneApplyStaticModelObjectTransform\(state, record\)/);
  assert.match(source, /object\.vertices\.positions = sceneModelTransformMeshFloats\(local\.positions/);
  assert.match(
    source,
    /if \(sceneModelHasSkins\(skinInstances\) \|\| sceneModelHasWeightAnimations\(asset\) \|\| sceneModelHasNodeAnimations\(asset\)\) \{/
  );
  assert.match(
    source,
    /await scenePrepareModelSkinPlayback\(stageState, asset, instanceModel, skinInstances, objectIDs, staged\.objects, staged\.points\)/
  );
  assert.match(
    source,
    /else \{\s*sceneRegisterStaticModelLiveRecord\(stageState, instanceModel, objectIDs\);/
  );
});

test("bootstrap bridges clamp01 into the WebGPU Scene3D sub-feature", () => {
  const prefix = fs.readFileSync(path.join(__dirname, "bootstrap-src", "26e-feature-scene3d-webgpu-prefix.ts"), "utf8");
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");
  const math = fs.readFileSync(path.join(__dirname, "bootstrap-src", "11-scene-math.ts"), "utf8");

  assert.match(prefix, /var clamp01 = sceneApi\.clamp01/);
  assert.match(prefix, /var sceneMat4MultiplyInto = sceneApi\.sceneMat4MultiplyInto/);
  assert.match(prefix, /var SCENE_POST_TONE_MAPPING = sceneApi\.SCENE_POST_TONE_MAPPING/);
  assert.match(prefix, /var SCENE_POST_BLOOM = sceneApi\.SCENE_POST_BLOOM/);
  assert.match(prefix, /var SCENE_POST_VIGNETTE = sceneApi\.SCENE_POST_VIGNETTE/);
  assert.match(prefix, /var SCENE_POST_COLOR_GRADE = sceneApi\.SCENE_POST_COLOR_GRADE/);
  assert.match(prefix, /var SCENE_POST_SSAO = sceneApi\.SCENE_POST_SSAO/);
  assert.match(prefix, /var SCENE_POST_DOF = sceneApi\.SCENE_POST_DOF/);
  assert.match(prefix, /var SCENE_POST_FXAA = sceneApi\.SCENE_POST_FXAA/);
  assert.match(prefix, /var generateInstancedGeometry = sceneApi\.generateInstancedGeometry/);
  assert.match(prefix, /var normalizeInstancedGeometryKind = sceneApi\.normalizeInstancedGeometryKind/);
  assert.match(prefix, /var buildSceneWorldDrawPlan = sceneApi\.buildSceneWorldDrawPlan/);
  // extractFrustumPlanesJS lives in 11-scene-math.ts (base bundle) but is called
  // by 16a's instanced GPU cull in the webgpu chunk — must be bridged here.
  assert.match(prefix, /var extractFrustumPlanesJS = sceneApi\.extractFrustumPlanesJS/);
  assert.match(prefix, /var createSceneWorldDrawScratch = sceneApi\.createSceneWorldDrawScratch/);
  assert.match(prefix, /var createSceneThickLineScratch = sceneApi\.createSceneThickLineScratch/);
  assert.match(prefix, /var expandSceneThickLineIntoScratch = sceneApi\.expandSceneThickLineIntoScratch/);
  assert.match(core, /\n    clamp01,\n/);
  assert.match(core, /buildSceneWorldDrawPlan: typeof buildSceneWorldDrawPlan === "function"/);
  assert.match(core, /extractFrustumPlanesJS: typeof extractFrustumPlanesJS === "function"/);
  assert.match(core, /createSceneWorldDrawScratch: typeof createSceneWorldDrawScratch === "function"/);
  assert.match(core, /createSceneThickLineScratch: typeof createSceneThickLineScratch === "function"/);
  assert.match(core, /expandSceneThickLineIntoScratch: typeof expandSceneThickLineIntoScratch === "function"/);
  assert.match(core, /\n    SCENE_POST_TONE_MAPPING: "toneMapping",\n/);
  assert.match(core, /\n    SCENE_POST_BLOOM: "bloom",\n/);
  assert.match(core, /\n    SCENE_POST_VIGNETTE: "vignette",\n/);
  assert.match(core, /\n    SCENE_POST_COLOR_GRADE: "colorGrade",\n/);
  assert.match(core, /\n    SCENE_POST_SSAO: "ssao",\n/);
  assert.match(core, /\n    SCENE_POST_DOF: "dof",\n/);
  assert.match(core, /\n    SCENE_POST_FXAA: "fxaa",\n/);
  assert.match(core, /generateInstancedGeometry: typeof generateInstancedGeometry === "function"/);
  assert.match(core, /normalizeInstancedGeometryKind: typeof normalizeInstancedGeometryKind === "function"/);
  assert.match(math, /sceneMat4MultiplyInto,\n/);
});

test("Scene3D postfx tonemap modes are honored by WebGL and WebGPU", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgl, /uniform int u_toneMapMode/);
  assert.match(webgl, /u_toneMapMode == 2[\s\S]*reinhard\(color\)/);
  assert.match(webgl, /u_toneMapMode == 3[\s\S]*filmic\(color\)/);
  assert.match(webgl, /scenePostToneMapMode\(effect\.mode\)/);

  assert.match(webgpu, /toneMapMode: f32/);
  assert.match(webgpu, /mode == 2[\s\S]*reinhard\(color\)/);
  assert.match(webgpu, /mode == 3[\s\S]*filmic\(color\)/);
  assert.match(webgpu, /sceneWebGPUToneMapMode\(effect\.mode\)/);
});

test("Scene3D WebGPU bloom blur avoids sparse radius tap grids", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /let radiusStep = clamp\(params\.radius \* 0\.35, 1\.0, 4\.0\)/);
  assert.match(webgpu, /offsets\[i\] \* radiusStep/);
  assert.doesNotMatch(webgpu, /offsets\[i\] \* params\.radius/);
});

test("Scene3D WebGPU SSAO uses a depth-backed post pass", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /var WGSL_POST_SSAO_FRAGMENT = \[/);
  assert.match(webgpu, /var WGSL_POST_DOF_FRAGMENT = \[/);
  assert.match(webgpu, /var depthTex: texture_depth_2d/);
  assert.match(webgpu, /textureLoad\(depthTex, p, 0\)/);
  assert.match(webgpu, /texture: \{ sampleType: "depth" \}/);
  assert.match(webgpu, /usage: GPUTextureUsage\.RENDER_ATTACHMENT \| GPUTextureUsage\.TEXTURE_BINDING/);
  assert.match(webgpu, /case SCENE_POST_SSAO:/);
  assert.match(webgpu, /case SCENE_POST_DOF:/);
  assert.match(webgpu, /postSSAOPasses \+= 1/);
  assert.match(webgpu, /postDOFPasses \+= 1/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-post-ssao-passes/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-post-dof-passes/);
});

test("Scene3D FXAA is wired as the chain-end postfx pass in WebGL and WebGPU", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const shared = fs.readFileSync(path.join(__dirname, "bootstrap-src", "16c-scene-shared-pbr.ts"), "utf8");
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // WebGL: GLSL fullscreen pass, dedicated program, wired into the effect switch.
  // The SCENE_POST_* kind constants live in 16c because 10-runtime-scene-core.js
  // publishes them for the WebGPU chunk and the WebGL file is now lazy.
  assert.match(shared, /var SCENE_POST_FXAA = "fxaa";/);
  assert.match(webgl, /const SCENE_POST_FXAA_SOURCE = \[/);
  assert.match(webgl, /float greenLuma\(vec3 c\) \{ return c\.g; \}/);
  assert.match(webgl, /function applyFXAA\(inputTex, effect, targetFBO, w, h\)/);
  assert.match(webgl, /getProgram\("fxaa", SCENE_POST_FXAA_SOURCE\)/);
  assert.match(webgl, /case SCENE_POST_FXAA:/);
  assert.match(webgl, /currentTexture = applyFXAA\(currentTexture, effect, targetFBO, passW, passH\);/);

  // WebGPU: WGSL fullscreen pass reusing the blit (texture+sampler) bind
  // group layout since FXAA has no tunable uniforms. The body is written once
  // against precision aliases and compiled into an f32 and an f16 variant, so
  // the source now names the body and the two variants built from it.
  assert.match(webgpu, /var WGSL_POST_FXAA_BODY = \[/);
  assert.match(webgpu, /var WGSL_POST_FXAA_FRAGMENT = sceneWebGPUPostShaderSource\(WGSL_POST_FXAA_BODY, false\);/);
  assert.match(webgpu, /var WGSL_POST_FXAA_FRAGMENT_F16 = sceneWebGPUPostShaderSource\(WGSL_POST_FXAA_BODY, true\);/);
  assert.match(webgpu, /fn greenLuma\(c: pf3\) -> pf \{/);
  assert.match(webgpu, /case SCENE_POST_FXAA: \{/);
  assert.match(webgpu, /getPipeline\("fxaa", postUsesF16 \? WGSL_POST_FXAA_FRAGMENT_F16 : WGSL_POST_FXAA_FRAGMENT, getPostBlitLayout\(\)\)/);
});

test("Scene3D WebGPU material uniforms cover physical PBR fields", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  for (const field of ["clearcoat", "sheen", "transmission", "iridescence", "anisotropy"]) {
    assert.match(webgpu, new RegExp(`${field}: f32`));
    assert.match(webgpu, new RegExp(`material\\.${field}`));
  }
  assert.match(webgpu, /new ArrayBuffer\(160\)/);
  assert.match(webgpu, /modelMatrix: mat4x4f/);
  assert.match(webgpu, /modelScaleSigns: vec4f/);
  assert.match(webgpu, /f\[20 \+ mi\] = model/);
  assert.match(webgpu, /f\[7\] = clamp01\(sceneNumber\(mat\.clearcoat, 0\)\)/);
  assert.match(webgpu, /f\[8\] = clamp01\(sceneNumber\(mat\.sheen, 0\)\)/);
  assert.match(webgpu, /f\[9\] = clamp01\(sceneNumber\(mat\.transmission, 0\)\)/);
  assert.match(webgpu, /f\[10\] = clamp01\(sceneNumber\(mat\.iridescence, 0\)\)/);
  assert.match(webgpu, /f\[11\] = Math\.max\(-1, Math\.min\(1, sceneNumber\(mat\.anisotropy, 0\)\)\)/);
  assert.match(webgpu, /roughness = clamp\(roughness \* \(1\.0 - abs\(material\.anisotropy\) \* 0\.28\), 0\.04, 1\.0\)/);
  assert.match(webgpu, /let clearcoat = clamp\(material\.clearcoat, 0\.0, 1\.0\)/);
  assert.match(webgpu, /let sheen = clamp\(material\.sheen, 0\.0, 1\.0\)/);
  assert.match(webgpu, /let iridescence = clamp\(material\.iridescence, 0\.0, 1\.0\)/);
  assert.match(webgpu, /let transmission = clamp\(material\.transmission, 0\.0, 1\.0\) \* \(1\.0 - metalness\)/);
});

test("Scene3D WebGPU reports custom material fallback diagnostics", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /function webGPUCustomMaterialStats\(materials\)/);
  assert.match(webgpu, /material\.customVertexWGSL/);
  assert.match(webgpu, /material\.customFragmentWGSL/);
  assert.match(webgpu, /material\.customUniforms/);
  assert.match(webgpu, /customMaterialFallbacks: customMaterialStats\.customMaterialFallbacks/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-custom-material-fallbacks/);
  assert.match(webgpu, /custom-wgsl-hooks-unsupported/);
});

test("Scene3D executes Selena custom shader materials in WebGL and WebGPU", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const webgpu = readWebGPUBackendSrc();

  assert.match(webgl, /function createSceneSelenaProgram\(gl, material, skinned\)/);
  assert.match(webgl, /ensureSelenaProgram\(mat, isSkinned\)/);
  assert.match(webgl, /uploadSelenaUniforms\(gl, selenaProgram, mat, obj\)/);
  assert.match(webgl, /bindSelenaTextures\(gl, selenaProgram, mat\)/);
  assert.match(webgl, /bindSelenaMeshAttribute\(gl, selenaProgram, "position"/);
  assert.match(webgl, /function webGLSelenaObjectModelMatrix\(obj\)/);
  assert.match(webgl, /obj && obj\.directVertices === true[\s\S]{0,120}return obj\.modelMatrix \|\| identityModelMatrix/);
  assert.match(webgl, /if \(name === "modelMatrix"\) return webGLSelenaObjectModelMatrix\(owner\);/);

  assert.match(webgpu, /function getSelenaPipeline\(material, blendMode, depthWrite, options\)/);
  assert.match(webgpu, /entryPoint: "vertexMain"/);
  assert.match(webgpu, /entryPoint: "fragmentMain"/);
  assert.match(webgpu, /function createSelenaBindGroup\(material, resource, cacheOwner, renderContext\)/);
  assert.match(webgpu, /createSelenaBindGroup\(mat, selenaResource, obj\)/);
  assert.match(webgpu, /function sceneSelenaRenderContextUniformValue/);
  assert.match(webgpu, /function sceneSelenaUniformBufferSlot/);
  assert.match(webgpu, /var vectorValue = Array\.isArray\(value\) \|\| \(value && typeof value\.length === "number"\)/);
  assert.match(webgpu, /if \(!vectorValue\) \{[\s\S]{0,180}f32\[base\] = sceneSelenaScalar\(value\)/);
  assert.match(webgpu, /if \(sceneSelenaIsMaterial\(material\)\) \{\n          continue;/);
});

test("Scene3D selena time auto-uniform: both backends declare the clock var and assign it before draws", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const webgpu = readWebGPUBackendSrc();

  // WebGL keeps the per-frame clock in its renderer closure. WebGPU keeps the
  // same value on selenaFrame, the object it hands to the module-scope uniform
  // packer in 16a1-scene-webgpu-selena-uniforms.ts.
  assert.match(webgl, /var sceneSelenaFrameTime = 0;/);
  assert.match(webgpu, /var selenaFrame = \{ viewProjection: scratchSelenaViewProjection, time: 0 \};/);

  // time is a forced reserved auto-uniform: both resolvers return the clock for
  // name === "time" before user values can shadow it.
  assert.match(webgl, /if \(name === "time"\) return sceneSelenaFrameTime;[\s\S]{0,900}hasOwnProperty\.call\(values, name\)/);
  assert.match(webgpu, /if \(name === "time"\) return sceneNumber\(frame && frame\.time, 0\);[\s\S]{0,240}sceneSelenaMaterialValue\(material, name\)/);

  // WebGPU: clock is set from frameTimeSeconds immediately after it is computed,
  // before any render-pass encoder draw commands.
  assert.match(webgpu, /var frameTimeSeconds = frameNowMS \/ 1000;\s*\n\s*selenaFrame\.time = frameTimeSeconds;/);

  // WebGL: clock is set right after scratchSelenaViewProjection is populated
  // (sceneMat4MultiplyInto), before the shadow pass and before drawPBRObjectList.
  assert.match(webgl, /sceneMat4MultiplyInto\(scratchSelenaViewProjection, projMatrix, viewMatrix\);\s*\n\s*sceneSelenaFrameTime = performance\.now\(\) \/ 1000;/);
});

test("Scene3D selena time auto-uniform: time is forced before customUniforms (reserved name)", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const webgpu = readWebGPUBackendSrc();

  // The time branch must appear BEFORE the customUniforms early-return in both
  // resolvers (mirrors mvp/normalMatrix), so a compiled `param time` default
  // shipped in customUniforms cannot shadow the per-frame clock.
  const webglResolver = webgl.match(/function selenaUniformValue[\s\S]{0,2200}/)[0];
  const webgpuResolver = webgpu.match(/function sceneSelenaUniformValue[\s\S]{0,1400}/)[0];

  const webglCustomIdx = webglResolver.indexOf('return values[name]');
  const webglTimeIdx = webglResolver.indexOf('if (name === "time")');
  assert.ok(webglTimeIdx !== -1, 'WebGL resolver must have time branch');
  assert.ok(webglCustomIdx !== -1, 'WebGL resolver must have customUniforms return');
  assert.ok(webglTimeIdx < webglCustomIdx, 'WebGL time must be forced before customUniforms');

  const webgpuCustomIdx = webgpuResolver.indexOf('sceneSelenaMaterialValue(material, name)');
  const webgpuTimeIdx = webgpuResolver.indexOf('if (name === "time")');
  assert.ok(webgpuTimeIdx !== -1, 'WebGPU resolver must have time branch');
  assert.ok(webgpuCustomIdx !== -1, 'WebGPU resolver must read material values after reserved uniforms');
  assert.ok(webgpuTimeIdx < webgpuCustomIdx, 'WebGPU time must be forced before material values');
});

test("Scene3D WebGL2 water renderer wires the compound-object shadow pass", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");

  // The compound-shadow program (compound-shadow.sel GLES) is compiled
  // alongside the sphere/cube object-shadow program, and both are disposed on
  // teardown.
  assert.match(webgl, /var compoundShadowProgram = compile\(entry\.compoundShadowVertexGLES, entry\.compoundShadowFragmentGLES, "Water compound shadow"\)/);
  assert.match(webgl, /if \(compoundShadowProgram\) gl\.deleteProgram\(compoundShadowProgram\)/);
  assert.match(webgl, /var compoundShadowDesc = sceneWaterParseDescriptor\(descriptors\.compoundShadow\)/);

  // The shared shadow RTT is sized/allocated whenever either shadow program is
  // available, since pool only ever samples the one "shadowTexture" result.
  assert.match(webgl, /var shadowTarget = null/);
  assert.match(webgl, /var nextShadowTarget = sceneWaterRenderCreateColorTarget\(gl, nextShadowSize\)/);

  // Proxy-sphere uniform scratch capped to compound-shadow.sel's array<vec4,32>.
  assert.match(webgl, /var COMPOUND_SHADOW_MAX_SPHERES = 32/);
  assert.match(webgl, /function fillCompoundShadowSpheres\(list\)/);

  // Pre-pass B picks compoundShadowProgram over the analytic shadowProgram
  // when the active object is compound (isMeshObject) and it has live proxy
  // spheres; both write into the same shadowTarget RTT.
  assert.match(webgl, /var compoundSphereCount = isMeshObject && compoundShadowProgram\s*\n\s*\? fillCompoundShadowSpheres\(liveEntry\.objectDisplacementSpheres\)\s*\n\s*: 0;/);
  assert.match(webgl, /var useCompoundShadow = compoundSphereCount > 0;/);
  assert.match(webgl, /sceneWaterRenderSetUniforms\(gl, compoundShadowProgram, compoundShadowDesc, \{\s*\n\s*spheres: compoundShadowSpheres, sphereCount: compoundSphereCount,/);
});

test("Scene3D water renderers use one scheduler and bounded balanced-quality work", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  // The mount owns animation. A backend-private rAF doubles WebGL work when
  // the mount also animates a WaterSystem and bypasses pause/offscreen policy.
  const forcedWater = webgl.match(/function createSceneWaterRendererWebGL[\s\S]*?return \{\s*\n\s*kind: "webgl"/);
  assert.ok(forcedWater, "forced WebGL water renderer should exist");
  assert.doesNotMatch(forcedWater[0], /requestAnimationFrame|cancelAnimationFrame/);
  assert.match(forcedWater[0], /function render\(bundle, viewport, frameMeta\)[\s\S]*drawFrame\(frameMeta\)/);
  assert.match(forcedWater[0], /maxCatchUpTicks: 1/);
  assert.doesNotMatch(forcedWater[0], /waterClockOptions\.maxCatchUpTicks\s*=/);
  assert.match(webgpu, /sceneWaterAdvanceClock\(system\.waterClock[\s\S]*?maxCatchUpTicks: 1,[\s\S]*?solverSubsteps: 2/);

  // Balanced/survival modes preserve simulation substeps and cadence retained
  // texture work. WebGPU also avoids stationary displacement.
  assert.match(webgl, /if \(clockFrame\.ticks > 0\) \{\s*drainWaterEvents\(\);\s*sim\.simulate\(clockFrame\.substeps\);\s*sim\.recomputeNormal\(\);\s*\}/);
  assert.match(webgl, /gl\.drawArrays\(gl\.TRIANGLES, 0, surfaceVertexCount\)/,
    "WebGL Selena caustics must project the authored water topology");
  assert.match(webgl, /var causticsCadenceDue = clockFrame\.ticks > 0 &&[\s\S]*Math\.floor\(logicalCausticsTickSeq \/ expensivePassCadence\)/);
  assert.match(webgl, /meshUploadSource === mesh && meshUploadProgram === prog/);
  assert.match(webgpu, /var expensivePassCadence = Math\.max\(1, system\.expensivePassCadence \|\| 1\)/);
  assert.match(webgpu, /system\.waterObjectMoved = objectMoved/);
  assert.match(webgpu, /for \(var waterTick = 0; waterTick < waterClock\.ticks; waterTick\+\+\)[\s\S]*var stepResult = dispatchWaterComputeStage\(encoder, system, entry, "simulation", simulationCompute\.pipeline, waterSimPass\)/);
  assert.match(webgpu, /optics\.caustics && refreshExpensivePasses/);
  assert.match(webgpu, /selenaPass\.draw\(system\.vertexCount\)/,
    "WebGPU Selena caustics must project the authored water topology");
});

test("Scene3D shared water clock is fixed-rate across display cadence and lifecycle", () => {
  const api = loadSceneWaterClockAPI();
  const options = { simulationHz: 60, maxCatchUpTicks: 2, solverSubsteps: 2 };

  function runCadence(displayHz, seconds) {
    const clock = {};
    api.sceneWaterAdvanceClock(clock, 0, true, false, options);
    const frames = displayHz * seconds;
    for (let frame = 1; frame <= frames; frame += 1) {
      api.sceneWaterAdvanceClock(clock, frame * 1000 / displayHz, true, false, options);
    }
    return clock;
  }

  for (const displayHz of [120, 60, 30]) {
    const clock = runCadence(displayHz, 1);
    assert.equal(clock.tickSeq, 60, displayHz + " Hz display must produce 60 simulation ticks/second");
    assert.equal(clock.solverSubstepSeq, 120, displayHz + " Hz display must preserve two solver substeps/tick");
    assert.equal(clock.droppedTicks, 0, displayHz + " Hz display must not drop healthy cadence");
  }

  const jitter = {};
  const jitterTimes = [0, 4, 11, 19, 28, 35, 51, 67, 83, 100];
  for (const now of jitterTimes) api.sceneWaterAdvanceClock(jitter, now, true, false, options);
  assert.equal(jitter.tickSeq, 6, "jitter must accumulate elapsed time instead of tying ticks to display count");
  assert.equal(jitter.solverSubstepSeq, 12);

  const stalled = {};
  api.sceneWaterAdvanceClock(stalled, 0, true, false, options);
  api.sceneWaterAdvanceClock(stalled, 100, true, false, options);
  assert.equal(stalled.ticks, 2, "catch-up must be capped");
  assert.equal(stalled.substeps, 4);
  assert.equal(stalled.dropped, 4, "six elapsed ticks minus two executed ticks must drop four");
  assert.equal(stalled.droppedTicks, 4);
  assert.ok(stalled.accumulatorMS >= 0 && stalled.accumulatorMS < stalled.tickMS);

  api.sceneWaterAdvanceClock(stalled, 90, true, false, options);
  assert.equal(stalled.ticks, 0, "clock rollback must not simulate");
  assert.equal(stalled.reset, true);
  api.sceneWaterAdvanceClock(stalled, 107, true, false, options);
  assert.equal(stalled.ticks, 1, "rollback must re-anchor at the rollback timestamp");

  api.sceneWaterAdvanceClock(stalled, 120, true, true, options);
  assert.equal(stalled.ticks, 0);
  assert.equal(stalled.anchored, false, "pause must unanchor the clock");
  api.sceneWaterAdvanceClock(stalled, 5000, true, false, options);
  assert.equal(stalled.ticks, 0, "first frame after unpause must never catch up hidden time");
  assert.equal(stalled.reset, true);
  api.sceneWaterAdvanceClock(stalled, 5017, true, false, options);
  assert.equal(stalled.ticks, 1);
  api.sceneWaterAdvanceClock(stalled, 9000, false, false, options);
  api.sceneWaterAdvanceClock(stalled, 12000, true, false, options);
  assert.equal(stalled.ticks, 0, "first frame after offscreen resume must never catch up");
});

test("Scene3D fixed-clock backend contracts skip zero-tick work and retain event IDs while paused", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const mount = readSceneMountSrc();

  assert.match(webgl, /var pendingDropEvents = new Map\(\)/);
  assert.match(webgl, /var pendingObjectDisplacementEvents = new Map\(\)/);
  assert.match(webgl, /queueWaterEvents\(liveEntry\);\s*if \(clockFrame\.ticks > 0\) \{\s*drainWaterEvents\(\);/);
  assert.match(webgl, /pendingDropEvents\.set\(dropEventID,[\s\S]*pendingObjectDisplacementEvents\.set\(displacementEventID, queuedEvent\)/);
  assert.match(webgl, /waterSeedEventPending: !seeded,[\s\S]*waterDropEventsPending: pendingDropEvents\.size,[\s\S]*waterObjectDisplacementEventsPending: pendingObjectDisplacementEvents\.size/);
  assert.match(webgl, /if \(clockFrame\.ticks > 0\) \{\s*drainWaterEvents\(\);\s*sim\.simulate\(clockFrame\.substeps\);\s*sim\.recomputeNormal\(\);\s*\}/);
  assert.match(webgl, /var causticsCadenceDue = clockFrame\.ticks > 0 &&[\s\S]*Math\.floor\(logicalCausticsTickSeq \/ expensivePassCadence\)/);
  assert.doesNotMatch(webgl.match(/function createSceneWaterRendererWebGL[\s\S]*?return \{\s*\n\s*kind: "webgl"/)[0], /requestAnimationFrame|cancelAnimationFrame/);

  assert.match(webgpu, /var hasSimulationTick = canConsumeWaterState && waterClock\.ticks > 0/);
  assert.match(webgpu, /if \(hasSimulationTick && !system\.seeded\)/);
  assert.match(webgpu, /if \(hasSimulationTick && dropEventID > 0 && system\.lastDropEventID !== dropEventID\)/);
  assert.match(webgpu, /hasSimulationTick\s*\? dispatchWaterObjectDisplacementEvents/);
  // P3 fusion (water-parity-campaign): the tick's simulation substeps and the
  // trailing normal reconstruction are batched into ONE compute pass
  // (waterSimPass) instead of 3 separate beginComputePass/end() sequences,
  // still gated by runWaterSim so fused work skips identically at rest.
  assert.match(webgpu, /var waterSimPass = encoder\.beginComputePass\(\{ label: "gosx-water-sim-normal-pass" \}\);/);
  assert.match(webgpu, /for \(var waterTick = 0; waterTick < waterClock\.ticks; waterTick\+\+\)[\s\S]*solverStep < 2/);
  assert.match(webgpu, /dispatchWaterComputeStage\(encoder, system, entry, "simulation", simulationCompute\.pipeline, waterSimPass\)/);
  assert.match(webgpu, /var normalResult = dispatchWaterComputeStage\(encoder, system, entry, "normal", normalCompute\.pipeline, waterSimPass\);\s*\n\s*waterSimPass\.end\(\);/);
  // The substep loop, the rest-energy decay, AND the normal dispatch/pass-end
  // all live inside `if (runWaterSim) {` -- i.e. one gate, one pass, no
  // separate `if (hasSimulationTick && runWaterSim)` gate for normal anymore.
  assert.match(webgpu, /if \(runWaterSim\) \{[\s\S]*var waterSimPass = encoder\.beginComputePass[\s\S]*waterSimPass\.end\(\);[\s\S]{0,800}\} else \{\s*\n\s*stats\.waterRestSubstepsSkipped/);
  assert.doesNotMatch(webgpu, /if \(hasSimulationTick && runWaterSim\) \{\s*var normalResult/);
  assert.match(webgpu, /Math\.floor\(Math\.max\(0, waterClock\.tickSeq \|\| 0\) \/ expensivePassCadence\)/);

  assert.equal((mount.match(/renderer\.render\([^;]*createSceneRenderFrameMeta\(/g) || []).length, 3,
    "every mount render seam must pass frame metadata");
  assert.match(mount, /renderer\.setLifecycle\(\{\s*nowMS:[\s\S]*active:[\s\S]*paused:[\s\S]*disposed:[\s\S]*reason:/);
  assert.match(mount, /if \(!active \|\| paused \|\| disposing\) lastSceneRenderNowMS = null/);
});

test("Scene3D WebGPU timing initialization failure unlocks CPU-rAF fallback", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const timingInit = webgpu.match(/function ensureGPUTiming\(\) \{[\s\S]*?\n    \}/);
  assert.ok(timingInit, "WebGPU timing initialization seam should exist");
  assert.match(timingInit[0], /catch \(error\) \{[\s\S]*candidateQuerySet\.destroy\(\)[\s\S]*candidateBuffer\.destroy\(\)[\s\S]*gpuTiming = false;\s*gpuTimingFailed = true;/);
  assert.match(webgpu, /return \{ available: active, active: active, pending: pending, failed: gpuTimingFailed, source: "gpu-timestamp" \}/);

  const fallback = createAdaptiveQualityHarness();
  fallback.renderer.pollPerformanceSample = function() { return null; };
  fallback.renderer.getPerformanceTimingStatus = function() {
    return { available: false, active: false, pending: false, failed: true, source: "gpu-timestamp" };
  };
  fallback.sample(99, 34);
  assert.equal(fallback.state.measurement, "cpu-raf");
  assert.equal(fallback.state.validSamples, 1, "failed GPU timing must not leave adaptive quality sample-starved");
});

test("Scene3D WebGPU quality allocation retries with bounded backoff and publishes telemetry", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");
  const applyQuality = webgpu.match(/function applySceneWaterQualityProfile\([\s\S]*?\n    \}\n\n    function retireWaterSystem/);
  assert.ok(applyQuality, "WebGPU quality allocation seam should exist");
  assert.match(applyQuality[0], /system\.qualityAllocationPending && webGPUFrameSeq < system\.qualityAllocationNextFrame/);
  assert.match(applyQuality[0], /system\.qualityAllocationFailures \+= 1;[\s\S]*system\.qualityAllocationConsecutiveFailures \+= 1;/);
  assert.match(applyQuality[0], /system\.qualityAllocationNextFrame = webGPUFrameSeq \+ Math\.min\(60,\s*Math\.pow\(2, Math\.min\(6, system\.qualityAllocationConsecutiveFailures - 1\)\)\)/);
  assert.match(webgpu, /waterQualityAllocationPending: published\.waterQualityAllocationPending \|\| 0,[\s\S]*waterQualityAllocationFailures: published\.waterQualityAllocationFailures \|\| 0,[\s\S]*waterQualityAllocationRetryFrame: published\.waterQualityAllocationRetryFrame \|\| 0/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-quality-allocation-pending/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-quality-allocation-failures/);
  assert.match(webgpu, /data-gosx-scene3d-webgpu-water-quality-allocation-retry-frame/);
});

test("Scene3D WebGL2 water caches uniform locations and bounds retained-pass work", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");

  // Cache hits and misses per WebGLProgram. Weak keys plus explicit disposal
  // keep renderer replacement from retaining deleted programs.
  assert.match(webgl, /var sceneWaterUniformLocations = new WeakMap\(\)/);
  assert.match(webgl, /if \(locations\.has\(name\)\) return locations\.get\(name\)/);
  assert.match(webgl, /sceneWaterUniformLocations\.delete\(program\)/);
  const applyUniforms = webgl.match(/function sceneWaterApplyPassUniforms[\s\S]*?\n  \}/)[0];
  const renderUniforms = webgl.match(/function sceneWaterRenderSetUniforms[\s\S]*?\n  \}/)[0];
  assert.doesNotMatch(applyUniforms, /gl\.getUniformLocation/);
  assert.doesNotMatch(renderUniforms, /gl\.getUniformLocation/);
  assert.match(applyUniforms, /sceneWaterUniformLocation\(gl, program,/);
  assert.match(renderUniforms, /sceneWaterUniformLocation\(gl, program,/);

  // Caustics retain their quality cadence, while the object-shadow RTT is
  // refreshed only when its exact object/light/pool footprint changes.
  assert.match(webgl, /var causticsCadenceDue = clockFrame\.ticks > 0 &&[\s\S]*Math\.floor\(logicalCausticsTickSeq \/ expensivePassCadence\)/);
  assert.match(webgl, /var shadowSignature = waterShadowSignature\(/);
  assert.match(webgl, /var refreshShadowPass = shadowSignature !== lastShadowSignature/);
  assert.match(webgl, /shadowTarget && \(useCompoundShadow \? compoundShadowProgram : shadowProgram\) && refreshShadowPass/);
  assert.match(webgl, /lastShadowSignature = shadowSignature;\s*\n\s*shadowRefreshCount\+\+/);

  // Match WebGPU's retained object-texture strategy: one mesh target update per
  // frame, with the other two textures reused and matrices still updated.
  assert.match(webgl, /var meshTexturePassSlot = meshTexturePassCursor % 3/);
  assert.match(webgl, /if \(meshTexturePassSlot === 0\)[\s\S]{0,900}else if \(meshTexturePassSlot === 1\)[\s\S]{0,900}else \{/);
  assert.match(webgl, /meshTexturePassCursor = \(meshTexturePassCursor \+ 1\) % 3/);
  assert.match(webgl, /objectRefractionMatrix\.set\(mvp\)[\s\S]{0,160}objectReflectionMatrix\.set/);

  // Balanced mode shades a smaller surface grid without changing the 192-cell
  // simulation or its two integration substeps, and exposes the choice in stats.
  assert.match(webgl, /activeSurfaceGrid = selectWaterSurfaceGrid\(adaptiveEnabled/);
  assert.match(webgl, /waterSurfaceGridResolution: gridResolution/);
  assert.match(webgl, /waterSurfaceVertices: surfaceVertexCount/);
});

test("Scene3D WebGL2 water seeds only the authored initial ripples", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const primeRipples = webgl.match(/function primeRipples\(\) \{[\s\S]*?\n    \}/);
  assert.ok(primeRipples, "forced WebGL water renderer should prime authored state");

  assert.match(primeRipples[0], /if \(!sim\.seed\(\)\) return false;/);
  assert.doesNotMatch(primeRipples[0], /sim\.seed\(\{/);
  assert.doesNotMatch(primeRipples[0], /sim\.drop\(/);
});

test("Scene3D WebGL2 water consumes live events and renderer inputs", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  const forcedWater = webgl.match(/function createSceneWaterRendererWebGL[\s\S]*?return \{\s*\n\s*kind: "webgl"/);
  assert.ok(forcedWater, "forced WebGL water renderer should exist");

  assert.match(forcedWater[0], /dropEventID > lastDropEventID/);
  assert.match(forcedWater[0], /pendingDropEvents\.set\(dropEventID, \{[\s\S]{0,500}dropEventStrength/);
  assert.match(forcedWater[0], /pendingObjectDisplacementEvents\.set\(displacementEventID, queuedEvent\)/);
  assert.match(forcedWater[0], /sim\.displace\(pendingObjectDisplacementEvents\.get\(displacementID\)\)/);
  assert.match(forcedWater[0], /var livePoolWidth = sceneWaterNum\(liveEntry\.poolWidth/);
  assert.match(forcedWater[0], /var liveLightDir = \[[\s\S]{0,240}liveEntry\.lightDirectionZ/);
  assert.match(forcedWater[0], /var liveOpticsEnable = \(liveEntry\.reflection \|\| liveEntry\.refraction\) \? 1 : 0/);
  assert.match(forcedWater[0], /sceneWaterRenderHexColor\(liveEntry\.shallowColor/);
  assert.match(forcedWater[0], /liveEntry\.aboveWaterColorB/);
});

test("Scene3D WebGL2 water refreshes analytic meshes by live transform signature", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");
  assert.match(webgl, /function refreshAnalyticMesh\(kind, center, radius, half, livePoolWidth, livePoolLength\)/);
  assert.match(webgl, /signature !== analyticMeshSignature[\s\S]{0,220}deleteAnalyticMesh\(sphereMesh\);[\s\S]{0,120}deleteAnalyticMesh\(boxMesh\);/);
  assert.match(webgl, /objectMesh = refreshAnalyticMesh\(liveKindNum, liveCenter, liveRadius, liveHalf, livePoolWidth, livePoolLength\)/);
});

test("Scene3D WebGL2 water pool pass wires the rounded-corner pool geometry (mirrors WebGPU)", () => {
  const webgl = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgl.ts"), "utf8");

  // Mirrors 16a-scene-webgpu.js's sceneWaterPoolShapeRounded verbatim, keyed
  // off entry.poolShape ("Rounded Box" / "rounded" / "roundbox").
  assert.match(webgl, /function sceneWaterPoolShapeRounded\(entry\) \{[\s\S]{0,200}rounded box.*roundbox/);

  // The clamp bound follows live dimensions instead of the construction entry.
  assert.match(webgl, /var livePoolMaxCornerRadius = Math\.max\(0, Math\.min\(livePoolWidth, livePoolLength\) - 0\.001\);/);

  // The rounded flag / clamped uniform value / draw-call vertex count are
  // recomputed every drawFrame() from the LIVE bundle entry (liveEntry), not
  // captured once from the construction-time `entry` -- this is the actual
  // fix for runtime "Rounded Box" switches never affecting the WebGL2 draw.
  assert.match(webgl, /var livePoolShapeRounded = sceneWaterPoolShapeRounded\(liveEntry\);/);
  assert.match(webgl, /var livePoolCornerRadius = livePoolShapeRounded\s*\n\s*\? Math\.max\(0, Math\.min\(livePoolMaxCornerRadius, sceneWaterNum\(liveEntry\.cornerRadius, 0\)\)\)\s*\n\s*: 0;/);

  // livePoolRounded (the draw-call vertex-count decision) reads the RAW,
  // unclamped liveEntry.cornerRadius > 0.0001 -- same gate as WebGPU's
  // `rounded = sceneWaterPoolShapeRounded(entry) && sceneNumber(entry.cornerRadius, 0) > 0.0001`.
  assert.match(webgl, /var livePoolRounded = livePoolShapeRounded && sceneWaterNum\(liveEntry\.cornerRadius, 0\) > 0\.0001;/);

  // Vertex count: 30 (5 faces * 6 verts) for the box, 44*9=396 for the
  // rounded floor-fan + wall-strip geometry, exactly like WebGPU's
  // roundedPoolVertexCount = 44 * 9.
  assert.match(webgl, /var livePoolVertexCount = livePoolRounded \? 44 \* 9 : 30;/);
  assert.match(webgl, /gl\.drawArrays\(gl\.TRIANGLES, 0, livePoolVertexCount\);/);

  // Both live locals are derived inside drawFrame() from liveEntry (declared
  // right before them), NOT from the construction-time `entry` -- guards
  // against regressing back to a renderer-creation-time snapshot.
  assert.match(webgl, /var liveEntry = \(lastBundle && Array\.isArray\(lastBundle\.waterSystems\) && lastBundle\.waterSystems\[0\]\) \|\| entry;/);
  assert.match(webgl, /var livePoolShapeRounded = sceneWaterPoolShapeRounded\(liveEntry\);/);
  assert.doesNotMatch(webgl, /var (poolShapeRounded|poolCornerRadius|poolRounded|poolVertexCount) = /);

  // cornerRadius/poolShape are fed into the pool pass's uniform values object,
  // where the descriptor-driven sceneWaterRenderSetUniforms applies them by
  // field name only if pool.sel's compiled descriptor declares them.
  assert.match(webgl, /sceneWaterRenderSetUniforms\(gl, poolProgram, poolDesc, \{\s*\n\s*mvp: mvp, normalMatrix: identity3,\s*\n\s*poolWidth: livePoolWidth, poolLength: livePoolLength, poolHeight: livePoolHeight,\s*\n\s*lightDir: liveLightDir,\s*\n[\s\S]{0,400}cornerRadius: livePoolCornerRadius, poolShape: livePoolShapeRounded \? 1 : 0,/);

  // No other WebGL2 pool draw call hardcodes the 30-vertex box count anymore.
  const poolDrawArraysCalls = webgl.match(/gl\.drawArrays\(gl\.TRIANGLES, 0, livePoolVertexCount\)/g) || [];
  assert.equal(poolDrawArraysCalls.length, 1, "exactly one draw call should consume livePoolVertexCount");
  assert.doesNotMatch(webgl, /gl\.drawArrays\(gl\.TRIANGLES, 0, 30\)/);
});

test("Scene3D WebGPU Selena materials can bind live water resources", () => {
  const webgpu = fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", "webgpu.ts"), "utf8");

  assert.match(webgpu, /function sceneSelenaResourceRef\(material, descriptor\)/);
  assert.match(webgpu, /trimmed\.indexOf\("gosx:"\) === 0/);
  assert.match(webgpu, /function sceneSelenaParseResourceRef\(ref\)/);
  assert.match(webgpu, /parts\[0\] !== "water"/);
  assert.match(webgpu, /function sceneSelenaLiveTextureView\(material, texture\)/);
  assert.match(webgpu, /case "caustics":[\s\S]{0,120}return resolved\.system\.causticsView/);
  assert.match(webgpu, /case "refraction":[\s\S]{0,160}return resolved\.system\.objectRefractionView/);
  assert.match(webgpu, /function sceneSelenaLiveBuffer\(material, bufferDescriptor\)/);
  assert.match(webgpu, /case "heightfield":[\s\S]{0,180}resolved\.system\.activeIndex === 0 \? resolved\.system\.bufferA : resolved\.system\.bufferB/);
  assert.match(webgpu, /function sceneSelenaStorageBufferDescriptors\(layout\)/);
  assert.match(webgpu, /buffer: \{ type: "read-only-storage" \}/);
  assert.match(webgpu, /var liveView = sceneSelenaLiveTextureView\(material, tex\)/);
  assert.match(webgpu, /var buffer = sceneSelenaLiveBuffer\(material, bufferDescriptor\)/);
});

test("Scene3D WebGPU Selena materials expose object matrices as auto-uniforms", () => {
  const webgpu = readWebGPUBackendSrc();
  const resolver = webgpu.match(/function sceneSelenaUniformValue[\s\S]{0,900}/)[0];

  // mvp and viewProjectionMatrix read the live matrix off selenaFrame, which
  // the renderer owns and passes in. The packer itself holds no renderer state.
  assert.match(resolver, /if \(name === "mvp" \|\| name === "viewProjectionMatrix"\) \{[\s\S]{0,140}return \(frame && frame\.viewProjection\) \|\| selenaIdentityMatrix4;/);
  assert.match(resolver, /if \(name === "modelMatrix"\) return webGPUSelenaObjectModelMatrix\(owner\);/);
  assert.match(webgpu, /function webGPUSelenaObjectModelMatrix\(obj\)/);
  assert.match(webgpu, /obj && obj\.directVertices === true[\s\S]{0,120}return webGPUObjectModelMatrix\(obj\)/);
  assert.match(webgpu, /function webGPUSelenaObjectModelMatrix\(obj\)[\s\S]{0,220}return selenaIdentityMatrix4;/);
  assert.match(webgpu, /sceneSelenaUniformData\(material, cacheOwner, renderContext, selenaFrame\)/);
});
