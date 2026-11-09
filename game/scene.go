package game

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/physics"
	"m31labs.dev/gosx/scene"
)

// SceneBuilder renders the current runtime state into Scene3D props: the
// scene declaration. The runtime runs it at the end of the first Step and of
// every Step after InvalidateScene, not on every Step. Per-frame motion (poses,
// instance transforms) takes the binary instance stream instead; see
// docs/engine-frame-loop.md.
type SceneBuilder func(*Context) scene.Props

type latestScene struct {
	props       scene.Props
	physicsHash string
	ok          bool
}

// Scene returns the latest Scene3D props produced by the runtime.
func (r *Runtime) Scene() (scene.Props, bool) {
	if r == nil || !r.latestScene.ok {
		return scene.Props{}, false
	}
	return r.latestScene.props, true
}

// SceneIR returns the latest canonical Scene3D IR.
func (r *Runtime) SceneIR() (scene.IR, bool) {
	props, ok := r.Scene()
	if !ok {
		return scene.IR{}, false
	}
	return props.CanonicalIR(), true
}

// InvalidateScene marks the scene declaration as changed: the next Step ends by
// running the SceneBuilder, and, when the runtime owns a scene-derived physics
// world, by checking whether the physics declaration changed. Call it when
// what the builder returns changes: an entity appears or goes, a material or
// mesh changes, a body is added. Steps without it reuse the last scene and do
// no scene work.
func (r *Runtime) InvalidateScene() {
	if r != nil {
		r.sceneDirty = true
	}
}

// BuildScene forces the configured SceneBuilder to run against the current
// runtime state.
func (r *Runtime) BuildScene() (scene.Props, bool) {
	if r == nil || r.sceneBuilder == nil {
		return scene.Props{}, false
	}
	ctx := &Context{
		Runtime: r,
		World:   r.world,
		Input:   r.input,
		Assets:  r.assets,
		Physics: r.physics,
		Frame:   r.frame,
		Phase:   PhaseRender,
		Delta:   r.frame.Delta,
	}
	r.rebuildScene(ctx)
	return r.Scene()
}

func (r *Runtime) rebuildScene(ctx *Context) {
	if r == nil {
		return
	}
	r.sceneDirty = false
	if r.sceneBuilder == nil {
		return
	}
	props := r.sceneBuilder(ctx)
	if !r.physicsFromScene {
		// The physics world comes from Config or the game manages it, so the
		// physics declaration does not matter: skip lowering and hashing.
		r.latestScene = latestScene{props: props, ok: true}
		return
	}
	ir := props.CanonicalIR()
	physicsHash := physicsDeclarationHash(ir)
	previousPhysicsHash := r.latestScene.physicsHash
	r.latestScene = latestScene{props: props, physicsHash: physicsHash, ok: true}
	if physicsHash != previousPhysicsHash {
		r.physics = physicsFromIR(ir)
	}
}

// PhysicsFromScene builds a physics.World from the Scene3D physics declaration.
func PhysicsFromScene(props scene.Props) *physics.World {
	return physicsFromIR(props.CanonicalIR())
}

func physicsFromIR(ir scene.IR) *physics.World {
	if ir.Physics == nil {
		return nil
	}
	return physics.BuildWorld(ir.PhysicsSpec())
}

// physicsDeclarationHash fingerprints the parts of the physics declaration that
// a rebuilt world would change: dynamic body poses and velocities are left out,
// because the running world owns them.
func physicsDeclarationHash(ir scene.IR) string {
	if ir.Physics == nil {
		return ""
	}
	declaration := *ir.Physics
	if len(ir.Physics.Bodies) > 0 {
		declaration.Bodies = append([]scene.IRRigidBody(nil), ir.Physics.Bodies...)
		for i := range declaration.Bodies {
			body := &declaration.Bodies[i]
			body.Velocity = scene.IRVector3{}
			body.AngularVelocity = scene.IRVector3{}
			if !body.Static && body.Mass > 0 {
				body.Position = scene.IRVector3{}
				body.Rotation = scene.IRVector3{}
			}
		}
	}
	data, err := json.Marshal(declaration)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// EngineConfig returns the engine surface config for mounting this runtime.
func (r *Runtime) EngineConfig() engine.Config {
	if r == nil {
		return engine.Config{}
	}
	props, ok := r.Scene()
	if !ok {
		props, ok = r.BuildScene()
	}
	var cfg engine.Config
	if ok {
		cfg = props.EngineConfig()
		if cfg.Name == scene.DefaultEngineName && strings.TrimSpace(r.name) != "" {
			cfg.Name = r.name
		}
	} else {
		cfg = r.genericEngineConfig()
	}
	cfg = r.withRuntimeProps(cfg)
	cfg.Capabilities = mergeCapabilities(cfg.Capabilities, r.profile.Capabilities)
	cfg.RequiredCapabilities = mergeCapabilities(cfg.RequiredCapabilities, r.profile.RequiredCapabilities)
	if cfg.MountAttrs == nil {
		cfg.MountAttrs = map[string]any{}
	}
	cfg.MountAttrs["data-gosx-game"] = true
	cfg.MountAttrs["data-gosx-game-profile"] = r.profile.Name
	cfg.MountAttrs["data-gosx-game-fixed-step"] = r.FixedStep().String()
	return cfg
}

func (r *Runtime) genericEngineConfig() engine.Config {
	name := strings.TrimSpace(r.name)
	if name == "" {
		name = "GoSXGame"
	}
	props := map[string]any{
		"profile":          r.profile.Name,
		"fixedStepSeconds": r.FixedStep().Seconds(),
		"assets":           r.assets.Manifest(),
	}
	raw, _ := json.Marshal(props)
	return engine.Config{
		Name:                 name,
		Kind:                 engine.KindSurface,
		Props:                raw,
		Capabilities:         r.profile.Capabilities,
		RequiredCapabilities: r.profile.RequiredCapabilities,
		MountAttrs: map[string]any{
			"data-gosx-game": true,
		},
	}
}

func (r *Runtime) withRuntimeProps(cfg engine.Config) engine.Config {
	if r == nil {
		return cfg
	}
	props := map[string]any{}
	if len(cfg.Props) > 0 {
		_ = json.Unmarshal(cfg.Props, &props)
	}
	if props == nil {
		props = map[string]any{}
	}
	props["gameProfile"] = r.profile.Name
	props["fixedStepSeconds"] = r.FixedStep().Seconds()
	if _, set := props["framePacing"]; !set && r.profile.FramePacing != "" {
		props["framePacing"] = r.profile.FramePacing
	}
	if assets := r.assets.Manifest(); len(assets) > 0 {
		props["assets"] = assets
	}
	audio := r.AudioManifest()
	if len(audio.Clips) > 0 || len(audio.Buses) > 0 {
		props["audio"] = audio
		cfg.Capabilities = mergeCapabilities(cfg.Capabilities, []engine.Capability{engine.CapAudio})
	}
	raw, err := json.Marshal(props)
	if err == nil {
		cfg.Props = raw
	}
	return cfg
}
