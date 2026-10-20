// Optional input-driven scalar tracks. Native Track/Key/Ease semantics, shared scheduler.
(function() {
  "use strict";
  const motion = window.__gosx.motion;
  if (motion.createTrackSampler) return;

  function slerp(out, a, b, t) {
    let dot = 0;
    for (let i = 0; i < 4; i++) dot += a[i] * b[i];
    const sign = dot < 0 ? -1 : 1;
    dot *= sign;
    if (dot > .9995) {
      let norm = 0;
      for (let i = 0; i < 4; i++) { out[i] = a[i] + t * (sign * b[i] - a[i]); norm += out[i] * out[i]; }
      norm = Math.sqrt(norm);
      for (let i = 0; i < 4; i++) out[i] = norm > 0 ? out[i] / norm : (i === 3 ? 1 : 0);
    } else {
      const angle = Math.acos(Math.max(-1, Math.min(1, dot))), sine = Math.sin(angle);
      const wa = Math.sin((1 - t) * angle) / sine, wb = Math.sin(t * angle) / sine * sign;
      for (let i = 0; i < 4; i++) out[i] = wa * a[i] + wb * b[i];
    }
    return out;
  }

  function createTrackSampler(frames, arity = 0) {
    if (!Number.isInteger(arity) || arity < 0 || arity > 5) throw new TypeError("invalid motion track arity");
    if (!Array.isArray(frames) || frames.length < 2 || frames.length > 4096) throw new RangeError("motion track needs 2..4096 keys");
    const width = [1, 2, 3, 4, 4, 4][arity];
    let previous = -Infinity;
    const keys = frames.map(frame => {
      if (!frame || !Number.isFinite(frame.at) || frame.at <= previous) throw new TypeError("motion track needs increasing finite times");
      let value = frame.value;
      if (arity) {
        if (!Array.isArray(value) || value.length !== width) throw new TypeError("invalid motion track vector width");
        value = Array.from(value);
        if (value.some(component => !Number.isFinite(component))) throw new TypeError("motion track needs finite components");
        if (arity === 4 && Math.abs(value.reduce((sum, component) => sum + component * component, 0) - 1) > 1e-6) throw new TypeError("motion track needs unit quaternions");
      } else if (!Number.isFinite(value)) throw new TypeError("motion track needs finite scalar values");
      previous = frame.at;
      const raw = frame.ease || {kind: 0}, args = raw.args == null ? [] : raw.args;
      if (!Number.isInteger(raw.kind) || raw.kind < 0 || raw.kind > 8 || !Array.isArray(args) || args.length > 4 || Array.from(args).some(arg => !Number.isFinite(arg)) ||
          raw.kind >= 1 && raw.kind <= 3 && args.length && args[0] <= 0 ||
          raw.kind === 5 && args.length && (!Number.isInteger(args[0]) || args[0] < 1) ||
          raw.kind === 4 && (args.length !== 4 || args[0] < 0 || args[0] > 1 || args[2] < 0 || args[2] > 1)) throw new TypeError("invalid motion track easing");
      return {at: frame.at, value, ease: {kind: raw.kind, args: args.slice()}};
    });
    return function(input, output) {
      if (!Number.isFinite(input)) throw new RangeError("motion track input must be finite");
      let lo = 0, hi = keys.length - 1;
      if (input <= keys[0].at) hi = lo;
      else if (input >= keys[hi].at) lo = hi;
      else while (hi - lo > 1) { const mid = (lo + hi) >>> 1; if (keys[mid].at <= input) lo = mid; else hi = mid; }
      const a = keys[lo], b = keys[hi], t = lo === hi ? 0 : motion.ease((input - a.at) / (b.at - a.at), a.ease);
      if (!arity) return lo === hi ? a.value : a.value + (b.value - a.value) * t;
      const out = output == null ? new Array(width) : output;
      if (!Array.isArray(out) || out.length !== width) throw new RangeError("motion track output has wrong width");
      if (arity === 4 && lo !== hi) return slerp(out, a.value, b.value, t);
      for (let i = 0; i < width; i++) out[i] = lo === hi ? a.value[i] : a.value[i] + (b.value[i] - a.value[i]) * t;
      return out;
    };
  }

  motion._formatStyleValue = function(property, value, unit) {
    if (["color", "background", "backgroundColor", "background-color"].includes(property) && value.length === 4) {
      const clamp = value => Math.max(0, Math.min(1, value));
      return "rgba(" + value.slice(0, 3).map(channel => Math.round(clamp(channel) * 255)).join(",") + "," + clamp(value[3]) + ")";
    }
    return value.map(component => String(component) + (unit || "")).join(" ");
  };

  function applyProperty(target, property, value) {
    if (!target || typeof target !== "object") return;
    const parts = String(property || "").split("."), group = parts.length > 1 ? parts[0] : "";
    let field = parts[parts.length - 1];
    if (group === "rotation" && ["x", "y", "z"].includes(field)) field = "rotation" + field.toUpperCase();
    if (group === "scale" && ["x", "y", "z"].includes(field)) field = "scale" + field.toUpperCase();
    if (["x", "y", "z", "rotationX", "rotationY", "rotationZ", "scaleX", "scaleY", "scaleZ", "opacity", "fov", "near", "far", "zoom"].includes(field)) target[field] = value;
  }

  // Scene layers resolve before geometry baking and material interning. These
  // adapters are optional so static scenes do not carry their implementation.
  motion._sceneObjects = function(objects, state) {
    const layers = state && state._gosxMotionRuntimeBindings;
    if (!layers || !layers.size) return objects;
    const byID = new Map();
    for (const layer of layers.values()) for (const item of layer.values()) {
      if (item.binding.target !== "sceneNode" && item.binding.target !== "materialUniform") continue;
      const id = String(item.binding.node || "");
      let entries = byID.get(id);
      if (!entries) byID.set(id, entries = []);
      entries.push(item);
    }
    return objects.map(source => {
      const entries = byID.get(String(source.id || ""));
      if (!entries) return source;
      const object = Object.assign({}, source, {_rigidMaterialProfileStable: false});
      for (const item of entries) {
        if (item.binding.target === "materialUniform") {
          if (object.customUniforms === source.customUniforms) object.customUniforms = Object.assign({}, source.customUniforms);
          object.customUniforms[item.binding.property] = item.value;
        } else applyProperty(object, item.binding.property, item.value);
      }
      return object;
    });
  };
  function sceneUniform(bundle, object, property, value, overrides) {
    const material = bundle.materials && bundle.materials[object.materialIndex];
    if (overrides && material) {
      let copy = overrides.get(object);
      if (!copy) {
        copy = Object.assign({}, material, {customUniforms: Object.assign({}, material.customUniforms, object.customUniforms)});
        overrides.set(object, copy);
        object.materialIndex = bundle.materials.push(copy) - 1;
      }
      object.customUniforms = copy.customUniforms;
    } else object.customUniforms = Object.assign({}, object.customUniforms);
    object.customUniforms[property] = value;
  };

  motion._sceneBinding = function(bundle, binding, value, materials) {
    for (const object of bundle.meshObjects || []) {
      if (!object || String(object.id || "") !== String(binding.node || "")) continue;
      if (binding.target === "materialUniform") sceneUniform(bundle, object, binding.property, value, materials);
      else if (binding.target === "sceneNode") applyProperty(object, binding.property, value);
    }
  };

  motion.createTrackSampler = createTrackSampler;
  motion.registerProgramFeature(function(record, spec) {
    for (const adapter of record.adapters) if (adapter.invalidate) adapter.invalidate("motion-track-ready");
    for (const track of spec.signals || []) {
      if (track.kind !== "track") continue;
      const input = record.signals.get(track.input), output = record.signals.get(track.id);
      if (!input || !output) continue;
      let sample;
      try { sample = createTrackSampler(track.frames, track.arity || 0); } catch (_error) { continue; }
      const scratch = track.arity ? new Array([1, 2, 3, 4, 4, 4][track.arity]) : undefined;
      const rest = sample(Number.MAX_VALUE);
      const update = function() {
        const time = input.get();
        if (Number.isFinite(time)) output.set(motion.reducedMotion() ? rest : sample(time, scratch));
      };
      record.stops.push(input.subscribe(update), motion.onReducedMotion(update));
    }
  });
  if (window.__gosx_register_bootstrap_feature) window.__gosx_register_bootstrap_feature("motion-track", () => ({}));
})();
