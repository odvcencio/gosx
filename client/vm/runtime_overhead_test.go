package vm

import (
	"testing"

	"m31labs.dev/gosx/island/program"
)

func TestPropsNestedViewsRemainIndependent(t *testing.T) {
	props := parseProps(&program.Program{}, `{"nested":{"score":1},"items":[1]}`)
	nested, items, root := props["nested"], props["items"], props["props"]
	nested.dict()["score"] = IntVal(2)
	items.list()[0] = IntVal(3)
	view := root.dict()
	viewNested, viewItems := view["nested"], view["items"]
	if viewNested.dict()["score"].Int64() != 1 || viewItems.list()[0].Int64() != 1 {
		t.Fatal("the props object aliases individual prop containers")
	}
	viewNested.dict()["score"] = IntVal(4)
	if nested.dict()["score"].Int64() != 2 {
		t.Fatal("individual props alias the props object")
	}
}

var benchmarkAutoKey string

func BenchmarkAutoKey(b *testing.B) {
	for _, tc := range []struct {
		name  string
		prop  string
		value Value
	}{
		{"index", "_index", IntVal(127)},
		{"wide-index", "_index", Int64Val(1<<60 + 1)},
		{"key", "_key", StringVal("tide-pool-7")},
	} {
		b.Run(tc.name, func(b *testing.B) {
			vm := &VM{props: map[string]Value{tc.prop: tc.value}}
			b.ReportAllocs()
			for b.Loop() {
				benchmarkAutoKey, _ = vm.autoKey(65535, "li")
			}
		})
	}
}
