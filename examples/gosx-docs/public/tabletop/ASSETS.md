# Tabletop assets

The shared tabletop demo uses the CC0 assets below. Poly Haven publishes them under the [CC0 1.0 license](https://polyhaven.com/license). Author names were checked against the Poly Haven asset API on 2026-09-26.

| Asset | Use | Author | License | Source |
| --- | --- | --- | --- | --- |
| Potted Plant 04 | Plant model | James Ray Cock | CC0 1.0 | [polyhaven.com/a/potted_plant_04](https://polyhaven.com/a/potted_plant_04) |
| Studio Small 09 | HDRI for baked image-based lighting | Sergej Majboroda | CC0 1.0 | [polyhaven.com/a/studio_small_09](https://polyhaven.com/a/studio_small_09) |
| Wood Table 001 | Table albedo, OpenGL normal, and roughness maps | Dimitrios Savva (photography), Rico Cilliers (processing) | CC0 1.0 | [polyhaven.com/a/wood_table_001](https://polyhaven.com/a/wood_table_001) |

Run `GOWORK=off go run scripts/generate-tabletop-assets.go` from the repository root to regenerate the model, texture variants, environment maps, and locally authored studio gradient. The builder optimizes the 1K plant model, creates KTX2 and PNG texture variants, and bakes the HDRI with GoSX assetpipe. The source HDRI is not served to visitors. The vase, book stack, candle, and sphere are typed Scene3D mesh groups, so the still life needs one model download.

The builder writes `studio-sweep.png` locally for the camera-facing studio gradient.
