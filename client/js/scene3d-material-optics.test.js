"use strict";
const test=require('node:test'), assert=require('node:assert/strict'), path=require('node:path');
const {spawnSync}=require('node:child_process');
const {readSceneRendererBackendSrc}=require('./scene3d-renderer-source-set.js');
const {createBoardWebGPUHarness,createWebGLRendererForPost,flushAsyncWork}=require('./runtime-test-harness.js');

function opticsBundle(api,material){return api.createSceneRenderBundle(64,64,'#000000',{x:0,y:0,z:5,fov:60,near:.1,far:100},[api.normalizeSceneObject({id:'solid',kind:'mesh',width:1,height:1,depth:1,wireframe:false,vertices:{positions:[-1,-1,0,1,-1,0,0,1,0],normals:[0,0,1,0,0,1,0,0,1],uvs:[0,0,1,0,.5,1],count:3},materialKind:'standard',...material},0,null)],[],[],[],[],{},0,[],[],[],[],[],0,false);}

for(const backend of ['WebGL','WebGPU'])test(`${backend} thickness and geometric-AA controls reach a draw and restore neutral on removal`,async t=>{
 const h=backend==='WebGPU'?await createBoardWebGPUHarness({fresh:true}):createWebGLRendererForPost({fresh:true});t.after(()=>h.renderer.dispose());
 const api=h.env.context.__gosx_scene3d_api;
 const enabled=opticsBundle(api,{thicknessMap:'/thickness.png',thickness:2,transmission:.7,specularAA:{variance:.15,threshold:.2}});
 const disabled=opticsBundle(api,{thickness:2,transmission:.7});
 assert.notEqual(enabled.materials[0].key,disabled.materials[0].key,'optics controls participate in cache identity');
 assert.equal(enabled.materials[0].thicknessMap,'/thickness.png');
 if(backend==='WebGL'){
  const gl=h.canvas.getContext('webgl2');const aa=[];const original=gl.uniform2f;gl.uniform2f=(loc,x,y)=>{if(loc?.name==='u_specularAA')aa.push([x,y]);original.call(gl,loc,x,y);};
  h.renderer.render(enabled,{width:64,height:64});await flushAsyncWork();h.renderer.render(enabled,{width:64,height:64});
  assert.ok(gl.ops.some(o=>o[0]==='uniform1i'&&o[1]==='u_hasThicknessMap'&&o[2]===1),'loaded map enables shader sampling');
  assert.ok(gl.ops.some(o=>o[0]==='uniform1i'&&o[1]==='u_thicknessMap'&&o[2]===8),'map uses unique material sampler slot');
  h.renderer.render(disabled,{width:64,height:64});assert.deepEqual(aa.at(-1),[0,0]);assert.ok(aa.some(v=>v[0]===.15&&v[1]===.2));
 }else{
  h.renderer.render(enabled,{width:64,height:64});await flushAsyncWork();h.renderer.render(enabled,{width:64,height:64});
  const groups=h.fake.state.renderPasses.flatMap(p=>p.bindGroups).filter(b=>b.slot===1&&b.group?.desc?.entries?.length===19).map(b=>b.group);assert.ok(groups.length,'material group is bound');
  const group=groups.at(-1),entries=group.desc.entries;assert.ok(entries.find(e=>e.binding===17)&&entries.find(e=>e.binding===18),'thickness texture and sampler are present');
  const buffer=entries.find(e=>e.binding===0).resource.buffer;
  const data=h.fake.state.writeBufferCalls.filter(w=>w.buffer===buffer).at(-1).data;
  assert.equal(data.length,72,'reuse material padding instead of increasing uniform allocation');assert.ok(Math.abs(data[63]-.15)<1e-6);assert.ok(Math.abs(data[67]-.2)<1e-6);assert.equal(new Uint32Array(data.buffer,data.byteOffset,72)[71],1,'loaded thickness flag reaches GPU');
  h.renderer.render(disabled,{width:64,height:64});
  const neutral=h.fake.state.writeBufferCalls.filter(w=>w.data?.length===72).at(-1).data;
  assert.equal(neutral[63],0);assert.equal(neutral[67],0);assert.equal(new Uint32Array(neutral.buffer,neutral.byteOffset,72)[71],0,'removed map clears loaded flag');
 }
});

test('WebGL clears prior glass depth after a post-processed frame', async t => {
 const h=createWebGLRendererForPost({fresh:true});t.after(()=>h.renderer.dispose());
 const api=h.env.context.__gosx_scene3d_api, gl=h.canvas.getContext('webgl2');
 const b=opticsBundle(api,{transmission:1,thickness:2});
 let writable=true;const clears=[];
 const depthMask=gl.depthMask.bind(gl),clear=gl.clear.bind(gl);
 gl.depthMask=value=>{writable=value;depthMask(value);};
 gl.clear=mask=>{if(mask&gl.DEPTH_BUFFER_BIT)clears.push(writable);clear(mask);};
 h.renderer.render(b,{width:64,height:64});
 gl.depthMask(false); // Actual post passes leave this state for the next frame.
 h.renderer.render(b,{width:64,height:64});
 assert.ok(clears.length>=2,'both main frames clear depth');
 assert.ok(clears.every(Boolean),'depth clears must remain effective after fullscreen passes');
});

test('actual GLSL optics: green spatial absorption, flat-normal neutrality, bounded geometric roughness',t=>{
 const source=readSceneRendererBackendSrc('webgl');
 const aa=source.slice(source.indexOf('    "float gsxSpecularRoughness'),source.indexOf('    GLSL_TRANSMISSION,')).split('\n').map(l=>l.trim()).filter(l=>l.startsWith('"')).map(l=>JSON.parse(l.replace(/,$/,''))).join('\n');
 const transmission=source.match(/const GLSL_TRANSMISSION = `([\s\S]*?)`;/)[1];
 const fragment=`#version 300 es
precision highp float; in vec2 v_uv; out vec4 fragColor;
#define GOSX_HDR_IBL 0
uniform int u_mode; uniform vec2 u_aa;
const float u_envRotation=0.0;const bool u_hasEnvMap=false;
uniform sampler2D u_envMap;const float u_envMapMaxLod=0.0;const float u_envIntensity=0.0;
const vec3 u_ambientColor=vec3(1);const float u_ambientIntensity=1.0;
const vec3 u_skyColor=vec3(0);const float u_skyIntensity=0.0;
const vec3 u_groundColor=vec3(0);const float u_groundIntensity=0.0;
uniform mat4 u_viewMatrix;
vec3 rotateEnvY(vec3 v,float a){return v;}vec2 envEquirectUV(vec3 v){return vec2(0);}
${aa}\n${transmission}
void main(){if(u_mode<2){fragColor=vec4(volumeTransmission(vec3(0),vec3(0,0,1),vec3(0,0,1),0.2,volumeThickness(v_uv)),1);return;}
 vec3 n=u_mode==2?vec3(0,0,1):normalize(vec3(sin(v_uv.x*100.0)*0.8,0,1));
 float rough=gsxSpecularRoughness(n,0.2,u_aa);fragColor=vec4(rough,0.2,0,1);}`;
 const run=spawnSync('python3',[path.join(__dirname,'testdata/scene3d-material-optics-pixels.py')],{input:JSON.stringify({fragment}),encoding:'utf8',timeout:30000,env:{...process.env,LIBGL_ALWAYS_SOFTWARE:'1',GALLIUM_DRIVER:'llvmpipe'}});
 assert.equal(run.status,0,run.stderr);const rows=JSON.parse(run.stdout);if(rows.skip){t.skip(rows.skip);return;}
 for(const [i,g]of [0,64/255,128/255,1].entries()){const actual=rows[0][i*16+8];for(const [c,tint]of [.25,.5,1].entries())assert.ok(Math.abs(actual[c]/255-Math.pow(tint,2*g))<.012,`linear green absorption band${i} channel${c}`);}
 assert.ok(rows[1].every(p=>Math.abs(p[0]-16)<=1),'missing map keeps scalar thickness');
 for(const index of [2,4,5])assert.ok(rows[index].every(p=>p[0]===p[1]),'flat/disabled AA preserves roughness');
 assert.ok(rows[3].some(p=>p[0]>p[1]+20),'geometric variation broadens the lobe');
 assert.ok(rows[3].every(p=>p[0]>=p[1]&&p[0]<=172),'AA never sharpens or exceeds bounded variance');
});
