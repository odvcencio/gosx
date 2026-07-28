(function() {
  "use strict";

  var SCENE_DOM_REGION_MAX = 16;
  var SCENE_DOM_REGION_DEFAULT_MAX = 8;
  var SCENE_DOM_REGION_SCROLL_IDLE_MS = 120;
  var sceneDOMRegionScrollActive = false;

  function sceneDOMRegionNumber(value, fallback) {
    var n = Number(value);
    return Number.isFinite(n) ? n : fallback;
  }

  function sceneDOMRegionMax(value) {
    var n = Math.floor(sceneDOMRegionNumber(value, SCENE_DOM_REGION_DEFAULT_MAX));
    if (n <= 0) n = SCENE_DOM_REGION_DEFAULT_MAX;
    return Math.max(1, Math.min(SCENE_DOM_REGION_MAX, n));
  }

  function sceneDOMRegionUniformName(value, fallback) {
    return typeof value === "string" && value.trim() ? value.trim() : fallback;
  }

  function sceneDOMRegionUniformPattern(value, fallback) {
    var pattern = typeof value === "string" ? value.trim() : "";
    if (!pattern) return fallback;
    if ((pattern.match(/%d/g) || []).length !== 1) return fallback;
    for (var i = 0; i < pattern.length; i += 1) {
      var c = pattern.charAt(i);
      if (c === "%") {
        if (pattern.charAt(i + 1) !== "d") return fallback;
        i += 1;
        continue;
      }
      if (!/[A-Za-z0-9_[\].]/.test(c)) return fallback;
    }
    return pattern;
  }

  function sceneDOMRegionFormat(pattern, index) {
    return pattern.replace("%d", String(index));
  }

  function sceneCustomPostDOMRegionsConfig(effect) {
    var raw = effect && effect.domRegions && typeof effect.domRegions === "object" ? effect.domRegions : null;
    var selector = raw && typeof raw.selector === "string" ? raw.selector.trim() : "";
    if (!selector || !effect || typeof effect.name !== "string" || !effect.name.trim()) return null;
    var uniforms = raw.uniforms && typeof raw.uniforms === "object" ? raw.uniforms : {};
    return {
      name: effect.name.trim(),
      selector: selector,
      max: sceneDOMRegionMax(raw.max),
      skipWhenHidden: raw.skipWhenHidden === true,
      suspendWhileScrolling: raw.suspendWhileScrolling === true,
      uniforms: {
        count: sceneDOMRegionUniformName(uniforms.count, "regionCount"),
        aspect: sceneDOMRegionUniformName(uniforms.aspect, "regionAspect"),
        rect: sceneDOMRegionUniformPattern(uniforms.rect, "region%dRect"),
        meta: sceneDOMRegionUniformPattern(uniforms.meta, "region%dMeta"),
      },
    };
  }

  function sceneDOMRegionRect(element) {
    if (!element || typeof element.getBoundingClientRect !== "function") {
      return { left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0 };
    }
    var rect = element.getBoundingClientRect();
    var left = sceneDOMRegionNumber(rect && rect.left, 0);
    var top = sceneDOMRegionNumber(rect && rect.top, 0);
    var width = Math.max(0, sceneDOMRegionNumber(rect && rect.width, sceneDOMRegionNumber(rect && rect.right, 0) - left));
    var height = Math.max(0, sceneDOMRegionNumber(rect && rect.height, sceneDOMRegionNumber(rect && rect.bottom, 0) - top));
    return {
      left: left,
      top: top,
      width: width,
      height: height,
      right: sceneDOMRegionNumber(rect && rect.right, left + width),
      bottom: sceneDOMRegionNumber(rect && rect.bottom, top + height),
    };
  }

  function sceneDOMRegionStyle(element) {
    if (typeof window === "undefined" || typeof window.getComputedStyle !== "function" || !element) return null;
    try {
      return window.getComputedStyle(element);
    } catch (_err) {
      return null;
    }
  }

  function sceneDOMRegionStyleValue(style, name) {
    if (!style) return "";
    if (typeof style.getPropertyValue === "function") {
      var value = style.getPropertyValue(name);
      if (value) return String(value);
    }
    var camel = String(name || "").replace(/-([a-z])/g, function(_match, letter) { return String(letter || "").toUpperCase(); });
    return style[camel] != null ? String(style[camel]) : "";
  }

  function sceneDOMRegionPixels(value) {
    if (typeof value !== "string") return 0;
    var match = value.match(/-?\d+(?:\.\d+)?/);
    return match ? Math.max(0, Number(match[0]) || 0) : 0;
  }

  function sceneDOMRegionCornerRadius(element, basis) {
    var style = sceneDOMRegionStyle(element);
    var px = Math.max(
      sceneDOMRegionPixels(sceneDOMRegionStyleValue(style, "border-top-left-radius")),
      sceneDOMRegionPixels(sceneDOMRegionStyleValue(style, "border-radius"))
    );
    return basis > 0 ? px / basis : 0;
  }

  function sceneDOMRegionHidden(element, rect) {
    if (!element) return true;
    if (rect.width <= 0 || rect.height <= 0) return true;
    var style = sceneDOMRegionStyle(element);
    var display = sceneDOMRegionStyleValue(style, "display");
    var visibility = sceneDOMRegionStyleValue(style, "visibility");
    var opacity = sceneDOMRegionNumber(sceneDOMRegionStyleValue(style, "opacity"), 1);
    return display === "none" || visibility === "hidden" || visibility === "collapse" || opacity <= 0.001;
  }

  function sceneDOMRegionOverlapPresence(target, viewport) {
    var left = Math.max(target.left, viewport.left);
    var top = Math.max(target.top, viewport.top);
    var right = Math.min(target.right, viewport.right);
    var bottom = Math.min(target.bottom, viewport.bottom);
    var area = Math.max(0, right - left) * Math.max(0, bottom - top);
    var targetArea = Math.max(0, target.width) * Math.max(0, target.height);
    if (targetArea <= 0) return 0;
    return Math.max(0, Math.min(1, area / targetArea));
  }

  function sceneDOMRegionMeasure(canvas, targets, max) {
    var viewport = sceneDOMRegionRect(canvas);
    var width = Math.max(1, viewport.width);
    var height = Math.max(1, viewport.height);
    var basis = Math.max(1, Math.min(width, height));
    var limit = sceneDOMRegionMax(max);
    var rects = new Array(limit * 4).fill(0);
    var meta = new Array(limit * 4).fill(0);
    var count = 0;
    var list = Array.isArray(targets) ? targets : [];
    for (var i = 0; i < list.length && count < limit; i += 1) {
      var element = list[i];
      var rect = sceneDOMRegionRect(element);
      var presence = sceneDOMRegionHidden(element, rect) ? 0 : sceneDOMRegionOverlapPresence(rect, viewport);
      if (presence <= 0) continue;
      var centerX = ((rect.left + rect.right) * 0.5 - viewport.left) / width;
      var centerY = ((rect.top + rect.bottom) * 0.5 - viewport.top) / height;
      var base = count * 4;
      rects[base] = centerX;
      rects[base + 1] = centerY;
      rects[base + 2] = rect.width / width * 0.5;
      rects[base + 3] = rect.height / height * 0.5;
      meta[base] = sceneDOMRegionCornerRadius(element, basis);
      meta[base + 1] = presence;
      meta[base + 2] = i;
      meta[base + 3] = 0;
      count += 1;
    }
    return {
      count: count,
      aspect: width / height,
      rects: rects,
      meta: meta,
    };
  }

  function sceneDOMRegionPatch(config, measurement) {
    var uniforms = {};
    uniforms[config.uniforms.count] = measurement.count;
    uniforms[config.uniforms.aspect] = measurement.aspect;
    for (var i = 0; i < config.max; i += 1) {
      var base = i * 4;
      uniforms[sceneDOMRegionFormat(config.uniforms.rect, i)] = [
        measurement.rects[base] || 0,
        measurement.rects[base + 1] || 0,
        measurement.rects[base + 2] || 0,
        measurement.rects[base + 3] || 0,
      ];
      uniforms[sceneDOMRegionFormat(config.uniforms.meta, i)] = [
        measurement.meta[base] || 0,
        measurement.meta[base + 1] || 0,
        measurement.meta[base + 2] || 0,
        measurement.meta[base + 3] || 0,
      ];
    }
    return { name: config.name, uniforms: uniforms };
  }

  function sceneCustomPostDOMRegionsVisible(effect) {
    var config = sceneCustomPostDOMRegionsConfig(effect);
    if (!config) return true;
    if (config.suspendWhileScrolling && sceneDOMRegionScrollActive) return false;
    if (!config.skipWhenHidden) return true;
    var uniforms = effect && effect.uniforms && typeof effect.uniforms === "object" ? effect.uniforms : {};
    var count = Math.floor(sceneDOMRegionNumber(uniforms[config.uniforms.count], 0));
    if (count <= 0) return false;
    var limit = Math.min(count, config.max);
    for (var i = 0; i < limit; i += 1) {
      var meta = uniforms[sceneDOMRegionFormat(config.uniforms.meta, i)];
      var presence = Array.isArray(meta) || (meta && typeof meta.length === "number")
        ? sceneDOMRegionNumber(meta[1], 0)
        : 0;
      if (presence > 0) return true;
    }
    return false;
  }

  function sceneCustomPostDOMRegionsFilterEffects(effects) {
    if (!Array.isArray(effects) || effects.length === 0) return [];
    var out = null;
    for (var i = 0; i < effects.length; i += 1) {
      var effect = effects[i];
      if (sceneCustomPostDOMRegionsVisible(effect)) {
        if (out) out.push(effect);
      } else if (!out) {
        out = effects.slice(0, i);
      }
    }
    return out || effects;
  }

  function createSceneCustomPostDOMRegionTracker(mount, canvas, state, scheduleRender) {
    var disposed = false;
    var raf = null;
    var scrollIdleTimer = null;
    var configs = [];
    var key = "";
    var lastPatchKey = "";
    var resizeObserver = null;
    var observed = [];
    var scheduledRender = typeof scheduleRender === "function" ? scheduleRender : function() {};

    function currentCanvas() {
      if (typeof canvas === "function") {
        var resolved = canvas();
        if (resolved) return resolved;
      } else if (canvas) {
        return canvas;
      }
      if (mount && typeof mount.querySelector === "function") {
        return mount.querySelector("[data-gosx-scene3d-canvas]");
      }
      return mount;
    }

    function disconnectObserved() {
      if (resizeObserver && typeof resizeObserver.disconnect === "function") {
        resizeObserver.disconnect();
      }
      observed = [];
    }

    function observeTargets(targets) {
      if (!resizeObserver || typeof resizeObserver.observe !== "function") return;
      disconnectObserved();
      if (mount) {
        resizeObserver.observe(mount);
        observed.push(mount);
      }
      var activeCanvas = currentCanvas();
      if (activeCanvas) {
        resizeObserver.observe(activeCanvas);
        observed.push(activeCanvas);
      }
      for (var i = 0; i < targets.length; i += 1) {
        resizeObserver.observe(targets[i]);
        observed.push(targets[i]);
      }
    }

    function queryTargets(selector) {
      if (typeof document === "undefined" || typeof document.querySelectorAll !== "function") return [];
      try {
        return Array.prototype.slice.call(document.querySelectorAll(selector));
      } catch (_err) {
        return [];
      }
    }

    function measureNow() {
      raf = null;
      if (disposed || configs.length === 0) return;
      var entries = [];
      var targetKeyParts = [];
      var allTargets = [];
      var activeCanvas = currentCanvas();
      var visibleCount = 0;
      for (var i = 0; i < configs.length; i += 1) {
        var config = configs[i];
        var targets = queryTargets(config.selector);
        var measurement = sceneDOMRegionMeasure(activeCanvas || mount, targets, config.max);
        allTargets = allTargets.concat(targets);
        targetKeyParts.push(config.name + ":" + config.selector + ":" + targets.length);
        visibleCount += measurement.count;
        entries.push(sceneDOMRegionPatch(config, measurement));
      }
      observeTargets(allTargets);
      var patchKey = JSON.stringify(entries);
      if (patchKey === lastPatchKey) return;
      lastPatchKey = patchKey;
      if (typeof applyScenePostUniformsCommand === "function") {
        applyScenePostUniformsCommand(state, { effects: entries });
      }
      scheduledRender("custom-post-dom-regions");
      if (mount && typeof mount.setAttribute === "function") {
        mount.setAttribute("data-gosx-scene3d-dom-regions", String(entries.length));
        mount.setAttribute("data-gosx-scene3d-dom-region-visible-count", String(visibleCount));
        mount.setAttribute("data-gosx-scene3d-dom-region-targets", targetKeyParts.join("|"));
      }
    }

    function scheduleMeasure() {
      if (disposed || configs.length === 0 || raf != null) return;
      var rafFn = typeof window !== "undefined" && typeof window.requestAnimationFrame === "function"
        ? window.requestAnimationFrame.bind(window)
        : function(callback) { return setTimeout(function() { callback(Date.now()); }, 0); };
      raf = rafFn(measureNow);
    }

    function hasScrollSuspendedConfig() {
      for (var i = 0; i < configs.length; i += 1) {
        if (configs[i] && configs[i].suspendWhileScrolling) return true;
      }
      return false;
    }

    function setScrollSuspended(value) {
      sceneDOMRegionScrollActive = value === true;
      if (mount && typeof mount.setAttribute === "function") {
        mount.setAttribute("data-gosx-scene3d-dom-regions-suspended", sceneDOMRegionScrollActive ? "true" : "false");
      }
    }

    function configure(postEffects) {
      var next = [];
      var effects = Array.isArray(postEffects) ? postEffects : [];
      for (var i = 0; i < effects.length; i += 1) {
        var config = sceneCustomPostDOMRegionsConfig(effects[i]);
        if (config) next.push(config);
      }
      var nextKey = JSON.stringify(next);
      if (nextKey === key) return;
      key = nextKey;
      configs = next;
      lastPatchKey = "";
      disconnectObserved();
      scheduleMeasure();
    }

    function onGeometryChange() {
      lastPatchKey = "";
      scheduleMeasure();
    }

    function onScroll() {
      if (disposed) return;
      if (!hasScrollSuspendedConfig()) {
        onGeometryChange();
        return;
      }
      if (raf != null) {
        var cancel = typeof window !== "undefined" && typeof window.cancelAnimationFrame === "function"
          ? window.cancelAnimationFrame.bind(window)
          : clearTimeout;
        cancel(raf);
        raf = null;
      }
      setScrollSuspended(true);
      scheduledRender("custom-post-dom-regions-scroll-suspended");
      if (scrollIdleTimer != null) clearTimeout(scrollIdleTimer);
      scrollIdleTimer = setTimeout(function() {
        scrollIdleTimer = null;
        if (disposed) return;
        setScrollSuspended(false);
        lastPatchKey = "";
        scheduleMeasure();
      }, SCENE_DOM_REGION_SCROLL_IDLE_MS);
    }

    if (typeof ResizeObserver === "function") {
      resizeObserver = new ResizeObserver(onGeometryChange);
    }
    if (typeof window !== "undefined" && typeof window.addEventListener === "function") {
      window.addEventListener("scroll", onScroll, true);
      window.addEventListener("resize", onGeometryChange);
    }

    configure(state && state.postEffects);

    return {
      configure: configure,
      schedule: scheduleMeasure,
      dispose: function() {
        disposed = true;
        disconnectObserved();
        if (raf != null) {
          var cancel = typeof window !== "undefined" && typeof window.cancelAnimationFrame === "function"
            ? window.cancelAnimationFrame.bind(window)
            : clearTimeout;
          cancel(raf);
          raf = null;
        }
        if (scrollIdleTimer != null) {
          clearTimeout(scrollIdleTimer);
          scrollIdleTimer = null;
        }
        setScrollSuspended(false);
        if (typeof window !== "undefined" && typeof window.removeEventListener === "function") {
          window.removeEventListener("scroll", onScroll, true);
          window.removeEventListener("resize", onGeometryChange);
        }
      },
      _measureNow: measureNow,
    };
  }

  if (typeof window !== "undefined") {
    window.__gosx_scene3d_dom_regions = {
      config: sceneCustomPostDOMRegionsConfig,
      measure: sceneDOMRegionMeasure,
      customPostVisible: sceneCustomPostDOMRegionsVisible,
      filterEffects: sceneCustomPostDOMRegionsFilterEffects,
      scrollActive: function() { return sceneDOMRegionScrollActive; },
      createTracker: createSceneCustomPostDOMRegionTracker,
    };
    if (window.__gosx_scene3d_api) {
      window.__gosx_scene3d_api.sceneCustomPostDOMRegionsVisible = sceneCustomPostDOMRegionsVisible;
      window.__gosx_scene3d_api.sceneCustomPostDOMRegionsFilterEffects = sceneCustomPostDOMRegionsFilterEffects;
    }
  }
  if (typeof globalThis !== "undefined") {
    globalThis.createSceneCustomPostDOMRegionTracker = createSceneCustomPostDOMRegionTracker;
    globalThis.sceneCustomPostDOMRegionsVisible = sceneCustomPostDOMRegionsVisible;
    globalThis.sceneCustomPostDOMRegionsFilterEffects = sceneCustomPostDOMRegionsFilterEffects;
  }
})();
