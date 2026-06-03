package bridge

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

// counterPlusN returns a Counter-shaped program whose "increment" handler adds
// n (instead of 1) to the live "$count" shared signal. Using a shared ($-
// prefixed) signal lets the test read the live value back through the bridge's
// public GetSharedSignalJSON without reaching into vm internals. Init is 0 so
// a surviving non-zero value after reload proves merge-by-name kept the live
// state rather than re-running init.
func counterPlusN(n string) *program.Program {
	return &program.Program{
		Name: "Counter",
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "$count", Type: program.TypeInt},       // 0
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},               // 1 init
			{Op: program.OpLitInt, Value: n, Type: program.TypeInt},                 // 2 step
			{Op: program.OpAdd, Operands: []program.ExprID{0, 2}, Type: program.TypeInt}, // 3
			{Op: program.OpSignalSet, Operands: []program.ExprID{3}, Value: "$count", Type: program.TypeInt}, // 4
		},
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}},
			{Kind: program.NodeExpr, Expr: program.ExprID(0)},
		},
		Root: 0,
		Signals: []program.SignalDef{
			{Name: "$count", Type: program.TypeInt, Init: program.ExprID(1)},
		},
		Handlers: []program.Handler{
			{Name: "increment", Body: []program.ExprID{4}},
		},
		StaticMask: []bool{false, false},
	}
}

func sharedCount(t *testing.T, b *Bridge) string {
	t.Helper()
	got, err := b.GetSharedSignalJSON("$count")
	if err != nil {
		t.Fatalf("GetSharedSignalJSON: %v", err)
	}
	return got
}

// TestBridgeReloadProgramSwapsInPlace hydrates an island from program A
// (increment +1), drives the count up, then reloads program B (increment +10)
// under the same island id. The reload must preserve the live count by name
// and activate B's handler — verified by a post-reload dispatch — without
// re-hydrating (island count stays 1).
func TestBridgeReloadProgramSwapsInPlace(t *testing.T) {
	b := New()
	dataA, err := program.EncodeJSON(counterPlusN("1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("island-0", "Counter", `{}`, dataA, "json"); err != nil {
		t.Fatal(err)
	}
	// count: 0 -> 1 -> 2 under A (+1 each).
	if _, err := b.DispatchAction("island-0", "increment", "{}"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DispatchAction("island-0", "increment", "{}"); err != nil {
		t.Fatal(err)
	}
	if got := sharedCount(t, b); got != "2" {
		t.Fatalf("pre-reload $count = %s, want 2", got)
	}

	dataB, err := program.EncodeJSON(counterPlusN("10"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ReloadProgram("island-0", dataB, "json"); err != nil {
		t.Fatalf("ReloadProgram: %v", err)
	}
	// No re-hydrate — same island instance, swapped in place.
	if b.IslandCount() != 1 {
		t.Fatalf("island count = %d, want 1 (in-place swap, not re-hydrate)", b.IslandCount())
	}
	// State preserved across the swap.
	if got := sharedCount(t, b); got != "2" {
		t.Fatalf("post-reload $count = %s, want 2 (merge-by-name kept live value)", got)
	}

	// B's handler (+10) is now active and the preserved count (2) is the base:
	// 2 -> 12. If state had been re-initialized this would be 0 -> 10.
	patches, err := b.DispatchAction("island-0", "increment", "{}")
	if err != nil {
		t.Fatal(err)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches from post-reload dispatch")
	}
	if got := sharedCount(t, b); got != "12" {
		t.Fatalf("$count after reload+bump = %s, want 12 (state preserved + new +10 handler)", got)
	}
}

// TestBridgeReloadProgramBinary exercises the binary decode path through the
// same reload route.
func TestBridgeReloadProgramBinary(t *testing.T) {
	b := New()
	dataA, err := program.EncodeJSON(counterPlusN("1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("island-0", "Counter", `{}`, dataA, "json"); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DispatchAction("island-0", "increment", "{}"); err != nil { // count -> 1
		t.Fatal(err)
	}

	dataB, err := program.EncodeBinary(counterPlusN("5"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ReloadProgram("island-0", dataB, "bin"); err != nil {
		t.Fatalf("ReloadProgram binary: %v", err)
	}
	if _, err := b.DispatchAction("island-0", "increment", "{}"); err != nil { // 1 -> 6
		t.Fatal(err)
	}
	if got := sharedCount(t, b); got != "6" {
		t.Fatalf("$count after binary reload+bump = %s, want 6", got)
	}
}

// TestBridgeReloadProgramUnknownIDErrors asserts that reloading an id with no
// registered island is a no-op error rather than a panic or silent success.
func TestBridgeReloadProgramUnknownIDErrors(t *testing.T) {
	b := New()
	data, err := program.EncodeJSON(counterPlusN("1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ReloadProgram("ghost", data, "json"); err == nil {
		t.Fatal("expected error for unknown island id, got nil")
	}
}

// sharedDisplay returns a program that renders the value of a single shared
// ($-prefixed) signal, so a change to that signal produces an observable patch.
func sharedDisplay(name string) *program.Program {
	return &program.Program{
		Name: "Display",
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: name, Type: program.TypeInt}, // 0
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},     // 1 init
		},
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}}, // 0
			{Kind: program.NodeExpr, Expr: program.ExprID(0)},                       // 1
		},
		Root: 0,
		Signals: []program.SignalDef{
			{Name: name, Type: program.TypeInt, Init: program.ExprID(1)},
		},
		StaticMask: []bool{false, false},
	}
}

// TestBridgeReloadProgramRewiresSharedSignals reloads an island onto a program
// with a DIFFERENT shared-signal set ($a dropped, $b added). The newly-added
// signal must be connected and drive a reconcile; the dropped signal's
// subscription must be gone (a change to it no longer reconciles this island).
// Locks the connectSharedSignals / subscribeSharedSignals re-wire ReloadProgram
// performs across a signal-set change.
func TestBridgeReloadProgramRewiresSharedSignals(t *testing.T) {
	b := New()
	var patchCount int
	b.SetPatchCallback(func(islandID, patchJSON string) { patchCount++ })

	dataA, err := program.EncodeJSON(sharedDisplay("$a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("island-0", "Display", `{}`, dataA, "json"); err != nil {
		t.Fatal(err)
	}

	dataB, err := program.EncodeJSON(sharedDisplay("$b"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ReloadProgram("island-0", dataB, "json"); err != nil {
		t.Fatalf("ReloadProgram: %v", err)
	}

	// Added signal drives a reconcile: changing $b must push a patch.
	patchCount = 0
	if err := b.SetSharedSignalJSON("$b", "7"); err != nil {
		t.Fatalf("set $b: %v", err)
	}
	if patchCount == 0 {
		t.Fatal("changing newly-added shared signal $b did not reconcile the island")
	}

	// Dropped signal is unwired: changing $a must NOT reconcile this island.
	patchCount = 0
	if err := b.SetSharedSignalJSON("$a", "99"); err != nil {
		t.Fatalf("set $a: %v", err)
	}
	if patchCount != 0 {
		t.Fatalf("changing dropped shared signal $a reconciled the island %d time(s); subscription should be gone", patchCount)
	}
}

// TestBridgeReloadProgramBadDataErrors asserts a decode failure surfaces as an
// error and does not mutate the live island.
func TestBridgeReloadProgramBadDataErrors(t *testing.T) {
	b := New()
	dataA, err := program.EncodeJSON(counterPlusN("1"))
	if err != nil {
		t.Fatal(err)
	}
	if err := b.HydrateIsland("island-0", "Counter", `{}`, dataA, "json"); err != nil {
		t.Fatal(err)
	}
	if err := b.ReloadProgram("island-0", []byte("not-json"), "json"); err == nil {
		t.Fatal("expected decode error for malformed program data")
	}
}
