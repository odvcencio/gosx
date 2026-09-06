"use strict";
const test=require("node:test"),assert=require("node:assert/strict");
const {FakeElement,createContext,runScript,bootstrapRuntimeSource,freshFeatureBundleSource,flushAsyncWork}=require("./runtime-test-harness.js");

async function mountedScale(enabled){
 const mount=new FakeElement("div",null);mount.id="scale-preview";
 const env=createContext({elements:[mount],enableWebGL:true,disableCanvas2D:true,fetchRoutes:{"/gosx/bootstrap-feature-engines.js":{text:freshFeatureBundleSource("engines")},"/gosx/bootstrap-feature-scene3d-webgl.js":{text:freshFeatureBundleSource("scene3d-webgl")}},manifest:{engines:[{id:"scale-engine",component:"GoSXScene3D",kind:"surface",mountId:mount.id,props:{width:640,height:360,autoRotate:false,camera:{x:0,y:0,z:6,fov:72},gizmoLocalScalePreview:enabled,selectionInputSignal:"$selection",gizmoInputSignal:"$mode",scene:{objects:[{id:"cube",kind:"box",size:1,scaleX:2,scaleY:3,scaleZ:4},...["hx","hy","hz"].map(id=>({id,kind:"lines",points:[{x:0,y:0,z:0},{x:1,y:0,z:0}],gizmoHelper:true,gizmoFormMode:"scale",gizmoControlID:"group",gizmoSize:1.25}))]}}}]}});
 const phases=[];mount.addEventListener("gosx:scene3d:input",e=>{if(e.detail.kind==="gizmo-commit")phases.push(e.detail.input)});
 const original=freshFeatureBundleSource("scene3d"),observed=original.replace("function applySceneObjectPatch(state, objectID, patch) {","function applySceneObjectPatch(state, objectID, patch) { observeOriginalScalePatch(state, objectID, patch); window.__scaleState=state; } function observeOriginalScalePatch(state, objectID, patch) {");assert.notEqual(observed,original);
 const source=observed.replace("function reinstallSceneCanvasInteractionHandles(reason) {","window.__scaleRebind=reinstallSceneCanvasInteractionHandles; function reinstallSceneCanvasInteractionHandles(reason) {");assert.notEqual(source,observed);
 runScript(bootstrapRuntimeSource,env.context,"runtime.js");runScript(source,env.context,"scene3d.js");await flushAsyncWork();
 env.context.__gosx_notify_shared_signal("$selection",JSON.stringify("cube"));env.context.__gosx_notify_shared_signal("$mode",JSON.stringify("scale"));await flushAsyncWork();
 const canvas=mount.children.find(c=>c.tagName==="CANVAS");assert.ok(canvas);canvas.getBoundingClientRect=()=>({left:0,top:0,width:640,height:360});
 const fire=(type,x)=>{const e={type,button:0,pointerId:7,clientX:x,clientY:180,preventDefault(){},stopPropagation(){},stopImmediatePropagation(){}};(type==="pointerdown"?canvas:env.context.document).dispatchEvent(e)};
 return {env,mount,canvas,phases,fire,cube:()=>env.context.__scaleState.objects.get("cube")};
}
for(const enabled of [false,true])test(`mounted local scale preview enabled=${enabled}`,async()=>{
 const h=await mountedScale(enabled);h.fire("pointerdown",340);h.fire("pointermove",360);
 assert.deepEqual(h.phases.map(p=>p.phase),["start","move"]);assert.equal(h.phases[1].axis,"x");
 const factor=h.phases[1].scaleFactor;assert.ok(factor>1.5);assert.ok(Math.abs(h.cube().scaleX-(enabled?2*factor:2))<1e-8);assert.equal(h.cube().scaleY,3);assert.equal(h.cube().scaleZ,4);
 h.fire("pointerup",360);assert.deepEqual(h.phases.map(p=>p.phase),["start","move","end"]);
});

for(const interruption of ["end","cancel","dispose","rebind","mode","selection","group","external","replacement","disable"])
test(`owned scale preview ${interruption}`,async()=>{
 const h=await mountedScale(true);h.fire("pointerdown",340);h.fire("pointermove",360);
 const factor=h.phases.at(-1).scaleFactor;assert.ok(Math.abs(h.cube().scaleX-2*factor)<1e-8);
 h.fire("pointermove",360);assert.ok(Math.abs(h.cube().scaleX-2*factor)<1e-8,"same factor must not compound");
 h.fire("pointermove",350);assert.ok(Math.abs(h.cube().scaleX-2*h.phases.at(-1).scaleFactor)<1e-8,"new factor relative to original");
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
 if(["end","external","replacement","group"].includes(interruption))h.fire("pointerup",350);
 else if(["cancel","disable"].includes(interruption))h.fire("pointercancel",999);
 await flushAsyncWork();
 const terminal=h.phases.filter(p=>p.phase==="end"||p.phase==="cancel");assert.equal(terminal.length,1);assert.equal(terminal[0].phase,interruption==="end"?"end":"cancel");
 const expected=interruption==="external"||interruption==="replacement"?[9,8,7]:interruption==="end"?[2*terminal[0].scaleFactor,3,4]:[2,3,4];
 assert.deepEqual([h.cube().scaleX,h.cube().scaleY,h.cube().scaleZ],expected);
 h.fire("pointerup",350);assert.equal(h.phases.filter(p=>p.phase==="end"||p.phase==="cancel").length,1);
});
