"use strict";
// Selena surface-kind materials: both renderers splice the material's hook
// into their standard PBR fragment in place of the material block, so the
// surface shares the standard lighting. The hook fixture comes from
// scene/testdata/selena/surface-rock.sel; scene/selena_surface_test.go keeps it
// in step with the Selena compiler.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs"), os = require("node:os"), path = require("node:path"), vm = require("node:vm");
const { execFileSync } = require("node:child_process");
const { createContext } = require("./runtime-test-harness.js");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const ts = require("node:module").createRequire(path.join(__dirname, "../runtime/package.json"))("typescript");

const fixture = path.join(__dirname, "testdata", "selena-surface");

function rendererContext(backend) {
  const env = createContext({});
  for (const file of ["10-runtime-primitives.ts", "10-runtime-scene-utils.ts", "11-scene-math.ts", "13-scene-material.ts", "16c1-scene-detail.ts", "16c2-scene-selena-lighting.ts"]) {
    vm.runInContext(fs.readFileSync(path.join(__dirname, "bootstrap-src", file), "utf8"), env.context);
  }
  const core = fs.readFileSync(path.join(__dirname, "bootstrap-src", "10-runtime-scene-core.ts"), "utf8");
  vm.runInContext(core.slice(0, core.indexOf("// Scene3D shared API")), env.context);
  vm.runInContext(ts.transpileModule(readSceneRendererBackendSrc(backend), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, env.context);
  return env.context;
}

function surfaceMaterial() {
  return {
    kind: "custom", shaderBackend: "selena",
    shaderLayout: JSON.parse(fs.readFileSync(path.join(fixture, "rock.layout.json"), "utf8")),
    customFragment: fs.readFileSync(path.join(fixture, "rock.glsl"), "utf8"),
    customFragmentWGSL: fs.readFileSync(path.join(fixture, "rock.wgsl"), "utf8"),
    customUniforms: { albedo: "/rock.png", tint: [0.9, 0.85, 0.8], roughness: 0.6 },
  };
}

// validate runs a real shader compiler when it is installed, like the other
// shader tests; CI's shader-validation job installs naga.
function validate(t, tool, args, source, ext) {
  try { execFileSync(tool, ["--version"], { stdio: "ignore" }); } catch (_error) { t.skip(tool + " not on PATH"); return; }
  const file = path.join(fs.mkdtempSync(path.join(os.tmpdir(), "gosx-surface-")), "shader" + ext);
  fs.writeFileSync(file, source);
  execFileSync(tool, [...args, file], { stdio: "pipe" });
}

function between(source, start, end) {
  const a = source.indexOf(start), b = source.indexOf(end, a);
  assert.ok(a >= 0 && b > a, `missing ${start} .. ${end}`);
  return source.slice(a, b);
}

test("WebGPU splices the surface hook into the standard PBR fragment", (t) => {
  const c = rendererContext("webgpu");
  const base = vm.runInContext("WGSL_PBR_FRAGMENT", c);
  const shader = c.sceneWebGPUSelenaSurfaceFragment(base, surfaceMaterial());
  assert.match(shader, /fn selenaSurface\(selenaGeo : SelenaSurfaceGeo\) -> SelenaSurface/);
  assert.match(shader, /@group\(2\) @binding\(0\) var<uniform> selenaParams : SelenaSurfaceParams;/);
  // The hook's texture names become the standard material slot bindings.
  assert.match(shader, /textureSample\(albedoTex, albedoSamp, selenaGeo\.uv\)/);
  assert.doesNotMatch(shader, /selenaSurfaceTex0/);
  const color = between(shader, "fn gosxPBRColor(in: VertexOutput) -> vec4f {", "@fragment fn fragmentMain");
  assert.match(color, /let gosxS = selenaSurface\(SelenaSurfaceGeo\(in\.worldPos, normalize\(in\.normal\), in\.uv/);
  assert.match(color, /var N = normalize\(gosxS\.normal\);/);
  assert.match(color, /let surfaceClearcoat = gosxS\.clearcoat;/);
  // The material block is gone; the standard lighting is untouched.
  assert.doesNotMatch(color, /material\.albedo|textureSample\(albedoTex|material\.hasNormalMap/);
  for (const shared of ["gosxLayerSetup(NoV, roughness, F0, surfaceClearcoat, surfaceClearcoatRoughness)", "gosxDirectBRDF(N, V, L,", "gosxImageLight(N, V, NoV,", "shadowFactor0(", "fog.hasFog"]) {
    assert.ok(color.includes(shared), "surface variant keeps " + shared);
  }
  const standard = between(base, "fn gosxPBRColor(in: VertexOutput) -> vec4f {", "@fragment fn fragmentMain");
  const tail = (source) => source.slice(source.indexOf("    let V = normalize(frame.cameraPos - in.worldPos);"));
  assert.equal(tail(color), tail(standard), "everything after the material block is the standard lighting");
  validate(t, "naga", [], shader, ".wgsl");
});

test("WebGL splices the surface hook into the standard PBR fragment", (t) => {
  const c = rendererContext("webgl");
  const base = vm.runInContext("SCENE_PBR_FRAGMENT_SOURCE", c);
  const shader = c.sceneWebGLSelenaSurfaceFragment(base, surfaceMaterial());
  assert.match(shader, /#define selenaSurfaceTex0 u_albedoMap\n/);
  assert.match(shader, /uniform vec3 selena_tint;/);
  assert.match(shader, /SelenaSurface selenaSurface\(SelenaSurfaceGeo selenaGeo\)/);
  const main = shader.slice(shader.indexOf("void main() {"));
  assert.match(main, /SelenaSurface gosxS = selenaSurface\(SelenaSurfaceGeo\(v_worldPosition, normalize\(v_normal\), v_uv/);
  assert.match(main, /if \(u_unlit\) \{/, "the unlit branch stays");
  assert.doesNotMatch(main, /vec3 albedo = u_albedo;|u_hasAlbedoMap|u_hasNormalMap/);
  const tail = (source) => source.slice(source.indexOf("    vec3 V = normalize(u_cameraPosition - v_worldPosition);"));
  assert.equal(tail(main), tail(base.slice(base.indexOf("void main() {"))), "everything after the material block is the standard lighting");
  validate(t, "glslangValidator", [], c.scenePBRFragmentSourceForContext({ getExtension: () => null, getParameter: () => 16 }, shader), ".frag");
});

test("both standard fragments still validate with the shared BRDF", (t) => {
  validate(t, "naga", [], vm.runInContext("WGSL_PBR_FRAGMENT", rendererContext("webgpu")), ".wgsl");
  validate(t, "glslangValidator", [], vm.runInContext("SCENE_PBR_FRAGMENT_SOURCE", rendererContext("webgl")), ".frag");
});

test("only usable surface-kind Selena materials take the surface path", () => {
  for (const [backend, hook, field] of [["webgpu", "sceneWebGPUSelenaSurfaceHook", "customFragmentWGSL"], ["webgl", "sceneWebGLSelenaSurfaceHook", "customFragment"]]) {
    const c = rendererContext(backend);
    const surface = surfaceMaterial();
    assert.ok(c[hook](surface).includes("selenaSurface("));
    assert.equal(c[hook]({ ...surface, shaderBackend: "" }), "");
    assert.equal(c[hook]({ ...surface, shaderLayout: { ...surface.shaderLayout, kind: "mesh" } }), "");
    assert.equal(c[hook]({ ...surface, shaderLayout: { ...surface.shaderLayout, surface: { ...surface.shaderLayout.surface, abi: "selena.surface.v2" } } }), "");
    assert.equal(c[hook]({ ...surface, [field]: "  " }), "");
    assert.equal(c[hook](null), "");
  }
  const c = rendererContext("webgpu");
  assert.equal(c.sceneWebGPUIsSelenaMaterial(surfaceMaterial()), false, "a surface hook is not a full Selena pipeline");
});

test("surface textures ride the standard slots with their color space and REPEAT wrap", () => {
  for (const [backend, map] of [["webgpu", "sceneWebGPUSelenaSurfaceTextures"], ["webgl", "sceneWebGLSelenaSurfaceTextures"]]) {
    const c = rendererContext(backend);
    const material = surfaceMaterial();
    material.textureDescriptors = { thickness: { uri: "/t.png" }, normal: { uri: "/stale.png" } };
    material.shaderLayout.surfaceTextures = [{ name: "albedo", uri: " /rock.png ", colorSpace: "srgb" }, { name: "rough", uri: "/r.png", colorSpace: "linear" }];
    c[map](material);
    const d = material.textureDescriptors;
    assert.deepEqual({ ...d.baseColor }, { uri: "/rock.png", role: "base-color", colorSpace: "srgb", view: "2d", wrapS: 10497, wrapT: 10497 });
    assert.deepEqual({ ...d.normal }, { uri: "/r.png", role: "data", colorSpace: "linear", view: "2d", wrapS: 10497, wrapT: 10497 });
    assert.equal(d.roughness, undefined);
    assert.equal(d.thickness.uri, "/t.png", "unrelated slots survive");
    const first = material.textureDescriptors;
    c[map](material);
    assert.equal(material.textureDescriptors, first, "the mapping runs once per material record");
  }
});

test("WebGL picks the surface program, waits for it, and uploads prefixed params", () => {
  const c = rendererContext("webgl");
  let created = 0;
  c.sceneWebGLCreateSelenaSurfaceProgram = (_gl, _material, skinned) => { created++; return { program: { skinned }, uniforms: {}, surface: [] }; };
  const cache = new Map(), gl = {};
  const custom = () => "custom";
  const material = surfaceMaterial();
  const info = c.sceneWebGLAuthoredProgram(gl, cache, material, false, custom);
  assert.equal(info.program.skinned, false);
  assert.equal(c.sceneWebGLAuthoredProgram(gl, cache, material, false, custom), info);
  assert.equal(c.sceneWebGLAuthoredProgram(gl, cache, material, true, custom).program.skinned, true);
  assert.equal(created, 2, "one program per hook and skinning variant");
  assert.equal(c.sceneWebGLAuthoredProgram(gl, cache, { kind: "custom" }, false, custom), "custom");
  assert.equal(c.sceneWebGLAuthoredProgram(gl, cache, { kind: "custom" }, true, custom), null);

  const calls = [];
  const record = (loc, ...values) => calls.push([loc, values.length === 1 && typeof values[0] === "object" ? Array.from(values[0]) : (values.length === 1 ? values[0] : values)]);
  const upload = {};
  for (const name of ["uniform1f", "uniform2f", "uniform3f", "uniform4f", "uniform1fv", "uniform2fv", "uniform3fv", "uniform4fv", "uniformMatrix3fv", "uniformMatrix4fv"]) upload[name] = record;
  const fields = material.shaderLayout.uniformBlock.fields;
  const params = fields.map((field) => ({ field, location: "selena_" + field.name }));
  c.sceneWebGLUploadSelenaSurface(upload, { surface: params }, material, (m, _layout, field) => m.customUniforms[field.name]);
  assert.deepEqual(calls.map((call) => call[0]).sort(), params.map((p) => p.location).sort());
  assert.ok(calls.some((call) => call[0] === "selena_roughness" && Math.abs(call[1] - 0.6) < 1e-6));
  c.sceneWebGLUploadSelenaSurface(upload, { uniforms: {} }, material, () => { throw new Error("a non-surface program uploads nothing"); });
});

test("WebGPU packs surface params once per frame into one @group(2) buffer", () => {
  const c = rendererContext("webgpu");
  c.GPUShaderStage = { VERTEX: 1, FRAGMENT: 2 };
  c.GPUBufferUsage = { UNIFORM: 64, COPY_DST: 8 };
  const writes = [];
  const device = {
    queue: { writeBuffer: (buffer, offset, data) => writes.push([buffer, offset, Array.from(data)]) },
    createBuffer: (desc) => ({ desc, destroyed: false, destroy() { this.destroyed = true; } }),
    createBindGroupLayout: (desc) => ({ desc }), createPipelineLayout: (desc) => ({ desc }),
    createShaderModule: (desc) => ({ desc }), createBindGroup: (desc) => ({ desc }),
  };
  const resources = c.sceneWebGPUSelenaSurfaceResources();
  const material = surfaceMaterial();
  const variant = c.sceneWebGPUSelenaSurfaceVariant(device, resources, material, "frame", "material", vm.runInContext("WGSL_PBR_FRAGMENT", c));
  assert.deepEqual(Array.from(variant.pipelineLayout.desc.bindGroupLayouts.slice(0, 2)), ["frame", "material"]);
  assert.equal(variant.pipelineLayout.desc.bindGroupLayouts.length, 3);
  assert.equal(c.sceneWebGPUSelenaSurfaceVariant(device, resources, surfaceMaterial(), "frame", "material", ""), variant, "materials with one hook share a variant");
  assert.equal(c.sceneWebGPUSelenaSurfaceVariant(device, resources, { kind: "standard" }, "frame", "material", ""), null);
  const frame = { time: 0 };
  c.sceneWebGPUSelenaSurfaceFrame(resources);
  const group = c.sceneWebGPUSelenaSurfaceGroup(device, resources, material, variant, frame);
  assert.equal(c.sceneWebGPUSelenaSurfaceGroup(device, resources, material, variant, frame), group);
  assert.equal(writes.length, 1, "one upload per frame");
  const fields = Object.fromEntries(material.shaderLayout.uniformBlock.fields.map((f) => [f.name, f.offset / 4]));
  const data = writes[0][2];
  assert.ok(Math.abs(data[fields.roughness] - 0.6) < 1e-6);
  assert.deepEqual(data.slice(fields.tint, fields.tint + 3).map((v) => Math.round(v * 100) / 100), [0.9, 0.85, 0.8]);
  c.sceneWebGPUSelenaSurfaceFrame(resources);
  c.sceneWebGPUSelenaSurfaceGroup(device, resources, material, variant, frame);
  assert.equal(writes.length, 2);
  const buffer = writes[0][0];
  c.sceneWebGPUDisposeSelenaSurfaces(resources);
  assert.equal(buffer.destroyed, true);
});

// The bundle builder packs the lazy renderer chunks: it strips shader
// indentation (and, for WebGL2, operator spacing) and renames shader locals
// unless a .replace call names them. The surface splice must still find the
// material block in the shipped text, and the names it declares must survive.
test("the surface splice matches the packed shaders in the shipped chunks", () => {
  for (const [backend, chunk, start, albedoRead, end, splice, names] of [
    ["webgpu", "bootstrap-feature-scene3d-webgpu.js", "fn gosxPBRColor(in: VertexOutput) -> vec4f {", /"[ \t]*var albedo = material\.albedo;"/, /"@fragment fn fragmentMain/, "sceneWebGPUSelenaSurfaceFragment",
      ["albedo", "roughness", "metalness", "volumePathLength", "ambientOcclusion", "emissiveColor", "emissiveStrength", "alphaEnabled", "coverage", "cutoff", "blendOpacity", "surfaceClearcoat", "surfaceClearcoatRoughness", "surfaceSheen", "surfaceTransmission"]],
    ["webgl", "bootstrap-feature-scene3d-webgl.js", "void main() {", /"[ \t]*vec3 albedo ?= ?u_albedo;"/, /"[ \t]*gosxSpecularOutput ?= ?vec4\(max\(/, "sceneWebGLSelenaSurfaceFragment",
      ["albedo", "roughness", "metalness", "volumePathLength", "ambientOcclusion", "emissiveColor", "emissiveStrength", "masked", "blendTexAlpha", "surfaceClearcoat", "surfaceClearcoatRoughness", "surfaceSheen", "surfaceTransmission"]],
  ]) {
    const code = fs.readFileSync(path.join(__dirname, chunk), "utf8");
    // The shipped PBR fragment is an array of JSON string literals. Find its
    // albedo read (an array element), then the entry line before it and the
    // end of the color function after it.
    const read = code.search(albedoRead);
    assert.ok(read >= 0, `${chunk} ships the ${backend} PBR fragment`);
    const at = code.lastIndexOf(JSON.stringify(start), read);
    const stop = read + code.slice(read).search(end);
    assert.ok(at >= 0 && stop > read, `${chunk} PBR fragment bounds`);
    const body = code.slice(at, stop).split(/",\s*(?:[A-Za-z_$][\w$]*,\s*)*"/).map((piece) => piece.replace(/^"|"$/g, "")).join("\n").replace(/\\"/g, '"');
    const c = rendererContext(backend);
    const spliced = c[splice](body, surfaceMaterial());
    const split = spliced.search(/(?:vec3|let) V ?= ?normalize/);
    for (const name of names) {
      assert.match(spliced.slice(0, split), new RegExp("\\b" + name + "\\b"), `${backend} surface block declares ${name}`);
    }
    for (const name of ["surfaceClearcoat", "surfaceTransmission", "ambientOcclusion", "emissiveColor", "roughness", "albedo"]) {
      assert.match(spliced.slice(split), new RegExp("\\b" + name + "\\b"), `${backend} packed lighting still reads ${name}`);
    }
  }
});
