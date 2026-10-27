Scene3D work limits measure the shipped bootstrap and PBR renderer chunks over the repository's fake GL and GPU device. CI verifies that those chunks match source before running the suite in `js-tests`.

The hero fixture is serialized from the docs home `HeroScene()` with its authored defaults. The other fixtures cover bloom, tone mapping and a shadow light, and 200 pyramids sharing three materials, including a named material. Desktop uses a fine pointer, DPR 2 and 1440×900 CSS pixels; phone uses a coarse pointer, DPR 2.75 and 390×844 CSS pixels. The suite asserts those CSS sizes and the selected full/balanced capability tier.

Each mount settles for eight frames. The work probe requests 120 real submissions at 30 Hz, recording both each metric's maximum per frame and its total. Totals catch increased frequency even when the peak stays constant. Upload counts include typed-array subranges and mapped GPU buffers. Post and shadow target counts sum live attachment pixels times samples; GL drawing-buffer MSAA is modeled as four samples when context antialiasing is enabled. These are operation counts and allocation sizes, not GPU execution time.

Separate settled mounts receive 120 RAF pulses at 60 Hz (two seconds) without invalidation: static, ambient spin, document hidden, and offscreen. This catches accidental permanent render loops. One pending render after hiding is part of the current baseline.

Run the gate twice to verify determinism:

```sh
node --test client/js/runtime-97-scene3d-work-budget.test.js
node --test client/js/runtime-97-scene3d-work-budget.test.js
GOWORK=off PERF_BUDGET_BASE_REF=origin/main go test -count=1 ./perf/budgets
```

A count above its limit fails. A count below its limit also fails until the limit is lowered. The base comparison reuses `wire.Ratchet` without a raise map; it rejects increases, dropped metrics and removed cases. Missing or unreadable base revisions fail. Only a verified base tree without the file permits its introduction.

After changing runtime source, run `GOWORK=off make build-bootstrap` and `GOWORK=off go generate ./island`. After changing the docs hero, refresh its fixture with `GOWORK=off go generate ./perf/budgets`. To record exact counts, run `UPDATE_SCENE3D_WORK_BUDGET=1 node --test client/js/runtime-97-scene3d-work-budget.test.js`, inspect the JSON diff, then rerun without the update variable. The base ratchet still rejects any increase.

Steady work uses 60 Hz sampling after the default governor warms up. The adaptive-default regression test separately checks demotion under a slow clock and recovery at 60 Hz. The down-only ratchet remains unchanged.
