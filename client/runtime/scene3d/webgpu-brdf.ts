// WebGPU standard-lighting BRDF, the twin of the WebGL2 BRDF module: keep
// their math identical. The GGX lobe, Smith visibility, Schlick Fresnel and
// the rect-area light moved here from the WebGPU PBR fragment, plus the
// multiple-scattering energy term (Fdez-Aguera 2019) and the clear coat
// layered over the base. The WebGL2 module documents the model.
//
// The rect-area diffuse term is the exact polygon form factor (three.js
// RE_Direct_RectArea_Physical with an identity LTC matrix); its specular uses
// a representative point instead of the fitted LTC tables, which this
// renderer does not upload. See the rect-area-specular cell in
// scene/capability/capability.go for the recorded gap.
//
// Samples use explicit LOD: a coat or an ambient branch can follow a
// per-pixel early return, which WGSL treats as non-uniform control flow.
const WGSL_BRDF = `
fn distributionGGX(N: vec3f, H: vec3f, roughness: f32) -> f32 {
    let a = roughness * roughness;
    let a2 = a * a;
    let NdotH = max(dot(N, H), 0.0);
    let NdotH2 = NdotH * NdotH;
    let denom = NdotH2 * (a2 - 1.0) + 1.0;
    return a2 / max(PI * denom * denom, 0.0000001);
}
fn geometrySchlickGGX(NdotV: f32, roughness: f32) -> f32 {
    let r = roughness + 1.0;
    let k = (r * r) / 8.0;
    return NdotV / (NdotV * (1.0 - k) + k);
}
fn geometrySmith(N: vec3f, V: vec3f, L: vec3f, roughness: f32) -> f32 {
    let NdotV = max(dot(N, V), 0.0);
    let NdotL = max(dot(N, L), 0.0);
    return geometrySchlickGGX(NdotV, roughness) * geometrySchlickGGX(NdotL, roughness);
}
fn fresnelSchlick(cosTheta: f32, F0: vec3f, F90: f32) -> vec3f {
    return F0 + (vec3f(F90) - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}
fn fresnelSchlickRoughness(cosTheta: f32, F0: vec3f, F90: f32, roughness: f32) -> vec3f {
    return F0 + (max(vec3f(1.0 - roughness) * F90, F0) - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);
}
fn ltcEdgeVectorFormFactor(v1: vec3f, v2: vec3f) -> vec3f {
    let x = dot(v1, v2);
    let y = abs(x);
    let a = 0.8543985 + (0.4965155 + 0.0145206 * y) * y;
    let b = 3.4175940 + (4.1616724 + y) * y;
    let v = a / b;
    var thetaSinTheta = 0.5 * inverseSqrt(max(1.0 - x * x, 0.0000001)) - v;
    if (x > 0.0) {
        thetaSinTheta = v;
    }
    return cross(v1, v2) * thetaSinTheta;
}
fn ltcClippedSphereFormFactor(f: vec3f) -> f32 {
    let l = length(f);
    return max((l * l + f.z) / (l + 1.0), 0.0);
}
fn rectAreaFormFactor(N: vec3f, P: vec3f, halfWidth: vec3f, halfHeight: vec3f, center: vec3f) -> f32 {
    let rect0 = center + halfWidth - halfHeight;
    let rect1 = center - halfWidth - halfHeight;
    let rect2 = center - halfWidth + halfHeight;
    let rect3 = center + halfWidth + halfHeight;
    let lightNormal = cross(rect1 - rect0, rect3 - rect0);
    if (dot(lightNormal, P - rect0) < 0.0) {
        return 0.0;
    }
    var helper = vec3f(0.0, 0.0, 1.0);
    if (abs(N.z) > 0.999) {
        helper = vec3f(1.0, 0.0, 0.0);
    }
    let T1 = normalize(cross(helper, N));
    let T2 = cross(N, T1);
    let basis = transpose(mat3x3f(T1, T2, N));
    let c0 = normalize(basis * (rect0 - P));
    let c1 = normalize(basis * (rect1 - P));
    let c2 = normalize(basis * (rect2 - P));
    let c3 = normalize(basis * (rect3 - P));
    var edges = ltcEdgeVectorFormFactor(c0, c1);
    edges = edges + ltcEdgeVectorFormFactor(c1, c2);
    edges = edges + ltcEdgeVectorFormFactor(c2, c3);
    edges = edges + ltcEdgeVectorFormFactor(c3, c0);
    return ltcClippedSphereFormFactor(edges);
}
fn rectAreaRepresentativePoint(P: vec3f, N: vec3f, V: vec3f, center: vec3f, halfWidth: vec3f, halfHeight: vec3f, planeNormal: vec3f) -> vec3f {
    let R = reflect(-V, N);
    var hit = center;
    let denom = dot(planeNormal, R);
    if (abs(denom) > 0.00001) {
        let t = dot(planeNormal, center - P) / denom;
        if (t > 0.0) {
            hit = P + R * t;
        }
    }
    let wLen = max(length(halfWidth), 0.00001);
    let hLen = max(length(halfHeight), 0.00001);
    let wDir = halfWidth / wLen;
    let hDir = halfHeight / hLen;
    let offset = hit - center;
    let u = clamp(dot(offset, wDir), -wLen, wLen);
    let v = clamp(dot(offset, hDir), -hLen, hLen);
    return center + wDir * u + hDir * v;
}
fn rectAreaLightRadiance(light: Light, P: vec3f, N: vec3f, V: vec3f, albedo: vec3f, roughness: f32, metalness: f32, F0: vec3f, F90: f32, NoV: f32) -> vec3f {
    let center = light.position.xyz;
    let halfWidth = light.areaHalfWidth.xyz;
    let halfHeight = light.areaHalfHeight.xyz;
    let formFactor = rectAreaFormFactor(N, P, halfWidth, halfHeight, center);
    if (formFactor <= 0.0) {
        return vec3f(0.0);
    }
    let radiance = light.color.rgb * light.direction.w;
    var out = albedo * (1.0 - metalness) * radiance * formFactor;
    let repPoint = rectAreaRepresentativePoint(P, N, V, center, halfWidth, halfHeight, light.direction.xyz);
    let toRep = repPoint - P;
    let repDist = length(toRep);
    let L = toRep / max(repDist, 0.0001);
    let NdotL = max(dot(N, L), 0.0);
    if (NdotL > 0.0) {
        let H = normalize(V + L);
        let D = distributionGGX(N, H, roughness);
        let G = geometrySmith(N, V, L, roughness);
        let F = fresnelSchlick(max(dot(H, V), 0.0), F0, F90);
        let brdf = (D * G * F) / (4.0 * NoV * NdotL + 0.0001);
        out = out + radiance * brdf * formFactor;
        gosxSpecularRadiance += radiance * brdf * formFactor;
    }
    return out;
}
struct GosxLayer { energy: vec3f, coat: f32, coatRoughness: f32 };
struct GosxLit { color: vec3f, specular: vec3f };
fn gosxDFGApprox(NoV: f32, roughness: f32) -> vec2f {
    let r = roughness * vec4f(-1.0, -0.0275, -0.572, 0.022) + vec4f(1.0, 0.0425, 1.04, -0.04);
    let a004 = min(r.x * r.x, exp2(-9.28 * NoV)) * r.x + r.y;
    return vec2f(-1.04, 1.04) * a004 + r.zw;
}
fn gosxDFG(NoV: f32, roughness: f32) -> vec2f {
    if (env.hasIBL != 0u) { return textureSampleLevel(iblBRDFLUT, iblSampler, vec2f(NoV, roughness), 0.0).rg; }
    return gosxDFGApprox(NoV, roughness);
}
fn gosxLayerSetup(NoV: f32, roughness: f32, F0: vec3f, coat: f32, coatRoughness: f32) -> GosxLayer {
    let dfg = gosxDFG(NoV, roughness);
    return GosxLayer(vec3f(1.0) + F0 * (1.0 / max(dfg.x + dfg.y, 0.05) - 1.0), clamp(coat, 0.0, 1.0), clamp(coatRoughness, 0.04, 1.0));
}
fn gosxMultiscatter(FssEss: vec3f, F0: vec3f, dfg: vec2f) -> vec3f {
    let Ems = 1.0 - dfg.x - dfg.y;
    let Favg = F0 + (vec3f(1.0) - F0) / 21.0;
    return Ems * FssEss * Favg / (vec3f(1.0) - Ems * Favg);
}
fn gosxDirectBRDF(N: vec3f, V: vec3f, L: vec3f, albedo: vec3f, metalness: f32, roughness: f32, F0: vec3f, F90: f32, specF0: vec3f, specF90: f32, transmission: f32, layer: GosxLayer) -> GosxLit {
    let H = normalize(V + L);
    let NdotL = max(dot(N, L), 0.0);
    let VdotH = max(dot(H, V), 0.0);
    let denominator = 4.0 * max(dot(N, V), 0.0) * NdotL + 0.0001;
    let specular = distributionGGX(N, H, roughness) * geometrySmith(N, V, L, roughness) * fresnelSchlick(VdotH, F0, F90) / denominator * layer.energy;
    let Fdiel = fresnelSchlick(VdotH, specF0, specF90);
    let kD = (1.0 - max(Fdiel.x, max(Fdiel.y, Fdiel.z))) * (1.0 - metalness);
    let base = kD * albedo * (1.0 - transmission) / PI + specular;
    if (layer.coat <= 0.0) { return GosxLit(base, specular); }
    let Fc = (0.04 + 0.96 * pow(1.0 - VdotH, 5.0)) * layer.coat;
    let coat = vec3f(distributionGGX(N, H, layer.coatRoughness) * geometrySmith(N, V, L, layer.coatRoughness) * Fc / denominator);
    return GosxLit(base * (1.0 - Fc) + coat, specular * (1.0 - Fc) + coat);
}
fn gosxCoatOver(base: GosxLit, coatRadiance: vec3f, NoV: f32, layer: GosxLayer) -> GosxLit {
    if (layer.coat <= 0.0) { return base; }
    let dfg = gosxDFG(NoV, layer.coatRoughness);
    let Ec = (0.04 * dfg.x + dfg.y) * layer.coat;
    return GosxLit(base.color * (1.0 - Ec) + coatRadiance * Ec, base.specular * (1.0 - Ec) + coatRadiance * Ec);
}
fn gosxImageLight(N: vec3f, V: vec3f, NoV: f32, albedo: vec3f, metalness: f32, roughness: f32, F0: vec3f, F90: f32, specF0: vec3f, specF90: f32, transmission: f32, layer: GosxLayer) -> GosxLit {
    let Nr = rotateEnvY(N, env.envRotation);
    let Rr = rotateEnvY(reflect(-V, N), env.envRotation);
    let maxLod = f32(max(env.radianceMipLevels, 1u) - 1u);
    let dfg = textureSampleLevel(iblBRDFLUT, iblSampler, vec2f(NoV, roughness), 0.0).rg;
    let irradiance = textureSampleLevel(iblIrradiance, iblSampler, Nr, 0.0).rgb;
    let FssEss = F0 * dfg.x + vec3f(F90) * dfg.y;
    var dielectric = specF0 * dfg.x + vec3f(specF90) * dfg.y;
    dielectric = dielectric + gosxMultiscatter(dielectric, specF0, dfg);
    let kD = (1.0 - max(dielectric.x, max(dielectric.y, dielectric.z))) * (1.0 - metalness);
    let specular = textureSampleLevel(iblRadiance, iblSampler, Rr, roughness * maxLod).rgb * FssEss + irradiance * gosxMultiscatter(FssEss, F0, dfg);
    let lit = GosxLit(irradiance * albedo * kD * (1.0 - transmission) + specular, specular);
    if (layer.coat <= 0.0) { return lit; }
    return gosxCoatOver(lit, textureSampleLevel(iblRadiance, iblSampler, Rr, layer.coatRoughness * maxLod).rgb, NoV, layer);
}
`;
