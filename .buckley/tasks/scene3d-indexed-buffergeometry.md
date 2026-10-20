# Task: preserve indexed BufferGeometry through Scene3D rendering

You are the acting implementation agent. This is the first P0 geometry slice in
the GoSX Three.js-subsumption roadmap: stop expanding indexed BufferGeometry
into triangle soup before it reaches the browser. Inspect the repository,
implement the smallest coherent vertical slice, run focused checks, and leave
the working tree uncommitted for independent review. Do not commit, push, edit
generated/minified bundles, add dependencies, or broaden into geometry groups,
custom attributes, morph targets, or a general BufferGeometry redesign.

Repository: GoSX, MIT licensed. Work from the typed source of truth. JavaScript
runtime sources in this repository are TypeScript; do not introduce a new
hand-written JavaScript implementation. Existing JavaScript test files may be
edited only when that is the established test surface.

## Outcome

For a valid indexed `scene.BufferGeometry`, preserve the unique vertex streams
and the authored triangle indices in `ObjectIR.Vertices`. Both WebGL2 and WebGPU
must upload an index buffer and issue indexed draws for the direct-vertex mesh
path, including standard PBR, Selena, and shadow rendering where those paths
already draw the geometry. CPU picking must traverse the same index order and
continue to report the authored triangle/primitive index and interpolated UV.

Unindexed BufferGeometry and legacy/generated non-direct mesh paths must remain
byte- and behavior-compatible unless a wire-shape extension is required.

## Wire contract

- Extend `MeshVertices` with an optional JSON `indices` stream. Use an integer
  representation that converts losslessly to a browser `Uint32Array`.
- When indices are absent, `Count` keeps its current meaning and all existing
  non-indexed behavior remains unchanged.
- When indices are valid, `Count` is the unique position vertex count; draw
  count is `len(indices)`. Copy positions, normals, UVs, and tangents once
  without index expansion, and copy indices without aliasing caller slices.
- Valid indexed geometry has a non-empty index count divisible by three and
  every index in `[0, Count)`. Fail closed for malformed indexed geometry: do
  not serialize or draw a partial mesh, and never let malformed input panic or
  reach an out-of-bounds GPU fetch.
- Preserve `Immutable`, `Revision`, and `Dynamic` semantics. Retained index
  buffers must participate in the same revision invalidation, reuse,
  telemetry, and retirement rules as retained vertex buffers.

## Runtime contract

- Normalize the optional wire indices once to `Uint32Array`; avoid per-frame
  expansion and per-frame index-buffer allocation for unchanged retained
  geometry.
- Copy indices anywhere the runtime clones or instantiates `vertices`, including
  model-local snapshots, so model transforms, animation, and backend switching
  do not silently drop topology.
- Keep vertex attribute addressing unique-vertex based. Draw using
  `drawElements(..., UNSIGNED_INT, ...)` in WebGL2 and
  `setIndexBuffer(..., "uint32")` + `drawIndexed(...)` in WebGPU when indices
  are present; keep existing `drawArrays` / `draw` calls otherwise.
- Cover the standard PBR, Selena, and shadow paths that consume direct vertices.
  Do not claim indexed support by fixing only one material/backend path.
- Update direct CPU exact picking to dereference indices while preserving
  authored triangle order, primitive index, hit point, and UV interpolation.
- Wireframe/selection fallbacks must either dereference indices correctly or
  deliberately take the existing safe non-retained path. No silent missing
  edges.
- Do not change glTF loader topology in this slice; it has its own indexed asset
  pipeline and is out of scope.

## Evidence required

Add focused deterministic tests that prove at minimum:

1. Go lowering and JSON preserve four unique vertices plus six indices for a
   two-triangle quad instead of expanding to six vertices.
2. Source-slice mutation after lowering cannot mutate the IR snapshot.
3. negative, out-of-range, and non-triangle index streams fail closed.
4. unindexed lowering is unchanged.
5. browser bundle construction keeps `Uint32Array` indices and unique vertex
   count through direct and retained object paths.
6. WebGL2 standard PBR, Selena, and shadow paths bind an element buffer and call
   `drawElements`; unindexed controls still call `drawArrays`.
7. WebGPU standard PBR, Selena, and shadow paths set the uint32 index buffer and
   call `drawIndexed`; unindexed controls still call `draw`.
8. retained index buffers are reused without upload on stable revision, rebuilt
   on revision change, and retired with their vertex buffers.
9. exact browser CPU picking hits both triangles of an indexed quad with the
   expected primitive index and interpolated UV, and an out-of-range pick
   misses.

Use the repository's existing test helpers and build scripts. Prefer changing
the fewest typed source files that close the end-to-end contract. Run the
focused Go and JavaScript/TypeScript checks you discover, plus:

```sh
gofmt -w <changed-go-files>
go test ./scene ./render/bundle
go vet ./scene ./render/bundle
git diff --check
```

Report every changed file, test command, denial, and unresolved limitation.
