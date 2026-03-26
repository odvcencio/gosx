package enginevm

import (
	"testing"

	"github.com/odvcencio/gosx/client/vm"
	rootengine "github.com/odvcencio/gosx/engine"
	islandprogram "github.com/odvcencio/gosx/island/program"
	"github.com/odvcencio/gosx/signal"
)

func TestRuntimeInitialReconcileCreatesObjects(t *testing.T) {
	prog := &rootengine.Program{
		Name: "GeometryZoo",
		Nodes: []rootengine.Node{
			{
				Kind: "camera",
				Props: map[string]islandprogram.ExprID{
					"x": 0,
					"y": 1,
					"z": 2,
				},
			},
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":     3,
					"color": 4,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "0.5", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "6", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "-1.2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
	}

	rt := New(prog, `{}`)
	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected 2 create commands, got %d", len(commands))
	}
	if commands[0].Kind != rootengine.CommandCreateObject || commands[1].Kind != rootengine.CommandCreateObject {
		t.Fatalf("expected create commands, got %#v", commands)
	}
}

func TestRuntimeTickProducesIncrementalMaterialAndTransformCommands(t *testing.T) {
	prog := &rootengine.Program{
		Name: "GeometryZoo",
		Nodes: []rootengine.Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.color", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 2},
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 3},
		},
	}

	rt := New(prog, `{}`)
	xSig := signal.New(vm.FloatVal(0))
	colorSig := signal.New(vm.StringVal("#8de1ff"))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.SetSharedSignal("$scene.color", colorSig)

	if commands := rt.Reconcile(); len(commands) != 1 {
		t.Fatalf("expected initial create command, got %d", len(commands))
	}

	xSig.Set(vm.FloatVal(3.25))
	colorSig.Set(vm.StringVal("#ff8f6b"))
	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected transform + material commands, got %#v", commands)
	}
	if commands[0].Kind != rootengine.CommandSetTransform {
		t.Fatalf("expected first command to be transform, got %v", commands[0].Kind)
	}
	if commands[1].Kind != rootengine.CommandSetMaterial {
		t.Fatalf("expected second command to be material, got %v", commands[1].Kind)
	}
}

func TestRuntimeMarksOnlyDependentNodesDirty(t *testing.T) {
	prog := &rootengine.Program{
		Name: "DirtyTracking",
		Nodes: []rootengine.Node{
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"x": 0,
				},
			},
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"y": 1,
				},
			},
			{
				Kind:   "mesh",
				Static: true,
				Props: map[string]islandprogram.ExprID{
					"z": 2,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.y", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.static", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 0},
			{Name: "$scene.y", Type: islandprogram.TypeFloat, Init: 1},
			{Name: "$scene.static", Type: islandprogram.TypeFloat, Init: 2},
		},
	}

	rt := New(prog, `{}`)
	rt.Reconcile()
	if got := rt.dirty; got[0] || got[1] || got[2] {
		t.Fatalf("expected clean runtime after initial reconcile, got %#v", got)
	}

	xSig := signal.New(vm.FloatVal(0))
	rt.SetSharedSignal("$scene.x", xSig)
	clearDirty(rt.dirty)

	xSig.Set(vm.FloatVal(2.5))
	if !rt.dirty[0] {
		t.Fatal("expected first node to be dirty after $scene.x change")
	}
	if rt.dirty[1] {
		t.Fatal("expected second node to remain clean after $scene.x change")
	}
	if rt.dirty[2] {
		t.Fatal("expected static node to remain clean after $scene.x change")
	}
}

func TestRuntimeClearsDirtyFlagsAfterReconcile(t *testing.T) {
	prog := &rootengine.Program{
		Name: "DirtyReconcile",
		Nodes: []rootengine.Node{
			{
				Kind: "mesh",
				Props: map[string]islandprogram.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpSignalGet, Value: "$scene.x", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "$scene.color", Type: islandprogram.TypeString},
			{Op: islandprogram.OpLitFloat, Value: "0", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitString, Value: "#8de1ff", Type: islandprogram.TypeString},
		},
		Signals: []islandprogram.SignalDef{
			{Name: "$scene.x", Type: islandprogram.TypeFloat, Init: 2},
			{Name: "$scene.color", Type: islandprogram.TypeString, Init: 3},
		},
	}

	rt := New(prog, `{}`)
	xSig := signal.New(vm.FloatVal(0))
	colorSig := signal.New(vm.StringVal("#8de1ff"))
	rt.SetSharedSignal("$scene.x", xSig)
	rt.SetSharedSignal("$scene.color", colorSig)
	rt.Reconcile()

	xSig.Set(vm.FloatVal(1.25))
	colorSig.Set(vm.StringVal("#ffd48f"))
	if !rt.dirty[0] {
		t.Fatal("expected node to be dirty after shared signal changes")
	}

	commands := rt.Reconcile()
	if len(commands) != 2 {
		t.Fatalf("expected transform + material commands, got %#v", commands)
	}
	if rt.dirty[0] {
		t.Fatal("expected dirty flag to clear after reconcile")
	}
}
