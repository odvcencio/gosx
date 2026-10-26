//go:build !race && !tinygo

package signal

import (
	"fmt"
	"testing"
)

func TestComputedStableDependenciesDoNotAllocateAfterWarmup(t *testing.T) {
	for _, count := range []int{1, 5, 16, 64} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			sources := make([]*Signal[int], count)
			for i := range sources {
				sources[i] = New(0)
			}
			computed := Derive(func() int {
				sum := 0
				for _, source := range sources {
					sum += source.Get()
				}
				return sum
			})
			defer computed.Stop()
			computed.Subscribe(func() {})
			value := 0
			recompute := func() {
				value++
				sources[0].Set(value)
				if got := computed.Get(); got != value {
					t.Fatalf("computed = %d, want %d", got, value)
				}
			}
			for range 3 {
				recompute()
			}
			if allocations := testing.AllocsPerRun(100, recompute); allocations != 0 {
				t.Fatalf("stable %d-source recomputation allocated %g times", count, allocations)
			}
		})
	}
}
