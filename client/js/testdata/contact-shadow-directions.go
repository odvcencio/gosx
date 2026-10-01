package main

import (
	"encoding/json"
	"os"

	"m31labs.dev/gosx/scene"
)

// Exercise the public lowering and wire encoder: Vector3 omits zero components.
func main() {
	var cases []scene.SceneIR
	for _, direction := range []scene.Vector3{
		scene.Vec3(0, -1, 0), scene.Vec3(0, 1, 0),
		scene.Vec3(-1, 0, 0), scene.Vec3(1, 0, 0),
		scene.Vec3(0, 0, -1), scene.Vec3(0, 0, 1),
	} {
		cases = append(cases, scene.Props{
			PostFX: scene.PostFX{Effects: []scene.PostEffect{scene.ContactShadows{Direction: direction}}},
		}.SceneIR())
		cases = append(cases, scene.Props{
			Graph:  scene.NewGraph(scene.DirectionalLight{Direction: direction}),
			PostFX: scene.PostFX{Effects: []scene.PostEffect{scene.ContactShadows{}}},
		}.SceneIR())
	}
	if err := json.NewEncoder(os.Stdout).Encode(cases); err != nil {
		panic(err)
	}
}
