// WebGL2 volume transmission uses one spare sampler after the eight material
// maps, two shadow arrays and three IBL products (14 of the core 16 units).
const GLSL_TRANSMISSION = `
uniform sampler2D u_transmissionScene;
uniform vec2 u_transmissionCapture;
uniform vec4 u_volume;
uniform vec3 u_attenuationColor;
uniform mat4 u_projectionMatrix;
vec3 transmissionEnvironment(vec3 ray, float roughness) {
    vec3 d = rotateEnvY(ray, u_envRotation);
#if GOSX_HDR_IBL
    if (u_hasIBL) return textureLod(u_iblRadiance, d, roughness * u_iblRadianceMaxLod).rgb * u_envIntensity;
#endif
    if (u_hasEnvMap) return textureLod(u_envMap, envEquirectUV(d), roughness * u_envMapMaxLod).rgb * u_envIntensity;
    float hemi = clamp(ray.y * 0.5 + 0.5, 0.0, 1.0);
    return u_ambientColor * u_ambientIntensity + u_skyColor * u_skyIntensity * hemi + u_groundColor * u_groundIntensity * (1.0 - hemi);
}
vec3 volumeTransmission(vec3 P, vec3 N, vec3 V, float roughness) {
    if (u_volume.y == 0.0) return vec3(0.0);
    vec3 ray = refract(-V, N, 1.0 / u_volume.y);
    if (dot(ray, ray) < 0.0001) return vec3(0.0);
    float pathLength = u_volume.x;
    vec3 light = transmissionEnvironment(ray, roughness);
    if (u_transmissionCapture.y > 0.5) {
        vec4 exit = u_projectionMatrix * u_viewMatrix * vec4(P + ray * pathLength, 1.0);
        if (exit.w > 0.0 && exit.z >= -exit.w) {
            vec2 uv = exit.xy / exit.w * 0.5 + 0.5;
            float edge = min(min(uv.x, uv.y), min(1.0 - uv.x, 1.0 - uv.y));
            vec3 scene = textureLod(u_transmissionScene, clamp(uv, vec2(0.0), vec2(1.0)), roughness * u_transmissionCapture.x).rgb;
            light = mix(light, scene, smoothstep(0.0, 0.025, edge));
        }
    }
    if (pathLength > 0.0 && u_volume.z > 0.0) light *= pow(u_attenuationColor, vec3(pathLength * u_volume.z));
    return light;
}
`;

/** @param {*} gl @returns {*} */
function sceneCreateTransmissionWebGL(gl) {
    var texture = null, fbo = null, width = 0, height = 0, hdr = false, ready = false, maxLod = 0;
    var unit = SCENE_TEXTURE_UNIT_FIRST_SHARED + 5;
    var data = new Float32Array(8);
    function release() {
        if (texture) gl.deleteTexture(texture);
        if (fbo) gl.deleteFramebuffer(fbo);
        texture = null; fbo = null; ready = false;
    }
    return {
        /** @param {*} w @param {*} h @param {*} targetHDR @param {*} settings */
        prepare: function(w, h, targetHDR, settings) {
            ready = false;
            if (!settings.screen || scenePBRMaxTextureUnits(gl) <= unit) { release(); return false; }
            if (!texture || w !== width || h !== height || hdr !== targetHDR) {
                release(); width = w; height = h; hdr = targetHDR;
                texture = gl.createTexture(); gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, texture);
                gl.texImage2D(gl.TEXTURE_2D, 0, hdr ? gl.RGBA16F : gl.RGBA8, w, h, 0, gl.RGBA, hdr ? gl.HALF_FLOAT : gl.UNSIGNED_BYTE, null);
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR_MIPMAP_LINEAR);
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR);
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE);
                gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE);
                fbo = gl.createFramebuffer(); gl.bindFramebuffer(gl.FRAMEBUFFER, fbo);
                gl.framebufferTexture2D(gl.FRAMEBUFFER, gl.COLOR_ATTACHMENT0, gl.TEXTURE_2D, texture, 0);
                if (gl.checkFramebufferStatus(gl.FRAMEBUFFER) !== gl.FRAMEBUFFER_COMPLETE) { release(); return false; }
            }
            maxLod = Math.min(settings.levels - 1, Math.floor(Math.log2(Math.max(w, h))));
            gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, texture);
            gl.texParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAX_LEVEL, maxLod);
            return true;
        },
        /** @param {*} target */
        capture: function(target) {
            if (!fbo) return;
            gl.bindFramebuffer(gl.READ_FRAMEBUFFER, target.framebuffer);
            gl.bindFramebuffer(gl.DRAW_FRAMEBUFFER, fbo);
            gl.blitFramebuffer(0, 0, width, height, 0, 0, width, height, gl.COLOR_BUFFER_BIT, gl.NEAREST);
            gl.bindFramebuffer(gl.FRAMEBUFFER, target.framebuffer);
            gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, texture); gl.generateMipmap(gl.TEXTURE_2D);
            ready = true;
        },
        /** @param {*} uniforms @param {*} mat @param {*} view @param {*} proj @param {*} placeholder */
        upload: function(uniforms, mat, view, proj, placeholder) {
            data.set(sceneTransmissionVolume(mat));
            gl.uniform4fv(uniforms.volume, data.subarray(0, 4));
            gl.uniform3fv(uniforms.attenuationColor, data.subarray(4, 7));
            gl.uniform2f(uniforms.transmissionCapture, maxLod, ready ? 1 : 0);
            gl.uniformMatrix4fv(uniforms.viewMatrix, false, view); gl.uniformMatrix4fv(uniforms.projectionMatrix, false, proj);
            gl.activeTexture(gl.TEXTURE0 + unit); gl.bindTexture(gl.TEXTURE_2D, ready ? texture : placeholder);
            gl.uniform1i(uniforms.transmissionScene, unit);
        },
        dispose: release,
    };
}
