  // Per-pass GPU timing.
  // These optional pass counters describe shadow and main draws only.
  // The frame timer also includes compute, water, picking and post effects.
  // Slot layout, per ring entry:
  //   0 shadow begin   1 shadow end   2 main begin   3 main end
  const SCENE_WEBGPU_PASS_STAMPS = 4;
  const SCENE_WEBGPU_PASS_RING = 3;

  function ensureGPUTiming(timingState, device) {
    if (timingState.gpuTimingDisposed) return false;
    if (timingState.gpuTiming !== null) return timingState.gpuTiming;
    timingState.gpuTiming = false;
    var candidateQuerySet = null;
    var candidateBuffers = [];
    try {
      var supportsTimestamps = device && device.features && typeof device.features.has === "function" && device.features.has("timestamp-query");
      if (!supportsTimestamps || typeof device.createQuerySet !== "function") {
        // timestamp-query is optional in WebGPU and Edge/D3D commonly omits
        // it even while the renderer is submitting healthy GPU work. Keep
        // the timing capability distinct from renderer activity so a
        // missing hardware timer is never reported as "GPU unsupported".
        if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-timing", "timer-unavailable");
        return timingState.gpuTiming;
      }
      candidateQuerySet = device.createQuerySet({ type: "timestamp", count: 6 });
      var slots = [];
      for (var i = 0; i < 3; i++) {
        var resolveBuffer = device.createBuffer({ size: 16, usage: GPUBufferUsage.QUERY_RESOLVE | GPUBufferUsage.COPY_SRC });
        candidateBuffers.push(resolveBuffer);
        var readbackBuffer = device.createBuffer({ size: 16, usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ });
        candidateBuffers.push(readbackBuffer);
        slots.push({
          resolve: resolveBuffer,
          readback: readbackBuffer,
          pending: false,
          mapping: false,
          frameSeq: 0,
        });
      }
      timingState.gpuTiming = {
        querySet: candidateQuerySet,
        slots: slots,
      };
      timingState.gpuTimingEncodingAvailable = null;
      timingState.gpuTimingFailed = false;
      if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-timing", "pending");
    } catch (error) {
      if (candidateQuerySet && typeof candidateQuerySet.destroy === "function") candidateQuerySet.destroy();
      for (var candidateIndex = 0; candidateIndex < candidateBuffers.length; candidateIndex++) {
        var candidateBuffer = candidateBuffers[candidateIndex];
        if (candidateBuffer && typeof candidateBuffer.destroy === "function") candidateBuffer.destroy();
      }
      timingState.gpuTiming = false;
      timingState.gpuTimingFailed = true;
      timingState.lastGPUPerformanceSample = null;
      if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-timing", "failed");
    }
    return timingState.gpuTiming;
  }

  function disableGPUTiming(timingState, timing) {
    if (!timing || timing === false) return;
    if (timingState.gpuTiming === timing) timingState.gpuTiming = false;
    timingState.gpuTimingFailed = true;
    timingState.lastGPUCompletionSample = null;
    timingState.failedGPUTimings.push({ timing: timing, retireAfterFrame: timingState.gpuTimingFrameSeq + 3 });
    timingState.lastGPUPerformanceSample = null;
    if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-timing", "failed");
  }

  function drainDeferredGPUResources(timingState, force) {
    for (var i = timingState.failedGPUTimings.length - 1; i >= 0; i--) {
      if (!force && timingState.gpuTimingFrameSeq < timingState.failedGPUTimings[i].retireAfterFrame) continue;
      destroyGPUTimingResources(timingState.failedGPUTimings[i].timing);
      timingState.failedGPUTimings.splice(i, 1);
    }
    for (var j = timingState.deferredWaterTextureRetirements.length - 1; j >= 0; j--) {
      var retirement = timingState.deferredWaterTextureRetirements[j];
      if (!force && timingState.gpuTimingFrameSeq < retirement.retireAfterFrame) continue;
      for (var k = 0; k < retirement.textures.length; k++) {
        destroyRendererGPUResource(retirement.textures[k]);
      }
      timingState.deferredWaterTextureRetirements.splice(j, 1);
    }
    for (var l = timingState.deferredWaterSystemRetirements.length - 1; l >= 0; l--) {
      var systemRetirement = timingState.deferredWaterSystemRetirements[l];
      if (!force && timingState.gpuTimingFrameSeq < systemRetirement.retireAfterFrame) continue;
      if (systemRetirement.system && typeof systemRetirement.system.dispose === "function") {
        try { systemRetirement.system.dispose(); } catch (_err) {}
      }
      timingState.deferredWaterSystemRetirements.splice(l, 1);
    }
  }

  function pollGPUTimingReadback(timingState, device) {
    var timing = ensureGPUTiming(timingState, device);
    if (!timing) return;
    for (var i = 0; i < timing.slots.length; i++) {
      var slot = timing.slots[i];
      if (!slot.pending || slot.mapping) continue;
      if (!slot.readback || typeof slot.readback.mapAsync !== "function") continue;
      slot.mapping = true;
      (function(activeTiming, activeSlot) {
        activeSlot.readback.mapAsync((typeof GPUMapMode !== "undefined" && GPUMapMode.READ) || 1).then(function() {
          if (!activeSlot.readback || typeof activeSlot.readback.getMappedRange !== "function") return;
          var values = new BigUint64Array(activeSlot.readback.getMappedRange().slice(0));
          if (timingState.gpuTiming === activeTiming && values.length >= 2 && values[1] > values[0]) {
            timingState.lastGPUPerformanceSample = {
              source: "gpu-timestamp",
              scope: "frame",
              frameSeq: activeSlot.frameSeq,
              gpuMS: Number(values[1] - values[0]) / 1000000,
              atMS: (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now(),
            };
            wgpuEmitGPUTiming(timingState.lastGPUPerformanceSample);
            // @ts-ignore TS7005 -- readback initializes the frame sample asynchronously.
            timingState.lastGPUCompletionSample = sceneLatestGPUCompletion(timingState.lastGPUCompletionSample, timingState.lastGPUPerformanceSample);
            if (timingState.mount) {
              timingState.queue("data-gosx-scene3d-webgpu-gpu-ms", timingState.lastGPUPerformanceSample.gpuMS.toFixed(3));
              timingState.queue("data-gosx-scene3d-webgpu-gpu-timing", "measured");
            }
          }
          activeSlot.readback.unmap();
          activeSlot.pending = false;
          activeSlot.mapping = false;
        }).catch(function() {
          activeSlot.pending = false;
          activeSlot.mapping = false;
          if (timingState.gpuTiming === activeTiming) disableGPUTiming(timingState, activeTiming);
        });
      })(timing, slot);
      break;
    }
  }

  function beginGPUFrameTiming(timingState, device, encoder) {
    pollGPUTimingReadback(timingState, device);
    var timing = ensureGPUTiming(timingState, device);
    if (!timing || !encoder) return null;
    if (typeof encoder.beginComputePass !== "function" || typeof encoder.resolveQuerySet !== "function" || typeof encoder.copyBufferToBuffer !== "function") {
      timingState.gpuTimingEncodingAvailable = false;
      if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-timing", "timer-unavailable");
      return null;
    }
    var startIndex = timingState.gpuTimingFrameSeq % timing.slots.length;
    for (var i = 0; i < timing.slots.length; i++) {
      var slotIndex = (startIndex + i) % timing.slots.length;
      var slot = timing.slots[slotIndex];
      if (!slot || slot.pending || slot.mapping) continue;
      try {
        encoder.beginComputePass({ label: "gosx-frame-timer-start", timestampWrites: {
          querySet: timing.querySet, beginningOfPassWriteIndex: slotIndex * 2,
        } }).end();
        timingState.gpuTimingEncodingAvailable = true;
        return { timing: timing, slot: slot, slotIndex: slotIndex };
      } catch (_timestampBeginError) {
        disableGPUTiming(timingState, timing);
        return null;
      }
    }
    return null;
  }

  function endGPUFrameTiming(timingState, encoder, token) {
    if (!token) return;
    try {
      encoder.beginComputePass({ label: "gosx-frame-timer-end", timestampWrites: {
        querySet: token.timing.querySet, endOfPassWriteIndex: token.slotIndex * 2 + 1,
      } }).end();
      encoder.resolveQuerySet(token.timing.querySet, token.slotIndex * 2, 2, token.slot.resolve, 0);
      encoder.copyBufferToBuffer(token.slot.resolve, 0, token.slot.readback, 0, 16);
      token.slot.pending = true;
      token.slot.frameSeq = timingState.gpuTimingFrameSeq;
    } catch (_timestampEndError) {
      token.slot.pending = false;
      token.slot.mapping = false;
      disableGPUTiming(timingState, token.timing);
    }
  }

  function ensureGPUPassTiming(timingState, device) {
    if (!((typeof window !== "undefined" && window.__gosx_scene3d_webgpu_pass_timing === true) || webGPUPerformanceTelemetry())) return false;
    if (timingState.gpuPassTiming !== null) return timingState.gpuPassTiming;
    timingState.gpuPassTiming = false;
    var querySet = null;
    var buffers = [];
    try {
      var supported = device && device.features && typeof device.features.has === "function" && device.features.has("timestamp-query");
      if (!supported || typeof device.createQuerySet !== "function") {
        if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-timing", "timer-unavailable");
        return timingState.gpuPassTiming;
      }
      querySet = device.createQuerySet({
        type: "timestamp",
        count: SCENE_WEBGPU_PASS_STAMPS * SCENE_WEBGPU_PASS_RING,
      });
      var slots = [];
      for (var i = 0; i < SCENE_WEBGPU_PASS_RING; i++) {
        var resolve = device.createBuffer({
          label: "gosx-pass-timing-resolve",
          size: SCENE_WEBGPU_PASS_STAMPS * 8,
          usage: GPUBufferUsage.QUERY_RESOLVE | GPUBufferUsage.COPY_SRC,
        });
        var readback = device.createBuffer({
          label: "gosx-pass-timing-readback",
          size: SCENE_WEBGPU_PASS_STAMPS * 8,
          usage: GPUBufferUsage.COPY_DST | GPUBufferUsage.MAP_READ,
        });
        buffers.push(resolve, readback);
        slots.push({ resolve: resolve, readback: readback, pending: false, mapping: false, frameSeq: 0, hasShadow: false });
      }
      timingState.gpuPassTiming = {
        querySet: querySet,
        slots: slots,
      };
      if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-timing", "pending");
    } catch (_passTimingError) {
      if (querySet && typeof querySet.destroy === "function") querySet.destroy();
      for (var b = 0; b < buffers.length; b++) {
        if (buffers[b] && typeof buffers[b].destroy === "function") buffers[b].destroy();
      }
      timingState.gpuPassTiming = false;
      if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-timing", "failed");
    }
    return timingState.gpuPassTiming;
  }

  // beginGPUPassTimingFrame picks the ring slot this frame writes into. It
  // returns null when no slot is free, so a frame never overwrites a slot whose
  // readback is still in flight.
  function beginGPUPassTimingFrame(timingState, device) {
    timingState.gpuPassTimingSlot = null;
    timingState.gpuPassTimingShadowUsed = false;
    var timing = ensureGPUPassTiming(timingState, device);
    if (!timing) return;
    var start = timingState.gpuTimingFrameSeq % timing.slots.length;
    for (var i = 0; i < timing.slots.length; i++) {
      var index = (start + i) % timing.slots.length;
      var slot = timing.slots[index];
      if (!slot || slot.pending || slot.mapping) continue;
      slot.hasShadow = false;
      timingState.gpuPassTimingSlot = { timing: timing, slot: slot, index: index };
      return;
    }
  }

  // gpuPassTimestampWrites returns the timestampWrites field for one pass, or
  // null. Spread it into the beginRenderPass descriptor only when it is not
  // null: a null field is a validation error on some implementations.
  //
  // Only the FIRST shadow pass is stamped. A second directional light opens a
  // second shadow pass, and the ring holds one pair, so stamping both would
  // report the second pass and hide the first.
  function gpuPassTimestampWrites(timingState, passName) {
    if (!timingState.gpuPassTimingSlot) return null;
    var base = timingState.gpuPassTimingSlot.index * SCENE_WEBGPU_PASS_STAMPS;
    if (passName === "shadow") {
      if (timingState.gpuPassTimingShadowUsed) return null;
      timingState.gpuPassTimingShadowUsed = true;
      timingState.gpuPassTimingSlot.slot.hasShadow = true;
      return { querySet: timingState.gpuPassTimingSlot.timing.querySet, beginningOfPassWriteIndex: base, endOfPassWriteIndex: base + 1 };
    }
    if (passName === "main") {
      return { querySet: timingState.gpuPassTimingSlot.timing.querySet, beginningOfPassWriteIndex: base + 2, endOfPassWriteIndex: base + 3 };
    }
    return null;
  }

  // endGPUPassTimingFrame resolves the slot into its readback buffer. Call it
  // once, after the last timed pass and before submit.
  function endGPUPassTimingFrame(timingState, encoder) {
    if (!timingState.gpuPassTimingSlot || !encoder) return;
    if (typeof encoder.resolveQuerySet !== "function" || typeof encoder.copyBufferToBuffer !== "function") {
      timingState.gpuPassTimingSlot = null;
      return;
    }
    var token = timingState.gpuPassTimingSlot;
    timingState.gpuPassTimingSlot = null;
    try {
      encoder.resolveQuerySet(
        token.timing.querySet,
        token.index * SCENE_WEBGPU_PASS_STAMPS,
        SCENE_WEBGPU_PASS_STAMPS,
        token.slot.resolve,
        0
      );
      encoder.copyBufferToBuffer(token.slot.resolve, 0, token.slot.readback, 0, SCENE_WEBGPU_PASS_STAMPS * 8);
      token.slot.pending = true;
      token.slot.frameSeq = timingState.gpuTimingFrameSeq;
    } catch (_passResolveError) {
      token.slot.pending = false;
      token.slot.mapping = false;
      timingState.gpuPassTiming = false;
    }
  }

  function pollGPUPassTimingReadback(timingState) {
    var timing = timingState.gpuPassTiming;
    if (!timing) return;
    for (var i = 0; i < timing.slots.length; i++) {
      var slot = timing.slots[i];
      if (!slot.pending || slot.mapping || timingState.gpuTimingFrameSeq - slot.frameSeq < 2) continue;
      if (!slot.readback || typeof slot.readback.mapAsync !== "function") continue;
      slot.mapping = true;
      (function(activeTiming, activeSlot) {
        activeSlot.readback.mapAsync((typeof GPUMapMode !== "undefined" && GPUMapMode.READ) || 1).then(function() {
          if (activeSlot.readback && typeof activeSlot.readback.getMappedRange === "function") {
            var values = new BigUint64Array(activeSlot.readback.getMappedRange().slice(0));
            recordGPUPassSample(timingState, activeSlot, values);
            activeSlot.readback.unmap();
          }
          activeSlot.pending = false;
          activeSlot.mapping = false;
        }).catch(function() {
          activeSlot.pending = false;
          activeSlot.mapping = false;
        });
      })(timing, slot);
      break;
    }
  }

  // recordGPUPassSample turns four raw timestamps into milliseconds. A zero or
  // decreasing pair means the implementation did not write that stamp, so the
  // reading is dropped rather than published as 0.
  function recordGPUPassSample(timingState, slot, values) {
    if (!values || values.length < SCENE_WEBGPU_PASS_STAMPS) return;
    var toMS = function(begin, end) {
      if (end <= begin) return -1;
      return Number(end - begin) / 1000000;
    };
    var shadowMS = slot.hasShadow ? toMS(values[0], values[1]) : 0;
    var mainMS = toMS(values[2], values[3]);
    if (mainMS < 0) return;
    // The whole-scene span runs from the first stamped pass to the end of the
    // main pass, so it includes the gap between passes — which is real GPU
    // time the frame spent.
    var sceneBegin = slot.hasShadow ? values[0] : values[2];
    var sceneMS = toMS(sceneBegin, values[3]);
    timingState.lastGPUPassSample = {
      source: "gpu-timestamp", scope: "passes", frameSeq: slot.frameSeq,
      shadowMS: shadowMS < 0 ? 0 : shadowMS,
      mainMS: mainMS,
      sceneMS: sceneMS < 0 ? mainMS : sceneMS,
      atMS: (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now(),
    };
    wgpuEmitGPUTiming(timingState.lastGPUPassSample);
    if (timingState.mount) {
      timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-shadow-ms", timingState.lastGPUPassSample.shadowMS.toFixed(3));
      timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-main-ms", timingState.lastGPUPassSample.mainMS.toFixed(3));
      timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-scene-ms", timingState.lastGPUPassSample.sceneMS.toFixed(3));
      timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-timing", "measured");
    }

  }

  function destroyGPUPassTimingResources(timingState) {
    var timing = timingState.gpuPassTiming;
    timingState.gpuPassTiming = null;
    timingState.gpuPassTimingSlot = null;
    destroyGPUTimingResources(timing);
  }

  function pollPerformanceSample(timingState, device) {
    pollGPUTimingReadback(timingState, device);
    pollGPUPassTimingReadback(timingState);
    var sample = timingState.lastGPUPerformanceSample;
    timingState.lastGPUPerformanceSample = null;
    return sample;
  }

  function getFrameTiming(timingState, device, lastDeviceLostInfo) {
    pollGPUTimingReadback(timingState, device);
    // @ts-ignore TS7005 -- readback initializes the frame sample asynchronously.
    return sceneWebGPUCompletionSnapshot(getPerformanceTimingStatus(timingState, device), timingState.lastGPUCompletionSample, timingState.gpuTimingDisposed, lastDeviceLostInfo);
  }

  function getPerformanceTimingStatus(timingState, device) {
    var timing = ensureGPUTiming(timingState, device);
    var active = Boolean(timing && timing !== false && Array.isArray(timing.slots) && timingState.gpuTimingEncodingAvailable === true);
    var pending = active && timingState.gpuTiming.slots.some(function(slot) { return slot && (slot.pending || slot.mapping); });
    return { available: active, active: active, pending: pending, failed: timingState.gpuTimingFailed, source: "gpu-timestamp" };
  }
