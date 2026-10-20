package ir_test

import (
	"fmt"
	"testing"

	"m31labs.dev/gosx/ir"
)

func TestLowerSharedSignalConstructors(t *testing.T) {
	for _, constructor := range []string{"NewShared", "Shared", "NewShared[int]", "Shared[int]"} {
		for _, name := range []string{"selection", "$selection"} {
			t.Run(constructor+"/"+name, func(t *testing.T) {
				prog, err := parse(t, []byte(fmt.Sprintf(`package main
import sig "m31labs.dev/gosx/signal"
//gosx:island
func HUD() Node {
    selected := sig.%s(%q, 3)
    return <button onClick={func() { selected.Set(4) }}>{selected.Get()}</button>
}
`, constructor, name)))
				if err != nil {
					t.Fatal(err)
				}
				signals := prog.Components[0].Scope.Signals
				if len(signals) != 1 || signals[0].Name != "$selection" || signals[0].Local != "selected" || signals[0].InitExpr != "3" {
					t.Fatalf("shared lowering = %+v", signals)
				}
				island, err := ir.LowerIsland(prog, 0)
				if err != nil {
					t.Fatal(err)
				}
				if len(island.Signals) != 1 || island.Signals[0].Name != "$selection" {
					t.Fatalf("island signals = %+v", island.Signals)
				}
			})
		}
	}
}
