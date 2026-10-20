package metric

import (
	"fmt"
	"slices"
	"testing"
)

func TestSingleAdmissionMaintainsTupleOrder(t *testing.T) {
	r := &Registry{}
	v, err := r.NewCounter(CounterOptions{Name: "ordered_total", Labels: []Label{
		{Name: "first", MaxValues: 64}, {Name: "second", MaxValues: 64},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tuple := range [][]string{{"z", "a"}, {"a", "z"}, {"a", "a"}, {"m", "m"}} {
		if err := v.Declare(tuple...); err != nil {
			t.Fatal(err)
		}
	}
	if !slices.IsSortedFunc(v.family.ordered, compareCells) {
		t.Fatal("single admissions broke tuple ordering")
	}
	if err := r.DeclareBatch([]TupleDeclaration{{v, []string{"b", "z"}}, {v, []string{"b", "a"}}}); err != nil {
		t.Fatal(err)
	}
	if !slices.IsSortedFunc(v.family.ordered, compareCells) {
		t.Fatal("batch after single admissions broke tuple ordering")
	}
}

func BenchmarkSingleAdmission(b *testing.B) {
	const count = 32768
	first, second := make([]string, 256), make([]string, 128)
	for i := range first {
		first[i] = fmt.Sprintf("%03d", i)
	}
	for i := range second {
		second[i] = fmt.Sprintf("%03d", i)
	}
	tuples := make([][]string, count)
	for i := range tuples {
		tuples[i] = []string{first[i/128], second[i%128]}
	}
	for _, mode := range []string{"bind", "declare"} {
		b.Run(mode, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				// Admission speed is measured at 32,768 tuples with room for
				// snapshot storage. Default capacity is gated separately.
				r, err := NewRegistry(RegistryOptions{MaxBytes: 16 << 20})
				if err != nil {
					b.Fatal(err)
				}
				labels := []Label{{Name: "first", MaxValues: 256}, {Name: "second", MaxValues: 128}}
				if mode == "bind" {
					labels[0].Values, labels[1].Values = first, second
				}
				v, err := r.NewCounter(CounterOptions{Name: "admitted_total", Labels: labels})
				if err != nil {
					b.Fatal(err)
				}
				for i := count - 1; i >= 0; i-- {
					if mode == "bind" {
						_, err = v.Bind(tuples[i]...)
					} else {
						err = v.Declare(tuples[i]...)
					}
					if err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
