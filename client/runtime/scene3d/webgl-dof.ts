// WebGL2 depth of field: a single-pass circle-of-confusion gather.
//
// Each pixel's blur radius (its CoC, in pixels) grows with its distance from
// the focus plane: |depth - focus| * aperture * maxBlur, capped at maxBlur.
// The pass walks a golden-angle spiral out to maxBlur with a fixed tap count
// and keeps a tap only where that tap's own CoC reaches back to this pixel, so
// a blurred background never pulls in the sharp subject in front of it. A tap
// behind this pixel may spread at most twice this pixel's CoC, so a sharp
// foreground edge does not smear across the blurred background behind it.
const SCENE_POST_DOF_SOURCE = `#version 300 es
precision highp float;
in vec2 v_uv;
uniform sampler2D u_texture;
uniform sampler2D u_depthTexture;
uniform float u_focusDistance;
uniform float u_aperture;
uniform float u_maxBlur;
uniform float u_near;
uniform float u_far;
out vec4 fragColor;
const float GOLDEN_ANGLE = 2.39996323;
const int DOF_TAPS = 48;
float linearDepth(float depth) {
    float z = depth * 2.0 - 1.0;
    return (2.0 * u_near * u_far) / max(0.0001, u_far + u_near - z * (u_far - u_near));
}
float cocOf(float depth, float maxBlur) { return clamp(abs(depth - u_focusDistance) * u_aperture * maxBlur, 0.0, maxBlur); }
void main() {
    vec2 texel = 1.0 / vec2(textureSize(u_texture, 0));
    float maxBlur = clamp(u_maxBlur, 0.0, 64.0);
    float centerDepth = linearDepth(texture(u_depthTexture, v_uv).r);
    float centerCoC = cocOf(centerDepth, maxBlur);
    vec3 color = texture(u_texture, v_uv).rgb;
    float total = 1.0;
    // Squared radius grows by 2 * stride per tap, so DOF_TAPS taps reach maxBlur.
    float stride = maxBlur * maxBlur / float(2 * DOF_TAPS);
    float radius = stride, angle = 0.0;
    for (int i = 0; i < DOF_TAPS; i++) {
        if (radius >= maxBlur || maxBlur < 0.5) break;
        vec2 uv = v_uv + vec2(cos(angle), sin(angle)) * texel * radius;
        vec3 sampleColor = texture(u_texture, uv).rgb;
        float sampleDepth = linearDepth(texture(u_depthTexture, uv).r);
        float sampleCoC = cocOf(sampleDepth, maxBlur);
        if (sampleDepth > centerDepth) sampleCoC = min(sampleCoC, centerCoC * 2.0);
        color += mix(color / total, sampleColor, smoothstep(radius - 0.5, radius + 0.5, sampleCoC));
        total += 1.0;
        radius += stride / radius;
        angle += GOLDEN_ANGLE;
    }
    fragColor = vec4(color / total, 1.0);
}`;
