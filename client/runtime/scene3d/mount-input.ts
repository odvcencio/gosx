// @ts-check
// Keep scene-owned geometry private; the optional input runtime projects hits.
function setupSceneControllerPickBridge(mount: HTMLElement, canvas: HTMLCanvasElement, readViewport: () => any, readBundle: () => any) {
  const request = (event: Event) => {
    window.__gosx.host.controllers.pickScene?.(mount, (event as CustomEvent).detail, canvas, readViewport, readBundle, scenePickTargetAtEvent, sceneScreenToRay);
  };
  mount.addEventListener("gosx:scene3d:pick-request", request);
  return () => mount.removeEventListener("gosx:scene3d:pick-request", request);
}

// Controller requests and canvas picking share the same rebind/dispose lifetime.
function setupSceneMountPickInteractions(
  mount: HTMLElement,
  canvas: HTMLCanvasElement,
  props: any,
  readViewport: () => any,
  readBundle: () => any,
  emitInteraction: (detail: any) => void,
  interactiveEnabled: boolean,
  emitPointerPhase: any,
) {
  const release = setupSceneControllerPickBridge(mount, canvas, readViewport, readBundle);
  const pick = setupScenePickInteractions(canvas, props, readViewport, readBundle, emitInteraction, interactiveEnabled, emitPointerPhase);
  const dispose = pick.dispose;
  pick.dispose = () => {
    release();
    dispose.call(pick);
  };
  return pick;
}
