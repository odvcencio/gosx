package docs

import (
	"fmt"

	"m31labs.dev/gosx/scene"
)

// Instanced-city geometry shared by the gpu-driven and instanced-classic
// workloads: benchCityBatches batches of benchCityPerBatch instances on a
// grid, behind rows of tall towers that hide most of them from a street-level
// camera.
const (
	benchCityBatches  = 8
	benchCityPerBatch = 5000
	benchCityTowers   = 48
)

// BenchGPUDrivenScene is the instanced city with GPU-driven instancing and
// two-phase occlusion culling on. Compare it with BenchInstancedClassicScene,
// which is the same scene on the classic instanced path: the difference is
// what the GPU-driven path buys on this machine.
func BenchGPUDrivenScene() scene.Props {
	props := benchInstancedCity()
	props.GPUDriven = &scene.GPUDriven{Occlusion: true}
	return props
}

// BenchInstancedClassicScene is BenchGPUDrivenScene without the GPU-driven
// mode.
func BenchInstancedClassicScene() scene.Props {
	return benchInstancedCity()
}

func benchInstancedCity() scene.Props {
	palette := []string{"#f2a65a", "#6fb7e0", "#9bd46a", "#e06f8b", "#c7a4f0", "#f0e17a", "#7ae0c9", "#e0a07a"}
	kinds := []scene.Geometry{
		scene.BoxGeometry{Width: 0.6, Height: 0.6, Depth: 0.6},
		scene.SphereGeometry{Radius: 0.35},
	}
	nodes := []scene.Node{
		scene.AmbientLight{Color: "#ffffff", Intensity: 0.35},
		scene.DirectionalLight{Color: "#fff4e0", Intensity: 1.4, Direction: scene.Vec3(-0.4, -1, -0.6), CastShadow: true, ShadowSize: 2048},
	}
	for b := 0; b < benchCityBatches; b++ {
		positions := make([]scene.Vector3, benchCityPerBatch)
		colors := make([]string, benchCityPerBatch)
		for i := range positions {
			n := b*benchCityPerBatch + i
			positions[i] = scene.Vec3(float64(n%200)-100, 0.3, -float64(n/200)*1.5)
			colors[i] = palette[(n/7)%len(palette)]
		}
		nodes = append(nodes, scene.InstancedMesh{
			ID:         fmt.Sprintf("bench-city-%d", b),
			Count:      benchCityPerBatch,
			Geometry:   kinds[b%len(kinds)],
			Positions:  positions,
			Colors:     colors,
			CastShadow: true,
		})
	}
	towers := make([]scene.Vector3, benchCityTowers)
	scales := make([]scene.Vector3, benchCityTowers)
	for i := range towers {
		towers[i] = scene.Vec3(float64(i%24)*8-92, 6, -6-float64(i/24)*20)
		scales[i] = scene.Vec3(7.5, 12, 1)
	}
	nodes = append(nodes, scene.InstancedMesh{
		ID:            "bench-city-towers",
		Count:         benchCityTowers,
		Geometry:      scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
		Positions:     towers,
		Scales:        scales,
		CastShadow:    true,
		ReceiveShadow: true,
	})
	return scene.Props{
		Width:      1024,
		Height:     600,
		Background: "#05080f",
		Responsive: scene.Bool(true),
		Controls:   "orbit",
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 2, 12),
			FOV:      60,
		},
		Graph: scene.NewGraph(nodes...),
	}
}
