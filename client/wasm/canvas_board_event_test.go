//go:build js && wasm && !gosx_tiny_islands_only

package main

import (
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/bridge"
	rootengine "m31labs.dev/gosx/engine"
	"m31labs.dev/gosx/island/program"
)

// float64ArrayFromGo materializes a JS Float64Array from a Go slice — the
// payload shape __gosx_canvas_event expects for its numeric args.
func float64ArrayFromGo(xs []float64) js.Value {
	arr := js.Global().Get("Float64Array").New(len(xs))
	for i, x := range xs {
		arr.SetIndex(i, x)
	}
	return arr
}

// rectBoardJSON encodes a single-rect board program to wire JSON so the wasm
// test can hydrate it through __gosx_hydrate_canvas (the public mount path).
func rectBoardJSON(t *testing.T, id string, x, y, w, h float64) string {
	t.Helper()
	lit := func(f float64) program.Expr {
		return program.Expr{Op: program.OpLitFloat, Value: ftoaWasm(f), Type: program.TypeFloat}
	}
	prog := &rootengine.Program{
		Name: "WasmEventBoard",
		EngineNodes: []rootengine.Node{
			{Kind: "rect", Props: map[string]program.ExprID{"x": 0, "y": 1, "width": 2, "height": 3, "id": 4}},
		},
		Exprs: []program.Expr{
			lit(x), lit(y), lit(w), lit(h),
			{Op: program.OpLitString, Value: id, Type: program.TypeString},
		},
	}
	data, err := rootengine.EncodeProgramJSON(prog)
	if err != nil {
		t.Fatalf("encode program: %v", err)
	}
	return string(data)
}

func ftoaWasm(f float64) string {
	neg := f < 0
	n := int(f)
	if neg {
		n = -n
	}
	if n == 0 {
		return "0"
	}
	var buf []byte
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	if neg {
		buf = append([]byte{'-'}, buf...)
	}
	return string(buf)
}

// TestCanvasEventRegisteredAndPans is the wasm keystone: __gosx_canvas_event is
// registered by the full-build runtime, and a "pan" event mutates the board's
// camera such that the next __gosx_render_canvas reflects the new pan.
func TestCanvasEventRegisteredAndPans(t *testing.T) {
	b := bridge.New()
	registerRuntime(b)

	eventFn := js.Global().Get("__gosx_canvas_event")
	if eventFn.Type() != js.TypeFunction {
		t.Fatalf("__gosx_canvas_event not registered (type %v)", eventFn.Type())
	}

	if ret := js.Global().Get("__gosx_hydrate_canvas").Invoke("cb-pan", "Board", `{"zoom":2}`, "{}", "json"); !ret.IsNull() {
		t.Fatalf("hydrate canvas: %v", ret)
	}

	// pan kind = 1 ; dx=20, dy=10 at zoom 2 → world delta (10,5) → pan (-10,+5).
	ret := eventFn.Invoke("cb-pan", int(bridge.CanvasBoardEventPan), float64ArrayFromGo([]float64{20, 10, 0, 0}), "")
	if !ret.IsNull() {
		t.Fatalf("pan event returned: %v", ret)
	}

	bundleJSON := js.Global().Get("__gosx_render_canvas").Invoke("cb-pan", 800, 600, 0)
	if bundleJSON.Type() != js.TypeString {
		t.Fatalf("render returned non-string: %v", bundleJSON)
	}
	parsed := js.Global().Get("JSON").Call("parse", bundleJSON.String())
	cam := parsed.Get("camera")
	if cam.Get("x").Float() != -10 {
		t.Errorf("panX = %v, want -10", cam.Get("x").Float())
	}
	if cam.Get("y").Float() != 5 {
		t.Errorf("panY = %v, want 5", cam.Get("y").Float())
	}
}

// TestCanvasEventZoom verifies a "zoom" event scales the rendered camera.
func TestCanvasEventZoom(t *testing.T) {
	b := bridge.New()
	registerRuntime(b)

	if ret := js.Global().Get("__gosx_hydrate_canvas").Invoke("cb-zoom", "Board", `{"zoom":1}`, "{}", "json"); !ret.IsNull() {
		t.Fatalf("hydrate: %v", ret)
	}
	// zoom kind=2: factor 2 toward center (400,300) of an 800x600 viewport.
	ret := js.Global().Get("__gosx_canvas_event").Invoke("cb-zoom", int(bridge.CanvasBoardEventZoom), float64ArrayFromGo([]float64{2, 400, 300, 800, 600}), "")
	if !ret.IsNull() {
		t.Fatalf("zoom event returned: %v", ret)
	}
	bundleJSON := js.Global().Get("__gosx_render_canvas").Invoke("cb-zoom", 800, 600, 0)
	parsed := js.Global().Get("JSON").Call("parse", bundleJSON.String())
	if z := parsed.Get("camera").Get("z").Float(); z != 2 {
		t.Errorf("zoom = %v, want 2", z)
	}
}

// TestCanvasEventPickWritesSignal verifies a "pick" event hit-tests through the
// camera and writes $surface.event.selectedID, readable via the shared-signal
// getter export.
func TestCanvasEventPickWritesSignal(t *testing.T) {
	b := bridge.New()
	registerRuntime(b)

	progJSON := rectBoardJSON(t, "node-A", 0, 0, 100, 100)
	if ret := js.Global().Get("__gosx_hydrate_canvas").Invoke("cb-pick", "Board", `{}`, progJSON, "json"); !ret.IsNull() {
		t.Fatalf("hydrate: %v", ret)
	}
	// World (50,50) at pan=(0,0) zoom=1, viewport 200x200 → screen (150,50).
	ret := js.Global().Get("__gosx_canvas_event").Invoke("cb-pick", int(bridge.CanvasBoardEventPick), float64ArrayFromGo([]float64{150, 50, 200, 200}), "")
	if !ret.IsNull() {
		t.Fatalf("pick event returned: %v", ret)
	}
	got := js.Global().Get("__gosx_get_shared_signal").Invoke("$surface.event.selectedID")
	if got.Type() != js.TypeString {
		t.Fatalf("get shared signal returned non-string: %v", got)
	}
	// The getter returns JSON; a string value serializes with quotes.
	if got.String() != `"node-A"` && got.String() != "node-A" {
		t.Errorf("selectedID = %s, want node-A", got.String())
	}
}

// TestCanvasEventUnknownBoardErrors verifies an event for an unregistered id
// returns an error string (not a panic).
func TestCanvasEventUnknownBoardErrors(t *testing.T) {
	b := bridge.New()
	registerRuntime(b)
	ret := js.Global().Get("__gosx_canvas_event").Invoke("ghost", int(bridge.CanvasBoardEventPan), float64ArrayFromGo([]float64{1, 1, 0, 0}), "")
	if ret.Type() != js.TypeString {
		t.Errorf("expected error string for unknown board, got %v", ret.Type())
	}
}
