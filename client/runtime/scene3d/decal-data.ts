// Projected decal set: turns a "decals" post effect plus any published pool
// streams into sorted, culled per-instance data for the WebGPU and WebGL2
// passes. It allocates only when the set of decals changes size.
(() => {
  const fx = (window as any).__gosx_scene3d_depthfx_api;
  if (!fx) return;

  const REC = 20;   // float32s per packed record (scene.DecalRecordStride)
  const INST = 40;  // float32s per instance: model 16, inverse 16, uv rect 4, tint 4
  const MAX_DECALS = 8192;
  const MAT_FLOATS = 16;

  const lin = (c: number): number => (c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4));

  interface DecalMaterialState {
    name: string; albedo: string; normal: string; roughness: string; emissive: string;
    columns: number; rows: number; multiply: boolean;
    uniforms: Float32Array; // 16 floats, see matUniform
  }

  // Per-material uniform layout, shared by both shaders:
  //   0..3  cosAngleStart, cosAngleEnd, edgeFade, normalStrength
  //   4..7  roughness, emissiveIntensity, hasNormalMap, hasRoughnessMap
  //   8..11 emissive rgb (linear), hasEmissiveMap
  //   12..15 color rgb (linear), blend (0 lit, 1 multiply)
  function normalizeMaterial(raw: any): DecalMaterialState {
    const u = new Float32Array(MAT_FLOATS);
    const num = (v: any, d: number): number => (typeof v === "number" && isFinite(v) ? v : d);
    const rad = Math.PI / 180;
    u[0] = Math.cos(num(raw.angleFadeStart, 65) * rad);
    u[1] = Math.cos(num(raw.angleFadeEnd, 88) * rad);
    u[2] = Math.max(0.001, Math.min(0.5, num(raw.edgeFade, 0.1)));
    u[3] = num(raw.normalStrength, 1);
    u[4] = Math.max(0.02, Math.min(1, num(raw.roughness, 0.8)));
    u[5] = Math.max(0, num(raw.emissiveIntensity, 0));
    u[6] = raw.normalMap ? 1 : 0;
    u[7] = raw.roughnessMap ? 1 : 0;
    fx.parseColor(raw.emissiveColor, u, 8, 1);
    u[11] = raw.emissiveMap ? 1 : 0;
    fx.parseColor(raw.color, u, 12, 1);
    const multiply = String(raw.blend || "").toLowerCase() === "multiply";
    u[15] = multiply ? 1 : 0;
    return {
      name: String(raw.name || ""), albedo: raw.albedo || "", normal: raw.normalMap || "", roughness: raw.roughnessMap || "", emissive: raw.emissiveMap || "",
      columns: Math.max(1, Math.floor(num(raw.columns, 1))), rows: Math.max(1, Math.floor(num(raw.rows, 1))), multiply, uniforms: u,
    };
  }

  function createDecalSet() {
    let effect: any = null;
    let materials: DecalMaterialState[] = [];
    let staticRecords: Float32Array = new Float32Array(0);
    let poolNames: string[] = [];
    let maxDistance = 120;

    let source = new Float32Array(0);     // static + pool records, source order
    let order = new Uint32Array(0);
    let layers = new Float32Array(0);
    let inst = new Float32Array(0);       // sorted instances
    let instMat = new Int32Array(0);
    let instCenter = new Float32Array(0); // x, y, z, radius per sorted instance
    let total = 0;
    let setVersion = 0;
    const poolSig = new Float64Array(16); // [count, revision] per pool name, up to 8 pools
    let lastPrepared = -1;
    const lastVP = new Float32Array(16);

    let vis = new Float32Array(0);        // culled instances
    let runMat = new Int32Array(0), runFirst = new Int32Array(0), runCount = new Int32Array(0);
    let runs = 0, visible = 0;
    const planes = new Float32Array(24);
    const byLayer = (a: number, b: number): number => layers[a] - layers[b] || a - b;

    function poolStore(name: string): any {
      const pools = (window as any).__gosx_scene3d_decal_pools;
      return pools && pools[name] || null;
    }

    function ingest(next: any): void {
      effect = next;
      materials = (Array.isArray(next.materials) ? next.materials : []).map(normalizeMaterial);
      staticRecords = Array.isArray(next.records) ? Float32Array.from(next.records) : new Float32Array(0);
      poolNames = Array.isArray(next.pools) ? next.pools.filter((n: any) => typeof n === "string").slice(0, 8) : [];
      maxDistance = typeof next.maxDistance === "number" && next.maxDistance > 0 ? next.maxDistance : 120;
      poolSig.fill(-1);
      setVersion++;
    }

    function poolsChanged(): boolean {
      let changed = false;
      for (let i = 0; i < poolNames.length; i++) {
        const store = poolStore(poolNames[i]);
        const count = store ? store.count | 0 : 0, rev = store ? +store.revision || 0 : 0;
        if (poolSig[i * 2] !== count || poolSig[i * 2 + 1] !== rev) { poolSig[i * 2] = count; poolSig[i * 2 + 1] = rev; changed = true; }
      }
      return changed;
    }

    function grow(n: number): void {
      if (source.length >= n * REC) return;
      const cap = Math.min(MAX_DECALS, Math.max(n, 64, (source.length / REC) * 2));
      source = new Float32Array(cap * REC); order = new Uint32Array(cap); layers = new Float32Array(cap);
      inst = new Float32Array(cap * INST); instMat = new Int32Array(cap); instCenter = new Float32Array(cap * 4);
      vis = new Float32Array(cap * INST);
      runMat = new Int32Array(cap); runFirst = new Int32Array(cap); runCount = new Int32Array(cap);
    }

    // Rebuild the sorted instance list. Records are packed oldest first within
    // each source, static decals first, and the layer sort is stable, so a newer
    // decal lands on top of an older one in the same layer.
    function rebuild(): void {
      let n = staticRecords.length / REC | 0;
      const stores: any[] = [];
      for (let i = 0; i < poolNames.length; i++) {
        const store = poolStore(poolNames[i]);
        stores.push(store);
        if (store && store.data) n += Math.min(store.count | 0, (store.data.length / REC) | 0);
      }
      n = Math.min(n, MAX_DECALS);
      grow(n);
      let at = 0;
      source.set(staticRecords.subarray(0, Math.min(staticRecords.length, n * REC)), 0);
      at = Math.min(staticRecords.length / REC | 0, n);
      for (let i = 0; i < stores.length && at < n; i++) {
        const store = stores[i];
        if (!store || !store.data) continue;
        const take = Math.min(store.count | 0, (store.data.length / REC) | 0, n - at);
        if (take > 0) { source.set(store.data.subarray(0, take * REC), at * REC); at += take; }
      }
      total = at;
      for (let i = 0; i < total; i++) { order[i] = i; layers[i] = source[i * REC + 16]; }
      order.subarray(0, total).sort(byLayer);
      let kept = 0;
      for (let k = 0; k < total; k++) {
        const s = order[k] * REC;
        const mi = source[s + 18] | 0;
        const m = materials[mi];
        if (!m) continue;
        const o = kept * INST;
        writeInstance(source, s, m, inst, o);
        instMat[kept] = mi;
        const sx = source[s + 8], sy = source[s + 9], sz = source[s + 10];
        instCenter[kept * 4] = source[s]; instCenter[kept * 4 + 1] = source[s + 1]; instCenter[kept * 4 + 2] = source[s + 2];
        instCenter[kept * 4 + 3] = 0.5 * Math.sqrt(sx * sx + sy * sy + sz * sz);
        kept++;
      }
      total = kept;
      setVersion++;
    }

    function writeInstance(src: Float32Array, s: number, m: DecalMaterialState, out: Float32Array, o: number): void {
      const px = src[s], py = src[s + 1], pz = src[s + 2], opacity = src[s + 3];
      const qx = src[s + 4], qy = src[s + 5], qz = src[s + 6], qw = src[s + 7];
      const sx = Math.max(1e-4, src[s + 8]), sy = Math.max(1e-4, src[s + 9]), sz = Math.max(1e-4, src[s + 10]);
      const x2 = qx + qx, y2 = qy + qy, z2 = qz + qz;
      const xx = qx * x2, xy = qx * y2, xz = qx * z2, yy = qy * y2, yz = qy * z2, zz = qz * z2, wx = qw * x2, wy = qw * y2, wz = qw * z2;
      // Rotation columns (c0, c1, c2).
      const c0x = 1 - (yy + zz), c0y = xy + wz, c0z = xz - wy;
      const c1x = xy - wz, c1y = 1 - (xx + zz), c1z = yz + wx;
      const c2x = xz + wy, c2y = yz - wx, c2z = 1 - (xx + yy);
      // Model = T * R * S, column-major.
      out[o] = c0x * sx; out[o + 1] = c0y * sx; out[o + 2] = c0z * sx; out[o + 3] = 0;
      out[o + 4] = c1x * sy; out[o + 5] = c1y * sy; out[o + 6] = c1z * sy; out[o + 7] = 0;
      out[o + 8] = c2x * sz; out[o + 9] = c2y * sz; out[o + 10] = c2z * sz; out[o + 11] = 0;
      out[o + 12] = px; out[o + 13] = py; out[o + 14] = pz; out[o + 15] = 1;
      // Inverse = S^-1 * R^T * T^-1: local_i = dot(column_i, p - t) / size_i.
      const dx0 = c0x * px + c0y * py + c0z * pz, dx1 = c1x * px + c1y * py + c1z * pz, dx2 = c2x * px + c2y * py + c2z * pz;
      out[o + 16] = c0x / sx; out[o + 17] = c1x / sy; out[o + 18] = c2x / sz; out[o + 19] = 0;
      out[o + 20] = c0y / sx; out[o + 21] = c1y / sy; out[o + 22] = c2y / sz; out[o + 23] = 0;
      out[o + 24] = c0z / sx; out[o + 25] = c1z / sy; out[o + 26] = c2z / sz; out[o + 27] = 0;
      out[o + 28] = -dx0 / sx; out[o + 29] = -dx1 / sy; out[o + 30] = -dx2 / sz; out[o + 31] = 1;
      const frame = Math.max(0, Math.floor(src[s + 11]));
      const cols = m.columns, rows = m.rows;
      const col = frame % cols, row = Math.floor(frame / cols) % rows;
      out[o + 32] = col / cols; out[o + 33] = row / rows; out[o + 34] = 1 / cols; out[o + 35] = 1 / rows;
      out[o + 36] = lin(src[s + 12]); out[o + 37] = lin(src[s + 13]); out[o + 38] = lin(src[s + 14]);
      out[o + 39] = src[s + 15] * opacity;
    }

    return {
      get materials() { return materials; },
      get visible() { return visible; },
      get instances() { return vis; },
      get runs() { return runs; },
      get runMaterial() { return runMat; },
      get runFirst() { return runFirst; },
      get runLength() { return runCount; },
      get version() { return setVersion; },
      // update ingests the effect and pool streams. Returns true when the sorted
      // set changed.
      update(next: any): boolean {
        let changed = false;
        const same = effect && next.materials === effect.materials && next.records === effect.records &&
          next.pools === effect.pools && next.maxDistance === effect.maxDistance;
        if (!same) { ingest(next); changed = true; } else effect = next;
        if (poolsChanged() || changed) { rebuild(); changed = true; }
        return changed;
      },
      // prepare culls to the view and builds draw runs. vp is the view-projection
      // that matches the depth buffer's clip range.
      prepare(vp: Float32Array, zeroToOne: boolean, eye: Float32Array): number {
        let same = lastPrepared === setVersion;
        for (let i = 0; same && i < 16; i++) if (lastVP[i] !== vp[i]) same = false;
        if (same) return visible;
        lastVP.set(vp); lastPrepared = setVersion;
        fx.frustumPlanes(planes, vp, zeroToOne);
        visible = 0; runs = 0;
        let prev = -1;
        for (let k = 0; k < total; k++) {
          const cx = instCenter[k * 4], cy = instCenter[k * 4 + 1], cz = instCenter[k * 4 + 2], r = instCenter[k * 4 + 3];
          const dx = cx - eye[0], dy = cy - eye[1], dz = cz - eye[2];
          const reach = maxDistance + r;
          if (dx * dx + dy * dy + dz * dz > reach * reach) continue;
          if (!fx.sphereVisible(planes, cx, cy, cz, r)) continue;
          vis.set(inst.subarray(k * INST, k * INST + INST), visible * INST);
          const mi = instMat[k];
          if (mi !== prev) { runMat[runs] = mi; runFirst[runs] = visible; runCount[runs] = 0; runs++; prev = mi; }
          runCount[runs - 1]++;
          visible++;
        }
        return visible;
      },
    };
  }

  fx.createDecalSet = createDecalSet;
  fx.DECAL_INSTANCE_FLOATS = INST;
  fx.DECAL_MATERIAL_FLOATS = MAT_FLOATS;
})();
