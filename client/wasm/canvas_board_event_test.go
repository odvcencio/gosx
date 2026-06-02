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

// gridBoardJSON encodes a multi-rect board program to wire JSON for the marquee
// / nav wasm tests. specs are (id, x, y, w, h) tuples; rects paint in order.
func gridBoardJSON(t *testing.T, specs [][5]any) string {
	t.Helper()
	prog := &rootengine.Program{Name: "WasmGridBoard"}
	push := func(e program.Expr) program.ExprID {
		prog.Exprs = append(prog.Exprs, e)
		return program.ExprID(len(prog.Exprs) - 1)
	}
	for _, s := range specs {
		id := s[0].(string)
		x := s[1].(float64)
		y := s[2].(float64)
		w := s[3].(float64)
		h := s[4].(float64)
		prog.EngineNodes = append(prog.EngineNodes, rootengine.Node{
			Kind: "rect",
			Props: map[string]program.ExprID{
				"x":      push(program.Expr{Op: program.OpLitFloat, Value: ftoaWasm(x), Type: program.TypeFloat}),
				"y":      push(program.Expr{Op: program.OpLitFloat, Value: ftoaWasm(y), Type: program.TypeFloat}),
				"width":  push(program.Expr{Op: program.OpLitFloat, Value: ftoaWasm(w), Type: program.TypeFloat}),
				"height": push(program.Expr{Op: program.OpLitFloat, Value: ftoaWasm(h), Type: program.TypeFloat}),
				"id":     push(program.Expr{Op: program.OpLitString, Value: id, Type: program.TypeString}),
			},
		})
	}
	data, err := rootengine.EncodeProgramJSON(prog)
	if err != nil {
		t.Fatalf("encode program: %v", err)
	}
	return string(data)
}

func sharedSignalString(name string) string {
	got := js.Global().Get("__gosx_get_shared_signal").Invoke(name)
	if got.Type() != js.TypeString {
		return ""
	}
	s := got.String()
	// The getter returns JSON; a string value comes back quoted.
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// TestCanvasEventMarqueeWritesSelectedIDs verifies a "marquee" event over two
// rects writes the comma-joined ids into $surface.event.selectedIDs through the
// public __gosx_canvas_event entrypoint.
func TestCanvasEventMarqueeWritesSelectedIDs(t *testing.T) {
	b := bridge.New()
	registerRuntime(b)

	// Two rects: world x∈[0,100] and x∈[150,250], y∈[0,100].
	progJSON := gridBoardJSON(t, [][5]any{
		{"node-A", 0.0, 0.0, 100.0, 100.0},
		{"node-B", 150.0, 0.0, 100.0, 100.0},
	})
	if ret := js.Global().Get("__gosx_hydrate_canvas").Invoke("cb-mq", "Board", `{}`, progJSON, "json"); !ret.IsNull() {
		t.Fatalf("hydrate: %v", ret)
	}
	// Screen rect covering both rects (cssW=600,cssH=400, pan=0, zoom=1):
	// world (-20,-20)→(300,120) maps to screen (280,220)→(600,80).
	ret := js.Global().Get("__gosx_canvas_event").Invoke("cb-mq", int(bridge.CanvasBoardEventMarquee), float64ArrayFromGo([]float64{280, 80, 600, 220, 600, 400}), "")
	if !ret.IsNull() {
		t.Fatalf("marquee event returned: %v", ret)
	}
	if got := sharedSignalString("$surface.event.selectedIDs"); got != "node-A,node-B" {
		t.Errorf("selectedIDs = %q, want node-A,node-B", got)
	}
	if got := sharedSignalString("$surface.event.selectedID"); got != "node-A" {
		t.Errorf("primary selectedID = %q, want node-A", got)
	}
}

// TestCanvasEventNavMovesSelectedID verifies a "nav" event walks the selection
// to the spatial neighbor through __gosx_canvas_event.
func TestCanvasEventNavMovesSelectedID(t *testing.T) {
	b := bridge.New()
	registerRuntime(b)

	// C centered on world origin (0,0); R to its right at world center (100,0).
	progJSON := gridBoardJSON(t, [][5]any{
		{"C", -10.0, -10.0, 20.0, 20.0},
		{"R", 90.0, -10.0, 20.0, 20.0},
	})
	if ret := js.Global().Get("__gosx_hydrate_canvas").Invoke("cb-nav", "Board", `{}`, progJSON, "json"); !ret.IsNull() {
		t.Fatalf("hydrate: %v", ret)
	}
	// Seed selection on C via a pick at its screen center, then nav right.
	// cssW=cssH=400, pan=(0,0), zoom=1: C center world (0,0) → screen (200,200).
	if ret := js.Global().Get("__gosx_canvas_event").Invoke("cb-nav", int(bridge.CanvasBoardEventPick), float64ArrayFromGo([]float64{200, 200, 400, 400}), ""); !ret.IsNull() {
		t.Fatalf("seed pick returned: %v", ret)
	}
	if got := sharedSignalString("$surface.event.selectedID"); got != "C" {
		t.Fatalf("seed selection = %q, want C", got)
	}
	// Nav right → R.
	ret := js.Global().Get("__gosx_canvas_event").Invoke("cb-nav", int(bridge.CanvasBoardEventNav), float64ArrayFromGo([]float64{float64(bridge.CanvasNavRight)}), "")
	if !ret.IsNull() {
		t.Fatalf("nav event returned: %v", ret)
	}
	if got := sharedSignalString("$surface.event.selectedID"); got != "R" {
		t.Errorf("selectedID after nav right = %q, want R", got)
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
