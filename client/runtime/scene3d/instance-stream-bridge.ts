// instance-stream-bridge.ts — lazy loader for the opt-in binary
// instance-transform fast path (bootstrap-feature-scene3d-instance-stream.js).
// @ts-check
//
// This file ships in the BASE scene3d bundle (bootstrap-feature-scene3d.js),
// the same as command-bridge.ts, which it mirrors: every Scene3D page pays a
// tiny fixed cost for this loader, but the actual instance-stream chunk
// (instance-stream.ts, ~1KB gzip) is fetched only the first time a caller
// actually streams a frame through handle.applyInstanceStream. A page that
// never calls it never fetches the chunk.
//
// Before this file existed, mount.ts's handle.applyInstanceStream read
// window.__gosx_scene3d_instance_stream_apply directly: present only if an
// author (or a Go/WASM engine) had ALREADY inserted a plain, hand-written
// <script src="/gosx/bootstrap-feature-scene3d-instance-stream.js"> tag
// themselves. That tag got no CSP nonce (so it was rejected outright on a
// nonce-based CSP), no versioned/hashed URL (so it got the year-long
// immutable Cache-Control server/runtime_assets.go applies to a versioned
// request, forever, from an unversioned path an app rebuild could silently
// change under it), and a first frame that arrived before the tag executed
// was dropped with no signal (the caller got {applied:false} and had no way
// to know whether that meant "stale revision" or "the chunk isn't here").
// This loader fixes all three: it reads the versioned URL island.go embeds,
// applies the same nonce/crossorigin/referrer policy every other lazy
// sub-feature chunk gets, and never silently drops a frame — see
// applyInstanceStreamFrame below.
(function() {
  if (typeof window === "undefined" || window.__gosxScene3DInstanceStreamBridgeLoader) return;
  window.__gosxScene3DInstanceStreamBridgeLoader = true;

  var loadPromise = null;

  // pendingFrames coalesces a frame that arrives while the chunk's one-time
  // load is still in flight, keyed by mount identity and then by the
  // frame's batch id (pendingBatchInfo). A page-owned game loop streaming
  // instance transforms every frame (60 fps, tens of KB each) during a
  // load that stalls for even a second or two used to queue every frame it
  // sent — tens of megabytes — because each queued call kept its own full
  // byte copy alive until the shared load settled. A later call for the
  // SAME (mount, batch) instead OVERWRITES the bucket slot below; nothing
  // else ever holds a direct reference to the byte copy it replaces (every
  // pending call's own .then() closure captures only the small, shared
  // key/mount/bucket, and re-reads the CURRENT bucket entry when the load
  // settles — see applyInstanceStreamFrame), so the replaced copy is
  // immediately eligible for garbage collection, not held alive for the
  // rest of the stall. The pending set this bounds is sized by the number
  // of distinct (mount, batch) pairs currently streaming, not by how many
  // frames arrived during the stall.
  var pendingFrames = new Map();

  function pendingBucketFor(mount) {
    var bucket = pendingFrames.get(mount);
    if (!bucket) {
      bucket = new Map();
      pendingFrames.set(mount, bucket);
    }
    return bucket;
  }

  // instanceStreamURL reads the versioned, content-hashed URL island.go
  // embeds as a data-* attribute on the main scene3d script tag (see
  // island.go's emitScene3DScriptTags and commandURL in command-bridge.ts,
  // which this mirrors exactly). Falls back to the unversioned compat path
  // only when the attribute is absent — a dev server or an older manifest
  // without the entry — so the loader still works, just without the
  // immutable long-lived cache a hashed URL gets.
  function instanceStreamURL() {
    try {
      /* @ts-expect-error TS2339 -- this object literal grows fields after construction; TypeScript does not apply evolving-object inference to .ts files (only to checkJs .js files) */ var tag = document.querySelector('script[data-gosx-script="feature-scene3d"]');
      if (tag && tag.dataset && tag.dataset.gosxScene3dInstanceStreamUrl) return tag.dataset.gosxScene3dInstanceStreamUrl;
    } catch (_e) {}
    return "/gosx/bootstrap-feature-scene3d-instance-stream.js";
  }

  // asUint8Array returns a read-only Uint8Array VIEW onto bytes (never a
  // copy), or null when bytes is not one of the three shapes
  // scene.InstanceStreamFrame.Encode's caller can pass. Shared by
  // pendingBatchInfo (reads the header only) and copyFrameBytes (copies
  // the whole payload).
  function asUint8Array(bytes) {
    try {
      if (bytes instanceof Uint8Array) return bytes;
      if (bytes && bytes.buffer instanceof ArrayBuffer && typeof bytes.byteOffset === "number") {
        return new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength);
      }
      if (typeof ArrayBuffer !== "undefined" && bytes instanceof ArrayBuffer) return new Uint8Array(bytes);
    } catch (_e) {}
    return null;
  }

  // GSXI_HEADER_BYTES and the magic/idLen field offsets mirror
  // decodeInstanceStreamFrame in instance-stream.ts -- see that file for
  // the authoritative wire format doc and the real decoder/validator. This
  // is deliberately NOT a decoder: it reads only far enough into the
  // header (the magic, then the length-prefixed batch id right after it)
  // to build a coalescing key BEFORE the chunk that owns real validation
  // has loaded. A frame this cannot even parse that far still gets a key
  // (the shared "unparsed" sentinel below), so repeated malformed frames
  // coalesce too, instead of growing the pending set once per call the
  // way the real decoder's eventual rejection would not.
  var GSXI_HEADER_BYTES = 24;

  function decodeBatchIDBytes(view, start, end) {
    if (typeof TextDecoder !== "undefined") return new TextDecoder("utf-8").decode(view.subarray(start, end));
    var out = "";
    for (var i = start; i < end; i++) out += String.fromCharCode(view[i]);
    return out;
  }

  // pendingBatchInfo returns { key, batchId } for bytes: key is the
  // pendingFrames coalescing key (namespaced so a batch literally named
  // "unparsed" can never collide with the malformed-frame sentinel);
  // batchId is the plain id text, used only for the error event a load
  // failure reports (see reportChunkLoadFailure).
  function pendingBatchInfo(bytes) {
    var view = asUint8Array(bytes);
    if (view && view.byteLength >= GSXI_HEADER_BYTES &&
        view[0] === 0x47 && view[1] === 0x53 && view[2] === 0x58 && view[3] === 0x49) {
      try {
        var dv = new DataView(view.buffer, view.byteOffset, view.byteLength);
        var idLen = dv.getUint16(20, true);
        var idEnd = GSXI_HEADER_BYTES + idLen;
        if (idEnd <= view.byteLength) {
          var batchId = decodeBatchIDBytes(view, GSXI_HEADER_BYTES, idEnd);
          return { key: "id:" + batchId, batchId: batchId };
        }
      } catch (_e) {}
    }
    return { key: "unparsed", batchId: "" };
  }

  // reportChunkLoadFailure is the load-time counterpart to instance-stream.ts's
  // own reportFailure: a frame that could not be applied because the CHUNK
  // itself never loaded must fail exactly as visibly as one the chunk
  // rejected after loading (malformed bytes, unknown batch, ...) — a
  // console.error plus a gosx:scene3d:instance-stream-error CustomEvent on
  // the mount, never a silent drop. Called once per still-pending (mount,
  // batch) when the load fails, never once per dropped/superseded frame —
  // see applyInstanceStreamFrame.
  function reportChunkLoadFailure(mount, batchId) {
    var reason = "instance-stream chunk failed to load";
    try {
      console.error("[gosx] scene3d instance-stream: " + reason + (batchId ? " (batch " + batchId + ")" : ""));
    } catch (_e) {}
    if (mount && typeof mount.dispatchEvent === "function") {
      var detail = { reason: reason, batchId: batchId || "" };
      var event = typeof CustomEvent === "function"
        ? new CustomEvent("gosx:scene3d:instance-stream-error", { detail: detail, bubbles: true })
        : { type: "gosx:scene3d:instance-stream-error", detail: detail };
      mount.dispatchEvent(event);
    }
    return { applied: false, reason: reason };
  }

  // loadInstanceStreamBridge fetches the chunk at most once per page,
  // exactly like command-bridge.ts's loadCommandBridge: every caller during
  // the in-flight load shares the SAME promise, so N mounts that each call
  // applyInstanceStream before the chunk arrives cause exactly one <script>
  // fetch, not N. A caller after the chunk already loaded resolves
  // immediately, synchronously-in-effect (a resolved promise's .then still
  // runs as a microtask, matching every other call's contract).
  function loadInstanceStreamBridge() {
    if (window.__gosx_scene3d_instance_stream_apply) return Promise.resolve(window.__gosx_scene3d_instance_stream_apply);
    if (loadPromise) return loadPromise;
    loadPromise = new Promise(function(resolve, reject) {
      var script = document.createElement("script");
      script.src = instanceStreamURL();
      script.async = true;
      script.type = "text/javascript";
      script.crossOrigin = "anonymous";
      script.referrerPolicy = "no-referrer";
      if (typeof script.setAttribute === "function") {
        script.setAttribute("src", script.src);
        script.setAttribute("type", "text/javascript");
        script.setAttribute("crossorigin", "anonymous");
        script.setAttribute("referrerpolicy", "no-referrer");
      }
      if (typeof gosxApplyCurrentScriptNonce === "function") {
        gosxApplyCurrentScriptNonce(script);
      }
      script.onload = function() {
        var fn = window.__gosx_scene3d_instance_stream_apply;
        if (typeof fn !== "function") {
          // The script executed with no network error but never published
          // its apply function -- an unrecoverable outcome for THIS load.
          // Clear the cache so the NEXT call starts a fresh fetch instead
          // of forever reusing a load that can never succeed, and reject
          // so every frame queued behind this one reports named, the same
          // as an actual network failure below.
          loadPromise = null;
          reject(new Error("instance-stream chunk loaded but did not publish its apply function"));
          return;
        }
        resolve(fn);
      };
      script.onerror = function(err) {
        loadPromise = null;
        reject(err);
      };
      (document.head || document.documentElement || document.body).appendChild(script);
    });
    return loadPromise;
  }

  // copyFrameBytes gives a caller's buffer a safe, independent copy before
  // this loader defers applying it to a later microtask (the queued path
  // below). decodeInstanceStreamFrame's own doc comment already documents
  // the zero-copy contract applyInstanceStreamFrame's caller relies on: the
  // source buffer is only guaranteed valid until that SYNCHRONOUS call
  // returns, because a per-frame encoder is free to reuse or free it right
  // after. The fast (already-loaded) path below still calls apply()
  // synchronously and needs no copy; only a frame queued behind the
  // one-time chunk load needs its own copy, since the encoder's buffer may
  // already be gone by the time the queued .then() runs.
  function copyFrameBytes(bytes) {
    var view = asUint8Array(bytes);
    return view ? view.slice() : bytes;
  }

  // applyInstanceStreamFrame is mount.ts's handle.applyInstanceStream body.
  // It lazy-loads the chunk on first use. A frame that arrives while that
  // one-time load is in flight is coalesced into pendingFrames, keyed by
  // (mount, batch id): a later frame for the same batch overwrites the
  // bucket slot, and every pending call's own .then()/.catch() reaction
  // re-reads that slot when the load settles rather than closing over the
  // entry it queued. Only the reaction that still finds ITS key present
  // (always exactly one per key: reactions on a promise run in attachment
  // order, so the first-attached call for a key wins, applying whatever is
  // CURRENTLY in the slot -- the newest frame -- and deletes it; every
  // later reaction for that same key then finds nothing left) applies or
  // reports; every other one resolves quietly as superseded. This never
  // resolves silently: a load failure reports through
  // reportChunkLoadFailure exactly once per still-pending batch, the same
  // way a decode or apply failure already does.
  window.__gosx_scene3d_apply_instance_stream_frame = function(sceneState, bytes, scheduleRender, mount) {
    var apply = window.__gosx_scene3d_instance_stream_apply;
    if (typeof apply === "function") return apply(sceneState, bytes, scheduleRender, mount);

    var info = pendingBatchInfo(bytes);
    var key = info.key;
    var bucket = pendingBucketFor(mount);
    bucket.set(key, { sceneState: sceneState, bytes: copyFrameBytes(bytes), scheduleRender: scheduleRender });

    return loadInstanceStreamBridge().then(function(loadedApply) {
      var fn = loadedApply || window.__gosx_scene3d_instance_stream_apply;
      var current = bucket.get(key);
      if (!current) {
        return { applied: false, reason: "superseded by a newer frame for the same batch" };
      }
      bucket.delete(key);
      if (bucket.size === 0) pendingFrames.delete(mount);
      if (typeof fn !== "function") return reportChunkLoadFailure(mount, info.batchId);
      return fn(current.sceneState, current.bytes, current.scheduleRender, mount);
    }, function(_err) {
      var current = bucket.get(key);
      if (!current) {
        return { applied: false, reason: "superseded by a newer frame for the same batch" };
      }
      bucket.delete(key);
      if (bucket.size === 0) pendingFrames.delete(mount);
      return reportChunkLoadFailure(mount, info.batchId);
    });
  };
})();
