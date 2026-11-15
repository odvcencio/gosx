// WebGL2 standard-lighting BRDF. The GGX lobe, Smith visibility and Schlick
// Fresnel moved here from the WebGL2 PBR fragment, with two energy terms:
//
//   - Multiple scattering (Fdez-Aguera 2019, "A Multiple-Scattering
//     Microfacet Model for Real-Time Image Based Lighting"). Image light adds
//     Fms*Ems, lit by irradiance; direct lights scale the lobe by
//     1 + F0 * (1 / Ess - 1). Ess = A + B from the split-sum DFG table, or
//     from Karis' analytic fit when no table is bound.
//   - A clear coat layered over the base: its own GGX lobe with F0 0.04
//     (IOR 1.5), and the base attenuated by (1 - coat Fresnel) so the two
//     layers never reflect more than arrives.
//
// gosxDirectBRDF returns the per-light BRDF (times NdotL by the caller) and
// writes the specular part for selective bloom; gosxImageLight returns the
// split-sum image light. Both read the per-pixel GosxLayer set up once in
// main. The WebGPU BRDF module is its twin; keep their math identical.
const GLSL_BRDF = `
float distributionGGX(vec3 N, vec3 H, float roughness) {
    float a = roughness * roughness;
    float a2 = a * a;
    float NdotH = max(dot(N, H), 0.0);
    float denom = NdotH * NdotH * (a2 - 1.0) + 1.0;
    return a2 / max(PI * denom * denom, 0.0000001);
}
float geometrySchlickGGX(float NdotV, float roughness) {
    float r = roughness + 1.0;
    float k = (r * r) / 8.0;
    return NdotV / (NdotV * (1.0 - k) + k);
}
float geometrySmith(vec3 N, vec3 V, vec3 L, float roughness) {
    return geometrySchlickGGX(max(dot(N, V), 0.0), roughness) * geometrySchlickGGX(max(dot(N, L), 0.0), roughness);
}
vec3 fresnelSchlick(float cosTheta, vec3 F0, float F90) {
    return F0 + (vec3(F90) - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}
vec3 fresnelSchlickRoughness(float cosTheta, vec3 F0, float F90, float roughness) {
    return F0 + (max(vec3(1.0 - roughness) * F90, F0) - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}
struct GosxLayer { vec3 energy; float coat; float coatRoughness; };
vec2 gosxDFGApprox(float NoV, float roughness) {
    vec4 r = roughness * vec4(-1.0, -0.0275, -0.572, 0.022) + vec4(1.0, 0.0425, 1.04, -0.04);
    float a004 = min(r.x * r.x, exp2(-9.28 * NoV)) * r.x + r.y;
    return vec2(-1.04, 1.04) * a004 + r.zw;
}
vec2 gosxDFG(float NoV, float roughness) {
#if GOSX_HDR_IBL
    if (u_hasIBL) return textureLod(u_iblBRDFLUT, vec2(NoV, roughness), 0.0).rg;
#endif
    return gosxDFGApprox(NoV, roughness);
}
GosxLayer gosxLayerSetup(float NoV, float roughness, vec3 F0, float coat, float coatRoughness) {
    vec2 dfg = gosxDFG(NoV, roughness);
    return GosxLayer(1.0 + F0 * (1.0 / max(dfg.x + dfg.y, 0.05) - 1.0), clamp(coat, 0.0, 1.0), clamp(coatRoughness, 0.04, 1.0));
}
vec3 gosxMultiscatter(vec3 FssEss, vec3 F0, vec2 dfg) {
    float Ems = 1.0 - dfg.x - dfg.y;
    vec3 Favg = F0 + (1.0 - F0) / 21.0;
    return Ems * FssEss * Favg / (1.0 - Ems * Favg);
}
vec3 gosxDirectBRDF(vec3 N, vec3 V, vec3 L, vec3 albedo, float metalness, float roughness, vec3 F0, float F90, vec3 specF0, float specF90, float transmission, GosxLayer layer, out vec3 specular) {
    vec3 H = normalize(V + L);
    float NdotL = max(dot(N, L), 0.0);
    float VdotH = max(dot(H, V), 0.0);
    float denominator = 4.0 * max(dot(N, V), 0.0) * NdotL + 0.0001;
    specular = distributionGGX(N, H, roughness) * geometrySmith(N, V, L, roughness) * fresnelSchlick(VdotH, F0, F90) / denominator * layer.energy;
    vec3 Fdiel = fresnelSchlick(VdotH, specF0, specF90);
    float kD = (1.0 - max(Fdiel.x, max(Fdiel.y, Fdiel.z))) * (1.0 - metalness);
    vec3 base = kD * albedo * (1.0 - transmission) / PI + specular;
    if (layer.coat <= 0.0) return base;
    float Fc = (0.04 + 0.96 * pow(1.0 - VdotH, 5.0)) * layer.coat;
    vec3 coat = vec3(distributionGGX(N, H, layer.coatRoughness) * geometrySmith(N, V, L, layer.coatRoughness) * Fc / denominator);
    specular = specular * (1.0 - Fc) + coat;
    return base * (1.0 - Fc) + coat;
}
vec3 gosxCoatOver(vec3 base, vec3 coatRadiance, float NoV, GosxLayer layer, inout vec3 specular) {
    if (layer.coat <= 0.0) return base;
    vec2 dfg = gosxDFG(NoV, layer.coatRoughness);
    float Ec = (0.04 * dfg.x + dfg.y) * layer.coat;
    specular = specular * (1.0 - Ec) + coatRadiance * Ec;
    return base * (1.0 - Ec) + coatRadiance * Ec;
}
#if GOSX_HDR_IBL
vec3 gosxImageLight(vec3 N, vec3 V, float NoV, vec3 albedo, float metalness, float roughness, vec3 F0, float F90, vec3 specF0, float specF90, float transmission, GosxLayer layer, out vec3 specular) {
    vec3 Nr = rotateEnvY(N, u_envRotation);
    vec3 Rr = rotateEnvY(reflect(-V, N), u_envRotation);
    vec2 dfg = texture(u_iblBRDFLUT, vec2(NoV, roughness)).rg;
    vec3 irradiance = texture(u_iblIrradiance, Nr).rgb;
    vec3 FssEss = F0 * dfg.x + vec3(F90) * dfg.y;
    vec3 dielectric = specF0 * dfg.x + vec3(specF90) * dfg.y;
    dielectric += gosxMultiscatter(dielectric, specF0, dfg);
    float kD = (1.0 - max(dielectric.x, max(dielectric.y, dielectric.z))) * (1.0 - metalness);
    specular = textureLod(u_iblRadiance, Rr, roughness * u_iblRadianceMaxLod).rgb * FssEss + irradiance * gosxMultiscatter(FssEss, F0, dfg);
    vec3 color = irradiance * albedo * kD * (1.0 - transmission) + specular;
    if (layer.coat <= 0.0) return color;
    return gosxCoatOver(color, textureLod(u_iblRadiance, Rr, layer.coatRoughness * u_iblRadianceMaxLod).rgb, NoV, layer, specular);
}
#endif
`;
