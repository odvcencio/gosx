// Depth-backed post effects. Only the WebGL2 backend loads this module.
// View reconstruction supports the engine's perspective and orthographic cameras.
const SCENE_POST_VIEW_POSITION_GLSL = `
uniform sampler2D u_depthTexture;
uniform mat4 u_projection;
vec3 postViewPosition(vec2 uv, float depth) {
    vec3 ndc = vec3(uv * 2.0 - 1.0, depth * 2.0 - 1.0);
    float z = (u_projection[3][2] - ndc.z * u_projection[3][3]) /
              (ndc.z * u_projection[2][3] - u_projection[2][2]);
    float w = u_projection[2][3] * z + u_projection[3][3];
    return vec3((ndc.xy * w - u_projection[2].xy * z - u_projection[3].xy) /
                vec2(u_projection[0][0], u_projection[1][1]), z);
}
vec3 postViewNormal(vec2 uv, vec3 p) {
    vec2 t = 1.0 / vec2(textureSize(u_depthTexture, 0));
    vec3 l = postViewPosition(uv - vec2(t.x, 0), texture(u_depthTexture, uv - vec2(t.x, 0)).r);
    vec3 r = postViewPosition(uv + vec2(t.x, 0), texture(u_depthTexture, uv + vec2(t.x, 0)).r);
    vec3 b = postViewPosition(uv - vec2(0, t.y), texture(u_depthTexture, uv - vec2(0, t.y)).r);
    vec3 a = postViewPosition(uv + vec2(0, t.y), texture(u_depthTexture, uv + vec2(0, t.y)).r);
    vec3 dx = abs(r.z - p.z) < abs(p.z - l.z) ? r - p : p - l;
    vec3 dy = abs(a.z - p.z) < abs(p.z - b.z) ? a - p : p - b;
    vec3 n = cross(dx, dy);
    n /= max(length(n), 0.000001);
    return dot(n, p) > 0.0 ? -n : n;
}`;

const SCENE_POST_SSAO_SOURCE = `#version 300 es
precision highp float;
precision highp sampler2D;
in vec2 v_uv;
uniform sampler2D u_texture;
uniform float u_radius;
uniform float u_intensity;
uniform float u_bias;
out vec4 fragColor;
${SCENE_POST_VIEW_POSITION_GLSL}
void main() {
    vec4 color = texture(u_texture, v_uv);
    float depth = texture(u_depthTexture, v_uv).r;
    if (depth >= 0.999999) { fragColor = color; return; }
    vec3 p = postViewPosition(v_uv, depth);
    vec3 n = postViewNormal(v_uv, p);
    vec2 texel = 1.0 / vec2(textureSize(u_depthTexture, 0));
    float radius = clamp(u_radius, 1.0, 64.0);
    float viewRadius = abs((u_projection[2][3] * p.z + u_projection[3][3]) *
                           texel.y * radius * 2.0 / u_projection[1][1]);
    float occlusion = 0.0;
    for (int i = 0; i < 12; i++) {
        float angle = float(i) * 2.39996323;
        vec2 uv = v_uv + vec2(cos(angle), sin(angle)) * texel * radius * sqrt((float(i) + 0.5) / 12.0);
        if (any(lessThan(uv, vec2(0))) || any(greaterThan(uv, vec2(1)))) continue;
        float d = texture(u_depthTexture, uv).r;
        if (d >= 0.999999) continue;
        vec3 delta = postViewPosition(uv, d) - p;
        float distance = length(delta);
        float horizon = max(0.0, dot(n, delta) - u_bias) / max(distance, 0.000001);
        float range = 1.0 - smoothstep(viewRadius, viewRadius * 3.0, distance);
        occlusion += horizon * range;
    }
    float visibility = clamp(1.0 - occlusion * clamp(u_intensity, 0.0, 2.0) / 6.0, 0.0, 1.0);
    fragColor = vec4(color.rgb * visibility, color.a);
}`;

// A sampled depth attachment must never belong to the active draw framebuffer.
function sceneWebGLPostReadsDepth(kind: string) {
    return kind === SCENE_POST_SSAO || kind === SCENE_POST_DOF || kind === SCENE_POST_CUSTOM_POST || kind === "atmosphere";
}
