import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import test from 'node:test';
const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const source = fs.readFileSync(new URL('../runtime/scene3d/geometry-assets.ts', import.meta.url),'utf8');
const code = ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText;
const vertices = {count:3,positions:[0,0,0,1,0,0,0,1,0],indices:[0,1,2],attributes:{heat:{itemSize:1,data:[1,2,3]}}};
function runtime(fetcher) {
 const window={location:{href:'https://test.local/table/1',origin:'https://test.local'}};
 vm.runInNewContext(code,{window,fetch:fetcher,URL,TextDecoder,console});
 return window.__gosx_scene3d_assets;
}
test('geometry references deduplicate and preserve typed command metadata without mutation',async()=>{
 let calls=0;
 const api=runtime(async()=>{calls++;return new Response(JSON.stringify(vertices));});
 const obj={id:'tile',verticesURL:'/assets/hash.json',shaderLayout:{future:'kept'}};
 const commands=[{kind:0,data:{kind:'mesh',props:obj}}];
 const [first,second]=await Promise.all([api.resolveCommands(commands),api.resolveObjects([obj])]);
 assert.equal(calls,1);assert.equal(first[0].data.props.vertices.count,3);
 assert.equal(first[0].data.props.shaderLayout,obj.shaderLayout);assert.equal(obj.vertices,undefined);
 assert.equal(second[0].vertices,first[0].data.props.vertices);
 await api.resolveObjects([obj]);assert.equal(calls,1);
});
test('rejected geometry leaves the healthy command data untouched and can retry',async()=>{
 let calls=0;
 const api=runtime(async()=>new Response(JSON.stringify(++calls===1?{...vertices,indices:[0,1,8]}:vertices)));
 const obj={verticesURL:'/assets/hash.json'};
 await assert.rejects(api.resolveObjects([obj]),/indices/);assert.equal(obj.vertices,undefined);
 assert.equal((await api.resolveObjects([obj]))[0].vertices.count,3);
});
test('geometry rejects cross origin, script URLs, credentials and oversized responses',async()=>{
 let calls=0;
 const api=runtime(async()=>{calls++;return new Response('{}',{headers:{'content-length':String(17<<20)}});});
 for(const url of ['https://elsewhere.test/mesh','javascript:alert(1)','https://user:pass@test.local/mesh']) await assert.rejects(api.resolveObjects([{verticesURL:url}]),/same-origin/);
 assert.equal(calls,0);
 await assert.rejects(api.resolveObjects([{verticesURL:'/too-big'}]),/budget/);
});
