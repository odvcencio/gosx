// instance-stream.ts — Scene3D binary instance-transform fast path.
// @ts-check
//
// The opt-in counterpart to command-runtime.ts. A page-owned game loop that
// wants to update a registered InstancedMesh batch's per-instance transforms
// every frame without paying for a MountCommandBatch JSON round trip calls
// window.__gosx_scene3d_instance_stream_bridge.dispatchInstanceStream(target,
// bytes) with the raw bytes scene.InstanceStreamFrame.Encode produced. This
// chunk (bootstrap-feature-scene3d-instance-stream.js) is fetched lazily on
// the FIRST such call, by instance-stream-bridge.ts -- part of the base
// scene3d bundle every page pays for -- not by the caller; see that file's
// doc comment for the load, queue, and error-event contract.
//
// This file owns everything the fast path needs once loaded, so mount.ts
// (the base bundle every Scene3D page pays for) carries only a tiny
// forwarding method:
//
//   1. The wire decoder (decodeInstanceStreamFrame).
//   2. applyInstanceStreamFrame, published as
//      window.__gosx_scene3d_instance_stream_apply, which mount.ts's
//      handle.applyInstanceStream calls once this chunk has loaded. It
//      writes straight into a registered InstancedMesh batch's sceneState
//      entry -- bypassing SceneIR diffing and the JSON command pipeline
//      entirely -- then asks for the next scheduled render the same way
//      applyCommands does.
//   3. The same target-resolution/dispatch convenience command-runtime.ts
//      provides for applyCommands: resolve a mount or engine id to its
//      ready handle, retrying until a timeout, then call
//      handle.applyInstanceStream(bytes).
//
// Only scene.InstanceStreamTransform (a rigid InstancedMesh batch's flat
// 4x4-per-instance array) has an apply target today. InstanceStreamFrame's
// transform+color kind is reserved: applying it needs to cooperate with the
// retained model-instance pose tracker's membership bookkeeping instead of
// writing a flat array, so it ships separately. InstanceStreamSkinnedPose
// stays reserved permanently, by design, not just for now: an
// InstancedGLBMesh crowd's clip/time/loop state already has an owner, the
// PoseFrame/GSP2 channel (scene/pose_frame.go, command-runtime.ts's
// dispatchPoseFrame/applyMountedPoseFrame). Wiring a second apply target for
// the same job here would give callers two incompatible ways to stream the
// same skinned-pose data; use PoseFrame for that instead. A frame carrying
// either reserved kind fails named (console.error plus a
// gosx:scene3d:instance-stream-error event) instead of being silently
// dropped.
(function() {
  if (typeof window === "undefined" || window.__gosx_scene3d_instance_stream_bridge) return;

  var MAGIC0 = 0x47, MAGIC1 = 0x53, MAGIC2 = 0x58, MAGIC3 = 0x49; // "GSXI"
  var HEADER_BYTES = 24;
  var STRIDE_BY_KIND = { 0: 16, 1: 20, 2: 18 };

  function align4(n) {
    var rem = n % 4;
    return rem === 0 ? n : n + (4 - rem);
  }

  // readUint64LE combines two little-endian uint32 reads instead of using
  // DataView.getBigUint64, so this codec runs unchanged on an engine without
  // BigInt-typed DataView accessors. A per-frame revision counter never
  // approaches Number.MAX_SAFE_INTEGER (2^53), so the combination is exact.
  function readUint64LE(dv, offset) {
    var lo = dv.getUint32(offset, true);
    var hi = dv.getUint32(offset + 4, true);
    return hi * 4294967296 + lo;
  }

  function decodeID(view, start, end) {
    if (typeof TextDecoder !== "undefined") {
      return new TextDecoder("utf-8").decode(view.subarray(start, end));
    }
    var out = "";
    for (var i = start; i < end; i++) out += String.fromCharCode(view[i]);
    return out;
  }

  // decodeInstanceStreamFrame parses the bytes scene.InstanceStreamFrame.Encode
  // (scene/instance_stream.go) produced. It returns null for anything it does
  // not recognize -- too short, bad magic, unsupported version or kind, or a
  // length that disagrees with the header -- rather than throwing, so a
  // malformed or truncated frame (a dropped WebSocket chunk, a caller bug)
  // reaches applyInstanceStreamFrame's named-failure path instead of
  // crashing the render loop.
  function decodeInstanceStreamFrame(bytes) {
    if (!bytes) return null;
    var view;
    if (bytes instanceof Uint8Array) {
      view = bytes;
    } else if (bytes.buffer instanceof ArrayBuffer) {
      view = new Uint8Array(bytes.buffer, bytes.byteOffset || 0, bytes.byteLength);
    } else if (bytes instanceof ArrayBuffer) {
      view = new Uint8Array(bytes);
    } else {
      return null;
    }
    if (view.byteLength < HEADER_BYTES) return null;
    if (view[0] !== MAGIC0 || view[1] !== MAGIC1 || view[2] !== MAGIC2 || view[3] !== MAGIC3) return null;
    var dv = new DataView(view.buffer, view.byteOffset, view.byteLength);
    if (dv.getUint8(4) !== 1) return null; // version
    var kind = dv.getUint8(5);
    var stride = STRIDE_BY_KIND[kind];
    if (!stride) return null;
    var revision = readUint64LE(dv, 8);
    var count = dv.getUint32(16, true);
    var idLen = dv.getUint16(20, true);
    var idEnd = HEADER_BYTES + idLen;
    if (idEnd > view.byteLength) return null;
    var batchId = decodeID(view, HEADER_BYTES, idEnd);
    var payloadOffset = HEADER_BYTES + align4(idLen);
    var floatCount = count * stride;
    if (payloadOffset + floatCount * 4 !== view.byteLength) return null;
    var data;
    try {
      // Zero-copy view into the source buffer. Valid only until the caller
      // reuses or frees that buffer, so a consumer MUST copy out (see
      // applyInstanceStreamFrame's TypedArray.set into its own retained
      // buffer below) before returning to the event loop.
      data = new Float32Array(view.buffer, view.byteOffset + payloadOffset, floatCount);
    } catch (_err) {
      // view.byteOffset + payloadOffset was not 4-byte aligned (an odd
      // subarray a caller handed in). Fall back to an explicit float-by-float
      // read instead of failing the frame outright.
      data = new Float32Array(floatCount);
      for (var i = 0; i < floatCount; i++) data[i] = dv.getFloat32(payloadOffset + i * 4, true);
    }
    return { batchId: batchId, revision: revision, kind: kind, stride: stride, count: count, data: data };
  }

  // --- apply (called through mount.ts's handle.applyInstanceStream) ---

  function reportFailure(mount, reason, batchId) {
    console.error("[gosx] scene3d instance-stream: " + reason + (batchId ? " (batch " + batchId + ")" : ""));
    if (mount && typeof mount.dispatchEvent === "function") {
      var detail = { reason: reason, batchId: batchId || "" };
      var event = typeof CustomEvent === "function"
        ? new CustomEvent("gosx:scene3d:instance-stream-error", { detail: detail, bubbles: true })
        : { type: "gosx:scene3d:instance-stream-error", detail: detail };
      mount.dispatchEvent(event);
    }
    return { applied: false, reason: reason };
  }

  // targetCollection names which sceneState collection a wire Kind updates.
  // See the file doc comment for why kinds 1 and 2 have no target yet.
  function targetCollection(sceneState, kind) {
    return kind === 0 ? sceneState.instancedMeshes : null;
  }

  // applyInstanceStreamFrame writes bytes into the mounted InstancedMesh
  // batch they name. sceneState and scheduleRender are the mount's own
  // closure state, passed in by mount.ts's forwarding handle method so this
  // file needs no access to renderer internals to do its job.
  //
  // The per-batch revision counter used to live in a module-scope Map
  // (revisionsByBatchID), shared by every Scene3D mount this chunk's IIFE
  // ever sees on the page. Two independent mounts that happen to register a
  // batch under the same id -- plausible, since a batch id is author-chosen
  // per component instance, not page-unique -- then shared one revision
  // counter: a frame for mount B could be rejected as "stale" because mount
  // A had already advanced past that revision, or vice versa. The counter
  // now lives on sceneState itself, so it is scoped to the one mount that
  // owns it, exactly like every other piece of per-mount state this
  // function reads and writes.
  function applyInstanceStreamFrame(sceneState, bytes, scheduleRender, mount) {
    var frame = decodeInstanceStreamFrame(bytes);
    if (!frame) return reportFailure(mount, "malformed or unrecognized instance-stream frame", "");
    var revisions = sceneState._instanceStreamRevisions;
    if (!(revisions instanceof Map)) {
      revisions = new Map();
      sceneState._instanceStreamRevisions = revisions;
    }
    var lastRevision = revisions.get(frame.batchId) || 0;
    if (!Number.isFinite(frame.revision) || frame.revision <= 0 || frame.revision <= lastRevision) {
      return { applied: false, reason: "stale-revision" };
    }
    var collection = targetCollection(sceneState, frame.kind);
    if (!collection) return reportFailure(mount, "unsupported instance-stream kind " + frame.kind, frame.batchId);
    var entry = null;
    for (var i = 0; i < collection.length; i++) {
      if (collection[i] && collection[i].id === frame.batchId) { entry = collection[i]; break; }
    }
    if (!entry) return reportFailure(mount, "unknown instance-stream batch id", frame.batchId);
    if (frame.count !== entry.count) {
      return reportFailure(mount, "instance-stream count " + frame.count + " does not match mounted batch count " + entry.count, frame.batchId);
    }
    var floatsNeeded = frame.count * 16;
    if (!(entry._instanceStreamBuffer instanceof Float32Array) || entry._instanceStreamBuffer.length !== floatsNeeded) {
      entry._instanceStreamBuffer = new Float32Array(floatsNeeded);
      entry.transforms = entry._instanceStreamBuffer;
    }
    entry._instanceStreamBuffer.set(frame.data.subarray(0, floatsNeeded));
    revisions.set(frame.batchId, frame.revision);
    scheduleRender("instance-stream");
    return { applied: true, revision: frame.revision };
  }

  window.__gosx_scene3d_instance_stream_apply = applyInstanceStreamFrame;

  // --- dispatch convenience (mirrors command-runtime.ts's record/key/ready) ---

  function key(target, options) {
    if (options && typeof options.engineID === "string" && options.engineID.trim()) return options.engineID.trim();
    if (typeof target === "string" && target.trim()) return target.trim();
    if (target && typeof target.id === "string" && target.id.trim()) return target.id.trim();
    return "";
  }

  function ready(handle) {
    return Boolean(handle && handle.__gosxScene3DCommandReady === true && typeof handle.applyInstanceStream === "function");
  }

  function record(target, options) {
    if (ready(target)) return target;
    if (target && ready(target.__gosxScene3DHandle)) return target.__gosxScene3DHandle;
    var id = key(target, options || {});
    var mount = id && document && typeof document.getElementById === "function" ? document.getElementById(id) : null;
    if (mount && ready(mount.__gosxScene3DHandle)) return mount.__gosxScene3DHandle;
    var engine = id && window.__gosx && window.__gosx.engines && typeof window.__gosx.engines.get === "function" ? window.__gosx.engines.get(id) : null;
    return engine && ready(engine.handle) ? engine.handle : null;
  }

  // dispatchInstanceStream polls for a ready mount the same way
  // command-runtime.ts's dispatchCommands does, then hands the raw bytes to
  // handle.applyInstanceStream. Each call resolves synchronously once the
  // mount is ready, so a hot per-frame caller that keeps its own ready-handle
  // reference (resolve once, reuse across frames) can also call
  // applyInstanceStream directly and skip this helper's polling entirely.
  function dispatchInstanceStream(target, bytes, options) {
    var opts = options || {};
    var id = key(target, opts);
    var deadline = Date.now() + Math.max(0, Math.floor(Number(opts.timeoutMS) || 10000));
    function poll(resolve, reject) {
      var handle = record(target, opts);
      if (handle) return resolve(handle.applyInstanceStream(bytes));
      if (!id) return reject(new Error("Scene3D instance-stream target is not ready and has no stable id"));
      if (Date.now() >= deadline) return reject(new Error("Scene3D instance-stream target did not become ready: " + id));
      setTimeout(function() { poll(resolve, reject); }, 16);
    }
    return new Promise(poll);
  }

  window.__gosx_scene3d_instance_stream_bridge = {
    decode: decodeInstanceStreamFrame,
    dispatchInstanceStream: dispatchInstanceStream,
  };
})();
