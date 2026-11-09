# Terrain, scatter, wind and shorelines

Large outdoor scenes in Scene3D are built from four engine features that share one set of ground data:

- `scene.Terrain` draws a heightmap ground with continuous level of detail and up to four splat layers.
- `scene.Scatter` places grass, shrubs, trees and rocks on that terrain from a seed.
- `scene.Wind` is one wind field that sways the scattered vegetation and that gameplay can sample.
- `scene.OceanShoreline` bakes the coastline from the terrain so the ocean's foam and depth follow it.

The same terrain also gives you a CPU height query and a physics heightfield, so a player stands on the surface they see.

## A worked example: an island

```go
package world

import (
	"m31labs.dev/gosx/physics"
	"m31labs.dev/gosx/scene"
)

func Island() (scene.Props, *physics.World, error) {
	// 1. Ground: a seeded 257 x 257 heightmap, 512 m across, 70 m tall.
	terrain := &scene.Terrain{
		Heightmap:   scene.GenerateHeightmap(scene.TerrainNoise{Seed: 7, Ridged: 0.35, Island: 0.55, Exponent: 1.3}),
		Position:    scene.Vector3{Y: -6}, // sample 0 sits 6 m below sea level
		Width:       512,
		HeightScale: 70,
		Layers: []scene.TerrainLayer{
			{Color: "#5f7f3a"},                                                // grass everywhere
			{Color: "#c9b78a", MaxHeight: scene.Float(-2), HeightBlend: 3},    // sand below -2 m
			{Albedo: "/textures/rock.png", Scale: 6, MinSlope: 34},            // rock on steep ground
			{Color: "#f2f4f6", MinHeight: scene.Float(44), MaxSlope: 40},      // snow on high, flat ground
		},
	}

	// 2. Wind for the whole scene.
	wind := &scene.Wind{Direction: 60, Speed: 6, Gust: 0.4}

	// 3. Vegetation, placed deterministically from the seed.
	sea := 0.0
	vegetation := scene.Scatter{
		ID: "veg", Terrain: terrain, Seed: 42, WaterLevel: &sea, WaterMargin: 0.5,
		Paths: []scene.ScatterPath{{Points: []scene.Vector3{{X: -200, Z: 0}, {X: 200, Z: 20}}, Width: 6}},
		Species: []scene.ScatterSpecies{
			{
				Name: "grass", Density: 0.5, MaxSlope: 30, MinScale: 0.7, MaxScale: 1.3, Align: 1,
				Wind: 0.8, WindHeight: 0.6,
				LODs: []scene.ScatterLOD{
					{Distance: 40, Geometry: scene.CylinderGeometry{RadiusBottom: 0.15, RadiusTop: 0.01, Height: 0.6, Segments: 5}, OffsetY: 0.3,
						Material: scene.StandardMaterial{Color: "#4f7a2a", Roughness: 0.9}},
					{Distance: 90, Keep: 0.5, Geometry: scene.BoxGeometry{Width: 0.2, Height: 0.5, Depth: 0.2}, OffsetY: 0.25,
						Material: scene.StandardMaterial{Color: "#4f7a2a", Roughness: 0.9}},
				},
			},
			{
				Name: "tree", Density: 0.008, MinHeight: scene.Float(2), MaxSlope: 35, MinScale: 0.8, MaxScale: 1.4,
				Sink: 0.3, CastShadow: true, ReceiveShadow: true, Wind: 0.15, WindHeight: 6,
				LODs: []scene.ScatterLOD{{Distance: 150, Geometry: scene.CylinderGeometry{RadiusBottom: 1.6, RadiusTop: 0.05, Height: 6, Segments: 8},
					OffsetY: 3, Material: scene.StandardMaterial{Color: "#2f5a2a"}}},
				Billboard: &scene.ScatterBillboard{Distance: 500, Width: 3, Height: 6, Material: scene.StandardMaterial{Color: "#2f5a2a"}},
			},
		},
	}

	props := scene.Props{
		Controls:      "orbit",
		ControlTarget: scene.Vector3{Y: 6},
		Camera:        scene.PerspectiveCamera{Position: scene.Vector3{X: -150, Y: 70, Z: 210}, FOV: 55, Near: 0.5, Far: 3000},
		GPUDriven:     &scene.GPUDriven{}, // WebGPU culls the scatter batches on the GPU
		Environment: scene.Environment{
			Sky:        &scene.Sky{Mode: "physical", SunDirection: scene.Vector3{X: -0.6, Y: 0.5, Z: -0.4}},
			FogColor:   "#c4d6ea",
			FogDensity: 0.0015,
			Terrain:    terrain,
			Wind:       wind,
			// 4. An ocean whose shore follows the terrain.
			Ocean: &scene.Ocean{WindDirection: 60, Shoreline: &scene.OceanShoreline{}},
		},
		Graph: scene.Graph{Nodes: []scene.Node{
			scene.DirectionalLight{Intensity: 2.6, Direction: scene.Vector3{X: -0.6, Y: -0.55, Z: -0.4}, CastShadow: true, ShadowSize: 2048},
			scene.HemisphereLight{SkyColor: "#bcd3ff", GroundColor: "#4a4034", Intensity: 0.5},
			vegetation,
		}},
	}

	// 5. Physics on the same ground.
	world := physics.NewWorld(physics.WorldConfig{Gravity: physics.Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60})
	ground, err := terrain.ColliderConfig()
	if err != nil {
		return props, nil, err
	}
	world.AddCollider(ground)
	return props, world, nil
}
```

Gameplay code can then ask the terrain and the wind directly:

```go
y := terrain.HeightAt(x, z)          // the surface the player stands on
s := terrain.Sample(x, z)            // height, smooth normal, slope in degrees, inside the terrain?
v := wind.Sample(x, z, seconds)      // wind velocity in m/s, gusts included
trees := vegetation.Placements()     // every placed instance, for collision or harvesting
```

## Terrain

### Height sources

A `Heightmap` is a grid of 16-bit samples. Sample 0 is the lowest point and 65535 the highest; `HeightScale` gives the height of 65535 in meters and `Position.Y` the height of 0.

| Source | Call |
|---|---|
| 16-bit grayscale PNG (8-bit and color also read) | `scene.DecodeHeightmapPNG(r)` |
| Headerless `.r16` / `.raw` export | `scene.HeightmapFromRaw(data, columns, rows, binary.LittleEndian)` |
| Any function | `scene.HeightmapFromFunc(columns, rows, func(u, v float64) float64)` |
| Seeded fractal noise | `scene.GenerateHeightmap(scene.TerrainNoise{...})` |

Column 0 lies at the terrain's minimum X and row 0 at its minimum Z (the top row of a PNG). Treat a heightmap as read-only once a scene uses it: the lowering caches its wire encoding, LOD table and shoreline bake on the value.

The samples ship inside the scene payload as base64. A 257 x 257 map adds about 176 KB. For larger maps, serve the same samples as a raw little-endian file and set `Terrain.Src` to its URL; the browser then fetches it and the server keeps the `Heightmap` for queries, LOD and baking.

### Level of detail without cracks

The browser draws the terrain as a quadtree of patches (CDLOD). Every frame it chooses, per patch, the coarsest level whose geometric error projects to at most `PixelError` pixels (default 1) at the current viewport height and field of view. The server measures each level's largest vertical error against the full heightmap when it lowers the terrain, so the choice follows the actual ground, not a guess.

Near a level boundary, odd vertices slide onto the next coarser grid. Adjacent patches therefore always share their edge vertices, and the ground stays closed while the camera moves. The unit test `client/js/scene3d-terrain.test.mjs` checks this for several cameras: every patch edge vertex next to a coarser patch lands exactly on a vertex of that patch. Lighting uses a per-sample normal map, so a slope shades the same at every level.

### Layers

Up to four `TerrainLayer`s are painted in order. Layer 0 covers everything; each later layer covers the earlier ones where all of its masks allow:

- `MinHeight` / `MaxHeight` (world Y, with `HeightBlend` meters of fade),
- `MinSlope` / `MaxSlope` (degrees, with `SlopeBlend` of fade),
- a `SplatMap` channel, when one is set: R paints layer 1, G layer 2, B layer 3.

A layer is a `Color` tint, an optional `Albedo` texture repeated every `Scale` meters, and a `Roughness`. On slopes steeper than `TriplanarSlope` (default 35 degrees) textures switch to triplanar projection, so cliffs do not stretch.

Terrain receives the scene's directional and spot shadows, fog and image-based lighting on both backends, because its pixels run through the same PBR fragment as every other mesh. The directional shadow map is fitted to cover the whole terrain as well as the shadow casters, so a large terrain spreads the map's texels thinner: raise the light's `ShadowSize` for sharper shadows.

## Physics

`terrain.ColliderConfig()` returns a static `physics.ShapeHeightfield` collider built from the same samples. The collider stores one float per sample and builds the two triangles of a cell only when a body or ray reaches it, so a 1025 x 1025 terrain costs about 8 MB. Contacts and raycasts go through the triangle-mesh code paths, including the internal-edge filter that stops bodies from catching on triangle seams. The shape is deterministic: the physics parity corpus (`physics/testdata/parity/heightfield_terrain.json`) replays bit for bit on amd64, with fused multiply-add, and on js/wasm.

`Collider.HeightAt(x, z)` reads the collider's surface. It always agrees with `Terrain.HeightAt` because both use the same diagonal split of each cell.

## Scatter

`Scatter` places each species on a jittered grid of `1/sqrt(Density)` meters. A candidate survives when the ground passes the species' height and slope ranges, lies above `WaterLevel + WaterMargin`, is off every `Path`, and a hashed draw passes the product of the `Mask` functions. Every random number is an integer hash of the seed, the species and the grid square, and every float product is rounded explicitly, so the same `Scatter` gives the same placement on every platform. `Placements()` returns it.

For drawing, the scatter area is cut into `CellSize` squares (default 64 m). Each cell lowers to one `InstancedMesh` batch per species level, with a distance band: level *i* draws while the camera's distance to the cell centre is below `LODs[i].Distance`, the billboard level (two crossed cards) takes over up to `Billboard.Distance`, and nothing draws beyond. A species with one level and `Distance` 0 draws everywhere as a single batch. `Keep` thins a far level. Transforms ship quantized to 8 bits per lane, about 22 bytes per instance per level.

Because the batches are ordinary instanced meshes, everything the instancing path does applies: `Props.GPUDriven` culls them on the GPU on WebGPU (with optional Hi-Z occlusion), and shadows work as for any instanced mesh.

Budget your counts. A density of 0.5 per square meter over a 512 m terrain is about 130,000 grass candidates before masks. Keep dense species on short LOD distances, and test the frame time on your weakest target device before raising them.

## Wind

`Environment.Wind` sets one field: it blows toward `Direction` (degrees, 0 toward +Z, 90 toward +X) at `Speed` m/s, and gust fronts `GustScale` meters apart roll downwind at `GustSpeed`, so neighbouring plants bend together. A batch sways when its species sets `Wind` (0 rigid, 1 grass). The bend grows from nothing at the instance origin to full at `WindHeight` meters above it, and reaches full strength at 10 m/s.

`wind.Sample(x, z, t)` returns the same field on the CPU. Other shaders can read it too:

- **WebGPU:** the renderer writes `wind = (dirX, dirZ, speed, time)` and `windGust = (gust, gustScale, gustSpeed, frequency)` as two `vec4f` at byte 160 of the frame uniform buffer (group 0, binding 0). Append `wind: vec4f, windGust: vec4f` to a custom shader's `FrameUniforms` struct.
- **WebGL2:** the instanced PBR program receives them as `uniform vec4 u_wind[3]`, followed by the batch's `(sway, height, 0, 0)`.

## Shorelines

Set `Ocean.Shoreline` with a terrain present and `Ocean.Bathymetry` nil. When the scene lowers, it bakes one texture over the terrain and installs it as the ocean's bathymetry: R holds the seabed height, G the signed distance to the waterline. The ocean then:

- uses the real seabed for its shallows, breakers and run-up,
- draws a foam band `FoamWidth` meters wide (default 3) that follows the waterline at any water level,
- makes the surf zone murkier: the absorption length shrinks to `Turbidity` (default 0.35) times `Clarity` at the shore and recovers over three foam widths,
- draws no water over dry land.

`scene.BakeShoreline(terrain, level, settings)` runs the same bake if you want to cache or inspect it. The default 256 x 256 bake adds roughly 50 to 150 KB to the page.

## WebGPU and WebGL2

WebGPU is the primary backend. WebGL2 draws the same terrain, scatter, wind and shoreline with fewer resources:

| | WebGPU | WebGL2 |
|---|---|---|
| Terrain error target | `PixelError` | 2 x `PixelError` (fewer, coarser patches) |
| Scatter culling | GPU-driven with `Props.GPUDriven` | per-batch distance bands only |
| Wind | vertex sway on every instanced path | vertex sway on the instanced program |

The terrain, wind and shoreline code ships in its own lazily loaded chunk, `bootstrap-feature-scene3d-terrain.js`, which a page fetches only when its scene sets `Environment.Terrain` or `Environment.Wind`.

## Limits

- One terrain per scene, on `Environment.Terrain`.
- Terrain receives shadows but does not cast them yet, so hills do not shadow valleys.
- Swaying vegetation casts its unswayed shadow.
- The terrain is not streamed: the whole heightmap is resident on the GPU.
- Continuous collision detection does not sweep against heightfields, as it does not against triangle meshes; keep fast bodies' steps short.
- Scatter LOD switches per cell, so a whole cell changes level at once.
