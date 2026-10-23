// CPU preparation follows renderer-owned batch records, whose retirement also
// releases these weakly held arrays. SceneIR and caller arrays stay untouched.
interface SceneInstancedPreparedData {
  transforms?: Float32Array;
  colors?: Float32Array;
  colorSource?: unknown[] | null;
  colorCount?: number;
}
const sceneInstancedPreparedData = new WeakMap<object, SceneInstancedPreparedData>();
let sceneInstancedParsedColors: Map<string, number[]> | null = null;

function sceneInstancedPreparedOwner(owner: object): SceneInstancedPreparedData {
  let cached = sceneInstancedPreparedData.get(owner);
  if (!cached) { cached = {}; sceneInstancedPreparedData.set(owner, cached); }
  return cached;
}

function scenePrepareInstancedTransforms(owner: object, source: any): Float32Array | null {
  if (source instanceof Float32Array || !Array.isArray(source)) {
    const cached = sceneInstancedPreparedData.get(owner);
    if (cached) cached.transforms = undefined;
    return source instanceof Float32Array ? source : null;
  }
  const cached = sceneInstancedPreparedOwner(owner);
  if (!cached.transforms || cached.transforms.length !== source.length) {
    cached.transforms = new Float32Array(source.length);
  }
  // Copy into retained storage: source identity cannot prove that a mutable
  // array is unchanged. TypedArray.set preserves constructor coercion/rounding.
  cached.transforms.set(source);
  return cached.transforms;
}

function sceneInstancedPreparedColor(value: any): number[] {
  if (typeof value !== "string") return sceneColorRGBA(value, [1, 1, 1, 1]);
  const colors = sceneInstancedParsedColors || (sceneInstancedParsedColors = new Map());
  let rgba = colors.get(value);
  if (!rgba) {
    rgba = sceneColorRGBA(value, [1, 1, 1, 1]);
    if (colors.size >= 512) colors.clear();
    colors.set(value, rgba);
  }
  return rgba;
}

function sceneInstancedColorSourceMatches(previous: unknown[] | null | undefined, source: any, length: number): boolean {
  if (!previous || previous.length !== length) return false;
  for (let i = 0; i < length; i++) {
    const value = source[i];
    // NaN payloads and coercible objects cannot be validated by value equality.
    if (value !== value || !Object.is(previous[i], value) || value !== null && (typeof value === "object" || typeof value === "function")) return false;
  }
  return true;
}

function scenePrepareInstancedColors(owner: object, source: any, count: number, fallbackWhite = false): Float32Array | null {
  let cached = sceneInstancedPreparedData.get(owner);
  const length = source && typeof source.length === "number" ? source.length : 0;
  const format = Array.isArray(source) && typeof source[0] === "string" ? 1
    : length >= count * 4 ? 4 : length >= count * 3 ? 3 : 0;
  if (count <= 0 || !length || !format) {
    if (count > 0 && fallbackWhite) {
      cached = cached || sceneInstancedPreparedOwner(owner);
      if (!cached.colors || cached.colors.length !== count * 4) cached.colors = new Float32Array(count * 4);
      if (cached.colorSource !== null || cached.colorCount !== count) cached.colors.fill(1);
      cached.colorSource = null; cached.colorCount = count;
      return cached.colors;
    }
    if (cached) { cached.colors = undefined; cached.colorSource = undefined; cached.colorCount = 0; }
    return null;
  }
  if (source instanceof Float32Array && format === 4) {
    if (cached) { cached.colors = undefined; cached.colorSource = undefined; cached.colorCount = 0; }
    return source;
  }
  cached = cached || sceneInstancedPreparedOwner(owner);
  const previous = cached.colorSource;
  if (format === 1 && cached.colors && cached.colorCount === count && sceneInstancedColorSourceMatches(previous, source, length)) return cached.colors;
  const needed = format === 4 ? length : count * 4;
  if (!cached.colors || cached.colors.length !== needed) cached.colors = new Float32Array(needed);
  const data = cached.colors;
  // A failed coercion may write a prefix. Invalidate the palette before
  // touching live storage so a later valid input cannot reuse partial data.
  cached.colorCount = 0;
  if (format === 4) data.set(source);
  else if (format === 3) {
    for (let i = 0; i < count; i++) {
      data[i * 4] = source[i * 3]; data[i * 4 + 1] = source[i * 3 + 1];
      data[i * 4 + 2] = source[i * 3 + 2]; data[i * 4 + 3] = 1;
    }
  } else {
    for (let i = 0; i < count; i++) {
      const rgba = sceneInstancedPreparedColor(source[i] || source[length - 1]);
      data[i * 4] = rgba[0]; data[i * 4 + 1] = rgba[1];
      data[i * 4 + 2] = rgba[2]; data[i * 4 + 3] = rgba[3];
    }
  }
  // Numeric inputs copy into retained storage, just like transforms. Only
  // expensive palette conversion needs a snapshot, reused on later edits.
  if (format === 1) {
    const snapshot = previous || [];
    snapshot.length = length;
    for (let i = 0; i < length; i++) snapshot[i] = source[i];
    cached.colorSource = snapshot;
  } else cached.colorSource = undefined;
  cached.colorCount = count;
  return data;
}
