package main

import (
	"encoding/json"
	"m31labs.dev/gosx/scene"
	"os"
)

func main() {
	tint := [3]float64{0.2, 0.5, 0.8}
	before := scene.SceneIR{InstancedMeshes: []scene.InstancedMeshIR{{ID: "glass", Kind: "box", Count: 1, Transmission: 1, Thickness: 2.5, AttenuationDistance: 4, AttenuationColor: &tint}}}
	after := scene.SceneIR{InstancedMeshes: []scene.InstancedMeshIR{{ID: "glass", Kind: "box", Count: 1, Transmission: 1}}}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{"initial": before.InstancedMeshes, "commands": scene.DiffCommands(before, after)}); err != nil {
		panic(err)
	}
}
