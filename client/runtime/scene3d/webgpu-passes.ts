  // Per-pass GPU timing.
  // These optional pass counters describe shadow and main draws only.
  // The frame timer also includes compute, water, picking and post effects.
  // Slot layout, per ring entry:
  //   0 shadow begin   1 shadow end   2 main begin   3 main end
  const SCENE_WEBGPU_PASS_STAMPS = 4;
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

  function beginGPUFrameTiming(timingState: any, device: any, encoder) {
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

  // endGPUPassTimingFrame resolves the slot into its readback buffer. Call it
  // once, after the last timed pass and before submit.
  function endGPUPassTimingFrame(timingState: any, encoder) {
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

  function uploadLights(uniformState: any, gpuRes: any, device: any, lights: any) {
    var lightArray = Array.isArray(lights) ? lights : [];
    ensureLightCapacity(uniformState, gpuRes, device, sceneWebGPULightCapacityFor(lightArray.length));
    var count = Math.min(lightArray.length, uniformState._lightCapacity);

    // Write lightCount into frame uniform buffer at byte offset 140.
    uniformState._lightCountBuf[0] = count;
    device.queue.writeBuffer(gpuRes.frameUniformBuffer, 140, uniformState._lightCountBuf);

    var census = sceneWebGPUPackLights(lightArray, count, uniformState._lightDataF, uniformState._lightColorCache);
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
    var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);

    // FogUniforms: vec3f fogColor(12) + f32 density(4) + u32 hasFog(4) + pad(12) = 32 bytes.
    var f = uniformState._fogUniformF;
    var u = uniformState._fogUniformU;
    f[0] = fogColorRGBA[0];
    f[1] = fogColorRGBA[1];
    f[2] = fogColorRGBA[2];
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
    var groundColorRGBA = sceneColorRGBA(env.groundColor, [0.12, 0.16, 0.22, 1]);
    // 80-byte EnvUniforms, matching WGSL.
    var data = uniformState._envUniformF;
    var words = uniformState._envUniformU;
    data[0] = ambientColorRGBA[0]; data[1] = ambientColorRGBA[1]; data[2] = ambientColorRGBA[2];
    data[3] = sceneNumber(env.ambientIntensity, 0);
    data[4] = skyColorRGBA[0]; data[5] = skyColorRGBA[1]; data[6] = skyColorRGBA[2];
    data[7] = sceneNumber(env.skyIntensity, 0);
    data[8] = groundColorRGBA[0]; data[9] = groundColorRGBA[1]; data[10] = groundColorRGBA[2];
    data[11] = sceneNumber(env.groundIntensity, 0);
    data[12] = Math.max(0, sceneNumber(env.envIntensity, 1));
    data[13] = sceneNumber(env.envRotation, 0);
    words[14] = ibl.active ? 1 : 0;
    words[15] = ibl.active ? Math.max(1, ibl.diagnostics.radianceMipLevels | 0) : 0;
    words[16] = envMap.active ? 1 : 0;
    data[17] = wgpuEnvMapMaxLod(envMap);
    words[18] = 0; words[19] = 0;
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
