// Compatibility entry point for first-failure loading by older WebGPU chunks.
// Current WebGPU chunks keep rejection handling with the renderer.
(function() {
  "use strict";
  function wgpuOptionalPipelinePass(label: string): string {
    if (/^gosx-post-/.test(label)) {
      var name = label.slice("gosx-post-".length);
      if (/^bloom|^blur$/.test(name)) return "bloom";
      if (name === "toneMapping") return "toneMapping";
      if (name === "colorGrade") return "colorGrade";
      if (/^atmosphere:/.test(name)) return "atmosphere";
      if (["contactShadows", "ssao", "dof", "fxaa", "vignette"].includes(name)) return name;
    }
    if (/^(gosx-post|post-)/.test(label)) return "post";
    if (/^(gosx-reflection|gosx-planar-reflection)/.test(label)) return "reflections";
    if (/^(gosx-transmission|post-depth-resolve)/.test(label)) return "post";
    return "";
  }

  function recover(guard: any, canvas: any, truth: any, label: string, message: string) {
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
  window.__gosx_scene3d_pipeline_recovery_api = { recover: recover };
})();
