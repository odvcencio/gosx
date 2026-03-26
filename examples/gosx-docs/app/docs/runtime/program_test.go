package docs

import (
	"testing"

	"github.com/odvcencio/gosx/client/enginevm"
	"github.com/odvcencio/gosx/client/vm"
	rootengine "github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/signal"
)

func TestSceneDemoProgramRespondsToSharedInputSignals(t *testing.T) {
	prog := SceneDemoProgram()
	rt := enginevm.New(prog, `{"width":720,"height":420}`)

	signals := make(map[string]*signal.Signal[vm.Value], len(prog.Signals))
	for _, def := range prog.Signals {
		sig := signal.New(rt.EvalExpr(def.Init))
		rt.SetSharedSignal(def.Name, sig)
		signals[def.Name] = sig
	}

	commands := rt.Reconcile()
	if len(commands) != 4 {
		t.Fatalf("expected initial create commands for camera + 3 meshes, got %d", len(commands))
	}

	signals["$input.pointer.x"].Set(vm.FloatVal(660))
	signals["$input.pointer.y"].Set(vm.FloatVal(120))
	signals["$input.key.arrowleft"].Set(vm.BoolVal(true))
	signals["$input.key.arrowup"].Set(vm.BoolVal(true))

	commands = rt.Reconcile()
	assertHasCommandKind(t, commands, rootengine.CommandSetCamera)
	assertHasCommandKind(t, commands, rootengine.CommandSetTransform)
	assertHasCommandKind(t, commands, rootengine.CommandSetMaterial)
}

func assertHasCommandKind(t *testing.T, commands []rootengine.Command, want rootengine.CommandKind) {
	t.Helper()
	for _, command := range commands {
		if command.Kind == want {
			return
		}
	}
	t.Fatalf("expected command %v in %#v", want, commands)
}
