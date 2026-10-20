'use strict';
// Fixed-step proofs use actual Go-authored terrain and hull data, never a
// teleport to certify a voyage. Browser QA separately checks the real controls.
const fs=require('node:fs'),vm=require('node:vm'),assert=require('node:assert/strict');
const root=process.argv[2],context={window:{},atob};
for(const name of ['vessel-navigation','vessel-physics','walk-surfaces'])vm.runInNewContext(fs.readFileSync(root+'/client/runtime/scene3d/'+name+'.ts','utf8'),context);
vm.runInNewContext(fs.readFileSync(root+'/client/js/bootstrap-feature-scene3d-walk.js','utf8'),context);
const {vessel,walk,ocean,finds,harborExit,places}=JSON.parse(fs.readFileSync(0,'utf8'));
const physics=context.window.__gosx_scene3d_vessel_physics,nav=context.window.__gosx_scene3d_vessel_navigation,walker=context.window.__gosx_scene3d_walk_api;
const ground=walker.decodeGround(walk.ground),grounds=walk.grounds.map(walker.decodeGround),floor=(x,z)=>walker.sampleGrounds(ground,grounds,x,z,-1e4);
for(let k=0;k<vessel.landings.length;k++) {
 const site=vessel.landings[k],find=finds[k].position,s=physics.create(vessel,ocean,walk);s.mode='sailing';s.trim=1;
 const bearingTo=p=>((Math.atan2(p.x-s.x,-(p.z-s.z))*180/Math.PI)+360)%360;
 const passage=[harborExit,...(places.find(p=>p.view===site.id).route||[])].map(p=>({x:p[0],z:p[1]}));
 assert.ok(nav.holdCourse(s,bearingTo(passage[0])));let time=0,furled=false;
 while(time<240 && nav.landing(s)?.id!==site.id) {
  if(passage.length&&Math.hypot(s.x-passage[0].x,s.z-passage[0].z)<20){passage.shift();nav.holdCourse(s,bearingTo(passage[0]||site.position));}
  if(Math.round(time*60)%60===0&&s.trim>0)nav.holdCourse(s,bearingTo(passage[0]||site.position));
  if(Math.hypot(s.x-site.position.x,s.z-site.position.z)<30){s.trim=0;furled=true;}
  physics.advance(s,1/60,time,{},()=>({y:0}),floor);time+=1/60;
 }
 assert.equal(nav.landing(s)?.id,site.id,'actual sail must reach '+site.id+': '+JSON.stringify({x:s.x,z:s.z,speed:s.speed,distance:s.distance}));
 assert.ok(s.speed<=site.maxSpeed&&s.distance>100&&!s.grounded,'arrival must slow in deep water after a real voyage '+JSON.stringify({id:site.id,furled,grounded:s.grounded,trim:s.trim,x:s.x,z:s.z,speed:s.speed,time}));
 const state=walker.createState({...site.position,rotationX:0,rotationY:0},walk,true);
 for(let i=0;i<1500 && Math.hypot(state.x-find.x,state.z-find.z)>.3;i++) {
  const d=Math.hypot(find.x-state.x,find.z-state.z),step=Math.min(.06,d);
  walker.move(state,(find.x-state.x)/d*step,(find.z-state.z)/d*step);
 }
 assert.ok(Math.hypot(state.x-find.x,state.z-find.z)<.35,'pier-to-find route must be walkable: '+site.id+JSON.stringify({x:state.x,z:state.z,find}));
 assert.ok(!walker.candidateAllowed(state,site.position.x+70,site.position.z),'walking cannot cross deep sea between islands');
 const shipPosition={x:s.x,z:s.z};s.mode='moored';s.trim=0;s.vx=s.vz=s.speed=0;
 assert.equal(physics.helm(s,{...find,y:find.y+1.7},true),'take');
 assert.equal(s.x,shipPosition.x);assert.equal(s.z,shipPosition.z);
 // Depart away from the island; nearby terrain remains a real hull constraint.
 nav.holdCourse(s,135);s.trim=1;for(let i=0;i<600;i++)physics.advance(s,1/60,time+i/60,{},()=>({y:0}),floor);
 assert.ok(s.speed>.5&&!s.grounded,'ship must be able to leave '+site.id);
 console.log(JSON.stringify({island:site.id,voyageSeconds:+time.toFixed(1),distance:+s.distance.toFixed(1),findReached:true,shipPreserved:true}));
}
