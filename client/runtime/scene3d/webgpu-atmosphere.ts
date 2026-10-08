// Living daylight sky overlay, drawn before opaque geometry.
const SCENE_CLOUD_WGSL = [
 "fn cloudHash(q: vec2f) -> f32 { var p=fract(q*vec2f(0.1031,0.1030)); p+=dot(p,p.yx+33.33); return fract((p.x+p.y)*p.x); }",
 "fn cloudNoise(p: vec2f) -> f32 {",
 " let i=floor(p); var f=fract(p); f=f*f*(3.0-2.0*f);",
 " return mix(mix(cloudHash(i),cloudHash(i+vec2f(1,0)),f.x),mix(cloudHash(i+vec2f(0,1)),cloudHash(i+1.0),f.x),f.y);",
 "}",
 "fn gosxClouds(ray: vec3f, eye: vec3f, settings: vec4f, drift: vec4f, ambient: vec3f, sunLight: vec3f, sun: vec3f) -> vec4f {",
 " if(cloud.p[11].x<=0.0) { return vec4f(0.0); }",
 " let origin=eye+vec3f(0,6371000.0,0);",
 " let b=dot(origin,ray); let radius=6371000.0+settings.y;",
 " let t=max(-b+sqrt(max(b*b-dot(origin,origin)+radius*radius,0.0)),0.0);",
 " var p=(eye.xz+ray.xz*t-drift.xy*drift.z)/settings.z;",
 " var footprint=length(fwidth(p));",
 " var density=0.0; var weight=0.52; var total=0.0;",
 " for(var i=0;i<5;i++) {",
 "  if(f32(i)>=cloud.p[12].w) { break; }",
 "  let filtered=1.0-smoothstep(0.3,1.0,footprint);",
 "  density+=weight*mix(0.5,cloudNoise(p),filtered); total+=weight;",
 "  p=p*2.03+vec2f(17.1,9.2); footprint*=2.03; weight*=0.5;",
 " }",
 " density/=max(total,0.1);",
 " let threshold=1.0-settings.x;",
 " let shape=smoothstep(threshold-0.12,threshold+0.18,density)*smoothstep(0.0,0.025,settings.x);",
 " let tau=shape*3.5; let beer=exp(-tau); let powder=1.0-exp(-tau*2.0);",
 " let mu=max(dot(ray,sun),0.0);",
 " let silver=pow(mu,32.0)*exp(-tau*0.7)*(1.0-beer)*2.2;",
 " var lighting=ambient*(0.45+0.4*beer)+sunLight*(0.35*powder+silver)*smoothstep(-0.04,0.03,sun.y);",
 " let horizon=gosxPhysicalSky(normalize(vec3f(ray.x,0.025,ray.z)),cloud.p[7],cloud.p[8],vec4f(cloud.p[9].xyz,2.0),cloud.p[10].x)*cloud.p[3].w;",
 " lighting=mix(lighting,horizon,1.0-exp(-t/65000.0));",
 " let alpha=(1.0-beer)*settings.w*smoothstep(-0.005,0.025,ray.y);",
 " return vec4f(lighting*alpha,alpha);",
 "}"
].join("\n");
const SCENE_CLOUD_SHADER_WGSL = [
 "struct Cloud { p: array<vec4f,16> }; @group(0) @binding(0) var<uniform> cloud: Cloud;",
 "struct Out { @builtin(position) position: vec4f, @location(0) ndc: vec2f };",
 "@vertex fn vertexMain(@builtin(vertex_index) i: u32) -> Out {",
 " let p=vec2f(f32((i<<1u)&2u),f32(i&2u))*2.0-1.0; var o: Out; o.position=vec4f(p,1,1);o.ndc=p;return o; }",
 "//SKY", "//CLOUD",
 "@fragment fn fragmentMain(in: Out) -> @location(0) vec4f {",
 " let ray=normalize(cloud.p[2].xyz+cloud.p[0].xyz*in.ndc.x*cloud.p[0].w+cloud.p[1].xyz*in.ndc.y*cloud.p[1].w);",
 " var c=gosxClouds(ray,cloud.p[13].xyz,cloud.p[11],cloud.p[12],cloud.p[14].xyz,cloud.p[15].xyz,cloud.p[9].xyz);",
 " if(cloud.p[6].x==0.0) {let rgb=c.rgb/max(c.a,1e-5);c=vec4f(select(1.055*pow(rgb,vec3f(1.0/2.4))-0.055,rgb*12.92,rgb<=vec3f(0.0031308))*c.a,c.a);} return c; }",
].join("\n");
function createSceneCloudWebGPU(device) {
 const data=new Float32Array(64),buffer=device.createBuffer({label:"gosx-clouds",size:256,usage:GPUBufferUsage.UNIFORM|GPUBufferUsage.COPY_DST});
 const layout=device.createBindGroupLayout({entries:[{binding:0,visibility:GPUShaderStage.FRAGMENT,buffer:{type:"uniform"}}]});
 const pipelineLayout=device.createPipelineLayout({bindGroupLayouts:[layout]});
 const module=device.createShaderModule({label:"gosx-clouds",code:SCENE_CLOUD_SHADER_WGSL.replace("//SKY",sceneSkyPhysicalSource("wgsl")).replace("//CLOUD",SCENE_CLOUD_WGSL)});
 const group=device.createBindGroup({layout,entries:[{binding:0,resource:{buffer}}]}),pipelines=new Map();
 return {draw:function(pass,opts){
   sceneCloudUniformData(opts,data);device.queue.writeBuffer(buffer,0,data);
   const key=opts.format+":"+opts.samples;let pipeline=pipelines.get(key);
   if(!pipeline){const blend={srcFactor:"one",dstFactor:"one-minus-src-alpha",operation:"add"};
     pipeline=wgpuCreateValidatedPipeline(device, "render", {label:"gosx-clouds",layout:pipelineLayout,vertex:{module,entryPoint:"vertexMain"},
       fragment:{module,entryPoint:"fragmentMain",targets:[{format:opts.format,blend:{color:blend,alpha:blend}}]},
       multisample:{count:opts.samples},depthStencil:{format:"depth24plus",depthWriteEnabled:false,depthCompare:"always"}});pipelines.set(key,pipeline);}
   pass.setPipeline(wgpuRequirePipeline(pipeline));pass.setBindGroup(0,group);pass.draw(3);
 },dispose:function(){buffer.destroy();pipelines.clear();}};
}
function sceneCloudWebGPUDraw(resources,device,pass,opts) {
 const sky=opts.environment && opts.environment.sky,config=sky && sceneSkyClouds(sky.clouds);
 if(!config || sky.mode!=="physical" || config.coverage<=0 || !sceneAtmosphereQuality(opts.meta).clouds){
   if(resources.clouds)resources.clouds.dispose();resources.clouds=null;return;
 }
 if(!resources.clouds)resources.clouds=createSceneCloudWebGPU(device);
 resources.clouds.draw(pass,opts);
}

function sceneOceanCloudWGSL() {
  return "struct Cloud { p: array<vec4f,16> }; @group(0) @binding(3) var<uniform> cloud: Cloud;\n"+SCENE_CLOUD_WGSL;
}

const SCENE_ATMOSPHERE_WGSL_COMMON = [
 "fn atmosphereHash(p: vec2f) -> f32 { return fract(52.9829189*fract(dot(p,vec2f(0.06711056,0.00583715)))); }",
 "fn atmosphereWorld(uv: vec2f,depth: f32) -> vec3f {",
 " let p=mat4x4f(atmo.p[0],atmo.p[1],atmo.p[2],atmo.p[3])*vec4f(uv*vec2f(2.0,-2.0)+vec2f(-1.0,1.0),depth,1.0); return p.xyz/p.w;",
 "}"
].join("\n");

const SCENE_ATMOSPHERE_WGSL_HAZE = [
 "let depth=atmosphereDepth(uv);",
 " if(depth<0.999999) {",
 "  let world=atmosphereWorld(uv,depth);let delta=world-atmo.p[4].xyz;",
 "  let distance=length(delta);let ray=delta/max(distance,1e-4);",
 "  let h=clamp(atmo.p[5].y*delta.y,-40.0,40.0);",
 "  var integral=1.0-h*0.5;if(abs(h)>=0.001){integral=(1.0-exp(-h))/h;}",
 "  let optical=atmo.p[5].x*distance*exp(-atmo.p[5].y*max(atmo.p[4].y,0.0))*integral;",
 "  let transmittance=exp(-min(optical,40.0));",
 "  let horizon=gosxPhysicalSky(normalize(vec3f(ray.x,0.025,ray.z)),atmo.p[6],atmo.p[7],vec4f(atmo.p[8].xyz,2.0),atmo.p[9].x)*atmo.p[12].z;",
 "  let mu=max(dot(ray,atmo.p[8].xyz),0.0);let forward=pow(mu,12.0)*atmo.p[5].z;",
 "  let scatter=mix(horizon,atmo.p[14].xyz*0.7,forward);",
 "  color=mix(scatter,color,transmittance);",
 "  color+=vec3f(atmosphereHash(position.xy)-0.5)*(1.0-transmittance)/1024.0;",
 " }"
].join("\n");

const SCENE_ATMOSPHERE_WGSL_RAYS = [
 "var shafts=vec3f(0.0);",
 " if(atmo.p[10].z>0.5 && atmo.p[11].x>0.0) {",
 "  let stepUV=(uv-atmo.p[10].xy)*atmo.p[11].z/max(atmo.p[11].w,1.0);",
 "  var sampleUV=uv-stepUV*(0.5+atmosphereHash(position.xy)*0.25);",
 "  var weight=1.0;var sum=0.0;var normalization=0.0;",
 "  for(var i=0;i<64;i++) {",
 "   if(f32(i)>=atmo.p[11].w){break;}",
 "   var mask=0.0;",
 "   if(all(sampleUV>vec2f(0.0)) && all(sampleUV<vec2f(1.0))) {",
 "    let visible=select(0.0,1.0,atmosphereDepth(sampleUV)>=0.999999);",
 "    let r=sampleUV-atmo.p[10].xy;mask=visible*exp(-dot(r,r)/0.012);",
 "   }",
 "   sum+=mask*weight;normalization+=weight;weight*=atmo.p[11].y;sampleUV-=stepUV;",
 "  }",
 "  let lowSun=1.0-smoothstep(0.15,0.8,atmo.p[8].y);",
 "  shafts=atmo.p[14].xyz*atmo.p[11].x*sum/max(normalization,1e-3)*(0.15+0.85*lowSun);",
 " }",
 " color+=shafts;"
].join("\n");

const SCENE_ATMOSPHERE_WGSL_GRAIN = [
 "let grain=(atmosphereHash(position.xy)-0.5)*atmo.p[12].y;",
 " color=clamp(color+grain*(0.4+0.6*sqrt(max(dot(color,vec3f(0.2126,0.7152,0.0722)),0.0))),vec3f(0.0),vec3f(1.0));"
].join("\n");
function sceneAtmospherePostWGSL(effect,samples,rayOnly) {
 const needsDepth=rayOnly || effect.haze;
 const depth=needsDepth?`@group(0) @binding(3) var sceneDepth: ${samples>1?"texture_depth_multisampled_2d":"texture_depth_2d"};
fn atmosphereDepth(uv: vec2f) -> f32 {
 let size=textureDimensions(sceneDepth);let p=clamp(vec2i(uv*vec2f(size)),vec2i(0),vec2i(size)-1);var d=1.0;
 ${samples>1?`for(var i=0;i<${samples};i++){d=min(d,textureLoad(sceneDepth,p,i));}`:"d=textureLoad(sceneDepth,p,0);"}return d;
}`:"";
 return [
   "struct Atmosphere { p: array<vec4f,15> }; @group(0) @binding(2) var<uniform> atmo: Atmosphere;",
   "@group(0) @binding(0) var inputTex: texture_2d<f32>; @group(0) @binding(1) var inputSampler: sampler;",
   depth,!rayOnly && effect.rays?"@group(0) @binding(4) var shaftsTex: texture_2d<f32>;":"",
   SCENE_ATMOSPHERE_WGSL_COMMON,
   effect.haze?sceneSkyPhysicalSource("wgsl"):"",effect.agx?sceneAgXSource("wgsl"):"",
   "@fragment fn fragmentMain(@location(0) uv: vec2f,@builtin(position) position: vec4f) -> @location(0) vec4f {",
   rayOnly?"var color=vec3f(0.0);":"var color=textureSampleLevel(inputTex,inputSampler,uv,0.0).rgb;",
   !rayOnly && effect.haze?SCENE_ATMOSPHERE_WGSL_HAZE:"",
   rayOnly?SCENE_ATMOSPHERE_WGSL_RAYS:effect.rays?"color+=textureSampleLevel(shaftsTex,inputSampler,uv,0.0).rgb;":"",
   effect.agx?"color=gosxAgX(color*atmo.p[12].x);color=select(1.055*pow(color,vec3f(1.0/2.4))-0.055,color*12.92,color<=vec3f(0.0031308));":"",
   effect.grain?SCENE_ATMOSPHERE_WGSL_GRAIN:"",
   effect.agx || effect.grain?"color+=vec3f(atmosphereHash(position.xy)-0.5)/255.0;":"",
   "return vec4f(max(color,vec3f(0.0)),1.0);}",
 ].join("\n");
}
function createSceneAtmospherePostWebGPU(host) {
 const device=host.device,data=new Float32Array(60),layouts=new Map();let rays=null;
 function release(){if(rays)rays.texture.destroy();rays=null;}
 function layout(key,depth,samples,composite) {
   if(layouts.has(key))return layouts.get(key);
   const entries=[{binding:0,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"float"}},
     {binding:1,visibility:GPUShaderStage.FRAGMENT,sampler:{type:"filtering"}},
     {binding:2,visibility:GPUShaderStage.FRAGMENT,buffer:{type:"uniform"}}];
   if(depth)entries.push({binding:3,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"depth",multisampled:samples>1}});
   if(composite)entries.push({binding:4,visibility:GPUShaderStage.FRAGMENT,texture:{sampleType:"float"}});
   const result=device.createBindGroupLayout({entries});layouts.set(key,result);return result;
 }
 function pass(args,effect,target,rayOnly) {
   const depth=rayOnly || effect.haze,key="atmosphere:"+(rayOnly?"rays":sceneAtmospherePostKey(effect))+":"+(depth?args.context.samples:1);
   const bgl=layout(key,depth,args.context.samples,!rayOnly && effect.rays);
   const pipeline=host.getPipeline(key,sceneAtmospherePostWGSL(effect,args.context.samples,rayOnly),bgl);
   const buffer=host.getParamBuffer(key,240);device.queue.writeBuffer(buffer,0,data);
   const entries=[{binding:0,resource:args.input},{binding:1,resource:host.sampler},{binding:2,resource:{buffer}}];
   if(depth)entries.push({binding:3,resource:args.context.depthView});
   if(!rayOnly && effect.rays)entries.push({binding:4,resource:rays.view});
   host.fullscreenPass(args.encoder,pipeline,device.createBindGroup({layout:bgl,entries}),target);
 }
 return {begin:function(effects){if(!effects.some(e=>e.rays))release();},
   apply:function(args){
     sceneAtmospherePostUniforms(args.effect,args.context,true,data);
     if(args.effect.rays){
       const scale=sceneAtmosphereQuality(args.context.meta).raySamples<=12?0.25:0.5;
       const w=Math.max(1,Math.round(args.width*scale)),h=Math.max(1,Math.round(args.height*scale));
       if(!rays || rays.width!==w || rays.height!==h){release();const texture=device.createTexture({label:"gosx-sun-shafts",size:[w,h,1],format:host.format,usage:GPUTextureUsage.RENDER_ATTACHMENT|GPUTextureUsage.TEXTURE_BINDING});rays={texture,view:texture.createView(),width:w,height:h};}
       pass(args,{rays:args.effect.rays},rays.view,true);
     }
     pass(args,args.effect,args.output,false);return args.output;
   },dispose:function(){release();layouts.clear();},
 };
}
