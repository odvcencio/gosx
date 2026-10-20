package motion

import (
	"math"
	"sync"
	"testing"
)

func TestPreparedSpringMatchesOriginalBits(t *testing.T) {
	physics := []Spring{
		{}, {Mass: 1, Stiffness: 400, Damping: 2, Velocity: -.1},
		{Mass: 1, Stiffness: 144, Damping: 24},
		{Mass: 1, Stiffness: 100, Damping: 60, Velocity: 3},
		{Mass: .3, Stiffness: 1200, Damping: 30, Velocity: -10},
		{Mass: -1, Stiffness: -2, Damping: -3},
	}
	for _, spring := range physics {
		for _, endpoints := range [][2]float64{{0, 1}, {-4, 2}, {1e6, -1e6}, {math.Copysign(0, -1), 0}} {
			from, to := endpoints[0], endpoints[1]
			prepared := spring.Prepare(from, to)
			if prepared.Duration() != spring.Duration(from, to) || len(prepared.checkpoints)*16 > 1216 {
				t.Fatal("incorrect duration or unbounded checkpoint table")
			}
			times := []float64{-1, 0, math.NaN(), math.Inf(1), math.Inf(-1), math.Nextafter(prepared.Duration(), 0), prepared.Duration(), prepared.Duration() + 1}
			for step := 0; step <= int(math.Ceil(prepared.Duration()/SpringDT)); step++ {
				at := float64(step) * SpringDT
				times = append(times, math.Nextafter(at, 0), at, math.Nextafter(at, math.Inf(1)), at+.37*SpringDT)
			}
			for i := len(times) - 1; i >= 0; i-- {
				at := times[i]
				got, want := prepared.Value(at), spring.Value(from, to, at)
				if math.Float64bits(got) != math.Float64bits(want) {
					t.Fatalf("%+v [%g,%g] at %.20g: got %.20g (%x), want %.20g (%x)", spring, from, to, at, got, math.Float64bits(got), want, math.Float64bits(want))
				}
			}
		}
	}
}

func TestPreparedSpringNonFiniteAndUnstableInputs(t *testing.T) {
	cases := []struct {
		spring   Spring
		from, to float64
	}{
		{Spring{Mass: math.NaN()}, 0, 1},
		{Spring{Stiffness: math.Inf(1)}, 0, 1},
		{Spring{Damping: math.Inf(1)}, 0, 1},
		{Spring{Velocity: math.Inf(-1)}, 0, 1},
		{Spring{}, math.NaN(), 1},
		{Spring{}, 0, math.Inf(1)},
		{Spring{Mass: math.Inf(-1), Stiffness: -1, Damping: -1}, 0, 1},
		{Spring{Mass: .001, Stiffness: 1e8, Damping: 1}, 0, 1},
	}
	for _, c := range cases {
		prepared := c.spring.Prepare(c.from, c.to)
		for _, at := range []float64{math.NaN(), math.Inf(1), math.Inf(-1), -1, 0, .003, .13, .5, 2, 9, 11, 1e100, math.MaxFloat64} {
			got, want := prepared.Value(at), c.spring.Value(c.from, c.to, at)
			if math.Float64bits(got) != math.Float64bits(want) && !(math.IsNaN(got) && math.IsNaN(want)) {
				t.Fatalf("%+v [%g,%g] at %g: %g vs %g", c.spring, c.from, c.to, at, got, want)
			}
		}
	}
	var zero PreparedSpring
	if zero.Value(1) != 0 || zero.Duration() != 0 {
		t.Fatal("zero sampler is not constant zero")
	}
}

func TestSpringNonFiniteTimesHaveBoundedWork(t *testing.T) {
	for _, spring := range []Spring{{}, {Mass: math.NaN()}, {Stiffness: math.Inf(1)}} {
		if spring.Value(-2, 3, math.NaN()) != -2 || spring.Value(-2, 3, math.Inf(-1)) != -2 || spring.Value(-2, 3, math.Inf(1)) != 3 {
			t.Fatal("non-finite times do not hold the defined endpoints")
		}
	}
	invalid := Spring{Mass: math.NaN()}
	for _, at := range []float64{11, 1e100, math.MaxFloat64} {
		if !math.IsNaN(invalid.Value(0, 1, at)) {
			t.Fatal("invalid physics did not preserve its invalid numeric result")
		}
	}
}

func TestPreparedSpringImmutableConcurrentAndZeroAlloc(t *testing.T) {
	spring := Spring{Mass: 1, Stiffness: 400, Damping: 2, Velocity: -.1}
	prepared := spring.Prepare(-.2, 1.4)
	want := spring.Value(-.2, 1.4, 8.007)
	spring.Stiffness = 1
	if prepared.Value(8.007) != want {
		t.Fatal("preparation aliases spring configuration")
	}
	if allocations := testing.AllocsPerRun(1000, func() { prepared.Value(8.007) }); allocations != 0 {
		t.Fatalf("sampling allocated %g times", allocations)
	}
	var readers sync.WaitGroup
	for i := 0; i < 8; i++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for j := 0; j < 100; j++ {
				if prepared.Value(8.007) != want {
					t.Error("concurrent sampling changed the result")
				}
			}
		}()
	}
	readers.Wait()
}

var preparedSpringBenchmarkSink float64

func BenchmarkSpringRepeatedSeeks(b *testing.B) {
	spring := Spring{Mass: 1, Stiffness: 400, Damping: 2, Velocity: -.1}
	prepared := spring.Prepare(-.2, 1.4)
	for _, cached := range []bool{false, true} {
		name := "original"
		if cached {
			name = "prepared"
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				at := .1 + float64((i*149)%251)*.0357
				if cached {
					preparedSpringBenchmarkSink = prepared.Value(at)
				} else {
					preparedSpringBenchmarkSink = spring.Value(-.2, 1.4, at)
				}
			}
		})
	}
}
