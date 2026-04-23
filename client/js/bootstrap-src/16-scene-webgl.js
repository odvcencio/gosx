  // PBR WebGL2 rendering backend for GoSX Scene3D.
  //
  // Provides Cook-Torrance BRDF with per-pixel lighting, replacing
  // the legacy vertex-color renderer for WebGL2-capable browsers.

  // Post-effect kind constants.
  var SCENE_POST_TONE_MAPPING = "toneMapping";
  var SCENE_POST_BLOOM = "bloom";
  var SCENE_POST_VIGNETTE = "vignette";
  var SCENE_POST_COLOR_GRADE = "colorGrade";

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
    "",
    "void main() {",
    "    v_worldPosition = a_position;",
    "    v_normal = normalize(a_normal);",
    "    v_uv = a_uv;",
    "",
    "    vec3 T = normalize(a_tangent.xyz);",
    "    vec3 N = v_normal;",
    "    vec3 B = cross(N, T) * a_tangent.w;",
    "    v_tangent = T;",
    "    v_bitangent = B;",
    "",
    "    gl_Position = u_projectionMatrix * u_viewMatrix * vec4(a_position, 1.0);",
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
    "",
    "// Camera",
    "uniform vec3 u_cameraPosition;",
    "uniform mat4 u_viewMatrix;",
    "",
    "// Material",
    "uniform vec3 u_albedo;",
    "uniform float u_roughness;",
    "uniform float u_metalness;",
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
    "        fragColor = vec4(color, u_opacity);",
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
    "    fragColor = vec4(color, u_opacity);",
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
    "    if (u_sizeAttenuation) {",
    "        gl_PointSize = max(size * (u_viewportHeight * 0.5) / max(-viewPos.z, 0.001), 1.0);",
    "    } else {",
    "        gl_PointSize = max(size, 1.0);",
    "    }",
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
    "",
    "uniform mat4 u_viewMatrix;",
    "uniform mat4 u_projectionMatrix;",
    "",
    "out vec3 v_worldPosition;",
    "out vec3 v_normal;",
    "out vec2 v_uv;",
    "out vec3 v_tangent;",
    "out vec3 v_bitangent;",
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
    "void main() {",
    "    vec3 color = texture(u_texture, v_uv).rgb;",
    "    color *= u_exposure;",
    "    color = aces(color);",
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

  // Create an offscreen framebuffer with HDR color texture and depth renderbuffer.
  // Uses RGBA16F when EXT_color_buffer_float is available, RGBA8 otherwise.
  function createScenePostFBO(gl, width, height) {
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

    var depthRB = gl.createRenderbuffer();
    gl.bindRenderbuffer(gl.RENDERBUFFER, depthRB);
    gl.renderbufferStorage(gl.RENDERBUFFER, gl.DEPTH_COMPONENT24, width, height);

    gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
    gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, colorTex, 0);
    gl.framebufferRenderbuffer(gl.FRAMEBUFFER, gl.DEPTH_ATTACHMENT, gl.RENDERBUFFER, depthRB);
    gl.bindFramebuffer(gl.FRAMEBUFFER, null);

    return { fbo: fbo, colorTex: colorTex, depthRB: depthRB, width: width, height: height };
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

  // Dispose an FBO and its attachments.
  function disposeScenePostFBO(gl, fboObj) {
    if (!fboObj) return;
    if (fboObj.colorTex) gl.deleteTexture(fboObj.colorTex);
    if (fboObj.depthRB) gl.deleteRenderbuffer(fboObj.depthRB);
    if (fboObj.fbo) gl.deleteFramebuffer(fboObj.fbo);
  }

  // Post-processing manager — orchestrates an effect chain between the
  // scene render and the final screen blit.
  function createScenePostProcessor(gl) {
    var quad = createSceneFullscreenQuad(gl);
    var sceneFBO = null;
    var auxFBO = null;
    var pingPong = null;
    var currentWidth = 0;
    var currentHeight = 0;

    // Lazily compiled shader programs, keyed by effect name.
    var programs = {};

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

    // Apply ACES tone mapping.
    function applyToneMapping(inputTex, effect, targetFBO, w, h) {
      var prog = getProgram("toneMapping", SCENE_POST_TONEMAPPING_SOURCE);
      if (!prog) return inputTex;
      beginPostPass(prog, inputTex, targetFBO ? targetFBO.fbo : null, w, h);
      gl.uniform1f(gl.getUniformLocation(prog.program, "u_exposure"), sceneNumber(effect.exposure, 1.0));
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
          sceneFBO = createScenePostFBO(gl, sw, sh);
          if (auxFBO) disposeScenePostFBO(gl, auxFBO);
          auxFBO = null;
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
      apply: function(effects, scaledW, scaledH, canvasW, canvasH) {
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
    };

    var uniforms = scenePBRCacheBaseUniforms(gl, program);

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
    var w = sceneNumber(dims && dims.width, 1);
    var h = sceneNumber(dims && dims.height, 1);
    var d = sceneNumber(dims && dims.depth, 1);

    if (kind === "sphere") {
      return generateInstancedSphereGeometry(
        sceneNumber(dims && dims.radius, 0.5),
        sceneNumber(dims && dims.segments, 16)
      );
    }
    if (kind === "plane") {
      return generateInstancedPlaneGeometry(w, d);
    }

    // Default: box geometry.
    return generateInstancedBoxGeometry(w, h, d);
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
    var rings = Math.max(4, segments);
    var slices = Math.max(4, segments * 2);

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

	    // Skinned PBR program — compiled lazily on first skinned object.
	    var skinnedProgram = null;
	    var skinnedProgramFailed = false;

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

    // Points program — compiled lazily on first points entry.
    var pointsProgram = null;

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
        return;
      }
      uniforms._lastMaterial = material;
      const albedoRGBA = sceneColorRGBA(mat.color, [0.8, 0.8, 0.8, 1]);
      gl.uniform3f(uniforms.albedo, albedoRGBA[0], albedoRGBA[1], albedoRGBA[2]);
      gl.uniform1f(uniforms.roughness, sceneNumber(mat.roughness, 0.5));
      gl.uniform1f(uniforms.metalness, sceneNumber(mat.metalness, 0));
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
      if (!hasPBRData && !hasPointsData && !hasInstancedData) {
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
      const projMatrix = scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, scratchProjMatrix);

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

      // Draw instanced meshes (after regular meshes, before points).
      drawInstancedMeshes(gl, bundle, viewMatrix, projMatrix);

      // Draw points entries (after meshes, before post-processing).
      var frameTimeSeconds = performance.now() / 1000;
      activeStaticPointEntries.clear();
      activeStaticPointKeys.clear();
      drawPointsEntries(gl, Array.isArray(bundle.points) ? bundle.points : [], bundle.environment, viewMatrix, projMatrix, frameTimeSeconds, renderH);
      drawPointsEntries(gl, buildComputePointsEntries(bundle.computeParticles, frameTimeSeconds), bundle.environment, viewMatrix, projMatrix, frameTimeSeconds, renderH);
      releaseInactiveStaticPointBuffers();

      // Restore state.
      gl.depthMask(true);
      gl.disable(gl.BLEND);

      // Apply post-processing chain if active.
      if (usePostProcessing && postProcessor) {
        postProcessor.apply(postEffects, renderW, renderH, canvas.width, canvas.height);
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

      for (var i = 0; i < objectList.length; i++) {
        const obj = objectList[i];
        const matIndex = sceneNumber(obj.materialIndex, 0);
        const mat = materials[matIndex] || null;
        var isSkinned = objectIsSkinned(obj);

        // Switch to skinned program if this object has skin data.
        if (isSkinned) {
          var sp = ensureSkinnedProgram();
          if (sp && currentProgram !== sp.program) {
            gl.useProgram(sp.program);
            currentProgram = sp.program;
            currentAttribs = sp.attributes;
            currentUniforms = sp.uniforms;

            // Re-upload per-frame uniforms to the skinned program.
            // Reuse the pre-computed camera and matrices from the main render pass.
            gl.uniformMatrix4fv(currentUniforms.viewMatrix, false, scratchViewMatrix);
            gl.uniformMatrix4fv(currentUniforms.projectionMatrix, false, scratchProjMatrix);
            gl.uniform3f(currentUniforms.cameraPosition, _frameCam.x, _frameCam.y, -_frameCam.z);

            var postEffects = Array.isArray(bundle.postEffects) ? bundle.postEffects : [];
            scenePBRUploadExposure(gl, currentUniforms, bundle.environment, postEffects.length > 0);

            scenePBRUploadLights(gl, currentUniforms, bundle.lights, bundle.environment, _frameLightsHash);
            scenePBRUploadEnvironmentMap(gl, currentUniforms, bundle.environment, textureCache, shadowSlots, shadowLightIndices);

            // Re-upload shadow uniforms.
            scenePBRUploadShadowUniforms(gl, currentUniforms, shadowSlots, shadowLightIndices, bundle.lights, bundle.environment);

            // Force material re-upload since we switched programs.
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
            colorBuffer: null,
          };
          computeParticleSystems.set(id, record);
        } else if (record.system) {
          record.system.entry = entry;
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
        computeEntry.id = system.entry && system.entry.id ? system.entry.id : ("scene-compute-points-" + i);
        computeEntry.count = system.count;
        computeEntry.color = typeof material.color === "string" ? material.color : "#ffffff";
        computeEntry.style = material.style;
        computeEntry.size = sceneNumber(material.size, 1);
        computeEntry.opacity = 1;
        computeEntry.blendMode = material.blendMode;
        computeEntry.attenuation = !!material.attenuation;
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

      var pp = ensurePointsProgram();
      if (!pp) {
        return;
      }

      gl.useProgram(pp.program);

      // Upload view/projection matrices.
      gl.uniformMatrix4fv(pp.uniforms.viewMatrix, false, viewMatrix);
      gl.uniformMatrix4fv(pp.uniforms.projectionMatrix, false, projMatrix);
      gl.uniform1f(pp.uniforms.viewportHeight, renderH);

      // Upload fog uniforms.
      var env = environment || {};
      var fogDensity = sceneNumber(env.fogDensity, 0);
      gl.uniform1i(pp.uniforms.hasFog, fogDensity > 0 ? 1 : 0);
      gl.uniform1f(pp.uniforms.fogDensity, fogDensity);
      var fogColorRGBA = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);
      gl.uniform3f(pp.uniforms.fogColor, fogColorRGBA[0], fogColorRGBA[1], fogColorRGBA[2]);

      // Enable point sprite rendering.
      gl.enable(gl.DEPTH_TEST);
      var _pointsModelMat = new Float32Array(16);
      var _pointsTilt = new Float32Array(16);
      var _pointsSpin = new Float32Array(16);

      for (var i = 0; i < pointsArray.length; i++) {
        var entry = pointsArray[i];

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
      var kind = typeof mesh.kind === "string" ? mesh.kind.toLowerCase() : "box";
      var w = sceneNumber(mesh.width, 1);
      var h = sceneNumber(mesh.height, 1);
      var d = sceneNumber(mesh.depth, 1);
      var r = sceneNumber(mesh.radius, 0.5);
      var s = sceneNumber(mesh.segments, 16);
      var key = kind + ":" + w + ":" + h + ":" + d + ":" + r + ":" + s;
      if (instancedGeometryCache[key]) return instancedGeometryCache[key];
      var geom = generateInstancedGeometry(kind, { width: w, height: h, depth: d, radius: r, segments: s });
      instancedGeometryCache[key] = geom;
      return geom;
    }

    // Draw all instanced meshes from the render bundle.
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
        if (!mesh.transforms || mesh.instanceCount <= 0) continue;

        var instanceCount = sceneNumber(mesh.instanceCount, 0);
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

        // Bind cached transforms VBO.
        gl.bindBuffer(gl.ARRAY_BUFFER, ensureStaticArrayVBO(staticMeshArrayVBOs, transformData));

        // Set up mat4 attribute (4 × vec4, each with divisor 1).
        // a_instanceMatrix occupies attribute locations starting at ip.attributes.instanceMatrix.
        var baseLoc = ip.attributes.instanceMatrix;
        for (var col = 0; col < 4; col++) {
          var loc = baseLoc + col;
          gl.enableVertexAttribArray(loc);
          gl.vertexAttribPointer(loc, 4, gl.FLOAT, false, 64, col * 16);
          gl.vertexAttribDivisor(loc, 1);
        }

        // Draw instanced.
        gl.drawArraysInstanced(gl.TRIANGLES, 0, geom.vertexCount, instanceCount);

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
