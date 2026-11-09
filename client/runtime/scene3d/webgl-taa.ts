// WebGL2 temporal resolve. History stores color and depth in two capped targets.
// Silhouettes use the closest neighborhood depth and remove jitter from history UVs.
// Clear neighborhoods and changed surface depth reject history. The resolve
// shader and the mover velocity pass live in the TAA motion module.
function sceneTemporalHalton(index: number, base: number) {
    var value = 0, fraction = 1;
    while (index > 0) { fraction /= base; value += fraction * (index % base); index = Math.floor(index / base); }
    return value;
}
function sceneTemporalJitter(projection: Float32Array, width: number, height: number, index: number, jitter: Float32Array) {
    var x = (sceneTemporalHalton(index % 8 + 1, 2) - 0.5) * 2 / width;
    var y = (sceneTemporalHalton(index % 8 + 1, 3) - 0.5) * 2 / height;
    jitter[0] = x * 0.5; jitter[1] = y * 0.5;
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
    var jitter = new Float32Array(4), motion = createSceneWebGLMotionVectors(gl), currentProjection: any = null, currentView: any = null;
    function release() {
        if (targets) { disposeScenePostFBO(gl, targets[0]); disposeScenePostFBO(gl, targets[1]); }
        targets = null; valid = false; index = 0; motion.reset();
    }
    function prepare(effects: any[], size: { width: number; height: number }, projection: Float32Array, view: Float32Array, canJitter: boolean, lights: any) {
        var effect = effects.find(e => e.kind === "taa");
        var upstream = effect ? effects.slice(0, effects.indexOf(effect) + 1) : [];
        // Live custom inputs cannot seed reusable history; spatial AA avoids jitter.
        if (!effect || !canJitter || failed || upstream.some(e => e.kind === SCENE_POST_CUSTOM_POST)) { release(); return false; }
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
        var nextStamp = JSON.stringify([upstream, upstream.some(e => e.kind === "contactShadows") ? lights : null]);
        var now = performance.now(); inverseView = sceneInvertOrthonormalView(view);
        var cut = Math.hypot(inverseView[12] - previousInverseView[12], inverseView[13] - previousInverseView[13], inverseView[14] - previousInverseView[14]) > 2;
        var turn = inverseView[8] * previousInverseView[8] + inverseView[9] * previousInverseView[9] + inverseView[10] * previousInverseView[10] < 0.5;
        var projectionChanged = Math.abs(projection[0] - previousProjection[0]) > 0.0001 || Math.abs(projection[5] - previousProjection[5]) > 0.0001 || projection[10] !== previousProjection[10] || projection[11] !== previousProjection[11];
        if (stamp !== nextStamp || now - lastTime > 250 || cut || turn || projectionChanged) valid = false;
        stamp = nextStamp; lastTime = now;
        // An invalid frame uses unjittered FXAA while resolve seeds fresh history.
        jitter[0] = 0; jitter[1] = 0;
        if (valid) sceneTemporalJitter(projection, width, height, index, jitter);
        currentProjection = projection; currentView = view; motion.begin();
        return valid;
    }
    // drawMotion runs after the scene draws: water regions, mover records, and the velocity pass on frames with history.
    function drawMotion(bundle: any, helpers: any) { motion.update(bundle, valid ? helpers : null, currentProjection, currentView, previousProjection, previousView, [width, height]); }
    function resolve(input: any, source: any, effect: any, frame: any) {
        if (!targets || !program || !frame) return input;
        var output = targets[index % 2], history = targets[(index + 1) % 2];
        for (var unit = 0; unit < 5; unit++) { gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, null); }
        gl.bindFramebuffer(gl.FRAMEBUFFER, output.fbo); gl.viewport(0, 0, width, height); gl.useProgram(program.program);
        var textures = [input, source.depthTex, history.colorTex, history.depthTex], names = ["u_texture", "u_depthTexture", "u_history", "u_historyDepth"];
        for (var t = 0; t < 4; t++) { gl.activeTexture(gl.TEXTURE0 + t); gl.bindTexture(gl.TEXTURE_2D, textures[t]); gl.uniform1i(sceneWaterUniformLocation(gl, program.program, names[t]), t); }
        var matrices = [frame.projection, inverseView, previousView, previousProjection];
        var uniforms = ["u_projection", "u_inverseView", "u_previousView", "u_previousProjection"];
        for (var m = 0; m < 4; m++) gl.uniformMatrix4fv(sceneWaterUniformLocation(gl, program.program, uniforms[m]), false, matrices[m]);
        gl.uniform4f(sceneWaterUniformLocation(gl, program.program, "u_temporalParams"), Math.max(0, Math.min(0.95, sceneNumber(effect.historyWeight, 0.9))), Math.max(0.5, Math.min(3, sceneNumber(effect.clampGamma, 1))), Math.max(0.0001, Math.min(0.1, sceneNumber(effect.depthThreshold, 0.01))), valid ? 1 : 0);
        gl.uniform4f(sceneWaterUniformLocation(gl, program.program, "u_temporalJitter"), jitter[0], jitter[1], jitter[2], jitter[3]);
        motion.bindResolve(program.program);
        drawSceneFullscreenQuad(gl, quad.vao);
        // Save exactly this frame's depth alongside its resolved color.
        for (var u = 0; u < 5; u++) { gl.activeTexture(gl.TEXTURE0 + u); gl.bindTexture(gl.TEXTURE_2D, null); }
        gl.bindFramebuffer(gl.READ_FRAMEBUFFER, source.fbo); gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, output.fbo);
        gl.blitFramebuffer(0, 0, width, height, 0, 0, width, height, gl.DEPTH_BUFFER_BIT, gl.NEAREST);
        previousView.set(frame.view); previousProjection.set(frame.projection); previousInverseView.set(inverseView);
        jitter[2] = jitter[0]; jitter[3] = jitter[1]; valid = true; index++;
        return output.colorTex;
    }
    return { prepare: prepare, motion: drawMotion, resolve: resolve, reset: release, ready: function() { return !!targets && !failed; },
        dispose: function() { release(); motion.dispose(); if (program) { gl.deleteProgram(program.program); gl.deleteShader(program.vertexShader); gl.deleteShader(program.fragmentShader); } program = null; },
        valid: function() { return valid; } };
}
