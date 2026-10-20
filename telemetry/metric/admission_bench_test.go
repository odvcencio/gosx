package metric

import (
	"fmt"
	"testing"
)

func BenchmarkDeclareBatch(b *testing.B) {
	for _, count := range []int{2048, 8192, 16384, 32768} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			values := make([][]string, count)
			for i := range values {
				values[i] = []string{fmt.Sprint(i / 64), fmt.Sprint(i % 64)}
			}
			batch := make([]TupleDeclaration, count)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				r, err := NewRegistry(RegistryOptions{MaxBytes: 32 << 20})
				if err != nil {
					b.Fatal(err)
				}
				v, err := r.NewCounter(CounterOptions{Name: "admission_total", Labels: []Label{{Name: "route", MaxValues: 1024}, {Name: "code", MaxValues: 64}}})
				if err != nil {
					b.Fatal(err)
				}
				for j := range batch {
					batch[j] = TupleDeclaration{v, values[j]}
				}
				if err := r.DeclareBatch(batch); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
