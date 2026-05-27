// Slice X.C.10: lowering benchmarks. The plan asks for "<100 ms to
// lower a 450-line file". This bench builds a synthetic 450-line source
// of mixed-complexity handlers and measures the lowerer end-to-end.

package golower

import (
	"strings"
	"testing"
)

func BenchmarkLowerLargeFile(b *testing.B) {
	src := buildLargeSource(450)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := LowerFile(src)
		// Some constructs in the synthetic file are intentionally
		// supported; an unexpected error would fail the bench.
		if err != nil {
			b.Fatalf("LowerFile: %v", err)
		}
	}
}

// buildLargeSource synthesizes a package whose source is approximately
// `lines` lines tall, containing a mix of pure functions, for-loops,
// and intrinsic calls — covering the patterns the X.C lowerer actually
// sees in production engine-surface handlers.
func buildLargeSource(lines int) []byte {
	var b strings.Builder
	b.WriteString("package handlers\n\nimport \"math\"\n\n")

	currentLines := 4 // package + import + blanks
	idx := 0
	for currentLines < lines {
		funcSrc := makeBenchFunc(idx)
		b.WriteString(funcSrc)
		b.WriteString("\n")
		currentLines += strings.Count(funcSrc, "\n") + 1
		idx++
	}
	return []byte(b.String())
}

// makeBenchFunc produces one function. Four shapes rotate so the
// benchmarked code matches the diversity of real engine-surface files.
// Slice Y.C added the fourth shape (LHS selector / indexed-set) so the
// bench picks up the lowering cost of OpFieldSet / OpIndexSet handlers
// — the actual shape graph_surface.go uses for state mutations.
func makeBenchFunc(i int) string {
	switch i % 6 {
	case 0:
		return funcPure(i)
	case 1:
		return funcLoop(i)
	case 2:
		return funcIntrinsic(i)
	case 3:
		return funcLHS(i)
	case 4:
		return funcUserFn(i)
	default:
		return funcHostMake(i)
	}
}

// funcHostMake produces a Y.E-shaped handler that exercises both
// OpHostCall (`c.MoveTo(...)`, `c.LineTo(...)`) and OpMake (a
// per-call force-table map) in a loop. Mirrors graph_surface.go's
// draw + stepLayout shapes more faithfully than the Y.A-Y.D
// fixtures, which kept canvas dispatch out of scope.
func funcHostMake(i int) string {
	idx := itoa(i)
	return `func HostMake` + idx + `(n int) int {
	m := make(map[string]float64, n)
	for j := 0; j < n; j = j + 1 {
		m["k"] = float64(j)
	}
	return len(m)
}`
}

// funcUserFn produces a Y.D-shaped handler that exercises the user-
// function call registry: a helper declared in the same package is
// called from the handler with multiple arguments and a composite
// return. Mirrors the `makeNode(id, x, y)` / `screenToWorld(sx, sy)`
// dispatch shape from graph_surface.go.
func funcUserFn(i int) string {
	idx := itoa(i)
	return `type pt` + idx + ` struct { X, Y float64 }

func makePt` + idx + `(x float64, y float64) pt` + idx + ` {
	return pt` + idx + `{X: x, Y: y}
}

func split` + idx + `(n float64) (float64, float64) {
	return n * 0.5, n * 0.25
}

func UserFn` + idx + `(n float64) float64 {
	p := makePt` + idx + `(n, n*2.0)
	hx, hy := split` + idx + `(n)
	return p.X + p.Y + hx + hy
}`
}

// funcLHS produces a Y.C-shaped handler that exercises both
// OpFieldSet (`p.X = ...`) and OpIndexSet (`m[k] += ...` and
// `s[i] *= ...`) in a tight loop — the kind of body stepLayout
// and the force-accumulator passes generate in graph_surface.go.
func funcLHS(i int) string {
	return `type lhs` + itoa(i) + ` struct { X, Y float64 }

func LHS` + itoa(i) + `(n int) float64 {
	p := lhs` + itoa(i) + `{X: 0.0, Y: 0.0}
	s := []float64{0.0, 0.0, 0.0, 0.0}
	m := map[string]float64{}
	for j := 0; j < n; j = j + 1 {
		p.X = p.X + 1.0
		p.Y = p.Y + 2.0
		s[j % 4] += float64(j)
		m["acc"] += p.X
	}
	return p.X + s[0] + m["acc"]
}`
}

func funcPure(i int) string {
	return `func Pure` + itoa(i) + `(x int) int { return x*2 + 1 }`
}

func funcLoop(i int) string {
	return `func Loop` + itoa(i) + `(n int) int {
	s := 0
	for j := 0; j < n; j++ {
		s = s + j
	}
	return s
}`
}

func funcIntrinsic(i int) string {
	return `func Intrinsic` + itoa(i) + `(x float64) float64 {
	return math.Sqrt(x*x + 1.0)
}`
}
