# G07 — Renderer Bench workloads: `gpu-driven` and `instanced-classic` (repo: gosx)

Depends on: G02 (uses `scene.GPUDriven`). Can run any time after G02; it is
most useful after G06.

## Goal

`/demos/scene3d-bench?workload=gpu-driven` and `?workload=instanced-classic`
render the same instanced city: 8 batches × 5 000 instances (boxes and
spheres, per-instance colors, all shadow casters) behind 48 tall towers, one
shadow-casting directional light. The first sets
`GPUDriven{Occlusion: true}`, the second nothing. Comparing their CPU submit
and frame cadence measures the mode on real hardware (the human check L4).

A test pins that the two workloads differ ONLY in the mode: their lowered
SceneIR marshals identically once `GPUDriven` is cleared.

## Step 1 — create `examples/gosx-docs/app/demos/scene3d-bench/gpu_driven.go`

Exact content:

```go
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
```

## Step 2 — `program.go`: dispatch

Anchor (exactly once, in `BenchScene`):

```go
	case "particles-storm":
		return BenchParticlesStormScene()
```

Insert directly after it:

```go
	case "gpu-driven":
		return BenchGPUDrivenScene()
	case "instanced-classic":
		return BenchInstancedClassicScene()
```

## Step 3 — `page.server.go`: label and description

3a. Replace the text `seven stress workloads` with `nine stress workloads`
(exactly one occurrence).

3b. Anchor (exactly once, in `workloadLabel`):

```go
	case "particles-storm":
		return "particles-storm"
```

Insert directly after it:

```go
	case "gpu-driven":
		return "gpu-driven"
	case "instanced-classic":
		return "instanced-classic"
```

## Step 4 — `page.gsx`: navigation

Anchor (exactly once):

```html
					<a href="?workload=particles-storm">Particle storm</a>
```

Insert directly after it:

```html
					<a href="?workload=gpu-driven">GPU-driven city</a>
					<a href="?workload=instanced-classic">Classic city</a>
```

## Step 5 — `program_test.go`

5a. Add `"encoding/json"` as the first entry of the import block.

5b. Anchor (exactly once, in `TestBenchScene_Dispatch`):

```go
		{"particles-storm", len(BenchParticlesStormScene().Graph.Nodes)},
```

Insert directly after it:

```go
		{"gpu-driven", len(BenchGPUDrivenScene().Graph.Nodes)},
		{"instanced-classic", len(BenchInstancedClassicScene().Graph.Nodes)},
```

5c. Append to the end of the file:

```go
// TestBenchGPUDrivenScene_PairsWithClassic proves the two city workloads draw
// the same scene and differ only in the GPU-driven mode, so comparing their
// frame times measures the mode and nothing else.
func TestBenchGPUDrivenScene_PairsWithClassic(t *testing.T) {
	driven := BenchGPUDrivenScene()
	classic := BenchInstancedClassicScene()
	if driven.GPUDriven == nil || !driven.GPUDriven.Occlusion {
		t.Fatalf("gpu-driven workload GPUDriven = %#v, want occlusion on", driven.GPUDriven)
	}
	if classic.GPUDriven != nil {
		t.Fatalf("instanced-classic workload must not set GPUDriven, got %#v", classic.GPUDriven)
	}
	drivenIR := driven.SceneIR()
	classicIR := classic.SceneIR()
	if len(drivenIR.InstancedMeshes) != benchCityBatches+1 || len(classicIR.InstancedMeshes) != len(drivenIR.InstancedMeshes) {
		t.Fatalf("instanced batches = %d / %d, want %d", len(drivenIR.InstancedMeshes), len(classicIR.InstancedMeshes), benchCityBatches+1)
	}
	instances := 0
	for _, mesh := range drivenIR.InstancedMeshes {
		instances += mesh.Count
	}
	if want := benchCityBatches*benchCityPerBatch + benchCityTowers; instances != want {
		t.Fatalf("instances = %d, want %d", instances, want)
	}
	drivenIR.GPUDriven = nil
	if !sceneRecordsEqual(t, drivenIR, classicIR) {
		t.Fatal("the two city workloads differ in more than the GPU-driven mode")
	}
}

func sceneRecordsEqual(t *testing.T, a, b scene.SceneIR) bool {
	t.Helper()
	left, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return string(left) == string(right)
}
```

## Verify

```sh
gofmt -l examples/gosx-docs/app/demos/scene3d-bench    # prints nothing
go vet ./examples/gosx-docs/app/demos/scene3d-bench/
go test ./examples/gosx-docs/... -count=1
git diff --check
```

Human check (L4, optional, needs a GPU): run the docs app, open
`/demos/scene3d-bench?workload=instanced-classic`, then `?workload=gpu-driven`,
and compare the CPU submit and rAF rows. On the GPU-driven page the mount
element carries `data-gosx-scene3d-webgpu-gpu-driven-active="true"`,
`-reason="occlusion"` and a `-camera-visible` count far below 40 048.

## Commit

`add(demos): add gpu-driven and instanced-classic bench workloads`

- the same 40k-instance city with and without GPU-driven occlusion culling
- pin that the two workloads differ only in the mode
