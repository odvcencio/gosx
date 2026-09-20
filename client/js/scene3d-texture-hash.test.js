"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

function runtime() {
  const context = vm.createContext({ window: {}, console });
  for (const name of ["10-runtime-primitives.ts", "10-runtime-scene-utils.ts", "11-scene-math.ts", "12-scene-geometry.ts", "13-scene-material.ts", "10-runtime-scene-core.ts", "15-scene-draw-plan.ts", "15b-scene-planner.ts"]) {
    let source = fs.readFileSync(path.join(__dirname, "bootstrap-src", name), "utf8");
    if (name === "10-runtime-scene-core.ts") source = source.slice(0, source.indexOf("// Scene3D shared API"));
    vm.runInContext(source, context, { filename: name });
  }
  return context;
}

test("HTML rerasterization never serializes its previous generated texture into itself", () => {
  const api = runtime();
  api.document = {};
  // Exercise the real clone/serialization path, including an existing mirror
  // URL, rather than the authored-markup fallback used by non-DOM tests.
  class Element {
    constructor(attributes, markup) { this.attributes = { ...attributes }; this.innerHTML = markup; }
    cloneNode() { return new Element(this.attributes, this.innerHTML); }
    removeAttribute(key) { delete this.attributes[key]; }
    setAttribute(key, value) { this.attributes[key] = value; }
    querySelectorAll() { return []; }
  }
  api.XMLSerializer = class {
    serializeToString(element) {
      const attrs = Object.entries(element.attributes).map(([k,v]) => `${k}="${String(v).replaceAll('&','&amp;').replaceAll('"','&quot;')}"`).join(' ');
      return `<div ${attrs}>${element.innerHTML}</div>`;
    }
  };
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../runtime/scene3d/overlay-dom.ts'), 'utf8'), api);
  const element = new Element({ class: 'relay', 'data-gosx-scope': 'terminal',
    'data-gosx-scene-html-texture-key': 'data:image/svg+xml,PREVIOUS_RASTER',
  }, '<strong>Charge 000</strong>');
  let firstLength = 0;
  for (let update = 0; update < 200; update++) {
    element.innerHTML = `<strong>Charge ${String(update).padStart(3,'0')}</strong>`;
    const previous = element.attributes['data-gosx-scene-html-texture-key'];
    const content = api.sceneHTMLTextureSerializeContent({ html: element.innerHTML }, element, 512, 192);
    assert.ok(!content.includes('data-gosx-scene-html-texture-key'));
    assert.ok(!content.includes(previous));
    assert.ok(content.includes(element.innerHTML));
    assert.ok(content.includes('data-gosx-scope="terminal"'));
    assert.equal(element.attributes['data-gosx-scene-html-texture-key'], previous, 'only the raster clone is stripped');
    const next = 'data:image/svg+xml,' + encodeURIComponent(content);
    if (!firstLength) firstLength = next.length;
    assert.equal(next.length, firstLength, 'equal-size markup keeps a constant raster payload');
    element.attributes['data-gosx-scene-html-texture-key'] = next;
  }
});

test("moving objects reuse material profiles and in-place appearance edits invalidate them", () => {
  const api = runtime();
  const object = { materialKind: "standard", color: "#445566", roughness: .4,
    texture: "data:image/png;base64," + "a".repeat(4096),
    specularColor: [1, .8, .6], customUniforms: { pulse: [1, 2] },
    textureDescriptors: { baseColor: { uri: "/first.png", colorSpace: "srgb" } },
  };
  const first = api.sceneObjectMaterialProfile(object);
  assert.equal(api.sceneObjectMaterialProfile({ ...object }), first, "equal actors share profile identity");
  for (let frame = 0; frame < 180; frame++) {
    object.x = frame / 60;
    assert.equal(api.sceneObjectMaterialProfile(object), first);
  }
  for (const change of [
    () => object.color = "#778899",
    () => object.texture = object.texture.slice(0, -1) + "b",
    () => object.roughness = "var(--shell-roughness)",
    () => object.specularColor[1] = .25,
    () => object.customUniforms.pulse[1] = 9,
    () => object.textureDescriptors.baseColor.colorSpace = "linear",
    () => object.customFragment = "void main() {}",
    () => object.renderPass = "alpha",
  ]) {
    const prior = api.sceneObjectMaterialProfile(object);
    change();
    const next = api.sceneObjectMaterialProfile(object);
    assert.notEqual(next, prior);
    assert.notEqual(next.key, prior.key);
    assert.equal(api.sceneObjectMaterialProfile(object), next);
  }
});

test("material indices remain bundle-local with shared, frozen and interleaved profiles", () => {
  const api=runtime(), profile=api.sceneObjectMaterialProfile({color:"#123456"});
  const frozen=Object.freeze({...profile,color:"#654321",key:"frozen"});
  const first={materials:[]}, second={materials:[]}, a=new Map(), b=new Map();
  assert.equal(api.sceneBundleMaterialIndex(first,a,profile),0);
  assert.equal(api.sceneBundleMaterialIndex(second,b,frozen),0);
  assert.equal(api.sceneBundleMaterialIndex(second,b,profile),1);
  for(let i=0;i<100;i++) {
    assert.equal(api.sceneBundleMaterialIndex(first,a,profile),0);
    assert.equal(api.sceneBundleMaterialIndex(second,b,profile),1);
    assert.equal(api.sceneBundleMaterialIndex(second,b,frozen),0);
  }
  assert.equal(first.materials.length,1);
  assert.equal(second.materials.length,2);
  assert.equal(JSON.stringify(profile).includes("_sceneBundle"),false);
  assert.equal(typeof profile._sceneBundleToken,"number");
});

test("direct TRS matrices match point transforms with rotation, nonuniform scale and drift", () => {
  const api = runtime();
  for (let frame = 0; frame < 40; frame++) {
    const object = { x: frame / 7, y: -.7, z: -3, rotationX: .27, rotationY: frame*.09, rotationZ: -.15,
      scaleX: 1.7, scaleY: frame%2 ? -.8 : .8, scaleZ: .6,
      spinX: .15, spinY: -.2, spinZ: .11, shiftX: .3, shiftY: .2, shiftZ: .1, driftPhase: .4, driftSpeed: 1.3 };
    const t=frame*.07, matrix=api.sceneObjectModelMatrix(object,t);
    for (const p of [[0,0,0],[1,0,0],[0,1,0],[0,0,1],[.3,-.7,1.5]]) {
      const expected={}; api.translateScenePointInto(expected,...p,object,t);
      for (const [axis,k] of ['x','y','z'].map((axis,k)=>[axis,k])) {
        const actual=matrix[k]*p[0]+matrix[k+4]*p[1]+matrix[k+8]*p[2]+matrix[k+12];
        assert.ok(Math.abs(actual-expected[axis])<.00001, axis);
      }
    }
  }
});

test("affine bounds and camera depths enclose the actual transformed corners", () => {
  const api=runtime(), local={minX:-2,maxX:1,minY:-.5,maxY:3,minZ:-1,maxZ:2};
  const camera={x:3,y:4,z:8,rotationX:-.5,rotationY:.2,rotationZ:.1};
  for(let i=0;i<40;i++) {
    const object={x:i*.1,y:1,z:-2,rotationX:.3,rotationY:i*.13,rotationZ:-.2,scaleX:1.5,scaleY:.7,scaleZ:i%2?-.9:.9};
    const m=api.sceneObjectModelMatrix(object,0), bounds=api.sceneTransformMeshBounds(local,m);
    const actual={minX:Infinity,minY:Infinity,minZ:Infinity,maxX:-Infinity,maxY:-Infinity,maxZ:-Infinity};
    for(let j=0;j<8;j++) {
      const p={}; api.translateScenePointInto(p,j&1?local.maxX:local.minX,j&2?local.maxY:local.minY,j&4?local.maxZ:local.minZ,object,0);
      for(const axis of ['X','Y','Z']) { actual['min'+axis]=Math.min(actual['min'+axis],p[axis.toLowerCase()]); actual['max'+axis]=Math.max(actual['max'+axis],p[axis.toLowerCase()]); }
    }
    for(const k in actual) assert.ok(Math.abs(actual[k]-bounds[k])<.00001,k);
    // Move camera components in opposite directions: their sum remains the
    // same, but the depth result must follow the actual pose.
    camera.x+=.03; camera.z-=.03;
    const depth=api.sceneBoundsDepthMetrics(bounds,camera,object), expected=[];
    for(let j=0;j<8;j++) {
      const p=api.sceneInverseRotatePoint({x:(j&1?bounds.maxX:bounds.minX)-camera.x,y:(j&2?bounds.maxY:bounds.minY)-camera.y,z:(j&4?bounds.maxZ:bounds.minZ)-camera.z},camera.rotationX,camera.rotationY,camera.rotationZ);
      expected.push(-p.z);
    }
    assert.ok(Math.abs(depth.near-Math.min(...expected))<.00001);
    assert.ok(Math.abs(depth.far-Math.max(...expected))<.00001);
  }
});

test("profile reuse observes registry changes and keeps dynamic shader factories live", () => {
  const api = runtime();
  const object = { materialKind: "shell" };
  const first = api.sceneObjectMaterialProfile(object);
  api.registerSceneMaterialProfile("shell", { opacity: .5, shaderData: [2, .1, .9] });
  const registered = api.sceneObjectMaterialProfile(object);
  assert.notEqual(registered, first);
  assert.equal(registered.opacity, .5);
  let pulse = 0;
  api.registerSceneMaterialProfile("shell", { shaderData: () => [2, ++pulse, 1] });
  const a = api.sceneObjectMaterialProfile(object);
  const b = api.sceneObjectMaterialProfile(object);
  assert.notEqual(a, b);
  assert.ok(b.shaderData[1] > a.shaderData[1]);
  api.unregisterSceneMaterialProfile("shell");
  assert.notEqual(api.sceneObjectMaterialProfile(object).key, registered.key);
});

test("CSS property reads are shared within a resolution pass and refresh on the next pass", () => {
  const api = runtime();
  let reads = 0, color = "#123456";
  api.window.getComputedStyle = () => ({ getPropertyValue: name => { reads++; return name === "--shell" ? color : ""; } });
  const mount = {};
  const pass = api.sceneCSSResolverContext({ mount });
  for (let i = 0; i < 1000; i++) {
    assert.equal(api.sceneCSSReadPropertyOnElement(pass, mount, "--shell"), color);
    assert.equal(api.sceneCSSReadPropertyOnElement(pass, mount, "--missing"), null);
  }
  assert.equal(reads, 2, "cache empty inherited properties as well as existing values");
  color = "#abcdef";
  const next = api.sceneCSSResolverContext({ mount, revision: 2 });
  assert.equal(api.sceneCSSReadPropertyOnElement(next, mount, "--shell"), color);
  assert.equal(reads, 3);
});

test("embedded texture content is scanned once across changing frame seeds and both planners", () => {
  const api = runtime();
  const texture = "data:image/png;base64," + "a".repeat(1024 * 1024);
  const first = api.scenePlannerHashString(2166136261, texture);
  assert.equal(api.sceneLongStringHashScannedUnits, texture.length);
  for (let frame = 0; frame < 180; frame++) {
    assert.equal(api.scenePlannerHashString(frame, texture), api.sceneHashString(frame, texture));
  }
  assert.equal(api.sceneLongStringHashScannedUnits, texture.length);
  assert.equal(api.scenePlannerHashString(2166136261, texture), first);
  assert.notEqual(api.scenePlannerHashString(2166136260, texture), first);
  assert.notEqual(api.scenePlannerHashString(2166136261, texture.slice(0, -1) + "b"), first);
});

test("cached texture digest preserves keyed material appearance invalidation", () => {
  const api = runtime();
  const texture = "data:image/png;base64," + "a".repeat(4096);
  const material = { key: "stable:" + texture, kind: "standard", texture, color: "#ffffff", opacity: 1, emissive: 0, roughness: .5, metalness: .3 };
  const hash = value => api.scenePlannerMaterialsHash({ materials: [value] });
  const initial = hash(material);
  for (const [field, value] of Object.entries({ color: "#113355", opacity: .4, emissive: .7, roughness: .8, metalness: .6, texture: texture.slice(0, -1) + "b" })) {
    assert.notEqual(hash({ ...material, [field]: value }), initial, field);
  }
  assert.equal(hash({ ...material }), initial);
});

test("texture digest retention is bounded by entry count and total string storage", () => {
  const api = runtime();
  // Smaller budgets exercise real eviction without allocating large fixtures.
  api.sceneLongStringHashMaxEntries = 3;
  api.sceneLongStringHashMaxUnits = 1400;
  const value = "a".repeat(400);
  const original = api.scenePlannerHashString(123, value);
  for (let i = 0; i < 20; i++) api.sceneHashString(i, value + i);
  assert.ok(api.sceneLongStringHashes.size <= 3);
  assert.ok(api.sceneLongStringHashUnits <= 1400);
  assert.equal(api.scenePlannerHashString(123, value), original, "eviction must not change a digest");
  api.sceneHashString(1, "x".repeat(1500));
  assert.ok(api.sceneLongStringHashUnits <= 1400, "oversized strings must not enter the cache");
});

test("material identities stay compact, exact and bounded with embedded atlases", () => {
  const api = runtime();
  const texture = "data:image/png;base64," + "a".repeat(1024 * 1024);
  const material = {kind:"standard", color:"#ffffff", texture, opacity:1};
  const first = api.sceneMaterialProfileKey(material);
  assert.ok(first.length < 1024, "material lookup must not concatenate the atlas into every actor key");
  for (let i = 0; i < 180; i++) assert.equal(api.sceneMaterialProfileKey({...material}), first);
  assert.notEqual(api.sceneMaterialProfileKey({...material, texture:texture.slice(0,-1)+"b"}), first);
  const token = api.sceneMaterialIdentityAtom(texture);
  assert.notEqual(api.sceneMaterialIdentityAtom(token), token, "literal values cannot impersonate interned tokens");
  for (let i = 0; i < 80; i++) api.sceneMaterialIdentityAtom("x".repeat(300)+i);
  assert.ok(vm.runInContext("sceneMaterialIdentityAtoms.size", api) <= 64);
  assert.ok(vm.runInContext("sceneMaterialIdentityUnits", api) <= 16*1024*1024);
  assert.notEqual(api.sceneMaterialIdentityAtom("x".repeat(300)), token, "an evicted token must never identify different data");
});

test("numeric camera depths and poses reuse CSS resolution while the draw plan stays current", () => {
  const api = runtime();
  let reads = 0, tint = "#123456";
  api.window.getComputedStyle = () => ({getPropertyValue: name => {
    reads++; return name === "--shell" ? tint : "";
  }});
  const mount = {}, camera = {x:0,y:0,z:10}, viewport = {width:800,height:600};
  const make = (frame, depth = frame + 4) => ({
    materials: [{kind:"standard",color:"var(--shell)",opacity:1}],
    objects: [{id:"pilot",kind:"box",materialIndex:0,x:frame,y:0,z:0}],
    meshObjects: [{id:"bug",kind:"mesh",materialIndex:0,depthCenter:depth,vertexOffset:0,vertexCount:3}],
  });
  let source = make(0), prepared = api.prepareScene(source,camera,viewport,null,{mount,revision:0});
  const initialReads = reads, signature = prepared.cssCache.inputSignature;
  assert.equal(prepared.ir.materials[0].color, tint);
  for (let frame=1;frame<=60;frame++) {
    source=make(frame);
    const prior=prepared;
    prepared=api.prepareScene(source,camera,viewport,prior,{mount,revision:0});
    assert.equal(prepared.cssCache.inputSignature,signature);
    assert.equal(prepared.ir.objects[0].x,frame);
    assert.equal(prepared.ir.meshObjects[0].depthCenter,frame+4);
    assert.notEqual(prepared.signature,prior.signature,"draw planning must follow movement");
  }
  assert.equal(reads,initialReads,"camera movement must not repeat computed-style reads");
  tint="#abcdef";
  prepared=api.prepareScene(source,camera,viewport,prepared,{mount,revision:1});
  assert.equal(prepared.ir.materials[0].color,tint);
  assert.ok(reads>initialReads,"a CSS revision still refreshes appearance");
  source=make(0,"var(--depth, 7)");
  prepared=api.prepareScene(source,camera,viewport,prepared,{mount,revision:1});
  assert.equal(prepared.ir.meshObjects[0].depthCenter,7);
  source=make(0,42);
  prepared=api.prepareScene(source,camera,viewport,prepared,{mount,revision:1});
  assert.equal(prepared.ir.meshObjects[0].depthCenter,42,"removing a CSS binding must discard its old patch");
  api.testNow=1000;
  vm.runInContext("Date.now = () => testNow",api);
  prepared=api.prepareScene(source,camera,viewport,prepared,{mount,revision:1,animationUntil:2000});
  const animationReads=reads;
  api.testNow=1020;
  tint="#fedcba";
  prepared=api.prepareScene(source,camera,viewport,prepared,{mount,revision:1,animationUntil:2000});
  assert.ok(reads>animationReads,"active CSS transitions refresh each transition frame");
  assert.equal(prepared.ir.materials[0].color,tint);
  const numeric=api.sceneCSSInputSignature(make(0));
  assert.notEqual(api.sceneCSSInputSignature(make(0,"var(--depth, 7)")),numeric);
  source=make(0);source.meshObjects[0].id="replacement";
  assert.notEqual(api.sceneCSSInputSignature(source),numeric,"replacement targets invalidate indexed patches");
  source=make(0);source.objects[0].x="var(--position, 1)";
  assert.notEqual(api.sceneCSSInputSignature(source),numeric);
});

test("producer-stamped retained meshes avoid repeated CSS string hashing without masking mutations", (t) => {
  const api = runtime();
  const makeRecord = index => ({
    id: `actor-${index}`, kind: "mesh", materialIndex: index % 7,
    renderPass: "opaque", _renderPassDerived: false,
    depthCenter: index / 10, vertexOffset: 0, vertexCount: 36,
    retainedGeometry: true,
  });
  const owners = Array.from({ length: 832 }, () => ({}));
  const stamped = owners.map((owner, index) => {
    const record = makeRecord(index);
    api.sceneStampRetainedMeshCSSInput(record, owner);
    return record;
  });
  const generic = stamped.map((_, index) => makeRecord(index));
  const countStringHashes = records => {
    let calls = 0;
    const original = api.scenePlannerHashString;
    api.scenePlannerHashString = function(...args) { calls++; return original(...args); };
    const started = process.hrtime.bigint();
    for (let iteration = 0; iteration < 80; iteration++) {
      api.sceneCSSInputSignature({ meshObjects: records });
    }
    const elapsedMS = Number(process.hrtime.bigint() - started) / 1e6;
    api.scenePlannerHashString = original;
    return { calls, elapsedMS };
  };
  const fast = countStringHashes(stamped);
  const slow = countStringHashes(generic);
  assert.ok(fast.calls < slow.calls / 8,
    `guarded fingerprints should remove per-record string hashes (${fast.calls} vs ${slow.calls})`);
  t.diagnostic(`66,560 retained records: stamped=${fast.elapsedMS.toFixed(2)}ms/${fast.calls} string hashes generic=${slow.elapsedMS.toFixed(2)}ms/${slow.calls}`);
  const measureFreshFrames = stamp => {
    const started = process.hrtime.bigint();
    for (let frame = 0; frame < 80; frame++) {
      const records = owners.map((owner, index) => {
        const record = makeRecord(index);
        if (stamp) api.sceneStampRetainedMeshCSSInput(record, owner);
        return record;
      });
      api.sceneCSSInputSignature({ meshObjects: records });
    }
    return Number(process.hrtime.bigint() - started) / 1e6;
  };
  const stampedFreshMS = measureFreshFrames(true);
  const genericFreshMS = measureFreshFrames(false);
  t.diagnostic(`80 fresh 832-record frames incl stamping: stamped=${stampedFreshMS.toFixed(2)}ms generic=${genericFreshMS.toFixed(2)}ms`);
  const sameOwner = {};
  const firstFrame = makeRecord(5);
  api.sceneStampRetainedMeshCSSInput(firstFrame, sameOwner);
  const firstFrameSignature = api.sceneCSSInputSignature({ meshObjects: [firstFrame] });
  const secondFrame = makeRecord(5);
  api.sceneStampRetainedMeshCSSInput(secondFrame, sameOwner);
  assert.equal(api.sceneCSSInputSignature({ meshObjects: [secondFrame] }), firstFrameSignature,
    "fresh producer records with the same owner and tuple retain their CSS signature");
  secondFrame.materialIndex++;
  api.sceneStampRetainedMeshCSSInput(secondFrame, sameOwner);
  const changedMaterial = api.sceneCSSInputSignature({ meshObjects: [secondFrame] });
  assert.notEqual(changedMaterial, firstFrameSignature, "material-index changes invalidate the owner tuple");
  secondFrame.renderPass = "alpha";
  api.sceneStampRetainedMeshCSSInput(secondFrame, sameOwner);
  assert.notEqual(api.sceneCSSInputSignature({ meshObjects: [secondFrame] }), changedMaterial,
    "render-pass changes invalidate the owner tuple");

  const bundle = { meshObjects: [stamped[0]] };
  const initial = api.sceneCSSInputSignature(bundle);
  stamped[0].id = "replacement";
  assert.notEqual(api.sceneCSSInputSignature(bundle), initial, "identity mutation invalidates the producer stamp");
  stamped[0].id = "actor-0";
  api.sceneStampRetainedMeshCSSInput(stamped[0], owners[0]);
  const restored = api.sceneCSSInputSignature(bundle);
  stamped[0]._renderPassDerived = null;
  assert.notEqual(api.sceneCSSInputSignature(bundle), restored,
    "undefined/boolean/null render-pass provenance remains distinct");
  stamped[0]._renderPassDerived = false;
  api.sceneStampRetainedMeshCSSInput(stamped[0], owners[0]);
  const opaque = api.sceneCSSInputSignature(bundle);
  stamped[0].opacity = "var(--mesh-opacity)";
  assert.notEqual(api.sceneCSSInputSignature(bundle), opaque,
    "an added CSS material input falls back to the general record hash");
  const objectCollection = { objects: [stamped[1]] };
  const objectSignature = api.sceneCSSInputSignature(objectCollection);
  stamped[1].color = "var(--actor-color)";
  assert.notEqual(api.sceneCSSInputSignature(objectCollection), objectSignature,
    "a stamp is trusted only under the meshObjects collection schema");
});


test("HTML surface material keys stay compact without aliasing content changes", () => {
  const api=runtime(), entry={id:"relay"};
  const texture={key:"html:"+"a".repeat(1024*1024),width:512,height:256};
  const key=api.sceneHTMLTextureMaterialKey(entry,texture,.8);
  assert.ok(key.length<128);
  for(let i=0;i<60;i++) assert.equal(api.sceneHTMLTextureMaterialKey(entry,texture,.8),key);
  for(const [e,t,o] of [
    [{id:"other"},texture,.8],
    [entry,{...texture,key:texture.key.slice(0,-1)+"b"},.8],
    [entry,{...texture,width:1024},.8],
    [entry,{...texture,height:512},.8],
    [entry,texture,.7],
  ]) assert.notEqual(api.sceneHTMLTextureMaterialKey(e,t,o),key);
  assert.equal(texture.key.length,1024*1024+5,"upload identity is preserved");
});

test("instanced model templates preserve normalization and re-read batch appearance", () => {
  const api=runtime();
  const batch={id:"swarm",src:" /swarm.glb ",color:"#ffaa00",roughness:.2,visible:false,static:false,
    specularColor:[.8,.7,.6],instances:[{id:"alpha",x:2,scaleX:-1,rotationY:.5},null,
      {z:4,parentMatrix:[1,0,0,0,0,1,0,0,0,0,1,0,3,0,0,1]}]};
  const models=api.sceneInstancedGLBMeshToModels(batch,0);
  assert.equal(models.length,2);
  for(const [slot,index] of [[0,0],[1,2]]) {
    const instance=batch.instances[index];
    const raw={...instance,id:batch.id+"/"+(instance.id||"instance-"+index),src:batch.src};
    for(const key of ['color','roughness','visible','static','specularColor']) raw[key]=batch[key];
    const expected=api.normalizeSceneModel(raw,"0-"+index);expected._instancedGLB=true;
    assert.equal(JSON.stringify(models[slot]),JSON.stringify(expected));
  }
  assert.notEqual(models[0],models[1]);
  batch.color="#0011ff";batch.specularColor[1]=.1;
  const next=api.sceneInstancedGLBMeshToModels(batch,0);
  assert.equal(next[0].materialOverride.color,"#0011ff");
  assert.equal(next[0].materialOverride.specularColor[1],.1);
  assert.equal(models[0].materialOverride.color,"#ffaa00");
});


test("large instance batches normalize their shared declaration once", () => {
  const api=runtime(),normalize=api.normalizeSceneModel;let normalizations=0;
  api.normalizeSceneModel=(...args)=>{normalizations++;return normalize(...args);};
  const batch={id:"swarm",src:"/actor.glb",instances:Array.from({length:180},(_,i)=>({id:String(i),x:i}))};
  const models=api.sceneInstancedGLBMeshToModels(batch,0);
  assert.equal(models.length,180);assert.equal(normalizations,1);
  assert.equal(new Set(models.map(m=>m.id)).size,180);
  batch.color="#abcdef";api.sceneInstancedGLBMeshToModels(batch,0);
  assert.equal(normalizations,2,"fresh commands re-evaluate shared appearance");
});

test("rigid declarations omit pose metadata while explicit empty clips retain bind semantics", () => {
  const api = runtime();
  const rigid = api.normalizeSceneInstancedGLBInstance({id:"same",x:1},0);
  assert.equal(Object.hasOwn(rigid,"animation"),false);
  const rigidModel = api.sceneInstancedGLBMeshToModels({id:"actors",src:"/a.glb",instances:[rigid]},0)[0];
  assert.equal(Object.hasOwn(rigidModel,"_crowdPose"),false);
  const idle = api.normalizeSceneInstancedGLBInstance({id:"same",animation:"Idle",animationTime:.75,animationLoop:true},0);
  const bind = api.normalizeSceneInstancedGLBInstance({id:"same",animation:""},0);
  assert.equal(Object.hasOwn(bind,"animation"),true);
  const model = api.sceneInstancedGLBMeshToModels({id:"actors",src:"/a.glb",instances:[idle]},0)[0];
  const empty = api.sceneInstancedGLBMeshToModels({id:"actors",src:"/a.glb",instances:[bind]},0)[0];
  assert.equal(model._crowdPose.animation,"Idle");
  assert.equal(empty._crowdPose.animation,"");
  // Dynamic poses must never enter the immutable hydration declaration key.
  api.__poseModels=[rigidModel,model,empty];
  vm.runInContext("globalThis.__poseKeys=__poseModels.map(m=>sceneInstancedGLBHydrationTemplates.get(m))",api);
  assert.equal(new Set(api.__poseKeys).size,1);
});


test("default opaque instanced batch reaches crowd staging with its resolved material override", async () => {
  const api = runtime();
  vm.runInContext(fs.readFileSync(path.join(__dirname, '../runtime/scene3d/mount-webgl.ts'), 'utf8'), api);
  const batch = api.normalizeSceneInstancedGLBMeshEntry({id:'mite',src:'/mite.glb',blendMode:'opaque',instances:[{id:'10000',animation:'Idle',animationTime:.3,animationLoop:true}]}, 0, null);
  const model = api.sceneCloneHydrationModel(api.sceneInstancedGLBMeshToModels(batch,0)[0]);
  assert.equal(model.materialOverride.blendMode, 'opaque');
  assert.equal(model._crowdPose.animation, 'Idle');
  const vertices = {joints:new Float32Array(4),weights:new Float32Array([1,0,0,0])};
  const shared = {...vertices,immutable:true,revision:0};
  const atlas = {primitives:new Map([[vertices,{vertices:shared,bounds:{}}]])};
  const asset = {objects:[{skin:{},skinIndex:0,vertices,material:{kind:'standard'}}],skins:[{}],animations:[],points:[],labels:[],sprites:[],html:[],lights:[]};
  let prepares=0;
  api.loadSceneModelAsset=async()=>asset;
  api.sceneModelHydrationIsCurrent=()=>true;
  api.ensureAnimationFeatureLoaded=async()=>({buildCrowdAtlas:()=>atlas,crowdPoseRows:()=>new Float32Array([1,2,.3])});
  api.sceneInstantiateModelObject=()=>({});
  const state={_crowdWebGLRequested:true,_crowdRenderer:{prepareCrowdAtlas:()=>prepares++}};
  const result=await api.sceneStageModelHydration(state,model,0,1);
  assert.equal(result.ok,true);
  assert.equal(prepares,1);
  assert.equal(result.staged.objects[0]._crowdSkin.atlas,atlas);
  assert.equal(result.staged.objects[0].vertices,shared);
  assert.equal(result.staged.modelSkins.length,0);
  assert.equal(api.sceneCrowdPrimitiveMaterialEligible(asset.objects[0],{materialOverride:{opacity:.5}}),false);
  assert.equal(api.sceneCrowdPrimitiveMaterialEligible(asset.objects[0],{materialOverride:{materialKind:'custom'}}),false);
});
