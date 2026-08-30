"use strict";

const { readSceneRendererBackendSrc } = require("./scene3d-renderer-source-set.js");

const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const {
  bootstrapRuntimeSource,
  createContext,
  freshFeatureBundleSource,
  runScript,
} = require("./runtime-test-harness.js");

const BRDF_MODEL = "ggx-split-sum/smith-schlick-k=alpha-over-2/schlick-fresnel";

function readSource(name) {
  return fs.readFileSync(path.join(__dirname, "bootstrap-src", name), "utf8");
}

function readRuntimeSource(name) {
  return fs.readFileSync(path.join(__dirname, "..", "runtime", "scene3d", name), "utf8");
}

function iblDescriptor() {
  return {
    schemaVersion: 1,
    source: "/ibl/studio.hdr",
    radiance: {
      uri: "/ibl/studio-radiance.ktx2",
      role: "environment-radiance",
      colorSpace: "linear",
      channels: "rgba",
      view: "cube",
      format: "rgba16f",
      mipLevels: 7,
      width: 64,
      height: 64,
      faces: 6,
    },
    irradiance: {
      uri: "/ibl/studio-irradiance.ktx2",
      role: "environment-irradiance",
      colorSpace: "linear",
      channels: "rgba",
      view: "cube",
      format: "rgba16f",
      mipLevels: 1,
      width: 16,
      height: 16,
      faces: 6,
    },
    brdfLUT: {
      uri: "/ibl/brdf-lut.ktx2",
      role: "brdf-lut",
      colorSpace: "linear",
      channels: "rg",
      view: "2d",
      format: "rg16f",
      mipLevels: 1,
      width: 256,
      height: 256,
      faces: 1,
    },
    brdfModel: BRDF_MODEL,
    roughnessPerLevel: [0, 1 / 6, 2 / 6, 3 / 6, 4 / 6, 5 / 6, 1],
    sphericalHarmonics: [[1, 0, 0], [0, 1, 0]],
  };
}

test("fresh Scene3D state and render bundles preserve the complete HDR descriptor contract", () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  const api = env.context.__gosx_scene3d_api;
  const ibl = iblDescriptor();
  const descriptors = {
    baseColor: { uri: "/materials/hero-base.png", role: "base-color", colorSpace: "srgb", channels: "rgba", view: "2d", format: "rgba8" },
    normal: { uri: "/materials/hero-normal.png", role: "normal", colorSpace: "linear", channels: "rgb", view: "2d", format: "rgba8" },
    roughness: { uri: "/materials/hero-roughness.png", role: "roughness", colorSpace: "linear", channels: "g", view: "2d", format: "rgba8" },
    metalness: { uri: "/materials/hero-metalness.png", role: "metalness", colorSpace: "linear", channels: "b", view: "2d", format: "rgba8" },
    occlusion: { uri: "/materials/hero-ao.png", role: "occlusion", colorSpace: "linear", channels: "r", view: "2d", format: "rgba8" },
    emissive: { uri: "/materials/hero-emissive.png", role: "emissive", colorSpace: "srgb", channels: "rgb", view: "2d", format: "rgba8" },
  };
  const state = api.createSceneState({
    scene: {
      environment: { ibl, envIntensity: 1.5, envRotation: 0.25 },
      materials: [{
        name: "hero",
        kind: "standard",
        color: "#ffffff",
        texture: descriptors.baseColor.uri,
        normalMap: descriptors.normal.uri,
        roughnessMap: descriptors.roughness.uri,
        metalnessMap: descriptors.metalness.uri,
        occlusionMap: descriptors.occlusion.uri,
        emissiveMap: descriptors.emissive.uri,
        textureDescriptors: descriptors,
      }],
      objects: [{ id: "hero-box", kind: "box", material: "hero" }],
    },
  });

  assert.equal(state.environment.ibl.brdfModel, BRDF_MODEL);
  assert.equal(state.environment.ibl.radiance.view, "cube");
  assert.equal(state.environment.ibl.radiance.mipLevels, 7);
  assert.equal(state.environment.ibl.brdfLUT.channels, "rg");
  assert.deepEqual(Array.from(state.environment.ibl.roughnessPerLevel), ibl.roughnessPerLevel);

  const objects = api.sceneStateObjectsWithMaterials(state);
  assert.equal(objects.length, 1);
  assert.equal(objects[0].occlusionMap, descriptors.occlusion.uri);
  assert.equal(objects[0].textureDescriptors.baseColor.colorSpace, "srgb");
  assert.equal(objects[0].textureDescriptors.normal.colorSpace, "linear");
  assert.equal(objects[0].textureDescriptors.occlusion.channels, "r");

  const bundle = api.createSceneRenderBundle(
    320,
    180,
    "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    objects,
    [],
    [],
    [],
    [],
    state.environment,
    0,
    [],
    [],
    [],
    [],
    [],
    0,
    false,
  );
  assert.equal(bundle.environment.ibl.brdfModel, BRDF_MODEL);
  assert.equal(bundle.environment.ibl.irradiance.role, "environment-irradiance");
  assert.equal(bundle.materials.length, 1);
  assert.equal(bundle.materials[0].occlusionMap, descriptors.occlusion.uri);
  assert.equal(bundle.materials[0].textureDescriptors.emissive.colorSpace, "srgb");
});

test("scene state and render bundles keep both specular texture slots distinct over one URI", () => {
  const env = createContext({});
  runScript(bootstrapRuntimeSource, env.context, "bootstrap-runtime.js");
  runScript(freshFeatureBundleSource("scene3d"), env.context, "bootstrap-feature-scene3d.js");
  const api = env.context.__gosx_scene3d_api;

  // One shared URI feeds both specular slots: normalization must keep the
  // two roles separate instead of collapsing them.
  const sharedUri = "/materials/spec-shared.png";
  const descriptors = {
    specularIntensity: { uri: sharedUri, role: "specular-intensity", colorSpace: "linear", channels: "a", view: "2d", format: "rgba8" },
    specularColor: { uri: sharedUri, role: "specular-color", colorSpace: "srgb", channels: "rgb", view: "2d", format: "rgba8" },
  };
  const state = api.createSceneState({
    scene: {
      materials: [{
        name: "spec",
        kind: "standard",
        color: "#ffffff",
        specularIntensity: 0.5,
        specularColor: [0.8, 0.6, 0.4],
        textureDescriptors: descriptors,
      }],
      objects: [{ id: "spec-box", kind: "box", material: "spec" }],
    },
  });

  const objects = api.sceneStateObjectsWithMaterials(state);
  assert.equal(objects.length, 1);
  assert.equal(objects[0].specularIntensity, 0.5);
  assert.deepEqual(Array.from(objects[0].specularColor), [0.8, 0.6, 0.4]);
  // Normalization adds zero mip/size/face fields to every descriptor; compare
  // the FULL normalized shape via a JSON roundtrip so VM-realm prototypes
  // never skew deepStrictEqual across realms.
  const normalized = (value) => JSON.parse(JSON.stringify(value));
  assert.deepEqual(normalized(objects[0].textureDescriptors.specularIntensity), {
    uri: sharedUri, role: "specular-intensity", colorSpace: "linear", channels: "a",
    view: "2d", format: "rgba8", mipLevels: 0, width: 0, height: 0, faces: 0,
  });
  assert.deepEqual(normalized(objects[0].textureDescriptors.specularColor), {
    uri: sharedUri, role: "specular-color", colorSpace: "srgb", channels: "rgb",
    view: "2d", format: "rgba8", mipLevels: 0, width: 0, height: 0, faces: 0,
  });

  const bundle = api.createSceneRenderBundle(
    320, 180, "#000000",
    { x: 0, y: 0, z: 6, fov: 72, near: 0.05, far: 128 },
    objects, [], [], [], [],
    state.environment, 0, [], [], [], [], [], 0, false,
  );
  assert.equal(bundle.materials.length, 1);
  assert.equal(bundle.materials[0].textureDescriptors.specularIntensity.role, "specular-intensity");
  assert.equal(bundle.materials[0].textureDescriptors.specularColor.role, "specular-color");
  assert.equal(bundle.materials[0].textureDescriptors.specularColor.colorSpace, "srgb");
  assert.equal(bundle.materials[0].textureDescriptors.specularIntensity.colorSpace, "linear");
  assert.deepEqual(normalized(bundle.materials[0].textureDescriptors.specularIntensity), {
    uri: sharedUri, role: "specular-intensity", colorSpace: "linear", channels: "a",
    view: "2d", format: "rgba8", mipLevels: 0, width: 0, height: 0, faces: 0,
  });
  assert.deepEqual(normalized(bundle.materials[0].textureDescriptors.specularColor), {
    uri: sharedUri, role: "specular-color", colorSpace: "srgb", channels: "rgb",
    view: "2d", format: "rgba8", mipLevels: 0, width: 0, height: 0, faces: 0,
  });

  // The normalized state and bundle descriptors must be copies, never aliases
  // of the authored input. Mutate the input only AFTER the bundle exists so
  // the earlier assertions exercise the untouched normalized objects.
  descriptors.specularIntensity.uri = "/materials/mutated-intensity.png";
  descriptors.specularColor.role = "mutated-role";
  assert.equal(objects[0].textureDescriptors.specularIntensity.uri, sharedUri);
  assert.equal(objects[0].textureDescriptors.specularColor.uri, sharedUri);
  assert.equal(objects[0].textureDescriptors.specularIntensity.role, "specular-intensity");
  assert.equal(objects[0].textureDescriptors.specularColor.role, "specular-color");
  assert.equal(bundle.materials[0].textureDescriptors.specularIntensity.uri, sharedUri);
  assert.equal(bundle.materials[0].textureDescriptors.specularColor.uri, sharedUri);
  assert.equal(bundle.materials[0].textureDescriptors.specularIntensity.role, "specular-intensity");
  assert.equal(bundle.materials[0].textureDescriptors.specularColor.role, "specular-color");
});

test("WebGL HDR IBL compiles the bounded variant and consumes split-sum products in linear space", () => {
  const source = readSceneRendererBackendSrc("webgl");
  const vertexStart = source.indexOf("const SCENE_PBR_VERTEX_SOURCE");
  const fragmentStart = source.indexOf("const SCENE_PBR_FRAGMENT_SOURCE");
  const fragmentEnd = source.indexOf("const SCENE_PBR_INSTANCED_VERTEX_SOURCE");
  const vertex = source.slice(vertexStart, fragmentStart);
  const fragment = source.slice(fragmentStart, fragmentEnd);

  assert.doesNotMatch(vertex, /#define GOSX_HDR_IBL/);
  assert.match(fragment, /"#define GOSX_HDR_IBL 1"/);
  assert.match(source, /scenePBRHDRIBLAvailable\(gl\)[\s\S]*maxUnits >= 20/);
  assert.match(fragment, /"uniform sampler2D u_specularIntensityMap;",/);
  assert.match(fragment, /"uniform bool u_hasSpecularIntensityMap;",/);
  assert.match(source, /scenePBRFragmentSourceForContext\(gl, SCENE_PBR_FRAGMENT_SOURCE\)/);
  assert.match(fragment, /textureLod\(u_iblRadiance, Rr, roughness \* u_iblRadianceMaxLod\)/);
  assert.match(fragment, /texture\(u_iblBRDFLUT, vec2\(NoV, roughness\)\)\.rg/);
  assert.match(fragment, /prefiltered \* \(F0 \* brdf\.x \+ vec3\(F90\) \* brdf\.y\)/);
  assert.match(fragment, /irradiance \* albedo \* kDenv/);
  assert.match(fragment, /ambient \*= ambientOcclusion/);
  assert.match(source, /gl\.SRGB8_ALPHA8 \|\| 0x8C43/);
  assert.match(source, /descriptor\.colorSpace === "srgb"[\s\S]*gl\.RGBA8 \|\| 0x8058/);
  assert.match(source, /scenePBRLinearHDRPixels/);
  assert.match(source, /OES_texture_float_linear/);
  assert.match(source, /gl\.RGBA16F \|\| 0x881A[\s\S]*gl\.FLOAT, linear\.pixels/);
  assert.match(fragment, /u_outputLinear == 0/);
  assert.match(source, /missing-ext-color-buffer-float/);
  assert.match(source, /product-container-metadata/);
  assert.match(source, /scenePBRIBLRoughnessMappingValid/);
  assert.doesNotMatch(source, /scenePBRTonemapHDRPixels/);
});

test("WebGPU consumes the same split-sum contract and keeps color/data texture formats distinct", () => {
  const source = readSceneRendererBackendSrc("webgpu");

  assert.match(source, new RegExp(BRDF_MODEL.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")));
  assert.match(source, /textureSampleLevel\(iblRadiance, iblSampler, Rr, roughness \* maxLod\)/);
  assert.match(source, /textureSample\(iblBRDFLUT, iblSampler, vec2f\(NoV, roughness\)\)\.rg/);
  assert.match(source, /prefiltered \* \(F0 \* brdf\.x \+ vec3f\(F90\) \* brdf\.y\)/);
  assert.match(source, /irradiance \* albedo \* kDenv/);
  assert.match(source, /ambient = ambient \* ambientOcclusion/);
  assert.match(source, /descriptor\.colorSpace === "srgb" \? "rgba8unorm-srgb" : "rgba8unorm"/);
  assert.match(source, /@group\(0\) @binding\(9\) var iblIrradiance: texture_cube<f32>/);
  assert.match(source, /@group\(0\) @binding\(10\) var iblRadiance: texture_cube<f32>/);
  assert.match(source, /@group\(0\) @binding\(11\) var iblBRDFLUT: texture_2d<f32>/);
  assert.match(source, /@group\(1\) @binding\(11\) var occlusionTex: texture_2d<f32>/);
  assert.match(source, /out\.ibl = Object\.assign\(\{\}, iblResources\.diagnostics\)/);
  assert.match(source, /renderTruth\(\)\.record\("ibl-" \+ diag\.state, diag\.reason\)/);
  assert.match(source, /frame\.toneMap != 4u/);
  assert.match(source, /webGPUIBLRoughnessMappingValid/);
  assert.match(source, /product-container-metadata/);
});

test("mount preloads the real KTX2 reader for complete IBL descriptors and never flips support silently", () => {
  const mount = readRuntimeSource("mount.ts") + "\n" + readRuntimeSource("mount-webgl.ts");
  assert.match(mount, /await settleSceneIBLFeature\(props\)/);
  assert.match(mount, /scenePropsHasIBLProducts/);
  assert.match(mount, /ensureGLTFFeatureLoaded\(\)/);
  assert.match(mount, /ibl-loader-unavailable/);
});
