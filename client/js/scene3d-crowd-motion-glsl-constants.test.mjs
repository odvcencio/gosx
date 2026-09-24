import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

// webgl.ts cannot import animation.ts's SCENE_CROWD_MOTION_* constants (the
// two files are separate bundle chunks with no shared module graph -- see
// animation.ts's "GPU-driven crowd motion" section and webgl.ts's "GPU-driven
// crowd motion batch binding" section for why each defines its own copy).
// This test is the guard against the two literals drifting apart silently.
const directory = path.dirname(fileURLToPath(import.meta.url));

function constant(file, name) {
  const source = fs.readFileSync(path.join(directory, '..', 'runtime', 'scene3d', file), 'utf8');
  const match = source.match(new RegExp('const ' + name + '\\s*=\\s*([0-9.]+)\\s*;'));
  assert.ok(match, `${name} not found as a const in ${file}`);
  return Number(match[1]);
}

test('SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS matches between animation.ts and webgl.ts', () => {
  assert.equal(
    constant('animation.ts', 'SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS'),
    constant('webgl.ts', 'SCENE_CROWD_MOTION_EXTRAPOLATION_SECONDS'),
  );
});

test('SCENE_CROWD_MOTION_RECORD_FLOATS matches between animation.ts and webgl.ts', () => {
  assert.equal(
    constant('animation.ts', 'SCENE_CROWD_MOTION_RECORD_FLOATS'),
    constant('webgl.ts', 'SCENE_CROWD_MOTION_RECORD_FLOATS'),
  );
});

test('GLSL clip table array size (SCENE_CROWD_MOTION_MAX_CLIPS) is a small, well-under-guaranteed-minimum bound', () => {
  const maxClips = constant('webgl.ts', 'SCENE_CROWD_MOTION_MAX_CLIPS');
  // GLSL ES 3.00 guarantees at least 256 vertex uniform vec4s total, shared
  // with every other uniform the vertex shader declares; this leaves ample
  // headroom for the base PBR/lighting/shadow uniform set.
  assert.ok(maxClips > 0 && maxClips <= 64, `unexpectedly large clip table bound: ${maxClips}`);
});
