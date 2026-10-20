package strictcheck

import (
	"m31labs.dev/gosx/ir"
	"m31labs.dev/gosx/transpile"
)

// addFileGosxWarnings runs ir.ValidateWarnings over every file's already-
// compiled *ir.Program and appends its findings to opts.Warnings. This is
// the check-time counterpart to lsp/symbols.go's own ir.ValidateWarnings
// call: the LSP runs it per keystroke against one file with no path
// attached (a document URI stands in for that); a whole-project check run
// covers every file in the package and needs the path on each diagnostic
// to be useful in a terminal, so this sets Span.File the same way
// validateImageContract already does for its own findings.
func addFileGosxWarnings(files []transpile.PackageFile, opts Options) {
	if opts.Warnings == nil {
		return
	}
	for _, file := range files {
		if file.Program == nil {
			continue
		}
		for _, diag := range ir.ValidateWarnings(file.Program) {
			diag.Span.File = file.Path
			addWarnings(opts, []ir.Diagnostic{diag})
		}
	}
}
