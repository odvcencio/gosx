// @ts-check
// Keep scene-owned geometry private; the optional input runtime projects hits.
function setupSceneControllerPickBridge(mount: HTMLElement, canvas: HTMLCanvasElement, readViewport: () => any, readBundle: () => any) {
  const request = (event: Event) => {
    window.__gosx.host.controllers.pickScene?.(mount, (event as CustomEvent).detail, canvas, readViewport, readBundle, scenePickTargetAtEvent, sceneScreenToRay);
  };
  mount.addEventListener("gosx:scene3d:pick-request", request);
  return () => mount.removeEventListener("gosx:scene3d:pick-request", request);
}
