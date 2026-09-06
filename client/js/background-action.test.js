"use strict";
const test=require("node:test"),assert=require("node:assert/strict");
const {navigationSource,createContext,runScript}=require("./runtime-test-harness.js");
function setup(response={text:'{"ok":true}'}) {
 const env=createContext({elements:[],fetchRoutes:{"http://localhost:3000/edit/__actions/transform":response}});
 runScript(navigationSource,env.context,"navigation.ts");
 env.context.document.documentElement.setAttribute("data-gosx-csrf-token","secret");
 return {env,send:(url="/edit/__actions/transform",pairs=[["target","a&b"]],options={})=>env.context.__gosx.navigation.submitBackgroundAction(url,pairs,options)};
}
test("background action POST fixed pairs and CSRF without navigation or data application",async()=>{
 const {env,send}=setup({text:'{"ok":true,"data":{"injected":"value"}}'});
 let applied=0,forms=0;env.context.__gosx_set_input_batch=()=>applied++;
 const create=env.context.document.createElement.bind(env.context.document);
 env.context.document.createElement=name=>{if(String(name).toLowerCase()==="form")forms++;return create(name)};
 const before=env.context.document.body.childNodes.length,href=env.context.location.href;
 const result=await send();assert.equal(result.ok,true);assert.equal(result.status,200);
 assert.equal(env.fetchCalls.length,1);const request=env.fetchCalls[0].init;
 assert.equal(request.method,"POST");assert.equal(request.redirect,"error");assert.equal(request.credentials,"same-origin");
 assert.equal(request.headers["X-CSRF-Token"],"secret");const body=new URLSearchParams(request.body.toString());assert.equal(body.get("target"),"a&b");assert.equal(body.get("csrf_token"),"secret");
 assert.equal(env.context.location.href,href);assert.equal(env.context.document.body.childNodes.length,before);
 assert.equal(applied,0);assert.equal(forms,0);
});
test("background refuses a redirected response without navigating",async()=>{
 const {env,send}=setup(),href=env.context.location.href;
 env.context.fetch=async()=>({status:200,redirected:true,text:async()=>' {"ok":true}'});
 assert.equal((await send()).ok,false);assert.equal(env.context.location.href,href);
});
for(const url of ["", "https://elsewhere.test/__actions/x", "javascript:alert(1)", "/api/x", "/__actions/x?x=1", "/__actions/x#frag", "/__actions/x?", "/__actions/x#", "http://user:pass@localhost:3000/__actions/x", "/__actions/a%2fb", "/__actions/x/"]) test(`background refuses URL ${url}`,async()=>{const {env,send}=setup();assert.equal((await send(url)).ok,false);assert.equal(env.fetchCalls.length,0)});
for(const pairs of [[["csrf_token","evil"]],[["x",1]],[["x","a"],["x","b"]],[["x"]],null])test(`background refuses pairs ${JSON.stringify(pairs)}`,async()=>{const {env,send}=setup();assert.equal((await send(undefined,pairs)).ok,false);assert.equal(env.fetchCalls.length,0)});
for(const response of [{text:'{"ok":false}'},{text:'{"ok":"yes"}'},{text:'{}'},{text:'null'},{text:'broken'},{text:'{"ok":true,"redirect":"/elsewhere"}'},{status:403,text:'{"ok":true}'}]) test(`background response refusal ${JSON.stringify(response)}`,async()=>{const {env,send}=setup(response),href=env.context.location.href;assert.equal((await send()).ok,false);assert.equal(env.context.location.href,href);assert.equal(env.fetchCalls.length,1)});
test("background network failure never native-submits; abort forwarded",async()=>{
 const {env,send}=setup();let options;env.context.fetch=async(_,opts)=>{options=opts;throw Error("network")};
 const signal={aborted:true};const href=env.context.location.href;assert.equal((await send(undefined,undefined,{signal})).ok,false);assert.equal(options.signal,signal);assert.equal(env.context.location.href,href);
});
