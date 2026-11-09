  // 16c2 — the Selena scene lighting block, shared by both Scene3D renderers.
  // It ships in the gated bootstrap-feature-scene3d-selena-lighting.js chunk
  // (wrapped by the 26n prefix/suffix), which a page advertises only when one
  // of its Selena materials reads lights, shadows or fog; the renderer chunks
  // load it on first use. The full monolith carries it inline. Every function
  // here is plain math or object work and touches no rendering context.

  // Both renderers fill one reusable record per frame with the engine inputs a
  // Selena mesh material reads for scene lighting: the camera, the key light
  // (sunDir/sunColor), ambient, fog, up to 8 packed lights and both shadow
  // slots with their cascades. The layout is Selena's
  // docs/engine-inputs.md#scene-lighting; a material receives a field only when
  // its context block declares that name. The record is allocated once, so a
  // frame writes in place and allocates nothing.
  var SCENE_SELENA_LIGHT_CAPACITY = 8;
  var SCENE_SELENA_CASCADE_CAPACITY = 4;
  var SCENE_SELENA_FAR_SPLIT = 3.0e38;
  var SCENE_SELENA_LIGHTING_NAMES = { cameraPos: 1, cameraForward: 1, sunDir: 1, sunColor: 1, ambient: 1, fog: 1, lightCount: 1,
    lights: 1, shadow0: 1, shadow1: 1, shadowSplits0: 1, shadowSplits1: 1, shadowMatrices0: 1, shadowMatrices1: 1 };

  function sceneSelenaLightingRecord() {
    return {
      cameraPos: new Float32Array(3), cameraForward: new Float32Array([0, 0, -1]),
      sunDir: new Float32Array([0, 1, 0]), sunColor: new Float32Array([1, 1, 1]),
      ambient: new Float32Array(3), fog: new Float32Array(4), lightCount: 0,
      lights: new Float32Array(SCENE_SELENA_LIGHT_CAPACITY * 16),
      shadow0: new Float32Array([0, 0, 0, -1]), shadow1: new Float32Array([0, 0, 0, -1]),
      shadowSplits0: new Float32Array(4), shadowSplits1: new Float32Array(4),
      shadowMatrices0: new Float32Array(SCENE_SELENA_CASCADE_CAPACITY * 16),
      shadowMatrices1: new Float32Array(SCENE_SELENA_CASCADE_CAPACITY * 16),
      // Renderer-side state, never uploaded: the source index of each packed
      // light and the shadow slot objects the texture binders read.
      sourceIndex: new Int32Array(SCENE_SELENA_LIGHT_CAPACITY),
      shadowMaps: [null, null],
    };
  }

  // Selena light kind codes: 0 ambient, 1 directional, 2 point, 3 spot,
  // 4 hemisphere. A light probe folds into ambient as in both renderers;
  // rect-area lights are not packed (-1).
  function sceneSelenaLightKind(kind) {
    switch (String(kind || "").toLowerCase()) {
    case "ambient": case "light-probe": return 0;
    case "directional": return 1;
    case "point": return 2;
    case "spot": return 3;
    case "hemisphere": return 4;
    default: return -1;
    }
  }

  function sceneSelenaPackLight(out, p, light, kind) {
    var base = p * 16;
    var intensity = sceneNumber(light.intensity, 1);
    var color = sceneColorRGBA(light.color, [1, 1, 1, 1]);
    out[base] = sceneNumber(light.x, 0); out[base + 1] = sceneNumber(light.y, 0); out[base + 2] = sceneNumber(light.z, 0); out[base + 3] = kind;
    out[base + 4] = sceneNumber(light.directionX, 0); out[base + 5] = sceneNumber(light.directionY, -1); out[base + 6] = sceneNumber(light.directionZ, 0);
    out[base + 7] = Math.max(0, sceneNumber(light.range, 0));
    out[base + 8] = color[0] * intensity; out[base + 9] = color[1] * intensity; out[base + 10] = color[2] * intensity;
    out[base + 11] = sceneNumber(light.decay, 2);
    var angle = sceneNumber(light.angle, 0);
    var ground = kind === 4 ? sceneColorRGBA(light.groundColor, [0, 0, 0, 1]) : null;
    out[base + 12] = ground ? ground[0] * intensity : Math.cos(angle);
    out[base + 13] = ground ? ground[1] * intensity : Math.cos(angle * (1 - Math.max(0, Math.min(1, sceneNumber(light.penumbra, 0)))));
    out[base + 14] = ground ? ground[2] * intensity : 0;
    out[base + 15] = -1;
  }

  // sceneSelenaClipToShadowUV writes one cascade matrix premultiplied by the
  // renderer's clip-to-texture mapping, so a Selena shader gets uv in xy and
  // stored-depth-comparable z after dividing by w on every backend. "gl" maps
  // clip xyz from -1..1 to 0..1; "gpu" maps x the same, flips y, and keeps z,
  // which WebGPU already stores as 0..1.
  function sceneSelenaClipToShadowUV(out, offset, m, clip) {
    var gpu = clip === "gpu";
    for (var col = 0; col < 4; col++) {
      var b = col * 4, w = m[b + 3];
      out[offset + b] = 0.5 * m[b] + 0.5 * w;
      out[offset + b + 1] = (gpu ? -0.5 : 0.5) * m[b + 1] + 0.5 * w;
      out[offset + b + 2] = gpu ? m[b + 2] : 0.5 * m[b + 2] + 0.5 * w;
      out[offset + b + 3] = w;
    }
  }

  // sceneSelenaCascadeBias makes the one bias Selena shaders subtract
  // (shadow0.y) act as bias * cascade.biasScale, the per-cascade bias both
  // renderers apply: it adds bias * (1 - biasScale) to the cascade's depth.
  function sceneSelenaCascadeBias(out, offset, light, cascade) {
    var scale = cascade ? sceneNumber(cascade.biasScale, 1) : 1;
    if (scale === 1) return;
    var delta = sceneNumber(light && light.shadowBias, 0.005) * (1 - scale);
    for (var col = 0; col < 4; col++) out[offset + col * 4 + 2] += delta * out[offset + col * 4 + 3];
  }

  // A slot carries numCascades and cascades[c].{lightMatrix, splitFar,
  // biasScale} on both backends (the WebGPU cascaded shadow module documents
  // its layout); a slot without cascades is one map whose matrix the caller
  // passes in matrices[s].
  function sceneSelenaFillShadowSlot(out, s, slot, matrix, light, packedIndex, clip) {
    var info = s === 0 ? out.shadow0 : out.shadow1;
    var splits = s === 0 ? out.shadowSplits0 : out.shadowSplits1;
    var matrices = s === 0 ? out.shadowMatrices0 : out.shadowMatrices1;
    var cascades = slot && slot.cascades ? Math.min(SCENE_SELENA_CASCADE_CAPACITY, slot.numCascades | 0) : (slot && matrix ? 1 : 0);
    if (packedIndex < 0) cascades = 0;
    matrices.fill(0);
    splits.fill(SCENE_SELENA_FAR_SPLIT);
    for (var c = 0; c < cascades; c++) {
      var cascade = slot.cascades ? slot.cascades[c] : null;
      var m = cascade ? cascade.lightMatrix : matrix;
      if (!m) { cascades = c; break; }
      sceneSelenaClipToShadowUV(matrices, c * 16, m, clip);
      sceneSelenaCascadeBias(matrices, c * 16, light, cascade);
      if (cascade && c < cascades - 1) splits[c] = sceneNumber(cascade.splitFar, SCENE_SELENA_FAR_SPLIT);
    }
    var size = Math.max(1, sceneNumber(slot && slot.size, 1024));
    info[0] = cascades;
    info[1] = sceneNumber(light && light.shadowBias, 0.005);
    info[2] = (1 + 8 * Math.max(0, Math.min(1, sceneNumber(light && light.shadowSoftness, 0)))) / size;
    info[3] = cascades > 0 ? packedIndex : -1;
    if (cascades > 0) out.lights[packedIndex * 16 + 15] = s;
    out.shadowMaps[s] = cascades > 0 ? slot : null;
  }

  // sceneSelenaFillLighting fills (and returns) the lighting record for one
  // frame. frame: { cam, view (column-major view matrix), lights, environment,
  // slots: [slot0, slot1], lightIndices: [i0, i1] into lights, matrices:
  // [m0, m1] for single-map slots, clip: "gl" | "gpu" }.
  function sceneSelenaFillLighting(out, frame) {
    out = out || sceneSelenaLightingRecord();
    var cam = frame.cam || {}, view = frame.view, env = frame.environment || {};
    out.cameraPos[0] = sceneNumber(cam.x, 0); out.cameraPos[1] = sceneNumber(cam.y, 0); out.cameraPos[2] = sceneNumber(cam.z, 0);
    if (view && view.length >= 16) {
      out.cameraForward[0] = -view[2]; out.cameraForward[1] = -view[6]; out.cameraForward[2] = -view[10];
    }
    var ambient = sceneColorRGBA(env.ambientColor, [1, 1, 1, 1]), ambientIntensity = sceneNumber(env.ambientIntensity, 0);
    out.ambient[0] = ambient[0] * ambientIntensity; out.ambient[1] = ambient[1] * ambientIntensity; out.ambient[2] = ambient[2] * ambientIntensity;
    var fog = sceneColorRGBA(env.fogColor, [0.5, 0.5, 0.5, 1]);
    out.fog[0] = fog[0]; out.fog[1] = fog[1]; out.fog[2] = fog[2]; out.fog[3] = Math.max(0, sceneNumber(env.fogDensity, 0));
    out.sunDir[0] = 0; out.sunDir[1] = 1; out.sunDir[2] = 0;
    out.sunColor[0] = 1; out.sunColor[1] = 1; out.sunColor[2] = 1;
    var lights = Array.isArray(frame.lights) ? frame.lights : [], sun = false, p = 0;
    out.lights.fill(0);
    for (var i = 0; i < lights.length && p < SCENE_SELENA_LIGHT_CAPACITY; i++) {
      var light = lights[i] || {}, kind = sceneSelenaLightKind(light.kind);
      if (kind < 0) continue;
      sceneSelenaPackLight(out.lights, p, light, kind);
      out.sourceIndex[p] = i;
      if (kind === 1 && !sun) {
        sun = true;
        var len = Math.hypot(out.lights[p * 16 + 4], out.lights[p * 16 + 5], out.lights[p * 16 + 6]) || 1;
        for (var k = 0; k < 3; k++) {
          out.sunDir[k] = -out.lights[p * 16 + 4 + k] / len;
          out.sunColor[k] = out.lights[p * 16 + 8 + k];
        }
      }
      p++;
    }
    out.lightCount = p;
    for (var s = 0; s < 2; s++) {
      var source = frame.lightIndices ? frame.lightIndices[s] : -1, packed = -1;
      for (var q = 0; q < p; q++) if (out.sourceIndex[q] === source) packed = q;
      sceneSelenaFillShadowSlot(out, s, frame.slots && frame.slots[s], frame.matrices && frame.matrices[s], lights[source], packed, frame.clip);
    }
    return out;
  }

  // sceneSelenaLightingValue returns the live value for a Selena context field
  // the lighting record supplies, or undefined for any other field.
  function sceneSelenaLightingValue(lighting, field) {
    var name = field && field.name;
    if (!lighting || !field || field.class !== "context" || !Object.prototype.hasOwnProperty.call(SCENE_SELENA_LIGHTING_NAMES, name)) return undefined;
    return lighting[name];
  }

  // sceneSelenaShadowMapSlot returns the shadow slot object for a Selena
  // shadowMap0/shadowMap1 engine texture (null while the slot is empty), or
  // undefined when the texture is not a shadow map.
  function sceneSelenaShadowMapSlot(lighting, texture) {
    var engine = texture && texture.engine;
    if (engine !== "shadowMap0" && engine !== "shadowMap1") return undefined;
    return (lighting && lighting.shadowMaps[engine === "shadowMap0" ? 0 : 1]) || null;
  }

  // sceneSelenaFrameLighting refreshes holder._gosxSelenaLighting for one frame
  // and returns it. It reuses holder._gosxSelenaLightingInput, so a frame
  // allocates no lighting objects. bundle supplies lights and environment.
  function sceneSelenaFrameLighting(holder, cam, view, bundle, slots, lightIndices, matrices, clip) {
    var input = holder._gosxSelenaLightingInput || (holder._gosxSelenaLightingInput = {});
    input.cam = cam; input.view = view; input.lights = bundle && bundle.lights; input.environment = bundle && bundle.environment;
    input.slots = slots; input.lightIndices = lightIndices; input.matrices = matrices; input.clip = clip;
    holder._gosxSelenaLighting = sceneSelenaFillLighting(holder._gosxSelenaLighting, input);
    return holder._gosxSelenaLighting;
  }

  // The full monolith inlines this file, but its WebGPU renderer still arrives
  // as the gated WebGPU chunk, whose shims read this API object. Publish it
  // here too (the gated lighting chunk's suffix sets the same functions), so a
  // monolith page lights Selena materials on WebGPU without advertising the
  // lighting chunk.
  if (typeof window !== "undefined" && window && !window.__gosx_scene3d_selena_lighting_api) {
    window.__gosx_scene3d_selena_lighting_api = { frame: sceneSelenaFrameLighting, value: sceneSelenaLightingValue, shadowSlot: sceneSelenaShadowMapSlot };
  }
