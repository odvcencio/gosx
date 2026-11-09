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
    scripts/corpus/run.sh -r origin/main -b -R buildbox

- `-r REF` exports REF of this repo into a clean directory. `-g DIR` tests an existing checkout instead.
- `-b` reruns each failed check against the consumer's own pinned GoSX, so a pre-existing failure is not blamed on the new GoSX. A cell reads `FAIL (base PASS)` for a regression.
- `-R HOST` runs every check on a tailnet host through `tailnet-run --host HOST` instead of the local machine (no local heavy lock needed; the host's slots cap the load). The runner copies the GoSX checkout into the consumer copy so the host can see it. Use `buildbox` for the private consumers: chitin-choir carries a large art pack, so never send it to the macOS hosts. A step may wait for a free slot first; `CORPUS_SLOT_WAIT` (default 30) minutes are added to each step timeout for that.
- `-a` keeps the existing matrix for this GoSX sha and replaces only the `-c` consumers (use it to rerun one consumer).
- `-c a,b` limits the consumers. `-k` keeps the consumer copies. `-B SEC` sets the run-time budget.
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

Without `-R`, the runner holds `flock /home/draco/work/.runtime/locks/gosx-heavy.lock` (`CORPUS_LOCK` overrides it) while it runs one consumer's checks, and releases it between consumers. Every `go` command runs under `timeout`: 10 minutes per build, vet, resolve and wasm step (`CORPUS_STEP_TIMEOUT`), and the manifest's `test_timeout` (8 minutes) per test run. The whole run stops starting new checks after `CORPUS_BUDGET` seconds of check run time (default 3600; time spent waiting for the lock is not counted); the rest show as SKIP. Measured on buildbox: about 30 minutes of check run time (summed step seconds) for all six consumers with baselines (bon-voyage tests are the longest at about 9 minutes); waiting for a free slot or lock comes on top.

## Output

`$CORPUS_ROOT/<gosx-sha>/` (default `CORPUS_ROOT=/home/draco/work/.runtime/gosx-corpus`):

- `matrix.tsv`: consumer, check, status, baseline, seconds, first error, log path.
- `matrix.md`: the matrix, the consumer commits and pinned GoSX versions, and the first error of every failing check.
- `consumers.tsv`, `logs/`.

## Changing the manifest

Columns: name, url, ref, moddir, build, vet, test, test_timeout, wasm, public. Add a consumer by adding a row. Keep test packages to the ones that exercise GoSX.
