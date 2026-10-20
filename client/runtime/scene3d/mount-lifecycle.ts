// mount-lifecycle.ts — mount preparation, fallback ownership, and frame clock.

// Inline scenes avoid promise suspension and copied records. Referenced geometry
// resolves while mount state is still private; a failed fetch leaves live state alone.
function sceneResolveMountGeometry(props: any, commands: any[] | null): Promise<{props: any, commands: any[] | null}> | null {
  const assets = window.__gosx_scene3d_assets;
  const objects = (props.scene || props).objects || [];
  const needsObjects = objects.some((object: any) => object?.verticesURL && !object.vertices);
  const needsCommands = commands?.some((command: any) => command?.data?.props?.verticesURL && !command.data.props.vertices);
  if (!assets || (!needsObjects && !needsCommands)) return null;
  return (async () => {
    if (needsObjects) {
      const resolved = await assets.resolveObjects(objects);
      props = props.scene ? { ...props, scene: { ...props.scene, objects: resolved } } : { ...props, objects: resolved };
    }
    if (needsCommands) commands = await assets.resolveCommands(commands);
    return { props, commands };
  })();
}

// Advance the capped animation clock by whole target intervals. The display
// can present only on rAF ticks, so a non-divisor rate alternates tick counts.
// Missed intervals are discarded in one step; there is no catch-up draw loop.
// The tolerance matches the existing gate and absorbs sub-ms rAF jitter.
function sceneAnimationFrameGate(now = 0, previous = 0, interval = 0, previousInterval = 0) {
  var elapsed = now - previous;
  if (!Number.isFinite(now) || !(interval > 0) || !(previous > 0) ||
      interval !== previousInterval || elapsed < 0 || elapsed > interval * 4) {
    return { shouldRender: true, atMS: Number.isFinite(now) ? now : 0 };
  }
  if (elapsed < interval - 0.75) return { shouldRender: false, atMS: previous };
  return { shouldRender: true, atMS: previous + Math.floor((elapsed + 0.75) / interval) * interval };
}

// Keep original authored nodes distinct from a pending renderer's generated
// canvas/labels. A replacement can claim the holder before the old asynchronous
// mount disposes; only its current owner may restore the original subtree.
function sceneRetainFallback(mount: any, owner: any = {}) {
  let holder = mount.__gosxScene3DFallback;
  if (!holder) {
    const nodes = Array.from(mount.childNodes || []);
    let element = null;
    if (nodes.some((node: any) => node.nodeType === 1 || String(node.textContent || "").trim())) {
      element = document.createElement("div");
      element.setAttribute("data-gosx-scene3d-fallback", "");
      for (const node of nodes) element.appendChild(node as Node);
    }
    holder = mount.__gosxScene3DFallback = { element, owner };
  }
  holder.owner = owner;
  const element = holder.element;
  if (element) element.hidden = true;
  return {
    element,
    show() { if (holder.owner === owner && element) element.hidden = false; },
    hide() { if (holder.owner === owner && element) element.hidden = true; },
    restore() {
      if (holder.owner !== owner || mount.__gosxScene3DFallback !== holder) return;
      if (element?.parentNode === mount) {
        while (element.firstChild) mount.insertBefore(element.firstChild, element);
        mount.removeChild(element);
      }
      delete mount.__gosxScene3DFallback;
    },
  };
}
