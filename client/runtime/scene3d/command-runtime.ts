// command-runtime.ts — Scene3D command application host.
// @ts-check

/**
 * @typedef {object} GoSXScene3DCommandRecord
 * @property {object} handle
 * @property {Element|null} mount
 */

(function() {
  if (typeof window === "undefined" || window.__gosx_scene3d_command_bridge) return;
  var revision = 0;
  var selector = 'script[type="application/json"][data-gosx-scene-commands]';
  var poseFields = ["x", "y", "z", "rotationX", "rotationY", "rotationZ", "scaleX", "scaleY", "scaleZ", "animation", "animationTime", "animationLoop"];
  var poseQueues = new Map();

  function key(target, options) {
    if (options && typeof options.engineID === "string" && options.engineID.trim()) return options.engineID.trim();
    if (typeof target === "string" && target.trim()) return target.trim();
    if (target && typeof target.id === "string" && target.id.trim()) return target.id.trim();
    return "";
  }

  function ready(handle) {
    return Boolean(handle && handle.__gosxScene3DCommandReady === true && typeof handle.applyCommands === "function");
  }

  // GSP2 and GSP3 share strict framing, IDs and scalar validation. Format
  // fields remain separate, and nothing reaches the renderer until decoding ends.
  function decodeInstanceFrame(input: any, motion: boolean) {
    var kind = "Scene3D " + (motion ? "motion" : "pose");
    var bytes = input instanceof ArrayBuffer ? new Uint8Array(input) : input;
    if (!(bytes instanceof Uint8Array)) throw new TypeError(kind + " frame must be an ArrayBuffer or Uint8Array");
    var view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    var decoder = new TextDecoder("utf-8", { fatal: true });
    var offset = 0;
    function need(size: number) {
      if (size > view.byteLength - offset) throw new RangeError("truncated " + kind + " frame");
    }
    function u16() { need(2); var value = view.getUint16(offset, true); offset += 2; return value; }
    function id() {
      var length = u16();
      if (!length) throw new TypeError("empty " + kind + " ID");
      need(length);
      var value = decoder.decode(bytes.subarray(offset, offset + length));
      offset += length;
      return value;
    }
    function number() {
      need(4);
      var value = view.getFloat32(offset, true);
      offset += 4;
      if (!Number.isFinite(value)) throw new TypeError("non-finite " + kind + " value");
      return value;
    }
    need(6);
    if (view.getUint8(0) !== 71 || view.getUint8(1) !== 83 || view.getUint8(2) !== 80 || view.getUint8(3) !== (motion ? 51 : 50)) {
      throw new TypeError("unsupported " + kind + " frame version");
    }
    offset = 4;
    var clipCount = u16();
    var clips = [""];
    var clipIDs = new Set();
    for (var c = 0; c < clipCount; c++) {
      var clip = id();
      if (clipIDs.has(clip)) throw new TypeError("duplicate Scene3D animation clip");
      clipIDs.add(clip);
      clips.push(clip);
    }
    var count = u16();
    var batches = [];
    var batchIDs = new Set();
    for (var i = 0; i < count; i++) {
      var batchID = id();
      if (batchIDs.has(batchID)) throw new TypeError("duplicate " + kind + " batch ID");
      batchIDs.add(batchID);
      var instanceCount = u16();
      var instances = [];
      var instanceIDs = new Set();
      for (var j = 0; j < instanceCount; j++) {
        var instanceID = id();
        if (instanceIDs.has(instanceID)) throw new TypeError("duplicate " + kind + " instance ID");
        instanceIDs.add(instanceID);
        var instance: any;
        if (motion) {
          instance = {
            id: instanceID,
            prevX: number(), prevY: number(), prevZ: number(),
            prevRotationX: number(), prevRotationY: number(), prevRotationZ: number(),
            prevScaleX: number(), prevScaleY: number(), prevScaleZ: number(),
            tPrev: number(),
            nextX: number(), nextY: number(), nextZ: number(),
            nextRotationX: number(), nextRotationY: number(), nextRotationZ: number(),
            nextScaleX: number(), nextScaleY: number(), nextScaleZ: number(),
            tNext: number(),
            animation: "", clipStartTime: 0, animationLoop: false, playbackRate: 1,
          };
          if (!(instance.tNext > instance.tPrev)) throw new RangeError("Scene3D motion frame requires tNext after tPrev");
        } else {
          instance = { id: instanceID, x: number(), y: number(), z: number(), rotationX: number(), rotationY: number(), rotationZ: number(), scaleX: number(), scaleY: number(), scaleZ: number(), animationTime: number(), animation: "", animationLoop: false };
          if (instance.animationTime < 0) throw new TypeError("negative Scene3D animation time");
        }
        var clipIndex = u16();
        if (clipIndex >= clips.length) throw new RangeError("unknown Scene3D animation clip index");
        instance.animation = clips[clipIndex];
        if (motion) instance.clipStartTime = number();
        need(1);
        var loop = view.getUint8(offset++);
        if (loop > 1) throw new TypeError("invalid Scene3D animation loop flag");
        instance.animationLoop = loop === 1;
        if (motion) instance.playbackRate = number();
        instances.push(instance);
      }
      batches.push({ id: batchID, instances: instances });
    }
    if (offset !== view.byteLength) throw new RangeError("trailing " + kind + " frame data");
    return batches;
  }

  function decodePoseFrame(input) { return decodeInstanceFrame(input, false); }

  function record(target, options) {
    if (ready(target)) return { handle: target, mount: null };
    if (target && ready(target.__gosxScene3DHandle)) return { handle: target.__gosxScene3DHandle, mount: target };
    var id = key(target, options || {});
    /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ var mount = id && document && typeof document.getElementById === "function" ? document.getElementById(id) : null;
    if (mount && ready(mount.__gosxScene3DHandle)) return { handle: mount.__gosxScene3DHandle, mount: mount };
    var engine = id && window.__gosx && window.__gosx.engines && typeof window.__gosx.engines.get === "function" ? window.__gosx.engines.get(id) : null;
    return engine && ready(engine.handle) ? { handle: engine.handle, mount: engine.mount || mount || null } : null;
  }

  function setAttr(mount, name, value) {
    if (mount && typeof mount.setAttribute === "function") mount.setAttribute(name, String(value));
  }

  function apply(rec, commands, rev) {
    setAttr(rec.mount, "data-gosx-scene3d-command-revision", rev);
    return Promise.resolve(rec.handle.applyCommands(commands)).then(function() {
      setAttr(rec.mount, "data-gosx-scene3d-command-applied-revision", rev);
      return { revision: rev, applied: true };
    });
  }

  // Binary frame paths share readiness and timeout handling. Validation and
  // fallback remain in their format-specific callers.
  function waitForCommandMount(target: any, opts: any, format: string, apply: (rec: any, resolve: (value: any) => void, reject: (error: unknown) => void) => unknown) {
    var id = key(target, opts);
    var deadline = Date.now() + Math.max(0, Math.floor(Number(opts.timeoutMS) || 10000));
    return new Promise(function poll(resolve, reject) {
      var rec = record(target, opts);
      if (rec) return apply(rec, resolve, reject);
      if (!id) return reject(new Error("Scene3D " + format + " target is not ready and has no stable id"));
      if (Date.now() >= deadline) return reject(new Error("Scene3D " + format + " target did not become ready: " + id));
      setTimeout(function() { poll(resolve, reject); }, 16);
    });
  }

  function dispatchCommands(target, commands, options) {
    if (!Array.isArray(commands)) return Promise.reject(new TypeError("Scene3D commands must be an array"));
    var opts = options || {};
    var id = key(target, opts);
    var deadline = Date.now() + Math.max(0, Math.floor(Number(opts.timeoutMS) || 10000));
    var rev = ++revision;
    function poll(resolve, reject) {
      var rec = record(target, opts);
      if (rec) return apply(rec, commands, rev).then(resolve, reject);
      if (!id) return reject(new Error("Scene3D command target is not ready and has no stable id"));
      if (Date.now() >= deadline) return reject(new Error("Scene3D command target did not become ready: " + id));
      setTimeout(function() { poll(resolve, reject); }, 16);
    }
    return new Promise(poll);
  }

  const sceneAPI = window.__gosx_scene3d_api || (window.__gosx_scene3d_api = {});

  const presentationLoads: Record<string, Promise<any>> = {};
  function loadPresentation(kind: string, datasetKey: string) {
    if (sceneAPI[kind]) return Promise.resolve(sceneAPI[kind]);
    return presentationLoads[kind] || (presentationLoads[kind] = new Promise((resolve, reject) => {
      const tag = document.querySelector('script[data-gosx-script="feature-scene3d"]');
      // @ts-expect-error TS2339 -- the selector matches a script element.
      const url = tag && tag.dataset[datasetKey], name = "Scene3D " + kind + " chunk";
      if (!url) return reject(new Error(name + " URL was not advertised"));
      const script = document.createElement("script");
      script.src = url; script.async = true; script.type = "text/javascript";
      script.crossOrigin = "anonymous"; script.referrerPolicy = "no-referrer";
      // @ts-expect-error TS2339 -- the selector matches a script element.
      script.nonce = tag.nonce;
      script.onload = () => sceneAPI[kind] ? resolve(sceneAPI[kind]) : reject(new Error(name + " did not publish its API"));
      script.onerror = () => reject(new Error("failed to load " + name));
      document.head.appendChild(script);
    }).catch(error => { delete presentationLoads[kind]; throw error; }));
  }

  async function playPresentation(target: any, plan: any, opts: any, kind: string, method: string, datasetKey: string) {
    opts ||= {};
    const id = key(target, opts), deadline = Date.now() + Math.max(0, opts.timeoutMS ?? 10000);
    let rec = record(target, opts);
    while (!rec) {
      if (!id || Date.now() >= deadline) throw new Error("Scene3D " + kind + " target is not ready");
      await new Promise(resolve => setTimeout(resolve, 16));
      rec = record(target, opts);
    }
    if (!rec.mount && typeof rec.handle[method] === "function") return Promise.resolve().then(() => rec.handle[method](plan));
    const mount = rec.mount || Array.from(document.querySelectorAll('[data-gosx-scene3d-command-ready]')).find(function() { return arguments[0].__gosxScene3DHandle === rec.handle; });
    if (!mount) throw new Error("Scene3D " + kind + " mount is unavailable");
    const api = await loadPresentation(kind, datasetKey);
    if (api.load) await api.load();
    return api.attach(plan, mount, rec.handle, () => mount.__gosxScene3DHandle === rec.handle);
  }

  function playTimeline(...args: any[]) {
    return playPresentation(args[0], args[1], args[2], "timeline", "playTimeline", "gosxScene3dTimelineUrl");
  }

  function burstParticles(...args: any[]) {
    return playPresentation(args[0], args[1], args[2], "particle-burst", "burstParticles", "gosxScene3dParticleBurstUrl");
  }

  function dispatchPoseFrame(target, frame, options) {
    var opts = options || {};
    var queueKey = key(target, opts) || target;
    if (!queueKey) return Promise.reject(new Error("Scene3D pose frame target has no stable id"));
    return new Promise(function(resolve, reject) {
      var queue = poseQueues.get(queueKey);
      if (!queue) {
        queue = { running: false, pending: null, stats: null, sequence: 0, progressed: 0, waitingForCommands: false };
        poseQueues.set(queueKey, queue);
      }
      if (queue.pending) {
        var older = queue.pending.opts.beforeCommands;
        var newer = opts.beforeCommands;
        var oldMembership = null;
        if (Array.isArray(older)) for (var i = older.length - 1; i >= 0; i--) {
          if (older[i] && older[i].kind === 11) { oldMembership = older[i]; break; }
        }
        var hasNewMembership = Array.isArray(newer) && newer.some(function(command) { return command && command.kind === 11; });
        if (oldMembership && !hasNewMembership) {
          opts = Object.assign({}, opts, { beforeCommands: [oldMembership].concat(Array.isArray(newer) ? newer : []) });
        }
        poseStats(queue.pending.target, queue.pending.opts).superseded++;
        queue.pending.resolve({ applied: false, binary: false, superseded: true });
      }
      queue.pending = { target: target, frame: frame, opts: opts, resolve: resolve, reject: reject, sequence: ++queue.sequence };
      // Membership commands retain their transaction ordering. While their
      // assets are pending, a mounted renderer may synchronously advance only
      // compatible committed identities; keep the latest job for final replay.
      if (queue.waitingForCommands) {
        var rec = record(target, opts);
        if (rec && typeof rec.handle.applyPendingPoseFrame === "function") {
          try {
            var progress = rec.handle.applyPendingPoseFrame(decodePoseFrame(frame));
            if (progress && progress.applied === true && progress.binary === true) queue.progressed = queue.pending.sequence;
          } catch (_error) { /* The queued transaction remains authoritative. */ }
        }
      }
      if (!queue.running) runPoseQueue(queueKey, queue);
    });
  }

  function poseStats(target, opts) {
    var queue = poseQueues.get(key(target, opts) || target);
    var rec = record(target, opts);
    if (rec && rec.handle.__gosxPoseFrameStats) return rec.handle.__gosxPoseFrameStats;
    var stats = queue && queue.stats || {
      accepted: 0, plannerCallsSkipped: 0, fallback: 0, superseded: 0,
      errors: 0, lastError: "", rejected: Object.create(null),
    };
    if (queue) queue.stats = stats;
    if (rec) rec.handle.__gosxPoseFrameStats = stats;
    return stats;
  }

  function runPoseQueue(queueKey, queue) {
    var job = queue.pending;
    if (!job) { queue.running = false; poseQueues.delete(queueKey); return; }
    queue.pending = null;
    queue.running = true;
    var operation;
    try {
      queue.waitingForCommands = Array.isArray(job.opts.beforeCommands);
      operation = queue.waitingForCommands
        ? dispatchCommands(job.target, job.opts.beforeCommands, job.opts).then(function() {
          queue.waitingForCommands = false;
          // A newer compatible pose already reached the committed wrappers.
          // Replaying this captured frame would roll them back after loading.
          return queue.progressed > job.sequence
            ? { applied: false, binary: true, superseded: true }
            : dispatchPoseFrameNow(job.target, job.frame, job.opts);
        })
        : dispatchPoseFrameNow(job.target, job.frame, job.opts);
    } catch (error) {
      operation = Promise.reject(error);
    }
    Promise.resolve(operation).then(function(result) {
      if (result && result.applied && result.binary === false) poseStats(job.target, job.opts).fallback++;
      job.resolve(result);
      runPoseQueue(queueKey, queue);
    }, function(error) {
      var stats = poseStats(job.target, job.opts);
      stats.errors++;
      stats.lastError = String(error && error.message || error);
      var rec = record(job.target, job.opts);
      var eventTarget = rec && rec.mount || window;
      if (typeof CustomEvent === "function" && eventTarget && typeof eventTarget.dispatchEvent === "function") {
        try { eventTarget.dispatchEvent(new CustomEvent("gosx:scene3d:pose-frame-error", { detail: { reason: stats.lastError } })); } catch (_error) {}
      }
      queue.waitingForCommands = false;
      job.reject(error);
      runPoseQueue(queueKey, queue);
    });
  }

  function dispatchPoseFrameNow(target, frame, opts) {
    function fallback(error) {
      if (!Array.isArray(opts.fallbackCommands)) throw error;
      return dispatchCommands(target, opts.fallbackCommands, opts).then(function(result) {
        return { applied: true, binary: false, fallbackReason: String(error && error.message || error), commandResult: result };
      });
    }
    var batches;
    try { batches = decodePoseFrame(frame); } catch (error) { return Promise.resolve().then(function() { return fallback(error); }); }
    return waitForCommandMount(target, opts, "pose frame", function(rec, resolve, reject) {
      poseStats(target, opts);
      if (typeof rec.handle.applyPoseFrame !== "function") return reject(new Error("Scene3D pose frames are unsupported by this mount"));
      return Promise.resolve().then(function() { return rec.handle.applyPoseFrame(batches); }).then(resolve, reject);
    }).catch(fallback);
  }

  function applyMountedPoseFrame(state, batches, updateRigidPoses, scheduleRender, handle) {
    var stats = handle.__gosxPoseFrameStats || (handle.__gosxPoseFrameStats = { accepted: 0, plannerCallsSkipped: 0, fallback: 0, superseded: 0, errors: 0, lastError: "", rejected: Object.create(null) });
    function reject(reason) {
      stats.rejected[reason] = (stats.rejected[reason] || 0) + 1;
      throw new Error("Scene3D pose frame rejected: " + reason);
    }
    if (!Array.isArray(batches)) reject("invalid-frame");
    var pending = Boolean(state._modelHydrationPromise || state._modelHydrationUncommitted);
    if (!state._hydratedModelRecords) {
      if (pending) return { applied: false, binary: true, pending: true };
      reject("renderer-not-ready");
    }
    var mounted = Array.isArray(state.instancedGLBMeshes) ? state.instancedGLBMeshes : [];
    var targets = [];
    var selected = pending ? [] : batches;
    for (var batch of batches) {
      var current = mounted.find(function(candidate) { return candidate.id === batch.id; });
      if (!Array.isArray(batch.instances)) reject("membership-changed");
      if (pending) {
        // The next queued declaration can add or reorder members while this
        // transaction loads. Advance only identities in its current snapshot;
        // their committed template, scope and wrapper are checked by the renderer.
        if (!current) continue;
        var instances = [];
        var poses = [];
        var currentByID = new Map();
        for (var instance of current.instances) currentByID.set(instance.id, instance);
        for (var pose of batch.instances) {
          var instance = currentByID.get(pose.id);
          if (!instance) continue;
          instances.push(instance);
          poses.push(pose);
        }
        if (poses.length) {
          targets.push({ instances: instances });
          selected.push({ id: batch.id, instances: poses });
        }
      } else {
        if (!current || current.instances.length !== batch.instances.length) reject("membership-changed");
        for (var index = 0; index < batch.instances.length; index++) {
          if (current.instances[index].id !== batch.instances[index].id) reject("membership-order-changed");
        }
        targets.push(current);
      }
    }
    batches = selected;
    // Reuse one flat rollback buffer across frames. No per-instance maps,
    // patch objects, or snapshot arrays are created on the accepted path.
    var previous = handle.__gosxPosePrevious || (handle.__gosxPosePrevious = []);
    var offset = 0;
    for (var batchIndex = 0; batchIndex < batches.length; batchIndex++) {
      for (var instanceIndex = 0; instanceIndex < batches[batchIndex].instances.length; instanceIndex++) {
        var instance = targets[batchIndex].instances[instanceIndex];
        var pose = batches[batchIndex].instances[instanceIndex];
        for (var field of poseFields) {
          previous[offset++] = instance[field];
          instance[field] = pose[field];
        }
      }
    }
    previous.length = offset;
    var retained = false;
    try { retained = updateRigidPoses(state, null, batches); } catch (_error) { retained = false; }
    if (!retained) {
      offset = 0;
      for (var batchIndex = 0; batchIndex < batches.length; batchIndex++) {
        for (var instanceIndex = 0; instanceIndex < batches[batchIndex].instances.length; instanceIndex++) {
          var instance = targets[batchIndex].instances[instanceIndex];
          for (var field of poseFields) instance[field] = previous[offset++];
        }
      }
      reject("retained-pose-unavailable");
    }
    stats.accepted++;
    stats.plannerCallsSkipped++;
    scheduleRender("pose-frame");
    return { applied: true, binary: true };
  }

  // GPU-driven crowd motion retains its own queues and mount validation.
  var motionQueues = new Map();

  function decodeMotionFrame(input) { return decodeInstanceFrame(input, true); }

  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function dispatchMotionFrame(target, frame, options) {
    var opts = options || {};
    var queueKey = key(target, opts) || target;
    if (!queueKey) return Promise.reject(new Error("Scene3D motion frame target has no stable id"));
    return new Promise(function(resolve, reject) {
      var queue = motionQueues.get(queueKey);
      if (!queue) {
        queue = { running: false, pending: null, stats: null };
        motionQueues.set(queueKey, queue);
      }
      if (queue.pending) {
        var older = queue.pending.opts.beforeCommands;
        var newer = opts.beforeCommands;
        var oldMembership = null;
        if (Array.isArray(older)) for (var i = older.length - 1; i >= 0; i--) {
          if (older[i] && older[i].kind === 11) { oldMembership = older[i]; break; }
        }
        var hasNewMembership = Array.isArray(newer) && newer.some(function(command) { return command && command.kind === 11; });
        if (oldMembership && !hasNewMembership) {
          opts = Object.assign({}, opts, { beforeCommands: [oldMembership].concat(Array.isArray(newer) ? newer : []) });
        }
        motionStats(queue.pending.target, queue.pending.opts).superseded++;
        queue.pending.resolve({ applied: false, binary: false, superseded: true });
      }
      queue.pending = { target: target, frame: frame, opts: opts, resolve: resolve, reject: reject };
      if (!queue.running) runMotionQueue(queueKey, queue);
    });
  }

  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function motionStats(target, opts) {
    var queue = motionQueues.get(key(target, opts) || target);
    var rec = record(target, opts);
    if (rec && rec.handle.__gosxMotionFrameStats) return rec.handle.__gosxMotionFrameStats;
    var stats = queue && queue.stats || {
      accepted: 0, superseded: 0, errors: 0, lastError: "", rejected: Object.create(null),
    };
    if (queue) queue.stats = stats;
    if (rec) rec.handle.__gosxMotionFrameStats = stats;
    return stats;
  }

  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function runMotionQueue(queueKey, queue) {
    var job = queue.pending;
    if (!job) { queue.running = false; motionQueues.delete(queueKey); return; }
    queue.pending = null;
    queue.running = true;
    var operation;
    try {
      operation = Array.isArray(job.opts.beforeCommands)
        ? dispatchCommands(job.target, job.opts.beforeCommands, job.opts).then(function() { return dispatchMotionFrameNow(job.target, job.frame, job.opts); })
        : dispatchMotionFrameNow(job.target, job.frame, job.opts);
    } catch (error) {
      operation = Promise.reject(error);
    }
    Promise.resolve(operation).then(function(result) {
      if (result && typeof result === "object" && "applied" in result && "binary" in result && result.applied && result.binary === false) {
        motionStats(job.target, job.opts).fallback = (motionStats(job.target, job.opts).fallback || 0) + 1;
      }
      job.resolve(result);
      runMotionQueue(queueKey, queue);
    }, function(error) {
      var stats = motionStats(job.target, job.opts);
      stats.errors++;
      stats.lastError = String(error && error.message || error);
      var rec = record(job.target, job.opts);
      var eventTarget = rec && rec.mount || window;
      if (typeof CustomEvent === "function" && eventTarget && typeof eventTarget.dispatchEvent === "function") {
        try { eventTarget.dispatchEvent(new CustomEvent("gosx:scene3d:motion-frame-error", { detail: { reason: stats.lastError } })); } catch (_error) {}
      }
      job.reject(error);
      runMotionQueue(queueKey, queue);
    });
  }

  // dispatchMotionFrameNow falls back, in order, to
  // options.fallbackPoseFrame (GSP2 bytes, applied through the existing
  // pose-frame path) and then options.fallbackCommands (the JSON command
  // path) -- so a page can adopt MotionFrame per crowd without a hard
  // dependency on the mount already supporting it.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function dispatchMotionFrameNow(target, frame, opts) {
    // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
    function fallbackToCommands(error) {
      if (!Array.isArray(opts.fallbackCommands)) throw error;
      return dispatchCommands(target, opts.fallbackCommands, opts).then(function(result) {
        return { applied: true, binary: false, fallbackReason: String(error && error.message || error), commandResult: result };
      });
    }
    // dispatchPoseFrameNow already tries opts.fallbackCommands itself (the
    // SAME opts this function received) when the pose frame it is handed
    // also fails, so falling back to a PoseFrame is a straight delegation:
    // whatever it resolves or rejects with -- a direct pose success, its own
    // command fallback, or a final rejection -- is this frame's outcome
    // too. This is deliberately NOT wrapped to relabel fallbackReason with
    // the MotionFrame's own failure: the pose (or command) layer's reason is
    // the more specific, more actionable one once the frame gets that far.
    // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
    function fallback(error) {
      if (opts.fallbackPoseFrame) return dispatchPoseFrameNow(target, opts.fallbackPoseFrame, opts);
      return fallbackToCommands(error);
    }
    // @ts-ignore TS7034 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
    var batches;
    try { batches = decodeMotionFrame(frame); } catch (error) { return Promise.resolve().then(function() { return fallback(error); }); }
    return waitForCommandMount(target, opts, "motion frame", function(rec, resolve, reject) {
      motionStats(target, opts);
      if (typeof rec.handle.applyMotionFrame !== "function") return reject(new Error("Scene3D motion frames are unsupported by this mount"));
      return Promise.resolve().then(function() { return rec.handle.applyMotionFrame(batches); }).then(resolve, reject);
    }).catch(fallback);
  }

  // applyMountedMotionFrame validates EVERY batch/instance ID and order
  // against state.instancedGLBMeshes before writing anything (mirroring
  // applyMountedPoseFrame's membership checks), then hands the whole decoded
  // frame to updateRigidMotion (sceneUpdateRigidInstanceMotion in
  // mount-webgl.ts) to resolve and write. Unlike applyMountedPoseFrame, no
  // scalar pose field is copied onto the instance objects here first: a
  // GPU-motion instance's authoritative state lives in
  // object._crowdMotion.record, not in instance.x/y/z/animation*, so there
  // is nothing to roll back -- updateRigidMotion validates every target
  // BEFORE writing any of them (see its doc comment), so a rejected frame
  // changes nothing.
  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function applyMountedMotionFrame(state, batches, updateRigidMotion, scheduleRender, handle) {
    var stats = handle.__gosxMotionFrameStats || (handle.__gosxMotionFrameStats = { accepted: 0, superseded: 0, errors: 0, lastError: "", rejected: Object.create(null) });
    // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
    function reject(reason) {
      stats.rejected[reason] = (stats.rejected[reason] || 0) + 1;
      throw new Error("Scene3D motion frame rejected: " + reason);
    }
    if (!Array.isArray(batches)) reject("invalid-frame");
    if (state._modelHydrationPromise || !state._hydratedModelRecords) reject("renderer-not-ready");
    var mounted = Array.isArray(state.instancedGLBMeshes) ? state.instancedGLBMeshes : [];
    for (var batch of batches) {
      // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
      var current = mounted.find(function(candidate) { return candidate.id === batch.id; });
      if (!current || !Array.isArray(batch.instances) || current.instances.length !== batch.instances.length) reject("membership-changed");
      for (var index = 0; index < batch.instances.length; index++) {
        if (current.instances[index].id !== batch.instances[index].id) reject("membership-order-changed");
      }
    }
    var retained = false;
    try { retained = updateRigidMotion(state, batches); } catch (_error) { retained = false; }
    if (!retained) reject("retained-motion-unavailable");
    stats.accepted++;
    scheduleRender("motion-frame");
    return { applied: true, binary: true };
  }

  function applyCommandScripts(root) {
    if (!root || typeof root.querySelectorAll !== "function" || !window.__gosx || !window.__gosx.engines) return;
    var tags = root.querySelectorAll(selector);
    for (var i = 0; i < tags.length; i++) {
      var commands;
      try {
        commands = JSON.parse(tags[i].textContent || "[]");
      } catch (err) {
        console.warn("[gosx] scene command payload parse failed:", err);
        continue;
      }
      if (!Array.isArray(commands) || !commands.length) continue;
      window.__gosx.engines.forEach(function(rec) {
        if (rec && rec.component === "GoSXScene3D" && rec.handle && typeof rec.handle.applyCommands === "function") {
          rec.handle.applyCommands(commands);
        }
      });
    }
  }

  window.__gosx_scene3d_command_bridge = {
    dispatchCommands: dispatchCommands,
    playTimeline: playTimeline,
    burstParticles: burstParticles,
    dispatchPoseFrame: dispatchPoseFrame,
    decodePoseFrame: decodePoseFrame,
    applyMountedPoseFrame: applyMountedPoseFrame,
    dispatchMotionFrame: dispatchMotionFrame,
    decodeMotionFrame: decodeMotionFrame,
    applyMountedMotionFrame: applyMountedMotionFrame,
    applyCommandScripts: applyCommandScripts,
  };
})();
