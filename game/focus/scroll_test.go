package focus

import "testing"

func TestScrollDelta(t *testing.T) {
	for _, c := range []struct {
		deflection, seconds, speed, dead, want float64
	}{
		{0, 1, 600, 0.25, 0},
		{0.2, 1, 600, 0.25, 0},
		{-0.25, 1, 600, 0.25, 0},
		{1, 1, 600, 0.25, 600},
		{-1, 0.5, 600, 0.25, -300},
		{0.625, 1, 600, 0.25, 300},
		{2, 1, 600, 0.25, 600},
		{1, 0, 600, 0.25, 0},
		{1, 1, 600, 1, 0},
	} {
		if got := ScrollDelta(c.deflection, c.seconds, c.speed, c.dead); got != c.want {
			t.Errorf("ScrollDelta(%v, %v, %v, %v) = %v, want %v", c.deflection, c.seconds, c.speed, c.dead, got, c.want)
		}
	}
}
