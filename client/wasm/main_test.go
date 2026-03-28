//go:build js && wasm

package main

import (
	"encoding/json"
	"strings"
	"syscall/js"
	"testing"

	"github.com/odvcencio/gosx"
	"github.com/odvcencio/gosx/client/bridge"
	"github.com/odvcencio/gosx/client/vm"
	rootengine "github.com/odvcencio/gosx/engine"
	"github.com/odvcencio/gosx/ir"
	"github.com/odvcencio/gosx/island/program"
	"github.com/rivo/uniseg"
)

func compileIslandProgram(t *testing.T, source string) *program.Program {
	t.Helper()

	irProg, err := gosx.Compile([]byte(source))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	for i, comp := range irProg.Components {
		if !comp.IsIsland {
			continue
		}
		prog, err := ir.LowerIsland(irProg, i)
		if err != nil {
			t.Fatalf("lower island: %v", err)
		}
		return prog
	}

	t.Fatal("no island component found")
	return nil
}

func setGlobalFunc(t *testing.T, name string, fn func(this js.Value, args []js.Value) any) {
	t.Helper()

	prev := js.Global().Get(name)
	wrapped := js.FuncOf(fn)
	js.Global().Set(name, wrapped)
	t.Cleanup(func() {
		wrapped.Release()
		js.Global().Set(name, prev)
	})
}

func setGlobalValue(t *testing.T, name string, value any) {
	t.Helper()

	prev := js.Global().Get(name)
	js.Global().Set(name, value)
	t.Cleanup(func() {
		js.Global().Set(name, prev)
	})
}

func uint8ArrayFromBytes(b []byte) js.Value {
	arr := js.Global().Get("Uint8Array").New(len(b))
	js.CopyBytesToJS(arr, b)
	return arr
}

func TestRuntimeHydrateAndDispatchCompiledGSX(t *testing.T) {
	prog := compileIslandProgram(t, `package main

//gosx:island
func Counter() Node {
	count := signal.New(0)
	increment := func() { count.Set(count.Get() + 1) }
	return <div class="counter">
		<span>{count.Get()}</span>
		<button onClick={increment}>+</button>
	</div>
}`)

	data, err := program.EncodeJSON(prog)
	if err != nil {
		t.Fatalf("encode json: %v", err)
	}

	var patchIslandID string
	var patches []vm.PatchOp
	setGlobalFunc(t, "__gosx_apply_patches", func(this js.Value, args []js.Value) any {
		patchIslandID = args[0].String()
		if err := json.Unmarshal([]byte(args[1].String()), &patches); err != nil {
			t.Fatalf("unmarshal patches: %v", err)
		}
		return nil
	})
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	registerRuntime(bridge.New())

	hydrateRet := js.Global().Get("__gosx_hydrate").Invoke("counter-0", prog.Name, `{}`, string(data), "json")
	if !hydrateRet.IsNull() {
		t.Fatalf("expected null hydrate result, got %q", hydrateRet.String())
	}

	actionRet := js.Global().Get("__gosx_action").Invoke("counter-0", "increment", `{}`)
	if got := actionRet.Int(); got <= 0 {
		t.Fatalf("expected positive patch count, got %d", got)
	}

	if patchIslandID != "counter-0" {
		t.Fatalf("expected patches for counter-0, got %q", patchIslandID)
	}
	if len(patches) == 0 {
		t.Fatal("expected patches from wasm dispatch")
	}

	foundTextPatch := false
	for _, patch := range patches {
		if patch.Kind == vm.PatchSetText && patch.Text == "1" {
			foundTextPatch = true
		}
	}
	if !foundTextPatch {
		t.Fatalf("expected text patch setting counter to 1, got %#v", patches)
	}
}

func TestRuntimeBinaryHydrateDisposeAndErrors(t *testing.T) {
	prog := compileIslandProgram(t, `package main

//gosx:island
func Toggle() Node {
	visible := signal.New(false)
	toggle := func() { visible.Set(!visible.Get()) }
	return <div>
		<p>{visible.Get()}</p>
		<button onClick={toggle}>Toggle</button>
	</div>
}`)

	data, err := program.EncodeBinary(prog)
	if err != nil {
		t.Fatalf("encode binary: %v", err)
	}

	setGlobalFunc(t, "__gosx_apply_patches", func(this js.Value, args []js.Value) any { return nil })
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	registerRuntime(bridge.New())

	hydrateRet := js.Global().Get("__gosx_hydrate").Invoke("toggle-0", prog.Name, `{}`, uint8ArrayFromBytes(data), "bin")
	if !hydrateRet.IsNull() {
		t.Fatalf("expected null hydrate result, got %q", hydrateRet.String())
	}

	js.Global().Get("__gosx_dispose").Invoke("toggle-0")

	actionRet := js.Global().Get("__gosx_action").Invoke("toggle-0", "toggle", `{}`)
	if !strings.Contains(actionRet.String(), `error: island "toggle-0" not found`) {
		t.Fatalf("expected disposed island error, got %q", actionRet.String())
	}
}

func TestRuntimeSetSharedSignalExport(t *testing.T) {
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	b := bridge.New()
	registerRuntime(b)

	ret := js.Global().Get("__gosx_set_shared_signal").Invoke("$presence", `{"count":3}`)
	if !ret.IsNull() {
		t.Fatalf("expected null result, got %q", ret.String())
	}

	val, ok := b.GetStore().Get("$presence")
	if !ok {
		t.Fatal("expected shared signal to be set")
	}
	if val.Fields["count"].Num != 3 {
		t.Fatalf("expected count 3, got %v", val.Fields["count"].Num)
	}
}

func TestRuntimeTextLayoutExport(t *testing.T) {
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())
	setGlobalFunc(t, "__gosx_measure_text_batch", func(this js.Value, args []js.Value) any {
		if len(args) < 2 {
			t.Fatalf("expected 2 args, got %d", len(args))
		}

		var texts []string
		if err := json.Unmarshal([]byte(args[1].String()), &texts); err != nil {
			t.Fatalf("unmarshal texts: %v", err)
		}

		widths := make([]float64, len(texts))
		for i, text := range texts {
			graphemes := uniseg.NewGraphemes(text)
			for graphemes.Next() {
				widths[i]++
			}
		}

		data, err := json.Marshal(widths)
		if err != nil {
			t.Fatalf("marshal widths: %v", err)
		}
		return js.ValueOf(string(data))
	})
	setGlobalValue(t, "__gosx_segment_words", js.Global().Get("Function").New("text", `
		return JSON.stringify(Array.from(new Intl.Segmenter(undefined, { granularity: "word" }).segment(text), function(entry) {
			return entry.segment;
		}));
	`))

	registerRuntime(bridge.New())

	ret := js.Global().Get("__gosx_text_layout").Invoke("hello world from gosx", "16px serif", 11, "normal", 2)
	if ret.Type() != js.TypeObject {
		t.Fatalf("expected object result, got %v", ret.Type())
	}
	if ret.Get("lineCount").Int() != 2 {
		t.Fatalf("expected 2 lines, got %d", ret.Get("lineCount").Int())
	}
	if ret.Get("height").Float() != 4 {
		t.Fatalf("expected height 4, got %v", ret.Get("height").Float())
	}
	if ret.Get("byteLen").Int() != len("hello world from gosx") {
		t.Fatalf("expected byteLen %d, got %d", len("hello world from gosx"), ret.Get("byteLen").Int())
	}
	if ret.Get("runeCount").Int() != len([]rune("hello world from gosx")) {
		t.Fatalf("expected runeCount %d, got %d", len([]rune("hello world from gosx")), ret.Get("runeCount").Int())
	}

	lines := ret.Get("lines")
	if lines.Length() != 2 {
		t.Fatalf("expected 2 line entries, got %d", lines.Length())
	}
	if got := lines.Index(0).Get("text").String(); got != "hello world" {
		t.Fatalf("line 0 text: got %q", got)
	}
	if got := lines.Index(1).Get("text").String(); got != "from gosx" {
		t.Fatalf("line 1 text: got %q", got)
	}
	if got := lines.Index(0).Get("runeStart").Int(); got != 0 {
		t.Fatalf("line 0 runeStart: got %d", got)
	}
	if got := lines.Index(0).Get("runeEnd").Int(); got != 12 {
		t.Fatalf("line 0 runeEnd: got %d", got)
	}
	if got := lines.Index(1).Get("runeStart").Int(); got != 12 {
		t.Fatalf("line 1 runeStart: got %d", got)
	}
	if got := lines.Index(1).Get("runeEnd").Int(); got != 21 {
		t.Fatalf("line 1 runeEnd: got %d", got)
	}

	emojiRet := js.Global().Get("__gosx_text_layout").Invoke("👨‍👩‍👧‍👦a", "16px serif", 1, "normal", 1)
	emojiLines := emojiRet.Get("lines")
	if emojiLines.Length() != 2 {
		t.Fatalf("expected 2 emoji line entries, got %d", emojiLines.Length())
	}
	if got := emojiLines.Index(0).Get("text").String(); got != "👨‍👩‍👧‍👦" {
		t.Fatalf("expected intact emoji grapheme, got %q", got)
	}

	softRet := js.Global().Get("__gosx_text_layout").Invoke("ab\u00adcd", "16px serif", 3, "normal", 1)
	softLines := softRet.Get("lines")
	if softLines.Length() != 2 {
		t.Fatalf("expected 2 soft-hyphen line entries, got %d", softLines.Length())
	}
	if got := softLines.Index(0).Get("text").String(); got != "ab-" {
		t.Fatalf("expected discretionary hyphen line, got %q", got)
	}

	wordRet := js.Global().Get("__gosx_text_layout").Invoke("hello,world", "16px serif", 7, "normal", 1)
	wordLines := wordRet.Get("lines")
	if wordLines.Length() != 2 {
		t.Fatalf("expected 2 punctuation-boundary line entries, got %d", wordLines.Length())
	}
	if got := wordLines.Index(0).Get("text").String(); got != "hello," {
		t.Fatalf("expected semantic run break at word boundary, got %q", got)
	}

	thaiRet := js.Global().Get("__gosx_text_layout").Invoke("สวัสดีครับโลก", "16px serif", 5, "normal", 1)
	thaiLines := thaiRet.Get("lines")
	if thaiLines.Length() != 3 {
		t.Fatalf("expected 3 Thai line entries, got %d", thaiLines.Length())
	}
	if got := thaiLines.Index(0).Get("text").String(); got != "สวัสดี" {
		t.Fatalf("expected Thai word boundary on line 0, got %q", got)
	}

	tabRet := js.Global().Get("__gosx_text_layout").Invoke("a\tb", "16px serif", 99, "pre-wrap", 1)
	if got := tabRet.Get("maxLineWidth").Float(); got != 9 {
		t.Fatalf("expected tab-stop width 9, got %v", got)
	}

	openRet := js.Global().Get("__gosx_text_layout").Invoke("(a", "16px serif", 1, "normal", 1)
	openLines := openRet.Get("lines")
	if openLines.Length() != 1 {
		t.Fatalf("expected 1 opening-punctuation line entry, got %d", openLines.Length())
	}
	if got := openLines.Index(0).Get("text").String(); got != "(a" {
		t.Fatalf("expected opening punctuation to stay with following glyph, got %q", got)
	}
}

func TestRuntimeSharedSignalsPatchOtherHydratedIslands(t *testing.T) {
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	type patchCall struct {
		islandID string
		patches  []vm.PatchOp
	}

	var calls []patchCall
	setGlobalFunc(t, "__gosx_apply_patches", func(this js.Value, args []js.Value) any {
		var patches []vm.PatchOp
		if err := json.Unmarshal([]byte(args[1].String()), &patches); err != nil {
			t.Fatalf("unmarshal patches: %v", err)
		}
		calls = append(calls, patchCall{
			islandID: args[0].String(),
			patches:  patches,
		})
		return nil
	})

	registerRuntime(bridge.New())

	writer := &program.Program{
		Name: "ThemeWriter",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}},
			{Kind: program.NodeElement, Tag: "button", Attrs: []program.Attr{{Kind: program.AttrEvent, Name: "click", Event: "switchTheme"}}, Children: []program.NodeID{2}},
			{Kind: program.NodeText, Text: "Switch"},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
			{Op: program.OpLitInt, Value: "1", Type: program.TypeInt},
			{Op: program.OpSignalSet, Operands: []program.ExprID{1}, Value: "$theme", Type: program.TypeInt},
		},
		Signals: []program.SignalDef{
			{Name: "$theme", Type: program.TypeInt, Init: 0},
		},
		Handlers: []program.Handler{
			{Name: "switchTheme", Body: []program.ExprID{2}},
		},
		StaticMask: []bool{false, false, true},
	}

	reader := &program.Program{
		Name: "ThemeReader",
		Nodes: []program.Node{
			{Kind: program.NodeElement, Tag: "div", Children: []program.NodeID{1}},
			{Kind: program.NodeExpr, Expr: 5},
		},
		Root: 0,
		Exprs: []program.Expr{
			{Op: program.OpLitInt, Value: "0", Type: program.TypeInt},
			{Op: program.OpSignalGet, Value: "$theme", Type: program.TypeInt},
			{Op: program.OpEq, Operands: []program.ExprID{1, 0}, Type: program.TypeBool},
			{Op: program.OpLitString, Value: "LiveJournal", Type: program.TypeString},
			{Op: program.OpLitString, Value: "Xanga", Type: program.TypeString},
			{Op: program.OpCond, Operands: []program.ExprID{2, 3, 4}, Type: program.TypeString},
		},
		Signals: []program.SignalDef{
			{Name: "$theme", Type: program.TypeInt, Init: 0},
		},
		StaticMask: []bool{false, false},
	}

	writerJSON, err := program.EncodeJSON(writer)
	if err != nil {
		t.Fatalf("encode writer: %v", err)
	}
	readerJSON, err := program.EncodeJSON(reader)
	if err != nil {
		t.Fatalf("encode reader: %v", err)
	}

	if ret := js.Global().Get("__gosx_hydrate").Invoke("writer-0", writer.Name, `{}`, string(writerJSON), "json"); !ret.IsNull() {
		t.Fatalf("hydrate writer: %q", ret.String())
	}
	if ret := js.Global().Get("__gosx_hydrate").Invoke("reader-0", reader.Name, `{}`, string(readerJSON), "json"); !ret.IsNull() {
		t.Fatalf("hydrate reader: %q", ret.String())
	}

	calls = nil
	ret := js.Global().Get("__gosx_action").Invoke("writer-0", "switchTheme", `{}`)
	if got := ret.String(); strings.HasPrefix(got, "error:") {
		t.Fatalf("dispatch returned error: %q", got)
	}

	var foundReader bool
	for _, call := range calls {
		if call.islandID != "reader-0" {
			continue
		}
		for _, patch := range call.patches {
			if patch.Kind == vm.PatchSetText && patch.Text == "Xanga" {
				foundReader = true
				break
			}
		}
	}
	if !foundReader {
		t.Fatalf("expected cross-island patch for reader-0, got %#v", calls)
	}
}

func TestRuntimeSetInputBatchExport(t *testing.T) {
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	b := bridge.New()
	registerRuntime(b)

	payload := js.Global().Get("JSON").Call("parse", `{
		"$input.pointer": {"x": 18, "y": -4.5},
		"$input.key": {"space": true}
	}`)
	ret := js.Global().Get("__gosx_set_input_batch").Invoke(payload)
	if !ret.IsNull() {
		t.Fatalf("expected null result, got %q", ret.String())
	}

	pointer, ok := b.GetStore().Get("$input.pointer")
	if !ok {
		t.Fatal("expected pointer signal to be set")
	}
	if got := pointer.Fields["x"].Num; got != 18 {
		t.Fatalf("expected x 18, got %v", got)
	}
	if got := pointer.Fields["y"].Num; got != -4.5 {
		t.Fatalf("expected y -4.5, got %v", got)
	}

	keyboard, ok := b.GetStore().Get("$input.key")
	if !ok {
		t.Fatal("expected key signal to be set")
	}
	if !keyboard.Fields["space"].Bool {
		t.Fatalf("expected space=true, got %#v", keyboard.Fields["space"])
	}
}

func TestRuntimeHydrateTickAndDisposeEngine(t *testing.T) {
	setGlobalValue(t, "__gosx_runtime_ready", js.Undefined())

	b := bridge.New()
	registerRuntime(b)

	prog := &rootengine.Program{
		Name: "GeometryZoo",
		Nodes: []rootengine.Node{
			{
				Kind:     "mesh",
				Geometry: "box",
				Material: "flat",
				Props: map[string]program.ExprID{
					"x":     0,
					"color": 1,
				},
			},
		},
		Exprs: []program.Expr{
			{Op: program.OpSignalGet, Value: "$scene.x", Type: program.TypeFloat},
			{Op: program.OpSignalGet, Value: "$scene.color", Type: program.TypeString},
			{Op: program.OpLitFloat, Value: "0", Type: program.TypeFloat},
			{Op: program.OpLitString, Value: "#8de1ff", Type: program.TypeString},
		},
		Signals: []program.SignalDef{
			{Name: "$scene.x", Type: program.TypeFloat, Init: 2},
			{Name: "$scene.color", Type: program.TypeString, Init: 3},
		},
	}

	data, err := rootengine.EncodeProgramJSON(prog)
	if err != nil {
		t.Fatalf("encode engine program: %v", err)
	}

	hydrateRet := js.Global().Get("__gosx_hydrate_engine").Invoke("engine-0", prog.Name, `{}`, string(data), "json")
	if got := hydrateRet.String(); !strings.Contains(got, `"kind":0`) {
		t.Fatalf("expected create-object command result, got %q", got)
	}
	if b.EngineCount() != 1 {
		t.Fatalf("expected 1 engine after hydrate, got %d", b.EngineCount())
	}

	batchRet := js.Global().Get("__gosx_set_input_batch").Invoke(`{"$scene.x":4.5,"$scene.color":"#ff8f6b"}`)
	if !batchRet.IsNull() {
		t.Fatalf("expected null input batch result, got %q", batchRet.String())
	}

	tickRet := js.Global().Get("__gosx_tick_engine").Invoke("engine-0")
	got := tickRet.String()
	if !strings.Contains(got, `"kind":2`) || !strings.Contains(got, `"kind":3`) {
		t.Fatalf("expected transform and material commands, got %q", got)
	}

	disposeRet := js.Global().Get("__gosx_engine_dispose").Invoke("engine-0")
	if !disposeRet.IsNull() {
		t.Fatalf("expected null dispose result, got %q", disposeRet.String())
	}
	if b.EngineCount() != 0 {
		t.Fatalf("expected 0 engines after dispose, got %d", b.EngineCount())
	}
}
