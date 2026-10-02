"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createBoardWebGPUHarness, createWebGLRendererForPost } = require("./runtime-test-harness.js");

async function fixture() {
  const h = await createBoardWebGPUHarness({fresh: true});
  h.canvas.width=64; h.canvas.height=64;
  const api = h.env.context.__gosx_scene3d_api;
  const objects = [
    {id:"wall",kind:"box",width:4,height:4,depth:0.2,z:-2,color:"#ff9020",materialKind:"standard"},
    {id:"glass",kind:"box",width:2,height:2,depth:1,z:0,color:"#ffffff",materialKind:"standard",opacity:1,transmission:1,thickness:1,attenuationDistance:2,attenuationColor:[0.25,0.5,1],ior:1.5},
  ];
  const bundle = api.createSceneRenderBundle(64,64,"#000000",{x:0,y:0,z:5,fov:60,near:0.1,far:100},objects,[],[],[],[],{},0,[],[],[],[],[],0,false);
  return {h,bundle};
}

test("opaque glass routes after opaque objects; mip capture excludes glass and reuses resources", async () => {
  const {h,bundle}=await fixture();
  const start=h.fake.state.renderPasses.length;
  h.renderer.render(bundle,{width:64,height:64},{qualityProfile:{tier:"full"}});
  const passes=h.fake.state.renderPasses.slice(start);
  const mips=passes.filter(p=>String(p.descriptor.label||"").startsWith("gosx-transmission-mip-"));
  assert.equal(mips.length,7,"64 px needs seven mip levels");
  const first=passes.indexOf(mips[0]),last=passes.indexOf(mips.at(-1));
  assert.ok(first>0 && last<passes.length-1,"opaque and glass draws must bracket the capture");
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-transmission"),"screen");
  assert.equal(h.env.context.__gosx_scene3d_api.sceneTransmissionDepthWrite({},bundle.materials[1],false),true);
  assert.equal(h.env.context.__gosx_scene3d_api.sceneTransmissionDepthWrite({depthWrite:false},bundle.materials[1],false),false);
  assert.equal(h.env.context.__gosx_scene3d_api.sceneTransmissionDepthWrite({},Object.assign({},bundle.materials[1],{opacity:0.5}),false),false);
  const count=h.fake.state.textures.length;
  h.renderer.render(bundle,{width:64,height:64},{qualityProfile:{tier:"full"}});
  assert.equal(h.fake.state.textures.length,count,"stable frames must not allocate capture textures");
});

test("low tiers skip capture; recovering and resizing rebuild without retaining the old target", async () => {
  const {h,bundle}=await fixture();
  for(const tier of ["constrained","survival","minimal"]) {
    const start=h.fake.state.renderPasses.length;
    h.renderer.render(bundle,{width:64,height:64},{qualityProfile:{tier}});
    assert.equal(h.fake.state.renderPasses.slice(start).filter(p=>String(p.descriptor.label||"").startsWith("gosx-transmission-mip-")).length,0);
    assert.equal(h.mount.getAttribute("data-gosx-scene3d-transmission"),"environment");
  }
  h.renderer.render(bundle,{width:64,height:64},{qualityProfile:{tier:"balanced"}});
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-transmission"),"screen");
  h.renderer.render(bundle,{width:128,height:64},{qualityProfile:{tier:"full"}});
  h.renderer.render(bundle,{width:128,height:64},{qualityProfile:{tier:"constrained"}});
  assert.equal(h.mount.getAttribute("data-gosx-scene3d-transmission"),"environment");
  h.renderer.dispose();
});

test("volume fields change material identity and preserve exact black attenuation", async () => {
  const {h}=await fixture();
  const api=h.env.context.__gosx_scene3d_api;
  const make=thickness=>api.createSceneRenderBundle(64,64,"#000000",{x:0,y:0,z:5,fov:60,near:0.1,far:100},
    [{id:"sheet",kind:"box",width:1,height:1,depth:1,materialKind:"standard",transmission:1,thickness,attenuationDistance:2,attenuationColor:[0,0.5,1]}],[],[],[],[],{},0,[],[],[],[],[],0,false);
  const a=make(1), b=make(2);
  assert.notEqual(a.materials[0].key,b.materials[0].key);
  const ray=api.sceneTransmissionVolume(a.materials[0]);
  assert.deepEqual(Array.from(ray),[1,1.5,0.5,0,0,0.5,1,0]);
});

test("WebGL restores the glass shader after the ocean and bounds mip generation", () => {
  const h = createWebGLRendererForPost({fresh:true});
  const gl = h.canvas.getContext("webgl2");
  const api = h.env.context.__gosx_scene3d_api;
  const bundle = api.createSceneRenderBundle(320,180,"#000000",{x:0,y:2,z:5,fov:60,near:0.1,far:100},
    [{id:"glass",kind:"box",width:2,height:2,depth:1,materialKind:"standard",transmission:1,thickness:1,ior:1.5}],
    [],[],[],[],{ocean:{size:10,resolution:8}},0,[],[],[],[],[],0,false);
  const start = gl.ops.length;
  h.renderer.render(bundle,{width:320,height:180},{qualityProfile:{tier:"balanced"}});
  const ops = gl.ops.slice(start);
  const copy = ops.findIndex(op=>op[0]==="blitFramebuffer");
  assert.ok(copy>0,"capture must follow the ocean");
  assert.ok(ops.some(op=>op[0]==="texParameteri" && op[2]===gl.TEXTURE_MAX_LEVEL && op[3]===4));
  assert.ok(ops.some(op=>op[0]==="generateMipmap"));
  const ocean = ops.slice(0,copy).findLast(op=>op[0]==="drawArrays");
  const glass = ops.slice(copy).find(op=>op[0]==="drawArrays" || op[0]==="drawElements");
  assert.ok(ocean && glass);
  assert.notEqual(glass.at(-1),ocean.at(-1),"glass must use its PBR shader, not the ocean shader");
  const lowStart = gl.ops.length;
  h.renderer.render(bundle,{width:320,height:180},{qualityProfile:{tier:"constrained"}});
  assert.ok(!gl.ops.slice(lowStart).some(op=>op[0]==="generateMipmap"));
  h.renderer.dispose();
});

test("WebGL shared water composites before glass capture and transparent overlays", () => {
  const h = createWebGLRendererForPost({fresh:true});
  const gl = h.canvas.getContext("webgl2");
  const api = h.env.context.__gosx_scene3d_api;
  const bundle = api.createSceneRenderBundle(320,180,"#000000",{x:0,y:2,z:5,fov:60,near:0.1,far:100},
    [{id:"glass",kind:"box",width:2,height:2,depth:1,materialKind:"standard",transmission:1,thickness:1}],
    [],[],[],[],{},0,[],[],[],[],[],0,false);
  for (const tier of ["full", "constrained"]) {
    const start = gl.ops.length;
    let composites = 0;
    h.renderer.render(bundle,{width:320,height:180},{qualityProfile:{tier},compositeBeforePost() {
      composites++;
      gl.ops.push(["water-composite"]);
    }});
    const ops = gl.ops.slice(start);
    const water = ops.findIndex(op => op[0] === "water-composite");
    const capture = ops.findIndex(op => op[0] === "blitFramebuffer");
    const glass = ops.findIndex((op, i) => i > water && (op[0] === "drawArrays" || op[0] === "drawElements"));
    assert.equal(composites, 1);
    assert.ok(water >= 0 && glass > water);
    if (tier === "full") assert.ok(capture > water && glass > capture);
  }
  h.renderer.dispose();
});
