(function() {
  "use strict";

  const registerFeature = window.__gosx_register_bootstrap_feature;
  if (typeof registerFeature !== "function") {
    console.error("[gosx] runtime bootstrap feature registry missing");
    return;
  }

  registerFeature("engines", function(api) {
    const engineFactories = api.engineFactories;
    const fetchProgram = api.fetchProgram;
    const inferProgramFormat = api.inferProgramFormat;
    const loadScriptTag = api.loadScriptTag;
    const engineFrame = api.engineFrame;
    const cancelEngineFrame = api.cancelEngineFrame;
    const capabilityList = api.capabilityList;
    const requiredCapabilityList = api.requiredCapabilityList;
    const engineCapabilityStatus = api.engineCapabilityStatus;
    const applyRuntimeCapabilityState = api.applyRuntimeCapabilityState;
    const activateInputProviders = api.activateInputProviders;
    const releaseInputProviders = api.releaseInputProviders;
    const clearChildren = api.clearChildren;
    const sceneNumber = api.sceneNumber;
    const sceneBool = api.sceneBool;
    const gosxReadSharedSignal = api.gosxReadSharedSignal;
    const gosxNotifySharedSignal = api.gosxNotifySharedSignal;
    const gosxSubscribeSharedSignal = api.gosxSubscribeSharedSignal;

    // -------------------------------------------------------------------------
    // Engine-surface bytecode hydration (//gosx:engine surface)
    //
    // Post-buildsurface-deletion (gosx PR #16), engine surfaces no longer
    // ship as per-component WASM artifacts. Instead the server emits a
    // <canvas data-gosx-engine-bytecode="/gosx/engines/<name>.<hash>.json">
    // placeholder, and the shared client WASM hosts the unified VM. Each
    // mount fetches the program JSON, hands it to the shared WASM's
    // __gosx_hydrate_engine_surface entry, and bridges DOM events +
    // requestAnimationFrame back into the bridge via the four
    // __gosx_*_engine_surface globals (hydrate / dispatch / tick / dispose).
    // -------------------------------------------------------------------------

    // surfaceInstances tracks live engine-surface instances by mount id:
    //   id -> { canvas, listeners, observer, resizeObserver, raf, disposed }
    const surfaceInstances = new Map();
    let _surfaceIDCounter = 0;

    function nextSurfaceID() {
      return "gosx-engine-surface-" + (++_surfaceIDCounter);
    }

    // modBits packs shift/ctrl/alt/meta into a single byte bitfield.
    function modBits(e) {
      return (e.shiftKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.altKey ? 4 : 0) | (e.metaKey ? 8 : 0);
    }

    // mountEngineSurface mounts a single canvas placeholder that carries
    // a data-gosx-engine-bytecode URL. Fetches the program JSON, calls
    // __gosx_hydrate_engine_surface to run Mount + bind the canvas host
    // receiver, then wires DOM events, rAF, and disposal back into the
    // shared WASM via the bridge globals.
    async function mountEngineSurface(placeholder) {
      const component = placeholder.getAttribute("data-gosx-engine-component") || "";
      const bytecodeURL = placeholder.getAttribute("data-gosx-engine-bytecode") || "";
      const propsB64 = placeholder.getAttribute("data-gosx-engine-props") || "";
      const status = placeholder.getAttribute("data-gosx-engine-status") || "";
      if (!bytecodeURL || status === "missing") {
        paintEngineSurfaceMissing(placeholder, component);
        return;
      }
      const hydrateFn = window.__gosx_hydrate_engine_surface;
      if (typeof hydrateFn !== "function") {
        console.error("[gosx] __gosx_hydrate_engine_surface not available — shared WASM not loaded");
        paintEngineSurfaceMissing(placeholder, component);
        return;
      }

      // Replace placeholder with an actual <canvas> if it isn't one.
      const canvas = _ensureSurfaceCanvas(placeholder);

      const id = nextSurfaceID();
      const instance = { canvas: canvas, listeners: [], observer: null, resizeObserver: null, raf: 0, disposed: false };
      surfaceInstances.set(id, instance);

      // Decode base64 props (the renderer base64-encodes the props JSON
      // so the data-* attribute stays HTML-safe — see
      // engine/surface/surface.go's encodeProps).
      let propsJSON = "{}";
      if (propsB64) {
        try {
          propsJSON = atob(propsB64);
        } catch (e) {
          console.warn("[gosx] failed to decode props for engine surface " + component + ":", e);
          propsJSON = "{}";
        }
      }

      // Fetch the program JSON. Surface payloads are tiny (~1-100 KB per
      // ADR 0003); a single fetch + arrayBuffer is the simplest path.
      let programData;
      try {
        const resp = await fetch(bytecodeURL);
        if (!resp.ok) {
          throw new Error("HTTP " + resp.status);
        }
        // Pass the raw JSON bytes through to WASM; the bridge decodes via
        // program.DecodeJSON. Using arrayBuffer + Uint8Array keeps the
        // path consistent with the islands hydrate (decodeProgramData).
        const buf = await resp.arrayBuffer();
        programData = new Uint8Array(buf);
      } catch (e) {
        console.error("[gosx] failed to fetch engine surface bytecode " + component + " from " + bytecodeURL + ":", e);
        surfaceInstances.delete(id);
        paintEngineSurfaceMissing(canvas, component);
        return;
      }

      try {
        const result = hydrateFn(id, component, propsJSON, programData, "json", canvas);
        if (typeof result === "string" && result !== "") {
          console.error("[gosx] hydrate engine surface " + component + " failed: " + result);
          surfaceInstances.delete(id);
          paintEngineSurfaceMissing(canvas, component);
          return;
        }
      } catch (e) {
        console.error("[gosx] hydrate engine surface " + component + " threw:", e);
        surfaceInstances.delete(id);
        paintEngineSurfaceMissing(canvas, component);
        return;
      }

      _bridgeEngineSurfaceEvents(id, canvas, instance);
      _startEngineSurfaceRAF(id, instance);
    }

    function _bridgeEngineSurfaceEvents(id, canvas, instance) {
      function dispatch(kind, payload, payloadStr) {
        if (instance.disposed) return;
        const fn = window.__gosx_dispatch_engine_surface_event;
        if (typeof fn !== "function") return;
        const buf = payload && payload.length > 0 ? new Float64Array(payload) : new Float64Array(0);
        fn(id, kind, buf, payloadStr || "");
      }

      function pointerHandler(kind) {
        return function(e) {
          const rect = canvas.getBoundingClientRect();
          const x = e.clientX - rect.left;
          const y = e.clientY - rect.top;
          dispatch(kind, [x, y, e.button || 0, e.buttons || 0, modBits(e)], "");
        };
      }

      const handlers = [
        ["click",         pointerHandler(1)],
        ["dblclick",      pointerHandler(2)],
        ["pointerdown",   pointerHandler(3)],
        ["pointermove",   pointerHandler(4)],
        ["pointerup",     pointerHandler(5)],
        ["pointercancel", pointerHandler(6)],
        ["wheel", function(e) {
          e.preventDefault();
          const rect = canvas.getBoundingClientRect();
          const x = e.clientX - rect.left;
          const y = e.clientY - rect.top;
          dispatch(7, [x, y, e.deltaX, e.deltaY, modBits(e)], "");
        }],
        ["keydown", function(e) {
          dispatch(8, [modBits(e)], e.key + "\t" + e.code);
        }],
        ["keyup", function(e) {
          dispatch(9, [modBits(e)], e.key + "\t" + e.code);
        }],
      ];

      for (const [evtName, handler] of handlers) {
        canvas.addEventListener(evtName, handler, evtName === "wheel" ? { passive: false } : undefined);
        instance.listeners.push([evtName, handler]);
      }

      if (typeof ResizeObserver === "function") {
        const ro = new ResizeObserver(function() {
          if (instance.disposed) return;
          _initEngineSurfaceCanvasSize(canvas);
          const dpr = window.devicePixelRatio || 1;
          dispatch(10, [canvas.clientWidth, canvas.clientHeight, dpr], "");
        });
        ro.observe(canvas);
        instance.resizeObserver = ro;
      }

      if (typeof MutationObserver === "function" && canvas.parentNode) {
        const mo = new MutationObserver(function() {
          if (!document.contains(canvas)) {
            _disposeEngineSurface(id);
          }
        });
        mo.observe(canvas.parentNode, { childList: true, subtree: false });
        instance.observer = mo;
      }
    }

    function _startEngineSurfaceRAF(id, instance) {
      const tickFn = window.__gosx_tick_engine_surface;
      if (typeof tickFn !== "function") return;
      function tick() {
        if (instance.disposed) return;
        try {
          tickFn(id, 1);
        } catch (e) {
          // Surface owns its errors — log once and keep ticking; a buggy
          // surface shouldn't break the rest of the page.
          console.error("[gosx] engine surface tick threw for " + id + ":", e);
        }
        instance.raf = requestAnimationFrame(tick);
      }
      instance.raf = requestAnimationFrame(tick);
    }

    function _initEngineSurfaceCanvasSize(canvas) {
      const dpr = window.devicePixelRatio || 1;
      const rect = canvas.getBoundingClientRect();
      const w = Math.max(1, Math.floor(rect.width));
      const h = Math.max(1, Math.floor(rect.height));
      if (canvas.width !== w * dpr) canvas.width = w * dpr;
      if (canvas.height !== h * dpr) canvas.height = h * dpr;
    }

    // _ensureSurfaceCanvas returns the placeholder as a <canvas>, swapping a
    // non-canvas placeholder for a real canvas that carries the same
    // attributes, then sizes it for the device pixel ratio. Shared by the
    // bytecode and surface-kind mount paths.
    function _ensureSurfaceCanvas(placeholder) {
      let canvas = placeholder;
      if (canvas.tagName.toLowerCase() !== "canvas") {
        canvas = document.createElement("canvas");
        for (const attr of Array.from(placeholder.attributes)) {
          canvas.setAttribute(attr.name, attr.value);
        }
        placeholder.parentNode.replaceChild(canvas, placeholder);
      }
      _initEngineSurfaceCanvasSize(canvas);
      return canvas;
    }

    function _disposeEngineSurface(id) {
      const inst = surfaceInstances.get(id);
      if (!inst || inst.disposed) return;
      inst.disposed = true;
      if (inst.raf) {
        cancelAnimationFrame(inst.raf);
        inst.raf = 0;
      }
      for (const [evtName, handler] of inst.listeners) {
        inst.canvas.removeEventListener(evtName, handler);
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
      surfaceInstances.delete(id);
    }

    function paintEngineSurfaceMissing(el, name) {
      try {
        if (!el || !el.getContext) return;
        const rect = el.getBoundingClientRect ? el.getBoundingClientRect() : { width: 200, height: 80 };
        const w = Math.max(80, Math.floor(rect.width));
        const h = Math.max(40, Math.floor(rect.height));
        if (el.width !== w) el.width = w;
        if (el.height !== h) el.height = h;
        const ctx = el.getContext("2d");
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

    // mountAllEngineSurfaces finds every <canvas data-gosx-engine-bytecode>
    // placeholder on the page and hydrates each one.
    async function mountAllEngineSurfaces() {
      const placeholders = document.querySelectorAll("[data-gosx-engine-bytecode]");
      if (!placeholders.length) return;
      const promises = Array.from(placeholders).map(function(el) {
        return mountEngineSurface(el).catch(function(e) {
          console.error("[gosx] unexpected error mounting engine surface:", e);
        });
      });
      await Promise.all(promises);
    }

    // -------------------------------------------------------------------------
    // Surface-kind hydration (gosx.CanvasBoard and other no-code primitives)
    //
    // Server-rendered surface primitives that carry data-gosx-surface-kind but
    // NO data-gosx-engine-bytecode ship a self-describing canvas with their
    // state inlined in data-gosx-engine-props — there is no separate program
    // artifact to fetch (a static CanvasBoard is a no-code primitive). They are
    // dispatched through the UNIFIED Phase 1d entry __gosx_hydrate(surfaceKind,
    // id, componentName, propsJSON, programData, format) instead of the
    // bytecode-specific __gosx_hydrate_engine_surface. The discovery query
    // explicitly excludes [data-gosx-engine-bytecode] so the two paths never
    // double-mount the same element.
    // -------------------------------------------------------------------------

    // decodeSurfaceProps resolves the data-gosx-engine-props attribute to a JSON
    // string. gosx.CanvasBoard emits raw (HTML-escaped) JSON, which the browser
    // un-escapes when read via getAttribute, so the attribute value parses as
    // JSON directly. The engine/surface Renderer.Mount path base64-encodes the
    // same attribute (see engine/surface/surface.go encodeProps); to stay
    // tolerant of both encodings we try the value as JSON first, then fall back
    // to base64-decode. Anything unparseable degrades to "{}".
    function decodeSurfaceProps(raw, component) {
      if (!raw) return "{}";
      try {
        JSON.parse(raw);
        return raw;
      } catch (e) {
        // not raw JSON — try base64.
      }
      try {
        const decoded = atob(raw);
        JSON.parse(decoded);
        return decoded;
      } catch (e) {
        console.warn("[gosx] failed to decode props for surface " + (component || "") + ":", e);
        return "{}";
      }
    }

    // mountSurfaceKind hydrates a single data-gosx-surface-kind placeholder via
    // the unified __gosx_hydrate dispatcher. It reuses the same canvas
    // preparation, event bridging, and teardown observers as the bytecode path
    // (mountEngineSurface) — only the discovery and the hydrate-call arguments
    // differ. A static board carries no program, so a valid-empty "{}" program
    // (format "json") is passed; the canvas2d bridge tolerates empty programs
    // (see client/bridge/bridge_canvasboard_full.go DecodeCanvasBoardProgram).
    async function mountSurfaceKind(placeholder) {
      const surfaceKind = placeholder.getAttribute("data-gosx-surface-kind") || "";
      const component = placeholder.getAttribute("data-gosx-engine-component") || "";
      const status = placeholder.getAttribute("data-gosx-engine-status") || "";
      if (!surfaceKind || status === "missing") {
        paintEngineSurfaceMissing(placeholder, component);
        return;
      }
      const hydrateFn = window.__gosx_hydrate;
      if (typeof hydrateFn !== "function") {
        console.error("[gosx] __gosx_hydrate not available — shared WASM not loaded");
        paintEngineSurfaceMissing(placeholder, component);
        return;
      }

      // Replace placeholder with an actual <canvas> if it isn't one (mirrors
      // mountEngineSurface so the shared sizing/event helpers apply uniformly).
      const canvas = _ensureSurfaceCanvas(placeholder);

      const id = nextSurfaceID();
      const instance = { canvas: canvas, listeners: [], observer: null, resizeObserver: null, raf: 0, disposed: false };
      surfaceInstances.set(id, instance);

      const propsJSON = decodeSurfaceProps(placeholder.getAttribute("data-gosx-engine-props") || "", component);

      // No-code primitives ship no program artifact; pass a valid-empty program.
      try {
        const result = hydrateFn(surfaceKind, id, component, propsJSON, "{}", "json");
        if (typeof result === "string" && result !== "") {
          console.error("[gosx] hydrate surface " + component + " (" + surfaceKind + ") failed: " + result);
          surfaceInstances.delete(id);
          paintEngineSurfaceMissing(canvas, component);
          return;
        }
      } catch (e) {
        console.error("[gosx] hydrate surface " + component + " (" + surfaceKind + ") threw:", e);
        surfaceInstances.delete(id);
        paintEngineSurfaceMissing(canvas, component);
        return;
      }

      // Reuse the bytecode path's DOM event bridging + resize/teardown
      // observers. The dispatch/tick globals these install are no-ops for ids
      // the engine-surface map does not own (their contract — see
      // engine_surface_full.go), so wiring them now is harmless and lets the
      // surface-kind path light up automatically once the canvas2d reconciler
      // exposes its own tick/dispatch/dispose entries.
      _bridgeEngineSurfaceEvents(id, canvas, instance);
    }

    // mountAllSurfaceKinds finds every server-rendered surface primitive that
    // carries data-gosx-surface-kind but NOT data-gosx-engine-bytecode (the
    // latter has its own mount path) and hydrates each one.
    async function mountAllSurfaceKinds() {
      const placeholders = document.querySelectorAll("[data-gosx-surface-kind]:not([data-gosx-engine-bytecode])");
      if (!placeholders.length) return;
      const promises = Array.from(placeholders).map(function(el) {
        return mountSurfaceKind(el).catch(function(e) {
          console.error("[gosx] unexpected error mounting surface-kind placeholder:", e);
        });
      });
      await Promise.all(promises);
    }
