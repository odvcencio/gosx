// Environment.Terrain runtime (lazy chunk bootstrap-feature-scene3d-terrain.js).
//
// This file is backend-neutral. It decodes the heightmap from scene.TerrainIR,
// builds the CDLOD quadtree (per-node height bounds), picks patches each frame
// so the projected geometric error stays under the authored pixel budget, and
// packs the uniform block both backends read. terrain-wgsl.ts (WebGPU) and
// terrain-glsl.ts (WebGL2) draw the selection with the renderer's own PBR
// fragment, so terrain receives the scene's shadows, fog and image lighting.
//
// Grid layout: level 0 is the finest; its vertex spacing is ir.spacing and
// level L uses spacing * 2^L. A node spans SCENE_TERRAIN_NODE_QUADS quads and
// draws as four quarter patches of SCENE_TERRAIN_PATCH_QUADS quads. Each patch
// instance is (originX, originZ, scale, level) in finest-grid index units, so
// two patches compute a shared edge vertex from the same integers.

const SCENE_TERRAIN_NODE_QUADS = 32;
const SCENE_TERRAIN_PATCH_QUADS = 16;
const SCENE_TERRAIN_MORPH = 0.3;
const SCENE_TERRAIN_MAX_PATCHES = 4096;
const SCENE_TERRAIN_MAX_LEVELS = 12;
const SCENE_TERRAIN_VEC4S = 36;

function sceneTerrainBytes(text: string): Uint8Array {
  const binary = atob(text);
  const out = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i++) out[i] = binary.charCodeAt(i);
  return out;
}

// sceneTerrainSamples reads little-endian 16-bit samples from base64 text or
// fetched bytes. It returns null when the count does not match the grid.
function sceneTerrainSamples(bytes: Uint8Array, count: number): Uint16Array | null {
  if (!bytes || bytes.length !== count * 2) return null;
  const out = new Uint16Array(count);
  for (let i = 0; i < count; i++) out[i] = bytes[2 * i] | (bytes[2 * i + 1] << 8);
  return out;
}

function sceneTerrainNumber(value: any, fallback: number): number {
  const n = Number(value);
  return Number.isFinite(n) ? n : fallback;
}

function sceneTerrainLinear(channel: number): number {
  const c = Math.max(0, Math.min(1, channel));
  return c <= 0.04045 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
}

// sceneTerrainColor parses #rgb or #rrggbb into linear RGB.
function sceneTerrainColor(text: any): number[] {
  let hex = typeof text === "string" ? text.trim().replace(/^#/, "") : "";
  if (hex.length === 3) hex = hex[0] + hex[0] + hex[1] + hex[1] + hex[2] + hex[2];
  const value = /^[0-9a-fA-F]{6}$/.test(hex) ? parseInt(hex, 16) : 0x808080;
  return [sceneTerrainLinear(((value >> 16) & 255) / 255), sceneTerrainLinear(((value >> 8) & 255) / 255), sceneTerrainLinear((value & 255) / 255)];
}

// sceneTerrainBuild turns a TerrainIR and its samples into the CPU model:
// normalized heights, the per-texel normal map with its mip chain, and the
// per-node height bounds the selection culls with.
function sceneTerrainBuild(ir: any, samples: Uint16Array): any {
  const columns = Math.floor(ir.columns), rows = Math.floor(ir.rows);
  const sizeX = sceneTerrainNumber(ir.sizeX, 256), sizeZ = sceneTerrainNumber(ir.sizeZ, sizeX);
  const scale = sceneTerrainNumber(ir.heightScale, 32), baseY = sceneTerrainNumber(ir.baseY, 0);
  const levels = Math.max(1, Math.min(SCENE_TERRAIN_MAX_LEVELS, Math.floor(sceneTerrainNumber(ir.levels, 1))));
  const spacing = sceneTerrainNumber(ir.spacing, Math.max(sizeX, sizeZ) / (SCENE_TERRAIN_NODE_QUADS * Math.pow(2, levels - 1)));
  const heights = new Float32Array(columns * rows);
  for (let i = 0; i < heights.length; i++) heights[i] = samples[i] / 65535;
  const model: any = {
    ir, columns, rows, sizeX, sizeZ, scale, baseY, levels, spacing, heights,
    minX: sceneTerrainNumber(ir.minX, -sizeX / 2), minZ: sceneTerrainNumber(ir.minZ, -sizeZ / 2),
    texelX: sizeX / (columns - 1), texelZ: sizeZ / (rows - 1),
    nodeSize: [], nodeCount: [], nodeMin: [], nodeMax: [], nodeDiag: [],
  };
  model.normals = sceneTerrainNormalMips(model);
  sceneTerrainBounds(model);
  return model;
}

function sceneTerrainTexel(model: any, column: number, row: number): number {
  const c = Math.max(0, Math.min(model.columns - 1, column));
  const r = Math.max(0, Math.min(model.rows - 1, row));
  return model.heights[r * model.columns + c];
}

// sceneTerrainHeight is scene.Terrain.HeightAt: the shared diagonal split from
// (i, j) to (i+1, j+1), clamped to the terrain edge.
function sceneTerrainHeight(model: any, x: number, z: number): number {
  const gx = Math.max(0, Math.min(model.columns - 1, (x - model.minX) / model.texelX));
  const gz = Math.max(0, Math.min(model.rows - 1, (z - model.minZ) / model.texelZ));
  const i = Math.min(Math.floor(gx), model.columns - 2), j = Math.min(Math.floor(gz), model.rows - 2);
  const fx = gx - i, fz = gz - j;
  const h00 = sceneTerrainTexel(model, i, j), h10 = sceneTerrainTexel(model, i + 1, j);
  const h01 = sceneTerrainTexel(model, i, j + 1), h11 = sceneTerrainTexel(model, i + 1, j + 1);
  const h = fx >= fz ? h00 + fx * (h10 - h00) + fz * (h11 - h10) : h00 + fz * (h01 - h00) + fx * (h11 - h01);
  return model.baseY + h * model.scale;
}

// sceneTerrainNormal is scene's per-sample normal: central differences in
// world units, one-sided at the edge.
function sceneTerrainNormal(model: any, column: number, row: number, out: number[]): number[] {
  const left = Math.max(0, column - 1), right = Math.min(model.columns - 1, column + 1);
  const down = Math.max(0, row - 1), up = Math.min(model.rows - 1, row + 1);
  const dhdx = (sceneTerrainTexel(model, right, row) - sceneTerrainTexel(model, left, row)) * model.scale / ((right - left) * model.texelX);
  const dhdz = (sceneTerrainTexel(model, column, up) - sceneTerrainTexel(model, column, down)) * model.scale / ((up - down) * model.texelZ);
  const length = Math.hypot(dhdx, 1, dhdz);
  out[0] = -dhdx / length; out[1] = 1 / length; out[2] = -dhdz / length;
  return out;
}

// sceneTerrainNormalMips packs the normal map as RGBA8 (xyz * 0.5 + 0.5) and
// box-filters a full mip chain so distant slopes do not shimmer.
function sceneTerrainNormalMips(model: any): any[] {
  const n = [0, 0, 0];
  let width = model.columns, height = model.rows;
  let level = new Float32Array(width * height * 3);
  for (let row = 0; row < height; row++) {
    for (let column = 0; column < width; column++) {
      sceneTerrainNormal(model, column, row, n);
      level.set(n, (row * width + column) * 3);
    }
  }
  const mips: any[] = [];
  for (;;) {
    const data = new Uint8Array(width * height * 4);
    for (let i = 0; i < width * height; i++) {
      const x = level[3 * i], y = level[3 * i + 1], z = level[3 * i + 2];
      const length = Math.hypot(x, y, z) || 1;
      data[4 * i] = Math.round((x / length * 0.5 + 0.5) * 255);
      data[4 * i + 1] = Math.round((y / length * 0.5 + 0.5) * 255);
      data[4 * i + 2] = Math.round((z / length * 0.5 + 0.5) * 255);
      data[4 * i + 3] = 255;
    }
    mips.push({ width, height, data });
    if (width === 1 && height === 1) break;
    const nextWidth = Math.max(1, width >> 1), nextHeight = Math.max(1, height >> 1);
    const next = new Float32Array(nextWidth * nextHeight * 3);
    for (let row = 0; row < nextHeight; row++) {
      for (let column = 0; column < nextWidth; column++) {
        for (let dz = 0; dz < 2; dz++) {
          for (let dx = 0; dx < 2; dx++) {
            const c = Math.min(width - 1, column * 2 + dx), r = Math.min(height - 1, row * 2 + dz);
            for (let k = 0; k < 3; k++) next[(row * nextWidth + column) * 3 + k] += level[(r * width + c) * 3 + k] * 0.25;
          }
        }
      }
    }
    level = next; width = nextWidth; height = nextHeight;
  }
  return mips;
}

// sceneTerrainBounds builds the min/max height pyramid. A level-0 node covers
// every sample that touches its square, edges included, so its box always
// encloses the surface drawn inside it.
function sceneTerrainBounds(model: any): void {
  for (let level = 0; level < model.levels; level++) {
    const count = 1 << (model.levels - 1 - level);
    model.nodeSize[level] = SCENE_TERRAIN_NODE_QUADS * model.spacing * Math.pow(2, level);
    model.nodeCount[level] = count;
    model.nodeMin[level] = new Float32Array(count * count).fill(Infinity);
    model.nodeMax[level] = new Float32Array(count * count).fill(-Infinity);
  }
  const count0 = model.nodeCount[0], size0 = model.nodeSize[0];
  for (let b = 0; b < count0; b++) {
    const z0 = b * size0, z1 = z0 + size0;
    const r0 = Math.max(0, Math.floor(z0 / model.texelZ)), r1 = Math.min(model.rows - 1, Math.ceil(z1 / model.texelZ));
    for (let a = 0; a < count0; a++) {
      const x0 = a * size0, x1 = x0 + size0;
      if (x0 > model.sizeX || z0 > model.sizeZ) continue;
      const c0 = Math.max(0, Math.floor(x0 / model.texelX)), c1 = Math.min(model.columns - 1, Math.ceil(x1 / model.texelX));
      let low = Infinity, high = -Infinity;
      for (let r = r0; r <= r1; r++) {
        for (let c = c0; c <= c1; c++) {
          const h = model.heights[r * model.columns + c];
          if (h < low) low = h;
          if (h > high) high = h;
        }
      }
      model.nodeMin[0][b * count0 + a] = model.baseY + low * model.scale;
      model.nodeMax[0][b * count0 + a] = model.baseY + high * model.scale;
    }
  }
  for (let level = 1; level < model.levels; level++) {
    const count = model.nodeCount[level], child = model.nodeCount[level - 1];
    for (let b = 0; b < count; b++) {
      for (let a = 0; a < count; a++) {
        let low = Infinity, high = -Infinity;
        for (let q = 0; q < 4; q++) {
          const index = (2 * b + (q >> 1)) * child + 2 * a + (q & 1);
          low = Math.min(low, model.nodeMin[level - 1][index]);
          high = Math.max(high, model.nodeMax[level - 1][index]);
        }
        model.nodeMin[level][b * count + a] = low;
        model.nodeMax[level][b * count + a] = high;
      }
    }
  }
  for (let level = 0; level < model.levels; level++) {
    let span = 0;
    const low = model.nodeMin[level], high = model.nodeMax[level];
    for (let i = 0; i < low.length; i++) if (high[i] >= low[i]) span = Math.max(span, high[i] - low[i]);
    const size = model.nodeSize[level];
    model.nodeDiag[level] = Math.sqrt(2 * size * size + span * span);
  }
}

// sceneTerrainRanges returns, per level, the camera distance beyond which the
// level is too fine to need. The error term keeps the projected error of the
// next coarser level under pixelError pixels; the size term keeps every level
// band wider than one node diagonal plus its morph zone, which is what makes
// a level-L edge vertex fully morphed exactly where a level L+1 patch begins.
function sceneTerrainRanges(model: any, viewportHeight: number, fovDegrees: number, pixelError: number, out: Float64Array): Float64Array {
  const fov = Math.max(1, Math.min(170, sceneTerrainNumber(fovDegrees, 60))) * Math.PI / 180;
  const k = Math.max(1, viewportHeight) / (2 * Math.tan(fov / 2));
  const errors = Array.isArray(model.ir.lodErrors) ? model.ir.lodErrors : [];
  let previous = 0;
  for (let level = 0; level < model.levels - 1; level++) {
    const errorRange = sceneTerrainNumber(errors[level + 1], 0) * k / Math.max(0.05, pixelError);
    const sizeRange = level === 0 ? model.nodeDiag[0] : previous + model.nodeDiag[level - 1] / (1 - SCENE_TERRAIN_MORPH);
    previous = Math.max(errorRange, sizeRange);
    out[level] = previous;
  }
  out[model.levels - 1] = 1e30;
  return out;
}

// sceneTerrainPlanes extracts the left, right, bottom and top clip planes from
// a column-major view-projection matrix. They are the same for GL and WebGPU
// depth conventions, which differ only in the z row.
function sceneTerrainPlanes(m: ArrayLike<number>, out: Float64Array): Float64Array {
  for (let p = 0; p < 4; p++) {
    const row = p >> 1, sign = p & 1 ? -1 : 1;
    for (let c = 0; c < 4; c++) out[p * 4 + c] = m[c * 4 + 3] + sign * m[c * 4 + row];
  }
  return out;
}

function sceneTerrainBoxVisible(planes: Float64Array | null, x0: number, y0: number, z0: number, x1: number, y1: number, z1: number): boolean {
  if (!planes) return true;
  for (let p = 0; p < 4; p++) {
    const a = planes[p * 4], b = planes[p * 4 + 1], c = planes[p * 4 + 2], d = planes[p * 4 + 3];
    if (a * (a >= 0 ? x1 : x0) + b * (b >= 0 ? y1 : y0) + c * (c >= 0 ? z1 : z0) + d < 0) return false;
  }
  return true;
}

function sceneTerrainSphereBox(eye: any, radius: number, x0: number, y0: number, z0: number, x1: number, y1: number, z1: number): boolean {
  const dx = Math.max(x0 - eye.x, 0, eye.x - x1), dy = Math.max(y0 - eye.y, 0, eye.y - y1), dz = Math.max(z0 - eye.z, 0, eye.z - z1);
  return dx * dx + dy * dy + dz * dz <= radius * radius;
}

// sceneTerrainSelect walks the quadtree (CDLOD) and writes one patch record
// per drawn quarter node into out. It returns the patch count.
function sceneTerrainSelect(model: any, eye: any, planes: Float64Array | null, ranges: Float64Array, out: Float32Array): number {
  const state = { model, eye, planes, ranges, out, count: 0, max: Math.floor(out.length / 4) };
  sceneTerrainSelectNode(state, model.levels - 1, 0, 0);
  return state.count;
}

function sceneTerrainEmit(state: any, level: number, a: number, b: number, quadrant: number): void {
  const model = state.model, half = model.nodeSize[level] / 2;
  const qa = 2 * a + (quadrant & 1), qb = 2 * b + (quadrant >> 1);
  if (qa * half >= model.sizeX || qb * half >= model.sizeZ || state.count >= state.max) return;
  const scale = Math.pow(2, level), o = state.count * 4;
  state.out[o] = qa * SCENE_TERRAIN_PATCH_QUADS * scale;
  state.out[o + 1] = qb * SCENE_TERRAIN_PATCH_QUADS * scale;
  state.out[o + 2] = scale;
  state.out[o + 3] = level;
  state.count++;
}

function sceneTerrainSelectNode(state: any, level: number, a: number, b: number): boolean {
  const model = state.model, size = model.nodeSize[level], count = model.nodeCount[level];
  const x0 = model.minX + a * size, z0 = model.minZ + b * size;
  if (a * size >= model.sizeX || b * size >= model.sizeZ) return true;
  const index = b * count + a, y0 = model.nodeMin[level][index], y1 = model.nodeMax[level][index];
  const x1 = x0 + size, z1 = z0 + size;
  if (level < model.levels - 1 && !sceneTerrainSphereBox(state.eye, state.ranges[level], x0, y0, z0, x1, y1, z1)) return false;
  if (!sceneTerrainBoxVisible(state.planes, x0, y0, z0, x1, y1, z1)) return true;
  if (level === 0 || !sceneTerrainSphereBox(state.eye, state.ranges[level - 1], x0, y0, z0, x1, y1, z1)) {
    for (let q = 0; q < 4; q++) sceneTerrainEmit(state, level, a, b, q);
    return true;
  }
  for (let q = 0; q < 4; q++) {
    if (!sceneTerrainSelectNode(state, level - 1, 2 * a + (q & 1), 2 * b + (q >> 1))) sceneTerrainEmit(state, level, a, b, q);
  }
  return true;
}

// sceneTerrainIndices is the patch index buffer, split on the same diagonal
// as scene.Terrain.HeightAt and wound counter-clockwise seen from above.
function sceneTerrainIndices(): Uint16Array {
  const q = SCENE_TERRAIN_PATCH_QUADS, out = new Uint16Array(q * q * 6);
  let o = 0;
  for (let j = 0; j < q; j++) {
    for (let i = 0; i < q; i++) {
      const v00 = j * (q + 1) + i, v10 = v00 + 1, v01 = v00 + q + 1, v11 = v01 + 1;
      out[o++] = v00; out[o++] = v11; out[o++] = v10;
      out[o++] = v00; out[o++] = v01; out[o++] = v11;
    }
  }
  return out;
}

// sceneTerrainUniforms packs the block both shaders read (36 vec4):
//   0 minX minZ sizeX sizeZ | 1 baseY heightScale columns rows
//   2 spacing patchQuads layerCount triplanarSlope | 3 hasSplat splatColumns splatRows lodTint
//   4..15 per level: morphStart, 1/(morphEnd-morphStart)
//   16..31 per layer: [linear rgb, roughness] [scale, hasAlbedo, hasNormal, 0]
//                     [minHeight, maxHeight, heightBlend, 0] [minSlope, maxSlope, slopeBlend, 0]
function sceneTerrainUniforms(model: any, ranges: Float64Array, layerFlags: number[], tint: boolean, out: Float32Array): Float32Array {
  const ir = model.ir, layers = Array.isArray(ir.layers) ? ir.layers.slice(0, 4) : [];
  out.fill(0);
  out.set([model.minX, model.minZ, model.sizeX, model.sizeZ, model.baseY, model.scale, model.columns, model.rows], 0);
  out.set([model.spacing, SCENE_TERRAIN_PATCH_QUADS, Math.max(1, layers.length), sceneTerrainNumber(ir.triplanarSlope, 35)], 8);
  const splat = ir.splat;
  out.set([splat ? 1 : 0, splat ? splat.columns : 1, splat ? splat.rows : 1, tint ? 1 : 0], 12);
  for (let level = 0; level < SCENE_TERRAIN_MAX_LEVELS; level++) {
    const o = 16 + level * 4;
    if (level >= model.levels - 1) { out[o] = 1e30; continue; }
    const start = level === 0 ? 0 : ranges[level - 1], end = ranges[level];
    const morphStart = start + (1 - SCENE_TERRAIN_MORPH) * (end - start);
    // Finish the morph 1% early: a float32 vertex stage then reaches k = 1
    // before the boundary instead of a hair short of it.
    out[o] = morphStart; out[o + 1] = 1 / Math.max(1e-6, (end - morphStart) * 0.99);
  }
  for (let i = 0; i < 4; i++) {
    const layer = layers[i] || layers[0] || {}, o = 64 + i * 16;
    const color = sceneTerrainColor(layer.color);
    const minSlope = sceneTerrainNumber(layer.minSlope, 0), maxSlope = sceneTerrainNumber(layer.maxSlope, 90);
    out.set([color[0], color[1], color[2], sceneTerrainNumber(layer.roughness, 0.9)], o);
    out.set([Math.max(0.01, sceneTerrainNumber(layer.scale, 4)), layerFlags[i] & 1 ? 1 : 0, layerFlags[i] & 2 ? 1 : 0, 0], o + 4);
    out.set([sceneTerrainNumber(layer.minHeight, -1e6), sceneTerrainNumber(layer.maxHeight, 1e6), Math.max(1e-3, sceneTerrainNumber(layer.heightBlend, 2)), 0], o + 8);
    out.set([minSlope <= 0 ? -1e6 : minSlope, maxSlope >= 90 ? 1e6 : maxSlope, Math.max(1e-3, sceneTerrainNumber(layer.slopeBlend, 6)), 0], o + 12);
  }
  return out;
}

// sceneTerrainSplat decodes the optional RGBA8 splat map.
function sceneTerrainSplat(ir: any): any {
  const splat = ir && ir.splat;
  if (!splat || typeof splat.data !== "string") return null;
  const data = sceneTerrainBytes(splat.data);
  return data.length === splat.columns * splat.rows * 4 ? { width: splat.columns, height: splat.rows, data } : null;
}

function sceneTerrainKey(ir: any): string {
  return [ir.id, ir.columns, ir.rows, ir.src || "", ir.heights ? ir.heights.length : 0, ir.minX, ir.minZ, ir.sizeX, ir.sizeZ, ir.baseY, ir.heightScale, ir.levels].join("|");
}

// sceneTerrainModelFor caches the decoded model on the backend resource record.
// A src terrain fetches its samples once and reports "pending" until they land.
function sceneTerrainModelFor(res: any, ir: any): any {
  const key = sceneTerrainKey(ir);
  if (res.modelKey === key && res.model) return res.model;
  if (res.modelKey !== key) {
    res.modelKey = key; res.model = null; res.modelError = "";
    const count = Math.floor(ir.columns) * Math.floor(ir.rows);
    if (typeof ir.heights === "string" && ir.heights) {
      const samples = sceneTerrainSamples(sceneTerrainBytes(ir.heights), count);
      if (samples) res.model = sceneTerrainBuild(ir, samples); else res.modelError = "heights";
    } else if (typeof ir.src === "string" && ir.src && typeof fetch === "function") {
      fetch(ir.src).then(function(response: any) { return response.arrayBuffer(); }).then(function(buffer: ArrayBuffer) {
        if (res.modelKey !== key) return;
        const samples = sceneTerrainSamples(new Uint8Array(buffer), count);
        if (samples) res.model = sceneTerrainBuild(ir, samples); else res.modelError = "src";
      }, function() { if (res.modelKey === key) res.modelError = "src"; });
    } else {
      res.modelError = "heights";
    }
  }
  return res.model;
}

// sceneTerrainFrame runs the per-frame CPU work shared by both backends: the
// level ranges for this viewport, frustum planes and the patch selection.
function sceneTerrainFrame(res: any, model: any, camera: any, viewProj: ArrayLike<number>, viewportHeight: number, pixelScale: number): number {
  if (!res.patches) {
    res.patches = new Float32Array(SCENE_TERRAIN_MAX_PATCHES * 4);
    res.ranges = new Float64Array(SCENE_TERRAIN_MAX_LEVELS);
    res.planes = new Float64Array(16);
  }
  const started = typeof performance !== "undefined" ? performance.now() : 0;
  const pixelError = sceneTerrainNumber(model.ir.pixelError, 1) * pixelScale;
  sceneTerrainRanges(model, viewportHeight, camera && camera.fov, pixelError, res.ranges);
  const eye = { x: sceneTerrainNumber(camera && camera.x, 0), y: sceneTerrainNumber(camera && camera.y, 0), z: sceneTerrainNumber(camera && camera.z, 0) };
  const planes = viewProj && viewProj.length >= 16 ? sceneTerrainPlanes(viewProj, res.planes) : null;
  res.count = sceneTerrainSelect(model, eye, planes, res.ranges, res.patches);
  res.selectMS = typeof performance !== "undefined" ? performance.now() - started : 0;
  return res.count;
}

function sceneTerrainTint(): boolean {
  return typeof window !== "undefined" && (window as any).__gosx_scene3d_terrain_lod_tint === true;
}

// Environment.Wind (scene/wind.go). The chunk keeps one shared state the
// renderers read: window.__gosx_scene3d_wind holds wind = (dirX, dirZ, speed,
// time) and windGust = (gust, gustScale, gustSpeed, frequency). WebGPU copies
// it to byte 160 of the frame uniforms and into the material block of every
// batch with a sway; WebGL2 uploads sceneWindFor(batch.wind) as u_wind[3].
const SCENE_WIND_STATE = new Float32Array(8);
const SCENE_WIND_BATCH = new Float32Array(12);

function sceneWindUpdate(environment: any, time: number): Float32Array {
  const wind = environment && environment.wind;
  const state = SCENE_WIND_STATE;
  if (!wind) { state.fill(0); state[5] = 1; }
  else {
    state[0] = sceneTerrainNumber(wind.directionX, 0); state[1] = sceneTerrainNumber(wind.directionZ, 1);
    state[2] = sceneTerrainNumber(wind.speed, 4); state[3] = sceneTerrainNumber(time, 0);
    state[4] = sceneTerrainNumber(wind.gust, 0.35); state[5] = Math.max(1, sceneTerrainNumber(wind.gustScale, 24));
    state[6] = sceneTerrainNumber(wind.gustSpeed, state[2]); state[7] = sceneTerrainNumber(wind.frequency, 1.3);
  }
  if (typeof window !== "undefined") (window as any).__gosx_scene3d_wind = state;
  return state;
}

// sceneWindFor returns the 12 floats one swaying batch reads: the shared
// state followed by (sway, height, 0, 0). No wind or no sway gives zero sway.
function sceneWindFor(batch: any): Float32Array {
  SCENE_WIND_BATCH.set(SCENE_WIND_STATE, 0);
  SCENE_WIND_BATCH[8] = batch && SCENE_WIND_STATE[2] > 0 ? sceneTerrainNumber(batch.sway, 0) : 0;
  SCENE_WIND_BATCH[9] = Math.max(1e-3, sceneTerrainNumber(batch && batch.height, 1));
  SCENE_WIND_BATCH[10] = 0; SCENE_WIND_BATCH[11] = 0;
  return SCENE_WIND_BATCH;
}

// sceneWindGust mirrors scene.WindIR.gustFactor; the shaders evaluate the same
// expression per batch vertex.
function sceneWindGust(state: ArrayLike<number>, x: number, z: number, t: number): number {
  const along = x * state[0] + z * state[1], across = x * state[1] - z * state[0];
  const phase = (along - state[6] * t) * (2 * Math.PI) / state[5] + 0.35 * Math.sin(across * (2 * Math.PI) / (state[5] * 1.7));
  return 1 + state[4] * Math.sin(phase);
}
