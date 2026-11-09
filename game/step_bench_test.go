package game

import (
	"fmt"
	"testing"
	"time"

	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/sim"
)

// benchSceneProps is an 80-node scene: 79 meshes, one with a rigid body, and a
// physics world, so the scene-derived physics path is live.
func benchSceneProps() scene.Props {
	nodes := make([]scene.Node, 0, 80)
	for i := 0; i < 79; i++ {
		nodes = append(nodes, scene.Mesh{
			ID:       fmt.Sprintf("mesh-%02d", i),
			Position: scene.Vec3(float64(i%10), 0, float64(i/10)),
			Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
			Material: scene.StandardMaterial{Color: "#88aacc", Roughness: 0.5},
		})
	}
	nodes = append(nodes, scene.Mesh{
		ID:       "ball",
		Position: scene.Vec3(0, 5, 0),
		Geometry: scene.SphereGeometry{Radius: 0.5},
		Material: scene.FlatMaterial{Color: "#fff"},
		RigidBody: &scene.RigidBody3D{
			Mass:      1,
			Colliders: []scene.Collider3D{{Shape: "sphere", Radius: 0.5}},
		},
	})
	return scene.Props{
		Width:   960,
		Height:  540,
		Physics: scene.PhysicsWorld{Colliders: []scene.Collider3D{{Shape: "plane", Normal: scene.Vector3{Y: 1}}}},
		Graph:   scene.NewGraph(nodes...),
	}
}

type benchPosition struct{ X, Y float64 }

// newBenchRuntime builds the steady-state runtime the step benchmark and the
// allocation test measure: three systems, input bindings, an 80-node scene
// whose declaration does not change, and no physics stepping (the scene-derived
// world is replaced by manual physics, so the measurement is the runtime's own
// work, not the solver's).
func newBenchRuntime(tb testing.TB) *Runtime {
	tb.Helper()
	props := benchSceneProps()
	world := NewWorld()
	id := world.Spawn()
	SetComponent(world, id, benchPosition{})
	rt := New(Config{
		World:         world,
		ManualPhysics: true,
		Systems: []System{
			Func("read-input", PhaseUpdate, func(ctx *Context) error {
				_ = ctx.Input.Down("move.left")
				return nil
			}),
			Func("integrate", PhaseFixedUpdate, func(ctx *Context) error {
				if p, ok := GetComponent[benchPosition](ctx.World, id); ok {
					p.X += 0.01
					SetComponent(ctx.World, id, p)
				}
				return nil
			}),
			Every(4, Func("slow", PhaseFixedUpdate, func(*Context) error { return nil })),
		},
		Scene: func(*Context) scene.Props { return props },
	})
	// Warm up: the first frames build the scene and size every buffer.
	for i := 0; i < 4; i++ {
		if _, err := rt.Step(time.Second / 60); err != nil {
			tb.Fatal(err)
		}
	}
	return rt
}

// BenchmarkRuntimeStep80Nodes measures one 60 Hz Runtime.Step with an 80-node
// scene that does not change between steps.
func BenchmarkRuntimeStep80Nodes(b *testing.B) {
	rt := newBenchRuntime(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := rt.Step(time.Second / 60); err != nil {
			b.Fatal(err)
		}
	}
}

// A steady-state Step, with and without input events, allocates nothing.
func TestRuntimeStepSteadyStateAllocatesNothing(t *testing.T) {
	rt := newBenchRuntime(t)
	if allocs := testing.AllocsPerRun(200, func() { _, _ = rt.Step(time.Second / 60) }); allocs != 0 {
		t.Fatalf("steady-state Step allocated %v times per call, want 0", allocs)
	}
	down := InputEvent{Kind: EventKeyDown, Code: "ArrowLeft"}
	up := InputEvent{Kind: EventKeyUp, Code: "ArrowLeft"}
	if allocs := testing.AllocsPerRun(200, func() {
		rt.Input().Apply(down)
		_, _ = rt.Step(time.Second / 60)
		rt.Input().Apply(up)
		_, _ = rt.Step(time.Second / 60)
	}); allocs != 0 {
		t.Fatalf("Step with input events allocated %v times per call, want 0", allocs)
	}
	// A frame that emits events reuses the event buffer too.
	emitting := New(Config{Systems: []System{Func("emit", PhaseLateUpdate, func(ctx *Context) error {
		ctx.Emit(Event{Type: "tick"})
		return nil
	})}})
	for i := 0; i < 4; i++ {
		_, _ = emitting.Step(time.Second / 60)
	}
	if allocs := testing.AllocsPerRun(200, func() { _, _ = emitting.Step(time.Second / 60) }); allocs != 0 {
		t.Fatalf("Step that emits an event allocated %v times per call, want 0", allocs)
	}
}

// The SceneBuilder runs on the first Step and after InvalidateScene, and the
// scene-derived physics world is rebuilt only when its declaration changes.
func TestRuntimeSceneRebuildsOnlyWhenInvalidated(t *testing.T) {
	builds := 0
	radius := 0.5
	rt := New(Config{Scene: func(*Context) scene.Props {
		builds++
		props := benchSceneProps()
		props.Graph.Nodes[len(props.Graph.Nodes)-1] = scene.Mesh{
			ID:       "ball",
			Geometry: scene.SphereGeometry{Radius: radius},
			RigidBody: &scene.RigidBody3D{
				Mass:      1,
				Colliders: []scene.Collider3D{{Shape: "sphere", Radius: radius}},
			},
		}
		return props
	}})
	for i := 0; i < 10; i++ {
		rt.Step(time.Second / 60)
	}
	if builds != 1 {
		t.Fatalf("builder ran %d times in 10 steps, want 1", builds)
	}
	world := rt.Physics()
	if world == nil || len(world.Bodies()) != 1 {
		t.Fatalf("scene-derived physics = %#v, want one body", world)
	}
	rt.InvalidateScene()
	rt.Step(time.Second / 60)
	rt.Step(time.Second / 60)
	if builds != 2 {
		t.Fatalf("builder ran %d times after one invalidation, want 2", builds)
	}
	if rt.Physics() != world {
		t.Fatal("an unchanged physics declaration rebuilt the physics world")
	}
	radius = 1
	rt.Step(time.Second / 60) // not invalidated: the change is not seen
	if rt.Physics() != world || builds != 2 {
		t.Fatal("a Step without InvalidateScene ran the builder")
	}
	invalidate := New(Config{
		Systems: []System{Func("spawn", PhaseUpdate, func(ctx *Context) error {
			if ctx.Frame.Index == 3 {
				ctx.InvalidateScene()
			}
			return nil
		})},
		Scene: func(*Context) scene.Props { builds++; return scene.Props{} },
	})
	builds = 0
	for i := 0; i < 5; i++ {
		invalidate.Step(time.Second / 60)
	}
	if builds != 2 {
		t.Fatalf("Context.InvalidateScene: builder ran %d times, want 2", builds)
	}
	rt.InvalidateScene()
	rt.Step(time.Second / 60)
	if rt.Physics() == world || len(rt.Physics().Bodies()) != 1 {
		t.Fatal("a changed physics declaration did not rebuild the physics world")
	}
	// A restore can change what the builder returns, so it invalidates.
	before := builds
	snap := invalidate.Snapshot()
	if err := invalidate.RestoreSnapshot(snap); err != nil {
		t.Fatal(err)
	}
	invalidate.Step(time.Second / 60)
	if builds != before+1 {
		t.Fatalf("restore: builder ran %d more times, want 1", builds-before)
	}
}

// Network inputs reach systems as a copy the runtime owns, so the caller can
// reuse its buffers, and a Step without new inputs sees none.
func TestRuntimeNetworkInputsAreCopied(t *testing.T) {
	var seen []string
	rt := New(Config{Systems: []System{Func("net", PhaseUpdate, func(ctx *Context) error {
		for id, in := range ctx.NetworkInputs {
			seen = append(seen, id+"="+string(in.Data))
		}
		return nil
	})}})
	buf := []byte(`"a"`)
	rt.ApplyNetworkInputs(map[string]sim.Input{"p1": {Data: buf}})
	copy(buf, `"b"`)
	rt.Step(time.Second / 60)
	rt.Step(time.Second / 60)
	if len(seen) != 1 || seen[0] != `p1="a"` {
		t.Fatalf("systems saw %v, want one p1 input with the bytes at apply time", seen)
	}
}

// Input normalizes mixed-case codes through a bounded cache.
func TestInputNormalizeCacheIsBounded(t *testing.T) {
	in := NewInput(Key("left", "ArrowLeft"))
	in.Apply(InputEvent{Kind: EventKeyDown, Code: " ArrowLeft "})
	if !in.Down("left") || in.Events()[0].Code != "arrowleft" {
		t.Fatalf("mixed-case code did not normalize: %+v", in.Events())
	}
	for i := 0; i < 2*maxNormalized; i++ {
		in.Apply(InputEvent{Kind: EventKeyDown, Code: fmt.Sprintf("Key%d", i)})
	}
	if len(in.normalized) > maxNormalized {
		t.Fatalf("cache holds %d codes, want at most %d", len(in.normalized), maxNormalized)
	}
	if got := in.Events()[len(in.Events())-1].Code; got != fmt.Sprintf("key%d", 2*maxNormalized-1) {
		t.Fatalf("code past the cache bound = %q", got)
	}
}
