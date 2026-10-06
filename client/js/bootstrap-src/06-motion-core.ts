// GoSX motion values and the shared browser frame scheduler.
(function() {
  "use strict";

  const gosx = window.__gosx || (window.__gosx = {});
  if (gosx.motion && gosx.motion.scheduler) return;
  const motion = gosx.motion || (gosx.motion = {});
  const scheduler = createMotionScheduler();
  const namedValues = new Map();
  const namedListeners = new Map();
  const programRecords = new Map();
  const sceneAdapters = new Map();
  let programObserver = null;
  let nextAnimationID = 0;

  function motionNumber(value, fallback) {
    const number = Number(value);
    return Number.isFinite(number) ? number : fallback;
  }

  function motionNow() {
    return typeof performance !== "undefined" && typeof performance.now === "function"
      ? performance.now()
      : Date.now();
  }

  function motionClamp(value, min, max) {
    return Math.max(min, Math.min(max, value));
  }

  function motionClone(value) {
    return Array.isArray(value) ? value.slice() : value;
  }

  function motionSame(a, b) {
    if (Array.isArray(a) || Array.isArray(b)) {
      if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false;
      for (let i = 0; i < a.length; i++) if (!motionSame(a[i], b[i])) return false;
      return true;
    }
    return Object.is(a, b);
  }

  const motionCSSUnits = new Set(["", "px", "%", "em", "rem", "vh", "vw", "vmin", "vmax", "deg", "rad", "turn", "ms", "s"]);
  const motionStyleProperties = new Set([
    "opacity", "color", "background", "backgroundColor", "background-color", "transform",
    "transform.x", "transform.y", "transform.z", "transform.scale", "transform.rotate", "transform.rotation",
    "width", "height", "minWidth", "maxWidth", "minHeight", "maxHeight", "top", "right", "bottom", "left",
    "margin", "marginTop", "marginRight", "marginBottom", "marginLeft", "padding", "paddingTop", "paddingRight",
    "paddingBottom", "paddingLeft", "borderRadius", "filter", "clipPath", "letterSpacing", "wordSpacing",
  ]);
  const motionSceneProperties = new Set([
    "x", "y", "z", "position.x", "position.y", "position.z", "rotation.x", "rotation.y", "rotation.z",
    "rotationX", "rotationY", "rotationZ", "scale.x", "scale.y", "scale.z", "opacity",
  ]);
  const motionCameraProperties = new Set([
    "x", "y", "z", "position.x", "position.y", "position.z", "rotation.x", "rotation.y", "rotation.z",
    "rotationX", "rotationY", "rotationZ", "fov", "near", "far", "zoom",
  ]);

  function motionBindingIsValid(binding) {
    if (!binding || typeof binding !== "object") return false;
    if ((binding.target != null && typeof binding.target !== "string") || typeof binding.property !== "string" ||
        (binding.unit != null && typeof binding.unit !== "string")) return false;
    const target = binding.target || "style";
    const property = binding.property;
    const unit = binding.unit || "";
    if (property !== property.trim() || unit !== unit.trim() || !motionCSSUnits.has(unit)) return false;
    if (target === "style") return motionStyleProperties.has(property);
    if (target === "cssVar") return /^--[A-Za-z_][A-Za-z0-9_-]{0,125}$/.test(property);
    if (target === "sceneNode") return motionSceneProperties.has(property) && typeof binding.node === "string" && Boolean(binding.node.trim()) && unit === "";
    if (target === "materialUniform") return /^[A-Za-z_][A-Za-z0-9_]{0,63}$/.test(property) && typeof binding.node === "string" && Boolean(binding.node.trim()) && unit === "";
    if (target === "camera") return motionCameraProperties.has(property) && unit === "";
    return false;
  }

  function motionMix(a, b, t) {
    if (Array.isArray(a) && Array.isArray(b)) {
      const out = new Array(Math.min(a.length, b.length));
      for (let i = 0; i < out.length; i++) out[i] = a[i] + (b[i] - a[i]) * t;
      return out;
    }
    return motionNumber(a, 0) + (motionNumber(b, 0) - motionNumber(a, 0)) * t;
  }

  // Piecewise scalar curve used by "curve" signals and camera rails. The math
  // mirrors motion.CurveValue in Go; motion/testdata/curve_golden.json pins both.
  function motionCurveStops(frames) {
    const stops = [];
    for (const frame of Array.isArray(frames) ? frames : []) {
      const at = frame && typeof frame.at === "number" ? frame.at : NaN;
      const value = frame && typeof frame.value === "number" ? frame.value : NaN;
      if (!Number.isFinite(at) || !Number.isFinite(value)) return [];
      if (stops.length && !(at > stops[stops.length - 1].at)) return [];
      stops.push({ at: at, value: value });
    }
    return stops.length >= 2 ? stops : [];
  }

  // Monotone-cubic (Fritsch-Carlson) tangent at stop k; mirrors curveTangent in Go.
  function motionCurveTangent(stops, k) {
    const n = stops.length;
    if (n < 2) return 0;
    const delta = function(i) { return (stops[i + 1].value - stops[i].value) / (stops[i + 1].at - stops[i].at); };
    if (n === 2) return delta(0);
    if (k > 0 && k < n - 1) {
      const d0 = delta(k - 1), d1 = delta(k);
      if (d0 * d1 <= 0) return 0;
      const h0 = stops[k].at - stops[k - 1].at, h1 = stops[k + 1].at - stops[k].at;
      const w1 = 2 * h1 + h0, w2 = h1 + 2 * h0;
      return (w1 + w2) / (w1 / d0 + w2 / d1);
    }
    let h0, h1, d0, d1;
    if (k === 0) {
      h0 = stops[1].at - stops[0].at; h1 = stops[2].at - stops[1].at; d0 = delta(0); d1 = delta(1);
    } else {
      h0 = stops[n - 1].at - stops[n - 2].at; h1 = stops[n - 2].at - stops[n - 3].at; d0 = delta(n - 2); d1 = delta(n - 3);
    }
    const m = ((2 * h0 + h1) * d0 - h0 * d1) / (h0 + h1);
    if (m * d0 <= 0) return 0;
    if (d0 * d1 <= 0 && Math.abs(m) > 3 * Math.abs(d0)) return 3 * d0;
    return m;
  }

  function motionCurveValue(stops, smooth, x) {
    const n = stops.length;
    if (n === 0) return 0;
    if (x <= stops[0].at) return stops[0].value;
    if (x >= stops[n - 1].at) return stops[n - 1].value;
    let i = 0;
    while (i < n - 2 && x >= stops[i + 1].at) i++;
    const a = stops[i], b = stops[i + 1];
    const h = b.at - a.at, t = (x - a.at) / h;
    if (!smooth) return a.value + (b.value - a.value) * t;
    const ma = motionCurveTangent(stops, i), mb = motionCurveTangent(stops, i + 1);
    const t2 = t * t, t3 = t2 * t;
    return (2 * t3 - 3 * t2 + 1) * a.value + (t3 - 2 * t2 + t) * h * ma + (-2 * t3 + 3 * t2) * b.value + (t3 - t2) * h * mb;
  }

  function createMotionScheduler() {
    const phases = { read: new Set(), evaluate: new Set(), write: new Set() };
    const rectRecords = new Map();
    const continuous = new Set();
    const sceneRecords = new Set();
    let frameCallbacks = new Map();
    let writes = new Map();
    let nextID = 0;
    let frameHandle = null;
    let activeFrameCallbacks = null;
    let frameKind = "raf";
    let inFrame = false;
    let lastFrameMS = null;
    let currentFrameMS = 0;
    let deltaSeconds = 0;
    let scrollX = motionNumber(window.scrollX, 0);
    let scrollY = motionNumber(window.scrollY, 0);
    const metrics = {
      frames: 0,
      lastFrameMS: 0,
      maxFrameMS: 0,
      maxReadMS: 0,
      maxEvaluateMS: 0,
      maxWriteMS: 0,
      maxRenderMS: 0,
    };

    function clockNow() {
      return typeof performance !== "undefined" && typeof performance.now === "function"
        ? performance.now()
        : Date.now();
    }

    function hasWork() {
      if (frameCallbacks.size || writes.size || continuous.size) return true;
      for (const item of sceneRecords) if (item.active || item.dirty) return true;
      return false;
    }

    function cancelScheduled() {
      lastFrameMS = null;
      if (frameHandle == null) return;
      if (frameKind === "raf" && typeof window.cancelAnimationFrame === "function") {
        window.cancelAnimationFrame(frameHandle);
      } else {
        clearTimeout(frameHandle);
      }
      frameHandle = null;
    }

    function wake() {
      if (inFrame) return;
      if (frameHandle != null) return;
      if (typeof window.requestAnimationFrame === "function") {
        frameKind = "raf";
        frameHandle = window.requestAnimationFrame(runFrame);
      } else {
        frameKind = "timeout";
        frameHandle = setTimeout(function() { runFrame(Date.now()); }, 16);
      }
    }

    function request(callback) {
      if (typeof callback !== "function") return 0;
      const id = ++nextID;
      frameCallbacks.set(id, callback);
      wake();
      return id;
    }

    function cancel(id) {
      if (id == null || id === 0) return;
      frameCallbacks.delete(id);
      if (activeFrameCallbacks) activeFrameCallbacks.delete(id);
      if (!hasWork()) cancelScheduled();
    }

    function on(phase, callback) {
      const set = phases[phase];
      if (!set || typeof callback !== "function") return function() {};
      set.add(callback);
      return function() { set.delete(callback); };
    }

    function addContinuous(callback) {
      if (typeof callback !== "function") return function() {};
      continuous.add(callback);
      wake();
      return function() {
        continuous.delete(callback);
        if (!hasWork()) cancelScheduled();
      };
    }

    function queueWrite(key, callback) {
      if (typeof callback !== "function") return;
      writes.set(key || callback, callback);
      wake();
    }

    function registerScene(callback) {
      if (typeof callback !== "function") return null;
      const record = { callback: callback, active: false, dirty: false, disposed: false };
      sceneRecords.add(record);
      return {
        setActive: function(active) {
          if (record.disposed) return;
          record.active = Boolean(active);
          if (record.active) wake();
          else if (!hasWork()) cancelScheduled();
        },
        invalidate: function() {
          if (record.disposed) return;
          record.dirty = true;
          wake();
        },
        clearPending: function() {
          if (record.disposed) return;
          record.dirty = false;
          if (!hasWork()) cancelScheduled();
        },
        dispose: function() {
          if (record.disposed) return;
          record.disposed = true;
          record.active = false;
          record.dirty = false;
          sceneRecords.delete(record);
          if (!hasWork()) cancelScheduled();
        },
        active: function() { return record.active; },
      };
    }

    function measure(record) {
      const element = record && record.element;
      if (!element || typeof element.getBoundingClientRect !== "function") return;
      const rect = element.getBoundingClientRect();
      const x = motionNumber(window.scrollX, 0);
      const y = motionNumber(window.scrollY, 0);
      record.left = motionNumber(rect.left, 0) + (record.fixed ? 0 : x);
      record.top = motionNumber(rect.top, 0) + (record.fixed ? 0 : y);
      record.width = Math.max(0, motionNumber(rect.width, rect.right - rect.left));
      record.height = Math.max(0, motionNumber(rect.height, rect.bottom - rect.top));
      record.ready = true;
      record.dirty = false;
    }

    function rectPositionAncestry(element) {
      let fixed = false;
      let sticky = false;
      for (let node = element; node && node.nodeType === 1; node = node.parentElement) {
        let position = "";
        try { position = window.getComputedStyle ? window.getComputedStyle(node).position : ""; } catch (_error) {}
        if (position === "fixed") fixed = true;
        if (position === "sticky" || position === "-webkit-sticky") sticky = true;
        if (fixed && sticky) break;
      }
      return { fixed: fixed, sticky: sticky };
    }

    function observeRect(element) {
      if (!element || typeof element !== "object") return function() {};
      let record = rectRecords.get(element);
      if (!record) {
        const position = rectPositionAncestry(element);
        record = { element: element, left: 0, top: 0, width: 0, height: 0, ready: false, dirty: true, observer: null, users: 0, fixed: position.fixed, sticky: position.sticky };
        rectRecords.set(element, record);
        if (typeof ResizeObserver === "function") {
          try {
            record.observer = new ResizeObserver(function() {
              record.dirty = true;
              wake();
            });
            record.observer.observe(element);
          } catch (_error) {
            record.observer = null;
          }
        }
        wake();
      }
      record.users++;
      let released = false;
      return function() {
        if (released) return;
        released = true;
        const active = rectRecords.get(element);
        if (active !== record || --record.users > 0) return;
        if (record.observer) record.observer.disconnect();
        rectRecords.delete(element);
      };
    }

    function rect(element) {
      const record = rectRecords.get(element);
      if (!record) return null;
      if (!record.ready) return null;
      return {
        left: record.left - (record.fixed ? 0 : scrollX),
        top: record.top - (record.fixed ? 0 : scrollY),
        width: record.width,
        height: record.height,
        right: record.left - (record.fixed ? 0 : scrollX) + record.width,
        bottom: record.top - (record.fixed ? 0 : scrollY) + record.height,
      };
    }

    function readRects() {
      scrollX = motionNumber(window.scrollX, 0);
      scrollY = motionNumber(window.scrollY, 0);
      for (const record of rectRecords.values()) {
        if (record.dirty || !record.ready) measure(record);
      }
    }

    function runCallbacks(set, args) {
      const list = Array.from(set);
      for (const callback of list) {
        try { callback.apply(null, args); } catch (error) {
          if (window.console && typeof window.console.error === "function") window.console.error("[gosx] motion phase failed:", error);
        }
      }
    }

    function flushWrites() {
      let rounds = 0;
      while (writes.size && rounds++ < 8) {
        const pending = writes;
        writes = new Map();
        for (const callback of pending.values()) {
          try { callback(currentFrameMS); } catch (error) {
            if (window.console && typeof window.console.error === "function") window.console.error("[gosx] motion write failed:", error);
          }
        }
      }
    }

    function runFrame(timestamp) {
      frameHandle = null;
      const startMS = clockNow();
      currentFrameMS = motionNumber(timestamp, startMS);
      deltaSeconds = lastFrameMS == null ? 0 : motionClamp((currentFrameMS - lastFrameMS) / 1000, 0, 0.25);
      lastFrameMS = currentFrameMS;
      inFrame = true;
      const callbacks = frameCallbacks;
      frameCallbacks = new Map();
      activeFrameCallbacks = callbacks;

      let phaseStart = clockNow();
      for (const [id, callback] of Array.from(callbacks)) {
        if (!callbacks.delete(id)) continue;
        try { callback(currentFrameMS); } catch (error) {
          if (window.console && typeof window.console.error === "function") window.console.error("[gosx] scheduled frame failed:", error);
        }
      }
      activeFrameCallbacks = null;
      metrics.maxReadMS = Math.max(metrics.maxReadMS, clockNow() - phaseStart);

      phaseStart = clockNow();
      readRects();
      runCallbacks(phases.read, [currentFrameMS, deltaSeconds]);
      metrics.maxReadMS = Math.max(metrics.maxReadMS, clockNow() - phaseStart);

      phaseStart = clockNow();
      runCallbacks(phases.evaluate, [currentFrameMS, deltaSeconds]);
      for (const callback of Array.from(continuous)) {
        try {
          if (callback(currentFrameMS, deltaSeconds) === false) continuous.delete(callback);
        } catch (error) {
          continuous.delete(callback);
          if (window.console && typeof window.console.error === "function") window.console.error("[gosx] motion animation stopped:", error);
        }
      }
      metrics.maxEvaluateMS = Math.max(metrics.maxEvaluateMS, clockNow() - phaseStart);

      phaseStart = clockNow();
      flushWrites();
      runCallbacks(phases.write, [currentFrameMS, deltaSeconds]);
      metrics.maxWriteMS = Math.max(metrics.maxWriteMS, clockNow() - phaseStart);

      phaseStart = clockNow();
      for (const record of Array.from(sceneRecords)) {
        if (record.disposed || (!record.active && !record.dirty)) continue;
        record.dirty = false;
        try { record.callback(currentFrameMS, deltaSeconds); } catch (error) {
          record.active = false;
          if (window.console && typeof window.console.error === "function") window.console.error("[gosx] scene frame failed:", error);
        }
      }
      metrics.maxRenderMS = Math.max(metrics.maxRenderMS, clockNow() - phaseStart);
      inFrame = false;
      metrics.frames++;
      metrics.lastFrameMS = Math.max(0, clockNow() - startMS);
      metrics.maxFrameMS = Math.max(metrics.maxFrameMS, metrics.lastFrameMS);
      if (hasWork()) wake();
      else lastFrameMS = null;
    }

    function requestWork() { wake(); }
    function snapshotMetrics() { return Object.assign({}, metrics); }

    function handleScroll(event) {
      if (rectRecords.size === 0) return;
      const target = event && event.target;
      const scrolling = document.scrollingElement || document.documentElement || document.body;
      for (const record of rectRecords.values()) {
        const nestedScroll = target && target !== window && target !== document && target !== scrolling && target !== document.documentElement && target !== document.body;
        if ((nestedScroll && target.contains && target.contains(record.element)) || (!nestedScroll && (record.fixed || record.sticky))) record.dirty = true;
      }
      requestWork();
    }

    function invalidateRects() {
      for (const record of rectRecords.values()) record.dirty = true;
    }

    if (typeof window.addEventListener === "function") {
      window.addEventListener("scroll", handleScroll, { passive: true });
      window.addEventListener("resize", function() { invalidateRects(); requestWork(); }, { passive: true });
      if (window.visualViewport && typeof window.visualViewport.addEventListener === "function") {
        window.visualViewport.addEventListener("scroll", requestWork, { passive: true });
        window.visualViewport.addEventListener("resize", function() { invalidateRects(); requestWork(); }, { passive: true });
      }
    }
    if (typeof document !== "undefined" && typeof document.addEventListener === "function") {
      document.addEventListener("scroll", handleScroll, { passive: true, capture: true });
    }
    return {
      request: request,
      cancel: cancel,
      on: on,
      addContinuous: addContinuous,
      queueWrite: queueWrite,
      registerScene: registerScene,
      observeRect: observeRect,
      rect: rect,
      invalidateRects: invalidateRects,
      wake: wake,
      now: function() { return inFrame ? currentFrameMS : clockNow(); },
      delta: function() { return deltaSeconds; },
      isFrame: function() { return inFrame; },
      metrics: snapshotMetrics,
    };
  }

  function registerNamedValue(name, value) {
    const key = String(name || "").trim();
    if (!key) return;
    namedValues.set(key, value);
    const listeners = namedListeners.get(key);
    if (listeners) for (const callback of Array.from(listeners)) callback(value.get());
  }

  function createSignal(initial, name) {
    let current = motionClone(initial);
    const listeners = new Set();
    const signal = {
      get: function() { return motionClone(current); },
      set: function(value) {
        const next = motionClone(value);
        if (motionSame(current, next)) return false;
        current = next;
        for (const callback of Array.from(listeners)) callback(motionClone(current));
        if (name) {
          const named = namedListeners.get(name);
          if (named) for (const callback of Array.from(named)) callback(motionClone(current));
          const runtimeAPI = window.__gosx_runtime_api;
          if (runtimeAPI && typeof runtimeAPI.setSharedSignalValue === "function") {
            runtimeAPI.setSharedSignalValue("motion." + name, motionClone(current));
          }
        }
        return true;
      },
      jump: function(value) { signal.set(value); },
      subscribe: function(callback, options) {
        if (typeof callback !== "function") return function() {};
        listeners.add(callback);
        if (!options || options.immediate !== false) callback(motionClone(current));
        return function() { listeners.delete(callback); };
      },
    };
    if (name) registerNamedValue(name, signal);
    return signal;
  }

  function motionEase(t, raw) {
    const ease = raw || {};
    const kind = motionNumber(ease.Kind != null ? ease.Kind : ease.kind, 0);
    const args = Array.isArray(ease.Args) ? ease.Args : (Array.isArray(ease.args) ? ease.args : []);
    const x = motionClamp(t, 0, 1);
    if (x <= 0 || x >= 1) return x;
    if (kind === 1) return Math.pow(x, args.length ? args[0] : 2);
    if (kind === 2) return 1 - Math.pow(1 - x, args.length ? args[0] : 2);
    if (kind === 3) {
      const p = args.length ? args[0] : 2;
      return x < 0.5 ? 0.5 * Math.pow(2 * x, p) : 1 - 0.5 * Math.pow(2 - 2 * x, p);
    }
    if (kind === 4) {
      const x1 = args.length >= 4 ? args[0] : 0;
      const y1 = args.length >= 4 ? args[1] : 0;
      const x2 = args.length >= 4 ? args[2] : 0;
      const y2 = args.length >= 4 ? args[3] : 0;
      let u = x;
      for (let i = 0; i < 8; i++) {
        const error = motionBezier(u, x1, x2) - x;
        const derivative = motionBezierDerivative(u, x1, x2);
        if (Math.abs(derivative) < 1e-10) break;
        u = motionClamp(u - error / derivative, 0, 1);
        if (Math.abs(error) < 1e-7) break;
      }
      if (Math.abs(motionBezier(u, x1, x2) - x) > 1e-5) {
        let lo = 0, hi = 1;
        for (let i = 0; i < 30; i++) {
          const mid = (lo + hi) * 0.5;
          if (motionBezier(mid, x1, x2) < x) lo = mid; else hi = mid;
          if (hi - lo < 1e-7) break;
        }
        u = (lo + hi) * 0.5;
      }
      return motionBezier(u, y1, y2);
    }
    if (kind === 5) {
      const steps = args.length && args[0] >= 1 ? args[0] : 4;
      return Math.floor(x * steps) / steps;
    }
    if (kind >= 6 && kind <= 8) {
      const s = args.length ? args[0] : 1.70158;
      if (kind === 6) return (s + 1) * x * x * x - s * x * x;
      if (kind === 7) {
        const q = x - 1;
        return 1 + (s + 1) * q * q * q + s * q * q;
      }
      const s2 = s * 1.525;
      const c3 = s2 + 1;
      if (x < 0.5) {
        const q = 2 * x;
        return 0.5 * (c3 * q * q * q - s2 * q * q);
      }
      const q = 2 * x - 2;
      return 0.5 * (c3 * q * q * q + s2 * q * q) + 1;
    }
    return x;
  }

  function motionBezier(u, c1, c2) {
    const v = 1 - u;
    return 3 * v * v * u * c1 + 3 * v * u * u * c2 + u * u * u;
  }

  function motionBezierDerivative(u, x1, x2) {
    return 3 * x1 * (1 - u) * (1 - 3 * u) + 3 * x2 * u * (2 - 3 * u) + 3 * u * u;
  }

  const reducedQuery = typeof window.matchMedia === "function"
    ? window.matchMedia("(prefers-reduced-motion: reduce)")
    : null;
  let reducedMotion = Boolean(reducedQuery && reducedQuery.matches);
  const reducedListeners = new Set();
  if (reducedQuery && typeof reducedQuery.addEventListener === "function") {
    reducedQuery.addEventListener("change", function(event) {
      reducedMotion = Boolean(event && event.matches);
      for (const callback of Array.from(reducedListeners)) callback(reducedMotion);
    });
  } else if (reducedQuery && typeof reducedQuery.addListener === "function") {
    reducedQuery.addListener(function(event) {
      reducedMotion = Boolean(event && event.matches);
      for (const callback of Array.from(reducedListeners)) callback(reducedMotion);
    });
  }

  function motionReducedPolicy(policy) {
    return policy === "fade" || policy === "static" ? policy : "skip";
  }

  function createSpringSignal(initial, options, name) {
    const opts = options || {};
    const value = createSignal(motionNumber(initial, 0), name);
    const mass = motionNumber(opts.mass, 1) > 0 ? motionNumber(opts.mass, 1) : 1;
    const stiffness = motionNumber(opts.stiffness, 100) > 0 ? motionNumber(opts.stiffness, 100) : 100;
    const damping = motionNumber(opts.damping, 10) > 0 ? motionNumber(opts.damping, 10) : 10;
    let current = motionNumber(initial, 0);
    let target = current;
    let velocity = motionNumber(opts.velocity, 0);
    let accumulator = 0;
    let stopContinuous = null;
    const policy = motionReducedPolicy(opts.reducedMotion);

    function tick(_now, delta) {
      accumulator += motionClamp(delta, 0, 0.25);
      const stepSeconds = 1 / 240;
      const steps = Math.floor((accumulator + 1e-12) / stepSeconds);
      accumulator = Math.max(0, accumulator - steps * stepSeconds);
      for (let i = 0; i < steps; i++) {
        const force = -stiffness * (current - target) - damping * velocity;
        velocity += (force / mass) * stepSeconds;
        current += velocity * stepSeconds;
      }
      if (Math.abs(current - target) < 0.001 && Math.abs(velocity) < 0.01) {
        current = target;
        velocity = 0;
        accumulator = 0;
        value.set(current);
        stopContinuous = null;
        return false;
      }
      value.set(current);
      return true;
    }

    function setTarget(next) {
      target = motionNumber(next, target);
      if (reducedMotion && policy === "skip") {
        current = target;
        velocity = 0;
        accumulator = 0;
        value.set(current);
        if (stopContinuous) { stopContinuous(); stopContinuous = null; }
        return;
      }
      if (reducedMotion && policy === "static") {
        current = target;
        velocity = 0;
        accumulator = 0;
        value.set(current);
        if (stopContinuous) { stopContinuous(); stopContinuous = null; }
        return;
      }
      if (Math.abs(current - target) < 0.001 && Math.abs(velocity) < 0.01) {
        current = target;
        velocity = 0;
        value.set(current);
        return;
      }
      if (!stopContinuous) stopContinuous = scheduler.addContinuous(tick);
    }

    value.setTarget = setTarget;
    value.velocity = function() { return velocity; };
    value.target = function() { return target; };
    const stopReduced = function(next) {
      if (next) {
        if (policy === "skip") {
          current = target;
          velocity = 0;
          accumulator = 0;
          value.set(current);
        }
        if (policy === "static") {
          current = target;
          velocity = 0;
          accumulator = 0;
          value.set(current);
        }
        if (policy === "skip" || policy === "static") {
          if (stopContinuous) stopContinuous();
          stopContinuous = null;
        }
      } else if (Math.abs(current - target) >= 0.001 || Math.abs(velocity) >= 0.01) {
        if (!stopContinuous) stopContinuous = scheduler.addContinuous(tick);
      }
    };
    reducedListeners.add(stopReduced);
    value.dispose = function() {
      if (stopContinuous) stopContinuous();
      stopContinuous = null;
      reducedListeners.delete(stopReduced);
    };
    value.setTarget(opts.to == null ? current : opts.to);
    return value;
  }

  function createTweenSignal(from, to, duration, ease, name, policy) {
    const value = createSignal(motionClone(from), name);
    let startValue = motionClone(from);
    let target = motionClone(to);
    let startMS = null;
    let length = Math.max(0, motionNumber(duration, 0)) * 1000;
    let stopContinuous = null;
    let easeSpec = ease || { kind: 0 };
    const reducedPolicy = motionReducedPolicy(policy);

    function tick(now) {
      if (startMS == null) startMS = now;
      const progress = length <= 0 ? 1 : motionClamp((now - startMS) / length, 0, 1);
      value.set(motionMix(startValue, target, motionEase(progress, easeSpec)));
      if (progress >= 1) {
        value.set(target);
        stopContinuous = null;
        return false;
      }
      return true;
    }

    function animateTo(next, options) {
      const config = options || {};
      startValue = value.get();
      target = motionClone(next);
      startMS = null;
      length = Math.max(0, motionNumber(config.duration, length / 1000)) * 1000;
      easeSpec = config.ease || easeSpec;
      if (motionSame(startValue, target)) {
        value.set(target);
        if (stopContinuous) stopContinuous();
        stopContinuous = null;
        return;
      }
      if (reducedMotion && reducedPolicy === "skip") {
        value.set(target);
        if (stopContinuous) stopContinuous();
        stopContinuous = null;
        return;
      }
      if (reducedMotion && reducedPolicy === "static") {
        startValue = motionClone(target);
        value.set(target);
        if (stopContinuous) stopContinuous();
        stopContinuous = null;
        return;
      }
      if (reducedMotion && reducedPolicy === "fade") length = Math.min(length || 100, 120);
      if (!stopContinuous) stopContinuous = scheduler.addContinuous(tick);
    }

    value.retarget = animateTo;
    value.target = function() { return motionClone(target); };
    const stopReduced = function(next) {
      if (next) {
        if (reducedPolicy === "skip") value.set(target);
        if (reducedPolicy === "static") {
          startValue = motionClone(target);
          value.set(target);
        }
        if (reducedPolicy === "fade") {
          startValue = value.get();
          length = Math.min(length || 120, 120);
          startMS = null;
          if (!stopContinuous) stopContinuous = scheduler.addContinuous(tick);
        } else {
          if (stopContinuous) stopContinuous();
          stopContinuous = null;
        }
      } else if (!motionSame(value.get(), target)) {
        startValue = value.get();
        startMS = null;
        if (!stopContinuous) stopContinuous = scheduler.addContinuous(tick);
      }
    };
    reducedListeners.add(stopReduced);
    value.dispose = function() {
      if (stopContinuous) stopContinuous();
      stopContinuous = null;
      reducedListeners.delete(stopReduced);
    };
    animateTo(to, { duration: length / 1000, ease: easeSpec });
    return value;
  }

  function createKeyframeSignal(frames, duration, name, policy) {
    const keys = Array.isArray(frames) ? frames.slice().sort(function(a,b) { return motionNumber(a.at,0)-motionNumber(b.at,0); }) : [];
    const first = keys.length ? keys[0].value : 0;
    const last = keys.length ? keys[keys.length - 1].value : first;
    const value = createSignal(first, name);
    let total = Math.max(0, motionNumber(duration, 0));
    let startMS = null;
    let stopContinuous = null;
    const reducedPolicy = motionReducedPolicy(policy);

    function sample(progress) {
      if (!keys.length) return first;
      const local = motionClamp(progress, 0, 1) * (total || 1);
      if (local <= motionNumber(keys[0].at, 0)) return motionClone(keys[0].value);
      for (let i = 0; i < keys.length - 1; i++) {
        const a = keys[i], b = keys[i+1];
        const ta = motionNumber(a.at,0), tb = motionNumber(b.at,1);
        if (local <= tb) {
          const alpha = tb === ta ? 1 : (local - ta) / (tb - ta);
          return motionMix(a.value, b.value, motionEase(alpha, a.ease));
        }
      }
      return motionClone(last);
    }

    function tick(now) {
      if (startMS == null) startMS = now;
      const p = total <= 0 ? 1 : motionClamp((now - startMS) / (total * 1000), 0, 1);
      value.set(sample(p));
      if (p >= 1) { value.set(last); stopContinuous = null; return false; }
      return true;
    }

    const stopReduced = function(next) {
      if (next) {
        if (reducedPolicy === "skip") value.set(last);
        if (reducedPolicy === "fade") {
          total = Math.min(total || 0.12, 0.12);
          startMS = null;
          if (!stopContinuous) stopContinuous = scheduler.addContinuous(tick);
        } else {
          if (stopContinuous) stopContinuous();
          stopContinuous = null;
        }
      } else if (!motionSame(value.get(), last)) {
        startMS = null;
        if (!stopContinuous) stopContinuous = scheduler.addContinuous(tick);
      }
    };
    reducedListeners.add(stopReduced);
    value.dispose = function() {
      if (stopContinuous) stopContinuous();
      stopContinuous = null;
      reducedListeners.delete(stopReduced);
    };
    if (reducedMotion && (reducedPolicy === "skip" || reducedPolicy === "static")) value.set(last);
    else {
      if (reducedMotion && reducedPolicy === "fade") total = Math.min(total || 0.12, 0.12);
      stopContinuous = scheduler.addContinuous(tick);
    }
    return value;
  }

  function motionResolveEase(ease) {
    if (typeof ease === "string") {
      const raw = ease.trim().toLowerCase();
      if (raw === "ease-in") return { kind: 1, args: [2] };
      if (raw === "ease-out") return { kind: 2, args: [2] };
      if (raw === "ease-in-out") return { kind: 3, args: [2] };
      const match = raw.match(/^cubic-bezier\(\s*([-+\d.]+)\s*,\s*([-+\d.]+)\s*,\s*([-+\d.]+)\s*,\s*([-+\d.]+)\s*\)$/);
      if (match) return { kind: 4, args: match.slice(1).map(Number) };
      return { kind: 0 };
    }
    return ease || { kind: 0 };
  }

  function motionTransformComponents(value) {
    const text = String(value || "none");
    const translate = text.match(/translate3d\(\s*([-+\d.]+)(px)?\s*,\s*([-+\d.]+)(px)?\s*,\s*([-+\d.]+)(px)?\s*\)/i);
    const scale = text.match(/scale\(\s*([-+\d.]+)(?:\s*,\s*[-+\d.]+)?\s*\)/i);
    const rotate = text.match(/rotate\(\s*([-+\d.]+)(deg|rad)\s*\)/i);
    let base = text.replace(/translate3d\([^)]*\)/ig, "").replace(/scale\([^)]*\)/ig, "").replace(/rotate\([^)]*\)/ig, "").trim();
    if (text === "none") base = "";
    const validTranslate = Boolean(translate && (translate[2] || Number(translate[1]) === 0) && (translate[4] || Number(translate[3]) === 0) && (translate[6] || Number(translate[5]) === 0));
    return {
      x: validTranslate ? Number(translate[1]) : 0,
      y: validTranslate ? Number(translate[3]) : 0,
      z: validTranslate ? Number(translate[5]) : 0,
      scale: scale ? Number(scale[1]) : 1,
      rotate: rotate ? Number(rotate[1]) * (rotate[2].toLowerCase() === "deg" ? Math.PI / 180 : 1) : 0,
      base: base,
      known: text === "none" || Boolean(validTranslate || scale || rotate),
    };
  }

  function motionFormatTransform(value) {
    if (!value.known) return value.base || "none";
    const parts = ["translate3d(" + value.x + "px, " + value.y + "px, " + value.z + "px)"];
    if (value.rotate) parts.push("rotate(" + value.rotate + "rad)");
    if (value.scale !== 1) parts.push("scale(" + value.scale + ")");
    if (value.base) parts.push(value.base);
    return parts.join(" ");
  }

  function motionSetStyle(element, property, value, unit) {
    if (!element || !element.style) return;
    const next = typeof value === "number" ? String(value) + (unit || "") : String(value == null ? "" : value);
    if (property.indexOf("--") === 0 && typeof element.style.setProperty === "function") {
      element.style.setProperty(property, next);
      return;
    }
    const camel = property.replace(/-([a-z])/g, function(_m, c) { return c.toUpperCase(); });
    if (element.style[camel] !== next) element.style[camel] = next;
  }

  function animateKeyframes(element, frames, options) {
    if (!element || !element.style || !Array.isArray(frames) || frames.length < 2) return null;
    const config = options || {};
    const first = frames[0] || {}, last = frames[frames.length - 1] || {};
    const duration = Math.max(0, motionNumber(config.duration, 0));
    const delay = Math.max(0, motionNumber(config.delay, 0));
    const ease = motionResolveEase(config.easing);
    const policy = motionReducedPolicy(config.reducedMotion || "skip");
    const reduce = Boolean(config.respectReducedMotion !== false && reducedMotion);
    let active = true;
    let startMS = motionNow() + (reduce ? 0 : delay);
    let stopContinuous = null;
    const id = ++nextAnimationID;
    let animateOpacityOnly = reduce && policy === "fade";
    let runtimeDuration = animateOpacityOnly ? Math.min(duration || 120, 120) : duration;
    let stopReduced = null;
    let resolveFinished;
    const finished = new Promise(function(resolve) { resolveFinished = resolve; });
    const properties = Array.from(new Set(frames.flatMap(function(frame) {
      return Object.keys(frame || {}).filter(function(property) { return property !== "offset" && property !== "easing"; });
    })));
    const offsets = frames.map(function(frame, index) {
      const raw = motionNumber(frame && frame.offset, NaN);
      return Number.isFinite(raw) ? motionClamp(raw, 0, 1) : index / (frames.length - 1);
    });
    for (let i = 1; i < offsets.length; i++) offsets[i] = Math.max(offsets[i], offsets[i - 1]);

    const originalStyles = new Map();
    const styleName = function(property) {
      return property.indexOf("--") === 0 ? property : property.replace(/[A-Z]/g, function(letter) { return "-" + letter.toLowerCase(); });
    };
    const styleField = function(property) {
      return property.indexOf("--") === 0 ? property : property.replace(/-([a-z])/g, function(_match, letter) { return letter.toUpperCase(); });
    };
    const readInline = function(property) {
      const cssName = styleName(property);
      if (typeof element.style.getPropertyValue === "function") {
        return { css: true, value: element.style.getPropertyValue(cssName), priority: typeof element.style.getPropertyPriority === "function" ? element.style.getPropertyPriority(cssName) : "" };
      }
      return { css: false, value: element.style[styleField(property)] || "", priority: "" };
    };
    const restoreInline = function(property, original) {
      const cssName = styleName(property);
      if (original.css && typeof element.style.setProperty === "function") {
        if (original.value || original.priority) element.style.setProperty(cssName, original.value, original.priority);
        else if (typeof element.style.removeProperty === "function") element.style.removeProperty(cssName);
        else element.style.setProperty(cssName, "");
      } else {
        element.style[styleField(property)] = original.value;
      }
    };
    for (const property of properties) originalStyles.set(property, readInline(property));
    const originalTransition = readInline("transition");
    let transitionSuppressed = false;

    function safeFinalStyles() {
      const styles = Object.assign({}, last);
      if (typeof last.opacity === "number" || (typeof last.opacity === "string" && last.opacity.trim() !== "")) {
        let naturalOpacity = NaN;
        try {
          const computed = window.getComputedStyle && window.getComputedStyle(element);
          naturalOpacity = motionNumber(computed && computed.opacity, NaN);
        } catch (_error) {}
        if (!Number.isFinite(naturalOpacity)) naturalOpacity = motionNumber(originalStyles.get("opacity") && originalStyles.get("opacity").value, 1);
        styles.opacity = Math.max(naturalOpacity, motionNumber(last.opacity, naturalOpacity));
      }
      return styles;
    }

    function applyStyles(styles, restoreTransitionAfter) {
      scheduler.queueWrite("motion-animation:" + id, function() {
        for (const property of Object.keys(styles)) motionSetStyle(element, property, styles[property]);
        if (restoreTransitionAfter && transitionSuppressed) {
          restoreInline("transition", originalTransition);
          transitionSuppressed = false;
        }
      });
    }

    function setTransitionSuppressed() {
      if (typeof element.style.setProperty === "function") element.style.setProperty("transition", "none", "important");
      else element.style.transition = "none";
      transitionSuppressed = true;
    }

    function applyStaticStyles() {
      const styles = safeFinalStyles();
      setTransitionSuppressed();
      for (const property of Object.keys(styles)) motionSetStyle(element, property, styles[property]);
      restoreInline("transition", originalTransition);
      transitionSuppressed = false;
    }

    function sample(progress) {
      let index = 0;
      while (index < frames.length - 2 && progress > offsets[index + 1]) index++;
      const a = frames[index] || first;
      const b = frames[index + 1] || last;
      const start = offsets[index] || 0;
      const end = offsets[index + 1] == null ? 1 : offsets[index + 1];
      const local = end <= start ? 1 : motionClamp((progress - start) / (end - start), 0, 1);
      const segmentEase = motionResolveEase(a.easing || config.easing || ease);
      const eased = motionEase(local, segmentEase);
      const styles = {};
      for (const property of properties) {
        const from = a[property];
        const to = b[property];
        if (animateOpacityOnly && property !== "opacity") {
          styles[property] = last[property];
        } else if (property === "opacity" && typeof from === "number" && typeof to === "number") {
          styles.opacity = from + (to - from) * eased;
        } else if (property === "transform" && !animateOpacityOnly) {
          const fromTransform = motionTransformComponents(from);
          const toTransform = motionTransformComponents(to);
          if (fromTransform.known && toTransform.known) {
            styles.transform = motionFormatTransform({
              x: fromTransform.x + (toTransform.x - fromTransform.x) * eased,
              y: fromTransform.y + (toTransform.y - fromTransform.y) * eased,
              z: fromTransform.z + (toTransform.z - fromTransform.z) * eased,
              rotate: fromTransform.rotate + (toTransform.rotate - fromTransform.rotate) * eased,
              scale: fromTransform.scale + (toTransform.scale - fromTransform.scale) * eased,
              base: toTransform.base || fromTransform.base,
              known: true,
            });
          } else styles[property] = local >= 1 ? to : from;
        } else {
          styles[property] = local >= 1 ? to : from;
        }
      }
      return styles;
    }

    function finish(styles) {
      applyStyles(styles, true);
      active = false;
      if (stopContinuous) stopContinuous();
      stopContinuous = null;
      if (stopReduced) reducedListeners.delete(stopReduced);
      resolveFinished({ finished: true });
    }

    if (reduce && policy === "skip") {
      finish(last);
      return { finished: finished, cancel: function() { active = false; } };
    }
    if (reduce && policy === "static") {
      applyStaticStyles();
      active = false;
      resolveFinished({ finished: true });
      return { finished: finished, cancel: function() { active = false; } };
    }
    setTransitionSuppressed();
    for (const property of Object.keys(sample(0))) motionSetStyle(element, property, sample(0)[property]);
    function tick(now) {
      if (!active) return false;
      const elapsed = now - startMS;
      if (elapsed < 0) return true;
      const progress = runtimeDuration <= 0 ? 1 : motionClamp(elapsed / runtimeDuration, 0, 1);
      applyStyles(progress >= 1 ? last : sample(progress), progress >= 1);
      if (progress >= 1) {
        active = false;
        stopContinuous = null;
        if (stopReduced) reducedListeners.delete(stopReduced);
        resolveFinished({ finished: true });
        return false;
      }
      return true;
    }

    stopReduced = function(next) {
      if (!active || config.respectReducedMotion === false || !next) return;
      if (policy === "skip") {
        finish(last);
      } else if (policy === "static") {
        applyStyles(safeFinalStyles(), true);
        active = false;
        if (stopContinuous) stopContinuous();
        stopContinuous = null;
        reducedListeners.delete(stopReduced);
        resolveFinished({ finished: true });
      } else {
        animateOpacityOnly = true;
        runtimeDuration = Math.min(runtimeDuration || 120, 120);
        startMS = motionNow();
      }
    };
    reducedListeners.add(stopReduced);
    stopContinuous = scheduler.addContinuous(tick);
    return {
      finished: finished,
      cancel: function() {
        if (!active) return;
        active = false;
        if (stopContinuous) stopContinuous();
        stopContinuous = null;
        if (stopReduced) reducedListeners.delete(stopReduced);
        scheduler.queueWrite("motion-animation:" + id, function() {
          for (const [property, original] of originalStyles) restoreInline(property, original);
          if (transitionSuppressed) {
            restoreInline("transition", originalTransition);
            transitionSuppressed = false;
          }
        });
        resolveFinished({ finished: false });
      },
    };
  }

  function motionQuery(root, selector) {
    const out = [];
    if (!root || !selector) return out;
    try {
      if (root.matches && root.matches(selector)) out.push(root);
      if (root.querySelectorAll) out.push.apply(out, Array.from(root.querySelectorAll(selector)));
    } catch (_error) {
      return [];
    }
    return out;
  }

  function setBoundDOMValue(binding, element, value, transformState) {
    if (!motionBindingIsValid(binding)) return;
    const property = String(binding.property || "");
    if (binding.target === "cssVar") {
      motionSetStyle(element, property, value, binding.unit || "");
      return;
    }
    if (property.indexOf("transform.") === 0) {
      const key = property.slice("transform.".length).toLowerCase();
      if (key === "x" || key === "y" || key === "z") transformState[key] = motionNumber(value, transformState[key]);
      else if (key === "scale") transformState.scale = motionNumber(value, transformState.scale);
      else if (key === "rotation" || key === "rotate") transformState.rotate = motionNumber(value, transformState.rotate);
      transformState.known = true;
      motionSetStyle(element, "transform", motionFormatTransform(transformState));
      return;
    }
    motionSetStyle(element, property, value, binding.unit || "");
  }

  function createProgramValue(spec, name, getValue) {
    const kind = spec.kind || spec.Kind;
    if (kind === "time") {
      const value = createSignal(0, name);
      const start = motionNow();
      value.dispose = scheduler.addContinuous(function(now) { value.set(Math.max(0, (now - start) / 1000)); return true; });
      return value;
    }
    if (kind === "visibility" || kind === "scroll" || kind === "pointer" || kind === "hover") return createSignal(0, name);
    if (kind === "spring") {
      const value = createSpringSignal(spec.from, {
        to: spec.input ? spec.from : spec.to,
        mass: spec.mass, stiffness: spec.stiffness, damping: spec.damping,
        velocity: spec.velocity, reducedMotion: spec.reducedMotion,
      }, name);
      return value;
    }
    if (kind === "tween") {
      return createTweenSignal(spec.from, spec.to, spec.duration, spec.ease, name, spec.reducedMotion);
    }
    if (kind === "keyframes") {
      const frames = (spec.frames || []).map(function(frame) { return { at: frame.at, value: frame.value, ease: frame.ease }; });
      return createKeyframeSignal(frames, spec.duration, name, spec.reducedMotion);
    }
    if (kind === "map" || kind === "clamp" || kind === "velocity") {
      const input = getValue(spec.input);
      const value = createSignal(0, name);
      let previous = motionNumber(input.get(), 0), previousTime = scheduler.now();
      const compute = function(next) {
        const number = motionNumber(next, 0);
        if (kind === "map") {
          const span = motionNumber(spec.to, 1) - motionNumber(spec.from, 0);
          const t = span === 0 ? 0 : (number - motionNumber(spec.from, 0)) / span;
          value.set(motionNumber(spec.min, 0) + (motionNumber(spec.max, 1) - motionNumber(spec.min, 0)) * t);
        } else if (kind === "clamp") value.set(motionClamp(number, motionNumber(spec.min, 0), motionNumber(spec.max, 1)));
        else {
          const now = scheduler.now(), dt = Math.max(1e-6, (now - previousTime) / 1000);
          value.set((number - previous) / dt);
          previous = number; previousTime = now;
        }
      };
      input.subscribe(compute);
      return value;
    }
    if (kind === "curve") {
      const input = getValue(spec.input);
      const stops = motionCurveStops(spec.frames);
      const smooth = spec.smooth === true;
      const value = createSignal(stops.length ? stops[0].value : 0, name);
      input.subscribe(function(next) { value.set(motionCurveValue(stops, smooth, motionNumber(next, 0))); });
      return value;
    }
    if (kind === "mix") {
      const a = getValue(spec.a), b = getValue(spec.b), weight = getValue(spec.weight);
      const value = createSignal(motionMix(a.get(), b.get(), motionClamp(motionNumber(weight.get(), 0), 0, 1)), name);
      const update = function() { value.set(motionMix(a.get(), b.get(), motionClamp(motionNumber(weight.get(), 0), 0, 1))); };
      a.subscribe(update, { immediate: false }); b.subscribe(update, { immediate: false }); weight.subscribe(update, { immediate: false });
      return value;
    }
    return createSignal(0, name);
  }

  function motionScrollTimelinesSupported() {
    try {
      return typeof CSS !== "undefined" && typeof CSS.supports === "function" && CSS.supports("animation-timeline: scroll()");
    } catch (_error) {
      return false;
    }
  }

  // The server compiles fixed page-scroll bindings to CSS and lists them in
  // program.cssCompiled. When the browser supports scroll timelines the CSS
  // owns them, so drop those bindings and any signal only they used. Otherwise
  // the program runs whole, which is the fallback.
  function motionWithoutCSSCompiled(program) {
    const listed = Array.isArray(program.cssCompiled) ? program.cssCompiled : [];
    if (!listed.length || !Array.isArray(program.bindings) || !motionScrollTimelinesSupported()) return program;
    const skip = new Set(listed.filter(function(index) { return Number.isInteger(index); }));
    const bindings = program.bindings.filter(function(_binding, index) { return !skip.has(index); });
    const specs = new Map();
    for (const spec of program.signals) if (spec && spec.id) specs.set(String(spec.id), spec);
    const needed = new Set();
    const visit = function(id) {
      const key = String(id || "");
      if (!key || needed.has(key)) return;
      needed.add(key);
      const spec = specs.get(key);
      if (spec) for (const ref of [spec.input, spec.a, spec.b, spec.weight]) visit(ref);
    };
    for (const binding of bindings) visit(binding && binding.signal);
    const signals = program.signals.filter(function(spec) { return spec && needed.has(String(spec.id)); });
    return Object.assign({}, program, { signals: signals, bindings: bindings });
  }

  function createProgram(root, raw) {
    const parsed = raw && typeof raw === "object" ? raw : {};
    const program = motionNumber(parsed.version, 0) === 1 && Array.isArray(parsed.signals) ? motionWithoutCSSCompiled(parsed) : parsed;
    if (motionNumber(program.version, 0) !== 1 || !Array.isArray(program.signals)) return null;
    const programID = String(program.id || "motion");
    const record = { root: root, id: programID, signals: new Map(), specs: new Map(), bindings: [], pins: [], stops: [], values: [], adapters: new Set(), disposed: false };
    for (const spec of program.signals) if (spec && spec.id) record.specs.set(String(spec.id), spec);
    const creating = new Set();
    function getValue(id) {
      const key = String(id || "");
      if (record.signals.has(key)) return record.signals.get(key);
      const spec = record.specs.get(key);
      if (!spec || creating.has(key)) return createSignal(0);
      creating.add(key);
      const value = createProgramValue(spec, programID + "." + key, getValue);
      record.signals.set(key, value);
      record.values.push(value);
      creating.delete(key);
      return value;
    }
    for (const spec of program.signals) if (spec && spec.id) getValue(spec.id);

    for (const spec of program.signals) {
      if (!spec || !spec.source) continue;
      const value = record.signals.get(String(spec.id));
      const source = spec.source;
      const type = source.kind || spec.kind;
      const elements = source.selector ? motionQuery(root, source.selector) : [];
      const element = elements[0] || null;
      if (type === "time") continue;
      if (type === "visibility") {
        if (element && typeof IntersectionObserver === "function") {
          const observer = new IntersectionObserver(function(entries) {
            for (const entry of entries || []) if (entry && entry.target === element) value.set(entry.isIntersecting ? motionNumber(entry.intersectionRatio, 0) : 0);
          }, { threshold: [0, 0.01, 0.25, 0.5, 0.75, 1] });
          observer.observe(element);
          record.stops.push(function() { observer.disconnect(); });
        }
      } else if (type === "hover") {
        if (element && typeof element.addEventListener === "function") {
          const on = function() { value.set(1); }, off = function() { value.set(0); };
          element.addEventListener("pointerenter", on, { passive: true });
          element.addEventListener("pointerleave", off, { passive: true });
          element.addEventListener("focusin", on);
          element.addEventListener("focusout", off);
          record.stops.push(function() {
            element.removeEventListener("pointerenter", on); element.removeEventListener("pointerleave", off);
            element.removeEventListener("focusin", on); element.removeEventListener("focusout", off);
          });
        }
      } else if (type === "pointer") {
        if (element && typeof element.addEventListener === "function") {
          record.stops.push(scheduler.observeRect(element));
          let x = 0, y = 0;
          const onMove = function(event) { x = motionNumber(event.clientX, 0); y = motionNumber(event.clientY, 0); scheduler.wake(); };
          const onLeave = function() { x = 0; y = 0; value.set(0); };
          element.addEventListener("pointermove", onMove, { passive: true });
          element.addEventListener("pointerleave", onLeave, { passive: true });
          const stop = scheduler.on("read", function() {
            const rect = scheduler.rect(element);
            if (!rect) return;
            const axis = source.axis || "y";
            const result = axis === "x" ? (x - rect.left) / Math.max(1, rect.width) : (y - rect.top) / Math.max(1, rect.height);
            value.set(motionClamp(result, 0, 1));
          });
          record.stops.push(stop, function() { element.removeEventListener("pointermove", onMove); element.removeEventListener("pointerleave", onLeave); });
        }
      } else if (type === "scroll") {
        const target = element;
        const onScroll = function() { if (target && typeof scheduler.invalidateRects === "function") scheduler.invalidateRects(); scheduler.wake(); };
        if (target && typeof target.addEventListener === "function") target.addEventListener("scroll", onScroll, { passive: true });
        else if (typeof window.addEventListener === "function") window.addEventListener("scroll", onScroll, { passive: true });
        const stop = scheduler.on("read", function() {
          const axis = source.axis || "y";
          let progress = 0;
          if (target) {
            const position = axis === "x" ? target.scrollLeft : target.scrollTop;
            const range = axis === "x" ? target.scrollWidth - target.clientWidth : target.scrollHeight - target.clientHeight;
            progress = range > 0 ? position / range : 0;
          } else {
            const scrolling = document.scrollingElement || document.documentElement || document.body;
            const position = axis === "x" ? motionNumber(window.scrollX, 0) : motionNumber(window.scrollY, 0);
            const range = scrolling ? (axis === "x" ? scrolling.scrollWidth - window.innerWidth : scrolling.scrollHeight - window.innerHeight) : 0;
            progress = range > 0 ? position / range : 0;
          }
          value.set(motionClamp(progress, 0, 1));
        });
        record.stops.push(stop, function() {
          if (target) target.removeEventListener("scroll", onScroll); else window.removeEventListener("scroll", onScroll);
        });
      }
    }

    for (const spec of program.signals) {
      if (!spec || spec.kind !== "spring" || !spec.input) continue;
      const spring = record.signals.get(String(spec.id));
      const input = record.signals.get(String(spec.input));
      if (spring && input && typeof spring.setTarget === "function") {
        record.stops.push(input.subscribe(function(next) { spring.setTarget(next); }));
        spring.setTarget(input.get());
      }
    }

    for (const binding of program.bindings || []) {
      if (!motionBindingIsValid(binding) || !binding.signal) continue;
      const value = record.signals.get(String(binding.signal));
      if (!value) continue;
      const target = binding.target || "style";
      if (target === "style" || target === "cssVar") {
        const elements = motionQuery(root, binding.selector);
        for (const element of elements) {
          const transformState = motionTransformComponents(element.style && element.style.transform);
          const apply = function(next) {
            scheduler.queueWrite(element, function() { setBoundDOMValue(binding, element, next, transformState); });
          };
          record.stops.push(value.subscribe(apply));
        }
      } else {
        const sceneElement = motionQuery(root, binding.selector)[0] || null;
        const attachment = sceneElement && sceneAdapters.get(sceneElement);
        const item = { binding: binding, signal: value, scene: sceneElement, adapter: attachment && attachment.adapter || null, latest: value.get() };
        if (item.adapter) record.adapters.add(item.adapter);
        const apply = function(next) {
          item.latest = motionClone(next);
          scheduler.queueWrite(item, function() {
            if (item.adapter && !record.disposed) {
              item.adapter.write(binding, motionClone(item.latest), record);
              if (typeof item.adapter.invalidate === "function") item.adapter.invalidate("motion-binding");
            }
          });
        };
        record.stops.push(value.subscribe(apply));
        record.bindings.push(item);
      }
    }

    for (const pin of program.pins || []) {
      const scene = motionQuery(root, pin.scene)[0] || null;
      const element = motionQuery(root, pin.element)[0] || null;
      if (scene && element) {
        record.stops.push(scheduler.observeRect(scene));
        record.stops.push(scheduler.observeRect(element));
        const attachment = scene && sceneAdapters.get(scene);
        record.pins.push({ spec: pin, scene: scene, element: element, adapter: attachment && attachment.adapter || null });
        if (attachment && attachment.adapter) record.adapters.add(attachment.adapter);
      }
    }
    if (record.pins.length) {
      record.stops.push(scheduler.on("write", function() {
        for (const pin of record.pins) {
          if (!pin.adapter || record.disposed) continue;
          const elementRect = scheduler.rect(pin.element), sceneRect = scheduler.rect(pin.scene);
          if (!elementRect || !sceneRect) continue;
          if (pin.adapter.pin(pin.spec, elementRect, sceneRect, record) && typeof pin.adapter.invalidate === "function") pin.adapter.invalidate("pin-to");
        }
      }));
    }
    scheduler.wake();
    return record;
  }

  function disposeProgram(record) {
    if (!record || record.disposed) return;
    record.disposed = true;
    for (const stop of record.stops) if (typeof stop === "function") stop();
    for (const value of record.values) if (value && typeof value.dispose === "function") value.dispose();
    for (const adapter of record.adapters) {
      if (adapter && typeof adapter.disposeProgram === "function") {
        adapter.disposeProgram(record);
        if (typeof adapter.invalidate === "function") adapter.invalidate("motion-dispose");
      }
    }
    record.adapters.clear();
    for (const [name, value] of namedValues) if (name.indexOf(record.id + ".") === 0 && record.signals.has(name.slice(record.id.length + 1)) && record.signals.get(name.slice(record.id.length + 1)) === value) namedValues.delete(name);
    programRecords.delete(record.root);
  }

  function mountProgramElement(element) {
    if (!element || !element.hasAttribute || !element.hasAttribute("data-gosx-motion-program")) return null;
    const existing = programRecords.get(element);
    if (existing) return existing;
    let parsed;
    try { parsed = JSON.parse(element.getAttribute("data-gosx-motion-program") || ""); } catch (_error) { return null; }
    const record = createProgram(element, parsed);
    if (!record) return null;
    programRecords.set(element, record);
    return record;
  }

  function mountPrograms(root) {
    const target = root || document.body || document.documentElement;
    if (target && target.nodeType === 1 && target.hasAttribute && target.hasAttribute("data-gosx-motion-program")) mountProgramElement(target);
    for (const element of motionQuery(target, "[data-gosx-motion-program]")) mountProgramElement(element);
    installProgramObserver(document.body || document.documentElement);
  }

  function disposePrograms(root) {
    for (const record of Array.from(programRecords.values())) {
      if (!root || record.root === root || (root.contains && root.contains(record.root))) disposeProgram(record);
    }
  }

  function installProgramObserver(root) {
    if (programObserver || typeof MutationObserver !== "function" || !root || programRecords.size === 0) return;
    programObserver = new MutationObserver(function(changes) {
      for (const change of changes || []) {
        if (change.type === "attributes" && change.target && change.attributeName === "data-gosx-motion-program") {
          const old = programRecords.get(change.target);
          if (old) disposeProgram(old);
          mountProgramElement(change.target);
        }
        for (const node of Array.from(change.addedNodes || [])) if (node && node.nodeType === 1) mountPrograms(node);
        for (const node of Array.from(change.removedNodes || [])) if (node && node.nodeType === 1) disposePrograms(node);
      }
    });
    programObserver.observe(root, { subtree: true, childList: true, attributes: true, attributeFilter: ["data-gosx-motion-program"] });
  }

  function attachScene(mount, adapter) {
    if (!mount || !adapter || typeof adapter.write !== "function") return function() {};
    const attachment = { mount: mount, adapter: adapter };
    sceneAdapters.set(mount, attachment);
    for (const record of programRecords.values()) {
      for (const binding of record.bindings) if (binding.scene === mount) {
        binding.adapter = adapter;
        record.adapters.add(adapter);
        scheduler.queueWrite(binding, function() {
          if (!record.disposed) {
            adapter.write(binding.binding, motionClone(binding.latest), record);
            if (typeof adapter.invalidate === "function") adapter.invalidate("motion-binding");
          }
        });
      }
      for (const pin of record.pins) if (pin.scene === mount) {
        pin.adapter = adapter;
        record.adapters.add(adapter);
      }
    }
    if (typeof adapter.invalidate === "function") adapter.invalidate("motion-attach");
    return function() {
      if (sceneAdapters.get(mount) !== attachment) return;
      sceneAdapters.delete(mount);
      for (const record of programRecords.values()) {
        for (const binding of record.bindings) if (binding.scene === mount && binding.adapter === adapter) binding.adapter = null;
        for (const pin of record.pins) if (pin.scene === mount && pin.adapter === adapter) pin.adapter = null;
      }
    };
  }

  function createPublicTween(from, options, name) {
    const opts = options || {};
    const value = createTweenSignal(from, from, opts.duration, motionResolveEase(opts.ease || opts.easing), name, opts.reducedMotion);
    value.to = function(target, nextOptions) { value.retarget(target, nextOptions || opts); return value; };
    return value;
  }

  function createDerivedSignal(input, calculate, name) {
    const value = createSignal(calculate(input.get()), name);
    const stop = input.subscribe(function(next) { value.set(calculate(next)); }, { immediate: false });
    value.dispose = stop;
    return value;
  }

  function bindDOM(signal, element, property, unit) {
    if (!signal || !element) return function() {};
    const binding = { target: property.indexOf("--") === 0 ? "cssVar" : "style", property: property, unit: unit || "" };
    if (!motionBindingIsValid(binding)) return function() {};
    const transform = motionTransformComponents(element.style && element.style.transform);
    return signal.subscribe(function(value) {
      scheduler.queueWrite(element, function() { setBoundDOMValue(binding, element, value, transform); });
    });
  }

  motion.ease = motionEase;
  motion.scheduler = scheduler;
  motion.signal = function(initial, name) { return createSignal(initial, name ? String(name) : ""); };
  motion.spring = function(initial, options) { return createSpringSignal(initial, options || {}, options && options.name ? String(options.name) : ""); };
  motion.tween = function(from, options) { return createPublicTween(from, options || {}, options && options.name ? String(options.name) : ""); };
  motion.timeline = function(frames, options) {
    const opts = options || {};
    return createKeyframeSignal(frames, opts.duration, opts.name ? String(opts.name) : "", opts.reducedMotion);
  };
  motion.map = function(signal, inMin, inMax, outMin, outMax, name) {
    return createDerivedSignal(signal, function(value) {
      const span = inMax - inMin, t = span === 0 ? 0 : (motionNumber(value, 0) - inMin) / span;
      return outMin + (outMax - outMin) * t;
    }, name || "");
  };
  motion.clamp = function(signal, min, max, name) {
    return createDerivedSignal(signal, function(value) { return motionClamp(motionNumber(value, 0), min, max); }, name || "");
  };
  motion.mix = function(a, b, weight, name) {
    const value = createSignal(motionMix(a.get(), b.get(), motionClamp(motionNumber(weight.get(), 0), 0, 1)), name || "");
    const update = function() { value.set(motionMix(a.get(), b.get(), motionClamp(motionNumber(weight.get(), 0), 0, 1))); };
    const stopA = a.subscribe(update, { immediate: false }), stopB = b.subscribe(update, { immediate: false }), stopW = weight.subscribe(update, { immediate: false });
    value.dispose = function() { stopA(); stopB(); stopW(); };
    return value;
  };
  motion.velocity = function(signal, name) {
    const value = createSignal(0, name || "");
    let previous = motionNumber(signal.get(), 0), previousMS = scheduler.now();
    const stop = scheduler.on("evaluate", function(now) {
      const current = motionNumber(signal.get(), 0);
      const dt = Math.max(1e-6, (now - previousMS) / 1000);
      value.set(current === previous ? 0 : (current - previous) / dt);
      previous = current; previousMS = now;
    });
    value.dispose = stop;
    return value;
  };
  motion.bind = bindDOM;
  motion.animateKeyframes = animateKeyframes;
  motion.animate = function(element, keyframes, options) { return animateKeyframes(element, keyframes, options); };
  motion.get = function(name) {
    const value = namedValues.get(String(name || ""));
    return value && typeof value.get === "function" ? value.get() : undefined;
  };
  motion.subscribe = function(name, callback, options) {
    const key = String(name || "");
    let listeners = namedListeners.get(key);
    if (!listeners) { listeners = new Set(); namedListeners.set(key, listeners); }
    listeners.add(callback);
    const value = namedValues.get(key);
    if ((!options || options.immediate !== false) && value && callback) callback(value.get());
    return function() { listeners.delete(callback); if (!listeners.size) namedListeners.delete(key); };
  };
  motion.attachScene = attachScene;
  motion.mountPrograms = mountPrograms;
  motion.disposePrograms = disposePrograms;
  motion.reducedMotion = function() { return reducedMotion; };
  motion.onReducedMotion = function(callback) { reducedListeners.add(callback); return function() { reducedListeners.delete(callback); }; };
  motion.snapshot = function() {
    const out = {};
    for (const [name, value] of namedValues) out[name] = value.get();
    return out;
  };

  if (typeof document !== "undefined") {
    mountPrograms(document.body || document.documentElement);
    if (typeof document.addEventListener === "function") {
      document.addEventListener("gosx:navigate", function() { mountPrograms(document.body || document.documentElement); });
      document.addEventListener("gosx:region:after", function() { mountPrograms(document.body || document.documentElement); });
    }
  }
})();
