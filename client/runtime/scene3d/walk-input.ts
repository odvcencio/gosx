// Touch and desktop look share pointer ownership, deltas and teardown.
function sceneWalkPointerInput(canvas: any, mount: HTMLElement, stick: any, controller: any, helpers: SceneWalkHelpers,
  listen: (target: any, name: string, callback: any) => void,
  callbacks: [() => void, (dx: number, dy: number) => void, () => void], enabled: boolean) {
  const [wake, apply, showHint] = callbacks;
  let moveID = -1, lookID = -1, originX = 0, originY = 0, lookX = 0, lookY = 0, dragged = false;
  const input = { x: 0, y: 0, clear: () => {
    moveID = lookID = -1; input.x = input.y = 0; stick.base.style.display = "none"; controller.active = false;
  } };
  const active = () => { controller.active = moveID >= 0 || lookID >= 0; };
  listen(canvas, "pointerdown", (event: PointerEvent) => {
    const touch = event.pointerType === "touch";
    if (controller.suspended || (!touch && (event.defaultPrevented || event.button !== 0 || helpers.locked(canvas)))) return;
    canvas.focus({preventScroll: true});
    if (touch) event.preventDefault();
    const rect = canvas.getBoundingClientRect();
    if (touch && event.clientX - rect.left < rect.width / 2 && moveID < 0) {
      moveID = event.pointerId; originX = event.clientX; originY = event.clientY;
      const parent = mount.getBoundingClientRect();
      stick.base.style.left = (originX - parent.left - 55) + "px"; stick.base.style.top = (originY - parent.top - 55) + "px";
      stick.knob.style.transform = "translate(0px,0px)"; stick.base.style.display = "block";
    } else if ((!touch || event.clientX - rect.left >= rect.width / 2) && lookID < 0) {
      lookID = event.pointerId; lookX = event.clientX; lookY = event.clientY; dragged = false;
    }
    if (typeof canvas.setPointerCapture === "function") canvas.setPointerCapture(event.pointerId);
    active();
  });
  listen(canvas, "pointermove", (event: PointerEvent) => {
    if (controller.suspended || helpers.locked(canvas)) return;
    const dx = event.clientX - lookX, dy = event.clientY - lookY;
    if (event.pointerId === moveID) {
      event.preventDefault();
      const mx = event.clientX - originX, my = event.clientY - originY, scale = Math.max(55, Math.hypot(mx, my));
      input.x = mx / scale; input.y = my / scale;
      stick.knob.style.transform = "translate(" + input.x * 55 + "px," + input.y * 55 + "px)";
      wake();
    } else if (event.pointerId === lookID && (event.pointerType === "touch" || dragged || Math.hypot(dx, dy) >= 3)) {
      event.preventDefault(); dragged = true; lookX = event.clientX; lookY = event.clientY; apply(dx, dy);
    }
  });
  const up = (event: PointerEvent) => {
    if (event.pointerType === "touch") event.preventDefault();
    if (event.pointerId === moveID) { moveID = -1; input.x = input.y = 0; stick.base.style.display = "none"; wake(); }
    if (event.pointerId === lookID) lookID = -1;
    active(); showHint();
  };
  for (const name of ["pointerup", "pointercancel", "lostpointercapture"]) listen(canvas, name, up);
  listen(canvas, "click", (event: PointerEvent) => {
    if (enabled && !controller.suspended && event.pointerType !== "touch" && !dragged) helpers.requestLock(canvas);
  });
  listen(document, "mousemove", (event: MouseEvent) => {
    if (!controller.suspended && helpers.locked(canvas)) apply(event.movementX, event.movementY);
  });
  return input;
}
window.__gosx_scene3d_walk_input = sceneWalkPointerInput;
