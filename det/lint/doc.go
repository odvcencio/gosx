// Package lint is the GoSX deterministic-code analyzer.
//
// A package opts in by putting a //gosx:deterministic directive line in a
// comment before its package clause, in any file:
//
//	//gosx:deterministic
//
//	// Package physics ...
//	package physics
//
// The analyzer then reports code that can give different bits on js/wasm,
// amd64 and arm64:
//
//	fused-multiply-add  a float product not rounded by an explicit conversion
//	math-call           a math function with a det replacement, or with none
//	map-range           ranging over a map, whose order is random
//	wall-clock          time.Now, time.Since and other clock or timer calls
//	global-rand         the global math/rand source, or crypto/rand
//	goroutine           a go statement
//	select              a select statement
//
// Type information is required to tell float arithmetic from integer
// arithmetic, so the analyzer is built on go/types through
// golang.org/x/tools/go/analysis. Syntax-only parsers such as gotreesitter
// cannot resolve types.
//
// # The product rule
//
// The Go spec lets a compiler fuse x*y + z into one operation with one
// rounding. The product x*y is exact in that case, so the sum differs from the
// unfused result. The spec says that only an explicit conversion rounds the
// product: float64(x*y) + z cannot be fused. An assignment, a function
// argument and a return value do not round, and inlining can move the product
// next to an addition in another function.
//
// The analyzer therefore accepts an unconverted float product only when its
// value is used by another multiplication or division, by a comparison, or by
// a conversion. In every other place write float64(x*y), or use det.MulAdd.
// A product with a constant power of two (x*2, 0.5*x) is accepted, because
// that product is exact except at the extremes of the exponent range.
//
// # Suppressing a report
//
// A line comment //gosx:allow <rule> <reason> on the same line, or on the
// line above, silences one rule for that line. The reason is required.
//
// Test files (_test.go) are not checked.
//
// # Running it
//
//	gosx detlint ./physics/...
//	go build -o gosx-detlint ./cmd/gosx-detlint && go vet -vettool=$PWD/gosx-detlint ./physics/...
package lint
