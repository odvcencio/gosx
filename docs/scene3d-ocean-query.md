# Query the ocean surface

`scene.NewOceanQuery(ocean, "high", floor).Sample(x, z, seconds)` returns the water above a world XZ coordinate: position, shading normal and water-particle velocity. Use `"low"` for the renderer's four-wave quality; high uses six waves. `Evaluate` takes the undisplaced grid coordinate instead.

The browser module `client/runtime/scene3d/ocean-query.ts` exposes `window.__gosx_scene3d_ocean_query.create`, `sample`, `evaluate` and `bathymetry`. Load the shared `ocean-waves.ts` first. Browser Ocean records use the normalized SceneIR properties. Go accepts the public Ocean zero-value defaults.

Both implementations use the same table in `scene/ocean_waves.json`. The Go implementation embeds it; `node scripts/generate-ocean-waves.mjs` writes the module used by the GPU uniform packer and the JS query. Coefficients and time round to float32, as the GPU uniform block does. Amplitude follows significant height: sum(a²) = Hs²/8. Gravity is 9.81 m/s².

The query includes horizontal Gerstner displacement, depth-dependent shoaling and the shader's nine-second run-up. Two fixed-point steps followed by up to eight Newton refinements invert the horizontal mapping. Normals use the shader's tangents; the shader omits derivatives of the shoaling and run-up terms. Velocity differentiates displacement in time at a material point; it is not an Eulerian current model. Pixel-scale capillary normal detail and foam are shading effects and are outside this surface query.

Pass a bathymetry callback that reproduces the GPU texture's level-zero bilinear R sample. JS `bathymetry(ImageData, ocean.bathymetry)` clamps to texel centres, filters R and then decodes linear or signed-sqrt height. A nil callback means deep water. A terrain heightfield is useful for collision but differs from the filtered texture; use the texture for visual parity in shallow water.

The query tests compare independent ports of the GLSL/WGSL vertex maths over a grid and several times to 0.0001 m. A shared 72-sample fixture checks Go/JS position, normal and velocity to 0.0000001. Actual GPU transcendental implementations and raster interpolation can introduce additional rounding.
