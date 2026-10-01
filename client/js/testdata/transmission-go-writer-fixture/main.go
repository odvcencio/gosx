package main

import (
	"encoding/json"
	"os"

	"m31labs.dev/gosx/scene"
)

func main() {
	tint := [3]float64{0, 0.5, 1}
	material := scene.StandardMaterial{
		Color: "#80c0ff", Texture: "/tint.png", Transmission: 1,
		Thickness: 2.5, AttenuationDistance: 4, AttenuationColor: &tint, IOR: scene.Float(1.5),
	}
	props := scene.Props{Graph: scene.NewGraph(
		scene.Mesh{ID: "glass", Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: material},
		scene.InstancedMesh{ID: "glass-instances", Count: 1, Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1}, Material: material, Positions: []scene.Vector3{{X: 2}}},
		scene.Model{ID: "glass-model", Src: "/glass.gosx3d.json", Material: material},
	)}
	if err := json.NewEncoder(os.Stdout).Encode(props); err != nil {
		panic(err)
	}
}
