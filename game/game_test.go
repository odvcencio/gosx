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

func TestWorldQueryIntoReusesBackingArrayAndOrders(t *testing.T) {
	world := NewWorld()
	first := world.Spawn()
	second := world.Spawn()
	third := world.Spawn()
	SetComponent(world, third, Transform{Position: Vec3{X: 3}})
	SetComponent(world, first, Transform{Position: Vec3{X: 1}})
	SetComponent(world, second, Transform{Position: Vec3{X: 2}})

	base := make([]ComponentRef[Transform], 1, 8)
	basePtr := &base[0]
	rows := QueryInto(world, base[:0])
	if len(rows) != 3 {
		t.Fatalf("expected three rows, got %#v", rows)
	}
	if &rows[0] != basePtr {
		t.Fatal("expected QueryInto to reuse destination backing array")
	}
	if rows[0].Entity != first || rows[1].Entity != second || rows[2].Entity != third {
		t.Fatalf("expected spawn-order rows, got %#v", rows)
	}
	world.Despawn(second)
	rows = QueryInto(world, rows)
	if len(rows) != 2 || rows[0].Entity != first || rows[1].Entity != third {
		t.Fatalf("expected despawned entity to be skipped, got %#v", rows)
	}
}

func TestWorldEntitiesIntoReusesBackingArrayAndOrders(t *testing.T) {
	world := NewWorld()
	first := world.Spawn()
	second := world.Spawn()
	third := world.Spawn()
	world.Despawn(second)

	base := make([]EntityID, 1, 4)
	basePtr := &base[0]
	entities := EntitiesInto(world, base[:0])
	if len(entities) != 2 {
		t.Fatalf("expected two entities, got %#v", entities)
	}
	if &entities[0] != basePtr {
		t.Fatal("expected EntitiesInto to reuse destination backing array")
	}
	if entities[0] != first || entities[1] != third {
		t.Fatalf("expected sorted live entities, got %#v", entities)
	}
}

func TestScratchReusesFrameStorage(t *testing.T) {
	scratch := NewScratch[Vec3](4)
	values := scratch.Append(Vec3{X: 1}, Vec3{X: 2})
	if len(values) != 2 || values[1].X != 2 {
		t.Fatalf("unexpected scratch values %#v", values)
	}
	firstPtr := &values[0]
	scratch.Reset()
	next := scratch.Append(Vec3{X: 3})
	if len(next) != 1 || next[0].X != 3 {
		t.Fatalf("unexpected reset scratch values %#v", next)
	}
	if &next[0] != firstPtr {
		t.Fatal("expected scratch storage to be reused")
	}
	cleared := scratch.Slice(2)
	if len(cleared) != 2 || cleared[0] != (Vec3{}) || cleared[1] != (Vec3{}) {
		t.Fatalf("expected zeroed scratch slice, got %#v", cleared)
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

func TestWeb3DProfileDeclaresFullStackSceneDefaults(t *testing.T) {
	profile := Web3DProfile()
	if profile.Name != ProfileWeb3D {
		t.Fatalf("expected web3d profile, got %q", profile.Name)
	}
	for _, capability := range []engine.Capability{
		engine.CapWebGL,
		engine.CapWebGL2,
		engine.CapPointer,
		engine.CapFetch,
		engine.CapStorage,
	} {
		if !hasCapability(profile.Capabilities, capability) {
			t.Fatalf("expected web3d capability %q in %#v", capability, profile.Capabilities)
		}
	}
	if hasCapability(profile.Capabilities, engine.CapGamepad) {
		t.Fatalf("web3d profile should not imply gamepad input, got %#v", profile.Capabilities)
	}
	if !hasBinding(profile.Bindings, "inspect") || !hasBinding(profile.Bindings, "camera.left") {
		t.Fatalf("expected web3d inspection/camera bindings, got %#v", profile.Bindings)
	}
}

func TestFightingProfileDeclaresVersusGameDefaults(t *testing.T) {
	profile := FightingProfile()
	if profile.Name != ProfileFighting {
		t.Fatalf("expected fighting profile, got %q", profile.Name)
	}
	if profile.FixedStep != time.Second/60 {
		t.Fatalf("expected 60hz fixed step, got %s", profile.FixedStep)
	}
	if profile.MaxSubsteps != 3 {
		t.Fatalf("expected bounded substeps for fighting profile, got %d", profile.MaxSubsteps)
	}
	for _, capability := range []engine.Capability{
		engine.CapWebGL,
		engine.CapWebGL2,
		engine.CapKeyboard,
		engine.CapGamepad,
		engine.CapAudio,
	} {
		if !hasCapability(profile.Capabilities, capability) {
			t.Fatalf("expected fighting capability %q in %#v", capability, profile.Capabilities)
		}
	}
	if !hasBinding(profile.Bindings, "attack.light_punch") || !hasBinding(profile.Bindings, "guard") {
		t.Fatalf("expected fighting attack/guard bindings, got %#v", profile.Bindings)
	}
}

func TestAssetsConstructorsBatchRegistrationAndPreloads(t *testing.T) {
	assets := NewAssets()
	assets.MustRegisterAll(
		WithPreload(WithContentType(GLB("fighter", "/models/fighter.glb"), "model/gltf-binary")),
		Texture("albedo", "/textures/fighter.png"),
		WithMetadata(Audio("hit", "/audio/hit.ogg"), "role", "sfx"),
	)

	preloads := assets.Preloads()
	if len(preloads) != 1 || preloads[0].ID != "fighter" || preloads[0].ContentType != "model/gltf-binary" {
		t.Fatalf("unexpected preloads %#v", preloads)
	}
	textures := assets.ByKind(AssetTexture)
	if len(textures) != 1 || textures[0].ID != "albedo" {
		t.Fatalf("unexpected texture assets %#v", textures)
	}
	audio, ok := assets.Resolve("hit")
	if !ok || audio.Metadata["role"] != "sfx" {
		t.Fatalf("unexpected audio asset %#v ok=%v", audio, ok)
	}
}

func TestRuntimeAudioManifestAndEvents(t *testing.T) {
	assets := NewAssets()
	assets.MustRegister(
		WithMetadata(
			WithMetadata(
				WithMetadata(WithPreload(Audio("hit", "/audio/hit.ogg")), "bus", "sfx"),
				"volume", "0.75",
			),
			"loop", "true",
		),
	)
	var emitted []Event
	rt := New(Config{
		Profile: FightingProfile(),
		Assets:  assets,
		Systems: []System{
			Func("audio", PhaseUpdate, func(ctx *Context) error {
				ctx.PlayAudio("hit", AudioPlayback{Volume: 0.5, Pan: -0.25})
				ctx.StopAudio("hit")
				emitted = append(emitted, ctx.Runtime.events...)
				return nil
			}),
		},
	})
	manifest := rt.AudioManifest(AudioBus{ID: "sfx", Volume: 0.8})
	if len(manifest.Clips) != 1 {
		t.Fatalf("expected one audio clip, got %#v", manifest)
	}
	clip := manifest.Clips[0]
	if clip.ID != "hit" || clip.Bus != "sfx" || !clip.Preload || !clip.Loop || clip.Volume != 0.75 {
		t.Fatalf("unexpected clip %#v", clip)
	}
	if len(manifest.Buses) != 1 || manifest.Buses[0].ID != "sfx" {
		t.Fatalf("unexpected buses %#v", manifest.Buses)
	}
	if _, err := rt.Step(time.Second / 60); err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 2 || emitted[0].Type != EventAudioPlay || emitted[1].Type != EventAudioStop {
		t.Fatalf("unexpected audio events %#v", emitted)
	}
}

func TestRuntimeEngineConfigCarriesAudioManifest(t *testing.T) {
	assets := NewAssets()
	assets.MustRegister(WithPreload(Audio("hit", "/audio/hit.ogg")))
	rt := New(Config{
		Profile: FightingProfile(),
		Assets:  assets,
		Scene: func(ctx *Context) scene.Props {
			return scene.Props{
				Graph: scene.NewGraph(scene.Mesh{
					ID:       "arena",
					Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
					Material: scene.FlatMaterial{Color: "#fff"},
				}),
			}
		},
	})
	cfg := rt.EngineConfig()
	if !hasCapability(cfg.Capabilities, engine.CapAudio) {
		t.Fatalf("expected audio capability, got %#v", cfg.Capabilities)
	}
	var props map[string]any
	if err := json.Unmarshal(cfg.Props, &props); err != nil {
		t.Fatal(err)
	}
	audioManifest, ok := props["audio"].(map[string]any)
	if !ok {
		t.Fatalf("expected audio manifest in props, got %#v", props)
	}
	clips, ok := audioManifest["clips"].([]any)
	if !ok || len(clips) != 1 {
		t.Fatalf("expected one audio clip in props, got %#v", audioManifest)
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

func hasBinding(bindings []Binding, action string) bool {
	for _, binding := range bindings {
		if binding.Action == action {
			return true
		}
	}
	return false
}
