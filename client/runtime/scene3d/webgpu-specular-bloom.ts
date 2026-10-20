type SceneSpecularTarget = string | { format: string; specular: boolean };
function sceneWebGPUColorTarget(format: string, specular: boolean): SceneSpecularTarget {
  return specular ? { format, specular: true } : format;
}
function sceneWebGPUColorFragment(module: any, target: SceneSpecularTarget, blend: any): any {
  const selective = typeof target !== "string" && target.specular;
  const format = typeof target === "string" ? target : target.format;
  return { module, entryPoint: selective ? "fragmentMainSpecular" : "fragmentMain",
    targets: selective ? [{ format, blend }, { format: "rgba16float", blend }] : [{ format, blend }] };
}
// The resolved attachment and optional MSAA attachment share the bounded main
// postFX dimensions. Disabling, resizing and disposal release both handles.
function sceneWebGPUSpecularTarget(device: any): any {
  let resolved: any = null, multisampled: any = null, view: any = null, msaaView: any = null;
  let key = "";
  function dispose(): void {
    if (resolved) resolved.destroy();
    if (multisampled) multisampled.destroy();
    resolved = multisampled = view = msaaView = null; key = "";
  }
  return {
    prepare(width: number, height: number, samples: number, enabled: boolean): any {
      if (!enabled) { dispose(); return null; }
      const next = width + "x" + height + ":" + samples;
      if (next !== key) {
        dispose();
        resolved = device.createTexture({ label: "gosx-specular-bloom", size: [width, height, 1], format: "rgba16float", usage: GPUTextureUsage.RENDER_ATTACHMENT | GPUTextureUsage.TEXTURE_BINDING });
        view = resolved.createView();
        if (samples > 1) {
          multisampled = device.createTexture({ label: "gosx-specular-bloom-msaa", size: [width, height, 1], format: "rgba16float", sampleCount: samples, usage: GPUTextureUsage.RENDER_ATTACHMENT });
          msaaView = multisampled.createView();
        }
        key = next;
      }
      return { view: msaaView || view, resolveTarget: msaaView ? view : undefined,
        loadOp: "clear", storeOp: "store", clearValue: { r: 0, g: 0, b: 0, a: 0 } };
    },
    view: (): any => view,
    dispose,
  };
}
