package game

import (
	"encoding/json"
	"strings"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/physics"
	"github.com/odvcencio/gosx/scene"
)

// SceneBuilder renders the current runtime state into Scene3D props.
type SceneBuilder func(*Context) scene.Props

type latestScene struct {
	props scene.Props
	ok    bool
}

// EngineMount is implemented by server.PageState and server.PageRuntime.
type EngineMount interface {
	Engine(engine.Config, gosx.Node) gosx.Node
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
	if r == nil || r.sceneBuilder == nil {
		return
	}
	props := r.sceneBuilder(ctx)
	r.latestScene = latestScene{props: props, ok: true}
	if r.physics == nil {
		r.physics = PhysicsFromScene(props)
	}
}

// PhysicsFromScene builds a physics.World from the Scene3D physics declaration.
func PhysicsFromScene(props scene.Props) *physics.World {
	ir := props.CanonicalIR()
	if ir.Physics == nil {
		return nil
	}
	return physics.BuildWorld(ir.PhysicsSpec())
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

// Mount registers the runtime's engine config with a page/runtime mount target.
func (r *Runtime) Mount(target EngineMount, fallback gosx.Node) gosx.Node {
	if target == nil {
		return fallback
	}
	return target.Engine(r.EngineConfig(), fallback)
}
