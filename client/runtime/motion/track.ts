// Optional input-driven scalar tracks. Native Track/Key/Ease semantics, shared scheduler.
(function() {
  "use strict";
  const motion = window.__gosx.motion;
  if (motion.createTrackSampler) return;

  function createTrackSampler(frames) {
    if (!Array.isArray(frames) || frames.length < 2 || frames.length > 4096) throw new RangeError("motion track needs 2..4096 keys");
    let previous = -Infinity;
    const keys = frames.map(frame => {
      if (!frame || !Number.isFinite(frame.at) || !Number.isFinite(frame.value) || frame.at <= previous) throw new TypeError("motion track needs finite values and increasing times");
      previous = frame.at;
      const raw = frame.ease || {kind: 0}, args = raw.args == null ? [] : raw.args;
      if (!Number.isInteger(raw.kind) || raw.kind < 0 || raw.kind > 8 || !Array.isArray(args) || args.length > 4 || args.some(arg => !Number.isFinite(arg)) ||
          raw.kind >= 1 && raw.kind <= 3 && args.length && args[0] <= 0 ||
          raw.kind === 5 && args.length && (!Number.isInteger(args[0]) || args[0] < 1) ||
          raw.kind === 4 && (args.length !== 4 || args[0] < 0 || args[0] > 1 || args[2] < 0 || args[2] > 1)) throw new TypeError("invalid motion track easing");
      return {at: frame.at, value: frame.value, ease: {kind: raw.kind, args: args.slice()}};
    });
    return function(input) {
      if (!Number.isFinite(input)) throw new RangeError("motion track input must be finite");
      if (input <= keys[0].at) return keys[0].value;
      if (input >= keys[keys.length - 1].at) return keys[keys.length - 1].value;
      let lo = 0, hi = keys.length - 1;
      while (hi - lo > 1) { const mid = (lo + hi) >>> 1; if (keys[mid].at <= input) lo = mid; else hi = mid; }
      const a = keys[lo], b = keys[hi], t = motion.ease((input - a.at) / (b.at - a.at), a.ease);
      return a.value + (b.value - a.value) * t;
    };
  }

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
      try { sample = createTrackSampler(track.frames); } catch (_error) { continue; }
      const rest = track.frames[track.frames.length - 1].value;
      const update = function() {
        const time = input.get();
        if (Number.isFinite(time)) output.set(motion.reducedMotion() ? rest : sample(time));
      };
      record.stops.push(input.subscribe(update), motion.onReducedMotion(update));
    }
  });
  if (window.__gosx_register_bootstrap_feature) window.__gosx_register_bootstrap_feature("motion-track", () => ({}));
})();
