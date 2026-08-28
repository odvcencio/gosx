// Command spot-shadow-typed-fixture emits the typed spot-light shadow
// lifecycle fixture for the browser spot-shadow tests. It lowers twelve
// scene cases through the real scene.Props -> SceneIR pipeline and derives
// transition commands from the real scene diff. It prints exactly one JSON
// object on stdout; errors go to stderr with a non-zero exit.
//
// NOTE: browser shadow coverage is only claimed after the corresponding
// browser test actually runs against this fixture. Native pixels check the
// ray/box occlusion footprint on the front receiver plane z=-0.45
// independently; this fixture carries no shadow matrices or shader math.
package main

import (
	"encoding/json"
	"fmt"
	"os"

	"m31labs.dev/gosx/scene"
)

const (
	schema     = "gosx.spot-shadow.fixture.v1"
	casterID   = "typed-spot-caster"
	receiverID = "typed-spot-receiver"
	lightID    = "typed-spot-key"
	ambientID  = "typed-spot-ambient"
	blackSpot1 = "typed-spot-black-1"
	blackSpot2 = "typed-spot-black-2"
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

// receiverNode is the shared static white box that receives shadows.
func receiverNode(receive bool) scene.Mesh {
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
		ReceiveShadow: receive,
	}
}

// ambientLight is the shared dim ambient fill that keeps shadowed areas
// visible. Every scene uses the identical value.
func ambientLight() scene.AmbientLight {
	return scene.AmbientLight{
		ID:        ambientID,
		Color:     "#404040",
		Intensity: 0.3,
	}
}

// casterMesh is the shared red caster box. opacity drives the discarded and
// equal alpha-cutoff cases; cast drives the no-caster negative control.
func casterMesh(opacity float64, cutoff scene.AlphaCutoff, cast bool) scene.Mesh {
	wireframe := false
	return scene.Mesh{
		ID:       casterID,
		Geometry: scene.BoxGeometry{Width: 0.55, Height: 0.55, Depth: 0.15},
		Material: scene.StandardMaterial{
			Color:       "#c02020",
			Roughness:   1,
			Metalness:   0,
			IOR:         scene.Float(1.5),
			Opacity:     scene.Float(opacity),
			Wireframe:   &wireframe,
			AlphaCutoff: cutoff,
		},
		Position:      scene.Vec3(-0.55, 0.35, 0.5),
		CastShadow:    cast,
		ReceiveShadow: false,
	}
}

// spotLight is the primary spot. Position, direction and castShadow vary
// across the named scenes; every other value is fixed. Intensity is an
// explicit 6 (not an out-of-range 12 that relied on downstream clamping)
// and Range is an explicit 6.5 (not 0 relying on the default), so the wire
// contract is bounded and unambiguous.
func spotLight(id string, position, direction scene.Vector3, cast bool) scene.SpotLight {
	return scene.SpotLight{
		ID:             id,
		Color:          "#ffffff",
		Intensity:      6,
		Position:       position,
		Direction:      direction,
		Angle:          0.75,
		Penumbra:       0,
		Range:          6.5,
		CastShadow:     cast,
		ShadowBias:     0.0001,
		ShadowSize:     512,
		ShadowSoftness: 0,
	}
}

// blackSpot is one unsupported wide-cone (half-angle > 90 degrees) spot
// used by invalid-prefix. Color is black with nonzero intensity so the
// wire omitempty can never resurrect an intended-zero intensity.
func blackSpot(id string, position scene.Vector3) scene.SpotLight {
	return scene.SpotLight{
		ID:         id,
		Color:      "#000000",
		Intensity:  0.5,
		Position:   position,
		Direction:  scene.Vec3(0.5, -0.5, -1),
		Angle:      1.8,
		CastShadow: true,
	}
}

// build lowers a node list through the actual typed Props{Graph}.SceneIR()
// pipeline. No map literals, no hand-authored IR.
func build(extra ...scene.Node) scene.SceneIR {
	nodes := append([]scene.Node{receiverNode(true), ambientLight()}, extra...)
	return scene.Props{Graph: scene.NewGraph(nodes...)}.SceneIR()
}

// Fixed spot parameters shared by the scene cases.
var (
	basePos  = scene.Vec3(-1.55, 1.35, 2.5)
	baseDir  = scene.Vec3(1, -1, -2)
	movedPos = scene.Vec3(-0.95, 1.35, 2.5) // base x + 0.6
	// Tilted ~0.61 rad from baseDir so the geometric oracle cone boundary
	// cuts through the caster shadow footprint (same light position, so the
	// physical occlusion set is identical to base ignoring the cone).
	aimedDir = scene.Vec3(3.6, -0.8, -2)
)

// buildFixture assembles the twelve scenes and the four live transitions.
// It performs no I/O and takes no arguments so the Go test can assert
// byte-for-byte determinism.
func buildFixture() fixture {
	onCaster := func() scene.Node { return casterMesh(1, scene.AlphaCutoff{}, true) }

	noCaster := func() scene.SceneIR {
		return build(spotLight(lightID, basePos, baseDir, true), casterMesh(1, scene.AlphaCutoff{}, false))
	}
	noReceiver := func() scene.SceneIR {
		return scene.Props{Graph: scene.NewGraph(
			receiverNode(false), ambientLight(),
			spotLight(lightID, basePos, baseDir, true), casterMesh(1, scene.AlphaCutoff{}, true),
		)}.SceneIR()
	}
	invalidPrefix := func() scene.SceneIR {
		return build(
			blackSpot(blackSpot1, scene.Vec3(-2.5, 1.8, 2.2)),
			blackSpot(blackSpot2, scene.Vec3(-0.4, 1.8, 2.6)),
			spotLight(lightID, basePos, baseDir, true),
			casterMesh(1, scene.AlphaCutoff{}, true),
		)
	}

	off := build(spotLight(lightID, basePos, baseDir, false), onCaster())
	on := build(spotLight(lightID, basePos, baseDir, true), onCaster())
	// ambient-only removes ONLY the primary spot: the shared caster and
	// receiver geometry and the ambient stay identical to "on".
	ambientOnly := build(onCaster())
	moved := build(spotLight(lightID, movedPos, baseDir, true), onCaster())
	movedOff := build(spotLight(lightID, movedPos, baseDir, false), onCaster())
	aimed := build(spotLight(lightID, basePos, aimedDir, true), onCaster())
	aimedOff := build(spotLight(lightID, basePos, aimedDir, false), onCaster())

	return fixture{
		Schema: schema,
		Scenes: map[string]scene.SceneIR{
			"off":            off,
			"on":             on,
			"ambient-only":   ambientOnly,
			"no-caster":      noCaster(),
			"no-receiver":    noReceiver(),
			"discarded":      build(spotLight(lightID, basePos, baseDir, true), casterMesh(0.25, scene.Cutoff(0.5), true)),
			"equal":          build(spotLight(lightID, basePos, baseDir, true), casterMesh(0.5, scene.Cutoff(0.5), true)),
			"moved":          moved,
			"moved-off":      movedOff,
			"aimed":          aimed,
			"aimed-off":      aimedOff,
			"invalid-prefix": invalidPrefix(),
		},
		Transitions: []transition{
			{Name: "off-to-on", From: "off", To: "on", Commands: scene.DiffCommands(off, on)},
			{Name: "on-to-moved", From: "on", To: "moved", Commands: scene.DiffCommands(on, moved)},
			{Name: "moved-to-aimed", From: "moved", To: "aimed", Commands: scene.DiffCommands(moved, aimed)},
			{Name: "aimed-to-off", From: "aimed", To: "off", Commands: scene.DiffCommands(aimed, off)},
		},
	}
}

func main() {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(buildFixture()); err != nil {
		fmt.Fprintf(os.Stderr, "spot-shadow-typed-fixture: %v\n", err)
		os.Exit(1)
	}
}
