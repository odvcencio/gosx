package teamai

import (
	"math"
	"testing"
)

func TestContestEvenIsAHalf(t *testing.T) {
	var m ContestModel
	// Equal ratings, a side-on approach (cos = 0), no speed edge, and timing
	// half the window off (the timing term is 0).
	c := Contest{Attacker: 70, Defender: 70, Angle: math.Pi / 2, Timing: 0.125}
	if p := m.Prob(c); !closeTo(p, 0.5, 1e-15) {
		t.Fatalf("even contest p = %v, want 0.5", p)
	}
}

func TestContestTermsPushTheRightWay(t *testing.T) {
	var m ContestModel
	base := Contest{Attacker: 70, Defender: 70, Angle: math.Pi / 2, Timing: 0.125}
	p0 := m.Prob(base)
	better := []struct {
		name string
		edit func(c *Contest)
	}{
		{"higher attacker rating", func(c *Contest) { c.Attacker = 80 }},
		{"better angle", func(c *Contest) { c.Angle = 0.2 }},
		{"more speed", func(c *Contest) { c.RelSpeed = 2 }},
		{"better timing", func(c *Contest) { c.Timing = 0.01 }},
	}
	for _, b := range better {
		c := base
		b.edit(&c)
		if p := m.Prob(c); p <= p0 {
			t.Errorf("%s: p = %v, not above %v", b.name, p, p0)
		}
	}
	worse := base
	worse.Defender, worse.Angle, worse.Timing = 90, math.Pi, 1
	if p := m.Prob(worse); p >= p0 {
		t.Errorf("worse contest p = %v, not below %v", p, p0)
	}
}

func TestContestChancesAndFloor(t *testing.T) {
	m := ContestModel{Draw: 0.2, Floor: 0.05}
	win, draw, loss := m.Chances(Contest{Attacker: 70, Defender: 70, Angle: math.Pi / 2, Timing: 0.125})
	if !closeTo(win, 0.4, 1e-15) || !closeTo(draw, 0.2, 1e-15) || !closeTo(loss, 0.4, 1e-15) {
		t.Fatalf("chances %v %v %v, want 0.4 0.2 0.4", win, draw, loss)
	}
	if p := m.Prob(Contest{Attacker: 0, Defender: 1000}); p != 0.05 {
		t.Fatalf("floor: p = %v, want 0.05", p)
	}
}

// The pinned values below are exact bits. They must be the same on amd64,
// arm64 and js/wasm; a change means the model's arithmetic changed.
func TestContestPinnedProbabilities(t *testing.T) {
	m := ContestModel{Bias: 0.1, Draw: 0.1}
	cases := []Contest{
		{Attacker: 72, Defender: 65, Angle: 0.4, RelSpeed: 1.5, Timing: 0.05},
		{Attacker: 55, Defender: 80, Angle: 2.1, RelSpeed: -0.7, Timing: 0.3},
		{Attacker: 90, Defender: 60, Angle: 0, RelSpeed: 12, Timing: 0},
		{Attacker: 61.5, Defender: 61.5, Angle: 1.3, RelSpeed: 0.25, Timing: 0.2},
	}
	want := []uint64{contestPin0, contestPin1, contestPin2, contestPin3}
	for i, c := range cases {
		p := m.Prob(c)
		if got := math.Float64bits(p); got != want[i] {
			t.Errorf("case %d: p = %v (bits %#x), pinned %#x", i, p, got, want[i])
		}
	}
}

// Recorded on linux/amd64 (GOAMD64=v1); 0.9043102225491665,
// 0.02903337390050884, 0.9977621514787236 and 0.5262256173338766.
const (
	contestPin0 = 0x3fecf01bfde93059
	contestPin1 = 0x3f9dbaecbd94b5f9
	contestPin2 = 0x3fefedaae43934f8
	contestPin3 = 0x3fe0d6d71b188670
)

// TestContestDistribution resolves one contest under 20,000 keys and checks
// the result counts against the chances, then pins the exact counts.
func TestContestDistribution(t *testing.T) {
	m := ContestModel{Draw: 0.1}
	c := Contest{Attacker: 72, Defender: 65, Angle: 0.9, RelSpeed: 0.5, Timing: 0.1}
	win, draw, loss := m.Chances(c)
	const n = 20000
	var counts [3]int // loss, draw, win
	for k := uint64(0); k < n; k++ {
		o := m.Resolve(c, 2026, k, 7, 11)
		counts[o.Result+1]++
		if o.Margin != o.P-o.Roll {
			t.Fatalf("margin %v != p - roll", o.Margin)
		}
	}
	for i, want := range []float64{loss, draw, win} {
		got := float64(counts[i]) / n
		// Four standard deviations of a binomial proportion.
		sd := math.Sqrt(want * (1 - want) / n)
		if math.Abs(got-want) > 4*sd {
			t.Errorf("result %d: frequency %.4f, chance %.4f (sd %.4f)", i-1, got, want, sd)
		}
	}
	if counts != contestCounts {
		t.Errorf("counts %v changed from the pinned %v; they must be identical on every platform", counts, contestCounts)
	}
}

// Loss, draw and win counts over keys 0 to 19,999. The chances are 0.1330,
// 0.1000 and 0.7670.
var contestCounts = [3]int{2649, 2006, 15345}

func TestContestResolveIsKeyed(t *testing.T) {
	var m ContestModel
	c := Contest{Attacker: 70, Defender: 68}
	a := m.Resolve(c, 1, 2, 3, 4)
	if b := m.Resolve(c, 1, 2, 3, 4); a != b {
		t.Fatalf("same keys gave %+v and %+v", a, b)
	}
	if b := m.Resolve(c, 1, 2, 4, 3); a.Roll == b.Roll {
		t.Fatal("swapping keys gave the same roll")
	}
}
