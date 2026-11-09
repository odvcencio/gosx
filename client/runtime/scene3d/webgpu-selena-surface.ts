// Selena surface-kind materials on WebGPU, the twin of the WebGL2 Selena
// surface module. The WGSL hook (`fn selenaSurface(selenaGeo) ->
// SelenaSurface`, bindings.SurfaceABI "selena.surface.v1") replaces the
// material-resolution block of the standard PBR fragment, so the surface is
// shaded by the standard lights, shadows, image light, fog and tone mapping.
// Its textures ride the standard material group's slots (renamed in the hook
// and bound through the slot descriptors); its params are one uniform buffer
// at @group(2) @binding(0), packed by the shared Selena uniform packer.

const SCENE_WEBGPU_SELENA_SURFACE_ABI = "selena.surface.v1";
// Slot i: [descriptor name, texture binding, sampler binding]. Same order as
// the WebGL twin and createMaterialBindGroup.
const SCENE_WEBGPU_SELENA_SURFACE_SLOTS = [["baseColor", "albedoTex", "albedoSamp"], ["normal", "normalTex", "normalSamp"],
  ["roughness", "roughnessTex", "roughnessSamp"], ["metalness", "metalnessTex", "metalnessSamp"],
  ["emissive", "emissiveTex", "emissiveSamp"], ["occlusion", "occlusionTex", "occlusionSamp"]];
// sceneWebGPUSelenaSurfaceHook returns the material's WGSL surface hook, or "" when
// the material is not a usable surface-kind Selena material.
function sceneWebGPUSelenaSurfaceHook(material: any) {
  const layout = material && material.shaderBackend === "selena" ? material.shaderLayout : null;
  if (!layout || layout.kind !== "surface" || !layout.surface || layout.surface.abi !== SCENE_WEBGPU_SELENA_SURFACE_ABI) return "";
  return typeof material.customFragmentWGSL === "string" ? material.customFragmentWGSL.trim() : "";
}

// sceneWebGPUSelenaSurfaceFragment splices a Selena surface hook into the
// standard PBR fragment: the hook's texture names become the material slot
// bindings, and the material block (albedo read to unlit branch, per-pixel
// normal to view vector) becomes the hook call. The bundle builder strips
// shader indentation and renames shader locals, except names in the string
// arguments of a .replace call: so the boundaries are regexes and the
// replacement blocks stay inline literals here.
function sceneWebGPUSelenaSurfaceFragment(source: string, material: any) {
  let hook = sceneWebGPUSelenaSurfaceHook(material);
  const names = material.shaderLayout.surface.textureNames || [];
  for (let i = 0; i < names.length && i < SCENE_WEBGPU_SELENA_SURFACE_SLOTS.length; i++) {
    hook = hook.split(names[i] + "Sampler").join(SCENE_WEBGPU_SELENA_SURFACE_SLOTS[i][2]).replace(new RegExp("\\b" + names[i] + "\\b", "g"), SCENE_WEBGPU_SELENA_SURFACE_SLOTS[i][1]);
  }
  const out = source.replace("fn gosxPBRColor(in: VertexOutput) -> vec4f {", function() { return hook + "\nfn gosxPBRColor(in: VertexOutput) -> vec4f {"; })
    .replace(/[ \t]*var albedo = material\.albedo;[\s\S]*?(?=[ \t]*if \(material\.unlit != 0u\) \{)/, `    let gosxS = selenaSurface(SelenaSurfaceGeo(in.worldPos, normalize(in.normal), in.uv, in.tangent, in.bitangent, normalize(frame.cameraPos - in.worldPos), in.instanceColor));
    var albedo = gosxS.baseColor * in.instanceColor.rgb;
    let finalOpacity = material.opacity * clamp(in.instanceColor.a, 0.0, 1.0);
    let cutoff = material.alphaCutoff;
    let alphaEnabled = cutoff >= 0.0;
    let coverage = finalOpacity * gosxS.alpha;
    let blendOpacity = coverage;
    var roughness = clamp(gosxS.roughness, 0.04, 1.0);
    roughness = clamp(roughness * (1.0 - abs(material.anisotropy) * 0.28), 0.04, 1.0);
    roughness = gsxSpecularRoughness(normalize(in.normal), roughness, vec2f(material.specularAAVariance, material.volume.w));
    let volumePathLength = volumeThickness(in.uv);
    var metalness = clamp(gosxS.metallic, 0.0, 1.0);
    var ambientOcclusion = clamp(gosxS.occlusion, 0.0, 1.0);
    let emissiveStrength = 1.0;
    var emissiveColor = gosxS.emissive;
`).replace(/[ \t]*var N = normalize\(in\.normal\);[\s\S]*?(?=[ \t]*let V = normalize\(frame\.cameraPos - in\.worldPos\);)/, `    var N = normalize(gosxS.normal);
    let surfaceClearcoat = gosxS.clearcoat; let surfaceClearcoatRoughness = gosxS.clearcoatRoughness; let surfaceSheen = gosxS.sheen; let surfaceTransmission = gosxS.transmission;
`);
  if (out.indexOf("let gosxS = selenaSurface(") < 0 || out.indexOf("var N = normalize(gosxS.normal);") < 0) throw new Error("[gosx] PBR fragment lost a Selena surface anchor");
  return out;
}

// sceneWebGPUSelenaSurfaceTextures binds the hook's textures through the standard
// slot descriptors once per material record.
function sceneWebGPUSelenaSurfaceTextures(material: any) {
  const list = material.shaderLayout.surfaceTextures;
  if (material._gosxSurfaceTextures === list) return;
  const descriptors = Object.assign({}, material.textureDescriptors || null);
  for (let i = 0; i < SCENE_WEBGPU_SELENA_SURFACE_SLOTS.length; i++) {
    const texture = Array.isArray(list) ? list[i] : null;
    const uri = texture && typeof texture.uri === "string" ? texture.uri.trim() : "";
    const srgb = Boolean(texture && texture.colorSpace === "srgb");
    if (uri) descriptors[SCENE_WEBGPU_SELENA_SURFACE_SLOTS[i][0]] = { uri: uri, role: srgb ? "base-color" : "data", colorSpace: srgb ? "srgb" : "linear", view: "2d", wrapS: 10497, wrapT: 10497 };
    else delete descriptors[SCENE_WEBGPU_SELENA_SURFACE_SLOTS[i][0]];
  }
  material.textureDescriptors = descriptors;
  material._gosxSurfaceTextures = list;
}

function sceneWebGPUSelenaSurfaceResources() {
  return { variants: new Map(), states: new Set(), frame: 0 };
}

function sceneWebGPUSelenaSurfaceFields(material: any) {
  const block = material.shaderLayout.uniformBlock;
  return block && Array.isArray(block.fields) ? block.fields : [];
}

// sceneWebGPUSelenaSurfaceVariant returns the pipeline variant for a surface
// material: its fragment module and pipeline layout, shared by every material
// with the same hook. It returns null for any other material.
function sceneWebGPUSelenaSurfaceVariant(device: any, resources: any, material: any, frameLayout: any, materialLayout: any, base: string) {
  const hook = sceneWebGPUSelenaSurfaceHook(material);
  if (!hook) return null;
  // Per-draw lookups reuse the material's last answer instead of rebuilding
  // the cache key, which would copy the hook source every frame.
  const memo = material._gosxWGPUSurfaceVariant;
  if (memo && memo.hook === material.customFragmentWGSL && memo.resources === resources) return memo.variant;
  sceneWebGPUSelenaSurfaceTextures(material);
  const key = JSON.stringify(material.shaderLayout.surface) + "\n" + hook;
  let variant = resources.variants.get(key);
  if (!variant) {
    const params = sceneWebGPUSelenaSurfaceFields(material).length > 0;
    const group = params ? device.createBindGroupLayout({ label: "gosx-selena-surface", entries: [
      { binding: 0, visibility: GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } }] }) : null;
    variant = {
      key: "surface" + resources.variants.size,
      groupLayout: group,
      pipelineLayout: device.createPipelineLayout({ bindGroupLayouts: group ? [frameLayout, materialLayout, group] : [frameLayout, materialLayout] }),
      fragment: device.createShaderModule({ label: "selena-surface-frag", code: sceneWebGPUSelenaSurfaceFragment(base, material) }),
    };
    resources.variants.set(key, variant);
  }
  material._gosxWGPUSurfaceVariant = { hook: material.customFragmentWGSL, resources: resources, variant: variant };
  return variant;
}

// sceneWebGPUSelenaSurfaceGroup returns the material's @group(2) param bind group,
// packed once per frame (sceneWebGPUSelenaSurfaceFrame advances the frame).
function sceneWebGPUSelenaSurfaceGroup(device: any, resources: any, material: any, variant: any, frame: any) {
  if (!variant.groupLayout) return null;
  const layout = material.shaderLayout;
  let state = material._gosxWGPUSurface;
  if (!state || state.device !== device || state.variant !== variant) {
    const size = Math.max(16, Math.ceil(sceneNumber(layout.uniformBlock.size, 16) / 16) * 16);
    const buffer = device.createBuffer({ label: "gosx-selena-surface-params", size: size, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    state = { device: device, variant: variant, buffer: buffer, data: new Float32Array(size / 4), frame: -1,
      group: device.createBindGroup({ layout: variant.groupLayout, entries: [{ binding: 0, resource: { buffer: buffer } }] }) };
    material._gosxWGPUSurface = state;
    resources.states.add(state);
  }
  if (state.frame !== resources.frame) {
    for (const field of sceneWebGPUSelenaSurfaceFields(material)) {
      if (!field || typeof field.name !== "string") continue;
      sceneSelenaWriteUniformField(state.data, Math.floor(sceneNumber(field.offset, 0) / 4), String(field.type || "float"),
        sceneSelenaUniformValue(material, layout, field, material, null, frame), field);
    }
    device.queue.writeBuffer(state.buffer, 0, state.data);
    state.frame = resources.frame;
  }
  return state.group;
}

function sceneWebGPUSelenaSurfaceFrame(resources: any) {
  resources.frame++;
}

function sceneWebGPUDisposeSelenaSurfaces(resources: any) {
  for (const state of resources.states) state.buffer.destroy();
  resources.states.clear();
  resources.variants.clear();
}
