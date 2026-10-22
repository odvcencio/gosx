package lint

import (
	"go/ast"
	"go/constant"
	"go/token"
	"go/types"
	"math"
	"strings"

	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/passes/inspect"
	"golang.org/x/tools/go/ast/inspector"
)

// Directive marks a package as deterministic.
const Directive = "//gosx:deterministic"

// Rule names, used in diagnostics and in //gosx:allow comments.
const (
	RuleFMA       = "fused-multiply-add"
	RuleMath      = "math-call"
	RuleMapRange  = "map-range"
	RuleClock     = "wall-clock"
	RuleRand      = "global-rand"
	RuleGoroutine = "goroutine"
	RuleSelect    = "select"
)

var allRules = map[string]bool{
	RuleFMA: true, RuleMath: true, RuleMapRange: true, RuleClock: true,
	RuleRand: true, RuleGoroutine: true, RuleSelect: true,
}

// Analyzer reports nondeterministic code in packages marked //gosx:deterministic.
var Analyzer = &analysis.Analyzer{
	Name:     "gosxdet",
	Doc:      "report code that can differ between js/wasm, amd64 and arm64 in packages marked //gosx:deterministic",
	URL:      "https://m31labs.dev/gosx/det",
	Requires: []*analysis.Analyzer{inspect.Analyzer},
	Run:      run,
}

// detReplacement maps math functions to the det function that replaces them.
var detReplacement = map[string]string{
	"Exp": "det.Exp", "Exp2": "det.Exp2", "Log": "det.Log", "Log2": "det.Log2",
	"Log10": "det.Log10", "Pow": "det.Pow", "Sin": "det.Sin", "Cos": "det.Cos",
	"Tan": "det.Tan", "Asin": "det.Asin", "Acos": "det.Acos", "Atan": "det.Atan",
	"Atan2": "det.Atan2", "Hypot": "det.Hypot", "Sinh": "det.Sinh",
	"Cosh": "det.Cosh", "Tanh": "det.Tanh",
	"Sincos": "det.Sin and det.Cos",
}

// mathNoReplacement lists math functions that can differ between platforms and
// have no det version yet.
var mathNoReplacement = map[string]bool{
	"Acosh": true, "Asinh": true, "Atanh": true, "Cbrt": true, "Erf": true,
	"Erfc": true, "Erfcinv": true, "Erfinv": true, "Expm1": true, "Gamma": true,
	"Lgamma": true, "Log1p": true, "J0": true, "J1": true, "Jn": true,
	"Y0": true, "Y1": true, "Yn": true, "Pow10": false,
}

var clockFuncs = map[string]bool{
	"Now": true, "Since": true, "Until": true, "After": true, "AfterFunc": true,
	"Tick": true, "NewTimer": true, "NewTicker": true, "Sleep": true,
}

// randConstructors are the package-level functions that build an explicit,
// seedable source and so are allowed.
var randConstructors = map[string]bool{
	"New": true, "NewSource": true, "NewZipf": true, "NewPCG": true, "NewChaCha8": true,
}

func run(pass *analysis.Pass) (any, error) {
	if !optedIn(pass.Files) {
		return nil, nil
	}
	files := map[*ast.File]bool{}
	allows := map[string]map[int]map[string]bool{} // file -> line -> rules
	for _, f := range pass.Files {
		name := pass.Fset.File(f.Pos()).Name()
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		files[f] = true
		allows[name] = collectAllows(pass, f)
	}

	r := &reporter{pass: pass, allows: allows}
	insp := pass.ResultOf[inspect.Analyzer].(*inspector.Inspector)
	nodes := []ast.Node{
		(*ast.BinaryExpr)(nil), (*ast.AssignStmt)(nil), (*ast.RangeStmt)(nil),
		(*ast.GoStmt)(nil), (*ast.SelectStmt)(nil), (*ast.SelectorExpr)(nil),
	}
	insp.WithStack(nodes, func(n ast.Node, push bool, stack []ast.Node) bool {
		if !push {
			return true
		}
		if !files[fileOf(pass, stack[0])] {
			return true
		}
		switch n := n.(type) {
		case *ast.BinaryExpr:
			r.checkProduct(n, stack)
		case *ast.AssignStmt:
			r.checkMulAssign(n)
		case *ast.RangeStmt:
			if isMap(pass.TypesInfo.TypeOf(n.X)) {
				r.report(n.For, RuleMapRange, "range over a map visits keys in random order; range over a sorted slice of keys instead", nil)
			}
		case *ast.GoStmt:
			r.report(n.Go, RuleGoroutine, "go statement: goroutine scheduling order is not deterministic", nil)
		case *ast.SelectStmt:
			r.report(n.Select, RuleSelect, "select statement: the ready case is chosen at random", nil)
		case *ast.SelectorExpr:
			r.checkSelector(n, stack)
		}
		return true
	})
	return nil, nil
}

func fileOf(pass *analysis.Pass, n ast.Node) *ast.File {
	f, _ := n.(*ast.File)
	return f
}

// optedIn reports whether any file carries the directive before its package
// clause.
func optedIn(files []*ast.File) bool {
	for _, f := range files {
		groups := f.Comments
		for _, g := range groups {
			if g.Pos() >= f.Package {
				break
			}
			for _, c := range g.List {
				if strings.TrimSpace(c.Text) == Directive {
					return true
				}
			}
		}
	}
	return false
}

func collectAllows(pass *analysis.Pass, f *ast.File) map[int]map[string]bool {
	out := map[int]map[string]bool{}
	for _, g := range f.Comments {
		for _, c := range g.List {
			text, ok := strings.CutPrefix(c.Text, "//gosx:allow")
			if !ok {
				continue
			}
			fields := strings.Fields(text)
			if len(fields) == 0 || !allRules[fields[0]] {
				pass.Reportf(c.Pos(), "gosx:allow needs a rule name: one of %s", ruleList())
				continue
			}
			if len(fields) < 2 {
				pass.Reportf(c.Pos(), "gosx:allow %s needs a reason after the rule name", fields[0])
				continue
			}
			line := pass.Fset.Position(c.Pos()).Line
			for _, l := range []int{line, line + 1} {
				if out[l] == nil {
					out[l] = map[string]bool{}
				}
				out[l][fields[0]] = true
			}
		}
	}
	return out
}

func ruleList() string {
	return strings.Join([]string{RuleFMA, RuleMath, RuleMapRange, RuleClock, RuleRand, RuleGoroutine, RuleSelect}, ", ")
}

type reporter struct {
	pass   *analysis.Pass
	allows map[string]map[int]map[string]bool
}

func (r *reporter) report(pos token.Pos, rule, msg string, fixes []analysis.SuggestedFix) {
	p := r.pass.Fset.Position(pos)
	if r.allows[p.Filename][p.Line][rule] {
		return
	}
	r.pass.Report(analysis.Diagnostic{
		Pos:            pos,
		Category:       rule,
		Message:        rule + ": " + msg,
		SuggestedFixes: fixes,
	})
}

func isMap(t types.Type) bool {
	if t == nil {
		return false
	}
	_, ok := t.Underlying().(*types.Map)
	return ok
}

func isFloatish(t types.Type) bool {
	if t == nil {
		return false
	}
	b, ok := t.Underlying().(*types.Basic)
	if !ok {
		return false
	}
	return b.Info()&(types.IsFloat|types.IsComplex) != 0
}

// checkProduct flags an unconverted float product in a context that can fuse.
func (r *reporter) checkProduct(n *ast.BinaryExpr, stack []ast.Node) {
	if n.Op != token.MUL {
		return
	}
	info := r.pass.TypesInfo
	tv := info.Types[n]
	if tv.Value != nil || !isFloatish(tv.Type) {
		return
	}
	if r.exactProduct(n.X) || r.exactProduct(n.Y) {
		return
	}
	if r.productContextOK(n, stack) {
		return
	}
	var fixes []analysis.SuggestedFix
	if name, ok := r.convName(tv.Type); ok {
		fixes = []analysis.SuggestedFix{{
			Message: "round the product with an explicit conversion",
			TextEdits: []analysis.TextEdit{
				{Pos: n.Pos(), End: n.Pos(), NewText: []byte(name + "(")},
				{Pos: n.End(), End: n.End(), NewText: []byte(")")},
			},
		}}
	}
	r.report(n.Pos(), RuleFMA, "float product is not rounded, so a compiler may fuse it into a multiply-add (arm64 does; amd64 with GOAMD64=v3 does); write float64(x*y), or use det.MulAdd or math.FMA", fixes)
}

// convName returns the conversion to write for type t, if it can be spelled
// from this package.
func (r *reporter) convName(t types.Type) (string, bool) {
	switch t := t.(type) {
	case *types.Basic:
		if t.Info()&types.IsFloat != 0 && t.Info()&types.IsUntyped == 0 {
			return t.Name(), true
		}
	case *types.Named:
		if t.Obj().Pkg() == r.pass.Pkg && t.Obj().Parent() == r.pass.Pkg.Scope() {
			// A named float type: converting back to it keeps the type.
			if b, ok := t.Underlying().(*types.Basic); ok && b.Info()&types.IsFloat != 0 {
				return t.Obj().Name(), true
			}
		}
	}
	return "", false
}

// exactProduct reports whether e is a constant power of two, so a product with
// it is exact.
func (r *reporter) exactProduct(e ast.Expr) bool {
	tv := r.pass.TypesInfo.Types[e]
	if tv.Value == nil {
		return false
	}
	v := constant.ToFloat(tv.Value)
	if v.Kind() != constant.Float && v.Kind() != constant.Int {
		return false
	}
	f, _ := constant.Float64Val(v)
	if f == 0 || math.IsInf(f, 0) {
		return false
	}
	frac, _ := math.Frexp(math.Abs(f))
	return frac == 0.5
}

// productContextOK reports whether the product n is used only where it cannot
// be fused with an addition: as an operand of * or /, in a comparison, or
// under a conversion.
func (r *reporter) productContextOK(n ast.Expr, stack []ast.Node) bool {
	cur := ast.Node(n)
	for i := len(stack) - 2; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			cur = p
			continue
		case *ast.UnaryExpr:
			if p.Op == token.SUB || p.Op == token.ADD {
				cur = p
				continue
			}
			return false
		case *ast.BinaryExpr:
			switch p.Op {
			case token.MUL, token.QUO, token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
				return true
			}
			return false
		case *ast.CallExpr:
			if cur != ast.Node(p.Fun) && r.pass.TypesInfo.Types[p.Fun].IsType() {
				return true // explicit conversion
			}
			return false
		default:
			return false
		}
	}
	return false
}

// checkMulAssign flags x *= y on floats: x then holds an unrounded product.
func (r *reporter) checkMulAssign(n *ast.AssignStmt) {
	if n.Tok != token.MUL_ASSIGN || len(n.Lhs) != 1 || len(n.Rhs) != 1 {
		return
	}
	if !isFloatish(r.pass.TypesInfo.TypeOf(n.Lhs[0])) || r.exactProduct(n.Rhs[0]) {
		return
	}
	r.report(n.Pos(), RuleFMA, "*= leaves an unrounded product that a later addition may fuse; write x = float64(x*y)", nil)
}

// checkSelector flags references to nondeterministic standard library
// functions: math, time, math/rand, math/rand/v2, crypto/rand, maps.
func (r *reporter) checkSelector(n *ast.SelectorExpr, stack []ast.Node) {
	obj := r.pass.TypesInfo.Uses[n.Sel]
	fn, ok := obj.(*types.Func)
	if !ok || fn.Pkg() == nil {
		return
	}
	// Methods have a receiver; only package-level functions are checked.
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		return
	}
	name := fn.Name()
	switch fn.Pkg().Path() {
	case "math":
		if rep, ok := detReplacement[name]; ok {
			r.report(n.Pos(), RuleMath, "math."+name+" can differ between platforms (assembly or fused code); use "+rep, nil)
		} else if mathNoReplacement[name] {
			r.report(n.Pos(), RuleMath, "math."+name+" can differ between platforms and has no det replacement yet", nil)
		}
	case "math/cmplx":
		r.report(n.Pos(), RuleMath, "math/cmplx."+name+" is built on math and can differ between platforms; write the complex arithmetic with det functions", nil)
	case "time":
		if clockFuncs[name] {
			r.report(n.Pos(), RuleClock, "time."+name+" reads the wall clock or a timer; pass the simulation time in as an argument", nil)
		}
	case "math/rand", "math/rand/v2":
		if !randConstructors[name] {
			r.report(n.Pos(), RuleRand, "global random source: use rand.New with an explicit seeded source", nil)
		}
	case "crypto/rand":
		r.report(n.Pos(), RuleRand, "crypto/rand is not reproducible; use a seeded math/rand source", nil)
	case "maps":
		if (name == "Keys" || name == "Values" || name == "All") && !r.sortedAround(stack) {
			r.report(n.Pos(), RuleMapRange, "maps."+name+" yields entries in random order; wrap it in slices.Sorted or sort the result", nil)
		}
	}
}

// sortedAround reports whether the maps.* call is an argument of slices.Sorted
// or a similar call that fixes the order.
func (r *reporter) sortedAround(stack []ast.Node) bool {
	// The stack ends with the selector. The nearest enclosing call is the
	// maps.X call itself; the next one out may be slices.Sorted(...).
	seen := false
	for i := len(stack) - 2; i >= 0; i-- {
		switch p := stack[i].(type) {
		case *ast.ParenExpr:
			continue
		case *ast.CallExpr:
			if !seen {
				seen = true
				continue
			}
			sel, ok := p.Fun.(*ast.SelectorExpr)
			if !ok {
				return false
			}
			fn, ok := r.pass.TypesInfo.Uses[sel.Sel].(*types.Func)
			if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "slices" {
				return false
			}
			switch fn.Name() {
			case "Sorted", "SortedFunc", "SortedStableFunc":
				return true
			}
			return false
		default:
			return false
		}
	}
	return false
}
