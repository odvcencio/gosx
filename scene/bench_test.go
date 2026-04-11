package scene

import (
	"encoding/json"
	"testing"
)

// benchMixedScene constructs a moderately complex scene representative of
// a production page: ~20 PBR meshes, a handful of lights, postfx, shadows,
// a few thick-line decorations, and an environment. Used as the fixture
// for every scene marshal benchmark in this file so results are
// comparable across runs.
//
// Counts are deliberately conservative so the benchmark runs fast but
// still exercises every legacyProps branch and every helper that a
// typical Scene3D page touches.
func benchMixedScene() Props {
	const boxCount = 20
	nodes := make([]Node, 0, 32)
	nodes = append(nodes,
		DirectionalLight{
			Color:      "#fff1d6",
			Intensity:  1.1,
			Direction:  Vec3(0.3, -1, -0.5),
			CastShadow: true,
			ShadowSize: 2048,
		},
		PointLight{
			Color:     "#5fa3ff",
			Intensity: 0.8,
			Position:  Vec3(0, 4, 0),
			Range:     15,
		},
		AmbientLight{Color: "#ffffff", Intensity: 0.2},
	)
	for i := 0; i < boxCount; i++ {
		nodes = append(nodes, Mesh{
			Geometry: SphereGeometry{Segments: 24},
			Material: StandardMaterial{
				Color:     "#d4af37",
				Roughness: 0.3,
				Metalness: 0.9,
			},
			Position:      Vec3(float64(i%5)*2, 0.5, float64(i/5)*2),
			CastShadow:    true,
			ReceiveShadow: true,
		})
	}
	nodes = append(nodes,
		Mesh{
			Geometry: LinesGeometry{
				Points:   []Vector3{Vec3(0, 2, 0), Vec3(1, 1, 0), Vec3(2, 0, 0)},
				Segments: [][2]int{{0, 1}, {1, 2}},
				Width:    4,
			},
			Material: FlatMaterial{
				Color:      "#8ecfff",
				BlendMode:  BlendAdditive,
				RenderPass: RenderAdditive,
			},
		},
		Mesh{
			Geometry: PlaneGeometry{Width: 20, Height: 20},
			Material: StandardMaterial{Color: "#1a1a18", Roughness: 0.8, Metalness: 0.1},
			Rotation: Rotate(-1.5708, 0, 0),
		},
	)
	return Props{
		Width:      1024,
		Height:     600,
		Background: "#05080f",
		Responsive: Bool(true),
		Controls:   "orbit",
		Camera: PerspectiveCamera{
			Position: Vec3(0, 4, 10),
			FOV:      55,
		},
		Environment: Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.2,
		},
		Shadows: Shadows{MaxPixels: ShadowMaxPixels1024},
		PostFX: PostFX{
			MaxPixels: PostFXMaxPixels1080p,
			Effects: []PostEffect{
				Bloom{Threshold: 0.8, Strength: 0.4, Radius: 6, Scale: 0.25},
				Tonemap{Mode: TonemapACES, Exposure: 1.1},
			},
		},
		Graph: NewGraph(nodes...),
	}
}

// BenchmarkPropsSceneIR measures the cost of the Props → SceneIR
// lowering step alone (graph walk, object/light/etc. record construction).
// Runs before any map allocations for legacyProps.
func BenchmarkPropsSceneIR(b *testing.B) {
	props := benchMixedScene()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = props.SceneIR()
	}
}

// BenchmarkPropsLegacyProps measures the cost of turning a SceneIR into
// the nested map[string]any wire format. This is what MarshalJSON calls
// internally and is the first phase of every scene render on the server.
func BenchmarkPropsLegacyProps(b *testing.B) {
	props := benchMixedScene()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = props.LegacyProps()
	}
}

// BenchmarkPropsMarshalJSON measures the full wire-format encoding path
// including json.Marshal over the nested map. This is what the server
// calls per page render when handing Scene3D props to the client.
func BenchmarkPropsMarshalJSON(b *testing.B) {
	props := benchMixedScene()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := json.Marshal(props)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkObjectIRLegacyProps measures the single-object map conversion
// cost. Called once per mesh inside legacyObjects; for a 20-mesh scene
// it runs 20x per page render.
func BenchmarkObjectIRLegacyProps(b *testing.B) {
	ir := ObjectIR{
		ID:            "bench-object",
		Kind:          "sphere",
		Radius:        0.5,
		Segments:      24,
		MaterialKind:  "standard",
		Color:         "#d4af37",
		Roughness:     0.3,
		Metalness:     0.9,
		X:             1.5,
		Y:             0.5,
		Z:             0,
		CastShadow:    true,
		ReceiveShadow: true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ir.legacyProps()
	}
}
