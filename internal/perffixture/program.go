package perffixture

import (
	"embed"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island/program"
)

//go:embed engine.js testdata/go-wasm/main.go
var programSources embed.FS

// ProgramSource returns an authored app factory or its Go-WASM source. It
// contains no compiled size or timing observations.
func ProgramSource(kind string) ([]byte, error) {
	switch kind {
	case "js":
		return programSources.ReadFile("engine.js")
	case "go-wasm":
		return programSources.ReadFile("testdata/go-wasm/main.go")
	default:
		return nil, fixtureError("/program/kind")
	}
}

// SharedProgram builds a drawable cube and camera using the production engine
// IR. Both generic shared engines and shared Scene3D fixtures consume it.
func SharedProgram() ([]byte, error) {
	b := engine.NewBuilder("BudgetEngine")
	b.Camera(map[string]program.ExprID{"z": b.Float(4), "fov": b.Float(55)})
	b.Mesh("box", "flat", map[string]program.ExprID{
		"id": b.String("box"), "color": b.String("#8de1ff"),
		"width": b.Float(1), "height": b.Float(1), "depth": b.Float(1),
	}, engine.MeshOptions{})
	return engine.EncodeProgramJSON(b.Build())
}
