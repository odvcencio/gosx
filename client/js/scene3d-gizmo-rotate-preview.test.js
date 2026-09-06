"use strict";
const test=require("node:test"),assert=require("node:assert/strict");
const {FakeElement,createContext,runScript,bootstrapRuntimeSource,freshFeatureBundleSource,flushAsyncWork}=require("./runtime-test-harness.js");

async function mountedRotate(enabled, targetPatch={}, helperPatch={}){
 const mount=new FakeElement("div",null);mount.id="scale-preview";
 const env=createContext({elements:[mount],enableWebGL:true,disableCanvas2D:true,fetchRoutes:{"/gosx/bootstrap-feature-engines.js":{text:freshFeatureBundleSource("engines")},"/gosx/bootstrap-feature-scene3d-webgl.js":{text:freshFeatureBundleSource("scene3d-webgl")}},manifest:{engines:[{id:"scale-engine",component:"GoSXScene3D",kind:"surface",mountId:mount.id,props:{width:640,height:360,autoRotate:false,camera:{x:0,y:0,z:6,fov:72},gizmoLocalRotatePreview:enabled,selectionInputSignal:"$selection",gizmoInputSignal:"$mode",scene:{objects:[{id:"cube",kind:"box",size:1,scaleX:2,scaleY:3,scaleZ:4,rotationZ:.3,...targetPatch},...["ring"].map(id=>({id,kind:"lines",points:[{x:0,y:0,z:0},{x:1,y:0,z:0}],gizmoHelper:true,gizmoFormMode:"rotate",gizmoControlID:"group",gizmoSize:1.25,...helperPatch}))]}}}]}});
 const phases=[];mount.addEventListener("gosx:scene3d:input",e=>{if(e.detail.kind==="gizmo-commit")phases.push(e.detail.input)});
 const original=freshFeatureBundleSource("scene3d"),observed=original.replace("function applySceneObjectPatch(state, objectID, patch) {","function applySceneObjectPatch(state, objectID, patch) { observeOriginalScalePatch(state, objectID, patch); window.__scaleState=state; } function observeOriginalScalePatch(state, objectID, patch) {");assert.notEqual(observed,original);
 const source=observed.replace("function reinstallSceneCanvasInteractionHandles(reason) {","window.__scaleRebind=reinstallSceneCanvasInteractionHandles; function reinstallSceneCanvasInteractionHandles(reason) {");assert.notEqual(source,observed);
 runScript(bootstrapRuntimeSource,env.context,"runtime.js");runScript(source,env.context,"scene3d.js");await flushAsyncWork();
 env.context.__gosx_notify_shared_signal("$selection",JSON.stringify("cube"));env.context.__gosx_notify_shared_signal("$mode",JSON.stringify("rotate"));await flushAsyncWork();
 const canvas=mount.children.find(c=>c.tagName==="CANVAS");assert.ok(canvas);canvas.getBoundingClientRect=()=>({left:0,top:0,width:640,height:360});
 const fire=(type,x,y=180)=>{const e={type,button:0,pointerId:7,clientX:x,clientY:y,preventDefault(){},stopPropagation(){},stopImmediatePropagation(){}};(type==="pointerdown"?canvas:env.context.document).dispatchEvent(e)};
 return {env,mount,canvas,phases,fire,cube:()=>env.context.__scaleState.objects.get("cube")};
}
for(const enabled of [false,true])test(`mounted local rotate preview enabled=${enabled}`,async()=>{
 const h=await mountedRotate(enabled);h.fire("pointerdown",372);h.fire("pointermove",320,128);
 assert.deepEqual(h.phases.map(p=>p.phase),["start","move"]);const angle=h.phases.at(-1).angleDelta;assert.ok(angle>1);
 assert.ok(Math.abs(h.cube().rotationZ-(enabled?.3+angle:.3))<1e-9);
 h.fire("pointercancel",999);assert.equal(h.cube().rotationZ,.3);
});

for(const interruption of ["end","cancel","dispose","rebind","mode","selection","group","external","replacement","disable"])
test(`owned rotate preview ${interruption}`,async()=>{
 const h=await mountedRotate(true);h.fire("pointerdown",372);h.fire("pointermove",320,128);
 const first=h.cube().rotationZ;h.fire("pointermove",320,128);assert.equal(h.cube().rotationZ,first,"no compounding");
 h.fire("pointermove",320,232);assert.ok(h.phases.at(-1).angleDelta<0);assert.equal(h.cube().rotationZ,.3+h.phases.at(-1).angleDelta);
 assert.deepEqual([h.cube().rotationX,h.cube().rotationY,h.cube().scaleX,h.cube().scaleY,h.cube().scaleZ,h.cube().x,h.cube().y,h.cube().z],[0,0,2,3,4,0,0,0]);
 const command=commands=>h.mount.dispatchEvent(new h.env.context.CustomEvent("gosx:scene3d:commands",{detail:{revision:1,commands}}));
 if(interruption==="external")command([{kind:2,objectId:"cube",data:{rotationX:.1,rotationY:.2,rotationZ:2}}]);
 else if(interruption==="replacement")command([{kind:1,objectId:"cube"},{kind:0,objectId:"cube",data:{kind:"box",props:{id:"cube",rotationX:.1,rotationY:.2,rotationZ:2}}}]);
 else if(interruption==="group")command([{kind:2,objectId:"ring",data:{gizmoSize:2}}]);
 else if(interruption==="mode")h.env.context.__gosx_notify_shared_signal("$mode",JSON.stringify("translate"));
 else if(interruption==="selection")h.env.context.__gosx_notify_shared_signal("$selection",JSON.stringify(""));
 else if(interruption==="dispose")h.env.context.__gosx_dispose_engine("scale-engine");
 else if(interruption==="rebind")h.env.context.__scaleRebind("test-rebind");
 else if(interruption==="disable")h.mount.__gosxScene3DHandle.updateSceneProps({gizmoLocalRotatePreview:false});
 if(["end","external","replacement","group"].includes(interruption))h.fire("pointerup",320,232);
 else if(["cancel","disable"].includes(interruption))h.fire("pointercancel",999);
 await flushAsyncWork();const terminal=h.phases.filter(p=>p.phase==="end"||p.phase==="cancel");assert.equal(terminal.length,1);assert.equal(terminal[0].phase,interruption==="end"?"end":"cancel");
 const want=["external","replacement"].includes(interruption)?[.1,.2,2]:[0,0,interruption==="end"?.3+terminal[0].angleDelta:.3];
 assert.deepEqual([h.cube().rotationX,h.cube().rotationY,h.cube().rotationZ],want);h.fire("pointerup",320,232);assert.equal(h.phases.filter(p=>p.phase==="end"||p.phase==="cancel").length,1);
});

const matrix=[1,0,0,0,0,1,0,0,0,0,1,0,0,0,0,1];
test("rotate angle branch remains original-relative and geometrically equivalent",async()=>{
 const h=await mountedRotate(true);h.fire("pointerdown",268,181);h.fire("pointermove",268,179);
 const delta=h.phases.at(-1).angleDelta;assert.ok(delta>6);assert.equal(h.cube().rotationZ,.3+delta);
 const equivalent=.3+delta-2*Math.PI;assert.ok(Math.abs(Math.cos(h.cube().rotationZ)-Math.cos(equivalent))<1e-12);
 assert.ok(Math.abs(Math.sin(h.cube().rotationZ)-Math.sin(equivalent))<1e-12);h.fire("pointercancel",999);assert.equal(h.cube().rotationZ,.3);
});
for(const [name,target,helper] of [["parent",{parentMatrix:matrix},{}],["vertices",{vertices:{positions:[0,0,0,1,0,0,0,1,0]}},{}],["tilt-x",{rotationX:.1},{}],["tilt-y",{rotationY:.1},{}],["spin-x",{spinX:.1},{}],["spin-y",{spinY:.1},{}],["helper-parent",{},{parentMatrix:matrix}]])test(`unsupported rotate ${name} remains event-only`,async()=>{
 const h=await mountedRotate(true,target,helper);const before=[h.cube().rotationX,h.cube().rotationY,h.cube().rotationZ];
 h.fire("pointerdown",372);h.fire("pointermove",320,128);h.fire("pointerup",320,128);
 assert.deepEqual(h.phases.map(p=>p.phase),["start","move","end"]);assert.deepEqual([h.cube().rotationX,h.cube().rotationY,h.cube().rotationZ],before);
 assert.equal(h.mount.getAttribute("data-gosx-scene3d-gizmo-preview-diagnostic"),"unsupported-world-z-transform");assert.equal(h.mount.getAttribute("data-gosx-scene3d-gizmo-diagnostic"),"");
});
