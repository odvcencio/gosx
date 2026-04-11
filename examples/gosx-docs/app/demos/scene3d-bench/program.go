// Package docs provides the Scene3D benchmark page at /demos/scene3d-bench.
//
// The page hosts a live in-browser frame-time overlay that reads
// performance.measure entries named "scene3d-render" (emitted by the
// PBR renderer when window.__gosx_scene3d_perf === true) and displays
// rolling p50/p95/max statistics across a 240-frame ring buffer.
//
// Scene workloads are selected via ?workload=<name> URL query. Each
// workload targets a different code path inside the renderer so a
// human driving the page in a browser can confirm that perf sweep
// optimizations hold up on real WebGL hardware — not just under the
// Node microbenchmark in client/js/runtime.bench.js.
package docs

import (
	"math"

	"github.com/odvcencio/gosx/scene"
)

// BenchScene dispatches to a workload-specific scene builder based on the
// query param. Unknown workloads fall back to the "mixed" scene.
func BenchScene(workload string) scene.Props {
	switch workload {
	case "static":
		return BenchStaticScene()
	case "pbr-heavy":
		return BenchPBRHeavyScene()
	case "thick-lines":
		return BenchThickLinesScene()
	case "particles":
		return BenchParticlesScene()
	case "mixed":
		fallthrough
	default:
		return BenchMixedScene()
	}
}

// BenchStaticScene is the baseline: 5 PBR meshes, no animation, no postfx.
// Every frame hits the dirty-tracked light/exposure upload fast path and
// the draw-list scratch arrays with effectively zero allocations. Expected
// frame cost should be dominated by GPU uniform + draw calls on real
// hardware, with JS-side work invisible.
func BenchStaticScene() scene.Props {
	return scene.Props{
		Width:      1024,
		Height:     600,
		Background: "#05080f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 1.5, 7),
			FOV:      55,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.3,
			SkyColor:         "#8de1ff",
			SkyIntensity:     0.4,
		},
		Graph: scene.NewGraph(
			scene.DirectionalLight{
				Color:     "#fff1d6",
				Intensity: 1.0,
				Direction: scene.Vec3(0.3, -1, -0.5),
			},
			scene.Mesh{
				Geometry: scene.SphereGeometry{Segments: 32},
				Material: scene.StandardMaterial{Color: "#D4AF37", Roughness: 0.3, Metalness: 0.9},
				Position: scene.Vec3(-1.5, 0.5, 0),
			},
			scene.Mesh{
				Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Material: scene.StandardMaterial{Color: "#E8E8E8", Roughness: 0.1, Metalness: 1.0},
				Position: scene.Vec3(1.5, 0.5, 0),
			},
			scene.Mesh{
				Geometry: scene.TorusGeometry{Radius: 0.5, Tube: 0.15, RadialSegments: 32, TubularSegments: 64},
				Material: scene.StandardMaterial{Color: "#c9a227", Roughness: 0.4, Metalness: 0.7},
				Position: scene.Vec3(0, 0.5, 0),
			},
			scene.Mesh{
				Geometry: scene.CylinderGeometry{RadiusTop: 0.3, RadiusBottom: 0.5, Height: 1.2, Segments: 24},
				Material: scene.StandardMaterial{Color: "#5fa3ff", Roughness: 0.5, Metalness: 0.2},
				Position: scene.Vec3(-3, 0.5, 0),
			},
			scene.Mesh{
				Geometry: scene.PlaneGeometry{Width: 10, Height: 10},
				Material: scene.StandardMaterial{Color: "#1a1a18", Roughness: 0.8, Metalness: 0.1},
				Rotation: scene.Rotate(-1.5708, 0, 0),
			},
		),
	}
}

// BenchPBRHeavyScene stress-tests the PBR mesh draw path: 30 shiny spheres
// arranged in a ring with a shadow-casting directional light and full
// postfx chain. Targets buildPBRDrawList, drawPBRObjectList, shadow map
// rendering, and the postfx framebuffer cap. Useful for catching
// regressions in the per-frame draw-list scratch pattern.
func BenchPBRHeavyScene() scene.Props {
	const count = 30
	nodes := []scene.Node{
		scene.DirectionalLight{
			Color:      "#fff1d6",
			Intensity:  1.1,
			Direction:  scene.Vec3(0.3, -1, -0.5),
			CastShadow: true,
			ShadowSize: 2048,
		},
		scene.PointLight{
			Color:     "#5fa3ff",
			Intensity: 0.8,
			Position:  scene.Vec3(0, 4, 0),
			Range:     15,
		},
		scene.Mesh{
			Geometry: scene.PlaneGeometry{Width: 20, Height: 20},
			Material: scene.StandardMaterial{Color: "#1a1a18", Roughness: 0.8, Metalness: 0.1},
			Rotation: scene.Rotate(-1.5708, 0, 0),
		},
	}
	for i := 0; i < count; i++ {
		angle := float64(i) / float64(count) * 2 * math.Pi
		x := math.Cos(angle) * 4
		z := math.Sin(angle) * 4
		nodes = append(nodes, scene.Mesh{
			Geometry: scene.SphereGeometry{Segments: 24},
			Material: scene.StandardMaterial{
				Color:     "#d4af37",
				Roughness: 0.25,
				Metalness: 0.9,
			},
			Position:      scene.Vec3(x, 0.6, z),
			CastShadow:    true,
			ReceiveShadow: true,
		})
	}
	return scene.Props{
		Width:      1024,
		Height:     600,
		Background: "#05080f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 4, 10),
			FOV:      55,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.2,
		},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.8, Strength: 0.4, Radius: 6, Scale: 0.25},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			},
		},
		Graph: scene.NewGraph(nodes...),
	}
}

// BenchThickLinesScene stress-tests the thick-line draw path with 120
// lightning segments split across all three blend passes (opaque, alpha,
// additive). Targets expandSceneThickLineIntoScratch, the pooled ping-pong
// buffers, and the per-pass index buffer upload/draw cycle.
func BenchThickLinesScene() scene.Props {
	nodes := []scene.Node{
		scene.AmbientLight{Color: "#8ecfff", Intensity: 0.4},
	}
	const bolts = 12
	for i := 0; i < bolts; i++ {
		angle := float64(i) / float64(bolts) * 2 * math.Pi
		cx := math.Cos(angle) * 3
		cz := math.Sin(angle) * 3
		points := []scene.Vector3{
			scene.Vec3(cx-0.4, 2.0, cz),
			scene.Vec3(cx-0.2, 1.0, cz+0.2),
			scene.Vec3(cx+0.1, 0.0, cz-0.1),
			scene.Vec3(cx-0.1, -1.0, cz+0.2),
			scene.Vec3(cx+0.3, -2.0, cz),
			scene.Vec3(cx, -2.5, cz-0.3),
		}
		segs := [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}}
		var pass scene.MaterialRenderPass
		var blend scene.MaterialBlendMode
		switch i % 3 {
		case 0:
			pass = scene.RenderOpaque
		case 1:
			pass = scene.RenderAlpha
			blend = scene.BlendAlpha
		case 2:
			pass = scene.RenderAdditive
			blend = scene.BlendAdditive
		}
		nodes = append(nodes, scene.Mesh{
			Geometry: scene.LinesGeometry{
				Points:   points,
				Segments: segs,
				Width:    4,
			},
			Material: scene.FlatMaterial{
				Color:      "#8ecfff",
				BlendMode:  blend,
				RenderPass: pass,
			},
		})
	}
	return scene.Props{
		Width:      1024,
		Height:     600,
		Background: "#05080f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 0, 8),
			FOV:      60,
		},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.6, Strength: 0.9, Radius: 10, Scale: 0.25},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.2},
			},
		},
		Graph: scene.NewGraph(nodes...),
	}
}

// BenchParticlesScene stress-tests the compute-particle path plus postfx.
// Uses a 2000-particle cloud with drift motion so the render loop is
// animated every frame and the dirty-tracking fast path is NOT a win
// (particle positions change per frame). Measures the dynamic-scene
// baseline.
func BenchParticlesScene() scene.Props {
	return scene.Props{
		Width:      1024,
		Height:     600,
		Background: "#05080f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 0, 10),
			FOV:      60,
		},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.5, Strength: 0.8, Radius: 8, Scale: 0.25},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			},
		},
		Graph: scene.NewGraph(
			scene.AmbientLight{Color: "#ffffff", Intensity: 0.8},
			scene.ComputeParticles{
				ID:    "bench-particles",
				Count: 2000,
				Emitter: scene.ParticleEmitter{
					Kind:     "sphere",
					Radius:   3,
					Rate:     120,
					Lifetime: 4,
					Scatter:  0.4,
				},
				Material: scene.ParticleMaterial{
					Color: "#a0d8ff",
					Size:  0.06,
				},
			},
		),
	}
}

// BenchMixedScene is the default workload — a mix of everything. 15 PBR
// meshes + 30 thick-line segments + postfx + shadows. Representative of
// a realistic non-trivial production scene and the best single-number
// test of overall render path health.
func BenchMixedScene() scene.Props {
	nodes := []scene.Node{
		scene.DirectionalLight{
			Color:      "#fff1d6",
			Intensity:  1.1,
			Direction:  scene.Vec3(0.3, -1, -0.5),
			CastShadow: true,
		},
		scene.PointLight{
			Color:     "#5fa3ff",
			Intensity: 0.6,
			Position:  scene.Vec3(-4, 4, 2),
			Range:     12,
		},
		scene.Mesh{
			Geometry: scene.PlaneGeometry{Width: 16, Height: 16},
			Material: scene.StandardMaterial{Color: "#1a1a18", Roughness: 0.8, Metalness: 0.1},
			Rotation: scene.Rotate(-1.5708, 0, 0),
		},
	}
	// 15 assorted PBR meshes in a spiral.
	for i := 0; i < 15; i++ {
		angle := float64(i) / 15 * 2 * math.Pi
		radius := 1.5 + float64(i)*0.15
		x := math.Cos(angle) * radius
		z := math.Sin(angle) * radius
		var geom scene.Geometry = scene.SphereGeometry{Segments: 20}
		if i%3 == 1 {
			geom = scene.BoxGeometry{Width: 0.6, Height: 0.6, Depth: 0.6}
		} else if i%3 == 2 {
			geom = scene.TorusGeometry{Radius: 0.3, Tube: 0.08, RadialSegments: 20, TubularSegments: 32}
		}
		nodes = append(nodes, scene.Mesh{
			Geometry:      geom,
			Material:      scene.StandardMaterial{Color: "#d4af37", Roughness: 0.3, Metalness: 0.85},
			Position:      scene.Vec3(x, 0.4, z),
			CastShadow:    true,
			ReceiveShadow: true,
			Spin:          scene.Rotate(0, 0.004, 0),
		})
	}
	// 6 thick lightning bolts mixing blend modes.
	boltPoints := []scene.Vector3{
		scene.Vec3(0, 3, 0),
		scene.Vec3(0.3, 2, 0.1),
		scene.Vec3(-0.1, 1, -0.2),
		scene.Vec3(0.2, 0, 0.3),
		scene.Vec3(-0.2, -1, 0.1),
	}
	boltSegs := [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}}
	for i := 0; i < 6; i++ {
		angle := float64(i) / 6 * 2 * math.Pi
		dx := math.Cos(angle) * 5
		dz := math.Sin(angle) * 5
		pts := make([]scene.Vector3, len(boltPoints))
		for j, p := range boltPoints {
			pts[j] = scene.Vec3(p.X+dx, p.Y, p.Z+dz)
		}
		nodes = append(nodes, scene.Mesh{
			Geometry: scene.LinesGeometry{Points: pts, Segments: boltSegs, Width: 5},
			Material: scene.FlatMaterial{
				Color:      "#8ecfff",
				BlendMode:  scene.BlendAdditive,
				RenderPass: scene.RenderAdditive,
			},
		})
	}
	return scene.Props{
		Width:      1024,
		Height:     600,
		Background: "#05080f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 3, 9),
			FOV:      55,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.2,
		},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.7, Strength: 0.6, Radius: 8, Scale: 0.25},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			},
		},
		Graph: scene.NewGraph(nodes...),
	}
}
