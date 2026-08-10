package vm

import (
	"encoding/json"
	"testing"

	rootengine "m31labs.dev/gosx/engine"
	islandprogram "m31labs.dev/gosx/island/program"
	"m31labs.dev/gosx/signal"
)

func computedSurfaceProgram(kind, baseName string) *rootengine.Program {
	return &rootengine.Program{
		Name: "ComputedSurface",
		Exprs: []islandprogram.Expr{
			{Op: islandprogram.OpLitFloat, Value: "1", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: baseName, Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpLitFloat, Value: "2", Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpMul, Operands: []islandprogram.ExprID{1, 2}, Type: islandprogram.TypeFloat},
			{Op: islandprogram.OpSignalGet, Value: "scaled", Type: islandprogram.TypeFloat},
		},
		Signals: []islandprogram.SignalDef{
			{Name: baseName, Type: islandprogram.TypeFloat, Init: 0},
		},
		Computeds: []islandprogram.ComputedDef{
			{Name: "scaled", Type: islandprogram.TypeFloat, Expr: 3},
		},
		EngineNodes: []islandprogram.EngineNode{
			{Kind: kind, Props: map[string]islandprogram.ExprID{"x": 4}},
		},
	}
}

func commandNumber(t *testing.T, command rootengine.Command, name string) float64 {
	t.Helper()
	var data map[string]any
	if err := json.Unmarshal(command.Data, &data); err != nil {
		t.Fatalf("decode command data %s: %v", command.Data, err)
	}
	value, ok := data[name].(float64)
	if !ok {
		t.Fatalf("command data %s has no numeric %q", command.Data, name)
	}
	return value
}

func TestSceneAdapterComputedDependencyTracksSharedBase(t *testing.T) {
	prog := computedSurfaceProgram("mesh", "$scene.x")
	rt := NewSceneAdapter(prog, `{}`)
	shared := signal.New(FloatVal(5))
	rt.SetSharedSignal("$scene.x", shared)

	initial := rt.Reconcile()
	if len(initial) != 1 || initial[0].Kind != rootengine.CommandCreateObject {
		t.Fatalf("initial scene commands = %#v, want one CreateObject", initial)
	}
	shared.Set(FloatVal(7))
	commands := rt.Reconcile()
	if len(commands) != 1 || commands[0].Kind != rootengine.CommandSetTransform {
		t.Fatalf("scene commands after computed base write = %#v, want one SetTransform", commands)
	}
	if got := commandNumber(t, commands[0], "x"); got != 14 {
		t.Fatalf("scene computed x = %v, want 14", got)
	}
}

func TestCanvasBoardAdapterComputedDependencyTracksSharedBase(t *testing.T) {
	prog := computedSurfaceProgram("rect", "$board.x")
	rt := NewCanvasBoardAdapter(prog, `{}`)
	shared := signal.New(FloatVal(5))
	rt.SetSharedSignal("$board.x", shared)

	initial := rt.Reconcile()
	if len(initial) != 1 || initial[0].Kind != rootengine.CommandCreateObject {
		t.Fatalf("initial board commands = %#v, want one CreateObject", initial)
	}
	shared.Set(FloatVal(7))
	commands := rt.Reconcile()
	if len(commands) != 1 || commands[0].Kind != rootengine.CommandSetTransform {
		t.Fatalf("board commands after computed base write = %#v, want one SetTransform", commands)
	}
	if got := commandNumber(t, commands[0], "x"); got != 14 {
		t.Fatalf("board computed x = %v, want 14", got)
	}
}
