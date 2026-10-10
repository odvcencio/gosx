"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const vm = require("node:vm");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const {spawnSync} = require("node:child_process");
const {readSceneRendererBackendSrc} = require("./scene3d-renderer-source-set.js");

function fn(source, name) {
  const start = source.indexOf("function " + name + "(");
  assert.ok(start >= 0, name);
  let depth = 0;
  for (let i = source.indexOf("{", start); i < source.length; i++) {
    if (source[i] === "{") depth++;
    if (source[i] === "}" && --depth === 0) return source.slice(start, i + 1);
  }
  throw new Error("unclosed " + name);
}
const texture = {name:"environment", dimension:"cube", gl:{unit:2}, wgsl:{textureBinding:1,samplerBinding:2}};
const material = () => ({customUniforms:{environment:"gosx:environment:radiance"}});

test("PBR vertex variants compile with the degenerate tangent fallback", t => {
  const source = readSceneRendererBackendSrc("webgl");
  const context = vm.createContext({});
  // Read the actual authored constants, including the shared affine/crowd
  // helpers. Compile all variants: a duplicate local can otherwise hide in
  // a path that a fake GPU accepts but a real driver rejects.
  const names = ["SCENE_GLSL_AFFINE_NORMAL", "SCENE_CROWD_MOTION_MAX_CLIPS",
    "SCENE_CROWD_MOTION_ATTRIBUTES_GLSL", "SCENE_CROWD_SKIN_MOTION_GLSL",
    "SCENE_PBR_VERTEX_SOURCE", "SCENE_PBR_INSTANCED_VERTEX_SOURCE",
    "SCENE_PBR_CROWD_MOTION_VERTEX_SOURCE", "SCENE_PBR_SKINNED_VERTEX_SOURCE"];
  for (const name of names) {
    const start = source.indexOf("const " + name + " = ");
    assert.ok(start >= 0, name);
    const valueStart = start + ("const " + name + " = ").length;
    const end = source[valueStart] === "["
      ? source.indexOf('].join("\\n");', valueStart) + '].join("\\n");'.length
      : source.indexOf(";\n", valueStart) + 1;
    vm.runInContext(source.slice(start, end), context);
  }
  if (spawnSync("glslangValidator", ["--version"]).status !== 0) {
    t.skip("glslangValidator unavailable");
    return;
  }
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "gosx-pbr-tangents-"));
  try {
    for (const name of names.filter(name => name.includes("VERTEX_SOURCE"))) {
      const shader = vm.runInContext(name, context);
      const file = path.join(dir, name + ".vert");
      fs.writeFileSync(file, shader);
      const compiled = spawnSync("glslangValidator", ["-S", "vert", file], {encoding:"utf8"});
      assert.equal(compiled.status, 0, name + ": " + compiled.stdout + compiled.stderr);
    }
  } finally {
    fs.rmSync(dir, {recursive:true, force:true});
  }
});

test("WebGL Selena shares validated IBL textures and uses cube-safe pending fallback", () => {
  const source = readSceneRendererBackendSrc("webgl");
  const bound = [], fetched = [];
  const cube = {}, placeholder2D = {}, radiance = {};
  const context = vm.createContext({
    textureCache:{_gosxSelenaEnvironment:null,_sceneTextureEpoch:0}, selenaPlaceholderTexture:placeholder2D,
    sceneNumber:(v,f)=>Number.isFinite(v)?v:f,
    sceneSelenaTextureDescriptors:l=>l.textures,
    scenePBRPlaceholderCube:()=>({texture:cube}),
    scenePBRBindTexture:(gl,unit,value,target)=>bound.push({unit,value,target}),
    scenePBRLoadTexture:(gl,url)=>{fetched.push(url); return null;},
  });
  vm.runInContext(["sceneWebGLSelenaEnvironmentSlot","sceneSelenaTextureURL","sceneWebGLBindSelenaTextures"].map(n=>fn(source,n)).join("\n"),context);
  const gl = {TEXTURE_CUBE_MAP:34067,TEXTURE_2D:3553,getUniformLocation:()=>({}),uniform1i:()=>{}};
  const info = {program:{},layout:{textures:[texture]}};
  context.sceneWebGLBindSelenaTextures(gl,info,material(),context.textureCache,placeholder2D);
  assert.equal(bound.at(-1).value,cube);
  assert.equal(bound.at(-1).target,gl.TEXTURE_CUBE_MAP);
  context.textureCache._gosxSelenaEnvironment={radiance:{texture:radiance},info:[1,.7,.2,6]};
  context.sceneWebGLBindSelenaTextures(gl,info,material(),context.textureCache,placeholder2D);
  assert.equal(bound.at(-1).value,radiance);
  assert.equal(bound.at(-1).unit,2);
  context.textureCache._gosxSelenaEnvironment=null;
  context.sceneWebGLBindSelenaTextures(gl,info,material(),context.textureCache,placeholder2D);
  assert.equal(bound.at(-1).value,cube,"removing environment must clear the previous resource");
  assert.deepEqual(fetched,[],"renderer resource refs must never become network URLs");
  assert.equal(context.textureCache._sceneTextureEpoch,3,"PBR material cache must rebind maps after Selena changes shared texture units");
  assert.equal(context.sceneWebGLSelenaEnvironmentSlot(material(),{...texture,dimension:"2d"}),"");
  assert.equal(context.sceneSelenaTextureURL(material(),texture,0),"");
});

test("WebGPU Selena reuses IBL views, updates bind groups on readiness/replacement and never refetches", () => {
  const source = readSceneRendererBackendSrc("webgpu");
  const buffer={}, cube={}, image={}, sampler={}, groups=[];
  const context=vm.createContext({
    selenaFrame:{}, iblResources:{active:false}, placeholderCubeView:cube,placeholderView:image,
    envMapSampler:sampler,linearSampler:{},GPUBufferUsage:{UNIFORM:1,COPY_DST:2},
    device:{createBindGroup:d=>{groups.push(d);return d;}},
    sceneNumber:(v,f)=>Number.isFinite(v)?v:f,
    sceneSelenaUniformData:()=>new Float32Array(4),sceneSelenaUniformBufferSlot:()=>"uniforms",
    wgpuCachedTrackedBuffer:()=>buffer,sceneSelenaTextureDescriptors:l=>l.textures,
    sceneSelenaStorageBufferDescriptors:()=>[],sceneSelenaStateDescriptors:()=>[],
    sceneSelenaLiveTextureView:()=>null,
    sceneSelenaTextureURL:()=>{throw Error("IBL resource passed to URL loader");},
  });
  vm.runInContext(["sceneWebGPUSelenaEnvironmentSlot","sceneWebGPUAppendSelenaTextures","createSelenaBindGroup"].map(n=>fn(source,n)).join("\n"),context);
  context.selenaTextureContext={
    device:context.device,textureCache:{},iblResources:context.iblResources,
    placeholderCubeView:cube,placeholderView:image,envMapSampler:sampler,linearSampler:context.linearSampler,
    liveView:context.sceneSelenaLiveTextureView,url:context.sceneSelenaTextureURL,
  };
  const mat=material(), owner={}, resource={layout:{textures:[texture]},bindGroupLayout:{}};
  const pending=context.createSelenaBindGroup(mat,resource,owner);
  assert.equal(pending.entries[1].resource,cube);
  assert.equal(pending.entries[2].resource,sampler,"explicit mip sampling must use the environment sampler");
  const view={}; Object.assign(context.iblResources,{active:true,radiance:{view}});
  const ready=context.createSelenaBindGroup(mat,resource,owner);
  assert.equal(ready.entries[1].resource,view);
  assert.notEqual(ready,pending);
  assert.equal(context.createSelenaBindGroup(mat,resource,owner),ready,"steady frames must reuse bind groups");
  context.iblResources.radiance={view:{}};
  assert.notEqual(context.createSelenaBindGroup(mat,resource,owner),ready,"replacement environment must not keep stale bindings");
  context.iblResources.active=false;
  assert.equal(context.createSelenaBindGroup(mat,resource,owner),pending);
  assert.equal(groups.length,3);
  const waterView={};
  context.selenaTextureContext.liveView=()=>waterView;
  const water=context.createSelenaBindGroup({customUniforms:{environment:"gosx:water:pool:caustics"}},
    {layout:{textures:[{...texture,dimension:"2d"}]},bindGroupLayout:{}},{});
  assert.equal(water.entries[1].resource,waterView,"the shared binder must preserve live water resources");
  assert.equal(water.entries[2].resource,context.linearSampler,"water retains its ordinary sampler");
});

test("environment metadata is renderer-owned only for declared context fields", () => {
  const gpu=readSceneRendererBackendSrc("webgpu");
  const context=vm.createContext({
    sceneSelenaRenderContextUniformValue:()=>undefined,
    sceneSelenaMaterialValue:(m,n)=>m.customUniforms[n],sceneSelenaUniformDefault:()=>undefined,
    sceneSelenaFloatCount:()=>4,
  });
  vm.runInContext(fn(gpu,"sceneSelenaUniformValue"),context);
  const m={customUniforms:{environmentInfo:[9,9,9,9]}}, frame={environmentInfo:[1,.5,.2,7]};
  assert.equal(context.sceneSelenaUniformValue(m,{}, {name:"environmentInfo",class:"context"},null,null,frame),frame.environmentInfo);
  assert.equal(context.sceneSelenaUniformValue(m,{}, {name:"environmentInfo",class:"param"},null,null,frame),m.customUniforms.environmentInfo);
  assert.deepEqual(Array.from(context.sceneSelenaUniformValue(m,{}, {name:"environmentInfo",class:"context"},null,null,{})),[0,0,0,0]);
});
