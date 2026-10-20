import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source=readFileSync(new URL('../runtime/host/modules.ts',import.meta.url),'utf8');
function importer(load){
 const context=vm.createContext({URL,Promise,Map,Error,document:{baseURI:'https://app.test/.proxy/'},window:{location:{href:'https://app.test/.proxy/room/demo'}},gosxRuntime:{}});
 vm.runInContext(source,context);
 return context.gosxModuleImporter(load);
}
test('native module imports resolve public URLs and share concurrent loads',async()=>{
 const calls=[],exports={SDK:class{}};
 const load=importer(async url=>{calls.push(url);return exports;});
 const a=load('./assets/sdk.js'),b=load('https://app.test/.proxy/assets/sdk.js');
 assert.equal(a,b);assert.equal(await a,exports);
 assert.deepEqual(calls,['https://app.test/.proxy/assets/sdk.js']);
 assert.equal(await load('./assets/sdk.js'),exports);
 assert.equal(calls.length,1);
});
test('failed imports may retry without evicting another URL',async()=>{
 let attempts=0;
 const load=importer(async url=>{if(url.endsWith('/retry.js')&&++attempts===1)throw new Error('offline');return {url};});
 const stable=load('/stable.js');
 await assert.rejects(load('/retry.js'),/offline/);
 assert.equal((await load('/retry.js')).url,'https://app.test/retry.js');
 assert.equal(load('/stable.js'),stable);assert.equal(attempts,2);
});
test('module imports reject source strings, credentials and empty URLs',async()=>{
 let calls=0;const load=importer(async()=>{calls++;});
 for(const url of ['', '  ', 'data:text/javascript,export default 1', 'javascript:alert(1)', 'blob:https://app.test/id','https://user:pass@app.test/sdk.js'])await assert.rejects(load(url));
 assert.equal(calls,0);
});
