package docs

import (
	_ "embed"
	"encoding/json"
)

// Generated from examples/gosx-docs/app/demos/tabletop/rooms.go by
// scripts/generate-tabletop-scene.go.
//
//go:embed demos/tabletop/home-scene.json
var homeTabletopSceneData []byte

func HeroScene() map[string]any {
	// home-scene.json is already lowered GoSX runtime props. Decode it as a
	// prop bag so the file-route spread preserves the generated SceneIR. A
	// scene.Props unmarshal cannot rebuild the authored Graph from SceneIR.
	var props map[string]any
	if err := json.Unmarshal(homeTabletopSceneData, &props); err != nil {
		panic(err)
	}
	return props
}
