# Scene3D Certification Matrix

`scene/cert` is the machine-readable certification source for Scene3D features. It answers the question GoSX needs to answer in CI, docs, and devtools: which layer has actually proven support for each public Scene3D feature?

The Go source in `matrix.go` is authoritative. `matrix.json` is generated from the same data for docs, tools, and external inspectors.

## Statuses

- `complete`: the dimension is implemented and covered by the current certification gate.
- `partial`: the dimension exists but still needs parity, diagnostics, examples, budgets, or tests.
- `fallback`: the feature has an intentional fallback instead of equivalent native behavior.
- `unsupported`: this dimension is unavailable and must stay explicit in docs/diagnostics.
- `notApplicable`: the dimension does not apply to the feature contract.

Every non-`complete` status carries a reason and next action.

## Strict Gate

`gosx scene certify --strict` currently enforces the v0.18.30 production-proof floor:

- built-in primitives are typed, serialized, bundled, WebGPU-native, and tested
- DOM HTML overlays preserve IR, bundle metadata, and accessible fallback semantics
- HTML texture surfaces preserve IR and render bundle metadata
- structured picking and HTML texture surfaces are not silently unsupported on WebGPU
- custom WGSL docs must not imply complete support before the WebGPU path is complete

The matrix is deliberately conservative. Features can ship as partial, fallback, or unsupported when that is the truthful public contract, but they cannot disappear from the matrix.
