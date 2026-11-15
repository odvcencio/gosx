package physics

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"m31labs.dev/gosx/det"
)

// Cross-architecture determinism gate for the character controller, the
// queries it runs, trigger events and the bodies it pushes.
//
// One scenario runs for characterGoldenSteps fixed steps and every step feeds
// a digest: every body float, every character state value and every trigger
// event. The digest must match testdata/character_determinism.json on every
// build: amd64 at any GOAMD64 level, arm64 (which fuses multiply-adds) and
// js/wasm. The scenario itself is built only from det math with every product
// rounded, so the inputs are the same bits everywhere too.
//
// Set PHYSICS_CHARACTER_REGEN=1 to rewrite the golden file.

const characterGoldenSteps = 1000

type characterGolden struct {
	Steps       int    `json:"steps"`
	Digest      string `json:"digest"`
	GeneratedBy string `json:"generatedBy"`
}

// mul rounds a product, so the scenario arithmetic cannot fuse on arm64.
func mul(a, b float64) float64 { return float64(a * b) }

// goldenTerrain is a rolling 24 x 24 cell mesh floor built with det math.
func goldenTerrain() ColliderConfig {
	const cells = 24
	const span = 48.0
	vertices := make([]Vec3, 0, (cells+1)*(cells+1))
	for iz := 0; iz <= cells; iz++ {
		for ix := 0; ix <= cells; ix++ {
			x := -span/2 + mul(span, float64(ix)/cells)
			z := -span/2 + mul(span, float64(iz)/cells)
			y := mul(0.35, det.Sin(mul(0.3, x))) + mul(0.25, det.Cos(mul(0.22, z))) - 0.6
			vertices = append(vertices, Vec3{X: x, Y: y, Z: z})
		}
	}
	indices := make([]int, 0, cells*cells*6)
	for iz := 0; iz < cells; iz++ {
		for ix := 0; ix < cells; ix++ {
			a := iz*(cells+1) + ix
			indices = append(indices, a, a+cells+1, a+cells+2, a, a+cells+2, a+1)
		}
	}
	return ColliderConfig{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices}
}

type goldenScene struct {
	world      *World
	characters []*Character
	lift       *RigidBody
	spinner    *RigidBody
	events     []TriggerEvent
}

func buildGoldenScene() *goldenScene {
	w := NewWorld(WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60, SolverIterations: 8})
	g := &goldenScene{world: w}
	w.AddCollider(goldenTerrain())
	// Stairs, a 30 degree ramp, a too-steep ramp and a walled corner.
	for i := 0; i < 5; i++ {
		h := mul(0.2, float64(i+1))
		w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.45, Height: h + 1, Depth: 3,
			Offset: Vec3{X: 6 + mul(0.45, float64(i)), Y: mul(0.5, h+1) - 0.6, Z: 0}})
	}
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 6, Height: 0.5, Depth: 4,
		Offset: Vec3{X: -8, Y: 0.6, Z: 6}, Rotation: QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/6)})
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 6, Height: 0.5, Depth: 4,
		Offset: Vec3{X: 8, Y: 1.2, Z: -8}, Rotation: QuatFromAxisAngle(Vec3{Z: 1}, math.Pi/3)})
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.5, Height: 3, Depth: 8, Offset: Vec3{X: -12, Y: 1, Z: -6}})
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 8, Height: 3, Depth: 0.5, Offset: Vec3{X: -8, Y: 1, Z: -10}})
	w.AddCollider(ColliderConfig{Shape: ShapePlane, Normal: Vec3{Y: 1}, Distance: -3})

	g.lift = w.AddBody(BodyConfig{ID: "lift", IsKinematic: true, Position: Vec3{X: 0, Y: 0.2, Z: 8}})
	g.lift.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 3, Height: 0.25, Depth: 3})
	g.spinner = w.AddBody(BodyConfig{ID: "spinner", IsKinematic: true, Position: Vec3{X: 4, Y: 0.3, Z: 6}})
	g.spinner.AddCollider(ColliderConfig{Shape: ShapeCylinder, Radius: 2, Height: 0.3})

	for i := 0; i < 3; i++ {
		crate := w.AddBody(BodyConfig{ID: fmt.Sprintf("crate-%d", i), Mass: 15, Friction: 0.5,
			Position: Vec3{X: -2 + mul(1.4, float64(i)), Y: 1.2, Z: -3}})
		crate.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 0.9, Height: 0.9, Depth: 0.9})
	}
	w.AddCollider(ColliderConfig{Shape: ShapeBox, IsTrigger: true, Width: 3, Height: 3, Depth: 3, Offset: Vec3{X: 2, Y: 1, Z: 0}})
	w.AddCollider(ColliderConfig{Shape: ShapeSphere, IsTrigger: true, Radius: 2, Offset: Vec3{X: -6, Y: 1, Z: -6}})
	w.SetTriggerHandler(func(e TriggerEvent) { g.events = append(g.events, e) })

	configs := []CharacterConfig{
		{ID: "walker", Position: Vec3{X: 0, Y: 2, Z: 0}, JumpSpeed: 5, CoyoteTime: 0.1},
		{ID: "climber", Position: Vec3{X: 3, Y: 2, Z: 0.5}, StepHeight: 0.35, JumpSpeed: 4.5, JumpBufferTime: 0.1},
		{ID: "rider", Position: Vec3{X: 0, Y: 2, Z: 8}, Radius: 0.3, TotalHeight: 1.6, JumpSpeed: 5},
		{ID: "pusher", Position: Vec3{X: -2, Y: 2, Z: -1}, Mass: 90, MaxSlope: math.Pi / 5},
	}
	for _, config := range configs {
		g.characters = append(g.characters, w.AddCharacter(config))
	}
	return g
}

// input scripts each character from the step number alone.
func (g *goldenScene) input(step, index int) CharacterInput {
	t := float64(step) / 60
	phase := float64(index)
	speed := 2.5 + mul(0.5, phase)
	angle := mul(0.21, t) + mul(1.7, phase)
	move := Vec3{X: mul(speed, det.Cos(angle)), Z: mul(speed, det.Sin(angle))}
	if index == 2 {
		// The rider mostly stands on the lift and the spinner.
		move = move.Mul(0.15)
	}
	return CharacterInput{Move: move, Jump: (step+index*37)%150 == 0}
}

func (g *goldenScene) step(step int) {
	t := float64(step) / 60
	g.lift.Position = Vec3{X: mul(2, det.Sin(mul(0.4, t))), Y: 0.2 + mul(1.2, 0.5+mul(0.5, det.Sin(mul(0.7, t)))), Z: 8}
	g.spinner.Rotation = QuatFromAxisAngle(Vec3{Y: 1}, mul(0.8, t))
	for i, c := range g.characters {
		c.Step(1.0/60, g.input(step, i))
	}
	g.world.StepFixed()
}

// digest runs the scenario and folds every step's state into one FNV-1a hash.
func characterDigest(steps int) (string, *goldenScene) {
	g := buildGoldenScene()
	h := fnv.New64a()
	var buf [8]byte
	f := func(v float64) {
		bits := math.Float64bits(v)
		for i := range buf {
			buf[i] = byte(bits >> (8 * i))
		}
		h.Write(buf[:])
	}
	vec := func(v Vec3) { f(v.X); f(v.Y); f(v.Z) }
	for step := 0; step < steps; step++ {
		g.step(step)
		for _, body := range g.world.bodies {
			vec(body.Position)
			f(body.Rotation.X)
			f(body.Rotation.Y)
			f(body.Rotation.Z)
			f(body.Rotation.W)
			vec(body.Velocity)
			vec(body.AngularVelocity)
		}
		for _, c := range g.characters {
			s := c.State()
			vec(s.Position)
			vec(s.Velocity)
			vec(s.GroundNormal)
			vec(s.GroundAnchor)
			f(s.AirTime)
			f(s.JumpTimer)
			f(float64(s.GroundCollider))
			if s.Grounded {
				f(1)
			} else {
				f(0)
			}
		}
		for _, e := range g.events {
			f(float64(e.Step))
			f(float64(e.Phase))
			f(float64(e.Trigger.index))
			f(float64(e.Other.index))
		}
		g.events = g.events[:0]
	}
	return fmt.Sprintf("%016x", h.Sum64()), g
}

func TestCharacterDeterminismGolden(t *testing.T) {
	digest, g := characterDigest(characterGoldenSteps)
	t.Logf("%s %s/%s character digest over %d steps: %s", runtime.Version(), runtime.GOOS, runtime.GOARCH, characterGoldenSteps, digest)

	// The scenario has to exercise what it claims to.
	grounded, airborne := 0, 0
	for _, c := range g.characters {
		if c.Grounded() {
			grounded++
		} else {
			airborne++
		}
	}
	if grounded == 0 {
		t.Fatal("no character ended on the ground; the scenario is broken")
	}

	path := filepath.Join("testdata", "character_determinism.json")
	if os.Getenv("PHYSICS_CHARACTER_REGEN") == "1" {
		data, _ := json.MarshalIndent(characterGolden{
			Steps:       characterGoldenSteps,
			Digest:      digest,
			GeneratedBy: runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH,
		}, "", "  ")
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", path)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v (set PHYSICS_CHARACTER_REGEN=1 to create it)", err)
	}
	var golden characterGolden
	if err := json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	if golden.Steps != characterGoldenSteps || golden.Digest != digest {
		t.Fatalf("character digest %s over %d steps on %s/%s, golden %s over %d steps (generated by %s)",
			digest, characterGoldenSteps, runtime.GOOS, runtime.GOARCH, golden.Digest, golden.Steps, golden.GeneratedBy)
	}
}

func BenchmarkCharacterStepTerrain(b *testing.B) {
	g := buildGoldenScene()
	for step := 0; step < 120; step++ {
		g.step(step)
	}
	c := g.characters[0]
	input := CharacterInput{Move: Vec3{X: 3, Z: 1}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Step(1.0/60, input)
		if i%240 == 239 {
			c.Teleport(Vec3{Y: 2})
		}
	}
}

func BenchmarkCharacterStepIntoWall(b *testing.B) {
	w := characterFloorWorld()
	w.AddCollider(ColliderConfig{Shape: ShapeBox, Width: 1, Height: 3, Depth: 40, Offset: Vec3{X: 2, Y: 1.5}})
	c := w.AddCharacter(CharacterConfig{Position: Vec3{Y: 1}})
	for i := 0; i < 120; i++ {
		c.Step(1.0/60, CharacterInput{Move: Vec3{X: 3, Z: 0.5}})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Step(1.0/60, CharacterInput{Move: Vec3{X: 3, Z: 0.5}})
		if i%600 == 599 {
			c.Teleport(Vec3{Y: 1})
		}
	}
}

func BenchmarkGoldenSceneStep(b *testing.B) {
	g := buildGoldenScene()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		g.step(i % 3000)
		g.events = g.events[:0]
	}
}
