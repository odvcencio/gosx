# CI gates

What each check proves, how to run it on your machine, and where it runs. A
pull request needs the hosted `test` check, which waits for the jobs below.

A draft pull request runs only the fast jobs (`release-gate`, `go-tests` and the
four `js-tests*` jobs). The `test` check fails on purpose for a draft, so it
reads as red until you mark the pull request ready for review.

## Hosted jobs and their Make targets

Run a gate locally with `make <target>`. Targets that need TinyGo, Binaryen or
Chrome say so in `AGENTS.md` and `docs/docs-site-operations.md`.

| Job (`.github/workflows/ci.yml`) | Make targets | What it proves |
|---|---|---|
| `release-gate` | `release-gate` | Release metadata, tag ancestry, the perf-budget wrapper and the module-zip smoke test are consistent. |
| `go-tests` | `fmt-check`, `lint-deterministic`, `verify-danmuji`, `test-ci-partitions`, `test-unit`, `test-telemetry`, `test-island-aot`, `test-wasmgen`, `test-editor`, `test-fuzz-smoke`, `test-desktop` | Formatting; deterministic packages use no non-deterministic APIs; generated Danmuji tests are fresh; every package is in a test partition; the unit tests, including the package map freshness test; telemetry; AOT islands; wasm generation; the `editor` module; short fuzz runs; Windows desktop. |
| `desktop-macos` | none (runs `go test` natively on macOS runners) | The macOS desktop backend works on arm64 and amd64. |
| `go-race-tests` | `test-race-pr` (pull requests), `test-race` (pushes) | Race-detector runs on the shared-state packages, then on everything. |
| `go-cli-tests`, `-b`, `-c` | `test-cli CLI_SHARD=0\|1\|2`, `build-cli` | The `gosx` CLI, including production builds, split into three shards. |
| `js-tests`, `js-tests-b`, `js-tests-scene3d`, `js-tests-scene3d-b` | `test-js`, `test-js-node NODE_TEST_GROUP=...`, `test-perf-graph` | The browser runtime under Node, the bundle freshness check, and the perf graph. |
| `wasm-tests` | `build-wasm-all`, `test-wasm`, `test-telemetry-wasm`, `test-wasm-islands`, `test-motion-parity`, `test-physics-parity`, `test-vm-parity`, `wasm-size-budget`, `wasm-heap-gate`, `test-water-prod` | Every package builds for js/wasm; wasm tests; the same bits on wasm and native; the wasm size and heap budgets; the production water smoke test. |
| `wire-gates` | `wire-gate` | The wire-size budgets for pages and runtime chunks. |
| `scene3d-v1-browser-renderer-proof`, `scene3d-v1-adapter-proof` | none (inline steps) | The Scene3D v1 contract in [scene3d-v1-support.md](scene3d-v1-support.md) holds in a real browser renderer and in the adapters. |
| `browser-tests`, `-b`, `-c` | `test-e2e E2E_SHARD=0\|1\|2`, `test-perf-browser`, `perf-budget-ci` | End-to-end tests in Chrome against the docs app, browser perf tests and the perf budgets. |
| `water-webgpu-certification` | none (runs `go test -tags e2e -run TestWater ./e2e`) | Water renders on a hardware WebGPU runner. Runs only when the repository sets `WATER_WEBGPU_RUNNER`. |
| `test` | none | The aggregate that branch protection requires. It passes only when every job above passed. |

## Other workflows

| Workflow | When it runs | What it proves |
|---|---|---|
| `corpus.yml` | Pull requests | The public consumer games still build and pass against this checkout. See `scripts/corpus/README.md`. |
| `determinism-arm64.yml` | When a deterministic package changes | The deterministic packages give the same results on arm64. |
| `shader-validation.yml` | Pull requests and pushes | The WGSL shaders validate with `naga`. |
| `release-governed.yml` | Manual | Builds and publishes a release tag. |

## Running everything locally

`make ci` runs every Make target in the table above except `test-unit` and
`test-race-pr`, which are subsets of `test` and `test-race`. It runs the
unsharded forms: `test` covers the CLI tests, and one `test-e2e` and one
`test-js` run instead of the shards. It needs TinyGo,
Binaryen, Chrome and Node, and it takes a long time; run the targets for the
area you changed first.

## Two gates that live outside Make

- `scripts/corpus/run.sh` builds the private consumer games too. See
  `scripts/corpus/README.md`.
- The package map: `go run ./scripts/pkgmap --check` fails when
  [packages.md](packages.md) is stale.
