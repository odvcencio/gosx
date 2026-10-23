// @ts-check
// GoSX deferred Scene3D: mounts a scene after first paint and idle, and only
// when every environment gate holds. The server withholds the scene chunk tag
// for such a scene and sets a `defer` object on its manifest entry; the engines
// chunk skips it. This chunk loads only on pages with such a scene, so pages
// without one pay nothing for the probes, the scheduler or the publisher.
//
// A scene is released to the engines chunk by dropping `entry.defer` and
// calling __gosx_mount_late_engine_factory, the same path a late-registered
// factory uses.
(function() {
  "use strict";
  if (typeof window.__gosx_register_bootstrap_feature !== "function") return;
  window.__gosx_register_bootstrap_feature("scene3d-defer", function(api) {
    const SOFTWARE_RENDERER = /swiftshader|llvmpipe|softpipe|lavapipe|software|microsoft basic render|basic render driver|mesa offscreen/i;
    let hardwareGPU = null;

    function mediaMatches(query) {
      try {
        return Boolean(typeof window.matchMedia === "function" && window.matchMedia(query).matches);
      } catch (_e) {
        return false;
      }
    }

    // WebGL2 with no major performance caveat and a renderer that is not a
    // software rasterizer. The WebGL probe context is released at once.
    function detectHardwareGPU() {
      if (hardwareGPU !== null) return hardwareGPU;
      let ok = false;
      try {
        const gl = document.createElement("canvas").getContext("webgl2", { failIfMajorPerformanceCaveat: true, powerPreference: "low-power" });
        if (gl) {
          const info = gl.getExtension("WEBGL_debug_renderer_info");
          ok = !SOFTWARE_RENDERER.test(String((info ? gl.getParameter(info.UNMASKED_RENDERER_WEBGL) : gl.getParameter(gl.RENDERER)) || ""));
          const lose = gl.getExtension("WEBGL_lose_context");
          if (lose) lose.loseContext();
        }
      } catch (_e) {
        ok = false;
      }
      hardwareGPU = ok;
      return ok;
    }

    // Motion and data settings change at run time, so they are never cached.
    const GATES = {
      "hardware-gpu": detectHardwareGPU,
      "full-motion": function() { return !mediaMatches("(prefers-reduced-motion: reduce)"); },
      "full-data": function() {
        const nav = typeof navigator !== "undefined" ? navigator : null;
        const connection = nav && (nav.connection || nav.mozConnection || nav.webkitConnection);
        if (connection && (connection.saveData || /(^|-)2g$/.test(String(connection.effectiveType || "")))) return false;
        return !mediaMatches("(prefers-reduced-data: reduce)");
      },
    };

    const PUBLISHED = { webgpu: "webgpu", webgl: "webgl2", webgl2: "webgl2", canvas: "canvas2d", canvas2d: "canvas2d" };

    // publishRendererChoice records the renderer, or "none" and the gate that
    // refused it, on <html>, the $gosx.renderer signal and a gosx:renderer event.
    function publishRendererChoice(renderer, reason, source) {
      const value = { renderer: PUBLISHED[String(renderer || "")] || "none", reason: String(reason || ""), source: String(source || "") };
      const root = document.documentElement;
      if (root) {
        root.setAttribute("data-gosx-renderer", value.renderer);
        root.setAttribute("data-gosx-renderer-reason", value.reason);
      }
      api.setSharedSignalJSON("$gosx.renderer", JSON.stringify(value));
      if (typeof document.dispatchEvent === "function" && typeof CustomEvent === "function") {
        document.dispatchEvent(new CustomEvent("gosx:renderer", { detail: value }));
      }
      return value;
    }

    let generation = 0;
    const pending = new Map();
    let sceneChunk = null;

    function afterLoad() {
      return document.readyState === "complete" ? Promise.resolve() : new Promise(function(resolve) { window.addEventListener("load", function() { resolve(); }, { once: true }); });
    }
    function afterTwoFrames() {
      return new Promise(function(resolve) { api.engineFrame(function() { api.engineFrame(function() { resolve(); }); }); });
    }
    function whenIdle(timeout) {
      return new Promise(function(resolve) {
        if (typeof window.requestIdleCallback === "function") window.requestIdleCallback(function() { resolve(); }, { timeout: timeout });
        else setTimeout(resolve, 250);
      });
    }
    function bootstrapTag() {
      return document.querySelector('script[data-gosx-script="bootstrap"]');
    }
    function preload(href, as) {
      if (!href) return;
      const head = document.head || document.documentElement;
      for (const node of Array.from(head.children || [])) {
        if (String(node.tagName || "").toUpperCase() === "LINK" && node.getAttribute("href") === href) return;
      }
      const link = document.createElement("link");
      link.setAttribute("rel", "preload"); link.setAttribute("as", as); link.setAttribute("href", href);
      link.setAttribute("crossorigin", "anonymous");
      if (as === "script") link.setAttribute("referrerpolicy", "no-referrer");
      head.appendChild(link);
    }
    function emitPreloads(entry) {
      const tag = bootstrapTag();
      const get = function(name) { return (tag && tag.getAttribute(name)) || ""; };
      preload(get("data-gosx-scene3d-url"), "script");
      preload(typeof navigator !== "undefined" && navigator.gpu ? get("data-gosx-scene3d-webgpu-url") : get("data-gosx-scene3d-webgl-url"), "script");
      for (const hint of (entry.defer && entry.defer.preloads) || []) preload(hint.href, hint.as);
    }
    function propsRef(entry) {
      for (const variant of (entry.defer && entry.defer.variants) || []) {
        if (variant && variant.media && variant.propsRef && mediaMatches(variant.media)) return variant.propsRef;
      }
      return entry.propsRef || "";
    }
    // A busy server answers 503 with Retry-After. Wait that long (at most 3 s,
    // plus up to 250 ms of jitter so a crowd does not return together) and try
    // once more; after that the poster stays.
    async function fetchProps(entry, live) {
      const ref = propsRef(entry);
      if (!ref) return entry.props || {};
      let response = await fetch(ref, { credentials: "same-origin" });
      if (response.status === 503) {
        const after = Number(response.headers && response.headers.get && response.headers.get("Retry-After"));
        const wait = Math.min(Number.isFinite(after) && after > 0 ? after * 1000 : 1000, 3000) + Math.floor(Math.random() * 250);
        await new Promise(function(resolve) { setTimeout(resolve, wait); });
        if (!live()) throw new Error("page changed while waiting to retry the scene props");
        response = await fetch(ref, { credentials: "same-origin" });
      }
      if (!response.ok) throw new Error("scene props " + ref + " returned " + response.status);
      return response.json();
    }

    // The server withheld the chunk tag, so insert it, copying the sub-chunk URL
    // contract from the bootstrap tag. A page with an eager scene already has the tag.
    function loadSceneChunk() {
      if (window.__gosx_scene3d_available) return Promise.resolve();
      if (sceneChunk) return sceneChunk;
      sceneChunk = new Promise(function(resolve, reject) {
        if (document.querySelector('script[data-gosx-script="feature-scene3d"]')) {
          // A page with an eager scene already loads the chunk; wait for it.
          let tries = 0;
          const poll = function() {
            if (window.__gosx_scene3d_available) resolve();
            else if (++tries > 400) reject(new Error("scene3d chunk did not load"));
            else setTimeout(poll, 50);
          };
          poll();
          return;
        }
        const tag = bootstrapTag();
        const src = tag && tag.getAttribute("data-gosx-scene3d-url");
        if (!src) {
          reject(new Error("no scene3d chunk URL on the bootstrap tag"));
          return;
        }
        const script = document.createElement("script");
        script.setAttribute("data-gosx-script", "feature-scene3d");
        const names = typeof tag.getAttributeNames === "function" ? tag.getAttributeNames()
          : tag.attributes && typeof tag.attributes.keys === "function" ? Array.from(tag.attributes.keys()) : [];
        for (const name of names) {
          if (name.indexOf("data-gosx-scene3d-") !== 0 || name === "data-gosx-scene3d-url" || name === "data-gosx-scene3d-integrity") continue;
          script.setAttribute(name, tag.getAttribute(name));
        }
        const integrity = tag.getAttribute("data-gosx-scene3d-integrity");
        if (integrity) script.setAttribute("integrity", integrity);
        script.setAttribute("crossorigin", "anonymous");
        script.setAttribute("referrerpolicy", "no-referrer");
        const nonce = tag.nonce || tag.getAttribute("nonce");
        if (nonce) script.setAttribute("nonce", nonce);
        script.onload = function() { resolve(); };
        script.onerror = function() { reject(new Error("failed to load script: " + src)); };
        script.src = src;
        script.setAttribute("src", src);
        (document.head || document.documentElement).appendChild(script);
      });
      sceneChunk.catch(function() { sceneChunk = null; });
      return sceneChunk;
    }

    // Publish the renderer the mount settled on once the scene chunk records it.
    function publishWhenBackendKnown(mount, entry) {
      const publish = function() {
        const backend = mount.getAttribute("data-gosx-scene3d-backend");
        if (!backend) return false;
        publishRendererChoice(backend, mount.getAttribute("data-gosx-scene3d-renderer-fallback") || "selected", entry.id);
        return true;
      };
      if (publish() || typeof MutationObserver !== "function") return;
      const observer = new MutationObserver(function() { if (publish()) observer.disconnect(); });
      observer.observe(mount, { attributes: true, attributeFilter: ["data-gosx-scene3d-backend"] });
    }

    function schedule(entry) {
      const mount = document.getElementById(entry.mountId || entry.id);
      if (!mount) return;
      const defer = entry.defer;
      const mine = generation;
      let cancelled = false;
      pending.set(entry.id, function() { cancelled = true; });
      const live = function() { return !cancelled && mine === generation && document.getElementById(entry.mountId || entry.id) === mount; };
      const state = function(value) { mount.setAttribute("data-gosx-scene3d-defer-state", value); };
      const now = defer.until === "now";
      state("scheduled");
      (defer.until === "load" ? afterLoad() : Promise.resolve())
        .then(function() { return defer.delayMs > 0 ? new Promise(function(r) { setTimeout(r, defer.delayMs); }) : null; })
        .then(function() { return now ? null : afterTwoFrames(); })
        .then(function() { return now ? null : whenIdle(defer.timeoutMs > 0 ? defer.timeoutMs : 1800); })
        .then(async function() {
          if (!live()) return;
          const required = (entry.requiredCapabilities || []).map(function(name) { return String(name || "").trim().toLowerCase(); });
          const rest = required.filter(function(name) { return !GATES[name]; });
          const base = api.engineCapabilityStatus(Object.assign({}, entry, { requiredCapabilities: rest }));
          const missing = required.filter(function(name) { return GATES[name] && !GATES[name](); }).concat(base.missing);
          api.applyRuntimeCapabilityState(mount, "engine", { ok: missing.length === 0, required: required.concat(base.required.filter(function(n) { return required.indexOf(n) < 0; })), supported: base.supported, missing: missing });
          if (missing.length > 0) {
            state("gated");
            publishRendererChoice("none", missing[0], entry.id);
            console.warn("[gosx] missing required engine capabilities for " + entry.id + ": " + missing.join(", "));
            return;
          }
          state("loading");
          emitPreloads(entry);
          const loaded = await Promise.all([fetchProps(entry, live), loadSceneChunk()]);
          if (!live()) return;
          entry.props = loaded[0];
          entry.requiredCapabilities = rest;
          delete entry.defer;
          state("mounting");
          window.__gosx_mount_late_engine_factory("GoSXScene3D");
          state("mounted");
          publishWhenBackendKnown(mount, entry);
        })
        .catch(function(error) {
          if (!live()) return;
          state("failed");
          console.warn("[gosx] deferred scene " + entry.id + " did not load:", error);
        })
        .then(function() { pending.delete(entry.id); });
    }

    return {
      gates: GATES,
      detectHardwareGPU: detectHardwareGPU,
      publishRendererChoice: publishRendererChoice,
      runtimeReady(manifest) {
        for (const entry of (manifest && manifest.engines) || []) {
          if (entry && entry.component === "GoSXScene3D" && entry.defer) schedule(entry);
        }
      },
      disposePage() {
        generation += 1;
        for (const cancel of Array.from(pending.values())) cancel();
        pending.clear();
      },
    };
  });
})();
