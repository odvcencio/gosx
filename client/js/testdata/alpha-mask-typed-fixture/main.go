// Command alpha-mask-typed-fixture emits the typed AlphaCutoff fixture for
// the browser alpha-mask tests. It lowers five same-geometry standard
// material scenes through the real scene.Props -> SceneIR pipeline and
// derives transition commands from the real scene diff. It prints exactly
// one JSON object on stdout; errors go to stderr with a non-zero exit.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"m31labs.dev/gosx/scene"
)

// objectID is the stable object identity shared by every fixture case.
const objectID = "typed-mask"

// transition is one named previous -> next mutation produced by the real
// scene.DiffCommands lowering.
type transition struct {
	Name     string          `json:"name"`
	Initial  scene.SceneIR   `json:"initial"`
	Commands []scene.Command `json:"commands"`
}

// scenes holds the five same-geometry SceneIR cases.
type scenes struct {
	Opaque   scene.SceneIR `json:"opaque"`
	Mask     scene.SceneIR `json:"mask"`
	Zero     scene.SceneIR `json:"zero"`
	Disabled scene.SceneIR `json:"disabled"`
	Discard  scene.SceneIR `json:"discard"`
}

// fixture is the JSON handoff consumed by the browser test.
type fixture struct {
	Schema      string       `json:"schema"`
	Scenes      scenes       `json:"scenes"`
	Transitions []transition `json:"transitions"`
}

// build lowers one standard-material cube case through the actual typed
// Props{Graph}.SceneIR() pipeline. No map literals, no hand-authored IR.
func build(cutoff scene.AlphaCutoff, opacity float64) scene.SceneIR {
	wireframe := false
	material := scene.StandardMaterial{
		Color:       "#3a7bd5",
		Roughness:   0.35,
		Metalness:   0,
		IOR:         scene.Float(1.5),
		Wireframe:   &wireframe,
		Opacity:     scene.Float(opacity),
		AlphaCutoff: cutoff,
	}
	graph := scene.NewGraph(scene.Mesh{
		ID:       objectID,
		Geometry: scene.CubeGeometry{Size: 1.5},
		Material: material,
	})
	return scene.Props{Graph: graph}.SceneIR()
}

// buildFixture assembles the five scenes and the three named transitions.
// It performs no I/O and takes no arguments so the Go test can assert
// byte-for-byte determinism.
func buildFixture() fixture {
	opaque := build(scene.AlphaCutoff{}, 1)
	mask := build(scene.Cutoff(0.5), 0.5)
	zero := build(scene.Cutoff(0), 0)
	disabled := build(scene.CutoffDisabled(), 0.5)
	discard := build(scene.Cutoff(2), 1)
	return fixture{
		Schema: "gosx.alpha-cutoff.fixture.v1",
		Scenes: scenes{
			Opaque:   opaque,
			Mask:     mask,
			Zero:     zero,
			Disabled: disabled,
			Discard:  discard,
		},
		Transitions: []transition{
			{
				Name:     "absent-to-zero",
				Initial:  opaque,
				Commands: scene.DiffCommands(opaque, zero),
			},
			{
				Name:     "mask-to-disabled",
				Initial:  mask,
				Commands: scene.DiffCommands(mask, disabled),
			},
			{
				Name:     "mask-to-absent",
				Initial:  mask,
				Commands: scene.DiffCommands(mask, opaque),
			},
		},
	}
}

func main() {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(buildFixture()); err != nil {
		fmt.Fprintf(os.Stderr, "alpha-mask-typed-fixture: %v\n", err)
		os.Exit(1)
	}
}
