import assert from 'node:assert/strict';
import fs from 'node:fs';
import vm from 'node:vm';
import test from 'node:test';
const source=fs.readFileSync(new URL('../runtime/host/navigation.ts',import.meta.url),'utf8');
const start=source.indexOf('  function observeNativeViewTransition(');
const end=source.indexOf('  const HEAD_START',start);
test('native transition lifecycle consumes expected cancellation and reports other failures',async()=>{
 const handlers={},warnings=[];
 vm.runInNewContext(source.slice(start,end),{window:{addEventListener:(name,handler)=>handlers[name]=handler},console:{warn:(...args)=>warnings.push(args)}});
 assert.equal(handlers.pageswap,handlers.pagereveal);
 handlers.pageswap({viewTransition:{ready:Promise.reject(Object.assign(new Error('interrupted'),{name:'AbortError'})),finished:Promise.resolve()}});
 handlers.pagereveal({viewTransition:{ready:Promise.reject(Object.assign(new Error('skipped'),{name:'InvalidStateError'})),finished:Promise.resolve()}});
 handlers.pageswap({viewTransition:null});await new Promise(resolve=>setImmediate(resolve));assert.equal(warnings.length,0);
 handlers.pagereveal({viewTransition:{ready:Promise.reject(new Error('unexpected'))}});await new Promise(resolve=>setImmediate(resolve));assert.equal(warnings.length,1);
});
