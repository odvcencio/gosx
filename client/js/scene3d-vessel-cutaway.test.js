'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const context={window:{}};
for(const name of ['vessel-physics','vessel-decks','vessel-cutaway'])vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../runtime/scene3d',name+'.ts'),'utf8'),context);
const physics=context.window.__gosx_scene3d_vessel_physics,decksApi=context.window.__gosx_scene3d_vessel_decks,api=context.window.__gosx_scene3d_vessel_cutaway_api;
const fixture=JSON.parse(fs.readFileSync(path.join(__dirname,'testdata/vessel-decks-liner.json'),'utf8'));
function mountStub(backend='webgl') {const attrs={'data-gosx-scene3d-backend':backend};return {id:'m',attrs,getAttribute:k=>attrs[k]??null,setAttribute:(k,v)=>{attrs[k]=String(v);}};}
function rig(pose,config,backend) {
 const decks=decksApi.create(fixture.decks,physics),mount=mountStub(backend),scheduled=[];
 const cw=api.create(decks,mount,reason=>scheduled.push(reason),config);decks.update(pose);cw.update(decks);
 return {decks,mount,cw,scheduled,pose};
}
// A point is cut when dot(n, p) + w > 0 for a plane; fade is cut[8].
const cutBy=(cut,plane,p)=>cut[4*plane]*p.x+cut[4*plane+1]*p.y+cut[4*plane+2]*p.z+cut[4*plane+3]>0;
const level={x:0,y:0,z:0,heading:0,pitch:0,roll:0},tilted={x:120,y:1.4,z:-80,heading:1.3,pitch:.06,roll:.22};
test('settings are validated and clamped',()=>{
 assert.equal(api.normalize(null,14),null);assert.equal(api.normalize('x',14),null);
 const n=api.normalize({enabled:true,deck:99,above:'fade',fadeAlpha:5,section:{axis:'q',position:1,side:1}},14);
 assert.equal(n.deck,13);assert.equal(n.fade,1);assert.equal(n.section.on,false,'a bad axis is ignored');
 assert.equal(api.normalize({enabled:true,deck:-3},14).deck,0);assert.equal(api.normalize({enabled:true,above:'fade'},14).fade,.15);
 assert.equal(api.normalize({enabled:true,section:{axis:'z',position:4,side:-1}},14).section.on,true);
 assert.equal(api.normalize({enabled:true,section:{axis:'z',position:4,side:0}},14).section.on,false);
 assert.equal(api.normalize({enabled:true,deck:2,height:NaN},14).height,NaN);
});
test('a level ship is cut at the ceiling of the chosen deck, and nothing is cut when disabled',()=>{
 const r=rig(level,{enabled:true,deck:5});
 const h=fixture.decks[6].y-.35;
 assert.ok(!cutBy(r.cw.cut,0,{x:0,y:h-.01,z:0}));assert.ok(cutBy(r.cw.cut,0,{x:0,y:h+.01,z:0}));
 assert.ok(!cutBy(r.cw.cut,0,{x:30,y:fixture.decks[5].y+1.8,z:-70}),'a head on the chosen deck stays');
 assert.ok(cutBy(r.cw.cut,0,{x:30,y:fixture.decks[6].y+.1,z:-70}),'the next deck goes');
 assert.equal(cutBy(r.cw.cut,1,{x:100,y:0,z:0}),false,'no section unless asked');
 assert.equal(r.cw.cut[8],0,'hide mode');assert.equal(r.mount.attrs['data-gosx-scene3d-cutaway'],'on');
 r.cw.set({enabled:false});r.cw.update(r.decks);
 assert.ok(r.cw.cut.every(v=>v===0));assert.equal(r.mount.attrs['data-gosx-scene3d-cutaway'],'off');
 assert.deepEqual(r.scheduled,['vessel-cutaway','vessel-cutaway'],'each change schedules a render');
});
test('on the top deck the cut still takes the funnel and masts',()=>{
 const r=rig(level,{enabled:true,deck:13}),top=fixture.decks[13].y;
 assert.ok(!cutBy(r.cw.cut,0,{x:0,y:top+2,z:0}));assert.ok(cutBy(r.cw.cut,0,{x:0,y:top+3.2,z:0}));
 const explicit=rig(level,{enabled:true,deck:0,height:20});assert.ok(cutBy(explicit.cw.cut,0,{x:0,y:20.1,z:0}));assert.ok(!cutBy(explicit.cw.cut,0,{x:0,y:19.9,z:0}));
});
test('the cut follows the ship while it rolls and pitches',()=>{
 const r=rig(tilted,{enabled:true,deck:8}),h=fixture.decks[9].y-.35;
 let checked=0;
 for(const [x,z] of [[-15,60],[0,0],[15,-90],[10,100],[-8,-40]]) {
  const below=physics.localPoint(tilted,x,h-.05,z),above=physics.localPoint(tilted,x,h+.05,z);
  assert.ok(!cutBy(r.cw.cut,0,below)&&cutBy(r.cw.cut,0,above),`local (${x},${z}) straddles the tilted cut`);checked++;
 }
 // A world-horizontal cut at the same height would misjudge points at the rails of a rolled ship.
 const rail=physics.localPoint(tilted,-15,h-.05,0);assert.ok(Math.abs(rail.y-physics.localPoint(tilted,15,h-.05,0).y)>4);
 assert.equal(checked,5);
});
test('the hull section removes one side of a ship-local plane',()=>{
 for(const [axis,side,pick] of [['x',1,(v)=>({x:v,y:12,z:5})],['x',-1,(v)=>({x:v,y:12,z:5})],['z',1,(v)=>({x:3,y:12,z:v})],['z',-1,(v)=>({x:3,y:12,z:v})]]) {
  const r=rig(tilted,{enabled:true,deck:13,section:{axis,position:2,side}});
  const far=physics.localPoint(tilted,...Object.values(pick(side*10))),near=physics.localPoint(tilted,...Object.values(pick(-side*10)));
  assert.ok(cutBy(r.cw.cut,1,far),`${axis} side ${side}: the removed side is cut`);assert.ok(!cutBy(r.cw.cut,1,near),`${axis} side ${side}: the kept side stays`);
  const edge=physics.localPoint(tilted,...Object.values(pick(2+.05*side)));assert.ok(cutBy(r.cw.cut,1,edge));
  const inner=physics.localPoint(tilted,...Object.values(pick(2-.05*side)));assert.ok(!cutBy(r.cw.cut,1,inner));
 }
});
test('fade keeps a share of pixels and parts stop casting shadows only while the cut is on',()=>{
 const r=rig(level,{enabled:true,deck:4,above:'fade',fadeAlpha:.25}),part={id:'ship/hull',castShadow:true},other={id:'ship/mast',castShadow:false};
 assert.equal(r.cw.cut[8],.25);
 r.cw.touch(part);r.cw.touch(other);
 assert.equal(part.cutaway,r.cw.cut);assert.equal(part.cutawayCrowd.length,8);assert.equal(part.castShadow,false);
 // The crowd view is the same memory: a later update shows in it with no copy.
 r.decks.update({...level,y:1});r.cw.update(r.decks);assert.equal(part.cutawayCrowd[3],r.cw.cut[3]);
 r.cw.set({enabled:false});r.cw.update(r.decks);r.cw.touch(part);r.cw.touch(other);
 assert.equal(part.castShadow,true,'the original is restored');assert.equal(other.castShadow,false);assert.equal(part.cutaway,null);assert.equal(part.cutawayCrowd,null);
});
test('a backend that cannot clip leaves the ship whole and says so',()=>{
 const r=rig(level,{enabled:true,deck:4},'webgpu'),part={id:'ship/hull',castShadow:true};
 r.cw.touch(part);assert.equal(part.cutaway,undefined);assert.equal(part.castShadow,true);
 assert.ok(r.cw.cut.every(v=>v===0));assert.equal(r.mount.attrs['data-gosx-scene3d-cutaway'],'unsupported');
 assert.deepEqual([...api.BACKENDS],['webgl']);
});
test('dispatch finds the vessel by mount and node and parses Go JSON',()=>{
 const r=rig(level,undefined);api.register('m','liner',r.cw);
 assert.equal(context.window.__gosx_scene3d_vessel_cutaway('m','liner',JSON.stringify({enabled:true,deck:3,above:'fade',section:{axis:'x',position:0,side:1}})),true);
 r.cw.update(r.decks);assert.equal(r.cw.state.deck,3);assert.equal(r.cw.state.section.on,true);
 assert.equal(context.window.__gosx_scene3d_vessel_cutaway('m','other','{}'),false);
 assert.equal(context.window.__gosx_scene3d_vessel_cutaway('x','liner','{}'),false);
 assert.equal(context.window.__gosx_scene3d_vessel_cutaway('m','liner','null'),false);
 api.unregister('m','liner',r.cw);assert.equal(context.window.__gosx_scene3d_vessel_cutaway('m','liner','{}'),false);
});
