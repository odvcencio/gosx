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
    "",
    "// Shadow maps (max 2 directional lights)",
    "uniform sampler2D u_shadowMap0;",
    "uniform mat4 u_lightSpaceMatrix0;",
    "uniform bool u_hasShadow0;",
    "uniform float u_shadowBias0;",
    "",
    "uniform sampler2D u_shadowMap1;",
    "uniform mat4 u_lightSpaceMatrix1;",
    "uniform bool u_hasShadow1;",
    "uniform float u_shadowBias1;",
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
    "// 4-tap Poisson disk PCF shadow sampling.",
    "float shadowFactor(sampler2D shadowMap, mat4 lightSpaceMatrix, float bias) {",
    "    vec4 lightSpacePos = lightSpaceMatrix * vec4(v_worldPosition, 1.0);",
    "    vec3 projCoords = lightSpacePos.xyz / lightSpacePos.w;",
    "    projCoords = projCoords * 0.5 + 0.5;",
    "",
    "    if (projCoords.z > 1.0) return 1.0;",
    "",
    "    float shadow = 0.0;",
    "    float texelSize = 1.0 / float(textureSize(shadowMap, 0).x);",
    "    vec2 poissonDisk[4] = vec2[](",
    "        vec2(-0.94201624, -0.39906216),",
    "        vec2(0.94558609, -0.76890725),",
    "        vec2(-0.094184101, -0.92938870),",
    "        vec2(0.34495938, 0.29387760)",
    "    );",
    "",
    "    for (int i = 0; i < 4; i++) {",
    "        float depth = texture(shadowMap, projCoords.xy + poissonDisk[i] * texelSize).r;",
    "        shadow += (projCoords.z - bias > depth) ? 0.0 : 1.0;",
    "    }",
    "    return shadow / 4.0;",
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
    "                shadow = shadowFactor(u_shadowMap0, u_lightSpaceMatrix0, u_shadowBias0);",
    "            } else if (u_hasShadow1 && i == u_shadowLightIndex1) {",
    "                shadow = shadowFactor(u_shadowMap1, u_lightSpaceMatrix1, u_shadowBias1);",
    "            }",
    "        }",
    "",
    "        vec3 radiance = lightColor * intensity * attenuation;",
    "        Lo += (kD * albedo / PI + specular) * radiance * NdotL * shadow;",
    "    }",
    "",
    "    // Environment hemisphere lighting.",
    "    float hemi = N.y * 0.5 + 0.5;",
    "    vec3 envDiffuse = u_ambientColor * u_ambientIntensity",
    "                    + u_skyColor * u_skyIntensity * hemi",
    "                    + u_groundColor * u_groundIntensity * (1.0 - hemi);",
    "    vec3 ambient = envDiffuse * albedo;",
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
    "    v_worldPosition = pos.xyz;",
    "    v_normal = normalize(norm);",
    "    v_uv = a_uv;",
    "",
    "    vec3 T = normalize(tang);",
    "    vec3 N = v_normal;",
    "    vec3 B = cross(N, T) * a_tangent.w;",
    "    v_tangent = T;",
    "    v_bitangent = B;",
    "",
    "    gl_Position = u_projectionMatrix * u_viewMatrix * pos;",
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

  // Render a depth-only shadow pass into the shadow framebuffer.
  // shadowState holds a persistent GL buffer and scratch typed array
  // to avoid per-object per-light per-frame allocations.
  function renderSceneShadowPass(gl, shadowProgram, shadowResources, lightMatrix, bundle, shadowState) {
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
    function beginPostPass(prog, inputTex, targetFBO, w, h) {
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

    if (typeof Image === "function") {
      const image = new Image();
      image.onload = function() {
        gl.bindTexture(gl.TEXTURE_2D, texture);
        gl.texImage2D(gl.TEXTURE_2D, 0, gl.RGBA, gl.RGBA, gl.UNSIGNED_BYTE, image);
        gl.generateMipmap(gl.TEXTURE_2D);
        gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
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

      shadowMap0: gl.getUniformLocation(program, "u_shadowMap0"),
      lightSpaceMatrix0: gl.getUniformLocation(program, "u_lightSpaceMatrix0"),
      hasShadow0: gl.getUniformLocation(program, "u_hasShadow0"),
      shadowBias0: gl.getUniformLocation(program, "u_shadowBias0"),
      shadowLightIndex0: gl.getUniformLocation(program, "u_shadowLightIndex0"),

      shadowMap1: gl.getUniformLocation(program, "u_shadowMap1"),
      lightSpaceMatrix1: gl.getUniformLocation(program, "u_lightSpaceMatrix1"),
      hasShadow1: gl.getUniformLocation(program, "u_hasShadow1"),
      shadowBias1: gl.getUniformLocation(program, "u_shadowBias1"),
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

    // Cache uniform locations — base set plus skinning extras.
    var uniforms = scenePBRCacheBaseUniforms(gl, program);
    uniforms.hasSkin = gl.getUniformLocation(program, "u_hasSkin");
    uniforms.jointMatrices = [];

    // Populate per-joint matrix uniform locations (max 64 joints).
    for (var j = 0; j < 64; j++) {
      uniforms.jointMatrices.push(gl.getUniformLocation(program, "u_jointMatrices[" + j + "]"));
    }

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

  // Upload the light array and environment uniforms for the current frame.
  function scenePBRUploadLights(gl, uniforms, lights, environment) {
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

  // Upload exposure and tone mapping mode uniforms.
  function scenePBRUploadExposure(gl, uniforms, environment, usePostProcessing) {
    var env = environment || {};
    var exposure = sceneNumber(env.exposure, 0);
    if (exposure <= 0) exposure = 1.0;
    gl.uniform1f(uniforms.exposure, exposure);
    gl.uniform1i(uniforms.toneMapMode, usePostProcessing ? 0 : sceneToneMapMode(env.toneMapping));
  }

  // Upload shadow map uniforms for both slots to the given program's uniforms.
  function scenePBRUploadShadowUniforms(gl, uniforms, shadowSlots, shadowLightMatrices, shadowLightIndices, lights) {
    var lightArray = Array.isArray(lights) ? lights : [];

    if (shadowSlots[0] && shadowLightMatrices[0]) {
      scenePBRBindTexture(gl, 5, shadowSlots[0].depthTexture);
      gl.uniform1i(uniforms.shadowMap0, 5);
      gl.uniformMatrix4fv(uniforms.lightSpaceMatrix0, false, shadowLightMatrices[0]);
      gl.uniform1i(uniforms.hasShadow0, 1);
      var bias0 = sceneNumber(lightArray[shadowLightIndices[0]] && lightArray[shadowLightIndices[0]].shadowBias, 0.005);
      gl.uniform1f(uniforms.shadowBias0, bias0);
      gl.uniform1i(uniforms.shadowLightIndex0, shadowLightIndices[0]);
    } else {
      gl.uniform1i(uniforms.hasShadow0, 0);
      gl.uniform1i(uniforms.shadowLightIndex0, -1);
    }

    if (shadowSlots[1] && shadowLightMatrices[1]) {
      scenePBRBindTexture(gl, 6, shadowSlots[1].depthTexture);
      gl.uniform1i(uniforms.shadowMap1, 6);
      gl.uniformMatrix4fv(uniforms.lightSpaceMatrix1, false, shadowLightMatrices[1]);
      gl.uniform1i(uniforms.hasShadow1, 1);
      var bias1 = sceneNumber(lightArray[shadowLightIndices[1]] && lightArray[shadowLightIndices[1]].shadowBias, 0.005);
      gl.uniform1f(uniforms.shadowBias1, bias1);
      gl.uniform1i(uniforms.shadowLightIndex1, shadowLightIndices[1]);
    } else {
      gl.uniform1i(uniforms.hasShadow1, 0);
      gl.uniform1i(uniforms.shadowLightIndex1, -1);
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

    // Shadow program (depth-only shader for shadow pass).
    const shadowProgram = createSceneShadowProgram(gl);

    // Shadow resources — up to 2 directional light shadow maps.
    // Created lazily on first use, reused across frames.
    var shadowSlots = [null, null];

    // Post-processing pipeline — created lazily when postEffects are present.
    var postProcessor = null;

    // Per-frame shadow state, shared between render() and drawPBRObjectList().
    var shadowLightMatrices = [null, null];
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

    // Persistent GPU buffers for points.
    const pointsPositionBuffer = gl.createBuffer();
    const pointsSizeBuffer = gl.createBuffer();
    const pointsColorBuffer = gl.createBuffer();
    var computeParticleSystems = new Map();
    var lastComputeParticleTimeSeconds = null;

    // Instanced PBR program — compiled lazily on first instanced mesh.
    var instancedProgram = null;

    // Persistent GPU buffers for instanced mesh rendering.
    const instanceTransformBuffer = gl.createBuffer();
    const instanceVertexBuffer = gl.createBuffer();
    const instanceNormalBuffer = gl.createBuffer();
    const instanceUVBuffer = gl.createBuffer();
    const instanceTangentBuffer = gl.createBuffer();

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

    // Per-frame camera cache — set once in render(), reused in drawPBRObjectList.
    var _frameCam = null;

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

      // --- Shadow Pass ---
      // Identify shadow-casting directional lights (max 2) and render depth maps.
      // Reset per-frame shadow state (closure-scoped for drawPBRObjectList access).
      shadowLightMatrices[0] = null; shadowLightMatrices[1] = null;
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
          var shadowSize = sceneNumber(light.shadowSize, 1024);
          // Clamp to reasonable range (driver limits).
          shadowSize = Math.max(256, Math.min(4096, shadowSize));
          // Apply scene-wide shadow pixel cap.
          shadowSize = resolveShadowSize(shadowSize, shadowMaxPixels);

          // Create or resize shadow resources for this slot.
          if (!shadowSlots[slot] || shadowSlots[slot].size !== shadowSize) {
            if (shadowSlots[slot]) {
              gl.deleteFramebuffer(shadowSlots[slot].framebuffer);
              gl.deleteTexture(shadowSlots[slot].depthTexture);
            }
            shadowSlots[slot] = createSceneShadowResources(gl, shadowSize);
          }

          var lightMatrix = sceneShadowLightSpaceMatrix(light, sceneBounds);
          shadowLightMatrices[slot] = lightMatrix;
          shadowLightIndices[slot] = li;

          renderSceneShadowPass(gl, shadowProgram, shadowSlots[slot], lightMatrix, bundle, shadowState);
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

      // Camera matrices — compute once per frame.
      _frameCam = sceneRenderCamera(bundle.camera);
      const cam = _frameCam;
      const aspect = Math.max(0.0001, canvas.width / Math.max(1, canvas.height));
      const viewMatrix = scenePBRViewMatrix(cam, scratchViewMatrix);
      const projMatrix = scenePBRProjectionMatrix(cam.fov, aspect, cam.near, cam.far, scratchProjMatrix);

      // Only activate PBR mesh program if there are mesh objects to draw.
      if (hasPBRData) {
      gl.useProgram(program);
      gl.uniformMatrix4fv(uniforms.viewMatrix, false, viewMatrix);
      gl.uniformMatrix4fv(uniforms.projectionMatrix, false, projMatrix);
      gl.uniform3f(uniforms.cameraPosition, cam.x, cam.y, -cam.z);

      // Upload exposure and tone mapping mode (disabled when post-processing handles it).
      scenePBRUploadExposure(gl, uniforms, bundle.environment, usePostProcessing);

      // Upload lights and environment once per frame.
      scenePBRUploadLights(gl, uniforms, bundle.lights, bundle.environment);

      // Upload shadow map uniforms (texture units 5 and 6, material maps use 0-4).
      scenePBRUploadShadowUniforms(gl, uniforms, shadowSlots, shadowLightMatrices, shadowLightIndices, bundle.lights);

      // Build draw list grouped by render pass.
      const drawList = buildPBRDrawList(bundle);
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
      drawPointsEntries(gl, Array.isArray(bundle.points) ? bundle.points : [], bundle.environment, viewMatrix, projMatrix, frameTimeSeconds, renderH);
      drawPointsEntries(gl, buildComputePointsEntries(bundle.computeParticles, frameTimeSeconds), bundle.environment, viewMatrix, projMatrix, frameTimeSeconds, renderH);

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
      skinnedProgram = createScenePBRSkinnedProgram(gl);
      if (!skinnedProgram) {
        console.warn("[gosx] Skinned PBR shader compilation failed; skinned objects will use static path.");
      }
      return skinnedProgram;
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

            scenePBRUploadLights(gl, currentUniforms, bundle.lights, bundle.environment);

            // Re-upload shadow uniforms.
            scenePBRUploadShadowUniforms(gl, currentUniforms, shadowSlots, shadowLightMatrices, shadowLightIndices, bundle.lights);

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

          var jointMatrices = obj.skin.jointMatrices;
          if (jointMatrices) {
            var jointCount = Math.min(Math.floor(jointMatrices.length / 16), 64);
            for (var ji = 0; ji < jointCount; ji++) {
              gl.uniformMatrix4fv(
                currentUniforms.jointMatrices[ji], false,
                jointMatrices.subarray(ji * 16, ji * 16 + 16)
              );
            }
          }
        } else if (currentUniforms.hasSkin) {
          gl.uniform1i(currentUniforms.hasSkin, 0);
        }

        // Upload vertex data for this object.
        const offset = obj.vertexOffset;
        const count = obj.vertexCount;

        // Positions (vec3).
        const positions = sliceToFloat32(bundle.worldMeshPositions, offset, count, 3, "positions");
        gl.bindBuffer(gl.ARRAY_BUFFER, positionBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, positions, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(currentAttribs.position);
        gl.vertexAttribPointer(currentAttribs.position, 3, gl.FLOAT, false, 0, 0);

        // Normals (vec3).
        const normals = sliceToFloat32(bundle.worldMeshNormals, offset, count, 3, "normals");
        gl.bindBuffer(gl.ARRAY_BUFFER, normalBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, normals, gl.DYNAMIC_DRAW);
        gl.enableVertexAttribArray(currentAttribs.normal);
        gl.vertexAttribPointer(currentAttribs.normal, 3, gl.FLOAT, false, 0, 0);

        // UVs (vec2).
        if (bundle.worldMeshUVs) {
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
        if (bundle.worldMeshTangents) {
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

          gl.bindBuffer(gl.ARRAY_BUFFER, jointsBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, joints instanceof Float32Array ? joints : new Float32Array(joints), gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.joints);
          gl.vertexAttribPointer(currentAttribs.joints, 4, gl.FLOAT, false, 0, 0);

          gl.bindBuffer(gl.ARRAY_BUFFER, weightsBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, weights instanceof Float32Array ? weights : new Float32Array(weights), gl.DYNAMIC_DRAW);
          gl.enableVertexAttribArray(currentAttribs.weights);
          gl.vertexAttribPointer(currentAttribs.weights, 4, gl.FLOAT, false, 0, 0);
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
        pointsEntries.push({
          id: system.entry && system.entry.id ? system.entry.id : ("scene-compute-points-" + i),
          count: system.count,
          color: typeof material.color === "string" ? material.color : "#ffffff",
          style: material.style,
          size: sceneNumber(material.size, 1),
          opacity: 1,
          blendMode: material.blendMode,
          attenuation: !!material.attenuation,
          // Model-matrix transform from the emitter: position, rotation, spin.
          // Particles are stored in LOCAL space so the renderer's model matrix
          // applies position + rotation + time-based spin uniformly.
          x: sceneNumber(emitter.x, 0),
          y: sceneNumber(emitter.y, 0),
          z: sceneNumber(emitter.z, 0),
          rotationX: sceneNumber(emitter.rotationX, 0),
          rotationY: sceneNumber(emitter.rotationY, 0),
          rotationZ: sceneNumber(emitter.rotationZ, 0),
          spinX: sceneNumber(emitter.spinX, 0),
          spinY: sceneNumber(emitter.spinY, 0),
          spinZ: sceneNumber(emitter.spinZ, 0),
          _cachedPos: system.positions,
          _cachedSizes: system.sizes,
          _cachedColors: record.colorBuffer,
        });
      }
      return pointsEntries;
    }

    // Draw all points entries from the render bundle.
    function drawPointsEntries(gl, pointsArray, environment, viewMatrix, projMatrix, timeSeconds, renderH) {
      if (pointsArray.length === 0) return;

      var pp = ensurePointsProgram();
      if (!pp) return;

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
        if (!entry._cachedPos && Array.isArray(entry.positions) && entry.positions.length >= count * 3) {
          entry._cachedPos = new Float32Array(entry.positions);
        }
        if (!entry._cachedSizes && Array.isArray(entry.sizes) && entry.sizes.length >= count) {
          entry._cachedSizes = new Float32Array(entry.sizes);
        }
        if (!entry._cachedColors && Array.isArray(entry.colors) && entry.colors.length >= count) {
          var rawColors = entry.colors;
          if (typeof rawColors[0] === "string") {
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
        // Positions (vec3) — upload from cached typed array.
        if (!entry._cachedPos) continue;
        gl.bindBuffer(gl.ARRAY_BUFFER, pointsPositionBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, entry._cachedPos, gl.STREAM_DRAW);
        gl.enableVertexAttribArray(pp.attributes.position);
        gl.vertexAttribPointer(pp.attributes.position, 3, gl.FLOAT, false, 0, 0);

        // Per-vertex sizes (float).
        var hasSizes = !!entry._cachedSizes;
        gl.uniform1i(pp.uniforms.hasPerVertexSize, hasSizes ? 1 : 0);
        if (hasSizes && pp.attributes.size >= 0) {
          gl.bindBuffer(gl.ARRAY_BUFFER, pointsSizeBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, entry._cachedSizes, gl.STREAM_DRAW);
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
          gl.bindBuffer(gl.ARRAY_BUFFER, pointsColorBuffer);
          gl.bufferData(gl.ARRAY_BUFFER, entry._cachedColors, gl.STREAM_DRAW);
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
      instancedProgram = createScenePBRInstancedProgram(gl);
      if (!instancedProgram) {
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

      scenePBRUploadLights(gl, ip.uniforms, bundle.lights, bundle.environment);
      scenePBRUploadShadowUniforms(gl, ip.uniforms, shadowSlots, shadowLightMatrices, shadowLightIndices, bundle.lights);

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

        // Upload geometry data to persistent buffers.
        gl.bindBuffer(gl.ARRAY_BUFFER, instanceVertexBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, geom.positions, gl.STATIC_DRAW);
        gl.enableVertexAttribArray(ip.attributes.position);
        gl.vertexAttribPointer(ip.attributes.position, 3, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceNormalBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, geom.normals, gl.STATIC_DRAW);
        gl.enableVertexAttribArray(ip.attributes.normal);
        gl.vertexAttribPointer(ip.attributes.normal, 3, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceUVBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, geom.uvs, gl.STATIC_DRAW);
        gl.enableVertexAttribArray(ip.attributes.uv);
        gl.vertexAttribPointer(ip.attributes.uv, 2, gl.FLOAT, false, 0, 0);

        gl.bindBuffer(gl.ARRAY_BUFFER, instanceTangentBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, geom.tangents, gl.STATIC_DRAW);
        gl.enableVertexAttribArray(ip.attributes.tangent);
        gl.vertexAttribPointer(ip.attributes.tangent, 4, gl.FLOAT, false, 0, 0);

        // Cache the transforms Float32Array on the mesh entry (same pattern as points _cachedPos).
        if (!mesh._cachedTransforms) {
          if (mesh.transforms instanceof Float32Array) {
            mesh._cachedTransforms = mesh.transforms;
          } else if (Array.isArray(mesh.transforms)) {
            mesh._cachedTransforms = new Float32Array(mesh.transforms);
          }
        }
        var transformData = mesh._cachedTransforms;
        if (!transformData) continue;

        // Upload instance transforms to GPU.
        gl.bindBuffer(gl.ARRAY_BUFFER, instanceTransformBuffer);
        gl.bufferData(gl.ARRAY_BUFFER, transformData, gl.STATIC_DRAW);

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
      gl.deleteBuffer(pointsPositionBuffer);
      gl.deleteBuffer(pointsSizeBuffer);
      gl.deleteBuffer(pointsColorBuffer);
      for (const record of computeParticleSystems.values()) {
        disposeComputeParticleSystemRecord(record);
      }
      computeParticleSystems.clear();
      lastComputeParticleTimeSeconds = null;
      gl.deleteBuffer(instanceTransformBuffer);
      gl.deleteBuffer(instanceVertexBuffer);
      gl.deleteBuffer(instanceNormalBuffer);
      gl.deleteBuffer(instanceUVBuffer);
      gl.deleteBuffer(instanceTangentBuffer);
      if (shadowState.buffer) gl.deleteBuffer(shadowState.buffer);

      for (const record of textureCache.values()) {
        if (record && record.texture) {
          gl.deleteTexture(record.texture);
        }
      }
      textureCache.clear();

      // Clean up shadow resources.
      for (var si = 0; si < shadowSlots.length; si++) {
        if (shadowSlots[si]) {
          gl.deleteFramebuffer(shadowSlots[si].framebuffer);
          gl.deleteTexture(shadowSlots[si].depthTexture);
          shadowSlots[si] = null;
        }
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
