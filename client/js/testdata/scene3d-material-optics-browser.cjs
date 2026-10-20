'use strict';
// Real browser oracle for StandardMaterial spatial volume and geometric AA,
// plus selective specular bloom. Uses Node 22 builtins and the production
// renderers. WebGPU pixels are copied from a private COPY_SRC attachment;
// this certifies actual GPU shading, not native swapchain presentation.
// Usage: GOSX_CHROME_BIN=... node .../scene3d-material-optics-browser.cjs REPO ART
// GOSX_OPTICS_AUTHORED=1 exercises current authored chunks before regeneration.
const fs = require('node:fs'), path = require('node:path'), os = require('node:os');
const http = require('node:http'), assert = require('node:assert/strict');
const {spawn, execFileSync} = require('node:child_process');
const [repo, art] = process.argv.slice(2);
if (!repo || !art) throw new Error('expected repository and artifact directory');
fs.mkdirSync(art, {recursive:true});
const chromePath = process.env.GOSX_CHROME_BIN;
if (!chromePath) throw new Error('GOSX_CHROME_BIN must name an owned executable');
const version = execFileSync(chromePath, ['--version'], {encoding:'utf8'}).trim();
const authored = process.env.GOSX_OPTICS_AUTHORED === '1';
const materialOnly = process.env.GOSX_OPTICS_MATERIAL_ONLY === '1';
const source = name => fs.readFileSync(path.join(repo,'client/js',name),'utf8');
const harness = authored ? require(path.join(repo,'client/js/runtime-test-harness.js')) : null;
const chunks = new Map([['/runtime.js', source('bootstrap-runtime.js')]]);
const goArgs=['run'];if(process.env.GOSX_OPTICS_MODFILE)goArgs.push('-modfile='+process.env.GOSX_OPTICS_MODFILE);goArgs.push('./client/js/testdata/material-optics-fixture');
const customFixture=execFileSync('go',goArgs,{cwd:repo,encoding:'utf8',timeout:60000,env:{...process.env,GOWORK:'off'}});
for (const name of ['scene3d','scene3d-compute','scene3d-webgl','scene3d-webgpu']) {
  chunks.set('/'+name+'.js', authored ? harness.freshFeatureBundleSource(name) : source('bootstrap-feature-'+name+'.js'));
}
const errors=[], report={version, authored, materialOnly, backends:{}, errors};
const delay=ms=>new Promise(r=>setTimeout(r,ms));
const browserMain = async (backend, materialOnly) => {
  const W=256,H=192, api=window.__gosx_scene3d_api, canvas=document.querySelector('canvas');
  canvas.width=W;canvas.height=H;
  let renderer,device,target,gpuFormat,gl;
  window.opticsErrors=[];
  if(backend==='webgpu') {
    const create=GPUDevice.prototype.createShaderModule;GPUDevice.prototype.createShaderModule=function(descriptor){const module=create.call(this,descriptor);module.getCompilationInfo().then(info=>{for(const m of info.messages)if(m.type==='error')window.opticsErrors.push(descriptor.label+': '+m.lineNum+':'+m.linePos+' '+m.message);});return module;};
    const adapter=await navigator.gpu.requestAdapter({forceFallbackAdapter:true});
    if(!adapter)throw Error('WebGPU adapter required');
    device=await adapter.requestDevice();
    device.addEventListener('uncapturederror',e=>window.opticsErrors.push(e.error.message));
    window.__gosx_scene3d_webgpu_probe=()=>({adapter,device,ready:true});
    await new Promise((r,j)=>{const s=document.createElement('script');s.src='/scene3d-webgpu.js';s.onload=r;s.onerror=j;document.head.append(s);});
    gpuFormat=navigator.gpu.getPreferredCanvasFormat();
    const context={configure(options){gpuFormat=options.format;},getCurrentTexture(){
      if(!target||target.width!==canvas.width||target.height!==canvas.height){target?.destroy();target=device.createTexture({size:[canvas.width,canvas.height],format:gpuFormat,usage:GPUTextureUsage.RENDER_ATTACHMENT|GPUTextureUsage.COPY_SRC});}
      return target;
    }};
    const get=canvas.getContext.bind(canvas);canvas.getContext=(name,...args)=>name==='webgpu'?context:get(name,...args);
    renderer=window.__gosx_scene3d_webgpu_api.createRenderer(canvas,{});
  } else {
    renderer=api.sceneBackendRegistry.select({webgl:true,webgl2:true,webgpu:false,canvas:false,canvas2d:false}).create(canvas,{background:'#000000'},{tier:'full'});
    gl=canvas.getContext('webgl2');if(!gl)throw Error('WebGL2 required');

  }
  if(!renderer)throw Error('renderer creation failed');
  const camera={x:0,y:0,z:4,fov:45,near:.1,far:30};
  const environment={ambientColor:'#ffffff',ambientIntensity:1,skyColor:'#000000',skyIntensity:0,groundColor:'#000000',groundIntensity:0,exposure:1};
  const vertices=(waves=0)=>{
    const positions=[],normals=[],uvs=[],count=waves?256:1;
    const vertex=(x,y)=>{positions.push(x*2-1,y*2-1,0);const nx=waves?Math.sin(x*Math.PI*waves)*.75:0;const l=Math.hypot(nx,1);normals.push(nx/l,0,1/l);uvs.push(x,y);};
    for(let i=0;i<count;i++){const a=i/count,b=(i+1)/count;vertex(a,0);vertex(b,0);vertex(b,1);vertex(a,0);vertex(b,1);vertex(a,1);}
    return {positions,normals,uvs,count:positions.length/3};
  };
  const quad=(extra={})=>({id:'probe',kind:'mesh',materialKind:'standard',wireframe:false,color:'#ffffff',roughness:.2,metalness:0,vertices:vertices(),...extra});
  const bundle=(objects,postEffects=[],env=environment,lights=[])=>{
    const b=api.createSceneRenderBundle(canvas.width,canvas.height,'#000000',camera,objects.map((o,i)=>api.normalizeSceneObject(o,i,null)),[],[],[],lights,env,0,[],[],[],[],[],0,false);
    b.postEffects=postEffects;return b;
  };
  async function read(b,frameMeta){
    for(let attempt=0;attempt<100;attempt++){renderer.render(b,{width:canvas.width,height:canvas.height},frameMeta);if(!device||(target&&renderer.diagnostics().pipelinePending===0))break;await new Promise(r=>setTimeout(r,20));}
    let out;
    if(device){
      if(!target)return new Uint8Array(canvas.width*canvas.height*4);
      const row=Math.ceil(canvas.width*4/256)*256;
      const buffer=device.createBuffer({size:row*canvas.height,usage:GPUBufferUsage.COPY_DST|GPUBufferUsage.MAP_READ});
      const encoder=device.createCommandEncoder();encoder.copyTextureToBuffer({texture:target},{buffer,bytesPerRow:row},[canvas.width,canvas.height]);device.queue.submit([encoder.finish()]);await buffer.mapAsync(GPUMapMode.READ);
      const bytes=new Uint8Array(buffer.getMappedRange());out=new Uint8Array(canvas.width*canvas.height*4);
      for(let y=0;y<canvas.height;y++)out.set(bytes.subarray(y*row,y*row+canvas.width*4),y*canvas.width*4);
      if(gpuFormat.startsWith('bgra'))for(let i=0;i<out.length;i+=4){const r=out[i];out[i]=out[i+2];out[i+2]=r;}
      buffer.unmap();buffer.destroy();
    }else{
      gl.bindFramebuffer(gl.READ_FRAMEBUFFER,null);gl.readBuffer(gl.BACK);
      const raw=new Uint8Array(canvas.width*canvas.height*4);gl.readPixels(0,0,canvas.width,canvas.height,gl.RGBA,gl.UNSIGNED_BYTE,raw);
      out=new Uint8Array(raw.length);for(let y=0;y<canvas.height;y++)out.set(raw.subarray(y*canvas.width*4,(y+1)*canvas.width*4),(canvas.height-y-1)*canvas.width*4);
      const error=gl.getError();if(error!==gl.NO_ERROR)throw Error('WebGL error '+error);
    }
    return out;
  }
  const image=pixels=>{const c=document.createElement('canvas');c.width=canvas.width;c.height=canvas.height;c.getContext('2d').putImageData(new ImageData(new Uint8ClampedArray(pixels),c.width,c.height),0,0);return c.toDataURL('image/png').split(',')[1];};
  const sample=(p,x,y=H/2)=>Array.from(p.slice((Math.floor(y)*canvas.width+Math.floor(x))*4,(Math.floor(y)*canvas.width+Math.floor(x))*4+3));
  const delta=(a,b)=>a.reduce((sum,n,i)=>sum+(i%4!==3?Math.abs(n-b[i]):0),0);
  const map=document.createElement('canvas');map.width=4;map.height=1;const mc=map.getContext('2d'),md=mc.createImageData(4,1);
  for(let i=0;i<4;i++)md.data.set([255,i*85,0,255],i*4);mc.putImageData(md,0,0);
  const glass={transmission:1,ior:1,thickness:2,attenuationColor:[.25,.5,1],attenuationDistance:1,roughness:.05};
  const backdrop=quad({id:'backdrop',z:-.2,scale:2,unlit:true});
  const mapped=bundle([backdrop,quad({...glass,thicknessMap:map.toDataURL()})]);
  let thickness;for(let i=0;i<100;i++){thickness=await read(mapped);if(sample(thickness,86)[0]>sample(thickness,170)[0]+20)break; if(i===99){window.debugImage=image(thickness);}await new Promise(r=>setTimeout(r,25));}
  const uniform=await read(bundle([backdrop,quad(glass)]));

  const columns=[86,114,142,170].map(x=>sample(thickness,x));
  if(!(columns[0][0]>columns[3][0]+20&&columns[0][1]>columns[3][1]+10))throw Error('green thickness must vary absorption: '+JSON.stringify({columns,gpuErrors:window.opticsErrors}));
  if(delta(thickness,uniform)<20000)throw Error('thickness texture has no pixel effect');
  const aa={variance:.15,threshold:.2};
  const metal={color:'#b0b0b0',metalness:1,roughness:.045};
  const dark={...environment,ambientIntensity:0,skyIntensity:0,groundIntensity:0};
  const lights=[{kind:'directional',color:'#ffffff',intensity:1.2,directionX:0,directionY:0,directionZ:-1}];
  const frames=async enabled=>{const result=[];for(let i=0;i<12;i++)result.push(await read(bundle([quad({...metal,vertices:vertices(72),x:i*.002,specularAA:enabled?aa:null})],[],dark,lights)));return result;};
  const off=await frames(false),on=await frames(true);
  // Normalize temporal variation by mean highlight energy: AA broadens a
  // subpixel lobe, so raw byte differences alone reward a disappearing light.
  const flicker=frames=>{let change=0,energy=0;for(let i=0;i<frames.length;i++)for(let y=60;y<132;y++)for(let x=78;x<178;x++)for(let c=0;c<3;c++){const p=(y*W+x)*4+c;energy+=frames[i][p];if(i)change+=Math.abs(frames[i][p]-frames[i-1][p]);}return {change,energy,relative:change/(energy/frames.length)};};
  const offFlicker=flicker(off),onFlicker=flicker(on);
  window.debugImages={'aa-off':image(off[0]),'aa-on':image(on[0])};
  if(!(offFlicker.change>1000&&onFlicker.relative<offFlicker.relative*.8))throw Error('AA does not reduce relative highlight flicker: '+JSON.stringify({offFlicker,onFlicker}));
  const flatOff=await read(bundle([quad(metal)],[],dark,lights)),flatOn=await read(bundle([quad({...metal,specularAA:aa})],[],dark,lights));
  if(delta(flatOff,flatOn)!==0)throw Error('AA changes flat geometric normals');
  const result={thicknessColumns:columns,offFlicker,onFlicker,images:{thickness:image(thickness),'aa-off':image(off[0]),'aa-on':image(on[0])}};
  if(materialOnly){renderer.dispose();target?.destroy();await device?.queue.onSubmittedWorkDone();if(window.opticsErrors.length)throw Error(window.opticsErrors.join("\n"));return result;}
  // Bloom controls share the same tone/linear targets; threshold changes only
  // extraction. A high threshold is the zero-source control, not a different
  // display pipeline.
  const post=threshold=>[{kind:'bloom',source:'specular',threshold,intensity:1.5,radius:1}];
  const diffuse=quad({specularIntensity:0,color:'#ffffff'});
  const diffuseOff=await read(bundle([diffuse],post(1000))),diffuseOn=await read(bundle([diffuse],post(.1)));
  if(delta(diffuseOff,diffuseOn)!==0)throw Error('white diffuse leaked into selective bloom');
  const shine=quad({...metal,roughness:.12,scaleX:.45,scaleY:.45});
  const metalOff=await read(bundle([shine],post(1000),dark,lights)),metalOn=await read(bundle([shine],post(.1),dark,lights));
  const bloomDelta=delta(metalOff,metalOn);if(bloomDelta<10000)throw Error('specular highlight did not bloom: '+JSON.stringify({bloomDelta,offEnergy:metalOff.reduce((s,v,i)=>s+(i%4===3?0:v),0),onEnergy:metalOn.reduce((s,v,i)=>s+(i%4===3?0:v),0),diagnostics:renderer.diagnostics?.()}));
  const cover=quad({id:'cover',z:.1,color:'#000000',specularIntensity:0});
  const occludedOff=await read(bundle([shine,cover],post(1000),dark,lights)),occludedOn=await read(bundle([shine,cover],post(.1),dark,lights));
  if(delta(occludedOff,occludedOn)!==0)throw Error('occluded specular leaked into bloom');
  result.bloomDelta=bloomDelta;result.images['selective-bloom']=image(metalOn);result.images['diffuse-only']=image(diffuseOn);
  const fixture=await (await fetch('/custom.json')).json();
  const customObject=name=>{const state=api.createSceneState({scene:fixture[name]});const object=api.sceneStateObjectsWithMaterials(state)[0];return quad({...object,vertices:vertices(),x:0,y:0,z:0});};
  const custom=customObject('contributing');
  if(!custom.specularFragmentGLSL||!custom.specularFragmentWGSL)throw Error('compiled shader library lost MRT companions');
  const customOff=await read(bundle([custom],post(1000),dark,lights)),customOn=await read(bundle([custom],post(.1),dark,lights));
  const customDelta=delta(customOff,customOn);if(customDelta<10000)throw Error('authored Selena specular did not bloom: '+customDelta);
  const plain={...customObject('plain'),id:'wood-cover',z:.1};
  const customCoverOff=await read(bundle([custom,plain],post(1000),dark,lights)),customCoverOn=await read(bundle([custom,plain],post(.1),dark,lights));
  if(delta(customCoverOff,customCoverOn)!==0)throw Error('zero-companion Selena did not occlude specular');
  result.customBloomDelta=customDelta;result.images['selena-bloom']=image(customOn);result.images['selena-occlusion']=image(customCoverOn);
  canvas.width=192;canvas.height=128;
  await read(bundle([quad(metal)],post(.1),dark,lights));
  await read(bundle([quad(metal)],[],dark,lights));
  await read(bundle([quad({...metal,specularAA:aa})],post(.1),dark,lights));
  renderer.dispose();target?.destroy();await device?.queue.onSubmittedWorkDone();
  if(window.opticsErrors.length)throw Error(window.opticsErrors.join('\n'));
  return result;
};
const html=backend=>`<!doctype html><meta charset="utf-8"><canvas></canvas><script src="/runtime.js"></script><script src="/scene3d.js"></script><script src="/scene3d-compute.js"></script>${backend==='webgl'?'<script src="/scene3d-webgl.js"></script>':''}<script>window.done=(${browserMain})(${JSON.stringify(backend)},${materialOnly});window.done.catch(()=>{});</script>`;
const server=http.createServer((req,res)=>{if(req.url==='/custom.json'){res.writeHead(200,{'Content-Type':'application/json'});res.end(customFixture);}else if(chunks.has(req.url)){res.writeHead(200,{'Content-Type':'text/javascript'});res.end(chunks.get(req.url));}else if(['/webgl','/webgpu'].includes(req.url)){res.writeHead(200,{'Content-Type':'text/html'});res.end(html(req.url.slice(1)));}else{res.writeHead(404);res.end();}});
let chrome,ws;const pending=new Map();let nextID=0;const profile=fs.mkdtempSync(path.join(os.tmpdir(),'gosx-optics-'));
function send(method,params={},sessionId){return new Promise((resolve,reject)=>{const id=++nextID;const timer=setTimeout(()=>{pending.delete(id);reject(Error('CDP timeout '+method));},60000);pending.set(id,{resolve,reject,timer});ws.send(JSON.stringify({id,method,params,sessionId}));});}
(async()=>{try{
  await new Promise(r=>server.listen(0,'127.0.0.1',r));
  chrome=spawn(chromePath,['--headless','--no-sandbox','--remote-debugging-port=0','--user-data-dir='+profile,'--ignore-gpu-blocklist','--enable-unsafe-swiftshader','--enable-unsafe-webgpu','--use-gl=angle','--use-angle=swiftshader','--use-webgpu-adapter=swiftshader','--use-gpu-in-tests','about:blank'],{stdio:['ignore','ignore','pipe']});
  let stderr='';chrome.stderr.on('data',d=>{stderr+=d;fs.writeFileSync(path.join(art,'chrome.log'),stderr);});
  let endpoint;for(let i=0;i<150&&!endpoint;i++){endpoint=stderr.match(/DevTools listening on (ws:\/\/\S+)/)?.[1];await delay(100);}if(!endpoint)throw Error('Chrome CDP unavailable');
  ws=new WebSocket(endpoint);await new Promise((r,j)=>{ws.onopen=r;ws.onerror=j;});
  ws.onmessage=({data})=>{const m=JSON.parse(data);if(m.id&&pending.has(m.id)){const p=pending.get(m.id);pending.delete(m.id);clearTimeout(p.timer);m.error?p.reject(Error(m.error.message)):p.resolve(m.result);}else if(m.method==='Runtime.exceptionThrown')errors.push(m.params.exceptionDetails.exception?.description||m.params.exceptionDetails.text);else if(m.method==='Runtime.consoleAPICalled'&&['error','warning'].includes(m.params.type))errors.push(m.params.args.map(a=>a.value||a.description).join(' '));};
  for(const backend of ['webgl','webgpu']){
    const {targetId}=await send('Target.createTarget',{url:'about:blank'});const {sessionId}=await send('Target.attachToTarget',{targetId,flatten:true});await send('Runtime.enable',{},sessionId);
    await send('Page.navigate',{url:'http://127.0.0.1:'+server.address().port+'/'+backend},sessionId);
    let ready=false;for(let i=0;i<100&&!ready;i++){const q=await send('Runtime.evaluate',{expression:'!!window.done',returnByValue:true},sessionId);ready=q.result?.value;await delay(50);}if(!ready)throw Error('probe did not start');
    const r=await send('Runtime.evaluate',{expression:'window.done',awaitPromise:true,returnByValue:true},sessionId);if(r.exceptionDetails){const debug=await send('Runtime.evaluate',{expression:'window.debugImages||{}',returnByValue:true},sessionId);for(const [n,d]of Object.entries(debug.result.value||{}))fs.writeFileSync(path.join(art,backend+'-'+n+'.png'),Buffer.from(d,'base64'));const snap=await send('Page.captureScreenshot',{format:'png'},sessionId);fs.writeFileSync(path.join(art,backend+'-failure.png'),Buffer.from(snap.data,'base64'));throw Error(r.exceptionDetails.exception?.description||r.exceptionDetails.text);}
    const result=r.result.value;for(const [name,data]of Object.entries(result.images)){fs.writeFileSync(path.join(art,backend+'-'+name+'.png'),Buffer.from(data,'base64'));}delete result.images;report.backends[backend]=result;
    await send('Target.closeTarget',{targetId});
  }
  assert.deepEqual(errors,[]);report.passed=true;
}catch(error){report.failure=error.stack;process.exitCode=1;}finally{
  fs.writeFileSync(path.join(art,'report.json'),JSON.stringify(report,null,2));console.log(JSON.stringify(report,null,2));
  ws?.close();for(const p of pending.values()){clearTimeout(p.timer);p.reject(Error('closed'));}chrome?.kill('SIGTERM');await delay(300);chrome?.kill('SIGKILL');server.close();fs.rmSync(profile,{recursive:true,force:true});
}})();
