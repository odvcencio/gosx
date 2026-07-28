# Water performance evidence

`scripts/water-profile-evidence.mjs` produces a repeatable evidence matrix for
the flagship `/demos/water` route. It measures all three presentation profiles
against the analytic Sphere and glTF Rubber Duck workloads:

| Profile | Sphere | Rubber Duck |
| --- | --- | --- |
| Hero | analytic fast path | glTF/object-target path |
| Balanced | analytic fast path | glTF/object-target path |
| Battery | analytic fast path | glTF/object-target path |

Each run creates `water-profile-evidence.json` and six viewport screenshots.
The JSON records:

- backend, fallback reason, adapter truth, device loss, and renderer state;
- requested DPR cap, actual DPR, surface/simulation/caustics/shadow/object
  target dimensions and budgets;
- authored feature toggles and mounted feature-system state;
- cold and warm navigation readiness;
- rAF p50, p95, p99, maximum interval, mean, and FPS;
- exact water frame and simulation advancement;
- shader diagnostic message/error counts;
- adaptive requested/active tier, reason, revision, and runtime timing;
- every numeric pass/draw/dispatch counter the selected backend exposes;
- Duck asset requests, browser errors, and screenshot paths.

The invariant gate always verifies that Hero, Balanced, and Battery authored
costs are monotonic, every profile/workload cell exists, frame and simulation
counters advance, shader diagnostics have no errors, and mounted resources
never exceed the selected profile. Hardware cadence thresholds are opt-in.

## Prerequisites

Run the real docs server in one terminal:

```sh
PORT=3100 SESSION_SECRET=gosx-water-evidence \
  go run ./cmd/gosx dev ./examples/gosx-docs
```

The browser harness uses Playwright but does not add an npm dependency to GoSX.
Point it at an existing `playwright` or `playwright-core` ESM entry and a
Chrome/Chromium executable:

```sh
export GOSX_PLAYWRIGHT_MODULE=/absolute/path/to/playwright-core/index.mjs
export GOSX_BROWSER_EXECUTABLE=/absolute/path/to/chrome
```

## Exact command

The repository's direct Node runtime can run the complete matrix:

```sh
/home/draco/.vscode-server/bin/1b6a188127eeaf9194f945eb6eb89a657e93c54c/node \
  scripts/water-profile-evidence.mjs \
  --url http://127.0.0.1:3100/demos/water \
  --out-dir build/water-profile-evidence \
  --budget perf/budgets/water-profile-evidence.json \
  --schema perf/water-profile-evidence.schema.json \
  --environment unspecified
```

The equivalent Make target is:

```sh
make water-profile-evidence \
  NODE=/home/draco/.vscode-server/bin/1b6a188127eeaf9194f945eb6eb89a657e93c54c/node \
  WATER_EVIDENCE_URL=http://127.0.0.1:3100/demos/water
```

Validate the harness and its checked-in configuration without starting a
browser:

```sh
make test-water-profile-evidence \
  NODE=/home/draco/.vscode-server/bin/1b6a188127eeaf9194f945eb6eb89a657e93c54c/node
```

## Hardware certification

Use `--environment hardware --enforce-hardware` only when the browser is
actually running on the GPU being certified:

```sh
/home/draco/.vscode-server/bin/1b6a188127eeaf9194f945eb6eb89a657e93c54c/node \
  scripts/water-profile-evidence.mjs \
  --url http://127.0.0.1:3100/demos/water \
  --out-dir build/water-profile-evidence-apple-m3 \
  --environment hardware \
  --enforce-hardware
```

The suggested thresholds live in
`perf/budgets/water-profile-evidence.json`; change that file or pass an
alternate `--budget` for a specific certification lab. The harness never
applies hardware timing thresholds to `software-raster` or `unspecified`
evidence.

Software raster is useful for lifecycle, fallback honesty, monotonic-resource,
shader-diagnostic, and frame-advancement gates. Its cadence is not evidence of
real-GPU performance and must not be used to claim parity with Three.js or any
other renderer.

## Output contract

- Evidence schema: `perf/water-profile-evidence.schema.json`
- Suggested budget: `perf/budgets/water-profile-evidence.json`
- Budget schema: `perf/budgets/water-profile-evidence-budget.schema.json`
- Default report: `build/water-profile-evidence/water-profile-evidence.json`
- Screenshots: `build/water-profile-evidence/{profile}-{sphere|duck}.png`

The output directory is replaceable with `--out-dir`. Profile query parameters
are applied by the harness; do not bake one profile into the base URL. New
browser contexts give each profile a cold cache, followed by a same-context
warm reload before its workload samples.
