(function(){"use strict";var _=window.__gosx_scene3d_api;if(!_){console.warn("[gosx] scene3d-postfx-webgpu chunk loaded without main scene3d bundle");return}var m=_.sceneNumber||function(r,a){var e=Number(r);return Number.isFinite(e)?e:a},O=_.sceneMat4MultiplyInto,P,w,D,V,R;function z(r){r&&(P=r.wgpuCreateValidatedPipeline,w=r.wgpuRequirePipeline,D=r.WGSL_FRAME_STRUCTS,V=r.WGSL_POINTS_SHADE,R=r.WGSL_POINTS_UNIFORM_FIELDS)}const j=`
struct MoverUniforms { current: mat4x4f, previous: mat4x4f };
@group(0) @binding(0) var<uniform> mover: MoverUniforms;
struct VelocityOut { @builtin(position) clip: vec4f, @location(0) current: vec4f, @location(1) previous: vec4f };
@vertex fn vertexMain(@location(0) position: vec3f, @location(1) previousPosition: vec3f) -> VelocityOut {
    var out: VelocityOut;
    out.clip = mover.current * vec4f(position, 1.0);
    out.current = out.clip;
    out.previous = mover.previous * vec4f(previousPosition, 1.0);
    return out;
}
fn uvOf(clip: vec4f) -> vec2f { let n = clip.xy / clip.w; return vec2f(n.x * 0.5 + 0.5, 0.5 - n.y * 0.5); }
@fragment fn fragmentMain(in: VelocityOut) -> @location(0) vec4f {
    return vec4f(uvOf(in.previous) - uvOf(in.current), in.current.w, 1.0);
}`;function Y(r){return`
struct TAAParams {
    inverseViewProjection: mat4x4f,
    viewProjection: mat4x4f,
    previousViewProjection: mat4x4f,
    settings: vec4f,
    jitter: vec4f,
    ocean: vec4f,
    pools: array<vec4f, 4>,
};
@group(0) @binding(0) var currentTex: texture_2d<f32>;
@group(0) @binding(1) var historyTex: texture_2d<f32>;
@group(0) @binding(2) var depthTex: ${r?"texture_depth_multisampled_2d":"texture_depth_2d"};
@group(0) @binding(3) var velocityTex: texture_2d<f32>;
@group(0) @binding(4) var linearSamp: sampler;
@group(0) @binding(5) var<uniform> taa: TAAParams;
struct FullscreenOut { @builtin(position) position: vec4f, @location(0) uv: vec2f };
@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> FullscreenOut {
    let p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    var out: FullscreenOut;
    out.position = vec4f(p[i], 0.0, 1.0);
    out.uv = vec2f(p[i].x * 0.5 + 0.5, 0.5 - p[i].y * 0.5);
    return out;
}
fn toYCoCg(c: vec3f) -> vec3f { return vec3f(dot(c, vec3f(0.25, 0.5, 0.25)), c.r * 0.5 - c.b * 0.5, -c.r * 0.25 + c.g * 0.5 - c.b * 0.25); }
fn fromYCoCg(c: vec3f) -> vec3f { return vec3f(c.x + c.y - c.z, c.x + c.z, c.x - c.y - c.z); }
fn waterAt(world: vec3f) -> bool {
    if (taa.ocean.z > 0.5 && abs(world.y - taa.ocean.x) <= taa.ocean.y) { return true; }
    for (var k = 0u; k < 4u; k = k + 1u) {
        let pool = taa.pools[k];
        if (pool.w > 0.5 && abs(world.x) <= pool.x && abs(world.z) <= pool.y && abs(world.y) <= pool.z) { return true; }
    }
    return false;
}
@fragment fn fragmentMain(in: FullscreenOut) -> @location(0) vec4f {
    let size = vec2i(textureDimensions(currentTex));
    let px = clamp(vec2i(in.position.xy), vec2i(0), size - vec2i(1));
    let current = textureLoad(currentTex, px, 0);
    if (taa.settings.z < 0.5) { return current; }
    let depth = textureLoad(depthTex, px, 0);
    let uv = (vec2f(px) + vec2f(0.5)) / vec2f(size);
    // Remove this frame's jitter before reprojecting through the depth.
    let ndc = vec4f((uv.x - taa.jitter.x) * 2.0 - 1.0, 1.0 - (uv.y - taa.jitter.y) * 2.0, depth, 1.0);
    var world = taa.inverseViewProjection * ndc;
    world = world / world.w;
    let previous = taa.previousViewProjection * vec4f(world.xyz, 1.0);
    if (previous.w <= 0.0) { return current; }
    var historyUV = vec2f(previous.x / previous.w * 0.5 + 0.5, 0.5 - previous.y / previous.w * 0.5);
    var reactive = 0.0;
    if (taa.settings.w > 0.5) {
        let v = textureLoad(velocityTex, px, 0);
        let surfaceW = (taa.viewProjection * vec4f(world.xyz, 1.0)).w;
        if (v.a > 0.5 && v.a < 1.5 && abs(v.b - surfaceW) <= 0.02 * abs(surfaceW) + 0.02) { historyUV = uv + v.xy; }
        if (v.a > 1.5) { reactive = 1.0; }
    }
    if (depth < 0.99999 && waterAt(world.xyz)) { reactive = max(reactive, 0.75); }
    historyUV = historyUV + taa.jitter.zw;
    if (any(historyUV < vec2f(0.0)) || any(historyUV > vec2f(1.0))) { return current; }
    var low = vec4f(1e20);
    var high = vec4f(-1e20);
    var mean = vec4f(0.0);
    var square = vec4f(0.0);
    for (var y = -1; y <= 1; y = y + 1) {
        for (var x = -1; x <= 1; x = x + 1) {
            let s = textureLoad(currentTex, clamp(px + vec2i(x, y), vec2i(0), size - vec2i(1)), 0);
            let c = vec4f(toYCoCg(s.rgb), s.a);
            low = min(low, c); high = max(high, c); mean = mean + c / 9.0; square = square + c * c / 9.0;
        }
    }
    let sigma = sqrt(max(vec4f(0.0), square - mean * mean));
    low = max(low, mean - sigma * taa.settings.y);
    high = min(high, mean + sigma * taa.settings.y);
    let historySample = textureSampleLevel(historyTex, linearSamp, historyUV, 0.0);
    let bounded = clamp(vec4f(toYCoCg(historySample.rgb), historySample.a), low, high);
    let motion = length((historyUV - uv) * vec2f(size));
    let change = abs(bounded.x - toYCoCg(current.rgb).x);
    let weight = taa.settings.x * exp(-motion * 0.05) / (1.0 + change * 8.0) * (1.0 - reactive * 0.85);
    return vec4f(mix(current.rgb, fromYCoCg(bounded.rgb), weight), mix(current.a, bounded.a, weight));
}`}function q(){return`
@group(0) @binding(0) var source: texture_2d<f32>;
@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    return vec4f(p[i], 0.0, 1.0);
}
@fragment fn fragmentMain(@builtin(position) position: vec4f) -> @location(0) vec4f {
    return textureLoad(source, vec2i(position.xy), 0);
}`}const H=new Float32Array([1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1]);function N(r,a){for(var e=0,t=1;r>0;)t/=a,e+=t*(r%a),r=Math.floor(r/a);return e}function J(r,a,e,t,i){for(var n=(N(i%8+1,2)-.5)*2/Math.max(1,e),u=(N(i%8+1,3)-.5)*2/Math.max(1,t),o=0;o<4;o++)r[a+o*4]+=n*r[a+o*4+3],r[a+o*4+1]+=u*r[a+o*4+3];return[n*.5,-u*.5]}function X(r,a){var e=r,t=a;t[0]=e[5]*e[10]*e[15]-e[5]*e[11]*e[14]-e[9]*e[6]*e[15]+e[9]*e[7]*e[14]+e[13]*e[6]*e[11]-e[13]*e[7]*e[10],t[4]=-e[4]*e[10]*e[15]+e[4]*e[11]*e[14]+e[8]*e[6]*e[15]-e[8]*e[7]*e[14]-e[12]*e[6]*e[11]+e[12]*e[7]*e[10],t[8]=e[4]*e[9]*e[15]-e[4]*e[11]*e[13]-e[8]*e[5]*e[15]+e[8]*e[7]*e[13]+e[12]*e[5]*e[11]-e[12]*e[7]*e[9],t[12]=-e[4]*e[9]*e[14]+e[4]*e[10]*e[13]+e[8]*e[5]*e[14]-e[8]*e[6]*e[13]-e[12]*e[5]*e[10]+e[12]*e[6]*e[9],t[1]=-e[1]*e[10]*e[15]+e[1]*e[11]*e[14]+e[9]*e[2]*e[15]-e[9]*e[3]*e[14]-e[13]*e[2]*e[11]+e[13]*e[3]*e[10],t[5]=e[0]*e[10]*e[15]-e[0]*e[11]*e[14]-e[8]*e[2]*e[15]+e[8]*e[3]*e[14]+e[12]*e[2]*e[11]-e[12]*e[3]*e[10],t[9]=-e[0]*e[9]*e[15]+e[0]*e[11]*e[13]+e[8]*e[1]*e[15]-e[8]*e[3]*e[13]-e[12]*e[1]*e[11]+e[12]*e[3]*e[9],t[13]=e[0]*e[9]*e[14]-e[0]*e[10]*e[13]-e[8]*e[1]*e[14]+e[8]*e[2]*e[13]+e[12]*e[1]*e[10]-e[12]*e[2]*e[9],t[2]=e[1]*e[6]*e[15]-e[1]*e[7]*e[14]-e[5]*e[2]*e[15]+e[5]*e[3]*e[14]+e[13]*e[2]*e[7]-e[13]*e[3]*e[6],t[6]=-e[0]*e[6]*e[15]+e[0]*e[7]*e[14]+e[4]*e[2]*e[15]-e[4]*e[3]*e[14]-e[12]*e[2]*e[7]+e[12]*e[3]*e[6],t[10]=e[0]*e[5]*e[15]-e[0]*e[7]*e[13]-e[4]*e[1]*e[15]+e[4]*e[3]*e[13]+e[12]*e[1]*e[7]-e[12]*e[3]*e[5],t[14]=-e[0]*e[5]*e[14]+e[0]*e[6]*e[13]+e[4]*e[1]*e[14]-e[4]*e[2]*e[13]-e[12]*e[1]*e[6]+e[12]*e[2]*e[5],t[3]=-e[1]*e[6]*e[11]+e[1]*e[7]*e[10]+e[5]*e[2]*e[11]-e[5]*e[3]*e[10]-e[9]*e[2]*e[7]+e[9]*e[3]*e[6],t[7]=e[0]*e[6]*e[11]-e[0]*e[7]*e[10]-e[4]*e[2]*e[11]+e[4]*e[3]*e[10]+e[8]*e[2]*e[7]-e[8]*e[3]*e[6],t[11]=-e[0]*e[5]*e[11]+e[0]*e[7]*e[9]+e[4]*e[1]*e[11]-e[4]*e[3]*e[9]-e[8]*e[1]*e[7]+e[8]*e[3]*e[5],t[15]=e[0]*e[5]*e[10]-e[0]*e[6]*e[9]-e[4]*e[1]*e[10]+e[4]*e[2]*e[9]+e[8]*e[1]*e[6]-e[8]*e[2]*e[5];var i=e[0]*t[0]+e[1]*t[4]+e[2]*t[8]+e[3]*t[12];if(!i||!isFinite(i))return!1;for(var n=0;n<16;n++)t[n]/=i;return!0}function $(r,a,e){var t=r&&r.environment&&r.environment.ocean;a[e]=t?m(t.level,0):0,a[e+1]=t?Math.max(.05,m(t.waveHeight,.8)*.75):0,a[e+2]=t?1:0,a[e+3]=0;for(var i=Array.isArray(r&&r.waterSystems)?r.waterSystems:[],n=0;n<4;n++){var u=i[n],o=e+4+n*4;a[o]=u?Math.max(.001,m(u.poolWidth,1)):0,a[o+1]=u?Math.max(.001,m(u.poolLength,1)):0,a[o+2]=u?.2*Math.max(.001,m(u.poolHeight,1))+.02:0,a[o+3]=u?1:0}}function E(r){for(var a=0;a<r.history.length;a++)r.history[a].texture.destroy();r.velocity&&r.velocity.destroy(),r.velocityDepth&&r.velocityDepth.destroy(),r.history=[],r.velocity=r.velocityDepth=r.velocityView=null,r.valid=!1}function K(r,a,e,t){if(!(r.velocity&&(!r.taa||r.history.length)&&e===r.width&&t===r.height)){E(r);for(var i=0;r.taa&&i<2;i++){var n=a.device.createTexture({label:"gosx-taa-history",size:[e,t,1],format:"rgba16float",usage:GPUTextureUsage.RENDER_ATTACHMENT|GPUTextureUsage.TEXTURE_BINDING});r.history.push({texture:n,view:n.createView()})}r.velocity=a.device.createTexture({label:"gosx-taa-velocity",size:[e,t,1],format:"rgba16float",usage:GPUTextureUsage.RENDER_ATTACHMENT|GPUTextureUsage.TEXTURE_BINDING}),r.velocityDepth=a.device.createTexture({label:"gosx-taa-velocity-depth",size:[e,t,1],format:"depth24plus",usage:GPUTextureUsage.RENDER_ATTACHMENT}),r.velocityView=r.velocity.createView(),r.width=e,r.height=t}}function T(r,a,e,t){return r.pipelines[e]||(r.pipelines[e]=t())}function C(r,a,e){return T(r,a,"resolve"+(e?"ms":""),function(){var t=a.device.createShaderModule({label:"taa-resolve",code:Y(e)}),i=a.device.createBindGroupLayout({label:"gosx-taa-resolve",entries:[{binding:0,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"unfilterable-float"}},{binding:1,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"float"}},{binding:2,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"depth",multisampled:e}},{binding:3,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"unfilterable-float"}},{binding:4,visibility:GPUShaderStage.FRAGMENT,sampler:{type:"filtering"}},{binding:5,visibility:GPUShaderStage.FRAGMENT,buffer:{type:"uniform"}}]});return{layout:i,pipeline:P(a.device,"render",{label:"gosx-taa-resolve",layout:a.device.createPipelineLayout({bindGroupLayouts:[i]}),vertex:{module:t,entryPoint:"vertexMain"},fragment:{module:t,entryPoint:"fragmentMain",targets:[{format:"rgba16float"}]},primitive:{topology:"triangle-list"}})}})}function L(r,a,e){return T(r,a,"blit|"+e,function(){var t=a.device.createShaderModule({label:"taa-blit",code:q()}),i=a.device.createBindGroupLayout({label:"gosx-taa-blit",entries:[{binding:0,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"unfilterable-float"}}]});return{layout:i,pipeline:P(a.device,"render",{label:"gosx-taa-blit",layout:a.device.createPipelineLayout({bindGroupLayouts:[i]}),vertex:{module:t,entryPoint:"vertexMain"},fragment:{module:t,entryPoint:"fragmentMain",targets:[{format:e}]},primitive:{topology:"triangle-list"}})}})}function F(r,a){return T(r,a,"velocity",function(){var e=a.device.createShaderModule({label:"taa-velocity",code:j}),t=a.device.createBindGroupLayout({label:"gosx-taa-mover",entries:[{binding:0,visibility:GPUShaderStage.VERTEX,buffer:{type:"uniform",hasDynamicOffset:!0,minBindingSize:128}}]}),i=function(n){return{arrayStride:12,attributes:[{shaderLocation:n,offset:0,format:"float32x3"}]}};return{layout:t,pipeline:P(a.device,"render",{label:"gosx-taa-velocity",layout:a.device.createPipelineLayout({bindGroupLayouts:[t]}),vertex:{module:e,entryPoint:"vertexMain",buffers:[i(0),i(1)]},fragment:{module:e,entryPoint:"fragmentMain",targets:[{format:"rgba16float"}]},primitive:{topology:"triangle-list"},depthStencil:{format:"depth24plus",depthWriteEnabled:!0,depthCompare:"less"}})}})}function k(r,a,e){return T(r,a,"mask"+(e?"ms":""),function(){var t=[D,"@group(0) @binding(0) var<uniform> frame: FrameUniforms;","struct PointsUniforms {",R,"};","@group(3) @binding(0) var sceneDepth: "+(e?"texture_depth_multisampled_2d":"texture_depth_2d")+";",V,"@fragment fn fragmentMain(in: PointsInput, @builtin(position) fragCoord: vec4f) -> @location(0) vec4f {","    if (pointsShade(in).a < 0.05 || fragCoord.z > textureLoad(sceneDepth, vec2i(fragCoord.xy), 0)) { discard; }","    return vec4f(0.0, 0.0, 0.0, 2.0);","}"].join(`
`),i=a.device.createBindGroupLayout({label:"gosx-taa-mask-depth",entries:[{binding:0,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"depth",multisampled:e}}]}),n=a.device.createShaderModule({label:"taa-particle-mask",code:t});return{depthLayout:i,pipeline:P(a.device,"render",{label:"gosx-taa-particle-mask",layout:a.device.createPipelineLayout({bindGroupLayouts:[a.frameLayout,a.materialLayout,a.pointsLayout,i]}),vertex:{module:a.pointsVertex,entryPoint:"vertexMain",buffers:[]},fragment:{module:n,entryPoint:"fragmentMain",targets:[{format:"rgba16float",blend:{color:{operation:"add",srcFactor:"zero",dstFactor:"one"},alpha:{operation:"max",srcFactor:"one",dstFactor:"one"}}}]},primitive:{topology:"triangle-list"},depthStencil:{format:"depth24plus",depthWriteEnabled:!1,depthCompare:"always"}})}})}function Q(r,a,e){r.moverBuffer&&r.moverCapacity>=e||(r.moverBuffer&&r.moverBuffer.destroy(),r.moverCapacity=Math.max(16,e*2),r.moverBuffer=a.device.createBuffer({label:"gosx-taa-movers",size:r.moverCapacity*256,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}))}function Z(r,a,e,t){for(var i=[],n=new Float32Array(16),u=new Set,o=0;o<t.length;o++){var s=t[o];if(!(!s||s.viewCulled||!s.id)){var c=a.skinBuffer(s),p=c?H:s.modelMatrix&&s.modelMatrix.length>=16?s.modelMatrix:null;if(!(!p||!s.retainedGeometry&&!c)){u.add(s.id);var f=r.movers.get(s.id),d=!f||c||f.model.some(function(B,b){return B!==p[b]});f&&d&&i.push({obj:s,skin:c,model:p,previous:f.model.slice()}),f||r.movers.set(s.id,f={model:new Float32Array(16),previousSkin:null}),f.model.set(p)}}}if(r.movers.forEach(function(B,b){u.has(b)||r.movers.delete(b)}),!i.length)return 0;Q(r,a,i.length);var g=F(r,a),x=new Float32Array(64),y=a.device.createBindGroup({layout:g.layout,entries:[{binding:0,resource:{buffer:r.moverBuffer,size:128}}]});e.setPipeline(w(g.pipeline));for(var A=0,l=0;l<i.length;l++){var v=i[l];if(O(n,r.viewProjection,v.model),x.set(n,0),O(n,r.previousViewProjection,v.previous),x.set(n,16),a.device.queue.writeBuffer(r.moverBuffer,l*256,x,0,32),e.setBindGroup(0,y,[l*256]),v.skin)e.setVertexBuffer(0,v.skin),e.setVertexBuffer(1,a.previousSkin(v.obj)||v.skin);else if(!a.bindPositions(e,0,v.obj)||!a.bindPositions(e,1,v.obj))continue;var h=a.bindIndices(e,v.obj);h>0?e.drawIndexed(h):e.draw(v.obj.vertexCount),A+=1}return A}function ee(r,a,e,t,i,n,u){for(var o=null,s=0,c=0;c<(t||[]).length;c++){var p=t[c]&&t[c].system,f=p&&(p._gosxWGPUCPPointsBG||p._gosxWGPUCPSoftPointsBG),d=p&&p.entry&&p.entry.material&&String(p.entry.material.blendMode||"").toLowerCase();!f||!f.bg||p.count<=0||d!=="alpha"&&d!=="additive"||(o||(o=k(r,a,n),e.setPipeline(w(o.pipeline)),e.setBindGroup(0,u),e.setBindGroup(1,a.materialGroup(),[0]),e.setBindGroup(3,a.device.createBindGroup({layout:o.depthLayout,entries:[{binding:0,resource:i}]}))),e.setBindGroup(2,f.bg),e.draw(6,p.count),s+=1)}return s}function re(r,a,e){var t=Array.isArray(a)?a.find(function(d){return d&&d.kind==="taa"}):null,i=Array.isArray(a)&&a.some(function(d){return d&&d.kind==="motionBlur"});if(r.taa=!!t,r.active=(!!t||i)&&e.postProcessing&&e.perspective,!r.active)return(r.history.length||r.velocity)&&E(r),!1;var n=r.resources(),u=JSON.stringify([a,e.width,e.height,e.samples]),o=e.view,s=[-(o[0]*o[12]+o[1]*o[13]+o[2]*o[14]),-(o[4]*o[12]+o[5]*o[13]+o[6]*o[14]),-(o[8]*o[12]+o[9]*o[13]+o[10]*o[14])],c=[-o[2],-o[6],-o[10]],p=Math.hypot(s[0]-r.previousEye[0],s[1]-r.previousEye[1],s[2]-r.previousEye[2])>2||c[0]*r.previousForward[0]+c[1]*r.previousForward[1]+c[2]*r.previousForward[2]<.5,f=[0,5,10,11,14].some(function(d){return Math.abs(e.projection[e.projectionOffset+d]-r.previousProjection[d])>1e-5});return(r.stamp!==u||e.now-r.lastAt>250||p||f||r.samples!==e.samples)&&(r.valid&&(r.resets+=1),r.valid=!1),r.stamp=u,r.lastAt=e.now,r.samples=e.samples,r.previousEye=s,r.previousForward=c,r.previousProjection.set(e.projection.subarray(e.projectionOffset,e.projectionOffset+16)),K(r,n,e.width,e.height),r.previousViewProjection.set(r.viewProjection),r.viewProjection.set(e.viewProjection),r.previousJitter=r.jitter,r.jitter=r.valid&&r.taa?J(e.projection,e.projectionOffset,e.width,e.height,r.frameIndex):[0,0],r.valid&&r.taa}function ae(r,a,e,t){if(!r.active||!r.velocity||r.taa&&!r.history.length)return e;var i=r.resources(),n=t.samples>1;r.taa&&(C(r,i,n),L(r,i,t.colorFormat)),F(r,i),Array.isArray(t.particles)&&t.particles.length&&k(r,i,n);var u=e.find(function(l){return l&&l.kind==="taa"})||{},o=a.beginRenderPass({label:"gosx-taa-velocity",colorAttachments:[{view:r.velocity.createView(),loadOp:"clear",storeOp:"store",clearValue:{r:0,g:0,b:0,a:0}}],depthStencilAttachment:{view:r.velocityDepth.createView(),depthLoadOp:"clear",depthClearValue:1,depthStoreOp:"discard"}});r.moverCount=r.valid?Z(r,i,o,t.objects||[]):0;var s=r.valid?ee(r,i,o,t.particles,t.depthView,n,t.frameGroup):0;if(o.end(),!r.taa)return r.valid=!0,e;X(r.viewProjection,r.inverse)||(r.valid=!1),r.params.set(r.inverse,0),r.params.set(r.viewProjection,16),r.params.set(r.previousViewProjection,32),r.params[48]=Math.max(0,Math.min(.95,m(u.historyWeight,.9))),r.params[49]=Math.max(.5,Math.min(3,m(u.clampGamma,1))),r.params[50]=r.valid?1:0,r.params[51]=r.moverCount+s>0?1:0,r.params[52]=r.jitter[0],r.params[53]=r.jitter[1],r.params[54]=r.previousJitter[0],r.params[55]=r.previousJitter[1],$(t.bundle,r.params,56),r.paramBuffer||(r.paramBuffer=i.device.createBuffer({label:"gosx-taa-params",size:r.params.byteLength,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST})),i.device.queue.writeBuffer(r.paramBuffer,0,r.params);var c=r.history[r.historyIndex%2],p=r.history[(r.historyIndex+1)%2],f=C(r,i,n),d=i.device.createBindGroup({layout:f.layout,entries:[{binding:0,resource:t.colorView},{binding:1,resource:p.view},{binding:2,resource:t.depthView},{binding:3,resource:r.velocity.createView()},{binding:4,resource:i.linearSampler},{binding:5,resource:{buffer:r.paramBuffer}}]}),g=a.beginRenderPass({label:"gosx-taa-resolve",colorAttachments:[{view:c.view,loadOp:"clear",storeOp:"store",clearValue:{r:0,g:0,b:0,a:0}}]});g.setPipeline(w(f.pipeline)),g.setBindGroup(0,d),g.draw(3),g.end();var x=L(r,i,t.colorFormat),y=a.beginRenderPass({label:"gosx-taa-copy",colorAttachments:[{view:t.colorView,loadOp:"load",storeOp:"store"}]});y.setPipeline(w(x.pipeline)),y.setBindGroup(0,i.device.createBindGroup({layout:x.layout,entries:[{binding:0,resource:c.view}]})),y.draw(3),y.end();var A=r.valid;return r.valid=!0,r.historyIndex+=1,r.frameIndex+=1,A?e.filter(function(l){return!l||l.kind!=="taa"}):e}function te(r,a,e){if(!(!r.active||!r.valid))for(var t=r.resources(),i=0;i<(e||[]).length;i++){var n=e[i],u=n&&t.skinBuffer(n);u&&((!n._gosxTAAPreviousSkin||n._gosxTAAPreviousSkin.size!==u.size)&&(n._gosxTAAPreviousSkin=t.device.createBuffer({label:"gosx-taa-previous-skin",size:u.size,usage:GPUBufferUsage.VERTEX|GPUBufferUsage.COPY_DST})),a.copyBufferToBuffer(u,0,n._gosxTAAPreviousSkin,0,u.size))}}function ie(r){var a={resources:r,history:[],historyIndex:0,width:0,height:0,samples:0,valid:!1,taa:!1,velocityView:null,stamp:"",lastAt:-1/0,frameIndex:0,active:!1,resets:0,jitter:[0,0],previousJitter:[0,0],viewProjection:new Float32Array(16),previousViewProjection:new Float32Array(16),inverse:new Float32Array(16),previousEye:[0,0,0],previousForward:[0,0,-1],previousProjection:new Float32Array(16),params:new Float32Array(76),paramBuffer:null,moverBuffer:null,moverCapacity:0,velocity:null,velocityDepth:null,movers:new Map,moverCount:0,pipelines:{}};return{prepare:function(e,t,i){if(!(!t||!Array.isArray(e)||!e.some(function(u){return u&&(u.kind==="taa"||u.kind==="motionBlur")}))){var n=a.resources();F(a,n),e.some(function(u){return u&&u.kind==="taa"})&&(C(a,n,i>1),L(a,n,"rgba16float"))}},beginFrame:function(e,t){return re(a,e,t)},resolve:function(e,t,i){return ae(a,e,t,i)},capturePreviousSkins:function(e,t){te(a,e,t)},velocityView:function(){return a.active&&a.valid?a.velocityView:null},stats:function(){return{taaActive:a.active?1:0,taaHistoryValid:a.valid?1:0,taaMovers:a.moverCount,taaResets:a.resets}},dispose:function(){E(a),a.paramBuffer&&a.paramBuffer.destroy(),a.moverBuffer&&a.moverBuffer.destroy(),a.paramBuffer=a.moverBuffer=null,a.movers.clear()}}}var oe=["struct DOFParams {","    focusDistance: f32,","    aperture: f32,","    maxBlur: f32,","    near: f32,","    texelSize: vec2f,","    far: f32,","    _pad: f32,","};","@group(0) @binding(0) var inputTex: texture_2d<f32>;","@group(0) @binding(1) var inputSamp: sampler;","@group(0) @binding(2) var depthTex: texture_depth_2d;","@group(0) @binding(3) var<uniform> params: DOFParams;","const GOLDEN_ANGLE: f32 = 2.39996323;","const DOF_TAPS: i32 = 48;","fn linearDepthAt(uv: vec2f) -> f32 {","    let dims = vec2f(textureDimensions(depthTex));","    let d = textureLoad(depthTex, vec2i(clamp(uv * dims, vec2f(0.0), dims - vec2f(1.0))), 0);","    let z = d * 2.0 - 1.0;","    return (2.0 * params.near * params.far) / max(0.0001, params.far + params.near - z * (params.far - params.near));","}","fn cocOf(depth: f32, maxBlur: f32) -> f32 {","    return clamp(abs(depth - params.focusDistance) * params.aperture * maxBlur, 0.0, maxBlur);","}","@fragment fn fragmentMain(@location(0) uv: vec2f) -> @location(0) vec4f {","    let maxBlur = clamp(params.maxBlur, 0.0, 64.0);","    let centerDepth = linearDepthAt(uv);","    let centerCoC = cocOf(centerDepth, maxBlur);","    var color = textureSampleLevel(inputTex, inputSamp, uv, 0.0).rgb;","    var total = 1.0;","    let stride = maxBlur * maxBlur / f32(2 * DOF_TAPS);","    var radius = stride;","    var angle = 0.0;","    for (var i = 0; i < DOF_TAPS; i = i + 1) {","        if (radius >= maxBlur || maxBlur < 0.5) { break; }","        let tapUV = uv + vec2f(cos(angle), sin(angle)) * params.texelSize * radius;","        let sampleColor = textureSampleLevel(inputTex, inputSamp, tapUV, 0.0).rgb;","        let sampleDepth = linearDepthAt(tapUV);","        var sampleCoC = cocOf(sampleDepth, maxBlur);","        if (sampleDepth > centerDepth) { sampleCoC = min(sampleCoC, centerCoC * 2.0); }","        color = color + mix(color / total, sampleColor, smoothstep(radius - 0.5, radius + 0.5, sampleCoC));","        total = total + 1.0;","        radius = radius + stride / radius;","        angle = angle + GOLDEN_ANGLE;","    }","    return vec4f(color / total, 1.0);","}"].join(`
`);function ne(r,a,e,t){var i=a||{};return new Float32Array([Math.max(0,m(r.focusDistance,8)),m(r.aperture,.04),m(r.maxBlur,8),Math.max(1e-4,m(i.near,.1)),1/Math.max(1,e),1/Math.max(1,t),Math.max(.1,m(i.far,1e3)),0])}const ue=`
struct ExposureParams { dt: f32, minExposure: f32, maxExposure: f32, key: f32, speedUp: f32, speedDown: f32, compensation: f32, reset: f32 };
struct ExposureState { exposure: f32, averageLuminance: f32, valid: f32, pad: f32 };
@group(0) @binding(0) var scene: texture_2d<f32>;
@group(0) @binding(1) var<uniform> params: ExposureParams;
@group(0) @binding(2) var<storage, read_write> state: ExposureState;
var<workgroup> sums: array<f32, 256>;
@compute @workgroup_size(256) fn measure(@builtin(local_invocation_index) index: u32) {
    let size = vec2f(textureDimensions(scene));
    var sum = 0.0;
    for (var k = 0u; k < 16u; k = k + 1u) {
        let cell = index * 16u + k;
        let grid = vec2f(f32(cell % 64u), f32(cell / 64u));
        let px = vec2i(min((grid + vec2f(0.5)) / 64.0 * size, size - vec2f(1.0)));
        let c = textureLoad(scene, px, 0).rgb;
        sum = sum + log2(max(dot(c, vec3f(0.2126, 0.7152, 0.0722)), 0.0001));
    }
    sums[index] = sum;
    workgroupBarrier();
    for (var stride = 128u; stride > 0u; stride = stride / 2u) {
        if (index < stride) { sums[index] = sums[index] + sums[index + stride]; }
        workgroupBarrier();
    }
    if (index == 0u) {
        let average = exp2(sums[0] / 4096.0);
        let target = clamp(params.key / max(average, 0.0001) * exp2(params.compensation), params.minExposure, params.maxExposure);
        var current = state.exposure;
        if (params.reset > 0.5 || state.valid < 0.5 || !(current > 0.0)) { current = target; }
        let rate = select(params.speedDown, params.speedUp, target > current);
        state.exposure = current + (target - current) * (1.0 - exp(-max(params.dt, 0.0) * rate));
        state.averageLuminance = average;
        state.valid = 1.0;
    }
}`,le=`
struct ExposureState { exposure: f32, averageLuminance: f32, valid: f32, pad: f32 };
@group(0) @binding(0) var<storage, read> state: ExposureState;
@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> @builtin(position) vec4f {
    let p = array<vec2f, 3>(vec2f(-1.0, -1.0), vec2f(3.0, -1.0), vec2f(-1.0, 3.0));
    return vec4f(p[i], 0.0, 1.0);
}
@fragment fn fragmentMain() -> @location(0) vec4f {
    let e = state.exposure;
    return vec4f(e, e, e, 1.0);
}`;function I(r){if(!r||typeof r!="object")return null;var a=function(n,u){var o=Number(n);return isFinite(o)&&o>0?o:u},e=a(r.minExposure,.1),t=a(r.maxExposure,8),i=Number(r.compensation);return{minExposure:Math.min(e,t),maxExposure:Math.max(e,t),key:a(r.key,.18),speedUp:a(r.speedUp,3),speedDown:a(r.speedDown,1),compensation:isFinite(i)?Math.max(-8,Math.min(8,i)):0}}function se(r){var a=null,e=null,t=null,i=null,n=null,u=null,o=null,s=!1,c=new Float32Array(8),p=-1,f=0,d={exposure:0,averageLuminance:0},g=null,x=!1;function y(l){if(!a){t=l.device.createBindGroupLayout({label:"gosx-exposure-measure",entries:[{binding:0,visibility:GPUShaderStage.COMPUTE,texture:{sampleType:"unfilterable-float"}},{binding:1,visibility:GPUShaderStage.COMPUTE,buffer:{type:"uniform"}},{binding:2,visibility:GPUShaderStage.COMPUTE,buffer:{type:"storage"}}]}),i=l.device.createBindGroupLayout({label:"gosx-exposure-apply",entries:[{binding:0,visibility:GPUShaderStage.FRAGMENT,buffer:{type:"read-only-storage"}}]}),a=P(l.device,"compute",{label:"gosx-exposure-measure",layout:l.device.createPipelineLayout({bindGroupLayouts:[t]}),compute:{module:l.device.createShaderModule({label:"exposure-measure",code:ue}),entryPoint:"measure"}});var v=l.device.createShaderModule({label:"exposure-apply",code:le});e=P(l.device,"render",{label:"gosx-exposure-apply",layout:l.device.createPipelineLayout({bindGroupLayouts:[i]}),vertex:{module:v,entryPoint:"vertexMain"},fragment:{module:v,entryPoint:"fragmentMain",targets:[{format:"rgba16float",blend:{color:{operation:"add",srcFactor:"zero",dstFactor:"src"},alpha:{operation:"add",srcFactor:"zero",dstFactor:"one"}}}]},primitive:{topology:"triangle-list"}}),n=l.device.createBuffer({label:"gosx-exposure-state",size:16,usage:GPUBufferUsage.STORAGE|GPUBufferUsage.COPY_SRC|GPUBufferUsage.COPY_DST}),u=l.device.createBuffer({label:"gosx-exposure-params",size:32,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST}),o=l.device.createBuffer({label:"gosx-exposure-readback",size:16,usage:GPUBufferUsage.MAP_READ|GPUBufferUsage.COPY_DST})}}function A(l){var v=[m(l&&l.x,0),m(l&&l.y,0),m(l&&l.z,0)],h=!g||Math.hypot(v[0]-g[0],v[1]-g[1],v[2]-g[2])>2;return g=v,h}return{prepare:function(l,v){v&&I(l&&l.autoExposure)&&y(r())},apply:function(l,v,h,B,b){var S=I(v&&v.autoExposure);if(!S||!h||!h.colorView)return x=!1,p=-1,!1;var G=r();y(G);var ce=p<0||b-p>250?0:(b-p)/1e3,pe=A(B)||!x;x=!0,p=b,c[0]=ce,c[1]=S.minExposure,c[2]=S.maxExposure,c[3]=S.key,c[4]=S.speedUp,c[5]=S.speedDown,c[6]=S.compensation,c[7]=pe?1:0,G.device.queue.writeBuffer(u,0,c);var M=l.beginComputePass({label:"gosx-exposure-measure"});M.setPipeline(w(a)),M.setBindGroup(0,G.device.createBindGroup({layout:t,entries:[{binding:0,resource:h.colorView},{binding:1,resource:{buffer:u}},{binding:2,resource:{buffer:n}}]})),M.dispatchWorkgroups(1),M.end();var U=l.beginRenderPass({label:"gosx-exposure-apply",colorAttachments:[{view:h.colorView,loadOp:"load",storeOp:"store"}]});return U.setPipeline(w(e)),U.setBindGroup(0,G.device.createBindGroup({layout:i,entries:[{binding:0,resource:{buffer:n}}]})),U.draw(3),U.end(),f+=1,!s&&f%30===1&&typeof o.mapAsync=="function"&&(l.copyBufferToBuffer(n,0,o,0,16),s=!0,(G.device.queue.onSubmittedWorkDone?G.device.queue.onSubmittedWorkDone():Promise.resolve()).then(function(){return o.mapAsync(typeof GPUMapMode<"u"?GPUMapMode.READ:1).then(function(){var W=new Float32Array(o.getMappedRange().slice(0));o.unmap(),d={exposure:W[0],averageLuminance:W[1]},s=!1})}).catch(function(){s=!1})),!0},stats:function(){return{autoExposure:x?Math.round(d.exposure*1e3)/1e3:0,autoExposureLuminance:x?d.averageLuminance:0}}}}window.__gosx_scene3d_postfx_webgpu_api={temporalAA:function(r,a){return z(a),ie(r)},autoExposure:function(r,a){return z(a),se(r)},dofFragment:oe,dofParams:ne}})();
