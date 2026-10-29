package vm

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"testing"
)

// The Go spec lets a compiler fuse x*y + z into one instruction that rounds once
// instead of twice. amd64 does it when GOAMD64 is v3 or higher and arm64 always
// does; amd64 at the default level and js/wasm never do. The bundle goldens were
// captured without fusion, so the rotation helpers must give the unfused bits on
// every target. Only an explicit conversion, float64(x*y), prevents fusion.

// unfusedRotatePoint is rotatePointTrig with each product rounded before the add
// or subtract that consumes it, so it gives the same bits on every target.
func unfusedRotatePoint(point point3, t rotTrig) point3 {
	x, y, z := point.X, point.Y, point.Z

	nextY := float64(y*t.cosX) - float64(z*t.sinX)
	nextZ := float64(y*t.sinX) + float64(z*t.cosX)
	y, z = nextY, nextZ

	nextX := float64(x*t.cosY) + float64(z*t.sinY)
	nextZ = float64(-x*t.sinY) + float64(z*t.cosY)
	x, z = nextX, nextZ

	nextX = float64(x*t.cosZ) - float64(y*t.sinZ)
	nextY = float64(x*t.sinZ) + float64(y*t.cosZ)
	return point3{X: nextX, Y: nextY, Z: z}
}

// unfusedInverseRotatePoint is the same reference for inverseRotatePointTrig.
func unfusedInverseRotatePoint(point point3, t rotTrig) point3 {
	x, y, z := point.X, point.Y, point.Z

	nextX := float64(x*t.cosZ) - float64(y*t.sinZ)
	nextY := float64(x*t.sinZ) + float64(y*t.cosZ)
	x, y = nextX, nextY

	nextX = float64(x*t.cosY) + float64(z*t.sinY)
	nextZ := float64(-x*t.sinY) + float64(z*t.cosY)
	x, z = nextX, nextZ

	nextY = float64(y*t.cosX) - float64(z*t.sinX)
	nextZ = float64(y*t.sinX) + float64(z*t.cosX)
	return point3{X: x, Y: nextY, Z: nextZ}
}

// The probe inputs multiply to 1 + 2^-26 + 2^-54. The product needs 54 fraction
// bits, so rounding it drops the 2^-54 term and a fused add keeps it.
var (
	fusionProbeX = 1 + math.Ldexp(1, -27)
	fusionProbeY = 1 + math.Ldexp(1, -27)
	fusionProbeZ = -1.0
)

// fusionProbeMulAdd is the shape a compiler may fuse. Inlining is off so the
// probe cannot be folded into a constant.
//
//go:noinline
func fusionProbeMulAdd(x, y, z float64) float64 { return x*y + z }

// fusionProbeRounded rounds the product first, which stops fusion.
//
//go:noinline
func fusionProbeRounded(x, y, z float64) float64 { return float64(x*y) + z }

// buildFusesMultiplyAdd reports whether this build fuses a multiply and an add.
func buildFusesMultiplyAdd() bool {
	return fusionProbeMulAdd(fusionProbeX, fusionProbeY, fusionProbeZ) !=
		fusionProbeRounded(fusionProbeX, fusionProbeY, fusionProbeZ)
}

// fusionRand is a xorshift generator whose outputs are integers over a power of
// two. Those values are exact, and the generator multiplies no floats, so the
// inputs are the same on every target.
type fusionRand struct{ state uint64 }

func (r *fusionRand) next() int32 {
	r.state ^= r.state << 13
	r.state ^= r.state >> 7
	r.state ^= r.state << 17
	return int32(r.state >> 32)
}

// unit returns a multiple of 2^-24 in [-1, 1).
func (r *fusionRand) unit() float64 { return float64(r.next()>>7) / (1 << 24) }

// coord returns a multiple of 2^-24 in [-16, 16).
func (r *fusionRand) coord() float64 { return float64(r.next()>>3) / (1 << 24) }

func samePoint3Bits(a, b point3) bool {
	return math.Float64bits(a.X) == math.Float64bits(b.X) &&
		math.Float64bits(a.Y) == math.Float64bits(b.Y) &&
		math.Float64bits(a.Z) == math.Float64bits(b.Z)
}

// TestRotationHelpersMatchUnfusedReference compares both rotation helpers with
// references that round every product. On a build that fuses (GOAMD64=v3, arm64)
// it fails if a product reaches an add without a conversion. On a build that
// does not fuse it cannot fail, so TestRotationHelpersRoundEveryProduct covers
// that case.
func TestRotationHelpersMatchUnfusedReference(t *testing.T) {
	r := fusionRand{state: 0x9E3779B97F4A7C15}
	const cases = 20000
	forward, inverse := 0, 0
	var firstForward, firstInverse string
	for i := 0; i < cases; i++ {
		p := point3{X: r.coord(), Y: r.coord(), Z: r.coord()}
		trig := rotTrig{
			sinX: r.unit(), cosX: r.unit(),
			sinY: r.unit(), cosY: r.unit(),
			sinZ: r.unit(), cosZ: r.unit(),
		}
		if got, want := rotatePointTrig(p, trig), unfusedRotatePoint(p, trig); !samePoint3Bits(got, want) {
			if forward == 0 {
				firstForward = fmt.Sprintf("point %v trig %+v: got %v, want %v", p, trig, got, want)
			}
			forward++
		}
		if got, want := inverseRotatePointTrig(p, trig), unfusedInverseRotatePoint(p, trig); !samePoint3Bits(got, want) {
			if inverse == 0 {
				firstInverse = fmt.Sprintf("point %v trig %+v: got %v, want %v", p, trig, got, want)
			}
			inverse++
		}
	}
	t.Logf("this build fuses a multiply and an add: %v", buildFusesMultiplyAdd())
	if forward != 0 {
		t.Errorf("rotatePointTrig differs from the unfused result in %d of %d cases; first: %s", forward, cases, firstForward)
	}
	if inverse != 0 {
		t.Errorf("inverseRotatePointTrig differs from the unfused result in %d of %d cases; first: %s", inverse, cases, firstInverse)
	}
}

// TestRotationHelpersRoundEveryProduct reads the source of both helpers and fails
// if an addition or subtraction takes a product that no float64 conversion
// rounds. TestRotationHelpersMatchUnfusedReference can only fail on a build that
// fuses; this test fails on every build, so the default test run catches the
// mistake. make lint-deterministic enforces the same rule for the packages that
// opt in to //gosx:deterministic; client/vm does not yet.
func TestRotationHelpersRoundEveryProduct(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "scene_render_bundle.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"rotatePointTrig", "inverseRotatePointTrig"} {
		var fn *ast.FuncDecl
		for _, decl := range file.Decls {
			if f, ok := decl.(*ast.FuncDecl); ok && f.Recv == nil && f.Name.Name == name {
				fn = f
			}
		}
		if fn == nil {
			t.Fatalf("%s not found in scene_render_bundle.go; update this test if it moved", name)
		}
		sums := 0
		var bare []string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sum, ok := n.(*ast.BinaryExpr)
			if !ok || (sum.Op != token.ADD && sum.Op != token.SUB) {
				return true
			}
			sums++
			for _, operand := range []ast.Expr{sum.X, sum.Y} {
				if isUnroundedProduct(operand) {
					bare = append(bare, fset.Position(operand.Pos()).String())
				}
			}
			return true
		})
		if len(bare) != 0 {
			t.Errorf("%s: %d products feed an addition or subtraction without a float64 conversion, so a build may fuse them; write float64(a*b). At %v",
				name, len(bare), bare)
		}
		if sums < 6 {
			t.Errorf("%s: found %d additions or subtractions, want at least 6; the test no longer reads the rotation arithmetic", name, sums)
		}
	}
}

// isUnroundedProduct reports whether e is a product, possibly negated or in
// parentheses. A conversion such as float64(a*b) is a call expression, so it is
// not one.
func isUnroundedProduct(e ast.Expr) bool {
	for {
		switch x := e.(type) {
		case *ast.ParenExpr:
			e = x.X
		case *ast.UnaryExpr:
			e = x.X
		case *ast.BinaryExpr:
			return x.Op == token.MUL
		default:
			return false
		}
	}
}
