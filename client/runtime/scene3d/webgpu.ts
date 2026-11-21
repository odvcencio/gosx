  // wgpuSameItems reports whether two arrays hold the same items, compared by identity.
  function wgpuSameItems(a: any[], b: any[]) {
    if (!a || a.length !== b.length) return false;
    for (var i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
    return true;
  }

  // wgpuPackElioSkinVertices packs the Elio skin kernel's input: per vertex, position, normalized weights, clamped joint
  // indices, normal and tangent, 72 bytes. src is [positions, joints, weights, normals, tangents]; padding vertices get weight 1.
  function wgpuPackElioSkinVertices(src: any[], count: number, paddedCount: number, jointCount: number) {
    var positions = src[0], joints = src[1], weights = src[2], normals = src[3], tangents = src[4];
    var stride = 72;
    var bytes = new Uint8Array(paddedCount * stride);
    var view = new DataView(bytes.buffer);
    var maxJoint = Math.max(0, jointCount - 1);
    for (var i = 0; i < paddedCount; i++) {
      var off = i * stride;
      if (i < count) {
        var p = i * 3;
        var q = i * 4;
        view.setFloat32(off, sceneNumber(positions[p], 0), true);
        view.setFloat32(off + 4, sceneNumber(positions[p + 1], 0), true);
        view.setFloat32(off + 8, sceneNumber(positions[p + 2], 0), true);
        var w0 = Math.max(0, sceneNumber(weights[q], 0));
        var w1 = Math.max(0, sceneNumber(weights[q + 1], 0));
        var w2 = Math.max(0, sceneNumber(weights[q + 2], 0));
        var w3 = Math.max(0, sceneNumber(weights[q + 3], 0));
        var sum = w0 + w1 + w2 + w3;
        if (sum <= 0.000001) {
          w0 = 1; w1 = 0; w2 = 0; w3 = 0;
        } else {
          w0 /= sum; w1 /= sum; w2 /= sum; w3 /= sum;
        }
        view.setFloat32(off + 12, w0, true);
        view.setFloat32(off + 16, w1, true);
        view.setFloat32(off + 20, w2, true);
        view.setFloat32(off + 24, w3, true);
        view.setUint32(off + 28, Math.min(maxJoint, Math.max(0, Math.floor(sceneNumber(joints[q], 0)))), true);
        view.setUint32(off + 32, Math.min(maxJoint, Math.max(0, Math.floor(sceneNumber(joints[q + 1], 0)))), true);
        view.setUint32(off + 36, Math.min(maxJoint, Math.max(0, Math.floor(sceneNumber(joints[q + 2], 0)))), true);
        view.setUint32(off + 40, Math.min(maxJoint, Math.max(0, Math.floor(sceneNumber(joints[q + 3], 0)))), true);
        view.setFloat32(off + 44, sceneNumber(normals[p], 0), true);
        view.setFloat32(off + 48, sceneNumber(normals[p + 1], 0), true);
        view.setFloat32(off + 52, sceneNumber(normals[p + 2], 0), true);
        view.setFloat32(off + 56, sceneNumber(tangents[q], 1), true);
        view.setFloat32(off + 60, sceneNumber(tangents[q + 1], 0), true);
        view.setFloat32(off + 64, sceneNumber(tangents[q + 2], 0), true);
        view.setFloat32(off + 68, sceneNumber(tangents[q + 3], 1), true);
      } else {
        view.setFloat32(off + 12, 1, true);
      }
    }
    return bytes;
  }

  function wgpuEmitGPUTiming(sample: any) {
    if (typeof window !== "undefined" && window.__gosx_scene3d_perf === true) {
      document.dispatchEvent(new CustomEvent("gosx:scene3d:perf", { detail: sample }));
    }
  }

  function wgpuCreatePipelineGuard(canvas: any): any {
    var guard: any = { frameAbort: null, pending: 0, created: 0, disposed: false, frameEncoder: null, framePass: null, frameCleanup: null, coreError: "", failures: [], disabled: new Set(), fail: fail, uncaptured: uncaptured, wrapFrame: wrapFrame, release: release, snapshot: snapshot, changed: changed };
    var notificationPending = false;
    function changed() {
      if (guard.disposed || notificationPending) return;
      notificationPending = true;
      Promise.resolve().then(function() {
        notificationPending = false;
        if (!guard.disposed && !guard.pending && typeof canvas.dispatchEvent === "function") {
          canvas.dispatchEvent(new CustomEvent("gosx:scene3d:resource-ready"));
        }
      });
    }
    function fail(label: string, message: string) {
      wgpuRecoverPipeline(guard, canvas, renderTruth(), label, message);
    }
    function uncaptured(event: any) {
      if (event && typeof event.preventDefault === "function") event.preventDefault();
      var error = event && event.error;
      fail("", String(error && error.message || error || "unknown WebGPU error"));
    }
    function wrapFrame(render: any, endFrame: any) {
      return function(bundle: any, viewport: any, frameMeta: any) {
        try { return render(bundle, viewport, frameMeta); }
        catch (error) {
          wgpuSubmitPendingFrame(guard, error);
          if (!error || !error.pipelineValidation) fail("frame", String(error && error.message || error));
          return error && error.pipelineValidation ? "pending" : "failed";
        } finally { guard.frameEncoder = guard.framePass = guard.frameCleanup = guard.frameAbort = null; endFrame(); }
      };
    }
    function release(raw: any) {
      guard.disposed = true;
      if (typeof raw.removeEventListener === "function") raw.removeEventListener("uncapturederror", uncaptured);
      else if (raw.onuncapturederror === uncaptured) raw.onuncapturederror = null;
    }
    function snapshot(out: any) {
      Object.assign(out, { pipelineCoreError: guard.coreError, pipelineFailures: guard.failures.slice(), disabledPipelinePasses: Array.from(guard.disabled), pipelinePending: guard.pending });
      out.postProcessing = out.postProcessing && !guard.disabled.has("post");
    }
    return guard;
  }

  // A pipeline object can exist even when validation rejected it. Pop each
  // scope before returning, while the device stack still belongs to this call.
  // Only the result promise is asynchronous; scopes never settle out of order.
  var wgpuPipelineValidation = new WeakMap();

  function wgpuRequirePipeline(pipeline: any): any {
    var state = pipeline && wgpuPipelineValidation.get(pipeline);
    if (state && !state.ready) throw state;
    return state && state.pipeline || pipeline;
  }

  function wgpuCreateValidatedPipeline(device: any, kind: string, descriptor: any): any {
    var guard = device.__gosxPipelineGuard;
    var state: any = { ready: false, pipelineValidation: true };
    var pipeline: any;
    var scoped = false;
    var error: any = null;
    if (guard) { guard.pending++; guard.created++; }
    function finish(failure: any) {
      state.ready = !failure;
      if (guard) {
        guard.pending--;
        if (failure && !guard.disposed) guard.fail(descriptor.label || kind, failure.message || String(failure));
        if (!guard.pending) guard.changed();
      }
    }
    var createAsync = kind === "render" ? device.createRenderPipelineAsync : device.createComputePipelineAsync;
    if (typeof createAsync === "function") {
      // Async creation reports validation on this descriptor's promise, without
      // sharing a device error scope with parallel builds or frame encoding.
      pipeline = {};
      wgpuPipelineValidation.set(pipeline, state);
      try {
        createAsync.call(device, descriptor).then(function(result: any) {
          state.pipeline = result;
          finish(null);
        }, finish);
      } catch (failure) { finish(failure); }
      return pipeline;
    }
    try {
      device.pushErrorScope("validation");
      scoped = true;
      pipeline = kind === "render" ? device.createRenderPipeline(descriptor) : device.createComputePipeline(descriptor);
    } catch (failure) {
      error = failure;
      pipeline = {};
    }
    wgpuPipelineValidation.set(pipeline, state);
    // popErrorScope removes the scope synchronously, even though its result
    // arrives later. Nested frame scopes therefore keep their own errors.
    if (scoped) {
      try {
        device.popErrorScope().then(function(failure) { finish(error || failure); }, finish);
      } catch (failure) { finish(error || failure); }
    } else finish(error);
    return pipeline;
  }

  // Pipeline validation can pause a frame after compute commands have advanced
  // CPU-side simulation state. Submit those valid commands before retrying so
  // seed flags, consumed events and ping-pong indices still describe the GPU.
  function wgpuSubmitPendingFrame(guard: any, error: any) {
    if (!error || !error.pipelineValidation || !guard.frameEncoder) return;
    try {
      if (guard.framePass) guard.framePass.end();
      if (guard.frameAbort) guard.frameAbort();
      if (guard.frameCleanup) guard.frameCleanup();
      guard.submit([guard.frameEncoder.finish()]);
    } catch (failure) { guard.fail("frame", String(failure && failure.message || failure)); }
  }

  function wgpuFinishGPUDrivenEncoding(host: any, encoder: any) {
    if (host) host.finishEncoding(encoder);
  }

  function wgpuDeferredCanvasView(guard: any, context: any): any {
    guard.canvasContext = context;
    if (guard.framePipelinesReady) return context.getCurrentTexture().createView();
    return guard.canvasView || (guard.canvasView = {});
  }

  function wgpuReplayFrameCommands(encoder: any, guard: any, commands: any[], view: any) {
    for (var i = 0; i < commands.length; i += 4) {
      var record = commands[i], method = commands[i + 1], args = commands[i + 2], group = commands[i + 3];
      if (method === "beginRenderPass" || method === "beginComputePass") {
        var descriptor = args[0];
        if (record.canvas) descriptor = Object.assign({}, descriptor, { colorAttachments: descriptor.colorAttachments.map(function(attachment: any) {
          if (!attachment) return attachment;
          var resolved = Object.assign({}, attachment);
          if (resolved.view === guard.canvasView) resolved.view = view;
          if (resolved.resolveTarget === guard.canvasView) resolved.resolveTarget = view;
          return resolved;
        }) });
        record.pass = encoder[method](descriptor);
      } else {
        if (group) {
          if (group.view !== view) { group.group = group.create(view); group.view = view; }
          args[1] = group.group;
        }
        var target = record ? record.pass : encoder;
        target[method].apply(target, args);
      }
    }
  }

  var renderMethods = new Set(["setPipeline", "setBindGroup", "setVertexBuffer", "setIndexBuffer", "setViewport", "setScissorRect", "setBlendConstant", "setStencilReference", "draw", "drawIndexed", "drawIndirect", "drawIndexedIndirect", "executeBundles", "beginOcclusionQuery", "endOcclusionQuery", "pushDebugGroup", "popDebugGroup", "insertDebugMarker", "end"]);
  var computeMethods = new Set(["setPipeline", "setBindGroup", "dispatchWorkgroups", "dispatchWorkgroupsIndirect", "pushDebugGroup", "popDebugGroup", "insertDebugMarker", "end"]);

  function wgpuFramePassProxy(record: any, prototype: any, guard: any, commands: any[], state: any, kind: string) {
    var wrappers: any = Object.create(null), active: any;
    active = new Proxy({}, { get: function(_, method) {
      if (wrappers[method]) return wrappers[method];
      var source = record.pass || prototype;
      var member = source ? Reflect.get(source, method, source) : null;
      if (source && typeof member !== "function") return member;
      if (!source && (typeof method !== "string" || !(kind === "render" ? renderMethods : computeMethods).has(method))) return undefined;
      if (!source && method === "executeBundles" && !guard.supportsBundles) return undefined;
      return wrappers[method] = function(...args: any[]) {
        if (method === "end" && guard.framePass === active) guard.framePass = null;
        if (state.recording) {
          var group = method === "setBindGroup" && guard.canvasGroups && guard.canvasGroups.get(args[1]);
          if (method === "setBindGroup" && args[2] && typeof args[2] === "object") args[2] = args[2].slice ? args[2].slice() : Array.from(args[2]);
          commands.push(record, method, args, group || null);
        } else return record.pass[method].apply(record.pass, args);
      };
    } });
    guard.framePass = active;
    return active;
  }

  function wgpuTrackFrameEncoder(encoder: any, guard: any): any {
    guard.frameEncoder = encoder;
    guard.framePass = null;
    guard.frameAbort = null;
    if (guard.framePipelinesReady) return encoder;
    var commands: any[] = [], state = { recording: false }, methods: any = Object.create(null);
    guard.frameAbort = function() { state.recording = false; commands.length = 0; };
    return new Proxy(encoder, { get: function(target, key) {
      var value = Reflect.get(target, key, target);
      if (typeof value !== "function") return value;
      if (methods[key]) return methods[key];
      if (key === "finish") return methods[key] = function(descriptor: any) {
        var view = state.recording ? guard.canvasContext.getCurrentTexture().createView() : null;
        wgpuReplayFrameCommands(target, guard, commands, view);
        return target.finish(descriptor);
      };
      if (key !== "beginRenderPass" && key !== "beginComputePass") return methods[key] = function(...args: any[]) {
        if (state.recording) { commands.push(null, key, args, null); return; }
        return value.apply(target, args);
      };
      return methods[key] = function(descriptor: any) {
        var kind = key === "beginRenderPass" ? "render" : "compute";
        var canvasPass = guard.canvasView && kind === "render" && descriptor.colorAttachments.some(function(attachment: any) {
          return attachment && (attachment.view === guard.canvasView || attachment.resolveTarget === guard.canvasView);
        });
        if (canvasPass) state.recording = true;
        if (state.recording) {
          descriptor = Object.assign({}, descriptor);
          if (descriptor.colorAttachments) descriptor.colorAttachments = descriptor.colorAttachments.map(function(attachment: any) { return attachment && Object.assign({}, attachment); });
          if (descriptor.depthStencilAttachment) descriptor.depthStencilAttachment = Object.assign({}, descriptor.depthStencilAttachment);
        }
        var pass: any = state.recording ? null : value.call(target, descriptor);
        var types: any = globalThis, constructor = types[kind === "render" ? "GPURenderPassEncoder" : "GPUComputePassEncoder"];
        var prototype = constructor && constructor.prototype || guard[kind + "PassPrototype"];
        if (pass && !prototype) {
          prototype = Object.create(null);
          (kind === "render" ? renderMethods : computeMethods).forEach(function(method: string) {
            if (typeof pass[method] === "function") prototype[method] = pass[method];
          });
          guard[kind + "PassPrototype"] = prototype;
        }
        var record: any = { pass: pass, canvas: canvasPass };
        if (state.recording) commands.push(record, key, [descriptor], null);
        return wgpuFramePassProxy(record, prototype, guard, commands, state, kind);
      };
    } });
  }

  function wgpuGuardDevice(raw: any, guard: any): any {
    var methods = Object.create(null);
    guard.submit = raw.queue.submit.bind(raw.queue);
    guard.supportsBundles = typeof raw.createRenderBundleEncoder === "function";
    return new Proxy(raw, {
      get: function(target, key) {
        if (key === "__gosxPipelineGuard") return guard;
        if (key === "createCommandEncoder") return function(descriptor) {
          var encoder = target.createCommandEncoder(descriptor);
          return descriptor && descriptor.label === "gosx-frame" ? wgpuTrackFrameEncoder(encoder, guard) : encoder;
        };
        if (key === "createBindGroup") return function(descriptor: any) {
          if (guard.canvasView && descriptor.entries.some(function(entry: any) { return entry.resource === guard.canvasView; })) {
            var handle = {}, record: any = { view: null, group: null, create: function(view: any) {
              return target.createBindGroup(Object.assign({}, descriptor, { entries: descriptor.entries.map(function(entry: any) {
                return entry.resource === guard.canvasView ? Object.assign({}, entry, { resource: view }) : entry;
              }) }));
            } };
            if (!guard.canvasGroups) guard.canvasGroups = new WeakMap();
            guard.canvasGroups.set(handle, record);
            return handle;
          }
          return target.createBindGroup(descriptor);
        };
        if (key === "createShaderModule") return function(descriptor) {
          target.pushErrorScope("validation");
          try { return target.createShaderModule(descriptor); }
          finally { target.popErrorScope().catch(function() {}); }
        };
        var value = Reflect.get(target, key, target);
        if (typeof value !== "function") return value;
        if (!methods[key] || methods[key].source !== value) methods[key] = { source: value, bound: value.bind(target) };
        return methods[key].bound;
      },
    });
  }

  // @ts-ignore TS7006 -- raw-source renderer tests use JavaScript signatures.
  function sceneLatestGPUCompletion(previous, next) {
    return previous && previous.frameSeq > next.frameSeq ? previous : next;
  }

  // @ts-ignore TS7006 -- raw-source renderer tests use JavaScript signatures.
  function sceneWebGPUCompletionSnapshot(status, sample, disposed, lost) {
    var state = "measured";
    if (!sample) state = "pending";
    else if (performance.now() - sample.atMS > 1000) state = "stale";
    if (!status.available) state = "unavailable";
    if (status.failed || lost) state = "failed";
    if (disposed) state = "disposed";
    return Object.assign({ status: state, source: "gpu-timestamp", scope: "frame",
      gpuMS: null, frameSeq: 0, atMS: 0 }, state === "measured" ? sample : {});
  }

  // webgpu.ts — WebGPU rendering backend for GoSX Scene3D.
  // @ts-check
  //
  // Parallel implementation of the PBR WebGL2 renderer (16-scene-webgl.js)
  // using the WebGPU API. Provides Cook-Torrance BRDF with per-pixel
  // lighting, shadow maps, fog, and post-processing. Points are rendered
  // as instanced camera-facing quads since WebGPU has no gl_PointSize.

  /**
   * @typedef {object} GoSXSceneWebGPURenderer
   * @property {(bundle: object, frame?: number) => void} render
   * @property {() => void} dispose
   */

  // renderTruth resolves the shared render-truth helpers published by
  // 15a-scene-postfx-shared.ts. This chunk (bootstrap-feature-scene3d-webgpu.js)
  // is a SEPARATE <script> whose IIFE does not concatenate 15a, so the only
  // link is the global. Resolved per call rather than cached at chunk-load
  // time because the main scene3d bundle and this chunk race on slow networks.
  // The no-op fallback keeps the renderer working when 15a is absent (an old
  // cached main bundle) instead of throwing mid-frame.
  var WEBGPU_RENDER_TRUTH_NOOP = {
    enabled: function() { return false; },
    chain: function() { return []; },
    mark: function() {},
    publish: function() {},
    record: function() {},
    latch: function() {},
    captureShaderInfo: function() {},
    pipelineFailure: function() {},
    implementation: function() { return "unknown"; },
    PIPELINE_MISSING: "missing",
    PIPELINE_PENDING: "pending",
    PIPELINE_FAILED: "failed",
    PIPELINE_OK: "ok",
  };

  function renderTruth() {
    if (typeof window !== "undefined" && window && window.__gosx_scene3d_render_truth_api) {
      return window.__gosx_scene3d_render_truth_api;
    }
    return WEBGPU_RENDER_TRUTH_NOOP;
  }

  function sceneWebGPUSRGBChannelToLinear(value) {
    var channel = Math.max(0, Math.min(1, sceneNumber(value, 0)));
    return channel <= 0.04045
      ? channel / 12.92
      : Math.pow((channel + 0.055) / 1.055, 2.4);
  }

  // -----------------------------------------------------------------------
  // WGSL Shader Sources
  // -----------------------------------------------------------------------

  // -- Shared constants embedded in multiple shaders --
  //
  // The light loop no longer carries a compile-time cap. lights is a
  // runtime-sized storage array, so the fragment shader bounds itself with
  // arrayLength(&lights). See sceneWebGPULightCapacityFor for the JS side.
  var WGSL_COMMON_CONSTANTS = [
    "const PI: f32 = 3.14159265359;",
  ].join("\n");
  var SCENE_WEBGPU_IBL_BRDF_MODEL = "ggx-split-sum/smith-schlick-k=alpha-over-2/schlick-fresnel";

  // -- Frame-level uniform structures --
  //
  // Light packs all seven GoSX light kinds. The type codes 0..4 match the
  // WebGL2 renderer (0=ambient, 1=directional, 2=point, 3=spot,
  // 4=hemisphere), so both backends read the same numbers. Code 5 is
  // rect-area, which only this renderer shades as a rectangle. A LightProbe
  // arrives as code 0, because a probe is a first-order ambient term with no
  // position and no distance falloff.
  //
  // Keep this layout in step with sceneWebGPUPackLights and with
  // SCENE_WEBGPU_LIGHT_FLOATS. The struct is 7 * vec4f = 112 bytes.
  var WGSL_FRAME_STRUCTS = [
    "struct Light {",
    "    position: vec4f,",       // xyz = position, w = type code
    "    direction: vec4f,",      // xyz = direction, w = intensity
    "    color: vec4f,",          // rgb = color (sky color for hemisphere), a = range
    "    params: vec4f,",         // x = decay, y = shadowBias, z = castShadow, w = spot cone angle
    "    groundPenumbra: vec4f,", // rgb = hemisphere ground color, a = spot penumbra
    "    areaHalfWidth: vec4f,",  // xyz = rect-area half-width vector, w = unused
    "    areaHalfHeight: vec4f,", // xyz = rect-area half-height vector, w = unused
    "};",
    "",
    "struct FrameUniforms {",
    "    viewMatrix: mat4x4f,",
    "    projMatrix: mat4x4f,",
    "    cameraPos: vec3f,",
    "    lightCount: u32,",
    "    viewportWidth: f32,",
    "    viewportHeight: f32,",
    "    toneMap: u32,",
    "    _pad0: u32,",
    "};",
    "",
    "struct FogUniforms {",
    "    fogColor: vec3f,",
    "    fogDensity: f32,",
    "    hasFog: u32,",
    "    _pad0: u32,",
    "    _pad1: u32,",
    "    _pad2: u32,",
    "};",
    "",
    "struct EnvUniforms {",
    "    ambientColor: vec3f,",
    "    ambientIntensity: f32,",
    "    skyColor: vec3f,",
    "    skyIntensity: f32,",
    "    groundColor: vec3f,",
    "    groundIntensity: f32,",
    "    envIntensity: f32,",
    "    envRotation: f32,",
    "    hasIBL: u32,",
    "    radianceMipLevels: u32,",
    "    hasEnvMap: u32,",
    "    envMapMaxLod: f32,",
    "    lightModel: u32,",
    "    _pad3: u32,",
    "};",
    "",
    WGSL_SHADOW_UNIFORMS_STRUCT,
  ].join("\n");

  // -- PBR material uniform structure --
  var WGSL_MATERIAL_STRUCT = [
    "struct MaterialUniforms {",
    "    albedo: vec3f,",
    "    roughness: f32,",
    "    metalness: f32,",
    "    emissive: f32,",
    "    opacity: f32,",
    "    clearcoat: f32,",
    "    sheen: f32,",
    "    transmission: f32,",
    "    iridescence: f32,",
    "    anisotropy: f32,",
    "    unlit: u32,",
    "    hasAlbedoMap: u32,",
    "    hasNormalMap: u32,",
    "    hasRoughnessMap: u32,",
    "    hasMetalnessMap: u32,",
    "    hasEmissiveMap: u32,",
    "    receiveShadow: u32,",
    "    hasOcclusionMap: u32,",
    "    modelMatrix: mat4x4f,",
    "    modelScaleSigns: vec4f,",
    // Trailing dedicated material scalars: the authored dielectric F0 and the
    // effective specular factors (min(IOR F0 * colour, 1) * intensity as an
    // aligned vec3f, then the intensity as F90). Every texture flag slot and
    // the model transform/sign floats keep their existing offsets; the vec3f
    // trailing fields pad the struct to 208 bytes total.
    "    dielectricF0: f32,",
    // Reuses the vec3f alignment word at u32 index 41; materialUniformData
    // pre-zeroes it so a plain pack stays
    // neutral until createMaterialBindGroup sets the real flag.
    "    hasSpecularIntensityMap: u32,",
    // f32 alpha cutoff at u32 index 42 (reusing the old padding word);
    // index 41 carries the specular flag and index 43 carries the
    // normal-map V scale, keeping the struct layout unchanged.
    "    alphaCutoff: f32, normalUVScaleV: f32,",
    "    specularF0: vec3f,",
    "    specularF90: f32,",
    // Per-channel log2 of the authored dielectric specular coefficient
    // (log2(iorF0) + log2(color)), with an exact-zero coefficient encoded
    // as the -1e30 sentinel so the shader can branch on it without any
    // epsilon substitution. The vec3f alignment lands at u32 indices
    // 48..50 and the trailing hasSpecularColorMap flag at index 51; this was
    // the end of the struct at 208 bytes before the fields below extended it
    // to 256 bytes.
      "    specularColorLog: vec3f,",
      "    hasSpecularColorMap: u32,",
    // Trailing fields added for glTF material-shading parity with the WebGL2
    // renderer: normalScale/occlusionStrength are the core glTF
    // normalTexture.scale / occlusionTexture.strength factors; emissiveColor
    // is the vec3f emissiveFactor (hasEmissiveColor distinguishes an
    // authored [0,0,0] factor from "no factor set", the latter falling back
    // to the pre-existing albedo-tinted glow for hand-authored materials);
    // rimColor/rimPower/rimStrength are the optional rim-light term, off by
    // default (rimStrength 0). u32 indices 52..55 fill one 16-byte block (no
    // vec3f, so no extra alignment need); emissiveColor's vec3f then starts
    // its own 16-byte-aligned block at index 56, same pattern as
    // specularF0/specularF90 above; rimColor repeats the pattern at index
    // 60, with index 63 left as reserved padding. Struct is 256 bytes total.
    "    normalScale: f32,",
    "    occlusionStrength: f32,",
    "    hasEmissiveColor: u32,",
    "    rimPower: f32,",
    "    emissiveColor: vec3f,",
    "    rimStrength: f32,",
    "    rimColor: vec3f,",
    "    specularAAVariance: f32,",
    "    volume: vec4f,",
    "    attenuationColor: vec3f,",
    "    hasThicknessMap: u32,",
    "};",
  ].join("\n");

  var WGSL_AFFINE_NORMAL = "fn gosxAffineNormal(m:mat4x4f,n:vec3f)->vec4f{let e=max(max(abs(m[0].xyz),abs(m[1].xyz)),abs(m[2].xyz));let s=max(e.x,max(e.y,e.z));if(s==0.0){return vec4f(normalize(n),1.0);}let a=m[0].xyz/s;let b=m[1].xyz/s;let c=m[2].xyz/s;let x=cross(b,c);let y=cross(c,a);let z=cross(a,b);let d=dot(a,x);if(abs(d)<=1e-12){return vec4f(normalize(n),1.0);}let o=select(-1.0,1.0,d>0.0);return vec4f(normalize(mat3x3f(x,y,z)*n*o),o);}";

  // -----------------------------------------------------------------------
  // PBR Vertex Shader (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_PBR_VERTEX = [
    WGSL_FRAME_STRUCTS,
    WGSL_MATERIAL_STRUCT,
    WGSL_AFFINE_NORMAL,
    "",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) worldPos: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec3f,",
    "    @location(4) bitangent: vec3f,",
    "    @location(5) @interpolate(flat) instanceColor: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(1) @binding(0) var<uniform> material: MaterialUniforms;",
    "@group(1) @binding(19) var<uniform> objectModel: mat4x4f;",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> VertexOutput {",
    "    var out: VertexOutput;",
    "    let worldPosition = objectModel * vec4f(in.position, 1.0);",
    "    let an = gosxAffineNormal(objectModel, in.normal);",
    "    out.worldPos = worldPosition.xyz;",
    "    out.normal = an.xyz;",
    "    out.uv = in.uv;",
    "    let rawT = (objectModel * vec4f(in.tangent.xyz, 0.0)).xyz;",
    "    let N = out.normal;",
    "    var tangentFrame = rawT - N * dot(N, rawT);",
    "    if (dot(tangentFrame, tangentFrame) < 1e-12) {",
    "        let axis = select(vec3f(0.0, 1.0, 0.0), vec3f(1.0, 0.0, 0.0), abs(N.x) < 0.8);",
    "        tangentFrame = axis - N * dot(N, axis);",
    "    }",
    "    let T = normalize(tangentFrame);",
    "    out.tangent = T;",
    "    out.bitangent = cross(N, T) * in.tangent.w * an.w;",
    "    out.instanceColor = vec4f(1.0, 1.0, 1.0, 1.0);",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * worldPosition;",
    "    return out;",
    "}",
  ].join("\n");

  // Extends Elio's stdlib.Skin() position blend to skin normals and tangents.
  // The runtime pads each dispatch buffer to the 64-wide workgroup size.
  //
  // One dispatch writes three contiguous regions of a single tracked output
  // buffer, each paddedCount elements wide: positions (vec3) first, then
  // normals (vec3), then tangents (vec4). Each invocation blends the four
  // bone influences into ONE weighted mat4x4 blend: its translation
  // column skins the position while the linear xyz columns skin the packed
  // source normal and tangent, safe-normalized so degenerate vectors pass
  // through as zero instead of NaN. Tangent w is carried through untouched so
  // bitangent handedness survives skinning. paddedCount comes from
  // arrayLength(&verts) to place the normal/tangent region bases.
  var SCENE_ELIO_SKIN_LBS_SOURCE = [
    WGSL_AFFINE_NORMAL,
    "",
    "struct SkinVertex {",
    "  px : f32,",
    "  py : f32,",
    "  pz : f32,",
    "  w0 : f32,",
    "  w1 : f32,",
    "  w2 : f32,",
    "  w3 : f32,",
    "  b0 : u32,",
    "  b1 : u32,",
    "  b2 : u32,",
    "  b3 : u32,",
    "  nx : f32,",
    "  ny : f32,",
    "  nz : f32,",
    "  tx : f32,",
    "  ty : f32,",
    "  tz : f32,",
    "  tw : f32,",
    "};",
    "",
    "@group(0) @binding(0) var<storage, read> bones : array<mat4x4<f32>>;",
    "@group(0) @binding(1) var<storage, read> verts : array<SkinVertex>;",
    "@group(0) @binding(2) var<storage, read_write> out : array<f32>;",
    "",
    "@compute @workgroup_size(64)",
    "fn skin(@builtin(global_invocation_id) gid : vec3<u32>) {",
    "  let i = gid.x;",
    "  let paddedCount = arrayLength(&verts);",
    "  let v = verts[i];",
    "  let m = (bones[v.b0] * v.w0 + bones[v.b1] * v.w1) + (bones[v.b2] * v.w2 + bones[v.b3] * v.w3);",
    "  let skinned = (m * vec4f(v.px, v.py, v.pz, 1.0)).xyz;",
    "  let an = gosxAffineNormal(m, vec3f(v.nx, v.ny, v.nz));",
    "  let rn = an.xyz;",
    "  let rt = (m * vec4f(v.tx, v.ty, v.tz, 0.0)).xyz;",
    "  let nLen = length(rn);",
    "  let sn = select(rn, rn / nLen, nLen > 0.000001);",
    "  let ot = rt - sn * dot(sn, rt);",
    "  let tLen = length(ot);",
    "  let st = select(ot, ot / tLen, tLen > 0.000001);",
    "  let posBase = i * 3u;",
    "  out[posBase] = skinned.x;",
    "  out[posBase + 1u] = skinned.y;",
    "  out[posBase + 2u] = skinned.z;",
    "  let normBase = (paddedCount * 3u) + posBase;",
    "  out[normBase] = sn.x;",
    "  out[normBase + 1u] = sn.y;",
    "  out[normBase + 2u] = sn.z;",
    "  let tanBase = (paddedCount * 6u) + (i * 4u);",
    "  out[tanBase] = st.x;",
    "  out[tanBase + 1u] = st.y;",
    "  out[tanBase + 2u] = st.z;",
    "  out[tanBase + 3u] = v.tw * an.w;",
    "}",
  ].join("\n");

  var SCENE_COMPUTED_MORPH_SOURCE = [
    WGSL_AFFINE_NORMAL,
    "",
    "struct MorphUniforms {",
    "  model : mat4x4<f32>,",
    "  alpha : f32,",
    "  count : f32,",
    "  _pad0 : f32,",
    "  _pad1 : f32,",
    "};",
    "",
    "@group(0) @binding(0) var<storage, read> sourcePacked : array<f32>;",
    "@group(0) @binding(1) var<storage, read> targetPacked : array<f32>;",
    "@group(0) @binding(2) var<storage, read_write> outPositions : array<f32>;",
    "@group(0) @binding(3) var<storage, read_write> outNormals : array<f32>;",
    "@group(0) @binding(4) var<storage, read_write> outTangents : array<f32>;",
    "@group(0) @binding(5) var<uniform> morph : MorphUniforms;",
    "",
    "fn safeNormalize(v : vec3<f32>, fallback : vec3<f32>) -> vec3<f32> {",
    "  let len = length(v);",
    "  if (len > 0.000001) {",
    "    return v / len;",
    "  }",
    "  return fallback;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn morphPose(@builtin(global_invocation_id) gid : vec3<u32>) {",
    "  let i = gid.x;",
    "  if (f32(i) >= morph.count) {",
    "    return;",
    "  }",
    "  let p = i * 3u;",
    "  let t = i * 4u;",
    "  let packed = i * 10u;",
    "  let a = clamp(morph.alpha, 0.0, 1.0);",
    "  let localPosition = mix(",
    "    vec3<f32>(sourcePacked[packed], sourcePacked[packed + 1u], sourcePacked[packed + 2u]),",
    "    vec3<f32>(targetPacked[packed], targetPacked[packed + 1u], targetPacked[packed + 2u]),",
    "    a",
    "  );",
    "  let worldPosition = morph.model * vec4<f32>(localPosition, 1.0);",
    "  outPositions[p] = worldPosition.x;",
    "  outPositions[p + 1u] = worldPosition.y;",
    "  outPositions[p + 2u] = worldPosition.z;",
    "",
    "  let localNormal = mix(",
    "    vec3<f32>(sourcePacked[packed + 3u], sourcePacked[packed + 4u], sourcePacked[packed + 5u]),",
    "    vec3<f32>(targetPacked[packed + 3u], targetPacked[packed + 4u], targetPacked[packed + 5u]),",
    "    a",
    "  );",
    "  let an = gosxAffineNormal(morph.model, localNormal);",
    "  let wn = an.xyz;",
    "  outNormals[p] = wn.x;",
    "  outNormals[p + 1u] = wn.y;",
    "  outNormals[p + 2u] = wn.z;",
    "",
    "  let localTangent = mix(",
    "    vec3<f32>(sourcePacked[packed + 6u], sourcePacked[packed + 7u], sourcePacked[packed + 8u]),",
    "    vec3<f32>(targetPacked[packed + 6u], targetPacked[packed + 7u], targetPacked[packed + 8u]),",
    "    a",
    "  );",
    "  let rt = (morph.model * vec4<f32>(localTangent, 0.0)).xyz;",
    "  let wt = safeNormalize(rt-wn*dot(wn,rt), vec3<f32>(1.0, 0.0, 0.0));",
    "  outTangents[t] = wt.x;",
    "  outTangents[t + 1u] = wt.y;",
    "  outTangents[t + 2u] = wt.z;",
    "  outTangents[t + 3u] = mix(sourcePacked[packed + 9u], targetPacked[packed + 9u], a) * an.w;",
    "}",
  ].join("\n");

  var SCENE_WATER_COMPUTE_SOURCE = [
    WGSL_COMMON_CONSTANTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct WaterDisplacementSphere {",
    "  offsetRadius: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(0) @binding(1) var<storage, read> inState: array<vec4f>;",
    "@group(0) @binding(2) var<storage, read_write> outState: array<vec4f>;",
    "@group(0) @binding(3) var<storage, read> objectSpheres: array<WaterDisplacementSphere>;",
    "",
    "fn waterIndex(x: u32, y: u32) -> u32 {",
    "  return y * params.resolution + x;",
    "}",
    "",
    "fn hash01(n: f32) -> f32 {",
    "  return fract(sin(n) * 43758.5453123);",
    "}",
    "",
    "fn waterCoord(i: u32) -> vec2f {",
    "  let res = params.resolution;",
    "  let x = i % res;",
    "  let y = i / res;",
    "  return (vec2f(f32(x), f32(y)) + vec2f(0.5)) / max(vec2f(f32(res)), vec2f(1.0));",
    "}",
    "",
    "fn volumeInSphere(coord: vec2f, center: vec3f, radius: f32, displacementScale: f32) -> f32 {",
    "  let safeRadius = max(radius, 0.0001);",
    "  let toCenter = vec3f(coord.x * 2.0 - 1.0, 0.0, coord.y * 2.0 - 1.0) - center;",
    "  let t = length(toCenter) / safeRadius;",
    "  let dy = exp(-pow(t * 1.5, 6.0));",
    "  let ymin = min(0.0, center.y - dy);",
    "  let ymax = min(max(0.0, center.y + dy), ymin + 2.0 * dy);",
    "  return (ymax - ymin) * 0.1 * displacementScale;",
    "}",
    "",
    "fn volumeInCube(coord: vec2f, center: vec3f, halfSize: vec3f, displacementScale: f32) -> f32 {",
    "  let safeHalfSize = max(halfSize, vec3f(0.0001));",
    "  let point = vec3f(coord.x * 2.0 - 1.0, 0.0, coord.y * 2.0 - 1.0);",
    "  let distanceToBox = abs(point - center) - safeHalfSize;",
    "  let signedDistance = length(max(distanceToBox, vec3f(0.0))) + min(max(distanceToBox.x, max(distanceToBox.y, distanceToBox.z)), 0.0);",
    "  let scale = max(max(safeHalfSize.x, safeHalfSize.y), safeHalfSize.z);",
    "  let t = max(signedDistance, 0.0) / scale;",
    "  let dy = exp(-pow(t * 1.5, 6.0));",
    "  let ymin = min(0.0, center.y - dy);",
    "  let ymax = min(max(0.0, center.y + dy), ymin + 2.0 * dy);",
    "  return (ymax - ymin) * 0.1 * displacementScale;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn seedDrops(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  let res = params.resolution;",
    "  let x = i % res;",
    "  let y = i / res;",
    "  let uv = (vec2f(f32(x), f32(y)) + vec2f(0.5)) / max(vec2f(f32(res)), vec2f(1.0));",
    "  var info = inState[i];",
    "  let count = min(params.seedDrops, 64u);",
    "  let seedSalt = params.seedSalt;",
    "  for (var j = 0u; j < count; j = j + 1u) {",
    "    let jf = f32(j + 1u);",
    "    let center = vec2f(hash01(jf * 12.9898 + seedSalt + 0.173), hash01(jf * 78.233 + seedSalt * 1.371 + 0.719));",
    "    let radius = max(params.dropRadius, 0.0001);",
    "    var drop = max(0.0, 1.0 - length(center - uv) / radius);",
    "    drop = 0.5 - cos(drop * PI) * 0.5;",
    "    let polarity = select(1.0, -1.0, (j & 1u) == 0u);",
    "    info.x = info.x + drop * params.dropStrength * polarity;",
    "  }",
    "  outState[i] = info;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn addDrop(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  let uv = waterCoord(i);",
    "  var info = inState[i];",
    "  let center = params.interactiveDrop.xy * 0.5 + vec2f(0.5);",
    "  let radius = max(params.interactiveDrop.z, 0.0001);",
    "  var drop = max(0.0, 1.0 - length(center - uv) / radius);",
    "  drop = 0.5 - cos(drop * PI) * 0.5;",
    "  info.x = info.x + drop * params.interactiveDrop.w;",
    "  outState[i] = info;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn displaceObject(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  var info = inState[i];",
    "  let kind = params.objectParams.x;",
    "  let displacementScale = max(params.objectParams.y, 0.0);",
    "  if (kind < 0.5 || displacementScale <= 0.0) {",
    "    outState[i] = info;",
    "    return;",
    "  }",
    "  let coord = waterCoord(i);",
    "  let previous = params.objectPreviousCenter.xyz;",
    "  let current = params.objectCenter.xyz;",
    "  if (kind < 1.5) {",
    "    let radius = params.objectHalfSizeRadius.w;",
    "    info.x = info.x + volumeInSphere(coord, previous, radius, displacementScale);",
    "    info.x = info.x - volumeInSphere(coord, current, radius, displacementScale);",
    "  } else if (kind < 2.5) {",
    "    let halfSize = params.objectHalfSizeRadius.xyz;",
    "    info.x = info.x + volumeInCube(coord, previous, halfSize, displacementScale);",
    "    info.x = info.x - volumeInCube(coord, current, halfSize, displacementScale);",
    "  } else {",
    "    let sphereCount = min(u32(params.objectParams.z), 32u);",
    "    for (var sphereIndex = 0u; sphereIndex < sphereCount; sphereIndex = sphereIndex + 1u) {",
    "      let sphere = objectSpheres[sphereIndex].offsetRadius;",
    "      let offset = sphere.xyz;",
    "      let radius = max(sphere.w, 0.0001);",
    "      info.x = info.x + volumeInSphere(coord, previous + offset, radius, displacementScale);",
    "      info.x = info.x - volumeInSphere(coord, current + offset, radius, displacementScale);",
    "    }",
    "  }",
    "  outState[i] = info;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn stepSimulation(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  let res = params.resolution;",
    "  let x = i % res;",
    "  let y = i / res;",
    "  var westX = 0u;",
    "  if (x > 0u) { westX = x - 1u; }",
    "  let eastX = min(x + 1u, res - 1u);",
    "  var southY = 0u;",
    "  if (y > 0u) { southY = y - 1u; }",
    "  let northY = min(y + 1u, res - 1u);",
    "  var info = inState[i];",
    "  let average = (",
    "    inState[waterIndex(westX, y)].x +",
    "    inState[waterIndex(eastX, y)].x +",
    "    inState[waterIndex(x, southY)].x +",
    "    inState[waterIndex(x, northY)].x",
    "  ) * 0.25;",
    "  info.y = (info.y + (average - info.x) * 2.0 * params.waveSpeed) * params.damping;",
    "  info.x = info.x + info.y;",
    "  outState[i] = info;",
    "}",
    "",
    "@compute @workgroup_size(64)",
    "fn updateNormals(@builtin(global_invocation_id) gid: vec3u) {",
    "  let i = gid.x;",
    "  if (i >= params.cellCount) { return; }",
    "  let res = params.resolution;",
    "  let x = i % res;",
    "  let y = i / res;",
    "  let eastX = min(x + 1u, res - 1u);",
    "  let northY = min(y + 1u, res - 1u);",
    "  var info = inState[i];",
    "  let deltaX = 2.0 * max(params.poolWidth, 0.0001) / max(f32(res), 1.0);",
    "  let deltaZ = 2.0 * max(params.poolLength, 0.0001) / max(f32(res), 1.0);",
    "  let dx = vec3f(deltaX, inState[waterIndex(eastX, y)].x - info.x, 0.0);",
    "  let dz = vec3f(0.0, inState[waterIndex(x, northY)].x - info.x, deltaZ);",
    "  let normal = normalize(cross(dz, dx));",
    "  info.z = normal.x;",
    "  info.w = normal.z;",
    "  outState[i] = info;",
    "}",
  ].join("\n");

  var SCENE_WATER_RENDER_VERTEX_SOURCE = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) worldPos: vec3f,",
    "  @location(1) normal: vec3f,",
    "  @location(2) uv: vec2f,",
    "  @location(3) height: f32,",
    "};",
    "",
    "struct WaterObjectTextureMatrices {",
    "  viewProjectionMatrix: mat4x4f,",
    "  reflectionViewProjectionMatrix: mat4x4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(1) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(1) @binding(1) var<storage, read> state: array<vec4f>;",
    "",
    "fn waterIndex(x: u32, y: u32) -> u32 {",
    "  return y * params.resolution + x;",
    "}",
    "",
    // The surface grid is drawn INDEXED. It used to be a non-indexed triangle list:
    // six vertices per quad, so every interior grid point was transformed ~6 times and
    // re-read `state[]` from the storage buffer ~6 times. At the shipped 192x192 grid
    // that is 218,886 vertex invocations per pass, twice per frame (above + below
    // surface) — and on Apple/Metal it dominated the frame: measured 200ms of GPU work
    // for a scene occupying 0.1 megapixels, while the same scene minus the water cost
    // 1.3ms. Immediate-mode desktop GPUs brute-force the duplication and show nothing.
    //
    // With an index buffer, vertex_index IS the grid vertex id (resolution^2 of them,
    // 36,864 at the default), the post-transform cache does its job, and the geometry
    // produced is identical.
    "@vertex fn vertexMain(@builtin(vertex_index) vertexIndex: u32) -> VertexOutput {",
    "  let cellsPerSide = max(params.resolution - 1u, 1u);",
    "  let quad = vertexIndex / 6u;",
    "  let corner = vertexIndex % 6u;",
    "  let cellX = quad % cellsPerSide;",
    "  let cellY = quad / cellsPerSide;",
    "  var ox = 0u;",
    "  var oy = 0u;",
    "  if (corner == 1u || corner == 2u || corner == 4u) { ox = 1u; }",
    "  if (corner == 2u || corner == 4u || corner == 5u) { oy = 1u; }",
    "  let gx = min(cellX + ox, params.resolution - 1u);",
    "  let gy = min(cellY + oy, params.resolution - 1u);",
    "  let uv = vec2f(f32(gx), f32(gy)) / max(vec2f(f32(params.resolution - 1u)), vec2f(1.0));",
    "  let info = state[waterIndex(gx, gy)];",
    "  let nx = info.z * params.normalScale;",
    "  let nz = info.w * params.normalScale;",
    "  let ny = sqrt(max(0.0, 1.0 - info.z * info.z - info.w * info.w));",
    "  var out: VertexOutput;",
    "  out.height = info.x;",
    "  out.uv = uv;",
    "  out.worldPos = vec3f((uv.x - 0.5) * params.poolWidth * 2.0, info.x * params.poolHeight, (uv.y - 0.5) * params.poolLength * 2.0);",
    "  out.normal = normalize(vec3f(nx, ny, nz));",
    "  out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(out.worldPos, 1.0);",
    "  return out;",
    "}",
  ].join("\n");

  var SCENE_WATER_RENDER_FRAGMENT_SOURCE = [
    WGSL_FRAME_STRUCTS,
    "",
    "const WATER_SURFACE_VIEW_BELOW: bool = false;",
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) worldPos: vec3f,",
    "  @location(1) normal: vec3f,",
    "  @location(2) uv: vec2f,",
    "  @location(3) height: f32,",
    "};",
    "",
    "struct WaterObjectTextureMatrices {",
    "  viewProjectionMatrix: mat4x4f,",
    "  reflectionViewProjectionMatrix: mat4x4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(1) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(1) @binding(2) var causticSampler: sampler;",
    "@group(1) @binding(3) var causticTexture: texture_2d<f32>;",
    "@group(1) @binding(4) var objectReflectionTexture: texture_2d<f32>;",
    "@group(1) @binding(5) var objectClippedReflectionTexture: texture_2d<f32>;",
    "@group(1) @binding(6) var objectRefractionTexture: texture_2d<f32>;",
    "@group(1) @binding(7) var waterSkyTexture: texture_cube<f32>;",
    "@group(1) @binding(8) var<uniform> objectTextureMatrices: WaterObjectTextureMatrices;",
    "",
    "fn roundedPoolSDF(point: vec2f, halfSize: vec2f, radius: f32) -> f32 {",
    "  let r = clamp(radius, 0.0, max(0.0, min(halfSize.x, halfSize.y) - 0.001));",
    "  let q = abs(point) - max(halfSize - vec2f(r), vec2f(0.001));",
    "  return length(max(q, vec2f(0.0))) + min(max(q.x, q.y), 0.0) - r;",
    "}",
    "fn roundedWaterSDF(point: vec2f, halfSize: vec2f, radius: f32) -> f32 {",
    "  let inset = max(min(halfSize.x, halfSize.y) * 0.018, 0.012);",
    "  return roundedPoolSDF(point, max(halfSize - vec2f(inset), vec2f(0.001)), max(radius - inset, 0.0));",
    "}",
    "",
    "fn sampleWaterSky(direction: vec3f) -> vec3f {",
    "  let sky = textureSample(waterSkyTexture, causticSampler, normalize(direction)).rgb;",
    "  let horizon = clamp(direction.y * 0.5 + 0.5, 0.0, 1.0);",
    "  let fallback = mix(params.deepColor.rgb * 0.55, params.shallowColor.rgb * 1.12, horizon);",
    "  return mix(fallback, sky, 0.82);",
    "}",
    "",
    "fn sampleProjectedTexture(tex: texture_2d<f32>, matrix: mat4x4f, worldPos: vec3f) -> vec4f {",
    "  let clip = matrix * vec4f(worldPos, 1.0);",
    "  let safeW = select(0.0001, clip.w, abs(clip.w) > 0.0001);",
    "  let ndc = clip.xyz / safeW;",
    "  let uv = clamp(ndc.xy * vec2f(0.5, -0.5) + vec2f(0.5), vec2f(0.0), vec2f(1.0));",
    "  let inBounds = step(0.0, uv.x) * step(0.0, uv.y) * step(uv.x, 1.0) * step(uv.y, 1.0) * step(0.0, clip.w);",
    "  return textureSampleLevel(tex, causticSampler, uv, 0.0) * inBounds;",
    "}",
    "",
    "fn intersectSurfaceSphereBounds(origin: vec3f, ray: vec3f, center: vec3f, radius: f32) -> f32 {",
    "  let toSphere = origin - center;",
    "  let a = dot(ray, ray);",
    "  let b = 2.0 * dot(toSphere, ray);",
    "  let c = dot(toSphere, toSphere) - radius * radius;",
    "  let discriminant = b * b - 4.0 * a * c;",
    "  if (discriminant > 0.0 && a > 0.0000001) {",
    "    let root = sqrt(discriminant);",
    "    let near = (-b - root) / (2.0 * a);",
    "    let far = (-b + root) / (2.0 * a);",
    "    if (near > 0.0) { return near; }",
    "    if (far > 0.0) { return 0.0; }",
    "  }",
    "  return 1000000.0;",
    "}",
    "",
    "fn surfaceObjectCenterWorld() -> vec3f {",
    "  return vec3f(params.objectCenter.x * params.poolWidth, params.objectCenter.y, params.objectCenter.z * params.poolLength);",
    "}",
    "",
    "fn surfaceObjectHalfSizeWorld() -> vec3f {",
    "  return vec3f(params.objectHalfSizeRadius.x * params.poolWidth, params.objectHalfSizeRadius.y, params.objectHalfSizeRadius.z * params.poolLength);",
    "}",
    "",
    "fn surfaceObjectRadiusWorld() -> f32 {",
    "  return max(params.objectHalfSizeRadius.w * params.poolLength, 0.001);",
    "}",
    "",
    "fn objectTextureRadiusWorld() -> f32 {",
    "  if (params.objectParams.x < 2.5) {",
    "    let halfSize = surfaceObjectHalfSizeWorld();",
    "    return max(max(max(halfSize.x, halfSize.y), halfSize.z), surfaceObjectRadiusWorld());",
    "  }",
    "  return max(surfaceObjectRadiusWorld(), 0.31);",
    "}",
    "",
    "fn sampleObjectRefraction(origin: vec3f, ray: vec3f) -> vec4f {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return vec4f(0.0); }",
    "  let hit = intersectSurfaceSphereBounds(origin, ray, surfaceObjectCenterWorld(), objectTextureRadiusWorld());",
    "  if (hit >= 1000000.0) { return vec4f(0.0); }",
    "  return sampleProjectedTexture(objectRefractionTexture, objectTextureMatrices.viewProjectionMatrix, origin + ray * hit);",
    "}",
    "",
    "fn sampleObjectReflection(origin: vec3f, ray: vec3f) -> vec4f {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return vec4f(0.0); }",
    "  let hit = intersectSurfaceSphereBounds(origin, ray, surfaceObjectCenterWorld(), objectTextureRadiusWorld());",
    "  if (hit >= 1000000.0) { return vec4f(0.0); }",
    "  return sampleProjectedTexture(objectReflectionTexture, objectTextureMatrices.reflectionViewProjectionMatrix, origin + ray * hit);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    "  var shapeAlpha = 1.0;",
    "  if (params.poolShape > 0.5) {",
    "    let halfSize = vec2f(max(params.poolWidth, 0.001), max(params.poolLength, 0.001));",
    "    let sdf = roundedWaterSDF(in.worldPos.xz, halfSize, params.cornerRadius);",
    "    let edge = max(0.008, min(params.poolWidth, params.poolLength) / max(f32(params.resolution), 1.0));",
    "    shapeAlpha = smoothstep(edge, -edge, sdf);",
    "    if (shapeAlpha <= 0.001) { discard; }",
    "  }",
    "  var n = normalize(in.normal);",
    "  if (WATER_SURFACE_VIEW_BELOW) { n = -n; }",
    "  let viewDir = normalize(frame.cameraPos - in.worldPos);",
    "  let reflectDir = reflect(-viewDir, n);",
    "  let refractEta = select(1.0 / 1.333, 1.333 / 1.0, WATER_SURFACE_VIEW_BELOW);",
    "  let refractDir = refract(-viewDir, n, refractEta);",
    "  let fresnelBase = select(0.25, 0.50, WATER_SURFACE_VIEW_BELOW);",
    "  let fresnel = mix(fresnelBase, 1.0, pow(1.0 - clamp(dot(n, viewDir), 0.0, 1.0), 3.0));",
    "  let causticsEnabled = clamp(params.opticsFlags.x, 0.0, 1.0);",
    "  let reflectionEnabled = clamp(params.opticsFlags.y, 0.0, 1.0);",
    "  let refractionEnabled = clamp(params.opticsFlags.z, 0.0, 1.0);",
    "  var causticTexel = vec3f(0.0);",
    "  if (causticsEnabled > 0.0) {",
    "    causticTexel = textureSample(causticTexture, causticSampler, clamp(in.uv, vec2f(0.0), vec2f(1.0))).rgb;",
    "  }",
    "  var reflectionTexel = vec4f(0.0);",
    "  var clippedReflectionTexel = vec4f(0.0);",
    "  var refractionTexel = vec4f(0.0);",
    "  if (reflectionEnabled > 0.0) {",
    "    reflectionTexel = sampleObjectReflection(in.worldPos, reflectDir);",
    "    clippedReflectionTexel = sampleProjectedTexture(objectClippedReflectionTexture, objectTextureMatrices.reflectionViewProjectionMatrix, in.worldPos);",
    "  }",
    "  if (refractionEnabled > 0.0) {",
    "    refractionTexel = sampleObjectRefraction(in.worldPos, refractDir);",
    "  }",
    "  let causticHint = causticTexel.r * causticsEnabled;",
    "  let depthMix = clamp(0.38 + in.height * 8.0 + in.uv.y * 0.18, 0.0, 1.0);",
    "  var reflectedColor = sampleWaterSky(reflectDir);",
    "  var refractedColor = mix(params.deepColor.rgb, params.shallowColor.rgb, depthMix);",
    "  if (WATER_SURFACE_VIEW_BELOW) {",
    "    reflectedColor = reflectedColor * vec3f(0.4, 0.9, 1.0);",
    "    refractedColor = refractedColor * vec3f(0.8, 1.0, 1.1) + vec3f(0.10, 0.38, 0.46) * causticHint * 0.10;",
    "  } else {",
    "    refractedColor = refractedColor * vec3f(0.25, 1.0, 1.25) + vec3f(0.18, 0.28, 0.22) * causticHint * 0.08;",
    "  }",
    "  if (params.objectParams.x >= 2.5 && params.opticsFlags.w > 0.0) {",
    "    if (WATER_SURFACE_VIEW_BELOW) {",
    "      if (params.objectParams.w > 0.5 && params.objectParams.w < 1.5) {",
    "        let refractedObject = max(refractionTexel, refractionTexel * vec4f(0.78, 1.0, 1.08, 1.0));",
    "        refractedColor = mix(refractedColor, refractedObject.rgb, refractedObject.a * refractionEnabled);",
    "        reflectedColor = mix(reflectedColor, reflectionTexel.rgb, reflectionTexel.a * reflectionEnabled);",
    "      } else {",
    "        refractedColor = mix(refractedColor, refractionTexel.rgb * vec3f(0.78, 1.0, 1.08), refractionTexel.a * refractionEnabled);",
    "        reflectedColor = mix(reflectedColor, reflectionTexel.rgb, reflectionTexel.a * reflectionEnabled);",
    "      }",
    "    } else if (params.objectParams.w > 0.5 && params.objectParams.w < 1.5) {",
    "      refractedColor = mix(refractedColor, refractionTexel.rgb, refractionTexel.a * refractionEnabled);",
    "      reflectedColor = mix(reflectedColor, reflectionTexel.rgb, reflectionTexel.a * reflectionEnabled);",
    "    } else {",
    "      refractedColor = mix(refractedColor, refractionTexel.rgb, refractionTexel.a * refractionEnabled);",
    "      reflectedColor = mix(reflectedColor, clippedReflectionTexel.rgb, clippedReflectionTexel.a * reflectionEnabled);",
    "    }",
    "  }",
    "  if (WATER_SURFACE_VIEW_BELOW) {",
    "    return vec4f(mix(reflectedColor, refractedColor, (1.0 - fresnel) * length(refractDir)), shapeAlpha);",
    "  }",
    "  return vec4f(mix(refractedColor, reflectedColor, fresnel), shapeAlpha);",
    "}",
  ].join("\n");

  var SCENE_WATER_RENDER_BELOW_FRAGMENT_SOURCE = SCENE_WATER_RENDER_FRAGMENT_SOURCE.replace(
    "const WATER_SURFACE_VIEW_BELOW: bool = false;",
    "const WATER_SURFACE_VIEW_BELOW: bool = true;"
  );

  var SCENE_WATER_POOL_VERTEX_SOURCE = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) worldPos: vec3f,",
    "  @location(1) normal: vec3f,",
    "  @location(2) tileUV: vec2f,",
    "  @location(3) waterUV: vec2f,",
    "  @location(4) face: f32,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(1) @binding(0) var<uniform> params: WaterUniforms;",
    "",
    "const WATER_POOL_ROUNDED_SEGMENTS: u32 = 44u;",
    "const WATER_POOL_ROUNDED_CORNER_SAMPLES: u32 = 11u;",
    "const WATER_POOL_ROUNDED_CORNER_STEPS: f32 = 10.0;",
    "const WATER_POOL_ROUNDED_FLOOR_VERTICES: u32 = WATER_POOL_ROUNDED_SEGMENTS * 3u;",
    "const WATER_POOL_HALF_PI: f32 = 1.57079632679;",
    "",
    "fn waterPoolCornerSign(corner: u32) -> vec2f {",
    "  var signValue = vec2f(1.0, 1.0);",
    "  if (corner == 1u || corner == 2u) { signValue.x = -1.0; }",
    "  if (corner >= 2u) { signValue.y = -1.0; }",
    "  return signValue;",
    "}",
    "",
    "fn waterPoolRoundedBoundaryPoint(index: u32, halfWidth: f32, halfLength: f32, radius: f32) -> vec2f {",
    "  let wrapped = index % WATER_POOL_ROUNDED_SEGMENTS;",
    "  let corner = min(wrapped / WATER_POOL_ROUNDED_CORNER_SAMPLES, 3u);",
    "  let local = wrapped % WATER_POOL_ROUNDED_CORNER_SAMPLES;",
    "  let signValue = waterPoolCornerSign(corner);",
    "  let inset = max(vec2f(halfWidth, halfLength) - vec2f(radius), vec2f(0.001));",
    "  let theta = f32(corner) * WATER_POOL_HALF_PI + f32(local) / WATER_POOL_ROUNDED_CORNER_STEPS * WATER_POOL_HALF_PI;",
    "  return signValue * inset + vec2f(cos(theta), sin(theta)) * radius;",
    "}",
    "",
    "fn waterPoolRoundedBoundaryNormal(point: vec2f, halfWidth: f32, halfLength: f32, radius: f32) -> vec2f {",
    "  let inset = max(vec2f(halfWidth, halfLength) - vec2f(radius), vec2f(0.001));",
    "  let absPoint = abs(point);",
    "  var outward = vec2f(0.0, 1.0);",
    "  if (absPoint.x > inset.x && absPoint.y > inset.y && radius > 0.0001) {",
    "    outward = normalize(point - sign(point) * inset);",
    "  } else if (absPoint.x / max(halfWidth, 0.001) > absPoint.y / max(halfLength, 0.001)) {",
    "    outward = vec2f(sign(point.x), 0.0);",
    "  } else {",
    "    outward = vec2f(0.0, sign(point.y));",
    "  }",
    "  return -outward;",
    "}",
    "",
    "fn waterPoolQuadUV(corner: u32) -> vec2f {",
    "  var uv = vec2f(0.0);",
    "  if (corner == 1u || corner == 2u || corner == 4u) { uv.x = 1.0; }",
    "  if (corner == 2u || corner == 4u || corner == 5u) { uv.y = 1.0; }",
    "  return uv;",
    "}",
    "",
    "fn waterPoolRoundedVertex(vertexIndex: u32, halfWidth: f32, halfLength: f32, floorY: f32, rimY: f32, radius: f32) -> VertexOutput {",
    "  var worldPos = vec3f(0.0);",
    "  var normal = vec3f(0.0, 1.0, 0.0);",
    "  var tileUV = vec2f(0.0);",
    "  var face = 0.0;",
    "  if (vertexIndex < WATER_POOL_ROUNDED_FLOOR_VERTICES) {",
    "    let tri = vertexIndex / 3u;",
    "    let corner = vertexIndex % 3u;",
    "    var point = vec2f(0.0);",
    "    if (corner == 1u) {",
    "      point = waterPoolRoundedBoundaryPoint((tri + 1u) % WATER_POOL_ROUNDED_SEGMENTS, halfWidth, halfLength, radius);",
    "    } else if (corner == 2u) {",
    "      point = waterPoolRoundedBoundaryPoint(tri, halfWidth, halfLength, radius);",
    "    }",
    "    worldPos = vec3f(point.x, floorY, point.y);",
    "    tileUV = point * 0.42;",
    "  } else {",
    "    let localIndex = vertexIndex - WATER_POOL_ROUNDED_FLOOR_VERTICES;",
    "    let segment = (localIndex / 6u) % WATER_POOL_ROUNDED_SEGMENTS;",
    "    let corner = localIndex % 6u;",
    "    let quadUV = waterPoolQuadUV(corner);",
    "    let pointA = waterPoolRoundedBoundaryPoint(segment, halfWidth, halfLength, radius);",
    "    let pointB = waterPoolRoundedBoundaryPoint((segment + 1u) % WATER_POOL_ROUNDED_SEGMENTS, halfWidth, halfLength, radius);",
    "    let point = mix(pointA, pointB, quadUV.x);",
    "    let inward = waterPoolRoundedBoundaryNormal(point, halfWidth, halfLength, radius);",
    "    worldPos = vec3f(point.x, mix(floorY, rimY, quadUV.y), point.y);",
    "    normal = vec3f(inward.x, 0.0, inward.y);",
    "    tileUV = vec2f((f32(segment) + quadUV.x) * 0.18, worldPos.y * 0.72);",
    "    face = 5.0;",
    "  }",
    "  var out: VertexOutput;",
    "  out.worldPos = worldPos;",
    "  out.normal = normal;",
    "  out.tileUV = tileUV;",
    "  out.waterUV = worldPos.xz / max(vec2f(params.poolWidth * 2.0, params.poolLength * 2.0), vec2f(0.001)) + vec2f(0.5);",
    "  out.face = face;",
    "  out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(worldPos, 1.0);",
    "  return out;",
    "}",
    "",
    "@vertex fn vertexMain(@builtin(vertex_index) vertexIndex: u32) -> VertexOutput {",
    "  let halfWidth = max(params.poolWidth, 0.001);",
    "  let halfLength = max(params.poolLength, 0.001);",
    "  let floorY = -max(params.poolHeight, 0.001);",
    "  let rimY = max(params.poolHeight * (2.0 / 12.0), 0.025);",
    "  let maxCornerRadius = max(0.0, min(halfWidth, halfLength) - 0.001);",
    "  let cornerRadius = clamp(params.cornerRadius, 0.0, maxCornerRadius);",
    "  if (params.poolShape > 0.5 && cornerRadius > 0.0001) {",
    "    return waterPoolRoundedVertex(vertexIndex, halfWidth, halfLength, floorY, rimY, cornerRadius);",
    "  }",
    "  let face = min(vertexIndex / 6u, 4u);",
    "  let corner = vertexIndex % 6u;",
    "  var u = 0.0;",
    "  var v = 0.0;",
    // The pool is an open vessel, not a solid box: floor faces upward and
    // walls face inward. This matches the rounded path and lets ordinary back
    // culling hide the large exterior shell without making the water
    // double-sided. The water surface itself keeps depth writes enabled, so
    // the overlap fix is independent from this presentation winding.
    "  if (corner == 1u || corner == 2u || corner == 5u) { u = 1.0; }",
    "  if (corner == 1u || corner == 4u || corner == 5u) { v = 1.0; }",
    "  var worldPos = vec3f(0.0);",
    "  var normal = vec3f(0.0, 1.0, 0.0);",
    "  var tileUV = vec2f(0.0);",
    "  if (face == 0u) {",
    "    worldPos = vec3f(mix(-halfWidth, halfWidth, u), floorY, mix(-halfLength, halfLength, v));",
    "    normal = vec3f(0.0, 1.0, 0.0);",
    "    tileUV = worldPos.xz * 0.42;",
    "  } else if (face == 1u) {",
    "    worldPos = vec3f(mix(-halfWidth, halfWidth, u), mix(floorY, rimY, v), halfLength);",
    "    normal = vec3f(0.0, 0.0, -1.0);",
    "    tileUV = vec2f(worldPos.x * 0.42, worldPos.y * 0.72);",
    "  } else if (face == 2u) {",
    "    worldPos = vec3f(mix(halfWidth, -halfWidth, u), mix(floorY, rimY, v), -halfLength);",
    "    normal = vec3f(0.0, 0.0, 1.0);",
    "    tileUV = vec2f(worldPos.x * 0.42, worldPos.y * 0.72);",
    "  } else if (face == 3u) {",
    "    worldPos = vec3f(halfWidth, mix(floorY, rimY, v), mix(halfLength, -halfLength, u));",
    "    normal = vec3f(-1.0, 0.0, 0.0);",
    "    tileUV = vec2f(worldPos.z * 0.42, worldPos.y * 0.72);",
    "  } else {",
    "    worldPos = vec3f(-halfWidth, mix(floorY, rimY, v), mix(-halfLength, halfLength, u));",
    "    normal = vec3f(1.0, 0.0, 0.0);",
    "    tileUV = vec2f(worldPos.z * 0.42, worldPos.y * 0.72);",
    "  }",
    "  var out: VertexOutput;",
    "  out.worldPos = worldPos;",
    "  out.normal = normal;",
    "  out.tileUV = tileUV;",
    "  out.waterUV = worldPos.xz / max(vec2f(params.poolWidth * 2.0, params.poolLength * 2.0), vec2f(0.001)) + vec2f(0.5);",
    "  out.face = f32(face);",
    "  out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(worldPos, 1.0);",
    "  return out;",
    "}",
  ].join("\n");

  var SCENE_WATER_POOL_FRAGMENT_SOURCE = [
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct WaterDisplacementSphere {",
    "  offsetRadius: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) worldPos: vec3f,",
    "  @location(1) normal: vec3f,",
    "  @location(2) tileUV: vec2f,",
    "  @location(3) waterUV: vec2f,",
    "  @location(4) face: f32,",
    "};",
    "",
    "@group(1) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(1) @binding(1) var<storage, read> state: array<vec4f>;",
    "@group(1) @binding(2) var poolSampler: sampler;",
    "@group(1) @binding(3) var causticTexture: texture_2d<f32>;",
    "@group(1) @binding(4) var objectShadowTexture: texture_2d<f32>;",
    "@group(1) @binding(5) var tileTexture: texture_2d<f32>;",
    "",
    "fn waterIndex(x: u32, y: u32) -> u32 {",
    "  return y * params.resolution + x;",
    "}",
    "",
    "fn sampleWaterInfo(uv: vec2f) -> vec4f {",
    "  let safeUV = clamp(uv, vec2f(0.0), vec2f(1.0));",
    "  let maxCoord = max(params.resolution - 1u, 1u);",
    "  let x = min(u32(round(safeUV.x * f32(maxCoord))), params.resolution - 1u);",
    "  let y = min(u32(round(safeUV.y * f32(maxCoord))), params.resolution - 1u);",
    "  return state[waterIndex(x, y)];",
    "}",
    "",
    "fn objectPoolShadow(uv: vec2f, point: vec3f) -> f32 {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return 0.0; }",
    "  let centerUV = params.objectCenter.xz * 0.5 + vec2f(0.5);",
    "  let aspect = vec2f(max(params.poolWidth / max(params.poolLength, 0.001), 0.001), 1.0);",
    "  let sphereRadius = max(params.objectHalfSizeRadius.w * 0.55, 0.018);",
    "  let cubeRadius = max(max(params.objectHalfSizeRadius.x, params.objectHalfSizeRadius.z) * 0.62, sphereRadius);",
    "  let radius = select(sphereRadius, cubeRadius, params.objectParams.x > 1.5);",
    "  let d = length((uv - centerUV) * aspect);",
    "  let footprint = 1.0 - smoothstep(radius, radius + max(radius * 1.25, 0.022), d);",
    "  let proximityRadius = select(params.objectHalfSizeRadius.w, max(max(params.objectHalfSizeRadius.x, params.objectHalfSizeRadius.y), params.objectHalfSizeRadius.z), params.objectParams.x > 1.5);",
    "  let proximity = 1.0 - smoothstep(proximityRadius, proximityRadius + max(proximityRadius * 2.0, 0.08), length(point - params.objectCenter.xyz));",
    "  return max(footprint * 0.68, proximity * 0.38);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    "  let waterUV = clamp(in.waterUV, vec2f(0.0), vec2f(1.0));",
    "  let info = sampleWaterInfo(waterUV);",
    "  let waterHeight = info.x * params.poolHeight;",
    "  let lightDir = normalize(params.lightDir.xyz);",
    "  let refracted = refract(-lightDir, vec3f(0.0, 1.0, 0.0), 1.0 / 1.333);",
    "  let refractedY = select(0.05, refracted.y, abs(refracted.y) > 0.05);",
    "  let projected = (in.worldPos.xz - in.worldPos.y * refracted.xz / refractedY) / max(vec2f(params.poolWidth * 2.0, params.poolLength * 2.0), vec2f(0.001));",
    "  let causticUV = clamp(projected * 0.75 + vec2f(0.5), vec2f(0.0), vec2f(1.0));",
    "  let tileColor = textureSample(tileTexture, poolSampler, in.tileUV).rgb;",
    "  let caustic = textureSample(causticTexture, poolSampler, causticUV).rgb;",
    "  let shadowMap = textureSample(objectShadowTexture, poolSampler, waterUV).r;",
    "  let objectShadow = max(shadowMap, objectPoolShadow(waterUV, in.worldPos));",
    "  let diffuse = max(dot(normalize(in.normal), normalize(-refracted)), 0.0);",
    "  let below = select(0.0, 1.0, in.worldPos.y < waterHeight);",
    "  let distanceFade = 1.0 / max(length(in.worldPos) * 0.52, 1.0);",
    "  let underwaterTint = vec3f(0.42, 0.92, 1.0);",
    "  let dryLight = 0.46 + diffuse * 0.34;",
    "  let causticEnergy = dot(caustic, vec3f(0.34, 0.44, 0.22)) * params.opticsFlags.x;",
    "  var color = tileColor * dryLight * distanceFade;",
    "  color = mix(color, color * underwaterTint * (0.72 + diffuse * 0.22) + caustic * (1.55 + causticEnergy * 0.6), below);",
    "  color = color * (1.0 - clamp(objectShadow, 0.0, 1.0) * 0.62);",
    "  let rim = smoothstep(0.0, 0.12, in.worldPos.y);",
    "  color = mix(color, color + vec3f(0.05, 0.035, 0.018), rim * (1.0 - below));",
    "  return vec4f(color, 1.0);",
    "}",
  ].join("\n");

  var SCENE_WATER_CAUSTICS_VERTEX_SOURCE = [
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) uv: vec2f,",
    "};",
    "",
    "@vertex fn vertexMain(@builtin(vertex_index) vertexIndex: u32) -> VertexOutput {",
    "  let x = f32((vertexIndex << 1u) & 2u);",
    "  let y = f32(vertexIndex & 2u);",
    "  var out: VertexOutput;",
    "  out.uv = vec2f(x, y);",
    "  out.clipPos = vec4f(x * 2.0 - 1.0, 1.0 - y * 2.0, 0.0, 1.0);",
    "  return out;",
    "}",
  ].join("\n");

  var SCENE_WATER_CAUSTICS_FRAGMENT_SOURCE = [
    WGSL_COMMON_CONSTANTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct WaterDisplacementSphere {",
    "  offsetRadius: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) uv: vec2f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(0) @binding(1) var<storage, read> state: array<vec4f>;",
    "@group(0) @binding(2) var<storage, read> objectSpheres: array<WaterDisplacementSphere>;",
    "@group(0) @binding(3) var objectShadowSampler: sampler;",
    "@group(0) @binding(4) var objectShadowTexture: texture_2d<f32>;",
    "",
    "fn waterIndex(x: u32, y: u32) -> u32 {",
    "  return y * params.resolution + x;",
    "}",
    "",
    "fn sampleWaterInfo(uv: vec2f) -> vec4f {",
    "  let safeUV = clamp(uv, vec2f(0.0), vec2f(1.0));",
    "  let maxCoord = max(params.resolution - 1u, 1u);",
    "  let x = min(u32(round(safeUV.x * f32(maxCoord))), params.resolution - 1u);",
    "  let y = min(u32(round(safeUV.y * f32(maxCoord))), params.resolution - 1u);",
    "  return state[waterIndex(x, y)];",
    "}",
    "",
    "fn objectShadowMask(uv: vec2f) -> f32 {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return 0.0; }",
    "  let centerUV = params.objectCenter.xz * 0.5 + vec2f(0.5);",
    "  let aspect = vec2f(max(params.poolWidth / max(params.poolLength, 0.001), 0.001), 1.0);",
    "  if (params.objectParams.x >= 2.5) {",
    "    let count = min(u32(params.objectParams.z), 32u);",
    "    var mask = 0.0;",
    "    for (var i = 0u; i < count; i = i + 1u) {",
    "      let sphere = objectSpheres[i].offsetRadius;",
    "      let sphereUV = centerUV + sphere.xz * 0.5;",
    "      let radius = max(sphere.w * 0.72, 0.012);",
    "      let d = length((uv - sphereUV) * aspect);",
    "      mask = max(mask, 1.0 - smoothstep(radius, radius + max(radius * 1.25, 0.018), d));",
    "    }",
    "    return mask;",
    "  }",
    "  let sphereRadius = max(params.objectHalfSizeRadius.w * 0.55, 0.018);",
    "  let cubeRadius = max(max(params.objectHalfSizeRadius.x, params.objectHalfSizeRadius.z) * 0.6, sphereRadius);",
    "  let radius = select(sphereRadius, cubeRadius, params.objectParams.x > 1.5);",
    "  let d = length((uv - centerUV) * aspect);",
    "  return 1.0 - smoothstep(radius, radius + max(radius * 1.2, 0.02), d);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
    "  let uv = clamp(in.uv, vec2f(0.0), vec2f(1.0));",
    "  let texel = 1.0 / max(f32(params.resolution), 1.0);",
    "  let c = sampleWaterInfo(uv);",
    "  let e = sampleWaterInfo(uv + vec2f(texel, 0.0));",
    "  let w = sampleWaterInfo(uv - vec2f(texel, 0.0));",
    "  let n = sampleWaterInfo(uv + vec2f(0.0, texel));",
    "  let s = sampleWaterInfo(uv - vec2f(0.0, texel));",
    "  let lightDir = normalize(params.lightDir.xyz);",
    "  let waterNormal = normalize(vec3f(c.z * params.normalScale, 1.0, c.w * params.normalScale));",
    "  let refracted = refract(-lightDir, waterNormal, 1.0 / 1.333);",
    "  let convergence = abs((e.x + w.x + n.x + s.x) - c.x * 4.0);",
    "  let slopeFocus = max(0.0, dot(normalize(vec3f(-refracted.x, max(refracted.y, 0.05), -refracted.z)), waterNormal));",
    "  let shimmer = 0.5 + 0.5 * sin((uv.x * 41.0 + uv.y * 37.0) + params.time * 2.4 + c.x * 180.0);",
    "  var intensity = smoothstep(0.001, 0.028, convergence * 0.72 + length(c.zw) * 0.035);",
    "  intensity = intensity * (0.52 + 0.48 * shimmer) * (0.58 + 0.42 * slopeFocus);",
    "  let shadow = max(objectShadowMask(uv), textureSample(objectShadowTexture, objectShadowSampler, uv).r);",
    "  intensity = intensity * (1.0 - shadow * 0.82);",
    "  let warm = vec3f(1.0, 0.78, 0.42);",
    "  let cool = vec3f(0.44, 0.95, 1.0);",
    "  return vec4f(mix(cool, warm, clamp(intensity * 1.8, 0.0, 1.0)) * intensity, 1.0);",
    "}",
  ].join("\n");

  var SCENE_WATER_OBJECT_TEXTURE_VERTEX_SOURCE = SCENE_WATER_CAUSTICS_VERTEX_SOURCE;

  var SCENE_WATER_OBJECT_TEXTURE_FRAGMENT_SOURCE = [
    WGSL_COMMON_CONSTANTS,
    "",
    "struct WaterUniforms {",
    "  resolution: u32,",
    "  cellCount: u32,",
    "  seedDrops: u32,",
    "  frameIndex: u32,",
    "  deltaTime: f32,",
    "  time: f32,",
    "  waveSpeed: f32,",
    "  damping: f32,",
    "  dropRadius: f32,",
    "  dropStrength: f32,",
    "  normalScale: f32,",
    "  poolWidth: f32,",
    "  poolHeight: f32,",
    "  poolLength: f32,",
    "  cornerRadius: f32,",
    "  poolShape: f32,",
    "  lightDir: vec4f,",
    "  shallowColor: vec4f,",
    "  deepColor: vec4f,",
    "  objectCenter: vec4f,",
    "  objectPreviousCenter: vec4f,",
    "  objectHalfSizeRadius: vec4f,",
    "  objectParams: vec4f,",
    "  opticsFlags: vec4f,",
    "  interactiveDrop: vec4f,",
    "  seedSalt: f32,",
    "};",
    "",
    "struct WaterDisplacementSphere {",
    "  offsetRadius: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) uv: vec2f,",
    "};",
    "",
    "struct ObjectTextureOutput {",
    "  @location(0) reflection: vec4f,",
    "  @location(1) clippedReflection: vec4f,",
    "  @location(2) refraction: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> params: WaterUniforms;",
    "@group(0) @binding(1) var<storage, read> objectSpheres: array<WaterDisplacementSphere>;",
    "",
    "fn objectMaskInfo(uv: vec2f) -> vec4f {",
    "  if (params.objectParams.x < 0.5 || params.opticsFlags.w <= 0.0) { return vec4f(0.0); }",
    "  let centerUV = params.objectCenter.xz * 0.5 + vec2f(0.5);",
    "  let aspect = vec2f(max(params.poolWidth / max(params.poolLength, 0.001), 0.001), 1.0);",
    "  var mask = 0.0;",
    "  var core = 0.0;",
    "  if (params.objectParams.x >= 2.5) {",
    "    let count = min(u32(params.objectParams.z), 32u);",
    "    for (var i = 0u; i < count; i = i + 1u) {",
    "      let sphere = objectSpheres[i].offsetRadius;",
    "      let sphereUV = centerUV + sphere.xz * 0.5;",
    "      let radius = max(sphere.w * 0.72, 0.012);",
    "      let d = length((uv - sphereUV) * aspect);",
    "      let localMask = 1.0 - smoothstep(radius, radius + max(radius * 1.18, 0.018), d);",
    "      mask = max(mask, localMask);",
    "      core = max(core, 1.0 - smoothstep(radius * 0.42, radius, d));",
    "    }",
    "  } else {",
    "    let sphereRadius = max(params.objectHalfSizeRadius.w * 0.55, 0.018);",
    "    let cubeRadius = max(max(params.objectHalfSizeRadius.x, params.objectHalfSizeRadius.z) * 0.6, sphereRadius);",
    "    let radius = select(sphereRadius, cubeRadius, params.objectParams.x > 1.5);",
    "    let d = length((uv - centerUV) * aspect);",
    "    mask = 1.0 - smoothstep(radius, radius + max(radius * 1.2, 0.02), d);",
    "    core = 1.0 - smoothstep(radius * 0.38, radius, d);",
    "  }",
    "  let objectTop = params.objectCenter.y + max(params.objectHalfSizeRadius.y, params.objectHalfSizeRadius.w);",
    "  let clipped = smoothstep(-0.08, 0.16, objectTop);",
    "  return vec4f(clamp(mask, 0.0, 1.0), clamp(core, 0.0, 1.0), clipped, 0.0);",
    "}",
    "",
    "@fragment fn fragmentMain(in: VertexOutput) -> ObjectTextureOutput {",
    "  let uv = clamp(in.uv, vec2f(0.0), vec2f(1.0));",
    "  let lightOffset = normalize(params.lightDir.xyz).xz * vec2f(0.025, -0.025);",
    "  let info = objectMaskInfo(clamp(uv - lightOffset, vec2f(0.0), vec2f(1.0)));",
    "  let mask = info.x;",
    "  let core = info.y;",
    "  let clippedMask = mask * info.z;",
    "  let rim = clamp(mask - core * 0.35, 0.0, 1.0);",
    "  let reflectionColor = mix(vec3f(0.12, 0.24, 0.42), vec3f(0.82, 0.92, 1.0), rim);",
    "  let clippedColor = mix(vec3f(0.10, 0.18, 0.28), vec3f(0.72, 0.84, 0.96), core);",
    "  let refractionColor = mix(vec3f(0.06, 0.22, 0.28), vec3f(0.88, 0.66, 0.36), core);",
    "  var out: ObjectTextureOutput;",
    "  out.reflection = vec4f(reflectionColor, mask * params.opticsFlags.y);",
    "  out.clippedReflection = vec4f(clippedColor, clippedMask * params.opticsFlags.y);",
    "  out.refraction = vec4f(refractionColor, mask * params.opticsFlags.z);",
    "  return out;",
    "}",
    "",
    "@fragment fn shadowMain(in: VertexOutput) -> @location(0) vec4f {",
    "  let info = objectMaskInfo(clamp(in.uv, vec2f(0.0), vec2f(1.0)));",
    "  let shadow = info.x * (0.42 + 0.58 * info.y);",
    "  return vec4f(vec3f(shadow), 1.0);",
    "}",
  ].join("\n");

  var SCENE_WATER_OBJECT_SHADOW_FRAGMENT_SOURCE = SCENE_WATER_OBJECT_TEXTURE_FRAGMENT_SOURCE;

  var SCENE_WATER_OBJECT_MESH_SHADOW_VERTEX_SOURCE = [
    "struct ObjectMeshShadowUniforms {",
    "  light: vec4f,",
    "  pool: vec4f,",
    "};",
    "",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> shadow: ObjectMeshShadowUniforms;",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> @builtin(position) vec4f {",
    "    let worldPosition = in.position;",
    "    let refractedLight = refract(-normalize(shadow.light.xyz), vec3f(0.0, 1.0, 0.0), 1.0 / 1.333);",
    "    let fallbackY = select(-0.0001, 0.0001, refractedLight.y >= 0.0);",
    "    let refractedY = select(fallbackY, refractedLight.y, abs(refractedLight.y) > 0.0001);",
    "    let projected = 0.75 * (worldPosition.xz - worldPosition.y * refractedLight.xz / refractedY);",
    "    return vec4f(",
    "      projected.x / max(shadow.pool.x, 0.0001),",
    "      projected.y / max(shadow.pool.y, 0.0001),",
    "      0.0,",
    "      1.0",
    "    );",
    "}",
  ].join("\n");

  var SCENE_WATER_OBJECT_MESH_SHADOW_FRAGMENT_SOURCE = [
    "@fragment fn fragmentMain() -> @location(0) vec4f {",
    "  return vec4f(1.0);",
    "}",
  ].join("\n");

  var WGSL_PBR_INSTANCED_VERTEX = [
    WGSL_FRAME_STRUCTS, WGSL_MATERIAL_STRUCT,
    WGSL_AFFINE_NORMAL,
    "",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec4f,",
    "    @location(4) instanceMatrix0: vec4f,",
    "    @location(5) instanceMatrix1: vec4f,",
    "    @location(6) instanceMatrix2: vec4f,",
    "    @location(7) instanceMatrix3: vec4f,",
    "    @location(8) instanceColor: vec4f,",
    "};",
    "",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) worldPos: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec3f,",
    "    @location(4) bitangent: vec3f,",
    "    @location(5) @interpolate(flat) instanceColor: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;", "@group(1) @binding(0) var<uniform> material: MaterialUniforms;", "fn gosxWindSway(m: mat4x4f, p: vec3f) -> vec4f { let w = m * vec4f(p, 1.0); let s = material.modelMatrix[2]; if (s.x <= 0.0) { return w; } let a = material.modelMatrix[0]; let b = material.modelMatrix[1]; let o = m[3].xz; let ph = (dot(o, a.xy) - b.z * a.w) * 6.2831853 / b.y + 0.35 * sin(dot(o, vec2f(a.y, -a.x)) * 6.2831853 / (b.y * 1.7)); let k = clamp(p.y / s.y, 0.0, 1.0); let f = s.x * a.z * 0.1 * (1.0 + b.x * sin(ph)) * (0.75 + 0.25 * sin(6.2831853 * b.w * a.w + o.x * 0.37 + o.y * 0.53)) * k * k * s.y * length(m[1].xyz); return vec4f(w.x + a.x * f, w.y - 0.3 * abs(f) * k, w.z + a.y * f, 1.0); }",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> VertexOutput {",
    "    var out: VertexOutput;",
    "    let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
    "    let world = gosxWindSway(model, in.position);",
    "    out.worldPos = world.xyz;",
    "    let an = gosxAffineNormal(model, in.normal);",
    "    out.normal = an.xyz;",
    "    out.uv = in.uv;",
    "    let rawT = (model * vec4f(in.tangent.xyz, 0.0)).xyz;",
    "    let N = out.normal;",
    "    var tangentFrame = rawT - N * dot(N, rawT);",
    "    if (dot(tangentFrame, tangentFrame) < 1e-12) {",
    "        let axis = select(vec3f(0.0, 1.0, 0.0), vec3f(1.0, 0.0, 0.0), abs(N.x) < 0.8);",
    "        tangentFrame = axis - N * dot(N, axis);",
    "    }",
    "    let T = normalize(tangentFrame);",
    "    out.tangent = T;",
    "    out.bitangent = cross(N, T) * in.tangent.w * an.w;",
    "    out.instanceColor = in.instanceColor;",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * world;",
    "    return out;",
    "}",
  ].join("\n");

  // Cull-path instanced vertex shader: location 8 = pickData (vec4u) instead
  // of instanceColor (vec4f). The output struct drops instanceColor — material
  // color is read from the per-material uniform in the fragment shader, so no
  // per-instance color interpolation is needed on the cull path. VertexOutput
  // is identical to the non-cull variant (same locations 0-4) so it is
  // compatible with WGSL_PBR_FRAGMENT without modification. pickData is read
  // in vertex but not forwarded to fragment (gpu picking consumes it natively).
  var WGSL_PBR_INSTANCED_CULL_VERTEX = WGSL_PBR_INSTANCED_VERTEX
    .replace("instanceColor: vec4f,", "pickData: vec4u,")
    .replace("out.instanceColor = in.instanceColor;", "out.instanceColor = vec4f(1.0, 1.0, 1.0, 1.0);");

  // -----------------------------------------------------------------------
  // PBR Fragment Shader (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_PBR_FRAGMENT = [
    "var<private> gosxSpecularRadiance: vec3f;",
    WGSL_COMMON_CONSTANTS,
    "",
    WGSL_FRAME_STRUCTS,
    "",
    WGSL_MATERIAL_STRUCT,
    "",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) worldPos: vec3f,",
    "    @location(1) normal: vec3f,",
    "    @location(2) uv: vec2f,",
    "    @location(3) tangent: vec3f,",
    "    @location(4) bitangent: vec3f,",
    "    @location(5) @interpolate(flat) instanceColor: vec4f,",
    "};",
    "",
    // Group 0: per-frame
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "@group(0) @binding(1) var<storage, read> lights: array<Light>;",
    "@group(0) @binding(2) var<uniform> fog: FogUniforms;",
    "@group(0) @binding(3) var<uniform> env: EnvUniforms;",
    "@group(0) @binding(4) var shadowMap0: texture_depth_2d_array;",
    "@group(0) @binding(5) var shadowSampler0: sampler_comparison;",
    "@group(0) @binding(6) var shadowMap1: texture_depth_2d_array;",
    "@group(0) @binding(7) var shadowSampler1: sampler_comparison;",
    "@group(0) @binding(8) var<uniform> shadow: ShadowUniforms;",
    "@group(0) @binding(9) var iblIrradiance: texture_cube<f32>;",
    "@group(0) @binding(10) var iblRadiance: texture_cube<f32>;",
    "@group(0) @binding(11) var iblBRDFLUT: texture_2d<f32>;",
    "@group(0) @binding(12) var iblSampler: sampler;",
    "@group(0) @binding(13) var envMapTex: texture_2d<f32>;",
    "@group(0) @binding(14) var envMapSampler: sampler;",
    "",
    // Group 1: per-material
    "@group(1) @binding(0) var<uniform> material: MaterialUniforms;",
    SCENE_WEBGPU_CUTAWAY_DECL_WGSL,
    "@group(1) @binding(1) var albedoTex: texture_2d<f32>;",
    "@group(1) @binding(2) var albedoSamp: sampler;",
    "@group(1) @binding(3) var normalTex: texture_2d<f32>;",
    "@group(1) @binding(4) var normalSamp: sampler;",
    "@group(1) @binding(5) var roughnessTex: texture_2d<f32>;",
    "@group(1) @binding(6) var roughnessSamp: sampler;",
    "@group(1) @binding(7) var metalnessTex: texture_2d<f32>;",
    "@group(1) @binding(8) var metalnessSamp: sampler;",
    "@group(1) @binding(9) var emissiveTex: texture_2d<f32>;",
    "@group(1) @binding(10) var emissiveSamp: sampler;",
    "@group(1) @binding(11) var occlusionTex: texture_2d<f32>;",
    "@group(1) @binding(12) var occlusionSamp: sampler;",
    "",
    // KHR specular-intensity map: the ALPHA channel is a linear intensity
    // multiplier. A missing, still-loading or failed map stays neutral via
    // the hasSpecularIntensityMap gate in the fragment body.
    "@group(1) @binding(13) var specularIntensityTex: texture_2d<f32>;",
    "@group(1) @binding(14) var specularIntensitySamp: sampler;",
    "",
    // KHR specular-color map: the RGB channels are the sRGB-encoded
    // authored specular tint. A missing, still-loading or failed map stays
    // neutral via the hasSpecularColorMap gate in the fragment body.
      "@group(1) @binding(15) var specularColorTex: texture_2d<f32>;",
      "@group(1) @binding(16) var specularColorSamp: sampler;",
      "@group(1) @binding(17) var thicknessTex: texture_2d<f32>;",
      "@group(1) @binding(18) var thicknessSamp: sampler;",
      "",
    // Cascaded shadow sampling (webgpu-shadows module).
    WGSL_SHADOW_CASCADE_FNS,
    "",
    "fn rotateEnvY(dir: vec3f, radians: f32) -> vec3f {",
    "    let c = cos(radians);",
    "    let s = sin(radians);",
    "    return vec3f(dir.x * c + dir.z * s, dir.y, -dir.x * s + dir.z * c);",
    "}",
    "",
    // Equirectangular direction-to-UV mapping. Ported verbatim from the GLSL
    // source (16-scene-webgl.js envEquirectUV) so both backends sample the
    // same texel for the same direction.
    "fn envEquirectUV(dir: vec3f) -> vec2f {",
    "    let d = normalize(dir);",
    "    return vec2f(atan2(d.z, d.x) / (2.0 * PI) + 0.5, asin(clamp(d.y, -1.0, 1.0)) / PI + 0.5);",
    "}",
    "",
    // Point light distance attenuation.
    "fn pointLightAttenuation(distance: f32, range: f32, decay: f32) -> f32 {",
    "    if (range > 0.0) {",
    "        let ratio = clamp(1.0 - pow(distance / range, 4.0), 0.0, 1.0);",
    "        return ratio * ratio / max(distance * distance, 0.0001);",
    "    }",
    "    return 1.0 / max(pow(distance, decay), 0.0001);",
    "}",
    "",
    // Spot cone falloff. Ported from the WebGL2 renderer so a spot light shades
    // the same on both GPU backends. L points from the surface toward the light;
    // spotDir is the direction the light shines. These comments stay on the JS
    // side on purpose: text inside a WGSL string ships in the bundle, a JS
    // comment does not.
    "fn spotConeAttenuation(L: vec3f, spotDir: vec3f, angle: f32, penumbra: f32) -> f32 {",
    "    let cosAngle = dot(L, -normalize(spotDir));",
    "    let outerCos = cos(angle);",
    "    let innerCos = cos(angle * (1.0 - penumbra));",
    "    return clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0);",
    "}",
    "",
    "fn gsxSpecularRoughness(n: vec3f, r: f32, aa: vec2f) -> f32 {",
    "    let dx = dpdx(n); let dy = dpdy(n);",
    "    let kernel = min(2.0 * aa.x * (dot(dx, dx) + dot(dy, dy)), aa.y);",
    "    if (kernel > 0.0) { return sqrt(sqrt(min(r*r*r*r + kernel, 1.0))); }",
    "    return r;",
    "}",
    WGSL_BRDF,
    WGSL_TRANSMISSION,
    "fn gosxPBRColor(in: VertexOutput) -> vec4f {",
    SCENE_WEBGPU_CUTAWAY_DISCARD_WGSL,
    // Resolve material properties, sampling textures when available.
    "    var albedo = material.albedo;",
    "    var texAlpha = 1.0;",
    "    if (material.hasAlbedoMap != 0u) {",
    "        let texAlbedo = textureSample(albedoTex, albedoSamp, in.uv);",
    "        albedo = albedo * texAlbedo.rgb;",
    "        texAlpha = texAlbedo.a;",
    "    }",
    "    albedo = albedo * in.instanceColor.rgb;",
    "    let finalOpacity = material.opacity * clamp(in.instanceColor.a, 0.0, 1.0);",
    "    let cutoff = material.alphaCutoff;",
    "    let alphaEnabled = cutoff >= 0.0;",
    "    let coverage = finalOpacity * texAlpha;",
    "    let blendOpacity = select(finalOpacity, coverage, cutoff < -1.5);",
    "",
    "    var roughness = material.roughness;",
    "    if (material.hasRoughnessMap != 0u) {",
    "        roughness = roughness * textureSample(roughnessTex, roughnessSamp, in.uv).g;",
    "    }",
    "    roughness = clamp(roughness, 0.04, 1.0);",
    "    roughness = clamp(roughness * (1.0 - abs(material.anisotropy) * 0.28), 0.04, 1.0);",
    "    roughness = gsxSpecularRoughness(normalize(in.normal), roughness, vec2f(material.specularAAVariance, material.volume.w));",
    "    let volumePathLength = volumeThickness(in.uv);",
    "",
    "    var metalness = material.metalness;",
    "    if (material.hasMetalnessMap != 0u) {",
    "        metalness = metalness * textureSample(metalnessTex, metalnessSamp, in.uv).b;",
    "    }",
    "    metalness = clamp(metalness, 0.0, 1.0);",
    "",
    "    var ambientOcclusion = 1.0;",
    "    if (material.hasOcclusionMap != 0u) {",
    // KHR occlusion contract: occludedColor = mix(color, color * sample,
    // strength), matching the WebGL2 renderer.
    "        let occlusionSample = clamp(textureSample(occlusionTex, occlusionSamp, in.uv).r, 0.0, 1.0);",
    "        ambientOcclusion = mix(1.0, occlusionSample, clamp(material.occlusionStrength, 0.0, 1.0));",
    "    }",
    "",
    // Emission: when the material carries an authored emissive colour
    // factor (material.hasEmissiveColor — every glTF-imported material sets
    // this, even to black) emission is emissiveFactor * emissiveTexture(if
    // any), times the KHR_materials_emissive_strength multiplier
    // (material.emissive) — NEVER albedo. Hand-authored materials that only
    // set the scalar `emissive` glow knob keep their pre-existing
    // albedo-tinted glow (hasEmissiveColor == 0u), matching the WebGL2
    // renderer.
    "    let emissiveStrength = material.emissive;",
    "    var emissiveColor: vec3f;",
    "    if (material.hasEmissiveColor != 0u) {",
    "        emissiveColor = material.emissiveColor;",
    "        if (material.hasEmissiveMap != 0u) {",
    "            emissiveColor = emissiveColor * textureSample(emissiveTex, emissiveSamp, in.uv).rgb;",
    "        }",
    "    } else if (material.hasEmissiveMap != 0u) {",
    "        emissiveColor = textureSample(emissiveTex, emissiveSamp, in.uv).rgb;",
    "    } else {",
    "        emissiveColor = albedo;",
    "    }",
    "",
    // Unlit path: output albedo directly.
    "    if (material.unlit != 0u) {",
    "        let color = albedo + emissiveColor * emissiveStrength;",
    "        // Alpha-cut discard after sampling to keep derivative uniformity.",
    "        if (alphaEnabled && coverage < cutoff) { discard; }",
    "        return vec4f(color, select(blendOpacity, 1.0, alphaEnabled));",
    "    }",
    "",
    // Resolve per-pixel normal via TBN matrix.
    "    var N = normalize(in.normal);",
    "    if (material.hasNormalMap != 0u) {",
    "        let T = normalize(in.tangent);",
    "        let B = normalize(in.bitangent);",
    "        let TBN = mat3x3f(T, B, N);",
    "        var mapNormal = textureSample(normalTex, normalSamp, in.uv * vec2f(material.modelScaleSigns.w, material.normalUVScaleV)).rgb * 2.0 - 1.0;",
    "        mapNormal = vec3f(mapNormal.xy * material.normalScale, mapNormal.z);",
    "        N = normalize(TBN * mapNormal);",
    "    }",
    "    let surfaceClearcoat = material.clearcoat; let surfaceClearcoatRoughness = 0.04; let surfaceSheen = material.sheen; let surfaceTransmission = material.transmission;",
    "    let V = normalize(frame.cameraPos - in.worldPos);",
    "    let NoV = max(dot(N, V), 0.0);",
      "",
      // Fresnel reflectance at normal incidence — the material's authored
      // dielectric F0 = ((ior-1)/(ior+1))^2 blended with metallic albedo.
      // Defaults to 0.04 (ior 1.5); 1.0 in the glTF ior=0 compatibility mode.
      // Direct and environment consumers share this single F0.
      // The authored KHR specular factors refine the dielectric lane:
      // material.specularF0 is min(IOR F0 * linear colour, 1) * intensity and
      // material.specularF90 is the intensity itself, both prepared CPU-side
      // so the packed buffer is always finite and bounded to [0, 1]. The
      // metallic mix keeps its exact fully-metal branch so a metal never
      // reads the dielectric lane.
      // A bound specular-intensity texture multiplies its linear ALPHA into
      // both shared factors BEFORE the metallic mix, so direct, scalar
      // diffuse, IBL and equirect consumers all see the updated values.
      "    var specIntensity = 1.0;",
      "    if (material.hasSpecularIntensityMap != 0u) {",
      "        specIntensity = textureSample(specularIntensityTex, specularIntensitySamp, in.uv).a;",
      "    }",
      "    var specF0 = material.specularF0 * specIntensity;",
      "    let specF90 = material.specularF90 * specIntensity;",
    "    if (material.hasSpecularColorMap != 0u) {",
    "        let texColor = textureSample(specularColorTex, specularColorSamp, in.uv).rgb;",
    "        var texF0 = vec3f(0.0);",
    // Per-channel decode: exactly-1 texel keeps the untextured result
    // bit-for-bit; exact-zero texel yields exact-zero F0; positive texel
    // multiplies in log space, clamped to 0 BEFORE exp2 (no overflow).
    // Combined specF90 scales every color-textured channel.
    "        if (texColor.r == 1.0) { texF0.r = specF0.r; }",
      "        else if (texColor.r > 0.0 && material.specularColorLog.r > -1e29) { texF0.r = exp2(min(material.specularColorLog.r + log2(texColor.r), 0.0)) * specF90; }",
      "        if (texColor.g == 1.0) { texF0.g = specF0.g; }",
      "        else if (texColor.g > 0.0 && material.specularColorLog.g > -1e29) { texF0.g = exp2(min(material.specularColorLog.g + log2(texColor.g), 0.0)) * specF90; }",
      "        if (texColor.b == 1.0) { texF0.b = specF0.b; }",
      "        else if (texColor.b > 0.0 && material.specularColorLog.b > -1e29) { texF0.b = exp2(min(material.specularColorLog.b + log2(texColor.b), 0.0)) * specF90; }",
    "        specF0 = texF0;",
      "    }",
      "    var F0 = mix(specF0, albedo, metalness);",
      "    var F90 = mix(specF90, 1.0, metalness);",
      "    if (metalness >= 1.0) {",
      "        F0 = albedo;",
      "        F90 = 1.0;",
      "    }",
    "",
    // Accumulate direct lighting.
    "    let transmission = clamp(surfaceTransmission, 0.0, 1.0) * (1.0 - metalness);",
    "    let gosxLayer = gosxLayerSetup(NoV, roughness, F0, surfaceClearcoat, surfaceClearcoatRoughness);",
    "    var Lo = vec3f(0.0); var indirectLight = vec3f(0.0);",
    "",
    // arrayLength bounds the loop against the storage buffer the JS side sized
    // this frame. No compile-time light cap remains.
    "    let lightCount = min(frame.lightCount, arrayLength(&lights));",
    // Each slot's shadow is sampled once per fragment, outside the light loop. Frames without a shadow slot use wgpuUnshadowedPBRFragment, which drops these calls.
    "    var shadowSlot0 = 1.0; var shadowSlot1 = 1.0;",
    "    if (material.receiveShadow != 0u && shadow.lightIndex.x >= 0) { shadowSlot0 = shadowSlotFactor(shadowMap0, shadowSampler0, shadow.slot0, in.worldPos); }",
    "    if (material.receiveShadow != 0u && shadow.lightIndex.y >= 0) { shadowSlot1 = shadowSlotFactor(shadowMap1, shadowSampler1, shadow.slot1, in.worldPos); }",
    "    for (var i = 0u; i < lightCount; i = i + 1u) {",
    "        let light = lights[i];",
    "        let lightType = u32(light.position.w);",
    "        let lightColor = light.color.rgb;",
    "        let intensity = light.direction.w;",
    "        let range = light.color.a;",
    "        let decay = light.params.x;",
    "",
    // Ambient light (type 0): flat contribution, no BRDF. A light probe
    // arrives here too; see sceneWebGPULightTypeCode.
    "        if (lightType == 0u) {",
    "            if (env.lightModel == 1u) { indirectLight = indirectLight + lightColor * intensity; } else { Lo = Lo + albedo * lightColor * intensity; }",
    "            continue;",
    "        }",
    "",
    // Hemisphere light (type 4): sky/ground blend driven by normal Y. Matches
    // the WebGL2 renderer.
    "        if (lightType == 4u) {",
    "            let hBlend = N.y * 0.5 + 0.5;",
    "            let hemiColor = mix(light.groundPenumbra.rgb, lightColor, hBlend);",
    "            if (env.lightModel == 1u) { indirectLight = indirectLight + hemiColor * intensity; } else { Lo = Lo + albedo * hemiColor * intensity; }",
    "            continue;",
    "        }",
    "",
    // Rect-area light (type 5): the rectangle's shape drives both terms. The
    // form factor already carries the cosine and the falloff, so the shared
    // BRDF block below does not apply.
    "        if (lightType == 5u) {",
    "            Lo = Lo + rectAreaLightRadiance(light, in.worldPos, N, V, albedo, roughness, metalness, F0, F90, NoV);",
    "            continue;",
    "        }",
    "",
    "        var L: vec3f;",
    "        var attenuation: f32 = 1.0;",
    "",
    "        if (lightType == 1u) {",
    // Directional light.
    "            L = normalize(-light.direction.xyz);",
    // Spot light (type 3): cone falloff times point-light distance falloff.
    "        } else if (lightType == 3u) {",
    "            let toLight = light.position.xyz - in.worldPos;",
    "            let dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "            let cone = spotConeAttenuation(L, light.direction.xyz, light.params.w, light.groundPenumbra.a);",
    "            attenuation = pointLightAttenuation(dist, range, decay) * cone;",
    "        } else {",
    // Point light (type 2).
    "            let toLight = light.position.xyz - in.worldPos;",
    "            let dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "            attenuation = pointLightAttenuation(dist, range, decay);",
    "        }",
    "",
    "        let NdotL = max(dot(N, L), 0.0);",
    // Cook-Torrance GGX with multiscatter energy and the clear-coat layer;
    // the diffuse weight is (1 - maxRGB(dielectric Fresnel)) * (1 - metalness).
    "        let lit = gosxDirectBRDF(N, V, L, albedo, metalness, roughness, F0, F90, specF0, specF90, transmission, gosxLayer);",
    // Shadow attenuation follows the light associated with each slot.
    "        var shadowAtten: f32 = 1.0;",
    "        if (material.receiveShadow != 0u && (lightType == 1u || lightType == 3u)) {",
    "            if (i32(i) == shadow.lightIndex.x) { shadowAtten = shadowSlot0; } else if (i32(i) == shadow.lightIndex.y) { shadowAtten = shadowSlot1; }",
    "        }",
    "",
    "        let radiance = lightColor * intensity * attenuation;",
    "        Lo = Lo + lit.color * radiance * NdotL * shadowAtten;",
    "        gosxSpecularRadiance += lit.specular * radiance * NdotL * shadowAtten;",
    "    }",
    "",
    "    // Assetpipe split-sum IBL, with hemisphere fallback while products load.",
    "    var ambient: vec3f;",
    "    var specularEnvironment = vec3f(0.0);",
    "    if (env.hasIBL != 0u) {",
    "        let ibl = gosxImageLight(N, V, NoV, albedo, metalness, roughness, F0, F90, specF0, specF90, transmission, gosxLayer);",
    "        ambient = ibl.color * env.envIntensity;",
    "        specularEnvironment = ibl.specular * env.envIntensity;",
    "    } else if (env.hasEnvMap != 0u) {",
    // Explicit LOD avoids seam/pole derivative blur and matches WebGL2.
    "        let Nr = rotateEnvY(N, env.envRotation);",
    "        let Rr = rotateEnvY(reflect(-V, N), env.envRotation);",
    "        let maxLod = env.envMapMaxLod;",
    "        let envDiffuse = textureSampleLevel(envMapTex, envMapSampler, envEquirectUV(Nr), maxLod).rgb * albedo;",
    "        let envSpecular = textureSampleLevel(envMapTex, envMapSampler, envEquirectUV(Rr), roughness * maxLod).rgb;",
    "        let Fenv = fresnelSchlickRoughness(NoV, F0, F90, roughness);",
    "        let FdielEnv = fresnelSchlickRoughness(NoV, specF0, specF90, roughness);",
    "        let kDenv = (1.0 - max(FdielEnv.x, max(FdielEnv.y, FdielEnv.z))) * (1.0 - metalness);",
    "        let envLit = gosxCoatOver(GosxLit((kDenv * envDiffuse * (1.0 - transmission) + envSpecular * Fenv * (1.0 - roughness * 0.65)) * env.envIntensity, envSpecular * Fenv * (1.0 - roughness * 0.65) * env.envIntensity), textureSampleLevel(envMapTex, envMapSampler, envEquirectUV(Rr), gosxLayer.coatRoughness * maxLod).rgb * env.envIntensity, NoV, gosxLayer);",
    "        ambient = envLit.color;",
    "        specularEnvironment = envLit.specular;",
    "    } else {",
    "        let hemi = N.y * 0.5 + 0.5;",
    "        let envDiffuse = env.ambientColor * env.ambientIntensity",
    "                       + env.skyColor * env.skyIntensity * hemi",
    "                       + env.groundColor * env.groundIntensity * (1.0 - hemi);",
    "        let hemiLit = gosxCoatOver(GosxLit(envDiffuse * albedo * (1.0 - transmission) * select(1.0, 1.0 - metalness, env.lightModel == 1u), vec3f(0.0)), envDiffuse, NoV, gosxLayer);",
    "        ambient = hemiLit.color;",
    "        specularEnvironment = hemiLit.specular;",
    "    }",
    "    if (env.hasIBL == 0u && env.hasEnvMap == 0u) {",
    "        let reflectedTransmission = transmission * transmissionEnvironment(reflect(-V, N), roughness) * fresnelSchlickRoughness(NoV, specF0, specF90, roughness);",
    "        ambient += reflectedTransmission;",
    "        specularEnvironment += reflectedTransmission;",
    "    }",
    "    if (env.lightModel == 1u) { ambient = ambient + indirectLight * albedo * (1.0 - metalness) * (1.0 - transmission); } ambient = ambient * ambientOcclusion;",
    "    gosxSpecularRadiance += specularEnvironment * ambientOcclusion;",
    "",
    // Emissive contribution.
    "    let emission = emissiveColor * emissiveStrength;",
    "",
    "    var color = ambient + Lo + emission;",
    "",
    // Rim light: a fresnel-style glancing-angle highlight, off by default
    // (material.rimStrength == 0 skips the pow()). Additive, matching the
    // WebGL2 renderer.
    "    if (material.rimStrength > 0.0001) {",
    "        let rim = pow(clamp(1.0 - NoV, 0.0, 1.0), max(material.rimPower, 0.0001)) * material.rimStrength;",
    "        color = color + material.rimColor * rim;",
    "    }",
    "",
    "    let sheen = clamp(surfaceSheen, 0.0, 1.0);",
    "    if (sheen > 0.0001) {",
    "        let velvet = pow(1.0 - NoV, 3.0) * sheen;",
    "        color = color + albedo * velvet * 0.55;",
    "    }",
    "",
    "    let iridescence = clamp(material.iridescence, 0.0, 1.0);",
    "    if (iridescence > 0.0001) {",
    "        let iri = vec3f(0.5) + vec3f(0.5) * cos(vec3f(0.0, 2.1, 4.2) + vec3f(NoV * 8.0));",
    "        color = mix(color, color * (vec3f(0.65) + iri * 0.7), iridescence * pow(1.0 - NoV, 2.0));",
    "    }",
    "",
    "    if (transmission > 0.0001) {",
    "        let Ft = fresnelSchlickRoughness(NoV, specF0, specF90, roughness);",
    "        color = color + transmission * ( vec3f(1.0) - Ft) * volumeTransmission(in.worldPos, N, V, roughness, volumePathLength);",
    "    }",
    "",
    // Exponential fog.
    "    if (fog.hasFog != 0u) {",
    "        let fogDist = length(in.worldPos - frame.cameraPos);",
    "        let fogFactor = exp(-fog.fogDensity * fog.fogDensity * fogDist * fogDist);",
    "        color = mix(fog.fogColor, color, clamp(fogFactor, 0.0, 1.0));",
    "        gosxSpecularRadiance *= clamp(fogFactor, 0.0, 1.0);",
    "    }",
    "",
    // Mode 4 hands linear scene-referred values to the post chain. Every
    // direct-to-display mode applies its authored curve followed by the
    // display transfer exactly once; filmic already returns display values.
    "    if (frame.toneMap != 4u) {",
    "        if (frame.toneMap == 1u) {",
    "            color = (color * (2.51 * color + vec3f(0.03))) / (color * (2.43 * color + vec3f(0.59)) + vec3f(0.14));",
    "        } else if (frame.toneMap == 2u) {",
    "            color = color / (color + vec3f(1.0));",
    "        } else if (frame.toneMap == 3u) {",
    "            let hejl = max(vec3f(0.0), color - vec3f(0.004));",
    "            color = clamp((hejl * (6.2 * hejl + vec3f(0.5))) / (hejl * (6.2 * hejl + vec3f(1.7)) + vec3f(0.06)), vec3f(0.0), vec3f(1.0));",
    "        }",
    "        if (frame.toneMap != 3u) {",
    "            color = pow(max(color, vec3f(0.0)), vec3f(1.0 / 2.2));",
    "        }",
    "    }",
    "",
    "    if (alphaEnabled && coverage < cutoff) { discard; }",
    "    return vec4f(color, select(blendOpacity, 1.0, alphaEnabled));",
    "}",
    "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f { return gosxPBRColor(in); }",
    "struct GosxSpecularOutput { @location(0) color: vec4f, @location(1) specular: vec4f };",
    "@fragment fn fragmentMainSpecular(in: VertexOutput) -> GosxSpecularOutput {",
    "  let color = gosxPBRColor(in); return GosxSpecularOutput(color, vec4f(max(gosxSpecularRadiance, vec3f(0)), color.a));",
    "}",
  ].join("\n");

  function sceneWaterObjectMeshFragmentSource(texturePassMode) {
    var mode = Math.max(1, Math.min(2, Math.floor(sceneNumber(texturePassMode, 1))));
    return [
      WGSL_MATERIAL_STRUCT,
      "",
      "struct VertexOutput {",
      "    @builtin(position) clipPos: vec4f,",
      "    @location(0) worldPos: vec3f,",
      "    @location(1) normal: vec3f,",
      "    @location(2) uv: vec2f,",
      "    @location(3) tangent: vec3f,",
      "    @location(4) bitangent: vec3f,",
      "    @location(5) @interpolate(flat) instanceColor: vec4f,",
      "};",
      "",
      "@group(1) @binding(0) var<uniform> material: MaterialUniforms;",
      "@group(1) @binding(1) var albedoTex: texture_2d<f32>;",
      "@group(1) @binding(2) var albedoSamp: sampler;",
      "@group(1) @binding(9) var emissiveTex: texture_2d<f32>;",
      "@group(1) @binding(10) var emissiveSamp: sampler;",
      "",
      "@fragment fn fragmentMain(in: VertexOutput) -> @location(0) vec4f {",
      "    let texturePassMode = " + mode + "u;",
      "    if (texturePassMode == 2u && in.worldPos.y < 0.0) { discard; }",
      "    var albedo = material.albedo;",
      "    var texAlpha = 1.0;",
      "    if (material.hasAlbedoMap != 0u) {",
      "        let texel = textureSample(albedoTex, albedoSamp, in.uv);",
      "        albedo = albedo * texel.rgb;",
      "        texAlpha = texel.a;",
      "    }",
      "    albedo = albedo * in.instanceColor.rgb;",
      "    var emissiveColor = albedo;",
      "    if (material.hasEmissiveMap != 0u) {",
      "        emissiveColor = textureSample(emissiveTex, emissiveSamp, in.uv).rgb;",
      "    }",
      "    let normal = normalize(in.normal);",
      "    let upLight = clamp(normal.y * 0.5 + 0.5, 0.0, 1.0);",
      "    let rim = pow(1.0 - upLight, 2.0);",
      "    var color = albedo * (0.58 + upLight * 0.34) + emissiveColor * material.emissive;",
      "    if (texturePassMode == 2u) {",
      "        color = mix(color, vec3f(0.62, 0.82, 0.96), 0.18 + rim * 0.24);",
      "    } else {",
      "        color = mix(color, vec3f(0.08, 0.18, 0.26), 0.08);",
      "    }",
      "    let unmaskedOpacity = material.opacity * clamp(in.instanceColor.a, 0.0, 1.0);",
      "    let cutoff = material.alphaCutoff;",
      "    let alphaEnabled = cutoff >= 0.0;",
      "    let coverage = unmaskedOpacity * texAlpha;",
      "    let blendOpacity = select(unmaskedOpacity, coverage, cutoff < -1.5);",
      "    // Alpha-cut discard after sampling; equality survives the cut.",
      "    if (alphaEnabled && coverage < cutoff) { discard; }",
      "    return vec4f(color, select(blendOpacity, 1.0, alphaEnabled));",
      "}",
    ].join("\n");
  }

  // -----------------------------------------------------------------------
  // Shadow Depth Shader (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_SHADOW_VERTEX = [
    "struct ShadowFrameUniforms {",
    "    lightViewProjection: mat4x4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;",
    "",
    "@vertex fn vertexMain(@location(0) position: vec3f) -> @builtin(position) vec4f {",
    "    return shadowFrame.lightViewProjection * vec4f(position, 1.0);",
    "}",
  ].join("\n");

  var WGSL_SHADOW_INSTANCED_VERTEX = [
    WGSL_AFFINE_NORMAL,
    "",
    "struct ShadowFrameUniforms {",
    "    lightViewProjection: mat4x4f,",
    "};",
    "",
    "struct VertexOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) @interpolate(flat) orientation: f32,",
    "};",
    "",
    "struct VertexInput {",
    "    @location(0) position: vec3f,",
    "    @location(4) instanceMatrix0: vec4f,",
    "    @location(5) instanceMatrix1: vec4f,",
    "    @location(6) instanceMatrix2: vec4f,",
    "    @location(7) instanceMatrix3: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> shadowFrame: ShadowFrameUniforms;",
    "",
    "@vertex fn vertexMain(in: VertexInput) -> VertexOutput {",
    "    let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
    "    var out: VertexOutput;",
    "    out.clipPos = shadowFrame.lightViewProjection * model * vec4f(in.position, 1.0);",
    "    out.orientation = gosxAffineNormal(model, vec3f(0.0,0.0,1.0)).w;",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_SHADOW_FRAGMENT = [
    "struct VertexOutput { @location(0) @interpolate(flat) orientation: f32, };",
    "@fragment fn fragmentMain(in: VertexOutput, @builtin(front_facing) frontFacing: bool) {",
    "  if (frontFacing != (in.orientation > 0.0)) { discard; }",
    "}",
  ].join("\n");

  var WGSL_SCENE_COLOR_FRAGMENT = [
    "struct ColorOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "    @location(1) material: vec3f,",
    "};",
    "",
    "@fragment fn fragmentMain(in: ColorOutput) -> @location(0) vec4f {",
    "    var color = in.color;",
    "    var rgb = color.rgb;",
    "    let kind = floor(in.material.x + 0.5);",
    "    let emissive = max(in.material.y, 0.0);",
    "    let tone = clamp(in.material.z, 0.0, 1.0);",
    "    if (kind > 3.5) {",
    "        rgb = rgb * mix(0.78, 1.0, tone);",
    "    } else if (kind > 2.5) {",
    "        rgb = rgb * (1.0 + emissive * 0.75);",
    "    } else if (kind > 1.5) {",
    "        rgb = mix(rgb, vec3f(0.92, 0.98, 1.0), 0.28 + tone * 0.16);",
    "        color.a = color.a * 0.84;",
    "    } else if (kind > 0.5) {",
    "        rgb = mix(rgb, vec3f(0.84, 0.94, 1.0), 0.18 + tone * 0.12);",
    "        color.a = color.a * 0.9;",
    "    } else {",
    "        rgb = rgb * mix(0.9, 1.0, tone);",
    "    }",
    "    return vec4f(clamp(rgb, vec3f(0.0), vec3f(1.0)), clamp(color.a, 0.0, 1.0));",
    "}",
  ].join("\n");

  var WGSL_SCENE_WORLD_COLOR_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct ColorInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) color: vec4f,",
    "    @location(2) material: vec3f,",
    "};",
    "",
    "struct ColorOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "    @location(1) material: vec3f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "",
    "@vertex fn vertexMain(in: ColorInput) -> ColorOutput {",
    "    var out: ColorOutput;",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(in.position, 1.0);",
    "    out.color = in.color;",
    "    out.material = in.material;",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_SCENE_CLIP_COLOR_VERTEX = [
    "struct ColorInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) color: vec4f,",
    "    @location(2) material: vec3f,",
    "};",
    "",
    "struct ColorOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "    @location(1) material: vec3f,",
    "};",
    "",
    "@vertex fn vertexMain(in: ColorInput) -> ColorOutput {",
    "    var out: ColorOutput;",
    "    out.clipPos = vec4f(in.position.xy, in.position.z, 1.0);",
    "    out.color = in.color;",
    "    out.material = in.material;",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_SURFACE_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct SurfaceInput {",
    "    @location(0) position: vec3f,",
    "    @location(1) uv: vec2f,",
    "};",
    "",
    "struct SurfaceOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) uv: vec2f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "",
    "@vertex fn vertexMain(in: SurfaceInput) -> SurfaceOutput {",
    "    var out: SurfaceOutput;",
    "    out.clipPos = frame.projMatrix * frame.viewMatrix * vec4f(in.position, 1.0);",
    "    out.uv = in.uv;",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_SURFACE_FRAGMENT = [
    WGSL_MATERIAL_STRUCT,
    "",
    "struct SurfaceOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) uv: vec2f,",
    "};",
    "",
    "@group(1) @binding(0) var<uniform> material: MaterialUniforms;",
    "@group(1) @binding(1) var albedoTex: texture_2d<f32>;",
    "@group(1) @binding(2) var albedoSamp: sampler;",
    "",
    "@fragment fn fragmentMain(in: SurfaceOutput) -> @location(0) vec4f {",
    "    let sampleColor = textureSample(albedoTex, albedoSamp, in.uv);",
    "    var rgb = sampleColor.rgb * material.albedo;",
    "    rgb = rgb * (1.0 + max(material.emissive, 0.0) * 0.5);",
    "    return vec4f(clamp(rgb, vec3f(0.0), vec3f(1.0)), clamp(sampleColor.a * material.opacity, 0.0, 1.0));",
    "}",
  ].join("\n");

  var WGSL_THICK_LINE_VERTEX = [
    WGSL_FRAME_STRUCTS,
    "",
    "struct ThickLineInput {",
    "    @location(0) positionA: vec3f,",
    "    @location(1) positionB: vec3f,",
    "    @location(2) colorA: vec4f,",
    "    @location(3) colorB: vec4f,",
    "    @location(4) side: f32,",
    "    @location(5) endpoint: f32,",
    "    @location(6) width: f32,",
    "};",
    "",
    "struct ThickLineOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "};",
    "",
    "@group(0) @binding(0) var<uniform> frame: FrameUniforms;",
    "",
    "fn safeNDC(clip: vec4f) -> vec2f {",
    "    return clip.xy / max(clip.w, 0.0001);",
    "}",
    "",
    "@vertex fn vertexMain(in: ThickLineInput) -> ThickLineOutput {",
    "    var out: ThickLineOutput;",
    "    let clipA = frame.projMatrix * frame.viewMatrix * vec4f(in.positionA, 1.0);",
    "    let clipB = frame.projMatrix * frame.viewMatrix * vec4f(in.positionB, 1.0);",
    "    let base = mix(clipA, clipB, clamp(in.endpoint, 0.0, 1.0));",
    "    let viewport = max(vec2f(frame.viewportWidth, frame.viewportHeight), vec2f(1.0));",
    "    let screenA = safeNDC(clipA) * (viewport * 0.5);",
    "    let screenB = safeNDC(clipB) * (viewport * 0.5);",
    "    var dir = screenB - screenA;",
    "    let len = length(dir);",
    "    if (len < 0.0001) {",
    "        dir = vec2f(1.0, 0.0);",
    "    } else {",
    "        dir = dir / len;",
    "    }",
    "    let normal = vec2f(-dir.y, dir.x);",
    "    let pixelOffset = normal * (in.side * max(in.width, 1.0) * 0.5);",
    "    let ndcOffset = pixelOffset / max(viewport * 0.5, vec2f(0.0001));",
    "    out.clipPos = base + vec4f(ndcOffset * base.w, 0.0, 0.0);",
    "    out.color = mix(in.colorA, in.colorB, clamp(in.endpoint, 0.0, 1.0));",
    "    return out;",
    "}",
  ].join("\n");

  var WGSL_THICK_LINE_FRAGMENT = [
    "struct ThickLineOutput {",
    "    @builtin(position) clipPos: vec4f,",
    "    @location(0) color: vec4f,",
    "};",
    "",
    "@fragment fn fragmentMain(in: ThickLineOutput) -> @location(0) vec4f {",
    "    return vec4f(clamp(in.color.rgb, vec3f(0.0), vec3f(1.0)), clamp(in.color.a, 0.0, 1.0));",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // Post-processing shaders (WGSL)
  // -----------------------------------------------------------------------

  var WGSL_POST_VERTEX = [
    "struct VertexOutput {",
    "    @builtin(position) position: vec4f,",
    "    @location(0) uv: vec2f,",
    "};",
    "",
    "const positions = array<vec2f, 4>(",
    "    vec2f(-1.0, -1.0),",
    "    vec2f( 1.0, -1.0),",
    "    vec2f(-1.0,  1.0),",
    "    vec2f( 1.0,  1.0),",
    ");",
    "const uvs = array<vec2f, 4>(",
    "    vec2f(0.0, 1.0),",
    "    vec2f(1.0, 1.0),",
    "    vec2f(0.0, 0.0),",
    "    vec2f(1.0, 0.0),",
    ");",
    "",
    "@vertex fn vertexMain(@builtin(vertex_index) vi: u32) -> VertexOutput {",
    "    var out: VertexOutput;",
    "    out.position = vec4f(positions[vi], 0.0, 1.0);",
    "    out.uv = uvs[vi];",
    "    return out;",
    "}",
  ].join("\n");
  var WGSL_POST_BLIT_FRAGMENT = [
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    return textureSample(inputTex, inputSamp, uv);",
    "}",
  ].join("\n");
  var WGSL_POST_TONEMAPPING_FRAGMENT = [
    "struct ToneMappingParams {",
    "    exposure: f32,",
    "    toneMapMode: f32,",
    "    _pad1: f32,",
    "    _pad2: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: ToneMappingParams;",
    "",
    "fn aces(x: vec3f) -> vec3f {",
    "    let a = 2.51;",
    "    let b = 0.03;",
    "    let c = 2.43;",
    "    let d = 0.59;",
    "    let e = 0.14;",
    "    return clamp((x * (a * x + b)) / (x * (c * x + d) + e), vec3f(0.0), vec3f(1.0));",
    "}",
    "",
    "fn reinhard(x: vec3f) -> vec3f {",
    "    return x / (x + vec3f(1.0));",
    "}",
    "",
    "fn filmic(x: vec3f) -> vec3f {",
    "    let y = max(vec3f(0.0), x - vec3f(0.004));",
    "    return clamp((y * (6.2 * y + vec3f(0.5))) / (y * (6.2 * y + vec3f(1.7)) + vec3f(0.06)), vec3f(0.0), vec3f(1.0));",
    "}",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let inputColor = textureSample(inputTex, inputSamp, uv);",
    "    var color = inputColor.rgb;",
    "    color = color * params.exposure;",
    "    let mode = i32(params.toneMapMode);",
    "    if (mode == 0) {",
    "        color = clamp(color, vec3f(0.0), vec3f(1.0));",
    "    } else if (mode == 2) {",
    "        color = reinhard(color);",
    "    } else if (mode == 3) {",
    "        color = filmic(color);",
    "    } else {",
    "        color = aces(color);",
    "    }",
    "    if (mode != 3) {",
    "        color = pow(max(color, vec3f(0.0)), vec3f(1.0 / 2.2));",
    "    }",
    "    return vec4f(color, inputColor.a);",
    "}",
  ].join("\n");
  function sceneWebGPUToneMapMode(mode) {
    if (typeof mode === "string") {
      var normalized = mode.trim().toLowerCase();
      if (normalized === "linear" || normalized === "none") return 0;
      if (normalized === "reinhard") return 2;
      if (normalized === "filmic") return 3;
    }
    return 1;
  }
  var WGSL_POST_BLOOM_BRIGHT_FRAGMENT = [
    "struct BloomBrightParams {",
    "    threshold: f32,",
    "    _pad0: f32,",
    "    _pad1: f32,",
    "    _pad2: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: BloomBrightParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    let brightness = dot(color, vec3f(0.2126, 0.7152, 0.0722));",
    // Soft knee, not a hard cut. A hard cut is discontinuous at the authored
    // threshold: a pixel one part in a thousand below it contributes nothing
    // and the same pixel one part above contributes its whole colour, so a slow
    // camera move snaps a highlight on. The knee crosses zero smoothly.
    //
    // This matches brightPassWGSL in render/bundle/bloom.go, which already used
    // the knee. render/bundle/postfx_drift_test.go records the divergence and
    // its brightDivergentTerms row retires when this lands.
    "    let excess = max(brightness - params.threshold, 0.0);",
    "    return vec4f(color * (excess / (excess + 1.0)), 1.0);",
    "}",
  ].join("\n");
  // -----------------------------------------------------------------------
  // shader-f16 in the post chain
  // -----------------------------------------------------------------------
  //
  // Half precision cuts register pressure and lets a GPU issue two lanes of
  // arithmetic per cycle. The win lands on the integrated and mobile parts where
  // GoSX is weakest, and it lands hardest on a shader that is a weighted average
  // of many texture taps.
  //
  // The post textures use RGBA16float. Blur and FXAA use bounded weights,
  // while tone mapping, bright extraction, and depth reconstruction stay f32.
  // Tests cover HDR inputs through the half and full precision variants.
  //
  // Where it is NOT safe, and why these shaders stay f32:
  //
  //   tone mapping   multiplies by exposure BEFORE clamping, so it is the one
  //                  post stage that handles values above 1 by design.
  //   SSAO and DOF   reconstruct view-space position from depth. A depth value
  //                  near the far plane loses metres of precision in f16.
  //   PBR lighting   sums an unbounded number of lights into an HDR colour, and
  //                  the GGX distribution divides by alpha squared. At roughness
  //                  0.02 that term is 1.6e-7, below the smallest normal f16
  //                  (6.1e-5), so it underflows to zero and the highlight
  //                  vanishes. This is the precision hazard f16 is famous for.
  //
  // vignette, colour grading, the bloom bright pass and the bloom composite are
  // one or two taps each. They are safe, but a single tap is not bandwidth bound,
  // so converting them would add bytes and buy nothing measurable.
  //
  // The variant is a two-line preamble. The body is written once against the
  // aliases, so there is no second copy of the shader to keep in step.
  function sceneWebGPUPostPrecisionPreamble(useF16) {
    if (!useF16) {
      return ["alias pf = f32;", "alias pf3 = vec3f;", ""];
    }
    return ["enable f16;", "alias pf = f16;", "alias pf3 = vec3h;", ""];
  }
  function sceneWebGPUPostShaderSource(bodyLines, useF16) {
    return sceneWebGPUPostPrecisionPreamble(useF16).concat(bodyLines).join("\n");
  }

  var WGSL_POST_BLUR_BODY = [
    "struct BlurParams {",
    "    direction: vec2f,",
    "    radius: f32,",
    "    _pad0: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: BlurParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let texDim = vec2f(textureDimensions(inputTex));",
    "    let texelSize = 1.0 / texDim;",
    "    var result = pf3(textureSample(inputTex, inputSamp, uv).rgb) * pf(0.227027);",
    "",
    "    let weights = vec4<pf>(pf(0.1945946), pf(0.1216216), pf(0.054054), pf(0.016216));",
    "    let radiusStep = clamp(params.radius * 0.35, 1.0, 4.0);",
    "",
    "    for (var i = 0u; i < 4u; i = i + 1u) {",
    "        let offset = params.direction * texelSize * f32(i + 1u) * radiusStep;",
    "        result = result + pf3(textureSample(inputTex, inputSamp, uv + offset).rgb) * weights[i];",
    "        result = result + pf3(textureSample(inputTex, inputSamp, uv - offset).rgb) * weights[i];",
    "    }",
    "    return vec4f(vec3f(result), 1.0);",
    "}",
  ];

  var WGSL_POST_BLUR_FRAGMENT = sceneWebGPUPostShaderSource(WGSL_POST_BLUR_BODY, false);
  var WGSL_POST_BLUR_FRAGMENT_F16 = sceneWebGPUPostShaderSource(WGSL_POST_BLUR_BODY, true);

  var WGSL_POST_BLOOM_COMPOSITE_FRAGMENT = [
    "struct BloomCompositeParams {",
    "    intensity: f32,",
    "    _pad0: f32,",
    "    _pad1: f32,",
    "    _pad2: f32,",
    "};",
    "",
    "@group(0) @binding(0) var sceneTex: texture_2d<f32>;",
    "@group(0) @binding(1) var sceneSamp: sampler;",
    "@group(0) @binding(2) var bloomTex: texture_2d<f32>;",
    "@group(0) @binding(3) var bloomSamp: sampler;",
    "@group(0) @binding(4) var<uniform> params: BloomCompositeParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let scene = textureSample(sceneTex, sceneSamp, uv).rgb;",
    "    let bloom = textureSample(bloomTex, bloomSamp, uv).rgb;",
    "    return vec4f(scene + bloom * params.intensity, 1.0);",
    "}",
  ].join("\n");

  var WGSL_POST_VIGNETTE_FRAGMENT = [
    "struct VignetteParams {",
    "    intensity: f32,",
    "    _pad0: f32,",
    "    _pad1: f32,",
    "    _pad2: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: VignetteParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    let center = uv - 0.5;",
    "    let dist = length(center);",
    "    let vignette = 1.0 - smoothstep(0.3, 0.7, dist * params.intensity);",
    "    return vec4f(color * vignette, 1.0);",
    "}",
  ].join("\n");

  var WGSL_POST_COLORGRADE_FRAGMENT = [
    "struct ColorGradeParams {",
    "    exposure: f32,",
    "    contrast: f32,",
    "    saturation: f32,",
    "    _pad0: f32,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var<uniform> params: ColorGradeParams;",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    var color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    color = color * params.exposure;",
    "    color = mix(vec3f(0.5), color, params.contrast);",
    "    let gray = dot(color, vec3f(0.2126, 0.7152, 0.0722));",
    "    color = mix(vec3f(gray), color, params.saturation);",
    "    return vec4f(clamp(color, vec3f(0.0), vec3f(1.0)), 1.0);",
    "}",
  ].join("\n");

  // FXAA 3.11 quality-preset edge anti-aliasing — the chain-end pass. Uses
  // the same 2-binding (texture + sampler) layout as WGSL_POST_BLIT_FRAGMENT
  // (getPostBlitLayout) since FXAA has no tunable uniforms. Algorithm
  // mirrors the native render/bundle FXAA pass (render/bundle/postfx.go,
  // fxaa311WGSLTemplate) for cross-backend parity: green-channel luma edge
  // detection, local contrast search, 2-tap + 2-tap subpixel blend.
  // FXAA reads nine taps and blends four more. Every tap is an 8-bit UNORM in
  // [0, 1] and every blend weight is at most 1, so half precision is exact
  // against the storage format. The edge-direction vector stays f32: it is
  // divided by a reduction term as small as 1/128, and its result is multiplied
  // by the texel size, so a half-precision reciprocal would quantize the sample
  // offsets and shift where FXAA looks.
  var WGSL_POST_FXAA_BODY = [
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "",
    "fn greenLuma(c: pf3) -> pf {",
    "    return c.g;",
    "}",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let texDim = vec2f(textureDimensions(inputTex));",
    "    let texelSize = 1.0 / texDim;",
    "",
    "    let rgbNW = pf3(textureSample(inputTex, inputSamp, uv + vec2f(-1.0, -1.0) * texelSize).rgb);",
    "    let rgbNE = pf3(textureSample(inputTex, inputSamp, uv + vec2f( 1.0, -1.0) * texelSize).rgb);",
    "    let rgbSW = pf3(textureSample(inputTex, inputSamp, uv + vec2f(-1.0,  1.0) * texelSize).rgb);",
    "    let rgbSE = pf3(textureSample(inputTex, inputSamp, uv + vec2f( 1.0,  1.0) * texelSize).rgb);",
    "    let rgbM  = pf3(textureSample(inputTex, inputSamp, uv).rgb);",
    "",
    "    let lumaNW = greenLuma(rgbNW);",
    "    let lumaNE = greenLuma(rgbNE);",
    "    let lumaSW = greenLuma(rgbSW);",
    "    let lumaSE = greenLuma(rgbSE);",
    "    let lumaM  = greenLuma(rgbM);",
    "",
    "    let lumaMin = min(lumaM, min(min(lumaNW, lumaNE), min(lumaSW, lumaSE)));",
    "    let lumaMax = max(lumaM, max(max(lumaNW, lumaNE), max(lumaSW, lumaSE)));",
    "",
    "    var dir = vec2f(",
    "        -f32((lumaNW + lumaNE) - (lumaSW + lumaSE)),",
    "         f32((lumaNW + lumaSW) - (lumaNE + lumaSE)),",
    "    );",
    "",
    "    let reduceMul = 1.0 / 8.0;",
    "    let reduceMin = 1.0 / 128.0;",
    "    let spanMax = 8.0;",
    "    let dirReduce = max(f32(lumaNW + lumaNE + lumaSW + lumaSE) * (0.25 * reduceMul), reduceMin);",
    "    let rcpDirMin = 1.0 / (min(abs(dir.x), abs(dir.y)) + dirReduce);",
    "    dir = clamp(dir * rcpDirMin, vec2f(-spanMax), vec2f(spanMax)) * texelSize;",
    "",
    "    let rgbA = pf(0.5) * (",
    "        pf3(textureSample(inputTex, inputSamp, uv + dir * (1.0 / 3.0 - 0.5)).rgb) +",
    "        pf3(textureSample(inputTex, inputSamp, uv + dir * (2.0 / 3.0 - 0.5)).rgb));",
    "    let rgbB = rgbA * pf(0.5) + pf(0.25) * (",
    "        pf3(textureSample(inputTex, inputSamp, uv + dir * -0.5).rgb) +",
    "        pf3(textureSample(inputTex, inputSamp, uv + dir *  0.5).rgb));",
    "",
    "    let lumaB = greenLuma(rgbB);",
    "    let color = select(rgbB, rgbA, lumaB < lumaMin || lumaB > lumaMax);",
    "    return vec4f(vec3f(color), 1.0);",
    "}",
  ];

  var WGSL_POST_FXAA_FRAGMENT = sceneWebGPUPostShaderSource(WGSL_POST_FXAA_BODY, false);
  var WGSL_POST_FXAA_FRAGMENT_F16 = sceneWebGPUPostShaderSource(WGSL_POST_FXAA_BODY, true);

  var WGSL_POST_SSAO_FRAGMENT = [
    "struct SSAOParams {",
    "    radius: f32,",
    "    intensity: f32,",
    "    bias: f32,",
    "    _pad0: f32,",
    "    texelSize: vec2f,",
    "    _pad1: vec2f,",
    "};",
    "",
    "@group(0) @binding(0) var inputTex: texture_2d<f32>;",
    "@group(0) @binding(1) var inputSamp: sampler;",
    "@group(0) @binding(2) var depthTex: texture_depth_2d;",
    "@group(0) @binding(3) var<uniform> params: SSAOParams;",
    "",
    "fn depthAt(uv: vec2f) -> f32 {",
    "    let dims = vec2f(textureDimensions(depthTex));",
    "    let p = vec2i(clamp(uv * dims, vec2f(0.0), dims - vec2f(1.0)));",
    "    return textureLoad(depthTex, p, 0);",
    "}",
    "",
    "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {",
    "    let color = textureSample(inputTex, inputSamp, uv).rgb;",
    "    let centerDepth = depthAt(uv);",
    "    if (centerDepth >= 0.9999) {",
    "        return vec4f(color, 1.0);",
    "    }",
    "    let offsets = array<vec2f, 8>(",
    "        vec2f(1.0, 0.0), vec2f(-1.0, 0.0), vec2f(0.0, 1.0), vec2f(0.0, -1.0),",
    "        vec2f(0.707, 0.707), vec2f(-0.707, 0.707), vec2f(0.707, -0.707), vec2f(-0.707, -0.707)",
    "    );",
    "    let radius = clamp(params.radius, 1.0, 64.0);",
    "    var occlusion = 0.0;",
    "    for (var i = 0u; i < 8u; i = i + 1u) {",
    "        let sampleDepth = depthAt(uv + offsets[i] * params.texelSize * radius);",
    "        let delta = centerDepth - sampleDepth;",
    "        let range = 1.0 - smoothstep(0.0, 0.035 * radius, abs(delta));",
    "        if (delta > max(params.bias, 0.0001)) {",
    "            occlusion = occlusion + range;",
    "        }",
    "    }",
    "    let ao = 1.0 - clamp((occlusion / 8.0) * clamp(params.intensity, 0.0, 2.0), 0.0, 0.92);",
    "    return vec4f(color * ao, 1.0);",
    "}",
  ].join("\n");

  // -----------------------------------------------------------------------
  // Buffer / Uniform Helpers
  // -----------------------------------------------------------------------

  // Align a byte count up to the specified alignment (typically 256 for uniform buffers).
  function wgpuAlignUp(size, alignment) {
    return Math.ceil(size / alignment) * alignment;
  }

  // Create a GPU buffer with the given usage flags and initial data (or size).
  function wgpuCreateBuffer(device, usage, dataOrSize) {
    var size;
    var mappedAtCreation = false;
    if (typeof dataOrSize === "number") {
      size = wgpuAlignUp(Math.max(dataOrSize, 4), 4);
    } else {
      size = wgpuAlignUp(Math.max(dataOrSize.byteLength, 4), 4);
      mappedAtCreation = true;
    }
    var buffer = device.createBuffer({
      size: size,
      usage: usage,
      mappedAtCreation: mappedAtCreation,
    });
    if (mappedAtCreation) {
      new dataOrSize.constructor(buffer.getMappedRange()).set(dataOrSize);
      buffer.unmap();
    }
    return buffer;
  }

  // -----------------------------------------------------------------------
  // Frustum Plane Extraction (browser-side parity with native cull.go)
  // -----------------------------------------------------------------------
  // extractFrustumPlanesJS + instancePassesCullTest are defined in
  // 11-scene-math.ts (shared by both this WebGPU renderer and 16-scene-webgl.js).
  //
  // This renderer passes scratchSelenaViewProjection (post-depth-remap, WebGPU
  // [0,1] clip convention) so the near=R2 half-depth formula is correct for
  // what the GPU actually clips.

  // -----------------------------------------------------------------------
  // Pipeline Cache
  // -----------------------------------------------------------------------

  // Build a cache key from pipeline configuration parameters.
  function wgpuPipelineKey(shaderVariant, blendMode, depthWrite, targetFormat, depthFormat, sampleCount) {
    return shaderVariant + "|" + blendMode + "|" + (depthWrite ? "1" : "0") + "|" + targetFormat + "|" + (depthFormat || "") + "|" + Math.max(1, Math.floor(sampleCount || 1));
  }

  // -----------------------------------------------------------------------
  // Texture Management
  // -----------------------------------------------------------------------

  // KTX2 block-texture path.
  //
  // 19a-scene-ktx2.ts publishes window.__gosx_scene3d_ktx2 and ships in the
  // lazily fetched glTF chunk, because only a model asset carries a .ktx2
  // texture. This renderer therefore resolves the reader at call time and
  // falls back to the image path when the chunk is absent.
  //
  // Registering wgpuUploadKTX2Texture on
  // window.__gosx_scene3d_ktx2_texture_loader is what opens the variant swap in
  // 19-scene-gltf.js. Until a renderer registers a loader, the swap keeps
  // serving the PNG or JPEG URI, because a .ktx2 URI in an image element is a
  // broken texture.
  function wgpuIsKTX2URL(url) {
    return typeof url === "string" && /\.ktx2(\?|#|$)/.test(url);
  }

  function wgpuKTX2API() {
    return typeof window !== "undefined" && window.__gosx_scene3d_ktx2
      ? window.__gosx_scene3d_ktx2
      : null;
  }
  function wgpuNotifyTextureSettled(record) {
    if (record && typeof notifySceneTextureLoaded === "function") {
      notifySceneTextureLoaded(record.src, Boolean(record.loaded && !record.failed));
    }
    var generation = record && record.generation;
    if (
      !record ||
      record.disposed ||
      !generation ||
      generation.disposed ||
      typeof generation.onResourceReady !== "function"
    ) {
      return;
    }
    generation.onResourceReady();
  }
  // wgpuUploadKTX2Texture fetches, decodes and uploads one KTX2 container into
  // an existing cache record.
  //
  // Every failure marks the record failed and warns. It never leaves the record
  // in the loaded state with the 1x1 placeholder still bound, because a blank
  // texture that reports success is worse than a visible failure: the three
  // failure paths the reader raises are a fetch that does not return 200, a
  // format this device cannot sample, and a level whose byte length disagrees
  // with its block arithmetic.
  // The signature is (context, url, record), the same shape the WebGL2
  // uploader uses, because both are published on the one gate global.
  function wgpuUploadKTX2Texture(device, url, record) {
    var ktx2 = wgpuKTX2API();
    if (!ktx2 || typeof ktx2.load !== "function" || typeof ktx2.uploadWebGPU !== "function") {
      record.failed = true;
      record.pending = false;
      return Promise.resolve(record);
    }
    return ktx2.load(url).then(function(image) {
      var texture = ktx2.uploadWebGPU(device, image, { label: "gosx.ktx2:" + url });
      if (record.disposed || record.generation && record.generation.disposed) {
        if (texture && typeof texture.destroy === "function") texture.destroy();
        return record;
      }
      // Swap only after the upload returns. A throw inside uploadWebGPU lands
      // in the catch below with the placeholder still bound and failed set.
      if (record.texture && typeof record.texture.destroy === "function") {
        record.texture.destroy();
      }
      record.texture = texture;
      record.view = texture.createView({ dimension: image.faces === 6 ? "cube" : "2d" });
      record.width = image.width;
      record.height = image.height;
      record.faces = image.faces;
      record.levels = image.levels.length;
      record.vkFormat = image.vkFormat;
      record.keyValues = image.keyValues || {};
      record.ktx2 = true;
      record.loaded = true;
      record.pending = false;
      wgpuNotifyTextureSettled(record);
      return record;
    }).catch(function(error) {
      if (record.disposed || record.generation && record.generation.disposed) return record;
      record.failed = true;
      record.pending = false;
      record.error = error && error.message ? error.message : String(error);
      try {
        console.warn("[gosx] KTX2 texture " + url + " failed: " + record.error);
      } catch (_e) {}
      wgpuNotifyTextureSettled(record);
      return record;
    });
  }
  function wgpuTextureDescriptor(raw, url, fallbackRole, fallbackColorSpace) {
    var descriptor = raw && typeof raw === "object" ? raw : {};
    return {
      uri: (typeof sceneTextAtlasURL === "function" ? sceneTextAtlasURL : String)(typeof descriptor.uri === "string" && descriptor.uri.trim() ? descriptor.uri.trim() : String(url || "").trim()),
      role: typeof descriptor.role === "string" ? descriptor.role.trim().toLowerCase() : String(fallbackRole || ""),
      colorSpace: typeof descriptor.colorSpace === "string" && descriptor.colorSpace.trim()
        ? descriptor.colorSpace.trim().toLowerCase()
        : String(fallbackColorSpace || "linear"),
      view: typeof descriptor.view === "string" && descriptor.view.trim() ? descriptor.view.trim().toLowerCase() : "2d",
      format: typeof descriptor.format === "string" ? descriptor.format.trim().toLowerCase() : "",
      mipLevels: Math.max(0, Math.floor(sceneNumber(descriptor.mipLevels, 0))),
      width: Math.max(0, Math.floor(sceneNumber(descriptor.width, 0))),
      height: Math.max(0, Math.floor(sceneNumber(descriptor.height, 0))),
      faces: Math.max(0, Math.floor(sceneNumber(descriptor.faces, 0))),
      wrapS: [33071, 33648, 10497].indexOf(descriptor.wrapS) >= 0 ? descriptor.wrapS : ((descriptor.role || fallbackRole) === "normal" ? 10497 : 33071),
      wrapT: [33071, 33648, 10497].indexOf(descriptor.wrapT) >= 0 ? descriptor.wrapT : ((descriptor.role || fallbackRole) === "normal" ? 10497 : 33071),
    };
  }
  function wgpuTextureCacheKey(descriptor) {
    return [
      descriptor.uri,
      descriptor.role,
      descriptor.colorSpace,
      descriptor.view,
      descriptor.format,
      descriptor.width,
      descriptor.height,
      descriptor.faces,
      descriptor.mipLevels,
      descriptor.wrapS,
      descriptor.wrapT,
    ].join("\u0000");
  }
  var wgpuMaterialSamplerCache = new WeakMap();
  function wgpuMaterialSampler(device, linearSampler, rawDescriptor, role) {
    var descriptor = wgpuTextureDescriptor(rawDescriptor, "", role, "linear");
    var cache = wgpuMaterialSamplerCache.get(device);
    if (!cache) {
      cache = new Map();
      wgpuMaterialSamplerCache.set(device, cache);
    }
    var key = descriptor.wrapS + ":" + descriptor.wrapT;
    if (!cache.has(key)) {
      function addressMode(wrap) {
        return wrap === 33071 ? "clamp-to-edge" : (wrap === 33648 ? "mirror-repeat" : "repeat");
      }
      cache.set(key, device.createSampler({
        addressModeU: addressMode(descriptor.wrapS), addressModeV: addressMode(descriptor.wrapT),
        magFilter: "linear", minFilter: "linear", mipmapFilter: "linear", maxAnisotropy: 8,
      }));
    }
    return cache.get(key);
  }
  // Device ownership fences cached pipelines across device-loss recovery.
  var wgpuTextureMipCaches = new WeakMap();
  function wgpuGenerateTextureMips(device: any, texture: any, format: string, levels: number) {
    if (levels <= 1) return Promise.resolve();
    var cache = wgpuTextureMipCaches.get(device);
    if (!cache) wgpuTextureMipCaches.set(device, cache = new Map());
    var resources = cache.get(format);
    var error: any = null;
    device.pushErrorScope("validation");
    try {
      if (!resources) {
        var layout = device.createBindGroupLayout({ entries: [
          { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
          { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
        ] });
        resources = {
          layout: layout,
          sampler: device.createSampler({ minFilter: "linear", magFilter: "linear" }),
          pipeline: device.createRenderPipeline({
            label: "gosx-texture-mips-" + format,
            layout: device.createPipelineLayout({ bindGroupLayouts: [layout] }),
            vertex: { module: device.createShaderModule({ code: WGSL_POST_VERTEX }), entryPoint: "vertexMain" },
            fragment: { module: device.createShaderModule({ code: WGSL_POST_BLIT_FRAGMENT }), entryPoint: "fragmentMain", targets: [{ format: format }] },
            primitive: { topology: "triangle-strip" },
          }),
        };
        cache.set(format, resources);
      }
      var encoder = device.createCommandEncoder({ label: "gosx-texture-mips" });
      for (var level = 1; level < levels; level++) {
        var group = device.createBindGroup({ layout: resources.layout, entries: [
          { binding: 0, resource: texture.createView({ baseMipLevel: level - 1, mipLevelCount: 1 }) },
          { binding: 1, resource: resources.sampler },
        ] });
        var pass = encoder.beginRenderPass({ label: "gosx-texture-mip-" + level, colorAttachments: [{
          view: texture.createView({ baseMipLevel: level, mipLevelCount: 1 }),
          loadOp: "clear", clearValue: [0, 0, 0, 0], storeOp: "store",
        }] });
        pass.setPipeline(resources.pipeline);
        pass.setBindGroup(0, group);
        pass.draw(4);
        pass.end();
      }
      // Queue order keeps mip generation between upload and publication.
      device.queue.submit([encoder.finish()]);
    } catch (failure) { error = failure; }
    // Pop before yielding: concurrent image loads cannot swap device scopes.
    return Promise.resolve(device.popErrorScope()).then(function(failure) {
      if (error || failure) throw error || failure;
    }).catch(function(failure) {
      if (cache.get(format) === resources) cache.delete(format);
      throw failure;
    });
  }
  function wgpuCreateImageTexture(device: any, width: number, height: number, colorSpace: string) {
    var format = colorSpace === "srgb" ? "rgba8unorm-srgb" : "rgba8unorm";
    var levels = Math.floor(Math.log2(Math.max(1, width, height))) + 1;
    return { format: format, levels: levels, texture: device.createTexture({
      size: [width, height, 1], format: format, mipLevelCount: levels,
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
    }) };
  }
  function wgpuTextureRecordDisposed(record: any): boolean { return !!(record.disposed || record.generation && record.generation.disposed); }
  function wgpuEnvMapMaxLod(envMap: any): number {
    return envMap.active ? Math.max(0, sceneNumber(envMap.record && envMap.record.levels, 1) - 1) : 0;
  }
  function wgpuLoadTexture(device, url, cache, rawDescriptor, fallbackRole, fallbackColorSpace, retryFailed = false) {
    if (!cache) return null;
    var descriptor = wgpuTextureDescriptor(rawDescriptor, url, fallbackRole, fallbackColorSpace);
    var key = descriptor.uri;
    if (!key) return null;
    var cacheKey = wgpuTextureCacheKey(descriptor);
    var cached = cache.get(cacheKey);
    if (cached) {
      if (retryFailed && cached.failed && !wgpuTextureRecordDisposed(cached)) {
        cached.failed = false;
        cached.loaded = false;
        cached.pending = true;
        cached.error = undefined;
        wgpuStartTextureLoad(device, cached);
      }
      return cached;
    }
    // Placeholder: 1x1 white pixel.
    var cube = descriptor.view === "cube";
    var placeholderTex = cube ? wgpuCreatePlaceholderCubeTexture(device) : device.createTexture({
      size: [1, 1, 1],
      format: "rgba8unorm",
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
    });
    if (!cube) {
      device.queue.writeTexture(
        { texture: placeholderTex },
        new Uint8Array([255, 255, 255, 255]),
        { bytesPerRow: 4 },
        [1, 1, 1]
      );
    }
    var record = {
      texture: placeholderTex,
      view: placeholderTex.createView({ dimension: cube ? "cube" : "2d" }),
      src: key,
      descriptor: descriptor,
      colorSpace: "",
      levels: 1,
      loaded: false,
      pending: true,
      failed: false,
      generation: cache._gosxGeneration || null,
      disposed: false,
    };
    cache.set(cacheKey, record);
    wgpuStartTextureLoad(device, record);
    return record;
  }
  function wgpuStartTextureLoad(device, record) {
    var descriptor = record.descriptor, key = record.src;
    var cube = descriptor.view === "cube";
    // KTX2 uploads supplied mips through its decoder, outside the Image path.
    if (wgpuIsKTX2URL(key) && wgpuKTX2API()) {
      wgpuUploadKTX2Texture(device, key, record);
      return record;
    }
    if (cube) {
      record.failed = true;
      record.pending = false;
      record.error = "cube descriptors require a KTX2 upload path";
      wgpuNotifyTextureSettled(record);
    } else if (typeof Image === "function") {
      var image = new Image();
      record.image = image;
      image.onload = function() {
        if (record.image !== image || wgpuTextureRecordDisposed(record)) return;
        // Preserve base/specular RGB under transparent pixels and MASK alpha
        // by decoding/copying these roles unpremultiplied.
        var needsUnpremultipliedAlpha = ["specular-color", "base-color"].indexOf(descriptor.role) >= 0;
        var w = image.width;
        var h = image.height;
        if (!w || !h) { image.onerror(""); return; }
        var imageTexture = wgpuCreateImageTexture(device, w, h, descriptor.colorSpace);
        var tex = imageTexture.texture;
        // Canvas makes SVG foreignObject pixels readable by Chromium WebGPU.
        var isHTMLTexture = key.indexOf("data:image/svg+xml") === 0 && key.indexOf("foreignObject") >= 0;
        var htmlCanvas = null;
        if (isHTMLTexture && typeof document !== "undefined" && document.createElement) {
          htmlCanvas = document.createElement("canvas");
          htmlCanvas.width = w; htmlCanvas.height = h;
          var htmlContext = htmlCanvas.getContext("2d");
          if (htmlContext) htmlContext.drawImage(image, 0, 0);
        }
        if (htmlCanvas && htmlContext) {
          var imagePromise = Promise.resolve(htmlCanvas);
        } else if (typeof createImageBitmap === "function") {
          /* @ts-expect-error TS2345 -- premultiplyAlpha widens to string through the ternary; the runtime value is always the literal "none" */ var bitmapOptions = needsUnpremultipliedAlpha ? { premultiplyAlpha: "none" } : undefined;
          imagePromise = createImageBitmap(image, bitmapOptions);
        }
        if (imagePromise) {
          imagePromise.then(async function(bitmap) {
            try {
              if (record.image !== image || wgpuTextureRecordDisposed(record)) { tex.destroy(); return; }
              device.queue.copyExternalImageToTexture(
                { source: bitmap },
                needsUnpremultipliedAlpha || isHTMLTexture ? { texture: tex, premultipliedAlpha: false } : { texture: tex },
                [w, h]
              );
              await wgpuGenerateTextureMips(device, tex, imageTexture.format, imageTexture.levels);
              if (record.image !== image || wgpuTextureRecordDisposed(record)) { tex.destroy(); return; }
              record.texture.destroy();
              record.texture = tex;
              record.view = tex.createView();
              record.levels = imageTexture.levels;
              record.colorSpace = descriptor.colorSpace === "srgb" ? "srgb" : "linear";
              record.loaded = true;
              record.pending = false;
              wgpuNotifyTextureSettled(record);
            } finally {
              if (typeof ImageBitmap !== "undefined" && bitmap instanceof ImageBitmap) bitmap.close();
            }
          }).catch(function() {
            tex.destroy();
            if (record.image !== image || wgpuTextureRecordDisposed(record)) return;
            record.failed = true;
            record.pending = false;
            wgpuNotifyTextureSettled(record);
          });
        } else {
          record.failed = true;
          record.pending = false;
          wgpuNotifyTextureSettled(record);
        }
      };
      image.onerror = function() {
        if (record.image !== image || wgpuTextureRecordDisposed(record)) return;
        record.failed = true;
        record.pending = false;
        wgpuNotifyTextureSettled(record);
      };
      image.crossOrigin = "anonymous";
      image.src = key;
    } else {
      record.failed = true;
      record.pending = false;
      wgpuNotifyTextureSettled(record);
    }
  }
  function wgpuWaterCubeMapFaceURLs(value) {
    var base = typeof value === "string" ? value.trim() : "";
    if (!base) return null;
    if (base.indexOf("{face}") >= 0) {
      return ["xpos", "xneg", "ypos", "ypos", "zpos", "zneg"].map(function(face) {
        return base.replace("{face}", face);
      });
    }
    if (base.charAt(base.length - 1) !== "/") base += "/";
    return ["xpos.jpg", "xneg.jpg", "ypos.jpg", "ypos.jpg", "zpos.jpg", "zneg.jpg"].map(function(face) {
      return base + face;
    });
  }
  function wgpuCreatePlaceholderCubeTexture(device) {
    var tex = device.createTexture({
      size: [1, 1, 6],
      format: "rgba8unorm",
      dimension: "2d",
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST,
    });
    var faces = new Uint8Array([
      150, 190, 210, 255,
      110, 155, 180, 255,
      190, 220, 232, 255,
      190, 220, 232, 255,
      125, 170, 195, 255,
      90, 135, 165, 255,
    ]);
    for (var i = 0; i < 6; i++) {
      device.queue.writeTexture(
        { texture: tex, origin: [0, 0, i] },
        faces.subarray(i * 4, i * 4 + 4),
        { bytesPerRow: 4, rowsPerImage: 1 },
        [1, 1, 1]
      );
    }
    return tex;
  }
  function wgpuLoadCubeTexture(device, value, cache) {
    if (!cache) return null;
    var urls = wgpuWaterCubeMapFaceURLs(value);
    if (!urls) return null;
    var key = "cube:" + urls.join("|");
    if (cache.has(key)) return cache.get(key);
    var placeholder = wgpuCreatePlaceholderCubeTexture(device);
    var record = {
      texture: placeholder,
      view: placeholder.createView({ dimension: "cube" }),
      src: key,
      faces: urls,
      loaded: false,
      pending: true,
      failed: false,
    };
    cache.set(key, record);
    if (typeof Image !== "function" || typeof createImageBitmap !== "function") {
      record.failed = true;
      record.pending = false;
      return record;
    }
    var images = new Array(6);
    var loaded = 0;
    var failed = false;
    function finishIfReady() {
      if (failed || loaded !== 6) return;
      var w = images[0].width;
      var h = images[0].height;
      if (!w || !h) {
        record.failed = true;
        record.pending = false;
        return;
      }
      var tex = device.createTexture({
        size: [w, h, 6],
        format: "rgba8unorm",
        dimension: "2d",
        usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
      });
      Promise.all(images.map(function(image) {
        return createImageBitmap(image);
      })).then(function(bitmaps) {
        bitmaps.forEach(function(bitmap, faceIndex) {
          device.queue.copyExternalImageToTexture(
            { source: bitmap },
            { texture: tex, origin: [0, 0, faceIndex] },
            [w, h]
          );
        });
        record.texture.destroy();
        record.texture = tex;
        record.view = tex.createView({ dimension: "cube" });
        record.loaded = true;
        record.pending = false;
      }).catch(function() {
        record.failed = true;
        record.pending = false;
      });
    }
    urls.forEach(function(url, index) {
      var image = new Image();
      image.onload = function() {
        images[index] = image;
        loaded++;
        finishIfReady();
      };
      image.onerror = function() {
        failed = true;
        record.failed = true;
        record.pending = false;
      };
      image.crossOrigin = "anonymous";
      image.src = url;
    });
    return record;
  }
  // -----------------------------------------------------------------------
  // Bind Group Layout Definitions
  // -----------------------------------------------------------------------
  function wgpuCreateFrameBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-frame",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 3, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth", viewDimension: "2d-array" } },
        { binding: 5, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "comparison" } },
        { binding: 6, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth", viewDimension: "2d-array" } },
        { binding: 7, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "comparison" } },
        { binding: 8, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 9, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "cube" } },
        { binding: 10, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "cube" } },
        { binding: 11, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
        { binding: 12, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
        { binding: 13, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
        { binding: 14, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
        { binding: 15, visibility: GPUShaderStage.FRAGMENT, texture: {} },
        { binding: 16, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 17, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }
  function wgpuCreateMaterialBindGroupLayout(device, models = false) {
    var entries: any[] = [
      { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
    ];
    // Nine texture/sampler pairs retain the material ABI at bindings 1..18.
    for (var binding = 1; binding < 19; binding += 2) entries.push(
      { binding: binding, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
      { binding: binding + 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} });
    if (models) entries.push({ binding: 19, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT,
      buffer: { type: "uniform", hasDynamicOffset: true, minBindingSize: SCENE_WEBGPU_CUTAWAY_SLOT_BYTES } });
    return device.createBindGroupLayout({ label: "gosx-material", entries: entries });
  }
  function wgpuCreatePointsBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-points",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
        { binding: 1, visibility: GPUShaderStage.VERTEX, buffer: { type: "read-only-storage" } },
      ],
    });
  }
  function wgpuCreatePointsUniformBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-points-uniform",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }
  function wgpuCreateShadowBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-shadow-frame",
      entries: [
        { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform", hasDynamicOffset: true, minBindingSize: 64 } },
      ],
    });
  }
  function wgpuCreatePostBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-post",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
      ],
    });
  }
  function wgpuCreatePostWithParamsBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-post-params",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }
  function wgpuCreateBloomCompositeBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-bloom-composite",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 4, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }
  function wgpuCreateSSAOBindGroupLayout(device) {
    return device.createBindGroupLayout({
      label: "gosx-ssao",
      entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
        { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
        { binding: 3, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      ],
    });
  }
  // -----------------------------------------------------------------------
  // Pipeline Creation
  // -----------------------------------------------------------------------
  // PBR vertex buffer layout (position, normal, uv, tangent).
  var WGPU_PBR_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 1 }] },
    { arrayStride: 8,  stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 2 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 3 }] },
  ];
  var WGPU_PBR_INSTANCED_VERTEX_LAYOUT = WGPU_PBR_VERTEX_LAYOUT.concat([
    {
      arrayStride: 64,
      stepMode: "instance",
      attributes: [
        { format: "float32x4", offset: 0,  shaderLocation: 4 },
        { format: "float32x4", offset: 16, shaderLocation: 5 },
        { format: "float32x4", offset: 32, shaderLocation: 6 },
        { format: "float32x4", offset: 48, shaderLocation: 7 },
      ],
    },
    { arrayStride: 16, stepMode: "instance", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 8 }] },
  ]);
  // Cull-path instanced layout: 80-byte InstanceRecord (mat4 @ 0-63, pickData
  // uint32x4 @ 64-79). Location 8 carries pickData (vec4u) instead of the
  // non-cull layout's instanceColor (vec4f). Material color comes from the
  // per-material uniform — fragment does NOT read per-instance vertex color.
  // Matches the native render/bundle/cull.go instanceRecordStride = 80.
  var WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT = WGPU_PBR_VERTEX_LAYOUT.concat([
    {
      arrayStride: 80,
      stepMode: "instance",
      attributes: [
        { format: "float32x4", offset: 0,  shaderLocation: 4 },
        { format: "float32x4", offset: 16, shaderLocation: 5 },
        { format: "float32x4", offset: 32, shaderLocation: 6 },
        { format: "float32x4", offset: 48, shaderLocation: 7 },
        { format: "uint32x4",  offset: 64, shaderLocation: 8 },
      ],
    },
  ]);
  // Shadow vertex buffer layout (position only).
  var WGPU_SHADOW_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
  ];
  var WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT = WGPU_SHADOW_VERTEX_LAYOUT.concat([
    {
      arrayStride: 64,
      stepMode: "instance",
      attributes: [
        { format: "float32x4", offset: 0,  shaderLocation: 4 },
        { format: "float32x4", offset: 16, shaderLocation: 5 },
        { format: "float32x4", offset: 32, shaderLocation: 6 },
        { format: "float32x4", offset: 48, shaderLocation: 7 },
      ],
    },
  ]);
  var WGPU_SCENE_COLOR_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 1 }] },
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 2 }] },
  ];
  var WGPU_SURFACE_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 8, stepMode: "vertex", attributes: [{ format: "float32x2", offset: 0, shaderLocation: 1 }] },
  ];
  var WGPU_THICK_LINE_VERTEX_LAYOUT = [
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 0 }] },
    { arrayStride: 12, stepMode: "vertex", attributes: [{ format: "float32x3", offset: 0, shaderLocation: 1 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 2 }] },
    { arrayStride: 16, stepMode: "vertex", attributes: [{ format: "float32x4", offset: 0, shaderLocation: 3 }] },
    { arrayStride: 4, stepMode: "vertex", attributes: [{ format: "float32", offset: 0, shaderLocation: 4 }] },
    { arrayStride: 4, stepMode: "vertex", attributes: [{ format: "float32", offset: 0, shaderLocation: 5 }] },
    { arrayStride: 4, stepMode: "vertex", attributes: [{ format: "float32", offset: 0, shaderLocation: 6 }] },
  ];
  var WGPU_POINTS_INSTANCE_VERTEX_LAYOUT = [
    {
      arrayStride: 32,
      stepMode: "instance",
      attributes: [
        { format: "float32x3", offset: 0, shaderLocation: 0 },
        { format: "float32", offset: 12, shaderLocation: 1 },
        { format: "float32x4", offset: 16, shaderLocation: 2 },
      ],
    },
  ];
  function wgpuBlendState(mode) {
    if (mode === "alpha") {
      return {
        color: { srcFactor: "src-alpha", dstFactor: "one-minus-src-alpha", operation: "add" },
        alpha: { srcFactor: "one", dstFactor: "one-minus-src-alpha", operation: "add" },
      };
    }
    if (mode === "additive") {
      return {
        color: { srcFactor: "src-alpha", dstFactor: "one", operation: "add" },
        alpha: { srcFactor: "one", dstFactor: "one", operation: "add" },
      };
    }
    return undefined; // opaque -- no blending
  }
  // The sample-count sign carries the determinant-selected front face while
  // its magnitude remains the actual MSAA count. This keeps the governed
  // factory signature at eight parameters without an allocation per variant.
  function wgpuCreatePBRPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, signedSampleCount) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-pbr-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: vertexModule.gosxVertexLayout || WGPU_PBR_VERTEX_LAYOUT,
      },
      fragment: sceneWebGPUColorFragment(fragmentModule, targetFormat, wgpuBlendState(blendMode)),
      primitive: { topology: "triangle-list", cullMode: "none", frontFace: signedSampleCount < 0 ? "cw" : "ccw" },
      multisample: { count: Math.max(1, Math.floor(Math.abs(signedSampleCount) || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }
  function wgpuCreatePBRInstancedPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-pbr-instanced-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_PBR_INSTANCED_VERTEX_LAYOUT,
      },
      fragment: sceneWebGPUColorFragment(fragmentModule, targetFormat, wgpuBlendState(blendMode)),
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }
  // Cull-path pipeline: uses WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT (80-byte
  // InstanceRecord with pickData uint32x4 at location 8). The same fragment
  // module is used — no per-instance color from vertex; fragment reads the
  // per-material uniform. Shadow pipeline is NOT added (shadows stay draw-all).
  function wgpuCreatePBRInstancedCullPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-pbr-instanced-cull-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_PBR_INSTANCED_CULL_VERTEX_LAYOUT,
      },
      fragment: sceneWebGPUColorFragment(fragmentModule, targetFormat, wgpuBlendState(blendMode)),
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }
  // Both shadow pipelines set cullMode "front" and set no frontFace, so the
  // WebGPU default "ccw" stands. 12-scene-geometry.ts and 16c-scene-shared-pbr.ts
  // wind their solids counter-clockwise as seen from outside, so the lit face is
  // front-facing and gets discarded. The map therefore records the far wall of a
  // caster, which pushes the stored depth a caster thickness past the receiver
  // and is the standard mitigation for peter-panning.
  //
  // render/bundle/renderer.go keeps the OPPOSITE face on the native path.
  // render/bundle/shadow_drift_test.go pins both settings and states why they
  // differ. Change either side there, not here alone.
  function wgpuCreateShadowPipeline(device, shadowLayout, vertexModule, frontFace) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-shadow",
      layout: device.createPipelineLayout({ bindGroupLayouts: [shadowLayout] }),
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_SHADOW_VERTEX_LAYOUT,
      },
      primitive: { topology: "triangle-list", cullMode: "front", frontFace: frontFace || "ccw" },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: true,
        depthCompare: "less-equal",
      },
    });
  }
  function wgpuCreateShadowInstancedPipeline(device, shadowLayout, vertexModule, fragmentModule) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-shadow-instanced",
      layout: device.createPipelineLayout({ bindGroupLayouts: [shadowLayout] }),
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT,
      },
      fragment: { module: fragmentModule, entryPoint: "fragmentMain", targets: [] },
      primitive: { topology: "triangle-list", cullMode: "none", frontFace: "ccw" },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: true,
        depthCompare: "less-equal",
      },
    });
  }
  function wgpuCreateSceneColorPipeline(device, pipelineLayout, vertexModule, fragmentModule, topology, blendMode, depthWrite, targetFormat, sampleCount) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-scene-color-" + topology + "-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_SCENE_COLOR_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: topology },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreateSurfacePipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-surface-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_SURFACE_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreateThickLinePipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-thick-line-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_THICK_LINE_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreatePointsPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-points-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: [],
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreatePointsVertexPipeline(device, pipelineLayout, vertexModule, fragmentModule, blendMode, depthWrite, targetFormat, sampleCount) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-points-vertex-" + blendMode,
      layout: pipelineLayout,
      vertex: {
        module: vertexModule,
        entryPoint: "vertexMain",
        buffers: WGPU_POINTS_INSTANCE_VERTEX_LAYOUT,
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{
          format: targetFormat,
          blend: wgpuBlendState(blendMode),
        }],
      },
      primitive: { topology: "triangle-list" },
      multisample: { count: Math.max(1, Math.floor(sampleCount || 1)) },
      depthStencil: {
        format: "depth24plus",
        depthWriteEnabled: depthWrite,
        depthCompare: "less-equal",
      },
    });
  }

  function wgpuCreatePostPipeline(device, layout, fragmentModule, targetFormat, name) {
    var vertModule = device.createShaderModule({ label: "post-vert", code: WGSL_POST_VERTEX });
    return wgpuCreateValidatedPipeline(device, "render", {
      label: "gosx-post" + (name ? "-" + name : ""),
      layout: layout,
      vertex: {
        module: vertModule,
        entryPoint: "vertexMain",
        buffers: [],
      },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{ format: targetFormat }],
      },
      primitive: { topology: "triangle-strip", stripIndexFormat: "uint32" },
    });
  }

  function wgpuIsErrorScopeLifecycleMessage(message) {
    var text = String(message || "").toLowerCase();
    return text.indexOf("poperrorscope") >= 0 && text.indexOf("instance dropped") >= 0;
  }

  // wgpuPopScopedErrorScope pops ONE device error scope.
  //
  // ONLY TWO CALLERS MAY EXIST, and both already do: ensureFBOs' allocation
  // guard below, and the per-frame validation scope
  // (beginWebGPUErrorScope / endWebGPUErrorScope), plus pipeline/module
  // creation scopes. Every resource scope is popped in the same synchronous
  // block that pushes it; no asynchronous build pops another build's scope.
  function wgpuPopScopedErrorScope(scopedDevice) {
    if (!scopedDevice || typeof scopedDevice.popErrorScope !== "function") {
      return Promise.resolve(null);
    }
    try {
      return scopedDevice.popErrorScope().then(function(scopeErr) {
        return scopeErr || null;
      }).catch(function(error) {
        var message = error && error.message ? error.message : String(error);
        if (wgpuIsErrorScopeLifecycleMessage(message)) return null;
        return error || new Error(message);
      });
    } catch (error) {
      var message = error && error.message ? error.message : String(error);
      if (wgpuIsErrorScopeLifecycleMessage(message)) {
        return Promise.resolve(null);
      }
      return Promise.resolve(error || new Error(message));
    }
  }

  // -----------------------------------------------------------------------
  // Shadow Resources
  // -----------------------------------------------------------------------

  // -----------------------------------------------------------------------
  // Post-Processing Manager (WebGPU)
  // -----------------------------------------------------------------------

  // onAllocationError (optional): called with an error message when the
  // HDR/post-FX render-target allocation below (ensureFBOs) fails — e.g. a
  // GPUOutOfMemoryError on a memory-tight browser. See ensureFBOs' error-scope
  // guard: WebGPU resource creation never throws synchronously, so an
  // allocation failure would otherwise silently poison sceneTex/auxTex/
  // depthTex and get reused (and fail validation) EVERY frame thereafter —
  // this is precisely what a memory-tight-browser "Buffer/Texture is invalid"
  // persistent-error session looks like. Guarding makes the failure a
  // detectable event (reported once, cache invalidated so the next
  // ensureFBOs call retries) instead of a poisoned resource used forever.
  // sceneWebGPUDeviceHasF16 reports whether the device negotiated shader-f16.
  // 16z requests the feature when the page asks for optional features, so the
  // gate already exists; this only reads the result.
  function sceneWebGPUDeviceHasF16(device) {
    var features = device && device.features;
    if (!features) return false;
    if (typeof features.has === "function") return features.has("shader-f16");
    return false;
  }

  // sceneWebGPUPostPrecisionMode names which post variant a device gets.
  // A page can force f32 with window.__gosx_scene3d_webgpu_post_f16 = false,
  // which is the first thing to try if a blur ever looks banded.
  function sceneWebGPUPostPrecisionMode(device) {
    if (typeof window !== "undefined" && window.__gosx_scene3d_webgpu_post_f16 === false) return "f32-forced";
    return sceneWebGPUDeviceHasF16(device) ? "f16" : "f32";
  }

  // packSelenaUniforms is injected by the caller (createSceneWebGPURenderer)
  // because the Selena uniform packer — sceneSelenaUniformData and the
  // sceneSelenaMaterialLayout / sceneSelenaUniformValue helpers it needs — lives
  // inside the RENDERER closure, which is a SIBLING of this factory, not an
  // enclosing scope. Calling it by bare name from here throws ReferenceError.
  // That crash sat undiscovered because the customPost case was unreachable:
  // normalizeScenePostEffect lowercased the kind, so this pass never ran and
  // never reached the uniform upload on the frame after its pipeline resolved.
  function sceneWebGPUPostFrameContext(frame) { return Array.isArray(frame) ? { lights: frame } : frame || {}; }
  // Preserve the HDR attachment rounding and the removed present sampler.
  function wgpuCanvasPostShader(name: string, fragmentSource: string, outputFormat: string, targetFormat: string) {
    if (name === "present" || outputFormat === targetFormat) return fragmentSource;
    fragmentSource = fragmentSource.replace(
      "@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f",
      "fn _p(uv: vec2f) -> vec4f");
    var roundSource = "\nfn _r(color: vec4f) -> vec4f {" +
      " let bits = bitcast<vec4u>(clamp(color, vec4f(0.0), vec4f(1.0)));" +
      " let rounded = bits + vec4u(0xfffu) + ((bits >> vec4u(13u)) & vec4u(1u));" +
      " return bitcast<vec4f>(rounded & vec4u(0xffffe000u)); }";
    if (name === "toneMapping" || name === "colorGrade") {
      // A pointwise effect can reproduce the removed present sampler's
      // fractional texel blend. Mip zero is the only level on post targets;
      // explicit LOD lets aligned texels skip neighbour work safely.
      fragmentSource = fragmentSource.replace("textureSample(inputTex, inputSamp, uv)", "textureSampleLevel(inputTex, inputSamp, uv, 0.0)");
      fragmentSource += roundSource +
        "\n@fragment fn fragmentMain(@location(0) uv: vec2f, @builtin(position) position: vec4f) -> @location(0) vec4f {" +
        " let size = vec2f(textureDimensions(inputTex)); let samplePos = uv * size - vec2f(0.5);" +
        " let delta = floor(samplePos) - floor(position.xy); let w = fract(samplePos);" +
        " let dx = dpdx(uv); let dy = dpdy(uv); let start = uv + dx * delta.x + dy * delta.y;" +
        " let a = _r(_p(start));" +
        " if (all(w == vec2f(0.0))) { return a; }" +
        " if (w.y == 0.0) { return mix(a, _r(_p(start + dx)), w.x); }" +
        " let c = _r(_p(start + dy)); if (w.x == 0.0) { return mix(a, c, w.y); }" +
        " let b = _r(_p(start + dx)); let d = _r(_p(start + dx + dy));" +
        " return mix(mix(a, b, w.x), mix(c, d, w.x), w.y); }";
    } else {
      fragmentSource += roundSource + "\n@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f { return _r(_p(uv)); }";
    }
    return fragmentSource;
  }

  function wgpuPostCanPresent(kind: string, width: number, height: number, canvasWidth: number, canvasHeight: number, format: string, last: boolean, disabled: boolean = false) {
    if (disabled || !last) return false;
    if (width !== canvasWidth || height !== canvasHeight) return false;
    if (format !== "rgba8unorm" && format !== "bgra8unorm" && format !== "rgba16float") return false;
    if (kind === SCENE_POST_TONE_MAPPING || kind === SCENE_POST_COLOR_GRADE) return true;
    // Edge/UV-dependent effects require exactly representable pixel centres.
    return (kind === SCENE_POST_FXAA || kind === "taa" || kind === SCENE_POST_VIGNETTE) &&
      (width & (width - 1)) === 0 && (height & (height - 1)) === 0;
  }

  function wgpuPostOutput(direct: boolean, canvasOutput: any, intermediateOutput: any) { return direct ? canvasOutput : intermediateOutput; }
  function wgpuDepthFxPass(state: any, effect: any, i: number, ...rest: any[]) { var api = window.__gosx_scene3d_depthfx_api; return api ? api.webgpuRun(state, effect, i, ...rest) : rest[1]; }
  function wgpuDepthFxDispose(state: any) { window.__gosx_scene3d_depthfx_api?.dispose(state); }
  function wgpuPostPipelineName(name: string, canvasVariant: boolean) { return canvasVariant && name !== "present" ? "canvas-" + name : name; }

  function wgpuWarmPostOutputs(name: string, source: string, layout: any, canvasVariant: boolean, presentation: string, target: string, getPipeline: any) {
    // Create both variants and the blit together so resize reuses ready paths.
    if (name !== "present") getPipeline("present", WGSL_POST_BLIT_FRAGMENT, layout);
    if (["toneMapping", "colorGrade", "fxaa", "vignette"].includes(name) && presentation !== target) {
      getPipeline(name, source, layout, canvasVariant ? target : presentation);
    }
  }

  function wgpuCreatePostProcessor(device, presentationFormat, onAllocationError, packSelenaUniforms) {
    var targetFormat = "rgba16float";
    let specularSource: any = null;
    // Resolve the precision variant once per post processor, not per frame.
    var postPrecisionMode = sceneWebGPUPostPrecisionMode(device);
    var postUsesF16 = postPrecisionMode === "f16";
    var disposed = false;
    var sceneTex = null, sceneTexView = null;
    var auxTex = null, auxTexView = null;
    var pingPongA = null, pingPongAView = null;
    var pingPongB = null, pingPongBView = null;
    var pingPongWidth = 0, pingPongHeight = 0;
    var depthTex = null, depthTexView: any = null, depthResolvePipeline: any = null, depthResolveLayout: any = null;
    var currentWidth = 0, currentHeight = 0, depthFx: any = { device: device }; // decals and motion blur: see depthfx.ts
    var linearSampler = device.createSampler({ magFilter: "linear", minFilter: "linear" });
    var atmospherePost = createSceneAtmospherePostWebGPU({ device, format: targetFormat, sampler: linearSampler, getPipeline, getParamBuffer, fullscreenPass });
    var mipBloom = createSceneWebGPUMipBloom({ device: device, format: targetFormat, sampler: linearSampler, getPipeline: getPipeline, getParamBuffer: getParamBuffer, paramsLayout: getPostParamsLayout, compositeLayout: getBloomCompositeLayout, fullscreenPass: fullscreenPass, compositeSource: WGSL_POST_BLOOM_COMPOSITE_FRAGMENT });
    // Same memoization pattern as the renderer's wgpuCachedBindGroup: a bind
    // group stays valid while the layout and every bound resource identity
    // are unchanged, and per-frame recreation churns GPU wrapper objects.
    // The device is fixed for this processor's lifetime, so it needs no
    // check. Resize replaces the texture views, which invalidates naturally.
    var postBindGroupOwners = { blit: {}, effects: new Map() };
    function postCachedBindGroup(owner, layout, entries) {
      var cache = owner.bgCache;
      if (cache && cache.layout === layout && cache.ids.length === entries.length) {
        var match = true;
        for (var ci = 0; ci < entries.length && match; ci++) {
          var res = entries[ci].resource;
          if (cache.ids[ci] !== (res && res.buffer ? res.buffer : res)) match = false;
        }
        if (match) return cache.bg;
      }
      var ids = [];
      for (var ii = 0; ii < entries.length; ii++) {
        var r = entries[ii].resource;
        ids.push(r && r.buffer ? r.buffer : r);
      }
      var bg = device.createBindGroup({ layout: layout, entries: entries });
      owner.bgCache = { layout: layout, ids: ids, bg: bg };
      return bg;
    }
    function postEffectBindGroupOwner(effect) {
      var name = (typeof effect.name === "string" && effect.name) ? effect.name : "custom";
      var owner = postBindGroupOwners.effects.get(name);
      if (!owner) {
        owner = {};
        postBindGroupOwners.effects.set(name, owner);
      }
      return owner;
    }
    // Render-truth chain state, owned by apply() but hoisted here so
    // fullscreenPass -- the ONE function every post pass funnels through --
    // can attribute its dispatch to the effect currently being processed.
    // Counting at the funnel instead of in each switch case means bloom's four
    // internal passes are counted honestly (dispatched=4), and it is impossible
    // to add a new effect case that forgets to report itself.
    var activePostChain = null, activePostIndex = -1;
    // Lazily compiled pipelines and layouts.
    var pipelines = {};
    var postParamsLayout = null, bloomCompositeLayout = null, postBlitLayout = null, ssaoLayout = null;
    // Uniform buffers for post params (reused each frame).
    var postParamBuffers = {};

    // ---- Custom post (Selena kind:"post") ----
    // Per-name async pipeline cache. Keys are "<name>:<wgslPrefix>".
    // Values: { pending: true } | { pipeline, bgl } | { failed: true }
    var customPostPipelineCache = new Map();
    // Per-name failure flag to emit console.warn only once.
    var customPostFailed = new Set();

    // wgpuCreateSelenaPostBGL: @group(0) for the Selena post contract.
    //   binding(0) texture_2d<f32> sceneColor
    //   binding(1) sampler
    //   binding(2) texture_depth_2d sceneDepth
    //   binding(3) sampler
    //   binding(4) uniform UserUniforms  (always present — placeholder 16 bytes when no params)
    var selenaPostBGL = null;
    function getSelenaPostBGL() {
      if (!selenaPostBGL) {
        selenaPostBGL = device.createBindGroupLayout({
          label: "gosx-selena-post",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
            { binding: 1, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, sampler: {} },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
            { binding: 4, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
          ],
        });
      }
      return selenaPostBGL;
    }

    // depthSampler: non-comparison sampler for sceneDepth (binding 3).
    var depthSampler = null;
    function getDepthSampler() {
      if (!depthSampler) depthSampler = device.createSampler({ magFilter: "nearest", minFilter: "nearest" });
      return depthSampler;
    }

    // buildCustomPostPipelineAsync: async-validates + caches a Selena post pipeline.
    function customPostWGSLModuleSource(effect) {
      var fragment = typeof effect.fragmentWGSL === "string" ? effect.fragmentWGSL.trim() : "";
      var vertex = typeof effect.vertexWGSL === "string" ? effect.vertexWGSL.trim() : "";
      if (fragment && vertex && fragment === vertex) {
        return fragment;
      }
      if (fragment && vertex) {
        return (fragment + "\n" + vertex).trim();
      }
      return (fragment || vertex).trim();
    }

    function buildCustomPostPipelineAsync(effect) {
      var wgsl = customPostWGSLModuleSource(effect);
      if (!wgsl) return null;
      var name = (typeof effect.name === "string" && effect.name) ? effect.name : "custom";
      var cacheKey = name + "\x00" + wgsl;
      var cached = customPostPipelineCache.get(cacheKey);
      if (cached) return cached.failed ? null : cached;

      var pending = { pending: true };
      customPostPipelineCache.set(cacheKey, pending);

      var scopedDevice = device;
      if (!scopedDevice) {
        customPostPipelineCache.delete(cacheKey);
        return null;
      }
      var module = scopedDevice.createShaderModule({ label: "selena-post-" + name, code: wgsl });
      renderTruth().captureShaderInfo(module, "selena-post-" + name);
      var bgl = getSelenaPostBGL();
      var pipelineLayout = scopedDevice.createPipelineLayout({ bindGroupLayouts: [bgl] });

      function markFailed(reason) {
        sceneReportPipelineFailure("post", name, reason);
        if (!customPostFailed.has(name)) {
          console.warn("[gosx] custom post pass '" + name + "' failed validation; becoming identity passthrough.", reason);
          customPostFailed.add(name);
        }
        customPostPipelineCache.set(cacheKey, { failed: true });
      }

      // Validated per object: the promise is the verdict, getCompilationInfo
      // the reason. No device error scope — it would be popped by whichever
      // other async build settled first. See wgpuPopScopedErrorScope.
      scopedDevice.createRenderPipelineAsync({
        label: "gosx-selena-post-" + name,
        layout: pipelineLayout,
        vertex: { module: module, entryPoint: "vertexMain", buffers: [] },
        fragment: {
          module: module,
          entryPoint: "fragmentMain",
          targets: [{ format: targetFormat }],
        },
        primitive: { topology: "triangle-list" },
      }).then(function(pipeline) {
        return sceneShaderModuleError([module]).then(function(compileErr) {
          if (disposed) return;
          if (compileErr) {
            markFailed(compileErr);
            return;
          }
          customPostPipelineCache.set(cacheKey, { pipeline: pipeline, bgl: bgl });
        });
      }).catch(function(err) {
        return sceneShaderModuleError([module]).then(function(compileErr) {
          if (disposed) return;
          markFailed(compileErr || err);
        });
      });
      return null; // pending this frame
    }

    // ensureCustomPostUniformBuffer: 16-byte placeholder when no uniforms, or
    // the Selena-packed uniform block from shaderLayout.
    var customPostUniformBuffers = new Map(); // name → buffer
    function ensureCustomPostUniformBuffer(effect) {
      var name = (typeof effect.name === "string" && effect.name) ? effect.name : "custom";
      var uniformData = typeof packSelenaUniforms === "function"
        ? packSelenaUniforms({ customUniforms: effect.uniforms, shaderLayout: effect.shaderLayout })
        : null;
      if (!uniformData || uniformData.byteLength === 0) {
        uniformData = new Float32Array(4); // 16-byte placeholder
      }
      var existing = customPostUniformBuffers.get(name);
      if (!existing || existing.size < uniformData.byteLength) {
        if (existing) existing.destroy();
        var buf = device.createBuffer({
          size: Math.max(16, uniformData.byteLength),
          usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
          label: "gosx-selena-post-uniforms-" + name,
        });
        customPostUniformBuffers.set(name, buf);
        existing = buf;
      }
      device.queue.writeBuffer(existing, 0, uniformData);
      return existing;
    }

    function getPostParamsLayout() {
      if (!postParamsLayout) postParamsLayout = wgpuCreatePostWithParamsBindGroupLayout(device);
      return postParamsLayout;
    }
    function getBloomCompositeLayout() {
      if (!bloomCompositeLayout) bloomCompositeLayout = wgpuCreateBloomCompositeBindGroupLayout(device);
      return bloomCompositeLayout;
    }
    function getPostBlitLayout() {
      if (!postBlitLayout) postBlitLayout = wgpuCreatePostBindGroupLayout(device);
      return postBlitLayout;
    }
    function getSSAOLayout() {
      if (!ssaoLayout) ssaoLayout = wgpuCreateSSAOBindGroupLayout(device);
      return ssaoLayout;
    }

    function getPipeline(name: string, fragmentSource: string, layout: any, outputFormat: string = targetFormat) {
      if (name === "present") { outputFormat = presentationFormat; layout = getPostBlitLayout(); }
      var cacheKey = name + ":" + outputFormat;
      if (pipelines[cacheKey]) return pipelines[cacheKey];
      var shader = wgpuCanvasPostShader(name, fragmentSource, outputFormat, targetFormat);
      var fragModule = device.createShaderModule({ label: "post-" + name, code: shader });
      var pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [layout] });
      var canvasVariant = outputFormat !== targetFormat;
      var pipeline = wgpuCreatePostPipeline(device, pipelineLayout, fragModule, outputFormat, wgpuPostPipelineName(name, canvasVariant));
      pipelines[cacheKey] = pipeline;
      wgpuWarmPostOutputs(name, fragmentSource, layout, canvasVariant, presentationFormat, targetFormat, getPipeline);
      return pipeline;
    }

    function getParamBuffer(name: string, byteSize: number) {
      if (postParamBuffers[name] && postParamBuffers[name].size >= byteSize) {
        return postParamBuffers[name];
      }
      if (postParamBuffers[name]) postParamBuffers[name].destroy();
      postParamBuffers[name] = device.createBuffer({
        size: wgpuAlignUp(byteSize, 16),
        usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
      });
      return postParamBuffers[name];
    }

    function ensureFBOs(width, height) {
      if (width === currentWidth && height === currentHeight && sceneTex) return;
      // Destroy old.
      mipBloom.dispose(); atmospherePost.dispose();
      if (sceneTex) sceneTex.destroy();
      if (auxTex) auxTex.destroy();
      if (depthTex) depthTex.destroy();

      // Guard allocation with an out-of-memory error scope: createTexture()
      // never throws synchronously in WebGPU (a failed allocation still
      // returns a texture object immediately, just an invalid one), so
      // without this the failure would be silent until something tries to
      // USE the invalid texture — and by then it's cached and reused every
      // frame. Nested inside whatever whole-frame scope the caller may
      // already have open (WebGPU error scopes stack); "out-of-memory" is a
      // distinct filter from the "validation" scope the frame loop uses, so
      // this doesn't steal or duplicate that reporting.
      var scopedAlloc = !!(device && typeof device.pushErrorScope === "function");
      if (scopedAlloc) {
        try { device.pushErrorScope("out-of-memory"); } catch (_err) { scopedAlloc = false; }
      }

      var texUsage = GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING;
      sceneTex = device.createTexture({ size: [width, height, 1], format: targetFormat, usage: texUsage });
      sceneTexView = sceneTex.createView();
      auxTex = device.createTexture({ size: [width, height, 1], format: targetFormat, usage: texUsage });
      auxTexView = auxTex.createView();
      depthTex = device.createTexture({
        size: [width, height, 1],
        format: "depth24plus",
        usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
      });
      depthTexView = depthTex.createView();

      currentWidth = width;
      currentHeight = height;

      if (scopedAlloc) {
        wgpuPopScopedErrorScope(device).then(function(error) {
          if (!error || disposed) return;
          // Invalidate so the NEXT ensureFBOs call retries allocation
          // instead of reusing the poisoned textures forever — this frame's
          // views are already handed out and may still render garbage, but
          // every subsequent frame gets a fresh attempt.
          currentWidth = -1;
          currentHeight = -1;
          if (typeof onAllocationError === "function") {
            onAllocationError(error.message || String(error));
          }
        });
      }
    }

    // Lazily (re)allocate the bloom ping-pong pair at a specific resolution.
    // Called from inside the bloom effect case with dims derived from
    // effect.scale, so Bloom.Scale reaches the WebGPU backend at parity with
    // the WebGL backend. Keeps the textures cached across frames and only
    // tears them down when the target resolution changes.
    function ensureBloomPingPong(w, h) {
      if (w === pingPongWidth && h === pingPongHeight && pingPongA) return;
      if (pingPongA) pingPongA.destroy();
      if (pingPongB) pingPongB.destroy();
      var texUsage = GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING;
      pingPongA = device.createTexture({ size: [w, h, 1], format: targetFormat, usage: texUsage });
      pingPongAView = pingPongA.createView();
      pingPongB = device.createTexture({ size: [w, h, 1], format: targetFormat, usage: texUsage });
      pingPongBView = pingPongB.createView();
      pingPongWidth = w;
      pingPongHeight = h;
    }

    function fullscreenPass(encoder, pipeline, bindGroup, targetView, options) {
      var opts = options && typeof options === "object" ? options : {};
      var pass = encoder.beginRenderPass({
        colorAttachments: [{
          view: targetView,
          loadOp: opts.loadOp === "load" ? "load" : "clear",
          storeOp: "store",
          clearValue: opts.clearValue || { r: 0, g: 0, b: 0, a: 1 },
        }],
      });
      if (opts.scissor && opts.scissor.width > 0 && opts.scissor.height > 0 && typeof pass.setScissorRect === "function") {
        pass.setScissorRect(opts.scissor.x, opts.scissor.y, opts.scissor.width, opts.scissor.height);
      }
      pass.setPipeline(wgpuRequirePipeline(pipeline));
      pass.setBindGroup(0, bindGroup);
      pass.draw(4);
      pass.end();
      // Render truth: this is a real, encoded, submitted draw -- the only
      // point in the post chain where "the pixels were written" becomes true.
      if (opts.markTruth !== false && activePostChain && activePostIndex >= 0) {
        renderTruth().mark(activePostChain, activePostIndex, "ok", 1);
      }
    }

    function copyPostTexture(encoder, pipeline, inputView, outputView) {
      var blitBG = device.createBindGroup({
        layout: getPostBlitLayout(),
        entries: [
          { binding: 0, resource: inputView },
          { binding: 1, resource: linearSampler },
        ],
      });
      fullscreenPass(encoder, pipeline, blitBG, outputView, { markTruth: false });
    }

    function applyContactShadows(encoder: any, input: any, output: any, effect: any, camera: any, size: { width: number; height: number }, lights: any) {
      var pipeline = getPipeline("contactShadows", WGSL_POST_CONTACT_SHADOWS_FRAGMENT, getSSAOLayout());
      var buffer = getParamBuffer("contactShadows:" + activePostIndex, 96);
      device.queue.writeBuffer(buffer, 0, sceneWebGPUContactUniforms(effect, camera, size.width, size.height, lights));
      var group = device.createBindGroup({ layout: getSSAOLayout(), entries: [
        { binding: 0, resource: input }, { binding: 1, resource: linearSampler },
        { binding: 2, resource: depthTexView }, { binding: 3, resource: { buffer: buffer } },
      ] });
      fullscreenPass(encoder, pipeline, group, output, {});
    }

    // Resolve covered MSAA depth before a depth-reading post pass.
    function prepareSceneDepth(effects: any[], samples: number) {
      if (samples !== 4 || !effects.some(effect => [SCENE_POST_SSAO, SCENE_POST_DOF, "contactShadows", SCENE_POST_CUSTOM_POST, "decals", "motionBlur"].includes(effect.kind))) return false;
      if (!depthResolvePipeline) {
        depthResolveLayout = device.createBindGroupLayout({ entries: [{ binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "depth", multisampled: true } }] });
        var module = device.createShaderModule({ label: "post-depth-resolve", code: `@group(0) @binding(0) var source: texture_depth_multisampled_2d;
@fragment fn fragmentMain(@builtin(position) pos: vec4f) -> @builtin(frag_depth) f32 {
  var depth = 1.0; for (var sample = 0; sample < 4; sample++) { depth = min(depth, textureLoad(source, vec2i(pos.xy), sample)); } return depth;
}` });
        depthResolvePipeline = wgpuCreateValidatedPipeline(device, "render", { label: "gosx-post-depth-resolve", layout: device.createPipelineLayout({ bindGroupLayouts: [depthResolveLayout] }),
          vertex: { module: device.createShaderModule({ code: WGSL_POST_VERTEX }), entryPoint: "vertexMain" }, fragment: { module: module, entryPoint: "fragmentMain", targets: [] },
          primitive: { topology: "triangle-strip" }, depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "always" } });
      }
      return true;
    }
    function resolveSceneDepth(encoder: any, sourceView: any, effects: any[], samples: number) {
      if (!prepareSceneDepth(effects, samples)) return;
      var group = device.createBindGroup({ layout: depthResolveLayout, entries: [{ binding: 0, resource: sourceView }] });
      var pass = encoder.beginRenderPass({ label: "gosx-post-depth-resolve", colorAttachments: [], depthStencilAttachment: { view: depthTexView, depthLoadOp: "clear", depthClearValue: 1, depthStoreOp: "store" } });
      pass.setPipeline(wgpuRequirePipeline(depthResolvePipeline)); pass.setBindGroup(0, group); pass.draw(4); pass.end();
    }
    function preparePipelines(effects: any[], samples: number) {
      prepareSceneDepth(effects, samples);
      getPipeline("blit", WGSL_POST_BLIT_FRAGMENT, getPostBlitLayout());
      getPipeline("present", WGSL_POST_BLIT_FRAGMENT, getPostBlitLayout());
      for (var effect of effects) {
        switch (effect.kind) {
          case SCENE_POST_TONE_MAPPING: getPipeline("toneMapping", WGSL_POST_TONEMAPPING_FRAGMENT, getPostParamsLayout()); break;
          case SCENE_POST_BLOOM:
            if (effect.mode === "mip") { mipBloom.preparePipelines(); break; }
            getPipeline("bloomBright", WGSL_POST_BLOOM_BRIGHT_FRAGMENT, getPostParamsLayout());
            getPipeline("blur", postUsesF16 ? WGSL_POST_BLUR_FRAGMENT_F16 : WGSL_POST_BLUR_FRAGMENT, getPostParamsLayout());
            getPipeline("bloomComposite", WGSL_POST_BLOOM_COMPOSITE_FRAGMENT, getBloomCompositeLayout()); break;
          case "contactShadows": getPipeline("contactShadows", WGSL_POST_CONTACT_SHADOWS_FRAGMENT, getSSAOLayout()); break;
          case SCENE_POST_SSAO: getPipeline("ssao", WGSL_POST_SSAO_FRAGMENT, getSSAOLayout()); break;
          case SCENE_POST_DOF: getPipeline("dof", wgpuPostfxDOFFragment(), getSSAOLayout()); break;
          case "taa":
          case SCENE_POST_FXAA: getPipeline("fxaa", postUsesF16 ? WGSL_POST_FXAA_FRAGMENT_F16 : WGSL_POST_FXAA_FRAGMENT, getPostBlitLayout()); break;
          case SCENE_POST_VIGNETTE: getPipeline("vignette", WGSL_POST_VIGNETTE_FRAGMENT, getPostParamsLayout()); break;
          case SCENE_POST_COLOR_GRADE: getPipeline("colorGrade", WGSL_POST_COLORGRADE_FRAGMENT, getPostParamsLayout()); break;
        }
      }
    }
    return {
      preparePipelines: preparePipelines,
      setSpecularSource: function(view: any) { specularSource = view; },
      getSceneTarget: function(width, height) {
        ensureFBOs(width, height);
        return { colorView: sceneTexView, depthView: depthTexView, colorFormat: targetFormat };
      },

      apply: function(encoder, effects, scaledW, scaledH, canvasW, canvasH, finalView, camera, lights: any) {
        var atmosphereContext = sceneWebGPUPostFrameContext(lights); lights = atmosphereContext.lights; atmospherePost.begin(effects);
        ensureFBOs(scaledW, scaledH); resolveSceneDepth(encoder, atmosphereContext.depthView, effects, atmosphereContext.samples);

        var currentTexView = sceneTexView;
        var blitPipeline = getPipeline("blit", WGSL_POST_BLIT_FRAGMENT, getPostBlitLayout());
        // postChain is the per-effect render-truth record. Built ONLY when the
        // diagnostics tier is on, so production pays one boolean read.
        //
        // Every entry starts at pipeline="missing", dispatched=0 and is
        // upgraded by the switch case that handles it. An effect whose case
        // never runs -- an unknown kind, or the lowercased-kind mismatch that
        // made customPost unreachable for three sessions -- therefore stays
        // visibly dead in the published chain instead of being indistinguishable
        // from a healthy pass. postEffects (the old counter) counts the chain
        // LENGTH and would read "1" in exactly that situation.
        var truth = renderTruth();
        var postTruthOn = truth.enabled();
        var postChain = postTruthOn ? truth.chain(effects) : null;
        var stats = {
          postEffects: effects.length,
          postSSAOPasses: 0,
          postDOFPasses: 0,
          postDOMRegionBoundedPasses: 0,
          postDOMRegionBoundedSkips: 0,
          postDOMRegionBoundedPixels: 0,
          postPrecision: postPrecisionMode,
          postColorFormat: targetFormat,
          postChain: postChain,
        };
        activePostChain = postChain;

        for (var i = 0; i < effects.length; i++) {
          var effect = effects[i];
          var outputView = currentTexView === sceneTexView ? auxTexView : sceneTexView;
          var directPresent = wgpuPostCanPresent(effect.kind, scaledW, scaledH, canvasW, canvasH, presentationFormat, i === effects.length - 1, device.__gosxPipelineGuard.disabled.has("directOutput"));
          outputView = wgpuPostOutput(directPresent, finalView, outputView);
          var outputFormat = wgpuPostOutput(directPresent, presentationFormat, targetFormat);
          var passW = scaledW;
          var passH = scaledH;
          activePostIndex = i;

          switch (effect.kind) {
            case "atmosphere": currentTexView = atmospherePost.apply({encoder, input: currentTexView, effect, output: outputView, width: scaledW, height: scaledH, context: atmosphereContext}); break;
            case SCENE_POST_TONE_MAPPING: {
              var pipeline = getPipeline("toneMapping", WGSL_POST_TONEMAPPING_FRAGMENT, getPostParamsLayout(), outputFormat);
              var buf = getParamBuffer("toneMapping", 16);
              device.queue.writeBuffer(buf, 0, new Float32Array([sceneNumber(effect.exposure, 1.0), sceneWebGPUToneMapMode(effect.mode), 0, 0]));
              var bg = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: buf } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, pipeline, bg, outputView);
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_BLOOM: {
              const bloomSource = effect.source === "specular" ? specularSource : currentTexView;
              if (!bloomSource) break;
              if (effect.mode === "mip") { currentTexView = mipBloom.apply({ encoder: encoder, input: currentTexView, source: bloomSource, effect: effect, output: outputView, width: scaledW, height: scaledH, index: i }); break; }
              // Bloom ping-pong resolution is scaledW/H * Bloom.Scale.
              // Zero / out-of-range scale falls back to 0.5 (v0.14.0 default),
              // matching the WebGL helper in applyBloom.
              var bloomScale = (effect.scale > 0 && effect.scale <= 1) ? effect.scale : 0.5;
              var halfW = Math.max(1, Math.floor(scaledW * bloomScale));
              var halfH = Math.max(1, Math.floor(scaledH * bloomScale));
              ensureBloomPingPong(halfW, halfH);
              var threshold = sceneNumber(effect.threshold, 0.8);
              var radius = sceneNumber(effect.radius, 5.0);
              var intensity = sceneNumber(effect.intensity, 0.5);

              // 1. Bright pass -> pingPongA.
              var brightPipeline = getPipeline("bloomBright", WGSL_POST_BLOOM_BRIGHT_FRAGMENT, getPostParamsLayout());
              var brightBuf = getParamBuffer("bloomBright", 16);
              device.queue.writeBuffer(brightBuf, 0, new Float32Array([threshold, 0, 0, 0]));
              var brightBG = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: bloomSource },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: brightBuf } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, brightPipeline, brightBG, pingPongAView);

              // 2. Horizontal blur: pingPongA -> pingPongB.
              var blurPipeline = getPipeline("blur", postUsesF16 ? WGSL_POST_BLUR_FRAGMENT_F16 : WGSL_POST_BLUR_FRAGMENT, getPostParamsLayout());
              var blurBuf = getParamBuffer("bloomBlurH", 16);
              device.queue.writeBuffer(blurBuf, 0, new Float32Array([1.0, 0.0, radius, 0]));
              var blurBGH = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: pingPongAView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: blurBuf } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, blurPipeline, blurBGH, pingPongBView);

              // 3. Vertical blur: pingPongB -> pingPongA.
              var blurBufV = getParamBuffer("bloomBlurV", 16);
              device.queue.writeBuffer(blurBufV, 0, new Float32Array([0.0, 1.0, radius, 0]));
              var blurBGV = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: pingPongBView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: blurBufV } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, blurPipeline, blurBGV, pingPongAView);

              // 4. Composite: scene + bloom -> output.
              var compPipeline = getPipeline("bloomComposite", WGSL_POST_BLOOM_COMPOSITE_FRAGMENT, getBloomCompositeLayout());
              var compBuf = getParamBuffer("bloomComposite", 16);
              device.queue.writeBuffer(compBuf, 0, new Float32Array([intensity, 0, 0, 0]));
              var compBG = device.createBindGroup({
                layout: getBloomCompositeLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: pingPongAView },
                  { binding: 3, resource: linearSampler },
                  { binding: 4, resource: { buffer: compBuf } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, compPipeline, compBG, outputView);
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_SSAO: {
              var ssaoPipeline = getPipeline("ssao", WGSL_POST_SSAO_FRAGMENT, getSSAOLayout());
              var ssaoBuf = getParamBuffer("ssao", 32);
              var radius = sceneNumber(effect.radius, 4.0);
              var intensity = sceneNumber(effect.intensity, 0.55);
              var bias = sceneNumber(effect.bias, 0.01);
              device.queue.writeBuffer(ssaoBuf, 0, new Float32Array([
                radius,
                intensity,
                bias,
                0,
                1 / Math.max(1, scaledW),
                1 / Math.max(1, scaledH),
                0,
                0,
              ]));
              var ssaoBG = device.createBindGroup({
                layout: getSSAOLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: depthTexView },
                  { binding: 3, resource: { buffer: ssaoBuf } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, ssaoPipeline, ssaoBG, outputView);
              stats.postSSAOPasses += 1;
              currentTexView = outputView;
              break;
            }
            case "contactShadows":
              applyContactShadows(encoder, currentTexView, outputView, effect, camera, { width: canvasW, height: canvasH }, lights);
              currentTexView = outputView;
              break;
            case SCENE_POST_DOF: {
              var dofPipeline = getPipeline("dof", wgpuPostfxDOFFragment(), getSSAOLayout());
              var dofBuf = getParamBuffer("dof", 32);
              device.queue.writeBuffer(dofBuf, 0, wgpuPostfxDOFParams(effect, camera, scaledW, scaledH));
              var dofBG = device.createBindGroup({
                layout: getSSAOLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: depthTexView },
                  { binding: 3, resource: { buffer: dofBuf } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, dofPipeline, dofBG, outputView);
              stats.postDOFPasses += 1;
              currentTexView = outputView;
              break;
            }
            case "taa": // Temporal resolve is unsupported here: retain FXAA edges.
            case SCENE_POST_FXAA: {
              // Chain-end edge AA. Reuses the blit bind group layout
              // (texture + sampler, no uniforms) since FXAA has no params.
              var fxaaPipeline = getPipeline("fxaa", postUsesF16 ? WGSL_POST_FXAA_FRAGMENT_F16 : WGSL_POST_FXAA_FRAGMENT, getPostBlitLayout(), outputFormat);
              var fxaaBG = device.createBindGroup({
                layout: getPostBlitLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, fxaaPipeline, fxaaBG, outputView);
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_VIGNETTE: {
              var vigPipeline = getPipeline("vignette", WGSL_POST_VIGNETTE_FRAGMENT, getPostParamsLayout(), outputFormat);
              var vigBuf = getParamBuffer("vignette", 16);
              device.queue.writeBuffer(vigBuf, 0, new Float32Array([sceneNumber(effect.intensity, 1.0), 0, 0, 0]));
              var vigBG = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: vigBuf } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, vigPipeline, vigBG, outputView);
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_COLOR_GRADE: {
              var cgPipeline = getPipeline("colorGrade", WGSL_POST_COLORGRADE_FRAGMENT, getPostParamsLayout(), outputFormat);
              var cgBuf = getParamBuffer("colorGrade", 16);
              device.queue.writeBuffer(cgBuf, 0, new Float32Array([
                sceneNumber(effect.exposure, 1.0),
                sceneNumber(effect.contrast, 1.0),
                sceneNumber(effect.saturation, 1.0),
                0,
              ]));
              var cgBG = device.createBindGroup({
                layout: getPostParamsLayout(),
                entries: [
                  { binding: 0, resource: currentTexView },
                  { binding: 1, resource: linearSampler },
                  { binding: 2, resource: { buffer: cgBuf } },
                ],
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ });
              fullscreenPass(encoder, cgPipeline, cgBG, outputView);
              currentTexView = outputView;
              break;
            }
            case SCENE_POST_CUSTOM_POST: {
              // Selena post contract: WGSL fullscreen triangle, vertexMain/fragmentMain,
              // @group(0) bindings: 0=sceneColor, 1=sceneColorSampler, 2=sceneDepth,
              //   3=sceneDepthSampler, 4=UserUniforms (16-byte placeholder when absent).
              var cpRes = buildCustomPostPipelineAsync(effect);
              if (!cpRes || cpRes.pending || cpRes.failed) {
                // Not yet compiled (first frame) or failed → identity passthrough.
                // currentTexView is unchanged; the output falls through to the blit.
                //
                // THIS is the branch that produced zero pixels for three
                // sessions while every health attribute read green. Distinguish
                // the three causes explicitly: no WGSL at all (missing), still
                // compiling (pending -- benign for a frame or two, a defect if
                // it persists), or rejected by the browser's own WGSL compiler
                // (failed -- and note that Selena validates with naga while
                // Edge compiles with Tint, so "failed" here on Edge alone is a
                // Tint/naga divergence, not necessarily a bad shader).
                if (postChain) {
                  var cpState = truth.PIPELINE_MISSING;
                  if (cpRes && cpRes.pending) cpState = truth.PIPELINE_PENDING;
                  else if (cpRes && cpRes.failed) cpState = truth.PIPELINE_FAILED;
                  else if (customPostFailed.has((typeof effect.name === "string" && effect.name) ? effect.name : "custom")) {
                    cpState = truth.PIPELINE_FAILED;
                  } else if (customPostWGSLModuleSource(effect)) {
                    cpState = truth.PIPELINE_PENDING;
                  }
                  truth.mark(postChain, i, cpState, 0);
                }
                break;
              }
              var cpUniformBuf = ensureCustomPostUniformBuffer(effect);
              var roi = scenePostDOMRegionPixelBounds(effect, passW, passH);
              if (roi.mode === "union" && !roi.bounds) {
                stats.postDOMRegionBoundedSkips += 1;
                if (postChain) truth.mark(postChain, i, "skipped", 0);
                break;
              }
              var cpBG = postCachedBindGroup(postEffectBindGroupOwner(effect), cpRes.bgl, [
                { binding: 0, resource: currentTexView },
                { binding: 1, resource: linearSampler },
                { binding: 2, resource: depthTexView },
                { binding: 3, resource: getDepthSampler() },
                { binding: 4, resource: { buffer: cpUniformBuf } },
              ]);
              if (roi.mode === "union") {
                copyPostTexture(encoder, blitPipeline, currentTexView, outputView);
                fullscreenPass(encoder, cpRes.pipeline, cpBG, outputView, { loadOp: "load", scissor: roi.bounds });
                stats.postDOMRegionBoundedPasses += 1;
                stats.postDOMRegionBoundedPixels += roi.bounds.width * roi.bounds.height;
              /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ } else {
                fullscreenPass(encoder, cpRes.pipeline, cpBG, outputView);
              }
              currentTexView = outputView;
              break;
            }
            default: currentTexView = wgpuDepthFxPass(depthFx, effect, i, encoder, currentTexView, outputView, outputFormat, depthTexView, scaledW, scaledH, atmosphereContext, postChain); break;
          }
        }

        // The present blit is not an authored effect. Detach attribution so a
        // dead trailing effect cannot inherit a successful presentation.
        activePostIndex = -1;

        // If no effects matched or we need a final blit.
        if (currentTexView !== finalView) {
          var blitBG = postCachedBindGroup(postBindGroupOwners.blit, getPostBlitLayout(), [
            { binding: 0, resource: currentTexView },
            { binding: 1, resource: linearSampler },
          /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ ]);
          fullscreenPass(encoder, getPipeline("present", WGSL_POST_BLIT_FRAGMENT, getPostBlitLayout()), blitBG, finalView);
        }
        activePostChain = null;
        return stats;
      },

      dispose: function() {
        mipBloom.dispose(); atmospherePost.dispose(); wgpuDepthFxDispose(depthFx);
        disposed = true;
        for (const texture of [sceneTex, auxTex, depthTex, pingPongA, pingPongB]) if (texture) texture.destroy();
        for (var key in postParamBuffers) { if (postParamBuffers[key]) postParamBuffers[key].destroy(); }
        customPostUniformBuffers.forEach(function(buf) { if (buf) buf.destroy(); });
        customPostUniformBuffers.clear();
        customPostPipelineCache.clear();
        customPostFailed.clear();
        if (selenaPostBGL) { selenaPostBGL = null; }
        if (depthSampler) { depthSampler = null; }
        sceneTex = auxTex = depthTex = pingPongA = pingPongB = null;
        currentWidth = currentHeight = pingPongWidth = pingPongHeight = 0;
      },
    };
  }

  // -----------------------------------------------------------------------
  // GPU picking (r32uint ID attachment + async 1x1 readback)
  // -----------------------------------------------------------------------
  //
  // Port of the native design in render/bundle/pick.go. The GPU answers ONE
  // question exactly: which pickable draw covers the pointer pixel. A separate
  // pick pass rasterizes only pickable geometry into an r32uint ID texture with
  // its own depth buffer. A 1x1 copyTextureToBuffer + mapAsync reads the ID
  // back. The frame never blocks on the readback.
  //
  // Deviation from pick.go, stated plainly: pick.go writes the ID as a second
  // color attachment of the main pass. This renderer keeps a SEPARATE pass
  // instead. A second main-pass attachment forces every one of the 31 main-pass
  // pipelines to declare a matching @location(1) u32 fragment output; one miss
  // is a pipeline/pass format mismatch that kills the whole frame. A separate
  // pass has the same result, costs nothing on frames with no queued pick, and
  // needs no edit to the existing pipelines.
  //
  // Parity rule: the ID only resolves IDENTITY. Every geometric field
  // (triangleIndex, uv, localPosition, worldPosition, depth, distance) comes
  // from the SAME shared CPU raycast helpers that 17-scene-input.ts runs on
  // WebGL2 (sceneRaycastPickGroup / sceneRaycastPickInstancedMeshes, exported
  // on window.__gosx_scene3d_api). Both backends therefore return the same hit
  // record shape with the same numbers. See sceneWebGPUPickResolve.

  var SCENE_WEBGPU_PICK_ID_FORMAT = "r32uint";
  // WebGPU requires bytesPerRow of a texture-to-buffer copy to be a multiple
  // of 256. 256 is also the smallest legal copy target, so one row holds the
  // single pixel we read.
  var SCENE_WEBGPU_PICK_ROW_ALIGNMENT = 256;
  // PickUniforms is two mat4x4f values plus four u32: 144 bytes. Each draw
  // gets its own 256-byte slot, because 256 is also the default
  // minUniformBufferOffsetAlignment that a dynamic offset must respect.
  var SCENE_WEBGPU_PICK_SLOT_BYTES = 144;
  // Upper bound on pickable draws per pass. 4096 slots is 1 MiB of uniform
  // space. Instanced meshes cost one slot per MESH, not per instance, so real
  // scenes stay far below this.
  var SCENE_WEBGPU_PICK_MAX_SLOTS = 4096;
  // Mirror of SCENE_PICK_MIN_EXTENT_X / _Y in 17-scene-input.ts. Keep the two
  // in sync; sceneWebGPUPickAllowsObject must accept exactly the objects that
  // sceneObjectAllowsPointerPick accepts, or the pick pass can hide a pickable
  // object behind a non-pickable one.
  var SCENE_WEBGPU_PICK_MIN_EXTENT_X = 0.12;
  var SCENE_WEBGPU_PICK_MIN_EXTENT_Y = 0.08;

  // Declarations shared by the three pick shaders. Keep them in one constant so
  // the vertex and fragment stages cannot drift apart, and so the bundle ships
  // one copy instead of three.
  var WGSL_PICK_OUTPUT_STRUCT = [
    "struct PickOutput {",
    "  @builtin(position) clipPos: vec4f,",
    "  @location(0) @interpolate(flat) id: u32,",
    "};",
  ].join("\n");

  var WGSL_PICK_UNIFORM_STRUCT = [
    "struct PickUniforms {",
    "  viewProjection: mat4x4f,",
    "  baseID: u32,",
    "  _pad0: u32,",
    "  _pad1: u32,",
    "  _pad2: u32,",
    "  modelMatrix: mat4x4f,",
    "};",
    "@group(0) @binding(0) var<uniform> pick: PickUniforms;",
  ].join("\n");

  var WGSL_PICK_VERTEX = [
    WGSL_PICK_UNIFORM_STRUCT,
    WGSL_PICK_OUTPUT_STRUCT,
    "@vertex fn vertexMain(@location(0) position: vec3f) -> PickOutput {",
    "  var out: PickOutput;",
    "  out.clipPos = pick.viewProjection * pick.modelMatrix * vec4f(position, 1.0);",
    "  out.id = pick.baseID;",
    "  return out;",
    "}",
  ].join("\n");

  // Instanced variant. Locations 4-7 carry the per-instance mat4, matching
  // WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT. The ID is baseID + instance_index, so
  // one draw stamps a distinct ID per instance exactly like
  // buildInstancedPickTargets in render/bundle/pick.go.
  var WGSL_PICK_INSTANCED_VERTEX = [
    WGSL_PICK_UNIFORM_STRUCT,
    WGSL_PICK_OUTPUT_STRUCT,
    "struct PickVertexInput {",
    "  @location(0) position: vec3f,",
    "  @location(4) instanceMatrix0: vec4f,",
    "  @location(5) instanceMatrix1: vec4f,",
    "  @location(6) instanceMatrix2: vec4f,",
    "  @location(7) instanceMatrix3: vec4f,",
    "};",
    "@vertex fn vertexMain(in: PickVertexInput, @builtin(instance_index) instance: u32) -> PickOutput {",
    "  let model = mat4x4f(in.instanceMatrix0, in.instanceMatrix1, in.instanceMatrix2, in.instanceMatrix3);",
    "  var out: PickOutput;",
    "  out.clipPos = pick.viewProjection * model * vec4f(in.position, 1.0);",
    "  out.id = pick.baseID + instance;",
    "  return out;",
    "}",
  ].join("\n");

  var WGSL_PICK_FRAGMENT = [
    WGSL_PICK_OUTPUT_STRUCT,
    "@fragment fn fragmentMain(in: PickOutput) -> @location(0) u32 {",
    "  return in.id;",
    "}",
  ].join("\n");

  // cullMode "none" and depthCompare "less-equal" match wgpuCreatePBRPipeline,
  // so the pick pass resolves the same front-most surface the main pass paints.
  // Double-sided rasterization also matches the CPU raycast, which accepts both
  // triangle windings.
  function wgpuCreatePickPipeline(device, pipelineLayout, vertexModule, fragmentModule, vertexLayout, label) {
    return wgpuCreateValidatedPipeline(device, "render", {
      label: label,
      layout: pipelineLayout,
      vertex: { module: vertexModule, entryPoint: "vertexMain", buffers: vertexLayout },
      fragment: {
        module: fragmentModule,
        entryPoint: "fragmentMain",
        targets: [{ format: SCENE_WEBGPU_PICK_ID_FORMAT }],
      },
      primitive: { topology: "triangle-list", cullMode: "none" },
      depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" },
    });
  }

  function sceneWebGPUPickSharedAPI() {
    if (typeof window === "undefined") return null;
    return window.__gosx_scene3d_api || null;
  }

  function sceneWebGPUPickSharedFn(name) {
    var api = sceneWebGPUPickSharedAPI();
    var fn = api ? api[name] : null;
    return typeof fn === "function" ? fn : null;
  }

  // Port of sceneBoundsSize in 17-scene-input.ts: absolute extents, largest
  // first.
  function sceneWebGPUPickBoundsExtents(bounds) {
    if (!bounds || typeof bounds !== "object") return [0, 0, 0];
    return [
      Math.abs(sceneNumber(bounds.maxX, 0) - sceneNumber(bounds.minX, 0)),
      Math.abs(sceneNumber(bounds.maxY, 0) - sceneNumber(bounds.minY, 0)),
      Math.abs(sceneNumber(bounds.maxZ, 0) - sceneNumber(bounds.minZ, 0)),
    ].sort(function(a, b) { return b - a; });
  }

  // Port of sceneObjectAllowsPointerPick in 17-scene-input.ts. The pick pass
  // must draw exactly the set that file picks, so a non-pickable object never
  // occludes a pickable one in the ID buffer.
  function sceneWebGPUPickAllowsObject(object) {
    if (!object || object.viewCulled || object._crowdBatch === true || object._crowdSkin) return false;
    if (typeof object.pickable === "boolean") return object.pickable;
    if (object.kind === "plane") return false;
    var extents = sceneWebGPUPickBoundsExtents(object.bounds);
    return extents[0] > SCENE_WEBGPU_PICK_MIN_EXTENT_X && extents[1] > SCENE_WEBGPU_PICK_MIN_EXTENT_Y;
  }

  // sceneWebGPUPickIDPlan assigns pick IDs over a bundle. ID 0 stays reserved
  // for background, so the first target gets ID 1 — the same convention as
  // buildInstancedPickTargets(meshes, 1) in render/bundle/pick.go.
  //
  // Group order matches sceneRaycastPick in 17-scene-input.ts: mesh objects
  // first, then instanced meshes. Each entry records the group, the index
  // within that group, and the instance span, so a returned ID maps back to one
  // exact draw.
  function sceneWebGPUPickIDPlan(bundle) {
    var plan = { entries: [], meshBases: [], instancedBases: [], nextID: 1 };
    if (!bundle) return plan;

    var meshObjects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
    for (var m = 0; m < meshObjects.length; m += 1) {
      plan.meshBases[m] = 0;
      var obj = meshObjects[m];
      if (!obj || !sceneWebGPUPickAllowsObject(obj)) continue;
      var vertexCount = Math.floor(sceneNumber(obj.vertexCount, 0));
      if (!Number.isFinite(sceneNumber(obj.vertexOffset, NaN)) || vertexCount <= 0) continue;
      plan.meshBases[m] = plan.nextID;
      plan.entries.push({ id: plan.nextID, group: "mesh", index: m, count: 1, slot: plan.entries.length });
      plan.nextID += 1;
    }

    var instanced = Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes : [];
    for (var i = 0; i < instanced.length; i += 1) {
      plan.instancedBases[i] = 0;
      var mesh = instanced[i];
      if (!mesh || !sceneWebGPUPickAllowsObject(mesh)) continue;
      var transforms = mesh.transforms;
      if (!transforms || typeof transforms.length !== "number") continue;
      var count = Math.min(
        Math.max(0, Math.floor(sceneNumber(mesh.count, 0))),
        Math.floor(transforms.length / 16)
      );
      if (count <= 0) continue;
      plan.instancedBases[i] = plan.nextID;
      plan.entries.push({ id: plan.nextID, group: "instanced", index: i, count: count, slot: plan.entries.length });
      plan.nextID += count;
    }
    return plan;
  }

  // sceneWebGPUPickEntryForID finds the plan entry that owns an ID and reports
  // the instance offset inside it.
  function sceneWebGPUPickEntryForID(plan, id) {
    var wanted = Math.floor(sceneNumber(id, 0));
    if (!plan || !Array.isArray(plan.entries) || wanted <= 0) return null;
    for (var i = 0; i < plan.entries.length; i += 1) {
      var entry = plan.entries[i];
      if (!entry) continue;
      if (wanted >= entry.id && wanted < entry.id + entry.count) {
        return { entry: entry, instanceIndex: wanted - entry.id };
      }
    }
    return null;
  }

  // sceneWebGPUPickSingleInstanceMesh clones a mesh down to the one instance
  // the GPU selected. sceneRaycastPickInstancedMeshes then computes the hit for
  // that instance with the identical math WebGL2 uses, instead of re-running
  // its nearest-instance search (which uses a sphere approximation and can
  // disagree with the exact rasterized answer).
  function sceneWebGPUPickSingleInstanceMesh(mesh, instanceIndex) {
    var transforms = mesh && mesh.transforms;
    var base = instanceIndex * 16;
    if (!transforms || typeof transforms.length !== "number" || base + 15 >= transforms.length) {
      return null;
    }
    var slice = new Float32Array(16);
    for (var k = 0; k < 16; k += 1) {
      slice[k] = sceneNumber(transforms[base + k], 0);
    }
    var clone = Object.create(Object.getPrototypeOf(mesh) || Object.prototype);
    for (var key in mesh) {
      if (Object.prototype.hasOwnProperty.call(mesh, key)) clone[key] = mesh[key];
    }
    clone.count = 1;
    clone.transforms = slice;
    return clone;
  }

  // Port of sceneNearestRaycastHit in 17-scene-input.ts.
  function sceneWebGPUPickNearestHit(current, candidate) {
    if (!candidate) return current;
    if (!current || candidate.distance < current.distance) return candidate;
    return current;
  }

  // sceneWebGPUPickRefineHit turns an ID into the full hit record by running
  // the shared CPU raycast on the ONE target the GPU selected.
  function sceneWebGPUPickRefineHit(bundle, plan, id, ray) {
    var found = sceneWebGPUPickEntryForID(plan, id);
    if (!found || !ray) return null;
    var entry = found.entry;

    if (entry.group === "mesh") {
      var raycastGroup = sceneWebGPUPickSharedFn("sceneRaycastPickGroup");
      var meshObjects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      var obj = meshObjects[entry.index];
      if (!raycastGroup || !obj) return null;
      return raycastGroup(ray, [obj], bundle.worldMeshPositions, entry.index, bundle.worldMeshUVs);
    }

    var raycastInstanced = sceneWebGPUPickSharedFn("sceneRaycastPickInstancedMeshes");
    var instanced = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : [];
    var mesh = instanced[entry.index];
    if (!raycastInstanced || !mesh) return null;
    var single = sceneWebGPUPickSingleInstanceMesh(mesh, found.instanceIndex);
    if (!single) return null;
    var hit = raycastInstanced(ray, [single], entry.index);
    if (!hit) return null;
    // Report the real mesh and the real instance index, not the single-instance
    // clone the raycast ran against.
    hit.object = mesh;
    hit.instanceIndex = found.instanceIndex;
    return hit;
  }

  // sceneWebGPUPickResolve produces the final hit record for a read-back ID.
  // It returns exactly what sceneRaycastPick returns on WebGL2: the same
  // fields, from the same helpers, plus `ray`. null means background.
  //
  // The third CPU group (bundle.objects over bundle.worldPositions) is merged
  // in on the CPU. The WebGPU renderer rasterizes bundle.worldPositions as a
  // line list, never as triangles, so that group has no ID-buffer draw to
  // resolve. Merging by nearest distance keeps coverage identical to
  // sceneRaycastPick.
  function sceneWebGPUPickResolve(bundle, plan, id, ray) {
    if (!bundle || !ray) return null;
    var hit = sceneWebGPUPickRefineHit(bundle, plan, id, ray);
    var raycastGroup = sceneWebGPUPickSharedFn("sceneRaycastPickGroup");
    if (raycastGroup && Array.isArray(bundle.objects) && bundle.objects.length > 0) {
      hit = sceneWebGPUPickNearestHit(
        hit,
        raycastGroup(ray, bundle.objects, bundle.worldPositions, 0, null)
      );
    }
    if (hit) hit.ray = ray;
    return hit;
  }

  function sceneWebGPUPickSnapshotBundle(bundle) {
    if (!bundle || !Array.isArray(bundle.meshObjects)) return bundle;
    var copy = Object.assign({}, bundle);
    copy.meshObjects = bundle.meshObjects.map(function(obj) {
      if (!obj || !obj.retainedGeometry || !obj.modelMatrix || obj.modelMatrix.length < 16) {
        return obj;
      }
      return Object.assign({}, obj, { modelMatrix: new Float32Array(obj.modelMatrix) });
    });
    return copy;
  }

  // createSceneWebGPUPicker owns the pick textures, pipelines, and the single
  // in-flight readback. The adapter supplies the renderer-scoped closures the
  // pick pass needs; keeping the picker at module scope stops
  // createSceneWebGPURenderer from growing further.
  //
  // adapter fields:
  //   viewProjection()          -> Float32Array(16), WebGPU-convention VP
  //   meshPositions()           -> { buffer, components } or null
  //   bindMeshPositions(...)    -> bool, binds a vertex slice by vertex range
  //   instancedGeometry(mesh)   -> { positions, vertexCount, ... } or null
  //   instancedGeometryBuffer(geom, slot, data) -> GPUBuffer
  //   instancedTransformBuffer(mesh, data)      -> GPUBuffer
  //   instancedCount(mesh)      -> int
  //   instancedTransforms(mesh, count) -> Float32Array or null
  //   onError(message)          -> void
  function createSceneWebGPUPicker(device, adapter) {
    if (!device || !adapter) return null;

    var vertexModule = null;
    var instancedVertexModule = null;
    var fragmentModule = null;
    var bindGroupLayout = null;
    var pipelineLayout = null;
    var pipeline = null;
    var instancedPipeline = null;
    var uniformBuffer = null;
    var bindGroup = null;
    var idTexture = null;
    var idView = null;
    var depthTexture = null;
    var depthView = null;
    var idWidth = 0;
    var idHeight = 0;
    var initFailed = false;
    var uniformSlots = 0;
    var uniformData = null;
    var uniformIDs = null;
    var pending = null;
    // Superseded requests whose copy is already on the GPU. Their callbacks
    // never fire, but their staging buffers must still be mapped and released
    // or the buffer leaks. Mirrors retiredPicks in render/bundle/pick.go.
    var retired = [];
    var stats = { requests: 0, drops: 0, passes: 0, draws: 0, readbacks: 0, skipped: 0, lastID: 0, lastError: "" };

    function ensureResources() {
      if (initFailed) return false;
      if (pipeline && instancedPipeline) return true;
      try {
        vertexModule = device.createShaderModule({ label: "pick-vert", code: WGSL_PICK_VERTEX });
        instancedVertexModule = device.createShaderModule({ label: "pick-instanced-vert", code: WGSL_PICK_INSTANCED_VERTEX });
        fragmentModule = device.createShaderModule({ label: "pick-frag", code: WGSL_PICK_FRAGMENT });
        // hasDynamicOffset is what makes one buffer serve every draw. Without
        // it each draw would need its own writeBuffer, and because
        // queue.writeBuffer lands BEFORE the submitted commands run, every draw
        // in the pass would read the LAST baseID written. One slot per draw plus
        // a dynamic offset is the only correct single-buffer form.
        bindGroupLayout = device.createBindGroupLayout({
          label: "gosx-pick-frame",
          entries: [{
            binding: 0,
            visibility: GPUShaderStage.VERTEX,
            buffer: { type: "uniform", hasDynamicOffset: true, minBindingSize: SCENE_WEBGPU_PICK_SLOT_BYTES },
          }],
        });
        pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [bindGroupLayout] });
        pipeline = wgpuCreatePickPipeline(
          device, pipelineLayout, vertexModule, fragmentModule,
          WGPU_SHADOW_VERTEX_LAYOUT, "gosx-pick"
        );
        instancedPipeline = wgpuCreatePickPipeline(
          device, pipelineLayout, instancedVertexModule, fragmentModule,
          WGPU_SHADOW_INSTANCED_VERTEX_LAYOUT, "gosx-pick-instanced"
        );
        return true;
      } catch (err) {
        initFailed = true;
        stats.lastError = String(err && (err.message || err) || "pick-init-failed");
        if (typeof adapter.onError === "function") adapter.onError(stats.lastError);
        return false;
      }
    }

    function ensureTargets(width, height) {
      if (idTexture && idWidth === width && idHeight === height) return true;
      if (idTexture) idTexture.destroy();
      if (depthTexture) depthTexture.destroy();
      idTexture = depthTexture = null;
      idView = depthView = null;
      idWidth = idHeight = 0;
      try {
        idTexture = device.createTexture({
          label: "gosx-pick-id",
          size: [width, height, 1],
          format: SCENE_WEBGPU_PICK_ID_FORMAT,
          usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.COPY_SRC,
        });
        depthTexture = device.createTexture({
          label: "gosx-pick-depth",
          size: [width, height, 1],
          format: "depth24plus",
          usage: GPUTextureUsage.RENDER_ATTACHMENT,
        });
        idView = idTexture.createView();
        depthView = depthTexture.createView();
        idWidth = width;
        idHeight = height;
        return true;
      } catch (err) {
        stats.lastError = String(err && (err.message || err) || "pick-target-failed");
        if (typeof adapter.onError === "function") adapter.onError(stats.lastError);
        return false;
      }
    }

    // ensureUniformSlots grows the uniform buffer to hold one 256-byte slot per
    // plan entry, and rebuilds the bind group to match. Returns the slot count
    // the buffer can serve; entries past it are skipped and counted.
    function ensureUniformSlots(wanted) {
      var need = Math.max(1, Math.min(wanted, SCENE_WEBGPU_PICK_MAX_SLOTS));
      if (uniformBuffer && uniformSlots >= need) return uniformSlots;
      if (uniformBuffer) uniformBuffer.destroy();
      uniformBuffer = device.createBuffer({
        label: "gosx-pick-uniforms",
        size: need * SCENE_WEBGPU_PICK_ROW_ALIGNMENT,
        usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
      });
      bindGroup = device.createBindGroup({
        layout: bindGroupLayout,
        entries: [{
          binding: 0,
          resource: { buffer: uniformBuffer, offset: 0, size: SCENE_WEBGPU_PICK_ROW_ALIGNMENT },
        }],
      });
      uniformSlots = need;
      uniformData = new Float32Array(need * SCENE_WEBGPU_PICK_ROW_ALIGNMENT / 4);
      uniformIDs = new Uint32Array(uniformData.buffer);
      return uniformSlots;
    }

    // uploadPickUniforms fills every slot and uploads them in ONE writeBuffer,
    // before the pass begins. Slot i holds the view-projection matrix and the
    // base ID of plan entry i.
    function uploadPickUniforms(plan, viewProjection, bundle) {
      var slots = ensureUniformSlots(plan.entries.length);
      var floatsPerSlot = SCENE_WEBGPU_PICK_ROW_ALIGNMENT / 4;
      var used = Math.min(plan.entries.length, slots);
      for (var i = 0; i < used; i += 1) {
        var base = i * floatsPerSlot;
        if (viewProjection && viewProjection.length >= 16) {
          for (var k = 0; k < 16; k += 1) uniformData[base + k] = viewProjection[k];
        }
        uniformIDs[base + 16] = plan.entries[i].id >>> 0;
        uniformIDs[base + 17] = 0;
        uniformIDs[base + 18] = 0;
        uniformIDs[base + 19] = 0;
        var entry = plan.entries[i];
        var meshObjects = bundle && Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
        var obj = entry && entry.group === "mesh" ? meshObjects[entry.index] : null;
        var model = obj && obj.retainedGeometry && obj.modelMatrix && obj.modelMatrix.length >= 16
          ? obj.modelMatrix
          : null;
        for (var mk = 0; mk < 16; mk++) {
          uniformData[base + 20 + mk] = model
            ? sceneNumber(model[mk], mk % 5 === 0 ? 1 : 0)
            : (mk % 5 === 0 ? 1 : 0);
        }
      }
      if (plan.entries.length > slots) stats.skipped += plan.entries.length - slots;
      device.queue.writeBuffer(uniformBuffer, 0, uniformData, 0, used * floatsPerSlot);
      return used;
    }

    // queuePick records a request in the caller's pointer space. x, y, width,
    // and height must be the SAME values the caller feeds sceneScreenToRay, so
    // the GPU pixel and the CPU ray agree. Only one pick may wait at a time;
    // a newer request replaces an older one that has not been submitted yet,
    // matching QueuePickResult in render/bundle/pick.go.
    function queuePick(x, y, width, height, callback) {
      if (typeof callback !== "function") return false;
      stats.requests += 1;
      if (pending) {
        stats.drops += 1;
        pending.superseded = true;
        if (pending.submitted && !pending.reading) {
          // The GPU already owns this copy. Keep the request so finishReadback
          // still maps and frees its staging buffer.
          retired.push(pending);
        } else if (!pending.submitted && pending.staging) {
          try { pending.staging.destroy(); } catch (err) { void err; }
        }
        // A request already reading owns its own cleanup.
      }
      pending = {
        x: sceneNumber(x, 0),
        y: sceneNumber(y, 0),
        width: Math.max(1, sceneNumber(width, 1)),
        height: Math.max(1, sceneNumber(height, 1)),
        callback: callback,
        submitted: false,
        superseded: false,
        reading: false,
        staging: null,
        bundle: null,
        plan: null,
        ray: null,
      };
      return true;
    }

    function failPending(request) {
      if (!request || request.superseded) return;
      if (pending === request) pending = null;
      request.callback(null);
    }

    // recordPickPass draws the pickable geometry of `bundle` into the ID
    // texture and records the 1x1 copy. Call it with the frame encoder AFTER
    // the main pass has ended, so every vertex and transform buffer exists.
    // targetWidth/targetHeight are the render-target size in device pixels.
    function recordPickPass(encoder, bundle, targetWidth, targetHeight) {
      var request = pending;
      if (!request || request.submitted || !encoder || !bundle) return false;
      var deviceWidth = Math.max(1, Math.floor(targetWidth));
      var deviceHeight = Math.max(1, Math.floor(targetHeight));

      var scaleX = deviceWidth / request.width;
      var scaleY = deviceHeight / request.height;
      var px = Math.floor(request.x * scaleX);
      var py = Math.floor(request.y * scaleY);
      if (px < 0 || py < 0 || px >= deviceWidth || py >= deviceHeight) {
        // Outside the surface. Report background, exactly like the
        // out-of-bounds branch of recordPickCopy in render/bundle/pick.go.
        failPending(request);
        return false;
      }

      var plan = sceneWebGPUPickIDPlan(bundle);
      var screenToRay = sceneWebGPUPickSharedFn("sceneScreenToRay");
      if (!screenToRay) {
        failPending(request);
        return false;
      }
      var ray = screenToRay(request.x, request.y, request.width, request.height, bundle.camera);
      if (!ensureResources() || !ensureTargets(deviceWidth, deviceHeight)) {
        failPending(request);
        return false;
      }

      var staging = null;
      try {
        staging = device.createBuffer({
          label: "gosx-pick-staging",
          size: SCENE_WEBGPU_PICK_ROW_ALIGNMENT,
          usage: GPUBufferUsage.MAP_READ | GPUBufferUsage.COPY_DST,
        });
      } catch (err) {
        stats.lastError = String(err && (err.message || err) || "pick-staging-failed");
        failPending(request);
        return false;
      }

      // Fill every uniform slot BEFORE the pass. queue.writeBuffer lands ahead
      // of the submitted commands, so per-draw writes would all collapse onto
      // the last value.
      var slots = uploadPickUniforms(
        plan,
        typeof adapter.viewProjection === "function" ? adapter.viewProjection() : null,
        bundle
      );

      var pass = encoder.beginRenderPass({
        label: "gosx-pick-pass",
        colorAttachments: [{
          view: idView,
          loadOp: "clear",
          storeOp: "store",
          clearValue: { r: 0, g: 0, b: 0, a: 0 },
        }],
        depthStencilAttachment: {
          view: depthView,
          depthLoadOp: "clear",
          depthClearValue: 1.0,
          depthStoreOp: "discard",
        },
      });
      // Only the pointer pixel is ever read back, so scissor the pass to it.
      // Vertex work still runs, fragment work does not.
      pass.setScissorRect(px, py, 1, 1);
      var draws = drawPickPass(pass, bundle, plan, slots);
      pass.end();

      encoder.copyTextureToBuffer(
        { texture: idTexture, origin: { x: px, y: py, z: 0 } },
        { buffer: staging, bytesPerRow: SCENE_WEBGPU_PICK_ROW_ALIGNMENT, rowsPerImage: 1 },
        { width: 1, height: 1, depthOrArrayLayers: 1 }
      );

      request.staging = staging;
      // Retained objects intentionally reuse one compact model-matrix array
      // across animation frames. Pick readback completes later, so snapshot
      // only those 64-byte matrices here; otherwise CPU hit refinement could
      // observe a newer transform than the GPU ID pass rasterized.
      request.bundle = sceneWebGPUPickSnapshotBundle(bundle);
      request.plan = plan;
      request.ray = ray;
      request.submitted = true;
      stats.passes += 1;
      stats.draws += draws;
      return true;
    }

    // drawPickPass records one draw per plan entry, each bound to its own
    // uniform slot through a dynamic offset. `slots` is the number of entries
    // the uniform buffer can serve; entries past it are dropped, not misdrawn.
    function drawPickPass(pass, bundle, plan, slots) {
      var draws = 0;
      var meshRecord = typeof adapter.meshPositions === "function" ? adapter.meshPositions() : null;
      var meshObjects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
      var instanced = Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes : [];
      var boundPipeline = "";

      for (var e = 0; e < plan.entries.length && e < slots; e += 1) {
        var entry = plan.entries[e];
        if (!entry) continue;
        var offset = [entry.slot * SCENE_WEBGPU_PICK_ROW_ALIGNMENT];

        if (entry.group === "mesh") {
          var obj = meshObjects[entry.index];
          if (!obj) continue;
          var count = Math.floor(sceneNumber(obj.vertexCount, 0));
          if (count <= 0) continue;
          if (boundPipeline !== "mesh") {
            pass.setPipeline(wgpuRequirePipeline(pipeline));
            boundPipeline = "mesh";
          }
          pass.setBindGroup(0, bindGroup, offset);
          if (!adapter.bindMeshPositions(pass, 0, meshRecord, sceneNumber(obj.vertexOffset, 0), count, obj)) continue;
          pass.draw(count);
          draws += 1;
          continue;
        }

        var mesh = instanced[entry.index];
        if (!mesh) continue;
        var instanceCount = adapter.instancedCount(mesh);
        var transformData = adapter.instancedTransforms(mesh, instanceCount);
        if (!transformData) continue;
        var geom = adapter.instancedGeometry(mesh);
        if (!geom || geom.vertexCount <= 0) continue;
        if (boundPipeline !== "instanced") {
          pass.setPipeline(wgpuRequirePipeline(instancedPipeline));
          boundPipeline = "instanced";
        }
        pass.setBindGroup(0, bindGroup, offset);
        pass.setVertexBuffer(0, adapter.instancedGeometryBuffer(geom, "_gosxWGPUInstancedShadowPositionBuffer", geom.positions));
        pass.setVertexBuffer(1, adapter.instancedTransformBuffer(mesh, transformData));
        // Draw every instance in one call so occlusion between a mesh's own
        // instances is right. The shader stamps baseID + instance_index, so the
        // read-back ID still names one exact instance.
        pass.draw(geom.vertexCount, Math.min(instanceCount, entry.count));
        draws += 1;
      }
      return draws;
    }

    // finishReadback starts the async map for a submitted request. It never
    // blocks: mapAsync resolves on a later task, and the callback runs then.
    // Call it right after device.queue.submit.
    function finishReadback() {
      // Drain superseded requests first so their staging buffers are freed even
      // though nothing consumes their result.
      if (retired.length > 0) {
        var stale = retired;
        retired = [];
        for (var i = 0; i < stale.length; i += 1) startReadback(stale[i]);
      }
      return startReadback(pending);
    }

    function startReadback(request) {
      if (!request || !request.submitted || !request.staging || request.reading) return false;
      request.reading = true;
      var staging = request.staging;
      staging.mapAsync(GPUMapMode.READ).then(function() {
        var id = 0;
        try {
          id = new Uint32Array(staging.getMappedRange(0, 4).slice(0))[0] >>> 0;
        } catch (err) {
          stats.lastError = String(err && (err.message || err) || "pick-map-failed");
        }
        try { staging.unmap(); } catch (unmapErr) { void unmapErr; }
        staging.destroy();
        stats.readbacks += 1;
        stats.lastID = id;
        if (request.superseded) return;
        if (pending === request) pending = null;
        request.callback(sceneWebGPUPickResolve(request.bundle, request.plan, id, request.ray));
      }).catch(function(err) {
        stats.lastError = String(err && (err.message || err) || "pick-readback-failed");
        try { staging.destroy(); } catch (destroyErr) { void destroyErr; }
        if (request.superseded) return;
        if (pending === request) pending = null;
        request.callback(null);
      });
      return true;
    }

    function hasPending() {
      return Boolean(pending);
    }

    function diagnostics() {
      return {
        pickRequests: stats.requests,
        pickDrops: stats.drops,
        pickPasses: stats.passes,
        pickDraws: stats.draws,
        pickSkippedDraws: stats.skipped,
        pickReadbacks: stats.readbacks,
        pickLastID: stats.lastID,
        pickLastError: stats.lastError,
      };
    }

    function dispose() {
      var abandoned = retired.concat(pending ? [pending] : []);
      for (var i = 0; i < abandoned.length; i += 1) {
        var request = abandoned[i];
        request.superseded = true;
        // A request already reading owns its own destroy in the map handler.
        if (request.staging && !request.reading) {
          try { request.staging.destroy(); } catch (err) { void err; }
        }
      }
      retired = [];
      pending = null;
      if (idTexture) idTexture.destroy();
      if (depthTexture) depthTexture.destroy();
      if (uniformBuffer) uniformBuffer.destroy();
      idTexture = depthTexture = uniformBuffer = null;
      idView = depthView = bindGroup = null;
      pipeline = instancedPipeline = null;
      uniformData = uniformIDs = null;
      idWidth = idHeight = uniformSlots = 0;
    }

    return {
      preparePipelines: ensureResources,
      queuePick: queuePick,
      recordPickPass: recordPickPass,
      finishReadback: finishReadback,
      hasPending: hasPending,
      diagnostics: diagnostics,
      dispose: dispose,
    };
  }

  // Publish the pure pick helpers so tests and tooling can check them against
  // the WebGL2 pick contract without a GPU.
  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    window.__gosx_scene3d_api.sceneWebGPUPickIDPlan = sceneWebGPUPickIDPlan;
    window.__gosx_scene3d_api.sceneWebGPUPickEntryForID = sceneWebGPUPickEntryForID;
    window.__gosx_scene3d_api.sceneWebGPUPickAllowsObject = sceneWebGPUPickAllowsObject;
    window.__gosx_scene3d_api.sceneWebGPUPickResolve = sceneWebGPUPickResolve;
    window.__gosx_scene3d_api.createSceneWebGPUPicker = createSceneWebGPUPicker;
  }

  // -----------------------------------------------------------------------
  // Lights
  // -----------------------------------------------------------------------
  //
  // GoSX declares seven light kinds. This renderer shades six of them
  // directly and folds LightProbe into the ambient term:
  //
  //   ambient      -> code 0, flat term
  //   light-probe  -> code 0, flat term (coefficients ignored)
  //   directional  -> code 1, plus the two shadow slots
  //   point        -> code 2, distance falloff
  //   spot         -> code 3, cone falloff times distance falloff
  //   hemisphere   -> code 4, sky/ground blend by normal Y
  //   rect-area    -> code 5, polygon form factor plus representative-point
  //                   specular
  //
  // Codes 0..4 are the same numbers the WebGL2 renderer writes, so a scene
  // reads the same type on both GPU backends.

  // One Light is 7 * vec4f. Keep in step with the WGSL struct.
  var SCENE_WEBGPU_LIGHT_FLOATS = 28;
  var SCENE_WEBGPU_LIGHT_BYTES = SCENE_WEBGPU_LIGHT_FLOATS * 4;

  // The storage buffer starts at 8 lights and doubles on demand. WGSL bounds
  // the loop with arrayLength(&lights), so the cap below only limits how much
  // memory one scene may claim. Exceeding it reports through reportIssue
  // instead of dropping lights in silence.
  var SCENE_WEBGPU_LIGHT_CAPACITY_MIN = 8;
  var SCENE_WEBGPU_LIGHT_CAPACITY_MAX = 256;

  // sceneWebGPULightCapacityFor returns the storage capacity that holds count
  // lights: a power of two, at least the minimum, never past the maximum.
  function sceneWebGPULightCapacityFor(count) {
    var wanted = Math.max(0, Math.floor(Number(count) || 0));
    var capacity = SCENE_WEBGPU_LIGHT_CAPACITY_MIN;
    while (capacity < wanted && capacity < SCENE_WEBGPU_LIGHT_CAPACITY_MAX) {
      capacity = capacity * 2;
    }
    return Math.min(capacity, SCENE_WEBGPU_LIGHT_CAPACITY_MAX);
  }

  // sceneWebGPULightTypeCode maps a light IR kind to its WGSL type code.
  //
  // light-probe maps to ambient, not point. A LightProbe carries no position,
  // so a point light would invent a falloff the author never asked for. The
  // WebGL2 renderer makes the same choice, which keeps the two backends equal.
  // Neither renderer reads LightProbe.Coefficients; see the light-probe-sh
  // cell in scene/capability/capability.go.
  function sceneWebGPULightTypeCode(kind) {
    switch (kind) {
      case "ambient": return 0;
      case "light-probe": return 0;
      case "directional": return 1;
      case "point": return 2;
      case "spot": return 3;
      case "hemisphere": return 4;
      case "rect-area": return 5;
      default: return 2;
    }
  }

  // sceneWebGPUCachedColor parses a light color once per distinct string.
  function sceneWebGPUCachedColor(value, fallback, cache) {
    var rgba = typeof value === "string" && cache ? cache[value] : null;
    if (rgba) {
      return rgba;
    }
    rgba = sceneColorRGBA(value, fallback);
    if (typeof value === "string" && cache) {
      cache[value] = rgba;
    }
    return rgba;
  }

  // sceneWebGPURectAreaBasis writes the world-space half-width and half-height
  // vectors of a rect-area light into out[0..5].
  //
  // GoSX authors a RectAreaLight as a position, an emission direction, a width
  // and a height. The WGSL form factor needs the two in-plane edge vectors, so
  // build an orthonormal frame around the direction here, once per light per
  // frame, instead of per fragment.
  //
  // Sign convention: three.js lights the side where
  // dot(cross(halfWidth, halfHeight), P - center) < 0. Negating the up vector
  // puts that lit side on the +direction side, so a rect-area light shines the
  // way its Direction points, like every other GoSX light.
  function sceneWebGPURectAreaBasis(light, out) {
    var nx = sceneNumber(light && light.directionX, 0);
    var ny = sceneNumber(light && light.directionY, -1);
    var nz = sceneNumber(light && light.directionZ, 0);
    var nlen = Math.sqrt((nx * nx) + (ny * ny) + (nz * nz));
    if (!(nlen > 0.000001)) {
      nx = 0; ny = -1; nz = 0; nlen = 1;
    }
    nx = nx / nlen; ny = ny / nlen; nz = nz / nlen;

    // Pick the world axis least aligned with the normal so the cross product
    // stays well conditioned.
    var ax = 0, ay = 0, az = 1;
    if (Math.abs(nz) > 0.9) {
      ax = 1; ay = 0; az = 0;
    }
    var rx = (ay * nz) - (az * ny);
    var ry = (az * nx) - (ax * nz);
    var rz = (ax * ny) - (ay * nx);
    var rlen = Math.sqrt((rx * rx) + (ry * ry) + (rz * rz));
    if (!(rlen > 0.000001)) {
      rx = 1; ry = 0; rz = 0; rlen = 1;
    }
    rx = rx / rlen; ry = ry / rlen; rz = rz / rlen;

    var ux = (ny * rz) - (nz * ry);
    var uy = (nz * rx) - (nx * rz);
    var uz = (nx * ry) - (ny * rx);

    var halfW = Math.max(0, sceneNumber(light && light.width, 1)) * 0.5;
    var halfH = Math.max(0, sceneNumber(light && light.height, 1)) * 0.5;
    out[0] = rx * halfW;
    out[1] = ry * halfW;
    out[2] = rz * halfW;
    out[3] = -ux * halfH;
    out[4] = -uy * halfH;
    out[5] = -uz * halfH;
    return out;
  }

  var _sceneWebGPURectBasis = new Float32Array(6);

  // sceneWebGPUPackLights writes count lights into out as SCENE_WEBGPU_LIGHT_FLOATS
  // floats each, in the WGSL Light layout. It returns a census the caller turns
  // into author diagnostics.
  //
  // Defaults match the WebGL2 renderer field for field, so neither backend
  // invents a value the other does not have. That includes angle: an unset
  // cone angle stays 0 on both backends, which draws nothing. The caller
  // reports that case instead of silently widening the cone here.
  function sceneWebGPUPackLights(lightArray, count, out, colorCache, physical = false) {
    var census = {
      count: 0,
      spot: 0,
      spotEmptyCone: 0,
      hemisphere: 0,
      rectArea: 0,
      lightProbe: 0,
      lightProbeWithCoefficients: 0,
    };
    var list = Array.isArray(lightArray) ? lightArray : [];
    var limit = Math.max(0, Math.min(Math.floor(Number(count) || 0), list.length));
    for (var i = 0; i < limit; i++) {
      var light = list[i] || {};
      var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
      var typeCode = sceneWebGPULightTypeCode(kind);
      var base = i * SCENE_WEBGPU_LIGHT_FLOATS;

      // position.xyz + type code
      out[base + 0] = sceneNumber(light.x, 0);
      out[base + 1] = sceneNumber(light.y, 0);
      out[base + 2] = sceneNumber(light.z, 0);
      out[base + 3] = typeCode;

      // direction.xyz + intensity
      out[base + 4] = sceneNumber(light.directionX, 0);
      out[base + 5] = sceneNumber(light.directionY, -1);
      out[base + 6] = sceneNumber(light.directionZ, 0);
      out[base + 7] = sceneNumber(light.intensity, 1);

      // color.rgb + range
      var lc = sceneWebGPUCachedColor(light.color, [1, 1, 1, 1], colorCache);
      out[base + 8] = wgpuLightChannel(lc[0], physical);
      out[base + 9] = wgpuLightChannel(lc[1], physical);
      out[base + 10] = wgpuLightChannel(lc[2], physical);
      out[base + 11] = sceneNumber(light.range, 0);

      // params: decay, shadowBias, castShadow, spot cone angle
      out[base + 12] = sceneNumber(light.decay, 2);
      out[base + 13] = sceneNumber(light.shadowBias, 0.005);
      out[base + 14] = light.castShadow ? 1 : 0;
      out[base + 15] = sceneNumber(light.angle, 0);

      // groundPenumbra: hemisphere ground color + spot penumbra
      var gc = sceneWebGPUCachedColor(light.groundColor, [0, 0, 0, 1], colorCache);
      out[base + 16] = wgpuLightChannel(gc[0], physical);
      out[base + 17] = wgpuLightChannel(gc[1], physical);
      out[base + 18] = wgpuLightChannel(gc[2], physical);
      out[base + 19] = clamp01(sceneNumber(light.penumbra, 0));

      // areaHalfWidth / areaHalfHeight: rect-area edge vectors, else zero.
      if (typeCode === 5) {
        sceneWebGPURectAreaBasis(light, _sceneWebGPURectBasis);
        out[base + 20] = _sceneWebGPURectBasis[0];
        out[base + 21] = _sceneWebGPURectBasis[1];
        out[base + 22] = _sceneWebGPURectBasis[2];
        out[base + 24] = _sceneWebGPURectBasis[3];
        out[base + 25] = _sceneWebGPURectBasis[4];
        out[base + 26] = _sceneWebGPURectBasis[5];
      } else {
        out[base + 20] = 0;
        out[base + 21] = 0;
        out[base + 22] = 0;
        out[base + 24] = 0;
        out[base + 25] = 0;
        out[base + 26] = 0;
      }
      out[base + 23] = 0;
      out[base + 27] = 0;

      census.count += 1;
      if (typeCode === 3) {
        census.spot += 1;
        if (!(sceneNumber(light.angle, 0) > 0)) {
          census.spotEmptyCone += 1;
        }
      } else if (typeCode === 4) {
        census.hemisphere += 1;
      } else if (typeCode === 5) {
        census.rectArea += 1;
      }
      if (kind === "light-probe") {
        census.lightProbe += 1;
        if (Array.isArray(light.coefficients) && light.coefficients.length > 0) {
          census.lightProbeWithCoefficients += 1;
        }
      }
    }
    return census;
  }

  // sceneWebGPUReportLightingIssue routes one lighting diagnostic to the shared
  // issue channel. Authors cannot see a wrong image on a backend they do not
  // run, so every known deviation reports itself.
  function sceneWebGPUReportLightingIssue(record) {
    try {
      if (typeof window !== "undefined" && window.__gosx && typeof window.__gosx.reportIssue === "function") {
        window.__gosx.reportIssue(record);
        return true;
      }
    } catch (e) {
      // Fall through to the console below.
    }
    if (typeof console !== "undefined" && typeof console.warn === "function") {
      console.warn("[gosx] scene3d lighting: " + (record && record.message));
    }
    return false;
  }

  // sceneWebGPULightIssues turns a census plus a light total into the list of
  // diagnostics an author must see. Pure, so a test can check it without a GPU.
  function sceneWebGPULightIssues(census, requested, capacity) {
    var issues = [];
    var total = Math.max(0, Math.floor(Number(requested) || 0));
    var cap = Math.max(0, Math.floor(Number(capacity) || 0));
    if (total > cap) {
      issues.push({
        code: "light-cap",
        message: total + " lights declared; WebGPU draws the first " + cap + ".",
      });
    }
    if (census && census.spotEmptyCone > 0) {
      issues.push({
        code: "spot-empty-cone",
        message: census.spotEmptyCone + " spot light(s) have Angle 0: the cone is empty and adds no light.",
      });
    }
    if (census && census.rectArea > 0) {
      issues.push({
        code: "rect-area-specular",
        message: census.rectArea + " rect-area light(s): diffuse is exact, specular is a representative-point approximation (no LTC tables).",
      });
    }
    if (census && census.lightProbeWithCoefficients > 0) {
      issues.push({
        code: "light-probe-sh",
        message: census.lightProbeWithCoefficients + " light probe(s): spherical-harmonic coefficients are ignored, the probe shades as ambient.",
      });
    }
    return issues;
  }

  // Publish the pure light helpers so tests can check the packing, the type
  // mapping, and the diagnostics without a GPU.
  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    window.__gosx_scene3d_api.sceneWebGPULightTypeCode = sceneWebGPULightTypeCode;
    window.__gosx_scene3d_api.sceneWebGPUPackLights = sceneWebGPUPackLights;
    window.__gosx_scene3d_api.sceneWebGPURectAreaBasis = sceneWebGPURectAreaBasis;
    window.__gosx_scene3d_api.sceneWebGPULightCapacityFor = sceneWebGPULightCapacityFor;
    window.__gosx_scene3d_api.sceneWebGPULightIssues = sceneWebGPULightIssues;
    window.__gosx_scene3d_api.SCENE_WEBGPU_LIGHT_FLOATS = SCENE_WEBGPU_LIGHT_FLOATS;
    window.__gosx_scene3d_api.SCENE_WEBGPU_LIGHT_BYTES = SCENE_WEBGPU_LIGHT_BYTES;
    window.__gosx_scene3d_api.SCENE_WEBGPU_LIGHT_CAPACITY_MAX = SCENE_WEBGPU_LIGHT_CAPACITY_MAX;
  }

  // SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES is the instance count above which
  // the renderer's own cull kernel pays for itself. Below it, one compute
  // dispatch, one uniform upload and one indirect draw cost more than drawing
  // every instance.
  var SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES = 256;

  // -----------------------------------------------------------------------
  // Render bundles
  // -----------------------------------------------------------------------
  //
  // A GPURenderBundle records a set of draws once and replays them with one
  // executeBundles call. Replay removes every per-draw crossing into the
  // browser's WebGPU implementation, and it removes the per-draw validation the
  // implementation repeats on a plain pass.
  //
  // The hard part is invalidation. A bundle holds its pipelines, bind groups and
  // buffers BY REFERENCE. Buffer CONTENTS may change freely — a uniform written
  // with queue.writeBuffer between frames reaches the replayed draw, which is
  // the whole point. What must never change is the command stream itself: the
  // object identities and the numeric arguments. Replaying a bundle whose draw
  // set moved on renders the wrong image, silently.
  //
  // This renderer does not guess at the determinants. It records the exact
  // command stream a frame WOULD encode, using a recorder that implements the
  // encoder subset the draw functions call, and compares that stream against the
  // stream the cached bundle was built from. The comparison is the command
  // stream, so it has no blind spots by construction: any change in a pipeline,
  // a bind group, a buffer, an offset, a vertex count or an instance count
  // changes a token and forces a re-encode.
  //
  // The recorder costs one walk of the draw list per frame and issues no WebGPU
  // calls. The saving is every setPipeline, setBindGroup, setVertexBuffer and
  // draw call the frame would otherwise make.

  var SCENE_WEBGPU_BUNDLE_OP_PIPELINE = 1;
  var SCENE_WEBGPU_BUNDLE_OP_BIND_GROUP = 2;
  var SCENE_WEBGPU_BUNDLE_OP_VERTEX_BUFFER = 3;
  var SCENE_WEBGPU_BUNDLE_OP_INDEX_BUFFER = 4;
  var SCENE_WEBGPU_BUNDLE_OP_DRAW = 5;
  var SCENE_WEBGPU_BUNDLE_OP_DRAW_INDEXED = 6;
  var SCENE_WEBGPU_BUNDLE_OP_DRAW_INDIRECT = 7;
  var SCENE_WEBGPU_BUNDLE_OP_DRAW_INDEXED_INDIRECT = 8;

  // sceneWebGPUBundleObjectID stamps a stable small integer on a GPU object so
  // the recorder can compare identities with a number rather than a reference.
  // A WeakMap would work too; a hidden property avoids the lookup cost on the
  // hot path and lets a test read the id back.
  function sceneWebGPUBundleObjectID(value, counter) {
    if (value === null || value === undefined) return 0;
    if (typeof value !== "object" && typeof value !== "function") return 0;
    var id = value.__gosxWGPUBundleID;
    if (typeof id === "number") return id;
    id = counter.next++;
    try {
      Object.defineProperty(value, "__gosxWGPUBundleID", {
        value: id,
        enumerable: false,
        writable: false,
        configurable: false,
      });
    } catch (_stampError) {
      // A frozen or exotic object cannot carry the stamp. Return 0, which never
      // matches a stamped id, so such a frame simply never replays a bundle.
      return 0;
    }
    return id;
  }

  // createSceneWebGPUDrawRecorder returns an object with the GPURenderPassEncoder
  // subset the bundled draw paths use. It appends every call to a reusable
  // numeric token array. It never allocates after the first few frames.
  function createSceneWebGPUDrawRecorder() {
    var tokens = [];
    var length = 0;
    var counter = { next: 1 };
    var drawCount = 0;
    var unsupported = 0;

    function push(value) {
      tokens[length++] = value;
    }

    function pushID(value) {
      var id = sceneWebGPUBundleObjectID(value, counter);
      if (id === 0 && value !== null && value !== undefined) unsupported++;
      tokens[length++] = id;
    }

    return {
      reset: function() {
        length = 0;
        drawCount = 0;
        unsupported = 0;
      },
      length: function() { return length; },
      drawCount: function() { return drawCount; },
      // unsupportedCount counts objects that refused an identity stamp. A
      // non-zero count makes the frame ineligible rather than risking a false
      // match on id 0.
      unsupportedCount: function() { return unsupported; },
      tokens: function() { return tokens; },

      setPipeline: function(pipeline) {
        push(SCENE_WEBGPU_BUNDLE_OP_PIPELINE);
        pushID(pipeline);
      },
      setBindGroup: function(index, group, offsets) {
        push(SCENE_WEBGPU_BUNDLE_OP_BIND_GROUP);
        push(index);
        pushID(group);
        // Dynamic offsets are numbers, and a changed offset changes the draw, so
        // they belong in the stream.
        if (offsets === null || offsets === undefined) {
          push(-1);
        } else if (typeof offsets === "number") {
          push(1);
          push(offsets);
        } else {
          push(offsets.length);
          for (var i = 0; i < offsets.length; i++) push(offsets[i]);
        }
      },
      setVertexBuffer: function(slot, buffer, offset, size) {
        push(SCENE_WEBGPU_BUNDLE_OP_VERTEX_BUFFER);
        push(slot);
        pushID(buffer);
        push(offset === undefined ? 0 : offset);
        push(size === undefined ? -1 : size);
      },
      setIndexBuffer: function(buffer, format, offset, size) {
        push(SCENE_WEBGPU_BUNDLE_OP_INDEX_BUFFER);
        pushID(buffer);
        push(format === "uint16" ? 16 : 32);
        push(offset === undefined ? 0 : offset);
        push(size === undefined ? -1 : size);
      },
      draw: function(vertexCount, instanceCount, firstVertex, firstInstance) {
        push(SCENE_WEBGPU_BUNDLE_OP_DRAW);
        push(vertexCount);
        push(instanceCount === undefined ? 1 : instanceCount);
        push(firstVertex === undefined ? 0 : firstVertex);
        push(firstInstance === undefined ? 0 : firstInstance);
        drawCount++;
      },
      drawIndexed: function(indexCount, instanceCount, firstIndex, baseVertex, firstInstance) {
        push(SCENE_WEBGPU_BUNDLE_OP_DRAW_INDEXED);
        push(indexCount);
        push(instanceCount === undefined ? 1 : instanceCount);
        push(firstIndex === undefined ? 0 : firstIndex);
        push(baseVertex === undefined ? 0 : baseVertex);
        push(firstInstance === undefined ? 0 : firstInstance);
        drawCount++;
      },
      drawIndirect: function(buffer, offset) {
        push(SCENE_WEBGPU_BUNDLE_OP_DRAW_INDIRECT);
        pushID(buffer);
        push(offset === undefined ? 0 : offset);
        drawCount++;
      },
      drawIndexedIndirect: function(buffer, offset) {
        push(SCENE_WEBGPU_BUNDLE_OP_DRAW_INDEXED_INDIRECT);
        pushID(buffer);
        push(offset === undefined ? 0 : offset);
        drawCount++;
      },
    };
  }

  // sceneWebGPUBundleTokensMatch compares a recorder's current stream against a
  // saved copy. It bails at the first difference.
  function sceneWebGPUBundleTokensMatch(tokens, length, saved) {
    if (!saved || saved.length !== length) return false;
    for (var i = 0; i < length; i++) {
      if (tokens[i] !== saved[i]) return false;
    }
    return true;
  }

  // sceneWebGPUCopyBundleTokens snapshots the recorder's stream. The snapshot is
  // a plain array of numbers, so it holds no reference to a GPU object and keeps
  // nothing alive.
  function sceneWebGPUCopyBundleTokens(tokens, length) {
    var out = new Array(length);
    for (var i = 0; i < length; i++) out[i] = tokens[i];
    return out;
  }

  // createSceneWebGPUBundleCache owns one cached GPURenderBundle and the token
  // stream it was built from. Call plan() with a function that drives an encoder,
  // then act on the verdict.
  function createSceneWebGPUBundleCache() {
    var recorder = createSceneWebGPUDrawRecorder();
    var savedTokens = null;
    var savedLayout = "";
    var bundle = null;
    var encodes = 0;
    var replays = 0;
    var lastDrawCount = 0;
    var consecutiveMisses = 0, previousMissTokens = null;

    return {
      recorder: recorder,
      // plan records the frame's command stream and reports whether the cached
      // bundle still matches it. layoutKey covers everything OUTSIDE the command
      // stream that a bundle encoder bakes in: the colour formats, the depth
      // format and the sample count.
      plan: function(layoutKey, encodeFn) {
        recorder.reset();
        encodeFn(recorder);
        lastDrawCount = recorder.drawCount();
        if (recorder.unsupportedCount() > 0) {
          return { reusable: false, eligible: false, reason: "unstamped-object" };
        }
        if (recorder.drawCount() === 0) {
          return { reusable: false, eligible: false, reason: "no-draws" };
        }
        // Ping-pong bindings never settle. Encode directly until two command
        // streams agree again, so temporary churn does not disable bundles forever.
        if (consecutiveMisses >= 4) {
          if (!sceneWebGPUBundleTokensMatch(recorder.tokens(), recorder.length(), previousMissTokens)) {
            previousMissTokens = sceneWebGPUCopyBundleTokens(recorder.tokens(), recorder.length());
            return { reusable: false, eligible: false, reason: "binding-thrash" };
          }
          consecutiveMisses = 0;
        }
        var reusable = bundle !== null &&
          savedLayout === layoutKey &&
          sceneWebGPUBundleTokensMatch(recorder.tokens(), recorder.length(), savedTokens);
        consecutiveMisses = reusable ? 0 : consecutiveMisses + 1;
        previousMissTokens = reusable ? null : sceneWebGPUCopyBundleTokens(recorder.tokens(), recorder.length());
        if (consecutiveMisses >= 4) return { reusable: false, eligible: false, reason: "binding-thrash" };
        return { reusable: reusable, eligible: true, reason: reusable ? "replay" : "re-encode" };
      },
      // adopt stores a freshly finished bundle together with the stream the
      // recorder holds right now. Call it immediately after plan() reported a
      // miss and the caller encoded for real.
      adopt: function(layoutKey, finished) {
        bundle = finished;
        savedLayout = layoutKey;
        savedTokens = sceneWebGPUCopyBundleTokens(recorder.tokens(), recorder.length());
        encodes++;
      },
      markReplayed: function() { replays++; },
      bundle: function() { return bundle; },
      invalidate: function() {
        bundle = null;
        savedTokens = null;
        savedLayout = "";
        consecutiveMisses = 0; previousMissTokens = null;
      },
      stats: function() {
        return { encodes: encodes, replays: replays, draws: lastDrawCount };
      },
    };
  }

  // sceneWebGPUBundleLayoutKey names the render-target shape a bundle encoder
  // bakes in. A bundle built for one shape is invalid for another.
  function sceneWebGPUBundleLayoutKey(colorFormat, depthFormat, sampleCount) {
    return String(colorFormat) + "|" + String(depthFormat) + "|" + String(sampleCount);
  }

  // sceneWebGPUBundleIneligibleReason names the first scene feature that keeps a
  // frame off the bundled path, or "" when the frame qualifies.
  //
  // The bundled set is the PBR mesh draws plus the instanced mesh draws. Every
  // other draw path stays on the direct encoder, and a frame that holds one of
  // them does not bundle at all. Two reasons:
  //
  //   1. Draw ORDER inside one render pass decides the image. Bundling a subset
  //      and interleaving direct draws around it would need the bundle split at
  //      every boundary, which throws the saving away.
  //   2. The excluded paths rebuild bind groups or reallocate vertex buffers per
  //      frame, so their command stream rarely repeats. Recording them would
  //      cost the walk and never earn a replay.
  //
  // Skinned and computed-morph draws bind buffers a compute pass rewrites.
  // The recorder would catch resource changes; these paths stay direct
  // because they rarely reuse the same command stream.
  function sceneWebGPUBundleIneligibleReason(flags) {
    if (!flags) return "no-flags";
    if (flags.disabled) return "disabled";
    if (flags.gpuDrivenSplit) return "gpu-driven-occlusion";
    if (flags.hasWater) return "water";
    if (flags.hasPoints) return "points";
    if (flags.hasLabels) return "labels";
    if (flags.hasScreenLines) return "screen-lines";
    if (flags.hasSurfaces) return "surfaces";
    if (flags.hasWorldLines) return "world-lines";
    if (flags.hasDynamicMeshes) return "dynamic-meshes";
    if (!flags.hasBundleableDraws) return "nothing-to-bundle";
    return "";
  }

  // SCENE_GPU_DRIVEN_INERT_HOST stands in for the GPU-driven instancing host
  // (indirect-instancing.ts, compute chunk) until that chunk publishes its factory.
  // Every method reports "not handled", so render() needs no branch for it.
  var SCENE_GPU_DRIVEN_INERT_HOST = {
    beginFrame: function() { return false; },
    owns: function() { return false; },
    drawMesh: function() { return false; },
    lightView: function() { return false; },
    drawShadowMesh: function() { return false; },
    splitsMainPass: function() { return false; },
    prepareMainPass: function() { return false; },
    splitMainPass: function() { return arguments[1]; },
    finishEncoding: function() { return false; },
    endFrame: function() { return false; },
    dispose: function() {},
  };

  // sceneWebGPUDrawListHasDynamicMesh reports whether any object in the three
  // pass lists draws through a path the bundled set excludes.
  function sceneWebGPUDrawListHasDynamicMesh(drawList, isDynamic) {
    if (!drawList) return false;
    var passes = ["opaque", "alpha", "additive"];
    for (var p = 0; p < passes.length; p++) {
      var list = drawList[passes[p]];
      if (!Array.isArray(list)) continue;
      for (var i = 0; i < list.length; i++) {
        if (isDynamic(list[i])) return true;
      }
    }
    return false;
  }

  if (typeof window !== "undefined" && window.__gosx_scene3d_api) {
    window.__gosx_scene3d_api.createSceneWebGPUDrawRecorder = createSceneWebGPUDrawRecorder;
    window.__gosx_scene3d_api.createSceneWebGPUBundleCache = createSceneWebGPUBundleCache;
    window.__gosx_scene3d_api.sceneWebGPUBundleLayoutKey = sceneWebGPUBundleLayoutKey;
    window.__gosx_scene3d_api.sceneWebGPUBundleIneligibleReason = sceneWebGPUBundleIneligibleReason;
    window.__gosx_scene3d_api.sceneWebGPUDrawListHasDynamicMesh = sceneWebGPUDrawListHasDynamicMesh;
    window.__gosx_scene3d_api.sceneWebGPUBundleObjectID = sceneWebGPUBundleObjectID;
    window.__gosx_scene3d_api.SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES = SCENE_WEBGPU_BUILTIN_CULL_MIN_INSTANCES;
  }

  // -----------------------------------------------------------------------
  // WebGPU Renderer
  // -----------------------------------------------------------------------

  function sceneWebGPUObjectReflected(obj) {
    var reflectedDirect = obj.directVertices && sceneAffineDeterminant(obj.modelMatrix, 0) < 0;
    return reflectedDirect;
  }

  function sceneWebGPUPipelineKind(reflected, base) {
    return reflected ? base + "-cw" : base;
  }

  function sceneWebGPUSelenaPipelineOptions(obj, reflectedDirect) {
    var selenaPipelineOptions = obj.doubleSided
      ? { cullMode: "none" }
      : (reflectedDirect ? { frontFace: "cw" } : null);
    return selenaPipelineOptions;
  }

  function sceneWebGPUSelenaPipelineSuffix(obj, reflectedDirect) {
    return obj.doubleSided ? ":ds" : (reflectedDirect ? ":cw" : "");
  }

  function sceneSelenaAttributeSource(name) {
    switch (name) {
    case "position": return "positions";
    case "normal": return "normals";
    case "uv": return "uvs";
    default: return "";
    }
  }
/* @ts-expect-error TS2393 -- webgl.ts carries an identical copy of this helper; see the note on its declaration there */
  function sceneSelenaAttributeComponents(type) {
    switch (String(type || "")) {
    case "vec2": return 2;
    case "vec3": return 3;
    case "vec4": return 4;
    // float scalars bind one component. Unknown declared types are invalid
    // descriptor metadata: report 0 so program creation fails closed instead
    // of silently widening a scalar stream to a vec3 fetch. Kept in sync with
    // webgl.ts's copy: in the monolith bundle both files share one scope, and
    // the later-concatenated function declaration silently shadows the other.
    case "float": return 1;
    default: return 0;
    }
  }

  function sceneSelenaWGPUFormat(type) {
    switch (sceneSelenaAttributeComponents(type)) {
    case 2: return "float32x2";
    case 4: return "float32x4";
    default: return "float32x3";
    }
  }

  function selenaPipelineFrontFace(options) {
    return options?.frontFace === "cw" ? "cw" : "ccw";
  }

  function sceneWebGPUPointModelMatrix(entry, _pointsTilt, _pointsModelMat) {
    var pointsModelMat = _pointsModelMat;
    if (entry.parentMatrix) {
      sceneMat4MultiplyInto(_pointsTilt, entry.parentMatrix, _pointsModelMat);
      pointsModelMat = _pointsTilt;
    }
    return pointsModelMat;
  }

  // Shared light matrices use WebGL's [-w,+w] clip-Z convention. WebGPU
  // clips depth to [0,w], so fold z'=(z+w)/2 into a fresh matrix used by
  // both the depth pass and receiver sampling. Keeping one converted value
  // prevents producer/consumer drift and never mutates shared input.
  function sceneWebGPUShadowDepthMatrix(matrix) {
    if (!matrix || matrix.length < 16) return null;
    var out = new Float32Array(16);
    for (var col = 0; col < 4; col++) {
      out[col * 4] = matrix[col * 4];
      out[col * 4 + 1] = matrix[col * 4 + 1];
      out[col * 4 + 2] = (matrix[col * 4 + 2] + matrix[col * 4 + 3]) * 0.5;
      out[col * 4 + 3] = matrix[col * 4 + 3];
    }
    return out;
  }

  // Night layer (Sky.Night): seeded stars on a cube-face grid, a moon disc with phase and its halo. px is the pixel angle in radians.
  var WGSL_SCENE_SKY_NIGHT = [
    "fn gosxNightHash(x0: u32) -> u32 { var x = x0; x ^= x >> 16u; x *= 0x7feb352du; x ^= x >> 15u; x *= 0x846ca68bu; return x ^ (x >> 16u); }",
    "fn gosxNightRand(x: u32) -> f32 { return f32(gosxNightHash(x) >> 8u) / 16777216.0; }",
    "fn gosxNightSky(ray: vec3f, moon: vec4f, star: vec4f, ph: vec4f, px: f32) -> vec3f {",
    " var c = vec3f(0.0); let ct = dot(ray, moon.xyz); var edge = 0.0;",
    " if (ph.z > 0.0 && ct > 0.0) {",
    "  let sinR = sqrt(max(1.0 - moon.w * moon.w, 1e-8));",
    "  let right = normalize(cross(moon.xyz, vec3f(0.0, 1.0, 0.0)) + vec3f(1e-6, 0.0, 0.0));",
    "  let d = vec2f(dot(ray, right), dot(ray, cross(right, moon.xyz))) / sinR;",
    "  let r = length(d); let aa = max(px / sinR, 1e-4);",
    "  edge = 1.0 - smoothstep(1.0 - aa, 1.0 + aa, r);",
    "  let lit = smoothstep(-0.03, 0.06, dot(vec3f(d, sqrt(max(1.0 - r * r, 0.0))), vec3f(ph.x, 0.0, -ph.y)));",
    "  c += edge * ph.z * 1.5 * vec3f(1.0, 0.97, 0.9) * (0.82 + 0.18 * sin(d.x * 5.3 + d.y * 2.1) * sin(d.y * 4.7 - d.x * 1.3)) * (0.006 + 0.994 * lit);",
    " }",
    " let ang = sqrt(max(2.0 * (1.0 - ct), 0.0));",
    " c += ph.w * vec3f(0.5, 0.65, 1.0) * (0.5 * exp(-ang * 18.0) + 0.05 * exp(-ang * 4.0));",
    " if (star.x > 0.0) {",
    "  let a = abs(ray);",
    "  var uv = ray.xy / a.z; var fid = select(5.0, 4.0, ray.z > 0.0);",
    "  if (a.x >= a.y && a.x >= a.z) { uv = ray.yz / a.x; fid = select(1.0, 0.0, ray.x > 0.0); }",
    "  else if (a.y >= a.z) { uv = ray.xz / a.y; fid = select(3.0, 2.0, ray.y > 0.0); }",
    "  let g = (uv * 0.5 + 0.5) * 64.0; let cell = clamp(floor(g), vec2f(0.0), vec2f(63.0)); let f = g - cell; let cc = (cell + 0.5) / 32.0 - 1.0;",
    "  let k = 1.0 + dot(cc, cc);",
    "  let key = u32(cell.x) + 64u * u32(cell.y) + 4096u * u32(fid) + 24576u * u32(star.z);",
    "  if (gosxNightRand(key) < star.y / (k * sqrt(k))) {",
    "   let pos = 0.2 + 0.6 * vec2f(gosxNightRand(key ^ 0x68bc21ebu), gosxNightRand(key ^ 0x02e5be93u));",
    "   var mag = gosxNightRand(key ^ 0x9e3779b9u); mag *= mag; mag *= mag;",
    "   let dist = length(f - pos) / (32.0 * k); let rad = px * (1.1 + 0.8 * mag);",
    "   c += star.x * (1.0 - edge) * smoothstep(0.0, 0.1, ray.y) * exp(-1.5 * dist * dist / (rad * rad)) * (0.12 + 0.88 * mag) * mix(vec3f(1.0, 0.82, 0.66), vec3f(0.7, 0.82, 1.0), gosxNightRand(key ^ 0x85ebca6bu));",
    "  }",
    " }",
    " return c + star.w * vec3f(0.004, 0.007, 0.014) * (1.2 - 0.6 * clamp(ray.y, 0.0, 1.0));",
    "}",
  ].join("\n");

  var WGSL_SCENE_SKY = [
    "struct Sky { right: vec4f, up: vec4f, forward: vec4f, top: vec4f, horizon: vec4f, bottom: vec4f, output: vec4f, betaR: vec4f, betaM: vec4f, sun: vec4f, extra: vec4f, moon: vec4f, stars: vec4f, phase: vec4f };",
    "@group(0) @binding(0) var<uniform> sky: Sky;",
    "@group(0) @binding(1) var skySampler: sampler;",
    "@group(0) @binding(2) var skyImage: texture_2d<f32>;",
    "@group(0) @binding(3) var skyCube: texture_cube<f32>;",
    "struct SkyVertex { @builtin(position) position: vec4f, @location(0) ndc: vec2f };",
    "@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> SkyVertex {",
    "  var p = array<vec2f, 3>(vec2f(-1,-1), vec2f(3,-1), vec2f(-1,3));",
    "  var out: SkyVertex; out.position = vec4f(p[i], 1, 1); out.ndc = p[i]; return out;",
    "}",
    "fn gosxSkyColor(in: SkyVertex) -> vec4f {",
    "  let ray = normalize(sky.forward.xyz + sky.right.xyz * in.ndc.x * sky.right.w + sky.up.xyz * in.ndc.y * sky.up.w); let px = length(dpdx(ray));",
    "  var color = mix(sky.horizon.xyz, select(sky.bottom.xyz, sky.top.xyz, ray.y >= 0), abs(ray.y));",
    "  let c = cos(sky.forward.w); let s = sin(sky.forward.w);",
    "  let d = vec3f(ray.x*c + ray.z*s, ray.y, -ray.x*s + ray.z*c);",
    "  if (sky.bottom.w == 1) {",
    "    let uv = vec2f(atan2(d.z,d.x)/6.28318530718+0.5, asin(clamp(d.y,-1.0,1.0))/3.14159265359+0.5);",
    "    color = textureSampleLevel(skyImage, skySampler, uv, sky.horizon.w).rgb;",
    "  } else if (sky.bottom.w == 2) { color = textureSampleLevel(skyCube, skySampler, d, sky.horizon.w).rgb;",
    "  } else if (sky.bottom.w == 3) { color = sky.horizon.xyz; } else if (sky.bottom.w == 4) { color = gosxPhysicalSky(ray, sky.betaR, sky.betaM, sky.sun, sky.extra.x); if (sky.stars.x > 0 || sky.stars.w > 0 || sky.phase.z > 0) { color = color * (1.0 - 0.85 * sky.stars.w) + gosxNightSky(ray, sky.moon, sky.stars, sky.phase, px) / max(sky.top.w, 0.001); } }",
    "  color = max(color * sky.top.w, vec3f(0));",
    "  if (sky.output.x == 0) { color = select(1.055*pow(color,vec3f(1.0/2.4))-0.055, color*12.92, color <= vec3f(0.0031308)); }",
    "  return vec4f(color, 1);",
    "}",
    "@fragment fn fragmentMain(in: SkyVertex) -> @location(0) vec4f { return gosxSkyColor(in); }",
    "struct GosxSkyOutput { @location(0) color: vec4f, @location(1) specular: vec4f };",
    "@fragment fn fragmentMainSpecular(in: SkyVertex) -> GosxSkyOutput { return GosxSkyOutput(gosxSkyColor(in), vec4f(0)); }",
  ].join("\n");

  // @ts-ignore TS7006 -- shared renderer resources are passed by the backend factory.
  function wgpuCreateSkyRenderer(device, textureCache, imagePlaceholder, cubePlaceholder) {
    var data = new Float32Array(56);
    var uniform = device.createBuffer({ label: "gosx-sky", size: data.byteLength, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    var sampler = device.createSampler({ addressModeU: "repeat", magFilter: "linear", minFilter: "linear", mipmapFilter: "linear" });
    var layout = device.createBindGroupLayout({ entries: [
      { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
      { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
      { binding: 2, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
      { binding: 3, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "cube" } },
    ] });
    var pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: [layout] });
    var module = device.createShaderModule({ label: "gosx-sky", code: WGSL_SCENE_SKY + "\n" + sceneSkyPhysicalSource("wgsl") + "\n" + WGSL_SCENE_SKY_NIGHT });
    var pipelines = new Map();
    // @ts-ignore TS7018 -- bind-group identities become available on the first draw.
    var cached = { group: null, image: null, cube: null };
    return {
      // @ts-ignore TS7006 -- frame inputs follow the shared scene contract.
      draw: function(pass, opts) {
        var env = opts.environment, sky = env.sky;
        sceneSkyUniformData(data, env, opts.view, opts.camera, opts.aspect, opts.linear, true);
        var image = imagePlaceholder, cube = cubePlaceholder, state = sky.mode === "physical" ? "physical" : "gradient";
        if (sky.mode === "environment") {
          var desc = env.ibl && env.ibl.radiance;
          var record = desc && desc.view === "cube" && desc.uri
            ? wgpuLoadTexture(device, desc.uri, textureCache, desc, "environment-radiance", "linear") : null;
          if (record && record.loaded && !record.failed) { cube = record.view; data[23] = 2; state = "environment-cube"; }
          else {
            record = env.envMap ? wgpuLoadTexture(device, env.envMap, textureCache, null, "environment-radiance", "srgb") : record;
            if (record && record.loaded && !record.failed) { image = record.view; data[23] = 1; state = "environment-map"; }
            else state = record && !record.failed ? "environment-pending" : "environment-unavailable";
          }
          data[19] = Math.max(0, sceneNumber(record && record.levels, 1) - 1) * Math.max(0, Math.min(1, sceneNumber(sky.blur, 0)));
        }
        device.queue.writeBuffer(uniform, 0, data);
        var key = opts.format + ":" + opts.samples + ":" + !!opts.specular;
        var pipeline = pipelines.get(key);
        if (!pipeline) {
          pipeline = wgpuCreateValidatedPipeline(device, "render", { label: "gosx-sky", layout: pipelineLayout,
            vertex: { module: module, entryPoint: "vertexMain" },
            fragment: sceneWebGPUColorFragment(module, sceneWebGPUColorTarget(opts.format, !!opts.specular), undefined),
            primitive: { topology: "triangle-list" }, multisample: { count: opts.samples },
            depthStencil: { format: "depth24plus", depthWriteEnabled: false, depthCompare: "always" } });
          pipelines.set(key, pipeline);
        }
        if (!cached.group || image !== cached.image || cube !== cached.cube) {
          cached.group = device.createBindGroup({ layout: layout, entries: [
            { binding: 0, resource: { buffer: uniform } }, { binding: 1, resource: sampler },
            { binding: 2, resource: image }, { binding: 3, resource: cube },
          ] });
          cached.image = image; cached.cube = cube;
        }
        pass.setPipeline(wgpuRequirePipeline(pipeline)); pass.setBindGroup(0, cached.group); pass.draw(3);
        return state;
      },
      dispose: function() { uniform.destroy(); pipelines.clear(); cached.group = null; },
    };
  }

  function wgpuArrayContentsMatch(a: ArrayLike<unknown> | null | undefined, b: ArrayLike<unknown> | null | undefined): boolean {
    if (!a || !b) return !a && !b;
    if (a.length !== b.length) return false;
    if (!a.length) return true;
    if (ArrayBuffer.isView(a) && ArrayBuffer.isView(b) && a.byteLength === b.byteLength && !(a.byteLength % 4 || a.byteOffset % 4 || b.byteOffset % 4)) {
      a = new Uint32Array(a.buffer, a.byteOffset, a.byteLength / 4);
      b = new Uint32Array(b.buffer, b.byteOffset, b.byteLength / 4);
    }
    for (var i = 0; i < a.length; i++) if (a[i] !== b[i]) return false;
    return true;
  }
  function wgpuIsPackedInstanceColors(data: any, count: number): boolean { return data instanceof Float32Array && data.length >= count * 4; }
  function wgpuShadowFrameBindGroup(device: any, groups: any[], layout: any, buffer: any) {
    var cached = groups[0];
    if (!cached || cached.buffer !== buffer) {
      cached = groups[0] = { buffer: buffer, group: device.createBindGroup({
        layout: layout, entries: [{ binding: 0, resource: { buffer: buffer, offset: 0, size: 64 } }],
      }) };
    }
    return cached.group;
  }

  function wgpuBindWorldMeshBuffers(pass: any, buffers: any): boolean {
    if (!buffers) return false;
    var attributes = [buffers.positions, buffers.normals, buffers.uvs, buffers.tangents];
    for (var slot = 0; slot < 4; slot++) {
      var record = attributes[slot];
      if (!record || !record.buffer) return false;
      pass.setVertexBuffer(slot, record.buffer, 0);
    }
    return true;
  }

  function wgpuUpdateFrameProof(stats: any, proof: any) {
    proof.frameSeq = stats.frameSeq || 0;
    proof.frameAt = stats.frameAt || 0;
    proof.waterSystems = stats.waterSystems || 0;
    proof.waterComputeDispatches = stats.waterComputeDispatches || 0;
    proof.waterDrawCalls = stats.waterDrawCalls || 0;
    proof.waterSimulationTickSeq = stats.waterSimulationTickSeq || 0;
    proof.waterSolverSubstepSeq = stats.waterSolverSubstepSeq || 0;
    proof.waterDroppedTicks = stats.waterDroppedTicks || 0;
    proof.waterNormalDispatchSeq = stats.waterNormalDispatchSeq || 0;
    proof.waterSampledStateSyncSeq = stats.waterSampledStateSyncSeq || 0;
    proof.waterAtRestSystems = stats.waterAtRestSystems || 0;
    proof.waterQualityTier = stats.waterQualityTier || "full";
    proof.waterQualityRevision = stats.waterQualityRevision || 0;
    proof.waterSurfaceResolution = stats.waterSurfaceResolution || 0;
    proof.waterCausticsResolution = stats.waterActiveCausticsResolution || 0;
    proof.waterObjectShadowResolution = stats.waterActiveObjectShadowResolution || 0;
    proof.waterObjectTextureWidth = stats.waterActiveObjectTextureWidth || 0;
    proof.waterObjectTextureHeight = stats.waterActiveObjectTextureHeight || 0;
    proof.waterObjectTexturePixelBudget = stats.waterActiveObjectTexturePixelBudget || 0;
    proof.waterQualityAllocationPending = stats.waterQualityAllocationPending || 0;
    proof.waterQualityAllocationFailures = stats.waterQualityAllocationFailures || 0;
    proof.waterQualityAllocationRetryFrame = stats.waterQualityAllocationRetryFrame || 0;
    proof.waterDPRCap = stats.waterQualityDPRCap || 1;
    proof.waterExpensivePassCadence = stats.waterExpensivePassCadence || 1;
  }

  function wgpuResetFrameStats(stats: any, proof: any) {
    for (var key in stats) stats[key] = undefined;
    // Pending pipelines retain the last submitted frame's progress.
    stats.frameSeq = proof.frameSeq || 0;
    stats.frameAt = proof.frameAt;
  }

  function webGPUPerformanceTelemetry() {
    var config = typeof window !== "undefined" && window.__gosx_telemetry_config;
    return typeof window !== "undefined" && (window.__gosx_scene3d_webgpu_telemetry === true ||
      (config && (config.scene3dDiagnostics === true || config.scene3dPerfTelemetry === true)));
  }

  function webGPUVerboseTelemetry() {
    return webGPUPerformanceTelemetry() || (typeof window !== "undefined" && window.__gosx_scene3d_cull_telemetry === true);
  }

  // An instanced PBR batch draws with the plain program, the detail variant
  // (detail === true) or a Selena surface variant (an object). These helpers
  // keep that choice out of the renderer closure.
  function wgpuInstancedSurface(variant: any) { return variant && variant !== true ? variant : null; }
  function wgpuInstancedPipelineParts(detail: any, plainLayout: any, detailResources: any, pbrFragment: any) {
    const surface = wgpuInstancedSurface(detail);
    if (surface) return { name: surface.key + ":pbr-instanced", layout: surface.pipelineLayout, fragment: surface.fragment };
    if (detail) return { name: "pbr-instanced-detail", layout: detailResources.pipelineLayout, fragment: pbrFragment(true) };
    return { name: "pbr-instanced", layout: plainLayout, fragment: pbrFragment(false) };
  }
  function wgpuBindInstancedSurfaceGroup(pass: any, device: any, resources: any, material: any, surface: any, frame: any) {
    if (surface && surface.groupLayout) pass.setBindGroup(2, sceneWebGPUSelenaSurfaceGroup(device, resources, material, surface, frame));
  }
  function wgpuInstancedCullReady(cullSys: any, surface: any) { return Boolean(cullSys && cullSys.isReady() && !surface); }

  // renderLightShadowCascades fills one shadow slot (webgpu-shadows module)
  // and renders every cascade layer. Casters cull once per light against its
  // full-scene fit, a superset of every cascade; each cascade writes its own
  // shadow-matrix arena slot, so no pass overwrites another's matrix.
  function renderLightShadowCascades(s: any, encoder: any, light: any, lightIndex: number, slot: number, rawLightMatrix: any, bounds: any, frame: any) { var uniformState = s.uniformState, drawState = s.drawState;
    var cascades = sceneWebGPUShadowCascadeCount(light, frame.camera);
    var size = Math.max(256, Math.min(4096, sceneNumber(light.shadowSize, 1024)));
    // The authored memory cap covers every cascade layer of the light.
    size = resolveShadowSize(size, frame.maxPixels > 0 ? frame.maxPixels / cascades : frame.maxPixels);
    var shadowSlot = drawState.shadowSlots[slot] = wgpuFitShadowSlot(wgpuEnsureShadowSlot(s.device, drawState.shadowSlots[slot], size, cascades, destroyRendererGPUResource), light, rawLightMatrix,
      { view: uniformState.scratchViewMatrix, camera: frame.camera, aspect: frame.aspect, bounds: bounds });
    var lightMatrix = sceneWebGPUShadowDepthMatrix(rawLightMatrix);
    frame.matrices[slot] = lightMatrix;
    frame.indices[slot] = lightIndex;
    frame.gpuDriven.lightView(encoder, lightMatrix, slot);
    frame.crowd.lightView(frame.crowdEncoder, lightMatrix, slot);
    var casterDraws = 0;
    for (var c = 0; c < cascades; c++) {
      casterDraws += renderShadowPass(drawState, s.device, encoder, shadowSlot.cascades[c].lightMatrix, frame.bundle, { view: shadowSlot.layerViews[c], lightSlot: slot, arenaSlot: c * 2 + slot, arenaSlots: 8 }, frame.buffers);
    }
    // Draws the shadow passes made for casters this frame, over every cascade and light.
    var casterStats = s.telemetryState.frameStats;
    casterStats.shadowCasterDraws = (lightIndex === 0 ? 0 : casterStats.shadowCasterDraws || 0) + casterDraws;
  }
  function render(s: any, bundle, viewport, frameMeta) { var timingState = s.timingState, meshState = s.meshState, uniformState = s.uniformState, pipeState = s.pipeState, selenaState = s.selenaState, waterState = s.waterState, drawState = s.drawState, telemetryState = s.telemetryState, gpuRes = s.gpuRes, pipelineGuard = s.pipelineGuard;
    if (typeof inflateSceneShaderLib === "function") {
      inflateSceneShaderLib(bundle);
    }
    adaptOrtho2DBoardBundle(bundle);
    if (!s.device) {
      startInit();
      return "pending";
    }
    if (s.initFailed || pipelineGuard.coreError) return "failed";
    if (pipelineGuard.pending) return "pending";
    if (!bundle) return "empty";

    var hasPBRData = Boolean(
      bundle.worldMeshPositions &&
      bundle.worldMeshNormals &&
      Array.isArray(bundle.meshObjects) &&
      bundle.meshObjects.length > 0
    );
    var hasPointsData = (Array.isArray(bundle.points) && bundle.points.length > 0) ||
      (Array.isArray(bundle.computeParticles) && bundle.computeParticles.length > 0);
    var hasInstancedData = Array.isArray(bundle.instancedMeshes) && bundle.instancedMeshes.length > 0;
    var hasWorldLines = hasWorldLineData(bundle);
    var hasScreenLines = hasScreenLineData(bundle);
    var hasSurfaces = hasSurfaceData(bundle);
    var hasLabels = hasLabelData(bundle);
    var hasWaterData = Array.isArray(bundle.waterSystems) && bundle.waterSystems.length > 0;
    var incomingWaterShaderSourcesByID = bundle && bundle.waterShaderSourcesByID && typeof bundle.waterShaderSourcesByID === "object" && Object.keys(bundle.waterShaderSourcesByID).length > 0
      ? bundle.waterShaderSourcesByID
      : sceneWaterShaderSourcesFromEntries(bundle && bundle.waterSystems);
    if (s.canvas && s.canvas.parentNode && typeof s.canvas.parentNode.setAttribute === "function") {
      var incomingWaterEntryCausticBytes = 0;
      var incomingWaterEntries = Array.isArray(bundle && bundle.waterSystems) ? bundle.waterSystems : [];
      for (var iwe = 0; iwe < incomingWaterEntries.length; iwe += 1) {
        var iweEntry = incomingWaterEntries[iwe];
        incomingWaterEntryCausticBytes = Math.max(incomingWaterEntryCausticBytes, iweEntry && typeof iweEntry.causticsWGSL === "string" ? iweEntry.causticsWGSL.trim().length : 0);
      }
      var incomingSourceKeys = incomingWaterShaderSourcesByID && typeof incomingWaterShaderSourcesByID === "object" ? Object.keys(incomingWaterShaderSourcesByID) : [];
      queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-incoming-entry-caustic-source-bytes", String(incomingWaterEntryCausticBytes));
      queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-incoming-shader-systems", String(incomingSourceKeys.length));
    }
    if (bundle && Array.isArray(bundle.waterSystems)) {
      bundle.waterSystems = sceneHydrateWaterEntriesFromSources(bundle.waterSystems, incomingWaterShaderSourcesByID);
      bundle.waterShaderSourcesByID = incomingWaterShaderSourcesByID;
    }
    var preparedScene = typeof prepareScene === "function"
      ? prepareScene(bundle, bundle.camera, viewport, s.lastPreparedScene, {
        mount: s.canvas && s.canvas.parentNode || null,
        sentinels: s.canvas && s.canvas.parentNode && s.canvas.parentNode.__gosxScene3DSentinels || null,
      })
      : null;
    if (preparedScene) {
      var waterShaderSourcesByID = bundle && bundle.waterShaderSourcesByID;
      s.lastPreparedScene = preparedScene;
      bundle = preparedScene.ir || bundle;
      if (waterShaderSourcesByID && Object.keys(waterShaderSourcesByID).length > 0 && bundle) {
        bundle.waterShaderSourcesByID = waterShaderSourcesByID;
      }
      if (bundle && Array.isArray(bundle.waterSystems)) {
        bundle.waterSystems = sceneHydrateWaterEntriesFromSources(bundle.waterSystems, incomingWaterShaderSourcesByID);
        if (incomingWaterShaderSourcesByID && Object.keys(incomingWaterShaderSourcesByID).length > 0) {
          bundle.waterShaderSourcesByID = incomingWaterShaderSourcesByID;
        }
      }
      if (s.canvas && s.canvas.parentNode) {
        s.canvas.parentNode.__gosxScene3DCSSDynamic = Boolean(preparedScene.cssDynamic);
      }
      hasPBRData = Boolean(
        bundle.worldMeshPositions &&
        bundle.worldMeshNormals &&
        Array.isArray(bundle.meshObjects) &&
        bundle.meshObjects.length > 0
      );
      hasPointsData = (Array.isArray(bundle.points) && bundle.points.length > 0) ||
        (Array.isArray(bundle.computeParticles) && bundle.computeParticles.length > 0);
      hasInstancedData = Array.isArray(bundle.instancedMeshes) && bundle.instancedMeshes.length > 0;
      hasWorldLines = hasWorldLineData(bundle);
      hasScreenLines = hasScreenLineData(bundle);
      hasSurfaces = hasSurfaceData(bundle);
      hasLabels = hasLabelData(bundle);
      hasWaterData = Array.isArray(bundle.waterSystems) && bundle.waterSystems.length > 0;
    }
    webGPUBeginRetainedMeshFrame(meshState, bundle);
    webGPUPrepareMaterialFrame(uniformState.materialFrame, s.device, bundle, meshState.retainedMeshAttributeEpoch, function(usage, size) { return wgpuCreateTrackedBuffer(meshState, s.device, usage, size); }, destroyRendererGPUResource, meshState.pointsEntryGPUBuffers);
    meshState.instancedCacheOwnerEpoch += 1;
    webGPUSweepInstancedCacheOwners(drawState, s.device);
    if (!hasPBRData && !hasPointsData && !hasInstancedData && !hasWorldLines && !hasScreenLines && !hasSurfaces && !hasLabels && !hasWaterData && !(bundle.environment && (bundle.environment.sky || bundle.environment.ocean || bundle.environment.terrain)) && !s.skyResources.renderer && !s.oceanResources.renderer) {
      webGPUSweepRetainedMeshBuffers(meshState);
      return "empty";
    }

    var width = s.canvas.width;
    var height = s.canvas.height;
    if (width <= 0 || height <= 0) return "empty";

    // Opt-in perf instrumentation. Mirrors the WebGL renderer: when
    // window.__gosx_scene3d_perf is truthy, emit performance.mark +
    // measure pairs so a PerformanceObserver (installed by gosx perf's
    // instrument.js) can collect per-frame wall-clock durations.
    var perfEnabled = typeof window !== "undefined" && window.__gosx_scene3d_perf === true;
    if (perfEnabled) {
      performance.mark("scene3d-render-start");
    }

    // Reconfigure the context ONLY if the surface actually changed (see
    // configureWebGPUCanvas: an unconditional configure() here was a per-frame
    // swapchain re-creation and a hard stall on Metal).
    configureWebGPUCanvas(telemetryState, s.device, s.canvas);

    // Determine post-processing. postFXForceDisabled (set by
    // disablePostProcessing(), the frame-error resilience "demote" step)
    // permanently overrides the authored post-FX chain for this renderer
    // instance — a scene that persistently fails post-FX allocation/
    // validation retries RAW rendering instead of drawing dead frames
    // forever with a poisoned post-FX target.
    bundle = sceneAtmosphereBundle(bundle, frameMeta);
    var postEffects = (Array.isArray(bundle.postEffects) ? bundle.postEffects : []).filter(function(effect) { return !pipelineGuard.disabled.has(effect.kind); });
    postEffects = sceneSpecularBloomEffects(bundle, postEffects, "webgpu", hasWorldLines || hasScreenLines || hasSurfaces || hasPointsData || hasWaterData || !!(bundle.environment && (bundle.environment.ocean || bundle.environment.clouds)), s.canvas.parentNode);
    var authoredPostEffects = postEffects.length > 0;
    var hasTransmission = sceneTransmissionPresent(bundle);
    var transmissionSettings = sceneTransmissionSettings(frameMeta, s.canvas.parentNode);
    transmissionSettings.screen = transmissionSettings.screen && hasTransmission && !s.postFXForceDisabled && !pipelineGuard.disabled.has("post");
    if (transmissionSettings.screen) postEffects = sceneTransmissionEffects(postEffects, bundle.environment);
    var usePostProcessing = postEffects.length > 0 && !s.postFXForceDisabled && !pipelineGuard.disabled.has("post");
    pipeState.targetFormat = usePostProcessing ? "rgba16float" : s.presentationFormat;
    pipeState.selectiveBloom = usePostProcessing && sceneSpecularBloomRequested(postEffects);

    // Compute scaled render-target dimensions (PostFX memory cap).
    var postFXMaxPixels = typeof bundle.postFXMaxPixels === "number" ? bundle.postFXMaxPixels : 0;
    var postfxFactor = usePostProcessing
      ? resolvePostFXFactor(postFXMaxPixels, width * height)
      : 1;
    var scaledW = Math.max(1, Math.floor(width * postfxFactor));
    var scaledH = Math.max(1, Math.floor(height * postfxFactor));
    var sampleCount = resolveWebGPUSampleCount(drawState, bundle);
    pipeState.activeSampleCount = sampleCount;
    pipeState.detailResources = sceneWebGPUEnsureDetailResources(s.device, pipeState.detailResources, bundle.materials, gpuRes.frameBindGroupLayout, gpuRes.materialBindGroupLayout, WGSL_PBR_FRAGMENT);
    pipeState.detailEnabled = frameMeta?.detailEnabled !== false;
    if (pipeState.detailEnabled && pipeState.detailResources) sceneWebGPUDetailBakeResources(s.device, pipeState.detailResources, gpuRes.placeholderView);
    if (drawState.scenePicker && drawState.scenePicker.hasPending()) drawState.scenePicker.preparePipelines();
    prepareBundlePipelines(s, s.device, bundle, preparedScene, postEffects, usePostProcessing, frameMeta?.warmup); s.temporalAA.prepare(postEffects, usePostProcessing, sampleCount); s.autoExposure.prepare(bundle.environment, usePostProcessing);
    if (pipelineGuard.coreError) return "failed";
    if (pipelineGuard.pending) return "pending";
    // The standard mesh, shadow and post variants are prepared above. Other
    // feature renderers can discover pipelines while encoding; defer their
    // canvas commands until the entire frame has encoded successfully.
    pipelineGuard.framePipelinesReady = !(hasPointsData || hasInstancedData || webGPUCrowdHost(drawState, s.device).present(bundle) || hasWorldLines || hasScreenLines || hasSurfaces || hasLabels || hasWaterData || hasTransmission ||
      bundle.environment && (bundle.environment.sky || bundle.environment.ocean || bundle.environment.clouds || bundle.environment.autoExposure || bundle.environment.terrain) ||
      (bundle.materials || []).some(function(material: any) { return sceneWebGPUIsSelenaMaterial(material); }) ||
      postEffects.some(function(effect: any) { return effect.kind === SCENE_POST_CUSTOM_POST || effect.kind === "taa"; }));

    // Upload per-frame uniforms (use scaled dims so point sprites and
    // projection aspect match the actual render target, not the canvas).
    var cam = uploadFrameUniforms(uniformState, gpuRes, s.device,
      bundle.camera,
      scaledW,
      scaledH,
      wgpuToneMapMode(bundle.environment, usePostProcessing)
    );
    uploadLights(uniformState, gpuRes, s.device, bundle.lights, wgpuPhysicalLights(bundle.environment));
    uploadFogUniforms(uniformState, gpuRes, s.device, bundle.environment);
    uploadEnvUniforms(uniformState, gpuRes, s.device, selenaState.frame, bundle.environment);
    if (s.temporalAA.beginFrame(postEffects, { postProcessing: usePostProcessing, perspective: !!cam && cam.kind !== "orthographic" && !(bundle.camera && bundle.camera.mode === "ortho2d"), width: scaledW, height: scaledH, samples: sampleCount, view: uniformState.scratchViewMatrix, projection: uniformState._frameUniformF, projectionOffset: 16, viewProjection: uniformState.scratchSelenaViewProjection, now: performance.now() })) s.device.queue.writeBuffer(gpuRes.frameUniformBuffer, 64, uniformState._frameUniformF, 16, 16);

    // --- Shadow Pass ---
    var shadowLightMatrices = [null, null];
    var shadowLightIndices = [-1, -1];
    var activeShadowCount = 0;

    var encoder = s.device.createCommandEncoder({ label: "gosx-frame" }), crowdEncoder = encoder;
    timingState.gpuTimingFrameSeq += 1;
    drainDeferredGPUResources(timingState, false);
    var gpuTimingToken = beginGPUFrameTiming(timingState, s.device, encoder);
    // Per-pass stamps ride on the render-pass descriptors, so the slot must be
    // chosen before the shadow pass opens.
    pollGPUPassTimingReadback(timingState);
    beginGPUPassTimingFrame(timingState, s.device); pipelineGuard.frameCleanup = function() { if (crowdEncoder !== encoder) { s.device.queue.submit([crowdEncoder.finish()]); crowdEncoder = encoder; } endGPUPassTimingFrame(timingState, encoder); endGPUFrameTiming(timingState, encoder, gpuTimingToken); wgpuFinishGPUDrivenEncoding(gpuDriven, encoder); };
    var scopedFrameErrors = beginWebGPUErrorScope(telemetryState, s.device);
    // Retain ownership before an atlas bake can suspend this frame for pipeline validation.
    sceneWebGPUSelenaSurfaceFrame(pipeState.selenaSurfaces);
    pipeState.detailResources = sceneWebGPUPrepareDetailFrame(s.device, pipeState.detailResources, bundle.materials, gpuRes.textureCache, {
      placeholderView: gpuRes.placeholderView, enabled: pipeState.detailEnabled,
    });
    // Reuse uniforms when color or detail controls change; retire buffers and
    // atlases that no longer belong to the active draw set.

    var frameNowMS = Number.isFinite(frameMeta?.nowMS)
      ? frameMeta.nowMS
      : performance.now();
    var frameActive = frameMeta?.active !== false;
    var frameQualityEnabled = frameMeta?.qualityEnabled === true;
    var frameQualityProfile = frameQualityEnabled && frameMeta.qualityProfile
      ? frameMeta.qualityProfile
      : null;
    var frameQualityRevision = frameQualityEnabled
      ? (Number.isFinite(frameMeta.qualityRevision)
        ? frameMeta.qualityRevision
        : (frameQualityProfile && Number.isFinite(frameQualityProfile.revision)
          ? frameQualityProfile.revision
          : (Number.isFinite(frameMeta.revision) ? frameMeta.revision : 0)))
      : 0;
    var frameTimeSeconds = frameNowMS / 1000;
    // Shader time is seekable; telemetry and compute retain wall time.
    selenaState.frame.time = frameMeta?.sceneTime ?? frameTimeSeconds;
    selenaState.frame.cameraProximity = Math.max(0, Math.min(1, sceneNumber(bundle.cameraProximity, 0)));
    var computeParticleRecords = updateComputeParticleSystems(drawState, s.device, bundle.computeParticles, encoder, frameTimeSeconds);
    var computedMorphStats = updateComputedMorphMeshes(meshState, s.device, bundle, encoder);
    s.temporalAA.capturePreviousSkins(encoder, bundle.meshObjects); var elioSkinStats = updateElioSkinnedMeshes(meshState, s.device, bundle, encoder); window.__gosx_scene3d_ocean_fft_api?.frame(s.oceanResources, { device: s.device, encoder: encoder, environment: bundle.environment, camera: cam, time: selenaState.frame.time, tier: frameMeta?.oceanTier, create: wgpuCreateValidatedPipeline, validation: wgpuPipelineValidation, guard: pipelineGuard });
    var pbrSceneBuffers = hasPBRData ? ensurePBRSceneAttributeBuffers(meshState, s.device, bundle) : null;
    drawState.activePickMeshPositions = pbrSceneBuffers;
    if (incomingWaterShaderSourcesByID && Object.keys(incomingWaterShaderSourcesByID).length > 0) {
      bundle.waterSystems = sceneHydrateWaterEntriesFromSources(bundle.waterSystems, incomingWaterShaderSourcesByID);
      bundle.waterShaderSourcesByID = incomingWaterShaderSourcesByID;
    }
    var waterDebugMode = sceneWebGPUWaterDebugMode(s.canvas);
    var waterUpdateStats = sceneWebGPUWaterDebugSkipsUpdate(waterDebugMode)
      ? updateWaterSystems([], encoder, frameNowMS, frameActive, frameQualityProfile, frameQualityRevision, bundle, pbrSceneBuffers, scaledW, scaledH, waterState, s.device)
      : updateWaterSystems(bundle.waterSystems, encoder, frameNowMS, frameActive, frameQualityProfile, frameQualityRevision, bundle, pbrSceneBuffers, scaledW, scaledH, waterState, s.device);
    // After frame uniform upload, cull before render passes.
    // Meshes without a cull kernel retain draw-all behavior.
    var materials = Array.isArray(bundle.materials) ? bundle.materials : [];
    var instancedDrawList = hasInstancedData
      ? buildInstancedDrawList(drawState, s.device, bundle, materials)
      : { opaque: [], alpha: [], additive: [] };
    var gpuDriven = webGPUGPUDrivenHost(drawState, s.device);
    gpuDriven.beginFrame(bundle, encoder, { viewProjection: uniformState.scratchSelenaViewProjection, camera: cam, width: scaledW, height: scaledH, sampleCount: sampleCount, targetFormat: pipeState.targetFormat, specular: pipeState.selectiveBloom, opaque: pipeState.detailResources ? instancedDrawList.opaque.filter(function(mesh = Object.create(null)) {
      var material = instancedMeshMaterial(mesh, bundle.materials);
      return !material || !material.detail;
    }) : instancedDrawList.opaque });
    var crowd = webGPUCrowdHost(drawState, s.device);
    // The crowd's frustum cull passes (the camera's and each light's) go in a command buffer of their own, submitted before the
    // frame's: on Metal (Apple M4 Pro, Chrome 157) a command buffer that holds them and the frame-end timestamp fails with "Insufficient
    // Memory" and loses the device. Dawn records each submit as one Metal command buffer. The occlusion cull reads this frame's depth.
    crowdEncoder = bundle.gpuDriven ? s.device.createCommandEncoder({ label: "gosx-crowd-cull" }) : encoder;
    crowd.beginFrame(bundle, crowdEncoder, { now: selenaState.frame.time, viewProjection: uniformState.scratchSelenaViewProjection, width: scaledW, height: scaledH, sampleCount: sampleCount, targetFormat: pipeState.targetFormat, specular: pipeState.selectiveBloom });
    updateInstancedCullSystems(drawState, s.device, bundle.instancedMeshes, encoder, uniformState.scratchSelenaViewProjection);
    var webGPUCullTotals = webGPUSummarizeCullSystems(drawState, s.device);

    var lightArray = Array.isArray(bundle.lights) ? bundle.lights : [];
    var sceneBounds = null;
    var shadowMaxPixels = (typeof bundle.shadowMaxPixels === "number") ? bundle.shadowMaxPixels : 0;
    var shadowFrame = { bundle: bundle, camera: cam, maxPixels: shadowMaxPixels, gpuDriven: gpuDriven, crowd: crowd, crowdEncoder: crowdEncoder, aspect: scaledW / scaledH, buffers: pbrSceneBuffers, matrices: shadowLightMatrices, indices: shadowLightIndices };

    for (var li = 0; li < lightArray.length && activeShadowCount < 2; li++) {
      var light = lightArray[li];
      if (!wgpuShadowCaster(light)) continue;

      if (!sceneBounds) sceneBounds = webGPUShadowComputeBounds(drawState, s.device, bundle);

      // Projection validation happens before authored-order slot selection.
      // A malformed or unsupported spot consumes no slot and cannot crowd
      // out a later eligible light.
      var rawLightMatrix = sceneShadowLightSpaceMatrix(light, sceneBounds);
      if (!rawLightMatrix) continue;

      renderLightShadowCascades(s, encoder, light, li, activeShadowCount, rawLightMatrix, sceneBounds, shadowFrame);
      activeShadowCount++;
    }

    // A light removal or newly invalid spot releases its no-longer-active
    // texture immediately. This mirrors WebGL slot retirement instead of
    // retaining stale GPU allocations until whole-renderer disposal.
    for (var staleShadowSlot = activeShadowCount; staleShadowSlot < drawState.shadowSlots.length; staleShadowSlot++) {
      if (drawState.shadowSlots[staleShadowSlot]) {
        destroyRendererGPUResource(drawState.shadowSlots[staleShadowSlot].texture);
        drawState.shadowSlots[staleShadowSlot] = null;
      }
    }

    pipeState.pbrShadowsActive = activeShadowCount > 0;
    uploadShadowUniforms(uniformState, gpuRes, s.device, drawState.shadowSlots, shadowLightMatrices, shadowLightIndices, bundle.lights); sceneSelenaFrameLighting(selenaState.frame, cam, uniformState.scratchViewMatrix, bundle, drawState.shadowSlots, shadowLightIndices, shadowLightMatrices, "gpu");

    // Create frame bind group.
    var shadowView0 = drawState.shadowSlots[0] ? drawState.shadowSlots[0].view : null;
    var shadowView1 = drawState.shadowSlots[1] ? drawState.shadowSlots[1].view : null;
    if (!uniformState.transmissionResources) uniformState.transmissionResources = wgpuCreateTransmissionResources(s.device);
    uniformState.transmissionView = uniformState.transmissionResources.prepare(scaledW, scaledH, pipeState.targetFormat, transmissionSettings);
    if (!uniformState.transmissionView) uniformState.transmissionResources.fallback();
    sceneTransmissionPublish(s.canvas.parentNode, hasTransmission ? uniformState.transmissionView ? "screen" : "environment" : "none");
    var frameBindGroup = createFrameBindGroup(uniformState, gpuRes, s.device, shadowView0, shadowView1);
    var waterObjectSceneTextureStats = sceneWebGPUWaterDebugSkipsDraw(waterDebugMode)
      ? renderWaterObjectSceneTexturePasses([], encoder, bundle, materials, frameBindGroup, pbrSceneBuffers, scaledW, scaledH, !usePostProcessing, waterState, s.device)
      : renderWaterObjectSceneTexturePasses(
        waterUpdateStats.records,
        encoder,
        bundle,
        materials,
        frameBindGroup,
        pbrSceneBuffers,
        scaledW,
        scaledH,
        !usePostProcessing, waterState, s.device
      );
    waterUpdateStats.waterObjectTexturePasses += waterObjectSceneTextureStats.waterObjectTexturePasses;
    waterUpdateStats.waterObjectTextureTargets += waterObjectSceneTextureStats.waterObjectTextureTargets;
    waterUpdateStats.waterObjectTexturePixels += waterObjectSceneTextureStats.waterObjectTexturePixels;
    waterUpdateStats.waterObjectTextureWidth = Math.max(waterUpdateStats.waterObjectTextureWidth || 0, waterObjectSceneTextureStats.waterObjectTextureWidth || 0);
    waterUpdateStats.waterObjectTextureHeight = Math.max(waterUpdateStats.waterObjectTextureHeight || 0, waterObjectSceneTextureStats.waterObjectTextureHeight || 0);
    waterUpdateStats.waterObjectTexturePixelBudget = Math.max(waterUpdateStats.waterObjectTexturePixelBudget || 0, waterObjectSceneTextureStats.waterObjectTexturePixelBudget || 0);
    waterUpdateStats.waterObjectTextureMeshPasses += waterObjectSceneTextureStats.waterObjectTextureMeshPasses;
    waterUpdateStats.waterObjectTextureMeshDrawCalls += waterObjectSceneTextureStats.waterObjectTextureMeshDrawCalls;
    waterUpdateStats.waterObjectTextureSelenaDrawCalls += waterObjectSceneTextureStats.waterObjectTextureSelenaDrawCalls;
    waterUpdateStats.waterObjectTextureFallbackPasses += waterObjectSceneTextureStats.waterObjectTextureFallbackPasses;
    waterUpdateStats.waterObjectTextureCandidateObjects += waterObjectSceneTextureStats.waterObjectTextureCandidateObjects;
    waterUpdateStats.waterObjectTextureSelectedObjects += waterObjectSceneTextureStats.waterObjectTextureSelectedObjects;
    waterUpdateStats.waterObjectTextureFallbackMissingObjects += waterObjectSceneTextureStats.waterObjectTextureFallbackMissingObjects;
    waterUpdateStats.waterObjectTextureFallbackMissingResources += waterObjectSceneTextureStats.waterObjectTextureFallbackMissingResources;
    waterUpdateStats.waterObjectTextureCandidateProfile = waterObjectSceneTextureStats.waterObjectTextureCandidateProfile || waterUpdateStats.waterObjectTextureCandidateProfile;

    // --- Main Render Target ---
    // @ts-ignore TS7034 -- target handles are nullable before the post/direct branch selects them.
    var mainColorView;
    // @ts-ignore TS7034 -- the optional MSAA resolve handle is assigned by the target branch.
    var mainResolveView = null;
    // @ts-ignore TS7034 -- depth handle follows the selected main target.
    var mainDepthTargetView;
    var postTarget = null;

    if (usePostProcessing) {
      if (!s.postProcessor) {
        s.postProcessor = wgpuCreatePostProcessor(s.device, s.presentationFormat, function(message: any) { reportWebGPUFrameError(telemetryState, message); }, function(material, owner, renderContext) {
          return sceneSelenaUniformData(material, owner, renderContext, selenaState.frame);
        });
      }
      postTarget = s.postProcessor.getSceneTarget(scaledW, scaledH);
      if (sampleCount > 1) {
        mainColorView = ensureMSAAColor(pipeState, s.device, pipeState.targetFormat, scaledW, scaledH, sampleCount);
        mainResolveView = postTarget.colorView;
        ensureMainDepth(pipeState, s.device, scaledW, scaledH, sampleCount);
        mainDepthTargetView = pipeState.mainDepthView;
      } else {
        mainColorView = postTarget.colorView;
        mainDepthTargetView = postTarget.depthView;
      }
    } else {
      var currentView = wgpuDeferredCanvasView(pipelineGuard, s.gpuCtx);
      if (sampleCount > 1) {
        mainColorView = ensureMSAAColor(pipeState, s.device, pipeState.targetFormat, width, height, sampleCount);
        mainResolveView = currentView;
      } else {
        mainColorView = currentView;
      }
      ensureMainDepth(pipeState, s.device, width, height, sampleCount);
      mainDepthTargetView = pipeState.mainDepthView;
    }

    // Clear color.
    var bg = sceneRenderBackground(bundle.background, usePostProcessing && !authoredPostEffects ? postEffects[0] : null);


    var mainColorAttachment = {
      view: mainColorView,
      loadOp: "clear",
      storeOp: "store",
      clearValue: { r: bg[0], g: bg[1], b: bg[2], a: bg[3] },
    };
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ if (mainResolveView) {
      mainColorAttachment.resolveTarget = mainResolveView;
    }

    const specularAttachment = s.specularTarget.prepare(scaledW, scaledH, sampleCount, pipeState.selectiveBloom);
    if (s.postProcessor) s.postProcessor.setSpecularSource(s.specularTarget.view());
    var mainPassDescriptor = {
      colorAttachments: specularAttachment ? [mainColorAttachment, specularAttachment] : [mainColorAttachment],
      depthStencilAttachment: {
        view: mainDepthTargetView,
        depthLoadOp: "clear",
        depthClearValue: 1.0,
        depthStoreOp: "store",
      },
    };
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ var mainStamps = gpuPassTimestampWrites(timingState, "main");
    if (mainStamps) mainPassDescriptor.timestampWrites = mainStamps;
    crowd.prepareMainPass(mainPassDescriptor);
    gpuDriven.prepareMainPass(mainPassDescriptor);
    var mainPass = encoder.beginRenderPass(mainPassDescriptor);
    var skyState = "none";
    if (bundle.environment && bundle.environment.sky) {
      if (!s.skyResources.renderer) s.skyResources.renderer = wgpuCreateSkyRenderer(s.device, gpuRes.textureCache, gpuRes.placeholderView, gpuRes.placeholderCubeView);
      skyState = s.skyResources.renderer.draw(mainPass, { environment: bundle.environment, view: uniformState.scratchViewMatrix, camera: cam, aspect: scaledW / scaledH, linear: usePostProcessing, format: pipeState.targetFormat, samples: sampleCount, specular: pipeState.selectiveBloom });
    }
    sceneCloudWebGPUDraw(s.skyResources, s.device, mainPass, { environment: bundle.environment, meta: frameMeta, view: uniformState.scratchViewMatrix, camera: cam, aspect: scaledW/scaledH, linear: usePostProcessing, format: pipeState.targetFormat, samples: sampleCount, timeSeconds: frameTimeSeconds });
    if (s.canvas.parentNode) queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-sky", skyState);


    var drawList = hasPBRData
      ? (preparedScene && preparedScene.pbrPasses ? preparedScene.pbrPasses : buildDrawList(bundle))
      : { opaque: [], alpha: [], additive: [] };
    var thickLineRecord = hasWorldLines ? webGPUThickLineRecord(drawState, bundle) : null;
    var worldLineEntries = hasWorldLines && !thickLineRecord ? webGPUWorldLinePasses(drawState, bundle) : [];
    var pointStats = webGPUPlannedPointStats(bundle, computeParticleRecords);
    var customMaterialStats = webGPUCustomMaterialStats(materials);
    wgpuResetFrameStats(telemetryState.frameStats, telemetryState.webGPUFrameProof);
    telemetryState.frameStats.renderer = "webgpu";
    telemetryState.frameStats.pointEntries = pointStats.pointEntries;
    telemetryState.frameStats.pointInstances = pointStats.pointInstances;
    telemetryState.frameStats.computeParticleEntries = pointStats.computeParticleEntries;
    telemetryState.frameStats.computeParticleInstances = pointStats.computeParticleInstances;
    telemetryState.frameStats.meshObjects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects.length : 0;
    telemetryState.frameStats.retainedMeshObjects = sceneNumber(bundle.retainedMeshObjectCount, 0);
    telemetryState.frameStats.retainedMeshVertices = sceneNumber(bundle.retainedMeshVertexCount, 0);
    telemetryState.frameStats.worldBakedMeshObjects = sceneNumber(bundle.worldBakedMeshObjectCount, 0);
    telemetryState.frameStats.worldBakedMeshVertices = sceneNumber(bundle.worldBakedMeshVertexCount, 0);
    telemetryState.frameStats.bundleBuildCPUms = sceneNumber(bundle.bundleBuildCPUms, 0);
    telemetryState.frameStats.plannerCPUms = sceneNumber(bundle.plannerTelemetry && bundle.plannerTelemetry.lastPlannerCPUms, 0);
    telemetryState.frameStats.plannerFullVertexHashScans = sceneNumber(bundle.plannerTelemetry && bundle.plannerTelemetry.fullVertexHashScans, 0);
      // meshDrawCalls is filled in below by drawPBRObjects (mutated in place
      // across the opaque/alpha/additive passes, mirroring how waterUpdateStats
      // accumulates across many draw functions within one frame) -- it counts
      // actual pass.draw() dispatches, i.e. SUBMITTED mesh objects only.
      // meshViewCulled is the CPU-frustum-culled complement: bundle.meshObjects
      // entries buildDrawList excluded via obj.viewCulled before drawPBRObjects
      // ever saw them. meshObjects (above) counts BOTH -- see the comment on
      // webGPUCountViewCulledMeshObjects for why that ambiguity is the bug this
      // pair of counters exists to close.
    telemetryState.frameStats.meshDrawCalls = 0;
    telemetryState.frameStats.meshViewCulled = webGPUCountViewCulledMeshObjects(bundle);
    telemetryState.frameStats.meshUndrawable = webGPUCountUndrawableMeshObjects(bundle);
    telemetryState.frameStats.skinnedMeshObjects = webGPUCountSkinnedMeshes(bundle);
    telemetryState.frameStats.computedMorphDispatches = computedMorphStats.computedMorphDispatches;
    telemetryState.frameStats.computedMorphVertices = computedMorphStats.computedMorphVertices;
    telemetryState.frameStats.computedMorphKernel = computedMorphStats.computedMorphKernel;
    Object.assign(telemetryState.frameStats, elioSkinStats);
    telemetryState.frameStats.instancedMeshes = Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes.length : 0;
    telemetryState.frameStats.instancedInstances = webGPUPlannedInstanceCount(bundle.instancedMeshes);
    telemetryState.frameStats.cullDispatches = webGPUCullTotals.dispatches;
    telemetryState.frameStats.cullSkippedDispatches = webGPUCullTotals.skipped;
    telemetryState.frameStats.cullBuiltinSystems = webGPUCullTotals.builtin;
    telemetryState.frameStats.lineEntries = thickLineRecord ? 1 : worldLineEntries.length;
    telemetryState.frameStats.surfaceEntries = Array.isArray(bundle.surfaces) ? bundle.surfaces.length : 0;
    telemetryState.frameStats.waterSystems = waterUpdateStats.waterSystems;
    telemetryState.frameStats.waterCells = waterUpdateStats.waterCells;
    telemetryState.frameStats.waterVertices = waterUpdateStats.waterVertices;
    telemetryState.frameStats.waterComputeDispatches = waterUpdateStats.waterComputeDispatches;
    telemetryState.frameStats.waterSimulationTicks = waterUpdateStats.waterSimulationTicks;
    telemetryState.frameStats.waterSolverSubsteps = waterUpdateStats.waterSolverSubsteps;
    telemetryState.frameStats.waterDroppedTicks = waterUpdateStats.waterDroppedTicks;
    telemetryState.frameStats.waterDroppedTicksThisFrame = waterUpdateStats.waterDroppedTicksThisFrame;
    telemetryState.frameStats.waterSimulationCatchUpCap = waterUpdateStats.waterSimulationCatchUpCap;
    telemetryState.frameStats.waterSimulationTickSeq = waterUpdateStats.waterSimulationTickSeq;
    telemetryState.frameStats.waterSolverSubstepSeq = waterUpdateStats.waterSolverSubstepSeq;
    telemetryState.frameStats.waterNormalDispatches = waterUpdateStats.waterNormalDispatches;
    telemetryState.frameStats.waterNormalDispatchSeq = waterUpdateStats.waterNormalDispatchSeq;
    telemetryState.frameStats.waterSampledStateCopies = waterUpdateStats.waterSampledStateCopies;
    telemetryState.frameStats.waterSampledStateSyncSeq = waterUpdateStats.waterSampledStateSyncSeq;
    // Publish the at-rest and upload counters returned by updateWaterSystems.
    telemetryState.frameStats.waterAtRestSystems = waterUpdateStats.waterAtRestSystems;
    telemetryState.frameStats.waterRestSubstepsSkipped = waterUpdateStats.waterRestSubstepsSkipped;
    telemetryState.frameStats.waterUniformUploads = waterUpdateStats.waterUniformUploads;
    telemetryState.frameStats.waterUniformUploadsSkipped = waterUpdateStats.waterUniformUploadsSkipped;
    telemetryState.frameStats.waterQualityTier = waterUpdateStats.waterQualityTier;
    telemetryState.frameStats.waterQualityRevision = waterUpdateStats.waterQualityRevision;
    telemetryState.frameStats.waterSurfaceResolution = waterUpdateStats.waterSurfaceResolution;
    telemetryState.frameStats.waterActiveCausticsResolution = waterUpdateStats.waterActiveCausticsResolution;
    telemetryState.frameStats.waterActiveObjectShadowResolution = waterUpdateStats.waterActiveObjectShadowResolution;
    telemetryState.frameStats.waterActiveObjectTextureWidth = waterUpdateStats.waterActiveObjectTextureWidth;
    telemetryState.frameStats.waterActiveObjectTextureHeight = waterUpdateStats.waterActiveObjectTextureHeight;
    telemetryState.frameStats.waterActiveObjectTexturePixelBudget = waterUpdateStats.waterActiveObjectTexturePixelBudget;
    telemetryState.frameStats.waterQualityAllocationPending = waterUpdateStats.waterQualityAllocationPending;
    telemetryState.frameStats.waterQualityAllocationFailures = waterUpdateStats.waterQualityAllocationFailures;
    telemetryState.frameStats.waterQualityAllocationRetryFrame = waterUpdateStats.waterQualityAllocationRetryFrame;
    telemetryState.frameStats.waterQualityDPRCap = waterUpdateStats.waterQualityDPRCap;
    telemetryState.frameStats.waterExpensivePassCadence = waterUpdateStats.waterExpensivePassCadence;
    telemetryState.frameStats.waterAuthoredComputeSystems = waterUpdateStats.waterAuthoredComputeSystems;
    telemetryState.frameStats.waterAuthoredComputeDispatches = waterUpdateStats.waterAuthoredComputeDispatches;
    telemetryState.frameStats.waterAuthoredComputeFallbacks = waterUpdateStats.waterAuthoredComputeFallbacks;
    telemetryState.frameStats.waterSelenaComputeSystems = waterUpdateStats.waterSelenaComputeSystems;
    telemetryState.frameStats.waterSelenaComputeDispatches = waterUpdateStats.waterSelenaComputeDispatches;
    telemetryState.frameStats.waterSelenaComputeFallbacks = waterUpdateStats.waterSelenaComputeFallbacks;
    telemetryState.frameStats.waterDropDispatches = waterUpdateStats.waterDropDispatches;
    telemetryState.frameStats.waterDropDispatchTotal = waterUpdateStats.waterDropDispatchTotal;
    telemetryState.frameStats.waterLastDropEventID = waterUpdateStats.waterLastDropEventID;
    telemetryState.frameStats.waterObjectSystems = waterUpdateStats.waterObjectSystems;
    telemetryState.frameStats.waterObjectDispatches = waterUpdateStats.waterObjectDispatches;
    telemetryState.frameStats.waterObjectEventDispatches = waterUpdateStats.waterObjectEventDispatches;
    telemetryState.frameStats.waterLastObjectDisplacementEventID = waterUpdateStats.waterLastObjectDisplacementEventID;
    telemetryState.frameStats.waterObjectSpheres = waterUpdateStats.waterObjectSpheres;
    telemetryState.frameStats.waterRoundedSystems = waterUpdateStats.waterRoundedSystems;
    telemetryState.frameStats.waterCornerRadius = waterUpdateStats.waterCornerRadius;
    telemetryState.frameStats.waterLightDirX = waterUpdateStats.waterLightDirX;
    telemetryState.frameStats.waterLightDirY = waterUpdateStats.waterLightDirY;
    telemetryState.frameStats.waterLightDirZ = waterUpdateStats.waterLightDirZ;
    telemetryState.frameStats.waterCausticSystems = waterUpdateStats.waterCausticSystems;
    telemetryState.frameStats.waterCausticPasses = waterUpdateStats.waterCausticPasses;
    telemetryState.frameStats.waterCausticTexturePixels = waterUpdateStats.waterCausticTexturePixels;
    telemetryState.frameStats.waterAuthoredCausticSystems = waterUpdateStats.waterAuthoredCausticSystems;
    telemetryState.frameStats.waterAuthoredCausticPasses = waterUpdateStats.waterAuthoredCausticPasses;
    telemetryState.frameStats.waterAuthoredCausticFallbacks = waterUpdateStats.waterAuthoredCausticFallbacks;
    telemetryState.frameStats.waterAuthoredCausticFallbackReason = waterUpdateStats.waterAuthoredCausticFallbackReason;
    telemetryState.frameStats.waterAuthoredCausticSourceBytes = waterUpdateStats.waterAuthoredCausticSourceBytes;
    telemetryState.frameStats.waterEntryCausticSourceBytes = waterUpdateStats.waterEntryCausticSourceBytes;
    telemetryState.frameStats.waterResolvedCausticSourceBytes = waterUpdateStats.waterResolvedCausticSourceBytes;
    telemetryState.frameStats.waterAuthoredSurfaceSourceBytes = waterUpdateStats.waterAuthoredSurfaceSourceBytes;
    telemetryState.frameStats.waterEntrySurfaceSourceBytes = waterUpdateStats.waterEntrySurfaceSourceBytes;
    telemetryState.frameStats.waterResolvedSurfaceSourceBytes = waterUpdateStats.waterResolvedSurfaceSourceBytes;
    telemetryState.frameStats.waterManifestShaderSystems = waterUpdateStats.waterManifestShaderSystems;
    telemetryState.frameStats.waterManifestShaderFields = waterUpdateStats.waterManifestShaderFields;
    telemetryState.frameStats.waterManifestCausticSourceBytes = waterUpdateStats.waterManifestCausticSourceBytes;
    telemetryState.frameStats.waterManifestSurfaceSourceBytes = waterUpdateStats.waterManifestSurfaceSourceBytes;
    telemetryState.frameStats.waterBundleShaderSystems = waterUpdateStats.waterBundleShaderSystems;
    telemetryState.frameStats.waterBundleCausticSourceBytes = waterUpdateStats.waterBundleCausticSourceBytes;
    telemetryState.frameStats.waterBundleSurfaceSourceBytes = waterUpdateStats.waterBundleSurfaceSourceBytes;
    telemetryState.frameStats.waterObjectTexturePasses = waterUpdateStats.waterObjectTexturePasses;
    telemetryState.frameStats.waterObjectTextureTargets = waterUpdateStats.waterObjectTextureTargets;
    telemetryState.frameStats.waterObjectTexturePixels = waterUpdateStats.waterObjectTexturePixels;
    telemetryState.frameStats.waterObjectTextureWidth = waterUpdateStats.waterObjectTextureWidth;
    telemetryState.frameStats.waterObjectTextureHeight = waterUpdateStats.waterObjectTextureHeight;
    telemetryState.frameStats.waterObjectTexturePixelBudget = waterUpdateStats.waterObjectTexturePixelBudget;
    telemetryState.frameStats.waterObjectTextureMeshPasses = waterUpdateStats.waterObjectTextureMeshPasses;
    telemetryState.frameStats.waterObjectTextureMeshDrawCalls = waterUpdateStats.waterObjectTextureMeshDrawCalls;
    telemetryState.frameStats.waterObjectTextureSelenaDrawCalls = waterUpdateStats.waterObjectTextureSelenaDrawCalls;
    telemetryState.frameStats.waterObjectTextureFallbackPasses = waterUpdateStats.waterObjectTextureFallbackPasses;
    telemetryState.frameStats.waterObjectTextureCandidateObjects = waterUpdateStats.waterObjectTextureCandidateObjects;
    telemetryState.frameStats.waterObjectTextureSelectedObjects = waterUpdateStats.waterObjectTextureSelectedObjects;
    telemetryState.frameStats.waterObjectTextureFallbackMissingObjects = waterUpdateStats.waterObjectTextureFallbackMissingObjects;
    telemetryState.frameStats.waterObjectTextureFallbackMissingResources = waterUpdateStats.waterObjectTextureFallbackMissingResources;
    telemetryState.frameStats.waterObjectTextureCandidateProfile = waterUpdateStats.waterObjectTextureCandidateProfile;
    telemetryState.frameStats.waterObjectShadowPasses = waterUpdateStats.waterObjectShadowPasses;
    telemetryState.frameStats.waterObjectShadowTexturePixels = waterUpdateStats.waterObjectShadowTexturePixels;
    telemetryState.frameStats.waterObjectShadowMeshPasses = waterUpdateStats.waterObjectShadowMeshPasses;
    telemetryState.frameStats.waterObjectShadowMeshDrawCalls = waterUpdateStats.waterObjectShadowMeshDrawCalls;
    telemetryState.frameStats.waterAuthoredObjectShadowPasses = waterUpdateStats.waterAuthoredObjectShadowPasses;
    telemetryState.frameStats.waterAuthoredObjectShadowFallbacks = waterUpdateStats.waterAuthoredObjectShadowFallbacks;
    telemetryState.frameStats.waterAuthoredObjectMeshShadowPasses = waterUpdateStats.waterAuthoredObjectMeshShadowPasses;
    telemetryState.frameStats.waterAuthoredObjectMeshShadowFallbacks = waterUpdateStats.waterAuthoredObjectMeshShadowFallbacks;
    telemetryState.frameStats.waterObjectShadowFallbackPasses = waterUpdateStats.waterObjectShadowFallbackPasses;
    telemetryState.frameStats.waterObjectShadowFallbackMissingObjects = waterUpdateStats.waterObjectShadowFallbackMissingObjects;
    telemetryState.frameStats.waterObjectShadowFallbackMissingResources = waterUpdateStats.waterObjectShadowFallbackMissingResources;
    telemetryState.frameStats.waterReflectionSystems = waterUpdateStats.waterReflectionSystems;
    telemetryState.frameStats.waterRefractionSystems = waterUpdateStats.waterRefractionSystems;
    telemetryState.frameStats.waterObjectOpticsSystems = waterUpdateStats.waterObjectOpticsSystems;
    telemetryState.frameStats.waterSelenaCausticPasses = waterUpdateStats.waterSelenaCausticPasses;
    telemetryState.frameStats.waterSelenaCausticFallbacks = waterUpdateStats.waterSelenaCausticFallbacks;
    telemetryState.frameStats.waterSelenaObjectShadowPasses = waterUpdateStats.waterSelenaObjectShadowPasses;
    telemetryState.frameStats.waterSelenaObjectShadowFallbacks = waterUpdateStats.waterSelenaObjectShadowFallbacks;
    telemetryState.frameStats.waterSelenaObjectMeshShadowPasses = waterUpdateStats.waterSelenaObjectMeshShadowPasses;
    telemetryState.frameStats.waterSelenaObjectMeshShadowFallbacks = waterUpdateStats.waterSelenaObjectMeshShadowFallbacks;
    telemetryState.frameStats.customMaterialFallbacks = customMaterialStats.customMaterialFallbacks;
    telemetryState.frameStats.customWGSLFallbacks = customMaterialStats.customWGSLFallbacks;
    telemetryState.frameStats.customUniformFallbacks = customMaterialStats.customUniformFallbacks;

    // --- Render bundle decision ---
    //
    // Non-bundleable paths retain the direct encoder.
    var bundleableDraws = (hasPBRData && (drawList.opaque.length + drawList.alpha.length + drawList.additive.length) > 0) ||
      (hasInstancedData && (instancedDrawList.opaque.length + instancedDrawList.alpha.length + instancedDrawList.additive.length) > 0);
    var bundleReason = sceneWebGPUBundleIneligibleReason({
      // Both halves must exist. An implementation that can build a bundle but
      // not replay one would leave the frame blank.
      disabled: Boolean(frameMeta?.frameRequest) || pipeState.selectiveBloom || hasTransmission || !webGPURenderBundlesEnabled() || Boolean(bundle.environment && bundle.environment.wind) ||
        typeof s.device.createRenderBundleEncoder !== "function" ||
        typeof mainPass.executeBundles !== "function",
      gpuDrivenSplit: gpuDriven.splitsMainPass(),
      hasWater: hasWaterData || Boolean(bundle.environment && bundle.environment.ocean),
      hasPoints: hasPointsData,
      hasLabels: hasLabels,
      hasScreenLines: hasScreenLines,
      hasSurfaces: hasSurfaces,
      hasWorldLines: hasWorldLines,
      hasDynamicMeshes: sceneWebGPUDrawListHasDynamicMesh(drawList, function(obj) {
        return webGPUObjectBlocksBundle(obj, materials);
      }),
      hasBundleableDraws: bundleableDraws,
    });
    telemetryState.frameStats.bundleState = "direct";
    telemetryState.frameStats.bundleReason = bundleReason;
    telemetryState.frameStats.bundleEncodes = 0;
    telemetryState.frameStats.bundleReplays = 0;
    telemetryState.frameStats.bundleDraws = 0;

    if (bundleReason === "") {
      if (!s.webGPUBundleCache) s.webGPUBundleCache = createSceneWebGPUBundleCache();
      var bundleLayoutKey = sceneWebGPUBundleLayoutKey(pipeState.targetFormat, "depth24plus", sampleCount);
      var bundleContext = {
        drawList: drawList,
        instancedDrawList: instancedDrawList,
        bundle: bundle,
        materials: materials,
        frameBindGroup: frameBindGroup,
        pbrBuffers: pbrSceneBuffers,
      };
      var verdict = s.webGPUBundleCache.plan(bundleLayoutKey, function(recorder) {
        encodeBundleableSceneDraws(drawState, s.device, recorder, bundleContext);
      });
      if (!verdict.eligible) {
        bundleReason = verdict.reason;
        telemetryState.frameStats.bundleReason = bundleReason;
      } else if (verdict.reusable) {
        mainPass.executeBundles([s.webGPUBundleCache.bundle()]);
        s.webGPUBundleCache.markReplayed();
        telemetryState.frameStats.bundleState = "replayed";
      } else {
        var bundleEncoder = s.device.createRenderBundleEncoder({
          label: "gosx-scene-bundle",
          colorFormats: [pipeState.targetFormat],
          depthStencilFormat: "depth24plus",
          sampleCount: sampleCount,
        });
        encodeBundleableSceneDraws(drawState, s.device, bundleEncoder, bundleContext);
        var finishedBundle = bundleEncoder.finish({ label: "gosx-scene-bundle" });
        s.webGPUBundleCache.adopt(bundleLayoutKey, finishedBundle);
        mainPass.executeBundles([finishedBundle]);
        telemetryState.frameStats.bundleState = "encoded";
      }
      if (telemetryState.frameStats.bundleState !== "direct") {
        var bundleStats = s.webGPUBundleCache.stats();
        telemetryState.frameStats.bundleEncodes = bundleStats.encodes;
        telemetryState.frameStats.bundleReplays = bundleStats.replays;
        telemetryState.frameStats.bundleDraws = bundleStats.draws;
      }
    }

    // Draw PBR meshes, WebGPU-native instanced meshes, world lines, and textured surfaces.
    var waterDrawnBeforeAlpha = false, oceanOpts = { publishAttribute: function(name: any, value: any) { queueWebGPUAttribute(telemetryState, name, value); }, device: s.device, environment: bundle.environment, camera: cam, meta: frameMeta, view: uniformState.scratchViewMatrix, aspect: scaledW/scaledH, viewProj: uniformState.scratchSelenaViewProjection, timeSeconds: selenaState.frame.time, quality: frameMeta?.oceanTier, linear: usePostProcessing, format: pipeState.targetFormat, samples: sampleCount, textureCache: gpuRes.textureCache, frameBindGroup: frameBindGroup, mount: s.canvas.parentNode, drawn: false, reflection: {} }; window.__gosx_scene3d_terrain_api?.webgpu(s.terrainResources, mainPass, { device: s.device, environment: bundle.environment, camera: cam, viewProj: uniformState.scratchSelenaViewProjection, height: scaledH, frameGroup: frameBindGroup, frameLayout: gpuRes.frameBindGroupLayout, materialLayout: gpuRes.materialBindGroupLayout, fragment: WGSL_PBR_FRAGMENT, frameStructs: WGSL_FRAME_STRUCTS, material: function(m: any, r: any) { return materialUniformData(uniformState, m, r); }, time: selenaState.frame.time, frameBuffer: gpuRes.frameUniformBuffer, format: pipeState.targetFormat, samples: sampleCount, specular: pipeState.selectiveBloom, textureCache: gpuRes.textureCache, load: wgpuLoadTexture, create: wgpuCreateValidatedPipeline, validation: wgpuPipelineValidation, attr: function(name: any, value: any) { queueWebGPUAttribute(telemetryState, name, value); } });
    // @ts-ignore TS7005 -- plain-JS reflection callback captures optional GPU target handles; bridge types govern its interface.
    const prepareOceanReflection = () => { if (pipelineGuard.disabled.has("reflections")) return; const r = sceneReflectWebGPU(s.oceanResources, s.device, { environment: bundle.environment, meta: frameMeta, pass: mainPass, encoder, descriptor: mainPassDescriptor, width: scaledW, height: scaledH, view: uniformState.scratchViewMatrix, proj: uniformState.scratchProjMatrix, camera: cam, linear: usePostProcessing, format: pipeState.targetFormat, samples: sampleCount, colorView: mainResolveView || mainColorView, depthView: mainDepthTargetView, frameData: uniformState._frameUniformF, frameGroup: (buffer = false) => _createFrameBindGroupUncached(uniformState, gpuRes, s.device, [shadowView0, shadowView1, uniformState.iblResources.active && uniformState.iblResources.irradiance && uniformState.iblResources.irradiance.view, uniformState.iblResources.active && uniformState.iblResources.radiance && uniformState.iblResources.radiance.view, uniformState.iblResources.active && uniformState.iblResources.brdfLUT && uniformState.iblResources.brdfLUT.view, uniformState.envMapResources.active && uniformState.envMapResources.record && uniformState.envMapResources.record.view], buffer), draw: (pass = mainPass,group = frameBindGroup) => { pass.setPipeline(wgpuRequirePipeline(getPBRPipeline(pipeState, s.device, "opaque", true, "cw"))); pass.setBindGroup(0,group); drawPBRObjects(drawState, s.device, pass, sceneReflectOpaqueList(drawList.opaque, materials), bundle, materials, group, "opaque", true, pbrSceneBuffers, null); } }); mainPass = r.pass; oceanOpts.reflection = r.record; }; window.__gosx_scene3d_crowd_api?.webgpu(mainPass, { device: s.device, environment: bundle.environment, camera: cam, proj: uniformState.scratchProjMatrix, viewProj: uniformState.scratchSelenaViewProjection, height: scaledH, time: bundle.timeSeconds, frameGroup: frameBindGroup, frameLayout: gpuRes.frameBindGroupLayout, materialLayout: gpuRes.materialBindGroupLayout, fragment: WGSL_PBR_FRAGMENT, frameStructs: WGSL_FRAME_STRUCTS, material: function(m: any, r: any) { return materialUniformData(uniformState, m, r); }, format: pipeState.targetFormat, samples: sampleCount, specular: pipeState.selectiveBloom, create: wgpuCreateValidatedPipeline, validation: wgpuPipelineValidation, mount: s.canvas.parentNode }); // Environment.Crowds (crowd-wgsl.ts)
    if (telemetryState.frameStats.bundleState === "direct" && (hasPBRData || hasInstancedData || hasWorldLines || hasSurfaces)) {
      // Opaque pass.
      if (drawList.opaque.length > 0) {
        drawPBRObjects(drawState, s.device, mainPass, drawList.opaque, bundle, materials, frameBindGroup, "opaque", true, pbrSceneBuffers, telemetryState.frameStats, frameMeta?.frameRequest);
      }
      telemetryState.frameStats.meshDrawCalls += crowd.drawColor(mainPass, frameBindGroup, materials);
      if (instancedDrawList.opaque.length > 0) {
        mainPass.setBindGroup(0, frameBindGroup);
        drawInstancedMeshes(drawState, s.device, mainPass, instancedDrawList.opaque, materials, "opaque", true);
      }
      if (hasSurfaces) {
        drawSurfaceEntries(drawState, s.device, mainPass, bundle, materials, "opaque", frameBindGroup);
      }
      if (thickLineRecord) {
        drawThickWorldLineEntries(drawState, s.device, mainPass, thickLineRecord, "opaque", frameBindGroup);
      } else if (worldLineEntries.length > 0) {
        drawWorldLineEntries(drawState, s.device, mainPass, worldLineEntries, "opaque", frameBindGroup);
      }

      // Two-phase occlusion: end the early pass, cull against its depth, and
      // draw the newly visible instances in a late pass that loads it.
      mainPass = gpuDriven.splitMainPass(encoder, mainPass, mainPassDescriptor, frameBindGroup, materials, instancedDrawList.opaque);
      mainPass = crowd.splitMainPass(encoder, mainPass, mainPassDescriptor, frameBindGroup, materials);

      prepareOceanReflection();
      // The water surface writes depth before translucent world surfaces.
      // A stele in front of the tide must remain visible after its HTML
      // texture is composited, while rocks behind the tide stay occluded.
      if (hasWaterData && !sceneWebGPUWaterDebugSkipsDraw(waterDebugMode)) {
        Object.assign(telemetryState.frameStats, drawWaterPoolEntries(mainPass, waterUpdateStats.records, frameBindGroup, waterState, s.device));
        Object.assign(telemetryState.frameStats, drawWaterSystemEntries(mainPass, waterUpdateStats.records, frameBindGroup, cam, waterState, s.device));
        waterDrawnBeforeAlpha = true;
      }
      oceanOpts.drawn = wgpuOceanDraw(s.oceanResources, mainPass, oceanOpts);
      if (uniformState.transmissionView && postTarget) {
        mainPass.end();
        uniformState.transmissionResources.capture(encoder, postTarget.colorView);
        gpuDriven.resumeMainPass(mainPassDescriptor);
        crowd.resumeMainPass(mainPassDescriptor);
        mainColorAttachment.loadOp = "load";
        if (specularAttachment) specularAttachment.loadOp = "load";
        mainPassDescriptor.depthStencilAttachment.depthLoadOp = "load";
        delete mainPassDescriptor["timestampWrites"];
        mainPass = encoder.beginRenderPass(mainPassDescriptor);
      }

      // Alpha pass.
      if (drawList.alpha.length > 0) {
        drawPBRObjects(drawState, s.device, mainPass, drawList.alpha, bundle, materials, frameBindGroup, "alpha", false, pbrSceneBuffers, telemetryState.frameStats, frameMeta?.frameRequest);
      }
      if (instancedDrawList.alpha.length > 0) {
        mainPass.setBindGroup(0, frameBindGroup);
        drawInstancedMeshes(drawState, s.device, mainPass, instancedDrawList.alpha, materials, "alpha", false);
      }
      if (hasSurfaces) {
        drawSurfaceEntries(drawState, s.device, mainPass, bundle, materials, "alpha", frameBindGroup);
      }
      if (thickLineRecord) {
        drawThickWorldLineEntries(drawState, s.device, mainPass, thickLineRecord, "alpha", frameBindGroup);
      } else if (worldLineEntries.length > 0) {
        drawWorldLineEntries(drawState, s.device, mainPass, worldLineEntries, "alpha", frameBindGroup);
      }

      // Additive pass.
      if (drawList.additive.length > 0) {
        drawPBRObjects(drawState, s.device, mainPass, drawList.additive, bundle, materials, frameBindGroup, "additive", false, pbrSceneBuffers, telemetryState.frameStats, frameMeta?.frameRequest);
      }
      if (instancedDrawList.additive.length > 0) {
        mainPass.setBindGroup(0, frameBindGroup);
        drawInstancedMeshes(drawState, s.device, mainPass, instancedDrawList.additive, materials, "additive", false);
      }
      if (hasSurfaces) {
        drawSurfaceEntries(drawState, s.device, mainPass, bundle, materials, "additive", frameBindGroup);
      }
      if (thickLineRecord) {
        drawThickWorldLineEntries(drawState, s.device, mainPass, thickLineRecord, "additive", frameBindGroup);
      } else if (worldLineEntries.length > 0) {
        drawWorldLineEntries(drawState, s.device, mainPass, worldLineEntries, "additive", frameBindGroup);
      }
    }

    if (hasWaterData && !waterDrawnBeforeAlpha && !sceneWebGPUWaterDebugSkipsDraw(waterDebugMode)) {
      Object.assign(telemetryState.frameStats, drawWaterPoolEntries(mainPass, waterUpdateStats.records, frameBindGroup, waterState, s.device), drawWaterSystemEntries(mainPass, waterUpdateStats.records, frameBindGroup, cam, waterState, s.device));
    }
    if (!oceanOpts.drawn) { prepareOceanReflection(); wgpuOceanDraw(s.oceanResources, mainPass, oceanOpts); }

    // Board label glyphs (M1 GPU-text slice 2). Drawn after the opaque/alpha
    // board fills so the alpha-blended glyphs composite over the rects. Lives
    // outside the (hasPBRData || …) block above so a labels-only board still
    // paints text. blendMode "alpha", depthWriteEnabled false.
    if (hasLabels) {
      drawBoardLabels(drawState, s.device, mainPass, bundle, "alpha", false);
    }

    if (hasScreenLines) {
      drawScreenLines(drawState, s.device, mainPass, bundle, frameBindGroup);
    }

    // Draw points.
    if (hasPointsData) {
      mainPass.setBindGroup(0, frameBindGroup);
      // Create a dummy material bind group for group 1 (points pipeline layout requires it).
      var dummyMatBG = createMaterialBindGroup(uniformState, gpuRes, meshState, s.device, null, false, gpuRes.defaultMaterialOwner);
      mainPass.setBindGroup(1, dummyMatBG, [0]);
      Object.assign(telemetryState.frameStats, drawPointsEntries(drawState, s.device, mainPass, bundle, cam, selenaState.frame.time));
      mainPass = s.drawComputeParticlePasses(encoder, mainPass, mainPassDescriptor, frameBindGroup, computeParticleRecords, bundle.environment, frameTimeSeconds, mainDepthTargetView);
    }

    mainPass.end();

    // GPU pick pass. Records only when a pick waits, and only ever touches
    // its own ID and depth textures. Runs after the main pass so every vertex
    // and instance-transform buffer the pick draws from already exists.
    // Pick coordinates map onto the main render target, so use the same
    // width/height the main pass used: scaled dimensions under post-FX,
    // canvas dimensions otherwise.
    if (drawState.scenePicker && drawState.scenePicker.hasPending()) {
      drawState.scenePicker.recordPickPass(
        encoder,
        bundle,
        usePostProcessing ? scaledW : width,
        usePostProcessing ? scaledH : height
      );
    }

    // Post-processing.
    if (usePostProcessing && s.postProcessor) {
      var screenView = wgpuDeferredCanvasView(pipelineGuard, s.gpuCtx);
      var chain = s.temporalAA.resolve(encoder, postEffects, { colorView: postTarget.colorView, colorFormat: pipeState.targetFormat, depthView: mainDepthTargetView, samples: sampleCount, objects: bundle.meshObjects, particles: computeParticleRecords, frameGroup: frameBindGroup, bundle: bundle }); s.autoExposure.apply(encoder, bundle.environment, postTarget, cam, frameNowMS);
      Object.assign(telemetryState.frameStats, s.postProcessor.apply(encoder, chain, scaledW, scaledH, width, height, screenView, bundle.camera, { lights: bundle.lights, environment: bundle.environment, camera: cam, viewProj: uniformState.scratchSelenaViewProjection, depthView: mainDepthTargetView, samples: sampleCount, meta: frameMeta, motion: { velocityView: s.temporalAA.velocityView() } }), s.temporalAA.stats(), s.autoExposure.stats());
    }

    // The frame-end marker loads the view post-processing presented; a frame
    // that records its commands until its pipelines are ready (the encoder is
    // then the tracking proxy) gets the canvas view patched in on replay, as
    // post-processing does. It is frame work, so the frame timer ends after it.
    endGPUPassTimingFrame(timingState, encoder, usePostProcessing && s.postProcessor ? screenView : null);
    endGPUFrameTiming(timingState, encoder, gpuTimingToken);
    gpuDriven.finishEncoding(encoder);
    crowd.finishEncoding(encoder);
    if (crowdEncoder !== encoder) { s.device.queue.submit([crowdEncoder.finish()]); crowdEncoder = encoder; }
    s.device.queue.submit([encoder.finish()]);
    Object.assign(telemetryState.frameStats, gpuDriven.endFrame(s.webGPUAttributeSink), crowd.endFrame());
    pollGPUTimingReadback(timingState, s.device);
    // Pick readback starts after submit and does not wait in this frame.
    if (drawState.scenePicker) drawState.scenePicker.finishReadback();
    webGPUSweepRetainedMeshBuffers(meshState);
    Object.assign(telemetryState.frameStats, webGPURetainedMeshFrameStats(meshState));
    publishWebGPUFrameStats(telemetryState, telemetryState.frameStats);
    if (scopedFrameErrors) endWebGPUErrorScope(telemetryState, s.device, true);

    if (perfEnabled) {
      performance.mark("scene3d-render-end");
      performance.measure("scene3d-render", "scene3d-render-start", "scene3d-render-end");
      performance.clearMarks("scene3d-render-start");
      performance.clearMarks("scene3d-render-end");
    }
    s.initialPipelinesReady = true;
    return "submitted";
  }
  function dispose(s: any) { var timingState = s.timingState, meshState = s.meshState, uniformState = s.uniformState, pipeState = s.pipeState, selenaState = s.selenaState, waterState = s.waterState, drawState = s.drawState, telemetryState = s.telemetryState, gpuRes = s.gpuRes, pipelineGuard = s.pipelineGuard; pipelineGuard.release(s.rawDevice);
    if (telemetryState.rendererResourcesDisposed) return;
    telemetryState.rendererResourcesDisposed = true; clearTimeout(telemetryState.webGPUAttributeTimer); telemetryState.webGPUAttributeTimer = null;
    if (s.skyResources.renderer) s.skyResources.renderer.dispose(); s.terrainResources.dispose?.(); sceneCloudDispose(s.skyResources); if (s.oceanResources.renderer) s.oceanResources.renderer.dispose(); s.oceanResources.fft?.dispose(); sceneReflectDispose(s.oceanResources);
    if (uniformState.transmissionResources) uniformState.transmissionResources.dispose(); uniformState.transmissionResources = null; uniformState.transmissionView = null; window.__gosx_scene3d_crowd_api?.dispose(s.device);
    s.skyResources.renderer = null;

    timingState.gpuTimingDisposed = true;
    timingState.lastGPUCompletionSample = null;
    try { destroyGPUTimingResources(timingState.gpuTiming); } catch (_err) {}
    try { destroyGPUPassTimingResources(timingState); } catch (_err) {}
    if (s.webGPUBundleCache) {
      try { s.webGPUBundleCache.invalidate(); } catch (_err) {}
    }
    s.webGPUBundleCache = null;
    timingState.gpuTiming = null;
    for (var failedTimingIndex = 0; failedTimingIndex < timingState.failedGPUTimings.length; failedTimingIndex++) {
      try { destroyGPUTimingResources(timingState.failedGPUTimings[failedTimingIndex].timing); } catch (_err) {}
    }
    timingState.failedGPUTimings.length = 0;
    try { drainDeferredGPUResources(timingState, true); } catch (_err) {}
    timingState.deferredWaterTextureRetirements.length = 0;
    timingState.deferredWaterSystemRetirements.length = 0;
    timingState.lastGPUPerformanceSample = null;
    if (drawState.scenePicker) {
      try { drawState.scenePicker.dispose(); } catch (_err) {}
    }
    drawState.scenePicker = null;
    drawState.activePickMeshPositions = null;
    for (const pair of Array.from(meshState.retainedMeshAttributeCache.entries())) {
      webGPURetireRetainedMeshEntry(meshState, pair[0], pair[1]);
    }

    destroyRendererGPUResource(gpuRes.frameUniformBuffer);
    gpuRes.frameUniformBuffer = null;
    destroyRendererGPUResource(gpuRes.lightStorageBuffer);
    gpuRes.lightStorageBuffer = null;
    // Release the light buffers that capacity growth replaced.
    for (var retiredLight = 0; retiredLight < uniformState._retiredLightBuffers.length; retiredLight++) {
      destroyRendererGPUResource(uniformState._retiredLightBuffers[retiredLight]);
    }
    uniformState._retiredLightBuffers.length = 0;
    destroyRendererGPUResource(gpuRes.fogUniformBuffer);
    gpuRes.fogUniformBuffer = null;
    destroyRendererGPUResource(gpuRes.envUniformBuffer);
    gpuRes.envUniformBuffer = null;
    destroyRendererGPUResource(gpuRes.shadowUniformBuffer);
    gpuRes.shadowUniformBuffer = null;
    destroyRendererGPUResource(meshState.positionBuffer);
    meshState.positionBuffer = null;
    destroyRendererGPUResource(meshState.normalBuffer);
    meshState.normalBuffer = null;
    destroyRendererGPUResource(s.uvBuffer);
    s.uvBuffer = null;
    destroyRendererGPUResource(meshState.tangentBuffer);
    meshState.tangentBuffer = null;
    destroyRendererGPUResource(s.shadowPositionBuffer);
    s.shadowPositionBuffer = null;
    destroyRendererGPUResource(drawState.shadowFrameBuffer);
    drawState.shadowFrameBuffer = null; drawState.shadowFrameBindGroups.length = 0;
    meshState.pointsEntryGPUBuffers.forEach(function(buffer) {
      destroyRendererGPUResource(buffer);
    });
    meshState.pointsEntryGPUBuffers.clear();
    uniformState.materialFrame.resources.clear(); uniformState.materialFrame.groups = []; uniformState.materialFrame.buffer = uniformState.materialFrame.data = null;
    // Board glyph atlases are textures (not tracked in pointsEntryGPUBuffers);
    // destroy them explicitly. The per-label glyph buffers are tracked buffers,
    drawState.boardGlyphAtlases.forEach(function(a) {
      if (a) destroyRendererGPUResource(a.texture);
    });
    drawState.boardGlyphAtlases.clear();
    drawState.boardTextOwners.clear();
    disposeComputeParticleSystems(drawState);
    disposeWaterSystems(waterState);
    for (const record of drawState.instancedCullSystems.values()) {
      if (record && record.system && typeof record.system.dispose === "function") {
        try { record.system.dispose(); } catch (_err) {}
      }
    }
    drawState.instancedCullSystems.clear();
    drawState.instancedCacheOwners.clear();
    drawState.gpuDrivenHosts.forEach(function(host) { host.dispose(); });
    drawState.gpuDrivenHosts.clear();
    waterState.waterRenderPipelineCache.clear();
    drawState.pointsAuthoredPipelineCache.clear();
    drawState.pointsAuthoredLayerFailed.clear();
    waterState.waterPoolPipelineCache = {};
    pipeState.waterObjectMeshPipelineCache = {};

    destroyRendererGPUResource(pipeState.mainDepthTexture);
    pipeState.mainDepthTexture = null;
    pipeState.mainDepthView = null;
    destroyRendererGPUResource(pipeState.mainMSAATexture);
    pipeState.mainMSAATexture = null;
    pipeState.mainMSAAView = null;
    destroyRendererGPUResource(s.dummyShadowTex);
    s.dummyShadowTex = null;
    gpuRes.dummyShadowView = gpuRes.dummyShadowArrayView = null;
    destroyRendererGPUResource(s.placeholderTex);
    s.placeholderTex = null;
    gpuRes.placeholderView = null;
    destroyRendererGPUResource(s.placeholderCubeTex);
    s.placeholderCubeTex = null;
    gpuRes.placeholderCubeView = null;

    for (var si = 0; si < drawState.shadowSlots.length; si++) {
      if (drawState.shadowSlots[si]) destroyRendererGPUResource(drawState.shadowSlots[si].texture);
      drawState.shadowSlots[si] = null;
    }
    gpuRes.textureCache._gosxGeneration.disposed = true; gpuRes.textureCache._gosxGeneration.onResourceReady();
    for (var record of gpuRes.textureCache.values()) {
      if (record) {
        record.disposed = true;
        if (record.image) {
          record.image.onload = null;
          record.image.onerror = null;
        }
        destroyRendererGPUResource(record.texture);
      }
    }
    sceneWebGPUDisposeDetail(pipeState.detailResources); pipeState.detailResources = null; sceneWebGPUDisposeSelenaSurfaces(pipeState.selenaSurfaces);
    gpuRes.textureCache.clear();
    selenaState.pipelineCache.clear();
    selenaState.computePipelineCache.clear();
    selenaState.postPipelineCache.clear();
    pipeState.pipelineCache = {};
    drawState.instancedGeometryCache = {};
    // Buffers cached on these owners are registered in
    // pointsEntryGPUBuffers and were destroyed before the owner objects are
    // replaced, so no current tracked buffer is orphaned.
    meshState.pbrSceneAttributeCache = {};
    telemetryState.webGPUEssentialAttributeCache = Object.create(null);
    gpuRes.defaultMaterialOwner = {};
    drawState.thickLineOwner = {};
    drawState.screenLineOwner = {};
    uniformState._frameBindGroupCache = null;
    s.lastPreparedScene = null;
    telemetryState.lastWebGPUFrameStats = null;
    waterState.waterManifestShaderSourcesByID = null;
    waterState.activeWaterShaderSourcesByID = null;

    if (s.postProcessor) {
      try { s.postProcessor.dispose(); s.specularTarget.dispose(); } catch (_err) {}
      s.postProcessor = null;
    }

    s.device = null;
    telemetryState.configuredSurfaceKey = "";
    s.postFXForceDisabled = false;
  }
  function diagnostics(s: any) { var timingState = s.timingState, meshState = s.meshState, uniformState = s.uniformState, pipeState = s.pipeState, selenaState = s.selenaState, waterState = s.waterState, drawState = s.drawState, telemetryState = s.telemetryState, gpuRes = s.gpuRes, pipelineGuard = s.pipelineGuard;
    var base = typeof sceneWebGPUDiagnostics === "function"
      ? sceneWebGPUDiagnostics()
      : {};
    var out = {};
    for (var key in base) {
      if (Object.prototype.hasOwnProperty.call(base, key)) {
        out[key] = base[key];
      }
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ }
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.renderer = "webgpu";
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.targetFormat = pipeState.targetFormat;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.activeSampleCount = pipeState.activeSampleCount;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.presentationAlphaMode = s.activePresentation.alphaMode;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.presentationColorSpace = s.activePresentation.colorSpace;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.presentationToneMappingMode = s.activePresentation.toneMappingMode;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.powerPreference = s.activePowerPreference;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.ready = !!s.device && !s.initFailed;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.initFailed = !!s.initFailed;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.initError = s.initError || "";
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.resourcesDisposed = telemetryState.rendererResourcesDisposed;
    out.resourceCacheEntries = (
      drawState.pointsAuthoredPipelineCache.size +
      drawState.pointsAuthoredLayerFailed.size +
      drawState.computeParticleSystems.size +
      waterState.waterSystems.size +
      drawState.instancedCullSystems.size +
      gpuRes.textureCache.size +
      selenaState.pipelineCache.size +
      selenaState.computePipelineCache.size +
      selenaState.postPipelineCache.size +
      Object.keys(pipeState.pipelineCache).length +
      Object.keys(waterState.waterPoolPipelineCache).length +
      Object.keys(pipeState.waterObjectMeshPipelineCache).length
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ );
    out.deviceLost = !!s.lastDeviceLostInfo || !!(base && base.lost);
    // Prefer THIS renderer's own lastDeviceLostInfo (set synchronously by
    // the device.lost handler above, never cleared) over the shared probe
    // snapshot (base.lost), which a successful re-probe nulls out the
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ // moment it recovers — often before a watchdog poll gets to read it.
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.deviceLostInfo = s.lastDeviceLostInfo || (base && base.lost ? base.lost : null);
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.frameSeq = timingState.webGPUFrameSeq;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.frameAt = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.frameAt || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.lastError = telemetryState.webGPULastError;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.waterSimulationTickSeq = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.waterSimulationTickSeq || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.waterSolverSubstepSeq = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.waterSolverSubstepSeq || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.waterDroppedTicks = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.waterDroppedTicks || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.waterNormalDispatchSeq = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.waterNormalDispatchSeq || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.waterSampledStateSyncSeq = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.waterSampledStateSyncSeq || 0;
    out.postProcessing = !!s.postProcessor; pipelineGuard.snapshot(out); Object.assign(out, { initialPipelinesReady: s.initialPipelinesReady });
    // Frame-error resilience state (see reportWebGPUFrameError /
    // disablePostProcessing / enablePostProcessing above and
    // 20-scene-mount.js's checkSceneWebGPUFrameErrorWatchdog, the poller
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ // that acts on these).
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.frameErrorStreak = telemetryState.webGPUConsecutiveFrameErrors;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.frameCleanStreak = telemetryState.webGPUConsecutiveCleanFrames;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.postFXDisabled = s.postFXForceDisabled;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.customMaterialFallbacks = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.customMaterialFallbacks || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.customMaterialFallbackReason = out.customMaterialFallbacks > 0 ? "custom-wgsl-hooks-unsupported" : "";
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.skinnedMeshObjects = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.skinnedMeshObjects || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.computedMorphDispatches = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.computedMorphDispatches || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.computedMorphVertices = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.computedMorphVertices || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.computedMorphKernel = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.computedMorphKernel || "";
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.elioSkinningDispatches = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.elioSkinningDispatches || 0;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.elioSkinningVertices = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.elioSkinningVertices || 0;
    out.elioSkinningKernel = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.elioSkinningKernel || "";
    // GPU picking. gpuPicking stays true whether or not a pick has run yet —
    // it reports the renderer capability, matching the gpu-picking cell in
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ // 16a-scene-webgpu.capabilities.json.
    out.gpuPicking = true;
    if (drawState.scenePicker) Object.assign(out, drawState.scenePicker.diagnostics());
    // Render truth: implementation identity, the post-chain dispatch record
    // and the event journal, so a single diagnostics() call is a complete
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ var truthApi = renderTruth();
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.implementation = truthApi.implementation(out.adapterInfo || {});
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.browserEngine = typeof truthApi.browserEngine === "function" ? truthApi.browserEngine() : "";
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.renderTruthEvents = typeof truthApi.events === "function" ? truthApi.events() : [];
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.shaderDiagnostics = typeof truthApi.shaderCounts === "function" ? truthApi.shaderCounts() : { messages: 0, errors: 0 };
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.postChain = telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.postChain || null;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.uniformTime = selenaState.frame.time;
    out.textureVariantContext = {
      backend: s.textureVariantContext.backend,
      uploadReady: s.textureVariantContext.uploadReady,
      tokens: s.textureVariantContext.tokens.slice(),
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ };
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ out.ibl = Object.assign({}, uniformState.iblResources.diagnostics);
    out.retainedGeometry = webGPURetainedMeshBufferStats(meshState);
    return out;
  }
  function createSceneWebGPURenderer(canvas, options) {
    if (typeof navigator === "undefined" || !navigator.gpu) return sceneWebGPUFactoryFailure(canvas, "navigator-gpu-unavailable");
    if (!canvas || typeof canvas.getContext !== "function") return sceneWebGPUFactoryFailure(canvas, "canvas-context-unavailable");
    var telemetryMount = canvas.parentNode && typeof canvas.parentNode.setAttribute === "function" ? canvas.parentNode : null;

    // Device + adapter come from the main-bundle probe (16z). The
    // probe has already verified BOTH requestAdapter AND requestDevice
    // succeed — if we're here, WebGPU is genuinely usable. Reusing the
    // probed device (instead of requesting another) sidesteps a subtle
    // failure mode where requestAdapter works twice but requestDevice
    // fails on the second call (seen on some mobile GPUs).
    var probe = _externalProbe();
    if (!probe || !probe.ready || !probe.adapter || !probe.device) {
      var probeError = probe && probe.error ? ": " + probe.error : "";
      return sceneWebGPUFactoryFailure(canvas, "probe-not-ready" + probeError);
    }
    var adapter = probe.adapter;
    var rawDevice = probe.device;
    var pipelineGuard = wgpuCreatePipelineGuard(canvas);
    var s: any = { device: wgpuGuardDevice(rawDevice, pipelineGuard), lastDeviceLostInfo: null, canvas: canvas, rawDevice: rawDevice, pipelineGuard: pipelineGuard };
    var rendererOptions = options && typeof options === "object" ? options : {};
    // lastDeviceLostInfo: { reason, message } captured by the device.lost
    // handler below (see initGPUResources), read back by diagnostics().
    // Kept on THIS renderer instance rather than read off the shared probe
    // snapshot, because a successful re-probe nulls that shared snapshot
    // (16z-scene-webgpu-probe.ts's _webgpuDeviceLostInfo) the moment it
    // recovers — often before the mount-level watchdog's next poll — so
    // reading the shared snapshot lost the detail exactly when it mattered.

    // Only NOW taint the canvas with a WebGPU context. If any of the
    // checks above failed we never reached this line, so the canvas
    // stays clean and the mount code can fall through to WebGL.
    var gpuCtx = canvas.getContext("webgpu");
    if (!gpuCtx) return sceneWebGPUFactoryFailure(canvas, "canvas-webgpu-context-unavailable");

    // initFailed remains for runtime device-loss recovery.
    var presentationFormat = navigator.gpu.getPreferredCanvasFormat();
    Object.assign(s, { drawComputeParticlePasses: drawComputeParticlePasses, initFailed: false, initError: "", gpuCtx: gpuCtx, presentationFormat: presentationFormat, specularTarget: sceneWebGPUSpecularTarget(s.device), postProcessor: null, postFXForceDisabled: false, initialPipelinesPrepared: false, initialPipelinesReady: false, preparedPipelineKey: "" });
    var presentationOptions = rendererOptions.presentation && typeof rendererOptions.presentation === "object" ? rendererOptions.presentation : {};
    var probeOptions = probe.probeOptions && typeof probe.probeOptions === "object" ? probe.probeOptions : {};
    s.activePowerPreference = sceneWebGPUCanvasPowerPreference(probeOptions.powerPreference);
    s.activePresentation = {
      alphaMode: sceneWebGPUCanvasAlphaMode(presentationOptions.alphaMode),
      colorSpace: sceneWebGPUCanvasColorSpace(presentationOptions.colorSpace),
      toneMappingMode: sceneWebGPUCanvasToneMappingMode(presentationOptions.toneMappingMode),
    };

    // GPU resources (initialized after device is ready).
    var gpuRes: any = { frameBindGroupLayout: undefined, materialBindGroupLayout: undefined, frameUniformBuffer: null, lightStorageBuffer: null, fogUniformBuffer: null, envUniformBuffer: null, shadowUniformBuffer: null, linearSampler: null, comparisonSampler: null, envMapSampler: null, dummyShadowArrayView: null, dummyShadowView: null, placeholderView: undefined, placeholderCubeView: null, defaultMaterialOwner: {}, textureCache: new Map() };
    var pipeState: any = {
      mainDepthTexture: null, mainDepthView: null, mainDepthWidth: 0, mainDepthHeight: 0, mainDepthSampleCount: 1,
      mainMSAATexture: null, mainMSAAView: null, mainMSAAWidth: 0, mainMSAAHeight: 0, mainMSAAFormat: "", mainMSAASampleCount: 1,
      shadowPipeline: null, shadowReflectedPipeline: null, shadowInstancedPipeline: null,
      shadowBindGroupLayout: null, shadowVertexModule: null, shadowInstancedVertexModule: null, shadowFragmentModule: null,
      elioSkinPipelineLayout: null, computedMorphPipelineLayout: null, elioSkinShaderModule: null, computedMorphShaderModule: null,
      targetFormat: presentationFormat, selectiveBloom: false, activeSampleCount: 1,
      pipelineCache: {}, waterObjectMeshPipelineCache: {},
      pbrPipelineLayout: null, pointsPipelineLayout: null, pointsVertexPipelineLayout: null,
      pbrVertexModule: null, pbrInstancedVertexModule: null, pbrInstancedCullVertexModule: null,
      pbrFragmentModule: null, pbrUnshadowedFragmentModule: null, pbrShadowsActive: false,
      waterObjectMeshRefractionFragmentModule: null, waterObjectMeshClippedFragmentModule: null,
      sceneWorldColorVertexModule: null, sceneClipColorVertexModule: null, sceneColorFragmentModule: null,
      surfaceVertexModule: null, surfaceFragmentModule: null, thickLineVertexModule: null, thickLineFragmentModule: null,
      pointsVertexModule: null, pointsInstancedVertexModule: null, pointsFragmentModule: null,
      detailResources: new Map().get("active"), detailEnabled: true, selenaSurfaces: sceneWebGPUSelenaSurfaceResources(),
    };
    // waterManifestShaderSourcesByID/waterState.activeWaterShaderSourcesByID remain as a
    // generic bundle/manifest water-source diagnostic cache (see
    // sceneWaterManifestShaderSources/updateWaterSystems below); they no
    // longer feed any pipeline decision now that the hand-written
    // data-prop/entry-field WGSL resolution tier has been retired in favor of
    // Selena-primary -> builtin-fallback (see getWaterPoolPipeline /
    // getWaterRenderPipeline / renderWaterCausticsPass / etc.).
    // waterAuthoredCausticsPipelineLastError/waterAuthoredSurfacePipelineLastError
    // are kept (always "") as stable no-op reads for the
    // waterAuthoredCausticFallbackReason/waterAuthoredSurfaceFallbackReason
    // stats fields below -- the authored render/compute pipeline tier itself
    // (and its caches/failure sets) has been removed since Selena is now the
    // sole primary WGSL source ahead of the builtin SCENE_WATER_*_SOURCE
    // fallback.

    if (typeof window !== "undefined") { window.__gosx_scene3d_asset_ext = window.__gosx_scene3d_asset_ext || {}; window.__gosx_scene3d_asset_ext.uv1 = Object.assign({ gpuWanted() { return ""; }, gpuModules(): any { return null; }, gpuDraw() {}, gpuDispose() {} }, window.__gosx_scene3d_asset_ext.uv1); }

    // Pipeline cache.

    // Shadow resources.

    // GPU picking (drawState.scenePicker) is created on the first queuePick call, so a scene that never
    // picks pays nothing — no textures, no pipelines, no extra pass.

    // Persistent GPU buffers.
    s.uvBuffer = null;

    // Points buffers.
    //
    // Each points entry owns its uniform/storage buffers. Uniform data can
    // move every frame (spin/fog/opacity), so it reuses per-entry GPUBuffer
    // storage with writeBuffer. Particle storage is keyed by the stable
    // typed-array payload and uploads only when source/count/color inputs
    // change.
    // Retained geometry buffers belong to this renderer/device, never to the
    // shared vertex object; enumerable map supports epoch sweeping.
    var meshState: any = {
      pointsEntryGPUBuffers: new Set(), pbrSceneAttributeCache: {},
      retainedMeshAttributeCache: new Map(), retainedMeshAttributeEpoch: 0, instancedCacheOwnerEpoch: 0,
      retainedMeshBufferStats: { liveBytes: 0, hits: 0, misses: 0, uploadCalls: 0, uploadBytes: 0, allocations: 0, rebuilds: 0, revisionInvalidations: 0, retirements: 0 },
      scratchPositions: null, scratchNormals: null, scratchUVs: null, scratchTangents: null,
      positionBuffer: null, normalBuffer: null, tangentBuffer: null,
      elioSkinBindGroupLayout: null, computedMorphBindGroupLayout: null,
      elioSkinPipeline: null, computedMorphPipeline: null,
      ensureElioSkinPipeline: function() { return ensureElioSkinPipeline(pipeState, meshState, s.device); }, ensureComputedMorphPipeline: function() { return ensureComputedMorphPipeline(pipeState, meshState, s.device); },
    };
    // Hoisted scratches so uniform uploads don't allocate fresh 128-byte
    // ArrayBuffers per entry per frame. The WGSL PointsUniforms layout is
    // vec4-aligned: mat4 + vec4 color/size + vec4 flags + vec4 params +
    // vec4 fog color. Wrapped Float32Array / Uint32Array views are created
    // once for the same underlying storage.
    var pointsUniformScratch = new ArrayBuffer(128);
    s.temporalAA = wgpuPostfxTemporalAA(function() {
      return { device: s.device, linearSampler: gpuRes.linearSampler, frameLayout: gpuRes.frameBindGroupLayout, materialLayout: gpuRes.materialBindGroupLayout, pointsLayout: drawState.pointsBindGroupLayout, pointsVertex: pipeState.pointsVertexModule,
        materialGroup: function() { return createMaterialBindGroup(uniformState, gpuRes, meshState, s.device, null, false, gpuRes.defaultMaterialOwner); }, previousSkin: function(obj: any) { return webGPUObjectResourceOwner(obj)._gosxTAAPreviousSkin || null; },
        // The owner's buffer, so the capture before this frame's dispatch sees last frame's skin.
        skinBuffer: function(obj: any) { return webGPUObjectIsSkinned(obj) && webGPUObjectSkinOutputBuffer(meshState, obj) || null; },
        bindPositions: function(pass: any, slot: number, obj: any) { return webGPUBindRetainedMeshAttribute(meshState, s.device, pass, slot, obj, "positions", 3); },
        bindIndices: function(pass: any, obj: any) { return webGPUBindRetainedMeshIndexBuffer(meshState, s.device, pass, obj); } };
    });
    s.autoExposure = wgpuPostfxAutoExposure(function() { return { device: s.device }; });
    s.softParticles = wgpuCreateSoftParticles(function() {
      return { device: s.device, frame: gpuRes.frameBindGroupLayout, material: gpuRes.materialBindGroupLayout, points: drawState.pointsBindGroupLayout, vertex: pipeState.pointsVertexModule,
        scratchF: drawState.pointsUniformScratchF, scratchU: drawState.pointsUniformScratchU, format: pipeState.targetFormat, samples: pipeState.activeSampleCount, bindGroup: function(owner: any, slot: string, layout: any, entries: any) { return wgpuCachedBindGroup(s.device, owner, slot, layout, entries); },
        buffer: function(owner: any, data: Float32Array) { return wgpuCachedTrackedBuffer(meshState, s.device, owner, "_gosxWGPUSoftPointsUniformBuffer", data, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, true); },
        materialGroup: function() { return createMaterialBindGroup(uniformState, gpuRes, meshState, s.device, null, false, gpuRes.defaultMaterialOwner); } };
    });
    s.lastPreparedScene = null;
    // gpuTimingEncodingAvailable: standard pass timestamp writes bracket all commands in the frame encoder.
    var timingState: any = {
      gpuTiming: null, gpuTimingFailed: false, gpuTimingEncodingAvailable: null,
      failedGPUTimings: [], lastGPUPerformanceSample: null, lastGPUCompletionSample: null,
      gpuTimingDisposed: false, gpuTimingFrameSeq: 0, webGPUFrameSeq: 0,
      deferredWaterTextureRetirements: [], deferredWaterSystemRetirements: [],
      gpuPassTiming: null, gpuPassTimingSlot: null, gpuPassTimingShadowUsed: false,
      lastGPUPassSample: null, gpuFrameTimerZeroReads: 0, gpuFrameTimerUnusable: false, mount: telemetryMount, queue: function(name: any, value: any) { queueWebGPUAttribute(telemetryState, name, value); },
    };
    // Helper renderers publish through the same queue as the main renderer.
    var telemetryState: any = {
      canvas: canvas, timing: timingState, sink: null, draw: null, rawDevice: rawDevice, gpuCtx: gpuCtx, presentationFormat: presentationFormat, activePresentation: s.activePresentation, pipelineGuard: pipelineGuard,
      configuredSurfaceKey: "", rendererResourcesDisposed: false, webGPUEssentialAttributeCache: Object.create(null), webGPUPendingAttributes: Object.create(null), webGPUAttributeMount: null,
      lastWebGPUDiagnosticAttributeAt: null, webGPUAttributeTimer: null, webGPUAttributeFormatting: false, webGPUFrameAttributesSeeded: false,
      frameStats: {}, lastWebGPUFrameStats: null, spareFrameStats: {}, webGPULastError: "", webGPUFrameProof: {},
      pendingWebGPUErrorScope: false, webGPUErrorReportCount: 0, webGPUConsecutiveFrameErrors: 0, webGPUConsecutiveCleanFrames: 0,
    };
    s.webGPUAttributeSink = { setAttribute: function(name: any, value: any) { queueWebGPUAttribute(telemetryState, name, value); } };
    telemetryState.sink = s.webGPUAttributeSink;

    // Cull telemetry: frame counter for throttling readback (~every 30 frames)
    // and the last aggregated survivor snapshot written to the mount attribute.
    // webGPUConsecutiveFrameErrors: consecutive frames (not capped, unlike
    // webGPUErrorReportCount's 3-report emit cap above) that ended with a
    // reported validation/OOM error — reset to 0 on the next clean frame.
    // Drives the mount-level demote (tear down post-FX, retry raw) / backend
    // fallback resilience in 20-scene-mount.js; see diagnostics().frameErrorStreak
    // and disablePostProcessing() below.
    // webGPUConsecutiveCleanFrames: the complement of webGPUConsecutiveFrameErrors
    // above — consecutive frames that ended with NO reported validation/OOM
    // error, reset to 0 the moment a frame errors (see reportWebGPUFrameError).
    // Drives the mount-level RESTORE step of the resilience ladder (see
    // diagnostics().frameCleanStreak and enablePostProcessing() below): once
    // a demoted scene has run clean for long enough, the mount re-enables
    // post-FX.
    // postFXForceDisabled: set by disablePostProcessing() (the "demote" step
    // of the frame-error resilience ladder) — once true, render() never
    // rebuilds or uses the post-FX chain again for this renderer instance,
    // until enablePostProcessing() (the "restore" step, called by the mount
    // once webGPUConsecutiveCleanFrames crosses its threshold) clears it, or
    // a fresh mount/renderer swap replaces this closure entirely.
    // webGPUBundleCache holds the cached GPURenderBundle and the command stream
    // it was built from. Created on the first eligible frame.
    s.webGPUBundleCache = null;

    // Shadow pass buffer.
    s.shadowPositionBuffer = null;

    // 1x1 dummy depth texture for shadow map bind group when no shadows.
    s.dummyShadowTex = null;

    // Water simulation resources.
    var _waterUniformBuf = new ArrayBuffer(256);
    var waterState: any = {
      waterSystems: new Map(), waterManifestScanKey: [], waterManifestShaderSourcesByID: null, activeWaterShaderSourcesByID: null,
      waterSystemRetireSerial: 0, lastWaterTimeSeconds: null, waterAuthoredCausticsPipelineLastError: "", waterAuthoredSurfacePipelineLastError: "",
      waterUniformScratch: _waterUniformBuf, waterUniformScratchF: new Float32Array(_waterUniformBuf), waterUniformScratchU: new Uint32Array(_waterUniformBuf),
      waterObjectSphereScratch: new Float32Array(WATER_MAX_DISPLACEMENT_SPHERES * 4), waterObjectMeshShadowUniformScratch: new Float32Array(8), waterObjectTextureMatrixScratch: new Float32Array(32),
      waterComputeBindGroupLayout: null, waterRenderBindGroupLayout: null, waterPoolBindGroupLayout: null, waterCausticsBindGroupLayout: null,
      waterObjectTextureBindGroupLayout: null, waterObjectMeshShadowBindGroupLayout: null, waterTileSampler: null, gpuRes: gpuRes,
      pipeState: pipeState, meshState: meshState, selenaState: null, uniformState: null, sceneWaterKnotArrayCache: undefined, waterPoolPipelineCache: {}, waterRenderPipelineCache: new Map(),
      waterPoolPipelineLayout: null, waterPoolVertexModule: null, waterPoolFragmentModule: null, waterRenderPipelineLayout: null, waterRenderVertexModule: null, waterRenderFragmentModule: null, waterRenderBelowFragmentModule: null,
      waterSeedPipeline: null, waterDropPipeline: null, waterDisplacementPipeline: null, waterStepPipeline: null, waterNormalPipeline: null, waterCausticsPipeline: null, waterObjectTexturePipeline: null, waterObjectShadowPipeline: null, waterObjectMeshShadowPipeline: null,
      waterClockAPI: (typeof window !== "undefined" && window.__gosx_scene3d_api) ? window.__gosx_scene3d_api : null, waterComputeShaderModule: null, waterCausticsVertexModule: null, waterCausticsFragmentModule: null, waterObjectTextureVertexModule: null, waterObjectTextureFragmentModule: null, waterObjectShadowFragmentModule: null, waterObjectMeshShadowVertexModule: null, waterObjectMeshShadowFragmentModule: null, waterComputePipelineLayout: null, waterCausticsPipelineLayout: null, waterObjectTexturePipelineLayout: null, waterObjectMeshShadowPipelineLayout: null, timingState: timingState, canvas: canvas,
    };

    var _frameUniformBuf = new ArrayBuffer(160), _fogUniformBuf = new ArrayBuffer(32), _shadowUniformBuf = new ArrayBuffer(SCENE_WEBGPU_SHADOW_UNIFORM_BYTES), _envUniformBuf = new ArrayBuffer(80), _materialUniformBuf = new ArrayBuffer(288);
    var uniformState: any = {
      scratchViewMatrix: new Float32Array(16), scratchProjMatrix: new Float32Array(16), scratchSelenaViewProjection: new Float32Array(16),
      _frameUniformF: new Float32Array(_frameUniformBuf), _frameUniformU: new Uint32Array(_frameUniformBuf),
      _fogUniformF: new Float32Array(_fogUniformBuf), _fogUniformU: new Uint32Array(_fogUniformBuf),
      _shadowUniformF: new Float32Array(_shadowUniformBuf), _shadowUniformI: new Int32Array(_shadowUniformBuf),
      _envUniformF: new Float32Array(_envUniformBuf), _envUniformU: new Uint32Array(_envUniformBuf),
      _lightCountBuf: new Uint32Array(1), _lightColorCache: {}, _retiredLightBuffers: [], _lightIssuesReported: Object.create(null),
      _materialUniformF: new Float32Array(_materialUniformBuf), _materialUniformU: new Uint32Array(_materialUniformBuf),
      iblResources: { key: "", irradiance: null, radiance: null, brdfLUT: null, active: false, diagnostics: { requested: false, active: false, state: "not-requested", reason: "", radianceMipLevels: 0 } },
      envMapResources: { key: "", record: null, active: false },
      materialFrame: { resources: new Map(), groups: [], signatures: [], textureRevision: 0, buffer: null, capacity: 0, data: null, words: null, offsets: new WeakMap() },
      _lightCapacity: SCENE_WEBGPU_LIGHT_CAPACITY_MIN, _lightDataF: new Float32Array(SCENE_WEBGPU_LIGHT_CAPACITY_MIN * SCENE_WEBGPU_LIGHT_FLOATS),
      _frameBindGroupCache: null, transmissionResources: null, transmissionView: null,
    };
    gpuRes.textureCache._gosxGeneration = {
      disposed: false,
      onResourceReady: function() {
        uniformState.materialFrame.textureRevision++;
        if (canvas && typeof canvas.dispatchEvent === "function") {
          var event = typeof CustomEvent === "function" ? new CustomEvent("gosx:scene3d:resource-ready") : { type: "gosx:scene3d:resource-ready" };
          canvas.dispatchEvent(event);
        }
      },
    };
    s.placeholderTex = null; s.placeholderCubeTex = null;
    // @ts-ignore TS7018 -- lazily allocated backend sky resources.
    s.skyResources = { renderer: null }; s.oceanResources = { renderer: null, failed: false }; s.terrainResources = {};
    var selenaState: any = { pipelineCache: new Map(), computePipelineCache: new Map(), postPipelineCache: new Map(), frame: { viewProjection: uniformState.scratchSelenaViewProjection, time: 0, cameraProximity: 0, environmentInfo: [0, 0, 0, 0] }, textureContext: null, waterPostBGL: null, waterPostDepthSampler: null, waterSystems: waterState.waterSystems, gpuRes: gpuRes, instancedCacheOwner: function(meshId?: any, fallback?: any, namespace?: any) { return webGPUInstancedCacheOwner(drawState, s.device, meshId, fallback, namespace); } }; waterState.selenaState = selenaState; waterState.uniformState = uniformState;
    var drawState: any = {
      meshState: meshState, pipeState: pipeState, uniformState: uniformState, gpuRes: gpuRes, selenaState: selenaState, timingState: timingState, pipelineGuard: pipelineGuard,
      shadowSlots: [null, null], shadowFrameBindGroups: [], instancedGeometryCache: {}, modelDynamicOffsets: [0],
      instancedCullSystems: new Map(), // meshId → { system, signature }
      instancedCacheOwners: new Map(), // meshId → owner of that mesh's cached GPU buffers and bind groups
      gpuDrivenHosts: new Map(), // "host" → GPU-driven instancing host; a Map keeps it typed any
      shadowFrameBuffer: null, shadowFrameBufferStride: 256, shadowFrameBufferCapacity: 0,
      _shadowCombinedMatrixScratch: null, // per-caster combined (lightVP × model) matrix for retained casters; allocated lazily, reused every frame
      cullTelemetryFrameCount: 0, lastCullSurvivors: null, // null | string (JSON)
      pointsBindGroupLayout: null, pointsUniformBindGroupLayout: null, pointsAuthoredUserUniformBGL: null, // the user-uniform BGL is a minimal @group(1) @binding(0) uniform layout for authored Points and ComputeParticle uniforms
      pointsAuthoredVertexPipelineLayout: null, /* [frame, userUniform, pointsUniform] for Points layers */ pointsAuthoredStoragePipelineLayout: null, /* [frame, userUniform, pointsStorage] for ComputeParticle render */
      pointsAuthoredPipelineCache: new Map(), /* keyed by "wgsl|blend|depth|format|samples" */ pointsAuthoredLayerFailed: new Map(), /* layerID → true: the authored pipeline failed; fall back to builtin and warn once */
      worldDrawScratch: typeof createSceneWorldDrawScratch === "function" ? createSceneWorldDrawScratch() : null, thickLineScratch: typeof createSceneThickLineScratch === "function" ? createSceneThickLineScratch() : null, thickLineOwner: {}, screenLineOwner: {},
      pointsUniformScratchF: new Float32Array(pointsUniformScratch), pointsUniformScratchU: new Uint32Array(pointsUniformScratch), computeParticleSystems: new Map(), lastComputeParticleTimeSeconds: null,
      boardGlyphAtlases: new Map(), boardTextOwners: new Map(), scenePicker: null, activePickMeshPositions: null, rendererOptions: rendererOptions, deviceStillActive: function(scopedDevice: any) { return rendererDeviceStillActive(s.device, scopedDevice); },
    }; telemetryState.draw = drawState; s.timingState = timingState; s.pipeState = pipeState; s.meshState = meshState; s.gpuRes = gpuRes; s.selenaState = selenaState; s.telemetryState = telemetryState; s.waterState = waterState; s.drawState = drawState; s.uniformState = uniformState;

    // Everything below used to be inside the .then() chain after
    // requestDevice resolved. It's now run synchronously so the
    // returned renderer is fully ready before the factory call returns.
    (function initGPUResources() {
      try {
        // Handle device loss post-factory. Record the loss detail on THIS
        // renderer (lastDeviceLostInfo, read back by diagnostics() below)
        // for diagnosis, and run our own local cleanup — but do NOT also
        // invalidate the shared probe here. 16z-scene-webgpu-probe.ts's own
        // sceneWebGPUWatchDeviceLoss already has a `.then()` listener on
        // this EXACT device (it is the same object handed to us as
        // probe.device above), and is the probe's single owner for
        // invalidation/reprobe bookkeeping. A single device.lost event
        // resolves every listener attached to it, so having both this
        // handler AND 16z's call sceneWebGPUInvalidateProbe counted one
        // real loss as two against WEBGPU_LOST_REPROBE_MAX_PER_WINDOW,
        // arming its reprobe backoff after a single device loss instead of
        // the intended three.
        s.device.lost.then(function(info) {
          console.warn("[gosx] WebGPU device lost:", info && info.message);
          // Journal the loss with a timestamp. A device that mounts healthy and
          // dies seconds later (observed on Firefox under GPU-memory pressure)
          // is indistinguishable from "never had WebGPU" in any single sample;
          // only an ordered timeline separates the two.
          renderTruth().record("device-lost", (info && info.reason ? info.reason + " " : "") + String(info && info.message || ""));
          s.lastDeviceLostInfo = {
            reason: (info && info.reason) || "",
            message: (info && info.message) || "",
          };
          s.initFailed = true;
          // Eagerly free every renderer-owned resource and logical cache. The
          // probe-based recovery path
          // (gosx:scene3d:webgpu-probe-ready → recoverSceneWebGPURenderer in
          // 20-scene-mount.js) creates a fresh renderer whose first render()
          // call rebuilds resources on the new device. dispose() shares this
          // idempotent path, so mount teardown after the loss is a safe no-op.
          dispose(s);
        }).catch(function() {});

        if (typeof rawDevice.addEventListener === "function") {
          rawDevice.addEventListener("uncapturederror", pipelineGuard.uncaptured);
        } else {
          rawDevice.onuncapturederror = pipelineGuard.uncaptured;
        }
        renderTruth().record("webgpu-device-ready", renderTruth().implementation(webGPUAdapterInfoSnapshot()));
/* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */
        configureWebGPUCanvas(telemetryState, s.device);

        // Create bind group layouts.
        gpuRes.frameBindGroupLayout = wgpuCreateFrameBindGroupLayout(s.device);
        gpuRes.materialBindGroupLayout = wgpuCreateMaterialBindGroupLayout(s.device, true);
        meshState.elioSkinBindGroupLayout = s.device.createBindGroupLayout({
          label: "gosx-elio-skin-lbs",
          entries: [
            { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
          ],
        });
        meshState.computedMorphBindGroupLayout = s.device.createBindGroupLayout({
          label: "gosx-computed-morph",
          entries: [
            { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
            { binding: 3, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
            { binding: 4, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
            { binding: 5, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } },
          ],
        });
        waterState.waterComputeBindGroupLayout = s.device.createBindGroupLayout({
          label: "gosx-water-compute",
          entries: [
            { binding: 0, visibility: GPUShaderStage.COMPUTE, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.COMPUTE, buffer: { type: "storage" } },
            { binding: 3, visibility: GPUShaderStage.COMPUTE, buffer: { type: "read-only-storage" } },
          ],
        });
        waterState.waterRenderBindGroupLayout = s.device.createBindGroupLayout({
          label: "gosx-water-render",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 5, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 6, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 7, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "cube" } },
            { binding: 8, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 9, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 10, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
          ],
        });
        waterState.waterPoolBindGroupLayout = s.device.createBindGroupLayout({
          label: "gosx-water-pool",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
            { binding: 5, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
          ],
        });
        waterState.waterCausticsBindGroupLayout = s.device.createBindGroupLayout({
          label: "gosx-water-caustics",
          entries: [
            { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
            { binding: 2, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
            { binding: 3, visibility: GPUShaderStage.FRAGMENT, sampler: { type: "filtering" } },
            { binding: 4, visibility: GPUShaderStage.FRAGMENT, texture: { sampleType: "float", viewDimension: "2d" } },
          ],
        });
        waterState.waterObjectTextureBindGroupLayout = s.device.createBindGroupLayout({
          label: "gosx-water-object-textures",
          entries: [
            { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
            { binding: 1, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "read-only-storage" } },
          ],
        });
        waterState.waterObjectMeshShadowBindGroupLayout = s.device.createBindGroupLayout({
          label: "gosx-water-object-mesh-shadow",
          entries: [
            { binding: 0, visibility: GPUShaderStage.VERTEX, buffer: { type: "uniform" } },
          ],
        });
        drawState.pointsBindGroupLayout = wgpuCreatePointsBindGroupLayout(s.device);
        drawState.pointsUniformBindGroupLayout = wgpuCreatePointsUniformBindGroupLayout(s.device);
        // Simple uniform BGL for authored user uniforms at group(1).
        drawState.pointsAuthoredUserUniformBGL = s.device.createBindGroupLayout({
          label: "gosx-points-authored-user",
          entries: [{ binding: 0, visibility: (typeof GPUShaderStage !== "undefined" ? GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT : 3), buffer: { type: "uniform" } }],
        });
        drawState.pointsAuthoredVertexPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [gpuRes.frameBindGroupLayout, drawState.pointsAuthoredUserUniformBGL, drawState.pointsUniformBindGroupLayout],
        });
        drawState.pointsAuthoredStoragePipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [gpuRes.frameBindGroupLayout, drawState.pointsAuthoredUserUniformBGL, drawState.pointsBindGroupLayout],
        });
        pipeState.shadowBindGroupLayout = wgpuCreateShadowBindGroupLayout(s.device);

        // Pipeline layouts.
        pipeState.pbrPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [gpuRes.frameBindGroupLayout, gpuRes.materialBindGroupLayout],
        });
        pipeState.elioSkinPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [meshState.elioSkinBindGroupLayout],
        });
        pipeState.computedMorphPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [meshState.computedMorphBindGroupLayout],
        });
        waterState.waterComputePipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [waterState.waterComputeBindGroupLayout],
        });
        waterState.waterRenderPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [gpuRes.frameBindGroupLayout, waterState.waterRenderBindGroupLayout],
        });
        waterState.waterPoolPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [gpuRes.frameBindGroupLayout, waterState.waterPoolBindGroupLayout],
        });
        waterState.waterCausticsPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [waterState.waterCausticsBindGroupLayout],
        });
        waterState.waterObjectTexturePipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [waterState.waterObjectTextureBindGroupLayout],
        });
        waterState.waterObjectMeshShadowPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [waterState.waterObjectMeshShadowBindGroupLayout],
        });
        pipeState.pointsPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [gpuRes.frameBindGroupLayout, gpuRes.materialBindGroupLayout, drawState.pointsBindGroupLayout],
        });
        pipeState.pointsVertexPipelineLayout = s.device.createPipelineLayout({
          bindGroupLayouts: [gpuRes.frameBindGroupLayout, gpuRes.materialBindGroupLayout, drawState.pointsUniformBindGroupLayout],
        });

        // Compile shader modules.
        pipeState.pbrVertexModule = s.device.createShaderModule({ label: "pbr-vert", code: WGSL_PBR_VERTEX });
        pipeState.pbrInstancedVertexModule = s.device.createShaderModule({ label: "pbr-instanced-vert", code: WGSL_PBR_INSTANCED_VERTEX });
        pipeState.pbrInstancedCullVertexModule = s.device.createShaderModule({ label: "pbr-instanced-cull-vert", code: WGSL_PBR_INSTANCED_CULL_VERTEX });
        pipeState.pbrFragmentModule = s.device.createShaderModule({ label: "pbr-frag", code: WGSL_PBR_FRAGMENT }); pipeState.pbrUnshadowedFragmentModule = null;
        pipeState.shadowVertexModule = s.device.createShaderModule({ label: "shadow-vert", code: WGSL_SHADOW_VERTEX });
        pipeState.shadowInstancedVertexModule = s.device.createShaderModule({ label: "shadow-instanced-vert", code: WGSL_SHADOW_INSTANCED_VERTEX });
        pipeState.shadowFragmentModule = s.device.createShaderModule({ label: "shadow-frag", code: WGSL_SHADOW_FRAGMENT });
        pipeState.sceneWorldColorVertexModule = s.device.createShaderModule({ label: "scene-world-color-vert", code: WGSL_SCENE_WORLD_COLOR_VERTEX });
        pipeState.sceneClipColorVertexModule = s.device.createShaderModule({ label: "scene-clip-color-vert", code: WGSL_SCENE_CLIP_COLOR_VERTEX });
        pipeState.sceneColorFragmentModule = s.device.createShaderModule({ label: "scene-color-frag", code: WGSL_SCENE_COLOR_FRAGMENT });
        pipeState.surfaceVertexModule = s.device.createShaderModule({ label: "surface-vert", code: WGSL_SURFACE_VERTEX });
        pipeState.surfaceFragmentModule = s.device.createShaderModule({ label: "surface-frag", code: WGSL_SURFACE_FRAGMENT });
        pipeState.thickLineVertexModule = s.device.createShaderModule({ label: "thick-line-vert", code: WGSL_THICK_LINE_VERTEX });
        pipeState.thickLineFragmentModule = s.device.createShaderModule({ label: "thick-line-frag", code: WGSL_THICK_LINE_FRAGMENT });
        pipeState.pointsVertexModule = s.device.createShaderModule({ label: "points-vert", code: WGSL_POINTS_VERTEX });
        pipeState.pointsInstancedVertexModule = s.device.createShaderModule({ label: "points-instanced-vert", code: WGSL_POINTS_INSTANCED_VERTEX });
        pipeState.pointsFragmentModule = s.device.createShaderModule({ label: "points-frag", code: WGSL_POINTS_FRAGMENT });

        // Create persistent uniform buffers.
        // FrameUniforms: 2*mat4 + vec3 + u32 + 2*f32 + 2*u32 = 128+16+16 = ~160 bytes.
        gpuRes.frameUniformBuffer = s.device.createBuffer({ size: 256, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        // Lights start at the minimum capacity and grow in uploadLights.
        gpuRes.lightStorageBuffer = s.device.createBuffer({
          label: "gosx-lights",
          size: uniformState._lightCapacity * SCENE_WEBGPU_LIGHT_BYTES,
          usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
        });
        gpuRes.fogUniformBuffer = s.device.createBuffer({ size: 32, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        gpuRes.envUniformBuffer = s.device.createBuffer({ size: 80, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        gpuRes.shadowUniformBuffer = s.device.createBuffer({ size: SCENE_WEBGPU_SHADOW_UNIFORM_BYTES, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
        drawState.shadowFrameBufferStride = Math.max(
          256,
          Math.floor(sceneNumber(s.device && s.device.limits && s.device.limits.minUniformBufferOffsetAlignment, 256))
        );
        drawState.shadowFrameBufferCapacity = 1;
        drawState.shadowFrameBuffer = s.device.createBuffer({
          size: drawState.shadowFrameBufferStride,
          usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        });

        // Create samplers.
        gpuRes.linearSampler = s.device.createSampler({
          magFilter: "linear",
          minFilter: "linear",
          mipmapFilter: "linear",
          addressModeU: "clamp-to-edge",
          addressModeV: "clamp-to-edge",
        });
        waterState.waterTileSampler = s.device.createSampler({
          magFilter: "linear",
          minFilter: "linear",
          mipmapFilter: "linear",
          addressModeU: "repeat",
          addressModeV: "repeat",
        });
        gpuRes.comparisonSampler = s.device.createSampler({
          compare: "less",
          magFilter: "linear",
          minFilter: "linear",
        });
        gpuRes.envMapSampler = s.device.createSampler({
          magFilter: "linear",
          minFilter: "linear",
          mipmapFilter: "linear",
          addressModeU: "repeat",
          addressModeV: "clamp-to-edge",
        });

        // Create 1x1 dummy shadow depth texture.
        s.dummyShadowTex = s.device.createTexture({
          size: [1, 1, 1],
          format: "depth24plus",
          usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
        });
        gpuRes.dummyShadowView = s.dummyShadowTex.createView(); gpuRes.dummyShadowArrayView = s.dummyShadowTex.createView({ dimension: "2d-array" });

        // Clear the dummy shadow texture to depth 1.0.
        var initEncoder = s.device.createCommandEncoder();
        initEncoder.beginRenderPass({
          colorAttachments: [],
          depthStencilAttachment: {
            view: gpuRes.dummyShadowView,
            depthLoadOp: "clear",
            depthClearValue: 1.0,
            depthStoreOp: "store",
          },
        }).end();
        s.device.queue.submit([initEncoder.finish()]);

        // Placeholder texture.
        s.placeholderTex = s.device.createTexture({
          size: [1, 1, 1],
          format: "rgba8unorm",
          usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST,
        });
        s.device.queue.writeTexture(
          { texture: s.placeholderTex },
          new Uint8Array([255, 255, 255, 255]),
          { bytesPerRow: 4 },
          [1, 1, 1]
        );
        gpuRes.placeholderView = s.placeholderTex.createView();
        s.placeholderCubeTex = wgpuCreatePlaceholderCubeTexture(s.device);
        gpuRes.placeholderCubeView = s.placeholderCubeTex.createView({ dimension: "cube" });
      } catch (err) {
        // Synchronous GPU resource setup failed — the probe said the
        // device was good, but something in the texture/buffer/shader
        // creation path failed anyway. Mark the renderer broken so
        // render() no-ops. The canvas is tainted at this point (we
        // already called getContext("webgpu") above), so the mount
        // code can't fall back to WebGL — but at least we log loudly
        // and stop doing broken work.
        s.initError = String(err && (err.message || err) || "unknown error");
        console.warn("[gosx] WebGPU synchronous init failed:", err);
        s.initFailed = true;
      }
    })();

    selenaState.textureContext = {
      device: s.device, textureCache: gpuRes.textureCache, iblResources: uniformState.iblResources, placeholderCubeView: gpuRes.placeholderCubeView, placeholderView: gpuRes.placeholderView,
      envMapSampler: gpuRes.envMapSampler, linearSampler: gpuRes.linearSampler, liveView: function(m: any, t: any) { return sceneSelenaLiveTextureView(selenaState, m, t); }, url: wgpuSceneSelenaTextureURL, frame: selenaState.frame,
    };

    // -----------------------------------------------------------------------
    // General mechanism: route water RENDER passes through the generic
    // descriptor-driven Selena WebGPU render path (getSelenaPipeline /
    // createSelenaBindGroup for mesh & mesh+state kinds, getSelenaPostPipeline
    // / createSelenaPostBindGroup for post kind above). Originally built as a
    // pool-only proof-of-concept; generalized here into shared plumbing
    // (sceneWaterSelenaResourceRef / sceneWaterSpheresContextArray /
    // sceneWaterSelenaMaterial / sceneWaterSelenaUsesPass / getWaterSelenaMeshDraw
    // / getWaterSelenaPostDraw) so every migrated pass is a thin
    // material-builder + render-context-builder pair, not a copy-pasted clone
    // of the pool functions.
    //
    // The five feedback compute kernels (seed/drop/displacement/simulation/
    // normal) are OUT OF SCOPE (a separate task routes those through a Selena
    // feedback-compute host path); every pass below is a RENDER pass. Any
    // water pass/configuration NOT (yet) routable through Selena keeps using
    // its hand-written waterXxx*WGSL-style hardcoded pipeline/bind-group path
    // below unchanged (e.g. a rounded pool shape, which pool.sel does not
    // implement).
    // -----------------------------------------------------------------------



    // -----------------------------------------------------------------------
    // Draw list construction
    // -----------------------------------------------------------------------

    // -----------------------------------------------------------------------
    // Shadow pass
    // -----------------------------------------------------------------------




    // -----------------------------------------------------------------------
    // PBR object drawing
    // -----------------------------------------------------------------------

    // drawComputeParticlePasses draws hard systems in the main pass, then soft
    // systems in the read-only depth continuation pass when any exist.
    function drawComputeParticlePasses(encoder: any, pass: any, descriptor: any, frameGroup: any, records: any[], environment: any, timeSeconds: number, depthView: any) {
      var soft = s.softParticles.plan(records, depthView);
      Object.assign(telemetryState.frameStats, drawComputeParticleEntries(drawState, s.device, pass, soft ? s.softParticles.hard(records) : records, environment, timeSeconds));
      if (!soft) return pass;
      pass = s.softParticles.beginPass(encoder, pass, descriptor);
      pass.setBindGroup(0, frameGroup);
      wgpuSoftParticleMergeStats(telemetryState.frameStats, s.softParticles.draw(pass, records, environment, timeSeconds, depthView));
      return pass;
    }

    // -----------------------------------------------------------------------
    // Main render function
    // -----------------------------------------------------------------------


    // -----------------------------------------------------------------------
    // Dispose
    // -----------------------------------------------------------------------


    // Device + GPU resources were already initialized synchronously
    // above (using the pre-probed device from 16z). If that setup
    // failed, initFailed is true and render() will no-op; return null
    // so the mount code can try to fall back — though note the canvas
    // is already tainted at this point (getContext("webgpu") ran
    // before initGPUResources), so the fallback will itself fail.
    // The probe in 16z is what prevents us from ever reaching this
    // state on broken backends — it verifies device creation works
    // before we're allowed to construct a renderer at all.
    if (s.initFailed && !pipelineGuard.coreError && !pipelineGuard.pending) return sceneWebGPUFactoryFailure(canvas, "init-failed: " + s.initError);

    var textureVariantTokens = [];
    try {
      if (typeof window !== "undefined" && typeof window.__gosx_scene3d_texture_tokens === "function") {
        textureVariantTokens = window.__gosx_scene3d_texture_tokens(
          "webgpu",
          s.device && s.device.features ? Array.from(s.device.features) : []
        );
      }
    } catch (_textureVariantError) {
      textureVariantTokens = [];
    }
    s.textureVariantContext = {
      backend: "webgpu",
      // wgpuUploadKTX2Texture is this renderer's concrete upload path. The
      // reader/parser module may load later with glTF, so construction must not
      // freeze a false result from a page-global readiness gate.
      uploadReady: typeof wgpuUploadKTX2Texture === "function",
      tokens: Array.isArray(textureVariantTokens) ? textureVariantTokens.slice().sort() : [],
    };


    return {
      kind: "webgpu",
      type: "webgpu",
      supportsRetainedGeometry: true,
      supportsBundle: supportsBundle,
      prepareCrowdAtlas: function(atlas: any) { return webGPUCrowdHost(drawState, s.device).prepareCrowdAtlas(atlas); },
      prepareCrowdMotionShaders: function() { return webGPUCrowdHost(drawState, s.device).prepareCrowdMotionShaders(); }, texturePreparation: { generation: gpuRes.textureCache["_gosxGeneration"], load: (url: any, descriptor: any, role: any, colorSpace: any, retryFailed = false) => wgpuLoadTexture(s.device, url, gpuRes.textureCache, descriptor, role, colorSpace, retryFailed) },
      queuePick: function(x: any, y: any, width: any, height: any, callback: any) { return queuePick(drawState, s.device, x, y, width, height, callback); },
      setLifecycle: function(state: any) { return setLifecycle(waterState, state); },
      pollPerformanceSample: function() { return pollPerformanceSample(timingState, s.device); },
      getPerformanceTimingStatus: function() { return getPerformanceTimingStatus(timingState, s.device); },
      getFrameTiming: function() { return getFrameTiming(timingState, s.device, s.lastDeviceLostInfo); },
      diagnostics: function() { return diagnostics(s); }, iblStatus: function() { return wgpuIBLStatus(s); },
      getFrameProgress: function() { return telemetryState.webGPUFrameProof; }, shaderPrograms: function() { return { total: pipelineGuard.created, pending: pipelineGuard.pending, failed: pipelineGuard.failures.length }; },
      render: pipelineGuard.wrapFrame(function(bundle: any, viewport: any, frameMeta: any) { return render(s, bundle, viewport, frameMeta); }, function() { endWebGPUErrorScope(telemetryState, s.device); }),
      getFailureReason: function() { return pipelineGuard.coreError ? "webgpu-pipeline-failed" : ""; },
      dispose: function() { return dispose(s); },
      disablePostProcessing: function() { return disablePostProcessing(s); },
      enablePostProcessing: function() { return enablePostProcessing(s); },
      textureVariantContext: s.textureVariantContext,
    };
  }

  function sceneWebGPUCommandSequence(bundle, viewport, previousPrepared) {
    var prepared = prepareScene(
      bundle || {},
      bundle && bundle.camera || {},
      viewport || {},
      previousPrepared || null
    );
    return scenePreparedCommandSequence(prepared);
  }

  // -----------------------------------------------------------------------
  // Integration
  // -----------------------------------------------------------------------

  // --- Early WebGPU adapter probe ---
  // Adapter probe + sceneWebGPUAvailable + createSceneWebGPURendererOrFallback
  // used to live here. They've been moved to:
  //   - 16z-scene-webgpu-probe.ts (main scene3d bundle) — owns the
  //     probe, the stub sceneWebGPUAvailable, and the fallback factory
  //     that reads from window.__gosx_scene3d_webgpu_api.
  //   - This file is now loaded only via bootstrap-feature-scene3d-webgpu.js
  //     (the sub-feature chunk), whose suffix publishes
  //     createSceneWebGPURenderer to the api so the stub can dispatch.
  //

  // Local availability checks the probe shared by the main bundle.
  // _externalProbe is a function (not a snapshot) so each call sees
  // the current probe state — the main bundle's probe is async and
  // may still be pending when this chunk first loads.
  function sceneWebGPUAvailable() {
    var probe = _externalProbe();
    return probe.ready && probe.adapter !== false && probe.adapter !== null;
  }

  function sceneWebGPUUsesSharedMeshStreams(obj) { return obj && !obj.directVertices; }

  // Open the KTX2 variant-swap gate.
  //
  // 19-scene-gltf.js swaps an image URI for a block variant only when
  // sceneKTX2UploadPathReady() answers true, and that reads this global. The
  // rule mirrors the Go side, which refuses a variant whose file was never
  // built: a renderer that cannot upload a block container must keep serving
  // the PNG or JPEG, or the swap trades a working texture for a broken one.
  //
  // Register the WebGPU uploader here, at the end of the renderer file, so the
  // gate opens only when this renderer is really present. Reading the flag
  // rather than the function keeps 19-scene-gltf.js free of a backend choice.
  if (typeof window !== "undefined") {
    window.__gosx_scene3d_ktx2_texture_loader = wgpuUploadKTX2Texture;
  }
  // Exact material signatures also catch mutable callers without revisions.
  var materialCacheFields = "kind materialKind color roughness metalness emissive opacity clearcoat sheen transmission iridescence anisotropy unlit normalUVScale ior alphaCutoff specularIntensity specularColor normalScale occlusionStrength emissiveColor rimPower rimStrength rimColor specularAA thickness attenuationDistance attenuationColor texture normalMap roughnessMap metalnessMap emissiveMap occlusionMap specularIntensityMap specularColorMap thicknessMap textureDescriptors blendMode renderPass revision version".split(" ");
  // The signature of a material is read from its fields every frame, so an edit of
  // the material in place still shows. A material whose fields (an object field
  // by its JSON) equal the last read keeps its last signature string, which
  // spares the JSON of the whole field list and its allocations.
  var webGPUSignatureReads = new WeakMap();
  function webGPUMaterialSignature(material: any): string {
    var counters = window.__gosx_scene3d_counters;
    var mat = material || {}, last = material && typeof material === "object" ? webGPUSignatureReads.get(material) : undefined;
    var values = new Array(materialCacheFields.length), same = Boolean(last);
    for (var i = 0; i < values.length; i++) {
      var value = mat[materialCacheFields[i]];
      if (value !== null && typeof value === "object") value = JSON.stringify(value);
      values[i] = value;
      if (same && !Object.is(value, last.values[i])) same = false;
    }
    if (same) return last.signature;
    if (counters) counters.materialSignatures = (counters.materialSignatures || 0) + 1;
    var signature = JSON.stringify(materialCacheFields.map(key => mat[key]));
    if (material && typeof material === "object") webGPUSignatureReads.set(material, { values: values, signature: signature });
    return signature;
  }
  function webGPUPrepareMaterialFrame(materialFrame: any, device: any, bundle: any, epoch: number, createBuffer: any, destroyBuffer: any, buffers: Set<any>) {
    materialFrame.epoch = epoch;
    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
    var alignment = Math.max(16, sceneNumber(device.limits && device.limits.minUniformBufferOffsetAlignment, 256));
    materialFrame.stride = Math.ceil(SCENE_WEBGPU_CUTAWAY_SLOT_BYTES / alignment) * alignment;
    var required = 1 + objects.filter(webGPUCutawayHasSlot).length;
    var resized = !materialFrame.buffer || required > materialFrame.capacity;
    if (resized) {
      var capacity = Math.max(1, materialFrame.capacity);
      while (capacity < required) capacity *= 2;
      if (materialFrame.buffer) {
        buffers.delete(materialFrame.buffer);
        destroyBuffer(materialFrame.buffer);
      }
      materialFrame.capacity = capacity;
      materialFrame.buffer = createBuffer(GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
        capacity * materialFrame.stride);
      materialFrame.data = new Float32Array(capacity * materialFrame.stride / 4);
      materialFrame.words = new Uint32Array(materialFrame.data.buffer);
      for (var mi = 0; mi < 16; mi++) materialFrame.data[mi] = mi % 5 === 0 ? 1 : 0;
      device.queue.writeBuffer(materialFrame.buffer, 0, materialFrame.data.subarray(0, 16));
    }
    var slotIndex = 0, firstDirty = 0, lastDirty = 0, lastBytes = 64;
    for (var i = 0; i < objects.length; i++) {
      var object = objects[i];
      if (!webGPUCutawayHasSlot(object)) continue;
      var matrix = object.retainedGeometry ? webGPUObjectModelMatrix(object) : null;
      var offset = ++slotIndex * materialFrame.stride;
      var slotKey = webGPUCutawayKey(object, matrix);
      if (slotKey) materialFrame.offsets.set(slotKey, offset);
      var changed = resized;
      for (var column = 0; column < 16; column++) {
        var index = offset / 4 + column, before = materialFrame.words[index];
        materialFrame.data[index] = matrix
          ? sceneNumber(matrix[column], column % 5 === 0 ? 1 : 0) : (column % 5 === 0 ? 1 : 0);
        // Compare stored float32 bits, including the rounding of wire numbers.
        if (materialFrame.words[index] !== before) changed = true;
      }
      var cutChanged = webGPUCutawayWrite(materialFrame, offset, object.cut);
      if (changed || cutChanged) { if (!firstDirty) firstDirty = slotIndex; lastDirty = slotIndex; lastBytes = cutChanged || resized ? SCENE_WEBGPU_CUTAWAY_SLOT_BYTES : 64; }
    }
    if (lastDirty) device.queue.writeBuffer(materialFrame.buffer, firstDirty * materialFrame.stride,
      materialFrame.data.subarray(firstDirty * materialFrame.stride / 4, (lastDirty * materialFrame.stride + lastBytes) / 4));
    var materials = Array.isArray(bundle.materials) ? bundle.materials : [];
    var signatures = materialFrame.signatures;
    signatures.length = materials.length;
    if (objects.some(object => object && !materials[object.materialIndex])) signatures.length++;
    materialFrame.groups = [];
    for (var index = 0; index < signatures.length; index++) {
      var signature = signatures[index] = webGPUMaterialSignature(materials[index]);
      for (var shadow = 0; shadow < 2; shadow++) {
        var record = materialFrame.resources.get(signature + ":" + shadow);
        if (record) record.seenEpoch = epoch;
      }
    }
    materialFrame.resources.forEach(function(record: any, key: string) {
      if (record.seenEpoch === epoch) return;
      for (var slot of ["_gosxWGPUMaterialUniform", "_gosxWGPUMaterialShadowUniform"]) {
        var buffer = record.owner[slot];
        if (buffer) { buffers.delete(buffer); destroyBuffer(buffer); }
      }
      materialFrame.resources.delete(key);
    });
  }
  function webGPUFrameMaterialGroup(materialFrame: any, index: number, material: any, receiveShadow: boolean, createGroup: any): any {
    var shadow = receiveShadow ? 1 : 0;
    var groups = materialFrame.groups[index];
    if (!groups) groups = materialFrame.groups[index] = [];
    if (groups[shadow]) return groups[shadow];
    var key = (materialFrame.signatures[index] || webGPUMaterialSignature(material)) + ":" + shadow;
    var record = materialFrame.resources.get(key);
    if (!record) {
      record = { owner: {}, bg: null, textureRevision: -1, modelBuffer: null, seenEpoch: materialFrame.epoch };
      materialFrame.resources.set(key, record);
    }
    record.seenEpoch = materialFrame.epoch;
    if (!record.bg || record.textureRevision !== materialFrame.textureRevision || record.modelBuffer !== materialFrame.buffer) {
      record.bg = createGroup(material, receiveShadow, record.owner);
      record.textureRevision = materialFrame.textureRevision; record.modelBuffer = materialFrame.buffer;
    }
    return groups[shadow] = record.bg;
  }
  function sceneWebGPUIsSelenaMaterial(material: any) {
    return Boolean(material && material.shaderBackend === "selena" && sceneSelenaMaterialLayout(material) && !sceneWebGPUSelenaSurfaceHook(material) &&
      ((typeof material.customVertexWGSL === "string" && material.customVertexWGSL.trim()) ||
       (typeof material.customFragmentWGSL === "string" && material.customFragmentWGSL.trim())));
  }
  function webGPUCustomMaterialStats(materials: any) {
    var stats = { customMaterialFallbacks: 0, customWGSLFallbacks: 0, customUniformFallbacks: 0 };
    var source = Array.isArray(materials) ? materials : [];
    for (var i = 0; i < source.length; i++) {
      var material = source[i] || {};
      if (sceneWebGPUIsSelenaMaterial(material)) continue;
      var hasWGSL = (typeof material.customVertexWGSL === "string" && material.customVertexWGSL.trim()) ||
        (typeof material.customFragmentWGSL === "string" && material.customFragmentWGSL.trim());
      var hasCustomUniforms = material.customUniforms && typeof material.customUniforms === "object" && Object.keys(material.customUniforms).length > 0;
      if (!hasWGSL && !hasCustomUniforms) continue;
      stats.customMaterialFallbacks += 1;
      if (hasWGSL) stats.customWGSLFallbacks += 1;
      if (hasCustomUniforms) stats.customUniformFallbacks += 1;
    }
    return stats;
  }
