// Command point-shadow-typed-fixture emits the typed point-light shadow
// lifecycle fixture for the browser point-shadow tests. It lowers every
// scene case through the real scene.Props -> SceneIR pipeline and derives
// transition commands from the real scene diff. It prints exactly one JSON
// object on stdout; errors go to stderr with a non-zero exit.
//
// NOTE: native point-shadow proof is only claimed after the corresponding
// browser test actually runs against this fixture. This fixture carries no
// shadow matrices or shader math.
package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"

	"m31labs.dev/gosx/scene"
)

const (
	schema       = "gosx.point-shadow.fixture.v1"
	receiverID   = "typed-point-receiver"
	casterID     = "typed-point-caster"
	lightID      = "typed-point-key"
	ambientID    = "typed-point-ambient"
	blackPointID = "typed-point-black"
	blackDirID   = "typed-point-black-dir"
)

type transition struct {
	Name     string          `json:"name"`
	From     string          `json:"from"`
	To       string          `json:"to"`
	Commands []scene.Command `json:"commands"`
}

type cameraJSON struct {
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Z         float64 `json:"z"`
	RotationX float64 `json:"rotationX"`
	RotationY float64 `json:"rotationY"`
	RotationZ float64 `json:"rotationZ"`
	FOV       float64 `json:"fov"`
}

type faceFixture struct {
	Camera      cameraJSON               `json:"camera"`
	Scenes      map[string]scene.SceneIR `json:"scenes"`
	Transitions []transition             `json:"transitions"`
}

type fixture struct {
	Schema string                 `json:"schema"`
	Faces  map[string]faceFixture `json:"faces"`
}

type vec [3]float64

// faceSpec carries the exact axis-permuted world coordinates for one face.
// Each permutation has determinant +1 and keeps the boxes axis-aligned, so
// only positions and positive box dimensions change; ambient has no position
// and point lights need no direction.
type faceSpec struct {
	name         string
	cam          vec
	rotX, rotY   float64
	receiverPos  vec
	receiverDims vec
	casterPos    vec
	casterDims   vec
	keyPos       vec
	keyMoved     vec
	blackPos     vec
	blackDir     vec
}

var faces = []faceSpec{
	{
		name: "nz", cam: vec{0, 0, 4},
		receiverPos: vec{0, 0, -0.5}, receiverDims: vec{3, 2.2, 0.1},
		casterPos: vec{-0.55, 0.35, 0.5}, casterDims: vec{0.55, 0.55, 0.15},
		keyPos: vec{-1.55, 1.35, 2.5}, keyMoved: vec{-0.95, 1.35, 2.5},
		blackPos: vec{2, 1, 1.5}, blackDir: vec{0.7, -0.7, -1},
	},
	{
		name: "pz", cam: vec{0, 0, -4}, rotY: math.Pi,
		receiverPos: vec{0, 0, 0.5}, receiverDims: vec{3, 2.2, 0.1},
		casterPos: vec{0.55, 0.35, -0.5}, casterDims: vec{0.55, 0.55, 0.15},
		keyPos: vec{1.55, 1.35, -2.5}, keyMoved: vec{0.95, 1.35, -2.5},
		blackPos: vec{-2, 1, -1.5}, blackDir: vec{-0.7, -0.7, 1},
	},
	{
		name: "px", cam: vec{-4, 0, 0}, rotY: -math.Pi / 2,
		receiverPos: vec{0.5, 0, 0}, receiverDims: vec{0.1, 2.2, 3},
		casterPos: vec{-0.5, 0.35, -0.55}, casterDims: vec{0.15, 0.55, 0.55},
		keyPos: vec{-2.5, 1.35, -1.55}, keyMoved: vec{-2.5, 1.35, -0.95},
		blackPos: vec{-1.5, 1, 2}, blackDir: vec{1, -0.7, 0.7},
	},
	{
		name: "nx", cam: vec{4, 0, 0}, rotY: math.Pi / 2,
		receiverPos: vec{-0.5, 0, 0}, receiverDims: vec{0.1, 2.2, 3},
		casterPos: vec{0.5, 0.35, 0.55}, casterDims: vec{0.15, 0.55, 0.55},
		keyPos: vec{2.5, 1.35, 1.55}, keyMoved: vec{2.5, 1.35, 0.95},
		blackPos: vec{1.5, 1, -2}, blackDir: vec{-1, -0.7, -0.7},
	},
	{
		name: "py", cam: vec{0, -4, 0}, rotX: math.Pi / 2,
		receiverPos: vec{0, 0.5, 0}, receiverDims: vec{3, 0.1, 2.2},
		casterPos: vec{-0.55, -0.5, 0.35}, casterDims: vec{0.55, 0.15, 0.55},
		keyPos: vec{-1.55, -2.5, 1.35}, keyMoved: vec{-0.95, -2.5, 1.35},
		blackPos: vec{2, -1.5, 1}, blackDir: vec{0.7, 1, -0.7},
	},
	{
		name: "ny", cam: vec{0, 4, 0}, rotX: -math.Pi / 2,
		receiverPos: vec{0, -0.5, 0}, receiverDims: vec{3, 0.1, 2.2},
		casterPos: vec{-0.55, 0.5, -0.35}, casterDims: vec{0.55, 0.15, 0.55},
		keyPos: vec{-1.55, 2.5, -1.35}, keyMoved: vec{-0.95, 2.5, -1.35},
		blackPos: vec{2, 1.5, -1}, blackDir: vec{0.7, -1, 0.7},
	},
}

func (f faceSpec) vec3(v vec) scene.Vector3 { return scene.Vec3(v[0], v[1], v[2]) }

func (f faceSpec) receiver(receive bool) scene.Mesh {
	wireframe := false
	return scene.Mesh{
		ID: receiverID,
		Geometry: scene.BoxGeometry{
			Width:  f.receiverDims[0],
			Height: f.receiverDims[1],
			Depth:  f.receiverDims[2],
		},
		Material: scene.StandardMaterial{
			Color:     "#ffffff",
			Roughness: 1,
			Metalness: 0,
			IOR:       scene.Float(1.5),
			Wireframe: &wireframe,
		},
		Position:      f.vec3(f.receiverPos),
		CastShadow:    false,
		ReceiveShadow: receive,
	}
}

func (f faceSpec) caster(opacity float64, cutoff scene.AlphaCutoff, cast bool) scene.Mesh {
	wireframe := false
	return scene.Mesh{
		ID:       casterID,
		Geometry: scene.BoxGeometry{Width: f.casterDims[0], Height: f.casterDims[1], Depth: f.casterDims[2]},
		Material: scene.StandardMaterial{
			Color:       "#c02020",
			Roughness:   1,
			Metalness:   0,
			IOR:         scene.Float(1.5),
			Opacity:     scene.Float(opacity),
			Wireframe:   &wireframe,
			AlphaCutoff: cutoff,
		},
		Position:      f.vec3(f.casterPos),
		CastShadow:    cast,
		ReceiveShadow: false,
	}
}

// key is the primary white point light. Only the cast flag and (for the
// moved scenes) the position vary; everything else is fixed and explicit.
func (f faceSpec) key(cast, moved bool) scene.PointLight {
	pos := f.keyPos
	if moved {
		pos = f.keyMoved
	}
	return scene.PointLight{
		ID:             lightID,
		Color:          "#ffffff",
		Intensity:      6,
		Position:       f.vec3(pos),
		Range:          6.5,
		Decay:          2,
		CastShadow:     cast,
		ShadowBias:     0.0001,
		ShadowSize:     512,
		ShadowSoftness: 0,
	}
}

// blackPoint is a shadow-requesting but visually inert point light used to
// probe slot ordering. Black color plus nonzero intensity keeps the wire
// omitempty from ever resurrecting an intended zero, and its distinct
// position guarantees the light matrices stay distinct from the key.
func (f faceSpec) blackPoint() scene.PointLight {
	return scene.PointLight{
		ID:         blackPointID,
		Color:      "#000000",
		Intensity:  0.5,
		Position:   f.vec3(f.blackPos),
		CastShadow: true,
	}
}

func (f faceSpec) blackDirectional() scene.DirectionalLight {
	return scene.DirectionalLight{
		ID:         blackDirID,
		Color:      "#000000",
		Intensity:  0.5,
		Direction:  f.vec3(f.blackDir),
		CastShadow: true,
		ShadowSize: 256,
	}
}

func (f faceSpec) build(receive bool, extra ...scene.Node) scene.SceneIR {
	nodes := append([]scene.Node{f.receiver(receive), ambientLight()}, extra...)
	return scene.Props{Graph: scene.NewGraph(nodes...)}.SceneIR()
}

func ambientLight() scene.AmbientLight {
	return scene.AmbientLight{ID: ambientID, Color: "#404040", Intensity: 0.3}
}

func buildFace(f faceSpec) faceFixture {
	out := faceFixture{
		Camera: cameraJSON{
			X: f.cam[0], Y: f.cam[1], Z: f.cam[2],
			RotationX: f.rotX, RotationY: f.rotY,
			FOV: 50,
		},
		Scenes: map[string]scene.SceneIR{
			"off":          f.build(true, f.key(false, false), f.caster(1, scene.AlphaCutoff{}, true)),
			"on":           f.build(true, f.key(true, false), f.caster(1, scene.AlphaCutoff{}, true)),
			"ambient-only": f.build(true, f.caster(1, scene.AlphaCutoff{}, true)),
		},
	}
	if f.name != "nz" {
		return out
	}
	off := out.Scenes["off"]
	on := out.Scenes["on"]
	moved := f.build(true, f.key(true, true), f.caster(1, scene.AlphaCutoff{}, true))
	out.Scenes["no-caster"] = f.build(true, f.key(true, false), f.caster(1, scene.AlphaCutoff{}, false))
	out.Scenes["no-receiver"] = f.build(false, f.key(true, false), f.caster(1, scene.AlphaCutoff{}, true))
	out.Scenes["discarded"] = f.build(true, f.key(true, false), f.caster(0.25, scene.Cutoff(0.5), true))
	out.Scenes["equal"] = f.build(true, f.key(true, false), f.caster(0.5, scene.Cutoff(0.5), true))
	out.Scenes["moved"] = moved
	out.Scenes["moved-off"] = f.build(true, f.key(false, true), f.caster(1, scene.AlphaCutoff{}, true))
	out.Scenes["slot1"] = f.build(true, f.blackPoint(), f.key(true, false), f.caster(1, scene.AlphaCutoff{}, true))
	out.Scenes["slot0-paired"] = f.build(true, f.key(true, false), f.blackPoint(), f.caster(1, scene.AlphaCutoff{}, true))
	out.Scenes["mixed-slot1"] = f.build(true, f.blackDirectional(), f.key(true, false), f.caster(1, scene.AlphaCutoff{}, true))
	out.Transitions = []transition{
		{Name: "off-to-on", From: "off", To: "on", Commands: scene.DiffCommands(off, on)},
		{Name: "on-to-moved", From: "on", To: "moved", Commands: scene.DiffCommands(on, moved)},
		{Name: "moved-to-off", From: "moved", To: "off", Commands: scene.DiffCommands(moved, off)},
	}
	return out
}

// buildFixture assembles all six faces and the nz-only live transitions.
// It performs no I/O and takes no arguments so the Go test can assert
// byte-for-byte determinism.
func buildFixture() fixture {
	fx := fixture{Schema: schema, Faces: make(map[string]faceFixture, len(faces))}
	for _, f := range faces {
		fx.Faces[f.name] = buildFace(f)
	}
	return fx
}

func main() {
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(buildFixture()); err != nil {
		fmt.Fprintf(os.Stderr, "point-shadow-typed-fixture: %v\n", err)
		os.Exit(1)
	}
}
