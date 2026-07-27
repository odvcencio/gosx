package physics

import (
	"encoding/json"
	"fmt"
	"hash"
	"hash/fnv"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"testing"
)

// Native-to-WebAssembly parity corpus.
//
// A server runs the authoritative simulation as a native binary. A browser runs
// the prediction as a js/wasm binary. Rollback netcode is only correct when the
// two produce the same floats from the same inputs. This corpus proves that.
//
// The proof works in three parts:
//
//   - A scenario is pure data (WorldSpec), so both targets build the same world.
//   - A digest covers every body float of every step, so a divergence at any
//     step fails, not only at the sampled steps.
//   - Sparse samples record the exact bit patterns, so a failure names the step,
//     the body and the field that moved.
//
// Every float is stored as its IEEE 754 bit pattern in hexadecimal. Decimal
// text would make the test depend on float formatting, and the claim under test
// is bit equality, not printed equality.
//
// The claim holds for a build that does not fuse a floating point multiply and
// add. That covers js/wasm, which has no fused multiply-add instruction, and
// amd64 at GOAMD64 v2 or lower, which is the Go default. See the fused
// multiply-add section below for the guard that reports a fusing build.
//
// Run `make test-physics-parity` to check both targets. Set
// PHYSICS_PARITY_REGEN=1 to rewrite the corpus from the scenario builders.

// parityFieldCount is the number of floats recorded for one body per step.
const parityFieldCount = 13

// parityFieldNames names those floats in record order, so a mismatch report can
// say which field moved instead of printing an index.
var parityFieldNames = [parityFieldCount]string{
	"position.x", "position.y", "position.z",
	"rotation.x", "rotation.y", "rotation.z", "rotation.w",
	"velocity.x", "velocity.y", "velocity.z",
	"angularVelocity.x", "angularVelocity.y", "angularVelocity.z",
}

// parityBody is one body's recorded state at one sampled step.
type parityBody struct {
	ID    string   `json:"id,omitempty"`
	Index int      `json:"index"`
	Bits  []string `json:"bits"`
}

// paritySample is the full world state at one sampled step.
type paritySample struct {
	Step   int          `json:"step"`
	Bodies []parityBody `json:"bodies"`
}

// parityCase is one corpus file: a scenario plus the results a run must
// reproduce exactly.
type parityCase struct {
	Name string `json:"name"`
	// Why records the paths the scenario stresses. It exists so a later reader
	// can tell whether deleting the case loses coverage.
	Why string `json:"why"`
	// GeneratedBy records the toolchain and the architecture of the run that
	// wrote the file. Go contracts a float multiply-add into a fused
	// multiply-add on some architectures and not on others, so the recording
	// architecture is part of the claim.
	GeneratedBy string `json:"generatedBy"`

	// FMAContraction records whether the generating build fused a floating
	// point multiply and add into one instruction. A build that fuses cannot
	// reproduce a corpus that a build without fusion recorded, and the reverse.
	FMAContraction bool `json:"fmaContraction"`

	Steps       int `json:"steps"`
	SampleEvery int `json:"sampleEvery"`

	Scenario WorldSpec `json:"scenario"`
	// SpecDigest covers every decoded scenario value. It fails when a JSON
	// round trip changes one input bit, which would make the rest of the
	// comparison meaningless.
	SpecDigest string `json:"specDigest"`

	// StateDigest covers every body float of every step, not only the samples.
	StateDigest string `json:"stateDigest"`
	// ContactSteps counts the steps that produced at least one manifold.
	// ContactPoints sums the manifold points over the whole run. Both must
	// match, which makes the broadphase order, the GJK and EPA search and the
	// manifold reduction part of the parity claim, not only the body state.
	ContactSteps  int `json:"contactSteps"`
	ContactPoints int `json:"contactPoints"`

	Samples []paritySample `json:"samples"`
}

// parityRun is what one replay of a scenario produced.
type parityRun struct {
	stateDigest   string
	contactSteps  int
	contactPoints int
	samples       []paritySample
	initial       []parityBody
	final         []parityBody
}

// ---------------------------------------------------------------------------
// Bit helpers
// ---------------------------------------------------------------------------

// bitsHex renders a float64 as its 16 hexadecimal IEEE 754 digits.
func bitsHex(v float64) string {
	return fmt.Sprintf("%016x", math.Float64bits(v))
}

// digest accumulates float bits into a stable 64-bit fingerprint. FNV-1a is
// chosen because it is pure Go, has no table, and lowers identically on every
// target, so the digest itself cannot be the thing that diverges.
type digest struct {
	h hash.Hash64
}

func newDigest() *digest {
	return &digest{h: fnv.New64a()}
}

func (d *digest) float(v float64) {
	bits := math.Float64bits(v)
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[i] = byte(bits >> (8 * i))
	}
	d.h.Write(buf[:])
}

func (d *digest) int(v int) {
	d.uint(uint64(v))
}

func (d *digest) uint(v uint64) {
	var buf [8]byte
	for i := 0; i < 8; i++ {
		buf[i] = byte(v >> (8 * i))
	}
	d.h.Write(buf[:])
}

func (d *digest) str(s string) {
	d.uint(uint64(len(s)))
	d.h.Write([]byte(s))
}

func (d *digest) sum() string {
	return fmt.Sprintf("%016x", d.h.Sum64())
}

// ---------------------------------------------------------------------------
// Fused multiply-add detection
// ---------------------------------------------------------------------------

// The Go specification lets an implementation fuse a floating point multiply and
// the add that consumes it into one instruction that rounds once instead of
// twice. The result differs from the unfused result by up to one unit in the
// last place, and a physics step feeds that difference into the next step.
//
// Go fuses on amd64 only when GOAMD64 is v3 or higher, fuses on arm64 always,
// and never fuses for js/wasm, because WebAssembly has no fused multiply-add
// instruction. A parity corpus is therefore valid for one fusion state only.
// The probe below reads that state at run time, so a mismatch reports the cause
// instead of an unexplained digest difference.

// fmaProbeX and fmaProbeY multiply to 1 + 2^-26 + 2^-54. The product needs 54
// fraction bits, so rounding it to 53 bits drops the 2^-54 term. Subtracting one
// then leaves 2^-26 without fusion and 2^-26 + 2^-54 with fusion.
var (
	fmaProbeX = 1 + math.Ldexp(1, -27)
	fmaProbeY = 1 + math.Ldexp(1, -27)
	fmaProbeZ = -1.0
)

// fmaProbeMulAdd is the shape the compiler may fuse. Inlining is off so the
// probe cannot be folded into a constant.
//
//go:noinline
func fmaProbeMulAdd(x, y, z float64) float64 { return x*y + z }

// fmaProbeRounded forces the product to round before the add. An explicit
// conversion to the same type is the documented way to stop fusion.
//
//go:noinline
func fmaProbeRounded(x, y, z float64) float64 { return float64(x*y) + z }

// fmaContractionActive reports whether this build fuses a multiply and an add.
func fmaContractionActive() bool {
	fused := fmaProbeMulAdd(fmaProbeX, fmaProbeY, fmaProbeZ)
	rounded := fmaProbeRounded(fmaProbeX, fmaProbeY, fmaProbeZ)
	return fused != rounded
}

// TestFusedMultiplyAddProbeIsSound checks the probe itself. A probe whose two
// arms agree on every build would report "no fusion" forever and would let the
// real cause of a parity failure stay hidden.
func TestFusedMultiplyAddProbeIsSound(t *testing.T) {
	rounded := fmaProbeRounded(fmaProbeX, fmaProbeY, fmaProbeZ)
	reference := math.FMA(fmaProbeX, fmaProbeY, fmaProbeZ)
	if rounded == reference {
		t.Fatalf("probe inputs cannot tell fusion apart: rounded and fused both give %016x; pick inputs whose product needs more than 53 fraction bits",
			math.Float64bits(rounded))
	}
	fused := fmaProbeMulAdd(fmaProbeX, fmaProbeY, fmaProbeZ)
	if fused != rounded && fused != reference {
		t.Fatalf("probe gave %016x, which is neither the rounded result %016x nor the fused result %016x",
			math.Float64bits(fused), math.Float64bits(rounded), math.Float64bits(reference))
	}
	t.Logf("%s %s/%s fuses multiply-add: %v", runtime.Version(), runtime.GOOS, runtime.GOARCH, fmaContractionActive())
}

// ---------------------------------------------------------------------------
// Scenario digest
// ---------------------------------------------------------------------------

// digestSpec walks a WorldSpec in declaration order and feeds every leaf value
// into d. The walk uses reflection so a new spec field joins the digest without
// an edit here; a forgotten field would silently leave an input unchecked.
func digestSpec(d *digest, v reflect.Value) {
	switch v.Kind() {
	case reflect.Struct:
		d.str("{")
		for i := 0; i < v.NumField(); i++ {
			d.str(v.Type().Field(i).Name)
			digestSpec(d, v.Field(i))
		}
		d.str("}")
	case reflect.Slice, reflect.Array:
		d.int(v.Len())
		for i := 0; i < v.Len(); i++ {
			digestSpec(d, v.Index(i))
		}
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			d.str("nil")
			return
		}
		digestSpec(d, v.Elem())
	case reflect.Float64, reflect.Float32:
		d.float(v.Float())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		d.int(int(v.Int()))
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		d.uint(v.Uint())
	case reflect.String:
		d.str(v.String())
	case reflect.Bool:
		if v.Bool() {
			d.str("true")
		} else {
			d.str("false")
		}
	default:
		panic("physics parity: scenario field kind " + v.Kind().String() + " has no digest rule")
	}
}

func specDigest(spec WorldSpec) string {
	d := newDigest()
	digestSpec(d, reflect.ValueOf(spec))
	return d.sum()
}

// ---------------------------------------------------------------------------
// Replay
// ---------------------------------------------------------------------------

// recordBody captures one body's 13 floats in the canonical order.
func recordBody(body *RigidBody) parityBody {
	return parityBody{
		ID:    body.ID,
		Index: body.index,
		Bits: []string{
			bitsHex(body.Position.X), bitsHex(body.Position.Y), bitsHex(body.Position.Z),
			bitsHex(body.Rotation.X), bitsHex(body.Rotation.Y), bitsHex(body.Rotation.Z), bitsHex(body.Rotation.W),
			bitsHex(body.Velocity.X), bitsHex(body.Velocity.Y), bitsHex(body.Velocity.Z),
			bitsHex(body.AngularVelocity.X), bitsHex(body.AngularVelocity.Y), bitsHex(body.AngularVelocity.Z),
		},
	}
}

func recordBodies(bodies []*RigidBody) []parityBody {
	out := make([]parityBody, 0, len(bodies))
	for _, body := range bodies {
		if body == nil {
			continue
		}
		out = append(out, recordBody(body))
	}
	return out
}

func feedBody(d *digest, body *RigidBody) {
	d.float(body.Position.X)
	d.float(body.Position.Y)
	d.float(body.Position.Z)
	d.float(body.Rotation.X)
	d.float(body.Rotation.Y)
	d.float(body.Rotation.Z)
	d.float(body.Rotation.W)
	d.float(body.Velocity.X)
	d.float(body.Velocity.Y)
	d.float(body.Velocity.Z)
	d.float(body.AngularVelocity.X)
	d.float(body.AngularVelocity.Y)
	d.float(body.AngularVelocity.Z)
}

// runParityScenario builds the world from spec and advances it by steps fixed
// steps. Generation and verification both call this function, so the recording
// rule cannot drift between writing the corpus and checking it.
func runParityScenario(spec WorldSpec, steps, sampleEvery int) parityRun {
	if sampleEvery <= 0 {
		sampleEvery = 1
	}
	world := BuildWorld(spec)
	bodies := world.bodies

	run := parityRun{initial: recordBodies(bodies)}
	d := newDigest()

	for step := 1; step <= steps; step++ {
		world.StepFixed()

		manifolds := 0
		for i := range world.contacts {
			manifolds += world.contacts[i].PointCount
		}
		if len(world.contacts) > 0 {
			run.contactSteps++
		}
		run.contactPoints += manifolds
		// The counts join the digest so a contact that appears on one target
		// only fails even when the body state has not drifted yet.
		d.int(len(world.contacts))
		d.int(manifolds)

		for _, body := range bodies {
			if body == nil {
				continue
			}
			feedBody(d, body)
		}

		if step%sampleEvery == 0 || step == steps {
			run.samples = append(run.samples, paritySample{Step: step, Bodies: recordBodies(bodies)})
		}
	}

	run.stateDigest = d.sum()
	run.final = recordBodies(bodies)
	return run
}

// ---------------------------------------------------------------------------
// Scenarios
// ---------------------------------------------------------------------------

// parityScenarios returns every corpus case. The bodies are authored here from
// a fixed seed; the resulting numbers are frozen into the JSON, so a replay
// never calls the random generator.
func parityScenarios() []parityCase {
	return []parityCase{
		parityTumblingMixedShapes(),
		parityConvexHullPile(),
		parityBoxManifoldTower(),
		parityConstraintChain(),
		parityContinuousCollision(),
		paritySleepAndWake(),
		parityMeshTerrain(),
	}
}

func randomUnitQuatForParity(rng *rand.Rand) Quat {
	// Marsaglia's method gives a uniform rotation from four normals. The
	// explicit Normalize keeps the stored quaternion a unit quaternion, which
	// is what the inertia tensor rotation assumes.
	return Quat{
		X: rng.NormFloat64(),
		Y: rng.NormFloat64(),
		Z: rng.NormFloat64(),
		W: rng.NormFloat64(),
	}.Normalize()
}

// parityTumblingMixedShapes extends the existing determinism scenario across
// targets: twelve mixed-shape bodies tumble onto a plane for 600 steps. It is
// the widest case, because it drives the inertia tensor rotation, the box, the
// sphere and the cylinder narrowphase, the warm-start cache and the sleep pass
// at once.
func parityTumblingMixedShapes() parityCase {
	rng := rand.New(rand.NewSource(99))
	spec := WorldSpec{
		Config: WorldConfig{Gravity: Vec3{Y: -10}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10},
		Static: []ColliderConfig{{Shape: ShapePlane, Normal: Vec3{Y: 1}}},
	}
	for i := 0; i < 12; i++ {
		body := BodySpec{Body: BodyConfig{
			ID:   "tumble-" + strconv.Itoa(i),
			Mass: 1 + rng.Float64(),
			Position: Vec3{
				X: (rng.Float64() - 0.5) * 3,
				Y: 1 + float64(i)*0.8,
				Z: (rng.Float64() - 0.5) * 3,
			},
			Rotation:        randomUnitQuatForParity(rng),
			Velocity:        Vec3{X: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
			AngularVelocity: Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
			Friction:        0.4,
			Restitution:     0.2,
		}}
		switch i % 3 {
		case 0:
			body.Colliders = []ColliderConfig{{Shape: ShapeBox, Width: 0.8, Height: 0.8, Depth: 0.8}}
		case 1:
			body.Colliders = []ColliderConfig{{Shape: ShapeSphere, Radius: 0.4}}
		default:
			body.Colliders = []ColliderConfig{{Shape: ShapeCylinder, Radius: 0.35, Height: 0.7}}
		}
		spec.Bodies = append(spec.Bodies, body)
	}
	return parityCase{
		Name:        "tumbling_mixed_shapes",
		Why:         "inertia tensor rotation, box and sphere and cylinder narrowphase, warm-start cache, sleep pass",
		Steps:       600,
		SampleEvery: 60,
		Scenario:    spec,
	}
}

// parityConvexHullPile drops random convex hulls onto a static box and a plane.
// Hull against hull is the only path that runs the full GJK search and the EPA
// expansion, and EPA keeps an expanding polytope whose face order decides the
// reported normal.
func parityConvexHullPile() parityCase {
	rng := rand.New(rand.NewSource(4242))
	spec := WorldSpec{
		Config: WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 120.0, SolverIterations: 12},
		Static: []ColliderConfig{
			{Shape: ShapePlane, Normal: Vec3{Y: 1}},
			{Shape: ShapeBox, Offset: Vec3{Y: 0.25}, Width: 4, Height: 0.5, Depth: 4},
		},
	}
	for i := 0; i < 8; i++ {
		// A hull of 10 points on a jittered sphere gives faces of unequal size,
		// so EPA has to choose between close candidates instead of hitting a
		// symmetric tie.
		hull := make([]Vec3, 0, 10)
		for p := 0; p < 10; p++ {
			dir := Vec3{X: rng.NormFloat64(), Y: rng.NormFloat64(), Z: rng.NormFloat64()}.Normalize()
			hull = append(hull, dir.Mul(0.25+0.15*rng.Float64()))
		}
		spec.Bodies = append(spec.Bodies, BodySpec{
			Body: BodyConfig{
				ID:              "hull-" + strconv.Itoa(i),
				Mass:            1.5,
				Position:        Vec3{X: (rng.Float64() - 0.5) * 1.2, Y: 0.9 + float64(i)*0.7, Z: (rng.Float64() - 0.5) * 1.2},
				Rotation:        randomUnitQuatForParity(rng),
				AngularVelocity: Vec3{X: rng.Float64() - 0.5, Y: rng.Float64() - 0.5, Z: rng.Float64() - 0.5},
				Friction:        0.5,
				Restitution:     0.1,
				Inertia:         Vec3{X: 0.05, Y: 0.05, Z: 0.05},
			},
			Colliders: []ColliderConfig{{Shape: ShapeConvexHull, Vertices: hull}},
		})
	}
	return parityCase{
		Name:        "convex_hull_pile",
		Why:         "GJK simplex search and EPA polytope expansion for hull against hull and hull against box",
		Steps:       480,
		SampleEvery: 48,
		Scenario:    spec,
	}
}

// parityBoxManifoldTower stacks boxes face to face and nudges the tower. Face
// against face contact produces more candidate points than a manifold can hold,
// so every step runs the clip and the reduction that picks four of them.
func parityBoxManifoldTower() parityCase {
	spec := WorldSpec{
		Config: WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60.0, SolverIterations: 14},
		Static: []ColliderConfig{{Shape: ShapePlane, Normal: Vec3{Y: 1}}},
	}
	for i := 0; i < 6; i++ {
		// A small yaw per level stops the boxes from sharing one clip plane, so
		// the reduction has to rank candidates rather than take the first four.
		yaw := 0.07 * float64(i)
		spec.Bodies = append(spec.Bodies, BodySpec{
			Body: BodyConfig{
				ID:          "brick-" + strconv.Itoa(i),
				Mass:        2,
				Position:    Vec3{X: 0.02 * float64(i), Y: 0.5 + float64(i)*1.02, Z: -0.01 * float64(i)},
				Rotation:    QuatFromAxisAngle(Vec3{Y: 1}, yaw),
				Friction:    0.6,
				Restitution: 0,
			},
			Colliders: []ColliderConfig{{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1}},
		})
	}
	// A sideways striker arrives after the tower has settled and reopens the
	// deep contacts, so the run covers both a resting and a disturbed manifold.
	spec.Bodies = append(spec.Bodies, BodySpec{
		Body: BodyConfig{
			ID:          "striker",
			Mass:        4,
			Position:    Vec3{X: -6, Y: 3.2},
			Velocity:    Vec3{X: 7},
			Friction:    0.3,
			Restitution: 0.2,
		},
		Colliders: []ColliderConfig{{Shape: ShapeBox, Width: 0.9, Height: 0.9, Depth: 0.9}},
	})
	return parityCase{
		Name:        "box_manifold_tower",
		Why:         "box against box clip, four-point manifold reduction, deep penetration position pass",
		Steps:       540,
		SampleEvery: 54,
		Scenario:    spec,
	}
}

// parityConstraintChain hangs a chain from an immovable anchor with one joint of
// every kind. The solver accumulates constraint impulses over fourteen
// iterations per step, so a single differing rounding compounds fast.
func parityConstraintChain() parityCase {
	spec := WorldSpec{
		Config: WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60.0, SolverIterations: 14},
	}
	// Body 1 is the anchor. Mass zero makes it immovable.
	spec.Bodies = append(spec.Bodies, BodySpec{Body: BodyConfig{ID: "anchor", Mass: 0, Position: Vec3{Y: 6}}})
	for i := 0; i < 4; i++ {
		spec.Bodies = append(spec.Bodies, BodySpec{
			Body: BodyConfig{
				ID:          "link-" + strconv.Itoa(i),
				Mass:        1 + 0.25*float64(i),
				Position:    Vec3{X: 0.6 * float64(i+1), Y: 6 - 0.9*float64(i+1)},
				Rotation:    QuatFromAxisAngle(Vec3{X: 1, Y: 1, Z: 0}, 0.3*float64(i+1)),
				Friction:    0.3,
				Restitution: 0.1,
			},
			Colliders: []ColliderConfig{{Shape: ShapeBox, Width: 0.4, Height: 0.4, Depth: 0.4}},
		})
	}
	spec.Constraints = []ConstraintSpec{
		{Kind: "point", BodyAID: "anchor", BodyBID: "link-0", AttachA: Vec3{X: 0.3}, AttachB: Vec3{X: -0.3}},
		{Kind: "hinge", BodyAID: "link-0", BodyBID: "link-1",
			AttachA: Vec3{X: 0.25}, AttachB: Vec3{X: -0.25},
			AxisA: Vec3{Z: 1}, AxisB: Vec3{Z: 1},
			MotorEnabled: true, MotorSpeed: 1.5, MaxMotorTorque: 6,
			LimitEnabled: true, LowerLimit: -0.9, UpperLimit: 0.9},
		{Kind: "distance", BodyAID: "link-1", BodyBID: "link-2", Distance: 0.8, Softness: 0.15},
		{Kind: "fixed", BodyAID: "link-2", BodyBID: "link-3", AttachA: Vec3{X: 0.2}, AttachB: Vec3{X: -0.2}},
	}
	// A ball swings into the chain so the constraints and the contacts fight in
	// the same iteration loop.
	spec.Bodies = append(spec.Bodies, BodySpec{
		Body: BodyConfig{ID: "wrecker", Mass: 6, Position: Vec3{X: 5, Y: 4.2}, Velocity: Vec3{X: -6}, Restitution: 0.4},
		Colliders: []ColliderConfig{
			{Shape: ShapeSphere, Radius: 0.45},
		},
	})
	spec.Static = []ColliderConfig{{Shape: ShapePlane, Normal: Vec3{Y: 1}}}
	return parityCase{
		Name:        "constraint_chain",
		Why:         "point, hinge with motor and limits, soft distance and fixed joint impulse accumulation",
		Steps:       600,
		SampleEvery: 60,
		Scenario:    spec,
	}
}

// parityContinuousCollision fires fast bodies at thin static geometry. The
// swept pass runs a conservative advance whose loop count depends on the
// distance it computes, so a differing rounding would change the loop count and
// not only the final float.
func parityContinuousCollision() parityCase {
	spec := WorldSpec{
		Config: WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60.0, SolverIterations: 8},
		Static: []ColliderConfig{
			{Shape: ShapePlane, Normal: Vec3{Y: 1}},
			{Shape: ShapeBox, Offset: Vec3{Y: 2}, Width: 6, Height: 0.05, Depth: 6},
		},
	}
	speeds := []float64{-40, -120, -240, -600, -2000}
	for i, speed := range speeds {
		spec.Bodies = append(spec.Bodies, BodySpec{
			Body: BodyConfig{
				ID:              "bullet-" + strconv.Itoa(i),
				Mass:            0.5,
				Position:        Vec3{X: -2 + float64(i), Y: 5},
				Velocity:        Vec3{Y: speed},
				AngularVelocity: Vec3{X: 0.5 * float64(i+1)},
				Restitution:     0.35,
				Friction:        0.2,
			},
			Colliders: []ColliderConfig{{Shape: ShapeSphere, Radius: 0.05}},
		})
	}
	// A capsule and a cone add the shapes the bullet list leaves out.
	spec.Bodies = append(spec.Bodies,
		BodySpec{
			Body:      BodyConfig{ID: "capsule", Mass: 1, Position: Vec3{X: 2.4, Y: 6}, Velocity: Vec3{Y: -55}, Restitution: 0.2},
			Colliders: []ColliderConfig{{Shape: ShapeCapsule, Radius: 0.2, Height: 0.6}},
		},
		BodySpec{
			Body:      BodyConfig{ID: "cone", Mass: 1, Position: Vec3{X: -3, Y: 6.5}, Velocity: Vec3{Y: -45, X: 1.5}, Restitution: 0.15, AngularVelocity: Vec3{Z: 2}},
			Colliders: []ColliderConfig{{Shape: ShapeCone, Radius: 0.3, Height: 0.8}},
		},
	)
	return parityCase{
		Name:        "continuous_collision",
		Why:         "swept conservative advance, capsule and cone narrowphase, restitution at high speed",
		Steps:       360,
		SampleEvery: 36,
		Scenario:    spec,
	}
}

// paritySleepAndWake settles a sleep-eligible stack, then drops a body on it.
// The sleep pass reads the contact list and the wake pass writes body state, so
// the case covers the one place where a contact ordering difference changes
// whether a body integrates at all.
func paritySleepAndWake() parityCase {
	spec := WorldSpec{
		Config: WorldConfig{
			Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10,
			SleepTime: 0.4, SleepLinearSpeed: 0.06, SleepAngularSpeed: 0.12,
		},
		Static: []ColliderConfig{{Shape: ShapePlane, Normal: Vec3{Y: 1}}},
	}
	for i := 0; i < 4; i++ {
		spec.Bodies = append(spec.Bodies, BodySpec{
			Body: BodyConfig{
				ID:       "sleeper-" + strconv.Itoa(i),
				Mass:     1.5,
				Position: Vec3{Y: 0.5 + float64(i)*1.01},
				CanSleep: true,
				Friction: 0.7,
			},
			Colliders: []ColliderConfig{{Shape: ShapeBox, Width: 1, Height: 1, Depth: 1}},
		})
	}
	// The waker starts high enough to arrive after the stack has slept.
	spec.Bodies = append(spec.Bodies, BodySpec{
		Body:      BodyConfig{ID: "waker", Mass: 3, Position: Vec3{X: 0.2, Y: 22}, Friction: 0.4, Restitution: 0.3},
		Colliders: []ColliderConfig{{Shape: ShapeSphere, Radius: 0.4}},
	})
	return parityCase{
		Name:        "sleep_and_wake",
		Why:         "sleep countdown, sleep blocking through contacts, wake on arrival, warm-start cache reuse",
		Steps:       600,
		SampleEvery: 60,
		Scenario:    spec,
	}
}

// parityMeshTerrain rolls bodies down a static triangle mesh. The mesh builds a
// bounding volume hierarchy with a median partition and marks internal edges
// through a map, so the case covers the two data structures whose order could
// differ between builds.
func parityMeshTerrain() parityCase {
	const cells = 6
	const span = 9.0
	vertices := make([]Vec3, 0, (cells+1)*(cells+1))
	for iz := 0; iz <= cells; iz++ {
		for ix := 0; ix <= cells; ix++ {
			x := -span/2 + span*float64(ix)/float64(cells)
			z := -span/2 + span*float64(iz)/float64(cells)
			// A saddle gives both convex ridges and concave valleys, so the
			// internal-edge marking has real work to do.
			y := 0.35*math.Sin(0.9*x) - 0.28*math.Cos(0.7*z) - 0.06*x
			vertices = append(vertices, Vec3{X: x, Y: y, Z: z})
		}
	}
	indices := make([]int, 0, cells*cells*6)
	for iz := 0; iz < cells; iz++ {
		for ix := 0; ix < cells; ix++ {
			a := iz*(cells+1) + ix
			b := a + 1
			c := a + cells + 1
			d := c + 1
			indices = append(indices, a, c, b, b, c, d)
		}
	}
	spec := WorldSpec{
		Config: WorldConfig{Gravity: Vec3{Y: -9.81}, FixedTimestep: 1.0 / 60.0, SolverIterations: 10},
		Static: []ColliderConfig{{Shape: ShapeTriangleMesh, Vertices: vertices, Indices: indices}},
	}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 6; i++ {
		body := BodySpec{Body: BodyConfig{
			ID:              "roller-" + strconv.Itoa(i),
			Mass:            1 + 0.3*float64(i),
			Position:        Vec3{X: -3 + float64(i)*1.1, Y: 2.5 + 0.4*float64(i), Z: (rng.Float64() - 0.5) * 2},
			Rotation:        randomUnitQuatForParity(rng),
			Velocity:        Vec3{X: 1.5},
			AngularVelocity: Vec3{Z: -3},
			Friction:        0.45,
			Restitution:     0.25,
		}}
		if i%2 == 0 {
			body.Colliders = []ColliderConfig{{Shape: ShapeSphere, Radius: 0.3}}
		} else {
			body.Colliders = []ColliderConfig{{Shape: ShapeBox, Width: 0.5, Height: 0.5, Depth: 0.5}}
		}
		spec.Bodies = append(spec.Bodies, body)
	}
	return parityCase{
		Name:        "mesh_terrain",
		Why:         "triangle mesh hierarchy walk, internal edge filtering, mesh against sphere and box contacts",
		Steps:       420,
		SampleEvery: 42,
		Scenario:    spec,
	}
}

// ---------------------------------------------------------------------------
// Corpus location
// ---------------------------------------------------------------------------

func parityCorpusDir() string {
	return filepath.Join("testdata", "parity")
}

// ---------------------------------------------------------------------------
// TestRegenerateParityCorpus — env-gated writer
// ---------------------------------------------------------------------------

// TestRegenerateParityCorpus rewrites the corpus from the scenario builders.
// Run it only after a deliberate change to the engine, and read the diff: every
// changed bit is a behaviour change that a client build must now follow.
func TestRegenerateParityCorpus(t *testing.T) {
	if os.Getenv("PHYSICS_PARITY_REGEN") == "" {
		t.Skip("set PHYSICS_PARITY_REGEN=1 to regenerate the native-to-wasm parity corpus")
	}
	dir := parityCorpusDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	for _, c := range parityScenarios() {
		run := runParityScenario(c.Scenario, c.Steps, c.SampleEvery)
		c.GeneratedBy = runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH
		c.FMAContraction = fmaContractionActive()
		c.SpecDigest = specDigest(c.Scenario)
		c.StateDigest = run.stateDigest
		c.ContactSteps = run.contactSteps
		c.ContactPoints = run.contactPoints
		c.Samples = run.samples

		if c.ContactPoints == 0 {
			t.Fatalf("%s: scenario produced no contact points; it would prove nothing about the narrowphase or the solver", c.Name)
		}
		data, err := json.MarshalIndent(c, "", "  ")
		if err != nil {
			t.Fatalf("marshal %s: %v", c.Name, err)
		}
		path := filepath.Join(dir, c.Name+".json")
		if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		t.Logf("wrote %s: %d steps, %d contact steps, %d contact points, digest %s",
			path, c.Steps, c.ContactSteps, c.ContactPoints, c.StateDigest)
	}
}

// ---------------------------------------------------------------------------
// TestParityCorpus — the gate
// ---------------------------------------------------------------------------

// TestParityCorpus replays every corpus scenario and demands bit equality.
// Run it natively and again under GOOS=js GOARCH=wasm. A pass on both targets
// is the proof that a server simulation and a client prediction agree.
func TestParityCorpus(t *testing.T) {
	dir := parityCorpusDir()
	paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		t.Fatalf("glob %s: %v", dir, err)
	}
	sort.Strings(paths)

	// A parity gate that passes on an empty corpus is worse than no gate,
	// because it reports success while checking nothing.
	const wantFiles = 7
	if len(paths) != wantFiles {
		t.Fatalf("parity corpus holds %d file(s) in %s, want %d; run PHYSICS_PARITY_REGEN=1 go test ./physics/ -run TestRegenerateParityCorpus",
			len(paths), dir, wantFiles)
	}

	totalSteps := 0
	totalContactPoints := 0
	totalFloats := 0
	fused := fmaContractionActive()

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var c parityCase
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatalf("unmarshal %s: %v", path, err)
		}

		// Reject a corpus file that could pass without simulating anything.
		switch {
		case c.Name == "":
			t.Fatalf("%s: corpus case has no name", path)
		case c.Steps < 100:
			t.Fatalf("%s: Steps = %d, want at least 100 so drift has room to appear", c.Name, c.Steps)
		case len(c.Scenario.Bodies) == 0:
			t.Fatalf("%s: scenario declares no bodies", c.Name)
		case len(c.Samples) < 2:
			t.Fatalf("%s: corpus holds %d sample(s), want at least 2", c.Name, len(c.Samples))
		case c.StateDigest == "":
			t.Fatalf("%s: corpus has no state digest", c.Name)
		case c.ContactPoints == 0:
			t.Fatalf("%s: corpus records no contact points, so the narrowphase and the solver stay unproven", c.Name)
		}
		for _, sample := range c.Samples {
			if len(sample.Bodies) == 0 {
				t.Fatalf("%s: sample at step %d records no bodies", c.Name, sample.Step)
			}
			for _, body := range sample.Bodies {
				if len(body.Bits) != parityFieldCount {
					t.Fatalf("%s: step %d body %q records %d float(s), want %d",
						c.Name, sample.Step, body.ID, len(body.Bits), parityFieldCount)
				}
			}
		}

		// Name the fusion state before comparing any float. A build that fuses
		// a multiply and an add cannot reproduce a corpus that a build without
		// fusion recorded, so reporting it first turns a wall of digest
		// differences into one actionable line.
		if fused != c.FMAContraction {
			t.Fatalf("%s: this build fuses multiply-add = %v, the corpus was recorded with %v (by %s). "+
				"Bit-exact parity with a js/wasm client needs a build that does not fuse. "+
				"On amd64 use GOAMD64 v2 or lower, which is the default; GOAMD64 v3 and higher fuse. "+
				"arm64 always fuses, so an arm64 server needs its own corpus and cannot match a browser client bit for bit",
				c.Name, fused, c.FMAContraction, c.GeneratedBy)
		}

		// Prove the inputs survived the JSON round trip before comparing any
		// output. A changed input bit would make an output mismatch a lie.
		if got := specDigest(c.Scenario); got != c.SpecDigest {
			t.Fatalf("%s: scenario digest = %s, want %s; a JSON round trip changed an input value on %s/%s",
				c.Name, got, c.SpecDigest, runtime.GOOS, runtime.GOARCH)
		}

		run := runParityScenario(c.Scenario, c.Steps, c.SampleEvery)

		if run.contactSteps != c.ContactSteps || run.contactPoints != c.ContactPoints {
			t.Errorf("%s: contact counts differ on %s/%s: got %d contact step(s) and %d point(s), want %d and %d; the broadphase order, the GJK and EPA search or the manifold reduction diverged",
				c.Name, runtime.GOOS, runtime.GOARCH,
				run.contactSteps, run.contactPoints, c.ContactSteps, c.ContactPoints)
		}

		// Report the first differing sample field. The digest already knows the
		// run diverged; the samples say where.
		reported := 0
		for si, want := range c.Samples {
			if si >= len(run.samples) {
				t.Errorf("%s: run produced %d sample(s), corpus holds %d", c.Name, len(run.samples), len(c.Samples))
				break
			}
			got := run.samples[si]
			if got.Step != want.Step {
				t.Errorf("%s: sample %d is step %d, corpus says step %d", c.Name, si, got.Step, want.Step)
				break
			}
			if len(got.Bodies) != len(want.Bodies) {
				t.Errorf("%s: step %d holds %d body state(s), corpus holds %d",
					c.Name, want.Step, len(got.Bodies), len(want.Bodies))
				break
			}
			for bi := range want.Bodies {
				wantBody := want.Bodies[bi]
				gotBody := got.Bodies[bi]
				if gotBody.ID != wantBody.ID || gotBody.Index != wantBody.Index {
					t.Errorf("%s: step %d slot %d is body %q index %d, corpus says %q index %d",
						c.Name, want.Step, bi, gotBody.ID, gotBody.Index, wantBody.ID, wantBody.Index)
					reported++
					break
				}
				for fi := range wantBody.Bits {
					if gotBody.Bits[fi] == wantBody.Bits[fi] {
						continue
					}
					gotV := hexToFloat(t, gotBody.Bits[fi])
					wantV := hexToFloat(t, wantBody.Bits[fi])
					t.Errorf("%s: step %d body %q %s: got %s (%.17g), want %s (%.17g); absolute difference %g on %s/%s",
						c.Name, want.Step, wantBody.ID, parityFieldNames[fi],
						gotBody.Bits[fi], gotV, wantBody.Bits[fi], wantV,
						math.Abs(gotV-wantV), runtime.GOOS, runtime.GOARCH)
					reported++
					if reported >= 8 {
						break
					}
				}
				if reported >= 8 {
					break
				}
			}
			if reported >= 8 {
				t.Errorf("%s: stopping after %d reported difference(s)", c.Name, reported)
				break
			}
		}

		if run.stateDigest != c.StateDigest {
			t.Errorf("%s: state digest over all %d step(s) = %s, want %s (generated by %s, verified on %s %s/%s)",
				c.Name, c.Steps, run.stateDigest, c.StateDigest, c.GeneratedBy,
				runtime.Version(), runtime.GOOS, runtime.GOARCH)
		}

		// A scenario whose bodies never move would pass every comparison above
		// while proving nothing, so demand motion.
		if !parityBodiesMoved(run.initial, run.final) {
			t.Errorf("%s: no body changed state over %d steps; the scenario is inert", c.Name, c.Steps)
		}

		totalSteps += c.Steps
		totalContactPoints += c.ContactPoints
		for _, sample := range c.Samples {
			totalFloats += len(sample.Bodies) * parityFieldCount
		}
	}

	t.Logf("verified %d parity corpus file(s) on %s %s/%s (fuses multiply-add: %v): %d simulated step(s), %d recorded float(s), %d contact point(s), bit exact",
		len(paths), runtime.Version(), runtime.GOOS, runtime.GOARCH, fused, totalSteps, totalFloats, totalContactPoints)
}

func hexToFloat(t *testing.T, hex string) float64 {
	t.Helper()
	bits, err := strconv.ParseUint(hex, 16, 64)
	if err != nil {
		t.Fatalf("corpus holds %q, which is not a 64-bit hexadecimal float pattern: %v", hex, err)
	}
	return math.Float64frombits(bits)
}

func parityBodiesMoved(initial, final []parityBody) bool {
	if len(initial) != len(final) {
		return true
	}
	for i := range initial {
		for f := range initial[i].Bits {
			if initial[i].Bits[f] != final[i].Bits[f] {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Coverage guard
// ---------------------------------------------------------------------------

// TestParityCorpusCoversTheRiskyPaths fails when the corpus loses a scenario
// that reaches one of the paths most likely to diverge between targets. Without
// it, deleting a hard case would still leave a green gate.
func TestParityCorpusCoversTheRiskyPaths(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(parityCorpusDir(), "*.json"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("parity corpus is empty")
	}

	shapes := make(map[ColliderShape]bool)
	constraints := make(map[string]bool)
	sleepers := 0
	motorised := 0
	rotated := 0

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		var c parityCase
		if err := json.Unmarshal(data, &c); err != nil {
			t.Fatalf("unmarshal %s: %v", path, err)
		}
		if c.Why == "" {
			t.Errorf("%s: case records no reason for existing", c.Name)
		}
		for _, static := range c.Scenario.Static {
			shapes[static.Shape] = true
		}
		for _, body := range c.Scenario.Bodies {
			for _, collider := range body.Colliders {
				shapes[collider.Shape] = true
			}
			if body.Body.CanSleep {
				sleepers++
			}
			if body.Body.Rotation != (Quat{W: 1}) && body.Body.Rotation != (Quat{}) {
				rotated++
			}
		}
		for _, constraint := range c.Scenario.Constraints {
			constraints[constraint.Kind] = true
			if constraint.MotorEnabled {
				motorised++
			}
		}
	}

	wantShapes := []ColliderShape{
		ShapeBox, ShapeSphere, ShapeCapsule, ShapePlane,
		ShapeCylinder, ShapeCone, ShapeConvexHull, ShapeTriangleMesh,
	}
	for _, shape := range wantShapes {
		if !shapes[shape] {
			t.Errorf("no parity scenario uses shape %s", shape)
		}
	}
	for _, kind := range []string{"point", "hinge", "distance", "fixed"} {
		if !constraints[kind] {
			t.Errorf("no parity scenario uses a %s constraint", kind)
		}
	}
	if sleepers == 0 {
		t.Error("no parity scenario uses a sleep-eligible body, so the sleep pass stays unproven")
	}
	if motorised == 0 {
		t.Error("no parity scenario drives a hinge motor")
	}
	if rotated < 10 {
		t.Errorf("only %d parity body/bodies start rotated; the inertia tensor rotation needs many non-identity poses", rotated)
	}
}
