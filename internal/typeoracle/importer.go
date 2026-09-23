package typeoracle

import (
	"go/importer"
	"go/token"
	"go/types"
	"io"
)

// importerForCompiler wraps go/importer.ForCompiler(fset, "gc", lookup) —
// see the package doc's Importer choice section for why "gc" mode plus a
// go-list-backed lookup, rather than "source" mode or an x/tools
// dependency, resolves a real module's dependency graph.
func importerForCompiler(fset *token.FileSet, lookup func(path string) (io.ReadCloser, error)) types.Importer {
	return importer.ForCompiler(fset, "gc", importer.Lookup(lookup))
}
