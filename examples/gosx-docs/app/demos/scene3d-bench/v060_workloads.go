package docs

import (
	"math"

	"m31labs.dev/gosx/scene"
)

// The three workloads below measure the v0.60.0 renderer features that the
// older workloads predate. Each is sized for an M1- or Iris Xe-class laptop and
// stays well under the known crowd limit (GPU-driven culling of 10,000 or more
// agents loses the Metal device until GoSX #829 merges).

// benchCrowdSeatCeiling bounds the stadium crowd. It is half the 10,000 mark
// where GPU-driven culling is known to lose the Metal device.
const benchCrowdSeatCeiling = 5000

// BenchCrowdScene is a small stadium: one seating bowl of animated fans around
// a pitch, lit by a two-cascade directional shadow. It measures the crowd pass
// (culling, level of detail, instanced draws) against a field with almost no
// other geometry.
func BenchCrowdScene() scene.Props {
	home := []scene.CrowdColor{{Color: "#c8102e", Weight: 3}, {Color: "#ffffff", Weight: 1}}
	away := []scene.CrowdColor{{Color: "#003087", Weight: 3}, {Color: "#ffc72c", Weight: 1}}
	sections := make([]scene.CrowdSection, 12)
	for i := range sections {
		sections[i].Colors = home
		if i >= 9 {
			sections[i].Colors = away
		}
		sections[i].Action = scene.CrowdCheer
		sections[i].Excitement = 0.6
	}
	return scene.Props{
		Width:               1024,
		Height:              600,
		Background:          "#0b141c",
		Responsive:          scene.Bool(true),
		Controls:            "orbit",
		PreferWebGPU:        scene.Bool(true),
		MaxDevicePixelRatio: 1.5,
		MaxPixels:           scene.PostFXMaxPixels720p,
		ControlTarget:       scene.Vec3(0, 4, 0),
		Camera:              scene.PerspectiveCamera{Position: scene.Vec3(0, 34, 52), FOV: 50, Near: 0.5, Far: 260},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.3,
			Sky:              &scene.Sky{Mode: "gradient", TopColor: "#0b141c", HorizonColor: "#2b4553", BottomColor: "#16232c"},
			Crowds: []scene.Crowd{{
				ID:   "bench-bowl",
				Seed: 7,
				Stands: []scene.CrowdStand{{
					Shape: scene.CrowdShapeBowl, HalfWidth: 30, HalfDepth: 20, CornerRadius: 12,
					Rows: 9, Sections: sections,
				}},
			}},
		},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels720p, Effects: []scene.PostEffect{
			scene.Bloom{Mode: "mip", Threshold: 0.9, Strength: 0.2, Radius: 8, Scale: 0.5},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
		}},
		Graph: scene.NewGraph(
			scene.DirectionalLight{
				ID: "bench-crowd-sun", Color: "#fff4e0", Intensity: 1.6, Direction: scene.Vec3(-0.4, -1, -0.5),
				CastShadow: true, ShadowSize: 2048, ShadowCascades: 2, ShadowDistance: 90, ShadowSoftness: 0.1, ShadowBias: 0.003,
			},
			scene.HemisphereLight{ID: "bench-crowd-hemi", SkyColor: "#8fb3c8", GroundColor: "#2a3a30", Intensity: 0.5},
			scene.Mesh{
				ID:       "bench-crowd-pitch",
				Geometry: scene.PlaneGeometry{Width: 56, Height: 36},
				Material: scene.StandardMaterial{Color: "#2f6b3a", Roughness: 0.9},
				Rotation: scene.Rotate(-math.Pi/2, 0, 0), ReceiveShadow: true,
			},
		),
	}
}

// BenchTerrainScene is a 256 m heightmap island with wind-swept grass and
// trees. It measures terrain level of detail and scatter batches (GPU-culled on
// WebGPU, distance bands on WebGL2). Terrain casts no shadow yet, so only the
// trees do.
func BenchTerrainScene() scene.Props {
	terrain := &scene.Terrain{
		Heightmap:   scene.GenerateHeightmap(scene.TerrainNoise{Seed: 7, Columns: 129, Rows: 129, Ridged: 0.3, Island: 0.5, Exponent: 1.3}),
		Position:    scene.Vec3(0, -4, 0),
		Width:       256,
		HeightScale: 34,
		Layers: []scene.TerrainLayer{
			{Color: "#5f7f3a"},
			{Color: "#c9b78a", MaxHeight: scene.Float(-1), HeightBlend: 3},
			{Color: "#7d7466", MinSlope: 32},
			{Color: "#eef1f4", MinHeight: scene.Float(22), MaxSlope: 38},
		},
	}
	sea := 0.0
	vegetation := scene.Scatter{
		ID: "bench-veg", Terrain: terrain, Seed: 42, WaterLevel: &sea, WaterMargin: 0.5,
		Species: []scene.ScatterSpecies{
			{
				Name: "grass", Density: 0.12, MaxSlope: 28, MinScale: 0.7, MaxScale: 1.3, Align: 1,
				Wind: 0.8, WindHeight: 0.6,
				LODs: []scene.ScatterLOD{{
					Distance: 60, Geometry: scene.CylinderGeometry{RadiusBottom: 0.15, RadiusTop: 0.01, Height: 0.6, Segments: 5}, OffsetY: 0.3,
					Material: scene.StandardMaterial{Color: "#4f7a2a", Roughness: 0.9},
				}},
			},
			{
				Name: "tree", Density: 0.004, MinHeight: scene.Float(1), MaxSlope: 32, MinScale: 0.8, MaxScale: 1.4,
				Sink: 0.3, CastShadow: true, ReceiveShadow: true, Wind: 0.15, WindHeight: 6,
				LODs: []scene.ScatterLOD{{
					Distance: 140, Geometry: scene.CylinderGeometry{RadiusBottom: 1.4, RadiusTop: 0.05, Height: 6, Segments: 8}, OffsetY: 3,
					Material: scene.StandardMaterial{Color: "#2f5a2a", Roughness: 0.85},
				}},
				Billboard: &scene.ScatterBillboard{Distance: 300, Width: 3, Height: 6, Material: scene.StandardMaterial{Color: "#2f5a2a"}},
			},
		},
	}
	return scene.Props{
		Width:               1024,
		Height:              600,
		Background:          "#9fb8d0",
		Responsive:          scene.Bool(true),
		Controls:            "orbit",
		PreferWebGPU:        scene.Bool(true),
		MaxDevicePixelRatio: 1.5,
		MaxPixels:           scene.PostFXMaxPixels720p,
		GPUDriven:           &scene.GPUDriven{},
		ControlTarget:       scene.Vec3(0, 8, 0),
		Camera:              scene.PerspectiveCamera{Position: scene.Vec3(-95, 42, 120), FOV: 55, Near: 0.5, Far: 1200},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.25,
			Sky:              &scene.Sky{Mode: "physical", SunDirection: scene.Vec3(-0.6, 0.5, -0.4)},
			FogColor:         "#c4d6ea",
			FogDensity:       0.0015,
			Terrain:          terrain,
			Wind:             &scene.Wind{Direction: 60, Speed: 6, Gust: 0.4},
		},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels720p, Effects: []scene.PostEffect{
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.0},
		}},
		Graph: scene.NewGraph(
			scene.DirectionalLight{ID: "bench-terrain-sun", Intensity: 2.4, Direction: scene.Vec3(-0.6, -0.55, -0.4), CastShadow: true, ShadowSize: 2048, ShadowCascades: 3, ShadowDistance: 220, ShadowBias: 0.003},
			scene.HemisphereLight{ID: "bench-terrain-hemi", SkyColor: "#bcd3ff", GroundColor: "#4a4034", Intensity: 0.5},
			vegetation,
		),
	}
}

// BenchImageQualityScene spins a ring of metal spheres under the full v0.60.0
// image-quality chain: temporal anti-aliasing, depth of field, motion blur and
// auto exposure. Compare it with the pbr-heavy workload to see what that chain
// costs on this machine.
func BenchImageQualityScene() scene.Props {
	const count = 16
	nodes := []scene.Node{
		scene.DirectionalLight{ID: "bench-iq-key", Color: "#fff1d6", Intensity: 2.2, Direction: scene.Vec3(0.3, -1, -0.5), CastShadow: true, ShadowSize: 2048, ShadowSoftness: 0.1, ShadowBias: 0.002},
		scene.PointLight{ID: "bench-iq-fill", Color: "#5fa3ff", Intensity: 1.2, Position: scene.Vec3(0, 4, 0), Range: 16},
		scene.Mesh{
			ID: "bench-iq-floor", Geometry: scene.PlaneGeometry{Width: 30, Height: 30},
			Material: scene.StandardMaterial{Color: "#4a5563", Roughness: 0.6, Metalness: 0.05},
			Rotation: scene.Rotate(-math.Pi/2, 0, 0), ReceiveShadow: true,
		},
	}
	for i := 0; i < count; i++ {
		angle := float64(i) / count * 2 * math.Pi
		nodes = append(nodes, scene.Mesh{
			ID:       "bench-iq-sphere-" + string(rune('a'+i)),
			Geometry: scene.SphereGeometry{Radius: 0.45, Segments: 32},
			Material: scene.StandardMaterial{
				Color: "#d4af37", Roughness: 0.12 + 0.05*float64(i%6), Metalness: 0.9,
			},
			Position:   scene.Vec3(math.Cos(angle)*4, 0.6, math.Sin(angle)*4),
			Spin:       scene.Rotate(0, 0.02, 0),
			Drift:      scene.Vec3(0, 0.6, 0),
			DriftSpeed: 0.9 + 0.1*float64(i%4),
			CastShadow: true, ReceiveShadow: true,
		})
	}
	return scene.Props{
		Width:               1024,
		Height:              600,
		Background:          "#05080f",
		Responsive:          scene.Bool(true),
		Controls:            "orbit",
		PreferWebGPU:        scene.Bool(true),
		MaxDevicePixelRatio: 1.5,
		MaxPixels:           scene.PostFXMaxPixels720p,
		ControlTarget:       scene.Vec3(0, 0.8, 0),
		Camera:              scene.PerspectiveCamera{Position: scene.Vec3(0, 5, 10), FOV: 50, Near: 0.1, Far: 80},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.2,
			AutoExposure:     &scene.AutoExposure{MinExposure: 0.6, MaxExposure: 2, Key: 0.2},
		},
		PostFX: scene.PostFX{MaxPixels: scene.PostFXMaxPixels720p, Effects: []scene.PostEffect{
			scene.DOF{FocusDistance: 9, Aperture: 0.03, MaxBlur: 8},
			scene.MotionBlur{Strength: 0.4},
			scene.Bloom{Mode: "mip", Threshold: 0.9, Strength: 0.25, Radius: 8, Scale: 0.5},
			scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			scene.TAA{HistoryWeight: 0.9, ClampGamma: 1.25},
		}},
		Graph: scene.NewGraph(nodes...),
	}
}
