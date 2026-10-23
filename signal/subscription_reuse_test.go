package signal

import (
	"math/rand/v2"
	"testing"
)

func TestComputedRepeatedDependencyChangesKeepCurrentValues(t *testing.T) {
	sources := []*Signal[int]{New(1), New(2), New(3)}
	values := []int{1, 2, 3}
	route, selected := New(0), 0
	sum := Derive(func() int {
		switch route.Get() {
		case 0:
			return sources[0].Get() + sources[1].Get()
		case 1:
			return sources[1].Get() + sources[0].Get()
		case 2:
			return sources[2].Get() + sources[0].Get()
		default:
			return sources[0].Get()
		}
	})
	defer sum.Stop()
	result := Derive(func() int { return 3 * sum.Get() })
	defer result.Stop()
	expected := func() int {
		switch selected {
		case 0, 1:
			return 3 * (values[0] + values[1])
		case 2:
			return 3 * (values[2] + values[0])
		default:
			return 3 * values[0]
		}
	}
	calls := 0
	check := func() {
		t.Helper()
		if got, want := result.Get(), expected(); got != want {
			t.Fatalf("computed=%d, want %d (route %d)", got, want, selected)
		}
	}
	result.Subscribe(func() { calls++; check() })
	rng := rand.New(rand.NewPCG(0xabc, 0xdef))
	change := func() {
		if rng.IntN(4) == 0 {
			selected = rng.IntN(4)
			route.Set(selected)
		} else {
			i := rng.IntN(3)
			values[i] = rng.IntN(100)
			sources[i].Set(values[i])
		}
	}
	for iteration := range 600 {
		before, old := calls, result.Get()
		if iteration%3 == 0 {
			Batch(func() { change(); check(); change(); check() })
		} else {
			change()
			check()
			want := 0
			if expected() != old {
				want = 1
			}
			if got := calls - before; got != want {
				t.Fatalf("iteration %d: notifications=%d, want %d", iteration, got, want)
			}
		}
		check()
	}
	last := result.Get()
	result.Stop()
	values[0]++
	sources[0].Set(values[0])
	if result.Get() != last {
		t.Fatal("stopped computed changed")
	}
}

func TestComputedRepeatedDiamondChangesNotifyOnce(t *testing.T) {
	base := New(0)
	left := Derive(func() int { return 2 * base.Get() })
	defer left.Stop()
	right := Derive(func() int { return 3 * base.Get() })
	defer right.Stop()
	result := Derive(func() []int { return []int{left.Get() + right.Get()} })
	defer result.Stop()
	calls := 0
	result.Subscribe(func() {
		calls++
		if got, want := result.Get()[0], 5*base.Get(); got != want {
			t.Fatalf("callback=%d, want %d", got, want)
		}
	})
	for value := 1; value <= 20; value++ {
		calls = 0
		base.Set(value)
		if got := result.Get()[0]; got != 5*value || calls != 1 {
			t.Fatalf("value %d: computed=%d, notifications=%d", value, got, calls)
		}
	}
}
