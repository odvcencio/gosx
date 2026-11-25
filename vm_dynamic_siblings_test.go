//go:build !tinygo

package gosx_test

import (
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/client/vm"
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/signal"
)

const scoreBugSource = `package main

//gosx:island
func ScoreBug() Node {
	score := signal.NewShared("app.score", view.Score{Clock: "--:--"})
	cmd := signal.NewShared("app.cmd", view.Cmd{})
	poke := func() { cmd.Set(view.Cmd{ID: cmd.Get().ID + 1, Name: "poke"}) }
	return <div class="scorebug" id="scorebug" data-tick={score.Get().Tick}>
		<Each of={score.Get().Teams} as="team">
			<div class="bug-team" data-ball={team.Ball}><b>{team.Code}</b><span class="bug-points">{team.Points}</span></div>
		</Each>
		<span class="bug-clock">{score.Get().Clock}</span>
		<If cond={score.Get().Tick > 2}><i id="bug-late">late</i></If>
		<button type="button" id="bug-poke" onClick={poke}>Poke</button>
	</div>
}
`

func TestCompiledScoreBugKeepsSharedParentInStep(t *testing.T) {
	prog, err := gosx.Compile([]byte(scoreBugSource))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	p, err := ir.LowerIsland(prog, 0)
	if err != nil {
		t.Fatalf("lower: %v", err)
	}
	isl := vm.NewIsland(p, "{}")
	sig := signal.New(scoreValue(0, "--:--"))
	isl.SetSharedSignal("$app.score", sig)
	isl.SetSharedSignal("$app.cmd", signal.New(vm.ObjectVal(map[string]vm.Value{})))
	isl.Reconcile()
	// Root children of the div after the first two writes. "#" is a text node.
	// The server render has [#, span, #, button, #].
	kids := []string{"#", "span", "#", "button", "#"}
	for step, st := range []vm.Value{scoreValue(1, "a", "AAA", "BBB"), scoreValue(3, "b", "AAA", "BBB")} {
		sig.Set(st)
		ops := isl.Reconcile()
		kids = replayRootKids(t, kids, ops)
		t.Logf("step %d children: %v", step, kids)
	}
	count := func(tag string) (n int) {
		for _, k := range kids {
			if k == tag {
				n++
			}
		}
		return n
	}
	if got := count("button"); got != 1 {
		t.Errorf("button#bug-poke appears %d times, want 1: %v", got, kids)
	}
	if got := count("i"); got != 1 {
		t.Errorf("#bug-late appears %d times, want 1 (Tick 3 > 2): %v", got, kids)
	}
	if got := count("div"); got != 2 {
		t.Errorf("div.bug-team appears %d times, want 2: %v", got, kids)
	}
}

// replayRootKids applies the ops that address the island root's own children
// (the ones this fault corrupts) to a list of child tags.
func replayRootKids(t *testing.T, kids []string, ops []vm.PatchOp) []string {
	t.Helper()
	for _, op := range ops {
		switch {
		case op.Kind == vm.PatchCreateElement && op.Path == "":
			i := op.Children[0]
			kids = append(kids[:i], append([]string{op.Tag}, kids[i:]...)...)
		case op.Kind == vm.PatchCreateText && op.Path == "":
			i := op.Children[0]
			kids = append(kids[:i], append([]string{"#"}, kids[i:]...)...)
		case op.Kind == vm.PatchReplaceElement && !strings.Contains(op.Path, "/") && op.Path != "":
			i, _ := strconv.Atoi(op.Path)
			kids[i] = op.Tag
		case op.Kind == vm.PatchRemoveElement && !strings.Contains(op.Path, "/"):
			i, _ := strconv.Atoi(op.Path)
			kids = append(kids[:i], kids[i+1:]...)
		}
	}
	return kids
}

func scoreValue(tick int, clock string, teams ...string) vm.Value {
	rows := make([]vm.Value, len(teams))
	for i, c := range teams {
		rows[i] = vm.ObjectVal(map[string]vm.Value{"Code": vm.StringVal(c), "Points": vm.IntVal(i), "Ball": vm.BoolVal(i == 0)})
	}
	return vm.ObjectVal(map[string]vm.Value{"Teams": vm.ArrayVal(rows), "Clock": vm.StringVal(clock), "Tick": vm.IntVal(tick)})
}
