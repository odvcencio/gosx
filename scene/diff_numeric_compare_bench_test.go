package scene

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"
)

// This file compares the live walk with the reference walk on single values, so
// one run shows what each change buys. BenchmarkDiffSceneUnchangedMeshes, in
// diff_numeric_bench_test.go, measures the whole diff.

// benchSliceKind times three comparisons of two slices with n elements: the
// reference walk on two copies, the live walk on two copies, and the live walk on
// one slice twice.
func benchSliceKind[E comparable](b *testing.B, name string, n int, value func(int) E) {
	left := make([]E, n)
	for index := range left {
		left[index] = value(index)
	}
	right := slices.Clone(left)
	a, c, same := sliceFieldValue(left), sliceFieldValue(right), sliceFieldValue(left)
	b.Run(name+"/reflection", func(b *testing.B) {
		for b.Loop() {
			if !referenceProvenJSONEqual(a, c) {
				b.Fatal("want equal")
			}
		}
	})
	b.Run(name+"/typed", func(b *testing.B) {
		for b.Loop() {
			if !provenJSONEqual(a, c) {
				b.Fatal("want equal")
			}
		}
	})
	b.Run(name+"/shared", func(b *testing.B) {
		for b.Loop() {
			if !provenJSONEqual(a, same) {
				b.Fatal("want equal")
			}
		}
	})
}

// BenchmarkProvenSliceEqual compares two slices of 164,082 elements, the index
// count of a table scene, for each element type the IR uses. The shared case
// shows the shortcut where it applies: the integer and string kinds. A shared
// float slice still reads every element, because a NaN fails.
func BenchmarkProvenSliceEqual(b *testing.B) {
	const n = 164082
	benchSliceKind(b, "uint32", n, func(i int) uint32 { return uint32(i * 7) })
	benchSliceKind(b, "uint16", n, func(i int) uint16 { return uint16(i * 7) })
	benchSliceKind(b, "int32", n, func(i int) int32 { return int32(i * 7) })
	benchSliceKind(b, "int", n, func(i int) int { return i * 7 })
	benchSliceKind(b, "float64", n, func(i int) float64 { return float64(i) * 0.25 })
	benchSliceKind(b, "float32", n, func(i int) float32 { return float32(i) * 0.25 })
	benchSliceKind(b, "uint8", n, func(i int) uint8 { return uint8(i) })
	benchSliceKind(b, "string", n/8, func(i int) string { return fmt.Sprintf("#%06x", i) })
	benchSliceKind(b, "pairs", n/2, func(i int) [2]int { return [2]int{i, i + 1} })
}

// BenchmarkProvenLayoutEqual compares two decoded shader layouts of about 140
// nodes each, the shape the table materials carry, and a uniform table.
func BenchmarkProvenLayoutEqual(b *testing.B) {
	decode := func() map[string]any {
		var layout map[string]any
		if err := json.Unmarshal([]byte(benchShaderLayoutJSON), &layout); err != nil {
			b.Fatal(err)
		}
		return layout
	}
	uniforms := func() map[string]any {
		return map[string]any{
			"sunColor": []float64{1, 0.96, 0.9}, "coat": 0.18, "environmentRadiance": "gosx:environment:radiance",
			"variant": 1.0, "toneColor": []float64{0.888, 0.831, 0.716}, "specular": 0.38, "ambient": []float64{0.2, 0.22, 0.25},
			"cameraPos": []float64{0, 6, 7}, "sunDir": []float64{-0.4, -1, -0.3}, "bodyColor": []float64{0.888, 0.831, 0.716},
			"roughness": 0.46,
		}
	}
	for _, testCase := range []struct {
		name string
		make func() map[string]any
	}{{"layout", decode}, {"uniforms", uniforms}} {
		one, two := testCase.make(), testCase.make()
		x, y := reflect.ValueOf(&one).Elem(), reflect.ValueOf(&two).Elem()
		b.Run(testCase.name+"/reflection", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if !referenceProvenJSONEqual(x, y) {
					b.Fatal("want equal")
				}
			}
		})
		b.Run(testCase.name+"/typed", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if !provenJSONEqual(x, y) {
					b.Fatal("want equal")
				}
			}
		})
	}
}
