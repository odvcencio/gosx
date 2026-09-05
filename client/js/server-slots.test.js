"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");
const { FakeElement } = require("./runtime-test-harness.js");
const source = fs.readFileSync(path.join(__dirname, "../runtime/host/server-slots.ts"), "utf8");

function fixture(options = {}) {
  const root = new FakeElement("div", null);
  root.id = "owner"; root.setAttribute("data-gosx-island", "Panels");
  const body = new FakeElement("div", null); root.appendChild(body);
  const panel = new FakeElement("div", null); panel.setAttribute("data-gosx-server-slot", "Profile"); body.appendChild(panel);
  panel.closest = () => root;
  const input = new FakeElement("input", null); input.value = "edited"; panel.appendChild(input);
  Object.defineProperty(panel, "hidden", { set(value) { if (value) this.setAttribute("hidden", ""); else this.removeAttribute("hidden"); }, get() { return this.hasAttribute("hidden"); } });
  const listener = () => {};
  root.addEventListener("click", listener, false);
  const previous = { root, listeners: [{ target:root, type:"click", listener, capture:false }] };
  let current = previous, aborts = 0, commits = 0;
  const api = {
    version: options.unsupported ? 0 : 1,
    prepare() { return JSON.stringify({ token:"1", patches:[], slots:options.slots || [{name:"Profile",path:"0",hidden:true}] }); },
    commit() { if (options.commitFailure) return "shared changed"; commits++; current = "new"; return ""; },
    abort() { aborts++; return ""; },
  };
  const context = { window:{__gosx:{islands:new Map([["owner", previous]]), runtime:{serverSlots:api}}}, console:{error(){}}, Map, Set,
    gosxHost:{patch:{apply(){}}, events:{setup(target,id,events,journal){ const next=()=>{}; target.addEventListener("click",next,false); journal.push({target,type:"click",listener:next,capture:false}); if(options.listenerFailure)throw new Error("listener setup failed"); return journal; }}} };
  context.window.__gosx.host=context.gosxHost;
  context.document={currentScript:{getAttribute(){return "0123456789abcdef";}}};
  context.gosxHost.serverSlotLoading={script:context.document.currentScript,key:"0123456789abcdef:fixture"};
  vm.runInNewContext(source, context);
  context.hydrateServerSlotIsland=context.gosxHost.serverSlots.hydrate;
  context.validateServerSlotPatches=context.gosxHost.serverSlots.validatePatches;
  const entry = { id:"owner", component:"Panels", serverSlots:["Profile"], events:[], props:{} };
  return {root,panel,input,previous,entry,context,run(){context.hydrateServerSlotIsland(entry,root,{data:"",format:"json"});},stats(){return {current,aborts,commits};}};
}

test("ServerSlot adoption preserves opaque input and transfers owner only after writes", () => {
  const f=fixture(); f.run(); assert.equal(f.stats().commits,1); assert.equal(f.input.value,"edited"); assert.equal(f.panel.childNodes[0],f.input); assert.equal(f.panel.hidden,true); assert.equal(f.root.listenerCount("click"),1);
});
test("ServerSlot partial prior listener removal restores old owner", () => {
  const f=fixture(); const second=()=>{};
  f.root.addEventListener("keydown",second,false);
  f.previous.listeners.push({target:f.root,type:"keydown",listener:second,capture:false});
  const remove=f.root.removeEventListener.bind(f.root);
  f.root.removeEventListener=(type,listener,capture)=>{if(type==="keydown")throw new Error("second removal fails");remove(type,listener,capture);};
  f.run(); assert.equal(f.stats().commits,0);assert.equal(f.stats().aborts,1);
  assert.equal(f.root.listenerCount("click"),1);assert.equal(f.root.listenerCount("keydown"),1);
  assert.equal(f.context.window.__gosx.islands.get("owner"),f.previous);
});
for(const failure of ["listenerFailure","commitFailure"]) test(`ServerSlot ${failure} restores DOM and old listeners/owner`, () => {
  const f=fixture({[failure]:true}); f.run(); assert.equal(f.stats().commits,0); assert.equal(f.stats().aborts,1); assert.equal(f.stats().current,f.previous); assert.equal(f.panel.hidden,false); assert.equal(f.root.listenerCount("click"),1); assert.equal(f.context.window.__gosx.islands.get("owner"),f.previous); assert.equal(f.panel.childNodes[0],f.input);
});
test("ServerSlot persistent setter failure still aborts staged owner and restores listeners", () => {
  const f=fixture(); f.panel.setAttribute=()=>{throw new Error("persistent setter failure");}; f.panel.removeAttribute=()=>{throw new Error("persistent remover failure");}; f.run(); assert.equal(f.stats().commits,0); assert.equal(f.stats().aborts,1); assert.equal(f.stats().current,f.previous); assert.equal(f.root.listenerCount("click"),1); assert.equal(f.context.window.__gosx.islands.get("owner"),f.previous);
});
test("ServerSlot unsupported capability leaves server fallback untouched", () => {
  const f=fixture({unsupported:true}); f.run(); assert.equal(f.stats().commits,0); assert.equal(f.stats().aborts,0); assert.equal(f.panel.hidden,false); assert.equal(f.root.listenerCount("click"),1);
});
test("ServerSlot patch barrier refuses descendant text and ancestor clear", () => {
  const f=fixture(); const inventory=new Map([["Profile",f.panel]]);
  for(const op of [{kind:0,path:"0/0",text:"clear"},{kind:0,path:"",text:"clear"},{kind:0,path:"0",text:"clear"},{kind:6,path:"0",tag:"div"}]) assert.throws(()=>f.context.validateServerSlotPatches(f.root,[op],inventory),/ServerSlot/);
  assert.doesNotThrow(()=>f.context.validateServerSlotPatches(f.root,[{kind:1,path:"0",attrName:"hidden",text:"true"}],inventory));
});

test("ServerSlot staged duplicate names fail exact inventory before commit", () => {
  const f=fixture({slots:[{name:"Profile",path:"0",hidden:false},{name:"Profile",path:"0",hidden:true}]});
  const second=new FakeElement("div",null); second.setAttribute("data-gosx-server-slot","Security"); second.closest=()=>f.root; f.root.firstElementChild.appendChild(second); f.entry.serverSlots.push("Security");
  f.run(); assert.equal(f.stats().commits,0); assert.equal(f.stats().aborts,1); assert.equal(f.stats().current,f.previous);
});

test("ServerSlot later patch rejects removed and externally replaced outlet", () => {
  const f=fixture(); const inventory=new Map([["Profile",f.panel]]);
  f.root.firstElementChild.removeChild(f.panel);
  assert.throws(()=>f.context.validateServerSlotPatches(f.root,[{kind:1,path:"",attrName:"class",text:"changed"}],inventory),/ServerSlot/);
  const replacement=new FakeElement("div",null); replacement.setAttribute("data-gosx-server-slot","Profile"); replacement.closest=()=>f.root; f.root.firstElementChild.appendChild(replacement);
  assert.throws(()=>f.context.validateServerSlotPatches(f.root,[{kind:1,path:"0",attrName:"hidden",text:"true"}],inventory),/ServerSlot/);
});
