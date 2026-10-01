// WebGL2 temporal resolve. History stores color and depth in two capped targets.
const SCENE_POST_TAA_SOURCE = `#version 300 es
precision highp float;
precision highp sampler2D;
in vec2 v_uv;
uniform sampler2D u_texture;
uniform sampler2D u_history;
uniform sampler2D u_historyDepth;
uniform mat4 u_inverseView;
uniform mat4 u_previousView;
uniform mat4 u_previousProjection;
uniform vec4 u_temporalParams;
out vec4 fragColor;
${SCENE_POST_VIEW_POSITION_GLSL}
vec3 toYCoCg(vec3 c) { return vec3(dot(c, vec3(0.25, 0.5, 0.25)), c.r * 0.5 - c.b * 0.5, -c.r * 0.25 + c.g * 0.5 - c.b * 0.25); }
vec3 fromYCoCg(vec3 c) { return vec3(c.x + c.y - c.z, c.x + c.z, c.x - c.y - c.z); }
void main() {
    vec4 current = texture(u_texture, v_uv);
    float depth = texture(u_depthTexture, v_uv).r;
    if (u_temporalParams.w < 0.5 || depth >= 0.999999) { fragColor = current; return; }
    vec4 world = u_inverseView * vec4(postViewPosition(v_uv, depth), 1);
    vec4 previousPosition = u_previousView * world;
    vec4 clip = u_previousProjection * previousPosition;
    if (clip.w <= 0.0) { fragColor = current; return; }
    vec2 uv = clip.xy / clip.w * 0.5 + 0.5;
    if (any(lessThan(uv, vec2(0))) || any(greaterThan(uv, vec2(1)))) { fragColor = current; return; }
    float oldDepth = texture(u_historyDepth, uv).r * 2.0 - 1.0;
    float oldZ = (u_previousProjection[3][2] - oldDepth * u_previousProjection[3][3]) /
                 (oldDepth * u_previousProjection[2][3] - u_previousProjection[2][2]);
    if (abs(oldZ - previousPosition.z) > u_temporalParams.z * max(1.0, abs(previousPosition.z))) { fragColor = current; return; }
    vec2 texel = 1.0 / vec2(textureSize(u_texture, 0));
    vec3 low = vec3(1e20), high = vec3(-1e20), mean = vec3(0), square = vec3(0);
    for (int y = -1; y <= 1; y++) for (int x = -1; x <= 1; x++) {
        vec3 c = toYCoCg(texture(u_texture, v_uv + vec2(x, y) * texel).rgb);
        low = min(low, c); high = max(high, c); mean += c / 9.0; square += c * c / 9.0;
    }
    vec3 sigma = sqrt(max(vec3(0), square - mean * mean));
    low = max(low, mean - sigma * u_temporalParams.y);
    high = min(high, mean + sigma * u_temporalParams.y);
    vec3 history = fromYCoCg(clamp(toYCoCg(texture(u_history, uv).rgb), low, high));
    float motion = length((uv - v_uv) / texel);
    float change = abs(toYCoCg(history).x - toYCoCg(current.rgb).x);
    float weight = u_temporalParams.x * exp(-motion * 0.05) / (1.0 + change * 8.0);
    fragColor = vec4(mix(current.rgb, history, weight), current.a);
}`;
function sceneTemporalHalton(index: number, base: number) {
    var value = 0, fraction = 1;
    while (index > 0) { fraction /= base; value += fraction * (index % base); index = Math.floor(index / base); }
    return value;
}
function sceneTemporalJitter(projection: Float32Array, width: number, height: number, index: number) {
    var x = (sceneTemporalHalton(index % 8 + 1, 2) - 0.5) * 2 / width;
    var y = (sceneTemporalHalton(index % 8 + 1, 3) - 0.5) * 2 / height;
    // Apply a clip-space translation to either projection, without changing depth.
    for (var column = 0; column < 4; column++) {
        projection[column * 4] += x * projection[column * 4 + 3];
        projection[column * 4 + 1] += y * projection[column * 4 + 3];
    }
}
function createSceneTemporalHistory(gl: any, quad: any) {
    var targets: any[] | null = null, program: any = null, failed = false, index = 0, valid = false;
    var width = 0, height = 0, stamp = "", lastTime = 0;
    var previousView = new Float32Array(16), previousProjection = new Float32Array(16);
    var previousInverseView = new Float32Array(16), inverseView: Float32Array | null = null;
    function release() {
        if (targets) { disposeScenePostFBO(gl, targets[0]); disposeScenePostFBO(gl, targets[1]); }
        targets = null; valid = false; index = 0;
    }
    function prepare(effects: any[], size: { width: number; height: number }, projection: Float32Array, view: Float32Array, canJitter: boolean, lights: any) {
        var effect = effects.find(e => e.kind === "taa");
        if (!effect || !canJitter || failed) { release(); return false; }
        if (!gl.getExtension("EXT_color_buffer_float") || !gl.blitFramebuffer || !gl.checkFramebufferStatus) return false;
        if (!program) program = createScenePostProgram(gl, SCENE_POST_TAA_SOURCE);
        if (!program) { failed = true; return false; }
        if (!targets || width !== size.width || height !== size.height) {
            release(); width = size.width; height = size.height;
            targets = [createScenePostFBO(gl, width, height, true), createScenePostFBO(gl, width, height, true)];
            for (var i = 0; i < 2; i++) {
                gl.bindFramebuffer(gl.FRAMEBUFFER, targets[i].fbo);
                if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) { release(); failed = true; return false; }
            }
        }
        // Snapshot complete upstream wire descriptors, including nested and future parameters.
        var upstream = effects.slice(0, effects.indexOf(effect) + 1),
            nextStamp = JSON.stringify([upstream, upstream.some(e => e.kind === "contactShadows") ? lights : null]);
        // Custom passes can read live clock/DOM auto-uniforms outside their descriptor.
        if (upstream.some(e => e.kind === SCENE_POST_CUSTOM_POST)) valid = false;
        var now = performance.now(); inverseView = sceneInvertOrthonormalView(view);
        var cut = Math.hypot(inverseView[12] - previousInverseView[12], inverseView[13] - previousInverseView[13], inverseView[14] - previousInverseView[14]) > 2;
        var turn = inverseView[8] * previousInverseView[8] + inverseView[9] * previousInverseView[9] + inverseView[10] * previousInverseView[10] < 0.5;
        var projectionChanged = Math.abs(projection[0] - previousProjection[0]) > 0.0001 || Math.abs(projection[5] - previousProjection[5]) > 0.0001 || projection[10] !== previousProjection[10] || projection[11] !== previousProjection[11];
        if (stamp !== nextStamp || now - lastTime > 250 || cut || turn || projectionChanged) valid = false;
        stamp = nextStamp; lastTime = now;
        sceneTemporalJitter(projection, width, height, index);
        return true;
    }
    function resolve(input: any, source: any, effect: any, frame: any) {
        if (!targets || !program || !frame) return input;
        var output = targets[index % 2], history = targets[(index + 1) % 2];
        for (var unit = 0; unit < 4; unit++) { gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, null); }
        gl.bindFramebuffer(gl.FRAMEBUFFER, output.fbo); gl.viewport(0, 0, width, height); gl.useProgram(program.program);
        var textures = [input, source.depthTex, history.colorTex, history.depthTex], names = ["u_texture", "u_depthTexture", "u_history", "u_historyDepth"];
        for (var t = 0; t < 4; t++) { gl.activeTexture(gl.TEXTURE0 + t); gl.bindTexture(gl.TEXTURE_2D, textures[t]); gl.uniform1i(gl.getUniformLocation(program.program, names[t]), t); }
        var matrices = [frame.projection, inverseView, previousView, previousProjection];
        var uniforms = ["u_projection", "u_inverseView", "u_previousView", "u_previousProjection"];
        for (var m = 0; m < 4; m++) gl.uniformMatrix4fv(gl.getUniformLocation(program.program, uniforms[m]), false, matrices[m]);
        gl.uniform4f(gl.getUniformLocation(program.program, "u_temporalParams"), Math.max(0, Math.min(0.95, sceneNumber(effect.historyWeight, 0.9))), Math.max(0.5, Math.min(3, sceneNumber(effect.clampGamma, 1))), Math.max(0.0001, Math.min(0.1, sceneNumber(effect.depthThreshold, 0.01))), valid ? 1 : 0);
        drawSceneFullscreenQuad(gl, quad.vao);
        // Save exactly this frame's depth alongside its resolved color.
        for (var u = 0; u < 4; u++) { gl.activeTexture(gl.TEXTURE0 + u); gl.bindTexture(gl.TEXTURE_2D, null); }
        gl.bindFramebuffer(gl.READ_FRAMEBUFFER, source.fbo); gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, output.fbo);
        gl.blitFramebuffer(0, 0, width, height, 0, 0, width, height, gl.DEPTH_BUFFER_BIT, gl.NEAREST);
        previousView.set(frame.view); previousProjection.set(frame.projection); previousInverseView.set(inverseView);
        valid = true; index++;
        return output.colorTex;
    }
    return { prepare: prepare, resolve: resolve, reset: release,
        dispose: function() { release(); if (program) { gl.deleteProgram(program.program); gl.deleteShader(program.vertexShader); gl.deleteShader(program.fragmentShader); } program = null; },
        valid: function() { return valid; } };
}
