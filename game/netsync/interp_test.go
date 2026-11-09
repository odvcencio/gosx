package netsync

import (
	"math"
	"testing"
)

func TestInterpolatorSamplesBetweenTicks(t *testing.T) {
	ip := NewInterpolator(2, 4)
	if _, ok := ip.Latest(); ok {
		t.Fatal("empty buffer reports a tick")
	}
	ip.Begin(10)
	ip.Add(1, []float32{0, 0})
	ip.Add(2, []float32{10, 10})
	ip.Begin(12)
	ip.Add(2, []float32{20, 30}) // order changed
	ip.Add(1, []float32{4, -4})
	ip.Begin(11) // out of order: ignored with its Adds
	ip.Add(1, []float32{999, 999})

	got := map[uint32][2]float32{}
	collect := func(id uint32, v []float32) { got[id] = [2]float32{v[0], v[1]} }
	ip.Sample(11, collect)
	if got[1] != [2]float32{2, -2} || got[2] != [2]float32{15, 20} {
		t.Fatalf("midpoint %v", got)
	}
	ip.Sample(5, collect) // before the oldest: hold the oldest
	if got[1] != [2]float32{0, 0} {
		t.Fatalf("before oldest %v", got)
	}
	ip.Sample(50, collect) // after the newest: hold the newest
	if got[1] != [2]float32{4, -4} || got[2] != [2]float32{20, 30} {
		t.Fatalf("after newest %v", got)
	}
	if tick, _ := ip.Latest(); tick != 12 {
		t.Fatalf("latest %d", tick)
	}
}

func TestInterpolatorEntityAppearsAndLeaves(t *testing.T) {
	ip := NewInterpolator(1, 3)
	ip.Begin(1)
	ip.Add(7, []float32{1})
	ip.Begin(2)
	ip.Add(8, []float32{5})
	seen := map[uint32]float32{}
	ip.Sample(1.5, func(id uint32, v []float32) { seen[id] = v[0] })
	if seen[7] != 1 || len(seen) != 1 {
		t.Fatalf("an entity missing from the next frame should hold: %v", seen)
	}
	// Ring wraps: four frames into three slots drops the oldest.
	ip.Begin(3)
	ip.Add(8, []float32{6})
	ip.Begin(4)
	ip.Add(8, []float32{8})
	clear(seen)
	ip.Sample(1, func(id uint32, v []float32) { seen[id] = v[0] })
	if seen[8] != 5 {
		t.Fatalf("oldest kept frame should be tick 2: %v", seen)
	}
	clear(seen)
	ip.Sample(3.25, func(id uint32, v []float32) { seen[id] = v[0] })
	if math.Abs(float64(seen[8]-6.5)) > 1e-6 {
		t.Fatalf("tick 3.25: %v", seen)
	}
}

func TestInterpolatorAllocations(t *testing.T) {
	ip := NewInterpolator(3, 8)
	vals := []float32{1, 2, 3}
	tick := uint64(0)
	push := func() {
		tick++
		ip.Begin(tick)
		for id := uint32(0); id < 16; id++ {
			ip.Add(id, vals)
		}
	}
	for range 16 {
		push()
	}
	sink := float32(0)
	if n := testing.AllocsPerRun(100, func() {
		push()
		ip.Sample(float64(tick)-1.5, func(_ uint32, v []float32) { sink += v[0] })
	}); n != 0 {
		t.Fatalf("%v allocations per push and sample", n)
	}
}
