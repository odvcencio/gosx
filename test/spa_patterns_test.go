package test

import (
	"testing"

	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/island/program"
)

func hydrateAndDispatch(t *testing.T, prog *program.Program, handler string) []byte {
	t.Helper()
	data, err := program.EncodeJSON(prog)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	b := bridge.New()
	if err := b.HydrateIsland("test-0", prog.Name, `{}`, data, "json"); err != nil {
		t.Fatalf("hydrate %s: %v", prog.Name, err)
	}
	patches, err := b.DispatchAction("test-0", handler, "{}")
	if err != nil {
		t.Fatalf("dispatch %s.%s: %v", prog.Name, handler, err)
	}
	patchJSON, _ := bridge.MarshalPatches(patches)
	return []byte(patchJSON)
}

func TestSPATabsSwitching(t *testing.T) {
	prog := program.TabsProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Tabs", `{}`, data, "json")

	// Switch to features tab
	patches, _ := b.DispatchAction("test-0", "showFeatures", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when switching tabs")
	}
	json, _ := bridge.MarshalPatches(patches)
	t.Logf("Tab switch patches: %s", json)

	// Switch to contact tab
	patches, _ = b.DispatchAction("test-0", "showContact", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches for contact tab")
	}

	// Switch back to about
	patches, _ = b.DispatchAction("test-0", "showAbout", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches for about tab")
	}
}

func TestSPAToggle(t *testing.T) {
	prog := program.ToggleProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Toggle", `{}`, data, "json")

	// Toggle on
	patches, _ := b.DispatchAction("test-0", "toggle", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when toggling on")
	}
	json, _ := bridge.MarshalPatches(patches)
	t.Logf("Toggle on patches: %s", json)

	// Toggle off
	patches, _ = b.DispatchAction("test-0", "toggle", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when toggling off")
	}
}

func TestSPATodo(t *testing.T) {
	prog := program.TodoProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Todo", `{}`, data, "json")

	// Add item
	patches, _ := b.DispatchAction("test-0", "addItem", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when adding item")
	}
	json, _ := bridge.MarshalPatches(patches)
	t.Logf("Add item patches: %s", json)

	// Clear all
	patches, _ = b.DispatchAction("test-0", "clearAll", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when clearing")
	}
}

func TestSPAForm(t *testing.T) {
	prog := program.FormProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Form", `{}`, data, "json")

	// Fill name
	patches, _ := b.DispatchAction("test-0", "updateName", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when updating name")
	}
	json, _ := bridge.MarshalPatches(patches)
	t.Logf("Update name patches: %s", json)

	// Validate
	patches, _ = b.DispatchAction("test-0", "validateForm", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when validating")
	}
}

func TestSPADerived(t *testing.T) {
	prog := program.DerivedProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Derived", `{}`, data, "json")

	// Increment quantity
	patches, _ := b.DispatchAction("test-0", "incQuantity", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when incrementing quantity")
	}
	json, _ := bridge.MarshalPatches(patches)
	t.Logf("Inc quantity patches: %s", json)

	// Toggle discount
	patches, _ = b.DispatchAction("test-0", "toggleDiscount", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when toggling discount")
	}
	json2, _ := bridge.MarshalPatches(patches)
	t.Logf("Toggle discount patches: %s", json2)
}
