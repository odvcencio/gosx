import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';
const source=fs.readFileSync(new URL('../runtime/host/navigation.ts',import.meta.url),'utf8');
const start=source.indexOf('  function observeNativeViewTransition(');
const end=source.indexOf('  const HEAD_START',start);
function harness(){
 const handlers={},warnings=[];
 vm.runInNewContext(source.slice(start,end),{window:{addEventListener:(name,handler)=>handlers[name]=handler},console:{warn:(...args)=>warnings.push(args)}});
 return {handlers,warnings};
}
test('outbound native fallback owns all promises before synchronously skipping animation',async()=>{
 const {handlers,warnings}=harness(),order=[];
 const transition={skipTransition(){order.push('skip');}};
 for(const key of ['ready','finished','updateCallbackDone'])transition[key]={catch(){order.push(key);}};
 handlers.pageswap({type:'pageswap',viewTransition:transition});
 assert.deepEqual(order,['ready','finished','updateCallbackDone','skip']);
 // Incoming transitions remain browser-owned; only the outbound fallback is skipped.
 handlers.pagereveal({type:'pagereveal',viewTransition:transition});
 assert.deepEqual(order.slice(4),['ready','finished','updateCallbackDone']);
 assert.equal(warnings.length,0);
});
test('native transition lifecycle consumes expected cancellation and reports other failures',async()=>{
 const {handlers,warnings}=harness();
 assert.equal(handlers.pageswap,handlers.pagereveal);
 handlers.pageswap({type:'pageswap',viewTransition:{
  ready:Promise.reject(Object.assign(new Error('interrupted'),{name:'AbortError'})),
  finished:Promise.reject(Object.assign(new Error('skipped'),{name:'InvalidStateError'})),
  updateCallbackDone:Promise.reject(Object.assign(new Error('interrupted'),{name:'AbortError'})),
  skipTransition(){},
 }});
 handlers.pagereveal({type:'pagereveal',viewTransition:null});
 await new Promise(resolve=>setImmediate(resolve));assert.equal(warnings.length,0);
 const error=new Error('unexpected update failure');
 handlers.pagereveal({type:'pagereveal',viewTransition:{updateCallbackDone:Promise.reject(error)}});
 await new Promise(resolve=>setImmediate(resolve));assert.equal(warnings.length,1);
 assert.equal(warnings[0][1],error);
});
