import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

const source = fs.readFileSync(new URL("../runtime/scene3d/webgl.ts", import.meta.url), "utf8");
function extract(name) {
  const start = source.indexOf("    function " + name + "(");
  assert.ok(start >= 0, name);
  const end = source.indexOf("\n    }\n", start);
  return source.slice(start, end + 7);
}
function harness() {
  let compilations=0;
  const context={gl:{},
    scenePBRHasCustomHooks:()=>true,sceneSelenaIsMaterial:()=>true,
    sceneSelenaMaterialLayout:m=>m.shaderLayout,
    scenePBRCustomUniformDeclarations:values=>Object.keys(values||{}).sort().map(k=>k+":"+ (Array.isArray(values[k])?values[k].length:"float")).join("|"),
    sceneMaterialProfileKey:m=>JSON.stringify(m),
    customProgramCache:new Map(),selenaProgramCache:new Map(),
    createScenePBRCustomProgram:()=>({program:++compilations}),
    createSceneSelenaProgram:()=>({program:++compilations}),
  };
  vm.runInNewContext(extract("ensureCustomProgram")+extract("ensureSelenaProgram"), context);
  return {context,count:()=>compilations};
}
test("animated Selena uniform values share one GPU program and keep skin variants separate",()=>{
  const h=harness(),m={key:"first",customVertex:"vertex",customFragment:"fragment",shaderLayout:{uniformBlock:{size:32}}};
  let first;
  for(let i=0;i<2000;i++) {
    const p=h.context.ensureSelenaProgram({...m,key:"frame-"+i,customUniforms:{phase:i/2000,tint:[i/2000,1,0]}},false);
    first ||= p;assert.equal(p,first);
  }
  assert.equal(h.count(),1);
  assert.notEqual(h.context.ensureSelenaProgram(m,true),first);
  assert.equal(h.count(),2);
  assert.notEqual(h.context.ensureSelenaProgram({...m,customFragment:"edited"},false),first);
  assert.notEqual(h.context.ensureSelenaProgram({...m,shaderLayout:{uniformBlock:{size:48}}},false),first);
  assert.equal(h.count(),4);
});
test("custom PBR program identity depends on uniform declarations, not uniform values",()=>{
  const h=harness(),m={key:"first",customVertex:"vertex",customFragment:"fragment"};
  let first;
  for(let i=0;i<2000;i++) {
    const p=h.context.ensureCustomProgram({...m,key:"frame-"+i,customUniforms:{phase:i/2000,tint:[i/2000,1,0]}});
    first ||= p;assert.equal(p,first);
  }
  assert.equal(h.count(),1);
  assert.notEqual(h.context.ensureCustomProgram({...m,customUniforms:{phase:1,tint:[1,0,0,1]}}),first);
  assert.equal(h.count(),2);
});
