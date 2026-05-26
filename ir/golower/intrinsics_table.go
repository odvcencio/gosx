// Known stdlib intrinsics that the X.B registry exposes. The lowerer
// consults this table when it encounters a selector-call expression
// (math.Sin(x), strings.Split(s, ","), ...) to decide whether to emit
// an OpCall with the qualified name or a "construct not supported"
// error.
//
// The list is hand-curated rather than read from vm.IntrinsicNames at
// init time so the lowerer can stay independent of the client/vm
// package (avoiding an awkward dependency direction — lowerers
// shouldn't import the evaluator) and so a missing intrinsic surfaces
// as a build-time error rather than a runtime no-op.
//
// Keep this list in sync with intrinsics_*.go in client/vm. A test in
// client/vm/intrinsics_test.go (TestIntrinsicNamesCoversAllFamilies)
// spot-checks the runtime side; cross-checking the lowerer side is the
// responsibility of TestKnownIntrinsicsRegisteredAtRuntime below.

package golower

// knownIntrinsics is the set of "pkg.Func" qualified names the lowerer
// recognizes. Membership in this map gates the lowering of selector
// call expressions; non-members produce a clear "unsupported call"
// error that points at the escape hatch.
var knownIntrinsics = map[string]bool{
	// math
	"math.Sin":   true,
	"math.Cos":   true,
	"math.Sqrt":  true,
	"math.Abs":   true,
	"math.Atan2": true,
	"math.Max":   true,
	"math.Min":   true,
	"math.Floor": true,
	"math.Ceil":  true,
	// math.Pi is the only non-function selector we expose; the lowerer
	// rewrites a bare "math.Pi" identifier into a zero-arg call so the
	// runtime can serve it through the same OpCall path.
	"math.Pi": true,

	// math/rand. The Go package is "rand" so selector text matches.
	"rand.Float64": true,
	"rand.Intn":    true,
	"rand.Seed":    true,

	// strings
	"strings.Split":     true,
	"strings.Join":      true,
	"strings.TrimSpace": true,
	"strings.Replace":   true,
	"strings.ToLower":   true,
	"strings.ToUpper":   true,
	"strings.Contains":  true,
	"strings.HasPrefix": true,
	"strings.HasSuffix": true,

	// strconv
	"strconv.Itoa":        true,
	"strconv.Atoi":        true,
	"strconv.FormatFloat": true,
	"strconv.ParseFloat":  true,

	// sort
	"sort.Slice": true,
}

// isIntrinsic reports whether qualifiedName is a recognized stdlib
// intrinsic. Used by expression lowering to decide between OpCall
// dispatch and an unsupported-call error.
func isIntrinsic(qualifiedName string) bool {
	return knownIntrinsics[qualifiedName]
}

// isConstantIntrinsic reports whether qualifiedName is a selector that
// should be lowered as a zero-arg OpCall even when there are no
// parentheses in the source (math.Pi is the canonical case). The
// lowerer's selector-expression path checks this to avoid emitting an
// OpPropGet/OpSignalGet that would never find the value at runtime.
func isConstantIntrinsic(qualifiedName string) bool {
	return qualifiedName == "math.Pi"
}
