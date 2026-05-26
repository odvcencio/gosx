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

// makeBenchFunc produces one function. Three shapes rotate so the
// benchmarked code matches the diversity of real engine-surface files.
func makeBenchFunc(i int) string {
	switch i % 3 {
	case 0:
		return funcPure(i)
	case 1:
		return funcLoop(i)
	default:
		return funcIntrinsic(i)
	}
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
