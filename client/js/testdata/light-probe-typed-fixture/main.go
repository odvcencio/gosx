// Command light-probe-typed-fixture emits typed light-probe fixtures for
// the native-browser light-probe regression probe
// (client/js/testdata/scene3d-light-probe-browser.cjs).
//
// It lowers two scene cases through the real scene.Props -> SceneIR
// pipeline and prints exactly one JSON object on stdout:
//
//	{"sparse":[...lights...],"zero":[...lights...]}
//
// The sparse case carries one non-zero coefficient entry (x=1, y=0.5) out
// of nine. Because scene.Vector3 components are json:omitempty, the emitted
// JSON proves on the real Go-to-browser wire that omitted components mean
// zero (entry 0 has no "z" key; entries 1..8 serialize as {}). The zero
// case carries nine all-zero Vector3 entries and must shade black.
//
// The browser probe runs this command once from the repository root, parses
// stdout, and consumes the emitted light arrays VERBATIM into its scene
// manifests. Errors go to stderr with a non-zero exit. This fixture is
// public OSS test tooling only.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"m31labs.dev/gosx/scene"
)

// emit lowers one light-probe scene through the real pipeline and encodes
// its IR light array. RawMessage keeps this file decoupled from the IR type
// name while still exercising the actual marshaller (and its omitempty
// semantics) end to end.
func emit(probe scene.LightProbe) (json.RawMessage, error) {
	ir := scene.Props{Graph: scene.NewGraph(probe)}.SceneIR()
	return json.Marshal(ir.Lights)
}

func main() {
	sparse := scene.LightProbe{ID: "probe", Color: "#0000ff", Intensity: 0.5,
		Coefficients: make([]scene.Vector3, 9)}
	sparse.Coefficients[0] = scene.Vector3{X: 1, Y: 0.5}

	zero := scene.LightProbe{ID: "probe", Color: "#0000ff", Intensity: 0.5,
		Coefficients: make([]scene.Vector3, 9)}

	sparseJSON, err := emit(sparse)
	if err != nil {
		fmt.Fprintln(os.Stderr, "light-probe-typed-fixture: sparse:", err)
		os.Exit(1)
	}
	zeroJSON, err := emit(zero)
	if err != nil {
		fmt.Fprintln(os.Stderr, "light-probe-typed-fixture: zero:", err)
		os.Exit(1)
	}

	out := struct {
		Sparse json.RawMessage `json:"sparse"`
		Zero   json.RawMessage `json:"zero"`
	}{sparseJSON, zeroJSON}

	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "light-probe-typed-fixture: encode:", err)
		os.Exit(1)
	}
}
