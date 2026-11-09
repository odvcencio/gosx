// Builtin WebGL2 points shaders. The fragment shading lives in pointsShade,
// so the hard program (SCENE_POINTS_FRAGMENT_SOURCE) and the soft-particle
// program share it without editing shader text.

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
    // Fog
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
    // Exponential fog
    "    if (u_hasFog != 0) {",
    "        float dist = length(viewPos.xyz);",
    "        v_fogFactor = exp(-u_fogDensity * u_fogDensity * dist * dist);",
    "        v_fogFactor = clamp(v_fogFactor, 0.0, 1.0);",
    "    } else {",
    "        v_fogFactor = 1.0;",
    "    }",
    "}",
  ].join("\n");

  const SCENE_POINTS_SHADE_GLSL = [
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
    "vec4 pointsShade() {",
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
    // glow: pure radial gaussian falloff — soft gas cloud, no core, no streaks
    "        vec2 centered = gl_PointCoord - vec2(0.5);",
    "        float radial = length(centered) * 2.0;",
    "        if (radial > 1.0) discard;",
    "        float g = exp(-radial * radial * 3.5);",
    "        alpha = g;",
    "    }",
    "",
    // Apply fog
    "    if (u_hasFog != 0) {",
    "        color = mix(u_fogColor, color, v_fogFactor);",
    "    }",
    "",
    "    return vec4(color, alpha * v_color.a * u_opacity);",
    "}",
  ].join("\n");

  const SCENE_POINTS_FRAGMENT_SOURCE = [
    "#version 300 es",
    "precision highp float;",
    "precision highp int;",
    SCENE_POINTS_SHADE_GLSL,
    "void main() { fragColor = pointsShade(); }",
  ].join("\n");

  // sceneWebGLCachePointArrays converts a points entry's positions, sizes and
  // colors to Float32Array once. Positions, sizes and colors are static for
  // authored points; compute particles arrive already cached.
  function sceneWebGLCachePointArrays(entry: any, count: number) {
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
  }
