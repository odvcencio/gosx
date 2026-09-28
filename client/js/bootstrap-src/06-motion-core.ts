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

  function motionMix(a, b, t) {
    if (Array.isArray(a) && Array.isArray(b)) {
      const out = new Array(Math.min(a.length, b.length));
      for (let i = 0; i < out.length; i++) out[i] = a[i] + (b[i] - a[i]) * t;
      return out;
    }
    return motionNumber(a, 0) + (motionNumber(b, 0) - motionNumber(a, 0)) * t;
  }

  function createMotionScheduler() {
    const phases = { read: new Set(), evaluate: new Set(), write: new Set() };
    const rectRecords = new Map();
    let rectMutationObserver = null;
    const continuous = new Set();
    const sceneRecords = new Set();
    let frameCallbacks = new Map();
    let writes = new Map();
    let nextID = 0;
    let frameHandle = null;
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
      let position = "";
      try { position = window.getComputedStyle ? window.getComputedStyle(element).position : ""; } catch (_error) {}
      record.fixed = position === "fixed";
      record.sticky = position === "sticky" || position === "-webkit-sticky";
      record.left = motionNumber(rect.left, 0) + (record.fixed ? 0 : x);
      record.top = motionNumber(rect.top, 0) + (record.fixed ? 0 : y);
      record.width = Math.max(0, motionNumber(rect.width, rect.right - rect.left));
      record.height = Math.max(0, motionNumber(rect.height, rect.bottom - rect.top));
      record.ready = true;
      record.dirty = false;
    }

    function invalidateRects() {
      for (const record of rectRecords.values()) record.dirty = true;
    }

    function ensureRectMutationObserver() {
      if (rectMutationObserver || typeof MutationObserver !== "function" || typeof document === "undefined") return;
      const root = document.documentElement || document.body;
      if (!root) return;
      try {
        rectMutationObserver = new MutationObserver(function(changes) {
          if (!changes || !changes.length) return;
          invalidateRects();
          requestWork();
        });
        rectMutationObserver.observe(root, { subtree: true, childList: true, characterData: true, attributes: true, attributeFilter: ["class", "style", "hidden", "width", "height"] });
      } catch (_error) {
        rectMutationObserver = null;
      }
    }

    function releaseRectMutationObserver() {
      if (rectRecords.size !== 0 || !rectMutationObserver) return;
      rectMutationObserver.disconnect();
      rectMutationObserver = null;
    }

    function observeRect(element) {
      if (!element || typeof element !== "object") return function() {};
      let record = rectRecords.get(element);
      if (!record) {
        record = { element: element, left: 0, top: 0, width: 0, height: 0, ready: false, dirty: true, observer: null, users: 0 };
        rectRecords.set(element, record);
        ensureRectMutationObserver();
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
        releaseRectMutationObserver();
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

      let phaseStart = clockNow();
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
      for (const callback of callbacks.values()) {
        try { callback(currentFrameMS); } catch (error) {
          if (window.console && typeof window.console.error === "function") window.console.error("[gosx] scheduled frame failed:", error);
        }
      }
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
      const target = event && event.target;
      const scrolling = document.scrollingElement || document.documentElement || document.body;
      for (const record of rectRecords.values()) {
        if (record.fixed || record.sticky || (target && target !== window && target !== document && target !== scrolling && target !== document.documentElement && target !== document.body)) record.dirty = true;
      }
      requestWork();
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

  function motionSlerp(a, b, t) {
    let bx = b[0], by = b[1], bz = b[2], bw = b[3];
    let dot = a[0] * bx + a[1] * by + a[2] * bz + a[3] * bw;
    if (dot < 0) { bx = -bx; by = -by; bz = -bz; bw = -bw; dot = -dot; }
    if (dot > 0.9995) {
      const x = a[0] + t * (bx - a[0]);
      const y = a[1] + t * (by - a[1]);
      const z = a[2] + t * (bz - a[2]);
      const w = a[3] + t * (bw - a[3]);
      const inv = 1 / Math.sqrt(x*x + y*y + z*z + w*w || 1);
      return [x*inv, y*inv, z*inv, w*inv];
    }
    dot = motionClamp(dot, -1, 1);
    const theta = Math.acos(dot);
    const sinTheta = Math.sin(theta);
    const wa = Math.sin((1-t)*theta) / sinTheta;
    const wb = Math.sin(t*theta) / sinTheta;
    return [wa*a[0]+wb*bx, wa*a[1]+wb*by, wa*a[2]+wb*bz, wa*a[3]+wb*bw];
  }

  function motionComponents(value, arity) {
    const width = arity === 0 ? 1 : (arity === 1 ? 2 : (arity === 2 ? 3 : 4));
    const values = value && Array.isArray(value.F) ? value.F : [];
    const out = new Array(width);
    for (let i = 0; i < width; i++) out[i] = motionNumber(values[i], 0);
    return out;
  }

  function motionSpringDuration(from, to, spring) {
    const mass = motionNumber(spring.Mass != null ? spring.Mass : spring.mass, 1) || 1;
    const stiffness = motionNumber(spring.Stiffness != null ? spring.Stiffness : spring.stiffness, 100) || 100;
    const damping = motionNumber(spring.Damping != null ? spring.Damping : spring.damping, 10) || 10;
    const omega0 = Math.sqrt(stiffness / mass);
    const zeta = damping / (2 * Math.sqrt(stiffness * mass));
    const rate = zeta >= 1
      ? omega0 * (zeta - Math.sqrt(zeta*zeta - 1))
      : zeta * omega0;
    if (rate <= 0) return 10;
    let result;
    if (zeta >= 1) {
      const t0 = -Math.log(1e-3) / rate;
      result = 1.4 * ((-Math.log(1e-3) + Math.log(1 + rate*t0)) / rate);
    } else {
      result = 1.4 * (-Math.log(1e-3) / rate);
    }
    return Math.min(10, result);
  }

  function motionSpringValue(from, to, t, spring) {
    if (t <= 0) return from;
    if (t >= motionSpringDuration(from, to, spring)) return to;
    const mass = motionNumber(spring.Mass != null ? spring.Mass : spring.mass, 1) || 1;
    const stiffness = motionNumber(spring.Stiffness != null ? spring.Stiffness : spring.stiffness, 100) || 100;
    const damping = motionNumber(spring.Damping != null ? spring.Damping : spring.damping, 10) || 10;
    let velocity = motionNumber(spring.Velocity != null ? spring.Velocity : spring.velocity, 0);
    let x = from;
    const steps = Math.floor(t / (1 / 240));
    for (let i = 0; i < steps; i++) {
      const force = -stiffness * (x - to) - damping * velocity;
      velocity += (force / mass) * (1 / 240);
      x += velocity * (1 / 240);
    }
    return x;
  }

  function motionValueLerp(a, b, t, arity) {
    const av = motionComponents(a, arity);
    const bv = motionComponents(b, arity);
    if (arity === 4) return motionSlerp(av, bv, t);
    for (let i = 0; i < av.length; i++) av[i] += t * (bv[i] - av[i]);
    return av;
  }

  function motionCubic(a, outTangent, b, inTangent, delta, t, arity) {
    const av = motionComponents(a, arity), ov = motionComponents(outTangent, arity);
    const bv = motionComponents(b, arity), iv = motionComponents(inTangent, arity);
    const t2 = t*t, t3 = t2*t;
    const h00 = 2*t3 - 3*t2 + 1, h10 = t3 - 2*t2 + t;
    const h01 = -2*t3 + 3*t2, h11 = t3 - t2;
    const out = new Array(av.length);
    for (let i=0;i<out.length;i++) out[i] = h00*av[i] + delta*h10*ov[i] + h01*bv[i] + delta*h11*iv[i];
    if (arity === 4) {
      const mag = Math.sqrt(out[0]*out[0]+out[1]*out[1]+out[2]*out[2]+out[3]*out[3]);
      if (mag < 1e-15) return [0,0,0,1];
      for (let i=0;i<4;i++) out[i] /= mag;
    }
    return out;
  }

  function motionEvalGenerator(track, time, reduced, out) {
    const gen = track.Gen || track.gen || {};
    const kind = motionNumber(gen.Kind != null ? gen.Kind : gen.kind, 0);
    const base = gen.Base || gen.base || {};
    const baseValues = motionComponents(base, motionNumber(base.Arity != null ? base.Arity : base.arity, 0));
    const spring = gen.Spring || gen.spring || {};
    if (kind === 1) {
      if (reduced) out.push([0,0,4,0,0,0,1]);
      else {
        const spin = gen.Spin || gen.spin || [0,0,0];
        const x=motionNumber(spin[0],0)*time/2, y=motionNumber(spin[1],0)*time/2, z=motionNumber(spin[2],0)*time/2;
        const qx=[Math.sin(x),0,0,Math.cos(x)], qy=[0,Math.sin(y),0,Math.cos(y)], qz=[0,0,Math.sin(z),Math.cos(z)];
        const mul=(a,b)=>[a[3]*b[0]+a[0]*b[3]+a[1]*b[2]-a[2]*b[1],a[3]*b[1]-a[0]*b[2]+a[1]*b[3]+a[2]*b[0],a[3]*b[2]+a[0]*b[1]-a[1]*b[0]+a[2]*b[3],a[3]*b[3]-a[0]*b[0]-a[1]*b[1]-a[2]*b[2]];
        const q=mul(mul(qx,qy),qz);
        out.push([0,0,4,q[0],q[1],q[2],q[3]]);
      }
    } else if (kind === 2) {
      const value = reduced ? motionNumber(baseValues[1],0) : motionSpringValue(motionNumber(baseValues[0],0), motionNumber(baseValues[1],0), time, spring);
      out.push([0,0,0,value]);
    } else if (kind === 3) {
      const drift = gen.Drift || gen.drift || [0,0,0], speed = gen.DriftSpeed || gen.driftSpeed || [0,0,0], phase = gen.DriftPhase || gen.driftPhase || [0,0,0];
      const x=[];
      for(let i=0;i<3;i++) x.push(reduced ? motionNumber(baseValues[i],0) : motionNumber(baseValues[i],0)+motionNumber(drift[i],0)*Math.sin(time*motionNumber(speed[i],0)+motionNumber(phase[i],0)));
      out.push([0,0,2,x[0],x[1],x[2]]);
    } else if (kind === 4) {
      const arity=motionNumber(gen.OscArity != null ? gen.OscArity : gen.oscArity,0);
      const count=arity===0?1:(arity===1?2:(arity===2?3:4));
      const bases=gen.OscBase||gen.oscBase||[], amps=gen.OscAmp||gen.oscAmp||[], freqs=gen.OscFreq||gen.oscFreq||[], phases=gen.OscPhase||gen.oscPhase||[];
      const vals=[];
      for(let i=0;i<count;i++) vals.push(motionNumber(bases[i],0)+(reduced?0:motionNumber(amps[i],0)*Math.sin(time*motionNumber(freqs[i],0)*2*Math.PI+motionNumber(phases[i],0))));
      out.push([0,0,arity].concat(vals));
    }
  }

  function evaluateTimeline(timeline, time, reducedMotion) {
    const writes = [];
    function visit(tl, baseOffset) {
      const children = tl && (tl.Children || tl.children) || [];
      for (const child of children) {
        if (!child) continue;
        const at=child.At||child.at||{};
        const kind=motionNumber(at.Kind != null?at.Kind:at.kind,0);
        const start=baseOffset+(kind===0?motionNumber(at.Val != null?at.Val:at.val,0):0);
        const track=child.Track||child.track;
        if (track) {
          const gen=track.Gen||track.gen;
          const targetID=motionNumber(track.TargetID != null?track.TargetID:track.targetID,0);
          const propID=motionNumber(track.PropID != null?track.PropID:track.propID,0);
          if (gen) {
            const startIndex=writes.length;
            motionEvalGenerator(track,time,Boolean(reducedMotion),writes);
            if(writes.length>startIndex){writes[startIndex][0]=targetID; writes[startIndex][1]=propID;}
            continue;
          }
          const keys=track.Keys||track.keys||[];
          if (!keys.length) continue;
          let value, arity;
          if (reducedMotion) {
            const last=keys[keys.length-1], raw=last.Value||last.value||{};
            arity=motionNumber(raw.Arity != null?raw.Arity:raw.arity,0); value=motionComponents(raw,arity);
          } else {
            const local=time-start;
            const first=keys[0], last=keys[keys.length-1];
            const firstV=first.Value||first.value||{}, lastV=last.Value||last.value||{};
            arity=motionNumber(firstV.Arity != null?firstV.Arity:firstV.arity,0);
            if(local<=motionNumber(first.T != null?first.T:first.t,0)) value=motionComponents(firstV,arity);
            else if(local>=motionNumber(last.T != null?last.T:last.t,0)) value=motionComponents(lastV,arity);
            else {
              let i=0;
              while(i<keys.length-2 && motionNumber((keys[i+1].T != null?keys[i+1].T:keys[i+1].t),0)<=local) i++;
              const ka=keys[i], kb=keys[i+1];
              const ta=motionNumber(ka.T != null?ka.T:ka.t,0), tb=motionNumber(kb.T != null?kb.T:kb.t,0);
              const alpha=(local-ta)/(tb-ta), interp=motionNumber(track.Interp != null?track.Interp:track.interp,0);
              const va=ka.Value||ka.value||{}, vb=kb.Value||kb.value||{};
              if(interp===1) value=motionComponents(va,arity);
              else if(interp===2 && (ka.OutTangent||ka.outTangent) && (kb.InTangent||kb.inTangent)) {
                value=motionCubic(va,ka.OutTangent||ka.outTangent,vb,kb.InTangent||kb.inTangent,tb-ta,alpha,arity);
              } else {
                const ease=ka.Ease||ka.ease||track.Ease||track.ease||{};
                value=motionValueLerp(va,vb,motionEase(alpha,ease),arity);
              }
            }
          }
          writes.push([targetID,propID,arity].concat(value));
        }
        const sub=child.Sub||child.sub;
        if(sub) visit(sub,start);
      }
    }
    visit(timeline,0);
    return writes;
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
      const steps = Math.floor(accumulator / (1 / 240));
      accumulator -= steps * (1 / 240);
      for (let i = 0; i < steps; i++) {
        const force = -stiffness * (current - target) - damping * velocity;
        velocity += (force / mass) * (1 / 240);
        current += velocity * (1 / 240);
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
      if (reducedMotion && policy === "static") return;
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
      if (reducedMotion && reducedPolicy === "static") return;
      if (reducedMotion && reducedPolicy === "fade") length = Math.min(length || 100, 120);
      if (!stopContinuous) stopContinuous = scheduler.addContinuous(tick);
    }

    value.retarget = animateTo;
    value.target = function() { return motionClone(target); };
    const stopReduced = function(next) {
      if (next) {
        if (reducedPolicy === "skip") value.set(target);
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
    if (reducedMotion && reducedPolicy === "skip") value.set(last);
    else if (reducedMotion && reducedPolicy === "static") value.set(first);
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
    let startMS = scheduler.now() + (reduce ? 0 : delay);
    let stopContinuous = null;
    let transform = motionTransformComponents(first.transform);
    const endTransform = motionTransformComponents(last.transform);
    const id = ++nextAnimationID;
    let animateOpacityOnly = reduce && policy === "fade";
    let runtimeDuration = animateOpacityOnly ? Math.min(duration || 120, 120) : duration;
    let stopReduced = null;
    let resolveFinished;
    const finished = new Promise(function(resolve) { resolveFinished = resolve; });

    function finish(styles) {
      scheduler.queueWrite("motion-animation:" + id, function() {
        for (const property of Object.keys(styles)) motionSetStyle(element, property, styles[property]);
      });
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
      active = false;
      resolveFinished({ finished: true });
      return { finished: finished, cancel: function() { active = false; } };
    }
    for (const property of Object.keys(first)) {
      motionSetStyle(element, property, animateOpacityOnly && property !== "opacity" ? last[property] : first[property]);
    }
    function tick(now) {
      if (!active) return false;
      const elapsed = now - startMS;
      if (elapsed < 0) return true;
      const progress = runtimeDuration <= 0 ? 1 : motionClamp(elapsed / runtimeDuration, 0, 1);
      const eased = motionEase(progress, ease);
      const styles = {};
      for (const property of Object.keys(last)) {
        if (animateOpacityOnly && property !== "opacity") {
          styles[property] = last[property];
          continue;
        }
        if (property === "opacity" && typeof first.opacity === "number" && typeof last.opacity === "number") {
          styles.opacity = first.opacity + (last.opacity - first.opacity) * eased;
        } else if (property === "transform" && transform.known && endTransform.known && !animateOpacityOnly) {
          styles.transform = motionFormatTransform({
            x: transform.x + (endTransform.x - transform.x) * eased,
            y: transform.y + (endTransform.y - transform.y) * eased,
            z: transform.z + (endTransform.z - transform.z) * eased,
            rotate: transform.rotate + (endTransform.rotate - transform.rotate) * eased,
            scale: transform.scale + (endTransform.scale - transform.scale) * eased,
            base: endTransform.base || transform.base,
            known: true,
          });
        } else {
          styles[property] = progress >= 1 ? last[property] : first[property];
        }
      }
      scheduler.queueWrite("motion-animation:" + id, function() {
        for (const property of Object.keys(styles)) motionSetStyle(element, property, styles[property]);
      });
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
        active = false;
        if (stopContinuous) stopContinuous();
        stopContinuous = null;
        reducedListeners.delete(stopReduced);
        resolveFinished({ finished: true });
      } else {
        animateOpacityOnly = true;
        runtimeDuration = Math.min(runtimeDuration || 120, 120);
        startMS = scheduler.now();
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
      const start = scheduler.now();
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
    if (kind === "mix") {
      const a = getValue(spec.a), b = getValue(spec.b), weight = getValue(spec.weight);
      const value = createSignal(motionMix(a.get(), b.get(), motionClamp(motionNumber(weight.get(), 0), 0, 1)), name);
      const update = function() { value.set(motionMix(a.get(), b.get(), motionClamp(motionNumber(weight.get(), 0), 0, 1))); };
      a.subscribe(update, { immediate: false }); b.subscribe(update, { immediate: false }); weight.subscribe(update, { immediate: false });
      return value;
    }
    return createSignal(0, name);
  }

  function createProgram(root, raw) {
    const program = raw && typeof raw === "object" ? raw : {};
    if (motionNumber(program.version, 0) !== 1 || !Array.isArray(program.signals)) return null;
    const programID = String(program.id || "motion");
    const record = { root: root, id: programID, signals: new Map(), specs: new Map(), bindings: [], pins: [], stops: [], values: [], disposed: false };
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
      if (!binding || !binding.signal) continue;
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
        const apply = function(next) {
          item.latest = motionClone(next);
          scheduler.queueWrite(item, function() {
            if (item.adapter && !record.disposed) {
              item.adapter.write(binding, motionClone(item.latest));
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
      }
    }
    if (record.pins.length) {
      record.stops.push(scheduler.on("write", function() {
        for (const pin of record.pins) {
          if (!pin.adapter || record.disposed) continue;
          const elementRect = scheduler.rect(pin.element), sceneRect = scheduler.rect(pin.scene);
          if (!elementRect || !sceneRect) continue;
          if (pin.adapter.pin(pin.spec, elementRect, sceneRect) && typeof pin.adapter.invalidate === "function") pin.adapter.invalidate("pin-to");
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

  function walkMotionElements(root, callback) {
    if (!root) return;
    if (root.nodeType === 1 && root.hasAttribute && root.hasAttribute("data-gosx-motion-program")) callback(root);
    const children = root.children || root.childNodes || [];
    for (const child of children) if (child && child.nodeType === 1) walkMotionElements(child, callback);
  }

  function mountPrograms(root) {
    const target = root || document.body || document.documentElement;
    walkMotionElements(target, mountProgramElement);
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
        scheduler.queueWrite(binding, function() {
          if (!record.disposed) {
            adapter.write(binding.binding, motionClone(binding.latest));
            if (typeof adapter.invalidate === "function") adapter.invalidate("motion-binding");
          }
        });
      }
      for (const pin of record.pins) if (pin.scene === mount) pin.adapter = adapter;
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
    const transform = motionTransformComponents(element.style && element.style.transform);
    return signal.subscribe(function(value) {
      scheduler.queueWrite(element, function() { setBoundDOMValue(binding, element, value, transform); });
    });
  }

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
      if (current !== previous) value.set((current - previous) / dt);
      previous = current; previousMS = now;
    });
    value.dispose = stop;
    return value;
  };
  motion.bind = bindDOM;
  motion.animateKeyframes = animateKeyframes;
  motion.animate = function(element, keyframes, options) { return animateKeyframes(element, keyframes, options); };
  motion.evaluateTimeline = evaluateTimeline;
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
