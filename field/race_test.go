package field

import (
	"sync"
	"testing"
)

func TestSampleConcurrent(t *testing.T) {
	f := FromFunc([3]int{32, 32, 32}, 3,
		AABB{Max: [3]float32{1, 1, 1}},
		func(x, y, z float32) []float32 { return []float32{x, y, z} },
	)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				_ = f.SampleVec3(0.5, 0.5, 0.5)
			}
		}()
	}
	wg.Wait()
}
