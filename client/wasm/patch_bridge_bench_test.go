//go:build js && wasm && !gosx_tiny_islands_only

// Measurements for the patch path across the WASM boundary.
//
// applyPatchedResult marshals []vm.PatchOp to JSON, hands the string to
// syscall/js, and lets the browser run JSON.parse. Three costs hide in that
// sentence, and only measurement says which one matters:
//
//  1. the Go-side encode,
//  2. the Go string to JS string copy (UTF-8 to UTF-16),
//  3. the JS-side JSON.parse.
//
// The benchmarks below separate all three, and compare the JSON route against a
// compact binary route that crosses as a Uint8Array. encodeBinaryPatches exists
// only to size that alternative; nothing in production calls it.
package main

import (
	"encoding/binary"
	"strconv"
	"syscall/js"
	"testing"

	"m31labs.dev/gosx/client/bridge"
	"m31labs.dev/gosx/client/vm"
)

// benchListBuildPatches mirrors what reconcile.go emits for a fresh list of
// `rows` rows, each holding `cells` cells with one text child.
func benchListBuildPatches(rows, cells int) []vm.PatchOp {
	ops := make([]vm.PatchOp, 0, rows*(2+cells*2))
	for r := 0; r < rows; r++ {
		rowPath := "0/" + strconv.Itoa(r)
		ops = append(ops,
			vm.PatchOp{Kind: vm.PatchCreateElement, Path: "0", Tag: "li", Children: []int{r}},
			vm.PatchOp{Kind: vm.PatchSetAttr, Path: rowPath, AttrName: "data-row", Text: strconv.Itoa(r)},
		)
		for c := 0; c < cells; c++ {
			cellPath := rowPath + "/" + strconv.Itoa(c)
			ops = append(ops,
				vm.PatchOp{Kind: vm.PatchCreateElement, Path: rowPath, Tag: "span", Children: []int{c}},
				vm.PatchOp{Kind: vm.PatchCreateText, Path: cellPath, Text: "r" + strconv.Itoa(r) + "c" + strconv.Itoa(c), Children: []int{0}},
			)
		}
	}
	return ops
}

// benchCounterPatches is the one-op batch a counter increment produces. This is
// the batch that actually runs on every user event.
func benchCounterPatches() []vm.PatchOp {
	return []vm.PatchOp{{Kind: vm.PatchSetText, Path: "1/0", Text: "42"}}
}

// installBenchSinks defines the JS receivers the cross-boundary benchmarks use.
func installBenchSinks(b *testing.B) {
	b.Helper()
	js.Global().Call("eval", `
	  globalThis.__benchTake = function (text) { globalThis.__benchLast = text.length; };
	  globalThis.__benchParse = function (text) {
	    var ops = JSON.parse(text);
	    globalThis.__benchLast = ops.length;
	  };
	  globalThis.__benchTakeBytes = function (bytes) { globalThis.__benchLast = bytes.length; };
	`)
}

// --- Go-side encode only ---

func BenchmarkMarshalPatchesCounter(b *testing.B) {
	ops := benchCounterPatches()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := bridge.MarshalPatches(ops); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalPatchesListBuild(b *testing.B) {
	ops := benchListBuildPatches(100, 3)
	out, err := bridge.MarshalPatches(ops)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(len(out)), "json_bytes")
	b.ReportMetric(float64(len(ops)), "ops")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := bridge.MarshalPatches(ops); err != nil {
			b.Fatal(err)
		}
	}
}

// --- boundary crossing only, pre-encoded payload ---

func BenchmarkBoundaryStringCounter(b *testing.B) {
	installBenchSinks(b)
	payload, err := bridge.MarshalPatches(benchCounterPatches())
	if err != nil {
		b.Fatal(err)
	}
	sink := js.Global().Get("__benchTake")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink.Invoke(payload)
	}
}

func BenchmarkBoundaryStringListBuild(b *testing.B) {
	installBenchSinks(b)
	payload, err := bridge.MarshalPatches(benchListBuildPatches(100, 3))
	if err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(len(payload)), "json_bytes")
	sink := js.Global().Get("__benchTake")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink.Invoke(payload)
	}
}

func BenchmarkBoundaryStringPlusParseListBuild(b *testing.B) {
	installBenchSinks(b)
	payload, err := bridge.MarshalPatches(benchListBuildPatches(100, 3))
	if err != nil {
		b.Fatal(err)
	}
	sink := js.Global().Get("__benchParse")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sink.Invoke(payload)
	}
}

// --- full production path ---

func BenchmarkFullPatchPathCounter(b *testing.B) {
	installBenchSinks(b)
	ops := benchCounterPatches()
	sink := js.Global().Get("__benchParse")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload, err := bridge.MarshalPatches(ops)
		if err != nil {
			b.Fatal(err)
		}
		sink.Invoke(payload)
	}
}

func BenchmarkFullPatchPathListBuild(b *testing.B) {
	installBenchSinks(b)
	ops := benchListBuildPatches(100, 3)
	sink := js.Global().Get("__benchParse")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		payload, err := bridge.MarshalPatches(ops)
		if err != nil {
			b.Fatal(err)
		}
		sink.Invoke(payload)
	}
}

// --- compact binary alternative, for sizing only ---

// encodeBinaryPatches writes a length-prefixed binary form of the op list. It
// exists to size the headroom a binary wire format would buy; no production
// code calls it.
func encodeBinaryPatches(ops []vm.PatchOp) []byte {
	out := make([]byte, 0, 16*len(ops))
	out = binary.LittleEndian.AppendUint32(out, uint32(len(ops)))
	for i := range ops {
		op := &ops[i]
		out = append(out, byte(op.Kind))
		out = appendBinaryString(out, op.Path)
		out = appendBinaryString(out, op.Tag)
		out = appendBinaryString(out, op.Text)
		out = appendBinaryString(out, op.AttrName)
		out = append(out, byte(len(op.Children)))
		for _, child := range op.Children {
			out = binary.LittleEndian.AppendUint16(out, uint16(child))
		}
	}
	return out
}

func appendBinaryString(out []byte, s string) []byte {
	out = binary.LittleEndian.AppendUint16(out, uint16(len(s)))
	return append(out, s...)
}

func BenchmarkEncodeBinaryPatchesListBuild(b *testing.B) {
	ops := benchListBuildPatches(100, 3)
	b.ReportMetric(float64(len(encodeBinaryPatches(ops))), "binary_bytes")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encodeBinaryPatches(ops)
	}
}

func BenchmarkBoundaryBytesListBuild(b *testing.B) {
	installBenchSinks(b)
	payload := encodeBinaryPatches(benchListBuildPatches(100, 3))
	buffer := js.Global().Get("Uint8Array").New(len(payload))
	sink := js.Global().Get("__benchTakeBytes")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		js.CopyBytesToJS(buffer, payload)
		sink.Invoke(buffer)
	}
}
