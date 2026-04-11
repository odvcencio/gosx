package docs

import (
	"github.com/odvcencio/gosx/scene"
)

// DemoLightningScene is a visual QA fixture for the v0.15.0 thick-line
// pipeline and the v0.15.4 per-pass blending fix. It renders three
// jagged line geometries with increasing widths and different blend
// modes so a human can eyeball the three code paths in a single frame:
//
//   - Hairline (Width=0, opaque)          — legacy gl.LINES path
//   - Thick opaque (Width=3, RenderOpaque) — thick-line quad expansion,
//     opaque index buffer, LEQUAL depth
//   - Thick additive (Width=6, Additive)   — thick-line quad expansion,
//     additive index buffer, additive blend — the path that was
//     broken before v0.15.4 (all thick lines rendered as alpha)
//
// On a correct build the additive bolt should glow noticeably brighter
// than the alpha-blended one at the same color, especially where the
// segments cross. On the pre-fix build they look identical.
//
// Run with `gosx dev examples/gosx-docs` and mount this scene via the
// Scene3D engine to validate visually.
func DemoLightningScene() scene.Props {
	// Shared jagged polyline — a crude lightning bolt traversing the scene.
	points := []scene.Vector3{
		scene.Vec3(-3.0, 2.5, 0),
		scene.Vec3(-2.0, 1.5, 0.2),
		scene.Vec3(-2.3, 0.5, -0.1),
		scene.Vec3(-1.0, -0.3, 0.3),
		scene.Vec3(-1.5, -1.5, 0),
		scene.Vec3(0.0, -2.2, -0.2),
	}
	segments := [][2]int{{0, 1}, {1, 2}, {2, 3}, {3, 4}, {4, 5}}

	return scene.Props{
		Width:      900,
		Height:     540,
		Background: "#05080f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 0, 8),
			FOV:      50,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.15,
		},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.6, Strength: 0.9, Radius: 10, Scale: 0.25},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.2},
			},
		},
		Graph: scene.NewGraph(
			scene.AmbientLight{
				Color:     "#8ecfff",
				Intensity: 0.5,
			},
			// Hairline bolt — legacy gl.LINES. Cool blue, leftmost.
			scene.Mesh{
				Geometry: scene.LinesGeometry{
					Points:   translatePoints(points, -3, 0, 0),
					Segments: segments,
					// Width zero → legacy path.
				},
				Material: scene.FlatMaterial{
					Color: "#5fa3ff",
				},
			},
			// Thick opaque bolt — middle. Should be crisp, depth-tested.
			scene.Mesh{
				Geometry: scene.LinesGeometry{
					Points:   translatePoints(points, 0, 0, 0),
					Segments: segments,
					Width:    3,
				},
				Material: scene.FlatMaterial{
					Color:      "#88d4ff",
					RenderPass: scene.RenderOpaque,
				},
			},
			// Thick additive bolt — rightmost. Should glow noticeably
			// brighter than the opaque version, especially where the
			// bolt crosses itself or overlaps the additive bolt below.
			scene.Mesh{
				Geometry: scene.LinesGeometry{
					Points:   translatePoints(points, 3, 0, 0),
					Segments: segments,
					Width:    6,
				},
				Material: scene.FlatMaterial{
					Color:      "#d6ebff",
					BlendMode:  scene.BlendAdditive,
					RenderPass: scene.RenderAdditive,
				},
			},
			// Second additive bolt overlapping the first so the
			// additive accumulation is visible where they cross.
			scene.Mesh{
				Geometry: scene.LinesGeometry{
					Points:   translatePoints(points, 3.3, 0.2, 0.1),
					Segments: segments,
					Width:    6,
				},
				Material: scene.FlatMaterial{
					Color:      "#b8dcff",
					BlendMode:  scene.BlendAdditive,
					RenderPass: scene.RenderAdditive,
				},
			},
		),
	}
}

// translatePoints offsets every point in a polyline by (dx, dy, dz),
// returning a fresh slice. Used by DemoLightningScene to spawn three
// laterally-offset copies of the same bolt shape without aliasing the
// source point data.
func translatePoints(src []scene.Vector3, dx, dy, dz float64) []scene.Vector3 {
	out := make([]scene.Vector3, len(src))
	for i, p := range src {
		out[i] = scene.Vec3(p.X+dx, p.Y+dy, p.Z+dz)
	}
	return out
}

func DemoScene() scene.Props {
	return scene.Props{
		Width:      800,
		Height:     500,
		Background: "#08151f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 1.5, 5),
			FOV:      60,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.2,
		},
		// Shadows caps each shadow map at 1024². Lights can still request
		// ShadowSize: 2048/4096 for future high-quality fallbacks, but the
		// default keeps per-light memory at ~4 MB instead of 16–64 MB.
		// Set MaxPixels: scene.ShadowMaxPixelsUnbounded to opt out.
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024},
		// PostFX pipeline: bloom + ACES tonemap, capped at 1080p worth of
		// offscreen framebuffer pixels so 4K/retina displays don't allocate
		// multi-hundred-megabyte render targets. Bloom runs at 1/4 of the
		// scaled pipeline — low-frequency blur, no visible quality loss.
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Bloom{Threshold: 0.8, Strength: 0.5, Radius: 6, Scale: 0.25},
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.1},
			},
		},
		Graph: scene.NewGraph(
			scene.DirectionalLight{
				Color:      "#fff1d6",
				Intensity:  1.0,
				Direction:  scene.Vec3(0.3, -1, -0.5),
				CastShadow: true,
				ShadowSize: 2048, // scaled down to 1024 by Shadows.MaxPixels
			},
			scene.PointLight{
				Color:     "#D4AF37",
				Intensity: 0.6,
				Position:  scene.Vec3(-2, 3, 2),
				Range:     15,
			},
			scene.Mesh{
				Geometry: scene.SphereGeometry{Segments: 32},
				Material: scene.StandardMaterial{
					Color:     "#D4AF37",
					Roughness: 0.3,
					Metalness: 0.9,
				},
				Position:      scene.Vec3(-1.5, 0.5, 0),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
				Material: scene.StandardMaterial{
					Color:     "#E8E8E8",
					Roughness: 0.1,
					Metalness: 1.0,
				},
				Position:      scene.Vec3(1.5, 0.5, 0),
				Spin:          scene.Rotate(0, 0.004, 0),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.TorusGeometry{
					Radius:          0.5,
					Tube:            0.15,
					RadialSegments:  32,
					TubularSegments: 64,
				},
				Material: scene.StandardMaterial{
					Color:     "#c9a227",
					Roughness: 0.4,
					Metalness: 0.7,
				},
				Position:      scene.Vec3(0, 0.5, 0),
				Spin:          scene.Rotate(0.003, 0.005, 0),
				CastShadow:    true,
				ReceiveShadow: true,
			},
			scene.Mesh{
				Geometry: scene.PlaneGeometry{Width: 8, Height: 8},
				Material: scene.StandardMaterial{
					Color:     "#1a1a18",
					Roughness: 0.8,
					Metalness: 0.1,
				},
				Rotation:      scene.Rotate(-1.5708, 0, 0),
				ReceiveShadow: true,
			},
		),
	}
}
