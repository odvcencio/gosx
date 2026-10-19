// Shared by optional geometry features; compiling easing owns no clock.
(function() {
  "use strict";
  const motion = window.__gosx.motion;
  if (motion.createEasing) return;
  // The returned sampler owns no animation. Compile CSS easing once, rather
  // than constructing an effect or reading layout during every sampled frame.
  motion.createEasing = function(value) {
    if (typeof value !== "string") return function(t) { return motion.ease(t, value); };
    if (typeof KeyframeEffect !== "function" || typeof Animation !== "function") return function(t) { return t; };
    try {
      const effect = new KeyframeEffect(null, [{}], {duration: 1, fill: "both", easing: value});
      const animation = new Animation(effect);
      return function(t) { animation.currentTime = t; return effect.getComputedTiming().progress ?? t; };
    } catch (_error) { return function(t) { return t; }; }
  };
})();
