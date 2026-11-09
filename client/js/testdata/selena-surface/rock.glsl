struct SelenaSurfaceGeo {
  vec3 worldPos;
  vec3 worldNormal;
  vec2 uv;
  vec3 tangent;
  vec3 bitangent;
  vec3 viewDir;
  vec4 color;
};
struct SelenaSurface {
  vec3 baseColor;
  float alpha;
  vec3 normal;
  float roughness;
  float metallic;
  float occlusion;
  vec3 emissive;
  float clearcoat;
  float clearcoatRoughness;
  float sheen;
  float transmission;
};
uniform vec3 selena_tint;
uniform float selena_roughness;
SelenaSurface selenaSurface(SelenaSurfaceGeo selenaGeo) {
  vec3 c = (texture(selenaSurfaceTex0, selenaGeo.uv).rgb * selena_tint);
  vec3 selenaOut_baseColor = c;
  float selenaOut_alpha = 1.0;
  vec3 selenaOut_normal = normalize(selenaGeo.worldNormal);
  float selenaOut_roughness = selena_roughness;
  float selenaOut_metallic = 0.0;
  float selenaOut_occlusion = 0.9;
  vec3 selenaOut_emissive = vec3(0.0, 0.0, 0.0);
  float selenaOut_clearcoat = 0.0;
  float selenaOut_clearcoatRoughness = 0.03;
  float selenaOut_sheen = 0.0;
  float selenaOut_transmission = 0.0;
  return SelenaSurface(selenaOut_baseColor, selenaOut_alpha, selenaOut_normal, selenaOut_roughness, selenaOut_metallic, selenaOut_occlusion, selenaOut_emissive, selenaOut_clearcoat, selenaOut_clearcoatRoughness, selenaOut_sheen, selenaOut_transmission);
}
