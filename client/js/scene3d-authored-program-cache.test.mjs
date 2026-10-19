import assert from "node:assert/strict";
import { createRequire } from "node:module";
import test from "node:test";
import vm from "node:vm";

const require = createRequire(import.meta.url);
const ts = createRequire(new URL("../runtime/package.json", import.meta.url))("typescript");
const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");
const source = readSceneRendererBackendSrc("webgl");
function extract(name) {
  const marker = "function " + name + "(";
  const start = source.indexOf(marker);
  assert.ok(start >= 0, name);
  const indent = source.slice(source.lastIndexOf("\n", start) + 1, start);
  const end = source.indexOf("\n" + indent + "}\n", start);
  return source.slice(start, end + indent.length + 3);
}

function harness() {
  let compilations=0;
  let sourceEncodings=0;
  const context={gl:{},selectiveBloom:false,
    scenePBRAuthoredProgramKeys:new WeakMap(),
    JSON:{stringify(value){
      if(Array.isArray(value)&&value.some(x=>typeof x==='string'&&x.startsWith('vertex')))sourceEncodings++;
      return JSON.stringify(value);
    }},
    scenePBRHasCustomHooks:()=>true,sceneSelenaIsMaterial:()=>true,
    sceneSelenaMaterialLayout:m=>m.shaderLayout,
    scenePBRCustomUniformDeclarations:values=>Object.keys(values||{}).sort().map(k=>k+":"+ (Array.isArray(values[k])?values[k].length:"float")).join("|"),
    sceneMaterialProfileKey:m=>JSON.stringify(m),
    customProgramCache:new Map(),selenaProgramCache:new Map(),scenePBRProgramFailed:()=>false,
    createScenePBRCustomProgram:()=>({program:++compilations}),
    createSceneSelenaProgram:()=>({program:++compilations}),
  };
  vm.runInNewContext(ts.transpileModule(extract("scenePBRAuthoredProgramKey")+extract("scenePBREnsureCustomProgram")+extract("scenePBREnsureSelenaProgram")+extract("ensureCustomProgram")+extract("ensureSelenaProgram"), { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context);
  return {context,count:()=>compilations,encodings:()=>sourceEncodings};
}
test("animated Selena uniform values share one GPU program and keep skin variants separate",()=>{
  const h=harness(),m={key:"first",customVertex:"vertex",customFragment:"fragment",shaderLayout:{uniformBlock:{size:32}}};
  let first;
  for(let i=0;i<2000;i++) {
    const p=h.context.ensureSelenaProgram({...m,key:"frame-"+i,customUniforms:{phase:i/2000,tint:[i/2000,1,0]}},false);
    first ||= p;assert.equal(p,first);
  }
  assert.equal(h.count(),1);
  assert.equal(h.encodings(),1,"fresh per-frame material profiles must not repeatedly encode shader source");
  assert.notEqual(h.context.ensureSelenaProgram(m,true),first);
  assert.equal(h.count(),2);
  assert.notEqual(h.context.ensureSelenaProgram({...m,customFragment:"edited"},false),first);
  assert.notEqual(h.context.ensureSelenaProgram({...m,shaderLayout:{uniformBlock:{size:48}}},false),first);
  assert.equal(h.count(),4);
});
test("Selena color and selective fragments retain separate cached programs",()=>{
  const h=harness(),m={customVertex:"vertex",customFragment:"color",specularFragmentGLSL:"color-plus-specular",shaderLayout:{uniformBlock:{size:32}}};
  const color=h.context.ensureSelenaProgram(m,false);
  h.context.selectiveBloom=true;
  const selective=h.context.ensureSelenaProgram(m,false);
  assert.notEqual(selective,color);
  assert.equal(h.context.ensureSelenaProgram(m,false),selective);
  h.context.selectiveBloom=false;
  assert.equal(h.context.ensureSelenaProgram(m,false),color);
  assert.equal(h.count(),2);
});
test("custom PBR program identity depends on uniform declarations, not uniform values",()=>{
  const h=harness(),m={key:"first",customVertex:"vertex",customFragment:"fragment"};
  let first;
  for(let i=0;i<2000;i++) {
    const p=h.context.ensureCustomProgram({...m,key:"frame-"+i,customUniforms:{phase:i/2000,tint:[i/2000,1,0]}});
    first ||= p;assert.equal(p,first);
  }
  assert.equal(h.count(),1);
  assert.equal(h.encodings(),1,"animated values must not repeatedly encode custom PBR source");
  assert.notEqual(h.context.ensureCustomProgram({...m,customUniforms:{phase:1,tint:[1,0,0,1]}}),first);
  assert.equal(h.count(),2);
});

test("Selena nested layout mutations invalidate keys without changing their wire identity",()=>{
  const h=harness(),m={customVertex:'vertex\n"quoted"',customFragment:'fragment\\source',shaderLayout:{uniformBlock:{fields:[{name:'tint',type:'vec3'}]}}};
  const first=h.context.ensureSelenaProgram(m,false);
  assert.deepEqual([...h.context.selenaProgramCache.keys()],[JSON.stringify([m.customVertex,m.customFragment,m.shaderLayout])]);
  m.shaderLayout.uniformBlock.fields[0].type='vec4';
  const edited=h.context.ensureSelenaProgram(m,false);
  assert.notEqual(edited,first);
  assert.equal(h.count(),2);
  m.shaderLayout.uniformBlock.fields[0].type='vec3';
  assert.equal(h.context.ensureSelenaProgram(m,false),first);
  assert.equal(h.encodings(),2,"returning to an existing layout must reuse its encoded shader key");
});

test("authored program key ownership follows cache clearing and WebGL contexts",()=>{
  const h=harness(),m={customVertex:'vertex',customFragment:'fragment',shaderLayout:{uniformBlock:{size:32}}};
  const first=h.context.ensureSelenaProgram(m,false);
  h.context.selenaProgramCache.clear();
  assert.notEqual(h.context.ensureSelenaProgram(m,false),first);
  assert.equal(h.encodings(),2,"clearing GPU programs also retires the key index on reuse");
  h.context.selenaProgramCache=new Map();
  assert.notEqual(h.context.ensureSelenaProgram(m,false),first);
  assert.equal(h.encodings(),3,"a fresh WebGL cache must own a fresh key index");
});
