// Scene3D frame owner (docs/engine-frame-loop.md).
//
// An engine registers one frame hook per mount (handle.setFrameHook). The
// mount's own requestAnimationFrame calls it before the frame's render bundle
// is built, as hook(nowMS, deltaMS), and the hook returns the frame's
// interpolation alpha. hook(nowMS, 0, true) tells a hook that the mount let it
// go (a newer hook replaced it, or the mount was disposed). So a Go game steps
// its simulation and streams its poses inside the frame that draws them, and
// the page has one frame loop.
//
// The owner also retains the render bundle: a hook frame in which nothing
// scheduled a render, and the renderer, viewport and camera are unchanged,
// draws the last bundle again instead of building one. serial counts the
// mount's scheduleRender calls, which every scene change goes through (a static
// scene would otherwise never show the change either). arrays is the bundle
// build's typed-array retention object (see sceneRetentionSlot).
//
// The mount owns the owner record and calls these helpers. Functions read their
// arguments through arguments[n], as mount-scene-focus.ts does: the test
// harnesses load these sources as plain JavaScript, and the Scene3D
// noImplicitAny ratchet counts typed parameters.
function createSceneFrameOwner() {
  return {
    hook: null, lastMS: NaN, alpha: 0, running: false, serial: 0, builds: 0, reuses: 0, arrays: {},
    retained: { serial: -1, renderer: null, width: 0, height: 0, camera: null },
  };
}

// sceneFrameOwnerRun runs the hook for the frame at nowMS (fallbackMS when the
// frame has no timestamp). Delta is 0 on the first hook frame and after the
// mount could not render (the mount resets lastMS). It reports whether the
// hook ran.
function sceneFrameOwnerRun() {
  const owner = arguments[0], now = arguments[1];
  if (!owner.hook || owner.running) return false;
  const at = typeof now === "number" && Number.isFinite(now) ? now : arguments[2];
  const delta = Number.isFinite(owner.lastMS) ? Math.max(0, at - owner.lastMS) : 0;
  const hook = owner.hook;
  let alpha = 0;
  owner.lastMS = at;
  owner.running = true;
  try {
    alpha = hook(at, delta);
  } catch (error) {
    console.error("[gosx] scene3d frame hook failed", error);
  } finally {
    owner.running = false;
  }
  owner.alpha = typeof alpha === "number" && Number.isFinite(alpha) ? Math.max(0, Math.min(1, alpha)) : 0;
  return true;
}

// sceneFrameOwnerSet installs fn as the hook (null removes it) and returns a
// function that removes it. onChange(active) lets the mount start or park its
// frame loop. A replaced hook is told it was let go.
function sceneFrameOwnerSet() {
  const owner = arguments[0], fn = arguments[1], now = arguments[2], onChange = arguments[3];
  if (fn != null && typeof fn !== "function") throw new TypeError("frame hook must be a function");
  const previous = owner.hook;
  owner.hook = fn || null;
  owner.lastMS = NaN;
  owner.alpha = 0;
  if (previous && previous !== owner.hook) sceneFrameOwnerRelease(previous, now);
  onChange(Boolean(owner.hook));
  return function() {
    if (owner.hook !== fn) return;
    owner.hook = null;
    owner.lastMS = NaN;
    onChange(false);
  };
}

function sceneFrameOwnerRelease() {
  const hook = arguments[0];
  try { hook(arguments[1], 0, true); } catch (_) { /* the engine is gone */ }
}

// sceneFrameOwnerDispose lets the hook go when the mount is disposed.
function sceneFrameOwnerDispose() {
  const owner = arguments[0], hook = owner.hook;
  owner.hook = null;
  if (hook) sceneFrameOwnerRelease(hook, arguments[1]);
}

// sceneFrameOwnerCanReuse(owner, bundle, renderer, viewport, camera, moving)
// reports whether this frame may draw bundle again. moving() reports motion
// that runs outside scheduleRender; it is called only when everything else
// matches.
function sceneFrameOwnerCanReuse() {
  const owner = arguments[0], bundle = arguments[1], viewport = arguments[3], r = owner.retained;
  if (!owner.hook || !bundle || r.serial !== owner.serial || r.renderer !== arguments[2]) return false;
  if (r.width !== viewport.cssWidth || r.height !== viewport.cssHeight || !sceneFrameOwnerSameRecord(r.camera, arguments[4])) return false;
  if (arguments[5]()) return false;
  owner.reuses++;
  return true;
}

// sceneFrameOwnerRetain(owner, serial, renderer, viewport, camera) records the
// inputs of the bundle just built.
function sceneFrameOwnerRetain() {
  const owner = arguments[0], viewport = arguments[3];
  owner.builds++;
  owner.retained = { serial: arguments[1], renderer: arguments[2], width: viewport.cssWidth, height: viewport.cssHeight, camera: Object.assign({}, arguments[4]) };
}

// sceneFrameOwnerTelemetry returns the frameHook and bundle telemetry fields.
function sceneFrameOwnerTelemetry() {
  const owner = arguments[0];
  return { frameHook: owner.hook ? { alpha: owner.alpha } : null, bundle: { builds: owner.builds, reuses: owner.reuses } };
}

// Shallow equality of two plain records. A rebuilt nested value counts as a
// change, which costs one bundle build.
function sceneFrameOwnerSameRecord() {
  const a = arguments[0], b = arguments[1];
  if (a === b) return true;
  if (!a || !b || typeof a !== "object" || typeof b !== "object") return false;
  const keys = Object.keys(a);
  if (keys.length !== Object.keys(b).length) return false;
  for (let i = 0; i < keys.length; i++) {
    if (a[keys[i]] !== b[keys[i]]) return false;
  }
  return true;
}
