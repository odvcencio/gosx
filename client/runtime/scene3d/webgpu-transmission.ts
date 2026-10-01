// Screen-space volume transmission. The opaque frame is copied into a distinct
// texture before glass draws, so it never samples its own render attachment.
const WGSL_TRANSMISSION = `
@group(0) @binding(15) var transmissionScene: texture_2d<f32>;
@group(0) @binding(16) var transmissionSampler: sampler;
@group(0) @binding(17) var<uniform> transmissionCapture: vec4f;
fn transmissionEnvironment(ray: vec3f, roughness: f32) -> vec3f {
    let d = rotateEnvY(ray, env.envRotation);
    if (env.hasIBL != 0u) {
        return textureSampleLevel(iblRadiance, iblSampler, d, roughness * f32(max(env.radianceMipLevels, 1u) - 1u)).rgb * env.envIntensity;
    }
    if (env.hasEnvMap != 0u) {
        return textureSampleLevel(envMapTex, envMapSampler, envEquirectUV(d), 0.0).rgb * env.envIntensity;
    }
    let hemi = clamp(ray.y * 0.5 + 0.5, 0.0, 1.0);
    return env.ambientColor * env.ambientIntensity + env.skyColor * env.skyIntensity * hemi + env.groundColor * env.groundIntensity * (1.0 - hemi);
}
fn volumeTransmission(P: vec3f, N: vec3f, V: vec3f, roughness: f32) -> vec3f {
    let ior = material.volume.y;
    if (ior == 0.0) { return vec3f(0.0); }
    let ray = refract(-V, N, 1.0 / ior);
    if (dot(ray, ray) < 0.0001) { return vec3f(0.0); }
    let pathLength = material.volume.x;
    var light = transmissionEnvironment(ray, roughness);
    if (transmissionCapture.y > 0.5) {
        let exit = frame.projMatrix * frame.viewMatrix * vec4f(P + ray * pathLength, 1.0);
        if (exit.w > 0.0 && exit.z >= 0.0) {
            let uv = exit.xy / exit.w * vec2f(0.5, -0.5) + vec2f(0.5);
            let edge = min(min(uv.x, uv.y), min(1.0 - uv.x, 1.0 - uv.y));
            let scene = textureSampleLevel(transmissionScene, transmissionSampler, clamp(uv, vec2f(0.0), vec2f(1.0)), roughness * transmissionCapture.x).rgb;
            light = mix(light, scene, smoothstep(0.0, 0.025, edge));
        }
    }
    // Exact black stays black for a finite optical path; zero path is neutral.
    if (pathLength > 0.0 && material.volume.z > 0.0) {
        light *= pow(material.attenuationColor, vec3f(pathLength * material.volume.z));
    }
    return light;
}
`;

const WGSL_TRANSMISSION_COPY = `
@group(0) @binding(0) var source: texture_2d<f32>;
@group(0) @binding(1) var sourceSampler: sampler;
struct Out { @builtin(position) pos: vec4f, @location(0) uv: vec2f };
@vertex fn vs(@builtin(vertex_index) i: u32) -> Out {
    let p = vec2f(f32((i << 1u) & 2u), f32(i & 2u));
    var o: Out; o.pos = vec4f(p * 2.0 - 1.0, 0.0, 1.0); o.uv = vec2f(p.x, 1.0 - p.y); return o;
}
@fragment fn fs(o: Out) -> @location(0) vec4f {
    return textureSampleLevel(source, sourceSampler, o.uv, 0.0);
}
`;

/** @param {*} device @returns {*} */
function wgpuCreateTransmissionResources(device) {
    var sampler = device.createSampler({ minFilter: "linear", magFilter: "linear", mipmapFilter: "linear" });
    var uniform = device.createBuffer({ label: "gosx-transmission-capture", size: 16, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
    var texture = null, view = null, views = [], bindings = [], copyBinding = null, sourceView = null;
    var width = 0, height = 0, levels = 0, format = "", pipeline = null;
    var module = device.createShaderModule({ label: "gosx-transmission-copy", code: WGSL_TRANSMISSION_COPY });
    var layout = device.createBindGroupLayout({ entries: [
        { binding: 0, visibility: GPUShaderStage.FRAGMENT, texture: {} },
        { binding: 1, visibility: GPUShaderStage.FRAGMENT, sampler: {} },
    ] });
    /** @param {*} input */
    function bind(input) {
        return device.createBindGroup({ layout: layout, entries: [{ binding: 0, resource: input }, { binding: 1, resource: sampler }] });
    }
    function release() {
        if (texture) texture.destroy();
        texture = null; view = null; views = []; bindings = []; copyBinding = null; sourceView = null;
    }
    return {
        sampler: sampler, uniform: uniform,
        /** @param {*} w @param {*} h @param {*} fmt @param {*} settings */
        prepare: function(w, h, fmt, settings) {
            var count = Math.min(settings.levels, 1 + Math.floor(Math.log2(Math.max(w, h))));
            if (!settings.screen) { release(); return null; }
            if (!texture || w !== width || h !== height || fmt !== format || count !== levels) {
                release(); width = w; height = h; levels = count;
                if (!pipeline || fmt !== format) {
                    pipeline = device.createRenderPipeline({ label: "gosx-transmission-mips", layout: device.createPipelineLayout({ bindGroupLayouts: [layout] }),
                        vertex: { module: module, entryPoint: "vs" }, fragment: { module: module, entryPoint: "fs", targets: [{ format: fmt }] }, primitive: { topology: "triangle-list" } });
                }
                format = fmt;
                texture = device.createTexture({ label: "gosx-transmission-opaque", size: [w, h], format: fmt, mipLevelCount: levels,
                    usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.RENDER_ATTACHMENT });
                view = texture.createView();
                for (var i = 0; i < levels; i++) {
                    views.push(texture.createView({ baseMipLevel: i, mipLevelCount: 1 }));
                    if (i > 0) bindings.push(bind(views[i - 1]));
                }
            }
            device.queue.writeBuffer(uniform, 0, new Float32Array([levels - 1, 1, 0, 0]));
            return view;
        },
        /** @param {*} encoder @param {*} input */
        capture: function(encoder, input) {
            if (!texture || !input) return;
            if (sourceView !== input) { sourceView = input; copyBinding = bind(input); }
            for (var i = 0; i < levels; i++) {
                var pass = encoder.beginRenderPass({ label: "gosx-transmission-mip-" + i,
                    colorAttachments: [{ view: views[i], loadOp: "clear", storeOp: "store" }] });
                pass.setPipeline(pipeline); pass.setBindGroup(0, i === 0 ? copyBinding : bindings[i - 1]); pass.draw(3); pass.end();
            }
        },
        fallback: function() { device.queue.writeBuffer(uniform, 0, new Float32Array(4)); },
        dispose: function() { release(); uniform.destroy(); },
    };
}
