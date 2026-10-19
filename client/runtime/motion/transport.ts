// Optional shared motion transport. Loaded through RequireFeature("motion-transport").
(function() {
  "use strict";
  const motion = window.__gosx.motion, scheduler = motion.scheduler;
  if (motion.createTransport) return;
  function clamp(value, min, max) { return Math.max(min, Math.min(max, value)); }
  // A transport samples absolute poses on the existing scheduler. It owns no
  // separate RAF loop and releases its scheduler slot at either endpoint.
  function createMotionTransport(options) {
    const opts = options || {}, adapters = new Set(), listeners = new Set();
    let time = 0, direction = 1, paused = opts.autoPlay !== true, disposed = false;
    let stop = null, last = null, revision = 0, previousTime = NaN, previousDirection = 0, previousPaused = null;
    let intent = 0;
    const labels = Object.assign(Object.create(null), opts.labels || {});
    function duration() {
      const value = typeof opts.durationMs === "function" ? opts.durationMs() : opts.durationMs;
      if (!Number.isFinite(value) || value < 0 || value > 86400000) throw new RangeError("motion transport duration");
      return value;
    }
    function state() { return snapshot(duration()); }
    function snapshot(span) { return { time: time, direction: direction, paused: paused, duration: span }; }
    function alive() { if (disposed) throw new Error("motion transport disposed"); }
    function position(value) {
      const at = typeof value === "string" ? labels[value] : value;
      if (!Number.isFinite(at)) throw new RangeError("motion transport position or label");
      return clamp(at, 0, duration());
    }
    function sample(span) {
      const current = snapshot(span == null ? duration() : span);
      if (time !== previousTime || direction !== previousDirection || paused !== previousPaused) {
        revision++; previousTime = time; previousDirection = direction; previousPaused = paused;
      }
      for (const adapter of adapters) adapter.sample(time, current);
      for (const listener of listeners) listener(current);
    }
    function halt() { if (stop) stop(); stop = null; last = null; }
    function tick(now) {
      if (disposed || paused) { stop = null; return false; }
      if (typeof document !== "undefined" && document.hidden) { last = null; return true; }
      const span = duration();
      if (last != null) time = clamp(time + clamp(now - last, 0, 250) * direction, 0, span);
      last = now;
      sample(span);
      if (direction > 0 ? time >= span : time <= 0) { stop = null; last = null; return false; }
      return true;
    }
    function start() {
      halt();
      if (paused || disposed || (typeof document !== "undefined" && document.hidden)) return;
      if (motion.reducedMotion()) { time = duration(); sample(); return; }
      if (direction > 0 ? time < duration() : time > 0) stop = scheduler.addContinuous(tick);
    }
    function visibility() { if (document.hidden) halt(); else start(); }
    const reduce = function(next) { if (next) { halt(); time = duration(); sample(); } else start(); };
    duration();
    for (const name of Object.keys(labels)) if (!Number.isFinite(labels[name]) || labels[name] < 0 || labels[name] > duration()) throw new RangeError("motion transport label");
    const stopReduced = motion.onReducedMotion(reduce);
    if (typeof document !== "undefined" && document.addEventListener) document.addEventListener("visibilitychange", visibility);
    const transport = {
      state: state, duration: duration,
      suspend: function() { alive(); halt(); },
      pause: function() { alive(); if (!paused) intent++; halt(); paused = true; sample(); },
      play: function() { alive(); if (paused || direction !== 1) intent++; paused = false; direction = 1; sample(); start(); },
      seek: function(at) { alive(); const next = position(at); if (time !== next || !paused) intent++; halt(); paused = true; time = next; sample(); },
      reverse: function() { alive(); if (paused || direction !== -1 || time === 0) intent++; paused = false; direction = -1; if (time === 0) time = duration(); sample(); start(); },
      replay: function() { alive(); if (time !== 0 || direction !== 1) intent++; halt(); time = 0; direction = 1; sample(); start(); },
      restore: function(next) {
        alive();
        if (!next || typeof next.paused !== "boolean" || (next.direction !== 1 && next.direction !== -1)) throw new TypeError("motion transport state");
        const at = position(next.time);
        if (time !== at || paused !== next.paused || direction !== next.direction) intent++;
        halt(); time = at; paused = next.paused; direction = next.direction; sample(); start();
      },
      refresh: function() { alive(); time = clamp(time, 0, duration()); sample(); if (!stop) start(); },
      subscribe: function(listener) { alive(); listeners.add(listener); listener(state()); return function() { listeners.delete(listener); }; },
      attach: function(adapter) {
        alive();
        if (!adapter || typeof adapter.sample !== "function") throw new TypeError("motion transport adapter");
        adapters.add(adapter); adapter.sample(time, state());
        return function() { adapters.delete(adapter); };
      },
      settled: async function(options) {
        alive();
        const config = options || {}, timeout = config.timeoutMS == null ? 5000 : config.timeoutMS;
        if (!Number.isFinite(timeout) || timeout <= 0 || timeout > 60000) throw new RangeError("motion transport settle timeout");
        const requestedIntent = intent, deadline = performance.now() + timeout;
        let timer, waiting = true;
        try {
          await Promise.race([
            (async function() {
              // Flush writes and await producers before requesting fresh
              // renderer submissions and presentation boundaries for capture.
              let version;
              do {
                version = revision;
                await new Promise(function(resolve) { scheduler.request(resolve); });
                if (!waiting) return;
                await Promise.all(Array.from(adapters, function(adapter) { return adapter.settled ? adapter.settled({timeoutMS: timeout}) : undefined; }));
                if (!waiting) return;
                await Promise.all(Array.from(adapters, function(adapter) { return adapter.present ? adapter.present({timeoutMS: Math.max(0, deadline - performance.now())}) : undefined; }));
                await new Promise(function(resolve) { scheduler.request(function() { scheduler.request(resolve); }); });
                if (intent !== requestedIntent) throw new Error("motion transport changed while settling");
              } while (waiting && !disposed && version !== revision);
            })(),
            new Promise(function(_resolve, reject) { timer = setTimeout(function() { reject(new Error("motion transport did not settle")); }, timeout); }),
          ]);
          alive();
        } finally { waiting = false; clearTimeout(timer); }
      },
      dispose: function() {
        if (disposed) return;
        disposed = true; halt(); adapters.clear(); listeners.clear(); stopReduced();
        if (typeof document !== "undefined" && document.removeEventListener) document.removeEventListener("visibilitychange", visibility);
      },
    };
    start();
    return transport;
  }

  // Mount readiness is asynchronous. Apply only the latest clock when a
  // deferred engine appears, rather than queueing a promise for every frame.
  function bindTransportScene(transport, mount) {
    let active = true;
    function sample(time, force) {
      const handle = mount.__gosxScene3DHandle;
      if (!handle || typeof handle.setAnimationClock !== "function") return;
      const seconds = motion.reducedMotion() ? 0 : time / 1000, clock = handle.getAnimationClock();
      if (force || clock.timeSeconds !== seconds || !clock.paused) handle.setAnimationClock({ timeSeconds: seconds });
    }
    const detach = transport.attach({ sample: function(time) { sample(time, false); }, present: function(options) {
      let submittedHandle;
      const deadline = performance.now() + (options ? options.timeoutMS : 5000);
      return (async function() {
        while (true) {
          if (!active) throw new Error("motion scene binding disposed");
          const current = mount.__gosxScene3DHandle;
          if (current && current !== submittedHandle) { sample(transport.state().time, true); submittedHandle = current; }
          if (current && typeof current.getAnimationClock === "function" && !current.getAnimationClock().pending) return;
          if (performance.now() >= deadline) throw new Error("motion scene frame did not submit");
          await new Promise(function(resolve) { scheduler.request(resolve); });
        }
      })();
    } });
    const observer = typeof MutationObserver === "function" ? new MutationObserver(function() { sample(transport.state().time); }) : null;
    if (observer) observer.observe(mount, { attributes: true, attributeFilter: ["data-gosx-scene3d-ready", "data-gosx-scene3d-mounted"] });
    return function() { active = false; detach(); if (observer) observer.disconnect(); };
  }


  const programs = new Map();
  function enableControls(target) {
    const buttons = Array.from(target.querySelectorAll("[data-gosx-transport-action]"));
    if (target.hasAttribute && target.hasAttribute("data-gosx-transport-action")) buttons.push(target);
    for (const button of buttons) if (motion.transport(button.getAttribute("data-gosx-transport"))) button.disabled = false;
  }
  motion.transport = function(target) {
    for (const [element, entry] of programs) if (element === target || entry.record.id === target) return entry.transport;
    return null;
  };
  motion.registerProgramFeature(function(record, spec) {
    if (!spec.transport) return;
    const element = record.root;
    let transport;
    try { transport = createMotionTransport(spec.transport); } catch (_error) { return; }
    programs.set(element, {record: record, transport: transport});
    record.transport = transport;
    for (const adapter of record.transportAdapters || []) record.stops.push(transport.attach(adapter));
    record.stops.push(function() { programs.delete(element); transport.dispose(); });
    for (const signal of spec.signals || []) if (signal.kind === "playhead") {
      const value = record.signals.get(signal.id);
      if (value) record.stops.push(transport.subscribe(function(state) { value.set(state.time / 1000); }));
    }
    for (const selector of spec.transport.scenes || []) {
      let mounts = [];
      try { mounts = element.querySelectorAll(selector); } catch (_error) { continue; }
      for (const mount of mounts) record.stops.push(bindTransportScene(transport, mount));
    }
    enableControls(document.body || document.documentElement);
  }, enableControls);
  motion.createTransport = createMotionTransport;
  motion.bindTransportScene = bindTransportScene;
  if (typeof document !== "undefined" && document.addEventListener) {
      document.addEventListener("click", function(event) {
        const button = event.target && event.target.closest && event.target.closest("[data-gosx-transport-action]");
        if (!button || button.disabled) return;
        const transport = motion.transport(button.getAttribute("data-gosx-transport"));
        if (!transport) return;
        const action = button.getAttribute("data-gosx-transport-action");
        if (["play", "pause", "reverse", "replay"].indexOf(action) >= 0) transport[action]();
        else if (action === "seek") {
          try { transport.seek(button.getAttribute("data-gosx-transport-label")); } catch (_error) { /* Invalid author labels leave the pose unchanged. */ }
        }
      });
  }
  motion.mountPrograms(document.body || document.documentElement);
  if (window.__gosx_register_bootstrap_feature) window.__gosx_register_bootstrap_feature("motion-transport", () => ({}));
})();
