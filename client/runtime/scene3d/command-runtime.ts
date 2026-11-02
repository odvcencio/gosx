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
  const commandQueues = new WeakMap<object, Promise<any>>();

  function key(target, options) {
    if (options && typeof options.engineID === "string" && options.engineID.trim()) return options.engineID.trim();
    if (typeof target === "string" && target.trim()) return target.trim();
    if (target && typeof target.id === "string" && target.id.trim()) return target.id.trim();
    return "";
  }

  function ready(handle) {
    return Boolean(handle && handle.__gosxScene3DCommandReady === true && typeof handle.applyCommands === "function");
  }

  // GSP2, GSP3 and GSP5 share strict framing, IDs and scalar validation. Format
  // fields remain separate, and nothing reaches the renderer until decoding ends.
  // GSP5 is the partial motion frame: a motion frame whose instances are named
  // by slot (their position in the mounted batch) instead of by ID, and which
  // lists only the instances that changed.
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
    var version = view.getUint8(3);
    var partial = motion && version === 53;
    if (view.getUint8(0) !== 71 || view.getUint8(1) !== 83 || view.getUint8(2) !== 80 || (version !== (motion ? 51 : 50) && !partial)) {
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
      var batchSize = partial ? u16() : 0;
      var instanceCount = u16();
      if (partial && instanceCount > batchSize) throw new RangeError("Scene3D partial motion frame lists more instances than its batch holds");
      var instances = [];
      var instanceIDs = new Set();
      var lastSlot = -1;
      for (var j = 0; j < instanceCount; j++) {
        var instance: any;
        if (partial) {
          var slot = u16();
          if (slot <= lastSlot) throw new RangeError("Scene3D partial motion frame slots must ascend strictly");
          if (slot >= batchSize) throw new RangeError("Scene3D partial motion frame slot is outside its batch");
          lastSlot = slot;
          instance = { slot: slot };
        } else {
          var instanceID = id();
          if (instanceIDs.has(instanceID)) throw new TypeError("duplicate " + kind + " instance ID");
          instanceIDs.add(instanceID);
          instance = { id: instanceID };
        }
        if (motion) {
          for (const side of ["prev", "next"]) {
            for (const field of poseFields.slice(0, 9)) {
              instance[side + field[0].toUpperCase() + field.slice(1)] = number();
            }
            instance[side === "prev" ? "tPrev" : "tNext"] = number();
          }
          if (!(instance.tNext > instance.tPrev)) throw new RangeError("Scene3D motion frame requires tNext after tPrev");
        } else {
          for (const field of poseFields.slice(0, 9)) instance[field] = number();
          instance.animationTime = number();
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
      batches.push(partial ? { id: batchID, partial: true, size: batchSize, instances: instances } : { id: batchID, instances: instances });
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

  function aborted(opts: any) {
    if (opts?.signal?.aborted) throw new DOMException("Scene3D operation canceled", "AbortError");
  }

  function apply(rec, commands, rev, opts) {
    // Asset fetch latency must not reorder mutations. A failed transaction
    // rejects its caller while leaving the mounted scene and queue healthy.
    const previous = commandQueues.get(rec.handle);
    const run = async () => {
      aborted(opts);
      const assets = window.__gosx_scene3d_assets;
      if (!assets && commands.some((command: any) => command?.data?.props?.verticesURL && !command.data.props.vertices)) {
        throw new Error("Scene3D geometry resolver is unavailable");
      }
      const referenced = commands.some(command => command?.data?.props?.verticesURL && !command.data.props.vertices);
      const resolved = assets && referenced ? await assets.resolveCommands(commands) : commands;
      aborted(opts);
      if (rec.mount && !ready(rec.mount.__gosxScene3DHandle)) throw new Error("Scene3D mount disposed");
      if (rec.mount && rec.mount.__gosxScene3DHandle !== rec.handle) throw new Error("Scene3D mount replaced");
      setAttr(rec.mount, "data-gosx-scene3d-command-revision", rev);
      await rec.handle.applyCommands(resolved);
      setAttr(rec.mount, "data-gosx-scene3d-command-applied-revision", rev);
      return { revision: rev, applied: true };
    };
    const operation = previous ? previous.catch(() => {}).then(run) : run();
    commandQueues.set(rec.handle, operation);
    operation.then(() => { if (commandQueues.get(rec.handle) === operation) commandQueues.delete(rec.handle); }, () => { if (commandQueues.get(rec.handle) === operation) commandQueues.delete(rec.handle); });
    return operation;
  }

  // Commands, frames and presentation share readiness and cancellation. A mount
  // can exist and still be unable to take the operation (its models are
  // hydrating): apply calls retry(why) to try again, until the call's deadline.
  function waitForCommandMount(target: any, opts: any, format: string, apply: (rec: any, resolve: (value: any) => void, reject: (error: unknown) => void, retry: (why: string) => void) => unknown) {
    var id = key(target, opts);
    var deadline = opts.timeoutMS === 0 ? Infinity : Date.now() + Math.max(0, Math.floor(Number(opts.timeoutMS) || 10000));
    return new Promise<any>(function(resolve, reject) {
      let timer: any = null, settled = false;
      const finish = (handler: any, value: any) => {
        if (settled) return;
        settled = true;
        if (timer !== null) clearTimeout(timer);
        opts.signal?.removeEventListener("abort", cancel);
        handler(value);
      };
      const cancel = () => finish(reject, new DOMException("Scene3D operation canceled", "AbortError"));
      const retry = (why: string) => {
        if (settled) return;
        if (Date.now() >= deadline) return finish(reject, new Error("Scene3D " + format + " target did not become ready" + (id ? ": " + id : "") + " (" + why + ")"));
        timer = setTimeout(poll, 16);
      };
      opts.signal?.addEventListener("abort", cancel, { once: true });
      function poll() {
        if (opts.signal?.aborted) return cancel();
        var rec = record(target, opts);
        const mount = typeof target === "object" && target ? target : (id ? document.getElementById(id) : null);
        if (mount?.getAttribute?.("data-gosx-scene3d-renderer") === "unsupported" || mount?.querySelector?.("[data-gosx-engine-unsupported]")) {
          return finish(reject, new Error("Scene3D renderer is unavailable"));
        }
        if (rec) {
          try { return apply(rec, value => finish(resolve, value), error => finish(reject, error), retry); }
          catch (error) { return finish(reject, error); }
        }
        if (!id) return finish(reject, new Error("Scene3D " + format + " target is not ready and has no stable id"));
        if (Date.now() >= deadline) return finish(reject, new Error("Scene3D " + format + " target did not become ready: " + id));
        timer = setTimeout(poll, 16);
      }
      poll();
    });
  }

  function getCamera(target: any) {
    const rec = record(target, {});
    return rec && typeof rec.handle.getCamera === "function" ? rec.handle.getCamera() : null;
  }

  // oceanWater makes (once) and returns the Float64Array a host reads the drawn water's frame from (Scene3D.OceanWater); sink, when given, is
  // added to the mount's sinks and called with it after every drawn frame; with stop it is removed again. recordOceanWater (mount-backend.ts) fills it. A handle given without its mount has none.
  function oceanWater(target: any, sink?: any, stop?: boolean) {
    const mount: any = record(target, {})?.mount;
    if (!mount) return null;
    if (!mount.__gosxOceanWater) mount.__gosxOceanWater = new Float64Array(340);
    if (sink) { const sinks = mount.__gosxOceanWaterSinks || (mount.__gosxOceanWaterSinks = new Set()); if (stop) { sinks.delete(sink); if (!sinks.size) mount.__gosxOceanWater[10] = 0; } else sinks.add(sink); } // the last sink gone: no host is left to ask
    return mount.__gosxOceanWater;
  }

  function whenReady(target: any, opts: any = {}) {
    return waitForCommandMount(target, { timeoutMS: 0, ...opts }, "readiness", (_rec, resolve) => resolve(true));
  }

  function setCamera(target: any, camera: any, opts: any = {}) {
    return waitForCommandMount(target, opts, "camera", (rec, resolve, reject) => {
      aborted(opts);
      if (typeof rec.handle.setCamera !== "function") return reject(new Error("Scene3D camera is unavailable"));
      rec.handle.setCamera(camera);
      resolve(true);
    });
  }

  // setOrbit moves the orbit target and limits of a mounted scene without moving
  // the camera. orbit is {target?, minDistance?, maxDistance?, pitchLimit?}.
  function setOrbit(target: any, orbit: any, opts: any = {}) {
    return waitForCommandMount(target, opts, "orbit", (rec, resolve, reject) => {
      aborted(opts);
      if (typeof rec.handle.setOrbit !== "function") return reject(new Error("Scene3D orbit is unavailable"));
      rec.handle.setOrbit(orbit);
      resolve(true);
    });
  }

  function setAnimationClock(target: any, clock: any, opts: any = {}) {
    return waitForCommandMount(target, opts, "animation clock", (rec, resolve, reject) => {
      aborted(opts);
      if (typeof rec.handle.setAnimationClock !== "function") return reject(new Error("Scene3D animation clock is unavailable"));
      rec.handle.setAnimationClock(clock);
      resolve(true);
    });
  }

  function dispatchCommands(target, commands, options) {
    if (!Array.isArray(commands)) return Promise.reject(new TypeError("Scene3D commands must be an array"));
    var opts = options || {};
    var rev = ++revision;
    return waitForCommandMount(target, opts, "command", function(rec, resolve, reject) {
      return apply(rec, commands, rev, opts).then(resolve, reject);
    });
  }

  function live(target: any, eventName: any, data: any, options: any) {
    if (typeof eventName !== "string" || !eventName.trim()) return Promise.reject(new TypeError("Scene3D Live event name is required"));
    var opts = options || {};
    return waitForCommandMount(target, opts, "live event", function(rec, resolve, reject) {
      aborted(opts);
      if (typeof rec.handle.applyLive !== "function") return reject(new Error("Scene3D Live events are unavailable"));
      return Promise.resolve(rec.handle.applyLive(eventName, data)).then(resolve, reject);
    });
  }

  const sceneAPI = window.__gosx_scene3d_api || (window.__gosx_scene3d_api = {});

  // One adapter per handle, shared by optional presentation features. Lower
  // orders run first, regardless of the order in which chunks were loaded.
  const commandHooks = new WeakMap<object, any>();
  function addCommandHook(mount: any, handle: any, name: string, order: number,
    beforeCommands: (commands: any[]) => void, dispose: () => void, alive: () => boolean) {
    let rec = commandHooks.get(handle);
    if (!rec) {
      const apply = handle.applyCommands, teardown = handle.dispose;
      rec = { hooks: new Map(), ordered: [], observer: null };
      const close = () => {
        if (!commandHooks.delete(handle)) return;
        rec.observer?.disconnect();
        if (handle.applyCommands === rec.commands) handle.applyCommands = apply;
        if (handle.dispose === rec.dispose) handle.dispose = teardown;
        const hooks = rec.ordered;
        rec.hooks.clear(); rec.ordered = [];
        let failure;
        for (const hook of hooks) {
          try { hook.dispose(); } catch (error) { failure ??= error; }
        }
        if (failure) throw failure;
      };
      rec.commands = handle.applyCommands = function(commands: any[]) {
        for (const hook of rec.ordered) hook.beforeCommands(commands);
        return apply.call(handle, commands);
      };
      rec.dispose = handle.dispose = function() { try { close(); } finally { teardown?.apply(handle, arguments); } };
      rec.apply = (commands: any[]) => apply.call(handle, commands);
      if (typeof MutationObserver !== 'undefined') {
        rec.observer = new MutationObserver(() => { if (!alive() || !mount.isConnected) close(); });
        rec.observer.observe(mount, { attributes: true, attributeFilter: ['data-gosx-scene3d-command-ready'] });
        if (mount.parentNode) rec.observer.observe(mount.parentNode, { childList: true });
      }
      commandHooks.set(handle, rec);
    }
    rec.hooks.set(name, { order, beforeCommands, dispose });
    rec.ordered = Array.from(rec.hooks.values()).sort((a: any, b: any) => a.order - b.order);
    return rec.apply;
  }
  sceneAPI.addCommandHook = addCommandHook;

  async function playPresentation(target: any, plan: any, opts: any, kind: string, method: string, datasetKey: string) {
    opts ||= {};
    const rec = record(target, opts) || await waitForCommandMount(target, opts, kind, (rec, resolve) => resolve(rec));
    if (!rec.mount && typeof rec.handle[method] === "function") return Promise.resolve().then(() => rec.handle[method](plan));
    const mount = rec.mount || Array.from(document.querySelectorAll('[data-gosx-scene3d-command-ready]')).find(function() { return arguments[0].__gosxScene3DHandle === rec.handle; });
    if (!mount) throw new Error("Scene3D " + kind + " mount is unavailable");
    const api = await sceneAPI.loadPresentation(kind, datasetKey);
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

  function frameStats(target: any, opts: any, motion: boolean) {
    const queue = (motion ? motionQueues : poseQueues).get(key(target, opts) || target);
    const rec = record(target, opts), property = "__gosx" + (motion ? "Motion" : "Pose") + "FrameStats";
    if (rec && rec.handle[property]) return rec.handle[property];
    const stats = queue && queue.stats || { accepted: 0, superseded: 0, errors: 0, lastError: "", rejected: Object.create(null) };
    if (!motion && !queue?.stats) Object.assign(stats, { plannerCallsSkipped: 0, fallback: 0 });
    if (queue) queue.stats = stats;
    if (rec) rec.handle[property] = stats;
    return stats;
  }

  function poseStats(target, opts) { return frameStats(target, opts, false); }

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

  // Binary frames only replace local pose fields. Keep two private wrapper
  // slots per committed identity so validation never mutates the wrapper or
  // matrix currently installed on any primitive. Shared declaration metadata
  // is still normalized once per selected batch on every frame; authored
  // material changes must retain their ordinary hydration rejection path.
  function sceneTryRetainedBinaryInstancePoses(state: any, batches: any[], records: any, bindings: SceneRetainedPoseBindings): boolean | null {
    const {scopeKey: sceneRigidMembershipScopeKey, templates: sceneInstancedGLBHydrationTemplates, expand: sceneInstancedGLBMeshToModels,
      clone: sceneCloneHydrationModel, matrix: sceneModelTransformMatrix, eligible: sceneRigidInstanceHydrationEligible,
      prepare: scenePrepareRigidInstancePatch, commit: sceneCommitRigidInstancePatch, number: sceneNumber, local: localPose} = bindings;
    const mounted: any[] = Array.isArray(state.instancedGLBMeshes) ? state.instancedGLBMeshes : [];
    const memberships = records.rigidInstancesByID;
    if (!(memberships instanceof Map)) return null;
    const scope = sceneRigidMembershipScopeKey(state);
    let held: any = state._retainedBinaryPoseCache;
    if (!held || held.records !== records || held.mounted !== mounted || held.scope !== scope) {
      held = { records, mounted, scope, batches: new WeakMap() };
      state._retainedBinaryPoseCache = held;
    }
    const patches: { entry: any; patch: any }[] = [];
    const keys = new Set<string>();
    const seenBatches = new Set<string>();
    for (const batch of batches) {
      if (!batch || typeof batch.id !== "string" || seenBatches.has(batch.id) || !Array.isArray(batch.instances)) return false;
      seenBatches.add(batch.id);
      const batchIndex = mounted.findIndex(function(candidate: any) { return candidate.id === batch.id; });
      const current = mounted[batchIndex];
      if (!current || !Array.isArray(current.instances) || current.instances.length !== batch.instances.length) return false;
      if (!current.instances.length) { held.batches.delete(current); return null; }
      // Parented/paletted declarations keep the established path. In
      // particular, a group/ParentNode's world affine matrix is never cached
      // here; the existing per-frame hierarchy owns its live composition.
      if (current.parentNode || current.palette && current.palette.length) { held.batches.delete(current); return null; }
      for (let index = 0; index < current.instances.length; index++) {
        if (!batch.instances[index] || current.instances[index].id !== batch.instances[index].id) return false;
        if (current.instances[index].parentMatrix) { held.batches.delete(current); return null; }
      }
      const seedBatch = Object.assign({}, current, { instances: [current.instances[0]] });
      const seed = sceneInstancedGLBMeshToModels(seedBatch, batchIndex)[0];
      const template = seed && sceneInstancedGLBHydrationTemplates.get(seed);
      if (!template || seed.fit) { held.batches.delete(current); return null; }
      let entries: any[] = held.batches.get(current);
      if (!entries) {
        entries = [];
        for (const instance of current.instances) {
          const id = current.id + "/" + instance.id;
          const membership = memberships.get(id);
          if (!membership || membership.template !== template || membership.scope !== scope || membership.staged !== records.rigidInstances.get(membership.key)) return false;
          const original = membership.staged.rigidInstanceModel;
          if (!original || original.fit || sceneInstancedGLBHydrationTemplates.get(original) !== template) return false;
          // Parent/tint metadata is outside the hydration key. Let ordinary
          // staging clear a previous declaration before cloning local slots.
          if (!localPose(original)) return null;
          entries.push({ membership, staged: membership.staged, key: membership.key, template, next: 0, models: [sceneCloneHydrationModel(original), sceneCloneHydrationModel(original)] });
        }
        held.batches.set(current, entries);
      }
      if (entries.length !== current.instances.length) { held.batches.delete(current); return null; }
      for (let index = 0; index < entries.length; index++) {
        const entry = entries[index], membership = entry.membership, instance = current.instances[index];
        if (memberships.get(current.id + "/" + instance.id) !== membership || membership.staged !== entry.staged || membership.key !== entry.key || template !== entry.template || membership.template !== template || membership.scope !== scope || membership.staged !== records.rigidInstances.get(membership.key)) {
          // A valid rehydration may reuse its containers. Drop old wrappers and
          // let ordinary atomic staging validate the new committed identities.
          held.batches.delete(current);
          return null;
        }
        if (keys.has(membership.key)) return false;
        keys.add(membership.key);
        const model = entry.models[entry.next];
        model.x = sceneNumber(instance.x, 0); model.y = sceneNumber(instance.y, 0); model.z = sceneNumber(instance.z, 0);
        model.rotationX = sceneNumber(instance.rotationX, 0); model.rotationY = sceneNumber(instance.rotationY, 0); model.rotationZ = sceneNumber(instance.rotationZ, 0);
        model.scaleX = sceneNumber(instance.scaleX, 1); model.scaleY = sceneNumber(instance.scaleY, 1); model.scaleZ = sceneNumber(instance.scaleZ, 1);
        const pose = model._crowdPose || (model._crowdPose = {});
        pose.animation = instance.animation || ""; pose.animationTime = instance.animationTime || 0; pose.animationLoop = instance.animationLoop === true;
        const matrix = sceneModelTransformMatrix(model);
        if (!sceneRigidInstanceHydrationEligible(model, matrix)) return false;
        const patch = scenePrepareRigidInstancePatch(state, membership.staged, model, matrix);
        if (!patch) return false;
        patches.push({ entry, patch });
      }
    }
    // All ownership, pose rows, and affine eligibility checks finish first.
    for (const { entry, patch } of patches) {
      sceneCommitRigidInstancePatch(patch);
      for (const object of patch.staged.objects) delete object._crowdMotion;
      patch.staged._poseRevision = patch.poseRevision + 1;
      entry.next = 1 - entry.next;
    }
    return true;
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

  // Sampled clip frames (GSP4) pose skinned models from exact clip times. A
  // frame is complete state for the models it names, so there is no queue and
  // no supersession: frames apply in dispatch order and the last one wins.
  //
  // createClipDecoder returns a decode(input) function that validates the whole
  // frame before anything reaches a model. Its result (the models array, each
  // model and each layer object) is REUSED by the next call to the same
  // decoder, so a caller must consume it before decoding again. Strings are
  // interned by byte content, so steady-state decoding allocates nothing.
  function createClipDecoder() {
    var textDecoder = new TextDecoder("utf-8", { fatal: true });
    var scratch = new DataView(new ArrayBuffer(4));
    var strings: any[] = [];
    var names: string[] = [];
    var models: any[] = [];
    var pools: any[][] = [];
    var bytes: Uint8Array = new Uint8Array(0);
    var offset = 0;
    function need(size: number) {
      if (size > bytes.length - offset) throw new RangeError("truncated Scene3D clip frame");
    }
    function u16() { need(2); var value = bytes[offset] | (bytes[offset + 1] << 8); offset += 2; return value; }
    function text() {
      var length = u16();
      if (!length) throw new TypeError("empty Scene3D clip frame ID");
      need(length);
      for (var i = 0; i < strings.length; i++) {
        var known = strings[i].bytes;
        if (known.length !== length) continue;
        var j = 0;
        while (j < length && known[j] === bytes[offset + j]) j++;
        if (j === length) { offset += length; return strings[i].text; }
      }
      var value = textDecoder.decode(bytes.subarray(offset, offset + length));
      if (strings.length < 512) strings.push({ bytes: bytes.slice(offset, offset + length), text: value });
      offset += length;
      return value;
    }
    return function decode(input: any) {
      bytes = input instanceof ArrayBuffer ? new Uint8Array(input) : input;
      if (!(bytes instanceof Uint8Array)) throw new TypeError("Scene3D clip frame must be an ArrayBuffer or Uint8Array");
      offset = 0;
      need(6);
      if (bytes[0] !== 71 || bytes[1] !== 83 || bytes[2] !== 80 || bytes[3] !== 52) throw new TypeError("unsupported Scene3D clip frame version");
      offset = 4;
      var nameCount = u16();
      for (var n = 0; n < nameCount; n++) names[n] = text();
      var count = u16();
      for (var i = 0; i < count; i++) {
        var model = models[i] || (models[i] = { id: "", layers: [] });
        var pool = pools[i] || (pools[i] = []);
        var modelID = text();
        for (var d = 0; d < i; d++) if (models[d].id === modelID) throw new TypeError("duplicate Scene3D clip frame model ID");
        model.id = modelID;
        need(1);
        var layerCount = bytes[offset++];
        if (layerCount > 4) throw new RangeError("too many Scene3D clip frame layers");
        for (var j = 0; j < layerCount; j++) {
          // One bounds check per 11-byte layer. The floats are read inline: a
          // helper that returns a double would box it into a heap number.
          need(11);
          var ref = bytes[offset] | (bytes[offset + 1] << 8);
          var layer = pool[j] || (pool[j] = { name: "", index: 0, time: 0, weight: 0, loop: false });
          if (ref & 0x8000) { layer.index = ref & 0x7fff; layer.name = ""; }
          else if (ref < nameCount) { layer.index = 0; layer.name = names[ref]; }
          else throw new RangeError("unknown Scene3D clip frame clip name");
          for (var k = 0; k < 4; k++) scratch.setUint8(k, bytes[offset + 2 + k]);
          layer.time = scratch.getFloat32(0, true);
          for (var w = 0; w < 4; w++) scratch.setUint8(w, bytes[offset + 6 + w]);
          layer.weight = scratch.getFloat32(0, true);
          if (!Number.isFinite(layer.time) || !Number.isFinite(layer.weight)) throw new TypeError("non-finite Scene3D clip frame value");
          if (layer.time < 0 || layer.weight < 0) throw new RangeError("negative Scene3D clip frame time or weight");
          var flags = bytes[offset + 10];
          if (flags > 1) throw new TypeError("invalid Scene3D clip frame layer flags");
          layer.loop = flags === 1;
          offset += 11;
          model.layers[j] = layer;
        }
        model.layers.length = layerCount;
      }
      models.length = count;
      if (offset !== bytes.length) throw new RangeError("trailing Scene3D clip frame data");
      return models;
    };
  }

  // decodeClipFrame decodes into fresh storage that the caller owns.
  function decodeClipFrame(input: any) { return createClipDecoder()(input); }

  function clipFrameStats(target: any, opts: any) {
    var rec = record(target, opts);
    var stats = (rec && rec.handle.__gosxClipFrameStats) || { accepted: 0, errors: 0, lastError: "", rejected: Object.create(null) };
    if (rec) rec.handle.__gosxClipFrameStats = stats;
    return stats;
  }

  function clipFrameFailed(target: any, opts: any, error: any) {
    var stats = clipFrameStats(target, opts);
    stats.errors++;
    stats.lastError = String(error && error.message || error);
    var rec = record(target, opts);
    var eventTarget = rec && rec.mount || window;
    if (typeof CustomEvent === "function" && eventTarget && typeof eventTarget.dispatchEvent === "function") {
      try { eventTarget.dispatchEvent(new CustomEvent("gosx:scene3d:clip-frame-error", { detail: { reason: stats.lastError } })); } catch (_error) {}
    }
  }

  function dispatchClipFrame(target: any, frame: any, options: any) {
    var opts = options || {};
    if (!key(target, opts) && !target) return Promise.reject(new Error("Scene3D clip frame target has no stable id"));
    // Fast path: a mounted, ready target decodes into its own reusable decoder
    // and applies synchronously, so nothing is retained between frames.
    var rec = record(target, opts);
    if (rec && typeof rec.handle.applyClipFrame === "function") {
      try {
        var decode = rec.handle.__gosxClipDecoder || (rec.handle.__gosxClipDecoder = createClipDecoder());
        return Promise.resolve(rec.handle.applyClipFrame(decode(frame)));
      } catch (error) {
        clipFrameFailed(target, opts, error);
        return Promise.reject(error);
      }
    }
    var models;
    try { models = decodeClipFrame(frame); } catch (error) { return Promise.reject(error); }
    return waitForCommandMount(target, opts, "clip frame", function(rec, resolve, reject) {
      if (typeof rec.handle.applyClipFrame !== "function") return reject(new Error("Scene3D clip frames are unsupported by this mount"));
      try { resolve(rec.handle.applyClipFrame(models)); } catch (error) { reject(error); }
    }).catch(function(error) {
      clipFrameFailed(target, opts, error);
      throw error;
    });
  }

  var CLIP_FRAME_RESULTS = [{ applied: true, binary: true, pending: false }, { applied: true, binary: true, pending: true }];

  function applyMountedClipFrame(state: any, models: any, animationApi: any, applyPose: any, scheduleRender: any, handle: any) {
    var stats = handle.__gosxClipFrameStats || (handle.__gosxClipFrameStats = { accepted: 0, errors: 0, lastError: "", rejected: Object.create(null) });
    // The animation chunk loads with the first skinned model. Without it no
    // model can be sampled yet, which is the same as a renderer that is not ready.
    var result = animationApi && animationApi.applyClipFrame ? animationApi.applyClipFrame(state, models, applyPose) : { applied: false, reason: "renderer-not-ready" };
    if (!result || result.applied !== true) {
      var reason = String(result && result.reason || "invalid-frame");
      stats.rejected[reason] = (stats.rejected[reason] || 0) + 1;
      throw new Error("Scene3D clip frame rejected: " + reason + (result && result.id ? " (" + result.id + ")" : ""));
    }
    stats.accepted++;
    if (!result.pending) scheduleRender("clip-frame");
    return CLIP_FRAME_RESULTS[result.pending ? 1 : 0];
  }

  // GPU-driven crowd motion retains its own queues and mount validation.
  var motionQueues = new Map();
  var MOTION_FRAME_PENDING = { applied: false, binary: true, pending: true };

  function decodeMotionFrame(input) { return decodeInstanceFrame(input, true); }

  // A GSP5 frame carries only the instances that changed. Superseding one
  // with a newer frame would lose the older frame's movers, so the motion
  // queue merges partial frames and never drops one for a full frame.
  // @ts-ignore TS7006 -- untyped, matching this file's convention.
  function isPartialMotionFrame(frame) {
    var bytes = frame instanceof ArrayBuffer ? new Uint8Array(frame, 0, Math.min(4, frame.byteLength)) : frame;
    return bytes instanceof Uint8Array && bytes.length >= 4 && bytes[0] === 71 && bytes[1] === 83 && bytes[2] === 80 && bytes[3] === 53;
  }

  // mergePartialMotionBatches folds the changes of a newer partial frame over
  // an older one: per batch and slot the newer entry wins. A batch whose size
  // changed between the two frames is replaced whole by the newer entry, since
  // its older slots refer to a membership that no longer exists.
  // @ts-ignore TS7006 -- untyped, matching this file's convention.
  function mergePartialMotionBatches(older, newer) {
    var merged = new Map();
    for (var group of [older, newer]) {
      for (var batch of group) {
        var entry = merged.get(batch.id);
        if (!entry || entry.size !== batch.size) {
          entry = { id: batch.id, partial: true, size: batch.size, slots: new Map() };
          merged.set(batch.id, entry);
        }
        for (var instance of batch.instances) entry.slots.set(instance.slot, instance);
      }
    }
    var out = [];
    for (var entry of merged.values()) {
      var slots = Array.from(entry.slots.keys()).sort(function(a: number, b: number) { return a - b; });
      out.push({ id: entry.id, partial: true, size: entry.size, instances: slots.map(function(slot) { return entry.slots.get(slot); }) });
    }
    return out;
  }

  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function dispatchMotionFrame(target, frame, options) {
    var opts = options || {};
    var queueKey = key(target, opts) || target;
    if (!queueKey) return Promise.reject(new Error("Scene3D motion frame target has no stable id"));
    return new Promise(function(resolve, reject) {
      var queue = motionQueues.get(queueKey);
      if (!queue) {
        queue = { running: false, pending: null, ahead: [], stats: null };
        motionQueues.set(queueKey, queue);
      }
      var partial = isPartialMotionFrame(frame);
      var decoded = null;
      if (queue.pending && (partial || queue.pending.partial)) {
        // Only two partial frames fold into one. A partial frame and a full
        // frame each depend on the other being applied, so both run, in order.
        var folded = null;
        if (partial && queue.pending.partial) {
          try { folded = mergePartialMotionBatches(queue.pending.decoded || decodeMotionFrame(queue.pending.frame), decodeMotionFrame(frame)); } catch (_error) { folded = null; }
        }
        if (folded) decoded = folded;
        else { queue.ahead.push(queue.pending); queue.pending = null; }
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
      queue.pending = { target: target, frame: frame, opts: opts, resolve: resolve, reject: reject, partial: partial, decoded: decoded };
      if (!queue.running) runMotionQueue(queueKey, queue);
    });
  }

  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function motionStats(target, opts) { return frameStats(target, opts, true); }

  // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
  function runMotionQueue(queueKey, queue) {
    var job = queue.ahead.length ? queue.ahead.shift() : queue.pending;
    if (!job) { queue.running = false; motionQueues.delete(queueKey); return; }
    if (job === queue.pending) queue.pending = null;
    queue.running = true;
    var operation;
    try {
      operation = Array.isArray(job.opts.beforeCommands)
        ? dispatchCommands(job.target, job.opts.beforeCommands, job.opts).then(function() { return dispatchMotionFrameNow(job.target, job.frame, job.opts, job.decoded); })
        : dispatchMotionFrameNow(job.target, job.frame, job.opts, job.decoded);
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
  function dispatchMotionFrameNow(target, frame, opts, decoded) {
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
    try { batches = decoded || decodeMotionFrame(frame); } catch (error) { return Promise.resolve().then(function() { return fallback(error); }); }
    return waitForCommandMount(target, opts, "motion frame", function(rec, resolve, reject, retry) {
      motionStats(target, opts);
      if (typeof rec.handle.applyMotionFrame !== "function") return reject(new Error("Scene3D motion frames are unsupported by this mount"));
      // A mount whose models are still hydrating reports pending. A motion
      // frame is sent once per change, so it is held (up to the call's timeout
      // and signal) and applied when they commit. A pose frame is staged
      // instead: it advances the identities already declared and does not wait.
      return Promise.resolve().then(function() { return rec.handle.applyMotionFrame(batches); })
        .then(function(result) { return result && result.pending === true ? retry("models are still hydrating") : resolve(result); }, reject);
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
    if (state._modelHydrationPromise || !state._hydratedModelRecords) {
      // The crowd's models are loading or about to be replaced; the motion
      // records need the committed objects. dispatchMotionFrame tries again. A
      // hydration that failed will not commit, so a frame does not wait for it.
      if (state._modelHydrationPromise || (state._modelHydrationUncommitted && !state._modelHydrationFailed)) return MOTION_FRAME_PENDING;
      reject("renderer-not-ready");
    }
    var mounted = Array.isArray(state.instancedGLBMeshes) ? state.instancedGLBMeshes : [];
    for (var batch of batches) {
      // @ts-ignore TS7006 -- untyped, matching this file's convention. the expect-error form would report this directive unused under tsconfig.scene3d.json (noImplicitAny off there); @ts-ignore is silent either way.
      var current = mounted.find(function(candidate) { return candidate.id === batch.id; });
      if (batch && batch.partial === true) {
        // A partial batch names agents by slot. Its size must equal the
        // mounted instance count; a frame built before a membership change
        // would otherwise move the wrong agents. Slots become the mounted IDs
        // so the writer below handles partial and full frames alike.
        if (!current || !Array.isArray(batch.instances) || current.instances.length !== batch.size) reject("membership-changed");
        for (var slotted of batch.instances) slotted.id = current.instances[slotted.slot].id || ("instance-" + slotted.slot);
        continue;
      }
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
    live: live,
    getCamera, oceanWater, setCamera, setOrbit, whenReady, setAnimationClock,
    playTimeline: playTimeline,
    burstParticles: burstParticles,
    dispatchPoseFrame: dispatchPoseFrame,
    decodePoseFrame: decodePoseFrame,
    applyMountedPoseFrame: applyMountedPoseFrame,
    tryRetainedBinaryInstancePoses: sceneTryRetainedBinaryInstancePoses,
    dispatchClipFrame: dispatchClipFrame,
    decodeClipFrame: decodeClipFrame,
    createClipDecoder: createClipDecoder,
    applyMountedClipFrame: applyMountedClipFrame,
    dispatchMotionFrame: dispatchMotionFrame,
    decodeMotionFrame: decodeMotionFrame,
    applyMountedMotionFrame: applyMountedMotionFrame,
    applyCommandScripts: applyCommandScripts,
  };
})();
