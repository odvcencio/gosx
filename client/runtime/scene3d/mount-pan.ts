// Shared fixed-camera pan math and captured gestures, loaded with optical zoom.
function szPan(camera: any, target: any, dx: number, dy: number, height: number): any {
    const radius = Math.max(0.001, Math.hypot(camera.x - target.x, camera.y - target.y, camera.z - target.z));
    const unit = camera.kind === 'orthographic'
      ? Math.abs(camera.top - camera.bottom) / Math.max(0.001, camera.zoom || 1) / Math.max(1, height)
      : 2 * radius * Math.tan(camera.fov * Math.PI / 360) / Math.max(1, height);
    let x = -dx * unit, y = dy * unit, z = 0;
    const rx = camera.rotationX || 0, ry = camera.rotationY || 0, rz = camera.rotationZ || 0;
    [y, z] = [y * Math.cos(rx) - z * Math.sin(rx), y * Math.sin(rx) + z * Math.cos(rx)];
    [x, z] = [x * Math.cos(ry) + z * Math.sin(ry), -x * Math.sin(ry) + z * Math.cos(ry)];
    [x, y] = [x * Math.cos(rz) - y * Math.sin(rz), x * Math.sin(rz) + y * Math.cos(rz)];
    return { x, y, z };
  }

function szNotify(canvas: any): void {
  if (typeof canvas.dispatchEvent === 'function') canvas.dispatchEvent(new CustomEvent('gosx:scene3d:navigation', { bubbles: true }));
}
function szOptical(initial: number, props: any): SceneZoomState {
  const min = Math.max(5, Math.min(120, Number(props.controlZoomMinFOV) || 30));
  const max = Math.max(min, Math.min(150, Number(props.controlZoomMaxFOV) || 90));
  return window.__gosx_runtime_api.scene3DZoom.create(initial, min, max);
}
function szSetupPan(canvas: any, props: any, base: any, helpers: any, listen: any): any {
  if (!props.controlPan || !helpers.pan) return { touch() {}, reset() {} };
  const controller = base.controller, orbit = base.zoomController || controller;
  let midpoint: { x: number; y: number } | null = null, grab: { id: number; x: number; y: number } | null = null;
  function move(dx: number, dy: number): void {
    if (!Number.isFinite(dx + dy)) return;
    const camera = controller.currentCamera(), target = orbit.mode === 'orbit' ? orbit.orbit.target : controller.target || { x: 0, y: 0, z: 0 };
    helpers.pan(szPan(camera, target, dx, dy, canvas.clientHeight || 800));
    helpers.schedule('controls-pan');
  }
  if (typeof props.controlPanHandle === 'string' && props.controlPanHandle.trim()) {
    const handle = document.querySelector(props.controlPanHandle);
    if (handle) {
      listen(handle, 'pointerdown', (event: PointerEvent) => {
        if (event.button !== 0 || event.isPrimary === false) return;
        szNotify(canvas); grab = { id: event.pointerId, x: event.clientX, y: event.clientY };
        handle.setPointerCapture(event.pointerId); event.preventDefault();
      }, { passive: false });
      listen(handle, 'pointermove', (event: PointerEvent) => {
        if (!grab || grab.id !== event.pointerId) return;
        move(event.clientX - grab.x, event.clientY - grab.y);
        grab.x = event.clientX; grab.y = event.clientY; event.preventDefault();
      }, { passive: false });
      for (const name of ['pointerup', 'pointercancel', 'lostpointercapture']) listen(handle, name, (event: PointerEvent) => {
        if (grab && grab.id === event.pointerId) grab = null;
      }, { passive: true });
      listen(handle, 'keydown', (event: KeyboardEvent) => {
        const delta: Record<string, [number, number]> = { ArrowLeft: [24, 0], ArrowRight: [-24, 0], ArrowUp: [0, 24], ArrowDown: [0, -24] };
        if (delta[event.key]) { szNotify(canvas); move(...delta[event.key]); event.preventDefault(); event.stopPropagation(); }
      }, { passive: false });
    }
  }
  return {
    touch(points: PointerEvent[], type: string): void {
      const center = points.length === 2 ? { x: (points[0].clientX + points[1].clientX) / 2, y: (points[0].clientY + points[1].clientY) / 2 } : null;
      if (type === 'pointermove' && center && midpoint) move(center.x - midpoint.x, center.y - midpoint.y);
      midpoint = center;
    },
    reset(): void { midpoint = null; grab = null; },
  };
}
