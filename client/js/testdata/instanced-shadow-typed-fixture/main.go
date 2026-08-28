// Command instanced-shadow-typed-fixture emits the typed multi-instance
// shadow lifecycle fixture for the browser instanced-shadow tests. It lowers
// nine scene cases through the real scene.Props -> SceneIR pipeline and
// derives transition commands from the real scene diff. It prints exactly one
// JSON object on stdout; errors go to stderr with a non-zero exit.
//
// NOTE: browser shadow coverage is only claimed after the corresponding
// browser test actually runs against this fixture.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"m31labs.dev/gosx/scene"
)

const (
	schema     = "gosx.instanced-shadow.fixture.v1"
	casterID   = "typed-inst-caster"
	receiverID = "typed-shadow-receiver"
	lightID    = "typed-shadow-key"
)

// transition is one named from -> to mutation produced by the real
// scene.DiffCommands lowering.
type transition struct {
	Name     string          `json:"name"`
	From     string          `json:"from"`
	To       string          `json:"to"`
	Commands []scene.Command `json:"commands"`
}

// fixture is the JSON handoff consumed by the browser test.
type fixture struct {
	Schema      string                   `json:"schema"`
	Scenes      map[string]scene.SceneIR `json:"scenes"`
	Transitions []transition             `json:"transitions"`
}

// receiverNode is the shared static white box that receives shadows. Every
// scene uses the identical value.
func receiverNode() scene.Mesh {
	wireframe := false
	return scene.Mesh{
		ID: receiverID,
		Geometry: scene.BoxGeometry{
			Width:  3,
			Height: 2.2,
			Depth:  0.1,
		},
		Material: scene.StandardMaterial{
			Color:     "#ffffff",
			Roughness: 1,
			Metalness: 0,
			IOR:       scene.Float(1.5),
			Wireframe: &wireframe,
		},
		Position:      scene.Vec3(0, 0, -0.5),
		CastShadow:    false,
		ReceiveShadow: true,
	}
}

// keyLight is the shared directional key light. Every scene uses the
// identical value.
func keyLight() scene.DirectionalLight {
	return scene.DirectionalLight{
		ID:             lightID,
		Intensity:      1.2,
		Direction:      scene.Vec3(0.7, -0.7, -1),
		CastShadow:     true,
		ShadowSize:     512,
		ShadowCascades: 1,
	}
}

// instancedCaster is the shared typed instanced caster batch. Count drives
// how many of the fixed instance slots are active.
func instancedCaster(count int, positions []scene.Vector3, rotations []scene.Euler, scales []scene.Vector3, colors []string, alphaCutoff scene.AlphaCutoff) scene.InstancedMesh {
	wireframe := false
	return scene.InstancedMesh{
		ID:       casterID,
		Count:    count,
		Geometry: scene.BoxGeometry{Width: 0.55, Height: 0.55, Depth: 0.15},
		Material: scene.StandardMaterial{
			Color:       "#2040c0",
			Roughness:   1,
			Metalness:   0,
			IOR:         scene.Float(1.5),
			Opacity:     scene.Float(1),
			Wireframe:   &wireframe,
			AlphaCutoff: alphaCutoff,
		},
		Positions:     positions,
		Rotations:     rotations,
		Scales:        scales,
		Colors:        colors,
		CastShadow:    true,
		ReceiveShadow: false,
	}
}

// referenceMesh is one ordinary mesh standing in for a caster instance so the
// browser test has unmasked reference shadow pixels that do not travel the
// instancing path.
func referenceMesh(id string, position scene.Vector3) scene.Mesh {
	wireframe := false
	return scene.Mesh{
		ID:       id,
		Geometry: scene.BoxGeometry{Width: 0.55, Height: 0.55, Depth: 0.15},
		Material: scene.StandardMaterial{
			Color:     "#2040c0",
			Roughness: 1,
			Metalness: 0,
			IOR:       scene.Float(1.5),
			Wireframe: &wireframe,
		},
		Position:   position,
		CastShadow: true,
	}
}

// build lowers a node list through the actual typed Props{Graph}.SceneIR()
// pipeline. No map literals, no hand-authored IR.
func build(extra ...scene.Node) scene.SceneIR {
	nodes := append([]scene.Node{receiverNode(), keyLight()}, extra...)
	return scene.Props{Graph: scene.NewGraph(nodes...)}.SceneIR()
}

// Fixed instance slots shared by the caster cases.
var (
	slotA = scene.Vec3(-0.55, 0.35, 0.5)
	slotB = scene.Vec3(0.35, 0.35, 0.5)
)

func colors50() []string     { return []string{"rgba(32,64,192,0.5)", "rgba(32,64,192,0.5)"} }
func colorsHalf() []string   { return []string{"rgba(32,64,192,0.5)", "rgba(32,64,192,0.25)"} }
func colorsAlpha1() []string { return []string{"rgba(32,64,192,1)", "rgba(32,64,192,1)"} }

// buildFixture assembles the nine scenes and the six named transitions. It
// performs no I/O and takes no arguments so the Go test can assert
// byte-for-byte determinism.
func buildFixture() fixture {
	idR := scene.Euler{}
	unit := scene.Vec3(1, 1, 1)

	both := build(instancedCaster(2, []scene.Vector3{slotA, slotB},
		[]scene.Euler{idR, idR}, []scene.Vector3{unit, unit}, colors50(),
		scene.Cutoff(0.5)))
	left := build(instancedCaster(2, []scene.Vector3{slotA, slotB},
		[]scene.Euler{idR, idR}, []scene.Vector3{unit, unit}, colorsHalf(),
		scene.Cutoff(0.5)))
	right := build(instancedCaster(2, []scene.Vector3{slotA, slotB},
		[]scene.Euler{idR, idR}, []scene.Vector3{unit, unit},
		[]string{"rgba(32,64,192,0.25)", "rgba(32,64,192,0.5)"},
		scene.Cutoff(0.5)))

	movedA := scene.Vec3(-0.9, 0.6, 0.55)
	movedB := scene.Vec3(0.7, 0.7, 0.4)
	// moved-opaque is the independent opaque CONTROL for moved: the exact
	// same typed transforms, geometry, material RGB and flags, but with the
	// alphaCutoff OMITTED and both instance colors at alpha 1. It is typed
	// construction only; no hand-authored IR or transforms.
	movedOpaque := build(instancedCaster(2,
		[]scene.Vector3{movedA, movedB},
		[]scene.Euler{scene.Rotate(0.4, -0.3, 0.2), scene.Rotate(-0.5, 0.4, -0.3)},
		[]scene.Vector3{scene.Vec3(1.2, 0.8, 1.5), scene.Vec3(0.9, 1.3, 1.1)},
		colorsAlpha1(), scene.AlphaCutoff{}))
	moved := build(instancedCaster(2,
		[]scene.Vector3{movedA, movedB},
		[]scene.Euler{scene.Rotate(0.4, -0.3, 0.2), scene.Rotate(-0.5, 0.4, -0.3)},
		[]scene.Vector3{scene.Vec3(1.2, 0.8, 1.5), scene.Vec3(0.9, 1.3, 1.1)},
		colors50(), scene.Cutoff(0.5)))

	one := build(instancedCaster(1, []scene.Vector3{slotA},
		[]scene.Euler{idR}, []scene.Vector3{unit},
		[]string{"rgba(32,64,192,0.5)"}, scene.Cutoff(0.5)))
	empty := build()

	refLeft := build(referenceMesh("typed-ref-left", slotA))
	refRight := build(referenceMesh("typed-ref-right", slotB))

	return fixture{
		Schema: schema,
		Scenes: map[string]scene.SceneIR{
			"both":            both,
			"left":            left,
			"right":           right,
			"moved-opaque":    movedOpaque,
			"moved":           moved,
			"one":             one,
			"empty":           empty,
			"reference-left":  refLeft,
			"reference-right": refRight,
		},
		Transitions: []transition{
			{Name: "both-to-left", From: "both", To: "left", Commands: scene.DiffCommands(both, left)},
			{Name: "left-to-right", From: "left", To: "right", Commands: scene.DiffCommands(left, right)},
			{Name: "right-to-moved", From: "right", To: "moved", Commands: scene.DiffCommands(right, moved)},
			{Name: "moved-to-one", From: "moved", To: "one", Commands: scene.DiffCommands(moved, one)},
			{Name: "one-to-empty", From: "one", To: "empty", Commands: scene.DiffCommands(one, empty)},
			{Name: "empty-to-both", From: "empty", To: "both", Commands: scene.DiffCommands(empty, both)},
		},
	}
}

func main() {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(buildFixture()); err != nil {
		fmt.Fprintf(os.Stderr, "instanced-shadow-typed-fixture: %v\n", err)
		os.Exit(1)
	}
}
