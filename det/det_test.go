package det

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"testing"
)

// rng is a fixed xorshift64* generator so every platform samples the same
// inputs.
type rng uint64

func (r *rng) next() uint64 {
	x := uint64(*r)
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	*r = rng(x)
	return x * 0x2545F4914F6CDD1D
}

// unit returns a float64 in [0, 1) built from 53 random bits.
func (r *rng) unit() float64 {
	return float64(r.next()>>11) / (1 << 53)
}

func (r *rng) in(lo, hi float64) float64 { return lo + float64((hi-lo)*r.unit()) }

type unaryCase struct {
	name string
	det  func(float64) float64
	std  func(float64) float64
	// ranges lists [lo, hi) intervals sampled uniformly.
	ranges [][2]float64
	// maxUlp is the bound against package math. It is 1 where math runs the
	// same portable algorithm. It is larger for functions built on Exp, because
	// math.Exp is assembly on amd64 and arm64 and differs in the last bits.
	maxUlp float64
	// ref, when set, is a high-precision reference. refUlp bounds det's error
	// against it; math has the same error against it (measured, see the
	// report in the commit message).
	ref    func(float64) float64
	refUlp float64
}

type binaryCase struct {
	name   string
	det    func(float64, float64) float64
	std    func(float64, float64) float64
	rangeX [2]float64
	rangeY [2]float64
	maxUlp float64
	ref    func(x, y float64) float64
	refUlp float64
}

var unaryCases = []unaryCase{
	{"Exp", Exp, math.Exp, [][2]float64{{-1, 1}, {-50, 50}, {-700, 700}}, 2, refExpF, 1},
	{"Exp2", Exp2, math.Exp2, [][2]float64{{-1, 1}, {-30, 30}, {-1000, 1000}}, 1, nil, 0},
	{"Log", Log, math.Log, [][2]float64{{0.5, 2}, {1e-6, 1e6}, {1e-300, 1e300}}, 1, nil, 0},
	{"Log2", Log2, math.Log2, [][2]float64{{0.5, 2}, {1e-6, 1e6}, {1e-300, 1e300}}, 1, nil, 0},
	{"Log10", Log10, math.Log10, [][2]float64{{0.5, 2}, {1e-6, 1e6}, {1e-300, 1e300}}, 1, nil, 0},
	{"Cbrt", Cbrt, math.Cbrt, [][2]float64{{-1, 1}, {-1000, 1000}, {-1e300, 1e300}, {-1e-310, 1e-310}}, 1, nil, 0},
	{"Sin", Sin, math.Sin, [][2]float64{{-1, 1}, {-100, 100}, {-1e6, 1e6}, {-1e12, 1e12}}, 1, nil, 0},
	{"Cos", Cos, math.Cos, [][2]float64{{-1, 1}, {-100, 100}, {-1e6, 1e6}, {-1e12, 1e12}}, 1, nil, 0},
	{"Tan", Tan, math.Tan, [][2]float64{{-1, 1}, {-100, 100}, {-1e6, 1e6}}, 1, nil, 0},
	{"Asin", Asin, math.Asin, [][2]float64{{-1, 1}}, 1, nil, 0},
	{"Acos", Acos, math.Acos, [][2]float64{{-1, 1}}, 1, nil, 0},
	{"Atan", Atan, math.Atan, [][2]float64{{-1, 1}, {-100, 100}, {-1e8, 1e8}}, 1, nil, 0},
	{"Sinh", Sinh, math.Sinh, [][2]float64{{-1, 1}, {-30, 30}, {-700, 700}}, 3, refSinh, 2},
	{"Cosh", Cosh, math.Cosh, [][2]float64{{-1, 1}, {-30, 30}, {-700, 700}}, 2, refCosh, 1},
	{"Tanh", Tanh, math.Tanh, [][2]float64{{-1, 1}, {-30, 30}, {-60, 60}}, 2, refTanh, 1},
}

var binaryCases = []binaryCase{
	{"Pow", Pow, math.Pow, [2]float64{0.01, 100}, [2]float64{-20, 20}, 8, refPow, 16},
	{"Atan2", Atan2, math.Atan2, [2]float64{-100, 100}, [2]float64{-100, 100}, 1, nil, 0},
	{"Hypot", Hypot, math.Hypot, [2]float64{-1e6, 1e6}, [2]float64{-1e6, 1e6}, 1, nil, 0},
}

const samplesPerRange = 8000

// ulpDiff returns the distance between a and b in units in the last place of
// the larger magnitude. It returns 0 for equal values and for two NaNs.
func ulpDiff(a, b float64) float64 {
	if a == b || (math.IsNaN(a) && math.IsNaN(b)) {
		return 0
	}
	if math.IsNaN(a) || math.IsNaN(b) || math.IsInf(a, 0) || math.IsInf(b, 0) {
		return math.Inf(1)
	}
	m := math.Max(math.Abs(a), math.Abs(b))
	if m < 0x1p-1022 {
		return math.Abs(a-b) / 0x1p-1074
	}
	_, e := math.Frexp(m)
	ulp := math.Ldexp(1, e-53)
	return math.Abs(a-b) / ulp
}

// buildFuses reports whether this build fuses x*y + z. Package math is
// compiled with the same flags, so on a fusing build (arm64, or amd64 with
// GOAMD64=v3) math differs from det in the last bits by design.
//
//go:noinline
func buildFuses(x, y, z float64) bool {
	return x*y+z != float64(x*y)+z
}

func skipIfFused(t *testing.T) {
	t.Helper()
	if buildFuses(1+0x1p-30, 1-0x1p-30, -1) {
		t.Skip("this build fuses x*y+z, so package math differs from det by design; golden and reference tests still run")
	}
}

func TestMatchesMath(t *testing.T) {
	skipIfFused(t)
	for _, c := range unaryCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			r := rng(0x9E3779B97F4A7C15)
			var worst float64
			var worstX float64
			for _, rg := range c.ranges {
				for i := 0; i < samplesPerRange; i++ {
					x := r.in(rg[0], rg[1])
					d := ulpDiff(c.det(x), c.std(x))
					if d > worst {
						worst, worstX = d, x
					}
					if c.ref != nil && i%8 == 0 {
						if rd := ulpDiff(c.det(x), c.ref(x)); rd > c.refUlp {
							t.Fatalf("%s is %.3f ulp from the reference at x=%v (limit %v)", c.name, rd, x, c.refUlp)
						}
					}
				}
			}
			t.Logf("worst %.3f ulp at x=%v", worst, worstX)
			if worst > c.maxUlp {
				t.Fatalf("%s differs from math by %.3f ulp at x=%v (limit %v)", c.name, worst, worstX, c.maxUlp)
			}
		})
	}
	for _, c := range binaryCases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			r := rng(0x9E3779B97F4A7C15)
			var worst, wx, wy float64
			for i := 0; i < samplesPerRange*3; i++ {
				x := r.in(c.rangeX[0], c.rangeX[1])
				y := r.in(c.rangeY[0], c.rangeY[1])
				d := ulpDiff(c.det(x, y), c.std(x, y))
				if d > worst {
					worst, wx, wy = d, x, y
				}
				if c.ref != nil && i%8 == 0 {
					if rd := ulpDiff(c.det(x, y), c.ref(x, y)); rd > c.refUlp {
						t.Fatalf("%s is %.3f ulp from the reference at (%v, %v) (limit %v)", c.name, rd, x, y, c.refUlp)
					}
				}
			}
			t.Logf("worst %.3f ulp at x=%v y=%v", worst, wx, wy)
			if worst > c.maxUlp {
				t.Fatalf("%s differs from math by %.3f ulp at (%v, %v) (limit %v)", c.name, worst, wx, wy, c.maxUlp)
			}
		})
	}
}

// TestSpecialCasesMatchMath checks the special values listed in the Go docs.
func TestSpecialCasesMatchMath(t *testing.T) {
	skipIfFused(t)
	vals := []float64{
		0, math.Copysign(0, -1), 1, -1, 0.5, -0.5, 2, -2, 3, 1e-310, -1e-310,
		math.Inf(1), math.Inf(-1), math.NaN(), math.MaxFloat64, -math.MaxFloat64,
		math.SmallestNonzeroFloat64, math.Pi, -math.Pi, math.Pi / 2, 1e300, 710, -746, 745,
		1 << 29, 1<<29 + 1, 1e22, -1e22,
	}
	same := func(a, b float64) bool {
		if math.IsNaN(a) || math.IsNaN(b) {
			return math.IsNaN(a) && math.IsNaN(b)
		}
		if a == 0 && b == 0 {
			return math.Signbit(a) == math.Signbit(b)
		}
		return ulpDiff(a, b) <= 1
	}
	for _, c := range unaryCases {
		for _, x := range vals {
			// math.Log is assembly on amd64 and is wrong for subnormal
			// inputs, see TestLogSubnormal.
			if (c.name == "Log" || c.name == "Log10") && x > 0 && x < 0x1p-1022 {
				continue
			}
			if got, want := c.det(x), c.std(x); !same(got, want) {
				t.Errorf("%s(%v) = %v, math gives %v", c.name, x, got, want)
			}
		}
	}
	for _, c := range binaryCases {
		for _, x := range vals {
			for _, y := range vals {
				if got, want := c.det(x, y), c.std(x, y); !same(got, want) {
					t.Errorf("%s(%v, %v) = %v, math gives %v", c.name, x, y, got, want)
				}
			}
		}
	}
}

func TestExactFunctionsAreExact(t *testing.T) {
	r := rng(7)
	for i := 0; i < 1000; i++ {
		x, y := r.in(-1e6, 1e6), r.in(-1e6, 1e6)
		if Sqrt(math.Abs(x)) != math.Sqrt(math.Abs(x)) || Floor(x) != math.Floor(x) || Ceil(x) != math.Ceil(x) ||
			Trunc(x) != math.Trunc(x) || Abs(x) != math.Abs(x) || Copysign(x, y) != math.Copysign(x, y) {
			t.Fatalf("exact wrapper differs at %v %v", x, y)
		}
	}
}

func TestMulAddIsNotFused(t *testing.T) {
	// x*y has a rounding error that FMA would keep. With a fused operation
	// the result would be z + the exact error term instead of 0.
	x := 1 + 0x1p-30
	y := 1 - 0x1p-30
	z := -1.0
	// Exact product is 1 - 2^-60; rounded product is 1.
	if got := MulAdd(x, y, z); got != 0 {
		t.Fatalf("MulAdd = %v, want 0 (rounded product)", got)
	}
	if got := math.FMA(x, y, z); got == 0 {
		t.Fatalf("math.FMA = 0, want the exact residual")
	}
	if got := MulSub(x, y, 1); got != 0 {
		t.Fatalf("MulSub = %v, want 0", got)
	}
}

// goldenFile records exact output bits so any platform can verify them.
type goldenEntry struct {
	// Digest is the SHA-256 (hex) of every output's bits, in order, over the
	// fixed sample sequence.
	Digest string `json:"digest"`
	// Samples are a few raw input/output pairs, for diagnosing a mismatch.
	Samples []goldenSample `json:"samples"`
}

type goldenSample struct {
	X    string `json:"x"`
	Y    string `json:"y,omitempty"`
	Want string `json:"want"`
}

const goldenPath = "testdata/golden.json"

// goldenData is embedded so a compiled test binary can run on another machine
// without the source tree.
//
//go:embed testdata/golden.json
var goldenData []byte

func hex64(f float64) string { return fmt.Sprintf("%016x", math.Float64bits(f)) }

func computeGolden() map[string]goldenEntry {
	out := map[string]goldenEntry{}
	for _, c := range unaryCases {
		r := rng(0xD1B54A32D192ED03)
		h := newDigest()
		var samples []goldenSample
		n := 0
		for _, rg := range c.ranges {
			for i := 0; i < 5000; i++ {
				x := r.in(rg[0], rg[1])
				y := c.det(x)
				h.add(y)
				if n%4999 == 0 {
					samples = append(samples, goldenSample{X: hex64(x), Want: hex64(y)})
				}
				n++
			}
		}
		out[c.name] = goldenEntry{Digest: h.sum(), Samples: samples}
	}
	for _, c := range binaryCases {
		r := rng(0xD1B54A32D192ED03)
		h := newDigest()
		var samples []goldenSample
		for i := 0; i < 15000; i++ {
			x, y := r.in(c.rangeX[0], c.rangeX[1]), r.in(c.rangeY[0], c.rangeY[1])
			v := c.det(x, y)
			h.add(v)
			if i%4999 == 0 {
				samples = append(samples, goldenSample{X: hex64(x), Y: hex64(y), Want: hex64(v)})
			}
		}
		out[c.name] = goldenEntry{Digest: h.sum(), Samples: samples}
	}
	return out
}

func TestGolden(t *testing.T) {
	got := computeGolden()
	if os.Getenv("DET_REGEN") == "1" {
		b, err := json.MarshalIndent(got, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}
	var want map[string]goldenEntry
	if err := json.Unmarshal(goldenData, &want); err != nil {
		t.Fatal(err)
	}
	for name, w := range want {
		g, ok := got[name]
		if !ok {
			t.Errorf("%s: golden entry has no function", name)
			continue
		}
		if g.Digest != w.Digest {
			for i := range w.Samples {
				if i < len(g.Samples) && g.Samples[i] != w.Samples[i] {
					t.Errorf("%s: sample %d: got %+v, want %+v", name, i, g.Samples[i], w.Samples[i])
				}
			}
			t.Errorf("%s: digest %s, want %s", name, g.Digest, w.Digest)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("%s: missing from golden file", name)
		}
	}
}

// TestLogSubnormal pins a case where the standard library is platform
// dependent: on amd64, math.Log(1e-310) returns about -709.09 instead of the
// correct -713.80, because the assembly does not normalize subnormal inputs.
// js/wasm uses the portable code and gets the correct value. det.Log is
// correct everywhere.
func TestLogSubnormal(t *testing.T) {
	for _, x := range []float64{1e-310, 5e-324, 0x1p-1060, 3e-315} {
		want := refToFloat(refLog(x))
		if d := ulpDiff(Log(x), want); d > 1 {
			t.Errorf("Log(%v) = %v, want %v (%.1f ulp off)", x, Log(x), want, d)
		}
		want10 := refToFloat(new(big.Float).SetPrec(refPrec).Quo(refLog(x), refLog(10)))
		if d := ulpDiff(Log10(x), want10); d > 2 {
			t.Errorf("Log10(%v) = %v, want %v (%.1f ulp off)", x, Log10(x), want10, d)
		}
	}
}
