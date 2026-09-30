// Living daylight sky overlay, drawn before opaque geometry.
const SCENE_CLOUD_GLSL = [
 "float cloudHash(vec2 p) { p=fract(p*vec2(0.1031,0.1030)); p+=dot(p,p.yx+33.33); return fract((p.x+p.y)*p.x); }",
 "float cloudNoise(vec2 p) {",
 " vec2 i=floor(p), f=fract(p); f=f*f*(3.-2.*f);",
 " return mix(mix(cloudHash(i),cloudHash(i+vec2(1,0)),f.x),mix(cloudHash(i+vec2(0,1)),cloudHash(i+1.),f.x),f.y);",
 "}",
 "vec4 gosxClouds(vec3 ray, vec3 eye, vec4 settings, vec4 drift, vec3 ambient, vec3 sunLight, vec3 sun) {",
 " if(settings.x<=0.) return vec4(0.);",
 " vec3 origin=eye+vec3(0,6371000.,0);",
 " float b=dot(origin,ray), radius=6371000.+settings.y;",
 " float t=max(-b+sqrt(max(b*b-dot(origin,origin)+radius*radius,0.)),0.);",
 " vec2 p=(eye.xz+ray.xz*t-drift.xy*drift.z)/settings.z;",
 " float footprint=length(fwidth(p));",
 " float density=0., weight=0.52, total=0.;",
 " for(int i=0;i<5;i++) {",
 "  if(float(i)>=drift.w) break;",
 "  float filtered=1.-smoothstep(0.3,1.,footprint);",
 "  density+=weight*mix(0.5,cloudNoise(p),filtered); total+=weight;",
 "  p=p*2.03+vec2(17.1,9.2); footprint*=2.03; weight*=0.5;",
 " }",
 " density/=max(total,0.1);",
 " float threshold=1.-settings.x;",
 " float shape=smoothstep(threshold-0.12,threshold+0.18,density)*smoothstep(0.,0.025,settings.x);",
 " float tau=shape*3.5, beer=exp(-tau), powder=1.-exp(-tau*2.);",
 " float mu=max(dot(ray,sun),0.);",
 " float silver=pow(mu,32.)*exp(-tau*0.7)*(1.-beer)*2.2;",
 " vec3 lighting=ambient*(0.45+0.4*beer)+sunLight*(0.35*powder+silver)*smoothstep(-0.04,0.03,sun.y);",
 " vec3 horizon=gosxPhysicalSky(normalize(vec3(ray.x,0.025,ray.z)),u_cloud[7],u_cloud[8],vec4(u_cloud[9].xyz,2.),u_cloud[10].x)*u_cloud[3].w;",
 " lighting=mix(lighting,horizon,1.-exp(-t/65000.));",
 " float alpha=(1.-beer)*settings.w*smoothstep(-0.005,0.025,ray.y);",
 " return vec4(lighting*alpha,alpha);",
 "}"
].join("\n");
const SCENE_CLOUD_FRAGMENT_GLSL = [
 "#version 300 es", "precision highp float;", "in vec2 v_uv; out vec4 fragColor; uniform vec4 u_cloud[16];",
 "//SKY", "//CLOUD",
 "void main() {",
 " vec2 ndc=v_uv*2.-1.; vec3 ray=normalize(u_cloud[2].xyz+u_cloud[0].xyz*ndc.x*u_cloud[0].w+u_cloud[1].xyz*ndc.y*u_cloud[1].w);",
 " vec4 c=gosxClouds(ray,u_cloud[13].xyz,u_cloud[11],u_cloud[12],u_cloud[14].xyz,u_cloud[15].xyz,u_cloud[9].xyz);",
 " if(u_cloud[6].x==0.) { vec3 rgb=c.rgb/max(c.a,1e-5); c.rgb=mix(1.055*pow(rgb,vec3(1./2.4))-.055,rgb*12.92,lessThanEqual(rgb,vec3(.0031308)))*c.a; }",
 " fragColor=c; }",
].join("\n");
function createSceneCloudWebGL(gl) {
 const program=createScenePostProgram(gl,SCENE_CLOUD_FRAGMENT_GLSL.replace("//SKY",sceneSkyPhysicalSource("glsl")).replace("//CLOUD",SCENE_CLOUD_GLSL));
 if(!program) return null;
 const data=new Float32Array(64),loc=gl.getUniformLocation(program.program,"u_cloud[0]"),quad=createSceneFullscreenQuad(gl);
 return {draw:function(opts) {
   sceneCloudUniformData(opts,data);const cull=gl.isEnabled(gl.CULL_FACE);
   gl.useProgram(program.program);gl.uniform4fv(loc,data);
   gl.disable(gl.CULL_FACE);gl.disable(gl.DEPTH_TEST);gl.depthMask(false);
   gl.enable(gl.BLEND);gl.blendFuncSeparate(gl.ONE,gl.ONE_MINUS_SRC_ALPHA,gl.ONE,gl.ONE_MINUS_SRC_ALPHA);
   drawSceneFullscreenQuad(gl,quad.vao);
   gl.disable(gl.BLEND);gl.enable(gl.DEPTH_TEST);gl.depthMask(true);if(cull)gl.enable(gl.CULL_FACE);
 },dispose:function(){gl.deleteProgram(program.program);gl.deleteShader(program.vertexShader);gl.deleteShader(program.fragmentShader);gl.deleteVertexArray(quad.vao);gl.deleteBuffer(quad.vbo);}};
}
function sceneCloudWebGLDraw(resources,gl,opts) {
 const sky=opts.environment && opts.environment.sky,config=sky && sceneSkyClouds(sky.clouds);
 if(!config || sky.mode!=="physical" || config.coverage<=0 || !sceneAtmosphereQuality(opts.meta).clouds) {
   if(resources.clouds)resources.clouds.dispose();resources.clouds=null;return;
 }
 if(!resources.clouds)resources.clouds=createSceneCloudWebGL(gl);
 if(resources.clouds)resources.clouds.draw(opts);
}

function sceneOceanCloudGLSL() { return "uniform vec4 u_cloud[16];\n"+SCENE_CLOUD_GLSL; }
function sceneOceanCloudWebGL(gl,program,opts,data) {
  sceneCloudUniformData(Object.assign({},opts,{aspect:opts.aspect||1}),data);
  if(!sceneAtmosphereQuality(opts.meta).clouds)data[44]=0;
  gl.uniform4fv(gl.getUniformLocation(program,"u_cloud[0]"),data);
}

const SCENE_ATMOSPHERE_GLSL_COMMON = [
 "float atmosphereHash(vec2 p) { return fract(52.9829189*fract(dot(p,vec2(0.06711056,0.00583715)))); }",
 "vec3 atmosphereWorld(vec2 uv,float depth) {",
 " vec4 p=mat4(u_atmo[0],u_atmo[1],u_atmo[2],u_atmo[3])*vec4(uv*2.-1.,depth*2.-1.,1.); return p.xyz/p.w;",
 "}"
].join("\n");

const SCENE_ATMOSPHERE_GLSL_HAZE = [
 "float depth=texture(u_depth,v_uv).r;",
 " if(depth<0.999999) {",
 "  vec3 world=atmosphereWorld(v_uv,depth),delta=world-u_atmo[4].xyz;",
 "  float distance=length(delta);vec3 ray=delta/max(distance,1e-4);",
 "  float h=clamp(u_atmo[5].y*delta.y,-40.,40.);",
 "  float integral=abs(h)<0.001?1.-h*0.5:(1.-exp(-h))/h;",
 "  float optical=u_atmo[5].x*distance*exp(-u_atmo[5].y*max(u_atmo[4].y,0.))*integral;",
 "  float transmittance=exp(-min(optical,40.));",
 "  vec3 horizon=gosxPhysicalSky(normalize(vec3(ray.x,0.025,ray.z)),u_atmo[6],u_atmo[7],vec4(u_atmo[8].xyz,2.),u_atmo[9].x)*u_atmo[12].z;",
 "  float mu=max(dot(ray,u_atmo[8].xyz),0.);",
 "  float forward=pow(mu,12.)*u_atmo[5].z;",
 "  vec3 scatter=mix(horizon,u_atmo[14].xyz*0.7,forward);",
 "  color=mix(scatter,color,transmittance);",
 "  color+=vec3(atmosphereHash(gl_FragCoord.xy)-0.5)*(1.-transmittance)/1024.;",
 " }"
].join("\n");

const SCENE_ATMOSPHERE_GLSL_RAYS = [
 "vec3 shafts=vec3(0.);",
 " if(u_atmo[10].z>0.5 && u_atmo[11].x>0.) {",
 "  vec2 stepUV=(v_uv-u_atmo[10].xy)*u_atmo[11].z/max(u_atmo[11].w,1.);",
 "  vec2 sampleUV=v_uv-stepUV*(0.5+atmosphereHash(gl_FragCoord.xy)*0.25);",
 "  float weight=1.,sum=0.,normalization=0.;",
 "  for(int i=0;i<64;i++) {",
 "   if(float(i)>=u_atmo[11].w)break;",
 "   float mask=0.;",
 "   if(all(greaterThan(sampleUV,vec2(0.))) && all(lessThan(sampleUV,vec2(1.)))) {",
 "    float visible=step(0.999999,texture(u_depth,sampleUV).r);",
 "    vec2 r=sampleUV-u_atmo[10].xy;",
 "    mask=visible*exp(-dot(r,r)/0.012);",
 "   }",
 "   sum+=mask*weight;normalization+=weight;weight*=u_atmo[11].y;sampleUV-=stepUV;",
 "  }",
 "  float lowSun=1.-smoothstep(0.15,0.8,u_atmo[8].y);",
 "  shafts=u_atmo[14].xyz*u_atmo[11].x*sum/max(normalization,1e-3)*(0.15+0.85*lowSun);",
 " }",
 " color+=shafts;"
].join("\n");

const SCENE_ATMOSPHERE_GLSL_GRAIN = [
 "float grain=(atmosphereHash(gl_FragCoord.xy)-0.5)*u_atmo[12].y;",
 " color=clamp(color+grain*(0.4+0.6*sqrt(max(dot(color,vec3(0.2126,0.7152,0.0722)),0.))),0.,1.);"
].join("\n");
function sceneAtmospherePostGLSL(effect,rayOnly) {
 const needsDepth=rayOnly || effect.haze;
 return ["#version 300 es","precision highp float; precision highp sampler2D;",
   "in vec2 v_uv; out vec4 fragColor; uniform sampler2D u_texture; uniform vec4 u_atmo[15];",
   needsDepth?"uniform sampler2D u_depth;":"",
   !rayOnly && effect.rays?"uniform sampler2D u_shafts;":"",
   SCENE_ATMOSPHERE_GLSL_COMMON,
   effect.haze?sceneSkyPhysicalSource("glsl"):"",effect.agx?sceneAgXSource("glsl"):"",
   "void main(){",rayOnly?"vec3 color=vec3(0.);":"vec3 color=texture(u_texture,v_uv).rgb;",
   !rayOnly && effect.haze?SCENE_ATMOSPHERE_GLSL_HAZE:"",
   rayOnly?SCENE_ATMOSPHERE_GLSL_RAYS:effect.rays?"color+=texture(u_shafts,v_uv).rgb;":"",
   effect.agx?"color=gosxAgX(color*u_atmo[12].x); color=mix(1.055*pow(color,vec3(1./2.4))-.055,color*12.92,lessThanEqual(color,vec3(.0031308)));":"",
   effect.grain?SCENE_ATMOSPHERE_GLSL_GRAIN:"",
   effect.agx || effect.grain?"color+=vec3(atmosphereHash(gl_FragCoord.xy)-0.5)/255.;":"",
   "fragColor=vec4(max(color,vec3(0.)),1.);}",
 ].join("\n");
}
function createSceneAtmospherePostWebGL(host) {
 const gl=host.gl,data=new Float32Array(60);let rays=null;
 function release(){if(rays)disposeScenePostFBO(gl,rays);rays=null;}
 function pass(args,effect,target,w,h,rayOnly) {
   const key="atmosphere:"+(rayOnly?"rays":sceneAtmospherePostKey(effect));
   const program=host.getProgram(key,sceneAtmospherePostGLSL(effect,rayOnly));
   if(!program)return args.input;
   host.beginPostPass(program,args.input,target?target.fbo:null,w,h);
   gl.uniform4fv(gl.getUniformLocation(program.program,"u_atmo[0]"),data);
   if(rayOnly || effect.haze){scenePBRBindTexture(gl,1,args.depth,gl.TEXTURE_2D);gl.uniform1i(gl.getUniformLocation(program.program,"u_depth"),1);}
   if(!rayOnly && effect.rays){scenePBRBindTexture(gl,2,rays.colorTex,gl.TEXTURE_2D);gl.uniform1i(gl.getUniformLocation(program.program,"u_shafts"),2);}
   drawSceneFullscreenQuad(gl,host.quad.vao);return target?target.colorTex:null;
 }
 return {
   begin:function(effects){if(!effects.some(e=>e.rays))release();},
   apply:function(args){
     sceneAtmospherePostUniforms(args.effect,args.context,false,data);
     if(args.effect.rays){
       const scale=sceneAtmosphereQuality(args.context.meta).raySamples<=12?0.25:0.5;
       const w=Math.max(1,Math.round(args.width*scale)),h=Math.max(1,Math.round(args.height*scale));
       if(!rays || rays.width!==w || rays.height!==h){release();rays=createScenePostFBO(gl,w,h,false);}
       pass(args,{rays:args.effect.rays},rays,w,h,true);
     }
     return pass(args,args.effect,args.target,args.passWidth,args.passHeight,false);
   },dispose:release,
 };
}
