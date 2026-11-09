package teamai

import (
	"fmt"
	"math"
	"runtime"
	"testing"
)

// These tests pin exact hashes. They were recorded on linux/amd64 at the
// default GOAMD64=v1 and must match on every platform GoSX targets: amd64 at
// any GOAMD64 level, arm64 (which fuses multiply-adds wherever the code lets
// it) and js/wasm. A mismatch means some float arithmetic in the package is
// not rounded explicitly, or depends on the platform.
//
// To re-record after an intended change, run with -run Deterministic -v and
// copy the printed values.

// drillCheckpoints are the drill state hashes (executor state, every body,
// the flag) after ticks 100, 200, ..., 1000 of the 22-agent drill.
var drillCheckpoints = [10]uint64{
	0x6675df1c0f689f26,
	0x2ea4d2b47679f1a3,
	0xb804b737c0a0f694,
	0xc401f99ded4ee714,
	0xca3c45d4aca00494,
	0xf027c1068621c888,
	0x4f5f6bf597ca52b3,
	0xb8f5bd4b89ac205b,
	0x45077a02aacc0457,
	0x67bef9ec8b64863b,
}

func TestDeterministicDrill1000Ticks(t *testing.T) {
	d := newDrill(t, 11, 2026)
	var got [10]uint64
	for i := 1; i <= 1000; i++ {
		d.step()
		if i%100 == 0 {
			got[i/100-1] = d.hash()
		}
	}
	t.Logf("%s/%s fuses x*y+z: %v; grabs %d, tags %d, escapes %d",
		runtime.GOOS, runtime.GOARCH, buildFuses(1+0x1p-30, 1-0x1p-30, -1), d.grabs, d.tags, d.escapes)
	if got != drillCheckpoints {
		for i := range got {
			if got[i] != drillCheckpoints[i] {
				t.Errorf("first difference after tick %d", (i+1)*100)
				break
			}
		}
		t.Errorf("checkpoints:\n%s", formatHashes(got[:]))
	}
	if d.grabs == 0 || d.tags+d.escapes == 0 {
		t.Errorf("the drill did not exercise grabs (%d) and contests (%d)", d.grabs, d.tags+d.escapes)
	}
}

// steeringHash is the hash of 20,000 seeded steering, interception and
// contest queries.
const steeringHash uint64 = 0xa535b4ef475e1da

func TestDeterministicSteering(t *testing.T) {
	r := NewRand(99)
	h := uint64(14695981039346656037)
	put := func(f float64) {
		b := math.Float64bits(f)
		for k := 0; k < 8; k++ {
			h ^= (b >> (8 * k)) & 0xff
			h *= 1099511628211
		}
	}
	bounds := Rect{Min: V2(-25, -50), Max: V2(25, 50)}
	others := make([]Body, 8)
	m := ContestModel{Draw: 0.1, Bias: 0.05}
	for i := 0; i < 20000; i++ {
		pos := V2(r.Range(-20, 20), r.Range(-40, 40))
		tpos := V2(r.Range(-20, 20), r.Range(-40, 40))
		tvel := V2(r.Range(-7, 7), r.Range(-7, 7))
		v0, vmax, acc := r.Range(0, 5), r.Range(5, 9), r.Range(0, 9)
		in := Intercept(pos, v0, vmax, acc, tpos, tvel, 5)
		put(in.Point.X)
		put(in.Point.Y)
		put(in.Time)
		cut := Cutoff(pos, v0, vmax, acc, tpos, tvel, bounds, r.Range(0, 0.5), 5)
		put(cut.Dir.X)
		put(cut.Dir.Y)
		put(cut.Time)
		a := Arrive(pos, tpos, vmax, acc, 0.25)
		put(a.X)
		put(a.Y)
		for k := range others {
			others[k].Pos = V2(r.Range(-3, 3), r.Range(-3, 3)).Add(pos)
		}
		s := Separation(pos, -1, others, 2.5, -1)
		put(s.X)
		put(s.Y)
		c := Contest{Attacker: r.Range(40, 99), Defender: r.Range(40, 99), Angle: ApproachAngle(tvel, pos.Sub(tpos)),
			RelSpeed: r.Range(-3, 3), Timing: r.Range(-0.4, 0.4)}
		o := m.Resolve(c, 7, uint64(i), 1, 2)
		put(o.P)
		put(float64(o.Result))
	}
	if h != steeringHash {
		t.Errorf("steering hash %#x, pinned %#x", h, steeringHash)
	}
}

func formatHashes(hs []uint64) string {
	s := ""
	for _, h := range hs {
		s += fmt.Sprintf("\t%#x,\n", h)
	}
	return s
}

// buildFuses reports whether this build fuses x*y + z.
//
//go:noinline
func buildFuses(x, y, z float64) bool {
	return x*y+z != float64(x*y)+z
}
