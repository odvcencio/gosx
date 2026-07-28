package scene

import (
	"encoding/json"
	"fmt"
	"math"
	"testing"

	"m31labs.dev/gosx/scene/capability"
)

// benchSceneAt builds a scene with n renderable meshes plus a fixed
// support cast: lights, shadows, an environment and a four-pass post
// effect chain. The shape copies what a production page ships. Each
// mesh carries a PBR material, a shadow flag and a world transform, so
// the lowering walk pays the same per-record cost a real page pays.
//
// Every eighth mesh sits inside a Group, so the walk is not flat. Every
// twelfth mesh spins, which feeds the motion program encoder.
//
// Only the LAST mesh is pickable. collectFeatures breaks out of its
// pickable scan on the first hit, so a pickable mesh near the front
// would hide the full-array scan the capability walk really pays.
func benchSceneAt(n int) Props {
	nodes := make([]Node, 0, n+8)
	nodes = append(nodes,
		DirectionalLight{
			Color:      "#fff1d6",
			Intensity:  1.1,
			Direction:  Vec3(0.3, -1, -0.5),
			CastShadow: true,
			ShadowSize: 2048,
		},
		PointLight{Color: "#5fa3ff", Intensity: 0.8, Position: Vec3(0, 4, 0), Range: 15},
		SpotLight{Color: "#ffd9a0", Intensity: 0.6, Position: Vec3(4, 6, 2), Angle: 0.5},
		AmbientLight{Color: "#101325", Intensity: 0.12},
		HemisphereLight{SkyColor: "#88aaff", GroundColor: "#221100", Intensity: 0.3},
	)

	var group Group
	for i := range n {
		angle := float64(i) * 0.017
		mesh := Mesh{
			Geometry: SphereGeometry{Radius: 0.5, Segments: 16},
			Material: StandardMaterial{
				Color:     "#c8a8ff",
				Roughness: 0.2 + math.Mod(float64(i)*0.01, 0.5),
				Metalness: 0.4,
				Emissive:  0.35,
			},
			Position:      Vec3(6*math.Cos(angle), 0.5+float64(i%7), 6*math.Sin(angle)),
			Rotation:      Rotate(0, angle, 0),
			CastShadow:    true,
			ReceiveShadow: true,
		}
		if i%12 == 0 {
			mesh.Spin = Rotate(0, 0.4, 0)
		}
		if i == n-1 {
			mesh.Pickable = Bool(true)
		}
		if i%8 == 0 {
			group.Children = append(group.Children, mesh)
			continue
		}
		nodes = append(nodes, mesh)
	}
	if len(group.Children) > 0 {
		group.ID = "cluster"
		group.Position = Vec3(0, 1, 0)
		nodes = append(nodes, group)
	}
	nodes = append(nodes,
		Mesh{
			Geometry: PlaneGeometry{Width: 60, Height: 60},
			Material: StandardMaterial{Color: "#05080f", Roughness: 0.9, Metalness: 0.05},
			Rotation: Rotate(-1.5708, 0, 0),
		},
		Label{Text: "origin", Position: Vec3(0, 2, 0)},
	)

	return Props{
		Width:      1280,
		Height:     720,
		Background: "#03070d",
		Responsive: Bool(true),
		Controls:   "orbit",
		Camera:     PerspectiveCamera{Position: Vec3(0, 6, 14), FOV: 55},
		Environment: Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.22,
			EnvironmentMap:   "/assets/studio.hdr",
			EnvIntensity:     0.8,
			ToneMapping:      "aces",
		},
		Shadows: Shadows{MaxPixels: ShadowMaxPixels1024},
		PostFX: PostFX{
			MaxPixels: PostFXMaxPixels1080p,
			Effects: []PostEffect{
				Bloom{Threshold: 0.7, Strength: 0.55, Radius: 8, Scale: 0.25},
				SSAO{Radius: 0.4, Intensity: 0.6},
				ColorGrade{Contrast: 1.05, Saturation: 1.1},
				Tonemap{Mode: TonemapACES, Exposure: 1.15},
			},
		},
		Graph: NewGraph(nodes...),
	}
}

var benchSceneSizes = []int{10, 100, 1000, 5000}

// BenchmarkSceneIRLower measures the typed lowering alone.
func BenchmarkSceneIRLower(b *testing.B) {
	for _, n := range benchSceneSizes {
		props := benchSceneAt(n)
		b.Run(fmt.Sprintf("objects=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sink = props.SceneIR()
			}
		})
	}
}

// BenchmarkSceneIRMarshal measures the marshal that follows the lowering.
// The lowering runs outside the timed loop, so this isolates the encoder.
func BenchmarkSceneIRMarshal(b *testing.B) {
	for _, n := range benchSceneSizes {
		ir := benchSceneAt(n).SceneIR()
		b.Run(fmt.Sprintf("objects=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				data, err := json.Marshal(ir)
				if err != nil {
					b.Fatal(err)
				}
				byteSink = data
			}
		})
	}
}

// BenchmarkSceneIRSpreadProps measures the whole server-side cost of one
// 3D page: lower the graph, then marshal it into the prop bag.
func BenchmarkSceneIRSpreadProps(b *testing.B) {
	for _, n := range benchSceneSizes {
		props := benchSceneAt(n)
		b.Run(fmt.Sprintf("objects=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				mapSink = props.spreadPropsFast()
			}
		})
	}
}

// BenchmarkSceneIRCollectFeatures measures the capability walk alone.
// Props.SceneIR() runs it once per render over the assembled records.
func BenchmarkSceneIRCollectFeatures(b *testing.B) {
	for _, n := range benchSceneSizes {
		ir := benchSceneAt(n).SceneIR()
		b.Run(fmt.Sprintf("objects=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				featureSink = collectFeatures(ir)
			}
		})
	}
}

// Package-level sinks stop the compiler from removing the benchmarked
// call. Each benchmark writes its result to the sink of its own type.
var (
	sink        SceneIR
	byteSink    []byte
	mapSink     map[string]any
	featureSink []capability.Feature
)
