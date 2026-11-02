package scene

import (
	"encoding/json"
	"math"
	"os"
	"testing"

	"m31labs.dev/gosx/motion"
)

func TestGroupSpinRetainsSharedPivot(t *testing.T) {
	ir := (Props{Graph: NewGraph(Group{
		ID: "fan-rotor", Position: Vec3(2, 3, 4), Spin: Rotate(4, 0, 0),
		Children: []Node{
			Mesh{ID: "hub", Geometry: BoxGeometry{}},
			Mesh{ID: "blade", Geometry: BoxGeometry{}, Position: Vec3(0, 2, 0)},
		},
	})}).SceneIR()
	if len(ir.Groups) != 1 || ir.Groups[0].ID != "fan-rotor" || len(ir.Objects) != 2 {
		t.Fatalf("retained rotor missing: groups=%v objects=%v", ir.Groups, ir.Objects)
	}
	for _, object := range ir.Objects {
		if object.ParentNode != "fan-rotor" || object.SpinX != 0 || object.SpinY != 0 || object.SpinZ != 0 {
			t.Fatalf("child spin was copied instead of retaining its parent: %#v", object)
		}
	}
	if ir.Objects[1].X != 0 || ir.Objects[1].Y != 2 || ir.Objects[1].Z != 0 {
		t.Fatalf("blade lost its local position: %#v", ir.Objects[1])
	}
	matrices, err := GroupWorldMatrices(ir.Groups, math.Pi/8)
	if err != nil {
		t.Fatal(err)
	}
	got := affinePoint(affineMatrix(matrices["fan-rotor"]), Vec3(0, 2, 0))
	if !vectorClose(got, Vec3(2, 3, 6), 1e-12) {
		t.Fatalf("blade rotated around its own origin: got %v, want (2,3,6)", got)
	}
	if len(ir.SpinTracks) != 1 || ir.SpinTracks[0].Target.Ref != "fan-rotor" || len(ir.MotionProgram) == 0 {
		t.Fatalf("group is not in shared motion transport: %v", ir.SpinTracks)
	}
}

func TestGroupSpinNestedScaleAndStaticPrefix(t *testing.T) {
	ir := (Props{Graph: NewGraph(Group{
		Position: Vec3(5, -2, 1), Scale: Vec3(2, 3, 4),
		Children: []Node{Group{
			ID: "outer", Position: Vec3(1, 0, 0), Spin: Rotate(0, 0, 2),
			Children: []Node{Group{
				ID: "inner", Position: Vec3(2, 0, 0), Spin: Rotate(0, 0, -1), Scale: Vec3(1, 2, 1),
				Children: []Node{Mesh{ID: "tip", Geometry: BoxGeometry{}, Position: Vec3(1, 0, 0)}},
			}},
		}},
	})}).SceneIR()
	if len(ir.Groups) != 2 || ir.Groups[1].ParentNode != "outer" || ir.Objects[0].ParentNode != "inner" {
		t.Fatalf("nested retained parents lost: groups=%v objects=%v", ir.Groups, ir.Objects)
	}
	seconds := math.Pi / 4
	matrices, err := GroupWorldMatrices(ir.Groups, seconds)
	if err != nil {
		t.Fatal(err)
	}
	// Independent 2D construction: inner rotates -pi/4, outer rotates pi/2,
	// then the static ancestor scales world axes and translates.
	x, y := math.Sqrt(.5), -math.Sqrt(.5)
	x += 2
	x, y = -y, x
	x = (x+1)*2 + 5
	y = y*3 - 2
	got := affinePoint(affineMatrix(matrices["inner"]), Vec3(1, 0, 0))
	if !vectorClose(got, Vec3(x, y, 1), 1e-12) {
		t.Fatalf("nested scaled pivot = %v, want %v", got, Vec3(x, y, 1))
	}
}

func TestGroupZeroSpinKeepsFlattenedWire(t *testing.T) {
	graph := NewGraph(Group{ID: "static", Position: Vec3(2, 3, 4), Children: []Node{
		Mesh{ID: "box", Geometry: BoxGeometry{}, Position: Vec3(1, 0, 0)},
	}})
	ir := graph.SceneIR()
	if len(ir.Groups) != 0 || ir.Objects[0].ParentNode != "" || ir.Objects[0].X != 3 || ir.Objects[0].Y != 3 || ir.Objects[0].Z != 4 {
		t.Fatalf("zero spin changed static lowering: %#v", ir)
	}
	got, err := json.Marshal(ir)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(NewGraph(Mesh{ID: "box", Geometry: BoxGeometry{}, Position: Vec3(3, 3, 4)}).SceneIR())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("zero-spin group changed wire bytes\ngot %s\nwant %s", got, want)
	}
}

func TestGroupSpinPickingAtAnimatedPositions(t *testing.T) {
	graph := NewGraph(Group{ID: "rotor", Spin: Rotate(0, 0, math.Pi/2), Children: []Node{
		Mesh{ID: "blade", Geometry: BoxGeometry{Width: .4, Height: .4, Depth: .4}, Position: Vec3(2, 0, 0)},
	}})
	accelerator := NewSceneAccelerator(graph)
	for _, seconds := range []float64{0, 1, 2, 3, 4, .5} {
		angle := seconds * math.Pi / 2
		position := Vec3(2*math.Cos(angle), 2*math.Sin(angle), 0)
		ray := Ray{Origin: Vec3(position.X, position.Y, 5), Direction: Vec3(0, 0, -1)}
		walk, ok := RaycastGraph(graph, ray, AtTime(seconds))
		if !ok || walk.ID != "blade" {
			t.Fatalf("time %g: graph walk missed moving blade at %v: %+v", seconds, position, walk)
		}
		fast, ok := accelerator.Raycast(ray, AtTime(seconds))
		if !ok || fast.ID != walk.ID || math.Abs(fast.Distance-walk.Distance) > 1e-12 || !vectorClose(fast.Point, walk.Point, 1e-12) {
			t.Fatalf("time %g: accelerator differs from animated graph: walk=%+v fast=%+v", seconds, walk, fast)
		}
	}
	oldPosition := Ray{Origin: Vec3(2, 0, 5), Direction: Vec3(0, 0, -1)}
	if hit, ok := RaycastGraph(graph, oldPosition, AtTime(1)); ok {
		t.Fatalf("picked stale unrotated position: %+v", hit)
	}
}

func TestGroupWorldMatricesRejectInvalidInputs(t *testing.T) {
	for _, groups := range [][]GroupIR{
		{{ID: "a"}, {ID: "a"}}, {{ID: ""}}, {{ID: "a", ParentNode: "missing"}},
		{{ID: "a", ParentNode: "b"}, {ID: "b", ParentNode: "a"}}, {{ID: "a", SpinX: math.Inf(1)}},
		{{ID: "a", ParentMatrix: []float64{1}}}, {{ID: "a", SpinX: math.MaxFloat64}},
	} {
		if _, err := GroupWorldMatrices(groups, 2); err == nil {
			t.Errorf("accepted invalid groups: %+v", groups)
		}
	}
	if _, err := GroupWorldMatrices([]GroupIR{{ID: "a"}}, math.NaN()); err == nil {
		t.Fatal("accepted nonfinite time")
	}
	if count := testing.AllocsPerRun(20, func() { _, _ = GroupWorldMatrices(nil, 1) }); count != 0 {
		t.Fatalf("zero-spin hierarchy allocated %v times", count)
	}
}

func TestGroupSpinCanonicalIRRetainsBoundary(t *testing.T) {
	props := Props{Graph: NewGraph(Group{ID: "rotor", Spin: Rotate(0, 1, 0), Children: []Node{Mesh{ID: "tip", Position: Vec3(2, 0, 0), Geometry: BoxGeometry{}}}})}
	ir := props.CanonicalIR()
	if err := ir.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(ir.Nodes) != 2 || ir.Nodes[0].Kind != "group" || ir.Nodes[1].Transform.ParentNode != "rotor" {
		t.Fatalf("lost canonical hierarchy: %+v", ir.Nodes)
	}
}

func TestGroupSpinMotionTransportTargetsTheBoundary(t *testing.T) {
	ir := NewGraph(Group{ID: "rotor", Spin: Rotate(.4, .8, 1.2), Children: []Node{Mesh{ID: "blade", Geometry: BoxGeometry{}, Position: Vec3(2, 0, 0)}}}).SceneIR()
	timeline, targets, properties, err := motion.DecodeProgram(ir.MotionProgram)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0] != "rotor" || len(properties) != 1 || properties[0] != "rotation" {
		t.Fatalf("wrong transport targets: %v %v", targets, properties)
	}
	for _, seconds := range []float64{0, .25, 1, 5, .5} {
		buffer := motion.NewWriteBuf(32)
		motion.Eval(timeline, seconds, motion.Policy{}, buffer)
		writes := buffer.Writes()
		if len(writes) != 7 {
			t.Fatalf("expected one quaternion write: %v", writes)
		}
		expected := motion.QuatFromEuler(.4*seconds, .8*seconds, 1.2*seconds)
		for i, want := range []float64{expected.X, expected.Y, expected.Z, expected.W} {
			if math.Abs(writes[3+i]-want) > 1e-12 {
				t.Fatalf("transport spin mismatch at %v: %v", seconds, writes)
			}
		}
	}
}

func TestGroupSpinCrossRuntimeFixture(t *testing.T) {
	data, err := os.ReadFile("../client/js/testdata/group-spin-affine.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Groups  []GroupIR `json:"groups"`
		Samples []struct {
			Time     float64                `json:"time"`
			Matrices map[string][16]float64 `json:"matrices"`
		} `json:"samples"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	for _, sample := range fixture.Samples {
		matrices, err := GroupWorldMatrices(fixture.Groups, sample.Time)
		if err != nil {
			t.Fatal(err)
		}
		for id, want := range sample.Matrices {
			for i, value := range want {
				if math.Abs(matrices[id][i]-value) > 1e-12 {
					t.Fatalf("stale cross-runtime fixture at %v: %s[%d]", sample.Time, id, i)
				}
			}
		}
	}
}

func TestGroupSpinPickingRejectsInvalidPoseAndCanResample(t *testing.T) {
	graph := NewGraph(Group{ID: "rotor", Spin: Rotate(0, 0, 1), Children: []Node{Mesh{ID: "tip", Geometry: SphereGeometry{Radius: .2}, Position: Vec3(2, 0, 0)}}})
	ray := Ray{Origin: Vec3(2, 0, 3), Direction: Vec3(0, 0, -1)}
	if len(TraceGraph(graph, ray, AtTime(math.NaN())).Hits) != 0 {
		t.Fatal("invalid clock produced graph hits")
	}
	accelerator := NewSceneAccelerator(graph, AtTime(math.NaN()))
	if len(accelerator.Trace(ray).Hits) != 0 {
		t.Fatal("invalid clock produced accelerator hits")
	}
	if len(accelerator.Trace(ray, AtTime(0)).Hits) == 0 {
		t.Fatal("accelerator could not recover by sampling a valid time")
	}
	overflow := NewGraph(Group{ID: "overflow", Spin: Rotate(math.MaxFloat64, 0, 0), Children: []Node{Mesh{ID: "tip", Geometry: SphereGeometry{Radius: 1}}}})
	if len(TraceGraph(overflow, Ray{Origin: Vec3(0, 0, 3), Direction: Vec3(0, 0, -1)}, AtTime(2)).Hits) != 0 {
		t.Fatal("overflowing spin produced a graph hit")
	}
}

func TestGroupSpinKeepsLeafMotionAndLocalTransforms(t *testing.T) {
	leaf := Mesh{ID: "blade", Position: Vec3(2, .5, -1), Rotation: Rotate(.7, .2, .4), Scale: Vec3(1, 2, 3), Spin: Rotate(.1, .2, .3), Geometry: BoxGeometry{}}
	ir := NewGraph(Group{ID: "rotor", Spin: Rotate(0, 0, 4), Children: []Node{leaf}}).SceneIR()
	object := ir.Objects[0]
	if object.X != leaf.Position.X || object.Y != leaf.Position.Y || object.Z != leaf.Position.Z || object.RotationX != leaf.Rotation.X || object.RotationY != leaf.Rotation.Y || object.RotationZ != leaf.Rotation.Z || object.ScaleX != 1 || object.ScaleY != 2 || object.ScaleZ != 3 || object.SpinX != .1 || object.SpinY != .2 || object.SpinZ != .3 {
		t.Fatalf("group rewrote leaf-local motion: %+v", object)
	}
	if len(ir.SpinTracks) != 2 {
		t.Fatalf("expected independent parent and leaf tracks: %v", ir.SpinTracks)
	}
}

func TestGroupSpinPickingComposesNestedReflectedParentScale(t *testing.T) {
	graph := NewGraph(Group{Position: Vec3(1, -2, 1), Rotation: Rotate(.2, .3, .4), Scale: Vec3(2, 3, -4), Children: []Node{
		Group{ID: "outer", Spin: Rotate(.4, .8, 1.2), Children: []Node{
			Group{ID: "inner", Position: Vec3(2, .4, 0), Spin: Rotate(-.2, .3, .8), Children: []Node{
				Mesh{ID: "tip", Position: Vec3(.7, 1, .2), Geometry: SphereGeometry{Radius: .2}},
			}},
		}},
	}})
	ir := graph.SceneIR()
	accelerator := NewSceneAccelerator(graph)
	for _, seconds := range []float64{0, .125, .5, 1, 5, 1} {
		matrices, err := GroupWorldMatrices(ir.Groups, seconds)
		if err != nil {
			t.Fatal(err)
		}
		center := affinePoint(affineMatrix(matrices["inner"]), Vec3(.7, 1, .2))
		ray := Ray{Origin: addVectors(center, Vec3(0, 0, 5)), Direction: Vec3(0, 0, -1)}
		direct := TraceGraph(graph, ray, AtTime(seconds))
		fast := accelerator.Trace(ray, AtTime(seconds))
		if direct.Closest == nil || fast.Closest == nil || direct.Closest.ID != "tip" || fast.Closest.ID != "tip" || math.Abs(direct.Closest.Distance-fast.Closest.Distance) > 1e-10 {
			t.Fatalf("reflected nested pick mismatch at %v: direct=%+v accelerator=%+v", seconds, direct.Closest, fast.Closest)
		}
	}
}
