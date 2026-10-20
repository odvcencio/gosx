const vm=require('node:vm'),assert=require('node:assert/strict'),fs=require('node:fs');
const input=JSON.parse(fs.readFileSync(0,'utf8'));
function deferred(){let resolve,reject;const promise=new Promise((a,b)=>{resolve=a;reject=b});return {promise,resolve,reject};}
async function flush(){for(let i=0;i<12;i++)await Promise.resolve();}
function fixture(href='https://example.test/',brand=true,frame=false){
 const events=new Map(),timers=new Map(),nodes=new Map();let next=0;
 const calls={status:0,retry:0,launch:0},pending={status:[],retry:[],launch:[]},messages=[];
 const service={};for(const key of Object.keys(calls))service[key]=()=>{calls[key]++;const d=deferred();pending[key].push(d);return d.promise;};
 const element={dataset:{},innerHTML:''};
 function node(id){if(!nodes.has(id))nodes.set(id,{textContent:'',disabled:false,addEventListener(k,f){this[k]=f},removeEventListener(k,f){if(this[k]===f)delete this[k]}});return nodes.get(id);}
 const url=new URL(href);const window={gosxDesktop:{__gosxDesktopBridge:brand,service(){return service;}},chrome:{webview:{postMessage(v){messages.push(JSON.parse(v))}}},addEventListener(k,f){events.set(k,f)},removeEventListener(k){events.delete(k)}};window.top=frame?{}:window;
 const document={readyState:'loading',documentElement:element,getElementById:node,addEventListener(k,f){events.set(k,f)},removeEventListener(k){events.delete(k)}};
 const context={window,document,location:{origin:url.origin,pathname:url.pathname,href},Promise,setTimeout(f){timers.set(++next,f);return next},setInterval(f){timers.set(++next,f);return next},clearTimeout(k){timers.delete(k)},clearInterval(k){timers.delete(k)}};
 vm.runInNewContext(input.script,context);
 return {window,context,events,timers,nodes,element,calls,pending,messages,start(){events.get('DOMContentLoaded')?.()},tick(){for(const fn of [...timers.values()])fn()},hide(){events.get('pagehide')?.()}};
}
(async()=>{
 const f=fixture();assert.equal(f.window.__gosx_telemetry_config.enabled,false);assert.equal(f.window.testMode,'renderer');assert.equal(f.calls.status,0);f.start();assert.equal(f.element.dataset.desktop,'true');assert.equal(f.messages[0].type,'document-ready');assert.equal(f.messages[1].kind,'render-process-exited');f.start();assert.equal(f.messages.length,2);
 for(let i=0;i<100;i++)f.tick();assert.equal(f.calls.status,1,'only one held status query');
 f.pending.status[0].resolve({state:'failed',error:'<img onerror=bad>',details:['<script>bad</script>']});await flush();
 assert.equal(f.nodes.get('message').textContent,'<img onerror=bad>');assert.equal(f.nodes.get('details').textContent,'<script>bad</script>');assert(f.element.innerHTML.includes('<button'));
 f.nodes.get('retry').click();f.nodes.get('retry').click();f.tick();assert.equal(f.calls.retry,1);assert.equal(f.calls.status,1);
 f.pending.retry[0].reject(new Error('retry denied'));await flush();assert.equal(f.nodes.get('status').textContent,'retry denied');assert.equal(f.nodes.get('retry').disabled,false);
 f.nodes.get('retry').click();f.pending.retry[1].resolve();await flush();assert.equal(f.calls.status,2);
 f.pending.status[1].resolve({navigate:true});await flush();assert.equal(f.calls.launch,1);f.tick();assert.equal(f.calls.status,2);f.pending.launch[0].reject(new Error('launch denied'));await flush();
 f.tick();assert.equal(f.calls.status,3);f.hide();assert.equal(f.timers.size,0);assert(!f.nodes.get('retry').click);f.pending.status[2].resolve({navigate:true});await flush();assert.equal(f.calls.launch,1,'no late launch after disposal');
 const foreign=fixture('https://foreign.test/');foreign.start();foreign.tick();assert.equal(foreign.calls.status,0);assert.equal(foreign.messages.length,0);assert.equal(foreign.window.testMode,undefined);
 const path=fixture('https://example.test/other');path.start();assert(!path.messages.some(m=>m.type==='document-ready'));
 const blank=fixture('about:blank');blank.start();assert.equal(blank.calls.status,1);assert.equal(blank.messages.length,0);
 const badBlank=fixture('about:blank#other');badBlank.start();assert.equal(badBlank.calls.status,0);
 const unbranded=fixture('https://example.test/',false);unbranded.start();assert.equal(unbranded.calls.status,0);
 const changed=fixture();changed.start();changed.context.location.origin='https://foreign.test';changed.pending.status[0].resolve({navigate:true});await flush();assert.equal(changed.calls.launch,0,'recheck origin on completion');
 const unload=fixture();unload.hide();unload.start();assert.equal(unload.calls.status,0);
 const frame=fixture('https://example.test/',true,true);frame.start();assert.equal(frame.calls.status,0);assert.equal(frame.messages.length,0);assert.equal(frame.window.testMode,undefined);
 const queued=fixture();queued.start();queued.pending.status[0].resolve({state:'failed'});await flush();queued.tick();queued.nodes.get('retry').click();queued.nodes.get('retry').click();assert.equal(queued.calls.retry,0,'serialize retry behind status');queued.pending.status[1].resolve({state:'failed'});await flush();assert.equal(queued.calls.retry,1);const previous=queued.nodes.get('status').textContent;queued.hide();queued.pending.retry[0].reject(new Error('late'));await flush();assert.equal(queued.nodes.get('status').textContent,previous,'late retry does not mutate disposed UI');
 const foreignAction=fixture();foreignAction.start();foreignAction.pending.status[0].resolve({state:'failed'});await flush();foreignAction.context.location.origin='https://foreign.test';foreignAction.nodes.get('retry').click();assert.equal(foreignAction.calls.retry,0);
 const optOut=fixture('about:blank');optOut.hide();const noBlank=JSON.parse(JSON.stringify(input));noBlank.script=noBlank.script.replace('"allowBlank":true','"allowBlank":false');const original=input.script;input.script=noBlank.script;const denied=fixture('about:blank');denied.start();assert.equal(denied.calls.status,0);input.script=original;input.script=input.script.replace('"report":true','"report":false');const noReady=fixture();noReady.start();assert.equal(noReady.calls.status,1);assert(!noReady.messages.some(m=>m.type==='document-ready'));input.script=original;
 console.log('host lifecycle behavior passed');
})().catch(e=>{console.error(e);process.exitCode=1});
