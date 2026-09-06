"use strict";
const test=require("node:test"),assert=require("node:assert/strict"),fs=require("node:fs"),path=require("node:path"),vm=require("node:vm");
const source=fs.readFileSync(path.join(__dirname,"../runtime/scene3d/mount.ts"),"utf8").split("/**")[0];
function setup(){
 const requests=[],events=[];let action="/edit/__actions/transform",current=true,resolve;
 const context={URL,AbortController,Promise,Set,CustomEvent:class{constructor(type,options){this.type=type;this.detail=options.detail}},window:{location:{href:"http://localhost:3000/edit"},__gosx:{navigation:{submitBackgroundAction:(url,pairs,options)=>{requests.push({url,pairs,options});return new Promise(r=>resolve=r)}}}}};
 vm.createContext(context);vm.runInContext(source,context);
 const transport=context.sceneGizmoCommitTransport({dispatchEvent:event=>events.push(event)},()=>action,()=>current);
 return {transport,requests,events,context,action:value=>action=value,current:value=>current=value,resolve:value=>resolve(value)};
}
const tick=()=>new Promise(resolve=>setImmediate(resolve));
for(const action of ["/__actions/x?", "/__actions/x#"])test(`gizmo refuses raw URL delimiter ${action}`,async()=>{
 const h=setup();h.action(action);h.transport.handle(payload("scale","start"));h.transport.handle(payload("scale"));await tick();assert.equal(h.requests.length,0);
});
const payload=(mode,phase="end")=>({target:"cube",mode,axis:mode==="rotate"?"z":"x",phase,position:{x:1,y:2,z:3},scaleFactor:1.5,angleDelta:-.25});
for(const mode of ["translate","scale","rotate"])test(`gizmo ${mode} submits only matching end with snapshotted action`,async()=>{
 const h=setup();h.transport.handle(payload(mode,"start"));h.action("/other/__actions/new");h.transport.handle(payload(mode,"move"));await tick();assert.equal(h.requests.length,0);
 h.transport.handle(payload(mode));h.transport.handle(payload(mode));await tick();assert.equal(h.requests.length,1);
 const r=h.requests[0],fields=Object.fromEntries(r.pairs);assert.equal(r.url,"http://localhost:3000/edit/__actions/transform");assert.equal(fields.phase,"end");assert.equal(fields.target,"cube");
 assert.equal(r.pairs.length,mode==="translate"?7:5);assert.equal(fields[mode==="translate"?"position_z":mode==="scale"?"scale_factor":"angle_delta"],mode==="translate"?"3":mode==="scale"?"1.5":"-0.25");
 h.resolve({ok:true,status:200,result:{data:{position:999}}});await tick();assert.equal(h.events.length,1);assert.equal(h.events[0].detail.result,undefined);
});
for(const kind of ["cancel","no-start","invalid-action","no-api","mismatch","nan","zero-scale","rotate-x","dispose-before","stale-before"])test(`gizmo transport refuses ${kind}`,async()=>{
 const h=setup(),p=payload("scale");if(kind==="invalid-action")h.action("https://other.test/__actions/x");if(kind!=="no-start")h.transport.handle({...p,phase:"start"});
 if(kind==="cancel")h.transport.handle({...p,phase:"cancel"});if(kind==="no-api")delete h.context.window.__gosx.navigation;
 if(kind==="mismatch")p.target="other";if(kind==="nan")p.scaleFactor=NaN;if(kind==="zero-scale")p.scaleFactor=0;
 if(kind==="rotate-x"){p.mode="rotate";p.axis="x";h.transport.handle({...p,phase:"start"})}
 if(kind==="dispose-before")h.transport.dispose();if(kind==="stale-before")h.current(false);
 h.transport.handle(p);await tick();assert.equal(h.requests.length,0);
});
test("gizmo dispose aborts pending request and suppresses late observation",async()=>{
 const h=setup();h.transport.handle(payload("translate","start"));h.transport.handle(payload("translate"));await tick();assert.equal(h.requests.length,1);
 h.transport.dispose();assert.equal(h.requests[0].options.signal.aborted,true);h.resolve({ok:true,status:200});await tick();assert.equal(h.events.length,0);
});
for(const p of [ {...payload("translate"),position:{x:1,y:Infinity,z:3}}, {...payload("translate"),position:{x:1,y:2}}, {...payload("rotate"),angleDelta:NaN}, {...payload("scale"),scaleFactor:"2"} ])test(`gizmo rejects malformed ${p.mode} values`,async()=>{
 const h=setup();h.transport.handle({...p,phase:"start"});h.transport.handle(p);await tick();assert.equal(h.requests.length,0);
});
test("gizmo stale lifetime suppresses late result without applying response",async()=>{
 const h=setup();h.transport.handle(payload("scale","start"));h.transport.handle(payload("scale"));await tick();h.current(false);h.resolve({ok:false,status:422});await tick();assert.equal(h.events.length,0);
});
