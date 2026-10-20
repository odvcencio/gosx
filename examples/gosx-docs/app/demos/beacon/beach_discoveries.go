package docs

import (
	"encoding/json"
	"math"

	"m31labs.dev/gosx/examples/gosx-docs/internal/beachgen"
	"m31labs.dev/gosx/scene"
)

// The inhabitants are small authored surfaces, baked by material rather than
// sent as hundreds of individual meshes. Pool water supplies the pick targets.
func blackglassBeachPoolLife() [][]scene.Mesh {
	coral := scene.StandardMaterial{Color: "#b57262", Roughness: .42}
	weed := scene.StandardMaterial{Color: "#477060", Roughness: .55}
	shell := scene.StandardMaterial{Color: "#c4afa1", Roughness: .48}
	groups := make([][]scene.Mesh, 3)
	for i, site := range blackglassBeachPoolSites() {
		x, z, r := site[0], site[1], site[2]
		level := blackglassBeachPoolLevel(x, z, r)
		var star scene.BufferGeometry
		star.Immutable, star.Revision = true, 1
		cx, cz := x-r*.3, z+r*.13
		for arm := 0; arm < 5; arm++ {
			a := float64(arm)*2*math.Pi/5 + .35
			point := func(angle, radius, height float64) scene.Vector3 {
				return scene.Vec3(cx+math.Cos(angle)*radius, level+height, cz+math.Sin(angle)*radius)
			}
			momentQuad(&star, point(a-.4, .065, -.035), scene.Vec3(cx, level+.012, cz),
				point(a+.4, .065, -.035), point(a, .24, -.022))
		}
		groups[0] = append(groups[0], scene.Mesh{Geometry: star, Material: coral})
		// A low anemone crown: eight tapered tentacles with varied lean.
		for j := 0; j < 8; j++ {
			a := float64(j) * math.Pi / 4
			cx, cz := x+r*.3+math.Cos(a)*.065, z-r*.26+math.Sin(a)*.065
			var tentacle scene.BufferGeometry
			tentacle.Immutable, tentacle.Revision = true, 1
			point := func(side, height float64) scene.Vector3 {
				return scene.Vec3(cx-math.Sin(a)*side+math.Cos(a)*height*.35, level-.04+height,
					cz+math.Cos(a)*side+math.Sin(a)*height*.35)
			}
			momentQuad(&tentacle, point(-.018, 0), point(.018, 0), point(.012, .09), point(-.012, .09))
			momentQuad(&tentacle, point(-.018, 0), point(-.012, .09), point(.012, .09), point(.018, 0))
			groups[1] = append(groups[1], scene.Mesh{Geometry: tentacle, Material: weed})
		}
		// Limpet shells follow the inner basalt lip instead of floating in space.
		for j := 0; j < 3; j++ {
			a := float64(j)*.27 + 1.8 + float64(i)*.4
			groups[2] = append(groups[2], scene.Mesh{
				Geometry: scene.CylinderGeometry{RadiusTop: .008, RadiusBottom: .05, Height: .035, Segments: 5},
				Position: scene.Vec3(x+math.Cos(a)*r*.82, level+.018, z+math.Sin(a)*r*.82), Material: shell})
		}
	}
	return groups
}

const blackglassHandheldX, blackglassHandheldZ = 12.45, 7.0

func blackglassBeachHandheld() [][]scene.Mesh {
	body := scene.StandardMaterial{Color: "#695884", Roughness: .52}
	dark := scene.StandardMaterial{Color: "#202735", Roughness: .66}
	screen := scene.StandardMaterial{Color: "#9da97b", Roughness: .2, Metalness: .12}
	buttons := scene.StandardMaterial{Color: "#9388ad", Roughness: .48}
	groups := make([][]scene.Mesh, 4)
	ground := beachgen.TerrainHeight(blackglassHandheldX, blackglassHandheldZ, beachgen.Seed)
	box := func(group int, material scene.StandardMaterial, x, y, z, w, h, d float64) {
		groups[group] = append(groups[group], scene.Mesh{
			Geometry: scene.BoxGeometry{Width: w, Height: h, Depth: d}, Material: material,
			Position: scene.Vec3(blackglassHandheldX+x, ground+y, blackglassHandheldZ+z)})
	}
	box(0, body, 0, .07, 0, .3, .09, .47)
	box(1, dark, 0, .119, -.095, .258, .012, .23)
	box(2, screen, 0, .127, -.09, .18, .008, .16)
	box(1, dark, -.083, .127, .114, .088, .019, .03)
	box(1, dark, -.083, .127, .114, .03, .019, .088)
	box(3, buttons, .075, .128, .085, .036, .022, .036)
	box(3, buttons, .105, .128, .13, .036, .022, .036)
	box(1, dark, 0, .125, .19, .08, .01, .015)
	return groups
}

func blackglassBeachDiscoveryModels() []scene.Node {
	return []scene.Node{
		scene.Model{ID: "tide-pool-life", Src: blackglassBeachModelRoot + "tide-pool-life.glb", Static: scene.Bool(true), Pickable: scene.Bool(false), ReceiveShadow: true},
		scene.Model{ID: "shore-handheld", Src: blackglassBeachModelRoot + "shore-handheld.glb", Static: scene.Bool(true),
			Interactive: true, Label: "Pick up the violet handheld beside the tide pools", ReceiveShadow: true},
	}
}

func blackglassHandheldPickup() []scene.Command {
	return []scene.Command{scene.SetTransformCommand("shore-handheld", scene.TransformPatch{Y: -100, ScaleX: 1, ScaleY: 1, ScaleZ: 1})}
}
func blackglassHandheldPickupCommands() string {
	data, err := json.Marshal(blackglassHandheldPickup())
	if err != nil {
		panic(err)
	}
	return string(data)
}
