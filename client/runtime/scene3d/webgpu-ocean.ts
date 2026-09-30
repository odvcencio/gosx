// The WebGPU open-ocean pass for Environment.Ocean.
//
// The WGSL twin of the WebGL2 ocean pass: one draw of a camera-centred polar grid
// built from vertex_index, Gerstner swell, sky reflection, GGX sun glint,
// crest scatter, whitecaps, and bathymetry-aware shallows, foam and run-up.
// It draws inside the main pass after opaque geometry with premultiplied
// alpha. Uniform layout: sceneOceanUniformData (16c).

const WGSL_SCENE_OCEAN = [
  "struct Ocean { viewProj: mat4x4f, p: array<vec4f, 35> };",
  "@group(0) @binding(0) var<uniform> ocean: Ocean;",
  "@group(0) @binding(1) var oceanSampler: sampler;",
  "@group(0) @binding(2) var bathymetry: texture_2d<f32>;",
  "fn oceanFloor(xz: vec2f) -> f32 {",
  "  if (ocean.p[6].z < 0.5) { return -1e4; }",
  "  let uv = clamp((xz - ocean.p[5].xy) / (ocean.p[5].zw - ocean.p[5].xy), vec2f(0.0), vec2f(1.0));",
  "  let r = textureSampleLevel(bathymetry, oceanSampler, uv, 0.0).r;",
  "  if (ocean.p[6].z > 1.5) { let s = 2.0 * r - 1.0; return sign(s) * s * s * ocean.p[6].y; }",
  "  return mix(ocean.p[6].x, ocean.p[6].y, r);",
  "}",
  "struct OceanVertex { @builtin(position) position: vec4f, @location(0) world: vec3f, @location(1) normal: vec3f,",
  "  @location(2) jacobian: f32, @location(3) crest: f32, @location(4) depth0: f32 };",
  "@vertex fn vertexMain(@builtin(vertex_index) id: u32) -> OceanVertex {",
  "  let segs = u32(ocean.p[8].y);",
  "  let quad = id / 6u; let corner = id - quad * 6u;",
  "  var ring = quad / segs; var seg = quad - ring * segs;",
  "  if (corner == 1u || corner == 3u || corner == 4u) { ring += 1u; }",
  "  if (corner == 2u || corner == 4u || corner == 5u) { seg += 1u; }",
  "  let r = ocean.p[8].z * (exp(ocean.p[8].w * f32(ring)) - 1.0);",
  "  let a = f32(seg) / f32(segs) * 6.28318530718;",
  "  let xz = floor(ocean.p[7].xz * 0.5) * 2.0 + r * vec2f(cos(a), sin(a));",
  "  let level = ocean.p[0].x; let t = ocean.p[0].z;",
  "  let depth0 = level - oceanFloor(xz);",
  "  var p = vec3f(xz.x, level, xz.y); var dx = vec3f(1.0, 0.0, 0.0); var dz = vec3f(0.0, 0.0, 1.0);",
  "  var amp = 0.0;",
  "  let waves = i32(ocean.p[6].w);",
  "  for (var i = 0; i < 6; i++) {",
  "    if (i >= waves) { break; }",
  "    let w0 = ocean.p[9 + i * 2]; let w1 = ocean.p[10 + i * 2];",
  "    let shoal = smoothstep(0.0, 1.2, depth0 * w0.z);",
  "    let A = w1.x * shoal; let QA = w1.y * shoal;",
  "    let th = w0.z * dot(w0.xy, xz) - w0.w * t + w1.z;",
  "    let s = sin(th); let c = cos(th); let kA = w0.z * A; let kQA = w0.z * QA;",
  "    p += vec3f(QA * w0.x * c, A * s, QA * w0.y * c);",
  "    dx += vec3f(-kQA * w0.x * w0.x * s, kA * w0.x * c, -kQA * w0.x * w0.y * s);",
  "    dz += vec3f(-kQA * w0.x * w0.y * s, kA * w0.y * c, -kQA * w0.y * w0.y * s);",
  "    amp += A;",
  "  }",
  "  let ph = fract(t * ocean.p[0].w / 9.0 + 0.15 * sin(xz.x * 0.07 + 1.3) + 0.08 * sin(xz.x * 0.19));",
  "  let surge = smoothstep(0.0, 0.28, ph) * (1.0 - smoothstep(0.28, 1.0, ph));",
  "  p.y += ocean.p[4].w * 0.45 * surge * (1.0 - smoothstep(0.3, 4.0, depth0));",
  "  var out: OceanVertex;",
  "  out.world = p; out.normal = normalize(cross(dz, dx)); out.depth0 = depth0;",
  "  out.jacobian = dx.x * dz.z - dx.z * dz.x;",
  "  out.crest = select(0.5, clamp((p.y - level) / (amp * 1.2) * 0.5 + 0.5, 0.0, 1.0), amp > 0.0);",
  "  var clip = ocean.viewProj * vec4f(p, 1.0);",
  "  clip.z = min(clip.z, clip.w * 0.99999);",
  "  out.position = clip;",
  "  return out;",
  "}",
  "//GOSX_REFLECTION",
  "//GOSX_CLOUD_SOURCE",
  "fn oceanSky(d: vec3f) -> vec3f {",
  "  if (ocean.p[26].w == 4.0) { var base = gosxPhysicalSky(d, ocean.p[28], ocean.p[29], vec4f(ocean.p[30].xyz, 2.0), ocean.p[31].x) * ocean.p[24].w; //GOSX_CLOUD_REFLECT\n return base; }",
  "  return mix(ocean.p[25].xyz, select(ocean.p[26].xyz, ocean.p[24].xyz, d.y >= 0.0), abs(d.y)) * ocean.p[24].w;",
  "}",
  "fn oceanHash(q: vec2f) -> f32 { var p = fract(q * vec2f(0.1031, 0.1030)); p += dot(p, p.yx + 33.33); return fract((p.x + p.y) * p.x); }",
  "fn oceanNoise(p: vec2f) -> f32 {",
  "  let i = floor(p); let f = fract(p); let u = f * f * (3.0 - 2.0 * f);",
  "  return mix(mix(oceanHash(i), oceanHash(i + vec2f(1.0, 0.0)), u.x), mix(oceanHash(i + vec2f(0.0, 1.0)), oceanHash(i + vec2f(1.0, 1.0)), u.x), u.y);",
  "}",
  "fn oceanFbm(q: vec2f) -> f32 { var p = q; var v = 0.0; var a = 0.5; for (var i = 0; i < 4; i++) { v += a * oceanNoise(p); p = p * 2.03 + 17.1; a *= 0.5; } return v; }",
  "fn oceanDetail(n: vec3f, xz: vec2f, t: f32, wind: vec2f) -> vec3f {",
  "  var g = vec2f(0.0); var lambda = 1.7;",
  "  for (var i = 0; i < 8; i++) {",
  "    let ang = f32(i) * 2.39996;",
  "    let D = normalize(wind * 2.2 + vec2f(cos(ang), sin(ang)));",
  "    let k = 6.2831853 / lambda;",
  "    let ph = k * dot(D, xz) - sqrt(9.81 * k) * t + f32(i) * 1.7;",
  "    let fade = 1.0 - smoothstep(0.6, 2.0, fwidth(ph));",
  "    g += D * (0.09 * pow(0.83, f32(i)) * cos(ph) * fade);",
  "    lambda *= 0.74;",
  "  }",
  "  return normalize(n + vec3f(-g.x, 0.0, -g.y) * (1.0 - smoothstep(40.0, 500.0, length(xz - ocean.p[7].xz))));",
  "}",
  "@fragment fn fragmentMain(in: OceanVertex) -> @location(0) vec4f {",
  "  let toCam = ocean.p[7].xyz - in.world; let dist = length(toCam); let V = toCam / dist;",
  "  let t = ocean.p[0].z * ocean.p[0].w;",
  "  let N = oceanDetail(normalize(in.normal), in.world.xz, t, ocean.p[9].xy);",
  "  let rough = clamp(ocean.p[2].w + 0.6 * length(fwidth(N)) + dist * 0.0003, ocean.p[2].w, 0.5);",
  "  let depth = max(in.world.y - oceanFloor(in.world.xz), 0.0);",
  "  let NdV = max(dot(N, V), 1e-3);",
  "  let L = normalize(ocean.p[34].xyz); let sunCol = ocean.p[33].xyz; let ambient = ocean.p[32].xyz;",
  "  let F = 0.02 + 0.98 * pow(1.0 - NdV, 5.0);",
  "  var R = reflect(-V, N); R.y = abs(R.y);",
  "  var refl = oceanSky(normalize(R));",
  "  //GOSX_REFLECTION_LOOKUP",
  "  let H = normalize(L + V);",
  "  let NdL = max(dot(N, L), 0.0); let NdH = max(dot(N, H), 0.0);",
  "  let al = rough * rough; let al2 = al * al; let dd = NdH * NdH * (al2 - 1.0) + 1.0;",
  "  let k = al * 0.5; let G = (NdL / (NdL * (1.0 - k) + k)) * (NdV / (NdV * (1.0 - k) + k));",
  "  let Fh = 0.02 + 0.98 * pow(1.0 - max(dot(H, V), 0.0), 5.0);",
  "  var spec = min(sunCol * (al2 / (3.14159265 * dd * dd)) * G * Fh / max(4.0 * NdV, 1e-3), vec3f(64.0));",
  "  //GOSX_SUN_PATH",
  "  let crest = in.crest * in.crest;",
  "  let scatter = ocean.p[3].xyz * (sunCol * 0.18 * pow(clamp(dot(V, -L) * 0.5 + 0.5, 0.0, 1.0), 4.0) + ambient * 0.12) * crest;",
  "  let clarity = ocean.p[1].w;",
  "  let T = exp(-3.0 * depth * 0.5 * (1.0 + 1.0 / max(V.y, 0.08)) / clarity);",
  "  let water = mix(ocean.p[2].xyz * ambient, ocean.p[1].xyz * ambient + scatter, clamp(1.0 - exp(-depth / (clarity * 0.6)), 0.0, 1.0));",
  "  var foam = 0.0;",
  "  if (dist < 250.0) {",
  "    let fp = in.world.xz + vec2f(t * 0.06, t * 0.035);",
  "    let lace = smoothstep(0.72, 0.94, 1.0 - abs(2.0 * oceanFbm(fp * 0.9) - 1.0)) * smoothstep(0.3, 0.7, oceanFbm(fp * 0.23 + vec2f(3.1)));",
  "    foam = (1.0 - smoothstep(0.25, 0.6, in.jacobian)) * lace;",
  "    if (ocean.p[6].z > 0.5) {",
  "      let ph = fract(t / 9.0 + 0.15 * sin(in.world.x * 0.07 + 1.3) + 0.08 * sin(in.world.x * 0.19));",
  "      let breakDepth = mix(1.6, 0.25, smoothstep(0.0, 0.28, ph));",
  "      let breaker = exp(-pow((in.depth0 - breakDepth) / 0.3, 2.0)) * (1.0 - smoothstep(0.28, 0.6, ph));",
  "      let edge = (1.0 - smoothstep(0.0, 0.1, depth)) * smoothstep(0.002, 0.03, depth);",
  "      let wash = (1.0 - smoothstep(0.2, 1.8, in.depth0)) * 0.35;",
  "      foam = max(foam, (max(breaker, edge) + wash) * lace * 1.6);",
  "    }",
  "    foam = clamp(foam * ocean.p[3].w, 0.0, 1.0) * (1.0 - smoothstep(80.0, 250.0, dist));",
  "  }",
  "  let foamLit = ocean.p[4].xyz * (ambient * 0.55 + sunCol * 0.04 * max(dot(N, L), 0.0));",
  "  var color = water * (1.0 - T) * (1.0 - F) + refl * F + spec;",
  "  var alpha = 1.0 - T * (1.0 - F);",
  "  color = mix(color, foamLit, foam); alpha = mix(alpha, 1.0, foam);",
  "  let shoreFade = select(1.0, smoothstep(0.0, 0.04, depth), ocean.p[6].z > 0.5);",
  "  color *= shoreFade; alpha *= shoreFade;",
  "  let hz = normalize(vec3f(-V.x, 0.001, -V.z));",
  "  let fog = max(1.0 - exp(-dist / (ocean.p[0].y * 0.25)), smoothstep(0.7, 0.98, dist / ocean.p[0].y));",
  "  color = mix(color, oceanSky(hz), fog); alpha = mix(alpha, 1.0, fog);",
  "  if (ocean.p[7].w == 0.0) {",
  "    let c = color / max(alpha, 1e-4);",
  "    color = select(1.055 * pow(c, vec3f(1.0 / 2.4)) - 0.055, c * 12.92, c <= vec3f(0.0031308)) * alpha;",
  "  }",
  "  return vec4f(color, alpha);",
  "}",
].join("\n");

// wgpuCreateOceanRenderer builds the pass lazily; pipelines are cached per
// target format and sample count, like the sky pass.
function wgpuCreateOceanRenderer(device, textureCache, reflections, clouds) {
  // A deep-sea placeholder (R = 0) until the bathymetry map loads.
  const placeholder = device.createTexture({ label: "gosx-ocean-bathymetry-placeholder", size: [1, 1, 1], format: "rgba8unorm",
    usage: GPUTextureUsage.TEXTURE_BINDING | GPUTextureUsage.COPY_DST });
  device.queue.writeTexture({ texture: placeholder }, new Uint8Array([0, 0, 0, 255]), { bytesPerRow: 4 }, [1, 1, 1]);
  const placeholderView = placeholder.createView();
  const data = new Float32Array(16 + 140 /* sceneOceanUniformData: 35 vec4 */);
  const uniform = device.createBuffer({ label: "gosx-ocean", size: data.byteLength, usage: GPUBufferUsage.UNIFORM | GPUBufferUsage.COPY_DST });
  const sampler = device.createSampler({ magFilter: "linear", minFilter: "linear", addressModeU: "clamp-to-edge", addressModeV: "clamp-to-edge" });
  const entries = [
    { binding: 0, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, buffer: { type: "uniform" } },
    { binding: 1, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, sampler: {} },
    { binding: 2, visibility: GPUShaderStage.VERTEX | GPUShaderStage.FRAGMENT, texture: { sampleType: "float" } },
  ];
  const cloudData = clouds ? new Float32Array(64) : null;
  const cloudBuffer = clouds ? device.createBuffer({size: 256, usage: GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}) : null;
  if (clouds) entries.push({binding: 3, visibility: GPUShaderStage.FRAGMENT, buffer: {type: "uniform"}});
  const layout = device.createBindGroupLayout({entries});
  const reflectLayout = reflections ? sceneReflectWebGPULayout(device) : null;
  const dummyReflection = reflections ? sceneReflectWebGPUFallback(device, placeholderView) : null;
  const pipelineLayout = device.createPipelineLayout({ bindGroupLayouts: reflections ? [layout, reflectLayout] : [layout] });
  const module = device.createShaderModule({ label: "gosx-ocean", code: WGSL_SCENE_OCEAN
    .replace("//GOSX_REFLECTION", reflections ? sceneOceanReflectWGSL() : "")
    .replace("//GOSX_CLOUD_SOURCE", clouds ? sceneOceanCloudWGSL() : "")
    .replace("//GOSX_CLOUD_REFLECT", clouds ? "let c = gosxClouds(d,cloud.p[13].xyz,cloud.p[11],cloud.p[12],cloud.p[14].xyz,cloud.p[15].xyz,cloud.p[9].xyz); base = base*(1.0-c.a)+c.rgb;" : "")
    .replace("//GOSX_REFLECTION_LOOKUP", reflections ? "refl = oceanGeometryReflection(in.world, normalize(R), N, rough, refl);" : "")
    .replace("//GOSX_SUN_PATH", reflections ? "spec = mix(spec, oceanSunPath(N,H,L,V,rough,sunCol), 0.35);" : "") + "\n" + sceneSkyPhysicalSource("wgsl") });
  const pipelines = new Map();
  const blend = { srcFactor: "one", dstFactor: "one-minus-src-alpha", operation: "add" };
  let group = null, groupView = null;
  return {
    draw: function(pass, opts) {
      const env = opts.environment, ocean = env.ocean;
      sceneOceanUniformData(ocean, env, opts.camera, opts.timeSeconds, opts.linear, opts.quality, data.subarray(16));
      data.set(opts.viewProj, 0);
      let view = placeholderView, state = "surface";
      if (ocean.bathymetry && ocean.bathymetry.src) {
        const record = wgpuLoadTexture(device, ocean.bathymetry.src, textureCache, null, "ocean-bathymetry", "linear");
        if (record && record.loaded && !record.failed) { view = record.view; state = "shore"; }
        else { data[16 + 26] = 0; state = record && record.failed ? "bathymetry-failed" : "bathymetry-pending"; }
      }
      device.queue.writeBuffer(uniform, 0, data);
      if (clouds) { sceneCloudUniformData(Object.assign({},opts,{view: opts.view, aspect: opts.aspect || 1}),cloudData); if (!sceneAtmosphereQuality(opts.meta).clouds) cloudData[44] = 0; device.queue.writeBuffer(cloudBuffer,0,cloudData); }
      const key = opts.format + ":" + opts.samples;
      let pipeline = pipelines.get(key);
      if (!pipeline) {
        pipeline = device.createRenderPipeline({ label: "gosx-ocean", layout: pipelineLayout,
          vertex: { module: module, entryPoint: "vertexMain" },
          fragment: { module: module, entryPoint: "fragmentMain", targets: [{ format: opts.format, blend: { color: blend, alpha: blend } }] },
          primitive: { topology: "triangle-list", cullMode: "none" }, multisample: { count: opts.samples },
          depthStencil: { format: "depth24plus", depthWriteEnabled: true, depthCompare: "less-equal" } });
        pipelines.set(key, pipeline);
      }
      if (!group || view !== groupView) {
        const bindings = [
          { binding: 0, resource: { buffer: uniform } }, { binding: 1, resource: sampler }, { binding: 2, resource: view },
        ];
        if (clouds) bindings.push({binding: 3, resource: {buffer: cloudBuffer}});
        group = device.createBindGroup({layout,entries: bindings});
        groupView = view;
      }
      pass.setPipeline(pipeline);
      pass.setBindGroup(0, group);
      if (reflections) pass.setBindGroup(1, sceneReflectWebGPUGroup(device, reflectLayout, opts.reflection || dummyReflection));
      pass.draw(data[16 + 32] * data[16 + 33] * 6);
      if (opts.frameBindGroup) pass.setBindGroup(0, opts.frameBindGroup);
      return state;
    },
    dispose: function() { uniform.destroy(); placeholder.destroy(); if (dummyReflection) dummyReflection.uniform.destroy(); if (cloudBuffer) cloudBuffer.destroy(); pipelines.clear(); group = null; },
  };
}

// wgpuOceanDraw is the one call the WebGPU renderer makes per frame; it
// returns true when it drew so the renderer can skip its fallback call.
function wgpuOceanDraw(resources, pass, opts) {
  const env = opts.environment;
  let state = "none";
  if (env && env.ocean) {
    const featureKey = (sceneOceanReflections(env.ocean.reflections) ? 1 : 0) + (env.sky && env.sky.mode === "physical" && env.sky.clouds ? 2 : 0);
    if (resources.renderer && resources.featureKey !== featureKey) { resources.renderer.dispose(); resources.renderer = null; resources.failed = false; }
    if (!resources.renderer && !resources.failed) {
      resources.featureKey = featureKey;
      resources.renderer = wgpuCreateOceanRenderer(opts.device, opts.textureCache, Boolean(featureKey & 1), Boolean(featureKey & 2));
      resources.failed = !resources.renderer;
    }
    state = resources.renderer ? resources.renderer.draw(pass, opts) : "unavailable";
  }
  if (opts.mount && opts.mount.setAttribute) opts.mount.setAttribute("data-gosx-scene3d-ocean", state);
  return state !== "none";
}
