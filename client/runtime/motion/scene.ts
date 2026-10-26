// Optional absolute Scene3D poses. Uses the shared scheduler and command bridge.
(function() {
  "use strict";
  const motion = window.__gosx.motion;
  if (motion.prepareSceneTransition) return;
  const discrete = new Set(["id", "kind", "type", "name", "geometry", "text", "count", "lineSegments", "indices", "segments", "radialSegments", "tubularSegments"]);
  const own = (value, key) => Object.prototype.hasOwnProperty.call(value, key);
  const commandKey = command => command.kind < 2 ? "object:" + command.objectId : command.kind + ":" + (command.objectId || "");
  const object = value => value !== null && typeof value === "object" && !Array.isArray(value);

  function copy(value, depth, budget) {
    if (depth > 32 || --budget.left < 0) throw new RangeError("scene transition value budget exceeded");
    if (typeof value === "number" && !Number.isFinite(value)) throw new TypeError("scene transition values must be finite");
    if (Array.isArray(value)) return value.map(child => copy(child, depth + 1, budget));
    if (object(value)) {
      const result = {};
      for (const key of Object.keys(value)) {
        if (["__proto__", "prototype", "constructor"].includes(key)) throw new TypeError("scene transition contains an unsafe data key");
        result[key] = copy(value[key], depth + 1, budget);
      }
      return result;
    }
    if (value !== null && !["string", "number", "boolean"].includes(typeof value)) throw new TypeError("scene transition requires JSON values");
    return value;
  }
  function freeze(value) {
    if (value && typeof value === "object") { Object.values(value).forEach(freeze); Object.freeze(value); }
    return value;
  }
  function defaults(value, keys, fallback) { for (const key of keys) if (!own(value, key)) value[key] = fallback; }
  function commands(input, budget) {
    if (!Array.isArray(input) || input.length > 10000) throw new RangeError("scene transition supports at most 10000 commands per pose");
    const result = new Map();
    for (const raw of input) {
      if (!raw || !Number.isInteger(raw.kind) || raw.kind < 0 || raw.kind > 15 || raw.kind <= 4 && !raw.objectId || raw.objectId != null && (typeof raw.objectId !== "string" || raw.objectId.length > 256) || raw.kind !== 1 && raw.data == null) throw new TypeError("scene transition contains an invalid command");
      const command = {kind: raw.kind};
      if (raw.objectId) command.objectId = raw.objectId;
      const snapshot = copy(raw.data == null ? null : raw.data, 0, budget);
      if (snapshot != null) command.data = snapshot;
      const data = command.data;
      if (object(data)) {
        if (command.kind === 2 || command.kind === 5) {
          defaults(data, ["x", "y", "z", "rotationX", "rotationY", "rotationZ"], 0);
          if (command.kind === 2) defaults(data, ["scaleX", "scaleY", "scaleZ"], 1);
          else data.transitionMS = 0;
        }
        if (command.kind === 0 && object(data.props)) {
          defaults(data.props, ["x", "y", "z"], 0);
          if (data.kind == null || data.kind === "mesh") {
            defaults(data.props, ["rotationX", "rotationY", "rotationZ"], 0);
            defaults(data.props, ["scaleX", "scaleY", "scaleZ"], 1);
          }
          for (const point of Array.isArray(data.props.points) ? data.props.points : []) if (object(point)) defaults(point, ["x", "y", "z"], 0);
        }
      }
      result.set(commandKey(command), freeze(command));
    }
    return result;
  }
  function compile(a, b, key, budget, depth = 0) {
    if (depth > 32 || --budget.left < 0) throw new RangeError("scene transition value budget exceeded");
    if (a === b) return {fixed: true, sample: () => a};
    let sample;
    if (discrete.has(key) && JSON.stringify(a) === JSON.stringify(b)) return {fixed: true, sample: () => a};
    if (a === undefined || b === undefined || discrete.has(key)) sample = t => t === 1 ? b : a;
    else if (typeof a === "number" && typeof b === "number") sample = t => a * (1 - t) + b * t;
    else if (key === "color" && /^#[\da-f]{6}$/i.test(a) && /^#[\da-f]{6}$/i.test(b)) {
      const channels = value => [1, 3, 5].map(i => parseInt(value.slice(i, i + 2), 16)), from = channels(a), to = channels(b);
      sample = t => "#" + from.map((v, i) => Math.round(v * (1 - t) + to[i] * t).toString(16).padStart(2, "0")).join("");
    } else if (Array.isArray(a) && Array.isArray(b) && a.length === b.length) {
      const tracks = a.map((v, i) => compile(v, b[i], key, budget, depth + 1));
      if (tracks.every(track => track.fixed)) return {fixed: true, sample: () => a};
      sample = t => Object.freeze(tracks.map(track => track.sample(t)));
    } else if (object(a) && object(b)) {
      const keys = Array.from(new Set([...Object.keys(a), ...Object.keys(b)])).sort();
      const tracks = keys.map(k => compile(a[k], b[k], k, budget, depth + 1));
      if (tracks.every(track => track.fixed)) return {fixed: true, sample: () => a};
      sample = t => {
        const result = {};
        for (let i = 0; i < keys.length; i++) { const value = tracks[i].sample(t); if (value !== undefined) result[keys[i]] = value; }
        return Object.freeze(result);
      };
    } else sample = t => t === 1 ? b : a;
    return {fixed: false, sample: t => t === 0 ? a : t === 1 ? b : sample(t)};
  }

  function prepareSceneTransition(from, to) {
    const budget = {left: 200000}, a = commands(from, budget), b = commands(to, budget);
    if (new TextEncoder().encode(JSON.stringify({version: 1, from, to})).length > 4 * 1024 * 1024) throw new RangeError("scene transition exceeds 4 MiB");
    budget.left = 200000;
    const plans = Array.from(new Set([...a.keys(), ...b.keys()]), key => {
      const old = a.get(key), next = b.get(key);
      let sample;
      if (!old || !next) sample = () => old || next;
      else if (old.kind !== next.kind || old.kind === 1 || old.kind === 0 && (old.data.kind !== next.data.kind || old.data.geometry !== next.data.geometry)) sample = t => t === 1 ? next : old;
      else {
        const track = compile(old.data, next.data, "", budget);
        sample = track.fixed ? () => old : t => t === 0 ? old : t === 1 ? next : Object.freeze({...old, data: track.sample(t)});
      }
      return {from: old || next, to: next || old, sample};
    });
    // Upserts must precede dependent patches. Compile both discrete endpoint
    // orders once rather than sorting every sampled frame.
    const order = endpoint => plans.slice().sort((x, y) => (x[endpoint].kind === 0 ? -1 : 0) - (y[endpoint].kind === 0 ? -1 : 0)).map(plan => plan.sample);
    const start = order("from"), end = order("to");
    let previous = NaN, cached;
    return Object.freeze({sample: function(progress) {
      if (!Number.isFinite(progress)) throw new RangeError("scene transition progress must be finite");
      const t = Math.max(0, Math.min(1, progress));
      if (t !== previous) { cached = Object.freeze((t === 1 ? end : start).map(sample => sample(t))); previous = t; }
      return cached;
    }});
  }

  // Coalesce samples while loading or applying asynchronous commands. The
  // existing bridge owns readiness, asset resolution and ordered submission.
  function bindSceneTransition(mount, transition) {
    if (!mount || typeof mount.getAttribute !== "function") throw new TypeError("scene transition requires a mount");
    let plan, desired, pending = null, revision = 0, applied = -1, owner, error, active = true, progress = 0;
    const last = new Map(), abort = new AbortController();
    function setTransition(next) { if (!next || typeof next.sample !== "function") throw new TypeError("scene transition requires a prepared plan"); plan = next; }
    setTransition(transition);
    function pump() {
      if (!active || !desired || pending) return;
      pending = (async function() {
        const deadline = performance.now() + 10000;
        let api;
        while (!(api = window.__gosx.scene3d)?.whenReady) {
          if (!active) return;
          if (performance.now() >= deadline) throw new Error("scene command bridge is unavailable");
          await new Promise(resolve => {
            const finish = () => { motion.scheduler.cancel(frame); abort.signal.removeEventListener("abort", finish); resolve(); };
            const frame = motion.scheduler.request(finish); abort.signal.addEventListener("abort", finish, {once: true});
          });
        }
        await api.whenReady(mount, {signal: abort.signal, timeoutMS: 10000});
        while (active) {
          const handle = mount.__gosxScene3DHandle, pose = desired, version = revision;
          if (owner !== handle) { last.clear(); owner = handle; }
          const changes = pose.filter(command => last.get(commandKey(command)) !== command);
          if (changes.length) await api.dispatchCommands(mount, changes, {signal: abort.signal, timeoutMS: 10000});
          if (!active) return;
          if (owner !== mount.__gosxScene3DHandle) { last.clear(); continue; }
          last.clear(); for (const command of pose) last.set(commandKey(command), command);
          applied = version; error = null;
          if (version === revision) return;
        }
      })().catch(next => {
        const current = mount.__gosxScene3DHandle;
        error = next;
        if (current && owner !== current) { owner = current; last.clear(); error = null; }
      }).finally(function() { pending = null; if (active && !error && revision !== applied) pump(); });
    }
    function sample(value) {
      if (!active) return;
      if (!Number.isFinite(value)) throw new RangeError("scene transition progress must be finite");
      progress = value; desired = plan.sample(motion.reducedMotion() ? 1 : value); revision++; error = null; pump();
    }
    const observer = typeof MutationObserver === "function" ? new MutationObserver(function() {
      if (owner === mount.__gosxScene3DHandle) return;
      last.clear(); applied = -1; revision++; error = null; pump();
    }) : null;
    if (observer) observer.observe(mount, {attributes: true, attributeFilter: ["data-gosx-scene3d-ready", "data-gosx-scene3d-mounted"]});
    const stopReduced = motion.onReducedMotion(function() { if (desired) sample(progress); });
    return {sample, setTransition, settled: async function() {
      while (pending) await pending;
      if (!active) throw new Error("scene transition disposed");
      if (error) throw error;
    }, dispose: function() { if (!active) return; active = false; abort.abort(); if (observer) observer.disconnect(); stopReduced(); last.clear(); }};
  }

  motion.prepareSceneTransition = prepareSceneTransition;
  motion.bindSceneTransition = bindSceneTransition;
  motion.registerProgramFeature(function(record, spec) {
    for (const binding of (Array.isArray(spec.bindings) ? spec.bindings : []).filter(binding => binding?.target === "sceneTransition").slice(0, 64)) {
      let controller;
      const signal = record.signals.get(binding.signal);
      if (!signal || binding.property !== "pose" || binding.unit || binding.transition?.version !== 1) continue;
      try { controller = bindSceneTransition(record.root.querySelector(binding.selector), prepareSceneTransition(binding.transition.from, binding.transition.to)); } catch (_error) { continue; }
      let latest = signal.get(), active = true;
      const apply = value => { latest = value; motion.scheduler.queueWrite(controller, function() { if (active && Number.isFinite(latest)) controller.sample(latest); }); };
      const stop = signal.subscribe(apply), adapter = {sample() {}, settled: controller.settled};
      if (!record.transportAdapters) record.transportAdapters = new Set();
      record.transportAdapters.add(adapter);
      if (record.transport) record.stops.push(record.transport.attach(adapter));
      record.stops.push(function() { active = false; stop(); record.transportAdapters.delete(adapter); controller.dispose(); });
    }
  });
  if (window.__gosx_register_bootstrap_feature) window.__gosx_register_bootstrap_feature("motion-scene", () => ({}));
})();
