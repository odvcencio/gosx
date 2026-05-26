package surface

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	gosx "m31labs.dev/gosx"
)

// TestNewRenderer_Mount_DataAttrs verifies that NewRenderer("Graph").Mount(props)
// produces a gosx.Node whose attributes include the required data-gosx-engine-*
// attributes with the correct values.
func TestNewRenderer_Mount_DataAttrs(t *testing.T) {
	// Seed the registry with a known entry.
	const componentName = "Graph"
	const wasmURL = "/gosx/engines/Graph.abc12345.wasm"
	const hashVal = "abc12345"

	injectRegistryEntry(componentName, &registryEntry{
		wasmURL:      wasmURL,
		hash:         hashVal,
		capabilities: []string{"canvas", "pointer"},
		mountAttrs:   map[string]string{"tabindex": "0"},
	})

	type GraphProps struct {
		Title string `json:"title"`
		N     int    `json:"n"`
	}

	props := GraphProps{Title: "My Graph", N: 42}
	propsJSON, err := json.Marshal(props)
	if err != nil {
		t.Fatalf("marshal props: %v", err)
	}
	wantPropsB64 := base64.StdEncoding.EncodeToString(propsJSON)

	r := NewRenderer(componentName)
	node := r.Mount(props)

	// Render to HTML so we can inspect attributes.
	html := gosx.RenderHTML(node)

	checkAttr(t, html, `data-gosx-engine-component="Graph"`)
	checkAttr(t, html, `data-gosx-engine-wasm="/gosx/engines/Graph.abc12345.wasm"`)
	checkAttr(t, html, `data-gosx-engine-props="`+wantPropsB64+`"`)
	checkAttr(t, html, `data-gosx-engine-caps="canvas,pointer"`)
	checkAttr(t, html, `tabindex="0"`)
}

// TestNewRenderer_Mount_NoEntry returns a safe fallback node when the component
// has not been registered via Discover.
func TestNewRenderer_Mount_NoEntry(t *testing.T) {
	r := NewRenderer("UnknownComponent")
	node := r.Mount(nil)
	html := gosx.RenderHTML(node)

	// Must include the component name attr.
	checkAttr(t, html, `data-gosx-engine-component="UnknownComponent"`)
	// Must NOT contain a wasm attr (entry is absent).
	if strings.Contains(html, "data-gosx-engine-wasm") {
		t.Errorf("expected no data-gosx-engine-wasm attr for unregistered component, got: %s", html)
	}
	// Must include the missing-status attribute so the bootstrap can paint
	// a "surface unavailable" placeholder (spec §D, defect 4).
	checkAttr(t, html, `data-gosx-engine-status="missing"`)
}

// TestMountEmitsMissingStatusWhenWasmURLEmpty covers spec §D / defect 4: a
// registered component whose wasmURL is empty (build failed with no cached
// prior) must not leak data-gosx-engine-wasm="" into the canvas attrs.
// Instead, the bootstrap sees data-gosx-engine-status="missing" and can
// degrade gracefully.
func TestMountEmitsMissingStatusWhenWasmURLEmpty(t *testing.T) {
	const componentName = "GraphMissingWasm"
	injectRegistryEntry(componentName, &registryEntry{
		wasmURL:      "",
		hash:         "",
		capabilities: []string{"canvas"},
	})

	r := NewRenderer(componentName)
	html := gosx.RenderHTML(r.Mount(nil))

	if strings.Contains(html, `data-gosx-engine-wasm=""`) {
		t.Errorf("empty wasmURL leaked into output: %s", html)
	}
	if strings.Contains(html, `data-gosx-engine-wasm=`) {
		t.Errorf("data-gosx-engine-wasm attr should be omitted when URL empty, got: %s", html)
	}
	if strings.Contains(html, `data-gosx-engine-props=`) {
		t.Errorf("data-gosx-engine-props should be omitted alongside missing wasm, got: %s", html)
	}
	checkAttr(t, html, `data-gosx-engine-status="missing"`)
	checkAttr(t, html, `data-gosx-engine-component="`+componentName+`"`)
}

// TestMountEmitsStaleAttrWhenEntryStale covers spec §B / defect 2: when the
// registry entry is stale (last build failed but we still have a usable
// cached WASM), the canvas must carry data-gosx-engine-stale="1" so the
// bootstrap can show a corner badge while mounting.
func TestMountEmitsStaleAttrWhenEntryStale(t *testing.T) {
	const componentName = "GraphStale"
	injectRegistryEntry(componentName, &registryEntry{
		wasmURL: "/gosx/engines/GraphStale.cafef00d.wasm",
		hash:    "cafef00d",
		stale:   true,
	})
	r := NewRenderer(componentName)
	html := gosx.RenderHTML(r.Mount(nil))
	checkAttr(t, html, `data-gosx-engine-stale="1"`)
	checkAttr(t, html, `data-gosx-engine-wasm="/gosx/engines/GraphStale.cafef00d.wasm"`)
}

// TestPropsRoundTrip verifies that Context.PropsInto correctly unmarshals
// the JSON bytes stored at construction.
func TestPropsRoundTrip(t *testing.T) {
	type MyProps struct {
		Name  string  `json:"name"`
		Value float64 `json:"value"`
	}

	want := MyProps{Name: "test", Value: 3.14}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	ctx := newContext(raw)

	// Props() should return the original JSON bytes.
	gotRaw := ctx.Props()
	if string(gotRaw) != string(raw) {
		t.Errorf("Props() = %q, want %q", gotRaw, raw)
	}

	// PropsInto should decode correctly.
	var got MyProps
	if err := ctx.PropsInto(&got); err != nil {
		t.Fatalf("PropsInto: %v", err)
	}
	if got.Name != want.Name || got.Value != want.Value {
		t.Errorf("PropsInto got %+v, want %+v", got, want)
	}
}

// TestContext_Done closes when Close is called.
func TestContext_Done(t *testing.T) {
	ctx := newContext(nil)
	select {
	case <-ctx.Done():
		t.Fatal("Done channel should not be closed yet")
	default:
	}

	ctx.Close()

	select {
	case <-ctx.Done():
		// expected
	default:
		t.Fatal("Done channel should be closed after Close()")
	}
}

// TestWrapMount_JSON verifies that WrapMount works in the JSON-fallback path
// (when called with a json.RawMessage instead of a typed *mountPayload).
func TestWrapMount_JSON(t *testing.T) {
	called := false
	var gotProps []byte

	fn := WrapMount(func(ctx *Context, c *Canvas) {
		called = true
		gotProps = ctx.Props()
	})

	raw := json.RawMessage(`{"x":1}`)
	fn(raw)

	if !called {
		t.Fatal("WrapMount did not call the handler")
	}
	if string(gotProps) != `{"x":1}` {
		t.Errorf("WrapMount: Props() = %q, want %q", gotProps, `{"x":1}`)
	}
}

// TestWrapPointer decodes a float64 payload into a PointerEvent.
func TestWrapPointer(t *testing.T) {
	var got PointerEvent
	fn := WrapPointer(func(ctx *Context, c *Canvas, ev PointerEvent) {
		got = ev
	})

	// Build a typed payload as the runtime would.
	ctx := newContext(nil)
	c := newNoopCanvas()
	payload := NewPointerPayload(ctx, c, []float64{10, 20, 1, 3, 2})
	fn(payload)

	if got.X != 10 || got.Y != 20 || got.Button != 1 || got.Buttons != 3 || got.Modifier != ModCtrl {
		t.Errorf("WrapPointer got %+v, want X=10 Y=20 Button=1 Buttons=3 Modifier=ModCtrl", got)
	}
}

// TestWrapWheel decodes a float64 payload into a WheelEvent.
func TestWrapWheel(t *testing.T) {
	var got WheelEvent
	fn := WrapWheel(func(ctx *Context, c *Canvas, ev WheelEvent) {
		got = ev
	})

	ctx := newContext(nil)
	c := newNoopCanvas()
	payload := NewWheelPayload(ctx, c, []float64{5, 6, -10, 20, 1})
	fn(payload)

	if got.X != 5 || got.Y != 6 || got.DeltaX != -10 || got.DeltaY != 20 || got.Modifier != ModShift {
		t.Errorf("WrapWheel got %+v", got)
	}
}

// TestWrapKey decodes a key event payload.
func TestWrapKey(t *testing.T) {
	var got KeyEvent
	fn := WrapKey(func(ctx *Context, c *Canvas, ev KeyEvent) {
		got = ev
	})

	ctx := newContext(nil)
	c := newNoopCanvas()
	payload := NewKeyPayload(ctx, c, []float64{4}, "ArrowUp", "ArrowUp")
	fn(payload)

	if got.Key != "ArrowUp" || got.Code != "ArrowUp" || got.Modifier != ModAlt {
		t.Errorf("WrapKey got %+v, want Key=ArrowUp Code=ArrowUp Modifier=ModAlt", got)
	}
}

// TestEncodeProps_Base64 verifies that encodeProps produces valid base64.
func TestEncodeProps_Base64(t *testing.T) {
	type P struct {
		A int `json:"a"`
	}
	b64 := encodeProps(P{A: 7})
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatalf("encodeProps produced invalid base64: %v", err)
	}
	var got P
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal base64: %v", err)
	}
	if got.A != 7 {
		t.Errorf("got A=%d, want 7", got.A)
	}
}

// checkAttr asserts that html contains the given substring.
func checkAttr(t *testing.T, html, want string) {
	t.Helper()
	if !strings.Contains(html, want) {
		t.Errorf("expected HTML to contain %q\nfull HTML: %s", want, html)
	}
}
