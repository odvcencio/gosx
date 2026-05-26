package enginevm

import (
	"testing"

	"m31labs.dev/gosx/client/vm"
	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
)

// TestRuntimeWrapperRoutesToSceneAdapter confirms the deprecated enginevm
// shim still produces a working *vm.SceneAdapter. Phase 1c moved the
// scene-engine reconciler implementation into client/vm/scene_adapter.go;
// enginevm.Runtime is now a type alias for vm.SceneAdapter and enginevm.New
// is a one-line forwarder. This test guards against the alias drifting.
func TestRuntimeWrapperRoutesToSceneAdapter(t *testing.T) {
	prog := &rootengine.Program{
		Name: "WrapperSmoke",
		EngineNodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"x": 0,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
		},
	}

	rt := New(prog, `{}`)
	if rt == nil {
		t.Fatal("New returned nil")
	}

	// Type alias: the wrapper's *Runtime is the same concrete type as
	// *vm.SceneAdapter. If this assertion stops compiling, the alias has
	// diverged.
	var adapter *vm.SceneAdapter = rt
	if adapter == nil {
		t.Fatal("type assertion to *vm.SceneAdapter failed")
	}

	commands := rt.Reconcile()
	if len(commands) != 1 {
		t.Fatalf("expected 1 create command from the wrapper, got %d", len(commands))
	}
	if commands[0].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected CommandCreateObject, got %v", commands[0].Kind)
	}
}

// TestRegisterMaterialProfileWrapper confirms the enginevm.RegisterMaterialProfile
// forwarder calls vm.RegisterMaterialProfile and that the returned cleanup
// removes the registration.
func TestRegisterMaterialProfileWrapper(t *testing.T) {
	cleanup := RegisterMaterialProfile("test-wrapper-profile", MaterialProfile{
		HasOpacity: true,
		Opacity:    0.5,
	})
	if cleanup == nil {
		t.Fatal("RegisterMaterialProfile returned nil cleanup")
	}
	cleanup()
}
