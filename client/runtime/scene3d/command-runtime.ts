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

  function key(target, options) {
    if (options && typeof options.engineID === "string" && options.engineID.trim()) return options.engineID.trim();
    if (typeof target === "string" && target.trim()) return target.trim();
    if (target && typeof target.id === "string" && target.id.trim()) return target.id.trim();
    return "";
  }

  function ready(handle) {
    return Boolean(handle && handle.__gosxScene3DCommandReady === true && typeof handle.applyCommands === "function");
  }

  // GSP2 is a pose frame. Validate the entire buffer before the
  // mounted renderer sees it, so malformed or truncated frames change nothing.
  function decodePoseFrame(input) {
    var bytes = input instanceof ArrayBuffer ? new Uint8Array(input) : input;
    if (!(bytes instanceof Uint8Array)) throw new TypeError("Scene3D pose frame must be an ArrayBuffer or Uint8Array");
    var view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
    var offset = 0;
    function need(size) {
      if (size > view.byteLength - offset) throw new RangeError("truncated Scene3D pose frame");
    }
    function u16() { need(2); var value = view.getUint16(offset, true); offset += 2; return value; }
    function id() {
      var length = u16();
      if (!length) throw new TypeError("empty Scene3D pose ID");
      need(length);
      var value = new TextDecoder("utf-8", { fatal: true }).decode(bytes.subarray(offset, offset + length));
      offset += length;
      return value;
    }
    function number() {
      need(4);
      var value = view.getFloat32(offset, true);
      offset += 4;
      if (!Number.isFinite(value)) throw new TypeError("non-finite Scene3D pose value");
      return value;
    }
    need(6);
    if (view.getUint8(0) !== 71 || view.getUint8(1) !== 83 || view.getUint8(2) !== 80 || view.getUint8(3) !== 50) {
      throw new TypeError("unsupported Scene3D pose frame version");
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
      if (batchIDs.has(batchID)) throw new TypeError("duplicate Scene3D pose batch ID");
      batchIDs.add(batchID);
      var instanceCount = u16();
      var instances = [];
      var instanceIDs = new Set();
      for (var j = 0; j < instanceCount; j++) {
        var instanceID = id();
        if (instanceIDs.has(instanceID)) throw new TypeError("duplicate Scene3D pose instance ID");
        instanceIDs.add(instanceID);
        var instance = { id: instanceID, x: number(), y: number(), z: number(), rotationX: number(), rotationY: number(), rotationZ: number(), scaleX: number(), scaleY: number(), scaleZ: number(), animationTime: number(), animation: "", animationLoop: false };
        if (instance.animationTime < 0) throw new TypeError("negative Scene3D animation time");
        var clipIndex = u16();
        if (clipIndex >= clips.length) throw new RangeError("unknown Scene3D animation clip index");
        instance.animation = clips[clipIndex];
        need(1);
        var loop = view.getUint8(offset++);
        if (loop > 1) throw new TypeError("invalid Scene3D animation loop flag");
        instance.animationLoop = loop === 1;
        instances.push(instance);
      }
      batches.push({ id: batchID, instances: instances });
    }
    if (offset !== view.byteLength) throw new RangeError("trailing Scene3D pose frame data");
    return batches;
  }

  function record(target, options) {
    if (ready(target)) return { handle: target, mount: null };
    if (target && ready(target.__gosxScene3DHandle)) return { handle: target.__gosxScene3DHandle, mount: target };
    var id = key(target, options || {});
    var mount = id && document && typeof document.getElementById === "function" ? document.getElementById(id) : null;
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

  function dispatchPoseFrame(target, frame, options) {
    var opts = options || {};
    function fallback(error) {
      if (!Array.isArray(opts.fallbackCommands)) throw error;
      return dispatchCommands(target, opts.fallbackCommands, opts).then(function(result) {
        return { applied: true, binary: false, fallbackReason: String(error && error.message || error), commandResult: result };
      });
    }
    var batches;
    try { batches = decodePoseFrame(frame); } catch (error) { return Promise.resolve().then(function() { return fallback(error); }); }
    var id = key(target, opts);
    var deadline = Date.now() + Math.max(0, Math.floor(Number(opts.timeoutMS) || 10000));
    function poll(resolve, reject) {
      var rec = record(target, opts);
      if (rec) {
        if (typeof rec.handle.applyPoseFrame !== "function") return reject(new Error("Scene3D pose frames are unsupported by this mount"));
        return Promise.resolve().then(function() { return rec.handle.applyPoseFrame(batches); }).then(resolve, reject);
      }
      if (!id) return reject(new Error("Scene3D pose frame target is not ready and has no stable id"));
      if (Date.now() >= deadline) return reject(new Error("Scene3D pose frame target did not become ready: " + id));
      setTimeout(function() { poll(resolve, reject); }, 16);
    }
    return new Promise(poll).catch(fallback);
  }

  function applyMountedPoseFrame(state, batches, updateRigidPoses, scheduleRender, handle) {
    var stats = handle.__gosxPoseFrameStats || (handle.__gosxPoseFrameStats = { accepted: 0, plannerCallsSkipped: 0, rejected: Object.create(null) });
    function reject(reason) {
      stats.rejected[reason] = (stats.rejected[reason] || 0) + 1;
      throw new Error("Scene3D pose frame rejected: " + reason);
    }
    if (!Array.isArray(batches)) reject("invalid-frame");
    if (state._modelHydrationPromise || !state._hydratedModelRecords) reject("renderer-not-ready");
    var mounted = Array.isArray(state.instancedGLBMeshes) ? state.instancedGLBMeshes : [];
    var byID = new Map(mounted.map(function(batch) { return [batch.id, batch]; }));
    var patches = [];
    for (var batch of batches) {
      var current = byID.get(batch.id);
      if (!current || !Array.isArray(batch.instances) || current.instances.length !== batch.instances.length) reject("membership-changed");
      var instances = new Map(current.instances.map(function(instance) { return [instance.id, instance]; }));
      for (var pose of batch.instances) {
        var instance = instances.get(pose.id);
        if (!instance) reject("membership-changed");
        patches.push({ instance: instance, pose: pose });
      }
    }
    var fields = ["x", "y", "z", "rotationX", "rotationY", "rotationZ", "scaleX", "scaleY", "scaleZ", "animation", "animationTime", "animationLoop"];
    var previous = patches.map(function(patch) { return fields.map(function(field) { return patch.instance[field]; }); });
    for (var patch of patches) for (var field of fields) patch.instance[field] = patch.pose[field];
    var retained = false;
    try { retained = updateRigidPoses(state); } catch (_error) { retained = false; }
    if (!retained) {
      for (var i = 0; i < patches.length; i++) for (var j = 0; j < fields.length; j++) patches[i].instance[fields[j]] = previous[i][j];
      reject("retained-pose-unavailable");
    }
    stats.accepted++;
    stats.plannerCallsSkipped++;
    scheduleRender("pose-frame");
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
    dispatchPoseFrame: dispatchPoseFrame,
    decodePoseFrame: decodePoseFrame,
    applyMountedPoseFrame: applyMountedPoseFrame,
    applyCommandScripts: applyCommandScripts,
  };
})();
