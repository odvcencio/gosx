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
    // Standalone surface WASM support (//gosx:engine surface)
    // -------------------------------------------------------------------------

    // surfaceInstances tracks live surface instances: id -> { go, canvas, listeners, observer, disposed }
    const surfaceInstances = new Map();
    let _surfaceIDCounter = 0;

    function nextSurfaceID() {
      return "gosx-surface-" + (++_surfaceIDCounter);
    }

    // modBits packs shift/ctrl/alt/meta into a single byte bitfield.
    function modBits(e) {
      return (e.shiftKey ? 1 : 0) | (e.ctrlKey ? 2 : 0) | (e.altKey ? 4 : 0) | (e.metaKey ? 8 : 0);
    }

    // mountStandaloneSurface mounts a single canvas placeholder that has
    // data-gosx-engine-wasm set, loading the WASM, bridging DOM events,
    // and wiring up requestAnimationFrame and dispose.
    async function mountStandaloneSurface(placeholder) {
      const component = placeholder.getAttribute("data-gosx-engine-component") || "";
      const wasmURL = placeholder.getAttribute("data-gosx-engine-wasm") || "";
      const propsJSON = placeholder.getAttribute("data-gosx-engine-props") || "{}";
      if (!wasmURL) return;

      // Replace placeholder with an actual <canvas> if not already one.
      let canvas = placeholder;
      if (canvas.tagName.toLowerCase() !== "canvas") {
        canvas = document.createElement("canvas");
        for (const attr of Array.from(placeholder.attributes)) {
          if (!attr.name.startsWith("data-gosx-engine-")) {
            canvas.setAttribute(attr.name, attr.value);
          }
        }
        placeholder.parentNode.replaceChild(canvas, placeholder);
      }

      const id = nextSurfaceID();
      const instance = { go: null, canvas: canvas, listeners: [], observer: null, disposed: false };
      surfaceInstances.set(id, instance);

      // Expose the JS->WASM bridge functions before the module is instantiated
      // so the WASM init code (which runs synchronously) can find them.
      window["__gosx_surface_register"] = function(componentName) {
        // Called by WASM init to signal the surface is ready.
        // We respond by adding event listeners and calling mount.
        _bridgeSurfaceEvents(id, componentName, canvas, instance);
        if (typeof window["__gosx_surface_mount"] === "function") {
          window["__gosx_surface_mount"](id, propsJSON, canvas);
        }
      };
      window["__gosx_surface_request_frame"] = function(surfaceID) {
        requestAnimationFrame(function(ts) {
          const inst = surfaceInstances.get(surfaceID);
          if (!inst || inst.disposed) return;
          if (typeof window["__gosx_surface_frame"] === "function") {
            window["__gosx_surface_frame"](surfaceID, ts);
          }
        });
      };

      // Fetch and instantiate the WASM.
      try {
        const go = new Go();
        instance.go = go;
        const resp = await fetch(wasmURL);
        const buf = await resp.arrayBuffer();
        const { instance: wasmInst } = await WebAssembly.instantiate(buf, go.importObject);
        go.run(wasmInst);
      } catch (e) {
        console.error("[gosx] failed to load surface WASM " + component + ":", e);
        surfaceInstances.delete(id);
      }
    }

    function _bridgeSurfaceEvents(id, _component, canvas, instance) {
      function dispatch(kind, payload, payloadStr) {
        if (instance.disposed) return;
        if (typeof window["__gosx_surface_event"] === "function") {
          window["__gosx_surface_event"](id, kind, payload || [], payloadStr || "");
        }
      }

      function onPointer(kind) {
        return function(e) {
          dispatch(kind, [e.offsetX, e.offsetY, e.button, e.buttons, modBits(e)], "");
        };
      }

      const handlers = [
        ["click",        onPointer(1)],
        ["dblclick",     onPointer(2)],
        ["pointerdown",  onPointer(3)],
        ["pointermove",  onPointer(4)],
        ["pointerup",    onPointer(5)],
        ["pointercancel",onPointer(6)],
        ["wheel", function(e) {
          e.preventDefault();
          dispatch(7, [e.offsetX, e.offsetY, e.deltaX, e.deltaY, modBits(e)], "");
        }],
        ["keydown", function(e) {
          dispatch(8, [modBits(e)], e.key + "\t" + e.code);
        }],
        ["keyup", function(e) {
          dispatch(9, [modBits(e)], e.key + "\t" + e.code);
        }],
      ];

      for (const [evtName, handler] of handlers) {
        canvas.addEventListener(evtName, handler, { passive: evtName === "wheel" ? false : true });
        instance.listeners.push([evtName, handler]);
      }

      // Resize observer — fires resize event (kind=10) and dispose on removal.
      const ro = new ResizeObserver(function(entries) {
        for (const entry of entries) {
          const rect = entry.contentRect;
          const dpr = window.devicePixelRatio || 1;
          dispatch(10, [rect.width, rect.height, dpr], "");
        }
      });
      ro.observe(canvas);

      // MutationObserver to detect canvas removal from DOM.
      const mo = new MutationObserver(function() {
        if (!document.contains(canvas)) {
          _disposeSurface(id);
        }
      });
      mo.observe(document.body, { childList: true, subtree: true });
      instance.observer = { ro: ro, mo: mo };
    }

    function _disposeSurface(id) {
      const inst = surfaceInstances.get(id);
      if (!inst || inst.disposed) return;
      inst.disposed = true;
      if (typeof window["__gosx_surface_event"] === "function") {
        window["__gosx_surface_event"](id, 11, [], "");
      }
      for (const [evtName, handler] of inst.listeners) {
        inst.canvas.removeEventListener(evtName, handler);
      }
      if (inst.observer) {
        inst.observer.ro.disconnect();
        inst.observer.mo.disconnect();
      }
      surfaceInstances.delete(id);
    }

    // mountAllSurfaceWASMs finds all canvas elements with data-gosx-engine-wasm
    // on the page and mounts each one.
    async function mountAllSurfaceWASMs() {
      const placeholders = document.querySelectorAll("[data-gosx-engine-wasm]");
      if (!placeholders.length) return;
      const promises = Array.from(placeholders).map(function(el) {
        return mountStandaloneSurface(el).catch(function(e) {
          console.error("[gosx] unexpected error mounting surface WASM:", e);
        });
      });
      await Promise.all(promises);
    }
