# 01 — Preflight: toolchains, checkouts, baseline

Goal: a working environment with both repositories side by side, and a
recorded green baseline. No source changes.

## Steps

1. Lay the two repositories out as siblings. Elio's `go.mod` contains
   `replace m31labs.dev/gosx => ../gosx`, so the directory names matter:

   ```
   <root>/gosx   # odvcencio/gosx
   <root>/elio   # odvcencio/elio
   ```

   If gosx is already cloned elsewhere, clone elio next to it
   (`git clone https://github.com/odvcencio/elio ../elio` from the gosx root).
   If you cannot rename the gosx directory, create a symlink named `gosx` next
   to elio that points at it.

2. Toolchains:
   - Go: `go.mod` needs 1.26; `GOTOOLCHAIN=auto` (the default) downloads it.
   - Node 22 (`node --version` prints `v22.x`).
   - TypeScript for the ratchet: `cd client/runtime && npm ci` (pinned 5.9.3).
     Without `node_modules` the ratchet passes silently, so never skip this.

3. Build the Elio CLI once. It is used by E-tasks and to regenerate WGSL:

   ```sh
   cd <root>/elio
   go build -o /tmp/elio ./cmd/elio
   /tmp/elio check parse/testdata/cull.elio      # prints: ok
   ```

   If `go build` complains about a missing `go.sum` entry, re-run with
   `GOFLAGS=-mod=mod`. Do not commit resulting `go.mod`/`go.sum` changes unless
   a task says so.

4. Record the gosx baseline (all must pass before you start):

   ```sh
   cd <root>/gosx
   make build-bootstrap && git status --porcelain      # must print nothing
   (cd cmd/buildbootstrap && GOWORK=off go test -count=1 -tags 'grammar_subset grammar_subset_typescript' ./...)
   node --test client/js/runtime-21-scene-gpu-cull-bundles.test.js client/js/scene3d-renderer-architecture.test.js client/js/bootstrap-size.test.mjs
   (cd client/runtime && npm run typecheck)
   go test ./scene/... ./scene/capability ./render/bundle
   ```

   Expected at the baseline commit: 78 JS tests pass, buildbootstrap `ok`, and
   typecheck prints `scene3d noImplicitAny ratchet OK: 4874 diagnostic(s)
   (baseline: 4880)`. Per-file counts: `webgpu.ts` 1744 (exactly its baseline,
   zero slack), `mount.ts` 335 (zero slack), `compute.ts` 110.

   To print per-file counts yourself:

   ```sh
   cd client/runtime && (./node_modules/.bin/tsc --project tsconfig.scene3d.ratchet.json || true) \
     | grep -oE '^[^(]+\(' | sed 's/($//' | sort | uniq -c | sort -rn
   ```

5. Record the elio baseline: `cd <root>/elio && go test ./...` must pass. Tests
   that need `naga` or `glslangValidator` skip when the tool is not on PATH.

6. Optional but required for G09: a Chromium with WebGPU. Playwright's
   Chromium works headless with SwiftShader when launched with:

   ```
   --enable-unsafe-webgpu --enable-features=Vulkan --use-vulkan=swiftshader
   --use-webgpu-adapter=swiftshader --use-angle=swiftshader --disable-vulkan-surface
   ```

   WebGPU needs a secure context, so serve pages from `http://127.0.0.1:<port>`,
   not `about:blank` or `file:`.

## Verify

- Every command in step 4 exits 0.
- `/tmp/elio check <root>/elio/parse/testdata/cull.elio` prints `ok`.

## Report

Write down the exact test counts and ratchet numbers. Later tasks compare
against them.
