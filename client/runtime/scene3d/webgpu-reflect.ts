// Geometry reflection capture; independent of the material shading path.
function sceneOceanReflectWGSL() { return [
  "struct Reflection { vp: mat4x4f, inverse: mat4x4f, planarVP: mat4x4f, settings: vec4f };",
  "@group(1) @binding(0) var<uniform> reflection: Reflection;",
  "@group(1) @binding(1) var reflectionSampler: sampler;",
  "@group(1) @binding(2) var opaqueColor: texture_2d<f32>;",
  "@group(1) @binding(3) var opaqueDepth: texture_2d<f32>;",
  "@group(1) @binding(4) var planarColor: texture_2d<f32>;",
  "fn reflectionLinear(c: vec3f) -> vec3f {",
  "  if (reflection.settings.w > 0.5) { return c; }",
  "  return select(pow((c+0.055)/1.055,vec3f(2.4)),c/12.92,c <= vec3f(0.04045));",
  "}",
  "fn reflectionWorld(uv: vec2f, depth: f32) -> vec3f {",
  "  let p = reflection.inverse*vec4f(uv*vec2f(2.0,-2.0)+vec2f(-1.0,1.0),depth,1.0);",
  "  return p.xyz/p.w;",
  "}",
  "fn reflectionUV(p: vec3f) -> vec3f {",
  "  let c = reflection.vp*vec4f(p,1.0);",
  "  return vec3f(c.xy/c.w*vec2f(0.5,-0.5)+0.5,c.w);",
  "}",
  "fn reflectionDelta(p: vec3f, uv: vec2f) -> f32 {",
  "  let dims = textureDimensions(opaqueDepth);",
  "  let d = textureLoad(opaqueDepth,clamp(vec2i(uv*vec2f(dims)),vec2i(0),vec2i(dims)-1),0).r;",
  "  if (d >= 0.999999) { return -1e6; }",
  "  return length(p-ocean.p[7].xyz)-length(reflectionWorld(uv,d)-ocean.p[7].xyz);",
  "}",
  "fn oceanGeometryReflection(world: vec3f, ray: vec3f, N: vec3f, rough: f32, sky: vec3f) -> vec3f {",
  "  if (reflection.settings.x <= 0.0) { return sky; }",
  "  var result = sky;",
  "  if (reflection.settings.z > 0.5) {",
  "    let c = reflection.planarVP*vec4f(world,1.0);",
  "    let uv = c.xy/c.w*vec2f(0.5,-0.5)+0.5+N.xz*0.018;",
  "    if (c.w > 0.0 && all(uv > vec2f(0.0)) && all(uv < vec2f(1.0))) {",
  "      let probe = textureSampleLevel(planarColor,reflectionSampler,uv,0.0);",
  "      result = mix(sky,reflectionLinear(probe.rgb),probe.a);",
  "    }",
  "  }",
  "  var previous = 0.35;",
  "  let start = world+N*0.15;",
  "  for (var i = 0; i < 20; i++) {",
  "    if (f32(i) >= reflection.settings.y) { break; }",
  "    let t = 0.6+f32(i)*0.7+f32(i*i)*0.65;",
  "    var p = start+ray*t; let screen = reflectionUV(p); var uv = screen.xy;",
  "    if (screen.z <= 0.0 || any(uv < vec2f(0.015)) || any(uv > vec2f(0.985))) { break; }",
  "    var delta = reflectionDelta(p,uv);",
  "    if (delta >= 0.0) {",
  "      var lo = previous; var hi = t;",
  "      for (var j = 0; j < 5; j++) {",
  "        let mid = (lo+hi)*0.5; let q = start+ray*mid;",
  "        if (reflectionDelta(q,reflectionUV(q).xy) > 0.0) { hi = mid; } else { lo = mid; }",
  "      }",
  "      p = start+ray*hi; uv = reflectionUV(p).xy; delta = reflectionDelta(p,uv);",
  "      let thickness = 0.35+hi*0.012;",
  "      let edge = smoothstep(0.015,0.12,min(min(uv.x,uv.y),min(1.0-uv.x,1.0-uv.y)));",
  "      let hit = edge*(1.0-smoothstep(thickness*0.5,thickness,delta));",
  "      result = mix(result,reflectionLinear(textureSampleLevel(opaqueColor,reflectionSampler,uv,0.0).rgb),hit);",
  "      break;",
  "    }",
  "    previous = t;",
  "  }",
  "  return mix(sky,result,reflection.settings.x*(1.0-smoothstep(0.18,0.5,rough)));",
  "}",
  "fn oceanSunPath(N: vec3f, H: vec3f, L: vec3f, V: vec3f, rough: f32, sun: vec3f) -> vec3f {",
  "  let wind = normalize(ocean.p[9].xy); let crossWind = vec2f(-wind.y,wind.x);",
  "  let slope = H.xz/max(H.y,0.08)-N.xz/max(N.y,0.15);",
  "  let along = 0.025+rough*rough*1.8; let across = 0.012+rough*rough*0.8;",
  "  let exponent = pow(dot(slope,wind),2.0)/along+pow(dot(slope,crossWind),2.0)/across;",
  "  let D = exp(-min(exponent,80.0))/(3.14159265*sqrt(along*across)*pow(max(H.y,0.08),4.0));",
  "  let fresnel = 0.02+0.98*pow(1.0-max(dot(H,V),0.0),5.0);",
  "  let visibility = max(L.y,0.0)/(max(L.y,0.0)+0.12)*smoothstep(-0.01,0.02,L.y);",
  "  return min(sun*D*fresnel*visibility/max(4.0*max(dot(N,V),0.05),0.2),vec3f(32.0));",
  "}"
].join("\n"); }

// Capture is a reduced MRT colour+depth pass. Multisampled depth resolves by
// the closest covered sample so silhouettes occlude reflections consistently.
function sceneReflectCaptureWGSL(samples) {
  return WGSL_POST_VERTEX + `\n@group(0) @binding(0) var color: texture_2d<f32>;
@group(0) @binding(1) var depth: ${samples > 1 ? "texture_depth_multisampled_2d" : "texture_depth_2d"};
struct Capture { @location(0) color: vec4f, @location(1) depth: f32 };
@fragment fn fragmentMain(@location(0) uv: vec2f) -> Capture {
 let dims = textureDimensions(color); let p = clamp(vec2i(uv*vec2f(dims)),vec2i(0),vec2i(dims)-1);
 var d = 1.0;
 ${samples > 1 ? `for (var s = 0; s < ${samples}; s++) { d = min(d,textureLoad(depth,p,s)); }` : "d = textureLoad(depth,p,0);"}
 return Capture(textureLoad(color,p,0),d);
}`;
}
function createSceneReflectWebGPU(device) {
  const sampler = device.createSampler({ minFilter: "linear", magFilter: "linear" });
  const uniform = device.createBuffer({ label: "gosx-reflection", size: 208, usage: GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST });
  const frame = device.createBuffer({ label: "gosx-reflection-frame", size: 256, usage: GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST });
  const pipelines = new Map(), textures = [];
  let key = "", targets = null;
  function texture(w,h,format,samples) {
    const t = device.createTexture({ label: "gosx-reflection", size: [w,h,1],format,sampleCount: samples || 1,
      usage: GPUTextureUsage.RENDER_ATTACHMENT|GPUTextureUsage.TEXTURE_BINDING });
    textures.push(t); return t.createView();
  }
  function ensure(opts,config) {
    const w = Math.max(1,Math.round(opts.width*config.resolution)), h = Math.max(1,Math.round(opts.height*config.resolution));
    const next = [w,h,opts.format,opts.samples].join(":");
    if (next === key) return;
    textures.forEach(t => t.destroy()); textures.length = 0;
    targets = { color: texture(w,h,opts.format,1), depth: texture(w,h,"r32float",1),
      planar: texture(w,h,opts.format,1), planarDepth: texture(w,h,"depth24plus",opts.samples),
      planarMSAA: opts.samples > 1 ? texture(w,h,opts.format,opts.samples) : null };
    key = next;
  }
  function capture(encoder,opts) {
    const k = opts.format+":"+opts.samples;
    let pipeline = pipelines.get(k);
    if (!pipeline) {
      const module = device.createShaderModule({ label: "gosx-reflection-capture",code: sceneReflectCaptureWGSL(opts.samples) });
      pipeline = device.createRenderPipeline({ label: "gosx-reflection-capture",layout: "auto",
        vertex: { module,entryPoint: "vertexMain" },fragment: { module,entryPoint: "fragmentMain",targets: [{format: opts.format},{format: "r32float"}] },primitive: {topology:"triangle-strip"} });
      pipelines.set(k,pipeline);
    }
    const group = device.createBindGroup({ layout: pipeline.getBindGroupLayout(0),entries: [
      {binding: 0,resource: opts.colorView},{binding: 1,resource: opts.depthView} ] });
    const pass = encoder.beginRenderPass({ label: "gosx-reflection-capture",colorAttachments: [targets.color,targets.depth].map(view => ({view,loadOp: "clear",storeOp: "store",clearValue: {r:1,g:1,b:1,a:1}})) });
    pass.setPipeline(pipeline); pass.setBindGroup(0,group); pass.draw(4); pass.end();
  }
  return {
    prepare: function(opts,config,quality) {
      ensure(opts,config);
      opts.pass.end();
      capture(opts.encoder,opts);
      const matrices = sceneReflectionMatrices(opts.view,opts.proj,opts.environment.ocean.level || 0,true);
      const doPlanar = config.mode.includes("planar") && quality.planar;
      if (doPlanar && opts.draw) {
        const data = new Float32Array(opts.frameData);
        data.set(matrices.view,0); data.set(matrices.projection,16);
        data[33] = 2*(opts.environment.ocean.level || 0)-opts.camera.y;
        device.queue.writeBuffer(frame,0,data);
        const attachment = { view: targets.planarMSAA || targets.planar,loadOp: "clear",storeOp: "store",clearValue: {r:0,g:0,b:0,a:0} };
        if (targets.planarMSAA) attachment.resolveTarget = targets.planar;
        const pass = opts.encoder.beginRenderPass({ label: "gosx-planar-reflection",colorAttachments: [attachment],
          depthStencilAttachment: {view: targets.planarDepth,depthLoadOp: "clear",depthStoreOp: "store",depthClearValue: 1} });
        opts.draw(pass,opts.frameGroup(frame)); pass.end();
      }
      const data = new Float32Array(52); data.set(matrices.vp); data.set(matrices.inverse,16); data.set(matrices.planarVP,32);
      data.set([config.strength,config.mode.includes("ssr") ? quality.reflectionSteps : 0,doPlanar ? 1 : 0,opts.linear ? 1 : 0],48);
      device.queue.writeBuffer(uniform,0,data);
      const resume = Object.assign({},opts.descriptor, {colorAttachments: opts.descriptor.colorAttachments.map(a => Object.assign({},a,{loadOp: "load"})),
        depthStencilAttachment: Object.assign({},opts.descriptor.depthStencilAttachment,{depthLoadOp: "load"}) });
      delete resume.timestampWrites;
      return { pass: opts.encoder.beginRenderPass(resume),record: {uniform,sampler,color: targets.color,depth: targets.depth,planar: targets.planar} };
    },
    dispose: function() { textures.forEach(t => t.destroy()); textures.length = 0; uniform.destroy(); frame.destroy(); pipelines.clear(); targets = null; },
  };
}
function sceneReflectWebGPU(resources,device,opts) {
  const config = sceneOceanReflections(opts.environment && opts.environment.ocean && opts.environment.ocean.reflections);
  const quality = sceneAtmosphereQuality(opts.meta);
  if (!config || !quality.reflections) { sceneReflectDispose(resources); return {pass: opts.pass,record: null}; }
  if (!resources.reflection) resources.reflection = createSceneReflectWebGPU(device);
  return resources.reflection.prepare(opts,config,quality);
}
function sceneReflectWebGPUGroup(device,layout,record) {
  return device.createBindGroup({layout,entries: [
    {binding: 0,resource: {buffer: record.uniform}}, {binding: 1,resource: record.sampler},
    {binding: 2,resource: record.color},{binding: 3,resource: record.depth},{binding: 4,resource: record.planar} ]});
}
function sceneReflectWebGPULayout(device) {
  return device.createBindGroupLayout({ entries: [
    {binding: 0,visibility: GPUShaderStage.FRAGMENT,buffer: {type: "uniform"}},
    {binding: 1,visibility: GPUShaderStage.FRAGMENT,sampler: {type: "filtering"}},
    {binding: 2,visibility: GPUShaderStage.FRAGMENT,texture: {sampleType: "float"}},
    {binding: 3,visibility: GPUShaderStage.FRAGMENT,texture: {sampleType: "unfilterable-float"}},
    {binding: 4,visibility: GPUShaderStage.FRAGMENT,texture: {sampleType: "float"}},
  ] });
}
function sceneReflectWebGPUFallback(device,view) {
  return { uniform: device.createBuffer({size: 208,usage: GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),
    sampler: device.createSampler({minFilter: "linear",magFilter: "linear"}),color: view,depth: view,planar: view };
}
