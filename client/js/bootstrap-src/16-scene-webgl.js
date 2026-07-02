  // PBR WebGL2 rendering backend for GoSX Scene3D.
  //
  // Provides Cook-Torrance BRDF with per-pixel lighting, replacing
  // the legacy vertex-color renderer for WebGL2-capable browsers.

  // Post-effect kind constants.
  var SCENE_POST_TONE_MAPPING = "toneMapping";
  var SCENE_POST_BLOOM = "bloom";
  var SCENE_POST_VIGNETTE = "vignette";
  var SCENE_POST_COLOR_GRADE = "colorGrade";
  var SCENE_POST_SSAO = "ssao";
  var SCENE_POST_DOF = "dof";
  var SCENE_POST_CUSTOM_POST = "customPost";

  // --- PBR Shader Sources ---

  const SCENE_PBR_VERTEX_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    "",
    "in vec3 a_position;",
    "in vec3 a_normal;",
    "in vec2 a_uv;",
    "in vec4 a_tangent;",
    "",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "",
    "out vec3 v_worldPosition;",
    "out vec3 v_normal;",
    "out vec2 v_uv;",
    "out vec3 v_tangent;",
    "out vec3 v_bitangent;",
    "out vec4 v_instanceColor;",
    "",
    "void gosxApplyCustomVertex(inout vec3 position, inout vec3 normal, inout vec2 uv) {}",
    "",
    "void main() {",
    "    vec3 gosxPosition = a_position;",
    "    vec3 gosxNormal = a_normal;",
    "    vec2 gosxUV = a_uv;",
    "    gosxApplyCustomVertex(gosxPosition, gosxNormal, gosxUV);",
    "    v_worldPosition = gosxPosition;",
    "    v_normal = normalize(gosxNormal);",
    "    v_uv = gosxUV;",
    "",
    "    vec3 T = normalize(a_tangent.xyz);",
    "    vec3 N = v_normal;",
    "    vec3 B = cross(N, T) * a_tangent.w;",
    "    v_tangent = T;",
    "    v_bitangent = B;",
    "    v_instanceColor = vec4(1.0);",
    "",
    "    gl_Position = u_projectionMatrix * u_viewMatrix * vec4(gosxPosition, 1.0);",
    "}",
  ].join("\n");

  const SCENE_PBR_FRAGMENT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    "",
    "in vec3 v_worldPosition;",
    "in vec3 v_normal;",
    "in vec2 v_uv;",
    "in vec3 v_tangent;",
    "in vec3 v_bitangent;",
    "in vec4 v_instanceColor;",
    "",
    "// Camera",
    "uniform vec3 u_cameraPosition;",
    "uniform mat4 u_viewMatrix;",
    "",
    "// Material",
    "uniform vec3 u_albedo;",
    "uniform float u_roughness;",
    "uniform float u_metalness;",
    "uniform float u_clearcoat;",
    "uniform float u_sheen;",
    "uniform float u_transmission;",
    "uniform float u_iridescence;",
    "uniform float u_anisotropy;",
    "uniform float u_emissive;",
    "uniform float u_opacity;",
    "uniform bool u_unlit;",
    "",
    "// Texture maps",
    "uniform sampler2D u_albedoMap;",
    "uniform sampler2D u_normalMap;",
    "uniform sampler2D u_roughnessMap;",
    "uniform sampler2D u_metalnessMap;",
    "uniform sampler2D u_emissiveMap;",
    "uniform bool u_hasAlbedoMap;",
    "uniform bool u_hasNormalMap;",
    "uniform bool u_hasRoughnessMap;",
    "uniform bool u_hasMetalnessMap;",
    "uniform bool u_hasEmissiveMap;",
    "",
    "// Lights (max 8)",
    "uniform int u_lightCount;",
    "uniform int u_lightTypes[8];",
    "uniform vec3 u_lightPositions[8];",
    "uniform vec3 u_lightDirections[8];",
    "uniform vec3 u_lightColors[8];",
    "uniform float u_lightIntensities[8];",
    "uniform float u_lightRanges[8];",
    "uniform float u_lightDecays[8];",
    "uniform float u_lightAngles[8];",
    "uniform float u_lightPenumbras[8];",
    "uniform vec3 u_lightGroundColors[8];",
    "",
    "// Environment",
    "uniform vec3 u_ambientColor;",
    "uniform float u_ambientIntensity;",
    "uniform vec3 u_skyColor;",
    "uniform float u_skyIntensity;",
    "uniform vec3 u_groundColor;",
    "uniform float u_groundIntensity;",
    "uniform sampler2D u_envMap;",
    "uniform bool u_hasEnvMap;",
    "uniform float u_envIntensity;",
    "uniform float u_envRotation;",
    "",
    "// Shadow maps (max 2 directional lights × up to 4 cascades each).",
    "// CSM: the renderer picks a cascade per fragment via view-space depth",
    "// against u_shadowCascadeSplits*[]. When u_shadowCascades* == 1 the",
    "// extra cascade samplers are bound to the same texture as cascade 0 and",
    "// the first branch always wins, matching the pre-CSM single-map path.",
    "uniform sampler2D u_shadowMap0_0;",
    "uniform sampler2D u_shadowMap0_1;",
    "uniform sampler2D u_shadowMap0_2;",
    "uniform sampler2D u_shadowMap0_3;",
    "uniform mat4 u_lightSpaceMatrices0[4];",
    "uniform float u_shadowCascadeSplits0[4];",
    "uniform int u_shadowCascades0;",
    "uniform bool u_hasShadow0;",
    "uniform float u_shadowBias0;",
    "uniform float u_shadowSoftness0;",
    "",
    "uniform sampler2D u_shadowMap1_0;",
    "uniform sampler2D u_shadowMap1_1;",
    "uniform sampler2D u_shadowMap1_2;",
    "uniform sampler2D u_shadowMap1_3;",
    "uniform mat4 u_lightSpaceMatrices1[4];",
    "uniform float u_shadowCascadeSplits1[4];",
    "uniform int u_shadowCascades1;",
    "uniform bool u_hasShadow1;",
    "uniform float u_shadowBias1;",
    "uniform float u_shadowSoftness1;",
    "",
    "// Per-object shadow receive control",
    "uniform bool u_receiveShadow;",
    "",
    "// Shadow-casting light indices — maps shadow slot to light array index.",
    "uniform int u_shadowLightIndex0;",
    "uniform int u_shadowLightIndex1;",
    "",
    "// Exposure and tone mapping control.",
    "uniform float u_exposure;",
    "uniform int u_toneMapMode;",
    "",
    "// Fog",
    "uniform int u_hasFog;",
    "uniform float u_fogDensity;",
    "uniform vec3 u_fogColor;",
    "",
    "out vec4 fragColor;",
    "",
    "const float PI = 3.14159265359;",
    "",
    "void gosxApplyCustomFragment(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv) {}",
    "",
    "// Poisson disk taps used for both the PCSS blocker search and the final",
    "// PCF filter. 8 taps provide a stable penumbra estimate without pushing",
    "// WebGL2 fragment register pressure too high.",
    "const vec2 kPoissonDisk8[8] = vec2[](",
    "    vec2(-0.94201624, -0.39906216),",
    "    vec2( 0.94558609, -0.76890725),",
    "    vec2(-0.09418410, -0.92938870),",
    "    vec2( 0.34495938,  0.29387760),",
    "    vec2(-0.91588581,  0.45771432),",
    "    vec2(-0.81544232, -0.87912464),",
    "    vec2( 0.38277543,  0.27676845),",
    "    vec2( 0.97484398,  0.75648379)",
    ");",
    "",
    "// PCSS (Percentage-Closer Soft Shadows) with PCF fallback.",
    "// The softness parameter is interpreted as a combined (light-size /",
    "// world-units) hint: when > 0 we do a blocker search, estimate the",
    "// penumbra from the receiver-to-blocker distance, and then PCF with a",
    "// filter radius scaled to the penumbra. When softness is 0 we skip the",
    "// extra samples and just return a hard comparison.",
    "float shadowFactor(sampler2D shadowMap, mat4 lightSpaceMatrix, float bias, float softness) {",
    "    vec4 lightSpacePos = lightSpaceMatrix * vec4(v_worldPosition, 1.0);",
    "    vec3 projCoords = lightSpacePos.xyz / lightSpacePos.w;",
    "    projCoords = projCoords * 0.5 + 0.5;",
    "",
    "    if (projCoords.z > 1.0) return 1.0;",
    "",
    "    float receiverDepth = projCoords.z;",
    "    float texelSize = 1.0 / float(textureSize(shadowMap, 0).x);",
    "",
    "    // Hard-shadow fast path.",
    "    if (softness <= 0.0001) {",
    "        float depth = texture(shadowMap, projCoords.xy).r;",
    "        return (receiverDepth - bias > depth) ? 0.0 : 1.0;",
    "    }",
    "",
    "    // --- Blocker search ---",
    "    // Sample a disk sized by the light's shadow-space \"size\" (softness)",
    "    // and average the depth of taps that are behind the receiver. These",
    "    // are the occluders contributing to the penumbra.",
    "    float blockerRadius = max(1.0, softness * 32.0);",
    "    float blockerDepthSum = 0.0;",
    "    float blockerCount = 0.0;",
    "    for (int i = 0; i < 8; i++) {",
    "        vec2 offset = kPoissonDisk8[i] * texelSize * blockerRadius;",
    "        float d = texture(shadowMap, projCoords.xy + offset).r;",
    "        if (receiverDepth - bias > d) {",
    "            blockerDepthSum += d;",
    "            blockerCount += 1.0;",
    "        }",
    "    }",
    "",
    "    // No blockers: fully lit.",
    "    if (blockerCount < 0.5) {",
    "        return 1.0;",
    "    }",
    "",
    "    float avgBlockerDepth = blockerDepthSum / blockerCount;",
    "",
    "    // --- Penumbra estimate ---",
    "    // penumbra = (receiver - blocker) / blocker * lightSize.",
    "    // Guard blocker against zero to avoid division by near-plane epsilon.",
    "    float penumbra = (receiverDepth - avgBlockerDepth) * softness / max(avgBlockerDepth, 1e-4);",
    "",
    "    // --- PCF with penumbra-scaled radius ---",
    "    float filterRadius = max(1.0, clamp(penumbra * 128.0, 1.0, softness * 96.0));",
    "    float shadow = 0.0;",
    "    for (int i = 0; i < 8; i++) {",
    "        vec2 offset = kPoissonDisk8[i] * texelSize * filterRadius;",
    "        float d = texture(shadowMap, projCoords.xy + offset).r;",
    "        shadow += (receiverDepth - bias > d) ? 0.0 : 1.0;",
    "    }",
    "    return shadow / 8.0;",
    "}",
    "",
    "// Cascaded-shadow dispatchers for up to 4 cascades per slot.",
    "// View-space positive depth is compared against u_shadowCascadeSplits*[c],",
    "// where split[c] is the far plane of cascade c. Sampler selection is",
    "// unrolled because GLSL ES 3.00 disallows dynamic uniform-int indexing of",
    "// sampler arrays; mat4 and float arrays are indexed normally.",
    "float shadowFactorSlot0(float viewDepth) {",
    "    int c = 0;",
    "    if (u_shadowCascades0 >= 2 && viewDepth >= u_shadowCascadeSplits0[0]) c = 1;",
    "    if (u_shadowCascades0 >= 3 && viewDepth >= u_shadowCascadeSplits0[1]) c = 2;",
    "    if (u_shadowCascades0 >= 4 && viewDepth >= u_shadowCascadeSplits0[2]) c = 3;",
    "    mat4 ls = u_lightSpaceMatrices0[c];",
    "    if (c == 1) return shadowFactor(u_shadowMap0_1, ls, u_shadowBias0, u_shadowSoftness0);",
    "    if (c == 2) return shadowFactor(u_shadowMap0_2, ls, u_shadowBias0, u_shadowSoftness0);",
    "    if (c == 3) return shadowFactor(u_shadowMap0_3, ls, u_shadowBias0, u_shadowSoftness0);",
    "    return shadowFactor(u_shadowMap0_0, ls, u_shadowBias0, u_shadowSoftness0);",
    "}",
    "",
    "float shadowFactorSlot1(float viewDepth) {",
    "    int c = 0;",
    "    if (u_shadowCascades1 >= 2 && viewDepth >= u_shadowCascadeSplits1[0]) c = 1;",
    "    if (u_shadowCascades1 >= 3 && viewDepth >= u_shadowCascadeSplits1[1]) c = 2;",
    "    if (u_shadowCascades1 >= 4 && viewDepth >= u_shadowCascadeSplits1[2]) c = 3;",
    "    mat4 ls = u_lightSpaceMatrices1[c];",
    "    if (c == 1) return shadowFactor(u_shadowMap1_1, ls, u_shadowBias1, u_shadowSoftness1);",
    "    if (c == 2) return shadowFactor(u_shadowMap1_2, ls, u_shadowBias1, u_shadowSoftness1);",
    "    if (c == 3) return shadowFactor(u_shadowMap1_3, ls, u_shadowBias1, u_shadowSoftness1);",
    "    return shadowFactor(u_shadowMap1_0, ls, u_shadowBias1, u_shadowSoftness1);",
    "}",
    "",
    "// GGX/Trowbridge-Reitz normal distribution function.",
    "float distributionGGX(vec3 N, vec3 H, float roughness) {",
    "    float a = roughness * roughness;",
    "    float a2 = a * a;",
    "    float NdotH = max(dot(N, H), 0.0);",
    "    float NdotH2 = NdotH * NdotH;",
    "    float denom = NdotH2 * (a2 - 1.0) + 1.0;",
    "    denom = PI * denom * denom;",
    "    return a2 / max(denom, 0.0000001);",
    "}",
    "",
    "// Smith geometry function (GGX variant) — single direction.",
    "float geometrySchlickGGX(float NdotV, float roughness) {",
    "    float r = roughness + 1.0;",
    "    float k = (r * r) / 8.0;",
    "    return NdotV / (NdotV * (1.0 - k) + k);",
    "}",
    "",
    "// Smith geometry function — combined for view and light directions.",
    "float geometrySmith(vec3 N, vec3 V, vec3 L, float roughness) {",
    "    float NdotV = max(dot(N, V), 0.0);",
    "    float NdotL = max(dot(N, L), 0.0);",
    "    return geometrySchlickGGX(NdotV, roughness) * geometrySchlickGGX(NdotL, roughness);",
    "}",
    "",
    "// Schlick fresnel approximation.",
    "vec3 fresnelSchlick(float cosTheta, vec3 F0) {",
    "    return F0 + (1.0 - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);",
    "}",
    "",
    "vec3 fresnelSchlickRoughness(float cosTheta, vec3 F0, float roughness) {",
    "    return F0 + (max(vec3(1.0 - roughness), F0) - F0) * pow(clamp(1.0 - cosTheta, 0.0, 1.0), 5.0);",
    "}",
    "",
    "vec3 rotateEnvY(vec3 dir, float radians) {",
    "    float c = cos(radians);",
    "    float s = sin(radians);",
    "    return vec3(dir.x * c + dir.z * s, dir.y, -dir.x * s + dir.z * c);",
    "}",
    "",
    "vec2 envEquirectUV(vec3 dir) {",
    "    vec3 d = normalize(dir);",
    "    return vec2(atan(d.z, d.x) / (2.0 * PI) + 0.5, asin(clamp(d.y, -1.0, 1.0)) / PI + 0.5);",
    "}",
    "",
    "// Point light distance attenuation.",
    "float pointLightAttenuation(float distance, float range, float decay) {",
    "    if (range > 0.0) {",
    "        float ratio = clamp(1.0 - pow(distance / range, 4.0), 0.0, 1.0);",
    "        return ratio * ratio / max(distance * distance, 0.0001);",
    "    }",
    "    return 1.0 / max(pow(distance, decay), 0.0001);",
    "}",
    "",
    "void main() {",
    "    // Resolve material properties, sampling textures when available.",
    "    vec3 albedo = u_albedo;",
    "    albedo *= v_instanceColor.rgb;",
    "    if (u_hasAlbedoMap) {",
    "        vec4 texAlbedo = texture(u_albedoMap, v_uv);",
    "        albedo *= texAlbedo.rgb;",
    "    }",
    "",
    "    float roughness = u_roughness;",
    "    if (u_hasRoughnessMap) {",
    "        roughness *= texture(u_roughnessMap, v_uv).g;",
    "    }",
    "    roughness = clamp(roughness, 0.04, 1.0);",
    "    roughness = clamp(roughness * (1.0 - abs(u_anisotropy) * 0.28), 0.04, 1.0);",
    "",
    "    float metalness = u_metalness;",
    "    if (u_hasMetalnessMap) {",
    "        metalness *= texture(u_metalnessMap, v_uv).b;",
    "    }",
    "    metalness = clamp(metalness, 0.0, 1.0);",
    "",
    "    float emissiveStrength = u_emissive;",
    "    vec3 emissiveColor = albedo;",
    "    if (u_hasEmissiveMap) {",
    "        emissiveColor = texture(u_emissiveMap, v_uv).rgb;",
    "    }",
    "",
    "    // Unlit path: output albedo directly.",
    "    if (u_unlit) {",
    "        vec3 color = albedo + emissiveColor * emissiveStrength;",
    "        float opacity = u_opacity;",
    "        gosxApplyCustomFragment(color, opacity, normalize(v_normal), v_worldPosition, v_uv);",
    "        fragColor = vec4(color, opacity * v_instanceColor.a);",
    "        return;",
    "    }",
    "",
    "    // Resolve per-pixel normal via TBN matrix.",
    "    vec3 N = normalize(v_normal);",
    "    if (u_hasNormalMap) {",
    "        vec3 T = normalize(v_tangent);",
    "        vec3 B = normalize(v_bitangent);",
    "        mat3 TBN = mat3(T, B, N);",
    "        vec3 mapNormal = texture(u_normalMap, v_uv).rgb * 2.0 - 1.0;",
    "        N = normalize(TBN * mapNormal);",
    "    }",
    "",
    "    vec3 V = normalize(u_cameraPosition - v_worldPosition);",
    "    float NoV = max(dot(N, V), 0.0);",
    "",
    "    // Fresnel reflectance at normal incidence — dielectric vs metallic blend.",
    "    vec3 F0 = mix(vec3(0.04), albedo, metalness);",
    "",
    "    // Accumulate direct lighting.",
    "    vec3 Lo = vec3(0.0);",
    "",
    "    // View-space positive depth of this fragment — used to pick a cascade",
    "    // in shadowFactorSlot*(). Light-space transforms already happen per",
    "    // cascade inside shadowFactor(); this extra multiply per fragment is",
    "    // cheap and isolates CSM logic to the fragment shader.",
    "    float viewDepth = -(u_viewMatrix * vec4(v_worldPosition, 1.0)).z;",
    "",
    "    for (int i = 0; i < 8; i++) {",
    "        if (i >= u_lightCount) break;",
    "",
    "        int lightType = u_lightTypes[i];",
    "        vec3 lightColor = u_lightColors[i];",
    "        float intensity = u_lightIntensities[i];",
    "",
    "        // Ambient light (type 0): add flat contribution, no BRDF.",
    "        if (lightType == 0) {",
    "            Lo += albedo * lightColor * intensity;",
    "            continue;",
    "        }",
    "",
    "        // Hemisphere light (type 4): sky/ground blend based on normal Y.",
    "        if (lightType == 4) {",
    "            float hBlend = N.y * 0.5 + 0.5;",
    "            vec3 hemiColor = mix(u_lightGroundColors[i], lightColor, hBlend);",
    "            Lo += albedo * hemiColor * intensity;",
    "            continue;",
    "        }",
    "",
    "        vec3 L;",
    "        float attenuation = 1.0;",
    "",
    "        if (lightType == 1) {",
    "            // Directional light.",
    "            L = normalize(-u_lightDirections[i]);",
    "        } else if (lightType == 3) {",
    "            // Spot light.",
    "            vec3 toLight = u_lightPositions[i] - v_worldPosition;",
    "            float dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "",
    "            // Cone attenuation.",
    "            float cosAngle = dot(L, -normalize(u_lightDirections[i]));",
    "            float outerCos = cos(u_lightAngles[i]);",
    "            float innerCos = cos(u_lightAngles[i] * (1.0 - u_lightPenumbras[i]));",
    "            float spotAtten = clamp((cosAngle - outerCos) / max(innerCos - outerCos, 0.001), 0.0, 1.0);",
    "",
    "            // Distance attenuation (same as point light).",
    "            attenuation = pointLightAttenuation(dist, u_lightRanges[i], u_lightDecays[i]) * spotAtten;",
    "        } else {",
    "            // Point light (type 2).",
    "            vec3 toLight = u_lightPositions[i] - v_worldPosition;",
    "            float dist = length(toLight);",
    "            L = toLight / max(dist, 0.0001);",
    "            attenuation = pointLightAttenuation(dist, u_lightRanges[i], u_lightDecays[i]);",
    "        }",
    "",
    "        vec3 H = normalize(V + L);",
    "        float NdotL = max(dot(N, L), 0.0);",
    "",
    "        // Cook-Torrance specular BRDF.",
    "        float D = distributionGGX(N, H, roughness);",
    "        float G = geometrySmith(N, V, L, roughness);",
    "        vec3 F = fresnelSchlick(max(dot(H, V), 0.0), F0);",
    "",
    "        vec3 numerator = D * G * F;",
    "        float denominator = 4.0 * max(dot(N, V), 0.0) * NdotL + 0.0001;",
    "        vec3 specular = numerator / denominator;",
    "",
    "        // Energy conservation: diffuse complement of specular.",
    "        vec3 kD = (vec3(1.0) - F) * (1.0 - metalness);",
    "",
    "        // Shadow attenuation for directional lights.",
    "        float shadow = 1.0;",
    "        if (u_receiveShadow && lightType == 1) {",
    "            if (u_hasShadow0 && i == u_shadowLightIndex0) {",
    "                shadow = shadowFactorSlot0(viewDepth);",
    "            } else if (u_hasShadow1 && i == u_shadowLightIndex1) {",
    "                shadow = shadowFactorSlot1(viewDepth);",
    "            }",
    "        }",
    "",
    "        vec3 radiance = lightColor * intensity * attenuation;",
    "        Lo += (kD * albedo / PI + specular) * radiance * NdotL * shadow;",
    "    }",
    "",
    "    // Environment lighting: equirectangular envMap when loaded, hemisphere fallback otherwise.",
    "    vec3 ambient;",
    "    if (u_hasEnvMap) {",
    "        vec3 Nr = rotateEnvY(N, u_envRotation);",
    "        vec3 Rr = rotateEnvY(reflect(-V, N), u_envRotation);",
    "        vec3 envDiffuse = texture(u_envMap, envEquirectUV(Nr)).rgb * albedo;",
    "        vec3 envSpecular = texture(u_envMap, envEquirectUV(Rr)).rgb;",
    "        vec3 Fenv = fresnelSchlickRoughness(max(dot(N, V), 0.0), F0, roughness);",
    "        vec3 kDenv = (vec3(1.0) - Fenv) * (1.0 - metalness);",
    "        ambient = (kDenv * envDiffuse + envSpecular * Fenv * (1.0 - roughness * 0.65)) * u_envIntensity;",
    "    } else {",
    "        float hemi = N.y * 0.5 + 0.5;",
    "        vec3 envDiffuse = u_ambientColor * u_ambientIntensity",
    "                        + u_skyColor * u_skyIntensity * hemi",
    "                        + u_groundColor * u_groundIntensity * (1.0 - hemi);",
    "        ambient = envDiffuse * albedo;",
    "    }",
    "",
    "    // Emissive contribution.",
    "    vec3 emission = emissiveColor * emissiveStrength;",
    "",
    "    vec3 color = ambient + Lo + emission;",
    "",
    "    float clearcoat = clamp(u_clearcoat, 0.0, 1.0);",
    "    if (clearcoat > 0.0001) {",
    "        float cc = pow(NoV, mix(12.0, 96.0, 1.0 - roughness)) * clearcoat;",
    "        color += vec3(cc * 0.28);",
    "    }",
    "",
    "    float sheen = clamp(u_sheen, 0.0, 1.0);",
    "    if (sheen > 0.0001) {",
    "        float velvet = pow(1.0 - NoV, 3.0) * sheen;",
    "        color += albedo * velvet * 0.55;",
    "    }",
    "",
    "    float iridescence = clamp(u_iridescence, 0.0, 1.0);",
    "    if (iridescence > 0.0001) {",
    "        vec3 iri = 0.5 + 0.5 * cos(vec3(0.0, 2.1, 4.2) + NoV * 8.0);",
    "        color = mix(color, color * (0.65 + iri * 0.7), iridescence * pow(1.0 - NoV, 2.0));",
    "    }",
    "",
    "    float transmission = clamp(u_transmission, 0.0, 1.0) * (1.0 - metalness);",
    "    if (transmission > 0.0001) {",
    "        color = mix(color, ambient + albedo * 0.1, transmission * 0.55);",
    "    }",
    "",
    "    // Exponential fog.",
    "    if (u_hasFog != 0) {",
    "        float fogDist = length(v_worldPosition - u_cameraPosition);",
    "        float fogFactor = exp(-u_fogDensity * u_fogDensity * fogDist * fogDist);",
    "        fogFactor = clamp(fogFactor, 0.0, 1.0);",
    "        color = mix(u_fogColor, color, fogFactor);",
    "    }",
    "",
    "    // Apply exposure.",
    "    color *= u_exposure;",
    "",
    "    // Tone mapping.",
    "    if (u_toneMapMode == 1) {",
    "        // ACES filmic.",
    "        color = (color * (2.51 * color + 0.03)) / (color * (2.43 * color + 0.59) + 0.14);",
    "    } else if (u_toneMapMode == 2) {",
    "        // Reinhard.",
    "        color = color / (color + vec3(1.0));",
    "    }",
    "    // else: linear (no tone mapping).",
    "",
    "    // Gamma correction.",
    "    color = pow(color, vec3(1.0 / 2.2));",
    "",
    "    float opacity = u_opacity;",
    "    gosxApplyCustomFragment(color, opacity, N, v_worldPosition, v_uv);",
    "    fragColor = vec4(color, opacity * v_instanceColor.a);",
    "}",
  ].join("\n");

  // --- Shadow Depth Shader Sources ---

  const SCENE_SHADOW_VERTEX_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec3 a_position;",
    "uniform mat4 u_lightViewProjection;",
    "void main() {",
    "    gl_Position = u_lightViewProjection * vec4(a_position, 1.0);",
    "}",
  ].join("\n");

  const SCENE_SHADOW_FRAGMENT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "void main() {}",
  ].join("\n");

  // --- Points Shader Sources ---

  const SCENE_POINTS_VERTEX_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    "",
    "in vec3 a_position;",
    "in float a_size;",
    "in vec4 a_color;",
    "",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "uniform mat4 u_modelMatrix;",
    "uniform float u_defaultSize;",
    "uniform vec4 u_defaultColor;",
    "uniform bool u_hasPerVertexColor;",
    "uniform bool u_hasPerVertexSize;",
    "uniform bool u_sizeAttenuation;",
    "uniform int u_pointStyle;",
    "uniform float u_viewportHeight;",
    "uniform float u_minPixelSize;",
    "uniform float u_maxPixelSize;",
    "",
    "// Fog",
    "uniform int u_hasFog;",
    "uniform float u_fogDensity;",
    "",
    "out vec4 v_color;",
    "out float v_fogFactor;",
    "out float v_pointSize;",
    "",
    "void main() {",
    "    vec4 worldPos = u_modelMatrix * vec4(a_position, 1.0);",
    "    vec4 viewPos = u_viewMatrix * worldPos;",
    "    gl_Position = u_projectionMatrix * viewPos;",
    "",
    "    float size = u_hasPerVertexSize ? a_size : u_defaultSize;",
    "    float pixelSize;",
    "    if (u_sizeAttenuation) {",
    "        pixelSize = max(size * (u_viewportHeight * 0.5) / max(-viewPos.z, 0.001), 1.0);",
    "    } else {",
    "        pixelSize = max(size, 1.0);",
    "    }",
    "    float minPixelSize = max(u_minPixelSize, 0.0);",
    "    if (minPixelSize > 0.0) {",
    "        pixelSize = max(pixelSize, minPixelSize);",
    "    }",
    "    if (u_maxPixelSize > 0.0) {",
    "        pixelSize = min(pixelSize, u_maxPixelSize);",
    "    }",
    "    gl_PointSize = pixelSize;",
    "    v_pointSize = gl_PointSize;",
    "",
    "    v_color = u_hasPerVertexColor ? a_color : u_defaultColor;",
    "",
    "    // Exponential fog",
    "    if (u_hasFog != 0) {",
    "        float dist = length(viewPos.xyz);",
    "        v_fogFactor = exp(-u_fogDensity * u_fogDensity * dist * dist);",
    "        v_fogFactor = clamp(v_fogFactor, 0.0, 1.0);",
    "    } else {",
    "        v_fogFactor = 1.0;",
    "    }",
    "}",
  ].join("\n");

  const SCENE_POINTS_FRAGMENT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    "",
    "in vec4 v_color;",
    "in float v_fogFactor;",
    "in float v_pointSize;",
    "",
    "uniform float u_opacity;",
    "uniform vec3 u_fogColor;",
    "uniform int u_hasFog;",
    "uniform int u_pointStyle;",
    "",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    float alpha = 1.0;",
    "    vec3 color = v_color.rgb;",
    "    if (u_pointStyle == 1) {",
    "        vec2 centered = gl_PointCoord - vec2(0.5);",
    "        float radial = length(centered);",
    "        float square = max(abs(centered.x), abs(centered.y));",
    "        float focus = clamp((v_pointSize - 1.0) / 10.0, 0.0, 1.0);",
    "        float coreRadius = mix(0.49, 0.18, focus);",
    "        float core = 1.0 - smoothstep(coreRadius, coreRadius + 0.05, square);",
    "        float halo = (1.0 - smoothstep(0.12, 0.72, radial)) * focus;",
    "        float streakX = 1.0 - smoothstep(0.02, 0.16, abs(centered.x));",
    "        float streakY = 1.0 - smoothstep(0.02, 0.16, abs(centered.y));",
    "        float streak = max(streakX, streakY) * focus;",
    "        alpha = clamp(core + halo * 0.5 + streak * 0.2, 0.0, 1.0);",
    "        color = mix(color, vec3(1.0), clamp(focus * 0.22 + core * focus * 0.28, 0.0, 0.4));",
    "    } else if (u_pointStyle == 2) {",
    "        // glow: pure radial gaussian falloff — soft gas cloud, no core, no streaks",
    "        vec2 centered = gl_PointCoord - vec2(0.5);",
    "        float radial = length(centered) * 2.0;",
    "        if (radial > 1.0) discard;",
    "        float g = exp(-radial * radial * 3.5);",
    "        alpha = g;",
    "    }",
    "",
    "    // Apply fog",
    "    if (u_hasFog != 0) {",
    "        color = mix(u_fogColor, color, v_fogFactor);",
    "    }",
    "",
    "    fragColor = vec4(color, alpha * v_color.a * u_opacity);",
    "}",
  ].join("\n");

  // --- Instanced PBR Vertex Shader ---
  //
  // Variant of the PBR vertex shader for instanced rendering.
  // Reads a per-instance mat4 transform from attributes 4-7 (with divisor 1).
  // Attribute layout:
  //   0: a_position  (vec3)
  //   1: a_normal    (vec3)
  //   2: a_uv        (vec2)
  //   3: a_tangent   (vec4)
  //   4-7: a_instanceMatrix (mat4, 4 × vec4 with divisor 1)
  //   8: a_instanceColor (vec4 with divisor 1, optional)

	  const SCENE_PBR_INSTANCED_VERTEX_SOURCE = [
	    "#version 300 es",
	    "precision highp float;",
	    "precision highp int;",
	    "",
    "in vec3 a_position;",
    "in vec3 a_normal;",
    "in vec2 a_uv;",
    "in vec4 a_tangent;",
    "in mat4 a_instanceMatrix;",
    "in vec4 a_instanceColor;",
    "",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "uniform bool u_hasInstanceColor;",
    "",
    "out vec3 v_worldPosition;",
    "out vec3 v_normal;",
    "out vec2 v_uv;",
    "out vec3 v_tangent;",
    "out vec3 v_bitangent;",
    "out vec4 v_instanceColor;",
    "",
    "void main() {",
    "    vec4 worldPos = a_instanceMatrix * vec4(a_position, 1.0);",
    "    v_worldPosition = worldPos.xyz;",
    "    mat3 normalMatrix = mat3(a_instanceMatrix);",
    "    v_normal = normalize(normalMatrix * a_normal);",
    "    v_uv = a_uv;",
    "    vec3 T = normalize(normalMatrix * a_tangent.xyz);",
    "    vec3 N = v_normal;",
    "    v_bitangent = cross(N, T) * a_tangent.w;",
    "    v_tangent = T;",
    "    v_instanceColor = u_hasInstanceColor ? a_instanceColor : vec4(1.0);",
    "    gl_Position = u_projectionMatrix * u_viewMatrix * worldPos;",
    "}",
  ].join("\n");

  // --- Skinned PBR Vertex Shader ---
  //
  // Variant of the PBR vertex shader with skeletal animation (vertex skinning).
  // Adds a_joints / a_weights attributes and u_jointMatrices uniform array.
  // When u_hasSkin is false, the shader behaves identically to the static PBR
  // vertex shader so it can be used as a universal fallback.

	  const SCENE_PBR_SKINNED_VERTEX_SOURCE = [
	    "#version 300 es",
	    "precision highp float;",
	    "precision highp int;",
	    "",
    "in vec3 a_position;",
    "in vec3 a_normal;",
    "in vec2 a_uv;",
    "in vec4 a_tangent;",
    "in vec4 a_joints;",
    "in vec4 a_weights;",
    "",
	    "uniform mat4 u_viewMatrix;",
	    "uniform mat4 u_projectionMatrix;",
	    "uniform mat4 u_modelMatrix;",
	    "uniform mat4 u_jointMatrices[64];",
	    "uniform bool u_hasSkin;",
    "",
    "out vec3 v_worldPosition;",
    "out vec3 v_normal;",
    "out vec2 v_uv;",
    "out vec3 v_tangent;",
    "out vec3 v_bitangent;",
    "out vec4 v_instanceColor;",
    "",
    "void main() {",
    "    vec4 pos = vec4(a_position, 1.0);",
    "    vec3 norm = a_normal;",
    "    vec3 tang = a_tangent.xyz;",
    "",
    "    if (u_hasSkin) {",
    "        mat4 skinMatrix =",
    "            a_weights.x * u_jointMatrices[int(a_joints.x)] +",
    "            a_weights.y * u_jointMatrices[int(a_joints.y)] +",
    "            a_weights.z * u_jointMatrices[int(a_joints.z)] +",
    "            a_weights.w * u_jointMatrices[int(a_joints.w)];",
    "",
    "        pos = skinMatrix * pos;",
    "        norm = mat3(skinMatrix) * norm;",
    "        tang = mat3(skinMatrix) * tang;",
    "    }",
    "",
	    "    vec4 worldPos = u_modelMatrix * pos;",
	    "    mat3 normalMatrix = mat3(u_modelMatrix);",
	    "    v_worldPosition = worldPos.xyz;",
	    "    v_normal = normalize(normalMatrix * norm);",
	    "    v_uv = a_uv;",
	    "",
	    "    vec3 T = normalize(normalMatrix * tang);",
	    "    vec3 N = v_normal;",
    "    vec3 B = cross(N, T) * a_tangent.w;",
    "    v_tangent = T;",
    "    v_bitangent = B;",
    "    v_instanceColor = vec4(1.0);",
    "",
	    "    gl_Position = u_projectionMatrix * u_viewMatrix * worldPos;",
    "}",
  ].join("\n");

  // --- Matrix Helpers ---

  // Build a 4x4 view matrix from camera position and Euler rotation.
  //
  // The GoSX camera convention: the camera has position (x, y, z) and Euler
  // angles (rotationX, rotationY, rotationZ). The existing renderer shifts
  // world points by (-camX, -camY, +camZ) then applies inverse rotation.
  // The +camZ convention means the camera Z is negated in world space —
  // the camera at Z=6 looks toward the origin along -Z.
  //
  // To produce a standard 4x4 view matrix we construct:
  //   V = inverseRotation * translation(-camX, -camY, +camZ)
  //
  // The inverse rotation is computed by applying -rotZ, -rotY, -rotX
  // (reverse order, negative angles) — matching sceneInverseRotatePoint.
  // Build a 4x4 view matrix into `out` (or a new Float32Array if omitted).
  function scenePBRViewMatrix(camera, out) {
    const cam = sceneRenderCamera(camera);
    const tx = -cam.x;
    const ty = -cam.y;
    const tz = -cam.z;

    // Inverse Euler: apply -rotZ, then -rotY, then -rotX.
    const sx = Math.sin(-cam.rotationX);
    const cx = Math.cos(-cam.rotationX);
    const sy = Math.sin(-cam.rotationY);
    const cy = Math.cos(-cam.rotationY);
    const sz = Math.sin(-cam.rotationZ);
    const cz = Math.cos(-cam.rotationZ);

    // Rotation matrix = Rx(-rx) * Ry(-ry) * Rz(-rz)
    // Column-major order for WebGL.
    const r00 = cy * cz;
    const r01 = cy * sz;
    const r02 = -sy;

    const r10 = sx * sy * cz - cx * sz;
    const r11 = sx * sy * sz + cx * cz;
    const r12 = sx * cy;

    const r20 = cx * sy * cz + sx * sz;
    const r21 = cx * sy * sz - sx * cz;
    const r22 = cx * cy;

    // Translation part: R * t
    const d0 = r00 * tx + r01 * ty + r02 * tz;
    const d1 = r10 * tx + r11 * ty + r12 * tz;
    const d2 = r20 * tx + r21 * ty + r22 * tz;

    // Column-major 4x4 matrix as Float32Array.
    var m = out || new Float32Array(16);
    m[0] = r00; m[1] = r10; m[2] = r20; m[3] = 0;
    m[4] = r01; m[5] = r11; m[6] = r21; m[7] = 0;
    m[8] = r02; m[9] = r12; m[10] = r22; m[11] = 0;
    m[12] = d0; m[13] = d1; m[14] = d2; m[15] = 1;
    return m;
  }

  // Build a perspective projection matrix into `out` (or a new Float32Array).
  // fov is in degrees, matching sceneRenderCamera output.
  function scenePBRProjectionMatrix(fov, aspect, near, far, out) {
    const fovRad = (fov * Math.PI) / 180;
    const f = 1 / Math.tan(fovRad * 0.5);
    const rangeInv = 1 / (near - far);

    // Column-major.
    var m = out || new Float32Array(16);
    m[0] = f / aspect; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = f; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = (near + far) * rangeInv; m[11] = -1;
    m[12] = 0; m[13] = 0; m[14] = 2 * near * far * rangeInv; m[15] = 0;
    return m;
  }

  function scenePBROrthographicProjectionMatrix(left, right, top, bottom, near, far, out) {
    var m = out || new Float32Array(16);
    const width = Math.max(0.000001, right - left);
    const height = Math.max(0.000001, top - bottom);
    const depth = Math.max(0.000001, far - near);
    m[0] = 2 / width; m[1] = 0; m[2] = 0; m[3] = 0;
    m[4] = 0; m[5] = 2 / height; m[6] = 0; m[7] = 0;
    m[8] = 0; m[9] = 0; m[10] = -2 / depth; m[11] = 0;
    m[12] = -(right + left) / width; m[13] = -(top + bottom) / height; m[14] = -(far + near) / depth; m[15] = 1;
    return m;
  }

  function scenePBRProjectionMatrixForCamera(camera, aspect, out) {
    const cam = sceneRenderCamera(camera);
    if (cam.kind === "orthographic") {
      const bounds = sceneOrthographicBounds(cam, Math.max(1, aspect * 1000), 1000);
      return scenePBROrthographicProjectionMatrix(bounds.left, bounds.right, bounds.top, bounds.bottom, cam.near, cam.far, out);
    }
    return scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, out);
  }

  // --- Shadow Map Infrastructure ---

  // Create a framebuffer with a depth-only texture for shadow mapping.
  function createSceneShadowResources(gl, size) {
    const framebuffer = gl.createFramebuffer();
    const depthTexture = gl.createTexture();

    gl.bindTexture(gl.TEXTURE_2D, depthTexture);
    gl.texImage2D(
      gl.TEXTURE_2D, 0, gl.DEPTH_COMPONENT24,
      size, size, 0,
      gl.DEPTH_COMPONENT, gl.UNSIGNED_INT, null
    );
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

    gl.bindFramebuffer(gl.FRAMEBUFFER, framebuffer);
    gl.framebufferTexture2D(
      gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.TEXTURE_2D, depthTexture, 0
    );
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);

    return { framebuffer: framebuffer, depthTexture: depthTexture, size: size };
  }

  // Create a shadow slot with N cascades. Each cascade gets its own
  // framebuffer+depth-texture pair. Cascade-specific matrices and view-space
  // far plane (splitFar) are filled in by computeShadowSlotCascadeMatrices().
  function createSceneShadowSlot(gl, size, numCascades) {
    var n = Math.max(1, Math.min(4, numCascades | 0));
    var cascades = [];
    for (var i = 0; i < n; i++) {
      var res = createSceneShadowResources(gl, size);
      cascades.push({
        framebuffer: res.framebuffer,
        depthTexture: res.depthTexture,
        size: size,
        cascadeIndex: i,
        splitNear: 0,
        splitFar: 0,
        lightMatrix: null,
        _lastPassHash: null,
      });
    }
    return {
      size: size,
      numCascades: n,
      cascades: cascades,
    };
  }

  // Release GPU resources for a shadow slot (all cascades). Safe to pass null.
  function disposeShadowSlot(gl, slot) {
    if (!slot) return;
    if (Array.isArray(slot.cascades)) {
      for (var i = 0; i < slot.cascades.length; i++) {
        var c = slot.cascades[i];
        if (c && c.framebuffer) gl.deleteFramebuffer(c.framebuffer);
        if (c && c.depthTexture) gl.deleteTexture(c.depthTexture);
      }
    } else {
      // Legacy single-cascade slot shape.
      if (slot.framebuffer) gl.deleteFramebuffer(slot.framebuffer);
      if (slot.depthTexture) gl.deleteTexture(slot.depthTexture);
    }
  }

  // Compute per-cascade light-space matrices and split-far view-space depths
  // for the given shadow slot. When numCascades === 1 the function falls back
  // to the legacy full-scene ortho fit so behaviour is identical to pre-CSM
  // single-map output.
  function computeShadowSlotCascadeMatrices(light, slot, sceneBounds, viewMatrix, fovDeg, aspect, camNear, camFar) {
    var n = slot.numCascades;
    if (n <= 1) {
      var m = sceneShadowLightSpaceMatrix(light, sceneBounds);
      slot.cascades[0].lightMatrix = m;
      slot.cascades[0].splitNear = 0;
      slot.cascades[0].splitFar = camFar || 100;
      return;
    }

    var lambda = sceneNumber(light.shadowCascadeLambda, 0.5);
    var splits = sceneShadowComputeCascadeSplits(camNear || 0.1, camFar || 100, n, lambda);

    // Light direction (normalized) — matches sceneShadowLightSpaceMatrix.
    var dx = sceneNumber(light.directionX, 0);
    var dy = sceneNumber(light.directionY, -1);
    var dz = sceneNumber(light.directionZ, 0);
    var len = Math.sqrt(dx * dx + dy * dy + dz * dz);
    if (len < 0.0001) { dx = 0; dy = -1; dz = 0; len = 1; }
    dx /= len; dy /= len; dz /= len;

    // Scene radius for extending each cascade's far plane along the light
    // axis so casters outside the camera frustum still contribute.
    var ex = (sceneBounds.maxX - sceneBounds.minX) * 0.5;
    var ey = (sceneBounds.maxY - sceneBounds.minY) * 0.5;
    var ez = (sceneBounds.maxZ - sceneBounds.minZ) * 0.5;
    var sceneRadius = Math.sqrt(ex * ex + ey * ey + ez * ez);

    var prevSplit = camNear || 0.1;
    for (var i = 0; i < n; i++) {
      var splitFar = splits[i];
      var corners = sceneShadowFrustumSubCorners(viewMatrix, fovDeg, aspect, prevSplit, splitFar);
      var matrix = sceneShadowFitLightSpaceOrtho([dx, dy, dz], corners, sceneRadius, slot.size);
      slot.cascades[i].lightMatrix = matrix;
      slot.cascades[i].splitNear = prevSplit;
      slot.cascades[i].splitFar = splitFar;
      prevSplit = splitFar;
    }
  }

  // --- Cascaded Shadow Map helpers ---
  //
  // Pure (GL-independent) math for splitting the view frustum into cascades
  // and fitting each cascade's orthographic light-space box. Exposed via
  // __gosx_scene3d_resource_api for unit tests; renderer orchestration below
  // consumes the same helpers.

  // Compute cascade far distances (view-space positive z) using the Parallel
  // Split Shadow Maps practical scheme: lambda * log-distribution + (1 - lambda)
  // * uniform-distribution. Returns an array of length numCascades where the
  // last element equals far.
  //
  // lambda=0 → uniform splits (more coverage far away, less near-detail).
  // lambda=1 → logarithmic splits (more near-detail, less far coverage).
  // lambda=0.5 is the practical default that works well for most scenes.
  function sceneShadowComputeCascadeSplits(near, far, numCascades, lambda) {
    var n = Math.max(0.0001, near || 0.1);
    var f = Math.max(n + 0.0001, far || 100);
    var count = Math.max(1, Math.min(4, numCascades | 0));
    var lam = Math.max(0, Math.min(1, typeof lambda === "number" ? lambda : 0.5));
    var ratio = f / n;
    var splits = new Array(count);
    for (var i = 0; i < count; i++) {
      var p = (i + 1) / count;
      var logSplit = n * Math.pow(ratio, p);
      var uniSplit = n + (f - n) * p;
      splits[i] = lam * logSplit + (1 - lam) * uniSplit;
    }
    return splits;
  }

  // Compute the 8 corners of a sub-frustum (in world space) bounded by
  // splitNear and splitFar (positive view-space depths). The projection is a
  // standard perspective with fovY in degrees, aspect = width/height. The
  // view matrix is column-major (same convention as scenePBRViewMatrix).
  //
  // Returns a flat Float32Array of 24 floats (8 corners × 3 components).
  function sceneShadowFrustumSubCorners(viewMatrix, fovDeg, aspect, splitNear, splitFar) {
    var fovY = (fovDeg * Math.PI) / 180;
    var tanY = Math.tan(fovY * 0.5);
    var tanX = tanY * (aspect || 1);

    var hNearY = splitNear * tanY;
    var hNearX = splitNear * tanX;
    var hFarY = splitFar * tanY;
    var hFarX = splitFar * tanX;

    // Corners in view space (camera looks down -Z). Order: near TL, TR, BL, BR,
    // far TL, TR, BL, BR.
    var viewCorners = [
      -hNearX,  hNearY, -splitNear,
       hNearX,  hNearY, -splitNear,
      -hNearX, -hNearY, -splitNear,
       hNearX, -hNearY, -splitNear,
      -hFarX,   hFarY,  -splitFar,
       hFarX,   hFarY,  -splitFar,
      -hFarX,  -hFarY,  -splitFar,
       hFarX,  -hFarY,  -splitFar,
    ];

    // Inverse view matrix: view is orthonormal (rotation) + translation, so
    // inverse is R^T + -R^T * t. For simplicity we invert analytically by
    // recognizing view = [R t; 0 1] where R is orthonormal.
    var invView = sceneInvertOrthonormalView(viewMatrix);

    var corners = new Float32Array(24);
    for (var i = 0; i < 8; i++) {
      var x = viewCorners[i * 3];
      var y = viewCorners[i * 3 + 1];
      var z = viewCorners[i * 3 + 2];
      // Apply inverse view (column-major mat4 * vec4(x,y,z,1)).
      corners[i * 3]     = invView[0] * x + invView[4] * y + invView[8]  * z + invView[12];
      corners[i * 3 + 1] = invView[1] * x + invView[5] * y + invView[9]  * z + invView[13];
      corners[i * 3 + 2] = invView[2] * x + invView[6] * y + invView[10] * z + invView[14];
    }
    return corners;
  }

  // Invert an orthonormal view matrix (columns 0,1,2 are unit, orthogonal).
  // Handles the view matrices scenePBRViewMatrix produces.
  function sceneInvertOrthonormalView(view) {
    var out = new Float32Array(16);
    // R^T: transpose the 3x3 rotation block.
    out[0]  = view[0];  out[1]  = view[4];  out[2]  = view[8];   out[3]  = 0;
    out[4]  = view[1];  out[5]  = view[5];  out[6]  = view[9];   out[7]  = 0;
    out[8]  = view[2];  out[9]  = view[6];  out[10] = view[10];  out[11] = 0;
    // t' = -R^T * t
    var tx = view[12], ty = view[13], tz = view[14];
    out[12] = -(out[0]  * tx + out[4]  * ty + out[8]  * tz);
    out[13] = -(out[1]  * tx + out[5]  * ty + out[9]  * tz);
    out[14] = -(out[2]  * tx + out[6]  * ty + out[10] * tz);
    out[15] = 1;
    return out;
  }

  // Fit a tight orthographic light-space matrix that encloses the given
  // world-space corners (24 floats = 8 points × xyz). The light direction
  // must be normalized. Returns a column-major Float32Array(16) = proj * view
  // suitable for use as u_lightSpaceMatrix.
  //
  // sceneBoundsRadius extends the far plane so shadow casters outside the
  // frustum (but between the light and the frustum) still contribute.
  function sceneShadowFitLightSpaceOrtho(lightDir, worldCorners, sceneBoundsRadius, shadowMapSize) {
    var dx = lightDir[0], dy = lightDir[1], dz = lightDir[2];
    var len = Math.sqrt(dx * dx + dy * dy + dz * dz);
    if (len < 0.0001) { dx = 0; dy = -1; dz = 0; len = 1; }
    dx /= len; dy /= len; dz /= len;

    // Centroid of the frustum corners — use as the light's lookAt target so
    // the bounding box stays centered.
    var cx = 0, cy = 0, cz = 0;
    for (var i = 0; i < 8; i++) {
      cx += worldCorners[i * 3];
      cy += worldCorners[i * 3 + 1];
      cz += worldCorners[i * 3 + 2];
    }
    cx /= 8; cy /= 8; cz /= 8;

    // Build a lookAt view from an "eye" offset back along -lightDir.
    // offset magnitude will be tightened by the ortho near plane; initial
    // guess is the distance to the farthest corner along the light axis.
    var fx = dx, fy = dy, fz = dz; // forward in view = lightDir

    var upX = 0, upY = 1, upZ = 0;
    if (Math.abs(fy) > 0.99) { upX = 0; upY = 0; upZ = 1; }

    // right = forward × up
    var rx = fy * upZ - fz * upY;
    var ry = fz * upX - fx * upZ;
    var rz = fx * upY - fy * upX;
    var rLen = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (rLen < 0.0001) rLen = 1;
    rx /= rLen; ry /= rLen; rz /= rLen;
    // up = right × forward (re-orthonormalize)
    upX = ry * fz - rz * fy;
    upY = rz * fx - rx * fz;
    upZ = rx * fy - ry * fx;

    // Project each corner into light-space axes (right, up, forward) to find
    // min/max extents.
    var minR = Infinity, maxR = -Infinity;
    var minU = Infinity, maxU = -Infinity;
    var minF = Infinity, maxF = -Infinity;
    for (var j = 0; j < 8; j++) {
      var px = worldCorners[j * 3] - cx;
      var py = worldCorners[j * 3 + 1] - cy;
      var pz = worldCorners[j * 3 + 2] - cz;
      var pr = rx * px + ry * py + rz * pz;
      var pu = upX * px + upY * py + upZ * pz;
      var pf = fx * px + fy * py + fz * pz;
      if (pr < minR) minR = pr; if (pr > maxR) maxR = pr;
      if (pu < minU) minU = pu; if (pu > maxU) maxU = pu;
      if (pf < minF) minF = pf; if (pf > maxF) maxF = pf;
    }

    // Snap the orthographic bounds to the shadow-map texel grid. Without this,
    // tiny camera movements move the cascade projection by sub-texel amounts,
    // which produces visible CSM shimmer. Snap in world light-space rather
    // than local-to-centroid space so small camera translations cancel out in
    // the final proj*view matrix until they cross a texel boundary.
    var snapSize = Math.max(0, shadowMapSize | 0);
    if (snapSize > 0 && isFinite(minR) && isFinite(maxR) && isFinite(minU) && isFinite(maxU)) {
      var width = maxR - minR;
      var height = maxU - minU;
      if (width > 0.000001 && height > 0.000001) {
        var texelR = width / snapSize;
        var texelU = height / snapSize;
        var centerWorldR = rx * cx + ry * cy + rz * cz;
        var centerWorldU = upX * cx + upY * cy + upZ * cz;
        var centerR = (minR + maxR) * 0.5;
        var centerU = (minU + maxU) * 0.5;
        var snappedCenterR = Math.round((centerWorldR + centerR) / texelR) * texelR;
        var snappedCenterU = Math.round((centerWorldU + centerU) / texelU) * texelU;
        var halfR = width * 0.5 + texelR;
        var halfU = height * 0.5 + texelU;
        centerR = snappedCenterR - centerWorldR;
        centerU = snappedCenterU - centerWorldU;
        minR = centerR - halfR;
        maxR = centerR + halfR;
        minU = centerU - halfU;
        maxU = centerU + halfU;
      }
    }

    // Expand the far plane outward along the light direction so shadow
    // casters behind the frustum (relative to the light) still render into
    // the shadow map.
    var extend = Math.max(0.0, sceneBoundsRadius || 0.0);
    minF -= extend;

    // Eye = centroid shifted back along -forward by (maxF - minF) so that the
    // frustum box sits entirely in front of the eye along the light axis.
    // With the standard GL view convention (+forward becomes the *negative*
    // Z row in the view matrix), points inside the box have view_z in
    // [-(maxF-minF), 0], which matches what the ortho proj below expects.
    var eyeX = cx - fx * maxF;
    var eyeY = cy - fy * maxF;
    var eyeZ = cz - fz * maxF;

    // View matrix rows: right, up, -forward. The negative-forward row is the
    // standard GL lookAt convention that makes view_z negative for points in
    // front of the eye — required for the ortho proj below to map them into
    // NDC [-1, 1] correctly.
    var tx = -(rx * eyeX + ry * eyeY + rz * eyeZ);
    var ty = -(upX * eyeX + upY * eyeY + upZ * eyeZ);
    var tz = -(-fx * eyeX + -fy * eyeY + -fz * eyeZ);

    var view = new Float32Array([
      rx,  upX, -fx, 0,
      ry,  upY, -fy, 0,
      rz,  upZ, -fz, 0,
      tx,  ty,   tz, 1,
    ]);

    // Ortho projection: right/left/top/bottom derived from min/max in light-
    // space right/up axes. Near/Far in standard GL convention: near is the
    // plane at view_z = -(0) = eye, far is view_z = -(maxF-minF).
    var l = minR, rr = maxR, b = minU, t = maxU;
    var near = 0.0;
    var far = (maxF - minF);
    if (far <= near) far = near + 1;

    var proj = new Float32Array([
      2 / (rr - l),            0,                       0,                            0,
      0,                       2 / (t - b),             0,                            0,
      0,                       0,                       -2 / (far - near),            0,
      -(rr + l) / (rr - l),   -(t + b) / (t - b),       -(far + near) / (far - near), 1,
    ]);

    return sceneMat4Multiply(proj, view);
  }

  // Compute an orthographic light-space matrix for a directional light.
  // sceneBounds is { minX, minY, minZ, maxX, maxY, maxZ }.
  function sceneShadowLightSpaceMatrix(light, sceneBounds) {
    // Light direction (normalized).
    var dx = sceneNumber(light.directionX, 0);
    var dy = sceneNumber(light.directionY, -1);
    var dz = sceneNumber(light.directionZ, 0);
    var len = Math.sqrt(dx * dx + dy * dy + dz * dz);
    if (len < 0.0001) {
      dx = 0; dy = -1; dz = 0; len = 1;
    }
    dx /= len; dy /= len; dz /= len;

    // Scene center and radius from AABB.
    var cx = (sceneBounds.minX + sceneBounds.maxX) * 0.5;
    var cy = (sceneBounds.minY + sceneBounds.maxY) * 0.5;
    var cz = (sceneBounds.minZ + sceneBounds.maxZ) * 0.5;
    var ex = (sceneBounds.maxX - sceneBounds.minX) * 0.5;
    var ey = (sceneBounds.maxY - sceneBounds.minY) * 0.5;
    var ez = (sceneBounds.maxZ - sceneBounds.minZ) * 0.5;
    var radius = Math.sqrt(ex * ex + ey * ey + ez * ez);
    if (radius < 0.01) radius = 10;

    // Position the light camera behind the scene center along the light direction.
    var eyeX = cx - dx * radius * 2;
    var eyeY = cy - dy * radius * 2;
    var eyeZ = cz - dz * radius * 2;

    // Build a lookAt view matrix (light looking at scene center).
    // Forward = normalize(center - eye) = (dx, dy, dz).
    var fx = dx, fy = dy, fz = dz;

    // Choose an up vector not parallel to forward.
    var upX = 0, upY = 1, upZ = 0;
    if (Math.abs(fy) > 0.99) {
      upX = 0; upY = 0; upZ = 1;
    }

    // Right = normalize(forward x up).
    var rx = fy * upZ - fz * upY;
    var ry = fz * upX - fx * upZ;
    var rz = fx * upY - fy * upX;
    var rLen = Math.sqrt(rx * rx + ry * ry + rz * rz);
    if (rLen < 0.0001) rLen = 1;
    rx /= rLen; ry /= rLen; rz /= rLen;

    // Recompute up = right x forward.
    upX = ry * fz - rz * fy;
    upY = rz * fx - rx * fz;
    upZ = rx * fy - ry * fx;

    // View matrix (column-major).
    var tx = -(rx * eyeX + ry * eyeY + rz * eyeZ);
    var ty = -(upX * eyeX + upY * eyeY + upZ * eyeZ);
    var tz = -(fx * eyeX + fy * eyeY + fz * eyeZ);

    // Note: forward is positive — we look along +forward, so no negation.
    var view = new Float32Array([
      rx,  upX, fx,  0,
      ry,  upY, fy,  0,
      rz,  upZ, fz,  0,
      tx,  ty,  tz,  1,
    ]);

    // Orthographic projection matrix (column-major).
    // Maps [-radius, radius] in all axes to [-1, 1] clip space.
    var near = 0.01;
    var far = radius * 4;
    var l = -radius, rr = radius, b = -radius, t = radius;
    var proj = new Float32Array([
      2 / (rr - l),     0,              0,                    0,
      0,                2 / (t - b),    0,                    0,
      0,                0,              -2 / (far - near),    0,
      -(rr + l) / (rr - l), -(t + b) / (t - b), -(far + near) / (far - near), 1,
    ]);

    // Multiply proj * view (column-major).
    return sceneMat4Multiply(proj, view);
  }

  // Compute the AABB of all objects in the bundle.
  function sceneShadowComputeBounds(bundle) {
    var minX = Infinity, minY = Infinity, minZ = Infinity;
    var maxX = -Infinity, maxY = -Infinity, maxZ = -Infinity;
    var positions = bundle.worldMeshPositions;
    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (obj.directVertices) continue;
      var offset = obj.vertexOffset;
      var count = obj.vertexCount;
      if (!Number.isFinite(offset) || !Number.isFinite(count) || count <= 0) continue;

      for (var v = 0; v < count; v++) {
        var idx = (offset + v) * 3;
        var px = positions[idx];
        var py = positions[idx + 1];
        var pz = positions[idx + 2];
        if (px < minX) minX = px;
        if (py < minY) minY = py;
        if (pz < minZ) minZ = pz;
        if (px > maxX) maxX = px;
        if (py > maxY) maxY = py;
        if (pz > maxZ) maxZ = pz;
      }
    }

    if (!isFinite(minX)) {
      return { minX: -10, minY: -10, minZ: -10, maxX: 10, maxY: 10, maxZ: 10 };
    }
    return { minX: minX, minY: minY, minZ: minZ, maxX: maxX, maxY: maxY, maxZ: maxZ };
  }

  // Computes a cheap change-detector hash for a shadow pass. When the
  // hash matches the last rendered pass for this light, the previously
  // drawn shadow map is still valid and the whole pass can be skipped.
  //
  // Inputs that move the hash:
  //   - lightMatrix (16 floats) — summed
  //   - For each shadow-casting mesh: vertexOffset + vertexCount +
  //     depthNear + depthFar. depthNear/Far is the transform-sensitive
  //     signal (recomputed by the cached sceneBoundsDepthMetrics), so
  //     moving a caster or camera invalidates the hash. vertexOffset/
  //     Count pick up topology changes (skinning, geometry swaps).
  //
  // Not hashed but acceptable:
  //   - Position values themselves (too expensive). We rely on depthNear/
  //     Far changing when vertices move, which is a reliable proxy for
  //     any transform but misses pure topology edits that keep the AABB
  //     constant (rare, and visually imperceptible because the shadow
  //     silhouette is unchanged).
  function sceneShadowPassHash(lightMatrix, meshObjects, options) {
    var opts = options || {};
    var h = 0;
    if (lightMatrix) {
      for (var i = 0; i < 16; i++) h += lightMatrix[i] || 0;
    }
    h += sceneFiniteNumber(opts.cascadeIndex, 0) * 101.0;
    h += sceneFiniteNumber(opts.splitNear, 0) * 103.0;
    h += sceneFiniteNumber(opts.splitFar, 0) * 107.0;
    h += sceneFiniteNumber(opts.shadowSize, 0) * 109.0;
    var casterCount = 0;
    for (var j = 0; j < meshObjects.length; j++) {
      var o = meshObjects[j];
      if (!o || !o.castShadow || o.viewCulled) continue;
      h += (o.vertexOffset || 0) + (o.vertexCount || 0)
         + (o.depthNear || 0) + (o.depthFar || 0);
      casterCount++;
    }
    h += casterCount * 17.0;
    return h;
  }

  // Render a depth-only shadow pass into the shadow framebuffer.
  // shadowState holds a persistent GL buffer and scratch typed array
  // to avoid per-object per-light per-frame allocations.
  //
  // Skips the entire pass when the content hash matches the previous
  // frame — on a static scene with static lights this reclaims every
  // bit of the shadow pipeline (clear, N×bufferData, N×drawArrays)
  // and lets the existing depth texture be sampled as-is.
  function renderSceneShadowPass(gl, shadowProgram, shadowResources, lightMatrix, bundle, shadowState) {
    var meshObjectsForHash = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];
    var passHash = sceneShadowPassHash(lightMatrix, meshObjectsForHash, {
      cascadeIndex: shadowResources && typeof shadowResources.cascadeIndex === "number" ? shadowResources.cascadeIndex : 0,
      splitNear: shadowResources && typeof shadowResources.splitNear === "number" ? shadowResources.splitNear : 0,
      splitFar: shadowResources && typeof shadowResources.splitFar === "number" ? shadowResources.splitFar : 0,
      shadowSize: shadowResources && typeof shadowResources.size === "number" ? shadowResources.size : 0,
    });
    if (shadowResources._lastPassHash === passHash) {
      return;
    }
    shadowResources._lastPassHash = passHash;

    gl.bindFramebuffer(gl.FRAMEBUFFER, shadowResources.framebuffer);
    gl.viewport(0, 0, shadowResources.size, shadowResources.size);
    gl.clearDepth(1);
    gl.clear(gl.DEPTH_BUFFER_BIT);

    gl.useProgram(shadowProgram.program);
    gl.uniformMatrix4fv(shadowProgram.uniforms.lightViewProjection, false, lightMatrix);

    gl.enable(gl.DEPTH_TEST);
    gl.depthMask(true);
    gl.depthFunc(gl.LEQUAL);
    gl.disable(gl.BLEND);

    // Only cull back faces for shadow pass to reduce peter-panning.
    gl.enable(gl.CULL_FACE);
    gl.cullFace(gl.FRONT);

    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : [];

    for (var i = 0; i < objects.length; i++) {
      var obj = objects[i];
      if (!obj || obj.viewCulled) continue;
      if (obj.directVertices) continue;
      if (!obj.castShadow) continue;
      if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) continue;

      var offset = obj.vertexOffset;
      var count = obj.vertexCount;

      // Upload positions, reusing scratch typed array.
      var length = count * 3;
      var start = offset * 3;
      if (!shadowState.scratch || shadowState.scratch.length < length) {
        shadowState.scratch = new Float32Array(length);
      }
      var positions = shadowState.scratch;
      for (var vi = 0; vi < length; vi++) {
        positions[vi] = bundle.worldMeshPositions[start + vi] || 0;
      }

      gl.bindBuffer(gl.ARRAY_BUFFER, shadowState.buffer);
      gl.bufferData(gl.ARRAY_BUFFER, positions.subarray(0, length), gl.DYNAMIC_DRAW);
      gl.enableVertexAttribArray(shadowProgram.attributes.position);
      gl.vertexAttribPointer(shadowProgram.attributes.position, 3, gl.FLOAT, false, 0, 0);

      gl.drawArrays(gl.TRIANGLES, 0, count);
    }

    // Restore cull state.
    gl.cullFace(gl.BACK);
    gl.disable(gl.CULL_FACE);

    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
  }

  // Compile the shadow depth shader program.
  function createSceneShadowProgram(gl) {
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_SHADOW_VERTEX_SOURCE);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_SHADOW_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Shadow shader");
    if (!program) return null;

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: {
        position: gl.getAttribLocation(program, "a_position"),
      },
      uniforms: {
        lightViewProjection: gl.getUniformLocation(program, "u_lightViewProjection"),
      },
    };
  }

  // --- Post-Processing Infrastructure ---

  // Shared vertex shader for all fullscreen post-processing passes.
  const SCENE_POST_VERTEX_SOURCE = [
    "#version 300 es",
    "in vec2 a_position;",
    "in vec2 a_uv;",
    "out vec2 v_uv;",
    "void main() {",
    "    v_uv = a_uv;",
    "    gl_Position = vec4(a_position, 0.0, 1.0);",
    "}",
  ].join("\n");

  // ACES filmic tone mapping pass.
  const SCENE_POST_TONEMAPPING_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_exposure;",
    "uniform int u_toneMapMode;",
    "out vec4 fragColor;",
    "",
    "vec3 aces(vec3 x) {",
    "    float a = 2.51;",
    "    float b = 0.03;",
    "    float c = 2.43;",
    "    float d = 0.59;",
    "    float e = 0.14;",
    "    return clamp((x * (a * x + b)) / (x * (c * x + d) + e), 0.0, 1.0);",
    "}",
    "",
    "vec3 reinhard(vec3 x) {",
    "    return x / (x + vec3(1.0));",
    "}",
    "",
    "vec3 filmic(vec3 x) {",
    "    x = max(vec3(0.0), x - vec3(0.004));",
    "    return clamp((x * (6.2 * x + 0.5)) / (x * (6.2 * x + 1.7) + 0.06), 0.0, 1.0);",
    "}",
    "",
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    color *= u_exposure;",
    "    if (u_toneMapMode == 0) {",
    "        color = clamp(color, 0.0, 1.0);",
    "    } else if (u_toneMapMode == 2) {",
    "        color = reinhard(color);",
    "    } else if (u_toneMapMode == 3) {",
    "        color = filmic(color);",
    "    } else {",
    "        color = aces(color);",
    "    }",
    "    fragColor = vec4(color, 1.0);",
    "}",
  ].join("\n");

  // Bloom bright pass — extracts pixels above luminance threshold.
  const SCENE_POST_BLOOM_BRIGHT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_threshold;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    float brightness = dot(color, vec3(0.2126, 0.7152, 0.0722));",
    "    fragColor = vec4(brightness > u_threshold ? color : vec3(0.0), 1.0);",
    "}",
  ].join("\n");

  // Gaussian blur — direction uniform selects horizontal or vertical pass.
  const SCENE_POST_BLUR_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform vec2 u_direction;",
    "uniform float u_radius;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec2 texelSize = 1.0 / vec2(textureSize(u_texture, 0));",
    "    vec3 result = texture(u_texture, v_uv).rgb * 0.227027;",
    "",
    "    float offsets[4] = float[](1.0, 2.0, 3.0, 4.0);",
    "    float weights[4] = float[](0.1945946, 0.1216216, 0.054054, 0.016216);",
    "",
    "    for (int i = 0; i < 4; i++) {",
    "        vec2 offset = u_direction * texelSize * offsets[i] * u_radius;",
    "        result += texture(u_texture, v_uv + offset).rgb * weights[i];",
    "        result += texture(u_texture, v_uv - offset).rgb * weights[i];",
    "    }",
    "    fragColor = vec4(result, 1.0);",
    "}",
  ].join("\n");

  // Bloom composite — additive blend of bloom texture onto scene.
  const SCENE_POST_BLOOM_COMPOSITE_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform sampler2D u_bloomTexture;",
    "uniform float u_intensity;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec3 scene = texture(u_texture, v_uv).rgb;",
    "    vec3 bloom = texture(u_bloomTexture, v_uv).rgb;",
    "    fragColor = vec4(scene + bloom * u_intensity, 1.0);",
    "}",
  ].join("\n");

  // Vignette darkening toward screen edges.
  const SCENE_POST_VIGNETTE_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_intensity;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    vec2 center = v_uv - 0.5;",
    "    float dist = length(center);",
    "    float vignette = 1.0 - smoothstep(0.3, 0.7, dist * u_intensity);",
    "    fragColor = vec4(color * vignette, 1.0);",
    "}",
  ].join("\n");

  // Depthless SSAO-style contrast pass. It samples a local luminance ring and
  // darkens pixels whose neighborhood suggests contact/creases; when a future
  // depth texture is threaded through postfx this shader can swap to true
  // depth reconstruction without changing the public effect kind.
  const SCENE_POST_SSAO_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_radius;",
    "uniform float u_intensity;",
    "out vec4 fragColor;",
    "",
    "float luminance(vec3 color) { return dot(color, vec3(0.2126, 0.7152, 0.0722)); }",
    "",
    "void main() {",
    "    vec2 texel = 1.0 / vec2(textureSize(u_texture, 0));",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    float center = luminance(color);",
    "    float r = max(1.0, u_radius);",
    "    float neighbor = 0.0;",
    "    neighbor += luminance(texture(u_texture, v_uv + texel * vec2( r,  0.0)).rgb);",
    "    neighbor += luminance(texture(u_texture, v_uv + texel * vec2(-r,  0.0)).rgb);",
    "    neighbor += luminance(texture(u_texture, v_uv + texel * vec2(0.0,  r)).rgb);",
    "    neighbor += luminance(texture(u_texture, v_uv + texel * vec2(0.0, -r)).rgb);",
    "    neighbor += luminance(texture(u_texture, v_uv + texel * vec2( r,  r)).rgb);",
    "    neighbor += luminance(texture(u_texture, v_uv + texel * vec2(-r,  r)).rgb);",
    "    neighbor += luminance(texture(u_texture, v_uv + texel * vec2( r, -r)).rgb);",
    "    neighbor += luminance(texture(u_texture, v_uv + texel * vec2(-r, -r)).rgb);",
    "    neighbor *= 0.125;",
    "    float crease = clamp((neighbor - center) * 2.25, 0.0, 1.0);",
    "    float occlusion = 1.0 - crease * clamp(u_intensity, 0.0, 2.0);",
    "    fragColor = vec4(color * occlusion, 1.0);",
    "}",
  ].join("\n");

  const SCENE_POST_DOF_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform sampler2D u_depthTexture;",
    "uniform float u_focusDistance;",
    "uniform float u_aperture;",
    "uniform float u_maxBlur;",
    "uniform float u_near;",
    "uniform float u_far;",
    "out vec4 fragColor;",
    "",
    "float linearDepth(float depth) {",
    "    float z = depth * 2.0 - 1.0;",
    "    return (2.0 * u_near * u_far) / max(0.0001, u_far + u_near - z * (u_far - u_near));",
    "}",
    "",
    "void main() {",
    "    vec2 texel = 1.0 / vec2(textureSize(u_texture, 0));",
    "    float depth = linearDepth(texture(u_depthTexture, v_uv).r);",
    "    float blur = clamp(abs(depth - u_focusDistance) * u_aperture * u_maxBlur, 0.0, u_maxBlur);",
    "    vec3 color = texture(u_texture, v_uv).rgb * 0.22;",
    "    color += texture(u_texture, v_uv + texel * vec2( blur,  0.0)).rgb * 0.10;",
    "    color += texture(u_texture, v_uv + texel * vec2(-blur,  0.0)).rgb * 0.10;",
    "    color += texture(u_texture, v_uv + texel * vec2(0.0,  blur)).rgb * 0.10;",
    "    color += texture(u_texture, v_uv + texel * vec2(0.0, -blur)).rgb * 0.10;",
    "    color += texture(u_texture, v_uv + texel * vec2( blur,  blur)).rgb * 0.095;",
    "    color += texture(u_texture, v_uv + texel * vec2(-blur,  blur)).rgb * 0.095;",
    "    color += texture(u_texture, v_uv + texel * vec2( blur, -blur)).rgb * 0.095;",
    "    color += texture(u_texture, v_uv + texel * vec2(-blur, -blur)).rgb * 0.095;",
    "    fragColor = vec4(color, 1.0);",
    "}",
  ].join("\n");

  // Color grading — exposure, contrast, and saturation adjustments.
  const SCENE_POST_COLORGRADE_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "in vec2 v_uv;",
    "uniform sampler2D u_texture;",
    "uniform float u_exposure;",
    "uniform float u_contrast;",
    "uniform float u_saturation;",
    "out vec4 fragColor;",
    "",
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    color *= u_exposure;",
    "    color = mix(vec3(0.5), color, u_contrast);",
    "    float gray = dot(color, vec3(0.2126, 0.7152, 0.0722));",
    "    color = mix(vec3(gray), color, u_saturation);",
    "    fragColor = vec4(clamp(color, 0.0, 1.0), 1.0);",
    "}",
  ].join("\n");

  // Create an offscreen framebuffer with HDR color texture and optional depth texture.
  // Uses RGBA16F when EXT_color_buffer_float is available, RGBA8 otherwise.
  function createScenePostFBO(gl, width, height, depthTexture) {
    var hdrSupported = Boolean(gl.getExtension("EXT_color_buffer_float"));
    var internalFormat = hdrSupported ? gl.RGBA16F : gl.RGBA8;
    var dataType = hdrSupported ? gl.FLOAT : gl.UNSIGNED_BYTE;

    var fbo = gl.createFramebuffer();
    var colorTex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, colorTex);
    gl.texImage2D(gl.TEXTURE_2D, 0, internalFormat, width, height, 0, gl.RGBA, dataType, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

    var depthRB = null;
    var depthTex = null;
    if (depthTexture) {
      depthTex = gl.createTexture();
      gl.bindTexture(gl.TEXTURE_2D, depthTex);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.DEPTH_COMPONENT24, width, height, 0, gl.DEPTH_COMPONENT, gl.UNSIGNED_INT, null);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    } else {
      depthRB = gl.createRenderbuffer();
      gl.bindRenderbuffer(gl.RENDERBUFFER, depthRB);
      gl.renderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT24, width, height);
    }

    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, colorTex, 0);
    if (depthTex) {
      gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.TEXTURE_2D, depthTex, 0);
    } else {
      gl.framebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.RENDERBUFFER, depthRB);
    }
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);

    return { fbo: fbo, colorTex: colorTex, depthRB: depthRB, depthTex: depthTex, width: width, height: height };
  }

  // Create a ping-pong FBO pair for multi-pass effect processing.
  function createScenePostPingPong(gl, width, height) {
    return {
      a: createScenePostFBO(gl, width, height),
      b: createScenePostFBO(gl, width, height),
    };
  }

  // Fullscreen quad geometry — two-triangle strip with position + UV.
  function createSceneFullscreenQuad(gl) {
    var vao = gl.createVertexArray();
    var vbo = gl.createBuffer();
    gl.bindVertexArray(vao);
    gl.bindBuffer(gl.ARRAY_BUFFER, vbo);
    // Fullscreen quad: position(xy) + uv, 4 vertices as triangle strip.
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array([
      -1, -1,  0, 0,
       1, -1,  1, 0,
      -1,  1,  0, 1,
       1,  1,  1, 1,
    ]), gl.STATIC_DRAW);
    gl.enableVertexAttribArray(0);
    gl.vertexAttribPointer(0, 2, gl.FLOAT, false, 16, 0);
    gl.enableVertexAttribArray(1);
    gl.vertexAttribPointer(1, 2, gl.FLOAT, false, 16, 8);
    gl.bindVertexArray(null);
    return { vao: vao, vbo: vbo };
  }

  // Draw a fullscreen quad using the bound shader program.
  function drawSceneFullscreenQuad(gl, quadVAO) {
    gl.bindVertexArray(quadVAO);
    gl.drawArrays(gl.TRIANGLE_STRIP, 0, 4);
    gl.bindVertexArray(null);
  }

  // Compile and link a post-processing shader program from the shared
  // vertex shader and a specific fragment shader source.
  function createScenePostProgram(gl, fragmentSource) {
    var vs = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_POST_VERTEX_SOURCE);
    if (!vs) return null;
    var fs = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!fs) {
      gl.deleteShader(vs);
      return null;
    }

    var prog = scenePBRLinkProgram(gl, vs, fs, "Post-process shader");
    if (!prog) return null;

    return { program: prog, vertexShader: vs, fragmentShader: fs };
  }

  // createSceneCustomPostProgram: links a Selena post program using the
  // caller-supplied vertex source (Selena emits its own vertex GLSL).
  function createSceneCustomPostProgram(gl, vertexSource, fragmentSource) {
    var vs = scenePBRCompileShader(gl, gl.VERTEX_SHADER, vertexSource);
    if (!vs) return null;
    var fs = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!fs) {
      gl.deleteShader(vs);
      return null;
    }
    var prog = scenePBRLinkProgram(gl, vs, fs, "Custom post shader");
    if (!prog) return null;
    return { program: prog, vertexShader: vs, fragmentShader: fs };
  }

  // Dispose an FBO and its attachments.
  function disposeScenePostFBO(gl, fboObj) {
    if (!fboObj) return;
    if (fboObj.colorTex) gl.deleteTexture(fboObj.colorTex);
    if (fboObj.depthTex) gl.deleteTexture(fboObj.depthTex);
    if (fboObj.depthRB) gl.deleteRenderbuffer(fboObj.depthRB);
    if (fboObj.fbo) gl.deleteFramebuffer(fboObj.fbo);
  }

  // ==========================================================================
  // WebGL2 water heightfield simulation (selena GLES sim shaders).
  //
  // A fragment-shader ping-pong driver that runs the selena-compiled GLES water
  // simulation kernels (seed / drop / displacement / simulation / normal) as
  // fullscreen passes over a pair of float state textures. This is the WebGL2
  // fallback for the WebGPU compute water path (16a); the WebGPU path is
  // untouched and still consumes the *WGSL slots. Self-contained + WebGL2-only.
  //
  // State texture layout (RGBA, one texel per grid cell):
  //   x = height, y = velocity, z = surface normal X, w = surface normal Z.
  // The render passes (A2) reconstruct normalY = sqrt(max(0, 1 - z*z - w*w))
  // and use surface normal = vec3(z, normalY, w).
  // ==========================================================================

  // Local numeric coercion (kept independent of the scene-core helpers so the
  // water driver stays self-contained).
  function sceneWaterNum(value, fallback) {
    var n = Number(value);
    return Number.isFinite(n) ? n : fallback;
  }

  function sceneWaterClamp(value, lo, hi) {
    if (value < lo) return lo;
    if (value > hi) return hi;
    return value;
  }

  // Grid resolution for the heightfield, matching the WebGPU clamp.
  function sceneWaterSimResolution(value) {
    var raw = Math.floor(sceneWaterNum(value, 256));
    if (!Number.isFinite(raw) || raw <= 0) raw = 256;
    return Math.max(16, Math.min(512, raw));
  }

  // Detect float render-target capabilities for the water state textures.
  // EXT_color_buffer_float makes RGBA32F (and RGBA16F) color-renderable in
  // WebGL2; EXT_color_buffer_half_float covers only the 16F formats. NEAREST
  // sampling is used unless OES_texture_float_linear is present.
  function sceneWaterGLFloatCaps(gl) {
    var colorBufferFloat = Boolean(gl.getExtension("EXT_color_buffer_float"));
    var colorBufferHalfFloat = colorBufferFloat || Boolean(gl.getExtension("EXT_color_buffer_half_float"));
    var floatLinear = Boolean(gl.getExtension("OES_texture_float_linear"));
    return {
      colorBufferFloat: colorBufferFloat,
      colorBufferHalfFloat: colorBufferHalfFloat,
      floatLinear: floatLinear,
    };
  }

  // Allocate one square float state texture wrapped in a framebuffer. Returns
  // null (and cleans up) when the FBO is not framebuffer-complete, so callers
  // can fall back to a lower-precision format.
  function sceneWaterCreateStateTarget(gl, size, internalFormat, dataType, filter) {
    var tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texImage2D(gl.TEXTURE_2D, 0, internalFormat, size, size, 0, gl.RGBA, dataType, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, filter);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, filter);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    var fbo = gl.createFramebuffer();
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
    var status = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    if (status !== gl.FRAMEBUFFER_COMPLETE) {
      gl.deleteTexture(tex);
      gl.deleteFramebuffer(fbo);
      return null;
    }
    return { tex: tex, fbo: fbo, size: size };
  }

  // Parse a Selena bindings.Layout descriptor (raw JSON string or parsed object).
  function sceneWaterParseDescriptor(raw) {
    if (!raw) return null;
    if (typeof raw === "string") {
      try { return JSON.parse(raw); } catch (e) { return null; }
    }
    if (typeof raw === "object") return raw;
    return null;
  }

  // Component width for a Selena scalar/vector uniform type.
  function sceneWaterTypeWidth(type) {
    switch (type) {
      case "vec4": return 4;
      case "vec3": return 3;
      case "vec2": return 2;
      default: return 1;
    }
  }

  // Set one scalar/vector uniform from a flat numeric value/array.
  function sceneWaterSetScalarUniform(gl, loc, type, value, base) {
    base = base || 0;
    switch (type) {
      case "float": gl.uniform1f(loc, +value[base]); break;
      case "vec2": gl.uniform2f(loc, value[base], value[base + 1]); break;
      case "vec3": gl.uniform3f(loc, value[base], value[base + 1], value[base + 2]); break;
      case "vec4": gl.uniform4f(loc, value[base], value[base + 1], value[base + 2], value[base + 3]); break;
      default: break;
    }
  }

  // Set a fixed-size array uniform element-by-element using the `name[i]`
  // location per the descriptor count. value is a flat numeric array packed at
  // the type's component width (e.g. vec4 array → 4 floats per element).
  function sceneWaterSetArrayUniform(gl, program, field, value, count) {
    if (!value || !value.length) return;
    var width = sceneWaterTypeWidth(field.type);
    var active = Math.min(count, Math.floor(value.length / width));
    for (var i = 0; i < active; i++) {
      var loc = gl.getUniformLocation(program, field.name + "[" + i + "]");
      if (!loc) continue;
      sceneWaterSetScalarUniform(gl, loc, field.type, value, i * width);
    }
  }

  // Apply the state sampler unit, texelSize (= 1/resolution), and every
  // uniform-block field of one sim-pass descriptor. `values` maps each field
  // name to a number (scalar) or a flat numeric array (vector/array). Array
  // fields (count > 1) are uploaded per element by name. The previous-state
  // texture must already be bound to the state unit by the caller.
  function sceneWaterApplyPassUniforms(gl, program, descriptor, resolution, values) {
    if (!descriptor) return;
    var states = Array.isArray(descriptor.states) ? descriptor.states : [];
    for (var s = 0; s < states.length; s++) {
      var glState = states[s] && states[s].gl;
      if (glState && glState.uniform) {
        var sLoc = gl.getUniformLocation(program, glState.uniform);
        if (sLoc) gl.uniform1i(sLoc, glState.unit || 0);
      }
    }
    var texelName = descriptor.grid && descriptor.grid.glTexelUniform;
    if (texelName) {
      var texLoc = gl.getUniformLocation(program, texelName);
      if (texLoc) gl.uniform2f(texLoc, 1 / resolution, 1 / resolution);
    }
    var block = descriptor.uniformBlock;
    var fields = block && Array.isArray(block.fields) ? block.fields : [];
    for (var f = 0; f < fields.length; f++) {
      var field = fields[f];
      if (!field || !field.name) continue;
      var value = values ? values[field.name] : undefined;
      var count = field.count && field.count > 1 ? field.count : 0;
      if (count > 0) {
        sceneWaterSetArrayUniform(gl, program, field, value, count);
        continue;
      }
      if (value === undefined || value === null) continue;
      var loc = gl.getUniformLocation(program, field.name);
      if (!loc) continue;
      if (field.type === "float") {
        gl.uniform1f(loc, +value);
      } else {
        sceneWaterSetScalarUniform(gl, loc, field.type, value, 0);
      }
    }
  }

  // GPU-faithful hash mirroring the WebGPU seed kernel's hash01(n)=fract(sin(n)*k).
  function sceneWaterHash01(n) {
    var v = Math.sin(n) * 43758.5453123;
    return v - Math.floor(v);
  }

  // Build the seed `drops[64]` uniform array (vec4: xy=uv center, w=signed
  // strength) replicating the WebGPU seedDrops kernel: per-drop hashed centers
  // and alternating polarity so a reset produces the same spread of ripples.
  function sceneWaterBuildSeedDrops(count, seedSalt, dropStrength) {
    count = Math.max(0, Math.min(64, Math.floor(count)));
    var out = new Float32Array(count * 4);
    for (var j = 0; j < count; j++) {
      var jf = j + 1;
      out[j * 4 + 0] = sceneWaterHash01(jf * 12.9898 + seedSalt + 0.173);
      out[j * 4 + 1] = sceneWaterHash01(jf * 78.233 + seedSalt * 1.371 + 0.719);
      out[j * 4 + 2] = 0;
      out[j * 4 + 3] = dropStrength * ((j & 1) === 0 ? -1 : 1);
    }
    return out;
  }

  // createSceneWaterSimWebGL: build a WebGL2 ping-pong heightfield water sim
  // driver from a lowered WaterSystemIR entry (camelCase JSON shape carrying the
  // *FragmentGLES / *VertexGLES sim shaders and shaderDescriptors). Returns a
  // driver object exposing per-event passes (drop / seed / displace), the
  // per-frame step (simulation + normal), the current state texture for the
  // render passes (A2), and dispose(). Returns null when float render targets
  // are unavailable or the required simulation/normal programs fail to compile.
  function createSceneWaterSimWebGL(gl, entry) {
    if (!gl || !entry || typeof entry !== "object") return null;

    var resolution = sceneWaterSimResolution(entry.resolution);
    var caps = sceneWaterGLFloatCaps(gl);
    if (!caps.colorBufferFloat && !caps.colorBufferHalfFloat) return null;
    var filter = caps.floatLinear ? gl.LINEAR : gl.NEAREST;

    // Prefer RGBA32F; fall back to RGBA16F when 32F is not renderable.
    var formats = [];
    if (caps.colorBufferFloat) formats.push({ internal: gl.RGBA32F, type: gl.FLOAT, label: "RGBA32F" });
    formats.push({ internal: gl.RGBA16F, type: gl.HALF_FLOAT, label: "RGBA16F" });

    var states = null;
    var formatLabel = "";
    for (var fi = 0; fi < formats.length; fi++) {
      var a = sceneWaterCreateStateTarget(gl, resolution, formats[fi].internal, formats[fi].type, filter);
      if (!a) continue;
      var b = sceneWaterCreateStateTarget(gl, resolution, formats[fi].internal, formats[fi].type, filter);
      if (!b) { gl.deleteTexture(a.tex); gl.deleteFramebuffer(a.fbo); continue; }
      states = [a, b];
      formatLabel = formats[fi].label;
      break;
    }
    if (!states) return null;

    // Shared fullscreen-triangle vertex (every sim shader emits the same one).
    var sharedVertex = entry.simulationVertexGLES || entry.normalVertexGLES ||
      entry.dropVertexGLES || entry.seedVertexGLES || entry.displacementVertexGLES || "";

    var descriptors = entry.shaderDescriptors && typeof entry.shaderDescriptors === "object"
      ? entry.shaderDescriptors : {};

    var passSpecs = {
      simulation: { vertex: entry.simulationVertexGLES, fragment: entry.simulationFragmentGLES, desc: "simulation" },
      normal: { vertex: entry.normalVertexGLES, fragment: entry.normalFragmentGLES, desc: "normal" },
      seed: { vertex: entry.seedVertexGLES, fragment: entry.seedFragmentGLES, desc: "seed" },
      drop: { vertex: entry.dropVertexGLES, fragment: entry.dropFragmentGLES, desc: "drop" },
      displacement: { vertex: entry.displacementVertexGLES, fragment: entry.displacementFragmentGLES, desc: "displacement" },
    };

    var programs = {};
    var compiledShaders = [];
    function compilePass(name) {
      var spec = passSpecs[name];
      if (!spec || !spec.fragment) return null;
      var vsSrc = spec.vertex || sharedVertex;
      if (!vsSrc) return null;
      var vs = scenePBRCompileShader(gl, gl.VERTEX_SHADER, vsSrc);
      if (!vs) return null;
      var fs = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, spec.fragment);
      if (!fs) { gl.deleteShader(vs); return null; }
      var prog = scenePBRLinkProgram(gl, vs, fs, "Water sim (" + name + ")");
      gl.deleteShader(vs);
      gl.deleteShader(fs);
      if (!prog) return null;
      compiledShaders.push(prog);
      return {
        program: prog,
        descriptor: sceneWaterParseDescriptor(descriptors[spec.desc]),
      };
    }

    programs.simulation = compilePass("simulation");
    programs.normal = compilePass("normal");
    if (!programs.simulation || !programs.normal) {
      // Required passes missing — tear down and signal unavailability.
      for (var ci = 0; ci < compiledShaders.length; ci++) gl.deleteProgram(compiledShaders[ci]);
      gl.deleteTexture(states[0].tex); gl.deleteFramebuffer(states[0].fbo);
      gl.deleteTexture(states[1].tex); gl.deleteFramebuffer(states[1].fbo);
      return null;
    }
    programs.seed = compilePass("seed");
    programs.drop = compilePass("drop");
    programs.displacement = compilePass("displacement");

    // Empty VAO: the sim vertex shaders are attribute-less (gl_VertexID drives a
    // fullscreen triangle), but WebGL2 draws still need a VAO bound so leftover
    // attribute state from PBR draws does not bleed in.
    var emptyVAO = gl.createVertexArray();

    var current = 0; // index of the state holding the latest valid frame.

    // Resting-surface defaults, mirroring the WebGPU runtime clamps.
    var waveSpeed = sceneWaterClamp(sceneWaterNum(entry.waveSpeed, 1.0), 0, 2);
    var damping = sceneWaterClamp(sceneWaterNum(entry.damping, 0.995), 0, 1);
    var dropRadius = sceneWaterClamp(sceneWaterNum(entry.dropRadius, 0.05), 0.0001, 0.5);
    var dropStrength = sceneWaterClamp(sceneWaterNum(entry.dropStrength, 0.05), -1, 1);
    var seedSalt = sceneWaterNum(entry.seedSalt, (Math.random() * 4096) | 0);

    function clearState() {
      for (var k = 0; k < 2; k++) {
        gl.bindFramebuffer(gl.FRAMEBUFFER, states[k].fbo);
        gl.viewport(0, 0, resolution, resolution);
        gl.clearColor(0, 0, 0, 0);
        gl.clear(gl.COLOR_BUFFER_BIT);
      }
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      current = 0;
    }
    clearState();

    // Run one fullscreen pass: read the current state, write the other FBO, swap.
    function runPass(name, values) {
      var pass = programs[name];
      if (!pass) return false;
      var read = states[current];
      var write = states[current ^ 1];
      var unit = 0;
      var descStates = pass.descriptor && Array.isArray(pass.descriptor.states) ? pass.descriptor.states : null;
      if (descStates && descStates[0] && descStates[0].gl) unit = descStates[0].gl.unit || 0;

      gl.bindFramebuffer(gl.FRAMEBUFFER, write.fbo);
      gl.viewport(0, 0, resolution, resolution);
      gl.disable(gl.DEPTH_TEST);
      gl.disable(gl.BLEND);
      gl.useProgram(pass.program);
      gl.activeTexture(gl.TEXTURE0 + unit);
      gl.bindTexture(gl.TEXTURE_2D, read.tex);
      sceneWaterApplyPassUniforms(gl, pass.program, pass.descriptor, resolution, values);
      gl.bindVertexArray(emptyVAO);
      gl.drawArrays(gl.TRIANGLES, 0, 3);
      gl.bindVertexArray(null);
      current ^= 1;
      return true;
    }

    // Advance the heightfield: simulation substep(s) then a normal recompute.
    // Defaults to 2 simulation substeps per frame to match the WebGPU path.
    function step(options) {
      var substeps = options && options.substeps > 0 ? Math.floor(options.substeps) : 2;
      var simValues = { waveSpeed: waveSpeed, damping: damping };
      for (var i = 0; i < substeps; i++) runPass("simulation", simValues);
      runPass("normal", null);
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      return true;
    }

    // Pointer-draw event: add a Gaussian-cosine bump at an NDC center [-1,1].
    function drop(opts) {
      if (!programs.drop) return false;
      var x = opts && opts.x != null ? +opts.x : sceneWaterNum(entry.dropX, 0);
      var z = opts && opts.z != null ? +opts.z : sceneWaterNum(entry.dropZ, 0);
      var radius = opts && opts.radius != null ? +opts.radius : dropRadius;
      var strength = opts && opts.strength != null ? +opts.strength : dropStrength;
      runPass("drop", {
        dropCenter: [sceneWaterClamp(x, -1, 1), sceneWaterClamp(z, -1, 1)],
        dropRadius: sceneWaterClamp(radius, 0.0001, 0.5),
        dropStrength: strength,
      });
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      return true;
    }

    // Reset event: stamp the procedural seed drops onto the resting surface.
    function seed(opts) {
      if (!programs.seed) return false;
      var count = Math.max(0, Math.min(64, Math.floor(
        opts && opts.count != null ? opts.count : sceneWaterNum(entry.seedDrops, 7))));
      if (count <= 0) return false;
      var radius = opts && opts.radius != null ? +opts.radius : dropRadius;
      var strength = opts && opts.strength != null ? +opts.strength : dropStrength;
      var salt = opts && opts.seedSalt != null ? +opts.seedSalt : seedSalt;
      runPass("seed", {
        dropCount: count,
        dropRadius: sceneWaterClamp(radius, 0.0001, 0.5),
        drops: sceneWaterBuildSeedDrops(count, salt, strength),
      });
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      return true;
    }

    // Object-move event: displace water by an object volume sweeping from its
    // previous to current center (sphere kind=1, cube kind=2, compound kind=3).
    function displace(opts) {
      if (!programs.displacement) return false;
      opts = opts || {};
      var center = opts.center || [sceneWaterNum(entry.objectX, 0), sceneWaterNum(entry.objectY, 0), sceneWaterNum(entry.objectZ, 0)];
      var prev = opts.prevCenter || (entry.objectPreviousSet
        ? [sceneWaterNum(entry.objectPreviousX, 0), sceneWaterNum(entry.objectPreviousY, 0), sceneWaterNum(entry.objectPreviousZ, 0)]
        : center);
      var halfSize = opts.halfSize || [
        sceneWaterNum(entry.objectHalfSizeX, 0.1),
        sceneWaterNum(entry.objectHalfSizeY, 0.1),
        sceneWaterNum(entry.objectHalfSizeZ, 0.1),
      ];
      var spheres = opts.spheres || null;
      runPass("displacement", {
        objectKind: opts.kind != null ? +opts.kind : sceneWaterNum(entry.objectKindValue, 0),
        displacementScale: opts.displacementScale != null ? +opts.displacementScale : Math.max(0, sceneWaterNum(entry.objectDisplacementScale, 1)),
        objectCenter: center,
        objectPrevCenter: prev,
        objectRadius: opts.radius != null ? +opts.radius : sceneWaterNum(entry.objectRadius, 0.1),
        objectHalfSize: halfSize,
        sphereCount: spheres ? Math.floor(spheres.length / 4) : 0,
        spheres: spheres,
      });
      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      return true;
    }

    function dispose() {
      for (var ci = 0; ci < compiledShaders.length; ci++) {
        if (compiledShaders[ci]) gl.deleteProgram(compiledShaders[ci]);
      }
      compiledShaders.length = 0;
      programs = {};
      if (states) {
        gl.deleteTexture(states[0].tex); gl.deleteFramebuffer(states[0].fbo);
        gl.deleteTexture(states[1].tex); gl.deleteFramebuffer(states[1].fbo);
        states = null;
      }
      if (emptyVAO) { gl.deleteVertexArray(emptyVAO); emptyVAO = null; }
    }

    return {
      resolution: resolution,
      format: formatLabel,
      floatLinear: caps.floatLinear,
      // The texture render passes (A2) sample the heightfield from this handle.
      currentStateTexture: function() { return states ? states[current].tex : null; },
      currentStateTarget: function() { return states ? states[current] : null; },
      texelSize: function() { return 1 / resolution; },
      step: step,
      drop: drop,
      seed: seed,
      displace: displace,
      clear: clearState,
      hasSeed: function() { return !!programs.seed; },
      hasDrop: function() { return !!programs.drop; },
      hasDisplacement: function() { return !!programs.displacement; },
      dispose: dispose,
    };
  }

  // ===========================================================================
  // A2: WebGL2 water RENDER passes (pool + object + surface).
  //
  // Companion to the A1 sim driver above. Given a lowered WaterSystemIR entry
  // (carrying the selena *GLES render shaders + shaderDescriptors) and a live
  // sim driver, this draws a depth-tested water scene each frame:
  //   1. pool  — procedural box (floor + 4 walls), tile + caustic + shadow.
  //   2. object — analytic floating sphere/cube via the object-material program.
  //   3. surface (above) — procedural grid VTF-displaced by the state height,
  //      Fresnel reflection (sky cube) + refraction + water tint.
  // The WebGPU render path (16a-scene-webgpu.js) is the behavioral reference for
  // draw order (pool -> object -> surface, depthCompare less-equal) and camera.
  // ===========================================================================

  // Parse a "#rrggbb" / "#rgb" hex string to a normalized [r,g,b] triple.
  function sceneWaterRenderHexColor(value, fallback) {
    var fb = fallback || [0.5, 0.7, 0.9];
    if (typeof value !== "string") return fb.slice();
    var s = value.trim();
    if (s.charAt(0) === "#") s = s.slice(1);
    if (s.length === 3) s = s.charAt(0) + s.charAt(0) + s.charAt(1) + s.charAt(1) + s.charAt(2) + s.charAt(2);
    if (s.length < 6) return fb.slice();
    var r = parseInt(s.slice(0, 2), 16);
    var g = parseInt(s.slice(2, 4), 16);
    var b = parseInt(s.slice(4, 6), 16);
    if (!Number.isFinite(r) || !Number.isFinite(g) || !Number.isFinite(b)) return fb.slice();
    return [r / 255, g / 255, b / 255];
  }

  // A 1x1 RGBA texture (used for the caustic + shadow stubs and as the
  // image-load placeholder for the tile texture).
  function sceneWaterRenderSolidTexture(gl, r, g, b, a) {
    var tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE,
      new Uint8Array([r & 255, g & 255, b & 255, a & 255]));
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.REPEAT);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.REPEAT);
    return tex;
  }

  // Allocate an RGBA8 color render target (texture + FBO) for the caustics and
  // object-shadow render-to-texture passes. LINEAR/CLAMP so the pool + surface
  // can project-sample it without wrap artifacts. Returns null when the FBO is
  // incomplete so the caller can fall back to the solid stub.
  function sceneWaterRenderCreateColorTarget(gl, size) {
    var tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, size, size, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    var fbo = gl.createFramebuffer();
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
    var status = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    if (status !== gl.FRAMEBUFFER_COMPLETE) {
      gl.deleteTexture(tex);
      gl.deleteFramebuffer(fbo);
      return null;
    }
    return { tex: tex, fbo: fbo, size: size };
  }

  // Allocate an RGBA8 color render target WITH a depth renderbuffer (texture +
  // depth + FBO) for the object-texture pre-passes (refraction / reflection /
  // clipped-reflection). The depth attachment lets the mesh self-occlude. The
  // color is LINEAR/CLAMP so the surface can projectively sample it. Returns
  // null when the FBO is incomplete.
  function sceneWaterRenderCreateColorDepthTarget(gl, size) {
    var tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, tex);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, size, size, 0, gl.RGBA, gl.UNSIGNED_BYTE, null);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    var depth = gl.createRenderbuffer();
    gl.bindRenderbuffer(gl.RENDERBUFFER, depth);
    gl.renderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT16, size, size);
    var fbo = gl.createFramebuffer();
    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, tex, 0);
    gl.framebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.RENDERBUFFER, depth);
    var status = gl.checkFramebufferStatus(gl.FRAMEBUFFER);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);
    gl.bindRenderbuffer(gl.RENDERBUFFER, null);
    if (status !== gl.FRAMEBUFFER_COMPLETE) {
      gl.deleteTexture(tex);
      gl.deleteRenderbuffer(depth);
      gl.deleteFramebuffer(fbo);
      return null;
    }
    return { tex: tex, depth: depth, fbo: fbo, size: size };
  }

  // Find the live world-space mesh for the active water object inside a render
  // bundle. The scene core bakes every mesh object's world-space triangle soup
  // into bundle.worldMeshPositions / worldMeshUVs (non-indexed), with each
  // bundle.meshObjects entry carrying a vertexOffset / vertexCount window into
  // them. We match the active object by id (float-duck / float-torus), falling
  // back to the first visible mesh carrying a model material. Returns
  // { positions, uvs, count, id, texture } (typed sub-arrays) or null.
  function sceneWaterRenderFindBundleMesh(bundle, targetID) {
    if (!bundle) return null;
    var objects = Array.isArray(bundle.meshObjects) ? bundle.meshObjects : null;
    var worldPos = bundle.worldMeshPositions;
    var worldUV = bundle.worldMeshUVs;
    var worldNormal = bundle.worldMeshNormals;
    if (!objects || !objects.length || !worldPos || !worldUV) return null;
    var chosen = null;
    for (var i = 0; i < objects.length; i++) {
      var o = objects[i];
      if (!o) continue;
      var count = Math.floor(sceneWaterNum(o.vertexCount, 0));
      var offset = Math.floor(sceneWaterNum(o.vertexOffset, 0));
      if (count <= 0 || offset < 0) continue;
      var id = String(o.id || "");
      if (targetID && (id === targetID || id.indexOf(targetID + ":") === 0 || id.indexOf(targetID + "-prim") === 0)) {
        chosen = { obj: o, offset: offset, count: count, id: id };
        break;
      }
      if (!chosen) chosen = { obj: o, offset: offset, count: count, id: id };
    }
    if (!chosen) return null;
    var start = chosen.offset * 3;
    var endP = start + chosen.count * 3;
    var startUV = chosen.offset * 2;
    var endUV = startUV + chosen.count * 2;
    if (endP > worldPos.length || endUV > worldUV.length) return null;
    var positions = worldPos instanceof Float32Array
      ? worldPos.subarray(start, endP)
      : new Float32Array(worldPos.slice(start, endP));
    var uvs = worldUV instanceof Float32Array
      ? worldUV.subarray(startUV, endUV)
      : new Float32Array(worldUV.slice(startUV, endUV));
    // Normals are baked world-space, same order/stride as positions (see
    // 10-runtime-scene-core.js: worldMeshPositions/worldMeshNormals/worldMeshUVs
    // are pushed together per vertex). Optional/defensive: a bundle lacking
    // worldMeshNormals (or a length mismatch) falls back to null and the
    // caller (refreshMeshUpload) uses a safe non-NaN default normal.
    var normals = null;
    if (worldNormal && endP <= worldNormal.length) {
      normals = worldNormal instanceof Float32Array
        ? worldNormal.subarray(start, endP)
        : new Float32Array(worldNormal.slice(start, endP));
    }
    return {
      positions: positions, normals: normals, uvs: uvs, count: chosen.count,
      id: chosen.id, texture: chosen.obj && chosen.obj.texture || "",
    };
  }

  // Map the live active water object to the scene mesh-object id the bundle
  // carries (mirrors the WebGPU sceneWaterActiveObjectID).
  function sceneWaterRenderActiveMeshID(activeObject, objectSubtype, objectKind) {
    var a = String(activeObject || "").trim().toLowerCase();
    var s = String(objectSubtype || "").trim().toLowerCase();
    if (s.indexOf("duck") >= 0 || a.indexOf("duck") >= 0) return "float-duck";
    if (s.indexOf("torus") >= 0 || a.indexOf("torus") >= 0) return "float-torus";
    if (a.indexOf("sphere") >= 0) return "float-sphere";
    if (a.indexOf("cube") >= 0 || a.indexOf("box") >= 0) return "float-cube";
    return "";
  }

  // Load a 2D texture from a URL. Returns a texture immediately (1x1 grey
  // placeholder) and uploads the decoded image when it arrives.
  function sceneWaterRenderLoad2D(gl, url) {
    var tex = sceneWaterRenderSolidTexture(gl, 128, 128, 128, 255);
    if (!url || typeof Image === "undefined") return tex;
    var img = new Image();
    img.crossOrigin = "anonymous";
    img.onload = function() {
      gl.bindTexture(gl.TEXTURE_2D, tex);
      gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
      gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, img);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.REPEAT);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.REPEAT);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
      gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
      var w = img.width, h = img.height;
      if ((w & (w - 1)) === 0 && (h & (h - 1)) === 0) {
        gl.generateMipmap(gl.TEXTURE_2D);
      } else {
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
      }
    };
    img.src = url;
    return tex;
  }

  // Resolve the 6 cube-map face URLs from a base, mirroring the WebGPU path's
  // convention (no -Y image; +Y is reused for both poles).
  function sceneWaterRenderCubeFaceURLs(value) {
    var base = typeof value === "string" ? value.trim() : "";
    if (!base) return null;
    if (base.indexOf("{face}") >= 0) {
      return ["xpos", "xneg", "ypos", "ypos", "zpos", "zneg"].map(function(f) { return base.replace("{face}", f); });
    }
    if (base.charAt(base.length - 1) !== "/") base += "/";
    return ["xpos.jpg", "xneg.jpg", "ypos.jpg", "ypos.jpg", "zpos.jpg", "zneg.jpg"].map(function(f) { return base + f; });
  }

  // Load a cube texture from a base URL. Returns a texture immediately (a
  // bluish 1x1-per-face placeholder) and uploads each decoded face as it loads.
  function sceneWaterRenderLoadCube(gl, value) {
    var faceTargets = [
      gl.TEXTURE_CUBE_MAP_POSITIVE_X, gl.TEXTURE_CUBE_MAP_NEGATIVE_X,
      gl.TEXTURE_CUBE_MAP_POSITIVE_Y, gl.TEXTURE_CUBE_MAP_NEGATIVE_Y,
      gl.TEXTURE_CUBE_MAP_POSITIVE_Z, gl.TEXTURE_CUBE_MAP_NEGATIVE_Z,
    ];
    var placeholders = [
      [150, 190, 210], [110, 155, 180], [190, 220, 232],
      [190, 220, 232], [125, 170, 195], [90, 135, 165],
    ];
    var tex = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_CUBE_MAP, tex);
    for (var i = 0; i < 6; i++) {
      var c = placeholders[i];
      gl.texImage2D(faceTargets[i], 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE,
        new Uint8Array([c[0], c[1], c[2], 255]));
    }
    gl.texParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_CUBE_MAP, gl.TEXTURE_WRAP_R, gl.CLAMP_TO_EDGE);
    var urls = sceneWaterRenderCubeFaceURLs(value);
    if (!urls || typeof Image === "undefined") return tex;
    for (var f = 0; f < 6; f++) {
      (function(target, url) {
        var img = new Image();
        img.crossOrigin = "anonymous";
        img.onload = function() {
          gl.bindTexture(gl.TEXTURE_CUBE_MAP, tex);
          gl.pixelStorei(gl.UNPACK_FLIP_Y_WEBGL, false);
          gl.texImage2D(target, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, img);
        };
        img.src = url;
      })(faceTargets[f], urls[f]);
    }
    return tex;
  }

  // Set every uniform-block field named in a render descriptor from a values
  // map. Handles mat4/mat3, float, vec2/3/4, and fixed-size vec arrays.
  function sceneWaterRenderSetUniforms(gl, program, descriptor, values) {
    var block = descriptor && descriptor.uniformBlock;
    var fields = block && Array.isArray(block.fields) ? block.fields : [];
    for (var i = 0; i < fields.length; i++) {
      var field = fields[i];
      if (!field || !field.name) continue;
      var v = values[field.name];
      if (v === undefined || v === null) continue;
      var count = field.count && field.count > 1 ? field.count : 0;
      if (count > 0) {
        sceneWaterSetArrayUniform(gl, program, field, v, count);
        continue;
      }
      var loc = gl.getUniformLocation(program, field.name);
      if (!loc) continue;
      switch (field.type) {
        case "mat4": gl.uniformMatrix4fv(loc, false, v); break;
        case "mat3": gl.uniformMatrix3fv(loc, false, v); break;
        case "float": gl.uniform1f(loc, +v); break;
        case "vec2": gl.uniform2f(loc, v[0], v[1]); break;
        case "vec3": gl.uniform3f(loc, v[0], v[1], v[2]); break;
        case "vec4": gl.uniform4f(loc, v[0], v[1], v[2], v[3]); break;
        default: break;
      }
    }
  }

  // Bind an ordered list of samplers to sequential texture units and point each
  // GLSL sampler uniform at its unit. list entries: {name, target, tex}.
  function sceneWaterRenderBindSamplers(gl, program, list) {
    for (var i = 0; i < list.length; i++) {
      var b = list[i];
      gl.activeTexture(gl.TEXTURE0 + i);
      gl.bindTexture(b.target, b.tex);
      var loc = gl.getUniformLocation(program, b.name);
      if (loc) gl.uniform1i(loc, i);
    }
  }

  // The sampler-uniform name carrying the ping-pong state for one render pass.
  // A constant (0,1,0) normal repeated `count` times: the safe fallback used
  // when a mesh soup lookup can't produce real per-vertex normals (see
  // refreshMeshUpload). Matches the pre-fix flat-shading "up" vector so a
  // missing-data edge case degrades to the old flat look instead of NaN.
  function sceneWaterRenderConstantUpNormals(count) {
    var out = new Float32Array(Math.max(0, count) * 3);
    for (var i = 0; i < out.length; i += 3) {
      out[i] = 0; out[i + 1] = 1; out[i + 2] = 0;
    }
    return out;
  }

  function sceneWaterRenderStateUniform(descriptor) {
    var states = descriptor && Array.isArray(descriptor.states) ? descriptor.states : [];
    if (states[0] && states[0].gl && states[0].gl.uniform) return states[0].gl.uniform;
    return "stateTex";
  }

  // The object-material GLES fragment samples the live heightfield via the `uv`
  // varying (stateAt(vUv)) and decides "submerged" from worldPos.y. The WebGPU
  // sibling samples the water at the fragment's WORLD xz instead, so we bake the
  // mesh uv as the pool's water-UV (worldXZ -> [0,1]) — this aligns the caustic
  // boost + submerged test with the object's actual footprint instead of the raw
  // parametric uv, which is what makes the object read as a lit solid (not a flat
  // cyan blob sampling the wrong cell).
  function sceneWaterRenderWaterUV(x, z, poolWidth, poolLength) {
    var duw = Math.max(poolWidth * 2, 0.001);
    var dul = Math.max(poolLength * 2, 0.001);
    var u = (x / duw) + 0.5;
    var v = (z / dul) + 0.5;
    return [Math.min(1, Math.max(0, u)), Math.min(1, Math.max(0, v))];
  }

  // Upload a baked position(vec3)+uv(vec2) indexed mesh and return its VAO.
  function sceneWaterRenderUploadMesh(gl, program, positions, normals, uvs, indices) {
    var vao = gl.createVertexArray();
    gl.bindVertexArray(vao);
    var posLoc = gl.getAttribLocation(program, "position");
    var normalLoc = gl.getAttribLocation(program, "normal");
    var uvLoc = gl.getAttribLocation(program, "uv");
    var posBuf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, posBuf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(positions), gl.STATIC_DRAW);
    if (posLoc >= 0) { gl.enableVertexAttribArray(posLoc); gl.vertexAttribPointer(posLoc, 3, gl.FLOAT, false, 0, 0); }
    var normalBuf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, normalBuf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(normals), gl.STATIC_DRAW);
    if (normalLoc >= 0) { gl.enableVertexAttribArray(normalLoc); gl.vertexAttribPointer(normalLoc, 3, gl.FLOAT, false, 0, 0); }
    var uvBuf = gl.createBuffer();
    gl.bindBuffer(gl.ARRAY_BUFFER, uvBuf);
    gl.bufferData(gl.ARRAY_BUFFER, new Float32Array(uvs), gl.STATIC_DRAW);
    if (uvLoc >= 0) { gl.enableVertexAttribArray(uvLoc); gl.vertexAttribPointer(uvLoc, 2, gl.FLOAT, false, 0, 0); }
    var idxBuf = gl.createBuffer();
    gl.bindBuffer(gl.ELEMENT_ARRAY_BUFFER, idxBuf);
    gl.bufferData(gl.ELEMENT_ARRAY_BUFFER, new Uint16Array(indices), gl.STATIC_DRAW);
    gl.bindVertexArray(null);
    return { vao: vao, count: indices.length, buffers: [posBuf, normalBuf, uvBuf, idxBuf] };
  }

  // Generate a UV-sphere mesh with world-space positions baked in (center +
  // radius * unit). The object-material vertex shader passes `position` straight
  // through as worldPos, so positions must already be in world space. A sphere's
  // normal is just the normalized center->vertex direction, i.e. the same unit
  // vector (nx, ny, nz) used to place the vertex, so it is baked world-space too
  // (mirrors the WebGPU/generic-PBR convention: normalMatrix is always identity
  // there because normals reach the shader already in world space).
  function sceneWaterRenderBuildSphere(gl, program, center, radius, poolWidth, poolLength, segments, rings) {
    segments = segments || 32;
    rings = rings || 24;
    var positions = [];
    var normals = [];
    var uvs = [];
    for (var y = 0; y <= rings; y++) {
      var v = y / rings;
      var phi = v * Math.PI;
      for (var x = 0; x <= segments; x++) {
        var u = x / segments;
        var theta = u * Math.PI * 2;
        var nx = Math.sin(phi) * Math.cos(theta);
        var ny = Math.cos(phi);
        var nz = Math.sin(phi) * Math.sin(theta);
        var wx = center[0] + nx * radius;
        var wy = center[1] + ny * radius;
        var wz = center[2] + nz * radius;
        positions.push(wx, wy, wz);
        normals.push(nx, ny, nz);
        var wuv = sceneWaterRenderWaterUV(wx, wz, poolWidth, poolLength);
        uvs.push(wuv[0], wuv[1]);
      }
    }
    var indices = [];
    var stride = segments + 1;
    for (var ry = 0; ry < rings; ry++) {
      for (var rx = 0; rx < segments; rx++) {
        var a = ry * stride + rx;
        var b = a + stride;
        indices.push(a, b, a + 1, a + 1, b, b + 1);
      }
    }
    return sceneWaterRenderUploadMesh(gl, program, positions, normals, uvs, indices);
  }

  // Generate an axis-aligned box mesh with world-space positions baked in
  // (center +/- half on each axis). Used for objectKind == cube so the floating
  // object is a real box, not the sphere approximation. uv is baked as the pool
  // water-UV like the sphere so the submerged + caustic logic matches.
  function sceneWaterRenderBuildBox(gl, program, center, half, poolWidth, poolLength) {
    var hx = Math.max(half[0], 0.001), hy = Math.max(half[1], 0.001), hz = Math.max(half[2], 0.001);
    var cx = center[0], cy = center[1], cz = center[2];
    // 8 corners.
    var c = [
      [cx - hx, cy - hy, cz - hz], [cx + hx, cy - hy, cz - hz],
      [cx + hx, cy + hy, cz - hz], [cx - hx, cy + hy, cz - hz],
      [cx - hx, cy - hy, cz + hz], [cx + hx, cy - hy, cz + hz],
      [cx + hx, cy + hy, cz + hz], [cx - hx, cy + hy, cz + hz],
    ];
    // 6 faces, each as two triangles over its 4 corner indices, with the
    // matching flat face normal (world-space, baked per-vertex like position).
    var faces = [
      [0, 3, 2, 1], // -z
      [4, 5, 6, 7], // +z
      [0, 4, 7, 3], // -x
      [1, 2, 6, 5], // +x
      [3, 7, 6, 2], // +y
      [0, 1, 5, 4], // -y
    ];
    var faceNormals = [
      [0, 0, -1],
      [0, 0, 1],
      [-1, 0, 0],
      [1, 0, 0],
      [0, 1, 0],
      [0, -1, 0],
    ];
    var positions = [];
    var normals = [];
    var uvs = [];
    var indices = [];
    for (var f = 0; f < faces.length; f++) {
      var q = faces[f];
      var n = faceNormals[f];
      var base = positions.length / 3;
      for (var k = 0; k < 4; k++) {
        var p = c[q[k]];
        positions.push(p[0], p[1], p[2]);
        normals.push(n[0], n[1], n[2]);
        var wuv = sceneWaterRenderWaterUV(p[0], p[2], poolWidth, poolLength);
        uvs.push(wuv[0], wuv[1]);
      }
      indices.push(base, base + 1, base + 2, base, base + 2, base + 3);
    }
    return sceneWaterRenderUploadMesh(gl, program, positions, normals, uvs, indices);
  }

  // Map an objectKind string to the float the render shaders expect:
  // none=0, sphere=1, cube=2, compound=3 (mirrors the WebGPU path).
  function sceneWaterRenderObjectKind(entry) {
    var raw = entry && (entry.objectKind || entry.activeObject) || "";
    var v = String(raw).trim().toLowerCase();
    if (!v || v === "none") return 0;
    if (v.indexOf("sphere") >= 0 || v.indexOf("ball") >= 0) return 1;
    if (v.indexOf("cube") >= 0 || v.indexOf("box") >= 0) return 2;
    return 3;
  }

  // Mirrors sceneWaterPoolShapeRounded in 16a-scene-webgpu.js verbatim: true
  // iff entry.poolShape selects the rounded-corner pool variant that pool.sel's
  // `if (poolShape > 0.5 && cornerRadius > 0.0001)` vertex() branch models.
  function sceneWaterPoolShapeRounded(entry) {
    if (!entry || typeof entry.poolShape !== "string") return false;
    var value = entry.poolShape.trim().toLowerCase();
    return value === "rounded box" || value === "rounded" || value === "roundbox";
  }

  // createSceneWaterRendererWebGL: a self-contained WebGL2 renderer for the
  // water demo, used only by the temporary force-WebGL verification hook (A2).
  // It owns the sim driver, render programs, textures and a sphere mesh, and
  // exposes the standard renderer interface (kind/render/dispose) the mount
  // drives. Returns null when the float sim or required render programs are
  // unavailable, so the caller can fall back to the normal renderer path.
  function createSceneWaterRendererWebGL(gl, canvas, entry) {
    if (!gl || !entry) return null;
    var sim = createSceneWaterSimWebGL(gl, entry);
    if (!sim) return null;

    var descriptors = entry.shaderDescriptors && typeof entry.shaderDescriptors === "object"
      ? entry.shaderDescriptors : {};

    function compile(vsSrc, fsSrc, label) {
      if (!vsSrc || !fsSrc) return null;
      // selena's GLES emitter now declares `precision highp float;` for fragment
      // stages, matching the ES 3.00 vertex default (highp), so the shared
      // uniforms (mvp, poolWidth, ...) agree in precision across stages and the
      // program links without the earlier mediump->highp fragment rewrite.
      var vs = scenePBRCompileShader(gl, gl.VERTEX_SHADER, vsSrc);
      if (!vs) return null;
      var fs = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, fsSrc);
      if (!fs) { gl.deleteShader(vs); return null; }
      var prog = scenePBRLinkProgram(gl, vs, fs, label);
      gl.deleteShader(vs); gl.deleteShader(fs);
      return prog;
    }

    var poolProgram = compile(entry.poolVertexGLES, entry.poolFragmentGLES, "Water pool");
    var surfaceProgram = compile(entry.surfaceVertexGLES, entry.surfaceFragmentGLES, "Water surface");
    var objectProgram = compile(entry.objectMaterialVertexGLES, entry.objectMaterialFragmentGLES, "Water object");
    // A2-refine-2: the textured glTF duck mesh program (duck-material.sel GLES).
    // Samples a per-vertex modelTexture for albedo + the live stateTex for the
    // submerged tint/caustic. Drives both the direct duck draw and the
    // object-texture (refraction/reflection/clipped) pre-passes the surface
    // samples. Optional — when absent, the mesh path falls back to objectProgram.
    var duckProgram = compile(entry.duckMaterialVertexGLES, entry.duckMaterialFragmentGLES, "Water duck");
    // Caustics: a fullscreen pass that reads the live sim state and emits the
    // refracted-light caustic pattern into a dedicated RTT. Object shadow: an
    // analytic sphere/cube fullscreen pass written into a second RTT in water-UV
    // space. Both are optional — on compile failure the pool/surface fall back to
    // the solid stubs so the renderer still works.
    var causticsProgram = compile(entry.causticsVertexGLES, entry.causticsFragmentGLES, "Water caustics");
    var shadowProgram = compile(entry.objectShadowVertexGLES, entry.objectShadowFragmentGLES, "Water object shadow");
    // Compound-shadow: the WebGL2-only footprint shadow pass for COMPOUND
    // objects (TorusKnot / Duck; objectKind >= 2.5), which object-shadow.sel
    // cannot express (it only supports sphere/cube). Additive and optional —
    // when absent or when no proxy spheres are available the renderer falls
    // back to the analytic shadowProgram (a single bounding-sphere blob).
    var compoundShadowProgram = compile(entry.compoundShadowVertexGLES, entry.compoundShadowFragmentGLES, "Water compound shadow");
    if (!poolProgram || !surfaceProgram) {
      sim.dispose();
      if (poolProgram) gl.deleteProgram(poolProgram);
      if (surfaceProgram) gl.deleteProgram(surfaceProgram);
      if (objectProgram) gl.deleteProgram(objectProgram);
      if (duckProgram) gl.deleteProgram(duckProgram);
      if (causticsProgram) gl.deleteProgram(causticsProgram);
      if (shadowProgram) gl.deleteProgram(shadowProgram);
      if (compoundShadowProgram) gl.deleteProgram(compoundShadowProgram);
      return null;
    }

    var poolDesc = sceneWaterParseDescriptor(descriptors.pool);
    var surfaceDesc = sceneWaterParseDescriptor(descriptors.surface);
    var objectDesc = sceneWaterParseDescriptor(descriptors.objectMaterial);
    var duckDesc = sceneWaterParseDescriptor(descriptors.duckMaterial);
    var causticsDesc = sceneWaterParseDescriptor(descriptors.caustics);
    var shadowDesc = sceneWaterParseDescriptor(descriptors.objectShadow);
    var compoundShadowDesc = sceneWaterParseDescriptor(descriptors.compoundShadow);

    // Pool dims / optics from the entry.
    var poolWidth = sceneWaterNum(entry.poolWidth, 1);
    var poolLength = sceneWaterNum(entry.poolLength, 1);
    var poolHeight = sceneWaterNum(entry.poolHeight, 1);

    // Rounded-corner pool wiring: mirrors 16a-scene-webgpu.js's
    // sceneWaterUniformData/drawWaterPoolEntries pairing exactly.
    // poolMaxCornerRadius derives from poolWidth/poolLength (fixed at
    // construction time; runtime pool-shape changes never resize the pool
    // itself), clamped to [0, min(poolWidth, poolLength) - 0.001] matching
    // the WebGPU path's system.waterCornerRadius derivation.
    //
    // The rounded flag / clamped uniform value / draw-call vertex count are
    // deliberately NOT captured here from `entry` -- they are recomputed
    // every drawFrame() from the live bundle entry (see the live* locals
    // near liveEntry below), because `entry` is only a renderer-creation-time
    // snapshot: a runtime poolShape/cornerRadius change from the control form
    // (e.g. switching to "Rounded Box") lands in the bundle's
    // waterSystems[0], never mutates this captured object in place. This
    // mirrors drawFrame's pre-existing liveEntry/liveKindNum pattern used for
    // runtime object-selection changes, and WebGPU's drawWaterPoolEntries,
    // which reads `system.entry` fresh every pass for the identical reason.
    var poolMaxCornerRadius = Math.max(0, Math.min(poolWidth, poolLength) - 0.001);
    // Geometry sizes. The box pool is 5 faces (floor + 4 walls) * 6 vertices;
    // the rounded pool is pool.sel's 44*3 floor-fan + 44*6 wall-strip = 44*9
    // (see livePoolVertexCount in drawFrame for the live per-frame pick).
    var gridResolution = Math.max(2, Math.min(160, sim.resolution));
    var surfaceCells = gridResolution - 1;
    var surfaceVertexCount = surfaceCells * surfaceCells * 6;
    var emptyVAO = gl.createVertexArray();

    var normalScale = sceneWaterNum(entry.normalScale, 1);
    var lightDir = [
      sceneWaterNum(entry.lightDirectionX, 2),
      sceneWaterNum(entry.lightDirectionY, 2),
      sceneWaterNum(entry.lightDirectionZ, -1),
    ];
    var waterColor = sceneWaterRenderHexColor(entry.shallowColor, [0.48, 0.82, 0.92]);
    var objectKind = sceneWaterRenderObjectKind(entry);
    var objectCenter = [sceneWaterNum(entry.objectX, 0), sceneWaterNum(entry.objectY, -0.5), sceneWaterNum(entry.objectZ, 0)];
    var objectHalf = [sceneWaterNum(entry.objectHalfSizeX, 0), sceneWaterNum(entry.objectHalfSizeY, 0), sceneWaterNum(entry.objectHalfSizeZ, 0)];
    var objectRadius = sceneWaterNum(entry.objectRadius, 0.25);
    var opticsEnable = (entry.reflection || entry.refraction) ? 1 : 1;
    var opticsCaustic = entry.caustics ? 1 : 0;
    var identity3 = new Float32Array([1, 0, 0, 0, 1, 0, 0, 0, 1]);
    var objectEnabled = objectKind > 0 ? 1 : 0;
    // vec4(halfX, halfY, halfZ, radius) — matches the caustics shader's
    // objectHalfRadius (sphere reads .w, cube reads .xz). Mirrors the WebGPU
    // objectHalfSizeRadius packing.
    var objectHalfRadius = [objectHalf[0], objectHalf[1], objectHalf[2], objectRadius];

    // Textures: real tile + sky cube. Caustics + object-shadow are rendered to
    // their own RTTs each frame (sizes mirror the WebGPU path: caustics =
    // causticsResolution, shadow = objectShadowResolution). The solid stubs
    // remain as the fallback when the RTT or its program is unavailable.
    var tileTex = sceneWaterRenderLoad2D(gl, entry.tileTexture);
    var skyTex = sceneWaterRenderLoadCube(gl, entry.cubeMap);
    var causticStub = sceneWaterRenderSolidTexture(gl, 14, 16, 18, 255); // ~0.06 grey
    var shadowStub = sceneWaterRenderSolidTexture(gl, 0, 0, 0, 255);     // 0 => no shadow

    var causticsSize = Math.max(64, Math.min(2048, Math.floor(sceneWaterNum(entry.causticsResolution, 1024)) || 1024));
    var shadowSize = Math.max(64, Math.min(2048, Math.floor(sceneWaterNum(entry.objectShadowResolution, 512)) || 512));
    var causticsTarget = causticsProgram ? sceneWaterRenderCreateColorTarget(gl, causticsSize) : null;
    // One shared shadow RTT: either shadowProgram (sphere/cube) or
    // compoundShadowProgram (compound) renders into it per frame — the pool
    // pass only ever samples the single "shadowTexture" result.
    var shadowTarget = (shadowProgram || compoundShadowProgram) ? sceneWaterRenderCreateColorTarget(gl, shadowSize) : null;
    // Compound-shadow proxy-sphere uniform scratch: vec4(offsetX, offsetY,
    // offsetZ, radius) per sphere, raw world units matching the objectCenterX/Z
    // convention this renderer already uses for the analytic shadow pass (see
    // liveCenter below) — NOT the WebGPU path's poolWidth/poolLength-normalized
    // storage-buffer convention. Capped to the compound-shadow.sel array<vec4,32>.
    var COMPOUND_SHADOW_MAX_SPHERES = 32;
    var compoundShadowSpheres = new Float32Array(COMPOUND_SHADOW_MAX_SPHERES * 4);
    // Fills compoundShadowSpheres from a live entry's objectDisplacementSpheres
    // ({offsetX, offsetY, offsetZ, radius} objects, per WaterDemoData's
    // duckDisplacementSpheres()/torusKnotDisplacementSpheres()) and returns the
    // active sphere count. Zero-radius / missing entries are skipped, mirroring
    // sceneWaterDisplacementSpheres' skip-non-positive-radius rule.
    function fillCompoundShadowSpheres(list) {
      var count = 0;
      if (Array.isArray(list)) {
        for (var i = 0; i < list.length && count < COMPOUND_SHADOW_MAX_SPHERES; i++) {
          var s = list[i];
          var radius = s ? +s.radius : 0;
          if (!(radius > 0)) continue;
          var base = count * 4;
          compoundShadowSpheres[base] = sceneWaterNum(s.offsetX, 0);
          compoundShadowSpheres[base + 1] = sceneWaterNum(s.offsetY, 0);
          compoundShadowSpheres[base + 2] = sceneWaterNum(s.offsetZ, 0);
          compoundShadowSpheres[base + 3] = radius;
          count++;
        }
      }
      for (var z = count * 4; z < compoundShadowSpheres.length; z++) compoundShadowSpheres[z] = 0;
      return count;
    }
    // Live caustic/shadow textures the pool + surface sample; fall back to stubs.
    var causticTex = causticsTarget ? causticsTarget.tex : causticStub;
    var shadowTex = shadowTarget ? shadowTarget.tex : shadowStub;

    // A2-refine-2: object-texture pre-pass targets. For MESH objects (duck /
    // torus-knot) the surface can't raytrace the real geometry, so — mirroring
    // the WebGPU path — we render the mesh to three offscreen color targets each
    // frame (refraction = normal camera, reflection = camera mirrored across the
    // water plane, clipped-reflection = reflection that discards submerged
    // fragments) and the surface projectively samples them where its refraction /
    // reflection rays hit the mesh's bounds sphere. Capped at 512² to match the
    // capped WebGPU object-texture size. Each carries a depth renderbuffer so the
    // duck self-occludes correctly. An empty transparent stub backs the surface
    // sampler when the mesh path is inactive.
    var objectTextureSize = 512;
    var objectRefractionTarget = duckProgram || objectProgram
      ? sceneWaterRenderCreateColorDepthTarget(gl, objectTextureSize) : null;
    var objectReflectionTarget = objectRefractionTarget
      ? sceneWaterRenderCreateColorDepthTarget(gl, objectTextureSize) : null;
    var objectClippedTarget = objectReflectionTarget
      ? sceneWaterRenderCreateColorDepthTarget(gl, objectTextureSize) : null;
    if (!objectReflectionTarget || !objectClippedTarget) {
      // Partial allocation — drop them all so the surface uses the empty stub.
      [objectRefractionTarget, objectReflectionTarget, objectClippedTarget].forEach(function(t) {
        if (t) { gl.deleteTexture(t.tex); gl.deleteRenderbuffer(t.depth); gl.deleteFramebuffer(t.fbo); }
      });
      objectRefractionTarget = objectReflectionTarget = objectClippedTarget = null;
    }
    var objectTextureStub = sceneWaterRenderSolidTexture(gl, 0, 0, 0, 0); // transparent => no mesh
    var objectRefractionTex = objectRefractionTarget ? objectRefractionTarget.tex : objectTextureStub;
    var objectReflectionTex = objectReflectionTarget ? objectReflectionTarget.tex : objectTextureStub;
    var objectClippedTex = objectClippedTarget ? objectClippedTarget.tex : objectTextureStub;

    // Persistent mesh upload (rebuilt each frame from the live bundle's
    // world-space mesh vertices for the active object) + its model texture.
    var meshUpload = null;       // { vao, posBuf, normBuf, uvBuf, count }
    var meshUploadKey = "";      // identity guard so we only re-upload on change
    var meshModelTex = null;     // glTF albedo texture for the active mesh
    var meshModelTexURL = "";
    // Projection matrices the surface uses to sample the object-texture targets.
    // refraction = current mvp; reflection = mvp * reflectAcrossWaterPlane.
    var objectRefractionMatrix = new Float32Array(16);
    var objectReflectionMatrix = new Float32Array(16);
    var reflectAcrossWater = new Float32Array([1, 0, 0, 0, 0, -1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1]);

    // Analytic object meshes: build BOTH a sphere and a box up front so that
    // switching the active object (sphere ↔ cube) takes effect without rebuilding
    // GL resources each frame. The render loop selects
    //   objectMesh = liveKindNum === 2 ? boxMesh : sphereMesh
    // per frame. Building both at init costs one extra buffer upload and avoids
    // any per-frame GL leak (no dynamic alloc/free on selection change).
    var sphereMesh = null;
    var boxMesh = null;
    if (objectProgram) {
      var initBoxHalf = [
        objectHalf[0] > 0 ? objectHalf[0] : objectRadius,
        objectHalf[1] > 0 ? objectHalf[1] : objectRadius,
        objectHalf[2] > 0 ? objectHalf[2] : objectRadius,
      ];
      sphereMesh = sceneWaterRenderBuildSphere(gl, objectProgram, objectCenter, Math.max(objectRadius, 0.001), poolWidth, poolLength, 32, 24);
      boxMesh = sceneWaterRenderBuildBox(gl, objectProgram, objectCenter, initBoxHalf, poolWidth, poolLength);
    }
    // Selected per-frame in the render loop based on liveKindNum.
    var objectMesh = null;

    // Frame clock for the caustic shimmer term (time uniform), in seconds.
    var causticsStart = (typeof performance !== "undefined" && performance.now) ? performance.now() : Date.now();

    // Seed a clearly visible set of ripples for the static screenshot, then a
    // couple of strong drops. The normal interaction layer drives these live;
    // here we just want motion on the surface for verification.
    var seeded = false;
    function primeRipples() {
      if (seeded) return;
      seeded = true;
      if (sim.hasSeed()) sim.seed({ count: 16, strength: 0.35, radius: 0.05 });
      if (sim.hasDrop()) {
        sim.drop({ x: 0.0, z: 0.0, strength: 0.6, radius: 0.05 });
        sim.drop({ x: -0.35, z: 0.25, strength: 0.5, radius: 0.045 });
        sim.drop({ x: 0.4, z: -0.3, strength: 0.5, radius: 0.045 });
      }
    }

    var disposed = false;
    var lastBundle = null;
    var rafId = 0;

    // (Re)upload the live world-space mesh soup into a persistent VAO and point
    // `position`/`normal`/`uv` at the active program's attribute locations. The
    // duck bobs each frame, so positions (and normals) are streamed
    // (DYNAMIC_DRAW) every call.
    function refreshMeshUpload(prog, mesh) {
      if (!prog || !mesh || !mesh.count) return;
      if (!meshUpload) {
        meshUpload = {
          vao: gl.createVertexArray(),
          posBuf: gl.createBuffer(),
          normBuf: gl.createBuffer(),
          uvBuf: gl.createBuffer(),
          count: 0,
        };
      }
      gl.bindVertexArray(meshUpload.vao);
      var posLoc = gl.getAttribLocation(prog, "position");
      gl.bindBuffer(gl.ARRAY_BUFFER, meshUpload.posBuf);
      gl.bufferData(gl.ARRAY_BUFFER, mesh.positions, gl.DYNAMIC_DRAW);
      if (posLoc >= 0) { gl.enableVertexAttribArray(posLoc); gl.vertexAttribPointer(posLoc, 3, gl.FLOAT, false, 0, 0); }
      var normalLoc = gl.getAttribLocation(prog, "normal");
      // mesh.normals is baked world-space (see sceneWaterRenderFindBundleMesh);
      // if a bundle is missing it (defensive), fall back to a constant up
      // normal for every vertex rather than leaving the attribute at its
      // WebGL default (0,0,0), which normalize()s to NaN in the shader.
      var normalData = mesh.normals && mesh.normals.length === mesh.count * 3
        ? mesh.normals
        : sceneWaterRenderConstantUpNormals(mesh.count);
      gl.bindBuffer(gl.ARRAY_BUFFER, meshUpload.normBuf);
      gl.bufferData(gl.ARRAY_BUFFER, normalData, gl.DYNAMIC_DRAW);
      if (normalLoc >= 0) { gl.enableVertexAttribArray(normalLoc); gl.vertexAttribPointer(normalLoc, 3, gl.FLOAT, false, 0, 0); }
      var uvLoc = gl.getAttribLocation(prog, "uv");
      gl.bindBuffer(gl.ARRAY_BUFFER, meshUpload.uvBuf);
      gl.bufferData(gl.ARRAY_BUFFER, mesh.uvs, gl.DYNAMIC_DRAW);
      if (uvLoc >= 0) { gl.enableVertexAttribArray(uvLoc); gl.vertexAttribPointer(uvLoc, 2, gl.FLOAT, false, 0, 0); }
      meshUpload.count = mesh.count;
      meshUploadKey = mesh.id + ":" + mesh.count;
      gl.bindVertexArray(null);
    }

    // Render the active mesh into one object-texture target with the given
    // view-projection + texture-pass mode (1 = refraction/reflection, 2 = clipped
    // reflection: discards submerged fragments). Mirrors the WebGPU
    // renderWaterObjectMeshTargetPass: clear transparent, depth-test on, no cull.
    function renderMeshTextureTarget(prog, desc, target, vpMatrix, mode, modelTex, stateTex) {
      if (!prog || !target || !meshUpload || !meshUpload.count) return;
      gl.bindFramebuffer(gl.FRAMEBUFFER, target.fbo);
      gl.viewport(0, 0, target.size, target.size);
      gl.enable(gl.DEPTH_TEST);
      gl.depthFunc(gl.LEQUAL);
      gl.depthMask(true);
      gl.disable(gl.BLEND);
      gl.disable(gl.CULL_FACE);
      gl.clearColor(0, 0, 0, 0);
      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);
      gl.useProgram(prog);
      gl.bindVertexArray(meshUpload.vao);
      var samplers = [{ name: sceneWaterRenderStateUniform(desc), target: gl.TEXTURE_2D, tex: stateTex }];
      if (modelTex) samplers.push({ name: "modelTexture", target: gl.TEXTURE_2D, tex: modelTex });
      sceneWaterRenderBindSamplers(gl, prog, samplers);
      sceneWaterRenderSetUniforms(gl, prog, desc, {
        mvp: vpMatrix, normalMatrix: identity3, lightDir: lightDir, poolHeight: poolHeight,
        baseColor: [1, 1, 1, 1], isTexturePass: 1, texturePassMode: mode,
      });
      gl.drawArrays(gl.TRIANGLES, 0, meshUpload.count);
      gl.bindVertexArray(null);
    }

    function drawFrame() {
      if (disposed) return;
      var width = canvas.width || 1;
      var height = canvas.height || 1;
      var aspect = width / Math.max(1, height);

      primeRipples();
      sim.step();
      var stateTex = sim.currentStateTexture();
      if (!stateTex) return;

      var camera = lastBundle && lastBundle.camera;
      var cam = sceneRenderCamera(camera);
      var view = scenePBRViewMatrix(camera);
      var proj = scenePBRProjectionMatrixForCamera(camera, aspect);
      var mvp = sceneMat4Multiply(proj, view);
      var cameraPos = [cam.x, cam.y, cam.z];
      var timeSec = ((typeof performance !== "undefined" && performance.now ? performance.now() : Date.now()) - causticsStart) / 1000;

      // ---- live object selection (A2-refine-2) ----
      // Read the live water entry from the bundle so object switches (e.g. the
      // control selecting "Rubber Duck") drive the render even though the
      // renderer + its analytic mesh were built once. The MESH objects
      // (duck / torus-knot) come straight from the bundle's world-space mesh
      // soup; the analytic sphere/cube keep their construction-time mesh.
      var liveEntry = (lastBundle && Array.isArray(lastBundle.waterSystems) && lastBundle.waterSystems[0]) || entry;
      // ---- live rounded-corner pool (mirrors WebGPU's drawWaterPoolEntries,
      // which reads system.entry fresh every pass) ----
      // livePoolCornerRadius (the uniform value) is clamped to
      // [0, poolMaxCornerRadius], matching the WebGPU path's
      // system.waterCornerRadius derivation, and is only non-zero when
      // livePoolShapeRounded selects the rounded variant.
      // livePoolRounded (the draw-call vertex-count decision) reads the RAW,
      // unclamped liveEntry.cornerRadius > 0.0001 -- independent of the
      // clamped uniform value above -- so pool.sel's own
      // `poolShape > 0.5 && cornerRadius > 0.0001` gate stays the single
      // source of truth for whether the 396-vertex rounded branch is what
      // the shader actually executes (see WebGPU's identical comment at
      // sceneWaterPoolSelenaRenderContext). Reading liveEntry here (not the
      // construction-time `entry`) is the actual fix: without it, a runtime
      // "Rounded Box" switch updates the control-form state but the WebGL2
      // pool keeps drawing the 30-vertex box forever.
      var livePoolShapeRounded = sceneWaterPoolShapeRounded(liveEntry);
      var livePoolCornerRadius = livePoolShapeRounded
        ? Math.max(0, Math.min(poolMaxCornerRadius, sceneWaterNum(liveEntry.cornerRadius, 0)))
        : 0;
      var livePoolRounded = livePoolShapeRounded && sceneWaterNum(liveEntry.cornerRadius, 0) > 0.0001;
      var livePoolVertexCount = livePoolRounded ? 44 * 9 : 30;
      var liveActiveObject = liveEntry.activeObject || liveEntry.objectKind || "";
      var liveSubtype = liveEntry.objectSubtype || "";
      var liveKindNum = sceneWaterRenderObjectKind(liveEntry);
      var liveMeshID = sceneWaterRenderActiveMeshID(liveActiveObject, liveSubtype, liveEntry.objectKind);
      var isMeshObject = liveMeshID === "float-duck" || liveMeshID === "float-torus";
      var liveCenter = [
        sceneWaterNum(liveEntry.objectX, objectCenter[0]),
        sceneWaterNum(liveEntry.objectY, objectCenter[1]),
        sceneWaterNum(liveEntry.objectZ, objectCenter[2]),
      ];
      var liveRadius = sceneWaterNum(liveEntry.objectRadius, objectRadius);
      // Live half-extents: prefer values from the live entry, fall back to the
      // construction-time objectHalf, then to liveRadius (mirrors boxHalf above).
      var liveHalfX = sceneWaterNum(liveEntry.objectHalfSizeX, 0);
      var liveHalfY = sceneWaterNum(liveEntry.objectHalfSizeY, 0);
      var liveHalfZ = sceneWaterNum(liveEntry.objectHalfSizeZ, 0);
      var liveHalf = [
        liveHalfX > 0 ? liveHalfX : (objectHalf[0] > 0 ? objectHalf[0] : liveRadius),
        liveHalfY > 0 ? liveHalfY : (objectHalf[1] > 0 ? objectHalf[1] : liveRadius),
        liveHalfZ > 0 ? liveHalfZ : (objectHalf[2] > 0 ? objectHalf[2] : liveRadius),
      ];
      // vec4(halfX, halfY, halfZ, radius) for caustics uniform (mirrors objectHalfRadius).
      var liveHalfRadius = [liveHalf[0], liveHalf[1], liveHalf[2], liveRadius];
      // Select the pre-built analytic mesh for the current live kind; no GL alloc.
      objectMesh = liveKindNum === 2 ? boxMesh : sphereMesh;
      // The mesh program: textured duck-material for the duck, plain
      // object-material for the torus (its <Mesh> uses water-object-material).
      var meshProgram = liveMeshID === "float-duck" && duckProgram ? duckProgram : objectProgram;
      var meshUsesModelTex = meshProgram === duckProgram;
      var meshData = isMeshObject && meshProgram ? sceneWaterRenderFindBundleMesh(lastBundle, liveMeshID) : null;
      if (meshData) refreshMeshUpload(meshProgram, meshData);

      // Object-texture projection matrices (WebGL clip convention, no WebGPU
      // depth remap — the surface only uses ndc.xy). refraction = current camera
      // view-projection; reflection = the same, pre-multiplied by a mirror across
      // the water plane y=0 (renders the mesh as seen in the surface mirror).
      objectRefractionMatrix.set(mvp);
      objectReflectionMatrix.set(sceneMat4Multiply(mvp, reflectAcrossWater));
      // The duck's albedo texture (lazy-loaded on first duck selection).
      if (liveMeshID === "float-duck" && meshUsesModelTex) {
        var wantTexURL = (meshData && meshData.texture) || "/water/models/duck/DuckCM.png";
        if (!meshModelTex || meshModelTexURL !== wantTexURL) {
          if (meshModelTex && meshModelTex !== objectTextureStub) gl.deleteTexture(meshModelTex);
          meshModelTex = sceneWaterRenderLoad2D(gl, wantTexURL);
          meshModelTexURL = wantTexURL;
        }
      }

      // ---- pre-pass A: caustics RTT ----
      // Fullscreen pass over the live sim state -> the caustic light pattern the
      // pool + surface project onto the wet floor. Mirrors the WebGPU caustics
      // setup (causticsResolution target, state at unit 0, resolution = sim grid).
      if (causticsProgram && causticsTarget) {
        gl.bindFramebuffer(gl.FRAMEBUFFER, causticsTarget.fbo);
        gl.viewport(0, 0, causticsTarget.size, causticsTarget.size);
        gl.disable(gl.DEPTH_TEST);
        gl.disable(gl.BLEND);
        gl.disable(gl.CULL_FACE);
        gl.clearColor(0, 0, 0, 1);
        gl.clear(gl.COLOR_BUFFER_BIT);
        gl.useProgram(causticsProgram);
        gl.bindVertexArray(emptyVAO);
        sceneWaterRenderBindSamplers(gl, causticsProgram, [
          { name: sceneWaterRenderStateUniform(causticsDesc), target: gl.TEXTURE_2D, tex: stateTex },
        ]);
        sceneWaterRenderSetUniforms(gl, causticsProgram, causticsDesc, {
          mvp: mvp, normalMatrix: identity3,
          poolWidth: poolWidth, poolLength: poolLength, poolHeight: poolHeight,
          normalScale: normalScale, resolution: sim.resolution, time: timeSec,
          objectKind: liveKindNum, objectCount: 0, opticsEnable: opticsEnable,
          lightDir: lightDir, objectCenter: liveCenter, objectHalfRadius: liveHalfRadius,
        });
        gl.drawArrays(gl.TRIANGLES, 0, 6);
      }

      // ---- pre-pass B: object-shadow RTT ----
      // Footprint rendered into water-UV space; the pool + surface darken by
      // this where the object occludes the refracted sun. Sphere/cube use the
      // analytic shadowProgram (object-shadow.sel); COMPOUND objects (duck /
      // torus-knot, objectKind >= 2.5 / isMeshObject) use compoundShadowProgram
      // (compound-shadow.sel) over the live proxy displacement spheres, which
      // the analytic pass cannot express — it falls back to a single
      // bounding-sphere blob (or nothing) for those. Both write the SAME
      // shadowTarget RTT; only one runs per frame.
      var compoundSphereCount = isMeshObject && compoundShadowProgram
        ? fillCompoundShadowSpheres(liveEntry.objectDisplacementSpheres)
        : 0;
      var useCompoundShadow = compoundSphereCount > 0;
      if (shadowTarget && (useCompoundShadow ? compoundShadowProgram : shadowProgram)) {
        gl.bindFramebuffer(gl.FRAMEBUFFER, shadowTarget.fbo);
        gl.viewport(0, 0, shadowTarget.size, shadowTarget.size);
        gl.disable(gl.DEPTH_TEST);
        gl.disable(gl.BLEND);
        gl.disable(gl.CULL_FACE);
        gl.clearColor(0, 0, 0, 1);
        gl.clear(gl.COLOR_BUFFER_BIT);
        gl.bindVertexArray(emptyVAO);
        if (useCompoundShadow) {
          // objectTop mirrors the raw-WGSL oracle's
          // objectCenter.y + max(halfSizeRadius.y, halfSizeRadius.w) (the
          // topmost Y of the object's bounding volume), gating the shadow off
          // once the object has been lifted clear of the pool.
          var shadowObjectTop = liveCenter[1] + Math.max(liveHalf[1], liveRadius);
          gl.useProgram(compoundShadowProgram);
          sceneWaterRenderSetUniforms(gl, compoundShadowProgram, compoundShadowDesc, {
            spheres: compoundShadowSpheres, sphereCount: compoundSphereCount,
            objectEnabled: 1, objectTop: shadowObjectTop, lightDir: lightDir,
            poolWidth: poolWidth, poolLength: poolLength,
            objectCenterX: liveCenter[0], objectCenterZ: liveCenter[2],
          });
        } else {
          gl.useProgram(shadowProgram);
          sceneWaterRenderSetUniforms(gl, shadowProgram, shadowDesc, {
            objectKind: liveKindNum, objectEnabled: liveKindNum > 0 ? 1 : 0, lightDir: lightDir,
            poolWidth: poolWidth, poolLength: poolLength,
            objectCenterX: liveCenter[0], objectCenterZ: liveCenter[2],
            objectRadius: liveRadius, objectHalfX: liveHalf[0], objectHalfZ: liveHalf[2],
          });
        }
        gl.drawArrays(gl.TRIANGLES, 0, 3);
      }

      // ---- pre-pass C: object-texture targets (mesh duck / torus) ----
      // Render the live mesh into the refraction / reflection / clipped targets
      // the surface projectively samples. Only runs when a mesh object is active
      // and its geometry resolved from the bundle; otherwise the surface's mesh
      // sampling is disabled (meshTextureEnable = 0) and the transparent stub
      // backs the samplers. Mirrors the WebGPU object-texture pass (capped 512²).
      var meshTextureReady = false;
      if (isMeshObject && meshData && meshProgram && objectRefractionTarget) {
        var meshDescActive = meshUsesModelTex ? duckDesc : objectDesc;
        var meshModel = meshUsesModelTex ? meshModelTex : null;
        renderMeshTextureTarget(meshProgram, meshDescActive, objectRefractionTarget, objectRefractionMatrix, 1, meshModel, stateTex);
        renderMeshTextureTarget(meshProgram, meshDescActive, objectReflectionTarget, objectReflectionMatrix, 1, meshModel, stateTex);
        renderMeshTextureTarget(meshProgram, meshDescActive, objectClippedTarget, objectReflectionMatrix, 2, meshModel, stateTex);
        meshTextureReady = true;
      }

      gl.bindFramebuffer(gl.FRAMEBUFFER, null);
      gl.viewport(0, 0, width, height);
      gl.enable(gl.DEPTH_TEST);
      gl.depthFunc(gl.LEQUAL);
      gl.depthMask(true);
      gl.disable(gl.BLEND);
      gl.disable(gl.CULL_FACE);
      var bg = sceneWaterRenderHexColor(entry.deepColor, [0.03, 0.08, 0.12]);
      gl.clearColor(bg[0] * 0.4, bg[1] * 0.4, bg[2] * 0.4, 1);
      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);

      // ---- 1. pool ----
      // The pool quads carry inward-facing normals (jeantimex-style tank: the
      // tiles line the INSIDE of the box). Cull the camera-facing front faces so
      // the near walls drop out and the camera looks INTO the pool — this is what
      // puts the caustic pattern on the FLOOR (not the outer walls) and lets the
      // submerged object + its floor shadow read. Restored to no-cull afterward so
      // the analytic object + double-sided surface keep their existing behavior.
      gl.enable(gl.CULL_FACE);
      gl.cullFace(gl.FRONT);
      gl.useProgram(poolProgram);
      gl.bindVertexArray(emptyVAO);
      sceneWaterRenderBindSamplers(gl, poolProgram, [
        { name: sceneWaterRenderStateUniform(poolDesc), target: gl.TEXTURE_2D, tex: stateTex },
        { name: "tileTexture", target: gl.TEXTURE_2D, tex: tileTex },
        { name: "causticTexture", target: gl.TEXTURE_2D, tex: causticTex },
        { name: "shadowTexture", target: gl.TEXTURE_2D, tex: shadowTex },
      ]);
      sceneWaterRenderSetUniforms(gl, poolProgram, poolDesc, {
        mvp: mvp, normalMatrix: identity3,
        poolWidth: poolWidth, poolLength: poolLength, poolHeight: poolHeight,
        lightDir: lightDir,
        // Rounded-corner pool: pool.sel's own params, picked up by name via
        // the descriptor-driven applicator above (see livePoolShapeRounded /
        // livePoolCornerRadius / livePoolRounded derivation near liveEntry).
        cornerRadius: livePoolCornerRadius, poolShape: livePoolShapeRounded ? 1 : 0,
      });
      gl.drawArrays(gl.TRIANGLES, 0, livePoolVertexCount);
      gl.disable(gl.CULL_FACE);

      // ---- 2. object ----
      // MESH objects (duck / torus): draw the live world-space mesh directly with
      // its material GLES (textured duck-material for the duck, object-material
      // for the torus), sampling the live stateTex for the submerged tint/caustic
      // + the duck's albedo. ANALYTIC objects (sphere / cube): the construction
      // -time UV-sphere / box, unchanged. A double-sided depth-tested draw so the
      // duck reads as a lit solid floating in the pool.
      if (isMeshObject && meshData && meshProgram && meshUpload && meshUpload.count) {
        gl.enable(gl.DEPTH_TEST);
        gl.depthFunc(gl.LEQUAL);
        gl.depthMask(true);
        gl.disable(gl.CULL_FACE);
        gl.useProgram(meshProgram);
        // Re-point attributes for this program (object-texture passes may have
        // used a different program's locations on the shared VAO).
        refreshMeshUpload(meshProgram, meshData);
        gl.bindVertexArray(meshUpload.vao);
        var directDesc = meshUsesModelTex ? duckDesc : objectDesc;
        var directSamplers = [{ name: sceneWaterRenderStateUniform(directDesc), target: gl.TEXTURE_2D, tex: stateTex }];
        if (meshUsesModelTex && meshModelTex) directSamplers.push({ name: "modelTexture", target: gl.TEXTURE_2D, tex: meshModelTex });
        sceneWaterRenderBindSamplers(gl, meshProgram, directSamplers);
        sceneWaterRenderSetUniforms(gl, meshProgram, directDesc, {
          mvp: mvp, normalMatrix: identity3, lightDir: lightDir, poolHeight: poolHeight,
          baseColor: meshUsesModelTex ? [1, 1, 1, 1] : [0.52, 0.54, 0.56, 1],
          isTexturePass: 0, texturePassMode: 0,
        });
        gl.drawArrays(gl.TRIANGLES, 0, meshUpload.count);
      } else if (objectProgram && objectMesh && liveKindNum > 0 && liveKindNum < 3) {
        gl.useProgram(objectProgram);
        gl.bindVertexArray(objectMesh.vao);
        sceneWaterRenderBindSamplers(gl, objectProgram, [
          { name: sceneWaterRenderStateUniform(objectDesc), target: gl.TEXTURE_2D, tex: stateTex },
        ]);
        sceneWaterRenderSetUniforms(gl, objectProgram, objectDesc, {
          mvp: mvp, normalMatrix: identity3, lightDir: lightDir, poolHeight: poolHeight,
          baseColor: [0.52, 0.54, 0.56, 1], isTexturePass: 0, texturePassMode: 0,
        });
        gl.drawElements(gl.TRIANGLES, objectMesh.count, gl.UNSIGNED_SHORT, 0);
      }

      // ---- 3. surface (above) ----
      gl.useProgram(surfaceProgram);
      gl.bindVertexArray(emptyVAO);
      gl.enable(gl.BLEND);
      gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
      gl.depthMask(false);
      sceneWaterRenderBindSamplers(gl, surfaceProgram, [
        { name: sceneWaterRenderStateUniform(surfaceDesc), target: gl.TEXTURE_2D, tex: stateTex },
        { name: "tileTexture", target: gl.TEXTURE_2D, tex: tileTex },
        { name: "causticTexture", target: gl.TEXTURE_2D, tex: causticTex },
        { name: "sky", target: gl.TEXTURE_CUBE_MAP, tex: skyTex },
        // A2-refine-2: the projected mesh object-texture targets (refraction /
        // reflection / clipped-reflection). When no mesh is active these bind the
        // transparent stub and the surface's mesh sampling resolves to nothing.
        { name: "objectRefractionTex", target: gl.TEXTURE_2D, tex: meshTextureReady ? objectRefractionTex : objectTextureStub },
        { name: "objectReflectionTex", target: gl.TEXTURE_2D, tex: meshTextureReady ? objectReflectionTex : objectTextureStub },
        { name: "objectClippedReflectionTex", target: gl.TEXTURE_2D, tex: meshTextureReady ? objectClippedTex : objectTextureStub },
      ]);
      // All objects (analytic and mesh) use live center/radius/half so that
      // selection changes (sphere ↔ cube) propagate to the surface shader.
      var surfCenter = liveCenter;
      var surfRadius = liveRadius;
      sceneWaterRenderSetUniforms(gl, surfaceProgram, surfaceDesc, {
        mvp: mvp, normalMatrix: identity3,
        poolWidth: poolWidth, poolLength: poolLength, poolHeight: poolHeight,
        normalScale: normalScale, gridResolution: gridResolution,
        objectKind: isMeshObject ? 3 : liveKindNum, objectSubtype: liveMeshID === "float-torus" ? 1 : 0,
        objectCount: 0, objectRadius: surfRadius,
        opticsEnable: opticsEnable, opticsCaustic: opticsCaustic,
        lightDir: lightDir, cameraPos: cameraPos, waterColor: waterColor,
        objectCenter: surfCenter, objectHalf: liveHalf,
        // Projected mesh object-texture sampling.
        meshTextureEnable: meshTextureReady ? 1 : 0,
        refractionMatrix: objectRefractionMatrix,
        reflectionMatrix: objectReflectionMatrix,
      });
      gl.drawArrays(gl.TRIANGLES, 0, surfaceVertexCount);

      gl.depthMask(true);
      gl.disable(gl.BLEND);
      gl.bindVertexArray(null);
    }

    // Self-driven animation loop: the mount's scheduler does not classify water
    // systems as "wants animation", so the forced water renderer keeps its own
    // rAF loop alive to animate the ripple. The mount still calls render(bundle)
    // each time it has a fresh bundle/camera; we just capture it here.
    function loop() {
      if (disposed) { rafId = 0; return; }
      drawFrame();
      if (typeof requestAnimationFrame === "function") {
        rafId = requestAnimationFrame(loop);
      }
    }

    function render(bundle /*, viewport */) {
      if (disposed) return;
      if (bundle) lastBundle = bundle;
      drawFrame();
      if (!rafId && typeof requestAnimationFrame === "function") {
        rafId = requestAnimationFrame(loop);
      }
    }

    function dispose() {
      if (disposed) return;
      disposed = true;
      if (rafId && typeof cancelAnimationFrame === "function") cancelAnimationFrame(rafId);
      rafId = 0;
      try { sim.dispose(); } catch (e) {}
      if (poolProgram) gl.deleteProgram(poolProgram);
      if (surfaceProgram) gl.deleteProgram(surfaceProgram);
      if (objectProgram) gl.deleteProgram(objectProgram);
      if (duckProgram) gl.deleteProgram(duckProgram);
      if (causticsProgram) gl.deleteProgram(causticsProgram);
      if (shadowProgram) gl.deleteProgram(shadowProgram);
      if (compoundShadowProgram) gl.deleteProgram(compoundShadowProgram);
      if (emptyVAO) gl.deleteVertexArray(emptyVAO);
      [sphereMesh, boxMesh].forEach(function(m) {
        if (m) {
          gl.deleteVertexArray(m.vao);
          for (var i = 0; i < m.buffers.length; i++) gl.deleteBuffer(m.buffers[i]);
        }
      });
      if (meshUpload) {
        gl.deleteVertexArray(meshUpload.vao);
        gl.deleteBuffer(meshUpload.posBuf);
        gl.deleteBuffer(meshUpload.normBuf);
        gl.deleteBuffer(meshUpload.uvBuf);
        meshUpload = null;
      }
      [objectRefractionTarget, objectReflectionTarget, objectClippedTarget].forEach(function(t) {
        if (t) { gl.deleteTexture(t.tex); gl.deleteRenderbuffer(t.depth); gl.deleteFramebuffer(t.fbo); }
      });
      if (causticsTarget) { gl.deleteTexture(causticsTarget.tex); gl.deleteFramebuffer(causticsTarget.fbo); }
      if (shadowTarget) { gl.deleteTexture(shadowTarget.tex); gl.deleteFramebuffer(shadowTarget.fbo); }
      [tileTex, skyTex, causticStub, shadowStub, objectTextureStub].forEach(function(t) { if (t) gl.deleteTexture(t); });
      if (meshModelTex && meshModelTex !== objectTextureStub) gl.deleteTexture(meshModelTex);
    }

    return {
      kind: "webgl",
      isWaterForced: true,
      render: render,
      dispose: dispose,
      resize: function() {},
      supportsBundle: function() { return true; },
    };
  }

  // Post-processing manager — orchestrates an effect chain between the
  // scene render and the final screen blit.
  function createScenePostProcessor(gl) {
    var quad = createSceneFullscreenQuad(gl);
    var sceneFBO = null;
    var auxFBO = null;
    var scratchFBO = null;
    var pingPong = null;
    var currentWidth = 0;
    var currentHeight = 0;

    // Lazily compiled shader programs, keyed by effect name.
    var programs = {};

    // Custom post program cache: name → program | null (null = failed, skip).
    var customPostPrograms = {};
    // Failed custom post names (to warn once only).
    var customPostFailed = {};

    // Get or compile a post-processing program.
    function getProgram(name, fragmentSource) {
      if (programs[name]) return programs[name];
      var prog = createScenePostProgram(gl, fragmentSource);
      if (prog) programs[name] = prog;
      return prog;
    }

	    // Bind a target FBO, set viewport, activate a program, and bind the
	    // input texture to unit 0 as u_texture. Callers set effect-specific
	    // uniforms afterwards, then call drawSceneFullscreenQuad.
	    function clearPostTextureBindings() {
	      for (var unit = 0; unit < 4; unit++) {
	        gl.activeTexture(gl.TEXTURE0 + unit);
	        gl.bindTexture(gl.TEXTURE_2D, null);
	      }
	    }

	    function beginPostPass(prog, inputTex, targetFBO, w, h) {
	      if (targetFBO) {
	        clearPostTextureBindings();
	      }
	      gl.bindFramebuffer(gl.FRAMEBUFFER, targetFBO);
	      gl.viewport(0, 0, w, h);
	      gl.useProgram(prog.program);
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, inputTex);
      gl.uniform1i(gl.getUniformLocation(prog.program, "u_texture"), 0);
    }

    // Run a complete fullscreen pass and return the resulting color texture
    // (or null when rendering to screen).
    function postPass(prog, inputTex, targetFBO, w, h) {
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    // applyCustomPost: runs a Selena-emitted GLSL post pass.
    // Selena post contract (WebGL2 variant):
    //   vertex: attribute vec2 a_position (Selena-emitted vert); v_uv = a_position*0.5+0.5
    //   fragment: uniform sampler2D _sceneColor, sampler2D _sceneDepth + user params by name
    // On compile/link failure: skip once-warned, identity passthrough.
    function applyCustomPost(inputTex, depthTex, effect, targetFBO, w, h) {
      var name = (typeof effect.name === "string" && effect.name) ? effect.name : "custom";
      if (customPostFailed[name]) return inputTex; // already failed → skip

      var vertSrc = (typeof effect.vertexGLSL === "string") ? effect.vertexGLSL.trim() : "";
      var fragSrc = (typeof effect.fragmentGLSL === "string") ? effect.fragmentGLSL.trim() : "";
      if (!vertSrc || !fragSrc) return inputTex; // no GLSL → identity

      if (!customPostPrograms.hasOwnProperty(name)) {
        var prog = createSceneCustomPostProgram(gl, vertSrc, fragSrc);
        if (!prog) {
          console.warn("[gosx] custom post pass '" + name + "' (WebGL2) compile/link failed; falling back to identity.");
          customPostFailed[name] = true;
          customPostPrograms[name] = null;
          return inputTex;
        }
        customPostPrograms[name] = prog;
      }

      var p = customPostPrograms[name];
      if (!p) return inputTex;

      gl.bindFramebuffer(gl.FRAMEBUFFER, targetFBO ? targetFBO.fbo : null);
      gl.viewport(0, 0, w, h);
      gl.useProgram(p.program);

      // Bind sceneColor to unit 0.
      gl.activeTexture(gl.TEXTURE0);
      gl.bindTexture(gl.TEXTURE_2D, inputTex);
      gl.uniform1i(gl.getUniformLocation(p.program, "_sceneColor"), 0);

      // Bind sceneDepth to unit 1 (if available).
      if (depthTex) {
        gl.activeTexture(gl.TEXTURE1);
        gl.bindTexture(gl.TEXTURE_2D, depthTex);
        gl.uniform1i(gl.getUniformLocation(p.program, "_sceneDepth"), 1);
      }

      // Upload user uniforms by name from shaderLayout.
      var uniforms = effect.uniforms || {};
      var layout = effect.shaderLayout;
      var fields = (layout && layout.uniformBlock && Array.isArray(layout.uniformBlock.fields))
        ? layout.uniformBlock.fields : [];
      for (var fi = 0; fi < fields.length; fi++) {
        var field = fields[fi];
        var val = Object.prototype.hasOwnProperty.call(uniforms, field.name) ? uniforms[field.name] : null;
        if (val === null || val === undefined) continue;
        var loc = gl.getUniformLocation(p.program, field.name);
        if (!loc) continue;
        if (typeof val === "number") {
          gl.uniform1f(loc, val);
        } else if (Array.isArray(val) || (val && val.length)) {
          switch (val.length) {
            case 2: gl.uniform2fv(loc, val); break;
            case 3: gl.uniform3fv(loc, val); break;
            case 4: gl.uniform4fv(loc, val); break;
            default: gl.uniform1fv(loc, val); break;
          }
        }
      }

      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    function scenePostToneMapMode(mode) {
      if (typeof mode === "string") {
        var normalized = mode.trim().toLowerCase();
        if (normalized === "linear" || normalized === "none") return 0;
        if (normalized === "reinhard") return 2;
        if (normalized === "filmic") return 3;
      }
      return 1;
    }

    // Apply the authored tone mapping curve.
    function applyToneMapping(inputTex, effect, targetFBO, w, h) {
      var prog = getProgram("toneMapping", SCENE_POST_TONEMAPPING_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_exposure"), sceneNumber(effect.exposure, 1.0));
      gl.uniform1i(gl.getUniformLocation(prog.program, "u_toneMapMode"), scenePostToneMapMode(effect.mode));
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    // Apply bloom: bright pass, horizontal blur, vertical blur, composite.
    // The bloom ping-pong buffers are allocated based on scaled scene dims
    // (not the pass dims, which flip to canvas dims on the last pass when
    // the composite writes directly to the screen).
    function applyBloom(inputTex, effect, targetFBO, passW, passH, scaledW, scaledH) {
      var brightProg = getProgram("bloomBright", SCENE_POST_BLOOM_BRIGHT_SOURCE);
      var blurProg = getProgram("bloomBlur", SCENE_POST_BLUR_SOURCE);
      var compositeProg = getProgram("bloomComposite", SCENE_POST_BLOOM_COMPOSITE_SOURCE);
      if (!brightProg || !blurProg || !compositeProg) return inputTex;

      // bloomScale is the bloom-internal downscale applied on top of the
      // main PostFX scaling. Defaults to 0.5 (v0.14.0 behavior). Out-of-range
      // values silently fall back to 0.5.
      var bloomScale = (effect.scale > 0 && effect.scale <= 1) ? effect.scale : 0.5;
      var halfW = Math.max(1, Math.floor(scaledW * bloomScale));
      var halfH = Math.max(1, Math.floor(scaledH * bloomScale));

      // Ensure ping-pong FBOs match the bloom target resolution.
      if (!pingPong || pingPong.a.width !== halfW || pingPong.a.height !== halfH) {
        if (pingPong) {
          disposeScenePostFBO(gl, pingPong.a);
          disposeScenePostFBO(gl, pingPong.b);
        }
        pingPong = createScenePostPingPong(gl, halfW, halfH);
      }

      var threshold = sceneNumber(effect.threshold, 0.8);
      var radius = sceneNumber(effect.radius, 5.0);
      var intensity = sceneNumber(effect.intensity, 0.5);

      // 1. Bright pass: scene texture -> pingPong.a (bloom-res).
      beginPostPass(brightProg, inputTex, pingPong.a.fbo, halfW, halfH);
      gl.uniform1f(gl.getUniformLocation(brightProg.program, "u_threshold"), threshold);
      drawSceneFullscreenQuad(gl, quad.vao);

      // 2. Horizontal blur: pingPong.a -> pingPong.b.
      beginPostPass(blurProg, pingPong.a.colorTex, pingPong.b.fbo, halfW, halfH);
      gl.uniform2f(gl.getUniformLocation(blurProg.program, "u_direction"), 1.0, 0.0);
      gl.uniform1f(gl.getUniformLocation(blurProg.program, "u_radius"), radius);
      drawSceneFullscreenQuad(gl, quad.vao);

      // 3. Vertical blur: pingPong.b -> pingPong.a.
      beginPostPass(blurProg, pingPong.b.colorTex, pingPong.a.fbo, halfW, halfH);
      gl.uniform2f(gl.getUniformLocation(blurProg.program, "u_direction"), 0.0, 1.0);
      gl.uniform1f(gl.getUniformLocation(blurProg.program, "u_radius"), radius);
      drawSceneFullscreenQuad(gl, quad.vao);

      // 4. Composite: scene + bloom -> targetFBO (or screen on last pass).
      // Uses passW/passH which are scaled for intermediate, canvas for final.
      beginPostPass(compositeProg, inputTex, targetFBO ? targetFBO.fbo : null, passW, passH);
      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D, pingPong.a.colorTex);
      gl.uniform1i(gl.getUniformLocation(compositeProg.program, "u_bloomTexture"), 1);
      gl.uniform1f(gl.getUniformLocation(compositeProg.program, "u_intensity"), intensity);
      drawSceneFullscreenQuad(gl, quad.vao);

      return targetFBO ? targetFBO.colorTex : null;
    }

    // Apply vignette darkening.
    function applyVignette(inputTex, effect, targetFBO, w, h) {
      var prog = getProgram("vignette", SCENE_POST_VIGNETTE_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_intensity"), sceneNumber(effect.intensity, 1.0));
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    // Apply color grading.
    function applyColorGrade(inputTex, effect, targetFBO, w, h) {
      var prog = getProgram("colorGrade", SCENE_POST_COLORGRADE_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_exposure"), sceneNumber(effect.exposure, 1.0));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_contrast"), sceneNumber(effect.contrast, 1.0));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_saturation"), sceneNumber(effect.saturation, 1.0));
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    function applySSAO(inputTex, effect, targetFBO, w, h) {
      var prog = getProgram("ssao", SCENE_POST_SSAO_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_radius"), sceneNumber(effect.radius, 4.0));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_intensity"), sceneNumber(effect.intensity, 0.55));
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    function applyDOF(inputTex, effect, targetFBO, w, h, camera) {
      if (!sceneFBO || !sceneFBO.depthTex) return inputTex;
      var prog = getProgram("dof", SCENE_POST_DOF_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.activeTexture(gl.TEXTURE1);
      gl.bindTexture(gl.TEXTURE_2D, sceneFBO.depthTex);
      gl.uniform1i(gl.getUniformLocation(prog.program, "u_depthTexture"), 1);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_focusDistance"), sceneNumber(effect.focusDistance, 8.0));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_aperture"), sceneNumber(effect.aperture, 0.04));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_maxBlur"), sceneNumber(effect.maxBlur, 8.0));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_near"), Math.max(0.0001, sceneNumber(camera && camera.near, 0.05)));
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_far"), Math.max(0.1, sceneNumber(camera && camera.far, 128)));
      drawSceneFullscreenQuad(gl, quad.vao);
      return targetFBO ? targetFBO.colorTex : null;
    }

    // Simple blit — copy a texture to the screen without any processing.
    var blitProg = null;
    var SCENE_POST_BLIT_SOURCE = [
      "#version 300 es",
      "precision highp float;",
      "in vec2 v_uv;",
      "uniform sampler2D u_texture;",
      "out vec4 fragColor;",
      "void main() {",
      "    fragColor = texture(u_texture, v_uv);",
      "}",
    ].join("\n");

    function blitToScreen(inputTex, w, h) {
      if (!blitProg) {
        blitProg = createScenePostProgram(gl, SCENE_POST_BLIT_SOURCE);
      }
      if (!blitProg) return;
      postPass(blitProg, inputTex, null, w, h);
    }

    return {
      // Prepare the offscreen FBO for the main scene render. Takes the canvas
      // backing-store dimensions and the postfx maxPixels cap from the bundle.
      // Returns { width, height, factor } — the scaled render target dims plus
      // the scale factor applied. Callers must use these dims for gl.viewport,
      // uniforms like u_viewportHeight, and the apply() call.
	      begin: function(canvasW, canvasH, maxPixels) {
        var factor = resolvePostFXFactor(maxPixels, canvasW * canvasH);
        var sw = Math.max(1, Math.floor(canvasW * factor));
        var sh = Math.max(1, Math.floor(canvasH * factor));

        // Invalidation key is scaled dims so both canvas resize and maxPixels
        // change trigger reallocation.
        if (sw !== currentWidth || sh !== currentHeight) {
          if (sceneFBO) disposeScenePostFBO(gl, sceneFBO);
          sceneFBO = createScenePostFBO(gl, sw, sh, true);
          if (auxFBO) disposeScenePostFBO(gl, auxFBO);
          auxFBO = null;
          if (scratchFBO) disposeScenePostFBO(gl, scratchFBO);
          scratchFBO = null;
          currentWidth = sw;
          currentHeight = sh;
        }
	        clearPostTextureBindings();
	        gl.bindFramebuffer(gl.FRAMEBUFFER, sceneFBO.fbo);
	        return { width: sw, height: sh, factor: factor };
	      },

      // Process the effect chain and output to the screen. Takes the scaled
      // dims (for intermediate FBO writes) and the canvas dims (for the final
      // blit to the default framebuffer).
      apply: function(effects, scaledW, scaledH, canvasW, canvasH, camera) {
        gl.bindFramebuffer(gl.FRAMEBUFFER, null);
        gl.disable(gl.DEPTH_TEST);

        var currentTexture = sceneFBO.colorTex;

        // Multi-effect chains need an auxiliary full-res FBO for intermediate
        // results. Allocated at SCALED dims, not canvas dims.
        if (effects.length > 1 && !auxFBO) {
          auxFBO = createScenePostFBO(gl, scaledW, scaledH);
        }

        for (var i = 0; i < effects.length; i++) {
          var effect = effects[i];
          var isLast = (i === effects.length - 1);

          var targetFBO = null;
          if (!isLast) {
            targetFBO = (currentTexture === sceneFBO.colorTex) ? auxFBO : sceneFBO;
            if (effect.kind === SCENE_POST_DOF && targetFBO === sceneFBO) {
              if (!scratchFBO || scratchFBO.width !== scaledW || scratchFBO.height !== scaledH) {
                if (scratchFBO) disposeScenePostFBO(gl, scratchFBO);
                scratchFBO = createScenePostFBO(gl, scaledW, scaledH);
              }
              targetFBO = scratchFBO;
            }
          }

          // Intermediate passes run at scaled dims; the final pass targets
          // the default framebuffer at canvas dims.
          var passW = isLast ? canvasW : scaledW;
          var passH = isLast ? canvasH : scaledH;

          switch (effect.kind) {
            case SCENE_POST_TONE_MAPPING:
              currentTexture = applyToneMapping(currentTexture, effect, targetFBO, passW, passH);
              break;
            case SCENE_POST_BLOOM:
              currentTexture = applyBloom(currentTexture, effect, targetFBO, passW, passH, scaledW, scaledH);
              break;
            case SCENE_POST_VIGNETTE:
              currentTexture = applyVignette(currentTexture, effect, targetFBO, passW, passH);
              break;
            case SCENE_POST_COLOR_GRADE:
              currentTexture = applyColorGrade(currentTexture, effect, targetFBO, passW, passH);
              break;
            case SCENE_POST_SSAO:
              currentTexture = applySSAO(currentTexture, effect, targetFBO, passW, passH);
              break;
            case SCENE_POST_DOF:
              currentTexture = applyDOF(currentTexture, effect, targetFBO, passW, passH, camera);
              break;
            case SCENE_POST_CUSTOM_POST: {
              // Selena post contract (WebGL2): use provided GLSL pair.
              // On failure the pass is skipped (identity passthrough).
              var depthTex = sceneFBO && sceneFBO.depthTex ? sceneFBO.depthTex : null;
              var next = applyCustomPost(currentTexture, depthTex, effect, targetFBO, passW, passH);
              if (next !== null) currentTexture = next;
              break;
            }
            default:
              // Unknown effect — skip.
              break;
          }

          if (isLast && currentTexture === null) break;
        }

        if (currentTexture !== null && effects.length > 0) {
          blitToScreen(currentTexture, canvasW, canvasH);
        } else if (effects.length === 0) {
          blitToScreen(sceneFBO.colorTex, canvasW, canvasH);
        }

        gl.enable(gl.DEPTH_TEST);
      },

      // Release all post-processing GPU resources.
      dispose: function() {
        if (sceneFBO) {
          disposeScenePostFBO(gl, sceneFBO);
          sceneFBO = null;
        }
        if (auxFBO) {
          disposeScenePostFBO(gl, auxFBO);
          auxFBO = null;
        }
        if (scratchFBO) {
          disposeScenePostFBO(gl, scratchFBO);
          scratchFBO = null;
        }
        if (pingPong) {
          disposeScenePostFBO(gl, pingPong.a);
          disposeScenePostFBO(gl, pingPong.b);
          pingPong = null;
        }
        for (var key in programs) {
          if (programs[key]) {
            gl.deleteShader(programs[key].vertexShader);
            gl.deleteShader(programs[key].fragmentShader);
            gl.deleteProgram(programs[key].program);
          }
        }
        programs = {};
        // Custom post programs.
        for (var cpKey in customPostPrograms) {
          var cpProg = customPostPrograms[cpKey];
          if (cpProg) {
            gl.deleteShader(cpProg.vertexShader);
            gl.deleteShader(cpProg.fragmentShader);
            gl.deleteProgram(cpProg.program);
          }
        }
        customPostPrograms = {};
        customPostFailed = {};
        if (blitProg) {
          gl.deleteShader(blitProg.vertexShader);
          gl.deleteShader(blitProg.fragmentShader);
          gl.deleteProgram(blitProg.program);
          blitProg = null;
        }
        if (quad) {
          gl.deleteBuffer(quad.vbo);
          gl.deleteVertexArray(quad.vao);
        }
        currentWidth = 0;
        currentHeight = 0;
      },
    };
  }

  // --- Texture Management ---

  // Load and cache a WebGL2 texture from a URL. Returns a record
  // { texture, loaded, failed } that updates asynchronously.
  function scenePBRTextureLooksHDR(url) {
    var key = typeof url === "string" ? url.trim().toLowerCase() : "";
    return key.endsWith(".hdr") || key.indexOf(".hdr?") >= 0 || key.indexOf(".hdr#") >= 0;
  }

  function scenePBRTonemapHDRPixels(parsed) {
    var width = Math.max(1, Math.floor(sceneNumber(parsed && parsed.width, 1)));
    var height = Math.max(1, Math.floor(sceneNumber(parsed && parsed.height, 1)));
    var source = parsed && parsed.data;
    var pixels = new Uint8Array(width * height * 4);
    for (var i = 0, j = 0; i < width * height; i++, j += 3) {
      var r = Math.max(0, sceneNumber(source && source[j], 0));
      var g = Math.max(0, sceneNumber(source && source[j + 1], 0));
      var b = Math.max(0, sceneNumber(source && source[j + 2], 0));
      pixels[i * 4] = Math.max(0, Math.min(255, Math.round(Math.pow(r / (1 + r), 1 / 2.2) * 255)));
      pixels[i * 4 + 1] = Math.max(0, Math.min(255, Math.round(Math.pow(g / (1 + g), 1 / 2.2) * 255)));
      pixels[i * 4 + 2] = Math.max(0, Math.min(255, Math.round(Math.pow(b / (1 + b), 1 / 2.2) * 255)));
      pixels[i * 4 + 3] = 255;
    }
    return { width: width, height: height, pixels: pixels };
  }

  function scenePBRLoadHDRTexture(gl, key, texture, record) {
    if (typeof fetch !== "function" || typeof sceneParseRadianceHDR !== "function") {
      return false;
    }
    fetch(key)
      .then(function(response) {
        if (!response || response.ok === false) {
          throw new Error("failed to fetch HDR environment map");
        }
        return response.arrayBuffer();
      })
      .then(function(buffer) {
        var parsed = sceneParseRadianceHDR(buffer);
        var ldr = scenePBRTonemapHDRPixels(parsed);
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, ldr.width, ldr.height, 0, gl.RGBA, gl.UNSIGNED_BYTE, ldr.pixels);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
        record.loaded = true;
        record.width = ldr.width;
        record.height = ldr.height;
        record.hdr = true;
      })
      .catch(function() {
        record.failed = true;
      });
    return true;
  }

  function scenePBRLoadTexture(gl, url, cache) {
    if (!cache) return null;
    const textureMap = cache;
    const key = typeof url === "string" ? url.trim() : "";
    if (!key) {
      return null;
    }
    if (textureMap.has(key)) {
      return textureMap.get(key);
    }

    const texture = gl.createTexture();
    const record = { texture: texture, src: key, loaded: false, failed: false };
    textureMap.set(key, record);

    // Initialize with a 1x1 white pixel placeholder.
    gl.bindTexture(gl.TEXTURE_2D, texture);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([255, 255, 255, 255]));
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

    if (scenePBRTextureLooksHDR(key) && scenePBRLoadHDRTexture(gl, key, texture, record)) {
      return record;
    }

    if (typeof Image === "function") {
      const image = new Image();
      image.onload = function() {
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, image);
        if (typeof gl.generateMipmap === "function" && gl.LINEAR_MIPMAP_LINEAR !== undefined) {
          gl.generateMipmap(gl.TEXTURE_2D);
          gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
        } else {
          gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
        }
        record.loaded = true;
      };
      image.onerror = function() {
        record.failed = true;
      };
      image.src = key;
    }

    return record;
  }

  // Bind a texture record to a specific sampler unit.
  function scenePBRBindTexture(gl, unit, texture) {
    gl.activeTexture(gl.TEXTURE0 + unit);
    gl.bindTexture(gl.TEXTURE_2D, texture);
  }

  // --- Shader Program ---

  // Cache the base uniform locations shared between the static and skinned
  // PBR programs. Returns a uniforms object with per-light arrays populated.
  function scenePBRCacheBaseUniforms(gl, program) {
    var uniforms = {
      viewMatrix: gl.getUniformLocation(program, "u_viewMatrix"),
      projectionMatrix: gl.getUniformLocation(program, "u_projectionMatrix"),
      cameraPosition: gl.getUniformLocation(program, "u_cameraPosition"),

      albedo: gl.getUniformLocation(program, "u_albedo"),
      roughness: gl.getUniformLocation(program, "u_roughness"),
      metalness: gl.getUniformLocation(program, "u_metalness"),
      clearcoat: gl.getUniformLocation(program, "u_clearcoat"),
      sheen: gl.getUniformLocation(program, "u_sheen"),
      transmission: gl.getUniformLocation(program, "u_transmission"),
      iridescence: gl.getUniformLocation(program, "u_iridescence"),
      anisotropy: gl.getUniformLocation(program, "u_anisotropy"),
      emissive: gl.getUniformLocation(program, "u_emissive"),
      opacity: gl.getUniformLocation(program, "u_opacity"),
      unlit: gl.getUniformLocation(program, "u_unlit"),

      albedoMap: gl.getUniformLocation(program, "u_albedoMap"),
      normalMap: gl.getUniformLocation(program, "u_normalMap"),
      roughnessMap: gl.getUniformLocation(program, "u_roughnessMap"),
      metalnessMap: gl.getUniformLocation(program, "u_metalnessMap"),
      emissiveMap: gl.getUniformLocation(program, "u_emissiveMap"),
      hasAlbedoMap: gl.getUniformLocation(program, "u_hasAlbedoMap"),
      hasNormalMap: gl.getUniformLocation(program, "u_hasNormalMap"),
      hasRoughnessMap: gl.getUniformLocation(program, "u_hasRoughnessMap"),
      hasMetalnessMap: gl.getUniformLocation(program, "u_hasMetalnessMap"),
      hasEmissiveMap: gl.getUniformLocation(program, "u_hasEmissiveMap"),

      lightCount: gl.getUniformLocation(program, "u_lightCount"),
      lightTypes: [],
      lightPositions: [],
      lightDirections: [],
      lightColors: [],
      lightIntensities: [],
      lightRanges: [],
      lightDecays: [],
      lightAngles: [],
      lightPenumbras: [],
      lightGroundColors: [],

      ambientColor: gl.getUniformLocation(program, "u_ambientColor"),
      ambientIntensity: gl.getUniformLocation(program, "u_ambientIntensity"),
      skyColor: gl.getUniformLocation(program, "u_skyColor"),
      skyIntensity: gl.getUniformLocation(program, "u_skyIntensity"),
      groundColor: gl.getUniformLocation(program, "u_groundColor"),
      groundIntensity: gl.getUniformLocation(program, "u_groundIntensity"),
      envMap: gl.getUniformLocation(program, "u_envMap"),
      hasEnvMap: gl.getUniformLocation(program, "u_hasEnvMap"),
      envIntensity: gl.getUniformLocation(program, "u_envIntensity"),
      envRotation: gl.getUniformLocation(program, "u_envRotation"),

      shadowMap0_0: gl.getUniformLocation(program, "u_shadowMap0_0"),
      shadowMap0_1: gl.getUniformLocation(program, "u_shadowMap0_1"),
      shadowMap0_2: gl.getUniformLocation(program, "u_shadowMap0_2"),
      shadowMap0_3: gl.getUniformLocation(program, "u_shadowMap0_3"),
      lightSpaceMatrices0: gl.getUniformLocation(program, "u_lightSpaceMatrices0"),
      shadowCascadeSplits0: gl.getUniformLocation(program, "u_shadowCascadeSplits0"),
      shadowCascades0: gl.getUniformLocation(program, "u_shadowCascades0"),
      hasShadow0: gl.getUniformLocation(program, "u_hasShadow0"),
      shadowBias0: gl.getUniformLocation(program, "u_shadowBias0"),
      shadowSoftness0: gl.getUniformLocation(program, "u_shadowSoftness0"),
      shadowLightIndex0: gl.getUniformLocation(program, "u_shadowLightIndex0"),

      shadowMap1_0: gl.getUniformLocation(program, "u_shadowMap1_0"),
      shadowMap1_1: gl.getUniformLocation(program, "u_shadowMap1_1"),
      shadowMap1_2: gl.getUniformLocation(program, "u_shadowMap1_2"),
      shadowMap1_3: gl.getUniformLocation(program, "u_shadowMap1_3"),
      lightSpaceMatrices1: gl.getUniformLocation(program, "u_lightSpaceMatrices1"),
      shadowCascadeSplits1: gl.getUniformLocation(program, "u_shadowCascadeSplits1"),
      shadowCascades1: gl.getUniformLocation(program, "u_shadowCascades1"),
      hasShadow1: gl.getUniformLocation(program, "u_hasShadow1"),
      shadowBias1: gl.getUniformLocation(program, "u_shadowBias1"),
      shadowSoftness1: gl.getUniformLocation(program, "u_shadowSoftness1"),
      shadowLightIndex1: gl.getUniformLocation(program, "u_shadowLightIndex1"),

      receiveShadow: gl.getUniformLocation(program, "u_receiveShadow"),

      exposure: gl.getUniformLocation(program, "u_exposure"),
      toneMapMode: gl.getUniformLocation(program, "u_toneMapMode"),

      hasFog: gl.getUniformLocation(program, "u_hasFog"),
      fogDensity: gl.getUniformLocation(program, "u_fogDensity"),
      fogColor: gl.getUniformLocation(program, "u_fogColor"),
    };

    for (var i = 0; i < 8; i++) {
      uniforms.lightTypes.push(gl.getUniformLocation(program, "u_lightTypes[" + i + "]"));
      uniforms.lightPositions.push(gl.getUniformLocation(program, "u_lightPositions[" + i + "]"));
      uniforms.lightDirections.push(gl.getUniformLocation(program, "u_lightDirections[" + i + "]"));
      uniforms.lightColors.push(gl.getUniformLocation(program, "u_lightColors[" + i + "]"));
      uniforms.lightIntensities.push(gl.getUniformLocation(program, "u_lightIntensities[" + i + "]"));
      uniforms.lightRanges.push(gl.getUniformLocation(program, "u_lightRanges[" + i + "]"));
      uniforms.lightDecays.push(gl.getUniformLocation(program, "u_lightDecays[" + i + "]"));
      uniforms.lightAngles.push(gl.getUniformLocation(program, "u_lightAngles[" + i + "]"));
      uniforms.lightPenumbras.push(gl.getUniformLocation(program, "u_lightPenumbras[" + i + "]"));
      uniforms.lightGroundColors.push(gl.getUniformLocation(program, "u_lightGroundColors[" + i + "]"));
    }

    return uniforms;
  }

  function scenePBRCustomUniformName(name) {
    const value = typeof name === "string" ? name.trim() : "";
    return /^[A-Za-z_][A-Za-z0-9_]*$/.test(value) ? value : "";
  }

  function scenePBRCustomUniformDeclaration(name, value) {
    const uniformName = scenePBRCustomUniformName(name);
    if (!uniformName) {
      return "";
    }
    if (typeof value === "number" || typeof value === "boolean") {
      return "uniform float " + uniformName + ";";
    }
    if (Array.isArray(value) || (value && typeof value.length === "number")) {
      const len = Math.max(0, Math.floor(sceneNumber(value.length, 0)));
      if (len >= 4) return "uniform vec4 " + uniformName + ";";
      if (len === 3) return "uniform vec3 " + uniformName + ";";
      if (len === 2) return "uniform vec2 " + uniformName + ";";
    }
    return "";
  }

  function scenePBRCustomUniformDeclarations(uniforms) {
    if (!uniforms || typeof uniforms !== "object") {
      return "";
    }
    const lines = [];
    const names = Object.keys(uniforms).sort();
    for (var i = 0; i < names.length; i++) {
      const declaration = scenePBRCustomUniformDeclaration(names[i], uniforms[names[i]]);
      if (declaration) {
        lines.push(declaration);
      }
    }
    return lines.join("\n");
  }

  function scenePBRCustomHookSource(source, functionName, signature) {
    const body = typeof source === "string" ? source.trim() : "";
    if (!body) {
      return "";
    }
    if (body.indexOf(functionName) >= 0) {
      return body;
    }
    return "void " + functionName + signature + " {\n" + body + "\n}";
  }

  function scenePBRBuildCustomVertexSource(material) {
    const uniforms = scenePBRCustomUniformDeclarations(material && material.customUniforms);
    const custom = scenePBRCustomHookSource(
      material && material.customVertex,
      "gosxApplyCustomVertex",
      "(inout vec3 position, inout vec3 normal, inout vec2 uv)",
    );
    if (!uniforms && !custom) {
      return SCENE_PBR_VERTEX_SOURCE;
    }
    const hook = custom || "void gosxApplyCustomVertex(inout vec3 position, inout vec3 normal, inout vec2 uv) {}";
    return SCENE_PBR_VERTEX_SOURCE.replace(
      "void gosxApplyCustomVertex(inout vec3 position, inout vec3 normal, inout vec2 uv) {}",
      [uniforms, hook].filter(Boolean).join("\n\n"),
    );
  }

  function scenePBRBuildCustomFragmentSource(material) {
    const uniforms = scenePBRCustomUniformDeclarations(material && material.customUniforms);
    const custom = scenePBRCustomHookSource(
      material && material.customFragment,
      "gosxApplyCustomFragment",
      "(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv)",
    );
    if (!uniforms && !custom) {
      return SCENE_PBR_FRAGMENT_SOURCE;
    }
    const hook = custom || "void gosxApplyCustomFragment(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv) {}";
    const replacement = [uniforms, hook].filter(Boolean).join("\n\n");
    return SCENE_PBR_FRAGMENT_SOURCE.replace(
      "void gosxApplyCustomFragment(inout vec3 color, inout float opacity, vec3 normal, vec3 worldPosition, vec2 uv) {}",
      replacement,
    );
  }

  function scenePBRCustomUniformLocations(gl, program, values) {
    const out = {};
    if (!values || typeof values !== "object") {
      return out;
    }
    const names = Object.keys(values);
    for (var i = 0; i < names.length; i++) {
      const name = scenePBRCustomUniformName(names[i]);
      if (!name) {
        continue;
      }
      const location = gl.getUniformLocation(program, name);
      if (location) {
        out[name] = location;
      }
    }
    return out;
  }

  function scenePBRHasCustomHooks(material) {
    return Boolean(
      material &&
      normalizeSceneMaterialKind(material.kind) === "custom" &&
      material.shaderBackend !== "selena" &&
      (
        (typeof material.customVertex === "string" && material.customVertex.trim()) ||
        (typeof material.customFragment === "string" && material.customFragment.trim()) ||
        (material.customUniforms && typeof material.customUniforms === "object" && Object.keys(material.customUniforms).length > 0)
      )
    );
  }

  // Compile PBR vertex + fragment shaders and return a program object with
  // cached uniform locations. Returns null on compile/link failure so the
  // caller can fall back to the legacy renderer.
  function createScenePBRProgram(gl) {
    const vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_PBR_VERTEX_SOURCE);
    if (!vertexShader) {
      return null;
    }
    const fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_PBR_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    const program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "PBR shader");
    if (!program) return null;

    // Cache attribute locations.
    const attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      normal: gl.getAttribLocation(program, "a_normal"),
      uv: gl.getAttribLocation(program, "a_uv"),
      tangent: gl.getAttribLocation(program, "a_tangent"),
    };

    // Cache uniform locations.
    const uniforms = scenePBRCacheBaseUniforms(gl, program);

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  function sceneSelenaMaterialLayout(material) {
    var layout = material && material.shaderLayout;
    if (!layout || typeof layout !== "object") return null;
    if (!layout.uniformBlock || typeof layout.uniformBlock !== "object") return null;
    if (!Array.isArray(layout.uniformBlock.fields)) return null;
    return layout;
  }

  function sceneSelenaIsMaterial(material) {
    return Boolean(
      material &&
      material.shaderBackend === "selena" &&
      sceneSelenaMaterialLayout(material) &&
      typeof material.customVertex === "string" &&
      material.customVertex.trim() &&
      typeof material.customFragment === "string" &&
      material.customFragment.trim()
    );
  }

  function sceneSelenaFloatCount(type) {
    switch (String(type || "")) {
    case "float": return 1;
    case "vec2": return 2;
    case "vec3": return 3;
    case "vec4": return 4;
    case "mat3": return 9;
    case "mat4": return 16;
    default: return 1;
    }
  }

  function sceneSelenaAttributeComponents(type) {
    switch (String(type || "")) {
    case "vec2": return 2;
    case "vec4": return 4;
    case "vec3":
    default:
      return 3;
    }
  }

  function sceneSelenaUniformDefault(layout, name) {
    var defaults = layout && layout.uniformBlock && Array.isArray(layout.uniformBlock.defaults)
      ? layout.uniformBlock.defaults
      : [];
    for (var i = 0; i < defaults.length; i++) {
      if (defaults[i] && defaults[i].name === name) {
        return defaults[i].values;
      }
    }
    return undefined;
  }

  function sceneSelenaTextureURL(material, texture, index) {
    var name = texture && texture.name;
    var values = material && material.customUniforms;
    if (values && typeof values === "object" && name && typeof values[name] === "string" && values[name].trim()) {
      return values[name].trim();
    }
    if (material && name && typeof material[name] === "string" && material[name].trim()) {
      return material[name].trim();
    }
    if (index === 0 && material && typeof material.texture === "string" && material.texture.trim()) {
      return material.texture.trim();
    }
    return "";
  }

  function sceneSelenaTextureDescriptors(layout) {
    return layout && Array.isArray(layout.textures) ? layout.textures : [];
  }

  function sceneSelenaUniformLocations(gl, program, layout) {
    var fields = layout && layout.uniformBlock && Array.isArray(layout.uniformBlock.fields)
      ? layout.uniformBlock.fields
      : [];
    var uniforms = {};
    for (var i = 0; i < fields.length; i++) {
      var field = fields[i];
      if (!field || typeof field.name !== "string") continue;
      uniforms[field.name] = gl.getUniformLocation(program, field.name);
    }
    return uniforms;
  }

  function createSceneSelenaProgram(gl, material) {
    var layout = sceneSelenaMaterialLayout(material);
    if (!layout) return null;
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, material.customVertex);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, material.customFragment);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }
    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Selena shader");
    if (!program) return null;
    var attrs = {};
    var layoutAttrs = Array.isArray(layout.attributes) ? layout.attributes : [];
    for (var i = 0; i < layoutAttrs.length; i++) {
      var attr = layoutAttrs[i] || {};
      if (typeof attr.name !== "string") continue;
      attrs[attr.name] = {
        loc: gl.getAttribLocation(program, attr.name),
        size: sceneSelenaAttributeComponents(attr.type),
      };
    }
    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attrs,
      uniforms: sceneSelenaUniformLocations(gl, program, layout),
      layout: layout,
    };
  }

  function createScenePBRCustomProgram(gl, material) {
    const vertexSource = scenePBRBuildCustomVertexSource(material);
    const fragmentSource = scenePBRBuildCustomFragmentSource(material);
    const vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, vertexSource);
    if (!vertexShader) {
      return null;
    }
    const fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, fragmentSource);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }
    const program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Custom PBR shader");
    if (!program) return null;
    const attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      normal: gl.getAttribLocation(program, "a_normal"),
      uv: gl.getAttribLocation(program, "a_uv"),
      tangent: gl.getAttribLocation(program, "a_tangent"),
    };
    const uniforms = scenePBRCacheBaseUniforms(gl, program);
    uniforms.customUniforms = scenePBRCustomUniformLocations(gl, program, material && material.customUniforms);
    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  // Compile the skinned PBR vertex shader with the same PBR fragment shader.
  // Returns a program object with cached attribute/uniform locations including
  // the joint matrix array and skin flag, or null on failure.
  function createScenePBRSkinnedProgram(gl) {
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_PBR_SKINNED_VERTEX_SOURCE);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_PBR_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Skinned PBR shader");
    if (!program) return null;

    // Cache attribute locations.
    var attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      normal: gl.getAttribLocation(program, "a_normal"),
      uv: gl.getAttribLocation(program, "a_uv"),
      tangent: gl.getAttribLocation(program, "a_tangent"),
      joints: gl.getAttribLocation(program, "a_joints"),
      weights: gl.getAttribLocation(program, "a_weights"),
    };

    // Cache uniform locations — base set plus skinning extras. The joint
    // matrix array is uploaded from its first slot in one call per skinned
    // draw; caching 64 individual locations made every fighter pay 64
    // lookups at compile time and 64 uniform uploads per frame.
    var uniforms = scenePBRCacheBaseUniforms(gl, program);
    uniforms.modelMatrix = gl.getUniformLocation(program, "u_modelMatrix");
    uniforms.hasSkin = gl.getUniformLocation(program, "u_hasSkin");
    uniforms.jointMatrices = gl.getUniformLocation(program, "u_jointMatrices[0]");

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  // Compile the points vertex + fragment shaders and return a program object
  // with cached attribute/uniform locations, or null on failure.
  function createScenePointsProgram(gl) {
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_POINTS_VERTEX_SOURCE);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_POINTS_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Points shader");
    if (!program) return null;

    var attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      size: gl.getAttribLocation(program, "a_size"),
      color: gl.getAttribLocation(program, "a_color"),
    };

    var uniforms = {
      viewMatrix: gl.getUniformLocation(program, "u_viewMatrix"),
      projectionMatrix: gl.getUniformLocation(program, "u_projectionMatrix"),
      modelMatrix: gl.getUniformLocation(program, "u_modelMatrix"),
      defaultSize: gl.getUniformLocation(program, "u_defaultSize"),
      defaultColor: gl.getUniformLocation(program, "u_defaultColor"),
      hasPerVertexColor: gl.getUniformLocation(program, "u_hasPerVertexColor"),
      hasPerVertexSize: gl.getUniformLocation(program, "u_hasPerVertexSize"),
      sizeAttenuation: gl.getUniformLocation(program, "u_sizeAttenuation"),
      pointStyle: gl.getUniformLocation(program, "u_pointStyle"),
      viewportHeight: gl.getUniformLocation(program, "u_viewportHeight"),
      minPixelSize: gl.getUniformLocation(program, "u_minPixelSize"),
      maxPixelSize: gl.getUniformLocation(program, "u_maxPixelSize"),
      opacity: gl.getUniformLocation(program, "u_opacity"),
      hasFog: gl.getUniformLocation(program, "u_hasFog"),
      fogDensity: gl.getUniformLocation(program, "u_fogDensity"),
      fogColor: gl.getUniformLocation(program, "u_fogColor"),
    };

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  // Compile the instanced PBR vertex shader with the shared PBR fragment shader.
  // Returns a program object with cached attribute/uniform locations, or null.
  function createScenePBRInstancedProgram(gl) {
    var vertexShader = scenePBRCompileShader(gl, gl.VERTEX_SHADER, SCENE_PBR_INSTANCED_VERTEX_SOURCE);
    if (!vertexShader) return null;
    var fragmentShader = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, SCENE_PBR_FRAGMENT_SOURCE);
    if (!fragmentShader) {
      gl.deleteShader(vertexShader);
      return null;
    }

    var program = scenePBRLinkProgram(gl, vertexShader, fragmentShader, "Instanced PBR shader");
    if (!program) return null;

    var attributes = {
      position: gl.getAttribLocation(program, "a_position"),
      normal: gl.getAttribLocation(program, "a_normal"),
      uv: gl.getAttribLocation(program, "a_uv"),
      tangent: gl.getAttribLocation(program, "a_tangent"),
      instanceMatrix: gl.getAttribLocation(program, "a_instanceMatrix"),
      instanceColor: gl.getAttribLocation(program, "a_instanceColor"),
    };

    var uniforms = scenePBRCacheBaseUniforms(gl, program);
    uniforms.hasInstanceColor = gl.getUniformLocation(program, "u_hasInstanceColor");

    return {
      program: program,
      vertexShader: vertexShader,
      fragmentShader: fragmentShader,
      attributes: attributes,
      uniforms: uniforms,
    };
  }

  // Generate PBR vertex data (positions, normals, UVs, tangents) for a geometry
  // kind. Returns { positions, normals, uvs, tangents, vertexCount } where each
  // array is a Float32Array ready for GPU upload.
  function generateInstancedGeometry(kind, dims) {
    kind = normalizeInstancedGeometryKind(kind);
    var w = sceneNumber(dims && dims.width, 1);
    var h = sceneNumber(dims && dims.height, 1);
    var d = sceneNumber(dims && dims.depth, 1);
    var size = sceneNumber(dims && dims.size, 0);
    if (kind === "cube" && size > 0) {
      w = size;
      h = size;
      d = size;
    }

    if (kind === "sphere") {
      return generateInstancedSphereGeometry(
        sceneNumber(dims && dims.radius, 0.5),
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "plane") {
      return generateInstancedPlaneGeometry(w, d);
    }
    if (kind === "pyramid") {
      return generateInstancedPyramidGeometry(w, h, d);
    }
    if (kind === "cylinder") {
      return generateInstancedCylinderGeometry(
        sceneNumber(dims && dims.radiusTop, sceneNumber(dims && dims.radius, 0.5)),
        sceneNumber(dims && dims.radiusBottom, sceneNumber(dims && dims.radius, 0.5)),
        h,
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "cone") {
      return generateInstancedCylinderGeometry(
        0,
        sceneNumber(dims && dims.radiusBottom, sceneNumber(dims && dims.radius, 0.5)),
        h,
        sceneNumber(dims && dims.segments, 32)
      );
    }
    if (kind === "torus") {
      return generateInstancedTorusGeometry(
        sceneNumber(dims && dims.radius, 0.7),
        sceneNumber(dims && dims.tube, 0.3),
        sceneNumber(dims && dims.radialSegments, 32),
        sceneNumber(dims && dims.tubularSegments, 16)
      );
    }

    // Default: box geometry.
    return generateInstancedBoxGeometry(w, h, d);
  }

  function normalizeInstancedGeometryKind(kind) {
    if (typeof normalizeSceneKind === "function") {
      return normalizeSceneKind(kind);
    }
    var text = typeof kind === "string" ? kind.trim().toLowerCase() : "";
    switch (text) {
      case "cubegeometry":
        return "cube";
      case "boxgeometry":
        return "box";
      case "planegeometry":
      case "quad":
      case "quadgeometry":
        return "plane";
      case "pyramidgeometry":
        return "pyramid";
      case "spheregeometry":
      case "uvsphere":
      case "uvspheregeometry":
        return "sphere";
      case "cylindergeometry":
        return "cylinder";
      case "conegeometry":
        return "cone";
      case "torusgeometry":
        return "torus";
      case "torusknotgeometry":
      case "torus-knot":
        return "torusknot";
      default:
        return text || "box";
    }
  }

  // Generate a unit box with the given dimensions. 36 vertices (12 triangles).
  // Each face has outward normals, [0,1] UVs, and MikkTSpace-compatible tangents.
  function generateInstancedBoxGeometry(w, h, d) {
    var hw = w * 0.5, hh = h * 0.5, hd = d * 0.5;

    // 6 faces × 2 triangles × 3 vertices = 36 vertices.
    // Each face: normal, tangent(vec4), 4 corners → 2 triangles.
    var faces = [
      // +Z face (front)
      { n: [0, 0, 1], t: [1, 0, 0, 1], v: [[-hw,-hh,hd],[hw,-hh,hd],[hw,hh,hd],[-hw,hh,hd]] },
      // -Z face (back)
      { n: [0, 0,-1], t: [-1, 0, 0, 1], v: [[hw,-hh,-hd],[-hw,-hh,-hd],[-hw,hh,-hd],[hw,hh,-hd]] },
      // +X face (right)
      { n: [1, 0, 0], t: [0, 0,-1, 1], v: [[hw,-hh,hd],[hw,-hh,-hd],[hw,hh,-hd],[hw,hh,hd]] },
      // -X face (left)
      { n: [-1, 0, 0], t: [0, 0, 1, 1], v: [[-hw,-hh,-hd],[-hw,-hh,hd],[-hw,hh,hd],[-hw,hh,-hd]] },
      // +Y face (top)
      { n: [0, 1, 0], t: [1, 0, 0, 1], v: [[-hw,hh,hd],[hw,hh,hd],[hw,hh,-hd],[-hw,hh,-hd]] },
      // -Y face (bottom)
      { n: [0,-1, 0], t: [1, 0, 0, 1], v: [[-hw,-hh,-hd],[hw,-hh,-hd],[hw,-hh,hd],[-hw,-hh,hd]] },
    ];

    var quadUVs = [[0,0],[1,0],[1,1],[0,1]];
    var triIndices = [0,1,2, 0,2,3];

    var vertexCount = 36;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var vi = 0;
    for (var fi = 0; fi < 6; fi++) {
      var face = faces[fi];
      for (var ti = 0; ti < 6; ti++) {
        var ci = triIndices[ti];
        var p = face.v[ci];
        positions[vi * 3]     = p[0];
        positions[vi * 3 + 1] = p[1];
        positions[vi * 3 + 2] = p[2];
        normals[vi * 3]     = face.n[0];
        normals[vi * 3 + 1] = face.n[1];
        normals[vi * 3 + 2] = face.n[2];
        uvs[vi * 2]     = quadUVs[ci][0];
        uvs[vi * 2 + 1] = quadUVs[ci][1];
        tangents[vi * 4]     = face.t[0];
        tangents[vi * 4 + 1] = face.t[1];
        tangents[vi * 4 + 2] = face.t[2];
        tangents[vi * 4 + 3] = face.t[3];
        vi++;
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a plane (quad) with the given width and depth, lying in the XZ plane.
  // 6 vertices (2 triangles), face normal pointing up (+Y).
  function generateInstancedPlaneGeometry(w, d) {
    var hw = w * 0.5, hd = d * 0.5;
    var vertexCount = 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);

    var corners = [[-hw, 0, hd], [hw, 0, hd], [hw, 0, -hd], [-hw, 0, -hd]];
    var cornerUVs = [[0, 0], [1, 0], [1, 1], [0, 1]];
    var triIndices = [0, 1, 2, 0, 2, 3];

    for (var i = 0; i < 6; i++) {
      var ci = triIndices[i];
      var p = corners[ci];
      positions[i * 3] = p[0]; positions[i * 3 + 1] = p[1]; positions[i * 3 + 2] = p[2];
      normals[i * 3] = 0; normals[i * 3 + 1] = 1; normals[i * 3 + 2] = 0;
      uvs[i * 2] = cornerUVs[ci][0]; uvs[i * 2 + 1] = cornerUVs[ci][1];
      tangents[i * 4] = 1; tangents[i * 4 + 1] = 0; tangents[i * 4 + 2] = 0; tangents[i * 4 + 3] = 1;
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vertexCount };
  }

  // Generate a UV sphere with the given radius and segment count.
  function generateInstancedSphereGeometry(radius, segments) {
    var slices = instancedSegmentCount(segments, 32, 3, 256);
    var rings = Math.max(2, Math.floor(slices / 2));

    // Count: each ring-slice quad = 2 triangles = 6 vertices,
    // except the top and bottom caps which are single triangles.
    var vertexCount = rings * slices * 6;
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;

    function spherePoint(ring, slice) {
      var phi = (ring / rings) * Math.PI;
      var theta = (slice / slices) * Math.PI * 2;
      var sp = Math.sin(phi);
      var nx = sp * Math.cos(theta);
      var ny = Math.cos(phi);
      var nz = sp * Math.sin(theta);
      return {
        px: nx * radius, py: ny * radius, pz: nz * radius,
        nx: nx, ny: ny, nz: nz,
        u: slice / slices, v: ring / rings,
        tx: -Math.sin(theta), ty: 0, tz: Math.cos(theta),
      };
    }

    function pushVert(pt) {
      positions[vi * 3] = pt.px; positions[vi * 3 + 1] = pt.py; positions[vi * 3 + 2] = pt.pz;
      normals[vi * 3] = pt.nx; normals[vi * 3 + 1] = pt.ny; normals[vi * 3 + 2] = pt.nz;
      uvs[vi * 2] = pt.u; uvs[vi * 2 + 1] = pt.v;
      tangents[vi * 4] = pt.tx; tangents[vi * 4 + 1] = pt.ty; tangents[vi * 4 + 2] = pt.tz; tangents[vi * 4 + 3] = 1;
      vi++;
    }

    for (var r = 0; r < rings; r++) {
      for (var s = 0; s < slices; s++) {
        var a = spherePoint(r, s);
        var b = spherePoint(r, s + 1);
        var c = spherePoint(r + 1, s + 1);
        var dd = spherePoint(r + 1, s);
        pushVert(a); pushVert(b); pushVert(c);
        pushVert(a); pushVert(c); pushVert(dd);
      }
    }

    return { positions: positions, normals: normals, uvs: uvs, tangents: tangents, vertexCount: vi };
  }

  function instancedSegmentCount(value, fallback, minValue, maxValue) {
    var count = Math.round(sceneNumber(value, fallback));
    return Math.max(minValue, Math.min(maxValue, count));
  }

  function instancedPositiveNumber(value, fallback) {
    var number = sceneNumber(value, fallback);
    return number > 0 ? number : fallback;
  }

  function instancedNormalize3(x, y, z) {
    var length = Math.sqrt(x * x + y * y + z * z);
    if (!Number.isFinite(length) || length <= 0.000001) {
      return [0, 1, 0];
    }
    return [x / length, y / length, z / length];
  }

  function instancedTriangleNormal(a, b, c) {
    var abx = b[0] - a[0];
    var aby = b[1] - a[1];
    var abz = b[2] - a[2];
    var acx = c[0] - a[0];
    var acy = c[1] - a[1];
    var acz = c[2] - a[2];
    return instancedNormalize3(
      aby * acz - abz * acy,
      abz * acx - abx * acz,
      abx * acy - aby * acx
    );
  }

  function createInstancedGeometryWriter(vertexCount) {
    var positions = new Float32Array(vertexCount * 3);
    var normals = new Float32Array(vertexCount * 3);
    var uvs = new Float32Array(vertexCount * 2);
    var tangents = new Float32Array(vertexCount * 4);
    var vi = 0;
    function push(position, normal, uv, tangent) {
      positions[vi * 3] = position[0];
      positions[vi * 3 + 1] = position[1];
      positions[vi * 3 + 2] = position[2];
      normals[vi * 3] = normal[0];
      normals[vi * 3 + 1] = normal[1];
      normals[vi * 3 + 2] = normal[2];
      uvs[vi * 2] = uv[0];
      uvs[vi * 2 + 1] = uv[1];
      tangents[vi * 4] = tangent[0];
      tangents[vi * 4 + 1] = tangent[1];
      tangents[vi * 4 + 2] = tangent[2];
      tangents[vi * 4 + 3] = tangent[3];
      vi++;
    }
    function build() {
      return {
        positions: vi * 3 === positions.length ? positions : positions.subarray(0, vi * 3),
        normals: vi * 3 === normals.length ? normals : normals.subarray(0, vi * 3),
        uvs: vi * 2 === uvs.length ? uvs : uvs.subarray(0, vi * 2),
        tangents: vi * 4 === tangents.length ? tangents : tangents.subarray(0, vi * 4),
        vertexCount: vi,
      };
    }
    return { push: push, build: build };
  }

  function pushInstancedFlatTri(writer, p0, p1, p2, uv0, uv1, uv2) {
    var normal = instancedTriangleNormal(p0, p1, p2);
    var tangent3 = instancedNormalize3(p1[0] - p0[0], p1[1] - p0[1], p1[2] - p0[2]);
    var tangent = [tangent3[0], tangent3[1], tangent3[2], 1];
    writer.push(p0, normal, uv0, tangent);
    writer.push(p1, normal, uv1, tangent);
    writer.push(p2, normal, uv2, tangent);
  }

  function generateInstancedPyramidGeometry(w, h, d) {
    var hw = instancedPositiveNumber(w, 1) * 0.5;
    var hh = instancedPositiveNumber(h, 1) * 0.5;
    var hd = instancedPositiveNumber(d, 1) * 0.5;
    var base = [[-hw, -hh, -hd], [hw, -hh, -hd], [hw, -hh, hd], [-hw, -hh, hd]];
    var apex = [0, hh, 0];
    var writer = createInstancedGeometryWriter(18);

    pushInstancedFlatTri(writer, base[0], base[1], base[2], [0, 0], [1, 0], [1, 1]);
    pushInstancedFlatTri(writer, base[0], base[2], base[3], [0, 0], [1, 1], [0, 1]);
    for (var i = 0; i < 4; i++) {
      pushInstancedFlatTri(writer, base[i], apex, base[(i + 1) % 4], [0, 1], [0.5, 0], [1, 1]);
    }
    return writer.build();
  }

  function generateInstancedCylinderGeometry(radiusTop, radiusBottom, height, segments) {
    var rt = Math.max(0, sceneNumber(radiusTop, 0.5));
    var rb = Math.max(0, sceneNumber(radiusBottom, 0.5));
    var h = instancedPositiveNumber(height, 1);
    if (rt === 0 && rb === 0) {
      rb = 0.5;
    }
    var count = instancedSegmentCount(segments, 32, 3, 256);
    var vertsPerSegment = (rt > 0 && rb > 0 ? 6 : 3) + (rb > 0 ? 3 : 0) + (rt > 0 ? 3 : 0);
    var writer = createInstancedGeometryWriter(count * vertsPerSegment);
    var halfH = h * 0.5;
    var slopeY = (rb - rt) / h;

    for (var i = 0; i < count; i++) {
      var u0 = i / count;
      var u1 = (i + 1) / count;
      var th0 = (Math.PI * 2 * i) / count;
      var th1 = (Math.PI * 2 * (i + 1)) / count;
      var c0 = Math.cos(th0);
      var s0 = Math.sin(th0);
      var c1 = Math.cos(th1);
      var s1 = Math.sin(th1);
      var n0 = instancedNormalize3(c0, slopeY, s0);
      var n1 = instancedNormalize3(c1, slopeY, s1);
      var t0 = [-s0, 0, c0, 1];
      var t1 = [-s1, 0, c1, 1];
      var b0 = [rb * c0, -halfH, rb * s0];
      var b1 = [rb * c1, -halfH, rb * s1];
      var top0 = [rt * c0, halfH, rt * s0];
      var top1 = [rt * c1, halfH, rt * s1];

      if (rb > 0 && rt > 0) {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top1, n1, [u1, 0], t1);
        writer.push(b1, n1, [u1, 1], t1);
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top0, n0, [u0, 0], t0);
        writer.push(top1, n1, [u1, 0], t1);
      } else if (rt === 0) {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top1, n1, [u1, 0], t1);
        writer.push(b1, n1, [u1, 1], t1);
      } else {
        writer.push(b0, n0, [u0, 1], t0);
        writer.push(top0, n0, [u0, 0], t0);
        writer.push(top1, n1, [u1, 0], t1);
      }

      if (rb > 0) {
        writer.push([0, -halfH, 0], [0, -1, 0], [0.5, 0.5], [1, 0, 0, 1]);
        writer.push(b0, [0, -1, 0], [0.5 + c0 * 0.5, 0.5 + s0 * 0.5], [1, 0, 0, 1]);
        writer.push(b1, [0, -1, 0], [0.5 + c1 * 0.5, 0.5 + s1 * 0.5], [1, 0, 0, 1]);
      }
      if (rt > 0) {
        writer.push([0, halfH, 0], [0, 1, 0], [0.5, 0.5], [1, 0, 0, 1]);
        writer.push(top1, [0, 1, 0], [0.5 + c1 * 0.5, 0.5 + s1 * 0.5], [1, 0, 0, 1]);
        writer.push(top0, [0, 1, 0], [0.5 + c0 * 0.5, 0.5 + s0 * 0.5], [1, 0, 0, 1]);
      }
    }
    return writer.build();
  }

  function generateInstancedTorusGeometry(radius, tube, radialSegments, tubularSegments) {
    var major = instancedPositiveNumber(radius, 0.7);
    var minor = instancedPositiveNumber(tube, 0.3);
    var radial = instancedSegmentCount(radialSegments, 32, 3, 256);
    var tubular = instancedSegmentCount(tubularSegments, 16, 3, 128);
    var writer = createInstancedGeometryWriter(radial * tubular * 6);

    function vertexAt(i, j) {
      var u = (Math.PI * 2 * i) / radial;
      var v = (Math.PI * 2 * j) / tubular;
      var cu = Math.cos(u);
      var su = Math.sin(u);
      var cv = Math.cos(v);
      var sv = Math.sin(v);
      var r = major + minor * cv;
      return {
        position: [r * cu, minor * sv, r * su],
        normal: instancedNormalize3(cv * cu, sv, cv * su),
        uv: [i / radial, j / tubular],
        tangent: [-su, 0, cu, 1],
      };
    }

    function pushTorusVertex(v) {
      writer.push(v.position, v.normal, v.uv, v.tangent);
    }

    for (var i = 0; i < radial; i++) {
      for (var j = 0; j < tubular; j++) {
        var a = vertexAt(i, j);
        var b = vertexAt(i, j + 1);
        var c = vertexAt(i + 1, j);
        var dd = vertexAt(i + 1, j + 1);
        pushTorusVertex(a);
        pushTorusVertex(b);
        pushTorusVertex(c);
        pushTorusVertex(c);
        pushTorusVertex(b);
        pushTorusVertex(dd);
      }
    }
    return writer.build();
  }

  function scenePBRCompileShader(gl, type, source) {
    const shader = gl.createShader(type);
    gl.shaderSource(shader, source);
    gl.compileShader(shader);
    if (!gl.getShaderParameter(shader, gl.COMPILE_STATUS)) {
      const label = type === gl.VERTEX_SHADER ? "vertex" : "fragment";
      console.warn("[gosx] PBR " + label + " shader compile failed:", gl.getShaderInfoLog(shader));
      gl.deleteShader(shader);
      return null;
    }
    return shader;
  }

  // Link a vertex and fragment shader into a program, with error logging.
  // Returns the linked program or null on failure (cleans up shaders on error).
  function scenePBRLinkProgram(gl, vertexShader, fragmentShader, label) {
    var program = gl.createProgram();
    gl.attachShader(program, vertexShader);
    gl.attachShader(program, fragmentShader);
    gl.linkProgram(program);
    if (!gl.getProgramParameter(program, gl.LINK_STATUS)) {
      console.warn("[gosx] " + label + " program link failed:", gl.getProgramInfoLog(program));
      gl.deleteProgram(program);
      gl.deleteShader(vertexShader);
      gl.deleteShader(fragmentShader);
      return null;
    }
    return program;
  }

  // --- Light Uniform Upload ---

  // Shared scratch for number → u32 bit reinterpretation used by the light
  // hash. Allocated once at module level; safe because the hash function
  // is called synchronously per upload and never recursively.
  var _scenePBRLightsHashBuf = new ArrayBuffer(4);
  var _scenePBRLightsHashFloat = new Float32Array(_scenePBRLightsHashBuf);
  var _scenePBRLightsHashInt = new Uint32Array(_scenePBRLightsHashBuf);

  function scenePBRLightsHashNumber(h, n) {
    _scenePBRLightsHashFloat[0] = (typeof n === "number" && n === n) ? n : 0;
    return Math.imul((h ^ _scenePBRLightsHashInt[0]) >>> 0, 16777619) >>> 0;
  }

  function scenePBRLightsHashString(h, s) {
    var str = (typeof s === "string") ? s : "";
    var len = str.length;
    for (var i = 0; i < len; i++) {
      h = Math.imul((h ^ str.charCodeAt(i)) >>> 0, 16777619) >>> 0;
    }
    // Length-delimit to distinguish "ab" + "c" from "a" + "bc".
    return Math.imul((h ^ (len + 1)) >>> 0, 16777619) >>> 0;
  }

  // hashLightContent computes the per-light sub-hash the frame-level
  // scenePBRLightsHash combines. Called from normalizeSceneLight (in
  // 10-runtime-scene-core.js) whenever a light is created or patched,
  // so the expensive string/number walk runs at mutation time — rare —
  // instead of per-frame. The result is stamped onto the light object
  // as `_lightHash` and read by scenePBRLightsHash without rehashing.
  //
  // Kept in 16-scene-webgl.js alongside scenePBRLightsHash so the two
  // must agree on what fields contribute to the hash; moving either
  // without the other is a correctness bug.
  function hashLightContent(l) {
    if (!l) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, l.kind);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.x, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.y, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.z, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionX, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionY, -1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionZ, 0));
    h = scenePBRLightsHashString(h, l.color);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.intensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.range, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.decay, 2));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.angle, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.penumbra, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.width, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.height, 0));
    h = scenePBRLightsHashString(h, l.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowBias, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSize, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowCascades, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSoftness, 0));
    return h;
  }

  // hashEnvironmentContent is the env-side counterpart to hashLightContent.
  // Called from normalizeSceneEnvironment and sceneResolveLightingEnvironment
  // whenever the environment is normalized so the cached sub-hash travels
  // with the environment object downstream.
  function hashEnvironmentContent(env) {
    if (!env) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, env.ambientColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.ambientIntensity, 0));
    h = scenePBRLightsHashString(h, env.skyColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.skyIntensity, 0));
    h = scenePBRLightsHashString(h, env.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.groundIntensity, 0));
    h = scenePBRLightsHashString(h, env.envMap);
    h = scenePBRLightsHashNumber(h, sceneNumber(env.envIntensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.envRotation, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(env.fogDensity, 0));
    h = scenePBRLightsHashString(h, env.fogColor);
    return h;
  }

  // scenePBRLightsHash combines per-light and per-environment cached
  // sub-hashes into a 32-bit frame hash. Called once per frame (hoisted
  // at the top of render()). Previously walked every field of every
  // light + environment on every call, which cost ~13µs per invocation
  // on a 3-light fixture — more than the mock upload it was gating!
  // Now reads the cached _lightHash from each light (stamped at
  // normalize time) and _envHash from the environment, mixing them
  // with the light count. Total cost: ~100ns for typical scenes, down
  // from 13µs. A 130× speedup on the dirty-tracking fast path.
  //
  // Falls back to the full hashLightContent / hashEnvironmentContent
  // walk when a light or environment lacks a cached stamp — keeps the
  // function correct for callers that construct raw objects outside
  // the normalize path (e.g. the bench harness).
  function scenePBRLightsHash(lights, environment) {
    var h = 2166136261;
    var lightArray = Array.isArray(lights) ? lights : [];
    var count = Math.min(lightArray.length, 8);
    h = Math.imul((h ^ count) >>> 0, 16777619) >>> 0;
    for (var i = 0; i < count; i++) {
      var l = lightArray[i];
      var sub = (l && typeof l._lightHash === "number") ? l._lightHash : hashLightContent(l);
      h = Math.imul((h ^ (sub >>> 0)) >>> 0, 16777619) >>> 0;
    }
    var envSub = (environment && typeof environment._envHash === "number")
      ? environment._envHash
      : hashEnvironmentContent(environment);
    h = Math.imul((h ^ (envSub >>> 0)) >>> 0, 16777619) >>> 0;
    return h;
  }

  // Upload the light array and environment uniforms for the current frame.
  // Dirty-tracked per uniforms object via a stamp on the uniforms itself —
  // each program (main, skinned, instanced) has its own uniforms struct
  // and therefore its own stamp. On a static scene the 3 per-frame calls
  // (one per program that needs the data) all early-out after the first
  // frame, saving roughly 90 gl.uniform* calls per frame.
  //
  // The optional `precomputedHash` parameter lets the caller hoist the
  // hash out of the per-upload path when a single frame does multiple
  // uploads (typical: main + skinned + instanced programs each call this
  // once). The bench harness at client/js/runtime.bench.js confirmed the
  // hash itself takes ~13µs on a 3-light fixture — cheaper than a real
  // WebGL upload but not free, so computing it once per frame and
  // sharing across the 3 call sites drops per-frame overhead from ~39µs
  // to ~13µs even in the worst case (full miss every frame).
  function scenePBRUploadLights(gl, uniforms, lights, environment, precomputedHash) {
    const contentHash = (typeof precomputedHash === "number")
      ? precomputedHash
      : scenePBRLightsHash(lights, environment);
    if (uniforms._lastLightsHash === contentHash) {
      return;
    }
    uniforms._lastLightsHash = contentHash;

    const lightArray = Array.isArray(lights) ? lights : [];
    const count = Math.min(lightArray.length, 8);

    // Per-call color cache: eliminates redundant sceneColorRGBA regex parsing
    // when multiple lights share the same color string within a single frame.
    var colorCache = {};

    gl.uniform1i(uniforms.lightCount, count);

    for (var i = 0; i < count; i++) {
      const light = lightArray[i];
      const kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";

      var lightType = 2; // default: point
      if (kind === "ambient") {
        lightType = 0;
      } else if (kind === "directional") {
        lightType = 1;
      } else if (kind === "spot") {
        lightType = 3;
      } else if (kind === "hemisphere") {
        lightType = 4;
      } else if (kind === "rect-area") {
        lightType = 2;
      } else if (kind === "light-probe") {
        lightType = 0;
      }

      gl.uniform1i(uniforms.lightTypes[i], lightType);
      gl.uniform3f(
        uniforms.lightPositions[i],
        sceneNumber(light.x, 0),
        sceneNumber(light.y, 0),
        sceneNumber(light.z, 0)
      );
      gl.uniform3f(
        uniforms.lightDirections[i],
        sceneNumber(light.directionX, 0),
        sceneNumber(light.directionY, -1),
        sceneNumber(light.directionZ, 0)
      );

      var colorKey = light.color;
      var lightColorRGBA = typeof colorKey === "string" && colorCache[colorKey];
      if (!lightColorRGBA) {
        lightColorRGBA = sceneColorRGBA(light.color, [1, 1, 1, 1]);
        if (typeof colorKey === "string") colorCache[colorKey] = lightColorRGBA;
      }
      gl.uniform3f(uniforms.lightColors[i], lightColorRGBA[0], lightColorRGBA[1], lightColorRGBA[2]);
      gl.uniform1f(uniforms.lightIntensities[i], sceneNumber(light.intensity, 1));
      gl.uniform1f(uniforms.lightRanges[i], sceneNumber(light.range, 0));
      gl.uniform1f(uniforms.lightDecays[i], sceneNumber(light.decay, 2));
      gl.uniform1f(uniforms.lightAngles[i], sceneNumber(light.angle, 0));
      gl.uniform1f(uniforms.lightPenumbras[i], sceneNumber(light.penumbra, 0));

      // Hemisphere ground color (uses color cache).
      var gcKey = light.groundColor;
      var gcRGBA = typeof gcKey === "string" && colorCache[gcKey];
      if (!gcRGBA) {
        gcRGBA = sceneColorRGBA(light.groundColor, [0, 0, 0, 1]);
        if (typeof gcKey === "string") colorCache[gcKey] = gcRGBA;
      }
      gl.uniform3f(uniforms.lightGroundColors[i], gcRGBA[0], gcRGBA[1], gcRGBA[2]);
    }

    // Zero out unused light slots so stale data does not contribute.
    for (var j = count; j < 8; j++) {
      gl.uniform1i(uniforms.lightTypes[j], 0);
      gl.uniform3f(uniforms.lightPositions[j], 0, 0, 0);
      gl.uniform3f(uniforms.lightDirections[j], 0, -1, 0);
      gl.uniform3f(uniforms.lightColors[j], 0, 0, 0);
      gl.uniform1f(uniforms.lightIntensities[j], 0);
      gl.uniform1f(uniforms.lightRanges[j], 0);
      gl.uniform1f(uniforms.lightDecays[j], 2);
      gl.uniform1f(uniforms.lightAngles[j], 0);
      gl.uniform1f(uniforms.lightPenumbras[j], 0);
      gl.uniform3f(uniforms.lightGroundColors[j], 0, 0, 0);
    }

    // Environment uniforms — also use the per-call cache.
    const env = environment || {};
    var ambientKey = env.ambientColor;
    var ambientColorRGBA = typeof ambientKey === "string" && colorCache[ambientKey];
    if (!ambientColorRGBA) {
      ambientColorRGBA = sceneColorRGBA(env.ambientColor, [1, 1, 1, 1]);
      if (typeof ambientKey === "string") colorCache[ambientKey] = ambientColorRGBA;
    }
    gl.uniform3f(uniforms.ambientColor, ambientColorRGBA[0], ambientColorRGBA[1], ambientColorRGBA[2]);
    gl.uniform1f(uniforms.ambientIntensity, sceneNumber(env.ambientIntensity, 0));

    var skyKey = env.skyColor;
    var skyColorRGBA = typeof skyKey === "string" && colorCache[skyKey];
    if (!skyColorRGBA) {
      skyColorRGBA = sceneColorRGBA(env.skyColor, [0.88, 0.94, 1, 1]);
      if (typeof skyKey === "string") colorCache[skyKey] = skyColorRGBA;
    }
    gl.uniform3f(uniforms.skyColor, skyColorRGBA[0], skyColorRGBA[1], skyColorRGBA[2]);
    gl.uniform1f(uniforms.skyIntensity, sceneNumber(env.skyIntensity, 0));

    var groundKey = env.groundColor;
    var groundColorRGBA = typeof groundKey === "string" && colorCache[groundKey];
    if (!groundColorRGBA) {
      groundColorRGBA = sceneColorRGBA(env.groundColor, [0.12, 0.16, 0.22, 1]);
      if (typeof groundKey === "string") colorCache[groundKey] = groundColorRGBA;
    }
    gl.uniform3f(uniforms.groundColor, groundColorRGBA[0], groundColorRGBA[1], groundColorRGBA[2]);
    gl.uniform1f(uniforms.groundIntensity, sceneNumber(env.groundIntensity, 0));

    // Fog uniforms.
    var fogDensity = sceneNumber(env.fogDensity, 0);
    gl.uniform1i(uniforms.hasFog, fogDensity > 0 ? 1 : 0);
    gl.uniform1f(uniforms.fogDensity, fogDensity);
    var fogKey = env.fogColor;
    var fogColorRGBA = typeof fogKey === "string" && colorCache[fogKey];
    if (!fogColorRGBA) {
      fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);
      if (typeof fogKey === "string") colorCache[fogKey] = fogColorRGBA;
    }
    gl.uniform3f(uniforms.fogColor, fogColorRGBA[0], fogColorRGBA[1], fogColorRGBA[2]);
  }

  // Convert a tone mapping string to the shader int mode.
  // 0 = linear (no tone mapping), 1 = ACES filmic, 2 = Reinhard.
  // Default (empty string) maps to ACES.
  function sceneToneMapMode(str) {
    if (typeof str === "string") {
      var s = str.toLowerCase();
      if (s === "linear") return 0;
      if (s === "reinhard") return 2;
    }
    return 1; // default: ACES
  }

  // Upload exposure and tone mapping mode uniforms. Dirty-tracked per
  // uniforms object via direct field stamps — only 2 uniforms to compare
  // (exposure, toneMapMode), so a hash is overkill; cached primitives +
  // strict equality are allocation-free and unambiguous. Called 3 times
  // per frame alongside scenePBRUploadLights; same early-return pattern
  // saves 6 redundant gl.uniform* calls per frame on static scenes.
  //
  // _lastExposure sentinel is NaN-initialized via undefined so the first
  // frame always uploads (undefined !== any finite exposure).
  function scenePBRUploadExposure(gl, uniforms, environment, usePostProcessing) {
    var env = environment || {};
    var exposure = sceneNumber(env.exposure, 0);
    if (exposure <= 0) exposure = 1.0;
    var toneMapMode = usePostProcessing ? 0 : sceneToneMapMode(env.toneMapping);
    if (uniforms._lastExposure === exposure && uniforms._lastToneMapMode === toneMapMode) {
      return;
    }
    uniforms._lastExposure = exposure;
    uniforms._lastToneMapMode = toneMapMode;
    gl.uniform1f(uniforms.exposure, exposure);
    gl.uniform1i(uniforms.toneMapMode, toneMapMode);
  }

  // Scratch buffers for cascade matrix / split uploads — 4 cascades × 16 =
  // 64 floats for matrices, 4 floats for splits. Reused across both slots
  // and across frames to avoid per-upload allocations.
  var _scenePBRCascadeMatScratch = new Float32Array(64);
  var _scenePBRCascadeSplitScratch = new Float32Array(4);

  function scenePBREnvironmentHasMap(environment) {
    return Boolean(environment && typeof environment.envMap === "string" && environment.envMap.trim());
  }

  function scenePBRSlotCascadeCount(slot, lightIndex) {
    if (!slot || lightIndex < 0) {
      return 0;
    }
    return Math.max(1, Math.min(4, slot.numCascades | 0));
  }

  function scenePBRShadowTextureCount(shadowSlots, shadowLightIndices) {
    var slots = Array.isArray(shadowSlots) ? shadowSlots : [];
    var indices = Array.isArray(shadowLightIndices) ? shadowLightIndices : [];
    var count = 0;
    for (var i = 0; i < slots.length; i++) {
      count += scenePBRSlotCascadeCount(slots[i], indices[i]);
    }
    return count;
  }

  function scenePBRTextureLayoutForFrame(shadowSlots, shadowLightIndices, environment) {
    var shadowCount = scenePBRShadowTextureCount(shadowSlots, shadowLightIndices);
    if (scenePBREnvironmentHasMap(environment)) {
      // Keep the legacy two-shadow reservation for non-shadowed env-map scenes
      // while still moving IBL after all active CSM cascades.
      shadowCount = Math.max(2, shadowCount);
    }
    return sceneAllocateTextureUnits({
      shadowCount: shadowCount,
      ibl: scenePBREnvironmentHasMap(environment),
    });
  }

  // Upload cascaded-shadow uniforms for both slots to the given program's
  // uniforms. `shadowSlots[s]` is either null (no shadow light in slot s)
  // or an object produced by createSceneShadowSlot with up to 4 cascades.
  function scenePBRUploadShadowUniforms(gl, uniforms, shadowSlots, shadowLightIndices, lights, environment) {
    var lightArray = Array.isArray(lights) ? lights : [];
    // Allocate only the active cascade count, while reserving IBL after the
    // cascades when an envMap is present. Slot offsets are packed, not
    // hard-coded to 4-wide blocks, so two single-cascade lights use units 5/6
    // and one 4-cascade CSM light uses 5/6/7/8.
    var layout = scenePBRTextureLayoutForFrame(shadowSlots, shadowLightIndices, environment);
    var shadowUnits = layout.shadows;

    var unitBase = 0;
    uploadCascadedSlot(gl, uniforms, 0, shadowSlots[0], shadowLightIndices[0],
      lightArray, shadowUnits, unitBase);
    unitBase += scenePBRSlotCascadeCount(shadowSlots[0], shadowLightIndices[0]);
    uploadCascadedSlot(gl, uniforms, 1, shadowSlots[1], shadowLightIndices[1],
      lightArray, shadowUnits, unitBase);
  }

  function uploadCascadedSlot(gl, uniforms, slotIndex, slot, lightIndex, lightArray, shadowUnits, unitBase) {
    var samplerKeys = slotIndex === 0
      ? ["shadowMap0_0", "shadowMap0_1", "shadowMap0_2", "shadowMap0_3"]
      : ["shadowMap1_0", "shadowMap1_1", "shadowMap1_2", "shadowMap1_3"];
    var matricesKey = slotIndex === 0 ? "lightSpaceMatrices0" : "lightSpaceMatrices1";
    var splitsKey = slotIndex === 0 ? "shadowCascadeSplits0" : "shadowCascadeSplits1";
    var cascadesKey = slotIndex === 0 ? "shadowCascades0" : "shadowCascades1";
    var hasKey = slotIndex === 0 ? "hasShadow0" : "hasShadow1";
    var biasKey = slotIndex === 0 ? "shadowBias0" : "shadowBias1";
    var softKey = slotIndex === 0 ? "shadowSoftness0" : "shadowSoftness1";
    var indexKey = slotIndex === 0 ? "shadowLightIndex0" : "shadowLightIndex1";
    var base = Math.max(0, unitBase | 0);

    if (!slot || lightIndex < 0) {
      gl.uniform1i(uniforms[hasKey], 0);
      gl.uniform1f(uniforms[softKey], 0);
      gl.uniform1i(uniforms[indexKey], -1);
      gl.uniform1i(uniforms[cascadesKey], 1);
      return;
    }

    var light = lightArray[lightIndex] || {};
    var numCascades = Math.max(1, Math.min(4, slot.numCascades | 0));

    // Bind each cascade's depth texture. When the allocator doesn't have
    // enough units for all cascades, fall back to reusing cascade 0's unit
    // — the shader's c=0 branch will dominate because the split comparison
    // against Infinity always returns cascade 0.
    for (var ci = 0; ci < 4; ci++) {
      var effectiveCascade = ci < numCascades ? slot.cascades[ci] : slot.cascades[0];
      var unit = shadowUnits[base + ci];
      if (unit == null) unit = shadowUnits[base] || shadowUnits[0] || null;
      if (unit == null) continue;
      scenePBRBindTexture(gl, unit, effectiveCascade.depthTexture);
      gl.uniform1i(uniforms[samplerKeys[ci]], unit);
    }

    // Pack matrices and splits. Matrix array = 4*16 = 64 floats; cascades
    // beyond numCascades are filled with cascade 0's matrix as a safe
    // fallback (shader never selects them when numCascades is set correctly,
    // but we still want deterministic uniforms).
    for (var mi = 0; mi < 4; mi++) {
      var src = (mi < numCascades ? slot.cascades[mi] : slot.cascades[0]).lightMatrix;
      for (var k = 0; k < 16; k++) {
        _scenePBRCascadeMatScratch[mi * 16 + k] = src ? src[k] : 0;
      }
    }
    // Split array: split[c] is the view-space far plane of cascade c; the
    // shader compares viewDepth >= splits[c-1] to advance from cascade c-1
    // → c. splits[N-1] (last cascade) is still written for determinism.
    for (var si = 0; si < 4; si++) {
      _scenePBRCascadeSplitScratch[si] = si < numCascades
        ? (slot.cascades[si].splitFar || 0)
        : Infinity;
    }

    gl.uniformMatrix4fv(uniforms[matricesKey], false, _scenePBRCascadeMatScratch);
    gl.uniform1fv(uniforms[splitsKey], _scenePBRCascadeSplitScratch);
    gl.uniform1i(uniforms[cascadesKey], numCascades);
    gl.uniform1i(uniforms[hasKey], 1);
    gl.uniform1f(uniforms[biasKey], sceneNumber(light.shadowBias, 0.005));
    gl.uniform1f(uniforms[softKey], Math.max(0, sceneNumber(light.shadowSoftness, 0)));
    gl.uniform1i(uniforms[indexKey], lightIndex);
  }

  function scenePBRUploadEnvironmentMap(gl, uniforms, environment, textureCache, shadowSlots, shadowLightIndices) {
    var env = environment || {};
    var envMap = typeof env.envMap === "string" ? env.envMap.trim() : "";
    if (!envMap) {
      gl.uniform1i(uniforms.hasEnvMap, 0);
      gl.uniform1f(uniforms.envIntensity, 0);
      gl.uniform1f(uniforms.envRotation, 0);
      return;
    }

    var layout = scenePBRTextureLayoutForFrame(shadowSlots, shadowLightIndices, env);
    var unit = layout && layout.ibl ? layout.ibl.irradiance : null;
    var record = scenePBRLoadTexture(gl, envMap, textureCache);
    var available = Boolean(record && record.texture && !record.failed);
    gl.uniform1i(uniforms.hasEnvMap, available ? 1 : 0);
    var envIntensity = Object.prototype.hasOwnProperty.call(env, "envIntensity")
      ? sceneNumber(env.envIntensity, 1)
      : 1;
    gl.uniform1f(uniforms.envIntensity, Math.max(0, envIntensity));
    gl.uniform1f(uniforms.envRotation, sceneNumber(env.envRotation, 0));
    if (available && unit != null) {
      scenePBRBindTexture(gl, unit, record.texture);
      gl.uniform1i(uniforms.envMap, unit);
    }
  }

  // --- Renderer ---

  function createScenePBRRenderer(gl, canvas) {
    const pbrProgram = createScenePBRProgram(gl);
    if (!pbrProgram) {
      return null;
    }

    const program = pbrProgram.program;
    const attribs = pbrProgram.attributes;
    const uniforms = pbrProgram.uniforms;
    const lineProgram = typeof createSceneWebGLProgram === "function" ? createSceneWebGLProgram(gl) : null;
    const lineResources = lineProgram && typeof createSceneWebGLResources === "function"
      ? createSceneWebGLResources(gl, lineProgram, null)
      : null;

	    // Skinned PBR program — compiled lazily on first skinned object.
	    var skinnedProgram = null;
	    var skinnedProgramFailed = false;
    var customProgramCache = new Map();
    var selenaProgramCache = new Map();

    // Shadow program (depth-only shader for shadow pass).
    const shadowProgram = createSceneShadowProgram(gl);

    // Shadow resources — up to 2 directional light shadow maps.
    // Created lazily on first use, reused across frames.
    var shadowSlots = [null, null];

    // Post-processing pipeline — created lazily when postEffects are present.
    var postProcessor = null;

    // Per-frame shadow state, shared between render() and drawPBRObjectList().
    // Light matrices now live on the per-cascade objects in shadowSlots[s];
    // only the light-index lookup table remains renderer-scoped.
    var shadowLightIndices = [-1, -1];

    // Persistent GPU buffers.
    const positionBuffer = gl.createBuffer();
    const normalBuffer = gl.createBuffer();
    const uvBuffer = gl.createBuffer();
    const tangentBuffer = gl.createBuffer();
    const jointsBuffer = gl.createBuffer();
    const weightsBuffer = gl.createBuffer();
    const selenaPlaceholderTexture = gl.createTexture();
    gl.bindTexture(gl.TEXTURE_2D, selenaPlaceholderTexture);
    gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, 1, 1, 0, gl.RGBA, gl.UNSIGNED_BYTE, new Uint8Array([255, 255, 255, 255]));
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
    gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);

    // Points program — compiled lazily on first points entry.
    var pointsProgram = null;
    // Per-layer authored GLSL program cache: layerID → {program, attrs, uniforms} | {failed:true}
    var pointsAuthoredGLPrograms = new Map();
    var pointsAuthoredGLFailed = new Map();

    // Per-typed-array VBO cache.
    //
    // Root cause this fixes: the prior renderer allocated three shared
    // global VBOs (pointsPositionBuffer/Size/Color) and re-uploaded the
    // cached typed arrays to them on every entry on every frame via
    // gl.bufferData(STREAM_DRAW). For an 8-12 entry galaxy scene with
    // thousands of vertices per layer, that was ~30 bufferData calls and
    // ~700KB of GPU upload per frame of already-cached, known-static data.
    // Chrome's driver optimizes the pattern away; Firefox's does not, so
    // the Scene3D draw loop on FF tripped the script watchdog and fell
    // to 10-19 fps on particle-heavy pages.
    //
    // New approach:
    //   - Static geometry data: WeakMap keyed by the typed-array
    //     reference. Upload once with STATIC_DRAW, reuse the buffer
    //     forever.
    //   - Static point data: persistent VBO slots keyed by stable point id
    //     plus attribute slot. Scene preparation may hand the renderer fresh
    //     point wrapper objects while preserving the same id and source
    //     buffers; keeping the GPU handle at renderer scope avoids treating
    //     that wrapper churn as new geometry. When a live update replaces
    //     positions/sizes/colors, the keyed slot deletes its old GL buffer
    //     before uploading the replacement. This keeps long-lived tabs from
    //     accumulating one stale color buffer per palette tick.
    //   - Dynamic data (compute particle systems): persistent per-entry
    //     buffer with bufferSubData each frame into DYNAMIC_DRAW storage.
    //     Still avoids the bufferData reallocation churn of the old path;
    //     only the data upload is paid, not the GPU memory allocator.
    //
    // All per-entry buffers — static and dynamic, points and meshes —
    // register into pointsEntryBuffers so dispose() can free them on
    // scene unmount without needing a per-subsystem bookkeeping pass.
    const staticMeshArrayVBOs = new WeakMap();
    const pointsEntryBuffers = new Set();
    const staticPointEntries = new Set();
    const activeStaticPointEntries = new Set();
    const staticPointKeyedVBOs = new Map();
    const activeStaticPointKeys = new Set();
    var computeParticleSystems = new Map();
    var lastComputeParticleTimeSeconds = null;
    var lastPreparedScene = null;
    var webglComputeParticleDrawStats = {
      drawEntries: 0,
      drawInstances: 0,
      drawCalls: 0,
      authoredDrawEntries: 0,
      authoredDrawInstances: 0,
      authoredDrawCalls: 0,
    };

    function resetWebGLComputeParticleDrawStats() {
      webglComputeParticleDrawStats.drawEntries = 0;
      webglComputeParticleDrawStats.drawInstances = 0;
      webglComputeParticleDrawStats.drawCalls = 0;
      webglComputeParticleDrawStats.authoredDrawEntries = 0;
      webglComputeParticleDrawStats.authoredDrawInstances = 0;
      webglComputeParticleDrawStats.authoredDrawCalls = 0;
    }

    function publishWebGLComputeParticleDrawStats() {
      var mount = canvas && canvas.parentNode ? canvas.parentNode : null;
      if (!mount || typeof mount.setAttribute !== "function") {
        return;
      }
      mount.__gosxScene3DWebGLStats = Object.assign({}, webglComputeParticleDrawStats);
      mount.setAttribute("data-gosx-scene3d-webgl-compute-particle-draw-entries", String(webglComputeParticleDrawStats.drawEntries));
      mount.setAttribute("data-gosx-scene3d-webgl-compute-particle-draw-instances", String(webglComputeParticleDrawStats.drawInstances));
      mount.setAttribute("data-gosx-scene3d-webgl-compute-particle-draw-calls", String(webglComputeParticleDrawStats.drawCalls));
      mount.setAttribute("data-gosx-scene3d-webgl-compute-particle-authored-draw-entries", String(webglComputeParticleDrawStats.authoredDrawEntries));
      mount.setAttribute("data-gosx-scene3d-webgl-compute-particle-authored-draw-instances", String(webglComputeParticleDrawStats.authoredDrawInstances));
      mount.setAttribute("data-gosx-scene3d-webgl-compute-particle-authored-draw-calls", String(webglComputeParticleDrawStats.authoredDrawCalls));
    }

    function ensureStaticArrayVBO(cache, typedArray) {
      return sceneCachedBuffer(cache, typedArray, function() {
        var buf = gl.createBuffer();
        pointsEntryBuffers.add(buf);
        return buf;
      }, function(buf, data) {
        gl.bindBuffer(gl.ARRAY_BUFFER, buf);
        gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
      });
    }

    function releaseEntryBufferSlot(entry, bufferSlot, keySlot) {
      if (!entry) return;
      var buf = entry[bufferSlot];
      if (buf) {
        gl.deleteBuffer(buf);
        pointsEntryBuffers.delete(buf);
        entry[bufferSlot] = null;
      }
      if (keySlot) {
        entry[keySlot] = null;
      }
    }

    function releaseStaticPointEntryBuffers(entry) {
      if (!entry) return;
      releaseEntryBufferSlot(entry, "_vboPos", "_vboPosSource");
      releaseEntryBufferSlot(entry, "_vboSizes", "_vboSizesSource");
      releaseEntryBufferSlot(entry, "_vboColors", "_vboColorsSource");
      staticPointEntries.delete(entry);
    }

    function staticPointVBOKey(entry, bufferSlot) {
      var id = entry && typeof entry.id === "string" ? entry.id.trim() : "";
      return id ? id + ":" + String(bufferSlot || "") : "";
    }

    function staticPointSourceToken(entry, keySlot, typedArray) {
      if (!entry) return typedArray;
      if (keySlot === "_vboPosSource" && entry.positions) return entry.positions;
      if (keySlot === "_vboSizesSource" && entry.sizes) return entry.sizes;
      if (keySlot === "_vboColorsSource" && entry.colors) return entry.colors;
      return typedArray;
    }

    function releaseStaticPointKeyedBuffer(key, record) {
      if (record && record.buffer) {
        gl.deleteBuffer(record.buffer);
        pointsEntryBuffers.delete(record.buffer);
      }
      if (key) {
        staticPointKeyedVBOs.delete(key);
      }
    }

    function ensureKeyedStaticPointVBO(entry, bufferSlot, keySlot, typedArray) {
      var key = staticPointVBOKey(entry, bufferSlot);
      if (!key) return null;
      activeStaticPointKeys.add(key);
      var source = staticPointSourceToken(entry, keySlot, typedArray);
      var byteLength = typedArray && Number.isFinite(typedArray.byteLength) ? typedArray.byteLength : 0;
      var record = staticPointKeyedVBOs.get(key);
      if (
        !record ||
        !record.buffer ||
        record.source !== source ||
        record.byteLength !== byteLength
      ) {
        releaseStaticPointKeyedBuffer(key, record);
        var buf = gl.createBuffer();
        pointsEntryBuffers.add(buf);
        gl.bindBuffer(gl.ARRAY_BUFFER, buf);
        gl.bufferData(gl.ARRAY_BUFFER, typedArray, gl.STATIC_DRAW);
        record = {
          buffer: buf,
          source: source,
          byteLength: byteLength,
        };
        staticPointKeyedVBOs.set(key, record);
      }
      return record.buffer;
    }

    function ensureStaticPointVBO(entry, bufferSlot, keySlot, typedArray) {
      if (!entry || !typedArray) return null;
      var keyed = ensureKeyedStaticPointVBO(entry, bufferSlot, keySlot, typedArray);
      if (keyed) return keyed;
      staticPointEntries.add(entry);
      activeStaticPointEntries.add(entry);
      if (entry[keySlot] !== typedArray || !entry[bufferSlot]) {
        releaseEntryBufferSlot(entry, bufferSlot, keySlot);
        var buf = gl.createBuffer();
        pointsEntryBuffers.add(buf);
        gl.bindBuffer(gl.ARRAY_BUFFER, buf);
        gl.bufferData(gl.ARRAY_BUFFER, typedArray, gl.STATIC_DRAW);
        entry[bufferSlot] = buf;
        entry[keySlot] = typedArray;
      }
      return entry[bufferSlot];
    }

    function releaseInactiveStaticPointBuffers() {
      for (const entry of Array.from(staticPointEntries)) {
        if (!activeStaticPointEntries.has(entry)) {
          releaseStaticPointEntryBuffers(entry);
        }
      }
      activeStaticPointEntries.clear();
      for (const key of Array.from(staticPointKeyedVBOs.keys())) {
        if (!activeStaticPointKeys.has(key)) {
          releaseStaticPointKeyedBuffer(key, staticPointKeyedVBOs.get(key));
        }
      }
      activeStaticPointKeys.clear();
    }

    // Dynamic path: used by compute particle systems whose backing
    // Float32Array is stable in identity but whose contents change every
    // frame. buildComputePointsEntries attaches a stable per-record entry
    // shell so entry[slot] persists across calls.
    function ensureDynamicPointsVBO(entry, slot, typedArray) {
      return sceneCachedBuffer(entry, typedArray, function() {
        var buf = gl.createBuffer();
        pointsEntryBuffers.add(buf);
        return buf;
      }, function(buf, data, state) {
        gl.bindBuffer(gl.ARRAY_BUFFER, buf);
        if (!state || state.bytesChanged) {
          gl.bufferData(gl.ARRAY_BUFFER, data, gl.DYNAMIC_DRAW);
        } else {
          gl.bufferSubData(gl.ARRAY_BUFFER, 0, data);
        }
      }, { slot: slot, dynamic: true });
    }

	    // Instanced PBR program — compiled lazily on first instanced mesh.
	    var instancedProgram = null;
	    var instancedProgramFailed = false;

    // Geometry cache: maps "kind:w:h:d:r:s" to generated geometry data.
    var instancedGeometryCache = {};

    // Local texture cache for this renderer instance.
    const textureCache = new Map();

    // Persistent shadow pass state — reuses one GL buffer and one scratch
    // Float32Array across all objects and lights, grown as needed.
    var shadowState = { buffer: gl.createBuffer(), scratch: null };

	    // Pre-allocated scratch buffers for view/projection matrices (Fix 8).
	    var scratchViewMatrix = new Float32Array(16);
	    var scratchProjMatrix = new Float32Array(16);
	    var scratchSelenaViewProjection = new Float32Array(16);
	    // Per-frame clock (seconds) fed to selena materials that declare `param time : float`.
	    // Set once per frame before any selena draw; explicit customUniforms.time still overrides.
	    var sceneSelenaFrameTime = 0;
	    var identityModelMatrix = new Float32Array([
	      1, 0, 0, 0,
	      0, 1, 0, 0,
	      0, 0, 1, 0,
	      0, 0, 0, 1,
	    ]);

    // Per-frame camera cache — set once in render(), reused in
    // drawPBRObjectList and drawInstancedMeshes. Pre-allocated so
    // sceneRenderCamera can mutate in place instead of returning a fresh
    // object each frame. Owned exclusively by this renderer — no other
    // code writes to this reference.
    var _frameCam = {
      x: 0, y: 0, z: 0,
      rotationX: 0, rotationY: 0, rotationZ: 0,
      fov: 0, near: 0, far: 0,
    };
    // Per-frame lights+environment content hash. Computed once at the top
    // of render() and reused by every scenePBRUploadLights call (main,
    // skinned, instanced) so the ~13µs hash cost is paid once per frame
    // instead of three times. Each uniforms struct still keeps its own
    // _lastLightsHash stamp so per-program dirty tracking works.
    var _frameLightsHash = 0;

    // Scratch Float32Arrays to avoid per-frame allocation when sizes are stable.
    var scratchPositions = null;
    var scratchNormals = null;
    var scratchUVs = null;
    var scratchTangents = null;

    function ensureScratch(name, length) {
      if (name === "positions") {
        if (!scratchPositions || scratchPositions.length < length) {
          scratchPositions = new Float32Array(length);
        }
        return scratchPositions;
      }
      if (name === "normals") {
        if (!scratchNormals || scratchNormals.length < length) {
          scratchNormals = new Float32Array(length);
        }
        return scratchNormals;
      }
      if (name === "uvs") {
        if (!scratchUVs || scratchUVs.length < length) {
          scratchUVs = new Float32Array(length);
        }
        return scratchUVs;
      }
      if (name === "tangents") {
        if (!scratchTangents || scratchTangents.length < length) {
          scratchTangents = new Float32Array(length);
        }
        return scratchTangents;
      }
      return new Float32Array(length);
    }

    // Convert a slice of a Float64Array source to Float32Array, reusing scratch.
    function sliceToFloat32(source, offset, count, stride, scratchName) {
      const length = count * stride;
      const start = offset * stride;
      const buf = ensureScratch(scratchName, length);
      for (var i = 0; i < length; i++) {
        buf[i] = source && source[start + i] !== undefined ? +source[start + i] : 0;
      }
      return buf.subarray(0, length);
    }

    function uploadCustomUniforms(gl, uniforms, values) {
      const locations = uniforms && uniforms.customUniforms;
      if (!locations || !values || typeof values !== "object") {
        return;
      }
      const names = Object.keys(locations);
      for (var i = 0; i < names.length; i++) {
        const name = names[i];
        const loc = locations[name];
        if (!loc) {
          continue;
        }
        const value = values[name];
        if (typeof value === "number" || typeof value === "boolean") {
          gl.uniform1f(loc, sceneNumber(value, 0));
        } else if ((Array.isArray(value) || (value && typeof value.length === "number")) && value.length >= 4) {
          gl.uniform4f(loc, sceneNumber(value[0], 0), sceneNumber(value[1], 0), sceneNumber(value[2], 0), sceneNumber(value[3], 0));
        } else if ((Array.isArray(value) || (value && typeof value.length === "number")) && value.length === 3) {
          gl.uniform3f(loc, sceneNumber(value[0], 0), sceneNumber(value[1], 0), sceneNumber(value[2], 0));
        } else if ((Array.isArray(value) || (value && typeof value.length === "number")) && value.length === 2) {
          gl.uniform2f(loc, sceneNumber(value[0], 0), sceneNumber(value[1], 0));
        }
      }
    }

    function uploadMaterial(gl, uniforms, material, textureCache) {
      const mat = material || {};
      // Global material cache on the program's uniforms object. Skip the
      // 6 gl.uniform* calls + 5 texture binds when the same material is
      // re-applied consecutively. Unlike the per-draw-loop lastMaterialIndex
      // check that callers already do, this survives program swaps and
      // covers the A→B→A pattern where material A is used, then B, then A
      // again — without this cache the second A upload would re-issue
      // every uniform even though the GL state is already correct.
      //
      // Reference equality is sufficient because materials in the scene
      // bundle are stable objects across frames (the materialLookup Map
      // in createSceneRenderBundle dedupes them by content hash). If a
      // consumer mutates a material in place, they're expected to flip
      // the bundle's materialIndex, which gives a different reference
      // and naturally triggers a re-upload.
      if (uniforms._lastMaterial === material) {
        uploadCustomUniforms(gl, uniforms, mat.customUniforms);
        return;
      }
      uniforms._lastMaterial = material;
      const albedoRGBA = sceneColorRGBA(mat.color, [0.8, 0.8, 0.8, 1]);
      gl.uniform3f(uniforms.albedo, albedoRGBA[0], albedoRGBA[1], albedoRGBA[2]);
      gl.uniform1f(uniforms.roughness, sceneNumber(mat.roughness, 0.5));
      gl.uniform1f(uniforms.metalness, sceneNumber(mat.metalness, 0));
      gl.uniform1f(uniforms.clearcoat, clamp01(sceneNumber(mat.clearcoat, 0)));
      gl.uniform1f(uniforms.sheen, clamp01(sceneNumber(mat.sheen, 0)));
      gl.uniform1f(uniforms.transmission, clamp01(sceneNumber(mat.transmission, 0)));
      gl.uniform1f(uniforms.iridescence, clamp01(sceneNumber(mat.iridescence, 0)));
      gl.uniform1f(uniforms.anisotropy, Math.max(-1, Math.min(1, sceneNumber(mat.anisotropy, 0))));
      gl.uniform1f(uniforms.emissive, sceneNumber(mat.emissive, 0));
      gl.uniform1f(uniforms.opacity, clamp01(sceneNumber(mat.opacity, 1)));
      gl.uniform1i(uniforms.unlit, mat.unlit ? 1 : 0);

      // Bind texture maps. Each map uses a dedicated texture unit.
      var textureMaps = [
        { prop: "texture",      has: "hasAlbedoMap",    sampler: "albedoMap",    unit: 0 },
        { prop: "normalMap",    has: "hasNormalMap",     sampler: "normalMap",    unit: 1 },
        { prop: "roughnessMap", has: "hasRoughnessMap",  sampler: "roughnessMap", unit: 2 },
        { prop: "metalnessMap", has: "hasMetalnessMap",  sampler: "metalnessMap", unit: 3 },
        { prop: "emissiveMap",  has: "hasEmissiveMap",   sampler: "emissiveMap",  unit: 4 },
      ];
      for (var ti = 0; ti < textureMaps.length; ti++) {
        var tm = textureMaps[ti];
        var record = mat[tm.prop] ? scenePBRLoadTexture(gl, mat[tm.prop], textureCache) : null;
        var loaded = Boolean(record && record.texture && record.loaded);
        gl.uniform1i(uniforms[tm.has], loaded ? 1 : 0);
        if (loaded) {
          scenePBRBindTexture(gl, tm.unit, record.texture);
          gl.uniform1i(uniforms[tm.sampler], tm.unit);
        }
      }
      uploadCustomUniforms(gl, uniforms, mat.customUniforms);
    }

    function applyBlendMode(gl, renderPass) {
      if (renderPass === "alpha") {
        gl.enable(gl.BLEND);
        gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
      } else if (renderPass === "additive") {
        gl.enable(gl.BLEND);
        gl.blendFunc(gl.SRC_ALPHA, gl.ONE);
      } else {
        gl.disable(gl.BLEND);
      }
    }

    function applyDepthMode(gl, renderPass) {
      gl.enable(gl.DEPTH_TEST);
      if (renderPass === "opaque") {
        gl.depthMask(true);
        gl.depthFunc(gl.LEQUAL);
      } else {
        gl.depthMask(false);
        gl.depthFunc(gl.LEQUAL);
      }
    }

    // Renderer-scoped scratch arrays for the per-frame draw list so each
    // render() call doesn't allocate three plain arrays + a result object
    // and then throw them all into the GC nursery. Sort in-place on the
    // persistent arrays; reset length to 0 at the top of each build.
    const _drawListOpaque = [];
    const _drawListAlpha = [];
    const _drawListAdditive = [];
    const _drawListResult = { opaque: _drawListOpaque, alpha: _drawListAlpha, additive: _drawListAdditive };

    // Collect objects into render-pass groups and sort translucent objects
    // by depth (back-to-front). Returns a renderer-scoped scratch object —
    // callers MUST NOT retain the reference across another buildPBRDrawList
    // invocation because the underlying arrays are reused in place.
    function buildPBRDrawList(bundle) {
      const objects = Array.isArray(bundle && bundle.meshObjects) ? bundle.meshObjects : [];
      const materials = Array.isArray(bundle.materials) ? bundle.materials : [];
      _drawListOpaque.length = 0;
      _drawListAlpha.length = 0;
      _drawListAdditive.length = 0;

      for (var i = 0; i < objects.length; i++) {
        const obj = objects[i];
        if (!obj || obj.viewCulled) {
          continue;
        }
        if (!Number.isFinite(obj.vertexOffset) || !Number.isFinite(obj.vertexCount) || obj.vertexCount <= 0) {
          continue;
        }
        const mat = materials[obj.materialIndex] || null;
        const pass = scenePBRObjectRenderPass(obj, mat);
        if (pass === "alpha") {
          _drawListAlpha.push(obj);
        } else if (pass === "additive") {
          _drawListAdditive.push(obj);
        } else {
          _drawListOpaque.push(obj);
        }
      }

      // Sort translucent passes back-to-front by depth center.
      _drawListAlpha.sort(scenePBRDepthSort);
      _drawListAdditive.sort(scenePBRDepthSort);

      return _drawListResult;
    }

    function render(bundle, viewport) {
      if (!bundle) {
        return;
      }

      // Opt-in perf instrumentation for the browser bench overlay at
      // /demos/scene3d-bench. The page sets window.__gosx_scene3d_perf
      // before bootstrap runs; when it's truthy we bracket the render
      // body with performance.mark / measure so a PerformanceObserver
      // (installed by the page) can collect wall-clock per-frame durations.
      //
      // Gate is a single truthy check, ~1ns when disabled — production
      // pages don't pay for it. Marks are cleared after each measure to
      // prevent unbounded accumulation of performance entries.
      var perfEnabled = typeof window !== "undefined" && window.__gosx_scene3d_perf === true;
      if (perfEnabled) {
        performance.mark("scene3d-render-start");
      }
      resetWebGLComputeParticleDrawStats();

      // Check that this bundle has PBR-compatible data or points.
      const hasPBRData = Boolean(
        bundle.worldMeshPositions &&
        bundle.worldMeshNormals &&
        Array.isArray(bundle.meshObjects) &&
        bundle.meshObjects.length > 0
      );
      const hasPointsData = (Array.isArray(bundle.points) && bundle.points.length > 0) ||
        (Array.isArray(bundle.computeParticles) && bundle.computeParticles.length > 0);
      const hasInstancedData = Array.isArray(bundle.instancedMeshes) && bundle.instancedMeshes.length > 0;
      const hasLineData = bundle.worldVertexCount > 0 ||
        (Array.isArray(bundle.surfaces) && bundle.surfaces.some(function(surface) {
          return surface && !(surface.sourceKind === "html" && !surface.textureReady);
        }));
      if (!hasPBRData && !hasPointsData && !hasInstancedData && !hasLineData) {
        return;
      }
      const preparedScene = typeof prepareScene === "function"
        ? prepareScene(bundle, bundle.camera, viewport, lastPreparedScene, {
          mount: canvas && canvas.parentNode || null,
          sentinels: canvas && canvas.parentNode && canvas.parentNode.__gosxScene3DSentinels || null,
        })
        : null;
      if (preparedScene) {
        lastPreparedScene = preparedScene;
        bundle = preparedScene.ir || bundle;
        if (canvas && canvas.parentNode) {
          canvas.parentNode.__gosxScene3DCSSDynamic = Boolean(preparedScene.cssDynamic);
        }
      }

      // --- Camera Matrices ---
      // Hoisted above the shadow pass because CSM cascade-frustum fitting
      // needs the inverse view matrix to reconstruct world-space frustum
      // corners per cascade.
      sceneRenderCamera(bundle.camera, _frameCam);
      const cam = _frameCam;
      const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
      const viewMatrix = scenePBRViewMatrix(cam, scratchViewMatrix);
      const projMatrix = scenePBRProjectionMatrixForCamera(cam, aspect, scratchProjMatrix);
      sceneMat4MultiplyInto(scratchSelenaViewProjection, projMatrix, viewMatrix);
      sceneSelenaFrameTime = performance.now() / 1000; // feed auto time uniform before any selena mesh draw

      // --- Shadow Pass ---
      // Identify shadow-casting directional lights (max 2) and render per-
      // cascade depth maps. Reset per-frame shadow state (closure-scoped for
      // drawPBRObjectList access).
      shadowLightIndices[0] = -1; shadowLightIndices[1] = -1;
      var activeShadowCount = 0;

      if (shadowProgram) {
        var lightArray = Array.isArray(bundle.lights) ? bundle.lights : [];
        var sceneBounds = null;
        var shadowMaxPixels = (typeof bundle.shadowMaxPixels === "number") ? bundle.shadowMaxPixels : 0;

        for (var li = 0; li < lightArray.length && activeShadowCount < 2; li++) {
          var light = lightArray[li];
          if (!light || !light.castShadow) continue;
          var kind = typeof light.kind === "string" ? light.kind.toLowerCase() : "";
          if (kind !== "directional") continue;

          // Compute scene bounds lazily (only if we have shadow casters).
          if (!sceneBounds) {
            sceneBounds = sceneShadowComputeBounds(bundle);
          }

          var slot = activeShadowCount;
          var numCascades = Math.max(1, Math.min(4, (light.shadowCascades | 0) || 1));
          var shadowSize = sceneNumber(light.shadowSize, 1024);
          shadowSize = Math.max(256, Math.min(4096, shadowSize));
          shadowSize = resolveShadowSize(shadowSize, shadowMaxPixels);

          // Create or resize shadow resources for this slot (per cascade).
          if (!shadowSlots[slot] ||
              shadowSlots[slot].size !== shadowSize ||
              shadowSlots[slot].numCascades !== numCascades) {
            disposeShadowSlot(gl, shadowSlots[slot]);
            shadowSlots[slot] = createSceneShadowSlot(gl, shadowSize, numCascades);
          }

          shadowLightIndices[slot] = li;

          // Fit a per-cascade light-space matrix. When numCascades > 1 the
          // camera's view frustum is split into N sub-frusta (PSSM); each
          // cascade's corners are fit to a tight ortho box in light-space.
          // numCascades == 1 matches the legacy full-scene ortho fit.
          computeShadowSlotCascadeMatrices(light, shadowSlots[slot], sceneBounds,
            viewMatrix, cam.fov, aspect, cam.near, cam.far);

          // Render one depth pass per cascade.
          for (var ci = 0; ci < shadowSlots[slot].numCascades; ci++) {
            var cascade = shadowSlots[slot].cascades[ci];
            renderSceneShadowPass(gl, shadowProgram, cascade, cascade.lightMatrix, bundle, shadowState);
          }
          activeShadowCount++;
        }
      }

      // --- Main Render Pass ---

      // Determine if post-processing is active for this frame.
      var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
      var postFXMaxPixels = (typeof bundle.postFXMaxPixels === "number") ? bundle.postFXMaxPixels : 0;
      var usePostProcessing = postEffects.length > 0;

      // renderW/renderH reflect the actual render target. When postfx is
      // active with a cap, these may be smaller than canvas dims. All
      // viewport-dependent shader uniforms (u_viewportHeight, etc.) and
      // the main gl.viewport call must use render dims, not canvas dims.
      var renderW = canvas.width;
      var renderH = canvas.height;

      if (usePostProcessing) {
        if (!postProcessor) {
          postProcessor = createScenePostProcessor(gl);
        }
        var scaled = postProcessor.begin(canvas.width, canvas.height, postFXMaxPixels);
        renderW = scaled.width;
        renderH = scaled.height;
      }

      // Resize viewport to the render target (scaled when postfx caps are active).
      gl.viewport(0, 0, renderW, renderH);

      // Clear — "transparent" clears to fully transparent for alpha compositing.
      var bgStr = typeof bundle.background === "string" ? bundle.background.trim().toLowerCase() : "";
      const bg = bgStr === "transparent" ? [0, 0, 0, 0] : sceneColorRGBA(bundle.background, [0.03, 0.08, 0.12, 1]);
      gl.clearColor(bg[0], bg[1], bg[2], bg[3]);
      gl.clearDepth(1);
      gl.clear(gl.COLOR_BUFFER_BIT | gl.DEPTH_BUFFER_BIT);

      // Camera matrices were already computed above the shadow pass so CSM
      // could build per-cascade frusta; `cam`, `viewMatrix`, `projMatrix`,
      // and `aspect` are still in scope here.

      // Compute the lights+environment content hash ONCE per frame and
      // reuse it across every scenePBRUploadLights call (main program,
      // skinned program on switch, instanced program). The hash is ~13µs
      // per call on typical fixtures; computing it 3× per frame was
      // burning ~26µs of overhead relative to a single-shot hoist.
      // Per-program dirty tracking is still preserved via the uniforms
      // stamp — the shared hash just tells each call site whether the
      // content changed since the last time THAT program was uploaded.
      //
      // Lives outside the `if (hasPBRData)` block below because
      // drawInstancedMeshes also uses _frameLightsHash and must see a
      // correct value even on scenes that have no PBR mesh data.
      _frameLightsHash = scenePBRLightsHash(bundle.lights, bundle.environment);

      // Only activate PBR mesh program if there are mesh objects to draw.
      if (hasPBRData) {
      gl.useProgram(program);
      gl.uniformMatrix4fv(uniforms.viewMatrix, false, viewMatrix);
      gl.uniformMatrix4fv(uniforms.projectionMatrix, false, projMatrix);
      gl.uniform3f(uniforms.cameraPosition, cam.x, cam.y, -cam.z);

      // Upload exposure and tone mapping mode (disabled when post-processing handles it).
      scenePBRUploadExposure(gl, uniforms, bundle.environment, usePostProcessing);

      // Upload lights and environment once per frame.
      scenePBRUploadLights(gl, uniforms, bundle.lights, bundle.environment, _frameLightsHash);
      scenePBRUploadEnvironmentMap(gl, uniforms, bundle.environment, textureCache, shadowSlots, shadowLightIndices);

      // Upload shadow map uniforms through the shared Scene3D texture-unit allocator.
      scenePBRUploadShadowUniforms(gl, uniforms, shadowSlots, shadowLightIndices, bundle.lights, bundle.environment);

      // Build draw list grouped by render pass.
      const drawList = preparedScene && preparedScene.pbrPasses
        ? preparedScene.pbrPasses
        : buildPBRDrawList(bundle);
      const materials = Array.isArray(bundle.materials) ? bundle.materials : [];

      // Draw opaque pass.
      applyBlendMode(gl, "opaque");
      applyDepthMode(gl, "opaque");
      drawPBRObjectList(gl, drawList.opaque, bundle, materials);

      // Draw alpha pass.
      if (drawList.alpha.length > 0) {
        applyBlendMode(gl, "alpha");
        applyDepthMode(gl, "alpha");
        drawPBRObjectList(gl, drawList.alpha, bundle, materials);
      }

      // Draw additive pass.
      if (drawList.additive.length > 0) {
        applyBlendMode(gl, "additive");
        applyDepthMode(gl, "additive");
        drawPBRObjectList(gl, drawList.additive, bundle, materials);
      }

      } // end if (hasPBRData)

      if (lineResources && bundle.worldVertexCount > 0) {
        renderSceneWebGLWorldBundle(gl, bundle, canvas, lineResources);
      }

      // Draw instanced meshes (after regular meshes, before points).
      drawInstancedMeshes(gl, bundle, viewMatrix, projMatrix);

      // Draw points entries (after meshes, before post-processing).
      var frameTimeSeconds = performance.now() / 1000;
      activeStaticPointEntries.clear();
      activeStaticPointKeys.clear();
      drawPointsEntries(gl, Array.isArray(bundle.points) ? bundle.points : [], bundle.environment, viewMatrix, projMatrix, frameTimeSeconds, renderH);
      drawPointsEntries(gl, buildComputePointsEntries(bundle.computeParticles, frameTimeSeconds), bundle.environment, viewMatrix, projMatrix, frameTimeSeconds, renderH);
      releaseInactiveStaticPointBuffers();
      publishWebGLComputeParticleDrawStats();

      // Restore state.
      gl.depthMask(true);
      gl.disable(gl.BLEND);

      // Apply post-processing chain if active.
      if (usePostProcessing && postProcessor) {
        postProcessor.apply(postEffects, renderW, renderH, canvas.width, canvas.height, bundle.camera);
        // Re-activate the PBR program for the next frame since post-processing
        // switches to its own shader programs.
        gl.useProgram(program);
      }

      if (perfEnabled) {
        performance.mark("scene3d-render-end");
        performance.measure("scene3d-render", "scene3d-render-start", "scene3d-render-end");
        // Clean up marks so they don't accumulate across frames. Measures
        // are consumed by the page's PerformanceObserver; leaving a short
        // tail of unconsumed measures is fine — the browser caps the buffer.
        performance.clearMarks("scene3d-render-start");
        performance.clearMarks("scene3d-render-end");
      }
    }

    // Check if an object has skinning data attached.
    function objectIsSkinned(obj) {
      return Boolean(
        obj && obj.skin &&
        obj.vertices && obj.vertices.joints && obj.vertices.weights
      );
    }

	    // Ensure the skinned PBR program is compiled (lazy init).
	    function ensureSkinnedProgram() {
	      if (skinnedProgram) return skinnedProgram;
	      if (skinnedProgramFailed) return null;
	      skinnedProgram = createScenePBRSkinnedProgram(gl);
	      if (!skinnedProgram) {
	        skinnedProgramFailed = true;
	        console.warn("[gosx] Skinned PBR shader compilation failed; skinned objects will use static path.");
	      }
	      return skinnedProgram;
	    }

    function ensureCustomProgram(material) {
      if (!scenePBRHasCustomHooks(material)) {
        return null;
      }
      const key = material && material.key ? material.key : sceneMaterialProfileKey(material);
      const cached = customProgramCache.get(key);
      if (cached) {
        return cached.failed ? null : cached.program;
      }
      const customProgram = createScenePBRCustomProgram(gl, material);
      customProgramCache.set(key, customProgram ? { program: customProgram } : { failed: true });
      if (!customProgram) {
        console.warn("[gosx] CustomMaterial shader compilation failed; object will use the standard PBR shader.");
      }
      return customProgram;
    }

    function ensureSelenaProgram(material) {
      if (!sceneSelenaIsMaterial(material)) {
        return null;
      }
      const key = material && material.key ? material.key : sceneMaterialProfileKey(material);
      const cached = selenaProgramCache.get(key);
      if (cached) {
        return cached.failed ? null : cached.program;
      }
      const selenaProgram = createSceneSelenaProgram(gl, material);
      selenaProgramCache.set(key, selenaProgram ? { program: selenaProgram } : { failed: true });
      if (!selenaProgram) {
        console.warn("[gosx] Selena shader compilation failed; object will use the standard PBR shader.");
      }
      return selenaProgram;
    }

    function webGLSelenaObjectModelMatrix(obj) {
      if (obj && obj.directVertices === true) {
        return obj.modelMatrix || identityModelMatrix;
      }
      return identityModelMatrix;
    }

    function selenaUniformValue(material, layout, field, owner) {
      var name = field && field.name;
      if (name === "mvp") return scratchSelenaViewProjection;
      if (name === "modelMatrix") return webGLSelenaObjectModelMatrix(owner);
      if (name === "normalMatrix") return [1, 0, 0, 0, 1, 0, 0, 0, 1];
      // time is a reserved auto-uniform (like mvp/normalMatrix): forced BEFORE
      // customUniforms so a declared `param time` — whose compiled default ships
      // in customUniforms via selenaDefaultUniforms — can't shadow the clock.
      if (name === "time") return sceneSelenaFrameTime;
      var values = material && material.customUniforms;
      if (values && typeof values === "object" && Object.prototype.hasOwnProperty.call(values, name)) {
        return values[name];
      }
      var def = sceneSelenaUniformDefault(layout, name);
      if (def !== undefined) return def;
      var count = sceneSelenaFloatCount(field && field.type);
      if (count === 16) return identityModelMatrix;
      if (count === 9) return [1, 0, 0, 0, 1, 0, 0, 0, 1];
      return 0;
    }

    function selenaScalar(value) {
      if (Array.isArray(value) || (value && typeof value.length === "number")) {
        return sceneNumber(value[0], 0);
      }
      return sceneNumber(value, 0);
    }

    function uploadSelenaUniforms(gl, info, material, owner) {
      var layout = info && info.layout;
      var fields = layout && layout.uniformBlock && Array.isArray(layout.uniformBlock.fields)
        ? layout.uniformBlock.fields
        : [];
      for (var i = 0; i < fields.length; i++) {
        var field = fields[i] || {};
        var loc = info.uniforms && info.uniforms[field.name];
        if (!loc) continue;
        var value = selenaUniformValue(material, layout, field, owner);
        switch (String(field.type || "")) {
        case "mat4":
          gl.uniformMatrix4fv(loc, false, value);
          break;
        case "mat3":
          gl.uniformMatrix3fv(loc, false, value);
          break;
        case "vec4":
          gl.uniform4f(loc, sceneNumber(value && value[0], 0), sceneNumber(value && value[1], 0), sceneNumber(value && value[2], 0), sceneNumber(value && value[3], 0));
          break;
        case "vec3":
          gl.uniform3f(loc, sceneNumber(value && value[0], 0), sceneNumber(value && value[1], 0), sceneNumber(value && value[2], 0));
          break;
        case "vec2":
          gl.uniform2f(loc, sceneNumber(value && value[0], 0), sceneNumber(value && value[1], 0));
          break;
        default:
          gl.uniform1f(loc, selenaScalar(value));
          break;
        }
      }
    }

    function bindSelenaTextures(gl, info, material) {
      var textures = sceneSelenaTextureDescriptors(info && info.layout);
      for (var i = 0; i < textures.length; i++) {
        var tex = textures[i] || {};
        var glBinding = tex.gl || {};
        var loc = gl.getUniformLocation(info.program, glBinding.uniform || tex.name);
        if (!loc) continue;
        var unit = Math.max(0, Math.floor(sceneNumber(glBinding.unit, i)));
        var url = sceneSelenaTextureURL(material, tex, i);
        var record = url ? scenePBRLoadTexture(gl, url, textureCache) : null;
        scenePBRBindTexture(gl, unit, record && record.texture ? record.texture : selenaPlaceholderTexture);
        gl.uniform1i(loc, unit);
      }
    }

    function bindSelenaMeshAttribute(gl, info, name, obj, bundle, offset, count, directVertices) {
      var attr = info && info.attributes && info.attributes[name];
      if (!attr || attr.loc < 0) return;
      var directKey = name === "position" ? "positions" : name === "normal" ? "normals" : name === "uv" ? "uvs" : "";
      var direct = directVertices ? scenePBRDirectAttribute(obj.vertices, directKey, count, attr.size) : null;
      if (bindScenePBRDirectAttribute(obj.vertices, directKey, attr.loc, attr.size, direct)) {
        return;
      }
      if (name === "position") {
        var positions = sliceToFloat32(bundle.worldMeshPositions, offset, count, 3, "positions");
        gl.bindBuffer(gl.ARRAY_BUFFER, positionBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, positions, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(attr.loc);
        gl.vertexAttribPointer(attr.loc, 3, gl.FLOAT, false, 0, 0);
        return;
      }
      if (name === "normal") {
        var normals = sliceToFloat32(bundle.worldMeshNormals, offset, count, 3, "normals");
        gl.bindBuffer(gl.ARRAY_BUFFER, normalBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, normals, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(attr.loc);
        gl.vertexAttribPointer(attr.loc, 3, gl.FLOAT, false, 0, 0);
        return;
      }
      if (name === "uv" && bundle.worldMeshUVs && !directVertices) {
        var uvs = sliceToFloat32(bundle.worldMeshUVs, offset, count, 2, "uvs");
        gl.bindBuffer(gl.ARRAY_BUFFER, uvBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, uvs, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(attr.loc);
        gl.vertexAttribPointer(attr.loc, 2, gl.FLOAT, false, 0, 0);
        return;
      }
      gl.disableVertexAttribArray(attr.loc);
      if (name === "uv") gl.vertexAttrib2f(attr.loc, 0, 0);
      else gl.vertexAttrib3f(attr.loc, 0, 0, name === "normal" ? 1 : 0);
    }

	    function scenePBRDirectAttribute(vertices, key, count, tupleSize) {
	      const data = vertices && vertices[key];
	      const required = Math.max(0, Math.floor(sceneNumber(count, 0))) * tupleSize;
	      if (!(data instanceof Float32Array) || data.length < required) {
	        return null;
	      }
	      if (data.length === required) {
	        return data;
	      }
	      let views = vertices._pbrAttributeViews;
	      if (!views) {
	        views = Object.create(null);
	        vertices._pbrAttributeViews = views;
	      }
	      let record = views[key];
	      if (!record || record.source !== data || record.length !== required) {
	        record = {
	          source: data,
	          length: required,
	          view: data.subarray(0, required),
	        };
	        views[key] = record;
	      }
	      return record.view;
	    }

    function bindScenePBRDirectAttribute(vertices, key, attrib, size, data) {
      if (!vertices || !Number.isFinite(attrib) || attrib < 0 || !(data instanceof Float32Array)) {
        return false;
      }
      let buffers = vertices._pbrAttributeBuffers;
      if (!buffers) {
        buffers = Object.create(null);
        vertices._pbrAttributeBuffers = buffers;
      }
	      let record = buffers[key];
	      if (!record || record.gl !== gl || record.data !== data) {
	        if (record && record.buffer && record.gl === gl) {
	          gl.deleteBuffer(record.buffer);
	          pointsEntryBuffers.delete(record.buffer);
	        }
	        record = {
	          gl,
	          data,
	          buffer: gl.createBuffer(),
	        };
	        pointsEntryBuffers.add(record.buffer);
        buffers[key] = record;
        gl.bindBuffer(gl.ARRAY_BUFFER, record.buffer);
        gl.bufferData(gl.ARRAY_BUFFER, data, gl.STATIC_DRAW);
      } else {
        gl.bindBuffer(gl.ARRAY_BUFFER, record.buffer);
      }
      gl.enableVertexAttribArray(attrib);
      gl.vertexAttribPointer(attrib, size, gl.FLOAT, false, 0, 0);
      return true;
    }

    function drawPBRObjectList(gl, objectList, bundle, materials) {
      var lastMaterialIndex = -1;
      // Track which program is currently bound so we can switch between
      // the static PBR program and the skinned variant per object.
      var currentProgram = program;       // the static PBR gl program
      var currentAttribs = attribs;
      var currentUniforms = uniforms;

      function uploadFrameUniformsForProgram(targetUniforms) {
        gl.uniformMatrix4fv(targetUniforms.viewMatrix, false, scratchViewMatrix);
        gl.uniformMatrix4fv(targetUniforms.projectionMatrix, false, scratchProjMatrix);
        gl.uniform3f(targetUniforms.cameraPosition, _frameCam.x, _frameCam.y, -_frameCam.z);

        var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
        scenePBRUploadExposure(gl, targetUniforms, bundle.environment, postEffects.length > 0);

        scenePBRUploadLights(gl, targetUniforms, bundle.lights, bundle.environment, _frameLightsHash);
        scenePBRUploadEnvironmentMap(gl, targetUniforms, bundle.environment, textureCache, shadowSlots, shadowLightIndices);
        scenePBRUploadShadowUniforms(gl, targetUniforms, shadowSlots, shadowLightIndices, bundle.lights, bundle.environment);
      }

      for (var i = 0; i < objectList.length; i++) {
        const obj = objectList[i];
        const matIndex = sceneNumber(obj.materialIndex, 0);
        const mat = materials[matIndex] || null;
        var isSkinned = objectIsSkinned(obj);
        var selenaProgram = !isSkinned ? ensureSelenaProgram(mat) : null;
        if (selenaProgram) {
          if (currentProgram !== selenaProgram.program) {
            gl.useProgram(selenaProgram.program);
            currentProgram = selenaProgram.program;
            currentAttribs = selenaProgram.attributes;
            currentUniforms = selenaProgram.uniforms;
            lastMaterialIndex = -1;
          }

          var selenaDepthWriteOverride = obj.depthWrite !== undefined && obj.depthWrite !== null;
          if (selenaDepthWriteOverride) {
            gl.depthMask(obj.depthWrite !== false);
          }

          uploadSelenaUniforms(gl, selenaProgram, mat, obj);
          bindSelenaTextures(gl, selenaProgram, mat);

          const selenaOffset = obj.vertexOffset;
          const selenaCount = obj.vertexCount;
          const selenaDirectVertices = Boolean(obj.directVertices && obj.vertices);
          bindSelenaMeshAttribute(gl, selenaProgram, "position", obj, bundle, selenaOffset, selenaCount, selenaDirectVertices);
          bindSelenaMeshAttribute(gl, selenaProgram, "normal", obj, bundle, selenaOffset, selenaCount, selenaDirectVertices);
          bindSelenaMeshAttribute(gl, selenaProgram, "uv", obj, bundle, selenaOffset, selenaCount, selenaDirectVertices);
          gl.drawArrays(gl.TRIANGLES, 0, selenaCount);

          if (selenaDepthWriteOverride) {
            var selenaPass = scenePBRObjectRenderPass(obj, mat);
            gl.depthMask(selenaPass === "opaque");
          }
          continue;
        }

        var customProgram = !isSkinned ? ensureCustomProgram(mat) : null;

        // Switch to skinned program if this object has skin data.
        if (customProgram) {
          if (currentProgram !== customProgram.program) {
            gl.useProgram(customProgram.program);
            currentProgram = customProgram.program;
            currentAttribs = customProgram.attributes;
            currentUniforms = customProgram.uniforms;
            uploadFrameUniformsForProgram(currentUniforms);
            lastMaterialIndex = -1;
          }
        } else if (isSkinned) {
          var sp = ensureSkinnedProgram();
          if (sp && currentProgram !== sp.program) {
            gl.useProgram(sp.program);
            currentProgram = sp.program;
            currentAttribs = sp.attributes;
            currentUniforms = sp.uniforms;
            uploadFrameUniformsForProgram(currentUniforms);
            lastMaterialIndex = -1;
          }
          // Fall back to static if skinned program failed to compile.
          if (!sp) isSkinned = false;
        } else if (currentProgram !== program) {
          // Switch back to static PBR program.
          gl.useProgram(program);
          currentProgram = program;
          currentAttribs = attribs;
          currentUniforms = uniforms;
          lastMaterialIndex = -1;
        }

        // Upload material uniforms only when the material changes.
        if (matIndex !== lastMaterialIndex) {
          uploadMaterial(gl, currentUniforms, mat, textureCache);
          lastMaterialIndex = matIndex;
        }

        // Per-object shadow receive control.
        gl.uniform1i(currentUniforms.receiveShadow, obj.receiveShadow ? 1 : 0);

        // Per-object depth write control.
        var objDepthWriteOverride = obj.depthWrite !== undefined && obj.depthWrite !== null;
        if (objDepthWriteOverride) {
          gl.depthMask(obj.depthWrite !== false);
        }

	        // Skinning: upload joint matrices and enable skin flag.
	        if (isSkinned) {
	          gl.uniform1i(currentUniforms.hasSkin, 1);
	          if (currentUniforms.modelMatrix) {
	            gl.uniformMatrix4fv(currentUniforms.modelMatrix, false, obj.modelMatrix || identityModelMatrix);
	          }

          var jointMatrices = obj.skin.jointMatrices;
          if (jointMatrices) {
            var jointCount = Math.min(Math.floor(jointMatrices.length / 16), 64);
            if (jointCount > 0 && currentUniforms.jointMatrices) {
              var jointUpload = jointMatrices;
              var requiredJointFloats = jointCount * 16;
              if (jointMatrices.length !== requiredJointFloats) {
                var jointViews = obj.skin._jointMatrixUploadViews;
                if (!jointViews) {
                  jointViews = Object.create(null);
                  obj.skin._jointMatrixUploadViews = jointViews;
                }
                var jointView = jointViews[requiredJointFloats];
                if (!jointView || jointView.source !== jointMatrices) {
                  jointView = {
                    source: jointMatrices,
                    view: jointMatrices.subarray(0, requiredJointFloats),
                  };
                  jointViews[requiredJointFloats] = jointView;
                }
                jointUpload = jointView.view;
              }
              gl.uniformMatrix4fv(currentUniforms.jointMatrices, false, jointUpload);
            }
          }
        } else if (currentUniforms.hasSkin) {
          gl.uniform1i(currentUniforms.hasSkin, 0);
        }

        // Upload vertex data for this object.
        const offset = obj.vertexOffset;
        const count = obj.vertexCount;
        const directVertices = Boolean(obj.directVertices && obj.vertices);
        const directPositions = directVertices ? scenePBRDirectAttribute(obj.vertices, "positions", count, 3) : null;
        const directNormals = directVertices ? scenePBRDirectAttribute(obj.vertices, "normals", count, 3) : null;
        const directUVs = directVertices ? scenePBRDirectAttribute(obj.vertices, "uvs", count, 2) : null;
        const directTangents = directVertices ? scenePBRDirectAttribute(obj.vertices, "tangents", count, 4) : null;

        // Positions (vec3).
        if (!bindScenePBRDirectAttribute(obj.vertices, "positions", currentAttribs.position, 3, directPositions)) {
          const positions = sliceToFloat32(bundle.worldMeshPositions, offset, count, 3, "positions");
          gl.bindBuffer(gl.ARRAY_BUFFER, positionBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, positions, gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.position);
          gl.vertexAttribPointer(currentAttribs.position, 3, gl.FLOAT, false, 0, 0);
        }

        // Normals (vec3).
        if (!bindScenePBRDirectAttribute(obj.vertices, "normals", currentAttribs.normal, 3, directNormals)) {
          const normals = sliceToFloat32(bundle.worldMeshNormals, offset, count, 3, "normals");
          gl.bindBuffer(gl.ARRAY_BUFFER, normalBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, normals, gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.normal);
          gl.vertexAttribPointer(currentAttribs.normal, 3, gl.FLOAT, false, 0, 0);
        }

        // UVs (vec2).
        if (bindScenePBRDirectAttribute(obj.vertices, "uvs", currentAttribs.uv, 2, directUVs)) {
          // Cached direct attribute bound.
        } else if (bundle.worldMeshUVs && !directVertices) {
          const uvs = sliceToFloat32(bundle.worldMeshUVs, offset, count, 2, "uvs");
          gl.bindBuffer(gl.ARRAY_BUFFER, uvBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, uvs, gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.uv);
          gl.vertexAttribPointer(currentAttribs.uv, 2, gl.FLOAT, false, 0, 0);
        } else if (currentAttribs.uv >= 0) {
          gl.disableVertexAttribArray(currentAttribs.uv);
          gl.vertexAttrib2f(currentAttribs.uv, 0, 0);
        }

        // Tangents (vec4).
        if (bindScenePBRDirectAttribute(obj.vertices, "tangents", currentAttribs.tangent, 4, directTangents)) {
          // Cached direct attribute bound.
        } else if (bundle.worldMeshTangents && !directVertices) {
          const tangents = sliceToFloat32(bundle.worldMeshTangents, offset, count, 4, "tangents");
          gl.bindBuffer(gl.ARRAY_BUFFER, tangentBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, tangents, gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.tangent);
          gl.vertexAttribPointer(currentAttribs.tangent, 4, gl.FLOAT, false, 0, 0);
        } else if (currentAttribs.tangent >= 0) {
          gl.disableVertexAttribArray(currentAttribs.tangent);
          gl.vertexAttrib4f(currentAttribs.tangent, 1, 0, 0, 1);
        }

        // Joints and weights (skinned meshes only).
        if (isSkinned && currentAttribs.joints >= 0 && currentAttribs.weights >= 0) {
          var joints = obj.vertices.joints;
          var weights = obj.vertices.weights;

          const directJoints = directVertices ? scenePBRDirectAttribute(obj.vertices, "joints", count, 4) : null;
          const directWeights = directVertices ? scenePBRDirectAttribute(obj.vertices, "weights", count, 4) : null;
          if (!bindScenePBRDirectAttribute(obj.vertices, "joints", currentAttribs.joints, 4, directJoints)) {
            gl.bindBuffer(gl.ARRAY_BUFFER, jointsBuffer);
            gl.bufferData(gl.ARRAY_BUFFER, joints instanceof Float32Array ? joints : new Float32Array(joints), gl.DYNAMIC_DRAW);
            gl.enableVertexAttribArray(currentAttribs.joints);
            gl.vertexAttribPointer(currentAttribs.joints, 4, gl.FLOAT, false, 0, 0);
          }

          if (!bindScenePBRDirectAttribute(obj.vertices, "weights", currentAttribs.weights, 4, directWeights)) {
            gl.bindBuffer(gl.ARRAY_BUFFER, weightsBuffer);
            gl.bufferData(gl.ARRAY_BUFFER, weights instanceof Float32Array ? weights : new Float32Array(weights), gl.DYNAMIC_DRAW);
            gl.enableVertexAttribArray(currentAttribs.weights);
            gl.vertexAttribPointer(currentAttribs.weights, 4, gl.FLOAT, false, 0, 0);
          }
        } else if (currentAttribs.joints >= 0) {
          gl.disableVertexAttribArray(currentAttribs.joints);
          gl.vertexAttrib4f(currentAttribs.joints, 0, 0, 0, 0);
          gl.disableVertexAttribArray(currentAttribs.weights);
          gl.vertexAttrib4f(currentAttribs.weights, 0, 0, 0, 0);
        }

        gl.drawArrays(gl.TRIANGLES, 0, count);

        // Restore depth mask if overridden by per-object control.
        if (objDepthWriteOverride) {
          // Restore to pass default: opaque = true, others = false.
          var mat2 = materials[sceneNumber(obj.materialIndex, 0)] || null;
          var pass2 = scenePBRObjectRenderPass(obj, mat2);
          gl.depthMask(pass2 === "opaque");
        }
      }

      // Ensure we leave the static PBR program active for subsequent passes.
      if (currentProgram !== program) {
        gl.useProgram(program);
      }
    }

    // Ensure the points program is compiled (lazy init).
    function ensurePointsProgram() {
      if (pointsProgram) return pointsProgram;
      pointsProgram = createScenePointsProgram(gl);
      if (!pointsProgram) {
        console.warn("[gosx] Points shader compilation failed; points will not render.");
      }
      return pointsProgram;
    }

    // ensurePointsAuthoredGLProgram: compile+link a per-layer GLSL program from
    // entry.customVertex/customFragment (synchronous API — check compile/link status).
    // Returns the program record or null (fallback to builtin with one console.warn).
    function ensurePointsAuthoredGLProgram(entry, layerID) {
      var cached = pointsAuthoredGLPrograms.get(layerID);
      if (cached) return cached.failed ? null : cached;
      var vertSrc = typeof entry.customVertex === "string" ? entry.customVertex.trim() : "";
      var fragSrc = typeof entry.customFragment === "string" ? entry.customFragment.trim() : "";
      if (!vertSrc || !fragSrc) return null;
      var vs = scenePBRCompileShader(gl, gl.VERTEX_SHADER, vertSrc);
      if (!vs) {
        if (!pointsAuthoredGLFailed.get(layerID)) {
          pointsAuthoredGLFailed.set(layerID, true);
          console.warn("[gosx] Points authored vertex shader failed for layer '" + layerID + "'; falling back to builtin.");
        }
        pointsAuthoredGLPrograms.set(layerID, { failed: true });
        return null;
      }
      var fs = scenePBRCompileShader(gl, gl.FRAGMENT_SHADER, fragSrc);
      if (!fs) {
        gl.deleteShader(vs);
        if (!pointsAuthoredGLFailed.get(layerID)) {
          pointsAuthoredGLFailed.set(layerID, true);
          console.warn("[gosx] Points authored fragment shader failed for layer '" + layerID + "'; falling back to builtin.");
        }
        pointsAuthoredGLPrograms.set(layerID, { failed: true });
        return null;
      }
      var prog = scenePBRLinkProgram(gl, vs, fs, "Points authored '" + layerID + "'");
      if (!prog) {
        if (!pointsAuthoredGLFailed.get(layerID)) {
          pointsAuthoredGLFailed.set(layerID, true);
          console.warn("[gosx] Points authored program link failed for layer '" + layerID + "'; falling back to builtin.");
        }
        pointsAuthoredGLPrograms.set(layerID, { failed: true });
        return null;
      }
      // Cache attribute and uniform locations — same contract as builtin.
      var attrs = {
        position: gl.getAttribLocation(prog, "a_position"),
        size: gl.getAttribLocation(prog, "a_size"),
        color: gl.getAttribLocation(prog, "a_color"),
      };
      var uniforms = {
        viewMatrix: gl.getUniformLocation(prog, "u_viewMatrix"),
        projectionMatrix: gl.getUniformLocation(prog, "u_projectionMatrix"),
        modelMatrix: gl.getUniformLocation(prog, "u_modelMatrix"),
        defaultSize: gl.getUniformLocation(prog, "u_defaultSize"),
        defaultColor: gl.getUniformLocation(prog, "u_defaultColor"),
        hasPerVertexColor: gl.getUniformLocation(prog, "u_hasPerVertexColor"),
        hasPerVertexSize: gl.getUniformLocation(prog, "u_hasPerVertexSize"),
        sizeAttenuation: gl.getUniformLocation(prog, "u_sizeAttenuation"),
        pointStyle: gl.getUniformLocation(prog, "u_pointStyle"),
        viewportHeight: gl.getUniformLocation(prog, "u_viewportHeight"),
        minPixelSize: gl.getUniformLocation(prog, "u_minPixelSize"),
        maxPixelSize: gl.getUniformLocation(prog, "u_maxPixelSize"),
        opacity: gl.getUniformLocation(prog, "u_opacity"),
        hasFog: gl.getUniformLocation(prog, "u_hasFog"),
        fogDensity: gl.getUniformLocation(prog, "u_fogDensity"),
        fogColor: gl.getUniformLocation(prog, "u_fogColor"),
      };
      // Upload author-defined uniforms (customUniforms).
      var record = { program: prog, vertexShader: vs, fragmentShader: fs, attributes: attrs, uniforms: uniforms };
      pointsAuthoredGLPrograms.set(layerID, record);
      return record;
    }

    // applyPointsAuthoredCustomUniforms: uploads entry.customUniforms to the
    // authored program. Uses the shaderLayout fields to determine byte count;
    // simple name → value binding via getUniformLocation.
    function applyPointsAuthoredCustomUniforms(prog, uniforms) {
      if (!uniforms || typeof uniforms !== "object") return;
      var keys = Object.keys(uniforms);
      for (var k = 0; k < keys.length; k++) {
        var name = keys[k];
        var val = uniforms[name];
        var loc = gl.getUniformLocation(prog.program, name);
        if (loc == null) continue;
        if (typeof val === "number") {
          gl.uniform1f(loc, val);
        } else if (Array.isArray(val) || (val && val.buffer instanceof ArrayBuffer)) {
          var arr = Array.isArray(val) ? val : Array.from(val);
          switch (arr.length) {
          case 1: gl.uniform1f(loc, arr[0]); break;
          case 2: gl.uniform2f(loc, arr[0], arr[1]); break;
          case 3: gl.uniform3f(loc, arr[0], arr[1], arr[2]); break;
          case 4: gl.uniform4f(loc, arr[0], arr[1], arr[2], arr[3]); break;
          default:
            if (arr.length === 9) gl.uniformMatrix3fv(loc, false, new Float32Array(arr));
            else if (arr.length === 16) gl.uniformMatrix4fv(loc, false, new Float32Array(arr));
            break;
          }
        }
      }
    }

    function disposeComputeParticleSystemRecord(record) {
      if (record && record.system && typeof record.system.dispose === "function") {
        record.system.dispose();
      }
      // Free any dynamic VBOs attached to the persistent compute entry
      // shell. Safe to run even if the entry never got drawn (all three
      // slots will just be undefined). pointsEntryBuffers is keyed by
      // buffer identity so remove there too to keep dispose() idempotent.
      if (record && record.pointsEntry) {
        var ce = record.pointsEntry;
        if (ce._vboPos) { gl.deleteBuffer(ce._vboPos); pointsEntryBuffers.delete(ce._vboPos); ce._vboPos = null; }
        if (ce._vboSizes) { gl.deleteBuffer(ce._vboSizes); pointsEntryBuffers.delete(ce._vboSizes); ce._vboSizes = null; }
        if (ce._vboColors) { gl.deleteBuffer(ce._vboColors); pointsEntryBuffers.delete(ce._vboColors); ce._vboColors = null; }
        record.pointsEntry = null;
      }
    }

    function syncComputeParticleSystems(entries) {
      var activeIds = new Set();
      var records = [];
      var sourceEntries = Array.isArray(entries) ? entries : [];
      for (var i = 0; i < sourceEntries.length; i++) {
        var entry = sourceEntries[i];
        if (!entry || typeof entry !== "object") continue;
        var id = typeof entry.id === "string" && entry.id ? entry.id : ("scene-particles-" + i);
        var signature = sceneComputeSystemSignature(entry);
        activeIds.add(id);
        var record = computeParticleSystems.get(id);
        if (!record || record.signature !== signature) {
          disposeComputeParticleSystemRecord(record);
          record = {
            signature: signature,
            system: createSceneParticleSystem(null, entry),
            sourceEntry: entry,
            colorBuffer: null,
          };
          computeParticleSystems.set(id, record);
        } else if (record.system) {
          record.system.entry = entry;
          record.sourceEntry = entry;
        }
        if (record && record.system) {
          records.push(record);
        }
      }
      for (const [id, record] of computeParticleSystems.entries()) {
        if (!activeIds.has(id)) {
          disposeComputeParticleSystemRecord(record);
          computeParticleSystems.delete(id);
        }
      }
      return records;
    }

    function buildComputePointsEntries(entries, timeSeconds) {
      var currentTime = Number.isFinite(timeSeconds) ? timeSeconds : 0;
      var deltaTime = lastComputeParticleTimeSeconds == null
        ? 0
        : Math.max(0, Math.min(0.1, currentTime - lastComputeParticleTimeSeconds));
      lastComputeParticleTimeSeconds = currentTime;
      var records = syncComputeParticleSystems(entries);
      var pointsEntries = [];
      for (var i = 0; i < records.length; i++) {
        var record = records[i];
        var system = record.system;
        if (!system) continue;
        system.update(deltaTime, currentTime);
        if (!record.colorBuffer || record.colorBuffer.length !== system.count * 4) {
          record.colorBuffer = new Float32Array(system.count * 4);
        }
        for (var pi = 0; pi < system.count; pi++) {
          var rgbBase = pi * 3;
          var rgbaBase = pi * 4;
          record.colorBuffer[rgbaBase] = system.colors[rgbBase];
          record.colorBuffer[rgbaBase + 1] = system.colors[rgbBase + 1];
          record.colorBuffer[rgbaBase + 2] = system.colors[rgbBase + 2];
          record.colorBuffer[rgbaBase + 3] = system.opacities[pi];
        }
        var material = system.entry && system.entry.material && typeof system.entry.material === "object"
          ? system.entry.material
          : {};
        var emitter = system.entry && system.entry.emitter && typeof system.entry.emitter === "object"
          ? system.entry.emitter
          : {};
        // Reuse the same synthetic entry object across frames. The
        // dynamic VBO helper keys its persistent GL buffers off entry
        // identity (_vboPos / _vboSizes / _vboColors on this object),
        // so rebuilding a fresh object each frame would allocate a new
        // buffer every tick and leak GPU memory. The record persists
        // in computeParticleSystems for the lifetime of the system;
        // stashing the entry on it gives us a stable handle.
        if (!record.pointsEntry) {
          record.pointsEntry = { _dynamic: true };
        }
        var computeEntry = record.pointsEntry;
        var sourceEntry = record.sourceEntry && typeof record.sourceEntry === "object"
          ? record.sourceEntry
          : (system.entry && typeof system.entry === "object" ? system.entry : {});
        computeEntry.id = sourceEntry.id ? sourceEntry.id : ("scene-compute-points-" + i);
        computeEntry.count = system.count;
        computeEntry.color = typeof material.color === "string" ? material.color : "#ffffff";
        computeEntry.style = material.style;
        computeEntry.size = sceneNumber(material.size, 1);
        computeEntry.opacity = 1;
        computeEntry.blendMode = material.blendMode;
        computeEntry.attenuation = !!material.attenuation;
        computeEntry.customVertex = typeof sourceEntry.renderVertex === "string" ? sourceEntry.renderVertex : "";
        computeEntry.customFragment = typeof sourceEntry.renderFragment === "string" ? sourceEntry.renderFragment : "";
        computeEntry.customUniforms = sourceEntry.renderUniforms && typeof sourceEntry.renderUniforms === "object"
          ? sourceEntry.renderUniforms
          : null;
        computeEntry.renderShaderLayout = sourceEntry.renderShaderLayout && typeof sourceEntry.renderShaderLayout === "object"
          ? sourceEntry.renderShaderLayout
          : null;
        computeEntry._computeParticlesSynthetic = true;
        // Model-matrix transform from the emitter: position, rotation, spin.
        // Particles are stored in LOCAL space so the renderer's model matrix
        // applies position + rotation + time-based spin uniformly.
        computeEntry.x = sceneNumber(emitter.x, 0);
        computeEntry.y = sceneNumber(emitter.y, 0);
        computeEntry.z = sceneNumber(emitter.z, 0);
        computeEntry.rotationX = sceneNumber(emitter.rotationX, 0);
        computeEntry.rotationY = sceneNumber(emitter.rotationY, 0);
        computeEntry.rotationZ = sceneNumber(emitter.rotationZ, 0);
        computeEntry.spinX = sceneNumber(emitter.spinX, 0);
        computeEntry.spinY = sceneNumber(emitter.spinY, 0);
        computeEntry.spinZ = sceneNumber(emitter.spinZ, 0);
        computeEntry._cachedPos = system.positions;
        computeEntry._cachedSizes = system.sizes;
        computeEntry._cachedColors = record.colorBuffer;
        pointsEntries.push(computeEntry);
      }
      return pointsEntries;
    }

    // Draw all points entries from the render bundle.
    function drawPointsEntries(gl, pointsArray, environment, viewMatrix, projMatrix, timeSeconds, renderH) {
      if (pointsArray.length === 0) {
        return;
      }

      var builtinPP = ensurePointsProgram();
      if (!builtinPP) {
        return;
      }

      // Upload fog uniforms once (shared by all entries in this call).
      var env = environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);

      // Enable point sprite rendering.
      gl.enable(gl.DEPTH_TEST);
      var _pointsModelMat = new Float32Array(16);
      var _pointsTilt = new Float32Array(16);
      var _pointsSpin = new Float32Array(16);
      var currentProgram = null; // track active program to avoid redundant useProgram calls

      for (var i = 0; i < pointsArray.length; i++) {
        var entry = pointsArray[i];
        // Select program: authored (GLSL) when customVertex/Fragment present, else builtin.
        var layerID = (typeof entry.id === "string" && entry.id) ? entry.id : ("points-" + i);
        var hasAuthoredGL = (typeof entry.customVertex === "string" && entry.customVertex.trim()) &&
                            (typeof entry.customFragment === "string" && entry.customFragment.trim());
        var pp = hasAuthoredGL ? ensurePointsAuthoredGLProgram(entry, layerID) : null;
        if (!pp) pp = builtinPP;
        if (currentProgram !== pp.program) {
          gl.useProgram(pp.program);
          currentProgram = pp.program;
          // Re-upload frame-level uniforms for the new program.
          gl.uniformMatrix4fv(pp.uniforms.viewMatrix, false, viewMatrix);
          gl.uniformMatrix4fv(pp.uniforms.projectionMatrix, false, projMatrix);
          gl.uniform1f(pp.uniforms.viewportHeight, renderH);
          if (pp.uniforms.hasFog != null) gl.uniform1i(pp.uniforms.hasFog, fogDensity > 0 ? 1 : 0);
          if (pp.uniforms.fogDensity != null) gl.uniform1f(pp.uniforms.fogDensity, fogDensity);
          if (pp.uniforms.fogColor != null) gl.uniform3f(pp.uniforms.fogColor, fogColorRGBA[0], fogColorRGBA[1], fogColorRGBA[2]);
        }
        // Upload authored custom uniforms if using an authored program.
        if (hasAuthoredGL && pp !== builtinPP) {
          applyPointsAuthoredCustomUniforms(pp, entry.customUniforms);
        }

        var px = sceneNumber(entry.x, 0);
        var py = sceneNumber(entry.y, 0);
        var pz = sceneNumber(entry.z, 0);
        var hasSpin = sceneNumber(entry.spinX, 0) !== 0 || sceneNumber(entry.spinY, 0) !== 0 || sceneNumber(entry.spinZ, 0) !== 0;

        if (hasSpin) {
          // Pinwheel: model = T * R_tilt * R_spin
          // R_spin applied first (local space), then R_tilt orients it.

          // R_spin from spin Euler.
          var spx = sceneNumber(entry.spinX, 0) * timeSeconds;
          var spy = sceneNumber(entry.spinY, 0) * timeSeconds;
          var spz = sceneNumber(entry.spinZ, 0) * timeSeconds;
          var csx = Math.cos(spx), ssx = Math.sin(spx);
          var csy = Math.cos(spy), ssy = Math.sin(spy);
          var csz = Math.cos(spz), ssz = Math.sin(spz);
          _pointsSpin[0] = csy*csz; _pointsSpin[4] = ssx*ssy*csz-csx*ssz; _pointsSpin[8]  = csx*ssy*csz+ssx*ssz; _pointsSpin[12] = 0;
          _pointsSpin[1] = csy*ssz; _pointsSpin[5] = ssx*ssy*ssz+csx*csz; _pointsSpin[9]  = csx*ssy*ssz-ssx*csz; _pointsSpin[13] = 0;
          _pointsSpin[2] = -ssy;    _pointsSpin[6] = ssx*csy;             _pointsSpin[10] = csx*csy;             _pointsSpin[14] = 0;
          _pointsSpin[3] = 0;       _pointsSpin[7] = 0;                   _pointsSpin[11] = 0;                   _pointsSpin[15] = 1;

          // R_tilt from static rotation Euler + translation.
          var rx = sceneNumber(entry.rotationX, 0);
          var ry = sceneNumber(entry.rotationY, 0);
          var rz = sceneNumber(entry.rotationZ, 0);
          var cxr = Math.cos(rx), sxr = Math.sin(rx);
          var cyr = Math.cos(ry), syr = Math.sin(ry);
          var czr = Math.cos(rz), szr = Math.sin(rz);
          _pointsTilt[0] = cyr*czr; _pointsTilt[4] = sxr*syr*czr-cxr*szr; _pointsTilt[8]  = cxr*syr*czr+sxr*szr; _pointsTilt[12] = px;
          _pointsTilt[1] = cyr*szr; _pointsTilt[5] = sxr*syr*szr+cxr*czr; _pointsTilt[9]  = cxr*syr*szr-sxr*czr; _pointsTilt[13] = py;
          _pointsTilt[2] = -syr;    _pointsTilt[6] = sxr*cyr;             _pointsTilt[10] = cxr*cyr;             _pointsTilt[14] = pz;
          _pointsTilt[3] = 0;       _pointsTilt[7] = 0;                   _pointsTilt[11] = 0;                   _pointsTilt[15] = 1;

          // model = tilt * spin
          sceneMat4MultiplyInto(_pointsModelMat, _pointsTilt, _pointsSpin);
        } else {
          // No spin — just static rotation + translation.
          var rx = sceneNumber(entry.rotationX, 0);
          var ry = sceneNumber(entry.rotationY, 0);
          var rz = sceneNumber(entry.rotationZ, 0);
          var cxr = Math.cos(rx), sxr = Math.sin(rx);
          var cyr = Math.cos(ry), syr = Math.sin(ry);
          var czr = Math.cos(rz), szr = Math.sin(rz);
          _pointsModelMat[0] = cyr*czr; _pointsModelMat[4] = sxr*syr*czr-cxr*szr; _pointsModelMat[8]  = cxr*syr*czr+sxr*szr; _pointsModelMat[12] = px;
          _pointsModelMat[1] = cyr*szr; _pointsModelMat[5] = sxr*syr*szr+cxr*czr; _pointsModelMat[9]  = cxr*syr*szr-sxr*czr; _pointsModelMat[13] = py;
          _pointsModelMat[2] = -syr;    _pointsModelMat[6] = sxr*cyr;             _pointsModelMat[10] = cxr*cyr;             _pointsModelMat[14] = pz;
          _pointsModelMat[3] = 0;       _pointsModelMat[7] = 0;                   _pointsModelMat[11] = 0;                   _pointsModelMat[15] = 1;
        }
        gl.uniformMatrix4fv(pp.uniforms.modelMatrix, false, _pointsModelMat);
        var count = sceneNumber(entry.count, 0);
        if (count <= 0) continue;

        // Blend mode.
        var blendMode = typeof entry.blendMode === "string" ? entry.blendMode.toLowerCase() : "";
        if (blendMode === "additive") {
          gl.enable(gl.BLEND);
          gl.blendFunc(gl.SRC_ALPHA, gl.ONE);
        } else if (blendMode === "alpha") {
          gl.enable(gl.BLEND);
          gl.blendFunc(gl.SRC_ALPHA, gl.ONE_MINUS_SRC_ALPHA);
        } else {
          gl.disable(gl.BLEND);
        }

        // Depth write control.
        var depthWrite = entry.depthWrite !== false;
        gl.depthMask(depthWrite);
        gl.depthFunc(gl.LEQUAL);

        // Uniforms.
        gl.uniform1f(pp.uniforms.opacity, clamp01(sceneNumber(entry.opacity, 1)));

        var defaultColorRGBA = sceneColorRGBA(entry.color, [1, 1, 1, 1]);
        gl.uniform4f(pp.uniforms.defaultColor, defaultColorRGBA[0], defaultColorRGBA[1], defaultColorRGBA[2], 1);
        gl.uniform1f(pp.uniforms.defaultSize, sceneNumber(entry.size, 1));
        gl.uniform1i(pp.uniforms.sizeAttenuation, entry.attenuation ? 1 : 0);
        gl.uniform1i(pp.uniforms.pointStyle, scenePointStyleCode(entry.style));
        gl.uniform1f(pp.uniforms.minPixelSize, Math.max(0, sceneNumber(entry.minPixelSize, 0)));
        gl.uniform1f(pp.uniforms.maxPixelSize, Math.max(0, sceneNumber(entry.maxPixelSize, 0)));

        // Cache typed arrays on first use — positions/sizes/colors are static.
        var rawPositions = entry.positions;
        if (!entry._cachedPos && rawPositions && (Array.isArray(rawPositions) || sceneIsNumericTypedArray(rawPositions)) && rawPositions.length >= count * 3) {
          entry._cachedPos = rawPositions instanceof Float32Array ? rawPositions : new Float32Array(rawPositions);
        }
        var rawSizes = entry.sizes;
        if (!entry._cachedSizes && rawSizes && (Array.isArray(rawSizes) || sceneIsNumericTypedArray(rawSizes)) && rawSizes.length >= count) {
          entry._cachedSizes = rawSizes instanceof Float32Array ? rawSizes : new Float32Array(rawSizes);
        }
        var rawColors = entry.colors;
        if (!entry._cachedColors && rawColors && (Array.isArray(rawColors) || sceneIsNumericTypedArray(rawColors)) && rawColors.length >= count) {
          if (Array.isArray(rawColors) && typeof rawColors[0] === "string") {
            entry._cachedColors = new Float32Array(count * 4);
            for (var ci = 0; ci < count; ci++) {
              var crgba = sceneColorRGBA(rawColors[ci], [1, 1, 1, 1]);
              entry._cachedColors[ci * 4] = crgba[0];
              entry._cachedColors[ci * 4 + 1] = crgba[1];
              entry._cachedColors[ci * 4 + 2] = crgba[2];
              entry._cachedColors[ci * 4 + 3] = crgba[3];
            }
          } else if (rawColors.length >= count * 4) {
            entry._cachedColors = new Float32Array(rawColors);
          } else if (rawColors.length >= count * 3) {
            entry._cachedColors = new Float32Array(count * 4);
            for (var ci2 = 0; ci2 < count; ci2++) {
              entry._cachedColors[ci2 * 4] = rawColors[ci2 * 3];
              entry._cachedColors[ci2 * 4 + 1] = rawColors[ci2 * 3 + 1];
              entry._cachedColors[ci2 * 4 + 2] = rawColors[ci2 * 3 + 2];
              entry._cachedColors[ci2 * 4 + 3] = 1;
            }
          }
        }
        // Positions (vec3) — bind the entry's cached VBO (uploaded once
        // per typed-array lifetime for static points, updated in-place
        // via bufferSubData for dynamic compute particle systems).
        if (!entry._cachedPos) continue;
        var posBuf = entry._dynamic
          ? ensureDynamicPointsVBO(entry, "_vboPos", entry._cachedPos)
          : ensureStaticPointVBO(entry, "_vboPos", "_vboPosSource", entry._cachedPos);
        gl.bindBuffer(gl.ARRAY_BUFFER, posBuf);
        gl.enableVertexAttribArray(pp.attributes.position);
        gl.vertexAttribPointer(pp.attributes.position, 3, gl.FLOAT, false, 0, 0);

        // Per-vertex sizes (float).
        var hasSizes = !!entry._cachedSizes;
        gl.uniform1i(pp.uniforms.hasPerVertexSize, hasSizes ? 1 : 0);
        if (hasSizes && pp.attributes.size >= 0) {
          var sizeBuf = entry._dynamic
            ? ensureDynamicPointsVBO(entry, "_vboSizes", entry._cachedSizes)
            : ensureStaticPointVBO(entry, "_vboSizes", "_vboSizesSource", entry._cachedSizes);
          gl.bindBuffer(gl.ARRAY_BUFFER, sizeBuf);
          gl.enableVertexAttribArray(pp.attributes.size);
          gl.vertexAttribPointer(pp.attributes.size, 1, gl.FLOAT, false, 0, 0);
        } else if (pp.attributes.size >= 0) {
          gl.disableVertexAttribArray(pp.attributes.size);
          gl.vertexAttrib1f(pp.attributes.size, sceneNumber(entry.size, 1));
        }

        // Per-vertex colors (vec4 rgba 0-1).
        var hasColors = !!entry._cachedColors;
        gl.uniform1i(pp.uniforms.hasPerVertexColor, hasColors ? 1 : 0);
        if (hasColors && pp.attributes.color >= 0) {
          var colorBuf = entry._dynamic
            ? ensureDynamicPointsVBO(entry, "_vboColors", entry._cachedColors)
            : ensureStaticPointVBO(entry, "_vboColors", "_vboColorsSource", entry._cachedColors);
          gl.bindBuffer(gl.ARRAY_BUFFER, colorBuf);
          gl.enableVertexAttribArray(pp.attributes.color);
          gl.vertexAttribPointer(pp.attributes.color, 4, gl.FLOAT, false, 0, 0);
        } else if (pp.attributes.color >= 0) {
          gl.disableVertexAttribArray(pp.attributes.color);
          gl.vertexAttrib4f(pp.attributes.color, defaultColorRGBA[0], defaultColorRGBA[1], defaultColorRGBA[2], 1);
        }

        gl.drawArrays(gl.POINTS, 0, count);
        if (entry._computeParticlesSynthetic) {
          webglComputeParticleDrawStats.drawEntries += 1;
          webglComputeParticleDrawStats.drawInstances += count;
          webglComputeParticleDrawStats.drawCalls += 1;
          if (hasAuthoredGL && pp !== builtinPP) {
            webglComputeParticleDrawStats.authoredDrawEntries += 1;
            webglComputeParticleDrawStats.authoredDrawInstances += count;
            webglComputeParticleDrawStats.authoredDrawCalls += 1;
          }
        }
      }

      // Restore state.
      gl.depthMask(true);
      gl.disable(gl.BLEND);

      // Switch back to PBR program.
      gl.useProgram(program);
    }

	    // Ensure the instanced PBR program is compiled (lazy init).
	    function ensureInstancedProgram() {
	      if (instancedProgram) return instancedProgram;
	      if (instancedProgramFailed) return null;
	      instancedProgram = createScenePBRInstancedProgram(gl);
	      if (!instancedProgram) {
	        instancedProgramFailed = true;
	        console.warn("[gosx] Instanced PBR shader compilation failed; instanced meshes will not render.");
	      }
	      return instancedProgram;
	    }

    // Look up or generate geometry for an instanced mesh entry.
    function getInstancedGeometry(mesh) {
      var kind = normalizeInstancedGeometryKind(mesh && mesh.kind);
      var size = sceneNumber(mesh && mesh.size, 0);
      var w = sceneNumber(mesh.width, 1);
      var h = sceneNumber(mesh.height, 1);
      var d = sceneNumber(mesh.depth, 1);
      var r = sceneNumber(mesh.radius, 0.5);
      var rt = sceneNumber(mesh.radiusTop, r);
      var rb = sceneNumber(mesh.radiusBottom, r);
      var tube = sceneNumber(mesh.tube, 0.3);
      var s = sceneNumber(mesh.segments, 32);
      var radial = sceneNumber(mesh.radialSegments, 32);
      var tubular = sceneNumber(mesh.tubularSegments, 16);
      var key = kind + ":" + size + ":" + w + ":" + h + ":" + d + ":" + r + ":" + rt + ":" + rb + ":" + tube + ":" + s + ":" + radial + ":" + tubular;
      if (instancedGeometryCache[key]) return instancedGeometryCache[key];
      var geom = generateInstancedGeometry(kind, {
        size: size,
        width: w,
        height: h,
        depth: d,
        radius: r,
        radiusTop: rt,
        radiusBottom: rb,
        tube: tube,
        segments: s,
        radialSegments: radial,
        tubularSegments: tubular,
      });
      instancedGeometryCache[key] = geom;
      return geom;
    }

    // Draw all instanced meshes from the render bundle.
    function sceneInstancedColorBuffer(mesh, count) {
      if (!mesh || count <= 0) {
        return null;
      }
      if (mesh._cachedInstanceColors) {
        return mesh._cachedInstanceColors;
      }
      var rawColors = mesh.colors;
      if (!rawColors || typeof rawColors.length !== "number") {
        return null;
      }
      if (Array.isArray(rawColors) && typeof rawColors[0] === "string") {
        mesh._cachedInstanceColors = new Float32Array(count * 4);
        for (var ci = 0; ci < count; ci++) {
          var rgba = sceneColorRGBA(rawColors[ci] || rawColors[rawColors.length - 1], [1, 1, 1, 1]);
          mesh._cachedInstanceColors[ci * 4] = rgba[0];
          mesh._cachedInstanceColors[ci * 4 + 1] = rgba[1];
          mesh._cachedInstanceColors[ci * 4 + 2] = rgba[2];
          mesh._cachedInstanceColors[ci * 4 + 3] = rgba[3];
        }
        return mesh._cachedInstanceColors;
      }
      if (rawColors.length >= count * 4) {
        mesh._cachedInstanceColors = rawColors instanceof Float32Array ? rawColors : new Float32Array(rawColors);
        return mesh._cachedInstanceColors;
      }
      if (rawColors.length >= count * 3) {
        mesh._cachedInstanceColors = new Float32Array(count * 4);
        for (var ni = 0; ni < count; ni++) {
          mesh._cachedInstanceColors[ni * 4] = rawColors[ni * 3];
          mesh._cachedInstanceColors[ni * 4 + 1] = rawColors[ni * 3 + 1];
          mesh._cachedInstanceColors[ni * 4 + 2] = rawColors[ni * 3 + 2];
          mesh._cachedInstanceColors[ni * 4 + 3] = 1;
        }
        return mesh._cachedInstanceColors;
      }
      return null;
    }

    function drawInstancedMeshes(gl, bundle, viewMatrix, projMatrix) {
      var meshes = Array.isArray(bundle.instancedMeshes) ? bundle.instancedMeshes : [];
      if (meshes.length === 0) return;

      var ip = ensureInstancedProgram();
      if (!ip) return;

      gl.useProgram(ip.program);

      // Ensure opaque render state (prior passes may leave blend/depth dirty).
      gl.enable(gl.DEPTH_TEST);
      gl.depthMask(true);
      gl.depthFunc(gl.LEQUAL);
      gl.disable(gl.BLEND);

      // Upload per-frame uniforms (camera, lights, fog, shadows).
      gl.uniformMatrix4fv(ip.uniforms.viewMatrix, false, viewMatrix);
      gl.uniformMatrix4fv(ip.uniforms.projectionMatrix, false, projMatrix);
      gl.uniform3f(ip.uniforms.cameraPosition, _frameCam.x, _frameCam.y, -_frameCam.z);

      var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
      scenePBRUploadExposure(gl, ip.uniforms, bundle.environment, postEffects.length > 0);

      scenePBRUploadLights(gl, ip.uniforms, bundle.lights, bundle.environment, _frameLightsHash);
      scenePBRUploadEnvironmentMap(gl, ip.uniforms, bundle.environment, textureCache, shadowSlots, shadowLightIndices);
      scenePBRUploadShadowUniforms(gl, ip.uniforms, shadowSlots, shadowLightIndices, bundle.lights, bundle.environment);

      var materials = Array.isArray(bundle.materials) ? bundle.materials : [];

      for (var i = 0; i < meshes.length; i++) {
        var mesh = meshes[i];
        if (!mesh.transforms) continue;

        // Count is serialized as `count` (legacyProps); `instanceCount` is often
        // absent. Resolve instanceCount→count→0 or the WebGL2 ring renders zero
        // instances.
        var instanceCount = sceneNumber(mesh.instanceCount, sceneNumber(mesh.count, 0));
        if (instanceCount <= 0) continue;

        // Generate or retrieve cached geometry for this mesh kind.
        var geom = getInstancedGeometry(mesh);
        if (!geom || geom.vertexCount <= 0) continue;

        // Upload material uniforms.
        // Engine-VM path uses materialIndex into bundle.materials.
        // Legacy path may have inline color/roughness/metalness.
        var mat = materials[sceneNumber(mesh.materialIndex, 0)] || null;
        if (!mat && mesh.color) {
          // Build an inline material from legacy properties.
          mat = {
            color: mesh.color,
            roughness: sceneNumber(mesh.roughness, 0.5),
            metalness: sceneNumber(mesh.metalness, 0),
          };
        }
        uploadMaterial(gl, ip.uniforms, mat, textureCache);

        // Per-object shadow receive control.
        gl.uniform1i(ip.uniforms.receiveShadow, mesh.receiveShadow ? 1 : 0);

        // Bind per-geometry cached VBOs. getInstancedGeometry returns
        // the same geom object for any mesh sharing kind+dimensions, so
        // the WeakMap keyed by geom.positions / normals / uvs / tangents
        // hits on every subsequent draw and avoids the repeat bufferData.
        gl.bindBuffer(gl.ARRAY_BUFFER, ensureStaticArrayVBO(staticMeshArrayVBOs, geom.positions));
        gl.enableVertexAttribArray(ip.attributes.position);
        gl.vertexAttribPointer(ip.attributes.position, 3, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, ensureStaticArrayVBO(staticMeshArrayVBOs, geom.normals));
        gl.enableVertexAttribArray(ip.attributes.normal);
        gl.vertexAttribPointer(ip.attributes.normal, 3, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, ensureStaticArrayVBO(staticMeshArrayVBOs, geom.uvs));
        gl.enableVertexAttribArray(ip.attributes.uv);
        gl.vertexAttribPointer(ip.attributes.uv, 2, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, ensureStaticArrayVBO(staticMeshArrayVBOs, geom.tangents));
        gl.enableVertexAttribArray(ip.attributes.tangent);
        gl.vertexAttribPointer(ip.attributes.tangent, 4, gl.FLOAT, false, 0, 0);

        // Cache the transforms Float32Array on the mesh entry (same
        // pattern as points _cachedPos). normalizeSceneInstancedMeshEntry
        // now carries _cachedTransforms forward across ticks when
        // mesh.transforms is unchanged by reference, so this branch
        // only allocates on the first draw of a mesh identity.
        if (!mesh._cachedTransforms) {
          if (mesh.transforms instanceof Float32Array) {
            mesh._cachedTransforms = mesh.transforms;
          } else if (Array.isArray(mesh.transforms)) {
            mesh._cachedTransforms = new Float32Array(mesh.transforms);
          }
        }
        var transformData = mesh._cachedTransforms;
        if (!transformData) continue;

        // -----------------------------------------------------------------------
        // CPU frustum cull (WebGL2 path — gpu-cull capability absent).
        // When a mesh carries a cull kernel (cullKernelWGSL present), run a
        // per-instance sphere-vs-frustum test using extractFrustumPlanesJS +
        // instancePassesCullTest (both defined in 11-scene-math.js).
        // Survivors are compacted into scratch Float32Arrays; instanceCount is
        // reduced to the survivor count. The compacted data is uploaded to
        // dedicated dynamic VBOs (_cpuCullTransformVBO/_cpuCullColorVBO) owned
        // by the mesh entry so bufferSubData runs per-frame without cache
        // invalidation. When no cull config is present the mesh draws all
        // instances unchanged via the normal static-VBO path.
        // -----------------------------------------------------------------------
        var hasCullConfig = (typeof mesh.cullKernelWGSL === "string" && mesh.cullKernelWGSL.trim().length > 0);
        // Fetch full color buffer (indexed by original instance) before compaction.
        var instanceColorData = sceneInstancedColorBuffer(mesh, instanceCount);
        // Whether the draw will use the culled VBOs or the cached static VBOs.
        var useCullVBOs = false;
        if (hasCullConfig) {
          var cullRadius = (typeof mesh.cullRadius === "number" && mesh.cullRadius > 0) ? mesh.cullRadius : 2.0;
          var planes = extractFrustumPlanesJS(scratchSelenaViewProjection);
          // Allocate or reuse scratch Float32Arrays.
          var maxTFloats = instanceCount * 16;
          if (!mesh._cpuCullScratchTransforms || mesh._cpuCullScratchTransforms.length < maxTFloats) {
            mesh._cpuCullScratchTransforms = new Float32Array(maxTFloats);
          }
          var scratchT = mesh._cpuCullScratchTransforms;
          var scratchC = null;
          if (instanceColorData) {
            var maxCFloats = instanceCount * 4;
            if (!mesh._cpuCullScratchColors || mesh._cpuCullScratchColors.length < maxCFloats) {
              mesh._cpuCullScratchColors = new Float32Array(maxCFloats);
            }
            scratchC = mesh._cpuCullScratchColors;
          }
          var survivorCount = 0;
          for (var ci = 0; ci < instanceCount; ci++) {
            if (instancePassesCullTest(transformData, ci, planes, cullRadius)) {
              var srcT = ci * 16;
              var dstT = survivorCount * 16;
              for (var fi = 0; fi < 16; fi++) scratchT[dstT + fi] = transformData[srcT + fi];
              if (scratchC) {
                var srcC = ci * 4;
                var dstC = survivorCount * 4;
                scratchC[dstC]     = instanceColorData[srcC];
                scratchC[dstC + 1] = instanceColorData[srcC + 1];
                scratchC[dstC + 2] = instanceColorData[srcC + 2];
                scratchC[dstC + 3] = instanceColorData[srcC + 3];
              }
              survivorCount++;
            }
          }
          instanceCount = survivorCount;
          // If nothing survives, skip the draw entirely.
          if (instanceCount <= 0) continue;

          // Upload compacted data to per-mesh dynamic VBOs. We do NOT use the
          // static WeakMap cache (ensureStaticArrayVBO) because scratchT/scratchC
          // are the same object references across frames; the cache would skip
          // the re-upload. Instead we own dedicated dynamic VBOs here.
          if (!mesh._cpuCullTransformVBO) {
            mesh._cpuCullTransformVBO = gl.createBuffer();
            pointsEntryBuffers.add(mesh._cpuCullTransformVBO);
          }
          gl.bindBuffer(gl.ARRAY_BUFFER, mesh._cpuCullTransformVBO);
          gl.bufferData(gl.ARRAY_BUFFER, scratchT.subarray(0, instanceCount * 16), gl.DYNAMIC_DRAW);

          if (scratchC) {
            if (!mesh._cpuCullColorVBO) {
              mesh._cpuCullColorVBO = gl.createBuffer();
              pointsEntryBuffers.add(mesh._cpuCullColorVBO);
            }
            gl.bindBuffer(gl.ARRAY_BUFFER, mesh._cpuCullColorVBO);
            gl.bufferData(gl.ARRAY_BUFFER, scratchC.subarray(0, instanceCount * 4), gl.DYNAMIC_DRAW);
          }
          instanceColorData = scratchC;
          useCullVBOs = true;
        }

        var hasInstanceColor = !!(instanceColorData && ip.attributes.instanceColor >= 0);
        if (ip.uniforms.hasInstanceColor) {
          gl.uniform1i(ip.uniforms.hasInstanceColor, hasInstanceColor ? 1 : 0);
        }

        // Bind transforms VBO: dynamic cull VBO or static cached VBO.
        if (useCullVBOs) {
          gl.bindBuffer(gl.ARRAY_BUFFER, mesh._cpuCullTransformVBO);
        } else {
          gl.bindBuffer(gl.ARRAY_BUFFER, ensureStaticArrayVBO(staticMeshArrayVBOs, transformData));
        }

        // Set up mat4 attribute (4 × vec4, each with divisor 1).
        // a_instanceMatrix occupies attribute locations starting at ip.attributes.instanceMatrix.
        var baseLoc = ip.attributes.instanceMatrix;
        for (var col = 0; col < 4; col++) {
          var loc = baseLoc + col;
          gl.enableVertexAttribArray(loc);
          gl.vertexAttribPointer(loc, 4, gl.FLOAT, false, 64, col * 16);
          gl.vertexAttribDivisor(loc, 1);
        }

        if (hasInstanceColor) {
          if (useCullVBOs && mesh._cpuCullColorVBO) {
            gl.bindBuffer(gl.ARRAY_BUFFER, mesh._cpuCullColorVBO);
          } else {
            gl.bindBuffer(gl.ARRAY_BUFFER, ensureStaticArrayVBO(staticMeshArrayVBOs, instanceColorData));
          }
          gl.enableVertexAttribArray(ip.attributes.instanceColor);
          gl.vertexAttribPointer(ip.attributes.instanceColor, 4, gl.FLOAT, false, 0, 0);
          gl.vertexAttribDivisor(ip.attributes.instanceColor, 1);
        }

        // Draw instanced.
        gl.drawArraysInstanced(gl.TRIANGLES, 0, geom.vertexCount, instanceCount);

        if (hasInstanceColor) {
          gl.vertexAttribDivisor(ip.attributes.instanceColor, 0);
          gl.disableVertexAttribArray(ip.attributes.instanceColor);
        }

        // Reset divisors and disable instance attribute slots.
        for (var col = 0; col < 4; col++) {
          var loc2 = baseLoc + col;
          gl.vertexAttribDivisor(loc2, 0);
          gl.disableVertexAttribArray(loc2);
        }
      }

      // Switch back to regular PBR program.
      gl.useProgram(program);
    }

    function dispose() {
      gl.deleteBuffer(positionBuffer);
      gl.deleteBuffer(normalBuffer);
      gl.deleteBuffer(uvBuffer);
      gl.deleteBuffer(tangentBuffer);
      gl.deleteBuffer(jointsBuffer);
      gl.deleteBuffer(weightsBuffer);
      gl.deleteTexture(selenaPlaceholderTexture);
      // Free all per-entry points / mesh VBOs allocated by the lazy
      // cache (static + dynamic paths both register here). WeakMap
      // entries for the static caches get GC'd alongside their typed
      // arrays — we just need to free the GL buffers explicitly since
      // WebGLBuffer has no GC hook.
      for (const buf of pointsEntryBuffers) {
        gl.deleteBuffer(buf);
      }
      pointsEntryBuffers.clear();
      staticPointEntries.clear();
      activeStaticPointEntries.clear();
      staticPointKeyedVBOs.clear();
      activeStaticPointKeys.clear();
      for (const record of computeParticleSystems.values()) {
        disposeComputeParticleSystemRecord(record);
      }
      computeParticleSystems.clear();
      lastComputeParticleTimeSeconds = null;
      if (shadowState.buffer) gl.deleteBuffer(shadowState.buffer);

      for (const record of textureCache.values()) {
        if (record && record.texture) {
          gl.deleteTexture(record.texture);
        }
      }
      textureCache.clear();

      // Clean up shadow resources (all cascades per slot).
      for (var si = 0; si < shadowSlots.length; si++) {
        disposeShadowSlot(gl, shadowSlots[si]);
        shadowSlots[si] = null;
      }

      // Clean up post-processing resources.
      if (postProcessor) {
        postProcessor.dispose();
        postProcessor = null;
      }

      if (shadowProgram) {
        gl.deleteShader(shadowProgram.vertexShader);
        gl.deleteShader(shadowProgram.fragmentShader);
        gl.deleteProgram(shadowProgram.program);
      }

      // Clean up skinned PBR program.
      if (skinnedProgram) {
        gl.deleteShader(skinnedProgram.vertexShader);
        gl.deleteShader(skinnedProgram.fragmentShader);
        gl.deleteProgram(skinnedProgram.program);
        skinnedProgram = null;
      }

      // Clean up points program.
      if (pointsProgram) {
        gl.deleteShader(pointsProgram.vertexShader);
        gl.deleteShader(pointsProgram.fragmentShader);
        gl.deleteProgram(pointsProgram.program);
        pointsProgram = null;
      }

      // Clean up instanced PBR program.
      if (instancedProgram) {
        gl.deleteShader(instancedProgram.vertexShader);
        gl.deleteShader(instancedProgram.fragmentShader);
        gl.deleteProgram(instancedProgram.program);
        instancedProgram = null;
      }
      instancedGeometryCache = {};

      for (const record of customProgramCache.values()) {
        const cp = record && record.program;
        if (cp) {
          gl.deleteShader(cp.vertexShader);
          gl.deleteShader(cp.fragmentShader);
          gl.deleteProgram(cp.program);
        }
      }
      customProgramCache.clear();

      for (const record of selenaProgramCache.values()) {
        const sp = record && record.program;
        if (sp) {
          gl.deleteShader(sp.vertexShader);
          gl.deleteShader(sp.fragmentShader);
          gl.deleteProgram(sp.program);
        }
      }
      selenaProgramCache.clear();

      if (lineProgram && lineResources) {
        disposeSceneWebGLRenderer(gl, lineProgram, lineResources);
      }

      gl.deleteShader(pbrProgram.vertexShader);
      gl.deleteShader(pbrProgram.fragmentShader);
      gl.deleteProgram(program);
    }

    return {
      kind: "webgl",
      render: render,
      dispose: dispose,
      type: "webgl-pbr",
    };
  }

  // Determine the render pass for an object given its material.
  function scenePBRObjectRenderPass(obj, material) {
    if (obj && typeof obj.renderPass === "string" && obj.renderPass) {
      const pass = obj.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    if (material && typeof material.renderPass === "string" && material.renderPass) {
      const pass = material.renderPass.toLowerCase();
      if (pass === "alpha" || pass === "additive" || pass === "opaque") {
        return pass;
      }
    }
    // If material opacity < 1, default to alpha pass.
    if (material && sceneNumber(material.opacity, 1) < 1) {
      return "alpha";
    }
    return "opaque";
  }

  // Depth-based sort comparator for translucent objects (back-to-front).
  function scenePBRDepthSort(a, b) {
    const da = sceneNumber(a && a.depthCenter, 0);
    const db = sceneNumber(b && b.depthCenter, 0);
    if (da !== db) {
      return db - da;
    }
    return String(a && a.id || "").localeCompare(String(b && b.id || ""));
  }

  function sceneWebGLCommandSequence(bundle, viewport, previousPrepared) {
    const prepared = prepareScene(
      bundle || {},
      bundle && bundle.camera || {},
      viewport || {},
      previousPrepared || null
    );
    return scenePreparedCommandSequence(prepared);
  }

  // --- Integration ---

  // Try to create a PBR renderer. If PBR shader compilation fails,
  // returns null so the caller can use the legacy renderer.
  function createScenePBRRendererOrFallback(gl, canvas, options) {
    if (!gl || !canvas) {
      return null;
    }
    // Require WebGL2 context for GLSL ES 3.0 features.
    if (typeof WebGL2RenderingContext === "undefined" || !(gl instanceof WebGL2RenderingContext)) {
      return null;
    }
    var renderer = null;
    try {
      renderer = createScenePBRRenderer(gl, canvas);
    } catch (e) {
      console.warn("[gosx] PBR renderer creation failed:", e);
      return null;
    }
    return renderer;
  }

  if (typeof window !== "undefined") {
    window.__gosx_scene3d_resource_api = Object.assign(window.__gosx_scene3d_resource_api || {}, {
      shadowPassHash: sceneShadowPassHash,
      computeCascadeSplits: sceneShadowComputeCascadeSplits,
      frustumSubCorners: sceneShadowFrustumSubCorners,
      fitLightSpaceOrtho: sceneShadowFitLightSpaceOrtho,
    });
  }

  if (typeof sceneBackendRegistry !== "undefined" && sceneBackendRegistry) {
    sceneBackendRegistry.register("webgl", {
      capabilities: ["webgl", "webgl2", "shaders", "instancing", "shadows"],
      create: function(canvas, props, capability) {
        const caps = capability || {};
        if (typeof createScenePBRRendererOrFallback === "function") {
          const useCanvasAlpha = sceneCanvasAlpha(props);
          const gl = typeof canvas.getContext === "function" ? canvas.getContext("webgl2", {
            alpha: useCanvasAlpha,
            premultipliedAlpha: useCanvasAlpha,
            antialias: caps.tier === "full" && !caps.lowPower && !caps.reducedData,
            powerPreference: caps.lowPower || caps.tier === "constrained" ? "low-power" : "high-performance",
          }) : null;
          if (gl) {
            const pbrRenderer = createScenePBRRendererOrFallback(gl, canvas, {});
            if (pbrRenderer) {
              return pbrRenderer;
            }
          }
        }
        return createSceneWebGLRenderer(canvas, {
          antialias: caps.tier === "full" && !caps.lowPower && !caps.reducedData,
          powerPreference: caps.lowPower || caps.tier === "constrained" ? "low-power" : "high-performance",
        });
      },
    });
  }
