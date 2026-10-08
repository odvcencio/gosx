"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const fixture = JSON.parse(fs.readFileSync(path.join(__dirname,"../../scene/testdata/text3d.json"),"utf8"));

function runtime() {
  const context = vm.createContext({ window:{},console });
  for (const name of ["10-runtime-primitives.ts","10-runtime-scene-utils.ts","11-scene-math.ts","12-scene-geometry.ts","13-scene-material.ts","10-runtime-scene-core.ts","15-scene-draw-plan.ts","15b-scene-planner.ts"]) {
    let source=fs.readFileSync(path.join(__dirname,"bootstrap-src",name),"utf8");
    if (name==="10-runtime-scene-core.ts") source=source.slice(0,source.indexOf("// Scene3D shared API"));
    vm.runInContext(source,context,{filename:name});
  }
  return context;
}

test("Go-authored Text3D wire creates a bounded world-space textured plane", () => {
  const api=runtime(), entry=api.createSceneState({scene:fixture},{}).html.get("score");
  entry.textureReady=true;
  const bundle=api.createSceneRenderBundle(640,360,"#08151f",{x:0,y:2,z:8,fov:60},[],[],[],[entry],[],{},0,[],[],[],[],[],921600);
  assert.equal(bundle.surfaces.length,1);
  const surface=bundle.surfaces[0], material=bundle.materials[surface.materialIndex];
  assert.equal(surface.sourceID,"score");assert.equal(surface.textureKey,"gosx-html://score");
  assert.equal(surface.vertexCount,6);assert.equal(surface.textureBytes,512*128*4);
  assert.equal(surface.textureMaxBytes,256*1024*4);
  assert.equal(material.unlit,true);assert.equal(material.renderPass,"alpha");
  const xs=Array.from(surface.positions).filter((_v,i)=>i%3===0);
  const ys=Array.from(surface.positions).filter((_v,i)=>i%3===1);
  assert.ok(Math.abs(Math.max(...xs)-Math.min(...xs)-2)<1e-6);
  assert.ok(Math.abs(Math.max(...ys)-Math.min(...ys)-.5)<1e-6);
  assert.ok(entry.html.includes("Team &lt;A&gt;\n12 &amp; 8"));
});

test("text retains readable fallback before texture readiness and updates through existing commands", () => {
  const api=runtime(), state=api.createSceneState({scene:fixture},{});
  assert.equal(state.html.get("score").mode,"texture");
  assert.equal(state.html.get("score").textureReady,false);
  api.applySceneCommands(state,[{kind:1,objectId:"score"},{kind:0,objectId:"score",data:{kind:"html",props:{...fixture.html[0],html:"<div>13</div>"}}}]);
  assert.equal(state.html.size,1);assert.equal(state.html.get("score").html,"<div>13</div>");
  const entry=state.html.get("score");
  const bundle=api.createSceneRenderBundle(640,360,"#08151f",{x:0,y:2,z:8,fov:60},[],[],[],[entry],[],{},0,[],[],[],[],[],921600);
  assert.equal(bundle.surfaces.length,0);assert.equal(bundle.html.length,1);
  assert.equal(bundle.html[0].html,"<div>13</div>");
});
