"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs"), path = require("node:path"), vm = require("node:vm");
const fixture = JSON.parse(fs.readFileSync(path.join(__dirname,"../../scene/testdata/overlay.json"),"utf8"));

function runtime() {
  const context = vm.createContext({window:{},console});
  for (const name of ["10-runtime-primitives.ts","10-runtime-scene-utils.ts","11-scene-math.ts","12-scene-geometry.ts","13-scene-material.ts","10-runtime-scene-core.ts","14-scene-lighting.ts","15-scene-draw-plan.ts","15b-scene-planner.ts"]) {
    let source=fs.readFileSync(path.join(__dirname,"bootstrap-src",name),"utf8");
    if(name==="10-runtime-scene-core.ts") source=source.slice(0,source.indexOf("// Scene3D shared API"));
    vm.runInContext(source,context,{filename:name});
  }
  return context;
}
function bundle(api,state,camera={x:0,y:0,z:8,fov:60},time=0) {
  return api.createSceneRenderBundle(640,360,"#08151f",camera,Array.from(state.objects.values()),[],[],Array.from(state.html.values()),[],{},time,[],[],[],[],[],921600);
}

test("Go-authored overlay follows current target translation, rotation, scale and spin", () => {
  const api=runtime(), state=api.createSceneState({scene:fixture},{});
  const object=api.normalizeSceneObject({id:"seat",shape:"box",x:1,y:0,z:0,rotationZ:Math.PI/2,scaleY:2},0);
  state.objects.set("seat",object);
  const entry=state.html.get("bubble"), original=JSON.stringify(entry);
  for(let i=0;i<12;i++) {
    object.x=i*.1; object.rotationZ=i*.07; object.spinZ=.2;
    const time=i*.1, world={}; api.translateScenePointInto(world,0,1,0,object,time);
    const expected=api.sceneProjectPoint(world,{x:0,y:0,z:8,fov:60},640,360);
    const html=bundle(api,state,undefined,time).html[0];
    assert.ok(Math.abs(html.position.x-expected.x)<1e-8);
    assert.ok(Math.abs(html.position.y-expected.y)<1e-8);
    assert.equal(html.offsetY,-12); assert.ok(html.html.includes("aria-live"));
  }
  assert.equal(JSON.stringify(entry),original,"projection must not accumulate transforms into authored data");
});

test("missing targets hide content, fixed HTML retains placement and camera changes reproject", () => {
  const api=runtime(), state=api.createSceneState({scene:fixture},{});
  assert.equal(bundle(api,state).html.length,0);
  state.objects.set("seat",api.normalizeSceneObject({id:"seat",shape:"box",x:1},0));
  const before=bundle(api,state).html[0];
  const moved=bundle(api,state,{x:1,y:0,z:8,fov:60}).html[0];
  assert.notEqual(before.position.x,moved.position.x);
  state.objects.delete("seat"); assert.equal(bundle(api,state).html.length,0);
  api.applySceneCommands(state,[{kind:0,objectId:"fixed",data:{kind:"html",props:{...fixture.html[0],id:"fixed",targetOffset:null}}}]);
  assert.equal(bundle(api,state).html.length,1,"legacy target metadata remains static");
  api.applySceneCommands(state,[{kind:1,objectId:"fixed"}]); assert.equal(bundle(api,state).html.length,0);
});
