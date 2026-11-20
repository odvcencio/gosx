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
