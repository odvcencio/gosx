package route

import (
	"go/parser"
	"strings"
	"sync"

	"m31labs.dev/gosx/ir"
)

// Compiled-expression cache for `.gsx` `{expr}` holes.
//
// WHY: ir.Node.Text keeps the raw Go source of each expression hole, so the
// render path called go/parser.ParseExpr once per hole per request. A profile of
// a `.gsx` page attributed 37.1% of all allocated objects to ParseExpr. Every
// call built a fresh token.FileSet, a scanner and an AST for source text that
// never changes between requests.
//
// The cache stores the lowered closure tree (see route/exprlower.go), not the
// AST, so a cache hit skips both the parse and the per-node type switch.
//
// Sharing one entry across goroutines is safe. A lowered closure captures only
// the operator, the folded literal value and child closures. It writes nothing.
// The same held for the ast.Expr the cache stored before: evalFileNode and its
// helpers only read Kind, Value, Name, Op, X, Y, Sel, Index, Fun and Args.
//
// The key set is finite in production because expression sources come from
// compiled templates, and prewarmFileProgramExprs fills the whole set the first
// time a program compiles. A pathological application could still build keys at
// runtime, so the cache stops accepting new entries at
// fileExprCacheMaxEntries and lowers on every call for the overflow.
// Stop-caching beats eviction here: no key is hotter than another, and an
// eviction policy would add a write lock to the read path.

const fileExprCacheMaxEntries = 8192

var (
	fileExprCacheMu sync.RWMutex
	// A present entry with a nil value records a parse failure. Cache the
	// failure so a malformed hole does not re-parse on every render.
	fileExprCache = make(map[string]fileExprFunc, 128)
)

// compiledFileExpr returns the lowered form of a `{expr}` hole, or nil when
// go/parser rejects the source.
func compiledFileExpr(src string) fileExprFunc {
	src = strings.TrimSpace(src)
	if src == "" {
		return nil
	}

	fileExprCacheMu.RLock()
	cached, ok := fileExprCache[src]
	fileExprCacheMu.RUnlock()
	if ok {
		return cached
	}

	var lowered fileExprFunc
	if parsed, err := parser.ParseExpr(src); err == nil {
		lowered = lowerFileExpr(parsed)
	}

	fileExprCacheMu.Lock()
	if len(fileExprCache) < fileExprCacheMaxEntries {
		fileExprCache[src] = lowered
	}
	fileExprCacheMu.Unlock()

	return lowered
}

// prewarmFileProgramExprs lowers every expression a program can evaluate.
//
// Run it once when a program enters the compile cache so the parse happens at
// cache-fill time, not on the first request. After it returns, the render path
// never calls go/parser for that program.
func prewarmFileProgramExprs(prog *ir.Program) {
	if prog == nil {
		return
	}
	for i := range prog.Nodes {
		node := &prog.Nodes[i]
		if node.Kind == ir.NodeExpr {
			compiledFileExpr(node.Text)
		}
		for _, attr := range node.Attrs {
			switch attr.Kind {
			case ir.AttrExpr, ir.AttrSpread:
				compiledFileExpr(attr.Expr)
			}
		}
	}
}

// resetFileExprCache clears the cache. Tests use it to measure a cold lowering.
func resetFileExprCache() {
	fileExprCacheMu.Lock()
	fileExprCache = make(map[string]fileExprFunc, 128)
	fileExprCacheMu.Unlock()
}

// fileExprCacheLen reports the current entry count. Tests use it to prove the
// prewarm ran and the size cap holds.
func fileExprCacheLen() int {
	fileExprCacheMu.RLock()
	n := len(fileExprCache)
	fileExprCacheMu.RUnlock()
	return n
}
