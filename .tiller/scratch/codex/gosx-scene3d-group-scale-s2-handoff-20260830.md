# GoSX Scene3D S2 Group.Scale implementation handoff

Date: 2026-08-30

Verdict: the final independent rereview's remaining scale-policy defect is
repaired on top of the exact reviewed checkpoint. The candidate is green under
the focused affine/race/TinyGo/browser matrix and the complete Go, JavaScript,
generation, size, formatting, and release gates available on this ancestry. It
is ready for a fresh independent rereview. This is not landing approval: the
mandated base does not contain R0, and hardware-native renderer-consumed
evidence has not been produced. Both remain explicit post-replay integration
obligations.

## Post-rereview scale-policy correction

The correction started from exact HEAD
`d5eb19a1a557ce78c6afb298d4210d76a227f4d2`, tree
`969c30e190a84365ef569e847824ab930d4bd408`. The independent rereview was
`/home/draco/work/gosx/.tiller/scratch/codex/gosx-scene3d-group-scale-s2-final-independent-rereview-20260830.md`,
SHA-256
`0ec73ea505b84c4b2be24b5372cb15841cbf0596604fee873a34c8e9ed68b06d`.
The final correction commit follows this tracked report, so its exact commit
and tree are supplied in the parent handoff rather than recursively embedded
here.

### Root cause and repair

- The browser instanced picker applied the accepted affine inverse, then
  rejected transformed ray directions with an absolute squared-length cutoff
  (`a <= 1e-12`). A condition-number-one uniform scale of `1e6` therefore
  became unpickable, and `1e9` was rejected even though the declared validator
  accepts it.
- The picker now obtains the transformed direction length with `Math.hypot`,
  rejects only zero/non-finite lengths, normalizes the local direction for the
  quadratic, and divides the local root by that length. The resulting value is
  the original world-ray parameter, so world distance/point semantics and
  non-orthogonal shear correctness are preserved. Local hit coordinates use
  the normalized direction and local root.
- Go and browser affine inversion previously formed `determinant * scale`
  before taking its reciprocal. That intermediate overflows for a valid basis
  near `9e307`, manufacturing an all-zero inverse while the browser reported
  success. Both implementations now divide in stable order
  (`1 / determinant / scale`) and fail closed on a zero or non-finite
  reciprocal, non-finite output, or an all-zero linear inverse.
- Validation acceptance was not narrowed or forked. Go, typed/raw schema,
  strict browser schema, and VM still share the same finite affine-bottom-row
  and scale-normalized determinant policy. Singular, non-finite, and
  unrepresentable inverse results fail closed at the inverse consumer.

### Adversarial coverage

- The production JavaScript picker is exercised at the exact former `1e6`
  cutoff, at `1e9` with exact expected world parameter `1e9`, and at `1e-9`.
- Reflected/sheared bases at `1e9` and `1e-9` preserve analytic world
  parameters and local hit coordinates under the same production picker.
- Go, browser schema, strict JavaScript schema, and VM accept uniform,
  reflected/sheared large and small finite bases plus the valid near-MaxFloat
  basis.
- Go and JavaScript produce finite, nonzero inverse coefficients for the
  `9e307` basis (normalized determinant `2`). Singular, non-finite, inverse-
  coefficient-overflow, and inverse-translation-overflow cases fail closed.
- The native Chrome harness runs the same production API for both the WebGL2
  and WebGPU affine cases and records the selected browser and source commit in
  `report.json`.

### Fresh browser receipt

Pre-commit receipt:
`/tmp/gosx-s2-affine-scale-policy-precommit.xeGJa1/report.json`, SHA-256
`bb7a49d149cd494a6682b982530ea247926ff1e714196ec8ad59396caa19c8bb`.
It records Chrome `143.0.7499.169` revision
`@164b20aab62509dad21fd46383951aeec084ad1e`, native WebGL2 and WebGPU,
zero errors/warnings/404s/unexpected requests/network failures, and successful
GL2/WebGPU affine plus CUBICSPLINE cases. Both affine backends record:

- former-cutoff hit distance `1000000`;
- `1e9` hit distance `999999999.9999999` and local `z=1`;
- small hit distance `1.0000000282819316e-9`;
- reflected/sheared large hit `2236067977.49979` against analytic
  `2236067977.4997897`;
- reflected/sheared small hit `2.236067978676758e-9` against analytic
  `2.2360679774997897e-9`;
- near-MaxFloat determinant `2` with nonzero subnormal inverse coefficients;
- singular and inverse-overflow results `0`.

The WebGL affine case records one retained draw and no world-baked vertices.
The WebGPU affine case records two passes, two submits, executed bundle `1`,
one retained object, and no world-baked vertices. One earlier fresh-context
attempt is preserved at
`/tmp/gosx-s2-affine-scale-policy-precommit.MCQYwP/report.json`: its scale
receipts were green, but the first WebGPU feature-chunk request alone was
canceled by Chrome while the later WebGPU case loaded and ran. A subsequent
fresh-context run above had no network failure; no source change or weakened
assertion separated the two attempts.

### Correction verification

| Command / evidence | Result |
| --- | --- |
| `GOWORK=off go test -count=1 ./internal/sceneaffine ./scene ./scene/schema ./client/vm ./render/bundle ./render/gpu/headless ./scene/harness ./scene/capability` | PASS |
| Same touched package set with `-race -count=1` | PASS |
| `GOWORK=off go test -count=1 ./cmd/gosx -run 'TestTinyGoWASMDependencyClosure(PrunesHostShaderCompiler\|ExcludesGoTreeSitterAndGob)$'` | PASS |
| `GOWORK=off go test -count=1 ./scene/harness -run '^TestV1CorpusContract$'` | PASS |
| Focused affine/pick Node suite | PASS, 27/27 |
| Complete client/runtime Node suite | PASS, 1,505/1,505 |
| `cmd/buildbootstrap` tagged test and `--check` | PASS; deterministic |
| TypeScript 5.9.3 typecheck | PASS |
| Focused `go vet` and `make fmt-check` | PASS; 81 files verified |
| `client/js/bootstrap-size.test.mjs` | PASS, 4/4 |
| `make release-gate` | PASS, all five aggregate checks |
| `go test -count=1 ./...` in the repository's normal environment | PASS; `cmd/gosx` 434.316s, `perf/ouroboros` 541.001s |
| Native Chrome WebGL2/WebGPU affine+CUBICSPLINE proof | PASS; exact receipt above |

An exploratory full run with `GOWORK=off` was not counted as an aggregate
result: it predictably made untouched
`TestVersionSkewResolveProjectWorkspace` ignore the `go.work` that the test
creates. The exact test passed under the normal environment, followed by the
complete green normal-environment run above.

### Unchanged budgets and generated identity

No cap, exception, manifest, workflow, governance, or dependency file changed.

- Production non-test Go versus `e920d5d5`: 848 additions, 251 deletions, net
  `+597` against the unchanged `+700` ceiling (103 lines spare).
- Authored production bootstrap/runtime TypeScript: 513 additions, 762
  deletions, net `-249` against the unchanged `+250` ceiling.
- `client/runtime/scene3d/mount-webgl.ts`: 4,797 lines against 4,879.
- Minimal WebGPU route: 1,086,044 raw / 287,031 gzip / 240,453 Brotli against
  unchanged 1,090,039 / 287,220 / 240,514 caps (3,995 / 189 / 61 spare).
- Full generated inventory: 64 paths, 17,813,033 bytes, digest
  `0282666e5ede7c9c68492ffb16a52fd8324adc08dc68a74193a00ef344771696`.
- Exactly eight generated outputs changed from `d5eb19a1`: the `.js`, `.gz`,
  `.br`, and `.map` siblings of `bootstrap-feature-scene3d` and `bootstrap`.
  Their aggregate digest is
  `19f5e106a4ea967c96d047c235efdd1b86e8e66a28d2faa7ad0c5ce0dcfdbe2c`.

## Provenance and exact tree

- Mandated base: e920d5d5ad84dba99303f2bfe378323164820517
- Original S2 implementation: af63060ef4d019e67149e284d4fbf04e7f08b1d5
- Original handoff: 4ace1ceb88a92fb74281f929987639012deb18c6
- Review-resolution code head:
  fc088426b02981ab3dcc01ccf62336db43b7a882
- Branch: codex/scene3d-group-scale-s2-20260830
- Isolated worktree:
  /home/draco/worktrees/codex-gosx-scene3d-group-scale-s2-20260830
- Canonical checkout /home/draco/work/gosx was not modified.
- No push, pull request, merge, or other remote mutation was performed.

Review-resolution commit fc088426 changes 78 files with 1,656 insertions and
929 deletions. The complete base-to-code tree changes 91 files with 3,502
insertions and 1,189 deletions. The documentation commit containing this
report follows the code head; its exact final HEAD is supplied in the parent
handoff message so the report does not contain a recursive self-hash.

## Independent-review resolution

### 1. InstancedMesh affine consumers

- Browser CPU picking now transforms rays with one stable exact affine inverse.
  It no longer projects independently onto matrix columns, so non-orthogonal
  shear is exact.
- Browser compute, native bundle, headless, and capability culling use the
  Frobenius norm of the linear 3x3 as a conservative upper bound on the largest
  singular value. Sheared objects can be over-retained but cannot be
  incorrectly rejected by the old max-column under-bound.
- WebGL and both WebGPU instanced vertex variants use inverse-transpose normal
  and tangent transforms.
- Native preview/bundle shaders use the same inverse-transpose convention.
- Adversarial tests pin the review matrix whose exact hit was previously
  missed, a shear whose true radius exceeds every column norm, shader
  mutation guards, and native/headless cull retention.

### 2. Retained/direct affine shading

- Retained WebGL, WebGPU, Selena, skinned, and morph paths use shared affine
  normal helpers rather than normalized model columns.
- WebGPU object uniforms carry the affine normal matrix derived from the exact
  model matrix.
- Defensive shader helpers use a neutral identity normal basis for a singular
  matrix; valid Scene3D documents cannot reach that path because singular and
  non-finite parent matrices are rejected at every boundary.
- Source-contract tests mutate the helper or restore the old column-normalize
  formulas and fail.

### 3. Reflection, winding, culling, and tangent handedness

- Baked geometry reverses every complete triangle and flips tangent.w when the
  applied affine determinant is negative. Unindexed triangles receive
  determinant-correct indices.
- Static, dynamic, and computed-pose glTF baking follow the same convention.
- Reflected WebGL retained/direct draws switch frontFace to CW for the draw and
  restore CCW afterward.
- Reflected WebGPU retained/direct draws select a CW front-face pipeline
  variant. Existing double-sided cullMode none behavior remains unchanged.
- Native bundle paths classify determinants, use inverse-transpose normals,
  and apply determinant-aware winding/cull handling. Mixed-sign instances use
  the shader classifier rather than one incorrect batch-wide sign.
- Analytic tests prove the reflected face normal and tangent frame agree, and
  mutation guards reject removal of the CW selection.

### 4. Supported glTF POINTS and LINES

- Modes 0, 1, 2, and 3 retain the exact affine parent matrix rather than
  reducing it to scalar scale plus an origin transform.
- Initial mount and animated updates carry identical parent-before-live-local
  semantics into the Points and Lines consumers.
- Tests use a hard-coded sheared matrix, cover all four modes, assert initial
  and animated world positions, and require the fresh upload path.

### 5. One strict affine validation and conditioning contract

- internal/sceneaffine owns the runtime-safe validator shared by the public
  scene wrapper and client VM.
- The contract is exactly 16 finite float64 values, a column-major affine
  bottom row of [0, 0, 0, 1], and a scale-normalized absolute 3x3 determinant
  greater than 1e-12.
- Uniformly tiny and uniformly large invertible transforms remain valid;
  singular, projective, ill-conditioned, non-finite, wrong-length, string, and
  filtered/shifted inputs are rejected.
- Go IR, typed/raw schema validation, JSON schema, strict browser validation,
  runtime normalization, VM parsing, and public scene.ValidParentMatrix use
  the same semantics.
- CPU inversion is scale-normalized. Invalid or singular defensive inputs
  return no inverse/no pick; shader-only defensive normal handling is neutral.

Dependency evidence:

- go list -deps ./internal/sceneaffine contains only unsafe, internal/cpu,
  math/bits, math, and m31labs.dev/gosx/internal/sceneaffine.
- client/vm imports internal/sceneaffine directly and does not import the host
  scene package or gotreesitter. Its pre-existing scene/earcut and scene/geom
  leaf dependencies remain.
- scene.ValidParentMatrix is a direct wrapper over the same validator.

### 6. Evidence and corpus honesty

- The browser fixture is affine-only: its draw/pass/pick counters, renderer
  buffers, model matrix, pipeline/front-face evidence, visible pixels, and
  canvas selection all belong to the same reflected/sheared affine mesh.
- Assertions require retained local geometry and fail on world-baked fallback.
- The WebGPU proof links drawIndexed to the encoded and executed renderer-owned
  bundle as well as the local position and material buffers.
- Expected matrix, transformed positions, normal, reflection, and canvas-pick
  values are analytic/hard-coded rather than computed by the production helper
  under test.
- nested-group-scale-pick is returned to blocked in the v1 corpus, with
  premature renderer/native evidence and owner claims removed. It must not be
  promoted until the post-replay hardware-native proof exists.

## Two regressions caught during the full-gate pass

### TinyGo dependency-edge regression

The first shared-validation implementation imported the public host scene
package from client/vm. The two TinyGo closure tests then correctly reported
that the WASM closure contained m31labs.dev/gosx/scene and the transitive
github.com/odvcencio/gotreesitter module.

This was causally baseline-proved in an archived checkout at
/tmp/gosx-s2-baseline-4ace.X7n03G:

- The identical pair
  TestTinyGoWASMDependencyClosurePrunesHostShaderCompiler and
  TestTinyGoWASMDependencyClosureExcludesGoTreeSitterAndGob passed at
  4ace1ceb in 0.087 seconds.
- The modified tree failed both predicates before the extraction.
- Moving only the validator into internal/sceneaffine and having both scene and
  client/vm depend on that leaf made the same pair pass in 0.085 seconds.

This is a dependency-edge correction, not a weakened test or an added
exemption. cmd/buildbootstrap/go.mod remained byte-clean throughout.

### GLSL version regression

The first WebGL affine-normal helper used determinant, inverse, and transpose
built-ins valid in GLSL 300 but not in the retained Selena GLSL 100 vertex
path. A real glslangValidator compile exposed the error. The helper now uses
cofactor/cross-product math accepted by both shader versions; the extracted
GLSL validation and source-contract tests pass. No renderer path was disabled.

## Exact Chrome affine proof receipt

Receipt:
/tmp/gosx-s2-affine-final.Em0J9a/report.json

The run used fresh native Chrome contexts. It recorded zero errors, warnings,
404s, unexpected requests, or network failures. Native capabilities were
webgl2=true and webgpu=true. Both cases disposed successfully.

The renderer-consumed reflected/sheared model matrix in both backends was:

[-1.4142135381698608, 0.7071067690849304, 0, 0,
  1.4142135381698608, 0.7071067690849304, 0, 0,
  0, 0, 1, 0,
  0.5, 0.5, 1, 1]

WebGL evidence:

- renderer=webgl, fallback=null, mounted=true
- renderer-owned retained drawElements
- local position buffer values linked to that draw
- worldMeshVertexCount=0 and cacheEntries=1, proving no world-baked fallback
- frontFace=2304, the WebGL CW enum; the earlier incorrect 2305/CCW state is
  gone
- real canvas select picked affine-group-child
- pick world/local point [0.5, 0.5, 1], depth=2
- visible pixels=5,940, maximum pixel delta=148

WebGPU evidence:

- renderer=webgpu, fallback=null, mounted=true
- renderer-owned retained drawIndexed
- the draw is encoded in bundle ID 1 and executedBundles=[1]
- linked local position buffer and material buffer
- pipeline frontFace=cw and cullMode=none
- worldMeshVertexCount=0, retainedMeshObjects=1, bundleState=encoded, and
  cacheEntries=1
- passes=2 and submits=2
- the same real canvas selection hit and world/local point
- visible pixels=5,940, maximum pixel delta=219

This receipt closes the three concrete blockers seen in the earlier
/tmp/gosx-affine-debug3.6tOXOU report: WebGL CCW front-face state, null canvas
picks, and missing WebGPU retained-draw linkage.

## Authored-code, line, bundle, and route budgets

All ceilings are unchanged.

- Production non-test Go versus the base: 836 additions, 251 deletions, net
  +585 against the +700 ceiling; 115 lines of headroom.
- Authored production bootstrap/runtime TS/JS: 504 additions, 758 deletions,
  net -254 against the +250 ceiling.
- client/runtime/scene3d/mount-webgl.ts: 4,797 lines against the unchanged
  4,879-line ceiling; 82 lines of headroom.
- No bundle, route, line, source, complexity, ownership, or exception cap was
  raised.

Minimal WebGPU route with runtime, scene3d, and webgpu:

| Encoding | Actual | Existing cap | Headroom |
| --- | ---: | ---: | ---: |
| Raw | 1,085,926 | 1,090,039 | 4,113 |
| Gzip | 286,988 | 287,220 | 232 |
| Brotli | 240,467 | 240,514 | 47 |

Generated bundle sizes:

| Bundle | Raw | Gzip | Brotli | Existing cap raw/gzip/br |
| --- | ---: | ---: | ---: | --- |
| bootstrap.js | 1,573,915 | 428,233 | 344,105 | 1,578,700 / 429,000 / 344,600 |
| bootstrap-runtime.js | 148,446 | 40,508 | 35,307 | 149,000 / 41,000 / 35,800 |
| bootstrap-feature-scene3d.js | 545,028 | 150,997 | 125,268 | 547,700 / 151,450 / 125,600 |
| bootstrap-feature-scene3d-compute.js | 31,211 | 9,351 | 8,411 | 32,000 / 9,500 / 8,600 |
| bootstrap-feature-scene3d-gltf.js | 44,506 | 15,920 | 14,157 | 45,000 / 16,200 / 14,420 |
| bootstrap-feature-scene3d-webgl.js | 219,701 | 60,586 | 51,422 | 220,750 / 60,800 / 51,600 |
| bootstrap-feature-scene3d-webgpu.js | 392,452 | 95,483 | 79,892 | 394,066 / 95,596 / 80,055 |

The smallest remaining measured margin is the minimal-route Brotli margin of
47 bytes. This is green but should be watched during replay.

## Generated inventory and reproducibility

The full current generated inventory contains 64 paths and 17,811,394 bytes.
Exactly 28 outputs differ from the direct e920d5d5 base because the review
resolution touches shared runtime, compute, glTF, WebGL, and WebGPU sources.
Those changed outputs total 15,875,590 bytes.

- Full current aggregate digest:
  b28abb758e8a1f7d87362109f5c4b5518dd3785401f65a0b11e091963ad94a1e
- Changed-output aggregate digest:
  7c8ee4f8194e60d944bd9cdc268b8a3181da8dcd7d1c3322fbde26732a039d39
- Direct-base full inventory digest:
  f3d1ab45bd0e3dcffcbc2c11f5a614ad3d8198c64b0b2de53df1978ef2d40d55
- Direct-base bytes for the same 28 paths: 15,934,060; current is 58,470
  bytes smaller.
- Prior 4ace1ceb bytes for the same 28 paths: 15,904,557; current is 28,967
  bytes smaller.

Digest algorithm: sort paths bytewise; for each path append path, NUL, the
lowercase SHA-256 hex digest of the file bytes, NUL; then SHA-256 the resulting
stream.

| Generated output | Bytes | SHA-256 |
| --- | ---: | --- |
| client/js/bootstrap-feature-scene3d-compute.js | 31,211 | c44c37761ed04fa6bcd9760a9814eb21ed4158f7158d4c42d9e6e2a6080bcd9a |
| client/js/bootstrap-feature-scene3d-compute.js.br | 8,411 | 611443d77c02a4b60bc052f30b5e909d2361dd93395fc44080d1a42288745984 |
| client/js/bootstrap-feature-scene3d-compute.js.gz | 9,351 | cda60a78d072ab9ca69e3b5f4ea7e0b175758189150e002e26a0218d8ddde364 |
| client/js/bootstrap-feature-scene3d-compute.js.map | 115,867 | 01dd9e466ad68f8b059c41715f92dad1d71141c10b3b0662acc011cf102b75b5 |
| client/js/bootstrap-feature-scene3d-gltf.js | 44,506 | 6840c4b5eab79daf617373c07fe5b7f1fe754d64684ab76cf7e5eee59974a1d3 |
| client/js/bootstrap-feature-scene3d-gltf.js.br | 14,157 | 650f15298a1b2a0d9be5ed01e86b4b81f144eedaea7c59f512f33f021b82201f |
| client/js/bootstrap-feature-scene3d-gltf.js.gz | 15,920 | f4287221f97513fd8762af7895bfc878c9ec7a0390c5e50e1173ea1090c94c40 |
| client/js/bootstrap-feature-scene3d-gltf.js.map | 228,820 | d7a7b448c6227bb5eca69d52b2e2d9186f0bc8f6805d04d021702fa87d3f1285 |
| client/js/bootstrap-feature-scene3d-webgl.js | 219,701 | 989fe0347a504bccf6f19ae47447cab818242ad7d57e85e2aa502ac004539a97 |
| client/js/bootstrap-feature-scene3d-webgl.js.br | 51,422 | d048f2d8f3e3658926248d39fc3869fc1d8121489ade8bd617399030f866ee5c |
| client/js/bootstrap-feature-scene3d-webgl.js.gz | 60,586 | 12552dbec89a9005ceea38916847b8cfed0cedc73daf9c5b70dd7383f5d1b9a0 |
| client/js/bootstrap-feature-scene3d-webgl.js.map | 827,434 | 8ed8a32a2b23e5fb7ba0a00a73b205e664b904864e28c3f8d3bb83c0d4969a84 |
| client/js/bootstrap-feature-scene3d-webgpu.js | 392,452 | 2d0b64a7f7ee4b42d179746a90196c0a9c35fd7b058b330c0fc1fed76c4936ee |
| client/js/bootstrap-feature-scene3d-webgpu.js.br | 79,892 | bf65e78a203d122d9464311b905e488fe6ee9fcbfdf68aa90f4cdf59dbac48e5 |
| client/js/bootstrap-feature-scene3d-webgpu.js.gz | 95,483 | 7f806c5da7e70073015d7c6c5480a43c98135c81cf90014635b52a8835509109 |
| client/js/bootstrap-feature-scene3d-webgpu.js.map | 1,391,277 | a36156b522e2f91e06853bf4d957895241f133cc8494b852141d7787594e33ea |
| client/js/bootstrap-feature-scene3d.js | 545,028 | 35c2c26efc58a4c893422a2d31646c377e18e423501f885de7523e4db050ff67 |
| client/js/bootstrap-feature-scene3d.js.br | 125,268 | bb2d7457dfc05ca82bbb3b62adf1115e3f9c36dac38d516830da713c42afd26a |
| client/js/bootstrap-feature-scene3d.js.gz | 150,997 | dac3fb1be62961332d255f0b8a663cb04b6e4db099899c9449a088440ffe64c5 |
| client/js/bootstrap-feature-scene3d.js.map | 2,270,277 | 9dd7ed06940e9e010d5af287e14779a5610330b3ddac9858d00391d465bd22b9 |
| client/js/bootstrap-runtime.js | 148,446 | 27c259bdc1101f7b73c6454c54b788089d7d3580a6143ec24f439520cde47f1d |
| client/js/bootstrap-runtime.js.br | 35,307 | a626fd24556da4c4947f56a62ef290b7d5173a7fe22e5a8cac25468953a49e4d |
| client/js/bootstrap-runtime.js.gz | 40,508 | 732cdb6f8e7751231c65c831bd3bd4d8ad4365336318ba2d8cee550ad9e15e51 |
| client/js/bootstrap-runtime.js.map | 532,027 | 11f7d2f95f5f398cf8f3f915c010b2e0c6fa22d2452fad9fda336840aad266cc |
| client/js/bootstrap.js | 1,573,915 | b3bf75be10094ea0b2b18b9a3d55ccafb57bbb69b4ffe4794741411d6338924f |
| client/js/bootstrap.js.br | 344,105 | 0624ec0f3edb4920b9b298186ec852a579119f334f9771cd24b3c906417072a2 |
| client/js/bootstrap.js.gz | 428,233 | 3f4ddd878a14be7cd7a26d045286ad4c7af4efaaf033b4c504eb441f3d81637f |
| client/js/bootstrap.js.map | 6,094,989 | d3c32606831742f53ca722c199a34fc9414c8c81da398e08cefbc44414aad094 |

cmd/buildbootstrap --check passed after final shader changes. No unexpected
generated path changed, client/js/chunks.json is unchanged, and
cmd/buildbootstrap/go.mod is unchanged.

## Verification evidence

| Evidence | Result |
| --- | --- |
| go test -race ./internal/sceneaffine ./scene ./scene/schema ./client/vm ./render/bundle ./render/gpu/headless ./scene/harness ./scene/capability | PASS |
| go test ./... | PASS; cmd/gosx 437.978s and perf/ouroboros 554.184s |
| make test-unit | PASS; partition total 186, unit 185, CLI 1; perf/ouroboros 423.561s |
| go vet ./internal/sceneaffine ./scene/... ./client/vm ./render/bundle ./render/gpu/headless ./cmd/gosx | PASS |
| Complete Node client/runtime test set | PASS; 1,503/1,503 in 11.168s |
| TypeScript 5.9.3 project typecheck | PASS |
| GOWORK=off go test with grammar_subset and grammar_subset_typescript in cmd/buildbootstrap | PASS; 14.021s |
| GOWORK=off go run with grammar_subset and grammar_subset_typescript . --check | PASS; deterministic |
| client/js/bootstrap-size.test.mjs | PASS; 4/4 including chunk closure |
| Native Chrome WebGL2/WebGPU affine proof | PASS; exact receipt above |
| glslangValidator compile of shared WebGL/Selena affine-normal source | PASS |
| make fmt-check | PASS; 81 files verified |
| git diff --check | PASS |
| go test ./scene/harness -run TestV1CorpusContract -count=1 | PASS |
| make release-gate | PASS; all 5 aggregate checks |

The full Node suite used
/home/draco/.vscode-server/bin/a5b500951314efd502d07465bd138dfbd714a960/node.
All substantive make test-js constituents passed even though npm/node were not
on the ordinary shell PATH.

The existing 1,000-descendant benchmark remains covered. Three prior direct-base
runs measured lower at 533,735 to 573,373 ns/op with 1,014 allocations and
marshal at 1,843,246 to 1,880,864 ns/op with 2,009 allocations. The scaled wire
delta is 51,898 bytes for 1,000 descendants and carries one parent matrix per
scaled descendant.

## Remaining risks and integration obligations

1. R0 is absent, not passed. The dispatch mandated direct base e920d5d5, which
   does not contain R0 commit 343c53e30221384a639cacc13884e1c7508533d3.
   Therefore the R0 source-role, exact-path, ownership, function, complexity,
   line, artifact, and Go cross-check harnesses do not exist in this worktree
   and were not run. This must be reported as a post-replay obligation, never
   as a local pass.
2. Hardware-native renderer proof remains. Native shader, determinant, winding,
   culling, preview, VM, headless, and drift tests are green, but no
   hardware-native renderer-consumed visible-face receipt was produced here.
   The corpus row correctly remains blocked until that proof exists.
3. The minimal-route Brotli margin is only 47 bytes. Replay must remeasure
   generated artifacts and route totals without raising caps.
4. Explicit zero-axis collapse remains outside the existing omitempty Group
   scale contract: a zero component resolves to one.
5. The native preview loader still does not render Model or InstancedGLB assets.
   Their matrices and browser paths are covered, but hardware-native coverage
   should include the supported native asset subset honestly.

## Replay and landing sequence

1. Construct reviewed ancestry in order: governed main
   84104d311962094bdd8ed30fde2c1f5ac6a5b7fb, R0
   343c53e30221384a639cacc13884e1c7508533d3, S0+S1
   e920d5d5ad84dba99303f2bfe378323164820517, then the three S2 commits ending
   with fc088426 and this handoff report.
2. Resolve integration overlaps without weakening R0 registries or raising
   budgets to self-approve protected-source changes.
3. Run every R0 inventory/cross-check, the literal make test-js wrapper, full
   Go/race/unit, generation, size, Chrome WebGL2/WebGPU, hardware-native
   renderer proof, corpus, fmt/vet, and release gates on the replayed ancestry.
4. Promote nested-group-scale-pick only after renderer-owned evidence is
   complete for every claimed backend.
5. Recompute the 64-path digest on the integrated ancestry; compare against its
   actual base rather than blindly expecting this direct-base digest.

No remote action is included in this handoff.
