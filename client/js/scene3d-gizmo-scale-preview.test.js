"use strict";
const test=require("node:test"),assert=require("node:assert/strict");
const {FakeElement,createContext,runScript,bootstrapRuntimeSource,freshFeatureBundleSource,flushAsyncWork}=require("./runtime-test-harness.js");

async function mountedScale(enabled){
 const mount=new FakeElement("div",null);mount.id="scale-preview";
 const env=createContext({elements:[mount],enableWebGL:true,disableCanvas2D:true,fetchRoutes:{"/gosx/bootstrap-feature-engines.js":{text:freshFeatureBundleSource("engines")},"/gosx/bootstrap-feature-scene3d-webgl.js":{text:freshFeatureBundleSource("scene3d-webgl")}},manifest:{engines:[{id:"scale-engine",component:"GoSXScene3D",kind:"surface",mountId:mount.id,props:{width:640,height:360,autoRotate:false,camera:{x:0,y:0,z:6,fov:72},gizmoLocalScalePreview:enabled,selectionInputSignal:"$selection",gizmoInputSignal:"$mode",scene:{objects:[{id:"cube",kind:"box",size:1,scaleX:2,scaleY:3,scaleZ:4},...["hx","hy","hz"].map(id=>({id,kind:"lines",points:[{x:0,y:0,z:0},{x:1,y:0,z:0}],gizmoHelper:true,gizmoFormMode:"scale",gizmoControlID:"group",gizmoSize:1.25}))]}}}]}});
 const phases=[];mount.addEventListener("gosx:scene3d:input",e=>{if(e.detail.kind==="gizmo-commit")phases.push(e.detail.input)});
 const original=freshFeatureBundleSource("scene3d"),observed=original.replace("function applySceneObjectPatch(state, objectID, patch) {","function applySceneObjectPatch(state, objectID, patch) { if(state.objects.get(objectID)?.gizmoHelper)window.__helperPatches=(window.__helperPatches||0)+1; observeOriginalScalePatch(state, objectID, patch); window.__scaleState=state; } function observeOriginalScalePatch(state, objectID, patch) {");assert.notEqual(observed,original);
 const source=observed.replace("function reinstallSceneCanvasInteractionHandles(reason) {","window.__scaleRebind=reinstallSceneCanvasInteractionHandles; function reinstallSceneCanvasInteractionHandles(reason) {");assert.notEqual(source,observed);
 runScript(bootstrapRuntimeSource,env.context,"runtime.js");runScript(source,env.context,"scene3d.js");await flushAsyncWork();
 env.context.__gosx_notify_shared_signal("$selection",JSON.stringify("cube"));env.context.__gosx_notify_shared_signal("$mode",JSON.stringify("scale"));await flushAsyncWork();
 const canvas=mount.children.find(c=>c.tagName==="CANVAS");assert.ok(canvas);canvas.getBoundingClientRect=()=>({left:0,top:0,width:640,height:360});
 const fire=(type,x)=>{const e={type,button:0,pointerId:7,clientX:x,clientY:180,preventDefault(){},stopPropagation(){},stopImmediatePropagation(){}};(type==="pointerdown"?canvas:env.context.document).dispatchEvent(e)};
 return {env,mount,canvas,phases,fire,cube:()=>env.context.__scaleState.objects.get("cube")};
}
test("100 identical mounted scale samples preserve helper identity",async()=>{
 const h=await mountedScale(true);h.fire("pointerdown",372);h.fire("pointermove",424);
 const state=h.env.context.__scaleState,ids=["hx","hy","hz"],before=ids.map(id=>state.objects.get(id));
 h.env.context.__helperPatches=0;
 for(let i=0;i<100;i++)h.fire("pointermove",424);
 const replaced=ids.filter((id,i)=>state.objects.get(id)!==before[i]).length;
 console.log(`unchanged100 helperPatches=${h.env.context.__helperPatches} replaced=${replaced}`);
 assert.equal(h.env.context.__helperPatches,0);assert.equal(replaced,0);
 h.fire("pointercancel",999);
});
test("helper sync updates two anchors rotations mode and visibility",async()=>{
 const h=await mountedScale(true),state=h.env.context.__scaleState;
 h.mount.dispatchEvent(new h.env.context.CustomEvent("gosx:scene3d:commands",{detail:{revision:1,commands:[{kind:0,objectId:"second",data:{kind:"box",props:{id:"second",x:2,y:3,z:-1,rotationX:.2,rotationY:.3,rotationZ:.4}}}]}}));
 const select=async value=>{h.env.context.__gosx_notify_shared_signal("$selection",JSON.stringify(value));await flushAsyncWork()};
 await select("second");
 const check=(values,visible)=>{for(const id of ["hx","hy","hz"]){const o=state.objects.get(id);assert.deepEqual([o.x,o.y,o.z,o.rotationX,o.rotationY,o.rotationZ],values);assert.equal(o.visible,visible)}};
 check([2,3,-1,.2,.3,.4],true);
 h.env.context.__gosx_notify_shared_signal("$mode",JSON.stringify("translate"));await flushAsyncWork();check([2,3,-1,.2,.3,.4],false);
 h.env.context.__gosx_notify_shared_signal("$mode",JSON.stringify("scale"));await flushAsyncWork();check([2,3,-1,.2,.3,.4],true);
 await select("cube");check([0,0,0,0,0,0],true);
 await select("");check([0,0,0,0,0,0],false);
});
for(const enabled of [false,true])test(`mounted local scale preview enabled=${enabled}`,async()=>{
 const h=await mountedScale(enabled);h.fire("pointerdown",372);h.fire("pointermove",424);
 assert.deepEqual(h.phases.map(p=>p.phase),["start","move"]);assert.equal(h.phases[1].axis,"x");
 const factor=h.phases[1].scaleFactor;assert.ok(factor>1.5);assert.ok(Math.abs(h.cube().scaleX-(enabled?2*factor:2))<1e-8);assert.equal(h.cube().scaleY,3);assert.equal(h.cube().scaleZ,4);
 h.fire("pointerup",424);assert.deepEqual(h.phases.map(p=>p.phase),["start","move","end"]);
});

test("mounted scale action uses end only and failed response preserves preview",async()=>{
 const h=await mountedScale(true),requests=[];
 h.mount.__gosxScene3DHandle.updateSceneProps({gizmoCommitAction:"/edit/__actions/transform"});
 h.env.context.__gosx.navigation={submitBackgroundAction:async(url,pairs)=>{requests.push({url,pairs});return {ok:false,status:422}}};
 h.fire("pointerdown",372);h.fire("pointermove",424);await flushAsyncWork();assert.equal(requests.length,0);
 h.fire("pointerup",424);await flushAsyncWork();assert.equal(requests.length,1);
 const fields=Object.fromEntries(requests[0].pairs);assert.equal(fields.mode,"scale");assert.equal(fields.phase,"end");
 assert.equal(h.cube().scaleX,2*Number(fields.scale_factor));assert.deepEqual(h.phases.map(p=>p.phase),["start","move","end"]);
 h.fire("pointerdown",372);h.fire("pointercancel",999);await flushAsyncWork();assert.equal(requests.length,1);
});

for(const interruption of ["end","cancel","dispose","rebind","mode","selection","group","external","replacement","disable"])
test(`owned scale preview ${interruption}`,async()=>{
 const h=await mountedScale(true);h.fire("pointerdown",372);h.fire("pointermove",424);
 const factor=h.phases.at(-1).scaleFactor;assert.ok(Math.abs(h.cube().scaleX-2*factor)<1e-8);
 h.fire("pointermove",424);assert.ok(Math.abs(h.cube().scaleX-2*factor)<1e-8,"same factor must not compound");
 h.fire("pointermove",398);assert.ok(Math.abs(h.cube().scaleX-2*h.phases.at(-1).scaleFactor)<1e-8,"new factor relative to original");
 assert.equal(h.cube().scaleY,3);assert.equal(h.cube().scaleZ,4);
 const command=commands=>h.mount.dispatchEvent(new h.env.context.CustomEvent("gosx:scene3d:commands",{detail:{revision:1,commands}}));
 if(interruption==="external")command([{kind:2,objectId:"cube",data:{scaleX:9,scaleY:8,scaleZ:7}}]);
 else if(interruption==="replacement")command([{kind:1,objectId:"cube"},{kind:0,objectId:"cube",data:{kind:"box",props:{id:"cube",scaleX:9,scaleY:8,scaleZ:7}}}]);
 else if(interruption==="group")command([{kind:2,objectId:"hx",data:{gizmoSize:2}}]);
 else if(interruption==="mode")h.env.context.__gosx_notify_shared_signal("$mode",JSON.stringify("translate"));
 else if(interruption==="selection")h.env.context.__gosx_notify_shared_signal("$selection",JSON.stringify(""));
 else if(interruption==="dispose")h.env.context.__gosx_dispose_engine("scale-engine");
 else if(interruption==="rebind")h.env.context.__scaleRebind("test-rebind");
 else if(interruption==="disable")h.mount.__gosxScene3DHandle.updateSceneProps({gizmoLocalScalePreview:false});
 if(["end","external","replacement","group"].includes(interruption))h.fire("pointerup",398);
 else if(["cancel","disable"].includes(interruption))h.fire("pointercancel",999);
 await flushAsyncWork();
 const terminal=h.phases.filter(p=>p.phase==="end"||p.phase==="cancel");assert.equal(terminal.length,1);assert.equal(terminal[0].phase,interruption==="end"?"end":"cancel");
 const expected=interruption==="external"||interruption==="replacement"?[9,8,7]:interruption==="end"?[2*terminal[0].scaleFactor,3,4]:[2,3,4];
 assert.deepEqual([h.cube().scaleX,h.cube().scaleY,h.cube().scaleZ],expected);
 h.fire("pointerup",398);assert.equal(h.phases.filter(p=>p.phase==="end"||p.phase==="cancel").length,1);
});
