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
     pipeline=device.createRenderPipeline({label:"gosx-clouds",layout:pipelineLayout,vertex:{module,entryPoint:"vertexMain"},
       fragment:{module,entryPoint:"fragmentMain",targets:[{format:opts.format,blend:{color:blend,alpha:blend}}]},
       multisample:{count:opts.samples},depthStencil:{format:"depth24plus",depthWriteEnabled:false,depthCompare:"always"}});pipelines.set(key,pipeline);}
   pass.setPipeline(pipeline);pass.setBindGroup(0,group);pass.draw(3);
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
