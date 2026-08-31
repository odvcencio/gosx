# Scene3D v1 support contract

This document defines the narrow Scene3D v1 boundary. It is a convergence
contract, not a claim of generic Three.js parity and not a claim that every
row is complete today. The executable source of truth is
`scene/harness/testdata/v1-corpus.json`; CI validates that manifest and every
evidence anchor with:

```sh
go test ./scene/harness -run '^TestV1CorpusContract' -count=1
```

The manifest uses two states. `targetBackends` names the backend closure target;
only an `enforced` row is a support claim, and every such target must be covered
by typed test evidence with an exact CI job, step, and command owner.

- `enforced`: automated evidence exists in a normal CI lane.
- `blocked`: the v1 requirement still needs implementation or executable
  end-to-end proof and therefore carries no CI-owned evidence claim. Scene3D
  v1 is not complete while any row is blocked.

## v1 boundary

Scene3D v1 targets common browser product viewers, configurators, simulation
dashboards, and interactive scenes. Its supported boundary is:

- WebGPU and WebGL2 consume the same SceneIR semantics. Capability fallback is
  allowed, but a backend must not silently render a scene it cannot represent.
- Typed scene graphs provide deterministic nesting and full translation,
  rotation, and scale semantics on leaves and groups, including matching
  raycast/pick behavior.
- Uncompressed glTF/GLB 2.0 supports single and multiple buffers, data URIs,
  same-origin external buffers, embedded images, bounded and sparse accessors,
  ordinary primitives, existing skin/morph support, and imported animation.
- Imported animation supports `LINEAR`, `STEP`, and `CUBICSPLINE` for
  translation, rotation, scale, and morph weights. The existing mixer
  play/stop/fade/loop/weight surface is the v1 action boundary.
- The currently tested standard material/texture subset is supported.
  Meshopt- and Draco-compressed geometry fails closed with named errors.
  `KHR_texture_basisu` is not silently advertised: renderer KTX2 block upload
  and glTF Basis import are separate browser capabilities. S4 owns asset-pipe
  normalization and this S0 row makes no asset-pipeline support claim.
- Desktop orbit/fly/first-person controls, pointer lock, picking, object drag,
  and transform-gizmo commits are the v1 interaction boundary.
- Supported browser post effects preserve declaration order. Native/headless
  custom-post behavior remains blocked until S5 provides backend-specific
  degradation and telemetry evidence; it is not claimed as browser parity.
- Hub updates either emit complete commands or reject a remount-required diff
  atomically. Generic hydration must not discard Scene3D command output.
- A corpus route must publish measured p95/p99 frame evidence and stay inside
  the existing JavaScript, network, WASM, and performance budgets.

## Executable corpus status

| Corpus ID | Current state | Closure needed |
| --- | --- | --- |
| `gltf-single-buffer-textured` | blocked | Add one end-to-end textured GLB case, not only isolated loader tests. |
| `gltf-multi-buffer-external` | enforced | Indexed data-URI and same-origin external buffers are tested. |
| `glb-bin-plus-external` | enforced | GLB BIN buffer 0 plus external buffer 1 is tested. |
| `gltf-sparse-embedded-image` | enforced | Sparse overlays and embedded bufferView images are tested. |
| `gltf-meshopt-rejection` | enforced | Meshopt input fails closed with a named error. |
| `gltf-draco-rejection` | enforced | Draco input fails closed with a named error. |
| `gltf-basis-ktx2-policy` | enforced | Basis import degradation and renderer KTX2 upload are separate browser facts; S4 owns asset-pipe policy. |
| `gltf-cubic-trs-morph` | enforced | Analytic fixture covers TRS, morph weights, clamps, seeks, native WebGL2 canvas pixels, and production WebGPU renderer pixels from the exact proof-private target. Actual WebGPU canvas presentation remains part of the release-pinned hardware completion obligation. |
| `nested-group-scale-pick` | blocked | Browser renderer proof and native unit coverage exist; require renderer-consumed native normals, winding/cull, and pick evidence before certification. |
| `desktop-controls-picking` | enforced | Orbit/first-person controls and picking stay inside the v1 boundary. |
| `desktop-gizmo-commit` | blocked | Add one end-to-end fly/pointer-lock/object-drag/gizmo commit proof. |
| `ordered-post-custom-uniforms` | blocked | Add one cross-backend order/uniform-patch proof before certifying this row. |
| `native-preview-degradation` | blocked | S5 must add backend-specific `CustomPost` degradation and telemetry evidence before this can be certified. |
| `hub-command-diff` | enforced | Diffable fields produce a lossless command stream. |
| `hub-remount-atomic-reject` | enforced | Remount fields reject before mutation or watcher notification. |
| `scene-p95-budget-route` | blocked | Add a dedicated corpus route and measured p95/p99 evidence. |
| `generic-adapter-command-envelope` | blocked | Generic hydration must return or apply Scene3D commands instead of discarding them. |

## Explicit non-goals

The following are outside the Scene3D v1 contract unless a later decision adds
their dependency, byte, and certification cost:

- generic Three.js feature parity or generic performance superiority;
- WebXR/XR;
- runtime meshopt, Draco, or BasisLZ transcoders (build-time normalization is
  permitted as a separate feature);
- IK, retargeting, broad animation state machines, CSG, NURBS, Studio, or a
  modeling kernel;
- advanced TAA, SSR, or motion-blur pipelines;
- full native visual parity or macOS/Linux native renderer parity;
- general mobile touch/pinch/gamepad Scene3D controls;
- alpha masks, SH light probes, custom vertex attributes, spot shadows, and
  point shadows as v1 completion requirements.

## Completion rule

Scene3D v1 is ready only when the manifest has no `blocked` rows, the named CI
lanes are green, budget changes are backed by measurements, and real WebGPU
hardware evidence exists for the release-pinned corpus. Passing the manifest
shape test alone proves that the contract is coherent; it does not certify the
blocked cases.
