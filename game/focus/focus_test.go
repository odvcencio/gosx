package focus

import "testing"

func TestWalk(t *testing.T) {
	for _, c := range []struct {
		n, at  int
		d      Dir
		noWrap bool
		want   int
	}{
		{3, -1, Next, false, 0}, {3, -1, Down, false, 0}, {3, -1, Prev, false, 2}, {3, -1, Up, false, 2},
		{3, 0, Right, false, 1}, {3, 2, Next, false, 0}, {3, 0, Left, false, 2},
		{3, 2, Next, true, 2}, {3, 0, Up, true, 0},
		{0, -1, Next, false, -1}, {1, 0, Next, false, 0}, {3, 7, Prev, false, 2},
	} {
		if got := Walk(c.n, c.at, c.d, c.noWrap); got != c.want {
			t.Errorf("Walk(%d, %d, %d, %v) = %d, want %d", c.n, c.at, c.d, c.noWrap, got, c.want)
		}
	}
}

func TestInRow(t *testing.T) {
	for _, c := range []struct {
		n, at int
		d     Dir
		want  int
		used  bool
	}{
		{3, 1, Right, 2, true}, {3, 2, Right, 2, true}, {3, 0, Left, 0, true}, {3, 1, Left, 0, true},
		{3, 1, Down, 1, false}, {3, -1, Right, -1, false},
	} {
		got, used := InRow(c.n, c.at, c.d)
		if got != c.want || used != c.used {
			t.Errorf("InRow(%d, %d, %d) = %d, %v; want %d, %v", c.n, c.at, c.d, got, used, c.want, c.used)
		}
	}
}

func TestStepChoice(t *testing.T) {
	abc := func(disabled ...int) func(int) bool {
		return func(i int) bool {
			for _, d := range disabled {
				if d == i {
					return false
				}
			}
			return true
		}
	}
	for _, c := range []struct {
		name               string
		n, selected, delta int
		enabled            func(int) bool
		want               int
	}{
		{"skips a disabled option", 3, 0, 1, abc(1), 2},
		{"wraps", 3, 2, 1, abc(1), 0},
		{"back", 3, 0, -1, abc(), 2},
		{"two on", 4, 0, 2, abc(1), 3},
		{"only one enabled: stays", 3, 1, 1, abc(0, 2), 1},
		{"all disabled", 3, 1, 1, abc(0, 1, 2), -1},
		{"selected disabled moves to an enabled one", 3, 1, 1, abc(1), 2},
		{"nothing selected, on", 3, -1, 1, abc(), 0},
		{"nothing selected, back", 3, -1, -1, abc(), 2},
		{"empty", 0, -1, 1, abc(), -1},
		{"zero delta", 3, 1, 0, abc(), 1},
	} {
		if got := StepChoice(c.n, c.selected, c.delta, c.enabled); got != c.want {
			t.Errorf("%s: StepChoice = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestStepRange(t *testing.T) {
	q := Range{Min: 0, Max: 1, Step: .25}
	for _, c := range []struct {
		r     Range
		v     float64
		delta int
		wrap  bool
		want  float64
	}{
		{q, .75, 1, true, 1}, {q, 1, 1, true, 0}, {q, 1, 1, false, 1}, {q, 0, -1, true, 1}, {q, 0, -1, false, 0},
		// Off the grid: snap first.
		{q, .3, 1, false, .5},
		// A step that is not a binary fraction still reaches the end.
		{Range{Min: 0, Max: 1, Step: .1}, .9, 1, true, 1}, {Range{Min: 0, Max: 1, Step: .1}, 1, 1, true, 0},
		// Past the end by part of a step: clamp, then wrap on the next press.
		{Range{Min: 0, Max: 10, Step: 3}, 9, 1, true, 10}, {Range{Min: 0, Max: 10, Step: 3}, 10, 1, true, 0},
		// No step: a hundredth of the span.
		{Range{Min: -100, Max: 100}, 0, 1, false, 2},
		// Degenerate ranges.
		{Range{Min: 5, Max: 5, Step: 1}, 5, 1, true, 5}, {Range{Min: 5, Max: 2}, 3, 1, false, 5},
	} {
		got := StepRange(c.r, c.v, c.delta, c.wrap)
		if d := got - c.want; d > 1e-9 || d < -1e-9 {
			t.Errorf("StepRange(%+v, %v, %d, %v) = %v, want %v", c.r, c.v, c.delta, c.wrap, got, c.want)
		}
	}
}
