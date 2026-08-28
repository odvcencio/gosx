package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestParseEventDataPreservesTypedAndStructuredFields(t *testing.T) {
	data := parseEventData(`{
		"checked":true,
		"clientX":13,
		"timeStamp":3000000000,
		"pointerID":7,
		"customIntegral":13,
		"data":{"sessionId":"s-1"},
		"eventData":"s-1"
	}`)
	if !data["checked"].Truth() || data["clientX"].Number() != 13 || data["pointerID"].Number() != 7 {
		t.Fatalf("typed scalar event data = %+v", data)
	}
	if data["clientX"].Type != program.TypeFloat || data["timeStamp"].Type != program.TypeFloat ||
		data["timeStamp"].Number() != 3000000000 {
		t.Fatalf("known float fields = clientX(%v,%v) timeStamp(%v,%v)",
			data["clientX"].Type, data["clientX"].Number(), data["timeStamp"].Type, data["timeStamp"].Number())
	}
	if data["customIntegral"].Type != program.TypeInt {
		t.Fatalf("generic integral payload type = %v, want TypeInt", data["customIntegral"].Type)
	}
	dataset := data["data"].Map()
	if dataset["sessionId"].String() != "s-1" || data["eventData"].String() != "s-1" {
		t.Fatalf("structured/transfer event data = %+v / %q", dataset, data["eventData"].String())
	}
}

func TestIslandEventMarkerConventionsAreStable(t *testing.T) {
	tests := map[string]string{
		"onDragStart":       "data-gosx-on-dragstart",
		"onDrop":            "data-gosx-on-drop",
		"onPointerDown":     "data-gosx-on-pointerdown",
		"onPointerCancel":   "data-gosx-on-pointercancel",
		"onDocumentKeyDown": "data-gosx-on-document-keydown",
		"onDocumentKeyUp":   "data-gosx-on-document-keyup",
		"onWindowResize":    "data-gosx-on-window-resize",
	}
	for source, marker := range tests {
		if got := eventMarkerAttr(eventAttrType(source)); got != marker {
			t.Errorf("event marker %s = %q, want %q", source, got, marker)
		}
	}
}

func TestMissingEventPayloadUsesTypedZeroValues(t *testing.T) {
	prog := &program.Program{
		Name: "MissingEventFields",
		Exprs: []program.Expr{
			{Op: program.OpLitString, Value: "seed", Type: program.TypeString},
			{Op: program.OpLitBool, Value: "true", Type: program.TypeBool},
			{Op: program.OpLitInt, Value: "5", Type: program.TypeInt},
			{Op: program.OpEventGet, Value: "value", Type: program.TypeString},
			{Op: program.OpSignalSet, Value: "text", Operands: []program.ExprID{3}, Type: program.TypeString},
			{Op: program.OpEventGet, Value: "key", Type: program.TypeString},
			{Op: program.OpSignalSet, Value: "key", Operands: []program.ExprID{5}, Type: program.TypeString},
			{Op: program.OpEventGet, Value: "checked", Type: program.TypeBool},
			{Op: program.OpSignalSet, Value: "checked", Operands: []program.ExprID{7}, Type: program.TypeBool},
			{Op: program.OpEventGet, Value: "selectedIndex", Type: program.TypeInt},
			{Op: program.OpSignalSet, Value: "selected", Operands: []program.ExprID{9}, Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "text", Type: program.TypeString},
		},
		Signals: []program.SignalDef{
			{Name: "text", Type: program.TypeString, Init: 0},
			{Name: "key", Type: program.TypeString, Init: 0},
			{Name: "checked", Type: program.TypeBool, Init: 1},
			{Name: "selected", Type: program.TypeInt, Init: 2},
		},
		Handlers: []program.Handler{{Name: "capture", Body: []program.ExprID{4, 6, 8, 10}}},
		Nodes:    []program.Node{{Kind: program.NodeExpr, Expr: 11}},
		Root:     0,
	}
	for _, payload := range []string{`{}`, `{"type":"click"}`} {
		island := NewIsland(prog, `{}`)
		island.Dispatch("capture", payload)
		if got := island.vm.signals["text"].Get(); got.Type != program.TypeString || got.String() != "" {
			t.Errorf("payload %s missing value = %#v/%q, want typed empty string", payload, got.Type, got.String())
		}
		if got := island.vm.signals["key"].Get(); got.Type != program.TypeString || got.String() != "" {
			t.Errorf("payload %s missing key = %#v/%q, want typed empty string", payload, got.Type, got.String())
		}
		if got := island.vm.signals["checked"].Get(); got.Type != program.TypeBool || got.Truth() {
			t.Errorf("payload %s missing checked = %#v/%v, want typed false", payload, got.Type, got.Truth())
		}
		if got := island.vm.signals["selected"].Get(); got.Type != program.TypeInt || got.Number() != 0 {
			t.Errorf("payload %s missing selectedIndex = %#v/%v, want typed zero", payload, got.Type, got.Number())
		}
	}
}

func TestIntegralJSONEventCoordinateUsesDeclaredFloatArithmetic(t *testing.T) {
	prog := &program.Program{
		Name: "FloatEventField",
		Exprs: []program.Expr{
			{Op: program.OpLitFloat, Value: "0", Type: program.TypeFloat},
			{Op: program.OpEventGet, Value: "clientX", Type: program.TypeFloat},
			{Op: program.OpLitInt, Value: "2", Type: program.TypeInt},
			{Op: program.OpDiv, Operands: []program.ExprID{1, 2}, Type: program.TypeFloat},
			{Op: program.OpSignalSet, Value: "half", Operands: []program.ExprID{3}, Type: program.TypeFloat},
		},
		Signals:  []program.SignalDef{{Name: "half", Type: program.TypeFloat, Init: 0}},
		Handlers: []program.Handler{{Name: "capture", Body: []program.ExprID{4}}},
	}
	island := NewIsland(prog, `{}`)
	island.Dispatch("capture", `{"clientX":13}`)
	got := island.vm.signals["half"].Get()
	if got.Type != program.TypeFloat || got.Number() != 6.5 {
		t.Fatalf("integral clientX / 2 = %#v/%v, want float 6.5", got.Type, got.Number())
	}
}
