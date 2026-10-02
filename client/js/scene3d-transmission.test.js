"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const { createBoardWebGPUHarness, createWebGLRendererForPost, flushAsyncWork } = require("./runtime-test-harness.js");

function triangle(material) {
  return { ...material, kind: "mesh", vertices: {
    count: 3, positions: [-1, -1, 0, 1, -1, 0, 0, 1, 0],
    normals: [0, 0, 1, 0, 0, 1, 0, 0, 1], uvs: [0, 0, 1, 0, 0.5, 1],
  } };
}

async function fixture() {
  const h = await createBoardWebGPUHarness({fresh: true});
  h.canvas.width=64; h.canvas.height=64;
  const api = h.env.context.__gosx_scene3d_api;
  const objects = [
    triangle({id:"wall",z:-2,color:"#ff9020",materialKind:"standard"}),
    triangle({id:"glass",z:0,color:"#ffffff",materialKind:"standard",opacity:1,transmission:1,thickness:1,attenuationDistance:2,attenuationColor:[0.25,0.5,1],ior:1.5}),
  ];
  const bundle = api.createSceneRenderBundle(64,64,"#000000",{x:0,y:0,z:5,fov:60,near:0.1,far:100},objects,[],[],[],[],{},0,[],[],[],[],[],0,false);
  assert.equal(bundle.meshObjects.length, 2, "the fixture contains drawable opaque and glass triangles");
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
  const opaque = passes.slice(0, first).flatMap(p => p.draws).filter(d => d.vertexCount === 3);
  const glass = passes.slice(last + 1).flatMap(p => p.draws).filter(d => d.vertexCount === 3);
  assert.equal(opaque.length, 1, "the opaque triangle draws before capture");
  assert.equal(glass.length, 1, "the glass triangle draws after capture");
  assert.equal(glass[0].pipeline.desc.depthStencil.depthWriteEnabled, true, "opaque glass writes depth");
  assert.equal(glass[0].pipeline.desc.depthStencil.depthCompare, "less-equal");
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

test("WebGPU glass draw pipelines honor explicit depthWrite and partial opacity", async t => {
  const {h,bundle}=await fixture();
  t.after(() => h.renderer.dispose());
  for (const [depthWrite, opacity, expected] of [[false,1,false], [undefined,0.35,false], [undefined,1,true]]) {
    bundle.meshObjects.find(object => object.id === "glass").depthWrite = depthWrite;
    bundle.materials[1] = {...bundle.materials[1], opacity};
    const start = h.fake.state.renderPasses.length;
    h.renderer.render(bundle,{width:64,height:64},{qualityProfile:{tier:"full"}});
    const passes = h.fake.state.renderPasses.slice(start);
    const capture = passes.findLastIndex(p => String(p.descriptor.label||"").startsWith("gosx-transmission-mip-"));
    const glass = passes.slice(capture+1).flatMap(p => p.draws).filter(d => d.vertexCount === 3);
    assert.equal(glass.length, 1, "the glass triangle actually draws after capture");
    assert.equal(glass[0].pipeline.desc.depthStencil.depthWriteEnabled, expected);
  }
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
    [triangle({id:"glass",materialKind:"standard",transmission:1,thickness:1,ior:1.5})],
    [],[],[],[],{ocean:{size:10,resolution:8}},0,[],[],[],[],[],0,false);
  const start = gl.ops.length;
  h.renderer.render(bundle,{width:320,height:180},{qualityProfile:{tier:"balanced"}});
  const ops = gl.ops.slice(start);
  const copy = ops.findIndex(op=>op[0]==="blitFramebuffer");
  assert.ok(copy>0,"capture must follow the ocean");
  assert.ok(ops.some(op=>op[0]==="texParameteri" && op[2]===gl.TEXTURE_MAX_LEVEL && op[3]===4));
  assert.ok(ops.some(op=>op[0]==="generateMipmap"));
  const ocean = ops.slice(0,copy).findLast(op=>op[0]==="drawArrays");
  const glass = ops.slice(copy).find(op=>op[0]==="drawArrays" && op[3]===3);
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
    [triangle({id:"glass",materialKind:"standard",transmission:1,thickness:1})],
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
    const glass = ops.findIndex((op, i) => i > water && op[0] === "drawArrays" && op[3] === 3);
    assert.equal(composites, 1);
    assert.ok(water >= 0 && glass > water);
    if (tier === "full") assert.ok(capture > water && glass > capture);
  }
  h.renderer.dispose();
});

test("WebGL textured glass keeps its loaded albedo on frame three after automatic post processing", async t => {
  const h = createWebGLRendererForPost({fresh:true});
  t.after(() => h.renderer.dispose());
  const gl = h.canvas.getContext("webgl2"), api = h.env.context.__gosx_scene3d_api;
  let active = gl.TEXTURE0;
  const bindings = new Map(), draws = [];
  const activeTexture = gl.activeTexture.bind(gl), bindTexture = gl.bindTexture.bind(gl), drawArrays = gl.drawArrays.bind(gl);
  gl.activeTexture = unit => { active = unit; activeTexture(unit); };
  gl.bindTexture = (target, texture) => { if (target === gl.TEXTURE_2D) bindings.set(active, texture); bindTexture(target, texture); };
  gl.drawArrays = (mode, first, count) => {
    if (count === 3) draws.push(bindings.get(gl.TEXTURE0));
    drawArrays(mode, first, count);
  };
  const bundle = api.createSceneRenderBundle(64,64,"#000000",{x:0,y:0,z:5,fov:60,near:0.1,far:100},
    [triangle({id:"glass",materialKind:"standard",texture:"/albedo.png",transmission:1,thickness:1})],
    [],[],[],[],{},0,[],[],[],[],[],0,false);
  const render = tier => h.renderer.render(bundle,{width:64,height:64},{qualityProfile:{tier}});
  render("full");
  await flushAsyncWork();
  render("full");
  const albedo = draws[1];
  assert.ok(albedo, "frame two binds the loaded albedo");
  for (const tier of ["full", "full", "constrained", "full"]) render(tier);
  assert.equal(draws.length, 6, "each frame draws the glass triangle");
  for (let i=2; i<draws.length; i++) assert.equal(draws[i], albedo, `frame ${i+1} retains the loaded albedo`);
});

for (const backend of ["WebGL", "WebGPU"]) {
  test(`${backend} implicit transmission output retains default ACES across quality tiers`, async t => {
    const h = backend === "WebGPU" ? await createBoardWebGPUHarness({fresh:true}) : createWebGLRendererForPost({fresh:true});
    t.after(() => h.renderer.dispose());
    const api = h.env.context.__gosx_scene3d_api;
    const bundle = api.createSceneRenderBundle(64,64,"#000000",{x:0,y:0,z:5,fov:60,near:0.1,far:100},
      [triangle({id:"glass",materialKind:"standard",transmission:1})],[],[],[],[],{},0,[],[],[],[],[],0,false);
    for (const tier of ["constrained", "full", "balanced", "constrained"]) {
      const operations = backend === "WebGL" ? h.canvas.getContext("webgl2").ops : h.fake.state.writeBufferCalls;
      const start = operations.length;
      h.renderer.render(bundle,{width:64,height:64},{qualityProfile:{tier}});
      const frame = operations.slice(start);
      const mode = backend === "WebGL"
        ? frame.findLast(op => op[0] === "uniform1i" && op[1] === "u_toneMapMode")[2]
        : tier === "constrained" ? new Uint32Array(frame.find(op => op.data?.length >= 40 && op.offset === 0).data.buffer)[38]
          : frame.findLast(op => op.data?.length === 4).data[1];
      assert.equal(mode, 1, `${tier} uses the existing ACES curve`);
    }
  });

  test(`${backend} transparent transmission canvas preserves uncovered and glass alpha`, async t => {
    const h = backend === "WebGPU" ? await createBoardWebGPUHarness({fresh:true}) : createWebGLRendererForPost({fresh:true});
    t.after(() => h.renderer.dispose());
    const api = h.env.context.__gosx_scene3d_api;
    const bundle = api.createSceneRenderBundle(64,64,"transparent",{x:0,y:0,z:5,fov:60,near:0.1,far:100},
      [triangle({id:"glass",materialKind:"standard",transmission:1,opacity:0.35})],[],[],[],[],{},0,[],[],[],[],[],0,false);
    h.renderer.render(bundle,{width:64,height:64},{qualityProfile:{tier:"full"}});
    let shader, clearAlpha;
    if (backend === "WebGL") {
      const gl = h.canvas.getContext("webgl2");
      clearAlpha = gl.ops.findLast(op => op[0] === "clearColor")[4];
      const finalDraw = gl.ops.findLast(op => op[0] === "drawArrays");
      assert.equal(gl.ops.findLast(op => op[0] === "bindFramebuffer")[2], null, "output reaches the canvas");
      shader = gl.programs.find(p => p.id === finalDraw[4]).attached.find(s => s.type === gl.FRAGMENT_SHADER).source;
    } else {
      clearAlpha = h.fake.state.renderPasses.find(p => p.descriptor.colorAttachments?.[0]?.clearValue).descriptor.colorAttachments[0].clearValue.a;
      shader = h.fake.state.renderPasses.flatMap(p => p.draws).find(d => d.pipeline?.desc?.fragment?.module?.label === "post-toneMapping").pipeline.desc.fragment.module.code;
      const output = h.fake.state.renderPasses.at(-1);
      assert.equal(output.descriptor.colorAttachments[0].view.__kind, "canvasTextureView");
      assert.match(output.draws[0].pipeline.desc.fragment.module.code, /return textureSample\(inputTex, inputSamp, uv\);/, "presentation passes the tone-mapped RGBA through");
    }
    assert.equal(clearAlpha, 0, "the scene target clears uncovered pixels to transparent");
    // Evaluate the alpha expression from the shader actually used by the output draw.
    const expression = shader.match(/(?:fragColor = vec4|return vec4f)\(color, ([^;]+)\);/)[1];
    const sample = shader.match(/(?:vec4|let) (\w+) = (?:texture|textureSample)\(/)?.[1];
    const outputAlpha = new Function(sample || "sample", `return ${expression};`);
    for (const alpha of [clearAlpha, 0.35, 1]) {
      const actual = outputAlpha({a:alpha});
      assert.equal(actual, alpha, "the post pass preserves input alpha");
      assert.equal(0.2 * actual + 0.8 * (1-actual), 0.2 * alpha + 0.8 * (1-alpha), "canvas compositing retains the page behind glass");
    }
  });
}
