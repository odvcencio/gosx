// WebGPU auto exposure (Environment.AutoExposure).
//
// Color contract: every renderer writes linear HDR light into the scene
// target; exposure is a linear multiplier applied to that target before the
// post chain, and tone mapping then applies the authored exposure and the
// display transfer once. Auto exposure only supplies the adapted multiplier.
//
// One compute workgroup (256 threads, 16 samples each) averages log2
// luminance over a 64 x 64 grid of the scene target, derives the exposure that
// maps that average to the key, and moves the stored exposure toward it with
// an exponential rate (speedUp when brightening, speedDown when darkening).
// A fullscreen pass then multiplies the scene target by the stored exposure
// with blending (dst * src), so no CPU readback sits on the frame path. A
// small staging copy is read asynchronously every 30 frames for telemetry.

const WGSL_AUTO_EXPOSURE_MEASURE = `
struct ExposureParams { dt: f32, minExposure: f32, maxExposure: f32, key: f32, speedUp: f32, speedDown: f32, compensation: f32, reset: f32 };
struct ExposureState { exposure: f32, averageLuminance: f32, valid: f32, pad: f32 };
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var<uniform> params: ExposureParams;
@group(0) @binding(2) var<storage, read_write> state: ExposureState;
var<workgroup> sums: array<f32, 256>;
@compute @workgroup_size(256) fn measure(@builtin(local_invocation_index) index: u32) {
    let size = vec2f(textureDimensions(scene));
    var sum = 0.0;
    for (var k = 0u; k < 16u; k = k + 1u) {
        let cell = index * 16u + k;
        let grid = vec2f(f32(cell % 64u), f32(cell / 64u));
        let px = vec2i(min((grid + vec2f(0.5)) / 64.0 * size, size - vec2f(1.0)));
        let c = textureLoad(scene, px, 0).rgb;
        sum = sum + log2(max(dot(c, vec3f(0.2126, 0.7152, 0.0722)), 0.0001));
    }
    sums[index] = sum;
    workgroupBarrier();
    for (var stride = 128u; stride > 0u; stride = stride / 2u) {
        if (index < stride) { sums[index] = sums[index] + sums[index + stride]; }
        workgroupBarrier();
    }
    if (index == 0u) {
        let average = exp2(sums[0] / 4096.0);
        let target = clamp(params.key / max(average, 0.0001) * exp2(params.compensation), params.minExposure, params.maxExposure);
        var current = state.exposure;
        if (params.reset > 0.5 || state.valid < 0.5 || !(current > 0.0)) { current = target; }
        let rate = select(params.speedDown, params.speedUp, target > current);
        state.exposure = current + (target - current) * (1.0 - exp(-max(params.dt, 0.0) * rate));
        state.averageLuminance = average;
        state.valid = 1.0;
    }
}`;

const WGSL_AUTO_EXPOSURE_APPLY = `
struct ExposureState { exposure: f32, averageLuminance: f32, valid: f32, pad: f32 };
@group(0) @binding(0) var<storage, read> state: ExposureState;
@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    return vec4f(p[i], 0.0, 1.0);
}
@fragment fn fragmentMain() -> @location(0) vec4f {
    let e = state.exposure;
    return vec4f(e, e, e, 1.0);
}`;

// wgpuAutoExposureSettings reads the environment record the scene state
// already normalized (same defaults), or null when auto exposure is off.
function wgpuAutoExposureSettings(raw: any) {
  if (!raw || typeof raw !== "object") return null;
  var positive = function(value: any, fallback: number) { var n = Number(value); return isFinite(n) && n > 0 ? n : fallback; };
  var low = positive(raw.minExposure, 0.1), high = positive(raw.maxExposure, 8), comp = Number(raw.compensation);
  return { minExposure: Math.min(low, high), maxExposure: Math.max(low, high), key: positive(raw.key, 0.18),
    speedUp: positive(raw.speedUp, 3), speedDown: positive(raw.speedDown, 1), compensation: isFinite(comp) ? Math.max(-8, Math.min(8, comp)) : 0 };
}

function wgpuCreateAutoExposure(resources: () => any) {
  var measurePipeline: any = null, applyPipeline: any = null, measureLayout: any = null, applyLayout: any = null;
  var stateBuffer: any = null, paramBuffer: any = null, staging: any = null, stagingBusy = false;
  var params = new Float32Array(8), lastAt = -1, frames = 0, last = { exposure: 0, averageLuminance: 0 };
  var previousEye: number[] | null = null, active = false;
  function ensure(r: any) {
    if (measurePipeline) return;
    measureLayout = r.device.createBindGroupLayout({ label: "gosx-exposure-measure", entries: [
      { binding: 0, visibility: GPUShaderStage.COMPUTE, texture: { sampleType: "unfilterable-float" } },
      { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } },
      { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
    ] });
    applyLayout = r.device.createBindGroupLayout({ label: "gosx-exposure-apply", entries: [
      { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
    ] });
    measurePipeline = wgpuCreateValidatedPipeline(r.device, "compute", { label: "gosx-exposure-measure",
      layout: r.device.createPipelineLayout({ bindGroupLayouts: [measureLayout] }),
      compute: { module: r.device.createShaderModule({ label: "exposure-measure", code: WGSL_AUTO_EXPOSURE_MEASURE }), entryPoint: "measure" } });
    var applyModule = r.device.createShaderModule({ label: "exposure-apply", code: WGSL_AUTO_EXPOSURE_APPLY });
    applyPipeline = wgpuCreateValidatedPipeline(r.device, "render", { label: "gosx-exposure-apply",
      layout: r.device.createPipelineLayout({ bindGroupLayouts: [applyLayout] }),
      vertex: { module: applyModule, entryPoint: "vertexMain" },
      fragment: { module: applyModule, entryPoint: "fragmentMain", targets: [{ format: "rgba16float", blend: {
        color: { operation: "add", srcFactor: "zero", dstFactor: "src" }, alpha: { operation: "add", srcFactor: "zero", dstFactor: "one" } } }] },
      primitive: { topology: "triangle-list" } });
    stateBuffer = r.device.createBuffer({ label: "gosx-exposure-state", size: 16, usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_SRC | GPUBufferUsage.COPY_DST });
    paramBuffer = r.device.createBuffer({ label: "gosx-exposure-params", size: 32, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    staging = r.device.createBuffer({ label: "gosx-exposure-readback", size: 16, usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST });
  }
  // A camera jump of more than 2 units is a cut: adapt at once.
  function cut(camera: any): boolean {
    var eye = [sceneNumber(camera && camera.x, 0), sceneNumber(camera && camera.y, 0), sceneNumber(camera && camera.z, 0)];
    var jumped = !previousEye || Math.hypot(eye[0] - previousEye[0], eye[1] - previousEye[1], eye[2] - previousEye[2]) > 2;
    previousEye = eye;
    return jumped;
  }
  return {
    // apply measures and adapts on the GPU, then multiplies the scene target.
    // It returns false (and does nothing) unless the scene asked for it.
    apply: function(encoder: any, environment: any, target: any, camera: any, nowMS: number): boolean {
      var settings = wgpuAutoExposureSettings(environment && environment.autoExposure);
      if (!settings || !target || !target.colorView) { active = false; lastAt = -1; return false; }
      var r = resources();
      ensure(r);
      var dt = lastAt < 0 || nowMS - lastAt > 250 ? 0 : (nowMS - lastAt) / 1000;
      var reset = cut(camera) || !active;
      active = true; lastAt = nowMS;
      params[0] = dt; params[1] = settings.minExposure; params[2] = settings.maxExposure; params[3] = settings.key;
      params[4] = settings.speedUp; params[5] = settings.speedDown; params[6] = settings.compensation; params[7] = reset ? 1 : 0;
      r.device.queue.writeBuffer(paramBuffer, 0, params);
      var compute = encoder.beginComputePass({ label: "gosx-exposure-measure" });
      compute.setPipeline(wgpuRequirePipeline(measurePipeline));
      compute.setBindGroup(0, r.device.createBindGroup({ layout: measureLayout, entries: [
        { binding: 0, resource: target.colorView }, { binding: 1, resource: { buffer: paramBuffer } }, { binding: 2, resource: { buffer: stateBuffer } },
      ] }));
      compute.dispatchWorkgroups(1);
      compute.end();
      var pass = encoder.beginRenderPass({ label: "gosx-exposure-apply", colorAttachments: [{ view: target.colorView, loadOp: "load", storeOp: "store" }] });
      pass.setPipeline(wgpuRequirePipeline(applyPipeline));
      pass.setBindGroup(0, r.device.createBindGroup({ layout: applyLayout, entries: [{ binding: 0, resource: { buffer: stateBuffer } }] }));
      pass.draw(3);
      pass.end();
      frames += 1;
      if (!stagingBusy && frames % 30 === 1 && typeof staging.mapAsync === "function") {
        encoder.copyBufferToBuffer(stateBuffer, 0, staging, 0, 16);
        stagingBusy = true;
        // Map only after this frame's submit: the copy must not race a pending map.
        (r.device.queue.onSubmittedWorkDone ? r.device.queue.onSubmittedWorkDone() : Promise.resolve()).then(function() {
          return staging.mapAsync(typeof GPUMapMode !== "undefined" ? GPUMapMode.READ : 1).then(function() {
            var values = new Float32Array(staging.getMappedRange().slice(0));
            staging.unmap();
            last = { exposure: values[0], averageLuminance: values[1] };
            stagingBusy = false;
          });
        }).catch(function() { stagingBusy = false; });
      }
      return true;
    },
    stats: function() { return { autoExposure: active ? Math.round(last.exposure * 1000) / 1000 : 0, autoExposureLuminance: active ? last.averageLuminance : 0 }; },
  };
}
