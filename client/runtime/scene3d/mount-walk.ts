// Opt-in walking authority. Loaded only for props.walk; camera changes use the
// existing controls scheduler and never mutate scene resources or draw plans.
// @ts-check
interface SceneWalkGround {
  minX: number; minZ: number; sizeX: number; sizeZ: number;
  cols: number; rows: number; minHeight: number; maxHeight: number; heights: string;
}
interface SceneWalkField extends SceneWalkGround { samples: Uint16Array; }
interface SceneWalkHelpers {
  camera: (camera: any) => any;
  requestLock: (canvas: any) => boolean;
  exitLock: (canvas: any) => void;
  locked: (canvas: any) => boolean;
  requestFrame: (callback: (now: number) => void) => number;
  cancelFrame: (handle: number) => void;
  now: () => number;
}
interface SceneWalkState {
  config: Record<string, any>; ground: SceneWalkField | null;
  camera: Record<string, any>; start: Record<string, any>;
  x: number; z: number; yaw: number; pitch: number; eyeY: number; velocityY: number;
  grounded: boolean; distance: number; bobWeight: number; bob: number;
  reduced: boolean; settling: boolean; moving: boolean;
  eyeHeight: number; radius: number; speed: number; sprint: number;
  slope: number; stepHeight: number; amplitude: number; lookSpeed: number;
}

(function() {
  const mounts = new Map<HTMLElement, () => void>();
  function number(value: any, fallback: number): number {
    const n = Number(value);
    return value == null || !Number.isFinite(n) ? fallback : n;
  }
  function positive(value: any, fallback: number): number {
    return number(value, 0) > 0 ? Number(value) : fallback;
  }
  function clamp(value: number, lo: number, hi: number): number {
    return Math.max(lo, Math.min(hi, value));
  }
  function decodeGround(ground: SceneWalkGround | null): SceneWalkField | null {
    if (!ground) return null;
    const count = ground.cols * ground.rows;
    if (!Number.isSafeInteger(count) || ground.cols < 1 || ground.rows < 1 || ground.sizeX <= 0 || ground.sizeZ <= 0) {
      throw new Error("invalid Scene3D walk ground dimensions");
    }
    const bytes = atob(ground.heights);
    if (bytes.length !== count * 2) throw new Error("invalid Scene3D walk height sample count");
    const samples = new Uint16Array(count);
    for (let i = 0; i < count; i++) samples[i] = bytes.charCodeAt(i * 2) | bytes.charCodeAt(i * 2 + 1) << 8;
    return Object.assign({}, ground, { samples });
  }
  function sampleGround(ground: SceneWalkField | null, x: number, z: number): number {
    if (!ground) return 0;
    const col = clamp((x - ground.minX) / ground.sizeX, 0, 1) * (ground.cols - 1);
    const row = clamp((z - ground.minZ) / ground.sizeZ, 0, 1) * (ground.rows - 1);
    const x0 = Math.floor(col), z0 = Math.floor(row);
    const x1 = Math.min(x0 + 1, ground.cols - 1), z1 = Math.min(z0 + 1, ground.rows - 1);
    const tx = col - x0, tz = row - z0, s = ground.samples;
    const a = s[z0 * ground.cols + x0] * (1 - tx) + s[z0 * ground.cols + x1] * tx;
    const b = s[z1 * ground.cols + x0] * (1 - tx) + s[z1 * ground.cols + x1] * tx;
    return ground.minHeight + (a * (1 - tz) + b * tz) / 65535 * (ground.maxHeight - ground.minHeight);
  }
  function groundHeight(s: SceneWalkState, x: number, z: number): number {
    return window.__gosx_scene3d_walk_surfaces.height(s.config.surfaces, x, z, sampleGround(s.ground, x, z));
  }
  function createState(camera: Record<string, any>, config: Record<string, any>, reduced: boolean): SceneWalkState {
    return {
      config, ground: decodeGround(config.ground || null), camera: Object.assign({}, camera), start: Object.assign({}, camera),
      x: number(camera.x, 0), z: number(camera.z, 0), yaw: number(camera.rotationY, 0), pitch: number(camera.rotationX, 0),
      eyeY: number(camera.y, 0), velocityY: 0, grounded: false, distance: 0, bobWeight: 0, bob: 0,
      reduced, settling: false, moving: false,
      eyeHeight: positive(config.eyeHeight, 1.7), radius: positive(config.radius, 0.35), speed: positive(config.walkSpeed, 1.6),
      sprint: positive(config.sprintMultiplier, 2.2), slope: Math.tan(clamp(positive(config.maxSlope, 38), 0, 89) * Math.PI / 180),
      stepHeight: positive(config.stepHeight, 0.3), amplitude: Math.max(0, number(config.headBob, 0.03)),
      lookSpeed: positive(config.lookSpeed, 2.2) / 1000,
    };
  }
  function colliderContains(c: Record<string, any>, x: number, y: number, z: number, radius: number): boolean {
    const dx = x - number(c.x, 0), dz = z - number(c.z, 0), cy = number(c.y, 0);
    if (c.kind === "cylinder") {
      if (y < cy || (c.height > 0 && y > cy + c.height)) return false;
      return Math.hypot(dx, dz) < positive(c.radius, 0) + radius;
    }
    if (c.kind === "sphere") {
      const r = positive(c.radius, 0), dy = y - cy;
      if (Math.abs(dy) > r) return false;
      return Math.hypot(dx, dz) < Math.sqrt(r * r - dy * dy) + radius;
    }
    if (c.kind === "box") {
      if (Math.abs(y - cy) > number(c.sizeY, 0) / 2) return false;
      const yaw = number(c.rotationY, 0), cos = Math.cos(yaw), sin = Math.sin(yaw);
      const bx = cos * dx - sin * dz, bz = sin * dx + cos * dz;
      const ox = Math.max(0, Math.abs(bx) - number(c.sizeX, 0) / 2);
      const oz = Math.max(0, Math.abs(bz) - number(c.sizeZ, 0) / 2);
      return ox * ox + oz * oz < radius * radius;
    }
    return false;
  }
  function candidateAllowed(s: SceneWalkState, x: number, z: number): boolean {
    const bounds = s.config.bounds;
    if (bounds && (x < bounds.minX || x > bounds.maxX || z < bounds.minZ || z > bounds.maxZ)) return false;
    const height = groundHeight(s, x, z), water = s.config.water;
    if (water && height < number(water.level, 0) - positive(water.maxDepth, 0.55)) return false;
    const rise = height - groundHeight(s, s.x, s.z), distance = Math.hypot(x - s.x, z - s.z);
    if (rise > s.stepHeight && rise > distance * s.slope) return false;
    // Inspect the uphill run over a step-sized span. Otherwise tiny rAF steps
    // would treat every continuous steep slope as a series of permitted steps.
    // A short ledge that levels out within StepHeight remains traversable.
    if (rise > 0 && distance > 0 && s.slope > 0) {
      const span = Math.max(distance, s.stepHeight / s.slope * 1.05);
      const ahead = groundHeight(s, s.x + (x - s.x) / distance * span, s.z + (z - s.z) / distance * span);
      const climb = ahead - groundHeight(s, s.x, s.z);
      if (climb > s.stepHeight && climb > span * s.slope) return false;
    }
    const colliders = s.config.colliders;
    if (Array.isArray(colliders)) {
      for (let i = 0; i < colliders.length; i++) if (colliderContains(colliders[i], x, height, z, s.radius)) return false;
    }
    return true;
  }
  function slideStep(s: SceneWalkState, dx: number, dz: number): void {
    const bounds = s.config.bounds;
    if (bounds) {
      dx = clamp(s.x + dx, bounds.minX, bounds.maxX) - s.x;
      dz = clamp(s.z + dz, bounds.minZ, bounds.maxZ) - s.z;
    }
    if (candidateAllowed(s, s.x + dx, s.z + dz)) { s.x += dx; s.z += dz; return; }
    if (dx && candidateAllowed(s, s.x + dx, s.z)) s.x += dx;
    if (dz && candidateAllowed(s, s.x, s.z + dz)) s.z += dz;
  }
  function move(s: SceneWalkState, dx: number, dz: number): number {
    if (!dx && !dz) return 0;
    s.grounded = true;
    // Bound travel per collision query so a capped long frame cannot tunnel
    // through an obstacle narrower than the player's diameter.
    const count = Math.max(1, Math.ceil(Math.hypot(dx, dz) / (s.radius * 0.5)));
    let walked = 0;
    for (let i = 0; i < count; i++) {
      const x = s.x, z = s.z;
      slideStep(s, dx / count, dz / count);
      walked += Math.hypot(s.x - x, s.z - z);
    }
    s.distance += walked;
    return walked;
  }
  function look(s: SceneWalkState, yaw: number, pitch: number): void {
    s.yaw += yaw; s.pitch = clamp(s.pitch + pitch, -1.45, 1.45);
    s.camera.rotationY = s.yaw; s.camera.rotationX = s.pitch; s.camera.rotationZ = 0;
  }
  function advance(s: SceneWalkState, dt: number, strafe: number, forward: number, sprint: boolean): boolean {
    dt = clamp(dt, 0, 0.1);
    const oldY = s.camera.y, oldX = s.x, oldZ = s.z;
    const length = Math.max(1, Math.hypot(strafe, forward)), speed = s.speed * (sprint ? s.sprint : 1) * dt / length;
    const sin = Math.sin(s.yaw), cos = Math.cos(s.yaw);
    const walked = move(s, (cos * strafe - sin * forward) * speed, (-sin * strafe - cos * forward) * speed);
    s.moving = walked > 0;
    if (s.grounded) {
      const target = groundHeight(s, s.x, s.z) + s.eyeHeight;
      const offset = s.eyeY - target, omega = 32, decay = Math.exp(-omega * dt);
      const term = (s.velocityY + omega * offset) * dt;
      s.eyeY = target + (offset + term) * decay;
      s.velocityY = (s.velocityY - omega * term) * decay;
      s.settling = Math.abs(s.eyeY - target) > 0.0001 || Math.abs(s.velocityY) > 0.0001;
      if (!s.settling) { s.eyeY = target; s.velocityY = 0; }
    }
    const bobOn = !s.reduced && s.amplitude > 0;
    s.bobWeight = bobOn ? clamp(s.bobWeight + (s.moving ? dt : -dt) / 0.2, 0, 1) : 0;
    s.bob = Math.sin(s.distance * Math.PI * 2 / 1.4) * s.amplitude * s.bobWeight * (sprint ? s.sprint : 1);
    if (s.grounded) { s.camera.x = s.x; s.camera.z = s.z; s.camera.y = s.eyeY + s.bob; }
    return s.camera.y !== oldY || s.x !== oldX || s.z !== oldZ;
  }
  function resetState(s: SceneWalkState, camera: Record<string, any>): void {
    Object.assign(s.camera, camera);
    s.x = camera.x; s.z = camera.z; s.eyeY = camera.y;
    s.yaw = camera.rotationY; s.pitch = camera.rotationX;
    s.velocityY = 0; s.grounded = false; s.distance = 0;
    s.bobWeight = 0; s.bob = 0; s.moving = false; s.settling = false;
  }
  const keys: Record<string, string> = {
    KeyW: "forward", KeyS: "back", KeyA: "left", KeyD: "right", ArrowUp: "forward", ArrowDown: "back",
    ArrowLeft: "turnLeft", ArrowRight: "turnRight", PageUp: "pitchUp", PageDown: "pitchDown",
    ShiftLeft: "sprint", ShiftRight: "sprint", Home: "reset",
    w: "forward", s: "back", a: "left", d: "right", Shift: "sprint",
  };
  function deadzone(value: number): number {
    const magnitude = Math.abs(value);
    return magnitude <= 0.15 ? 0 : Math.sign(value) * (magnitude - 0.15) / 0.85;
  }
  // Default look for the joystick and hint. The rules sit under :where() (zero
  // specificity) in one runtime stylesheet, so any page rule restyles them.
  const WALK_STYLE_ATTR = "data-gosx-scene3d-walk-style";
  function ensureWalkStyles(): void {
    const head = document.head;
    if (!head || typeof document.createElement !== "function") return;
    for (const child of Array.from(head.children || [])) if (child.hasAttribute && child.hasAttribute(WALK_STYLE_ATTR)) return;
    const style = document.createElement("style");
    style.setAttribute(WALK_STYLE_ATTR, "true"); style.setAttribute("data-gosx-css-layer", "runtime");
    style.setAttribute("data-gosx-css-owner", "gosx-bootstrap"); style.setAttribute("data-gosx-css-source", "gosx-runtime");
    style.textContent = ":where(.gosx-scene3d-walk-joystick){display:none;position:absolute;width:110px;height:110px;border-radius:50%;background:rgba(20,25,30,.35);border:1px solid rgba(255,255,255,.5);pointer-events:none;z-index:10}"
      + ":where(.gosx-scene3d-walk-knob){position:absolute;left:31px;top:31px;width:48px;height:48px;border-radius:50%;background:rgba(255,255,255,.6);pointer-events:none}"
      + ":where(.gosx-scene3d-walk-hint){position:absolute;left:50%;bottom:12px;transform:translateX(-50%);max-width:90%;padding:6px 10px;border-radius:6px;background:rgba(20,25,30,.65);color:white;font:12px/1.4 sans-serif;text-align:center;pointer-events:none;z-index:9}";
    head.appendChild(style);
  }
  function joystick(mount: HTMLElement): { base: HTMLDivElement; knob: HTMLDivElement } {
    ensureWalkStyles();
    const base = document.createElement("div"), knob = document.createElement("div");
    base.setAttribute("class", "gosx-scene3d-walk-joystick"); knob.setAttribute("class", "gosx-scene3d-walk-knob");
    base.setAttribute("aria-hidden", "true");
    base.appendChild(knob); mount.appendChild(base);
    return { base, knob };
  }
  function hintElement(mount: HTMLElement, config: Record<string, any>): HTMLElement | null {
    if (config.hint === "none") return null;
    ensureWalkStyles();
    const hint = document.createElement("div");
    hint.setAttribute("class", "gosx-scene3d-walk-hint"); hint.setAttribute("role", "note");
    const touch = window.matchMedia && window.matchMedia("(pointer: coarse)").matches;
    hint.textContent = config.hint || (touch ? "Left thumb to move · drag right side to look" : "Click to walk · WASD or arrows to move · mouse to look · Shift to sprint · Esc to release");
    mount.appendChild(hint);
    return hint;
  }
  function setup(canvas: any, props: any, readCamera: () => any, schedule: (reason: string) => void, sceneState: any, helpers: SceneWalkHelpers): any {
    const mount = canvas.closest('[data-gosx-engine="GoSXScene3D"]') || canvas.parentElement;
    const media = window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)");
    const state = createState(helpers.camera(readCamera()), props.walk, !!(media && media.matches));
    const controller = { mode: "first-person", suspended: false, touched: false, active: false, keys: new Set<string>(),
      currentCamera: () => state.camera,
      setSuspended: (value: boolean) => { controller.suspended = value; clearInput(); showHint(); },
      carryCamera: (camera: any) => { state.x = camera.x; state.z = camera.z; state.eyeY = camera.y; state.yaw = camera.rotationY; Object.assign(state.camera, camera); },
      syncCamera: (camera: any) => { if (!controller.touched) resetState(state, helpers.camera(camera)); },
      applyCamera: (camera: any) => { clearInput(); controller.touched = true; resetState(state, helpers.camera(camera)); },
    };
    if (sceneState) sceneState._gosxMotionController = controller;
    const stick = joystick(mount), hint = hintElement(mount, props.walk), pads = new Set<number>();
    let frame = 0, last = 0, disposed = false;
    let moveID = -1, lookID = -1, originX = 0, originY = 0, lookX = 0, lookY = 0, stickX = 0, stickY = 0;
    const listeners: Array<[any, string, EventListener]> = [];
    canvas.style.touchAction = "none"; canvas.style.cursor = "crosshair";
    canvas.setAttribute("tabindex", "0");
    canvas.setAttribute("aria-label", (props.ariaLabel || props.label || "3D scene") + ". Walk with WASD or up/down arrows; left/right arrows turn; PageUp/PageDown look up/down; Shift sprints; Home resets.");
    canvas.setAttribute("aria-keyshortcuts", "W A S D ArrowUp ArrowDown ArrowLeft ArrowRight PageUp PageDown Shift Home");
    function listen(target: any, name: string, callback: any): void {
      target.addEventListener(name, callback, { passive: false }); listeners.push([target, name, callback]);
    }
    function focused(): boolean { return document.activeElement === canvas || helpers.locked(canvas); }
    function showHint(): void { if (hint) hint.hidden = controller.suspended || helpers.locked(canvas) || state.moving || stickX !== 0 || stickY !== 0; }
    function clearInput(): void {
      controller.keys.clear(); moveID = -1; lookID = -1; stickX = 0; stickY = 0;
      stick.base.style.display = "none"; controller.active = false; state.moving = false;
    }
    function reset(): void {
      clearInput(); controller.touched = true; resetState(state, state.start);
      if (frame) helpers.cancelFrame(frame); frame = 0;
      schedule("controls-reset"); showHint(); if (pads.size) wake();
    }
    function wake(): void {
      if (disposed || frame) return;
      last = helpers.now(); frame = helpers.requestFrame(tick);
    }
    // When scene content already animates (an ocean, particles), its paced
    // loop renders every frame and reads the walk camera; scheduling more
    // renders would draw out of phase with it and exceed MaxFPS.
    let loopHost: any = null;
    function sceneAnimates(): boolean {
      if (!loopHost && typeof canvas.closest === "function") loopHost = canvas.closest("[data-gosx-scene3d-render-loop]");
      return !!loopHost && loopHost.getAttribute("data-gosx-scene3d-render-loop") === "active" && loopHost.getAttribute("data-gosx-scene3d-render-loop-wants-animation") === "true";
    }
    const capFPS = Number(props && props.maxFPS) || 0;
    const minRenderMS = capFPS > 0 ? 1000 / capFPS : 0;
    let renderDue = false, lastRender = -Infinity;
    function tick(now: number): void {
      frame = 0;
      if (disposed || controller.suspended) return;
      const dt = clamp((now - last) / 1000, 0, 0.1); last = now;
      let strafe = stickX, forward = -stickY, yaw = 0, pitch = 0, sprint = Math.hypot(stickX, stickY) > 0.9;
      if (focused()) {
        const k = controller.keys;
        strafe += Number(k.has("right")) - Number(k.has("left")); forward += Number(k.has("forward")) - Number(k.has("back"));
        yaw = (Number(k.has("turnLeft")) - Number(k.has("turnRight"))) * 1.3 * dt;
        pitch = (Number(k.has("pitchUp")) - Number(k.has("pitchDown"))) * 1.4 * dt;
        sprint = sprint || k.has("sprint");
      }
      if (pads.size && typeof navigator.getGamepads === "function") {
        const connected = navigator.getGamepads();
        for (let i = 0; i < connected.length; i++) {
          const pad = connected[i]; if (!pad || !pad.connected) continue;
          strafe += deadzone(pad.axes[0] || 0); forward -= deadzone(pad.axes[1] || 0);
          yaw -= deadzone(pad.axes[2] || 0) * 2.2 * dt; pitch -= deadzone(pad.axes[3] || 0) * 2.2 * dt;
          sprint = sprint || !!(pad.buttons[10] && pad.buttons[10].pressed) || !!(pad.buttons[7] && pad.buttons[7].value > 0.5);
        }
      }
      if (yaw || pitch) { look(state, yaw, pitch); controller.touched = true; }
      if (strafe || forward) controller.touched = true;
      const changed = advance(state, dt, strafe, forward, sprint);
      // Movement integrates every display frame, but renders honour the
      // scene's MaxFPS: a 120 Hz display must not double an authored 60 fps cap.
      if (changed || yaw || pitch) renderDue = true;
      if (renderDue && sceneAnimates()) renderDue = false;
      else if (renderDue && now - lastRender >= minRenderMS - 1) { renderDue = false; lastRender = now; schedule("controls"); }
      showHint();
      const navigating = Math.abs(strafe) + Math.abs(forward) + Math.abs(yaw) + Math.abs(pitch) > 0;
      if (navigating || renderDue || state.settling || state.bobWeight > 0 || pads.size) frame = helpers.requestFrame(tick);
    }
    function pointerDown(event: PointerEvent): void {
      if (controller.suspended) return;
      if (event.defaultPrevented && event.pointerType !== "touch") return;
      canvas.focus({ preventScroll: true });
      if (event.pointerType !== "touch") return;
      event.preventDefault();
      const rect = canvas.getBoundingClientRect();
      if (event.clientX - rect.left < rect.width / 2 && moveID < 0) {
        moveID = event.pointerId; originX = event.clientX; originY = event.clientY;
        const parent = mount.getBoundingClientRect();
        stick.base.style.left = (originX - parent.left - 55) + "px"; stick.base.style.top = (originY - parent.top - 55) + "px";
        stick.knob.style.transform = "translate(0px,0px)"; stick.base.style.display = "block";
      } else if (event.clientX - rect.left >= rect.width / 2 && lookID < 0) {
        lookID = event.pointerId; lookX = event.clientX; lookY = event.clientY;
      }
      if (typeof canvas.setPointerCapture === "function") canvas.setPointerCapture(event.pointerId);
      controller.active = moveID >= 0 || lookID >= 0;
    }
    function pointerMove(event: PointerEvent): void {
      if (event.pointerType !== "touch") return;
      event.preventDefault();
      if (event.pointerId === moveID) {
        const dx = event.clientX - originX, dy = event.clientY - originY, scale = Math.max(55, Math.hypot(dx, dy));
        stickX = dx / scale; stickY = dy / scale;
        stick.knob.style.transform = "translate(" + stickX * 55 + "px," + stickY * 55 + "px)";
        wake();
      } else if (event.pointerId === lookID) {
        look(state, -(event.clientX - lookX) * state.lookSpeed, -(event.clientY - lookY) * state.lookSpeed);
        lookX = event.clientX; lookY = event.clientY; controller.touched = true; schedule("controls");
      }
    }
    function pointerUp(event: PointerEvent): void {
      if (event.pointerType === "touch") event.preventDefault();
      if (event.pointerId === moveID) { moveID = -1; stickX = 0; stickY = 0; stick.base.style.display = "none"; wake(); }
      if (event.pointerId === lookID) lookID = -1;
      controller.active = moveID >= 0 || lookID >= 0; showHint();
    }
    function keyDown(event: KeyboardEvent): void {
      if (controller.suspended || !focused()) return;
      const key = keys[event.code] || keys[event.key]; if (!key) return;
      event.preventDefault();
      if (key === "reset") { reset(); return; }
      controller.keys.add(key); wake();
    }
    function keyUp(event: KeyboardEvent): void {
      const key = keys[event.code] || keys[event.key]; if (!key) return;
      controller.keys.delete(key); if (focused()) event.preventDefault();
    }
    listen(canvas, "pointerdown", pointerDown); listen(canvas, "pointermove", pointerMove);
    listen(canvas, "pointerup", pointerUp); listen(canvas, "pointercancel", pointerUp); listen(canvas, "lostpointercapture", pointerUp);
    listen(canvas, "click", (event: PointerEvent) => { if (event.pointerType !== "touch") { helpers.requestLock(canvas); showHint(); } });
    listen(document, "mousemove", (event: MouseEvent) => {
      if (controller.suspended || !helpers.locked(canvas)) return;
      look(state, -event.movementX * state.lookSpeed, -event.movementY * state.lookSpeed);
      controller.touched = true; schedule("controls");
    });
    listen(document, "keydown", keyDown); listen(document, "keyup", keyUp);
    listen(canvas, "blur", () => { if (!helpers.locked(canvas)) clearInput(); });
    listen(window, "blur", clearInput);
    listen(document, "pointerlockchange", () => { if (!helpers.locked(canvas)) clearInput(); showHint(); });
    listen(document, "click", (event: MouseEvent) => {
      const target = event.target as Element;
      const button = target && typeof target.closest === "function" && target.closest("[data-gosx-scene3d-reset]");
      if (!button) return;
      const id = button.getAttribute("data-gosx-scene3d-reset");
      if (id === mount.id || (id === "" && document.querySelectorAll('[data-gosx-engine="GoSXScene3D"]').length === 1)) reset();
    });
    if (props.walk.gamepad !== false) {
      listen(window, "gamepadconnected", (event: GamepadEvent) => { pads.add(event.gamepad.index); wake(); });
      listen(window, "gamepaddisconnected", (event: GamepadEvent) => { pads.delete(event.gamepad.index); wake(); });
    }
    if (media && typeof media.addEventListener === "function") listen(media, "change", () => { state.reduced = media.matches; wake(); });
    mounts.set(mount, reset);
    return { controller, reset, bindReset: (handler: any) => mounts.set(mount, handler), stopInertia: () => false, dispose: () => {
      disposed = true; if (frame) helpers.cancelFrame(frame); helpers.exitLock(canvas); clearInput();
      for (const listener of listeners) listener[0].removeEventListener(listener[1], listener[2]);
      stick.base.remove(); if (hint) hint.remove(); mounts.delete(mount);
      if (sceneState && sceneState._gosxMotionController === controller) sceneState._gosxMotionController = null;
    } };
  }
  function resetCamera(target?: string | HTMLElement): boolean {
    const mount = typeof target === "string" ? document.getElementById(target) : target;
    const reset = mount ? mounts.get(mount) : !target && mounts.size === 1 ? mounts.values().next().value : null;
    if (!reset) return false;
    reset(); return true;
  }
  window.__gosx_scene3d_walk_api = { setup, decodeGround, sampleGround, createState, candidateAllowed, move, advance, look, resetCamera };
  const api = (window.__gosx = window.__gosx || {}).scene3d || (window.__gosx.scene3d = {});
  api.resetCamera = resetCamera;
})();
