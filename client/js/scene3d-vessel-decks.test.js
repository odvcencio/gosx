'use strict';
const test=require('node:test'),assert=require('node:assert/strict'),fs=require('node:fs'),path=require('node:path'),vm=require('node:vm');
const context={window:{}};
for(const name of ['vessel-physics','vessel-decks','walk-surfaces'])vm.runInNewContext(fs.readFileSync(path.join(__dirname,'../runtime/scene3d',name+'.ts'),'utf8'),context);
const physics=context.window.__gosx_scene3d_vessel_physics,api=context.window.__gosx_scene3d_vessel_decks,surfaces=context.window.__gosx_scene3d_walk_surfaces;
const fixture=JSON.parse(fs.readFileSync(path.join(__dirname,'testdata/vessel-decks-liner.json'),'utf8'));
const poseOf=p=>({x:p[0],y:p[1],z:p[2],heading:p[3],pitch:p[4],roll:p[5]});
test('JavaScript reads the same deck as Go for every fixture point, tilted or level',()=>{
 const decks=api.create(fixture.decks,physics);let checked=0,onDeck=0;
 for(const probe of fixture.probes) {
  decks.update(poseOf(probe.pose));
  const w={x:probe.world[0],y:probe.world[1],z:probe.world[2]},local=decks.toLocal(w);
  for(const [i,key] of ['x','y','z'].entries())assert.ok(Math.abs(local[key]-probe.local[i])<1e-6,`local ${key}: ${local[key]} vs ${probe.local[i]}`);
  assert.equal(decks.deckAt(local),probe.deck,`deck at ${probe.local}`);
  // Feet exactly on a surface: the world column under them meets the same point, so JavaScript must find Go's floor.
  // (Above the deck, a tilted ship moves the column sideways, and near a rail that can leave the deck.)
  if(probe.floorOk&&Math.abs(probe.local[1]-probe.floor)<1e-9) {
   const height=decks.height(w.x,w.z,w.y),back=decks.toLocal({x:w.x,y:height,z:w.z});
   assert.ok(Math.abs(back.y-probe.floor)<1e-6,`floor ${back.y} vs ${probe.floor} at ${probe.local}`);onDeck++;
  }
  checked++;
 }
 assert.ok(checked>400&&onDeck>150,`${checked} probes, ${onDeck} on a floor`);
});
test('a rolled ship keeps a walker on the deck they stand on',()=>{
 const decks=api.create(fixture.decks,physics);
 const pose={x:0,y:0,z:0,heading:.7,pitch:.05,roll:.25};decks.update(pose);
 for(const level of [0,4,9,13]) {
  const y=fixture.decks[level].y,w=physics.localPoint(pose,-12,y,50);
  assert.ok(Math.abs(decks.height(w.x,w.z,w.y)-w.y)<1e-9,`deck ${level} height under the walker`);
  assert.ok(Math.abs(decks.height(w.x,w.z,w.y+.4)-w.y)<1e-9,'a small hop above the deck keeps the same deck');
 }
 // The roll puts the port side of deck 9 well above the starboard side, so a tilt-blind reader that uses the ship's mean height gets a different deck.
 const port=physics.localPoint(pose,-15,fixture.decks[9].y,50),starboard=physics.localPoint(pose,15,fixture.decks[9].y,50);
 assert.ok(Math.abs(port.y-starboard.y)>5);
 assert.equal(decks.deckAt(decks.toLocal(port)),9);assert.equal(decks.deckAt(decks.toLocal(starboard)),9);
});
test('holes and decks above the feet are skipped, and stairs climb between decks',()=>{
 const decks=api.create(fixture.decks,physics);decks.update({x:0,y:0,z:0,heading:0,pitch:0,roll:0});
 const mid=fixture.decks.findIndex(d=>(d.holes||[]).length>=3),atriumDeck=fixture.decks[mid];
 // Over the atrium, feet at this deck's level fall to the deck below.
 assert.equal(decks.height(0,0,atriumDeck.y),fixture.decks[mid-1].y);
 // A deck more than a step above the feet is not seen; one within a step is.
 assert.equal(decks.height(-10,60,fixture.decks[3].y-.6),fixture.decks[2].y);
 assert.equal(decks.height(-10,60,fixture.decks[3].y-.4),fixture.decks[3].y);
 // With no feet height the highest deck wins (used by bounds checks).
 assert.equal(decks.height(-10,60),fixture.decks.at(-1).y);
 // Up a flight: the height rises smoothly from deck 0 to deck 1.
 const flight=fixture.decks[0].ramps[0];let last=fixture.decks[0].y,feet=last;
 for(let t=0;t<=1;t+=.05) {
  const z=flight.z+(flight.fromY<=flight.toY?-1:1)*flight.sizeZ/2*(1-2*t),h=decks.height(flight.x,z,feet);
  assert.ok(h>=last-1e-9,`monotone ${h} after ${last}`);assert.ok(h-last<.35,'no step bigger than the walker can climb');last=h;feet=h;
 }
 assert.ok(Math.abs(last-fixture.decks[1].y)<.4,`flight tops out at deck 1, got ${last}`);
});
test('a deck surface plugs into the walker height query with a feet argument',()=>{
 const decks=api.create(fixture.decks,physics);decks.update({x:0,y:0,z:0,heading:0,pitch:0,roll:0});
 const list=[decks.surface,{x:0,y:2,z:0,sizeX:4,sizeZ:4}];
 assert.equal(surfaces.height(list,-10,60,-Infinity,fixture.decks[5].y),fixture.decks[5].y);
 assert.equal(surfaces.height(list,0,0,-Infinity,100)>=2,true,'rectangles still work beside deck surfaces');
 assert.equal(surfaces.height([{x:0,y:2,z:0,sizeX:4,sizeZ:4}],1,1,-Infinity),2,'plain rectangles ignore feet');
});
