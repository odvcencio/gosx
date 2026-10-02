package main

import (
	"encoding/json"
	"os"

	"m31labs.dev/gosx/scene"
)

func main() {
	stopped := (scene.Props{Environment: scene.Environment{Ocean: &scene.Ocean{
		Choppiness: -1, Speed: -1, Foam: -1, Surf: -1,
	}}}).SceneIR().Environment.Ocean
	wire, err := json.Marshal(stopped)
	if err != nil {
		panic(err)
	}
	var roundTrip scene.Ocean
	if err := json.Unmarshal(wire, &roundTrip); err != nil {
		panic(err)
	}
	previous := scene.Props{Environment: scene.Environment{Ocean: &scene.Ocean{}}}
	next := scene.Props{Environment: scene.Environment{Exposure: 2}}
	if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
		"previous":           previous,
		"sceneIRRemoval":     scene.DiffCommands(previous.SceneIR(), next.SceneIR()),
		"canonicalIRRemoval": scene.DiffIRCommands(previous.CanonicalIR(), next.CanonicalIR()),
		"stopped":            scene.Props{Environment: scene.Environment{Ocean: stopped}},
		"defaults":           scene.Props{Environment: scene.Environment{Ocean: &scene.Ocean{}}},
		"roundTrip":          scene.Props{Environment: scene.Environment{Ocean: &roundTrip}},
	}); err != nil {
		panic(err)
	}
}
