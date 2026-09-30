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
