// gosx engine-surface bootstrap — standalone bytecode hydrator.
//
// Served as /gosx/surface/runtime.js by surface.RuntimeHandler. This
// bootstrap is for pages that consume engine surfaces via surface.HeadAssets()
// WITHOUT loading the full gosx page bootstrap chain
// (/gosx/bootstrap.js + /gosx/bootstrap-feature-engines.js). The classic
// example: hyphae's hypha-viz, which renders a single engine surface and
// embeds it via `<canvas data-gosx-engine-bytecode="...">`.
//
// The bootstrap responsibility chain:
//
//   1. Load wasm_exec.js (Go's syscall/js shim — the Go constructor).
//   2. Fetch + instantiate /gosx/runtime.wasm. This is the SHARED client
//      WASM that exposes __gosx_hydrate_engine_surface (added by sycamore
//      in this PR). The WASM blocks on a select{} so it stays alive to
//      service hydrate/tick/dispatch/dispose calls.
//   3. Once the WASM has installed its globals, scan the DOM for
//      <canvas data-gosx-engine-bytecode="..."> placeholders and mount
//      each one: fetch the program JSON, call hydrate, wire DOM events
//      and a requestAnimationFrame loop, set up MutationObserver
//      teardown.
//
// Post-buildsurface-deletion (gosx PR #16) this is the ONLY engine-surface
// path that still works in this loader; the legacy
// data-gosx-engine-wasm path was removed when internal/buildsurface/ was
// deleted (ADR 0005 closure record 2026-05-27).
//
// Event payload protocol (must match client/wasm/engine_surface_full.go's
// decodeFloat64Array + bridge.DispatchEngineSurfaceEvent's prop staging):
//
//   | kind | event         | payload                  | string         |
//   |  1   | click         | [x,y,button,buttons,mod] | ""             |
//   |  2   | dblclick      | [x,y,button,buttons,mod] | ""             |
//   |  3   | pointerdown   | [x,y,button,buttons,mod] | ""             |
//   |  4   | pointermove   | [x,y,button,buttons,mod] | ""             |
//   |  5   | pointerup     | [x,y,button,buttons,mod] | ""             |
//   |  6   | pointercancel | [x,y,button,buttons,mod] | ""             |
//   |  7   | wheel         | [x,y,dx,dy,mod]          | ""             |
//   |  8   | keydown       | [mod]                    | key+"\t"+code  |
//   |  9   | keyup         | [mod]                    | key+"\t"+code  |
//   | 10   | resize        | [w,h,dpr]                | ""             |
//
"use strict";

(function () {
  // KIND_TABLE is the canonical kind → numeric mapping documented in
  // engine/surface/runtime/runtime.go (lines 18-37). The native-side
  // TestBootstrapKindTableMatchesRuntimeRuntime test parses this object
  // and diffs it against the Go side to catch drift. Production
  // dispatch uses literal numbers in the event handlers below — this
  // table is purely the cross-language anchor.
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
  // Referenced once so a future minifier can't dead-code-eliminate it.
  void KIND_TABLE;

  // --- Shared WASM loader (one-shot per page) -------------------------------

  let runtimePromise = null;
  function loadRuntime() {
    if (runtimePromise) return runtimePromise;
    runtimePromise = loadWasmExec().then(function () {
      return instantiateRuntime();
    }).then(function () {
      return waitForRuntimeReady();
    });
    return runtimePromise;
  }

  function loadWasmExec() {
    return new Promise(function (resolve, reject) {
      if (typeof Go === "function") return resolve();
      const s = document.createElement("script");
      s.src = "/gosx/surface/wasm_exec.js";
      s.defer = false;
      s.onload = function () {
        if (typeof Go === "function") resolve();
        else reject(new Error("wasm_exec.js loaded but Go is undefined"));
      };
      s.onerror = function () { reject(new Error("failed to load /gosx/surface/wasm_exec.js")); };
      document.head.appendChild(s);
    });
  }

  // instantiateRuntime fetches + runs /gosx/runtime.wasm. The WASM blocks
  // forever (per main.go) and installs the __gosx_* globals during its
  // initial registerRuntime() call.
  async function instantiateRuntime() {
    const url = "/gosx/runtime.wasm";
    const go = new Go();
    let inst;
    if (typeof WebAssembly.instantiateStreaming === "function") {
      const stream = WebAssembly.instantiateStreaming(fetch(url), go.importObject);
      const result = await stream.catch(async function () {
        // Some servers don't return application/wasm; retry through arrayBuffer.
        const r = await fetch(url);
        const buf = await r.arrayBuffer();
        return WebAssembly.instantiate(buf, go.importObject);
      });
      inst = result.instance;
    } else {
      const r = await fetch(url);
      const buf = await r.arrayBuffer();
      const result = await WebAssembly.instantiate(buf, go.importObject);
      inst = result.instance;
    }
    // Fire-and-forget — go.run blocks on the WASM's `select {}`.
    go.run(inst);
  }

  // waitForRuntimeReady polls for the bridge globals so the rest of the
  // bootstrap doesn't fire before the WASM has installed them. The wait
  // is bounded by a 5-second timeout — past that we surface an error
  // rather than hang.
  function waitForRuntimeReady() {
    return new Promise(function (resolve, reject) {
      const start = Date.now();
      function probe() {
        if (typeof window.__gosx_hydrate_engine_surface === "function") {
          return resolve();
        }
        if (Date.now() - start > 5000) {
          return reject(new Error("/gosx/runtime.wasm did not install __gosx_hydrate_engine_surface within 5s"));
        }
        setTimeout(probe, 25);
      }
      probe();
    });
  }

  // --- Per-mount instance plumbing ------------------------------------------

  const instances = new Map();
  let _idCounter = 0;
  function nextID() { return "gosx-engine-surface-" + (++_idCounter); }

  function modBits(e) {
    return (e.shiftKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) |
           (e.altKey   ? 4 : 0) | (e.metaKey  ? 8 : 0);
  }

  function canvasPoint(canvas, ev) {
    const rect = canvas.getBoundingClientRect();
    return [ev.clientX - rect.left, ev.clientY - rect.top];
  }

  // mountSurface hydrates one <canvas data-gosx-engine-bytecode> placeholder.
  async function mountSurface(placeholder) {
    const component = placeholder.getAttribute("data-gosx-engine-component") || "";
    const bytecodeURL = placeholder.getAttribute("data-gosx-engine-bytecode") || "";
    const propsB64 = placeholder.getAttribute("data-gosx-engine-props") || "";
    const status = placeholder.getAttribute("data-gosx-engine-status") || "";
    if (!bytecodeURL || status === "missing") {
      paintMissing(placeholder, component);
      return;
    }

    let canvas = placeholder;
    if (canvas.tagName.toLowerCase() !== "canvas") {
      canvas = document.createElement("canvas");
      for (const attr of Array.from(placeholder.attributes)) {
        canvas.setAttribute(attr.name, attr.value);
      }
      placeholder.parentNode.replaceChild(canvas, placeholder);
    }
    initCanvasSize(canvas);

    let propsJSON = "{}";
    if (propsB64) {
      try { propsJSON = atob(propsB64); }
      catch (e) {
        console.warn("gosx/surface: failed to decode props for", component, e);
      }
    }

    let programData;
    try {
      const resp = await fetch(bytecodeURL);
      if (!resp.ok) throw new Error("HTTP " + resp.status);
      const buf = await resp.arrayBuffer();
      programData = new Uint8Array(buf);
    } catch (e) {
      console.error("gosx/surface: failed to fetch bytecode for", component, "from", bytecodeURL, e);
      paintMissing(canvas, component);
      return;
    }

    const id = nextID();
    const instance = { canvas: canvas, listeners: [], resizeObserver: null, observer: null, raf: 0, disposed: false };
    instances.set(id, instance);

    try {
      const result = window.__gosx_hydrate_engine_surface(id, component, propsJSON, programData, "json", canvas);
      if (typeof result === "string" && result !== "") {
        console.error("gosx/surface: hydrate", component, "failed:", result);
        instances.delete(id);
        paintMissing(canvas, component);
        return;
      }
    } catch (e) {
      console.error("gosx/surface: hydrate", component, "threw:", e);
      instances.delete(id);
      paintMissing(canvas, component);
      return;
    }

    bridgeEvents(id, canvas, instance);
    startRAF(id, instance);
  }

  function bridgeEvents(id, canvas, instance) {
    function dispatch(kind, payload, payloadStr) {
      if (instance.disposed) return;
      const fn = window.__gosx_dispatch_engine_surface_event;
      if (typeof fn !== "function") return;
      const buf = payload && payload.length > 0 ? new Float64Array(payload) : new Float64Array(0);
      fn(id, kind, buf, payloadStr || "");
    }

    function onPointer(kind) {
      return function (ev) {
        const p = canvasPoint(canvas, ev);
        dispatch(kind, [p[0], p[1], ev.button || 0, ev.buttons || 0, modBits(ev)], "");
      };
    }

    const handlers = [
      ["click",         onPointer(1)],
      ["dblclick",      onPointer(2)],
      ["pointerdown",   onPointer(3)],
      ["pointermove",   onPointer(4)],
      ["pointerup",     onPointer(5)],
      ["pointercancel", onPointer(6)],
      ["wheel", function (ev) {
        ev.preventDefault();
        const p = canvasPoint(canvas, ev);
        dispatch(7, [p[0], p[1], ev.deltaX, ev.deltaY, modBits(ev)], "");
      }],
      ["keydown", function (ev) { dispatch(8, [modBits(ev)], ev.key + "\t" + ev.code); }],
      ["keyup",   function (ev) { dispatch(9, [modBits(ev)], ev.key + "\t" + ev.code); }],
    ];
    for (const [name, fn] of handlers) {
      canvas.addEventListener(name, fn, name === "wheel" ? { passive: false } : undefined);
      instance.listeners.push([name, fn]);
    }

    if (typeof ResizeObserver === "function") {
      const ro = new ResizeObserver(function () {
        if (instance.disposed) return;
        initCanvasSize(canvas);
        const dpr = window.devicePixelRatio || 1;
        dispatch(10, [canvas.clientWidth, canvas.clientHeight, dpr], "");
      });
      ro.observe(canvas);
      instance.resizeObserver = ro;
    }

    if (typeof MutationObserver === "function" && canvas.parentNode) {
      const mo = new MutationObserver(function () {
        if (!document.contains(canvas)) {
          disposeSurface(id);
        }
      });
      mo.observe(canvas.parentNode, { childList: true, subtree: false });
      instance.observer = mo;
    }
  }

  function startRAF(id, instance) {
    const tickFn = window.__gosx_tick_engine_surface;
    if (typeof tickFn !== "function") return;
    function tick() {
      if (instance.disposed) return;
      try { tickFn(id, 1); }
      catch (e) { console.error("gosx/surface: tick threw for", id, e); }
      instance.raf = requestAnimationFrame(tick);
    }
    instance.raf = requestAnimationFrame(tick);
  }

  function initCanvasSize(canvas) {
    const dpr = window.devicePixelRatio || 1;
    const rect = canvas.getBoundingClientRect();
    const w = Math.max(1, Math.floor(rect.width));
    const h = Math.max(1, Math.floor(rect.height));
    if (canvas.width  !== w * dpr) canvas.width  = w * dpr;
    if (canvas.height !== h * dpr) canvas.height = h * dpr;
  }

  function disposeSurface(id) {
    const inst = instances.get(id);
    if (!inst || inst.disposed) return;
    inst.disposed = true;
    if (inst.raf) {
      cancelAnimationFrame(inst.raf);
      inst.raf = 0;
    }
    for (const [name, fn] of inst.listeners) {
      inst.canvas.removeEventListener(name, fn);
    }
    if (inst.resizeObserver && typeof inst.resizeObserver.disconnect === "function") {
      inst.resizeObserver.disconnect();
    }
    if (inst.observer && typeof inst.observer.disconnect === "function") {
      inst.observer.disconnect();
    }
    const fn = window.__gosx_dispose_engine_surface;
    if (typeof fn === "function") {
      try { fn(id); } catch (e) { /* swallow */ }
    }
    instances.delete(id);
  }

  function paintMissing(canvas, name) {
    try {
      if (!canvas || !canvas.getContext) return;
      const rect = canvas.getBoundingClientRect ? canvas.getBoundingClientRect() : { width: 200, height: 80 };
      const w = Math.max(80, Math.floor(rect.width));
      const h = Math.max(40, Math.floor(rect.height));
      if (canvas.width  !== w) canvas.width  = w;
      if (canvas.height !== h) canvas.height = h;
      const ctx = canvas.getContext("2d");
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

  // --- Discovery -----------------------------------------------------------

  async function discoverAndMount(root) {
    const nodes = (root || document).querySelectorAll("[data-gosx-engine-bytecode]");
    if (nodes.length === 0) return;
    try {
      await loadRuntime();
    } catch (e) {
      console.error("gosx/surface: failed to load runtime:", e);
      for (const node of nodes) paintMissing(node, node.getAttribute("data-gosx-engine-component") || "");
      return;
    }
    const promises = [];
    for (const node of nodes) {
      promises.push(mountSurface(node).catch(function (e) {
        console.error("gosx/surface: unexpected mount error:", e);
      }));
    }
    await Promise.all(promises);
  }

  if (document.readyState === "loading") {
    document.addEventListener("DOMContentLoaded", function () { discoverAndMount(); });
  } else {
    discoverAndMount();
  }

  // Expose the discoverer so dynamically-inserted placeholders can be mounted.
  globalThis.__gosx_surface_discover = discoverAndMount;
})();
