# GoSX Scene3D S2 Group.Scale implementation handoff

Date: 2026-08-30

Verdict: the bounded S2 implementation is complete and green on the explicitly
mandated direct S0+S1 base. It is ready for integration review. It is not a
landing approval until the R0-governed ancestry is constructed and the R0-only
source-role, ownership, function, complexity, line, artifact, and Go
cross-check gates are rerun there.

## Provenance and repository state

- Base: e920d5d5ad84dba99303f2bfe378323164820517
- Implementation commit: af63060ef4d019e67149e284d4fbf04e7f08b1d5
- Branch: codex/scene3d-group-scale-s2-20260830
- Isolated worktree:
  /home/draco/worktrees/codex-gosx-scene3d-group-scale-s2-20260830
- Canonical checkout /home/draco/work/gosx was not modified.
- No push, pull request, merge, or other remote mutation was performed.

The dispatch required the worktree to be rooted directly at e920d5d5. That
instruction intentionally differs from the readiness report's preferred
landing ancestry of governed main 84104d31, R0 343c53e3, and then S0+S1
e920d5d5. The direct base does not contain the R0-only governance harnesses.

## Delivered contract

- Group has a Scale field with the existing per-component default: each zero
  component resolves to one; finite non-zero and negative values are retained.
  Explicit zero-axis collapse remains outside the existing omitempty contract.
- A descendant of a non-unit scaled Group carries one optional, exact,
  column-major 16-float parentMatrix. It maps the leaf's local, post-motion TRS
  result into world space.
- Group composition is exact parent * translation * rotation * scale. A
  non-uniform parent scale followed by a rotated child therefore retains shear
  instead of attempting an invalid TRS decomposition.
- Leaves keep authored local TRS, spin, drift, transition, and clip behavior.
  Mesh.Scale remains leaf-only and does not begin propagating to Mesh children.
- Scale-free group chains stay on the legacy scalar lowering path. The tests
  prove byte-identical serialization for the scale-free scene fixture.
- Object, model, points, ordinary instances, GLB instances, compute emitter,
  light, and anchor policies are explicit. InstancedMesh matrices are
  premultiplied rather than decomposed. Positional values use affine point
  transforms; directional values use the linear 3x3 and normalize; billboard
  pixel and DOM sizes remain leaf-authored.
- Go walk raycast and BVH use the same affine inverse, forward-point,
  inverse-transpose-normal, determinant, and conservative Frobenius-bound
  utilities.
- Native preview, native VM/render bundles, browser geometry, lighting,
  picking, glTF model roots, and both Points renderer backends consume the same
  parent-before-live-local matrix semantics.
- Wire, canonical IR, schema, clone/ownership, compatibility conversion, and
  transform diff set/change/explicit-null-reset semantics are closed together.

The strict browser document validator accepts an absent field but rejects
explicit null, wrong length, non-number, and non-finite entries. The command
patch protocol separately accepts null as the required reset operation.

## Change and authored-code budgets

Implementation commit versus the base:

- 50 files changed
- 1,746 insertions and 394 deletions
- Authored production Go/API/IR: 627 additions, 202 deletions, net +425
  against the +700 ceiling
- Authored production TS/JS: 161 additions, 180 deletions, net -19 against the
  +250 ceiling
- client/runtime/scene3d/mount-webgl.ts: 4,857 lines against the unchanged
  4,879-line ceiling
- No source, bundle, route, line, complexity, or exception budget was raised

The main adversarial Go test file is 461 lines and covers byte identity,
zero/default behavior, exact shear and nested composition, leaf-only
Mesh.Scale, the spatial-node roster, analytic and randomized picker parity,
canonical/compatibility ownership, deep hierarchies, diff behavior, and
1,000-descendant benchmarks.

## Generated inventory and reproducibility

Exactly 16 generated outputs changed because live Points transforms under a
sheared parent required edits to the actual WebGL and WebGPU feature sources:

| Generated output | Bytes | SHA-256 |
| --- | ---: | --- |
| client/js/bootstrap.js | 1,576,475 | 03c48dcc3cd519c8793c4f924ba193ec2fdefa2cbf374107335932f0b2b783c5 |
| client/js/bootstrap.js.br | 343,818 | 2575bd8b46803ab9949c1a7cf5c9469fe1c1ccf860d847061633ca33bc19b7f4 |
| client/js/bootstrap.js.gz | 428,297 | f71a071fcc4526f3bdd2407c554426ffbab3c0c0cf38864b285582b372064054 |
| client/js/bootstrap.js.map | 6,107,185 | d821bd5ac75033ac677d384b9c19df10ac6c5e5caa6156bdfa0b64da4398d9ae |
| client/js/bootstrap-feature-scene3d.js | 546,964 | 889cd125853a099b49b75bfead9160f614698bca98682ef706d5d766241a3a57 |
| client/js/bootstrap-feature-scene3d.js.br | 125,514 | c632275f390873fa4481eadfef4fa1fbfb9ad838cb36680eb7a4e7d19c9b2d4e |
| client/js/bootstrap-feature-scene3d.js.gz | 151,372 | cfc72c0f4b9175830e042680eae0a90cff9760bab4f56e88b4ab665a328b9ae9 |
| client/js/bootstrap-feature-scene3d.js.map | 2,279,652 | fbf5c8d86de5538054a413de2a35ba32449f37916595956afa6b7ba6b34fbab7 |
| client/js/bootstrap-feature-scene3d-webgl.js | 219,672 | 6bacc240c8296b9fd9007453ed1c608852160c7dba6e189af60d466ce8985aa8 |
| client/js/bootstrap-feature-scene3d-webgl.js.br | 51,259 | 2a05c5d373e5c11aff1deb84ee2354ad7964bd5077bfff59c37706601c899dba |
| client/js/bootstrap-feature-scene3d-webgl.js.gz | 60,450 | a353a616cf684a8d4e888b50cdd71d97c5bb6315451675719016b72bb2f121ad |
| client/js/bootstrap-feature-scene3d-webgl.js.map | 826,973 | 2a22394ebd1ff56ca6aa25741075950c48f4ed627cbf6b7a3e28cd36c1fab005 |
| client/js/bootstrap-feature-scene3d-webgpu.js | 392,396 | 61d2b64b01a69107abdfa6277222a06565150bc4e7db914acfd75f689d73a4a5 |
| client/js/bootstrap-feature-scene3d-webgpu.js.br | 79,416 | 58479359562a579176829dcb6b07be72ba93d9666386db4fe6bc42ba67a62c82 |
| client/js/bootstrap-feature-scene3d-webgpu.js.gz | 94,935 | 70d5ab2125c2dabd2a74792544d5b46325a143969e44616632a54e57c915da02 |
| client/js/bootstrap-feature-scene3d-webgpu.js.map | 1,389,420 | e30d66a742af547df56bbd1d0518dcc09aa1953d0adca7ddeedd8f11ca8a3455 |

The 16 changed artifacts total 14,673,798 bytes, down 29,503 bytes from their
14,703,301-byte base total.

- Changed-artifact aggregate digest:
  f960e31b138760a465b4956e695aa794e7c87508635a627a3e0e7ef4415abd36
- Full generated inventory: 64 paths, 17,840,361 bytes
- Full generated-inventory aggregate digest:
  b9532972407f6eef432b5c1ff944e076b6f6852f2467d931e789f5a9c9020a7f
- Direct e920d5d5 base inventory digest:
  f3d1ab45bd0e3dcffcbc2c11f5a614ad3d8198c64b0b2de53df1978ef2d40d55

Digest algorithm: sort paths bytewise; for each path append path, NUL, the
lowercase SHA-256 hex digest of the file bytes, NUL; then SHA-256 the resulting
stream. The readiness report's b67fdd0a baseline reproduces on the different
governed-main-plus-R0 ancestry, not on the dispatch-mandated e920d5d5 base.

The generator check passed after generation, so committed outputs are
byte-identical to the repository generator. client/js/chunks.json did not
change. The authored-only strict validator remains outside every bundle.

## Bundle and route-size evidence

| Bundle | Raw | Gzip | Brotli | Existing ceiling |
| --- | ---: | ---: | ---: | --- |
| bootstrap.js | 1,576,475 | 428,297 | 343,818 | 1,578,700 / 429,000 / 344,600 |
| bootstrap-feature-scene3d.js | 546,964 | 151,372 | 125,514 | 547,700 / 151,450 / 125,600 |
| bootstrap-feature-scene3d-webgl.js | 219,672 | 60,450 | 51,259 | unchanged |
| bootstrap-feature-scene3d-webgpu.js | 392,396 | 94,935 | 79,416 | unchanged |

The shared-math extraction more than paid for the affine additions. The
repository bootstrap size suite passed without changing a threshold.

## Verification evidence

| Evidence | Result |
| --- | --- |
| go test -race ./scene ./scene/schema ./scene/preview ./client/vm ./render/bundle -run 'GroupScale\|ParentMatrix\|TransformPatch' -count=3 | PASS |
| go test ./scene ./scene/schema ./scene/preview ./client/vm ./render/bundle ./scene/harness | PASS |
| make test-unit | PASS; 184-package non-CLI lane, including perf/ouroboros |
| Node test over ./client/js/*.test.js, ./client/js/*.test.mjs, and ./client/runtime/**/*.test.js | PASS; 1,498/1,498 |
| Focused final Node affine and Points-pick suites after strict-null tightening | PASS; 21/21 |
| TypeScript 5.9.3 typecheck of client/runtime/tsconfig.json | PASS |
| GOWORK=off go test -tags 'grammar_subset grammar_subset_typescript' ./... in cmd/buildbootstrap | PASS |
| GOWORK=off go run -tags 'grammar_subset grammar_subset_typescript' . --check in cmd/buildbootstrap | PASS |
| Node client/js/bootstrap-size.test.mjs | PASS; 4/4 |
| Native Chrome WebGL2 and WebGPU browser proof | PASS; exact affine matrix, vertices, pick oracle, real draws/submissions, restore/dispose; no errors, warnings, 404s, or unexpected requests |
| go vet ./scene/... ./client/vm ./render/bundle | PASS |
| make fmt-check | PASS; 81 files verified |
| git diff --check | PASS |
| go test ./scene/harness -run '^TestV1CorpusContract' -count=1 | PASS |
| make release-gate | PASS; all 5 aggregate checks |

The final browser artifact report is intentionally uncommitted at
/tmp/gosx-s2-browser-final.7kq3Xr/report.json.

The host did not expose npm and did not place node on PATH, so the exact
make test-js wrapper could not be invoked. Its substantive four gates were run
individually instead: pinned TypeScript typecheck, builder tests, generator
check, and the complete Node suite using
/home/draco/.vscode-server/bin/a5b500951314efd502d07465bd138dfbd714a960/node.

The browser proof extends the repository's native Chrome glTF CUBICSPLINE
WebGL2/WebGPU test with a direct affine mesh using non-uniform parent scale and
a 45-degree rotated child, yielding shear. It proves exact consumed vertices
and picking in both renderers. The Go lowering/schema/ray tests and native
preview/VM tests bridge the authored Group API through the remaining layers.

## 1,000-descendant benchmark

Three Linux amd64 runs:

| Benchmark | Time | Wire bytes | Wire delta | Heap bytes | Allocations |
| --- | --- | ---: | ---: | ---: | ---: |
| GroupScaleLower1000 | 533,735 to 573,373 ns/op | 166,055 | 51,898 | 2,026,309 to 2,026,312 B/op | 1,014 allocs/op |
| GroupScaleMarshal1000 | 1,843,246 to 1,880,864 ns/op | 166,055 | 51,898 | 1,362,946 to 1,367,929 B/op | 2,009 allocs/op |

The scaled-descendant wire delta is about 51.898 bytes per descendant and
contains one parent matrix, not a second backend-specific transform.

## Design deviations and remaining risks

1. Ancestry and R0 gate gap. The mandated direct e920d5d5 base lacks governed
   main and R0. R0-only source-role, exact-path, ownership, function,
   complexity, line, artifact, and Go cross-check harnesses were therefore not
   available to run in this branch. The implementation must be replayed onto
   the reviewed governed-main + R0 + S0/S1 integration and those exact gates
   must pass before landing.
2. Renderer-source scope. The readiness report predicted eight shared generated
   outputs and treated backend changes as a warning. Live Points motion cannot
   be prebaked or decomposed beneath shear, so client/runtime/scene3d/webgl.ts
   and webgpu.ts now upload parent * live-local matrices using the extracted
   shared math. That necessary source change produces 16 artifacts. There is no
   duplicated backend affine formula, and browser tests prove the contract, but
   R0 must audit the additional protected-source inventory.
3. Wrapper reproducibility. Run the literal make test-js target on an
   integration host with normal npm/node PATH even though all substantive
   constituent gates passed here.
4. Explicit collapse. Per the readiness decision and existing wire defaults, a
   literal zero scale component means one; zero-axis collapse is not supported.
5. Native model preview. The preview's pre-existing loader does not render
   Model or InstancedGLB assets. Their exact matrices are retained through the
   wire/browser paths; native object/points preview and VM matrices are covered.
6. Model payload breadth. GLB mesh and root matrices have exact affine
   semantics. Legacy primitive/line/points payload expansion inside non-GLTF
   model assets retains its existing behavior and should be examined if that
   asset form is brought into the v1 Group.Scale contract.
7. End-to-end proof shape. The native browser proof consumes the exact affine
   wire object rather than compiling a complete Group-authored Go example in
   the browser job. Typed lowering, wire/schema, Go ray/BVH, native, and browser
   tests collectively bridge those layers, but an integrated Go fixture would
   be useful defense in depth after ancestry integration.

## Integration handoff

1. Construct the reviewed ancestry in the required order: governed main
   84104d311962094bdd8ed30fde2c1f5ac6a5b7fb, R0
   343c53e30221384a639cacc13884e1c7508533d3, S0+S1
   e920d5d5ad84dba99303f2bfe378323164820517, then this S2 implementation.
2. Resolve the known .github/workflows/ci.yml overlap while preserving main's
   release-governed dependencies, S0+S1 corpus/browser job, and S2's generic
   Scene3D v1 proof wording.
3. Run all R0 governance inventories and cross-checks. Do not alter an
   ownership/role registry merely to self-approve the WebGL/WebGPU source
   changes.
4. Rerun the focused race, full unit, full JS through make test-js, generation
   check, bundle size, browser WebGL2/WebGPU, corpus, vet/fmt, and release gates
   on the integrated commit.
5. Recompute the 64-path generated digest on that ancestry and review the
   expected base-dependent result rather than comparing this direct-base digest
   blindly with b67fdd0a.

No remote action is included in this handoff.
