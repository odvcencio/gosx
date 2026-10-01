"use strict";

const test = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const path = require("node:path");
const {
  createBoardWebGPUHarness, createWebGLRendererForPost,
  createContext, runScript, bootstrapSource, flushAsyncWork,
} = require("./runtime-test-harness.js");

const props = JSON.parse(execFileSync("go", ["run", "./client/js/testdata/transmission-go-writer-fixture"], {
  cwd: path.resolve(__dirname, "../.."), env: { ...process.env, GOWORK: "off" }, encoding: "utf8",
}));

function assertVolume(material) {
  assert.equal(material.transmission, 1);
  assert.equal(material.thickness, 2.5);
  assert.equal(material.attenuationDistance, 4);
  assert.deepEqual(Array.from(material.attenuationColor), [0, 0.5, 1]);
}

function bundleFromState(api, state) {
  return api.createSceneRenderBundle(64, 64, "#000000", { x: 0, y: 0, z: 5, fov: 60, near: 0.1, far: 100 },
    api.sceneStateObjectsWithMaterials(state), [], [], [], [], {}, 0, [], [],
    api.sceneStateInstancedMeshesWithMaterials(state), [], [], 0, false);
}

for (const backend of ["WebGPU", "WebGL"]) {
  test(`${backend} receives Go volume controls through scene state and named materials`, async () => {
    const h = backend === "WebGPU" ? await createBoardWebGPUHarness({ fresh: true }) : createWebGLRendererForPost({ fresh: true });
    try {
      const api = h.env.context.__gosx_scene3d_api;
      for (const named of [false, true]) {
        const wire = JSON.parse(JSON.stringify(props));
        if (named) {
          wire.scene.materials = [{ ...wire.scene.objects[0], name: "volume", kind: "standard", variants: { constrained: { roughness: 0.7 } } }];
          wire.scene.objects[0] = { id: "glass", kind: "box", material: "volume" };
          wire.scene.instancedMeshes[0] = { id: "glass-instances", kind: "box", count: 1, positions: [2, 0, 0], material: "volume" };
        }
        const state = api.createSceneState(wire, { tier: "constrained" });
        assertVolume(api.sceneStateObjectsWithMaterials(state)[0]);
        assertVolume(api.sceneStateInstancedMeshesWithMaterials(state)[0]);
        assertVolume(state.models[0].materialOverride);
        const bundle = bundleFromState(api, state);
        bundle.materials.forEach(assertVolume);
        h.canvas.width = h.canvas.height = 64;
        h.renderer.render(bundle, { width: 64, height: 64 });
        if (backend === "WebGPU") {
          const uploads = h.fake.state.writeBufferCalls.filter(call => ArrayBuffer.isView(call.data) && call.data.length === 72);
          assert.ok(uploads.some(call => Array.from(call.data.slice(64, 71)).every((v, i) => v === [2.5, 1.5, 0.25, 0, 0, 0.5, 1][i])), "material uniforms retain volume and exact black absorption");
        } else {
          const gl = h.canvas.getContext("webgl2");
          assert.ok(gl.ops.some(op => op[0] === "uniform4fv" && op[1] === "u_volume" && op[2][0] === 2.5 && op[2][2] === 0.25));
          assert.ok(gl.ops.some(op => op[0] === "uniform3fv" && op[1] === "u_attenuationColor" && op[2].join() === "0,0.5,1"));
        }
        // Unrelated live patches retain volume fields and own their RGB arrays.
        api.applySceneCommands(state, [
          { kind: 3, objectId: "glass", data: { roughness: 0.7 } },
          { kind: 8, data: { instancedMeshes: [{ id: "glass-instances", roughness: 0.7 }] } },
          ...(named ? [{ kind: 9, data: { materials: [{ ...state._materialSource[0], roughness: 0.7 }] } }] : []),
        ]);
        assertVolume(api.sceneStateObjectsWithMaterials(state)[0]);
        assertVolume(api.sceneStateInstancedMeshesWithMaterials(state)[0]);
        if (named) {
          const resolved = api.sceneStateObjectsWithMaterials(state)[0];
          resolved.attenuationColor[1] = 0.2;
          assertVolume(state.materials[0]);
        }
        assert.deepEqual(wire.scene.objects[0].attenuationColor, named ? undefined : [0, 0.5, 1]);
      }
    } finally { h.renderer.dispose(); }
  });
}

test("model hydration preserves Go volume overrides without inventing overrides for absent controls", async () => {
  const env = createContext({ fetchRoutes: { "/glass.gosx3d.json": { text: JSON.stringify({ objects: [
    { id: "imported", kind: "box", material: { kind: "standard", thickness: 9, attenuationDistance: 10, attenuationColor: [1, 1, 1] } },
  ] }) } } });
  runScript(bootstrapSource, env.context, "bootstrap.js");
  await flushAsyncWork();
  const api = env.context.__gosx_scene3d_api;
  const state = api.createSceneState(props);
  await api.applySceneCommands(state, [{ kind: 10, data: { models: props.scene.models } }]);
  const imported = api.sceneStateObjectsWithMaterials(state).find(object => object.id.endsWith("/imported"));
  assert.ok(imported);
  assertVolume(imported);
  assertVolume(bundleFromState(api, state).materials.find(material => material.thickness === 2.5));
  const absent = api.createSceneState({ scene: { models: [{ id: "plain", src: "/plain.glb" }] } });
  assert.equal(absent.models[0].materialOverride, null);
  await api.applySceneCommands(state, [{ kind: 10, data: { models: [{ ...props.scene.models[0], thickness: 0, attenuationDistance: 0, attenuationColor: [0, 0, 0] }] } }]);
  const thin = api.sceneStateObjectsWithMaterials(state).find(object => object.id.endsWith("/imported"));
  assert.equal(thin.thickness, 0);
  assert.equal(thin.attenuationDistance, 0);
  assert.deepEqual(Array.from(thin.attenuationColor), [0, 0, 0]);
});

for (const backend of ["WebGPU", "WebGL"]) {
  test(`${backend} transmission equation tints light with resolved base color and texture`, async () => {
    const h = backend === "WebGPU" ? await createBoardWebGPUHarness({ fresh: true }) : createWebGLRendererForPost({ fresh: true });
    try {
      const api = h.env.context.__gosx_scene3d_api;
      h.renderer.render(bundleFromState(api, api.createSceneState(props)), { width: 64, height: 64 });
      const sources = backend === "WebGPU" ? h.fake.state.shaderModules.map(module => module.code)
        : h.canvas.getContext("webgl2")._activeProgram.attached.map(shader => shader.source);
      const shader = sources.find(source => /color (?:\+=|= color \+) transmission.*volumeTransmission/.test(source));
      assert.ok(shader, "assembled PBR shader includes transmission");
      const equation = shader.match(/color (?:\+=|= color \+) (transmission[^;]*volumeTransmission\([^;]+\));/)[1];
      const expression = equation.replace(/vec3f?\(1\.0\)/g, "1").replace(/volumeTransmission\([^)]*\)/, "light");
      const evaluate = new Function("transmission", "albedo", "Ft", "light", `return ${expression};`);
      assert.ok(shader.indexOf("texAlbedo.rgb") < shader.indexOf(equation), "texture modulates albedo before transmission");
      for (const texture of [[1, 1, 1], [0.25, 0.5, 0.75], [0, 0, 0]]) {
        const base = [0.2, 0.4, 0.8], light = [0.8, 0.6, 0.4], fresnel = [0.04, 0.08, 0.12];
        for (const transmission of [0, 0.5, 1]) {
          for (let i = 0; i < 3; i++) {
            const actual = evaluate(transmission, base[i] * texture[i], fresnel[i], light[i]);
            const expected = transmission * (1 - fresnel[i]) * light[i] * base[i] * texture[i];
            assert.ok(Math.abs(actual - expected) < 1e-12, "full and partial transmission retain color and texture tint");
          }
        }
      }
    } finally { h.renderer.dispose(); }
  });
}
