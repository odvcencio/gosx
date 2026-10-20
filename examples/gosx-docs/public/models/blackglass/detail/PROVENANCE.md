# Detail texture provenance

The Blackglass Beach detail layers use two scanned CC0 texture sets from
Poly Haven. CC0 places them in the public domain; no attribution is
required, and the authors are listed here as a courtesy.

| Files | Source | Authors | License |
| --- | --- | --- | --- |
| `sand-albedo.jpg`, `sand-normal.jpg`, `sand-rough.jpg` | [Coast Sand 01](https://polyhaven.com/a/coast_sand_01) | Rob Tuytel | [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/) |
| `basalt-albedo.jpg`, `basalt-normal.jpg`, `basalt-rough.jpg` | [Rock Face 03](https://polyhaven.com/a/rock_face_03) | Dario Barresi (photography), Rico Cilliers (processing) | [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/) |

## Processing

Each set was downloaded as the 1K JPEG maps (`diff`, `nor_gl`, `rough`) from
`https://dl.polyhaven.org/file/ph-assets/Textures/jpg/1k/<name>/` and reduced
for use as a detail layer:

- Albedo: converted to greyscale and scaled so the mean is mid-grey (128),
  because the detail layer modulates the base colour around 0.5. Resized to
  512 x 512, JPEG quality 85.
- Normal (OpenGL convention): resized to 512 x 512, JPEG quality 90.
- Roughness: resized to 256 x 256, JPEG quality 85.
