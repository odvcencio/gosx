package vm

import "testing"

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
