"use strict";
const test=require("node:test"), assert=require("node:assert/strict"), fs=require("node:fs"), path=require("node:path"), vm=require("node:vm");
const loader=fs.readFileSync(path.join(__dirname,"../runtime/host/server-slot-loader.ts"),"utf8");
const capability=fs.readFileSync(path.join(__dirname,"../runtime/host/server-slots.ts"),"utf8");
function ref(hash="0123456789abcdef"){return {version:1,hash,path:"/gosx/server-slots.js?v="+hash,integrity:"sha256-"+"A".repeat(43)+"="};}
function fixture(){
 const scripts=[],timers=new Map(),host={};let id=0,fail="";
 const document={currentScript:null,createElement(){if(fail==="create")throw Error("create");const attrs={};return {setAttribute(k,v){if(fail==="attr")throw Error("attr");attrs[k]=v;},getAttribute(k){return attrs[k]},remove(){if(fail==="remove")throw Error("remove");this.removed=true;}};},querySelector(){if(fail==="query")throw Error("query");return null;},head:{appendChild(s){if(fail==="append")throw Error("append");scripts.push(s);}}};
 const context={window:{__gosx:{host}},document,Promise,Map,Set,console,setTimeout(fn){timers.set(++id,fn);return id;},clearTimeout(i){timers.delete(i);}};
 vm.runInNewContext(loader,context);
 return {host,scripts,timers,context,setFailure(value){fail=value;},load(value=ref()){return context.ensureServerSlotHost(value)},execute(s){document.currentScript=s;vm.runInNewContext(capability,context);document.currentScript=null;}};
}
test("ServerSlot loader shares one verified activation and refuses changed identity",async()=>{
 const f=fixture();const a=f.load(),b=f.load();assert.equal(a,b);assert.equal(f.scripts.length,1);assert.equal(f.scripts[0].integrity,ref().integrity);f.execute(f.scripts[0]);f.scripts[0].onload();const api=await a;assert.equal(api.version,1);assert.equal(api.hash,ref().hash);await assert.rejects(f.load(ref("fedcba9876543210")),/identity changed/);assert.equal(f.host.serverSlots,api);
});
for(const where of ["create","attr","query","append"])test("ServerSlot setup failure clears pending: "+where,async()=>{
 const f=fixture();f.setFailure(where);await assert.rejects(f.load(),new RegExp(where));assert.equal(f.host.serverSlotLoading,undefined);f.setFailure("");const retry=f.load();f.execute(f.scripts[0]);f.scripts[0].onload();await retry;
});
test("ServerSlot timeout cleanup can throw without wedging retry; late old script cannot publish",async()=>{
 const f=fixture();const a=f.load();const old=f.scripts[0];const rejected=assert.rejects(a,/timeout/);f.setFailure("remove");[...f.timers.values()][0]();await rejected;assert.equal(f.host.serverSlotLoading,undefined);f.setFailure("");const b=f.load(ref("fedcba9876543210"));const current=f.scripts[1];f.execute(current);current.onload();const api=await b;f.execute(old);old.onload();assert.equal(f.host.serverSlots,api);assert.equal(api.hash,"fedcba9876543210");
});
test("ServerSlot descriptor and failed capability never publish readiness",async()=>{
 const f=fixture();await assert.rejects(f.load({...ref(),integrity:"wrong"}),/descriptor/);assert.equal(f.scripts.length,0);const p=f.load();f.scripts[0].onload();await assert.rejects(p,/capability mismatch/);assert.equal(f.host.serverSlotLoading,undefined);assert.equal(f.host.serverSlots,undefined);
});
