//gosx:deterministic

package hazards

import (
	"maps"
	"math"
	"math/rand"
	"slices"
	"time"
)

type Meters float64

func fma(x, y, z float64) float64 {
	a := x*y + z // want `fused-multiply-add: float product`
	b := z - x*y // want `fused-multiply-add`
	c := float64(x*y) + z
	d := x * y // want `fused-multiply-add`
	e := float64((x * y) * z)
	f := x*y/z + 1
	g := -(x * y) + z // want `fused-multiply-add`
	h := x*y < z
	i := int(x * y)
	j := take(x * y) // want `fused-multiply-add`
	k := 2*x + y
	l := x*0.5 + y
	m := float32(x * y)
	_, _, _, _, _, _, _, _, _, _, _, _ = a, b, c, d, e, f, g, h, i, j, k, l
	_ = m
	return float64(x*y) + float64(z*x)
}

func take(v float64) float64 { return v }

func ret(x, y float64) float64 {
	return x * y // want `fused-multiply-add`
}

func compound(x, y float64) float64 {
	x *= y // want `fused-multiply-add: \*=`
	x *= 2
	x += y
	return x
}

func named(x, y Meters) Meters {
	return x*y + 1 // want `fused-multiply-add`
}

func consts() float64 {
	const a = 3.0 * 4.0
	return a + 1
}

func ints(a, b, c int) int { return a*b + c }

func complexes(a, b, c complex128) complex128 {
	return a*b + c // want `fused-multiply-add`
}

func mathCalls(x float64) float64 {
	_ = math.Sqrt(x)
	_ = math.Floor(x)
	_ = math.FMA(x, x, x)
	f := math.Sin // want `math-call: math.Sin can differ .* use det.Sin`
	_ = f
	_ = math.Erf(x)                       // want `math-call: math.Erf .* no det replacement`
	return math.Exp(x) + math.Atan2(x, 1) // want `math-call: math.Exp .* det.Exp` `math-call: math.Atan2 .* det.Atan2`
}

func maprange(m map[string]int) (n int) {
	for k, v := range m { // want `map-range: range over a map`
		_ = k
		n += v
	}
	for range maps.Keys(m) { // want `map-range: maps.Keys`
		n++
	}
	for _, k := range slices.Sorted(maps.Keys(m)) {
		n += len(k)
	}
	for _, v := range []int{1, 2} {
		n += v
	}
	//gosx:allow map-range summing integers is order independent
	for _, v := range m {
		n += v
	}
	for _, v := range m { //gosx:allow map-range order independent sum
		n += v
	}
	return n
}

func clocks() time.Duration {
	start := time.Now()      // want `wall-clock: time.Now`
	return time.Since(start) // want `wall-clock: time.Since`
}

func clockOK(d time.Duration) time.Duration { return d * 2 }

func randoms(seed int64) float64 {
	_ = rand.Intn(10) // want `global-rand`
	r := rand.New(rand.NewSource(seed))
	return r.Float64()
}

func concurrency(ch chan int) {
	go func() {}() // want `goroutine: go statement`
	select {       // want `select: select statement`
	case <-ch:
	default:
	}
}

func badAllow(m map[int]int) {
	// want +1 `gosx:allow map-range needs a reason`
	//gosx:allow map-range
	for range m { // want `map-range`
	}
	// want +1 `gosx:allow needs a rule name`
	//gosx:allow nonsense because
}
