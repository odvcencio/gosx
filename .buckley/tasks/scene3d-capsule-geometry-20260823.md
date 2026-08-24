# Ox Alpha task: Scene3D CapsuleGeometry vertical slice

Repository: `m31labs.dev/gosx`, MIT licensed at the repository root.
Source base before this task-only checkpoint: `90a80ef3e895efc8339a8ed74578242b75215563`.
Return only a complete valid unified diff. Do not include prose or Markdown fences.

## Goal

Implement the first explicitly deferred primitive in
`docs/scene3d-native-webgpu-spec.md`: a coherent `scene.CapsuleGeometry`
contract from typed Go authoring through SceneIR, the existing TypeScript
browser geometry path, native/headless rendering, exact picking, bounds, and
focused parity tests.

This is one bounded primitive slice, not a renderer refactor or demo redesign.
Do not touch Gridiron, gsxmail, CSS, `.gsx` pages, authentication, actions,
navigation, animation scheduling, HTML textures, or unrelated Scene3D work.

## Public contract

Add a typed geometry whose canonical wire kind is `"capsule"`. Use a small,
idiomatic API with these authored dimensions:

- radius;
- straight cylindrical body length along +Y/-Y;
- cap segments per hemisphere;
- radial segments around the Y axis.

The capsule is centered at the origin. Its total Y extent is the straight body
length plus two radii. Normals point outward and UVs are finite and bounded.
Normalize zero/invalid dimensions and segment counts deterministically in the
same style as existing sphere/cylinder/torus primitives. Keep existing wire
compatibility by accepting the canonical name and the `capsuleGeometry` alias
where primitive-name normalization already supports aliases.

Preserve every authored dimension in SceneIR and its typed/legacy adapters;
do not overload an unrelated field if doing so makes the wire contract
ambiguous. Update the schema vocabulary and inspection/cost estimates where
the primitive catalog requires them.

## Rendering and correctness requirements

1. Add capsule generation to the shared Go `scene/geom` generator and route
   `scene.Tessellate` through it. Native/headless consumers must reuse that
   generator rather than adding a second Go mesh implementation.
2. Extend the existing TypeScript source geometry path used by browser
   WebGPU/WebGL. Do not add or hand-edit JavaScript source. Do not edit
   generated `.js`, `.map`, `.gz`, or `.br` artifacts in this patch.
3. Emit triangle data with counter-clockwise front faces, nondegenerate
   triangles, outward unit-length normals within ordinary floating tolerance,
   and finite UVs. Join body and hemispheres without a positional crack.
4. Make native primitive normalization, cache keys, generated buffers, and
   culling radius honor radius/body length/cap/radial resolution. Distinct
   authored resolutions or dimensions must not alias in the cache.
5. Exact CPU picking must work through the shared tessellated triangle path,
   including translated/scaled meshes and instanced meshes. Broadphase bounds
   must enclose every emitted vertex.
6. Preserve ordinary material, transform, shadow, visibility, animation, and
   fallback behavior by flowing through existing generic primitive paths; do
   not create capsule-only renderer branches beyond geometry construction and
   parameter normalization.
7. Unsupported/invalid internal states must fail visibly or fall back safely;
   never silently drop the draw.

## Scope and size

Modify only files needed under these existing areas:

- `scene/` (typed API, IR/schema, shared geometry, tessellation, picking/bounds,
  inspection, and focused tests);
- `render/bundle/` (native primitive normalization/cache/bounds and tests);
- `client/js/bootstrap-src/` plus focused source-level Node tests under
  `client/js/`;
- the capsule entry/status in `docs/scene3d-native-webgpu-spec.md` if useful.

Do not add dependencies, generated bundles, binary/golden images, examples, or
new build systems. Keep production additions under 700 net lines and tests
under 700 net lines. Prefer extending shared tables/switches and existing test
matrices over parallel abstractions.

## Required evidence in tests

Add focused tests proving:

- typed lowering and JSON/schema round-trip preserve all capsule parameters;
- default and custom segment counts produce deterministic nonempty geometry;
- vertex Y bounds match the authored body length/radius contract;
- normals, UVs, winding, seam positions, and triangles are valid;
- native kind aliases, cache separation, cull radius, and buffer upload work;
- exact ray hits and misses behave correctly, and accelerated picking agrees
  with the direct graph walk;
- the browser TypeScript generator returns matching bounds/count semantics and
  valid winding for the same representative inputs.

Design the patch to pass:

```sh
go test ./scene/... ./render/bundle/... -count=1
node --test client/js/12-scene-geometry.test.mjs client/js/12-scene-geometry-winding.test.mjs
go test ./cmd/buildbootstrap/... -count=1
go test ./... -count=1
go build ./...
git diff --check
```

Return the complete patch only.
