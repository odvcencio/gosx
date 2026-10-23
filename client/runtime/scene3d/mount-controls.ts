// mount-controls.ts — the built-in camera controls: orbit, fly, pointer lock and inertia.
// @ts-check
//
// setupSceneBuiltInControls attaches the pointer, wheel and key listeners and
// returns a handle. The handle is the ONLY way outside code reaches this
// state: an earlier version let the managed control-forms camera callback read
// flyMode, controls and cancelOrbitInertia directly, which threw
// ReferenceError because those are locals of setupSceneBuiltInControls.
//
// This file is a gate candidate: a scene with Controls "none" runs none of it.

/**
 * @typedef {object} GoSXSceneControlState
 * @property {string} mode
 * @property {boolean} active
 * @property {() => void} dispose
 */
  function normalizeSceneControlsMode(value) {
    switch (String(value || "").trim().toLowerCase()) {
      case "orbit":
        return "orbit";
      case "first-person":
      case "firstperson":
      case "fps":
        return "first-person";
      case "fly":
      case "free":
      case "free-camera":
        return "fly";
      default:
        return "";
    }
  }

  // @ts-ignore TS7006 -- this fragment also runs as plain JavaScript in source fixtures
  function sceneWalkEnabled(props) {
    return props && normalizeSceneControlsMode(props.controls) === "first-person" && props.walk && typeof props.walk === "object" && !Array.isArray(props.walk);
  }

  // @ts-ignore TS7006 -- prop gate is also evaluated by plain JS tests
  function sceneVesselEnabled(props) {
    return props && props.vessel && typeof props.vessel === "object" && typeof props.vessel.nodeId === "string" && props.vessel.nodeId.length > 0;
  }

  // @ts-ignore TS7006 -- keep this helper JavaScript-compatible in the runtime bundle
  function sceneRunFrameGuard(callback, args, shouldRecover, recover) {
    try { return callback.apply(null, args); } catch (error) { if (shouldRecover()) recover(); throw error; }
  }

  // @ts-ignore TS7006 -- keep this helper JavaScript-compatible in the runtime bundle
  function sceneControlController(handle) {
    return handle && handle.controller;
  }

  // @ts-ignore TS7006 -- keep this helper JavaScript-compatible in the runtime bundle
  function applySceneMotionBindings(bundle, sceneState, controlHandle) {
    applyMotionBindingsToRuntimeBundle.call(null, bundle, sceneState, sceneControlController(controlHandle));
    return bundle;
  }

  function motionSceneCameraProperty() {
    const property = arguments[0];
    const parts = String(property || "").split("."), group = parts.length > 1 ? parts[0] : "", field = parts[parts.length - 1];
    if (group === "rotation" && ["x", "y", "z"].includes(field)) return "rotation" + field.toUpperCase();
    return ["x", "y", "z", "fov", "near", "far", "zoom", "rotationX", "rotationY", "rotationZ"].includes(field) ? field : "";
  }

  function sceneMotionObjects() {
    const motion = window.__gosx && window.__gosx.motion;
    return motion && motion._sceneObjects ? motion._sceneObjects(arguments[0], arguments[1]) : arguments[0];
  }

  function applyMotionBindingsToRuntimeBundle() {
    const bundle = arguments[0], sceneState = arguments[1], controls = arguments[2];
    const layers = sceneState && sceneState._gosxMotionRuntimeBindings;
    if (!bundle || !layers || typeof layers.values !== "function") return;
    const materials = new Map();
    let cameraOverride = false;
    for (const layer of layers.values()) {
      for (const item of layer.values()) {
        if (item.binding.target === "camera") {
          const field = motionSceneCameraProperty.call(null, item.binding.property);
          if (field) { if (!bundle.camera) bundle.camera = {}; bundle.camera[field] = item.value; cameraOverride = true; }
        } else {
          const apply = window.__gosx.motion._sceneBinding;
          if (apply) apply(bundle, item.binding, item.value, materials);
          // Finite timelines use validated canonical properties and can run
          // without the optional scalar-track adapters.
          else if (item.binding.target === "sceneNode") for (const object of bundle.meshObjects || []) {
            if (object.id === item.binding.node) object[item.binding.property] = item.value;
          }
        }
      }
    }
    if (cameraOverride) applySceneControlsCamera(controls, bundle.camera);
  }

  function attachSceneMotionBridge() {
    const mount = arguments[0], sceneState = arguments[1], scheduleRender = arguments[2];
    const motion = window.__gosx && window.__gosx.motion;
    if (!motion || typeof motion.attachScene !== "function") return function() {};
    return motion.attachScene(mount, {
      write: function() {
        const binding = arguments[0], value = arguments[1], programToken = arguments[2] || "motion";
        /** @type {Map<object|string, Map<string, any>>} */
        const layers = sceneState._gosxMotionRuntimeBindings || (sceneState._gosxMotionRuntimeBindings = new Map());
        let entries = layers.get(programToken);
        if (!entries) { entries = new Map(); layers.set(programToken, entries); }
        if ((binding.target === "sceneNode" || binding.target === "camera") && ["position", "scale", "rotation"].includes(binding.property)) {
          if (!Array.isArray(value) || value.length !== (binding.property === "rotation" ? 4 : 3) || value.some(component => !Number.isFinite(component))) return;
          let components = value;
          if (binding.property === "rotation") {
            const euler = sceneQuatToEulerXYZ(value[0], value[1], value[2], value[3]);
            components = [euler.x, euler.y, euler.z];
          }
          for (let i = 0; i < 3; i++) {
            const property = binding.property + "." + ["x", "y", "z"][i];
            const key = [binding.target, binding.node || "", property].join("|");
            const entry = entries.get(key);
            if (entry) entry.value = components[i];
            else entries.set(key, {key, binding: Object.assign({}, binding, {property}), value: components[i]});
          }
          return;
        }
        const key = [binding.target, binding.node || "", binding.property || ""].join("|");
        entries.set(key, { key: key, binding: Object.assign({}, binding), value: value });
      },
      pin: function() {
        const pin = arguments[0], elementRect = arguments[1], sceneRect = arguments[2], programToken = arguments[3] || "motion";
        if (!(sceneRect.width > 0) || !(sceneRect.height > 0)) return false;
        const camera = sceneState.camera || {}, distance = Math.max(0.01, Math.abs(sceneNumber(camera.z, 6)));
        const worldHeight = 2 * distance * Math.tan(sceneNumber(camera.fov, 75) * Math.PI / 360);
        const x = sceneNumber(camera.x, 0) + (elementRect.left + elementRect.width * 0.5 - sceneRect.left - sceneRect.width * 0.5) * worldHeight / sceneRect.height;
        const y = sceneNumber(camera.y, 0) + (sceneRect.top + sceneRect.height * 0.5 - elementRect.top - elementRect.height * 0.5) * worldHeight / sceneRect.height;
        const layers = sceneState._gosxMotionRuntimeBindings || (sceneState._gosxMotionRuntimeBindings = new Map());
        let entries = layers.get(programToken);
        if (!entries) { entries = new Map(); layers.set(programToken, entries); }
        const node = String(pin.node || "");
        const xKey = ["sceneNode", node, "position.x"].join("|");
        const yKey = ["sceneNode", node, "position.y"].join("|");
        const oldX = entries.get(xKey), oldY = entries.get(yKey);
        const changed = !oldX || !oldY || !Object.is(oldX.value, x) || !Object.is(oldY.value, y);
        entries.set(xKey, { key: xKey, binding: { target: "sceneNode", node: node, property: "position.x" }, value: x });
        entries.set(yKey, { key: yKey, binding: { target: "sceneNode", node: node, property: "position.y" }, value: y });
        return changed;
      },
      disposeProgram: function() {
        const layers = sceneState._gosxMotionRuntimeBindings;
        if (!layers || typeof layers.delete !== "function" || !layers.delete(arguments[0] || "motion")) return;
        const baseCamera = sceneState.camera;
        let camera = baseCamera && typeof baseCamera === "object" ? Object.assign({}, baseCamera) : {};
        let hasCameraOverride = false;
        for (const layer of layers.values()) for (const item of layer.values()) if (item.binding.target === "camera") {
          const field = motionSceneCameraProperty.call(null, item.binding.property);
          if (field) {
            if (!camera || typeof camera !== "object") camera = {};
            camera[field] = item.value;
            hasCameraOverride = true;
          }
        }
        if (!hasCameraOverride) camera = baseCamera;
        applySceneControlsCamera(sceneState._gosxMotionController, camera);
        scheduleRender("motion-dispose");
      },
      invalidate: function() { scheduleRender("motion-binding"); },
    });
  }

  function sceneMotionRequestFrame() {
    const callback = arguments[0];
    const scheduler = window.__gosx && window.__gosx.motion && window.__gosx.motion.scheduler;
    if (scheduler && typeof scheduler.request === "function") return scheduler.request(callback);
    if (typeof window.requestAnimationFrame === "function") return window.requestAnimationFrame(callback);
    return setTimeout(function() { callback(Date.now()); }, 16);
  }

  function sceneMotionCancelFrame() {
    const handle = arguments[0];
    const scheduler = window.__gosx && window.__gosx.motion && window.__gosx.motion.scheduler;
    if (scheduler && typeof scheduler.cancel === "function") scheduler.cancel(handle);
    else if (typeof window.cancelAnimationFrame === "function") window.cancelAnimationFrame(handle);
    else clearTimeout(handle);
  }

  function sceneControlsTarget(props) {
    const raw = props && props.controlTarget && typeof props.controlTarget === "object" ? props.controlTarget : null;
    return {
      x: sceneNumber(raw && raw.x, 0),
      y: sceneNumber(raw && raw.y, 0),
      z: sceneNumber(raw && raw.z, 0),
    };
  }

  // sceneControlsApplyOrbit changes the orbit target and its distance and
  // pitch limits after mount. orbit is {target?, minDistance?, maxDistance?,
  // pitchLimit?}; a field left out keeps its current value. It reports whether
  // anything changed. The caller then re-derives the orbit state from the
  // camera (applySceneControlsCamera), so the orbit continues from the new
  // target without a jump. A limit that is not a positive number keeps its
  // value. Controllers with no orbit (walk, vessel, fly) ignore it.
  // @ts-ignore TS7006 -- this fragment also runs as plain JavaScript in source fixtures
  function sceneControlsApplyOrbit(controls, orbit) {
    // A vessel scene wraps the orbit controller in its own, which forwards to the one inside.
    if (controls && typeof controls.applyOrbit === "function") return controls.applyOrbit(orbit);
    if (!controls || (controls.mode !== "orbit" && controls.mode !== "fixed") || !sceneIsPlainObject(orbit)) {
      return false;
    }
    let changed = false;
    if (sceneIsPlainObject(orbit.target)) {
      const current = controls.target || { x: 0, y: 0, z: 0 };
      const next = {
        x: sceneNumber(orbit.target.x, current.x),
        y: sceneNumber(orbit.target.y, current.y),
        z: sceneNumber(orbit.target.z, current.z),
      };
      changed = next.x !== current.x || next.y !== current.y || next.z !== current.z;
      controls.target = next;
    }
    if (controls.mode === "orbit") {
      // @ts-ignore TS7006 -- plain JavaScript fixture
      const finite = (value) => typeof value === "number" && Number.isFinite(value) && value > 0; // zero leaves a limit as it is
      const minDistance = finite(orbit.minDistance) ? Math.max(0.001, orbit.minDistance) : controls.minDistance;
      const maxDistance = Math.max(minDistance, finite(orbit.maxDistance) ? orbit.maxDistance : controls.maxDistance);
      const pitchLimit = finite(orbit.pitchLimit) ? sceneOrbitPitchLimit(orbit.pitchLimit) : controls.pitchLimit;
      // @ts-ignore TS7006 -- plain JavaScript fixture
      const number = (value, current) => typeof value === "number" && Number.isFinite(value) ? value : current;
      const minPitch = number(orbit.minPitch, controls.minPitch), maxPitch = number(orbit.maxPitch, controls.maxPitch), minHeight = number(orbit.minHeight, controls.minHeight);
      changed = changed || minDistance !== controls.minDistance || maxDistance !== controls.maxDistance || pitchLimit !== controls.pitchLimit ||
        minPitch !== controls.minPitch || maxPitch !== controls.maxPitch || minHeight !== controls.minHeight;
      controls.minPitch = minPitch;
      controls.maxPitch = maxPitch;
      controls.minHeight = minHeight;
      controls.minDistance = minDistance;
      controls.maxDistance = maxDistance;
      controls.pitchLimit = pitchLimit;
    }
    return changed;
  }

  // sceneControlsFollowCameraCommands runs after SetCamera commands wrote the
  // scene camera (applySceneCommands writes only the scene state). Built-in
  // controls keep their own orbit or fly state, so the last camera command,
  // with its orbit settings, goes through them as handle.setCamera does. It
  // reports whether a camera command was present.
  // @ts-ignore TS7006 -- plain JavaScript fixture
  function sceneControlsFollowCameraCommands(controls, sceneState, commands) {
    let hasCamera = false;
    for (const command of commands) {
      if (!command || command.kind !== 5) continue;
      hasCamera = true;
      sceneControlsApplyOrbit(controls, command.data && command.data.orbit);
    }
    if (!hasCamera) return false;
    applySceneControlsCamera(controls, sceneState.camera);
    return true;
  }

  // The orbit target now in effect: the controller's, which a camera command or
  // pan may have moved since mount, else the mount-time prop.
  // @ts-ignore TS7006 -- plain JavaScript fixture
  function sceneControlsCurrentTarget(controls, props) {
    return controls && controls.target ? Object.assign({}, controls.target) : sceneControlsTarget(props);
  }

  function sceneControlsRotateSpeed(props) {
    return Math.max(0.1, sceneNumber(props && props.controlRotateSpeed, 1));
  }

  function sceneControlsZoomSpeed(props) {
    return Math.max(0.05, sceneNumber(props && props.controlZoomSpeed, 1));
  }

  function sceneControlsLookSpeed(props) {
    return Math.max(0.05, sceneNumber(props && props.controlLookSpeed, sceneNumber(props && props.controlRotateSpeed, 1)));
  }

  function sceneControlsMoveSpeed(props) {
    return Math.max(0.01, sceneNumber(props && props.controlMoveSpeed, 4));
  }

  const SCENE_ORBIT_MAX_SPEED = Math.PI * 6;
  const SCENE_ORBIT_DAMPING = 6;
  const SCENE_ORBIT_STOP_SPEED = (Math.PI / 180) * 0.01;
  const SCENE_ORBIT_DEFAULT_PITCH_LIMIT = 1.4;
  const SCENE_ORBIT_MAX_PITCH_LIMIT = Math.PI / 2 - (Math.PI / 180) * 0.001;
  const SCENE_ORBIT_MAX_PITCH_SINE = Math.sin(SCENE_ORBIT_MAX_PITCH_LIMIT);
  const SCENE_ORBIT_DEFAULT_MIN_DISTANCE = 0.6;
  const SCENE_ORBIT_DEFAULT_MAX_DISTANCE = 256;

  function sceneOrbitPitchLimit(value) {
    return sceneClamp(
      sceneNumber(value, SCENE_ORBIT_DEFAULT_PITCH_LIMIT),
      0.1,
      SCENE_ORBIT_MAX_PITCH_LIMIT,
    );
  }

  // A floor above the target needs enough radius to reach it without a pole
  // singularity. The floor wins when authored distance/pitch bounds conflict.
  // @ts-ignore TS7006 -- this fragment also runs as plain JavaScript in source fixtures
  function sceneOrbitClampRadius(limits, target, radius) {
    let min = Math.max(0.001, sceneNumber(limits.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE));
    if (Number.isFinite(limits.minHeight)) {
      const height = limits.minHeight - sceneNumber(target && target.y, 0);
      min = Math.max(min, height / SCENE_ORBIT_MAX_PITCH_SINE);
    }
    return sceneClamp(radius, min, Math.max(min, sceneNumber(limits.maxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE)));
  }

  // The pitch an orbit may take at its radius. PitchLimit is the symmetric default; MinPitch and MaxPitch
  // (either may be zero or negative) replace its two sides; MinHeight raises the lower side so the camera
  // stays at or above that world height. limits is the controls or an orbit state.
  // @ts-ignore TS7006 -- this fragment also runs as plain JavaScript in source fixtures
  function sceneOrbitClampPitch(limits, orbit, pitch) {
    const limit = sceneOrbitPitchLimit(limits.pitchLimit), cap = SCENE_ORBIT_MAX_PITCH_LIMIT;
    let min = sceneClamp(sceneNumber(limits.minPitch, -limit), -cap, cap);
    const max = sceneClamp(sceneNumber(limits.maxPitch, limit), -cap, cap);
    if (Number.isFinite(limits.minHeight)) {
      const ratio = (limits.minHeight - sceneNumber(orbit.target && orbit.target.y, 0)) / Math.max(sceneNumber(orbit.radius, 1), 0.001);
      min = Math.max(min, Math.asin(sceneClamp(ratio, -1, 1)));
    }
    return sceneClamp(pitch, min, Math.max(min, max));
  }

  function sceneControlsRotateMode(props) {
    const mode = String(props && props.controlRotateMode || "").trim().toLowerCase();
    if (mode === "pixel-degrees" || mode === "pixel-degree" || mode === "pixels-degrees") {
      return "pixel-degrees";
    }
    return "viewport";
  }

  function sceneControlsRotateDirection(props) {
    const direction = String(props && props.controlRotateDirection || "").trim().toLowerCase();
    switch (direction) {
      case "grab":
      case "track":
      case "direct":
        return -1;
      default:
        return 1;
    }
  }

  function sceneControlsPitchLimit(props) {
    return sceneOrbitPitchLimit(props && props.controlPitchLimit);
  }

  function sceneControlsMinDistance(props) {
    return Math.max(0.001, sceneNumber(props && props.controlMinDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE));
  }

  function sceneControlsMaxDistance(props, minDistance) {
    return Math.max(
      Math.max(0.001, sceneNumber(minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE)),
      sceneNumber(props && props.controlMaxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE),
    );
  }

  function scenePointerLockRequested(props) {
    return sceneBool(props && props.pointerLock, false);
  }

  function scenePointerLockElement() {
    if (typeof document === "undefined" || !document) {
      return null;
    }
    return document.pointerLockElement ||
      document.mozPointerLockElement ||
      document.webkitPointerLockElement ||
      null;
  }

  function scenePointerLockActive(canvas) {
    return Boolean(canvas && scenePointerLockElement() === canvas);
  }

  function sceneRequestPointerLock(canvas) {
    if (!canvas || typeof canvas.requestPointerLock !== "function") {
      return false;
    }
    const fallback = () => {
      try {
        const request = canvas.requestPointerLock();
        if (request) Promise.resolve(request).catch(() => {});
        return true;
      } catch (_error) { return false; }
    };
    try {
      const request = canvas.requestPointerLock({ unadjustedMovement: true });
      // Modern browsers reject unsupported raw input asynchronously. The
      // synchronous catch below only covers the older, void-returning API.
      if (request) Promise.resolve(request).catch((error) => {
        if (error && error.name === "NotSupportedError") fallback();
      });
      return true;
    } catch (_unadjustedError) {
      return fallback();
    }
  }

  function sceneExitPointerLock(canvas) {
    if (!scenePointerLockActive(canvas)) {
      return;
    }
    const exit = document.exitPointerLock || document.mozExitPointerLock || document.webkitExitPointerLock;
    if (typeof exit !== "function") {
      return;
    }
    try {
      exit.call(document);
    } catch (_error) {}
  }

  function sceneWorldCameraPosition(camera) {
    const normalized = sceneRenderCamera(camera);
    return {
      x: normalized.x,
      y: normalized.y,
      z: normalized.z,
    };
  }

  function sceneFlyStateFromCamera(camera) {
    const normalized = sceneRenderCamera(camera);
    return {
      position: sceneWorldCameraPosition(normalized),
      yaw: sceneNumber(normalized.rotationY, 0),
      pitch: sceneClamp(sceneNumber(normalized.rotationX, 0), -1.52, 1.52),
      kind: normalized.kind,
      fov: normalized.fov,
      left: normalized.left,
      right: normalized.right,
      top: normalized.top,
      bottom: normalized.bottom,
      zoom: normalized.zoom,
      near: normalized.near,
      far: normalized.far,
    };
  }

  function sceneFlyCamera(state, fallbackCamera) {
    const base = sceneRenderCamera(fallbackCamera);
    const fly = state || sceneFlyStateFromCamera(base);
    const position = fly.position || { x: 0, y: 0, z: 6 };
    return {
      x: sceneNumber(position.x, base.x),
      y: sceneNumber(position.y, base.y),
      z: sceneNumber(position.z, base.z),
      kind: base.kind,
      rotationX: sceneClamp(sceneNumber(fly.pitch, base.rotationX), -1.52, 1.52),
      rotationY: sceneNumber(fly.yaw, base.rotationY),
      rotationZ: 0,
      fov: sceneNumber(fly.fov, base.fov),
      left: sceneNumber(fly.left, base.left),
      right: sceneNumber(fly.right, base.right),
      top: sceneNumber(fly.top, base.top),
      bottom: sceneNumber(fly.bottom, base.bottom),
      zoom: sceneNumber(fly.zoom, base.zoom),
      near: sceneNumber(fly.near, base.near),
      far: sceneNumber(fly.far, base.far),
    };
  }

  function sceneOrbitStateFromCamera(camera, target, controls) {
    const normalized = sceneRenderCamera(camera);
    const worldPosition = sceneWorldCameraPosition(normalized);
    const orbitTarget = target || { x: 0, y: 0, z: 0 };
    const offsetX = worldPosition.x - sceneNumber(orbitTarget.x, 0);
    const offsetY = worldPosition.y - sceneNumber(orbitTarget.y, 0);
    const offsetZ = worldPosition.z - sceneNumber(orbitTarget.z, 0);
    const minDistance = Math.max(0.001, sceneNumber(controls && controls.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE));
    const maxDistance = Math.max(minDistance, sceneNumber(controls && controls.maxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE));
    const radius = sceneOrbitClampRadius(controls || {}, orbitTarget, Math.hypot(offsetX, offsetY, offsetZ));
    const limits = controls || {};
    const state = {
      target: {
        x: sceneNumber(orbitTarget.x, 0),
        y: sceneNumber(orbitTarget.y, 0),
        z: sceneNumber(orbitTarget.z, 0),
      },
      radius,
      minDistance,
      maxDistance,
      pitchLimit: sceneOrbitPitchLimit(limits.pitchLimit),
      minPitch: limits.minPitch,
      maxPitch: limits.maxPitch,
      minHeight: limits.minHeight,
      yaw: Math.atan2(offsetX, offsetZ),
      pitch: Math.asin(sceneClamp(offsetY / Math.max(radius, 0.001), -1, 1)),
      kind: normalized.kind,
      fov: normalized.fov,
      left: normalized.left,
      right: normalized.right,
      top: normalized.top,
      bottom: normalized.bottom,
      zoom: normalized.zoom,
      near: normalized.near,
      far: normalized.far,
    };
    state.pitch = sceneOrbitClampPitch(state, state, state.pitch);
    return state;
  }

  function sceneOrbitCamera(state, fallbackCamera) {
    /* @ts-expect-error TS2554 -- this call omits trailing arguments the JS caller has always been able to omit */ const base = sceneRenderCamera(fallbackCamera);
    const orbit = state || sceneOrbitStateFromCamera(base, { x: 0, y: 0, z: 0 });
    const radius = sceneOrbitClampRadius(orbit, orbit.target, sceneNumber(orbit.radius, 6));
    const pitch = sceneOrbitClampPitch(orbit, Object.assign({}, orbit, { radius }), sceneNumber(orbit.pitch, 0));
    const yaw = sceneNumber(orbit.yaw, 0);
    const target = orbit.target || { x: 0, y: 0, z: 0 };
    const cosPitch = Math.cos(pitch);
    const worldPosition = {
      x: sceneNumber(target.x, 0) + Math.sin(yaw) * cosPitch * radius,
      y: sceneNumber(target.y, 0) + Math.sin(pitch) * radius,
      z: sceneNumber(target.z, 0) + Math.cos(yaw) * cosPitch * radius,
    };
    const forward = {
      x: sceneNumber(target.x, 0) - worldPosition.x,
      y: sceneNumber(target.y, 0) - worldPosition.y,
      z: sceneNumber(target.z, 0) - worldPosition.z,
    };
    const horizontal = Math.max(0.0001, Math.hypot(forward.x, forward.z));
    return {
      x: worldPosition.x,
      y: worldPosition.y,
      z: worldPosition.z,
      kind: base.kind,
      rotationX: Math.atan2(forward.y, horizontal),
      rotationY: Math.atan2(-forward.x, -forward.z),
      rotationZ: 0,
      fov: sceneNumber(orbit.fov, base.fov),
      left: sceneNumber(orbit.left, base.left),
      right: sceneNumber(orbit.right, base.right),
      top: sceneNumber(orbit.top, base.top),
      bottom: sceneNumber(orbit.bottom, base.bottom),
      zoom: sceneNumber(orbit.zoom, base.zoom),
      near: sceneNumber(orbit.near, base.near),
      far: sceneNumber(orbit.far, base.far),
    };
  }

  function createSceneControls(props) {
    const mode = normalizeSceneControlsMode(props && props.controls);
    if (!mode) {
      return null;
    }
    const minDistance = sceneControlsMinDistance(props);
    const maxDistance = sceneControlsMaxDistance(props, minDistance);
    return {
      mode,
      active: false,
      touched: false,
      pointerId: null,
      lastX: 0,
      lastY: 0,
      rotateMode: sceneControlsRotateMode(props),
      rotateDirection: sceneControlsRotateDirection(props),
      rotateSpeed: sceneControlsRotateSpeed(props),
      zoomSpeed: sceneControlsZoomSpeed(props),
      lookSpeed: sceneControlsLookSpeed(props),
      moveSpeed: sceneControlsMoveSpeed(props),
      minDistance,
      maxDistance,
      pitchLimit: sceneControlsPitchLimit(props),
      pointerLock: mode !== "orbit" && scenePointerLockRequested(props),
      pointerLocked: false,
      orbit: null,
      orbitVelocityYaw: 0,
      orbitVelocityPitch: 0,
      orbitLastMoveMS: 0,
      fly: null,
      keys: new Set(),
      target: sceneControlsTarget(props),
    };
  }

  function sceneOrbitStopInertia(controls) {
    if (!controls) return;
    controls.orbitVelocityYaw = 0;
    controls.orbitVelocityPitch = 0;
  }

  function sceneOrbitInertiaActive(controls) {
    if (!controls || controls.active) return false;
    return Math.abs(sceneNumber(controls.orbitVelocityYaw, 0)) > SCENE_ORBIT_STOP_SPEED ||
      Math.abs(sceneNumber(controls.orbitVelocityPitch, 0)) > SCENE_ORBIT_STOP_SPEED;
  }

  function sceneOrbitApplyInertia(controls, readSourceCamera, deltaSeconds) {
    if (!sceneOrbitInertiaActive(controls)) return false;
    if (!controls.orbit) {
      syncSceneControlsFromSource(controls, readSourceCamera);
    }
    if (!controls.orbit) return false;
    const seconds = Math.max(0.001, Math.min(0.05, sceneNumber(deltaSeconds, 0)));
    controls.orbit.yaw += sceneNumber(controls.orbitVelocityYaw, 0) * seconds;
    const wanted = controls.orbit.pitch + sceneNumber(controls.orbitVelocityPitch, 0) * seconds;
    const nextPitch = sceneOrbitClampPitch(controls, controls.orbit, wanted);
    if (nextPitch !== wanted) {
      controls.orbitVelocityPitch = 0;
    }
    controls.orbit.pitch = nextPitch;
    const damping = Math.exp(-SCENE_ORBIT_DAMPING * seconds);
    controls.orbitVelocityYaw *= damping;
    controls.orbitVelocityPitch *= damping;
    if (Math.abs(controls.orbitVelocityYaw) <= SCENE_ORBIT_STOP_SPEED) controls.orbitVelocityYaw = 0;
    if (Math.abs(controls.orbitVelocityPitch) <= SCENE_ORBIT_STOP_SPEED) controls.orbitVelocityPitch = 0;
    controls.touched = true;
    return true;
  }

  function syncSceneControlsFromCamera(controls, camera) {
    if (controls && controls.syncCamera) { controls.syncCamera(camera); return; }
    if (!controls || controls.active || controls.touched) {
      return;
    }
    if (controls.mode === "orbit") {
      controls.orbit = sceneOrbitStateFromCamera(camera, controls.target, controls);
    } else if (controls.mode === "first-person" || controls.mode === "fly") {
      controls.fly = sceneFlyStateFromCamera(camera);
    }
  }

  function applySceneControlsCamera(controls, camera) {
    if (controls && controls.applyCamera) { controls.applyCamera(camera); return; }
    if (!controls) {
      return;
    }
    controls.active = false;
    controls.touched = true;
    controls.pointerId = null;
    sceneOrbitStopInertia(controls);
    if (controls.keys && typeof controls.keys.clear === "function") {
      controls.keys.clear();
    }
    if (controls.mode === "orbit") {
      controls.orbit = sceneOrbitStateFromCamera(camera, controls.target, controls);
    } else if (controls.mode === "first-person" || controls.mode === "fly") {
      controls.fly = sceneFlyStateFromCamera(camera);
    }
  }

  function sceneScrollViewportHeight() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const visualViewport = window.visualViewport;
    return Math.max(1, sceneNumber(
      visualViewport && visualViewport.height,
      sceneNumber(window.innerHeight, sceneNumber(scrollingElement && scrollingElement.clientHeight, 0)),
    ));
  }

  function sceneScrollTop() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const visualViewport = window.visualViewport;
    const visualViewportTop = sceneNumber(visualViewport && visualViewport.pageTop, NaN);
    if (Number.isFinite(visualViewportTop)) {
      return Math.max(0, visualViewportTop);
    }
    return Math.max(0, sceneNumber(
      window.scrollY,
      sceneNumber(window.pageYOffset, sceneNumber(scrollingElement && scrollingElement.scrollTop, 0)),
    ));
  }

  function sceneScrollMax() {
    const scrollingElement = document.scrollingElement || document.documentElement || document.body;
    const scrollHeight = Math.max(
      sceneNumber(scrollingElement && scrollingElement.scrollHeight, 0),
      sceneNumber(document.documentElement && document.documentElement.scrollHeight, 0),
      sceneNumber(document.body && document.body.scrollHeight, 0),
    );
    return Math.max(1, scrollHeight - sceneScrollViewportHeight());
  }

  function sceneUpdateScrollCameraMetrics(scrollCamera, includeMax, activeInput) {
    if (!scrollCamera) {
      return;
    }
    scrollCamera._scrollTop = sceneScrollTop();
    if (includeMax || !Number.isFinite(sceneNumber(scrollCamera._scrollMax, NaN))) {
      scrollCamera._scrollMax = sceneScrollMax();
    }
    if (activeInput) {
      scrollCamera._activeInputUntil = sceneNowMilliseconds() + 180;
    }
  }

  function sceneAdvanceScrollCamera(scrollCamera) {
    if (!scrollCamera) {
      return;
    }
    const scrollTop = sceneNumber(scrollCamera._scrollTop, 0);
    const scrollMax = Math.max(1, sceneNumber(scrollCamera._scrollMax, 1));
    const usesRange = scrollCamera.start !== scrollCamera.end;
    scrollCamera._progress = usesRange ? Math.pow(Math.min(1, Math.max(0, scrollTop / scrollMax)), 0.5) : 0;
    var target = usesRange ? (scrollCamera._progress || 0) : 0;
    var current = sceneNumber(scrollCamera._smoothProgress, target);
    if (sceneNumber(scrollCamera._activeInputUntil, 0) >= sceneNowMilliseconds()) {
      current = target;
    } else if (Math.abs(target - current) < 0.0005) {
      current = target;
    } else {
      current += (target - current) * 0.08;
    }
    scrollCamera._smoothProgress = current;
  }

  // A normalized, renderer-owned signal for materials that need to react to
  // the scroll camera entering the scene. This intentionally follows the
  // smoothed progress rather than raw scrollTop, so every backend receives the
  // same calm value and material activity cannot jump ahead of the camera.
  // A camera without a declared scroll range has no proximity signal.
  function sceneCameraProximityValue(scrollCamera) {
    if (!scrollCamera || scrollCamera.start === scrollCamera.end) {
      return 0;
    }
    const progress = sceneNumber(
      scrollCamera._smoothProgress,
      sceneNumber(scrollCamera._progress, 0),
    );
    return Math.max(0, Math.min(1, progress));
  }

  function sceneCurrentControlCamera(controls, sourceCamera, scrollCamera) {
    var cam;
    if (controls && controls.currentCamera) {
      controls.syncCamera(sourceCamera);
      return controls.currentCamera();
    } else if (controls && controls.mode === "orbit") {
      syncSceneControlsFromCamera(controls, sourceCamera);
      cam = controls.orbit ? sceneOrbitCamera(controls.orbit, sourceCamera) : sceneRenderCamera(sourceCamera);
    } else if (controls && (controls.mode === "first-person" || controls.mode === "fly")) {
      syncSceneControlsFromCamera(controls, sourceCamera);
      cam = controls.fly ? sceneFlyCamera(controls.fly, sourceCamera) : sceneRenderCamera(sourceCamera);
    } else {
      cam = sceneRenderCamera(sourceCamera);
    }
    if (scrollCamera && scrollCamera.start !== scrollCamera.end) {
      var progress = sceneNumber(scrollCamera._smoothProgress, sceneNumber(scrollCamera._progress, 0));
      cam.z = scrollCamera.start + progress * (scrollCamera.end - scrollCamera.start);
    }
    if (scrollCamera && scrollCamera.offset) {
      var scrollTop = sceneNumber(scrollCamera._scrollTop, 0);
      cam.x += scrollTop * sceneNumber(scrollCamera.offset.x, 0);
      cam.y += scrollTop * sceneNumber(scrollCamera.offset.y, 0);
      cam.z += scrollTop * sceneNumber(scrollCamera.offset.z, 0);
    }
    return cam;
  }

  function sceneBundleWithCameraOverride(bundle, camera) {
    if (!bundle) {
      return bundle;
    }
    const targetCamera = sceneRenderCamera(camera);
    const sourceCamera = sceneRenderCamera(bundle.camera);
    if (sceneCameraEquivalent(targetCamera, sourceCamera)) {
      return bundle;
    }
    return Object.assign({}, bundle, {
      camera: targetCamera,
      sourceCamera: sourceCamera,
    });
  }

  function sceneControlsMetrics(readViewport, props) {
    const viewport = typeof readViewport === "function" ? readViewport() : null;
    return {
      width: Math.max(1, sceneViewportValue(viewport, "cssWidth", sceneNumber(props && props.width, 720))),
      height: Math.max(1, sceneViewportValue(viewport, "cssHeight", sceneNumber(props && props.height, 420))),
    };
  }

  function syncSceneControlsFromSource(controls, readSourceCamera) {
    const sourceCamera = typeof readSourceCamera === "function" ? readSourceCamera() : null;
    syncSceneControlsFromCamera(controls, sourceCamera);
  }

  function sceneOrbitStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event) {
    if (controls.active || !scenePointerCanStartDrag(controls, event)) {
      return;
    }
    syncSceneControlsFromSource(controls, readSourceCamera);
    controls.active = true;
    controls.touched = true;
    controls.pointerId = event.pointerId;
    sceneOrbitStopInertia(controls);
    controls.orbitLastMoveMS = sceneNumber(event && event.timeStamp, sceneNowMilliseconds());
    const metrics = sceneControlsMetrics(readViewport, props);
    const point = sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height);
    controls.lastX = point.x;
    controls.lastY = point.y;
    canvas.style.cursor = "grabbing";
    attachDocumentListeners();
    if (event.isTrusted === true && event.pointerType !== "" && typeof canvas.setPointerCapture === "function" && event.pointerId != null) {
      try {
        canvas.setPointerCapture(event.pointerId);
      } catch (_) {}
    }
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function sceneOrbitMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const metrics = sceneControlsMetrics(readViewport, props);
    const sample = sceneLocalPointerSample(event, canvas, metrics.width, metrics.height, controls, "move");
    if (!controls.orbit) {
      syncSceneControlsFromSource(controls, readSourceCamera);
    }
    const now = sceneNumber(event && event.timeStamp, sceneNowMilliseconds());
    const seconds = Math.max((now - sceneNumber(controls.orbitLastMoveMS, now)) / 1000, 1 / 240);
    const rotateSpeed = sceneNumber(controls.rotateSpeed, 1);
    const rotateDirection = sceneNumber(controls.rotateDirection, 1) < 0 ? -1 : 1;
    // Horizontal camera orbit is opposite a grabbed scene. Vertical camera
    // pitch is the inverse projection: moving the camera down makes the
    // grabbed scene track upward with the pointer.
    const pitchDirection = rotateDirection < 0 ? 1 : rotateDirection;
    const pixelRadians = (Math.PI / 180) * rotateSpeed;
    const deltaYaw = controls.rotateMode === "pixel-degrees"
      ? sample.deltaX * pixelRadians * rotateDirection
      : (sample.deltaX / Math.max(metrics.width, 1)) * Math.PI * rotateSpeed * rotateDirection;
    const deltaPitch = controls.rotateMode === "pixel-degrees"
      ? sample.deltaY * pixelRadians * pitchDirection
      : (sample.deltaY / Math.max(metrics.height, 1)) * Math.PI * rotateSpeed * pitchDirection;
    controls.orbit.yaw += deltaYaw;
    controls.orbit.pitch = sceneOrbitClampPitch(controls, controls.orbit, controls.orbit.pitch + deltaPitch);
    controls.orbitVelocityYaw = sceneClamp(deltaYaw / seconds, -SCENE_ORBIT_MAX_SPEED, SCENE_ORBIT_MAX_SPEED);
    controls.orbitVelocityPitch = sceneClamp(deltaPitch / seconds, -SCENE_ORBIT_MAX_SPEED, SCENE_ORBIT_MAX_SPEED);
    controls.orbitLastMoveMS = now;
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function sceneOrbitFinishDrag(controls, canvas, detachDocumentListeners, event, scheduleOrbitInertia) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const pointerId = controls.pointerId;
    const releaseTime = sceneNumber(event && event.timeStamp, sceneNowMilliseconds());
    const releaseDelay = Math.max(0, (releaseTime - sceneNumber(controls.orbitLastMoveMS, releaseTime)) / 1000);
    const releaseDamping = Math.exp(-SCENE_ORBIT_DAMPING * releaseDelay);
    controls.orbitVelocityYaw *= releaseDamping;
    controls.orbitVelocityPitch *= releaseDamping;
    controls.active = false;
    controls.pointerId = null;
    canvas.style.cursor = "grab";
    detachDocumentListeners();
    if (pointerId != null && typeof canvas.releasePointerCapture === "function") {
      try {
        canvas.releasePointerCapture(pointerId);
      } catch (_error) {}
    }
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    if (typeof scheduleOrbitInertia === "function") {
      scheduleOrbitInertia();
    }
  }

  function sceneOrbitApplyWheel(controls, readSourceCamera, scheduleRender, event) {
    syncSceneControlsFromSource(controls, readSourceCamera);
    sceneOrbitStopInertia(controls);
    controls.touched = true;
    controls.orbit.radius = sceneOrbitClampRadius(controls.orbit, controls.orbit.target,
      controls.orbit.radius * Math.exp(sceneNumber(event && event.deltaY, 0) * 0.001 * controls.zoomSpeed));
    controls.orbit.pitch = sceneOrbitClampPitch(controls, controls.orbit, controls.orbit.pitch);
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function sceneFlyEnsureState(controls, readSourceCamera) {
    if (!controls.fly) {
      syncSceneControlsFromSource(controls, readSourceCamera);
    }
    if (!controls.fly) {
      controls.fly = sceneFlyStateFromCamera(null);
    }
    if (!controls.fly.position) {
      controls.fly.position = { x: 0, y: 0, z: -6 };
    }
    return controls.fly;
  }

  function sceneFlyStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event) {
    if (controls.active || !scenePointerCanStartDrag(controls, event)) {
      return;
    }
    sceneFlyEnsureState(controls, readSourceCamera);
    controls.active = true;
    controls.touched = true;
    controls.pointerId = event.pointerId;
    const metrics = sceneControlsMetrics(readViewport, props);
    const point = sceneLocalPointerPoint(event, canvas, metrics.width, metrics.height);
    controls.lastX = point.x;
    controls.lastY = point.y;
    canvas.style.cursor = "grabbing";
    if (typeof canvas.focus === "function") {
      canvas.focus({ preventScroll: true });
    }
    attachDocumentListeners();
    if (controls.pointerLock) {
      sceneRequestPointerLock(canvas);
      controls.pointerLocked = scenePointerLockActive(canvas);
    }
    if (event.isTrusted === true && event.pointerType !== "" && typeof canvas.setPointerCapture === "function" && event.pointerId != null) {
      try {
        canvas.setPointerCapture(event.pointerId);
      } catch (_) {}
    }
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function sceneFlyMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    const metrics = sceneControlsMetrics(readViewport, props);
    const pointerLocked = controls.pointerLock && scenePointerLockActive(canvas);
    controls.pointerLocked = pointerLocked;
    const sample = pointerLocked ? {
      deltaX: sceneNumber(event && event.movementX, 0),
      deltaY: sceneNumber(event && event.movementY, 0),
    } : sceneLocalPointerSample(event, canvas, metrics.width, metrics.height, controls, "move");
    const fly = sceneFlyEnsureState(controls, readSourceCamera);
    fly.yaw += (sample.deltaX / Math.max(metrics.width, 1)) * Math.PI * controls.lookSpeed * (controls.zoomScale || 1);
    fly.pitch = sceneClamp(
      fly.pitch + (sample.deltaY / Math.max(metrics.height, 1)) * Math.PI * controls.lookSpeed * (controls.zoomScale || 1),
      -1.52,
      1.52,
    );
    if (typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
    scheduleRender("controls");
  }

  function sceneFlyFinishDrag(controls, canvas, detachDocumentListeners, event) {
    if (!sceneDragMatchesActivePointer(controls, event)) {
      return;
    }
    if (controls.pointerLock && scenePointerLockActive(canvas) && event && event.type !== "pointerlockchange") {
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
      if (typeof event.stopPropagation === "function") {
        event.stopPropagation();
      }
      return;
    }
    const pointerId = controls.pointerId;
    controls.active = false;
    controls.pointerId = null;
    controls.pointerLocked = false;
    canvas.style.cursor = "crosshair";
    detachDocumentListeners();
    if (pointerId != null && typeof canvas.releasePointerCapture === "function") {
      try {
        canvas.releasePointerCapture(pointerId);
      } catch (_error) {}
    }
    if (event && typeof event.preventDefault === "function") {
      event.preventDefault();
    }
    if (event && typeof event.stopPropagation === "function") {
      event.stopPropagation();
    }
  }

  function sceneFlyKeyCode(event) {
    const code = String(event && (event.code || event.key) || "").toLowerCase();
    switch (code) {
      case "keyw":
      case "w":
      case "arrowup":
        return "forward";
      case "keys":
      case "s":
      case "arrowdown":
        return "back";
      case "keya":
      case "a":
      case "arrowleft":
        return "left";
      case "keyd":
      case "d":
      case "arrowright":
        return "right";
      case "space":
      case " ":
        return "up";
      case "shiftleft":
      case "shiftright":
      case "controlleft":
      case "controlright":
        return "down";
      default:
        return "";
    }
  }

  function sceneOrbitKeyCode(event) {
    const code = String(event && (event.code || event.key) || "").toLowerCase();
    switch (code) {
      case "arrowleft":
        return "left";
      case "arrowright":
        return "right";
      case "arrowup":
        return "up";
      case "arrowdown":
        return "down";
      case "equal":
      case "numpadadd":
      case "+":
        return "zoom-in";
      case "minus":
      case "numpadsubtract":
      case "-":
        return "zoom-out";
      case "home":
        return "reset";
      default:
        return "";
    }
  }

  function sceneOrbitApplyKey(controls, readSourceCamera, key) {
    if (!controls || controls.mode !== "orbit" || !key) {
      return false;
    }
    if (key === "reset") {
      const sourceCamera = typeof readSourceCamera === "function" ? readSourceCamera() : null;
      applySceneControlsCamera(controls, sourceCamera);
      return true;
    }
    syncSceneControlsFromSource(controls, readSourceCamera);
    sceneOrbitStopInertia(controls);
    controls.touched = true;
    if (key === "left" || key === "right") {
      controls.orbit.yaw += (key === "left" ? -1 : 1) * (Math.PI / 24) * controls.rotateSpeed;
      return true;
    }
    if (key === "up" || key === "down") {
      controls.orbit.pitch = sceneOrbitClampPitch(controls, controls.orbit, controls.orbit.pitch + (key === "up" ? -1 : 1) * (Math.PI / 36) * controls.rotateSpeed);
      return true;
    }
    const zoomScale = Math.exp((key === "zoom-in" ? -1 : 1) * 0.12 * controls.zoomSpeed);
    controls.orbit.radius = sceneOrbitClampRadius(controls.orbit, controls.orbit.target, controls.orbit.radius * zoomScale);
    controls.orbit.pitch = sceneOrbitClampPitch(controls, controls.orbit, controls.orbit.pitch);
    return true;
  }

  function sceneFlyApplyMovement(controls, readSourceCamera, deltaSeconds) {
    if (!controls || !controls.keys || controls.keys.size === 0) {
      return false;
    }
    const fly = sceneFlyEnsureState(controls, readSourceCamera);
    const speed = controls.moveSpeed * (controls.zoomScale || 1) * Math.max(0.001, deltaSeconds || 1 / 60);
    const yaw = sceneNumber(fly.yaw, 0);
    const pitch = controls.mode === "fly" ? sceneNumber(fly.pitch, 0) : 0;
    const cosPitch = Math.cos(pitch);
    const forward = {
      x: Math.sin(yaw) * cosPitch,
      y: -Math.sin(pitch),
      z: -Math.cos(yaw) * cosPitch,
    };
    const right = { x: Math.cos(yaw), y: 0, z: Math.sin(yaw) };
    let dx = 0;
    let dy = 0;
    let dz = 0;
    if (controls.keys.has("forward")) {
      dx += forward.x; dy += forward.y; dz += forward.z;
    }
    if (controls.keys.has("back")) {
      dx -= forward.x; dy -= forward.y; dz -= forward.z;
    }
    if (controls.keys.has("right")) {
      dx += right.x; dz += right.z;
    }
    if (controls.keys.has("left")) {
      dx -= right.x; dz -= right.z;
    }
    if (controls.keys.has("up")) {
      dy += 1;
    }
    if (controls.keys.has("down")) {
      dy -= 1;
    }
    const length = Math.hypot(dx, dy, dz);
    if (length <= 0.0001) {
      return false;
    }
    fly.position.x += (dx / length) * speed;
    fly.position.y += (dy / length) * speed;
    fly.position.z += (dz / length) * speed;
    controls.touched = true;
    return true;
  }

  // @ts-ignore TS7006 -- keep this call site JavaScript-compatible in the runtime bundle
  function setupSceneBuiltInControls(canvas, props, readViewport, readSourceCamera, scheduleRender, sceneState) {
    let base = Object.create(null);
    // @ts-ignore TS7006 -- this source is also evaluated as plain JavaScript.
    const schedule = reason => {
      if (base && base.resetZoom && (reason === "controls-reset" || reason === "vessel-reset")) base.resetZoom();
      scheduleRender(reason);
    };
    base = setupSceneBaseControls(canvas, props, readViewport, readSourceCamera, schedule, sceneState);
    if ((props.controlZoom || props.optics) && !base.controller) {
      // Fixed cameras retain their authored angle. Opt-in navigation must not
      // enable orbit rotation or consume single-pointer object gestures.
      const fixed = {
        mode: "fixed", touched: false, keys: new Set(), target: sceneControlsTarget(props),
        camera: sceneRenderCamera(readSourceCamera()),
        // @ts-ignore TS7006 -- plain JavaScript control adapter.
        syncCamera: camera => { if (!fixed.touched) fixed.camera = sceneRenderCamera(camera); },
        currentCamera: () => { fixed.syncCamera(readSourceCamera()); return fixed.camera; },
        // @ts-ignore TS7006 -- plain JavaScript control adapter.
        applyCamera: camera => { fixed.camera = sceneRenderCamera(camera); fixed.touched = true; },
      };
      base.controller = fixed;
      if (sceneState) sceneState._gosxMotionController = fixed;
    }
    if (sceneVesselEnabled(props)) base = window.__gosx_scene3d_vessel_api.setup(canvas, props, base, sceneState, {
      schedule, current: sceneCurrentControlCamera, apply: applySceneControlsCamera, orbit: sceneControlsApplyOrbit,
      subscribe: typeof gosxSubscribeSharedSignal === "function" ? gosxSubscribeSharedSignal : undefined,
      publish: typeof setSharedSignalValue === "function" ? setSharedSignalValue : undefined,
      // @ts-ignore TS7006 -- renderer-neutral creation seam for the lazy wake
      addObject: (state, id, object) => state.objects.set(id, normalizeSceneObject(Object.assign({ id, kind: "mesh" }, object), id)),
      // @ts-ignore TS2339 -- Chromium deviceMemory is also used by the ocean quality gate
      lowHardware: () => gosxLowEndHardware(navigator.deviceMemory, navigator.hardwareConcurrency),
    });
    if ((!props.controlZoom && !props.optics) || !base.controller) return base;
    const controller = base.controller, current = controller.currentCamera;
    // @ts-ignore TS7006 -- plain JavaScript control adapter.
    if (!current) controller.syncCamera = camera => {
      if (controller.touched) return;
      if (controller.mode === "orbit") controller.orbit = sceneOrbitStateFromCamera(camera, controller.target, controller);
      else controller.fly = sceneFlyStateFromCamera(camera);
    };
    const read = current ? () => current() : () => {
      controller.syncCamera(readSourceCamera());
      return controller.mode === "orbit" ? sceneOrbitCamera(controller.orbit, readSourceCamera()) : sceneFlyCamera(controller.fly, readSourceCamera());
    };
    return window.__gosx_runtime_api.scene3DZoom.setup(canvas, props, base, {
      read,
      subscribe: typeof gosxSubscribeSharedSignal === "function" ? gosxSubscribeSharedSignal : undefined,
      publish: typeof setSharedSignalValue === "function" ? setSharedSignalValue : undefined,
      // @ts-ignore TS7006 -- viewport FOV is sampled at view reset, before optical zoom.
      fov: camera => sceneViewportCamera(camera, sceneState.camera || readSourceCamera(), readViewport()).fov,
      // @ts-ignore TS7006 -- plain JavaScript control adapter.
      apply: camera => {
        controller.active = false; controller.touched = true; controller.pointerId = null; sceneOrbitStopInertia(controller);
        controller.keys.clear();
        if (controller.mode === "orbit") controller.orbit = sceneOrbitStateFromCamera(camera, controller.target, controller);
        else controller.fly = sceneFlyStateFromCamera(camera);
      },
      // @ts-ignore TS7006 -- screen-space pan keeps the authored camera angle.
      pan: delta => {
        const camera = read();
        if (controller.mode === "orbit") {
          for (const key of ["x", "y", "z"]) controller.orbit.target[key] += delta[key];
          controller.target = Object.assign({}, controller.orbit.target);
        } else if (controller.mode === "fixed") {
          controller.camera = Object.assign({}, camera, { x: camera.x + delta.x, y: camera.y + delta.y, z: camera.z + delta.z });
          for (const key of ["x", "y", "z"]) controller.target[key] += delta[key];
        } else if (controller.fly) {
          for (const key of ["x", "y", "z"]) controller.fly.position[key] += delta[key];
        }
        controller.touched = true;
      }, schedule: scheduleRender, requestFrame: sceneMotionRequestFrame, cancelFrame: sceneMotionCancelFrame, now: sceneNowMilliseconds,
    });
  }
  // @ts-ignore TS7006 -- shared JavaScript control entrypoint
  function setupSceneBaseControls(canvas, props, readViewport, readSourceCamera, scheduleRender, sceneState) {
    if (sceneWalkEnabled(props)) {
      return window.__gosx_scene3d_walk_api.setup(canvas, props, readSourceCamera, scheduleRender, sceneState, {
        camera: sceneRenderCamera, requestLock: sceneRequestPointerLock, exitLock: sceneExitPointerLock,
        locked: scenePointerLockActive, requestFrame: sceneMotionRequestFrame, cancelFrame: sceneMotionCancelFrame, now: sceneNowMilliseconds,
      });
    }
    const controls = createSceneControls(props);
    if (sceneState) sceneState._gosxMotionController = controls;
    if (!canvas || !controls) {
      return {
        controller: controls,
        dispose() {},
      };
    }

    let documentListenersAttached = false;
    let orbitFrame = 0;
    let orbitLastFrameMS = 0;
    let flyFrame = 0;
    let flyLastFrameMS = 0;
    const flyMode = controls.mode === "first-person" || controls.mode === "fly";
    canvas.style.cursor = flyMode ? "crosshair" : "grab";
    canvas.style.touchAction = "none";
    if (!canvas.hasAttribute("tabindex")) {
      canvas.setAttribute("tabindex", "0");
    }
    if (!flyMode && !canvas.hasAttribute("aria-keyshortcuts")) {
      canvas.setAttribute("aria-keyshortcuts", "ArrowLeft ArrowRight ArrowUp ArrowDown Home + -");
    }

    function onPointerLockChange(event) {
      if (!flyMode || !controls.pointerLock) {
        return;
      }
      const locked = scenePointerLockActive(canvas);
      controls.pointerLocked = locked;
      if (!locked && controls.active) {
        sceneFlyFinishDrag(controls, canvas, detachDocumentListeners, event || { type: "pointerlockchange" });
      }
    }

    function attachDocumentListeners() {
      if (documentListenersAttached) {
        return;
      }
      documentListenersAttached = true;
      document.addEventListener("pointermove", onPointerMove);
      document.addEventListener("pointerup", finishPointerDrag);
      document.addEventListener("pointercancel", finishPointerDrag);
    }

    function detachDocumentListeners() {
      if (!documentListenersAttached) {
        return;
      }
      documentListenersAttached = false;
      document.removeEventListener("pointermove", onPointerMove);
      document.removeEventListener("pointerup", finishPointerDrag);
      document.removeEventListener("pointercancel", finishPointerDrag);
    }

    function cancelOrbitInertia() {
      if (orbitFrame) {
        sceneMotionCancelFrame.call(null, orbitFrame);
        orbitFrame = 0;
      }
    }

    function scheduleOrbitInertia() {
      if (flyMode || orbitFrame || !sceneOrbitInertiaActive(controls)) {
        return;
      }
      orbitLastFrameMS = sceneNowMilliseconds();
      const step = function(now) {
        orbitFrame = 0;
        const current = sceneNumber(now, sceneNowMilliseconds());
        const delta = Math.min(0.05, Math.max(0.001, (current - orbitLastFrameMS) / 1000));
        orbitLastFrameMS = current;
        if (sceneOrbitApplyInertia(controls, readSourceCamera, delta)) {
          scheduleRender("controls-inertia");
        }
        if (sceneOrbitInertiaActive(controls)) {
          orbitFrame = sceneMotionRequestFrame.call(null, step);
        }
      };
      orbitFrame = sceneMotionRequestFrame.call(null, step);
    }

    function onPointerDown(event) {
      if (event && event.defaultPrevented) {
        return;
      }
      if (flyMode) {
        sceneFlyStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event);
      } else {
        cancelOrbitInertia();
        sceneOrbitStartDrag(controls, canvas, props, readViewport, readSourceCamera, attachDocumentListeners, event);
      }
    }

    function onPointerMove(event) {
      if (flyMode) {
        sceneFlyMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event);
      } else {
        sceneOrbitMoveDrag(controls, canvas, props, readViewport, readSourceCamera, scheduleRender, event);
      }
    }

    function finishPointerDrag(event) {
      if (flyMode) {
        sceneFlyFinishDrag(controls, canvas, detachDocumentListeners, event);
      } else {
        sceneOrbitFinishDrag(controls, canvas, detachDocumentListeners, event, scheduleOrbitInertia);
      }
    }

    function onWheel(event) {
      if (!flyMode) {
        sceneOrbitApplyWheel(controls, readSourceCamera, scheduleRender, event);
      }
    }

    function scheduleFlyMovement() {
      if (!flyMode || flyFrame || controls.keys.size === 0) {
        return;
      }
      flyLastFrameMS = sceneNowMilliseconds();
      const step = function(now) {
        flyFrame = 0;
        const current = sceneNumber(now, sceneNowMilliseconds());
        const delta = Math.min(0.05, Math.max(0.001, (current - flyLastFrameMS) / 1000));
        flyLastFrameMS = current;
        if (sceneFlyApplyMovement(controls, readSourceCamera, delta)) {
          scheduleRender("controls");
        }
        if (controls.keys.size > 0) {
          flyFrame = sceneMotionRequestFrame.call(null, step);
        }
      };
      flyFrame = sceneMotionRequestFrame.call(null, step);
    }

    function onKeyDown(event) {
      if (!flyMode) {
        if (document.activeElement !== canvas) {
          return;
        }
        const orbitKey = sceneOrbitKeyCode(event);
        if (!sceneOrbitApplyKey(controls, readSourceCamera, orbitKey)) {
          return;
        }
        cancelOrbitInertia();
        scheduleRender(orbitKey === "reset" ? "controls-reset" : "controls-keyboard");
        if (typeof event.preventDefault === "function") {
          event.preventDefault();
        }
        return;
      }
      if (document.activeElement !== canvas && !controls.touched) {
        return;
      }
      const key = sceneFlyKeyCode(event);
      if (!key) {
        return;
      }
      controls.keys.add(key);
      sceneFlyApplyMovement(controls, readSourceCamera, 1 / 60);
      scheduleFlyMovement();
      scheduleRender("controls");
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
    }

    function onKeyUp(event) {
      if (!flyMode) {
        return;
      }
      const key = sceneFlyKeyCode(event);
      if (!key) {
        return;
      }
      controls.keys.delete(key);
      if (typeof event.preventDefault === "function") {
        event.preventDefault();
      }
    }

    canvas.addEventListener("pointerdown", onPointerDown);
    canvas.addEventListener("pointermove", onPointerMove);
    canvas.addEventListener("pointerup", finishPointerDrag);
    canvas.addEventListener("pointercancel", finishPointerDrag);
    canvas.addEventListener("lostpointercapture", finishPointerDrag);
    canvas.addEventListener("wheel", onWheel, { passive: false });
    document.addEventListener("keydown", onKeyDown);
    if (flyMode) {
      document.addEventListener("keyup", onKeyUp);
      if (controls.pointerLock) {
        document.addEventListener("pointerlockchange", onPointerLockChange);
        document.addEventListener("mozpointerlockchange", onPointerLockChange);
        document.addEventListener("webkitpointerlockchange", onPointerLockChange);
      }
    }

    return {
      controller: controls,
      cancelTouch() { controls.active = false; controls.pointerId = null; detachDocumentListeners(); cancelOrbitInertia(); sceneOrbitStopInertia(controls); },
      // stopInertia cancels an orbit glide. The managed control-forms camera
      // callback used to read flyMode, cancelOrbitInertia and controls
      // directly, but those are locals of this function, so the callback threw
      // ReferenceError. Expose the operation on the handle instead.
      stopInertia() {
        if (flyMode) return false;
        cancelOrbitInertia();
        sceneOrbitStopInertia(controls);
        return true;
      },
      dispose() {
        detachDocumentListeners();
        cancelOrbitInertia();
        if (sceneState) sceneState._gosxMotionController = null;
        if (flyFrame) {
          sceneMotionCancelFrame.call(null, flyFrame);
          flyFrame = 0;
        }
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishPointerDrag);
        canvas.removeEventListener("pointercancel", finishPointerDrag);
        canvas.removeEventListener("lostpointercapture", finishPointerDrag);
        canvas.removeEventListener("wheel", onWheel);
        document.removeEventListener("keydown", onKeyDown);
        if (flyMode) {
          sceneExitPointerLock(canvas);
          document.removeEventListener("keyup", onKeyUp);
          if (controls.pointerLock) {
            document.removeEventListener("pointerlockchange", onPointerLockChange);
            document.removeEventListener("mozpointerlockchange", onPointerLockChange);
            document.removeEventListener("webkitpointerlockchange", onPointerLockChange);
          }
        }
      },
    };
  }
