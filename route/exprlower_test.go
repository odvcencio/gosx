package route

import (
	"go/ast"
	"go/parser"
	"reflect"
	"strings"
	"testing"

	"m31labs.dev/gosx"
	"m31labs.dev/gosx/ir"
)

// referenceEvalFileNode is the AST-walking interpreter that route used before
// ahead-of-time lowering landed. It stays as the test oracle: the differential
// test below proves that lowerFileExpr answers the same value for every
// supported expression shape.
func referenceEvalFileNode(expr ast.Expr, env fileRenderEnv) any {
	switch node := expr.(type) {
	case *ast.BasicLit:
		return evalBasicLit(node)
	case *ast.Ident:
		return evalIdent(node.Name, env)
	case *ast.BinaryExpr:
		return applyFileBinaryOp(node.Op,
			referenceEvalFileNode(node.X, env),
			referenceEvalFileNode(node.Y, env))
	case *ast.UnaryExpr:
		return applyFileUnaryOp(node.Op, referenceEvalFileNode(node.X, env))
	case *ast.ParenExpr:
		return referenceEvalFileNode(node.X, env)
	case *ast.SelectorExpr:
		return selectValue(referenceEvalFileNode(node.X, env), node.Sel.Name)
	case *ast.IndexExpr:
		return indexValue(referenceEvalFileNode(node.X, env), referenceEvalFileNode(node.Index, env))
	case *ast.CallExpr:
		values := make([]any, 0, len(node.Args))
		for _, arg := range node.Args {
			values = append(values, referenceEvalFileNode(arg, env))
		}
		return callValue(referenceEvalFileNode(node.Fun, env), values)
	default:
		return nil
	}
}

func exprLowerTestEnv() fileRenderEnv {
	return fileRenderEnv{
		values: map[string]any{
			"data": map[string]any{
				"count":  3,
				"name":   "gosx",
				"ratio":  1.5,
				"on":     true,
				"off":    false,
				"items":  []string{"alpha", "beta", "gamma"},
				"lookup": map[string]int{"a": 1, "b": 2},
				"blank":  "",
				"none":   nil,
			},
			"total": 10,
			"label": "Hello",
		},
		funcs: map[string]any{
			"len":   fileEvalLen,
			"upper": strings.ToUpper,
			"add":   func(a, b int) int { return a + b },
			"boom":  func() (string, error) { return "", errNotUsed },
		},
	}
}

var errNotUsed = errTestSentinel("boom")

type errTestSentinel string

func (e errTestSentinel) Error() string { return string(e) }

var exprLowerCases = []string{
	`data.count`,
	`data.name`,
	`data.ratio`,
	`data.missing`,
	`data.none`,
	`total`,
	`label`,
	`unbound`,
	`true`,
	`false`,
	`nil`,
	`42`,
	`3.25`,
	`"literal"`,
	`'x'`,
	`"a" + "b"`,
	`label + " world"`,
	`data.count + 1`,
	`data.count - 5`,
	`data.count * 2`,
	`data.count / 2`,
	`data.count / 0`,
	`data.count % 2`,
	`data.count % 0`,
	`data.count > 2`,
	`data.count >= 3`,
	`data.count < 2`,
	`data.count <= 3`,
	`data.count == 3`,
	`data.count != 3`,
	`data.blank == ""`,
	`data.none == ""`,
	`data.on && data.off`,
	`data.on || data.off`,
	`!data.on`,
	`-data.count`,
	`+data.count`,
	`(data.count + 1) * 2`,
	`data.items[0]`,
	`data.items[9]`,
	`data.lookup["a"]`,
	`data.lookup["zz"]`,
	`len(data.items)`,
	`len(data.name)`,
	`upper(data.name)`,
	`add(1, 2)`,
	`add(1)`,
	`boom()`,
	`data.items`,
	`data.lookup`,
	`^data.count`,
	`func() {}`,
}

func TestLoweredExprMatchesReferenceInterpreter(t *testing.T) {
	env := exprLowerTestEnv()
	for _, src := range exprLowerCases {
		parsed, err := parser.ParseExpr(src)
		if err != nil {
			t.Fatalf("parse %q: %v", src, err)
		}
		want := referenceEvalFileNode(parsed, env)
		got := lowerFileExpr(parsed)(env)
		if !reflect.DeepEqual(want, got) {
			t.Errorf("expr %q: lowered = %#v (%T), reference = %#v (%T)", src, got, got, want, want)
		}
		// evalFileExpr goes through the cache. It must agree too.
		if cached := evalFileExpr(src, env); !reflect.DeepEqual(want, cached) {
			t.Errorf("expr %q: cached = %#v, reference = %#v", src, cached, want)
		}
	}
}

func TestLoweredExprShadowsBindingsPerScope(t *testing.T) {
	env := fileRenderEnv{values: map[string]any{"item": "base"}}
	inner := env.withValue("item", "loop").withValue("i", 7)

	if got := evalFileExpr("item", env); got != "base" {
		t.Fatalf("outer item = %#v, want base", got)
	}
	if got := evalFileExpr("item", inner); got != "loop" {
		t.Fatalf("inner item = %#v, want loop", got)
	}
	if got := evalFileExpr("i", inner); got != 7 {
		t.Fatalf("inner i = %#v, want 7", got)
	}
	// The parent env must not see the child binding.
	if got := evalFileExpr("i", env); got != nil {
		t.Fatalf("outer i = %#v, want nil", got)
	}
}

func TestCompiledFileExprCachesParseFailure(t *testing.T) {
	resetFileExprCache()
	const bad = "data.((("
	if got := compiledFileExpr(bad); got != nil {
		t.Fatalf("expected nil for malformed expression, got %#v", got)
	}
	if fileExprCacheLen() != 1 {
		t.Fatalf("expected the failure to be cached, cache len = %d", fileExprCacheLen())
	}
	if got := compiledFileExpr(bad); got != nil {
		t.Fatalf("expected nil on the second call, got %#v", got)
	}
	if fileExprCacheLen() != 1 {
		t.Fatalf("cache grew on a repeat failure, len = %d", fileExprCacheLen())
	}
}

func TestCompiledFileExprStopsAtSizeCap(t *testing.T) {
	resetFileExprCache()
	t.Cleanup(resetFileExprCache)

	for i := 0; i < fileExprCacheMaxEntries+64; i++ {
		src := "unique_" + itoaTest(i)
		if compiledFileExpr(src) == nil {
			t.Fatalf("expected %q to lower", src)
		}
	}
	if got := fileExprCacheLen(); got > fileExprCacheMaxEntries {
		t.Fatalf("cache exceeded the cap: len = %d, cap = %d", got, fileExprCacheMaxEntries)
	}
	// Overflow keys still evaluate. They just lower on every call.
	if got := evalFileExpr("unique_1 == nil", fileRenderEnv{}); got != true {
		t.Fatalf("overflow expression = %#v, want true", got)
	}
}

func itoaTest(i int) string {
	if i == 0 {
		return "0"
	}
	var digits [24]byte
	pos := len(digits)
	for i > 0 {
		pos--
		digits[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(digits[pos:])
}

func TestPrewarmFileProgramExprsFillsCache(t *testing.T) {
	source := []byte(`package bench

func Page() Node {
	return <div class={data.cls} {...data.rest}>{data.title}</div>
}
`)
	prog, err := gosx.Compile(source)
	if err != nil {
		t.Fatal(err)
	}

	resetFileExprCache()
	t.Cleanup(resetFileExprCache)

	prewarmFileProgramExprs(prog)

	want := map[string]bool{}
	for i := range prog.Nodes {
		node := &prog.Nodes[i]
		if node.Kind == ir.NodeExpr {
			want[strings.TrimSpace(node.Text)] = true
		}
		for _, attr := range node.Attrs {
			switch attr.Kind {
			case ir.AttrExpr, ir.AttrSpread:
				want[strings.TrimSpace(attr.Expr)] = true
			}
		}
	}
	if len(want) == 0 {
		t.Fatal("fixture has no expression holes")
	}

	fileExprCacheMu.RLock()
	defer fileExprCacheMu.RUnlock()
	for src := range want {
		if _, ok := fileExprCache[src]; !ok {
			t.Errorf("prewarm missed %q", src)
		}
	}
}
