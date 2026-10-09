package playground

import "testing"

func TestPlaygroundEventTypeMapsTypedGestureAttributes(t *testing.T) {
	for attr, want := range map[string]string{
		"onWheel":              "wheel",
		"onDblClick":           "dblclick",
		"onContextMenu":        "contextmenu",
		"onLostPointerCapture": "lostpointercapture",
	} {
		if got := playgroundEventType(attr); got != want {
			t.Errorf("playgroundEventType(%q) = %q, want %q", attr, got, want)
		}
		if _, ok := playgroundAllowedEvents[want]; !ok {
			t.Errorf("playgroundAllowedEvents lacks %q", want)
		}
	}
}

// TestCompileSourceAcceptsTypedGestureHandlers compiles the typed attributes
// end to end and checks the preview carries the DOM-named markers.
func TestCompileSourceAcceptsTypedGestureHandlers(t *testing.T) {
	source := `package playground

import "m31labs.dev/gosx/signal"

//gosx:island
func Gestures() Node {
	n := signal.New(0)
	bump := func() { n.Set(n.Get() + 1) }
	return <div tabIndex="0" onWheel={bump} onDblClick={bump} onContextMenu={bump} onLostPointerCapture={bump}>{n.Get()}</div>
}
`
	result, err := CompileSource([]byte(source))
	if err != nil {
		t.Fatalf("CompileSource: %v", err)
	}
	if len(result.Diagnostics) != 0 {
		t.Fatalf("typed gesture handlers rejected: %v", result.Diagnostics)
	}
	if len(result.Program) == 0 {
		t.Fatal("expected a compiled program")
	}
	for _, marker := range []string{
		"data-gosx-on-wheel", "data-gosx-on-dblclick",
		"data-gosx-on-contextmenu", "data-gosx-on-lostpointercapture",
	} {
		if !previewHasAttr(result.Preview, marker) {
			t.Errorf("preview lacks %s: %#v", marker, result.Preview)
		}
	}
}
