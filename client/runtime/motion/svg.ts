// Optional retained SVG geometry. No renderer, clock or application script.
(function() {
  "use strict";
  const motion = window.__gosx.motion;
  if (motion.createSVGMorph) return;
  const numeric = /[-+]?(?:\d*\.\d+|\d+\.?\d*)(?:e[-+]?\d+)?/gi;
  const geometry = ["x", "y", "x1", "y1", "x2", "y2", "cx", "cy", "r", "rx", "ry", "width", "height", "d", "points", "transform"];
  const shapes = "rect,circle,ellipse,line,path,polyline,polygon,use";

  function interpolate(from, to, key) {
    if (from == null || to == null || from.length > 65536 || to.length > 65536) return null;
    if (from.replace(numeric, "#") !== to.replace(numeric, "#")) return null;
    // Arc flags are discrete. Retain valid destination paths rather than emit
    // fractional flags; arbitrary topology changes use the same fade fallback.
    if (key === "d" && /[aA]/.test(to)) return null;
    const a = (from.match(numeric) || []).map(Number), b = (to.match(numeric) || []).map(Number);
    if (!a.length || a.length > 4096 || a.length !== b.length || a.some(n => !Number.isFinite(n)) || b.some(n => !Number.isFinite(n))) return null;
    const parts = to.split(numeric);
    return function(t) {
      if (t === 0) return from;
      if (t === 1) return to;
      let text = parts[0];
      for (let i = 0; i < a.length; i++) text += String(a[i] * (1 - t) + b[i] * t) + parts[i + 1];
      return text;
    };
  }

  function actors(svg) {
    const result = new Map();
    for (const el of svg.querySelectorAll("[data-morph-id]")) {
      const id = el.getAttribute("data-morph-id");
      // Ambiguous identities must not silently borrow a different actor's pose.
      result.set(id, result.has(id) ? null : el);
    }
    if (!result.size) result.set("", svg);
    return result;
  }

  function actorShapes(actor) {
    return [actor, ...actor.querySelectorAll(shapes)].filter(el => el === actor || el.closest("[data-morph-id]") === actor || actor.tagName.toLowerCase() === "svg" && !el.closest("[data-morph-id]"));
  }

  function actorTracks(from, to) {
    const a = actorShapes(from), b = actorShapes(to), tracks = [];
    if (a.length !== b.length || a.length > 4096) return null;
    for (let i = 0; i < b.length; i++) {
      if (a[i].tagName !== b[i].tagName) return null;
      for (const key of geometry) {
        const start = a[i].getAttribute(key), end = b[i].getAttribute(key);
        if (start === end) continue;
        const value = interpolate(start, end, key);
        if (!value) return null;
        tracks.push({el: b[i], key: key, value: value, final: end});
      }
    }
    return tracks;
  }

  function createSVGMorph(from, to, options) {
    if (from === to || [from, to].some(el => !el || el.namespaceURI !== "http://www.w3.org/2000/svg" || el.tagName.toLowerCase() !== "svg")) throw new TypeError("SVG morph requires distinct SVG snapshots");
    const source = actors(from), tracks = [], fades = [];
    for (const [id, el] of actors(to)) {
      if (!el) continue;
      const previous = source.get(id), matched = previous && actorTracks(previous, el);
      if (matched) tracks.push(...matched);
      else {
        const opacity = el.style.getPropertyValue("opacity"), priority = el.style.getPropertyPriority("opacity");
        const base = typeof getComputedStyle === "function" ? Number(getComputedStyle(el).opacity) : Number(opacity || 1);
        fades.push({el: el, opacity: opacity, priority: priority, base: Number.isFinite(base) ? base : 1});
      }
    }
    const ease = motion.createEasing(options && options.easing);
    let disposed = false, progress = 1;
    function restore() {
      for (const track of tracks) track.el.setAttribute(track.key, track.final);
      for (const fade of fades) {
        if (fade.opacity) fade.el.style.setProperty("opacity", fade.opacity, fade.priority);
        else fade.el.style.removeProperty("opacity");
      }
    }
    function sample(value) {
      if (disposed) return;
      if (!Number.isFinite(value)) throw new RangeError("SVG morph progress must be finite");
      progress = Math.max(0, Math.min(1, value));
      if (motion.reducedMotion() || progress === 1) { restore(); return; }
      const t = progress === 0 ? 0 : ease(progress);
      for (const track of tracks) track.el.setAttribute(track.key, track.value(t));
      for (const fade of fades) fade.el.style.setProperty("opacity", String(fade.base * Math.max(0, Math.min(1, t))));
    }
    const stopReduced = motion.onReducedMotion(function() { sample(progress); });
    return {sample: sample, dispose: function() { if (disposed) return; disposed = true; stopReduced(); restore(); }};
  }

  motion.createSVGMorph = createSVGMorph;
  motion.registerProgramFeature(function(record, spec) {
    for (const binding of (Array.isArray(spec.svgMorphs) ? spec.svgMorphs : []).slice(0, 128)) {
      if (!binding) continue;
      const signal = record.signals.get(binding.signal);
      if (!signal) continue;
      let transition;
      try { transition = createSVGMorph(record.root.querySelector(binding.from), record.root.querySelector(binding.to)); } catch (_error) { continue; }
      let latest = signal.get(), active = true;
      const apply = function(value) { latest = value; motion.scheduler.queueWrite(transition, function() { if (active && Number.isFinite(latest)) transition.sample(latest); }); };
      const stop = signal.subscribe(apply);
      record.stops.push(function() { active = false; stop(); transition.dispose(); });
    }
  });
  if (window.__gosx_register_bootstrap_feature) window.__gosx_register_bootstrap_feature("motion-svg", () => ({}));
})();
