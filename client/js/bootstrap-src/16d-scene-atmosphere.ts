// Opt-in atmosphere contracts and camera-independent quality policy.
function sceneAtmosphereNumber(raw, key, fallback, lo, hi) {
  return Math.max(lo, Math.min(hi, sceneNumber(raw && raw[key], fallback)));
}
function sceneOceanReflections(raw) {
  if (!sceneIsPlainObject(raw)) return null;
  const mode = String(raw.mode || "").trim().toLowerCase();
  if (!["ssr", "planar", "ssr+planar"].includes(mode)) return null;
  return { mode, resolution: sceneAtmosphereNumber(raw, "resolution", 0.5, 0.125, 1),
    strength: sceneAtmosphereNumber(raw, "strength", 1, 0, 1) };
}
// Authored settings stay immutable. Tier profiles and explicit ladder rungs
// shed planar first, then ray samples, then all optional atmosphere work.
function sceneAtmosphereQuality(meta) {
  const profile = meta && meta.qualityProfile || {};
  const tier = meta && meta.atmosphereTier || (meta && meta.qualityEnabled ? String(profile.tier || meta.qualityTier || "full") : "full");
  const off = ["survival", "minimal", "low", "off"].includes(tier);
  const cheap = off || ["reduced", "balanced", "medium"].includes(tier);
  const rung = meta && meta.atmosphere || profile.atmosphere || {};
  return { reflections: !off && rung.reflections !== false,
    planar: !cheap && rung.planar !== false,
    reflectionSteps: off ? 0 : cheap ? 10 : 20,
    clouds: !off && rung.clouds !== false, cloudOctaves: cheap ? 3 : 5,
    godRays: !off && rung.godRays !== false, raySamples: cheap ? 12 : 64,
    haze: !off && rung.haze !== false, grain: !off && rung.grain !== false };
}
function sceneReflectionMatrices(view, proj, level, webgpu) {
  const mirror = new Float32Array([1,0,0,0, 0,-1,0,0, 0,0,1,0, 0,2*level,0,1]);
  const reflected = sceneMat4Multiply(view, mirror);
  const vp = sceneMat4Multiply(proj, view);
  return { view: reflected, projection: sceneReflectionProjection(reflected, proj, level, webgpu), vp, inverse: sceneAtmosphereInverse(vp),
    planarVP: sceneMat4Multiply(proj, reflected) };
}

// A pivoted inverse handles perspective and orthographic cameras alike.
function sceneAtmosphereInverse(m) {
  const rows = Array.from({length: 4}, (_, r) => Array.from({length: 8}, (_, c) => c < 4 ? m[c*4+r] : +(c-4 === r)));
  for (let c = 0; c < 4; c++) {
    let pivot = c;
    for (let r = c+1; r < 4; r++) if (Math.abs(rows[r][c]) > Math.abs(rows[pivot][c])) pivot = r;
    [rows[c], rows[pivot]] = [rows[pivot], rows[c]];
    const d = rows[c][c];
    if (Math.abs(d) < 1e-12) return new Float32Array(16);
    for (let j = 0; j < 8; j++) rows[c][j] /= d;
    for (let r = 0; r < 4; r++) {
      if (r === c) continue;
      const k = rows[r][c];
      for (let j = 0; j < 8; j++) rows[r][j] -= k * rows[c][j];
    }
  }
  return new Float32Array(Array.from({length: 16}, (_, i) => rows[i%4][4+(i/4|0)]));
}
// Clip the reflected camera's near plane to the mean sea level. Based on
// Lengyel's oblique near-plane projection; WGSL uses a [0,w] depth range.
function sceneReflectionProjection(view, proj, level, webgpu) {
  const p = new Float32Array(proj), invView = sceneAtmosphereInverse(view);
  if (webgpu) for (let c = 0; c < 4; c++) p[c*4+2] = 2*p[c*4+2]-p[c*4+3];
  const plane = Array.from({length: 4}, (_, c) => invView[c*4+1]-level*invView[c*4+3]);
  const inv = sceneAtmosphereInverse(p), corner = [Math.sign(plane[0]),Math.sign(plane[1]),1,1];
  const q = Array.from({length: 4}, (_, r) => corner.reduce((v,k,c) => v+inv[c*4+r]*k,0));
  const dot = plane.reduce((v,k,i) => v+k*q[i],0);
  if (Math.abs(dot) > 1e-6) for (let c = 0; c < 4; c++) p[c*4+2] = plane[c]*2/dot-p[c*4+3];
  if (webgpu) for (let c = 0; c < 4; c++) p[c*4+2] = 0.5*(p[c*4+2]+p[c*4+3]);
  return p;
}
function sceneReflectDispose(resources) {
  if (resources.reflection) resources.reflection.dispose();
  resources.reflection = null;
}
function sceneReflectOpaqueList(list, materials) {
  // User shader uniforms have their own frame ownership. Visible custom
  // materials still reflect through SSR; the fallback draws built-in PBR.
  return (list || []).filter(obj => {
    const mat = materials[sceneNumber(obj.materialIndex,0)];
    return !(mat && (mat.shaderBackend === "selena" || mat.customVertex || mat.customFragment));
  });
}

function sceneAtmosphereTier(state, deviceTier) {
  const ladder = !state || state.mode !== "ladder" ? null
    : state.rungIndex === 0 ? "survival" : state.rungIndex >= state.ladder.length-1 ? "full" : "balanced";
  // GPU timing cannot see a CPU-bound device, so the capability tier caps the
  // ladder: a balanced device never runs full atmosphere, a constrained one
  // runs survival.
  const cap = deviceTier === "constrained" ? "survival" : deviceTier === "balanced" ? "balanced" : null;
  if (!cap) return ladder;
  if (!ladder) return cap;
  const rank = { full: 0, balanced: 1, survival: 2 };
  return rank[ladder] >= rank[cap] ? ladder : cap;
}

function sceneSkyClouds(raw) {
  if (!sceneIsPlainObject(raw)) return null;
  return { coverage: sceneAtmosphereNumber(raw,"coverage",0.45,0,1),
    altitude: sceneAtmosphereNumber(raw,"altitude",1500,100,12000),
    scale: sceneAtmosphereNumber(raw,"scale",3000,100,20000),
    speed: sceneAtmosphereNumber(raw,"speed",8,0,100),
    direction: ((sceneNumber(raw.direction,0)%360)+360)%360,
    opacity: sceneAtmosphereNumber(raw,"opacity",0.85,0,1) };
}
function sceneCloudUniformData(opts,out) {
  const env=opts.environment, config=sceneSkyClouds(env.sky.clouds), quality=sceneAtmosphereQuality(opts.meta);
  sceneSkyUniformData(out,env,opts.view,opts.camera,opts.aspect,opts.linear);
  const angle=config.direction*Math.PI/180;
  out.set([config.coverage,config.altitude,config.scale,config.opacity],44);
  out.set([Math.sin(angle)*config.speed,Math.cos(angle)*config.speed,opts.timeSeconds||0,quality.cloudOctaves],48);
  out.set([opts.camera.x||0,opts.camera.y||0,opts.camera.z||0,0],52);
  const light=sceneOceanUniformData({},env,opts.camera,0,true,"low");
  out.set([light[128],light[129],light[130],0],56);
  out.set([light[132],light[133],light[134],0],60);
  return out;
}

function sceneCloudDispose(resources) { if(resources.clouds)resources.clouds.dispose();resources.clouds=null; }

function sceneHaze(raw) {
  if(!sceneIsPlainObject(raw))return null;
  return {density:sceneAtmosphereNumber(raw,"density",0.0015,0,0.1),heightFalloff:sceneAtmosphereNumber(raw,"heightFalloff",0.04,0,1),sunScatter:sceneAtmosphereNumber(raw,"sunScatter",0.25,0,1)};
}
function sceneAtmospherePostEffect(raw) {
  const out=Object.assign({},raw);
  out.intensity=sceneAtmosphereNumber(raw,"intensity",raw.kind==="grain"?0.015:0.18,0,raw.kind==="grain"?0.1:2);
  if(raw.kind==="godRays") {out.decay=sceneAtmosphereNumber(raw,"decay",0.96,0,1);out.density=sceneAtmosphereNumber(raw,"density",0.9,0,2);out.samples=Math.round(sceneAtmosphereNumber(raw,"samples",32,8,64));}
  return out;
}
function sceneAtmosphereBundle(bundle,meta) {
  const env=bundle.environment||{},effects=bundle.postEffects||[];
  if(!env.haze && !effects.some(e=>e.kind==="godRays" || e.kind==="grain" || e.mode==="agx"))return bundle;
  let next=env;
  if(env.haze) {
    const haze=sceneHaze(env.haze),enabled=sceneAtmosphereQuality(meta).haze && haze.density>0;
    next=Object.assign({},env,{fogDensity:enabled?0:(env.fogDensity||haze.density)});
    next._envHash=hashEnvironmentContent(next);
  }
  return Object.assign({},bundle,{environment:next,postEffects:sceneAtmosphereEffects(effects,next,meta)});
}
// Fuse haze and shafts before bloom; preserve the order of grading effects.
function sceneAtmosphereEffects(effects,env,meta) {
  const quality=sceneAtmosphereQuality(meta),sky=env && env.sky;
  const haze=env && env.haze && quality.haze && sceneHaze(env.haze).density>0 ? sceneHaze(env.haze) : null;
  let rays=null;const out=[];
  for(const raw of effects){
    if(raw.kind==="godRays") {if(quality.godRays && sky && sky.mode==="physical"){const effect=sceneAtmospherePostEffect(raw);if(effect.intensity>0)rays=effect;}continue;}
    if(raw.kind==="grain") {if(quality.grain){const intensity=sceneAtmospherePostEffect(raw).intensity;if(intensity>0)out.push({kind:"atmosphere",grain:intensity});}continue;}
    if(raw.kind==="toneMapping" && raw.mode==="agx") {out.push({kind:"atmosphere",agx:true,exposure:sceneNumber(raw.exposure,1)||1});continue;}
    out.push(raw);
  }
  if(haze || rays)out.unshift({kind:"atmosphere",haze,rays});
  return out;
}
function sceneAtmospherePostKey(effect) { return [!!effect.haze,!!effect.rays,!!effect.agx,!!effect.grain].map(x=>+x).join(""); }
function sceneAtmospherePostUniforms(effect,ctx,webgpu,out) {
  out.fill(0);out.set(sceneAtmosphereInverse(ctx.viewProj),0);
  const camera=ctx.camera;out.set([camera.x||0,camera.y||0,camera.z||0,0],16);
  if(effect.haze)out.set([effect.haze.density,effect.haze.heightFalloff,effect.haze.sunScatter,0],20);
  const env=ctx.environment||{},sky=env.sky||{mode:"physical"};sceneSkyPhysicalParams(sky,out,24);
  const p=out.subarray(24,40),direction=[p[8],p[9],p[10]];
  const sun=[camera.x+direction[0]*10000,camera.y+direction[1]*10000,camera.z+direction[2]*10000,1];
  const clip=Array.from({length:4},(_,r)=>sun.reduce((v,k,c)=>v+ctx.viewProj[c*4+r]*k,0));
  const visible=clip[3]>0 && direction[1]>-0.015;
  const uv=[clip[0]/Math.max(clip[3],1e-5)*0.5+0.5,clip[1]/Math.max(clip[3],1e-5)*(webgpu?-0.5:0.5)+0.5];
  out.set([uv[0],uv[1],visible?1:0,0],40);
  if(effect.rays)out.set([effect.rays.intensity,effect.rays.decay,effect.rays.density,Math.min(effect.rays.samples,sceneAtmosphereQuality(ctx.meta).raySamples)],44);
  out.set([effect.exposure||1,effect.grain||0,sceneNumber(sky.intensity,1)||1,0],48);
  const light=sceneOceanUniformData({},Object.assign({},env,{sky}),camera,0,true,"low");out.set(light.subarray(128,131),52);out.set(light.subarray(132,135),56);
  return out;
}

// AgX's log-exposure fit and Rec2020 matrices follow the public three.js /
// Filament formulation. Output here is linear sRGB; the post pass encodes once.
// https://github.com/mrdoob/three.js/blob/dev/src/renderers/shaders/ShaderChunk/tonemapping_pars_fragment.glsl.js
function sceneAgXSource(kind) { return kind === "glsl" ? "vec3 gosxAgX(vec3 color) {\n vec3 c=mat3(0.544812080,0.140419345,0.088817138,0.373797444,0.754107783,0.178859755,0.081380964,0.105396747,0.732315427)*max(color,vec3(0.));\n vec3 x=clamp((log2(max(c,vec3(1e-10)))+12.47393)/16.5,0.,1.);\n // Sixth-order fit of the log-exposure sigmoid: smooth toe and shoulder.\n vec3 y=((((15.5*x-40.14)*x+31.96)*x-6.868)*x+0.4298)*x*x+0.1191*x-0.00232;\n vec3 display=mat3(1.127100582,-0.141329763,-0.141329763,-0.110606643,1.157823702,-0.110606643,-0.016493939,-0.016493939,1.251936407)*y;\n return clamp(mat3(1.660500000,-0.124600000,-0.018200000,-0.587600000,1.132900000,-0.100600000,-0.072800000,-0.008300000,1.118700000)*pow(max(display,vec3(0.)),vec3(2.2)),0.,1.);\n}" : "fn gosxAgX(color: vec3f) -> vec3f {\n let c=mat3x3f(vec3f(0.544812080,0.140419345,0.088817138),vec3f(0.373797444,0.754107783,0.178859755),vec3f(0.081380964,0.105396747,0.732315427))*max(color,vec3f(0.0));\n let x=clamp((log2(max(c,vec3f(1e-10)))+12.47393)/16.5,vec3f(0.0),vec3f(1.0));\n let y=((((15.5*x-40.14)*x+31.96)*x-6.868)*x+0.4298)*x*x+0.1191*x-0.00232;\n let display=mat3x3f(vec3f(1.127100582,-0.141329763,-0.141329763),vec3f(-0.110606643,1.157823702,-0.110606643),vec3f(-0.016493939,-0.016493939,1.251936407))*y;\n return clamp(mat3x3f(vec3f(1.660500000,-0.124600000,-0.018200000),vec3f(-0.587600000,1.132900000,-0.100600000),vec3f(-0.072800000,-0.008300000,1.118700000))*pow(max(display,vec3f(0.0)),vec3f(2.2)),vec3f(0.0),vec3f(1.0));\n}"; }
