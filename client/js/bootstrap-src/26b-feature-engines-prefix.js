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
      // Tear down the matching WASM-side instance. canvas2d boards live in the
      // CanvasBoardAdapter map (__gosx_dispose_canvas); every other surface
      // kind (and the bytecode path) lives in the engine-surface map
      // (__gosx_dispose_engine_surface). Both globals are no-ops for ids they
      // don't own, so calling the wrong one is harmless — but routing by kind
      // keeps the teardown precise.
      const disposeName = inst.kind === "canvas2d"
        ? "__gosx_dispose_canvas"
        : "__gosx_dispose_engine_surface";
      const fn = window[disposeName];
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

      // canvas2d boards own a distinct input + paint path: their pointer/
      // wheel/click events route through __gosx_canvas_event (camera pan/zoom
      // + pick), NOT the engine-surface dispatcher, and they paint through the
      // bundle-driven render loop below (tick → render → paintCanvasBundle on
      // the 2D context). Every OTHER surface kind — and the bytecode/GPU
      // engine-surface path — keeps the engine-surface DOM bridging (its
      // dispatch/tick globals are no-ops for ids the engine-surface map does
      // not own, so the wiring is harmless there) and owns its own GPU draw
      // loop (engine_surface_full.go).
      if (surfaceKind === "canvas2d") {
        instance.kind = "canvas2d";
        // Expose the WASM-side board id on the element so external callers
        // (tooling, e2e harnesses) can address this board's __gosx_render_canvas
        // / __gosx_canvas_event without reaching into the closure-scoped
        // surfaceInstances map. Read-only handle; nothing in the runtime keys
        // off it.
        try { canvas.setAttribute("data-gosx-surface-id", id); } catch (e) { /* tolerate */ }
        _bridgeCanvasBoardEvents(id, canvas, instance);
        _startCanvasSurfaceRAF(id, canvas, instance);
      } else {
        _bridgeEngineSurfaceEvents(id, canvas, instance);
      }
    }

    // _startCanvasSurfaceRAF drives the canvas2d paint loop for a hydrated
    // gosx.CanvasBoard. Each frame it (cheaply) reconciles the board via
    // __gosx_tick_canvas, fetches the RenderBundle JSON via __gosx_render_canvas
    // at the canvas's CSS size, parses it, and replays it onto the 2D context
    // through the shared painter (window.__gosx_paint_canvas_bundle, installed
    // by bootstrap-src/26b1-canvas2d-painter.js). The backing store is sized
    // for the device pixel ratio (via _ensureSurfaceCanvas / the ResizeObserver),
    // and the context is pre-scaled by dpr so the painter can work in CSS pixels.
    function _startCanvasSurfaceRAF(id, canvas, instance) {
      const renderFn = window.__gosx_render_canvas;
      const paintFn = window.__gosx_paint_canvas_bundle;
      if (typeof renderFn !== "function" || typeof paintFn !== "function") {
        // Shared WASM or painter not present — leave the placeholder as-is.
        return;
      }
      const tickFn = window.__gosx_tick_canvas;
      let ctx = null;
      try {
        ctx = canvas.getContext("2d");
      } catch (e) {
        ctx = null;
      }
      if (!ctx) return;

      function frame() {
        if (instance.disposed) return;
        const dpr = window.devicePixelRatio || 1;
        // CSS (logical) size the OrthoCamera2D transform centers on. Fall back
        // to the backing-store size divided by dpr when layout is unavailable.
        const cssW = Math.max(1, canvas.clientWidth || Math.floor(canvas.width / dpr) || 1);
        const cssH = Math.max(1, canvas.clientHeight || Math.floor(canvas.height / dpr) || 1);
        try {
          if (typeof tickFn === "function") {
            tickFn(id);
          }
          const json = renderFn(id, cssW, cssH, frame._t || 0);
          frame._t = (frame._t || 0) + 1 / 60;
          if (typeof json === "string" && json !== "" && json[0] !== "e") {
            const bundle = JSON.parse(json);
            // Pre-scale the context so the painter draws in CSS pixels onto a
            // dpr-sized backing store. setTransform resets any prior scale.
            if (typeof ctx.setTransform === "function") {
              ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
            }
            paintFn(ctx, bundle, cssW, cssH, dpr);
          }
        } catch (e) {
          // A buggy board shouldn't break the rest of the page — log once a
          // frame and keep going. (The render-loop owns its errors, mirroring
          // _startEngineSurfaceRAF.)
          console.error("[gosx] canvas2d paint loop threw for " + id + ":", e);
        }
        instance.raf = requestAnimationFrame(frame);
      }
      instance.raf = requestAnimationFrame(frame);
    }

    // Canvas2D interaction event kinds — must match bridge.CanvasBoardEventKind
    // (client/bridge/bridge_canvasboard_event_full.go). The integers cross the
    // JS↔WASM boundary as __gosx_canvas_event's second argument.
    const CANVAS_EVENT_PAN = 1;
    const CANVAS_EVENT_ZOOM = 2;
    const CANVAS_EVENT_PICK = 3;

    // Sub-threshold pointer travel (CSS px) below which a press→release counts
    // as a click (→ pick) rather than a drag (→ pan). Matches the Figma-style
    // "a tiny wobble during a click is still a click" affordance.
    const CANVAS_CLICK_SLOP = 4;

    // _bridgeCanvasBoardEvents wires a canvas2d board's pointer/wheel input into
    // the WASM camera + pick path via window.__gosx_canvas_event(id, kind, …):
    // drag-to-pan, wheel-to-zoom (toward cursor), click-to-pick (press+release
    // under the slop). It also installs the SAME resize + MutationObserver
    // teardown observers the engine-surface path uses, so _disposeEngineSurface
    // cleans a canvas2d board up identically. Coordinates are CSS-logical pixels
    // (clientX/Y minus the canvas rect), matching the OrthoCamera2D transform.
    function _bridgeCanvasBoardEvents(id, canvas, instance) {
      function emit(kind, floats) {
        if (instance.disposed) return;
        const fn = window.__gosx_canvas_event;
        if (typeof fn !== "function") return;
        try {
          fn(id, kind, new Float64Array(floats), "");
        } catch (e) {
          console.error("[gosx] __gosx_canvas_event threw for " + id + ":", e);
        }
      }

      function cssSize() {
        const dpr = window.devicePixelRatio || 1;
        const w = Math.max(1, canvas.clientWidth || Math.floor(canvas.width / dpr) || 1);
        const h = Math.max(1, canvas.clientHeight || Math.floor(canvas.height / dpr) || 1);
        return { w: w, h: h };
      }

      // Drag state: active pointer, last point we panned from (delta source),
      // press origin, and whether travel crossed the slop (a drag suppresses
      // the click→pick on release).
      let activePointer = null;
      let lastX = 0;
      let lastY = 0;
      let pressX = 0;
      let pressY = 0;
      let dragged = false;

      function localPoint(e) {
        const rect = canvas.getBoundingClientRect();
        return { x: e.clientX - rect.left, y: e.clientY - rect.top };
      }

      const onPointerDown = function(e) {
        // Primary button / touch / pen only — let middle/right pass through.
        if (e.button !== undefined && e.button !== 0) return;
        const p = localPoint(e);
        activePointer = e.pointerId !== undefined ? e.pointerId : 0;
        lastX = p.x;
        lastY = p.y;
        pressX = p.x;
        pressY = p.y;
        dragged = false;
        if (typeof canvas.setPointerCapture === "function" && e.pointerId !== undefined) {
          try { canvas.setPointerCapture(e.pointerId); } catch (err) { /* tolerate */ }
        }
      };

      const onPointerMove = function(e) {
        if (activePointer === null) return;
        if (e.pointerId !== undefined && e.pointerId !== activePointer) return;
        const p = localPoint(e);
        const dx = p.x - lastX;
        const dy = p.y - lastY;
        if (dx === 0 && dy === 0) return;
        if (Math.abs(p.x - pressX) > CANVAS_CLICK_SLOP || Math.abs(p.y - pressY) > CANVAS_CLICK_SLOP) {
          dragged = true;
        }
        lastX = p.x;
        lastY = p.y;
        // pan payload: [dxScreen, dyScreen, _, _]
        emit(CANVAS_EVENT_PAN, [dx, dy, 0, 0]);
      };

      const onPointerUp = function(e) {
        if (activePointer === null) return;
        if (e.pointerId !== undefined && e.pointerId !== activePointer) return;
        const p = localPoint(e);
        const wasDrag = dragged;
        activePointer = null;
        if (typeof canvas.releasePointerCapture === "function" && e.pointerId !== undefined) {
          try { canvas.releasePointerCapture(e.pointerId); } catch (err) { /* tolerate */ }
        }
        // Sub-slop press+release = a click → pick. A real drag already panned.
        if (!wasDrag) {
          const sz = cssSize();
          emit(CANVAS_EVENT_PICK, [p.x, p.y, sz.w, sz.h]);
        }
      };

      const onPointerCancel = function(e) {
        if (e.pointerId !== undefined && e.pointerId !== activePointer) return;
        activePointer = null;
        dragged = false;
      };

      const onWheel = function(e) {
        e.preventDefault();
        const p = localPoint(e);
        const sz = cssSize();
        // deltaY < 0 (wheel up) zooms IN. A multiplicative factor keeps zoom
        // uniform across scales; the small exponent stops a notch over-shooting.
        let dy = e.deltaY;
        if (e.deltaMode === 1) dy *= 16; // lines
        else if (e.deltaMode === 2) dy *= sz.h; // pages
        const factor = Math.exp(-dy * 0.0015);
        // zoom payload: [factor, cursorX, cursorY, cssW, cssH]
        emit(CANVAS_EVENT_ZOOM, [factor, p.x, p.y, sz.w, sz.h]);
      };

      const handlers = [
        ["pointerdown", onPointerDown],
        ["pointermove", onPointerMove],
        ["pointerup", onPointerUp],
        ["pointercancel", onPointerCancel],
        ["wheel", onWheel],
      ];
      for (const [evtName, handler] of handlers) {
        canvas.addEventListener(evtName, handler, evtName === "wheel" ? { passive: false } : undefined);
        instance.listeners.push([evtName, handler]);
      }
      // touch-action:none lets pointer events drive pan/zoom without the
      // browser hijacking the gesture for native scroll/zoom.
      try { canvas.style.touchAction = "none"; } catch (e) { /* tolerate */ }

      // Resize + teardown observers — identical contract to the engine-surface
      // path so _disposeEngineSurface cleans canvas2d boards up the same way.
      if (typeof ResizeObserver === "function") {
        const ro = new ResizeObserver(function() {
          if (instance.disposed) return;
          _initEngineSurfaceCanvasSize(canvas);
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
