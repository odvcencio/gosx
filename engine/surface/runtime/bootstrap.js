// gosx engine-surface bootstrap.
//
// Loaded as /gosx/surface/runtime.js. Walks the DOM for canvas placeholders
// emitted by surface.Renderer.Mount, fetches and instantiates each component's
// WASM, then bridges DOM events to the Go runtime via the symbols documented
// in engine/surface/runtime/runtime.go.
//
// Event kinds (kept in lockstep with runtime.go:18-37 — see runtime_test.go
// for the diff guard):
//   0 mount, 1 click, 2 dblclick, 3 pointerdown, 4 pointermove,
//   5 pointerup, 6 pointercancel, 7 wheel, 8 keydown, 9 keyup,
//  10 resize, 11 dispose
//
// Modifier bits: 1 = Shift, 2 = Ctrl, 4 = Alt, 8 = Meta.
"use strict";

(function () {
  // KIND_TABLE is the canonical map mirrored from runtime.go:18-37.
  // The test in runtime_test.go reads this object's source and diffs it.
  var KIND_TABLE = {
    mount: 0,
    click: 1,
    dblclick: 2,
    pointerdown: 3,
    pointermove: 4,
    pointerup: 5,
    pointercancel: 6,
    wheel: 7,
    keydown: 8,
    keyup: 9,
    resize: 10,
    dispose: 11
  };

  // wasmExecOnce guarantees the wasm_exec.js shim loads exactly once.
  var wasmExecPromise = null;
  function loadWasmExec() {
    if (wasmExecPromise) return wasmExecPromise;
    wasmExecPromise = new Promise(function (resolve, reject) {
      if (typeof Go === "function") return resolve();
      var s = document.createElement("script");
      s.src = "/gosx/surface/wasm_exec.js";
      s.defer = false;
      s.onload = function () {
        if (typeof Go === "function") resolve();
        else reject(new Error("wasm_exec.js loaded but Go is undefined"));
      };
      s.onerror = function () { reject(new Error("failed to load /gosx/surface/wasm_exec.js")); };
      document.head.appendChild(s);
    });
    return wasmExecPromise;
  }

  // wasmModuleCache memoises compileStreaming results per URL so two surfaces
  // sharing a component name reuse one Module.
  var wasmModuleCache = {};
  function fetchAndCompile(url) {
    if (wasmModuleCache[url]) return wasmModuleCache[url];
    var p;
    if (typeof WebAssembly.compileStreaming === "function") {
      p = WebAssembly.compileStreaming(fetch(url));
    } else {
      p = fetch(url).then(function (r) { return r.arrayBuffer(); }).then(function (b) {
        return WebAssembly.compile(b);
      });
    }
    wasmModuleCache[url] = p;
    return p;
  }

  // modBits returns the modifier bitfield for a DOM event.
  function modBits(ev) {
    return (ev.shiftKey ? 1 : 0) | (ev.ctrlKey ? 2 : 0) |
           (ev.altKey ? 4 : 0)   | (ev.metaKey ? 8 : 0);
  }

  // canvasPoint converts a clientX/Y pair into canvas-local coordinates.
  function canvasPoint(canvas, ev) {
    var rect = canvas.getBoundingClientRect();
    return [ev.clientX - rect.left, ev.clientY - rect.top];
  }

  // emit dispatches an event to the Go runtime if the entry-points exist.
  function emit(id, kind, floats, str) {
    var fn = globalThis.__gosx_surface_event;
    if (typeof fn !== "function") return;
    var buf = floats ? new Float64Array(floats) : new Float64Array(0);
    fn(id, kind, buf, str || "");
  }

  // mountSurface instantiates one component's WASM and attaches handlers
  // to the canvas element. Returns a teardown function (idempotent).
  function mountSurface(canvas) {
    var name = canvas.getAttribute("data-gosx-engine-component") || "";
    var status = canvas.getAttribute("data-gosx-engine-status") || "";
    var wasmURL = canvas.getAttribute("data-gosx-engine-wasm") || "";
    var propsB64 = canvas.getAttribute("data-gosx-engine-props") || "";
    var stale = canvas.getAttribute("data-gosx-engine-stale") === "1";

    if (canvas.__gosxMounted) return canvas.__gosxMounted;

    // Defect 4 path: server signals an unavailable surface.
    if (status === "missing" || !wasmURL) {
      paintMissing(canvas, name);
      canvas.__gosxMounted = function () {};
      return canvas.__gosxMounted;
    }

    if (stale) {
      paintStaleBadge(canvas);
    }

    var id = name + "#" + (++mountSeq);

    var disposed = false;
    var teardown = function () {
      if (disposed) return;
      disposed = true;
      var fn = globalThis.__gosx_surface_dispose;
      if (typeof fn === "function") {
        try { fn(id); } catch (e) { /* swallow */ }
      }
      removeListeners();
      delete canvas.__gosxMounted;
    };

    var removeListeners = function () {}; // populated after mount

    loadWasmExec()
      .then(function () { return fetchAndCompile(wasmURL); })
      .then(function (mod) {
        if (disposed) return;
        var go = new Go();
        return WebAssembly.instantiate(mod, go.importObject).then(function (inst) {
          // Run the WASM module; it will install __gosx_surface_* globals
          // and (typically) block on a channel, returning control to JS.
          go.run(inst);
          if (disposed) return;
          var mountFn = globalThis.__gosx_surface_mount;
          if (typeof mountFn !== "function") {
            console.error("gosx/surface: __gosx_surface_mount not registered after WASM run for", name);
            return;
          }
          mountFn(id, name, canvas, propsB64);
          attachListeners();
          watchDetach();
          resizeOnce();
          driveFrames();
        });
      })
      .catch(function (err) {
        console.error("gosx/surface: mount failed for", name, err);
        paintMissing(canvas, name);
      });

    function attachListeners() {
      var onClick = function (ev) {
        var p = canvasPoint(canvas, ev);
        emit(id, KIND_TABLE.click, [p[0], p[1], ev.button, ev.buttons, modBits(ev)]);
      };
      var onDbl = function (ev) {
        var p = canvasPoint(canvas, ev);
        emit(id, KIND_TABLE.dblclick, [p[0], p[1], ev.button, ev.buttons, modBits(ev)]);
      };
      var onDown = function (ev) {
        canvas.setPointerCapture && canvas.setPointerCapture(ev.pointerId);
        var p = canvasPoint(canvas, ev);
        emit(id, KIND_TABLE.pointerdown, [p[0], p[1], ev.button, ev.buttons, modBits(ev)]);
      };
      var onMove = function (ev) {
        var p = canvasPoint(canvas, ev);
        emit(id, KIND_TABLE.pointermove, [p[0], p[1], ev.button, ev.buttons, modBits(ev)]);
      };
      var onUp = function (ev) {
        var p = canvasPoint(canvas, ev);
        emit(id, KIND_TABLE.pointerup, [p[0], p[1], ev.button, ev.buttons, modBits(ev)]);
      };
      var onCancel = function (ev) {
        var p = canvasPoint(canvas, ev);
        emit(id, KIND_TABLE.pointercancel, [p[0], p[1], ev.button, ev.buttons, modBits(ev)]);
      };
      var onWheel = function (ev) {
        ev.preventDefault();
        var p = canvasPoint(canvas, ev);
        emit(id, KIND_TABLE.wheel, [p[0], p[1], ev.deltaX, ev.deltaY, modBits(ev)]);
      };
      var onKeyDown = function (ev) {
        emit(id, KIND_TABLE.keydown, [modBits(ev)], ev.key + "\t" + ev.code);
      };
      var onKeyUp = function (ev) {
        emit(id, KIND_TABLE.keyup, [modBits(ev)], ev.key + "\t" + ev.code);
      };

      canvas.addEventListener("click", onClick);
      canvas.addEventListener("dblclick", onDbl);
      canvas.addEventListener("pointerdown", onDown);
      canvas.addEventListener("pointermove", onMove);
      canvas.addEventListener("pointerup", onUp);
      canvas.addEventListener("pointercancel", onCancel);
      canvas.addEventListener("wheel", onWheel, { passive: false });
      canvas.addEventListener("keydown", onKeyDown);
      canvas.addEventListener("keyup", onKeyUp);

      var ro = null;
      if (typeof ResizeObserver === "function") {
        ro = new ResizeObserver(function () { resizeOnce(); });
        ro.observe(canvas);
      }
      window.addEventListener("resize", resizeOnce);

      removeListeners = function () {
        canvas.removeEventListener("click", onClick);
        canvas.removeEventListener("dblclick", onDbl);
        canvas.removeEventListener("pointerdown", onDown);
        canvas.removeEventListener("pointermove", onMove);
        canvas.removeEventListener("pointerup", onUp);
        canvas.removeEventListener("pointercancel", onCancel);
        canvas.removeEventListener("wheel", onWheel);
        canvas.removeEventListener("keydown", onKeyDown);
        canvas.removeEventListener("keyup", onKeyUp);
        window.removeEventListener("resize", resizeOnce);
        if (ro) ro.disconnect();
      };
    }

    function resizeOnce() {
      var dpr = window.devicePixelRatio || 1;
      var rect = canvas.getBoundingClientRect();
      var w = Math.max(1, Math.floor(rect.width));
      var h = Math.max(1, Math.floor(rect.height));
      if (canvas.width !== w * dpr) canvas.width = w * dpr;
      if (canvas.height !== h * dpr) canvas.height = h * dpr;
      emit(id, KIND_TABLE.resize, [w, h, dpr]);
    }

    function watchDetach() {
      if (!canvas.parentNode || typeof MutationObserver !== "function") return;
      var parent = canvas.parentNode;
      var mo = new MutationObserver(function () {
        if (!document.contains(canvas)) {
          mo.disconnect();
          teardown();
        }
      });
      mo.observe(parent, { childList: true, subtree: false });
    }

    function driveFrames() {
      var frameFn = globalThis.__gosx_surface_frame;
      if (typeof frameFn !== "function") return;
      var tick = function (ts) {
        if (disposed) return;
        try { frameFn(id, ts); } catch (e) { /* surface owns errors */ }
        requestAnimationFrame(tick);
      };
      requestAnimationFrame(tick);
    }

    canvas.__gosxMounted = teardown;
    return teardown;
  }

  // paintMissing draws a centered "surface unavailable" label on the canvas
  // for components that report status=missing or that fail to fetch their WASM.
  function paintMissing(canvas, name) {
    try {
      var rect = canvas.getBoundingClientRect();
      var w = Math.max(80, Math.floor(rect.width));
      var h = Math.max(40, Math.floor(rect.height));
      canvas.width = w;
      canvas.height = h;
      var ctx = canvas.getContext && canvas.getContext("2d");
      if (!ctx) return;
      ctx.fillStyle = "rgba(120,100,75,0.08)";
      ctx.fillRect(0, 0, w, h);
      ctx.fillStyle = "rgba(60,50,40,0.7)";
      ctx.font = "13px -apple-system, system-ui, sans-serif";
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      ctx.fillText("surface unavailable" + (name ? " — " + name : ""), w / 2, h / 2);
    } catch (e) { /* tolerate */ }
  }

  // paintStaleBadge draws a small corner indicator that the cached WASM is
  // stale (the most recent build failed but we are serving the previous one).
  function paintStaleBadge(canvas) {
    // No-op visual hint until the WASM mounts; the WASM is expected to draw
    // on top. We avoid blocking the mount: the badge is purely advisory.
    canvas.setAttribute("data-gosx-stale-acked", "1");
  }

  // mountSeq is the per-page id counter used to disambiguate instances.
  var mountSeq = 0;

  // discoverAndMount scans the document and mounts every surface placeholder.
  function discoverAndMount(root) {
    var nodes = (root || document).querySelectorAll("[data-gosx-engine-component]");
    for (var i = 0; i < nodes.length; i++) {
      var el = nodes[i];
      if (el.tagName && el.tagName.toLowerCase() === "canvas") {
        mountSurface(el);
      }
    }
  }

  // Expose register stub (the WASM module installs the real handlers).
  if (typeof globalThis.__gosx_surface_register !== "function") {
    globalThis.__gosx_surface_register = function () {};
  }

  // Always pre-install a noop __gosx_surface_event so callers (and the
  // browser-side QA harness) can probe its presence before any WASM has run.
  if (typeof globalThis.__gosx_surface_event !== "function") {
    var pending = [];
    globalThis.__gosx_surface_event = function (id, kind, payload, payloadStr) {
      pending.push([id, kind, payload, payloadStr]);
    };
    globalThis.__gosx_surface_event.__pending = pending;
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { discoverAndMount(); });
  } else {
    discoverAndMount();
  }

  // Expose the discoverer so dynamically-inserted placeholders can be mounted.
  globalThis.__gosx_surface_discover = discoverAndMount;
})();
