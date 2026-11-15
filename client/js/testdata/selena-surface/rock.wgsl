struct SelenaSurfaceGeo {
  worldPos : vec3<f32>,
  worldNormal : vec3<f32>,
  uv : vec2<f32>,
  tangent : vec3<f32>,
  bitangent : vec3<f32>,
  viewDir : vec3<f32>,
  color : vec4<f32>,
};
struct SelenaSurface {
  baseColor : vec3<f32>,
  alpha : f32,
  normal : vec3<f32>,
  roughness : f32,
  metallic : f32,
  occlusion : f32,
  emissive : vec3<f32>,
  clearcoat : f32,
  clearcoatRoughness : f32,
  sheen : f32,
  transmission : f32,
};
struct SelenaSurfaceParams {
  tint : vec3<f32>,
  roughness : f32,
};
@group(2) @binding(0) var<uniform> selenaParams : SelenaSurfaceParams;
fn selenaSurface(selenaGeo : SelenaSurfaceGeo) -> SelenaSurface {
  let c = (textureSample(selenaSurfaceTex0, selenaSurfaceTex0Sampler, selenaGeo.uv).rgb * selenaParams.tint);
  let selenaOut_baseColor = c;
  let selenaOut_alpha = 1.0;
  let selenaOut_normal = normalize(selenaGeo.worldNormal);
  let selenaOut_roughness = selenaParams.roughness;
  let selenaOut_metallic = 0.0;
  let selenaOut_occlusion = 0.9;
  let selenaOut_emissive = vec3<f32>(0.0, 0.0, 0.0);
  let selenaOut_clearcoat = 0.0;
  let selenaOut_clearcoatRoughness = 0.03;
  let selenaOut_sheen = 0.0;
  let selenaOut_transmission = 0.0;
  return SelenaSurface(selenaOut_baseColor, selenaOut_alpha, selenaOut_normal, selenaOut_roughness, selenaOut_metallic, selenaOut_occlusion, selenaOut_emissive, selenaOut_clearcoat, selenaOut_clearcoatRoughness, selenaOut_sheen, selenaOut_transmission);
}
