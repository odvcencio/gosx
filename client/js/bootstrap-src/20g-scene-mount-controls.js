// 20g — the built-in camera controls: orbit, fly, pointer lock and inertia.
//
// setupSceneBuiltInControls attaches the pointer, wheel and key listeners and
// returns a handle. The handle is the ONLY way outside code reaches this
// state: an earlier version let the managed control-forms camera callback read
// flyMode, controls and cancelOrbitInertia directly, which threw
// ReferenceError because those are locals of setupSceneBuiltInControls.
//
// This file is a gate candidate: a scene with Controls "none" runs none of it.
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

  function sceneControlsTarget(props) {
    const raw = props && props.controlTarget && typeof props.controlTarget === "object" ? props.controlTarget : null;
    return {
      x: sceneNumber(raw && raw.x, 0),
      y: sceneNumber(raw && raw.y, 0),
      z: sceneNumber(raw && raw.z, 0),
    };
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
  const SCENE_ORBIT_DEFAULT_MIN_DISTANCE = 0.6;
  const SCENE_ORBIT_DEFAULT_MAX_DISTANCE = 256;

  function sceneOrbitPitchLimit(value) {
    return sceneClamp(
      sceneNumber(value, SCENE_ORBIT_DEFAULT_PITCH_LIMIT),
      0.1,
      SCENE_ORBIT_MAX_PITCH_LIMIT,
    );
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
    try {
      canvas.requestPointerLock({ unadjustedMovement: true });
      return true;
    } catch (_unadjustedError) {
      try {
        canvas.requestPointerLock();
        return true;
      } catch (_error) {
        return false;
      }
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
    const radius = sceneClamp(Math.hypot(offsetX, offsetY, offsetZ), minDistance, maxDistance);
    const pitchLimit = sceneOrbitPitchLimit(controls && controls.pitchLimit);
    const pitchRatioLimit = Math.sin(pitchLimit);
    return {
      target: {
        x: sceneNumber(orbitTarget.x, 0),
        y: sceneNumber(orbitTarget.y, 0),
        z: sceneNumber(orbitTarget.z, 0),
      },
      radius,
      minDistance,
      maxDistance,
      pitchLimit,
      yaw: Math.atan2(offsetX, offsetZ),
      pitch: Math.asin(sceneClamp(offsetY / Math.max(radius, 0.001), -pitchRatioLimit, pitchRatioLimit)),
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

  function sceneOrbitCamera(state, fallbackCamera) {
    const base = sceneRenderCamera(fallbackCamera);
    const orbit = state || sceneOrbitStateFromCamera(base, { x: 0, y: 0, z: 0 });
    const minDistance = Math.max(0.001, sceneNumber(orbit.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE));
    const maxDistance = Math.max(minDistance, sceneNumber(orbit.maxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE));
    const radius = sceneClamp(sceneNumber(orbit.radius, 6), minDistance, maxDistance);
    const pitchLimit = sceneOrbitPitchLimit(orbit.pitchLimit);
    const pitch = sceneClamp(sceneNumber(orbit.pitch, 0), -pitchLimit, pitchLimit);
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
    const pitchLimit = sceneOrbitPitchLimit(controls.pitchLimit);
    const nextPitch = sceneClamp(
      controls.orbit.pitch + sceneNumber(controls.orbitVelocityPitch, 0) * seconds,
      -pitchLimit,
      pitchLimit,
    );
    if (nextPitch === -pitchLimit || nextPitch === pitchLimit) {
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

  function sceneCurrentControlCamera(controls, sourceCamera, scrollCamera) {
    var cam;
    if (controls && controls.mode === "orbit") {
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
    if (typeof canvas.setPointerCapture === "function" && event.pointerId != null) {
      canvas.setPointerCapture(event.pointerId);
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
    const pitchLimit = sceneOrbitPitchLimit(controls.pitchLimit);
    controls.orbit.yaw += deltaYaw;
    controls.orbit.pitch = sceneClamp(
      controls.orbit.pitch + deltaPitch,
      -pitchLimit,
      pitchLimit,
    );
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
    controls.orbit.radius = sceneClamp(
      controls.orbit.radius * Math.exp(sceneNumber(event && event.deltaY, 0) * 0.001 * controls.zoomSpeed),
      Math.max(0.001, sceneNumber(controls.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE)),
      Math.max(sceneNumber(controls.minDistance, SCENE_ORBIT_DEFAULT_MIN_DISTANCE), sceneNumber(controls.maxDistance, SCENE_ORBIT_DEFAULT_MAX_DISTANCE)),
    );
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
    if (typeof canvas.setPointerCapture === "function" && event.pointerId != null) {
      canvas.setPointerCapture(event.pointerId);
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
    fly.yaw += (sample.deltaX / Math.max(metrics.width, 1)) * Math.PI * controls.lookSpeed;
    fly.pitch = sceneClamp(
      fly.pitch + (sample.deltaY / Math.max(metrics.height, 1)) * Math.PI * controls.lookSpeed,
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

  function sceneFlyApplyMovement(controls, readSourceCamera, deltaSeconds) {
    if (!controls || !controls.keys || controls.keys.size === 0) {
      return false;
    }
    const fly = sceneFlyEnsureState(controls, readSourceCamera);
    const speed = controls.moveSpeed * Math.max(0.001, deltaSeconds || 1 / 60);
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

  function setupSceneBuiltInControls(canvas, props, readViewport, readSourceCamera, scheduleRender) {
    const controls = createSceneControls(props);
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
    if (flyMode && !canvas.hasAttribute("tabindex")) {
      canvas.setAttribute("tabindex", "0");
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
        cancelAnimationFrame(orbitFrame);
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
          orbitFrame = requestAnimationFrame(step);
        }
      };
      orbitFrame = requestAnimationFrame(step);
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
          flyFrame = requestAnimationFrame(step);
        }
      };
      flyFrame = requestAnimationFrame(step);
    }

    function onKeyDown(event) {
      if (!flyMode || (document.activeElement !== canvas && !controls.touched)) {
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
    canvas.addEventListener("wheel", onWheel);
    if (flyMode) {
      document.addEventListener("keydown", onKeyDown);
      document.addEventListener("keyup", onKeyUp);
      if (controls.pointerLock) {
        document.addEventListener("pointerlockchange", onPointerLockChange);
        document.addEventListener("mozpointerlockchange", onPointerLockChange);
        document.addEventListener("webkitpointerlockchange", onPointerLockChange);
      }
    }

    return {
      controller: controls,
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
        if (flyFrame) {
          cancelAnimationFrame(flyFrame);
          flyFrame = 0;
        }
        canvas.removeEventListener("pointerdown", onPointerDown);
        canvas.removeEventListener("pointermove", onPointerMove);
        canvas.removeEventListener("pointerup", finishPointerDrag);
        canvas.removeEventListener("pointercancel", finishPointerDrag);
        canvas.removeEventListener("lostpointercapture", finishPointerDrag);
        canvas.removeEventListener("wheel", onWheel);
        if (flyMode) {
          sceneExitPointerLock(canvas);
          document.removeEventListener("keydown", onKeyDown);
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
