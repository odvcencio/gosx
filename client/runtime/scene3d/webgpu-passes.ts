  // Per-pass GPU timing.
  // These optional pass counters describe shadow and main draws only.
  // The frame timer also includes compute, water, picking and post effects.
  // Slot layout, per ring entry:
  //   0 shadow begin   1 shadow end   2 main begin   3 main end
  //   4, 5 the frame-end marker: an empty render pass that loads and stores
  //   the presented image after post-processing. Because it reads what post
  //   wrote, the GPU runs it after post; an independent pass is not ordered on
  //   tile GPUs (Apple), where a one-texel stamp pass ran early. 5 minus the
  //   first stamped begin is the frame from the shadow pass to the image.
  const SCENE_WEBGPU_PASS_STAMPS = 6;
  const SCENE_WEBGPU_PASS_RING = 3;

  function ensureGPUTiming(timingState: any, device: any) {
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

  function disableGPUTiming(timingState: any, timing) {
    if (!timing || timing === false) return;
    if (timingState.gpuTiming === timing) timingState.gpuTiming = false;
    timingState.gpuTimingFailed = true;
    timingState.lastGPUCompletionSample = null;
    timingState.failedGPUTimings.push({ timing: timing, retireAfterFrame: timingState.gpuTimingFrameSeq + 3 });
    timingState.lastGPUPerformanceSample = null;
    if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-timing", "failed");
  }

  function drainDeferredGPUResources(timingState: any, force) {
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

  // The frame timer stamps with empty compute passes. Metal (Chrome 157,
  // Apple M4 Pro) writes 0 for both stamps of an empty pass, so the frame
  // timer never measures there, and a stamp pass that does work does not span
  // the render passes on that tile GPU either. After three unusable reads the
  // renderer stops stamping the frame and publishes the render-pass span
  // (shadow begin to main end) as its GPU time instead; post effects are not
  // in that span.
  function noteGPUFrameTimerMiss(timingState: any) {
    if (timingState.gpuFrameTimerUnusable) return;
    timingState.gpuFrameTimerZeroReads = (timingState.gpuFrameTimerZeroReads || 0) + 1;
    if (timingState.gpuFrameTimerZeroReads < 3) return;
    timingState.gpuFrameTimerUnusable = true;
    if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-timing", "pass-span");
  }

  function pollGPUTimingReadback(timingState: any, device: any) {
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
          if (timingState.gpuTiming === activeTiming && values.length >= 2 && values[1] <= values[0]) {
            noteGPUFrameTimerMiss(timingState);
          }
          if (timingState.gpuTiming === activeTiming && values.length >= 2 && values[1] > values[0]) {
            timingState.gpuFrameTimerZeroReads = 0;
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

  function beginGPUFrameTiming(timingState: any, device: any, encoder) {
    pollGPUTimingReadback(timingState, device);
    var timing = ensureGPUTiming(timingState, device);
    if (!timing || !encoder || timingState.gpuFrameTimerUnusable) return null;
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

  function endGPUFrameTiming(timingState: any, encoder, token) {
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

  function ensureGPUPassTiming(timingState: any, device: any) {
    if (!((typeof window !== "undefined" && window.__gosx_scene3d_webgpu_pass_timing === true) || webGPUPerformanceTelemetry() || timingState.gpuFrameTimerUnusable === true)) return false;
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
        slots.push({ resolve: resolve, readback: readback, pending: false, mapping: false, frameSeq: 0, hasShadow: false, hasFrameEnd: false });
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
  function beginGPUPassTimingFrame(timingState: any, device: any) {
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
      slot.hasFrameEnd = false;
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
  function gpuPassTimestampWrites(timingState: any, passName) {
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

  // wgpuFrameEndMarker encodes the frame-end marker (stamps 4 and 5) on the
  // presented view. It draws nothing; the load and store keep the image.
  function wgpuFrameEndMarker(token: any, encoder: any, finalView: any) {
    if (!finalView || typeof encoder.beginRenderPass !== "function") return false;
    var base = token.index * SCENE_WEBGPU_PASS_STAMPS;
    try {
      encoder.beginRenderPass({ label: "gosx-frame-end-marker", colorAttachments: [{ view: finalView, loadOp: "load", storeOp: "store" }],
        timestampWrites: { querySet: token.timing.querySet, beginningOfPassWriteIndex: base + 4, endOfPassWriteIndex: base + 5 } }).end();
      return true;
    } catch (_frameEndMarkerError) {
      return false;
    }
  }

  // endGPUPassTimingFrame resolves the slot into its readback buffer. Call it
  // once, after the last timed pass and before submit. finalView, when given,
  // is the presented image, and the frame-end marker is encoded on it.
  function endGPUPassTimingFrame(timingState: any, encoder, finalView?: any) {
    if (!timingState.gpuPassTimingSlot || !encoder) return;
    if (typeof encoder.resolveQuerySet !== "function" || typeof encoder.copyBufferToBuffer !== "function") {
      timingState.gpuPassTimingSlot = null;
      return;
    }
    var token = timingState.gpuPassTimingSlot;
    timingState.gpuPassTimingSlot = null;
    try {
      token.slot.hasFrameEnd = wgpuFrameEndMarker(token, encoder, finalView);
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

  function pollGPUPassTimingReadback(timingState: any) {
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
  function recordGPUPassSample(timingState: any, slot, values) {
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
    // The frame span runs to the end of the frame-end marker; the tail runs
    // from the main pass's end to the marker's end (picking, post and the
    // marker's own load and store). Only the marker's end is ordered after
    // post: on a tile GPU a pass can begin before the one ahead of it ends, so
    // its begin stamp is reported (markerLeadMS) but not used. Both spans are
    // -1 when the frame had no marker or its end is not after the main pass.
    var markerEnd = slot.hasFrameEnd && values[5] > values[3];
    var frameMS = markerEnd ? toMS(sceneBegin, values[5]) : -1;
    var tailMS = markerEnd ? Number(values[5] - values[3]) / 1000000 : -1;
    var markerLeadMS = slot.hasFrameEnd ? Number(values[4] - values[3]) / 1000000 : null;
    timingState.lastGPUPassSample = {
      source: "gpu-timestamp", scope: "passes", frameSeq: slot.frameSeq,
      shadowMS: shadowMS < 0 ? 0 : shadowMS,
      mainMS: mainMS,
      sceneMS: sceneMS < 0 ? mainMS : sceneMS,
      frameMS: frameMS, tailMS: tailMS, markerLeadMS: markerLeadMS,
      atMS: (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now(),
    };
    wgpuEmitGPUTiming(timingState.lastGPUPassSample);
    if (timingState.gpuFrameTimerUnusable) {
      timingState.lastGPUPerformanceSample = {
        source: "gpu-pass-timestamp", scope: "passes", frameSeq: slot.frameSeq,
        gpuMS: timingState.lastGPUPassSample.sceneMS, atMS: timingState.lastGPUPassSample.atMS,
      };
      if (timingState.mount) timingState.queue("data-gosx-scene3d-webgpu-gpu-ms", timingState.lastGPUPassSample.sceneMS.toFixed(3));
    }
    if (timingState.mount) {
      timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-shadow-ms", timingState.lastGPUPassSample.shadowMS.toFixed(3));
      timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-main-ms", timingState.lastGPUPassSample.mainMS.toFixed(3));
      timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-scene-ms", timingState.lastGPUPassSample.sceneMS.toFixed(3));
      if (frameMS > 0) {
        timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-frame-ms", frameMS.toFixed(3));
        timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-tail-ms", tailMS.toFixed(3));
      }
      timingState.queue("data-gosx-scene3d-webgpu-gpu-pass-timing", "measured");
    }

  }

  function destroyGPUPassTimingResources(timingState: any) {
    var timing = timingState.gpuPassTiming;
    timingState.gpuPassTiming = null;
    timingState.gpuPassTimingSlot = null;
    destroyGPUTimingResources(timing);
  }

  function pollPerformanceSample(timingState: any, device: any) {
    pollGPUTimingReadback(timingState, device);
    pollGPUPassTimingReadback(timingState);
    var sample = timingState.lastGPUPerformanceSample;
    timingState.lastGPUPerformanceSample = null;
    return sample;
  }

  function getFrameTiming(timingState: any, device: any, lastDeviceLostInfo) {
    pollGPUTimingReadback(timingState, device);
    // @ts-ignore TS7005 -- readback initializes the frame sample asynchronously.
    return sceneWebGPUCompletionSnapshot(getPerformanceTimingStatus(timingState, device), timingState.lastGPUCompletionSample, timingState.gpuTimingDisposed, lastDeviceLostInfo);
  }

  function getPerformanceTimingStatus(timingState: any, device: any) {
    var timing = ensureGPUTiming(timingState, device);
    if (timingState.gpuFrameTimerUnusable) {
      var passTiming = timingState.gpuPassTiming;
      var passActive = Boolean(passTiming && passTiming !== false && Array.isArray(passTiming.slots));
      var passPending = passActive && passTiming.slots.some(function(slot: any) { return slot && (slot.pending || slot.mapping); });
      return { available: passActive, active: passActive, pending: passPending, failed: !passActive, source: "gpu-pass-timestamp" };
    }
    var active = Boolean(timing && timing !== false && Array.isArray(timing.slots) && timingState.gpuTimingEncodingAvailable === true);
    var pending = active && timingState.gpuTiming.slots.some(function(slot) { return slot && (slot.pending || slot.mapping); });
    return { available: active, active: active, pending: pending, failed: timingState.gpuTimingFailed, source: "gpu-timestamp" };
  }

  function ensureScratch(meshState: any, name, length) {
    if (name === "positions") {
      if (!meshState.scratchPositions || meshState.scratchPositions.length < length) meshState.scratchPositions = new Float32Array(length);
      return meshState.scratchPositions;
    }
    if (name === "normals") {
      if (!meshState.scratchNormals || meshState.scratchNormals.length < length) meshState.scratchNormals = new Float32Array(length);
      return meshState.scratchNormals;
    }
    if (name === "uvs") {
      if (!meshState.scratchUVs || meshState.scratchUVs.length < length) meshState.scratchUVs = new Float32Array(length);
      return meshState.scratchUVs;
    }
    if (name === "tangents") {
      if (!meshState.scratchTangents || meshState.scratchTangents.length < length) meshState.scratchTangents = new Float32Array(length);
      return meshState.scratchTangents;
    }
    return new Float32Array(length);
  }

  function wgpuCreateTrackedBuffer(meshState: any, device: any, usage, dataOrSize) {
    var size = typeof dataOrSize === "number"
      ? wgpuAlignUp(Math.max(dataOrSize, 4), 4)
      : wgpuAlignUp(Math.max(dataOrSize.byteLength, 4), 4);
    var buffer = wgpuCreateBuffer(device, usage, dataOrSize);
    try { buffer._gosxByteLength = size; } catch (_err) {}
    meshState.pointsEntryGPUBuffers.add(buffer);
    return buffer;
  }

  function wgpuUploadTrackedBuffer(meshState: any, device: any, usage, buffer, data, state) {
    var needed = wgpuAlignUp(Math.max(data && data.byteLength || 0, 4), 4);
    if (state && state.bytesChanged && wgpuTrackedBufferSize(buffer) < needed) {
      if (buffer && typeof buffer.destroy === "function") {
        meshState.pointsEntryGPUBuffers.delete(buffer);
        buffer.destroy();
      }
      buffer = wgpuCreateTrackedBuffer(meshState, device, usage, needed);
    }
    device.queue.writeBuffer(buffer, 0, data);
    return buffer;
  }

  // Owned snapshots compare once per frame/input, reusing storage on edits.
  // Fresh equal arrays and repeated passes skip allocation and uploads.
  function wgpuStablePBRAttributeBuffer(meshState: any, device: any, slot, typedArray, owner = meshState.pbrSceneAttributeCache, bufferSlot = slot + "B") {
    var frame = owner[slot + "Frame"] || (owner[slot + "Frame"] = { epoch: -1, data: null });
    if (frame.epoch === meshState.instancedCacheOwnerEpoch && frame.data === typedArray && meshState.pointsEntryGPUBuffers.has(owner[bufferSlot])) return owner[bufferSlot];
    var snap = owner[slot];
    var changed = !wgpuArrayContentsMatch(snap, typedArray);
    if (changed) {
      if (snap && snap.length === typedArray.length) snap.set(typedArray);
      else owner[slot] = snap = typedArray.slice();
    }
    var buffer = wgpuCachedTrackedBuffer(meshState, device, owner, bufferSlot, snap, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, changed);
    frame.epoch = meshState.instancedCacheOwnerEpoch; frame.data = typedArray;
    return buffer;
  }
  function wgpuCachedTrackedBuffer(meshState: any, device: any, owner, slot, typedArray, usage, dynamic) {
    if (!owner || !typedArray) return null;
    // Cross-renderer staleness guard: if the buffer cached at owner[slot] was
    // created by a prior renderer (not in this renderer's pointsEntryGPUBuffers
    // set), the underlying GPU resource was destroyed when that renderer was
    // disposed or lost its device. Clear the stale JS reference so that the
    // alloc path below creates a fresh buffer on the current device. Without
    // this, sceneCachedSlotBuffer would blindly reuse the dead buffer (its .size
    // property survives .destroy()), causing "Buffer with '' label is invalid"
    // errors on every frame after device-loss recovery until the page is reloaded.
    var staleCandidate = owner[slot];
    if (staleCandidate && !meshState.pointsEntryGPUBuffers.has(staleCandidate)) {
      owner[slot] = null;
      // Drop bind-group pools referencing this foreign buffer.
      var staleBGPool = "_gosxWGPUSBGC" + slot;
      if (Array.isArray(owner[staleBGPool])) owner[staleBGPool] = [];
      // Clear the material bind-group single-entry caches that key on the
      // materialBuffer identity (both shadow and non-shadow variants).
      owner["_gosxWGPUMatBGCache"] = null;
      owner["_gosxWGPUMatBGCacheS"] = null;
    }
    if (typeof sceneCachedBuffer === "function") {
      return sceneCachedBuffer(owner, typedArray, function(data) {
        return wgpuCreateTrackedBuffer(meshState, device, usage, data && data.byteLength || 4);
      }, function(buffer, data, state) {
        return wgpuUploadTrackedBuffer(meshState, device, usage, buffer, data, state);
      }, { slot: slot, dynamic: !!dynamic });
    }
    var existing = owner[slot];
    if (!existing || wgpuTrackedBufferSize(existing) < typedArray.byteLength) {
      if (existing && typeof existing.destroy === "function") {
        meshState.pointsEntryGPUBuffers.delete(existing);
        existing.destroy();
      }
      existing = wgpuCreateTrackedBuffer(meshState, device, usage, typedArray && typedArray.byteLength || 4);
      owner[slot] = existing;
      device.queue.writeBuffer(existing, 0, typedArray);
      owner[slot + "Source"] = typedArray;
      return existing;
    }
    if (dynamic || owner[slot + "Source"] !== typedArray) {
      device.queue.writeBuffer(existing, 0, typedArray);
      owner[slot + "Source"] = typedArray;
    }
    return existing;
  }

  function ensurePointsUniformGPUBuffer(meshState: any, device: any, owner, uniformData) {
    return wgpuCachedTrackedBuffer(meshState, device, 
      owner,
      "_gosxWGPUPointsUniformBuffer",
      uniformData,
      GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
      true
    );
  }

  function ensurePointsParticleVertexGPUBuffer(meshState: any, device: any, entry, particleData) {
    return wgpuCachedTrackedBuffer(meshState, device, 
      entry,
      "_gosxWGPUPointsParticleVertexBuffer",
      particleData,
      GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST,
      false
    );
  }

  function webGPURetireRetainedMeshAttribute(meshState: any, entry, key) {
    var record = entry && entry.attributes && entry.attributes[key];
    if (!record) return;
    if (record.buffer) {
      meshState.pointsEntryGPUBuffers.delete(record.buffer);
      try { record.buffer.destroy(); } catch (_err) {}
    }
    meshState.retainedMeshBufferStats.liveBytes = Math.max(0, meshState.retainedMeshBufferStats.liveBytes - record.byteLength);
    meshState.retainedMeshBufferStats.retirements += 1;
    delete entry.attributes[key];
  }

  function webGPURetireRetainedMeshEntry(meshState: any, vertices, entry) {
    if (!entry) return;
    var keys = Object.keys(entry.attributes || {});
    for (var i = 0; i < keys.length; i++) {
      webGPURetireRetainedMeshAttribute(meshState, entry, keys[i]);
    }
    meshState.retainedMeshAttributeCache.delete(vertices);
  }

  function webGPUBeginRetainedMeshFrame(meshState: any, bundle) {
    meshState.retainedMeshAttributeEpoch += 1;
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      // Keep retained and indexed direct-vertex resources alive for this frame.
      if (!obj || !obj.vertices) continue;
      if (!obj.retainedGeometry && !obj.directVertices) continue;
      var entry = meshState.retainedMeshAttributeCache.get(obj.vertices);
      if (entry) entry.lastSeenEpoch = meshState.retainedMeshAttributeEpoch;
    }
  }

  function webGPUSweepRetainedMeshBuffers(meshState: any) {
    for (const pair of Array.from(meshState.retainedMeshAttributeCache.entries())) {
      if (pair[1].lastSeenEpoch !== meshState.retainedMeshAttributeEpoch) {
        webGPURetireRetainedMeshEntry(meshState, pair[0], pair[1]);
      }
    }
  }

  function webGPURetainedMeshBufferStats(meshState: any) {
    return Object.assign({}, meshState.retainedMeshBufferStats, {
      cacheEntries: meshState.retainedMeshAttributeCache.size,
      epoch: meshState.retainedMeshAttributeEpoch,
    });
  }

  function webGPURetainedMeshFrameStats(meshState: any) {
    var stats = webGPURetainedMeshBufferStats(meshState);
    return {
      retainedCacheEntries: stats.cacheEntries,
      retainedCacheHits: stats.hits,
      retainedCacheMisses: stats.misses,
      retainedUploadCalls: stats.uploadCalls,
      retainedUploadBytes: stats.uploadBytes,
      retainedAllocations: stats.allocations,
      retainedRebuilds: stats.rebuilds,
      retainedRevisionInvalidations: stats.revisionInvalidations,
      retainedRetirements: stats.retirements,
      retainedLiveBytes: stats.liveBytes,
    };
  }

  function webGPUBindRetainedMeshAttribute(meshState: any, device: any, pass, slot, obj, key, components) {
    if (!obj || !obj.retainedGeometry || !obj.directVertices) return false;
    var count = webGPUObjectVertexCount(obj);
    var data = webGPUDirectAttribute(obj, key, count, components);
    if (!data) return false;
    var vertices = obj.vertices;
    var entry = meshState.retainedMeshAttributeCache.get(vertices);
    if (entry && entry.revision !== obj.geometryRevision) {
      meshState.retainedMeshBufferStats.rebuilds += 1;
      meshState.retainedMeshBufferStats.revisionInvalidations += 1;
      webGPURetireRetainedMeshEntry(meshState, vertices, entry);
      entry = null;
    }
    if (!entry) {
      entry = {
        revision: obj.geometryRevision,
        lastSeenEpoch: meshState.retainedMeshAttributeEpoch,
        attributes: Object.create(null),
      };
      meshState.retainedMeshAttributeCache.set(vertices, entry);
    }
    entry.lastSeenEpoch = meshState.retainedMeshAttributeEpoch;
    var record = entry.attributes[key];
    if (!record || record.data !== data || record.components !== components) {
      if (record) {
        meshState.retainedMeshBufferStats.rebuilds += 1;
        webGPURetireRetainedMeshAttribute(meshState, entry, key);
      }
      var buffer = wgpuCreateTrackedBuffer(meshState, device, 
        GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST,
        data.byteLength || 4
      );
      if (!buffer) return false;
      device.queue.writeBuffer(buffer, 0, data);
      record = {
        buffer: buffer,
        data: data,
        components: components,
        byteLength: data.byteLength,
      };
      entry.attributes[key] = record;
      meshState.retainedMeshBufferStats.misses += 1;
      meshState.retainedMeshBufferStats.allocations += 1;
      meshState.retainedMeshBufferStats.uploadCalls += 1;
      meshState.retainedMeshBufferStats.uploadBytes += data.byteLength;
      meshState.retainedMeshBufferStats.liveBytes += data.byteLength;
    } else {
      meshState.retainedMeshBufferStats.hits += 1;
    }
    pass.setVertexBuffer(slot, record.buffer, 0, Math.max(4, count * components * 4));
    return true;
  }

  // webGPUDirectMeshEntry returns the cache entry for a direct-vertex
  // mesh's geometry and marks it live this frame. Retained entries rebuild
  // on revision change; non-retained ones (skinned draws) are kept while
  // their typed arrays keep the same identity.
  function webGPUDirectMeshEntry(meshState: any, obj: any) {
    var vertices = obj.vertices;
    var retained = obj.retainedGeometry === true;
    var entry = meshState.retainedMeshAttributeCache.get(vertices);
    if (entry && retained && entry.revision !== obj.geometryRevision) {
      meshState.retainedMeshBufferStats.rebuilds += 1;
      meshState.retainedMeshBufferStats.revisionInvalidations += 1;
      webGPURetireRetainedMeshEntry(meshState, vertices, entry);
      entry = null;
    }
    if (!entry) {
      entry = {
        revision: retained ? obj.geometryRevision : undefined,
        lastSeenEpoch: meshState.retainedMeshAttributeEpoch,
        attributes: Object.create(null),
      };
      meshState.retainedMeshAttributeCache.set(vertices, entry);
    }
    entry.lastSeenEpoch = meshState.retainedMeshAttributeEpoch;
    return entry;
  }

  // webGPUBindSkinnedUVBuffer binds a skinned mesh's UVs. The compute pass
  // deforms positions, normals and tangents; UVs are plain geometry, so
  // they upload once per vertex stream, like the index buffer, and every
  // skinned object that draws that stream shares the buffer. The record
  // matches webGPUBindRetainedMeshAttribute's "uvs" record, so a retained
  // draw of the same stream reuses it too. A mesh without UVs binds one
  // zero-filled buffer per stream. Callers pass a skinned object, which
  // always has vertices; every "uvs" record holds two components.
  function webGPUBindSkinnedUVBuffer(meshState: any, device: any, pass: any, slot: number, obj: any, count: number) {
    if (!(count > 0)) return false;
    var entry = webGPUDirectMeshEntry(meshState, obj);
    var data = webGPUDirectAttribute(obj, "uvs", count, 2);
    var key = data ? "uvs" : "uvs:default";
    var record = entry.attributes[key];
    if (!data) data = record && record.data.length === count * 2 ? record.data : new Float32Array(count * 2);
    if (!record || record.data !== data) {
      if (record) {
        meshState.retainedMeshBufferStats.rebuilds += 1;
        webGPURetireRetainedMeshAttribute(meshState, entry, key);
      }
      var buffer = wgpuCreateTrackedBuffer(meshState, device, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, data.byteLength || 4);
      if (!buffer) return false;
      device.queue.writeBuffer(buffer, 0, data);
      record = { buffer: buffer, data: data, components: 2, byteLength: data.byteLength };
      entry.attributes[key] = record;
      meshState.retainedMeshBufferStats.misses += 1;
      meshState.retainedMeshBufferStats.allocations += 1;
      meshState.retainedMeshBufferStats.uploadCalls += 1;
      meshState.retainedMeshBufferStats.uploadBytes += data.byteLength;
      meshState.retainedMeshBufferStats.liveBytes += data.byteLength;
    } else {
      meshState.retainedMeshBufferStats.hits += 1;
    }
    pass.setVertexBuffer(slot, record.buffer, 0, Math.max(4, count * 2 * 4));
    return true;
  }

  // webGPUBindRetainedMeshIndexBuffer uploads (once) and binds the optional
  // authored index stream of a direct-vertex mesh as a uint32 index buffer,
  // returning its triangle-index count. Retained entries rebuild on revision
  // change and retire with their attribute buffers; non-retained direct
  // geometry (skinned draws) reuses while the Uint32Array identity is stable.
  // Returns 0 when there are no valid indices so callers keep draw().
  function webGPUBindRetainedMeshIndexBuffer(meshState: any, device: any, pass: any, obj: any) {
    if (!pass) return 0;
    var vertices = obj && obj.vertices;
    if (!vertices) return 0;
    var data = vertices.indices;
    if (!(data instanceof Uint32Array) || data.length < 3 || data.length % 3 !== 0) {
      return 0;
    }
    var entry = webGPUDirectMeshEntry(meshState, obj);
    var record = entry.attributes.indices;
    if (!record || record.data !== data) {
      if (record) {
        meshState.retainedMeshBufferStats.rebuilds += 1;
        webGPURetireRetainedMeshAttribute(meshState, entry, "indices");
      }
      var buffer = wgpuCreateTrackedBuffer(meshState, device, 
        GPUBufferUsage.INDEX | GPUBufferUsage.COPY_DST,
        data.byteLength || 4
      );
      if (!buffer) return 0;
      device.queue.writeBuffer(buffer, 0, data);
      record = { buffer: buffer, data: data, components: 1, byteLength: data.byteLength };
      entry.attributes.indices = record;
      meshState.retainedMeshBufferStats.misses += 1;
      meshState.retainedMeshBufferStats.allocations += 1;
      meshState.retainedMeshBufferStats.uploadCalls += 1;
      meshState.retainedMeshBufferStats.uploadBytes += data.byteLength;
      meshState.retainedMeshBufferStats.liveBytes += data.byteLength;
    } else {
      meshState.retainedMeshBufferStats.hits += 1;
    }
    pass.setIndexBuffer(record.buffer, "uint32");
    return data.length;
  } function webGPUSelenaIndexCount(meshState, device, pass: any, obj: any): number { return obj.directVertices || webGPUObjectIsSkinned(obj) ? webGPUBindRetainedMeshIndexBuffer(meshState, device, pass, obj) : 0; } // World-baked streams are already index-expanded

  function webGPUDefaultAttributeData(meshState: any, obj, key, count, tupleSize, defaults) {
    var direct = webGPUDirectAttribute(obj, key, count, tupleSize);
    if (direct) return direct;
    var stride = Math.max(1, tupleSize);
    var data = ensureScratch(meshState, key, Math.max(0, count * stride));
    for (var i = 0; i < count; i++) {
      for (var c = 0; c < stride; c++) {
        data[i * stride + c] = sceneNumber(defaults && defaults[c], 0);
      }
    }
    return data.subarray(0, count * stride);
  }

  // webGPUElioSkinVertexBuffer returns the GPU copy of a mesh's packed skin input. The bytes are packed from the source
  // attributes, uploaded once and dropped: the cache keeps only the source arrays, which rebuild them after a device loss.
  function webGPUElioSkinVertexBuffer(meshState: any, device: any, obj: any, count: number, paddedCount: number, jointCount: number) {
    var vertices = obj && obj.vertices;
    // Same source defaults the CPU draw path used before skinning moved to
    // the compute pass: normals fall back to +Z, tangents to (1,0,0,w=1).
    var key = [webGPUDirectAttribute(obj, "positions", count, 3), webGPUDirectAttribute(obj, "joints", count, 4),
      webGPUDirectAttribute(obj, "weights", count, 4), webGPUDefaultAttributeData(meshState, obj, "normals", count, 3, [0, 0, 1]),
      webGPUDefaultAttributeData(meshState, obj, "tangents", count, 4, [1, 0, 0, 1]), count, paddedCount, jointCount];
    if (!vertices || !key[0] || !key[1] || !key[2] || !key[3] || !key[4] || count <= 0 || paddedCount <= 0) return null;
    var cache = vertices._gosxWGPUElioSkinVertexData;
    var buffer = cache && meshState.pointsEntryGPUBuffers.has(cache.buffer) ? cache.buffer : null;
    if (buffer && wgpuSameItems(cache.key, key)) return buffer;
    var bytes = wgpuPackElioSkinVertices(key, count, paddedCount, jointCount);
    if (buffer && wgpuTrackedBufferSize(buffer) < bytes.byteLength) { meshState.pointsEntryGPUBuffers.delete(buffer); buffer.destroy(); buffer = null; }
    if (!buffer) buffer = wgpuCreateTrackedBuffer(meshState, device, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, bytes.byteLength);
    device.queue.writeBuffer(buffer, 0, bytes);
    vertices._gosxWGPUElioSkinVertexData = { key: key, buffer: buffer };
    return buffer;
  }

  function webGPUElioEnsureOutputBuffer(meshState: any, device: any, record, paddedCount) {
    var bytes = Math.max(4, paddedCount * 10 * 4);
    // Cross-renderer staleness guard: scene objects retain their skin
    // records across renderer rebuilds, but dispose() destroys every buffer
    // tracked in pointsEntryGPUBuffers. A cached outputBuffer absent from
    // THIS renderer's set belongs to a dead device — drop the stale JS
    // reference WITHOUT calling destroy() again (dispose already destroyed
    // it), so the alloc path below creates a fresh buffer on the current
    // device. Mirrors the owner[slot] guard in sceneCachedTrackedBuffer.
    // The bind group is invalidated too: it was created on the dead device
    // and references the destroyed buffer, so no cache path below may
    // return with a live-looking bindGroup around the dead buffer.
    if (record.outputBuffer && !meshState.pointsEntryGPUBuffers.has(record.outputBuffer)) {
      record.outputBuffer = null;
      record.bindGroup = null;
    }
    if (record.outputBuffer && wgpuTrackedBufferSize(record.outputBuffer) >= bytes) return record.outputBuffer;
    if (record.outputBuffer && typeof record.outputBuffer.destroy === "function") {
      meshState.pointsEntryGPUBuffers.delete(record.outputBuffer);
      record.outputBuffer.destroy();
    }
    record.outputBuffer = wgpuCreateTrackedBuffer(meshState, device, GPUBufferUsage.STORAGE | GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST | GPUBufferUsage.COPY_SRC, bytes);
    return record.outputBuffer;
  }

  // webGPUObjectSkinOutputBuffer returns the skin output buffer the owner
  // holds on this device, before or after this frame's dispatch.
  function webGPUObjectSkinOutputBuffer(meshState: any, obj: any) {
    var owner = webGPUObjectResourceOwner(obj);
    var record = owner && owner._gosxWGPUElioSkinRecord;
    return record && record.outputBuffer && meshState.pointsEntryGPUBuffers.has(record.outputBuffer) ? record.outputBuffer : null;
  }

  function webGPUElioSkinRecord(meshState: any, device: any, obj) {
    if (!webGPUObjectIsSkinned(obj)) return null;
    meshState.ensureElioSkinPipeline();
    var count = webGPUObjectVertexCount(obj);
    var skin = obj.skin;
    var jointCount = Math.floor(sceneNumber(skin && skin.jointMatrices && skin.jointMatrices.length, 0) / 16);
    if (count <= 0 || jointCount <= 0) return null;
    var paddedCount = Math.max(64, Math.ceil(count / 64) * 64);
    var vertexBuffer = webGPUElioSkinVertexBuffer(meshState, device, obj, count, paddedCount, jointCount);
    var boneData = webGPUElioBoneData(obj, jointCount);
    if (!vertexBuffer || !boneData) return null;

    // The output buffer and bind group belong to the scene object, not to
    // this frame's draw record: one of each per skinned object.
    var owner = webGPUObjectResourceOwner(obj);
    var record = owner._gosxWGPUElioSkinRecord;
    if (!record) {
      record = {};
      owner._gosxWGPUElioSkinRecord = record;
    }

    var boneBuffer = wgpuCachedTrackedBuffer(meshState, device, 
      skin,
      "_gosxWGPUElioSkinBoneBuffer",
      boneData,
      GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
      true
    );
    var outputBuffer = webGPUElioEnsureOutputBuffer(meshState, device, record, paddedCount);
    // paddedCount places the normal/tangent regions inside the single
    // output buffer; the kernel derives it from arrayLength(&verts).
    if (!boneBuffer || !vertexBuffer || !outputBuffer) return null;

    if (
      !record.bindGroup ||
      record.boneBuffer !== boneBuffer ||
      record.vertexBuffer !== vertexBuffer ||
      record.paddedCount !== paddedCount ||
      record.outputBuffer !== outputBuffer
    ) {
      record.bindGroup = device.createBindGroup({
        layout: meshState.elioSkinBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: boneBuffer } },
          { binding: 1, resource: { buffer: vertexBuffer } },
          { binding: 2, resource: { buffer: outputBuffer } },
        ],
      });
      record.boneBuffer = boneBuffer;
      record.vertexBuffer = vertexBuffer;
      record.outputBuffer = outputBuffer;
    }
    record.count = count;
    record.paddedCount = paddedCount;
    record.workgroups = Math.ceil(paddedCount / 64);
    obj._gosxWGPUElioSkinOutputBuffer = outputBuffer;
    obj._gosxWGPUElioSkinOutputPaddedCount = paddedCount;
    return record;
  }

  function updateElioSkinnedMeshes(meshState: any, device: any, bundle, encoder) {
    var stats = {
      elioSkinningDispatches: 0,
      elioSkinningVertices: 0,
      elioSkinningOutputBytes: 0,
      elioSkinningKernel: "m31labs.dev/elio/stdlib.Skin",
    };
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    var pass = null;
    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled || !webGPUObjectIsSkinned(obj)) continue;
      var record = webGPUElioSkinRecord(meshState, device, obj);
      if (!record) continue;
      if (!pass) {
        pass = encoder.beginComputePass({ label: "gosx-elio-skin-lbs" });
        pass.setPipeline(wgpuRequirePipeline(meshState.elioSkinPipeline));
      }
      pass.setBindGroup(0, record.bindGroup);
      pass.dispatchWorkgroups(record.workgroups);
      stats.elioSkinningDispatches += 1;
      stats.elioSkinningVertices += record.count;
      // Position, normal and tangent: 40 bytes a vertex, padded to the workgroup.
      stats.elioSkinningOutputBytes += record.paddedCount * 40;
    }
    if (pass) pass.end();
    return stats;
  }

  function webGPUComputedMorphData(meshState: any, obj) {
    var morph = obj && obj.computedMorph;
    if (!morph || !meshState.computedMorphBindGroupLayout) return null;
    var requested = Math.max(0, Math.floor(sceneNumber(morph.count, sceneNumber(obj && obj.vertexCount, 0))));
    var objCount = Math.max(0, Math.floor(sceneNumber(obj && obj.vertexCount, requested)));
    var count = Math.min(requested, objCount);
    var sourcePositions = webGPUComputedMorphArray(morph, "sourcePositions", count, 3);
    var targetPositions = webGPUComputedMorphArray(morph, "targetPositions", count, 3);
    if (!sourcePositions || !targetPositions || count <= 0) return null;
    var sourceNormals = webGPUComputedMorphArray(morph, "sourceNormals", count, 3) ||
      webGPUComputedMorphDefaultArray(morph, "_defaultSourceNormals", count, 3, [0, 0, 1]);
    var targetNormals = webGPUComputedMorphArray(morph, "targetNormals", count, 3) || sourceNormals;
    var sourceTangents = webGPUComputedMorphArray(morph, "sourceTangents", count, 4) ||
      webGPUComputedMorphDefaultArray(morph, "_defaultSourceTangents", count, 4, [1, 0, 0, 1]);
    var targetTangents = webGPUComputedMorphArray(morph, "targetTangents", count, 4) || sourceTangents;
    if (!sourceNormals || !targetNormals || !sourceTangents || !targetTangents) return null;
    return {
      morph: morph,
      count: count,
      sourcePositions: sourcePositions,
      targetPositions: targetPositions,
      sourceNormals: sourceNormals,
      targetNormals: targetNormals,
      sourceTangents: sourceTangents,
      targetTangents: targetTangents,
    };
  }

  function webGPUComputedMorphEnsureOutputBuffer(meshState: any, device: any, record, slot, count, components) {
    var bytes = Math.max(4, Math.max(0, Math.floor(sceneNumber(count, 0))) * Math.max(1, components) * 4);
    var buffer = record && record[slot];
    // Cross-renderer staleness guard: scene objects retain their morph
    // records across renderer rebuilds, but dispose() destroys every buffer
    // tracked in pointsEntryGPUBuffers. A cached output buffer absent from
    // THIS renderer's set belongs to a dead device — drop the stale JS
    // reference WITHOUT calling destroy() again (dispose already destroyed
    // it), so the alloc path below creates a fresh buffer on the current
    // device. The bind group is invalidated too: it was created on the dead
    // device and references the destroyed output buffers, so no cache path
    // may return with a live-looking bindGroup around a dead buffer.
    // Mirrors the guard in webGPUElioEnsureOutputBuffer.
    if (buffer && !meshState.pointsEntryGPUBuffers.has(buffer)) {
      record[slot] = null;
      record.bindGroup = null;
      buffer = null;
    }
    if (buffer && wgpuTrackedBufferSize(buffer) >= bytes) return buffer;
    if (buffer && typeof buffer.destroy === "function") {
      meshState.pointsEntryGPUBuffers.delete(buffer);
      // The live-but-undersized buffer is about to be destroyed and
      // replaced. Invalidate the cached bind group BEFORE destruction so it
      // cannot retain a reference to this soon-to-be-destroyed output
      // buffer; a stale bindGroup around a destroyed buffer must never be
      // returned by the cache path.
      record.bindGroup = null;
      buffer.destroy();
    }
    buffer = wgpuCreateTrackedBuffer(meshState, device, GPUBufferUsage.STORAGE | GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, bytes);
    record[slot] = buffer;
    return buffer;
  }

  function webGPUComputedMorphRecord(meshState: any, device: any, obj) {
    var data = webGPUComputedMorphData(meshState, obj);
    if (!data) return null;
    meshState.ensureComputedMorphPipeline();
    var morph = data.morph;
    var record = morph._gosxWGPUComputedMorphRecord;
    if (!record) {
      record = {};
      morph._gosxWGPUComputedMorphRecord = record;
    }
    var count = data.count;
    var sourcePacked = webGPUComputedMorphPackedData(morph, "_gosxWGPUComputedMorphSourcePackedData", count, data.sourcePositions, data.sourceNormals, data.sourceTangents);
    var targetPacked = webGPUComputedMorphPackedData(morph, "_gosxWGPUComputedMorphTargetPackedData", count, data.targetPositions, data.targetNormals, data.targetTangents);
    var sourcePackedBuffer = wgpuCachedTrackedBuffer(meshState, device, morph, "_gosxWGPUComputedMorphSourcePacked", sourcePacked, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, false);
    var targetPackedBuffer = wgpuCachedTrackedBuffer(meshState, device, morph, "_gosxWGPUComputedMorphTargetPacked", targetPacked, GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST, false);
    var uniformData = webGPUComputedMorphUniformData(obj, morph, count);
    var uniformBuffer = wgpuCachedTrackedBuffer(meshState, device, morph, "_gosxWGPUComputedMorphUniform", uniformData, GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, true);
    var positionBuffer = webGPUComputedMorphEnsureOutputBuffer(meshState, device, record, "positionBuffer", count, 3);
    var normalBuffer = webGPUComputedMorphEnsureOutputBuffer(meshState, device, record, "normalBuffer", count, 3);
    var tangentBuffer = webGPUComputedMorphEnsureOutputBuffer(meshState, device, record, "tangentBuffer", count, 4);
    if (
      !sourcePackedBuffer || !targetPackedBuffer ||
      !uniformBuffer || !positionBuffer || !normalBuffer || !tangentBuffer
    ) {
      return null;
    }
    if (
      !record.bindGroup ||
      record.sourcePackedBuffer !== sourcePackedBuffer ||
      record.targetPackedBuffer !== targetPackedBuffer ||
      record.positionBuffer !== positionBuffer ||
      record.normalBuffer !== normalBuffer ||
      record.tangentBuffer !== tangentBuffer ||
      record.uniformBuffer !== uniformBuffer
    ) {
      record.bindGroup = device.createBindGroup({
        layout: meshState.computedMorphBindGroupLayout,
        entries: [
          { binding: 0, resource: { buffer: sourcePackedBuffer } },
          { binding: 1, resource: { buffer: targetPackedBuffer } },
          { binding: 2, resource: { buffer: positionBuffer } },
          { binding: 3, resource: { buffer: normalBuffer } },
          { binding: 4, resource: { buffer: tangentBuffer } },
          { binding: 5, resource: { buffer: uniformBuffer } },
        ],
      });
      record.sourcePackedBuffer = sourcePackedBuffer;
      record.targetPackedBuffer = targetPackedBuffer;
      record.positionBuffer = positionBuffer;
      record.normalBuffer = normalBuffer;
      record.tangentBuffer = tangentBuffer;
      record.uniformBuffer = uniformBuffer;
    }
    record.count = count;
    record.workgroups = Math.ceil(Math.max(64, count) / 64);
    obj._gosxWGPUComputedMorphRecord = record;
    return record;
  }

  function updateComputedMorphMeshes(meshState: any, device: any, bundle, encoder) {
    var stats = {
      computedMorphDispatches: 0,
      computedMorphVertices: 0,
      computedMorphKernel: "m31labs.dev/gosx/scene3d.ComputedMorph",
    };
    var objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
    var pass = null;
    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled || webGPUObjectIsSkinned(obj)) continue;
      var record = webGPUComputedMorphRecord(meshState, device, obj);
      if (!record) continue;
      if (!pass) {
        pass = encoder.beginComputePass({ label: "gosx-computed-morph" });
        pass.setPipeline(wgpuRequirePipeline(meshState.computedMorphPipeline));
      }
      pass.setBindGroup(0, record.bindGroup);
      pass.dispatchWorkgroups(record.workgroups);
      stats.computedMorphDispatches += 1;
      stats.computedMorphVertices += record.count;
    }
    if (pass) pass.end();
    return stats;
  }

  // Selena chooses the vertex slots. Elio packs three deformed streams into
  // one buffer, while UVs and user attributes stay on the original geometry.
  function webGPUBindElioSelenaAttribute(meshState: any, device: any, pass, obj, attr, count) {
    const buffer = obj._gosxWGPUElioSkinOutputBuffer;
    const padded = obj._gosxWGPUElioSkinOutputPaddedCount;
    if (!buffer || !(padded >= count)) return false;
    if (attr.source === "custom") return webGPUBindRetainedMeshAttribute(meshState, device, pass, attr.slot, obj, "custom:" + attr.name, attr.components);
    if (attr.source === "uvs" && attr.components === 2) return webGPUBindSkinnedUVBuffer(meshState, device, pass, attr.slot, obj, count);
    let offset;
    if (attr.source === "positions" && attr.components === 3) offset = 0;
    else if (attr.source === "normals" && attr.components === 3) offset = padded * 12;
    else if (attr.source === "tangents" && attr.components === 4) offset = padded * 24;
    else return false;
    pass.setVertexBuffer(attr.slot, buffer, offset, count * attr.components * 4);
    return true;
  }

  function webGPUBindElioSkinnedBuffers(meshState: any, device: any, pass, obj, count) {
    var outputBuffer = obj && obj._gosxWGPUElioSkinOutputBuffer;
    if (!outputBuffer) return false;
    // One tracked buffer holds three contiguous paddedCount-sized regions:
    // positions at byte 0, normals at paddedCount*12, tangents at
    // paddedCount*24. Bind each region at its exact offset and the logical
    // (unpadded) draw size; slot 2 stays on its own UV buffer.
    var paddedCount = sceneNumber(obj && obj._gosxWGPUElioSkinOutputPaddedCount, 0);
    if (!(paddedCount > 0)) return false;
    var vec3Bytes = Math.max(4, count * 3 * 4);
    pass.setVertexBuffer(0, outputBuffer, 0, vec3Bytes);
    pass.setVertexBuffer(1, outputBuffer, paddedCount * 12, vec3Bytes);
    if (!webGPUBindSkinnedUVBuffer(meshState, device, pass, 2, obj, count)) return false;
    pass.setVertexBuffer(3, outputBuffer, paddedCount * 24, Math.max(4, count * 4 * 4));
    return true;
  }

  function ensurePBRSceneAttributeBuffers(meshState: any, device: any, bundle) {
    if (!bundle) return null;
    var vertexCount = webGPUSceneMeshVertexCount(bundle);
    if (vertexCount <= 0) return null;
    var positions = webGPUSceneMeshAttributeData(bundle, "worldMeshPositions", 3, [0, 0, 0], vertexCount);
    var normals = webGPUSceneMeshAttributeData(bundle, "worldMeshNormals", 3, [0, 0, 1], vertexCount);
    var uvs = webGPUSceneMeshAttributeData(bundle, "worldMeshUVs", 2, [0, 0], vertexCount);
    var tangents = webGPUSceneMeshAttributeData(bundle, "worldMeshTangents", 4, [1, 0, 0, 1], vertexCount);
    // wgpuStablePBRAttributeBuffer (not wgpuCachedTrackedBuffer(bundle, ...))
    // -- `bundle` is a brand-new object every render() call (see
    // pbrSceneAttributeCache's declaration above), so caching on it can
    // never skip a re-upload; content-compare against the renderer-scoped
    // cache instead, so a scene whose mesh geometry hasn't actually changed
    // (the common case -- see the water demo's static float-* objects)
    // skips the createBuffer + queue.writeBuffer pair entirely.
    return {
      positions: { buffer: wgpuStablePBRAttributeBuffer(meshState, device, "positions", positions), components: 3 },
      normals: { buffer: wgpuStablePBRAttributeBuffer(meshState, device, "normals", normals), components: 3 },
      uvs: { buffer: wgpuStablePBRAttributeBuffer(meshState, device, "uvs", uvs), components: 2 },
      tangents: { buffer: wgpuStablePBRAttributeBuffer(meshState, device, "tangents", tangents), components: 4 },
      vertexCount: vertexCount,
    };
  }
  function uploadFrameUniforms(uniformState: any, gpuRes: any, device: any, camera: any, width: any, height: any, toneMapMode: any) {
    var cam;
    var camPosZ;
    if (camera && camera.mode === "ortho2d") {
      // 2D board path — mirrors the Mode branch at the top of the native
      // computeMVP (render/bundle/math.go). The RAW RenderCamera wire
      // fields are read directly: x/y carry the pan, z carries the zoom,
      // near/far are -1/1. This MUST run before sceneRenderCamera — the
      // normalizer strips `mode`, applies 3D defaults (z→6, near→0.05,
      // far→128), and treats z as a position, which would silently
      // mangle the 2D camera.
      sceneMat4Ortho2DView(camera, uniformState.scratchViewMatrix);
      sceneMat4Ortho2DProj(camera, width, height, uniformState.scratchProjMatrix);
      // Returned cam: render()'s only downstream consumer of the return
      // value is drawPointsEntries, which never reads its cam parameter
      // (and 2D bundles carry no points — Configure2DBundle strips
      // lights/env/postFX and the board adapter emits only
      // meshObjects/materials/background). z is 0 because camera.z
      // carries the zoom, not a position — the cameraPos uniform below
      // must not inherit it; with no lights in 2D bundles cameraPos is
      // inert anyway.
      cam = {
        mode: "ortho2d",
        x: sceneNumber(camera.x, 0),
        y: sceneNumber(camera.y, 0),
        z: 0,
      };
      camPosZ = 0;
    } else {
      cam = sceneRenderCamera(camera);
      var aspect = Math.max(0.0001, width / Math.max(1, height));
      scenePBRViewMatrix(cam, uniformState.scratchViewMatrix);
      if (typeof scenePBRProjectionMatrixForCamera === "function") {
        scenePBRProjectionMatrixForCamera(cam, aspect, uniformState.scratchProjMatrix);
      } else {
        scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, uniformState.scratchProjMatrix);
      }
      camPosZ = cam.z; // cameraPos.z is the world-space eye position.
    }

    // scenePBRProjectionMatrix and sceneMat4Ortho2DProj produce a
    // WebGL-convention matrix whose clip-z range is [-w, w]. WebGPU's
    // clip volume keeps z in [0, w], so without this remap every
    // primitive in the front half of the frustum is silently clipped.
    // Pre-multiplying by the depth-remap matrix R (row 2 = 0.5 *
    // (row 2 + row 3)) converts to WebGPU clip space. Affects every
    // WebGPU pipeline that consumes frame.projMatrix (PBR meshes, world
    // lines, surfaces, points, compute particles). For the ortho-2D
    // board (near=-1, far=1) the board plane z=0 lands at clip z=0.5.
    uniformState.scratchProjMatrix[2]  = 0.5 * (uniformState.scratchProjMatrix[2]  + uniformState.scratchProjMatrix[3]);
    uniformState.scratchProjMatrix[6]  = 0.5 * (uniformState.scratchProjMatrix[6]  + uniformState.scratchProjMatrix[7]);
    uniformState.scratchProjMatrix[10] = 0.5 * (uniformState.scratchProjMatrix[10] + uniformState.scratchProjMatrix[11]);
    uniformState.scratchProjMatrix[14] = 0.5 * (uniformState.scratchProjMatrix[14] + uniformState.scratchProjMatrix[15]);
    sceneMat4MultiplyInto(uniformState.scratchSelenaViewProjection, uniformState.scratchProjMatrix, uniformState.scratchViewMatrix);

    // FrameUniforms layout (std140):
    // mat4x4f viewMatrix:  0  (64 bytes)
    // mat4x4f projMatrix:  64 (64 bytes)
    // vec3f cameraPos:     128 (12 bytes)
    // u32 lightCount:      140 (4 bytes)
    // f32 viewportWidth:   144 (4 bytes)
    // f32 viewportHeight:  148 (4 bytes)
    // u32 toneMap:         152 (4 bytes)
    // u32 _pad:            156 (4 bytes)
    var f = uniformState._frameUniformF;
    var u = uniformState._frameUniformU;
    f.set(uniformState.scratchViewMatrix, 0);          // offset 0
    f.set(uniformState.scratchProjMatrix, 16);         // offset 64
    f[32] = cam.x;                         // cameraPos.x
    f[33] = cam.y;                         // cameraPos.y
    f[34] = camPosZ;                       // cameraPos.z (3D: world eye; ortho2d: 0 — z carries zoom)
    // lightCount set below in uploadLights
    f[36] = width;                          // viewportWidth
    f[37] = height;                         // viewportHeight
    u[38] = Math.max(0, Math.min(4, Math.floor(sceneNumber(toneMapMode, 1)))); // toneMap/output mode
    u[39] = 0;                              // pad

    device.queue.writeBuffer(gpuRes.frameUniformBuffer, 0, f);
    return cam;
  }

  function uploadWaterReflectionFrameUniforms(uniformState: any, gpuRes: any, device: any, camera: any, width: any, height: any, toneMap: any) {
    if (camera && camera.mode === "ortho2d") {
      return uploadFrameUniforms(uniformState, gpuRes, device, camera, width, height, toneMap);
    }
    var cam = sceneRenderCamera(camera);
    var aspect = Math.max(0.0001, width / Math.max(1, height));
    var position = sceneWaterCameraWorldPosition(cam);
    var direction = sceneWaterCameraWorldDirection(cam);
    var target = {
      x: position.x + direction.x,
      y: position.y + direction.y,
      z: position.z + direction.z,
    };
    var eye = sceneWaterMirrorWaterPoint(position);
    var reflectedTarget = sceneWaterMirrorWaterPoint(target);
    var reflectedUp = sceneWaterReflectionCameraUp(camera);
    sceneWaterLookAtViewMatrix(eye, reflectedTarget, reflectedUp, uniformState.scratchViewMatrix);
    if (typeof scenePBRProjectionMatrixForCamera === "function") {
      scenePBRProjectionMatrixForCamera(cam, aspect, uniformState.scratchProjMatrix);
    } else {
      scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, uniformState.scratchProjMatrix);
    }

    uniformState.scratchProjMatrix[2]  = 0.5 * (uniformState.scratchProjMatrix[2]  + uniformState.scratchProjMatrix[3]);
    uniformState.scratchProjMatrix[6]  = 0.5 * (uniformState.scratchProjMatrix[6]  + uniformState.scratchProjMatrix[7]);
    uniformState.scratchProjMatrix[10] = 0.5 * (uniformState.scratchProjMatrix[10] + uniformState.scratchProjMatrix[11]);
    uniformState.scratchProjMatrix[14] = 0.5 * (uniformState.scratchProjMatrix[14] + uniformState.scratchProjMatrix[15]);
    sceneMat4MultiplyInto(uniformState.scratchSelenaViewProjection, uniformState.scratchProjMatrix, uniformState.scratchViewMatrix);

    var f = uniformState._frameUniformF;
    var u = uniformState._frameUniformU;
    f.set(uniformState.scratchViewMatrix, 0);
    f.set(uniformState.scratchProjMatrix, 16);
    f[32] = eye.x;
    f[33] = eye.y;
    f[34] = eye.z;
    f[36] = width;
    f[37] = height;
    u[38] = toneMap ? 1 : 0;
    u[39] = 0;
    device.queue.writeBuffer(gpuRes.frameUniformBuffer, 0, f);
    return {
      kind: cam.kind,
      x: eye.x,
      y: eye.y,
      z: eye.z,
      fov: cam.fov,
      near: cam.near,
      far: cam.far,
    };
  }

  // ensureLightCapacity grows the light storage buffer to hold capacity
  // lights. The frame bind group is rebuilt every frame after uploadLights,
  // so the new buffer reaches the shader on the same frame it appears.
  function ensureLightCapacity(uniformState: any, gpuRes: any, device: any, capacity: any) {
    if (capacity <= uniformState._lightCapacity && gpuRes.lightStorageBuffer) {
      return;
    }
    if (gpuRes.lightStorageBuffer) {
      uniformState._retiredLightBuffers.push(gpuRes.lightStorageBuffer);
    }
    uniformState._lightCapacity = capacity;
    uniformState._lightDataF = new Float32Array(capacity * SCENE_WEBGPU_LIGHT_FLOATS);
    gpuRes.lightStorageBuffer = device.createBuffer({
      label: "gosx-lights",
      size: capacity * SCENE_WEBGPU_LIGHT_BYTES,
      usage: GPUBufferUsage.STORAGE | GPUBufferUsage.COPY_DST,
    });
  }

  // reportLightIssuesOnce forwards each new diagnostic exactly once.
  function reportLightIssuesOnce(uniformState: any, issues: any) {
    for (var i = 0; i < issues.length; i++) {
      var issue = issues[i];
      var key = issue.code + "|" + issue.message;
      if (uniformState._lightIssuesReported[key]) {
        continue;
      }
      uniformState._lightIssuesReported[key] = true;
      sceneWebGPUReportLightingIssue({
        scope: "scene3d",
        type: "lighting",
        source: "webgpu",
        code: issue.code,
        message: issue.message,
      });
    }
  }

  function uploadLights(uniformState: any, gpuRes: any, device: any, lights: any, physical = false) {
    var lightArray = Array.isArray(lights) ? lights : [];
    ensureLightCapacity(uniformState, gpuRes, device, sceneWebGPULightCapacityFor(lightArray.length));
    var count = Math.min(lightArray.length, uniformState._lightCapacity);

    // Write lightCount into frame uniform buffer at byte offset 140.
    uniformState._lightCountBuf[0] = count;
    device.queue.writeBuffer(gpuRes.frameUniformBuffer, 140, uniformState._lightCountBuf);

    var census = sceneWebGPUPackLights(lightArray, count, uniformState._lightDataF, uniformState._lightColorCache, physical);
    reportLightIssuesOnce(uniformState, sceneWebGPULightIssues(census, lightArray.length, uniformState._lightCapacity));

    if (count > 0) {
      // Write only the used prefix. The shader stops at lightCount, so stale
      // floats past that point never reach a fragment.
      device.queue.writeBuffer(
        gpuRes.lightStorageBuffer,
        0,
        uniformState._lightDataF,
        0,
        count * SCENE_WEBGPU_LIGHT_FLOATS,
      );
    }
    return census;
  }

  function uploadFogUniforms(uniformState: any, gpuRes: any, device: any, environment: any) {
    var env = environment || {};
    var fogDensity = sceneNumber(env.fogDensity, 0);
    var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]); var physical = wgpuPhysicalLights(env);

    // FogUniforms: vec3f fogColor(12) + f32 density(4) + u32 hasFog(4) + pad(12) = 32 bytes.
    var f = uniformState._fogUniformF;
    var u = uniformState._fogUniformU;
    f[0] = wgpuLightChannel(fogColorRGBA[0], physical);
    f[1] = wgpuLightChannel(fogColorRGBA[1], physical);
    f[2] = wgpuLightChannel(fogColorRGBA[2], physical);
    f[3] = fogDensity;
    u[4] = fogDensity > 0 ? 1 : 0;
    u[5] = 0;
    u[6] = 0;
    u[7] = 0;
    device.queue.writeBuffer(gpuRes.fogUniformBuffer, 0, f);
  }

  function syncEnvironmentIBL(uniformState: any, gpuRes: any, device: any, environment: any) {
    var env = environment || {};
    var ibl = env.ibl && typeof env.ibl === "object" ? env.ibl : null;
    if (!ibl) {
      uniformState.iblResources.active = false;
      uniformState.iblResources.diagnostics = { requested: false, active: false, state: "not-requested", reason: "", radianceMipLevels: 0 };
      return uniformState.iblResources;
    }
    var radiance = webGPUIBLProduct(ibl, "radiance", "environment-radiance", "cube", "rgba16f");
    var irradiance = webGPUIBLProduct(ibl, "irradiance", "environment-irradiance", "cube", "rgba16f");
    var brdf = webGPUIBLProduct(ibl, "brdfLUT", "brdf-lut", "2d", "rg16f");
    var model = typeof ibl.brdfModel === "string" ? ibl.brdfModel.trim() : "";
    var diag = { requested: true, active: false, state: "validating", reason: "", radianceMipLevels: 0 };
    if (!radiance || !irradiance || !brdf) {
      diag.state = "unsupported";
      diag.reason = "descriptor-role-color-view-format";
    } else if (model !== SCENE_WEBGPU_IBL_BRDF_MODEL) {
      diag.state = "unsupported";
      diag.reason = "brdf-model:" + (model || "missing");
    } else if (!webGPUIBLRoughnessMappingValid(ibl, radiance.mipLevels)) {
      diag.state = "unsupported";
      diag.reason = "roughness-mip-mapping";
    } else if (!wgpuKTX2API()) {
      diag.state = "unsupported";
      diag.reason = "ktx2-loader-unavailable";
    } else {
      var key = [radiance.uri, irradiance.uri, brdf.uri, model].join("\u0000");
      if (uniformState.iblResources.key !== key) {
        /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ uniformState.iblResources.key = key;
        /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ uniformState.iblResources.radiance = wgpuLoadTexture(device, radiance.uri, gpuRes.textureCache, radiance);
        /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ uniformState.iblResources.irradiance = wgpuLoadTexture(device, irradiance.uri, gpuRes.textureCache, irradiance);
        uniformState.iblResources.brdfLUT = wgpuLoadTexture(device, brdf.uri, gpuRes.textureCache, brdf);
      }
      var failed = [uniformState.iblResources.radiance, uniformState.iblResources.irradiance, uniformState.iblResources.brdfLUT].some(function(record) {
        return record && record.failed;
      });
      var active = webGPUIBLRecordValid(uniformState.iblResources.radiance, radiance, model) &&
        webGPUIBLRecordValid(uniformState.iblResources.irradiance, irradiance, "lambert-sh9") &&
        webGPUIBLRecordValid(uniformState.iblResources.brdfLUT, brdf, model);
      if (active) {
        diag.active = true;
        diag.state = "active";
        diag.radianceMipLevels = Math.max(1, uniformState.iblResources.radiance.levels || radiance.mipLevels || 1);
      } else {
        var allLoaded = [uniformState.iblResources.radiance, uniformState.iblResources.irradiance, uniformState.iblResources.brdfLUT].every(function(record) {
          return record && record.loaded;
        });
        diag.state = failed || allLoaded ? "failed" : "loading";
        diag.reason = failed ? "product-upload" : (allLoaded ? "product-container-metadata" : "product-pending");
      }
    }
    uniformState.iblResources.active = diag.active;
    uniformState.iblResources.diagnostics = diag;
    if ((diag.state === "unsupported" || diag.state === "failed") && uniformState.iblResources.lastWarning !== diag.reason) {
      uniformState.iblResources.lastWarning = diag.reason;
      try { console.warn("[gosx] WebGPU IBL " + diag.state + ": " + diag.reason); } catch (_error) {}
      renderTruth().record("ibl-" + diag.state, diag.reason);
    }
    return uniformState.iblResources;
  }

  // syncEnvironmentMap loads the legacy equirectangular Environment.EnvironmentMap
  // image through the shared 2D texture cache/loader (wgpuLoadTexture), the
  // same path material albedo maps use. IBL wins: iblActive (ibl.active from
  // syncEnvironmentIBL, not merely "requested") suppresses this, matching
  // 16-scene-webgl.js's scenePBRUploadEnvironmentMap status machine, which
  // only zeroes hasEnvMap once iblStatus.active is true.
  function syncEnvironmentMap(uniformState: any, gpuRes: any, device: any, environment: any, iblActive: any) {
    var env = environment || {};
    var url = typeof env.envMap === "string" ? env.envMap.trim() : "";
    if (!url || iblActive) {
      uniformState.envMapResources.active = false;
      return uniformState.envMapResources;
    }
    if (uniformState.envMapResources.key !== url) {
      uniformState.envMapResources.key = url;
      // Legacy equirect PNG/JPEG is sRGB, as in WebGL2; uploading it as
      // linear skipped decoding and made samples about 2.2x too bright.
      uniformState.envMapResources.record = wgpuLoadTexture(device, url, gpuRes.textureCache, null, "environment-radiance", "srgb");
    }
    var record = uniformState.envMapResources.record;
    uniformState.envMapResources.active = Boolean(record && record.loaded && !record.failed);
    return uniformState.envMapResources;
  }

  function uploadEnvUniforms(uniformState: any, gpuRes: any, device: any, selenaFrame: any, environment: any) {
    var env = environment || {};
    var ibl = syncEnvironmentIBL(uniformState, gpuRes, device, env);
    var envMap = syncEnvironmentMap(uniformState, gpuRes, device, env, ibl.active);
    selenaFrame.environmentInfo = sceneWebGPUSelenaEnvironmentInfo(ibl, env);
    var ambientColorRGBA = sceneColorRGBA(env.ambientColor, [1, 1, 1, 1]);
    var skyColorRGBA = sceneColorRGBA(env.skyColor, [0.88, 0.94, 1, 1]);
    var groundColorRGBA = sceneColorRGBA(env.groundColor, [0.12, 0.16, 0.22, 1]); var physical = wgpuPhysicalLights(env);
    // 80-byte EnvUniforms, matching WGSL.
    var data = uniformState._envUniformF;
    var words = uniformState._envUniformU;
    data[0] = wgpuLightChannel(ambientColorRGBA[0], physical); data[1] = wgpuLightChannel(ambientColorRGBA[1], physical); data[2] = wgpuLightChannel(ambientColorRGBA[2], physical);
    data[3] = sceneNumber(env.ambientIntensity, 0);
    data[4] = wgpuLightChannel(skyColorRGBA[0], physical); data[5] = wgpuLightChannel(skyColorRGBA[1], physical); data[6] = wgpuLightChannel(skyColorRGBA[2], physical);
    data[7] = sceneNumber(env.skyIntensity, 0);
    data[8] = wgpuLightChannel(groundColorRGBA[0], physical); data[9] = wgpuLightChannel(groundColorRGBA[1], physical); data[10] = wgpuLightChannel(groundColorRGBA[2], physical);
    data[11] = sceneNumber(env.groundIntensity, 0);
    data[12] = Math.max(0, sceneNumber(env.envIntensity, 1));
    data[13] = sceneNumber(env.envRotation, 0);
    words[14] = ibl.active ? 1 : 0;
    words[15] = ibl.active ? Math.max(1, ibl.diagnostics.radianceMipLevels | 0) : 0;
    words[16] = envMap.active ? 1 : 0;
    data[17] = wgpuEnvMapMaxLod(envMap);
    words[18] = physical ? 1 : 0; words[19] = 0;
    device.queue.writeBuffer(gpuRes.envUniformBuffer, 0, data);
  }
  // uploadShadowUniforms packs every slot's cascades (webgpu-shadows module).
  // shadowLightMatrices is kept for callers that read the one-map matrices.
  function uploadShadowUniforms(uniformState: any, gpuRes: any, device: any, shadowSlots: any, shadowLightMatrices: any, shadowLightIndices: any, lights: any) {
    var span = wgpuPackShadowUniforms(uniformState._shadowUniformF, uniformState._shadowUniformI, shadowSlots, shadowLightIndices, Array.isArray(lights) ? lights : []); if (span) device.queue.writeBuffer(gpuRes.shadowUniformBuffer, span[0] * 4, uniformState._shadowUniformF.subarray(span[0], span[1]));
  }

  function materialUniformData(uniformState: any, material: any, receiveShadow: any) {
    var mat = material || {};
    var albedoRGBA = sceneColorRGBA(mat.color, [0.8, 0.8, 0.8, 1]);

    // Stable material fields retain their ABI; model transforms use the frame arena.
    var f = uniformState._materialUniformF;
    var u = uniformState._materialUniformU;

    f[0] = sceneWebGPUSRGBChannelToLinear(albedoRGBA[0]);
    f[1] = sceneWebGPUSRGBChannelToLinear(albedoRGBA[1]);
    f[2] = sceneWebGPUSRGBChannelToLinear(albedoRGBA[2]);
    f[3] = sceneNumber(mat.roughness, 0.5);
    f[4] = sceneNumber(mat.metalness, 0);
    f[5] = sceneNumber(mat.emissive, 0);
    f[6] = clamp01(sceneNumber(mat.opacity, 1));
    f[7] = clamp01(sceneNumber(mat.clearcoat, 0));
    f[8] = clamp01(sceneNumber(mat.sheen, 0));
    f[9] = clamp01(sceneNumber(mat.transmission, 0));
    f[10] = clamp01(sceneNumber(mat.iridescence, 0));
    f[11] = Math.max(-1, Math.min(1, sceneNumber(mat.anisotropy, 0)));
    u[12] = (mat.unlit || mat.kind === "flat" || mat.materialKind === "flat") ? 1 : 0;
    u[13] = 0; u[14] = 0; u[15] = 0; u[16] = 0; u[17] = 0;
    u[18] = receiveShadow ? 1 : 0;
    u[19] = 0; // hasOcclusionMap, set by createMaterialBindGroup
    for (var mi = 0; mi < 16; mi++) f[20 + mi] = mi % 5 === 0 ? 1 : 0; if (mat.wind && window.__gosx_scene3d_terrain_api) f.set(window.__gosx_scene3d_terrain_api.windFor(mat.wind), 20); // wind sway (scene/wind.go): material.modelMatrix is otherwise unused
    f[36] = f[37] = f[38] = 1;
    f[39] = mat.normalUVScale ? sceneNumber(mat.normalUVScale[0], 1) : 1; // modelScaleSigns.w: normal-map U scale
    f[40] = sceneWebGPUDielectricF0(mat.ior);
    f[41] = 0; // hasSpecularIntensityMap, set by createMaterialBindGroup
    var alphaCutoff = sceneNormalizeMaterialAlphaCutoff(mat.alphaCutoff, null);
    f[42] = (typeof alphaCutoff === "number" && Number.isFinite(alphaCutoff) && alphaCutoff >= 0)
      ? (alphaCutoff <= 1 ? Math.fround(alphaCutoff) : 2)
      : (["alpha", "additive"].includes(mat.renderPass || mat.blendMode) ? -2 : -1); // -2 preserves texel alpha for BLEND; -1 keeps OPAQUE semantics.
    f[43] = mat.normalUVScale ? sceneNumber(mat.normalUVScale[1], 1) : 1; // normalUVScaleV
    var specular = sceneWebGPUSpecularFactors(mat);
    f[44] = specular.f0[0];
    f[45] = specular.f0[1];
    f[46] = specular.f0[2];
    f[47] = specular.f90;
    var colorLogs = sceneWebGPUSpecularColorLogs(mat);
    f[48] = colorLogs[0];
    f[49] = colorLogs[1];
    f[50] = colorLogs[2];
    u[51] = 0; // hasSpecularColorMap, set by createMaterialBindGroup
    f[52] = sceneNumber(mat.normalScale, 1);
    f[53] = clamp01(sceneNumber(mat.occlusionStrength, 1));
    u[54] = sceneWebGPUHasEmissiveColor(mat) ? 1 : 0;
    f[55] = Math.max(0.0001, sceneNumber(mat.rimPower, 2));
    var emissiveColor = sceneWebGPUEmissiveColor(mat);
    f[56] = emissiveColor[0];
    f[57] = emissiveColor[1];
    f[58] = emissiveColor[2];
    f[59] = Math.max(0, sceneNumber(mat.rimStrength, 0));
    var rimColor = sceneWebGPURimColor(mat);
    f[60] = rimColor[0];
    f[61] = rimColor[1];
    f[62] = rimColor[2];
    var aa = mat.specularAA || {};
    f[63] = clamp01(sceneNumber(aa.variance, 0));
    f.set(sceneTransmissionVolume(mat), 64);
    f[67] = clamp01(sceneNumber(aa.threshold, 0));
    u[71] = 0; // thickness texture ready flag; existing padding, no buffer growth
    return { data: f, u: u };
  }

  // Memoized bind group on an owner slot. Bind groups reference their
  // resources by identity, so one stays valid while the device, the layout,
  // and every bound resource are unchanged. Recreating them per frame
  // churned thousands of GPU wrapper objects per second across the point
  // layers; WebKit frees the backing Metal objects only on JS garbage
  // collection, so the churn grows the GPU process between collections
  // (iOS jetsam pressure). Identity source per entry: `resource.buffer`
  // for buffer bindings, the resource itself for views and samplers.
  function wgpuCachedBindGroup(device: any, owner: any, slot: any, layout: any, entries: any) {
    var cache = owner[slot];
    if (cache && cache.device === device && cache.layout === layout && cache.ids.length === entries.length) {
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
    owner[slot] = { device: device, layout: layout, ids: ids, bg: bg };
    return bg;
  }

  function createMaterialBindGroup(uniformState: any, gpuRes: any, meshState: any, device: any, material: any, receiveShadow: any, cacheOwner: any) {
    var mat = material || {};
    var uniform = materialUniformData(uniformState, mat, receiveShadow);
    var u = uniform.u;
    // Texture records.
    var textureMaps = [
      { prop: "texture", descriptor: "baseColor", role: "base-color", colorSpace: "srgb", index: 13 },
      { prop: "normalMap", descriptor: "normal", role: "normal", colorSpace: "linear", index: 14 },
      { prop: "roughnessMap", descriptor: "roughness", role: "roughness", colorSpace: "linear", index: 15 },
      { prop: "metalnessMap", descriptor: "metalness", role: "metalness", colorSpace: "linear", index: 16 },
      { prop: "emissiveMap", descriptor: "emissive", role: "emissive", colorSpace: "srgb", index: 17 },
      { prop: "occlusionMap", descriptor: "occlusion", role: "ambient-occlusion", colorSpace: "linear", index: 19 },
      { prop: "specularIntensityMap", descriptor: "specularIntensity", role: "specular-intensity", colorSpace: "linear", index: 41 },
      { prop: "specularColorMap", descriptor: "specularColor", role: "specular-color", colorSpace: "srgb", index: 51 },
      { prop: "thicknessMap", descriptor: "thickness", role: "data", colorSpace: "linear", index: 71 },
    ];
    var texViews = [];
    var texSamplers = [];
    for (var ti = 0; ti < textureMaps.length; ti++) {
      var tm = textureMaps[ti];
      var descriptor = mat.textureDescriptors && mat.textureDescriptors[tm.descriptor];
      var url = descriptor && typeof descriptor.uri === "string" && descriptor.uri.trim()
        ? descriptor.uri.trim()
        : mat[tm.prop];
      var record = url ? wgpuLoadTexture(device, url, gpuRes.textureCache, descriptor, tm.role, tm.colorSpace) : null;
      var loaded = Boolean(record && record.loaded);
      u[tm.index] = loaded ? 1 : 0;
      texViews.push(loaded ? record.view : gpuRes.placeholderView);
      texSamplers.push(wgpuMaterialSampler(device, gpuRes.linearSampler, descriptor, tm.role));
    }
    var owner = (cacheOwner && typeof cacheOwner === "object")
      ? cacheOwner
      : ((material && typeof material === "object") ? material : gpuRes.defaultMaterialOwner);
    var slot = receiveShadow ? "_gosxWGPUMaterialShadowUniform" : "_gosxWGPUMaterialUniform";
    var materialBuffer = wgpuCachedTrackedBuffer(meshState, device,
      owner,
      slot,
      uniform.data,
      GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
      true
    );
    // Memoize the bind group. The material uniform buffer is written above
    // (dynamic=true) and referenced by identity in the bind group, so the
    // same bind group remains valid across frames as long as the material
    // buffer and texture views are unchanged. Invalidate on device change
    // (device-loss recovery) or when a texture finishes async loading.
    var bgCacheSlot = receiveShadow ? "_gosxWGPUMatBGCacheS" : "_gosxWGPUMatBGCache";
    var bgCache = owner[bgCacheSlot];
    if (bgCache && bgCache.device === device && bgCache.materialBuffer === materialBuffer &&
        bgCache.modelBuffer === uniformState.materialFrame.buffer) {
      var viewsMatch = true;
      for (var ti2 = 0; ti2 < texViews.length && viewsMatch; ti2++) {
        if (bgCache.texViews[ti2] !== texViews[ti2] || bgCache.texSamplers[ti2] !== texSamplers[ti2]) viewsMatch = false;
      }
      if (viewsMatch) return bgCache.bg;
    }
    // Create bind group with texture views and sampler.
    var entries: any[] = [{ binding: 0, resource: { buffer: materialBuffer } }];
    for (var textureIndex = 0; textureIndex < texViews.length; textureIndex++) entries.push(
      { binding: textureIndex * 2 + 1, resource: texViews[textureIndex] },
      { binding: textureIndex * 2 + 2, resource: texSamplers[textureIndex] });
    var modelBuffer = uniformState.materialFrame.buffer;
    if (modelBuffer) entries.push({ binding: 19, resource: { buffer: modelBuffer, size: SCENE_WEBGPU_CUTAWAY_SLOT_BYTES } });
    var matBG = device.createBindGroup({ layout: gpuRes.materialBindGroupLayout, entries: entries });
    owner[bgCacheSlot] = { device: device, materialBuffer: materialBuffer, texViews: texViews,
      texSamplers: texSamplers, modelBuffer: modelBuffer, bg: matBG };
    return matBG;
  }

  function createFrameBindGroup(uniformState: any, gpuRes: any, device: any, shadowView0: any, shadowView1: any) {
    var view0 = shadowView0 || gpuRes.dummyShadowArrayView;
    var view1 = shadowView1 || gpuRes.dummyShadowArrayView;
    var iblIrradianceView = uniformState.iblResources.active && uniformState.iblResources.irradiance ? uniformState.iblResources.irradiance.view : gpuRes.placeholderCubeView;
    var iblRadianceView = uniformState.iblResources.active && uniformState.iblResources.radiance ? uniformState.iblResources.radiance.view : gpuRes.placeholderCubeView;
    var iblBRDFView = uniformState.iblResources.active && uniformState.iblResources.brdfLUT ? uniformState.iblResources.brdfLUT.view : gpuRes.placeholderView;
    var envMapView = uniformState.envMapResources.active && uniformState.envMapResources.record ? uniformState.envMapResources.record.view : gpuRes.placeholderView;
    var cache = uniformState._frameBindGroupCache;
    if (
      cache &&
      cache.device === device &&
      cache.layout === gpuRes.frameBindGroupLayout &&
      cache.frame === gpuRes.frameUniformBuffer &&
      cache.lights === gpuRes.lightStorageBuffer &&
      cache.fog === gpuRes.fogUniformBuffer &&
      cache.env === gpuRes.envUniformBuffer &&
      cache.view0 === view0 &&
      cache.view1 === view1 &&
      cache.sampler === gpuRes.comparisonSampler &&
      cache.shadow === gpuRes.shadowUniformBuffer &&
      cache.iblIrradiance === iblIrradianceView &&
      cache.iblRadiance === iblRadianceView &&
      cache.iblBRDF === iblBRDFView &&
      cache.iblSampler === gpuRes.linearSampler &&
      cache.envMap === envMapView &&
      cache.envMapSampler === gpuRes.envMapSampler && cache.transmission === uniformState.transmissionView
    ) {
      return cache.bindGroup;
    }
    var bindGroup = _createFrameBindGroupUncached(uniformState, gpuRes, device, [view0, view1, iblIrradianceView, iblRadianceView, iblBRDFView, envMapView]);
    uniformState._frameBindGroupCache = {
      device: device,
      layout: gpuRes.frameBindGroupLayout,
      frame: gpuRes.frameUniformBuffer,
      lights: gpuRes.lightStorageBuffer,
      fog: gpuRes.fogUniformBuffer,
      env: gpuRes.envUniformBuffer,
      view0: view0,
      view1: view1,
      sampler: gpuRes.comparisonSampler,
      shadow: gpuRes.shadowUniformBuffer,
      iblIrradiance: iblIrradianceView,
      iblRadiance: iblRadianceView,
      iblBRDF: iblBRDFView,
      iblSampler: gpuRes.linearSampler,
      envMap: envMapView,
      envMapSampler: gpuRes.envMapSampler,
      transmission: uniformState.transmissionView,
      bindGroup: bindGroup,
    };
    return bindGroup;
  }

  function _createFrameBindGroupUncached(uniformState: any, gpuRes: any, device: any, views: any[], reflectionFrame = false) { var shadowView0 = views[0], shadowView1 = views[1], iblIrradianceView = views[2], iblRadianceView = views[3], iblBRDFView = views[4], envMapView = views[5];
    return device.createBindGroup({
      layout: gpuRes.frameBindGroupLayout,
      entries: [
        { binding: 0, resource: { buffer: reflectionFrame || gpuRes.frameUniformBuffer } },
        { binding: 1, resource: { buffer: gpuRes.lightStorageBuffer } },
        { binding: 2, resource: { buffer: gpuRes.fogUniformBuffer } },
        { binding: 3, resource: { buffer: gpuRes.envUniformBuffer } },
        { binding: 4, resource: shadowView0 || gpuRes.dummyShadowArrayView },
        { binding: 5, resource: gpuRes.comparisonSampler },
        { binding: 6, resource: shadowView1 || gpuRes.dummyShadowArrayView },
        { binding: 7, resource: gpuRes.comparisonSampler },
        { binding: 8, resource: { buffer: gpuRes.shadowUniformBuffer } },
        { binding: 9, resource: iblIrradianceView || gpuRes.placeholderCubeView },
        { binding: 10, resource: iblRadianceView || gpuRes.placeholderCubeView },
        { binding: 11, resource: iblBRDFView || gpuRes.placeholderView },
        { binding: 12, resource: gpuRes.linearSampler },
        { binding: 13, resource: envMapView || gpuRes.placeholderView },
        { binding: 14, resource: gpuRes.envMapSampler },
        { binding: 15, resource: uniformState.transmissionView || gpuRes.placeholderView },
        { binding: 16, resource: uniformState.transmissionResources.sampler },
        { binding: 17, resource: { buffer: uniformState.transmissionResources.uniform } },
      ],
    });
  }

  // Ensure main depth texture matches canvas size.
  function ensureMainDepth(pipeState: any, device: any, width: any, height: any, sampleCount: any) {
    sampleCount = Math.max(1, Math.floor(sampleCount || 1));
    if (pipeState.mainDepthTexture && pipeState.mainDepthWidth === width && pipeState.mainDepthHeight === height && pipeState.mainDepthSampleCount === sampleCount) return;
    if (pipeState.mainDepthTexture) pipeState.mainDepthTexture.destroy();
    pipeState.mainDepthTexture = device.createTexture({
      size: [width, height, 1],
      format: "depth24plus",
      sampleCount: sampleCount,
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
    });
    pipeState.mainDepthView = pipeState.mainDepthTexture.createView();
    pipeState.mainDepthWidth = width;
    pipeState.mainDepthHeight = height;
    pipeState.mainDepthSampleCount = sampleCount;
  }

  function ensureMSAAColor(pipeState: any, device: any, targetFormat: any, width: any, height: any, sampleCount: any) {
    sampleCount = Math.max(1, Math.floor(sampleCount || 1));
    if (sampleCount <= 1) return null;
    if (
      pipeState.mainMSAATexture &&
      pipeState.mainMSAAWidth === width &&
      pipeState.mainMSAAHeight === height &&
      pipeState.mainMSAASampleCount === sampleCount && pipeState.mainMSAAFormat === targetFormat
    ) {
      return pipeState.mainMSAAView;
    }
    if (pipeState.mainMSAATexture) pipeState.mainMSAATexture.destroy();
    pipeState.mainMSAATexture = device.createTexture({
      size: [width, height, 1],
      format: targetFormat,
      sampleCount: sampleCount,
      usage: GPUTextureUsage.RENDER_ATTACHMENT,
    });
    pipeState.mainMSAAView = pipeState.mainMSAATexture.createView();
    pipeState.mainMSAAWidth = width;
    pipeState.mainMSAAHeight = height;
    pipeState.mainMSAASampleCount = sampleCount;
    pipeState.mainMSAAFormat = targetFormat;
    return pipeState.mainMSAAView;
  }

  function ensureElioSkinPipeline(pipeState: any, meshState: any, device: any) {
    if (meshState.elioSkinPipeline) return;
    pipeState.elioSkinShaderModule = device.createShaderModule({ label: "elio-skin-lbs", code: SCENE_ELIO_SKIN_LBS_SOURCE });
    meshState.elioSkinPipeline = wgpuCreateValidatedPipeline(device, "compute", {
      label: "gosx-elio-skin-lbs",
      layout: pipeState.elioSkinPipelineLayout,
      compute: { module: pipeState.elioSkinShaderModule, entryPoint: "skin" },
    });
  }

  function ensureComputedMorphPipeline(pipeState: any, meshState: any, device: any) {
    if (meshState.computedMorphPipeline) return;
    pipeState.computedMorphShaderModule = device.createShaderModule({ label: "computed-morph", code: SCENE_COMPUTED_MORPH_SOURCE });
    meshState.computedMorphPipeline = wgpuCreateValidatedPipeline(device, "compute", {
      label: "gosx-computed-morph",
      layout: pipeState.computedMorphPipelineLayout,
      compute: { module: pipeState.computedMorphShaderModule, entryPoint: "morphPose" },
    });
  }

  function getShadowPipeline(pipeState: any, device: any, reflected?: any) {
    if (reflected) {
      if (!pipeState.shadowReflectedPipeline) pipeState.shadowReflectedPipeline = wgpuCreateShadowPipeline(device, pipeState.shadowBindGroupLayout, pipeState.shadowVertexModule, "cw");
      return pipeState.shadowReflectedPipeline;
    }
    if (!pipeState.shadowPipeline) pipeState.shadowPipeline = wgpuCreateShadowPipeline(device, pipeState.shadowBindGroupLayout, pipeState.shadowVertexModule, "ccw");
    return pipeState.shadowPipeline;
  }

  function getShadowInstancedPipeline(pipeState: any, device: any) {
    if (pipeState.shadowInstancedPipeline) return pipeState.shadowInstancedPipeline;
    pipeState.shadowInstancedPipeline = wgpuCreateShadowInstancedPipeline(device, pipeState.shadowBindGroupLayout, pipeState.shadowInstancedVertexModule, pipeState.shadowFragmentModule);
    return pipeState.shadowInstancedPipeline;
  }
  // pbrFragment picks the PBR fragment module: the detail module, the full
  // module while a shadow slot is active, or the unshadowed module.
  function pbrFragment(pipeState: any, device: any, detail: boolean) {
    if (detail) return pipeState.detailResources.fragment;
    if (pipeState.pbrShadowsActive) return pipeState.pbrFragmentModule;
    if (!pipeState.pbrUnshadowedFragmentModule) pipeState.pbrUnshadowedFragmentModule = device.createShaderModule({ label: "pbr-frag-unshadowed", code: wgpuUnshadowedPBRFragment(WGSL_PBR_FRAGMENT) });
    return pipeState.pbrUnshadowedFragmentModule;
  }

  function pbrKeyPrefix(pipeState: any, detail: boolean) { return (pipeState.selectiveBloom ? "specular|" : "") + (detail || pipeState.pbrShadowsActive ? "" : "unshadowed|"); }

  // Get or create a PBR pipeline for the given blend mode.
  // PBR variant: true for detail, a Selena surface variant, else false.
  function pbrVariant(pipeState: any, gpuRes: any, device: any, material: any) { return material && material.detail ? true : sceneWebGPUSelenaSurfaceVariant(device, pipeState.selenaSurfaces, material, gpuRes.frameBindGroupLayout, gpuRes.materialBindGroupLayout, WGSL_PBR_FRAGMENT) || false; }

  function getPBRPipeline(pipeState: any, device: any, blendMode: any, depthWrite: any, frontFace: any, detail: any = false, uv1: any = "") {
    var reflected = frontFace === "cw", variant = detail === true ? pipeState.detailResources : detail || null, kind = sceneWebGPUPipelineKind(reflected, "pbr");
    var key = pbrKeyPrefix(pipeState, !!variant) + wgpuPipelineKey((detail === true ? sceneDetailVariantKey(kind, true) : variant ? variant.key + ":" + kind : kind) + (uv1 ? ":uv1" + uv1 : ""), blendMode, depthWrite, pipeState.targetFormat, "depth24plus", pipeState.activeSampleCount);
    if (pipeState.pipelineCache[key]) return pipeState.pipelineCache[key];
    var uv1Modules = uv1 ? window.__gosx_scene3d_asset_ext.uv1.gpuModules(device, WGSL_PBR_VERTEX, WGSL_PBR_FRAGMENT, uv1) : null; var pipeline = wgpuCreatePBRPipeline(device, variant ? variant.pipelineLayout : pipeState.pbrPipelineLayout, uv1Modules ? uv1Modules.vertex : pipeState.pbrVertexModule, variant ? variant.fragment : uv1Modules ? uv1Modules.fragment : pbrFragment(pipeState, device, false), blendMode, depthWrite, sceneWebGPUColorTarget(pipeState.targetFormat, pipeState.selectiveBloom), reflected ? -pipeState.activeSampleCount : pipeState.activeSampleCount);
    pipeState.pipelineCache[key] = pipeline;
    return pipeline;
  }

  // detail: true for the detail variant, a Selena surface variant object, or false.
  function getPBRInstancedPipeline(pipeState: any, device: any, blendMode: any, depthWrite: any, detail: any = false) {
    var parts = wgpuInstancedPipelineParts(detail, pipeState.pbrPipelineLayout, pipeState.detailResources, function(d: any) { return pbrFragment(pipeState, device, d); });
    var key = pbrKeyPrefix(pipeState, !!detail) + wgpuPipelineKey(parts.name, blendMode, depthWrite, pipeState.targetFormat, "depth24plus", pipeState.activeSampleCount);
    if (pipeState.pipelineCache[key]) return pipeState.pipelineCache[key];
    var pipeline = wgpuCreatePBRInstancedPipeline(device, parts.layout, pipeState.pbrInstancedVertexModule, parts.fragment, blendMode, depthWrite, sceneWebGPUColorTarget(pipeState.targetFormat, pipeState.selectiveBloom), pipeState.activeSampleCount);
    pipeState.pipelineCache[key] = pipeline;
    return pipeline;
  }

  function getPBRInstancedCullPipeline(pipeState: any, device: any, blendMode: any, depthWrite: any, detail: any = false) {
    var key = pbrKeyPrefix(pipeState, detail) + wgpuPipelineKey(detail ? "pbr-instanced-cull-detail" : "pbr-instanced-cull", blendMode, depthWrite, pipeState.targetFormat, "depth24plus", pipeState.activeSampleCount);
    if (pipeState.pipelineCache[key]) return pipeState.pipelineCache[key];
    var pipeline = wgpuCreatePBRInstancedCullPipeline(device, detail ? pipeState.detailResources.pipelineLayout : pipeState.pbrPipelineLayout, pipeState.pbrInstancedCullVertexModule, pbrFragment(pipeState, device, detail), blendMode, depthWrite, sceneWebGPUColorTarget(pipeState.targetFormat, pipeState.selectiveBloom), pipeState.activeSampleCount);
    pipeState.pipelineCache[key] = pipeline;
    return pipeline;
  }

  function getSceneColorPipeline(pipeState: any, gpuRes: any, device: any, space: any, topology: any, blendMode: any, depthWrite: any) {
    var normalizedSpace = space === "clip" ? "clip" : "world";
    var normalizedTopology = topology === "triangle-list" ? "triangle-list" : "line-list";
    var key = wgpuPipelineKey("scene-color-" + normalizedSpace + "-" + normalizedTopology, blendMode, depthWrite, pipeState.targetFormat, "depth24plus", pipeState.activeSampleCount);
    if (pipeState.pipelineCache[key]) return pipeState.pipelineCache[key];
    var vertexModule = normalizedSpace === "clip" ? pipeState.sceneClipColorVertexModule : pipeState.sceneWorldColorVertexModule;
    var pipeline = wgpuCreateSceneColorPipeline(device, device.createPipelineLayout({ bindGroupLayouts: [gpuRes.frameBindGroupLayout] }), vertexModule, pipeState.sceneColorFragmentModule, normalizedTopology, blendMode, depthWrite, pipeState.targetFormat, pipeState.activeSampleCount);
    pipeState.pipelineCache[key] = pipeline;
    return pipeline;
  }

  function getSurfacePipeline(pipeState: any, device: any, blendMode: any, depthWrite: any) {
    var key = wgpuPipelineKey("surface", blendMode, depthWrite, pipeState.targetFormat, "depth24plus", pipeState.activeSampleCount);
    if (pipeState.pipelineCache[key]) return pipeState.pipelineCache[key];
    var pipeline = wgpuCreateSurfacePipeline(device, pipeState.pbrPipelineLayout, pipeState.surfaceVertexModule, pipeState.surfaceFragmentModule, blendMode, depthWrite, pipeState.targetFormat, pipeState.activeSampleCount);
    pipeState.pipelineCache[key] = pipeline;
    return pipeline;
  }

  function getThickLinePipeline(pipeState: any, gpuRes: any, device: any, blendMode: any, depthWrite: any) {
    var key = wgpuPipelineKey("thick-line", blendMode, depthWrite, pipeState.targetFormat, "depth24plus", pipeState.activeSampleCount);
    if (pipeState.pipelineCache[key]) return pipeState.pipelineCache[key];
    var pipeline = wgpuCreateThickLinePipeline(device, device.createPipelineLayout({ bindGroupLayouts: [gpuRes.frameBindGroupLayout] }), pipeState.thickLineVertexModule, pipeState.thickLineFragmentModule, blendMode, depthWrite, pipeState.targetFormat, pipeState.activeSampleCount);
    pipeState.pipelineCache[key] = pipeline;
    return pipeline;
  }

  // Get or create a points pipeline for the given blend mode.
  function getPointsPipeline(pipeState: any, device: any, blendMode: any, depthWrite: any) {
    var key = wgpuPipelineKey("points", blendMode, depthWrite, pipeState.targetFormat, "depth24plus", pipeState.activeSampleCount);
    if (pipeState.pipelineCache[key]) return pipeState.pipelineCache[key];
    var pipeline = wgpuCreatePointsPipeline(device, pipeState.pointsPipelineLayout, pipeState.pointsVertexModule, pipeState.pointsFragmentModule, blendMode, depthWrite, pipeState.targetFormat, pipeState.activeSampleCount);
    pipeState.pipelineCache[key] = pipeline;
    return pipeline;
  }

  function getPointsVertexPipeline(pipeState: any, device: any, blendMode: any, depthWrite: any) {
    var key = wgpuPipelineKey("points-vertex", blendMode, depthWrite, pipeState.targetFormat, "depth24plus", pipeState.activeSampleCount);
    if (pipeState.pipelineCache[key]) return pipeState.pipelineCache[key];
    var pipeline = wgpuCreatePointsVertexPipeline(device, pipeState.pointsVertexPipelineLayout, pipeState.pointsInstancedVertexModule, pipeState.pointsFragmentModule, blendMode, depthWrite, pipeState.targetFormat, pipeState.activeSampleCount);
    pipeState.pipelineCache[key] = pipeline;
    return pipeline;
  }

  function getObjectSelenaPipeline(getSelenaPipeline: any, object: any, material: any, blend: string, depth: boolean) {
    if (!sceneWebGPUIsSelenaMaterial(material)) return null;
    var options = sceneWebGPUSelenaPipelineOptions(object, sceneWebGPUObjectReflected(object));
    return getSelenaPipeline(material, blend, depth, options);
  }

  function getWaterObjectMeshPipeline(pipeState: any, device: any, texturePassMode: any, blendMode: any, depthWrite: any) {
    var normalizedMode = texturePassMode === 2 ? 2 : 1;
    var normalizedBlend = blendMode === "alpha" || blendMode === "additive" ? blendMode : "alpha";
    var normalizedDepthWrite = depthWrite !== false;
    var key = wgpuPipelineKey("water-object-mesh-" + normalizedMode, normalizedBlend, normalizedDepthWrite, WATER_OBJECT_TEXTURE_FORMAT, "depth24plus", 1);
    if (pipeState.waterObjectMeshPipelineCache[key]) return pipeState.waterObjectMeshPipelineCache[key];
    var fragmentModule = normalizedMode === 2 ? pipeState.waterObjectMeshClippedFragmentModule : pipeState.waterObjectMeshRefractionFragmentModule;
    if (!fragmentModule) return null;
    var pipeline = wgpuCreatePBRPipeline(device, pipeState.pbrPipelineLayout, pipeState.pbrVertexModule, fragmentModule, normalizedBlend, normalizedDepthWrite, WATER_OBJECT_TEXTURE_FORMAT, 1);
    pipeState.waterObjectMeshPipelineCache[key] = pipeline;
    return pipeline;
  }

  // updateInstancedCullSystems dispatches GPU frustum-cull compute passes for
  // all InstancedMeshes that carry a cullKernelWGSL + the gpu-cull capability.
  // Called once per frame, AFTER uploadFrameUniforms (so scratchSelenaViewProjection
  // is ready), BEFORE the shadow pass and main render pass. Mirrors
  // updateComputeParticleSystems shape.
  //
  // vp is the current frame's scratchSelenaViewProjection (post-depth-remap,
  // WebGPU [0,1] clip convention — see extractFrustumPlanesJS comment above).
  //
  // Returns a Map: meshId → { system, isReady }.
  function updateInstancedCullSystems(drawState: any, device: any, instancedMeshes: any, encoder: any, vp: any) {
    if (!Array.isArray(instancedMeshes) || instancedMeshes.length === 0) {
      return drawState.instancedCullSystems;
    }
    // Check if gpu-cull capability is active. The webgpu chunk is only loaded
    // when webgpu is active, so we can check the API's capabilities JSON or
    // simply gate on the feature we know is set to true in
    // 16a-scene-webgpu.capabilities.json. We guard on createSceneInstancedCullSystem
    // being available (exported by 16b into __gosx_scene3d_api).
    var cullApi = (typeof window !== "undefined" && window.__gosx_scene3d_api)
      ? window.__gosx_scene3d_api
      : null;
    var createCullFn = cullApi && typeof cullApi.createSceneInstancedCullSystem === "function"
      ? cullApi.createSceneInstancedCullSystem
      : null;
    var sigFn = cullApi && typeof cullApi.cullSystemSignature === "function"
      ? cullApi.cullSystemSignature
      : null;

    var planes = extractFrustumPlanesJS(vp);
    var activeIds = new Set();
    var fingerprintFn = cullApi && typeof cullApi.sceneInstanceTransformFingerprint === "function"
      ? cullApi.sceneInstanceTransformFingerprint
      : null;
    var maxScaleFn = cullApi && typeof cullApi.sceneInstancedMaxTransformScale === "function"
      ? cullApi.sceneInstancedMaxTransformScale
      : null;

    for (var i = 0; i < instancedMeshes.length; i++) {
      var mesh = instancedMeshes[i];
      if (!mesh || webGPUGPUDrivenHost(drawState, device).owns(mesh)) continue;
      var wgsl = (typeof mesh.cullKernelWGSL === "string" && mesh.cullKernelWGSL.trim()) ? mesh.cullKernelWGSL.trim() : null;
      // A mesh without an authored kernel still culls on the GPU when the
      // renderer's own kernel applies. webGPUBuiltinCullEligible states the
      // three conditions.
      if (!wgsl && !webGPUBuiltinCullEligible(mesh)) continue;
      if (!createCullFn) continue;

      var meshId = (typeof mesh.id === "string" && mesh.id) ? mesh.id : ("mesh-" + i);
      activeIds.add(meshId);

      // Recreate system when kernel or capacity changes.
      var sig = sigFn ? sigFn(mesh) : "";
      var existing = drawState.instancedCullSystems.get(meshId);
      if (!existing || existing.signature !== sig) {
        if (existing && existing.system && typeof existing.system.dispose === "function") {
          existing.system.dispose();
        }
        var newSystem = createCullFn(device, mesh, { fallbackRadius: webGPUInstancedCullRadius(mesh) });
        drawState.instancedCullSystems.set(meshId, { system: newSystem, signature: sig });
        existing = drawState.instancedCullSystems.get(meshId);
      }

      if (!existing || !existing.system) continue;
      var sys = existing.system;
      if (!sys.isReady()) continue;

      // Build instance records for this mesh. The native InstanceRecord is
      // 80B: mat4 (col-major, 64B) + pickData uint32x4 (16B), zero pickData
      // for now (S6 consumer will supply real pickData). The records depend
      // ONLY on the (static) transforms, so build + upload them ONCE per
      // system + transforms array — rebuilding the 80B buffer and re-uploading
      // it to the GPU every frame is pure waste (≈450KB/frame of allocations →
      // GC churn → frame hitches) for a static instanced ring. After the first
      // upload we pass null so sys.update skips the input-buffer write and only
      // refreshes the frustum-plane uniform + dispatches.
      // Instanced meshes serialize their count under `count` (legacyProps);
      // `instanceCount` is often absent. instancedMeshCount() resolves
      // instanceCount→count→0, so the cull operates on the REAL count instead
      // of 0 (which left the input buffer unpopulated → drawIndirect rendered
      // only degenerate zero-matrix instances → an invisible ring).
      // The identity check below catches a scene that reuses one transforms
      // array. A scene core that rebuilds the array every frame defeats it,
      // so the fingerprint catches that case too: equal contents skip the
      // 80-byte-per-instance encode AND the queue.writeBuffer. A static
      // 10 000-instance mesh re-uploaded 800 KB per frame before this.
      var instanceCount = instancedMeshCount(mesh);
      var transforms = mesh.transforms;
      var fingerprint = fingerprintFn && transforms && instanceCount > 0
        ? fingerprintFn(transforms, instanceCount)
        : null;
      var records = null;
      // The fingerprint is authoritative when available. It also fixes the
      // opposite defect: a scene that mutates one transforms array IN PLACE
      // kept the array identity, so the identity check alone never re-uploaded
      // and the GPU culled against stale transforms.
      var contentsChanged = fingerprint !== null
        ? existing.uploadedFingerprint !== fingerprint
        : existing.uploadedTransforms !== transforms;
      if (transforms && instanceCount > 0 && contentsChanged) {
        var tf = (transforms instanceof Float32Array) ? transforms : new Float32Array(transforms);
        var recF = existing.recordScratch && existing.recordScratch.length === instanceCount * 20
          ? existing.recordScratch
          : new Float32Array(instanceCount * 20); // 20 f32 slots = 80B per record; zero-init covers pickData
        existing.recordScratch = recF;
        for (var j = 0; j < instanceCount; j++) {
          var src = j * 16;
          var dst = j * 20;
          for (var k = 0; k < 16; k++) recF[dst + k] = (src + k < tf.length) ? tf[src + k] : 0;
        }
        records = recF;
        existing.uploadedTransforms = transforms;
      }
      if (contentsChanged) {
        existing.uploadedFingerprint = fingerprint;
        // maxInstanceScale only matters for an authored kernel, and it costs a
        // pass over the transforms, so recompute it only when they change.
        existing.maxInstanceScale = maxScaleFn && transforms
          ? maxScaleFn(transforms, instanceCount)
          : 1;
      }

      // Get geometry vertex count for the drawArgs reset.
      var geom = getInstancedGeometry(drawState, device, mesh);
      var vertexCount = (geom && geom.vertexCount > 0) ? geom.vertexCount : 1;

      sys.update(device, encoder, planes, vertexCount, records, instanceCount, {
        transformFingerprint: fingerprint,
        maxInstanceScale: existing.maxInstanceScale,
      });
    }

    // GC: dispose systems for meshes no longer in the bundle.
    for (var _it = drawState.instancedCullSystems.entries(), _entry = _it.next(); !_entry.done; _entry = _it.next()) {
      var _id   = _entry.value[0];
      var _rec  = _entry.value[1];
      if (!activeIds.has(_id)) {
        if (_rec && _rec.system && typeof _rec.system.dispose === "function") {
          _rec.system.dispose();
        }
        drawState.instancedCullSystems.delete(_id);
      }
    }

    // Cull telemetry readback — gated on window.__gosx_scene3d_cull_telemetry === true.
    // Throttled to ~every 30 frames to avoid GPU readback stalls every frame.
    // Poll BEFORE requesting the next readback: mapAsync reads data from the
    // PREVIOUS cycle's copy (already submitted); calling requestSurvivorReadback
    // first would encode a new copy into the still-open encoder, causing mapAsync
    // to race against an unsubmitted write and read stale zeros.
    var cullTelemetryOn = (typeof window !== "undefined" && window.__gosx_scene3d_cull_telemetry === true);
    if (cullTelemetryOn) {
      drawState.cullTelemetryFrameCount += 1;
      // Step 1: poll — reads survivor count from previous cycle's submitted copy.
      var survivorSnapshot = {};
      for (var _pi = drawState.instancedCullSystems.entries(), _pe = _pi.next(); !_pe.done; _pe = _pi.next()) {
        var _pId  = _pe.value[0];
        var _pRec = _pe.value[1];
        if (_pRec && _pRec.system && _pRec.system.isReady()) {
          if (typeof _pRec.system.pollSurvivors === "function") {
            _pRec.system.pollSurvivors();
          }
          survivorSnapshot[_pId] = {
            instanceCount: _pRec.system.instanceCount || 0,
            survivors: _pRec.system.lastSurvivors,
          };
        }
      }
      drawState.lastCullSurvivors = JSON.stringify(survivorSnapshot);
      // Step 2: request next readback after polling (copy encoded into current encoder).
      if (drawState.cullTelemetryFrameCount >= 30) {
        drawState.cullTelemetryFrameCount = 0;
        for (var _ti = drawState.instancedCullSystems.entries(), _te = _ti.next(); !_te.done; _te = _ti.next()) {
          var _tRec = _te.value[1];
          if (_tRec && _tRec.system && _tRec.system.isReady() && typeof _tRec.system.requestSurvivorReadback === "function") {
            _tRec.system.requestSurvivorReadback(encoder);
          }
        }
      }
    } else {
      drawState.lastCullSurvivors = null;
    }

    return drawState.instancedCullSystems;
  }

  function ensureShadowFrameBufferCapacity(drawState: any, device: any, matrixCount: any) {
    var required = Math.max(1, Math.floor(sceneNumber(matrixCount, 1)));
    if (drawState.shadowFrameBuffer && drawState.shadowFrameBufferCapacity >= required) {
      return true;
    }
    var capacity = Math.max(1, drawState.shadowFrameBufferCapacity);
    while (capacity < required) capacity *= 2;
    destroyRendererGPUResource(drawState.shadowFrameBuffer);
    drawState.shadowFrameBuffer = device.createBuffer({
      size: capacity * drawState.shadowFrameBufferStride,
      usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
    });
    drawState.shadowFrameBufferCapacity = capacity;
    return Boolean(drawState.shadowFrameBuffer);
  }

  // drawSharedBufferShadowCaster draws a caster whose positions live in a
  // buffer the renderer owns: the skinning output or a computed morph. It
  // returns the draws made, or -1 when obj is neither.
  function drawSharedBufferShadowCaster(drawState: any, device: any, pass: any, shadowBG: any, baseMatrixOffset: any, sp: any, shadowState: any, obj: any) {
    var skinnedBuffer = null, morphRecord = null;
    if (webGPUObjectIsSkinned(obj)) {
      skinnedBuffer = obj._gosxWGPUElioSkinOutputBuffer;
      if (!skinnedBuffer) return 0;
    } else {
      morphRecord = webGPUObjectComputedMorphDrawRecord(obj);
      if (!morphRecord) return -1;
    }
    if (shadowState.pipeline !== "static") {
      pass.setPipeline(wgpuRequirePipeline(sp));
      shadowState.pipeline = "static";
    }
    pass.setBindGroup(0, shadowBG, [baseMatrixOffset]);
    if (skinnedBuffer) pass.setVertexBuffer(0, skinnedBuffer);
    else if (!webGPUBindComputedMorphBuffer(pass, 0, morphRecord.positionBuffer, obj.vertexCount, 3)) return 0;
    var indexCount = webGPUBindRetainedMeshIndexBuffer(drawState.meshState, device, pass, obj);
    if (indexCount > 0) pass.drawIndexed(indexCount);
    else pass.draw(obj.vertexCount);
    return 1;
  }

  function renderShadowPass(drawState: any, device: any, encoder: any, lightMatrix: any, bundle: any, shadowResource: any, pbrBuffers: any) {
    var sp = getShadowPipeline(drawState.pipeState, device);
    if (!sp) return;
    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
    // Each light owns a base matrix followed by its retained caster matrices.
    // Reserve both regions before encoding so the second light cannot grow
    // or destroy the shared arena before this frame's single submission.
    var baseSlot = Math.max(0, Math.floor(sceneNumber(shadowResource.arenaSlot, sceneNumber(shadowResource.lightSlot, 0)))) * (objects.length + 1);
    var baseMatrixOffset = baseSlot * drawState.shadowFrameBufferStride;
    if (!ensureShadowFrameBufferCapacity(drawState, device, Math.max(sceneNumber(shadowResource.arenaSlots, 2) * (objects.length + 1), baseSlot + objects.length + 1))) return;
    device.queue.writeBuffer(drawState.shadowFrameBuffer, baseMatrixOffset, lightMatrix, 0, 16);
    var shadowBG = wgpuShadowFrameBindGroup(device, drawState.shadowFrameBindGroups, drawState.pipeState.shadowBindGroupLayout, drawState.shadowFrameBuffer);
    var shadowPassDescriptor = {
      colorAttachments: [],
      depthStencilAttachment: {
        view: shadowResource.view,
        depthLoadOp: "clear",
        depthClearValue: 1.0,
        depthStoreOp: "store",
      },
    };
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ var shadowStamps = gpuPassTimestampWrites(drawState.timingState, "shadow");
    if (shadowStamps) shadowPassDescriptor.timestampWrites = shadowStamps;
    var pass = encoder.beginRenderPass(shadowPassDescriptor);
    pass.setBindGroup(0, shadowBG, [baseMatrixOffset]);
    var shadowState = { pipeline: "" };
    var retainedShadowMatrixSlot = baseSlot + 1;
    var casterDraws = 0;
    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (!obj.castShadow) continue;
      if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;
      if (obj._crowdBatch === true || obj._crowdSkin) continue;
      var sharedBufferDraws = drawSharedBufferShadowCaster(drawState, device, pass, shadowBG, baseMatrixOffset, sp, shadowState, obj);
      if (sharedBufferDraws >= 0) {
        casterDraws += sharedBufferDraws;
        continue;
      }

      if (obj.retainedGeometry && obj.directVertices) {
        // Retained geometry casts from cached model-space positions. Indexed
        // meshes use drawIndexed; expanded procedural triangle lists use
        // draw. The shadow uniform carries the light view-projection alone,
        // so every model-space caster folds in its own transform per draw.
        var reflectedCaster = sceneWebGPUObjectReflected(obj), retainedPipelineKind = sceneWebGPUPipelineKind(reflectedCaster, "static");
        if (shadowState.pipeline !== retainedPipelineKind) {
          pass.setPipeline(wgpuRequirePipeline(getShadowPipeline(drawState.pipeState, device, reflectedCaster)));
          shadowState.pipeline = retainedPipelineKind;
        }
        if (!webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, 0, obj, "positions", 3)) continue;
        var casterIndexCount = webGPUBindRetainedMeshIndexBuffer(drawState.meshState, device, pass, obj);
        var casterModel = obj.modelMatrix, casterMatrix = lightMatrix;
        if (casterModel && casterModel.length >= 16) {
          if (!drawState._shadowCombinedMatrixScratch) drawState._shadowCombinedMatrixScratch = new Float32Array(16);
          sceneMat4MultiplyInto(drawState._shadowCombinedMatrixScratch, lightMatrix, casterModel);
          casterMatrix = drawState._shadowCombinedMatrixScratch;
        }
        var casterMatrixOffset = retainedShadowMatrixSlot * drawState.shadowFrameBufferStride;
        retainedShadowMatrixSlot += 1;
        device.queue.writeBuffer(drawState.shadowFrameBuffer, casterMatrixOffset, casterMatrix, 0, 16);
        pass.setBindGroup(0, shadowBG, [casterMatrixOffset]);
        if (casterIndexCount > 0) pass.drawIndexed(casterIndexCount);
        else pass.draw(obj.vertexCount);
        casterDraws++;
        continue;
      }

      if (shadowState.pipeline !== "static") {
        pass.setPipeline(wgpuRequirePipeline(sp));
        shadowState.pipeline = "static";
      }

      pass.setBindGroup(0, shadowBG, [baseMatrixOffset]);
      if (!webGPUBindSceneMeshVertexBuffer(pass, 0, pbrBuffers && pbrBuffers.positions, obj.vertexOffset, obj.vertexCount)) continue;
      pass.draw(obj.vertexCount);
      casterDraws++;
    }

    pass.setBindGroup(0, shadowBG, [baseMatrixOffset]);
    webGPUCrowdHost(drawState, device).drawShadow(pass, Math.max(0, Math.floor(sceneNumber(shadowResource.lightSlot, 0))));
    drawInstancedShadowMeshes(drawState, device, pass, bundle, Math.max(0, Math.floor(sceneNumber(shadowResource.lightSlot, 0))));
    pass.end();
    return casterDraws;
  }

  function drawPBRObjects(drawState: any, device: any, pass: any, objectList: any, bundle: any, materials: any, frameBindGroup: any, blendMode: any, depthWrite: any, pbrBuffers: any, stats: any, frameRequest = null) {
    var lastMaterialIndex = -1;
    var lastReceiveShadow = null;
    var lastMaterialOwner = null;
    var lastModelOffset = -1;
    var currentPipelineKind = "";
    var objectDepthWrite = depthWrite, worldBuffersBound = false;
    function bindMeshAttribute(attr, obj, offset, count) {
      if (webGPUObjectIsSkinned(obj)) return webGPUBindElioSelenaAttribute(drawState.meshState, device, pass, obj, attr, count);
      // Retained geometry keeps its own buffers; it is not in the world-baked
      // pbrBuffers that vertexOffset indexes. Fail closed on a missing stream.
      if (obj.retainedGeometry && obj.directVertices) return webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, attr.slot, obj, attr.source === "custom" ? "custom:" + attr.name : attr.source, attr.components);
      var computedRecord = webGPUObjectComputedMorphDrawRecord(obj);
      if (attr.source === "positions") {
        if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.positionBuffer, count, 3)) return true;
        return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.positions, offset, count);
      }
      if (attr.source === "normals") {
        if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.normalBuffer, count, 3)) return true;
        return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.normals, offset, count);
      }
      if (attr.source === "uvs") {
        return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.uvs, offset, count);
      }
      if (attr.source === "tangents") {
        if (computedRecord && webGPUBindComputedMorphBuffer(pass, attr.slot, computedRecord.tangentBuffer, count, 4)) return true;
        return webGPUBindSceneMeshVertexBuffer(pass, attr.slot, pbrBuffers && pbrBuffers.tangents, offset, count);
      }
      // Custom attribute stream: bind fail-closed from the retained direct
      // vertex data under its "custom:<name>" key. Any failure returns false
      // so the caller skips the draw instead of binding a stale slot.
      if (attr.source === "custom") {
        return webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, attr.slot, obj, "custom:" + attr.name, attr.components);
      }
      return false;
    }
    function bindPBRPipeline(reflected, material = Object.create(null), uv1 = "") {
      var hasDetail = Boolean(material && material.detail), variant = pbrVariant(drawState.pipeState, drawState.gpuRes, device, material), surface = variant === true ? null : variant;
      var kind = hasDetail ? sceneDetailVariantKey(sceneWebGPUPipelineKind(reflected, "pbr"), true) : (surface ? surface.key + ":" : "") + sceneWebGPUPipelineKind(reflected, "pbr");
      kind += ":" + objectDepthWrite + uv1;
      if (surface && surface.groupLayout) pass.setBindGroup(2, sceneWebGPUSelenaSurfaceGroup(device, drawState.pipeState.selenaSurfaces, material, surface, drawState.selenaState.frame));
      if (currentPipelineKind === kind) return;
      pass.setPipeline(wgpuRequirePipeline(getPBRPipeline(drawState.pipeState, device, blendMode, objectDepthWrite, reflected ? "cw" : "ccw", variant, uv1)));
      pass.setBindGroup(0, frameBindGroup);
      currentPipelineKind = kind;
      lastMaterialIndex = -1;
      lastReceiveShadow = null;
      lastMaterialOwner = null;
    }
    for (var i = 0; i < objectList.length; i++) {
      var obj = objectList[i];
      if (obj._crowdBatch === true || obj._crowdSkin) continue;
      var matIndex = sceneNumber(obj.materialIndex, 0);
      var mat = materials[matIndex] || null;
      objectDepthWrite = sceneTransmissionDepthWrite(obj, mat, depthWrite);
      var receiveShadow = !!obj.receiveShadow;
      var offset = Math.max(0, Math.floor(obj.vertexOffset));
      var count = obj.vertexCount;
      var isSkinned = webGPUObjectIsSkinned(obj);
      var computedMorphRecord = !isSkinned ? webGPUObjectComputedMorphDrawRecord(obj) : null;
      if (isSkinned || computedMorphRecord || obj.retainedGeometry) worldBuffersBound = false;
      // cullMode: "back" is getSelenaPipeline's own default (every OTHER
      // caller passes no options and relies on it -- see the comment on
      // pipelineCullMode there). obj.doubleSided opts a single mesh object
      // out of that default; anything absent/false resolves to the exact
      // same "back" value the hardcoded default produced before this line
      // existed. obj.doubleSided opts a mesh out; cullMode:"none" draws both
      // faces regardless of winding, so it is safe under any convention.
      //
      // HISTORY, because the previous comment here inverted the truth and a
      // reader must not restore it. It warned that "back" facing DEPENDED on
      // box/sphere winding their triangles with the right-hand normal
      // OPPOSITE the declared shading normal. That was a real measurement of
      // a real state, but it described a defect rather than a requirement:
      // 12-scene-geometry.ts wound its solid meshes clockwise while
      // 16c-scene-shared-pbr.ts and scene/geom both wound counter-clockwise,
      // so the same authored shape had opposite winding depending only on
      // whether it was instanced.
      //
      // 12-scene-geometry.ts now winds counter-clockwise, so all three
      // producers agree and every generator's geometric normal points the
      // same way as its shading normal. The native renderer is the evidence
      // that this is the correct sense: render/bundle draws scene/geom with
      // CullBack plus FrontFaceCCW (renderer.go), render/gpu/jsgpu maps that
      // pair to cullMode "back" plus frontFace "ccw" with no inversion, and
      // its golden frames pass. So "back" here now culls the same faces the
      // native path culls.
      //
      // UNVERIFIED ON HARDWARE. Confirm on a real GPU that a non-doubleSided
      // Selena mesh still shows its exterior.
      //
      // This is not the only path the winding change moved, and an earlier
      // note here said it was. Three SHADOW sites cull as well: the two
      // gosx-shadow pipelines above and the WebGL2 shadow pass. They cull the
      // FRONT face, so the winding change moved each of them from recording
      // the near surface of a caster to recording the far one.
      // render/bundle/shadow_drift_test.go measures that move and pins all
      // three settings. See client/js/12-scene-geometry-winding.test.mjs for
      // the winding numbers.
      var reflectedDirect = sceneWebGPUObjectReflected(obj);
      var selenaResource = getObjectSelenaPipeline(function(m: any, b: any, d: any, o: any) { return getSelenaPipeline(drawState.pipeState, drawState.selenaState, device, m, b, d, o); }, obj, mat, blendMode, depthWrite);
      if (selenaResource) {
        worldBuffersBound = false;
        var selenaKey = "selena:" + (isSkinned ? "skin:" : "") + (mat && mat.key || matIndex) + sceneWebGPUSelenaPipelineSuffix(obj, reflectedDirect);
        if (currentPipelineKind !== selenaKey) {
          pass.setPipeline(wgpuRequirePipeline(selenaResource.pipeline));
          currentPipelineKind = selenaKey;
        }
        var selenaBG = createSelenaBindGroup(drawState.meshState, drawState.selenaState, device, mat, selenaResource, obj);
        if (selenaBG) {
          pass.setBindGroup(0, selenaBG);
          var allBound = true;
          for (var ai = 0; ai < selenaResource.attrs.length; ai++) {
            if (!bindMeshAttribute(selenaResource.attrs[ai], obj, offset, count)) {
              allBound = false;
              break;
            }
          }
          // Fail closed: skip the whole object (and its draw-call stat)
          // when any declared attribute could not be bound.
          if (!allBound) continue;
          var selenaIndexCount = webGPUSelenaIndexCount(drawState.meshState, device, pass, obj);
          if (selenaIndexCount > 0) pass.drawIndexed(selenaIndexCount);
          else pass.draw(count);
          if (frameRequest) frameRequest.record(obj.id);
          if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
          continue;
        }
      }
      if (mat && mat.detail) pass.setBindGroup(2, sceneWebGPUUploadDetail(device, drawState.pipeState.detailResources, mat, drawState.pipeState.detailEnabled));
      if (isSkinned) {
        bindPBRPipeline(reflectedDirect, mat);
        var skinnedOwner = mat || obj;
        if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow || skinnedOwner !== lastMaterialOwner) {
          var skinnedMatBG = webGPUFrameMaterialGroup(drawState.uniformState.materialFrame, matIndex, mat, receiveShadow, function(m: any, r: any, o: any) { return createMaterialBindGroup(drawState.uniformState, drawState.gpuRes, drawState.meshState, device, m, r, o); });
          pass.setBindGroup(1, skinnedMatBG, [0]);
          lastModelOffset = 0;
          lastMaterialIndex = matIndex;
          lastReceiveShadow = receiveShadow;
          lastMaterialOwner = skinnedOwner;
        }
        if (webGPUBindElioSkinnedBuffers(drawState.meshState, device, pass, obj, count)) {
          var skinnedPBRIndexCount = webGPUBindRetainedMeshIndexBuffer(drawState.meshState, device, pass, obj);
          if (skinnedPBRIndexCount > 0) pass.drawIndexed(skinnedPBRIndexCount);
          else pass.draw(count);
          if (frameRequest) frameRequest.record(obj.id);
          if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
        }
        continue;
      }
      bindPBRPipeline(reflectedDirect, mat, window.__gosx_scene3d_asset_ext.uv1.gpuWanted(obj, mat));

      var matBG = webGPUFrameMaterialGroup(drawState.uniformState.materialFrame, matIndex, mat, receiveShadow, function(m: any, r: any, o: any) { return createMaterialBindGroup(drawState.uniformState, drawState.gpuRes, drawState.meshState, device, m, r, o); });
      var modelOffset = webGPUCutawayOffset(drawState.uniformState.materialFrame, obj);
      if (matIndex !== lastMaterialIndex || receiveShadow !== lastReceiveShadow || matBG !== lastMaterialOwner || modelOffset !== lastModelOffset) {
        drawState.modelDynamicOffsets[0] = modelOffset;
        pass.setBindGroup(1, matBG, drawState.modelDynamicOffsets);
        lastMaterialIndex = matIndex;
        lastReceiveShadow = receiveShadow;
        lastMaterialOwner = matBG;
        lastModelOffset = modelOffset;
      }
      if (computedMorphRecord) {
        if (!webGPUBindComputedMorphBuffer(pass, 0, computedMorphRecord.positionBuffer, count, 3)) continue;
        if (!webGPUBindComputedMorphBuffer(pass, 1, computedMorphRecord.normalBuffer, count, 3)) continue;
        if (!webGPUBindSceneMeshVertexBuffer(pass, 2, pbrBuffers && pbrBuffers.uvs, offset, count)) continue;
        if (!webGPUBindComputedMorphBuffer(pass, 3, computedMorphRecord.tangentBuffer, count, 4)) continue;
        pass.draw(count);
        if (frameRequest) frameRequest.record(obj.id);
        if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
        continue;
      }
      if (obj.retainedGeometry) {
        if (!webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, 0, obj, "positions", 3)) continue;
        if (!webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, 1, obj, "normals", 3)) continue;
        if (!webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, 2, obj, "uvs", 2)) continue;
        if (!webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, 3, obj, "tangents", 4)) continue;
        var pbrIndexCount = webGPUBindRetainedMeshIndexBuffer(drawState.meshState, device, pass, obj);
        if (pbrIndexCount > 0) {
          pass.drawIndexed(pbrIndexCount);
        } else {
          pass.draw(count);
        }
        if (frameRequest) frameRequest.record(obj.id);
        if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
        continue;
      }
      if (!worldBuffersBound) worldBuffersBound = wgpuBindWorldMeshBuffers(pass, pbrBuffers);
      if (!worldBuffersBound) continue;
      window.__gosx_scene3d_asset_ext.uv1.gpuWanted(obj, mat) ? (worldBuffersBound = false, window.__gosx_scene3d_asset_ext.uv1.gpuDraw(device, pass, obj, offset, count, webGPUBindSceneMeshVertexBuffer, pbrBuffers)) : pass.draw(count, 1, offset);
      if (frameRequest) frameRequest.record(obj.id);
      if (stats) stats.meshDrawCalls = (stats.meshDrawCalls || 0) + 1;
    }
  }

  function instancedMeshTransformData(drawState: any, device: any, mesh: any, count: any) {
    if (!mesh || count <= 0 || !mesh.transforms) return null;
    if (!mesh._cachedTransforms) mesh._cachedTransforms = scenePrepareInstancedTransforms(webGPUInstancedCacheOwner(drawState, device, mesh.id, mesh), mesh.transforms);
    var data = mesh._cachedTransforms;
    return data && data.length >= count * 16 ? data : null;
  }

  function instancedMeshColorData(drawState: any, device: any, mesh: any, count: any) {
    if (!mesh || count <= 0) return null;
    return mesh._cachedInstanceColors = scenePrepareInstancedColors(webGPUInstancedCacheOwner(drawState, device, mesh.id, mesh), mesh.colors, count, true);
  }

  function getInstancedGeometry(drawState: any, device: any, mesh: any) {
    if (typeof generateInstancedGeometry !== "function") return null;
    var kind = typeof normalizeInstancedGeometryKind === "function"
      ? normalizeInstancedGeometryKind(mesh && mesh.kind)
      : (typeof mesh.kind === "string" ? mesh.kind.toLowerCase() : "box");
    var size = sceneNumber(mesh && mesh.size, 0);
    var w = sceneNumber(mesh.width, 1);
    var h = sceneNumber(mesh.height, 1);
    var d = sceneNumber(mesh.depth, 1);
    var r = sceneNumber(mesh.radius, 0.5);
    var rt = sceneNumber(mesh.radiusTop, r);
    var rb = sceneNumber(mesh.radiusBottom, r);
    var tube = sceneNumber(mesh.tube, 0.3);
    var s = sceneNumber(mesh.segments, 32);
    var radial = sceneNumber(mesh.radialSegments, 32);
    var tubular = sceneNumber(mesh.tubularSegments, 16);
    var key = kind + ":" + size + ":" + w + ":" + h + ":" + d + ":" + r + ":" + rt + ":" + rb + ":" + tube + ":" + s + ":" + radial + ":" + tubular;
    if (drawState.instancedGeometryCache[key]) return drawState.instancedGeometryCache[key];
    var geom = generateInstancedGeometry(kind, {
      size: size,
      width: w,
      height: h,
      depth: d,
      radius: r,
      radiusTop: rt,
      radiusBottom: rb,
      tube: tube,
      segments: s,
      radialSegments: radial,
      tubularSegments: tubular,
    });
    drawState.instancedGeometryCache[key] = geom;
    return geom;
  }

  function ensureInstancedGeometryGPUBuffer(drawState: any, device: any, geom: any, slot: any, data: any) {
    return wgpuCachedTrackedBuffer(drawState.meshState, device, geom, slot, data, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, false);
  }

  // Persistent mesh IDs own buffers and groups across transient draw records.
  function webGPUInstancedCacheOwner(drawState: any, device: any, meshId = "", fallback?: any, namespace = "instanced") {
    if (!meshId) return fallback;
    var key = namespace + ":" + meshId;
    var owner = drawState.instancedCacheOwners.get(key);
    if (!owner) {
      owner = { meshId: meshId, seenEpoch: 0 };
      drawState.instancedCacheOwners.set(key, owner);
    }
    owner.seenEpoch = drawState.meshState.instancedCacheOwnerEpoch;
    return owner;
  }

  // webGPUSweepInstancedCacheOwners frees the cached buffers of instanced
  // meshes that have not drawn for 120 rendered frames.
  function webGPUSweepInstancedCacheOwners(drawState: any, device: any) {
    drawState.instancedCacheOwners.forEach(function(owner, meshId) {
      if (drawState.meshState.instancedCacheOwnerEpoch - owner.seenEpoch <= 120) return;
      var slots = Object.keys(owner).filter(function(slot) { return drawState.meshState.pointsEntryGPUBuffers.has(owner[slot]); });
      for (var i = 0; i < slots.length; i++) {
        var buffer = owner[slots[i]];
        if (!buffer) continue;
        drawState.meshState.pointsEntryGPUBuffers.delete(buffer);
        destroyRendererGPUResource(buffer);
      }
      drawState.instancedCacheOwners.delete(meshId);
    });
  }

  // webGPUGPUDrivenHost returns this renderer's GPU-driven instancing host
  // (indirect-instancing.ts), or the inert host until the compute chunk publishes the
  // factory. The hooks are the renderer's own factories, shader sources and
  // instanced helpers, so the host builds pipelines that match the renderer's.
  function webGPUGPUDrivenHost(drawState: any, device: any) {
    var host = drawState.gpuDrivenHosts.get("host");
    if (host) return host;
    var api = typeof window !== "undefined" ? window.__gosx_scene3d_api : null;
    if (!device || !api || typeof api.createSceneGPUDrivenHost !== "function") return SCENE_GPU_DRIVEN_INERT_HOST;
    host = api.createSceneGPUDrivenHost(device, {
      onResourceReady: drawState.pipelineGuard.changed,
      createFrameBindGroupLayout: wgpuCreateFrameBindGroupLayout,
      createMaterialBindGroupLayout: wgpuCreateMaterialBindGroupLayout,
      createShadowBindGroupLayout: wgpuCreateShadowBindGroupLayout,
      pbrInstancedVertexWGSL: WGSL_PBR_INSTANCED_VERTEX,
      pbrFragmentWGSL: WGSL_PBR_FRAGMENT,
      shadowInstancedVertexWGSL: WGSL_SHADOW_INSTANCED_VERTEX,
      shadowFragmentWGSL: WGSL_SHADOW_FRAGMENT,
      pbrVertexLayout: WGPU_PBR_VERTEX_LAYOUT,
      shadowVertexLayout: WGPU_SHADOW_VERTEX_LAYOUT,
      blendState: wgpuBlendState,
      instancedMeshCount: instancedMeshCount,
      instancedMeshColorData: function(mesh: any, count: any) { return instancedMeshColorData(drawState, device, mesh, count); },
      getInstancedGeometry: function(mesh: any) { return getInstancedGeometry(drawState, device, mesh); },
      ensureInstancedGeometryGPUBuffer: function(geom: any, slot: any, data: any) { return ensureInstancedGeometryGPUBuffer(drawState, device, geom, slot, data); },
      instancedCullRadius: webGPUInstancedCullRadius,
      drawInstancedMeshes: function(pass: any, meshList: any, materials: any, blendMode: any, depthWrite: any) { return drawInstancedMeshes(drawState, device, pass, meshList, materials, blendMode, depthWrite); },
    });
    drawState.gpuDrivenHosts.set("host", host);
    return host;
  }

  // webGPUCrowdHost returns this renderer's crowd host. The
  // hooks bind a batch record's retained geometry and its material group.
  function webGPUCrowdHost(drawState: any, device: any) {
    var host = drawState.gpuDrivenHosts.get("crowd");
    if (host) return host;
    host = createSceneWebGPUCrowd(device, {
      bindAttribute: function(pass: any, slot: number, obj: any, key: string, components: number) { return webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, slot, obj, key, components); },
      bindIndex: function(pass: any, obj: any) { return webGPUBindRetainedMeshIndexBuffer(drawState.meshState, device, pass, obj); },
      materialGroup: function(record, materials) {
        var index = sceneNumber(record.materialIndex, 0);
        return webGPUFrameMaterialGroup(drawState.uniformState.materialFrame, index, materials[index] || null, !!record.receiveShadow, function(m: any, r: any, o: any) { return createMaterialBindGroup(drawState.uniformState, drawState.gpuRes, drawState.meshState, device, m, r, o); });
      },
    });
    drawState.gpuDrivenHosts.set("crowd", host);
    return host;
  }

  // Share content snapshots across all passes and fresh per-frame mesh copies.
  function ensureInstancedTransformGPUBuffer(drawState: any, device: any, mesh: any, data: any) {
    return wgpuStablePBRAttributeBuffer(drawState.meshState, device, "_gosxWGPUInstanceTransformSnapshot", data, webGPUInstancedCacheOwner(drawState, device, mesh && mesh.id) || mesh, "_gosxWGPUInstanceTransformBuffer");
  }

  function ensureInstancedColorGPUBuffer(drawState: any, device: any, mesh: any, data: any) {
    return wgpuStablePBRAttributeBuffer(drawState.meshState, device, "_gosxWGPUInstanceColorSnapshot", data, webGPUInstancedCacheOwner(drawState, device, mesh && mesh.id) || mesh, "_gosxWGPUInstanceColorBuffer");
  }

  function buildInstancedDrawList(drawState: any, device: any, bundle: any, materials: any) {
    var meshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : [];
    var opaque = [];
    var alpha = [];
    var additive = [];
    for (var i = 0; i < meshes.length; i++) {
      var mesh = meshes[i];
      if (!mesh || mesh.viewCulled) continue;
      if (instancedMeshCount(mesh) <= 0) continue;
      if (!instancedMeshTransformData(drawState, device, mesh, instancedMeshCount(mesh))) continue;
      var mat = instancedMeshMaterial(mesh, materials);
      var pass = scenePBRObjectRenderPass(mesh, mat);
      if (pass === "alpha") alpha.push(mesh);
      else if (pass === "additive") additive.push(mesh);
      else opaque.push(mesh);
    }
    alpha.sort(scenePBRDepthSort);
    additive.sort(scenePBRDepthSort);
    return { opaque: opaque, alpha: alpha, additive: additive };
  }

  function drawInstancedMeshes(drawState: any, device: any, pass: any, meshList: any, materials: any, blendMode: any, depthWrite: any) {
    for (var i = 0; i < meshList.length; i++) {
      var mesh = meshList[i];
      var instanceCount = instancedMeshCount(mesh);
      var transformData = instancedMeshTransformData(drawState, device, mesh, instanceCount);
      if (!transformData) continue;

      var geom = getInstancedGeometry(drawState, device, mesh);
      if (!geom || geom.vertexCount <= 0) continue;

      var mat = instancedMeshMaterial(mesh, materials), variant = pbrVariant(drawState.pipeState, drawState.gpuRes, device, mat), surface = wgpuInstancedSurface(variant);
      var meshDepthWrite = sceneTransmissionDepthWrite(mesh, mat, depthWrite);
      pass.setBindGroup(1, createMaterialBindGroup(drawState.uniformState, drawState.gpuRes, drawState.meshState, device, mesh.wind ? Object.assign({}, mat, { wind: mesh.wind }) : mat, !!mesh.receiveShadow, webGPUInstancedCacheOwner(drawState, device, mesh.id) || mesh), [0]);
      if (mat && mat.detail) pass.setBindGroup(2, sceneWebGPUUploadDetail(device, drawState.pipeState.detailResources, mat, drawState.pipeState.detailEnabled));
      else wgpuBindInstancedSurfaceGroup(pass, device, drawState.pipeState.selenaSurfaces, mat, surface, drawState.selenaState.frame);
      if (webGPUGPUDrivenHost(drawState, device).drawMesh(pass, mesh, meshDepthWrite)) continue;

      // Indirect draw via GPU cull (D3: ready cull record → drawIndirect;
      // not-ready / no kernel / capability absent → draw-all).
      var meshId = (typeof mesh.id === "string" && mesh.id) ? mesh.id : ("mesh-" + i);
      var cullRecord = instancedMeshUsesCull(mesh) ? drawState.instancedCullSystems.get(meshId) : null;
      var cullSys = cullRecord && cullRecord.system;

      if (wgpuInstancedCullReady(cullSys, surface)) {
        // GPU-culled path: slot 4 = outputBuf (80B InstanceRecord, cull layout).
        // Use the cull pipeline (loc 8 = pickData vec4u) instead of the
        // standard pipeline (loc 8 = instanceColor vec4f).
        pass.setPipeline(wgpuRequirePipeline(getPBRInstancedCullPipeline(drawState.pipeState, device, blendMode, meshDepthWrite, Boolean(mat && mat.detail))));
        pass.setVertexBuffer(0, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedPositionBuffer", geom.positions));
        pass.setVertexBuffer(1, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedNormalBuffer", geom.normals));
        pass.setVertexBuffer(2, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedUVBuffer", geom.uvs));
        pass.setVertexBuffer(3, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedTangentBuffer", geom.tangents));
        pass.setVertexBuffer(4, cullSys.outputBuf);
        pass.drawIndirect(cullSys.drawArgsBuf, 0);
      } else {
        // Draw-all path (not-ready, no kernel, or capability absent).
        pass.setPipeline(wgpuRequirePipeline(getPBRInstancedPipeline(drawState.pipeState, device, blendMode, meshDepthWrite, variant)));
        pass.setVertexBuffer(0, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedPositionBuffer", geom.positions));
        pass.setVertexBuffer(1, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedNormalBuffer", geom.normals));
        pass.setVertexBuffer(2, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedUVBuffer", geom.uvs));
        pass.setVertexBuffer(3, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedTangentBuffer", geom.tangents));
        pass.setVertexBuffer(4, ensureInstancedTransformGPUBuffer(drawState, device, mesh, transformData));
        pass.setVertexBuffer(5, ensureInstancedColorGPUBuffer(drawState, device, mesh, instancedMeshColorData(drawState, device, mesh, instanceCount)));
        pass.draw(geom.vertexCount, instanceCount);
      }
    }
  }

  function webGPUShadowComputeBounds(drawState: any, device: any, bundle: any) {
    var bounds = typeof sceneShadowComputeBounds === "function" ? sceneShadowComputeBounds(bundle) : null;
    var meshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : [];
    for (var i = 0; i < meshes.length; i++) {
      var mesh = meshes[i];
      if (!mesh || mesh.viewCulled) continue;
      var count = instancedMeshCount(mesh);
      var transforms = instancedMeshTransformData(drawState, device, mesh, count);
      if (!transforms) continue;
      bounds = expandInstancedBounds(bounds, mesh, transforms, count);
    }
    return bounds || { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 };
  }

  function drawInstancedShadowMeshes(drawState: any, device: any, pass: any, bundle: any, lightSlot = 0) {
    var meshes = Array.isArray(bundle && bundle.instancedMeshes) ? bundle.instancedMeshes : [];
    var drew = false;
    for (var i = 0; i < meshes.length; i++) {
      var mesh = meshes[i];
      if (!mesh || mesh.viewCulled || !mesh.castShadow) continue;
      if (webGPUGPUDrivenHost(drawState, device).drawShadowMesh(pass, mesh, lightSlot)) { drew = false; continue; }
      var instanceCount = instancedMeshCount(mesh);
      var transformData = instancedMeshTransformData(drawState, device, mesh, instanceCount);
      if (!transformData) continue;
      var geom = getInstancedGeometry(drawState, device, mesh);
      if (!geom || geom.vertexCount <= 0) continue;
      if (!drew) {
        pass.setPipeline(wgpuRequirePipeline(getShadowInstancedPipeline(drawState.pipeState, device)));
        drew = true;
      }
      pass.setVertexBuffer(0, ensureInstancedGeometryGPUBuffer(drawState, device, geom, "_gosxWGPUInstancedShadowPositionBuffer", geom.positions));
      pass.setVertexBuffer(1, ensureInstancedTransformGPUBuffer(drawState, device, mesh, transformData));
      pass.draw(geom.vertexCount, instanceCount);
    }
  }

  // webGPUSummarizeCullSystems totals the cull dispatch counters across the
  // live systems so the mount can publish them.
  function webGPUSummarizeCullSystems(drawState: any, device: any) {
    var dispatches = 0;
    var skipped = 0;
    var builtin = 0;
    drawState.instancedCullSystems.forEach(function(record) {
      var system = record && record.system;
      if (!system) return;
      dispatches += sceneNumber(system.dispatchCount, 0);
      skipped += sceneNumber(system.skippedDispatchCount, 0);
      if (system.usesBuiltinKernel) builtin += 1;
    });
    return { dispatches: dispatches, skipped: skipped, builtin: builtin };
  }

  // encodeBundleableSceneDraws drives ONE target through the PBR and instanced
  // draws of all three passes. The target is either the recorder (a dry run
  // that issues no WebGPU calls) or a real GPURenderBundleEncoder. Both see the
  // same call sequence, which is what makes the recorder's verdict sound.
  function encodeBundleableSceneDraws(drawState: any, device: any, target: any, ctx: any) {
    var passes = [
      { name: "opaque", blend: "opaque", depthWrite: true },
      { name: "alpha", blend: "alpha", depthWrite: false },
      { name: "additive", blend: "additive", depthWrite: false },
    ];
    for (var p = 0; p < passes.length; p++) {
      var spec = passes[p];
      var meshList = ctx.drawList[spec.name];
      if (meshList && meshList.length > 0) {
        drawPBRObjects(drawState, device, target, meshList, ctx.bundle, ctx.materials, ctx.frameBindGroup, spec.blend, spec.depthWrite, ctx.pbrBuffers, null);
      }
      var instancedList = ctx.instancedDrawList[spec.name];
      if (instancedList && instancedList.length > 0) {
        target.setBindGroup(0, ctx.frameBindGroup);
        drawInstancedMeshes(drawState, device, target, instancedList, ctx.materials, spec.blend, spec.depthWrite);
      }
    }
  }
  // -----------------------------------------------------------------------
  // Authored Points / ComputeParticle render pipelines
  // -----------------------------------------------------------------------
  // Per-layer/system authored pipeline caches. Each cache entry is either a
  // {pipeline} object or {failed:true} sentinel. Failure is one-shot: once
  // an authored shader fails validation the layer falls back to builtin for
  // the rest of the session with a single console.warn.
  //
  // Binding contract for authored Points layers (drawPointsEntries):
  //   @group(0) @binding(0)  FrameUniforms (uniform)
  //   @group(1) @binding(0)  UserUniforms  (authored uniforms, uniform)
  //   @group(2) @binding(0)  PointsUniforms (uniform)
  //   vertex buffer slot 0: per-instance (position:vec3f, size:f32, color:vec4f, stride=32)
  //
  // Binding contract for authored ComputeParticle render (drawComputeParticleEntries):
  //   @group(0) @binding(0)  FrameUniforms (uniform)
  //   @group(1) @binding(0)  UserUniforms  (authored uniforms, uniform)
  //   @group(2) @binding(0)  PointsUniforms (uniform)
  //   @group(2) @binding(1)  particles array<ParticleInstance> (storage read)
  //   no vertex buffers (instance index reads from storage)

  // buildAuthoredPointsVertexPipeline: for Points layers, uses vertex buffer (instanced path).
  function buildAuthoredPointsVertexPipelineAsync(drawState: any, device: any, entry, blendMode, depthWrite, systemID) {
    // The joined cache key embeds both WGSL sources (~13 KB). This function
    // runs per layer per frame, and rebuilding the key allocated ~29 MB/s
    // of transient strings across 19 layers. Memoize the trimmed sources
    // and the key on the entry; the raw source identities plus the
    // pipeline parameters validate the memo. Reusing one key string also
    // keeps the Map lookup cheap (the string hash is computed once).
    var rawVert = (typeof entry.customVertexWGSL === "string") ? entry.customVertexWGSL : "";
    var rawFrag = (typeof entry.customFragmentWGSL === "string") ? entry.customFragmentWGSL : "";
    var memo = entry._gosxWGPUPointsPipelineKeyMemo;
    if (!memo || memo.rawVert !== rawVert || memo.rawFrag !== rawFrag ||
        memo.blendMode !== blendMode || memo.depthWrite !== depthWrite ||
        memo.targetFormat !== drawState.pipeState.targetFormat || memo.sampleCount !== drawState.pipeState.activeSampleCount) {
      var trimmedVert = rawVert.trim();
      var trimmedFrag = rawFrag.trim();
      memo = {
        rawVert: rawVert,
        rawFrag: rawFrag,
        blendMode: blendMode,
        depthWrite: depthWrite,
        targetFormat: drawState.pipeState.targetFormat,
        sampleCount: drawState.pipeState.activeSampleCount,
        vertWGSL: trimmedVert,
        fragWGSL: trimmedFrag,
        key: (trimmedVert && trimmedFrag)
          ? [trimmedVert, trimmedFrag, blendMode, depthWrite ? "1" : "0", drawState.pipeState.targetFormat, drawState.pipeState.activeSampleCount].join("|")
          : "",
      };
      entry._gosxWGPUPointsPipelineKeyMemo = memo;
    }
    var vertWGSL = memo.vertWGSL;
    var fragWGSL = memo.fragWGSL;
    if (!vertWGSL || !fragWGSL) return null; // no authored shader
    var cacheKey = memo.key;
    var cached = drawState.pointsAuthoredPipelineCache.get(cacheKey);
    if (cached) return cached.failed ? null : cached;

    var pending = { pending: true };
    drawState.pointsAuthoredPipelineCache.set(cacheKey, pending);
    var scopedDevice = device;
    if (!scopedDevice) {
      drawState.pointsAuthoredPipelineCache.delete(cacheKey);
      return null;
    }
    var vertMod = scopedDevice.createShaderModule({ label: "points-authored-vert", code: vertWGSL });
    renderTruth().captureShaderInfo(vertMod, "points-authored-vert");
    var fragMod = scopedDevice.createShaderModule({ label: "points-authored-frag", code: fragWGSL });
    renderTruth().captureShaderInfo(fragMod, "points-authored-frag");

    function markFailed(reason) {
      sceneReportPipelineFailure("points", systemID, reason);
      if (!drawState.pointsAuthoredLayerFailed.get(systemID)) {
        drawState.pointsAuthoredLayerFailed.set(systemID, true);
        console.warn("[gosx] Points authored pipeline failed for layer '" + systemID + "'; falling back to builtin.", reason);
      }
      drawState.pointsAuthoredPipelineCache.set(cacheKey, { failed: true });
    }

    // This is the site the mis-pairing bug hit hardest: 19 point layers on
    // one page build here, all overlapping, all previously sharing one
    // device error scope stack. Validation is now per object.
    scopedDevice.createRenderPipelineAsync({
      label: "gosx-points-authored-" + blendMode,
      layout: drawState.pointsAuthoredVertexPipelineLayout,
      vertex: { module: vertMod, entryPoint: "vertexMain", buffers: WGPU_POINTS_INSTANCE_VERTEX_LAYOUT },
      fragment: { module: fragMod, entryPoint: "fragmentMain", targets: [{ format: drawState.pipeState.targetFormat, blend: wgpuBlendState(blendMode) }] },
      primitive: { topology: "triangle-list" },
      multisample: { count: Math.max(1, Math.floor(drawState.pipeState.activeSampleCount || 1)) },
      depthStencil: { format: "depth24plus", depthWriteEnabled: depthWrite, depthCompare: "less-equal" },
    }).then(function(pipeline) {
      return sceneShaderModuleError([vertMod, fragMod]).then(function(compileErr) {
        if (!drawState.deviceStillActive(scopedDevice)) return;
        if (compileErr) {
          markFailed(compileErr);
          return;
        }
        drawState.pointsAuthoredPipelineCache.set(cacheKey, { pipeline: pipeline });
      });
    }).catch(function(err) {
      return sceneShaderModuleError([vertMod, fragMod]).then(function(compileErr) {
        if (!drawState.deviceStillActive(scopedDevice)) return;
        markFailed(compileErr || err);
      });
    });
    return null; // pending first frame — builtin fallback used
  }

  // buildAuthoredParticleRenderPipelineAsync: for ComputeParticles render, reads from storage.
  function buildAuthoredParticleRenderPipelineAsync(drawState: any, device: any, entry, blendMode, depthWrite, systemID) {
    // Same per-frame key memoization as buildAuthoredPointsVertexPipelineAsync.
    var rawVert = (typeof entry.renderVertexWGSL === "string") ? entry.renderVertexWGSL : "";
    var rawFrag = (typeof entry.renderFragmentWGSL === "string") ? entry.renderFragmentWGSL : "";
    var memo = entry._gosxWGPUCPRenderPipelineKeyMemo;
    if (!memo || memo.rawVert !== rawVert || memo.rawFrag !== rawFrag ||
        memo.blendMode !== blendMode || memo.depthWrite !== depthWrite ||
        memo.targetFormat !== drawState.pipeState.targetFormat || memo.sampleCount !== drawState.pipeState.activeSampleCount) {
      var trimmedVert = rawVert.trim();
      var trimmedFrag = rawFrag.trim();
      memo = {
        rawVert: rawVert,
        rawFrag: rawFrag,
        blendMode: blendMode,
        depthWrite: depthWrite,
        targetFormat: drawState.pipeState.targetFormat,
        sampleCount: drawState.pipeState.activeSampleCount,
        vertWGSL: trimmedVert,
        fragWGSL: trimmedFrag,
        key: (trimmedVert && trimmedFrag)
          ? ["cr", trimmedVert, trimmedFrag, blendMode, depthWrite ? "1" : "0", drawState.pipeState.targetFormat, drawState.pipeState.activeSampleCount].join("|")
          : "",
      };
      entry._gosxWGPUCPRenderPipelineKeyMemo = memo;
    }
    var vertWGSL = memo.vertWGSL;
    var fragWGSL = memo.fragWGSL;
    if (!vertWGSL || !fragWGSL) return null;
    var cacheKey = memo.key;
    var cached = drawState.pointsAuthoredPipelineCache.get(cacheKey);
    if (cached) return cached.failed ? null : cached;

    // Selena points modules may expose dual entries:
    //   vertexStorageMain — reads particle state from a storage buffer (preferred for
    //                       ComputeParticles render path which has no vertex buffers)
    //   vertexMain        — attribute variant (fallback)
    // Check the WGSL source first; also accept shaderLayout.entryPoints.vertexStorage.
    var vertEntry = "vertexMain";
    var renderLayout = entry.renderShaderLayout && typeof entry.renderShaderLayout === "object"
      ? entry.renderShaderLayout
      : entry.shaderLayout;
    if (vertWGSL.indexOf("vertexStorageMain") !== -1) {
      vertEntry = "vertexStorageMain";
    } else if (renderLayout && renderLayout.entryPoints && renderLayout.entryPoints.vertexStorage) {
      vertEntry = renderLayout.entryPoints.vertexStorage;
    }

    var pending = { pending: true };
    drawState.pointsAuthoredPipelineCache.set(cacheKey, pending);
    var scopedDevice = device;
    if (!scopedDevice) {
      drawState.pointsAuthoredPipelineCache.delete(cacheKey);
      return null;
    }
    var vertMod = scopedDevice.createShaderModule({ label: "particle-render-authored-vert", code: vertWGSL });
    renderTruth().captureShaderInfo(vertMod, "particle-render-authored-vert");
    var fragMod = scopedDevice.createShaderModule({ label: "particle-render-authored-frag", code: fragWGSL });
    renderTruth().captureShaderInfo(fragMod, "particle-render-authored-frag");

    function markFailed(reason) {
      sceneReportPipelineFailure("particle-render", systemID, reason);
      if (!drawState.pointsAuthoredLayerFailed.get(systemID)) {
        drawState.pointsAuthoredLayerFailed.set(systemID, true);
        console.warn("[gosx] ComputeParticle authored render pipeline failed for system '" + systemID + "'; falling back to builtin.", reason);
      }
      drawState.pointsAuthoredPipelineCache.set(cacheKey, { failed: true });
    }

    // Per-object validation; no device error scope. This build overlaps the
    // compute kernel build for the SAME system, which is exactly the pairing
    // that reported a rejected kernel against a healthy render pipeline.
    scopedDevice.createRenderPipelineAsync({
      label: "gosx-particle-render-authored-" + blendMode,
      layout: drawState.pointsAuthoredStoragePipelineLayout,
      vertex: { module: vertMod, entryPoint: vertEntry, buffers: [] },
      fragment: { module: fragMod, entryPoint: "fragmentMain", targets: [{ format: drawState.pipeState.targetFormat, blend: wgpuBlendState(blendMode) }] },
      primitive: { topology: "triangle-list" },
      multisample: { count: Math.max(1, Math.floor(drawState.pipeState.activeSampleCount || 1)) },
      depthStencil: { format: "depth24plus", depthWriteEnabled: depthWrite, depthCompare: "less-equal" },
    }).then(function(pipeline) {
      return sceneShaderModuleError([vertMod, fragMod]).then(function(compileErr) {
        if (!drawState.deviceStillActive(scopedDevice)) return;
        if (compileErr) {
          markFailed(compileErr);
          return;
        }
        drawState.pointsAuthoredPipelineCache.set(cacheKey, { pipeline: pipeline });
      });
    }).catch(function(err) {
      return sceneShaderModuleError([vertMod, fragMod]).then(function(compileErr) {
        if (!drawState.deviceStillActive(scopedDevice)) return;
        markFailed(compileErr || err);
      });
    });
    return null;
  }

  // ensurePointsAuthoredUserUniformBuffer: allocates / updates a per-layer
  // user-uniform buffer from entry.customUniforms and shaderLayout.
  function ensurePointsAuthoredUserUniformBuffer(drawState: any, device: any, entry, ownerKey, uniforms, layout) {
    var uniformData = sceneSelenaUniformData({ customUniforms: uniforms, shaderLayout: layout }, null, null, drawState.selenaState.frame);
    if (!uniformData || uniformData.byteLength === 0) {
      // No user uniforms — create a minimal 16-byte placeholder so group(1) is always bound.
      uniformData = new Float32Array(4);
    }
    var cacheOwner = entry;
    var buffer = wgpuCachedTrackedBuffer(drawState.meshState, device,
      cacheOwner,
      ownerKey,
      uniformData,
      GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST,
      true
    );
    return buffer;
  }

  function disposeComputeParticleSystems(drawState: any) {
    for (const record of drawState.computeParticleSystems.values()) {
      if (record && record.system && typeof record.system.dispose === "function") {
        try { record.system.dispose(); } catch (_err) {}
      }
    }
    drawState.computeParticleSystems.clear();
    drawState.lastComputeParticleTimeSeconds = null;
  }

  function syncComputeParticleSystems(drawState: any, device: any, entries) {
    var activeIds = new Set();
    var records = [];
    var sourceEntries = Array.isArray(entries) ? entries : [];
    for (var i = 0; i < sourceEntries.length; i++) {
      var entry = sourceEntries[i];
      if (!entry || typeof entry !== "object") continue;
      var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-particles-" + i);
      var signature = sceneComputeSystemSignature(entry);
      activeIds.add(id);
      var record = drawState.computeParticleSystems.get(id);
      if (!record || record.signature !== signature) {
        if (record && record.system && typeof record.system.dispose === "function") {
          record.system.dispose();
        }
        record = {
          signature: signature,
          system: createSceneParticleSystem(device, entry),
        };
        drawState.computeParticleSystems.set(id, record);
      } else if (record.system) {
        record.system.entry = entry;
      }
      if (record && record.system) {
        records.push(record);
      }
    }
    for (const [id, record] of drawState.computeParticleSystems.entries()) {
      if (!activeIds.has(id)) {
        if (record && record.system && typeof record.system.dispose === "function") {
          record.system.dispose();
        }
        drawState.computeParticleSystems.delete(id);
      }
    }
    return records;
  }

  function updateComputeParticleSystems(drawState: any, device: any, entries, encoder, timeSeconds) {
    var currentTime = Number.isFinite(timeSeconds) ? timeSeconds : 0;
    var deltaTime = drawState.lastComputeParticleTimeSeconds == null
      ? 0
      : Math.max(0, Math.min(0.1, currentTime - drawState.lastComputeParticleTimeSeconds));
    drawState.lastComputeParticleTimeSeconds = currentTime;
    var records = syncComputeParticleSystems(drawState, device, entries);
    for (var i = 0; i < records.length; i++) {
      if (records[i].system && typeof records[i].system.update === "function") {
        records[i].system.update(device, encoder, deltaTime, currentTime);
      }
    }
    return records;
  }

  function colorPrimitiveOwner(drawState: any, name) {
    if (!drawState.screenLineOwner[name]) drawState.screenLineOwner[name] = {};
    return drawState.screenLineOwner[name];
  }

  function drawColorPrimitive(drawState: any, device: any, renderPass, entry, frameBindGroup) {
    if (!entry || !entry.vertexCount) return false;
    var owner = entry.owner || colorPrimitiveOwner(drawState, entry.name || "primitive");
    var positions = toSceneFloat32Array(entry.positions);
    var colors = toSceneFloat32Array(entry.colors);
    var materials = entry.materials ? toSceneFloat32Array(entry.materials) : fallbackMaterialData(owner, entry.vertexCount);
    var vertexCount = primitiveVertexCount(positions, colors, materials, entry.vertexCount);
    if (vertexCount <= 0) return false;

    var blend = entry.blend === "alpha" || entry.blend === "additive" ? entry.blend : "opaque";
    var topology = entry.topology === "triangle-list" ? "triangle-list" : "line-list";
    var depthWrite = typeof entry.depthWrite === "boolean" ? entry.depthWrite : linePassDepthWrite(blend);
    renderPass.setPipeline(wgpuRequirePipeline(getSceneColorPipeline(drawState.pipeState, drawState.gpuRes, device, entry.space === "clip" ? "clip" : "world", topology, blend, depthWrite)));
    renderPass.setBindGroup(0, frameBindGroup);
    renderPass.setVertexBuffer(0, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUPrimitivePositions", positions, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.setVertexBuffer(1, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUPrimitiveColors", colors, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.setVertexBuffer(2, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUPrimitiveMaterials", materials, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.draw(vertexCount);
    return true;
  }

  function webGPUWorldLinePasses(drawState: any, bundle) {
    if (!hasWorldLineData(bundle)) return [];
    if (typeof buildSceneWorldDrawPlan === "function") {
      if (!drawState.worldDrawScratch && typeof createSceneWorldDrawScratch === "function") {
        drawState.worldDrawScratch = createSceneWorldDrawScratch();
      }
      var drawPlan = buildSceneWorldDrawPlan(bundle, drawState.worldDrawScratch);
      if (drawPlan) {
        return [
          { name: "world-static-opaque", owner: drawPlan, positions: drawPlan.staticOpaquePositions, colors: drawPlan.staticOpaqueColors, materials: drawPlan.staticOpaqueMaterials, vertexCount: drawPlan.staticOpaqueVertexCount, blend: "opaque", space: "world", topology: "line-list" },
          { name: "world-dynamic-opaque", owner: drawPlan, positions: drawPlan.dynamicOpaquePositions, colors: drawPlan.dynamicOpaqueColors, materials: drawPlan.dynamicOpaqueMaterials, vertexCount: drawPlan.dynamicOpaqueVertexCount, blend: "opaque", space: "world", topology: "line-list" },
          { name: "world-alpha", owner: drawPlan.alphaPositions ? drawPlan : null, positions: drawPlan.alphaPositions, colors: drawPlan.alphaColors, materials: drawPlan.alphaMaterials, vertexCount: drawPlan.alphaVertexCount, blend: "alpha", space: "world", topology: "line-list", depthWrite: false },
          { name: "world-additive", owner: drawPlan.additivePositions ? drawPlan : null, positions: drawPlan.additivePositions, colors: drawPlan.additiveColors, materials: drawPlan.additiveMaterials, vertexCount: drawPlan.additiveVertexCount, blend: "additive", space: "world", topology: "line-list", depthWrite: false },
        ];
      }
    }
    var vertexCount = Math.max(0, Math.floor(sceneNumber(bundle.worldVertexCount, 0)));
    return [{
      name: "world-fallback",
      owner: bundle,
      positions: bundle.worldPositions,
      colors: bundle.worldColors,
      materials: fallbackMaterialData(bundle, vertexCount),
      vertexCount: vertexCount,
      blend: "alpha",
      space: "world",
      topology: "line-list",
      depthWrite: false,
    }];
  }

  function drawWorldLineEntries(drawState: any, device: any, renderPass, entries, passName, frameBindGroup) {
    var drew = false;
    for (var i = 0; i < entries.length; i++) {
      var entry = entries[i];
      if (!entry || entry.blend !== passName) continue;
      drew = drawColorPrimitive(drawState, device, renderPass, entry, frameBindGroup) || drew;
    }
    return drew;
  }

  function webGPUThickLineRecord(drawState: any, bundle) {
    if (!webGPUHasThickWorldLines(bundle) || !webGPUCanUseThickWorldLines(bundle)) return null;
    if (!bundle.worldPositions || !bundle.worldColors) return null;
    if (!drawState.thickLineScratch && typeof createSceneThickLineScratch === "function") {
      drawState.thickLineScratch = createSceneThickLineScratch();
    }
    if (!drawState.thickLineScratch || typeof expandSceneThickLineIntoScratch !== "function") return null;
    var segmentCount = webGPUWorldLineSegmentCount(bundle);
    if (segmentCount <= 0 || segmentCount > 16384) return null;
    var usedSegments = expandSceneThickLineIntoScratch(
      drawState.thickLineScratch,
      bundle.worldPositions,
      bundle.worldColors,
      bundle.worldLineWidths,
      bundle.worldLinePasses,
      segmentCount
    );
    if (usedSegments <= 0) return null;
    return {
      scratch: drawState.thickLineScratch,
      usedVerts: usedSegments * 4,
      owner: drawState.thickLineOwner,
    };
  }

  function drawThickWorldLineEntries(drawState: any, device: any, renderPass, record, passName, frameBindGroup) {
    var scratch = record && record.scratch;
    var usedVerts = record && record.usedVerts || 0;
    if (!scratch || usedVerts <= 0) return false;
    var indexData = thickLinePassIndexData(record, passName);
    if (!indexData || indexData.count <= 0) return false;
    var owner = record.owner || drawState.thickLineOwner;
    var blend = passName === "alpha" || passName === "additive" ? passName : "opaque";
    renderPass.setPipeline(wgpuRequirePipeline(getThickLinePipeline(drawState.pipeState, drawState.gpuRes, device, blend, linePassDepthWrite(blend))));
    renderPass.setBindGroup(0, frameBindGroup);
    renderPass.setVertexBuffer(0, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUThickLinePositionA", scratch.positionsA.subarray(0, usedVerts * 3), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.setVertexBuffer(1, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUThickLinePositionB", scratch.positionsB.subarray(0, usedVerts * 3), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.setVertexBuffer(2, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUThickLineColorA", scratch.colorsA.subarray(0, usedVerts * 4), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.setVertexBuffer(3, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUThickLineColorB", scratch.colorsB.subarray(0, usedVerts * 4), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.setVertexBuffer(4, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUThickLineSide", scratch.sides.subarray(0, usedVerts), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.setVertexBuffer(5, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUThickLineEndpoint", scratch.endpoints.subarray(0, usedVerts), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    renderPass.setVertexBuffer(6, wgpuCachedTrackedBuffer(drawState.meshState, device, owner, "_gosxWGPUThickLineWidth", scratch.widths.subarray(0, usedVerts), GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
    var indexBuffer = wgpuCachedTrackedBuffer(drawState.meshState, device, owner, indexData.slot, indexData.data, GPUBufferUsage.INDEX | GPUBufferUsage.COPY_DST, true);
    renderPass.setIndexBuffer(indexBuffer, "uint16");
    renderPass.drawIndexed(indexData.count);
    return true;
  }

  function drawScreenLines(drawState: any, device: any, renderPass, bundle, frameBindGroup) {
    if (!hasScreenLineData(bundle)) return false;
    var vertexCount = Math.max(0, Math.floor(sceneNumber(bundle.vertexCount, 0)));
    return drawColorPrimitive(drawState, device, renderPass, {
      name: "screen-lines",
      owner: bundle,
      positions: screenLinePositionData(bundle),
      colors: bundle.colors,
      materials: fallbackMaterialData(bundle, vertexCount),
      vertexCount: vertexCount,
      blend: "alpha",
      space: "clip",
      topology: "line-list",
      depthWrite: false,
    }, frameBindGroup);
  }

  function drawSurfaceEntries(drawState: any, device: any, renderPass, bundle, materials, passName, frameBindGroup) {
    var entries = surfaceEntries(bundle, passName);
    if (!entries.length) return false;
    var blend = passName === "alpha" || passName === "additive" ? passName : "opaque";
    renderPass.setPipeline(wgpuRequirePipeline(getSurfacePipeline(drawState.pipeState, device, blend, blend === "opaque")));
    renderPass.setBindGroup(0, frameBindGroup);
    var drew = false;
    for (var i = 0; i < entries.length; i++) {
      var surface = entries[i];
      var mat = materials[sceneNumber(surface.materialIndex, 0)] || null;
      var positions = toSceneFloat32Array(surface.positions);
      var uvs = toSceneFloat32Array(surface.uv);
      var vertexCount = Math.min(Math.floor(positions.length / 3), Math.floor(uvs.length / 2), Math.max(0, Math.floor(sceneNumber(surface.vertexCount, 0))));
      if (vertexCount <= 0) continue;
      renderPass.setBindGroup(1, createMaterialBindGroup(drawState.uniformState, drawState.gpuRes, drawState.meshState, device, mat, false, surface), [0]);
      renderPass.setVertexBuffer(0, wgpuCachedTrackedBuffer(drawState.meshState, device, surface, "_gosxWGPUSurfacePositions", positions, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.setVertexBuffer(1, wgpuCachedTrackedBuffer(drawState.meshState, device, surface, "_gosxWGPUSurfaceUVs", uvs, GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true));
      renderPass.draw(vertexCount);
      drew = true;
    }
    return drew;
  }

  function resolveWebGPUSampleCount(drawState: any, bundle) {
    var requested = sceneNumber(bundle && (bundle.msaaSamples != null ? bundle.msaaSamples : bundle.sampleCount), sceneNumber(drawState.rendererOptions.msaaSamples, 0));
    if (requested <= 1 && (drawState.rendererOptions.antialias === true || sceneBool(bundle && bundle.antialias, false) || sceneBool(bundle && bundle.msaa, false))) {
      requested = 4;
    }
    if (requested >= 4) return 4;
    return 1;
  }

  // -----------------------------------------------------------------------
  // Points drawing (instanced billboard quads)
  // -----------------------------------------------------------------------

  function drawPointsEntries(drawState: any, device: any, pass, bundle, cam, timeSeconds) {
    var stats = webGPUEmptyPointDrawStats();
    var pointsArray = Array.isArray(bundle.points) ? bundle.points : [];
    if (pointsArray.length === 0) return stats;

    var env = bundle.environment || {};
    var fogDensity = sceneNumber(env.fogDensity, 0);
    var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);

    var _pointsModelMat = new Float32Array(16);
    var _pointsTilt = new Float32Array(16);
    var _pointsSpin = new Float32Array(16);

    for (var i = 0; i < pointsArray.length; i++) {
      var entry = pointsArray[i];
      var count = sceneNumber(entry.count, 0);
      if (count <= 0) {
        stats.pointSkippedEmpty += 1;
        continue;
      }

      // Compute model matrix (same logic as WebGL2 backend).
      var px = sceneNumber(entry.x, 0);
      var py = sceneNumber(entry.y, 0);
      var pz = sceneNumber(entry.z, 0);
      var rx = sceneNumber(entry.rotationX, 0);
      var ry = sceneNumber(entry.rotationY, 0);
      var rz = sceneNumber(entry.rotationZ, 0);
      var hasSpin = sceneNumber(entry.spinX, 0) !== 0 || sceneNumber(entry.spinY, 0) !== 0 || sceneNumber(entry.spinZ, 0) !== 0;

      if (hasSpin) {
        var spx = sceneNumber(entry.spinX, 0) * timeSeconds;
        var spy = sceneNumber(entry.spinY, 0) * timeSeconds;
        var spz = sceneNumber(entry.spinZ, 0) * timeSeconds;
        sceneEulerMatrixInto(_pointsSpin, spx, spy, spz, 0, 0, 0);
        sceneEulerMatrixInto(_pointsTilt, rx, ry, rz, px, py, pz);
        sceneMat4MultiplyInto(_pointsModelMat, _pointsTilt, _pointsSpin);
      } else {
        sceneEulerMatrixInto(_pointsModelMat, rx, ry, rz, px, py, pz);
      }

      var pointsModelMat = sceneWebGPUPointModelMatrix(entry, _pointsTilt, _pointsModelMat);

      // Build PointsUniforms buffer.
      // Layout: mat4x4f(64) + vec4 defaultColorAndSize(16) +
      // vec4u flags(16) + vec4 params(16) + vec4 fogColor(16) = 128.
      drawState.pointsUniformScratchF.fill(0);
      var puF = drawState.pointsUniformScratchF;
      var puU = drawState.pointsUniformScratchU;

      puF.set(pointsModelMat, 0);   // modelMatrix @ 0
      var defaultColorRGBA = sceneColorRGBA(entry.color, [1, 1, 1, 1]);
      puF[16] = defaultColorRGBA[0]; // defaultColorAndSize.r @ 64
      puF[17] = defaultColorRGBA[1];
      puF[18] = defaultColorRGBA[2];
      puF[19] = sceneNumber(entry.size, 1); // defaultColorAndSize.w/defaultSize
      puU[20] = 0; // flags.x: hasPerVertexColor
      puU[21] = 0; // flags.y: hasPerVertexSize
      puU[22] = entry.attenuation ? 1 : 0; // flags.z: sizeAttenuation
      puU[23] = scenePointStyleCode(entry.style); // flags.w: pointStyle
      puF[24] = clamp01(sceneNumber(entry.opacity, 1)); // params.x: opacity
      puF[25] = fogDensity > 0 ? 1 : 0; // params.y: hasFog
      puF[26] = fogDensity; // params.z: fogDensity
      puF[27] = sceneNumber(entry.maxPixelSize, 0); // params.w: maxPixelSize
      puF[28] = fogColorRGBA[0]; // fogColor.r @ 112
      puF[29] = fogColorRGBA[1];
      puF[30] = fogColorRGBA[2];
      puF[31] = sceneNumber(entry.minPixelSize, 0); // fogColor.a carries minPixelSize for points.

      // Cache particle typed arrays on entry.
      var rawPositions = entry.positions;
      if (!entry._cachedPos && rawPositions && (Array.isArray(rawPositions) || sceneIsNumericTypedArray(rawPositions)) && rawPositions.length >= count * 3) {
        entry._cachedPos = rawPositions instanceof Float32Array ? rawPositions : new Float32Array(rawPositions);
      }
      var rawSizes = entry.sizes;
      if (!entry._cachedSizes && rawSizes && (Array.isArray(rawSizes) || sceneIsNumericTypedArray(rawSizes)) && rawSizes.length >= count) {
        entry._cachedSizes = rawSizes instanceof Float32Array ? rawSizes : new Float32Array(rawSizes);
      }
      var rawColors = entry.colors;
      if (!entry._cachedColors && rawColors && (Array.isArray(rawColors) || sceneIsNumericTypedArray(rawColors)) && rawColors.length >= count) {
        if (Array.isArray(rawColors) && typeof rawColors[0] === "string") {
          entry._cachedColors = new Float32Array(count * 4);
          for (var ci = 0; ci < count; ci++) {
            var crgba = sceneColorRGBA(rawColors[ci], [1, 1, 1, 1]);
            entry._cachedColors[ci * 4] = crgba[0];
            entry._cachedColors[ci * 4 + 1] = crgba[1];
            entry._cachedColors[ci * 4 + 2] = crgba[2];
            entry._cachedColors[ci * 4 + 3] = crgba[3];
          }
        } else if (rawColors.length >= count * 4) {
          entry._cachedColors = new Float32Array(rawColors);
        } else if (rawColors.length >= count * 3) {
          entry._cachedColors = new Float32Array(count * 4);
          for (var ci2 = 0; ci2 < count; ci2++) {
            entry._cachedColors[ci2 * 4] = rawColors[ci2 * 3];
            entry._cachedColors[ci2 * 4 + 1] = rawColors[ci2 * 3 + 1];
            entry._cachedColors[ci2 * 4 + 2] = rawColors[ci2 * 3 + 2];
            entry._cachedColors[ci2 * 4 + 3] = 1;
          }
        }
      }

      if (!entry._cachedPos) {
        stats.pointSkippedNoPositions += 1;
        continue;
      }

      var hasSizes = !!entry._cachedSizes;
      var hasColors = !!entry._cachedColors;
      puU[20] = hasColors ? 1 : 0;
      puU[21] = hasSizes ? 1 : 0;

      var pointsUniformBuffer = ensurePointsUniformGPUBuffer(drawState.meshState, device, entry, puF);

      // Build particle instance vertex buffer.
      // Each particle: vec3f position(12) + f32 size(4) + vec4f color(16) = 32 bytes.
      var particleData = ensurePointsParticleData(entry, count, hasSizes, hasColors, defaultColorRGBA);
      var pointsParticleBuffer = ensurePointsParticleVertexGPUBuffer(drawState.meshState, device, entry, particleData);

      // Select pipeline: authored (async-validated) when shader fields present,
      // else builtin instanced-vertex pipeline.
      var blendMode = typeof entry.blendMode === "string" ? entry.blendMode.toLowerCase() : "";
      var depthWrite = entry.depthWrite !== false;
      var validBlend = blendMode === "additive" || blendMode === "alpha" ? blendMode : "opaque";

      // Truthiness only — the pipeline builder trims and validates once,
      // memoized on the entry. Trimming ~6 KB WGSL strings here allocated
      // per layer per frame.
      var hasAuthoredWGSL = (typeof entry.customVertexWGSL === "string" && entry.customVertexWGSL) &&
                            (typeof entry.customFragmentWGSL === "string" && entry.customFragmentWGSL);
      var layerID = entry.id || ("points-" + i);
      var authoredResource = hasAuthoredWGSL && !drawState.pointsAuthoredLayerFailed.get(layerID)
        ? buildAuthoredPointsVertexPipelineAsync(drawState, device, entry, validBlend, depthWrite, layerID)
        : null;
      var useAuthored = authoredResource && !authoredResource.failed && !authoredResource.pending && authoredResource.pipeline;

      var pointsBG, pipeline;
      if (useAuthored) {
        // Authored path: bind group 1 = user uniforms, group 2 = PointsUniforms.
        var userUnifBuf = ensurePointsAuthoredUserUniformBuffer(drawState, device, entry, "_gosxWGPUPointsUserUniform", entry.customUniforms, entry.shaderLayout);
        var userUnifBG = wgpuCachedBindGroup(device, entry, "_gosxWGPUPointsUserUniformBG", drawState.pointsAuthoredUserUniformBGL, [
          { binding: 0, resource: { buffer: userUnifBuf } },
        ]);
        pointsBG = wgpuCachedBindGroup(device, entry, "_gosxWGPUPointsUniformBG", drawState.pointsUniformBindGroupLayout, [
          { binding: 0, resource: { buffer: pointsUniformBuffer } },
        ]);
        pipeline = authoredResource.pipeline;
        pass.setPipeline(wgpuRequirePipeline(pipeline));
        pass.setVertexBuffer(0, pointsParticleBuffer);
        pass.setBindGroup(1, userUnifBG);
        pass.setBindGroup(2, pointsBG);
      } else {
        // Builtin path.
        pointsBG = wgpuCachedBindGroup(device, entry, "_gosxWGPUPointsUniformBG", drawState.pointsUniformBindGroupLayout, [
          { binding: 0, resource: { buffer: pointsUniformBuffer } },
        ]);
        pipeline = getPointsVertexPipeline(drawState.pipeState, device, validBlend, depthWrite);
        pass.setPipeline(wgpuRequirePipeline(pipeline));
        pass.setVertexBuffer(0, pointsParticleBuffer);
        pass.setBindGroup(1, createMaterialBindGroup(drawState.uniformState, drawState.gpuRes, drawState.meshState, device, null, false, drawState.gpuRes.defaultMaterialOwner), [0]);
        pass.setBindGroup(2, pointsBG);
      }
      pass.draw(6, count); // 6 vertices per quad, count instances
      stats.pointDrawEntries += 1;
      stats.pointDrawInstances += count;
      stats.pointDrawCalls += 1;
      if (useAuthored) {
        stats.pointAuthoredDrawEntries += 1;
        stats.pointAuthoredDrawInstances += count;
        stats.pointAuthoredDrawCalls += 1;
      }
    }
    return stats;
  }

  function drawComputeParticleEntries(drawState: any, device: any, pass: any, records: any[], environment: any, timeSeconds: number) {
    var stats = {
      computeParticleDrawEntries: 0,
      computeParticleDrawInstances: 0,
      computeParticleDrawCalls: 0,
      computeParticleAuthoredDrawEntries: 0,
      computeParticleAuthoredDrawInstances: 0,
      computeParticleAuthoredDrawCalls: 0,
      computeParticleAuthoredCandidateEntries: 0,
      computeParticleAuthoredPendingEntries: 0,
      computeParticleAuthoredFailedEntries: 0,
      computeParticleSkippedNotReady: 0,
      computeParticleSkippedEmpty: 0,
    };
    if (!Array.isArray(records) || records.length === 0) return stats;

    var env = environment || {};
    var fogDensity = sceneNumber(env.fogDensity, 0);
    var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);
    var _computeModelMat = new Float32Array(16);
    var _computeTilt = new Float32Array(16);
    var _computeSpin = new Float32Array(16);

    for (var i = 0; i < records.length; i++) {
      var record = records[i];
      var system = record && record.system;
      if (!system || !system.renderBuffer || system.count <= 0) {
        stats.computeParticleSkippedEmpty += 1;
        continue;
      }
      if (typeof system.isReady === "function" && !system.isReady()) {
        stats.computeParticleSkippedNotReady += 1;
        continue;
      }

      var entry = system.entry && typeof system.entry === "object" ? system.entry : {};
      var material = entry.material && typeof entry.material === "object" ? entry.material : {};
      var emitter = entry.emitter && typeof entry.emitter === "object" ? entry.emitter : {};

      wgpuComputeParticleModelMatrix(emitter, timeSeconds, _computeModelMat, _computeTilt, _computeSpin);

      var puF = wgpuComputeParticlePointsUniforms(drawState.pointsUniformScratchF, drawState.pointsUniformScratchU, entry, material, _computeTilt, _computeModelMat, fogDensity, fogColorRGBA);

      var blendMode = typeof material.blendMode === "string" ? material.blendMode.toLowerCase() : "";
      var validBlend = blendMode === "additive" || blendMode === "alpha" ? blendMode : "opaque";
      var depthWrite = entry.depthWrite === true || (validBlend === "opaque" && entry.depthWrite !== false);

      // Authored render path: check for renderVertexWGSL/renderFragmentWGSL
      // on the entry. Truthiness only — the pipeline builder trims and
      // validates once, memoized on the entry.
      var hasAuthoredRender = (typeof entry.renderVertexWGSL === "string" && entry.renderVertexWGSL) &&
                              (typeof entry.renderFragmentWGSL === "string" && entry.renderFragmentWGSL);
      var pointsUniformBuffer = ensurePointsUniformGPUBuffer(drawState.meshState, device, system, puF);
      var cpSystemID = (entry && typeof entry.id === "string") ? entry.id : ("cp-" + i);
      if (hasAuthoredRender) {
        stats.computeParticleAuthoredCandidateEntries += 1;
      }
      if (hasAuthoredRender && drawState.pointsAuthoredLayerFailed.get(cpSystemID)) {
        stats.computeParticleAuthoredFailedEntries += 1;
      }
      var cpAuthoredResource = hasAuthoredRender && !drawState.pointsAuthoredLayerFailed.get(cpSystemID)
        ? buildAuthoredParticleRenderPipelineAsync(drawState, device, entry, validBlend, depthWrite, cpSystemID)
        : null;
      if (cpAuthoredResource && cpAuthoredResource.pending) {
        stats.computeParticleAuthoredPendingEntries += 1;
      }
      var useCPAuthored = cpAuthoredResource && !cpAuthoredResource.failed && !cpAuthoredResource.pending && cpAuthoredResource.pipeline;

      var pipeline, pointsBG;
      if (useCPAuthored) {
        // Authored render: group 1 = user uniforms, group 2 = PointsUniforms + particles storage.
        var cpUserUnifBuf = ensurePointsAuthoredUserUniformBuffer(drawState, device, system, "_gosxWGPUCPRenderUserUniform", entry.renderUniforms, entry.renderShaderLayout);
        var cpUserUnifBG = wgpuCachedBindGroup(device, system, "_gosxWGPUCPRenderUserUniformBG", drawState.pointsAuthoredUserUniformBGL, [
          { binding: 0, resource: { buffer: cpUserUnifBuf } },
        ]);
        pointsBG = wgpuCachedBindGroup(device, system, "_gosxWGPUCPPointsBG", drawState.pointsBindGroupLayout, [
          { binding: 0, resource: { buffer: pointsUniformBuffer } },
          { binding: 1, resource: { buffer: system.renderBuffer } },
        ]);
        pipeline = cpAuthoredResource.pipeline;
        pass.setPipeline(wgpuRequirePipeline(pipeline));
        pass.setBindGroup(1, cpUserUnifBG);
        pass.setBindGroup(2, pointsBG);
      } else {
        // Builtin render path.
        pointsBG = wgpuCachedBindGroup(device, system, "_gosxWGPUCPPointsBG", drawState.pointsBindGroupLayout, [
          { binding: 0, resource: { buffer: pointsUniformBuffer } },
          { binding: 1, resource: { buffer: system.renderBuffer } },
        ]);
        pipeline = getPointsPipeline(drawState.pipeState, device, validBlend, depthWrite);
        pass.setPipeline(wgpuRequirePipeline(pipeline));
        pass.setBindGroup(1, createMaterialBindGroup(drawState.uniformState, drawState.gpuRes, drawState.meshState, device, null, false, drawState.gpuRes.defaultMaterialOwner), [0]);
        pass.setBindGroup(2, pointsBG);
      }
      pass.draw(6, system.count);
      stats.computeParticleDrawEntries += 1;
      stats.computeParticleDrawInstances += system.count;
      stats.computeParticleDrawCalls += 1;
      if (useCPAuthored) {
        stats.computeParticleAuthoredDrawEntries += 1;
        stats.computeParticleAuthoredDrawInstances += system.count;
        stats.computeParticleAuthoredDrawCalls += 1;
      }
    }
    return stats;
  }

  // -----------------------------------------------------------------------
  // Board labels (M1 GPU-text slice 2): render canvas-board LABEL text as GPU
  // glyphs through the BoardText Selena material. Canonical material source is
  // render/boardgpu/board_text.sel → render/boardgpu/board_text.go (host-side
  // tested). The WGSL + shaderLayout below are copied verbatim from that Go
  // file's boardTextWGSL / boardTextShaderLayout; do NOT diverge from them (and
  // do NOT change the RenderBundle Go schema in this slice).
  // -----------------------------------------------------------------------
  var BOARD_TEXT_WGSL = "struct Uniforms {\n  mvp : mat4x4<f32>,\n  normalMatrix : mat3x3<f32>,\n  textColor : vec3<f32>,\n};\n@group(0) @binding(0) var<uniform> u : Uniforms;\n\n@group(0) @binding(1) var atlas : texture_2d<f32>;\n@group(0) @binding(2) var atlasSampler : sampler;\n\nstruct VertexInput {\n  @location(0) position : vec3<f32>,\n  @location(1) uv : vec2<f32>,\n};\n\nstruct VertexOutput {\n  @builtin(position) position : vec4<f32>,\n  @location(0) vUv : vec2<f32>,\n};\n\n@vertex\nfn vertexMain(in : VertexInput) -> VertexOutput {\n  var out : VertexOutput;\n  out.vUv = in.uv;\n  out.position = (u.mvp * vec4<f32>(in.position, 1.0));\n  return out;\n}\n\n@fragment\nfn fragmentMain(in : VertexOutput) -> @location(0) vec4<f32> {\n  let coverage = textureSample(atlas, atlasSampler, in.vUv).a;\n  return vec4<f32>(u.textColor.r, u.textColor.g, u.textColor.b, coverage);\n}\n";

  // Only the fields the WebGPU Selena pipeline path reads are kept here
  // (attributes, uniformBlock, textures[].wgsl, wgsl.binding, material). The gl/
  // metal/schemaVersion/etc. fields in render/boardgpu/board_text.go's full
  // boardTextShaderLayout are for the GLSL/Metal backends and are inert in JS.
  var BOARD_TEXT_LAYOUT = {
    material: "BoardText",
    attributes: [
      { name: "position", type: "vec3", location: 0 },
      { name: "uv", type: "vec2", location: 1 },
    ],
    uniformBlock: {
      size: 128,
      fields: [
        { name: "mvp", type: "mat4", offset: 0, size: 64 },
        { name: "normalMatrix", type: "mat3", offset: 64, size: 48 },
        { name: "textColor", type: "vec3", offset: 112, size: 12 },
      ],
      defaults: [{ name: "textColor", type: "vec3", values: [0.902, 0.929, 0.953] }],
    },
    textures: [{ name: "atlas", wgsl: { group: 0, textureBinding: 1, samplerBinding: 2 } }],
    wgsl: { group: 0, binding: 0 },
  };

  // A synthetic Selena "material" object so the BoardText draw reuses the exact
  // pipeline path (getSelenaPipeline) that BoardFill rects use. textColor is
  // overwritten per-label before the bind group is built.
  var boardTextMaterial = {
    shaderBackend: "selena",
    customVertexWGSL: BOARD_TEXT_WGSL,
    customFragmentWGSL: BOARD_TEXT_WGSL,
    shaderLayout: BOARD_TEXT_LAYOUT,
    customUniforms: { textColor: [0.902, 0.929, 0.953] },
  };

  // Per-font glyph atlas cache. Key = CSS font string. Each entry holds the
  // uploaded GPUTexture/view, atlas pixel dims, font ascent/descent (CSS px),
  // and a glyph map char → { u0,v0,u1,v1, w (cell width CSS px), advance CSS px }.

  // Stable per-label GPU-buffer owners, keyed by the label's id. Board bundles
  // are re-parsed every dirty frame, so the label objects are fresh each frame;
  // keying the tracked-buffer cache on a persistent owner (not the per-frame
  // object) lets wgpuCachedTrackedBuffer REUSE the uniform/pos/uv buffers across
  // frames instead of reallocating (and leaking) 3×N buffers per pan/zoom frame.

  // ensureBoardGlyphAtlas builds (and caches) a coverage atlas for `font`
  // covering `chars` (a string of needed glyphs). White-on-transparent so the
  // texture alpha = glyph coverage, matching the BoardText fragment's .a read.
  // Returns null when canvas rasterization is unavailable (e.g. node tests).
  function ensureBoardGlyphAtlas(drawState: any, device: any, font, chars) {
    var entry = drawState.boardGlyphAtlases.get(font);
    var needed = "";
    for (var ci = 0; ci < chars.length; ci++) {
      var ch = chars[ci];
      if (entry && entry.glyphs[ch]) continue;
      if (needed.indexOf(ch) === -1) needed += ch;
    }
    if (entry && needed === "") return entry;
    if (typeof OffscreenCanvas === "undefined" && typeof document === "undefined") return null;

    // Union of previously-cached chars + newly needed ones (rebuild whole atlas).
    var allChars = needed;
    if (entry) {
      for (var k in entry.glyphs) {
        if (entry.glyphs.hasOwnProperty(k) && allChars.indexOf(k) === -1) allChars += k;
      }
    }
    if (allChars === "") return entry || null;

    var sizePx = parseBoardFontSizePx(font);
    var pad = 2;
    var measureCanvas = boardCreateCanvas(8, 8);
    if (!measureCanvas) return null;
    var mctx = measureCanvas.getContext("2d");
    // Need a real text-capable 2D context. The node test harness's fake
    // context lacks fillText/measureText, so glyph rasterization degrades to
    // null there (no GPU text) — the documented node-harness behavior; the
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ // DOM-overlay label path still runs unaffected.
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ if (!mctx || typeof mctx.fillText !== "function" || typeof mctx.measureText !== "function") return null;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ mctx.font = font;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ mctx.textBaseline = "alphabetic";
    var mm = mctx.measureText("Mg");
    var ascent = (mm && mm.actualBoundingBoxAscent > 0) ? mm.actualBoundingBoxAscent : sizePx * 0.8;
    var descent = (mm && mm.actualBoundingBoxDescent > 0) ? mm.actualBoundingBoxDescent : sizePx * 0.2;
    var cellH = Math.ceil(ascent + descent) + pad * 2;

    // Lay glyphs left-to-right in a single row.
    var metrics = [];
    var totalW = 0;
    for (var gi = 0; gi < allChars.length; gi++) {
      /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ var g = allChars[gi];
      var adv = mctx.measureText(g).width;
      var cellW = Math.ceil(adv) + pad * 2;
      metrics.push({ ch: g, advance: adv, x: totalW, w: cellW });
      totalW += cellW;
    }
    var atlasW = Math.max(1, totalW);
    var atlasH = Math.max(1, cellH);

    var atlasCanvas = boardCreateCanvas(atlasW, atlasH);
    if (!atlasCanvas) return null;
    var actx = atlasCanvas.getContext("2d");
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ if (!actx) return null;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ actx.clearRect(0, 0, atlasW, atlasH);
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ actx.font = font;
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ actx.textBaseline = "alphabetic";
    actx.fillStyle = "#ffffff";
    var glyphs = {};
    for (var mi = 0; mi < metrics.length; mi++) {
      /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ var me = metrics[mi];
      actx.fillText(me.ch, me.x + pad, pad + ascent);
      glyphs[me.ch] = {
        u0: me.x / atlasW,
        v0: 0,
        u1: (me.x + me.w) / atlasW,
        v1: 1,
        w: me.w,
        advance: me.advance,
      };
    }

    var texture = device.createTexture({
      size: [atlasW, atlasH, 1],
      format: "rgba8unorm",
      usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST | GPUTextureUsage.RENDER_ATTACHMENT,
    });
    device.queue.copyExternalImageToTexture(
      { source: atlasCanvas },
      { texture: texture },
      [atlasW, atlasH]
    );
    if (entry && entry.texture && typeof entry.texture.destroy === "function") {
      entry.texture.destroy();
    }
    var built = {
      texture: texture,
      view: texture.createView(),
      width: atlasW,
      height: atlasH,
      ascent: ascent,
      descent: descent,
      pad: pad,
      glyphs: glyphs,
    };
    drawState.boardGlyphAtlases.set(font, built);
    return built;
  }

  // drawBoardLabels lays out one position+uv quad per glyph per label and draws
  // them through the BoardText Selena pipeline. Glyph world size = pixelSize /
  // zoom (camera.z) so on-screen text stays a constant pixel size regardless of
  // zoom — mirroring the line-width /zoom trick in render/boardgpu/boardgpu.go.
  // The frame MVP (scratchSelenaViewProjection, the same ortho2D MVP BoardFill
  // rects use) is consumed via sceneSelenaUniformData("mvp").
  function drawBoardLabels(drawState: any, device: any, pass, bundle, blendMode, depthWrite) {
    var labels = Array.isArray(bundle.labels) ? bundle.labels : [];
    /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ if (!labels.length) return;
    var resource = getSelenaPipeline(drawState.pipeState, drawState.selenaState, device, boardTextMaterial, blendMode, depthWrite);
    if (!resource) return;

    var cam = bundle.camera || {};
    var zoom = (typeof cam.z === "number" && cam.z > 0) ? cam.z : 1;

    // Group labels by font so each distinct font hits one atlas.
    var byFont = {};
    var fontOrder = [];
    for (var i = 0; i < labels.length; i++) {
      var lb = labels[i] || {};
      var fnt = (typeof lb.font === "string" && lb.font !== "") ? lb.font : "14px system-ui, sans-serif";
      if (!byFont[fnt]) { byFont[fnt] = []; fontOrder.push(fnt); }
      byFont[fnt].push(lb);
    }

    var pipelineSet = false;
    for (var fi = 0; fi < fontOrder.length; fi++) {
      var font = fontOrder[fi];
      var group = byFont[font];
      var chars = "";
      for (var gi = 0; gi < group.length; gi++) {
        var t = String(group[gi].text == null ? "" : group[gi].text);
        for (var ti = 0; ti < t.length; ti++) {
          if (chars.indexOf(t[ti]) === -1) chars += t[ti];
        }
      }
      if (chars === "") continue;
      var atlas = ensureBoardGlyphAtlas(drawState, device, font, chars);
      if (!atlas) continue;

      // World units per CSS px (constant on-screen text → divide by zoom).
      var wpp = 1 / zoom;
      var ascentW = atlas.ascent * wpp;
      var descentW = atlas.descent * wpp;
      // The glyph ink sits `pad` px inside its atlas cell; shift the cell left
      // by that so the ink lands at the true pen advance (26b1 fillText parity).
      var padW = (atlas.pad || 0) * wpp;

      for (var li = 0; li < group.length; li++) {
        var label = group[li];
        var text = String(label.text == null ? "" : label.text);
        if (text === "") continue;
        var pos = label.position || {};
        var baseX = (typeof pos.x === "number" ? pos.x : 0);
        var baseY = (typeof pos.y === "number" ? pos.y : 0);

        var positions = [];
        var uvs = [];
        var penX = baseX;
        for (var c = 0; c < text.length; c++) {
          var glyph = atlas.glyphs[text[c]];
          if (!glyph) continue;
          var cellW = glyph.w * wpp;
          // Quad spans the glyph cell: vertically [baseline-descent, baseline+ascent].
          var x0 = penX - padW;
          var x1 = x0 + cellW;
          // The cell extends `pad` px beyond the ink ascent/descent (top & bottom),
          // so the quad must too — otherwise the coverage texels stretch.
          // Quad spans the glyph cell: vertically [baseline-descent, baseline
          // +ascent] in the board's +Y-up world. The atlas row 0 (v=0) is the
          // glyph top, so v=0 maps to yTop and v=1 to yBot — glyphs upright.
          var yTop = baseY + ascentW + padW;
          var yBot = baseY - descentW - padW;
          positions.push(
            x0, yBot, 0, x1, yBot, 0, x1, yTop, 0,
            x0, yBot, 0, x1, yTop, 0, x0, yTop, 0
          );
          uvs.push(
            glyph.u0, glyph.v1, glyph.u1, glyph.v1, glyph.u1, glyph.v0,
            glyph.u0, glyph.v1, glyph.u1, glyph.v0, glyph.u0, glyph.v0
          );
          penX += glyph.advance * wpp;
        }
        var vertexCount = positions.length / 3;
        if (vertexCount === 0) continue;

        // textColor uniform (default #e6edf3 → BoardText default), parsed to RGB.
        var rgba = sceneColorRGBA(
          (typeof label.color === "string" && label.color !== "") ? label.color : "#e6edf3",
          [0.902, 0.929, 0.953]
        );
        boardTextMaterial.customUniforms.textColor = [rgba[0], rgba[1], rgba[2]];

        // Stable owner keyed by label id so the tracked buffers persist and are
        // reused across frames (the label object itself is re-parsed per frame).
        var ownerKey = (typeof label.id === "string" && label.id) ? label.id : ("__bt:" + font + ":" + li);
        var owner = drawState.boardTextOwners.get(ownerKey);
        if (!owner) { owner = {}; drawState.boardTextOwners.set(ownerKey, owner); }
        var uniformData = sceneSelenaUniformData(boardTextMaterial, null, null, drawState.selenaState.frame);
        var uniformBuffer = wgpuCachedTrackedBuffer(drawState.meshState, device,
          owner, "_gosxBoardTextUniform", uniformData,
          GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST, true
        );
        var posBuffer = wgpuCachedTrackedBuffer(drawState.meshState, device,
          owner, "_gosxBoardTextPos", new Float32Array(positions),
          GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true
        );
        var uvBuffer = wgpuCachedTrackedBuffer(drawState.meshState, device,
          owner, "_gosxBoardTextUV", new Float32Array(uvs),
          GPUBufferUsage.VERTEX | GPUBufferUsage.COPY_DST, true
        );
        if (!uniformBuffer || !posBuffer || !uvBuffer) continue;

        var bindGroup = device.createBindGroup({
          layout: resource.bindGroupLayout,
          entries: [
            { binding: 0, resource: { buffer: uniformBuffer } },
            { binding: 1, resource: atlas.view },
            { binding: 2, resource: drawState.gpuRes.linearSampler },
          ],
        });

        if (!pipelineSet) {
          pass.setPipeline(wgpuRequirePipeline(resource.pipeline));
          pipelineSet = true;
        }
        pass.setBindGroup(0, bindGroup);
        pass.setVertexBuffer(0, posBuffer);
        pass.setVertexBuffer(1, uvBuffer);
        pass.draw(vertexCount);
      }
    }
  }

  // -----------------------------------------------------------------------
  // GPU picking wiring
  // -----------------------------------------------------------------------

  // The world-space mesh attribute record for the frame in flight. render()
  // sets it; the pick adapter reads it so the pick pass binds the very same
  // vertex buffer the main pass drew from.

  function ensureScenePicker(drawState: any, device: any) {
    if (drawState.scenePicker || !device) return drawState.scenePicker;
    drawState.scenePicker = createSceneWebGPUPicker(device, {
      viewProjection: function() { return drawState.uniformState.scratchSelenaViewProjection; },
      meshPositions: function() { return drawState.activePickMeshPositions && drawState.activePickMeshPositions.positions || null; },
      bindMeshPositions: function(pass, slot, record, vertexOffset, vertexCount, obj) {
        if (obj && obj.retainedGeometry) {
          return webGPUBindRetainedMeshAttribute(drawState.meshState, device, pass, slot, obj, "positions", 3);
        }
        return webGPUBindSceneMeshVertexBuffer(pass, slot, record, vertexOffset, vertexCount);
      },
      instancedGeometry: function(mesh: any) { return getInstancedGeometry(drawState, device, mesh); },
      instancedGeometryBuffer: function(geom: any, slot: any, data: any) { return ensureInstancedGeometryGPUBuffer(drawState, device, geom, slot, data); },
      instancedTransformBuffer: function(mesh: any, data: any) { return ensureInstancedTransformGPUBuffer(drawState, device, mesh, data); },
      instancedCount: instancedMeshCount,
      instancedTransforms: function(mesh: any, count: any) { return instancedMeshTransformData(drawState, device, mesh, count); },
      // Do NOT route pick failures through reportWebGPUFrameError. That
      // counter drives the mount watchdog that falls the whole scene back to
      // WebGL. A pick that cannot allocate must degrade picking alone.
      onError: function(message) { console.warn("[gosx] WebGPU pick unavailable:", message); },
    });
    return drawState.scenePicker;
  }

  // queuePick schedules one GPU pick. x, y, width, and height are pointer-
  // space values — pass the same numbers you pass sceneScreenToRay. The
  // callback receives a hit record shaped exactly like sceneRaycastPick's, or
  // null for background. It runs one to two frames later; nothing blocks.
  function queuePick(drawState: any, device: any, x, y, width, height, callback) {
    var picker = ensureScenePicker(drawState, device);
    if (!picker) {
      if (typeof callback === "function") callback(null);
      return false;
    }
    return picker.queuePick(x, y, width, height, callback);
  }

  var WEBGPU_DIAGNOSTIC_ATTRIBUTE_INTERVAL_MS = 250;
  function sceneWebGPUCanvasConfiguration(telemetryState: any) {
    var config = {
      device: telemetryState.rawDevice,
      format: telemetryState.presentationFormat,
      usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING,
      alphaMode: telemetryState.activePresentation.alphaMode,
      colorSpace: telemetryState.activePresentation.colorSpace,
    };
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ if (telemetryState.activePresentation.toneMappingMode) {
      config.toneMapping = { mode: telemetryState.activePresentation.toneMappingMode };
    }
    return config;
  }

  function sceneWebGPUSurfaceKey(telemetryState: any, device: any, canvas) {
    var p = telemetryState.activePresentation || {};
    return [
      canvas ? canvas.width : 0,
      canvas ? canvas.height : 0,
      telemetryState.presentationFormat,
      p.alphaMode,
      p.colorSpace,
      p.toneMappingMode || "",
      device ? "d" : "",
    ].join("|");
  }

  // configureWebGPUCanvas reconfigures the canvas swapchain ONLY when the surface
  // it depends on actually changed (size, format, alpha mode, colour space, tone
  // mapping, device).
  //
  // This used to be called unconditionally on every rendered frame, under a comment
  // that claimed it reconfigured "if canvas resized" — there was no such check.
  // GPUCanvasContext.configure() re-creates the drawable; on Metal that is an
  // expensive, synchronising driver operation, so the water demo paid a fixed
  // per-frame stall that no amount of reducing its workload could touch: disabling
  // caustics, reflection, refraction, or cutting the mesh to a quarter of its
  // vertices all changed the frame rate by nothing, because the cost was never in
  // the work being drawn. It pinned frames to a multiple of the display refresh on
  // Apple hardware while D3D12/NVIDIA drivers, which make a redundant configure()
  // nearly free, showed no symptom at all.
  function configureWebGPUCanvas(telemetryState: any, device: any, canvas) {
    var target = canvas || (telemetryState.gpuCtx && telemetryState.gpuCtx.canvas) || null;
    var key = sceneWebGPUSurfaceKey(telemetryState, device, target);
    if (key === telemetryState.configuredSurfaceKey) return false;
    telemetryState.gpuCtx.configure(sceneWebGPUCanvasConfiguration(telemetryState));
    telemetryState.configuredSurfaceKey = key;
    return true;
  }

  function queueWebGPUAttribute(telemetryState: any, name, value) {
    if (telemetryState.webGPUPendingAttributes[name] === value) return;
    telemetryState.webGPUPendingAttributes[name] = value;
    if (!telemetryState.webGPUAttributeFormatting) scheduleWebGPUAttributeMirror(telemetryState);
  }

  function scheduleWebGPUAttributeMirror(telemetryState: any) {
    if (telemetryState.rendererResourcesDisposed || telemetryState.webGPUAttributeTimer !== null || typeof setTimeout !== "function") return;
    var now = (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now();
    var delay = webGPUVerboseTelemetry() || telemetryState.lastWebGPUDiagnosticAttributeAt == null ? 0 :
      Math.max(0, WEBGPU_DIAGNOSTIC_ATTRIBUTE_INTERVAL_MS - (now - telemetryState.lastWebGPUDiagnosticAttributeAt));
    telemetryState.webGPUAttributeTimer = setTimeout(function() {
      telemetryState.webGPUAttributeTimer = null;
      if (!telemetryState.rendererResourcesDisposed) mirrorWebGPUFrameAttributes(telemetryState);
    }, delay);
    // Node harness timers should not retain disposed or static test contexts.
    if (telemetryState.webGPUAttributeTimer && typeof telemetryState.webGPUAttributeTimer.unref === "function") telemetryState.webGPUAttributeTimer.unref();
  }

  function mirrorWebGPUAttributes(telemetryState: any, mount) {
    if (telemetryState.webGPUAttributeTimer !== null) clearTimeout(telemetryState.webGPUAttributeTimer);
    telemetryState.webGPUAttributeTimer = null;
    if (telemetryState.webGPUAttributeMount !== mount) {
      telemetryState.webGPUAttributeMount = mount;
      telemetryState.webGPUEssentialAttributeCache = Object.create(null);
    }
    for (var name in telemetryState.webGPUPendingAttributes) {
      var value = telemetryState.webGPUPendingAttributes[name];
      if (telemetryState.webGPUEssentialAttributeCache[name] === value) continue;
      telemetryState.webGPUEssentialAttributeCache[name] = value;
      if (value === null) mount.removeAttribute(name);
      else mount.setAttribute(name, value);
    }
  }

  function publishWebGPUFrameStats(telemetryState: any, stats: any) {
    var mount = telemetryState.canvas && telemetryState.canvas.parentNode;
    if (!mount) return;
    telemetryState.timing.webGPUFrameSeq += 1;
    var published = stats;
    // Render only into the unpublished buffer, including frames that abort.
    telemetryState.frameStats = telemetryState.lastWebGPUFrameStats || telemetryState.spareFrameStats;
    published.lastError = telemetryState.webGPULastError || undefined;
    published.frameErrorStreak = telemetryState.webGPUConsecutiveFrameErrors || undefined;
    published.frameSeq = telemetryState.timing.webGPUFrameSeq;
    published.frameAt = (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now();
    telemetryState.lastWebGPUFrameStats = published;
    mount.__gosxScene3DWebGPUStats = published;

    wgpuUpdateFrameProof(published, telemetryState.webGPUFrameProof);
    mount.__gosxScene3DWebGPUProof = telemetryState.webGPUFrameProof;
    mirrorWebGPUFrameAttributes(telemetryState);
  }

  // mirrorWebGPUSkinAttributes publishes the skinning and shadow-caster counts.
  function mirrorWebGPUSkinAttributes(telemetryState: any, published: any) {
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-skinned-mesh-objects", String(published.skinnedMeshObjects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-shadow-caster-draws", String(published.shadowCasterDraws || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-computed-morph-dispatches", String(published.computedMorphDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-computed-morph-vertices", String(published.computedMorphVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-computed-morph-kernel", String(published.computedMorphKernel || ""));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-elio-skinning-dispatches", String(published.elioSkinningDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-elio-skinning-vertices", String(published.elioSkinningVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-elio-skinning-output-bytes", String(published.elioSkinningOutputBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-elio-skinning-kernel", String(published.elioSkinningKernel || ""));
  }

  function mirrorWebGPUFrameAttributes(telemetryState: any) {
    var mount = telemetryState.canvas && telemetryState.canvas.parentNode;
    if (!mount || typeof mount.setAttribute !== "function") return;
    var published = telemetryState.lastWebGPUFrameStats;
    var now = (typeof performance !== "undefined" && typeof performance.now === "function") ? performance.now() : Date.now();
    var verboseTelemetry = webGPUVerboseTelemetry();
    var diagnosticElapsed = (published && !telemetryState.webGPUFrameAttributesSeeded) || telemetryState.lastWebGPUDiagnosticAttributeAt == null
      ? Infinity : now - telemetryState.lastWebGPUDiagnosticAttributeAt;
    var mirrorDiagnostics = verboseTelemetry || telemetryState.webGPUAttributeMount !== mount || diagnosticElapsed < 0 || diagnosticElapsed >= WEBGPU_DIAGNOSTIC_ATTRIBUTE_INTERVAL_MS;
    if (!mirrorDiagnostics) { scheduleWebGPUAttributeMirror(telemetryState); return; }
    telemetryState.lastWebGPUDiagnosticAttributeAt = now;
    if (!published) { mirrorWebGPUAttributes(telemetryState, mount); return; }
    telemetryState.webGPUFrameAttributesSeeded = true;
    telemetryState.webGPUAttributeFormatting = true;
    try {
    // Exact progress and simulation proof live in JS. Mirror existing
    // attributes at the diagnostic cadence for probes and E2E checks.
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-frame-seq", String(published.frameSeq || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-frame-at", String(published.frameAt || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-systems", String(published.waterSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-compute-dispatches", String(published.waterComputeDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-draw-calls", String(published.waterDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-tick-seq", String(published.waterSimulationTickSeq || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-substep-seq", String(published.waterSolverSubstepSeq || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-dropped-ticks", String(published.waterDroppedTicks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-normal-seq", String(published.waterNormalDispatchSeq || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-state-sync-seq", String(published.waterSampledStateSyncSeq || 0));
    // M5 at-rest gating (water-parity-campaign): a per-frame count of water
    // systems currently parked (skipping sim substeps/normal/state-copy/
    // caustics, retaining last-rendered textures). Exact values stay in JS;
    // the DOM mirror follows the same cadence as other diagnostics. See
    // WATER_REST_ENERGY_EPSILON's comment above updateWaterSystems.
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-at-rest-systems", String(published.waterAtRestSystems || 0));
    // Preserve the render-bundle counters used by performance probes.
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-bundle-state", String(published.bundleState || "direct"));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-bundle-reason", String(published.bundleReason || ""));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-bundle-encodes", String(published.bundleEncodes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-bundle-replays", String(published.bundleReplays || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-bundle-draws", String(published.bundleDraws || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-mesh-objects", String(published.retainedMeshObjects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-mesh-vertices", String(published.retainedMeshVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-world-baked-mesh-objects", String(published.worldBakedMeshObjects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-world-baked-mesh-vertices", String(published.worldBakedMeshVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-cache-entries", String(published.retainedCacheEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-cache-hits", String(published.retainedCacheHits || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-cache-misses", String(published.retainedCacheMisses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-upload-bytes", String(published.retainedUploadBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-allocations", String(published.retainedAllocations || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-rebuilds", String(published.retainedRebuilds || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-retirements", String(published.retainedRetirements || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-retained-live-bytes", String(published.retainedLiveBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-bundle-build-cpu-ms", String(published.bundleBuildCPUms || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-planner-cpu-ms", String(published.plannerCPUms || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-planner-full-vertex-hash-scans", String(published.plannerFullVertexHashScans || 0));
    // GPU cull dispatch counters. skipped-dispatches proves the static-scene
    // fingerprint skip fired.
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-cull-dispatches", String(published.cullDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-cull-skipped-dispatches", String(published.cullSkippedDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-cull-builtin-systems", String(published.cullBuiltinSystems || 0));
    // Which precision the post chain compiled with. "f16" means the device
    // negotiated shader-f16 and the blur and FXAA stages run half precision.
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-post-precision", String(published.postPrecision || "none"));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-post-dom-region-bounded-passes", String(published.postDOMRegionBoundedPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-post-dom-region-bounded-skips", String(published.postDOMRegionBoundedSkips || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-post-dom-region-bounded-pixels", String(published.postDOMRegionBoundedPixels || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-last-error", telemetryState.webGPULastError || null);
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-entries", String(published.pointEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-instances", String(published.pointInstances || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-draw-entries", String(published.pointDrawEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-draw-instances", String(published.pointDrawInstances || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-draw-calls", String(published.pointDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-authored-draw-entries", String(published.pointAuthoredDrawEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-authored-draw-instances", String(published.pointAuthoredDrawInstances || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-authored-draw-calls", String(published.pointAuthoredDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-skipped-empty", String(published.pointSkippedEmpty || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-point-skipped-no-positions", String(published.pointSkippedNoPositions || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-entries", String(published.computeParticleEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-instances", String(published.computeParticleInstances || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-draw-entries", String(published.computeParticleDrawEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-draw-instances", String(published.computeParticleDrawInstances || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-draw-calls", String(published.computeParticleDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-authored-draw-entries", String(published.computeParticleAuthoredDrawEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-authored-draw-instances", String(published.computeParticleAuthoredDrawInstances || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-authored-draw-calls", String(published.computeParticleAuthoredDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-authored-candidate-entries", String(published.computeParticleAuthoredCandidateEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-authored-pending-entries", String(published.computeParticleAuthoredPendingEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-authored-failed-entries", String(published.computeParticleAuthoredFailedEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-skipped-empty", String(published.computeParticleSkippedEmpty || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-compute-particle-skipped-not-ready", String(published.computeParticleSkippedNotReady || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-cells", String(published.waterCells || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-vertices", String(published.waterVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-compute-systems", String(published.waterAuthoredComputeSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-compute-dispatches", String(published.waterAuthoredComputeDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-compute-fallbacks", String(published.waterAuthoredComputeFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-compute-systems", String(published.waterSelenaComputeSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-compute-dispatches", String(published.waterSelenaComputeDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-compute-fallbacks", String(published.waterSelenaComputeFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-drop-dispatches", String(published.waterDropDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-drop-dispatch-total", String(published.waterDropDispatchTotal || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-drop-event", String(published.waterLastDropEventID || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-systems", String(published.waterObjectSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-dispatches", String(published.waterObjectDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-event-dispatches", String(published.waterObjectEventDispatches || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-event", String(published.waterLastObjectDisplacementEventID || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-spheres", String(published.waterObjectSpheres || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-rounded-systems", String(published.waterRoundedSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-corner-radius", String(published.waterCornerRadius || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-light-dir-x", String(published.waterLightDirX || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-light-dir-y", String(published.waterLightDirY || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-light-dir-z", String(published.waterLightDirZ || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-rest-substeps-skipped", String(published.waterRestSubstepsSkipped || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-uniform-uploads", String(published.waterUniformUploads || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-uniform-uploads-skipped", String(published.waterUniformUploadsSkipped || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-caustic-systems", String(published.waterCausticSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-caustic-passes", String(published.waterCausticPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-caustic-texture-pixels", String(published.waterCausticTexturePixels || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-caustic-systems", String(published.waterAuthoredCausticSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-caustic-passes", String(published.waterAuthoredCausticPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-caustic-fallbacks", String(published.waterAuthoredCausticFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-caustic-fallback-reason", String(published.waterAuthoredCausticFallbackReason || ""));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-caustic-source-bytes", String(published.waterAuthoredCausticSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-entry-caustic-source-bytes", String(published.waterEntryCausticSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-resolved-caustic-source-bytes", String(published.waterResolvedCausticSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-surface-source-bytes", String(published.waterAuthoredSurfaceSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-entry-surface-source-bytes", String(published.waterEntrySurfaceSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-resolved-surface-source-bytes", String(published.waterResolvedSurfaceSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-manifest-shader-systems", String(published.waterManifestShaderSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-manifest-shader-fields", String(published.waterManifestShaderFields || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-manifest-caustic-source-bytes", String(published.waterManifestCausticSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-manifest-surface-source-bytes", String(published.waterManifestSurfaceSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-bundle-shader-systems", String(published.waterBundleShaderSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-bundle-caustic-source-bytes", String(published.waterBundleCausticSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-bundle-surface-source-bytes", String(published.waterBundleSurfaceSourceBytes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-passes", String(published.waterObjectTexturePasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-targets", String(published.waterObjectTextureTargets || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-pixels", String(published.waterObjectTexturePixels || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-width", String(published.waterObjectTextureWidth || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-height", String(published.waterObjectTextureHeight || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-pixel-budget", String(published.waterObjectTexturePixelBudget || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-expensive-pass-cadence", String(published.waterExpensivePassCadence || 1));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-quality-allocation-pending", String(published.waterQualityAllocationPending || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-quality-allocation-failures", String(published.waterQualityAllocationFailures || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-quality-allocation-retry-frame", String(published.waterQualityAllocationRetryFrame || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-mesh-passes", String(published.waterObjectTextureMeshPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-mesh-draw-calls", String(published.waterObjectTextureMeshDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-selena-draw-calls", String(published.waterObjectTextureSelenaDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-fallback-passes", String(published.waterObjectTextureFallbackPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-candidate-objects", String(published.waterObjectTextureCandidateObjects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-selected-objects", String(published.waterObjectTextureSelectedObjects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-fallback-missing-objects", String(published.waterObjectTextureFallbackMissingObjects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-fallback-missing-resources", String(published.waterObjectTextureFallbackMissingResources || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-texture-candidate-profile", String(published.waterObjectTextureCandidateProfile || ""));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-shadow-passes", String(published.waterObjectShadowPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-shadow-texture-pixels", String(published.waterObjectShadowTexturePixels || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-shadow-mesh-passes", String(published.waterObjectShadowMeshPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-shadow-mesh-draw-calls", String(published.waterObjectShadowMeshDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-object-shadow-passes", String(published.waterAuthoredObjectShadowPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-object-shadow-fallbacks", String(published.waterAuthoredObjectShadowFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-object-mesh-shadow-passes", String(published.waterAuthoredObjectMeshShadowPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-object-mesh-shadow-fallbacks", String(published.waterAuthoredObjectMeshShadowFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-shadow-fallback-passes", String(published.waterObjectShadowFallbackPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-shadow-fallback-missing-objects", String(published.waterObjectShadowFallbackMissingObjects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-shadow-fallback-missing-resources", String(published.waterObjectShadowFallbackMissingResources || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-reflection-systems", String(published.waterReflectionSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-refraction-systems", String(published.waterRefractionSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-object-optics-systems", String(published.waterObjectOpticsSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-pool-passes", String(published.waterPoolPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-pool-draw-calls", String(published.waterPoolDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-pool-draw-vertices", String(published.waterPoolDrawVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-pool-tile-texture-loaded", String(published.waterPoolTileTextureLoaded || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-pool-tile-texture-fallbacks", String(published.waterPoolTileTextureFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-pool-tile-texture-pending", String(published.waterPoolTileTexturePending || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-pool-tile-texture-failed", String(published.waterPoolTileTextureFailed || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-pool-passes", String(published.waterAuthoredPoolPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-pool-vertex-passes", String(published.waterAuthoredPoolVertexPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-pool-fragment-passes", String(published.waterAuthoredPoolFragmentPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-pool-fallbacks", String(published.waterAuthoredPoolFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-pool-passes", String(published.waterSelenaPoolPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-pool-fallbacks", String(published.waterSelenaPoolFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-surface-passes", String(published.waterSelenaSurfacePasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-surface-fallbacks", String(published.waterSelenaSurfaceFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-caustic-passes", String(published.waterSelenaCausticPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-caustic-fallbacks", String(published.waterSelenaCausticFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-object-shadow-passes", String(published.waterSelenaObjectShadowPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-object-shadow-fallbacks", String(published.waterSelenaObjectShadowFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-object-mesh-shadow-passes", String(published.waterSelenaObjectMeshShadowPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-selena-object-mesh-shadow-fallbacks", String(published.waterSelenaObjectMeshShadowFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-draw-entries", String(published.waterDrawEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-draw-vertices", String(published.waterDrawVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-surface-mesh-resolution", String(published.waterSurfaceMeshResolution || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-surface-above-draw-calls", String(published.waterSurfaceAboveDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-surface-above-draw-vertices", String(published.waterSurfaceAboveDrawVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-surface-below-draw-calls", String(published.waterSurfaceBelowDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-surface-below-draw-vertices", String(published.waterSurfaceBelowDrawVertices || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-surface-systems", String(published.waterAuthoredSurfaceSystems || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-surface-draw-calls", String(published.waterAuthoredSurfaceDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-surface-vertex-draw-calls", String(published.waterAuthoredSurfaceVertexDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-surface-pending-draw-calls", String(published.waterAuthoredSurfacePendingDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-surface-fallbacks", String(published.waterAuthoredSurfaceFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-authored-surface-fallback-reason", String(published.waterAuthoredSurfaceFallbackReason || ""));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-sky-cube-texture-loaded", String(published.waterSkyCubeTextureLoaded || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-sky-cube-texture-fallbacks", String(published.waterSkyCubeTextureFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-sky-cube-texture-pending", String(published.waterSkyCubeTexturePending || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-water-sky-cube-texture-failed", String(published.waterSkyCubeTextureFailed || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-mesh-objects", String(published.meshObjects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-mesh-draw-calls", String(published.meshDrawCalls || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-mesh-view-culled", String(published.meshViewCulled || 0));
    mirrorWebGPUSkinAttributes(telemetryState, published);
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-instanced-meshes", String(published.instancedMeshes || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-instanced-instances", String(published.instancedInstances || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-line-entries", String(published.lineEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-surface-entries", String(published.surfaceEntries || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-post-effects", String(published.postEffects || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-post-ssao-passes", String(published.postSSAOPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-post-dof-passes", String(published.postDOFPasses || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-custom-material-fallbacks", String(published.customMaterialFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-custom-wgsl-fallbacks", String(published.customWGSLFallbacks || 0));
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-custom-uniform-fallbacks", String(published.customUniformFallbacks || 0));
    if (published.customMaterialFallbacks > 0) {
      queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-custom-material-fallback-reason", "custom-wgsl-hooks-unsupported");
    } else {
      queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-custom-material-fallback-reason", null);
    }
    // Render-truth surface: backend-neutral attribute names both renderers
    // write, so probes and deploy gates never branch on which backend won.
    // Gated on the diagnostics tier -- when it is off nothing here runs and
    // production pays a single boolean read per diagnostic interval.
    var truthApi = renderTruth();
    if (truthApi.enabled()) {
      truthApi.publish(telemetryState.sink, {
        backend: "webgpu",
        postChain: published.postChain,
        meshSubmitted: published.meshObjects || 0,
        meshDrawn: published.meshDrawCalls || 0,
        meshViewCulled: published.meshViewCulled || 0,
        meshUndrawable: published.meshUndrawable || 0,
        pointsSubmitted: published.pointEntries || 0,
        pointsDrawn: published.pointDrawEntries || 0,
        pointInstancesSubmitted: published.pointInstances || 0,
        pointInstancesDrawn: published.pointDrawInstances || 0,
        uniformTime: telemetryState.draw.selenaState.frame.time,
        adapterInfo: webGPUAdapterInfoSnapshot(),
      });
    }
    // Cull survivor telemetry: written when __gosx_scene3d_cull_telemetry is
    // enabled; removed otherwise so the attribute is absent in production.
    if (telemetryState.draw.lastCullSurvivors !== null) {
      queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-cull-survivors", telemetryState.draw.lastCullSurvivors);
    } else {
      queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-cull-survivors", null);
    }
    mirrorWebGPUAttributes(telemetryState, mount);      } finally { telemetryState.webGPUAttributeFormatting = false; }
  }

  function reportWebGPUFrameError(telemetryState: any, message) {
    var text = String(message || "").slice(0, 500);
    if (!text) return;
    // Unlike webGPUErrorReportCount below (capped at 3 lifetime emits so a
    // persistently-erroring session doesn't spam window.__gosx_emit
    // forever), webGPUConsecutiveFrameErrors is UNCAPPED and reset to 0 on
    // the next clean frame (see endWebGPUErrorScope) — it's what the
    // mount-level resilience watchdog polls via diagnostics().frameErrorStreak
    // to decide when to demote (tear down post-FX) and, if that doesn't
    // help, fall back to WebGL.
    telemetryState.webGPUConsecutiveFrameErrors += 1;
    // Any error frame breaks a clean streak, however long — the mount
    // must not restore post-FX on a scene that is still failing.
    telemetryState.webGPUConsecutiveCleanFrames = 0;
    telemetryState.webGPULastError = text;
    var stats = telemetryState.lastWebGPUFrameStats || telemetryState.spareFrameStats;
    stats.lastError = text;
    stats.frameErrorStreak = telemetryState.webGPUConsecutiveFrameErrors;
    queueWebGPUAttribute(telemetryState, "data-gosx-scene3d-webgpu-last-error", text);
    mirrorWebGPUFrameAttributes(telemetryState);
    if (telemetryState.webGPUErrorReportCount >= 3) return;
    telemetryState.webGPUErrorReportCount += 1;
    try {
      if (typeof window !== "undefined" && typeof window.__gosx_emit === "function") {
        window.__gosx_emit("error", "scene3d-webgpu", "render-error", {
          error: text,
          pointEntries: stats.pointEntries || 0,
          pointInstances: stats.pointInstances || 0,
          pointDrawEntries: stats.pointDrawEntries || 0,
          pointDrawInstances: stats.pointDrawInstances || 0,
          computeParticleDrawInstances: stats.computeParticleDrawInstances || 0,
        });
      }
    } catch (_err) {}
  }

  function beginWebGPUErrorScope(telemetryState: any, device: any) {
    if (!device || telemetryState.pendingWebGPUErrorScope || typeof device.pushErrorScope !== "function") return false;
    try {
      device.pushErrorScope("validation");
      telemetryState.pendingWebGPUErrorScope = true;
      return true;
    } catch (_err) {
      telemetryState.pendingWebGPUErrorScope = false;
      return false;
    }
  }

  function endWebGPUErrorScope(telemetryState: any, device: any, submitted: boolean = false) {
    if (!device || !telemetryState.pendingWebGPUErrorScope || typeof device.popErrorScope !== "function") return;
    telemetryState.pendingWebGPUErrorScope = false;
    try {
      device.popErrorScope().then(function(error) {
        if (error) {
          telemetryState.pipelineGuard.uncaptured({ error: error });
        } else if (submitted) {
          telemetryState.webGPUConsecutiveFrameErrors = 0;
          telemetryState.webGPUConsecutiveCleanFrames += 1;
          telemetryState.webGPULastError = "";
          if (telemetryState.lastWebGPUFrameStats && telemetryState.lastWebGPUFrameStats.lastError) {
            telemetryState.lastWebGPUFrameStats.lastError = undefined;
            telemetryState.lastWebGPUFrameStats.frameErrorStreak = undefined;
            mirrorWebGPUFrameAttributes(telemetryState);
          }
        }
      }).catch(function(error) {
        var message = error && error.message ? error.message : String(error);
        if (isWebGPUErrorScopeLifecycleMessage(message)) return;
        reportWebGPUFrameError(telemetryState, message);
      });
    } catch (error) {
      var message = error && error.message ? error.message : String(error);
      if (isWebGPUErrorScopeLifecycleMessage(message)) return;
      reportWebGPUFrameError(telemetryState, message);
    }
  }
  function sceneWebGPUFactoryFailure(canvas: any, reason) {
    var text = String(reason || "unknown");
    try {
      if (typeof window !== "undefined") {
        var rect = canvas && typeof canvas.getBoundingClientRect === "function" ? canvas.getBoundingClientRect() : null;
        window.__gosx_scene3d_webgpu_factory_error = text;
        window.__gosx_scene3d_webgpu_factory_context = {
          reason: text,
          canvasChildren: canvas && canvas.childNodes ? canvas.childNodes.length : -1,
          canvasParent: canvas && canvas.parentNode && canvas.parentNode.tagName ? canvas.parentNode.tagName : "",
          canvasWidth: canvas && Number(canvas.width) || 0,
          canvasHeight: canvas && Number(canvas.height) || 0,
          canvasConnected: !!(canvas && canvas.isConnected),
          canvasRectWidth: rect ? Number(rect.width) || 0 : 0,
          canvasRectHeight: rect ? Number(rect.height) || 0 : 0,
        };
      }
    } catch (_err) {}
    console.warn("[gosx] WebGPU factory unavailable:", text);
    return null;
  }
  function rendererDeviceStillActive(device: any, scopedDevice) {
    return !!device && device === scopedDevice;
  }
  function sceneWebGPUWaterDebugMode(canvas: any) {
    var raw = "";
    try {
      if (typeof window !== "undefined" && window.location && window.location.search && typeof URLSearchParams === "function") {
        raw = new URLSearchParams(window.location.search).get("gosx-water-debug") || "";
      }
    } catch (_err) {}
    if (!raw && canvas && typeof canvas.getAttribute === "function") {
      raw = canvas.getAttribute("data-gosx-scene3d-water-debug") || "";
    }
    return String(raw || "").trim().toLowerCase();
  }
  // Synchronous device initialization — device was already obtained
  // by the main-bundle probe (16z). Previously this was a two-stage
  // async sequence (requestAdapter → requestDevice → set up GPU
  // resources), but the probe now owns the adapter+device lifecycle
  // so we can do all the GPU-resource setup synchronously at factory
  // construction time, ensuring the renderer is never returned in a
  // half-initialized state.
  //
  // startInit is retained as a no-op for the existing call site in
  // render() ("if (!device) startInit()") to keep the diff tight;
  // the first render call falls straight through since device is
  // already set.
  function startInit() { /* no-op: device already initialized */ }
  function prepareBundlePipelines(s: any, device: any, bundle: any, prepared: any, effects: any[], post: boolean, warmup?: any) { if (warmup) prepareBundlePipelines(s, device, warmup, warmup.prepared, [], false); // Props.ShaderWarmup variants (pipeline-warmup.ts)
    var materials = bundle.materials || [];
    var instancedVariants = (bundle.instancedMeshes || []).map(function(mesh: any) {
      var material = instancedMeshMaterial(mesh, materials), blend = scenePBRObjectRenderPass(mesh, material);
      return [blend, sceneTransmissionDepthWrite(mesh, material, blend === "opaque"), Boolean(material && material.detail), instancedMeshUsesCull(mesh)].join(":");
    }).sort().join(",");
    var key = prepared ? [prepared.pipelineVariantHash, prepared.materialsHash, prepared.shadowPassHash, instancedVariants, (bundle.instancedMeshes || []).length, (bundle.waterSystems || []).length, s.pipeState.targetFormat, s.pipeState.activeSampleCount, s.pipeState.selectiveBloom, post, effects.map(function(effect: any) { return effect.kind + ":" + effect.mode; }).join(",")].join("|") : "";
    if (key && key === s.preparedPipelineKey) return;
    var passes = prepared && prepared.pbrPasses || buildDrawList(bundle);
    var shadows = Array.isArray(bundle.lights) && bundle.lights.some(function(light: any) { return light.castShadow; });
    s.pipeState.pbrShadowsActive = shadows && bundle.lights.some(wgpuShadowCaster);
    if (shadows) getShadowPipeline(s.pipeState, device, false);
    for (var blend of ["opaque", "alpha", "additive"]) {
      var objects = passes[blend] || [];
      var depth = blend === "opaque";
      for (var object of objects) {
        var material = materials[object.materialIndex];
        var reflected = sceneWebGPUObjectReflected(object);
        if (!getObjectSelenaPipeline(function(m: any, b: any, d: any, o: any) { return getSelenaPipeline(s.pipeState, s.selenaState, device, m, b, d, o); }, object, material, blend, depth)) getPBRPipeline(s.pipeState, device, blend, sceneTransmissionDepthWrite(object, material, depth), reflected ? "cw" : "ccw", pbrVariant(s.pipeState, s.gpuRes, device, material));
        if (shadows && object.castShadow) getShadowPipeline(s.pipeState, device, reflected);
        if (webGPUObjectIsSkinned(object)) ensureElioSkinPipeline(s.pipeState, s.meshState, device);
        if (object.computedMorph) ensureComputedMorphPipeline(s.pipeState, s.meshState, device);
      }
    }
    for (var mesh of bundle.instancedMeshes || []) {
      var material = instancedMeshMaterial(mesh, materials);
      var meshBlend = scenePBRObjectRenderPass(mesh, material);
      var meshDepth = sceneTransmissionDepthWrite(mesh, material, meshBlend === "opaque"), meshDetail = pbrVariant(s.pipeState, s.gpuRes, device, material);
      getPBRInstancedPipeline(s.pipeState, device, meshBlend, meshDepth, meshDetail);
      if (instancedMeshUsesCull(mesh)) getPBRInstancedCullPipeline(s.pipeState, device, meshBlend, meshDepth, meshDetail === true);
      if (shadows && mesh.castShadow) getShadowInstancedPipeline(s.pipeState, device);
    }
    if (bundle.waterSystems && bundle.waterSystems.length) ensureWaterPipelines(s.waterState, s.pipeState, device);
    if (post) {
      if (!s.postProcessor) s.postProcessor = wgpuCreatePostProcessor(device, s.telemetryState.presentationFormat, function(message: any) { reportWebGPUFrameError(s.telemetryState, message); }, function(material: any, owner: any, context: any) {
        return sceneSelenaUniformData(material, owner, context, s.selenaState.frame);
      });
      s.postProcessor.preparePipelines(effects, s.pipeState.activeSampleCount);
    }
    s.initialPipelinesPrepared = true;
    s.preparedPipelineKey = key;
  }
  function supportsBundle(bundle) {
    if (webGPUUnsupportedLineStyles(bundle)) {
      return false;
    }
    if (!webGPUCanUseThickWorldLines(bundle)) {
      return false;
    }
    return true;
  }
  // disablePostProcessing: the frame-error resilience "demote" step (see
  // the mount's checkSceneWebGPUFrameErrorWatchdog). Tears down the
  // post-FX chain's GPU resources (HDR scene target, bloom ping-pong,
  // depth, cached pipelines/bind groups — everything wgpuCreatePostProcessor
  // owns) and permanently stops rebuilding them for this renderer instance,
  // so a scene whose post-FX allocation/validation is persistently failing
  // (memory-tight browser, poisoned target) falls back to raw rendering
  // instead of resubmitting the same broken frame forever. Idempotent —
  // returns false if already demoted (nothing to do).
  function disablePostProcessing(s: any) {
    if (s.postFXForceDisabled) return false;
    s.postFXForceDisabled = true;
    if (s.postProcessor) {
      try { s.postProcessor.dispose(); s.specularTarget.dispose(); } catch (_err) {}
      s.postProcessor = null;
    }
    // Give raw rendering a fresh error-streak window: if post-FX really
    // was the problem, this resets the counter the fallback threshold
    // compares against, so the mount only escalates to a full backend
    // swap if raw rendering ALSO keeps failing.
    s.telemetryState.webGPUConsecutiveFrameErrors = 0;
    return true;
  }
  // enablePostProcessing: the frame-error resilience "restore" step — the
  // way back that disablePostProcessing (above) never had. Called by
  // the mount's checkSceneWebGPUFrameErrorResilience once a
  // demoted renderer has produced enough consecutive clean frames (see
  // diagnostics().frameCleanStreak). Clears postFXForceDisabled so the
  // very next render() call with a non-empty bundle.postEffects rebuilds
  // the post-FX chain itself (usePostProcessing below, and the lazy
  // `if (!postProcessor) postProcessor = wgpuCreatePostProcessor(...)`
  // in render()) — no GPU resource construction happens here; this
  // function only flips the gate. Idempotent — returns false if post-FX
  // is not currently force-disabled (nothing to do).
  function enablePostProcessing(s: any) {
    if (!s.postFXForceDisabled || s.pipelineGuard.disabled.has("post")) return false;
    s.postFXForceDisabled = false;
    return true;
  }
