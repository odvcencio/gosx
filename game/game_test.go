package game

import (
	"encoding/json"
	"errors"
	"strconv"
	"testing"
	"time"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/physics"
	"m31labs.dev/gosx/scene"
	"m31labs.dev/gosx/sim"
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

func TestInputPointerButtonBinding(t *testing.T) {
	input := NewInput(PointerButton("attack.primary", "Mouse0"))
	input.Apply(InputEvent{Kind: EventPointerDown, Code: "Mouse0", X: 10, Y: 12})
	if !input.Down("attack.primary") || !input.Pressed("attack.primary") {
		t.Fatalf("expected primary attack down+pressed, got %#v", input.Action("attack.primary"))
	}
	input.EndFrame()
	input.Apply(InputEvent{Kind: EventPointerUp, Code: "Mouse0"})
	if input.Down("attack.primary") || !input.Released("attack.primary") {
		t.Fatalf("expected primary attack released, got %#v", input.Action("attack.primary"))
	}
}

func TestFirstPersonProfileCapabilitiesAndBindings(t *testing.T) {
	profile := FirstPersonProfile()
	if profile.Name != ProfileFirstPerson {
		t.Fatalf("unexpected profile name %q", profile.Name)
	}
	if !hasCapability(profile.Capabilities, engine.CapPointerLock) {
		t.Fatalf("expected pointer lock capability, got %#v", profile.Capabilities)
	}
	if hasCapability(profile.RequiredCapabilities, engine.CapPointerLock) {
		t.Fatalf("pointer lock should be optional for drag-look fallback, got %#v", profile.RequiredCapabilities)
	}
	if len(profile.Bindings) == 0 {
		t.Fatal("expected first-person bindings")
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

func TestRuntimePhaseOrderAndFirstError(t *testing.T) {
	updateErr := errors.New("update failed")
	var phases []string
	rt := New(Config{
		FixedStep:   10 * time.Millisecond,
		MaxSubsteps: 4,
		Systems: []System{
			Func("update", PhaseUpdate, func(ctx *Context) error {
				phases = append(phases, string(ctx.Phase))
				return updateErr
			}),
			Func("fixed", PhaseFixedUpdate, func(ctx *Context) error {
				phases = append(phases, "fixed-"+strconv.Itoa(ctx.FixedStep))
				return nil
			}),
			Func("late", PhaseLateUpdate, func(ctx *Context) error {
				phases = append(phases, string(ctx.Phase))
				return errors.New("late failed")
			}),
			Func("render", PhaseRender, func(ctx *Context) error {
				phases = append(phases, string(ctx.Phase))
				return nil
			}),
		},
	})

	frame, err := rt.Step(25 * time.Millisecond)
	if !errors.Is(err, updateErr) {
		t.Fatalf("expected first update error, got %v", err)
	}
	if frame.FixedSteps != 2 || frame.Alpha != 0.5 {
		t.Fatalf("expected two fixed steps and alpha 0.5, got %#v", frame)
	}
	want := []string{"update", "fixed-0", "fixed-1", "late-update", "render"}
	if len(phases) != len(want) {
		t.Fatalf("phase count = %d, want %d: %#v", len(phases), len(want), phases)
	}
	for i := range want {
		if phases[i] != want[i] {
			t.Fatalf("phase[%d] = %q, want %q; all=%#v", i, phases[i], want[i], phases)
		}
	}
}

func TestRuntimeManualPhysicsSkipsAutomaticStep(t *testing.T) {
	phys := physics.NewWorld(physics.WorldConfig{FixedTimestep: 1.0 / 60.0, Gravity: physics.Vec3{}})
	body := phys.AddBody(physics.BodyConfig{Mass: 1, Velocity: physics.Vec3{X: 1}})
	rt := New(Config{
		Physics:       phys,
		ManualPhysics: true,
	})

	frame, err := rt.Step(time.Second / 60)
	if err != nil {
		t.Fatal(err)
	}
	if frame.FixedSteps != 1 {
		t.Fatalf("expected one fixed step, got %d", frame.FixedSteps)
	}
	if body.Position.X != 0 {
		t.Fatalf("manual physics should not auto-step body, got position %#v", body.Position)
	}
}

func TestRuntimeNetworkInputsAreFrameLocalCopies(t *testing.T) {
	payload, err := json.Marshal(InputEvent{Kind: EventActionDown, Action: "fire"})
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string]sim.Input{"p1": {Data: payload}}
	var seen []byte
	var pressed bool
	rt := New(Config{
		Systems: []System{
			Func("input", PhaseUpdate, func(ctx *Context) error {
				seen = append([]byte(nil), ctx.NetworkInputs["p1"].Data...)
				pressed = ctx.Input.Pressed("fire")
				ctx.NetworkInputs["p1"] = sim.Input{Data: []byte("mutated")}
				return nil
			}),
		},
	})

	rt.ApplyNetworkInputs(inputs)
	payload[0] = 'X'
	if _, err := rt.Step(rt.FixedStep()); err != nil {
		t.Fatal(err)
	}
	if string(seen) == string(payload) || !json.Valid(seen) {
		t.Fatalf("expected frame-local network input copy, got %q after caller mutation %q", seen, payload)
	}
	if !pressed {
		t.Fatal("expected action event from network input to be applied before systems run")
	}
	if got := rt.Frame().Index; got != 1 {
		t.Fatalf("expected frame 1, got %d", got)
	}
}

func TestRuntimeDefaultSnapshotRestoresPhysicsState(t *testing.T) {
	phys := physics.NewWorld(physics.WorldConfig{FixedTimestep: 1.0 / 60.0, Gravity: physics.Vec3{}})
	body := phys.AddBody(physics.BodyConfig{ID: "ball", Mass: 1, Velocity: physics.Vec3{X: 3}})
	rt := New(Config{Physics: phys})

	if _, err := rt.Step(time.Second / 60); err != nil {
		t.Fatal(err)
	}
	snapshot := rt.Snapshot()
	if len(snapshot) == 0 {
		t.Fatal("expected default snapshot")
	}
	firstPosition := body.Position.X
	if firstPosition <= 0 {
		t.Fatalf("expected body to advance before snapshot, got %#v", body.Position)
	}
	if _, err := rt.Step(time.Second / 60); err != nil {
		t.Fatal(err)
	}
	if body.Position.X <= firstPosition {
		t.Fatalf("expected body to advance after second step, got %#v", body.Position)
	}

	rt.Restore(snapshot)
	if got := rt.Frame().Index; got != 1 {
		t.Fatalf("expected restored frame 1, got %d", got)
	}
	if body.Position.X != firstPosition {
		t.Fatalf("expected physics body restore to x=%v, got %#v", firstPosition, body.Position)
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
	if err := assets.RegisterAll(
		WithVariant(
			WithPreload(WithContentType(GLB("fighter", "/models/fighter.glb"), "model/gltf-binary")),
			VariantCapabilities(
				VariantBytes(
					VariantCompression(
						VariantQuality(
							VariantContentType(Variant("/models/fighter.meshopt.glb"), "model/gltf-binary"),
							"high",
						),
						"meshopt",
					),
					640_000,
				),
				"webgl2",
				"meshopt",
			),
		),
		Texture("albedo", "/textures/fighter.png"),
		WithMetadata(Audio("hit", "/audio/hit.ogg"), "role", "sfx"),
	); err != nil {
		t.Fatal(err)
	}

	preloads := assets.Preloads()
	if len(preloads) != 1 || preloads[0].ID != "fighter" || preloads[0].ContentType != "model/gltf-binary" {
		t.Fatalf("unexpected preloads %#v", preloads)
	}
	if len(preloads[0].Variants) != 1 || preloads[0].Variants[0].Compression != "meshopt" || preloads[0].Variants[0].RequiredCapabilities[0] != "webgl2" {
		t.Fatalf("unexpected preload variants %#v", preloads[0].Variants)
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

func TestAssetVariantSelection(t *testing.T) {
	base := WithVariant(
		WithVariant(Texture("terrain", "/textures/terrain.png"),
			VariantCapabilities(
				VariantCompression(
					VariantContentType(Variant("/textures/terrain.ktx2"), "image/ktx2"),
					"ktx2",
				),
				"webgpu",
				"ktx2",
			),
		),
		VariantCapabilities(
			VariantCompression(
				VariantContentType(Variant("/textures/terrain-basis.ktx2"), "image/ktx2"),
				"basis",
			),
			"webgl2",
			"basis",
		),
	)
	assets := NewAssets()
	if _, err := assets.Register(base); err != nil {
		t.Fatal(err)
	}

	fallback, ok := assets.ResolveFor("terrain", "webgl")
	if !ok || fallback.URI != "/textures/terrain.png" || len(fallback.Variants) != 0 {
		t.Fatalf("expected base fallback without variants, got %#v ok=%v", fallback, ok)
	}
	webgl2, ok := assets.ResolveFor("terrain", "webgl2", "basis")
	if !ok || webgl2.URI != "/textures/terrain-basis.ktx2" || webgl2.ContentType != "image/ktx2" || webgl2.Metadata["compression"] != "basis" {
		t.Fatalf("expected basis variant, got %#v ok=%v", webgl2, ok)
	}
	webgpu := assets.ManifestFor("webgpu", "ktx2")
	if len(webgpu) != 1 || webgpu[0].URI != "/textures/terrain.ktx2" || webgpu[0].Metadata["compression"] != "ktx2" {
		t.Fatalf("expected ktx2 manifest variant, got %#v", webgpu)
	}
}

func TestAssetMustHelpersReturnErrorsInsteadOfPanics(t *testing.T) {
	assets := NewAssets()
	if got, err := assets.MustRegister(AssetRef{}); err == nil || got.ID != "" || got.Kind != "" || got.URI != "" {
		t.Fatalf("MustRegister invalid asset = %#v, %v; want zero error", got, err)
	}
	if err := assets.MustRegisterAll(Texture("", "/textures/fighter.png")); err == nil {
		t.Fatal("MustRegisterAll accepted asset without an id")
	}
}

func TestRuntimeAudioManifestAndEvents(t *testing.T) {
	assets := NewAssets()
	if _, err := assets.Register(
		WithMetadata(
			WithMetadata(
				WithMetadata(WithPreload(Audio("hit", "/audio/hit.ogg")), "bus", "sfx"),
				"volume", "0.75",
			),
			"loop", "true",
		),
	); err != nil {
		t.Fatal(err)
	}
	var emitted []Event
	rt := New(Config{
		Profile: FightingProfile(),
		Assets:  assets,
		Systems: []System{
			Func("audio", PhaseUpdate, func(ctx *Context) error {
				ctx.PlayAudio("hit", AudioAt(Vec3{X: 2, Y: 1.5, Z: -4}, AudioPlayback{
					Volume:        0.5,
					Pan:           -0.25,
					RefDistance:   2,
					MaxDistance:   64,
					RolloffFactor: 0.75,
				}))
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
	playback, ok := emitted[0].Data.(AudioPlayback)
	if !ok || playback.Position == nil || playback.Position.X != 2 || playback.RefDistance != 2 || playback.MaxDistance != 64 || playback.RolloffFactor != 0.75 {
		t.Fatalf("expected spatial audio playback payload, got %#v", emitted[0].Data)
	}
}

func TestRuntimeEngineConfigCarriesAudioManifest(t *testing.T) {
	assets := NewAssets()
	if _, err := assets.Register(WithPreload(Audio("hit", "/audio/hit.ogg"))); err != nil {
		t.Fatal(err)
	}
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

func TestRuntimeRefreshesScenePhysicsWhenDeclarationChanges(t *testing.T) {
	stage := 0
	rt := New(Config{
		Scene: func(ctx *Context) scene.Props {
			id := "crate"
			x := 0.0
			if stage == 1 {
				x = 3
			} else if stage > 1 {
				id = "crate-b"
			}
			return scene.Props{
				Physics: scene.PhysicsWorld{FixedTimestep: 1.0 / 60.0},
				Graph: scene.NewGraph(scene.Mesh{
					ID:       id,
					Position: scene.Vec3(x, 0, 0),
					Geometry: scene.BoxGeometry{Width: 1, Height: 1, Depth: 1},
					Material: scene.FlatMaterial{Color: "#fff"},
					RigidBody: &scene.RigidBody3D{
						Mass: 1,
						Colliders: []scene.Collider3D{{
							Shape:  "box",
							Width:  1,
							Height: 1,
							Depth:  1,
						}},
					},
				}),
			}
		},
	})

	if _, ok := rt.BuildScene(); !ok {
		t.Fatal("expected initial scene")
	}
	first := rt.Physics()
	if first == nil || len(first.Bodies()) != 1 || first.Bodies()[0].ID != "crate" {
		t.Fatalf("expected initial scene physics body crate, got %#v", first)
	}

	stage = 1
	if _, ok := rt.BuildScene(); !ok {
		t.Fatal("expected transform-only refreshed scene")
	}
	if rt.Physics() != first {
		t.Fatal("dynamic body transform changes should not rebuild the scene-derived physics world")
	}

	stage = 2
	if _, ok := rt.BuildScene(); !ok {
		t.Fatal("expected refreshed scene")
	}
	second := rt.Physics()
	if second == nil || len(second.Bodies()) != 1 || second.Bodies()[0].ID != "crate-b" {
		t.Fatalf("expected refreshed scene physics body crate-b, got %#v", second)
	}
	if second == first {
		t.Fatal("expected scene physics world to be rebuilt after declaration change")
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
