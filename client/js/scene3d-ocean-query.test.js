'use strict';
const test = require('node:test'), assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path'), vm = require('node:vm');
const root = path.join(__dirname, '../..'), context = { window: {} };
for (const name of ['ocean-waves', 'ocean-query']) vm.runInNewContext(fs.readFileSync(path.join(root, 'client/runtime/scene3d', name + '.ts'), 'utf8'), context);
const api = context.window.__gosx_scene3d_ocean_query;
const near = (a, b, eps = 1e-4) => assert.ok(Math.abs(a-b) <= eps, `${a} != ${b}`);
// Independent GLSL/WGSL vertex arithmetic, including the texture shoal and surge.
function reference(o, quality, floor, x, z, time) {
  const weights = [1,.62,.42,.28,.19,.13], ratios = [1,.73,.53,.39,.28,.21], angles = [0,.38,-.46,.83,-.95,1.4];
  const f = Math.fround, count = quality === 'low' ? 4 : 6, sum = weights.slice(0,count).reduce((s,a)=>s+a*a,0);
  const smooth = (a,b,v) => { const t = Math.max(0,Math.min(1,(v-a)/(b-a))); return t*t*(3-2*t); };
  const p=[x,f(o.level || 0),z], dx=[1,0,0], dz=[0,0,1], depth=p[1]-floor(x,z), t=f(time);
  for(let i=0;i<count;i++) {
    const angle=(o.windDirection||0)*Math.PI/180+angles[i], k0=2*Math.PI/((o.waveLength||18)*ratios[i]);
    const wx=f(Math.sin(angle)), wz=f(Math.cos(angle)), k=f(k0), omega=f(Math.sqrt(9.81*k0)*f(o.speed||1));
    const shoal=smooth(0,1.2,depth*k), a=f(weights[i]*(o.waveHeight||.8)/Math.sqrt(8*sum))*shoal, qa=f((o.choppiness||.6)/(k0*count))*shoal;
    const theta=k*(wx*x+wz*z)-omega*t+f(((i*.6180339887)%1)*2*Math.PI), s=Math.sin(theta), c=Math.cos(theta);
    const dp=[qa*wx*c,a*s,qa*wz*c], ddx=[-k*qa*wx*wx*s,k*a*wx*c,-k*qa*wx*wz*s], ddz=[-k*qa*wx*wz*s,k*a*wz*c,-k*qa*wz*wz*s];
    for(let j=0;j<3;j++) { p[j]+=dp[j]; dx[j]+=ddx[j]; dz[j]+=ddz[j]; }
  }
  const phase=t*f(o.speed||1)/9+.15*Math.sin(x*.07+1.3)+.08*Math.sin(x*.19), ph=phase-Math.floor(phase);
  p[1]+=f(o.surf||.5)*.45*smooth(0,.28,ph)*(1-smooth(.28,1,ph))*(1-smooth(.3,4,depth));
  const n=[dz[1]*dx[2]-dz[2]*dx[1],dz[2]*dx[0]-dz[0]*dx[2],dz[0]*dx[1]-dz[1]*dx[0]], len=Math.hypot(...n);
  return { p, n:n.map(v=>v/len) };
}
test('CPU matches GPU reference and solves horizontal displacement over 5832 samples', () => {
  let samples=0;
  for(const o of [{waveHeight:.9,waveLength:17,choppiness:.7,windDirection:8,speed:1,surf:.6},
    {waveHeight:3,waveLength:30,choppiness:1,windDirection:133,speed:1.7,surf:.8},
    {waveHeight:6,waveLength:120,choppiness:.9,windDirection:301,speed:.4,surf:.2,level:1}])
    for(const quality of ['high','low']) for(const floor of [()=>-1e4,()=>-.2,(x,z)=>-.5+.02*x+.015*z]) {
      const q=api.create(o,quality,floor);
      for(const time of [0,.7,9.1,63]) for(let x=-20;x<=20;x+=5) for(let z=-20;z<=20;z+=5) {
        const actual=api.evaluate(q,x,z,time), r=reference(o,quality,floor,x,z,time);
        ['x','y','z'].forEach((axis,j)=>{near(actual[axis],r.p[j]);near(actual.normal[axis],r.n[j]);});
        const sample=api.sample(q,x,z,time), rr=reference(o,quality,floor,sample.parameterX,sample.parameterZ,time);
        near(sample.x,x);near(sample.z,z);near(sample.y,rr.p[1]);samples++;
      }
    }
  assert.equal(samples,5832);
});
test('analytic water velocity matches time derivative including run-up', () => {
  const q=api.create({windDirection:8,speed:1.3,surf:.7},'high',()=>-.6), t=.6,h=.001;
  const p=api.evaluate(q,3,-1,t),a=api.evaluate(q,3,-1,t-h),b=api.evaluate(q,3,-1,t+h);
  ['x','y','z'].forEach(axis=>near(p.velocity[axis],(b[axis]-a[axis])/(2*h),1e-4));
});
test('Go and JavaScript share golden position, normal and velocity samples', () => {
  const cases=JSON.parse(fs.readFileSync(path.join(root,'scene/testdata/ocean-query-parity.json'),'utf8'));
  for(const c of cases) {
    const p=api.sample(api.create(c.ocean,c.quality,()=>c.floor),c.x,c.z,c.time);
    for(const axis of ['x','y','z']) { near(p[axis],c.position[axis],1e-7);near(p.normal[axis],c.normal[axis],1e-7);near(p.velocity[axis],c.velocity[axis],1e-7); }
  }
});
test('canonical wave table matches the generated runtime module', () => {
  assert.deepEqual(JSON.parse(JSON.stringify(context.window.__gosx_scene3d_ocean_waves.constants)),JSON.parse(fs.readFileSync(path.join(root,'scene/ocean_waves.json'),'utf8')));
});
test('bathymetry filters R before signed-sqrt decoding and clamps texel centres', () => {
  const image={width:2,height:2,data:new Uint8Array([0,0,0,255,255,0,0,255,0,0,0,255,255,0,0,255])};
  const mapping={minX:0,minZ:0,maxX:2,maxZ:2,minHeight:-6,maxHeight:6,encoding:'signed-sqrt'};
  const floor=api.bathymetry(image,mapping);near(floor(1,1),0);near(floor(.75,1),-1.5);near(floor(0,2),-6);
});
test('finite bathymetry never extrudes its land edge into the open ocean',()=>{
 const image={width:1,height:1,data:new Uint8Array([255,0,0,255])};
 for(const encoding of ['linear','signed-sqrt']) {
  const floor=api.bathymetry(image,{minX:-80,minZ:-60,maxX:80,maxZ:60,minHeight:-8,maxHeight:8,encoding});
  near(floor(80,60),8);
  for(const [x,z] of [[80.01,0],[-80.01,0],[0,60.01],[0,-60.01],[5000,-5000]])near(floor(x,z),-1e4);
  const q=api.create({waveHeight:1.15},'high',floor);
  const water=api.sample(q,5000,-5000,8);
  assert.ok(Number.isFinite(water.y) && Math.abs(water.y)<2,'swell continues outside the chart');
 }
});
test('public zero-valued Ocean props match omitted defaults; normalized values retain zeros', () => {
  const defaults=api.create({},'high'),zero=api.create({waveHeight:0,waveLength:0,choppiness:0,speed:0,surf:0},'high');
  assert.deepEqual(Array.from(zero.data),Array.from(defaults.data));
  const stopped=api.create({waveHeight:.8,waveLength:18,choppiness:0,speed:0,surf:0},'high',undefined,true);
  near(api.evaluate(stopped,3,-2,0).y,api.evaluate(stopped,3,-2,5).y);
  for (let i=0;i<6;i++) near(stopped.data[36+i*8+5],0);
});
test('spectral ocean: Go and JavaScript share golden samples of the swell waves', () => {
  const cases=JSON.parse(fs.readFileSync(path.join(root,'scene/testdata/ocean-spectrum-query-parity.json'),'utf8'));
  assert.ok(cases.length>=16);
  for(const c of cases) {
    const q=api.create(c.ocean,'high',x=>(c.ocean.level||0)-c.floorOffset-c.floorSlope*x,true);
    assert.equal(q.swell.length,24);
    const p=api.sample(q,c.x,c.z,c.time);
    for(const axis of ['x','y','z']) { near(p[axis],c.position[axis],1e-6);near(p.normal[axis],c.normal[axis],1e-6);near(p.velocity[axis],c.velocity[axis],1e-6); }
  }
});
test('spectral ocean: velocity and tangents are derivatives of the position, and time is not rounded to float32', () => {
  const cases=JSON.parse(fs.readFileSync(path.join(root,'scene/testdata/ocean-spectrum-query-parity.json'),'utf8'));
  const c=cases[10], q=api.create(c.ocean,'high',()=>-1e4,true), h=1e-4, t=c.time+0.123456789;
  const p=api.evaluate(q,3,-1,t), a=api.evaluate(q,3,-1,t-h), b=api.evaluate(q,3,-1,t+h);
  ['x','y','z'].forEach(axis=>near(p.velocity[axis],(b[axis]-a[axis])/(2*h),1e-5));
  const dx=api.evaluate(q,3+h,-1,t), mx=api.evaluate(q,3-h,-1,t);
  near(p.tangentX.y,(dx.y-mx.y)/(2*h),1e-5);near(p.tangentX.x,(dx.x-mx.x)/(2*h),1e-5);
  assert.notEqual(api.evaluate(q,0,0,1234.5).y,api.evaluate(q,0,0,Math.fround(1234.5)+1e-3).y);
});
test('a spectral ocean without swell waves keeps the Gerstner table', () => {
  const o={waveHeight:.9,waveLength:17,choppiness:.7,windDirection:8,speed:1,surf:.6};
  assert.deepEqual(Array.from(api.create({...o,spectrum:{seed:1}},'high').data),Array.from(api.create(o,'high').data));
  assert.equal(api.create({...o,spectrum:{seed:1}},'high').swell,null);
});
