// Opt-in zoom shares the controls scheduler; animated scenes keep their frame cap.
interface SceneZoomState { value: number; target: number; min: number; max: number; }
// Shared records use Go field names so typed islands read the same public model.
function szOpticsReading(camera: any, config: any, factor: number): any {
  const wrap = (n: number) => (n % 360 + 360) % 360;
  const bearing = wrap(-(Number(camera.rotationY) || 0) * 180 / Math.PI);
  let sight: any = null;
  for (const place of (Array.isArray(config.landmarks) ? config.landmarks.slice(0, 256) : [])) {
    const p = place && place.position;
    if (!p || ![p.x, p.z, camera.x, camera.z].every(Number.isFinite)) continue;
    const dx = p.x - camera.x, dz = p.z - camera.z, distance = Math.hypot(dx, dz);
    const offset = wrap(Math.atan2(dx, -dz) * 180 / Math.PI - bearing + 180) - 180;
    if (distance < 12 || Math.abs(offset) > 12 || sight && Math.abs(offset) >= Math.abs(sight.offset)) continue;
    sight = { place, distance, offset };
  }
  const range = sight ? sight.distance : 0;
  return { Ready: true, Enabled: factor > 1, Factor: factor, Bearing: bearing,
    BearingLabel: String(Math.round(bearing) % 360).padStart(3, '0') + '° ' + ['N', 'NE', 'E', 'SE', 'S', 'SW', 'W', 'NW'][Math.round(bearing / 45) % 8],
    LandmarkID: sight ? String(sight.place.id || '') : '', LandmarkName: sight ? String(sight.place.name || '') : '', Range: range,
    RangeLabel: sight ? (range < 1000 ? Math.round(range) + ' m' : (range / 1000).toFixed(1) + ' km') + ' · ' + (Math.abs(sight.offset) < 1 ? 'on bearing' : Math.round(Math.abs(sight.offset)) + '° ' + (sight.offset > 0 ? 'right' : 'left')) : '',
    CanNavigate: !!(sight && sight.place.landing) };
}
// Optical magnification never moves the camera or takes over vessel controls.
function szSetupLens(canvas: any, props: any, helpers: any, listen: any): any {
  const mount = canvas.closest('[data-gosx-engine="GoSXScene3D"]') || canvas;
  const config = props.optics || {}, magnification = Number(config.magnification) || 3;
  let factor = 1, nextReading = 0, unsubscribe = () => {};
  function publish(): void {
    mount.dispatchEvent(new CustomEvent('gosx:scene3d:optical-zoom-state', { bubbles: true, detail: { factor } }));
  }
  function set(value: number): void {
    if (!Number.isFinite(value) || value < 1 || value > 8 || factor === value) return;
    factor = value; nextReading = 0; publish(); helpers.schedule('controls-optical-zoom');
  }
  listen(mount, 'gosx:scene3d:optical-zoom', (event: CustomEvent) => {
    if (event.target === mount && event.detail && typeof event.detail.factor === 'number') set(event.detail.factor);
  }, false);
  if (config.inputSignal && helpers.subscribe) unsubscribe = helpers.subscribe(config.inputSignal, (enabled: any) => {
    if (typeof enabled === 'boolean') set(enabled ? magnification : 1);
  });
  function reset(): void {
    set(1);
    if (config.inputSignal && helpers.publish) helpers.publish(config.inputSignal, false);
  }
  const scope = mount.closest && mount.closest('[data-gosx-scene3d-control-scope]') || mount;
  listen(scope, 'keydown', (event: KeyboardEvent) => {
    if (factor === 1 || event.key !== 'Escape') return;
    event.preventDefault(); event.stopPropagation(); reset();
  }, true);
  return {
    reset, dispose: () => { unsubscribe(); },
    camera: (camera: any) => {
      const now = helpers.now();
      if (config.outputSignal && helpers.publish && now >= nextReading) {
        nextReading = now + 100;
        helpers.publish(config.outputSignal, szOpticsReading(camera, config, factor));
      }
      if (factor === 1 || camera.kind === 'orthographic') return camera;
      const fov = Number(helpers.fov ? helpers.fov(camera) : camera.fov) || 75;
      return Object.assign({}, camera, { fov: 360 / Math.PI * Math.atan(Math.tan(fov * Math.PI / 360) / factor), _gosxZoomFOV: true });
    },
  };
}
(function() {
  function clamp(value: number, min: number, max: number): number { return Math.max(min, Math.min(max, value)); }
  function create(value: number, min: number, max: number): SceneZoomState {
    return { value: clamp(value, min, max), target: clamp(value, min, max), min, max };
  }
  function change(s: SceneZoomState, delta: number): void {
    if (Number.isFinite(delta)) s.target = clamp(s.target * Math.exp(clamp(delta, -4, 4)), s.min, s.max);
  }
  function advance(s: SceneZoomState, dt: number, reduced: boolean): boolean {
    const before = s.value;
    s.value += (s.target - s.value) * (reduced ? 1 : 1 - Math.exp(-18 * Math.max(0, dt)));
    if (Math.abs(s.target - s.value) < 0.001) s.value = s.target;
    return before !== s.value;
  }
  function wheel(event: WheelEvent, height: number): number {
    return event.deltaY * (event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? height : 1) * (event.ctrlKey ? 0.01 : 0.001);
  }
  function distance(a: { clientX: number; clientY: number }, b: { clientX: number; clientY: number }): number {
    return Math.hypot(a.clientX - b.clientX, a.clientY - b.clientY);
  }
  function pinch(previous: number, next: number): number { return previous > 0 && next > 0 ? Math.log(previous / next) : 0; }
  function scale(fov: number, initial: number): number { return Math.tan(fov * Math.PI / 360) / Math.tan(initial * Math.PI / 360); }
  function setup(canvas: any, props: any, base: any, helpers: any): any {
    if ((!props.controlZoom && !props.optics) || !base.controller) return base;
    const controller = base.controller, orbit = base.zoomController || controller, read = helpers.read, sync = controller.syncCamera;
    const originalCurrent = controller.currentCamera, originalApply = controller.applyCamera;
    const media = window.matchMedia && window.matchMedia('(prefers-reduced-motion: reduce)');
    const pointers = new Map<number, PointerEvent>(), listeners: Array<[any, string, any, any]> = [], pan = szSetupPan(canvas, props, base, helpers, listen);
    const optical = szSetupLens(canvas, props, helpers, listen);
    let state: SceneZoomState, view = '', initial = 75, frame = 0, last = 0, pinching = false, span = 0, disposed = false;
    const oldAction = canvas.style.touchAction, oldShortcuts = canvas.getAttribute('aria-keyshortcuts');
    canvas.style.touchAction = 'none';
    canvas.setAttribute('aria-keyshortcuts', (oldShortcuts || '') + ' + - = _');
    function select(): void {
      const next = base.state && base.state.mode === 'sailing' ? base.state.cameraMode : orbit.mode;
      if (next === view && state) return;
      view = next; controller.zoomScale = 1;
      if (next === 'orbit') {
        read(); initial = orbit.orbit.radius;
        state = create(initial, orbit.minDistance, orbit.maxDistance);
      } else if (next === 'stern') {
        initial = base.state.length * 1.8;
        state = create(initial, initial * 0.45, initial * 2.5); base.state.followDistance = state.value;
      } else {
        initial = Number(next !== 'wheel' && helpers.fov ? helpers.fov(read()) : read().fov) || 75; state = szOptical(initial, props);
      }
    }
    function reset(): void { optical.reset(); view = ''; select(); }
    base.resetZoom = reset;
    function apply(): void {
      if (view === 'orbit') orbit.orbit.radius = state.value;
      else if (view === 'stern') base.state.followDistance = state.value;
      else controller.zoomScale = scale(state.value, initial);
    }
    function schedule(): void {
      const host = canvas.closest('[data-gosx-scene3d-render-loop]');
      if (!host || host.getAttribute('data-gosx-scene3d-render-loop-wants-animation') !== 'true' || host.getAttribute('data-gosx-scene3d-render-loop') !== 'active') helpers.schedule('controls-zoom');
    }
    function tick(now: number): void {
      frame = 0; if (disposed) return;
      select(); if (advance(state, (now - last) / 1000, !!(media && media.matches))) { apply(); schedule(); }
      last = now;
      if (state.value !== state.target) frame = helpers.requestFrame(tick);
    }
    function zoom(delta: number): void {
      select(); change(state, delta * (Number(props.controlZoomSpeed) || 1)); controller.touched = true; orbit.touched = true;
      if (media && media.matches) { advance(state, 0, true); apply(); schedule(); }
      else if (!frame && state.value !== state.target) { last = helpers.now(); frame = helpers.requestFrame(tick); }
    }
    controller.currentCamera = () => {
      select(); const camera = read();
      return optical.camera(!props.controlZoom || view === 'orbit' || view === 'stern' ? camera : Object.assign({}, camera, { fov: state.value, _gosxZoomFOV: true }));
    };
    controller.applyCamera = (camera: any) => {
      if (originalApply) originalApply(camera); else helpers.apply(camera);
      reset();
    };
    function listen(target: any, name: string, fn: any, options: any): void {
      target.addEventListener(name, fn, options); listeners.push([target, name, fn, options]);
    }
    if (props.controlZoom) listen(canvas, 'wheel', (event: WheelEvent) => { event.preventDefault(); event.stopImmediatePropagation(); szNotify(canvas); zoom(wheel(event, canvas.clientHeight || 800)); }, { passive: false, capture: true });
    listen(canvas, 'keydown', (event: KeyboardEvent) => {
      if (!props.controlZoom || document.activeElement !== canvas) return;
      if (!['+', '=', '-', '_'].includes(event.key)) return;
      event.preventDefault(); event.stopImmediatePropagation(); zoom(event.key === '+' || event.key === '=' ? -0.12 : 0.12);
    }, { capture: true });
    function touch(event: PointerEvent): void {
      if (!props.controlZoom || event.pointerType !== 'touch') return;
      if (event.type === 'pointerdown') {
        pointers.set(event.pointerId, event);
        if (pointers.size === 2) { pinching = true; szNotify(canvas); if (base.cancelTouch) base.cancelTouch(); }
      } else if (event.type === 'pointermove' && pointers.has(event.pointerId)) pointers.set(event.pointerId, event);
      else if (event.type !== 'pointermove') pointers.delete(event.pointerId);
      if (!pinching) return;
      event.preventDefault(); event.stopImmediatePropagation();
      if (event.type === 'pointerdown' && canvas.setPointerCapture) canvas.setPointerCapture(event.pointerId);
      const points = Array.from(pointers.values()), next = points.length === 2 ? distance(points[0], points[1]) : 0;
      if (event.type === 'pointermove' && span && next) zoom(pinch(span, next));
      pan.touch(points, event.type);
      span = next;
      if (!pointers.size) { pinching = false; span = 0; }
    }
    for (const name of ['pointerdown', 'pointermove', 'pointerup', 'pointercancel']) listen(canvas, name, touch, { passive: false, capture: true });
    listen(window, 'blur', () => { pointers.clear(); pinching = false; span = 0; pan.reset(); }, { passive: true });
    if (media && media.addEventListener) listen(media, 'change', () => {
      if (media.matches) { select(); advance(state, 0, true); apply(); schedule(); }
    }, { passive: true });
    const originalReset = base.reset;
    if (originalReset) base.reset = () => { originalReset(); reset(); };
    const dispose = base.dispose;
    base.dispose = () => {
      optical.dispose();
      disposed = true; if (frame) helpers.cancelFrame(frame);
      for (const [target, name, fn, options] of listeners) target.removeEventListener(name, fn, options);
      pointers.clear(); controller.currentCamera = originalCurrent; controller.syncCamera = sync; controller.applyCamera = originalApply;
      controller.zoomScale = 1; canvas.style.touchAction = oldAction;
      if (oldShortcuts == null) canvas.removeAttribute('aria-keyshortcuts'); else canvas.setAttribute('aria-keyshortcuts', oldShortcuts);
      dispose();
    };
    return base;
  }
  window.__gosx_runtime_api.scene3DZoom = { setup, create, change, advance, wheel, distance, pinch, scale, panDelta: szPan };
})();
