// Package enginevm exists as a deprecated compatibility shim around
// vm.SceneAdapter. The scene reconciler implementation lives in
// m31labs.dev/gosx/client/vm/scene_adapter.go (plus scene_resolver.go and
// scene_render_bundle.go after Phase 1c's internal split). New code should
// depend on vm.SceneAdapter directly.
//
// This shim preserves the public surface that Phase 1b's bridge and existing
// downstream consumers depend on: enginevm.Runtime (aliased to
// vm.SceneAdapter), enginevm.New (calls vm.NewSceneAdapter), and the
// material-profile registration entry points. The shim is slated for removal
// in Phase 1d once downstream consumers migrate to vm.SceneAdapter.
package enginevm

import (
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/client/vm"
)

// Runtime is the legacy public alias for vm.SceneAdapter.
//
// Deprecated: use vm.SceneAdapter directly.
type Runtime = vm.SceneAdapter

// MaterialProfile is the legacy alias for the material-profile preset type.
//
// Deprecated: use vm.MaterialProfile.
type MaterialProfile = vm.MaterialProfile

// New constructs a live scene-engine runtime. Deprecated wrapper around
// vm.NewSceneAdapter — prefer the new constructor in new code.
//
// Deprecated: use vm.NewSceneAdapter.
func New(prog *rootengine.Program, propsJSON string) *Runtime {
	return vm.NewSceneAdapter(prog, propsJSON)
}

// RegisterMaterialProfile installs or replaces a material profile and returns
// a cleanup function that restores the previous profile for that kind. This
// is a thin pass-through to vm.RegisterMaterialProfile preserved for legacy
// callers.
//
// Deprecated: use vm.RegisterMaterialProfile.
func RegisterMaterialProfile(kind string, profile MaterialProfile) func() {
	return vm.RegisterMaterialProfile(kind, profile)
}
