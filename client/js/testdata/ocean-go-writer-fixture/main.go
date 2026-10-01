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
	if err := json.NewEncoder(os.Stdout).Encode(map[string]scene.Props{
		"stopped":   {Environment: scene.Environment{Ocean: stopped}},
		"defaults":  {Environment: scene.Environment{Ocean: &scene.Ocean{}}},
		"roundTrip": {Environment: scene.Environment{Ocean: &roundTrip}},
	}); err != nil {
		panic(err)
	}
}
