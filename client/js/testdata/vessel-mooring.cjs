'use strict';
// Run by the beacon Go test with the real authored vessel and collision map.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const root=process.argv[2],context={window:{},atob};
for(const name of ['vessel-physics','walk-surfaces']) {
 const file=root+'/client/runtime/scene3d/'+name+'.ts';
 vm.runInNewContext(fs.readFileSync(file,'utf8'),context);
}
// Ground decoding/sampling lives in the lazy walk bundle, with no DOM work at load.
vm.runInNewContext(fs.readFileSync(root+'/client/js/bootstrap-feature-scene3d-walk.js','utf8'),context);
const {vessel,walk,ocean}=JSON.parse(fs.readFileSync(0,'utf8'));
const physics=context.window.__gosx_scene3d_vessel_physics,walkAPI=context.window.__gosx_scene3d_walk_api;
const ground=walkAPI.decodeGround(walk.ground),floor=(x,z)=>walkAPI.sampleGround(ground,x,z);
for(const rudder of [1]) {
 const s=physics.create(vessel,ocean,walk),start=s.heading;s.mode='sailing';s.trim=1;
 assert.ok(physics.allowed(s,s.x,s.z,s.heading,floor),'authored mooring must start clear of collisions');
 let five;
 for(let i=0;i<600;i++) {
  physics.advance(s,1/60,i/60,{rudder},()=>({y:0}),floor);
  if(i===299)five=Math.abs(s.heading-start)*180/Math.PI;
 }
 assert.ok(five>=30,`rudder ${rudder}: ${five} degrees in five seconds`);
 assert.ok(s.speed>1,`rudder ${rudder}: ${s.speed} m/s in ten seconds`);
 console.log(JSON.stringify({rudder,degreesAtFiveSeconds:five,speedAtTenSeconds:s.speed}));
}
const s=physics.create(vessel,ocean,walk);s.mode='sailing';
for(let i=0;i<720;i++) {
 physics.advance(s,1/60,i/60,{sail:i<90?1:0,rudder:i>=90&&i<240?1:0},()=>({y:0}),floor);
}
assert.ok(physics.speedCurve(s.heading-s.wind)>0,'recorded input sequence must clear irons at the real mooring');
assert.ok(s.speed>1,`recorded input sequence: ${s.speed} m/s`);
console.log(JSON.stringify({sequence:'W 1.5s, D 2.5s, coast 8s',speed:s.speed}));
