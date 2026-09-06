package headless

import (
	"math"
	"testing"
)

func TestWorldLineHomogeneousClip(t *testing.T) {
	for axis := 0; axis < 3; axis++ {
		for _, sign := range []float32{-1, 1} {
			a := clipVertex{clip: [4]float32{0, 0, 0, 1}, color: [4]float32{1, 0, 0, 1}}
			b := a
			b.clip[axis] = sign * 2
			b.color = [4]float32{0, 0, 1, .5}
			x, y, ok := clipLine(a, b)
			if !ok || x.clip != a.clip || y.clip[axis] != sign || y.color[0] != .5 || y.color[3] != .75 {
				t.Fatalf("clip %+v %+v %v", x, y, ok)
			}
			a.clip[axis] = sign * 3
			if _, _, ok := clipLine(a, b); ok {
				t.Fatal("outside admitted")
			}
		}
	}
	if _, _, ok := clipLine(clipVertex{clip: [4]float32{float32(math.NaN()), 0, 0, 1}}, clipVertex{clip: [4]float32{0, 0, 0, 1}}); ok {
		t.Fatal("NaN admitted")
	}
}
