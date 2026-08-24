# Scene3D light cache invalidation parity

Return one **bare, complete unified diff** and nothing else. Do not use Markdown
fences or commentary. The diff must apply to this exact clean HEAD:

`34f511ac2d997cd9ed4c7791216247f46452594f`

## Allowed scope

- Modify only `client/js/bootstrap-src/15b-scene-planner.ts` and
  `client/js/runtime-05-scene-camera-planner.test.js`.
- Do not add files.
- Do not modify generated runtime bundles, Go source, SceneIR lowering, geometry,
  picking, HTML textures, lazy loading, rendering backends, or unrelated code.
- Keep the change TypeScript-first, small, and consistent with the existing hash
  helpers and Node test style. Do not refactor adjacent code.

## Defect

The backend-neutral prepared-scene signature includes `bundle.lights` through
`scenePlannerHashLight`, but that hash omits authored light fields that the
typed Go SceneIR, shared browser normalization, and shared PBR light upload hash
all preserve and consume. In particular, it omits `groundColor`, `angle`,
`penumbra`, `range`, `decay`, `width`, `height`, `shadowBias`,
`shadowCascades`, and `shadowSoftness`.

A baseline probe created a prepared scene with a shadow-casting spot light at
`range: 6`, mutated only that light to `range: 12`, and called `prepareScene`
with the previous prepared value. Current main returned the exact same prepared
object with the exact same signature and `rebuilds: 1`. The live IR contained
the new range, but the backend-neutral cache classified a visible attenuation
change as unchanged and retained derived planner hashes/state.

Observed baseline result:

```json
{"same":true,"signatureSame":true,"rebuilds":1,"range":12,"shadowHash":true}
```

This differs from the shared GPU light-content hash, which already treats these
fields as render-affecting. It creates inconsistent invalidation semantics
between typed SceneIR/live patches and the shared WebGL/WebGPU preparation path.

## Required behavior

1. Extend `scenePlannerHashLight` so every render-affecting scalar/string field
   already covered by shared `hashLightContent` also contributes to the prepared
   scene signature. Preserve the existing `id`, `castShadow`, and `shadowSize`
   contributions. Do not remove fields or change hash primitives/defaults beyond
   what is necessary for the missing fields.
2. Add one focused regression in
   `client/js/runtime-05-scene-camera-planner.test.js` through the public
   `api.prepareScene` path. It must prove that changing a spot light's omitted
   attenuation/cone fields invalidates the previous prepared value, while an
   unchanged subsequent call still reuses the new prepared value.
3. The test must fail on the exact baseline for the demonstrated defect and
   pass after the hash fix. Keep the fixture minimal and deterministic.
4. Preserve all public APIs, zero/default behavior, prepared-scene reuse for
   unchanged input, and renderer bundle outputs.

## Exact relevant source

The cache gate in `client/js/bootstrap-src/15b-scene-planner.ts`:

```ts
  function prepareScene(ir, camera, viewport, lastPrepared, cssContext) {
    const plannerStartedAt = scenePlannerNow();
    const fullVertexHashScansBefore = scenePlannerTelemetryState.fullVertexHashScans;
    scenePlannerTelemetryState.plans += 1;
    const initialSource = ir && typeof ir === "object" ? ir : {};
    const css = sceneCSSResolverContext(cssContext);
    css.nowMs = typeof cssContext === "object" && cssContext && cssContext.nowMs ? cssContext.nowMs : Date.now();
    const cssInputSignature = sceneCSSInputSignature(initialSource);
    const cssCache = lastPrepared && lastPrepared.cssCache;
    const hasActiveVarTransitions = cssCache && Array.isArray(cssCache.varTransitions) && cssCache.varTransitions.length > 0;
    const cssResolved = cssCache
      && cssCache.inputSignature === cssInputSignature
      && cssCache.revision === css.revision
      && cssCache.transitionFrame === css.transitionFrame
      && !hasActiveVarTransitions
        ? sceneCSSApplyCachedResolution(initialSource, cssCache)
        : sceneResolveCSSBundleWithContext(initialSource, css, cssInputSignature);
    const source = cssResolved.ir;
    const resolvedCamera = camera || source.camera || {};
    const signature = scenePreparedSignature(source, resolvedCamera, viewport);
    if (lastPrepared && lastPrepared.signature === signature) {
      scenePlannerTelemetryState.cacheHits += 1;
      lastPrepared.ir = source;
      lastPrepared.camera = resolvedCamera;
      lastPrepared.viewport = viewport;
      lastPrepared.resolvedEnv = source.environment || {};
      lastPrepared.cssDynamic = Boolean(cssResolved.dynamic);
      lastPrepared.cssCache = cssResolved.cache;
      const elapsed = Math.max(0, scenePlannerNow() - plannerStartedAt);
      scenePlannerTelemetryState.lastPlannerCPUms = elapsed;
      scenePlannerTelemetryState.plannerCPUms += elapsed;
      lastPrepared.telemetry = scenePlannerTelemetrySnapshot(fullVertexHashScansBefore);
      source.plannerTelemetry = lastPrepared.telemetry;
      return lastPrepared;
    }
```

The prepared signature and current incomplete light hash:

```ts
  function scenePreparedSignature(bundle, camera, viewport) {
    let hash = 2166136261 >>> 0;
    hash = scenePlannerHashString(hash, "v");
    hash = scenePlannerHashNumber(hash, sceneNumber(bundle && (bundle.bundleVersion != null ? bundle.bundleVersion : bundle.version), 0));
    hash = scenePlannerHashCamera(hash, camera);
    hash = scenePlannerHashViewport(hash, viewport);
    hash = scenePlannerHashCollection(hash, bundle && bundle.meshObjects, scenePlannerHashMeshObject);
    hash = scenePlannerHashCollection(hash, bundle && bundle.objects, scenePlannerHashLineObject);
    hash = scenePlannerHashCollection(hash, bundle && bundle.materials, scenePlannerHashMaterial);
    hash = scenePlannerHashCollection(hash, bundle && bundle.lights, scenePlannerHashLight);
    hash = scenePlannerHashCollection(hash, bundle && bundle.points, scenePlannerHashPointsEntry);
    hash = scenePlannerHashCollection(hash, bundle && bundle.instancedMeshes, scenePlannerHashInstancedEntry);
    hash = scenePlannerHashCollection(hash, bundle && bundle.computeParticles, scenePlannerHashComputeEntry);
    hash = scenePlannerHashCollection(hash, bundle && bundle.waterSystems, scenePlannerHashWaterSystemEntry);
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldPositions));
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldColors));
    hash = scenePlannerHashArrayShape(hash, bundle && bundle.worldLineWidths);
    hash = scenePlannerHashArrayShape(hash, bundle && bundle.worldLinePasses);
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldMeshPositions));
    hash = scenePlannerHashNumber(hash, arrayLength(bundle && bundle.worldMeshNormals));
    return String(hash);
  }

  function scenePlannerHashLight(hash, light) {
    hash = scenePlannerHashString(hash, light && light.id || "");
    hash = scenePlannerHashString(hash, light && light.kind || "");
    hash = scenePlannerHashString(hash, light && light.color || "");
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.intensity, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.x, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.y, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.z, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.directionX, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.directionY, 0));
    hash = scenePlannerHashNumber(hash, sceneNumber(light && light.directionZ, 0));
    hash = scenePlannerHashNumber(hash, light && light.castShadow ? 1 : 0);
    return scenePlannerHashNumber(hash, sceneNumber(light && light.shadowSize, 0));
  }
```

The authoritative shared render-content coverage in
`client/js/bootstrap-src/16c-scene-shared-pbr.ts` is read-only context; do not
modify that file:

```ts
  function hashLightContent(l) {
    if (!l) return 0;
    var h = 2166136261;
    h = scenePBRLightsHashString(h, l.kind);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.x, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.y, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.z, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionX, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionY, -1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.directionZ, 0));
    h = scenePBRLightsHashString(h, l.color);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.intensity, 1));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.range, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.decay, 2));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.angle, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.penumbra, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.width, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.height, 0));
    h = scenePBRLightsHashString(h, l.groundColor);
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowBias, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSize, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowCascades, 0));
    h = scenePBRLightsHashNumber(h, sceneNumber(l.shadowSoftness, 0));
    return h;
  }
```

The existing planner test in
`client/js/runtime-05-scene-camera-planner.test.js` already establishes the
fixture and cache-reuse style:

```js
test("bootstrap resolves Scene3D CSS custom properties in the planner", async () => {
  // setup omitted
  const viewport = { cssWidth: 320, cssHeight: 180, pixelWidth: 320, pixelHeight: 180, pixelRatio: 1 };
  const prepared = api.prepareScene(bundle, bundle.camera, viewport, null, { mount, sentinels, revision: 1 });
  const cached = api.prepareScene(bundle, bundle.camera, viewport, prepared, { mount, sentinels, revision: 1 });
  assert.equal(cached, prepared);
  // remaining assertions omitted
});
```

## Validation target

The resulting diff should pass:

```sh
node --test client/js/runtime-05-scene-camera-planner.test.js
node --test client/js/runtime-05-scene-camera-planner.test.js --test-name-pattern='light.*cache|cache.*light'
```
