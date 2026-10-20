import fs from 'node:fs';
import vm from 'node:vm';
import { createRequire } from 'node:module';
import { execFileSync } from 'node:child_process';

const ts = createRequire(new URL('../runtime/package.json', import.meta.url))('typescript');

export function crowdContext({ decode = false, baseline = false } = {}) {
  let allocations = 0;
  class TrackedFloat32Array extends Float32Array {
    constructor(...args) { super(...args); allocations++; }
  }
  const context = vm.createContext({ console, ArrayBuffer, Float32Array: TrackedFloat32Array, TextDecoder, URL, Blob });
  for (const name of ['bootstrap-src/11-scene-math.ts', ...(decode ? ['../runtime/scene3d/gltf.ts'] : []), '../runtime/scene3d/animation.ts']) {
    let source = fs.readFileSync(new URL(name, import.meta.url), 'utf8');
    if (baseline && name.endsWith('animation.ts')) source = execFileSync('git', [
      'show', `${typeof baseline === 'string' ? baseline : 'origin/main'}:client/runtime/scene3d/animation.ts`,
    ], { cwd: new URL('../..', import.meta.url), encoding: 'utf8' });
    vm.runInContext(ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022 } }).outputText, context, { filename: name });
  }
  return { context, allocations: () => allocations };
}

export function decodedGLB(context, path) {
  const bytes = fs.readFileSync(path);
  const parsed = context.sceneParseGLB(bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength));
  return context.gltfSceneToModelAsset(context.gltfExtractScene(parsed.json, parsed.binaryBuffer), path);
}

export function syntheticRig({ jointCount = 4, vertexCount = 128, primitiveCount = 1, duration = 1, clipCount = 2 } = {}) {
  const identity = [1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 0, 0, 1];
  const nodes = [{ translation: [3, -2, 5], children: [] }];
  const inverseBindMatrices = new Float32Array(jointCount * 16);
  for (let j = 0; j < jointCount; j++) {
    nodes[0].children.push(j + 1);
    nodes.push({ translation: [j * .1, j * .2, -j * .1] });
    inverseBindMatrices.set(identity, j * 16);
  }
  const objects = [];
  for (let p = 0; p < primitiveCount; p++) {
    const positions = new Float32Array(vertexCount * 3);
    const joints = new Float32Array(vertexCount * 4);
    const weights = new Float32Array(vertexCount * 4);
    for (let i = 0; i < vertexCount; i++) {
      positions.set([Math.sin(i * .17) * (p + 1), Math.cos(i * .13) * 2, i % 37 * .03], i * 3);
      for (let k = 0; k < 4; k++) {
        joints[i * 4 + k] = (i + k) % jointCount;
        weights[i * 4 + k] = i % 37 ? k + 1 : 0;
      }
    }
    objects.push({ skinIndex: 0, vertices: { count: vertexCount, positions, joints, weights } });
  }
  const animations = [];
  for (let c = 0; c < clipCount; c++) animations.push({ name: 'clip-' + c, duration, channels: [
    { targetNode: 1 + c % jointCount, property: 'translation', times: new Float32Array([0, duration]), values: new Float32Array([0, 0, 0, 2, -1, 3]), interpolation: 'LINEAR' },
    { targetNode: 1 + (c + 1) % jointCount, property: 'scale', times: new Float32Array([0, duration]), values: new Float32Array([1, 1, 1, -1, 2, .3]), interpolation: 'LINEAR' },
  ] });
  return { nodes, skins: [{ joints: Array.from({ length: jointCount }, (_, i) => i + 1), inverseBindMatrices }], objects, animations };
}
