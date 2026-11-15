// GoSX Scene3D animation graph feature chunk.
//
// A page loads it by requiring the "scene3d-animation-graph" runtime feature
// (animgraph.Feature in Go). It evaluates GSA1 frames from motion/animgraph:
// layered clip sampling with bone masks and additive layers, humanoid rig
// mapping and clip retargeting, and IK. It installs
// window.__gosx.scene3d.defineAnimationRig, dispatchAnimationFrame and
// animationFeedback, and wraps two hooks of the animation chunk's API at run
// time, so no budgeted renderer chunk carries any of this code.

(function() {
  "use strict";
