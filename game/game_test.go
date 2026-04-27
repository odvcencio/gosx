package game

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/physics"
	"github.com/odvcencio/gosx/scene"
	"github.com/odvcencio/gosx/sim"
)

func TestClockFixedStepAndClamp(t *testing.T) {
	clock := NewClock(LoopConfig{
		FixedStep:   10 * time.Millisecond,
		MaxDelta:    25 * time.Millisecond,
		MaxSubsteps: 2,
	})
	frame := clock.Advance(35 * time.Millisecond)
	if frame.Delta != 25*time.Millisecond {
		t.Fatalf("delta should clamp to max delta, got %s", frame.Delta)
	}
	if frame.FixedSteps != 2 {
		t.Fatalf("expected 2 fixed steps, got %d", frame.FixedSteps)
	}
	if frame.Alpha != 0.5 {
		t.Fatalf("expected alpha 0.5, got %f", frame.Alpha)
	}
}

func TestWorldComponentsQueryAndResources(t *testing.T) {
	world := NewWorld()
	entity := world.Spawn(WithName("probe"))
	if entity == InvalidEntity {
		t.Fatal("expected entity")
	}
	if got, ok := world.Entity("probe"); !ok || got != entity {
		t.Fatalf("name lookup = %v %v", got, ok)
	}
	if !SetComponent(world, entity, Transform{Position: Vec3{X: 1}}) {
		t.Fatal("expected component set")
	}
	if !UpdateComponent[Transform](world, entity, func(transform *Transform) {
		transform.Position.Y = 2
	}) {
		t.Fatal("expected component update")
	}
	transform, ok := GetComponent[Transform](world, entity)
	if !ok || transform.Position.X != 1 || transform.Position.Y != 2 {
		t.Fatalf("unexpected transform %#v ok=%v", transform, ok)
	}
	rows := Query[Transform](world)
	if len(rows) != 1 || rows[0].Entity != entity {
		t.Fatalf("unexpected query rows %#v", rows)
	}
	type constants struct{ Gravity float64 }
	SetResource(world, constants{Gravity: 9.81})
	resource, ok := GetResource[constants](world)
	if !ok || resource.Gravity != 9.81 {
		t.Fatalf("unexpected resource %#v ok=%v", resource, ok)
	}
	world.Despawn(entity)
	if _, ok := GetComponent[Transform](world, entity); ok {
		t.Fatal("despawn should remove components")
	}
}

func TestWorldDuplicateNamesPreferNewestEntity(t *testing.T) {
	world := NewWorld()
	first := world.Spawn(WithName("probe"))
	second := world.Spawn(WithName("probe"))
	if got, ok := world.Entity("probe"); !ok || got != second {
		t.Fatalf("expected newest named entity, got %v ok=%v", got, ok)
	}
	world.Despawn(first)
	if got, ok := world.Entity("probe"); !ok || got != second {
		t.Fatalf("despawning old duplicate should keep newest, got %v ok=%v", got, ok)
	}
}

func TestInputActionEdges(t *testing.T) {
	input := NewInput(Key("jump", "Space"))
	input.Apply(InputEvent{Kind: EventKeyDown, Code: "Space"})
	if !input.Down("jump") || !input.Pressed("jump") {
		t.Fatalf("expected jump down+pressed, got %#v", input.Action("jump"))
	}
	input.EndFrame()
	if !input.Down("jump") || input.Pressed("jump") {
		t.Fatalf("expected held without pressed, got %#v", input.Action("jump"))
	}
	input.Apply(InputEvent{Kind: EventKeyUp, Code: "Space"})
	if input.Down("jump") || !input.Released("jump") {
		t.Fatalf("expected jump released, got %#v", input.Action("jump"))
	}
}

func TestRuntimeRunsSystemsPhysicsAndScene(t *testing.T) {
	phys := physics.NewWorld(physics.WorldConfig{FixedTimestep: 1.0 / 60.0, Gravity: physics.Vec3{}})
	body := phys.AddBody(physics.BodyConfig{ID: "ball", Mass: 1, Velocity: physics.Vec3{X: 1}})
	rt := New(Config{
		Physics: phys,
		Systems: []System{
			Func("spawn", PhaseUpdate, func(ctx *Context) error {
				if len(ctx.World.Entities()) == 0 {
					id := ctx.World.Spawn(WithName("sample"))
					SetComponent(ctx.World, id, Transform{Position: Vec3{X: 2}})
				}
				return nil
			}),
			Func("force", PhaseFixedUpdate, func(ctx *Context) error {
				body.ApplyForce(physics.Vec3{X: 1})
				return nil
			}),
			Func("event", PhaseLateUpdate, func(ctx *Context) error {
				ctx.Emit(Event{Type: "late"})
				return nil
			}),
		},
		Scene: func(ctx *Context) scene.Props {
			return scene.Props{
				Width:  320,
				Height: 200,
				Graph: scene.NewGraph(scene.Mesh{
					ID:       "sample",
					Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
					Material: scene.FlatMaterial{Color: "#ffffff"},
				}),
			}
		},
	})
	frame, err := rt.Step(time.Second / 60)
	if err != nil {
		t.Fatal(err)
	}
	if frame.FixedSteps != 1 {
		t.Fatalf("expected one fixed step, got %d", frame.FixedSteps)
	}
	if body.Position.X == 0 {
		t.Fatal("expected physics body to advance")
	}
	if _, ok := rt.World().Entity("sample"); !ok {
		t.Fatal("expected system-created entity")
	}
	if len(rt.Events()) != 1 || rt.Events()[0].Type != "late" {
		t.Fatalf("unexpected events %#v", rt.Events())
	}
	if _, ok := rt.Scene(); !ok {
		t.Fatal("expected latest scene")
	}
}

func TestRuntimeEngineConfigAddsGameCapabilities(t *testing.T) {
	rt := New(Config{
		Name:    "LabRuntime",
		Profile: ScientificProfile(),
		Scene: func(ctx *Context) scene.Props {
			requireWebGL := true
			return scene.Props{
				RequireWebGL: &requireWebGL,
				Graph: scene.NewGraph(scene.Mesh{
					ID:       "probe",
					Geometry: scene.SphereGeometry{Radius: 1},
					Material: scene.StandardMaterial{Color: "#77c6ff"},
				}),
			}
		},
	})
	cfg := rt.EngineConfig()
	if cfg.Name != "LabRuntime" {
		t.Fatalf("expected runtime name, got %q", cfg.Name)
	}
	if cfg.Kind != engine.KindSurface {
		t.Fatalf("expected surface engine, got %q", cfg.Kind)
	}
	if cfg.MountAttrs["data-gosx-game"] != true {
		t.Fatalf("expected game mount attr, got %#v", cfg.MountAttrs)
	}
	if !hasCapability(cfg.Capabilities, engine.CapCompute) {
		t.Fatalf("expected scientific compute capability, got %#v", cfg.Capabilities)
	}
	if !hasCapability(cfg.RequiredCapabilities, engine.CapWebGL) {
		t.Fatalf("expected Scene3D webgl requirement, got %#v", cfg.RequiredCapabilities)
	}
}

func TestPhysicsFromScene(t *testing.T) {
	props := scene.Props{
		Physics: scene.PhysicsWorld{FixedTimestep: 1.0 / 30.0},
		Graph: scene.NewGraph(scene.Mesh{
			ID:       "ball",
			Geometry: scene.SphereGeometry{Radius: 1},
			Material: scene.FlatMaterial{Color: "#fff"},
			RigidBody: &scene.RigidBody3D{
				Mass: 1,
				Colliders: []scene.Collider3D{{
					Shape:  "sphere",
					Radius: 1,
				}},
			},
		}),
	}
	world := PhysicsFromScene(props)
	if world == nil {
		t.Fatal("expected physics world")
	}
	if got := world.FixedTimestep(); got != 1.0/30.0 {
		t.Fatalf("expected fixed timestep 1/30, got %f", got)
	}
	if len(world.Bodies()) != 1 {
		t.Fatalf("expected one body, got %d", len(world.Bodies()))
	}
}

func TestRuntimeImplementsSimulationAndNetworkInput(t *testing.T) {
	var pressed bool
	rt := New(Config{
		Systems: []System{
			Func("input", PhaseUpdate, func(ctx *Context) error {
				pressed = ctx.Input.Pressed("fire")
				if len(ctx.NetworkInputs) != 1 {
					t.Fatalf("expected one network input, got %#v", ctx.NetworkInputs)
				}
				return nil
			}),
		},
	})
	payload, _ := json.Marshal(InputEvent{Kind: EventActionDown, Action: "fire"})
	rt.Tick(map[string]sim.Input{"p1": {Data: payload}})
	if !pressed {
		t.Fatal("expected fire action to be pressed during tick")
	}
	state := rt.State()
	if len(state) == 0 {
		t.Fatal("expected default state payload")
	}
	var decoded RuntimeState
	if err := json.Unmarshal(state, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Frame != 1 {
		t.Fatalf("expected frame 1, got %d", decoded.Frame)
	}
}

func TestRuntimeRestoreUpdatesPublicFrameState(t *testing.T) {
	rt := New(Config{})
	rt.Restore([]byte(`{"frame":7,"timeSeconds":0.25}`))
	frame := rt.Frame()
	if frame.Index != 7 {
		t.Fatalf("expected restored frame index 7, got %d", frame.Index)
	}
	if frame.Time != 250*time.Millisecond {
		t.Fatalf("expected restored time 250ms, got %s", frame.Time)
	}
	state := rt.State()
	var decoded RuntimeState
	if err := json.Unmarshal(state, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Frame != 7 || decoded.TimeSeconds != 0.25 {
		t.Fatalf("unexpected restored state %#v", decoded)
	}
}

func hasCapability(values []engine.Capability, want engine.Capability) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
