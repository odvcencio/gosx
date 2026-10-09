  // Keep rejection handling with its renderer; loading Scene3D alone cannot run it.
  function wgpuOptionalPipelinePass(label: string): string {
    if (/^gosx-post-/.test(label)) {
      var name = label.slice("gosx-post-".length);
      if (/^bloom|^blur$/.test(name)) return "bloom";
      if (/^atmosphere:/.test(name)) return "atmosphere";
      if (["toneMapping", "colorGrade", "contactShadows", "ssao", "dof", "fxaa", "vignette"].includes(name)) return name;
    }
    if (/^(gosx-post|post-|gosx-transmission)/.test(label)) return "post";
    if (/^gosx-(?:planar-)?reflection/.test(label)) return "reflections";
    return "";
  }
  function wgpuRecoverPipeline(guard: any, canvas: any, truth: any, label: string, message: string) {
    if (!label) {
      var match = message.match(/(?:RenderPipeline|ComputePipeline|ShaderModule) with ['"]([^'"]+)['"] label|['"](gosx-(?:post|reflection|transmission)[^'"]*)['"]/);
      label = match && (match[1] || match[2]) || "uncaptured";
    }
    if (guard.disposed || guard.coreError) return;
    var pass = wgpuOptionalPipelinePass(label);
    if (pass && guard.disabled.has(pass)) return;
    if (pass) guard.disabled.add(pass);
    else guard.coreError = message;
    guard.failures.push({ label: label, pass: pass || "core", message: message });
    guard.changed();
    truth.pipelineFailure(pass || "core", label, message);
    var detail = { pipeline: label, pass: pass || "core", error: message, action: pass ? "disabled" : "webgl2-fallback" };
    try { if (typeof window.__gosx_emit === "function") window.__gosx_emit("warn", "scene3d-webgpu", "pipeline-failed", detail); } catch (_err) {}
    console.warn("[gosx] WebGPU " + (pass ? pass + " disabled" : "core pipeline failed; falling back to WebGL2") + ": " + message);
    if (canvas.parentNode && typeof canvas.parentNode.setAttribute === "function") {
      canvas.parentNode.setAttribute("data-gosx-scene3d-webgpu-pipeline-failed", JSON.stringify(guard.failures));
    }
  }
