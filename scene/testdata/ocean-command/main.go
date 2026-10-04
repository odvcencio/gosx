package main

import (
	"encoding/json"
	"m31labs.dev/gosx/scene"
	"os"
)

func main() {
	payload := map[string][]scene.Command{
		"typed-scene":     {scene.SetSceneEnvironmentCommand(scene.EnvironmentIR{})},
		"typed-canonical": {scene.SetIREnvironmentCommand(scene.IREnvironment{})},
		"diff-scene":      scene.DiffCommands(scene.SceneIR{Environment: scene.EnvironmentIR{Ocean: &scene.Ocean{}}}, scene.SceneIR{}),
		"diff-canonical":  scene.DiffIRCommands(scene.IR{Environment: scene.IREnvironment{Ocean: &scene.Ocean{}}}, scene.IR{}),
	}
	if err := json.NewEncoder(os.Stdout).Encode(payload); err != nil {
		panic(err)
	}
}
