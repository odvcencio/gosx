// Vessel cutaway ("dollhouse") on the WebGPU renderer. vessel-cutaway.ts keeps
// object.cutaway (Float32Array(12): decks-above plane, hull-section plane, fade)
// and object.cutawayCrowd (its first 8 floats) current; this file carries them
// to the GPU the way webgl.ts does through u_cut and u_crowdCut.
//
// A cut object owns one slot of the per-frame model arena (webgpu.ts,
// materialFrame): the 64-byte model matrix, then the three planes. Binding 19
// of the material group is that slot, visible to both stages, so the fragment
// shader reads the planes of the object it is drawing. A point p is cut when
// dot(plane.xyz, p) + plane.w > 0; all zeros cuts nothing. Slot 0 and the slots
// of uncut objects keep zero planes. Skinned and Selena-custom meshes use slot 0
// and are not cut.
// Crowd agents are culled in the crowd vertex shaders, by the agent's origin
// against the first two planes, as in the WebGL crowd shaders.

const SCENE_WEBGPU_CUTAWAY_FLOATS = 12;
// Model matrix (64 bytes) plus three vec4 planes.
const SCENE_WEBGPU_CUTAWAY_SLOT_BYTES = 112;
// Index of the first crowd plane float in the crowd params (after the clip table).
const SCENE_WEBGPU_CUTAWAY_CROWD_FLOATS = 8;

// Fragment declaration of binding 19: the vertex shaders read the matrix alone.
const SCENE_WEBGPU_CUTAWAY_DECL_WGSL = [
  "struct GosxObjectCut { model: mat4x4f, cut: array<vec4f, 3> };",
  "@group(1) @binding(19) var<uniform> gosxObject: GosxObjectCut;",
].join("\n");

// First statements of gosxPBRColor, before any texture is sampled. The hull
// section plane discards outright; the decks-above plane discards unless the
// interleaved gradient noise falls under the fade share (0 hides everything).
const SCENE_WEBGPU_CUTAWAY_DISCARD_WGSL = [
  "    if (dot(gosxObject.cut[1].xyz, in.worldPos) + gosxObject.cut[1].w > 0.0) { discard; }",
  "    if (dot(gosxObject.cut[0].xyz, in.worldPos) + gosxObject.cut[0].w > 0.0 && fract(52.9829189 * fract(dot(in.clipPos.xy, vec2f(0.06711056, 0.00583715)))) >= gosxObject.cut[2].x) { discard; }",
].join("\n");

// Crowd: needs crowdParams.cut (struct field) and a model matrix.
const SCENE_WEBGPU_CUTAWAY_CROWD_WGSL =
  "fn crowdCulled(m: mat4x4f) -> bool { let p = m[3].xyz; return dot(crowdParams.cut[0].xyz, p) + crowdParams.cut[0].w > 0.0 || dot(crowdParams.cut[1].xyz, p) + crowdParams.cut[1].w > 0.0; }";

// A mesh needs an arena slot when its geometry is retained or it carries planes.
function webGPUCutawayHasSlot(object: any): boolean {
  return Boolean(object && (object.retainedGeometry || object.cut));
}

// The WeakMap key of an object's slot: the object when it is cut, else its matrix.
function webGPUCutawayKey(object: any, matrix: any): any {
  return object.cut ? object : matrix;
}

// The dynamic offset of an object's slot; 0 (the identity slot) when it has none.
function webGPUCutawayOffset(materialFrame: any, object: any): number {
  const key = object.cut ? object : (object.retainedGeometry ? object.modelMatrix : null);
  return (key && materialFrame.offsets.get(key)) || 0;
}

// Writes the planes of `cut` (or zeros) after the matrix of the slot at `offset`
// bytes. Returns true when a stored float changed, comparing bits so the upload
// is skipped for a still ship.
function webGPUCutawayWrite(materialFrame: any, offset: number, cut: any): boolean {
  let changed = false;
  const base = offset / 4 + 16;
  for (let k = 0; k < SCENE_WEBGPU_CUTAWAY_FLOATS; k++) {
    const v = cut && cut.length > k && Number.isFinite(cut[k]) ? cut[k] : 0;
    const before = materialFrame.words[base + k];
    materialFrame.data[base + k] = v;
    if (materialFrame.words[base + k] !== before) changed = true;
  }
  return changed;
}

// Copies the two crowd planes (or zeros) into the crowd params at float `at`.
function webGPUCutawayCrowdParams(f: Float32Array, at: number, cut: any): void {
  for (let k = 0; k < SCENE_WEBGPU_CUTAWAY_CROWD_FLOATS; k++) f[at + k] = cut && cut.length > k && Number.isFinite(cut[k]) ? cut[k] : 0;
}
