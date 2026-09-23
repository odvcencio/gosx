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

  // reportChunkLoadFailure is the load-time counterpart to instance-stream.ts's
  // own reportFailure: a frame that could not be applied because the CHUNK
  // itself never loaded must fail exactly as visibly as one the chunk
  // rejected after loading (malformed bytes, unknown batch, ...) — a
  // console.error plus a gosx:scene3d:instance-stream-error CustomEvent on
  // the mount, never a silent drop.
  function reportChunkLoadFailure(mount) {
    var reason = "instance-stream chunk failed to load";
    try {
      console.error("[gosx] scene3d instance-stream: " + reason);
    } catch (_e) {}
    if (mount && typeof mount.dispatchEvent === "function") {
      var detail = { reason: reason, batchId: "" };
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
      script.onload = function() { resolve(window.__gosx_scene3d_instance_stream_apply); };
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
    try {
      if (bytes instanceof Uint8Array) return bytes.slice();
      if (bytes && bytes.buffer instanceof ArrayBuffer && typeof bytes.byteOffset === "number") {
        return new Uint8Array(bytes.buffer, bytes.byteOffset, bytes.byteLength).slice();
      }
      if (typeof ArrayBuffer !== "undefined" && bytes instanceof ArrayBuffer) return new Uint8Array(bytes.slice(0));
    } catch (_e) {}
    return bytes;
  }

  // applyInstanceStreamFrame is mount.ts's handle.applyInstanceStream body.
  // It lazy-loads the chunk on first use, queues every frame that arrives
  // while that one-time load is in flight (each caller's own .then()
  // callback on the SAME shared loadPromise; multiple reactions on one
  // promise fire in attachment order, so frames apply in arrival order once
  // the chunk is ready — functionally a FIFO queue with no extra array),
  // and never resolves silently: a load failure reports through
  // reportChunkLoadFailure exactly like a decode or apply failure does.
  window.__gosx_scene3d_apply_instance_stream_frame = function(sceneState, bytes, scheduleRender, mount) {
    var apply = window.__gosx_scene3d_instance_stream_apply;
    if (typeof apply === "function") return apply(sceneState, bytes, scheduleRender, mount);
    var queued = copyFrameBytes(bytes);
    return loadInstanceStreamBridge().then(function(loadedApply) {
      var fn = loadedApply || window.__gosx_scene3d_instance_stream_apply;
      if (typeof fn !== "function") return reportChunkLoadFailure(mount);
      return fn(sceneState, queued, scheduleRender, mount);
    }, function(_err) {
      return reportChunkLoadFailure(mount);
    });
  };
})();
