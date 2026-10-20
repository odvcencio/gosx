// Validate emitted detail shaders without starting a browser or requiring a GPU.
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import vm from "node:vm";
import { spawnSync } from "node:child_process";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const { readSceneRendererBackendSrc } = require("../client/js/scene3d-renderer-source-set.js");
const root = path.resolve(import.meta.dirname, "..");
const dir = fs.mkdtempSync(path.join(os.tmpdir(), "scene-detail-shaders-"));
let passed = 0;
function validate(tool, args) {
  const result = spawnSync(tool, args, { encoding: "utf8" });
  if (result.error || result.status !== 0) throw new Error(result.error || result.stdout + result.stderr);
  passed++;
}
try {
  for (const backend of ["webgl", "webgpu"]) {
    const context = vm.createContext({});
    vm.runInContext(fs.readFileSync(path.join(root, "client/js/bootstrap-src/16c1-scene-detail.ts"), "utf8"), context);
    vm.runInContext(readSceneRendererBackendSrc(backend), context);
    if (backend === "webgl") {
      const fragment = path.join(dir, "detail.frag");
      fs.writeFileSync(fragment, vm.runInContext("sceneWebGLDetailFragment(SCENE_PBR_FRAGMENT_SOURCE, true)", context));
      for (const name of ["SCENE_PBR_VERTEX_SOURCE", "SCENE_PBR_SKINNED_VERTEX_SOURCE", "SCENE_PBR_INSTANCED_VERTEX_SOURCE", "SCENE_PBR_CROWD_VERTEX_SOURCE", "SCENE_PBR_CROWD_MOTION_VERTEX_SOURCE"]) {
        const vertex = path.join(dir, name + ".vert"); fs.writeFileSync(vertex, vm.runInContext(name, context));
        validate(process.env.GLSLANG_VALIDATOR || "/usr/bin/glslangValidator", ["-l", vertex, fragment]);
      }
      // The low-unit context excludes HDR IBL; validate that preprocessing too.
      fs.writeFileSync(fragment, fs.readFileSync(fragment, "utf8").replace("#define GOSX_HDR_IBL 1", "#define GOSX_HDR_IBL 0"));
      validate(process.env.GLSLANG_VALIDATOR || "/usr/bin/glslangValidator", ["-S", "frag", fragment]);
      const bakeSources = [];
      context.scenePBRCompileShader = (_, type, source) => (bakeSources.push(source), {});
      context.scenePBRLinkProgram = () => ({});
      context.scenePBRBindTexture = () => {};
      context.sceneWebGLDetailBakeResources({ createTexture() {}, texImage2D() {}, texParameteri() {}, createVertexArray() {}, createFramebuffer() {} }, {});
      const bakeVertex = path.join(dir, "bake.vert"), bakeFragment = path.join(dir, "bake.frag");
      fs.writeFileSync(bakeVertex, bakeSources[0]); fs.writeFileSync(bakeFragment, bakeSources[1]);
      validate(process.env.GLSLANG_VALIDATOR || "/usr/bin/glslangValidator", ["-l", bakeVertex, bakeFragment]);
    } else {
      for (const name of ["sceneWebGPUDetailFragment(WGSL_PBR_FRAGMENT, true)", "WGSL_PBR_VERTEX", "WGSL_PBR_INSTANCED_VERTEX", "WGSL_PBR_INSTANCED_CULL_VERTEX"]) {
        const file = path.join(dir, "detail" + passed + ".wgsl"); fs.writeFileSync(file, vm.runInContext(name, context));
        validate(process.env.NAGA || path.join(os.homedir(), ".cargo/bin/naga"), [file, file + ".spv"]);
      }
      let bakeSource = "";
      context.sceneWebGPUDetailBakeResources({ createShaderModule: d => (bakeSource = d.code, {}), createRenderPipeline: () => ({}) }, {}, {});
      const bake = path.join(dir, "bake.wgsl"); fs.writeFileSync(bake, bakeSource);
      validate(process.env.NAGA || path.join(os.homedir(), ".cargo/bin/naga"), [bake, bake + ".spv"]);
    }
  }
  console.log(`Scene3D detail shader validation: ${passed} passed (7 GLSL, 5 WGSL).`);
} finally {
  fs.rmSync(dir, { recursive: true, force: true });
}
