package bridge

import (
	"encoding/json"
	"math/rand"
	"strconv"
	"strings"
	"testing"

	"m31labs.dev/gosx/client/vm"
)

// TestMarshalPatchesMatchesEncodingJSON pins the hand-rolled encoder to
// encoding/json byte for byte.
//
// patch.js, runtime.test.js and recorded fixtures all read this exact text, so a
// single escaping difference is a wire break. Any change to appendPatchesJSON
// must keep this test green.
func TestMarshalPatchesMatchesEncodingJSON(t *testing.T) {
	cases := map[string][]vm.PatchOp{
		"nil":   nil,
		"empty": {},
		"set text": {
			{Kind: vm.PatchSetText, Path: "1/0", Text: "42"},
		},
		"empty path and empty text": {
			{Kind: vm.PatchSetText, Path: "", Text: ""},
		},
		"every field": {
			{
				Kind:     vm.PatchCreateElement,
				Path:     "0/12/3",
				Tag:      "li",
				Text:     "row",
				AttrName: "data-row",
				Children: []int{0, 7, 42},
			},
		},
		"html in text": {
			{Kind: vm.PatchSetText, Path: "0", Text: `<strong class="x">a & b</strong>`},
		},
		"quotes and backslashes": {
			{Kind: vm.PatchSetAttr, Path: "0", AttrName: "title", Text: `he said "hi\there"`},
		},
		"control characters": {
			{Kind: vm.PatchSetText, Path: "0", Text: "a\tb\nc\rd\x00e\x1ff\x7fg"},
		},
		"bell and form feed": {
			{Kind: vm.PatchSetText, Path: "0", Text: "\a\b\f\v"},
		},
		"multibyte and emoji": {
			{Kind: vm.PatchSetText, Path: "0", Text: "héllo wörld — 日本語 🎉"},
		},
		"line and paragraph separators": {
			{Kind: vm.PatchSetText, Path: "0", Text: "a b c"},
		},
		"invalid utf8": {
			{Kind: vm.PatchSetText, Path: "0", Text: "ok\xff\xfe\x80bad"},
		},
		"lone surrogate bytes": {
			{Kind: vm.PatchSetText, Path: "0", Text: "\xed\xa0\x80"},
		},
		"negative and large children": {
			{Kind: vm.PatchReorder, Path: "0", Children: []int{-1, 0, 65535, 1000000}},
		},
		"empty children slice stays omitted": {
			{Kind: vm.PatchReorder, Path: "0", Children: []int{}},
		},
		"high kind value": {
			{Kind: vm.PatchKind(200), Path: "0"},
		},
		"multi op batch": {
			{Kind: vm.PatchRemoveElement, Path: "0/3"},
			{Kind: vm.PatchCreateText, Path: "0", Text: "x", Children: []int{3}},
			{Kind: vm.PatchRemoveAttr, Path: "0/3", AttrName: "hidden"},
			{Kind: vm.PatchSetValue, Path: "0/4", AttrName: "value", Text: "typed"},
		},
	}

	for name, ops := range cases {
		t.Run(name, func(t *testing.T) {
			want, err := json.Marshal(ops)
			if err != nil {
				t.Fatalf("encoding/json failed: %v", err)
			}
			got, err := MarshalPatches(ops)
			if err != nil {
				t.Fatalf("MarshalPatches failed: %v", err)
			}
			if got != string(want) {
				t.Errorf("encoder diverged\n got: %s\nwant: %s", got, want)
			}
		})
	}
}

// TestMarshalPatchesMatchesEncodingJSONRandomized runs the same equality check
// over a randomized corpus, so escaping paths the table misses still get hit.
func TestMarshalPatchesMatchesEncodingJSONRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(20260726))
	for round := 0; round < 3000; round++ {
		ops := make([]vm.PatchOp, rng.Intn(4))
		for i := range ops {
			ops[i] = vm.PatchOp{
				Kind:     vm.PatchKind(rng.Intn(12)),
				Path:     randomPatchText(rng),
				Tag:      randomPatchText(rng),
				Text:     randomPatchText(rng),
				AttrName: randomPatchText(rng),
			}
			if n := rng.Intn(4); n > 0 {
				ops[i].Children = make([]int, n)
				for j := range ops[i].Children {
					ops[i].Children[j] = rng.Intn(2000) - 500
				}
			}
		}
		want, err := json.Marshal(ops)
		if err != nil {
			t.Fatalf("round %d: encoding/json failed: %v", round, err)
		}
		got, err := MarshalPatches(ops)
		if err != nil {
			t.Fatalf("round %d: MarshalPatches failed: %v", round, err)
		}
		if got != string(want) {
			t.Fatalf("round %d: encoder diverged\n got: %s\nwant: %s", round, got, want)
		}
	}
}

// randomPatchText builds a short string from a pool that spans safe ASCII,
// escape-forcing bytes, multi-byte runes and invalid UTF-8.
func randomPatchText(rng *rand.Rand) string {
	pool := []string{
		"", "a", "0/1", "div", "data-x", " ", "\t", "\n", "\r", "\x00", "\x0b",
		"\x1f", "\x7f", `"`, `\`, "<", ">", "&", "é", "日", "🎉", " ",
		" ", "\xff", "\x80", "\xed\xa0\x80", "\xc3", "ok",
	}
	var b strings.Builder
	for n := rng.Intn(6); n > 0; n-- {
		b.WriteString(pool[rng.Intn(len(pool))])
	}
	return b.String()
}

// --- benchmarks ---

func benchPatchListBuild(rows, cells int) []vm.PatchOp {
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

func BenchmarkMarshalPatchesCounter(b *testing.B) {
	ops := []vm.PatchOp{{Kind: vm.PatchSetText, Path: "1/0", Text: "42"}}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalPatches(ops); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalPatchesListBuild(b *testing.B) {
	ops := benchPatchListBuild(100, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := MarshalPatches(ops); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalPatchesListBuildEncodingJSON(b *testing.B) {
	ops := benchPatchListBuild(100, 3)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := json.Marshal(ops); err != nil {
			b.Fatal(err)
		}
	}
}
