import { createRequire } from 'node:module';
import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');
const source = fs.readFileSync(new URL('../runtime/scene3d/command-runtime.ts',import.meta.url),'utf8');
const compile = source => ts.transpileModule(source,{compilerOptions:{target:ts.ScriptTarget.ES2022}}).outputText;
function harness() {
 const calls=[], attrs={};
 const handle={__gosxScene3DCommandReady:true,applyCommands(commands){calls.push(commands);},getCamera(){return {kind:'perspective',x:2,rotationX:-.8,fov:40};},setCamera(camera){this.camera=camera;},setAnimationClock(clock){this.clock=clock;}};
 const mount={id:'stage',__gosxScene3DHandle:handle,setAttribute(k,v){attrs[k]=v;},getAttribute(k){return attrs[k];},querySelector(){return null;}};
 const window={__gosx:{engines:new Map()}}, document={getElementById(id){return id==='stage'?mount:null;}};
 const context=vm.createContext({window,document,Promise,Map,WeakMap,Set,ArrayBuffer,Uint8Array,DataView,TextDecoder,DOMException,Date,setTimeout,clearTimeout});
 vm.runInContext(compile(source),context);
 return {api:window.__gosx_scene3d_command_bridge,window,mount,handle,calls,attrs};
}
test('public camera preserves explicit resets and exposes effective pose',async()=>{
 const h=harness();
 assert.equal(h.api.getCamera(h.mount).rotationX,-.8);
 await h.api.setCamera(h.mount,{kind:'perspective',x:0,rotationX:0,fov:40});
 assert.equal(h.handle.camera.rotationX,0);assert.equal(h.handle.camera.x,0);
 await h.api.setAnimationClock(h.mount,{timeSeconds:0,paused:true});
 assert.equal(h.handle.clock.paused,true);
 assert.equal(await h.api.whenReady(h.mount),true);
});
test('readiness cancellation removes its wait and prevents a late mutation',async()=>{
 const h=harness(), abort=new AbortController();
 h.handle.__gosxScene3DCommandReady=false;
 const pending=h.api.setCamera(h.mount,{x:8},{signal:abort.signal});
 abort.abort();
 await assert.rejects(pending,{name:'AbortError'});
 h.handle.__gosxScene3DCommandReady=true;
 await new Promise(resolve=>setTimeout(resolve,25));
 assert.equal(h.handle.camera,undefined);
});
test('geometry resolution preserves transaction order and failed fetch leaves scene healthy',async()=>{
 const h=harness();let release;
 h.window.__gosx_scene3d_assets={async resolveCommands(commands){if(commands[0].id===1)await new Promise(resolve=>release=resolve);if(commands[0].id===3)throw new Error('fetch failed');return commands;}};
 const command=id=>({id,kind:0,data:{props:{verticesURL:"/geometry"}}});
 const first=h.api.dispatchCommands(h.mount,[command(1)]),second=h.api.dispatchCommands(h.mount,[command(2)]);
 await Promise.resolve();await Promise.resolve();
 assert.equal(h.calls.length,0);release();
 await Promise.all([first,second]);assert.deepEqual(h.calls.map(c=>c[0].id),[1,2]);
 await assert.rejects(h.api.dispatchCommands(h.mount,[command(3)]),/fetch failed/);
 await h.api.dispatchCommands(h.mount,[command(4)]);assert.deepEqual(h.calls.map(c=>c[0].id),[1,2,4]);
});
test('cancellation during geometry fetch cannot mutate a replaced scene',async()=>{
 const h=harness(),abort=new AbortController();let release;
 h.window.__gosx_scene3d_assets={resolveCommands(){return new Promise(resolve=>release=()=>resolve([{id:1}]));}};
 const pending=h.api.dispatchCommands(h.mount,[{id:1,kind:0,data:{props:{verticesURL:"/geometry"}}}],{signal:abort.signal});
 await Promise.resolve();await Promise.resolve();abort.abort();await assert.rejects(pending,{name:'AbortError'});
 release();await new Promise(resolve=>setTimeout(resolve,0));assert.equal(h.calls.length,0);
});
test('terminal unsupported readiness rejects without waiting for timeout',async()=>{
 const h=harness();h.attrs['data-gosx-scene3d-renderer']='unsupported';
 await assert.rejects(h.api.whenReady(h.mount),/unavailable/);
});

test('authored fallback keeps node identity through hidden updates, failure and dispose',()=>{
 function element(name){return {name,nodeType:1,childNodes:[],hidden:false,parentNode:null,textContent:'',setAttribute(){},get firstChild(){return this.childNodes[0]||null;},appendChild(node){if(node.parentNode)node.parentNode.removeChild(node);this.childNodes.push(node);node.parentNode=this;},removeChild(node){this.childNodes.splice(this.childNodes.indexOf(node),1);node.parentNode=null;},insertBefore(node,next){if(node.parentNode)node.parentNode.removeChild(node);this.childNodes.splice(this.childNodes.indexOf(next),0,node);node.parentNode=this;}};}
 const mount=element('mount'),svg=element('svg');mount.appendChild(svg);
 const mountSource=fs.readFileSync(new URL('../runtime/scene3d/mount-lifecycle.ts',import.meta.url),'utf8');
 const helper=mountSource.slice(mountSource.indexOf('function sceneRetainFallback('));
 const ctx=vm.createContext({document:{createElement:element}});vm.runInContext(compile(helper),ctx);
 const fallback=ctx.sceneRetainFallback(mount);mount.appendChild(fallback.element);
 assert.equal(fallback.element.hidden,true);assert.equal(svg.parentNode,fallback.element);
 svg.textContent='updated authoritative hand';fallback.show();
 assert.equal(fallback.element.hidden,false);assert.equal(svg.textContent,'updated authoritative hand');
 fallback.hide();
 const replacement=ctx.sceneRetainFallback(mount,{});
 assert.strictEqual(replacement.element,fallback.element);
 fallback.restore();
 assert.strictEqual(svg.parentNode,replacement.element,"stale owner restored the winner's fallback");
 replacement.restore();
 assert.equal(svg.parentNode,mount);assert.equal(mount.childNodes.length,1);assert.equal(mount.childNodes[0],svg);
 const empty=element('empty'),first=ctx.sceneRetainFallback(empty,{}),oldCanvas=element('canvas');
 empty.appendChild(oldCanvas);
 const next=ctx.sceneRetainFallback(empty,{});
 assert.equal(first.element,null);assert.equal(next.element,null,"generated canvas became authored fallback");
 first.restore();assert.ok(empty.__gosxScene3DFallback,"stale owner cleared the current holder");
 next.restore();assert.equal(empty.__gosxScene3DFallback,undefined);
});


test('external handles reject unresolved geometry without the mounted scene resolver',async()=>{
 const h=harness();
 await assert.rejects(h.api.dispatchCommands(h.mount,[{kind:0,data:{props:{verticesURL:'/geometry/hash.json'}}}]),/resolver is unavailable/);
 assert.equal(h.calls.length,0);
 await h.api.dispatchCommands(h.mount,[{kind:0,data:{props:{vertices:{count:0,positions:[]}}}}]);
 assert.equal(h.calls.length,1);
});
