# Consumer corpus

Builds and tests the real GoSX consumers against a GoSX checkout, so a
release that breaks a game is caught before it ships.

| Consumer | Visibility | Checked in hosted CI |
|---|---|---|
| primalrage, chitin-choir, bon-voyage, caribbean-dominoes | private | no (needs a token) |
| gridiron-2000, cicada (`workstation` module) | public | yes (`.github/workflows/corpus.yml`) |

## Release gate (private consumers)

The integrator runs this before each release, from a GoSX checkout:

    export TMPDIR=/home/draco/work/.runtime/tmp
    scripts/corpus/run.sh -r origin/main -b

- `-r REF` exports REF of this repo into a clean directory. `-g DIR` tests an existing checkout instead.
- `-b` reruns each failed check against the consumer's own pinned GoSX, so a pre-existing failure is not blamed on the new GoSX. A cell reads `FAIL (base PASS)` for a regression.
- `-c a,b` limits the consumers. `-k` keeps the consumer copies. `-B SEC` sets the budget.
- Exit status is 1 when a check regressed (with `-b`) or failed (without it).

## What it does

For each row of `manifest.tsv`, in order, never in parallel:

1. Shallow-fetches the consumer's `ref` (default `main`; pin a SHA in the manifest to freeze it) into `$CORPUS_ROOT/work/<name>`. The canonical checkouts under `/home/draco/work` are never touched.
2. Points every `m31labs.dev/gosx[/sub]` module the consumer requires at the GoSX checkout with `go mod edit -replace`, dropping any replace the consumer had (cicada pins a fork).
3. Runs five checks with `GOFLAGS=-p=2`, `-mod=mod`, `GOWORK=off`, `GOMEMLIMIT=1500MiB`:

| Check | Command |
|---|---|
| resolve | `go list -deps` over the build packages (module graph and go.sum) |
| build | `go build` over the build packages |
| vet | `go vet` over the vet packages |
| test | `go test -short -count=1 -timeout T` over the test packages in the manifest. These are the GoSX-facing packages, not the game-rule suites |
| wasm | `GOOS=js GOARCH=wasm go build` of the client packages (N/A when the consumer has none) |

If resolve fails, the other checks are skipped.

Every `go` command runs under `flock /home/draco/work/.runtime/locks/gosx-heavy.lock` (`CORPUS_LOCK` overrides it) and under `timeout`: 10 minutes per build, vet, resolve and wasm step (`CORPUS_STEP_TIMEOUT`), and the manifest's `test_timeout` (8 minutes) per test run. The whole run stops starting new checks after `CORPUS_BUDGET` seconds (default 5400, lock waits included); the rest show as SKIP.

## Output

`$CORPUS_ROOT/<gosx-sha>/` (default `CORPUS_ROOT=/home/draco/work/.runtime/gosx-corpus`):

- `matrix.tsv`: consumer, check, status, baseline, seconds, first error, log path.
- `matrix.md`: the matrix, the consumer commits and pinned GoSX versions, and the first error of every failing check.
- `consumers.tsv`, `logs/`.

## Changing the manifest

Columns: name, url, ref, moddir, build, vet, test, test_timeout, wasm, public. Add a consumer by adding a row. Keep test packages to the ones that exercise GoSX.
