// Bounded screen-space contact rays supplement the authored shadow maps.
// Off-screen casters still rely on those maps; this pass only adds local grounding.
const SCENE_POST_CONTACT_SHADOWS_SOURCE = `#version 300 es
precision highp float;
precision highp sampler2D;
in vec2 v_uv;
uniform sampler2D u_texture;
uniform vec3 u_lightDirection;
uniform vec4 u_contactParams;
out vec4 fragColor;
${SCENE_POST_VIEW_POSITION_GLSL}
void main() {
    vec4 color = texture(u_texture, v_uv);
    float depth = texture(u_depthTexture, v_uv).r;
    if (depth >= 0.999999) { fragColor = color; return; }
    vec3 p = postViewPosition(v_uv, depth);
    vec3 n = postViewNormal(v_uv, p);
    vec3 light = normalize(u_lightDirection);
    float facing = max(0.0, dot(n, light));
    if (facing <= 0.0) { fragColor = color; return; }
    float shadow = 0.0;
    float distanceLimit = u_contactParams.x;
    float thickness = u_contactParams.y;
    float bias = u_contactParams.z;
    for (int i = 0; i < 16; i++) {
        float distance = distanceLimit * (float(i) + 0.5) / 16.0;
        vec3 q = p + n * bias + light * distance;
        vec4 clip = u_projection * vec4(q, 1);
        if (clip.w <= 0.0) break;
        vec2 uv = clip.xy / clip.w * 0.5 + 0.5;
        if (any(lessThan(uv, vec2(0))) || any(greaterThan(uv, vec2(1)))) break;
        float d = texture(u_depthTexture, uv).r;
        if (d >= 0.999999) continue;
        float delta = postViewPosition(uv, d).z - q.z;
        // Smooth thickness and distance fades soften contacts without a blur
        // that would leak through object silhouettes or cost another target.
        float hit = smoothstep(bias, bias * 3.0, delta) *
                    (1.0 - smoothstep(thickness * 0.5, thickness, delta));
        shadow = max(shadow, hit * (1.0 - smoothstep(0.0, distanceLimit, distance)));
    }
    fragColor = vec4(color.rgb * (1.0 - shadow * u_contactParams.w * facing), color.a);
}`;

function sceneContactShadowLight(effect: any, lights: any[], view: Float32Array) {
    var direction = effect.direction;
    if (!direction) {
        var sun = (lights || []).find(function(light) { return light.kind === "directional"; });
        direction = sun ? { x: sun.directionX, y: sun.directionY, z: sun.directionZ } : { x: 0.5, y: -1, z: 0.3 };
    }
    var x = -sceneNumber(direction.x, 0.5), y = -sceneNumber(direction.y, -1), z = -sceneNumber(direction.z, 0.3);
    var length = Math.hypot(x, y, z);
    if (length < 0.000001) return [0, 1, 0];
    return [(view[0] * x + view[4] * y + view[8] * z) / length,
        (view[1] * x + view[5] * y + view[9] * z) / length,
        (view[2] * x + view[6] * y + view[10] * z) / length];
}

function sceneContactShadowParams(effect: any) {
    return [Math.max(0.01, Math.min(10, sceneNumber(effect.distance, 1))),
        Math.max(0.001, Math.min(2, sceneNumber(effect.thickness, 0.15))),
        Math.max(0.0001, Math.min(0.1, sceneNumber(effect.bias, 0.01))),
        Math.max(0, Math.min(1, sceneNumber(effect.intensity, 0.45)))];
}
