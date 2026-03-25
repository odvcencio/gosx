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

func TestSPATabsCSSClassToggling(t *testing.T) {
	prog := program.TabsProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Tabs", `{}`, data, "json")

	// Initially activeTab=0, so switching to Features should produce class patches
	patches, _ := b.DispatchAction("test-0", "showFeatures", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches including class changes when switching tabs")
	}
	patchJSON, _ := bridge.MarshalPatches(patches)
	patchStr := string(patchJSON)
	t.Logf("CSS class toggle patches: %s", patchStr)

	// Verify that SetAttr patches are present (class changes on buttons)
	foundSetAttr := false
	for _, p := range patches {
		if p.Kind == 1 { // PatchSetAttr
			foundSetAttr = true
		}
	}
	if !foundSetAttr {
		t.Error("expected PatchSetAttr patches for CSS class toggling")
	}
}

func TestSPAToggle(t *testing.T) {
	prog := program.ToggleProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Toggle", `{}`, data, "json")

	// Toggle on via click
	patches, _ := b.DispatchAction("test-0", "toggle", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when toggling on")
	}
	json, _ := bridge.MarshalPatches(patches)
	t.Logf("Toggle on patches: %s", json)

	// Toggle off via click
	patches, _ = b.DispatchAction("test-0", "toggle", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when toggling off")
	}
}

func TestSPAToggleKeyboard(t *testing.T) {
	prog := program.ToggleProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Toggle", `{}`, data, "json")

	// Toggle on via keyboard handler
	patches, _ := b.DispatchAction("test-0", "toggleKey", `{"key":"Enter"}`)
	if len(patches) == 0 {
		t.Fatal("expected patches when toggling via keyboard")
	}
	patchJSON, _ := bridge.MarshalPatches(patches)
	t.Logf("Toggle keyboard patches: %s", patchJSON)

	// Toggle off via keyboard handler
	patches, _ = b.DispatchAction("test-0", "toggleKey", `{"key":"Enter"}`)
	if len(patches) == 0 {
		t.Fatal("expected patches when toggling off via keyboard")
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

	// Fill name via button toggle
	patches, _ := b.DispatchAction("test-0", "fillName", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when filling name via button")
	}
	json, _ := bridge.MarshalPatches(patches)
	t.Logf("Fill name patches: %s", json)

	// Validate
	patches, _ = b.DispatchAction("test-0", "validateForm", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when validating")
	}
}

func TestSPAFormTwoWayBinding(t *testing.T) {
	prog := program.FormProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "Form", `{}`, data, "json")

	// Simulate typing in the input — dispatch with event data containing value
	patches, _ := b.DispatchAction("test-0", "updateName", `{"value":"Bob"}`)
	if len(patches) == 0 {
		t.Fatal("expected patches when updating name via two-way binding")
	}
	patchJSON, _ := bridge.MarshalPatches(patches)
	patchStr := string(patchJSON)
	t.Logf("Two-way binding patches: %s", patchStr)

	// The name signal should now be "Bob" — validate should set valid=true
	patches, _ = b.DispatchAction("test-0", "validateForm", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when validating after two-way binding")
	}
	patchJSON2, _ := bridge.MarshalPatches(patches)
	t.Logf("Validate after binding: %s", patchJSON2)
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

func TestSPAListAddItem(t *testing.T) {
	prog := program.ListProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "List", `{}`, data, "json")

	// Add item with event data
	patches, _ := b.DispatchAction("test-0", "addItem", `{"value":"apple"}`)
	if len(patches) == 0 {
		t.Fatal("expected patches when adding item")
	}
	patchJSON, _ := bridge.MarshalPatches(patches)
	t.Logf("Add item patches: %s", patchJSON)

	// Add another item
	patches, _ = b.DispatchAction("test-0", "addItem", `{"value":"banana"}`)
	if len(patches) == 0 {
		t.Fatal("expected patches when adding second item")
	}
	patchJSON2, _ := bridge.MarshalPatches(patches)
	t.Logf("Add second item patches: %s", patchJSON2)
}

func TestSPAListRemoveAndClear(t *testing.T) {
	prog := program.ListProgram()
	data, _ := program.EncodeJSON(prog)
	b := bridge.New()
	b.HydrateIsland("test-0", "List", `{}`, data, "json")

	// Add items first
	b.DispatchAction("test-0", "addItem", `{"value":"x"}`)
	b.DispatchAction("test-0", "addItem", `{"value":"y"}`)

	// Remove last item (decrements count)
	patches, _ := b.DispatchAction("test-0", "removeLastItem", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when removing last item")
	}
	patchJSON, _ := bridge.MarshalPatches(patches)
	t.Logf("Remove last patches: %s", patchJSON)

	// Clear all
	patches, _ = b.DispatchAction("test-0", "clearItems", "{}")
	if len(patches) == 0 {
		t.Fatal("expected patches when clearing items")
	}
	patchJSON2, _ := bridge.MarshalPatches(patches)
	t.Logf("Clear all patches: %s", patchJSON2)
}
