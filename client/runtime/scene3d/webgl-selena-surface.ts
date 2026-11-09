// Selena surface-kind materials on WebGL2.
//
// A surface material carries a GLSL fragment hook, not a shader: Selena
// emits `SelenaSurface selenaSurface(SelenaSurfaceGeo selenaGeo)` plus its
// records and prefixed param uniforms (bindings.SurfaceABI
// "selena.surface.v1"). This module splices the hook into the standard PBR
// fragment in place of its material-resolution block, so the surface is lit
// by the same lights, shadows, image light, fog, exposure and tone mapping as
// a StandardMaterial. The hook's textures ride the standard material slots:
// texture i is bound through the slot's texture descriptor with the authored
// color space and REPEAT wrap, so no new texture units are needed.

const SCENE_WEBGL_SELENA_SURFACE_ABI = "selena.surface.v1";
// Slot i: [descriptor name, sampler uniform]. Order matches the WebGPU twin.
const SCENE_WEBGL_SELENA_SURFACE_SLOTS = [["baseColor", "u_albedoMap"], ["normal", "u_normalMap"], ["roughness", "u_roughnessMap"],
  ["metalness", "u_metalnessMap"], ["emissive", "u_emissiveMap"], ["occlusion", "u_occlusionMap"]];

// sceneWebGLSelenaSurfaceHook returns the material's GLSL surface hook, or "" when
// the material is not a usable surface-kind Selena material.
function sceneWebGLSelenaSurfaceHook(material: any) {
  const layout = material && material.shaderBackend === "selena" ? material.shaderLayout : null;
  if (!layout || layout.kind !== "surface" || !layout.surface || layout.surface.abi !== SCENE_WEBGL_SELENA_SURFACE_ABI) return "";
  return typeof material.customFragment === "string" ? material.customFragment.trim() : "";
}

// sceneWebGLSelenaSurfaceFragment splices a Selena surface hook into the
// standard PBR fragment. The material block runs from the albedo read to the
// unlit branch, and from the per-pixel normal to the view vector; the unlit
// branch between them stays. The bundle builder strips shader indentation and
// operator spacing and renames shader locals, except names that appear in the
// string arguments of a .replace call: so the boundaries are regexes and the
// replacement blocks stay inline literals here.
function sceneWebGLSelenaSurfaceFragment(source: string, material: any) {
  const names = (material.shaderLayout.surface.textureNames || []).slice(0, SCENE_WEBGL_SELENA_SURFACE_SLOTS.length);
  const defines = names.map(function(name: string, i: number) { return "#define " + name + " " + SCENE_WEBGL_SELENA_SURFACE_SLOTS[i][1] + "\n"; }).join("");
  const hook = defines + sceneWebGLSelenaSurfaceHook(material) + "\n";
  const out = source.replace("void main() {", function() { return hook + "void main() {"; })
    .replace(/[ \t]*vec3 albedo ?= ?u_albedo;[\s\S]*?(?=[ \t]*if \(u_unlit\) \{)/, `    SelenaSurface gosxS = selenaSurface(SelenaSurfaceGeo(v_worldPosition, normalize(v_normal), v_uv, v_tangent, v_bitangent, normalize(u_cameraPosition - v_worldPosition), v_instanceColor));
    vec3 albedo = gosxS.baseColor * v_instanceColor.rgb;
    float roughness = clamp(gosxS.roughness, 0.04, 1.0);
    roughness = clamp(roughness * (1.0 - abs(u_anisotropy) * 0.28), 0.04, 1.0);
    roughness = gsxSpecularRoughness(normalize(v_normal), roughness, u_specularAA);
    float volumePathLength = volumeThickness(v_uv);
    float metalness = clamp(gosxS.metallic, 0.0, 1.0);
    float coverage = u_opacity * clamp(v_instanceColor.a, 0.0, 1.0) * gosxS.alpha;
    bool masked = u_alphaCutoff >= 0.0;
    float blendTexAlpha = gosxS.alpha;
    if (masked && coverage < u_alphaCutoff) discard;
    float ambientOcclusion = clamp(gosxS.occlusion, 0.0, 1.0);
    float emissiveStrength = 1.0;
    vec3 emissiveColor = gosxS.emissive;
`).replace(/[ \t]*vec3 N ?= ?normalize\(v_normal\);[\s\S]*?(?=[ \t]*vec3 V ?= ?normalize\(u_cameraPosition - v_worldPosition\);)/, `    vec3 N = normalize(gosxS.normal);
    float surfaceClearcoat = gosxS.clearcoat, surfaceClearcoatRoughness = gosxS.clearcoatRoughness, surfaceSheen = gosxS.sheen, surfaceTransmission = gosxS.transmission;
`);
  if (out.indexOf("SelenaSurface gosxS") < 0 || out.indexOf("vec3 N = normalize(gosxS.normal);") < 0) throw new Error("[gosx] PBR fragment lost a Selena surface anchor");
  return out;
}

// sceneWebGLSelenaSurfaceTextures binds the hook's textures through the standard
// slot descriptors once per material record.
function sceneWebGLSelenaSurfaceTextures(material: any) {
  const list = material.shaderLayout.surfaceTextures;
  if (material._gosxSurfaceTextures === list) return;
  const descriptors = Object.assign({}, material.textureDescriptors || null);
  for (let i = 0; i < SCENE_WEBGL_SELENA_SURFACE_SLOTS.length; i++) {
    const texture = Array.isArray(list) ? list[i] : null;
    const uri = texture && typeof texture.uri === "string" ? texture.uri.trim() : "";
    const srgb = Boolean(texture && texture.colorSpace === "srgb");
    if (uri) descriptors[SCENE_WEBGL_SELENA_SURFACE_SLOTS[i][0]] = { uri: uri, role: srgb ? "base-color" : "data", colorSpace: srgb ? "srgb" : "linear", view: "2d", wrapS: 10497, wrapT: 10497 };
    else delete descriptors[SCENE_WEBGL_SELENA_SURFACE_SLOTS[i][0]];
  }
  material.textureDescriptors = descriptors;
  material._gosxSurfaceTextures = list;
}

function sceneWebGLCreateSelenaSurfaceProgram(gl: WebGL2RenderingContext, material: any, skinned: boolean) {
  const fragment = scenePBRFragmentSourceForContext(gl, sceneWebGLSelenaSurfaceFragment(SCENE_PBR_FRAGMENT_SOURCE, material));
  const vertex = skinned ? SCENE_PBR_SKINNED_VERTEX_SOURCE : SCENE_PBR_VERTEX_SOURCE;
  const fields = material.shaderLayout.uniformBlock && Array.isArray(material.shaderLayout.uniformBlock.fields) ? material.shaderLayout.uniformBlock.fields : [];
  const prefix = String(material.shaderLayout.surface.glUniformPrefix || "");
  return scenePBRCreateProgramInfo(gl, vertex, fragment, skinned ? "Selena surface shader (skinned)" : "Selena surface shader", function(program: any) {
    const uniforms = scenePBRCacheBaseUniforms(gl, program);
    if (skinned) {
      uniforms.modelMatrix = gl.getUniformLocation(program, "u_modelMatrix");
      uniforms.hasSkin = gl.getUniformLocation(program, "u_hasSkin");
      uniforms.jointMatrices = gl.getUniformLocation(program, "u_jointMatrices[0]");
    }
    const params = [];
    for (const field of fields) {
      const location = field && typeof field.name === "string" ? gl.getUniformLocation(program, prefix + field.name) : null;
      if (location) params.push({ field: field, location: location });
    }
    return { attributes: scenePBRAttributeLocations(gl, program, skinned ? "position normal uv tangent joints weights" : "position normal uv tangent"), uniforms: uniforms, surface: params };
  });
}

// sceneWebGLSelenaSurfaceProgram returns the linked surface program for material,
// false while it is still compiling, or null when material is not a surface
// material or its program failed (the caller then draws the standard path).
function sceneWebGLSelenaSurfaceProgram(gl: WebGL2RenderingContext, cache: Map<string, any>, material: any, skinned: boolean) {
  const hook = sceneWebGLSelenaSurfaceHook(material);
  if (!hook) return null;
  // Per-draw lookups reuse the material's last answer instead of rebuilding
  // the cache key, which would copy the hook source every frame.
  const slot = skinned ? "_gosxGLSurfaceSkinned" : "_gosxGLSurfaceStatic";
  const memo = material[slot];
  let record = memo && memo.hook === material.customFragment && memo.cache === cache ? memo.record : null;
  if (!record) {
    sceneWebGLSelenaSurfaceTextures(material);
    const key = (skinned ? "skinned\n" : "static\n") + JSON.stringify(material.shaderLayout.surface) + "\n" + hook;
    record = cache.get(key);
    if (!record) {
      const info = sceneWebGLCreateSelenaSurfaceProgram(gl, material, skinned);
      record = info ? { program: info } : { failed: true };
      cache.set(key, record);
    }
    material[slot] = { hook: material.customFragment, cache: cache, record: record };
  }
  if (record.failed || scenePBRProgramFailed(record.program.program)) return null;
  return scenePBRProgramReady(gl, record.program.program) ? record.program : false;
}

// sceneWebGLAuthoredProgram picks the authored PBR program variant for a
// draw: the Selena surface program (false while compiling), else the
// CustomMaterial hook program for a static mesh, else null.
function sceneWebGLAuthoredProgram(gl: WebGL2RenderingContext, cache: Map<string, any>, material: any, skinned: boolean, ensureCustom: (material: any) => any) {
  const surface = sceneWebGLSelenaSurfaceProgram(gl, cache, material, skinned);
  if (surface !== null) return surface;
  return skinned ? null : ensureCustom(material);
}

// sceneWebGLUploadSelenaSurface writes a surface program's params for one
// draw; resolve is the renderer's Selena uniform resolver (params, defaults,
// engine inputs). Other programs are left alone.
function sceneWebGLUploadSelenaSurface(gl: WebGL2RenderingContext, info: any, material: any, resolve: any) {
  if (!info || !info.surface) return;
  const layout = material.shaderLayout;
  for (const param of info.surface) {
    sceneSelenaUploadUniform(gl, param.location, param.field.type, resolve(material, layout, param.field, material), Math.floor(sceneNumber(param.field.count, 0)));
  }
}

function sceneWebGLDisposeSelenaSurfaces(cache: Map<string, any>, disposeProgram: (info: any) => void) {
  for (const record of cache.values()) disposeProgram(record && record.program);
  cache.clear();
}
