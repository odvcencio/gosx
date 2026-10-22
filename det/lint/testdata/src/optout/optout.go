// This package has no directive, so nothing is reported.
package optout

import (
	"math"
	"time"
)

func f(x, y, z float64, m map[int]int) float64 {
	for range m {
	}
	_ = time.Now()
	return x*y + z + math.Sin(x)
}
