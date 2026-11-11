# Stability tiers

Every package carries one of three tiers. The tier says what you can rely on
when you upgrade. The per-package labels are in the [package map](packages.md).

| Tier | What it means |
|------|---------------|
| stable | A break needs a CHANGELOG entry with a migration path, and a run of the consumer corpus first. |
| experimental | May break in any minor release. Pin an exact GoSX version if you depend on it. |
| internal-use | Public today because of how the repo grew. It supports another package or a tool. Do not build on it; it may move under `internal/`. |

GoSX is pre-1.0, so a stable package can still take a clean break. The tier
tells you how loudly the break is announced, not that it never happens.

## What is experimental in v0.60.0

These shipped with the v0.60.0 jam work. They are in use, and their APIs are
still settling:

- the game kit (`game` and its subpackages), `room`, `netplay`, `sim`;
- `motion/animgraph` and `motion`, `physics`, `vessel`, `det`;
- Scene3D terrain, crowds, decals and the image-quality passes are part of
  `scene`, whose stable surface is the contract in
  [scene3d-v1-support.md](scene3d-v1-support.md). Features newer than that
  contract are experimental even though the package is stable;
- `assetpipe`, `field`, `crdt`, `render`, `ir`, `desktop`, `editor`, `lsp`,
  `telemetry`, `scheduled`.

## Changing a tier

Each package has one row in `scripts/pkgmap/layers.txt`. Edit the row, then run
`go run ./scripts/pkgmap` to refresh the package map. A package with no row
fails `go test ./scripts/pkgmap`, so a new package has to choose a tier.
