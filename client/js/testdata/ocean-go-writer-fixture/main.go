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
	if err := json.NewEncoder(os.Stdout).Encode(map[string]*scene.Ocean{
		"stopped":   stopped,
		"defaults":  (scene.Props{Environment: scene.Environment{Ocean: &scene.Ocean{}}}).SceneIR().Environment.Ocean,
		"roundTrip": (scene.Props{Environment: scene.Environment{Ocean: &roundTrip}}).SceneIR().Environment.Ocean,
	}); err != nil {
		panic(err)
	}
}
