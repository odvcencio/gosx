package checkers

import (
	"math"
	"sort"

	checkermaterials "m31labs.dev/gosx/examples/gosx-docs/app/demos/checkers/materials"
	"m31labs.dev/gosx/scene"
)

const (
	boardHoleCount   = 121
	pieceCount       = 60
	activePieceCount = 20
	holeSpacing      = 0.52
)

var playerColors = []string{
	"#e66b62",
	"#e5b84f",
	"#69c98a",
	"#60a8dc",
	"#9a7bd3",
	"#d878a5",
}

type boardHole struct {
	ID       int
	Position scene.Vector3
}

// ShowcaseScene is the static typed Scene3D composition for the first
// Chinese Checkers wedge. Rules and selection state intentionally do not live
// here; the scene is a bounded presentation scaffold for the later game core.
func ShowcaseScene() scene.Props {
	return ShowcaseSceneWithMaterial(string(checkermaterials.CarvedWood))
}

func ShowcaseSceneWithMaterial(value string) scene.Props {
	holes := boardHoles()
	boardMaterial := scene.Material(scene.StandardMaterial{Color: "#49301f", Roughness: 0.72, Metalness: 0.03, Clearcoat: 0.16})
	if profile, err := checkermaterials.Compile(checkermaterials.Family(value)); err == nil {
		boardMaterial = profile.Selena
	}
	graph := []scene.Node{
		scene.HemisphereLight{
			ID:          "checkers-ambient",
			SkyColor:    "#d8e7ef",
			GroundColor: "#261b14",
			Intensity:   0.42,
		},
		scene.DirectionalLight{
			ID:         "checkers-key",
			Color:      "#fff0d2",
			Intensity:  1.35,
			Direction:  scene.Vec3(-0.45, -1, -0.35),
			CastShadow: true,
			ShadowSize: 1024,
		},
		scene.PointLight{
			ID:        "checkers-rim",
			Color:     "#8edbc4",
			Intensity: 0.75,
			Position:  scene.Vec3(3.5, 4.8, -3),
			Range:     16,
			Decay:     2,
		},
		scene.Mesh{
			ID:            "checkers-board-base",
			Geometry:      scene.CylinderGeometry{RadiusTop: 4.15, RadiusBottom: 4.25, Height: 0.32, Segments: 48},
			Material:      boardMaterial,
			Position:      scene.Vec3(0, -0.22, 0),
			CastShadow:    true,
			ReceiveShadow: true,
		},
		socketInstances(holes),
	}

	for player, positions := range initialPieceCamps(holes) {
		if player != 0 && player != 3 {
			continue
		}
		graph = append(graph, pieceInstances(player, positions))
	}

	return scene.Props{
		Background:    "#080b0f",
		Responsive:    scene.Bool(true),
		FillHeight:    scene.Bool(true),
		Controls:      "orbit",
		ControlTarget: scene.Vec3(0, 0, 0),
		Camera: scene.PerspectiveCamera{
			Position: scene.Vec3(0, 6.8, 7.6),
			FOV:      43,
			Near:     0.1,
			Far:      80,
		},
		Environment: scene.Environment{
			AmbientColor:     "#ffffff",
			AmbientIntensity: 0.16,
		},
		Shadows: scene.Shadows{MaxPixels: scene.ShadowMaxPixels1024},
		PostFX: scene.PostFX{
			MaxPixels: scene.PostFXMaxPixels1080p,
			Effects: []scene.PostEffect{
				scene.Tonemap{Mode: scene.TonemapACES, Exposure: 1.05},
				scene.Vignette{Intensity: 0.34},
			},
		},
		Graph: scene.NewGraph(graph...),
	}
}

func boardHoles() []boardHole {
	rowCounts := [...]int{1, 2, 3, 4, 13, 12, 11, 10, 9, 10, 11, 12, 13, 4, 3, 2, 1}
	holes := make([]boardHole, 0, boardHoleCount)
	for row, count := range rowCounts {
		z := (float64(row) - 8) * holeSpacing * math.Sqrt(3) / 2
		for column := 0; column < count; column++ {
			x := (float64(column) - float64(count-1)/2) * holeSpacing
			holes = append(holes, boardHole{
				ID:       len(holes),
				Position: scene.Vec3(x, 0, z),
			})
		}
	}
	return holes
}

func socketInstances(holes []boardHole) scene.InstancedMesh {
	positions := make([]scene.Vector3, len(holes))
	for i, hole := range holes {
		positions[i] = scene.Vec3(hole.Position.X, -0.015, hole.Position.Z)
	}
	return scene.InstancedMesh{
		ID:            "checkers-sockets",
		Count:         len(positions),
		Geometry:      scene.SphereGeometry{Radius: 0.145, Segments: 16},
		Material:      scene.StandardMaterial{Color: "#161a1d", Roughness: 0.58, Metalness: 0.16},
		Positions:     positions,
		Scales:        repeatedScale(len(positions), scene.Vec3(1, 0.34, 1)),
		ReceiveShadow: true,
	}
}

func initialPieceCamps(holes []boardHole) [6][]scene.Vector3 {
	var camps [6][]scene.Vector3
	used := make(map[int]bool, pieceCount)
	for player := 0; player < len(camps); player++ {
		angle := -math.Pi/2 + float64(player)*math.Pi/3
		dx, dz := math.Cos(angle), math.Sin(angle)
		ordered := append([]boardHole(nil), holes...)
		sort.SliceStable(ordered, func(i, j int) bool {
			a := ordered[i].Position.X*dx + ordered[i].Position.Z*dz
			b := ordered[j].Position.X*dx + ordered[j].Position.Z*dz
			if math.Abs(a-b) < 1e-9 {
				return ordered[i].ID < ordered[j].ID
			}
			return a > b
		})
		for _, hole := range ordered {
			if used[hole.ID] {
				continue
			}
			used[hole.ID] = true
			camps[player] = append(camps[player], scene.Vec3(hole.Position.X, 0.2, hole.Position.Z))
			if len(camps[player]) == 10 {
				break
			}
		}
	}
	return camps
}

func pieceInstances(player int, positions []scene.Vector3) scene.InstancedMesh {
	return scene.InstancedMesh{
		ID:       "checkers-player-" + string(rune('1'+player)),
		Count:    len(positions),
		Geometry: scene.SphereGeometry{Radius: 0.205, Segments: 24},
		Material: scene.StandardMaterial{
			Color:        playerColors[player],
			Roughness:    0.2 + float64(player%3)*0.1,
			Metalness:    0.08,
			Clearcoat:    0.68,
			Transmission: 0.04,
		},
		Positions:     positions,
		CastShadow:    true,
		ReceiveShadow: true,
	}
}

// visualCommands derives the complete instanced renderer state from the same
// authoritative board snapshot used by the semantic controls.
func visualCommands(board []int) []scene.Command {
	holes := boardHoles()
	positions := make([][]scene.Vector3, CampCount)
	for h, owner := range board {
		if owner < 1 || owner > CampCount || h >= len(holes) {
			continue
		}
		p := holes[h].Position
		positions[owner-1] = append(positions[owner-1], scene.Vec3(p.X, 0.2, p.Z))
	}
	graph := []scene.Node{socketInstances(holes)}
	for player, points := range positions {
		if len(points) > 0 {
			graph = append(graph, pieceInstances(player, points))
		}
	}
	ir := (scene.Props{Graph: scene.NewGraph(graph...)}).SceneIR()
	return []scene.Command{scene.SetInstancedMeshesCommand(ir.InstancedMeshes)}
}

func repeatedScale(count int, scale scene.Vector3) []scene.Vector3 {
	out := make([]scene.Vector3, count)
	for i := range out {
		out[i] = scale
	}
	return out
}
