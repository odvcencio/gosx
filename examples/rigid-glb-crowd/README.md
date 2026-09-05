# Rigid imported GLB crowd fixture

From the framework checkout:

```sh
make build-bootstrap
go run ./examples/rigid-glb-crowd
```

Open `http://127.0.0.1:8179/?backend=webgl&mode=batch&count=256`.
Use `backend=webgpu` for WebGPU and `mode=models` for individual `scene.Model`
declarations. Add `shadows=true` to exercise the separate shadow passes.
The fixture serves the generated chunks from this checkout, authors its scene
in Go/GoSX, and creates a small indexed, two-material GLB in Go. It is a renderer
regression fixture, not an art-direction sample.

`scene.InstancedGLBMesh` now shares immutable geometry and submits opaque rigid
model instances in batches per source primitive, material and shadow-reception
state. Moving declarations retain the source geometry. WebGL streams transforms
into reusable buffers; WebGPU owns reusable geometry, transform, color and
material resources per renderer and retires removed batches.

Per-object bounds and exact CPU picking remain available. Shadow passes retain
individual caster matrices; this change does **not** instance shadow draws.
Transparent, outlined, double-sided, custom-shader and water-scene draws retain
the ordinary mesh path. Skeletons, animated morphs and animated node transforms
keep their animation-owned geometry. Mirrored model transforms also use the
existing path. A statically folded morph can share its immutable result.

The debug snapshot's `rigidGLBBatching` reports candidate primitive instances,
visible instances, color batches and fallback reasons. One actor with two
primitives contributes two primitive instances. `counts.meshObjects` still
counts individual picking/shadow records; `counts.colorMeshObjects` excludes
records drawn by an instanced batch. The existing `counts.drawCalls` estimate
describes color entries, not measured GPU commands or shadow passes.

Browser checks should compare identical backends, viewports and shadow settings.
Count observed instanced calls and allocations; do not infer game frame rate
from this low-polygon fixture. CPU hydration still creates per-actor records,
and alpha sorting and animated crowds are separate investments.

Regression coverage: `client/js/runtime-25-scene-glb-batches.test.js` exercises
the generated mounted runtime, hydration updates, both GPU backends, buffer
reuse and retirement, multiple materials, animation fallbacks, and shadow
matrix isolation. The original Canvas2D wireframe fallback remains available;
GPU glTF surfaces are solid unless the asset authors wireframe explicitly.
