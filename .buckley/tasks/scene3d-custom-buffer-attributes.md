# Task: custom BufferAttributes for Scene3D Selena meshes

You are the acting implementation agent. This is the next bounded P0 geometry
slice in the GoSX Three.js-subsumption roadmap. Indexed BufferGeometry is now
merged; add typed custom float vertex attributes that authored Selena mesh
shaders can consume without expanding geometry or allocating per frame.
Inspect the repository, implement the smallest coherent vertical slice, and
leave the working tree uncommitted for independent review. Do not commit,
push, edit generated/minified bundles, add dependencies, or broaden into
interleaved storage, geometry groups/material slots, integer attributes,
instancing, morph targets, or a general geometry redesign.

Repository: GoSX, MIT licensed. Work from the typed TypeScript and Go sources
of truth. Existing JavaScript tests may be edited where they are the
established runtime test surface, but do not create a hand-maintained runtime
implementation outside the typed build.

## Outcome

An author can attach named, per-vertex float data to
`scene.BufferGeometry`, describe the same attribute in an existing Selena
material's `shaderLayout.attributes`, and have both WebGL2 and WebGPU bind that
stream at the descriptor's declared shader location for the mesh draw. Indexed
geometry must remain indexed, and unchanged retained geometry must reuse its
custom GPU buffers.

This first slice may require the explicit retained snapshot contract
(`Immutable: true` with a revision) for a custom attribute to reach an authored
shader. If the current renderer cannot safely preserve custom attributes on
the CPU-baked mutable path without a broad redesign, reject that unsupported
combination clearly and deterministically rather than silently dropping the
attribute. Preserve ordinary mutable/unindexed meshes exactly as before.

## Typed and wire contract

- Introduce the smallest public typed attribute value needed by
  `BufferGeometry`: a float value slice plus an item/component count. Support
  one through four components (`float`, `vec2`, `vec3`, `vec4`) only.
- Add an optional name-keyed custom-attribute collection to `BufferGeometry`
  and `MeshVertices`. Choose names and field shapes that read naturally in Go
  and serialize compactly and unambiguously.
- Custom names must be non-empty shader identifiers and must not collide with
  built-in position/normal/uv/tangent/index/skin streams. Define one canonical
  validation rule shared by tests.
- Every custom stream must contain exactly `Count * ItemSize` finite values.
  ItemSize outside `[1,4]`, malformed names, non-finite values, or wrong-length
  streams fail closed before serialization; never permit a partial GPU fetch.
- Copy map entries and data slices during lowering so later caller mutation
  cannot alter the IR snapshot. Do not rely on Go map iteration order for
  hashes, validation output, snapshots, or GPU slot assignment.
- Preserve the existing `Immutable`, `Revision`, `Dynamic`, indices, and
  built-in attribute contracts. No custom attributes means byte- and
  behavior-compatible output.

## Runtime and shader contract

- Normalize each valid wire custom stream once to `Float32Array`; do not
  recreate arrays each frame. Preserve it anywhere vertices are cloned,
  transformed, snapshotted, switched between backends, or hashed for plans.
- Extend Selena attribute resolution rather than adding a separate shader
  system. A descriptor attribute whose name is not built in resolves only
  against the mesh's declared custom attribute of the matching component
  count. Add correct `float32`, `float32x2`, `float32x3`, and `float32x4`
  WebGPU formats and matching WebGL `vertexAttribPointer` component counts.
- Attribute order and shader locations come from the Selena descriptor, not
  object-map iteration. Missing data, duplicate/invalid descriptor locations,
  or a descriptor/data component mismatch must fail the authored draw safely
  and observably; do not shift later locations or substitute unrelated data.
- WebGL2 must upload/bind custom streams for the Selena direct mesh path and
  reuse/retire their buffers under the existing direct-geometry lifecycle.
- WebGPU must include custom streams in the Selena vertex-buffer layout, bind
  them in matching slots, and reuse/rebuild/retire retained buffers under the
  existing revision lifecycle and telemetry.
- Indexed Selena meshes must continue to bind the uint32 index buffer and use
  `drawElements` / `drawIndexed`. Standard PBR and built-in-only Selena
  materials remain unchanged.
- Do not claim general glTF custom-semantic support in this slice. The target
  is typed `scene.BufferGeometry` flowing through the direct Scene3D path.

## Evidence required

Add focused deterministic tests that prove at minimum:

1. Go lowering and JSON preserve named scalar, vec2, vec3, and vec4 streams
   with their item sizes and exact values.
2. Lowered custom maps/data do not alias caller maps or slices.
3. malformed names, built-in collisions, item sizes, lengths, and non-finite
   data fail closed; no malformed vertex payload reaches JSON.
4. no-custom and existing indexed/unindexed BufferGeometry behavior is
   unchanged.
5. browser ingest creates stable `Float32Array` custom streams and preserves
   them across every relevant vertex clone/snapshot path.
6. WebGL2 Selena binds a custom scalar and vector stream at the descriptor's
   declared locations with the correct component counts, then performs the
   indexed draw; mismatch and missing-data controls do not draw partially.
7. WebGPU Selena creates matching float32 vertex formats, binds custom buffers
   in descriptor order, then performs the indexed draw; mismatch and
   missing-data controls fail safely.
8. an unchanged retained revision reuses custom GPU buffers without another
   upload, a revision change rebuilds them, and retirement destroys them.
9. planner/topology hashing is deterministic across different map insertion
   orders and changes when custom attribute schema or data identity changes as
   appropriate to the existing revision contract.
10. standard PBR and built-in-only Selena controls still render through their
    existing paths.

Use the repository's test helpers and canonical build scripts. Prefer the fewest
typed source files that close this contract. Run the focused checks you can
discover, plus:

```sh
gofmt -w <changed-go-files>
go test ./scene ./render/bundle
go vet ./scene ./render/bundle
git diff --check
```

If the harness denies shell execution, continue implementing and report the
exact commands an independent reviewer must run. Report every changed file,
test command, denial, and unresolved limitation.
