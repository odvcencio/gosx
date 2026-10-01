// Contact shadow rays share the WebGL2 view-space contract. WebGPU UVs point down.
const WGSL_POST_CONTACT_SHADOWS_FRAGMENT = `
struct ContactParams { projection: mat4x4f, light: vec4f, settings: vec4f };
@group(0) @binding(0) var inputTex: texture_2d<f32>;
@group(0) @binding(1) var inputSamp: sampler;
@group(0) @binding(2) var depthTex: texture_depth_2d;
@group(0) @binding(3) var<uniform> params: ContactParams;
fn depthAt(uv: vec2f) -> f32 {
    let dims = vec2f(textureDimensions(depthTex));
    return textureLoad(depthTex, vec2i(clamp(uv * dims, vec2f(0), dims - vec2f(1))), 0);
}
fn viewAt(uv: vec2f, d: f32) -> vec3f {
    let m = params.projection;
    let ndc = vec3f(uv.x * 2.0 - 1.0, 1.0 - uv.y * 2.0, d * 2.0 - 1.0);
    let z = (m[3].z - ndc.z * m[3].w) / (ndc.z * m[2].w - m[2].z);
    let w = m[2].w * z + m[3].w;
    return vec3f((ndc.xy * w - m[2].xy * z - m[3].xy) / vec2f(m[0].x, m[1].y), z);
}
fn normalAt(uv: vec2f, p: vec3f) -> vec3f {
    let t = 1.0 / vec2f(textureDimensions(depthTex));
    let l = viewAt(uv - vec2f(t.x, 0), depthAt(uv - vec2f(t.x, 0)));
    let r = viewAt(uv + vec2f(t.x, 0), depthAt(uv + vec2f(t.x, 0)));
    let a = viewAt(uv - vec2f(0, t.y), depthAt(uv - vec2f(0, t.y)));
    let b = viewAt(uv + vec2f(0, t.y), depthAt(uv + vec2f(0, t.y)));
    let dx = select(p - l, r - p, abs(r.z - p.z) < abs(p.z - l.z));
    let dy = select(p - b, a - p, abs(a.z - p.z) < abs(p.z - b.z));
    var n = cross(dx, dy); n = n / max(length(n), 0.000001);
    return select(n, -n, dot(n, p) > 0.0);
}
@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {
    let color = textureSample(inputTex, inputSamp, uv);
    let depth = depthAt(uv);
    if (depth >= 0.999999) { return color; }
    let p = viewAt(uv, depth);
    let n = normalAt(uv, p);
    let light = normalize(params.light.xyz);
    let facing = max(0.0, dot(n, light));
    let distanceLimit = params.settings.x;
    let thickness = params.settings.y;
    let bias = params.settings.z;
    var shadow = 0.0;
    for (var i = 0; i < 16; i++) {
        let distance = distanceLimit * (f32(i) + 0.5) / 16.0;
        let q = p + n * bias + light * distance;
        let clip = params.projection * vec4f(q, 1);
        if (clip.w <= 0.0) { break; }
        let sampleUV = vec2f(clip.x / clip.w * 0.5 + 0.5, 0.5 - clip.y / clip.w * 0.5);
        if (any(sampleUV < vec2f(0)) || any(sampleUV > vec2f(1))) { break; }
        let d = depthAt(sampleUV);
        if (d >= 0.999999) { continue; }
        let delta = viewAt(sampleUV, d).z - q.z;
        let hit = smoothstep(bias, bias * 3.0, delta) * (1.0 - smoothstep(thickness * 0.5, thickness, delta));
        shadow = max(shadow, hit * (1.0 - smoothstep(0.0, distanceLimit, distance)));
    }
    return vec4f(color.rgb * (1.0 - shadow * params.settings.w * facing), color.a);
}`;

function sceneWebGPUContactUniforms(effect: any, camera: any, width: number, height: number, lights: any[]) {
    var view = scenePBRViewMatrix(camera);
    var projection = scenePBRProjectionMatrixForCamera(camera, width / Math.max(1, height));
    var sun = (lights || []).find(function(light) { return light.kind === "directional"; });
    var direction = effect.direction || (sun ? { x: sun.directionX, y: sun.directionY, z: sun.directionZ } : { x: 0.5, y: -1, z: 0.3 });
    var x = -sceneNumber(direction.x, 0), y = -sceneNumber(direction.y, 0), z = -sceneNumber(direction.z, 0);
    var length = Math.max(0.000001, Math.hypot(x, y, z));
    var data = new Float32Array(24); data.set(projection);
    data.set([(view[0] * x + view[4] * y + view[8] * z) / length,
        (view[1] * x + view[5] * y + view[9] * z) / length,
        (view[2] * x + view[6] * y + view[10] * z) / length, 0], 16);
    data.set([Math.max(0.01, Math.min(10, sceneNumber(effect.distance, 1))),
        Math.max(0.001, Math.min(2, sceneNumber(effect.thickness, 0.15))),
        Math.max(0.0001, Math.min(0.1, sceneNumber(effect.bias, 0.01))),
        Math.max(0, Math.min(1, sceneNumber(effect.intensity, 0.45)))], 20);
    return data;
}
